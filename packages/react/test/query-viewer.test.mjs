// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import test from "node:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";
import { EditorProvider, LiveViewer, QueryViewComposer, useMaterializeCapability } from "../dist/index.js";

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const idle = Object.freeze({ phase: "idle", sequence: 0, can_undo: false, can_redo: false });
const capabilities = Object.freeze({ authority: "engine", manifest: { operations: {} }, selection: { available: [], optional_unavailable: [] } });

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 10));

function makeEditor() {
  const calls = [];
  return {
    calls,
    snapshot: () => idle,
    subscribe(listener) { listener(idle); return () => {}; },
    getCapabilities: () => capabilities,
    async preview(edit, options) { calls.push(["preview", edit, options]); return {}; },
    async materializeView(input, options) { calls.push(["materialize", input, options]); return { shape: "opaque" }; },
    cancelPreview() { return idle; },
  };
}

function publication(viewData, shape = "diagram") {
  return {
    sequence: 1,
    complete: true,
    view_address: "view:test",
    revision: {},
    view_data_hash: "hash",
    state_input: {},
    view_data: viewData,
    render_data: { shape },
    presentation: { selection_keys: ["selected"], zoom: 2, pan: { x: 3, y: 4 }, expanded_keys: [], sorting: [], display_preferences: {} },
  };
}

function makeViewer() {
  const calls = [];
  let current;
  return {
    calls,
    getPublication: () => current,
    async setViewData(snapshot) {
      calls.push(["set", snapshot]);
      current = publication(snapshot.view_data, snapshot.shape);
      return { ok: true, outcome: "published", state: { status: "ready", publication: current } };
    },
    async cancel() { calls.push(["cancel"]); },
  };
}

const session = Object.freeze({ authority: "engine", persistence: "ephemeral", session: {}, capabilities });

test("QueryViewComposer renders host schema and sends create/edit/duplicate/remove through Composer preview intents", async () => {
  const editor = makeEditor();
  const built = [];
  const kinds = ["create", "edit", "duplicate", "remove"];
  const intents = kinds.map((kind) => ({
    id: `intent-${kind}`,
    kind,
    label: kind,
    ...(kind === "create" ? {} : { target_id: "view-one" }),
    availability: { status: "available" },
    fields: kind === "create" ? [
      { id: "name", label: "Name", type: "text", value: "Initial", required: true },
      { id: "limit", label: "Limit", type: "number", value: 5 },
      { id: "dense", label: "Dense", type: "boolean", value: false },
      { id: "shape", label: "Shape", type: "select", value: "2d", options: [{ value: "2d", label: "2D" }, { value: "3d", label: "3D" }] },
    ] : [],
    buildEdit(draft) { built.push([kind, draft]); return { kind: "semantic_operations", request: { opaque: kind } }; },
  }));
  let selected;
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(EditorProvider, { editor, session },
    React.createElement(QueryViewComposer, {
      definitions: [{ id: "view-one", kind: "view", label: "Primary" }, { id: "query-off", kind: "query", label: "Disabled", availability: { status: "unavailable", reason: "not supported" } }],
      selectedId: "view-one", onSelect: (id) => { selected = id; }, intents,
    }))); });
  const selects = renderer.root.findAllByType("select");
  assert.equal(selects[0].findAllByType("option")[2].props.disabled, true);
  await act(async () => selects[0].props.onChange({ currentTarget: { value: "query-off" } }));
  assert.equal(selected, "query-off");
  const buttons = renderer.root.findAllByType("button");
  assert.deepEqual(buttons.slice(0, 4).map((button) => button.children.join("")), kinds);
  await act(async () => buttons[0].props.onClick());
  const inputs = renderer.root.findAllByType("input");
  await act(async () => inputs[0].props.onChange({ currentTarget: { value: "Updated" } }));
  await act(async () => renderer.root.findByType("form").props.onSubmit({ preventDefault() {} }));
  assert.deepEqual(built, [["create", { name: "Updated", limit: 5, dense: false, shape: "2d" }]]);
  assert.deepEqual(editor.calls[0], ["preview", { kind: "semantic_operations", request: { opaque: "create" } }, { intent_id: "intent-create" }]);
  await act(async () => renderer.unmount());
});

