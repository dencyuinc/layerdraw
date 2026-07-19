// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import { Composer, ComposerError, type ComposerFailureCode, type ComposerSnapshot, type EditorEdit } from "@layerdraw/composer";
import type { OutputBlob, WorkbenchOutcome } from "@layerdraw/engine-client";
import type { AuthoringDecision, AuthoringGrantSummary } from "@layerdraw/protocol/access";
import type {
  ApplyToHandleResult,
  MaterializeViewInput,
  OpenDocumentResult,
  WorkbenchPreviewResult,
} from "@layerdraw/protocol/engine";
import type { OpenRuntimeDocumentResult, RuntimeCommitInput, RuntimeCommitResult } from "@layerdraw/protocol/runtime";
import type { ViewData } from "@layerdraw/protocol/semantic";
import {
  BrowserEditorError,
  negotiateEditorCapabilities,
  type BrowserDocumentInput,
  type BrowserDocumentSession,
  type BrowserEditor,
  type BrowserEditorCapabilityState,
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

interface PreviewContext {
  readonly edit: EditorEdit;
  readonly result: EditorPreviewResult;
  readonly runtime?: BrowserRuntimePreviewResult;
  applyError: BrowserEditorError | undefined;
}

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

function unwrapEngineResult<T>(outcome: WorkbenchOutcome<any>, operation: string): Readonly<{ payload: T; blobs: readonly OutputBlob[] }> {
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
  return { payload: response.payload as T, blobs: outcome.blobs };
}

function unwrapEngine<T>(outcome: WorkbenchOutcome<any>, operation: string): T {
  return unwrapEngineResult<T>(outcome, operation).payload;
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
  readonly #flights = new Set<Promise<unknown>>();
  readonly #previewContexts = new WeakMap<WorkbenchPreviewResult, PreviewContext>();
  readonly #composer: Composer;
  #session: ActiveSession | undefined;
  #capabilities: BrowserEditorCapabilityState | undefined;
  #providerOpened = false;
  #closed = false;
  #closePromise: Promise<void> | undefined;
  #intentSequence = 0;

  constructor(options: BrowserEditorOptions) {
    this.#options = options;
    this.#composer = new Composer({
      preview: async (edit, signal) => {
        try { return await this.#track(this.#previewAuthoritative(edit, signal)); }
        catch (error) { throw composerError(error); }
      },
      apply: async (edit, presentation, signal) => {
        const context = this.#previewContexts.get(presentation.preview);
        try {
          if (context === undefined || context.edit !== edit) throw new BrowserEditorError("editor.invalid_state", "The apply request does not match its authoritative preview.");
          return await this.#track(this.#applyAuthoritative(context, presentation.preview, signal));
        } catch (error) {
          if (context !== undefined) context.applyError = transportError(error);
          throw composerError(error);
        }
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
    const selection = negotiateEditorCapabilities(manifest, {
      required: this.#options.required_capabilities ?? defaults,
      ...(this.#options.optional_capabilities === undefined ? {} : { optional: this.#options.optional_capabilities }),
    });
    return this.#withController(async (signal) => {
      try {
      await this.#options.document_provider?.open(input, signal);
      this.#providerOpened = this.#options.document_provider !== undefined;
      this.#assertOperation(signal);
      if (input.authority === "engine") {
        const session = unwrapEngine<OpenDocumentResult>(await this.#options.engine_client.workbench.openDocument(input.input, { signal }), "Open document");
        if (!session.capabilities.preview_operations || !session.capabilities.apply_to_handle) {
          await this.#closeEngineSession(session, signal).catch(() => undefined);
          throw new BrowserEditorError("editor.capability_unavailable", "This document does not support semantic preview and apply.");
        }
        this.#session = { authority: "engine", session };
        this.#capabilities = { authority: "engine", manifest, selection };
        this.#assertOperation(signal);
        return { authority: "engine", persistence: "ephemeral", session, capabilities: this.#capabilities };
      }
      const session = await this.#options.runtime_client!.openDocument(input.input, { signal });
      this.#session = { authority: "runtime", session };
      this.#capabilities = { authority: "runtime", manifest, selection };
      this.#assertOperation(signal);
      return { authority: "runtime", persistence: "durable", session, capabilities: this.#capabilities };
    } catch (error) {
      if (this.#providerOpened && !this.#closed) {
        await this.#options.document_provider?.close().catch(() => undefined);
        this.#providerOpened = false;
      }
      throw transportError(error);
      }
    });
  }

  async preview(edit: EditorEdit, options: Readonly<{ intent_id?: string; inverse?: EditorEdit }> = {}): Promise<EditorPreviewResult> {
    this.#requireSession();
    const intent = {
      id: options.intent_id ?? `browser-editor-${++this.#intentSequence}`,
      edit,
      ...(options.inverse === undefined ? {} : { inverse: options.inverse }),
    };
    const snapshot = await this.#track(this.#composer.preview(intent));
    if (this.#closed) throw new BrowserEditorError("editor.cancelled", "The Browser Editor closed before preview completed.");
    const context = snapshot.presentation === undefined ? undefined : this.#previewContexts.get(snapshot.presentation.preview);
    if (context !== undefined && context.edit === edit && snapshot.intent === intent) return context.result;
    throw snapshotError(snapshot);
  }

  async apply(edit: EditorEdit): Promise<EditorApplyResult> {
    this.#requireSession();
    const before = this.#composer.snapshot();
    if (before.phase !== "ready" || before.intent?.edit !== edit) {
      if (before.failure !== undefined) throw snapshotError(before);
      throw new BrowserEditorError("editor.invalid_state", "Apply requires the latest valid preview for the same edit.");
    }
    if (before.presentation === undefined) throw new BrowserEditorError("editor.invalid_state", "Apply requires preview presentation data.");
    const context = this.#previewContexts.get(before.presentation.preview);
    if (context === undefined || context.edit !== edit) throw new BrowserEditorError("editor.invalid_state", "Apply requires the exact authoritative preview context.");
    context.applyError = undefined;
    const snapshot = await this.#track(this.#composer.apply());
    if (this.#closed) throw new BrowserEditorError("editor.cancelled", "The Browser Editor closed before apply completed.");
    if (snapshot.apply_result !== undefined) return snapshot.apply_result as EditorApplyResult;
    if (context.applyError !== undefined) throw context.applyError;
    throw snapshotError(snapshot);
  }

  async undo(): Promise<ComposerSnapshot> { this.#requireSession(); return this.#track(this.#composer.undo()); }
  async redo(): Promise<ComposerSnapshot> { this.#requireSession(); return this.#track(this.#composer.redo()); }
  async retry(): Promise<ComposerSnapshot> { this.#requireSession(); return this.#track(this.#composer.retry()); }
  cancelPreview(): ComposerSnapshot { this.#requireSession(); return this.#composer.cancelPreview(); }
  snapshot(): ComposerSnapshot { return this.#composer.snapshot(); }
  subscribe(listener: (snapshot: ComposerSnapshot) => void): () => void { return this.#composer.subscribe(listener); }
  getCapabilities(): BrowserEditorCapabilityState | undefined { return this.#capabilities; }

  async materializeView(input: MaterializeViewInput): Promise<ViewData> {
    const session = this.#requireSession();
    return this.#withController(async (signal) => {
      try {
      if (session.authority === "runtime") {
        const view = await this.#options.runtime_client!.materializeView(input, session.session.session, { signal });
        this.#assertOperation(signal);
        return view;
      }
      const bound = { ...input, document_generation: session.session.document_generation } as MaterializeViewInput;
      const view = unwrapEngine<ViewData>(await this.#options.engine_client.workbench.materializeView(bound, { signal }), "Materialize view");
      this.#assertOperation(signal);
      return view;
    } catch (error) {
      throw transportError(error);
      }
    });
  }

  async close(): Promise<void> {
    if (this.#closePromise !== undefined) return this.#closePromise;
    this.#closed = true;
    for (const controller of this.#controllers) controller.abort();
    this.#closePromise = this.#closeOwned();
    return this.#closePromise;
  }

  async #closeOwned(): Promise<void> {
    await this.#composer.close();
    while (this.#flights.size > 0) await Promise.allSettled([...this.#flights]);
    const session = this.#session;
    this.#session = undefined;
    this.#capabilities = undefined;
    const controller = new AbortController();
    const cleanup: Promise<unknown>[] = [];
    if (session?.authority === "engine") cleanup.push(this.#closeEngineSession(session.session, controller.signal));
    if (session?.authority === "runtime") cleanup.push(this.#options.runtime_client!.closeDocument(session.session.session, { signal: controller.signal }));
    cleanup.push(
      this.#providerOpened ? this.#options.document_provider?.close() ?? Promise.resolve() : Promise.resolve(),
      this.#options.asset_resolver.dispose?.() ?? Promise.resolve(),
      this.#options.engine_client.dispose(),
      this.#options.runtime_client?.dispose?.() ?? Promise.resolve(),
    );
    if ((await Promise.allSettled(cleanup)).some((result) => result.status === "rejected")) {
      throw new BrowserEditorError("editor.transport_failed", "One or more Browser Editor resources failed to close.");
    }
    this.#providerOpened = false;
  }

  async #previewAuthoritative(edit: EditorEdit, signal: AbortSignal): Promise<Readonly<{ preview: WorkbenchPreviewResult; authoring_decision?: AuthoringDecision; grant_summary?: AuthoringGrantSummary }>> {
    const session = this.#requireSession();
    try {
      if (session.authority === "runtime") {
        const runtime = await this.#options.runtime_client!.previewEditor(edit, session.session, { signal });
        this.#assertOperation(signal);
        const preview = Object.freeze({ ...runtime.preview }) as WorkbenchPreviewResult;
        const result: EditorPreviewResult = {
          authority: "runtime", preview,
          authoring_decision: runtime.authoring_decision, grant_summary: runtime.grant_summary,
          conflicts: preview.conflicts, diagnostics: preview.diagnostics,
        };
        this.#previewContexts.set(preview, { edit, result, runtime: { ...runtime, preview }, applyError: undefined });
        return result;
      }
      const enginePreview = await this.#previewEngine(edit, signal);
      this.#assertOperation(signal);
      const preview = Object.freeze({ ...enginePreview.preview }) as WorkbenchPreviewResult;
      let authoringDecision;
      let grantSummary;
      if (preview.authoring_impact !== undefined && this.#options.authoring_access_client !== undefined) {
        [authoringDecision, grantSummary] = await Promise.all([
          this.#options.authoring_access_client.evaluatePreview(preview, signal),
          this.#options.authoring_access_client.getEffectiveGrant(signal),
        ]);
        this.#assertOperation(signal);
      }
      const result: EditorPreviewResult = {
        authority: authoringDecision === undefined ? "engine" : "trusted_access", preview,
        ...(authoringDecision === undefined ? {} : { authoring_decision: authoringDecision }),
        ...(grantSummary === undefined ? {} : { grant_summary: grantSummary }),
        conflicts: preview.conflicts, diagnostics: preview.diagnostics,
      };
      this.#previewContexts.set(preview, { edit, result, applyError: undefined });
      return result;
    } catch (error) {
      throw transportError(error);
    }
  }

  async #applyAuthoritative(context: PreviewContext, preview: WorkbenchPreviewResult, signal: AbortSignal): Promise<EditorApplyResult> {
    const session = this.#requireSession();
    const { edit, result } = context;
    if (result?.authoring_decision?.outcome === "deny") throw new BrowserEditorError("editor.access_denied", "Authoring access was denied.");
    if (result.authoring_decision?.outcome === "approval_required") {
      if (preview.authoring_impact === undefined || this.#options.approval_handler === undefined) {
        throw new BrowserEditorError("editor.access_denied", "Authoring approval is required but no trusted approval path is available.");
      }
      const approval = await this.#options.approval_handler.requestApproval(result, signal);
      this.#assertOperation(signal);
      if (approval !== "approved") throw new BrowserEditorError(approval === "denied" ? "editor.access_denied" : "editor.approval_cancelled", "The authoring approval did not succeed.");
    }
    let applied: EditorApplyResult;
    if (session.authority === "runtime") {
      if (context.runtime === undefined || this.#options.runtime_commit_input_factory === undefined) {
        throw new BrowserEditorError("editor.invalid_state", "Runtime commit input construction was not supplied.");
      }
      const metadata = this.#options.runtime_commit_input_factory({
        edit, session: session.session, authoring_proof: context.runtime.authoring_proof,
        operation_batch: context.runtime.operation_batch,
      });
      const input: RuntimeCommitInput = {
        ...metadata,
        session: session.session.session,
        operation_batch: context.runtime.operation_batch,
        authoring_proof: context.runtime.authoring_proof,
      };
      if (input.session !== session.session.session || input.operation_batch !== context.runtime.operation_batch || input.authoring_proof !== context.runtime.authoring_proof) {
        throw new BrowserEditorError("editor.invalid_state", "Runtime commit evidence did not preserve the authoritative preview candidate.");
      }
      const committed = await this.#options.runtime_client!.commitOperations(input, { signal });
      this.#assertOperation(signal);
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
      const engineResult = unwrapEngineResult<ApplyToHandleResult>(outcome, "Apply edit");
      const engineApplied = engineResult.payload;
      this.#assertOperation(signal);
      this.#session = { authority: "engine", session: { ...session.session, document_generation: engineApplied.document_generation } };
      if (this.#options.document_provider === undefined) applied = { persistence: "ephemeral", applied: engineApplied };
      else {
        const receipt = await this.#options.document_provider.writeWithPrecondition({ edit, preview: result, applied: engineApplied, blobs: engineResult.blobs }, signal);
        this.#assertOperation(signal);
        applied = { persistence: "host_callback", receipt };
      }
    }
    await this.#options.approval_handler?.reportResult(applied, signal);
    this.#assertOperation(signal);
    return applied;
  }

  async #previewEngine(edit: EditorEdit, signal: AbortSignal): Promise<Readonly<{ preview: WorkbenchPreviewResult; blobs: readonly OutputBlob[] }>> {
    if (edit.kind === "semantic_operations") {
      const result = unwrapEngineResult<WorkbenchPreviewResult>(await this.#options.engine_client.workbench.previewOperations(edit.request, { signal }), "Preview operations");
      return { preview: result.payload, blobs: result.blobs };
    }
    if (edit.kind === "fragment") {
      const result = unwrapEngineResult<WorkbenchPreviewResult>(await this.#options.engine_client.workbench.previewFragment(edit.request, { signal }), "Preview fragment");
      return { preview: result.payload, blobs: result.blobs };
    }
    const result = unwrapEngineResult<WorkbenchPreviewResult>(await this.#options.engine_client.workbench.previewSourcePatch(edit.request, { signal }), "Preview source patch");
    return { preview: result.payload, blobs: result.blobs };
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
  #assertOperation(signal: AbortSignal): void {
    if (this.#closed || signal.aborted) throw new BrowserEditorError("editor.cancelled", "The Browser Editor operation was cancelled.");
  }
  #track<T>(promise: Promise<T>): Promise<T> {
    const tracked = promise.finally(() => { this.#flights.delete(tracked); });
    this.#flights.add(tracked);
    return tracked;
  }
  #withController<T>(operation: (signal: AbortSignal) => Promise<T>): Promise<T> {
    const controller = new AbortController();
    this.#controllers.add(controller);
    return this.#track(Promise.resolve().then(() => operation(controller.signal)).finally(() => {
      this.#controllers.delete(controller);
    }));
  }
}

export function createBrowserEditor(options: BrowserEditorOptions): BrowserEditor {
  return new BrowserEditorImpl(options);
}
