// SPDX-License-Identifier: Apache-2.0

import assert from "node:assert/strict";
import { Buffer } from "node:buffer";
import { createHash } from "node:crypto";
import { test } from "node:test";
import {
  EngineClientDecodeError,
  EngineClientInputError,
  EngineClientStateError,
  EngineClientTransportError,
} from "../dist/index.js";
import { createInternalEngineClient } from "../dist/internal/client.js";
import {
  StrictFakeTransport,
  collectors,
  creationOptions,
  limits,
  makeFactory,
  makePortableRequest,
  rejectedResponse,
  sha256,
  successResponse,
  waitFor,
} from "./support.mjs";

const encode = (value) =>
  new TextEncoder().encode(
    typeof value === "string" ? value : JSON.stringify(value),
  ).buffer;

const digestBytes = (bytes) =>
  new Uint8Array(createHash("sha256").update(bytes).digest());

function deferred() {
  let resolve;
  const promise = new Promise((accepted) => {
    resolve = accepted;
  });
  return { promise, resolve };
}

function runtimeWithSha256(sha256Override) {
  return {
    now: () => Date.now(),
    randomBytes: (length) => new Uint8Array(length).fill(11),
    sha256: sha256Override,
    transferArrayBuffer: (buffer) =>
      structuredClone(buffer, { transfer: [buffer] }),
    setTimer: (callback, delayMs) => setTimeout(callback, delayMs),
    clearTimer: (handle) => clearTimeout(handle),
  };
}

async function create(overrides, options = {}, runtime) {
  const factory = await makeFactory(overrides);
  const client = await createInternalEngineClient({
    transportFactory: factory,
    protocolCollectors: collectors,
    options: { ...creationOptions, ...options },
    ...(runtime === undefined ? {} : { runtime }),
  });
  return { client, factory };
}

function twoBlobRequest(ids = ["first", "second"]) {
  const firstBytes = new TextEncoder().encode("same");
  const secondBytes = new TextEncoder().encode("same");
  const refs = ids.map((blob_id, index) => ({
    blob_id,
    digest: sha256(index === 0 ? firstBytes : secondBytes),
    lifetime: "request",
    media_type: "text/plain; charset=utf-8",
    size: "4",
  }));
  const { request } = makePortableRequest("same");
  request.input.entry_path = "first.ldl";
  request.input.project_source_tree = [
    { blob: refs[0], path: "first.ldl" },
    { blob: refs[1], path: "second.ldl" },
  ];
  request.blobs = [
    { ref: refs[0], bytes: firstBytes },
    { ref: refs[1], bytes: secondBytes },
  ];
  return { request, refs, firstBytes, secondBytes };
}

test("abort remains live until output digest validation is terminal", async () => {
  const gate = deferred();
  let digestCalls = 0;
  let outputDigestStarted = false;
  const runtime = runtimeWithSha256(async (bytes) => {
    digestCalls++;
    if (digestCalls === 1) return digestBytes(bytes);
    outputDigestStarted = true;
    await gate.promise;
    return digestBytes(bytes);
  });
  const { client, factory } = await create(
    {
      async compile(request) {
        return successResponse(request.request_id);
      },
    },
    { disposeTimeoutMs: 20 },
    runtime,
  );
  const controller = new AbortController();
  let settled = false;
  const compile = client
    .compile(makePortableRequest().request, {
      signal: controller.signal,
      timeoutMs: 200,
    })
    .finally(() => {
      settled = true;
    });
  await waitFor(() => outputDigestStarted);
  controller.abort();
  await waitFor(() => settled, 500);
  const outcome = await compile;
  assert.equal(outcome.origin, "client");
  assert.equal(outcome.reason, "signal");
  assert.equal(factory.endpoints.length, 2);
  gate.resolve();
  await client.dispose();
});

test("compile timeout remains live while output digest validation is pending", async () => {
  const gate = deferred();
  let digestCalls = 0;
  let outputDigestStarted = false;
  const runtime = runtimeWithSha256(async (bytes) => {
    digestCalls++;
    if (digestCalls === 1) return digestBytes(bytes);
    outputDigestStarted = true;
    await gate.promise;
    return digestBytes(bytes);
  });
  const { client, factory } = await create(
    {
      async compile(request) {
        return successResponse(request.request_id);
      },
    },
    { cancelGraceMs: 10 },
    runtime,
  );
  const compile = client.compile(makePortableRequest().request, {
    timeoutMs: 20,
  });
  await waitFor(() => outputDigestStarted);
  const outcome = await compile;
  assert.equal(outcome.origin, "client");
  assert.equal(outcome.reason, "timeout");
  assert.equal(factory.endpoints.length, 2);
  gate.resolve();
  await client.dispose();
});

