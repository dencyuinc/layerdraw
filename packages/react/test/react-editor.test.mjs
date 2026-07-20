// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import test from "node:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";
import { Composer } from "../../composer/dist/state-machine.js";
import {
  CapabilityControl,
  DocumentOutline,
  EditorCommandButton,
  EditorLiveRegion,
  EditorPanel,
  EditorProvider,
  EditorShell,
  EditorToolbar,
  EditorWorkspace,
  RestoreFocus,
  SemanticInspector,
  SourceNavigationList,
  classifyEditorAction,
  filterNavigationItems,
  reconcileNavigationSelection,
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

test("cleanup preserves host-owned previews and obsolete flights cannot overwrite current state", async () => {
  const editor = makeEditor({ phase: "previewing", sequence: 1, can_undo: false, can_redo: false });
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(EditorProvider, { editor, session: ephemeralSession }, React.createElement("output"))); });
  await act(async () => renderer.unmount());
  assert.deepEqual(editor.calls, [], "an injected editor's pre-existing preview is not owned by the provider");

  const concurrent = makeEditor();
  const flights = [];
  concurrent.preview = () => new Promise((resolve, reject) => flights.push({ resolve, reject }));
  let commands;
  function Probe() { const value = useEditor(); commands = value.commands; return React.createElement("output", null, `${value.state.pendingAction ?? "idle"}:${value.state.error instanceof Error ? value.state.error.message : "ok"}`); }
  await act(async () => { renderer = TestRenderer.create(React.createElement(EditorProvider, { editor: concurrent, session: ephemeralSession }, React.createElement(Probe))); });
  let first; let second;
  await act(async () => { first = commands.preview({ kind: "semantic_operations", request: {} }); second = commands.preview({ kind: "semantic_operations", request: {} }); await Promise.resolve(); });
  assert.equal(renderer.root.findByType("output").children.join(""), "preview:ok");
  await act(async () => { flights[0].reject(new Error("obsolete")); await first; });
  assert.equal(renderer.root.findByType("output").children.join(""), "preview:ok");
  await act(async () => { flights[1].resolve({ preview: {} }); await second; });
  assert.equal(renderer.root.findByType("output").children.join(""), "idle:ok");
  await act(async () => renderer.unmount());
});

test("controls distinguish unavailable, denied, pending, ephemeral, and durable states", () => {
  const ready = { ...idle, phase: "ready", intent: { id: "i", edit: {} } };
  assert.equal(classifyEditorAction("apply", { session: durableSession, snapshot: ready, pendingAction: undefined, decision: undefined }, false), "unavailable");
  assert.equal(classifyEditorAction("apply", { session: durableSession, snapshot: ready, pendingAction: undefined, decision: { outcome: "deny" } }, true), "denied");
  assert.equal(classifyEditorAction("apply", { session: durableSession, snapshot: ready, pendingAction: "apply", decision: undefined }, true), "pending");
  assert.equal(classifyEditorAction("apply", { session: ephemeralSession, snapshot: ready, pendingAction: undefined, decision: undefined }, true), "ephemeral");
  assert.equal(classifyEditorAction("apply", { session: durableSession, snapshot: ready, pendingAction: undefined, decision: undefined }, true), "durable");
  assert.equal(classifyEditorAction("apply", { session: durableSession, snapshot: idle, pendingAction: undefined, decision: undefined }, true), "unavailable");
  assert.equal(classifyEditorAction("undo", { session: durableSession, snapshot: { ...idle, can_undo: true }, pendingAction: undefined, decision: undefined }, true), "durable");
  assert.equal(classifyEditorAction("redo", { session: durableSession, snapshot: { ...idle, can_redo: true }, pendingAction: undefined, decision: undefined }, true), "durable");
  assert.equal(classifyEditorAction("retry", { session: durableSession, snapshot: { ...idle, phase: "failed", failure: { recoverable: true } }, pendingAction: undefined, decision: undefined }, true), "durable");
  assert.equal(classifyEditorAction("cancel-preview", { session: durableSession, snapshot: { ...idle, phase: "previewing" }, pendingAction: "preview", decision: undefined }, true), "durable");
});

