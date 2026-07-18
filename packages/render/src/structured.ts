// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {
  assertRenderData,
  type ContextRenderData,
  type DiffRenderData,
  type MatrixRenderData,
  type RenderBounds,
  type RenderSourceBinding,
  type StableRenderDiagnostic,
  type TableRenderData,
} from "./index.js";
import {
  DEFAULT_VISUAL_SURFACE_LIMITS,
  VISUAL_SURFACE_HARD_LIMITS,
  VisualSurfaceError,
  type VisualHitTarget,
  type VisualInteractionMetadata,
  type VisualSurfaceLimits,
  type VisualSurfaceOptions,
} from "./visual.js";

export type StructuredVisualShape = "table" | "matrix" | "context" | "diff";

export interface StructuredVirtualWindow {
  readonly start: number;
  readonly count: number;
}

export interface StructuredVisualSurfaceOptions extends VisualSurfaceOptions {
  readonly virtual_window?: StructuredVirtualWindow;
}

export interface HeadlessSurfaceItem {
  readonly hit_target_id: string;
  readonly render_key: string;
  readonly role: string;
  readonly row_index?: number;
  readonly column_index?: number;
  readonly focus_order: number;
  readonly source_binding: RenderSourceBinding;
}

export interface StructuredNavigationModel {
  readonly mode: "grid" | "linear";
  readonly focus_order: readonly string[];
  readonly keys: readonly string[];
}

export interface StructuredVirtualizationModel {
  readonly axis: "row" | "group" | "change";
  readonly total_units: number;
  readonly rendered_start: number;
  readonly rendered_count: number;
  readonly before_count: number;
  readonly after_count: number;
}

export interface StructuredOverflowModel {
  readonly viewport_clipping: true;
  readonly label_mode: "clip" | "ellipsis";
  readonly max_label_scalars: number;
}

export interface StructuredVisualSurface<
  Shape extends StructuredVisualShape = StructuredVisualShape,
> {
  readonly visual_surface_schema_version: 1;
  readonly kind: Shape;
  readonly svg: string;
  readonly viewport: RenderBounds;
  readonly empty: boolean;
  readonly interaction: VisualInteractionMetadata;
  readonly headless_items: readonly HeadlessSurfaceItem[];
  readonly navigation: StructuredNavigationModel;
  readonly virtualization: StructuredVirtualizationModel;
  readonly overflow: StructuredOverflowModel;
  readonly diagnostics: readonly StableRenderDiagnostic[];
}

type StructuredData =
  | TableRenderData
  | MatrixRenderData
  | ContextRenderData
  | DiffRenderData;

interface Item {
  readonly render_key: string;
  readonly primitive_kind: string;
  readonly role: string;
  readonly label: string;
  readonly bounds: RenderBounds;
  readonly row_index?: number;
  readonly column_index?: number;
  readonly text?: string;
  readonly attributes?: string;
}

interface NormalizedOptions {
  readonly viewport?: RenderBounds;
  readonly accessibility_label?: string;
  readonly label_overflow: Readonly<{
    mode: "clip" | "ellipsis";
    max_scalars: number;
  }>;
  readonly limits: VisualSurfaceLimits;
  readonly virtual_window?: StructuredVirtualWindow;
}

class BoundedSvgWriter {
  private readonly encoder = new TextEncoder();
  private output = "";
  private bytes = 0;

  constructor(private readonly limit: number) {}

  push(value: string): void {
    const nextBytes = this.encoder.encode(value).byteLength;
    if (nextBytes > this.limit - this.bytes)
      resource("max_svg_bytes", this.limit);
    this.output += value;
    this.bytes += nextBytes;
  }

  finish(): string {
    return this.output;
  }
}

