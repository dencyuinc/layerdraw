// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { LibraryController, LibraryProjectContext, LibrarySnapshot } from "@layerdraw/library";
import type { RegistryAction, RegistryArtifactKind, RegistrySourceKind } from "@layerdraw/registry-client";
import { baseShellCatalogs, createTranslator, useOptionalI18n, type Translator } from "@layerdraw/react";
import { createElement, useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";

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
  return createElement("svg", { viewBox: "0 0 24 24", width: 15, height: 15, fill: "none", stroke: "currentColor", strokeWidth: 1.8, strokeLinecap: "round", strokeLinejoin: "round", "aria-hidden": true },
    createElement("circle", { cx: 11, cy: 11, r: 7 }), createElement("path", { d: "m21 21-4.3-4.3" }));
}

function packGlyph(kind: RegistryArtifactKind): ReactNode {
  const path = kind === "template"
    ? "M4 4h16v5H4zM4 12h7v8H4zM14 12h6v8h-6z"
    : "M12 2 3 7v10l9 5 9-5V7zM3 7l9 5m0 0 9-5m-9 5v10";
  return createElement("svg", { viewBox: "0 0 24 24", width: 18, height: 18, fill: "none", stroke: "currentColor", strokeWidth: 1.6, strokeLinecap: "round", strokeLinejoin: "round", "aria-hidden": true },
    createElement("path", { d: path }));
}

