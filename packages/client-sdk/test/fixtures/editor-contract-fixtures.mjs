// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

export const localWasm = Object.freeze({
  mode: "local_wasm",
  manifest: { operations: { "engine.preview_operations": { enabled: true, protocol_version: "1.0" } } },
  expected_persistence: "ephemeral",
});

export const injectedHost = Object.freeze({
  mode: "injected_host",
  manifest: { operations: {
    "engine.preview_operations": { enabled: true, protocol_version: "1.0" },
    "runtime.commit_operations": { enabled: true, protocol_version: "1.0" },
  } },
  expected_persistence: "durable",
});

export const unavailableCapability = Object.freeze({
  status: "unavailable",
  capability_id: "runtime.commit_operations",
  reason: "not_configured",
});

export const deniedGrant = Object.freeze({
  authoring_decision: { outcome: "deny", missing_capabilities: ["schema:write"] },
});

export const staleRevision = Object.freeze({ conflicts: [{ kind: "stale_revision" }] });

export const closeReopen = Object.freeze(["open", "close", "open"]);
