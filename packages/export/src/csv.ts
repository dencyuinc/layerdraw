// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { ExportPlan, ExportRepresentation, ViewData } from "@layerdraw/protocol/semantic";
import { canonicalJSON } from "./canonical.js";
import type { ExportResourceLimits } from "./types.js";

function scalar(value: unknown): { text: string; explicitEmpty: boolean } {
  if (value === undefined || (value && typeof value === "object" && (value as {kind?: string}).kind === "absent"))
    return { text: "", explicitEmpty: false };
  if (value === "") return { text: "", explicitEmpty: true };
  if (value === null) return { text: "null", explicitEmpty: false };
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean")
    return { text: String(value), explicitEmpty: false };
  if (typeof value === "object") {
    const tagged = value as Record<string, unknown>;
    const kind = tagged.kind;
    if (kind === "string" || kind === "enum" || kind === "date" || kind === "datetime") return scalar(tagged.string_value);
    if (kind === "boolean") return scalar(tagged.boolean_value);
    if (kind === "integer") return scalar(tagged.integer_value);
    if (kind === "number") return scalar(tagged.number_value);
  }
  return { text: canonicalJSON(value), explicitEmpty: false };
}

function field(value: unknown, delimiter: string): string {
  const normalized = scalar(value);
  if (normalized.explicitEmpty) return '""';
  return normalized.text.includes(delimiter) || /["\r\n]/u.test(normalized.text)
    ? `"${normalized.text.replaceAll('"', '""')}"` : normalized.text;
}

const roleSchemas: Readonly<Record<string, readonly string[]>> = Object.freeze({
  "table:columns": ["viewdata_key", "id", "address", "label", "value_type", "enum_values", "source_column_addresses", "state_field_path", "source_refs"],
  "table:rows": ["viewdata_key", "cells", "source_refs"],
  "matrix:cells": ["viewdata_key", "row_key", "column_key", "semantic_refs", "display_value", "source_refs"],
  "matrix:row_axis": ["viewdata_key", "entity_address", "label", "source_refs"],
  "matrix:column_axis": ["viewdata_key", "entity_address", "label", "source_refs"],
});

function roleItems(view: ViewData, role: string): ReadonlyMap<string, Readonly<Record<string, unknown>>> | undefined {
  let items: readonly object[] | undefined;
  if (view.kind === "table" && view.table) {
    if (role === "columns") items = view.table.columns;
    else if (role === "rows") items = view.table.rows;
  } else if (view.kind === "matrix" && view.matrix) {
    if (role === "cells") items = view.matrix.cells;
    else if (role === "row_axis") items = view.matrix.row_axis;
    else if (role === "column_axis") items = view.matrix.column_axis;
  }
  if (!items) return undefined;
  return new Map(items.map((item) => {
    const record = item as Readonly<Record<string, unknown>>;
    return [String(record.key), record] as const;
  }));
}

function rowValue(column: string, item: Readonly<Record<string, unknown>>, representation: ExportRepresentation): unknown {
  if (column === "viewdata_key") return representation.viewdata_key;
  if (column === "source_refs") return representation.source;
  return item[column];
}

export function serializeCSV(plan: ExportPlan, view: ViewData, limits: ExportResourceLimits): Map<string, Uint8Array> {
  const delimiter = plan.format === "csv" ? "," : "\t";
  const encoder = new TextEncoder();
  const outputs = new Map<string, Uint8Array>();
  let totalRows = 0;
  for (const artifact of plan.artifacts) {
    const columns = roleSchemas[`${view.kind}:${artifact.role}`];
    const items = roleItems(view, artifact.role);
    if (!columns || !items) throw new TypeError("unsupported tabular artifact role");
    const representations = plan.representations.filter((item) => item.disposition === "tabular" && item.artifact_role === artifact.role);
    const rows = representations.map((representation) => {
      const item = items.get(representation.viewdata_key);
      if (!item) throw new TypeError("tabular representation has no ViewData item");
      return columns.map((column) => rowValue(column, item, representation));
    });
    totalRows += rows.length;
    if (totalRows > limits.max_csv_rows) throw new RangeError("CSV row limit");
    const lines: string[] = [];
    if (plan.serializer_options.header) lines.push(columns.map((key) => field(key, delimiter)).join(delimiter));
    for (const row of rows) lines.push(row.map((value) => field(value, delimiter)).join(delimiter));
    outputs.set(artifact.role, encoder.encode(`${lines.join("\n")}\n`));
  }
  return outputs;
}
