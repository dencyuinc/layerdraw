// SPDX-License-Identifier: Apache-2.0

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  WailsBindingError,
  createWailsDesktopClient,
  createWailsEngineClient,
  wailsEngineBindingDescriptors,
  wailsRuntimeBindingDescriptors,
} from "../dist/wails.js";
import { EngineClientInputError } from "../dist/index.js";
import {
  creationOptions,
  makePortableRequest,
  rejectedResponse,
} from "./support.mjs";
import {
  assertPortableCompileParityOutcome,
  portableParityInput,
} from "../../engine-wasm/test/shared/real-engine.mjs";

const repositoryRoot = new URL("../../../", import.meta.url);
const handshakeFixture = JSON.parse(await readFile(new URL("schemas/fixtures/engine/handshake-success.json", repositoryRoot), "utf8"));
const runtimeCommitRequest = JSON.parse(await readFile(new URL("schemas/fixtures/runtime/commit-request.json", repositoryRoot), "utf8"));
const runtimeCommitFailure = JSON.parse(await readFile(new URL("schemas/fixtures/runtime/commit-failed.json", repositoryRoot), "utf8"));
const bindingCompatibility = JSON.parse(await readFile(new URL("schemas/fixtures/desktop/wails-binding-compatibility-v1.json", repositoryRoot), "utf8"));
const parityCorpus = JSON.parse(await readFile(new URL("tests/conformance/testdata/engine_compile_parity_v1.json", repositoryRoot), "utf8"));

const methodNames = [
  "EngineApplyToHandle", "EngineClassifyAuthoringImpact", "EngineCloseDocument",
  "EngineCompile", "EngineExecuteQuery", "EngineFindSymbols", "EngineFindUsages",
  "EngineFormatScope", "EngineGetNeighbors", "EngineHandshake", "EngineInspectSubgraph",
  "EngineListModules", "EngineListReferences", "EngineMaterializeView", "EngineOpenDocument",
  "EngineOrganizeWorkspace", "EnginePlanExport", "EnginePreviewFragment",
  "EnginePreviewOperations", "EnginePreviewSourcePatch", "EngineReadDeclarations",
  "EngineReadModules", "EngineReadReferences", "EngineReadRows", "EngineReadScope",
  "EngineReplaceSourceTree", "RuntimeCancelOperation", "RuntimeCommitOperations",
  "RuntimeControlAutosave", "RuntimeCloseDocument", "RuntimeGetOperationResult",
  "RuntimeGetStateSnapshot", "RuntimeHandshake", "RuntimeInspectDocument",
  "RuntimeListRevisions", "RuntimeOpenDocument", "RuntimePreviewOperations",
  "RuntimePreviewRestore", "RuntimeRecoverOperations", "RuntimeSaveDocument", "RuntimeStageAsset",
];

function decodeBase64(value) {
  return Buffer.from(value, "base64");
}

function encodeBase64(value) {
  return Buffer.from(value).toString("base64");
}

function generatedBindings(overrides = {}) {
  const bindings = {};
  for (const name of methodNames) {
    bindings[name] = overrides[name] ?? (async () => { throw new Error(`unexpected generated call ${name}`); });
  }
  return bindings;
}

function handshakeResponse(exchange, mutate = () => {}) {
  const request = JSON.parse(decodeBase64(exchange.control).toString("utf8"));
  const response = structuredClone(handshakeFixture);
  response.request_id = request.request_id;
  response.payload.endpoint_instance_id = "wails-endpoint-1";
  response.payload.capability_manifest.transports = ["wails"];
  response.payload.capability_statuses = [
    ...request.payload.required_capabilities.map((capability_id) => ({ capability_id, enabled: true, protocol_version: "1.0" })),
    ...request.payload.optional_capabilities.map((capability_id) => ({ capability_id, enabled: false, protocol_version: "1.0", unavailable_reason: "unsupported" })),
  ].filter((status, index, all) => all.findIndex((candidate) => candidate.capability_id === status.capability_id) === index);
  mutate(response, request);
  return {
    operation: exchange.operation,
    control: encodeBase64(JSON.stringify(response)),
    blobs: [],
  };
}

