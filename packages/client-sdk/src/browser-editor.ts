// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import { Composer, ComposerError, type ComposerFailureCode, type ComposerSnapshot, type EditorEdit } from "@layerdraw/composer";
import type { WorkbenchOutcome } from "@layerdraw/engine-client";
import type { AuthoringDecision, AuthoringGrantSummary } from "@layerdraw/protocol/access";
import type {
  ApplyToHandleResult,
  MaterializeViewInput,
  OpenDocumentResult,
  WorkbenchPreviewResult,
} from "@layerdraw/protocol/engine";
import type { OpenRuntimeDocumentResult, RuntimeCommitResult } from "@layerdraw/protocol/runtime";
import type { ViewData } from "@layerdraw/protocol/semantic";
import {
  BrowserEditorError,
  negotiateEditorCapabilities,
  type BrowserDocumentInput,
  type BrowserDocumentSession,
  type BrowserEditor,
  type BrowserEditorOptions,
  type BrowserRuntimePreviewResult,
  type EditorApplyResult,
  type EditorPreviewResult,
  type RuntimeCommittedEditorResult,
  type RuntimeNotCommittedEditorResult,
} from "./editor.js";

type ActiveSession =
  | Readonly<{ authority: "engine"; session: OpenDocumentResult }>
  | Readonly<{ authority: "runtime"; session: OpenRuntimeDocumentResult }>;

function aborted(error: unknown): boolean {
  return typeof error === "object" && error !== null && "name" in error && error.name === "AbortError";
}

function transportError(error: unknown): BrowserEditorError {
  if (error instanceof BrowserEditorError) return error;
  return new BrowserEditorError(
    aborted(error) ? "editor.cancelled" : "editor.transport_failed",
    aborted(error) ? "The Browser Editor operation was cancelled." : "The Browser Editor host operation failed.",
  );
}

function unwrapEngine<T>(outcome: WorkbenchOutcome<any>, operation: string): T {
  if (outcome.origin === "client") throw new BrowserEditorError("editor.cancelled", `${operation} was cancelled.`);
  const response = outcome.response;
  if (response.outcome !== "success" || response.payload === undefined) {
    const stale = response.payload?.conflicts?.some?.((conflict: { kind?: string }) => conflict.kind === "stale_revision") === true;
    throw new BrowserEditorError(
      stale ? "editor.stale_revision" : response.outcome === "cancelled" ? "editor.cancelled" : response.outcome === "rejected" ? "editor.conflict" : "editor.transport_failed",
      `${operation} did not succeed.`,
      "failure" in response ? response.failure : undefined,
      response.diagnostics,
    );
  }
  return response.payload as T;
}

function editorCode(code: ComposerFailureCode): BrowserEditorError["code"] {
  if (code === "composer.capability_unavailable") return "editor.capability_unavailable";
  if (code === "composer.access_denied") return "editor.access_denied";
  if (code === "composer.stale_revision") return "editor.stale_revision";
  if (code === "composer.conflict" || code === "composer.validation_failed") return "editor.conflict";
  if (code === "composer.cancelled") return "editor.cancelled";
  if (code === "composer.transport_failed") return "editor.transport_failed";
  return "editor.invalid_state";
}

function snapshotError(snapshot: ComposerSnapshot): BrowserEditorError {
  const failure = snapshot.failure;
  if (failure === undefined) return new BrowserEditorError("editor.invalid_state", "The Browser Editor command did not complete.");
  return new BrowserEditorError(editorCode(failure.code), failure.message, undefined, failure.diagnostics);
}

function composerError(error: unknown): ComposerError {
  const editor = transportError(error);
  const code: ComposerFailureCode = editor.code === "editor.access_denied" ? "composer.access_denied"
    : editor.code === "editor.stale_revision" ? "composer.stale_revision"
      : editor.code === "editor.capability_unavailable" ? "composer.capability_unavailable"
        : editor.code === "editor.cancelled" || editor.code === "editor.approval_cancelled" ? "composer.cancelled"
          : editor.code === "editor.conflict" ? "composer.conflict"
            : editor.code === "editor.invalid_state" ? "composer.invalid_state" : "composer.transport_failed";
  return new ComposerError({ code, message: editor.message, recoverable: true, diagnostics: editor.diagnostics as never, conflicts: [] });
}

