// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {
  assertRenderData,
  type DiagramRenderData,
  type FlowConnectorPrimitive,
  type FlowRenderData,
  type RenderBounds,
  type RenderPoint,
  type RenderSourceBinding,
  type StableRenderDiagnostic,
  type TreeRenderData,
} from "./index.js";

export type GraphVisualShape = "diagram" | "tree" | "flow";

export interface VisualSurfaceLimits {
  readonly max_primitives: number;
  readonly max_route_points: number;
  readonly max_text_scalars: number;
  readonly max_svg_bytes: number;
  readonly max_extent: number;
}

export interface VisualSurfaceOptions {
  readonly viewport?: RenderBounds;
  readonly accessibility_label?: string;
  readonly label_overflow?: Readonly<{
    mode: "clip" | "ellipsis";
    max_scalars: number;
  }>;
  readonly limits?: VisualSurfaceLimits;
}

export interface VisualHitTarget {
  readonly hit_target_id: string;
  readonly render_key: string;
  readonly primitive_kind: string;
  readonly accessibility_label: string;
  readonly focus_order: number;
  readonly source_binding: RenderSourceBinding;
}

export interface VisualInteractionMetadata {
  readonly hit_targets: readonly VisualHitTarget[];
  readonly focus_order: readonly string[];
  readonly source_bindings: readonly RenderSourceBinding[];
}

export interface GraphVisualSurface<
  Shape extends GraphVisualShape = GraphVisualShape,
> {
  readonly visual_surface_schema_version: 1;
  readonly kind: Shape;
  readonly svg: string;
  readonly viewport: RenderBounds;
  readonly empty: boolean;
  readonly interaction: VisualInteractionMetadata;
  readonly diagnostics: readonly StableRenderDiagnostic[];
}

export const VISUAL_SURFACE_HARD_LIMITS: VisualSurfaceLimits = Object.freeze({
  max_primitives: 100_000,
  max_route_points: 400_000,
  max_text_scalars: 4_000_000,
  max_svg_bytes: 64 * 1024 * 1024,
  max_extent: 10_000_000,
});

export const DEFAULT_VISUAL_SURFACE_LIMITS: VisualSurfaceLimits = Object.freeze({
  max_primitives: 50_000,
  max_route_points: 200_000,
  max_text_scalars: 1_000_000,
  max_svg_bytes: 32 * 1024 * 1024,
  max_extent: 1_000_000,
});

export class VisualSurfaceError extends Error {
  constructor(
    readonly code:
      | "render.visual_data_invalid"
      | "render.visual_options_invalid"
      | "render.visual_resource_limit",
    message: string
  ) {
    super(message);
  }
}

type Item = Readonly<{
  render_key: string;
  primitive_kind: string;
  accessibility_label: string;
  emitted_strings: readonly string[];
  route_points: number;
  body: (context: ItemContext) => string;
}>;

type ItemContext = Readonly<{
  target: VisualHitTarget;
  svg_id: string;
  arrow_id: string;
  label: (value: string) => string;
}>;

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

const emptyBounds: RenderBounds = { x: 0, y: 0, width: 320, height: 180 };
const defaultOverflow = Object.freeze({
  mode: "ellipsis" as const,
  max_scalars: 256,
});

