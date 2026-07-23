// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { BrowserEditor, BrowserEditorError } from "@layerdraw/client-sdk/editor";
import type { EditorEdit } from "@layerdraw/composer";
import type { CapabilityID } from "@layerdraw/protocol/common";
import type { MaterializeViewInput } from "@layerdraw/protocol/engine";
import type { ViewData } from "@layerdraw/protocol/semantic";
import type { Viewer, ViewerOperationResult, ViewerPublication, ViewerSnapshot, ViewerState } from "@layerdraw/viewer";
import {
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type FormEvent,
  type ReactNode,
} from "react";
import { useEditor, useEditorCapabilities } from "./provider.js";

export type QueryViewKind = "query" | "view";
export type QueryViewActionKind = "create" | "edit" | "duplicate" | "remove";
export type QueryViewFieldValue = string | number | boolean | readonly string[];
export type QueryViewDraft = Readonly<Record<string, QueryViewFieldValue>>;

export type QueryViewAvailability =
  | Readonly<{ status: "available" }>
  | Readonly<{ status: "unavailable"; capability_id?: CapabilityID; reason: string; requirement?: "optional" | "required" }>
  | Readonly<{ status: "denied"; reason: string }>;

export type QueryViewField = Readonly<{
  id: string;
  label: string;
  required?: boolean;
  description?: string;
}> & (
  | Readonly<{ type: "text"; value?: string; placeholder?: string }>
  | Readonly<{ type: "number"; value?: number; min?: number; max?: number; step?: number }>
  | Readonly<{ type: "boolean"; value?: boolean }>
  | Readonly<{ type: "select"; value?: string; options: readonly Readonly<{ value: string; label: string }>[] }>
  | Readonly<{ type: "multi_select"; value?: readonly string[]; options: readonly Readonly<{ value: string; label: string }>[] }>
);

export interface QueryViewDefinition {
  readonly id: string;
  readonly kind: QueryViewKind;
  readonly label: string;
  readonly description?: string;
  readonly availability?: QueryViewAvailability;
}

export interface QueryViewIntent {
  readonly id: string;
  readonly kind: QueryViewActionKind;
  readonly label: string;
  readonly target_id?: string;
  readonly availability: QueryViewAvailability;
  /** Host-projected capability/schema fields. React does not infer Engine schema. */
  readonly fields: readonly QueryViewField[];
  /** Host/Composer-owned semantic intent builder. */
  readonly buildEdit: (draft: QueryViewDraft) => EditorEdit;
}

function initialDraft(fields: readonly QueryViewField[]): QueryViewDraft {
  return Object.freeze(Object.fromEntries(fields.map((field) => [field.id,
    field.value ?? (field.type === "boolean" ? false : field.type === "number" ? 0 : field.type === "multi_select" ? [] : ""),
  ])));
}

