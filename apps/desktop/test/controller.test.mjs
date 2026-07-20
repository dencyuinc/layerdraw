// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import test from "node:test";
import { DesktopShellController } from "../dist/controller.js";

const emptyViewer = { status: "empty", reason: "no_snapshot" };
const readyViewer = { status: "ready", publication: { presentation: { selection_keys: [] } } };

function harness(initial) {
  let lifecycleSnapshot = initial;
  const lifecycleListeners = new Set();
  const calls = [];
  let selectionFailure = false;
  let recoveryFailure = false;
  let viewerFailure = false;
  let viewerReject = false;
  const lifecycle = {
    getSnapshot: () => lifecycleSnapshot,
    subscribe(listener) { lifecycleListeners.add(listener); return () => lifecycleListeners.delete(listener); },
    async selectView(address, signal) {
      calls.push(["select", address, signal]);
      if (selectionFailure) throw new Error("native path must not escape");
    },
    async showRecoveryOptions(signal) {
      calls.push(["recovery-options", signal]);
      if (recoveryFailure) throw new Error("provider details must not escape");
    },
  };
  const viewer = {
    getState: () => emptyViewer,
    async setViewData(input) {
      calls.push(["snapshot", input.sequence]);
      if (viewerFailure) throw new Error("renderer details");
      return viewerReject ? { ok: false, state: emptyViewer } : { ok: true, state: readyViewer };
    },
    async applyViewDataUpdate(input) {
      calls.push(["update", input.sequence]);
      if (viewerFailure) throw new Error("renderer details");
      return viewerReject ? { ok: false, state: emptyViewer } : { ok: true, state: readyViewer };
    },
    setSelection(keys) { calls.push(["viewer-selection", keys]); if (viewerFailure) throw new Error("selection details"); },
    async cancel() { calls.push(["cancel"]); },
  };
  return {
    controller: new DesktopShellController({ lifecycle, viewer }), calls,
    emit(next) { lifecycleSnapshot = next; for (const listener of lifecycleListeners) listener(); },
    listeners: lifecycleListeners,
    failSelection(value = true) { selectionFailure = value; },
    failRecovery(value = true) { recoveryFailure = value; },
    failViewer(value = true) { viewerFailure = value; },
    rejectViewer(value = true) { viewerReject = value; },
  };
}

function project(overrides = {}) {
  return {
    project_id: "p1", session_generation: 1, display_name: "Roadmap", authoritative_revision_token: "rev:8",
    authoritative_revision_label: "Revision 8", editor: {}, editor_session: {},
    views: [{ address: "view:main", label: "Main", shape: "diagram" }], selected_view_address: "view:main",
    access: { status: "allowed", label: "Local owner" },
    storage: { kind: "local", status: "connected", label: "On this Mac" }, persistence: "clean", ...overrides,
  };
}

function snapshot(sequence, overrides = {}) {
  return { sequence, phase: "ready", capabilities: {}, project: project(), ...overrides };
}

function frame(sequence, overrides = {}) {
  return {
    kind: sequence === 1 ? "snapshot" : "update", project_id: "p1", session_generation: 1, view_address: "view:main",
    authoritative_revision_token: "rev:8", input: { sequence }, ...overrides,
  };
}

async function settle() { await new Promise((resolve) => setImmediate(resolve)); }

test("accepts only monotonic frames bound to the active project, view, and authoritative revision", async () => {
  const h = harness(snapshot(0));
  let publications = 0;
  h.controller.subscribe(() => publications++);
  h.controller.start();
  h.emit(snapshot(1, { viewer_frame: frame(1) }));
  await settle();
  assert.equal(h.controller.getSnapshot().viewer.status, "ready");
  assert.deepEqual(h.calls, [["snapshot", 1]]);

  h.emit(snapshot(2, { viewer_frame: frame(1) }));
  await settle();
  assert.deepEqual(h.calls, [["snapshot", 1]], "duplicate Viewer frame is not replayed");
  h.emit(snapshot(1, { viewer_frame: frame(2) }));
  await settle();
  assert.deepEqual(h.calls, [["snapshot", 1]], "stale lifecycle publication is ignored");

  h.emit(snapshot(3, { viewer_frame: frame(2, { authoritative_revision_token: "rev:forged" }) }));
  await settle();
  assert.equal(h.controller.getSnapshot().failure.code, "desktop.context_mismatch");
  assert.ok(publications >= 4);
});

