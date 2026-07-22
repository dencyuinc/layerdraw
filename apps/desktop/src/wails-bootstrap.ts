// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { WailsGeneratedBindings } from "@layerdraw/engine-client/wails";
import { createLibrary } from "@layerdraw/library";
import type { CapabilityID } from "@layerdraw/protocol/common";
import { createHostRegistryClient } from "@layerdraw/registry-client/host";
import { createReviewModel } from "@layerdraw/review";
import type { Viewer, ViewerPresentationState, ViewerState } from "@layerdraw/viewer";
import type { DesktopExternalImportPreview, DesktopExternalStoragePort, DesktopFeatureAvailability, DesktopHostResult, DesktopLifecyclePhase, DesktopLifecycleSnapshot, DesktopMCPConnectRequest, DesktopMCPConnection, DesktopMCPPort, DesktopMCPResult, DesktopMCPStatus, DesktopNativeExportProfile, DesktopNativeInterchangePort, DesktopNativeSerializeResult, DesktopProjectDialogPort, DesktopProjectLifecyclePort, DesktopProjectOpenDTO, DesktopRecentProjectDTO } from "./contracts.js";
import { createDesktopGeneratedBindings, type DesktopWailsInvoke } from "./wails-bindings.js";
import type { DesktopLibraryFeature, DesktopProjectOwnerBinding, DesktopRegistryHostBinding, DesktopReviewFeature, DesktopReviewHostBinding } from "./wails-owner.js";

export const desktopLifecycleEvent = "layerdraw:desktop-lifecycle";
export const desktopProjectEvent = "layerdraw:desktop-project";

type JsonObject = Readonly<Record<string, unknown>>;

export interface DesktopWailsApplicationBinding {
  State(): Promise<unknown>;
  CreateProjectDialog(requestID: string): Promise<DesktopHostResult<DesktopProjectOpenDTO>>;
  OpenProjectDialog(requestID: string): Promise<DesktopHostResult<DesktopProjectOpenDTO>>;
  RecentProjects(): Promise<DesktopHostResult<readonly DesktopRecentProjectDTO[]>>;
  OpenRecentProject(projectID: string): Promise<DesktopHostResult<DesktopProjectOpenDTO>>;
  CloseCurrentProject(): Promise<{ readonly outcome: string }>;
  ConnectExternal(input: JsonObject): Promise<DesktopHostResult<JsonObject>>;
  InspectExternal(connectionID: string): Promise<DesktopHostResult<JsonObject>>;
  RefreshExternal(connectionID: string): Promise<DesktopHostResult<JsonObject>>;
  DisconnectExternal(connectionID: string): Promise<DesktopHostResult<JsonObject>>;
  SelectExternalRemote(input: JsonObject): Promise<DesktopHostResult<JsonObject>>;
  AcquireExternalLease(session: JsonObject, binding: JsonObject): Promise<DesktopHostResult<JsonObject>>;
  PlanExternalReconcile(session: JsonObject, input: JsonObject, restricted: boolean): Promise<DesktopHostResult<JsonObject>>;
  ApplyExternalReconcile(session: JsonObject, plan: JsonObject, resolution: string): Promise<DesktopHostResult<JsonObject>>;
  NativeExportProfiles(): Promise<unknown>;
  SerializeNativeExport(input: unknown): Promise<unknown>;
  PublishNativeExportDialog(input: unknown): Promise<unknown>;
  ImportExternalDialog(input: unknown): Promise<unknown>;
}

export interface DesktopWailsMCPBinding {
  MCPStatus(): Promise<DesktopMCPStatus>;
  SetMCPEnabled(enabled: boolean, transport: "local"): Promise<DesktopMCPResult<DesktopMCPStatus>>;
  RestartMCP(): Promise<DesktopMCPResult<DesktopMCPStatus>>;
  ListMCPConnections(): Promise<readonly DesktopMCPConnection[]>;
  CreateMCPConnection(request: DesktopMCPConnectRequest): Promise<DesktopMCPResult<DesktopMCPConnection>>;
  RevokeMCPConnection(connectionID: string): Promise<DesktopMCPResult<DesktopMCPConnection>>;
  MCPClientConfig(connectionID: string): Promise<string>;
  DeleteMCPConnection(connectionID: string): Promise<DesktopMCPResult<DesktopMCPConnection>>;
}

export interface DesktopWailsRuntimeBinding {
  EventsOn(name: string, listener: (event: unknown) => void): void;
  EventsOff(name: string): void;
  LogError?(message: string): void;
}

