// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {readFile, writeFile} from "node:fs/promises";
import {fileURLToPath} from "node:url";
import {parentPort, workerData} from "node:worker_threads";

import {createVerifiedWasmEndpoint} from "../../dist/artifact.js";
import {installEngineWorker} from "../../dist/worker-runtime.js";

if (parentPort === null) throw new Error("node worker adapter requires parentPort");

const listeners = new Map([["message", new Set()], ["messageerror", new Set()]]);
const scope = {
  postMessage(message, transfer) {
    parentPort.postMessage(message, [...transfer]);
  },
  addEventListener(type, listener) {
    listeners.get(type).add(listener);
  },
  removeEventListener(type, listener) {
    listeners.get(type).delete(listener);
  },
  close() {
    parentPort.close();
  },
};

parentPort.on("message", (data) => {
  if (data?.kind === "__layerdraw_test_crash") throw new Error("test-only process crash after real artifact initialization");
  for (const listener of listeners.get("message")) listener({data});
});
parentPort.on("messageerror", () => {
  for (const listener of listeners.get("messageerror")) listener();
});

const artifactBaseURL = typeof workerData?.artifactBaseURL === "string" ? workerData.artifactBaseURL : new URL("../../dist/", import.meta.url).href;
installEngineWorker(scope, (init) => createVerifiedWasmEndpoint(init, {
  artifactBaseURL,
  async loadBytes(url) {
    const bytes = await readFile(fileURLToPath(url));
    const snapshot = bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength);
    if (workerData?.replaceWasmExecAfterRead === true && new URL(url).pathname.endsWith("/wasm_exec.js")) {
      await writeFile(fileURLToPath(url), "globalThis.__layerdrawUnverifiedWasmExecRan = true;\n");
    }
    return snapshot;
  },
}), {checkEnvironment: () => true});