function runtimeHandshakeResponse(exchange, mutate = () => {}) {
  const request = JSON.parse(decodeBase64(exchange.control).toString("utf8"));
  const operations = Object.fromEntries(request.payload.required_capabilities.map((operation) => [operation, { enabled: true, protocol_version: "1.0" }]));
  const payload = {
    capability_manifest: {
      limits: {
        max_blob_bytes: { hard_maximum: "16777216", unit: "bytes" },
        max_blob_total_bytes: { hard_maximum: "67108864", unit: "bytes" },
        max_commit_operations: { hard_maximum: "10000", unit: "items" },
        max_history_items: { hard_maximum: "10000", unit: "items" },
        max_output_bytes: { hard_maximum: "67108864", unit: "bytes" },
        max_state_mutations: { hard_maximum: "10000", unit: "items" },
      },
      manifest_etag: `sha256:${"7".repeat(64)}`,
      operations,
      storage_capabilities: ["assets", "history", "recovery_journal", "state"],
    },
    capability_statuses: request.payload.required_capabilities.map((capability_id) => ({ capability_id, enabled: true, protocol_version: "1.0" })),
    endpoint_instance_id: "wails-runtime-endpoint-1",
    host_release: "0.0.0-dev",
    negotiated_protocols: [{ name: "runtime", version: "1.0", schema_digest: request.payload.protocols[0].versions[0].schema_digest }],
    release_manifest_digest: creationOptions.expectedReleaseManifestDigest,
  };
  const response = {
    diagnostics: [],
    host_release: "0.0.0-dev",
    outcome: "success",
    payload,
    protocol: { name: "runtime", version: "1.0" },
    request_id: request.request_id,
  };
  mutate(response, request);
  return { operation: exchange.operation, control: encodeBase64(JSON.stringify(response)), blobs: [] };
}

function runtimeCommitResponse(exchange, mutate = () => {}) {
  const request = JSON.parse(decodeBase64(exchange.control).toString("utf8"));
  const response = structuredClone(runtimeCommitFailure);
  response.request_id = request.request_id;
  mutate(response, request);
  return { operation: exchange.operation, control: encodeBase64(JSON.stringify(response)), blobs: [] };
}

function options(bindings, extra = {}) {
  return {
    bindings,
    bindingProtocolVersion: "1.0",
    client: creationOptions,
    ...extra,
  };
}

function transportLimits(overrides = {}) {
  return {
    maxControlBytes: 32 * 1024,
    maxControlDepth: 16,
    maxBlobIdBytes: 256,
    maxBuffers: 128,
    maxInputBlobBytes: 16 * 1024 * 1024,
    maxInputTotalBytes: 64 * 1024 * 1024,
    maxOutputBlobBytes: 16 * 1024 * 1024,
    maxOutputTotalBytes: 64 * 1024 * 1024,
    maxResponsePublishBytes: 65 * 1024 * 1024,
    ...overrides,
  };
}

test("Wails adapter performs handshake and preserves compile envelopes and blobs mechanically", async () => {
  const records = [];
  const bindings = generatedBindings({
    async EngineHandshake(exchange) {
      records.push(exchange);
      return handshakeResponse(exchange);
    },
    async EngineCompile(exchange) {
      records.push(exchange);
      const request = JSON.parse(decodeBase64(exchange.control).toString("utf8"));
      const response = await rejectedResponse(request.request_id);
      return { operation: exchange.operation, control: encodeBase64(JSON.stringify(response)), blobs: [] };
    },
  });
  const client = await createWailsEngineClient(options(bindings));
  assert.equal(client.getEndpoint().handshake.capability_manifest.transports[0], "wails");

  const portable = makePortableRequest();
  const result = await client.compile(portable.request, { requestId: "wails-compile-1" });
  assert.equal(result.origin, "engine");
  assert.equal(result.outcome, "rejected");
  const compileExchange = records[1];
  assert.equal(compileExchange.operation, "engine.compile");
  assert.deepEqual(
    [...decodeBase64(compileExchange.blobs[0].bytes)],
    [...portable.bytes],
  );
  const wireRequest = JSON.parse(decodeBase64(compileExchange.control).toString("utf8"));
  assert.equal(wireRequest.request_id, "wails-compile-1");
  assert.deepEqual(wireRequest.payload, portable.request.input);
  await client.dispose();
});

