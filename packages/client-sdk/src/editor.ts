// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { ComposerIntent, ComposerSnapshot, EditorEdit } from "@layerdraw/composer";
import type { CompileRequestBlob, EngineClient, OutputBlob } from "@layerdraw/engine-client";
import type { AuthoringDecision, AuthoringGrantSummary } from "@layerdraw/protocol/access";
import type {
  CapabilityID,
  CapabilityManifest,
  OperationCapability,
  ProtocolDiagnostic,
  ProtocolFailure,
} from "@layerdraw/protocol/common";
import type {
  ApplyToHandleResult,
  MaterializeViewInput,
  OpenDocumentInput,
  OpenDocumentResult,
  SemanticConflict,
  WorkbenchPreviewResult,
} from "@layerdraw/protocol/engine";
import type {
  CommittedRevisionRef,
  ConflictEvidence,
  OpenRuntimeDocumentInput,
  OpenRuntimeDocumentResult,
  OperationResult,
  AuthoringProof,
  RuntimeCommitInput,
  RuntimeCapabilityManifest,
  RuntimeCommitResult,
  RuntimeOperationBatch,
  RuntimeSessionRef,
} from "@layerdraw/protocol/runtime";
import type { Diagnostic, ViewData } from "@layerdraw/protocol/semantic";

export type BrowserDocumentInput =
  | Readonly<{ authority: "engine"; input: OpenDocumentInput; blobs?: readonly CompileRequestBlob[] }>
  | Readonly<{ authority: "runtime"; input: OpenRuntimeDocumentInput }>;

export type BrowserDocumentSession =
  | Readonly<{ authority: "engine"; persistence: "ephemeral"; session: OpenDocumentResult; capabilities: BrowserEditorCapabilityState }>
  | Readonly<{ authority: "runtime"; persistence: "durable"; session: OpenRuntimeDocumentResult; capabilities: BrowserEditorCapabilityState }>;

export interface BrowserEditorCapabilityState {
  readonly authority: "engine" | "runtime";
  readonly manifest: CapabilityManifest | RuntimeCapabilityManifest;
  readonly selection: EditorCapabilitySelection;
}

export interface EditorPreviewResult {
  readonly authority: "engine" | "trusted_access" | "runtime";
  readonly preview: WorkbenchPreviewResult;
  readonly authoring_decision?: AuthoringDecision;
  readonly grant_summary?: AuthoringGrantSummary;
  readonly conflicts: readonly SemanticConflict[] | readonly ConflictEvidence[];
  readonly diagnostics: readonly Diagnostic[] | readonly ProtocolDiagnostic[];
}

export type RuntimeCommittedOperationResult = OperationResult &
  Readonly<{
    status: "committed" | "committed_external_failed" | "committed_external_pending" | "committed_state_stale";
    committed_revision: CommittedRevisionRef;
  }>;

export type RuntimeNotCommittedOperationResult = OperationResult &
  Readonly<{
    status: "needs_review" | "rejected";
    committed_revision?: never;
  }>;

export type RuntimeCommittedEditorResult = Omit<RuntimeCommitResult, "operation_result"> &
  Readonly<{ operation_result: RuntimeCommittedOperationResult }>;

export type RuntimeNotCommittedEditorResult = Omit<RuntimeCommitResult, "operation_result"> &
  Readonly<{ operation_result: RuntimeNotCommittedOperationResult }>;

export type EditorApplyResult =
  | Readonly<{
      persistence: "ephemeral";
      applied: ApplyToHandleResult;
      committed_revision?: never;
    }>
  | Readonly<{
      persistence: "host_callback";
      receipt: HostWriteReceipt;
      committed_revision?: never;
    }>
  | Readonly<{
      persistence: "durable";
      result: RuntimeCommittedEditorResult;
      committed_revision: CommittedRevisionRef;
    }>
  | Readonly<{
      persistence: "runtime_not_committed";
      result: RuntimeNotCommittedEditorResult;
      committed_revision?: never;
    }>;

