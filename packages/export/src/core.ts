// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {
  isExportPlan,
  isExportSourceManifest,
  isExternalStateSummary,
  isViewData,
  type CompletedExportArtifactEntry,
  type ExportPlan,
  type ExportSourceManifest,
  type ViewData,
} from "@layerdraw/protocol/semantic";
import { assertRenderData, type RenderData } from "@layerdraw/render";
import { canonicalBytes, canonicalJSON, equalBytes, sha256 } from "./canonical.js";
import { serializeCSV } from "./csv.js";
import { serializeSVG } from "./svg.js";
import type {
  ExportArtifact,
  ExportDiagnosticCode,
  ExportResourceLimits,
  ExportResult,
  ResolvedExportResource,
  SerializerInput,
} from "./types.js";

const DEFAULT_LIMITS: ExportResourceLimits = Object.freeze({
  max_input_bytes: 64 * 1024 * 1024,
  max_artifacts: 64,
  max_representations: 1_000_000,
  max_units: 10_000,
  max_resources: 10_000,
  max_svg_primitives: 1_000_000,
  max_csv_rows: 1_000_000,
  max_output_bytes: 256 * 1024 * 1024,
});
const DIGEST = /^sha256:[0-9a-f]{64}$/u;
const fidelity = new Map([["lossy", 0], ["visual_only", 1], ["traceable_summary", 2], ["lossless", 3]]);
const visualShapes = new Set(["diagram", "matrix", "tree", "flow", "context", "diff"]);
const inputKeys = new Set(["export_plan","view_data","render_data","serializer_profile","rasterizer_profile","rasterizer","assets","fonts","state_summary","clock","signal"]);
const limitKeys = new Set(Object.keys(DEFAULT_LIMITS));

class Failure extends Error {
  constructor(readonly code: ExportDiagnosticCode) { super(code); }
}
const fail = (code: ExportDiagnosticCode): never => { throw new Failure(code); };
const same = (left: unknown, right: unknown): boolean => canonicalJSON(left) === canonicalJSON(right);
const closed = (value: unknown, keys: ReadonlySet<string>): value is Record<string, unknown> =>
  Boolean(value && typeof value === "object" && !Array.isArray(value) && Object.keys(value).every((key) => keys.has(key)));

function validatePublicInput(input: SerializerInput): void {
  if (!closed(input, inputKeys) || !("export_plan" in input) || !("view_data" in input) || !Array.isArray(input.assets) || !Array.isArray(input.fonts))
    fail("export.serializer_failed");
  const rawArtifacts = input.export_plan && typeof input.export_plan === "object"
    ? (input.export_plan as { artifacts?: unknown }).artifacts : undefined;
  if (Array.isArray(rawArtifacts) && rawArtifacts.some((item) =>
    Boolean(item && typeof item === "object" && Object.prototype.hasOwnProperty.call(item, "content_digest"))))
    fail("export.source_manifest_invalid");
  if (input.serializer_profile !== undefined && (!closed(input.serializer_profile, new Set(["schema_version","ref","limits"])) ||
    (input.serializer_profile.limits !== undefined && !closed(input.serializer_profile.limits, limitKeys))))
    fail("export.profile_incompatible");
  if (input.clock !== undefined && (!closed(input.clock, new Set(["fixed_rfc3339"])) ||
    typeof input.clock.fixed_rfc3339 !== "string" || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z$/u.test(input.clock.fixed_rfc3339)))
    fail("export.profile_incompatible");
}

function limits(input: SerializerInput): ExportResourceLimits {
  const overrides = input.serializer_profile?.limits ?? {};
  const result = { ...DEFAULT_LIMITS, ...overrides };
  for (const value of Object.values(result))
    if (!Number.isSafeInteger(value) || value <= 0) fail("export.profile_incompatible");
  return result;
}

