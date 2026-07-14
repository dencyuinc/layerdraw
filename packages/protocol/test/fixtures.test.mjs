// SPDX-License-Identifier: Apache-2.0

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  decodeCompileRequestEnvelope,
  decodeCompileResponseEnvelope,
  decodeHandshakeResponseEnvelope,
  encodeCompileRequestEnvelope,
  encodeCompileResponseEnvelope,
  encodeHandshakeResponseEnvelope,
  isCompileRequestEnvelope,
  isCompileResponseEnvelope,
  isHandshakeResponseEnvelope,
} from "../dist/engine.gen.js";
import {
  decodeBlobRef,
  decodeCanonicalInt64,
  decodeCanonicalNonNegativeInt64,
  decodeCanonicalUint64,
  decodeDigest,
  decodeJsonValue,
  decodeOperationCapability,
  decodeRfc3339Time,
  encodeBlobRef,
  encodeCanonicalInt64,
  encodeCanonicalNonNegativeInt64,
  encodeDigest,
  encodeJsonValue,
  encodeOperationCapability,
  encodeRfc3339Time,
  maxWireJSONBytes,
  maxWireJSONDepth,
} from "../dist/common.gen.js";
import {
  decodeCompiledExportRecipeDocument,
  decodeCompiledQueryRecipeDocument,
  decodeCompiledViewRecipeDocument,
  decodeDiagnostic,
  decodeSearchField,
  encodeCompiledExportRecipeDocument,
  encodeCompiledQueryRecipeDocument,
  encodeCompiledViewRecipeDocument,
  encodeDiagnostic,
  encodeSearchField,
} from "../dist/semantic.gen.js";

const fixtureRoot = new URL("../../../schemas/fixtures/engine/", import.meta.url);
const commonFixtureRoot = new URL("../../../schemas/fixtures/common/", import.meta.url);
const conformanceCorpusURL = new URL("../../../schemas/fixtures/conformance/v1.json", import.meta.url);
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

const sharedCodecs = {
  CanonicalInt64: [decodeCanonicalInt64, encodeCanonicalInt64],
  CanonicalNonNegativeInt64: [decodeCanonicalNonNegativeInt64, encodeCanonicalNonNegativeInt64],
  CompiledExportRecipeDocument: [decodeCompiledExportRecipeDocument, encodeCompiledExportRecipeDocument],
  CompiledQueryRecipeDocument: [decodeCompiledQueryRecipeDocument, encodeCompiledQueryRecipeDocument],
  CompiledViewRecipeDocument: [decodeCompiledViewRecipeDocument, encodeCompiledViewRecipeDocument],
  Diagnostic: [decodeDiagnostic, encodeDiagnostic],
  Digest: [decodeDigest, encodeDigest],
  JsonValue: [decodeJsonValue, encodeJsonValue],
  OperationCapability: [decodeOperationCapability, encodeOperationCapability],
  Rfc3339Time: [decodeRfc3339Time, encodeRfc3339Time],
  SearchField: [decodeSearchField, encodeSearchField],
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

test("shared recursive JsonValue limits are exact", async () => {
  const corpus = await readCorpus();
  const deep = "[".repeat(corpus.max_json_depth) + '"x"' + "]".repeat(corpus.max_json_depth);
  assert.equal(encodeJsonValue(decodeJsonValue(deep)), deep);
  assert.throws(() => decodeJsonValue("[" + deep + "]"));
  const maximum = '"' + "a".repeat(corpus.max_json_bytes - 2) + '"';
  assert.equal(encodeJsonValue(decodeJsonValue(maximum)), maximum);
  assert.throws(() => decodeJsonValue('"' + "a".repeat(corpus.max_json_bytes - 1) + '"'));
});

test("shared outcome and tagged-union mutations are isolated", async (context) => {
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
      default:
        assert.fail(`unknown request mutation ${vector.mutation}`);
    }
    assert.throws(() => decodeCompileRequestEnvelope(JSON.stringify(value)));
  });
});
