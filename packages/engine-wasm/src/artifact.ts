// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {
  browserTransportLimits,
  failure,
  failureDefinitions,
  hasExactKeys,
  hasInitialTransportLimits,
  isArrayRecord,
  isBoundedOpaqueString,
  isDigest,
  isFixedArrayBuffer,
  isPlainRecord,
  maxEndpointGenerationBytes,
  transportLimitKeys,
  validateEndpointResponse,
  validateFailure,
  workerProtocol,
  workerProtocolVersion,
  type EngineWorkerBlob,
  type EngineWorkerFailure,
  type EngineWorkerTransportLimits,
} from "./protocol.js";
import {
  EngineEndpointInitializationError,
  type EngineByteEndpoint,
  type EngineByteEndpointInit,
  type EngineByteEndpointResult,
} from "./worker-runtime.js";

const requiredArtifactFiles = Object.freeze([
  "LICENSE",
  "LICENSING.md",
  "NOTICE",
  "THIRD_PARTY_NOTICES.txt",
  "engine-wasm-worker-v1.json",
  "engine-wasm.authority.json",
  "engine-wasm.cdx.json",
  "layerdraw-engine.wasm",
  "licenses/Apache-2.0.txt",
  "wasm_exec.js",
]);
const expectedArtifactMediaTypes: Readonly<Record<string, string>> = Object.freeze({
  "LICENSE": "text/plain; charset=utf-8",
  "LICENSING.md": "text/markdown; charset=utf-8",
  "NOTICE": "text/plain; charset=utf-8",
  "THIRD_PARTY_NOTICES.txt": "text/plain; charset=utf-8",
  "engine-wasm-worker-v1.json": "application/json",
  "engine-wasm.authority.json": "application/json",
  "engine-wasm.cdx.json": "application/json",
  "layerdraw-engine.wasm": "application/wasm",
  "licenses/Apache-2.0.txt": "text/plain; charset=utf-8",
  "wasm_exec.js": "text/javascript",
});
const expectedCompilerLimits: Readonly<Record<string, number>> = Object.freeze({
  max_project_source_files: 512,
  max_project_source_bytes: 16_777_216,
  max_pack_files: 1_024,
  max_pack_bytes: 33_554_432,
  max_assets: 256,
  max_asset_bytes: 16_777_216,
  max_raster_dimension: 8_192,
  max_raster_pixels: 16_777_216,
  max_declarations: 250_000,
});
const requiredBrowserPrimitives = Object.freeze([
  "Blob",
  "TextDecoder",
  "TextEncoder",
  "URL.createObjectURL",
  "URL.revokeObjectURL",
  "WebAssembly",
  "crypto.getRandomValues",
  "crypto.subtle.digest",
  "dedicated_module_worker",
  "fetch",
  "performance.now",
  "structuredClone",
  "transferable_fixed_ArrayBuffer",
]);

export interface VerifiedArtifactLoaderOptions {
  readonly artifactBaseURL: string;
  readonly packageManifestURL: string;
  readonly loadBytes?: (url: string) => Promise<ArrayBuffer>;
}

interface ArtifactFile {
  readonly path: string;
  readonly media_type: string;
  readonly size: number;
  readonly digest: string;
}

interface WasmBridge {
  readonly workerProtocol: string;
  readonly workerProtocolVersion: number;
  initialize(endpointGeneration: string, releaseManifestDigest: string): unknown;
  request(endpointGeneration: string, control: ArrayBuffer, blobIDs: string[], blobs: ArrayBuffer[]): unknown;
  dispose(endpointGeneration: string): unknown;
}

interface GoRuntime {
  readonly importObject: WebAssembly.Imports;
  run(instance: WebAssembly.Instance): Promise<void>;
}

interface GoRuntimeConstructor {
  new(): GoRuntime;
}

function initializationFailure(): never {
  throw new EngineEndpointInitializationError("engine.worker.initialization_failed");
}

function artifactMismatch(): never {
  throw new EngineEndpointInitializationError("engine.worker.artifact_mismatch");
}

function snapshotBytes(value: unknown): ArrayBuffer {
  if (!isFixedArrayBuffer(value)) initializationFailure();
  const snapshot = value.slice(0);
  if (!isFixedArrayBuffer(snapshot)) initializationFailure();
  return snapshot;
}

