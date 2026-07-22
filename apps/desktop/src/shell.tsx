// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { CapabilityID } from "@layerdraw/protocol/common";
import {
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
  type RefObject,
} from "react";
import type { DesktopProjectContext, DesktopFeatureAvailability, DesktopMCPPort, DesktopProjectDialogPort, DesktopRecentProjectDTO, DesktopSettingsPort } from "./contracts.js";
import { DesktopSettingsDialog } from "./settings-dialog.js";
import { DesktopShellController } from "./controller.js";
import type { DesktopEditorCapabilityIDs } from "./editor-surface.js";
import { DesktopViewerSurface } from "./viewer-surface.js";
import { ReviewPanel } from "@layerdraw/react/review";
import { baseShellCatalogs, createTranslator, useOptionalI18n, type Translator } from "@layerdraw/react/i18n";
import { Button, Tab, TabsList, TabsRoot } from "@layerdraw/react/primitives";
import type { ReviewModel } from "@layerdraw/review";
import type { ViewerState } from "@layerdraw/viewer";
import type { LibraryController, LibrarySnapshot } from "@layerdraw/library";
import { DesktopLibraryPanel } from "./library-panel.js";
import { LayerDrawIcon, LayerDrawWordmark } from "./brand.js";

/**
 * Fallback translator so the shell renders correctly when no I18nProvider is
 * present (unit tests). The real frontend wraps the shell in an I18nProvider
 * that follows the OS locale; switching locale never touches this component.
 */
const defaultTranslator: Translator = createTranslator("en", baseShellCatalogs);

/**
 * Present a project display name, refusing to surface internal identifiers.
 * Legacy recents whose stored name is a raw `doc_…`/`revision_…` identifier
 * render as the localized untitled label instead (internal IDs belong to
 * diagnostics affordances only).
 */
export function presentProjectName(t: Translator, displayName: string | undefined): string {
  const trimmed = displayName?.trim() ?? "";
  if (trimmed === "" || /^(doc|revision|session|project)_[0-9a-zA-Z]/u.test(trimmed)) return t.t("hub.recent.untitled");
  return trimmed;
}

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
  /** Persisted application settings; enables the Settings window (menu Settings…). */
  readonly settings?: DesktopSettingsPort;
  /** Applies a UI locale override chosen in Settings ("system" = OS-follow). */
  readonly onLocaleChange?: (locale: string) => void;
  /**
   * Returns from the open workspace to the project hub without a process
   * restart. When omitted the breadcrumb renders as a non-interactive location
   * indicator (the host close binding is wired separately).
   */
  readonly onReturnToProjects?: () => void;
}

function statusChip(kind: string, text: string): ReactNode {
  return <span className="ld-desktop-chip" data-status={kind} key={kind}>{text}</span>;
}

/**
 * Only abnormal states surface as chips; healthy storage/access/persistence
 * stay silent and internal identifiers (revision hashes) never render.
 */
function renderAbnormalStatuses(t: Translator, project: DesktopProjectContext): ReactNode {
  const chips: ReactNode[] = [];
  if (project.storage.status === "conflict") chips.push(statusChip("conflict", t.t("workspace.status.conflict")));
  if (project.storage.status === "reconcile_pending") chips.push(statusChip("reconcile_pending", t.t("workspace.status.reconcile_pending")));
  if (project.access.status === "denied") chips.push(statusChip("denied", t.t("workspace.status.denied")));
  if (chips.length === 0) return null;
  return <div className="ld-desktop-statuses" aria-label={t.t("workspace.inspector")}>{chips}</div>;
}

type EditorMode = "structure" | "views";

