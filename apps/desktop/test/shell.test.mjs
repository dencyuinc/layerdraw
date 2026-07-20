// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import test from "node:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";
import { DesktopShell } from "../dist/shell.js";
import { DesktopViewerSurface } from "../dist/viewer-surface.js";

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

function fakeController(initial) {
  let state = initial;
  const listeners = new Set();
  const calls = [];
  return {
    calls,
    getSnapshot: () => state,
    subscribe(listener) { listeners.add(listener); return () => listeners.delete(listener); },
    start() { calls.push(["start"]); }, async stop() { calls.push(["stop"]); },
    async reopen() { calls.push(["reopen"]); }, async selectView(address) { calls.push(["select", address]); },
    setViewerSelection(keys) { calls.push(["viewer-selection", keys]); },
    emit(next) { state = next; for (const listener of listeners) listener(); },
  };
}

const project = {
  project_id: "p1", session_generation: 1, display_name: "Roadmap", authoritative_revision_token: "rev:8", authoritative_revision_label: "Revision 8",
  editor: {}, editor_session: {}, views: [{ address: "view:main", label: "Main", shape: "diagram" }, { address: "view:table", label: "Details", shape: "table" }],
  selected_view_address: "view:main", access: { status: "allowed", label: "Local owner" },
  storage: { kind: "local", status: "connected", label: "On this Mac" }, persistence: "clean",
};
const empty = { status: "empty", reason: "no_snapshot" };
function shellState(overrides = {}) { return { lifecycle: { sequence: 1, phase: "ready", capabilities: { "engine.materialize_view": { status: "available" } }, project }, viewer: empty, pending_action: undefined, failure: undefined, ...overrides }; }

test("Desktop shell exposes landmarks, authoritative context, view navigation, and injected editor surface", async () => {
  const controller = fakeController(shellState());
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(DesktopShell, {
    controller, viewSelectionCapability: "engine.materialize_view", editorSurface: (value) => React.createElement("p", null, `Editor ${value.project_id}`),
  })); });
  assert.equal(renderer.root.findByType("h1").children.join(""), "Roadmap");
  assert.equal(renderer.root.findByProps({ "aria-label": "Views" }).type, "nav");
  assert.equal(renderer.root.findByProps({ "aria-label": "Canvas" }).type, "section");
  assert.equal(renderer.root.findAllByProps({ "aria-label": "Project status" }).some((node) => node.type === "aside"), true);
  const details = renderer.root.findAllByType("button").find((button) => button.props["aria-label"] === "Details");
  await act(async () => details.props.onClick());
  assert.deepEqual(controller.calls.at(-1), ["select", "view:table"]);
  assert.ok(renderer.root.findAllByType("p").some((node) => node.children.join("") === "Editor p1"));
  await act(async () => renderer.unmount());
  assert.deepEqual(controller.calls.at(-1), ["stop"]);
});

test("unadvertised view selection is visibly disabled and closed failures do not expose host details", async () => {
  const state = shellState({
    lifecycle: { sequence: 2, phase: "ready", capabilities: {}, project },
    failure: { code: "desktop.viewer_failed", message_key: "desktop.error.viewer_failed", recoverable: true },
  });
  const controller = fakeController(state);
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(DesktopShell, { controller, viewSelectionCapability: "engine.materialize_view" })); });
  const buttons = renderer.root.findAllByType("button");
  assert.equal(buttons.every((button) => button.props.disabled === true), true);
  assert.ok(buttons.every((button) => button.props["aria-label"].includes("unavailable")));
  const status = renderer.root.findAll((node) => node.props.role === "status").map((node) => node.children.join(" ")).join(" ");
  assert.match(status, /view could not be displayed/i);
  assert.doesNotMatch(status, /desktop\.viewer_failed|provider|path/);
  await act(async () => renderer.unmount());
});

test("startup, recovery, empty, draining, and stopped lifecycle states stay operable and accessible", async () => {
  const controller = fakeController(shellState({ lifecycle: { sequence: 0, phase: "starting", capabilities: {} } }));
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(DesktopShell, { controller, viewSelectionCapability: "engine.materialize_view" })); });
  assert.match(renderer.root.findByProps({ role: "status" }).children.join(""), /Starting/);
  await act(async () => controller.emit(shellState({ lifecycle: { sequence: 1, phase: "recovery", capabilities: {} } })));
  assert.match(renderer.root.findByProps({ role: "alert" }).children.join(""), /recovery/);
  await act(async () => renderer.root.findByType("button").props.onClick());
  assert.deepEqual(controller.calls.at(-1), ["reopen"]);
  await act(async () => controller.emit(shellState({ lifecycle: { sequence: 2, phase: "ready", capabilities: {} } })));
  assert.match(renderer.root.findByType("p").children.join(""), /Open or create/);
  await act(async () => controller.emit(shellState({ lifecycle: { sequence: 3, phase: "draining", capabilities: {} } })));
  assert.match(renderer.root.findByProps({ role: "status" }).children.join(""), /closing/);
  await act(async () => controller.emit(shellState({ lifecycle: { sequence: 4, phase: "stopped", capabilities: {} } })));
  assert.match(renderer.root.findByProps({ role: "status" }).children.join(""), /closing/);
  await act(async () => renderer.unmount());
});

test("Viewer empty, loading, cancelling, disposed, and recoverable states have status surfaces", async () => {
  for (const state of [
    { status: "empty", reason: "view_empty" }, { status: "loading", operation: "snapshot" }, { status: "cancelling" }, { status: "disposed" },
    { status: "recoverable_error", error: {}, previous: undefined },
  ]) {
    let renderer;
    await act(async () => { renderer = TestRenderer.create(React.createElement(DesktopViewerSurface, { state, onSelectionChange() {} })); });
    assert.ok(renderer.toJSON());
    await act(async () => renderer.unmount());
  }
});
