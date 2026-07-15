// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import test from "node:test";

import {EngineWorkerTransportError, createEngineWorkerTransport as createProductionTransport} from "../dist/index.js";
import {createEngineWorkerTransportWithFactory} from "../dist/host.js";
import {artifactDigest, bytes, generation, LinkedWorker, linkedWorkerHarness, makeEchoEndpoint, nextTask, releaseDigest, transportLimits} from "./helpers.mjs";

function options(harness, overrides = {}) {
  return {
    workerModuleURL: "https://example.invalid/engine-worker.js",
    endpointGeneration: generation,
    expectedArtifactManifestDigest: artifactDigest,
    releaseManifestDigest: releaseDigest,
    workerFactory: harness.factory,
    disposeTimeoutMilliseconds: 50,
    ...overrides,
  };
}

function createEngineWorkerTransport(specification) {
  const {workerModuleURL, workerFactory, ...transportOptions} = specification;
  const factory = workerFactory ?? ((url, workerOptions) => new globalThis.Worker(url, workerOptions));
  return createEngineWorkerTransportWithFactory(transportOptions, workerModuleURL, factory);
}

function isCode(code) {
  return (error) => error instanceof EngineWorkerTransportError && error.failure.code === code;
}

test("host sends exact transfers, observes immediate detachment, and returns owned response buffers", async () => {
  const harness = linkedWorkerHarness();
  const transport = createEngineWorkerTransport(options(harness));
  const limits = await transport.ready;
  assert.equal(limits.max_buffers, 2_048);

  const control = bytes(1, 2, 3);
  const blobBytes = bytes(4, 5);
  const exchange = transport.request({exchangeID: "exchange-1", control, blobs: [{blob_id: "a", bytes: blobBytes}]});
  assert.equal(control.byteLength, 0);
  assert.equal(blobBytes.byteLength, 0);
  await exchange.accepted;
  const response = await exchange.response;
  assert.deepEqual([...new Uint8Array(response.control)], [1, 2, 3]);
  assert.deepEqual([...new Uint8Array(response.blobs[0].bytes)], [4, 5]);
  await transport.dispose();
  assert.equal(harness.worker.terminated, true);
});

test("host enforces single flight and does not claim same-worker hard cancellation", async () => {
  const harness = linkedWorkerHarness();
  const transport = createEngineWorkerTransport(options(harness));
  await transport.ready;
  const first = transport.request({exchangeID: "exchange-1", control: bytes(1), blobs: []});
  assert.throws(
    () => transport.request({exchangeID: "exchange-2", control: bytes(2), blobs: []}),
    isCode("engine.worker.malformed_message"),
  );
  await first.accepted;
  transport.terminate();
  await assert.rejects(first.response, isCode("engine.worker.terminated_by_caller"));
  assert.throws(() => transport.request({exchangeID: "exchange-3", control: bytes(3), blobs: []}), isCode("engine.worker.terminated_by_caller"));
  await nextTask();
});

test("crash invalidates the endpoint while explicit replacement gets a fresh generation", async () => {
  const crashedHarness = linkedWorkerHarness();
  const crashed = createEngineWorkerTransport(options(crashedHarness));
  await crashed.ready;
  const exchange = crashed.request({exchangeID: "exchange-1", control: bytes(255), blobs: []});
  await exchange.accepted;
  await assert.rejects(exchange.response, isCode("engine.worker.crashed"));

  const replacementHarness = linkedWorkerHarness();
  const replacement = createEngineWorkerTransport(options(replacementHarness, {endpointGeneration: "test-generation-2"}));
  await replacement.ready;
  const retried = replacement.request({exchangeID: "exchange-2", control: bytes(7), blobs: []});
  await retried.accepted;
  assert.deepEqual([...new Uint8Array((await retried.response).control)], [7]);
  await replacement.dispose();
});

test("dispose marks terminal first, rejects pending work, and is idempotent", async () => {
  const harness = linkedWorkerHarness();
  const transport = createEngineWorkerTransport(options(harness));
  await transport.ready;
  const exchange = transport.request({exchangeID: "exchange-1", control: bytes(1), blobs: []});
  const responseRejection = assert.rejects(exchange.response, isCode("engine.worker.disposed"));
  const acceptedRejection = assert.rejects(exchange.accepted, isCode("engine.worker.disposed"));
  const first = transport.dispose();
  const second = transport.dispose();
  assert.equal(first, second);
  await Promise.all([first, responseRejection, acceptedRejection]);
  assert.throws(() => transport.request({exchangeID: "exchange-2", control: bytes(2), blobs: []}), isCode("engine.worker.disposed"));
});