export function renderTableVisualSurface(
  data: TableRenderData,
  options?: StructuredVisualSurfaceOptions
): StructuredVisualSurface<"table"> {
  validateShape(data, "table");
  const normalized = normalizeOptions(options);
  const unitWindow = selectWindow(data.rows.length, normalized.virtual_window);
  const selectedRows = new Set(
    data.rows.slice(unitWindow.start, unitWindow.start + unitWindow.count).map((row) => row.render_key)
  );
  const columnIndex = new Map(data.columns.map((column, index) => [column.render_key, index]));
  const rowIndex = new Map(data.rows.map((row, index) => [row.render_key, index]));
  const items: Item[] = data.columns.map((column, index) => ({
    render_key: column.render_key,
    primitive_kind: "table.column",
    role: "columnheader",
    label: `${column.label}, ${column.value_type}${column.frozen ? ", frozen" : ""}`,
    bounds: column.header_bounds,
    column_index: index,
    text: column.label,
    attributes: ` data-column-id="${xml(column.id)}" data-value-type="${column.value_type}" data-frozen="${column.frozen}"`,
  }));
  for (const row of data.rows) {
    if (!selectedRows.has(row.render_key)) continue;
    const index = rowIndex.get(row.render_key)!;
    items.push({
      render_key: row.render_key,
      primitive_kind: "table.row",
      role: "row",
      label: `Row ${index + 1}${row.frozen ? ", frozen" : ""}`,
      bounds: rowBounds(row, data.columns),
      row_index: index,
      attributes: ` data-frozen="${row.frozen}"`,
    });
    for (const cell of data.cells) {
      if (cell.row_key !== row.render_key) continue;
      const col = columnIndex.get(cell.column_key);
      items.push({
        render_key: cell.render_key,
        primitive_kind: "table.cell",
        role: "gridcell",
        label: `Row ${index + 1}, column ${col === undefined ? cell.column_key : col + 1}: ${cell.text.length === 0 ? "empty" : cell.text}`,
        bounds: cell.bounds,
        row_index: index,
        ...(col === undefined ? {} : { column_index: col }),
        text: cell.text,
        attributes: ` data-row-key="${xml(cell.row_key)}" data-column-key="${xml(cell.column_key)}"`,
      });
    }
  }
  return buildSurface(data, items, normalized, unitWindow, "row", "grid");
}

export function renderMatrixVisualSurface(
  data: MatrixRenderData,
  options?: StructuredVisualSurfaceOptions
): StructuredVisualSurface<"matrix"> {
  validateShape(data, "matrix");
  const normalized = normalizeOptions(options);
  const unitWindow = selectWindow(data.row_axes.length, normalized.virtual_window);
  const rows = new Set(
    data.row_axes.slice(unitWindow.start, unitWindow.start + unitWindow.count).map((axis) => axis.render_key)
  );
  const rowIndex = new Map(data.row_axes.map((axis, index) => [axis.render_key, index]));
  const columnIndex = new Map(data.column_axes.map((axis, index) => [axis.render_key, index]));
  const items: Item[] = [];
  for (const [index, axis] of data.row_axes.entries())
    if (rows.has(axis.render_key))
      items.push({
        render_key: axis.render_key,
        primitive_kind: "matrix.row_axis",
        role: "rowheader",
        label: `Row axis ${axis.label}`,
        bounds: axis.bounds,
        row_index: index,
        text: axis.label,
      });
  for (const [index, axis] of data.column_axes.entries())
    items.push({
      render_key: axis.render_key,
      primitive_kind: "matrix.column_axis",
      role: "columnheader",
      label: `Column axis ${axis.label}`,
      bounds: axis.bounds,
      column_index: index,
      text: axis.label,
    });
  for (const cell of data.cells) {
    if (!rows.has(cell.row_axis_key)) continue;
    const row = rowIndex.get(cell.row_axis_key);
    const column = columnIndex.get(cell.column_axis_key);
    items.push({
      render_key: cell.render_key,
      primitive_kind: "matrix.cell",
      role: "gridcell",
      label: `Matrix row ${(row ?? -1) + 1}, column ${(column ?? -1) + 1}: ${cell.text.length === 0 ? "empty" : cell.text}`,
      bounds: cell.bounds,
      ...(row === undefined ? {} : { row_index: row }),
      ...(column === undefined ? {} : { column_index: column }),
      text: cell.text,
      attributes: ` data-row-axis-key="${xml(cell.row_axis_key)}" data-column-axis-key="${xml(cell.column_axis_key)}"`,
    });
  }
  for (const legend of data.legends ?? [])
    items.push({
      render_key: legend.render_key,
      primitive_kind: "matrix.legend",
      role: "note",
      label: `Legend ${legend.label}`,
      bounds: legend.bounds,
      text: legend.label,
    });
  for (const total of data.totals)
    items.push({
      render_key: total.render_key,
      primitive_kind: "matrix.total",
      role: "gridcell",
      label: `Supplied total ${total.text}`,
      bounds: total.bounds,
      text: total.text,
      attributes: ` data-axis-key="${xml(total.axis_key)}"`,
    });
  return buildSurface(data, items, normalized, unitWindow, "row", "grid");
}

