// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {
  isJsonValue,
  type Digest,
  type JsonValue,
} from "@layerdraw/protocol/common";
import {
  isFlowConnectorKind,
  isStableAddress,
  isTableViewValueType,
  isViewDataItemKey,
  isViewDataSourceRefs,
  type FlowConnectorKind,
  type StableAddress,
  type TableViewValueType,
  type ViewDataItemKey,
  type ViewDataSourceRefs,
} from "@layerdraw/protocol/semantic";

export type RenderShape =
  | "diagram"
  | "table"
  | "matrix"
  | "tree"
  | "flow"
  | "context"
  | "diff";
export type RendererProfileRef = Readonly<{
  profile_id: string;
  profile_version: string;
  specification_digest: Digest;
}>;

export type ShapeOptions = Readonly<{ kind: RenderShape }>;
export type AlgorithmOptions =
  | Readonly<{
      kind: "layered";
      crossing_reduction: "barycenter" | "median";
      rank_separation: number;
    }>
  | Readonly<{ kind: "grid"; columns: number }>
  | Readonly<{ kind: "radial"; radius_step: number }>
  | Readonly<{ kind: "native" }>;
export type LayoutSeedOptions = Readonly<{ value: string }>;
export type DensityOptions = Readonly<{
  value: "compact" | "comfortable" | "spacious";
}>;
export type OrientationOptions = Readonly<{
  value: "top_to_bottom" | "bottom_to_top" | "left_to_right" | "right_to_left";
}>;
export type ViewportOptions = Readonly<{
  width: number;
  height: number;
  device_scale: number;
}>;
export type ThemeOptions = Readonly<{
  theme_id: string;
  theme_version: string;
  specification_digest: Digest;
  color_scheme: "light" | "dark";
}>;
export type LocaleOptions = Readonly<{ language_tag: string }>;
export type TimezoneOptions = Readonly<{ iana_name: string }>;
export type FontPolicy = Readonly<{
  families: readonly string[];
  fallback: "forbid";
  required_digests: readonly Digest[];
}>;
export type AssetPolicy = Readonly<{
  mode: "resolved_only" | "embed";
  required_digests: readonly Digest[];
}>;
export type RasterizerProfileRef = Readonly<{
  profile_id: string;
  profile_version: string;
  specification_digest: Digest;
}>;
export type InteractionPolicy = Readonly<{
  selection: boolean;
  pan: boolean;
  zoom: boolean;
}>;

export interface RenderRecipe {
  readonly render_recipe_schema_version: 1;
  readonly renderer_profile: RendererProfileRef;
  readonly shape: ShapeOptions;
  readonly layout_algorithm: AlgorithmOptions;
  readonly layout_seed: LayoutSeedOptions;
  readonly density: DensityOptions;
  readonly orientation?: OrientationOptions;
  readonly viewport?: ViewportOptions;
  readonly theme: ThemeOptions;
  readonly locale: LocaleOptions;
  readonly timezone: TimezoneOptions;
  readonly font_policy: FontPolicy;
  readonly asset_policy: AssetPolicy;
  readonly rasterizer_profile?: RasterizerProfileRef;
  readonly interaction_policy?: InteractionPolicy;
}

export type RenderBounds = Readonly<{
  x: number;
  y: number;
  width: number;
  height: number;
}>;
export type RenderPoint = Readonly<{ x: number; y: number }>;
export type StableRenderDiagnostic = Readonly<{
  code: string;
  severity: "error" | "warning" | "information" | "hint";
  message_key: string;
  arguments: Readonly<Record<string, JsonValue>>;
  protocol_version?: 1;
  owner_address?: StableAddress;
  subject_address?: StableAddress;
}>;
export type RenderSourceBinding = Readonly<{
  render_key: string;
  viewdata_key?: ViewDataItemKey;
  source_refs?: ViewDataSourceRefs;
}>;
export interface VisualPrimitive {
  readonly render_key: string;
}
export type DiagramContainerPrimitive = VisualPrimitive &
  Readonly<{ bounds: RenderBounds; child_keys: readonly string[] }>;
export type DiagramOccurrencePrimitive = VisualPrimitive &
  Readonly<{
    bounds: RenderBounds;
    port_keys: readonly string[];
    label_key?: string;
  }>;
export type DiagramPortPrimitive = VisualPrimitive &
  Readonly<{ position: RenderPoint; occurrence_key: string }>;
export type DiagramEdgePathPrimitive = VisualPrimitive &
  Readonly<{
    points: readonly RenderPoint[];
    from_port_key: string;
    to_port_key: string;
  }>;
export type DiagramLabelAnchor =
  | Readonly<{ kind: "occurrence"; occurrence_key: string }>
  | Readonly<{ kind: "edge_path"; edge_path_key: string }>;
export type DiagramDecorationTarget =
  | Readonly<{ kind: "occurrence"; occurrence_key: string }>
  | Readonly<{ kind: "container"; container_key: string }>;
export type DiagramLabelPrimitive = VisualPrimitive &
  Readonly<{ bounds: RenderBounds; text: string; anchor: DiagramLabelAnchor }>;