function validateTopology(plan: ExportPlan, value: ExportResourceLimits): void {
  if (plan.artifacts.length > value.max_artifacts || plan.representations.length > value.max_representations || plan.units.length > value.max_units)
    fail("export.serializer_failed");
  if (plan.artifacts.filter((item) => item.primary).length !== 1) fail("export.source_manifest_invalid");
  const roles = new Map(plan.artifacts.map((item) => [item.role, item]));
  const paths = new Set<string>();
  if (roles.size !== plan.artifacts.length) fail("export.source_manifest_invalid");
  for (const artifact of plan.artifacts) {
    if (paths.has(artifact.logical_path) || artifact.logical_path === plan.source_manifest_path) fail("export.source_manifest_invalid");
    paths.add(artifact.logical_path);
  }
  const units = new Map(plan.units.map((item) => [item.unit_id, item]));
  if (units.size !== plan.units.length) fail("export.source_manifest_invalid");
  for (const unit of plan.units) if (!roles.has(unit.artifact_role)) fail("export.source_manifest_invalid");
  const locatorSets = new Map<string, Set<string>>();
  const representationTuples = new Set<string>();
  for (const representation of plan.representations) {
    if (representation.disposition === "omitted") continue;
    const unit = representation.unit_id ? units.get(representation.unit_id) : undefined;
    if (!representation.artifact_role || !roles.has(representation.artifact_role) || !unit || unit.artifact_role !== representation.artifact_role ||
      !unit.viewdata_keys.includes(representation.viewdata_key) || !representation.locator)
      fail("export.source_manifest_invalid");
    const role = representation.artifact_role!;
    const locator = representation.locator!;
    const tuple = `${representation.viewdata_key}\0${role}\0${locator}`;
    if (representationTuples.has(tuple)) fail("export.source_manifest_invalid");
    representationTuples.add(tuple);
    const set = locatorSets.get(role) ?? new Set<string>();
    if (set.has(locator)) fail("export.source_manifest_invalid");
    set.add(locator);
    locatorSets.set(role, set);
  }
  if (plan.source_manifest_required !== Boolean(plan.source_manifest_path)) fail("export.source_manifest_invalid");
}

function validateCompatibility(plan: ExportPlan, view: ViewData): void {
  if (plan.serializer_options.kind !== plan.format || plan.exporter_profile.format !== plan.format || plan.serializer_profile.format !== plan.format)
    fail("export.profile_incompatible");
  const supported = plan.format === "json" ||
    ((plan.format === "svg" || plan.format === "png") && visualShapes.has(view.kind)) ||
    ((plan.format === "csv" || plan.format === "tsv") && (view.kind === "table" || view.kind === "matrix"));
  if (!supported) fail("export.unsupported_shape_format");
  if ((fidelity.get(plan.requested_fidelity) ?? 99) > (fidelity.get(plan.effective_maximum_fidelity) ?? -1))
    fail("export.fidelity_unavailable");
  if (!plan.recipe_address.startsWith(`${view.view_address}:export:`)) fail("export.render_input_mismatch");
  if (plan.state_policy !== view.state_policy || !same(plan.state_input, view.state_input)) fail("export.render_input_mismatch");
}

function validateMappings(plan: ExportPlan, view: ViewData): void {
  const sources = new Map<string, unknown>([["viewdata-root", view.source]]);
  const visit = (value: unknown, inheritedSource?: unknown): void => {
    if (Array.isArray(value)) { for (const item of value) visit(item, inheritedSource); return; }
    if (!value || typeof value !== "object") return;
    const object = value as Record<string, unknown>;
    const source = "source" in object ? object.source : inheritedSource;
    if (typeof object.key === "string") {
      if (sources.has(object.key)) fail("export.render_input_mismatch");
      sources.set(object.key, source);
    }
    for (const item of Object.values(object)) visit(item, source);
  };
  visit(view[view.kind], view.source);
  const represented = new Set<string>();
  for (const representation of plan.representations) {
    const expectedSource = sources.get(representation.viewdata_key);
    const tuple = `${representation.viewdata_key}\0${representation.artifact_role ?? ""}\0${representation.locator ?? ""}`;
    if (represented.has(tuple) || !sources.has(representation.viewdata_key) || !same(expectedSource, representation.source))
      fail("export.render_input_mismatch");
    represented.add(tuple);
  }
  for (const unit of plan.units) for (const key of unit.viewdata_keys)
    if (!sources.has(key)) fail("export.render_input_mismatch");
}

async function semanticHash(domain: string, value: unknown): Promise<`sha256:${string}`> {
  const prefix = new TextEncoder().encode(`layerdraw-language-1\0${domain}\0`);
  const body = canonicalBytes(value);
  const joined = new Uint8Array(prefix.length + body.length);
  joined.set(prefix); joined.set(body, prefix.length);
  return sha256(joined);
}

function stableDiagnostics(view: ViewData): unknown[] {
  const removeMessages = (value: unknown): unknown => {
    if (Array.isArray(value)) return value.map(removeMessages);
    if (value && typeof value === "object") return Object.fromEntries(Object.entries(value)
      .filter(([key]) => key !== "message").map(([key, item]) => [key, removeMessages(item)]));
    return value;
  };
  return removeMessages(view.diagnostics) as unknown[];
}