async function defaultLoadBytes(url: string): Promise<ArrayBuffer> {
  const fetchValue = Reflect.get(globalThis, "fetch");
  if (typeof fetchValue !== "function") initializationFailure();
  const response = await Reflect.apply(fetchValue, globalThis, [url]) as {ok?: unknown; arrayBuffer?: unknown};
  if (response.ok !== true || typeof response.arrayBuffer !== "function") initializationFailure();
  const value = await Reflect.apply(response.arrayBuffer, response, []) as unknown;
  if (!isFixedArrayBuffer(value)) initializationFailure();
  return value;
}

async function sha256(value: ArrayBuffer): Promise<string> {
  const cryptoValue = Reflect.get(globalThis, "crypto") as {subtle?: {digest?: unknown}} | undefined;
  const subtle = cryptoValue?.subtle;
  const digestFunction = subtle?.digest;
  if (typeof digestFunction !== "function") initializationFailure();
  const raw = await Reflect.apply(digestFunction, subtle, ["SHA-256", value]) as unknown;
  if (!isFixedArrayBuffer(raw)) initializationFailure();
  const hex = [...new Uint8Array(raw)].map((byte) => byte.toString(16).padStart(2, "0")).join("");
  return `sha256:${hex}`;
}

function decodeJSON(value: ArrayBuffer): unknown {
  const decoder = Reflect.get(globalThis, "TextDecoder");
  if (typeof decoder !== "function") initializationFailure();
  const instance = Reflect.construct(decoder, ["utf-8", {fatal: true}]);
  const text = Reflect.apply(Reflect.get(instance, "decode"), instance, [new Uint8Array(value)]);
  if (typeof text !== "string") initializationFailure();
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return initializationFailure();
  }
}

const base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

function base64(value: ArrayBuffer): string {
  const bytes = new Uint8Array(value);
  let encoded = "";
  for (let offset = 0; offset < bytes.length; offset += 3) {
    const first = bytes[offset];
    if (first === undefined) initializationFailure();
    const second = bytes[offset + 1];
    const third = bytes[offset + 2];
    encoded += base64Alphabet.charAt(first >>> 2);
    encoded += base64Alphabet.charAt(((first & 0x03) << 4) | ((second ?? 0) >>> 4));
    encoded += second === undefined ? "=" : base64Alphabet.charAt(((second & 0x0f) << 2) | ((third ?? 0) >>> 6));
    encoded += third === undefined ? "=" : base64Alphabet.charAt(third & 0x3f);
  }
  return encoded;
}

interface VerifiedModuleResource {
  readonly url: string;
  release(): void;
}

function createVerifiedModuleResource(value: ArrayBuffer): VerifiedModuleResource {
  if (Reflect.get(globalThis, "self") === globalThis) {
    const BlobValue = Reflect.get(globalThis, "Blob") as typeof Blob | undefined;
    const URLValue = Reflect.get(globalThis, "URL") as typeof URL | undefined;
    if (typeof BlobValue !== "function" || typeof URLValue?.createObjectURL !== "function" || typeof URLValue.revokeObjectURL !== "function") {
      initializationFailure();
    }
    const blob = new BlobValue([value], {type: "text/javascript"});
    const url = URLValue.createObjectURL(blob);
    if (typeof url !== "string" || url.length === 0) initializationFailure();
    let released = false;
    return Object.freeze({
      url,
      release(): void {
        if (released) return;
        released = true;
        URLValue.revokeObjectURL(url);
      },
    });
  }
  const url = `data:text/javascript;base64,${base64(value)}`;
  return Object.freeze({url, release(): void { /* Data URLs have no external resource handle. */ }});
}

async function executeVerifiedJavaScript(value: ArrayBuffer): Promise<void> {
  const resource = createVerifiedModuleResource(value);
  try {
    await import(resource.url);
  } finally {
    resource.release();
  }
}

function isSafeArtifactPath(value: unknown): value is string {
  return typeof value === "string" && /^(?:[A-Za-z0-9._-]+\/)*[A-Za-z0-9._-]+$/.test(value) &&
    !value.split("/").some((part) => part === "." || part === "..");
}

