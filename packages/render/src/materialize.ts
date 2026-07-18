// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import { type Digest, type JsonValue } from "@layerdraw/protocol/common";
import {
  encodeViewData,
  isViewData,
  type ContextAttribute,
  type ContextFact,
  type DiagramOccurrence,
  type Diagnostic,
  type DiagnosticArgumentValue,
  type DiffChange,
  type FlowConnector,
  type FlowCycleRef,
  type FlowViewData,
  type MatrixCell,
  type MatrixDisplayValue,
  type RecipeScalar,
  type TableCell,
  type TableColumn,
  type TableRow,
  type ViewData,
  type ViewDataItemKey,
  type ViewDataSemanticValue,
  type ViewDataSourceRefs,
  type ViewDataValue,
} from "@layerdraw/protocol/semantic";
import {
  assertRenderData,
  assertRenderRecipe,
  RenderContractError,
  type AlgorithmOptions,
  type ContextRenderData,
  type DiagramRenderData,
  type DiffRenderData,
  type FlowRenderData,
  type MatrixRenderData,
  type RenderBounds,
  type RenderData,
  type RendererProfileRef,
  type RenderPoint,
  type RenderRecipe,
  type RenderShape,
  type RenderSourceBinding,
  type StableRenderDiagnostic,
  type TableRenderData,
  type TreeRenderData,
} from "./index.js";

export type RenderAlgorithmKind = AlgorithmOptions["kind"];

export interface ResolvedRendererProfile {
  readonly renderer_profile: RendererProfileRef;
  readonly supported_shapes: readonly RenderShape[];
  readonly supported_algorithms: readonly RenderAlgorithmKind[];
}

export interface ResolvedFontMetrics {
  readonly family: string;
  readonly digest: Digest;
  readonly units_per_em: number;
  readonly ascent: number;
  readonly descent: number;
  readonly line_gap: number;
  readonly default_advance: number;
  readonly glyph_advances: Readonly<Record<string, number>>;
}

export interface ResolvedAssetDimensions {
  readonly digest: Digest;
  readonly width: number;
  readonly height: number;
}

export interface RenderLayoutPolicy {
  readonly item_order: "viewdata";
  readonly tie_breaker: "seeded_key_hash";
  readonly coordinate_precision: number;
  readonly item_width: number;
  readonly item_height: number;
  readonly horizontal_gap: number;
  readonly vertical_gap: number;
  readonly container_padding: number;
  readonly cell_padding: number;
  readonly lane_header_size: number;
  readonly badge_size: number;
  readonly overlay_offset: number;
  readonly port_offset: number;
  readonly font_size: number;
  readonly asset_scale: number;
}

export interface RenderResourceLimits {
  readonly max_primitives: number;
  readonly max_route_points: number;
  readonly max_text_length: number;
  readonly max_depth: number;
  readonly max_extent: number;
}

export interface RenderMaterializationInput {
  readonly view_data: ViewData;
  readonly view_data_hash: Digest;
  readonly recipe: RenderRecipe;
  readonly resolved_profile: ResolvedRendererProfile;
  readonly resolved_fonts: readonly ResolvedFontMetrics[];
  readonly resolved_assets: readonly ResolvedAssetDimensions[];
  readonly layout: RenderLayoutPolicy;
  readonly limits: RenderResourceLimits;
}

export type RenderMaterializationDiagnosticCode =
  | "render.input_invalid"
  | "render.profile_unsupported"
  | "render.profile_incompatible"
  | "render.font_missing"
  | "render.asset_missing"
  | "render.geometry_invalid"
  | "render.resource_limit";

export type RenderMaterializationDiagnostic = StableRenderDiagnostic &
  Readonly<{
    code: RenderMaterializationDiagnosticCode;
  }>;

export type RenderMaterializationResult =
  | Readonly<{
      ok: true;
      data: RenderData;
      diagnostics: readonly RenderMaterializationDiagnostic[];
    }>
  | Readonly<{
      ok: false;
      diagnostics: readonly RenderMaterializationDiagnostic[];
    }>;

const digestPattern = /^sha256:[0-9a-f]{64}$/;
const shapeOrder: readonly RenderShape[] = [
  "context",
  "diagram",
  "diff",
  "flow",
  "matrix",
  "table",
  "tree",
];
const algorithmOrder: readonly RenderAlgorithmKind[] = [
  "grid",
  "layered",
  "native",
  "radial",
];

class MaterializationFailure extends Error {
  constructor(readonly diagnostic: RenderMaterializationDiagnostic) {
    super(diagnostic.code);
  }
}

class BuildContext {
  readonly bindings: RenderSourceBinding[] = [];
  primitiveCount = 0;
  routePointCount = 0;

  constructor(
    readonly input: RenderMaterializationInput,
    readonly renderInputHash: Digest,
    readonly font: ResolvedFontMetrics
  ) {}

  bind(
    renderKey: string,
    viewdataKey: ViewDataItemKey,
    source: ViewDataSourceRefs
  ): void {
    this.primitiveCount += 1;
    if (this.primitiveCount > this.input.limits.max_primitives)
      fail(
        "render.resource_limit",
        "max_primitives",
        this.input.limits.max_primitives
      );
    this.bindings.push({
      render_key: renderKey,
      viewdata_key: viewdataKey,
      source_refs: source,
    });
  }

  points(points: readonly RenderPoint[]): readonly RenderPoint[] {
    this.routePointCount += points.length;
    if (this.routePointCount > this.input.limits.max_route_points)
      fail(
        "render.resource_limit",
        "max_route_points",
        this.input.limits.max_route_points
      );
    return points.map((point) => ({
      x: this.number(point.x),
      y: this.number(point.y),
    }));
  }

  number(value: number): number {
    if (!Number.isFinite(value))
      fail("render.geometry_invalid", "non_finite_coordinate", 0);
    const factor = 10 ** this.input.layout.coordinate_precision;
    const rounded = Math.round(value * factor) / factor;
    return Object.is(rounded, -0) ? 0 : rounded;
  }

  text(value: string): string {
    if (Array.from(value).length > this.input.limits.max_text_length)
      fail(
        "render.resource_limit",
        "max_text_length",
        this.input.limits.max_text_length
      );
    return value;
  }

  measure(value: string): Readonly<{ width: number; height: number }> {
    this.text(value);
    let advance = 0;
    for (const scalar of value)
      advance += this.font.glyph_advances[scalar] ?? this.font.default_advance;
    const scale = this.input.layout.font_size / this.font.units_per_em;
    return {
      width: this.number(advance * scale),
      height: this.number(
        (this.font.ascent + this.font.descent + this.font.line_gap) * scale
      ),
    };
  }

  rect(x: number, y: number, width: number, height: number): RenderBounds {
    if (width < 0 || height < 0)
      fail("render.geometry_invalid", "negative_extent", 0);
    return {
      x: this.number(x),
      y: this.number(y),
      width: this.number(width),
      height: this.number(height),
    };
  }
}

export async function materializeRenderData(
  input: RenderMaterializationInput
): Promise<RenderMaterializationResult> {
  const diagnostics = validateInput(input);
  if (diagnostics.length > 0) return { ok: false, diagnostics };
  try {
    preflight(input);
    const renderInputHash = await hashMaterializationInput(input);
    const selectedFamily = input.recipe.font_policy.families[0]!;
    const context = new BuildContext(
      input,
      renderInputHash,
      input.resolved_fonts.find((font) => font.family === selectedFamily)!
    );
    const data = build(context);
    if (
      data.bounds.width > input.limits.max_extent ||
      data.bounds.height > input.limits.max_extent
    ) {
      fail("render.resource_limit", "max_extent", input.limits.max_extent);
    }
    assertRenderData(data);
    return { ok: true, data, diagnostics: [] };
  } catch (error) {
    if (error instanceof MaterializationFailure)
      return { ok: false, diagnostics: [error.diagnostic] };
    const reason = error instanceof Error ? error.message : "unknown";
    return {
      ok: false,
      diagnostics: [diagnostic("render.geometry_invalid", { reason })],
    };
  }
}

export async function hashMaterializationInput(
  input: RenderMaterializationInput
): Promise<Digest> {
  const payload = {
    view_data_hash: input.view_data_hash,
    view_data_canonical: encodeViewData(input.view_data),
    recipe: input.recipe,
    resolved_profile: input.resolved_profile,
    resolved_fonts: input.resolved_fonts,
    resolved_assets: input.resolved_assets,
    layout: input.layout,
    limits: input.limits,
  };
  const bytes = new TextEncoder().encode(
    `layerdraw-render-1\0materialization-input\0${canonical(payload)}`
  );
  const hashed = new Uint8Array(await crypto.subtle.digest("SHA-256", bytes));
  return `sha256:${Array.from(hashed, (byte) =>
    byte.toString(16).padStart(2, "0")
  ).join("")}` as Digest;
}

