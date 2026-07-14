// SPDX-License-Identifier: Apache-2.0

import {
  isCompileRequestEnvelope,
  isCompileResponseEnvelope,
  isHandshakeResponseEnvelope,
} from "../src/engine.gen.js";
import type {
  CompileRequestEnvelope,
  CompileResponseEnvelope,
  HandshakeResponseEnvelope,
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
  {kind: "state", field_path: "review.status"},
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

export function narrowFixture(value: unknown):
  | CompileRequestEnvelope
  | CompileResponseEnvelope
  | HandshakeResponseEnvelope
  | undefined {
  if (isCompileRequestEnvelope(value)) return value;
  if (isCompileResponseEnvelope(value)) return value;
  if (isHandshakeResponseEnvelope(value)) return value;
  return undefined;
}
