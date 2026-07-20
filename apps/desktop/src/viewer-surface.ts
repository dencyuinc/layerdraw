// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { RenderBounds, RenderData } from "@layerdraw/render";
import type { ViewerPublication, ViewerState } from "@layerdraw/viewer";
import { createElement, type KeyboardEvent, type ReactNode } from "react";

export interface DesktopViewerSurfaceProps {
  readonly state: ViewerState;
  readonly onSelectionChange: (keys: readonly string[]) => void;
}

function activate(event: KeyboardEvent, callback: () => void): void {
  if (event.key !== "Enter" && event.key !== " ") return;
  event.preventDefault();
  callback();
}

function rect(key: string, bounds: RenderBounds, label: string, selected: boolean, onSelect: () => void): ReactNode {
  return createElement("g", {
    key, role: "button", tabIndex: 0, "aria-label": label, "aria-pressed": selected,
    className: "ld-desktop-viewer-item", "data-selected": selected,
    onClick: onSelect, onKeyDown: (event: KeyboardEvent) => activate(event, onSelect),
  },
  createElement("rect", { x: bounds.x, y: bounds.y, width: bounds.width, height: bounds.height, rx: 8 }),
  createElement("text", { x: bounds.x + 10, y: bounds.y + 22 }, label));
}

function path(key: string, points: readonly Readonly<{ x: number; y: number }>[]): ReactNode {
  return createElement("polyline", { key, points: points.map((point) => `${point.x},${point.y}`).join(" "), className: "ld-desktop-viewer-path" });
}

function visualItems(data: Exclude<RenderData, { kind: "table" }>): readonly Readonly<{ key: string; bounds: RenderBounds; label: string }>[] {
  if (data.kind === "diagram") return [
    ...data.containers.map((item) => ({ key: item.render_key, bounds: item.bounds, label: item.render_key })),
    ...data.occurrences.map((item) => ({ key: item.render_key, bounds: item.bounds, label: data.labels.find((label) => label.anchor.kind === "occurrence" && label.anchor.occurrence_key === item.render_key)?.text ?? item.render_key })),
  ];
  if (data.kind === "tree") return data.occurrences.map((item) => ({ key: item.render_key, bounds: item.bounds, label: item.label }));
  if (data.kind === "flow") return [...data.lanes.map((item) => ({ key: item.render_key, bounds: item.bounds, label: item.label })), ...data.steps.map((item) => ({ key: item.render_key, bounds: item.bounds, label: item.label }))];
  if (data.kind === "matrix") return [...data.row_axes, ...data.column_axes, ...data.cells, ...data.totals].map((item) => ({ key: item.render_key, bounds: item.bounds, label: "text" in item ? item.text : item.label }));
  if (data.kind === "context") return [...data.groups, ...data.facts, ...data.relation_summaries, ...(data.truncation_markers ?? [])].map((item) => ({ key: item.render_key, bounds: item.bounds, label: "text" in item ? item.text : item.label }));
  return [...data.before, ...data.after, ...data.changes, ...data.fields].map((item) => ({ key: item.render_key, bounds: item.bounds, label: "label" in item ? item.label : "field_path" in item ? item.field_path : item.change_kind }));
}

function table(publication: ViewerPublication, onSelectionChange: (keys: readonly string[]) => void): ReactNode {
  const data = publication.render_data;
  if (data.kind !== "table") return null;
  const selected = new Set(publication.presentation.selection_keys);
  return createElement("div", { className: "ld-desktop-table-scroll", tabIndex: 0 },
    createElement("table", null,
      createElement("thead", null, createElement("tr", null, data.columns.map((column) => createElement("th", { key: column.render_key, scope: "col" }, column.label)))),
      createElement("tbody", null, data.rows.map((row) => createElement("tr", { key: row.render_key }, data.columns.map((column) => {
        const cell = data.cells.find((candidate) => candidate.row_key === row.render_key && candidate.column_key === column.render_key);
        return createElement("td", { key: column.render_key }, cell === undefined ? null : createElement("button", {
          type: "button", "aria-pressed": selected.has(cell.render_key), onClick: () => onSelectionChange([cell.render_key]),
        }, cell.text));
      }))))));
}

function publicationSurface(publication: ViewerPublication, onSelectionChange: (keys: readonly string[]) => void): ReactNode {
  const data = publication.render_data;
  if (data.kind === "table") return table(publication, onSelectionChange);
  const selected = new Set(publication.presentation.selection_keys);
  const connections = data.kind === "diagram" ? data.edge_paths.map((item) => path(item.render_key, item.points))
    : data.kind === "tree" ? [...data.duplicate_refs, ...data.cycle_refs].map((item) => path(item.render_key, item.points))
      : data.kind === "flow" ? [...data.connectors, ...data.cycle_refs].map((item) => path(item.render_key, item.points)) : [];
  return createElement("svg", {
    className: "ld-desktop-viewer-canvas", role: "group", "aria-label": `${data.kind} view`,
    viewBox: `${data.bounds.x} ${data.bounds.y} ${Math.max(data.bounds.width, 1)} ${Math.max(data.bounds.height, 1)}`,
  }, connections, visualItems(data).map((item) => rect(item.key, item.bounds, item.label, selected.has(item.key), () => onSelectionChange([item.key]))));
}

export function DesktopViewerSurface({ state, onSelectionChange }: DesktopViewerSurfaceProps): ReactNode {
  if (state.status === "loading" || state.status === "cancelling") return createElement("p", { role: "status", "aria-live": "polite" }, "Loading view…");
  if (state.status === "empty") return createElement("p", { className: "ld-desktop-empty" }, state.reason === "view_empty" ? "This view is empty." : "Select a view to begin.");
  if (state.status === "disposed") return createElement("p", { role: "status" }, "Viewer closed.");
  const publication = "publication" in state ? state.publication : state.previous;
  return createElement("div", { className: "ld-desktop-viewer", "data-viewer-state": state.status },
    publication === undefined ? null : publicationSurface(publication, onSelectionChange),
    state.status === "ready" ? null : createElement("p", { role: "status", className: "ld-desktop-viewer-status" }, "The view needs attention."));
}
