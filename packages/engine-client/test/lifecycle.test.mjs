// SPDX-License-Identifier: Apache-2.0

import assert from "node:assert/strict";
import { test } from "node:test";
import {
  EngineClientBackpressureError,
  EngineClientDecodeError,
  EngineClientStateError,
  EngineClientTransportError,
} from "../dist/index.js";
import { createInternalEngineClient } from "../dist/internal/client.js";
import {
  StrictFakeTransport,
  collectors,
  creationOptions,
  makeFactory,
  makePortableRequest,
  rejectedResponse,
  stalledDigestLease,
  waitFor,
} from "./support.mjs";

async function create(overrides, options = {}) {
  const factory = await makeFactory(overrides);
  const client = await createInternalEngineClient({
    transportFactory: factory,
    protocolCollectors: collectors,
    options: { ...creationOptions, ...options },
  });
  return { client, factory };
}

test("single flight rejects a second compile immediately and identifies duplicates", async () => {
  const { client } = await create({
    compile() {
      return StrictFakeTransport.PENDING;
    },
  });
  const first = client.compile(makePortableRequest().request, {
    requestId: "active-id",
  });
  await assert.rejects(
    Promise.resolve().then(() =>
      client.compile(makePortableRequest().request, { requestId: "other-id" }),
    ),
    (error) =>
      error instanceof EngineClientBackpressureError &&
      error.code === "SINGLE_FLIGHT_BUSY",
  );
  assert.throws(
    () =>
      client.compile(makePortableRequest().request, { requestId: "active-id" }),
    (error) =>
      error instanceof EngineClientStateError &&
      error.code === "DUPLICATE_REQUEST_ID",
  );
  const restart = client.restart();
  const outcome = await first;
  assert.equal(outcome.origin, "client");
  assert.equal(outcome.reason, "restart");
  await restart;
  await client.dispose();
});

test("abort joins a reusable exchange without replacing the endpoint", async () => {
  const { client, factory } = await create({
    compile() {
      return StrictFakeTransport.PENDING;
    },
  });
  const controller = new AbortController();
  const promise = client.compile(makePortableRequest().request, {
    requestId: "abort-me",
    signal: controller.signal,
  });
  await waitFor(() => factory.endpoints[0].requests.length === 2);
  controller.abort(new Error("must never escape"));
  const outcome = await promise;
  assert.equal(outcome.origin, "client");
  assert.equal(outcome.reason, "signal");
  assert.equal(factory.endpoints[0].requests[1].cancelCount, 1);
  assert.equal(factory.endpoints.length, 1);
  assert.equal(client.state, "ready");
  await client.dispose();
});

test("abort hard-stops and joins a permanently stalled input digest", async () => {
  let digestCalls = 0;
  let digestLease;
  const timers = new Set();
  const runtime = {
    now: () => 1_700_000_000_000,
    randomBytes: (length) => new Uint8Array(length).fill(7),
    sha256: (bytes) => {
      digestCalls++;
      digestLease = stalledDigestLease(bytes);
      return digestLease;
    },
    transferArrayBuffer: (buffer) =>
      structuredClone(buffer, { transfer: [buffer] }),
    setTimer(callback) {
      const handle = { callback };
      timers.add(handle);
      return handle;
    },
    clearTimer(handle) {
      timers.delete(handle);
    },
  };
  const factory = await makeFactory();
  const client = await createInternalEngineClient({
    transportFactory: factory,
    protocolCollectors: collectors,
    options: creationOptions,
    runtime,
  });
  const controller = new AbortController();
  let settled = false;
  const compile = client
    .compile(makePortableRequest().request, { signal: controller.signal })
    .finally(() => {
      settled = true;
    });
  await waitFor(() => digestCalls === 1);
  controller.abort();
  const outcome = await compile;
  assert.equal(settled, true);
  assert.equal(outcome.reason, "signal");
  assert.equal(digestLease.abortCount, 1);
  assert.equal(digestLease.joinedSettled, true);
  assert.equal(digestLease.retainsInput, false);
  assert.equal(factory.endpoints[0].requests.length, 1);
  assert.equal(timers.size, 0);
  await client.dispose();
});

