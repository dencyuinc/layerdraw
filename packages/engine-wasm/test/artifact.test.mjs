// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import {readFile} from "node:fs/promises";
import test from "node:test";

import {createVerifiedWasmEndpoint} from "../dist/artifact.js";
import {sha256} from "./shared/real-engine.mjs";

const canonicalManifest = JSON.parse(await readFile(new URL("../dist/engine-wasm.manifest.json", import.meta.url), "utf8"));

function arrayBuffer(value) {
  const bytes = Buffer.from(value);
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength);
}

async function assertRejectedBeforeArtifactReads(mutate) {
  const manifest = structuredClone(canonicalManifest);
  mutate(manifest);
  const bytes = arrayBuffer(`${JSON.stringify(manifest)}\n`);
  const expectedArtifactManifestDigest = await sha256(bytes);
  let reads = 0;
  await assert.rejects(createVerifiedWasmEndpoint({
    endpointGeneration: "metadata-negative-test",
    expectedArtifactManifestDigest,
    releaseManifestDigest: `sha256:${"5".repeat(64)}`,
  }, {
    artifactBaseURL: "https://artifact.invalid/dist/",
    async loadBytes(url) {
      reads += 1;
      if (url.endsWith("/engine-wasm.manifest.json")) return bytes.slice(0);
      throw new Error("manifest validation read an artifact file");
    },
  }), (error) => error?.failure?.code === "engine.worker.artifact_mismatch");
  assert.equal(reads, 1, "falsified metadata reached artifact file loading");
}

test("the TypeScript loader rejects every closed browser-contract and license field mutation", async (context) => {
  const cases = [
    ["module dedicated Worker", (value) => { value.browser_contract.module_dedicated_worker = false; }],
    ["SharedArrayBuffer", (value) => { value.browser_contract.shared_array_buffer = true; }],
    ["WASM threads", (value) => { value.browser_contract.wasm_threads = true; }],
    ["product license", (value) => { value.licenses.product = "MIT"; }],
    ["runtime license", (value) => { value.licenses.runtime_support = "MIT"; }],
    ["SBOM path", (value) => { value.licenses.sbom = "other.cdx.json"; }],
  ];
  for (let index = 0; index < canonicalManifest.browser_contract.required_primitives.length; index += 1) {
    const primitive = canonicalManifest.browser_contract.required_primitives[index];
    cases.push([`required primitive ${primitive}`, (value) => { value.browser_contract.required_primitives[index] = "falsified"; }]);
  }
  for (const [name, mutate] of cases) await context.test(name, () => assertRejectedBeforeArtifactReads(mutate));
});