function validateArtifactFiles(value: unknown): readonly ArtifactFile[] {
  if (!isArrayRecord(value) || value.length !== requiredArtifactFiles.length) artifactMismatch();
  const result: ArtifactFile[] = [];
  const seen = new Set<string>();
  for (const candidate of value) {
    const path = isPlainRecord(candidate) && typeof candidate.path === "string" ? candidate.path : "";
    const expectedMediaType = expectedArtifactMediaTypes[path];
    if (!isPlainRecord(candidate) || !hasExactKeys(candidate, ["path", "media_type", "size", "digest"]) ||
        !isSafeArtifactPath(candidate.path) || expectedMediaType === undefined || candidate.media_type !== expectedMediaType ||
        typeof candidate.size !== "number" || !Number.isSafeInteger(candidate.size) || candidate.size < 0 || !isDigest(candidate.digest) || seen.has(candidate.path)) artifactMismatch();
    seen.add(candidate.path);
    result.push(candidate as unknown as ArtifactFile);
  }
  const sorted = [...seen].sort();
  if (JSON.stringify(sorted) !== JSON.stringify([...requiredArtifactFiles].sort())) artifactMismatch();
  return result;
}

function exactStringArray(value: unknown, expected: readonly string[]): boolean {
  return isArrayRecord(value) && value.length === expected.length && expected.every((item, index) => value[index] === item);
}

function isReleaseVersion(value: unknown): value is string {
  return typeof value === "string" && value.length <= 255 &&
    /^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/.test(value);
}

interface ValidatedManifest {
  readonly files: readonly ArtifactFile[];
  readonly releaseVersion: string;
  readonly sourceRevision: string;
  readonly goVersion: string;
  readonly protocolSchemaDigest: string;
  readonly runtimeSupport: Readonly<Record<string, unknown>>;
  readonly licenses: Readonly<Record<string, unknown>>;
  readonly sbomAuthority: ValidatedSBOMAuthorityDescriptor;
}

interface ValidatedSBOMAuthorityDescriptor {
  readonly file: string;
  readonly digest: string;
}

interface GeneratedModuleBuildInfo {
  readonly path: string;
  readonly version: string;
}

interface GeneratedSBOMAuthority {
  readonly goVersion: string;
  readonly manifestRuntimeSupport: Readonly<Record<string, unknown>>;
  readonly manifestLicenses: Readonly<Record<string, unknown>>;
  readonly moduleBuildInfo: readonly GeneratedModuleBuildInfo[];
  readonly sbomComponents: readonly unknown[];
  readonly sbomRootLicenses: readonly unknown[];
  readonly sbomRootDependsOn: readonly unknown[];
  readonly sbomLeafDependencies: readonly unknown[];
}

