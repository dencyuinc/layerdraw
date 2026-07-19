// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type {
  BrowserEditor,
  BrowserEditorFactory,
  EditorApplyResult,
} from "../src/editor.js";

declare const editor: BrowserEditor;
declare const factory: BrowserEditorFactory;

editor.open({ authority: "engine", input: {} as never });
editor.preview({ kind: "semantic_operations", request: {} as never });
editor.apply({ kind: "fragment", request: {} as never });
editor.materializeView({} as never);
editor.close();
factory({} as never);

declare const applyResult: EditorApplyResult;
if (applyResult.persistence === "durable") {
  void applyResult.committed_revision.revision_id;
  const committedStatus:
    | "committed"
    | "committed_external_failed"
    | "committed_external_pending"
    | "committed_state_stale" = applyResult.result.operation_result.status;
  void applyResult.result.operation_result.committed_revision.revision_id;
  void committedStatus;
} else {
  const noCommittedRevision: undefined = applyResult.committed_revision;
  void noCommittedRevision;
}

if (applyResult.persistence === "runtime_not_committed") {
  const notCommittedStatus: "needs_review" | "rejected" = applyResult.result.operation_result.status;
  const noRuntimeCommittedRevision: undefined = applyResult.result.operation_result.committed_revision;
  void notCommittedStatus;
  void noRuntimeCommittedRevision;
}
