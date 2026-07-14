// SPDX-License-Identifier: Apache-2.0

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  decodeCompileRequestEnvelope,
  decodeCompileResponseEnvelope,
  decodeCanonicalSourcePath,
  decodeEffectiveResourceLimits,
  decodeExportRecipeBlobRef,
  decodeNormalizedPackArtifactBlobRef,
  decodeNormalizedPackCanonicalBlobRef,
  decodeNormalizedProjectArtifactBlobRef,
  decodeNormalizedProjectCanonicalBlobRef,
  decodeQueryRecipeBlobRef,
  decodeViewRecipeBlobRef,
  decodeHandshakeResponseEnvelope,
  encodeCompileRequestEnvelope,
  encodeCompileResponseEnvelope,
  encodeCanonicalSourcePath,
  encodeEffectiveResourceLimits,
  encodeExportRecipeBlobRef,
  encodeNormalizedPackArtifactBlobRef,
  encodeNormalizedPackCanonicalBlobRef,
  encodeNormalizedProjectArtifactBlobRef,
  encodeNormalizedProjectCanonicalBlobRef,
  encodeQueryRecipeBlobRef,
  encodeViewRecipeBlobRef,
  encodeHandshakeResponseEnvelope,
  isCompileRequestEnvelope,
  isCompileResponseEnvelope,
  isHandshakeResponseEnvelope,
} from "../dist/engine.gen.js";
import {
  decodeBlobRef,
  decodeCanonicalInt64,
  decodeCanonicalNonNegativeInt64,
  decodeCanonicalNonNegativeSafeInteger,
  decodeCanonicalPositiveInt64,
  decodeCanonicalPositiveSafeInteger,
  decodeCanonicalSafeInteger,
  decodeCanonicalUint64,
  decodeByteResourceLimitCapability,
  decodeCapabilityID,
  decodeDigest,
  decodeEndpointInstanceID,
  decodeJsonValue,
  decodeHandshakeRequest,
  decodeOperationCapability,
  decodeManifestETag,
  decodeProtocolOffer,
  decodeProtocolVersion,
  decodeProtocolVersionOrRange,
  decodeProtocolVersionRange,
  decodeRequestedCapabilityStatus,
  decodeReleaseVersion,
  decodeRfc3339Time,
  decodeTotalItems,
  decodeUpgradeDiagnosticData,
  encodeBlobRef,
  encodeCanonicalInt64,
  encodeCanonicalNonNegativeInt64,
  encodeCanonicalNonNegativeSafeInteger,
  encodeCanonicalPositiveInt64,
  encodeCanonicalPositiveSafeInteger,
  encodeCanonicalSafeInteger,
  encodeCanonicalUint64,
  encodeByteResourceLimitCapability,
  encodeCapabilityID,
  encodeDigest,
  encodeEndpointInstanceID,
  encodeJsonValue,
  encodeHandshakeRequest,
  encodeOperationCapability,
  encodeManifestETag,
  encodeProtocolOffer,
  encodeProtocolVersion,
  encodeProtocolVersionOrRange,
  encodeProtocolVersionRange,
  encodeRequestedCapabilityStatus,
  encodeReleaseVersion,
  encodeRfc3339Time,
  encodeTotalItems,
  encodeUpgradeDiagnosticData,
  maxWireJSONBytes,
  maxWireJSONDepth,
} from "../dist/common.gen.js";
import {
  decodeCompiledExportRecipeDocument,
  decodeCompiledQueryRecipeDocument,
  decodeCompiledViewRecipeDocument,
  decodeDiagnostic,
  decodeDiagnosticArgumentValue,
  decodeCanonicalFiniteDecimal,
  decodeCanonicalPositiveFiniteDecimal,
  decodeExportDimension,
  decodeExportOptions,
  decodeRecipePredicate,
  decodeRecipeRowPredicate,
  decodeSearchField,
  decodeStableAddress,
  decodeViewPlacement,
  decodeViewRenderSet,
  encodeCompiledExportRecipeDocument,
  encodeCompiledQueryRecipeDocument,
  encodeCompiledViewRecipeDocument,
  encodeDiagnostic,
  encodeDiagnosticArgumentValue,
  encodeCanonicalFiniteDecimal,
  encodeCanonicalPositiveFiniteDecimal,
  encodeExportDimension,
  encodeExportOptions,
  encodeRecipePredicate,
  encodeRecipeRowPredicate,
  encodeSearchField,
  encodeStableAddress,
  encodeViewPlacement,
  encodeViewRenderSet,
} from "../dist/semantic.gen.js";

