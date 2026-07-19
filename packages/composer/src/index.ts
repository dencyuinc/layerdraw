// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {
  isPreviewFragmentInput,
  isPreviewOperationsInput,
  isPreviewSourcePatchInput,
  type PreviewFragmentInput,
  type PreviewOperationsInput,
  type PreviewSourcePatchInput,
  type WorkbenchPreviewResult,
} from "@layerdraw/protocol/engine";
import type { AuthoringDecision, AuthoringGrantSummary } from "@layerdraw/protocol/access";
import { ComposerContractError, type ComposerPresentationState, type EditorEdit } from "./contracts.js";

export type EditorOperationRequest =
  | PreviewOperationsInput
  | PreviewFragmentInput
  | PreviewSourcePatchInput;

export interface ComposerIntentAdapter<TIntent> {
  toEditorEdit(intent: TIntent): EditorEdit;
}

export function toEditorOperationRequest(edit: EditorEdit): EditorOperationRequest {
  const valid =
    (edit.kind === "semantic_operations" && isPreviewOperationsInput(edit.request)) ||
    (edit.kind === "fragment" && isPreviewFragmentInput(edit.request)) ||
    (edit.kind === "source_patch" && isPreviewSourcePatchInput(edit.request));
  if (!valid) throw new ComposerContractError();
  return edit.request;
}

export function retainComposerPresentation(
  preview: WorkbenchPreviewResult,
  access?: Readonly<{
    authoring_decision?: AuthoringDecision;
    grant_summary?: AuthoringGrantSummary;
  }>,
): ComposerPresentationState {
  return {
    preview,
    ...(access?.authoring_decision === undefined
      ? {}
      : { authoring_decision: access.authoring_decision }),
    ...(access?.grant_summary === undefined
      ? {}
      : { grant_summary: access.grant_summary }),
  };
}

export * from "./builders.js";
export * from "./contracts.js";
export * from "./state-machine.js";