function validateInput(
  input: RenderMaterializationInput
): RenderMaterializationDiagnostic[] {
  const diagnostics: RenderMaterializationDiagnostic[] = [];
  if (input === null || typeof input !== "object")
    return [diagnostic("render.input_invalid", { field: "input" })];
  if (
    !closedObject(input, [
      "view_data",
      "view_data_hash",
      "recipe",
      "resolved_profile",
      "resolved_fonts",
      "resolved_assets",
      "layout",
      "limits",
    ])
  )
    return [diagnostic("render.input_invalid", { field: "input" })];
  try {
    assertRenderRecipe(input.recipe);
  } catch (error) {
    const reason =
      error instanceof RenderContractError ? error.code : "invalid_recipe";
    diagnostics.push(
      diagnostic(
        reason === "render.profile_incompatible"
          ? "render.profile_incompatible"
          : "render.input_invalid",
        { field: "recipe", reason }
      )
    );
  }
  if (!isViewData(input.view_data))
    diagnostics.push(
      diagnostic("render.input_invalid", { field: "view_data" })
    );
  if (!isDigest(input.view_data_hash))
    diagnostics.push(
      diagnostic("render.input_invalid", { field: "view_data_hash" })
    );
  if (!validProfile(input.resolved_profile))
    diagnostics.push(
      diagnostic("render.input_invalid", { field: "resolved_profile" })
    );
  if (!validLayout(input.layout))
    diagnostics.push(diagnostic("render.input_invalid", { field: "layout" }));
  if (!validLimits(input.limits))
    diagnostics.push(diagnostic("render.input_invalid", { field: "limits" }));
  if (!validFonts(input.resolved_fonts))
    diagnostics.push(
      diagnostic("render.input_invalid", { field: "resolved_fonts" })
    );
  if (!validAssets(input.resolved_assets))
    diagnostics.push(
      diagnostic("render.input_invalid", { field: "resolved_assets" })
    );
  if (diagnostics.length > 0) return stableDiagnostics(diagnostics);

  const recipeProfile = canonical(input.recipe.renderer_profile);
  if (recipeProfile !== canonical(input.resolved_profile.renderer_profile)) {
    diagnostics.push(
      diagnostic("render.profile_incompatible", { reason: "identity_mismatch" })
    );
  }
  if (input.recipe.shape.kind !== input.view_data.kind)
    diagnostics.push(
      diagnostic("render.profile_incompatible", { reason: "shape_mismatch" })
    );
  if (!input.resolved_profile.supported_shapes.includes(input.view_data.kind))
    diagnostics.push(
      diagnostic("render.profile_unsupported", { shape: input.view_data.kind })
    );
  if (
    !input.resolved_profile.supported_algorithms.includes(
      input.recipe.layout_algorithm.kind
    )
  )
    diagnostics.push(
      diagnostic("render.profile_incompatible", {
        algorithm: input.recipe.layout_algorithm.kind,
      })
    );
  if (
    input.recipe.layout_algorithm.kind === "grid" &&
    !Number.isSafeInteger(input.recipe.layout_algorithm.columns)
  )
    diagnostics.push(
      diagnostic("render.profile_incompatible", {
        algorithm: "grid",
        reason: "columns_must_be_integer",
      })
    );
  if (
    ["context", "diff", "matrix", "table"].includes(input.view_data.kind) &&
    !["grid", "native"].includes(input.recipe.layout_algorithm.kind)
  ) {
    diagnostics.push(
      diagnostic("render.profile_incompatible", {
        algorithm: input.recipe.layout_algorithm.kind,
        shape: input.view_data.kind,
      })
    );
  }

  const fontDigests = new Set(input.resolved_fonts.map((font) => font.digest));
  const fontFamilies = new Set(input.resolved_fonts.map((font) => font.family));
  if (input.recipe.font_policy.families.length === 0)
    diagnostics.push(diagnostic("render.font_missing", { family: "primary" }));
  for (const family of input.recipe.font_policy.families)
    if (!fontFamilies.has(family))
      diagnostics.push(diagnostic("render.font_missing", { family }));
  for (const digest of input.recipe.font_policy.required_digests)
    if (!fontDigests.has(digest))
      diagnostics.push(diagnostic("render.font_missing", { digest }));

  const assetDigests = new Set(
    input.resolved_assets.map((asset) => asset.digest)
  );
  const requiredAssets = new Set<string>(
    input.recipe.asset_policy.required_digests
  );
  collectAssetDigests(input.view_data, requiredAssets);
  for (const digest of [...requiredAssets].sort(compare))
    if (!assetDigests.has(digest as Digest))
      diagnostics.push(diagnostic("render.asset_missing", { digest }));
  return stableDiagnostics(diagnostics);
}

function validProfile(value: unknown): value is ResolvedRendererProfile {
  if (
    !closedObject(value, [
      "renderer_profile",
      "supported_shapes",
      "supported_algorithms",
    ])
  )
    return false;
  const profile = value as unknown as ResolvedRendererProfile;
  try {
    assertRenderRecipe(dummyRecipe(profile.renderer_profile));
  } catch {
    return false;
  }
  return (
    sortedEnum(profile.supported_shapes, shapeOrder) &&
    sortedEnum(profile.supported_algorithms, algorithmOrder)
  );
}

function validFonts(value: unknown): value is readonly ResolvedFontMetrics[] {
  if (!Array.isArray(value) || value.length === 0) return false;
  let previous = "";
  for (const item of value) {
    if (
      !closedObject(item, [
        "family",
        "digest",
        "units_per_em",
        "ascent",
        "descent",
        "line_gap",
        "default_advance",
        "glyph_advances",
      ])
    )
      return false;
    const font = item as unknown as ResolvedFontMetrics;
    const order = `${font.family}\0${font.digest}`;
    if (
      typeof font.family !== "string" ||
      font.family.length === 0 ||
      order <= previous ||
      !isDigest(font.digest)
    )
      return false;
    if (
      ![font.units_per_em, font.ascent, font.default_advance].every(positive) ||
      ![font.descent, font.line_gap].every(nonnegative)
    )
      return false;
    if (
      !closedRecord(font.glyph_advances) ||
      Object.entries(font.glyph_advances).some(
        ([key, advance]) =>
          Array.from(key).length !== 1 || !nonnegative(advance)
      )
    )
      return false;
    previous = order;
  }
  return true;
}

function validAssets(
  value: unknown
): value is readonly ResolvedAssetDimensions[] {
  if (!Array.isArray(value)) return false;
  let previous = "";
  for (const item of value) {
    if (!closedObject(item, ["digest", "width", "height"])) return false;
    const asset = item as unknown as ResolvedAssetDimensions;
    if (
      !isDigest(asset.digest) ||
      asset.digest <= previous ||
      !positive(asset.width) ||
      !positive(asset.height)
    )
      return false;
    previous = asset.digest;
  }
  return true;
}

function validLayout(value: unknown): value is RenderLayoutPolicy {
  const keys = [
    "item_order",
    "tie_breaker",
    "coordinate_precision",
    "item_width",
    "item_height",
    "horizontal_gap",
    "vertical_gap",
    "container_padding",
    "cell_padding",
    "lane_header_size",
    "badge_size",
    "overlay_offset",
    "port_offset",
    "font_size",
    "asset_scale",
  ];
  if (!closedObject(value, keys)) return false;
  const layout = value as unknown as RenderLayoutPolicy;
  return (
    layout.item_order === "viewdata" &&
    layout.tie_breaker === "seeded_key_hash" &&
    Number.isSafeInteger(layout.coordinate_precision) &&
    layout.coordinate_precision >= 0 &&
    layout.coordinate_precision <= 6 &&
    [
      layout.item_width,
      layout.item_height,
      layout.horizontal_gap,
      layout.vertical_gap,
      layout.container_padding,
      layout.cell_padding,
      layout.lane_header_size,
      layout.badge_size,
      layout.font_size,
    ].every(positive) &&
    [layout.overlay_offset, layout.port_offset, layout.asset_scale].every(
      nonnegative
    )
  );
}

function validLimits(value: unknown): value is RenderResourceLimits {
  if (
    !closedObject(value, [
      "max_primitives",
      "max_route_points",
      "max_text_length",
      "max_depth",
      "max_extent",
    ])
  )
    return false;
  const limits = value as unknown as RenderResourceLimits;
  return (
    [
      limits.max_primitives,
      limits.max_route_points,
      limits.max_text_length,
      limits.max_depth,
    ].every((item) => Number.isSafeInteger(item) && item > 0) &&
    positive(limits.max_extent)
  );
}

function preflight(input: RenderMaterializationInput): void {
  const limits = input.limits;
  let primitives = 0;
  let routePoints = 0;
  const addPrimitives = (count: number): void => {
    if (count > limits.max_primitives - primitives) {
      fail("render.resource_limit", "max_primitives", limits.max_primitives);
    }
    primitives += count;
  };
  const addRoutes = (routes: number): void => {
    if (routes > Math.floor((limits.max_route_points - routePoints) / 4)) {
      fail(
        "render.resource_limit",
        "max_route_points",
        limits.max_route_points
      );
    }
    routePoints += routes * 4;
  };

  switch (input.view_data.kind) {
    case "diagram": {
      const view = input.view_data.diagram!;
      validateDiagramReferences(input);
      addPrimitives(view.occurrences.length * 2);
      addPrimitives(view.containers.length);
      addPrimitives(
        view.overlays.length + view.badges.length + view.support_items.length
      );
      if (
        view.edges.length > Math.floor((limits.max_primitives - primitives) / 4)
      ) {
        fail("render.resource_limit", "max_primitives", limits.max_primitives);
      }
      addPrimitives(view.edges.length * 4);
      addRoutes(view.edges.length);
      break;
    }
    case "table": {
      const view = input.view_data.table!;
      validateTableCells(
        view.columns.map((item) => item.key),
        view.rows
      );
      addPrimitives(view.columns.length + view.rows.length);
      addProduct(
        view.rows.length,
        view.columns.length,
        limits.max_primitives - primitives,
        addPrimitives,
        limits.max_primitives
      );
      break;
    }
    case "matrix": {
      const view = input.view_data.matrix!;
      validateMatrixCoordinates(
        view.row_axis.map((item) => item.key),
        view.column_axis.map((item) => item.key),
        view.cells
      );
      addPrimitives(view.row_axis.length + view.column_axis.length);
      addPrimitives(view.cells.length);
      break;
    }
    case "tree": {
      const view = input.view_data.tree!;
      let occurrences = 0;
      const stack = [...view.roots]
        .reverse()
        .map((item) => ({ item, depth: 0 }));
      while (stack.length > 0) {
        const entry = stack.pop()!;
        if (entry.depth > limits.max_depth)
          fail("render.resource_limit", "max_depth", limits.max_depth);
        occurrences += 1;
        if (occurrences > limits.max_primitives)
          fail(
            "render.resource_limit",
            "max_primitives",
            limits.max_primitives
          );
        for (
          let index = entry.item.children.length - 1;
          index >= 0;
          index -= 1
        ) {
          stack.push({
            item: entry.item.children[index]!,
            depth: entry.depth + 1,
          });
        }
      }
      addPrimitives(occurrences);
      addPrimitives(view.link_refs.length + view.cycle_refs.length);
      addRoutes(view.link_refs.length + view.cycle_refs.length);
      break;
    }
    case "flow": {
      const view = input.view_data.flow!;
      validateFlowReferences(view);
      let junctions = 0;
      for (const step of view.steps)
        junctions += Number(step.branch) + Number(step.join);
      addPrimitives(view.lanes.length + view.steps.length);
      addPrimitives(
        junctions + view.connectors.length + view.cycle_refs.length
      );
      addRoutes(view.connectors.length + view.cycle_refs.length);
      break;
    }
    case "context": {
      const view = input.view_data.context!;
      for (const group of view.groups)
        addPrimitives(1 + group.attributes.length + group.facts.length);
      break;
    }
    case "diff": {
      const view = input.view_data.diff!;
      for (const change of view.changes) {
        addPrimitives(
          1 +
            change.fields.length +
            Number(change.before_address !== undefined) +
            Number(change.after_address !== undefined)
        );
      }
      break;
    }
  }
}

