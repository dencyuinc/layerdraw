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
  decodeCanonicalUint64,
  decodeDigest,
  decodeJsonValue,
  encodeBlobRef,
  encodeJsonValue,
} from "../dist/common.gen.js";

const fixtureRoot = new URL("../../../schemas/fixtures/engine/", import.meta.url);
const commonFixtureRoot = new URL("../../../schemas/fixtures/common/", import.meta.url);

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
  assert.deepEqual(JSON.parse(JSON.stringify(extended)), extended);
});

test("shared common fixtures have byte-identical canonical encoding", async () => {
  const blobText = (await readFile(new URL("blob-ref-canonical.json", commonFixtureRoot), "utf8")).trim();
  assert.equal(encodeBlobRef(decodeBlobRef(blobText)), blobText);
  const jsonText = (await readFile(new URL("json-value-canonical.json", commonFixtureRoot), "utf8")).trim();
  assert.equal(encodeJsonValue(decodeJsonValue(jsonText)), jsonText);
});

test("scalar, outcome, and normalized artifact invariants are enforced", async () => {
  for (const invalid of ["-0", "01", "18446744073709551616"]) {
    assert.throws(() => decodeCanonicalUint64(JSON.stringify(invalid)));
  }
  for (const invalid of [
    "sha256:ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
    "md5:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  ]) assert.throws(() => decodeDigest(JSON.stringify(invalid)));

  const success = await readFixture("compile-success.json");
  assert.equal(isCompileResponseEnvelope({...success, failure: {category: "invariant", code: "bad", message: "bad", retryable: false}}), false);
  const normalized = success.payload.normalized_artifact;
  assert.equal(isCompileResponseEnvelope({...success, payload: {...success.payload, normalized_artifact: {...normalized, pack: normalized.project}}}), false);
});