function validateContractCorpus(value: unknown): EngineWorkerTransportLimits {
  const keys = ["spdx_license_identifier", "worker_protocol", "worker_protocol_version", "transport_id", "identifier_limits", "transport_limits", "failure_definitions", "outer_messages"];
  if (!isPlainRecord(value) || !hasExactKeys(value, keys) || value.spdx_license_identifier !== "Apache-2.0" ||
      value.worker_protocol !== workerProtocol || value.worker_protocol_version !== workerProtocolVersion || value.transport_id !== "wasm_worker") artifactMismatch();
  const identifiers = value.identifier_limits;
  if (!isPlainRecord(identifiers) || !hasExactKeys(identifiers, ["endpoint_generation_utf8_bytes", "exchange_id_utf8_bytes", "blob_id_utf8_bytes"]) ||
      identifiers.endpoint_generation_utf8_bytes !== 128 || identifiers.exchange_id_utf8_bytes !== 128 || identifiers.blob_id_utf8_bytes !== 256) artifactMismatch();
  if (!hasInitialTransportLimits(value.transport_limits)) artifactMismatch();
  if (!isArrayRecord(value.failure_definitions) || value.failure_definitions.length !== Object.keys(failureDefinitions).length) artifactMismatch();
  const failureCodes = Object.keys(failureDefinitions);
  for (let index = 0; index < failureCodes.length; index += 1) {
    const code = failureCodes[index];
    const entry = value.failure_definitions[index];
    if (code === undefined || !isPlainRecord(entry) || !hasExactKeys(entry, ["code", "phase", "retryable"]) || entry.code !== code || validateFailure(entry) === undefined) artifactMismatch();
  }
  const outer = value.outer_messages;
  if (!isPlainRecord(outer) || !hasExactKeys(outer, ["host_to_worker", "worker_to_host"]) || !isPlainRecord(outer.host_to_worker) || !isPlainRecord(outer.worker_to_host)) artifactMismatch();
  const host = outer.host_to_worker;
  const worker = outer.worker_to_host;
  if (!hasExactKeys(host, ["init", "request", "dispose"]) || !hasExactKeys(worker, ["ready", "accepted", "response", "transport_failure"]) ||
      !exactStringArray(host.init, ["worker_protocol", "worker_protocol_version", "kind", "endpoint_generation", "expected_artifact_manifest_digest", "release_manifest_digest"]) ||
      !exactStringArray(host.request, ["worker_protocol", "worker_protocol_version", "kind", "endpoint_generation", "exchange_id", "control", "blobs"]) ||
      !exactStringArray(host.dispose, ["worker_protocol", "worker_protocol_version", "kind", "endpoint_generation"]) ||
      !exactStringArray(worker.ready, ["worker_protocol", "worker_protocol_version", "kind", "endpoint_generation", "artifact_manifest_digest", "transport_limits"]) ||
      !exactStringArray(worker.accepted, ["worker_protocol", "worker_protocol_version", "kind", "endpoint_generation", "exchange_id"]) ||
      !exactStringArray(worker.response, ["worker_protocol", "worker_protocol_version", "kind", "endpoint_generation", "exchange_id", "control", "blobs"]) ||
      !exactStringArray(worker.transport_failure, ["worker_protocol", "worker_protocol_version", "kind", "endpoint_generation?", "exchange_id?", "failure"])) artifactMismatch();
  return value.transport_limits;
}

function validateManifest(value: unknown, expectedReleaseVersion: string): ValidatedManifest {
  const keys = ["artifact_id", "artifact_manifest_version", "build", "protocol", "runtime_support", "sbom_authority", "files", "transport", "compiler_limits", "browser_contract", "licenses"];
  if (!isPlainRecord(value) || !hasExactKeys(value, keys) || value.artifact_id !== "@layerdraw/engine-wasm" || value.artifact_manifest_version !== 1) artifactMismatch();
  const build = value.build;
  if (!isPlainRecord(build) || !hasExactKeys(build, ["cgo_enabled", "go_version", "goexperiment", "goos_goarch", "main_package", "source_revision", "release_version", "flags"]) ||
      build.cgo_enabled !== false || typeof build.go_version !== "string" || build.go_version.length === 0 || build.goexperiment !== "" || build.goos_goarch !== "js/wasm" ||
      build.main_package !== "./cmd/layerdraw-engine" || typeof build.source_revision !== "string" || !/^[0-9a-f]{40}$/.test(build.source_revision) ||
      build.release_version !== expectedReleaseVersion) artifactMismatch();
  const protocol = value.protocol;
  if (!isPlainRecord(protocol) || !hasExactKeys(protocol, ["name", "version", "schema_digest"]) || protocol.name !== "engine" ||
      protocol.version !== "1.0" || !isDigest(protocol.schema_digest)) artifactMismatch();
  const runtime = value.runtime_support;
  if (!isPlainRecord(runtime) || !hasExactKeys(runtime, ["file", "go_version", "digest"]) ||
      typeof runtime.file !== "string" || runtime.file.length === 0 || typeof runtime.go_version !== "string" || runtime.go_version.length === 0 || !isDigest(runtime.digest)) artifactMismatch();
  const transport = value.transport;
  if (!isPlainRecord(transport) || !hasExactKeys(transport, [
    "id", "worker_protocol", "worker_protocol_version", "contract_file", "endpoint_instance_id_provenance",
    "release_manifest_digest_provenance", "single_flight", "transfer", ...transportLimitKeys,
  ]) || transport.id !== "wasm_worker" || transport.worker_protocol !== workerProtocol ||
      transport.worker_protocol_version !== workerProtocolVersion || transport.contract_file !== "engine-wasm-worker-v1.json" ||
      transport.endpoint_instance_id_provenance !== "runtime_crypto_rand" || transport.release_manifest_digest_provenance !== "verified_worker_input" ||
      transport.single_flight !== true || transport.transfer !== "array_buffer") artifactMismatch();
  for (const key of transportLimitKeys) if (transport[key] !== browserTransportLimits[key]) artifactMismatch();
  const compiler = value.compiler_limits;
  const compilerKeys = Object.keys(expectedCompilerLimits);
  if (!isPlainRecord(compiler) || !hasExactKeys(compiler, compilerKeys)) artifactMismatch();
  for (const key of compilerKeys) if (compiler[key] !== expectedCompilerLimits[key]) artifactMismatch();
  const browser = value.browser_contract;
  if (!isPlainRecord(browser) || !hasExactKeys(browser, ["module_dedicated_worker", "shared_array_buffer", "wasm_threads", "required_primitives"]) ||
      browser.module_dedicated_worker !== true || browser.shared_array_buffer !== false || browser.wasm_threads !== false ||
      !exactStringArray(browser.required_primitives, requiredBrowserPrimitives)) artifactMismatch();
  const licenses = value.licenses;
  if (!isPlainRecord(licenses) || !hasExactKeys(licenses, ["product", "runtime_support", "sbom"]) ||
      typeof licenses.product !== "string" || licenses.product.length === 0 || typeof licenses.runtime_support !== "string" || licenses.runtime_support.length === 0 ||
      typeof licenses.sbom !== "string" || licenses.sbom.length === 0) artifactMismatch();
  const sbomAuthority = value.sbom_authority;
  if (!isPlainRecord(sbomAuthority) || !hasExactKeys(sbomAuthority, ["file", "digest"]) ||
      sbomAuthority.file !== "engine-wasm.authority.json" || !isDigest(sbomAuthority.digest)) artifactMismatch();
  if (!exactStringArray(build.flags, [
    "-trimpath",
    "-buildvcs=false",
    `-ldflags=-buildid= -s -w -X main.releaseVersion=${build.release_version} -X main.sourceRevision=${build.source_revision} -X main.sbomAuthorityDigest=${sbomAuthority.digest}`,
  ])) artifactMismatch();
  return Object.freeze({
    files: validateArtifactFiles(value.files),
    releaseVersion: build.release_version,
    sourceRevision: build.source_revision,
    goVersion: build.go_version,
    protocolSchemaDigest: protocol.schema_digest,
    runtimeSupport: runtime,
    licenses,
    sbomAuthority: sbomAuthority as unknown as ValidatedSBOMAuthorityDescriptor,
  });
}