test("generated Engine and Runtime binding closure exactly matches the Desktop owner parity fixture", async () => {
  const fixture = JSON.parse(await readFile(new URL("schemas/fixtures/desktop/owner-binding-parity-v1.json", repositoryRoot), "utf8"));
  const expected = fixture.bindings
    .filter(([, target]) => target === "engine_client" || target === "runtime_client")
    .map(([generatedMethod, target, , operation]) => ({ generatedMethod, operation, target }))
    .sort((left, right) => left.generatedMethod.localeCompare(right.generatedMethod));
  const actual = [...wailsEngineBindingDescriptors, ...wailsRuntimeBindingDescriptors]
    .map((entry) => ({ ...entry }))
    .sort((left, right) => left.generatedMethod.localeCompare(right.generatedMethod));
  assert.deepEqual(actual, expected);
});

test("Desktop compatibility fixture preserves Browser/Wails request semantics and binding decoders", async () => {
  const descriptors = [...wailsEngineBindingDescriptors, ...wailsRuntimeBindingDescriptors];
  for (const expected of bindingCompatibility.bindings.filter((entry) => entry.target !== "registry_client")) {
    assert.ok(descriptors.some((entry) =>
      entry.generatedMethod === expected.generated_method &&
      entry.target === expected.target &&
      entry.operation === expected.operation));
    const browserText = await readFile(new URL(expected.browser_request_fixture, repositoryRoot), "utf8");
    const desktopText = await readFile(new URL(expected.desktop_request_fixture, repositoryRoot), "utf8");
    assert.notEqual(browserText, desktopText);
    const browser = JSON.parse(browserText);
    const desktop = JSON.parse(desktopText);
    assert.equal(browser.operation, expected.operation);
    assert.equal(desktop.operation, expected.operation);
    delete browser.request_id;
    delete desktop.request_id;
    assert.deepEqual(desktop, browser);
  }
});

test("Wails client executes every shared portable compile and cancellation vector", async () => {
  let releaseCancellation;
  const cancellationGate = new Promise((resolve) => { releaseCancellation = resolve; });
  const selected = parityCorpus.cases;
  assert.equal(selected.length, 10);
  const byRequest = new Map(selected.map((entry) => [entry.expected.response.request_id, entry]));
  const bindings = generatedBindings({
    EngineHandshake: async (exchange) => handshakeResponse(exchange),
    EngineCompile: async (exchange) => {
      const request = JSON.parse(decodeBase64(exchange.control).toString("utf8"));
      const testCase = byRequest.get(request.request_id);
      assert.notEqual(testCase, undefined);
      if (testCase.execution === "cancel") await cancellationGate;
      const response = structuredClone(testCase.expected.response);
      response.engine_release = "0.0.0-dev";
      response.request_id = request.request_id;
      return {
        operation: exchange.operation,
        control: encodeBase64(JSON.stringify(response)),
        blobs: testCase.expected.blobs.map((blob) => ({ blob_id: blob.blob_id, bytes: blob.bytes_base64 })),
      };
    },
  });
  const client = await createWailsEngineClient(options(bindings));
  for (const testCase of selected.filter((entry) => entry.execution === "compile")) {
    const outcome = await client.compile(portableParityInput(testCase), { requestId: testCase.expected.response.request_id });
    await assertPortableCompileParityOutcome(outcome, testCase, "0.0.0-dev");
  }
  const cancellation = selected.find((entry) => entry.execution === "cancel");
  const controller = new AbortController();
  const pending = client.compile(portableParityInput(cancellation), {
    requestId: cancellation.expected.response.request_id,
    signal: controller.signal,
  });
  controller.abort();
  const cancelled = await pending;
  assert.equal(cancelled.origin, "client");
  assert.equal(cancelled.outcome, "cancelled");
  releaseCancellation();
  await new Promise((resolve) => setImmediate(resolve));
  await client.dispose();
});

test("capabilities come only from the open-time handshake, never generated method presence", async () => {
  const bindings = generatedBindings({
    EngineHandshake: async (exchange) => handshakeResponse(exchange),
  });
  const client = await createWailsEngineClient(options(bindings, {
    client: { ...creationOptions, optionalCapabilities: ["engine.open_document"] },
  }));
  assert.equal(typeof bindings.EngineOpenDocument, "function");
  assert.equal(client.hasCapability("engine.open_document"), false);
  await client.dispose();
});