function addProduct(
  left: number,
  right: number,
  remaining: number,
  add: (count: number) => void,
  limit: number
): void {
  if (left !== 0 && right > Math.floor(remaining / left)) {
    fail("render.resource_limit", "max_primitives", limit);
  }
  add(left * right);
}

function validateDiagramReferences(input: RenderMaterializationInput): void {
  const view = input.view_data.diagram!;
  const occurrences = new Map(view.occurrences.map((item) => [item.key, item]));
  for (const item of view.occurrences) {
    if (item.parent_key !== undefined && !occurrences.has(item.parent_key))
      fail("render.geometry_invalid", "missing_diagram_parent", 0);
  }
  for (const item of view.containers) {
    if (!occurrences.has(item.occurrence_key))
      fail("render.geometry_invalid", "missing_container_occurrence", 0);
    for (const childKey of item.child_keys)
      if (!occurrences.has(childKey))
        fail("render.geometry_invalid", "missing_container_child", 0);
  }
  for (const item of view.edges) {
    if (
      !occurrences.has(item.from_occurrence_key) ||
      !occurrences.has(item.to_occurrence_key)
    )
      fail("render.geometry_invalid", "missing_edge_occurrence", 0);
  }
  for (const item of view.overlays)
    if (!occurrences.has(item.target_occurrence_key))
      fail("render.geometry_invalid", "missing_overlay_target", 0);
  for (const item of view.badges)
    if (!occurrences.has(item.target_occurrence_key))
      fail("render.geometry_invalid", "missing_badge_target", 0);

  if (
    input.recipe.layout_algorithm.kind === "native" &&
    input.view_data.shape.diagram!.layout === "manual"
  ) {
    const rootByAddress = new Map<string, DiagramOccurrence[]>();
    for (const item of view.occurrences) {
      if (item.parent_key === undefined && item.role !== "support") {
        const roots = rootByAddress.get(item.entity_address) ?? [];
        roots.push(item);
        rootByAddress.set(item.entity_address, roots);
      }
    }
    for (const placement of input.view_data.shape.diagram!.placements) {
      const roots = rootByAddress.get(placement.entity_address);
      if (roots === undefined)
        fail("render.geometry_invalid", "dangling_diagram_placement", 0);
      if (roots.length !== 1)
        fail("render.geometry_invalid", "ambiguous_diagram_placement", 0);
    }
    const placedAddresses = new Set(
      input.view_data.shape.diagram!.placements.map(
        (placement) => placement.entity_address
      )
    );
    for (const item of view.occurrences)
      if (
        item.parent_key === undefined &&
        item.role !== "support" &&
        !placedAddresses.has(item.entity_address)
      )
        fail("render.geometry_invalid", "missing_diagram_placement", 0);
  }
}

function validateTableCells(
  columnKeys: readonly ViewDataItemKey[],
  rows: readonly TableRow[]
): void {
  const expected = new Set<string>(columnKeys);
  for (const row of rows) {
    const actual = Object.keys(row.cells);
    if (
      actual.length !== expected.size ||
      actual.some((key) => !expected.has(key))
    ) {
      fail("render.geometry_invalid", "table_cell_keys_mismatch", 0);
    }
  }
}

function validateMatrixCoordinates(
  rowKeys: readonly ViewDataItemKey[],
  columnKeys: readonly ViewDataItemKey[],
  cells: readonly MatrixCell[]
): void {
  const rows = new Set<string>(rowKeys);
  const columns = new Set<string>(columnKeys);
  const coordinates = new Set<string>();
  for (const cell of cells) {
    if (!rows.has(cell.row_key) || !columns.has(cell.column_key))
      fail("render.geometry_invalid", "missing_matrix_axis", 0);
    const coordinate = `${cell.row_key}\0${cell.column_key}`;
    if (coordinates.has(coordinate))
      fail("render.geometry_invalid", "duplicate_matrix_coordinate", 0);
    coordinates.add(coordinate);
  }
  if (
    rowKeys.length !== 0 &&
    columnKeys.length > Math.floor(Number.MAX_SAFE_INTEGER / rowKeys.length)
  ) {
    fail("render.geometry_invalid", "matrix_coordinate_overflow", 0);
  }
  if (coordinates.size !== rowKeys.length * columnKeys.length)
    fail("render.geometry_invalid", "matrix_coordinates_incomplete", 0);
}

function validateFlowReferences(view: FlowViewData): void {
  const lanes = new Map(view.lanes.map((item) => [item.key, item]));
  const steps = new Map(view.steps.map((item) => [item.key, item]));
  const membership = new Set<ViewDataItemKey>();
  for (const lane of view.lanes) {
    for (const stepKey of lane.step_keys) {
      const step = steps.get(stepKey);
      if (step === undefined)
        fail("render.geometry_invalid", "missing_flow_lane_step", 0);
      if (step.lane_key !== lane.key)
        fail("render.geometry_invalid", "flow_lane_membership_mismatch", 0);
      if (membership.has(stepKey))
        fail("render.geometry_invalid", "duplicate_flow_lane_step", 0);
      membership.add(stepKey);
    }
  }
  for (const step of view.steps) {
    if (!lanes.has(step.lane_key))
      fail("render.geometry_invalid", "missing_flow_lane", 0);
    if (!membership.has(step.key))
      fail("render.geometry_invalid", "missing_flow_lane_membership", 0);
  }
  const connectorByKey = new Map(
    view.connectors.map((item) => [item.key, item])
  );
  for (const item of [...view.connectors, ...view.cycle_refs]) {
    if (!steps.has(item.from_step_key) || !steps.has(item.to_step_key))
      fail("render.geometry_invalid", "missing_flow_step", 0);
  }
  for (const cycle of view.cycle_refs) {
    const connector = connectorByKey.get(cycle.connector_key);
    if (connector !== undefined) {
      const left = canonical({
        from: connector.from_step_key,
        to: connector.to_step_key,
        kind: connector.kind,
        branch_value: connector.branch_value,
        branch_rows: connector.branch_row_addresses,
        relations: connector.relation_addresses,
      });
      const right = canonical({
        from: cycle.from_step_key,
        to: cycle.to_step_key,
        kind: cycle.kind,
        branch_value: cycle.branch_value,
        branch_rows: cycle.branch_row_addresses,
        relations: cycle.relation_addresses,
      });
      if (left !== right)
        fail("render.geometry_invalid", "flow_cycle_connector_mismatch", 0);
    }
  }
}

function build(context: BuildContext): RenderData {
  switch (context.input.view_data.kind) {
    case "diagram":
      return buildDiagram(context);
    case "table":
      return buildTable(context);
    case "matrix":
      return buildMatrix(context);
    case "tree":
      return buildTree(context);
    case "flow":
      return buildFlow(context);
    case "context":
      return buildContext(context);
    case "diff":
      return buildDiff(context);
  }
}

function base(
  context: BuildContext,
  kind: RenderShape,
  bounds: RenderBounds
): Omit<RenderData, "kind"> {
  return {
    render_data_schema_version: 1,
    renderer_profile: context.input.recipe.renderer_profile,
    view_data_hash: context.input.view_data_hash,
    render_input_hash: context.renderInputHash,
    shape: kind,
    layout_seed: context.input.recipe.layout_seed.value,
    locale: context.input.recipe.locale.language_tag,
    timezone: context.input.recipe.timezone.iana_name,
    bounds,
    source_bindings: context.bindings,
    resolved_asset_digests: context.input.resolved_assets.map(
      (asset) => asset.digest
    ),
    resolved_font_digests: [
      ...new Set(context.input.resolved_fonts.map((font) => font.digest)),
    ].sort(compare),
    diagnostics: viewDataDiagnostics(context.input.view_data.diagnostics),
  } as Omit<RenderData, "kind">;
}