test("stale output is discarded and malformed current-generation output crashes safely", async () => {
  const harness = linkedWorkerHarness();
  const transport = createEngineWorkerTransport(options(harness));
  await transport.ready;
  harness.worker.emitMessage({
    worker_protocol: "layerdraw.engine_worker",
    worker_protocol_version: 1,
    kind: "accepted",
    endpoint_generation: "stale-generation",
    exchange_id: "unknown",
  });
  await nextTask();
  assert.throws(() => transport.request({exchangeID: "bad", control: new Uint8Array(1), blobs: []}), isCode("engine.worker.malformed_message"));
  let accessorCalled = false;
  const accessor = {exchangeID: "bad", blobs: []};
  Object.defineProperty(accessor, "control", {enumerable: true, get() { accessorCalled = true; return bytes(1); }});
  assert.throws(() => transport.request(accessor), isCode("engine.worker.malformed_message"));
  assert.equal(accessorCalled, false);
  harness.worker.emitMessage({unknown: true});
  assert.throws(() => transport.request({exchangeID: "exchange-1", control: bytes(1), blobs: []}), isCode("engine.worker.crashed"));
});

test("initialization mismatch, worker construction, fatal events, and post failures are redacted", async () => {
  const mismatchHarness = linkedWorkerHarness(() => makeEchoEndpoint({artifactManifestDigest: `sha256:${"c".repeat(64)}`}));
  const mismatch = createEngineWorkerTransport(options(mismatchHarness));
  await assert.rejects(mismatch.ready, isCode("engine.worker.artifact_mismatch"));

  assert.throws(() => createEngineWorkerTransport(options({factory: () => { throw new Error("private"); }})), isCode("engine.worker.unsupported"));
  assert.throws(() => createEngineWorkerTransport(options({factory: () => ({})}, {disposeTimeoutMilliseconds: -1})), isCode("engine.worker.malformed_message"));

  const fatalHarness = linkedWorkerHarness();
  const fatal = createEngineWorkerTransport(options(fatalHarness));
  await fatal.ready;
  fatalHarness.worker.emitError("messageerror");
  assert.throws(() => fatal.request({exchangeID: "x", control: bytes(1), blobs: []}), isCode("engine.worker.crashed"));

  const postHarness = linkedWorkerHarness();
  const post = createEngineWorkerTransport(options(postHarness));
  await post.ready;
  postHarness.worker.postMessage = () => { throw new Error("private"); };
  const exchange = post.request({exchangeID: "exchange-1", control: bytes(1), blobs: []});
  await assert.rejects(exchange.accepted, isCode("engine.worker.transfer_failed"));
  await assert.rejects(exchange.response, isCode("engine.worker.transfer_failed"));
  await post.dispose();
});

class ManualWorker {
  listeners = new Map([["message", new Set()], ["messageerror", new Set()], ["error", new Set()]]);
  posts = [];
  terminated = false;

  postMessage(message, transfer) {
    this.posts.push({message, transfer});
  }

  addEventListener(type, listener) {
    this.listeners.get(type).add(listener);
  }

  removeEventListener(type, listener) {
    this.listeners.get(type).delete(listener);
  }

  terminate() {
    this.terminated = true;
  }

  emit(data) {
    for (const listener of [...this.listeners.get("message")]) listener({data});
  }
}

function readyMessage(overrides = {}) {
  return {
    worker_protocol: "layerdraw.engine_worker",
    worker_protocol_version: 1,
    kind: "ready",
    endpoint_generation: generation,
    artifact_manifest_digest: artifactDigest,
    transport_limits: transportLimits,
    ...overrides,
  };
}

function acceptedMessage(overrides = {}) {
  return {
    worker_protocol: "layerdraw.engine_worker",
    worker_protocol_version: 1,
    kind: "accepted",
    endpoint_generation: generation,
    exchange_id: "exchange-1",
    ...overrides,
  };
}

