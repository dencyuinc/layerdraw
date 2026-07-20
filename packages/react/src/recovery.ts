// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { BrowserEditorError } from "@layerdraw/client-sdk/editor";
import type { ComposerIntent } from "@layerdraw/composer";
import type { ProtocolDiagnostic } from "@layerdraw/protocol/common";
import type { Diagnostic } from "@layerdraw/protocol/semantic";
import {
  createElement,
  useEffect,
  useId,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useEditor, type EditorState } from "./provider.js";

export type AuthoringWorkflowStatus =
  | "idle"
  | "previewing"
  | "review"
  | "approval-required"
  | "denied"
  | "approval-cancelled"
  | "stale"
  | "conflict"
  | "disconnected"
  | "failed"
  | "applied-ephemeral"
  | "applied-host"
  | "applied-not-committed"
  | "applied-durable";

export type AuthoringConnectionState =
  | Readonly<{ status: "connected" }>
  | Readonly<{ status: "disconnected"; reason: string }>;

function errorCode(error: unknown): BrowserEditorError["code"] | undefined {
  if (typeof error !== "object" || error === null || !("code" in error)) return undefined;
  const code = error.code;
  return typeof code === "string" && code.startsWith("editor.") ? code as BrowserEditorError["code"] : undefined;
}

export function classifyAuthoringWorkflow(
  state: Pick<EditorState, "session" | "snapshot" | "decision" | "conflicts" | "error" | "pendingAction">,
  connection: AuthoringConnectionState = { status: "connected" },
): AuthoringWorkflowStatus {
  const failure = state.snapshot.failure;
  const code = errorCode(state.error);
  if (connection.status === "disconnected" || code === "editor.transport_failed" || failure?.code === "composer.transport_failed" || failure?.code === "composer.session_closed") return "disconnected";
  if (code === "editor.approval_cancelled") return "approval-cancelled";
  if (state.decision?.outcome === "deny" || code === "editor.access_denied" || failure?.code === "composer.access_denied") return "denied";
  if (failure?.code === "composer.stale_revision" || code === "editor.stale_revision" || state.conflicts.some((conflict) => "kind" in conflict && conflict.kind === "stale_revision")) return "stale";
  if (failure?.code === "composer.conflict" || state.conflicts.length > 0) return "conflict";
  if (state.pendingAction === "preview" || state.snapshot.phase === "previewing") return "previewing";
  if (state.snapshot.phase === "applied") {
    if (state.snapshot.apply_result?.persistence === "durable") return "applied-durable";
    if (state.snapshot.apply_result?.persistence === "host_callback") return "applied-host";
    if (state.snapshot.apply_result?.persistence === "runtime_not_committed") return "applied-not-committed";
    return "applied-ephemeral";
  }
  if (state.snapshot.phase === "ready" && state.decision?.outcome === "approval_required") return "approval-required";
  if (state.snapshot.phase === "ready") return "review";
  if (state.snapshot.phase === "failed" || state.error !== undefined) return "failed";
  return "idle";
}

export interface AuthoringOperationContext {
  readonly operation_id: string;
  readonly revision: string;
}

export interface AuthoringRecoveryHandlers {
  readonly refresh?: (intent: ComposerIntent | undefined, signal: AbortSignal) => Promise<void>;
  readonly reopen?: (signal: AbortSignal) => Promise<void>;
  readonly discard?: (intent: ComposerIntent | undefined, signal: AbortSignal) => Promise<void>;
}

export interface AuthoringRecoveryWorkflowProps {
  readonly context: AuthoringOperationContext;
  readonly connection?: AuthoringConnectionState;
  readonly approvalAvailable?: boolean;
  readonly handlers?: AuthoringRecoveryHandlers;
  readonly heading?: string;
}

type RecoveryAction = "refresh" | "reopen" | "discard" | "repreview" | "retry" | "apply";

function diagnosticMessage(diagnostic: Diagnostic | ProtocolDiagnostic): string {
  return diagnostic.message ?? ("message_key" in diagnostic ? diagnostic.message_key : diagnostic.code);
}

