// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import test from "node:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";
import { AuthoringRecoveryWorkflow, EditorProvider, classifyAuthoringWorkflow } from "../dist/index.js";

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const idle = Object.freeze({ phase: "idle", sequence: 0, can_undo: false, can_redo: false });
const capabilities = Object.freeze({ authority: "runtime", manifest: { operations: {} }, selection: { available: [], optional_unavailable: [] } });
const durableSession = Object.freeze({ authority: "runtime", persistence: "durable", session: {}, capabilities });
const ephemeralSession = Object.freeze({ authority: "engine", persistence: "ephemeral", session: {}, capabilities });
const edit = Object.freeze({ kind: "semantic_operations", request: { opaque: true } });
const intent = Object.freeze({ id: "intent-one", edit });
const approvalPresentation = Object.freeze({
  preview: { diagnostics: [], conflicts: [], authoring_impact: { entries: [] } },
  authoring_decision: { outcome: "approval_required" },
  grant_summary: { granted_capabilities: [], constrained_capabilities: [] },
});

function makeEditor(initial = idle) {
  let snapshot = initial;
  const listeners = new Set();
  const calls = [];
  const editor = {
    calls,
    snapshot: () => snapshot,
    subscribe(listener) { listeners.add(listener); listener(snapshot); return () => listeners.delete(listener); },
    emit(next) { snapshot = next; for (const listener of listeners) listener(snapshot); },
    getCapabilities: () => capabilities,
    async preview(value, options) { calls.push(["preview", value, options]); return {}; },
    async apply(value) { calls.push(["apply", value]); return { persistence: "durable", committed_revision: {} }; },
    async retry() { calls.push(["retry"]); return snapshot; },
    async undo() { return snapshot; }, async redo() { return snapshot; },
    cancelPreview() { return snapshot; },
  };
  return editor;
}

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function state(snapshot, additions = {}) {
  return { session: durableSession, snapshot, decision: undefined, conflicts: [], error: undefined, pendingAction: undefined, ...additions };
}

function button(renderer, label) {
  return renderer.root.findAllByType("button").find((candidate) => candidate.children.join("") === label);
}

test("workflow classifier keeps denied, stale, conflict, disconnected, approval, and persistence states distinct", () => {
  assert.equal(classifyAuthoringWorkflow(state(idle)), "idle");
  assert.equal(classifyAuthoringWorkflow(state({ ...idle, phase: "ready" })), "review");
  assert.equal(classifyAuthoringWorkflow(state({ ...idle, phase: "ready" }, { decision: { outcome: "approval_required" } })), "approval-required");
  assert.equal(classifyAuthoringWorkflow(state({ ...idle, phase: "ready" }, { decision: { outcome: "deny" } })), "denied");
  assert.equal(classifyAuthoringWorkflow(state({ ...idle, phase: "failed", failure: { code: "composer.stale_revision" } })), "stale");
  assert.equal(classifyAuthoringWorkflow(state({ ...idle, phase: "failed", failure: { code: "composer.conflict" } })), "conflict");
  assert.equal(classifyAuthoringWorkflow(state(idle), { status: "disconnected", reason: "runtime restarted" }), "disconnected");
  assert.equal(classifyAuthoringWorkflow(state(idle, { error: { code: "editor.transport_failed" } })), "disconnected");
  assert.equal(classifyAuthoringWorkflow(state({ ...idle, phase: "applied", apply_result: { persistence: "durable" } })), "applied-durable");
  assert.equal(classifyAuthoringWorkflow(state({ ...idle, phase: "applied", apply_result: { persistence: "ephemeral" } })), "applied-ephemeral");
  assert.equal(classifyAuthoringWorkflow(state({ ...idle, phase: "applied", apply_result: { persistence: "host_callback" } })), "applied-host");
  assert.equal(classifyAuthoringWorkflow(state({ ...idle, phase: "applied", apply_result: { persistence: "runtime_not_committed" } })), "applied-not-committed");
});

test("diagnostics are grouped and navigable with immutable operation/revision, impact, and grant context", async () => {
  const diagnostic = (severity, code) => ({ severity, code, message: `${severity} message`, related: [] });
  const impact = { entries: [{ action: "update", subject_kind: "view", capability: "view:write", subject_address: "project:local/x/view:y" }] };
  const preview = { diagnostics: [diagnostic("error", "engine.error"), diagnostic("warning", "engine.warning")], conflicts: [], authoring_impact: impact };
  const decision = { outcome: "approval_required" };
  const grant = { granted_capabilities: ["view:write"], constrained_capabilities: ["source:maintain"] };
  const editor = makeEditor({ ...idle, phase: "ready", intent, presentation: { preview, authoring_decision: decision, grant_summary: grant } });
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(EditorProvider, { editor, session: durableSession }, React.createElement(AuthoringRecoveryWorkflow, {
    context: { operation_id: "operation-42", revision: "revision-7" }, approvalAvailable: false,
  }))); });
  assert.equal(renderer.root.findByProps({ "data-workflow-status": "approval-required" }).type, "section");
  const text = JSON.stringify(renderer.toJSON());
  assert.match(text, /operation-42/); assert.match(text, /revision-7/); assert.match(text, /update: view \(view:write\)/);
  assert.match(text, /Granted: view:write/); assert.match(text, /Constrained: source:maintain/);
  assert.equal(renderer.root.findByProps({ "aria-label": "Diagnostic groups" }).findAllByType("a").length, 2);
  assert.equal(button(renderer, "Request approval and apply").props.disabled, true, "missing trusted approval path cannot be bypassed");
  assert.equal(editor.calls.length, 0);
  await act(async () => { renderer.update(React.createElement(EditorProvider, { editor, session: durableSession }, React.createElement(AuthoringRecoveryWorkflow, {
    context: { operation_id: "new-operation", revision: "revision-8" }, approvalAvailable: false,
  }))); });
  const retained = JSON.stringify(renderer.toJSON());
  assert.match(retained, /operation-42/); assert.match(retained, /revision-7/);
  assert.doesNotMatch(retained, /new-operation|revision-8/, "diagnostics retain the context that produced them");
  await act(async () => renderer.unmount());
});