async function validateViewHash(plan: ExportPlan, view: ViewData): Promise<void> {
  const projection = { ...view, diagnostics: stableDiagnostics(view) };
  if (await semanticHash("export-viewdata", projection) !== plan.view_data_hash) fail("export.render_input_mismatch");
}

async function validateResources(required: readonly string[], supplied: readonly ResolvedExportResource[], missing: "export.asset_missing" | "export.font_missing", value: ExportResourceLimits): Promise<void> {
  if (supplied.length > value.max_resources || supplied.length !== required.length) fail(missing);
  const seen = new Set<string>();
  for (const item of supplied) {
    if (!closed(item, new Set(["digest","bytes"])) || !DIGEST.test(item.digest) || !(item.bytes instanceof Uint8Array) || seen.has(item.digest)) fail(missing);
    seen.add(item.digest);
    if (await sha256(item.bytes) !== item.digest) fail(missing);
  }
  if (required.some((digest) => !seen.has(digest))) fail(missing);
}

function validateRender(plan: ExportPlan, view: ViewData, render: RenderData | undefined): RenderData | undefined {
  if (plan.requires_renderer && !render) fail("export.render_required");
  if (!plan.requires_renderer && render) fail("export.render_input_mismatch");
  if (!render) return undefined;
  try { assertRenderData(render); } catch { fail("export.render_input_mismatch"); }
  if (render.shape !== view.kind || render.kind !== view.kind || render.view_data_hash !== plan.view_data_hash || plan.layout_requirement !== "presentation_geometry")
    fail("export.render_input_mismatch");
  if (!same(render.resolved_asset_digests, plan.required_asset_digests) || !same(render.resolved_font_digests, plan.required_font_digests))
    fail("export.render_input_mismatch");
  const rendered = new Map<string, ExportPlan["representations"]>();
  for (const representation of plan.representations) {
    if (representation.disposition !== "rendered" || representation.viewdata_key === "viewdata-root") continue;
    const matches = rendered.get(representation.viewdata_key) ?? [];
    rendered.set(representation.viewdata_key, [...matches, representation]);
  }
  const bound = new Set<string>();
  for (const binding of render.source_bindings) {
    const key = binding.viewdata_key;
    const source = binding.source_refs;
    if (!key || !source) fail("export.render_input_mismatch");
    const matches = rendered.get(key!);
    if (!matches || matches.length !== 1 || !same(matches[0]!.source, source)) fail("export.render_input_mismatch");
    bound.add(key!);
  }
  for (const key of rendered.keys()) if (!bound.has(key)) fail("export.render_input_mismatch");
  return render;
}

function profile(input: SerializerInput): void {
  if (!input.serializer_profile) fail("export.profile_missing");
  const serializerProfile = input.serializer_profile!;
  if (serializerProfile.schema_version !== 1 || !same(serializerProfile.ref, input.export_plan.serializer_profile))
    fail("export.profile_incompatible");
}

function viewDataArtifact(input: SerializerInput): Uint8Array {
  const options = input.export_plan.serializer_options;
  const viewData = { ...input.view_data, diagnostics: stableDiagnostics(input.view_data) };
  const artifact: Record<string, unknown> = { format: "layerdraw-viewdata", schema_version: 1, view_data: viewData };
  if (options.diagnostics) artifact.diagnostics = stableDiagnostics(input.view_data);
  if (options.state_summary) {
    if (!input.state_summary || !isExternalStateSummary(input.state_summary)) fail("export.serializer_failed");
    artifact.state_summary = input.state_summary;
  } else if (input.state_summary) fail("export.serializer_failed");
  return canonicalBytes(artifact, true);
}

async function validateStateSummary(input: SerializerInput): Promise<void> {
  const selected = Boolean(input.export_plan.serializer_options.state_summary);
  if (!selected) {
    if (input.state_summary !== undefined || input.export_plan.state_summary_hash !== undefined) fail("export.serializer_failed");
    return;
  }
  const summary = input.state_summary;
  if (!summary || !isExternalStateSummary(summary) || !input.export_plan.state_summary_hash || input.view_data.revision.kind !== "single" ||
    summary.definition_hash !== input.view_data.revision.definition_hash || summary.state_version !== input.export_plan.state_input.state_version)
    fail("export.serializer_failed");
  const validSummary = summary!;
  if (await sha256(canonicalBytes(validSummary.payload)) !== validSummary.payload_hash ||
    await semanticHash("export-state-summary", validSummary) !== input.export_plan.state_summary_hash)
    fail("export.serializer_failed");
}

