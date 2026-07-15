// SPDX-License-Identifier: Apache-2.0

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { after, afterEach, test } from "node:test";
import {
  decodeCompileRequestEnvelope,
  decodeHandshakeRequestEnvelope,
  encodeCompileResponseEnvelope,
  encodeHandshakeResponseEnvelope,
} from "@layerdraw/protocol/engine";
import {
  EngineClientInputError,
  EngineClientTransportError,
} from "../dist/index.js";
import { createWasmEngineClient } from "../dist/wasm.js";
import {
  creationOptions,
  expectedReleaseDigest,
  makePortableRequest,
  successResponse,
  waitFor,
} from "./support.mjs";

const artifactDigest = `sha256:${"a".repeat(64)}`;
const root = new URL("../../../", import.meta.url);
const handshakeTemplate = JSON.parse(
  await readFile(
    new URL("schemas/fixtures/engine/handshake-success.json", root),
    "utf8",
  ),
);
const transportLimits = Object.freeze({
  max_control_bytes: 8_388_608,
  max_control_depth: 128,
  max_blob_id_bytes: 256,
  max_buffers: 2_048,
  max_input_blob_bytes: 33_554_432,
  max_input_total_bytes: 67_108_864,
  max_output_blob_bytes: 33_554_432,
  max_output_total_bytes: 67_108_864,
  max_response_publish_bytes: 75_497_472,
});
const decoder = new TextDecoder("utf-8", { fatal: true });
const encoder = new TextEncoder();
const workerDescriptor = Object.getOwnPropertyDescriptor(globalThis, "Worker");

function encodeControl(value) {
  return encoder.encode(value).buffer;
}

class ProtocolWorker {
  static instances = [];
  static mismatchArtifact = false;
  static stallCompile = false;

  listeners = new Map([
    ["message", new Set()],
    ["messageerror", new Set()],
    ["error", new Set()],
  ]);
  terminated = false;
  endpointGeneration = undefined;
  releaseManifestDigest = undefined;
  artifactManifestDigest = undefined;
  compileRequests = 0;
  endpointIndex;

  constructor(_moduleURL, options) {
    assert.equal(options.type, "module");
    this.endpointIndex = ProtocolWorker.instances.length + 1;
    ProtocolWorker.instances.push(this);
  }

  postMessage(message, transfer) {
    if (this.terminated) throw new Error("worker terminated /private/path");
    const cloned = structuredClone(message, { transfer: [...transfer] });
    queueMicrotask(() => void this.#receive(cloned));
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

  crash() {
    this.#emitEvent("error");
  }

  async #receive(message) {
    if (this.terminated) return;
    if (message.kind === "init") {
      this.endpointGeneration = message.endpoint_generation;
      this.releaseManifestDigest = message.release_manifest_digest;
      this.artifactManifestDigest = message.expected_artifact_manifest_digest;
      this.#emitMessage({
        worker_protocol: "layerdraw.engine_worker",
        worker_protocol_version: 1,
        kind: "ready",
        endpoint_generation: this.endpointGeneration,
        artifact_manifest_digest: ProtocolWorker.mismatchArtifact
          ? `sha256:${"b".repeat(64)}`
          : this.artifactManifestDigest,
        transport_limits: transportLimits,
      });
      return;
    }
    if (message.kind === "dispose") {
      this.#emitMessage({
        worker_protocol: "layerdraw.engine_worker",
        worker_protocol_version: 1,
        kind: "transport_failure",
        endpoint_generation: this.endpointGeneration,
        failure: {
          code: "engine.worker.disposed",
          phase: "lifecycle",
          retryable: false,
        },
      });
      return;
    }
    if (message.kind !== "request") return;

    this.#emitMessage({
      worker_protocol: "layerdraw.engine_worker",
      worker_protocol_version: 1,
      kind: "accepted",
      endpoint_generation: this.endpointGeneration,
      exchange_id: message.exchange_id,
    });

    const control = decoder.decode(message.control);
    const operation = JSON.parse(control).operation;
    if (operation === "engine.handshake") {
      const request = decodeHandshakeRequestEnvelope(control);
      const response = structuredClone(handshakeTemplate);
      response.request_id = request.request_id;
      response.payload.endpoint_instance_id = `wasm-endpoint-${this.endpointIndex}`;
      response.payload.release_manifest_digest = this.releaseManifestDigest;
      response.payload.capability_manifest.transports = ["wasm_worker"];
      response.payload.capability_statuses = [
        ...request.payload.required_capabilities.map((capability_id) => ({
          capability_id,
          enabled: true,
          protocol_version: "1.0",
        })),
        ...request.payload.optional_capabilities.map((capability_id) => ({
          capability_id,
          enabled: false,
          protocol_version: "1.0",
          unavailable_reason: "unsupported",
        })),
      ].filter(
        (status, index, all) =>
          all.findIndex(
            (candidate) => candidate.capability_id === status.capability_id,
          ) === index,
      );
      this.#respond(
        message.exchange_id,
        encodeControl(encodeHandshakeResponseEnvelope(response)),
        [],
      );
      return;
    }

    const request = decodeCompileRequestEnvelope(control);
    this.compileRequests += 1;
    if (ProtocolWorker.stallCompile) return;
    assert.deepEqual(
      message.blobs.map((blob) => blob.blob_id),
      ["compile/source/document.ldl"],
    );
    const result = await successResponse(request.request_id);
    this.#respond(
      message.exchange_id,
      encodeControl(encodeCompileResponseEnvelope(result.response)),
      result.blobs.map((blob) => ({
        blob_id: blob.blobId,
        bytes: blob.bytes,
      })),
    );
  }

  #respond(exchangeID, control, blobs) {
    this.#emitMessage(
      {
        worker_protocol: "layerdraw.engine_worker",
        worker_protocol_version: 1,
        kind: "response",
        endpoint_generation: this.endpointGeneration,
        exchange_id: exchangeID,
        control,
        blobs,
      },
      [control, ...blobs.map((blob) => blob.bytes)],
    );
  }

  #emitMessage(message, transfer = []) {
    const cloned = structuredClone(message, { transfer });
    queueMicrotask(() => {
      if (!this.terminated) this.#emitEvent("message", { data: cloned });
    });
  }

  #emitEvent(type, event) {
    if (this.terminated) return;
    for (const listener of [...this.listeners.get(type)]) listener(event);
  }
}