export function renderDiagramVisualSurface(
  data: DiagramRenderData,
  options?: VisualSurfaceOptions
): GraphVisualSurface<"diagram"> {
  validateShape(data, "diagram");
  const labelByOccurrence = new Map<string, string>();
  const labelByEdge = new Map<string, string>();
  for (const label of data.labels) {
    if (label.anchor.kind === "occurrence")
      labelByOccurrence.set(label.anchor.occurrence_key, label.text);
    else labelByEdge.set(label.anchor.edge_path_key, label.text);
  }
  const occurrenceLabel = (key: string): string =>
    labelByOccurrence.get(key) ?? key;
  const items: Item[] = [];
  for (const primitive of data.containers)
    items.push({
      render_key: primitive.render_key,
      primitive_kind: "diagram.container",
      accessibility_label: `Container ${primitive.render_key}`,
      emitted_strings: [primitive.child_keys.join(" ")],
      route_points: 0,
      body: () =>
        rect(
          primitive.bounds,
          "ld-container",
          ` data-child-keys="${xml(primitive.child_keys.join(" "))}"`
        ),
    });
  for (const primitive of data.occurrences)
    items.push({
      render_key: primitive.render_key,
      primitive_kind: "diagram.occurrence",
      accessibility_label: `Occurrence ${occurrenceLabel(primitive.render_key)}`,
      emitted_strings: [
        primitive.port_keys.join(" "),
        ...(primitive.label_key === undefined ? [] : [primitive.label_key]),
      ],
      route_points: 0,
      body: () =>
        rect(
          primitive.bounds,
          "ld-occurrence",
          ` data-port-keys="${xml(primitive.port_keys.join(" "))}"${
            primitive.label_key === undefined
              ? ""
              : ` data-label-key="${xml(primitive.label_key)}"`
          }`
        ),
    });
  for (const primitive of data.ports)
    items.push({
      render_key: primitive.render_key,
      primitive_kind: "diagram.port",
      accessibility_label: `Port for ${occurrenceLabel(primitive.occurrence_key)}`,
      emitted_strings: [primitive.occurrence_key],
      route_points: 0,
      body: () =>
        `<circle class="ld-port" cx="${number(primitive.position.x)}" cy="${number(primitive.position.y)}" r="3" data-occurrence-key="${xml(primitive.occurrence_key)}"/>`,
    });
  for (const primitive of data.edge_paths)
    items.push({
      render_key: primitive.render_key,
      primitive_kind: "diagram.edge",
      accessibility_label: `Edge ${labelByEdge.get(primitive.render_key) ?? primitive.render_key}`,
      emitted_strings: [primitive.from_port_key, primitive.to_port_key],
      route_points: primitive.points.length,
      body: ({ arrow_id }) =>
        polyline(
          primitive.points,
          "ld-edge",
          ` marker-end="url(#${arrow_id})" data-from-port-key="${xml(primitive.from_port_key)}" data-to-port-key="${xml(primitive.to_port_key)}"`
        ),
    });
  for (const primitive of data.labels)
    items.push({
      render_key: primitive.render_key,
      primitive_kind: "diagram.label",
      accessibility_label: `Label ${primitive.text}`,
      emitted_strings: [
        primitive.text,
        primitive.anchor.kind,
        primitive.anchor.kind === "occurrence"
          ? primitive.anchor.occurrence_key
          : primitive.anchor.edge_path_key,
      ],
      route_points: 0,
      body: ({ svg_id, label }) =>
        `<g data-anchor-kind="${primitive.anchor.kind}" data-anchor-key="${xml(
          primitive.anchor.kind === "occurrence"
            ? primitive.anchor.occurrence_key
            : primitive.anchor.edge_path_key
        )}">${clippedText(primitive.bounds, primitive.text, svg_id, label)}</g>`,
    });
  for (const primitive of data.overlays)
    items.push({
      render_key: primitive.render_key,
      primitive_kind: "diagram.overlay",
      accessibility_label: `Overlay ${primitive.display_identity}`,
      emitted_strings: [
        primitive.display_identity,
        primitive.target.kind,
        primitive.target.kind === "occurrence"
          ? primitive.target.occurrence_key
          : primitive.target.container_key,
      ],
      route_points: 0,
      body: ({ svg_id, label }) =>
        `${rect(
          primitive.bounds,
          "ld-overlay",
          ` data-target-kind="${primitive.target.kind}" data-target-key="${xml(
            primitive.target.kind === "occurrence"
              ? primitive.target.occurrence_key
              : primitive.target.container_key
          )}"`
        )}${clippedText(primitive.bounds, primitive.display_identity, `${svg_id}-text`, label)}`,
    });
  for (const primitive of data.badges)
    items.push({
      render_key: primitive.render_key,
      primitive_kind: "diagram.badge",
      accessibility_label: `Badge ${primitive.label}`,
      emitted_strings: [
        primitive.label,
        primitive.target.kind,
        primitive.target.kind === "occurrence"
          ? primitive.target.occurrence_key
          : primitive.target.container_key,
      ],
      route_points: 0,
      body: ({ svg_id, label }) =>
        `${rect(
          primitive.bounds,
          "ld-badge",
          ` data-target-kind="${primitive.target.kind}" data-target-key="${xml(
            primitive.target.kind === "occurrence"
              ? primitive.target.occurrence_key
              : primitive.target.container_key
          )}"`
        )}${clippedText(primitive.bounds, primitive.label, `${svg_id}-text`, label)}`,
    });
  for (const primitive of data.support_geometry)
    items.push({
      render_key: primitive.render_key,
      primitive_kind: `diagram.support.${primitive.support_kind}`,
      accessibility_label: `Hidden support ${primitive.support_kind}: ${primitive.display_identity}`,
      emitted_strings: [primitive.support_kind, primitive.display_identity],
      route_points: 0,
      body: ({ svg_id, label }) =>
        `${rect(primitive.bounds, "ld-support", ` data-support-kind="${primitive.support_kind}"`)}${clippedText(primitive.bounds, primitive.display_identity, `${svg_id}-text`, label)}`,
    });
  return buildSurface(data, items, options);
}

export function renderTreeVisualSurface(
  data: TreeRenderData,
  options?: VisualSurfaceOptions
): GraphVisualSurface<"tree"> {
  validateShape(data, "tree");
  const labels = new Map(data.occurrences.map((item) => [item.render_key, item.label]));
  const items: Item[] = [];
  for (const primitive of data.occurrences)
    items.push({
      render_key: primitive.render_key,
      primitive_kind: primitive.parent_key === undefined ? "tree.root" : "tree.occurrence",
      accessibility_label: `${primitive.parent_key === undefined ? "Root" : "Tree occurrence"} ${primitive.label}, depth ${primitive.depth}`,
      emitted_strings: [
        primitive.label,
        String(primitive.depth),
        ...(primitive.parent_key === undefined ? [] : [primitive.parent_key]),
      ],
      route_points: 0,
      body: ({ svg_id, label }) =>
        `${rect(primitive.bounds, primitive.parent_key === undefined ? "ld-tree-root" : "ld-tree-occurrence", ` data-depth="${primitive.depth}"${primitive.parent_key === undefined ? "" : ` data-parent-key="${xml(primitive.parent_key)}"`}`)}${clippedText(primitive.bounds, primitive.label, `${svg_id}-text`, label)}`,
    });
  for (const [collection, kind] of [
    [data.duplicate_refs, "duplicate"],
    [data.cycle_refs, "cycle"],
  ] as const)
    for (const primitive of collection)
      items.push({
        render_key: primitive.render_key,
        primitive_kind: `tree.${kind}_reference`,
        accessibility_label: `${kind === "duplicate" ? "Duplicate" : "Cycle"} reference from ${labels.get(primitive.from_occurrence_key) ?? primitive.from_occurrence_key} to ${labels.get(primitive.to_occurrence_key) ?? primitive.to_occurrence_key}`,
        emitted_strings: [
          kind,
          primitive.from_occurrence_key,
          primitive.to_occurrence_key,
        ],
        route_points: primitive.points.length,
        body: ({ arrow_id }) =>
          `${polyline(
            primitive.points,
            `ld-tree-${kind}`,
            ` marker-end="url(#${arrow_id})" data-from-occurrence-key="${xml(
              primitive.from_occurrence_key
            )}" data-to-occurrence-key="${xml(primitive.to_occurrence_key)}"`
          )}<text class="ld-reference-label" x="${number(primitive.points[0]!.x)}" y="${number(primitive.points[0]!.y)}" dominant-baseline="hanging">${kind}</text>`,
      });
  return buildSurface(data, items, options);
}

export function renderFlowVisualSurface(
  data: FlowRenderData,
  options?: VisualSurfaceOptions
): GraphVisualSurface<"flow"> {
  validateShape(data, "flow");
  const laneLabels = new Map(data.lanes.map((item) => [item.render_key, item.label]));
  const stepLabels = new Map(data.steps.map((item) => [item.render_key, item.label]));
  const items: Item[] = [];
  for (const primitive of data.lanes)
    items.push({
      render_key: primitive.render_key,
      primitive_kind: "flow.lane",
      accessibility_label: `Lane ${primitive.label}`,
      emitted_strings: [primitive.label],
      route_points: 0,
      body: ({ svg_id, label }) =>
        `${rect(primitive.bounds, "ld-lane")}${clippedText(primitive.bounds, primitive.label, `${svg_id}-text`, label)}`,
    });
  for (const primitive of data.steps)
    items.push({
      render_key: primitive.render_key,
      primitive_kind: "flow.step",
      accessibility_label: `Step ${primitive.label} in lane ${laneLabels.get(primitive.lane_key) ?? primitive.lane_key}`,
      emitted_strings: [primitive.label, primitive.lane_key],
      route_points: 0,
      body: ({ svg_id, label }) =>
        `${rect(primitive.bounds, "ld-step", ` data-lane-key="${xml(primitive.lane_key)}"`)}${clippedText(primitive.bounds, primitive.label, `${svg_id}-text`, label)}`,
    });
  for (const [collection, kind] of [
    [data.branches, "branch"],
    [data.joins, "join"],
  ] as const)
    for (const primitive of collection)
      items.push({
        render_key: primitive.render_key,
        primitive_kind: `flow.${kind}`,
        accessibility_label: `${kind === "branch" ? "Branch" : "Join"} for ${primitive.step_keys.map((key) => stepLabels.get(key) ?? key).join(", ")}`,
        emitted_strings: [kind, primitive.step_keys.join(" ")],
        route_points: 0,
        body: () =>
          `<circle class="ld-${kind}" cx="${number(primitive.position.x)}" cy="${number(primitive.position.y)}" r="5" data-step-keys="${xml(primitive.step_keys.join(" "))}"/>`,
      });
  for (const primitive of data.connectors)
    items.push(flowConnectorItem(primitive, false, stepLabels));
  for (const primitive of data.cycle_refs)
    items.push(flowConnectorItem(primitive, true, stepLabels));
  return buildSurface(data, items, options);
}

function flowConnectorItem(
  primitive: FlowConnectorPrimitive,
  cycle: boolean,
  stepLabels: ReadonlyMap<string, string>
): Item {
  const from = stepLabels.get(primitive.from_step_key) ?? primitive.from_step_key;
  const to = stepLabels.get(primitive.to_step_key) ?? primitive.to_step_key;
  const label = primitive.label.length === 0 ? "" : `, ${primitive.label}`;
  return {
    render_key: primitive.render_key,
    primitive_kind: cycle ? "flow.cycle_reference" : `flow.connector.${primitive.connector_kind}`,
    accessibility_label: `${cycle ? "Cycle reference" : "Connector"} ${primitive.connector_kind} from ${from} to ${to}${label}`,
    emitted_strings: [
      primitive.label,
      primitive.connector_kind,
      primitive.from_step_key,
      primitive.to_step_key,
    ],
    route_points: primitive.points.length,
    body: ({ svg_id, arrow_id, label: overflow }) => {
      const text = overflow(primitive.label);
      const pathId = `${svg_id}-path`;
      const path = `<path id="${pathId}" class="${cycle ? "ld-flow-cycle" : `ld-flow-connector ld-flow-${primitive.connector_kind}`}" d="${pathData(primitive.points)}" fill="none" marker-end="url(#${arrow_id})" data-connector-kind="${primitive.connector_kind}" data-from-step-key="${xml(primitive.from_step_key)}" data-to-step-key="${xml(primitive.to_step_key)}"/>`;
      if (text.length === 0) return path;
      return `${path}<text class="ld-connector-label"><textPath href="#${pathId}" startOffset="50%" text-anchor="middle">${xml(text)}</textPath></text><title>${xml(primitive.label)}</title>`;
    },
  };
}

function buildSurface<Shape extends GraphVisualShape>(
  data: (DiagramRenderData | TreeRenderData | FlowRenderData) &
    Readonly<{ kind: Shape }>,
  items: readonly Item[],
  options: VisualSurfaceOptions | undefined
): GraphVisualSurface<Shape> {
  const normalized = validateOptions(options);
  const limits = normalized.limits;
  if (items.length > limits.max_primitives)
    resource("max_primitives", limits.max_primitives);
  const routePoints = items.reduce((sum, item) => sum + item.route_points, 0);
  if (routePoints > limits.max_route_points)
    resource("max_route_points", limits.max_route_points);
  validateGraphGeometry(data, limits.max_extent);
  if (normalized.viewport !== undefined)
    validateExtent(normalized.viewport, limits.max_extent);

  const empty = items.length === 0;
  const viewport =
    normalized.viewport ?? (empty ? emptyBounds : positiveViewport(data.bounds));
  const accessibleLabel =
    normalized.accessibility_label ??
    (empty ? `Empty ${data.kind} visual` : `${capitalize(data.kind)} visual`);
  const emittedStringBytes = validateEmittedStrings(
    [
      accessibleLabel,
      ...items.flatMap((item) => [
        item.render_key,
        item.primitive_kind,
        item.accessibility_label,
        ...item.emitted_strings,
      ]),
    ],
    limits.max_text_scalars,
    limits.max_svg_bytes
  );
  const renderKeyIdBytes = items.reduce(
    (total, item) => total + utf8ByteCount(item.render_key) * 2,
    0
  );
  if (renderKeyIdBytes > limits.max_svg_bytes - emittedStringBytes)
    resource("max_svg_bytes", limits.max_svg_bytes);

  const bindingByKey = new Map(
    data.source_bindings.map((binding) => [binding.render_key, binding])
  );
  const hitTargets: VisualHitTarget[] = [];
  const overflow = normalized.label_overflow;
  const label = (value: string): string => {
    if (overflow.mode === "clip") return value;
    return truncateScalars(value, overflow.max_scalars);
  };
  const hashSuffix = data.render_input_hash.slice("sha256:".length, "sha256:".length + 16);
  const rootClipId = `ld-surface-clip-${hashSuffix}`;
  const arrowId = `ld-arrow-${hashSuffix}`;
  const writer = new BoundedSvgWriter(limits.max_svg_bytes);
  writer.push(
    `<svg xmlns="http://www.w3.org/2000/svg" role="img" aria-label="${xml(accessibleLabel)}" viewBox="${number(viewport.x)} ${number(viewport.y)} ${number(viewport.width)} ${number(viewport.height)}" width="${number(viewport.width)}" height="${number(viewport.height)}" preserveAspectRatio="xMidYMid meet" overflow="hidden"><title>${xml(accessibleLabel)}</title><defs><clipPath id="${rootClipId}"><rect x="${number(viewport.x)}" y="${number(viewport.y)}" width="${number(viewport.width)}" height="${number(viewport.height)}"/></clipPath><marker id="${arrowId}" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z"/></marker><style>${style}</style></defs><g clip-path="url(#${rootClipId})">`
  );
  for (const [focusOrder, item] of items.entries()) {
    const sourceBinding = bindingByKey.get(item.render_key);
    if (sourceBinding === undefined)
      throw new VisualSurfaceError(
        "render.visual_data_invalid",
        `missing source binding for ${item.render_key}`
      );
    const target: VisualHitTarget = {
      hit_target_id: `hit:${item.render_key}`,
      render_key: item.render_key,
      primitive_kind: item.primitive_kind,
      accessibility_label: item.accessibility_label,
      focus_order: focusOrder,
      source_binding: sourceBinding,
    };
    hitTargets.push(target);
    const svgId = `ld-${hex(item.render_key)}`;
    writer.push(
      `<g id="${svgId}" data-hit-target-id="${xml(target.hit_target_id)}" data-render-key="${xml(item.render_key)}" data-primitive-kind="${xml(item.primitive_kind)}" role="graphics-symbol" aria-label="${xml(item.accessibility_label)}" tabindex="0"><title>${xml(item.accessibility_label)}</title>${item.body({ target, svg_id: svgId, arrow_id: arrowId, label })}</g>`
    );
  }
  if (empty)
    writer.push(
      `<g class="ld-empty" role="status" aria-label="${xml(accessibleLabel)}"><rect class="ld-empty-background" x="${number(viewport.x)}" y="${number(viewport.y)}" width="${number(viewport.width)}" height="${number(viewport.height)}"/><text x="${number(viewport.x)}" y="${number(viewport.y)}" dominant-baseline="hanging">${xml(accessibleLabel)}</text></g>`
    );
  writer.push("</g></svg>");
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
    diagnostics: data.diagnostics,
  };
}

