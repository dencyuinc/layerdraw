// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import test from "node:test";
import { createDesktopWailsComposition, createDesktopWailsLifecycle, createUnavailableViewer, desktopLifecycleEvent, waitForDesktopApplicationReady } from "../dist/index.js";
import { wailsEngineBindingDescriptors, wailsRuntimeBindingDescriptors } from "@layerdraw/engine-client/wails";

test("Wails bootstrap waits for native startup before invoking generated bindings", async () => {
  const phases = ["stopped", "starting", "ready"];
  let calls = 0;
  await waitForDesktopApplicationReady({ async State() { calls += 1; return phases.shift() ?? "ready"; } }, { timeoutMs: 100, pollIntervalMs: 0 });
  assert.equal(calls, 3);
  let transientCalls = 0;
  await waitForDesktopApplicationReady({ async State() { transientCalls += 1; if (transientCalls === 1) throw new Error("binding unavailable"); return "ready"; } }, { timeoutMs: 100, pollIntervalMs: 0 });
  assert.equal(transientCalls, 2);
  await assert.rejects(waitForDesktopApplicationReady({ async State() { return "recovery"; } }, { timeoutMs: 100, pollIntervalMs: 0 }), /failed to become ready/);
  await assert.rejects(waitForDesktopApplicationReady({ async State() { return "stopped"; } }, { timeoutMs: 0, pollIntervalMs: 0 }), /timed out/);
  await assert.rejects(waitForDesktopApplicationReady({ async State() { return "ready"; } }, { timeoutMs: -1 }), /options are invalid/);
});

test("Wails bootstrap derives only lifecycle framing and fails closed before a project publication", async () => {
  let eventName;
  let eventListener;
  const runtime = {
    EventsOn(name, listener) { eventName = name; eventListener = listener; },
    EventsOff() {},
  };
  const lifecycle = await createDesktopWailsLifecycle({ async State() { return "ready"; } }, runtime);
  assert.equal(eventName, desktopLifecycleEvent);
  assert.equal(lifecycle.getSnapshot().phase, "ready");
  assert.deepEqual(lifecycle.getSnapshot().capabilities["desktop.project"], { status: "unavailable", reason: "host_disabled" });
  let calls = 0;
  const unsubscribe = lifecycle.subscribe(() => { calls += 1; });
  eventListener({ state: "draining" });
  assert.equal(calls, 1);
  assert.equal(lifecycle.getSnapshot().phase, "draining");
  unsubscribe();
  eventListener({ state: "forged" });
  assert.equal(calls, 1);
  assert.equal(lifecycle.getSnapshot().phase, "recovery");
  await lifecycle.selectView("view", new AbortController().signal);
  assert.equal(lifecycle.getSnapshot().failure.code, "desktop.selection_failed");
  await lifecycle.showRecoveryOptions(new AbortController().signal);
  assert.equal(lifecycle.getSnapshot().failure.code, "desktop.lifecycle_failed");
});

test("capability-disabled Viewer reports an explicit closed state", async () => {
  const viewer = createUnavailableViewer();
  assert.equal(viewer.getState().status, "unsupported_profile");
  assert.equal(viewer.getState().error.details.reason, "host_disabled");
  const rejected = await viewer.setViewData({});
  assert.equal(rejected.ok, false);
  assert.equal(rejected.error.code, "viewer.profile_unsupported");
  await viewer.dispose();
  assert.equal(viewer.getState().status, "disposed");
});

test("project-owner actions use typed publications instead of frontend reconstruction", async () => {
  const calls = [];
  const project = { project_id: "project", session_generation: 7, display_name: "Project", authoritative_revision_token: "revision", authoritative_revision_label: "r1", editor: {}, editor_session: {}, views: [{ address: "view:main", label: "Main", shape: "diagram" }], selected_view_address: "view:main", access: { status: "allowed", label: "Allowed" }, storage: { kind: "local", status: "connected", label: "Local" }, persistence: "clean" };
  const publication = { sequence: 3, phase: "ready", capabilities: {}, project };
  let publicationCalls = 0;
  const eventListeners = new Map();
  const owner = {
    async ProjectPublication() { publicationCalls += 1; return publication; },
    async SelectProjectView(input) { calls.push(["select", input]); return { outcome: "success", publication: { ...publication, sequence: 4 } }; },
    async ShowProjectRecoveryOptions(input) { calls.push(["recovery", input]); return { outcome: "success", publication }; },
    async DisconnectProjectExternal(input) { calls.push(["disconnect", input]); return { outcome: "success", publication }; },
    CreateProjectViewer() { return createUnavailableViewer(); },
  };
  const lifecycle = await createDesktopWailsLifecycle({ async State() { return "ready"; } }, { EventsOn(name, listener) { eventListeners.set(name, listener); }, EventsOff() {} }, owner);
  eventListeners.get("layerdraw:desktop-project")();
  await new Promise((resolve) => setTimeout(resolve, 0));
  assert.equal(publicationCalls, 2);
  assert.equal(lifecycle.getSnapshot().sequence, 4);
  await lifecycle.selectView("view:main", new AbortController().signal);
  await lifecycle.showRecoveryOptions(new AbortController().signal);
  await lifecycle.disconnectExternal(new AbortController().signal);
  assert.deepEqual(calls.map(([kind]) => kind), ["select", "recovery", "disconnect"]);
  assert.equal(calls[0][1].session_generation, 7);
  assert.equal(calls[0][1].view_address, "view:main");
});

test("Wails composition exposes the exact generated Engine and Runtime closure", async () => {
  const application = {
    async State() { return "ready"; },
    async CreateProjectDialog() { return { outcome: "failed" }; },
    async OpenProjectDialog() { return { outcome: "failed" }; },
    async RecentProjects() { return { outcome: "success", value: [] }; },
    async ConnectExternal() { return { outcome: "failed" }; },
    async InspectExternal() { return { outcome: "failed" }; },
    async RefreshExternal() { return { outcome: "failed" }; },
    async DisconnectExternal() { return { outcome: "failed" }; },
    async SelectExternalRemote() { return { outcome: "failed" }; },
    async AcquireExternalLease() { return { outcome: "failed" }; },
    async PlanExternalReconcile() { return { outcome: "failed" }; },
    async ApplyExternalReconcile() { return { outcome: "failed" }; },
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
  assert.equal(composition.viewer.getState().status, "unsupported_profile");
  assert.equal(composition.library.status, "unavailable");
  assert.equal(composition.review.status, "unavailable");
  assert.equal((await composition.projectDialogs.recent()).outcome, "success");
  assert.equal((await composition.mcp.status()).enabled, false);
  assert.equal((await composition.nativeInterchange.profiles())[0].format, "json");
  const controller = new AbortController(); controller.abort();
  await assert.rejects(composition.nativeInterchange.publish({ request_id: "request", artifact_id: "artifact", extension: "json" }, controller.signal), { name: "AbortError" });
});