export type DiagramOverlayPrimitive = VisualPrimitive &
  Readonly<{
    bounds: RenderBounds;
    target: DiagramDecorationTarget;
    display_identity: string;
  }>;
export type DiagramBadgePrimitive = VisualPrimitive &
  Readonly<{
    bounds: RenderBounds;
    target: DiagramDecorationTarget;
    label: string;
  }>;
export type DiagramSupportPrimitive = VisualPrimitive &
  Readonly<{
    bounds: RenderBounds;
    support_kind: "hidden_entity" | "hidden_relation" | "source_only";
    display_identity: string;
  }>;
export type TreeOccurrencePrimitive = VisualPrimitive &
  Readonly<{
    bounds: RenderBounds;
    parent_key?: string;
    depth: number;
    label: string;
  }>;
export type TreeReferencePrimitive = VisualPrimitive &
  Readonly<{
    points: readonly RenderPoint[];
    from_occurrence_key: string;
    to_occurrence_key: string;
  }>;
export type FlowLanePrimitive = VisualPrimitive &
  Readonly<{ bounds: RenderBounds; label: string }>;
export type FlowStepPrimitive = VisualPrimitive &
  Readonly<{ bounds: RenderBounds; lane_key: string; label: string }>;
export type FlowJunctionPrimitive = VisualPrimitive &
  Readonly<{ position: RenderPoint; step_keys: readonly string[] }>;
export type FlowConnectorPrimitive = VisualPrimitive &
  Readonly<{
    points: readonly RenderPoint[];
    from_step_key: string;
    to_step_key: string;
    connector_kind: FlowConnectorKind;
    label: string;
  }>;
export type TableColumnPrimitive = VisualPrimitive &
  Readonly<{
    x: number;
    width: number;
    frozen: boolean;
    id: string;
    label: string;
    value_type: TableViewValueType;
    header_bounds: RenderBounds;
  }>;
export type TableRowPrimitive = VisualPrimitive &
  Readonly<{ y: number; height: number; frozen: boolean }>;
export type TableCellPrimitive = VisualPrimitive &
  Readonly<{
    bounds: RenderBounds;
    row_key: string;
    column_key: string;
    text: string;
  }>;
export type MatrixAxisPrimitive = VisualPrimitive &
  Readonly<{ bounds: RenderBounds; label: string }>;
export type MatrixCellPrimitive = VisualPrimitive &
  Readonly<{
    bounds: RenderBounds;
    row_axis_key: string;
    column_axis_key: string;
    text: string;
  }>;
export type MatrixTotalPrimitive = VisualPrimitive &
  Readonly<{ bounds: RenderBounds; axis_key: string; text: string }>;
export type ContextGroupPrimitive = VisualPrimitive &
  Readonly<{ bounds: RenderBounds; label: string }>;
export type ContextFactPrimitive = VisualPrimitive &
  Readonly<{ bounds: RenderBounds; group_key: string; text: string }>;
export type DiffSidePrimitive = VisualPrimitive &
  Readonly<{ bounds: RenderBounds; label: string }>;
export type DiffChangePrimitive = VisualPrimitive &
  Readonly<{ bounds: RenderBounds }> &
  (
    | Readonly<{ change_kind: "added"; after_key: string; before_key?: never }>
    | Readonly<{
        change_kind: "removed";
        before_key: string;
        after_key?: never;
      }>
    | Readonly<{
        change_kind: "updated" | "moved" | "moved_updated";
        before_key: string;
        after_key: string;
      }>
  );
export type DiffFieldPrimitive = VisualPrimitive &
  Readonly<{
    bounds: RenderBounds;
    change_key: string;
    field_path: string;
    before_text?: string;
    after_text?: string;
  }>;

export interface RenderDataBase {
  readonly render_data_schema_version: 1;
  readonly renderer_profile: RendererProfileRef;
  readonly view_data_hash: Digest;
  readonly render_input_hash: Digest;
  readonly shape: RenderShape;
  readonly layout_seed: string;
  readonly locale: string;
  readonly timezone: string;
  readonly bounds: RenderBounds;
  readonly source_bindings: readonly RenderSourceBinding[];
  readonly resolved_asset_digests: readonly Digest[];
  readonly resolved_font_digests: readonly Digest[];
  readonly diagnostics: readonly StableRenderDiagnostic[];
}

export type DiagramRenderData = RenderDataBase &
  Readonly<{
    kind: "diagram";
    containers: readonly DiagramContainerPrimitive[];
    occurrences: readonly DiagramOccurrencePrimitive[];
    ports: readonly DiagramPortPrimitive[];
    edge_paths: readonly DiagramEdgePathPrimitive[];
    labels: readonly DiagramLabelPrimitive[];
    overlays: readonly DiagramOverlayPrimitive[];
    badges: readonly DiagramBadgePrimitive[];
    support_geometry: readonly DiagramSupportPrimitive[];
  }>;
export type TableRenderData = RenderDataBase &
  Readonly<{
    kind: "table";
    columns: readonly TableColumnPrimitive[];
    rows: readonly TableRowPrimitive[];
    cells: readonly TableCellPrimitive[];
  }>;