function kindChip(t: Translator, value: "" | RegistryArtifactKind, active: "" | RegistryArtifactKind, busy: boolean, select: (kind: "" | RegistryArtifactKind) => void): ReactNode {
  const labels: Record<string, string> = { "": t.t("library.kind.all"), pack: t.t("library.kind.packs"), template: t.t("library.kind.templates") };
  return createElement("button", {
    key: value === "" ? "all" : value, type: "button", className: "ld-library-chip",
    "aria-pressed": value === active, disabled: busy, onClick: () => select(value),
  }, labels[value]);
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

  const searchRow = createElement("form", { className: "ld-library-searchrow", onSubmit: search, "aria-label": t.t("library.browse") },
    createElement("div", { className: "ld-library-searchbox" },
      searchIcon(),
      createElement("input", {
        type: "search", value: query, disabled: busy, placeholder: t.t("library.searchPlaceholder"),
        "aria-label": t.t("library.search"), onChange: (event) => setQuery(event.currentTarget.value),
      })),
    createElement("div", { className: "ld-library-chips", role: "group", "aria-label": t.t("library.kind") },
      ([ "", "pack", "template" ] as const).map((value) => kindChip(t, value, kind, busy, selectKind))),
    createElement("button", { type: "submit", className: "ld-btn ld-btn-primary", disabled: busy }, t.t("library.browse")));

  const results = snapshot.results.length === 0
    ? (searched && !busy ? createElement("p", { className: "ld-hub-empty" }, t.t("library.empty.noResults")) : null)
    : createElement("div", { className: "ld-library-grid", "aria-label": t.t("library.results") }, snapshot.results.map((release) =>
      createElement("button", {
        type: "button",
        key: `${release.identity.kind}:${release.identity.canonical_id}:${release.identity.version}`,
        className: "ld-library-card",
        disabled: busy,
        "aria-pressed": selected?.digest === release.digest,
        onClick: () => setSnapshot(library.select(release.identity)),
      },
        createElement("span", { className: "ld-library-card-glyph", "aria-hidden": true }, packGlyph(release.identity.kind)),
        createElement("b", null, release.identity.canonical_id),
        createElement("span", { className: "ld-library-card-meta" }, `${release.identity.version} · ${release.identity.kind === "template" ? t.t("library.kind.templates") : t.t("library.kind.packs")}`),
        createElement("small", null, `${release.publisher_id} · ${release.source_id}`))));

  const selection = selected === undefined ? null : createElement("section", { className: "ld-library-detail", "aria-label": t.t("library.selected.title") },
    createElement("div", { className: "ld-library-detail-head" },
      createElement("span", { className: "ld-insp-kind" }, selected.identity.kind === "template" ? t.t("library.kind.templates") : t.t("library.kind.packs")),
      createElement("h3", null, selected.identity.canonical_id),
      createElement("span", { className: "ld-library-card-meta" }, `${selected.identity.version} · ${selected.license}`)),
    selected.identity.kind === "pack"
      ? createElement("div", { className: "ld-library-detail-action" },
        createElement("span", null, t.t("library.action.label")),
        createElement("div", { className: "ld-library-chips" },
          (["install", "update"] as const).map((value) => createElement("button", {
            key: value, type: "button", className: "ld-library-chip", "aria-pressed": action === value, disabled: busy, onClick: () => setAction(value),
          }, t.t(`library.action.${value}`)))))
      : createElement("p", { className: "ld-hub-templates-hint" }, t.t("library.template.hint")),
    createElement("button", {
      type: "button", className: "ld-btn ld-btn-primary", disabled: busy || (selected.identity.kind === "pack" && project === undefined),
      onClick: () => { void update(library.preview(selectedAction, project)); },
    }, t.t("library.preview")));

  const plan = snapshot.plan === undefined || snapshot.status !== "awaiting_confirmation" ? null : createElement("section", { className: "ld-library-detail", "aria-label": t.t("library.plan.title") },
    createElement("h3", null, t.t("library.plan.title")),
    createElement("p", { className: "ld-library-card-meta" }, `${snapshot.plan.action.replaceAll("_", " ")} · ${t.t("library.plan.artifacts", { count: String(snapshot.plan.artifacts.length) })}`),
    createElement("p", { className: "ld-hub-templates-hint" }, snapshot.plan.migration_required ? t.t("library.plan.migration") : t.t("library.plan.noMigration")),
    createElement("button", { type: "button", className: "ld-btn ld-btn-primary", disabled: busy, onClick: () => { const id = `desktop-library-${Date.now()}-${++operationSequence.current}`; void update(library.confirm(id, id)); } }, t.t("library.plan.apply")));

  const recover = snapshot.transaction === undefined || snapshot.status !== "recoverable_error" ? null : createElement("button", {
    type: "button", className: "ld-btn ld-btn-secondary", disabled: busy,
    onClick: () => { void update(library.recoverTransaction(snapshot.transaction!.plan.transaction_id)); },
  }, t.t("library.recover"));

  const sources = createElement("section", { className: "ld-library-sourcecard", "aria-label": t.t("library.sources") },
    createElement("h2", { className: "ld-sec-label" }, t.t("library.sources")),
    createElement("div", { className: "ld-settings-card" },
      snapshot.sources.length === 0 ? null : snapshot.sources.map((source) => createElement("div", { key: source.source_id, className: "ld-settings-row" },
        createElement("span", { className: "ld-settings-row-label" }, source.source_id,
          createElement("small", null, source.kind.replaceAll("_", " "))),
        createElement("span", { className: "ld-settings-row-control" },
          createElement("span", { className: "ld-settings-badge", "data-on": source.connected }, source.connected ? t.t("library.source.connected") : t.t("library.source.disconnected")),
          source.connected
            ? createElement("button", { type: "button", className: "ld-btn ld-btn-secondary", disabled: busy, onClick: () => { void update(library.disconnectSource(source.source_id)); } }, t.t("library.source.disconnect"))
            : createElement("button", { type: "button", className: "ld-btn ld-btn-secondary", disabled: busy || connection.trim() === "", onClick: () => { void update(library.connectSource(source.source_id, connection.trim())); } }, t.t("library.source.connect"))))),
      snapshot.sources.some((source) => !source.connected) ? createElement("div", { className: "ld-settings-row" },
        createElement("span", { className: "ld-settings-row-label" }, t.t("library.source.connectionRef")),
        createElement("span", { className: "ld-settings-row-control" },
          createElement("input", { className: "ld-settings-input", value: connection, disabled: busy, "aria-label": t.t("library.source.connectionRef"), onChange: (event) => setConnection(event.currentTarget.value) }))) : null,
      addOpen
        ? createElement("form", { onSubmit: configure, "aria-label": t.t("library.source.add") },
          createElement("div", { className: "ld-settings-row" },
            createElement("span", { className: "ld-settings-row-label" }, t.t("library.source.id")),
            createElement("span", { className: "ld-settings-row-control" }, createElement("input", { className: "ld-settings-input", value: sourceID, disabled: busy, "aria-label": t.t("library.source.id"), onChange: (event) => setSourceID(event.currentTarget.value) }))),
          createElement("div", { className: "ld-settings-row" },
            createElement("span", { className: "ld-settings-row-label" }, t.t("library.source.kind")),
            createElement("span", { className: "ld-settings-row-control" }, createElement("select", { className: "ld-library-select", value: sourceKind, disabled: busy, "aria-label": t.t("library.source.kind"), onChange: (event) => setSourceKind((event.currentTarget as HTMLSelectElement).value as RegistrySourceKind) }, sourceKinds.map((value) => createElement("option", { value, key: value }, value.replaceAll("_", " "))))),
          ),
          createElement("div", { className: "ld-settings-row" },
            createElement("span", { className: "ld-settings-row-label" }, t.t("library.source.endpoint")),
            createElement("span", { className: "ld-settings-row-control" }, createElement("input", { className: "ld-settings-input", value: endpoint, disabled: busy, "aria-label": t.t("library.source.endpoint"), onChange: (event) => setEndpoint(event.currentTarget.value) }))),
          createElement("div", { className: "ld-settings-formfoot" },
            createElement("button", { type: "submit", className: "ld-btn ld-btn-primary", disabled: busy || sourceID.trim() === "" || endpoint.trim() === "" }, t.t("library.source.add"))))
        : createElement("div", { className: "ld-settings-formfoot" },
          createElement("button", { type: "button", className: "ld-btn ld-btn-secondary", disabled: busy, onClick: () => setAddOpen(true) }, `＋ ${t.t("library.source.add")}`))));

  return createElement("section", { className: "ld-library-panel", "aria-label": t.t("library.title") },
    snapshot.failure === undefined ? null : createElement("div", { role: "alert", className: "ld-banner ld-banner-danger" },
      createElement("p", { className: "ld-banner-reason" }, t.t("library.failed", { code: snapshot.failure.code }))),
    searchRow,
    !sourcesConnected && snapshot.status === "ready" ? createElement("div", { className: "ld-library-empty" },
      createElement("span", { "aria-hidden": true }, packGlyph("pack")),
      createElement("b", null, t.t("library.empty.noSources")),
      createElement("span", null, t.t("library.empty.noSourcesHint"))) : null,
    results,
    selection,
    plan,
    recover,
    sources);
}
