// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import test from "node:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";
import {
  CapabilityControl,
  EditorCommandButton,
  EditorLiveRegion,
  EditorPanel,
  EditorProvider,
  EditorShell,
  EditorToolbar,
  EditorWorkspace,
  RestoreFocus,
  classifyEditorAction,
  useEditor,
  useEditorCapabilities,
  useEditorConflicts,
  useEditorDecision,
  useEditorDiagnostics,
  useEditorGrant,
  useEditorImpact,
  useEditorPreview,
  useEditorSession,
  useEditorState,
} from "../dist/index.js";

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const idle = Object.freeze({ phase: "idle", sequence: 0, can_undo: false, can_redo: false });
const manifest = Object.freeze({ operations: {
  "runtime.commit_operations": { enabled: true },
  "engine.apply_to_handle": { enabled: false, unavailable_reason: "host_disabled" },
} });
const durableSession = Object.freeze({ authority: "runtime", persistence: "durable", session: {}, capabilities: { authority: "runtime", manifest, selection: { available: [], optional_unavailable: [] } } });
const ephemeralSession = Object.freeze({ authority: "engine", persistence: "ephemeral", session: {}, capabilities: { authority: "engine", manifest, selection: { available: [], optional_unavailable: [] } } });

function makeEditor(initial = idle) {
  let snapshot = initial;
  const listeners = new Set();
  const calls = [];
  return {
    calls,
    get listenerCount() { return listeners.size; },
    snapshot: () => snapshot,
    subscribe(listener) { listeners.add(listener); listener(snapshot); return () => listeners.delete(listener); },
    getCapabilities: () => ({ authority: "runtime", manifest, selection: { available: [], optional_unavailable: [] } }),
    emit(next) { snapshot = Object.freeze(next); for (const listener of listeners) listener(snapshot); },
    async preview(edit) { calls.push(["preview", edit]); return { preview: snapshot.presentation?.preview }; },
    async apply(edit) { calls.push(["apply", edit]); return { persistence: "ephemeral", applied: {} }; },
    async undo() { calls.push(["undo"]); return snapshot; },
    async redo() { calls.push(["redo"]); return snapshot; },
    async retry() { calls.push(["retry"]); return snapshot; },
    cancelPreview() { calls.push(["cancel"]); snapshot = idle; for (const listener of listeners) listener(snapshot); return snapshot; },
    async materializeView() { return {}; }, async open() { return durableSession; }, async close() { calls.push(["close"]); },
  };
}

test("provider uses only the injected editor, exposes authoritative state, and replaces subscriptions", async () => {
  const first = makeEditor();
  const second = makeEditor();
  const observed = [];
  function Probe() {
    const full = useEditor();
    const state = useEditorState();
    observed.push({ same: full.state === state, session: useEditorSession(), preview: useEditorPreview(), diagnostics: useEditorDiagnostics(), impact: useEditorImpact(), grant: useEditorGrant(), decision: useEditorDecision(), conflicts: useEditorConflicts(), capabilities: useEditorCapabilities() });
    return React.createElement("output", null, state.snapshot.phase);
  }
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(EditorProvider, { editor: first, session: ephemeralSession }, React.createElement(Probe))); });
  assert.equal(first.listenerCount, 1);
  const preview = { status: "valid", diagnostics: [{ code: "notice" }], conflicts: [{ kind: "semantic" }], authoring_impact: { impact_digest: "impact" } };
  const decision = { outcome: "allow" };
  const grant = { granted_capabilities: ["graph:write"] };
  await act(async () => first.emit({ phase: "ready", sequence: 1, can_undo: false, can_redo: false, presentation: { preview, authoring_decision: decision, grant_summary: grant } }));
  const latest = observed.at(-1);
  assert.equal(latest.same, true); assert.equal(latest.preview, preview); assert.deepEqual(latest.diagnostics, preview.diagnostics);
  assert.equal(latest.impact, preview.authoring_impact); assert.equal(latest.decision, decision); assert.equal(latest.grant, grant);
  assert.deepEqual(latest.conflicts, preview.conflicts); assert.equal(latest.session, ephemeralSession);
  await act(async () => renderer.update(React.createElement(EditorProvider, { editor: second, session: durableSession }, React.createElement(Probe))));
  assert.equal(first.listenerCount, 0); assert.equal(second.listenerCount, 1);
  await act(async () => renderer.unmount());
  assert.equal(second.listenerCount, 0); assert.deepEqual(first.calls, []); assert.deepEqual(second.calls, []);
});

test("session replacement cancels pending preview and ignores late completion", async () => {
  const editor = makeEditor();
  let resolve;
  editor.preview = (edit) => { editor.calls.push(["preview", edit]); return new Promise((done) => { resolve = done; }); };
  let commands;
  function Probe() { const value = useEditor(); commands = value.commands; return React.createElement("output", null, value.state.pendingAction ?? "idle"); }
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(EditorProvider, { editor, session: ephemeralSession }, React.createElement(Probe))); });
  let flight;
  await act(async () => { flight = commands.preview({ kind: "semantic_operations", request: {} }); await Promise.resolve(); });
  await act(async () => editor.emit({ phase: "previewing", sequence: 1, can_undo: false, can_redo: false }));
  await act(async () => renderer.update(React.createElement(EditorProvider, { editor, session: durableSession }, React.createElement(Probe))));
  assert.deepEqual(editor.calls.at(-1), ["cancel"]);
  await act(async () => { resolve({ preview: {} }); await flight; });
  assert.equal(renderer.root.findByType("output").children.join(""), "idle");
  await act(async () => renderer.unmount());
});

