// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import test from "node:test";
import { createBrowserEditor, BrowserEditorError } from "../dist/index.js";

const generation = {
  document_handle: { endpoint_instance_id: "endpoint-1", value: "document_abcdefghijklmnop" },
  value: "1",
};
const openResult = {
  capabilities: { preview_operations: true, apply_to_handle: true },
  document_generation: generation,
  document_handle: generation.document_handle,
};
const impact = { entries: [] };
const validPreview = {
  authoring_impact: impact,
  authoring_impact_digest: "sha256:impact",
  base_generation: generation,
  changed_source_files: [],
  conflicts: [],
  diagnostics: [],
  preview_digest: "sha256:preview",
  preview_id: { endpoint_instance_id: "endpoint-1", value: "preview_abcdefghijklmnop" },
  proposed_generation: { ...generation, value: "2" },
  required_authoring_capabilities: [],
  resulting_hashes: {},
  semantic_diff: {},
  source_diff: {},
  status: "valid",
};
const edit = { kind: "semantic_operations", request: { batch: { operations: [{ kind: "test" }] }, limits: {}, preconditions: {} } };
const viewData = { kind: "test-view" };
const success = (payload, blobs = []) => ({
  origin: "engine",
  outcome: "success",
  blobs,
  response: { outcome: "success", payload, diagnostics: [], request_id: "test" },
});

function makeEngine(overrides = {}) {
  const calls = [];
  let disposed = false;
  const workbench = {
    openDocument: async (input, options) => { calls.push(["open", input, options]); return success(openResult); },
    previewOperations: async (input, options) => { calls.push(["preview", input, options]); return success(validPreview); },
    previewFragment: async (input, options) => { calls.push(["fragment", input, options]); return success(validPreview); },
    previewSourcePatch: async (input, options) => { calls.push(["source-patch", input, options]); return success(validPreview); },
    applyToHandle: async (input, options) => { calls.push(["apply", input, options]); return success({ document_generation: validPreview.proposed_generation }); },
    materializeView: async (input, options) => { calls.push(["view", input, options]); return success(viewData); },
    closeDocument: async (input, options) => { calls.push(["close", input, options]); return success({ closed: true }); },
    ...overrides,
  };
  return {
    calls,
    client: {
      state: "ready",
      workbench,
      dispose: async () => { disposed = true; },
      getCapabilities: () => manifest,
      getEndpoint: () => ({}),
      hasCapability: () => true,
      compile: async () => ({}),
      restart: async () => {},
    },
    disposed: () => disposed,
  };
}

const assetResolver = () => ({
  resolve: async () => new Uint8Array(),
  put: async () => "asset",
  describeCapability: () => ({}),
});
const manifest = { operations: { "engine.preview_operations": { enabled: true, protocol_version: "1.0" } } };
manifest.operations["engine.open_document"] = { enabled: true, protocol_version: "1.0" };
manifest.operations["engine.apply_to_handle"] = { enabled: true, protocol_version: "1.0" };
const runtimeManifest = { operations: {
  "runtime.open_document": { enabled: true, protocol_version: "1.0" },
  "runtime.preview_operations": { enabled: true, protocol_version: "1.0" },
  "runtime.commit_operations": { enabled: true, protocol_version: "1.0" },
} };
const commitMetadata = { operation_id: "operation_1", idempotency_key: "idempotency_1", trigger: "explicit_save" };

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((resolvePromise, rejectPromise) => { resolve = resolvePromise; reject = rejectPromise; });
  return { promise, resolve, reject };
}

test("local Engine lifecycle preserves requests and reports only ephemeral authority", async () => {
  const engine = makeEngine();
  const editor = createBrowserEditor({ engine_client: engine.client, asset_resolver: assetResolver(), capability_manifest: manifest });
  const opened = await editor.open({ authority: "engine", input: { compile_input: {}, requested_limits: {} } });
  assert.equal(opened.persistence, "ephemeral");
  const preview = await editor.preview(edit);
  assert.equal(preview.authority, "engine");
  assert.equal(engine.calls.find(([name]) => name === "preview")[1], edit.request);
  assert.equal(await editor.materializeView({ kind: "query" }), viewData);
  const applied = await editor.apply(edit);
  assert.equal(applied.persistence, "ephemeral");
  assert.deepEqual(engine.calls.find(([name]) => name === "apply")[1], {
    base_generation: validPreview.base_generation,
    preview_digest: validPreview.preview_digest,
    preview_id: validPreview.preview_id,
  });
  await editor.close();
  await editor.close();
  assert.equal(engine.disposed(), true);
});

