// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import React from "react";
import { createRoot } from "react-dom/client";
import {
  AuthoringRecoveryWorkflow,
  DocumentOutline,
  EditorCommandButton,
  EditorLiveRegion,
  EditorPanel,
  EditorProvider,
  EditorShell,
  EditorToolbar,
  EditorWorkspace,
  SemanticInspector,
  LiveViewer,
  QueryViewComposer,
} from "../dist/index.js";

const capabilityId = "runtime.commit_operations";
const edit = { kind: "semantic_operations", request: {} };
const preview = { status: "valid", diagnostics: [], conflicts: [] };
const allow = { outcome: "allow" };
const deny = { outcome: "deny" };
const ready = (decision = allow) => ({
  phase: "ready", sequence: 1, can_undo: true, can_redo: false,
  intent: { id: "browser-e2e", edit }, presentation: { preview, authoring_decision: decision },
});

function session(enabled) {
  return {
    authority: "runtime", persistence: "durable", session: {},
    capabilities: {
      authority: "runtime",
      manifest: { operations: { [capabilityId]: enabled ? { enabled: true } : { enabled: false, unavailable_reason: "host_disabled" } } },
      selection: { available: enabled ? [capabilityId] : [], optional_unavailable: [] },
    },
  };
}

function makeEditor(initial = ready()) {
  let snapshot = initial;
  const listeners = new Set();
  const api = {
    calls: [],
    applyRejects: false,
    get listenerCount() { return listeners.size; },
    snapshot: () => snapshot,
    subscribe(listener) { listeners.add(listener); listener(snapshot); return () => listeners.delete(listener); },
    getCapabilities: () => currentSession.capabilities,
    emit(next) { snapshot = next; for (const listener of listeners) listener(snapshot); },
    async preview(value) { api.calls.push(["preview", value]); return { preview }; },
    async apply() { if (api.applyRejects) throw new Error("host apply failed"); return { persistence: "durable", result: {}, committed_revision: {} }; },
    async undo() { return snapshot; }, async redo() { return snapshot; }, async retry() { return snapshot; },
    cancelPreview() { api.emit({ phase: "idle", sequence: snapshot.sequence + 1, can_undo: false, can_redo: false }); return snapshot; },
    async materializeView(input) {
      if (input.mode === "loading") return new Promise(() => {});
      if (input.mode === "error") throw Object.assign(new Error("query materialization failed"), { diagnostics: [{ code: "query.failed" }] });
      return { mode: input.mode, shape: input.mode === "3d" ? "3d" : "2d", items: input.mode === "dense" ? Array.from({ length: 200 }, (_, index) => index) : [] };
    }, async open() { return currentSession; }, async close() {},
  };
  return api;
}

const root = createRoot(document.querySelector("#root"));
let currentSession = session(true);
let activeEditor = makeEditor();
let previousEditor;
let recoveryConnection = { status: "connected" };
let approvalAvailable = true;
const recoveryCalls = [];
let navigationSelection = { stale: false };
let lastSourceNavigation;
const navigationItems = Array.from({ length: 10_000 }, (_, index) => ({
  address: `project:p:entity:item_${String(index).padStart(4, "0")}`,
  display_name: `Engine item ${index}`,
  kind: index % 3 === 0 ? "relation" : "entity",
  source_range: { start_byte: String(index * 10), end_byte: String(index * 10 + 5), module_path: "main.ldl", origin: { kind: "project" } },
  availability: index === 2 ? "denied" : index === 3 ? "partial" : "available",
}));
let inspectorDraft = "Engine value";
let viewerMode = "empty";
let viewerPublication;
const viewer = {
  getPublication: () => viewerPublication,
  async cancel() {},
  async setViewData(snapshot) {
    viewerPublication = {
      ...snapshot,
      render_data: { shape: snapshot.view_data.shape },
      presentation: viewerPublication?.view_address === snapshot.view_address
        ? viewerPublication.presentation
        : { selection_keys: ["selected"], zoom: 2, pan: { x: 4, y: 8 }, expanded_keys: [], sorting: [], display_preferences: {} },
    };
    const state = snapshot.view_data.mode === "empty"
      ? { status: "empty", reason: "view_empty", publication: viewerPublication }
      : snapshot.view_data.mode === "partial"
        ? { status: "partial_stream", publication: viewerPublication }
        : { status: "ready", publication: viewerPublication };
    return { ok: true, outcome: "published", state };
  },
};
const requests = new Map();
function requestFor(mode) {
  if (!requests.has(mode)) requests.set(mode, {
    key: mode,
    input: { mode },
    toViewerSnapshot: () => ({ viewer_snapshot_schema_version: 1, sequence: requests.size, complete: mode !== "partial", view_address: "view:fixture", revision: {}, view_data_hash: `hash:${mode}`, state_input: {} }),
  });
  return requests.get(mode);
}
const intents = ["create", "edit", "duplicate", "remove"].map((kind) => ({
  id: `fixture-${kind}`, kind, label: kind, ...(kind === "create" ? {} : { target_id: "view:fixture" }), availability: { status: "available" },
  fields: kind === "create" ? [{ id: "name", label: "Name", type: "text", value: "New view" }] : [],
  buildEdit: () => edit,
}));

