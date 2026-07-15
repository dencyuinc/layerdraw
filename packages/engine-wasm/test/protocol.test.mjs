// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import test from "node:test";

import {
  failure,
  getSafeKind,
  getSafeRouting,
  isFixedArrayBuffer,
  validateEndpointResponse,
  validateInitMessage,
  validateRequestMessage,
  validateRequestPayload,
  validateTransportLimits,
  validateWorkerMessage,
  workerProtocol,
  workerProtocolVersion,
} from "../dist/protocol.js";
import {artifactDigest, bytes, generation, releaseDigest, transportLimits} from "./helpers.mjs";

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
    control: bytes(1),
    blobs: [],
    ...overrides,
  };
}

test("init grammar is closed and bounded without evaluating accessors", () => {
  assert.ok(validateInitMessage(init()));
  assert.equal(validateInitMessage(init({unknown: true})), undefined);
  assert.equal(validateInitMessage(init({worker_protocol_version: 2})), undefined);
  assert.equal(validateInitMessage(init({endpoint_generation: "x".repeat(129)})), undefined);
  assert.ok(validateInitMessage(init({endpoint_generation: "😀".repeat(32)})));
  assert.equal(validateInitMessage(init({endpoint_generation: "😀".repeat(33)})), undefined);
  assert.equal(validateInitMessage(init({release_manifest_digest: "sha256:ABC"})), undefined);

  let called = false;
  const accessor = init();
  Object.defineProperty(accessor, "endpoint_generation", {enumerable: true, get() { called = true; return generation; }});
  assert.equal(validateInitMessage(accessor), undefined);
  assert.equal(called, false);

  const inherited = Object.assign(Object.create({unknown: true}), init());
  assert.equal(validateInitMessage(inherited), undefined);
  const nullPrototype = Object.assign(Object.create(null), init());
  assert.ok(validateInitMessage(nullPrototype));
  const symbol = init();
  symbol[Symbol("unknown")] = true;
  assert.equal(validateInitMessage(symbol), undefined);
});

test("only exact fixed ArrayBuffer identities are accepted", () => {
  assert.equal(isFixedArrayBuffer(new ArrayBuffer(0)), true);
  assert.equal(isFixedArrayBuffer(new Uint8Array(1)), false);
  assert.equal(isFixedArrayBuffer(new SharedArrayBuffer(1)), false);
  assert.equal(isFixedArrayBuffer(new ArrayBuffer(1, {maxByteLength: 2})), false);
  const decorated = new ArrayBuffer(1);
  decorated.extra = true;
  assert.equal(isFixedArrayBuffer(decorated), false);
});

test("request attachments reject aliases, duplicate or unordered ids, views, and limits", () => {
  assert.ok(validateRequestMessage(request({blobs: [{blob_id: "a", bytes: bytes(1)}, {blob_id: "b", bytes: bytes(2)}]}), transportLimits));
  assert.equal(validateRequestMessage(request({control: new Uint8Array(1)}), transportLimits), undefined);
  const aliased = bytes(1);
  assert.equal(validateRequestMessage(request({control: aliased, blobs: [{blob_id: "a", bytes: aliased}]}), transportLimits), undefined);
  assert.equal(validateRequestMessage(request({blobs: [{blob_id: "a", bytes: bytes(1)}, {blob_id: "a", bytes: bytes(2)}]}), transportLimits), undefined);
  assert.equal(validateRequestMessage(request({blobs: [{blob_id: "b", bytes: bytes(1)}, {blob_id: "a", bytes: bytes(2)}]}), transportLimits), undefined);
  assert.ok(validateRequestMessage(request({blobs: [{blob_id: "\ue000", bytes: bytes(1)}, {blob_id: "😀", bytes: bytes(2)}]}), transportLimits));
  assert.equal(validateRequestMessage(request({blobs: [{blob_id: "😀", bytes: bytes(1)}, {blob_id: "\ue000", bytes: bytes(2)}]}), transportLimits), undefined);
  assert.equal(validateRequestMessage(
    request({blobs: [{blob_id: "a", bytes: bytes(...new Array(1_025).fill(0))}]}),
    {...transportLimits, max_input_blob_bytes: 1_024},
  ), undefined);
  assert.equal(validateRequestMessage(request({blobs: [{blob_id: "a", bytes: new SharedArrayBuffer(1)}]}), transportLimits), undefined);
  assert.equal(validateRequestMessage(request({blobs: [{blob_id: "a", bytes: bytes(1), digest: artifactDigest}]}), transportLimits), undefined);
  assert.ok(validateRequestMessage(request({exchange_id: "é".repeat(64), blobs: [{blob_id: "é".repeat(128), bytes: bytes(1)}]}), transportLimits));
  assert.equal(validateRequestMessage(request({exchange_id: "é".repeat(65)}), transportLimits), undefined);
  assert.equal(validateRequestMessage(request({blobs: [{blob_id: "é".repeat(129), bytes: bytes(1)}]}), transportLimits), undefined);

  assert.ok(validateRequestPayload({exchangeID: "exchange-1", control: bytes(1), blobs: []}, transportLimits));
  assert.equal(validateRequestPayload(null, transportLimits), undefined);
  assert.equal(validateRequestPayload({exchangeID: "", control: bytes(1), blobs: []}, transportLimits), undefined);
  assert.equal(validateRequestPayload({exchangeID: "exchange-1", control: bytes(), blobs: []}, transportLimits), undefined);
  assert.equal(validateRequestPayload({exchangeID: "exchange-1", control: bytes(1), blobs: [], unknown: true}, transportLimits), undefined);
});

