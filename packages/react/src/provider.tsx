// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type {
  BrowserDocumentSession,
  BrowserEditor,
  BrowserEditorCapabilityState,
  EditorApplyResult,
  EditorPreviewResult,
} from "@layerdraw/client-sdk/editor";
import type { ComposerSnapshot, EditorEdit } from "@layerdraw/composer";
import type { AuthoringDecision, AuthoringGrantSummary } from "@layerdraw/protocol/access";
import type { CapabilityID, ProtocolDiagnostic } from "@layerdraw/protocol/common";
import type { SemanticConflict, WorkbenchPreviewResult } from "@layerdraw/protocol/engine";
import type { ConflictEvidence } from "@layerdraw/protocol/runtime";
import type { Diagnostic } from "@layerdraw/protocol/semantic";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react";

export type EditorPendingAction = "preview" | "apply" | "undo" | "redo" | "retry" | undefined;

export interface EditorState {
  readonly editor: BrowserEditor;
  readonly session: BrowserDocumentSession | undefined;
  readonly snapshot: ComposerSnapshot;
  readonly preview: WorkbenchPreviewResult | undefined;
  readonly diagnostics: readonly (Diagnostic | ProtocolDiagnostic)[];
  readonly impact: WorkbenchPreviewResult["authoring_impact"] | undefined;
  readonly decision: AuthoringDecision | undefined;
  readonly grant: AuthoringGrantSummary | undefined;
  readonly conflicts: readonly (SemanticConflict | ConflictEvidence)[];
  readonly capabilities: BrowserEditorCapabilityState | undefined;
  readonly pendingAction: EditorPendingAction;
  readonly error: unknown;
}

export interface EditorCommands {
  preview(edit: EditorEdit, options?: Readonly<{ intent_id?: string; inverse?: EditorEdit }>): Promise<EditorPreviewResult | undefined>;
  apply(edit?: EditorEdit): Promise<EditorApplyResult | undefined>;
  undo(): Promise<ComposerSnapshot | undefined>;
  redo(): Promise<ComposerSnapshot | undefined>;
  retry(): Promise<ComposerSnapshot | undefined>;
  cancelPreview(): ComposerSnapshot | undefined;
}

interface EditorContextValue {
  readonly state: EditorState;
  readonly commands: EditorCommands;
}

const EditorContext = createContext<EditorContextValue | undefined>(undefined);

function useEditorSnapshot(editor: BrowserEditor): ComposerSnapshot {
  const subscribe = useCallback((listener: () => void) => editor.subscribe(listener), [editor]);
  const getSnapshot = useCallback(() => editor.snapshot(), [editor]);
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}

function collectState(
  editor: BrowserEditor,
  session: BrowserDocumentSession | undefined,
  snapshot: ComposerSnapshot,
  pendingAction: EditorPendingAction,
  error: unknown,
): EditorState {
  const presentation = snapshot.presentation;
  return {
    editor,
    session,
    snapshot,
    preview: presentation?.preview,
    diagnostics: snapshot.failure?.diagnostics ?? presentation?.preview.diagnostics ?? [],
    impact: presentation?.preview.authoring_impact,
    decision: presentation?.authoring_decision,
    grant: presentation?.grant_summary,
    conflicts: snapshot.failure?.conflicts ?? presentation?.preview.conflicts ?? [],
    capabilities: session?.capabilities ?? editor.getCapabilities(),
    pendingAction,
    error,
  };
}

export interface EditorProviderProps {
  /** The host owns this instance and all of its transport and persistence dependencies. */
  readonly editor: BrowserEditor;
  readonly session?: BrowserDocumentSession;
  readonly children?: ReactNode;
}

