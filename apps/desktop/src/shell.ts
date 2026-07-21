// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { CapabilityID } from "@layerdraw/protocol/common";
import {
  createElement,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
  type RefObject,
} from "react";
import type { DesktopFeatureAvailability, DesktopMCPPort, DesktopProjectDialogPort, DesktopRecentProjectDTO } from "./contracts.js";
import { DesktopShellController } from "./controller.js";
import { DesktopEditorSurface, type DesktopEditorCapabilityIDs } from "./editor-surface.js";
import { DesktopViewerSurface } from "./viewer-surface.js";
import { ReviewPanel } from "@layerdraw/react/review";
import { baseShellCatalogs, createTranslator, useOptionalI18n, type Translator } from "@layerdraw/react/i18n";
import type { ReviewModel } from "@layerdraw/review";
import type { LibraryController } from "@layerdraw/library";
import { DesktopMCPPanel } from "./mcp-panel.js";
import { DesktopLibraryPanel } from "./library-panel.js";
import { LayerDrawWordmark } from "./brand.js";

/**
 * Fallback translator so the shell renders correctly when no I18nProvider is
 * present (unit tests). The real frontend wraps the shell in an I18nProvider
 * that follows the OS locale; switching locale never touches this component.
 */
const defaultTranslator: Translator = createTranslator("en", baseShellCatalogs);

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
  /**
   * Returns from the open workspace to the project hub without a process
   * restart. When omitted the breadcrumb renders as a non-interactive location
   * indicator (the host close binding is wired separately).
   */
  readonly onReturnToProjects?: () => void;
}

function statusChip(kind: string, text: string): ReactNode {
  return createElement("span", { className: "ld-desktop-chip", "data-status": kind }, text);
}

type EditorMode = "structure" | "views";

