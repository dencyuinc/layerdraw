// SPDX-License-Identifier: Apache-2.0

import assert from "node:assert/strict";
import {readFile} from "node:fs/promises";
import test from "node:test";

import Ajv2020 from "ajv/dist/2020.js";

const schemaRoot = new URL("../../../schemas/", import.meta.url);

async function readJSON(path) {
  return JSON.parse(await readFile(new URL(path, schemaRoot), "utf8"));
}

function protocolVersion(value) {
  const match = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/.exec(value);
  if (match === null) return undefined;
  const version = [Number(match[1]), Number(match[2])];
  return version.every((part) => Number.isSafeInteger(part) && part <= 0xffffffff) ? version : undefined;
}

function compareVersion(left, right) {
  return left[0] === right[0] ? left[1] - right[1] : left[0] - right[0];
}

function protocolRange(value) {
  const parts = value.split("..");
  if (parts.length !== 2) return undefined;
  const lower = protocolVersion(parts[0]);
  const upper = protocolVersion(parts[1]);
  return lower !== undefined && upper !== undefined && lower[0] === upper[0] && compareVersion(lower, upper) <= 0 ? [lower, upper] : undefined;
}

function canonicalInteger(value, minimum, maximum, pattern) {
  if (!pattern.test(value)) return false;
  try { const parsed = BigInt(value); return parsed >= minimum && parsed <= maximum; } catch { return false; }
}

function canonicalBinary64(value, positive) {
  if (!/^-?(0|[1-9][0-9]*)(?:\.[0-9]+)?(?:e[+-][1-9][0-9]*)?$/.test(value)) return false;
  const parsed = Number(value);
  return Number.isFinite(parsed) && !Object.is(parsed, -0) && (!positive || parsed > 0) && String(parsed) === value;
}

function canonicalSourcePath(value) {
  return value !== "" && !value.startsWith("/") && !value.includes("\\") && !value.includes("\0") &&
    value.split("/").every((segment) => segment !== "" && segment !== "." && segment !== "..");
}

function stableAddressTuple(value) {
  const parts = value.split(":");
  if (parts.length < 3 || parts[0] !== "ldl") return undefined;
  let origin;
  let components;
  let pathStart;
  if (parts[1] === "project") {
    origin = 0; components = [parts[2]]; pathStart = 3;
  } else if (parts[1] === "pack" && parts.length >= 4) {
    origin = 1; components = [parts[2], parts[3]]; pathStart = 4;
  } else return undefined;
  if ((parts.length - pathStart) % 2 !== 0) return undefined;
  const path = [];
  for (let index = pathStart; index < parts.length; index += 2) path.push([parts[index], parts[index + 1]]);
  return {origin, components, path};
}

function compareStableAddresses(left, right) {
  const leftTuple = stableAddressTuple(left);
  const rightTuple = stableAddressTuple(right);
  if (leftTuple === undefined || rightTuple === undefined) return undefined;
  if (leftTuple.origin !== rightTuple.origin) return leftTuple.origin - rightTuple.origin;
  for (let index = 0; index < Math.min(leftTuple.components.length, rightTuple.components.length); index++) {
    if (leftTuple.components[index] !== rightTuple.components[index]) return leftTuple.components[index] < rightTuple.components[index] ? -1 : 1;
  }
  if (leftTuple.components.length !== rightTuple.components.length) return leftTuple.components.length - rightTuple.components.length;
  if (leftTuple.path.length !== rightTuple.path.length) return leftTuple.path.length - rightTuple.path.length;
  const ranks = new Map([["entity-type", 0], ["relation-type", 1], ["layer", 2], ["entity", 3], ["relation", 4], ["query", 5], ["view", 6], ["reference", 7], ["column", 8], ["constraint", 9], ["row", 10], ["parameter", 11], ["table-column", 12], ["export", 13]]);
  for (let index = 0; index < leftTuple.path.length; index++) {
    const leftSegment = leftTuple.path[index];
    const rightSegment = rightTuple.path[index];
    const kind = ranks.get(leftSegment[0]) - ranks.get(rightSegment[0]);
    if (kind !== 0) return kind;
    if (leftSegment[1] !== rightSegment[1]) return leftSegment[1] < rightSegment[1] ? -1 : 1;
  }
  return 0;
}