export function renderContextVisualSurface(
  data: ContextRenderData,
  options?: StructuredVisualSurfaceOptions
): StructuredVisualSurface<"context"> {
  validateShape(data, "context");
  const normalized = normalizeOptions(options);
  const unitWindow = selectWindow(data.groups.length, normalized.virtual_window);
  const groups = new Set(
    data.groups.slice(unitWindow.start, unitWindow.start + unitWindow.count).map((group) => group.render_key)
  );
  const items: Item[] = [];
  for (const [index, group] of data.groups.entries())
    if (groups.has(group.render_key)) {
      items.push({
        render_key: group.render_key,
        primitive_kind: "context.group",
        role: "group",
        label: `Context group ${group.label}`,
        bounds: group.bounds,
        row_index: index,
        text: group.label,
      });
    }
  for (const [collection, kind, role] of [
    [data.facts, "fact", "listitem"],
    [data.relation_summaries, "relation_summary", "listitem"],
    [data.truncation_markers ?? [], "truncation_marker", "status"],
  ] as const)
    for (const primitive of collection)
      if (groups.has(primitive.group_key))
        items.push({
          render_key: primitive.render_key,
          primitive_kind: `context.${kind}`,
          role,
          label: `${kind.replaceAll("_", " ")} ${primitive.text}`,
          bounds: primitive.bounds,
          text: primitive.text,
          attributes: ` data-group-key="${xml(primitive.group_key)}"`,
        });
  return buildSurface(data, items, normalized, unitWindow, "group", "linear");
}

export function renderDiffVisualSurface(
  data: DiffRenderData,
  options?: StructuredVisualSurfaceOptions
): StructuredVisualSurface<"diff"> {
  validateShape(data, "diff");
  const normalized = normalizeOptions(options);
  const unitWindow = selectWindow(data.changes.length, normalized.virtual_window);
  const changes = data.changes.slice(unitWindow.start, unitWindow.start + unitWindow.count);
  const changeKeys = new Set(changes.map((change) => change.render_key));
  const sideKeys = new Set(
    changes.flatMap((change) => [change.before_key, change.after_key].filter((key): key is string => key !== undefined))
  );
  const changeIndex = new Map(data.changes.map((change, index) => [change.render_key, index]));
  const items: Item[] = [];
  for (const [collection, side] of [[data.before, "before"], [data.after, "after"]] as const)
    for (const primitive of collection)
      if (sideKeys.has(primitive.render_key))
        items.push({
          render_key: primitive.render_key,
          primitive_kind: `diff.${side}`,
          role: "cell",
          label: `${side} ${primitive.label}`,
          bounds: primitive.bounds,
          text: primitive.label,
        });
  for (const change of changes)
    items.push({
      render_key: change.render_key,
      primitive_kind: `diff.change.${change.change_kind}`,
      role: "row",
      label: `${change.change_kind.replaceAll("_", " ")} change`,
      bounds: change.bounds,
      ...(changeIndex.get(change.render_key) === undefined
        ? {}
        : { row_index: changeIndex.get(change.render_key)! }),
      text: change.change_kind,
      attributes: ` data-change-kind="${change.change_kind}"`,
    });
  for (const field of data.fields)
    if (changeKeys.has(field.change_key))
      items.push({
        render_key: field.render_key,
        primitive_kind: "diff.field",
        role: "cell",
        label: `${field.field_path}: before ${field.before_text ?? "absent"}; after ${field.after_text ?? "absent"}`,
        bounds: field.bounds,
        text: `${field.field_path}: ${field.before_text ?? "∅"} → ${field.after_text ?? "∅"}`,
        attributes: ` data-change-key="${xml(field.change_key)}" data-field-path="${xml(field.field_path)}"`,
      });
  return buildSurface(data, items, normalized, unitWindow, "change", "linear");
}

