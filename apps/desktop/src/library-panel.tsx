// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { LibraryController, LibraryProjectContext, LibrarySnapshot } from "@layerdraw/library";
import type { RegistryAction, RegistryArtifactKind, RegistrySourceKind } from "@layerdraw/registry-client";
import { baseShellCatalogs, createTranslator, useOptionalI18n, type Translator } from "@layerdraw/react";
import { tokenSelect } from "./token-select.js";
import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";

export interface DesktopLibraryPanelProps {
  readonly library: LibraryController;
  readonly project?: LibraryProjectContext;
}

const sourceKinds = ["official", "organization_private", "self_hosted", "local_directory", "git"] as const satisfies readonly RegistrySourceKind[];

function actionFor(kind: RegistryArtifactKind, requested: RegistryAction): RegistryAction {
  return kind === "template" ? "create_from_template" : requested === "update" ? "update" : "install";
}

const defaultTranslator: Translator = createTranslator("en", baseShellCatalogs);

function searchIcon(): ReactNode {
  return (
    <svg viewBox="0 0 24 24" width={15} height={15} fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" aria-hidden={true}>
      <circle cx={11} cy={11} r={7} /><path d="m21 21-4.3-4.3" />
    </svg>
  );
}

function packGlyph(kind: RegistryArtifactKind): ReactNode {
  const path = kind === "template"
    ? "M4 4h16v5H4zM4 12h7v8H4zM14 12h6v8h-6z"
    : "M12 2 3 7v10l9 5 9-5V7zM3 7l9 5m0 0 9-5m-9 5v10";
  return (
    <svg viewBox="0 0 24 24" width={18} height={18} fill="none" stroke="currentColor" strokeWidth={1.6} strokeLinecap="round" strokeLinejoin="round" aria-hidden={true}>
      <path d={path} />
    </svg>
  );
}

function kindChip(t: Translator, value: "" | RegistryArtifactKind, active: "" | RegistryArtifactKind, busy: boolean, select: (kind: "" | RegistryArtifactKind) => void): ReactNode {
  const labels: Record<string, string> = { "": t.t("library.kind.all"), pack: t.t("library.kind.packs"), template: t.t("library.kind.templates") };
  return (
    <button
      key={value === "" ? "all" : value} type="button" className="ld-library-chip"
      aria-pressed={value === active} disabled={busy} onClick={() => select(value)}
    >{labels[value]}</button>
  );
}