function buildDiagram(context: BuildContext): DiagramRenderData {
  const view = context.input.view_data.diagram!;
  const occurrences = [...view.occurrences];
  const byKey = new Map(occurrences.map((item) => [item.key, item]));
  const depths = new Map<ViewDataItemKey, number>();
  for (const item of occurrences) {
    const chain: DiagramOccurrence[] = [];
    const visiting = new Set<ViewDataItemKey>();
    let current: DiagramOccurrence | undefined = item;
    while (current !== undefined && !depths.has(current.key)) {
      if (visiting.has(current.key))
        fail("render.geometry_invalid", "diagram_parent_cycle", 0);
      visiting.add(current.key);
      chain.push(current);
      if (chain.length - 1 > context.input.limits.max_depth) {
        fail(
          "render.resource_limit",
          "max_depth",
          context.input.limits.max_depth
        );
      }
      if (current.parent_key === undefined) break;
      current = byKey.get(current.parent_key);
      if (current === undefined)
        fail("render.geometry_invalid", "missing_diagram_parent", 0);
    }
    let value = current === undefined ? 0 : (depths.get(current.key) ?? -1) + 1;
    for (let index = chain.length - 1; index >= 0; index -= 1) {
      const member = chain[index]!;
      if (depths.has(member.key)) {
        value = depths.get(member.key)! + 1;
        continue;
      }
      if (value > context.input.limits.max_depth) {
        fail(
          "render.resource_limit",
          "max_depth",
          context.input.limits.max_depth
        );
      }
      depths.set(member.key, Math.max(0, value));
      value += 1;
    }
  }
  const positions = layoutPositions(
    context,
    occurrences.map((item) => item.key),
    depths
  );
  const manualBounds = manualDiagramBounds(context, occurrences);
  const occurrenceBounds = new Map<ViewDataItemKey, RenderBounds>();
  const occurrenceKeys = new Map<ViewDataItemKey, string>();
  const labels: DiagramRenderData["labels"][number][] = [];
  const outputOccurrences: DiagramRenderData["occurrences"][number][] = [];
  for (const item of occurrences) {
    const labelText = context.text(shortAddress(item.entity_address));
    const measured = context.measure(labelText);
    const asset = largestAsset(context, item.source);
    const width = Math.max(
      context.input.layout.item_width,
      measured.width + context.input.layout.cell_padding * 2,
      (asset?.width ?? 0) * context.input.layout.asset_scale
    );
    const height = Math.max(
      context.input.layout.item_height,
      measured.height + context.input.layout.cell_padding * 2,
      (asset?.height ?? 0) * context.input.layout.asset_scale
    );
    const position = positions.get(item.key)!;
    const bounds =
      manualBounds.get(item.key) ??
      context.rect(position.x, position.y, width, height);
    occurrenceBounds.set(item.key, bounds);
    const occurrenceKey = renderKey("diagram-occurrence", item.key);
    occurrenceKeys.set(item.key, occurrenceKey);
    const labelKey = renderKey("diagram-label", item.key);
    outputOccurrences.push({
      render_key: occurrenceKey,
      bounds,
      port_keys: [],
      label_key: labelKey,
    });
    labels.push({
      render_key: labelKey,
      bounds: centeredText(context, bounds, labelText),
      text: labelText,
      anchor: { kind: "occurrence", occurrence_key: occurrenceKey },
    });
    context.bind(occurrenceKey, item.key, item.source);
    context.bind(labelKey, item.key, item.source);
  }

  const containers: DiagramRenderData["containers"][number][] = [];
  // Fold inner containers first so each outer bound includes finalized child geometry.
  const containersByDepth = [...view.containers].sort((left, right) => {
    const leftDepth = depths.get(left.occurrence_key);
    const rightDepth = depths.get(right.occurrence_key);
    if (leftDepth === undefined || rightDepth === undefined) {
      fail("render.geometry_invalid", "missing_container_occurrence", 0);
    }
    return rightDepth - leftDepth || compare(left.key, right.key);
  });
  for (const item of containersByDepth) {
    const occurrence = byKey.get(item.occurrence_key);
    if (occurrence === undefined)
      fail("render.geometry_invalid", "missing_container_occurrence", 0);
    const childBounds = item.child_keys.map((key) => {
      const value = occurrenceBounds.get(key);
      if (value === undefined)
        fail("render.geometry_invalid", "missing_container_child", 0);
      return value;
    });
    const content =
      childBounds.length > 0
        ? union(childBounds)
        : occurrenceBounds.get(item.occurrence_key)!;
    const bounds =
      manualBounds.get(item.occurrence_key) ??
      expand(context, content, context.input.layout.container_padding);
    const ownerRenderKey = occurrenceKeys.get(item.occurrence_key)!;
    occurrenceBounds.set(item.occurrence_key, bounds);
    const owner = outputOccurrences.find(
      (value) => value.render_key === ownerRenderKey
    )!;
    Object.assign(owner, { bounds });
    const key = renderKey("diagram-container", item.key);
    containers.push({
      render_key: key,
      bounds,
      child_keys: item.child_keys
        .map((child) => occurrenceKeys.get(child))
        .filter((value): value is string => value !== undefined),
    });
    context.bind(key, item.key, item.source);
  }

  const ports: DiagramRenderData["ports"][number][] = [];
  const edgePaths: DiagramRenderData["edge_paths"][number][] = [];
  for (const edge of view.edges) {
    const fromBounds = occurrenceBounds.get(edge.from_occurrence_key);
    const toBounds = occurrenceBounds.get(edge.to_occurrence_key);
    if (fromBounds === undefined || toBounds === undefined)
      fail("render.geometry_invalid", "missing_edge_occurrence", 0);
    const fromOccurrence = occurrenceKeys.get(edge.from_occurrence_key)!;
    const toOccurrence = occurrenceKeys.get(edge.to_occurrence_key)!;
    const from = edgePoint(context, fromBounds, true);
    const to = edgePoint(context, toBounds, false);
    const fromPort = renderKey("diagram-port-from", edge.key);
    const toPort = renderKey("diagram-port-to", edge.key);
    ports.push(
      { render_key: fromPort, position: from, occurrence_key: fromOccurrence },
      { render_key: toPort, position: to, occurrence_key: toOccurrence }
    );
    context.bind(fromPort, edge.key, edge.source);
    context.bind(toPort, edge.key, edge.source);
    const ownerFrom = outputOccurrences.find(
      (item) => item.render_key === fromOccurrence
    )!;
    const ownerTo = outputOccurrences.find(
      (item) => item.render_key === toOccurrence
    )!;
    (ownerFrom.port_keys as string[]).push(fromPort);
    (ownerTo.port_keys as string[]).push(toPort);
    const pathKey = renderKey("diagram-edge", edge.key);
    const points = route(context, from, to);
    edgePaths.push({
      render_key: pathKey,
      points,
      from_port_key: fromPort,
      to_port_key: toPort,
    });
    context.bind(pathKey, edge.key, edge.source);
    const text = context.text(shortAddress(edge.relation_address));
    const midpoint = points[Math.floor(points.length / 2)]!;
    const measured = context.measure(text);
    const labelKey = renderKey("diagram-edge-label", edge.key);
    labels.push({
      render_key: labelKey,
      bounds: context.rect(
        midpoint.x - measured.width / 2,
        midpoint.y - measured.height / 2,
        measured.width,
        measured.height
      ),
      text,
      anchor: { kind: "edge_path", edge_path_key: pathKey },
    });
    context.bind(labelKey, edge.key, edge.source);
  }
  const overlays: DiagramRenderData["overlays"][number][] = [];
  for (const item of view.overlays) {
    const target = occurrenceBounds.get(item.target_occurrence_key);
    if (target === undefined)
      fail("render.geometry_invalid", "missing_overlay_target", 0);
    const key = renderKey("diagram-overlay", item.key);
    overlays.push({
      render_key: key,
      bounds: context.rect(
        target.x +
          target.width -
          context.input.layout.badge_size -
          context.input.layout.overlay_offset,
        target.y + context.input.layout.overlay_offset,
        context.input.layout.badge_size,
        context.input.layout.badge_size
      ),
      target: {
        kind: "occurrence",
        occurrence_key: occurrenceKeys.get(item.target_occurrence_key)!,
      },
      display_identity: context.text(shortAddress(item.overlay_entity_address)),
    });
    context.bind(key, item.key, item.source);
  }
  const badges: DiagramRenderData["badges"][number][] = [];
  for (const item of view.badges) {
    const target = occurrenceBounds.get(item.target_occurrence_key);
    if (target === undefined)
      fail("render.geometry_invalid", "missing_badge_target", 0);
    const key = renderKey("diagram-badge", item.key);
    badges.push({
      render_key: key,
      bounds: context.rect(
        target.x + target.width - context.input.layout.badge_size,
        target.y - context.input.layout.badge_size / 2,
        context.input.layout.badge_size,
        context.input.layout.badge_size
      ),
      target: {
        kind: "occurrence",
        occurrence_key: occurrenceKeys.get(item.target_occurrence_key)!,
      },
      label: context.text(item.label ?? ""),
    });
    context.bind(key, item.key, item.source);
  }
  const supportGeometry: DiagramRenderData["support_geometry"][number][] = [];
  const current = allBounds([
    ...outputOccurrences,
    ...containers,
    ...labels,
    ...overlays,
    ...badges,
  ]);
  for (const [index, item] of view.support_items.entries()) {
    const key = renderKey("diagram-support", item.key);
    supportGeometry.push({
      render_key: key,
      bounds: context.rect(
        current.x +
          index *
            (context.input.layout.badge_size +
              context.input.layout.horizontal_gap),
        current.y + current.height + context.input.layout.vertical_gap,
        context.input.layout.badge_size,
        context.input.layout.badge_size
      ),
      support_kind: item.support_kind,
      display_identity: context.text(
        shortAddress(
          item.entity_address ?? item.relation_address ?? item.support_kind
        )
      ),
    });
    context.bind(key, item.key, item.source);
  }
  const containerByKey = new Map(
    view.containers.map((item) => [item.key, item])
  );
  // RenderData paint order is outer-to-inner; canonical keys close same-depth ties.
  containers.sort((left, right) => {
    const leftKey = left.render_key.slice("diagram-container:".length);
    const rightKey = right.render_key.slice("diagram-container:".length);
    const leftContainer = containerByKey.get(leftKey);
    const rightContainer = containerByKey.get(rightKey);
    if (leftContainer === undefined || rightContainer === undefined)
      fail("render.geometry_invalid", "missing_container_occurrence", 0);
    return (
      depths.get(leftContainer.occurrence_key)! -
        depths.get(rightContainer.occurrence_key)! || compare(leftKey, rightKey)
    );
  });
  const bounds = allBounds([
    ...outputOccurrences,
    ...containers,
    ...labels,
    ...overlays,
    ...badges,
    ...supportGeometry,
    ...ports,
    ...edgePaths,
  ]);
  return {
    ...base(context, "diagram", bounds),
    kind: "diagram",
    containers,
    occurrences: outputOccurrences,
    ports,
    edge_paths: edgePaths,
    labels,
    overlays,
    badges,
    support_geometry: supportGeometry,
  };
}