function validateShape(
  value: unknown,
  expected: GraphVisualShape
): asserts value is DiagramRenderData | TreeRenderData | FlowRenderData {
  try {
    assertRenderData(value);
  } catch (error) {
    const message = error instanceof Error ? error.message : "invalid RenderData";
    throw new VisualSurfaceError("render.visual_data_invalid", message);
  }
  if (value.kind !== expected)
    throw new VisualSurfaceError(
      "render.visual_data_invalid",
      `expected ${expected} RenderData`
    );
}

function validateOptions(options: VisualSurfaceOptions | undefined): Readonly<{
  viewport?: RenderBounds;
  accessibility_label?: string;
  label_overflow: Readonly<{ mode: "clip" | "ellipsis"; max_scalars: number }>;
  limits: VisualSurfaceLimits;
}> {
  if (options === undefined)
    return { label_overflow: defaultOverflow, limits: DEFAULT_VISUAL_SURFACE_LIMITS };
  if (!record(options) || !onlyKeys(options, ["viewport", "accessibility_label", "label_overflow", "limits"]))
    option("options must be a closed object");
  if (options.accessibility_label !== undefined &&
      (typeof options.accessibility_label !== "string" || options.accessibility_label.length === 0))
    option("accessibility_label must be nonempty");
  if (options.viewport !== undefined) validateBounds(options.viewport, true, "viewport");
  let labelOverflow = defaultOverflow;
  if (options.label_overflow !== undefined) {
    if (!record(options.label_overflow) ||
        !onlyKeys(options.label_overflow, ["mode", "max_scalars"]) ||
        !Object.hasOwn(options.label_overflow, "mode") ||
        !Object.hasOwn(options.label_overflow, "max_scalars") ||
        !["clip", "ellipsis"].includes(String(options.label_overflow.mode)) ||
        !safePositive(options.label_overflow.max_scalars))
      option("label_overflow is invalid");
    labelOverflow = options.label_overflow as typeof defaultOverflow;
  }
  let limits = DEFAULT_VISUAL_SURFACE_LIMITS;
  if (options.limits !== undefined) {
    const keys = ["max_primitives", "max_route_points", "max_text_scalars", "max_svg_bytes", "max_extent"] as const;
    const candidate: unknown = options.limits;
    if (!record(candidate) || !onlyKeys(candidate, keys) ||
        !keys.every((key) => Object.hasOwn(candidate, key) && safePositive(candidate[key])))
      option("limits must be a complete positive safe-integer object");
    for (const key of keys)
      if (Number(candidate[key]) > VISUAL_SURFACE_HARD_LIMITS[key])
        option(`${key} exceeds the package hard limit`);
    limits = candidate as unknown as VisualSurfaceLimits;
  }
  return {
    ...(options.viewport === undefined ? {} : { viewport: options.viewport }),
    ...(options.accessibility_label === undefined ? {} : { accessibility_label: options.accessibility_label }),
    label_overflow: labelOverflow,
    limits,
  };
}

