// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import test from "node:test";

import {failure, workerProtocol, workerProtocolVersion} from "../dist/protocol.js";
import {EngineEndpointInitializationError, installEngineWorker} from "../dist/worker-runtime.js";
import {artifactDigest, bytes, generation, makeEchoEndpoint, nextTask, releaseDigest, transportLimits} from "./helpers.mjs";

class RecordingScope {
  listeners = new Map([["message", new Set()], ["messageerror", new Set()]]);
  posted = [];
  closed = false;

  postMessage(message, transfer) {
    const cloned = structuredClone(message, {transfer: [...transfer]});
    this.posted.push({message: cloned, transfer: [...transfer]});
  }

  addEventListener(type, listener) {
    this.listeners.get(type).add(listener);
  }

  removeEventListener(type, listener) {
    this.listeners.get(type).delete(listener);
  }

  close() {
    this.closed = true;
  }

  message(data) {
    for (const listener of [...this.listeners.get("message")]) listener({data});
  }

  messageError() {
    for (const listener of [...this.listeners.get("messageerror")]) listener();
  }
}

function init(overrides = {}) {
  return {
    worker_protocol: workerProtocol,
    worker_protocol_version: workerProtocolVersion,
    kind: "init",
    endpoint_generation: generation,
    expected_artifact_manifest_digest: artifactDigest,
    release_manifest_digest: releaseDigest,
    ...overrides,
  };
}

function request(overrides = {}) {
  return {
    worker_protocol: workerProtocol,
    worker_protocol_version: workerProtocolVersion,
    kind: "request",
    endpoint_generation: generation,
    exchange_id: "exchange-1",
    control: bytes(1, 2, 3),
    blobs: [{blob_id: "a", bytes: bytes(4, 5)}],
    ...overrides,
  };
}

async function initialized(createEndpoint = () => makeEchoEndpoint(), options = {checkEnvironment: () => true}) {
  const scope = new RecordingScope();
  const uninstall = installEngineWorker(scope, createEndpoint, options);
  scope.message(init());
  await nextTask();
  return {scope, uninstall};
}

test("worker initializes once and transfers opaque adapter output exactly", async () => {
  const endpoint = makeEchoEndpoint();
  const {scope} = await initialized(() => endpoint);
  assert.equal(scope.posted[0].message.kind, "ready");
  assert.deepEqual(scope.posted[0].message.transport_limits, transportLimits);

  const input = request();
  scope.message(input);
  assert.equal(endpoint.requests, 1);
  assert.equal(scope.posted[1].message.kind, "accepted");
  assert.equal(scope.posted[2].message.kind, "response");
  assert.deepEqual([...new Uint8Array(scope.posted[2].message.control)], [1, 2, 3]);
  assert.deepEqual([...new Uint8Array(scope.posted[2].message.blobs[0].bytes)], [4, 5]);
  assert.equal(scope.posted[2].transfer.length, 2);
  for (const transferred of scope.posted[2].transfer) assert.equal(transferred.byteLength, 0);

  scope.message(request({exchange_id: "exchange-2", control: bytes(6), blobs: []}));
  assert.equal(endpoint.requests, 2);
});

test("malformed and stale messages fail locally without entering the adapter", async () => {
  const endpoint = makeEchoEndpoint();
  const {scope} = await initialized(() => endpoint);
  scope.message({...request(), unknown: true});
  scope.message(request({endpoint_generation: "stale"}));
  scope.message({worker_protocol: workerProtocol, worker_protocol_version: workerProtocolVersion, kind: "unknown"});
  assert.equal(endpoint.requests, 0);
  assert.deepEqual(scope.posted.slice(1).map((entry) => entry.message.failure.code), [
    "engine.worker.malformed_message",
    "engine.worker.stale_generation",
    "engine.worker.malformed_message",
  ]);
  scope.message(request());
  assert.equal(endpoint.requests, 1);
});