test("a close racing a raw response always retires the endpoint", async () => {
  const { client, factory } = await create({
    async compile(request, _blobs, _index, transport, record) {
      const response = await rejectedResponse(request.request_id);
      queueMicrotask(() => {
        record.responseBox.resolve({ control: encode(response), blobs: [] });
        transport.closedBox.resolve({ code: "WORKER_CRASHED", retryable: true });
      });
      return StrictFakeTransport.PENDING;
    },
  });
  await client.compile(makePortableRequest().request).catch((error) => {
    assert.ok(error instanceof EngineClientTransportError);
  });
  await waitFor(() => factory.endpoints.length === 2 && client.state === "ready");
  assert.equal(client.getEndpoint().generation, 2);
  await client.dispose();
});

test("cancel join validates a fulfilled terminal response before reuse", async () => {
  const malformed = await create({
    compile() {
      return StrictFakeTransport.PENDING;
    },
    cancel(record) {
      record.responseBox.resolve({ control: encode("{malformed"), blobs: [] });
      return { reusable: true };
    },
  });
  const malformedController = new AbortController();
  const malformedCompile = malformed.client.compile(
    makePortableRequest().request,
    { signal: malformedController.signal },
  );
  await waitFor(() => malformed.factory.endpoints[0].requests.length === 2);
  malformedController.abort();
  const malformedOutcome = await malformedCompile;
  assert.equal(malformedOutcome.origin, "client");
  assert.equal(malformed.factory.endpoints.length, 2);
  await malformed.client.dispose();

  const engineCancelled = await create({
    compile() {
      return StrictFakeTransport.PENDING;
    },
    async cancel(record) {
      const response = await rejectedResponse(record.decoded.request_id);
      response.outcome = "cancelled";
      response.diagnostics = [];
      response.failure = {
        category: "cancelled",
        code: "engine.cancelled.safe",
        message: "cancelled",
        retryable: false,
      };
      record.responseBox.resolve({ control: encode(response), blobs: [] });
      return { reusable: true };
    },
  });
  const engineController = new AbortController();
  const engineCompile = engineCancelled.client.compile(
    makePortableRequest().request,
    { signal: engineController.signal },
  );
  await waitFor(() => engineCancelled.factory.endpoints[0].requests.length === 2);
  engineController.abort();
  const engineOutcome = await engineCompile;
  assert.equal(engineOutcome.origin, "engine");
  assert.equal(engineOutcome.outcome, "cancelled");
  assert.equal(engineCancelled.factory.endpoints.length, 1);
  await engineCancelled.client.dispose();
});

