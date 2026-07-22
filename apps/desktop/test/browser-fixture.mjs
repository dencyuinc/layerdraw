// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import { mountDesktopShell } from "../dist/index.js";
import { auditAccessibility } from "../dist/native-shell.js";

const listeners = new Set();
const renderData = {
  kind: "diagram", shape: "diagram", bounds: { x: 0, y: 0, width: 800, height: 500 },
  containers: [
    { render_key: "layer:product", bounds: { x: 30, y: 30, width: 300, height: 180 }, child_keys: ["node:one"] },
    { render_key: "layer:delivery", bounds: { x: 430, y: 240, width: 300, height: 180 }, child_keys: ["node:two"] },
  ],
  ports: [
    { render_key: "from", position: { x: 260, y: 115 }, occurrence_key: "node:one" },
    { render_key: "to", position: { x: 480, y: 315 }, occurrence_key: "node:two" },
  ], overlays: [], badges: [], support_geometry: [], diagnostics: [], source_bindings: [],
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
let viewerSelection = [];
const viewerState = () => ({ status: "ready", publication: { render_data: renderData, presentation: { selection_keys: viewerSelection } } });
const editorManifest = { operations: { "engine.preview_operations": { enabled: true }, "runtime.commit_operations": { enabled: true } } };
const editorSession = { authority: "runtime", persistence: "durable", session: {}, capabilities: { authority: "runtime", manifest: editorManifest, selection: { available: [], optional_unavailable: [] } } };
let editorSnapshot = { phase: "idle", sequence: 0, can_undo: true, can_redo: false };
const editorListeners = new Set();
const calls = [];
const editor = {
  snapshot: () => editorSnapshot, subscribe(listener) { editorListeners.add(listener); listener(editorSnapshot); return () => editorListeners.delete(listener); },
  getCapabilities: () => editorSession.capabilities,
  async undo() { calls.push(["undo"]); return editorSnapshot; }, async redo() { return editorSnapshot; }, async retry() { return editorSnapshot; }, cancelPreview() { return editorSnapshot; },
};
const project = {
  project_id: "project:roadmap", session_generation: 1, display_name: "Desktop roadmap", authoritative_revision_token: "revision:12", authoritative_revision_label: "Revision 12",
  editor, editor_session: editorSession, views: [{ address: "view:diagram", label: "System map", shape: "diagram" }, { address: "view:table", label: "Inventory", shape: "table" }],
  selected_view_address: "view:diagram", access: { status: "allowed", label: "Local owner" }, storage: { kind: "local", status: "connected", label: "On this Mac" }, persistence: "clean",
  library_project: { project_id: "ldl:project:roadmap", revision: "revision:12", definition_hash: "sha256:definition", resolved_lock_digest: "sha256:lock", dependency_snapshot: { resolved_lock_digest: "sha256:lock", installs: [] } },
};
let available = true;
let sequence = 0;
let lifecycleSnapshot;
function makeLifecycle(selected = project.selected_view_address) {
  return { sequence: ++sequence, phase: "ready", capabilities: available ? { "engine.materialize_view": { status: "available" } } : {}, project: { ...project, selected_view_address: selected } };
}
lifecycleSnapshot = makeLifecycle();
const lifecycle = {
  getSnapshot: () => lifecycleSnapshot,
  subscribe(listener) { listeners.add(listener); return () => listeners.delete(listener); },
  async showRecoveryOptions() {},
  async selectView(address) { calls.push(["select", address]); lifecycleSnapshot = makeLifecycle(address); for (const listener of listeners) listener(); },
};
const viewerPort = {
  getState: viewerState, setSelection(keys) { viewerSelection = [...keys]; calls.push(["viewer", ...keys]); }, async cancel() {},
};
let mcpEnabled = false;
let mcpGeneration = 1;
let mcpConnections = [];
const mcp = {
	async status() { return { enabled: mcpEnabled, transport: "local", instructions: "Use the local Desktop MCP entrypoint.", generation: mcpGeneration }; },
	async setEnabled(enabled) { mcpEnabled = enabled; mcpGeneration += 1; calls.push(["mcp-enable", enabled]); return { outcome: "success", value: await this.status() }; },
	async restart() { mcpGeneration += 2; mcpConnections = mcpConnections.map((value) => ({ ...value, status: "host_restarted" })); calls.push(["mcp-restart"]); return { outcome: "success", value: await this.status() }; },
	async listConnections() { return mcpConnections; },
	async createConnection(request) { const value = { connection_id: "connection-1", client_id: request.client_id, session_id: "session-1", protocol_version: request.protocol_version, document_id: request.document_id, delegation_id: "delegation-1", agent_id: request.agent_id, capabilities: request.capabilities, permissions: request.permissions, expires_at: request.expires_at, generation: "1", status: "connected" }; mcpConnections = [value]; calls.push(["mcp-connect", request]); return { outcome: "success", value }; },
	async revokeConnection(id) { mcpConnections = mcpConnections.map((value) => ({ ...value, status: "revoked" })); calls.push(["mcp-revoke", id]); return { outcome: "success", value: mcpConnections[0] }; },
};
const registrySource = { source_id: "official", kind: "official", endpoint_ref: "https://registry.layerdraw.test", trust_policy_id: "desktop-local", cache_policy: "on_demand", priority: 100, connected: true, revision: 1 };
const registryRelease = { identity: { kind: "pack", canonical_id: "layerdraw/catalog", version: "1.0.0" }, source_id: "official", publisher_id: "layerdraw", digest: "sha256:artifact", manifest_digest: "sha256:manifest", dependency_metadata_digest: "sha256:dependencies", size: 10, dependencies: [], compatibility: [], license: "MIT", provenance_digest: "sha256:provenance" };
let libraryState = { status: "idle", query: "", sources: [], results: [], capabilities: { browse: true, manage_sources: true, plan_transactions: true, commit_transactions: true, author_artifacts: true } };
const libraryPublish = (extension) => structuredClone(libraryState = { ...libraryState, ...extension });
const library = {
  snapshot: () => structuredClone(libraryState), cancel() {},
  async refreshSources() { calls.push(["library-sources"]); return libraryPublish({ status: "ready", sources: [registrySource] }); },
  async search(query, kind) { calls.push(["library-search", query, kind]); return libraryPublish({ status: "ready", query, results: [registryRelease] }); },
  select(identity) { calls.push(["library-select", identity.canonical_id]); return libraryPublish({ selected: registryRelease, status: "ready" }); },
  async preview(action, context) { calls.push(["library-preview", action, context.project_id]); return libraryPublish({ status: "awaiting_confirmation", plan: { transaction_id: "tx", plan_digest: "sha256:plan", action, artifacts: [{ release: registryRelease }], migration_required: false } }); },
  async confirm(operation, idempotency) { calls.push(["library-confirm", operation, idempotency]); return libraryPublish({ status: "committed", transaction: { plan: libraryState.plan, events: [{ state: "committed" }] } }); },
  async configureSource(source) { calls.push(["library-configure", source.source_id]); return libraryPublish({ status: "ready", sources: [{ ...source, connected: false, revision: 1 }] }); },
  async connectSource(sourceID, connection) { calls.push(["library-connect", sourceID, connection]); return libraryPublish({ status: "ready", sources: [{ ...registrySource, source_id: sourceID, connected: true }] }); },
  async disconnectSource(sourceID) { calls.push(["library-disconnect", sourceID]); return libraryPublish({ status: "ready", sources: [{ ...registrySource, source_id: sourceID, connected: false }] }); },
  async recoverTransaction(transactionID) { calls.push(["library-recover", transactionID]); return libraryPublish({ status: "committed" }); },
};
let storedSettings = { schema_version: 1, theme: "light", zoom_percent: 100, locale: "en" };
const settings = {
  async load() { calls.push(["settings-load"]); return { outcome: "success", value: storedSettings }; },
  async update(value) { calls.push(["settings-update", value]); storedSettings = value; return { outcome: "success", value }; },
};
mountDesktopShell(document.querySelector("#root"), {
	lifecycle, viewer: viewerPort, mcp, library, settings, viewSelectionCapability: "engine.materialize_view",
  editorCapabilities: { preview: "engine.preview_operations", apply: "runtime.commit_operations", history: "runtime.commit_operations" },
});
window.go = { desktopwails: { ShellBinding: {
  async CommandStatus() { return { outcome: "success", value: [{ id: "desktop.settings", generation: "1" }] }; },
  async InvokeCommand() { return { outcome: "success", value: { id: "desktop.settings", generation: "1" } }; },
} } };
window.desktopWorkflow = {
  calls,
  audit: auditAccessibility,
  capability(value) { available = value; lifecycleSnapshot = makeLifecycle(lifecycleSnapshot.project.selected_view_address); for (const listener of listeners) listener(); },
};
