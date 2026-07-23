// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { buildSemanticEdit, type SemanticEditContext } from "@layerdraw/composer";
import type { CreateQueryOperation, CreateViewOperation, DocumentGeneration, SemanticOperation, WorkbenchLimits } from "@layerdraw/protocol/engine";
import type { EntityTypeAddress, LayerAddress, ProjectRootAddress, QueryAddress, RelationTypeAddress } from "@layerdraw/protocol/semantic";
import { baseShellCatalogs, createTranslator, useEditor, useOptionalI18n, type Translator } from "@layerdraw/react";
import { groupAuthoringSchema, type AuthoringSchema, type AuthoringSubject } from "./authoring-schema.js";
import type { DesktopProjectContext } from "./contracts.js";
import { tokenSelect } from "./token-select.js";

const defaultTranslator: Translator = createTranslator("en", baseShellCatalogs);

const editLimits: WorkbenchLimits = Object.freeze({ max_items: "4096", max_output_bytes: "4000000" });

const viewShapes = ["diagram", "table", "matrix", "tree", "flow", "context", "diff"] as const;
const viewCategories = ["topology", "inventory", "hierarchy", "flow", "context", "dependency", "impact", "diff"] as const;

/** Derives a valid LocalIdentifier from a display name; falls back to a stamped id. */
export function localIdentifierFrom(displayName: string, fallback: string): string {
  const slug = displayName.toLowerCase().replaceAll(/[^a-z0-9]+/gu, "_").replaceAll(/^_+|_+$/gu, "").replaceAll(/_{2,}/gu, "_");
  return /^[a-z][a-z0-9_]*$/u.test(slug) ? slug : fallback;
}

interface ChipGroupProps {
  readonly label: string;
  readonly hint: string;
  readonly options: readonly AuthoringSubject[];
  readonly selected: readonly string[];
  readonly onToggle: (address: string) => void;
}

function ChipGroup({ label, hint, options, selected, onToggle }: ChipGroupProps): ReactNode {
  return (
    <div className="ld-field">
      <span className="ld-field-label">{label}</span>
      {options.length === 0
        ? <span className="ld-field-hint">{hint}</span>
        : (
          <span className="ld-field-chips" role="group" aria-label={label}>
            {options.map((option) => (
              <button
                key={option.address}
                type="button"
                className="ld-choice-chip"
                role="checkbox"
                aria-checked={selected.includes(option.address)}
                onClick={() => onToggle(option.address)}
              >{option.display_name}</button>
            ))}
          </span>
        )}
    </div>
  );
}

export interface DesktopViewAuthoringProps {
  readonly project: DesktopProjectContext;
  readonly creating: boolean;
  readonly onCloseCreate: () => void;
}

/**
 * Views-mode inspector authoring: the New View form composes one atomic
 * semantic batch (backing select query + view) via composer builders. The
 * Engine stays the only semantics owner; failures surface with reason + code
 * through the editor surface below.
 */
