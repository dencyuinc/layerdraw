// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { LibraryController, LibraryProjectContext, LibrarySnapshot } from "@layerdraw/library";
import type { RegistryAction, RegistryArtifactKind, RegistrySourceKind } from "@layerdraw/registry-client";
import { createElement, useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";

export interface DesktopLibraryPanelProps {
  readonly library: LibraryController;
  readonly project?: LibraryProjectContext;
}

const sourceKinds = ["official", "organization_private", "self_hosted", "local_directory", "git"] as const satisfies readonly RegistrySourceKind[];

function actionFor(kind: RegistryArtifactKind, requested: RegistryAction): RegistryAction {
  return kind === "template" ? "create_from_template" : requested === "update" ? "update" : "install";
}

export function DesktopLibraryPanel({ library, project }: DesktopLibraryPanelProps): ReactNode {
  const [snapshot, setSnapshot] = useState<LibrarySnapshot>(() => library.snapshot());
  const [query, setQuery] = useState("");
  const [kind, setKind] = useState<"" | RegistryArtifactKind>("");
  const [action, setAction] = useState<RegistryAction>("install");
  const [sourceID, setSourceID] = useState("");
  const [sourceKind, setSourceKind] = useState<RegistrySourceKind>("local_directory");
  const [endpoint, setEndpoint] = useState("");
  const [connection, setConnection] = useState("");
  const operationSequence = useRef(0);
  const busy = ["loading", "previewing", "applying"].includes(snapshot.status);
  const update = async (operation: Promise<LibrarySnapshot>): Promise<void> => { setSnapshot(await operation); };

  useEffect(() => {
    void update(library.refreshSources());
    return () => library.cancel();
  }, [library]);

  const search = (event: FormEvent): void => {
    event.preventDefault();
    void update(library.search(query, kind === "" ? undefined : kind));
  };
  const configure = (event: FormEvent): void => {
    event.preventDefault();
    if (sourceID.trim() === "" || endpoint.trim() === "") return;
    void update(library.configureSource({ source_id: sourceID.trim(), kind: sourceKind, endpoint_ref: endpoint.trim(), trust_policy_id: "desktop-local", cache_policy: "on_demand", priority: 100 }));
  };
  const selected = snapshot.selected;
  const selectedAction = selected === undefined ? action : actionFor(selected.identity.kind, action);

  return createElement("section", { className: "ld-library-panel", "aria-label": "Library" },
    createElement("div", { className: "ld-library-heading" },
      createElement("div", null, createElement("h2", null, "Library"), createElement("p", null, "Browse trusted packs and templates.")),
      createElement("span", { role: "status", "aria-live": "polite", "data-status": snapshot.status }, snapshot.status.replaceAll("_", " "))),
    snapshot.failure === undefined ? null : createElement("p", { role: "alert", className: "ld-library-failure" }, `Library request failed: ${snapshot.failure.code}`),
    createElement("form", { onSubmit: search, "aria-label": "Browse Registry" },
      createElement("label", null, "Search", createElement("input", { type: "search", value: query, disabled: busy, onChange: (event) => setQuery(event.currentTarget.value) })),
      createElement("label", null, "Kind", createElement("select", { value: kind, disabled: busy, onChange: (event) => setKind((event.currentTarget as HTMLSelectElement).value as "" | RegistryArtifactKind) },
        createElement("option", { value: "" }, "All"), createElement("option", { value: "pack" }, "Packs"), createElement("option", { value: "template" }, "Templates"))),
      createElement("button", { type: "submit", disabled: busy }, "Browse")),
    createElement("ul", { className: "ld-library-results", "aria-label": "Registry results" }, snapshot.results.map((release) =>
      createElement("li", { key: `${release.identity.kind}:${release.identity.canonical_id}:${release.identity.version}` },
        createElement("button", { type: "button", disabled: busy, "aria-pressed": selected?.digest === release.digest, onClick: () => setSnapshot(library.select(release.identity)) },
          createElement("strong", null, release.identity.canonical_id), createElement("span", null, `${release.identity.version} · ${release.identity.kind}`), createElement("small", null, release.source_id))))),
    selected === undefined ? null : createElement("section", { className: "ld-library-selection", "aria-label": "Selected artifact" },
      createElement("h3", null, selected.identity.canonical_id),
      selected.identity.kind === "pack" ? createElement("label", null, "Action", createElement("select", { value: action, disabled: busy, onChange: (event) => setAction((event.currentTarget as HTMLSelectElement).value as RegistryAction) },
        createElement("option", { value: "install" }, "Install"), createElement("option", { value: "update" }, "Update"))) : createElement("p", null, "Create a new project from this template."),
      createElement("button", { type: "button", disabled: busy || (selected.identity.kind === "pack" && project === undefined), onClick: () => { void update(library.preview(selectedAction, project)); } }, selected.identity.kind === "template" ? "Preview template" : `Preview ${selectedAction}`)),
    snapshot.plan === undefined || snapshot.status !== "awaiting_confirmation" ? null : createElement("section", { className: "ld-library-plan", "aria-label": "Registry change preview" },
      createElement("h3", null, "Confirm Registry change"),
      createElement("p", null, `${snapshot.plan.action.replaceAll("_", " ")} · ${snapshot.plan.artifacts.length} artifact${snapshot.plan.artifacts.length === 1 ? "" : "s"}`),
      createElement("p", null, snapshot.plan.migration_required ? "Migration review is required." : "No state migration is required."),
      createElement("button", { type: "button", disabled: busy, onClick: () => { const id = `desktop-library-${Date.now()}-${++operationSequence.current}`; void update(library.confirm(id, id)); } }, "Confirm and apply")),
    snapshot.transaction === undefined || snapshot.status !== "recoverable_error" ? null : createElement("button", { type: "button", disabled: busy, onClick: () => { void update(library.recoverTransaction(snapshot.transaction!.plan.transaction_id)); } }, "Recover Registry transaction"),
    createElement("details", { className: "ld-library-sources" },
      createElement("summary", null, `Sources (${snapshot.sources.length})`),
      createElement("ul", null, snapshot.sources.map((source) => createElement("li", { key: source.source_id },
        createElement("div", null, createElement("strong", null, source.source_id), createElement("span", null, source.connected ? "Connected" : "Disconnected")),
        source.connected
          ? createElement("button", { type: "button", disabled: busy, onClick: () => { void update(library.disconnectSource(source.source_id)); } }, "Disconnect")
          : createElement("button", { type: "button", disabled: busy || connection.trim() === "", onClick: () => { void update(library.connectSource(source.source_id, connection.trim())); } }, "Connect")))),
      createElement("label", null, "Connection reference", createElement("input", { value: connection, disabled: busy, onChange: (event) => setConnection(event.currentTarget.value) })),
      createElement("form", { onSubmit: configure, "aria-label": "Configure Registry source" },
        createElement("label", null, "Source ID", createElement("input", { value: sourceID, disabled: busy, onChange: (event) => setSourceID(event.currentTarget.value) })),
        createElement("label", null, "Source kind", createElement("select", { value: sourceKind, disabled: busy, onChange: (event) => setSourceKind((event.currentTarget as HTMLSelectElement).value as RegistrySourceKind) }, sourceKinds.map((value) => createElement("option", { value, key: value }, value.replaceAll("_", " "))))),
        createElement("label", null, "Endpoint", createElement("input", { value: endpoint, disabled: busy, onChange: (event) => setEndpoint(event.currentTarget.value) })),
        createElement("button", { type: "submit", disabled: busy || sourceID.trim() === "" || endpoint.trim() === "" }, "Add source"))));
}