function realUTCDateTime(value) {
  const match = /^([0-9]{4})-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](?:\.[0-9]{1,9})?Z$/.exec(value);
  if (match === null) return false;
  const year = Number(match[1]); const month = Number(match[2]); const day = Number(match[3]);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  return day <= [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][month - 1];
}

function addLayerDrawFormats(ajv) {
  const signed = /^(0|-[1-9][0-9]*|[1-9][0-9]*)$/;
  const unsigned = /^(0|[1-9][0-9]*)$/;
  const positive = /^[1-9][0-9]*$/;
  ajv.addFormat("int64-decimal", {type: "string", validate: (value) => canonicalInteger(value, -(2n ** 63n), (2n ** 63n) - 1n, signed)});
  ajv.addFormat("positive-int64-decimal", {type: "string", validate: (value) => canonicalInteger(value, 1n, (2n ** 63n) - 1n, positive)});
  ajv.addFormat("nonnegative-int64-decimal", {type: "string", validate: (value) => canonicalInteger(value, 0n, (2n ** 63n) - 1n, unsigned)});
  ajv.addFormat("uint64-decimal", {type: "string", validate: (value) => canonicalInteger(value, 0n, (2n ** 64n) - 1n, unsigned)});
  ajv.addFormat("safe-integer-decimal", {type: "string", validate: (value) => canonicalInteger(value, -(2n ** 53n) + 1n, (2n ** 53n) - 1n, signed)});
  ajv.addFormat("positive-safe-integer-decimal", {type: "string", validate: (value) => canonicalInteger(value, 1n, (2n ** 53n) - 1n, positive)});
  ajv.addFormat("nonnegative-safe-integer-decimal", {type: "string", validate: (value) => canonicalInteger(value, 0n, (2n ** 53n) - 1n, unsigned)});
  ajv.addFormat("finite-binary64-decimal", {type: "string", validate: (value) => canonicalBinary64(value, false)});
  ajv.addFormat("positive-finite-binary64-decimal", {type: "string", validate: (value) => canonicalBinary64(value, true)});
  ajv.addFormat("protocol-version", {type: "string", validate: (value) => protocolVersion(value) !== undefined});
  ajv.addFormat("protocol-version-range", {type: "string", validate: (value) => protocolRange(value) !== undefined});
  ajv.addFormat("protocol-version-or-range", {type: "string", validate: (value) => protocolVersion(value) !== undefined || protocolRange(value) !== undefined});
  ajv.addFormat("canonical-source-path", {type: "string", validate: canonicalSourcePath});
  ajv.addFormat("date-time", {type: "string", validate: realUTCDateTime});
}