function validateGeneratedSBOMAuthority(value: unknown): GeneratedSBOMAuthority {
  const keys = ["authority_version", "go_version", "manifest_runtime_support", "manifest_licenses", "module_build_info", "sbom_components", "sbom_root_licenses", "sbom_root_depends_on", "sbom_leaf_dependencies"];
  if (!isPlainRecord(value) || !hasExactKeys(value, keys) || value.authority_version !== 1 || typeof value.go_version !== "string" || value.go_version.length === 0 ||
      !isPlainRecord(value.manifest_runtime_support) || !hasExactKeys(value.manifest_runtime_support, ["file", "go_version", "digest"]) ||
      typeof value.manifest_runtime_support.file !== "string" || typeof value.manifest_runtime_support.go_version !== "string" || !isDigest(value.manifest_runtime_support.digest) ||
      !isPlainRecord(value.manifest_licenses) || !hasExactKeys(value.manifest_licenses, ["product", "runtime_support", "sbom"]) ||
      typeof value.manifest_licenses.product !== "string" || typeof value.manifest_licenses.runtime_support !== "string" || typeof value.manifest_licenses.sbom !== "string" ||
      !isArrayRecord(value.module_build_info) || !isArrayRecord(value.sbom_components) || !isArrayRecord(value.sbom_root_licenses) ||
      !isArrayRecord(value.sbom_root_depends_on) || !isArrayRecord(value.sbom_leaf_dependencies)) artifactMismatch();
  for (const module of value.module_build_info) {
    if (!isPlainRecord(module) || !hasExactKeys(module, ["path", "version"]) || typeof module.path !== "string" || module.path.length === 0 ||
        typeof module.version !== "string" || module.version.length === 0) artifactMismatch();
  }
  if (!value.sbom_components.every(isPlainRecord) || !value.sbom_root_licenses.every(isPlainRecord) ||
      !value.sbom_root_depends_on.every((item) => typeof item === "string" && item.length > 0)) artifactMismatch();
  for (const dependency of value.sbom_leaf_dependencies) {
    if (!isPlainRecord(dependency) || !hasExactKeys(dependency, ["ref", "dependsOn"]) || typeof dependency.ref !== "string" || dependency.ref.length === 0 ||
        !isArrayRecord(dependency.dependsOn) || !dependency.dependsOn.every((item) => typeof item === "string" && item.length > 0)) artifactMismatch();
  }
  return Object.freeze({
    goVersion: value.go_version,
    manifestRuntimeSupport: value.manifest_runtime_support,
    manifestLicenses: value.manifest_licenses,
    moduleBuildInfo: Object.freeze(value.module_build_info as unknown as GeneratedModuleBuildInfo[]),
    sbomComponents: Object.freeze(value.sbom_components),
    sbomRootLicenses: Object.freeze(value.sbom_root_licenses),
    sbomRootDependsOn: Object.freeze(value.sbom_root_depends_on),
    sbomLeafDependencies: Object.freeze(value.sbom_leaf_dependencies),
  });
}

