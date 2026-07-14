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

function addLayerDrawVocabulary(ajv) {
  ajv.addKeyword({keyword: "x-layerdraw-go-package", schemaType: "string", errors: false, validate: () => true});
  ajv.addKeyword({keyword: "x-layerdraw-max-json-bytes", schemaType: "number", errors: false, validate: () => true});
  ajv.addKeyword({keyword: "x-layerdraw-max-json-depth", schemaType: "number", errors: false, validate: () => true});
  ajv.addKeyword({keyword: "x-layerdraw-ts-module", schemaType: "string", errors: false, validate: () => true});
  ajv.addKeyword({keyword: "x-layerdraw-tagged-union", schemaType: "object", errors: false, validate(rule, data) {
    if (data === null || typeof data !== "object" || Array.isArray(data)) return true;
    const variant = rule.variants[String(data[rule.property])];
    if (variant === undefined) return false;
    const own = (key) => Object.prototype.hasOwnProperty.call(data, key);
    return (variant.required ?? []).every(own) && (variant.forbidden ?? []).every((key) => !own(key)) &&
      (variant.empty ?? []).every((key) => Array.isArray(data[key]) && data[key].length === 0) &&
      (variant.non_empty ?? []).every((key) => Array.isArray(data[key]) && data[key].length > 0);
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
  const ajv = new Ajv2020({allErrors: true, strict: true, validateFormats: false});
  addLayerDrawVocabulary(ajv);
  ajv.addMetaSchema(meta);
  for (const document of documents) ajv.addSchema(document);
  return (id, name) => ajv.compile({$ref: `${id}#/$defs/${name}`});
}

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

  const predicate = compile(semantic, "RecipePredicate");
  assert.equal(predicate({kind: "field", field: "id", operator: "eq", value: {kind: "scalar", scalar_value: {kind: "string", string_value: "x"}}}), true);
  assert.equal(predicate({kind: "field"}), false);
  assert.equal(predicate({kind: "field", field: "id", operator: "exists", value: {kind: "scalar", scalar_value: {kind: "string", string_value: "x"}}}), false);

  const compileInput = compile(engine, "CompileInput");
  const base = {entry_path: "main.ldl", installed_pack_tree: [], mode: "project", project_source_tree: [{path: "main.ldl", blob: {blob_id: "b", digest, lifetime: "request", media_type: "text/plain", size: "1"}}], referenced_assets: [], resolved_dependencies: {format: "layerdraw-resolved", format_version: 1, installs: [], language: 1}, resource_limits: {}};
  assert.equal(compileInput(base), true);
  assert.equal(compileInput({...base, mode: "pack", root_pack_id: "publisher/pack"}), false);
});
