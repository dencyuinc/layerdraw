// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { BrowserRuntimeClient } from "@layerdraw/client-sdk/editor";
import { createBrowserEditor } from "@layerdraw/client-sdk/browser-editor";
import type { EditorEdit } from "@layerdraw/composer";
import { createWailsDesktopClient, type WailsDesktopClient, type WailsGeneratedBindings } from "@layerdraw/engine-client/wails";
import type { CapabilityID, CapabilityManifest, Digest } from "@layerdraw/protocol/common";
import type { MaterializeViewInput, WorkbenchPreviewResult } from "@layerdraw/protocol/engine";
import type { OpenRuntimeDocumentResult, RuntimeCommitResult, RuntimeOperationBatch } from "@layerdraw/protocol/runtime";
import type { ViewData } from "@layerdraw/protocol/semantic";
import type { RenderRecipe, RenderShape } from "@layerdraw/render";
import { createViewer, type Viewer, type ViewerSnapshot } from "@layerdraw/viewer";
import type { DesktopFeatureAvailability, DesktopLifecycleSnapshot, DesktopProjectContext, DesktopProjectPublicationDTO, DesktopViewerFrame } from "./contracts.js";
import type { DesktopEditorPreviewDTO, DesktopOwnerResultDTO, DesktopProjectHostBinding, DesktopProjectOwnerBinding } from "./wails-owner.js";

const releaseDigest = `sha256:${"0".repeat(64)}` as Digest;
const profileDigest = `sha256:${"1".repeat(64)}` as Digest;
const fontDigest = `sha256:${"2".repeat(64)}` as Digest;
const shapes = ["context", "diagram", "diff", "flow", "matrix", "table", "tree"] as const satisfies readonly RenderShape[];

function unwrap<T>(response: Readonly<{ outcome: string; payload?: T }>): T {
  if (response.outcome !== "success" || response.payload === undefined) throw new Error("desktop generated operation failed");
  return response.payload;
}

function runtimeAdapter(client: WailsDesktopClient, host: DesktopProjectHostBinding): BrowserRuntimeClient {
  const adapter: BrowserRuntimeClient = {
    getCapabilities: () => client.getCapabilities(),
    async openDocument(input, { signal }) {
      if (signal.aborted) throw new DOMException("Aborted", "AbortError");
      // Adopt the app-owned session (single tracked session per project);
      // opening a parallel runtime session would fork the working document
      // and durable commits against it fail closed.
      return await host.ProjectOpenSession({ document_id: (input as Readonly<{ document_id: string }>).document_id });
    },
    async previewEditor(edit: EditorEdit, session: OpenRuntimeDocumentResult, { signal }) {
      if (signal.aborted) throw new DOMException("Aborted", "AbortError");
      if (edit.kind !== "semantic_operations") throw new Error("Desktop Runtime supports semantic operation previews only");
      const operationBatch: RuntimeOperationBatch = {
        document_id: session.session.scope.document_id,
        base_revision: session.committed_revision,
        expected_definition_hash: session.committed_revision.definition_hash,
        operations: edit.request.batch,
        preconditions: edit.request.preconditions,
      };
      const result: DesktopEditorPreviewDTO = await host.PreviewEditor({ session: session.session, operation_batch: operationBatch });
      return {
        preview: result.preview as WorkbenchPreviewResult,
        authoring_proof: result.runtime.authoring_proof,
        operation_batch: operationBatch,
        authoring_decision: result.runtime.preview_evaluation.authoring_decision,
        grant_summary: result.grant_summary,
      };
    },
    async commitOperations(input, { signal }): Promise<RuntimeCommitResult> { return unwrap(await client.commitOperations(input, { signal })); },
    async materializeView(input: MaterializeViewInput, session, { signal }): Promise<ViewData> {
      if (signal.aborted) throw new DOMException("Aborted", "AbortError");
      return (await host.MaterializeProjectView(session, input.view_address)).view_data;
    },
    async closeDocument() {
      // The adopted session belongs to the app lifecycle; the host closes it
      // through CloseProject, never the frontend editor teardown.
    },
  };
  return Object.freeze(adapter);
}

function featureCapabilities(manifest: CapabilityManifest): Readonly<Record<CapabilityID, DesktopFeatureAvailability>> {
  const available = Object.freeze({ status: "available" as const });
  const unavailable = Object.freeze({ status: "unavailable" as const, reason: "not_advertised" as const });
  return Object.freeze({
    "desktop.project": available,
    "engine.materialize_view": manifest.operations["engine.materialize_view"]?.enabled === true ? available : unavailable,
    "desktop.recovery": available,
    "desktop.external_storage": available,
    "desktop.registry": available,
    "desktop.review": available,
  }) as Readonly<Record<CapabilityID, DesktopFeatureAvailability>>;
}

