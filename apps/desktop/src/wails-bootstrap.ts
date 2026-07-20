// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { WailsGeneratedBindings } from "@layerdraw/engine-client/wails";
import type { Viewer, ViewerPresentationState, ViewerState } from "@layerdraw/viewer";
import type { DesktopExternalImportPreview, DesktopLifecyclePhase, DesktopLifecycleSnapshot, DesktopMCPConnectRequest, DesktopMCPConnection, DesktopMCPPort, DesktopMCPResult, DesktopMCPStatus, DesktopNativeExportProfile, DesktopNativeInterchangePort, DesktopNativeSerializeResult, DesktopProjectLifecyclePort } from "./contracts.js";
import { createDesktopGeneratedBindings, type DesktopWailsInvoke } from "./wails-bindings.js";

export const desktopLifecycleEvent = "layerdraw:desktop-lifecycle";

export interface DesktopWailsApplicationBinding {
  State(): Promise<unknown>;
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
}

export interface DesktopWailsRuntimeBinding {
  EventsOn(name: string, listener: (event: unknown) => void): void;
  EventsOff(name: string): void;
}

export interface DesktopWailsComposition {
  readonly lifecycle: DesktopProjectLifecyclePort;
  readonly viewer: Viewer;
  /** Exact Engine + Runtime method closure used by the Wails clients. */
  readonly generatedBindings: WailsGeneratedBindings;
  readonly nativeInterchange: DesktopNativeInterchangePort;
  readonly mcp: DesktopMCPPort;
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
  return value === "starting" || value === "ready" || value === "recovery" || value === "draining" || value === "stopped"
    ? value : "recovery";
}

function eventPhase(value: unknown): DesktopLifecyclePhase {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return "recovery";
  return lifecyclePhase((value as Readonly<Record<string, unknown>>).state);
}

/**
 * Adapts only Wails lifecycle framing. Project/editor/view publications are
 * supplied by their owning adapters and never reconstructed from global state.
 */
export async function createDesktopWailsLifecycle(
  application: DesktopWailsApplicationBinding,
  runtime: DesktopWailsRuntimeBinding,
): Promise<DesktopProjectLifecyclePort> {
  let sequence = 0;
  let snapshot: DesktopLifecycleSnapshot = Object.freeze({ sequence, phase: lifecyclePhase(await application.State()), capabilities: Object.freeze({}) });
  const listeners = new Set<() => void>();
  const onLifecycle = (event: unknown): void => {
    snapshot = Object.freeze({ sequence: ++sequence, phase: eventPhase(event), capabilities: snapshot.capabilities });
    for (const listener of [...listeners]) listener();
  };
  runtime.EventsOn(desktopLifecycleEvent, onLifecycle);
  return Object.freeze({
    getSnapshot: () => snapshot,
    subscribe(listener: () => void) { listeners.add(listener); return () => listeners.delete(listener); },
    async selectView() { throw new Error("desktop project owner is not connected"); },
    async showRecoveryOptions() { throw new Error("desktop recovery owner is not connected"); },
	async disconnectExternal() { throw new Error("desktop external storage owner is not connected"); },
  });
}

/** Constructs the Desktop-owned adapters without discovering Wails globals. */
export async function createDesktopWailsComposition(
	application: DesktopWailsApplicationBinding,
	runtime: DesktopWailsRuntimeBinding,
	mcpBinding: DesktopWailsMCPBinding,
	invoke: DesktopWailsInvoke,
): Promise<DesktopWailsComposition> {
  const [lifecycle, viewer] = await Promise.all([
    createDesktopWailsLifecycle(application, runtime),
    Promise.resolve(createUnopenedViewer()),
	]);
	const mcp: DesktopMCPPort = Object.freeze({
		status: () => mcpBinding.MCPStatus(),
		setEnabled: (enabled: boolean) => mcpBinding.SetMCPEnabled(enabled, "local"),
		restart: () => mcpBinding.RestartMCP(),
		listConnections: () => mcpBinding.ListMCPConnections(),
		createConnection: (request: DesktopMCPConnectRequest) => mcpBinding.CreateMCPConnection(request),
		revokeConnection: (connectionID: string) => mcpBinding.RevokeMCPConnection(connectionID),
	});
	return Object.freeze({
    lifecycle,
    viewer,
    mcp,
    generatedBindings: createDesktopGeneratedBindings(invoke),
    nativeInterchange: nativeInterchange(application),
  });
}

const emptyPresentation: ViewerPresentationState = Object.freeze({
  selection_keys: [], zoom: 1, pan: Object.freeze({ x: 0, y: 0 }), expanded_keys: [], sorting: [], display_preferences: Object.freeze({}),
});

/** A capability-free Viewer boundary used before any project owner publishes. */
export function createUnopenedViewer(): Viewer {
  let state: ViewerState = Object.freeze({ status: "empty", reason: "no_snapshot" });
  return Object.freeze({
    async setViewData() { return { ok: false as const, outcome: "rejected" as const, error: { code: "viewer.input_invalid" as const, recoverable: true, details: {} }, state }; },
    async applyViewDataUpdate() { return { ok: false as const, outcome: "rejected" as const, error: { code: "viewer.input_invalid" as const, recoverable: true, details: {} }, state }; },
    updatePresentation: () => emptyPresentation,
    setSelection: () => emptyPresentation,
    inspectSource: () => undefined,
    getState: () => state,
    getPublication: () => undefined,
    async cancel() {},
    async dispose() { state = Object.freeze({ status: "disposed" }); },
  });
}