export type MatrixRenderData = RenderDataBase &
  Readonly<{
    kind: "matrix";
    row_axes: readonly MatrixAxisPrimitive[];
    column_axes: readonly MatrixAxisPrimitive[];
    cells: readonly MatrixCellPrimitive[];
    totals: readonly MatrixTotalPrimitive[];
  }>;
export type TreeRenderData = RenderDataBase &
  Readonly<{
    kind: "tree";
    occurrences: readonly TreeOccurrencePrimitive[];
    duplicate_refs: readonly TreeReferencePrimitive[];
    cycle_refs: readonly TreeReferencePrimitive[];
  }>;
export type FlowRenderData = RenderDataBase &
  Readonly<{
    kind: "flow";
    lanes: readonly FlowLanePrimitive[];
    steps: readonly FlowStepPrimitive[];
    branches: readonly FlowJunctionPrimitive[];
    joins: readonly FlowJunctionPrimitive[];
    connectors: readonly FlowConnectorPrimitive[];
    cycle_refs: readonly FlowConnectorPrimitive[];
  }>;
export type ContextRenderData = RenderDataBase &
  Readonly<{
    kind: "context";
    groups: readonly ContextGroupPrimitive[];
    facts: readonly ContextFactPrimitive[];
    relation_summaries: readonly ContextFactPrimitive[];
  }>;
export type DiffRenderData = RenderDataBase &
  Readonly<{
    kind: "diff";
    before: readonly DiffSidePrimitive[];
    after: readonly DiffSidePrimitive[];
    changes: readonly DiffChangePrimitive[];
    fields: readonly DiffFieldPrimitive[];
  }>;
export type RenderData =
  | DiagramRenderData
  | TableRenderData
  | MatrixRenderData
  | TreeRenderData
  | FlowRenderData
  | ContextRenderData
  | DiffRenderData;

const digestPattern = /^sha256:[0-9a-f]{64}$/;
const profilePattern = /^[a-z0-9][a-z0-9._/-]*$/;
const versionPattern = /^[1-9][0-9]*(?:\.[0-9]+){0,2}$/;

export class RenderContractError extends TypeError {
  constructor(
    readonly code:
      | "render.recipe_invalid"
      | "render.profile_incompatible"
      | "render.data_invalid",
    message: string
  ) {
    super(message);
  }
}

export function assertRenderRecipe(
  value: unknown
): asserts value is RenderRecipe {
  const recipe = object(value, "render.recipe_invalid", "RenderRecipe");
  exact(
    recipe,
    [
      "render_recipe_schema_version",
      "renderer_profile",
      "shape",
      "layout_algorithm",
      "layout_seed",
      "density",
      "orientation",
      "viewport",
      "theme",
      "locale",
      "timezone",
      "font_policy",
      "asset_policy",
      "rasterizer_profile",
      "interaction_policy",
    ],
    "render.recipe_invalid"
  );
  if (recipe.render_recipe_schema_version !== 1)
    invalid("render.recipe_invalid", "unsupported RenderRecipe schema version");
  profile(recipe.renderer_profile, "renderer_profile");
  closed(recipe.shape, ["kind"], "shape");
  if (!shape(String((recipe.shape as Record<string, unknown>).kind)))
    invalid("render.recipe_invalid", "unknown shape");
  algorithm(recipe.layout_algorithm);
  closed(recipe.layout_seed, ["value"], "layout_seed");
  nonempty(
    (recipe.layout_seed as Record<string, unknown>).value,
    "layout_seed.value"
  );
  closed(recipe.density, ["value"], "density");
  enumeration(
    (recipe.density as Record<string, unknown>).value,
    ["compact", "comfortable", "spacious"],
    "density.value"
  );
  optionalClosed(recipe.orientation, ["value"], "orientation", (v) =>
    enumeration(
      v.value,
      ["top_to_bottom", "bottom_to_top", "left_to_right", "right_to_left"],
      "orientation.value"
    )
  );
  optionalClosed(
    recipe.viewport,
    ["width", "height", "device_scale"],
    "viewport",
    (v) => {
      positive(v.width, "viewport.width");
      positive(v.height, "viewport.height");
      positive(v.device_scale, "viewport.device_scale");
    }
  );
  const theme = closed(
    recipe.theme,
    ["theme_id", "theme_version", "specification_digest", "color_scheme"],
    "theme"
  );
  nonempty(theme.theme_id, "theme.theme_id");
  nonempty(theme.theme_version, "theme.theme_version");
  digest(theme.specification_digest, "theme.specification_digest");
  enumeration(theme.color_scheme, ["light", "dark"], "theme.color_scheme");
  const locale = closed(recipe.locale, ["language_tag"], "locale");
  nonempty(locale.language_tag, "locale.language_tag");
  const timezone = closed(recipe.timezone, ["iana_name"], "timezone");
  nonempty(timezone.iana_name, "timezone.iana_name");
  const fonts = closed(
    recipe.font_policy,
    ["families", "fallback", "required_digests"],
    "font_policy"
  );
  stringArray(fonts.families, "font_policy.families");
  enumeration(fonts.fallback, ["forbid"], "font_policy.fallback");
  digestArray(fonts.required_digests, "font_policy.required_digests");
  const assets = closed(
    recipe.asset_policy,
    ["mode", "required_digests"],
    "asset_policy"
  );
  enumeration(assets.mode, ["resolved_only", "embed"], "asset_policy.mode");
  digestArray(assets.required_digests, "asset_policy.required_digests");
  if (recipe.rasterizer_profile !== undefined)
    profile(recipe.rasterizer_profile, "rasterizer_profile");
  optionalClosed(
    recipe.interaction_policy,
    ["selection", "pan", "zoom"],
    "interaction_policy",
    (v) => {
      bool(v.selection, "interaction_policy.selection");
      bool(v.pan, "interaction_policy.pan");
      bool(v.zoom, "interaction_policy.zoom");
    }
  );
}