export interface DesktopWailsOwnerBindings {
  readonly project?: DesktopProjectOwnerBinding;
  readonly registry?: DesktopRegistryHostBinding;
  readonly review?: DesktopReviewHostBinding;
}

export interface DesktopWailsComposition {
  readonly lifecycle: DesktopProjectLifecyclePort;
  readonly viewer: Viewer;
  readonly generatedBindings: WailsGeneratedBindings;
  readonly projectDialogs: DesktopProjectDialogPort;
  readonly externalStorage: DesktopExternalStoragePort;
  readonly nativeInterchange: DesktopNativeInterchangePort;
  readonly library: DesktopLibraryFeature;
  readonly review: DesktopReviewFeature;
  readonly mcp: DesktopMCPPort;
}

interface RefreshableDesktopLifecycle extends DesktopProjectLifecyclePort {
  refreshPublication(): Promise<void>;
}

type DesktopResult = Readonly<{ outcome?: unknown; value?: unknown }>;
function successful(value: unknown): unknown {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("desktop native interchange failed");
  const result = value as DesktopResult;
  if (result.outcome !== "success" || result.value === undefined) throw new Error("desktop native interchange failed");
  return result.value;
}

function nativeInterchange(application: DesktopWailsApplicationBinding): DesktopNativeInterchangePort {
  return Object.freeze({
    async profiles() { return successful(await application.NativeExportProfiles()) as readonly DesktopNativeExportProfile[]; },
    async serialize(input: Parameters<DesktopNativeInterchangePort["serialize"]>[0], signal: AbortSignal) {
      if (signal.aborted) throw new DOMException("Aborted", "AbortError");
      return successful(await application.SerializeNativeExport({ plan: input.export_plan, view_data: input.view_data, assets: [], fonts: [] })) as DesktopNativeSerializeResult;
    },
    async publish(input: Parameters<DesktopNativeInterchangePort["publish"]>[0], signal: AbortSignal) {
      if (signal.aborted) throw new DOMException("Aborted", "AbortError");
      successful(await application.PublishNativeExportDialog(input));
    },
    async importOperations(input: Parameters<DesktopNativeInterchangePort["importOperations"]>[0], signal: AbortSignal) {
      if (signal.aborted) throw new DOMException("Aborted", "AbortError");
      return successful(await application.ImportExternalDialog(input)) as DesktopExternalImportPreview;
    },
  });
}

function lifecyclePhase(value: unknown): DesktopLifecyclePhase {
  return value === "starting" || value === "ready" || value === "recovery" || value === "draining" || value === "stopped" ? value : "recovery";
}

function eventPhase(value: unknown): DesktopLifecyclePhase {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return "recovery";
  return lifecyclePhase((value as Readonly<Record<string, unknown>>).state);
}

const unavailable: DesktopFeatureAvailability = Object.freeze({ status: "unavailable", reason: "host_disabled" });

export async function waitForDesktopApplicationReady(
  application: Pick<DesktopWailsApplicationBinding, "State">,
  options: Readonly<{ timeoutMs?: number; pollIntervalMs?: number }> = {},
): Promise<void> {
  const timeoutMs = options.timeoutMs ?? 30_000;
  const pollIntervalMs = options.pollIntervalMs ?? 10;
  if (!Number.isSafeInteger(timeoutMs) || timeoutMs < 0 || !Number.isSafeInteger(pollIntervalMs) || pollIntervalMs < 0) {
    throw new Error("Desktop readiness options are invalid");
  }
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    try {
      const phase = lifecyclePhase(await application.State());
      if (phase === "ready") return;
      if (phase === "recovery" || phase === "draining") throw new Error("Desktop backend failed to become ready");
    } catch (error) {
      if (error instanceof Error && error.message === "Desktop backend failed to become ready") throw error;
    }
    if (Date.now() >= deadline) throw new Error("Desktop backend readiness timed out");
    await new Promise<void>((resolve) => setTimeout(resolve, pollIntervalMs));
  }
}

function unavailableCapabilities(): Readonly<Record<CapabilityID, DesktopFeatureAvailability>> {
  return Object.freeze({
    "desktop.project": unavailable,
    "engine.materialize_view": unavailable,
    "desktop.recovery": unavailable,
    "desktop.external_storage": unavailable,
    "desktop.registry": unavailable,
    "desktop.review": unavailable,
  }) as Readonly<Record<CapabilityID, DesktopFeatureAvailability>>;
}

