// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { CapabilityID } from "@layerdraw/protocol/common";
import {
  EditorCommandButton,
  EditorLiveRegion,
  EditorProvider,
  EditorToolbar,
  RestoreFocus,
  useEditorConflicts,
  useEditorDiagnostics,
  baseShellCatalogs,
  createTranslator,
  useOptionalI18n,
  type Translator,
} from "@layerdraw/react";
import type { ReactNode } from "react";
import type { DesktopProjectContext } from "./contracts.js";

export interface DesktopEditorCapabilityIDs {
  readonly preview: CapabilityID;
  readonly apply: CapabilityID;
  readonly history: CapabilityID;
}

export interface DesktopEditorSurfaceProps {
  readonly project: DesktopProjectContext;
  readonly capabilities: DesktopEditorCapabilityIDs;
}

/**
 * Authoring command strip. Internal editor state machinery (phase, persistence,
 * grant, impact) never renders here — failures surface through diagnostics and
 * conflicts, which are user-actionable.
 */
const defaultTranslator: Translator = createTranslator("en", baseShellCatalogs);

function EditorInspector({ capabilities }: Readonly<{ capabilities: DesktopEditorCapabilityIDs }>): ReactNode {
  const t = useOptionalI18n() ?? defaultTranslator;
  const diagnostics = useEditorDiagnostics();
  const conflicts = useEditorConflicts();
  return (
    <RestoreFocus>
      <div className="ld-desktop-editor">
        <EditorToolbar label={t.t("editor.commands")}>
          <EditorCommandButton action="apply" capabilityId={capabilities.apply}>{t.t("editor.apply")}</EditorCommandButton>
          <EditorCommandButton action="undo" capabilityId={capabilities.history}>{t.t("editor.undo")}</EditorCommandButton>
          <EditorCommandButton action="redo" capabilityId={capabilities.history}>{t.t("editor.redo")}</EditorCommandButton>
          <EditorCommandButton action="retry" capabilityId={capabilities.preview}>{t.t("editor.retry")}</EditorCommandButton>
          <EditorCommandButton action="cancel-preview" capabilityId={capabilities.preview}>{t.t("editor.cancelPreview")}</EditorCommandButton>
        </EditorToolbar>
        {diagnostics.length === 0 ? null : (
          <section aria-labelledby="desktop-diagnostics">
            <h2 id="desktop-diagnostics">{t.t("editor.diagnostics", { count: diagnostics.length })}</h2>
            <ul>{diagnostics.map((diagnostic, index) => <li key={`${diagnostic.code}:${index}`}>{diagnostic.code}</li>)}</ul>
          </section>
        )}
        {conflicts.length === 0 ? null : <p role="status">{t.t("editor.conflicts", { count: conflicts.length })}</p>}
        <EditorLiveRegion />
      </div>
    </RestoreFocus>
  );
}

export function DesktopEditorSurface({ project, capabilities }: DesktopEditorSurfaceProps): ReactNode {
  return (
    <EditorProvider editor={project.editor} session={project.editor_session}>
      <EditorInspector capabilities={capabilities} />
    </EditorProvider>
  );
}