export function assertRenderData(value: unknown): asserts value is RenderData {
  const data = object(value, "render.data_invalid", "RenderData");
  const kind = String(data.kind);
  if (!shape(kind) || data.shape !== kind)
    invalid("render.data_invalid", "RenderData kind and shape must match");
  const shapeFields = Object.keys(primitiveFields[kind as RenderShape]);
  const base = [
    "kind",
    "render_data_schema_version",
    "renderer_profile",
    "view_data_hash",
    "render_input_hash",
    "shape",
    "layout_seed",
    "locale",
    "timezone",
    "bounds",
    "source_bindings",
    "resolved_asset_digests",
    "resolved_font_digests",
    "diagnostics",
  ];
  exact(data, [...base, ...shapeFields], "render.data_invalid");
  if (data.render_data_schema_version !== 1)
    invalid("render.data_invalid", "unsupported RenderData schema version");
  profile(data.renderer_profile, "renderer_profile", "render.data_invalid");
  digest(data.view_data_hash, "view_data_hash", "render.data_invalid");
  digest(data.render_input_hash, "render_input_hash", "render.data_invalid");
  nonempty(data.layout_seed, "layout_seed", "render.data_invalid");
  nonempty(data.locale, "locale", "render.data_invalid");
  nonempty(data.timezone, "timezone", "render.data_invalid");
  bounds(data.bounds, "bounds");
  digestArray(
    data.resolved_asset_digests,
    "resolved_asset_digests",
    "render.data_invalid"
  );
  digestArray(
    data.resolved_font_digests,
    "resolved_font_digests",
    "render.data_invalid"
  );
  if (!Array.isArray(data.source_bindings))
    invalid("render.data_invalid", "source_bindings must be an array");
  const bindings = new Map<string, RenderSourceBinding>();
  for (const raw of data.source_bindings) {
    const binding = closed(
      raw,
      ["render_key", "viewdata_key", "source_refs"],
      "source_binding",
      "render.data_invalid"
    );
    nonempty(
      binding.render_key,
      "source_binding.render_key",
      "render.data_invalid"
    );
    if (bindings.has(String(binding.render_key)))
      invalid("render.data_invalid", "duplicate render_key binding");
    if (binding.viewdata_key === undefined && binding.source_refs === undefined)
      invalid(
        "render.data_invalid",
        "a binding must trace to ViewData or source refs"
      );
    if (
      binding.viewdata_key !== undefined &&
      !isViewDataItemKey(binding.viewdata_key)
    )
      invalid(
        "render.data_invalid",
        "source_binding.viewdata_key is not canonical ViewData identity"
      );
    if (
      binding.source_refs !== undefined &&
      !isViewDataSourceRefs(binding.source_refs)
    )
      invalid(
        "render.data_invalid",
        "source_binding.source_refs is not canonical ViewData source refs"
      );
    bindings.set(String(binding.render_key), raw as RenderSourceBinding);
  }
  const collections: Record<string, Map<string, Record<string, unknown>>> = {};
  const primitiveKeys = new Set<string>();
  for (const field of shapeFields) {
    const occurrences = data[field];
    if (!Array.isArray(occurrences))
      invalid("render.data_invalid", `${field} must be an array`);
    const collection = new Map<string, Record<string, unknown>>();
    collections[field] = collection;
    for (const raw of occurrences) {
      const occurrence = validatePrimitive(
        raw,
        field,
        primitiveFields[kind as RenderShape][field] ?? []
      );
      const renderKey = String(occurrence.render_key);
      if (primitiveKeys.has(renderKey))
        invalid(
          "render.data_invalid",
          `duplicate visual render_key ${renderKey}`
        );
      primitiveKeys.add(renderKey);
      collection.set(renderKey, occurrence);
      if (!bindings.has(renderKey))
        invalid(
          "render.data_invalid",
          `untraceable visual occurrence ${renderKey}`
        );
    }
  }
  for (const renderKey of bindings.keys())
    if (!primitiveKeys.has(renderKey))
      invalid(
        "render.data_invalid",
        `source binding ${renderKey} has no visual primitive`
      );
  for (const [field, collection] of Object.entries(collections)) {
    for (const primitive of collection.values())
      validatePrimitiveReferences(
        kind as RenderShape,
        field,
        primitive,
        collections
      );
  }
  if (!Array.isArray(data.diagnostics))
    invalid("render.data_invalid", "diagnostics must be an array");
  for (const raw of data.diagnostics) {
    const diagnostic = closed(
      raw,
      [
        "code",
        "severity",
        "message_key",
        "arguments",
        "protocol_version",
        "owner_address",
        "subject_address",
      ],
      "diagnostic",
      "render.data_invalid"
    );
    nonempty(diagnostic.code, "diagnostic.code", "render.data_invalid");
    nonempty(
      diagnostic.message_key,
      "diagnostic.message_key",
      "render.data_invalid"
    );
    enumeration(
      diagnostic.severity,
      ["error", "warning", "information", "hint"],
      "diagnostic.severity",
      "render.data_invalid"
    );
    object(diagnostic.arguments, "render.data_invalid", "diagnostic.arguments");
    if (!isJsonValue(diagnostic.arguments))
      invalid(
        "render.data_invalid",
        "diagnostic.arguments must be recursive canonical JsonValue"
      );
    if (
      diagnostic.protocol_version !== undefined &&
      diagnostic.protocol_version !== 1
    )
      invalid("render.data_invalid", "diagnostic.protocol_version is invalid");
    for (const key of ["owner_address", "subject_address"] as const)
      if (diagnostic[key] !== undefined && !isStableAddress(diagnostic[key]))
        invalid("render.data_invalid", `diagnostic.${key} is invalid`);
  }
}

