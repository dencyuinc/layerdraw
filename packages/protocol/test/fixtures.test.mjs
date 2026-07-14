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
  encodeNormalizedPackArtifact,
  encodeNormalizedProjectArtifact,
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
  encodeExtensions,
  encodeJsonObject,
  isExtensions,
  isJsonObject,
  isJsonValue,
  isOperationCapability,
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
  decodePackRootAddress,
  decodeProjectRootAddress,
  decodeViewRecipeSource,
  decodeViewDiagramShape,
  decodeViewFlowShape,
  decodeViewMatrixAxis,
  decodeViewMatrixCell,
  decodeViewTableColumnSource,
  decodeViewTableShape,
  decodeViewTreeShape,
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
  encodePackRootAddress,
  encodeProjectRootAddress,
  encodeViewRecipeSource,
  encodeViewDiagramShape,
  encodeViewFlowShape,
  encodeViewMatrixAxis,
  encodeViewMatrixCell,
  encodeViewTableColumnSource,
  encodeViewTableShape,
  encodeViewTreeShape,
  encodeViewPlacement,
  encodeViewRenderSet,
  isDiagnosticArgumentValue,
  isRecipePredicate,
  isRecipeRowPredicate,
} from "../dist/semantic.gen.js";

const fixtureRoot = new URL("../../../schemas/fixtures/engine/", import.meta.url);
const commonFixtureRoot = new URL("../../../schemas/fixtures/common/", import.meta.url);
const conformanceCorpusURL = new URL("../../../schemas/fixtures/conformance/v1.json", import.meta.url);
const formatCorpusURL = new URL("../../../schemas/fixtures/conformance/formats-v1.json", import.meta.url);
const exportOptionsCorpusURL = new URL("../../../schemas/fixtures/conformance/export-options-v1.json", import.meta.url);
const predicateCorpusURL = new URL("../../../schemas/fixtures/conformance/predicates-v1.json", import.meta.url);
const viewSourceCorpusURL = new URL("../../../schemas/fixtures/conformance/view-sources-v1.json", import.meta.url);
const unicodeScalarCorpusURL = new URL("../../../schemas/fixtures/conformance/unicode-scalars-v1.json", import.meta.url);
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
  assert.equal(isOperationCapability(inherited), false);
  assert.throws(() => encodeOperationCapability(inherited));

  const hidden = {};
  Object.defineProperty(hidden, "enabled", {value: true, enumerable: false});
  Object.defineProperty(hidden, "protocol_version", {value: "1.0", enumerable: true});
  assert.equal(isOperationCapability(hidden), false);
  assert.throws(() => encodeOperationCapability(hidden));

  const symbol = {enabled: true, protocol_version: "1.0"};
  symbol[Symbol("extension")] = true;
  assert.equal(isOperationCapability(symbol), false);
  assert.throws(() => encodeOperationCapability(symbol));

  let getterCalled = false;
  const getter = {protocol_version: "1.0"};
  Object.defineProperty(getter, "enabled", {enumerable: true, get() { getterCalled = true; return true; }});
  assert.equal(isOperationCapability(getter), false);
  assert.throws(() => encodeOperationCapability(getter));
  assert.equal(getterCalled, false);

  const nullPrototype = Object.assign(Object.create(null), {enabled: true, protocol_version: "1.0"});
  assert.equal(isOperationCapability(nullPrototype), true);
  assert.equal(encodeOperationCapability(nullPrototype), '{"enabled":true,"protocol_version":"1.0"}');

  for (const hostile of [
    new Proxy({}, {getPrototypeOf() { throw new Error("prototype trap"); }}),
    new Proxy({}, {ownKeys() { throw new Error("ownKeys trap"); }}),
    new Proxy({enabled: true, protocol_version: "1.0"}, {getOwnPropertyDescriptor() { throw new Error("descriptor trap"); }}),
  ]) assert.equal(isOperationCapability(hostile), false);
});