test("trusted access, approval, and host persistence remain explicit", async () => {
  const blob = { ref: { blob_id: "replacement-1", digest: "sha256:replacement", lifetime: "request", media_type: "text/plain", size: "3" }, bytes: new Uint8Array([1, 2, 3]) };
  const sourceDiff = { edits: [{ replacement_blob: blob.ref }] };
  const engine = makeEngine({
    applyToHandle: async (input, options) => { engine.calls.push(["apply", input, options]); return success({ document_generation: validPreview.proposed_generation, source_diff: sourceDiff }, [blob]); },
  });
  const events = [];
  const editor = createBrowserEditor({
    engine_client: engine.client,
    asset_resolver: assetResolver(),
    capability_manifest: manifest,
    authoring_access_client: {
      evaluatePreview: async (preview) => { events.push(["evaluate", preview]); return { outcome: "approval_required" }; },
      getEffectiveGrant: async () => ({ granted_capabilities: [] }),
    },
    approval_handler: {
      requestApproval: async (preview) => { events.push(["approve", preview]); return "approved"; },
      reportResult: async (result) => { events.push(["report", result]); },
    },
    document_provider: {
      open: async () => {}, read: async () => ({}), close: async () => {},
      writeWithPrecondition: async (input) => { events.push(["write", input]); return { receipt_id: "receipt-1", persistence_claim: "host_defined" }; },
    },
  });
  await editor.open({ authority: "engine", input: { compile_input: {}, requested_limits: {} } });
  const preview = await editor.preview(edit);
  assert.equal(preview.authority, "trusted_access");
  assert.equal((await editor.apply(edit)).persistence, "host_callback");
  assert.deepEqual(events.map(([name]) => name), ["evaluate", "approve", "write", "report"]);
  const write = events.find(([name]) => name === "write")[1];
  assert.strictEqual(write.blobs[0], blob);
  assert.strictEqual(write.blobs[0].bytes, blob.bytes);
  assert.strictEqual(write.applied.source_diff.edits[0].replacement_blob, blob.ref);
  await editor.close();
});

test("denial and approval cancellation are typed and never reach apply", async () => {
  const engine = makeEngine();
  const editor = createBrowserEditor({
    engine_client: engine.client,
    asset_resolver: assetResolver(),
    capability_manifest: manifest,
    authoring_access_client: {
      evaluatePreview: async () => ({ outcome: "deny" }),
      getEffectiveGrant: async () => ({ granted_capabilities: [] }),
    },
  });
  await editor.open({ authority: "engine", input: { compile_input: {}, requested_limits: {} } });
  await editor.preview(edit);
  await assert.rejects(editor.apply(edit), (error) => error instanceof BrowserEditorError && error.code === "editor.access_denied");
  assert.equal(engine.calls.some(([name]) => name === "apply"), false);
  await editor.close();
});

test("Runtime mode commits the exact preview proof and reports authoritative revision", async () => {
  const engine = makeEngine();
  const committedRevision = { document_id: "doc-1", revision_id: "rev-2", definition_hash: "sha256:def2", graph_hash: "sha256:graph2" };
  const runtimeSession = {
    access_summary: { granted_capabilities: [] }, capability_manifest: manifest,
    committed_revision: { ...committedRevision, revision_id: "rev-1" },
    session: { session_id: "session-1" }, state_input: { kind: "none" },
    working_document: { base_revision: { ...committedRevision, revision_id: "rev-1" }, session: { session_id: "session-1" }, working_generation: "1" },
  };
  const runtimePreview = {
    preview: validPreview, authoring_proof: { proof: "trusted" },
    operation_batch: { marker: "exact-batch" }, authoring_decision: { outcome: "allow" },
    grant_summary: { granted_capabilities: [] },
  };
  const calls = [];
  const runtime = {
    getCapabilities: () => runtimeManifest,
    openDocument: async () => runtimeSession,
    previewEditor: async (value) => { calls.push(["preview", value]); return runtimePreview; },
    commitOperations: async (input) => { calls.push(["commit", input]); return { operation_result: { status: "committed", committed_revision: committedRevision } }; },
    materializeView: async () => viewData,
    closeDocument: async (session) => { calls.push(["close", session]); },
  };
  const editor = createBrowserEditor({
    engine_client: engine.client, runtime_client: runtime, asset_resolver: assetResolver(), capability_manifest: manifest,
    runtime_commit_input_factory: () => ({
      ...commitMetadata,
      session: { malicious: true }, operation_batch: { malicious: true }, authoring_proof: { malicious: true },
    }),
  });
  assert.equal((await editor.open({ authority: "runtime", input: { document_id: "doc-1" } })).persistence, "durable");
  assert.equal((await editor.preview(edit)).authority, "runtime");
  const applied = await editor.apply(edit);
  assert.equal(applied.persistence, "durable");
  assert.equal(applied.committed_revision, committedRevision);
  assert.equal(calls.find(([name]) => name === "commit")[1].authoring_proof, runtimePreview.authoring_proof);
  assert.strictEqual(calls.find(([name]) => name === "commit")[1].operation_batch, runtimePreview.operation_batch);
  assert.strictEqual(calls.find(([name]) => name === "commit")[1].session, runtimeSession.session);
  assert.equal(await editor.materializeView({ kind: "query" }), viewData);
  await editor.close();
});