function recipe(shape: RenderShape): RenderRecipe {
  return {
    render_recipe_schema_version: 1,
    renderer_profile: { profile_id: "layerdraw/default", profile_version: "1.0", specification_digest: profileDigest },
    shape: { kind: shape },
    layout_algorithm: shape === "diagram" || shape === "tree" || shape === "flow" ? { kind: "layered", crossing_reduction: "median", rank_separation: 64 } : { kind: "native" },
    layout_seed: { value: "layerdraw-desktop" }, density: { value: "comfortable" }, orientation: { value: "top_to_bottom" },
    theme: { theme_id: "default", theme_version: "1", specification_digest: profileDigest, color_scheme: "light" },
    locale: { language_tag: "en-US" }, timezone: { iana_name: "UTC" },
    font_policy: { families: ["Inter"], fallback: "forbid", required_digests: [fontDigest] },
    asset_policy: { mode: "resolved_only", required_digests: [] }, interaction_policy: { selection: true, pan: true, zoom: true },
  };
}

function desktopViewer(manifest: CapabilityManifest): Viewer {
  // Renderer profiles describe the local presentation implementation, not an
  // Engine operation. The trusted Desktop owner advertises the renderer it
  // instantiates while preserving the generated Engine operation manifest.
  const advertised = { enabled: true, id: "layerdraw/default", version: "1.0" } as const;
  const viewerManifest: CapabilityManifest = Object.freeze({ ...manifest, manifest_etag: profileDigest, renderer_profiles: [advertised] });
  return createViewer({
    renderer_profile: { renderer_profile: { profile_id: advertised.id, profile_version: advertised.version, specification_digest: profileDigest }, supported_shapes: [...shapes], supported_algorithms: ["grid", "layered", "native", "radial"] },
    render_recipes: Object.fromEntries(shapes.map((shape) => [shape, recipe(shape)])) as Readonly<Record<RenderShape, RenderRecipe>>,
    asset_resolver: { async resolve() { return []; } },
    font_resolver: { async resolve(families, digests) { return families.map((family, index) => ({ family, digest: digests[index] ?? fontDigest, units_per_em: 1000, ascent: 750, descent: 250, line_gap: 100, default_advance: 500, glyph_advances: { " ": 250, i: 250, m: 750 } })); } },
    capability_manifest: viewerManifest,
    layout: { item_order: "viewdata", tie_breaker: "seeded_key_hash", coordinate_precision: 3, item_width: 96, item_height: 48, horizontal_gap: 24, vertical_gap: 20, container_padding: 12, cell_padding: 6, lane_header_size: 28, badge_size: 14, overlay_offset: 3, port_offset: 2, font_size: 14, asset_scale: 1 },
    render_limits: { max_primitives: 10000, max_route_points: 10000, max_text_length: 4096, max_depth: 128, max_extent: 1_000_000 },
    viewer_limits: { max_queued_updates: 4, max_snapshot_bytes: 5_000_000, max_snapshot_items: 100_000, max_render_items: 100_000, max_retained_bytes: 10_000_000, max_presentation_references: 1000, max_display_preferences: 32, max_display_preference_bytes: 4096, max_event_deliveries: 4, min_zoom: 0.1, max_zoom: 8, max_pan: 10_000 },
  });
}