export function DesktopShell({ controller, viewSelectionCapability, editorCapabilities, reviewModel, library, libraryAvailability, reviewAvailability, mcp, projectDialogs, settings, onLocaleChange, onReturnToProjects }: DesktopShellProps): ReactNode {
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
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsPane, setSettingsPane] = useState<"general" | "mcp_defaults" | "agent_access">("general");
  const [hubPage, setHubPage] = useState<"projects" | "library">("projects");
  const [reviewOpen, setReviewOpen] = useState(false);
  const project = state.lifecycle.project;
  const capability = state.lifecycle.capabilities[viewSelectionCapability];
  const viewSelectionAvailable = capability?.status === "available";

  useEffect(() => {
    controller.start();
    return () => { void controller.stop(); };
  }, [controller]);
  useEffect(() => {
    if (typeof window === "undefined") return;
    const onMenu = (event: Event): void => {
      const command = (event as CustomEvent).detail;
      if (settings !== undefined) {
        if (command === "settings") { setSettingsPane("general"); setSettingsOpen(true); }
        if (command === "panel:mcp") { setSettingsPane(controller.getSnapshot().lifecycle.project === undefined ? "mcp_defaults" : "agent_access"); setSettingsOpen(true); }
      }
      if (command === "panel:review") setReviewOpen(true);
    };
    window.addEventListener("layerdraw:menu", onMenu);
    return () => window.removeEventListener("layerdraw:menu", onMenu);
  }, [settings, controller]);
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
    return (
      <main className="ld-desktop-shell ld-desktop-boot" aria-label={t.t("app.name")}>
        <div className="ld-desktop-boot-mark"><LayerDrawWordmark title={t.t("app.name")} /></div>
        <p role="status" aria-live="polite">{t.t("status.starting")}</p>
      </main>
    );
  }
  if (state.lifecycle.phase === "recovery") {
    return (
      <main className="ld-desktop-shell ld-desktop-boot" aria-label={t.t("app.name")}>
        <div className="ld-desktop-boot-mark"><LayerDrawWordmark title={t.t("app.name")} /></div>
        <p role="alert">{t.t("recovery.title")}</p>
        <button type="button" className="ld-btn ld-btn-primary" disabled={state.pending_action !== undefined} onClick={() => { void controller.reviewRecovery(); }}>{t.t("recovery.action")}</button>
      </main>
    );
  }
  if (state.lifecycle.phase === "draining" || state.lifecycle.phase === "stopped") {
    return (
      <main className="ld-desktop-shell ld-desktop-boot" aria-label={t.t("app.name")}>
        <p role="status">{t.t("status.closing")}</p>
      </main>
    );
  }

  const settingsDialog = !settingsOpen || settings === undefined ? null : (
    <DesktopSettingsDialog
      settings={settings}
      initialPane={settingsPane}
      {...(mcp === undefined ? {} : { mcp })}
      {...(project === undefined ? {} : { projectName: project.display_name, projectID: project.project_id })}
      onClose={() => setSettingsOpen(false)}
      {...(onLocaleChange === undefined ? {} : { onLocaleChange })}
    />
  );

  if (project === undefined) return (
    <DesktopHub
      t={t}
      projectDialogs={projectDialogs}
      dialogPending={dialogPending}
      dialogFailure={dialogFailure}
      detailsOpen={detailsOpen}
      setDetailsOpen={setDetailsOpen}
      recentProjects={recentProjects}
      failure={failure}
      library={library}
      runProjectDialog={runProjectDialog}
      openRecentProject={openRecentProject}
      settingsDialog={settingsDialog}
      hubPage={hubPage}
      setHubPage={setHubPage}
      {...(libraryAvailability === undefined ? {} : { libraryAvailability })}
    />
  );

  const viewList = (
    <ul className="ld-rail-list">
      {project.views.map((view) => (
        <li key={view.address}>
          <button
            type="button"
            className="ld-rail-item"
            disabled={!viewSelectionAvailable || state.pending_action !== undefined}
            aria-current={view.address === project.selected_view_address ? "page" : undefined}
            aria-label={!viewSelectionAvailable ? `${view.label}. ${t.t("status.unavailable")}` : view.label}
            onClick={() => { void controller.selectView(view.address); }}
          >
            {shapeGlyph(view.shape)}
            <span className="ld-rail-item-label">{view.label}</span>
          </button>
        </li>
      ))}
    </ul>
  );

  const structurePlaceholder = <p className="ld-rail-empty">{t.t("workspace.structure.empty")}</p>;
  const selectedView = project.views.find((view) => view.address === project.selected_view_address);
  const persistenceLabel = t.t(`workspace.persistence.${project.persistence}`);

  return (
    <main className="ld-desktop-shell ld-desktop-app" aria-label={t.t("app.name")}>
      <header className="ld-workspace-topbar">
        <div className="ld-workspace-lead">
          <span className="ld-workspace-brand"><LayerDrawWordmark title={t.t("app.name")} /></span>
          {renderBreadcrumb(t, project.display_name, heading, onReturnToProjects)}
        </div>
        <div className="ld-workspace-trail">
          {renderAbnormalStatuses(t, project)}
          <span className="ld-workspace-save" role="status">{persistenceLabel}</span>
        </div>
      </header>
      {failure === null ? null : <div role={state.failure?.recoverable === true ? "status" : "alert"} className="ld-banner ld-banner-warning">{failure}</div>}
      <div className="ld-desktop-workspace">
        <nav className="ld-desktop-sidebar" aria-label={t.t("workspace.mode.label")}>
          {renderModeSwitch(t, editorMode, setEditorMode)}
          <h2 className="ld-panehead">{editorMode === "views" ? t.t("workspace.views") : t.t("workspace.pane.layers")}</h2>
          {editorMode === "views" ? viewList : structurePlaceholder}
        </nav>
        <section className="ld-desktop-canvas" aria-label={t.t("workspace.canvas")} data-mode={editorMode}>
          {editorMode === "views"
            ? <DesktopViewerSurface state={state.viewer} onSelectionChange={(keys) => controller.setViewerSelection(keys)} />
            : <p className="ld-desktop-empty">{t.t("workspace.structure.empty")}</p>}
        </section>
        <aside className="ld-desktop-inspector" aria-label={t.t("workspace.inspector")}>
          {selectedView === undefined ? null : (
            <header className="ld-insp-head">
              <span className="ld-insp-kind">{t.t("workspace.kind.view")}</span>
              <h2>{selectedView.label}</h2>
            </header>
          )}
          {project.storage.kind === "external" ? inspectorSection(t.t("inspector.section.storage"), true, (
            <section className="ld-desktop-storage" aria-label={t.t("inspector.section.storage")}>
              <dl>
                <div><dt>{t.t("storage.provider")}</dt><dd>{project.storage.provider_label ?? project.storage.label}</dd></div>
                <div><dt>{t.t("storage.account")}</dt><dd>{project.storage.account_label ?? t.t("storage.unavailable")}</dd></div>
                <div><dt>{t.t("storage.scope")}</dt><dd>{project.storage.scope_label ?? t.t("storage.unavailable")}</dd></div>
                <div><dt>{t.t("storage.lastSync")}</dt><dd>{project.storage.last_sync_label ?? t.t("storage.never")}</dd></div>
                <div><dt>{t.t("storage.pending")}</dt><dd>{String(project.storage.pending_changes ?? 0)}</dd></div>
              </dl>
              {project.storage.status === "conflict" || project.storage.status === "reconcile_pending"
                ? <p role="status" className="ld-desktop-storage-warning">{t.t("storage.review")}</p> : null}
              <p className="ld-desktop-storage-consequence">{project.storage.disconnect_consequence ?? t.t("storage.consequence")}</p>
              <button type="button" className="ld-btn ld-btn-danger-quiet" disabled={state.pending_action !== undefined} onClick={() => { void controller.disconnectExternal(); }}>{t.t("storage.disconnect")}</button>
            </section>
          )) : null}
        </aside>
      </div>
      {renderStatusbar(t, state.viewer, selectedView?.label)}
      {!reviewOpen || reviewModel === undefined ? null : (
        <aside className="ld-review-sheet" role="dialog" aria-label={t.t("inspector.section.review")}>
          <header className="ld-review-sheet-head">
            <h2>{t.t("inspector.section.review")}</h2>
            <button type="button" className="ld-settings-close" aria-label={t.t("settings.close")} onClick={() => setReviewOpen(false)}>×</button>
          </header>
          <ReviewPanel model={reviewModel} />
        </aside>
      )}
      {settingsDialog}
      <div className="ld-desktop-visually-hidden" role="status" aria-live="polite" aria-atomic={true}>{state.pending_action === undefined ? "" : t.t(`workspace.pending.${state.pending_action}`)}</div>
    </main>
  );
}