function addLayerDrawVocabulary(ajv) {
  ajv.addKeyword({keyword: "x-layerdraw-go-package", schemaType: "string", errors: false, validate: () => true});
  ajv.addKeyword({keyword: "x-layerdraw-max-json-bytes", schemaType: "number", errors: false, validate: () => true});
  ajv.addKeyword({keyword: "x-layerdraw-max-json-depth", schemaType: "number", errors: false, validate: () => true});
  ajv.addKeyword({keyword: "x-layerdraw-ts-module", schemaType: "string", errors: false, validate: () => true});
  ajv.addKeyword({keyword: "x-layerdraw-stable-address-order", schemaType: "string", type: "array", errors: false, validate(selector, data) {
    const address = (item) => selector === "$item" ? item : item?.[selector];
    for (let index = 1; index < data.length; index++) {
      const left = address(data[index - 1]);
      const right = address(data[index]);
      if (typeof left !== "string" || typeof right !== "string" || compareStableAddresses(left, right) >= 0) return false;
    }
    return true;
  }});
  ajv.addKeyword({keyword: "x-layerdraw-disjoint-arrays", schemaType: "array", errors: false, validate(rules, data) {
    if (data === null || typeof data !== "object" || Array.isArray(data)) return true;
    return rules.every((rule) => {
      if (!Array.isArray(data[rule.left]) || !Array.isArray(data[rule.right])) return false;
      const left = new Set(data[rule.left]);
      return data[rule.right].every((item) => !left.has(item));
    });
  }});
  ajv.addKeyword({keyword: "x-layerdraw-tagged-union", schemaType: "object", errors: false, validate(rule, data) {
    if (data === null || typeof data !== "object" || Array.isArray(data)) return true;
    const variant = rule.variants[String(data[rule.property])];
    if (variant === undefined) return false;
    const own = (key) => Object.prototype.hasOwnProperty.call(data, key);
    return (variant.required ?? []).every(own) && (variant.forbidden ?? []).every((key) => !own(key)) &&
      (variant.empty ?? []).every((key) => Array.isArray(data[key]) && data[key].length === 0) &&
      (variant.non_empty ?? []).every((key) => Array.isArray(data[key]) && data[key].length > 0) &&
      Object.entries(variant.allowed_values ?? {}).every(([key, values]) => !own(key) || values.includes(data[key]));
  }});
  ajv.addKeyword({keyword: "x-layerdraw-diff-source", schemaType: "boolean", errors: false, validate(enabled, data) {
    if (!enabled || data === null || typeof data !== "object" || Array.isArray(data) || data.kind !== "diff") return true;
    return typeof data.before === "string" && data.before.length > 0 && typeof data.after === "string" && data.after.length > 0 && data.before !== data.after &&
      (Object.prototype.hasOwnProperty.call(data, "query_address") || (data.arguments !== null && typeof data.arguments === "object" && !Array.isArray(data.arguments) && Object.keys(data.arguments).length === 0));
  }});
  ajv.addKeyword({keyword: "x-layerdraw-outcome-envelope", schemaType: "boolean", errors: false, validate(enabled, data) {
    if (!enabled || data === null || typeof data !== "object" || Array.isArray(data)) return true;
    const own = (key) => Object.prototype.hasOwnProperty.call(data, key);
    if (data.outcome === "success") return own("payload") && !own("failure");
    if (data.outcome === "rejected") return !own("payload") && !own("failure") && Array.isArray(data.diagnostics) && data.diagnostics.length > 0;
    if (data.outcome === "failed" || data.outcome === "cancelled") return !own("payload") && own("failure");
    return true;
  }});
  ajv.addKeyword({keyword: "x-layerdraw-ordered-range", schemaType: "boolean", errors: false, validate(enabled, data) {
    if (!enabled || data === null || typeof data !== "object") return true;
    try { return BigInt(data.start_byte) <= BigInt(data.end_byte); } catch { return false; }
  }});
  ajv.addKeyword({keyword: "x-layerdraw-operator-value", schemaType: "object", errors: false, validate(rule, data) {
    if (data === null || typeof data !== "object" || typeof data[rule.operator] !== "string") return true;
    const present = Object.prototype.hasOwnProperty.call(data, rule.value);
    return rule.valueless.includes(data[rule.operator]) ? !present : present;
  }});
  ajv.addKeyword({keyword: "x-layerdraw-protocol-offer", schemaType: "boolean", errors: false, validate(enabled, data) {
    if (!enabled || data === null || typeof data !== "object") return true;
    const range = protocolRange(data.supported_range);
    if (range === undefined || !Array.isArray(data.versions)) return false;
    const seen = new Set();
    return data.versions.every((binding) => {
      const version = protocolVersion(binding.version);
      if (version === undefined || seen.has(binding.version) || compareVersion(version, range[0]) < 0 || compareVersion(version, range[1]) > 0) return false;
      seen.add(binding.version);
      return true;
    });
  }});
  ajv.addKeyword({keyword: "x-layerdraw-limit-capability", schemaType: "boolean", errors: false, validate(enabled, data) {
    if (!enabled || data === null || typeof data !== "object") return true;
    try { return BigInt(data.default_value) <= BigInt(data.hard_maximum) && BigInt(data.effective_maximum) <= BigInt(data.hard_maximum); } catch { return false; }
  }});
  ajv.addKeyword({keyword: "x-layerdraw-unique-array-keys", schemaType: "array", errors: false, validate(rules, data) {
    if (data === null || typeof data !== "object") return true;
    return rules.every((rule) => {
      const seen = new Set();
      return Array.isArray(data[rule.array]) && data[rule.array].every((item) => !seen.has(item[rule.property]) && Boolean(seen.add(item[rule.property])));
    });
  }});
}