function fieldControl(field: QueryViewField, controlId: string, value: QueryViewFieldValue, set: (value: QueryViewFieldValue) => void): ReactNode {
  const common = { id: controlId, name: field.id, required: field.required, "aria-describedby": field.description === undefined ? undefined : `${controlId}-description` };
  if (field.type === "boolean") {
    return <input {...common} type="checkbox" checked={Boolean(value)} onChange={(event: ChangeEvent<HTMLInputElement>) => set(event.currentTarget.checked)} />;
  }
  if (field.type === "multi_select") {
    const selected = Array.isArray(value) ? (value as readonly string[]) : [];
    return (
      <span className="ld-query-view-chips" role="group" id={controlId} aria-describedby={common["aria-describedby"]}>
        {field.options.map((option) => {
          const granted = selected.includes(option.value);
          return (
            <button
              key={option.value}
              type="button"
              className="ld-query-view-chip"
              role="checkbox"
              aria-checked={granted}
              onClick={() => set(granted ? selected.filter((entry) => entry !== option.value) : [...selected, option.value])}
            >{option.label}</button>
          );
        })}
      </span>
    );
  }
  if (field.type === "select") {
    return (
      <select {...common} value={String(value)} onChange={(event: ChangeEvent<HTMLSelectElement>) => set(event.currentTarget.value)}>
        {field.options.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
      </select>
    );
  }
  return (
    <input
      {...common}
      type={field.type}
      value={value as string | number}
      {...(field.type === "text" ? { placeholder: field.placeholder } : { min: field.min, max: field.max, step: field.step })}
      onChange={(event: ChangeEvent<HTMLInputElement>) => set(field.type === "number" ? event.currentTarget.valueAsNumber : event.currentTarget.value)}
    />
  );
}

export interface QueryViewComposerProps {
  readonly definitions: readonly QueryViewDefinition[];
  readonly selectedId?: string;
  readonly onSelect: (id: string) => void;
  readonly intents: readonly QueryViewIntent[];
  readonly heading?: string;
}

export function QueryViewComposer({ definitions, selectedId, onSelect, intents, heading = "Queries and views" }: QueryViewComposerProps): ReactNode {
  const { commands } = useEditor();
  const [activeId, setActiveId] = useState<string>();
  const [draft, setDraft] = useState<QueryViewDraft>({});
  const [localError, setLocalError] = useState<string>();
  const formId = useId();
  const selected = definitions.find((definition) => definition.id === selectedId);
  const active = intents.find((intent) => intent.id === activeId);

  const begin = (intent: QueryViewIntent): void => {
    if (intent.availability.status !== "available") return;
    setActiveId(intent.id);
    setDraft(initialDraft(intent.fields));
    setLocalError(undefined);
  };
  const submit = (event: FormEvent): void => {
    event.preventDefault();
    const current = intents.find((intent) => intent.id === activeId);
    if (current === undefined) { setLocalError("This action is no longer available."); return; }
    if (current.availability.status !== "available") { setLocalError(current.availability.reason); return; }
    if (current.target_id !== undefined && current.target_id !== selectedId) { setLocalError("The action target is no longer selected."); return; }
    try {
      const edit = current.buildEdit(draft);
      setLocalError(undefined);
      void commands.preview(edit, { intent_id: current.id });
    } catch (error) {
      setLocalError(error instanceof Error ? error.message : "The action could not be prepared.");
    }
  };

  return (
    <section className="ld-query-view-composer" aria-labelledby={`${formId}-heading`}>
      <h2 id={`${formId}-heading`}>{heading}</h2>
      <label htmlFor={`${formId}-definition`}>Selected definition</label>
      <select id={`${formId}-definition`} value={selectedId ?? ""} onChange={(event: ChangeEvent<HTMLSelectElement>) => onSelect(event.currentTarget.value)}>
        <option value="">Select…</option>
        {definitions.map((definition) => (
          <option
            key={definition.id}
            value={definition.id}
            disabled={definition.availability?.status !== undefined && definition.availability.status !== "available"}
          >
            {`${definition.label} (${definition.kind})`}
          </option>
        ))}
      </select>
      {selected?.description === undefined ? null : <p>{selected.description}</p>}
      <div className="ld-query-view-actions" role="toolbar" aria-label="Query and view actions">
        {intents.map((intent) => {
          const blocked = intent.availability.status !== "available" || (intent.target_id !== undefined && intent.target_id !== selectedId);
          const reason = intent.availability.status === "available" ? undefined : intent.availability.reason;
          return (
            <button key={intent.id} type="button" disabled={blocked} aria-disabled={blocked} title={reason} onClick={() => begin(intent)}>
              {intent.label}
            </button>
          );
        })}
      </div>
      {localError === undefined ? null : <div role="alert">{localError}</div>}
      {active === undefined ? null : (
        <form onSubmit={submit} data-query-view-action={active.kind}>
          <h3>{active.label}</h3>
          {active.fields.map((field) => {
            const controlId = `${formId}-${field.id}`;
            return (
              <div key={field.id} className="ld-query-view-field">
                <label htmlFor={controlId}>{field.label}</label>
                {fieldControl(field, controlId, draft[field.id] ?? "", (value) => setDraft((current) => Object.freeze({ ...current, [field.id]: value })))}
                {field.description === undefined ? null : <small id={`${controlId}-description`}>{field.description}</small>}
              </div>
            );
          })}
          <button type="submit" disabled={active.availability.status !== "available" || (active.target_id !== undefined && active.target_id !== selectedId)}>
            {active.label}
          </button>
          <button type="button" onClick={() => { setActiveId(undefined); setLocalError(undefined); }}>Cancel</button>
        </form>
      )}
    </section>
  );
}

export interface LiveViewRequest {
  readonly key: string;
  readonly input: MaterializeViewInput;
  /** Adds authoritative stream metadata; the returned ViewData is always used verbatim. */
  readonly toViewerSnapshot: (viewData: ViewData) => ViewerSnapshot;
}

export type LiveViewerState =
  | Readonly<{ status: "idle" | "debouncing" | "materializing"; previous?: ViewerPublication }>
  | Readonly<{ status: "ready" | "empty" | "partial"; viewer: ViewerState; publication?: ViewerPublication }>
  | Readonly<{ status: "unavailable"; reason: string; capability_id?: CapabilityID; requirement: "optional" | "required" }>
  | Readonly<{ status: "error"; error: unknown; diagnostics: readonly unknown[]; previous?: ViewerPublication }>;

export interface UseLiveViewerOptions {
  readonly editor: BrowserEditor;
  readonly viewer: Viewer;
  readonly request?: LiveViewRequest;
  readonly availability?: QueryViewAvailability;
  readonly debounceMs?: number;
}

function publishedState(state: ViewerState): LiveViewerState {
  const publication = "publication" in state ? state.publication : "previous" in state ? state.previous : undefined;
  if (state.status === "ready") return { status: "ready", viewer: state, publication: state.publication };
  if (state.status === "empty") return { status: "empty", viewer: state, ...(publication === undefined ? {} : { publication }) };
  if (state.status === "partial_stream") return { status: "partial", viewer: state, publication: state.publication };
  const error = "error" in state ? state.error : state;
  return { status: "error", error, diagnostics: "error" in state ? state.error.render_diagnostics ?? [] : [], ...(publication === undefined ? {} : { previous: publication }) };
}

const available: QueryViewAvailability = Object.freeze({ status: "available" });

export function useLiveViewer({ editor, viewer, request, availability = available, debounceMs = 150 }: UseLiveViewerOptions): LiveViewerState {
  const generation = useRef(0);
  const latestRequest = useRef(request);
  latestRequest.current = request;
  const [state, setState] = useState<LiveViewerState>({ status: "idle" });
  const availabilityStatus = availability.status;
  const unavailableReason = availability.status === "available" ? undefined : availability.reason;
  const unavailableCapability = availability.status === "unavailable" ? availability.capability_id : undefined;
  const unavailableRequirement = availability.status === "unavailable" ? availability.requirement : undefined;
  const requestKey = request?.key;
  useEffect(() => {
    const current = ++generation.current;
    const previous = viewer.getPublication();
    if (availabilityStatus !== "available") {
      setState({
        status: "unavailable",
        reason: unavailableReason ?? "unavailable",
        requirement: availabilityStatus === "unavailable" ? unavailableRequirement ?? "optional" : "required",
        ...(unavailableCapability === undefined ? {} : { capability_id: unavailableCapability }),
      });
      return;
    }
    if (requestKey === undefined) { setState({ status: "idle", ...(previous === undefined ? {} : { previous }) }); return; }
    const controller = new AbortController();
    setState({ status: "debouncing", ...(previous === undefined ? {} : { previous }) });
    const timer = setTimeout(() => {
      if (generation.current !== current) return;
      const currentRequest = latestRequest.current;
      if (currentRequest === undefined || currentRequest.key !== requestKey) return;
      setState({ status: "materializing", ...(previous === undefined ? {} : { previous }) });
      void editor.materializeView(currentRequest.input, { signal: controller.signal }).then(async (viewData) => {
        if (generation.current !== current || controller.signal.aborted) return;
        const snapshot = currentRequest.toViewerSnapshot(viewData);
        const result: ViewerOperationResult = await viewer.setViewData({ ...snapshot, view_data: viewData });
        if (generation.current === current && !controller.signal.aborted) setState(publishedState(result.state));
      }).catch((error: BrowserEditorError | unknown) => {
        if (generation.current !== current || controller.signal.aborted) return;
        const diagnostics = typeof error === "object" && error !== null && "diagnostics" in error && Array.isArray(error.diagnostics) ? error.diagnostics : [];
        setState({ status: "error", error, diagnostics, ...(previous === undefined ? {} : { previous }) });
      });
    }, Math.max(0, debounceMs));
    return () => { clearTimeout(timer); controller.abort(); void viewer.cancel(); };
  }, [availabilityStatus, debounceMs, editor, requestKey, unavailableCapability, unavailableReason, unavailableRequirement, viewer]);
  return state;
}

export interface LiveViewerProps extends UseLiveViewerOptions {
  readonly children: (state: LiveViewerState) => ReactNode;
}

export function LiveViewer({ children, ...options }: LiveViewerProps): ReactNode {
  const state = useLiveViewer(options);
  return (
    <section className="ld-live-viewer" aria-busy={state.status === "debouncing" || state.status === "materializing"} data-viewer-state={state.status}>
      {state.status === "error" ? <div role="alert">The view could not be materialized.</div> : null}
      {state.status === "unavailable" ? <div role={state.requirement === "required" ? "alert" : "status"}>{state.reason}</div> : null}
      {children(state)}
    </section>
  );
}

export function useMaterializeCapability(capabilityId: CapabilityID, requirement: "optional" | "required" = "optional"): QueryViewAvailability {
  const capabilities = useEditorCapabilities();
  return useMemo(() => {
    const operation = capabilities?.manifest.operations[capabilityId];
    return operation?.enabled === true ? { status: "available" } : { status: "unavailable", capability_id: capabilityId, reason: operation?.unavailable_reason ?? "not_advertised", requirement };
  }, [capabilities, capabilityId, requirement]);
}
