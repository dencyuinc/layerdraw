// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import test from "node:test";
import { createDesktopWailsComposition, createDesktopWailsLifecycle, createUnopenedViewer, desktopLifecycleEvent } from "../dist/index.js";
import { wailsEngineBindingDescriptors, wailsRuntimeBindingDescriptors } from "@layerdraw/engine-client/wails";

test("Wails bootstrap derives only lifecycle framing and fails closed before a project publication", async () => {
  let eventName;
  let eventListener;
  const runtime = {
    EventsOn(name, listener) { eventName = name; eventListener = listener; },
    EventsOff() {},
  };
  const lifecycle = await createDesktopWailsLifecycle({ async State() { return "ready"; } }, runtime);
  assert.equal(eventName, desktopLifecycleEvent);
  assert.deepEqual(lifecycle.getSnapshot(), { sequence: 0, phase: "ready", capabilities: {} });
  let calls = 0;
  const unsubscribe = lifecycle.subscribe(() => { calls += 1; });
  eventListener({ state: "draining" });
  assert.equal(calls, 1);
  assert.equal(lifecycle.getSnapshot().phase, "draining");
  unsubscribe();
  eventListener({ state: "forged" });
  assert.equal(calls, 1);
  assert.equal(lifecycle.getSnapshot().phase, "recovery");
  await assert.rejects(lifecycle.selectView("view", new AbortController().signal));
  await assert.rejects(lifecycle.showRecoveryOptions(new AbortController().signal));
});

test("unopened Viewer never accepts hostless publications", async () => {
  const viewer = createUnopenedViewer();
  assert.deepEqual(viewer.getState(), { status: "empty", reason: "no_snapshot" });
  const rejected = await viewer.setViewData({});
  assert.equal(rejected.ok, false);
  assert.equal(rejected.error.code, "viewer.input_invalid");
  await viewer.dispose();
  assert.equal(viewer.getState().status, "disposed");
});

test("Wails composition exposes the exact generated Engine and Runtime closure", async () => {
  const composition = await createDesktopWailsComposition(
    { async State() { return "ready"; } },
    { EventsOn() {}, EventsOff() {} },
    async (_method, exchange) => ({ outcome: "success", value: exchange }),
  );
  const expected = [...wailsEngineBindingDescriptors, ...wailsRuntimeBindingDescriptors].map((item) => item.generatedMethod).sort();
  assert.deepEqual(Object.keys(composition.generatedBindings).sort(), expected);
  assert.equal(composition.lifecycle.getSnapshot().phase, "ready");
  assert.equal(composition.viewer.getState().status, "empty");
});