export async function createDesktopWailsLifecycle(
  application: Pick<DesktopWailsApplicationBinding, "State">,
  runtime: DesktopWailsRuntimeBinding,
  owner?: DesktopProjectOwnerBinding,
): Promise<RefreshableDesktopLifecycle> {
  const owned = owner === undefined ? undefined : await owner.ProjectPublication();
  let sequence = owned?.sequence ?? 0;
  let snapshot: DesktopLifecycleSnapshot = owned ?? Object.freeze({ sequence, phase: lifecyclePhase(await application.State()), capabilities: unavailableCapabilities() });
  const listeners = new Set<() => void>();
  const publish = (next: DesktopLifecycleSnapshot): void => {
    sequence = Math.max(sequence + 1, next.sequence);
    snapshot = Object.freeze({ ...next, sequence });
    for (const listener of [...listeners]) listener();
  };
  const failUnavailable = (code: "desktop.selection_failed" | "desktop.lifecycle_failed"): void => {
    publish({ ...snapshot, failure: Object.freeze({ code, message_key: code === "desktop.selection_failed" ? "desktop.error.selection_failed" : "desktop.error.lifecycle_failed", recoverable: true }) });
  };
  const action = async (kind: "select" | "recovery" | "disconnect", viewAddress: string | undefined, signal: AbortSignal): Promise<void> => {
    const project = snapshot.project;
    if (owner === undefined || project === undefined) { failUnavailable(kind === "select" ? "desktop.selection_failed" : "desktop.lifecycle_failed"); return; }
    const base = { project_id: project.project_id, session_generation: project.session_generation, authoritative_revision_token: project.authoritative_revision_token };
    const result = kind === "select"
      ? await owner.SelectProjectView({ ...base, view_address: viewAddress ?? "" }, signal)
      : kind === "recovery"
        ? await owner.ShowProjectRecoveryOptions(base, signal)
        : await owner.DisconnectProjectExternal(base, signal);
    if (result.outcome === "success") publish(result.publication);
    else failUnavailable(kind === "select" ? "desktop.selection_failed" : "desktop.lifecycle_failed");
  };
  const onLifecycle = (event: unknown): void => publish({ ...snapshot, phase: eventPhase(event) });
  runtime.EventsOn(desktopLifecycleEvent, onLifecycle);
  if (owner !== undefined) {
    runtime.EventsOn(desktopProjectEvent, () => {
      void owner.ProjectPublication().then(publish, (error: unknown) => {
        runtime.LogError?.(`Desktop project publication failed: ${error instanceof Error ? `${error.name}: ${error.message}` : "unknown error"}`);
        failUnavailable("desktop.lifecycle_failed");
      });
    });
  }
  return Object.freeze({
    getSnapshot: () => snapshot,
    subscribe(listener: () => void) { listeners.add(listener); return () => listeners.delete(listener); },
    selectView(viewAddress: string, signal: AbortSignal) { return action("select", viewAddress, signal); },
    showRecoveryOptions(signal: AbortSignal) { return action("recovery", undefined, signal); },
    disconnectExternal(signal: AbortSignal) { return action("disconnect", undefined, signal); },
		async refreshPublication() { if (owner !== undefined) publish(await owner.ProjectPublication()); },
  });
}

