// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { BrowserDocumentSession, BrowserEditor } from "@layerdraw/client-sdk/editor";
import type { CapabilityID } from "@layerdraw/protocol/common";
import type { ExportPlan, ViewData } from "@layerdraw/protocol/semantic";
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
	provider_label?: string;
	account_label?: string;
	scope_label?: string;
	last_sync_label?: string;
	pending_changes?: number;
	disconnect_consequence?: string;
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
  /** Opens the host-owned explicit restore/discard workflow; it never restores implicitly. */
  showRecoveryOptions(signal: AbortSignal): Promise<void>;
	/** Disconnects through the host; the shell never handles credentials. */
	disconnectExternal(signal: AbortSignal): Promise<void>;
}

export interface DesktopNativeExportProfile {
  readonly format: "json" | "csv" | "tsv";
  readonly schema_version: 1;
  readonly requires_shape: readonly string[];
}

export interface DesktopNativeArtifactRef {
  readonly artifact_id: string;
  readonly logical_path: string;
  readonly media_type: string;
  readonly content_digest: string;
}

export interface DesktopNativeSerializeResult {
  readonly artifact: DesktopNativeArtifactRef;
  readonly source_manifest: Readonly<Record<string, unknown>>;
}

export interface DesktopExternalImportPreview {
  readonly profile: string;
  readonly media_type: string;
  /** Generated Engine SemanticOperationBatch; submit to preview_operations before any write. */
  readonly batch: Readonly<Record<string, unknown>>;
  readonly source_hash: string;
}

export interface DesktopNativeInterchangePort {
  profiles(): Promise<readonly DesktopNativeExportProfile[]>;
  serialize(input: Readonly<{ export_plan: ExportPlan; view_data: ViewData }>, signal: AbortSignal): Promise<DesktopNativeSerializeResult>;
  publish(input: Readonly<{ request_id: string; artifact_id: string; extension: string }>, signal: AbortSignal): Promise<void>;
  importOperations(input: Readonly<{ request_id: string; profile: string; extension: string }>, signal: AbortSignal): Promise<DesktopExternalImportPreview>;
}

/** Dependencies are constructed by the Wails bootstrap (#122/#143), never discovered globally. */
export interface DesktopShellPorts {
	readonly lifecycle: DesktopProjectLifecyclePort;
	readonly viewer: Viewer;
}

export interface DesktopMCPFailure {
	readonly code: "desktop.mcp_transport_failed" | "desktop.mcp_disabled" | "desktop.mcp_version_mismatch" | "desktop.mcp_scope_denied" | "desktop.mcp_disconnected" | "desktop.mcp_host_restarted" | "desktop.agent_delegation_failed";
	readonly retryable: boolean;
	readonly recovery: "retry" | "reconnect" | "configure_adapter" | "upgrade" | "review";
}

export interface DesktopMCPStatus {
	readonly enabled: boolean;
	readonly transport: "local";
	readonly instructions: string;
	readonly generation: number;
}

export interface DesktopMCPPermissions { readonly read: boolean; readonly export: boolean; readonly propose: boolean; readonly apply: boolean }

export interface DesktopMCPConnection {
	readonly connection_id: string;
	readonly client_id: string;
	readonly session_id: string;
	readonly protocol_version: "desktop-mcp-v1";
	readonly document_id: string;
	readonly delegation_id: string;
	readonly agent_id: string;
	readonly capabilities: readonly string[];
	readonly permissions: DesktopMCPPermissions;
	readonly expires_at: string;
	readonly generation: string;
	readonly status: "connected" | "revoking" | "revoked" | "expired" | "host_restarted";
}

export interface DesktopMCPConnectRequest {
	readonly client_id: string;
	readonly protocol_version: "desktop-mcp-v1";
	readonly document_id: string;
	readonly agent_id: string;
	readonly capabilities: readonly string[];
	readonly permissions: DesktopMCPPermissions;
	readonly expires_at: string;
	readonly confirm_apply: boolean;
}

export type DesktopMCPResult<T> = Readonly<{ outcome: "success"; value: T }> | Readonly<{ outcome: "failed" | "rejected" | "cancelled"; failure?: DesktopMCPFailure }>;

export interface DesktopMCPPort {
	status(): Promise<DesktopMCPStatus>;
	setEnabled(enabled: boolean): Promise<DesktopMCPResult<DesktopMCPStatus>>;
	restart(): Promise<DesktopMCPResult<DesktopMCPStatus>>;
	listConnections(): Promise<readonly DesktopMCPConnection[]>;
	createConnection(request: DesktopMCPConnectRequest): Promise<DesktopMCPResult<DesktopMCPConnection>>;
	revokeConnection(connectionID: string): Promise<DesktopMCPResult<DesktopMCPConnection>>;
}
