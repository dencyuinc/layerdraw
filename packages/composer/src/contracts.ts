// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { AuthoringDecision, AuthoringGrantSummary } from "@layerdraw/protocol/access";
import type {
  PreviewFragmentInput,
  PreviewOperationsInput,
  PreviewSourcePatchInput,
  WorkbenchPreviewResult,
} from "@layerdraw/protocol/engine";

export type EditorEdit =
  | Readonly<{ kind: "semantic_operations"; request: PreviewOperationsInput }>
  | Readonly<{ kind: "fragment"; request: PreviewFragmentInput }>
  | Readonly<{ kind: "source_patch"; request: PreviewSourcePatchInput }>;

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
