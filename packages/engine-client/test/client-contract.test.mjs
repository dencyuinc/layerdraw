// SPDX-License-Identifier: Apache-2.0

import assert from "node:assert/strict";
import { Buffer } from "node:buffer";
import { test } from "node:test";
import {
  EngineClientBackpressureError,
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
  makeFactory,
  makePortableRequest,
  rejectedResponse,
  sha256,
  successResponse,
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

test("creation handshakes once and exposes only deep-frozen negotiated state", async () => {
  const { client, factory } = await create(undefined, {
    requiredCapabilities: ["engine.compile", "engine.compile"],
    optionalCapabilities: ["engine.optional", "engine.compile"],
  });
  assert.equal(client.state, "ready");
  assert.equal(factory.endpoints.length, 1);
  assert.equal(factory.endpoints[0].requests.length, 1);
  const request = factory.endpoints[0].requests[0].decoded;
  assert.deepEqual(request.payload.required_capabilities, ["engine.compile"]);
  assert.deepEqual(request.payload.optional_capabilities, ["engine.optional"]);
  assert.equal(request.payload.client_release, "0.0.0");
  assert.equal(request.payload.protocols[0].supported_range, "1.0..1.0");

  const snapshot = client.getEndpoint();
  assert.equal(snapshot.generation, 1);
  assert.equal(snapshot.handshake.endpoint_instance_id, "fake-endpoint-1");
  assert.ok(Object.isFrozen(snapshot));
  assert.ok(Object.isFrozen(snapshot.handshake));
  assert.ok(Object.isFrozen(client.getCapabilities().operations));
  assert.equal(client.hasCapability("engine.compile"), true);
  assert.equal(client.hasCapability("engine.optional"), false);
  assert.equal(client.hasCapability("not valid"), false);
  assert.throws(() => {
    snapshot.handshake.capability_manifest.transports.push("stdio");
  }, TypeError);
  assert.equal("request" in client, false);
  assert.equal("handshake" in client, false);
  assert.equal("transport" in client, false);
  await client.dispose();
});

test("compile preserves exact engine outcome provenance", async () => {
  const outcomes = ["rejected", "failed", "cancelled"];
  for (const expected of outcomes) {
    const { client } = await create({
      async compile(request) {
        const response = await rejectedResponse(request.request_id);
        if (expected !== "rejected") {
          response.outcome = expected;
          response.diagnostics = [];
          response.failure = {
            category: expected === "cancelled" ? "cancelled" : "io",
            code: `engine.${expected}.safe`,
            message: "safe failure",
            retryable: expected === "failed",
          };
        }
        return { response, blobs: [] };
      },
    });
    const outcome = await client.compile(makePortableRequest().request, {
      requestId: `engine-${expected}`,
    });
    assert.equal(outcome.origin, "engine");
    assert.equal(outcome.outcome, expected);
    assert.deepEqual(outcome.blobs, []);
    assert.equal(outcome.response.request_id, `engine-${expected}`);
    await client.dispose();
  }
});

test("workbench facade routes generated operations without interpreting LDL", async () => {
  const { request } = makePortableRequest();
  const seen = [];
  const { client } = await create({
    async workbench(request, blobs) {
      seen.push({ operation: request.operation, blobs: blobs.length });
      return {
        response: {
          diagnostics: [],
          engine_release: "0.0.0-dev",
          failure: {
            category: "io",
            code: "workbench.test.failed",
            message: "safe failure",
            retryable: false,
            workbench_category: "execution_failed",
          },
          outcome: "failed",
          protocol: { name: "engine", version: "1.0" },
          request_id: request.request_id,
        },
        blobs: [],
      };
    },
  });
  const limits = { max_items: "10", max_output_bytes: "10000" };
  const generation = {
    document_handle: {
      endpoint_instance_id: "fake-endpoint-1",
      value: "document_abcdefghijklmnop",
    },
    value: "1",
  };
  const preconditions = {
    document_generation: generation,
    expected_child_sets: [],
    expected_subject_hashes: [],
    expected_subtree_hashes: [],
  };
  const sourceBytes = new TextEncoder().encode("fragment");
  const sourceBlob = {
    blob_id: "workbench/source",
    digest: sha256(sourceBytes),
    lifetime: "request",
    media_type: "text/plain; charset=utf-8",
    size: String(sourceBytes.byteLength),
  };
  const sourceRange = {
    end_byte: "1",
    module_path: "document.ldl",
    origin: { kind: "project" },
    start_byte: "0",
  };
  const base = {
    document_generation: generation,
    limits,
  };
  const calls = [
    ["openDocument", { compile_input: request.input, requested_limits: limits }, { blobs: request.blobs }],
    ["listModules", base, {}],
    ["readModules", { ...base, modules: [{ module_path: "document.ldl", origin: { kind: "project" } }] }, {}],
    ["findSymbols", { ...base, case_mode: "sensitive", match_mode: "exact", query: "alpha" }, {}],
    ["inspectSubgraph", { ...base, depth: 1, root_addresses: ["ldl:project:p:entity:alpha"] }, {}],
    ["readDeclarations", { ...base, addresses: ["ldl:project:p:entity:alpha"] }, {}],
    ["readRows", { ...base, owner_addresses: ["ldl:project:p:entity:alpha"] }, {}],
    ["getNeighbors", { ...base, depth: 1, direction: "outgoing", entity_addresses: ["ldl:project:p:entity:alpha"] }, {}],
    ["findUsages", { ...base, target_addresses: ["ldl:project:p:entity:alpha"] }, {}],
    ["readScope", { ...base, owner_address: "ldl:project:p:entity:alpha" }, {}],
    ["listReferences", base, {}],
    ["readReferences", { ...base, addresses: ["ldl:project:p:reference:guide"] }, {}],
    ["previewSourcePatch", {
      limits,
      patch: {
        patches: [{
          expected_source_digest: sha256(new Uint8Array([1])),
          replacement_blob: sourceBlob,
          source_range: sourceRange,
        }],
      },
      preconditions,
    }, { blobs: [{ ref: sourceBlob, bytes: sourceBytes }] }],
    ["previewFragment", {
      fragment: {
        allowed_kinds: ["entity_type"],
        fragment_blob: sourceBlob,
        insertion_owner: "ldl:project:p",
        intent: "insert",
      },
      limits,
      preconditions,
    }, { blobs: [{ ref: sourceBlob, bytes: sourceBytes }] }],
    ["formatScope", { limits, preconditions, scope_addresses: ["ldl:project:p:entity:alpha"] }, {}],
    ["organizeWorkspace", { limits, preconditions, strategy: "standard_layout" }, {}],
    ["applyToHandle", {
      base_generation: generation,
      preview_digest: sha256(new Uint8Array([2])),
      preview_id: { endpoint_instance_id: "fake-endpoint-1", value: "preview_abcdefghijklmnop" },
    }, {}],
    ["closeDocument", { document_generation: generation, document_handle: generation.document_handle }, {}],
    ["replaceSourceTree", { compile_input: request.input, expected_generation: generation }, { blobs: request.blobs }],
  ];
  for (const [method, input, options] of calls) {
    const outcome = await client.workbench[method](input, {
      ...options,
      requestId: `wb-${method}`,
    });
    assert.equal(outcome.origin, "engine");
    assert.equal(outcome.outcome, "failed");
    assert.equal(outcome.response.request_id, `wb-${method}`);
    assert.equal(outcome.response.failure.workbench_category, "execution_failed");
    assert.deepEqual(outcome.blobs, []);
  }
  assert.deepEqual(seen, [
    { operation: "engine.open_document", blobs: 1 },
    { operation: "engine.list_modules", blobs: 0 },
    { operation: "engine.read_modules", blobs: 0 },
    { operation: "engine.find_symbols", blobs: 0 },
    { operation: "engine.inspect_subgraph", blobs: 0 },
    { operation: "engine.read_declarations", blobs: 0 },
    { operation: "engine.read_rows", blobs: 0 },
    { operation: "engine.get_neighbors", blobs: 0 },
    { operation: "engine.find_usages", blobs: 0 },
    { operation: "engine.read_scope", blobs: 0 },
    { operation: "engine.list_references", blobs: 0 },
    { operation: "engine.read_references", blobs: 0 },
    { operation: "engine.preview_source_patch", blobs: 1 },
    { operation: "engine.preview_fragment", blobs: 1 },
    { operation: "engine.format_scope", blobs: 0 },
    { operation: "engine.organize_workspace", blobs: 0 },
    { operation: "engine.apply_to_handle", blobs: 0 },
    { operation: "engine.close_document", blobs: 0 },
    { operation: "engine.replace_source_tree", blobs: 1 },
  ]);
  await client.dispose();
});

test("success validates every output and transfers caller-owned bytes", async () => {
  let delivered;
  const { client } = await create({
    async compile(request) {
      delivered = await successResponse(request.request_id);
      return delivered;
    },
  });
  const outcome = await client.compile(makePortableRequest().request);
  assert.equal(outcome.origin, "engine");
  assert.equal(outcome.outcome, "success");
  assert.equal(outcome.blobs.length, 3);
  assert.deepEqual(
    outcome.blobs.map((blob) => new TextDecoder().decode(blob.bytes)),
    ["query-output", "artifact-output", "canonical-output"],
  );
  assert.ok(delivered.blobs.every((blob) => blob.bytes.byteLength === 0));
  const retained = outcome.blobs[0].bytes;
  await client.dispose();
  assert.equal(new TextDecoder().decode(retained), "query-output");
});

test("copy admission snapshots the visible range, including pooled Buffer views", async () => {
  let received;
  const { client } = await create({
    async compile(request, blobs) {
      received = new Uint8Array(blobs[0].bytes).slice();
      return { response: await rejectedResponse(request.request_id), blobs: [] };
    },
  });
  const pool = Buffer.allocUnsafe(128);
  const visible = pool.subarray(19, 25);
  visible.set([1, 2, 3, 4, 5, 6]);
  const { request, ref } = makePortableRequest("123456");
  ref.digest = sha256(visible);
  request.blobs = [{ ref, bytes: visible }];
  const promise = client.compile(request);
  visible.fill(99);
  await promise;
  assert.deepEqual([...received], [1, 2, 3, 4, 5, 6]);
  await client.dispose();
});

test("transfer admission detaches synchronously and preserves bytes", async () => {
  let received;
  const { client } = await create({
    async compile(request, blobs) {
      received = new Uint8Array(blobs[0].bytes).slice();
      return { response: await rejectedResponse(request.request_id), blobs: [] };
    },
  });
  const source = new TextEncoder().encode("transfer-me");
  const buffer = source.slice().buffer;
  const { request, ref } = makePortableRequest("transfer-me");
  request.blobs = [{ ref, bytes: buffer, ownership: "transfer" }];
  const promise = client.compile(request);
  assert.equal(buffer.byteLength, 0);
  await promise;
  assert.equal(new TextDecoder().decode(received), "transfer-me");
  await client.dispose();
});

test("aliases use one exact attachment and conflicting aliases fail synchronously", async () => {
  const { client } = await create();
  const { request, ref } = makePortableRequest();
  request.input.project_source_tree.push({
    blob: structuredClone(ref),
    path: "alias.ldl",
  });
  const accepted = await client.compile(request);
  assert.equal(accepted.outcome, "rejected");

  const conflicting = makePortableRequest();
  conflicting.request.input.project_source_tree.push({
    blob: {
      ...structuredClone(conflicting.ref),
      media_type: "application/octet-stream",
    },
    path: "conflict.ldl",
  });
  assert.throws(
    () => client.compile(conflicting.request),
    (error) =>
      error instanceof EngineClientInputError &&
      error.code === "INVALID_BLOB_TABLE",
  );
  await client.dispose();
});

test("hostile wrappers and byte ownership fail closed without invoking accessors", async () => {
  const { client } = await create();
  let accessed = false;
  const hostile = makePortableRequest();
  hostile.request.blobs = [
    Object.defineProperty({}, "ref", {
      enumerable: true,
      get() {
        accessed = true;
        throw new Error("secret /Users/private/source.ldl");
      },
    }),
  ];
  assert.throws(
    () => client.compile(hostile.request),
    (error) =>
      error instanceof EngineClientInputError &&
      error.code === "INVALID_BLOB_TABLE",
  );
  assert.equal(accessed, false);

  const wrongView = makePortableRequest();
  wrongView.request.blobs = [
    { ref: wrongView.ref, bytes: new Uint16Array(wrongView.bytes.buffer) },
  ];
  assert.throws(
    () => client.compile(wrongView.request),
    (error) =>
      error instanceof EngineClientInputError &&
      error.code === "UNSUPPORTED_BYTE_OWNERSHIP",
  );

  const partialTransfer = makePortableRequest("tiny");
  partialTransfer.request.blobs = [
    {
      ref: partialTransfer.ref,
      bytes: Buffer.from("tiny").buffer,
      ownership: "transfer",
    },
  ];
  assert.throws(
    () => client.compile(partialTransfer.request),
    (error) =>
      error instanceof EngineClientInputError &&
      ["BLOB_SIZE_MISMATCH", "UNSUPPORTED_BYTE_OWNERSHIP"].includes(error.code),
  );

  if (typeof SharedArrayBuffer === "function") {
    const shared = makePortableRequest("shared");
    shared.request.blobs = [
      { ref: shared.ref, bytes: new Uint8Array(new SharedArrayBuffer(6)) },
    ];
    assert.throws(
      () => client.compile(shared.request),
      (error) =>
        error instanceof EngineClientInputError &&
        error.code === "UNSUPPORTED_BYTE_OWNERSHIP",
    );
  }

  let resizableBuffer;
  try {
    resizableBuffer = new ArrayBuffer(8, { maxByteLength: 16 });
  } catch {
    resizableBuffer = undefined;
  }
  if (resizableBuffer?.resizable) {
    const resizable = makePortableRequest("12345678");
    resizable.request.blobs = [
      {
        ref: resizable.ref,
        bytes: resizableBuffer,
        ownership: "transfer",
      },
    ];
    assert.throws(
      () => client.compile(resizable.request),
      (error) =>
        error instanceof EngineClientInputError &&
        error.code === "UNSUPPORTED_BYTE_OWNERSHIP",
    );
    assert.equal(resizableBuffer.byteLength, 8);
  }
  await client.dispose();
});

test("blob tables reject missing, unexpected, duplicate, and non-request refs", async () => {
  const { client } = await create();
  const missing = makePortableRequest();
  missing.request.blobs = [];
  assert.throws(() => client.compile(missing.request), EngineClientInputError);

  const duplicate = makePortableRequest();
  duplicate.request.blobs.push(duplicate.request.blobs[0]);
  assert.throws(() => client.compile(duplicate.request), EngineClientInputError);

  const unexpected = makePortableRequest();
  unexpected.request.blobs.push({
    ref: {
      ...structuredClone(unexpected.ref),
      blob_id: "unexpected/blob",
    },
    bytes: unexpected.bytes,
  });
  assert.throws(() => client.compile(unexpected.request), EngineClientInputError);

  const lifetime = makePortableRequest();
  lifetime.ref.lifetime = "session";
  assert.throws(() => client.compile(lifetime.request), EngineClientInputError);
  await client.dispose();
});

test("size is synchronous, digest is verified before transmission", async () => {
  const { client, factory } = await create();
  const sized = makePortableRequest();
  sized.ref.size = "999";
  assert.throws(
    () => client.compile(sized.request),
    (error) =>
      error instanceof EngineClientInputError &&
      error.code === "BLOB_SIZE_MISMATCH",
  );

  const digested = makePortableRequest();
  digested.ref.digest = `sha256:${"0".repeat(64)}`;
  await assert.rejects(
    client.compile(digested.request),
    (error) =>
      error instanceof EngineClientInputError &&
      error.code === "BLOB_DIGEST_MISMATCH",
  );
  assert.equal(factory.endpoints[0].requests.length, 1, "only handshake sent");
  await client.dispose();
});

test("output missing, unexpected, corrupt, and role-conflicting tables poison and replace", async () => {
  const variants = ["missing", "unexpected", "size", "digest", "conflict"];
  for (const variant of variants) {
    const { client, factory } = await create({
      async compile(request, _blobs, _index, _transport, record) {
        const success = await successResponse(request.request_id);
        if (variant === "missing") success.blobs.pop();
        if (variant === "unexpected") {
          success.blobs.push({ blobId: "zz-unexpected", bytes: new ArrayBuffer(1) });
        }
        if (variant === "size") {
          success.blobs[0].bytes = new ArrayBuffer(99);
        }
        if (variant === "digest") {
          new Uint8Array(success.blobs[0].bytes)[0] ^= 1;
        }
        if (variant === "conflict") {
          const refs = collectors.collectCompileResultBlobRefs(success.response.payload);
          refs[1].blob_id = refs[0].blob_id;
          refs[1].media_type = "application/conflict";
          queueMicrotask(() =>
            record.responseBox.resolve({
              control: new TextEncoder().encode(JSON.stringify(success.response))
                .buffer,
              blobs: success.blobs,
            }),
          );
          return StrictFakeTransport.PENDING;
        }
        success.blobs.sort((left, right) => (left.blobId < right.blobId ? -1 : 1));
        return success;
      },
    });
    await assert.rejects(
      client.compile(makePortableRequest().request),
      EngineClientDecodeError,
    );
    assert.equal(client.state, "ready");
    assert.equal(factory.endpoints.length, 2);
    assert.equal(client.getEndpoint().generation, 2);
    await client.dispose();
  }
});

test("request ID boundaries are scalar, bounded, and reusable after settlement", async () => {
  const { client, factory } = await create();
  for (const invalid of ["", "x".repeat(129), "\ud800", "😀".repeat(129)]) {
    assert.throws(
      () => client.compile(makePortableRequest().request, { requestId: invalid }),
      (error) =>
        error instanceof EngineClientInputError &&
        error.code === "INVALID_REQUEST_ID",
    );
  }
  const max = "😀".repeat(128);
  await client.compile(makePortableRequest().request, { requestId: max });
  await client.compile(makePortableRequest().request, { requestId: max });
  await client.compile(makePortableRequest().request);
  await client.compile(makePortableRequest().request);
  const autoIds = factory.endpoints[0].requests
    .filter((record) => record.operation === "engine.compile")
    .slice(-2)
    .map((record) => record.decoded.request_id);
  assert.equal(new Set(autoIds).size, 2);
  assert.ok(autoIds.every((id) => /^ec1-[0-9a-f]{24}-[0-9a-f]{16}$/.test(id)));
  await client.dispose();
});

test("compile timeout overrides are bounded synchronously", async () => {
  const { client } = await create();
  for (const timeoutMs of [0, -1, 1.5, Number.POSITIVE_INFINITY, 600_001]) {
    assert.throws(
      () => client.compile(makePortableRequest().request, { timeoutMs }),
      (error) =>
        error instanceof EngineClientInputError &&
        error.code === "INVALID_ARGUMENT",
    );
  }
  await client.compile(makePortableRequest().request, { timeoutMs: 600_000 });
  await client.dispose();
});

test("abort listeners and timers are removed on every engine terminal outcome", async () => {
  const { client } = await create();
  let listener;
  let adds = 0;
  let removes = 0;
  const signal = {
    aborted: false,
    addEventListener(type, next) {
      assert.equal(type, "abort");
      adds++;
      listener = next;
    },
    removeEventListener(type, next) {
      assert.equal(type, "abort");
      assert.equal(next, listener);
      removes++;
    },
  };
  await client.compile(makePortableRequest().request, { signal });
  assert.equal(adds, 1);
  assert.equal(removes, 1);
  await client.dispose();
});

test("already-aborted calls do not inspect or retain the request", async () => {
  const { client, factory } = await create();
  const controller = new AbortController();
  controller.abort(new Error("secret reason"));
  let inspected = false;
  const request = Object.defineProperty({}, "input", {
    enumerable: true,
    get() {
      inspected = true;
      throw new Error("must not read");
    },
  });
  const outcome = await client.compile(request, {
    requestId: "pre-aborted",
    signal: controller.signal,
  });
  assert.equal(outcome.origin, "client");
  assert.equal(outcome.reason, "signal");
  assert.equal(inspected, false);
  assert.equal(factory.endpoints[0].requests.length, 1);
  await client.dispose();
});

test("stable errors redact raw adapter failures and never expose cause", async () => {
  const secret = "LDL secret /Users/alice/private.ldl https://worker.invalid";
  const { client } = await create({
    async compile() {
      const error = new Error(secret);
      error.stack = `${secret}\nstack`;
      throw error;
    },
  });
  await assert.rejects(client.compile(makePortableRequest().request), (error) => {
    assert.ok(error instanceof EngineClientTransportError);
    assert.equal(error.code, "BROKEN_PIPE");
    assert.equal("cause" in error, false);
    assert.equal(JSON.stringify(error).includes(secret), false);
    assert.equal(error.message.includes(secret), false);
    assert.equal(error.details.replacementSucceeded, true);
    return true;
  });
  await client.dispose();
});

test("malformed current-generation control is a decode failure with replacement", async () => {
  const { client, factory } = await create({
    async compile(request, blobs, index, transport, record) {
      queueMicrotask(() =>
        record.responseBox.resolve({
          control: new TextEncoder().encode("{not-json").buffer,
          blobs: [],
        }),
      );
      return StrictFakeTransport.PENDING;
    },
  });
  await assert.rejects(
    client.compile(makePortableRequest().request),
    (error) =>
      error instanceof EngineClientDecodeError &&
      error.code === "MALFORMED_MESSAGE" &&
      error.details.replacementSucceeded === true,
  );
  assert.equal(factory.endpoints.length, 2);
  await client.dispose();
});
