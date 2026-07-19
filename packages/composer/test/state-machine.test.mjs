// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { Composer, ComposerError } from "../dist/state-machine.js";

const operationsEnvelope = JSON.parse(await readFile(new URL("../../../schemas/fixtures/engine/workbench-preview-operations-request.json", import.meta.url), "utf8"));
const validPreview = JSON.parse(await readFile(new URL("../../../schemas/fixtures/engine/workbench-preview-valid-warning.json", import.meta.url), "utf8"));
const conflictPreview = JSON.parse(await readFile(new URL("../../../schemas/fixtures/engine/workbench-preview-conflict-only-response.json", import.meta.url), "utf8")).payload;
const stalePreview = structuredClone(conflictPreview);
stalePreview.conflicts[0].kind = "stale_revision";
const edit = { kind: "semantic_operations", request: operationsEnvelope.payload };

const deferred = () => {
  let resolve;
  let reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
};

test("newer preview wins even when an older response arrives last", async () => {
  const pending = [];
  const composer = new Composer({
    preview: (_edit, signal) => {
      const result = deferred();
      pending.push({ ...result, signal });
      return result.promise;
    },
    apply: async () => ({ persistence: "ephemeral", applied: {} }),
  });
  const first = composer.preview({ id: "first", edit });
  const second = composer.preview({ id: "second", edit });
  assert.equal(pending[0].signal.aborted, true);
  pending[1].resolve({ preview: validPreview });
  await second;
  pending[0].resolve({ preview: conflictPreview });
  await first;
  assert.equal(composer.snapshot().phase, "ready");
  assert.equal(composer.snapshot().intent.id, "second");
});

test("preview cancellation is typed, recoverable, and cannot publish a late response", async () => {
  const request = deferred();
  const composer = new Composer({
    preview: (_edit, signal) => {
      signal.addEventListener("abort", () => request.reject(Object.assign(new Error("cancelled"), { name: "AbortError" })), { once: true });
      return request.promise;
    },
    apply: async () => ({ persistence: "ephemeral", applied: {} }),
  });
  const running = composer.preview({ id: "cancel", edit });
  const cancelled = composer.cancelPreview();
  await running;
  assert.equal(cancelled.failure.code, "composer.cancelled");
  assert.equal(cancelled.failure.recoverable, true);
  assert.equal(composer.snapshot().phase, "failed");
});

test("apply retains preview evidence and distinguishes ephemeral from durable authority", async () => {
  const results = [
    { persistence: "ephemeral", applied: { preview_digest: "ephemeral" } },
    { persistence: "durable", result: { operation_result: { status: "committed" } }, committed_revision: { revision_id: "rev-2" } },
  ];
  const composer = new Composer({
    preview: async () => ({ preview: validPreview, grant_summary: { granted_capabilities: [] } }),
    apply: async () => results.shift(),
  });
  await composer.preview({ id: "ephemeral", edit });
  const ephemeral = await composer.apply();
  assert.equal(ephemeral.apply_result.persistence, "ephemeral");
  assert.equal(ephemeral.apply_result.committed_revision, undefined);
  assert.equal(ephemeral.presentation.preview.authoring_impact_digest, validPreview.authoring_impact_digest);
  await composer.preview({ id: "durable", edit });
  const durable = await composer.apply();
  assert.equal(durable.apply_result.persistence, "durable");
  assert.equal(durable.apply_result.committed_revision.revision_id, "rev-2");
});

test("semantic undo and redo call the host and never fabricate revisions", async () => {
  const inverse = structuredClone(edit);
  let applyCount = 0;
  const composer = new Composer({
    preview: async () => ({ preview: validPreview }),
    apply: async () => ({ persistence: "ephemeral", applied: { ordinal: ++applyCount } }),
  });
  await composer.preview({ id: "change", edit, inverse });
  assert.equal((await composer.apply()).can_undo, true);
  const undone = await composer.undo();
  assert.equal(undone.can_redo, true);
  assert.equal(undone.apply_result.persistence, "ephemeral");
  const redone = await composer.redo();
  assert.equal(redone.can_undo, true);
  assert.equal(applyCount, 3);
});