export function DesktopViewAuthoring({ project, creating, onCloseCreate }: DesktopViewAuthoringProps): ReactNode {
  const t = useOptionalI18n() ?? defaultTranslator;
  const { commands } = useEditor();
  const [schema, setSchema] = useState<AuthoringSchema>();
  const [generation, setGeneration] = useState<DocumentGeneration>();
  const [failure, setFailure] = useState<string>();
  const [name, setName] = useState("");
  const [shape, setShape] = useState<string>("diagram");
  const [category, setCategory] = useState<string>("topology");
  const [layers, setLayers] = useState<readonly string[]>([]);
  const [entityTypes, setEntityTypes] = useState<readonly string[]>([]);
  const [relationTypes, setRelationTypes] = useState<readonly string[]>([]);
  const readGeneration = project.readDocumentGeneration;
  const readSubjects = project.readSubjects;

  useEffect(() => {
    if (readGeneration === undefined || readSubjects === undefined) return;
    let cancelled = false;
    setFailure(undefined);
    void (async () => {
      try {
        const nextGeneration = await readGeneration();
        const subjects = await readSubjects();
        if (!cancelled) {
          setGeneration(nextGeneration);
          setSchema(groupAuthoringSchema(subjects));
        }
      } catch (error) {
        if (!cancelled) setFailure(error instanceof Error && error.message !== "" ? error.message : "desktop.authoring_schema_failed");
      }
    })();
    return () => { cancelled = true; };
  }, [readGeneration, readSubjects, project.authoritative_revision_token]);

  const projectRoot = (project.library_project?.project_id ?? "") as ProjectRootAddress;
  if (readGeneration === undefined || readSubjects === undefined || projectRoot === "") return null;
  if (failure !== undefined) return <p role="alert" className="ld-authoring-failure">{t.t("authoring.failed", { code: failure })}</p>;
  if (!creating) return null;
  if (schema === undefined || generation === undefined) return <p role="status" className="ld-field-hint">{t.t("authoring.loading")}</p>;

  const toggle = (current: readonly string[], set: (next: readonly string[]) => void) => (address: string): void => {
    set(current.includes(address) ? current.filter((entry) => entry !== address) : [...current, address]);
  };

  const submit = (event: FormEvent): void => {
    event.preventDefault();
    const trimmed = name.trim();
    if (trimmed === "") return;
    const viewId = localIdentifierFrom(trimmed, `view_${Date.now() % 100000}`);
    const queryId = `${viewId}_scope`;
    const sorted = <T extends string>(values: readonly string[]): readonly T[] => ([...values].sort() as unknown as readonly T[]);
    const context: SemanticEditContext = {
      limits: editLimits,
      preconditions: { document_generation: generation, expected_child_sets: [], expected_subject_hashes: [], expected_subtree_hashes: [] },
    };
    const query: CreateQueryOperation = {
      operation: "create_subject", subject_kind: "query", id: queryId, parent_address: projectRoot,
      fields: {
        display_name: `${trimmed} scope`,
        select: {
          ...(layers.length === 0 ? {} : { layer_addresses: sorted<LayerAddress>(layers) }),
          ...(entityTypes.length === 0 ? {} : { entity_type_addresses: sorted<EntityTypeAddress>(entityTypes) }),
          ...(relationTypes.length === 0 ? {} : { relation_type_addresses: sorted<RelationTypeAddress>(relationTypes) }),
        },
      },
    };
    const view: CreateViewOperation = {
      operation: "create_subject", subject_kind: "view", id: viewId, parent_address: projectRoot,
      fields: {
        display_name: trimmed,
        category: category as CreateViewOperation["fields"]["category"],
        shape: { kind: shape as CreateViewOperation["fields"]["shape"]["kind"] },
        source: { kind: "query", query_address: `${projectRoot}:query:${queryId}` as QueryAddress, arguments: {} },
      },
    };
    const operations: readonly SemanticOperation[] = [query, view];
    void commands.preview(buildSemanticEdit(operations, context), { intent_id: `create-view-${viewId}` });
  };

  return (
    <form className="ld-authoring-form" aria-label={t.t("authoring.view.newTitle")} onSubmit={submit}>
      <div className="ld-fgroup">
        <span className="ld-fgroup-label">{t.t("authoring.section.definition")}</span>
        <div className="ld-field">
          <label className="ld-field-label" htmlFor="ld-new-view-name">{t.t("authoring.view.name")}</label>
          <input
            id="ld-new-view-name"
            className="ld-field-input"
            value={name}
            required
            placeholder={t.t("authoring.view.namePlaceholder")}
            onChange={(event) => setName(event.currentTarget.value)}
          />
        </div>
        <div className="ld-field">
          <span className="ld-field-label">{t.t("authoring.view.shape")}</span>
          {tokenSelect(t.t("authoring.view.shape"), shape, viewShapes.map((value) => ({ value, label: t.t(`authoring.shape.${value}`) })), setShape)}
        </div>
        <div className="ld-field">
          <span className="ld-field-label">{t.t("authoring.view.category")}</span>
          {tokenSelect(t.t("authoring.view.category"), category, viewCategories.map((value) => ({ value, label: t.t(`authoring.category.${value}`) })), setCategory)}
        </div>
      </div>
      <div className="ld-fgroup">
        <span className="ld-fgroup-label">{t.t("authoring.section.projection")}</span>
        <ChipGroup label={t.t("authoring.view.layers")} hint={t.t("authoring.view.filterHint")} options={schema.layers} selected={layers} onToggle={toggle(layers, setLayers)} />
        <ChipGroup label={t.t("authoring.view.entityTypes")} hint={t.t("authoring.view.filterHint")} options={schema.entityTypes} selected={entityTypes} onToggle={toggle(entityTypes, setEntityTypes)} />
        <ChipGroup label={t.t("authoring.view.relationTypes")} hint={t.t("authoring.view.filterHint")} options={schema.relationTypes} selected={relationTypes} onToggle={toggle(relationTypes, setRelationTypes)} />
      </div>
      <div className="ld-authoring-foot">
        <button type="submit" className="ld-btn ld-btn-primary" disabled={name.trim() === ""}>{t.t("authoring.preview")}</button>
        <button type="button" className="ld-btn ld-btn-secondary" onClick={onCloseCreate}>{t.t("authoring.cancel")}</button>
      </div>
    </form>
  );
}
