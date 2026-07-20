// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {
  createElement,
  useCallback,
  useEffect,
  useRef,
  type HTMLAttributes,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import { useEditorState } from "./provider.js";

function classes(base: string, extra: string | undefined): string { return extra === undefined ? base : `${base} ${extra}`; }

export function EditorShell({ className, ...props }: HTMLAttributes<HTMLDivElement>): ReactNode {
  return createElement("div", { ...props, className: classes("ld-editor-shell", className) });
}

export function EditorWorkspace({ className, ...props }: HTMLAttributes<HTMLDivElement>): ReactNode {
  return createElement("div", { ...props, className: classes("ld-editor-workspace", className) });
}

export interface EditorPanelProps extends HTMLAttributes<HTMLElement> {
  readonly label: string;
  readonly placement?: "primary" | "secondary" | "inspector";
}

export function EditorPanel({ label, placement = "primary", className, ...props }: EditorPanelProps): ReactNode {
  return createElement("section", {
    ...props,
    "aria-label": label,
    "data-placement": placement,
    className: classes("ld-editor-panel", className),
  });
}

export interface EditorToolbarProps extends HTMLAttributes<HTMLDivElement> {
  readonly label: string;
}

function enabledButtons(toolbar: HTMLDivElement): HTMLButtonElement[] {
  return [...toolbar.querySelectorAll<HTMLButtonElement>("button[data-layerdraw-command]:not(:disabled)")];
}

export function EditorToolbar({ label, className, onKeyDown, ...props }: EditorToolbarProps): ReactNode {
  const ref = useRef<HTMLDivElement>(null);
  const handleKeyDown = useCallback((event: KeyboardEvent<HTMLDivElement>) => {
    onKeyDown?.(event);
    if (event.defaultPrevented || !["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
    const toolbar = ref.current;
    if (toolbar === null) return;
    const buttons = enabledButtons(toolbar);
    if (buttons.length === 0) return;
    const current = buttons.indexOf(event.target as HTMLButtonElement);
    const next = event.key === "Home" ? 0
      : event.key === "End" ? buttons.length - 1
        : event.key === "ArrowRight" ? (Math.max(current, -1) + 1) % buttons.length
          : (current <= 0 ? buttons.length : current) - 1;
    buttons[next]?.focus();
    event.preventDefault();
  }, [onKeyDown]);
  return createElement("div", {
    ...props,
    ref,
    role: "toolbar",
    "aria-label": label,
    className: classes("ld-editor-toolbar", className),
    onKeyDown: handleKeyDown,
  });
}

export interface EditorLiveRegionProps {
  readonly format?: (state: ReturnType<typeof useEditorState>) => string;
}

function defaultAnnouncement(state: ReturnType<typeof useEditorState>): string {
  if (state.pendingAction !== undefined) return `${state.pendingAction} in progress.`;
  if (state.error instanceof Error) return state.error.message;
  if (state.snapshot.failure !== undefined) return state.snapshot.failure.message;
  if (state.decision?.outcome === "deny") return "Authoring access denied.";
  if (state.decision?.outcome === "approval_required") return "Authoring approval required.";
  if (state.snapshot.phase === "applied") return state.session?.persistence === "durable" ? "Changes committed." : "Ephemeral changes applied.";
  if (state.snapshot.phase === "ready") return "Preview ready.";
  return "";
}

export function EditorLiveRegion({ format = defaultAnnouncement }: EditorLiveRegionProps): ReactNode {
  const state = useEditorState();
  return createElement("div", { className: "ld-visually-hidden", role: "status", "aria-live": "polite", "aria-atomic": true }, format(state));
}

export interface RestoreFocusProps { readonly children?: ReactNode; }

export function RestoreFocus({ children }: RestoreFocusProps): ReactNode {
  const state = useEditorState();
  const previousPending = useRef(state.pendingAction);
  const origin = useRef<HTMLElement | null>(null);
  useEffect(() => {
    if (previousPending.current === undefined && state.pendingAction !== undefined && document.activeElement instanceof HTMLElement) {
      origin.current = document.activeElement;
    }
    if (previousPending.current !== undefined && state.pendingAction === undefined && origin.current?.isConnected) {
      origin.current.focus({ preventScroll: true });
      origin.current = null;
    }
    previousPending.current = state.pendingAction;
  }, [state.pendingAction]);
  return children;
}
