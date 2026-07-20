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
  const application = {
    async State() { return "ready"; },
    async NativeExportProfiles() { return { outcome: "success", value: [{ format: "json", schema_version: 1, requires_shape: [] }] }; },
    async SerializeNativeExport() { return { outcome: "success", value: { artifact: { artifact_id: "artifact", logical_path: "view.json", media_type: "application/json", content_digest: `sha256:${"a".repeat(64)}` }, source_manifest: {} } }; },
    async PublishNativeExportDialog() { return { outcome: "success", value: { published: true } }; },
    async ImportExternalDialog() { return { outcome: "success", value: { profile: "layerdraw.operations-json@1", media_type: "application/json", batch: { operations: [] }, source_hash: `sha256:${"b".repeat(64)}` } }; },
  };
  const composition = await createDesktopWailsComposition(
    application,
    { EventsOn() {}, EventsOff() {} },
		{ async MCPStatus() { return { enabled: false, transport: "local", instructions: "", generation: 0 }; }, async SetMCPEnabled() { return { outcome: "failed" }; }, async RestartMCP() { return { outcome: "failed" }; }, async ListMCPConnections() { return []; }, async CreateMCPConnection() { return { outcome: "failed" }; }, async RevokeMCPConnection() { return { outcome: "failed" }; } },
		async (_method, exchange) => ({ outcome: "success", value: exchange }),
  );
  const expected = [...wailsEngineBindingDescriptors, ...wailsRuntimeBindingDescriptors].map((item) => item.generatedMethod).sort();
  assert.deepEqual(Object.keys(composition.generatedBindings).sort(), expected);
  assert.equal(composition.lifecycle.getSnapshot().phase, "ready");
  assert.equal(composition.viewer.getState().status, "empty");
  assert.equal((await composition.mcp.status()).enabled, false);
  assert.equal((await composition.nativeInterchange.profiles())[0].format, "json");
  const controller = new AbortController(); controller.abort();
  await assert.rejects(composition.nativeInterchange.publish({ request_id: "request", artifact_id: "artifact", extension: "json" }, controller.signal), { name: "AbortError" });
});