const fixtureRoot = new URL("../../../schemas/fixtures/engine/", import.meta.url);
const commonFixtureRoot = new URL("../../../schemas/fixtures/common/", import.meta.url);
const conformanceCorpusURL = new URL("../../../schemas/fixtures/conformance/v1.json", import.meta.url);
const formatCorpusURL = new URL("../../../schemas/fixtures/conformance/formats-v1.json", import.meta.url);
const exportOptionsCorpusURL = new URL("../../../schemas/fixtures/conformance/export-options-v1.json", import.meta.url);
const predicateCorpusURL = new URL("../../../schemas/fixtures/conformance/predicates-v1.json", import.meta.url);
const canonicalEngineRoot = new URL("../../../schemas/fixtures/conformance/engine/", import.meta.url);

async function readFixture(name) {
  return JSON.parse(await readFile(new URL(name, fixtureRoot), "utf8"));
}

for (const [name, validate, decode, encode] of [
  ["compile-request.json", isCompileRequestEnvelope, decodeCompileRequestEnvelope, encodeCompileRequestEnvelope],
  ["compile-success.json", isCompileResponseEnvelope, decodeCompileResponseEnvelope, encodeCompileResponseEnvelope],
  ["compile-success-pack.json", isCompileResponseEnvelope, decodeCompileResponseEnvelope, encodeCompileResponseEnvelope],
  ["compile-rejected.json", isCompileResponseEnvelope, decodeCompileResponseEnvelope, encodeCompileResponseEnvelope],
  ["handshake-success.json", isHandshakeResponseEnvelope, decodeHandshakeResponseEnvelope, encodeHandshakeResponseEnvelope],
  ["handshake-rejected.json", isHandshakeResponseEnvelope, decodeHandshakeResponseEnvelope, encodeHandshakeResponseEnvelope],
]) {
  test(`${name} validates and canonically round-trips without loss`, async () => {
    const text = await readFile(new URL(name, fixtureRoot), "utf8");
    const fixture = JSON.parse(text);
    assert.equal(validate(fixture), true);
    const canonical = encode(decode(text));
    assert.deepEqual(JSON.parse(canonical), fixture);
    assert.equal(encode(decode(canonical)), canonical);
  });
}

test("invalid compile input is rejected", async () => {
  const fixture = await readFixture("compile-invalid-request.json");
  assert.equal(isCompileRequestEnvelope(fixture), false);
});

test("unknown response fields are rejected while explicit extensions survive", async () => {
  const fixture = await readFixture("compile-rejected.json");
  assert.equal(isCompileResponseEnvelope({...fixture, unexpected: true}), false);
  const extended = {...fixture, extensions: {"example.test": {enabled: true}}};
  assert.equal(isCompileResponseEnvelope(extended), true);
  assert.deepEqual(JSON.parse(encodeCompileResponseEnvelope(decodeCompileResponseEnvelope(JSON.stringify(extended)))), extended);
});

test("shared common fixtures have byte-identical canonical encoding", async () => {
  const blobText = (await readFile(new URL("blob-ref-canonical.json", commonFixtureRoot), "utf8")).trim();
  assert.equal(encodeBlobRef(decodeBlobRef(blobText)), blobText);
  const jsonText = (await readFile(new URL("json-value-canonical.json", commonFixtureRoot), "utf8")).trim();
  assert.equal(encodeJsonValue(decodeJsonValue(jsonText)), jsonText);
});

test("TypeScript matches shared canonical Go/TypeScript engine-envelope bytes", async (context) => {
  for (const [name, decode, encode] of [
    ["compile-request.json", decodeCompileRequestEnvelope, encodeCompileRequestEnvelope],
    ["compile-rejected.json", decodeCompileResponseEnvelope, encodeCompileResponseEnvelope],
    ["compile-success-pack.json", decodeCompileResponseEnvelope, encodeCompileResponseEnvelope],
    ["handshake-success.json", decodeHandshakeResponseEnvelope, encodeHandshakeResponseEnvelope],
    ["handshake-rejected.json", decodeHandshakeResponseEnvelope, encodeHandshakeResponseEnvelope],
  ]) await context.test(name, async () => {
    const source = await readFile(new URL(name, fixtureRoot), "utf8");
    const canonical = (await readFile(new URL(name, canonicalEngineRoot), "utf8")).trim();
    assert.equal(encode(decode(source)), canonical);
    assert.equal(encode(decode(canonical)), canonical);
  });
});