export function DesktopShell({ controller, viewSelectionCapability, editorCapabilities, reviewModel, library, libraryAvailability, reviewAvailability, mcp, projectDialogs, onReturnToProjects }: DesktopShellProps): ReactNode {
  const contextTranslator = useOptionalI18n();
  const t = contextTranslator ?? defaultTranslator;
  const state = useSyncExternalStore(controller.subscribe, controller.getSnapshot, controller.getSnapshot);
  const heading = useRef<HTMLHeadingElement>(null);
  const dialogSequence = useRef(0);
  const [dialogPending, setDialogPending] = useState<"create" | "open" | "recent">();
  const [dialogFailure, setDialogFailure] = useState<string>();
  const [detailsOpen, setDetailsOpen] = useState(false);
  const [recentProjects, setRecentProjects] = useState<readonly DesktopRecentProjectDTO[]>([]);
  const [editorMode, setEditorMode] = useState<EditorMode>("views");
  const project = state.lifecycle.project;
  const capability = state.lifecycle.capabilities[viewSelectionCapability];
  const viewSelectionAvailable = capability?.status === "available";

  useEffect(() => {
    controller.start();
    return () => { void controller.stop(); };
  }, [controller]);
  useEffect(() => { heading.current?.focus({ preventScroll: true }); }, [project?.project_id, project?.selected_view_address]);
  const hubVisible = project === undefined && state.lifecycle.phase === "ready";
  useEffect(() => {
    if (!hubVisible || projectDialogs === undefined) return;
    let cancelled = false;
    void projectDialogs.recent().then((result) => {
      if (!cancelled && result.outcome === "success") setRecentProjects(result.value);
    }, () => {});
    return () => { cancelled = true; };
  }, [hubVisible, projectDialogs, dialogPending]);

  const failure = state.failure === undefined ? null : t.t(`error.${state.failure.message_key}`);
  const runProjectDialog = (kind: "create" | "open"): void => {
    if (projectDialogs === undefined || dialogPending !== undefined) return;
    setDialogPending(kind);
    setDialogFailure(undefined);
    const requestID = `desktop-shell-${kind}-${++dialogSequence.current}`;
    void projectDialogs[kind](requestID).then((result) => {
      if (result.outcome === "failed" || result.outcome === "rejected") setDialogFailure(result.failure?.code ?? "desktop.adapter_unavailable");
    }, () => setDialogFailure("desktop.adapter_unavailable")).finally(() => setDialogPending(undefined));
  };
  const openRecentProject = (projectID: string): void => {
    if (projectDialogs === undefined || dialogPending !== undefined) return;
    setDialogPending("recent");
    setDialogFailure(undefined);
    void projectDialogs.openRecent(projectID).then((result) => {
      if (result.outcome === "failed" || result.outcome === "rejected") setDialogFailure(result.failure?.code ?? "desktop.adapter_unavailable");
    }, () => setDialogFailure("desktop.adapter_unavailable")).finally(() => setDialogPending(undefined));
  };

  if (state.lifecycle.phase === "starting") {
    return createElement("main", { className: "ld-desktop-shell ld-desktop-boot", "aria-label": t.t("app.name") },
      createElement("div", { className: "ld-desktop-boot-mark" }, createElement(LayerDrawWordmark, { title: t.t("app.name") })),
      createElement("p", { role: "status", "aria-live": "polite" }, t.t("status.starting")));
  }
  if (state.lifecycle.phase === "recovery") {
    return createElement("main", { className: "ld-desktop-shell ld-desktop-boot", "aria-label": t.t("app.name") },
      createElement("div", { className: "ld-desktop-boot-mark" }, createElement(LayerDrawWordmark, { title: t.t("app.name") })),
      createElement("p", { role: "alert" }, t.t("recovery.title")),
      createElement("button", { type: "button", className: "ld-btn ld-btn-primary", disabled: state.pending_action !== undefined, onClick: () => { void controller.reviewRecovery(); } }, t.t("recovery.action")));
  }
  if (state.lifecycle.phase === "draining" || state.lifecycle.phase === "stopped") {
    return createElement("main", { className: "ld-desktop-shell ld-desktop-boot", "aria-label": t.t("app.name") },
      createElement("p", { role: "status" }, t.t("status.closing")));
  }

  if (project === undefined) return renderHub({ t, projectDialogs, dialogPending, dialogFailure, detailsOpen, setDetailsOpen, recentProjects, failure, library, runProjectDialog, openRecentProject });

  const viewList = createElement("ul", { className: "ld-rail-list" }, project.views.map((view) =>
    createElement("li", { key: view.address },
      createElement("button", {
        type: "button",
        className: "ld-rail-item",
        disabled: !viewSelectionAvailable || state.pending_action !== undefined,
        "aria-current": view.address === project.selected_view_address ? "page" : undefined,
        "aria-label": !viewSelectionAvailable ? `${view.label}. ${t.t("status.unavailable")}` : view.label,
        onClick: () => { void controller.selectView(view.address); },
      }, createElement("span", { className: "ld-rail-item-label" }, view.label), createElement("small", null, view.shape))),
  ));

  const structurePlaceholder = createElement("div", { className: "ld-rail-empty" },
    createElement("p", null, t.t("workspace.mode.structure")),
    createElement("small", null, "—"));

  return createElement("main", { className: "ld-desktop-shell ld-desktop-app", "aria-label": t.t("app.name") },
    createElement("header", { className: "ld-workspace-topbar" },
      createElement("div", { className: "ld-workspace-lead" },
        renderBreadcrumb(t, project.display_name, heading, onReturnToProjects),
        renderModeSwitch(t, editorMode, setEditorMode)),
      createElement("div", { className: "ld-desktop-statuses", "aria-label": t.t("workspace.inspector") },
        statusChip(project.storage.status, project.storage.label), statusChip(project.access.status, project.access.label), statusChip(project.persistence, project.authoritative_revision_label))),
    failure === null ? null : createElement("div", { role: state.failure?.recoverable === true ? "status" : "alert", className: "ld-banner ld-banner-warning" }, failure),
    createElement("div", { className: "ld-desktop-workspace" },
      createElement("nav", { className: "ld-desktop-sidebar", "aria-label": t.t("workspace.views") },
        createElement("h2", null, editorMode === "views" ? t.t("workspace.views") : t.t("workspace.mode.structure")),
        editorMode === "views" ? viewList : structurePlaceholder),
      createElement("section", { className: "ld-desktop-canvas", "aria-label": t.t("workspace.canvas") },
        createElement(DesktopViewerSurface, { state: state.viewer, onSelectionChange: (keys) => controller.setViewerSelection(keys) })),
      createElement("aside", { className: "ld-desktop-inspector", "aria-label": t.t("workspace.inspector") },
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
          createElement("button", { type: "button", className: "ld-btn ld-btn-danger-quiet", disabled: state.pending_action !== undefined, onClick: () => { void controller.disconnectExternal(); } }, "Disconnect")) : null,
        createElement(DesktopEditorSurface, { project, capabilities: editorCapabilities }),
        library === undefined ? null : createElement(DesktopLibraryPanel, { library, project: project.library_project }),
        renderFeatureStatus(libraryAvailability, reviewAvailability),
        reviewModel === undefined ? null : createElement(ReviewPanel, { model: reviewModel }),
        createElement(DesktopMCPPanel, { mcp: mcp ?? unavailableMCP, projectID: project.project_id }))),
    createElement("div", { className: "ld-desktop-visually-hidden", role: "status", "aria-live": "polite", "aria-atomic": true }, state.pending_action === "select_view" ? "Opening view…" : state.pending_action === "review_recovery" ? "Opening recovery options…" : state.pending_action === "disconnect_storage" ? "Disconnecting external storage…" : ""));
}

