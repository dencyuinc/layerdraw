// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import React from "react";
import { createRoot } from "react-dom/client";
import { DesktopShell } from "../dist/index.js";

const listeners = new Set();
const renderData = {
  kind: "diagram", shape: "diagram", bounds: { x: 0, y: 0, width: 800, height: 500 },
  containers: [], ports: [], overlays: [], badges: [], support_geometry: [], diagnostics: [], source_bindings: [],
  occurrences: [
    { render_key: "node:one", bounds: { x: 80, y: 80, width: 180, height: 70 }, port_keys: [], label_key: "label:one" },
    { render_key: "node:two", bounds: { x: 480, y: 280, width: 180, height: 70 }, port_keys: [], label_key: "label:two" },
  ],
  labels: [
    { render_key: "label:one", bounds: { x: 90, y: 90, width: 160, height: 30 }, text: "Product", anchor: { kind: "occurrence", occurrence_key: "node:one" } },
    { render_key: "label:two", bounds: { x: 490, y: 290, width: 160, height: 30 }, text: "Delivery", anchor: { kind: "occurrence", occurrence_key: "node:two" } },
  ],
  edge_paths: [{ render_key: "edge:one", points: [{ x: 260, y: 115 }, { x: 480, y: 315 }], from_port_key: "from", to_port_key: "to" }],
};
const viewer = { status: "ready", publication: { render_data: renderData, presentation: { selection_keys: [] } } };
const project = {
  project_id: "project:roadmap", session_generation: 1, display_name: "Desktop roadmap", authoritative_revision_token: "revision:12", authoritative_revision_label: "Revision 12",
  editor: {}, editor_session: {}, views: [{ address: "view:diagram", label: "System map", shape: "diagram" }, { address: "view:table", label: "Inventory", shape: "table" }],
  selected_view_address: "view:diagram", access: { status: "allowed", label: "Local owner" }, storage: { kind: "local", status: "connected", label: "On this Mac" }, persistence: "clean",
};
let available = true;
let state;
function makeState(selected = project.selected_view_address) {
  return { lifecycle: { sequence: Date.now(), phase: "ready", capabilities: available ? { "engine.materialize_view": { status: "available" } } : {}, project: { ...project, selected_view_address: selected } }, viewer, pending_action: undefined, failure: undefined };
}
state = makeState();
const calls = [];
const controller = {
  getSnapshot: () => state,
  subscribe(listener) { listeners.add(listener); return () => listeners.delete(listener); },
  start() {}, async stop() {}, async reopen() {},
  async selectView(address) { calls.push(["select", address]); state = makeState(address); for (const listener of listeners) listener(); },
  setViewerSelection(keys) { calls.push(["viewer", ...keys]); },
};
createRoot(document.querySelector("#root")).render(React.createElement(DesktopShell, {
  controller, viewSelectionCapability: "engine.materialize_view", editorSurface: () => React.createElement("p", null, "Editor ready"),
}));
window.desktopWorkflow = {
  calls,
  capability(value) { available = value; state = makeState(state.lifecycle.project.selected_view_address); for (const listener of listeners) listener(); },
};
