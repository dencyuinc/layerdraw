// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { CapabilityID } from "@layerdraw/protocol/common";
import {
  createElement,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import type { DesktopFeatureAvailability, DesktopMCPPort, DesktopProjectDialogPort, DesktopShellFailure } from "./contracts.js";
import { DesktopShellController } from "./controller.js";
import { DesktopEditorSurface, type DesktopEditorCapabilityIDs } from "./editor-surface.js";
import { DesktopViewerSurface } from "./viewer-surface.js";
import { ReviewPanel } from "@layerdraw/react/review";
import type { ReviewModel } from "@layerdraw/review";
import type { LibraryController } from "@layerdraw/library";
import { DesktopMCPPanel } from "./mcp-panel.js";
import { DesktopLibraryPanel } from "./library-panel.js";

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
  /** Canonical Review owner shared with MCP; omitted only when no project Review session exists. */
  readonly reviewModel?: ReviewModel;
  readonly library?: LibraryController;
  readonly libraryAvailability?: DesktopFeatureAvailability;
  readonly reviewAvailability?: DesktopFeatureAvailability;
  readonly mcp?: DesktopMCPPort;
  readonly projectDialogs?: DesktopProjectDialogPort;
  readonly labels?: DesktopShellLabels;
}

function statusChip(kind: string, text: string): ReactNode {
  return createElement("span", { className: "ld-desktop-chip", "data-status": kind }, text);
}

