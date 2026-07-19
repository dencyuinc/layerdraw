// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { CapabilityID } from "@layerdraw/protocol/common";
import { createElement, useId, useRef, type ButtonHTMLAttributes, type ReactNode } from "react";
import { useCapability, useEditorCommands, useEditorState, type EditorState } from "./provider.js";

export type EditorAction = "apply" | "undo" | "redo" | "retry" | "cancel-preview";
export type EditorActionState = "unavailable" | "denied" | "pending" | "ephemeral" | "durable";

export function classifyEditorAction(
  state: Pick<EditorState, "session" | "snapshot" | "decision" | "pendingAction">,
  capabilityAvailable: boolean,
): EditorActionState {
  if (!capabilityAvailable) return "unavailable";
  if (state.pendingAction !== undefined || state.snapshot.phase === "previewing" || state.snapshot.phase === "applying") return "pending";
  if (state.decision?.outcome === "deny") return "denied";
  return state.session?.persistence === "durable" ? "durable" : "ephemeral";
}

export interface EditorCommandButtonProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, "onClick" | "disabled"> {
  readonly action: EditorAction;
  /** Supplied by the host from its capability contract; this package never maps actions to capabilities. */
  readonly capabilityId: CapabilityID;
  readonly children: ReactNode;
  readonly unavailableLabel?: string;
  readonly deniedLabel?: string;
  readonly pendingLabel?: string;
  readonly onComplete?: () => void;
}

export function EditorCommandButton({
  action,
  capabilityId,
  children,
  unavailableLabel = "This action is unavailable.",
  deniedLabel = "You do not have permission for this action.",
  pendingLabel = "This action is in progress.",
  onComplete,
  ...buttonProps
}: EditorCommandButtonProps): ReactNode {
  const state = useEditorState();
  const commands = useEditorCommands();
  const capability = useCapability(capabilityId);
  const status = classifyEditorAction(state, capability.available);
  const descriptionId = useId();
  const buttonRef = useRef<HTMLButtonElement>(null);
  const disabled = status === "unavailable" || status === "denied" || status === "pending";
  const description = status === "unavailable"
    ? `${unavailableLabel} ${capability.reason ?? ""}`.trim()
    : status === "denied" ? deniedLabel
      : status === "pending" ? pendingLabel
        : status === "durable" ? "Changes are committed durably."
          : "Changes remain ephemeral unless the host persists them.";

  const invoke = async (): Promise<void> => {
    const origin = buttonRef.current;
    if (action === "apply") await commands.apply();
    else if (action === "undo") await commands.undo();
    else if (action === "redo") await commands.redo();
    else if (action === "retry") await commands.retry();
    else commands.cancelPreview();
    onComplete?.();
    if (origin?.isConnected) origin.focus({ preventScroll: true });
  };

  return createElement("span", { className: "ld-control" },
    createElement("button", {
      ...buttonProps,
      ref: buttonRef,
      type: buttonProps.type ?? "button",
      disabled,
      "aria-disabled": disabled,
      "aria-busy": status === "pending",
      "aria-describedby": descriptionId,
      "data-layerdraw-command": action,
      "data-action-state": status,
      onClick: () => { void invoke(); },
    }, children),
    createElement("span", { id: descriptionId, className: "ld-visually-hidden" }, description),
  );
}

export interface CapabilityControlProps {
  readonly capabilityId: CapabilityID;
  readonly children: ReactNode;
  readonly fallback?: ReactNode;
}

export function CapabilityControl({ capabilityId, children, fallback = null }: CapabilityControlProps): ReactNode {
  return useCapability(capabilityId).available ? children : fallback;
}