export function AuthoringRecoveryWorkflow({
  context,
  connection = { status: "connected" },
  approvalAvailable = true,
  handlers = {},
  heading = "Authoring review and recovery",
}: AuthoringRecoveryWorkflowProps): ReactNode {
  const { state, commands } = useEditor();
  const status = classifyAuthoringWorkflow(state, connection);
  const id = useId();
  const generation = useRef(0);
  const activeFlight = useRef(false);
  const controllers = useRef(new Set<AbortController>());
  const [pending, setPending] = useState<RecoveryAction>();
  const [localError, setLocalError] = useState<string>();
  const diagnosticContext = useRef<Readonly<{ sequence: number; value: AuthoringOperationContext }>>({ sequence: state.snapshot.sequence, value: context });
  if (state.diagnostics.length > 0 && diagnosticContext.current.sequence !== state.snapshot.sequence) {
    diagnosticContext.current = { sequence: state.snapshot.sequence, value: context };
  }

  useEffect(() => () => {
    generation.current += 1;
    activeFlight.current = false;
    for (const controller of controllers.current) controller.abort();
    controllers.current.clear();
  }, []);

  useEffect(() => {
    generation.current += 1;
    activeFlight.current = false;
    for (const controller of controllers.current) controller.abort();
    controllers.current.clear();
    setPending(undefined);
    setLocalError(undefined);
  }, [context.operation_id, context.revision]);

  const runHost = (action: RecoveryAction, operation: ((signal: AbortSignal) => Promise<void>) | undefined): void => {
    if (operation === undefined || activeFlight.current) return;
    const current = ++generation.current;
    const controller = new AbortController();
    controllers.current.add(controller);
    activeFlight.current = true;
    setPending(action);
    setLocalError(undefined);
    void Promise.resolve().then(() => operation(controller.signal)).catch((error: unknown) => {
      if (!controller.signal.aborted && generation.current === current) setLocalError(error instanceof Error ? error.message : "Recovery failed.");
    }).finally(() => {
      controllers.current.delete(controller);
      if (!controller.signal.aborted && generation.current === current) { activeFlight.current = false; setPending(undefined); }
    });
  };

  const runCommand = (action: RecoveryAction, operation: () => Promise<unknown>): void => {
    if (activeFlight.current) return;
    const current = ++generation.current;
    activeFlight.current = true;
    setPending(action);
    setLocalError(undefined);
    void operation().finally(() => {
      if (generation.current === current) { activeFlight.current = false; setPending(undefined); }
    });
  };

  const repreview = (): void => {
    const intent = state.snapshot.intent;
    if (intent === undefined || status === "denied") return;
    runCommand("repreview", () => commands.preview(intent.edit, { intent_id: intent.id, ...(intent.inverse === undefined ? {} : { inverse: intent.inverse }) }));
  };
  const retry = (): void => {
    if (status === "denied") return;
    runCommand("retry", () => commands.retry());
  };
  const apply = (): void => {
    if (status === "denied" || (status === "approval-required" && !approvalReady)) return;
    runCommand("apply", () => commands.apply());
  };

  const diagnostics = state.diagnostics;
  const bySeverity = new Map<string, (Diagnostic | ProtocolDiagnostic)[]>();
  for (const diagnostic of diagnostics) {
    const group = bySeverity.get(diagnostic.severity) ?? [];
    group.push(diagnostic);
    bySeverity.set(diagnostic.severity, group);
  }
  const intent = state.snapshot.intent;
  const persistence = state.snapshot.apply_result?.persistence ?? state.session?.persistence ?? "ephemeral";
  const disabled = pending !== undefined;
  const approvalReady = approvalAvailable && state.impact !== undefined && state.grant !== undefined;
  const displayedContext = state.diagnostics.length > 0 ? diagnosticContext.current.value : context;
  const persistenceLabel = state.snapshot.apply_result?.persistence === "durable" ? "Durable committed revision"
    : state.snapshot.apply_result?.persistence === "runtime_not_committed" ? "Runtime did not commit"
      : state.snapshot.apply_result?.persistence === "host_callback" ? "Host-persisted change"
        : state.snapshot.apply_result?.persistence === "ephemeral" ? "Ephemeral local change"
          : state.session?.persistence === "durable" ? "Durable session; change not yet committed" : "Ephemeral local session";

  return createElement("section", { className: "ld-authoring-recovery", "aria-labelledby": `${id}-heading`, "data-workflow-status": status },
    createElement("h2", { id: `${id}-heading` }, heading),
    createElement("dl", { className: "ld-operation-context" },
      createElement("dt", null, "Operation"), createElement("dd", null, displayedContext.operation_id),
      createElement("dt", null, "Revision"), createElement("dd", null, displayedContext.revision),
      createElement("dt", null, "Persistence"), createElement("dd", { "data-persistence": persistence }, persistenceLabel)),
    createElement("p", { role: status === "denied" || status === "disconnected" || status === "failed" ? "alert" : "status" },
      status === "disconnected" ? connection.status === "disconnected" ? connection.reason : "The editor transport is disconnected."
        : status === "denied" ? "Authoring access was denied."
          : status === "approval-required" ? approvalReady ? "Review the authoritative impact before requesting approval." : "Approval is required but no complete trusted approval review is available."
            : status === "approval-cancelled" ? "Approval was cancelled. The change was not applied."
              : status === "stale" ? "The document revision changed. Your original intent is preserved."
                : status === "conflict" ? "The authoring intent conflicts with the current document."
                  : status === "applied-durable" ? "Changes are committed durably."
                    : status === "applied-host" ? "The host persisted the change."
                      : status === "applied-not-committed" ? "The Runtime did not commit the change."
                        : status === "applied-ephemeral" ? "Changes are applied locally and remain ephemeral."
                          : status === "failed" ? "The authoring operation failed." : ""),
    localError === undefined ? null : createElement("p", { role: "alert" }, localError),
    diagnostics.length === 0 ? null : createElement("nav", { "aria-label": "Diagnostic groups" },
      createElement("ul", null, [...bySeverity.keys()].map((severity) => createElement("li", { key: severity }, createElement("a", { href: `#${id}-diagnostics-${severity}` }, `${severity} (${bySeverity.get(severity)?.length ?? 0})`))))),
    [...bySeverity].map(([severity, items]) => createElement("section", { key: severity, id: `${id}-diagnostics-${severity}`, "aria-label": `${severity} diagnostics` },
      createElement("h3", null, severity),
      createElement("ol", null, items.map((diagnostic, index) => createElement("li", { key: `${diagnostic.code}-${index}` },
        createElement("code", null, diagnostic.code), " ", diagnosticMessage(diagnostic)))))),
    state.impact === undefined ? null : createElement("section", { "aria-label": "Authoring impact" },
      createElement("h3", null, "Authoring impact"),
      createElement("p", null, `${state.impact.entries.length} impacted subject(s)`),
      createElement("ul", null, state.impact.entries.map((entry, index) => createElement("li", { key: `${entry.subject_address ?? entry.owner_address ?? "impact"}-${index}` }, `${entry.action}: ${entry.subject_kind} (${entry.capability})`)))),
    state.grant === undefined ? null : createElement("section", { "aria-label": "Grant summary" },
      createElement("h3", null, "Grant summary"),
      createElement("p", null, `Granted: ${state.grant.granted_capabilities.join(", ") || "none"}`),
      createElement("p", null, `Constrained: ${state.grant.constrained_capabilities.join(", ") || "none"}`)),
    state.conflicts.length === 0 ? null : createElement("section", { "aria-label": "Conflicts" }, createElement("h3", null, "Conflicts"),
      createElement("ol", null, state.conflicts.map((conflict, index) => createElement("li", { key: index }, "kind" in conflict ? conflict.kind : "Runtime revision conflict")))),
    createElement("div", { className: "ld-recovery-actions", role: "group", "aria-label": "Recovery actions" },
      status === "review" || status === "approval-required" ? createElement("button", { type: "button", disabled: disabled || (status === "approval-required" && !approvalReady), onClick: apply }, status === "approval-required" ? "Request approval and apply" : "Apply") : null,
      status === "stale" || status === "conflict" ? createElement("button", { type: "button", disabled: disabled || handlers.refresh === undefined, onClick: () => runHost("refresh", handlers.refresh === undefined ? undefined : (signal) => handlers.refresh?.(intent, signal) ?? Promise.resolve()) }, "Refresh") : null,
      status === "stale" || status === "conflict" || status === "approval-cancelled" ? createElement("button", { type: "button", disabled: disabled || intent === undefined, onClick: repreview }, "Re-preview intent") : null,
      (status === "failed" || status === "stale" || status === "conflict") && state.snapshot.failure?.recoverable === true ? createElement("button", { type: "button", disabled, onClick: retry }, "Retry") : null,
      status === "disconnected" ? createElement("button", { type: "button", disabled: disabled || handlers.reopen === undefined, onClick: () => runHost("reopen", handlers.reopen) }, "Reopen session") : null,
      status === "stale" || status === "conflict" || status === "approval-cancelled" ? createElement("button", { type: "button", disabled: disabled || handlers.discard === undefined, onClick: () => runHost("discard", handlers.discard === undefined ? undefined : (signal) => handlers.discard?.(intent, signal) ?? Promise.resolve()) }, "Discard intent") : null),
    pending === undefined ? null : createElement("p", { role: "status" }, `${pending} in progress.`),
  );
}
