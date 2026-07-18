// SPDX-License-Identifier: Apache-2.0

import assert from "node:assert/strict";
import { Buffer } from "node:buffer";
import { test } from "node:test";
import {
  EngineClientDecodeError,
  EngineClientDisposeError,
  EngineClientInputError,
  EngineClientStateError,
  EngineClientTransportError,
} from "../dist/index.js";
import { createInternalEngineClient } from "../dist/internal/client.js";
import { defaultRuntime } from "../dist/internal/runtime.js";
import {
  StrictFakeTransport,
  collectors,
  completedDigestLease,
  creationOptions,
  limits,
  makeFactory,
  makePortableRequest,
  rejectedResponse,
  rejectedDigestLease,
  sha256,
  stalledDigestLease,
  successResponse,
  waitFor,
} from "./support.mjs";

const encode = (value) =>
  new TextEncoder().encode(
    typeof value === "string" ? value : JSON.stringify(value),
  ).buffer;

function runtimeWithSha256(startSha256, timerOverrides = {}) {
  return {
    now: () => Date.now(),
    randomBytes: (length) => new Uint8Array(length).fill(11),
    sha256: startSha256,
    transferArrayBuffer: (buffer) =>
      structuredClone(buffer, { transfer: [buffer] }),
    setTimer:
      timerOverrides.setTimer ??
      ((callback, delayMs) => setTimeout(callback, delayMs)),
    clearTimer:
      timerOverrides.clearTimer ?? ((handle) => clearTimeout(handle)),
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

function stalledRuntime(stallCall) {
  let calls = 0;
  let stalled;
  return {
    runtime: runtimeWithSha256((bytes) => {
      calls++;
      if (calls === stallCall) {
        stalled = stalledDigestLease(bytes);
        return stalled;
      }
      return completedDigestLease(bytes);
    }),
    get lease() {
      return stalled;
    },
  };
}

async function bounded(promise, label) {
  let timer;
  try {
    return await Promise.race([
      promise,
      new Promise((_resolve, reject) => {
        timer = setTimeout(
          () => reject(new Error(`${label} exceeded its lifecycle bound`)),
          1_000,
        );
      }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}

function assertStalledLeaseJoined(harness) {
  assert.ok(harness.lease);
  assert.equal(harness.lease.abortCount, 1);
  assert.equal(harness.lease.joinedSettled, true);
  assert.equal(harness.lease.retainsInput, false);
}

function adversarialCloseCases() {
  let rootAccessorCalls = 0;
  let detailAccessorCalls = 0;
  const rootAccessor = { retryable: true };
  Object.defineProperty(rootAccessor, "code", {
    enumerable: true,
    get() {
      rootAccessorCalls++;
      throw new Error("SECRET /private/close-code");
    },
  });
  const accessorDetails = {};
  Object.defineProperty(accessorDetails, "signal", {
    enumerable: true,
    get() {
      detailAccessorCalls++;
      throw new Error("SECRET /private/close-detail");
    },
  });
  return [
    {
      name: "unknown close key",
      settle(transport) {
        transport.closedBox.resolve({
          code: "WORKER_CRASHED",
          retryable: true,
          secretPath: "/Users/private/worker",
        });
      },
    },
    {
      name: "root accessor",
      settle(transport) {
        transport.closedBox.resolve(rootAccessor);
      },
      verify() {
        assert.equal(rootAccessorCalls, 0);
      },
    },
    {
      name: "root Proxy trap",
      settle(transport) {
        transport.closedBox.resolve(new Proxy({}, {
          ownKeys() {
            throw new Error("SECRET /private/close-proxy");
          },
        }));
      },
    },
    {
      name: "invalid code",
      settle(transport) {
        transport.closedBox.resolve({
          code: "SECRET_/Users/private/worker",
          retryable: true,
        });
      },
    },
    {
      name: "invalid retryability",
      settle(transport) {
        transport.closedBox.resolve({
          code: "WORKER_CRASHED",
          retryable: "true",
        });
      },
    },
    {
      name: "hostile details Proxy",
      settle(transport) {
        transport.closedBox.resolve({
          code: "WORKER_CRASHED",
          retryable: true,
          details: new Proxy({}, {
            ownKeys() {
              return ["signal"];
            },
            getOwnPropertyDescriptor() {
              throw new Error("SECRET /private/close-details-proxy");
            },
          }),
        });
      },
    },
    {
      name: "hostile details accessor",
      settle(transport) {
        transport.closedBox.resolve({
          code: "WORKER_CRASHED",
          retryable: true,
          details: accessorDetails,
        });
      },
      verify() {
        assert.equal(detailAccessorCalls, 0);
      },
    },
    {
      name: "rejected close promise",
      settle(transport) {
        transport.closedBox.reject(new Error("SECRET /private/close-rejection"));
      },
    },
  ];
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

test("default SHA-256 leases are exact and hard-abortable", async () => {
  const runtime = defaultRuntime();
  for (const size of [0, 1, 55, 56, 64, 65, 20_000]) {
    const bytes = Uint8Array.from({ length: size }, (_value, index) => index % 251);
    const lease = runtime.sha256(bytes);
    const digest = await lease.result;
    await lease.joined;
    assert.equal(`sha256:${Buffer.from(digest).toString("hex")}`, sha256(bytes));
  }

  const lease = runtime.sha256(new Uint8Array(20_000_000));
  lease.abort();
  lease.abort();
  await assert.rejects(lease.result);
  await lease.joined;
});

test("permanently stalled input digests are aborted and joined for every lifecycle interrupt", async (context) => {
  await context.test("compile timeout", async () => {
    const harness = stalledRuntime(1);
    const { client, factory } = await create(
      undefined,
      { defaultCompileTimeoutMs: 15 },
      harness.runtime,
    );
    const outcome = await bounded(
      client.compile(makePortableRequest().request),
      "compile timeout",
    );
    assert.equal(outcome.origin, "client");
    assert.equal(outcome.reason, "timeout");
    assert.equal(factory.endpoints[0].requests.length, 1);
    assertStalledLeaseJoined(harness);
    await client.dispose();
  });

  await context.test("restart", async () => {
    const harness = stalledRuntime(1);
    const { client, factory } = await create(undefined, {}, harness.runtime);
    const compile = client.compile(makePortableRequest().request);
    await waitFor(() => harness.lease !== undefined);
    const restart = client.restart();
    const outcome = await bounded(compile, "restart compile join");
    await bounded(restart, "restart");
    assert.equal(outcome.origin, "client");
    assert.equal(outcome.reason, "restart");
    assert.equal(factory.endpoints.length, 2);
    assertStalledLeaseJoined(harness);
    await client.dispose();
  });

  await context.test("dispose", async () => {
    const harness = stalledRuntime(1);
    const { client } = await create(undefined, {}, harness.runtime);
    const compile = client.compile(makePortableRequest().request);
    await waitFor(() => harness.lease !== undefined);
    const disposal = client.dispose();
    const outcome = await bounded(compile, "dispose compile join");
    await bounded(disposal, "dispose");
    assert.equal(outcome.origin, "client");
    assert.equal(outcome.reason, "dispose");
    assert.equal(client.state, "disposed");
    assertStalledLeaseJoined(harness);
  });

  await context.test("endpoint close", async () => {
    const harness = stalledRuntime(1);
    const { client, factory } = await create(undefined, {}, harness.runtime);
    const compile = client.compile(makePortableRequest().request);
    await waitFor(() => harness.lease !== undefined);
    factory.endpoints[0].closedBox.resolve({
      code: "WORKER_CRASHED",
      retryable: true,
    });
    await assert.rejects(bounded(compile, "endpoint close"), (error) => {
      assert.ok(error instanceof EngineClientTransportError);
      assert.equal(error.code, "WORKER_CRASHED");
      assert.equal(error.details.replacementSucceeded, true);
      return true;
    });
    assert.equal(client.state, "ready");
    assert.equal(factory.endpoints.length, 2);
    assertStalledLeaseJoined(harness);
    await client.dispose();
  });
});

test("cancellation hard-aborts and joins a permanently stalled output digest", async () => {
  const harness = stalledRuntime(2);
  const { client, factory } = await create(
    {
      async compile(request) {
        return successResponse(request.request_id);
      },
    },
    {},
    harness.runtime,
  );
  const controller = new AbortController();
  const compile = client.compile(makePortableRequest().request, {
    signal: controller.signal,
  });
  await waitFor(() => harness.lease !== undefined);
  controller.abort();
  const outcome = await bounded(compile, "output digest cancellation");
  assert.equal(outcome.origin, "client");
  assert.equal(outcome.reason, "signal");
  assert.equal(factory.endpoints.length, 2);
  assertStalledLeaseJoined(harness);
  await client.dispose();
});

test("abort remains live until output digest validation is terminal", async () => {
  let digestCalls = 0;
  let outputDigestStarted = false;
  let outputLease;
  const runtime = runtimeWithSha256((bytes) => {
    digestCalls++;
    if (digestCalls === 1) return completedDigestLease(bytes);
    outputDigestStarted = true;
    outputLease = stalledDigestLease(bytes);
    return outputLease;
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
  assert.equal(outputLease.abortCount, 1);
  assert.equal(outputLease.joinedSettled, true);
  assert.equal(outputLease.retainsInput, false);
  await client.dispose();
});

test("compile timeout remains live while output digest validation is pending", async () => {
  let digestCalls = 0;
  let outputLease;
  let resolveOutputDigestStarted;
  const outputDigestStarted = new Promise((resolve) => {
    resolveOutputDigestStarted = resolve;
  });
  let compileTimer;
  const runtime = runtimeWithSha256((bytes) => {
    digestCalls++;
    if (digestCalls === 1) return completedDigestLease(bytes);
    outputLease = stalledDigestLease(bytes);
    resolveOutputDigestStarted();
    return outputLease;
  }, {
    setTimer(callback, delayMs) {
      if (delayMs !== 20) return setTimeout(callback, delayMs);
      assert.equal(compileTimer, undefined);
      compileTimer = { callback, cleared: false };
      return compileTimer;
    },
    clearTimer(handle) {
      if (handle === compileTimer) {
        handle.cleared = true;
        return;
      }
      clearTimeout(handle);
    },
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
  await outputDigestStarted;
  assert.ok(compileTimer);
  compileTimer.callback();
  const outcome = await compile;
  assert.equal(outcome.origin, "client");
  assert.equal(outcome.reason, "timeout");
  assert.equal(factory.endpoints.length, 2);
  assert.equal(outputLease.abortCount, 1);
  assert.equal(outputLease.joinedSettled, true);
  assert.equal(outputLease.retainsInput, false);
  assert.equal(compileTimer.cleared, true);
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

test("valid transport close records are snapshotted and details are redacted", async () => {
  const { client, factory } = await create({
    compile() {
      return StrictFakeTransport.PENDING;
    },
  });
  const compile = client.compile(makePortableRequest().request);
  await waitFor(() => factory.endpoints[0].requests.length === 2);
  factory.endpoints[0].closedBox.resolve({
    code: "PROCESS_EXITED",
    retryable: false,
    details: {
      exitCode: 9,
      path: "/Users/private/engine",
    },
  });
  await assert.rejects(compile, (error) => {
    assert.ok(error instanceof EngineClientTransportError);
    assert.equal(error.code, "PROCESS_EXITED");
    assert.equal(error.retryable, false);
    assert.deepEqual({ ...error.details }, {
      exitCode: 9,
      replacementSucceeded: true,
    });
    return true;
  });
  assert.equal(factory.endpoints.length, 2);
  assert.equal(client.getEndpoint().generation, 2);
  await client.dispose();
});

test("safe details enforce a closed grammar and domain for every public key", () => {
  const safeRequestId = `ec1-${"a".repeat(24)}-${"b".repeat(16)}`;
  const safe = new EngineClientTransportError("PROCESS_EXITED", false, {
    outcome: "failed",
    diagnosticCount: 0,
    failureCode: "engine.compile_failed",
    requestId: safeRequestId,
    generation: 1,
    blobCount: 0,
    limit: Number.MAX_SAFE_INTEGER,
    exitCode: 0xffff_ffff,
    signal: "SIGTERM",
    replacementSucceeded: false,
  });
  assert.deepEqual({ ...safe.details }, {
    outcome: "failed",
    diagnosticCount: 0,
    failureCode: "engine.compile_failed",
    requestId: safeRequestId,
    generation: 1,
    blobCount: 0,
    limit: Number.MAX_SAFE_INTEGER,
    exitCode: 0xffff_ffff,
    signal: "SIGTERM",
    replacementSucceeded: false,
  });

  const unsafeStrings = [
    "SECRET /Users/private/engine",
    "https://secret.invalid/worker.js",
    "stderr: fatal\nSECRET",
    "arbitrary secret text",
  ];
  for (const key of ["outcome", "failureCode", "requestId", "signal"]) {
    for (const value of unsafeStrings) {
      const error = new EngineClientTransportError("BROKEN_PIPE", true, {
        [key]: value,
        generation: 2,
      });
      assert.deepEqual({ ...error.details }, { generation: 2 });
      const exposed = JSON.stringify(error);
      for (const fragment of ["SECRET", "private", "https", "stderr", "arbitrary"]) {
        assert.equal(exposed.includes(fragment), false);
      }
    }
  }

  const invalidDomains = new EngineClientTransportError("BROKEN_PIPE", true, {
    outcome: "unknown",
    diagnosticCount: -1,
    failureCode: "not-a-protocol-code",
    requestId: "caller-request",
    generation: 0,
    blobCount: 1.5,
    limit: Number.MAX_SAFE_INTEGER + 1,
    exitCode: -1,
    signal: "TERM",
    replacementSucceeded: "true",
  });
  assert.equal(invalidDomains.details, undefined);
});

test("active and idle unsafe close strings are totally redacted", async (context) => {
  const unsafeDetails = {
    outcome: "SECRET /Users/private/outcome",
    failureCode: "https://secret.invalid/failure",
    requestId: "stderr: request\nSECRET",
    signal: "arbitrary secret text",
    exitCode: 9,
  };

  await context.test("active", async () => {
    const { client, factory } = await create({
      compile() {
        return StrictFakeTransport.PENDING;
      },
    });
    const compile = client.compile(makePortableRequest().request);
    await waitFor(() => factory.endpoints[0].requests.length === 2);
    factory.endpoints[0].closedBox.resolve({
      code: "PROCESS_EXITED",
      retryable: false,
      details: unsafeDetails,
    });
    await assert.rejects(bounded(compile, "unsafe active close"), (error) => {
      assert.ok(error instanceof EngineClientTransportError);
      assert.deepEqual({ ...error.details }, {
        exitCode: 9,
        replacementSucceeded: true,
      });
      const exposed = JSON.stringify(error);
      for (const fragment of ["SECRET", "private", "https", "stderr", "arbitrary"]) {
        assert.equal(exposed.includes(fragment), false);
      }
      return true;
    });
    assert.equal(factory.endpoints[0].stopped, true);
    assert.equal(client.getEndpoint().generation, 2);
    await client.dispose();
  });

  await context.test("idle", async () => {
    const { client, factory } = await create();
    const old = factory.endpoints[0];
    old.crash(unsafeDetails);
    await waitFor(() => factory.endpoints.length === 2 && client.state === "ready");
    assert.equal(old.stopped, true);
    assert.equal(old.closedBox.settled, true);
    assert.equal(client.getEndpoint().generation, 2);
    await client.dispose();
  });
});

test("native closed Promise method overrides stay behind the core boundary", async (context) => {
  const unhandled = [];
  const onUnhandled = (reason) => unhandled.push(reason);
  process.on("unhandledRejection", onUnhandled);
  try {
    for (const mode of ["catch-getter", "then-getter", "then-nonpromise"]) {
      for (const partialFailure of [false, true]) {
        await context.test(
          `${mode} ${partialFailure ? "partial failure" : "owned endpoint"}`,
          async () => {
            let methodReads = 0;
            let hardStops = 0;
            let gracefulStops = 0;
            const factory = await makeFactory({
              create(generation, owner) {
                const endpoint = new StrictFakeTransport(
                  owner,
                  generation,
                  owner.endpoints.length,
                );
                owner.endpoints.push(endpoint);
                if (partialFailure) {
                  endpoint.ready = Promise.resolve({
                    ...limits,
                    maxBuffers: 0,
                  });
                }
                const hardStop = endpoint.terminate.bind(endpoint);
                endpoint.terminate = () => {
                  hardStops++;
                  return hardStop();
                };
                const gracefulStop = endpoint.dispose.bind(endpoint);
                endpoint.dispose = () => {
                  gracefulStops++;
                  return gracefulStop();
                };
                const terminal = endpoint.closedBox.promise;
                if (mode === "catch-getter") {
                  Object.defineProperty(terminal, "catch", {
                    get() {
                      methodReads++;
                      throw new Error("SECRET /private/closed-catch");
                    },
                  });
                } else if (mode === "then-getter") {
                  Object.defineProperty(terminal, "then", {
                    get() {
                      methodReads++;
                      throw new Error("SECRET /private/closed-then");
                    },
                  });
                } else {
                  Object.defineProperty(terminal, "then", {
                    get() {
                      methodReads++;
                      return () => undefined;
                    },
                  });
                }
                endpoint.closed = terminal;
                return endpoint;
              },
            });

            if (partialFailure) {
              await assert.rejects(
                bounded(
                  createInternalEngineClient({
                    transportFactory: factory,
                    protocolCollectors: collectors,
                    options: { ...creationOptions, disposeTimeoutMs: 10 },
                  }),
                  `${mode} partial open`,
                ),
                (error) =>
                  error instanceof EngineClientTransportError &&
                  error.code === "CONNECT_FAILED" &&
                  !JSON.stringify(error).includes("SECRET"),
              );
              assert.equal(hardStops, 1);
              assert.equal(gracefulStops, 0);
            } else {
              const client = await bounded(
                createInternalEngineClient({
                  transportFactory: factory,
                  protocolCollectors: collectors,
                  options: { ...creationOptions, disposeTimeoutMs: 10 },
                }),
                `${mode} creation`,
              );
              assert.equal(client.getEndpoint().generation, 1);
              await bounded(client.dispose(), `${mode} disposal`);
              assert.equal(hardStops, 0);
              assert.equal(gracefulStops, 1);
            }
            assert.equal(methodReads, 0);
            assert.equal(factory.endpoints.length, 1);
            assert.equal(factory.endpoints[0].stopped, true);
            assert.equal(factory.endpoints[0].closedBox.settled, true);
          },
        );
      }
    }
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(unhandled, []);
  } finally {
    process.removeListener("unhandledRejection", onUnhandled);
  }
});

test("transport admission traps are captured once before bounded cleanup", async (context) => {
  const cases = [
    ["ready Proxy throw", "ready", "proxy", "throw"],
    ["ready non-Promise", "ready", "proxy", "noncallable"],
    ["request accessor throw", "request", "accessor", "throw"],
    ["request non-callable", "request", "proxy", "noncallable"],
    ["dispose accessor throw", "dispose", "accessor", "throw"],
    ["dispose non-callable", "dispose", "proxy", "noncallable"],
    ["terminate accessor throw", "terminate", "accessor", "throw"],
    ["terminate non-callable", "terminate", "proxy", "noncallable"],
  ];
  const lifecycleKeys = ["ready", "closed", "request", "dispose", "terminate"];
  for (const [name, trapped, mechanism, behavior] of cases) {
    await context.test(name, async () => {
      const reads = Object.fromEntries(lifecycleKeys.map((key) => [key, 0]));
      let terminateCalls = 0;
      let disposeCalls = 0;
      const factory = await makeFactory({
        create(generation, owner) {
          const endpoint = new StrictFakeTransport(
            owner,
            generation,
            owner.endpoints.length,
          );
          owner.endpoints.push(endpoint);
          const terminate = endpoint.terminate.bind(endpoint);
          endpoint.terminate = () => {
            terminateCalls++;
            return terminate();
          };
          const dispose = endpoint.dispose.bind(endpoint);
          endpoint.dispose = () => {
            disposeCalls++;
            return dispose();
          };
          const invalid = () => {
            if (behavior === "throw") {
              throw new Error(`SECRET /private/${trapped}`);
            }
            return trapped === "ready" ? limits : undefined;
          };
          if (mechanism === "accessor") {
            Object.defineProperty(endpoint, trapped, {
              configurable: true,
              get() {
                reads[trapped]++;
                return invalid();
              },
            });
            return endpoint;
          }
          return new Proxy(endpoint, {
            get(target, property, receiver) {
              if (lifecycleKeys.includes(property)) reads[property]++;
              if (property === trapped) return invalid();
              return Reflect.get(target, property, receiver);
            },
          });
        },
      });
      await assert.rejects(
        bounded(
          createInternalEngineClient({
            transportFactory: factory,
            protocolCollectors: collectors,
            options: { ...creationOptions, disposeTimeoutMs: 10 },
          }),
          name,
        ),
        (error) =>
          error instanceof EngineClientTransportError &&
          error.code === "CONNECT_FAILED" &&
          !JSON.stringify(error).includes("SECRET"),
      );
      if (mechanism === "proxy") {
        for (const key of lifecycleKeys) assert.equal(reads[key], 1);
      } else {
        assert.equal(reads[trapped], 1);
      }
      if (trapped === "terminate") {
        assert.equal(terminateCalls, 0);
        assert.equal(disposeCalls, 1);
      } else {
        assert.equal(terminateCalls, 1);
        assert.equal(disposeCalls, 0);
      }
      assert.equal(factory.endpoints[0].stopped, true);
      assert.equal(factory.endpoints[0].closedBox.settled, true);
    });
  }
});

test("open exchange members and response promises are admitted once", async (context) => {
  const cases = [
    ["request throws", "request-throw", false],
    ["request returns non-exchange", "request-nonexchange", false],
    ["response accessor throws", "response-throw", false],
    ["response is non-Promise", "response-nonpromise", false],
    ["cancel accessor throws", "cancel-throw", false],
    ["plain response then getter throws", "plain-then-throw", false],
    ["native response catch getter", "native-catch", true],
    ["native response then getter", "native-then", true],
    ["native response non-Promise then", "native-then-nonpromise", true],
  ];
  const unhandled = [];
  const onUnhandled = (reason) => unhandled.push(reason);
  process.on("unhandledRejection", onUnhandled);
  try {
    for (const [name, mode, succeeds] of cases) {
      await context.test(name, async () => {
        let requestReads = 0;
        let responseReads = 0;
        let cancelReads = 0;
        let promiseMethodReads = 0;
        let hardStops = 0;
        const factory = await makeFactory({
          create(generation, owner) {
            const endpoint = new StrictFakeTransport(
              owner,
              generation,
              owner.endpoints.length,
            );
            owner.endpoints.push(endpoint);
            const hardStop = endpoint.terminate.bind(endpoint);
            endpoint.terminate = () => {
              hardStops++;
              return hardStop();
            };
            const request = endpoint.request.bind(endpoint);
            Object.defineProperty(endpoint, "request", {
              configurable: true,
              get() {
                requestReads++;
                return (input) => {
                  if (mode === "request-throw") {
                    throw new Error("SECRET /private/request-call");
                  }
                  if (mode === "request-nonexchange") return undefined;
                  const exchange = request(input);
                  const originalResponse = exchange.response;
                  if (mode === "response-throw") {
                    void originalResponse.then(undefined, () => undefined);
                    Object.defineProperty(exchange, "response", {
                      get() {
                        responseReads++;
                        throw new Error("SECRET /private/response-getter");
                      },
                    });
                  } else if (mode === "response-nonpromise") {
                    void originalResponse.then(undefined, () => undefined);
                    Object.defineProperty(exchange, "response", {
                      get() {
                        responseReads++;
                        return undefined;
                      },
                    });
                  } else if (mode === "cancel-throw") {
                    Object.defineProperty(exchange, "cancel", {
                      get() {
                        cancelReads++;
                        throw new Error("SECRET /private/cancel-getter");
                      },
                    });
                  } else if (mode === "plain-then-throw") {
                    void originalResponse.then(undefined, () => undefined);
                    exchange.response = Object.defineProperty({}, "then", {
                      get() {
                        promiseMethodReads++;
                        throw new Error("SECRET /private/response-then");
                      },
                    });
                  } else if (mode === "native-catch") {
                    Object.defineProperty(originalResponse, "catch", {
                      get() {
                        promiseMethodReads++;
                        throw new Error("SECRET /private/response-catch");
                      },
                    });
                  } else if (mode === "native-then") {
                    Object.defineProperty(originalResponse, "then", {
                      get() {
                        promiseMethodReads++;
                        throw new Error("SECRET /private/response-native-then");
                      },
                    });
                  } else if (mode === "native-then-nonpromise") {
                    Object.defineProperty(originalResponse, "then", {
                      get() {
                        promiseMethodReads++;
                        return () => undefined;
                      },
                    });
                  }
                  if (mode !== "response-throw" && mode !== "response-nonpromise") {
                    const response = exchange.response;
                    Object.defineProperty(exchange, "response", {
                      configurable: true,
                      get() {
                        responseReads++;
                        return response;
                      },
                    });
                  }
                  if (mode !== "cancel-throw") {
                    const cancel = exchange.cancel;
                    Object.defineProperty(exchange, "cancel", {
                      configurable: true,
                      get() {
                        cancelReads++;
                        return cancel;
                      },
                    });
                  }
                  return exchange;
                };
              },
            });
            return endpoint;
          },
        });
        const creation = bounded(
          createInternalEngineClient({
            transportFactory: factory,
            protocolCollectors: collectors,
            options: { ...creationOptions, disposeTimeoutMs: 10 },
          }),
          name,
        );
        if (succeeds) {
          const client = await creation;
          assert.equal(requestReads, 1);
          assert.equal(responseReads, 1);
          assert.equal(cancelReads, 1);
          assert.equal(promiseMethodReads, 0);
          await client.dispose();
          assert.equal(hardStops, 0);
        } else {
          await assert.rejects(creation, (error) => {
            assert.ok(error instanceof EngineClientTransportError);
            assert.ok(["CONNECT_FAILED", "TRANSFER_FAILED"].includes(error.code));
            assert.equal(JSON.stringify(error).includes("SECRET"), false);
            return true;
          });
          assert.equal(requestReads, 1);
          if (!mode.startsWith("request-")) {
            assert.equal(responseReads, 1);
            assert.equal(cancelReads, 1);
          }
          if (mode === "plain-then-throw") assert.equal(promiseMethodReads, 1);
          assert.equal(hardStops, 1);
        }
        assert.equal(factory.endpoints.length, 1);
        assert.equal(factory.endpoints[0].stopped, true);
        assert.equal(factory.endpoints[0].closedBox.settled, true);
      });
    }
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(unhandled, []);
  } finally {
    process.removeListener("unhandledRejection", onUnhandled);
  }
});

test("shutdown method results are bounded, redacted, and singly owned", async (context) => {
  const cases = [
    ["throwing dispose", "throw", false, true],
    ["non-Promise dispose", "nonpromise", false, true],
    ["stopping non-Promise dispose", "stopping-nonpromise", false, false],
    ["throwing then getter", "then-getter", false, true],
    ["non-callable then", "then-noncallable", false, true],
    ["native catch getter", "native-catch", true, false],
    ["native then getter", "native-then", true, false],
    ["native non-Promise then", "native-then-nonpromise", true, false],
    ["plain catch getter", "plain-catch", true, false],
    ["reentrant then", "reentrant-then", true, false],
  ];
  const unhandled = [];
  const onUnhandled = (reason) => unhandled.push(reason);
  process.on("unhandledRejection", onUnhandled);
  try {
    for (const [name, mode, clean, expectsHardStop] of cases) {
      await context.test(name, async () => {
        let disposeReads = 0;
        let terminateReads = 0;
        let disposeCalls = 0;
        let terminateCalls = 0;
        let promiseMethodReads = 0;
        let nestedDisposal;
        let client;
        const factory = await makeFactory({
          create(generation, owner) {
            const endpoint = new StrictFakeTransport(
              owner,
              generation,
              owner.endpoints.length,
            );
            owner.endpoints.push(endpoint);
            const dispose = endpoint.dispose.bind(endpoint);
            const terminate = endpoint.terminate.bind(endpoint);
            const stopGracefully = () => {
              endpoint.stopped = true;
              endpoint.closedBox.resolve({
                code: "WORKER_CRASHED",
                retryable: true,
              });
            };
            const disposeMethod = () => {
              disposeCalls++;
              if (mode === "throw") {
                throw new Error("SECRET /private/dispose-call");
              }
              if (mode === "nonpromise") return undefined;
              if (mode === "stopping-nonpromise") {
                stopGracefully();
                return undefined;
              }
              if (mode === "then-getter") {
                return Object.defineProperty({}, "then", {
                  get() {
                    promiseMethodReads++;
                    throw new Error("SECRET /private/dispose-then");
                  },
                });
              }
              if (mode === "then-noncallable") return { then: undefined };
              if (mode === "plain-catch") {
                return {
                  get catch() {
                    promiseMethodReads++;
                    throw new Error("SECRET /private/dispose-catch");
                  },
                  then(resolve) {
                    stopGracefully();
                    resolve();
                  },
                };
              }
              if (mode === "reentrant-then") {
                return {
                  then(resolve, reject) {
                    nestedDisposal = client.dispose();
                    stopGracefully();
                    resolve();
                    reject(new Error("SECRET /private/late-reject"));
                    throw new Error("SECRET /private/late-throw");
                  },
                };
              }
              const returned = dispose();
              const property = mode === "native-catch" ? "catch" : "then";
              Object.defineProperty(returned, property, {
                get() {
                  promiseMethodReads++;
                  if (mode === "native-then-nonpromise") {
                    return () => undefined;
                  }
                  throw new Error(`SECRET /private/dispose-${property}`);
                },
              });
              return returned;
            };
            Object.defineProperty(endpoint, "dispose", {
              configurable: true,
              get() {
                disposeReads++;
                return disposeMethod;
              },
            });
            Object.defineProperty(endpoint, "terminate", {
              configurable: true,
              get() {
                terminateReads++;
                return () => {
                  terminateCalls++;
                  return terminate();
                };
              },
            });
            return endpoint;
          },
        });
        client = await createInternalEngineClient({
          transportFactory: factory,
          protocolCollectors: collectors,
          options: { ...creationOptions, disposeTimeoutMs: 10 },
        });
        const disposal = client.dispose();
        if (clean) {
          await bounded(disposal, name);
        } else {
          await assert.rejects(bounded(disposal, name), (error) => {
            assert.ok(error instanceof EngineClientDisposeError);
            assert.equal(JSON.stringify(error).includes("SECRET"), false);
            return true;
          });
        }
        assert.equal(terminateCalls, expectsHardStop ? 1 : 0);
        assert.equal(disposeReads, 1);
        assert.equal(terminateReads, 1);
        assert.equal(disposeCalls, 1);
        if (mode.startsWith("native-")) assert.equal(promiseMethodReads, 0);
        if (mode === "then-getter") assert.equal(promiseMethodReads, 1);
        if (mode === "plain-catch") assert.equal(promiseMethodReads, 0);
        if (mode === "reentrant-then") assert.equal(nestedDisposal, disposal);
        assert.equal(factory.endpoints[0].stopped, true);
        assert.equal(factory.endpoints[0].closedBox.settled, true);
      });
    }
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(unhandled, []);
  } finally {
    process.removeListener("unhandledRejection", onUnhandled);
  }
});

test("closed is captured once across active and idle getter and Proxy traps", async (context) => {
  const unhandled = [];
  const onUnhandled = (reason) => unhandled.push(reason);
  process.on("unhandledRejection", onUnhandled);
  try {
    for (const mode of ["getter", "proxy"]) {
      for (const activity of ["active", "idle"]) {
        await context.test(`${activity} ${mode}`, async () => {
          let closedReads = 0;
          const factory = await makeFactory({
            create(generation, owner) {
              const endpoint = new StrictFakeTransport(
                owner,
                generation,
                owner.endpoints.length,
              );
              owner.endpoints.push(endpoint);
              if (owner.endpoints.length !== 1) return endpoint;
              const readClosed = () => {
                closedReads++;
                if (closedReads > 1) {
                  throw new Error("SECRET /private/closed-reread");
                }
                return endpoint.closedBox.promise;
              };
              if (mode === "getter") {
                Object.defineProperty(endpoint, "closed", {
                  configurable: true,
                  get: readClosed,
                });
                return endpoint;
              }
              return new Proxy(endpoint, {
                get(target, property, receiver) {
                  if (property === "closed") return readClosed();
                  return Reflect.get(target, property, receiver);
                },
              });
            },
            compile() {
              return StrictFakeTransport.PENDING;
            },
          });
          const client = await createInternalEngineClient({
            transportFactory: factory,
            protocolCollectors: collectors,
            options: creationOptions,
          });
          const old = factory.endpoints[0];
          let compile;
          if (activity === "active") {
            compile = client.compile(makePortableRequest().request);
            await waitFor(() => old.requests.length === 2);
          }
          old.closedBox.resolve({ code: "WORKER_CRASHED", retryable: true });
          if (compile !== undefined) {
            await assert.rejects(bounded(compile, `${activity} ${mode}`));
          }
          await waitFor(
            () => factory.endpoints.length === 2 && client.state === "ready",
          );
          assert.equal(closedReads, 1);
          assert.equal(old.stopped, true);
          assert.equal(old.closedBox.settled, true);
          assert.equal(client.getEndpoint().generation, 2);
          await client.dispose();
        });
      }
    }
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(unhandled, []);
  } finally {
    process.removeListener("unhandledRejection", onUnhandled);
  }
});

test("throwing closed acquisition and hostile thenables replace or settle bounded", async (context) => {
  const unhandled = [];
  const onUnhandled = (reason) => unhandled.push(reason);
  process.on("unhandledRejection", onUnhandled);
  try {
    for (const mode of ["getter", "proxy"]) {
      await context.test(`throwing ${mode}`, async () => {
        let closedReads = 0;
        const factory = await makeFactory({
          create(generation, owner) {
            const endpoint = new StrictFakeTransport(
              owner,
              generation,
              owner.endpoints.length,
            );
            owner.endpoints.push(endpoint);
            if (owner.endpoints.length !== 1) return endpoint;
            const throwClosed = () => {
              closedReads++;
              throw new Error("SECRET /private/closed-acquisition");
            };
            if (mode === "getter") {
              Object.defineProperty(endpoint, "closed", { get: throwClosed });
              return endpoint;
            }
            return new Proxy(endpoint, {
              get(target, property, receiver) {
                if (property === "closed") return throwClosed();
                return Reflect.get(target, property, receiver);
              },
            });
          },
        });
        const client = await createInternalEngineClient({
          transportFactory: factory,
          protocolCollectors: collectors,
          options: { ...creationOptions, disposeTimeoutMs: 10 },
        });
        await waitFor(
          () => factory.endpoints.length === 2 && client.state === "ready",
        );
        assert.equal(closedReads, 1);
        assert.equal(factory.endpoints[0].stopped, true);
        assert.equal(factory.endpoints[0].closedBox.settled, true);
        assert.equal(client.getEndpoint().generation, 2);
        await client.dispose();
      });
    }

    const hostileThenables = [
      () => Promise.reject(new Error("SECRET /private/closed-rejection")),
      () => Object.defineProperty({}, "then", {
        get() {
          throw new Error("SECRET /private/then-getter");
        },
      }),
      () => new Proxy({}, {
        get(_target, property) {
          if (property === "then") {
            throw new Error("SECRET /private/then-proxy");
          }
        },
      }),
      () => ({
        then(_resolve, reject) {
          reject(new Error("SECRET /private/then-rejection"));
          throw new Error("SECRET /private/then-throw");
        },
      }),
    ];
    for (const [index, makeThenable] of hostileThenables.entries()) {
      await context.test(`hostile thenable ${index + 1}`, async () => {
        let closedReads = 0;
        const factory = await makeFactory({
          create(generation, owner) {
            const endpoint = new StrictFakeTransport(
              owner,
              generation,
              owner.endpoints.length,
            );
            owner.endpoints.push(endpoint);
            if (owner.endpoints.length === 1) {
              Object.defineProperty(endpoint, "closed", {
                get() {
                  closedReads++;
                  return makeThenable();
                },
              });
            }
            return endpoint;
          },
        });
        const client = await createInternalEngineClient({
          transportFactory: factory,
          protocolCollectors: collectors,
          options: { ...creationOptions, disposeTimeoutMs: 10 },
        });
        await waitFor(
          () => factory.endpoints.length === 2 && client.state === "ready",
        );
        assert.equal(closedReads, 1);
        assert.equal(factory.endpoints[0].stopped, true);
        assert.equal(client.getEndpoint().generation, 2);
        await client.dispose();
      });
    }

    await context.test("permanently pending thenable", async () => {
      let closedReads = 0;
      const factory = await makeFactory({
        create(generation, owner) {
          const endpoint = new StrictFakeTransport(
            owner,
            generation,
            owner.endpoints.length,
          );
          owner.endpoints.push(endpoint);
          if (owner.endpoints.length === 1) {
            Object.defineProperty(endpoint, "closed", {
              get() {
                closedReads++;
                return { then() {} };
              },
            });
          }
          return endpoint;
        },
      });
      const client = await createInternalEngineClient({
        transportFactory: factory,
        protocolCollectors: collectors,
        options: { ...creationOptions, disposeTimeoutMs: 10 },
      });
      await bounded(client.restart(), "pending closed replacement");
      assert.equal(closedReads, 1);
      assert.equal(factory.endpoints[0].stopped, true);
      assert.equal(factory.endpoints[0].closedBox.settled, true);
      assert.equal(client.state, "ready");
      assert.equal(client.getEndpoint().generation, 2);
      await client.dispose();
    });

    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(unhandled, []);
  } finally {
    process.removeListener("unhandledRejection", onUnhandled);
  }
});

test("a replacement candidate close race becomes stable failed and remains recoverable", async () => {
  const unhandled = [];
  const onUnhandled = (reason) => unhandled.push(reason);
  process.on("unhandledRejection", onUnhandled);
  try {
    let candidateClosedReads = 0;
    const factory = await makeFactory({
      create(generation, owner) {
        const endpoint = new StrictFakeTransport(
          owner,
          generation,
          owner.endpoints.length,
        );
        owner.endpoints.push(endpoint);
        if (owner.endpoints.length === 2) {
          Object.defineProperty(endpoint, "closed", {
            get() {
              candidateClosedReads++;
              throw new Error("SECRET /private/replacement-closed");
            },
          });
        }
        return endpoint;
      },
    });
    const client = await createInternalEngineClient({
      transportFactory: factory,
      protocolCollectors: collectors,
      options: { ...creationOptions, disposeTimeoutMs: 10 },
    });
    factory.endpoints[0].closedBox.resolve({
      code: "WORKER_CRASHED",
      retryable: true,
    });
    await waitFor(() => client.state === "failed");
    assert.notEqual(client.state, "replacing");
    assert.equal(factory.endpoints.length, 2);
    assert.equal(candidateClosedReads, 1);
    assert.equal(factory.endpoints[0].stopped, true);
    await waitFor(() => factory.endpoints[1].stopped === true);

    await bounded(client.restart(), "recovery after replacement close race");
    assert.equal(client.state, "ready");
    assert.equal(client.getEndpoint().generation, 3);
    assert.equal(factory.endpoints.length, 3);
    await client.dispose();
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(unhandled, []);
  } finally {
    process.removeListener("unhandledRejection", onUnhandled);
  }
});

test("dispose racing a rejected captured close is bounded, terminal, and redacted", async () => {
  const unhandled = [];
  const onUnhandled = (reason) => unhandled.push(reason);
  process.on("unhandledRejection", onUnhandled);
  try {
    const { client, factory } = await create(undefined, { disposeTimeoutMs: 10 });
    const old = factory.endpoints[0];
    old.closedBox.reject(new Error("SECRET stderr /Users/private/dispose-race"));
    const result = await bounded(
      client.dispose().then(
        () => "clean",
        (error) => error,
      ),
      "dispose close race",
    );
    assert.ok(result instanceof Error);
    assert.equal(result.code, "DISPOSE_FAILED");
    assert.equal(JSON.stringify(result).includes("SECRET"), false);
    assert.equal(JSON.stringify(result).includes("private"), false);
    assert.equal(client.state, "disposed");
    assert.equal(old.stopped, true);
    assert.equal(old.closedBox.settled, true);
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(unhandled, []);
  } finally {
    process.removeListener("unhandledRejection", onUnhandled);
  }
});

test("active adversarial close notifications use one fallback and never reject the observer", async (context) => {
  const unhandled = [];
  const onUnhandled = (reason) => unhandled.push(reason);
  process.on("unhandledRejection", onUnhandled);
  try {
    for (const closeCase of adversarialCloseCases()) {
      await context.test(closeCase.name, async () => {
        const { client, factory } = await create({
          compile() {
            return StrictFakeTransport.PENDING;
          },
        });
        const compile = client.compile(makePortableRequest().request);
        await waitFor(() => factory.endpoints[0].requests.length === 2);
        closeCase.settle(factory.endpoints[0]);
        await assert.rejects(bounded(compile, closeCase.name), (error) => {
          assert.ok(error instanceof EngineClientTransportError);
          assert.equal(error.code, "WORKER_CRASHED");
          assert.equal(error.retryable, true);
          assert.equal(error.details.replacementSucceeded, true);
          assert.equal(JSON.stringify(error).includes("SECRET"), false);
          assert.equal(JSON.stringify(error).includes("private"), false);
          return true;
        });
        closeCase.verify?.();
        assert.equal(factory.endpoints.length, 2);
        assert.equal(client.state, "ready");
        assert.equal(client.getEndpoint().generation, 2);
        await client.dispose();
      });
    }
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(unhandled, []);
  } finally {
    process.removeListener("unhandledRejection", onUnhandled);
  }
});

test("idle adversarial close notifications retire the dead endpoint without unhandled rejection", async (context) => {
  const unhandled = [];
  const onUnhandled = (reason) => unhandled.push(reason);
  process.on("unhandledRejection", onUnhandled);
  try {
    for (const closeCase of adversarialCloseCases()) {
      await context.test(closeCase.name, async () => {
        const { client, factory } = await create();
        const old = factory.endpoints[0];
        closeCase.settle(old);
        await Promise.resolve();
        if (client.state === "ready") {
          assert.notEqual(client.getEndpoint().generation, 1);
        }
        await waitFor(
          () => factory.endpoints.length === 2 && client.state === "ready",
        );
        closeCase.verify?.();
        assert.equal(old.stopped, true);
        assert.equal(client.getEndpoint().generation, 2);
        await client.dispose();
      });
    }
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(unhandled, []);
  } finally {
    process.removeListener("unhandledRejection", onUnhandled);
  }
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

  const cancelledResponse = await rejectedResponse("cancelled-request");
  cancelledResponse.outcome = "cancelled";
  cancelledResponse.diagnostics = [];
  cancelledResponse.failure = {
    category: "cancelled",
    code: "engine.cancelled.safe",
    message: "cancelled",
    retryable: false,
  };
  const engineCancelled = await create({
    compile() {
      return StrictFakeTransport.PENDING;
    },
    cancel(record) {
      const response = structuredClone(cancelledResponse);
      response.request_id = record.decoded.request_id;
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
  const runtime = runtimeWithSha256((bytes) => {
    calls++;
    if (calls === 1) {
      return rejectedDigestLease(new Error("SECRET crypto /Users/private"));
    }
    return stalledDigestLease(bytes);
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

test("transport factory capabilities are snapshotted once across generations", async () => {
  const factory = await makeFactory();
  const values = {
    transportId: factory.transportId,
    connectFailureCode: factory.connectFailureCode,
    create: factory.create,
  };
  const reads = {
    transportId: 0,
    connectFailureCode: 0,
    create: 0,
  };
  for (const property of Object.keys(values)) {
    Object.defineProperty(factory, property, {
      configurable: true,
      enumerable: true,
      get() {
        reads[property]++;
        return values[property];
      },
    });
  }

  const client = await createInternalEngineClient({
    transportFactory: factory,
    protocolCollectors: collectors,
    options: creationOptions,
  });
  await client.restart();
  await client.dispose();
  assert.deepEqual(reads, {
    transportId: 1,
    connectFailureCode: 1,
    create: 1,
  });
  assert.equal(factory.endpoints.length, 2);
});

test("hostile response rejection identity is redacted before classification", async () => {
  const hostile = new Proxy({}, {
    getPrototypeOf() {
      throw new Error("SECRET /private/rejection-prototype");
    },
  });
  const { client, factory } = await create({
    compile() {
      throw hostile;
    },
  });
  await assert.rejects(client.compile(makePortableRequest().request), (error) => {
    assert.ok(error instanceof EngineClientTransportError);
    assert.equal(error.code, "BROKEN_PIPE");
    assert.equal(error.details.replacementSucceeded, true);
    assert.equal(JSON.stringify(error).includes("SECRET"), false);
    return true;
  });
  assert.equal(factory.endpoints.length, 2);
  await client.dispose();
});

test("reentrant cancellation disposal cannot start a replacement", async () => {
  let client;
  let nestedDisposal;
  const factory = await makeFactory({
    compile() {
      return StrictFakeTransport.PENDING;
    },
    cancel() {
      nestedDisposal = client.dispose();
      return { reusable: false };
    },
  });
  client = await createInternalEngineClient({
    transportFactory: factory,
    protocolCollectors: collectors,
    options: creationOptions,
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
  assert.equal(nestedDisposal, client.dispose());
  await nestedDisposal;
  assert.equal(factory.endpoints.length, 1);
  assert.equal(client.state, "disposed");
});

test("reentrant pending hard stop observes the published disposal single flight", async () => {
  const defaults = await makeFactory();
  let client;
  let nestedDisposal;
  const factory = await makeFactory({
    create(generation, owner) {
      const endpoint = new StrictFakeTransport(
        owner,
        generation,
        owner.endpoints.length,
      );
      owner.endpoints.push(endpoint);
      if (endpoint.endpointIndex > 0) {
        const terminate = endpoint.terminate.bind(endpoint);
        endpoint.terminate = () => {
          nestedDisposal = client.dispose();
          terminate();
        };
      }
      return endpoint;
    },
    handshake(request, index) {
      if (index > 0) return new Promise(() => {});
      return defaults.handshake(request, index);
    },
  });
  client = await createInternalEngineClient({
    transportFactory: factory,
    protocolCollectors: collectors,
    options: creationOptions,
  });
  const replacement = client.restart();
  await waitFor(() => factory.endpoints.length === 2);
  const disposal = client.dispose();
  assert.equal(nestedDisposal, disposal);
  await disposal;
  await replacement;
  assert.equal(client.state, "disposed");
  assert.equal(factory.endpoints[1].stopped, true);
});