export interface HostWriteReceipt extends Readonly<Record<string, unknown>> {
  readonly receipt_id: string;
  readonly persistence_claim: "host_defined";
}

export interface BrowserEditor {
  open(input: BrowserDocumentInput): Promise<BrowserDocumentSession>;
  preview(edit: EditorEdit, options?: Readonly<{ intent_id?: string; inverse?: EditorEdit }>): Promise<EditorPreviewResult>;
  apply(edit: EditorEdit): Promise<EditorApplyResult>;
  undo(): Promise<ComposerSnapshot>;
  redo(): Promise<ComposerSnapshot>;
  retry(): Promise<ComposerSnapshot>;
  cancelPreview(): ComposerSnapshot;
  snapshot(): ComposerSnapshot;
  subscribe(listener: (snapshot: ComposerSnapshot) => void): () => void;
  getCapabilities(): BrowserEditorCapabilityState | undefined;
  materializeView(input: MaterializeViewInput, options?: Readonly<{ signal?: AbortSignal }>): Promise<ViewData>;
  close(): Promise<void>;
}

export interface DocumentProvider {
  open(input: BrowserDocumentInput, signal: AbortSignal): Promise<unknown>;
  read(signal: AbortSignal): Promise<unknown>;
  writeWithPrecondition(input: HostWriteInput, signal: AbortSignal): Promise<HostWriteReceipt>;
  close(): Promise<void>;
}

export interface HostWriteInput {
  readonly edit: EditorEdit;
  readonly preview: EditorPreviewResult;
  readonly applied: ApplyToHandleResult;
  /** Exact Engine response attachments referenced by applied.source_diff. */
  readonly blobs: readonly OutputBlob[];
}

export interface AssetResolver {
  resolve(logicalRef: string, signal: AbortSignal): Promise<Uint8Array>;
  put(bytes: Uint8Array, signal: AbortSignal): Promise<string>;
  describeCapability(): Readonly<Record<string, unknown>>;
  dispose?(): Promise<void>;
}

export interface AuthoringAccessClient {
  getEffectiveGrant(signal: AbortSignal): Promise<AuthoringGrantSummary>;
  evaluatePreview(preview: WorkbenchPreviewResult, signal: AbortSignal): Promise<AuthoringDecision>;
}

export interface ApprovalHandler {
  requestApproval(preview: EditorPreviewResult, signal: AbortSignal): Promise<"approved" | "cancelled" | "denied">;
  reportResult(result: EditorApplyResult, signal: AbortSignal): Promise<void>;
}

export interface BrowserRuntimePreviewResult {
  readonly preview: WorkbenchPreviewResult;
  readonly authoring_proof: AuthoringProof;
  readonly operation_batch: RuntimeOperationBatch;
  readonly authoring_decision: AuthoringDecision;
  readonly grant_summary: AuthoringGrantSummary;
}

export interface BrowserRuntimeClient {
  getCapabilities(): RuntimeCapabilityManifest;
  openDocument(input: OpenRuntimeDocumentInput, options: Readonly<{ signal: AbortSignal }>): Promise<OpenRuntimeDocumentResult>;
  previewEditor(edit: EditorEdit, session: OpenRuntimeDocumentResult, options: Readonly<{ signal: AbortSignal }>): Promise<BrowserRuntimePreviewResult>;
  commitOperations(input: RuntimeCommitInput, options: Readonly<{ signal: AbortSignal }>): Promise<RuntimeCommitResult>;
  materializeView(input: MaterializeViewInput, session: RuntimeSessionRef, options: Readonly<{ signal: AbortSignal }>): Promise<ViewData>;
  closeDocument(session: RuntimeSessionRef, options: Readonly<{ signal: AbortSignal }>): Promise<void>;
  dispose?(): Promise<void>;
}

export type RuntimeCommitMetadata = Omit<RuntimeCommitInput, "session" | "operation_batch" | "authoring_proof">;