export async function createDesktopWailsComposition(
  application: DesktopWailsApplicationBinding,
  runtime: DesktopWailsRuntimeBinding,
  mcpBinding: DesktopWailsMCPBinding,
  invoke: DesktopWailsInvoke,
  owners: DesktopWailsOwnerBindings = {},
): Promise<DesktopWailsComposition> {
  const lifecycle = await createDesktopWailsLifecycle(application, runtime, owners.project);
  const mcp: DesktopMCPPort = Object.freeze({
    status: () => mcpBinding.MCPStatus(),
    setEnabled: (enabled: boolean) => mcpBinding.SetMCPEnabled(enabled, "local"),
    clientConfig: (connectionID: string) => mcpBinding.MCPClientConfig(connectionID),
    deleteConnection: (connectionID: string) => mcpBinding.DeleteMCPConnection(connectionID),
    restart: () => mcpBinding.RestartMCP(),
    listConnections: () => mcpBinding.ListMCPConnections(),
    createConnection: (request: DesktopMCPConnectRequest) => mcpBinding.CreateMCPConnection(request),
    revokeConnection: (connectionID: string) => mcpBinding.RevokeMCPConnection(connectionID),
  });
	const refreshAfterOpen = async (result: Awaited<ReturnType<DesktopWailsApplicationBinding["CreateProjectDialog"]>>) => {
		if (result.outcome === "success") {
			await lifecycle.refreshPublication();
			if (lifecycle.getSnapshot().project === undefined) throw new Error("Desktop project publication remained empty after a successful open");
		}
		return result;
	};
  const projectDialogs: DesktopProjectDialogPort = Object.freeze({
    create: async (id: string) => refreshAfterOpen(await application.CreateProjectDialog(id)),
    open: async (id: string) => refreshAfterOpen(await application.OpenProjectDialog(id)),
    close: async () => {
      const result = await application.CloseCurrentProject();
      await lifecycle.refreshPublication();
      return result as never;
    },
    recent: () => application.RecentProjects(),
    openRecent: async (projectID: string) => refreshAfterOpen(await application.OpenRecentProject(projectID)),
  });
  const externalStorage: DesktopExternalStoragePort = Object.freeze({
    connect: (input: JsonObject) => application.ConnectExternal(input), inspect: (id: string) => application.InspectExternal(id), refresh: (id: string) => application.RefreshExternal(id), disconnect: (id: string) => application.DisconnectExternal(id),
    selectRemote: (input: JsonObject) => application.SelectExternalRemote(input), acquireLease: (session: JsonObject, binding: JsonObject) => application.AcquireExternalLease(session, binding),
    planReconcile: (session: JsonObject, input: JsonObject, restricted: boolean) => application.PlanExternalReconcile(session, input, restricted), applyReconcile: (session: JsonObject, plan: JsonObject, resolution: string) => application.ApplyExternalReconcile(session, plan, resolution),
  });
  const registry = owners.registry;
  const library: DesktopLibraryFeature = registry === undefined
    ? Object.freeze({ status: "unavailable", availability: unavailable })
    : Object.freeze({ status: "available", value: createLibrary({ client: createHostRegistryClient({ invoke: async (request: unknown) => JSON.parse(await registry.RegistryDispatch(JSON.stringify(request))) as unknown }), capabilities: { browse: true, manage_sources: true, plan_transactions: true, commit_transactions: true, author_artifacts: true } }) });
  const reviewBinding = owners.review;
  const review: DesktopReviewFeature = reviewBinding === undefined
    ? Object.freeze({ status: "unavailable", availability: unavailable })
    : Object.freeze({ status: "available", value: createReviewModel({
      snapshot: ({ signal }) => signal.aborted ? Promise.reject(new DOMException("Aborted", "AbortError")) : reviewBinding.ReviewSnapshot(),
      comment: (input, { signal }) => signal.aborted ? Promise.reject(new DOMException("Aborted", "AbortError")) : reviewBinding.ReviewComment(input),
      approveAndApply: (input, { signal }) => signal.aborted ? Promise.reject(new DOMException("Aborted", "AbortError")) : reviewBinding.ReviewApproveAndApply(input),
      withdraw: (input, { signal }) => signal.aborted ? Promise.reject(new DOMException("Aborted", "AbortError")) : reviewBinding.ReviewWithdraw(input),
    }) });
  return Object.freeze({ lifecycle, viewer: owners.project?.CreateProjectViewer() ?? createUnavailableViewer(), mcp, generatedBindings: createDesktopGeneratedBindings(invoke), projectDialogs, externalStorage, nativeInterchange: nativeInterchange(application), library, review });
}

const emptyPresentation: ViewerPresentationState = Object.freeze({ selection_keys: [], zoom: 1, pan: Object.freeze({ x: 0, y: 0 }), expanded_keys: [], sorting: [], display_preferences: Object.freeze({}) });

/** Explicit capability-disabled Viewer used until the project owner is installed. */
export function createUnavailableViewer(): Viewer {
  const error = Object.freeze({ code: "viewer.profile_unsupported" as const, recoverable: true, details: Object.freeze({ capability: "engine.materialize_view", reason: "host_disabled" }) });
  let state: ViewerState = Object.freeze({ status: "unsupported_profile", error });
  const reject = async () => ({ ok: false as const, outcome: "rejected" as const, error, state });
  return Object.freeze({
    setViewData: reject,
    applyViewDataUpdate: reject,
    updatePresentation: () => emptyPresentation,
    setSelection: () => emptyPresentation,
    inspectSource: () => undefined,
    getState: () => state,
    getPublication: () => undefined,
    async cancel() {},
    async dispose() { state = Object.freeze({ status: "disposed" }); },
  });
}
