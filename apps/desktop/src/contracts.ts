// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { BrowserDocumentSession, BrowserEditor } from "@layerdraw/client-sdk/editor";
import type { CapabilityID } from "@layerdraw/protocol/common";
import type { ViewDataUpdate, Viewer, ViewerSnapshot } from "@layerdraw/viewer";

export type DesktopLifecyclePhase = "starting" | "ready" | "recovery" | "draining" | "stopped";
export type DesktopFeatureAvailability = Readonly<
  | { status: "available" }
  | { status: "unavailable"; reason: "not_advertised" | "host_disabled" | "adapter_unavailable" | "policy_denied" | "recovery_required" }
>;

/** UI-visible failure values are closed and deliberately cannot carry paths, content, or provider errors. */
export interface DesktopShellFailure {
  readonly code:
    | "desktop.lifecycle_failed"
    | "desktop.selection_failed"
    | "desktop.viewer_rejected"
    | "desktop.viewer_failed"
    | "desktop.context_mismatch";
  readonly message_key: string;
  readonly recoverable: boolean;
}

export interface DesktopViewChoice {
  readonly address: string;
  readonly label: string;
  readonly shape: "context" | "diagram" | "diff" | "flow" | "matrix" | "table" | "tree";
}

export interface DesktopProjectContext {
  readonly project_id: string;
  /** Monotonic host-issued generation; changes on close/reopen even for the same project. */
  readonly session_generation: number;
  readonly display_name: string;
  /** Opaque equality token issued by Runtime. It is not interpreted by the shell. */
  readonly authoritative_revision_token: string;
  readonly authoritative_revision_label: string;
  readonly editor: BrowserEditor;
  readonly editor_session: BrowserDocumentSession;
  readonly views: readonly DesktopViewChoice[];
  readonly selected_view_address?: string;
  readonly access: Readonly<{ status: "allowed" | "limited" | "denied"; label: string }>;
  readonly storage: Readonly<{
    kind: "local" | "external";
    status: "connected" | "syncing" | "conflict" | "reconcile_pending" | "unavailable";
    label: string;
  }>;
  readonly persistence: "clean" | "preview_pending" | "ephemeral" | "durable_pending" | "reconcile_pending";
}

export type DesktopViewerFrame = Readonly<{
  project_id: string;
  session_generation: number;
  view_address: string;
  authoritative_revision_token: string;
}> & (
  | Readonly<{ kind: "snapshot"; input: ViewerSnapshot }>
  | Readonly<{ kind: "update"; input: ViewDataUpdate }>
);

export interface DesktopLifecycleSnapshot {
  readonly sequence: number;
  readonly phase: DesktopLifecyclePhase;
  readonly capabilities: Readonly<Record<CapabilityID, DesktopFeatureAvailability>>;
  readonly project?: DesktopProjectContext;
  readonly viewer_frame?: DesktopViewerFrame;
  readonly failure?: DesktopShellFailure;
}

/**
 * Adapter boundary for #123. Native dialogs, path resolution, storage, leases,
 * recovery, and Runtime revision rules stay behind this port.
 */
export interface DesktopProjectLifecyclePort {
  getSnapshot(): DesktopLifecycleSnapshot;
  subscribe(listener: () => void): () => void;
  selectView(viewAddress: string, signal: AbortSignal): Promise<void>;
  reopen(signal: AbortSignal): Promise<void>;
}

/** Dependencies are constructed by the Wails bootstrap (#122/#143), never discovered globally. */
export interface DesktopShellPorts {
  readonly lifecycle: DesktopProjectLifecyclePort;
  readonly viewer: Viewer;
}