test("Desktop client implements the existing Runtime facade through its generated handshake", async () => {
  const bindings = generatedBindings({
    EngineHandshake: async (exchange) => handshakeResponse(exchange),
    RuntimeHandshake: async (exchange) => runtimeHandshakeResponse(exchange),
  });
  const desktop = await createWailsDesktopClient({
    ...options(bindings),
    expectedReleaseManifestDigest: creationOptions.expectedReleaseManifestDigest,
  });
  assert.equal(desktop.state, "ready");
  assert.equal(desktop.hasCapability("runtime.open_document"), true);
  assert.equal(desktop.hasCapability("runtime.unknown"), false);
  assert.equal(desktop.engine.getEndpoint().handshake.capability_manifest.transports[0], "wails");
  const invalidCalls = [
    () => desktop.openDocument({}),
    () => desktop.inspectDocument({}),
    () => desktop.previewOperations({}),
    () => desktop.commitOperations({}),
    () => desktop.saveDocument({}),
    () => desktop.controlAutosave({}),
    () => desktop.getStateSnapshot({}),
    () => desktop.listRevisions({}),
    () => desktop.previewRestore({}),
    () => desktop.stageAsset({}, new Uint8Array()),
    () => desktop.closeDocument({}),
    () => desktop.cancelOperation({}),
    () => desktop.getOperationResult({}),
    () => desktop.recoverOperations({}),
  ];
  for (const invoke of invalidCalls) assert.throws(invoke, TypeError);
  await desktop.dispose();
  assert.equal(desktop.state, "disposed");
});

test("Runtime handshake rejects correlation, schema, and capability-closure mismatches", async () => {
  const cases = [
    ["request id", (response) => { response.request_id = "hostile-request"; }, "CORRELATION_MISMATCH"],
    ["schema digest", (response) => { response.payload.negotiated_protocols[0].schema_digest = `sha256:${"a".repeat(64)}`; }, "PROTOCOL_MISMATCH"],
    ["status manifest", (response) => {
      response.payload.capability_statuses[0].enabled = false;
      response.payload.capability_statuses[0].unavailable_reason = "unsupported";
    }, "PROTOCOL_MISMATCH"],
    ["status closure", (response) => { response.payload.capability_statuses.pop(); }, "NEGOTIATION_REJECTED"],
  ];
  for (const [name, mutate, code] of cases) {
    const bindings = generatedBindings({
      EngineHandshake: async (exchange) => handshakeResponse(exchange),
      RuntimeHandshake: async (exchange) => runtimeHandshakeResponse(exchange, mutate),
    });
    await assert.rejects(
      createWailsDesktopClient({
        ...options(bindings),
        expectedReleaseManifestDigest: creationOptions.expectedReleaseManifestDigest,
      }),
      (error) => error.code === code,
      name,
    );
  }
});

test("Desktop creation joins both handshakes and releases either successful sibling", async () => {
  for (const failingSide of ["runtime", "engine"]) {
    let activeSubscriptions = 0;
    let release;
    let delayedCalls = 0;
    const gate = new Promise((resolve) => { release = resolve; });
    const shutdown = {
      subscribe() {
        activeSubscriptions++;
        let active = true;
        return () => {
          if (active) activeSubscriptions--;
          active = false;
        };
      },
    };
    const bindings = generatedBindings({
      EngineHandshake: async (exchange) => {
        if (failingSide === "engine") throw new Error("private engine failure");
        delayedCalls++;
        await gate;
        return handshakeResponse(exchange);
      },
      RuntimeHandshake: async (exchange) => {
        if (failingSide === "runtime") {
          return runtimeHandshakeResponse(exchange, (response) => { response.request_id = "hostile-request"; });
        }
        delayedCalls++;
        await gate;
        return runtimeHandshakeResponse(exchange);
      },
    });
    const creation = createWailsDesktopClient({
      ...options(bindings, { shutdown }),
      expectedReleaseManifestDigest: creationOptions.expectedReleaseManifestDigest,
    });
    let settled = false;
    void creation.then(() => { settled = true; }, () => { settled = true; });
    while (delayedCalls === 0) await new Promise((resolve) => setImmediate(resolve));
    assert.equal(settled, false, `${failingSide} failure returned before sibling ownership joined`);
    release();
    await assert.rejects(creation, (error) =>
      failingSide === "runtime"
        ? error.code === "CORRELATION_MISMATCH"
        : error instanceof Error && !String(error).includes("private"));
    assert.equal(activeSubscriptions, 0, `${failingSide} failure leaked a shutdown subscription`);
  }
});

