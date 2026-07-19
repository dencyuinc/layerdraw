// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { AuthoringDecision, AuthoringGrantSummary } from "@layerdraw/protocol/access";
import {
  isPreviewFragmentInput,
  isPreviewOperationsInput,
  isPreviewSourcePatchInput,
  type PreviewFragmentInput,
  type PreviewOperationsInput,
  type PreviewSourcePatchInput,
  type WorkbenchPreviewResult,
} from "@layerdraw/protocol/engine";

export type EditorEdit =
  | Readonly<{ kind: "semantic_operations"; request: PreviewOperationsInput }>
  | Readonly<{ kind: "fragment"; request: PreviewFragmentInput }>
  | Readonly<{ kind: "source_patch"; request: PreviewSourcePatchInput }>;

export type EditorOperationRequest =
  | PreviewOperationsInput
  | PreviewFragmentInput
  | PreviewSourcePatchInput;

export interface ComposerIntentAdapter<TIntent> {
  toEditorEdit(intent: TIntent): EditorEdit;
}

export interface ComposerPresentationState {
  readonly preview: WorkbenchPreviewResult;
  readonly authoring_decision?: AuthoringDecision;
  readonly grant_summary?: AuthoringGrantSummary;
}

export class ComposerContractError extends TypeError {
  readonly code = "composer.invalid_editor_edit";

  constructor(message = "The editor edit does not match its protocol request contract.") {
    super(message);
    this.name = "ComposerContractError";
  }
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