function validateBounds(value: unknown, positive: boolean, name: string): asserts value is RenderBounds {
  if (!record(value) || !onlyKeys(value, ["x", "y", "width", "height"]) ||
      ![value.x, value.y, value.width, value.height].every(finite) ||
      (positive ? Number(value.width) <= 0 || Number(value.height) <= 0 : Number(value.width) < 0 || Number(value.height) < 0))
    option(`${name} is invalid`);
}

function validateExtent(bounds: RenderBounds, limit: number): void {
  validateBounds(bounds, false, "bounds");
  if ([bounds.x, bounds.y, bounds.width, bounds.height, bounds.x + bounds.width, bounds.y + bounds.height]
      .some((value) => Math.abs(value) > limit))
    resource("max_extent", limit);
}

function validateGraphGeometry(
  data: DiagramRenderData | TreeRenderData | FlowRenderData,
  limit: number
): void {
  validateExtent(data.bounds, limit);
  if (data.kind === "diagram") {
    for (const primitive of [
      ...data.containers,
      ...data.occurrences,
      ...data.labels,
      ...data.overlays,
      ...data.badges,
      ...data.support_geometry,
    ])
      validateExtent(primitive.bounds, limit);
    for (const primitive of data.ports)
      validatePointExtent(primitive.position, limit);
    for (const primitive of data.edge_paths)
      for (const point of primitive.points) validatePointExtent(point, limit);
    return;
  }
  if (data.kind === "tree") {
    for (const primitive of data.occurrences)
      validateExtent(primitive.bounds, limit);
    for (const primitive of [...data.duplicate_refs, ...data.cycle_refs])
      for (const point of primitive.points) validatePointExtent(point, limit);
    return;
  }
  for (const primitive of [...data.lanes, ...data.steps])
    validateExtent(primitive.bounds, limit);
  for (const primitive of [...data.branches, ...data.joins])
    validatePointExtent(primitive.position, limit);
  for (const primitive of [...data.connectors, ...data.cycle_refs])
    for (const point of primitive.points) validatePointExtent(point, limit);
}

