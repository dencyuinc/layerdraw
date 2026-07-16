// SPDX-License-Identifier: Apache-2.0

import {
  isCompileRequestEnvelope,
  isCompileResponseEnvelope,
  isHandshakeRequestEnvelope,
  isHandshakeResponseEnvelope,
} from "../src/engine.gen.js";
import type {
  ColumnCreateSubjectFields,
  CompileRequestEnvelope,
  CompileResponseEnvelope,
  CreateEntityTypeColumnOperation,
  CreateViewExportOperation,
  HandshakeRequestEnvelope,
  HandshakeResponseEnvelope,
  QueryParameterCreateSubjectFields,
  ViewExportCreateSubjectFields,
} from "../src/engine.gen.js";
import type {
  ViewRecipeSource,
  ViewTableColumnSource,
} from "../src/semantic.gen.js";

export const typedViewTableColumnSources: ReadonlyArray<ViewTableColumnSource> = [
  {kind: "field", field: "tags"},
  {kind: "attribute", column_addresses: ["ldl:project:p:entity-type:t:column:c"]},
  {kind: "relation_endpoint", endpoint: "from", field: "display_name"},
  {kind: "derived_count", direction: "both", relation_type_addresses: ["ldl:project:p:relation-type:r"]},
  {kind: "state", field_path: "system.updated_at"},
];

// These are intentionally representable host structs; generated encoders must
// reject them using the schema authority rather than trusting static typing.
export const typedInvalidViewTableColumnSources: ReadonlyArray<ViewTableColumnSource> = [
  {kind: "field"},
  {kind: "attribute", column_addresses: [], field: "id"},
  {kind: "relation_endpoint", endpoint: "from", field: "description"},
  {kind: "derived_count"},
  {kind: "state"},
];

const typedArguments = {"ldl:project:p:query:q:parameter:x": {kind: "string", string_value: "x"}} as const;
export const typedValidDiffSources: ReadonlyArray<ViewRecipeSource> = [
  {kind: "diff", before: "base", after: "head", arguments: {}},
  {kind: "diff", before: "base", after: "head", query_address: "ldl:project:p:query:q", arguments: typedArguments},
];
export const typedInvalidDiffSources: ReadonlyArray<ViewRecipeSource> = [
  {kind: "diff", after: "head", arguments: {}},
  {kind: "diff", before: "base", arguments: {}},
  {kind: "diff", before: "", after: "head", arguments: {}},
  {kind: "diff", before: "base", after: "", arguments: {}},
  {kind: "diff", before: "same", after: "same", arguments: {}},
  {kind: "diff", before: "base", after: "head", arguments: typedArguments},
];

export const typedColumnCreateFields: ColumnCreateSubjectFields = {display_name: "Email", value_type: "string", format: "email"};
export const typedQueryParameterCreateFields: QueryParameterCreateSubjectFields = {value_type: "string", format: "uri"};
export const typedViewExportCreateFields: ViewExportCreateSubjectFields = {format: "json", filename: "view.json", fidelity: "lossless"};
export const typedColumnCreateOperation: CreateEntityTypeColumnOperation = {operation: "create_subject", parent_address: "ldl:project:p:entity-type:t", subject_kind: "entity_type_column", id: "email", fields: typedColumnCreateFields};
export const typedViewExportCreateOperation: CreateViewExportOperation = {operation: "create_subject", parent_address: "ldl:project:p:view:v", subject_kind: "view_export", id: "json", fields: typedViewExportCreateFields};
// @ts-expect-error generated Column create fields retain semantic.StringFormat rather than string
export const typedInvalidColumnFormat: ColumnCreateSubjectFields = {display_name: "Bad", value_type: "string", format: "garbage"};
// @ts-expect-error generated View Export create fields retain semantic.ExportFormat rather than string
export const typedInvalidExportFormat: ViewExportCreateSubjectFields = {format: "garbage", filename: "bad.out", fidelity: "lossless"};

export function narrowFixture(value: unknown):
  | CompileRequestEnvelope
  | CompileResponseEnvelope
  | HandshakeRequestEnvelope
  | HandshakeResponseEnvelope
  | undefined {
  if (isCompileRequestEnvelope(value)) return value;
  if (isCompileResponseEnvelope(value)) return value;
  if (isHandshakeRequestEnvelope(value)) return value;
  if (isHandshakeResponseEnvelope(value)) return value;
  return undefined;
}