function renderBreadcrumb(t: Translator, projectName: string, heading: RefObject<HTMLHeadingElement | null>, onReturnToProjects: (() => void) | undefined): ReactNode {
  const back = onReturnToProjects === undefined
    ? <span className="ld-breadcrumb-root">{t.t("workspace.back")}</span>
    : (
      <button type="button" className="ld-breadcrumb-back" onClick={() => onReturnToProjects()}>
        <span aria-hidden={true}>{"‹ "}</span>{t.t("workspace.back")}
      </button>
    );
  return (
    <nav className="ld-breadcrumb" aria-label={t.t("workspace.back")}>
      {back}
      <span className="ld-breadcrumb-sep" aria-hidden={true}>/</span>
      <h1 ref={heading} tabIndex={-1} className="ld-breadcrumb-current">{projectName}</h1>
    </nav>
  );
}

function renderModeSwitch(t: Translator, mode: EditorMode, setMode: (mode: EditorMode) => void): ReactNode {
  return (
    <div className="ld-modeswitch">
      <TabsRoot value={mode} onValueChange={(value: unknown) => setMode(value as EditorMode)}>
        <TabsList aria-label={t.t("workspace.mode.label")}>
          <Tab value="structure">{t.t("workspace.mode.structure")}</Tab>
          <Tab value="views">{t.t("workspace.mode.views")}</Tab>
        </TabsList>
      </TabsRoot>
    </div>
  );
}