function validatePointExtent(point: RenderPoint, limit: number): void {
  if (Math.abs(point.x) > limit || Math.abs(point.y) > limit)
    resource("max_extent", limit);
}

function validateEmittedStrings(
  values: readonly string[],
  scalarLimit: number,
  byteLimit: number
): number {
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
  return totalBytes;
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

function utf8ByteCount(value: string): number {
  let count = 0;
  for (const scalar of value) {
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
  return count;
}

function truncateScalars(value: string, limit: number): string {
  const prefix: string[] = [];
  let count = 0;
  for (const scalar of value) {
    count += 1;
    if (count <= limit) prefix.push(scalar);
    if (count > limit)
      return `${prefix.slice(0, Math.max(0, limit - 1)).join("")}…`;
  }
  return value;
}

function positiveViewport(bounds: RenderBounds): RenderBounds {
  return {
    x: bounds.x,
    y: bounds.y,
    width: bounds.width === 0 ? 1 : bounds.width,
    height: bounds.height === 0 ? 1 : bounds.height,
  };
}

function rect(bounds: RenderBounds, className: string, extra = ""): string {
  return `<rect class="${className}" x="${number(bounds.x)}" y="${number(bounds.y)}" width="${number(bounds.width)}" height="${number(bounds.height)}"${extra}/>`;
}

function clippedText(
  bounds: RenderBounds,
  value: string,
  id: string,
  overflow: (value: string) => string
): string {
  const text = overflow(value);
  return `<clipPath id="${id}-clip"><rect x="${number(bounds.x)}" y="${number(bounds.y)}" width="${number(bounds.width)}" height="${number(bounds.height)}"/></clipPath><text class="ld-label" x="${number(bounds.x)}" y="${number(bounds.y)}" dominant-baseline="hanging" clip-path="url(#${id}-clip)">${xml(text)}</text>${text === value ? "" : `<title>${xml(value)}</title>`}`;
}

function polyline(points: readonly RenderPoint[], className: string, extra = ""): string {
  return `<polyline class="${className}" points="${points.map((point) => `${number(point.x)},${number(point.y)}`).join(" ")}" fill="none"${extra}/>`;
}

function pathData(points: readonly RenderPoint[]): string {
  return points.map((point, index) => `${index === 0 ? "M" : "L"} ${number(point.x)} ${number(point.y)}`).join(" ");
}

function number(value: number): string {
  return Object.is(value, -0) ? "0" : String(value);
}

function xml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&apos;");
}

function hex(value: string): string {
  return Array.from(new TextEncoder().encode(value), (byte) =>
    byte.toString(16).padStart(2, "0")
  ).join("");
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

function resource(name: string, limit: number): never {
  throw new VisualSurfaceError(
    "render.visual_resource_limit",
    `${name} exceeds ${limit}`
  );
}

function capitalize(value: string): string {
  return `${value.slice(0, 1).toUpperCase()}${value.slice(1)}`;
}

const style = `.ld-container,.ld-occurrence,.ld-overlay,.ld-badge,.ld-support,.ld-tree-root,.ld-tree-occurrence,.ld-lane,.ld-step,.ld-empty-background{vector-effect:non-scaling-stroke}.ld-container{fill:#f8fafc;stroke:#64748b;stroke-width:1.5}.ld-occurrence,.ld-tree-occurrence,.ld-step{fill:#fff;stroke:#334155;stroke-width:1.5}.ld-tree-root{fill:#eff6ff;stroke:#1d4ed8;stroke-width:2}.ld-edge,.ld-flow-connector{stroke:#475569;stroke-width:1.5;vector-effect:non-scaling-stroke}.ld-port{fill:#475569;stroke:#fff;stroke-width:1;vector-effect:non-scaling-stroke}.ld-label,.ld-reference-label,.ld-connector-label,.ld-empty text{fill:#0f172a;font:12px sans-serif;pointer-events:none}.ld-overlay{fill:#dbeafe;stroke:#2563eb}.ld-badge{fill:#fef3c7;stroke:#d97706}.ld-support{fill:#f1f5f9;stroke:#64748b;stroke-dasharray:3 2}.ld-tree-duplicate{stroke:#d97706;stroke-width:2;stroke-dasharray:5 3;vector-effect:non-scaling-stroke}.ld-tree-cycle,.ld-flow-cycle{stroke:#dc2626;stroke-width:2;stroke-dasharray:8 3;vector-effect:non-scaling-stroke}.ld-lane{fill:#f8fafc;stroke:#cbd5e1}.ld-branch{fill:#fef3c7;stroke:#b45309}.ld-join{fill:#dcfce7;stroke:#15803d}.ld-flow-control{stroke-dasharray:6 3}.ld-flow-data{stroke-dasharray:2 3}.ld-flow-error{stroke:#dc2626}.ld-flow-message{stroke:#2563eb;stroke-dasharray:8 3}.ld-flow-sequence{stroke:#475569}.ld-empty-background{fill:#f8fafc;stroke:#cbd5e1;stroke-dasharray:4 3}`;