test("Runtime responses reject mismatched request IDs and fence late cancelled callbacks", async () => {
  let calls = 0;
  let release;
  const gate = new Promise((resolve) => { release = resolve; });
  const bindings = generatedBindings({
    EngineHandshake: async (exchange) => handshakeResponse(exchange),
    RuntimeHandshake: async (exchange) => runtimeHandshakeResponse(exchange),
    RuntimeCommitOperations: async (exchange) => {
      calls++;
      if (calls === 1) return runtimeCommitResponse(exchange, (response) => { response.request_id = "hostile-request"; });
      if (calls === 2) await gate;
      return runtimeCommitResponse(exchange);
    },
  });
  const desktop = await createWailsDesktopClient({
    ...options(bindings),
    expectedReleaseManifestDigest: creationOptions.expectedReleaseManifestDigest,
  });
  await assert.rejects(
    desktop.commitOperations(runtimeCommitRequest.payload, { requestId: "runtime-correlation" }),
    (error) => error.code === "CORRELATION_MISMATCH",
  );
  const controller = new AbortController();
  const pending = desktop.commitOperations(runtimeCommitRequest.payload, {
    requestId: "runtime-late",
    signal: controller.signal,
  });
  while (calls < 2) await new Promise((resolve) => setImmediate(resolve));
  controller.abort();
  await assert.rejects(pending, (error) => error.code === "REQUEST_CANCELLED");
  release();
  await new Promise((resolve) => setImmediate(resolve));
  const recovered = await desktop.commitOperations(runtimeCommitRequest.payload, { requestId: "runtime-recovered" });
  assert.equal(recovered.request_id, "runtime-recovered");
  assert.equal(desktop.state, "ready");
  await desktop.dispose();
});

test("Runtime transport preflights encoded size and control depth before publication", async () => {
  let mode = "oversize";
  const limits = transportLimits({
    maxInputBlobBytes: 64,
    maxInputTotalBytes: 128,
    maxOutputBlobBytes: 64,
    maxOutputTotalBytes: 128,
    maxResponsePublishBytes: 33 * 1024,
  });
  const bindings = generatedBindings({
    EngineHandshake: async (exchange) => handshakeResponse(exchange),
    RuntimeHandshake: async (exchange) => runtimeHandshakeResponse(exchange),
    RuntimeCommitOperations: async (exchange) => {
      if (mode === "oversize") {
        return { operation: exchange.operation, control: encodeBase64(Buffer.alloc(limits.maxControlBytes + 1)), blobs: [] };
      }
      if (mode === "blob") {
        return { operation: exchange.operation, control: encodeBase64(JSON.stringify(runtimeCommitFailure)), blobs: [{ blob_id: "oversize", bytes: encodeBase64(Buffer.alloc(65)) }] };
      }
      if (mode === "count") {
        return { operation: exchange.operation, control: encodeBase64(JSON.stringify(runtimeCommitFailure)), blobs: Array.from({ length: limits.maxBuffers + 1 }, (_, index) => ({ blob_id: `blob-${index}`, bytes: "" })) };
      }
      return { operation: exchange.operation, control: encodeBase64("[".repeat(limits.maxControlDepth + 1) + "]".repeat(limits.maxControlDepth + 1)), blobs: [] };
    },
  });
  const desktop = await createWailsDesktopClient({
    ...options(bindings, { transportLimits: limits }),
    expectedReleaseManifestDigest: creationOptions.expectedReleaseManifestDigest,
  });
  await assert.rejects(desktop.commitOperations(runtimeCommitRequest.payload), (error) => error.code === "MALFORMED_MESSAGE");
  mode = "depth";
  await assert.rejects(desktop.commitOperations(runtimeCommitRequest.payload), (error) => error.code === "MALFORMED_MESSAGE");
  mode = "blob";
  await assert.rejects(desktop.commitOperations(runtimeCommitRequest.payload), (error) => error.code === "MALFORMED_MESSAGE");
  mode = "count";
  await assert.rejects(desktop.commitOperations(runtimeCommitRequest.payload), (error) => error.code === "MALFORMED_MESSAGE");
  const inputBytes = new Uint8Array(65);
  await assert.rejects(desktop.stageAsset({
    session: runtimeCommitRequest.payload.session,
    content_blob: {
      blob_id: "input-oversize",
      digest: `sha256:${"0".repeat(64)}`,
      lifetime: "request",
      media_type: "application/octet-stream",
      size: String(inputBytes.byteLength),
    },
  }, inputBytes), (error) => error.code === "MALFORMED_MESSAGE");
  await desktop.dispose();
});

