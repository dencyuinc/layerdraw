// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import test from "node:test";
import {
  BrowserEditorError,
  RequiredEditorCapabilityError,
  negotiateEditorCapabilities,
} from "../dist/editor.js";
import {
  closeReopen,
  deniedGrant,
  injectedHost,
  localWasm,
  staleRevision,
  unavailableCapability,
} from "./fixtures/editor-contract-fixtures.mjs";

const enabled = { enabled: true, protocol_version: "1.0" };
const disabled = {
  enabled: false,
  protocol_version: "1.0",
  unavailable_reason: "not_configured",
};

test("Browser Editor exposes a closed typed failure channel", () => {
  const error = new BrowserEditorError("editor.stale_revision", "Refresh required.");
  assert.equal(error.code, "editor.stale_revision");
  assert.deepEqual(error.diagnostics, []);
});

test("required capabilities fail fast from the manifest", () => {
  assert.throws(
    () => negotiateEditorCapabilities(
      { operations: { "engine.preview_operations": disabled } },
      { required: ["engine.preview_operations"] },
    ),
    (error) => error instanceof RequiredEditorCapabilityError &&
      error.unavailable[0].capability_id === "engine.preview_operations",
  );
});

test("optional capabilities remain typed unavailable instead of disappearing", () => {
  assert.deepEqual(
    negotiateEditorCapabilities(
      { operations: {
        "engine.preview_operations": enabled,
        "runtime.commit_operations": disabled,
      } },
      {
        required: ["engine.preview_operations"],
        optional: ["runtime.commit_operations", "engine.materialize_view"],
      },
    ),
    {
      available: ["engine.preview_operations"],
      optional_unavailable: [
        { status: "unavailable", capability_id: "runtime.commit_operations", reason: "not_configured" },
        { status: "unavailable", capability_id: "engine.materialize_view", reason: "not_advertised" },
      ],
    },
  );
});

test("local WASM and injected host manifests negotiate the same operation names", () => {
  const local = negotiateEditorCapabilities(
    localWasm.manifest,
    { required: ["engine.preview_operations"] },
  );
  const host = negotiateEditorCapabilities(
    injectedHost.manifest,
    { required: ["engine.preview_operations"], optional: ["runtime.commit_operations"] },
  );
  assert.deepEqual(local.available, ["engine.preview_operations"]);
  assert.deepEqual(host.available, ["engine.preview_operations", "runtime.commit_operations"]);
  assert.equal(localWasm.expected_persistence, "ephemeral");
  assert.equal(injectedHost.expected_persistence, "durable");
});

test("closed and reopened sessions are represented as separate lifecycle results", () => {
  assert.deepEqual(closeReopen, ["open", "close", "open"]);
});

test("denied grants and stale revisions stay presentation data rather than capability inference", () => {
  assert.equal(deniedGrant.authoring_decision.outcome, "deny");
  assert.equal(staleRevision.conflicts[0].kind, "stale_revision");
  assert.equal(unavailableCapability.status, "unavailable");
});
