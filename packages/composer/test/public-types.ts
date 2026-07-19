// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {
  buildEntityEdit,
  buildQueryEdit,
  buildRelationEdit,
  buildRowEdit,
  buildViewEdit,
  type ComposerApplyResult,
  type SemanticEditContext,
} from "../src/index.js";
import type {
  CreateEntityOperation,
  CreateQueryOperation,
  CreateRelationTypeOperation,
  CreateViewOperation,
  NonCreateSemanticOperation,
} from "@layerdraw/protocol/engine";

declare const context: SemanticEditContext;
declare const entity: CreateEntityOperation;
declare const relation: CreateRelationTypeOperation;
declare const row: NonCreateSemanticOperation;
declare const query: CreateQueryOperation;
declare const view: CreateViewOperation;
buildEntityEdit(entity, context);
buildRelationEdit(relation, context);
buildRowEdit(row, context);
buildQueryEdit(query, context);
buildViewEdit(view, context);

declare const result: ComposerApplyResult;
if (result.persistence === "durable") void result.committed_revision.revision_id;
else {
  const absent: undefined = result.committed_revision;
  void absent;
}