const primitiveFields: Readonly<
  Record<RenderShape, Readonly<Record<string, readonly string[]>>>
> = {
  diagram: {
    containers: ["render_key", "bounds", "child_keys"],
    occurrences: ["render_key", "bounds", "port_keys", "label_key"],
    ports: ["render_key", "position", "occurrence_key"],
    edge_paths: ["render_key", "points", "from_port_key", "to_port_key"],
    labels: ["render_key", "bounds", "text", "anchor"],
    overlays: ["render_key", "bounds", "target", "display_identity"],
    badges: ["render_key", "bounds", "target", "label"],
    support_geometry: [
      "render_key",
      "bounds",
      "support_kind",
      "display_identity",
    ],
  },
  table: {
    columns: [
      "render_key",
      "x",
      "width",
      "frozen",
      "id",
      "label",
      "value_type",
      "header_bounds",
    ],
    rows: ["render_key", "y", "height", "frozen"],
    cells: ["render_key", "bounds", "row_key", "column_key", "text"],
  },
  matrix: {
    row_axes: ["render_key", "bounds", "label"],
    column_axes: ["render_key", "bounds", "label"],
    cells: ["render_key", "bounds", "row_axis_key", "column_axis_key", "text"],
    totals: ["render_key", "bounds", "axis_key", "text"],
  },
  tree: {
    occurrences: ["render_key", "bounds", "parent_key", "depth", "label"],
    duplicate_refs: [
      "render_key",
      "points",
      "from_occurrence_key",
      "to_occurrence_key",
    ],
    cycle_refs: [
      "render_key",
      "points",
      "from_occurrence_key",
      "to_occurrence_key",
    ],
  },
  flow: {
    lanes: ["render_key", "bounds", "label"],
    steps: ["render_key", "bounds", "lane_key", "label"],
    branches: ["render_key", "position", "step_keys"],
    joins: ["render_key", "position", "step_keys"],
    connectors: [
      "render_key",
      "points",
      "from_step_key",
      "to_step_key",
      "connector_kind",
      "label",
    ],
    cycle_refs: [
      "render_key",
      "points",
      "from_step_key",
      "to_step_key",
      "connector_kind",
      "label",
    ],
  },
  context: {
    groups: ["render_key", "bounds", "label"],
    facts: ["render_key", "bounds", "group_key", "text"],
    relation_summaries: ["render_key", "bounds", "group_key", "text"],
  },
  diff: {
    before: ["render_key", "bounds", "label"],
    after: ["render_key", "bounds", "label"],
    changes: ["render_key", "bounds", "change_kind", "before_key", "after_key"],
    fields: [
      "render_key",
      "bounds",
      "change_key",
      "field_path",
      "before_text",
      "after_text",
    ],
  },
};