test("Runtime rejection never fabricates a committed revision", async () => {
  const engine = makeEngine();
  const runtimeSession = { session: { session_id: "session-1" }, committed_revision: {}, working_document: { base_revision: {} } };
  const runtime = {
    getCapabilities: () => runtimeManifest, openDocument: async () => runtimeSession,
    previewEditor: async () => ({ preview: validPreview, authoring_proof: {}, operation_batch: {}, authoring_decision: { outcome: "allow" }, grant_summary: {} }),
    commitOperations: async () => ({ operation_result: { status: "rejected", diagnostics: [] } }),
    materializeView: async () => viewData, closeDocument: async () => {},
  };
  const editor = createBrowserEditor({ engine_client: engine.client, runtime_client: runtime, asset_resolver: assetResolver(), capability_manifest: manifest, runtime_commit_input_factory: () => commitMetadata });
  await editor.open({ authority: "runtime", input: { document_id: "doc-1" } });
  await editor.preview(edit);
  const result = await editor.apply(edit);
  assert.equal(result.persistence, "runtime_not_committed");
  assert.equal(result.committed_revision, undefined);
  await editor.close();
});

test("close aborts pending work, releases every dependency, and prevents reopen", async () => {
  let assetDisposed = false;
  const engine = makeEngine({
    previewOperations: (_input, { signal }) => new Promise((_resolve, reject) => signal.addEventListener("abort", () => reject(Object.assign(new Error("aborted"), { name: "AbortError" })), { once: true })),
  });
  const editor = createBrowserEditor({ engine_client: engine.client, asset_resolver: { ...assetResolver(), dispose: async () => { assetDisposed = true; } }, capability_manifest: manifest });
  await editor.open({ authority: "engine", input: { compile_input: {}, requested_limits: {} } });
  const pending = editor.preview(edit);
  await editor.close();
  await assert.rejects(pending, (error) => error.code === "editor.cancelled");
  assert.equal(assetDisposed, true);
  await assert.rejects(editor.open({ authority: "engine", input: { compile_input: {}, requested_limits: {} } }), (error) => error.code === "editor.invalid_state");
});

test("Composer state, subscriptions, undo, and redo use the same authoritative facade", async () => {
  const engine = makeEngine();
  const observed = [];
  const editor = createBrowserEditor({ engine_client: engine.client, asset_resolver: assetResolver() });
  const unsubscribe = editor.subscribe((snapshot) => observed.push(snapshot.phase));
  await editor.open({ authority: "engine", input: { compile_input: {}, requested_limits: {} } });
  const inverse = structuredClone(edit);
  await editor.preview(edit, { intent_id: "forward", inverse });
  await editor.apply(edit);
  assert.equal(editor.snapshot().can_undo, true);
  const undone = await editor.undo();
  assert.equal(undone.phase, "applied");
  assert.equal(undone.can_redo, true);
  const redone = await editor.redo();
  assert.equal(redone.phase, "applied");
  assert.equal(redone.can_undo, true);
  assert.ok(observed.includes("previewing") && observed.includes("applying"));
  unsubscribe();
  await editor.close();
});