Object.defineProperty(globalThis, "Worker", {
  configurable: true,
  writable: true,
  value: ProtocolWorker,
});

after(() => {
  if (workerDescriptor === undefined) delete globalThis.Worker;
  else Object.defineProperty(globalThis, "Worker", workerDescriptor);
});

afterEach(() => {
  ProtocolWorker.mismatchArtifact = false;
  ProtocolWorker.stallCompile = false;
});

function options(overrides = {}) {
  return {
    client: creationOptions,
    expectedArtifactManifestDigest: artifactDigest,
    ...overrides,
  };
}

test("WASM adapter handshakes, compiles, replaces, and disposes through a Worker", async () => {
  ProtocolWorker.instances.length = 0;
  const client = await createWasmEngineClient(options({ workerName: "test-engine" }));
  assert.equal(client.state, "ready");
  assert.deepEqual(client.getEndpoint().handshake.capability_manifest.transports, [
    "wasm_worker",
  ]);

  const outcome = await client.compile(makePortableRequest().request, {
    requestId: "wasm-compile",
  });
  assert.equal(outcome.origin, "engine");
  assert.equal(outcome.outcome, "success");
  assert.deepEqual(
    outcome.blobs.map((blob) => decoder.decode(blob.bytes)),
    ["query-output", "artifact-output", "canonical-output"],
  );

  await client.restart();
  assert.equal(client.getEndpoint().generation, 2);
  assert.equal(
    client.getEndpoint().handshake.endpoint_instance_id,
    "wasm-endpoint-2",
  );
  assert.equal(ProtocolWorker.instances.length, 2);
  assert.equal(ProtocolWorker.instances[0].terminated, true);

  await client.dispose();
  assert.equal(client.state, "disposed");
  assert.equal(ProtocolWorker.instances[1].terminated, true);
});

test("WASM adapter replaces an idle Worker after a stable close notification", async () => {
  ProtocolWorker.instances.length = 0;
  const client = await createWasmEngineClient(options());
  ProtocolWorker.instances[0].crash();
  await waitFor(
    () =>
      client.state === "ready" &&
      ProtocolWorker.instances.length === 2 &&
      client.getEndpoint().generation === 2,
  );
  assert.equal(ProtocolWorker.instances[0].terminated, true);
  await client.dispose();
});

test("WASM adapter hard-cancels an active Worker and replaces it before settling", async () => {
  ProtocolWorker.instances.length = 0;
  ProtocolWorker.stallCompile = true;
  const client = await createWasmEngineClient(options());
  const controller = new AbortController();
  const pending = client.compile(makePortableRequest().request, {
    requestId: "wasm-cancel",
    signal: controller.signal,
  });
  await waitFor(() => ProtocolWorker.instances[0].compileRequests === 1);
  controller.abort();
  const outcome = await pending;
  assert.equal(outcome.origin, "client");
  assert.equal(outcome.outcome, "cancelled");
  assert.equal(outcome.reason, "signal");
  await waitFor(
    () =>
      client.state === "ready" &&
      ProtocolWorker.instances.length === 2 &&
      client.getEndpoint().generation === 2,
  );
  await client.dispose();
});

test("WASM adapter redacts initialization failures behind the client boundary", async () => {
  ProtocolWorker.instances.length = 0;
  ProtocolWorker.mismatchArtifact = true;
  await assert.rejects(
    createWasmEngineClient(options()),
    (error) =>
      error instanceof EngineClientTransportError &&
      error.code === "CONNECT_FAILED" &&
      error.cause === undefined,
  );
});

test("WASM adapter rejects malformed public options before constructing a Worker", async () => {
  ProtocolWorker.instances.length = 0;
  await assert.rejects(
    createWasmEngineClient({
      client: creationOptions,
      expectedArtifactManifestDigest: artifactDigest,
      workerDisposeTimeoutMs: 1.5,
    }),
    (error) =>
      error instanceof EngineClientInputError && error.code === "INVALID_ARGUMENT",
  );
  await assert.rejects(
    createWasmEngineClient({
      client: creationOptions,
      expectedArtifactManifestDigest: "not-a-digest",
    }),
    (error) =>
      error instanceof EngineClientInputError && error.code === "INVALID_ARGUMENT",
  );
  await assert.rejects(
    createWasmEngineClient({
      client: creationOptions,
      expectedArtifactManifestDigest: artifactDigest,
      workerDisposeTimeoutMs: 60_001,
    }),
    (error) =>
      error instanceof EngineClientInputError && error.code === "INVALID_ARGUMENT",
  );
  await assert.rejects(
    createWasmEngineClient({
      client: creationOptions,
      expectedArtifactManifestDigest: artifactDigest,
      unexpected: true,
    }),
    (error) =>
      error instanceof EngineClientInputError && error.code === "INVALID_ARGUMENT",
  );
  assert.equal(ProtocolWorker.instances.length, 0);
  assert.equal(expectedReleaseDigest, creationOptions.expectedReleaseManifestDigest);
});