test("hostile cancel results fail closed without leaking proxy errors", async () => {
  const { client, factory } = await create({
    compile() {
      return StrictFakeTransport.PENDING;
    },
    cancel() {
      return new Proxy({}, {
        ownKeys() {
          throw new Error("SECRET /private/cancel");
        },
      });
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
  assert.equal(factory.endpoints.length, 2);
  await client.dispose();
});

test("transport exchange IDs are bounded and independent from Engine request IDs", async () => {
  const { client, factory } = await create();
  const requestId = "😀".repeat(128);
  await client.compile(makePortableRequest().request, { requestId });
  const [handshake, compile] = factory.endpoints[0].requests;
  for (const record of [handshake, compile]) {
    assert.ok(Buffer.byteLength(record.input.exchangeId, "utf8") <= 128);
  }
  assert.notEqual(compile.input.exchangeId, requestId);
  assert.equal(compile.decoded.request_id, requestId);
  assert.notEqual(handshake.input.exchangeId, compile.input.exchangeId);
  await client.dispose();
});

test("blob ordering is bytewise UTF-8 in both transport directions", async () => {
  const bmp = "\uE000";
  const astral = "\u{10000}";
  let sent;
  const { client } = await create({
    async compile(request, blobs) {
      sent = blobs.map((blob) => blob.blobId);
      const success = await successResponse(request.request_id);
      const refs = collectors.collectCompileResultBlobRefs(success.response.payload);
      const ids = ["a", bmp, astral];
      refs.forEach((ref, index) => {
        ref.blob_id = ids[index];
      });
      success.blobs = refs
        .map((ref, index) => ({
          blobId: ref.blob_id,
          bytes: success.values[index].slice().buffer,
        }))
        .sort((left, right) =>
          Buffer.compare(Buffer.from(left.blobId), Buffer.from(right.blobId)),
        );
      return success;
    },
  });
  const { request } = twoBlobRequest([astral, bmp]);
  const outcome = await client.compile(request);
  assert.deepEqual(sent, [bmp, astral]);
  assert.equal(outcome.origin, "engine");
  assert.equal(outcome.outcome, "success");
  await client.dispose();
});

test("aliased request buffers are rejected before any transfer detaches", async () => {
  const { client, factory } = await create();
  for (const copyAlias of [false, true]) {
    const { request, refs } = twoBlobRequest();
    const buffer = new TextEncoder().encode("same").buffer;
    request.blobs = [
      { ref: refs[0], bytes: buffer, ownership: "transfer" },
      copyAlias
        ? { ref: refs[1], bytes: new Uint8Array(buffer) }
        : { ref: refs[1], bytes: buffer, ownership: "transfer" },
    ];
    assert.throws(
      () => client.compile(request),
      (error) =>
        error instanceof EngineClientInputError &&
        error.code === "UNSUPPORTED_BYTE_OWNERSHIP",
    );
    assert.equal(buffer.byteLength, 4);
  }
  assert.equal(factory.endpoints[0].requests.length, 1);
  await client.dispose();
});

test("SHA-256 failures are redacted and sequential verification starts no sibling", async () => {
  let calls = 0;
  const never = new Promise(() => {});
  const runtime = runtimeWithSha256(async () => {
    calls++;
    if (calls === 1) throw new Error("SECRET crypto /Users/private");
    return never;
  });
  const { client, factory } = await create(undefined, {}, runtime);
  await assert.rejects(client.compile(twoBlobRequest().request), (error) => {
    assert.ok(error instanceof EngineClientTransportError);
    assert.equal(error.code, "DIGEST_FAILED");
    assert.equal("cause" in error, false);
    assert.equal(error.message.includes("SECRET"), false);
    return true;
  });
  assert.equal(calls, 1);
  assert.equal(factory.endpoints[0].requests.length, 1);
  await client.dispose();
});

test("hostile Proxy traps in requests, nested values, and options are input errors", async () => {
  const { client } = await create();
  const hostile = () =>
    new Proxy({}, {
      getPrototypeOf() {
        throw new Error("SECRET /Users/private/source.ldl");
      },
    });
  const nested = makePortableRequest();
  nested.request.input = hostile();
  for (const invoke of [
    () => client.compile(hostile()),
    () => client.compile(makePortableRequest().request, hostile()),
    () => client.compile(nested.request),
  ]) {
    assert.throws(invoke, (error) => {
      assert.ok(error instanceof EngineClientInputError);
      assert.equal(error.code, "INVALID_ARGUMENT");
      assert.equal(error.message.includes("SECRET"), false);
      return true;
    });
  }
  await client.dispose();
});

test("endpoint release identity is consistent across handshake and compile", async () => {
  const defaults = await makeFactory();
  const badHandshakeFactory = await makeFactory({
    async handshake(request, index) {
      const response = await defaults.handshake(request, index);
      response.engine_release = "9.9.9";
      return response;
    },
  });
  await assert.rejects(
    createInternalEngineClient({
      transportFactory: badHandshakeFactory,
      protocolCollectors: collectors,
      options: creationOptions,
    }),
    (error) =>
      error instanceof EngineClientDecodeError &&
      error.code === "PROTOCOL_MISMATCH",
  );

  const { client, factory } = await create({
    async compile(request) {
      const response = await rejectedResponse(request.request_id);
      response.engine_release = "9.9.9";
      return { response, blobs: [] };
    },
  });
  await assert.rejects(client.compile(makePortableRequest().request), (error) => {
    assert.ok(error instanceof EngineClientDecodeError);
    assert.equal(error.code, "PROTOCOL_MISMATCH");
    return true;
  });
  assert.equal(factory.endpoints.length, 2);
  await client.dispose();
});

test("negotiated control depth is enforced for requests and responses", async () => {
  const tooShallow = await makeFactory({
    readyValue: { ...limits, maxControlDepth: 1 },
  });
  await assert.rejects(
    createInternalEngineClient({
      transportFactory: tooShallow,
      protocolCollectors: collectors,
      options: creationOptions,
    }),
    EngineClientTransportError,
  );
  assert.equal(tooShallow.endpoints[0].requests.length, 0);

  const { client, factory } = await create({
    readyValue: { ...limits, maxControlDepth: 6 },
    async compile(request) {
      return successResponse(request.request_id);
    },
  });
  await assert.rejects(
    client.compile(makePortableRequest().request),
    (error) =>
      error instanceof EngineClientDecodeError &&
      error.code === "MALFORMED_MESSAGE",
  );
  assert.equal(factory.endpoints.length, 2);
  await client.dispose();
});

test("restart rejects synchronously once disposal begins during replacement", async () => {
  const defaults = await makeFactory();
  const { client, factory } = await create({
    async handshake(request, index) {
      if (index > 0) return new Promise(() => {});
      return defaults.handshake(request, index);
    },
  });
  const replacement = client.restart();
  await waitFor(() => factory.endpoints.length === 2);
  const disposal = client.dispose();
  assert.throws(
    () => client.restart(),
    (error) =>
      error instanceof EngineClientStateError && error.code === "DISPOSING",
  );
  await disposal;
  await replacement;
  assert.equal(client.state, "disposed");
});