function exactJSONEqual(left: unknown, right: unknown): boolean {
  if (left === null || right === null || typeof left !== "object" || typeof right !== "object") return Object.is(left, right);
  if (Array.isArray(left) || Array.isArray(right)) {
    return Array.isArray(left) && Array.isArray(right) && left.length === right.length && left.every((item, index) => exactJSONEqual(item, right[index]));
  }
  if (!isPlainRecord(left) || !isPlainRecord(right)) return false;
  const leftKeys = Object.keys(left).sort();
  const rightKeys = Object.keys(right).sort();
  return exactStringArray(leftKeys, rightKeys) && leftKeys.every((key) => exactJSONEqual(left[key], right[key]));
}

function validateSBOM(value: unknown, releaseVersion: string, wasmDigest: string, authority: GeneratedSBOMAuthority): void {
  if (!isPlainRecord(value) || !hasExactKeys(value, ["$schema", "bomFormat", "specVersion", "version", "metadata", "components", "dependencies"]) ||
      value.$schema !== "http://cyclonedx.org/schema/bom-1.6.schema.json" || value.bomFormat !== "CycloneDX" || value.specVersion !== "1.6" ||
      value.version !== 1 || !isArrayRecord(value.components) || !isArrayRecord(value.dependencies)) artifactMismatch();
  const metadata = value.metadata;
  if (!isPlainRecord(metadata) || !hasExactKeys(metadata, ["component"])) artifactMismatch();
  const component = metadata.component;
  const rootRef = `pkg:npm/%40layerdraw/engine-wasm@${releaseVersion}`;
  if (!isPlainRecord(component) || !hasExactKeys(component, ["type", "name", "version", "purl", "bom-ref", "hashes", "licenses"]) ||
      component.type !== "application" || component.name !== "@layerdraw/engine-wasm" || component.version !== releaseVersion ||
      component.purl !== rootRef || component["bom-ref"] !== rootRef || !isArrayRecord(component.hashes) || component.hashes.length !== 1 ||
      !isPlainRecord(component.hashes[0]) || !hasExactKeys(component.hashes[0], ["alg", "content"]) || component.hashes[0].alg !== "SHA-256" ||
      component.hashes[0].content !== wasmDigest.slice("sha256:".length) || !exactJSONEqual(component.licenses, authority.sbomRootLicenses) ||
      !exactJSONEqual(value.components, authority.sbomComponents) || value.dependencies.length !== authority.sbomLeafDependencies.length + 1) artifactMismatch();
  const rootDependency = value.dependencies[0];
  if (!isPlainRecord(rootDependency) || !hasExactKeys(rootDependency, ["ref", "dependsOn"]) || rootDependency.ref !== rootRef ||
      !exactJSONEqual(rootDependency.dependsOn, authority.sbomRootDependsOn) || !exactJSONEqual(value.dependencies.slice(1), authority.sbomLeafDependencies)) artifactMismatch();
}

function validatePackageReleaseAuthority(value: unknown): string {
  if (!isPlainRecord(value) || value.name !== "@layerdraw/engine-wasm" || !isReleaseVersion(value.version)) initializationFailure();
  return value.version;
}

