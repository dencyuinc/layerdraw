// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {createHash} from "node:crypto";
import {readFile} from "node:fs/promises";
import {resolve, sep} from "node:path";
import {pathToFileURL} from "node:url";
import {parentPort, workerData} from "node:worker_threads";

if (parentPort === null) throw new Error("parent port is required");
const artifactDirectory = resolve(workerData.artifactDirectory);
let bridge;
let generation;

const digest = (value) => `sha256:${createHash("sha256").update(value).digest("hex")}`;

async function loadVerifiedArtifact(expectedManifestDigest) {
  const manifestPath = resolve(artifactDirectory, "engine-wasm.manifest.json");
  const manifestBytes = await readFile(manifestPath);
  if (digest(manifestBytes) !== expectedManifestDigest) throw new Error("artifact manifest mismatch");
  const manifest = JSON.parse(manifestBytes);
  for (const file of manifest.files) {
    const path = resolve(artifactDirectory, file.path);
    if (!path.startsWith(`${artifactDirectory}${sep}`)) throw new Error("unsafe artifact path");
    const bytes = await readFile(path);
    if (bytes.byteLength !== file.size || digest(bytes) !== file.digest) throw new Error("artifact file mismatch");
  }
  await import(pathToFileURL(resolve(artifactDirectory, "wasm_exec.js")).href);
  const go = new globalThis.Go();
  const bytes = await readFile(resolve(artifactDirectory, "layerdraw-engine.wasm"));
  const {instance} = await WebAssembly.instantiate(bytes, go.importObject);
  void go.run(instance);
  for (let attempt = 0; attempt < 100 && !globalThis.__layerdrawEngineWasmV1; attempt += 1) {
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 0));
  }
  if (!globalThis.__layerdrawEngineWasmV1) throw new Error("Go WASM bridge did not register");
  return globalThis.__layerdrawEngineWasmV1;
}

parentPort.on("message", (message) => {
  if (message.kind === "init") {
    void loadVerifiedArtifact(message.expected_artifact_manifest_digest).then((value) => {
      bridge = value;
      generation = message.endpoint_generation;
      const initialized = bridge.initialize(generation, message.release_manifest_digest);
      parentPort.postMessage({kind: "ready", initialized, artifact_manifest_digest: message.expected_artifact_manifest_digest});
    }, () => parentPort.postMessage({kind: "fatal"}));
    return;
  }
  if (message.kind === "request") {
    parentPort.postMessage({kind: "accepted", exchange_id: message.exchange_id});
    const result = bridge.request(message.endpoint_generation, message.control, message.blob_ids, message.blobs);
    const transfer = result.ok ? [result.control, ...result.blobs] : [];
    parentPort.postMessage({kind: "response", exchange_id: message.exchange_id, result}, transfer);
    return;
  }
  if (message.kind === "dispose") {
    bridge.dispose(message.endpoint_generation ?? generation);
    parentPort.postMessage({kind: "disposed"});
  }
});
