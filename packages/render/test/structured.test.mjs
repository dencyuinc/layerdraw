// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import test from "node:test";
import {
  assertRenderData,
  DEFAULT_VISUAL_SURFACE_LIMITS,
  renderContextVisualSurface,
  renderDiffVisualSurface,
  renderMatrixVisualSurface,
  renderTableVisualSurface,
  VisualSurfaceError,
} from "../dist/index.js";
import { renderTableVisualSurface as tableSubpath } from "../dist/table.js";
import { renderMatrixVisualSurface as matrixSubpath } from "../dist/matrix.js";
import { renderContextVisualSurface as contextSubpath } from "../dist/context.js";
import { renderDiffVisualSurface as diffSubpath } from "../dist/diff.js";

const digest = `sha256:${"a".repeat(64)}`;
const profile = {
  profile_id: "layerdraw/default",
  profile_version: "1.0",
  specification_digest: digest,
};
const bounds = { x: 0, y: 0, width: 320, height: 240 };
const unit = { x: 0, y: 0, width: 80, height: 30 };
const viewdataKey = `vdi:render-primitive:${"A".repeat(43)}`;
const sourceRefs = {
  asset_digests: [],
  cell_refs: [],
  entity_addresses: ["ldl:project:p:entity:a"],
  layer_addresses: [],
  relation_addresses: ["ldl:project:p:relation:r"],
  row_addresses: [],
  state: { reads: [] },
  subject_addresses: ["ldl:project:p:entity:a"],
};

function base(kind, collections) {
  const keys = Object.values(collections).flat().map((item) => item.render_key);
  return {
    kind,
    render_data_schema_version: 1,
    renderer_profile: profile,
    view_data_hash: digest,
    render_input_hash: digest,
    shape: kind,
    layout_seed: "structured-fixture",
    locale: "en-US",
    timezone: "UTC",
    bounds,
    source_bindings: keys.map((render_key) => ({
      render_key,
      viewdata_key: viewdataKey,
      source_refs: sourceRefs,
    })),
    resolved_asset_digests: [],
    resolved_font_digests: [digest],
    diagnostics: [{ code: "fixture", severity: "information", message_key: "fixture", arguments: {} }],
    ...collections,
  };
}

function table() {
  return base("table", {
    columns: [
      { render_key: "column:metadata", x: 0, width: 100, frozen: true, id: "entity", label: "Entity metadata", value_type: "stable_address", header_bounds: { ...unit, width: 100 } },
      { render_key: "column:source", x: 100, width: 120, frozen: false, id: "source", label: "Source column", value_type: "string", header_bounds: { ...unit, x: 100, width: 120 } },
    ],
    rows: [
      { render_key: "row:attribute", y: 30, height: 30, frozen: false },
      { render_key: "row:relation", y: 60, height: 30, frozen: false },
    ],
    cells: [
      { render_key: "cell:a:metadata", bounds: { ...unit, y: 30, width: 100 }, row_key: "row:attribute", column_key: "column:metadata", text: "Application A" },
      { render_key: "cell:a:source", bounds: { ...unit, x: 100, y: 30, width: 120 }, row_key: "row:attribute", column_key: "column:source", text: "type workbook" },
      { render_key: "cell:r:metadata", bounds: { ...unit, y: 60, width: 100 }, row_key: "row:relation", column_key: "column:metadata", text: "A → B" },
      { render_key: "cell:r:source", bounds: { ...unit, x: 100, y: 60, width: 120 }, row_key: "row:relation", column_key: "column:source", text: "relation list" },
    ],
  });
}