function validateBridge(value: unknown): WasmBridge {
  if (!isPlainRecord(value) || value.workerProtocol !== workerProtocol || value.workerProtocolVersion !== workerProtocolVersion ||
      typeof value.initialize !== "function" || typeof value.request !== "function" || typeof value.dispose !== "function") initializationFailure();
  return value as unknown as WasmBridge;
}

function validateInitialized(
  value: unknown,
  generation: string,
  manifest: ValidatedManifest,
  generatedAuthority: GeneratedSBOMAuthority,
): EngineWorkerTransportLimits {
  if (!isPlainRecord(value) || !hasExactKeys(value, ["ok", "endpoint_generation", "engine_release", "source_revision", "protocol_schema_digest", "go_version", "module_build_info", "sbom_authority_digest", "transport_limits"]) ||
      value.ok !== true || value.endpoint_generation !== generation || value.engine_release !== manifest.releaseVersion ||
      value.source_revision !== manifest.sourceRevision || value.protocol_schema_digest !== manifest.protocolSchemaDigest ||
      value.go_version !== manifest.goVersion || value.go_version !== generatedAuthority.goVersion ||
      value.sbom_authority_digest !== manifest.sbomAuthority.digest || !hasInitialTransportLimits(value.transport_limits) ||
      !exactJSONEqual(value.module_build_info, generatedAuthority.moduleBuildInfo)) initializationFailure();
  return value.transport_limits;
}

function validateBridgeResult(value: unknown, generation: string, limits: EngineWorkerTransportLimits): EngineByteEndpointResult {
  const crashed = (): EngineByteEndpointResult => Object.freeze({ok: false, failure: failure("engine.worker.crashed")});
  if (!isPlainRecord(value) || typeof value.ok !== "boolean") return crashed();
  if (!value.ok) {
    if (!hasExactKeys(value, ["ok", "failure"])) return crashed();
    const validated = validateFailure(value.failure);
    return validated === undefined ? crashed() : Object.freeze({ok: false, failure: validated});
  }
  if (!hasExactKeys(value, ["ok", "endpoint_generation", "control", "blob_ids", "blobs"]) || value.endpoint_generation !== generation ||
      !isFixedArrayBuffer(value.control) || !isArrayRecord(value.blob_ids) || !isArrayRecord(value.blobs) || value.blob_ids.length !== value.blobs.length) {
    return crashed();
  }
  const blobs: EngineWorkerBlob[] = [];
  for (let index = 0; index < value.blob_ids.length; index += 1) {
    const blobID = value.blob_ids[index];
    const bytes = value.blobs[index];
    if (!isBoundedOpaqueString(blobID, limits.max_blob_id_bytes) || !isFixedArrayBuffer(bytes)) {
      return crashed();
    }
    blobs.push(Object.freeze({blob_id: blobID, bytes}));
  }
  const response = {control: value.control, blobs: Object.freeze(blobs)};
  const validatedResponse = validateEndpointResponse(response, limits);
  return validatedResponse === undefined ? crashed() :
    Object.freeze({ok: true, response: validatedResponse});
}

async function waitForBridge(): Promise<WasmBridge> {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    const value = Reflect.get(globalThis, "__layerdrawEngineWasmV1");
    if (value !== undefined) return validateBridge(value);
    await new Promise<void>((resolve) => {
      const setTimeoutValue = Reflect.get(globalThis, "setTimeout");
      if (typeof setTimeoutValue !== "function") initializationFailure();
      Reflect.apply(setTimeoutValue, globalThis, [resolve, 0]);
    });
  }
  return initializationFailure();
}

