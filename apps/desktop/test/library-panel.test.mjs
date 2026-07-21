// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import test from "node:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";
import { DesktopLibraryPanel } from "../dist/library-panel.js";

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const digest = `sha256:${"a".repeat(64)}`;
const source = { source_id: "local", kind: "local_directory", endpoint_ref: "/registry", trust_policy_id: "desktop-local", cache_policy: "on_demand", priority: 100, connected: false, revision: 1 };
const release = { identity: { kind: "template", canonical_id: "starter", version: "1.0.0" }, source_id: "local", publisher_id: "local", digest, manifest_digest: digest, dependency_metadata_digest: digest, size: 10, dependencies: [], compatibility: [], license: "MIT", provenance_digest: digest };
const plan = { transaction_id: "tx", plan_digest: digest, action: "create_from_template", artifacts: [release], migration_required: false };

function libraryFixture() {
  const calls = [];
  let state = { status: "idle", query: "", sources: [], results: [], capabilities: { browse: true, manage_sources: true, plan_transactions: true, commit_transactions: true, author_artifacts: true } };
  const publish = (next) => { state = { ...state, ...next }; return structuredClone(state); };
  return {
    calls,
    snapshot: () => structuredClone(state),
    cancel() { calls.push(["cancel"]); },
    async refreshSources() { calls.push(["sources"]); return publish({ status: "ready", sources: [source] }); },
    async search(query, kind) { calls.push(["search", query, kind]); return publish({ status: "ready", query, results: [release] }); },
    select(identity) { calls.push(["select", identity.canonical_id]); return publish({ status: "ready", selected: release }); },
    async preview(action, project) { calls.push(["preview", action, project]); return publish({ status: "awaiting_confirmation", plan }); },
    async confirm(operationID, idempotencyKey) { calls.push(["confirm", operationID, idempotencyKey]); return publish({ status: "committed", transaction: { plan, events: [{ state: "committed" }] } }); },
    async configureSource(configured) { calls.push(["configure", configured]); return publish({ status: "ready", sources: [{ ...configured, connected: false, revision: 1 }] }); },
    async connectSource(sourceID, connection) { calls.push(["connect", sourceID, connection]); return publish({ status: "ready", sources: [{ ...source, connected: true }] }); },
    async disconnectSource(sourceID) { calls.push(["disconnect", sourceID]); return publish({ status: "ready", sources: [source] }); },
    async recoverTransaction(transactionID) { calls.push(["recover", transactionID]); return publish({ status: "committed" }); },
  };
}

test("Library panel executes browse, template preview, confirmation, and source management through its owner", async () => {
  const library = libraryFixture();
  let renderer;
  await act(async () => { renderer = TestRenderer.create(React.createElement(DesktopLibraryPanel, { library })); });
  assert.deepEqual(library.calls[0], ["sources"]);

  const browse = renderer.root.findByProps({ "aria-label": "Browse Registry" });
  await act(async () => browse.findByProps({ type: "search" }).props.onChange({ currentTarget: { value: "starter" } }));
  await act(async () => browse.props.onSubmit({ preventDefault() {} }));
  assert.deepEqual(library.calls.find((call) => call[0] === "search"), ["search", "starter", undefined]);

  await act(async () => renderer.root.findByProps({ "aria-label": "Registry results" }).findByType("button").props.onClick());
  await act(async () => renderer.root.findByProps({ "aria-label": "Selected artifact" }).findByType("button").props.onClick());
  assert.deepEqual(library.calls.find((call) => call[0] === "preview"), ["preview", "create_from_template", undefined]);

  await act(async () => renderer.root.findByProps({ "aria-label": "Registry change preview" }).findByType("button").props.onClick());
  const confirmation = library.calls.find((call) => call[0] === "confirm");
  assert.match(confirmation[1], /^desktop-library-/);
  assert.equal(confirmation[1], confirmation[2]);

  const details = renderer.root.findByType("details");
  const connection = details.findAllByType("input")[0];
  await act(async () => connection.props.onChange({ currentTarget: { value: "credential:local" } }));
  await act(async () => details.findAllByType("button").find((button) => button.children.includes("Connect")).props.onClick());
  assert.deepEqual(library.calls.find((call) => call[0] === "connect"), ["connect", "local", "credential:local"]);

  const configure = renderer.root.findByProps({ "aria-label": "Configure Registry source" });
  const inputs = configure.findAllByType("input");
  await act(async () => inputs[0].props.onChange({ currentTarget: { value: "team" } }));
  await act(async () => inputs[1].props.onChange({ currentTarget: { value: "https://registry.example" } }));
  await act(async () => configure.props.onSubmit({ preventDefault() {} }));
  assert.equal(library.calls.find((call) => call[0] === "configure")[1].source_id, "team");

  await act(async () => renderer.unmount());
  assert.deepEqual(library.calls.at(-1), ["cancel"]);
});