const shapeGlyphPaths: Readonly<Record<string, string>> = {
  diagram: "M2 2h5v5H2zM9 9h5v5H9zM7 4.5h4.5V9",
  table: "M2 2.5h12v11H2zM2 6h12M6.5 6v7.5M11 6v7.5",
  matrix: "M2 13V8m4 5V5m4 8V9m4 4V3",
  tree: "M8 2v4M8 6H4v4M8 6h4v4M4 10v4M12 10v4",
  flow: "M2 8h4m4 0h4M6 5.5h4v5H6z",
  context: "M8 2a6 6 0 1 1 0 12A6 6 0 0 1 8 2zM8 5v3l2 2",
  diff: "M6 2v12M10 2v12M2 5h4m4 6h4",
};

/** Compact shape glyph for the view list (one per view shape family). */
function shapeGlyph(shape: string): ReactNode {
  return (
    <svg viewBox="0 0 16 16" width={14} height={14} fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" aria-hidden={true}>
      <path d={shapeGlyphPaths[shape] ?? "M2 2.5h12v11H2z"} />
    </svg>
  );
}

function renderStatusbar(t: Translator, viewer: ViewerState, viewLabel: string | undefined): ReactNode {
  const publication = viewer.status === "ready" ? viewer.publication : undefined;
  const attention = !["ready", "loading", "cancelling", "empty", "disposed"].includes(viewer.status);
  const counts: string[] = [];
  if (publication !== undefined) {
    const data = publication.render_data;
    if (data.kind === "diagram") counts.push(t.t("workspace.statusbar.entities", { count: String(data.occurrences.length) }), t.t("workspace.statusbar.relations", { count: String(data.edge_paths.length) }));
    if (data.kind === "table") counts.push(t.t("workspace.statusbar.entities", { count: String(data.rows.length) }));
    if (data.kind === "tree") counts.push(t.t("workspace.statusbar.entities", { count: String(data.occurrences.length) }));
  }
  return (
    <footer className="ld-statusbar">
      <span className={attention ? "ld-statusbar-attention" : "ld-statusbar-ok"}>{attention ? t.t("workspace.statusbar.attention") : `✓ ${t.t("workspace.statusbar.ok")}`}</span>
      {viewLabel === undefined ? null : <span>{[t.t("workspace.statusbar.view", { name: viewLabel }), ...counts].join(" · ")}</span>}
      <span className="ld-statusbar-spacer" />
      <span>{t.t("workspace.statusbar.undo")}</span>
    </footer>
  );
}

/** Lucide folder icon (product system icon family). */
function folderIcon(): ReactNode {
  return (
    <svg viewBox="0 0 24 24" width={16} height={16} fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" aria-hidden={true}>
      <path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z" />
    </svg>
  );
}

/** Lucide library icon (product system icon family). */
function libraryIcon(): ReactNode {
  return (
    <svg viewBox="0 0 24 24" width={16} height={16} fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" aria-hidden={true}>
      <path d="m16 6 4 14" /><path d="M12 6v14" /><path d="M8 8v12" /><path d="M4 4v16" />
    </svg>
  );
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
  readonly settingsDialog?: ReactNode;
  readonly hubPage: "projects" | "library";
  readonly setHubPage: (page: "projects" | "library") => void;
  readonly libraryAvailability?: DesktopFeatureAvailability;
}