test("the worker rejects reentrant second work while its synchronous endpoint is executing", async () => {
  const scope = new RecordingScope();
  let calls = 0;
  const endpoint = makeEchoEndpoint({
    request(input) {
      calls += 1;
      scope.message(request({exchange_id: "exchange-2", control: bytes(2), blobs: []}));
      return {ok: true, response: {control: input.control.slice(0), blobs: []}};
    },
  });
  installEngineWorker(scope, () => endpoint, {checkEnvironment: () => true});
  scope.message(init());
  await nextTask();
  scope.message(request({blobs: []}));
  assert.equal(calls, 1);
  assert.equal(scope.posted[2].message.failure.code, "engine.worker.malformed_message");
  assert.equal(scope.posted[3].message.kind, "response");
});

test("adapter throws and invalid output become one redacted crashed failure", async () => {
  const first = await initialized(() => makeEchoEndpoint());
  first.scope.message(request({control: bytes(255), blobs: []}));
  assert.equal(first.scope.posted.at(-1).message.failure.code, "engine.worker.crashed");
  assert.equal(first.scope.posted.at(-1).message.failure.message, undefined);
  assert.equal(first.scope.closed, true);

  const same = bytes(1);
  const second = await initialized(() => makeEchoEndpoint({request: () => ({ok: true, response: {control: same, blobs: [{blob_id: "a", bytes: same}]}})}));
  second.scope.message(request({blobs: []}));
  assert.equal(second.scope.posted.at(-1).message.failure.code, "engine.worker.crashed");
  assert.equal(second.scope.closed, true);
});

test("closed endpoint failures preserve their corpus classification and terminality", async () => {
  const recoverable = await initialized(() => makeEchoEndpoint({
    request: () => ({ok: false, failure: failure("engine.worker.malformed_message")}),
  }));
  recoverable.scope.message(request({blobs: []}));
  assert.deepEqual(recoverable.scope.posted.at(-1).message.failure, {
    code: "engine.worker.malformed_message", phase: "request", retryable: false,
  });
  assert.equal(recoverable.scope.closed, false);

  const terminal = await initialized(() => makeEchoEndpoint({
    request: () => ({ok: false, failure: failure("engine.worker.transfer_failed")}),
  }));
  terminal.scope.message(request({blobs: []}));
  assert.equal(terminal.scope.posted.at(-1).message.failure.code, "engine.worker.transfer_failed");
  assert.equal(terminal.scope.closed, true);

  const invalid = await initialized(() => makeEchoEndpoint({
    request: () => ({ok: false, failure: {code: "private", phase: "private", retryable: true}}),
  }));
  invalid.scope.message(request({blobs: []}));
  assert.equal(invalid.scope.posted.at(-1).message.failure.code, "engine.worker.crashed");

  const mismatch = new EngineEndpointInitializationError("engine.worker.artifact_mismatch");
  assert.equal(mismatch.failure.code, "engine.worker.artifact_mismatch");
  assert.equal(mismatch.name, "EngineEndpointInitializationError");
});

test("initialization failures are closed and dispose is idempotent", async () => {
  const unsupported = new RecordingScope();
  installEngineWorker(unsupported, () => makeEchoEndpoint(), {checkEnvironment: () => false});
  unsupported.message(init());
  assert.equal(unsupported.posted[0].message.failure.code, "engine.worker.unsupported");
  assert.equal(unsupported.closed, true);

  const rejected = new RecordingScope();
  installEngineWorker(rejected, () => Promise.reject(new Error("secret")), {checkEnvironment: () => true});
  rejected.message(init());
  await nextTask();
  assert.equal(rejected.posted[0].message.failure.code, "engine.worker.initialization_failed");

  const mismatch = new RecordingScope();
  installEngineWorker(mismatch, () => makeEchoEndpoint({artifactManifestDigest: `sha256:${"c".repeat(64)}`}), {checkEnvironment: () => true});
  mismatch.message(init());
  await nextTask();
  assert.equal(mismatch.posted[0].message.failure.code, "engine.worker.artifact_mismatch");

  const endpoint = makeEchoEndpoint();
  const active = await initialized(() => endpoint);
  active.scope.message({
    worker_protocol: workerProtocol,
    worker_protocol_version: workerProtocolVersion,
    kind: "dispose",
    endpoint_generation: generation,
  });
  assert.equal(endpoint.disposed, true);
  assert.equal(active.scope.posted.at(-1).message.failure.code, "engine.worker.disposed");
  assert.equal(active.scope.closed, true);
  active.uninstall();
  active.uninstall();
});

