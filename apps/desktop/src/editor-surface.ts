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
import { createElement, type ReactNode } from "react";
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
  return createElement(RestoreFocus, null,
    createElement("div", { className: "ld-desktop-editor" },
      createElement(EditorToolbar, { label: t.t("editor.commands") },
        createElement(EditorCommandButton, { action: "apply", capabilityId: capabilities.apply, children: t.t("editor.apply") }),
        createElement(EditorCommandButton, { action: "undo", capabilityId: capabilities.history, children: t.t("editor.undo") }),
        createElement(EditorCommandButton, { action: "redo", capabilityId: capabilities.history, children: t.t("editor.redo") }),
        createElement(EditorCommandButton, { action: "retry", capabilityId: capabilities.preview, children: t.t("editor.retry") }),
        createElement(EditorCommandButton, { action: "cancel-preview", capabilityId: capabilities.preview, children: t.t("editor.cancelPreview") })),
      diagnostics.length === 0 ? null : createElement("section", { "aria-labelledby": "desktop-diagnostics" },
        createElement("h2", { id: "desktop-diagnostics" }, t.t("editor.diagnostics", { count: diagnostics.length })),
        createElement("ul", null, diagnostics.map((diagnostic, index) => createElement("li", { key: `${diagnostic.code}:${index}` }, diagnostic.code)))),
      conflicts.length === 0 ? null : createElement("p", { role: "status" }, t.t("editor.conflicts", { count: conflicts.length })),
      createElement(EditorLiveRegion)));
}

export function DesktopEditorSurface({ project, capabilities }: DesktopEditorSurfaceProps): ReactNode {
  return createElement(EditorProvider, { editor: project.editor, session: project.editor_session },
    createElement(EditorInspector, { capabilities }));
}