test("timeout hard cancellation replaces and joins before outcome settlement", async () => {
  let cancelFinished = false;
  const { client, factory } = await create(
    {
      compile() {
        return StrictFakeTransport.PENDING;
      },
      async cancel() {
        await new Promise((resolve) => setImmediate(resolve));
        cancelFinished = true;
        return { reusable: false };
      },
    },
    { defaultCompileTimeoutMs: 20 },
  );
  const outcome = await client.compile(makePortableRequest().request, {
    requestId: "timeout-me",
  });
  assert.equal(outcome.origin, "client");
  assert.equal(outcome.reason, "timeout");
  assert.equal(cancelFinished, true);
  assert.equal(factory.endpoints.length, 2);
  assert.equal(client.getEndpoint().generation, 2);
  await client.dispose();
});

test("client cancellation remains a value when its required replacement fails", async () => {
  const { client, factory } = await create({
    compile() {
      return StrictFakeTransport.PENDING;
    },
    cancel() {
      return { reusable: false };
    },
    create(generation, owner) {
      if (owner.endpoints.length > 0) {
        throw new Error("sensitive replacement failure");
      }
      const endpoint = new StrictFakeTransport(owner, generation, 0);
      owner.endpoints.push(endpoint);
      return endpoint;
    },
  });
  const controller = new AbortController();
  const compile = client.compile(makePortableRequest().request, {
    signal: controller.signal,
  });
  await waitFor(() => factory.endpoints[0].requests.length === 2);
  controller.abort();
  const outcome = await compile;
  assert.equal(outcome.origin, "client");
  assert.equal(outcome.reason, "signal");
  assert.equal(client.state, "failed");
  await client.dispose().catch(() => undefined);
});

test("concurrent restarts coalesce, cancel active work, and publish one generation", async () => {
  const { client, factory } = await create({
    compile() {
      return StrictFakeTransport.PENDING;
    },
  });
  const compile = client.compile(makePortableRequest().request);
  await waitFor(() => factory.endpoints[0].requests.length === 2);
  const first = client.restart();
  const second = client.restart();
  assert.equal(first, second);
  assert.equal(client.state, "replacing");
  assert.throws(
    () => client.getEndpoint(),
    (error) =>
      error instanceof EngineClientStateError && error.code === "NOT_READY",
  );
  const outcome = await compile;
  assert.equal(outcome.reason, "restart");
  await first;
  assert.equal(factory.endpoints.length, 2);
  assert.equal(client.getEndpoint().generation, 2);
  await client.dispose();
});

test("dispose is idempotent, wins active work, joins, and is terminal", async () => {
  const { client, factory } = await create({
    compile() {
      return StrictFakeTransport.PENDING;
    },
  });
  const compile = client.compile(makePortableRequest().request);
  await waitFor(() => factory.endpoints[0].requests.length === 2);
  const first = client.dispose();
  const second = client.dispose();
  assert.equal(first, second);
  assert.equal(client.state, "disposing");
  const outcome = await compile;
  assert.equal(outcome.reason, "dispose");
  await first;
  assert.equal(client.state, "disposed");
  assert.throws(
    () => client.compile(makePortableRequest().request),
    (error) =>
      error instanceof EngineClientStateError && error.code === "DISPOSED",
  );
  assert.throws(() => client.restart(), EngineClientStateError);
  assert.throws(() => client.getCapabilities(), EngineClientStateError);
});

test("dispose aborts an in-progress replacement and prevents publication", async () => {
  let blockReplacement = true;
  const { client, factory } = await create({
    async handshake(request, index) {
      if (index > 0 && blockReplacement) return new Promise(() => {});
      const defaultFactory = await makeFactory();
      return defaultFactory.handshake(request, index);
    },
  });
  const restarting = client.restart();
  await waitFor(() => factory.endpoints.length === 2);
  const disposing = client.dispose();
  await disposing;
  await restarting;
  blockReplacement = false;
  assert.equal(client.state, "disposed");
  assert.throws(() => client.getEndpoint(), EngineClientStateError);
});