function matrix() {
  return base("matrix", {
    row_axes: [
      { render_key: "matrix:row:a", bounds: { ...unit, y: 30 }, label: "Application A" },
      { render_key: "matrix:row:b", bounds: { ...unit, y: 60 }, label: "Application B" },
    ],
    column_axes: [
      { render_key: "matrix:column:x", bounds: { ...unit, x: 80 }, label: "Store X" },
      { render_key: "matrix:column:y", bounds: { ...unit, x: 160 }, label: "Store Y" },
    ],
    cells: [
      { render_key: "matrix:direct", bounds: { ...unit, x: 80, y: 30 }, row_axis_key: "matrix:row:a", column_axis_key: "matrix:column:x", text: "direct" },
      { render_key: "matrix:path", bounds: { ...unit, x: 160, y: 30 }, row_axis_key: "matrix:row:a", column_axis_key: "matrix:column:y", text: "path" },
      { render_key: "matrix:empty", bounds: { ...unit, x: 80, y: 60 }, row_axis_key: "matrix:row:b", column_axis_key: "matrix:column:x", text: "" },
    ],
    legends: [{ render_key: "matrix:legend", bounds: { ...unit, y: 100, width: 160 }, label: "direct / path" }],
    totals: [{ render_key: "matrix:total", bounds: { ...unit, x: 240, y: 30 }, axis_key: "matrix:row:a", text: "2" }],
  });
}

function context() {
  return base("context", {
    groups: [
      { render_key: "context:group:a", bounds: { x: 0, y: 0, width: 240, height: 100 }, label: "System A" },
      { render_key: "context:group:b", bounds: { x: 0, y: 110, width: 240, height: 100 }, label: "System B" },
    ],
    facts: [{ render_key: "context:fact", bounds: { ...unit, y: 30, width: 200 }, group_key: "context:group:a", text: "owner = Ops" }],
    relation_summaries: [{ render_key: "context:relation", bounds: { ...unit, y: 60, width: 200 }, group_key: "context:group:a", text: "outgoing to Store X" }],
    truncation_markers: [{ render_key: "context:truncated", bounds: { ...unit, y: 140, width: 200 }, group_key: "context:group:b", text: "3 supplied facts omitted" }],
  });
}

function diff() {
  const kinds = ["added", "removed", "updated", "moved", "moved_updated", "unchanged"];
  const before = [];
  const after = [];
  const changes = [];
  const fields = [];
  for (const [index, kind] of kinds.entries()) {
    const beforeKey = kind === "added" ? undefined : `diff:before:${kind}`;
    const afterKey = kind === "removed" ? undefined : `diff:after:${kind}`;
    if (beforeKey) before.push({ render_key: beforeKey, bounds: { ...unit, y: index * 35 }, label: `before ${kind}` });
    if (afterKey) after.push({ render_key: afterKey, bounds: { ...unit, x: 100, y: index * 35 }, label: `after ${kind}` });
    changes.push({
      render_key: `diff:change:${kind}`,
      bounds: { x: 0, y: index * 35, width: 200, height: 30 },
      change_kind: kind,
      ...(beforeKey ? { before_key: beforeKey } : {}),
      ...(afterKey ? { after_key: afterKey } : {}),
    });
    fields.push({ render_key: `diff:field:${kind}`, bounds: { ...unit, x: 210, y: index * 35 }, change_key: `diff:change:${kind}`, field_path: "display_name", ...(beforeKey ? { before_text: "Old" } : {}), ...(afterKey ? { after_text: "New" } : {}) });
  }
  return base("diff", { before, after, changes, fields });
}

const corpus = {
  table: [renderTableVisualSurface, tableSubpath, table],
  matrix: [renderMatrixVisualSurface, matrixSubpath, matrix],
  context: [renderContextVisualSurface, contextSubpath, context],
  diff: [renderDiffVisualSurface, diffSubpath, diff],
};

test("structured surfaces are repeatable, browser-neutral, and match byte goldens", async () => {
  const golden = JSON.parse(await readFile(new URL("fixtures/structured-surface-golden.json", import.meta.url), "utf8"));
  const actual = {};
  for (const [kind, [render, subpath, fixture]] of Object.entries(corpus)) {
    const first = render(fixture());
    const second = render(structuredClone(fixture()));
    assert.deepEqual(first, second);
    assert.deepEqual(subpath(fixture()), first);
    actual[kind] = {
      svg_sha256: createHash("sha256").update(first.svg).digest("hex"),
      svg_bytes: Buffer.byteLength(first.svg),
    };
    assert.equal(first.interaction.hit_targets.length, first.headless_items.length);
    assert.deepEqual(first.navigation.focus_order, first.interaction.focus_order);
    assert.deepEqual(first.interaction.source_bindings, first.interaction.hit_targets.map((item) => item.source_binding));
    assert.deepEqual(first.diagnostics, fixture().diagnostics);
    assert.match(first.svg, /role="(?:grid|document)"/);
  }
  assert.deepEqual(actual, golden);
});