test("QueryViewComposer exposes typed unavailable/denied actions without invoking their builders", async () => {
  const editor = makeEditor();
  let builds = 0;
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(EditorProvider, { editor, session },
    React.createElement(QueryViewComposer, { definitions: [], onSelect() {}, intents: [
      { id: "off", kind: "create", label: "Unavailable", availability: { status: "unavailable", capability_id: "engine.materialize_view", reason: "renderer missing" }, fields: [], buildEdit() { builds += 1; return {}; } },
      { id: "denied", kind: "remove", label: "Denied", availability: { status: "denied", reason: "read only" }, fields: [], buildEdit() { builds += 1; return {}; } },
    ] }))); });
  for (const button of renderer.root.findAllByType("button")) { assert.equal(button.props.disabled, true); button.props.onClick(); }
  assert.equal(builds, 0);
  await act(async () => renderer.unmount());
});

test("active intents are re-resolved and fail closed after target or capability props change", async () => {
  const editor = makeEditor();
  let builds = 0;
  const availableIntent = { id: "edit", kind: "edit", label: "Edit", target_id: "view-one", availability: { status: "available" }, fields: [], buildEdit() { builds += 1; return {}; } };
  const definitions = [{ id: "view-one", kind: "view", label: "One" }, { id: "view-two", kind: "view", label: "Two" }];
  const element = (selectedId, intent) => React.createElement(EditorProvider, { editor, session }, React.createElement(QueryViewComposer, {
    definitions, selectedId, onSelect() {}, intents: [intent],
  }));
  let renderer;
  await act(async () => { renderer = TestRenderer.create(element("view-one", availableIntent)); });
  await act(async () => renderer.root.findByType("button").props.onClick());
  await act(async () => { renderer.update(element("view-two", availableIntent)); });
  await act(async () => renderer.root.findByType("form").props.onSubmit({ preventDefault() {} }));
  assert.equal(builds, 0);
  assert.match(renderer.root.findByProps({ role: "alert" }).children.join(""), /no longer selected/);
  await act(async () => { renderer.update(element("view-one", { ...availableIntent, availability: { status: "denied", reason: "read only" } })); });
  await act(async () => renderer.root.findByType("form").props.onSubmit({ preventDefault() {} }));
  assert.equal(builds, 0);
  assert.equal(renderer.root.findByProps({ role: "alert" }).children.join(""), "read only");
  await act(async () => renderer.unmount());
});

test("a throwing host intent builder becomes an actionable alert and never reaches preview", async () => {
  const editor = makeEditor();
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(EditorProvider, { editor, session }, React.createElement(QueryViewComposer, {
    definitions: [], onSelect() {}, intents: [{ id: "bad", kind: "create", label: "Create", availability: { status: "available" }, fields: [], buildEdit() { throw new Error("schema metadata is stale"); } }],
  }))); });
  await act(async () => renderer.root.findByType("button").props.onClick());
  await act(async () => renderer.root.findByType("form").props.onSubmit({ preventDefault() {} }));
  assert.equal(renderer.root.findByProps({ role: "alert" }).children.join(""), "schema metadata is stale");
  assert.equal(editor.calls.length, 0);
  await act(async () => renderer.unmount());
});