test("denied grants expose no apply or retry escape hatch", async () => {
  const editor = makeEditor({ ...idle, phase: "ready", intent, presentation: { preview: { diagnostics: [], conflicts: [] }, authoring_decision: { outcome: "deny" } } });
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(EditorProvider, { editor, session: durableSession }, React.createElement(AuthoringRecoveryWorkflow, { context: { operation_id: "denied", revision: "r1" } }))); });
  assert.equal(renderer.root.findByProps({ "data-workflow-status": "denied" }).type, "section");
  assert.equal(renderer.root.findAllByType("button").length, 0);
  assert.equal(editor.calls.length, 0);
  await act(async () => renderer.unmount());
});

test("approval cancellation remains not-applied and keeps the exact intent available for re-preview", async () => {
  const editor = makeEditor({ ...idle, phase: "ready", intent, presentation: approvalPresentation });
  editor.apply = async () => { editor.calls.push(["apply"]); throw Object.assign(new Error("cancelled"), { code: "editor.approval_cancelled" }); };
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(EditorProvider, { editor, session: durableSession }, React.createElement(AuthoringRecoveryWorkflow, { context: { operation_id: "approval", revision: "r2" } }))); });
  await act(async () => { await button(renderer, "Request approval and apply").props.onClick(); await Promise.resolve(); });
  assert.equal(renderer.root.findByProps({ "data-workflow-status": "approval-cancelled" }).type, "section");
  await act(async () => { await button(renderer, "Re-preview intent").props.onClick(); await Promise.resolve(); });
  assert.deepEqual(editor.calls.at(-1), ["preview", edit, { intent_id: "intent-one" }]);
  await act(async () => renderer.unmount());
});

test("stale and repeated conflict recovery preserve intent and offer refresh, re-preview, retry, and discard explicitly", async () => {
  const stale = { ...idle, phase: "failed", intent, failure: { code: "composer.stale_revision", message: "stale", recoverable: true, diagnostics: [], conflicts: [{ kind: "stale_revision" }] } };
  const editor = makeEditor(stale);
  const hostCalls = [];
  const handlers = {
    async refresh(value) { hostCalls.push(["refresh", value]); },
    async discard(value) { hostCalls.push(["discard", value]); },
  };
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(EditorProvider, { editor, session: durableSession }, React.createElement(AuthoringRecoveryWorkflow, { context: { operation_id: "stale", revision: "old" }, handlers }))); });
  await act(async () => { button(renderer, "Refresh").props.onClick(); await Promise.resolve(); });
  await act(async () => { button(renderer, "Re-preview intent").props.onClick(); await Promise.resolve(); });
  await act(async () => { button(renderer, "Discard intent").props.onClick(); await Promise.resolve(); });
  assert.deepEqual(hostCalls, [["refresh", intent], ["discard", intent]]);
  assert.deepEqual(editor.calls.at(-1), ["preview", edit, { intent_id: "intent-one" }]);

  await act(async () => editor.emit({ ...stale, failure: { ...stale.failure, code: "composer.conflict", conflicts: [{ kind: "same_field_changed" }] } }));
  assert.equal(renderer.root.findByProps({ "data-workflow-status": "conflict" }).type, "section");
  assert.ok(button(renderer, "Refresh"));
  assert.ok(button(renderer, "Retry"));
  await act(async () => editor.emit({ ...stale, failure: { code: "composer.validation_failed", message: "retry", recoverable: true, diagnostics: [], conflicts: [] } }));
  await act(async () => { button(renderer, "Retry").props.onClick(); await Promise.resolve(); });
  await act(async () => editor.emit({ ...stale, failure: { code: "composer.validation_failed", message: "again", recoverable: true, diagnostics: [], conflicts: [] } }));
  assert.ok(button(renderer, "Retry"), "repeated failure remains explicitly recoverable");
  assert.deepEqual(editor.calls.at(-1), ["retry"]);
  await act(async () => renderer.unmount());
});