function buildSurface<Shape extends StructuredVisualShape>(
  data: StructuredData & Readonly<{ kind: Shape }>,
  items: readonly Item[],
  options: NormalizedOptions,
  unitWindow: Readonly<{ start: number; count: number; total: number }>,
  axis: StructuredVirtualizationModel["axis"],
  mode: StructuredNavigationModel["mode"]
): StructuredVisualSurface<Shape> {
  const viewport = options.viewport ?? positiveViewport(data.bounds);
  validateExtent(viewport, options.limits.max_extent);
  const accessibleLabel = options.accessibility_label ?? `${capitalize(data.kind)} visual`;
  preflightStructuredData(data, items, accessibleLabel, options.limits);
  const bindingByKey = new Map(data.source_bindings.map((binding) => [binding.render_key, binding]));
  const hitTargets: VisualHitTarget[] = [];
  const headlessItems: HeadlessSurfaceItem[] = [];
  const visible = (value: string): string =>
    options.label_overflow.mode === "clip"
      ? value
      : truncate(value, options.label_overflow.max_scalars);
  const empty = unitWindow.total === 0;
  const writer = new BoundedSvgWriter(options.limits.max_svg_bytes);
  writer.push(`<svg xmlns="http://www.w3.org/2000/svg" role="${mode === "grid" ? "grid" : "document"}" aria-label="${xml(accessibleLabel)}" viewBox="${number(viewport.x)} ${number(viewport.y)} ${number(viewport.width)} ${number(viewport.height)}" width="${number(viewport.width)}" height="${number(viewport.height)}" overflow="hidden"><title>${xml(accessibleLabel)}</title><style>g>rect{fill:#fff;stroke:#64748b;vector-effect:non-scaling-stroke}g[data-primitive-kind*="added"]>rect{fill:#dcfce7}g[data-primitive-kind*="removed"]>rect{fill:#fee2e2}g[data-primitive-kind*="updated"]>rect{fill:#fef3c7}g[data-primitive-kind*="moved"]>rect{fill:#dbeafe}g[data-primitive-kind*="unchanged"]>rect{fill:#f8fafc}text{fill:#0f172a;font:12px sans-serif;pointer-events:none}</style>`);
  for (const [focusOrder, item] of items.entries()) {
    const binding = bindingByKey.get(item.render_key);
    if (binding === undefined)
      invalid(`missing source binding for ${item.render_key}`);
    const target: VisualHitTarget = {
      hit_target_id: `hit:${item.render_key}`,
      render_key: item.render_key,
      primitive_kind: item.primitive_kind,
      accessibility_label: item.label,
      focus_order: focusOrder,
      source_binding: binding,
    };
    hitTargets.push(target);
    headlessItems.push({
      hit_target_id: target.hit_target_id,
      render_key: item.render_key,
      role: item.role,
      ...(item.row_index === undefined ? {} : { row_index: item.row_index }),
      ...(item.column_index === undefined ? {} : { column_index: item.column_index }),
      focus_order: focusOrder,
      source_binding: binding,
    });
    const text = item.text === undefined ? "" : visible(item.text);
    writer.push(`<g data-hit-target-id="${xml(target.hit_target_id)}" data-render-key="${xml(item.render_key)}" data-primitive-kind="${xml(item.primitive_kind)}" role="${item.role}" aria-label="${xml(item.label)}" tabindex="0"${item.row_index === undefined ? "" : ` aria-rowindex="${item.row_index + 1}"`}${item.column_index === undefined ? "" : ` aria-colindex="${item.column_index + 1}"`}${item.attributes ?? ""}><title>${xml(item.label)}</title><rect x="${number(item.bounds.x)}" y="${number(item.bounds.y)}" width="${number(item.bounds.width)}" height="${number(item.bounds.height)}"/><text x="${number(item.bounds.x)}" y="${number(item.bounds.y)}" dominant-baseline="hanging">${xml(text)}</text>${text === (item.text ?? "") ? "" : `<title>${xml(item.text ?? "")}</title>`}</g>`);
  }
  if (empty)
    writer.push(`<g role="status" aria-label="Empty ${data.kind} visual"><text x="${number(viewport.x)}" y="${number(viewport.y)}">Empty ${data.kind} visual</text></g>`);
  writer.push("</svg>");
  const svg = writer.finish();
  return {
    visual_surface_schema_version: 1,
    kind: data.kind,
    svg,
    viewport,
    empty,
    interaction: {
      hit_targets: hitTargets,
      focus_order: hitTargets.map((target) => target.hit_target_id),
      source_bindings: hitTargets.map((target) => target.source_binding),
    },
    headless_items: headlessItems,
    navigation: {
      mode,
      focus_order: hitTargets.map((target) => target.hit_target_id),
      keys: mode === "grid" ? ["ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "Home", "End"] : ["ArrowUp", "ArrowDown", "Home", "End"],
    },
    virtualization: {
      axis,
      total_units: unitWindow.total,
      rendered_start: unitWindow.start,
      rendered_count: unitWindow.count,
      before_count: unitWindow.start,
      after_count: unitWindow.total - unitWindow.start - unitWindow.count,
    },
    overflow: {
      viewport_clipping: true,
      label_mode: options.label_overflow.mode,
      max_label_scalars: options.label_overflow.max_scalars,
    },
    diagnostics: data.diagnostics,
  };
}

function preflightStructuredData(
  data: StructuredData,
  visibleItems: readonly Item[],
  accessibleLabel: string,
  limits: VisualSurfaceLimits
): void {
  validateExtent(data.bounds, limits.max_extent);
  const collections = structuredCollections(data);
  const primitiveCount = collections.reduce(
    (total, [, primitives]) => total + primitives.length,
    0
  );
  if (primitiveCount > limits.max_primitives)
    resource("max_primitives", limits.max_primitives);

  const strings: string[] = [
    accessibleLabel,
    `Empty ${data.kind} visual`,
    data.kind,
  ];
  for (const [field, primitives] of collections) {
    for (const primitive of primitives) {
      if (!record(primitive)) invalid(`${field} primitive is invalid`);
      if (primitive.bounds !== undefined)
        validateExtent(primitive.bounds as RenderBounds, limits.max_extent);
      if (primitive.header_bounds !== undefined)
        validateExtent(
          primitive.header_bounds as RenderBounds,
          limits.max_extent
        );
      if (finite(primitive.x) && finite(primitive.width))
        validateExtent(
          { x: primitive.x, y: 0, width: primitive.width, height: 0 },
          limits.max_extent
        );
      if (finite(primitive.y) && finite(primitive.height))
        validateExtent(
          { x: 0, y: primitive.y, width: 0, height: primitive.height },
          limits.max_extent
        );
      strings.push(field, ...structuralStrings(primitive));
    }
  }
  for (const item of visibleItems)
    strings.push(
      item.render_key,
      item.primitive_kind,
      item.role,
      item.label,
      item.text ?? "",
      item.attributes ?? ""
    );
  validateEmittedStrings(
    strings,
    limits.max_text_scalars,
    limits.max_svg_bytes
  );
}

function structuredCollections(
  data: StructuredData
): readonly (readonly [string, readonly unknown[]])[] {
  switch (data.kind) {
    case "table":
      return [
        ["columns", data.columns],
        ["rows", data.rows],
        ["cells", data.cells],
      ];
    case "matrix":
      return [
        ["row_axes", data.row_axes],
        ["column_axes", data.column_axes],
        ["cells", data.cells],
        ["legends", data.legends ?? []],
        ["totals", data.totals],
      ];
    case "context":
      return [
        ["groups", data.groups],
        ["facts", data.facts],
        ["relation_summaries", data.relation_summaries],
        ["truncation_markers", data.truncation_markers ?? []],
      ];
    case "diff":
      return [
        ["before", data.before],
        ["after", data.after],
        ["changes", data.changes],
        ["fields", data.fields],
      ];
  }
}

function structuralStrings(value: Record<string, unknown>): string[] {
  const strings: string[] = [];
  for (const [key, item] of Object.entries(value)) {
    if (typeof item === "string") strings.push(key, item);
    else if (typeof item === "boolean") strings.push(key, String(item));
    else if (Array.isArray(item))
      for (const entry of item)
        if (typeof entry === "string") strings.push(key, entry);
  }
  return strings;
}

function validateEmittedStrings(
  values: readonly string[],
  scalarLimit: number,
  byteLimit: number
): void {
  let totalScalars = 0;
  let totalBytes = 0;
  for (const value of values) {
    const scalars = scalarCount(value, scalarLimit - totalScalars);
    if (scalars > scalarLimit - totalScalars)
      resource("max_text_scalars", scalarLimit);
    totalScalars += scalars;
    const bytes = escapedXmlByteCount(value, byteLimit - totalBytes);
    if (bytes > byteLimit - totalBytes)
      resource("max_svg_bytes", byteLimit);
    totalBytes += bytes;
  }
}

function scalarCount(value: string, remaining: number): number {
  let count = 0;
  for (const _scalar of value) {
    count += 1;
    if (count > remaining) return count;
  }
  return count;
}

function escapedXmlByteCount(value: string, remaining: number): number {
  let count = 0;
  for (const scalar of value) {
    if (scalar === "&") count += 5;
    else if (scalar === "<" || scalar === ">") count += 4;
    else if (scalar === '"' || scalar === "'") count += 6;
    else {
      const codePoint = scalar.codePointAt(0)!;
      count +=
        codePoint <= 0x7f
          ? 1
          : codePoint <= 0x7ff
            ? 2
            : codePoint <= 0xffff
              ? 3
              : 4;
    }
    if (count > remaining) return count;
  }
  return count;
}

function normalizeOptions(options: StructuredVisualSurfaceOptions | undefined): NormalizedOptions {
  if (options === undefined)
    return {
      label_overflow: { mode: "clip", max_scalars: 4096 },
      limits: DEFAULT_VISUAL_SURFACE_LIMITS,
    };
  if (!record(options) || !onlyKeys(options, ["viewport", "accessibility_label", "label_overflow", "limits", "virtual_window"]))
    option("options must be a closed object");
  if (options.accessibility_label !== undefined &&
      (typeof options.accessibility_label !== "string" || options.accessibility_label.length === 0))
    option("accessibility_label must be nonempty");
  if (options.viewport !== undefined) validateBounds(options.viewport, true);
  const overflow = options.label_overflow ?? { mode: "clip" as const, max_scalars: 4096 };
  if (!record(overflow) || !onlyKeys(overflow, ["mode", "max_scalars"]) ||
      !["clip", "ellipsis"].includes(String(overflow.mode)) || !safePositive(overflow.max_scalars))
    option("label_overflow is invalid");
  const limits = options.limits ?? DEFAULT_VISUAL_SURFACE_LIMITS;
  const limitKeys = ["max_primitives", "max_route_points", "max_text_scalars", "max_svg_bytes", "max_extent"] as const;
  if (!record(limits) || !onlyKeys(limits, limitKeys) ||
      !limitKeys.every((key) => Object.hasOwn(limits, key) && safePositive(limits[key]) && Number(limits[key]) <= VISUAL_SURFACE_HARD_LIMITS[key]))
    option("limits are invalid or exceed package hard limits");
  if (options.virtual_window !== undefined) {
    const virtualWindow: unknown = options.virtual_window;
    if (!record(virtualWindow) || !onlyKeys(virtualWindow, ["start", "count"]) ||
        !Number.isSafeInteger(virtualWindow.start) || Number(virtualWindow.start) < 0 || !safePositive(virtualWindow.count))
      option("virtual_window is invalid");
  }
  return {
    ...(options.viewport === undefined ? {} : { viewport: options.viewport }),
    ...(options.accessibility_label === undefined ? {} : { accessibility_label: options.accessibility_label }),
    label_overflow: overflow as Readonly<{ mode: "clip" | "ellipsis"; max_scalars: number }>,
    limits: limits as unknown as VisualSurfaceLimits,
    ...(options.virtual_window === undefined
      ? {}
      : { virtual_window: options.virtual_window as StructuredVirtualWindow }),
  };
}

function selectWindow(total: number, requested: StructuredVirtualWindow | undefined) {
  if (requested === undefined) return { start: 0, count: total, total };
  const start = Math.min(requested.start, total);
  return { start, count: Math.min(requested.count, total - start), total };
}

function validateShape(value: unknown, expected: StructuredVisualShape): asserts value is StructuredData {
  try {
    assertRenderData(value);
  } catch (error) {
    invalid(error instanceof Error ? error.message : "invalid RenderData");
  }
  if ((value as StructuredData).kind !== expected) invalid(`expected ${expected} RenderData`);
}

function rowBounds(row: TableRenderData["rows"][number], columns: TableRenderData["columns"]): RenderBounds {
  const x = columns.length === 0 ? 0 : Math.min(...columns.map((column) => column.x));
  const end = columns.length === 0 ? x : Math.max(...columns.map((column) => column.x + column.width));
  return { x, y: row.y, width: end - x, height: row.height };
}

function validateBounds(value: unknown, positive: boolean): asserts value is RenderBounds {
  if (!record(value) || !onlyKeys(value, ["x", "y", "width", "height"]) ||
      ![value.x, value.y, value.width, value.height].every(finite) ||
      Number(value.width) < (positive ? Number.MIN_VALUE : 0) || Number(value.height) < (positive ? Number.MIN_VALUE : 0))
    option("bounds are invalid");
}

function validateExtent(bounds: RenderBounds, limit: number): void {
  validateBounds(bounds, false);
  if ([bounds.x, bounds.y, bounds.width, bounds.height, bounds.x + bounds.width, bounds.y + bounds.height]
      .some((value) => Math.abs(value) > limit))
    resource("max_extent", limit);
}

function positiveViewport(bounds: RenderBounds): RenderBounds {
  return { ...bounds, width: bounds.width === 0 ? 1 : bounds.width, height: bounds.height === 0 ? 1 : bounds.height };
}

function truncate(value: string, limit: number): string {
  const scalars = Array.from(value);
  return scalars.length <= limit ? value : `${scalars.slice(0, Math.max(0, limit - 1)).join("")}…`;
}

function xml(value: string): string {
  return value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;").replaceAll("'", "&apos;");
}

function number(value: number): string {
  return Object.is(value, -0) ? "0" : String(value);
}

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function onlyKeys(value: Record<string, unknown>, keys: readonly string[]): boolean {
  return Object.keys(value).every((key) => keys.includes(key));
}

function finite(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

function safePositive(value: unknown): value is number {
  return Number.isSafeInteger(value) && Number(value) > 0;
}

function option(message: string): never {
  throw new VisualSurfaceError("render.visual_options_invalid", message);
}

function invalid(message: string): never {
  throw new VisualSurfaceError("render.visual_data_invalid", message);
}

function resource(name: string, limit: number): never {
  throw new VisualSurfaceError("render.visual_resource_limit", `${name} exceeds ${limit}`);
}

function capitalize(value: string): string {
  return `${value.slice(0, 1).toUpperCase()}${value.slice(1)}`;
}