function renderBreadcrumb(t: Translator, projectName: string, heading: RefObject<HTMLHeadingElement | null>, onReturnToProjects: (() => void) | undefined): ReactNode {
  const back = onReturnToProjects === undefined
    ? createElement("span", { className: "ld-breadcrumb-root" }, t.t("workspace.back"))
    : createElement("button", { type: "button", className: "ld-breadcrumb-back", onClick: () => onReturnToProjects() },
        createElement("span", { "aria-hidden": true }, "‹ "), t.t("workspace.back"));
  return createElement("nav", { className: "ld-breadcrumb", "aria-label": t.t("workspace.back") },
    back,
    createElement("span", { className: "ld-breadcrumb-sep", "aria-hidden": true }, "/"),
    createElement("h1", { ref: heading, tabIndex: -1, className: "ld-breadcrumb-current" }, projectName));
}

function renderModeSwitch(t: Translator, mode: EditorMode, setMode: (mode: EditorMode) => void): ReactNode {
  const option = (value: EditorMode, label: string): ReactNode =>
    createElement("button", {
      type: "button",
      className: "ld-segment",
      "aria-pressed": mode === value,
      onClick: () => setMode(value),
    }, label);
  return createElement("div", { className: "ld-mode-switch", role: "group", "aria-label": t.t("workspace.mode.label") },
    option("structure", t.t("workspace.mode.structure")),
    option("views", t.t("workspace.mode.views")));
}

interface HubProps {
  readonly t: Translator;
  readonly projectDialogs: DesktopProjectDialogPort | undefined;
  readonly dialogPending: "create" | "open" | "recent" | undefined;
  readonly dialogFailure: string | undefined;
  readonly detailsOpen: boolean;
  readonly setDetailsOpen: (open: boolean) => void;
  readonly recentProjects: readonly DesktopRecentProjectDTO[];
  readonly failure: string | null;
  readonly library: LibraryController | undefined;
  readonly runProjectDialog: (kind: "create" | "open") => void;
  readonly openRecentProject: (projectID: string) => void;
}