class BrowserEditorImpl implements BrowserEditor {
  readonly #options: BrowserEditorOptions;
  readonly #controllers = new Set<AbortController>();
  readonly #composer: Composer;
  #session: ActiveSession | undefined;
  #lastPreview: EditorPreviewResult | undefined;
  #runtimePreview: BrowserRuntimePreviewResult | undefined;
  #closed = false;
  #intentSequence = 0;
  #lastHostError: BrowserEditorError | undefined;

  constructor(options: BrowserEditorOptions) {
    this.#options = options;
    this.#composer = new Composer({
      preview: async (edit, signal) => {
        try { return await this.#previewAuthoritative(edit, signal); }
        catch (error) { this.#lastHostError = transportError(error); throw composerError(error); }
      },
      apply: async (edit, presentation, signal) => {
        try { return await this.#applyAuthoritative(edit, presentation.preview, signal); }
        catch (error) { this.#lastHostError = transportError(error); throw composerError(error); }
      },
    });
  }

  async open(input: BrowserDocumentInput): Promise<BrowserDocumentSession> {
    this.#assertOpen();
    if (this.#session !== undefined) throw new BrowserEditorError("editor.invalid_state", "A document is already open.");
    if (input.authority === "runtime" && this.#options.runtime_client === undefined) {
      throw new BrowserEditorError("editor.capability_unavailable", "No Runtime client was supplied.");
    }
    const manifest = input.authority === "runtime" ? this.#options.runtime_client!.getCapabilities() : this.#options.engine_client.getCapabilities();
    const defaults = input.authority === "runtime"
      ? ["runtime.open_document", "runtime.preview_operations", "runtime.commit_operations"] as const
      : ["engine.open_document", "engine.preview_operations", "engine.apply_to_handle"] as const;
    negotiateEditorCapabilities(manifest, {
      required: this.#options.required_capabilities ?? defaults,
      ...(this.#options.optional_capabilities === undefined ? {} : { optional: this.#options.optional_capabilities }),
    });
    const controller = this.#begin();
    let providerOpened = false;
    try {
      await this.#options.document_provider?.open(input, controller.signal);
      providerOpened = this.#options.document_provider !== undefined;
      if (input.authority === "engine") {
        const session = unwrapEngine<OpenDocumentResult>(await this.#options.engine_client.workbench.openDocument(input.input, { signal: controller.signal }), "Open document");
        if (!session.capabilities.preview_operations || !session.capabilities.apply_to_handle) {
          await this.#closeEngineSession(session, controller.signal).catch(() => undefined);
          throw new BrowserEditorError("editor.capability_unavailable", "This document does not support semantic preview and apply.");
        }
        this.#session = { authority: "engine", session };
        return { authority: "engine", persistence: "ephemeral", session };
      }
      const session = await this.#options.runtime_client!.openDocument(input.input, { signal: controller.signal });
      this.#session = { authority: "runtime", session };
      return { authority: "runtime", persistence: "durable", session };
    } catch (error) {
      if (providerOpened) await this.#options.document_provider?.close().catch(() => undefined);
      throw transportError(error);
    } finally {
      this.#end(controller);
    }
  }

  async preview(edit: EditorEdit, options: Readonly<{ intent_id?: string; inverse?: EditorEdit }> = {}): Promise<EditorPreviewResult> {
    this.#requireSession();
    const intent = {
      id: options.intent_id ?? `browser-editor-${++this.#intentSequence}`,
      edit,
      ...(options.inverse === undefined ? {} : { inverse: options.inverse }),
    };
    this.#lastHostError = undefined;
    this.#lastPreview = undefined;
    this.#runtimePreview = undefined;
    const snapshot = await this.#composer.preview(intent);
    if (this.#lastPreview !== undefined && snapshot.intent === intent) return this.#lastPreview;
    if (this.#lastHostError !== undefined) throw this.#lastHostError;
    throw snapshotError(snapshot);
  }

  async apply(edit: EditorEdit): Promise<EditorApplyResult> {
    this.#requireSession();
    const before = this.#composer.snapshot();
    if (before.phase !== "ready" || before.intent?.edit !== edit) {
      if (before.failure !== undefined) throw snapshotError(before);
      throw new BrowserEditorError("editor.invalid_state", "Apply requires the latest valid preview for the same edit.");
    }
    this.#lastHostError = undefined;
    const snapshot = await this.#composer.apply();
    if (snapshot.apply_result !== undefined) return snapshot.apply_result as EditorApplyResult;
    if (this.#lastHostError !== undefined) throw this.#lastHostError;
    throw snapshotError(snapshot);
  }

  async undo(): Promise<ComposerSnapshot> { this.#requireSession(); return this.#composer.undo(); }
  async redo(): Promise<ComposerSnapshot> { this.#requireSession(); return this.#composer.redo(); }
  async retry(): Promise<ComposerSnapshot> { this.#requireSession(); return this.#composer.retry(); }
  cancelPreview(): ComposerSnapshot { this.#requireSession(); return this.#composer.cancelPreview(); }
  snapshot(): ComposerSnapshot { return this.#composer.snapshot(); }
  subscribe(listener: (snapshot: ComposerSnapshot) => void): () => void { return this.#composer.subscribe(listener); }

  async materializeView(input: MaterializeViewInput): Promise<ViewData> {
    const session = this.#requireSession();
    const controller = this.#begin();
    try {
      if (session.authority === "runtime") {
        return await this.#options.runtime_client!.materializeView(input, session.session.session, { signal: controller.signal });
      }
      const bound = { ...input, document_generation: session.session.document_generation } as MaterializeViewInput;
      return unwrapEngine<ViewData>(await this.#options.engine_client.workbench.materializeView(bound, { signal: controller.signal }), "Materialize view");
    } catch (error) {
      throw transportError(error);
    } finally {
      this.#end(controller);
    }
  }

  async close(): Promise<void> {
    if (this.#closed) return;
    this.#closed = true;
    for (const controller of this.#controllers) controller.abort();
    this.#controllers.clear();
    await this.#composer.close();
    const session = this.#session;
    this.#session = undefined;
    this.#lastPreview = undefined;
    this.#runtimePreview = undefined;
    const controller = new AbortController();
    const cleanup: Promise<unknown>[] = [];
    if (session?.authority === "engine") cleanup.push(this.#closeEngineSession(session.session, controller.signal));
    if (session?.authority === "runtime") cleanup.push(this.#options.runtime_client!.closeDocument(session.session.session, { signal: controller.signal }));
    cleanup.push(
      this.#options.document_provider?.close() ?? Promise.resolve(),
      this.#options.asset_resolver.dispose?.() ?? Promise.resolve(),
      this.#options.engine_client.dispose(),
      this.#options.runtime_client?.dispose?.() ?? Promise.resolve(),
    );
    if ((await Promise.allSettled(cleanup)).some((result) => result.status === "rejected")) {
      throw new BrowserEditorError("editor.transport_failed", "One or more Browser Editor resources failed to close.");
    }
  }

  async #previewAuthoritative(edit: EditorEdit, signal: AbortSignal): Promise<Readonly<{ preview: WorkbenchPreviewResult; authoring_decision?: AuthoringDecision; grant_summary?: AuthoringGrantSummary }>> {
    const session = this.#requireSession();
    try {
      if (session.authority === "runtime") {
        const runtime = await this.#options.runtime_client!.previewEditor(edit, session.session, { signal });
        const result: EditorPreviewResult = {
          authority: "runtime", preview: runtime.preview,
          authoring_decision: runtime.authoring_decision, grant_summary: runtime.grant_summary,
          conflicts: runtime.preview.conflicts, diagnostics: runtime.preview.diagnostics,
        };
        this.#runtimePreview = runtime;
        this.#lastPreview = result;
        return result;
      }
      const preview = await this.#previewEngine(edit, signal);
      let authoringDecision;
      let grantSummary;
      if (preview.authoring_impact !== undefined && this.#options.authoring_access_client !== undefined) {
        [authoringDecision, grantSummary] = await Promise.all([
          this.#options.authoring_access_client.evaluatePreview(preview, signal),
          this.#options.authoring_access_client.getEffectiveGrant(signal),
        ]);
      }
      const result: EditorPreviewResult = {
        authority: authoringDecision === undefined ? "engine" : "trusted_access", preview,
        ...(authoringDecision === undefined ? {} : { authoring_decision: authoringDecision }),
        ...(grantSummary === undefined ? {} : { grant_summary: grantSummary }),
        conflicts: preview.conflicts, diagnostics: preview.diagnostics,
      };
      this.#runtimePreview = undefined;
      this.#lastPreview = result;
      return result;
    } catch (error) {
      throw transportError(error);
    }
  }

  async #applyAuthoritative(edit: EditorEdit, preview: WorkbenchPreviewResult, signal: AbortSignal): Promise<EditorApplyResult> {
    const session = this.#requireSession();
    const result = this.#lastPreview;
    if (result?.authoring_decision?.outcome === "deny") throw new BrowserEditorError("editor.access_denied", "Authoring access was denied.");
    if (this.#options.approval_handler !== undefined && preview.authoring_impact !== undefined) {
      const approval = await this.#options.approval_handler.requestApproval(result!, signal);
      if (approval !== "approved") throw new BrowserEditorError(approval === "denied" ? "editor.access_denied" : "editor.approval_cancelled", "The authoring approval did not succeed.");
    }
    let applied: EditorApplyResult;
    if (session.authority === "runtime") {
      if (this.#runtimePreview === undefined || this.#options.runtime_commit_input_factory === undefined) {
        throw new BrowserEditorError("editor.invalid_state", "Runtime commit input construction was not supplied.");
      }
      const input = this.#options.runtime_commit_input_factory({
        edit, session: session.session, authoring_proof: this.#runtimePreview.authoring_proof,
        operation_batch: this.#runtimePreview.operation_batch,
      });
      const committed = await this.#options.runtime_client!.commitOperations(input, { signal });
      applied = this.#runtimeApplyResult(committed);
      if (applied.persistence === "durable") {
        this.#session = { authority: "runtime", session: { ...session.session, committed_revision: applied.committed_revision, working_document: { ...session.session.working_document, base_revision: applied.committed_revision } } };
      }
    } else {
      const outcome = await this.#options.engine_client.workbench.applyToHandle({
        base_generation: preview.base_generation,
        preview_digest: preview.preview_digest!,
        preview_id: preview.preview_id!,
      }, { signal });
      const engineApplied = unwrapEngine<ApplyToHandleResult>(outcome, "Apply edit");
      this.#session = { authority: "engine", session: { ...session.session, document_generation: engineApplied.document_generation } };
      if (this.#options.document_provider === undefined) applied = { persistence: "ephemeral", applied: engineApplied };
      else {
        const receipt = await this.#options.document_provider.writeWithPrecondition({ edit, preview: result, applied: engineApplied }, signal);
        applied = { persistence: "host_callback", receipt };
      }
    }
    await this.#options.approval_handler?.reportResult(applied, signal);
    this.#lastPreview = undefined;
    this.#runtimePreview = undefined;
    return applied;
  }

  async #previewEngine(edit: EditorEdit, signal: AbortSignal): Promise<WorkbenchPreviewResult> {
    if (edit.kind === "semantic_operations") return unwrapEngine(await this.#options.engine_client.workbench.previewOperations(edit.request, { signal }), "Preview operations");
    if (edit.kind === "fragment") return unwrapEngine(await this.#options.engine_client.workbench.previewFragment(edit.request, { signal }), "Preview fragment");
    return unwrapEngine(await this.#options.engine_client.workbench.previewSourcePatch(edit.request, { signal }), "Preview source patch");
  }

  #runtimeApplyResult(result: RuntimeCommitResult): EditorApplyResult {
    const operation = result.operation_result;
    if (operation.status === "needs_review" || operation.status === "rejected") {
      return { persistence: "runtime_not_committed", result: result as RuntimeNotCommittedEditorResult };
    }
    return { persistence: "durable", result: result as RuntimeCommittedEditorResult, committed_revision: operation.committed_revision! };
  }

  async #closeEngineSession(session: OpenDocumentResult, signal: AbortSignal): Promise<void> {
    unwrapEngine(await this.#options.engine_client.workbench.closeDocument({
      document_generation: session.document_generation,
      document_handle: session.document_handle,
    }, { signal }), "Close document");
  }

  #assertOpen(): void {
    if (this.#closed) throw new BrowserEditorError("editor.invalid_state", "The Browser Editor is closed.");
  }
  #requireSession(): ActiveSession {
    this.#assertOpen();
    if (this.#session === undefined) throw new BrowserEditorError("editor.invalid_state", "No document is open.");
    return this.#session;
  }
  #begin(): AbortController { const controller = new AbortController(); this.#controllers.add(controller); return controller; }
  #end(controller: AbortController): void { this.#controllers.delete(controller); }
}

export function createBrowserEditor(options: BrowserEditorOptions): BrowserEditor {
  return new BrowserEditorImpl(options);
}