const optionalPrimitiveFields = new Set([
  "label_key",
  "parent_key",
  "before_key",
  "after_key",
  "before_text",
  "after_text",
]);
function validatePrimitive(
  value: unknown,
  name: string,
  keys: readonly string[]
): Record<string, unknown> {
  const primitive = closed(value, keys, name, "render.data_invalid");
  for (const key of keys)
    if (!optionalPrimitiveFields.has(key) && !Object.hasOwn(primitive, key))
      invalid("render.data_invalid", `${name}.${key} is required`);
  nonempty(primitive.render_key, `${name}.render_key`, "render.data_invalid");
  if (primitive.bounds !== undefined)
    bounds(primitive.bounds, `${name}.bounds`);
  if (primitive.header_bounds !== undefined)
    bounds(primitive.header_bounds, `${name}.header_bounds`);
  if (primitive.position !== undefined)
    point(primitive.position, `${name}.position`);
  if (primitive.points !== undefined) {
    if (!Array.isArray(primitive.points) || primitive.points.length < 2)
      invalid(
        "render.data_invalid",
        `${name}.points must contain at least two points`
      );
    for (const [index, value] of primitive.points.entries())
      point(value, `${name}.points[${index}]`);
  }
  for (const key of keys.filter(
    (key) => key.endsWith("_key") || key === "field_path"
  )) {
    if (primitive[key] !== undefined)
      nonempty(primitive[key], `${name}.${key}`, "render.data_invalid");
  }
  for (const key of keys.filter(
    (key) =>
      key.endsWith("_text") ||
      key === "text" ||
      key === "label" ||
      key === "id" ||
      key === "display_identity"
  )) {
    if (primitive[key] !== undefined && typeof primitive[key] !== "string")
      invalid("render.data_invalid", `${name}.${key} must be a string`);
  }
  for (const key of keys.filter((key) => key.endsWith("_keys")))
    stringArray(primitive[key], `${name}.${key}`, "render.data_invalid");
  for (const key of ["x", "y"])
    if (keys.includes(key)) finite(primitive[key], `${name}.${key}`);
  for (const key of ["width", "height"])
    if (keys.includes(key)) nonnegative(primitive[key], `${name}.${key}`);
  if (keys.includes("frozen"))
    bool(primitive.frozen, `${name}.frozen`, "render.data_invalid");
  if (
    keys.includes("value_type") &&
    !isTableViewValueType(primitive.value_type)
  )
    invalid("render.data_invalid", `${name}.value_type is invalid`);
  if (keys.includes("support_kind"))
    enumeration(
      primitive.support_kind,
      ["hidden_entity", "hidden_relation", "source_only"],
      `${name}.support_kind`,
      "render.data_invalid"
    );
  if (
    keys.includes("connector_kind") &&
    !isFlowConnectorKind(primitive.connector_kind)
  )
    invalid("render.data_invalid", `${name}.connector_kind is invalid`);
  if (
    keys.includes("depth") &&
    (!Number.isSafeInteger(primitive.depth) || (primitive.depth as number) < 0)
  )
    invalid(
      "render.data_invalid",
      `${name}.depth must be a nonnegative safe integer`
    );
  if (keys.includes("change_kind")) {
    enumeration(
      primitive.change_kind,
      ["added", "removed", "updated", "moved", "moved_updated"],
      `${name}.change_kind`,
      "render.data_invalid"
    );
    const before = Object.hasOwn(primitive, "before_key");
    const after = Object.hasOwn(primitive, "after_key");
    if (primitive.change_kind === "added" && (before || !after))
      invalid("render.data_invalid", `${name}.added requires after only`);
    if (primitive.change_kind === "removed" && (!before || after))
      invalid("render.data_invalid", `${name}.removed requires before only`);
    if (
      ["updated", "moved", "moved_updated"].includes(
        String(primitive.change_kind)
      ) &&
      (!before || !after)
    )
      invalid(
        "render.data_invalid",
        `${name}.${String(primitive.change_kind)} requires before and after`
      );
  }
  return primitive;
}

function point(value: unknown, name: string): void {
  const v = closed(value, ["x", "y"], name, "render.data_invalid");
  finite(v.x, `${name}.x`);
  finite(v.y, `${name}.y`);
}