test("LiveViewer debounces, aborts obsolete materialization, suppresses stale results, and forwards authoritative ViewData unchanged", async () => {
  const first = deferred();
  const second = deferred();
  const editor = makeEditor();
  const flights = [first, second];
  const signals = [];
  editor.materializeView = (_input, options) => { signals.push(options.signal); return flights[signals.length - 1].promise; };
  const viewer = makeViewer();
  const states = [];
  const request = (key) => ({ key, input: { key }, toViewerSnapshot: (viewData) => ({ sequence: key === "one" ? 1 : 2, view_data: { forged: true }, shape: "2d" }) });
  const render = (value) => { states.push(value); return React.createElement("output", null, value.status); };
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(LiveViewer, { editor, viewer, request: request("one"), debounceMs: 0 }, render)); });
  await act(settle);
  assert.equal(states.at(-1).status, "materializing");
  await act(async () => { renderer.update(React.createElement(LiveViewer, { editor, viewer, request: request("two"), debounceMs: 0 }, render)); });
  await act(settle);
  assert.equal(signals[0].aborted, true);
  const fresh = { authoritative: "fresh" };
  await act(async () => { second.resolve(fresh); await second.promise; await new Promise((resolve) => setTimeout(resolve, 0)); });
  assert.equal(states.at(-1).status, "ready");
  assert.equal(viewer.calls.filter(([kind]) => kind === "set").length, 1);
  assert.equal(viewer.calls.find(([kind]) => kind === "set")[1].view_data, fresh, "React replaces any host-supplied placeholder with authoritative ViewData");
  const cancels = viewer.calls.filter(([kind]) => kind === "cancel").length;
  await act(async () => { renderer.update(React.createElement(LiveViewer, { editor, viewer, request: request("two"), debounceMs: 0 }, render)); });
  await act(settle);
  assert.equal(signals.length, 2, "a fresh request object with the same key does not rematerialize");
  assert.equal(viewer.calls.filter(([kind]) => kind === "cancel").length, cancels, "same-key rerenders do not cancel Viewer state");
  await act(async () => { first.resolve({ authoritative: "stale" }); await first.promise; await Promise.resolve(); });
  assert.equal(viewer.calls.filter(([kind]) => kind === "set").length, 1);
  await act(async () => renderer.unmount());
});

test("LiveViewer reports optional/required capability absence and materialization diagnostics", async () => {
  const editor = makeEditor();
  const viewer = makeViewer();
  let current;
  const render = (state) => { current = state; return null; };
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(LiveViewer, {
    editor, viewer, availability: { status: "unavailable", capability_id: "renderer.3d", reason: "3D renderer unavailable", requirement: "required" },
  }, render)); });
  assert.deepEqual(current, { status: "unavailable", capability_id: "renderer.3d", reason: "3D renderer unavailable", requirement: "required" });
  assert.equal(renderer.root.findByProps({ role: "alert" }).children.join(""), "3D renderer unavailable");
  assert.equal(editor.calls.length, 0);
  editor.materializeView = async () => { throw Object.assign(new Error("failed"), { diagnostics: [{ code: "query.invalid" }] }); };
  await act(async () => { renderer.update(React.createElement(LiveViewer, {
    editor, viewer, request: { key: "failure", input: {}, toViewerSnapshot: () => ({}) }, debounceMs: 0,
  }, render)); });
  await act(settle);
  assert.equal(current.status, "error");
  assert.deepEqual(current.diagnostics, [{ code: "query.invalid" }]);
  await act(async () => renderer.unmount());
});

test("useMaterializeCapability propagates required absence into a fail-clear alert", async () => {
  const editor = makeEditor();
  const viewer = makeViewer();
  function RequiredViewer() {
    const availability = useMaterializeCapability("renderer.required", "required");
    return React.createElement(LiveViewer, { editor, viewer, availability }, () => null);
  }
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(EditorProvider, { editor, session }, React.createElement(RequiredViewer))); });
  assert.equal(renderer.root.findByProps({ role: "alert" }).children.join(""), "not_advertised");
  assert.equal(editor.calls.length, 0);
  await act(async () => renderer.unmount());
});

test("compatible 2D/3D publications remain Viewer-owned; React never resets selection or camera", async () => {
  const editor = makeEditor();
  const viewer = makeViewer();
  viewer.updatePresentation = () => { throw new Error("React must not own presentation semantics"); };
  let renderer;
  for (const shape of ["2d", "3d"]) {
    await act(async () => {
      const element = React.createElement(LiveViewer, {
        editor, viewer, request: { key: shape, input: { shape }, toViewerSnapshot: () => ({ shape }) }, debounceMs: 0,
      }, () => null);
      if (renderer === undefined) renderer = TestRenderer.create(element); else renderer.update(element);
    });
    await act(settle);
    assert.deepEqual(viewer.getPublication().presentation, { selection_keys: ["selected"], zoom: 2, pan: { x: 3, y: 4 }, expanded_keys: [], sorting: [], display_preferences: {} });
  }
  await act(async () => renderer.unmount());
});