test("applies ordered updates and closes stale async completions", async () => {
  const h = harness(snapshot(0));
  h.controller.start();
  h.emit(snapshot(1, { viewer_frame: frame(1) }));
  await settle();
  h.emit(snapshot(2, { viewer_frame: frame(2) }));
  await settle();
  assert.deepEqual(h.calls.slice(0, 2), [["snapshot", 1], ["update", 2]]);
  await h.controller.close();
  assert.deepEqual(h.calls.at(-1), ["cancel"]);
  assert.equal(h.listeners.size, 0);
  h.emit(snapshot(3, { viewer_frame: frame(3) }));
  await settle();
  assert.equal(h.calls.some((call) => call[1] === 3), false);
});

test("selection is constrained to advertised views and exposes only closed failures", async () => {
  const h = harness(snapshot(0));
  h.controller.start();
  await h.controller.selectView("view:unknown");
  assert.equal(h.controller.getSnapshot().failure.code, "desktop.selection_failed");
  assert.deepEqual(h.calls, []);
  await h.controller.selectView("view:main");
  assert.equal(h.calls[0][0], "select");
  assert.equal(h.controller.getSnapshot().failure, undefined);
  h.failSelection();
  await h.controller.selectView("view:main");
  assert.deepEqual(h.controller.getSnapshot().failure, {
    code: "desktop.selection_failed", message_key: "desktop.error.selection_failed", recoverable: true,
  });
  assert.doesNotMatch(JSON.stringify(h.controller.getSnapshot()), /native path/);
});

test("explicit recovery handoff, Viewer rejection, exceptions, and selection stay typed and recoverable", async () => {
  const h = harness(snapshot(0));
  h.controller.start();
  h.failRecovery();
  await h.controller.reviewRecovery();
  assert.equal(h.controller.getSnapshot().failure.code, "desktop.lifecycle_failed");
  assert.doesNotMatch(JSON.stringify(h.controller.getSnapshot()), /provider details/);

  h.rejectViewer();
  h.emit(snapshot(1, { viewer_frame: frame(1) }));
  await settle();
  assert.equal(h.controller.getSnapshot().failure.code, "desktop.viewer_rejected");
  h.rejectViewer(false); h.failViewer();
  h.emit(snapshot(2, { viewer_frame: frame(2) }));
  await settle();
  assert.equal(h.controller.getSnapshot().failure.code, "desktop.viewer_failed");
  h.controller.setViewerSelection(["node:1"]);
  assert.equal(h.controller.getSnapshot().failure.code, "desktop.viewer_failed");
  h.failViewer(false);
  h.controller.setViewerSelection(["node:1"]);
  assert.equal(h.controller.getSnapshot().failure, undefined);
});

test("start and close are idempotent and closed controllers reject new work", async () => {
  const h = harness(snapshot(0));
  h.controller.start(); h.controller.start();
  assert.equal(h.listeners.size, 1);
  await h.controller.close(); await h.controller.close();
  await h.controller.selectView("view:main");
  await h.controller.reviewRecovery();
  h.controller.setViewerSelection(["ignored"]);
  assert.deepEqual(h.calls, [["cancel"]]);
});

test("same project reopen uses a new session generation and remount replays the authoritative frame", async () => {
  const h = harness(snapshot(0));
  h.controller.start();
  h.emit(snapshot(1, { viewer_frame: frame(1) }));
  await settle();
  h.emit(snapshot(2, {
    project: project({ session_generation: 2 }),
    viewer_frame: frame(1, { session_generation: 2 }),
  }));
  await settle();
  assert.deepEqual(h.calls.filter((call) => call[0] === "snapshot"), [["snapshot", 1], ["snapshot", 1]]);

  await h.controller.stop();
  h.controller.start();
  await settle();
  assert.equal(h.calls.filter((call) => call[0] === "snapshot").length, 3);
});