test("all semantic edit variants route through Engine and preview cancellation is typed", async () => {
  let rejectPending;
  const engine = makeEngine();
  const editor = createBrowserEditor({ engine_client: engine.client, asset_resolver: assetResolver() });
  await editor.open({ authority: "engine", input: { compile_input: {}, requested_limits: {} } });
  const fragment = { kind: "fragment", request: {} };
  await editor.preview(fragment);
  assert.equal(engine.calls.some(([name]) => name === "fragment"), true);
  const sourcePatch = { kind: "source_patch", request: {} };
  await editor.preview(sourcePatch);
  assert.equal(engine.calls.some(([name]) => name === "source-patch"), true);
  engine.client.workbench.previewOperations = (_input, { signal }) => new Promise((_resolve, reject) => {
    rejectPending = reject;
    signal.addEventListener("abort", () => reject(Object.assign(new Error("aborted"), { name: "AbortError" })), { once: true });
  });
  const pending = editor.preview(edit);
  await Promise.resolve();
  assert.equal(editor.cancelPreview().failure.code, "composer.cancelled");
  await assert.rejects(pending, (error) => error.code === "editor.cancelled");
  await editor.close();
});

test("transport capability is authoritative and cannot be fabricated by an option manifest", async () => {
  const engine = makeEngine();
  engine.client.getCapabilities = () => ({ operations: {} });
  const editor = createBrowserEditor({ engine_client: engine.client, asset_resolver: assetResolver(), capability_manifest: manifest });
  await assert.rejects(
    editor.open({ authority: "engine", input: { compile_input: {}, requested_limits: {} } }),
    (error) => error.code === "editor.required_capability_unavailable",
  );
  await editor.close();
});

test("required capability options cannot suppress the Browser Editor baseline", async () => {
  const engine = makeEngine();
  engine.client.getCapabilities = () => ({ operations: {} });
  const editor = createBrowserEditor({ engine_client: engine.client, asset_resolver: assetResolver(), required_capabilities: [] });
  await assert.rejects(
    editor.open({ authority: "engine", input: { compile_input: {}, requested_limits: {} } }),
    (error) => error.code === "editor.required_capability_unavailable",
  );
  assert.equal(engine.calls.some(([name]) => name === "open"), false);
  await editor.close();
});

test("approval cancellation and cleanup failures retain their typed channels", async () => {
  const engine = makeEngine();
  const editor = createBrowserEditor({
    engine_client: engine.client,
    asset_resolver: { ...assetResolver(), dispose: async () => { throw new Error("dispose failed"); } },
    authoring_access_client: { evaluatePreview: async () => ({ outcome: "approval_required" }), getEffectiveGrant: async () => ({}) },
    approval_handler: { requestApproval: async () => "cancelled", reportResult: async () => {} },
  });
  await editor.open({ authority: "engine", input: { compile_input: {}, requested_limits: {} } });
  await editor.preview(edit);
  await assert.rejects(editor.apply(edit), (error) => error.code === "editor.approval_cancelled");
  await assert.rejects(editor.close(), (error) => error.code === "editor.transport_failed");
});

test("late superseded previews cannot replace the current apply context", async () => {
  const firstGate = deferred();
  const secondGate = deferred();
  let call = 0;
  const engine = makeEngine({ previewOperations: () => (++call === 1 ? firstGate.promise : secondGate.promise) });
  const editor = createBrowserEditor({ engine_client: engine.client, asset_resolver: assetResolver() });
  await editor.open({ authority: "engine", input: { compile_input: {}, requested_limits: {} } });
  const firstEdit = structuredClone(edit);
  const secondEdit = structuredClone(edit);
  const firstPreview = { ...validPreview, preview_digest: "sha256:first", preview_id: { ...validPreview.preview_id, value: "preview_first_12345678" } };
  const secondPreview = { ...validPreview, preview_digest: "sha256:second", preview_id: { ...validPreview.preview_id, value: "preview_second_12345678" } };
  const first = editor.preview(firstEdit, { intent_id: "first" });
  const second = editor.preview(secondEdit, { intent_id: "second" });
  secondGate.resolve(success(secondPreview));
  assert.equal((await second).preview.preview_digest, "sha256:second");
  firstGate.resolve(success(firstPreview));
  await assert.rejects(first, (error) => error.code === "editor.invalid_state");
  await editor.apply(secondEdit);
  assert.equal(engine.calls.find(([name]) => name === "apply")[1].preview_digest, "sha256:second");
  await editor.close();
});