function manualDiagramBounds(
  context: BuildContext,
  occurrences: readonly DiagramOccurrence[]
): Map<ViewDataItemKey, RenderBounds> {
  const output = new Map<ViewDataItemKey, RenderBounds>();
  const shape = context.input.view_data.shape.diagram!;
  if (
    context.input.recipe.layout_algorithm.kind !== "native" ||
    shape.layout !== "manual"
  )
    return output;
  const rootByAddress = new Map<string, DiagramOccurrence>();
  for (const item of occurrences) {
    if (item.parent_key === undefined && item.role !== "support")
      rootByAddress.set(item.entity_address, item);
  }
  for (const placement of shape.placements) {
    const occurrence = rootByAddress.get(placement.entity_address);
    if (occurrence === undefined)
      fail("render.geometry_invalid", "dangling_diagram_placement", 0);
    output.set(
      occurrence.key,
      context.rect(
        Number(placement.x),
        Number(placement.y),
        Number(placement.width),
        Number(placement.height)
      )
    );
  }
  return output;
}

function buildTable(context: BuildContext): TableRenderData {
  const view = context.input.view_data.table!;
  const columns = [...view.columns];
  const rows = [...view.rows];
  const texts = new Map<string, string>();
  const widths = new Map<ViewDataItemKey, number>();
  for (const column of columns)
    widths.set(
      column.key,
      context.measure(column.label).width +
        context.input.layout.cell_padding * 2
    );
  for (const row of rows)
    for (const column of columns) {
      const text = tableText(row.cells[column.key]);
      texts.set(`${row.key}\0${column.key}`, text);
      widths.set(
        column.key,
        Math.max(
          widths.get(column.key)!,
          context.measure(text).width + context.input.layout.cell_padding * 2,
          context.input.layout.item_width
        )
      );
    }
  const lineHeight =
    context.measure("M").height + context.input.layout.cell_padding * 2;
  const columnOutput: TableRenderData["columns"][number][] = [];
  const rowOutput: TableRenderData["rows"][number][] = [];
  const cells: TableRenderData["cells"][number][] = [];
  let x = 0;
  const xByColumn = new Map<ViewDataItemKey, number>();
  for (const column of columns) {
    const width = widths.get(column.key)!;
    xByColumn.set(column.key, x);
    const key = renderKey("table-column", column.key);
    columnOutput.push({
      render_key: key,
      x: context.number(x),
      width: context.number(width),
      frozen: false,
      id: column.id,
      label: context.text(column.label),
      value_type: column.value_type,
      header_bounds: context.rect(x, 0, width, lineHeight),
    });
    context.bind(key, column.key, columnSource(column));
    x += width;
  }
  for (const [rowIndex, row] of rows.entries()) {
    const y = lineHeight + rowIndex * lineHeight;
    const rowKey = renderKey("table-row", row.key);
    rowOutput.push({
      render_key: rowKey,
      y: context.number(y),
      height: context.number(lineHeight),
      frozen: false,
    });
    context.bind(rowKey, row.key, row.source);
    for (const column of columns) {
      const key = renderKey("table-cell", `${row.key}:${column.key}`);
      const text = context.text(texts.get(`${row.key}\0${column.key}`)!);
      const source = row.cells[column.key]?.source ?? row.source;
      cells.push({
        render_key: key,
        bounds: context.rect(
          xByColumn.get(column.key)!,
          y,
          widths.get(column.key)!,
          lineHeight
        ),
        row_key: rowKey,
        column_key: renderKey("table-column", column.key),
        text,
      });
      context.bind(key, row.key, source);
    }
  }
  const bounds = context.rect(0, 0, x, lineHeight * (rows.length + 1));
  return {
    ...base(context, "table", bounds),
    kind: "table",
    columns: columnOutput,
    rows: rowOutput,
    cells,
  };
}

function buildMatrix(context: BuildContext): MatrixRenderData {
  const view = context.input.view_data.matrix!;
  const rowAxis = [...view.row_axis];
  const columnAxis = [...view.column_axis];
  const cellsInput = [...view.cells];
  let rowHeaderWidth = context.input.layout.item_width;
  for (const item of rowAxis)
    rowHeaderWidth = Math.max(
      rowHeaderWidth,
      context.measure(item.label).width + context.input.layout.cell_padding * 2
    );
  let cellWidth = context.input.layout.item_width;
  for (const item of cellsInput)
    cellWidth = Math.max(
      cellWidth,
      context.measure(matrixText(item.display_value)).width +
        context.input.layout.cell_padding * 2
    );
  const cellHeight = Math.max(
    context.input.layout.item_height,
    context.measure("M").height + context.input.layout.cell_padding * 2
  );
  const rowKeys = new Map<ViewDataItemKey, string>();
  const columnKeys = new Map<ViewDataItemKey, string>();
  const rowAxes: MatrixRenderData["row_axes"][number][] = [];
  const columnAxes: MatrixRenderData["column_axes"][number][] = [];
  for (const [index, item] of rowAxis.entries()) {
    const key = renderKey("matrix-row-axis", item.key);
    rowKeys.set(item.key, key);
    rowAxes.push({
      render_key: key,
      bounds: context.rect(
        0,
        cellHeight + index * cellHeight,
        rowHeaderWidth,
        cellHeight
      ),
      label: context.text(item.label),
    });
    context.bind(key, item.key, item.source);
  }
  for (const [index, item] of columnAxis.entries()) {
    const key = renderKey("matrix-column-axis", item.key);
    columnKeys.set(item.key, key);
    columnAxes.push({
      render_key: key,
      bounds: context.rect(
        rowHeaderWidth + index * cellWidth,
        0,
        cellWidth,
        cellHeight
      ),
      label: context.text(item.label),
    });
    context.bind(key, item.key, item.source);
  }
  const rowIndex = new Map(rowAxis.map((item, index) => [item.key, index]));
  const columnIndex = new Map(
    columnAxis.map((item, index) => [item.key, index])
  );
  const cells: MatrixRenderData["cells"][number][] = [];
  for (const item of cellsInput) {
    const row = rowIndex.get(item.row_key);
    const column = columnIndex.get(item.column_key);
    if (row === undefined || column === undefined)
      fail("render.geometry_invalid", "missing_matrix_axis", 0);
    const key = renderKey("matrix-cell", item.key);
    cells.push({
      render_key: key,
      bounds: context.rect(
        rowHeaderWidth + column * cellWidth,
        cellHeight + row * cellHeight,
        cellWidth,
        cellHeight
      ),
      row_axis_key: rowKeys.get(item.row_key)!,
      column_axis_key: columnKeys.get(item.column_key)!,
      text: context.text(matrixText(item.display_value)),
    });
    context.bind(key, item.key, item.source);
  }
  const bounds = context.rect(
    0,
    0,
    rowHeaderWidth + columnAxis.length * cellWidth,
    cellHeight + rowAxis.length * cellHeight
  );
  return {
    ...base(context, "matrix", bounds),
    kind: "matrix",
    row_axes: rowAxes,
    column_axes: columnAxes,
    cells,
    totals: [],
  };
}