function inspectorSection(label: string, open: boolean, children: ReactNode): ReactNode {
  return (
    <details className="ld-inspector-section" open={open}>
      <summary>{label}</summary>
      <div className="ld-inspector-section-body">{children}</div>
    </details>
  );
}

function DesktopHub({ t, projectDialogs, dialogPending, dialogFailure, detailsOpen, setDetailsOpen, recentProjects, failure, library, runProjectDialog, openRecentProject, settingsDialog, hubPage, setHubPage, libraryAvailability }: HubProps): ReactNode {
  const [dismissed, setDismissed] = useState<string>();
  const [librarySnapshot, setLibrarySnapshot] = useState<LibrarySnapshot>();

  useEffect(() => {
    if (library === undefined) return;
    let cancelled = false;
    void library.refreshSources().then(async (snapshot) => {
      if (cancelled) return;
      if (!snapshot.sources.some((source) => source.connected)) { setLibrarySnapshot(snapshot); return; }
      const results = await library.search("", "template");
      if (!cancelled) setLibrarySnapshot(results);
    }, () => {});
    return () => { cancelled = true; library.cancel(); };
  }, [library]);

  const rail = (
    <aside className="ld-rail" aria-label={t.t("nav.section")}>
      <div className="ld-rail-brand"><LayerDrawWordmark title={t.t("app.name")} /></div>
      <nav className="ld-rail-nav" aria-label={t.t("nav.section")}>
        <button type="button" className={`ld-rail-item${hubPage === "projects" ? " ld-rail-item-active" : ""}`} aria-current={hubPage === "projects" ? "page" : undefined} onClick={() => setHubPage("projects")}>{folderIcon()}{t.t("nav.projects")}</button>
        <button type="button" className={`ld-rail-item${hubPage === "library" ? " ld-rail-item-active" : ""}`} aria-current={hubPage === "library" ? "page" : undefined} onClick={() => setHubPage("library")}>{libraryIcon()}{t.t("nav.library")}</button>
      </nav>
    </aside>
  );

  const actions = projectDialogs === undefined ? null : (
    <div className="ld-hub-actions" aria-label={t.t("hub.actions.label")}>
      <Button variant="secondary" disabled={dialogPending !== undefined} onClick={() => runProjectDialog("open")}>{dialogPending === "open" ? t.t("hub.action.opening") : t.t("hub.action.open")}</Button>
      <Button variant="primary" disabled={dialogPending !== undefined} onClick={() => runProjectDialog("create")}>{dialogPending === "create" ? t.t("hub.action.creating") : t.t("hub.action.new")}</Button>
    </div>
  );

  const activeFailure = dialogFailure ?? (failure === null ? undefined : failure);
  const bannerVisible = activeFailure !== undefined && dismissed !== activeFailure;
  const errorBanner = !bannerVisible ? null : (
    <div role="alert" className="ld-banner ld-banner-danger">
      <svg className="ld-banner-icon" viewBox="0 0 24 24" aria-hidden={true}>
        <path d="M12 9v4" /><path d="M12 17h.01" />
        <path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3l-8.5-14.1a2 2 0 0 0-3.4 0Z" />
      </svg>
      <div className="ld-banner-body">
        <p className="ld-banner-reason">
          <b>{t.t("hub.error.title")}</b>
          {" "}
          {dialogFailure === undefined ? failure : t.error(dialogFailure)}
        </p>
        {dialogFailure === undefined || !detailsOpen ? null : <p className="ld-banner-details"><code>{dialogFailure}</code></p>}
      </div>
      {dialogFailure === undefined ? null : <button type="button" className="ld-banner-toggle" aria-expanded={detailsOpen} onClick={() => setDetailsOpen(!detailsOpen)}>{detailsOpen ? t.t("hub.error.hideDetails") : t.t("hub.error.showDetails")}</button>}
      <button type="button" className="ld-banner-dismiss" aria-label={t.t("hub.error.dismiss")} onClick={() => setDismissed(activeFailure)}>×</button>
    </div>
  );

  const recent = projectDialogs === undefined ? null : (
    <section className="ld-hub-recent" aria-label={t.t("hub.recent.title")}>
      <h2 className="ld-sec-label">{t.t("hub.recent.title")}</h2>
      {recentProjects.length === 0
        ? <p className="ld-hub-empty">{t.t("hub.recent.empty")}</p>
        : <ul className="ld-recent-list">{recentProjects.map((entry) => renderRecentRow(t, entry, dialogPending, openRecentProject))}</ul>}
    </section>
  );

  const templateResults = librarySnapshot?.results.filter((release) => release.identity.kind === "template") ?? [];
  const librarySourcesConnected = librarySnapshot?.sources.some((source) => source.connected) === true;
  const templates = (
    <section className="ld-hub-templates" aria-label={t.t("hub.templates.title")}>
      <h2 className="ld-sec-label">{t.t("hub.templates.title")}</h2>
      <div className="ld-template-cards">
        {templateResults.map((release) => (
          <button
            type="button"
            key={`${release.identity.canonical_id}:${release.identity.version}`}
            className="ld-template-card"
            disabled={dialogPending !== undefined}
            onClick={() => runProjectDialog("create")}
          >
            <span className="ld-template-glyph" aria-hidden={true}><LayerDrawIcon title="" size={15} /></span>
            <b>{release.identity.canonical_id}</b>
            <span className="ld-template-src">{`${release.identity.version} · ${release.source_id}`}</span>
          </button>
        ))}
        <button type="button" className="ld-template-blank" disabled={dialogPending !== undefined} onClick={() => runProjectDialog("create")}>{t.t("hub.templates.blank")}</button>
      </div>
      {librarySourcesConnected ? null : <p className="ld-hub-templates-hint">{t.t("hub.templates.hint")}</p>}
    </section>
  );

  const projectsMain = (
    <div className="ld-hub-main">
      <header className="ld-hub-header">
        <h1>{t.t("hub.title")}</h1>
        {actions}
      </header>
      {errorBanner}
      {recent}
      {templates}
    </div>
  );
  const libraryMain = (
    <div className="ld-hub-main">
      <header className="ld-hub-header">
        <h1>{t.t("nav.library")}</h1>
      </header>
      {library === undefined
        ? <p className="ld-hub-empty">{libraryAvailability !== undefined && libraryAvailability.status === "unavailable" ? t.error(`desktop.library_${libraryAvailability.reason}`) : t.t("status.unavailable")}</p>
        : <DesktopLibraryPanel library={library} />}
    </div>
  );
  return (
    <main className="ld-desktop-shell ld-desktop-hub" aria-label={t.t("app.name")}>
      {rail}
      {hubPage === "library" ? libraryMain : projectsMain}
      {settingsDialog ?? null}
    </main>
  );
}