test("table and matrix expose deterministic grid navigation, empty cells, totals, legends, and source drill-down", () => {
  const tableSurface = renderTableVisualSurface(table(), { virtual_window: { start: 1, count: 1 } });
  assert.deepEqual(tableSurface.virtualization, { axis: "row", total_units: 2, rendered_start: 1, rendered_count: 1, before_count: 1, after_count: 0 });
  assert.equal(tableSurface.navigation.mode, "grid");
  assert.ok(tableSurface.headless_items.some((item) => item.role === "columnheader"));
  assert.ok(tableSurface.svg.includes("data-column-id=\"entity\""));
  const matrixSurface = renderMatrixVisualSurface(matrix());
  assert.ok(matrixSurface.svg.includes("Matrix row 2, column 1: empty"));
  assert.ok(matrixSurface.svg.includes("matrix.legend"));
  assert.ok(matrixSurface.svg.includes("Supplied total 2"));
  assert.ok(matrixSurface.interaction.hit_targets.find((item) => item.render_key === "matrix:direct").source_binding.source_refs.relation_addresses.length > 0);
});

test("optional v1 structured extensions preserve backward compatibility", () => {
  const currentMatrix = matrix();
  const { legends: _legends, ...matrixWithoutLegends } = currentMatrix;
  const legacyMatrix = {
    ...matrixWithoutLegends,
    source_bindings: matrixWithoutLegends.source_bindings.filter(
      (binding) => binding.render_key !== "matrix:legend"
    ),
  };
  assert.doesNotThrow(() => assertRenderData(legacyMatrix));
  assert.deepEqual(
    renderMatrixVisualSurface(legacyMatrix),
    renderMatrixVisualSurface({ ...legacyMatrix, legends: [] })
  );

  const currentContext = context();
  const { truncation_markers: _markers, ...contextWithoutMarkers } = currentContext;
  const legacyContext = {
    ...contextWithoutMarkers,
    source_bindings: contextWithoutMarkers.source_bindings.filter(
      (binding) => binding.render_key !== "context:truncated"
    ),
  };
  assert.doesNotThrow(() => assertRenderData(legacyContext));
  assert.deepEqual(
    renderContextVisualSurface(legacyContext),
    renderContextVisualSurface({ ...legacyContext, truncation_markers: [] })
  );

  for (const value of [
    { ...legacyMatrix, legends: null },
    { ...legacyMatrix, legends: [{ render_key: "bad", bounds: unit, label: "x", unknown: true }] },
    { ...legacyContext, truncation_markers: "bad" },
  ])
    assert.throws(
      () => assertRenderData(value),
      (error) => error.code === "render.data_invalid"
    );
});

test("context preserves supplied facts, relation summaries, truncation, and group virtualization", () => {
  const surface = renderContextVisualSurface(context(), { virtual_window: { start: 1, count: 1 } });
  assert.equal(surface.virtualization.axis, "group");
  assert.equal(surface.virtualization.before_count, 1);
  assert.match(surface.svg, /3 supplied facts omitted/);
  assert.doesNotMatch(surface.svg, /owner = Ops/);
});

test("diff distinguishes every supplied state without rename or move inference", () => {
  const surface = renderDiffVisualSurface(diff());
  for (const kind of ["added", "removed", "updated", "moved", "moved_updated", "unchanged"])
    assert.ok(surface.interaction.hit_targets.some((item) => item.primitive_kind === `diff.change.${kind}`));
  assert.match(surface.svg, /before Old; after New/);
  assert.equal(surface.virtualization.axis, "change");
});