async function authority() {
  const meta = await readJSON("meta/layerdraw-protocol-schema-v1.json");
  const documents = await Promise.all([
    readJSON("protocol-common/v1.schema.json"),
    readJSON("semantic/v1.schema.json"),
    readJSON("engine-protocol/v1.schema.json"),
  ]);
  const ajv = new Ajv2020({allErrors: true, strict: true, validateFormats: true});
  addLayerDrawVocabulary(ajv);
  addLayerDrawFormats(ajv);
  ajv.addMetaSchema(meta);
  for (const document of documents) ajv.addSchema(document);
  return (id, name) => ajv.compile({$ref: `${id}#/$defs/${name}`});
}

test("normalized schema and document authority require request lifetime and the exact byte profile", async () => {
  const engine = await readJSON("engine-protocol/v1.schema.json");
  const readme = await readFile(new URL("README.md", schemaRoot), "utf8");
  const descriptions = {
    NormalizedPackArtifactBlobRef: "The public normalized Pack artifact bytes contain exactly the same JSON value as the canonical normalized Pack document; the canonical RFC 8785 UTF-8 bytes have no trailing LF, and these public bytes are exactly those canonical bytes followed by one LF.",
    NormalizedPackCanonicalBlobRef: "The canonical normalized Pack document bytes: RFC 8785 UTF-8 with no trailing LF.",
    NormalizedProjectArtifactBlobRef: "The public normalized Project artifact bytes contain exactly the same JSON value as the canonical normalized Project document; the canonical RFC 8785 UTF-8 bytes have no trailing LF, and these public bytes are exactly those canonical bytes followed by one LF.",
    NormalizedProjectCanonicalBlobRef: "The canonical normalized Project document bytes: RFC 8785 UTF-8 with no trailing LF.",
  };
  for (const [name, description] of Object.entries(descriptions)) {
    assert.equal(engine.$defs[name].description, description);
    assert.deepEqual(engine.$defs[name].properties.lifetime, {type: "string", const: "request"});
  }
  for (const authorityText of [JSON.stringify(engine), readme]) {
    assert.doesNotMatch(authorityText, /\b(?:may be identical|may equal canonical)\b/i);
  }
  assert.match(readme, /Canonical and public artifact\s+roles contain exactly the same JSON value\. Canonical bytes are RFC 8785 UTF-8\s+with no trailing LF; public artifact bytes are exactly those canonical bytes\s+followed by one LF\./);
});

test("published dialect requires format assertion and every codec-critical format agrees with authority vectors", async (context) => {
  const meta = await readJSON("meta/layerdraw-protocol-schema-v1.json");
  assert.equal(meta.$vocabulary["https://json-schema.org/draft/2020-12/vocab/format-assertion"], true);
  const compile = await authority();
  const corpus = await readJSON("fixtures/conformance/formats-v1.json");
  for (const vector of corpus.vectors) await context.test(vector.name, () => {
    assert.equal(compile(vector.schema_id, vector.type)(vector.value), vector.valid);
  });
});