test("denial, stale revision, unavailable capability, and retry stay typed", async () => {
  let attempt = 0;
  const composer = new Composer({
    preview: async () => {
      attempt += 1;
      if (attempt === 1) return { preview: validPreview, authoring_decision: { outcome: "deny" } };
      if (attempt === 2) return { preview: stalePreview };
      if (attempt === 3) throw new ComposerError({ code: "composer.capability_unavailable", message: "missing", recoverable: true, diagnostics: [], conflicts: [] });
      return { preview: validPreview };
    },
    apply: async () => ({ persistence: "ephemeral", applied: {} }),
  });
  assert.equal((await composer.preview({ id: "typed", edit })).failure.code, "composer.access_denied");
  assert.equal((await composer.retry()).failure.code, "composer.stale_revision");
  assert.equal((await composer.retry()).failure.code, "composer.capability_unavailable");
  assert.equal((await composer.retry()).phase, "ready");
});

test("observable state, invalid commands, host callbacks, and transport failures stay deterministic", async () => {
  const observed = [];
  let failTransport = false;
  const composer = new Composer({
    preview: async () => {
      if (failTransport) throw new Error("network details must not escape");
      return { preview: validPreview };
    },
    apply: async () => ({ persistence: "host_callback", receipt: { id: "host-1" } }),
  });
  const unsubscribe = composer.subscribe((snapshot) => observed.push(snapshot.phase));
  assert.equal(composer.cancelPreview().failure.code, "composer.invalid_state");
  assert.equal((await composer.retry()).failure.code, "composer.invalid_state");
  assert.equal((await composer.apply()).failure.code, "composer.invalid_state");
  assert.equal((await composer.undo()).failure.code, "composer.invalid_state");
  assert.equal((await composer.redo()).failure.code, "composer.invalid_state");
  assert.equal((await composer.preview({ id: "", edit })).failure.code, "composer.validation_failed");
  await composer.preview({ id: "host", edit });
  assert.equal((await composer.apply()).apply_result.persistence, "host_callback");
  failTransport = true;
  assert.equal((await composer.preview({ id: "transport", edit })).failure.code, "composer.transport_failed");
  unsubscribe();
  const count = observed.length;
  await composer.close();
  await composer.close();
  assert.equal(observed.length, count);
  assert.equal((await composer.preview({ id: "closed", edit })).failure.code, "composer.session_closed");
});

test("close aborts an authoritative apply and suppresses its late result", async () => {
  const pendingApply = deferred();
  const composer = new Composer({
    preview: async () => ({ preview: validPreview }),
    apply: (_edit, _preview, signal) => {
      signal.addEventListener("abort", () => pendingApply.resolve({ persistence: "ephemeral", applied: {} }), { once: true });
      return pendingApply.promise;
    },
  });
  await composer.preview({ id: "close-apply", edit });
  const applying = composer.apply();
  const rejectedPreview = await composer.preview({ id: "concurrent-preview", edit });
  assert.equal(rejectedPreview.failure.code, "composer.invalid_state");
  assert.equal(composer.snapshot().phase, "applying");
  await composer.close();
  await applying;
  assert.equal(composer.snapshot().phase, "closed");
});

test("runtime non-commit is recoverable and close suppresses outstanding work", async () => {
  const closePending = deferred();
  let closeCalled = false;
  const composer = new Composer({
    preview: async (value) => value === edit ? { preview: validPreview } : closePending.promise,
    apply: async () => ({ persistence: "runtime_not_committed", result: { operation_result: { status: "rejected" } } }),
    close: async () => { closeCalled = true; },
  });
  await composer.preview({ id: "not-committed", edit });
  assert.equal((await composer.apply()).failure.code, "composer.conflict");
  const otherEdit = structuredClone(edit);
  const outstanding = composer.preview({ id: "close", edit: otherEdit });
  await composer.close();
  closePending.resolve({ preview: validPreview });
  await outstanding;
  assert.equal(composer.snapshot().phase, "closed");
  assert.equal(closeCalled, true);
});