test("closed Composer history leaves every editor action unavailable", async () => {
  const composer = new Composer({
    preview: async () => ({ preview: { status: "valid", diagnostics: [], conflicts: [] } }),
    apply: async () => ({ persistence: "ephemeral", applied: {} }),
  });
  const edit = { kind: "semantic_operations", request: {} };
  await composer.preview({ id: "history", edit, inverse: edit });
  assert.equal((await composer.apply()).can_undo, true);
  await composer.close();
  const snapshot = composer.snapshot();
  assert.equal(snapshot.phase, "closed");
  assert.equal(snapshot.can_undo, true, "the Composer intentionally retains its semantic history after close");
  for (const action of ["apply", "undo", "redo", "retry", "cancel-preview"]) {
    assert.equal(classifyEditorAction(action, { session: durableSession, snapshot, pendingAction: undefined, decision: undefined }, true), "unavailable", action);
  }
});

test("capability controls route commands and restore focus", async () => {
  const editor = makeEditor({ ...idle, phase: "ready", intent: { id: "i", edit: { kind: "semantic_operations", request: {} } } });
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

const sourceRange = (start = "0") => ({ start_byte: start, end_byte: String(Number(start) + 4), module_path: "main.ldl", origin: { kind: "project" } });
const navigationItem = (address, display_name, kind = "entity", availability = "available") => ({
  address, display_name, kind, availability, source_range: sourceRange(), matched_field: "display_name", matched_value: display_name,
});

test("navigation consumes Engine identities, filters structured fields, and reconciles rename/delete", () => {
  const before = navigationItem("project:p:entity:old", "Old name");
  const after = navigationItem("project:p:entity:new", "New name");
  assert.deepEqual(filterNavigationItems([before, after], "NEW", new Set(["entity"])), [after]);
  assert.deepEqual(filterNavigationItems([before], "entity:old"), [before]);
  assert.deepEqual(reconcileNavigationSelection({ address: before.address, lastSourceRange: before.source_range, stale: false }, [after]), {
    address: before.address, lastSourceRange: before.source_range, stale: true,
  });
  assert.deepEqual(reconcileNavigationSelection(
    { address: before.address, lastSourceRange: before.source_range, stale: false },
    [after],
    new Map([[before.address, after.address]]),
  ), { address: after.address, lastSourceRange: after.source_range, stale: false });
});

test("outline bounds large results, distinguishes availability, and supports keyboard/source navigation", async () => {
  const items = Array.from({ length: 500 }, (_, index) => navigationItem(
    `project:p:entity:item_${String(index).padStart(4, "0")}`,
    `Item ${index}`,
    index === 1 ? "relation" : "entity",
    index === 2 ? "denied" : index === 3 ? "partial" : "available",
  ));
  let selection = { stale: false };
  const navigated = [];
  let renderer;
  const render = () => React.createElement(DocumentOutline, {
    items, selection, maxVisibleItems: 25,
    onSelectionChange(next) { selection = next; },
    onNavigateSource(range, address) { navigated.push([range, address]); },
  });
  await act(async () => { renderer = TestRenderer.create(render()); });
  assert.equal(renderer.root.findAllByProps({ role: "option" }).length, 25);
  assert.equal(renderer.root.findByProps({ role: "status" }).children.join(""), "25 of 500 results shown.");
  assert.equal(renderer.root.findAllByProps({ "data-availability": "denied" })[0].props["aria-disabled"], true);
  const listbox = renderer.root.findByProps({ role: "listbox" });
  await act(async () => listbox.props.onKeyDown({ key: "ArrowDown", preventDefault() {} }));
  assert.equal(selection.address, items[0].address);
  await act(async () => { renderer.update(render()); });
  await act(async () => listbox.props.onKeyDown({ key: "End", preventDefault() {} }));
  assert.equal(selection.address, items[24].address);
  await act(async () => { renderer.update(render()); });
  await act(async () => listbox.props.onKeyDown({ key: "Enter", preventDefault() {} }));
  assert.equal(navigated.at(-1)[1], items[24].address);
  selection = { address: items[499].address, lastSourceRange: items[499].source_range, stale: false };
  await act(async () => { renderer.update(render()); });
  assert.equal(renderer.root.findByProps({ role: "listbox" }).props["aria-activedescendant"], undefined, "a bounded-out selection must not reference an unmounted option");
  const search = renderer.root.findByType("input");
  await act(async () => search.props.onChange({ currentTarget: { value: "Item 1" } }));
  assert.equal(renderer.root.findByProps({ role: "listbox" }).props["aria-activedescendant"], undefined, "a filtered-out selection must not reference an unmounted option");
  await act(async () => renderer.unmount());
});

test("deleted selections retain source handoff and inspectors emit the supplied Composer edit verbatim", async () => {
  const selected = navigationItem("project:p:view:gone", "Gone", "view");
  const edits = [
    { kind: "semantic_operations", request: { role: "field" } },
    { kind: "semantic_operations", request: { role: "relation" } },
    { kind: "semantic_operations", request: { role: "row" } },
    { kind: "fragment", request: { role: "fragment" } },
  ];
  const drafts = ["field", "relation", "row", "fragment"];
  const renderedDrafts = [...drafts];
  const editor = makeEditor();
  const navigated = [];
  let selection = { address: selected.address, lastSourceRange: selected.source_range, stale: false };
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(EditorProvider, { editor, session: durableSession },
    React.createElement(DocumentOutline, { items: [], selection, onSelectionChange(next) { selection = next; }, onNavigateSource(range) { navigated.push(range); } }),
    React.createElement(SourceNavigationList, { targets: [
      { id: "diagnostic", label: "Open diagnostic", source_range: selected.source_range, address: selected.address, availability: "partial" },
      { id: "denied", label: "Denied diagnostic", source_range: selected.source_range, availability: "denied" },
      { id: "unavailable", label: "Unavailable diagnostic", source_range: selected.source_range, availability: "unavailable" },
      { id: "readonly", label: "Read-only diagnostic", source_range: selected.source_range, availability: "read-only" },
    ], onNavigateSource(range) { navigated.push(range); } }),
    React.createElement(SemanticInspector, { address: selected.address, fields: [
      ...edits.map((edit, index) => ({
        id: `editable-${index}`, label: `Edit ${index}`, draft: drafts[index],
        onDraftChange(value) { drafts[index] = value; },
        buildEdit(draft) { return { ...edit, request: { ...edit.request, draft } }; },
      })),
      { id: "readonly", label: "Identity", draft: selected.address, availability: "read-only" },
    ] }),
  )); });
  assert.equal(selection.stale, true);
  assert.equal(renderer.root.findByProps({ role: "status" }).children.join(""), "No structured results. Diagnostics remain available.");
  const stale = renderer.root.findByProps({ className: "ld-navigation-stale" });
  await act(async () => stale.props.onClick());
  const diagnostic = renderer.root.findAllByType("button").find((button) => button.children.join("") === "Open diagnostic");
  await act(async () => diagnostic.props.onClick());
  const readonlyDiagnostic = renderer.root.findAllByType("button").find((button) => button.children.join("") === "Read-only diagnostic");
  await act(async () => readonlyDiagnostic.props.onClick());
  for (const label of ["Denied diagnostic", "Unavailable diagnostic"]) {
    const blocked = renderer.root.findAllByType("button").find((button) => button.children.join("") === label);
    assert.equal(blocked.props.disabled, true);
    assert.equal(blocked.props["aria-disabled"], true);
    assert.equal(blocked.props.onClick, undefined);
  }
  assert.deepEqual(navigated, [selected.source_range, selected.source_range, selected.source_range]);
  const buttons = renderer.root.findAllByType("button");
  const previews = buttons.filter((button) => button.children.join("") === "Preview change" && button.props.disabled === false);
  const inputs = renderer.root.findAllByType("input").filter((input) => input.props.disabled === false);
  for (let index = 0; index < inputs.length; index++) await act(async () => inputs[index].props.onChange({ currentTarget: { value: `${drafts[index]}-changed` } }));
  for (const preview of previews) await act(async () => { preview.props.onClick(); await Promise.resolve(); });
  assert.deepEqual(editor.calls, edits.map((edit, index) => ["preview", { ...edit, request: { ...edit.request, draft: renderedDrafts[index] } }]));
  assert.equal(buttons.some((button) => button.props.disabled === true), true);
  await act(async () => renderer.unmount());
});
