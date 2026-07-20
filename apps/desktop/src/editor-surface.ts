// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { CapabilityID } from "@layerdraw/protocol/common";
import {
  EditorCommandButton,
  EditorLiveRegion,
  EditorProvider,
  EditorToolbar,
  RestoreFocus,
  useEditorConflicts,
  useEditorDecision,
  useEditorDiagnostics,
  useEditorGrant,
  useEditorImpact,
  useEditorState,
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

function summaryValue(value: unknown, fallback: string): string {
  if (value === undefined || value === null) return fallback;
  if (typeof value === "object" && "outcome" in value && typeof value.outcome === "string") return value.outcome.replaceAll("_", " ");
  return fallback;
}

function EditorInspector({ capabilities }: Readonly<{ capabilities: DesktopEditorCapabilityIDs }>): ReactNode {
  const state = useEditorState();
  const diagnostics = useEditorDiagnostics();
  const conflicts = useEditorConflicts();
  const decision = useEditorDecision();
  const grant = useEditorGrant();
  const impact = useEditorImpact();
  return createElement(RestoreFocus, null,
    createElement("div", { className: "ld-desktop-editor" },
      createElement(EditorToolbar, { label: "Authoring commands" },
        createElement(EditorCommandButton, { action: "apply", capabilityId: capabilities.apply, children: "Apply" }),
        createElement(EditorCommandButton, { action: "undo", capabilityId: capabilities.history, children: "Undo" }),
        createElement(EditorCommandButton, { action: "redo", capabilityId: capabilities.history, children: "Redo" }),
        createElement(EditorCommandButton, { action: "retry", capabilityId: capabilities.preview, children: "Retry" }),
        createElement(EditorCommandButton, { action: "cancel-preview", capabilityId: capabilities.preview, children: "Cancel preview" })),
      createElement("dl", { className: "ld-desktop-editor-summary" },
        createElement("div", null, createElement("dt", null, "Editor"), createElement("dd", null, state.snapshot.phase)),
        createElement("div", null, createElement("dt", null, "Persistence"), createElement("dd", null, state.session?.persistence ?? "unavailable")),
        createElement("div", null, createElement("dt", null, "Access"), createElement("dd", null, summaryValue(decision, "not evaluated"))),
        createElement("div", null, createElement("dt", null, "Grant"), createElement("dd", null, grant === undefined ? "unavailable" : "evaluated")),
        createElement("div", null, createElement("dt", null, "Impact"), createElement("dd", null, impact === undefined ? "none declared" : "declared"))),
      diagnostics.length === 0 ? null : createElement("section", { "aria-labelledby": "desktop-diagnostics" },
        createElement("h2", { id: "desktop-diagnostics" }, `Diagnostics (${diagnostics.length})`),
        createElement("ul", null, diagnostics.map((diagnostic, index) => createElement("li", { key: `${diagnostic.code}:${index}` }, diagnostic.code)))),
      conflicts.length === 0 ? null : createElement("p", { role: "status" }, `${conflicts.length} conflict${conflicts.length === 1 ? "" : "s"} require attention.`),
      createElement(EditorLiveRegion)));
}

export function DesktopEditorSurface({ project, capabilities }: DesktopEditorSurfaceProps): ReactNode {
  return createElement(EditorProvider, { editor: project.editor, session: project.editor_session },
    createElement(EditorInspector, { capabilities }));
}