test("messageerror crashes a live worker and repeated init is rejected", async () => {
  const active = await initialized();
  active.scope.message(init());
  assert.equal(active.scope.posted.at(-1).message.failure.code, "engine.worker.malformed_message");
  active.scope.messageError();
  assert.equal(active.scope.posted.at(-1).message.failure.code, "engine.worker.crashed");
});

test("default environment probe is fail-closed for every required primitive", async () => {
  const supported = new RecordingScope();
  installEngineWorker(supported, () => makeEchoEndpoint());
  supported.message(init());
  await nextTask();
  assert.equal(supported.posted[0].message.kind, "ready");

  const replaceGlobal = async (name, value, run) => {
    const descriptor = Object.getOwnPropertyDescriptor(globalThis, name);
    Object.defineProperty(globalThis, name, {configurable: true, writable: true, value});
    try {
      await run();
    } finally {
      if (descriptor === undefined) delete globalThis[name];
      else Object.defineProperty(globalThis, name, descriptor);
    }
  };
  for (const [name, value] of [
    ["WebAssembly", undefined],
    ["TextEncoder", undefined],
    ["TextDecoder", undefined],
    ["fetch", undefined],
    ["Blob", undefined],
    ["URL", {}],
    ["URL", {createObjectURL() { return "blob:test"; }}],
    ["URL", {revokeObjectURL() {}}],
    ["crypto", {}],
    ["crypto", {getRandomValues() {}, subtle: {}}],
    ["crypto", {subtle: {digest() {}}}],
    ["performance", {}],
  ]) await replaceGlobal(name, value, async () => {
    const scope = new RecordingScope();
    installEngineWorker(scope, () => makeEchoEndpoint());
    scope.message(init());
    assert.equal(scope.posted[0].message.failure.code, "engine.worker.unsupported");
  });
  await replaceGlobal("structuredClone", undefined, async () => {
    const scope = new RecordingScope();
    installEngineWorker(scope, () => makeEchoEndpoint());
    scope.message(init());
    assert.equal(scope.posted.length, 0);
    assert.equal(scope.closed, true);
  });
  await replaceGlobal("structuredClone", () => { throw new Error("clone failed"); }, async () => {
    const scope = new RecordingScope();
    installEngineWorker(scope, () => makeEchoEndpoint());
    scope.message(init());
    assert.equal(scope.posted.length, 0);
    assert.equal(scope.closed, true);
  });
});