function renderHub({ t, projectDialogs, dialogPending, dialogFailure, detailsOpen, setDetailsOpen, recentProjects, failure, library, runProjectDialog, openRecentProject }: HubProps): ReactNode {
  const rail = createElement("aside", { className: "ld-rail", "aria-label": t.t("nav.section") },
    createElement("div", { className: "ld-rail-brand" }, createElement(LayerDrawWordmark, { title: t.t("app.name") })),
    createElement("nav", { className: "ld-rail-nav", "aria-label": t.t("nav.section") },
      createElement("span", { className: "ld-rail-item ld-rail-item-active", "aria-current": "page" }, t.t("nav.projects")),
      createElement("span", { className: "ld-rail-item ld-rail-item-muted" }, t.t("nav.library"))));

  const actions = projectDialogs === undefined ? null : createElement("div", { className: "ld-hub-actions", "aria-label": t.t("hub.actions.label") },
    createElement("button", { type: "button", className: "ld-btn ld-btn-primary", disabled: dialogPending !== undefined, onClick: () => runProjectDialog("create") }, dialogPending === "create" ? t.t("hub.action.creating") : t.t("hub.action.new")),
    createElement("button", { type: "button", className: "ld-btn ld-btn-secondary", disabled: dialogPending !== undefined, onClick: () => runProjectDialog("open") }, dialogPending === "open" ? t.t("hub.action.opening") : t.t("hub.action.open")));

  const errorBanner = failure === null && dialogFailure === undefined ? null : createElement("div", { role: "alert", className: "ld-banner ld-banner-danger" },
    createElement("div", { className: "ld-banner-body" },
      createElement("p", { className: "ld-banner-title" }, t.t("hub.error.title")),
      createElement("p", { className: "ld-banner-reason" }, dialogFailure === undefined ? failure : t.error(dialogFailure))),
    dialogFailure === undefined ? null : createElement("button", { type: "button", className: "ld-banner-toggle", "aria-expanded": detailsOpen, onClick: () => setDetailsOpen(!detailsOpen) }, detailsOpen ? t.t("hub.error.hideDetails") : t.t("hub.error.showDetails")),
    dialogFailure === undefined || !detailsOpen ? null : createElement("p", { className: "ld-banner-details" }, createElement("code", null, dialogFailure)));

  const recent = projectDialogs === undefined ? null : createElement("section", { className: "ld-hub-recent", "aria-label": t.t("hub.recent.title") },
    createElement("h2", null, t.t("hub.recent.title")),
    recentProjects.length === 0
      ? createElement("p", { className: "ld-hub-empty" }, t.t("hub.recent.empty"))
      : createElement("ul", { className: "ld-recent-list" }, recentProjects.map((entry) => renderRecentRow(t, entry, dialogPending, openRecentProject))));

  const templates = library === undefined ? null : createElement("section", { className: "ld-hub-templates", "aria-label": t.t("hub.templates.title") },
    createElement("h2", null, t.t("hub.templates.title")),
    createElement(DesktopLibraryPanel, { library }));

  return createElement("main", { className: "ld-desktop-shell ld-desktop-hub", "aria-label": t.t("app.name") },
    rail,
    createElement("div", { className: "ld-hub-main" },
      createElement("header", { className: "ld-hub-header" },
        createElement("div", { className: "ld-hub-heading" },
          createElement("h1", null, t.t("hub.title")),
          createElement("p", null, t.t("hub.subtitle"))),
        actions),
      errorBanner,
      recent,
      templates));
}

function renderRecentRow(t: Translator, entry: DesktopRecentProjectDTO, dialogPending: string | undefined, openRecentProject: (projectID: string) => void): ReactNode {
  const missing = entry.availability === "missing";
  const opened = typeof entry.last_opened_at === "string" ? t.t("hub.recent.opened", { when: t.formatRelativeTime(entry.last_opened_at) }) : "";
  return createElement("li", { key: entry.project_id },
    createElement("button", {
      type: "button",
      className: "ld-recent-row",
      disabled: dialogPending !== undefined || missing,
      "aria-label": missing ? `${entry.display_name}. ${t.t("hub.recent.missing")}` : entry.display_name,
      onClick: () => openRecentProject(entry.project_id),
    },
      createElement("span", { className: "ld-recent-name" }, entry.display_name),
      missing
        ? createElement("span", { className: "ld-recent-badge", "data-status": "missing" }, t.t("hub.recent.missing"))
        : createElement("time", { className: "ld-recent-meta" }, opened)));
}

function renderFeatureStatus(libraryAvailability: DesktopFeatureAvailability | undefined, reviewAvailability: DesktopFeatureAvailability | undefined): ReactNode {
  if (libraryAvailability === undefined && reviewAvailability === undefined) return null;
  return createElement("section", { className: "ld-feature-status", "aria-label": "Desktop capabilities" },
    createElement("h2", null, "Capabilities"),
    createElement("ul", null,
      libraryAvailability === undefined ? null : createElement("li", { "data-status": libraryAvailability.status }, `Library: ${libraryAvailability.status}`),
      reviewAvailability === undefined ? null : createElement("li", { "data-status": reviewAvailability.status }, `Review: ${reviewAvailability.status}`)));
}

const unavailableMCP: DesktopMCPPort = Object.freeze<DesktopMCPPort>({
	async status() { return { enabled: false, transport: "local", instructions: "", generation: 0 }; },
	async setEnabled() { return { outcome: "failed", failure: { code: "desktop.mcp_disabled", retryable: false, recovery: "configure_adapter" } }; },
	async restart() { return { outcome: "failed", failure: { code: "desktop.mcp_disabled", retryable: false, recovery: "configure_adapter" } }; },
	async listConnections() { return []; },
	async createConnection() { return { outcome: "failed", failure: { code: "desktop.mcp_disabled", retryable: false, recovery: "configure_adapter" } }; },
	async revokeConnection() { return { outcome: "failed", failure: { code: "desktop.mcp_disabled", retryable: false, recovery: "configure_adapter" } }; },
});