test("legacy scalar negatives remain enforced", async () => {
  for (const invalid of ["-0", "01", "18446744073709551616"]) {
    assert.throws(() => decodeCanonicalUint64(JSON.stringify(invalid)));
  }
  for (const invalid of [
    "sha256:ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
    "md5:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  ]) assert.throws(() => decodeDigest(JSON.stringify(invalid)));
});

test("TypeScript encoders inspect exactly own enumerable data properties", () => {
  const inherited = Object.create({enabled: true, protocol_version: "1.0"});
  assert.throws(() => encodeOperationCapability(inherited));

  const hidden = {};
  Object.defineProperty(hidden, "enabled", {value: true, enumerable: false});
  Object.defineProperty(hidden, "protocol_version", {value: "1.0", enumerable: true});
  assert.throws(() => encodeOperationCapability(hidden));

  const symbol = {enabled: true, protocol_version: "1.0"};
  symbol[Symbol("extension")] = true;
  assert.throws(() => encodeOperationCapability(symbol));

  let getterCalled = false;
  const getter = {protocol_version: "1.0"};
  Object.defineProperty(getter, "enabled", {enumerable: true, get() { getterCalled = true; return true; }});
  assert.throws(() => encodeOperationCapability(getter));
  assert.equal(getterCalled, false);

  const nullPrototype = Object.assign(Object.create(null), {enabled: true, protocol_version: "1.0"});
  assert.equal(encodeOperationCapability(nullPrototype), '{"enabled":true,"protocol_version":"1.0"}');
});

const sharedCodecs = {
  ByteResourceLimitCapability: [decodeByteResourceLimitCapability, encodeByteResourceLimitCapability],
  CapabilityID: [decodeCapabilityID, encodeCapabilityID],
  CanonicalFiniteDecimal: [decodeCanonicalFiniteDecimal, encodeCanonicalFiniteDecimal],
  CanonicalInt64: [decodeCanonicalInt64, encodeCanonicalInt64],
  CanonicalNonNegativeInt64: [decodeCanonicalNonNegativeInt64, encodeCanonicalNonNegativeInt64],
  CanonicalNonNegativeSafeInteger: [decodeCanonicalNonNegativeSafeInteger, encodeCanonicalNonNegativeSafeInteger],
  CanonicalPositiveFiniteDecimal: [decodeCanonicalPositiveFiniteDecimal, encodeCanonicalPositiveFiniteDecimal],
  CanonicalPositiveInt64: [decodeCanonicalPositiveInt64, encodeCanonicalPositiveInt64],
  CanonicalPositiveSafeInteger: [decodeCanonicalPositiveSafeInteger, encodeCanonicalPositiveSafeInteger],
  CanonicalSafeInteger: [decodeCanonicalSafeInteger, encodeCanonicalSafeInteger],
  CanonicalSourcePath: [decodeCanonicalSourcePath, encodeCanonicalSourcePath],
  CanonicalUint64: [decodeCanonicalUint64, encodeCanonicalUint64],
  CompiledExportRecipeDocument: [decodeCompiledExportRecipeDocument, encodeCompiledExportRecipeDocument],
  CompiledQueryRecipeDocument: [decodeCompiledQueryRecipeDocument, encodeCompiledQueryRecipeDocument],
  CompiledViewRecipeDocument: [decodeCompiledViewRecipeDocument, encodeCompiledViewRecipeDocument],
  Diagnostic: [decodeDiagnostic, encodeDiagnostic],
  Digest: [decodeDigest, encodeDigest],
  EndpointInstanceID: [decodeEndpointInstanceID, encodeEndpointInstanceID],
  EffectiveResourceLimits: [decodeEffectiveResourceLimits, encodeEffectiveResourceLimits],
  ExportRecipeBlobRef: [decodeExportRecipeBlobRef, encodeExportRecipeBlobRef],
  ExportDimension: [decodeExportDimension, encodeExportDimension],
  ExportOptions: [decodeExportOptions, encodeExportOptions],
  JsonValue: [decodeJsonValue, encodeJsonValue],
  HandshakeRequest: [decodeHandshakeRequest, encodeHandshakeRequest],
  ManifestETag: [decodeManifestETag, encodeManifestETag],
  NormalizedPackArtifactBlobRef: [decodeNormalizedPackArtifactBlobRef, encodeNormalizedPackArtifactBlobRef],
  NormalizedPackCanonicalBlobRef: [decodeNormalizedPackCanonicalBlobRef, encodeNormalizedPackCanonicalBlobRef],
  NormalizedProjectArtifactBlobRef: [decodeNormalizedProjectArtifactBlobRef, encodeNormalizedProjectArtifactBlobRef],
  NormalizedProjectCanonicalBlobRef: [decodeNormalizedProjectCanonicalBlobRef, encodeNormalizedProjectCanonicalBlobRef],
  OperationCapability: [decodeOperationCapability, encodeOperationCapability],
  ProtocolOffer: [decodeProtocolOffer, encodeProtocolOffer],
  ProtocolVersion: [decodeProtocolVersion, encodeProtocolVersion],
  ProtocolVersionOrRange: [decodeProtocolVersionOrRange, encodeProtocolVersionOrRange],
  ProtocolVersionRange: [decodeProtocolVersionRange, encodeProtocolVersionRange],
  RecipePredicate: [decodeRecipePredicate, encodeRecipePredicate],
  RecipeRowPredicate: [decodeRecipeRowPredicate, encodeRecipeRowPredicate],
  RequestedCapabilityStatus: [decodeRequestedCapabilityStatus, encodeRequestedCapabilityStatus],
  QueryRecipeBlobRef: [decodeQueryRecipeBlobRef, encodeQueryRecipeBlobRef],
  ReleaseVersion: [decodeReleaseVersion, encodeReleaseVersion],
  Rfc3339Time: [decodeRfc3339Time, encodeRfc3339Time],
  SearchField: [decodeSearchField, encodeSearchField],
  StableAddress: [decodeStableAddress, encodeStableAddress],
  TotalItems: [decodeTotalItems, encodeTotalItems],
  UpgradeDiagnosticData: [decodeUpgradeDiagnosticData, encodeUpgradeDiagnosticData],
  ViewPlacement: [decodeViewPlacement, encodeViewPlacement],
  ViewRecipeBlobRef: [decodeViewRecipeBlobRef, encodeViewRecipeBlobRef],
  ViewRenderSet: [decodeViewRenderSet, encodeViewRenderSet],
};

async function readCorpus() {
  return JSON.parse(await readFile(conformanceCorpusURL, "utf8"));
}

test("shared canonical and rejection corpus matches exact bytes", async (context) => {
  const corpus = await readCorpus();
  assert.equal(corpus.schema_version, 1);
  assert.equal(corpus.max_json_bytes, maxWireJSONBytes);
  assert.equal(corpus.max_json_depth, maxWireJSONDepth);
  for (const vector of corpus.canonical_cases) await context.test(vector.name, () => {
    const codec = sharedCodecs[vector.type];
    assert.ok(codec, `unknown shared codec ${vector.type}`);
    const encoded = codec[1](codec[0](vector.input));
    assert.equal(encoded, vector.expected);
    assert.equal(codec[1](codec[0](encoded)), encoded);
  });
  for (const vector of corpus.rejection_cases) await context.test(vector.name, () => {
    const codec = sharedCodecs[vector.type];
    assert.ok(codec, `unknown shared codec ${vector.type}`);
    assert.throws(() => codec[0](vector.input));
  });
});

test("shared custom format authority vectors match TypeScript codecs", async (context) => {
  const corpus = JSON.parse(await readFile(formatCorpusURL, "utf8"));
  for (const vector of corpus.vectors) await context.test(vector.name, () => {
    const codec = sharedCodecs[vector.type];
    assert.ok(codec, `unknown format codec ${vector.type}`);
    const input = JSON.stringify(vector.value);
    if (vector.valid) assert.equal(codec[1](codec[0](input)), input);
    else assert.throws(() => codec[0](input));
  });
});

test("every closed export-option variant has shared canonical and rejection bytes", async (context) => {
  const corpus = JSON.parse(await readFile(exportOptionsCorpusURL, "utf8"));
  assert.equal(corpus.schema_version, 1);
  for (const vector of corpus.canonical_cases) await context.test(`${vector.name} canonical`, () => {
    assert.equal(encodeExportOptions(decodeExportOptions(vector.input)), vector.expected);
  });
  for (const vector of corpus.rejection_cases) await context.test(`${vector.name} rejection`, () => {
    assert.throws(() => decodeExportOptions(vector.input));
  });
});

test("every predicate branch has shared canonical and rejection bytes", async (context) => {
  const corpus = JSON.parse(await readFile(predicateCorpusURL, "utf8"));
  for (const vector of corpus.canonical_cases) await context.test(`${vector.name} canonical`, () => {
    const codec = sharedCodecs[vector.type];
    assert.equal(codec[1](codec[0](vector.input)), vector.expected);
  });
  for (const vector of corpus.rejection_cases) await context.test(`${vector.name} rejection`, () => {
    assert.throws(() => sharedCodecs[vector.type][0](vector.input));
  });
});

test("shared recursive JsonValue limits are exact", async () => {
  const corpus = await readCorpus();
  const deep = "[".repeat(corpus.max_json_depth) + '"x"' + "]".repeat(corpus.max_json_depth);
  assert.equal(encodeJsonValue(decodeJsonValue(deep)), deep);
  assert.throws(() => decodeJsonValue("[" + deep + "]"));
  const maximum = '"' + "a".repeat(corpus.max_json_bytes - 2) + '"';
  assert.equal(encodeJsonValue(decodeJsonValue(maximum)), maximum);
  assert.throws(() => decodeJsonValue('"' + "a".repeat(corpus.max_json_bytes - 1) + '"'));
});

test("programmatic JsonValue cycles and depth fail with TypeError", () => {
  const self = {};
  self.self = self;
  assert.throws(() => encodeJsonValue(self), TypeError);
  const left = {};
  const right = {};
  left.right = right;
  right.left = left;
  assert.throws(() => encodeJsonValue(left), TypeError);

  let value = "leaf";
  for (let depth = 0; depth < maxWireJSONDepth; depth++) value = [value];
  const encoded = encodeJsonValue(value);
  assert.deepEqual(decodeJsonValue(encoded), value);
  assert.throws(() => encodeJsonValue([value]), TypeError);
});

function containerDepth(value) {
  if (value === null || typeof value !== "object") return 0;
  const children = Array.isArray(value) ? value : Object.values(value);
  return 1 + children.reduce((maximum, child) => Math.max(maximum, containerDepth(child)), 0);
}

function expectCycleError(callback) {
  assert.throws(callback, (error) => error instanceof TypeError && error.message === "protocol value contains a cycle");
}

function expectDepthError(callback) {
  assert.throws(callback, (error) => error instanceof TypeError && error.message === `protocol value exceeds depth ${maxWireJSONDepth}`);
}

test("semantic recursive codecs reject self and mutual cycles while preserving aliases", () => {
  const cases = [
    [
      "DiagnosticArgumentValue",
      encodeDiagnosticArgumentValue,
      () => { const value = {kind: "array", array_value: []}; value.array_value.push(value); return value; },
      () => { const left = {kind: "array", array_value: []}; const right = {kind: "array", array_value: [left]}; left.array_value.push(right); return left; },
      () => { const shared = {kind: "string", string_value: "shared"}; return {kind: "array", array_value: [shared, shared]}; },
    ],
    [
      "RecipePredicate",
      encodeRecipePredicate,
      () => { const value = {kind: "not"}; value.child = value; return value; },
      () => { const left = {kind: "not"}; const right = {kind: "not", child: left}; left.child = right; return left; },
      () => { const shared = {kind: "field", field: "name", operator: "exists"}; return {kind: "all", children: [shared, shared]}; },
    ],
    [
      "RecipeRowPredicate",
      encodeRecipeRowPredicate,
      () => { const value = {kind: "not"}; value.child = value; return value; },
      () => { const left = {kind: "not"}; const right = {kind: "not", child: left}; left.child = right; return left; },
      () => { const shared = {kind: "state", field_path: "name", operator: "exists"}; return {kind: "all", children: [shared, shared]}; },
    ],
  ];
  for (const [name, encode, self, mutual, alias] of cases) {
    expectCycleError(() => encode(self()), `${name} self cycle`);
    expectCycleError(() => encode(mutual()), `${name} mutual cycle`);
    assert.doesNotThrow(() => encode(alias()), `${name} acyclic alias`);
  }
});

test("semantic recursive codecs enforce exact programmatic wire depth", () => {
  let diagnosticAtLimit = {kind: "array", array_value: []};
  for (let depth = 0; depth < 63; depth++) diagnosticAtLimit = {kind: "array", array_value: [diagnosticAtLimit]};
  assert.equal(containerDepth(diagnosticAtLimit), maxWireJSONDepth);
  assert.doesNotThrow(() => encodeDiagnosticArgumentValue(diagnosticAtLimit));
  assert.deepEqual(decodeDiagnosticArgumentValue(encodeDiagnosticArgumentValue(diagnosticAtLimit)), diagnosticAtLimit);

  let diagnosticTooDeep = {kind: "string", string_value: "leaf"};
  for (let depth = 0; depth < 64; depth++) diagnosticTooDeep = {kind: "array", array_value: [diagnosticTooDeep]};
  assert.equal(containerDepth(diagnosticTooDeep), maxWireJSONDepth + 1);
  expectDepthError(() => encodeDiagnosticArgumentValue(diagnosticTooDeep));

  for (const [name, encode, leaf] of [
    ["RecipePredicate", encodeRecipePredicate, {kind: "field", field: "name", operator: "exists"}],
    ["RecipeRowPredicate", encodeRecipeRowPredicate, {kind: "state", field_path: "name", operator: "exists"}],
  ]) {
    let value = leaf;
    for (let depth = 1; depth < maxWireJSONDepth; depth++) value = {kind: "not", child: value};
    assert.equal(containerDepth(value), maxWireJSONDepth, name);
    assert.doesNotThrow(() => encode(value), name);
    const tooDeep = {kind: "not", child: value};
    assert.equal(containerDepth(tooDeep), maxWireJSONDepth + 1, name);
    expectDepthError(() => encode(tooDeep), name);
  }
});

test("engine codecs apply the shared cycle and depth preflight", async () => {
  const aliased = await readFixture("compile-request.json");
  const shared = {enabled: true};
  aliased.extensions = {"example.left": shared, "example.right": shared};
  assert.doesNotThrow(() => encodeCompileRequestEnvelope(aliased));

  const cyclic = await readFixture("compile-request.json");
  const self = {};
  self.self = self;
  cyclic.extensions = {"example.cycle": self};
  expectCycleError(() => encodeCompileRequestEnvelope(cyclic));

  const atLimit = await readFixture("compile-request.json");
  let extension = "leaf";
  for (let depth = 0; depth < maxWireJSONDepth - 2; depth++) extension = [extension];
  atLimit.extensions = {"example.depth": extension};
  assert.equal(containerDepth(atLimit), maxWireJSONDepth);
  assert.doesNotThrow(() => encodeCompileRequestEnvelope(atLimit));

  const tooDeep = await readFixture("compile-request.json");
  tooDeep.extensions = {"example.depth": [extension]};
  assert.equal(containerDepth(tooDeep), maxWireJSONDepth + 1);
  expectDepthError(() => encodeCompileRequestEnvelope(tooDeep));
});

test("canonical byte limit uses emitted bytes for escaped characters and multibyte keys", async (context) => {
  const byteLength = (value) => new TextEncoder().encode(value).length;
  for (const fill of ["<", ">", "&", "\u2028", "\u2029"]) await context.test(`text U+${fill.codePointAt(0).toString(16)}`, () => {
    const base = {field_path: "p", include_in_embedding: false, lexical_weight: 1, text: ""};
    const emptyBytes = byteLength(encodeSearchField(base));
    const unitBytes = byteLength(encodeSearchField({...base, text: fill})) - emptyBytes;
    const available = maxWireJSONBytes - emptyBytes;
    const text = fill.repeat(Math.floor(available / unitBytes)) + "a".repeat(available % unitBytes);
    assert.equal(byteLength(encodeSearchField({...base, text})), maxWireJSONBytes);
    assert.throws(() => encodeSearchField({...base, text: text + "a"}), TypeError);
  });
  for (const key of ["界", "😀"]) await context.test(`key U+${key.codePointAt(0).toString(16)}`, () => {
    const emptyBytes = byteLength(encodeJsonValue({[key]: ""}));
    const text = "a".repeat(maxWireJSONBytes - emptyBytes);
    assert.equal(byteLength(encodeJsonValue({[key]: text})), maxWireJSONBytes);
    assert.throws(() => encodeJsonValue({[key]: text + "a"}), TypeError);
  });
});

test("shared response-envelope mutations are rejected before blob resolution", async (context) => {
  const corpus = await readCorpus();
  for (const vector of corpus.mutation_cases) await context.test(vector.name, async () => {
    const value = await readFixture(vector.fixture);
    switch (vector.mutation) {
      case "add_valid_failure":
        value.failure = {category: "invariant", code: "engine.invariant", message: "safe", retryable: false};
        break;
      case "add_valid_pack_variant": {
        const pack = await readFixture("compile-success-pack.json");
        value.payload.normalized_artifact.pack = pack.payload.normalized_artifact.pack;
        break;
      }
      case "remove_pack_variant":
        delete value.payload.normalized_artifact.pack;
        break;
      case "set_failed":
        value.outcome = "failed";
        break;
      case "set_cancelled":
        value.outcome = "cancelled";
        break;
      case "add_valid_success_payload": {
        const success = await readFixture("compile-success.json");
        value.payload = success.payload;
        break;
      }
      case "remove_project_graph":
        delete value.payload.normalized_artifact.graph_hash;
        break;
      case "add_pack_graph":
        value.payload.normalized_artifact.graph_hash = "sha256:" + "e".repeat(64);
        break;
      case "add_pack_search_document": {
        const success = await readFixture("compile-success.json");
        value.payload.normalized_artifact.search_documents = success.payload.normalized_artifact.search_documents;
        break;
      }
      case "corrupt_project_media_type":
        value.payload.normalized_artifact.project.artifact_json.media_type = "application/json";
        break;
      case "set_project_artifact_session_lifetime":
        value.payload.normalized_artifact.project.artifact_json.lifetime = "session";
        break;
      case "set_project_artifact_persistent_lifetime":
        value.payload.normalized_artifact.project.artifact_json.lifetime = "persistent";
        break;
      case "set_project_canonical_session_lifetime":
        value.payload.normalized_artifact.project.canonical_json.lifetime = "session";
        break;
      case "set_project_canonical_persistent_lifetime":
        value.payload.normalized_artifact.project.canonical_json.lifetime = "persistent";
        break;
      case "set_pack_artifact_session_lifetime":
        value.payload.normalized_artifact.pack.artifact_json.lifetime = "session";
        break;
      case "set_pack_artifact_persistent_lifetime":
        value.payload.normalized_artifact.pack.artifact_json.lifetime = "persistent";
        break;
      case "set_pack_canonical_session_lifetime":
        value.payload.normalized_artifact.pack.canonical_json.lifetime = "session";
        break;
      case "set_pack_canonical_persistent_lifetime":
        value.payload.normalized_artifact.pack.canonical_json.lifetime = "persistent";
        break;
      default:
        assert.fail(`unknown mutation ${vector.mutation}`);
    }
    assert.throws(() => decodeCompileResponseEnvelope(JSON.stringify(value)));
  });
});

test("shared compile request mode/root mutations are rejected", async (context) => {
  const corpus = await readCorpus();
  for (const vector of corpus.request_mutation_cases) await context.test(vector.name, async () => {
    const value = await readFixture(vector.fixture);
    switch (vector.mutation) {
      case "add_project_root":
        value.payload.root_pack_id = "publisher/root";
        break;
      case "set_pack_without_root":
        value.payload.mode = "pack";
        break;
      case "set_pack_empty_root":
        value.payload.mode = "pack";
        value.payload.root_pack_id = "";
        break;
      case "set_pack_with_root":
        value.payload.mode = "pack";
        value.payload.root_pack_id = "publisher/root";
        break;
      case "set_pack_bad_root":
        value.payload.mode = "pack";
        value.payload.root_pack_id = "Bad";
        value.payload.installed_pack_tree = value.payload.project_source_tree;
        value.payload.project_source_tree = [];
        break;
      case "project_asset_pack_id": {
        const source = value.payload.project_source_tree[0];
        value.payload.referenced_assets = [{origin: "project", pack_id: "publisher/pack", locator: "asset.svg", blob: source.blob, digest: source.blob.digest, media_type: "image/svg+xml"}];
        break;
      }
      case "pack_asset_without_id": {
        const source = value.payload.project_source_tree[0];
        value.payload.referenced_assets = [{origin: "pack", locator: "asset.svg", blob: source.blob, digest: source.blob.digest, media_type: "image/svg+xml"}];
        break;
      }
      case "bad_source_path":
        value.payload.project_source_tree[0].path = "../document.ldl";
        break;
      default:
        assert.fail(`unknown request mutation ${vector.mutation}`);
    }
    assert.throws(() => decodeCompileRequestEnvelope(JSON.stringify(value)));
  });
});