test("public object predicates reject non-scalar Unicode keys at every object surface", async () => {
  const compileRequest = await readFixture("compile-request.json");
  const invalidCases = [];
  for (const badKey of ["\ud800", "\udc00"]) {
    invalidCases.push(
      ["JsonValue root map", isJsonValue, encodeJsonValue, {[badKey]: null}],
      ["JsonObject root map", isJsonObject, encodeJsonObject, {[badKey]: null}],
      ["Extensions additionalProperties map", isExtensions, encodeExtensions, {[badKey]: null}],
      ["DiagnosticArgumentValue recursive object map", isDiagnosticArgumentValue, encodeDiagnosticArgumentValue, {kind: "object", object_value: {[badKey]: {kind: "string", string_value: "leaf"}}}],
      ["closed root object", isCompileRequestEnvelope, encodeCompileRequestEnvelope, {...compileRequest, [badKey]: null}],
      ["nested closed object", isCompileRequestEnvelope, encodeCompileRequestEnvelope, {...compileRequest, payload: {...compileRequest.payload, [badKey]: null}}],
      ["nested extension map", isCompileRequestEnvelope, encodeCompileRequestEnvelope, {...compileRequest, extensions: {[badKey]: null}}],
    );
  }
  for (const [name, predicate, encode, value] of invalidCases) {
    assert.equal(predicate(value), false, name);
    assert.throws(() => encode(value), TypeError, name);
  }

  const sharedJSON = {enabled: true};
  const validExtensions = {"example.界": sharedJSON, "example.😀": sharedJSON};
  assert.equal(isJsonValue(validExtensions), true);
  assert.equal(isJsonObject(validExtensions), true);
  assert.equal(isExtensions(validExtensions), true);
  assert.doesNotThrow(() => encodeJsonValue(validExtensions));
  assert.doesNotThrow(() => encodeJsonObject(validExtensions));
  assert.doesNotThrow(() => encodeExtensions(validExtensions));

  const sharedDiagnostic = {kind: "string", string_value: "shared"};
  const validDiagnostic = {kind: "object", object_value: {"界": sharedDiagnostic, "😀": sharedDiagnostic}};
  assert.equal(isDiagnosticArgumentValue(validDiagnostic), true);
  assert.doesNotThrow(() => encodeDiagnosticArgumentValue(validDiagnostic));

  const validCompileRequest = {...compileRequest, extensions: validExtensions};
  assert.equal(isCompileRequestEnvelope(validCompileRequest), true);
  assert.doesNotThrow(() => encodeCompileRequestEnvelope(validCompileRequest));
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
  PackRootAddress: [decodePackRootAddress, encodePackRootAddress],
  ProjectRootAddress: [decodeProjectRootAddress, encodeProjectRootAddress],
  TotalItems: [decodeTotalItems, encodeTotalItems],
  UpgradeDiagnosticData: [decodeUpgradeDiagnosticData, encodeUpgradeDiagnosticData],
  ViewPlacement: [decodeViewPlacement, encodeViewPlacement],
  ViewRecipeBlobRef: [decodeViewRecipeBlobRef, encodeViewRecipeBlobRef],
  ViewRenderSet: [decodeViewRenderSet, encodeViewRenderSet],
  ViewRecipeSource: [decodeViewRecipeSource, encodeViewRecipeSource],
  ViewDiagramShape: [decodeViewDiagramShape, encodeViewDiagramShape],
  ViewFlowShape: [decodeViewFlowShape, encodeViewFlowShape],
  ViewMatrixAxis: [decodeViewMatrixAxis, encodeViewMatrixAxis],
  ViewMatrixCell: [decodeViewMatrixCell, encodeViewMatrixCell],
  ViewTableColumnSource: [decodeViewTableColumnSource, encodeViewTableColumnSource],
  ViewTableShape: [decodeViewTableShape, encodeViewTableShape],
  ViewTreeShape: [decodeViewTreeShape, encodeViewTreeShape],
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

test("every View source and address-bearing shape contract has shared bytes", async (context) => {
  const corpus = JSON.parse(await readFile(viewSourceCorpusURL, "utf8"));
  assert.equal(corpus.schema_version, 1);
  assert.equal(corpus.canonical_cases.length, 30);
  assert.equal(corpus.rejection_cases.length, 59);
  for (const vector of corpus.canonical_cases) await context.test(`${vector.name} canonical`, () => {
    const codec = sharedCodecs[vector.type];
    assert.equal(codec[1](codec[0](vector.input)), vector.expected);
  });
  for (const vector of corpus.rejection_cases) await context.test(`${vector.name} rejection`, () => {
    assert.throws(() => sharedCodecs[vector.type][0](vector.input));
  });
});

test("published scalar-Unicode vectors match TypeScript codecs recursively", async (context) => {
  const corpus = JSON.parse(await readFile(unicodeScalarCorpusURL, "utf8"));
  assert.equal(corpus.schema_version, 1);
  assert.equal(corpus.canonical_cases.length, 2);
  assert.equal(corpus.rejection_cases.length, 9);
  for (const vector of corpus.canonical_cases) await context.test(`${vector.name} canonical`, () => {
    const codec = sharedCodecs[vector.type];
    assert.equal(codec[1](codec[0](vector.input)), vector.expected);
  });
  for (const vector of corpus.rejection_cases) await context.test(`${vector.name} rejection`, () => {
    assert.throws(() => sharedCodecs[vector.type][0](vector.input));
  });
});

test("typed-programmatic normalized roots and View sources enforce semantic closure", async () => {
  const project = structuredClone((await readFixture("compile-success.json")).payload.normalized_artifact.project);
  const pack = structuredClone((await readFixture("compile-success-pack.json")).payload.normalized_artifact.pack);
  for (const project_address of ["ldl:pack:publisher:pack", "ldl:project:p:view:v"]) {
    assert.throws(() => encodeNormalizedProjectArtifact({...project, project_address}), TypeError);
  }
  for (const pack_address of ["ldl:project:p", "ldl:pack:publisher:pack:view:v"]) {
    assert.throws(() => encodeNormalizedPackArtifact({...pack, pack_address}), TypeError);
  }

  const validColumns = [
    {kind: "field", field: "tags"},
    {kind: "attribute", column_addresses: ["ldl:project:p:entity-type:t:column:c"]},
    {kind: "relation_endpoint", endpoint: "from", field: "display_name"},
    {kind: "derived_count", direction: "both", relation_type_addresses: ["ldl:project:p:relation-type:r"]},
    {kind: "state", field_path: "system.updated_at"},
  ];
  for (const value of validColumns) assert.doesNotThrow(() => encodeViewTableColumnSource(value));
  const invalidColumns = [
    {kind: "field"},
    {kind: "attribute", column_addresses: [], field: "id"},
    {kind: "attribute", column_addresses: ["ldl:project:p"]},
    {kind: "attribute", column_addresses: ["ldl:project:p:entity-type:t:column:c", "ldl:project:p:entity-type:t:column:c"]},
    {kind: "attribute", column_addresses: ["ldl:pack:publisher:shared-pack:entity-type:t:column:c", "ldl:project:p:entity-type:t:column:c"]},
    {kind: "relation_endpoint", endpoint: "from", field: "description"},
    {kind: "derived_count"},
    {kind: "derived_count", direction: "both", relation_type_addresses: ["ldl:project:p:entity:e"]},
    {kind: "derived_count", direction: "both", relation_type_addresses: ["ldl:project:p:relation-type:r", "ldl:project:p:relation-type:r"]},
    {kind: "state"},
    {kind: "state", field_path: "review.status"},
  ];
  for (const value of invalidColumns) assert.throws(() => encodeViewTableColumnSource(value), TypeError);

  const query_address = "ldl:project:p:query:q";
  const argumentsWithValue = {"ldl:project:p:query:q:parameter:x": {kind: "string", string_value: "x"}};
  for (const value of [
    {kind: "query", query_address, arguments: {}},
    {kind: "diff", before: "base", after: "head", arguments: {}},
    {kind: "diff", before: "base", after: "head", query_address, arguments: {}},
    {kind: "diff", before: "base", after: "head", query_address, arguments: argumentsWithValue},
  ]) assert.doesNotThrow(() => encodeViewRecipeSource(value));
  for (const value of [
    {kind: "query", query_address: "ldl:project:p", arguments: {}},
    {kind: "query", query_address, arguments: {"not-an-address": {kind: "string", string_value: "x"}}},
    {kind: "diff", after: "head", arguments: {}},
    {kind: "diff", before: "base", arguments: {}},
    {kind: "diff", before: "", after: "head", arguments: {}},
    {kind: "diff", before: "base", after: "", arguments: {}},
    {kind: "diff", before: "same", after: "same", arguments: {}},
    {kind: "diff", before: "base", after: "head", arguments: argumentsWithValue},
  ]) assert.throws(() => encodeViewRecipeSource(value), TypeError);
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
      () => { const shared = {kind: "state", field_path: "system.updated_at", operator: "exists"}; return {kind: "all", children: [shared, shared]}; },
    ],
  ];
  for (const [name, encode, self, mutual, alias] of cases) {
    expectCycleError(() => encode(self()), `${name} self cycle`);
    expectCycleError(() => encode(mutual()), `${name} mutual cycle`);
    assert.doesNotThrow(() => encode(alias()), `${name} acyclic alias`);
  }
});