test("worker redacts factory, disposal, and postMessage failures across lifecycle phases", async () => {
  const factoryFailure = new RecordingScope();
  installEngineWorker(factoryFailure, () => { throw new Error("private factory failure"); }, {checkEnvironment: () => true});
  factoryFailure.message(init());
  assert.equal(factoryFailure.posted[0].message.failure.code, "engine.worker.initialization_failed");

  const invalid = new RecordingScope();
  installEngineWorker(invalid, () => makeEchoEndpoint({
    transportLimits: {...transportLimits, max_control_bytes: 1},
    dispose() { throw new Error("private dispose failure"); },
  }), {checkEnvironment: () => true});
  invalid.message(init());
  await nextTask();
  assert.equal(invalid.posted[0].message.failure.code, "engine.worker.initialization_failed");

  const mismatch = new RecordingScope();
  installEngineWorker(mismatch, () => makeEchoEndpoint({
    artifactManifestDigest: `sha256:${"c".repeat(64)}`,
    dispose() { throw new Error("private dispose failure"); },
  }), {checkEnvironment: () => true});
  mismatch.message(init());
  await nextTask();
  assert.equal(mismatch.posted[0].message.failure.code, "engine.worker.artifact_mismatch");

  const postFailure = new RecordingScope();
  postFailure.postMessage = () => { throw new Error("post failed"); };
  installEngineWorker(postFailure, () => makeEchoEndpoint(), {checkEnvironment: () => false});
  postFailure.message(init());
  assert.equal(postFailure.closed, true);

  const readyFailure = new RecordingScope();
  readyFailure.postMessage = () => { throw new Error("ready post failed"); };
  installEngineWorker(readyFailure, () => makeEchoEndpoint(), {checkEnvironment: () => true});
  readyFailure.message(init());
  await nextTask();
  assert.equal(readyFailure.closed, true);

  const acceptedFailure = new RecordingScope();
  let acceptedPosts = 0;
  acceptedFailure.postMessage = (message, transfer) => {
    acceptedPosts += 1;
    if (message.kind === "accepted") throw new Error("accepted post failed");
    RecordingScope.prototype.postMessage.call(acceptedFailure, message, transfer);
  };
  installEngineWorker(acceptedFailure, () => makeEchoEndpoint(), {checkEnvironment: () => true});
  acceptedFailure.message(init());
  await nextTask();
  acceptedFailure.message(request({blobs: []}));
  assert.equal(acceptedPosts, 2);
  assert.equal(acceptedFailure.closed, true);

  const responseFailure = new RecordingScope();
  responseFailure.postMessage = (message, transfer) => {
    if (message.kind === "response") throw new Error("response post failed");
    RecordingScope.prototype.postMessage.call(responseFailure, message, transfer);
  };
  installEngineWorker(responseFailure, () => makeEchoEndpoint({dispose() { throw new Error("dispose failed"); }}), {checkEnvironment: () => true});
  responseFailure.message(init());
  await nextTask();
  responseFailure.message(request({blobs: []}));
  assert.equal(responseFailure.posted.at(-1).message.failure.code, "engine.worker.transfer_failed");
  assert.equal(responseFailure.closed, true);

  const failurePostFailure = new RecordingScope();
  const failureEndpoint = makeEchoEndpoint({
    request: () => ({ok: false, failure: failure("engine.worker.malformed_message")}),
  });
  failurePostFailure.postMessage = (message, transfer) => {
    if (message.kind === "transport_failure") throw new Error("failure post failed");
    RecordingScope.prototype.postMessage.call(failurePostFailure, message, transfer);
  };
  installEngineWorker(failurePostFailure, () => failureEndpoint, {checkEnvironment: () => true});
  failurePostFailure.message(init());
  await nextTask();
  failurePostFailure.message(request({blobs: []}));
  assert.equal(failurePostFailure.closed, true);
  assert.equal(failureEndpoint.disposed, true);
});

test("dispose during async initialization releases the eventual endpoint and stale dispose is rejected", async () => {
  let resolveEndpoint;
  const created = new Promise((resolve) => { resolveEndpoint = resolve; });
  const scope = new RecordingScope();
  installEngineWorker(scope, () => created, {checkEnvironment: () => true});
  scope.message(init());
  scope.message(request());
  assert.equal(scope.posted[0].message.failure.code, "engine.worker.initialization_failed");
  scope.message({worker_protocol: workerProtocol, worker_protocol_version: workerProtocolVersion, kind: "dispose", endpoint_generation: "stale"});
  assert.equal(scope.posted[1].message.failure.code, "engine.worker.stale_generation");
  scope.message({worker_protocol: workerProtocol, worker_protocol_version: workerProtocolVersion, kind: "dispose", endpoint_generation: generation});
  const eventual = makeEchoEndpoint({dispose() { throw new Error("dispose failed"); }});
  resolveEndpoint(eventual);
  await nextTask();
  assert.equal(scope.closed, true);

  const active = await initialized(() => makeEchoEndpoint({dispose() { throw new Error("dispose failed"); }}));
  active.uninstall();
  active.uninstall();
  assert.equal(active.scope.listeners.get("message").size, 0);
});