test("Desktop restart renegotiates both manifests and keeps the replacement transport ready", async () => {
  let engineGeneration = 0;
  let engineHandshakes = 0;
  let runtimeHandshakes = 0;
  const bindings = generatedBindings({
    EngineHandshake: async (exchange) => {
      engineHandshakes++;
      return handshakeResponse(exchange, (response) => {
        response.payload.endpoint_instance_id = `wails-endpoint-${++engineGeneration}`;
      });
    },
    RuntimeHandshake: async (exchange) => {
      runtimeHandshakes++;
      return runtimeHandshakeResponse(exchange);
    },
  });
  const desktop = await createWailsDesktopClient({
    ...options(bindings),
    expectedReleaseManifestDigest: creationOptions.expectedReleaseManifestDigest,
  });
  await desktop.restart();
  assert.equal(desktop.state, "ready");
  assert.equal(engineHandshakes, 2);
  assert.equal(runtimeHandshakes, 2);
  assert.equal(desktop.engine.getEndpoint().generation, 2);
  await desktop.dispose();
});

test("Desktop coalesces concurrent restarts and dispose fences an in-flight replacement", async () => {
  let handshakes = 0;
  let release;
  const gate = new Promise((resolve) => { release = resolve; });
  const bindings = generatedBindings({
    EngineHandshake: async (exchange) => handshakeResponse(exchange),
    RuntimeHandshake: async (exchange) => {
      handshakes++;
      if (handshakes === 2) await gate;
      return runtimeHandshakeResponse(exchange);
    },
  });
  const desktop = await createWailsDesktopClient({
    ...options(bindings),
    expectedReleaseManifestDigest: creationOptions.expectedReleaseManifestDigest,
  });
  const first = desktop.restart();
  const second = desktop.restart();
  assert.equal(first, second);
  while (handshakes < 2) await new Promise((resolve) => setImmediate(resolve));
  const disposed = desktop.dispose();
  release();
  await assert.rejects(first, (error) => error.code === "REPLACEMENT_FAILED");
  await disposed;
  assert.equal(desktop.state, "disposed");
  await assert.rejects(desktop.restart(), (error) => error.code === "REPLACEMENT_FAILED");
});

test("failed Desktop replacement publishes no partial Runtime transport and remains retryable", async () => {
  let engineHandshakes = 0;
  const bindings = generatedBindings({
    EngineHandshake: async (exchange) => {
      engineHandshakes++;
      if (engineHandshakes === 2) throw new Error("private backend failure");
      return handshakeResponse(exchange, (response) => {
        response.payload.endpoint_instance_id = `wails-recovery-${engineHandshakes}`;
      });
    },
    RuntimeHandshake: async (exchange) => runtimeHandshakeResponse(exchange),
  });
  const desktop = await createWailsDesktopClient({
    ...options(bindings),
    expectedReleaseManifestDigest: creationOptions.expectedReleaseManifestDigest,
  });
  await assert.rejects(desktop.restart(), (error) => error.code === "REPLACEMENT_FAILED" && !String(error).includes("private"));
  assert.equal(desktop.state, "failed");
  await desktop.restart();
  assert.equal(desktop.state, "ready");
  assert.equal(desktop.engine.getEndpoint().generation, 3);
  await desktop.dispose();
});

test("binding mismatch and incomplete generated surface fail fast with typed recovery", async () => {
  const bindings = generatedBindings();
  await assert.rejects(
    createWailsEngineClient({ ...options(bindings), bindingProtocolVersion: "2.0" }),
    (error) => error instanceof WailsBindingError && error.code === "BINDING_VERSION_MISMATCH" && error.recovery === "upgrade_desktop",
  );
  delete bindings.EngineCompile;
  await assert.rejects(
    createWailsEngineClient(options(bindings)),
    (error) => error instanceof WailsBindingError && error.code === "BINDING_SURFACE_INCOMPLETE" && error.recovery === "regenerate_bindings",
  );
});