test("host closes invalid Worker ordering and accepts recoverable current-exchange failures", async () => {
  const recoverableWorker = new ManualWorker();
  const recoverable = createEngineWorkerTransport(options({factory: () => recoverableWorker}));
  recoverableWorker.emit(readyMessage());
  await recoverable.ready;
  const failed = recoverable.request({exchangeID: "exchange-1", control: bytes(1), blobs: []});
  recoverableWorker.emit({
    worker_protocol: "layerdraw.engine_worker",
    worker_protocol_version: 1,
    kind: "transport_failure",
    endpoint_generation: generation,
    exchange_id: "exchange-1",
    failure: {code: "engine.worker.malformed_message", phase: "request", retryable: false},
  });
  await assert.rejects(failed.accepted, isCode("engine.worker.malformed_message"));
  await assert.rejects(failed.response, isCode("engine.worker.malformed_message"));
  const next = recoverable.request({exchangeID: "exchange-2", control: bytes(2), blobs: []});
  const nextAccepted = assert.rejects(next.accepted, isCode("engine.worker.terminated_by_caller"));
  const nextResponse = assert.rejects(next.response, isCode("engine.worker.terminated_by_caller"));
  recoverable.terminate();
  await Promise.all([nextAccepted, nextResponse]);

  for (const messages of [
    [acceptedMessage()],
    [acceptedMessage(), acceptedMessage()],
    [{...acceptedMessage(), kind: "response", control: bytes(1), blobs: []}],
  ]) {
    const worker = new ManualWorker();
    const transport = createEngineWorkerTransport(options({factory: () => worker}));
    worker.emit(readyMessage());
    await transport.ready;
    const exchange = transport.request({exchangeID: "exchange-1", control: bytes(1), blobs: []});
    if (messages.length === 1 && messages[0].kind === "accepted") messages[0].exchange_id = "unknown";
    for (const message of messages) worker.emit(message);
    await assert.rejects(exchange.response, isCode("engine.worker.crashed"));
    if (messages[0].kind !== "accepted" || messages.length === 1) await assert.rejects(exchange.accepted, isCode("engine.worker.crashed"));
    else await exchange.accepted;
  }
});

test("host validates initial output, initial post, default Worker construction, and terminal disposal branches", async () => {
  const wrong = new ManualWorker();
  const mismatch = createEngineWorkerTransport(options({factory: () => wrong}));
  wrong.emit(readyMessage({artifact_manifest_digest: `sha256:${"c".repeat(64)}`}));
  await assert.rejects(mismatch.ready, isCode("engine.worker.artifact_mismatch"));
  await mismatch.dispose();

  const initialPost = new ManualWorker();
  initialPost.postMessage = () => { throw new Error("private initial post failure"); };
  const failed = createEngineWorkerTransport(options({factory: () => initialPost}));
  await assert.rejects(failed.ready, isCode("engine.worker.transfer_failed"));
  await failed.dispose();

  const descriptor = Object.getOwnPropertyDescriptor(globalThis, "Worker");
  class DefaultWorker extends LinkedWorker {
    constructor() {
      super(() => makeEchoEndpoint(), {checkEnvironment: () => true});
    }
  }
  Object.defineProperty(globalThis, "Worker", {configurable: true, writable: true, value: DefaultWorker});
  try {
    const specification = options({}, {workerFactory: undefined});
    const {workerFactory: _factory, workerModuleURL: _url, ...productionOptions} = specification;
    const transport = createProductionTransport(productionOptions);
    await transport.ready;
    await transport.dispose();
  } finally {
    if (descriptor === undefined) delete globalThis.Worker;
    else Object.defineProperty(globalThis, "Worker", descriptor);
  }

  const timerDescriptor = Object.getOwnPropertyDescriptor(globalThis, "setTimeout");
  const immediateWorker = new ManualWorker();
  const immediate = createEngineWorkerTransport(options({factory: () => immediateWorker}));
  immediateWorker.emit(readyMessage());
  await immediate.ready;
  Object.defineProperty(globalThis, "setTimeout", {configurable: true, writable: true, value: undefined});
  try {
    await immediate.dispose();
    assert.equal(immediateWorker.terminated, true);
  } finally {
    Object.defineProperty(globalThis, "setTimeout", timerDescriptor);
  }

  assert.throws(() => createEngineWorkerTransport(options({factory: () => new ManualWorker()}, {workerModuleURL: ""})), isCode("engine.worker.malformed_message"));
});