test("published LayerDraw schema vocabulary asserts unions, outcomes, ranges, and offers", async () => {
  const compile = await authority();
  const common = "https://schemas.layerdraw.dev/protocol-common/v1";
  const semantic = "https://schemas.layerdraw.dev/semantic/v1";
  const engine = "https://schemas.layerdraw.dev/engine-protocol/v1";

  const total = compile(common, "TotalItems");
  assert.equal(total({known: true, exact: "1"}), true);
  assert.equal(total({known: false}), true);
  assert.equal(total({known: true}), false);
  assert.equal(total({known: false, exact: "1"}), false);

  const offer = compile(common, "ProtocolOffer");
  const digest = `sha256:${"a".repeat(64)}`;
  assert.equal(offer({name: "engine", supported_range: "1.0..1.2", versions: [{version: "1.0", schema_digest: digest}, {version: "1.2", schema_digest: digest}]}), true);
  assert.equal(offer({name: "engine", supported_range: "1.2..1.0", versions: [{version: "1.0", schema_digest: digest}]}), false);
  assert.equal(offer({name: "engine", supported_range: "1.9..2.0", versions: [{version: "1.9", schema_digest: digest}]}), false);
  assert.equal(offer({name: "engine", supported_range: "1.0..1.2", versions: [{version: "1.3", schema_digest: digest}]}), false);

  const handshake = compile(common, "HandshakeRequest");
  const handshakeBase = {client_release: "1.0.0", protocols: [{name: "engine", supported_range: "1.0..1.0", versions: [{version: "1.0", schema_digest: digest}]}], required_capabilities: ["engine.compile"], optional_capabilities: ["renderer.svg"]};
  assert.equal(handshake(handshakeBase), true);
  assert.equal(handshake({...handshakeBase, required_capabilities: ["engine.compile", "engine.compile"]}), false);
  assert.equal(handshake({...handshakeBase, optional_capabilities: ["engine.compile"]}), false);

  const stableAddress = compile(semantic, "StableAddress");
  assert.equal(stableAddress("ldl:project:p:entity-type:t:column:c"), true);
  assert.equal(stableAddress("ldl:pack:publisher:pack:entity:e"), false);
  assert.equal(stableAddress("ldl:project:p:entity-type:t:row:r"), false);

  const projectRoot = compile(semantic, "ProjectRootAddress");
  assert.equal(projectRoot("ldl:project:p"), true);
  assert.equal(projectRoot("ldl:pack:publisher:pack"), false);
  assert.equal(projectRoot("ldl:project:p:view:v"), false);
  const packRoot = compile(semantic, "PackRootAddress");
  assert.equal(packRoot("ldl:pack:publisher:pack"), true);
  assert.equal(packRoot("ldl:project:p"), false);
  assert.equal(packRoot("ldl:pack:publisher:pack:view:v"), false);
  const publicationRef = (media_type) => ({blob_id: "b", digest, lifetime: "request", media_type, size: "1"});
  const normalizedProject = compile(engine, "NormalizedProjectArtifact");
  const projectPublication = {project_address: "ldl:project:p", canonical_json: publicationRef("application/vnd.layerdraw.normalized-project.v1+json"), artifact_json: publicationRef("application/vnd.layerdraw.project.v1+json")};
  assert.equal(normalizedProject(projectPublication), true);
  assert.equal(normalizedProject({...projectPublication, project_address: "ldl:pack:publisher:pack"}), false);
  assert.equal(normalizedProject({...projectPublication, project_address: "ldl:project:p:view:v"}), false);
  const normalizedPack = compile(engine, "NormalizedPackArtifact");
  const packPublication = {pack_address: "ldl:pack:publisher:pack", canonical_json: publicationRef("application/vnd.layerdraw.normalized-pack.v1+json"), artifact_json: publicationRef("application/vnd.layerdraw.pack.v1+json")};
  assert.equal(normalizedPack(packPublication), true);
  assert.equal(normalizedPack({...packPublication, pack_address: "ldl:project:p"}), false);
  assert.equal(normalizedPack({...packPublication, pack_address: "ldl:pack:publisher:pack:view:v"}), false);

  const columnSource = compile(semantic, "ViewTableColumnSource");
  for (const value of [
    {kind: "field", field: "tags"},
    {kind: "attribute", column_addresses: ["ldl:project:p:entity-type:t:column:c"]},
    {kind: "relation_endpoint", endpoint: "from", field: "display_name"},
    {kind: "derived_count", direction: "both", relation_type_addresses: ["ldl:project:p:relation-type:r"]},
    {kind: "state", field_path: "system.updated_at"},
  ]) assert.equal(columnSource(value), true);
  for (const value of [
    {kind: "field"},
    {kind: "field", field: "not_a_field"},
    {kind: "attribute"},
    {kind: "attribute", column_addresses: [], field: "id"},
    {kind: "relation_endpoint", endpoint: "from"},
    {kind: "relation_endpoint", endpoint: "from", field: "description"},
    {kind: "derived_count"},
    {kind: "derived_count", direction: "both", field_path: "system.updated_at"},
    {kind: "state"},
    {kind: "state", field_path: "system.updated_at", relation_type_addresses: []},
    {kind: "state", field_path: "review.status"},
  ]) assert.equal(columnSource(value), false);

  const viewSource = compile(semantic, "ViewRecipeSource");
  const parameter = "ldl:project:p:query:q:parameter:x";
  const query = "ldl:project:p:query:q";
  const argumentsWithValue = {[parameter]: {kind: "string", string_value: "x"}};
  assert.equal(viewSource({kind: "diff", before: "base", after: "head", arguments: {}}), true);
  assert.equal(viewSource({kind: "diff", before: "base", after: "head", query_address: query, arguments: argumentsWithValue}), true);
  assert.equal(viewSource({kind: "diff", before: "", after: "head", arguments: {}}), false);
  assert.equal(viewSource({kind: "diff", before: "base", after: "", arguments: {}}), false);
  assert.equal(viewSource({kind: "diff", before: "same", after: "same", arguments: {}}), false);
  assert.equal(viewSource({kind: "diff", before: "base", after: "head", arguments: argumentsWithValue}), false);

  const predicate = compile(semantic, "RecipePredicate");
  assert.equal(predicate({kind: "field", field: "id", operator: "eq", value: {kind: "scalar", scalar_value: {kind: "string", string_value: "x"}}}), true);
  assert.equal(predicate({kind: "field"}), false);
  assert.equal(predicate({kind: "field", field: "id", operator: "exists", value: {kind: "scalar", scalar_value: {kind: "string", string_value: "x"}}}), false);

  const compileInput = compile(engine, "CompileInput");
  const base = {entry_path: "main.ldl", installed_pack_tree: [], mode: "project", project_source_tree: [{path: "main.ldl", blob: {blob_id: "b", digest, lifetime: "request", media_type: "text/plain", size: "1"}}], referenced_assets: [], resolved_dependencies: {format: "layerdraw-resolved", format_version: 1, installs: [], language: 1}, resource_limits: {}};
  assert.equal(compileInput(base), true);
  assert.equal(compileInput({...base, mode: "pack", root_pack_id: "publisher/pack"}), false);

  for (const [name, mediaType] of [["QueryRecipeBlobRef", "query"], ["ViewRecipeBlobRef", "view"], ["ExportRecipeBlobRef", "export"]]) {
    const recipeRef = compile(engine, name);
    const value = {blob_id: mediaType, digest, lifetime: "request", media_type: `application/vnd.layerdraw.${mediaType}-recipe.v1+json`, size: "1"};
    assert.equal(recipeRef(value), true);
    assert.equal(recipeRef({...value, lifetime: "session"}), false);
    assert.equal(recipeRef({...value, lifetime: "persistent"}), false);
  }
});

test("independent schema authority matches every shared View source vector", async (context) => {
  const compile = await authority();
  const semantic = "https://schemas.layerdraw.dev/semantic/v1";
  const corpus = await readJSON("fixtures/conformance/view-sources-v1.json");
  assert.equal(corpus.schema_version, 1);
  assert.equal(corpus.canonical_cases.length, 30);
  assert.equal(corpus.rejection_cases.length, 59);
  for (const vector of corpus.canonical_cases) await context.test(`${vector.name} accepted`, () => {
    assert.equal(compile(semantic, vector.type)(JSON.parse(vector.input)), true);
  });
  for (const vector of corpus.rejection_cases) await context.test(`${vector.name} rejected`, () => {
    assert.equal(compile(semantic, vector.type)(JSON.parse(vector.input)), false);
  });
});