test("missing host recovery handlers are visibly disabled instead of becoming no-op controls", async () => {
  const stale = { ...idle, phase: "failed", intent, failure: { code: "composer.stale_revision", message: "stale", recoverable: true, diagnostics: [], conflicts: [] } };
  const editor = makeEditor(stale);
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(EditorProvider, { editor, session: durableSession }, React.createElement(AuthoringRecoveryWorkflow, { context: { operation_id: "missing", revision: "r" } }))); });
  assert.equal(button(renderer, "Refresh").props.disabled, true);
  assert.equal(button(renderer, "Discard intent").props.disabled, true);
  assert.equal(button(renderer, "Re-preview intent").props.disabled, false);
  assert.equal(button(renderer, "Retry").props.disabled, false);
  await act(async () => renderer.unmount());
});

test("synchronous host recovery throws are contained, actionable, and release the flight lock", async () => {
  const stale = { ...idle, phase: "failed", intent, failure: { code: "composer.stale_revision", message: "stale", recoverable: true, diagnostics: [], conflicts: [] } };
  const editor = makeEditor(stale);
  let attempts = 0;
  const refresh = () => { attempts += 1; throw new Error("refresh adapter failed"); };
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(EditorProvider, { editor, session: durableSession }, React.createElement(AuthoringRecoveryWorkflow, { context: { operation_id: "throw", revision: "r" }, handlers: { refresh } }))); });
  await act(async () => { button(renderer, "Refresh").props.onClick(); await Promise.resolve(); await Promise.resolve(); });
  assert.equal(renderer.root.findByProps({ role: "alert" }).children.join(""), "refresh adapter failed");
  assert.equal(button(renderer, "Refresh").props.disabled, false);
  await act(async () => { button(renderer, "Refresh").props.onClick(); await Promise.resolve(); await Promise.resolve(); });
  assert.equal(attempts, 2, "the synchronous throw does not leave recovery permanently locked");
  await act(async () => renderer.unmount());
});

test("operation context replacement aborts host recovery and ignores every obsolete completion", async () => {
  const stale = { ...idle, phase: "failed", intent, failure: { code: "composer.stale_revision", message: "stale", recoverable: true, diagnostics: [], conflicts: [] } };
  const editor = makeEditor(stale);
  const gate = deferred();
  let signal;
  const handlers = { refresh(_intent, value) { signal = value; return gate.promise; } };
  const element = (operation_id, revision) => React.createElement(EditorProvider, { editor, session: durableSession }, React.createElement(AuthoringRecoveryWorkflow, { context: { operation_id, revision }, handlers }));
  let renderer;
  await act(async () => { renderer = TestRenderer.create(element("old-operation", "old-revision")); });
  await act(async () => { button(renderer, "Refresh").props.onClick(); await Promise.resolve(); });
  assert.equal(signal.aborted, false);
  await act(async () => { renderer.update(element("new-operation", "new-revision")); });
  assert.equal(signal.aborted, true);
  assert.equal(renderer.root.findAllByProps({ role: "alert" }).length, 0);
  await act(async () => { gate.reject(new Error("obsolete failure")); await gate.promise.catch(() => undefined); await Promise.resolve(); });
  assert.equal(renderer.root.findAllByProps({ role: "alert" }).length, 0);
  assert.equal(button(renderer, "Refresh").props.disabled, false);
  const text = JSON.stringify(renderer.toJSON());
  assert.match(text, /new-operation/); assert.match(text, /new-revision/); assert.doesNotMatch(text, /old-operation|old-revision|obsolete failure/);
  await act(async () => renderer.unmount());
});

test("disconnected Runtime offers safe reopen and aborts abandoned-session recovery on cleanup", async () => {
  const editor = makeEditor();
  let reopenSignal;
  const reopen = (signal) => { reopenSignal = signal; return new Promise(() => {}); };
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(EditorProvider, { editor, session: durableSession }, React.createElement(AuthoringRecoveryWorkflow, {
    context: { operation_id: "reconnect", revision: "runtime-9" }, connection: { status: "disconnected", reason: "Runtime restarted" }, handlers: { reopen },
  }))); });
  assert.equal(renderer.root.findByProps({ role: "alert" }).children.join(""), "Runtime restarted");
  await act(async () => { button(renderer, "Reopen session").props.onClick(); await Promise.resolve(); });
  assert.equal(reopenSignal.aborted, false);
  await act(async () => renderer.unmount());
  assert.equal(reopenSignal.aborted, true);
});

test("ephemeral and durable results remain visibly distinct", async () => {
  for (const [session, persistence, label] of [[ephemeralSession, "ephemeral", "Ephemeral local change"], [durableSession, "durable", "Durable committed revision"]]) {
    const editor = makeEditor({ ...idle, phase: "applied", apply_result: { persistence } });
    let renderer;
    await act(async () => { renderer = TestRenderer.create(React.createElement(EditorProvider, { editor, session }, React.createElement(AuthoringRecoveryWorkflow, { context: { operation_id: persistence, revision: "r" } }))); });
    assert.equal(renderer.root.findByProps({ "data-persistence": persistence }).children.join(""), label);
    await act(async () => renderer.unmount());
  }
});