function buildTree(context: BuildContext): TreeRenderData {
  const view = context.input.view_data.tree!;
  const flattened: {
    item: typeof view.roots[number];
    parent?: ViewDataItemKey;
    depth: number;
  }[] = [];
  const seen = new Set<ViewDataItemKey>();
  const stack = [...view.roots].reverse().map((item) => ({
    item,
    parent: undefined as ViewDataItemKey | undefined,
    depth: 0,
  }));
  while (stack.length > 0) {
    const entry = stack.pop()!;
    if (entry.depth > context.input.limits.max_depth)
      fail(
        "render.resource_limit",
        "max_depth",
        context.input.limits.max_depth
      );
    if (seen.has(entry.item.key))
      fail("render.geometry_invalid", "duplicate_tree_occurrence", 0);
    seen.add(entry.item.key);
    if (entry.parent === undefined) {
      flattened.push({ item: entry.item, depth: entry.depth });
    } else {
      flattened.push({
        item: entry.item,
        parent: entry.parent,
        depth: entry.depth,
      });
    }
    for (let index = entry.item.children.length - 1; index >= 0; index -= 1) {
      stack.push({
        item: entry.item.children[index]!,
        parent: entry.item.key,
        depth: entry.depth + 1,
      });
    }
  }
  const depths = new Map(flattened.map((item) => [item.item.key, item.depth]));
  const positions = layoutPositions(
    context,
    flattened.map((item) => item.item.key),
    depths
  );
  const keys = new Map<ViewDataItemKey, string>();
  const boundsByKey = new Map<ViewDataItemKey, RenderBounds>();
  const entityTargets = new Map<string, ViewDataItemKey>();
  const occurrences: TreeRenderData["occurrences"][number][] = [];
  for (const entry of flattened) {
    const position = positions.get(entry.item.key)!;
    const width = Math.max(
      context.input.layout.item_width,
      context.measure(shortAddress(entry.item.entity_address)).width +
        context.input.layout.cell_padding * 2
    );
    const bounds = context.rect(
      position.x,
      position.y,
      width,
      context.input.layout.item_height
    );
    const key = renderKey("tree-occurrence", entry.item.key);
    keys.set(entry.item.key, key);
    boundsByKey.set(entry.item.key, bounds);
    if (!entityTargets.has(entry.item.entity_address))
      entityTargets.set(entry.item.entity_address, entry.item.key);
    const label = context.text(shortAddress(entry.item.entity_address));
    occurrences.push(
      entry.parent === undefined
        ? { render_key: key, bounds, depth: entry.depth, label }
        : {
            render_key: key,
            bounds,
            parent_key: renderKey("tree-occurrence", entry.parent),
            depth: entry.depth,
            label,
          }
    );
    context.bind(key, entry.item.key, entry.item.source);
  }
  const references = (
    items: readonly typeof view.link_refs[number][],
    prefix: string
  ): TreeRenderData["duplicate_refs"] =>
    items.map((item) => {
      const from = boundsByKey.get(item.from_occurrence_key);
      const targetKey = entityTargets.get(item.to_entity_address);
      const to =
        targetKey === undefined ? undefined : boundsByKey.get(targetKey);
      if (from === undefined || to === undefined)
        fail("render.geometry_invalid", "missing_tree_reference", 0);
      const key = renderKey(prefix, item.key);
      const value = {
        render_key: key,
        points: route(context, center(from), center(to)),
        from_occurrence_key: keys.get(item.from_occurrence_key)!,
        to_occurrence_key: keys.get(targetKey!)!,
      };
      context.bind(key, item.key, item.source);
      return value;
    });
  const duplicateRefs = references(view.link_refs, "tree-duplicate");
  const cycleRefs = references(view.cycle_refs, "tree-cycle");
  return {
    ...base(
      context,
      "tree",
      allBounds([...occurrences, ...duplicateRefs, ...cycleRefs])
    ),
    kind: "tree",
    occurrences,
    duplicate_refs: duplicateRefs,
    cycle_refs: cycleRefs,
  };
}

function buildFlow(context: BuildContext): FlowRenderData {
  const view = context.input.view_data.flow!;
  const lanesInput = [...view.lanes];
  const stepsInput = [...view.steps];
  const horizontal = isHorizontal(context);
  const laneThickness =
    context.input.layout.item_width +
    context.input.layout.lane_header_size +
    context.input.layout.horizontal_gap * 2;
  const stepStride =
    (horizontal
      ? context.input.layout.item_width
      : context.input.layout.item_height) +
    (horizontal
      ? context.input.layout.horizontal_gap
      : context.input.layout.vertical_gap);
  const laneSet = new Set(lanesInput.map((lane) => lane.key));
  const stepSet = new Set(stepsInput.map((step) => step.key));
  for (const step of stepsInput)
    if (!laneSet.has(step.lane_key))
      fail("render.geometry_invalid", "missing_flow_lane", 0);
  for (const lane of lanesInput)
    for (const stepKey of lane.step_keys)
      if (!stepSet.has(stepKey))
        fail("render.geometry_invalid", "missing_flow_lane_step", 0);
  const stepByKey = new Map(stepsInput.map((step) => [step.key, step]));
  const stepsByLane = new Map(
    lanesInput.map((lane) => [
      lane.key,
      lane.step_keys.map((stepKey) => stepByKey.get(stepKey)!),
    ])
  );
  let longest = 1;
  for (const steps of stepsByLane.values())
    longest = Math.max(longest, steps.length);
  const laneKeys = new Map<ViewDataItemKey, string>();
  const stepKeys = new Map<ViewDataItemKey, string>();
  const stepBounds = new Map<ViewDataItemKey, RenderBounds>();
  const lanes: FlowRenderData["lanes"][number][] = [];
  for (const [index, lane] of lanesInput.entries()) {
    const key = renderKey("flow-lane", lane.key);
    laneKeys.set(lane.key, key);
    const bounds = horizontal
      ? context.rect(
          0,
          index * laneThickness,
          longest * stepStride + context.input.layout.lane_header_size,
          laneThickness
        )
      : context.rect(
          index * laneThickness,
          0,
          laneThickness,
          longest * stepStride + context.input.layout.lane_header_size
        );
    lanes.push({ render_key: key, bounds, label: context.text(lane.label) });
    context.bind(key, lane.key, lane.source);
  }
  const steps: FlowRenderData["steps"][number][] = [];
  const branches: FlowRenderData["branches"][number][] = [];
  const joins: FlowRenderData["joins"][number][] = [];
  for (const [laneIndex, lane] of lanesInput.entries())
    for (const [stepIndex, step] of (
      stepsByLane.get(lane.key) ?? []
    ).entries()) {
      const laneSteps = stepsByLane.get(lane.key) ?? [];
      const effectiveStepIndex = reverseOrientation(context)
        ? laneSteps.length - stepIndex - 1
        : stepIndex;
      const x = horizontal
        ? context.input.layout.lane_header_size +
          effectiveStepIndex * stepStride
        : laneIndex * laneThickness + context.input.layout.horizontal_gap;
      const y = horizontal
        ? laneIndex * laneThickness + context.input.layout.vertical_gap
        : context.input.layout.lane_header_size +
          effectiveStepIndex * stepStride;
      const bounds = context.rect(
        x,
        y,
        context.input.layout.item_width,
        context.input.layout.item_height
      );
      const key = renderKey("flow-step", step.key);
      stepKeys.set(step.key, key);
      stepBounds.set(step.key, bounds);
    }
  for (const step of stepsInput) {
    const key = stepKeys.get(step.key)!;
    const bounds = stepBounds.get(step.key)!;
    steps.push({
      render_key: key,
      bounds,
      lane_key: laneKeys.get(step.lane_key)!,
      label: context.text(shortAddress(step.entity_address)),
    });
    context.bind(key, step.key, step.source);
    if (step.branch) {
      const branchKey = renderKey("flow-branch", step.key);
      branches.push({
        render_key: branchKey,
        position: edgePoint(context, bounds, true),
        step_keys: [key],
      });
      context.bind(branchKey, step.key, step.source);
    }
    if (step.join) {
      const joinKey = renderKey("flow-join", step.key);
      joins.push({
        render_key: joinKey,
        position: edgePoint(context, bounds, false),
        step_keys: [key],
      });
      context.bind(joinKey, step.key, step.source);
    }
  }
  const connector = (
    item: FlowConnector | FlowCycleRef,
    prefix: string
  ): FlowRenderData["connectors"][number] => {
    const from = stepBounds.get(item.from_step_key);
    const to = stepBounds.get(item.to_step_key);
    if (from === undefined || to === undefined)
      fail("render.geometry_invalid", "missing_flow_step", 0);
    const key = renderKey(prefix, item.key);
    const result = {
      render_key: key,
      points: route(
        context,
        edgePoint(context, from, true),
        edgePoint(context, to, false)
      ),
      from_step_key: stepKeys.get(item.from_step_key)!,
      to_step_key: stepKeys.get(item.to_step_key)!,
      connector_kind: item.kind,
      label: context.text(scalarText(item.branch_value)),
    };
    context.bind(key, item.key, item.source);
    return result;
  };
  const connectors = view.connectors.map((item) =>
    connector(item, "flow-connector")
  );
  const cycleRefs = view.cycle_refs.map((item) =>
    connector(item, "flow-cycle")
  );
  return {
    ...base(
      context,
      "flow",
      allBounds([
        ...lanes,
        ...steps,
        ...branches,
        ...joins,
        ...connectors,
        ...cycleRefs,
      ])
    ),
    kind: "flow",
    lanes,
    steps,
    branches,
    joins,
    connectors,
    cycle_refs: cycleRefs,
  };
}

function buildContext(context: BuildContext): ContextRenderData {
  const view = context.input.view_data.context!;
  const groupsInput = [...view.groups];
  const groups: ContextRenderData["groups"][number][] = [];
  const facts: ContextRenderData["facts"][number][] = [];
  const summaries: ContextRenderData["relation_summaries"][number][] = [];
  let y = 0;
  for (const group of groupsInput) {
    const entries: (
      | { kind: "fact"; item: ContextAttribute; text: string }
      | { kind: "summary"; item: ContextFact; text: string }
    )[] = [
      ...group.attributes.map((item) => ({
        kind: "fact" as const,
        item,
        text: contextAttributeText(item),
      })),
      ...group.facts.map((item) => ({
        kind: "summary" as const,
        item,
        text: item.text,
      })),
    ];
    let width = Math.max(
      context.input.layout.item_width,
      context.measure(group.label).width + context.input.layout.cell_padding * 2
    );
    for (const entry of entries)
      width = Math.max(
        width,
        context.measure(entry.text).width +
          context.input.layout.cell_padding * 2
      );
    const rowHeight = Math.max(
      context.input.layout.item_height,
      context.measure("M").height + context.input.layout.cell_padding * 2
    );
    const groupHeight =
      rowHeight * (entries.length + 1) +
      context.input.layout.container_padding * 2;
    const groupKey = renderKey("context-group", group.key);
    const groupBounds = context.rect(
      0,
      y,
      width + context.input.layout.container_padding * 2,
      groupHeight
    );
    groups.push({
      render_key: groupKey,
      bounds: groupBounds,
      label: context.text(group.label),
    });
    context.bind(groupKey, group.key, group.source);
    for (const [index, entry] of entries.entries()) {
      const key = renderKey(
        entry.kind === "fact" ? "context-fact" : "context-summary",
        entry.item.key
      );
      const primitive = {
        render_key: key,
        bounds: context.rect(
          context.input.layout.container_padding,
          y + context.input.layout.container_padding + (index + 1) * rowHeight,
          width,
          rowHeight
        ),
        group_key: groupKey,
        text: context.text(entry.text),
      };
      if (entry.kind === "fact") facts.push(primitive);
      else summaries.push(primitive);
      context.bind(key, entry.item.key, entry.item.source);
    }
    y += groupHeight + context.input.layout.vertical_gap;
  }
  return {
    ...base(context, "context", allBounds([...groups, ...facts, ...summaries])),
    kind: "context",
    groups,
    facts,
    relation_summaries: summaries,
  };
}