test("controls distinguish unavailable, denied, pending, ephemeral, and durable states", () => {
  assert.equal(classifyEditorAction({ session: durableSession, snapshot: idle, pendingAction: undefined, decision: undefined }, false), "unavailable");
  assert.equal(classifyEditorAction({ session: durableSession, snapshot: idle, pendingAction: undefined, decision: { outcome: "deny" } }, true), "denied");
  assert.equal(classifyEditorAction({ session: durableSession, snapshot: idle, pendingAction: "apply", decision: undefined }, true), "pending");
  assert.equal(classifyEditorAction({ session: ephemeralSession, snapshot: idle, pendingAction: undefined, decision: undefined }, true), "ephemeral");
  assert.equal(classifyEditorAction({ session: durableSession, snapshot: idle, pendingAction: undefined, decision: undefined }, true), "durable");
});

test("capability controls route commands and restore focus", async () => {
  const editor = makeEditor({ ...idle, intent: { id: "i", edit: { kind: "semantic_operations", request: {} } } });
  let focused = false;
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(EditorProvider, { editor, session: durableSession },
    React.createElement(CapabilityControl, { capabilityId: "runtime.commit_operations", fallback: "fallback" }, React.createElement(EditorCommandButton, { action: "apply", capabilityId: "runtime.commit_operations", "aria-label": "Apply" }, "Apply")),
    React.createElement(CapabilityControl, { capabilityId: "engine.apply_to_handle", fallback: React.createElement("i", null, "Unavailable") }, "hidden")),
  { createNodeMock: (element) => element.type === "button" ? { isConnected: true, focus: () => { focused = true; } } : null }); });
  const button = renderer.root.findByType("button");
  assert.equal(button.props["data-action-state"], "durable"); assert.equal(button.props.disabled, false);
  assert.equal(renderer.root.findByType("i").children.join(""), "Unavailable");
  await act(async () => { await button.props.onClick(); await Promise.resolve(); });
  assert.equal(editor.calls[0][0], "apply"); assert.equal(focused, true);
  await act(async () => renderer.unmount());
});

test("toolbar arrow, home, and end keys move focus among enabled controls", async () => {
  const editor = makeEditor();
  const buttons = [];
  let active;
  const createNodeMock = (element) => {
    if (element.type === "button") { const node = { focus: () => { active = node; }, isConnected: true }; buttons.push(node); return node; }
    if (element.props.role === "toolbar") return { querySelectorAll: () => buttons };
    return null;
  };
  const action = (name) => React.createElement(EditorCommandButton, { action: name, capabilityId: "runtime.commit_operations", key: name }, name);
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(EditorProvider, { editor, session: durableSession }, React.createElement(EditorToolbar, { label: "Editing commands" }, action("undo"), action("redo"), action("retry"))), { createNodeMock }); });
  const toolbar = renderer.root.findByProps({ role: "toolbar" });
  const key = (value, target) => toolbar.props.onKeyDown({ key: value, target, defaultPrevented: false, preventDefault() {} });
  buttons[0].focus(); key("ArrowRight", buttons[0]); assert.equal(active, buttons[1]);
  key("End", buttons[1]); assert.equal(active, buttons[2]); key("Home", buttons[2]); assert.equal(active, buttons[0]);
  key("ArrowLeft", buttons[0]); assert.equal(active, buttons[2]);
  await act(async () => renderer.unmount());
});

test("layout landmarks and live failures remain accessible", async () => {
  const editor = makeEditor({ ...idle, phase: "failed", failure: { message: "Preview failed", diagnostics: [], conflicts: [] } });
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(EditorProvider, { editor, session: ephemeralSession }, React.createElement(RestoreFocus, null,
    React.createElement(EditorShell, null, React.createElement(EditorToolbar, { label: "Tools" }), React.createElement(EditorWorkspace, null,
      React.createElement(EditorPanel, { label: "Canvas" }), React.createElement(EditorPanel, { label: "Inspector", placement: "inspector" })), React.createElement(EditorLiveRegion))))); });
  assert.equal(renderer.root.findByProps({ role: "toolbar" }).props["aria-label"], "Tools");
  assert.equal(renderer.root.findByProps({ "aria-label": "Inspector" }).props["data-placement"], "inspector");
  assert.equal(renderer.root.findByProps({ role: "status" }).children.join(""), "Preview failed");
  await act(async () => renderer.unmount());
});

test("hooks fail clearly outside the dependency-injection provider", async () => {
  function Invalid() { useEditorState(); return null; }
  await assert.rejects(async () => { await act(async () => TestRenderer.create(React.createElement(Invalid))); }, /require an EditorProvider/);
});