function validatePrimitiveReferences(
  shape: RenderShape,
  field: string,
  primitive: Record<string, unknown>,
  collections: Record<string, Map<string, Record<string, unknown>>>
): void {
  const one = (key: string, target: string): void => {
    if (
      primitive[key] !== undefined &&
      !collections[target]?.has(String(primitive[key]))
    )
      invalid(
        "render.data_invalid",
        `${field}.${key} does not reference ${target}`
      );
  };
  const many = (key: string, target: string): void => {
    for (const value of (primitive[key] as readonly string[] | undefined) ?? [])
      if (!collections[target]?.has(value))
        invalid(
          "render.data_invalid",
          `${field}.${key} does not reference ${target}`
        );
  };
  if (shape === "diagram") {
    if (field === "containers") many("child_keys", "occurrences");
    if (field === "occurrences") {
      many("port_keys", "ports");
      one("label_key", "labels");
    }
    if (field === "ports") one("occurrence_key", "occurrences");
    if (field === "edge_paths") {
      one("from_port_key", "ports");
      one("to_port_key", "ports");
    }
    if (field === "labels") {
      const [collection, key] = diagramReference(
        primitive.anchor,
        `${field}.anchor`,
        {
          occurrence: ["occurrences", "occurrence_key"],
          edge_path: ["edge_paths", "edge_path_key"],
        }
      );
      if (!collections[collection]?.has(key))
        invalid(
          "render.data_invalid",
          `${field}.anchor does not reference ${collection}`
        );
    }
    if (["overlays", "badges"].includes(field)) {
      const [collection, key] = diagramReference(
        primitive.target,
        `${field}.target`,
        {
          occurrence: ["occurrences", "occurrence_key"],
          container: ["containers", "container_key"],
        }
      );
      if (!collections[collection]?.has(key))
        invalid(
          "render.data_invalid",
          `${field}.target does not reference ${collection}`
        );
    }
  } else if (shape === "tree") {
    if (field === "occurrences") one("parent_key", "occurrences");
    if (field === "duplicate_refs" || field === "cycle_refs") {
      one("from_occurrence_key", "occurrences");
      one("to_occurrence_key", "occurrences");
    }
  } else if (shape === "flow") {
    if (field === "steps") one("lane_key", "lanes");
    if (field === "branches" || field === "joins") many("step_keys", "steps");
    if (field === "connectors" || field === "cycle_refs") {
      one("from_step_key", "steps");
      one("to_step_key", "steps");
    }
  } else if (shape === "table" && field === "cells") {
    one("row_key", "rows");
    one("column_key", "columns");
  } else if (shape === "matrix") {
    if (field === "cells") {
      one("row_axis_key", "row_axes");
      one("column_axis_key", "column_axes");
    }
    if (
      field === "totals" &&
      !collections.row_axes?.has(String(primitive.axis_key)) &&
      !collections.column_axes?.has(String(primitive.axis_key))
    )
      invalid(
        "render.data_invalid",
        "totals.axis_key does not reference an axis"
      );
  } else if (
    shape === "context" &&
    (field === "facts" || field === "relation_summaries")
  ) {
    one("group_key", "groups");
  } else if (shape === "diff") {
    if (field === "changes") {
      one("before_key", "before");
      one("after_key", "after");
    }
    if (field === "fields") one("change_key", "changes");
  }
}

function diagramReference(
  value: unknown,
  name: string,
  variants: Readonly<Record<string, readonly [string, string]>>
): readonly [string, string] {
  const reference = object(value, "render.data_invalid", name);
  const kind = typeof reference.kind === "string" ? reference.kind : "";
  const variant = variants[kind];
  if (variant === undefined)
    invalid("render.data_invalid", `${name}.kind is invalid`);
  exact(reference, ["kind", variant[1]], "render.data_invalid");
  nonempty(
    reference[variant[1]],
    `${name}.${variant[1]}`,
    "render.data_invalid"
  );
  return [variant[0], String(reference[variant[1]])];
}

export async function hashRenderInput(
  value: Readonly<{
    view_data_hash: Digest;
    recipe: RenderRecipe;
    asset_digests: readonly Digest[];
    font_digests: readonly Digest[];
  }>
): Promise<Digest> {
  const input = closed(
    value,
    ["view_data_hash", "recipe", "asset_digests", "font_digests"],
    "RenderInput"
  );
  assertRenderRecipe(input.recipe);
  digest(input.view_data_hash, "view_data_hash");
  digestArray(input.asset_digests, "asset_digests");
  digestArray(input.font_digests, "font_digests");
  const bytes = new TextEncoder().encode(
    `layerdraw-render-1\0render-input\0${canonical(input)}`
  );
  const hashed = new Uint8Array(await crypto.subtle.digest("SHA-256", bytes));
  return `sha256:${Array.from(hashed, (byte) =>
    byte.toString(16).padStart(2, "0")
  ).join("")}` as Digest;
}