function App() {
  const command = (action, label) => React.createElement(EditorCommandButton, { action, capabilityId, "aria-label": label }, label);
  return React.createElement(EditorProvider, { editor: activeEditor, session: currentSession },
    React.createElement(EditorShell, { style: { height: "100vh" } },
      React.createElement(EditorToolbar, { label: "Editing commands" },
        command("apply", "Apply"), command("undo", "Undo"), command("redo", "Redo"), command("retry", "Retry"), command("cancel-preview", "Cancel preview")),
      React.createElement(EditorWorkspace, null,
        React.createElement(EditorPanel, { label: "Canvas" },
          React.createElement(DocumentOutline, {
            items: navigationItems,
            selection: navigationSelection,
            maxVisibleItems: 40,
            onSelectionChange(next) { navigationSelection = next; render(); },
            onNavigateSource(range, address) { lastSourceNavigation = { range, address }; },
          }),
          React.createElement(LiveViewer, { editor: activeEditor, viewer, request: requestFor(viewerMode), debounceMs: 0 }, (state) => {
            if (state.status === "ready" || state.status === "partial" || state.status === "empty") {
              const data = state.publication?.view_data;
              if (state.status === "empty") return React.createElement("p", null, "Empty view");
              return React.createElement("div", { className: data?.items?.length > 0 ? "ld-viewer-dense" : undefined, "data-render-shape": data?.shape, "data-item-count": data?.items?.length ?? 0 }, state.status === "partial" ? "Partial view" : `${data?.shape} view`);
            }
            return React.createElement("p", null, state.status);
          })),
        React.createElement(EditorPanel, { label: "Inspector", placement: "inspector" },
          React.createElement(SemanticInspector, {
            address: navigationSelection.address,
            fields: [{
              id: "name", label: "Name", draft: inspectorDraft,
              onDraftChange(value) { inspectorDraft = value; render(); },
              buildEdit(draft) { return { kind: "fragment", request: { fragment: draft } }; },
            }, { id: "identity", label: "Identity", draft: navigationSelection.address ?? "none", availability: "read-only" }],
          }),
          React.createElement(QueryViewComposer, { definitions: [{ id: "view:fixture", kind: "view", label: "Fixture view" }], selectedId: "view:fixture", onSelect() {}, intents }),
          React.createElement(AuthoringRecoveryWorkflow, {
            context: { operation_id: "browser-e2e", revision: "revision-1" },
            connection: recoveryConnection,
            approvalAvailable,
            handlers: {
              async refresh(intent) { recoveryCalls.push(["refresh", intent?.id]); },
              async discard(intent) { recoveryCalls.push(["discard", intent?.id]); },
              async reopen() { recoveryCalls.push(["reopen"]); recoveryConnection = { status: "connected" }; activeEditor.emit(ready()); render(); },
            },
          }))),
      React.createElement(EditorLiveRegion)),
  );
}

function render() { root.render(React.createElement(App)); }
render();

window.editorWorkflow = {
  capability(enabled) { currentSession = session(enabled); render(); },
  deny() { activeEditor.emit(ready(deny)); },
  allow() { activeEditor.applyRejects = false; activeEditor.emit(ready()); },
  previewing() { activeEditor.emit({ phase: "previewing", sequence: 2, can_undo: false, can_redo: false }); },
  failApply() { activeEditor.applyRejects = true; activeEditor.emit(ready()); },
  replaceEditor() { previousEditor = activeEditor; activeEditor = makeEditor(); render(); },
  viewer(mode) { viewerMode = mode; render(); },
  recovery(mode) {
    approvalAvailable = true;
    recoveryConnection = { status: "connected" };
    if (mode === "approval-unavailable") { approvalAvailable = false; activeEditor.emit(ready({ outcome: "approval_required" })); render(); }
    else if (mode === "conflict") activeEditor.emit({ phase: "failed", sequence: 4, can_undo: false, can_redo: false, intent: { id: "browser-e2e", edit }, failure: { code: "composer.conflict", message: "conflict", recoverable: true, diagnostics: [], conflicts: [{ kind: "same_field_changed" }] } });
    else if (mode === "disconnected") { recoveryConnection = { status: "disconnected", reason: "Runtime restarted" }; render(); }
    else activeEditor.emit(ready());
  },
  recoveryCalls() { return recoveryCalls; },
  listenerCounts() { return { previous: previousEditor?.listenerCount ?? -1, current: activeEditor.listenerCount }; },
  navigation() { return { selection: navigationSelection, source: lastSourceNavigation, calls: activeEditor.calls }; },
};