export async function createVerifiedWasmEndpoint(init: EngineByteEndpointInit, options: VerifiedArtifactLoaderOptions): Promise<EngineByteEndpoint> {
  if (!isBoundedOpaqueString(init.endpointGeneration, maxEndpointGenerationBytes) || !isDigest(init.expectedArtifactManifestDigest) ||
      !isDigest(init.releaseManifestDigest) || typeof options.artifactBaseURL !== "string" || options.artifactBaseURL.length === 0 ||
      typeof options.packageManifestURL !== "string" || options.packageManifestURL.length === 0) initializationFailure();
  const loadBytes = options.loadBytes ?? defaultLoadBytes;
  const resolveURL = (path: string): string => new URL(path, options.artifactBaseURL).href;
  let expectedReleaseVersion: string;
  try {
    expectedReleaseVersion = validatePackageReleaseAuthority(decodeJSON(snapshotBytes(await loadBytes(options.packageManifestURL))));
  } catch {
    return initializationFailure();
  }
  let manifestBytes: ArrayBuffer;
  try {
    manifestBytes = snapshotBytes(await loadBytes(resolveURL("engine-wasm.manifest.json")));
  } catch {
    return initializationFailure();
  }
  if (await sha256(manifestBytes) !== init.expectedArtifactManifestDigest) artifactMismatch();
  const manifest = validateManifest(decodeJSON(manifestBytes), expectedReleaseVersion);
  const files = manifest.files;
  const loaded = new Map<string, ArrayBuffer>();
  for (const file of files) {
    let bytes: ArrayBuffer;
    try {
      bytes = snapshotBytes(await loadBytes(resolveURL(file.path)));
    } catch {
      return artifactMismatch();
    }
    if (bytes.byteLength !== file.size || await sha256(bytes) !== file.digest) artifactMismatch();
    loaded.set(file.path, bytes);
  }
  const corpus = loaded.get("engine-wasm-worker-v1.json");
  const authorityBytes = loaded.get(manifest.sbomAuthority.file);
  const sbom = loaded.get("engine-wasm.cdx.json");
  const wasm = loaded.get("layerdraw-engine.wasm");
  const wasmExec = loaded.get("wasm_exec.js");
  const wasmFile = files.find((file) => file.path === "layerdraw-engine.wasm");
  if (corpus === undefined || authorityBytes === undefined || sbom === undefined || wasm === undefined || wasmExec === undefined || wasmFile === undefined ||
      await sha256(authorityBytes) !== manifest.sbomAuthority.digest || await sha256(wasmExec) !== manifest.runtimeSupport.digest) artifactMismatch();
  const generatedAuthority = validateGeneratedSBOMAuthority(decodeJSON(authorityBytes));
  if (generatedAuthority.goVersion !== manifest.goVersion || !exactJSONEqual(generatedAuthority.manifestRuntimeSupport, manifest.runtimeSupport) ||
      !exactJSONEqual(generatedAuthority.manifestLicenses, manifest.licenses)) artifactMismatch();
  validateSBOM(decodeJSON(sbom), manifest.releaseVersion, wasmFile.digest, generatedAuthority);
  const limits = validateContractCorpus(decodeJSON(corpus));
  try {
    await executeVerifiedJavaScript(wasmExec);
    const GoValue = Reflect.get(globalThis, "Go") as GoRuntimeConstructor | undefined;
    if (typeof GoValue !== "function") initializationFailure();
    const go = new GoValue();
    const instantiated = await WebAssembly.instantiate(new Uint8Array(wasm), go.importObject);
    void go.run(instantiated.instance);
    const bridge = await waitForBridge();
    const initialized = bridge.initialize(init.endpointGeneration, init.releaseManifestDigest);
    const bridgeLimits = validateInitialized(initialized, init.endpointGeneration, manifest, generatedAuthority);
    if (!transportLimitKeys.every((key) => bridgeLimits[key] === limits[key])) initializationFailure();
    let disposed = false;
    const endpoint: EngineByteEndpoint = {
      artifactManifestDigest: init.expectedArtifactManifestDigest,
      transportLimits: bridgeLimits,
      request(input): EngineByteEndpointResult {
        if (disposed) return Object.freeze({ok: false, failure: failure("engine.worker.disposed")});
        const blobIDs = input.blobs.map((blob) => blob.blob_id);
        const blobBuffers = input.blobs.map((blob) => blob.bytes);
        return validateBridgeResult(bridge.request(init.endpointGeneration, input.control, blobIDs, blobBuffers), init.endpointGeneration, bridgeLimits);
      },
      dispose(): void {
        if (disposed) return;
        disposed = true;
        const result = bridge.dispose(init.endpointGeneration);
        if (!isPlainRecord(result) || !hasExactKeys(result, ["ok"]) || result.ok !== true) initializationFailure();
      },
    };
    return Object.freeze(endpoint);
  } catch (error) {
    if (error instanceof EngineEndpointInitializationError) throw error;
    return initializationFailure();
  }
}
