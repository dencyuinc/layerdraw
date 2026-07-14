// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import {chmod, cp, mkdtemp, readFile, rm} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {pathToFileURL} from "node:url";
import test from "node:test";

import {EngineWorkerTransportError} from "../dist/index.js";
import {createEngineWorkerTransportWithFactory} from "../dist/host.js";
import {
  encode,
  handshakeAndCompileProjectAndPack,
  handshakeControl,
  performRequest,
  projectCompileCase,
  releaseManifestDigest,
  sha256,
} from "./shared/real-engine.mjs";
import {NodeWorkerAdapter} from "./node-worker-adapter.mjs";

const workerModuleURL = new URL("./fixtures/node-worker-entry.mjs", import.meta.url).href;
const manifestBytes = await readFile(new URL("../dist/engine-wasm.manifest.json", import.meta.url));
const manifestBuffer = manifestBytes.buffer.slice(manifestBytes.byteOffset, manifestBytes.byteOffset + manifestBytes.byteLength);
const artifactManifestDigest = await sha256(manifestBuffer);
const artifactManifest = JSON.parse(manifestBytes);
const expectedLimitKeys = [
  "max_blob_id_bytes", "max_buffers", "max_control_bytes", "max_control_depth",
  "max_input_blob_bytes", "max_input_total_bytes", "max_output_blob_bytes",
  "max_output_total_bytes", "max_response_publish_bytes",
];

function createEndpoint(endpointGeneration) {
  let adapter;
  const transport = createEngineWorkerTransportWithFactory({
    endpointGeneration,
    expectedArtifactManifestDigest: artifactManifestDigest,
    releaseManifestDigest,
    disposeTimeoutMilliseconds: 2_000,
  }, workerModuleURL, (url, options) => {
    adapter = new NodeWorkerAdapter(url, options);
    return adapter;
  });
  return {transport, get adapter() { return adapter; }};
}

function isFailure(code) {
  return (error) => error instanceof EngineWorkerTransportError && error.failure.code === code;
}

test("real Node worker_threads owns the packaged Go/WASM lifecycle", {timeout: 120_000}, async () => {
  const exits = [];
  const first = createEndpoint("node-real-generation-1");
  exits.push(first.adapter.exited);
  const limits = await first.transport.ready;
  assert.deepEqual(Object.keys(limits).sort(), expectedLimitKeys);

  const staleControl = encode({});
  const staleFailure = first.adapter.nextMessage((message) => message.kind === "transport_failure" && message.exchange_id === "node-stale-exchange");
  first.adapter.postMessage({
    worker_protocol: "layerdraw.engine_worker",
    worker_protocol_version: 1,
    kind: "request",
    endpoint_generation: "node-stale-generation",
    exchange_id: "node-stale-exchange",
    control: staleControl,
    blobs: [],
  }, [staleControl]);
  assert.equal(staleControl.byteLength, 0);
  assert.deepEqual((await staleFailure).failure, {code: "engine.worker.stale_generation", phase: "lifecycle", retryable: true});

  const firstEndpointID = await handshakeAndCompileProjectAndPack(first.transport, artifactManifest.protocol.schema_digest, "node-first");
  const firstDispose = first.transport.dispose();
  assert.equal(firstDispose, first.transport.dispose());
  await firstDispose;

  const cancelled = createEndpoint("node-real-generation-cancelled");
  exits.push(cancelled.adapter.exited);
  await cancelled.transport.ready;
  const slowControl = new ArrayBuffer(8_388_608);
  new Uint8Array(slowControl).fill(0x20);
  const slow = cancelled.transport.request({exchangeID: "node-cancelled-exchange", control: slowControl, blobs: []});
  assert.equal(slowControl.byteLength, 0);
  await slow.accepted;
  cancelled.transport.terminate();
  await assert.rejects(slow.response, isFailure("engine.worker.terminated_by_caller"));
  await cancelled.transport.dispose();

  const replacement = createEndpoint("node-real-generation-replacement");
  exits.push(replacement.adapter.exited);
  await replacement.transport.ready;
  const replacementEndpointID = await handshakeAndCompileProjectAndPack(replacement.transport, artifactManifest.protocol.schema_digest, "node-replacement");
  assert.notEqual(replacementEndpointID, firstEndpointID);
  await replacement.transport.dispose();

  const crashed = createEndpoint("node-real-generation-crashed");
  exits.push(crashed.adapter.exited);
  await crashed.transport.ready;
  crashed.adapter.crashForTest();
  await crashed.adapter.exited;
  assert.throws(
    () => crashed.transport.request({exchangeID: "after-crash", control: encode({}), blobs: []}),
    isFailure("engine.worker.crashed"),
  );
  await crashed.transport.dispose();

  await Promise.all(exits);
});

test("Node executes the verified wasm_exec byte snapshot after its source path changes", {timeout: 120_000}, async () => {
  const temporary = await mkdtemp(join(tmpdir(), "layerdraw-engine-wasm-snapshot-"));
  const artifactRoot = join(temporary, "dist");
  await cp(new URL("../dist/", import.meta.url), artifactRoot, {recursive: true});
  await chmod(join(artifactRoot, "wasm_exec.js"), 0o644);
  let adapter;
  try {
    const transport = createEngineWorkerTransportWithFactory({
      endpointGeneration: "node-verified-snapshot-race",
      expectedArtifactManifestDigest: artifactManifestDigest,
      releaseManifestDigest,
      disposeTimeoutMilliseconds: 2_000,
    }, workerModuleURL, (url, options) => {
      adapter = new NodeWorkerAdapter(url, options, {
        artifactBaseURL: `${pathToFileURL(artifactRoot).href}/`,
        replaceWasmExecAfterRead: true,
      });
      return adapter;
    });
    await transport.ready;
    assert.match(await readFile(join(artifactRoot, "wasm_exec.js"), "utf8"), /__layerdrawUnverifiedWasmExecRan/);
    assert.equal(await handshakeAndCompileProjectAndPack(transport, artifactManifest.protocol.schema_digest, "node-snapshot-race").then(() => true), true);
    await transport.dispose();
    await adapter.exited;
  } finally {
    adapter?.terminate();
    await rm(temporary, {recursive: true, force: true});
  }
});

test("Go bridge rejects an outer attachment size mismatch before attachment acquisition", {timeout: 120_000}, async () => {
  const endpoint = createEndpoint("node-attachment-bind-mismatch");
  await endpoint.transport.ready;
  await performRequest(endpoint.transport, "node-bind-handshake-exchange", {
    control: handshakeControl(artifactManifest.protocol.schema_digest, "node-bind-handshake-request"),
    blobs: [],
  });
  const input = await projectCompileCase("node-bind-project-request");
  const wrongBytes = new ArrayBuffer(1);
  const exchange = endpoint.transport.request({
    exchangeID: "node-bind-project-exchange",
    control: input.control,
    blobs: [{blob_id: input.blobs[0].blob_id, bytes: wrongBytes}],
  });
  assert.equal(wrongBytes.byteLength, 0);
  await exchange.accepted;
  await assert.rejects(exchange.response, isFailure("engine.worker.transfer_failed"));
  await endpoint.transport.dispose();
  await endpoint.adapter.exited;
});