function pngDimensions(plan: ExportPlan): { width: number; height: number; density: number; background: string } {
  const widthOption = plan.serializer_options.width;
  const heightOption = plan.serializer_options.height;
  if (!widthOption || !heightOption || widthOption.kind !== "value" || heightOption.kind !== "value") fail("export.profile_incompatible");
  const width = Number(widthOption!.value), height = Number(heightOption!.value), density = Number(plan.serializer_options.scale);
  if (!(Number.isSafeInteger(width) && width > 0 && Number.isSafeInteger(height) && height > 0 && Number.isFinite(density) && density > 0)) fail("export.profile_incompatible");
  return { width, height, density, background: plan.serializer_options.background ?? "transparent" };
}

function validPNG(bytes: Uint8Array, width: number, height: number): boolean {
  if (bytes.length < 24 || !equalBytes(bytes.subarray(0, 8), new Uint8Array([137,80,78,71,13,10,26,10]))) return false;
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  let offset = 8, chunks = 0, sawHeader = false, sawData = false, sawEnd = false;
  while (offset + 12 <= bytes.length && chunks < 1_000_000) {
    const length = view.getUint32(offset);
    if (length > bytes.length - offset - 12) return false;
    const type = String.fromCharCode(...bytes.subarray(offset + 4, offset + 8));
    if (!/^[A-Za-z]{4}$/u.test(type)) return false;
    if (crc32(bytes.subarray(offset + 4, offset + 8 + length)) !== view.getUint32(offset + 8 + length)) return false;
    if (chunks === 0) {
      if (type !== "IHDR" || length !== 13 || view.getUint32(offset + 8) !== width || view.getUint32(offset + 12) !== height) return false;
      const bitDepth = bytes[offset + 16]!, colorType = bytes[offset + 17]!;
      if (![1,2,4,8,16].includes(bitDepth) || ![0,2,3,4,6].includes(colorType) || bytes[offset + 18]! !== 0 || bytes[offset + 19]! !== 0 || bytes[offset + 20]! > 1) return false;
      sawHeader = true;
    } else if (type === "IHDR") return false;
    if (type === "IDAT") { if (length === 0) return false; sawData = true; }
    if (type === "IEND") { if (length !== 0) return false; sawEnd = true; offset += 12; break; }
    offset += 12 + length;
    chunks += 1;
  }
  return sawHeader && sawData && sawEnd && offset === bytes.length;
}

function crc32(bytes: Uint8Array): number {
  let crc = 0xffffffff;
  for (const byte of bytes) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit += 1) crc = (crc >>> 1) ^ (0xedb88320 & -(crc & 1));
  }
  return (crc ^ 0xffffffff) >>> 0;
}

async function rasterize(input: SerializerInput, render: RenderData, value: ExportResourceLimits): Promise<Uint8Array> {
  if (!input.rasterizer || !input.rasterizer_profile) fail("export.profile_missing");
  const implementation = input.rasterizer!;
  const rasterizerProfile = input.rasterizer_profile!;
  if (implementation.api_version !== 1 || !same(implementation.profile, rasterizerProfile)) fail("export.profile_incompatible");
  const dimensions = pngDimensions(input.export_plan);
  const svg = serializeSVG(input.export_plan, render, value);
  const request = { api_version: 1 as const, profile: rasterizerProfile, svg, ...dimensions, assets: input.assets, fonts: input.fonts, ...(input.signal ? {signal: input.signal} : {}) };
  try {
    const first = await implementation.rasterize(request);
    const second = await implementation.rasterize(request);
    if (!same(first.profile, rasterizerProfile) || !same(second.profile, rasterizerProfile) || first.width !== dimensions.width || first.height !== dimensions.height || first.density !== dimensions.density || !equalBytes(first.bytes, second.bytes) || !validPNG(first.bytes, dimensions.width, dimensions.height))
      fail("export.serializer_failed");
    return first.bytes;
  } catch (error) {
    if (error instanceof Failure) throw error;
    fail("export.serializer_failed");
  } finally {
    try { await implementation.dispose?.(); } catch { /* host errors do not cross the boundary */ }
  }
  throw new Failure("export.serializer_failed");
}

async function artifactBytes(input: SerializerInput, render: RenderData | undefined, value: ExportResourceLimits): Promise<Map<string, Uint8Array>> {
  const output = new Map<string, Uint8Array>();
  if (input.export_plan.format === "json") output.set(input.export_plan.artifacts[0]!.role, viewDataArtifact(input));
  else if (input.export_plan.format === "csv" || input.export_plan.format === "tsv")
    return serializeCSV(input.export_plan, input.view_data, value);
  else if (input.export_plan.format === "svg") output.set(input.export_plan.artifacts[0]!.role, serializeSVG(input.export_plan, render!, value));
  else if (input.export_plan.format === "png") output.set(input.export_plan.artifacts[0]!.role, await rasterize(input, render!, value));
  else fail("export.unsupported_shape_format");
  return output;
}