function buildDiff(context: BuildContext): DiffRenderData {
  const changesInput = [...context.input.view_data.diff!.changes];
  const before: DiffRenderData["before"][number][] = [];
  const after: DiffRenderData["after"][number][] = [];
  const changes: DiffRenderData["changes"][number][] = [];
  const fields: DiffRenderData["fields"][number][] = [];
  const sideWidth = context.input.layout.item_width * 2;
  const rowHeight = Math.max(
    context.input.layout.item_height,
    context.measure("M").height + context.input.layout.cell_padding * 2
  );
  let y = 0;
  for (const change of changesInput) {
    const beforeKey =
      change.before_address === undefined
        ? undefined
        : renderKey("diff-before", change.key);
    const afterKey =
      change.after_address === undefined
        ? undefined
        : renderKey("diff-after", change.key);
    if (beforeKey !== undefined) {
      before.push({
        render_key: beforeKey,
        bounds: context.rect(0, y, sideWidth, rowHeight),
        label: context.text(shortAddress(change.before_address!)),
      });
      context.bind(
        beforeKey,
        change.key,
        change.before_source ?? change.source
      );
    }
    if (afterKey !== undefined) {
      after.push({
        render_key: afterKey,
        bounds: context.rect(
          sideWidth + context.input.layout.horizontal_gap,
          y,
          sideWidth,
          rowHeight
        ),
        label: context.text(shortAddress(change.after_address!)),
      });
      context.bind(afterKey, change.key, change.after_source ?? change.source);
    }
    const changeKey = renderKey("diff-change", change.key);
    const changeBounds = context.rect(
      0,
      y,
      sideWidth * 2 + context.input.layout.horizontal_gap,
      rowHeight * Math.max(1, change.fields.length + 1)
    );
    const primitive = changePrimitive(
      change,
      changeKey,
      changeBounds,
      beforeKey,
      afterKey
    );
    changes.push(primitive);
    context.bind(changeKey, change.key, change.source);
    for (const [index, field] of change.fields.entries()) {
      const key = renderKey("diff-field", field.key);
      fields.push({
        render_key: key,
        bounds: context.rect(
          context.input.layout.cell_padding,
          y + (index + 1) * rowHeight,
          changeBounds.width - context.input.layout.cell_padding * 2,
          rowHeight
        ),
        change_key: changeKey,
        field_path: context.text(field.path.join(".")),
        ...(field.before_present
          ? { before_text: context.text(semanticText(field.before)) }
          : {}),
        ...(field.after_present
          ? { after_text: context.text(semanticText(field.after)) }
          : {}),
      });
      context.bind(key, field.key, change.source);
    }
    y += changeBounds.height + context.input.layout.vertical_gap;
  }
  return {
    ...base(
      context,
      "diff",
      allBounds([...before, ...after, ...changes, ...fields])
    ),
    kind: "diff",
    before,
    after,
    changes,
    fields,
  };
}

function changePrimitive(
  change: DiffChange,
  renderKeyValue: string,
  bounds: RenderBounds,
  beforeKey: string | undefined,
  afterKey: string | undefined
): DiffRenderData["changes"][number] {
  if (change.kind === "added") {
    if (afterKey === undefined)
      fail("render.geometry_invalid", "missing_diff_after", 0);
    return {
      render_key: renderKeyValue,
      bounds,
      change_kind: "added",
      after_key: afterKey,
    };
  }
  if (change.kind === "removed") {
    if (beforeKey === undefined)
      fail("render.geometry_invalid", "missing_diff_before", 0);
    return {
      render_key: renderKeyValue,
      bounds,
      change_kind: "removed",
      before_key: beforeKey,
    };
  }
  if (beforeKey === undefined || afterKey === undefined)
    fail("render.geometry_invalid", "missing_diff_side", 0);
  return {
    render_key: renderKeyValue,
    bounds,
    change_kind: change.kind,
    before_key: beforeKey,
    after_key: afterKey,
  };
}

function layoutPositions(
  context: BuildContext,
  keys: readonly ViewDataItemKey[],
  depths: ReadonlyMap<ViewDataItemKey, number>
): Map<ViewDataItemKey, RenderPoint> {
  const output = new Map<ViewDataItemKey, RenderPoint>();
  const algorithm = context.input.recipe.layout_algorithm;
  const orderedKeys = [...keys];
  const density = densityScale(context.input.recipe);
  const rankSeparation =
    algorithm.kind === "layered" ? algorithm.rank_separation : 0;
  const widthStride =
    context.input.layout.item_width +
    Math.max(context.input.layout.horizontal_gap * density, rankSeparation);
  const heightStride =
    context.input.layout.item_height +
    Math.max(context.input.layout.vertical_gap * density, rankSeparation);
  if (algorithm.kind === "grid") {
    for (const [index, key] of orderedKeys.entries())
      output.set(key, {
        x: (index % algorithm.columns) * widthStride,
        y: Math.floor(index / algorithm.columns) * heightStride,
      });
  } else if (algorithm.kind === "radial") {
    const directions = [
      [1, 0],
      [1, 1],
      [0, 1],
      [-1, 1],
      [-1, 0],
      [-1, -1],
      [0, -1],
      [1, -1],
    ] as const;
    for (const [index, key] of orderedKeys.entries()) {
      const ring = Math.floor(index / 8) + 1;
      const direction = directions[index % 8]!;
      output.set(key, {
        x: (ring + direction[0] * ring) * algorithm.radius_step,
        y: (ring + direction[1] * ring) * algorithm.radius_step,
      });
    }
  } else {
    const groups = new Map<number, ViewDataItemKey[]>();
    for (const key of orderedKeys) {
      const rank = depths.get(key) ?? 0;
      const group = groups.get(rank) ?? [];
      group.push(key);
      groups.set(rank, group);
    }
    let maxRank = 0;
    for (const rank of groups.keys()) maxRank = Math.max(maxRank, rank);
    for (const [rank, group] of groups)
      for (const [index, key] of group.entries()) {
        const effectiveRank = reverseOrientation(context)
          ? maxRank - rank
          : rank;
        output.set(
          key,
          isHorizontal(context)
            ? { x: effectiveRank * widthStride, y: index * heightStride }
            : { x: index * widthStride, y: effectiveRank * heightStride }
        );
      }
  }
  return output;
}

function route(
  context: BuildContext,
  from: RenderPoint,
  to: RenderPoint
): readonly RenderPoint[] {
  if (isHorizontal(context)) {
    const middle = context.number((from.x + to.x) / 2);
    return context.points([
      from,
      { x: middle, y: from.y },
      { x: middle, y: to.y },
      to,
    ]);
  }
  const middle = context.number((from.y + to.y) / 2);
  return context.points([
    from,
    { x: from.x, y: middle },
    { x: to.x, y: middle },
    to,
  ]);
}

function edgePoint(
  context: BuildContext,
  bounds: RenderBounds,
  outgoing: boolean
): RenderPoint {
  if (isHorizontal(context))
    return {
      x: context.number(
        outgoing
          ? bounds.x + bounds.width + context.input.layout.port_offset
          : bounds.x - context.input.layout.port_offset
      ),
      y: context.number(bounds.y + bounds.height / 2),
    };
  return {
    x: context.number(bounds.x + bounds.width / 2),
    y: context.number(
      outgoing
        ? bounds.y + bounds.height + context.input.layout.port_offset
        : bounds.y - context.input.layout.port_offset
    ),
  };
}

function allBounds(
  values: readonly {
    readonly bounds?: RenderBounds;
    readonly points?: readonly RenderPoint[];
    readonly position?: RenderPoint;
  }[]
): RenderBounds {
  const bounds: RenderBounds[] = [];
  for (const value of values) {
    if (value.bounds !== undefined) bounds.push(value.bounds);
    if (value.points !== undefined)
      for (const point of value.points)
        bounds.push({ x: point.x, y: point.y, width: 0, height: 0 });
    if (value.position !== undefined)
      bounds.push({
        x: value.position.x,
        y: value.position.y,
        width: 0,
        height: 0,
      });
  }
  return bounds.length === 0
    ? { x: 0, y: 0, width: 0, height: 0 }
    : union(bounds);
}

function union(bounds: readonly RenderBounds[]): RenderBounds {
  let minX = Number.POSITIVE_INFINITY;
  let minY = Number.POSITIVE_INFINITY;
  let maxX = Number.NEGATIVE_INFINITY;
  let maxY = Number.NEGATIVE_INFINITY;
  for (const item of bounds) {
    minX = Math.min(minX, item.x);
    minY = Math.min(minY, item.y);
    maxX = Math.max(maxX, item.x + item.width);
    maxY = Math.max(maxY, item.y + item.height);
  }
  return { x: minX, y: minY, width: maxX - minX, height: maxY - minY };
}

