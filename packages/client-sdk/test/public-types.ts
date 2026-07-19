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
  applyResult.committed_revision.revision_id;
} else {
  const noCommittedRevision: undefined = applyResult.committed_revision;
  void noCommittedRevision;
}
