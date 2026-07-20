// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { CapabilityID } from "@layerdraw/protocol/common";
import {
  createElement,
  useEffect,
  useRef,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import type { DesktopShellFailure } from "./contracts.js";
import { DesktopShellController } from "./controller.js";
import { DesktopEditorSurface, type DesktopEditorCapabilityIDs } from "./editor-surface.js";
import { DesktopViewerSurface } from "./viewer-surface.js";

export interface DesktopShellLabels {
  readonly application: string;
  readonly views: string;
  readonly canvas: string;
  readonly inspector: string;
  readonly reviewRecovery: string;
  readonly noProject: string;
  readonly recovery: string;
  readonly unavailable: string;
  readonly failure: Readonly<Record<DesktopShellFailure["message_key"], string>>;
}

const labels: DesktopShellLabels = Object.freeze({
  application: "LayerDraw Desktop", views: "Views", canvas: "Canvas", inspector: "Project status", reviewRecovery: "Review recovery options",
  noProject: "Open or create a project to begin.", recovery: "LayerDraw needs recovery before this project can open.", unavailable: "This action is unavailable.",
  failure: {
    "desktop.error.lifecycle_failed": "Recovery options could not be opened.",
    "desktop.error.selection_failed": "The selected view could not be opened.",
    "desktop.error.viewer_rejected": "The view update was rejected.",
    "desktop.error.viewer_failed": "The view could not be displayed.",
    "desktop.error.context_mismatch": "A stale view update was ignored.",
  },
});

export interface DesktopShellProps {
  readonly controller: DesktopShellController;
  /** Exact capability ID supplied by the Desktop composition contract. */
  readonly viewSelectionCapability: CapabilityID;
  readonly editorCapabilities: DesktopEditorCapabilityIDs;
  readonly labels?: DesktopShellLabels;
}

function statusChip(kind: string, text: string): ReactNode {
  return createElement("span", { className: "ld-desktop-chip", "data-status": kind }, text);
}

export function DesktopShell({ controller, viewSelectionCapability, editorCapabilities, labels: suppliedLabels = labels }: DesktopShellProps): ReactNode {
  const state = useSyncExternalStore(controller.subscribe, controller.getSnapshot, controller.getSnapshot);
  const heading = useRef<HTMLHeadingElement>(null);
  const project = state.lifecycle.project;
  const capability = state.lifecycle.capabilities[viewSelectionCapability];
  const viewSelectionAvailable = capability?.status === "available";

  useEffect(() => {
    controller.start();
    return () => { void controller.stop(); };
  }, [controller]);
  useEffect(() => { heading.current?.focus({ preventScroll: true }); }, [project?.project_id, project?.selected_view_address]);

  const failure = state.failure === undefined ? null : suppliedLabels.failure[state.failure.message_key];
  if (state.lifecycle.phase === "starting") return createElement("main", { className: "ld-desktop-shell", "aria-label": suppliedLabels.application }, createElement("p", { role: "status", "aria-live": "polite" }, "Starting LayerDraw…"));
  if (state.lifecycle.phase === "recovery") return createElement("main", { className: "ld-desktop-shell ld-desktop-centered", "aria-label": suppliedLabels.application },
    createElement("h1", null, suppliedLabels.application), createElement("p", { role: "alert" }, suppliedLabels.recovery),
    createElement("button", { type: "button", disabled: state.pending_action !== undefined, onClick: () => { void controller.reviewRecovery(); } }, suppliedLabels.reviewRecovery));
  if (state.lifecycle.phase === "draining" || state.lifecycle.phase === "stopped") return createElement("main", { className: "ld-desktop-shell ld-desktop-centered", "aria-label": suppliedLabels.application }, createElement("p", { role: "status" }, "LayerDraw is closing…"));
  if (project === undefined) return createElement("main", { className: "ld-desktop-shell ld-desktop-centered", "aria-label": suppliedLabels.application }, createElement("h1", null, suppliedLabels.application), createElement("p", null, suppliedLabels.noProject));

  const viewList = createElement("ul", null, project.views.map((view) =>
    createElement("li", { key: view.address },
      createElement("button", {
        type: "button",
        disabled: !viewSelectionAvailable || state.pending_action !== undefined,
        "aria-current": view.address === project.selected_view_address ? "page" : undefined,
        "aria-label": !viewSelectionAvailable ? `${view.label}. ${suppliedLabels.unavailable}` : view.label,
        onClick: () => { void controller.selectView(view.address); },
      }, createElement("span", null, view.label), createElement("small", null, view.shape))),
  ));

  return createElement("main", { className: "ld-desktop-shell", "aria-label": suppliedLabels.application },
    createElement("header", { className: "ld-desktop-titlebar" },
      createElement("div", null, createElement("p", { className: "ld-desktop-eyebrow" }, suppliedLabels.application), createElement("h1", { ref: heading, tabIndex: -1 }, project.display_name)),
      createElement("div", { className: "ld-desktop-statuses", "aria-label": suppliedLabels.inspector },
        statusChip(project.storage.status, project.storage.label), statusChip(project.access.status, project.access.label), statusChip(project.persistence, project.authoritative_revision_label))),
    failure === null ? null : createElement("div", { role: state.failure?.recoverable === true ? "status" : "alert", className: "ld-desktop-notice" }, failure),
    createElement("div", { className: "ld-desktop-workspace" },
      createElement("nav", { className: "ld-desktop-sidebar", "aria-label": suppliedLabels.views },
        createElement("h2", null, suppliedLabels.views),
        viewList),
      createElement("section", { className: "ld-desktop-canvas", "aria-label": suppliedLabels.canvas },
        createElement(DesktopViewerSurface, { state: state.viewer, onSelectionChange: (keys) => controller.setViewerSelection(keys) })),
      createElement("aside", { className: "ld-desktop-inspector", "aria-label": suppliedLabels.inspector },
        createElement(DesktopEditorSurface, { project, capabilities: editorCapabilities }))),
    createElement("div", { className: "ld-desktop-visually-hidden", role: "status", "aria-live": "polite", "aria-atomic": true }, state.pending_action === "select_view" ? "Opening view…" : state.pending_action === "review_recovery" ? "Opening recovery options…" : ""));
}