export function DesktopShell({ controller, viewSelectionCapability, editorCapabilities, reviewModel, library, libraryAvailability, reviewAvailability, mcp, projectDialogs, labels: suppliedLabels = labels }: DesktopShellProps): ReactNode {
  const state = useSyncExternalStore(controller.subscribe, controller.getSnapshot, controller.getSnapshot);
  const heading = useRef<HTMLHeadingElement>(null);
  const dialogSequence = useRef(0);
  const [dialogPending, setDialogPending] = useState<"create" | "open">();
  const [dialogFailure, setDialogFailure] = useState(false);
  const project = state.lifecycle.project;
  const capability = state.lifecycle.capabilities[viewSelectionCapability];
  const viewSelectionAvailable = capability?.status === "available";

  useEffect(() => {
    controller.start();
    return () => { void controller.stop(); };
  }, [controller]);
  useEffect(() => { heading.current?.focus({ preventScroll: true }); }, [project?.project_id, project?.selected_view_address]);

  const failure = state.failure === undefined ? null : suppliedLabels.failure[state.failure.message_key];
  const runProjectDialog = (kind: "create" | "open"): void => {
    if (projectDialogs === undefined || dialogPending !== undefined) return;
    setDialogPending(kind);
    setDialogFailure(false);
    const requestID = `desktop-shell-${kind}-${++dialogSequence.current}`;
    void projectDialogs[kind](requestID).then((result) => {
      if (result.outcome === "failed" || result.outcome === "rejected") setDialogFailure(true);
    }, () => setDialogFailure(true)).finally(() => setDialogPending(undefined));
  };
  if (state.lifecycle.phase === "starting") return createElement("main", { className: "ld-desktop-shell", "aria-label": suppliedLabels.application }, createElement("p", { role: "status", "aria-live": "polite" }, "Starting LayerDraw…"));
  if (state.lifecycle.phase === "recovery") return createElement("main", { className: "ld-desktop-shell ld-desktop-centered", "aria-label": suppliedLabels.application },
    createElement("h1", null, suppliedLabels.application), createElement("p", { role: "alert" }, suppliedLabels.recovery),
    createElement("button", { type: "button", disabled: state.pending_action !== undefined, onClick: () => { void controller.reviewRecovery(); } }, suppliedLabels.reviewRecovery));
  if (state.lifecycle.phase === "draining" || state.lifecycle.phase === "stopped") return createElement("main", { className: "ld-desktop-shell ld-desktop-centered", "aria-label": suppliedLabels.application }, createElement("p", { role: "status" }, "LayerDraw is closing…"));
  const featureStatus = libraryAvailability === undefined && reviewAvailability === undefined ? null : createElement("section", { "aria-label": "Desktop capabilities" },
    createElement("h2", null, "Capabilities"),
    createElement("ul", null,
      libraryAvailability === undefined ? null : createElement("li", { "data-status": libraryAvailability.status }, `Library: ${libraryAvailability.status}`),
      reviewAvailability === undefined ? null : createElement("li", { "data-status": reviewAvailability.status }, `Review: ${reviewAvailability.status}`)));
  if (project === undefined) return createElement("main", { className: "ld-desktop-shell ld-desktop-centered", "aria-label": suppliedLabels.application }, createElement("h1", null, suppliedLabels.application), createElement("p", null, suppliedLabels.noProject),
    projectDialogs === undefined ? null : createElement("div", { className: "ld-desktop-project-actions", "aria-label": "Project actions" },
      createElement("button", { type: "button", disabled: dialogPending !== undefined, onClick: () => runProjectDialog("create") }, dialogPending === "create" ? "Creating…" : "New project"),
      createElement("button", { type: "button", disabled: dialogPending !== undefined, onClick: () => runProjectDialog("open") }, dialogPending === "open" ? "Opening…" : "Open project")),
    failure === null && !dialogFailure ? null : createElement("div", { role: "alert", className: "ld-desktop-notice" }, failure ?? "The project could not be opened."), featureStatus, library === undefined ? null : createElement(DesktopLibraryPanel, { library }));

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
        project.storage.kind === "external" ? createElement("section", { className: "ld-desktop-storage", "aria-label": "External storage" },
          createElement("h2", null, "External storage"),
          createElement("dl", null,
            createElement("div", null, createElement("dt", null, "Provider"), createElement("dd", null, project.storage.provider_label ?? project.storage.label)),
            createElement("div", null, createElement("dt", null, "Account"), createElement("dd", null, project.storage.account_label ?? "Unavailable")),
            createElement("div", null, createElement("dt", null, "Scope"), createElement("dd", null, project.storage.scope_label ?? "Unavailable")),
            createElement("div", null, createElement("dt", null, "Last sync"), createElement("dd", null, project.storage.last_sync_label ?? "Never")),
            createElement("div", null, createElement("dt", null, "Pending"), createElement("dd", null, String(project.storage.pending_changes ?? 0)))),
          project.storage.status === "conflict" || project.storage.status === "reconcile_pending"
            ? createElement("p", { role: "status", className: "ld-desktop-storage-warning" }, "Review external changes before publishing.") : null,
          createElement("p", { className: "ld-desktop-storage-consequence" }, project.storage.disconnect_consequence ?? "Disconnecting keeps the local project and stops external sync."),
          createElement("button", { type: "button", disabled: state.pending_action !== undefined, onClick: () => { void controller.disconnectExternal(); } }, "Disconnect")) : null,
        createElement(DesktopEditorSurface, { project, capabilities: editorCapabilities }),
        library === undefined ? null : createElement(DesktopLibraryPanel, { library, project: project.library_project }),
        featureStatus,
        reviewModel === undefined ? null : createElement(ReviewPanel, { model: reviewModel }),
        createElement(DesktopMCPPanel, { mcp: mcp ?? unavailableMCP, projectID: project.project_id }))),
    createElement("div", { className: "ld-desktop-visually-hidden", role: "status", "aria-live": "polite", "aria-atomic": true }, state.pending_action === "select_view" ? "Opening view…" : state.pending_action === "review_recovery" ? "Opening recovery options…" : state.pending_action === "disconnect_storage" ? "Disconnecting external storage…" : ""));
}

const unavailableMCP: DesktopMCPPort = Object.freeze<DesktopMCPPort>({
	async status() { return { enabled: false, transport: "local", instructions: "", generation: 0 }; },
	async setEnabled() { return { outcome: "failed", failure: { code: "desktop.mcp_disabled", retryable: false, recovery: "configure_adapter" } }; },
	async restart() { return { outcome: "failed", failure: { code: "desktop.mcp_disabled", retryable: false, recovery: "configure_adapter" } }; },
	async listConnections() { return []; },
	async createConnection() { return { outcome: "failed", failure: { code: "desktop.mcp_disabled", retryable: false, recovery: "configure_adapter" } }; },
	async revokeConnection() { return { outcome: "failed", failure: { code: "desktop.mcp_disabled", retryable: false, recovery: "configure_adapter" } }; },
});