function expand(
  context: BuildContext,
  bounds: RenderBounds,
  amount: number
): RenderBounds {
  return context.rect(
    bounds.x - amount,
    bounds.y - amount,
    bounds.width + amount * 2,
    bounds.height + amount * 2
  );
}
function center(bounds: RenderBounds): RenderPoint {
  return { x: bounds.x + bounds.width / 2, y: bounds.y + bounds.height / 2 };
}
function centeredText(
  context: BuildContext,
  bounds: RenderBounds,
  text: string
): RenderBounds {
  const measured = context.measure(text);
  return context.rect(
    bounds.x + (bounds.width - measured.width) / 2,
    bounds.y + (bounds.height - measured.height) / 2,
    measured.width,
    measured.height
  );
}
function renderKey(kind: string, key: string): string {
  return `${kind}:${key}`;
}
function shortAddress(address: string): string {
  return address.split(":").at(-1) ?? address;
}
function compare(left: string, right: string): number {
  return left < right ? -1 : left > right ? 1 : 0;
}

function effectiveOrientation(
  context: BuildContext
): NonNullable<RenderRecipe["orientation"]>["value"] {
  if (context.input.recipe.orientation !== undefined)
    return context.input.recipe.orientation.value;
  if (context.input.view_data.kind === "diagram")
    return context.input.view_data.shape.diagram!.direction;
  return "top_to_bottom";
}
function isHorizontal(context: BuildContext): boolean {
  return (
    effectiveOrientation(context) === "left_to_right" ||
    effectiveOrientation(context) === "right_to_left"
  );
}
function reverseOrientation(context: BuildContext): boolean {
  return (
    effectiveOrientation(context) === "bottom_to_top" ||
    effectiveOrientation(context) === "right_to_left"
  );
}
function densityScale(recipe: RenderRecipe): number {
  return recipe.density.value === "compact"
    ? 0.8
    : recipe.density.value === "spacious"
    ? 1.25
    : 1;
}

function largestAsset(
  context: BuildContext,
  source: ViewDataSourceRefs
): ResolvedAssetDimensions | undefined {
  const allowed = new Set(source.asset_digests);
  return context.input.resolved_assets
    .filter((asset) => allowed.has(asset.digest))
    .sort(
      (left, right) =>
        right.width * right.height - left.width * left.height ||
        compare(left.digest, right.digest)
    )[0];
}

function tableText(cell: TableCell | undefined): string {
  return cell?.present === true ? viewDataValueText(cell.value) : "";
}
function viewDataValueText(value: ViewDataValue | undefined): string {
  if (value === undefined) return "";
  if (value.kind === "stable_address") return value.stable_address ?? "";
  if (value.kind === "string_set") return (value.string_set ?? []).join(", ");
  return scalarText(value.scalar);
}
function scalarText(value: RecipeScalar | undefined): string {
  if (value === undefined) return "";
  if (value.kind === "boolean")
    return value.boolean_value === true ? "true" : "false";
  if (value.kind === "integer") return String(value.integer_value ?? "");
  if (value.kind === "number") return value.number_value ?? "";
  return value.string_value ?? "";
}
function matrixText(value: MatrixDisplayValue): string {
  if (value.kind === "boolean")
    return value.boolean === true ? "true" : "false";
  if (value.kind === "integer") return value.integer ?? "";
  if (value.kind === "string_set") return (value.string_set ?? []).join(", ");
  return (value.attributes ?? [])
    .map(
      (item) => `${shortAddress(item.column_address)}=${scalarText(item.value)}`
    )
    .join(", ");
}
function contextAttributeText(item: ContextAttribute): string {
  return Object.entries(item.values)
    .sort(([left], [right]) => compare(left, right))
    .map(([key, value]) => `${shortAddress(key)}=${scalarText(value)}`)
    .join(", ");
}
function semanticText(value: ViewDataSemanticValue | undefined): string {
  if (value === undefined || value.kind === "absent") return "";
  if (value.kind === "address") return value.address ?? "";
  if (value.kind === "boolean")
    return value.boolean === true ? "true" : "false";
  if (value.kind === "decimal") return value.decimal ?? "";
  if (value.kind === "integer") return value.integer ?? "";
  if (value.kind === "string") return value.string ?? "";
  if (value.kind === "token") return value.token ?? "";
  if (value.kind === "blob") return value.blob?.digest ?? "";
  if (value.kind === "array")
    return `[${(value.array ?? []).map(semanticText).join(",")}]`;
  return `{${(value.map ?? [])
    .map((entry) => `${entry.key}:${semanticText(entry.value)}`)
    .join(",")}}`;
}

function viewDataDiagnostics(
  values: readonly Diagnostic[]
): StableRenderDiagnostic[] {
  return values.map((value) => ({
    code: value.code,
    severity: value.severity === "info" ? "information" : value.severity,
    message_key: value.message_key,
    arguments: diagnosticArguments(value.arguments),
    protocol_version: 1,
    ...(value.owner_address === undefined
      ? {}
      : { owner_address: value.owner_address }),
    ...(value.subject_address === undefined
      ? {}
      : { subject_address: value.subject_address }),
  }));
}

function diagnosticArguments(
  values: Readonly<Record<string, DiagnosticArgumentValue>>
): Readonly<Record<string, JsonValue>> {
  const output: Record<string, JsonValue> = {};
  for (const key of Object.keys(values).sort(compare))
    output[key] = diagnosticArgumentValue(values[key]!);
  return output;
}

function diagnosticArgumentValue(value: DiagnosticArgumentValue): JsonValue {
  switch (value.kind) {
    case "array":
      return value.array_value!.map(diagnosticArgumentValue);
    case "boolean":
      return value.boolean_value!;
    case "integer":
      return value.integer_value!;
    case "number":
      return value.number_value!;
    case "object":
      return diagnosticArguments(value.object_value!);
    case "stable_address":
      return value.stable_address_value!;
    case "string":
      return value.string_value!;
  }
}

function columnSource(column: TableColumn): ViewDataSourceRefs {
  const subjectAddresses = [...column.source_column_addresses].sort(compare);
  if (column.address !== undefined) subjectAddresses.push(column.address);
  subjectAddresses.sort(compare);
  return {
    asset_digests: [],
    cell_refs: [],
    entity_addresses: [],
    layer_addresses: [],
    relation_addresses: [],
    row_addresses: [],
    state: { reads: [] },
    subject_addresses: [...new Set(subjectAddresses)],
  };
}

function collectAssetDigests(value: unknown, output: Set<string>): void {
  if (Array.isArray(value)) {
    for (const item of value) collectAssetDigests(item, output);
    return;
  }
  if (value === null || typeof value !== "object") return;
  for (const [key, item] of Object.entries(value)) {
    if (key === "asset_digests" && Array.isArray(item)) {
      for (const digest of item)
        if (typeof digest === "string") output.add(digest);
    } else {
      collectAssetDigests(item, output);
    }
  }
}

function stableDiagnostics(
  values: readonly RenderMaterializationDiagnostic[]
): RenderMaterializationDiagnostic[] {
  return [...values].sort(
    (left, right) =>
      compare(left.code, right.code) ||
      compare(canonical(left.arguments), canonical(right.arguments))
  );
}
function diagnostic(
  code: RenderMaterializationDiagnosticCode,
  argumentsValue: Readonly<Record<string, string | boolean>>
): RenderMaterializationDiagnostic {
  return {
    code,
    severity: "error",
    message_key: code,
    arguments: argumentsValue,
  };
}
function fail(
  code: RenderMaterializationDiagnosticCode,
  resource: string,
  limit: number
): never {
  throw new MaterializationFailure(
    diagnostic(
      code,
      code === "render.resource_limit"
        ? { resource, limit: String(limit) }
        : { reason: resource }
    )
  );
}
function isDigest(value: unknown): value is Digest {
  return typeof value === "string" && digestPattern.test(value);
}
function positive(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && value > 0;
}
function nonnegative(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0;
}
function closedRecord(value: unknown): value is Record<string, number> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
function closedObject(
  value: unknown,
  keys: readonly string[]
): value is Record<string, unknown> {
  return (
    value !== null &&
    typeof value === "object" &&
    !Array.isArray(value) &&
    Object.keys(value).every((key) => keys.includes(key)) &&
    keys.every((key) => Object.hasOwn(value, key))
  );
}
function sortedEnum<T extends string>(
  value: unknown,
  order: readonly T[]
): value is readonly T[] {
  if (
    !Array.isArray(value) ||
    value.some((item) => typeof item !== "string" || !order.includes(item as T))
  )
    return false;
  const indexes = value.map((item) => order.indexOf(item as T));
  return (
    new Set(indexes).size === indexes.length &&
    indexes.every((item, index) => index === 0 || indexes[index - 1]! < item)
  );
}

function dummyRecipe(profile: RendererProfileRef): RenderRecipe {
  const digest = profile?.specification_digest;
  return {
    render_recipe_schema_version: 1,
    renderer_profile: profile,
    shape: { kind: "diagram" },
    layout_algorithm: { kind: "native" },
    layout_seed: { value: "validation" },
    density: { value: "comfortable" },
    theme: {
      theme_id: "validation",
      theme_version: "1",
      specification_digest: digest,
      color_scheme: "light",
    },
    locale: { language_tag: "en" },
    timezone: { iana_name: "UTC" },
    font_policy: {
      families: ["validation"],
      fallback: "forbid",
      required_digests: [],
    },
    asset_policy: { mode: "resolved_only", required_digests: [] },
  };
}

function canonical(value: unknown): string {
  if (value === null || typeof value === "boolean" || typeof value === "string")
    return JSON.stringify(value);
  if (typeof value === "number") {
    if (!Number.isFinite(value)) return "null";
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
  if (typeof value === "object")
    return `{${Object.entries(value as Record<string, unknown>)
      .filter(([, item]) => item !== undefined)
      .sort(([left], [right]) => compare(left, right))
      .map(([key, item]) => `${JSON.stringify(key)}:${canonical(item)}`)
      .join(",")}}`;
  return JSON.stringify(String(value));
}
