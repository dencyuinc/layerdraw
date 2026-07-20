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

const repositoryRoot = new URL("../../../", import.meta.url);
const handshakeFixture = JSON.parse(await readFile(new URL("schemas/fixtures/engine/handshake-success.json", repositoryRoot), "utf8"));

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

function runtimeHandshakeResponse(exchange) {
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
