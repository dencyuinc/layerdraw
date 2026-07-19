// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { AuthoringDecision, AuthoringGrantSummary } from "@layerdraw/protocol/access";
import type { ApplyToHandleResult, SemanticConflict, WorkbenchPreviewResult } from "@layerdraw/protocol/engine";
import type { CommittedRevisionRef, ConflictEvidence, OperationResult, RuntimeCommitResult, RuntimeFailureCode } from "@layerdraw/protocol/runtime";
import type { Diagnostic } from "@layerdraw/protocol/semantic";
import type { ComposerPresentationState, EditorEdit } from "./contracts.js";

export type ComposerFailureCode =
  | "composer.access_denied"
  | "composer.cancelled"
  | "composer.capability_unavailable"
  | "composer.conflict"
  | "composer.invalid_state"
  | "composer.session_closed"
  | "composer.stale_revision"
  | "composer.transport_failed"
  | "composer.validation_failed";

export interface ComposerFailure {
  readonly code: ComposerFailureCode;
  readonly message: string;
  readonly recoverable: boolean;
  readonly diagnostics: readonly Diagnostic[];
  readonly conflicts: readonly (SemanticConflict | ConflictEvidence)[];
}

export class ComposerError extends Error {
  constructor(readonly failure: ComposerFailure) {
    super(failure.message);
    this.name = "ComposerError";
  }
}

export type RuntimeCommittedOperationResult = OperationResult & Readonly<{
  status: "committed" | "committed_external_failed" | "committed_external_pending" | "committed_state_stale";
  committed_revision: CommittedRevisionRef;
}>;
export type RuntimeNotCommittedOperationResult = OperationResult & Readonly<{
  status: "needs_review" | "rejected";
  committed_revision?: never;
}>;
export type RuntimeCommittedComposerResult = Omit<RuntimeCommitResult, "operation_result"> &
  Readonly<{ operation_result: RuntimeCommittedOperationResult }>;
export type RuntimeNotCommittedComposerResult = Omit<RuntimeCommitResult, "operation_result"> &
  Readonly<{ operation_result: RuntimeNotCommittedOperationResult }>;

export type ComposerApplyResult =
  | Readonly<{ persistence: "ephemeral"; applied: ApplyToHandleResult; committed_revision?: never }>
  | Readonly<{ persistence: "host_callback"; receipt: Readonly<Record<string, unknown>>; committed_revision?: never }>
  | Readonly<{ persistence: "durable"; result: RuntimeCommittedComposerResult; committed_revision: CommittedRevisionRef }>
  | Readonly<{ persistence: "runtime_not_committed"; result: RuntimeNotCommittedComposerResult; committed_revision?: never }>;

export interface ComposerIntent {
  readonly id: string;
  readonly edit: EditorEdit;
  readonly inverse?: EditorEdit;
}

export interface ComposerHost {
  preview(edit: EditorEdit, signal: AbortSignal): Promise<Readonly<{
    preview: WorkbenchPreviewResult;
    authoring_decision?: AuthoringDecision;
    grant_summary?: AuthoringGrantSummary;
  }>>;
  apply(edit: EditorEdit, preview: ComposerPresentationState, signal: AbortSignal): Promise<ComposerApplyResult>;
  close?(): Promise<void>;
}

export type ComposerPhase = "idle" | "previewing" | "ready" | "applying" | "applied" | "failed" | "closed";

export interface ComposerSnapshot {
  readonly phase: ComposerPhase;
  readonly sequence: number;
  readonly intent?: ComposerIntent;
  readonly presentation?: ComposerPresentationState;
  readonly apply_result?: ComposerApplyResult;
  readonly failure?: ComposerFailure;
  readonly can_undo: boolean;
  readonly can_redo: boolean;
}

type Listener = (snapshot: ComposerSnapshot) => void;
type HistoryEntry = Readonly<{ forward: ComposerIntent; inverse: EditorEdit }>;

const emptyDiagnostics: readonly Diagnostic[] = [];
const emptyConflicts: readonly SemanticConflict[] = [];

export class Composer {
  readonly #host: ComposerHost;
  readonly #listeners = new Set<Listener>();
  readonly #past: HistoryEntry[] = [];
  readonly #future: HistoryEntry[] = [];
  #controller: AbortController | undefined;
  #sequence = 0;
  #snapshot: ComposerSnapshot;