test("transport limits and endpoint responses are closed and internally ordered", () => {
  assert.deepEqual(validateTransportLimits(transportLimits), transportLimits);
  assert.ok(validateTransportLimits({...transportLimits, max_control_bytes: 1}));
  assert.equal(validateTransportLimits({...transportLimits, max_buffers: 0}), undefined);
  assert.equal(validateTransportLimits({...transportLimits, max_buffers: "8"}), undefined);
  assert.equal(validateTransportLimits({...transportLimits, max_buffers: 2_049}), undefined);
  assert.equal(validateTransportLimits({...transportLimits, max_input_blob_bytes: 2_000, max_input_total_bytes: 1_000}), undefined);
  assert.equal(validateTransportLimits({...transportLimits, max_output_blob_bytes: 67_108_865}), undefined);
  assert.equal(validateTransportLimits({...transportLimits, max_response_publish_bytes: 1}), undefined);
  assert.equal(validateTransportLimits({...transportLimits, extra: true}), undefined);
  assert.ok(validateEndpointResponse({control: bytes(1), blobs: []}, transportLimits));
  const same = bytes(1);
  assert.equal(validateEndpointResponse({control: same, blobs: [{blob_id: "a", bytes: same}]}, transportLimits), undefined);
});

test("worker failures are a closed code, phase, and retryability table", () => {
  for (const code of [
    "engine.worker.unsupported",
    "engine.worker.initialization_failed",
    "engine.worker.artifact_mismatch",
    "engine.worker.malformed_message",
    "engine.worker.stale_generation",
    "engine.worker.transfer_failed",
    "engine.worker.crashed",
    "engine.worker.terminated_by_caller",
    "engine.worker.disposed",
  ]) {
    const message = {
      worker_protocol: workerProtocol,
      worker_protocol_version: workerProtocolVersion,
      kind: "transport_failure",
      endpoint_generation: generation,
      failure: failure(code),
    };
    assert.ok(validateWorkerMessage(message));
    assert.equal(validateWorkerMessage({...message, failure: {...message.failure, retryable: !message.failure.retryable}}), undefined);
  }
});

test("safe routing and kind inspection ignore arbitrary object graphs", () => {
  assert.deepEqual(getSafeRouting({endpoint_generation: generation, exchange_id: "exchange-1"}), {endpoint_generation: generation, exchange_id: "exchange-1"});
  assert.equal(getSafeKind({kind: "request"}), "request");
  assert.equal(getSafeKind({kind: "x".repeat(65)}), undefined);
  let called = false;
  const value = {};
  Object.defineProperty(value, "kind", {enumerable: true, get() { called = true; return "request"; }});
  assert.equal(getSafeKind(value), undefined);
  assert.deepEqual(getSafeRouting(value), {});
  assert.equal(called, false);
  assert.equal(validateWorkerMessage({worker_protocol: workerProtocol, worker_protocol_version: workerProtocolVersion, kind: "future"}), undefined);
  assert.equal(validateWorkerMessage({
    worker_protocol: workerProtocol,
    worker_protocol_version: workerProtocolVersion,
    kind: "response",
    endpoint_generation: generation,
    exchange_id: "x",
    control: bytes(1),
    blobs: [],
  }), undefined);
  const localFailure = {
    worker_protocol: workerProtocol,
    worker_protocol_version: workerProtocolVersion,
    kind: "transport_failure",
    failure: failure("engine.worker.crashed"),
  };
  assert.ok(validateWorkerMessage(localFailure));
  assert.equal(validateWorkerMessage({...localFailure, endpoint_generation: "x".repeat(129)}), undefined);
  assert.equal(validateWorkerMessage({...localFailure, exchange_id: "x".repeat(129)}), undefined);
});