test("approval_required fails closed without a trusted handler in Engine and Runtime modes", async () => {
  const engine = makeEngine();
  const local = createBrowserEditor({
    engine_client: engine.client, asset_resolver: assetResolver(),
    authoring_access_client: { evaluatePreview: async () => ({ outcome: "approval_required" }), getEffectiveGrant: async () => ({}) },
  });
  await local.open({ authority: "engine", input: { compile_input: {}, requested_limits: {} } });
  await local.preview(edit);
  await assert.rejects(local.apply(edit), (error) => error.code === "editor.access_denied");
  assert.equal(engine.calls.some(([name]) => name === "apply"), false);
  await local.close();

  let committed = false;
  const runtimeSession = { session: { session_id: "session-approval" }, committed_revision: {}, working_document: { base_revision: {} } };
  const runtime = {
    getCapabilities: () => runtimeManifest, openDocument: async () => runtimeSession,
    previewEditor: async () => ({ preview: validPreview, authoring_proof: {}, operation_batch: {}, authoring_decision: { outcome: "approval_required" }, grant_summary: {} }),
    commitOperations: async () => { committed = true; return { operation_result: { status: "rejected", diagnostics: [] } }; },
    materializeView: async () => viewData, closeDocument: async () => {},
  };
  const hosted = createBrowserEditor({ engine_client: makeEngine().client, runtime_client: runtime, asset_resolver: assetResolver(), runtime_commit_input_factory: () => commitMetadata });
  await hosted.open({ authority: "runtime", input: { document_id: "doc-approval" } });
  await hosted.preview(edit);
  await assert.rejects(hosted.apply(edit), (error) => error.code === "editor.access_denied");
  assert.equal(committed, false);
  await hosted.close();
});

test("approval boundaries are never called when Engine and Runtime declare no authoring impact", async () => {
  const noImpactPreview = { ...validPreview };
  delete noImpactPreview.authoring_impact;
  delete noImpactPreview.authoring_impact_digest;
  delete noImpactPreview.required_authoring_capabilities;
  let localApprovalCalls = 0;
  const engine = makeEngine({ previewOperations: async () => success(noImpactPreview) });
  const local = createBrowserEditor({
    engine_client: engine.client, asset_resolver: assetResolver(),
    approval_handler: {
      requestApproval: async () => { localApprovalCalls++; return "approved"; },
      reportResult: async () => { localApprovalCalls++; },
    },
  });
  await local.open({ authority: "engine", input: { compile_input: {}, requested_limits: {} } });
  await local.preview(edit);
  await local.apply(edit);
  assert.equal(localApprovalCalls, 0);
  await local.close();

  let runtimeApprovalCalls = 0;
  const runtimeSession = { session: { session_id: "session-no-impact" }, committed_revision: {}, working_document: { base_revision: {} } };
  const runtime = {
    getCapabilities: () => runtimeManifest, openDocument: async () => runtimeSession,
    previewEditor: async () => ({ preview: noImpactPreview, authoring_proof: {}, operation_batch: {}, authoring_decision: { outcome: "allow" }, grant_summary: {} }),
    commitOperations: async () => ({ operation_result: { status: "rejected", diagnostics: [] } }),
    materializeView: async () => viewData, closeDocument: async () => {},
  };
  const hosted = createBrowserEditor({
    engine_client: makeEngine().client, runtime_client: runtime, asset_resolver: assetResolver(), runtime_commit_input_factory: () => commitMetadata,
    approval_handler: {
      requestApproval: async () => { runtimeApprovalCalls++; return "approved"; },
      reportResult: async () => { runtimeApprovalCalls++; },
    },
  });
  await hosted.open({ authority: "runtime", input: { document_id: "doc-no-impact" } });
  await hosted.preview(edit);
  await hosted.apply(edit);
  assert.equal(runtimeApprovalCalls, 0);
  await hosted.close();
});