  constructor(host: ComposerHost) {
    this.#host = host;
    this.#snapshot = this.#makeSnapshot({ phase: "idle", sequence: 0 });
  }

  snapshot(): ComposerSnapshot { return this.#snapshot; }

  subscribe(listener: Listener): () => void {
    this.#listeners.add(listener);
    listener(this.#snapshot);
    return () => this.#listeners.delete(listener);
  }

  async preview(intent: ComposerIntent): Promise<ComposerSnapshot> {
    if (this.#snapshot.phase === "closed") return this.#transientFailure("composer.session_closed", "The Composer session is closed.", false, intent);
    if (this.#snapshot.phase === "applying") return this.#transientFailure("composer.invalid_state", "An apply is already in progress.", true, intent);
    if (!intent.id) return this.#fail("composer.validation_failed", "Intent id is required.", true, intent);
    this.#controller?.abort();
    const sequence = ++this.#sequence;
    const controller = new AbortController();
    this.#controller = controller;
    this.#publish({ phase: "previewing", sequence, intent });
    try {
      const result = await this.#host.preview(intent.edit, controller.signal);
      if (sequence !== this.#sequence || controller.signal.aborted) return this.#snapshot;
      const presentation: ComposerPresentationState = {
        preview: result.preview,
        ...(result.authoring_decision === undefined ? {} : { authoring_decision: result.authoring_decision }),
        ...(result.grant_summary === undefined ? {} : { grant_summary: result.grant_summary }),
      };
      if (result.authoring_decision?.outcome === "deny") {
        return this.#fail("composer.access_denied", "Access denied the authoring intent.", true, intent, presentation);
      }
      if (result.preview.status === "invalid") {
        const stale = result.preview.conflicts.some((conflict) => conflict.kind === "stale_revision");
        return this.#fail(stale ? "composer.stale_revision" : "composer.validation_failed", stale ? "The preview revision is stale." : "The preview is invalid.", true, intent, presentation);
      }
      this.#publish({ phase: "ready", sequence, intent, presentation });
      return this.#snapshot;
    } catch (error) {
      if (sequence !== this.#sequence) return this.#snapshot;
      return this.#failFrom(error, intent);
    } finally {
      if (this.#controller === controller) this.#controller = undefined;
    }
  }

  cancelPreview(): ComposerSnapshot {
    if (this.#snapshot.phase === "closed") return this.#transientFailure("composer.session_closed", "The Composer session is closed.", false, this.#snapshot.intent);
    if (this.#snapshot.phase === "applying") return this.#transientFailure("composer.invalid_state", "An apply is already in progress.", true, this.#snapshot.intent);
    if (this.#snapshot.phase !== "previewing") return this.#fail("composer.invalid_state", "No preview is in progress.", true, this.#snapshot.intent);
    this.#controller?.abort();
    this.#controller = undefined;
    ++this.#sequence;
    return this.#fail("composer.cancelled", "The preview was cancelled.", true, this.#snapshot.intent);
  }

  async retry(): Promise<ComposerSnapshot> {
    const intent = this.#snapshot.intent;
    if (this.#snapshot.phase === "closed") return this.#transientFailure("composer.session_closed", "The Composer session is closed.", false, intent);
    if (this.#snapshot.phase === "applying") return this.#transientFailure("composer.invalid_state", "An apply is already in progress.", true, intent);
    if (this.#snapshot.phase !== "failed" || intent === undefined) return this.#fail("composer.invalid_state", "There is no failed intent to retry.", true, intent);
    return this.preview(intent);
  }

  async apply(): Promise<ComposerSnapshot> { return this.#apply(true); }

  async undo(): Promise<ComposerSnapshot> {
    if (this.#snapshot.phase === "closed") return this.#transientFailure("composer.session_closed", "The Composer session is closed.", false, this.#snapshot.intent);
    if (this.#snapshot.phase === "applying") return this.#transientFailure("composer.invalid_state", "An apply is already in progress.", true, this.#snapshot.intent);
    const entry = this.#past.at(-1);
    if (entry === undefined) return this.#fail("composer.invalid_state", "There is no semantic intent to undo.", true, this.#snapshot.intent);
    const intent: ComposerIntent = { id: `${entry.forward.id}:undo`, edit: entry.inverse };
    const previewed = await this.preview(intent);
    if (previewed.phase !== "ready" || previewed.intent !== intent) return this.#transientFailure("composer.invalid_state", "The undo intent was superseded by a newer preview.", true, intent);
    const applied = await this.#apply(false);
    if (applied.phase === "applied") {
      this.#past.pop();
      this.#future.push(entry);
      this.#refreshFlags();
    }
    return this.#snapshot;
  }

  async redo(): Promise<ComposerSnapshot> {
    if (this.#snapshot.phase === "closed") return this.#transientFailure("composer.session_closed", "The Composer session is closed.", false, this.#snapshot.intent);
    if (this.#snapshot.phase === "applying") return this.#transientFailure("composer.invalid_state", "An apply is already in progress.", true, this.#snapshot.intent);
    const entry = this.#future.at(-1);
    if (entry === undefined) return this.#fail("composer.invalid_state", "There is no semantic intent to redo.", true, this.#snapshot.intent);
    const previewed = await this.preview(entry.forward);
    if (previewed.phase !== "ready" || previewed.intent !== entry.forward) return this.#transientFailure("composer.invalid_state", "The redo intent was superseded by a newer preview.", true, entry.forward);
    const applied = await this.#apply(false);
    if (applied.phase === "applied") {
      this.#future.pop();
      this.#past.push(entry);
      this.#refreshFlags();
    }
    return this.#snapshot;
  }

  async close(): Promise<void> {
    if (this.#snapshot.phase === "closed") return;
    this.#controller?.abort();
    this.#controller = undefined;
    ++this.#sequence;
    this.#publish({ phase: "closed", sequence: this.#sequence });
    this.#listeners.clear();
    let closeError: unknown;
    try {
      await this.#host.close?.();
    } catch (error) {
      closeError = error;
    }
    if (closeError !== undefined) {
      throw new ComposerError({
        code: "composer.transport_failed",
        message: "The Composer host failed while closing the session.",
        recoverable: false,
        diagnostics: emptyDiagnostics,
        conflicts: emptyConflicts,
      });
    }
  }

  async #apply(recordHistory: boolean): Promise<ComposerSnapshot> {
    const { intent, presentation } = this.#snapshot;
    if (this.#snapshot.phase === "closed") return this.#transientFailure("composer.session_closed", "The Composer session is closed.", false, intent);
    if (this.#snapshot.phase === "applying") return this.#transientFailure("composer.invalid_state", "An apply is already in progress.", true, intent);
    if (this.#snapshot.phase !== "ready" || intent === undefined || presentation === undefined) return this.#fail("composer.invalid_state", "A valid preview is required before apply.", true, intent, presentation);
    const sequence = ++this.#sequence;
    const controller = new AbortController();
    this.#controller = controller;
    this.#publish({ phase: "applying", sequence, intent, presentation });
    try {
      const result = await this.#host.apply(intent.edit, presentation, controller.signal);
      if (sequence !== this.#sequence || controller.signal.aborted) return this.#snapshot;
      if (result.persistence === "runtime_not_committed") return this.#failRuntimeResult(result, intent, presentation);
      if (result.persistence === "durable" && !sameRevision(result.committed_revision, result.result.operation_result.committed_revision)) {
        return this.#fail("composer.validation_failed", "Runtime returned contradictory committed revisions.", false, intent, presentation, result);
      }
      this.#publish({ phase: "applied", sequence, intent, presentation, apply_result: result });
      if (recordHistory && intent.inverse !== undefined && intent.edit.kind === "semantic_operations" && intent.inverse.kind === "semantic_operations") {
        this.#past.push({ forward: intent, inverse: intent.inverse });
        this.#future.splice(0);
        this.#refreshFlags();
      }
      return this.#snapshot;
    } catch (error) {
      if (sequence !== this.#sequence) return this.#snapshot;
      return this.#failFrom(error, intent, presentation);
    } finally {
      if (this.#controller === controller) this.#controller = undefined;
    }
  }

  #failFrom(error: unknown, intent?: ComposerIntent, presentation?: ComposerPresentationState): ComposerSnapshot {
    if (error instanceof ComposerError) {
      this.#publish({
        phase: "failed",
        sequence: this.#sequence,
        ...(intent === undefined ? {} : { intent }),
        ...(presentation === undefined ? {} : { presentation }),
        failure: error.failure,
      });
      return this.#snapshot;
    }
    const aborted = typeof error === "object" && error !== null && "name" in error && error.name === "AbortError";
    return this.#fail(aborted ? "composer.cancelled" : "composer.transport_failed", aborted ? "The operation was cancelled." : "The Composer host operation failed.", true, intent, presentation);
  }

  #fail(code: ComposerFailureCode, message: string, recoverable: boolean, intent?: ComposerIntent, presentation?: ComposerPresentationState, applyResult?: ComposerApplyResult): ComposerSnapshot {
    const failure: ComposerFailure = {
      code,
      message,
      recoverable,
      diagnostics: presentation?.preview.diagnostics ?? emptyDiagnostics,
      conflicts: presentation?.preview.conflicts ?? emptyConflicts,
    };
    this.#publish({
      phase: "failed",
      sequence: this.#sequence,
      ...(intent === undefined ? {} : { intent }),
      ...(presentation === undefined ? {} : { presentation }),
      ...(applyResult === undefined ? {} : { apply_result: applyResult }),
      failure,
    });
    return this.#snapshot;
  }

  #transientFailure(code: ComposerFailureCode, message: string, recoverable: boolean, intent?: ComposerIntent): ComposerSnapshot {
    const { can_undo: _undo, can_redo: _redo, failure: _failure, ...state } = this.#snapshot;
    this.#publish({
      ...state,
      ...(state.intent === undefined && intent !== undefined ? { intent } : {}),
      failure: {
        code,
        message,
        recoverable,
        diagnostics: emptyDiagnostics,
        conflicts: emptyConflicts,
      },
    });
    return this.#snapshot;
  }

  #failRuntimeResult(result: Extract<ComposerApplyResult, { persistence: "runtime_not_committed" }>, intent: ComposerIntent, presentation: ComposerPresentationState): ComposerSnapshot {
    const operation = result.result.operation_result;
    const code = runtimeFailureCode(operation.failure_code);
    const failure: ComposerFailure = {
      code,
      message: "Runtime did not commit the intent.",
      recoverable: code !== "composer.validation_failed",
      diagnostics: [...presentation.preview.diagnostics, ...operation.diagnostics],
      conflicts: [
        ...presentation.preview.conflicts,
        ...(operation.conflict_evidence === undefined ? [] : [operation.conflict_evidence]),
      ],
    };
    this.#publish({ phase: "failed", sequence: this.#sequence, intent, presentation, apply_result: result, failure });
    return this.#snapshot;
  }

  #refreshFlags(): void { this.#publish(this.#snapshot); }

  #makeSnapshot(value: Omit<ComposerSnapshot, "can_undo" | "can_redo">): ComposerSnapshot {
    return Object.freeze({ ...value, can_undo: this.#past.length > 0, can_redo: this.#future.length > 0 });
  }

  #publish(value: Omit<ComposerSnapshot, "can_undo" | "can_redo"> | ComposerSnapshot): void {
    const { can_undo: _undo, can_redo: _redo, ...state } = value as ComposerSnapshot;
    this.#snapshot = this.#makeSnapshot(state);
    for (const listener of this.#listeners) listener(this.#snapshot);
  }
}

function runtimeFailureCode(code: RuntimeFailureCode | undefined): ComposerFailureCode {
  if (code === "runtime.cancelled") return "composer.cancelled";
  if (code === "runtime.capability_unavailable") return "composer.capability_unavailable";
  if (code === "runtime.authorization_proof_invalid" || code === "runtime.authorization_stale") return "composer.access_denied";
  if (code === "runtime.stale_revision" || code === "runtime.stale_session_generation" || code === "runtime.session_expired") return "composer.stale_revision";
  if (code === undefined) return "composer.conflict";
  return "composer.validation_failed";
}

function sameRevision(left: CommittedRevisionRef, right: CommittedRevisionRef): boolean {
  return left.definition_hash === right.definition_hash &&
    left.document_id === right.document_id &&
    left.graph_hash === right.graph_hash &&
    left.provider_version === right.provider_version &&
    left.revision_id === right.revision_id;
}