export function DesktopLibraryPanel({ library, project }: DesktopLibraryPanelProps): ReactNode {
  const t = useOptionalI18n() ?? defaultTranslator;
  const [snapshot, setSnapshot] = useState<LibrarySnapshot>(() => library.snapshot());
  const [query, setQuery] = useState("");
  const [kind, setKind] = useState<"" | RegistryArtifactKind>("");
  const [action, setAction] = useState<RegistryAction>("install");
  const [addOpen, setAddOpen] = useState(false);
  const [sourceID, setSourceID] = useState("");
  const [sourceKind, setSourceKind] = useState<RegistrySourceKind>("local_directory");
  const [endpoint, setEndpoint] = useState("");
  const [connection, setConnection] = useState("");
  const [searched, setSearched] = useState(false);
  const operationSequence = useRef(0);
  const busy = ["loading", "previewing", "applying"].includes(snapshot.status);
  const update = async (operation: Promise<LibrarySnapshot>): Promise<void> => { setSnapshot(await operation); };

  useEffect(() => {
    void update(library.refreshSources());
    return () => library.cancel();
  }, [library]);

  const runSearch = (nextKind: "" | RegistryArtifactKind): void => {
    setSearched(true);
    void update(library.search(query, nextKind === "" ? undefined : nextKind));
  };
  const search = (event: FormEvent): void => { event.preventDefault(); runSearch(kind); };
  const selectKind = (value: "" | RegistryArtifactKind): void => { setKind(value); runSearch(value); };
  const configure = (event: FormEvent): void => {
    event.preventDefault();
    if (sourceID.trim() === "" || endpoint.trim() === "") return;
    void update(library.configureSource({ source_id: sourceID.trim(), kind: sourceKind, endpoint_ref: endpoint.trim(), trust_policy_id: "desktop-local", cache_policy: "on_demand", priority: 100 }));
  };
  const selected = snapshot.selected;
  const selectedAction = selected === undefined ? action : actionFor(selected.identity.kind, action);
  const sourcesConnected = snapshot.sources.some((source) => source.connected);

  const searchRow = (
    <form className="ld-library-searchrow" onSubmit={search} aria-label={t.t("library.browse")}>
      <div className="ld-library-searchbox">
        {searchIcon()}
        <input
          type="search" value={query} disabled={busy} placeholder={t.t("library.searchPlaceholder")}
          aria-label={t.t("library.search")} onChange={(event) => setQuery(event.currentTarget.value)}
        />
      </div>
      <div className="ld-library-chips" role="group" aria-label={t.t("library.kind")}>
        {(["", "pack", "template"] as const).map((value) => kindChip(t, value, kind, busy, selectKind))}
      </div>
      <button type="submit" className="ld-btn ld-btn-primary" disabled={busy}>{t.t("library.browse")}</button>
    </form>
  );

  const results = snapshot.results.length === 0
    ? (searched && !busy ? <p className="ld-hub-empty">{t.t("library.empty.noResults")}</p> : null)
    : (
      <div className="ld-library-grid" aria-label={t.t("library.results")}>
        {snapshot.results.map((release) => (
          <button
            type="button"
            key={`${release.identity.kind}:${release.identity.canonical_id}:${release.identity.version}`}
            className="ld-library-card"
            disabled={busy}
            aria-pressed={selected?.digest === release.digest}
            onClick={() => setSnapshot(library.select(release.identity))}
          >
            <span className="ld-library-card-glyph" aria-hidden={true}>{packGlyph(release.identity.kind)}</span>
            <b>{release.identity.canonical_id}</b>
            <span className="ld-library-card-meta">{`${release.identity.version} · ${release.identity.kind === "template" ? t.t("library.kind.templates") : t.t("library.kind.packs")}`}</span>
            <small>{`${release.publisher_id} · ${release.source_id}`}</small>
          </button>
        ))}
      </div>
    );

  const selection = selected === undefined ? null : (
    <section className="ld-library-detail" aria-label={t.t("library.selected.title")}>
      <div className="ld-library-detail-head">
        <span className="ld-insp-kind">{selected.identity.kind === "template" ? t.t("library.kind.templates") : t.t("library.kind.packs")}</span>
        <h3>{selected.identity.canonical_id}</h3>
        <span className="ld-library-card-meta">{`${selected.identity.version} · ${selected.license}`}</span>
      </div>
      {selected.identity.kind === "pack" ? (
        <div className="ld-library-detail-action">
          <span>{t.t("library.action.label")}</span>
          <div className="ld-library-chips">
            {(["install", "update"] as const).map((value) => (
              <button
                key={value} type="button" className="ld-library-chip" aria-pressed={action === value} disabled={busy} onClick={() => setAction(value)}
              >{t.t(`library.action.${value}`)}</button>
            ))}
          </div>
        </div>
      ) : (
        <p className="ld-hub-templates-hint">{t.t("library.template.hint")}</p>
      )}
      <button
        type="button" className="ld-btn ld-btn-primary" disabled={busy || (selected.identity.kind === "pack" && project === undefined)}
        onClick={() => { void update(library.preview(selectedAction, project)); }}
      >{t.t("library.preview")}</button>
    </section>
  );

  const plan = snapshot.plan === undefined || snapshot.status !== "awaiting_confirmation" ? null : (
    <section className="ld-library-detail" aria-label={t.t("library.plan.title")}>
      <h3>{t.t("library.plan.title")}</h3>
      <p className="ld-library-card-meta">{`${t.t(`library.action.${snapshot.plan.action}`)} · ${t.t("library.plan.artifacts", { count: String(snapshot.plan.artifacts.length) })}`}</p>
      <p className="ld-hub-templates-hint">{snapshot.plan.migration_required ? t.t("library.plan.migration") : t.t("library.plan.noMigration")}</p>
      <button type="button" className="ld-btn ld-btn-primary" disabled={busy} onClick={() => { const id = `desktop-library-${Date.now()}-${++operationSequence.current}`; void update(library.confirm(id, id)); }}>{t.t("library.plan.apply")}</button>
    </section>
  );

  const recover = snapshot.transaction === undefined || snapshot.status !== "recoverable_error" ? null : (
    <button
      type="button" className="ld-btn ld-btn-secondary" disabled={busy}
      onClick={() => { void update(library.recoverTransaction(snapshot.transaction!.plan.transaction_id)); }}
    >{t.t("library.recover")}</button>
  );

  const sources = (
    <section className="ld-library-sourcecard" aria-label={t.t("library.sources")}>
      <h2 className="ld-sec-label">{t.t("library.sources")}</h2>
      <div className="ld-settings-card">
        {snapshot.sources.length === 0 ? null : snapshot.sources.map((source) => (
          <div key={source.source_id} className="ld-settings-row">
            <span className="ld-settings-row-label">
              {source.source_id}
              <small>{t.t(`library.sourceKind.${source.kind}`)}</small>
            </span>
            <span className="ld-settings-row-control">
              <span className="ld-settings-badge" data-on={source.connected}>{source.connected ? t.t("library.source.connected") : t.t("library.source.disconnected")}</span>
              {source.connected
                ? <button type="button" className="ld-btn ld-btn-secondary" disabled={busy} onClick={() => { void update(library.disconnectSource(source.source_id)); }}>{t.t("library.source.disconnect")}</button>
                : <button type="button" className="ld-btn ld-btn-secondary" disabled={busy || connection.trim() === ""} onClick={() => { void update(library.connectSource(source.source_id, connection.trim())); }}>{t.t("library.source.connect")}</button>}
            </span>
          </div>
        ))}
        {snapshot.sources.some((source) => !source.connected) ? (
          <div className="ld-settings-row">
            <span className="ld-settings-row-label">{t.t("library.source.connectionRef")}</span>
            <span className="ld-settings-row-control">
              <input className="ld-settings-input" value={connection} disabled={busy} aria-label={t.t("library.source.connectionRef")} onChange={(event) => setConnection(event.currentTarget.value)} />
            </span>
          </div>
        ) : null}
        {addOpen ? (
          <form onSubmit={configure} aria-label={t.t("library.source.add")}>
            <div className="ld-settings-row">
              <span className="ld-settings-row-label">{t.t("library.source.id")}</span>
              <span className="ld-settings-row-control"><input className="ld-settings-input" value={sourceID} disabled={busy} aria-label={t.t("library.source.id")} onChange={(event) => setSourceID(event.currentTarget.value)} /></span>
            </div>
            <div className="ld-settings-row">
              <span className="ld-settings-row-label">{t.t("library.source.kind")}</span>
              <span className="ld-settings-row-control">
                {tokenSelect(t.t("library.source.kind"), sourceKind, sourceKinds.map((value) => ({ value, label: t.t(`library.sourceKind.${value}`) })), (value) => setSourceKind(value as RegistrySourceKind))}
              </span>
            </div>
            <div className="ld-settings-row">
              <span className="ld-settings-row-label">{t.t("library.source.endpoint")}</span>
              <span className="ld-settings-row-control"><input className="ld-settings-input" value={endpoint} disabled={busy} aria-label={t.t("library.source.endpoint")} onChange={(event) => setEndpoint(event.currentTarget.value)} /></span>
            </div>
            <div className="ld-settings-formfoot">
              <button type="submit" className="ld-btn ld-btn-primary" disabled={busy || sourceID.trim() === "" || endpoint.trim() === ""}>{t.t("library.source.add")}</button>
            </div>
          </form>
        ) : (
          <div className="ld-settings-formfoot">
            <button type="button" className="ld-btn ld-btn-secondary" disabled={busy} onClick={() => setAddOpen(true)}>{`＋ ${t.t("library.source.add")}`}</button>
          </div>
        )}
      </div>
    </section>
  );

  return (
    <section className="ld-library-panel" aria-label={t.t("library.title")}>
      {snapshot.failure === undefined ? null : (
        <div role="alert" className="ld-banner ld-banner-danger">
          <p className="ld-banner-reason">{t.t("library.failed", { code: snapshot.failure.code })}</p>
        </div>
      )}
      {searchRow}
      {!sourcesConnected && snapshot.status === "ready" ? (
        <div className="ld-library-empty">
          <span aria-hidden={true}>{packGlyph("pack")}</span>
          <b>{t.t("library.empty.noSources")}</b>
          <span>{t.t("library.empty.noSourcesHint")}</span>
        </div>
      ) : null}
      {results}
      {selection}
      {plan}
      {recover}
      {sources}
    </section>
  );
}