function renderRecentRow(t: Translator, entry: DesktopRecentProjectDTO, dialogPending: string | undefined, openRecentProject: (projectID: string) => void): ReactNode {
  const missing = entry.availability === "missing";
  const name = presentProjectName(t, entry.display_name);
  const location = typeof entry["location_label"] === "string" ? entry["location_label"] : undefined;
  const opened = typeof entry.last_opened_at === "string" ? t.t("hub.recent.opened", { when: t.formatRelativeTime(entry.last_opened_at) }) : "";
  return (
    <li key={entry.project_id}>
      <button
        type="button"
        className="ld-recent-row"
        disabled={dialogPending !== undefined || missing}
        aria-label={missing ? `${name}. ${t.t("hub.recent.missing")}` : name}
        onClick={() => openRecentProject(entry.project_id)}
      >
        <span className="ld-recent-fileicon" aria-hidden={true}><LayerDrawIcon title="" size={16} /></span>
        <span className="ld-recent-meta">
          <span className="ld-recent-name">{name}</span>
          {location === undefined ? null : <span className="ld-recent-path">{location}</span>}
        </span>
        {missing
          ? <span className="ld-recent-badge" data-status="missing">{t.t("hub.recent.missing")}</span>
          : <time className="ld-recent-when">{opened}</time>}
        <span className="ld-recent-chev" aria-hidden={true}>›</span>
      </button>
    </li>
  );
}