test("idle endpoint crash triggers one replacement and stale close cannot affect it", async () => {
  const { client, factory } = await create();
  const old = factory.endpoints[0];
  old.crash({ exitCode: 9 });
  await waitFor(() => factory.endpoints.length === 2 && client.state === "ready");
  assert.equal(client.getEndpoint().generation, 2);
  old.crash({ exitCode: 10 });
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(factory.endpoints.length, 2);
  assert.equal(client.state, "ready");
  await client.dispose();
});

test("failed replacement remains failed and explicit restart retries monotonically", async () => {
  let rejectSecond = true;
  const defaults = await makeFactory();
  const { client, factory } = await create({
    async handshake(request, index) {
      const response = await defaults.handshake(request, index);
      if (index === 1 && rejectSecond) {
        response.payload.endpoint_instance_id = "fake-endpoint-1";
      }
      return response;
    },
  });
  await assert.rejects(client.restart(), (error) => {
    assert.ok(error instanceof EngineClientTransportError);
    assert.equal(error.code, "REPLACEMENT_FAILED");
    return true;
  });
  assert.equal(client.state, "failed");
  assert.throws(() => client.compile(makePortableRequest().request), (error) => {
    return error instanceof EngineClientStateError && error.code === "FAILED";
  });
  rejectSecond = false;
  await client.restart();
  assert.equal(client.state, "ready");
  assert.equal(client.getEndpoint().generation, 3);
  assert.equal(factory.endpoints.length, 3);
  await client.dispose();
});

test("response observed before a later abort stays engine-originated", async () => {
  const response = await rejectedResponse("placeholder");
  const { client } = await create({
    compile(request) {
      const copy = structuredClone(response);
      copy.request_id = request.request_id;
      return { response: copy, blobs: [] };
    },
  });
  const controller = new AbortController();
  const promise = client.compile(makePortableRequest().request, {
    signal: controller.signal,
  });
  const outcome = await promise;
  controller.abort();
  assert.equal(outcome.origin, "engine");
  assert.equal(outcome.outcome, "rejected");
  await client.dispose();
});

test("negotiation rejects correlation, release, transport, capability, and identity faults", async () => {
  const cases = [
    ["correlation", (response) => (response.request_id = "wrong")],
    [
      "release",
      (response) =>
        (response.payload.release_manifest_digest = `sha256:${"1".repeat(64)}`),
    ],
    [
      "transport",
      (response) => (response.payload.capability_manifest.transports = ["other"]),
    ],
    [
      "capability",
      (response) => (response.payload.capability_statuses[0].enabled = false),
    ],
  ];
  for (const [name, mutate] of cases) {
    const defaults = await makeFactory();
    const factory = await makeFactory({
      async handshake(request, index) {
        const response = await defaults.handshake(request, index);
        mutate(response);
        if (name === "capability") {
          response.payload.capability_statuses[0].unavailable_reason = "unsupported";
        }
        return response;
      },
    });
    await assert.rejects(
      createInternalEngineClient({
        transportFactory: factory,
        protocolCollectors: collectors,
        options: creationOptions,
      }),
      (error) =>
        error instanceof EngineClientDecodeError ||
        error instanceof EngineClientTransportError,
    );
    assert.equal(factory.endpoints[0].stopped, true, name);
  }
});

test("creation timeout is bounded and destroys the unnegotiated endpoint", async () => {
  const factory = await makeFactory({
    create(generation, owner) {
      const endpoint = new StrictFakeTransport(owner, generation, 0);
      endpoint.ready = new Promise(() => {});
      owner.endpoints.push(endpoint);
      return endpoint;
    },
  });
  await assert.rejects(
    createInternalEngineClient({
      transportFactory: factory,
      protocolCollectors: collectors,
      options: { ...creationOptions, handshakeTimeoutMs: 20 },
    }),
    (error) =>
      error instanceof EngineClientTransportError &&
      error.code === "TIMEOUT_DURING_CREATION",
  );
  assert.equal(factory.endpoints[0].stopped, true);
});
