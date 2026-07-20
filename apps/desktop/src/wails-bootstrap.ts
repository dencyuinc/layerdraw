// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { WailsGeneratedBindings } from "@layerdraw/engine-client/wails";
import type { Viewer, ViewerPresentationState, ViewerState } from "@layerdraw/viewer";
import type { DesktopLifecyclePhase, DesktopLifecycleSnapshot, DesktopProjectLifecyclePort } from "./contracts.js";
import { createDesktopGeneratedBindings, type DesktopWailsInvoke } from "./wails-bindings.js";

export const desktopLifecycleEvent = "layerdraw:desktop-lifecycle";

export interface DesktopWailsApplicationBinding {
  State(): Promise<unknown>;
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
  });
}

/** Constructs the Desktop-owned adapters without discovering Wails globals. */
export async function createDesktopWailsComposition(
  application: DesktopWailsApplicationBinding,
  runtime: DesktopWailsRuntimeBinding,
  invoke: DesktopWailsInvoke,
): Promise<DesktopWailsComposition> {
  const [lifecycle, viewer] = await Promise.all([
    createDesktopWailsLifecycle(application, runtime),
    Promise.resolve(createUnopenedViewer()),
  ]);
  return Object.freeze({
    lifecycle,
    viewer,
    generatedBindings: createDesktopGeneratedBindings(invoke),
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