test("public option and generated-binding boundaries snapshot once and fail closed", async () => {
  const bindings = generatedBindings({ EngineHandshake: async (exchange) => handshakeResponse(exchange) });
  await assert.rejects(
    createWailsEngineClient({ ...options(bindings), unexpected: true }),
    (error) => error instanceof EngineClientInputError && error.code === "INVALID_ARGUMENT",
  );
  const hostileBindings = { ...bindings };
  Object.defineProperty(hostileBindings, "EngineCompile", { get() { throw new Error("/private/path secret"); } });
  await assert.rejects(
    createWailsEngineClient(options(hostileBindings)),
    (error) => error instanceof WailsBindingError && error.code === "BINDING_SURFACE_INCOMPLETE" && !String(error).includes("private"),
  );
  await assert.rejects(
    createWailsEngineClient(options(bindings, { transportLimits: { maxControlBytes: 1 } })),
    (error) => error instanceof EngineClientInputError && error.code === "INVALID_ARGUMENT",
  );
  await assert.rejects(
    createWailsDesktopClient({
      ...options(bindings),
      expectedReleaseManifestDigest: creationOptions.expectedReleaseManifestDigest,
      requiredRuntimeCapabilities: ["runtime.handshake"],
      optionalRuntimeCapabilities: ["runtime.handshake"],
    }),
    (error) => error instanceof EngineClientInputError && error.code === "INVALID_ARGUMENT",
  );
});

test("Abort rejects the pending Wails exchange and ignores its late callback", async () => {
  let release;
  let compileCalls = 0;
  const late = new Promise((resolve) => { release = resolve; });
  const bindings = generatedBindings({
    EngineHandshake: async (exchange) => handshakeResponse(exchange),
    EngineCompile: async (exchange) => {
      compileCalls++;
      await late;
      const request = JSON.parse(decodeBase64(exchange.control).toString("utf8"));
      const response = await rejectedResponse(request.request_id);
      return { operation: exchange.operation, control: encodeBase64(JSON.stringify(response)), blobs: [] };
    },
  });
  const client = await createWailsEngineClient(options(bindings));
  const controller = new AbortController();
  const pending = client.compile(makePortableRequest().request, { signal: controller.signal });
  while (compileCalls === 0) await new Promise((resolve) => setImmediate(resolve));
  controller.abort();
  const outcome = await pending;
  assert.equal(outcome.origin, "client");
  assert.equal(outcome.outcome, "cancelled");
  assert.equal(outcome.reason, "signal");
  release();
  await new Promise((resolve) => setImmediate(resolve));
  await client.dispose();
});

test("shutdown rejects pending work deterministically and late Wails completion cannot republish", async () => {
  let shutdownListener;
  let release;
  const late = new Promise((resolve) => { release = resolve; });
  const bindings = generatedBindings({
    EngineHandshake: async (exchange) => handshakeResponse(exchange),
    EngineCompile: async (exchange) => {
      await late;
      const request = JSON.parse(decodeBase64(exchange.control).toString("utf8"));
      const response = await rejectedResponse(request.request_id);
      return { operation: exchange.operation, control: encodeBase64(JSON.stringify(response)), blobs: [] };
    },
  });
  const shutdown = { subscribe(listener) { shutdownListener = listener; return () => { shutdownListener = undefined; }; } };
  const client = await createWailsEngineClient(options(bindings, { shutdown }));
  const pending = client.compile(makePortableRequest().request);
  shutdownListener();
  await assert.rejects(pending, (error) => error.code === "BROKEN_PIPE");
  release();
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(client.state, "failed");
  await client.dispose();
});

test("Wails entrypoint is injection-only and browser-bundle safe", async () => {
  const source = await readFile(new URL("packages/engine-client/dist/wails.js", repositoryRoot), "utf8");
  assert.doesNotMatch(source, /node:|@wailsapp|window\.|globalThis\.|runtime\/runtime/);
  const packageJSON = JSON.parse(await readFile(new URL("packages/engine-client/package.json", repositoryRoot), "utf8"));
  assert.equal(packageJSON.exports["./wails"].import, "./dist/wails.js");
});