async function manifest(plan: ExportPlan, view: ViewData, entries: readonly CompletedExportArtifactEntry[]): Promise<{ value: ExportSourceManifest; bytes: Uint8Array; digest: `sha256:${string}` }> {
  const primary = entries.find((item) => item.primary);
  if (!primary) fail("export.source_manifest_invalid");
  const value: ExportSourceManifest = {
    format: "layerdraw-export-sources", schema_version: 1, invocation_hash: plan.invocation_hash,
    view_data_hash: plan.view_data_hash, state_policy: plan.state_policy, state_input: plan.state_input,
    recipe_hash: plan.recipe_hash, profile_ref_hash: plan.profile_ref_hash, profile_requirements_hash: plan.profile_requirements_hash,
    recipe_address: plan.recipe_address, revision: view.revision, requested_fidelity: plan.requested_fidelity,
    native_maximum_fidelity: plan.native_maximum_fidelity, effective_maximum_fidelity: plan.effective_maximum_fidelity,
    fidelity_basis: plan.fidelity_basis, exporter_profile: plan.exporter_profile, serializer_profile: plan.serializer_profile,
    primary_artifact: primary!.logical_path, artifacts: entries, representations: plan.representations,
    asset_digests: plan.required_asset_digests, font_digests: plan.required_font_digests,
    ...(plan.state_summary_hash ? { state_summary_hash: plan.state_summary_hash } : {}),
  };
  if (!isExportSourceManifest(value)) fail("export.source_manifest_invalid");
  const bytes = canonicalBytes(value, true);
  return { value, bytes, digest: await sha256(bytes) };
}

export async function serializeExport(input: SerializerInput): Promise<ExportResult> {
  try {
    validatePublicInput(input);
    if (input.signal?.aborted) fail("export.serializer_failed");
    if (!isExportPlan(input.export_plan) || !isViewData(input.view_data)) fail("export.serializer_failed");
    profile(input);
    const value = limits(input);
    if (input.assets.length + input.fonts.length > value.max_resources) fail("export.serializer_failed");
    const resourceBytes = [...input.assets, ...input.fonts].reduce((total, item) =>
      total + (item && typeof item === "object" && item.bytes instanceof Uint8Array ? item.bytes.byteLength : 0), 0);
    const inputBytes = canonicalBytes(input.export_plan).byteLength + canonicalBytes(input.view_data).byteLength + resourceBytes;
    if (inputBytes > value.max_input_bytes) fail("export.serializer_failed");
    validateTopology(input.export_plan, value);
    validateCompatibility(input.export_plan, input.view_data);
    validateMappings(input.export_plan, input.view_data);
    await validateViewHash(input.export_plan, input.view_data);
    await validateStateSummary(input);
    await validateResources(input.export_plan.required_asset_digests, input.assets, "export.asset_missing", value);
    await validateResources(input.export_plan.required_font_digests, input.fonts, "export.font_missing", value);
    const render = validateRender(input.export_plan, input.view_data, input.render_data);
    const byteMap = await artifactBytes(input, render, value);
    const artifacts: ExportArtifact[] = [];
    let outputBytes = 0;
    for (const entry of input.export_plan.artifacts) {
      const bytes = byteMap.get(entry.role);
      if (!bytes) fail("export.serializer_failed");
      const artifactBytesValue = bytes!;
      outputBytes += artifactBytesValue.byteLength;
      if (outputBytes > value.max_output_bytes) fail("export.serializer_failed");
      artifacts.push({ entry: { ...entry, content_digest: await sha256(artifactBytesValue) }, bytes: artifactBytesValue });
    }
    if (!input.export_plan.source_manifest_required) return { ok: true, artifacts };
    const sidecar = await manifest(input.export_plan, input.view_data, artifacts.map((item) => item.entry));
    if (outputBytes + sidecar.bytes.byteLength > value.max_output_bytes) fail("export.serializer_failed");
    return { ok: true, artifacts, source_manifest: sidecar.value, source_manifest_bytes: sidecar.bytes,
      source_manifest_path: input.export_plan.source_manifest_path!, source_manifest_digest: sidecar.digest };
  } catch (error) {
    const code = error instanceof Failure ? error.code : "export.serializer_failed";
    return { ok: false, diagnostics: [{ code, severity: "error" }] };
  }
}