export type RuntimeCommitInputFactory = (context: Readonly<{
  edit: EditorEdit;
  session: OpenRuntimeDocumentResult;
  authoring_proof: AuthoringProof;
  operation_batch: RuntimeOperationBatch;
}>) => RuntimeCommitMetadata;

export interface BrowserEditorOptions {
  readonly engine_client: EngineClient;
  readonly runtime_client?: BrowserRuntimeClient;
  readonly authoring_access_client?: AuthoringAccessClient;
  readonly document_provider?: DocumentProvider;
  readonly asset_resolver: AssetResolver;
  /** Additional host-declared capabilities. Transport capabilities are always
   * read from the selected Engine or Runtime and cannot be fabricated here. */
  readonly capability_manifest?: CapabilityManifest | RuntimeCapabilityManifest;
  readonly required_capabilities?: readonly CapabilityID[];
  readonly optional_capabilities?: readonly CapabilityID[];
  readonly approval_handler?: ApprovalHandler;
  readonly runtime_commit_input_factory?: RuntimeCommitInputFactory;
}

export type BrowserEditorFactory = (options: BrowserEditorOptions) => BrowserEditor;

export type BrowserEditorFailureCode =
  | "editor.invalid_state"
  | "editor.capability_unavailable"
  | "editor.access_denied"
  | "editor.approval_cancelled"
  | "editor.stale_revision"
  | "editor.conflict"
  | "editor.transport_failed"
  | "editor.cancelled";

export class BrowserEditorError extends Error {
  constructor(
    readonly code: BrowserEditorFailureCode,
    message: string,
    readonly failure?: ProtocolFailure,
    readonly diagnostics: readonly Diagnostic[] | readonly ProtocolDiagnostic[] = [],
  ) {
    super(message);
    this.name = "BrowserEditorError";
  }
}

export interface CapabilityUnavailable {
  readonly status: "unavailable";
  readonly capability_id: CapabilityID;
  readonly reason: string;
}

export interface EditorCapabilitySelection {
  readonly available: readonly CapabilityID[];
  readonly optional_unavailable: readonly CapabilityUnavailable[];
}

export class RequiredEditorCapabilityError extends Error {
  readonly code = "editor.required_capability_unavailable";
  readonly unavailable: readonly CapabilityUnavailable[];

  constructor(unavailable: readonly CapabilityUnavailable[]) {
    super("One or more required Browser Editor capabilities are unavailable.");
    this.name = "RequiredEditorCapabilityError";
    this.unavailable = unavailable;
  }
}

type OperationManifest = Readonly<{ operations: Readonly<Record<string, OperationCapability>> }>;

function inspectCapability(manifest: OperationManifest, capabilityId: CapabilityID): CapabilityUnavailable | undefined {
  const capability = manifest.operations[capabilityId];
  if (capability?.enabled === true) return undefined;
  const reason = capability?.unavailable_reason ?? "not_advertised";
  return { status: "unavailable", capability_id: capabilityId, reason };
}

export function negotiateEditorCapabilities(
  manifest: CapabilityManifest | RuntimeCapabilityManifest,
  request: Readonly<{
    required?: readonly CapabilityID[];
    optional?: readonly CapabilityID[];
  }>,
): EditorCapabilitySelection {
  const required = request.required ?? [];
  const optional = request.optional ?? [];
  const requiredUnavailable = required
    .map((id) => inspectCapability(manifest, id))
    .filter((value): value is CapabilityUnavailable => value !== undefined);
  if (requiredUnavailable.length > 0) throw new RequiredEditorCapabilityError(requiredUnavailable);
  return {
    available: [...required, ...optional.filter((id) => inspectCapability(manifest, id) === undefined)],
    optional_unavailable: optional
      .map((id) => inspectCapability(manifest, id))
      .filter((value): value is CapabilityUnavailable => value !== undefined),
  };
}

export type EditorContractFailure = Readonly<{
  outcome: "failed" | "cancelled" | "rejected";
  failure?: ProtocolFailure;
  diagnostics: readonly ProtocolDiagnostic[];
}>;