test("every recursive public predicate is total and enforces exact wire graph bounds", () => {
  const jsonAtLimit = () => {
    let value = "leaf";
    for (let depth = 0; depth < maxWireJSONDepth; depth++) value = [value];
    return value;
  };
  const diagnosticAtLimit = () => {
    let value = {kind: "array", array_value: []};
    for (let depth = 0; depth < 63; depth++) value = {kind: "array", array_value: [value]};
    return value;
  };
  const predicateAtLimit = (leaf) => {
    let value = leaf;
    for (let depth = 1; depth < maxWireJSONDepth; depth++) value = {kind: "not", child: value};
    return value;
  };
  const cases = [
    {
      name: "JsonValue",
      predicate: isJsonValue,
      encode: encodeJsonValue,
      self: () => { const value = {}; value.self = value; return value; },
      mutual: () => { const left = {}; const right = {left}; left.right = right; return left; },
      alias: () => { const shared = {value: "shared"}; return {left: shared, right: shared}; },
      atLimit: jsonAtLimit,
      tooDeep: () => [jsonAtLimit()],
    },
    {
      name: "DiagnosticArgumentValue",
      predicate: isDiagnosticArgumentValue,
      encode: encodeDiagnosticArgumentValue,
      self: () => { const value = {kind: "array", array_value: []}; value.array_value.push(value); return value; },
      mutual: () => { const left = {kind: "array", array_value: []}; const right = {kind: "array", array_value: [left]}; left.array_value.push(right); return left; },
      alias: () => { const shared = {kind: "string", string_value: "shared"}; return {kind: "array", array_value: [shared, shared]}; },
      atLimit: diagnosticAtLimit,
      tooDeep: () => { let value = {kind: "string", string_value: "leaf"}; for (let depth = 0; depth < 64; depth++) value = {kind: "array", array_value: [value]}; return value; },
    },
    {
      name: "RecipePredicate",
      predicate: isRecipePredicate,
      encode: encodeRecipePredicate,
      self: () => { const value = {kind: "not"}; value.child = value; return value; },
      mutual: () => { const left = {kind: "not"}; const right = {kind: "not", child: left}; left.child = right; return left; },
      alias: () => { const shared = {kind: "field", field: "name", operator: "exists"}; return {kind: "all", children: [shared, shared]}; },
      atLimit: () => predicateAtLimit({kind: "field", field: "name", operator: "exists"}),
      tooDeep: () => ({kind: "not", child: predicateAtLimit({kind: "field", field: "name", operator: "exists"})}),
    },
    {
      name: "RecipeRowPredicate",
      predicate: isRecipeRowPredicate,
      encode: encodeRecipeRowPredicate,
      self: () => { const value = {kind: "not"}; value.child = value; return value; },
      mutual: () => { const left = {kind: "not"}; const right = {kind: "not", child: left}; left.child = right; return left; },
      alias: () => { const shared = {kind: "state", field_path: "system.updated_at", operator: "exists"}; return {kind: "all", children: [shared, shared]}; },
      atLimit: () => predicateAtLimit({kind: "state", field_path: "system.updated_at", operator: "exists"}),
      tooDeep: () => ({kind: "not", child: predicateAtLimit({kind: "state", field_path: "system.updated_at", operator: "exists"})}),
    },
  ];

  for (const {name, predicate, encode, self, mutual, alias, atLimit, tooDeep} of cases) {
    for (const [cycleName, cycle] of [["self", self], ["mutual", mutual]]) {
      const value = cycle();
      assert.doesNotThrow(() => predicate(value), `${name} ${cycleName} cycle predicate threw`);
      assert.equal(predicate(value), false, `${name} ${cycleName} cycle`);
      expectCycleError(() => encode(value), `${name} ${cycleName} cycle encoder`);
    }
    const aliased = alias();
    assert.equal(predicate(aliased), true, `${name} shared alias`);
    assert.doesNotThrow(() => encode(aliased), `${name} shared alias encoder`);

    const exact = atLimit();
    assert.equal(containerDepth(exact), maxWireJSONDepth, `${name} exact depth`);
    assert.equal(predicate(exact), true, `${name} exact depth predicate`);
    assert.doesNotThrow(() => encode(exact), `${name} exact depth encoder`);

    const excessive = tooDeep();
    assert.equal(containerDepth(excessive), maxWireJSONDepth + 1, `${name} excessive depth`);
    assert.equal(predicate(excessive), false, `${name} excessive depth predicate`);
    expectDepthError(() => encode(excessive), `${name} excessive depth encoder`);
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
    ["RecipeRowPredicate", encodeRecipeRowPredicate, {kind: "state", field_path: "system.updated_at", operator: "exists"}],
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
  assert.equal(isCompileRequestEnvelope(aliased), true);
  assert.doesNotThrow(() => encodeCompileRequestEnvelope(aliased));

  const cyclic = await readFixture("compile-request.json");
  const self = {};
  self.self = self;
  cyclic.extensions = {"example.cycle": self};
  assert.equal(isCompileRequestEnvelope(cyclic), false);
  expectCycleError(() => encodeCompileRequestEnvelope(cyclic));

  const atLimit = await readFixture("compile-request.json");
  let extension = "leaf";
  for (let depth = 0; depth < maxWireJSONDepth - 2; depth++) extension = [extension];
  atLimit.extensions = {"example.depth": extension};
  assert.equal(containerDepth(atLimit), maxWireJSONDepth);
  assert.equal(isCompileRequestEnvelope(atLimit), true);
  assert.doesNotThrow(() => encodeCompileRequestEnvelope(atLimit));

  const tooDeep = await readFixture("compile-request.json");
  tooDeep.extensions = {"example.depth": [extension]};
  assert.equal(containerDepth(tooDeep), maxWireJSONDepth + 1);
  assert.equal(isCompileRequestEnvelope(tooDeep), false);
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
