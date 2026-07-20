// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import React from "react";
import { createRoot } from "react-dom/client";
import {
  DocumentOutline,
  EditorCommandButton,
  EditorLiveRegion,
  EditorPanel,
  EditorProvider,
  EditorShell,
  EditorToolbar,
  EditorWorkspace,
  SemanticInspector,
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
    async materializeView() { return {}; }, async open() { return currentSession; }, async close() {},
  };
  return api;
}

const root = createRoot(document.querySelector("#root"));
let currentSession = session(true);
let activeEditor = makeEditor();
let previousEditor;
let navigationSelection = { stale: false };
let lastSourceNavigation;
const navigationItems = Array.from({ length: 300 }, (_, index) => ({
  address: `project:p:entity:item_${String(index).padStart(4, "0")}`,
  display_name: `Engine item ${index}`,
  kind: index % 3 === 0 ? "relation" : "entity",
  source_range: { start_byte: String(index * 10), end_byte: String(index * 10 + 5), module_path: "main.ldl", origin: { kind: "project" } },
  availability: index === 2 ? "denied" : index === 3 ? "partial" : "available",
}));
let inspectorDraft = "Engine value";

function App() {
  const command = (action, label) => React.createElement(EditorCommandButton, { action, capabilityId, "aria-label": label }, label);
  return React.createElement(EditorProvider, { editor: activeEditor, session: currentSession },
    React.createElement(EditorShell, { style: { height: "100vh" } },
      React.createElement(EditorToolbar, { label: "Editing commands" },
        command("apply", "Apply"), command("undo", "Undo"), command("redo", "Redo"), command("retry", "Retry"), command("cancel-preview", "Cancel preview")),
      React.createElement(EditorWorkspace, null,
        React.createElement(EditorPanel, { label: "Canvas" }, React.createElement(DocumentOutline, {
          items: navigationItems,
          selection: navigationSelection,
          maxVisibleItems: 40,
          onSelectionChange(next) { navigationSelection = next; render(); },
          onNavigateSource(range, address) { lastSourceNavigation = { range, address }; },
        })),
        React.createElement(EditorPanel, { label: "Inspector", placement: "inspector" }, React.createElement(SemanticInspector, {
          address: navigationSelection.address,
          fields: [{
            id: "name", label: "Name", draft: inspectorDraft,
            onDraftChange(value) { inspectorDraft = value; render(); },
            buildEdit(draft) { return { kind: "fragment", request: { fragment: draft } }; },
          }, { id: "identity", label: "Identity", draft: navigationSelection.address ?? "none", availability: "read-only" }],
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
  listenerCounts() { return { previous: previousEditor?.listenerCount ?? -1, current: activeEditor.listenerCount }; },
  navigation() { return { selection: navigationSelection, source: lastSourceNavigation, calls: activeEditor.calls }; },
};