test("Engine and Runtime adapters preserve one semantic request and normalized preview result", async () => {
  const engine = makeEngine();
  const local = createBrowserEditor({ engine_client: engine.client, asset_resolver: assetResolver() });
  await local.open({ authority: "engine", input: { compile_input: {}, requested_limits: {} } });
  const localResult = await local.preview(edit);
  assert.strictEqual(engine.calls.find(([name]) => name === "preview")[1], edit.request);

  let runtimeEdit;
  const runtimeSession = { session: { session_id: "session-cross-adapter" }, committed_revision: {}, working_document: { base_revision: {} } };
  const runtime = {
    getCapabilities: () => runtimeManifest, openDocument: async () => runtimeSession,
    previewEditor: async (received) => {
      runtimeEdit = received;
      return { preview: validPreview, authoring_proof: {}, operation_batch: {}, authoring_decision: { outcome: "allow" }, grant_summary: {} };
    },
    commitOperations: async () => ({ operation_result: { status: "rejected", diagnostics: [] } }),
    materializeView: async () => viewData, closeDocument: async () => {},
  };
  const hosted = createBrowserEditor({ engine_client: makeEngine().client, runtime_client: runtime, asset_resolver: assetResolver(), runtime_commit_input_factory: () => commitMetadata });
  await hosted.open({ authority: "runtime", input: { document_id: "doc-cross-adapter" } });
  const runtimeResult = await hosted.preview(edit);
  assert.strictEqual(runtimeEdit, edit);
  const normalize = ({ preview, conflicts, diagnostics }) => ({ preview, conflicts, diagnostics });
  assert.deepEqual(normalize(runtimeResult), normalize(localResult));
  await Promise.all([local.close(), hosted.close()]);
});

test("open exposes authoritative optional capability availability", async () => {
  const engine = makeEngine();
  const editor = createBrowserEditor({
    engine_client: engine.client, asset_resolver: assetResolver(), optional_capabilities: ["engine.materialize_view"],
  });
  const opened = await editor.open({ authority: "engine", input: { compile_input: {}, requested_limits: {} } });
  assert.strictEqual(opened.capabilities.manifest, manifest);
  assert.deepEqual(opened.capabilities.selection.optional_unavailable, [{ status: "unavailable", capability_id: "engine.materialize_view", reason: "not_advertised" }]);
  assert.strictEqual(editor.getCapabilities(), opened.capabilities);
  await editor.close();
  assert.equal(editor.getCapabilities(), undefined);
});

test("close joins an abort-ignoring provider open before dependency cleanup", async () => {
  const gate = deferred();
  let providerClosed = false;
  const engine = makeEngine();
  const editor = createBrowserEditor({
    engine_client: engine.client, asset_resolver: assetResolver(),
    document_provider: {
      open: async () => gate.promise, read: async () => ({}), writeWithPrecondition: async () => ({}),
      close: async () => { providerClosed = true; },
    },
  });
  const opening = editor.open({ authority: "engine", input: { compile_input: {}, requested_limits: {} } });
  await Promise.resolve();
  let closeSettled = false;
  const closing = editor.close().then(() => { closeSettled = true; });
  await Promise.resolve();
  assert.equal(closeSettled, false);
  gate.resolve();
  await assert.rejects(opening, (error) => error.code === "editor.cancelled");
  await closing;
  assert.equal(providerClosed, true);
  assert.equal(engine.calls.some(([name]) => name === "open"), false);
});

test("close joins abort-ignoring materialization and prevents late success", async () => {
  const gate = deferred();
  const engine = makeEngine({ materializeView: async () => gate.promise });
  const editor = createBrowserEditor({ engine_client: engine.client, asset_resolver: assetResolver() });
  await editor.open({ authority: "engine", input: { compile_input: {}, requested_limits: {} } });
  const materialized = editor.materializeView({ kind: "query" });
  await Promise.resolve();
  let closeSettled = false;
  const closing = editor.close().then(() => { closeSettled = true; });
  await Promise.resolve();
  assert.equal(closeSettled, false);
  gate.resolve(success(viewData));
  await assert.rejects(materialized, (error) => error.code === "editor.cancelled");
  await closing;
});

test("close joins an abort-ignoring provider write and prevents late apply success", async () => {
  const gate = deferred();
  const engine = makeEngine();
  const editor = createBrowserEditor({
    engine_client: engine.client, asset_resolver: assetResolver(),
    document_provider: {
      open: async () => {}, read: async () => ({}), close: async () => {},
      writeWithPrecondition: async () => gate.promise,
    },
  });
  await editor.open({ authority: "engine", input: { compile_input: {}, requested_limits: {} } });
  await editor.preview(edit);
  const applying = editor.apply(edit);
  await Promise.resolve();
  let closeSettled = false;
  const closing = editor.close().then(() => { closeSettled = true; });
  await Promise.resolve();
  assert.equal(closeSettled, false);
  gate.resolve({ receipt_id: "late-receipt", persistence_claim: "host_defined" });
  await assert.rejects(applying, (error) => error.code === "editor.cancelled");
  await closing;
});
