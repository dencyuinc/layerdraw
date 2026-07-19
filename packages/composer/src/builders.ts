// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {
  isPreviewFragmentInput,
  isPreviewOperationsInput,
  isPreviewSourcePatchInput,
  type CreateEntityOperation,
  type CreateQueryOperation,
  type CreateQueryParameterOperation,
  type CreateRelationTypeOperation,
  type CreateViewExportOperation,
  type CreateViewOperation,
  type CreateViewTableColumnOperation,
  type EngineEditPreconditions,
  type NonCreateSemanticOperation,
  type PreviewFragmentInput,
  type PreviewOperationsInput,
  type PreviewSourcePatchInput,
  type SemanticOperation,
  type WorkbenchLimits,
} from "@layerdraw/protocol/engine";
import { ComposerContractError, type EditorEdit } from "./contracts.js";

export interface SemanticEditContext {
  readonly preconditions: EngineEditPreconditions;
  readonly limits: WorkbenchLimits;
}

export type EntityEditOperation = CreateEntityOperation;
export type RelationEditOperation = CreateRelationTypeOperation | NonCreateSemanticOperation;
export type RowEditOperation = NonCreateSemanticOperation;
export type QueryEditOperation = CreateQueryOperation | CreateQueryParameterOperation;
export type ViewEditOperation = CreateViewOperation | CreateViewTableColumnOperation | CreateViewExportOperation;

export function buildSemanticEdit(
  operations: readonly SemanticOperation[],
  context: SemanticEditContext,
): EditorEdit {
  const request: PreviewOperationsInput = {
    batch: { operations: [...operations] },
    limits: context.limits,
    preconditions: context.preconditions,
  };
  if (!isPreviewOperationsInput(request)) throw new ComposerContractError("Invalid semantic operation intent.");
  return { kind: "semantic_operations", request };
}

export const buildEntityEdit = (operation: EntityEditOperation, context: SemanticEditContext): EditorEdit =>
  buildSemanticEdit([operation], context);
export const buildRelationEdit = (operation: RelationEditOperation, context: SemanticEditContext): EditorEdit =>
  buildSemanticEdit([operation], context);
export const buildRowEdit = (operation: RowEditOperation, context: SemanticEditContext): EditorEdit =>
  buildSemanticEdit([operation], context);
export const buildQueryEdit = (operation: QueryEditOperation, context: SemanticEditContext): EditorEdit =>
  buildSemanticEdit([operation], context);
export const buildViewEdit = (operation: ViewEditOperation, context: SemanticEditContext): EditorEdit =>
  buildSemanticEdit([operation], context);

export function buildFragmentEdit(request: PreviewFragmentInput): EditorEdit {
  if (!isPreviewFragmentInput(request)) throw new ComposerContractError("Invalid source-fragment intent.");
  return { kind: "fragment", request };
}

export function buildSourcePatchEdit(request: PreviewSourcePatchInput): EditorEdit {
  if (!isPreviewSourcePatchInput(request)) throw new ComposerContractError("Invalid source-patch intent.");
  return { kind: "source_patch", request };
}
