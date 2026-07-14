// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import {readFile} from "node:fs/promises";
import test from "node:test";

import {assertCompileParityResponse, encode} from "./shared/real-engine.mjs";

const artifactManifest = JSON.parse(await readFile(new URL("../dist/engine-wasm.manifest.json", import.meta.url), "utf8"));
const corpus = JSON.parse(await readFile(new URL("../../../tests/conformance/testdata/engine_compile_parity_v1.json", import.meta.url), "utf8"));

function bytes(value) {
  const buffer = Buffer.from(value, "base64");
  return buffer.buffer.slice(buffer.byteOffset, buffer.byteOffset + buffer.byteLength);
}

function validResponse(testCase) {
  const semantics = structuredClone(testCase.expected.response);
  semantics.engine_release = artifactManifest.build.release_version;
  return {
    control: encode(semantics),
    blobs: testCase.expected.blobs.map((blob) => ({blob_id: blob.blob_id, bytes: bytes(blob.bytes_base64)})),
  };
}

function findRef(value, blobID) {
  if (Array.isArray(value)) {
    for (const item of value) {
      const found = findRef(item, blobID);
      if (found !== undefined) return found;
    }
    return undefined;
  }
  if (value === null || typeof value !== "object") return undefined;
  if (value.blob_id === blobID) return value;
  for (const item of Object.values(value)) {
    const found = findRef(item, blobID);
    if (found !== undefined) return found;
  }
  return undefined;
}

test("the shared parity oracle rejects forged success semantics, metadata, ordering, and bytes", async (context) => {
  const testCase = corpus.cases[0];
  await assertCompileParityResponse(validResponse(testCase), testCase, artifactManifest.build.release_version);
  const cases = [
    ["definition hash", (response) => { const semantic = JSON.parse(new TextDecoder().decode(response.control)); semantic.payload.definition_hash = `sha256:${"0".repeat(64)}`; response.control = encode(semantic); }],
    ["subject semantic hash", (response) => { const semantic = JSON.parse(new TextDecoder().decode(response.control)); semantic.payload.subject_semantic_hashes[0].hash = `sha256:${"0".repeat(64)}`; response.control = encode(semantic); }],
    ["diagnostics", (response) => { const semantic = JSON.parse(new TextDecoder().decode(response.control)); semantic.diagnostics = [{code: "bogus"}]; response.control = encode(semantic); }],
    ["ordered blob IDs", (response) => { response.blobs.reverse(); }],
    ["media type", (response) => { const semantic = JSON.parse(new TextDecoder().decode(response.control)); findRef(semantic, response.blobs[0].blob_id).media_type = "application/bogus"; response.control = encode(semantic); }],
    ["size", (response) => { const semantic = JSON.parse(new TextDecoder().decode(response.control)); findRef(semantic, response.blobs[0].blob_id).size = "1"; response.control = encode(semantic); }],
    ["artifact digest", (response) => { const semantic = JSON.parse(new TextDecoder().decode(response.control)); findRef(semantic, response.blobs[0].blob_id).digest = `sha256:${"0".repeat(64)}`; response.control = encode(semantic); }],
    ["output bytes", (response) => { new Uint8Array(response.blobs[0].bytes)[0] ^= 0xff; }],
  ];
  for (const [name, mutate] of cases) await context.test(name, async () => {
    const response = validResponse(testCase);
    mutate(response);
    await assert.rejects(assertCompileParityResponse(response, testCase, artifactManifest.build.release_version), /Go|differs/);
  });
});