export async function createDesktopWailsProjectOwner(host: DesktopProjectHostBinding, bindings: WailsGeneratedBindings): Promise<DesktopProjectOwnerBinding> {
  const client = await createWailsDesktopClient({
    bindings, bindingProtocolVersion: "1.0", expectedReleaseManifestDigest: releaseDigest,
    client: { expectedReleaseManifestDigest: releaseDigest },
  });
  const runtime = runtimeAdapter(client, host);
  const viewer = desktopViewer(client.engine.getCapabilities());
  let project: DesktopProjectContext | undefined;
  let viewerFrame: DesktopViewerFrame | undefined;
  let sequence = 0;
  const publish = async (dto: DesktopProjectPublicationDTO): Promise<DesktopLifecycleSnapshot> => {
    if (dto.project === undefined) return Object.freeze({ sequence: ++sequence, phase: dto.phase, capabilities: featureCapabilities(client.engine.getCapabilities()) });
    if (project?.project_id !== dto.project.project_id || project.session_generation !== dto.project.session_generation) {
      await project?.editor.close();
      viewerFrame = undefined;
      const editor = createBrowserEditor({
        engine_client: client.engine, runtime_client: runtime,
        asset_resolver: { async resolve() { throw new Error("Desktop asset is unavailable"); }, async put() { throw new Error("Desktop asset writes require a host operation"); }, describeCapability: () => ({ mode: "host_owned" }) },
        runtime_commit_input_factory: () => ({ operation_id: `desktop_editor_${++sequence}`, idempotency_key: `desktop_editor_${sequence}`, trigger: "explicit_save" }),
      });
      const editorSession = await editor.open({ authority: "runtime", input: dto.project.open_input });
      project = Object.freeze({
        project_id: dto.project.project_id, session_generation: dto.project.session_generation, display_name: dto.project.display_name,
        authoritative_revision_token: dto.project.authoritative_revision.revision_id, authoritative_revision_label: dto.project.authoritative_revision.revision_id,
        engine: client.engine,
        ...(editorSession.authority === "runtime" ? {
          readDocumentGeneration: () => host.ProjectDocumentGeneration(editorSession.session.session),
          readSubjects: () => host.ProjectSubjects(editorSession.session.session),
          readStructure: () => host.ProjectStructure(editorSession.session.session),
        } : {}),
        editor, editor_session: editorSession, views: dto.project.views, access: { status: "allowed" as const, label: "Local owner" },
        storage: { kind: "local" as const, status: "connected" as const, label: "Local project" }, persistence: dto.project.persistence, library_project: dto.project.library_project,
      }) satisfies DesktopProjectContext;
    } else {
      const revision = dto.project.authoritative_revision.revision_id;
      if (viewerFrame?.authoritative_revision_token !== revision) viewerFrame = undefined;
      const selected = project.selected_view_address !== undefined && dto.project.views.some((view) => view.address === project?.selected_view_address)
        ? project.selected_view_address
        : undefined;
      const { selected_view_address: _selectedViewAddress, ...projectWithoutSelection } = project;
      project = Object.freeze({
        ...projectWithoutSelection,
        display_name: dto.project.display_name,
        authoritative_revision_token: revision,
        authoritative_revision_label: revision,
        views: dto.project.views,
        persistence: dto.project.persistence,
        library_project: dto.project.library_project,
        ...(selected === undefined ? {} : { selected_view_address: selected }),
      });
    }
    if (project === undefined) throw new Error("Desktop project hydration failed");
    return Object.freeze({ sequence: ++sequence, phase: dto.phase, capabilities: featureCapabilities(client.engine.getCapabilities()), project, ...(viewerFrame === undefined ? {} : { viewer_frame: viewerFrame }) });
  };
  const refresh = () => host.ProjectPublication().then(publish);
  const success = async (): Promise<DesktopOwnerResultDTO> => ({ outcome: "success", publication: await refresh() });
  const select = async (input: Readonly<{ view_address: string }>, signal: AbortSignal): Promise<DesktopOwnerResultDTO> => {
    if (signal.aborted) return { outcome: "cancelled", failure: { code: "desktop.selection_cancelled", recoverable: true } };
    if (project?.editor_session.authority !== "runtime" || !project.views.some((view) => view.address === input.view_address)) return { outcome: "rejected", failure: { code: "desktop.selection_invalid", recoverable: true } };
    try {
      const materialized = await host.MaterializeProjectView(project.editor_session.session.session, input.view_address);
      if (signal.aborted) return { outcome: "cancelled", failure: { code: "desktop.selection_cancelled", recoverable: true } };
      const snapshot: ViewerSnapshot = { viewer_snapshot_schema_version: 1, sequence: ++sequence, complete: true, view_address: materialized.view_data.view_address, revision: materialized.view_data.revision, view_data_hash: materialized.view_data_hash as Digest, state_input: materialized.view_data.state_input, view_data: materialized.view_data };
      const result = await viewer.setViewData(snapshot);
      if (!result.ok) return { outcome: result.outcome === "cancelled" ? "cancelled" : "rejected", failure: { code: result.error.code, recoverable: result.error.recoverable } };
      const revision = materialized.view_data.revision.revision_id ?? project.authoritative_revision_token;
      project = Object.freeze({ ...project, selected_view_address: input.view_address });
      viewerFrame = Object.freeze({ project_id: project.project_id, session_generation: project.session_generation, view_address: input.view_address, authoritative_revision_token: revision, kind: "snapshot", input: snapshot });
      return { outcome: "success", publication: await refresh() };
    } catch {
      return { outcome: "rejected", failure: { code: "desktop.selection_failed", recoverable: true } };
    }
  };
  return Object.freeze({ ProjectPublication: refresh, SelectProjectView: select, ShowProjectRecoveryOptions: success, DisconnectProjectExternal: success, CreateProjectViewer: () => viewer });
}