test("overflow, empty states, malformed input, closed options, and resource ceilings fail deterministically", () => {
  const clipped = renderTableVisualSurface(table(), { label_overflow: { mode: "ellipsis", max_scalars: 4 }, viewport: { x: 0, y: 0, width: 80, height: 40 } });
  assert.equal(clipped.overflow.label_mode, "ellipsis");
  assert.match(clipped.svg, /Ent…/);
  for (const [render, fixture] of [
    [renderTableVisualSurface, () => base("table", { columns: [], rows: [], cells: [] })],
    [renderMatrixVisualSurface, () => base("matrix", { row_axes: [], column_axes: [], cells: [], legends: [], totals: [] })],
    [renderContextVisualSurface, () => base("context", { groups: [], facts: [], relation_summaries: [], truncation_markers: [] })],
    [renderDiffVisualSurface, () => base("diff", { before: [], after: [], changes: [], fields: [] })],
  ]) {
    const surface = render(fixture());
    assert.equal(surface.empty, true);
    assert.equal(surface.interaction.hit_targets.length, 0);
    assert.match(surface.svg, /role="status"/);
  }
  const invalidCases = [
    () => renderTableVisualSurface(matrix()),
    () => renderTableVisualSurface({ ...table(), source_bindings: [] }),
    () => renderTableVisualSurface(table(), { unknown: true }),
    () => renderTableVisualSurface(table(), { accessibility_label: "" }),
    () => renderTableVisualSurface(table(), { virtual_window: { start: -1, count: 1 } }),
    () => renderTableVisualSurface(table(), { label_overflow: { mode: "bad", max_scalars: 1 } }),
    () => renderTableVisualSurface(table(), { viewport: { x: 0, y: 0, width: 0, height: 1 } }),
  ];
  for (const invoke of invalidCases)
    assert.throws(invoke, (error) => error instanceof VisualSurfaceError && /render\.visual_(?:data|options)_invalid/.test(error.code));
  for (const limits of [
    { ...DEFAULT_VISUAL_SURFACE_LIMITS, max_primitives: 1 },
    { ...DEFAULT_VISUAL_SURFACE_LIMITS, max_text_scalars: 1 },
    { ...DEFAULT_VISUAL_SURFACE_LIMITS, max_svg_bytes: 100 },
    { ...DEFAULT_VISUAL_SURFACE_LIMITS, max_extent: 10 },
  ])
    assert.throws(
      () => renderTableVisualSurface(table(), { limits }),
      (error) => error instanceof VisualSurfaceError && error.code === "render.visual_resource_limit"
    );
});

test("virtual windows cannot hide primitive, extent, text, or multibyte byte limit violations", () => {
  const virtual = { virtual_window: { start: 0, count: 1 } };
  assert.throws(
    () => renderTableVisualSurface(table(), {
      ...virtual,
      limits: { ...DEFAULT_VISUAL_SURFACE_LIMITS, max_primitives: 5 },
    }),
    (error) =>
      error instanceof VisualSurfaceError &&
      error.code === "render.visual_resource_limit" &&
      /max_primitives/.test(error.message)
  );

  const extreme = table();
  extreme.rows[1].y = DEFAULT_VISUAL_SURFACE_LIMITS.max_extent + 1;
  assert.throws(
    () => renderTableVisualSurface(extreme, virtual),
    (error) =>
      error instanceof VisualSurfaceError &&
      error.code === "render.visual_resource_limit" &&
      /max_extent/.test(error.message)
  );

  const giant = table();
  giant.cells[2].text = "隠".repeat(1_000);
  assert.throws(
    () => renderTableVisualSurface(giant, {
      ...virtual,
      limits: { ...DEFAULT_VISUAL_SURFACE_LIMITS, max_text_scalars: 900 },
    }),
    (error) =>
      error instanceof VisualSurfaceError &&
      error.code === "render.visual_resource_limit" &&
      /max_text_scalars/.test(error.message)
  );
  assert.throws(
    () => renderTableVisualSurface(giant, {
      ...virtual,
      limits: { ...DEFAULT_VISUAL_SURFACE_LIMITS, max_svg_bytes: 2_500 },
    }),
    (error) =>
      error instanceof VisualSurfaceError &&
      error.code === "render.visual_resource_limit" &&
      /max_svg_bytes/.test(error.message)
  );

  assert.throws(
    () => renderTableVisualSurface(table(), {
      accessibility_label: "界".repeat(100),
      limits: { ...DEFAULT_VISUAL_SURFACE_LIMITS, max_svg_bytes: 4_000 },
    }),
    (error) =>
      error instanceof VisualSurfaceError &&
      error.code === "render.visual_resource_limit" &&
      /max_svg_bytes/.test(error.message)
  );
});