function canonical(value: unknown): string {
  if (value === null || typeof value === "boolean" || typeof value === "string")
    return JSON.stringify(value);
  if (typeof value === "number") {
    if (!Number.isFinite(value))
      invalid("render.recipe_invalid", "non-finite number");
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
  const entries = Object.entries(value as Record<string, unknown>)
    .filter(([, item]) => item !== undefined)
    .sort(([left], [right]) => (left < right ? -1 : left > right ? 1 : 0));
  return `{${entries
    .map(([key, item]) => `${JSON.stringify(key)}:${canonical(item)}`)
    .join(",")}}`;
}

function algorithm(value: unknown): void {
  const v = object(value, "render.recipe_invalid", "layout_algorithm");
  const kind = String(v.kind);
  if (kind === "native") {
    exact(v, ["kind"], "render.recipe_invalid");
    return;
  }
  if (kind === "grid") {
    exact(v, ["kind", "columns"], "render.recipe_invalid");
    positive(v.columns, "layout_algorithm.columns");
    return;
  }
  if (kind === "radial") {
    exact(v, ["kind", "radius_step"], "render.recipe_invalid");
    positive(v.radius_step, "layout_algorithm.radius_step");
    return;
  }
  if (kind === "layered") {
    exact(
      v,
      ["kind", "crossing_reduction", "rank_separation"],
      "render.recipe_invalid"
    );
    enumeration(
      v.crossing_reduction,
      ["barycenter", "median"],
      "layout_algorithm.crossing_reduction"
    );
    positive(v.rank_separation, "layout_algorithm.rank_separation");
    return;
  }
  invalid("render.recipe_invalid", "unknown layout algorithm");
}
function profile(
  value: unknown,
  name: string,
  code: RenderContractError["code"] = "render.profile_incompatible"
): void {
  const v = closed(
    value,
    ["profile_id", "profile_version", "specification_digest"],
    name,
    code
  );
  if (typeof v.profile_id !== "string" || !profilePattern.test(v.profile_id))
    invalid(code, `${name}.profile_id is invalid`);
  if (
    typeof v.profile_version !== "string" ||
    !versionPattern.test(v.profile_version)
  )
    invalid(code, `${name}.profile_version is invalid`);
  digest(v.specification_digest, `${name}.specification_digest`, code);
}
function bounds(value: unknown, name: string): void {
  const v = closed(
    value,
    ["x", "y", "width", "height"],
    name,
    "render.data_invalid"
  );
  finite(v.x, `${name}.x`);
  finite(v.y, `${name}.y`);
  nonnegative(v.width, `${name}.width`);
  nonnegative(v.height, `${name}.height`);
}
function shape(value: string): value is RenderShape {
  return [
    "diagram",
    "table",
    "matrix",
    "tree",
    "flow",
    "context",
    "diff",
  ].includes(value);
}
function object(
  value: unknown,
  code: RenderContractError["code"],
  name: string
): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value))
    invalid(code, `${name} must be an object`);
  return value as Record<string, unknown>;
}
function exact(
  value: Record<string, unknown>,
  keys: readonly string[],
  code: RenderContractError["code"]
): void {
  const allowed = new Set(keys);
  for (const key of Object.keys(value))
    if (!allowed.has(key)) invalid(code, `unknown option ${key}`);
}
function closed(
  value: unknown,
  keys: readonly string[],
  name: string,
  code: RenderContractError["code"] = "render.recipe_invalid"
): Record<string, unknown> {
  const v = object(value, code, name);
  exact(v, keys, code);
  return v;
}
function optionalClosed(
  value: unknown,
  keys: readonly string[],
  name: string,
  check: (value: Record<string, unknown>) => void
): void {
  if (value === undefined) return;
  check(closed(value, keys, name));
}
function invalid(code: RenderContractError["code"], message: string): never {
  throw new RenderContractError(code, message);
}
function nonempty(
  value: unknown,
  name: string,
  code: RenderContractError["code"] = "render.recipe_invalid"
): void {
  if (typeof value !== "string" || value.length === 0)
    invalid(code, `${name} must be nonempty`);
}
function enumeration(
  value: unknown,
  values: readonly string[],
  name: string,
  code: RenderContractError["code"] = "render.recipe_invalid"
): void {
  if (typeof value !== "string" || !values.includes(value))
    invalid(code, `${name} is invalid`);
}
function bool(
  value: unknown,
  name: string,
  code: RenderContractError["code"] = "render.recipe_invalid"
): void {
  if (typeof value !== "boolean") invalid(code, `${name} must be boolean`);
}
function finite(value: unknown, name: string): void {
  if (typeof value !== "number" || !Number.isFinite(value))
    invalid("render.data_invalid", `${name} must be finite`);
}
function positive(value: unknown, name: string): void {
  if (typeof value !== "number" || !Number.isFinite(value) || value <= 0)
    invalid("render.recipe_invalid", `${name} must be positive`);
}
function nonnegative(value: unknown, name: string): void {
  if (typeof value !== "number" || !Number.isFinite(value) || value < 0)
    invalid("render.data_invalid", `${name} must be nonnegative`);
}
function digest(
  value: unknown,
  name: string,
  code: RenderContractError["code"] = "render.recipe_invalid"
): void {
  if (typeof value !== "string" || !digestPattern.test(value))
    invalid(code, `${name} must be a SHA-256 digest`);
}
function stringArray(
  value: unknown,
  name: string,
  code: RenderContractError["code"] = "render.recipe_invalid"
): void {
  if (
    !Array.isArray(value) ||
    value.some((item) => typeof item !== "string" || item.length === 0)
  )
    invalid(code, `${name} must be a string array`);
  if (new Set(value).size !== value.length)
    invalid(code, `${name} must not contain duplicates`);
}
function digestArray(
  value: unknown,
  name: string,
  code: RenderContractError["code"] = "render.recipe_invalid"
): void {
  if (!Array.isArray(value)) invalid(code, `${name} must be an array`);
  for (const item of value) digest(item, name, code);
  for (let index = 1; index < value.length; index++)
    if (String(value[index - 1]) >= String(value[index]))
      invalid(code, `${name} must be sorted and unique`);
}

export * from "./materialize.js";
export {
  DEFAULT_VISUAL_SURFACE_LIMITS,
  renderDiagramVisualSurface,
  renderFlowVisualSurface,
  renderTreeVisualSurface,
  VISUAL_SURFACE_HARD_LIMITS,
  VisualSurfaceError,
  type GraphVisualShape,
  type GraphVisualSurface,
  type VisualHitTarget,
  type VisualInteractionMetadata,
  type VisualSurfaceLimits,
  type VisualSurfaceOptions,
} from "./visual.js";