export function EditorProvider({ editor, session, children }: EditorProviderProps): ReactNode {
  const snapshot = useEditorSnapshot(editor);
  const generation = useRef(0);
  const flightSequence = useRef(0);
  const ownedPreview = useRef<Readonly<{ generation: number; flight: number }> | undefined>(undefined);
  const mounted = useRef(true);
  const [pendingAction, setPendingAction] = useState<EditorPendingAction>();
  const [error, setError] = useState<unknown>();

  useEffect(() => {
    mounted.current = true;
    const currentGeneration = ++generation.current;
    setPendingAction(undefined);
    setError(undefined);
    return () => {
      mounted.current = false;
      generation.current = currentGeneration + 1;
      if (ownedPreview.current?.generation === currentGeneration && editor.snapshot().phase === "previewing") {
        ownedPreview.current = undefined;
        editor.cancelPreview();
      }
    };
  }, [editor, session]);

  const run = useCallback(async <T,>(
    action: Exclude<EditorPendingAction, undefined>,
    operation: () => Promise<T>,
    ownership?: "preview",
  ): Promise<T | undefined> => {
    const currentGeneration = generation.current;
    const flight = ++flightSequence.current;
    if (ownership === "preview") ownedPreview.current = { generation: currentGeneration, flight };
    setPendingAction(action);
    setError(undefined);
    try {
      const result = await operation();
      return mounted.current && generation.current === currentGeneration && flightSequence.current === flight ? result : undefined;
    } catch (caught) {
      if (mounted.current && generation.current === currentGeneration && flightSequence.current === flight) setError(caught);
      return undefined;
    } finally {
      if (ownedPreview.current?.generation === currentGeneration && ownedPreview.current.flight === flight) ownedPreview.current = undefined;
      if (mounted.current && generation.current === currentGeneration && flightSequence.current === flight) setPendingAction(undefined);
    }
  }, []);

  const commands = useMemo<EditorCommands>(() => ({
    preview: (edit, options) => run("preview", () => editor.preview(edit, options), "preview"),
    apply: (edit) => {
      const selected = edit ?? editor.snapshot().intent?.edit;
      if (selected === undefined) return Promise.resolve(undefined);
      return run("apply", () => editor.apply(selected));
    },
    undo: () => run("undo", () => editor.undo()),
    redo: () => run("redo", () => editor.redo()),
    retry: () => run("retry", () => editor.retry()),
    cancelPreview: () => {
      if (editor.snapshot().phase !== "previewing") return undefined;
      ownedPreview.current = undefined;
      ++flightSequence.current;
      setPendingAction(undefined);
      return editor.cancelPreview();
    },
  }), [editor, run]);

  const state = useMemo(
    () => collectState(editor, session, snapshot, pendingAction, error),
    [editor, session, snapshot, pendingAction, error],
  );
  const value = useMemo(() => ({ state, commands }), [state, commands]);
  return <EditorContext.Provider value={value}>{children}</EditorContext.Provider>;
}

function useRequiredContext(): EditorContextValue {
  const value = useContext(EditorContext);
  if (value === undefined) throw new Error("LayerDraw editor hooks require an EditorProvider.");
  return value;
}

export function useEditor(): EditorContextValue { return useRequiredContext(); }
export function useEditorState(): EditorState { return useRequiredContext().state; }
export function useEditorCommands(): EditorCommands { return useRequiredContext().commands; }
export function useEditorSession(): BrowserDocumentSession | undefined { return useEditorState().session; }
export function useEditorPreview(): WorkbenchPreviewResult | undefined { return useEditorState().preview; }
export function useEditorDiagnostics(): readonly (Diagnostic | ProtocolDiagnostic)[] { return useEditorState().diagnostics; }
export function useEditorImpact(): WorkbenchPreviewResult["authoring_impact"] | undefined { return useEditorState().impact; }
export function useEditorGrant(): AuthoringGrantSummary | undefined { return useEditorState().grant; }
export function useEditorDecision(): AuthoringDecision | undefined { return useEditorState().decision; }
export function useEditorConflicts(): readonly (SemanticConflict | ConflictEvidence)[] { return useEditorState().conflicts; }
export function useEditorCapabilities(): BrowserEditorCapabilityState | undefined { return useEditorState().capabilities; }

export function useCapability(capabilityId: CapabilityID): Readonly<{ available: boolean; reason?: string }> {
  const capabilities = useEditorCapabilities();
  const operation = capabilities?.manifest.operations[capabilityId];
  return useMemo(() => operation?.enabled === true
    ? { available: true }
    : { available: false, reason: operation?.unavailable_reason ?? "not_advertised" },
  [operation]);
}
