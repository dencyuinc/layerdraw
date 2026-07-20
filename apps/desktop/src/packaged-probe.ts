// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { RenderData } from "@layerdraw/render";
import type { ViewerState } from "@layerdraw/viewer";
import { mountDesktopShell } from "./mount.js";

const digest = `sha256:${"0".repeat(64)}`;
const renderData = Object.freeze({
  render_data_schema_version: 1,
  renderer_profile: { profile_id: "desktop.packaged-probe", profile_version: "1.0.0", specification_digest: digest },
  view_data_hash: digest, render_input_hash: digest, shape: "diagram", layout_seed: "desktop-packaged-probe",
  locale: "en", timezone: "UTC", bounds: { x: 0, y: 0, width: 900, height: 540 }, source_bindings: [],
  resolved_asset_digests: [], resolved_font_digests: [], diagnostics: [], kind: "diagram",
  containers: [
    { render_key: "layer:application", bounds: { x: 35, y: 35, width: 810, height: 190 }, child_keys: ["node:product", "node:delivery"] },
    { render_key: "layer:infrastructure", bounds: { x: 35, y: 300, width: 810, height: 190 }, child_keys: ["node:runtime", "node:storage"] },
  ],
  ports: [
    { render_key: "port:product", position: { x: 300, y: 130 }, occurrence_key: "node:product" },
    { render_key: "port:delivery", position: { x: 540, y: 130 }, occurrence_key: "node:delivery" },
    { render_key: "port:runtime", position: { x: 300, y: 395 }, occurrence_key: "node:runtime" },
    { render_key: "port:storage", position: { x: 540, y: 395 }, occurrence_key: "node:storage" },
  ],
  overlays: [], badges: [], support_geometry: [],
  occurrences: [
    { render_key: "node:product", bounds: { x: 95, y: 85, width: 205, height: 85 }, port_keys: ["port:product"], label_key: "label:product" },
    { render_key: "node:delivery", bounds: { x: 540, y: 85, width: 205, height: 85 }, port_keys: ["port:delivery"], label_key: "label:delivery" },
    { render_key: "node:runtime", bounds: { x: 95, y: 350, width: 205, height: 85 }, port_keys: ["port:runtime"], label_key: "label:runtime" },
    { render_key: "node:storage", bounds: { x: 540, y: 350, width: 205, height: 85 }, port_keys: ["port:storage"], label_key: "label:storage" },
  ],
  labels: [
    { render_key: "label:product", bounds: { x: 110, y: 100, width: 175, height: 30 }, text: "Product", anchor: { kind: "occurrence", occurrence_key: "node:product" } },
    { render_key: "label:delivery", bounds: { x: 555, y: 100, width: 175, height: 30 }, text: "Delivery", anchor: { kind: "occurrence", occurrence_key: "node:delivery" } },
    { render_key: "label:runtime", bounds: { x: 110, y: 365, width: 175, height: 30 }, text: "Runtime", anchor: { kind: "occurrence", occurrence_key: "node:runtime" } },
    { render_key: "label:storage", bounds: { x: 555, y: 365, width: 175, height: 30 }, text: "Storage", anchor: { kind: "occurrence", occurrence_key: "node:storage" } },
  ],
  edge_paths: [
    { render_key: "edge:product-runtime", points: [{ x: 300, y: 130 }, { x: 300, y: 395 }], from_port_key: "port:product", to_port_key: "port:runtime" },
    { render_key: "edge:delivery-storage", points: [{ x: 540, y: 130 }, { x: 540, y: 395 }], from_port_key: "port:delivery", to_port_key: "port:storage" },
  ],
}) as unknown as RenderData;

const editorManifest = Object.freeze({ operations: { "engine.preview_operations": { enabled: false, unavailable_reason: "packaged_probe" }, "runtime.commit_operations": { enabled: false, unavailable_reason: "packaged_probe" } } });
const editorSession = Object.freeze({ authority: "runtime", persistence: "durable", session: {}, capabilities: { authority: "runtime", manifest: editorManifest, selection: { available: [], optional_unavailable: [] } } });
const editorSnapshot = Object.freeze({ phase: "idle", sequence: 0, can_undo: false, can_redo: false });
const editor = Object.freeze({
  snapshot: () => editorSnapshot, subscribe: (listener: (value: typeof editorSnapshot) => void) => { listener(editorSnapshot); return () => {}; },
  getCapabilities: () => editorSession.capabilities, async undo() { return editorSnapshot; }, async redo() { return editorSnapshot; },
  async retry() { return editorSnapshot; }, cancelPreview() { return editorSnapshot; },
});

/** Mounts real production Desktop components with deterministic owner-shaped data. */
export function mountPackagedProbeShell(root: Element): void {
  let selection: readonly string[] = [];
  const viewerState = (): ViewerState => ({ status: "ready", publication: { render_data: renderData, presentation: { selection_keys: selection } } } as ViewerState);
  const project = {
    project_id: "project:packaged-probe", session_generation: 1, display_name: "Packaged Desktop conformance",
    authoritative_revision_token: "revision:1", authoritative_revision_label: "Revision 1", editor, editor_session: editorSession,
    views: [{ address: "view:diagram", label: "System map", shape: "diagram" }], selected_view_address: "view:diagram",
    access: { status: "allowed", label: "Local owner" }, storage: { kind: "local", status: "connected", label: "On this device" }, persistence: "clean",
  };
  const lifecycle = {
    getSnapshot: () => ({ sequence: 1, phase: "ready", capabilities: { "engine.materialize_view": { status: "available" } }, project }),
    subscribe: () => () => {}, async selectView() {}, async showRecoveryOptions() {},
  };
  const viewer = {
    getState: viewerState,
    setSelection(keys: readonly string[]) { selection = [...keys]; },
    async cancel() {},
  };
  mountDesktopShell(root, {
    lifecycle, viewer, viewSelectionCapability: "engine.materialize_view",
    editorCapabilities: { preview: "engine.preview_operations", apply: "runtime.commit_operations", history: "runtime.commit_operations" },
  } as never);
}
