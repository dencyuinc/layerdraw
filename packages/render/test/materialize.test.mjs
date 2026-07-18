// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import {
  assertRenderData,
  hashMaterializationInput,
  materializeRenderData,
} from "../dist/index.js";

const digest = `sha256:${"a".repeat(64)}`;
const digestB = `sha256:${"b".repeat(64)}`;
const rendererProfile = {
  profile_id: "layerdraw/default",
  profile_version: "1.0",
  specification_digest: digest,
};
const resolvedProfile = {
  renderer_profile: rendererProfile,
  supported_shapes: [
    "context",
    "diagram",
    "diff",
    "flow",
    "matrix",
    "table",
    "tree",
  ],
  supported_algorithms: ["grid", "layered", "native", "radial"],
};
const font = {
  family: "Inter",
  digest,
  units_per_em: 1000,
  ascent: 750,
  descent: 250,
  line_gap: 100,
  default_advance: 500,
  glyph_advances: { " ": 250, i: 250, m: 750 },
};
const layout = {
  item_order: "viewdata",
  tie_breaker: "seeded_key_hash",
  coordinate_precision: 3,
  item_width: 96,
  item_height: 48,
  horizontal_gap: 24,
  vertical_gap: 20,
  container_padding: 12,
  cell_padding: 6,
  lane_header_size: 28,
  badge_size: 14,
  overlay_offset: 3,
  port_offset: 2,
  font_size: 14,
  asset_scale: 1,
};
const limits = {
  max_primitives: 10000,
  max_route_points: 10000,
  max_text_length: 4096,
  max_depth: 128,
  max_extent: 1_000_000,
};

const corpus = JSON.parse(
  await readFile(
    new URL(
      "../../../tests/conformance/testdata/viewdata_conformance_v1.json",
      import.meta.url
    ),
    "utf8"
  )
);
const corpusNames = {
  diagram: "composed_diagram",
  table: "table_automatic",
  matrix: "matrix",
  tree: "tree",
  flow: "flow",
  context: "context",
  diff: "definition_diff",
};
const viewDataByShape = Object.fromEntries(
  Object.entries(corpusNames).map(([shape, name]) => {
    const entry = corpus.cases.find((candidate) => candidate.name === name);
    return [shape, entry.expected.normalized_response.payload.view_data];
  })
);
const treeRoot = viewDataByShape.tree.tree.roots[0];
const treeChild = treeRoot.children[0];
viewDataByShape.tree.tree.link_refs.push({
  key: `vdi:tree-link:${"L".repeat(43)}`,
  from_occurrence_key: treeRoot.key,
  to_entity_address: treeChild.entity_address,
  relation_address: treeChild.via_relation_address,
  source: treeChild.source,
});
viewDataByShape.tree.tree.cycle_refs.push({
  key: `vdi:tree-cycle:${"C".repeat(43)}`,
  from_occurrence_key: treeChild.key,
  to_entity_address: treeRoot.entity_address,
  relation_address: treeChild.via_relation_address,
  source: treeChild.source,
});
const flowConnector = viewDataByShape.flow.flow.connectors[0];
viewDataByShape.flow.flow.steps[0].branch = true;
viewDataByShape.flow.flow.steps[1].join = true;
viewDataByShape.flow.flow.cycle_refs.push({
  ...structuredClone(flowConnector),
  key: `vdi:flow-cycle:${"Y".repeat(43)}`,
  connector_key: `vdi:flow-connector:${"Y".repeat(43)}`,
  from_step_key: flowConnector.to_step_key,
  to_step_key: flowConnector.from_step_key,
});

function recipe(shape, extension = {}) {
  return {
    render_recipe_schema_version: 1,
    renderer_profile: rendererProfile,
    shape: { kind: shape },
    layout_algorithm: ["diagram", "tree", "flow"].includes(shape)
      ? { kind: "layered", crossing_reduction: "median", rank_separation: 64 }
      : { kind: "native" },
    layout_seed: { value: "fixture-seed" },
    density: { value: "comfortable" },
    orientation: { value: "top_to_bottom" },
    theme: {
      theme_id: "default",
      theme_version: "1",
      specification_digest: digest,
      color_scheme: "light",
    },
    locale: { language_tag: "en-US" },
    timezone: { iana_name: "UTC" },
    font_policy: {
      families: ["Inter"],
      fallback: "forbid",
      required_digests: [digest],
    },
    asset_policy: { mode: "resolved_only", required_digests: [] },
    ...extension,
  };
}

function input(shape, extension = {}) {
  return {
    view_data: structuredClone(viewDataByShape[shape]),
    view_data_hash: digest,
    recipe: recipe(shape),
    resolved_profile: structuredClone(resolvedProfile),
    resolved_fonts: [structuredClone(font)],
    resolved_assets: [],
    layout: structuredClone(layout),
    limits: structuredClone(limits),
    ...extension,
  };
}

function primitiveCounts(data) {
  const base = new Set([
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
  ]);
  return Object.fromEntries(
    Object.entries(data)
      .filter(([key, value]) => !base.has(key) && Array.isArray(value))
      .map(([key, value]) => [key, value.length])
  );
}

async function jsonDigest(value) {
  const bytes = new TextEncoder().encode(JSON.stringify(value));
  const hashed = new Uint8Array(await crypto.subtle.digest("SHA-256", bytes));
  return `sha256:${Array.from(hashed, (byte) =>
    byte.toString(16).padStart(2, "0")
  ).join("")}`;
}

function branchLabel(value) {
  if (value === undefined) return "";
  if (value.kind === "boolean") return value.boolean_value ? "true" : "false";
  if (value.kind === "integer") return String(value.integer_value ?? "");
  if (value.kind === "number") return value.number_value ?? "";
  return value.string_value ?? "";
}

test("all seven View shapes materialize repeatably and match deterministic goldens", async () => {
  const golden = JSON.parse(
    await readFile(
      new URL("fixtures/materialization-golden.json", import.meta.url),
      "utf8"
    )
  );
  const actual = {};
  for (const shape of Object.keys(corpusNames)) {
    const first = await materializeRenderData(input(shape));
    const second = await materializeRenderData(input(shape));
    assert.equal(
      first.ok,
      true,
      `${shape}: ${JSON.stringify(first.diagnostics)}`
    );
    assert.deepEqual(second, first);
    assertRenderData(first.data);
    assert.equal(first.data.kind, shape);
    assert.equal(first.data.locale, "en-US");
    assert.equal(first.data.timezone, "UTC");
    assert.equal(
      first.data.source_bindings.length,
      Object.values(primitiveCounts(first.data)).reduce(
        (sum, count) => sum + count,
        0
      )
    );
    assert.ok(
      first.data.source_bindings.every(
        (binding) =>
          Object.hasOwn(binding, "viewdata_key") &&
          Object.hasOwn(binding, "source_refs")
      )
    );
    actual[shape] = {
      render_data_digest: await jsonDigest(first.data),
      render_input_hash: first.data.render_input_hash,
      bounds: first.data.bounds,
      primitive_counts: primitiveCounts(first.data),
    };
  }
  assert.deepEqual(actual, golden);
});

test("shape-specific identity, routing, measurement, grouping, and change invariants survive materialization", async () => {
  const results = Object.fromEntries(
    await Promise.all(
      Object.keys(corpusNames).map(async (shape) => {
        const result = await materializeRenderData(input(shape));
        assert.equal(result.ok, true);
        return [shape, result.data];
      })
    )
  );
  assert.ok(
    results.diagram.occurrences.length > 0 &&
      results.diagram.edge_paths.every((edge) => edge.points.length >= 2)
  );
  assert.ok(
    results.diagram.labels.length >= results.diagram.occurrences.length
  );
  assert.ok(
    results.diagram.containers.length > 0 &&
      results.diagram.overlays.length > 0 &&
      results.diagram.badges.length > 0 &&
      results.diagram.support_geometry.length > 0
  );
  assert.ok(
    results.tree.occurrences.every((item) =>
      Number.isSafeInteger(item.depth)
    ) &&
      results.tree.duplicate_refs.length > 0 &&
      results.tree.cycle_refs.length > 0
  );
  assert.ok(
    results.flow.lanes.length > 0 &&
      results.flow.connectors.every((item) => item.points.length >= 2)
  );
  assert.deepEqual(
    results.flow.connectors.map((item) => ({
      connector_kind: item.connector_kind,
      label: item.label,
    })),
    viewDataByShape.flow.flow.connectors.map((item) => ({
      connector_kind: item.kind,
      label: branchLabel(item.branch_value),
    }))
  );
  assert.deepEqual(
    results.flow.cycle_refs.map((item) => ({
      connector_kind: item.connector_kind,
      label: item.label,
    })),
    viewDataByShape.flow.flow.cycle_refs.map((item) => ({
      connector_kind: item.kind,
      label: branchLabel(item.branch_value),
    }))
  );
  assert.ok(results.flow.branches.length > 0 && results.flow.joins.length > 0);
  assert.equal(
    results.flow.cycle_refs.length,
    viewDataByShape.flow.flow.cycle_refs.length
  );
  assert.equal(
    results.table.cells.length,
    results.table.rows.length * results.table.columns.length
  );
  assert.ok(results.table.columns.every((column) => column.width > 0));
  assert.equal(
    results.matrix.cells.length,
    viewDataByShape.matrix.matrix.cells.length
  );
  assert.equal(
    results.matrix.totals.length,
    0,
    "normative Matrix ViewData forbids inferred totals"
  );
  assert.equal(
    results.context.groups.length,
    viewDataByShape.context.context.groups.length
  );
  assert.equal(
    results.diff.changes.length,
    viewDataByShape.diff.diff.changes.length
  );
  assert.ok(
    Object.values(results).every(
      (data) => data.bounds.width >= 0 && data.bounds.height >= 0
    )
  );
});

test("malformed, adversarial, profile, font, asset, and geometry failures return stable typed diagnostics", async () => {
  const malformed = input("diagram", { view_data: { kind: "diagram" } });
  assert.deepEqual(
    (await materializeRenderData(malformed)).diagnostics.map(
      (item) => item.code
    ),
    ["render.input_invalid"]
  );

  const unsupported = input("diagram");
  unsupported.resolved_profile.supported_shapes = [
    "context",
    "diff",
    "flow",
    "matrix",
    "table",
    "tree",
  ];
  assert.deepEqual(
    (await materializeRenderData(unsupported)).diagnostics.map(
      (item) => item.code
    ),
    ["render.profile_unsupported"]
  );

  const incompatible = input("diagram");
  incompatible.resolved_profile.renderer_profile.profile_version = "2.0";
  assert.deepEqual(
    (await materializeRenderData(incompatible)).diagnostics.map(
      (item) => item.code
    ),
    ["render.profile_incompatible"]
  );

  const missingFont = input("diagram", {
    resolved_fonts: [{ ...font, family: "Mono" }],
  });
  assert.deepEqual(
    (await materializeRenderData(missingFont)).diagnostics.map(
      (item) => item.code
    ),
    ["render.font_missing"]
  );

  const missingAsset = input("diagram");
  missingAsset.recipe.asset_policy.required_digests = [digestB];
  assert.deepEqual(
    (await materializeRenderData(missingAsset)).diagnostics.map(
      (item) => item.code
    ),
    ["render.asset_missing"]
  );

  const missingViewAsset = input("diagram");
  missingViewAsset.view_data.diagram.occurrences[0].source.asset_digests = [
    digestB,
  ];
  assert.deepEqual(
    (await materializeRenderData(missingViewAsset)).diagnostics.map(
      (item) => item.code
    ),
    ["render.asset_missing"]
  );

  const invalidGeometry = input("diagram");
  invalidGeometry.view_data.diagram.edges[0].from_occurrence_key = `vdi:diagram-occurrence:${"Z".repeat(
    43
  )}`;
  assert.deepEqual(
    (await materializeRenderData(invalidGeometry)).diagnostics.map(
      (item) => item.code
    ),
    ["render.geometry_invalid"]
  );
});

test("hard resource limits fail closed without partial RenderData", async () => {
  for (const extension of [
    { limits: { ...limits, max_primitives: 1 } },
    { limits: { ...limits, max_route_points: 1 } },
    { limits: { ...limits, max_text_length: 1 } },
    { limits: { ...limits, max_extent: 1 } },
  ]) {
    const result = await materializeRenderData(input("diagram", extension));
    assert.equal(result.ok, false);
    assert.deepEqual(
      result.diagnostics.map((item) => item.code),
      ["render.resource_limit"]
    );
    assert.equal("data" in result, false);
  }
  const deepTree = input("tree");
  deepTree.view_data.tree.roots[0].children[0].children.push({
    ...structuredClone(deepTree.view_data.tree.roots[0]),
    key: `vdi:tree-occurrence:${"D".repeat(43)}`,
    children: [],
  });
  const depthResult = await materializeRenderData({
    ...deepTree,
    limits: { ...limits, max_depth: 1 },
  });
  assert.deepEqual(
    depthResult.diagnostics.map((item) => item.code),
    ["render.resource_limit"]
  );

  const deepDiagram = input("diagram");
  const nested = deepDiagram.view_data.diagram.occurrences.find(
    (item) => item.parent_key !== undefined
  );
  deepDiagram.view_data.diagram.occurrences.push({
    ...structuredClone(nested),
    key: `vdi:diagram-occurrence:${"D".repeat(43)}`,
    parent_key: nested.key,
  });
  const diagramDepthResult = await materializeRenderData({
    ...deepDiagram,
    limits: { ...limits, max_depth: 1 },
  });
  assert.deepEqual(
    diagramDepthResult.diagnostics.map((item) => item.code),
    ["render.resource_limit"]
  );
});

test("seed, metrics, dimensions, layout, locale, and timezone are explicit hashed inputs", async () => {
  const baseline = input("diagram");
  const variants = [
    input("diagram", {
      recipe: recipe("diagram", { layout_seed: { value: "other" } }),
    }),
    input("diagram", { resolved_fonts: [{ ...font, default_advance: 501 }] }),
    input("diagram", { layout: { ...layout, horizontal_gap: 25 } }),
    input("diagram", {
      recipe: recipe("diagram", { locale: { language_tag: "ja-JP" } }),
    }),
    input("diagram", {
      recipe: recipe("diagram", { timezone: { iana_name: "Asia/Tokyo" } }),
    }),
  ];
  const baselineHash = await hashMaterializationInput(baseline);
  for (const variant of variants)
    assert.notEqual(await hashMaterializationInput(variant), baselineHash);

  const withAsset = input("diagram", {
    resolved_assets: [{ digest: digestB, width: 20, height: 10 }],
  });
  const resizedAsset = input("diagram", {
    resolved_assets: [{ digest: digestB, width: 21, height: 10 }],
  });
  assert.notEqual(
    await hashMaterializationInput(withAsset),
    await hashMaterializationInput(resizedAsset)
  );
});

test("closed algorithms, orientations, density, font glyph metrics, and asset dimensions affect geometry deterministically", async () => {
  const variants = [
    input("diagram", {
      recipe: recipe("diagram", {
        layout_algorithm: { kind: "grid", columns: 2 },
      }),
    }),
    input("diagram", {
      recipe: recipe("diagram", {
        layout_algorithm: { kind: "radial", radius_step: 80 },
      }),
    }),
    input("diagram", {
      recipe: recipe("diagram", { layout_algorithm: { kind: "native" } }),
    }),
    input("diagram", {
      recipe: recipe("diagram", { orientation: { value: "left_to_right" } }),
    }),
    input("diagram", {
      recipe: recipe("diagram", { orientation: { value: "right_to_left" } }),
    }),
    input("diagram", {
      recipe: recipe("diagram", { orientation: { value: "bottom_to_top" } }),
    }),
    input("diagram", {
      recipe: recipe("diagram", { density: { value: "compact" } }),
    }),
    input("diagram", {
      recipe: recipe("diagram", { density: { value: "spacious" } }),
    }),
    input("diagram", {
      resolved_fonts: [
        { ...font, glyph_advances: { ...font.glyph_advances, a: 900 } },
      ],
    }),
  ];
  const renderDigests = new Set();
  for (const variant of variants) {
    const first = await materializeRenderData(variant);
    const second = await materializeRenderData(structuredClone(variant));
    assert.equal(first.ok, true, JSON.stringify(first.diagnostics));
    assert.deepEqual(second, first);
    renderDigests.add(await jsonDigest(first.data));
  }
  assert.ok(renderDigests.size >= 6);

  const withAsset = input("diagram");
  withAsset.view_data.diagram.occurrences[0].source.asset_digests = [digestB];
  withAsset.resolved_assets = [{ digest: digestB, width: 240, height: 120 }];
  const assetResult = await materializeRenderData(withAsset);
  assert.equal(assetResult.ok, true);
  assert.ok(
    assetResult.data.occurrences.some((item) => item.bounds.width >= 240)
  );
});

test("all closed table and matrix display value variants receive deterministic text measurements", async () => {
  const tableVariants = [
    { kind: "stable_address", stable_address: "ldl:project:p:entity:alpha" },
    { kind: "string_set", string_set: ["alpha", "beta"] },
    { kind: "scalar", scalar: { kind: "boolean", boolean_value: true } },
    { kind: "scalar", scalar: { kind: "integer", integer_value: "42" } },
    { kind: "scalar", scalar: { kind: "number", number_value: "4.2" } },
    { kind: "scalar", scalar: { kind: "date", string_value: "2026-07-18" } },
  ];
  for (const value of tableVariants) {
    const variant = input("table");
    const column = variant.view_data.table.columns[0];
    const row = variant.view_data.table.rows[0];
    row.cells[column.key] = { present: true, value, source: row.source };
    const result = await materializeRenderData(variant);
    assert.equal(result.ok, true, JSON.stringify(result.diagnostics));
    assert.ok(result.data.cells.some((cell) => cell.text.length > 0));
  }
  const matrixVariants = [
    { kind: "boolean", boolean: true },
    { kind: "integer", integer: "7" },
    { kind: "string_set", string_set: ["alpha", "beta"] },
    {
      kind: "attributes",
      attributes: [
        {
          relation_address: "ldl:project:p:relation:alpha_beta",
          row_address: "ldl:project:p:relation:alpha_beta:row:primary",
          column_address: "ldl:project:p:relation-type:calls:column:protocol",
          value: { kind: "enum", string_value: "http" },
        },
      ],
    },
  ];
  for (const display_value of matrixVariants) {
    const variant = input("matrix");
    variant.view_data.matrix.cells[0].display_value = display_value;
    const result = await materializeRenderData(variant);
    assert.equal(result.ok, true, JSON.stringify(result.diagnostics));
    assert.equal(typeof result.data.cells[0].text, "string");
  }
});

test("closed materialization input rejects unknown, unordered, nonfinite, and incompatible inputs", async () => {
  const invalidInputs = [];
  invalidInputs.push({ ...input("diagram"), unknown: true });
  invalidInputs.push(
    input("diagram", {
      resolved_profile: {
        ...resolvedProfile,
        supported_shapes: [...resolvedProfile.supported_shapes].reverse(),
      },
    })
  );
  invalidInputs.push(
    input("diagram", {
      resolved_fonts: [{ ...font, units_per_em: Number.NaN }],
    })
  );
  invalidInputs.push(
    input("diagram", {
      resolved_assets: [{ digest: digestB, width: 0, height: 1 }],
    })
  );
  invalidInputs.push(
    input("diagram", { layout: { ...layout, coordinate_precision: 7 } })
  );
  invalidInputs.push(input("diagram", { limits: { ...limits, max_depth: 0 } }));
  for (const invalid of invalidInputs)
    assert.deepEqual(
      (await materializeRenderData(invalid)).diagnostics.map(
        (item) => item.code
      ),
      ["render.input_invalid"]
    );

  const shapeMismatch = input("diagram", { recipe: recipe("tree") });
  assert.deepEqual(
    (await materializeRenderData(shapeMismatch)).diagnostics.map(
      (item) => item.code
    ),
    ["render.profile_incompatible"]
  );
  const algorithmMismatch = input("table", {
    recipe: recipe("table", {
      layout_algorithm: {
        kind: "layered",
        crossing_reduction: "median",
        rank_separation: 64,
      },
    }),
  });
  assert.deepEqual(
    (await materializeRenderData(algorithmMismatch)).diagnostics.map(
      (item) => item.code
    ),
    ["render.profile_incompatible"]
  );
  const fractionalGrid = input("diagram", {
    recipe: recipe("diagram", {
      layout_algorithm: { kind: "grid", columns: 1.5 },
    }),
  });
  assert.deepEqual(
    (await materializeRenderData(fractionalGrid)).diagnostics.map(
      (item) => item.code
    ),
    ["render.profile_incompatible"]
  );
  const noPrimaryFont = input("diagram", {
    recipe: recipe("diagram", {
      font_policy: { families: [], fallback: "forbid", required_digests: [] },
    }),
  });
  assert.deepEqual(
    (await materializeRenderData(noPrimaryFont)).diagnostics.map(
      (item) => item.code
    ),
    ["render.font_missing"]
  );
});

test("semantic ViewData order survives every ordered shape collection", async () => {
  const tableInput = input("table");
  tableInput.view_data.table.columns.reverse();
  const sourceRow = tableInput.view_data.table.rows[0];
  const authoredFirst = {
    ...structuredClone(sourceRow),
    key: `vdi:table-row:${"R".repeat(43)}`,
  };
  tableInput.view_data.table.rows = [authoredFirst, sourceRow];
  const tableResult = await materializeRenderData(tableInput);
  assert.equal(tableResult.ok, true, JSON.stringify(tableResult.diagnostics));
  assert.deepEqual(
    tableResult.data.columns.map((item) => item.id),
    tableInput.view_data.table.columns.map((item) => item.id)
  );
  assert.deepEqual(
    tableResult.data.rows.map((item) => item.render_key),
    tableInput.view_data.table.rows.map((item) => `table-row:${item.key}`)
  );

  const diffInput = input("diff");
  diffInput.view_data.diff.changes.reverse();
  const diffResult = await materializeRenderData(diffInput);
  assert.equal(diffResult.ok, true, JSON.stringify(diffResult.diagnostics));
  assert.deepEqual(
    diffResult.data.changes.map((item) => item.render_key),
    diffInput.view_data.diff.changes.map((item) => `diff-change:${item.key}`)
  );

  const treeInput = input("tree");
  const root = treeInput.view_data.tree.roots[0];
  const originalChild = root.children[0];
  const authoredFirstChild = {
    ...structuredClone(originalChild),
    key: `vdi:tree-occurrence:${"O".repeat(43)}`,
    children: [],
  };
  root.children = [authoredFirstChild, originalChild];
  const treeResult = await materializeRenderData(treeInput);
  assert.equal(treeResult.ok, true, JSON.stringify(treeResult.diagnostics));
  assert.deepEqual(
    treeResult.data.occurrences.slice(0, 3).map((item) => item.render_key),
    [root, authoredFirstChild, originalChild].map(
      (item) => `tree-occurrence:${item.key}`
    )
  );

  const matrixInput = input("matrix");
  matrixInput.view_data.matrix.row_axis.reverse();
  matrixInput.view_data.matrix.column_axis.reverse();
  matrixInput.view_data.matrix.cells.reverse();
  const matrixResult = await materializeRenderData(matrixInput);
  assert.equal(matrixResult.ok, true, JSON.stringify(matrixResult.diagnostics));
  assert.deepEqual(
    matrixResult.data.row_axes.map((item) => item.render_key),
    matrixInput.view_data.matrix.row_axis.map(
      (item) => `matrix-row-axis:${item.key}`
    )
  );
  assert.deepEqual(
    matrixResult.data.column_axes.map((item) => item.render_key),
    matrixInput.view_data.matrix.column_axis.map(
      (item) => `matrix-column-axis:${item.key}`
    )
  );
  assert.deepEqual(
    matrixResult.data.cells.map((item) => item.render_key),
    matrixInput.view_data.matrix.cells.map((item) => `matrix-cell:${item.key}`)
  );

  const flowInput = input("flow");
  flowInput.view_data.flow.lanes[0].step_keys.reverse();
  const flowResult = await materializeRenderData(flowInput);
  assert.equal(flowResult.ok, true, JSON.stringify(flowResult.diagnostics));
  assert.deepEqual(
    flowResult.data.steps.map((item) => item.render_key),
    flowInput.view_data.flow.steps.map((item) => `flow-step:${item.key}`)
  );
  const laneYs = flowInput.view_data.flow.lanes[0].step_keys.map(
    (key) =>
      flowResult.data.steps.find(
        (item) => item.render_key === `flow-step:${key}`
      ).bounds.y
  );
  assert.ok(
    laneYs[0] < laneYs[1],
    "lane.step_keys controls topological placement"
  );
});

test("render_input_hash covers canonical ViewData even when the claimed semantic hash is reused", async () => {
  const first = input("table");
  const second = input("table");
  second.view_data.table.columns.reverse();
  assert.equal(first.view_data_hash, second.view_data_hash);
  assert.notEqual(
    await hashMaterializationInput(first),
    await hashMaterializationInput(second)
  );
});

test("successful RenderData preserves ordered stable ViewData diagnostic identity", async () => {
  const diagnosticInput = input("table");
  diagnosticInput.view_data.diagnostics = [
    {
      arguments: {
        nested: {
          kind: "object",
          object_value: {
            values: {
              kind: "array",
              array_value: [
                { kind: "integer", integer_value: "42" },
                { kind: "number", number_value: "1.25" },
                {
                  kind: "stable_address",
                  stable_address_value: diagnosticInput.view_data.view_address,
                },
                { kind: "boolean", boolean_value: true },
              ],
            },
          },
        },
      },
      code: "LDL9998",
      message: "localized text is not render identity",
      message_key: "render_nested_test",
      owner_address: diagnosticInput.view_data.project_address,
      protocol_version: 1,
      related: [
        {
          message: "localized related text",
          relation: "current",
          subject_address: diagnosticInput.view_data.view_address,
        },
      ],
      severity: "info",
      subject_address: diagnosticInput.view_data.view_address,
    },
    {
      arguments: {
        detail: { kind: "string", string_value: "second" },
      },
      code: "LDL0001",
      message_key: "render_order_test",
      protocol_version: 1,
      related: [],
      severity: "warning",
    },
  ];
  const result = await materializeRenderData(diagnosticInput);
  assert.equal(result.ok, true, JSON.stringify(result.diagnostics));
  assert.deepEqual(result.diagnostics, []);
  assert.deepEqual(result.data.diagnostics, [
    {
      code: "LDL9998",
      severity: "information",
      message_key: "render_nested_test",
      arguments: {
        nested: {
          values: ["42", "1.25", diagnosticInput.view_data.view_address, true],
        },
      },
      protocol_version: 1,
      owner_address: diagnosticInput.view_data.project_address,
      subject_address: diagnosticInput.view_data.view_address,
    },
    {
      code: "LDL0001",
      severity: "warning",
      message_key: "render_order_test",
      arguments: { detail: "second" },
      protocol_version: 1,
    },
  ]);
});

test("native manual Diagram placement and direction are honored exactly", async () => {
  const manual = input("diagram", {
    recipe: recipe("diagram", {
      layout_algorithm: { kind: "native" },
      orientation: undefined,
    }),
  });
  const visibleRoots = manual.view_data.diagram.occurrences.filter(
    (item) => item.parent_key === undefined && item.role !== "support"
  );
  const root = visibleRoots[0];
  manual.view_data.shape.diagram.layout = "manual";
  manual.view_data.shape.diagram.direction = "left_to_right";
  manual.view_data.shape.diagram.placements = visibleRoots
    .map((item, index) =>
      item.key === root.key
        ? {
            entity_address: item.entity_address,
            x: "10.25",
            y: "-20.5",
            width: "300.75",
            height: "120.125",
          }
        : {
            entity_address: item.entity_address,
            x: String(index * 400),
            y: "0",
            width: "96",
            height: "48",
          }
    )
    .sort((left, right) =>
      left.entity_address < right.entity_address
        ? -1
        : left.entity_address > right.entity_address
        ? 1
        : 0
    );
  const result = await materializeRenderData(manual);
  assert.equal(result.ok, true, JSON.stringify(result.diagnostics));
  assert.deepEqual(
    result.data.occurrences.find(
      (item) => item.render_key === `diagram-occurrence:${root.key}`
    ).bounds,
    { x: 10.25, y: -20.5, width: 300.75, height: 120.125 }
  );
  assert.equal(
    result.data.edge_paths.some(
      (item) => item.points[0].y === item.points[1].y
    ),
    true,
    "Diagram direction supplies the absent recipe orientation"
  );

  const dangling = structuredClone(manual);
  const child = dangling.view_data.diagram.occurrences.find(
    (item) => item.parent_key !== undefined
  );
  dangling.view_data.shape.diagram.placements[0].entity_address =
    child.entity_address;
  const danglingResult = await materializeRenderData(dangling);
  assert.deepEqual(danglingResult.diagnostics, [
    {
      code: "render.geometry_invalid",
      severity: "error",
      message_key: "render.geometry_invalid",
      arguments: { reason: "dangling_diagram_placement" },
    },
  ]);

  const missing = structuredClone(manual);
  missing.view_data.shape.diagram.placements =
    missing.view_data.shape.diagram.placements.slice(1);
  const missingResult = await materializeRenderData(missing);
  assert.deepEqual(missingResult.diagnostics, [
    {
      code: "render.geometry_invalid",
      severity: "error",
      message_key: "render.geometry_invalid",
      arguments: { reason: "missing_diagram_placement" },
    },
  ]);

  dangling.recipe.layout_algorithm = {
    kind: "layered",
    crossing_reduction: "median",
    rank_separation: 64,
  };
  assert.equal((await materializeRenderData(dangling)).ok, true);
});

test("display payload required by visual adapters remains in RenderData", async () => {
  const results = Object.fromEntries(
    await Promise.all(
      Object.keys(corpusNames).map(async (shape) => {
        const result = await materializeRenderData(input(shape));
        assert.equal(result.ok, true, JSON.stringify(result.diagnostics));
        return [shape, result.data];
      })
    )
  );
  assert.ok(
    results.table.columns.every(
      (item) =>
        typeof item.id === "string" &&
        typeof item.label === "string" &&
        typeof item.value_type === "string" &&
        item.header_bounds.width === item.width
    )
  );
  assert.ok(results.flow.lanes.every((item) => typeof item.label === "string"));
  assert.ok(results.flow.steps.every((item) => typeof item.label === "string"));
  assert.ok(
    results.tree.occurrences.every((item) => typeof item.label === "string")
  );
  assert.ok(
    results.diff.before.every((item) => typeof item.label === "string")
  );
  assert.ok(results.diff.after.every((item) => typeof item.label === "string"));
  assert.ok(
    results.diagram.badges.every((item) => typeof item.label === "string")
  );
  assert.ok(
    results.diagram.overlays.every(
      (item) => typeof item.display_identity === "string"
    )
  );
  assert.ok(
    results.diagram.support_geometry.every(
      (item) =>
        typeof item.display_identity === "string" &&
        ["hidden_entity", "hidden_relation", "source_only"].includes(
          item.support_kind
        )
    )
  );
});

test("shape preflight rejects excessive products and inconsistent cross-references", async () => {
  const largeTable = input("table");
  const sourceRow = largeTable.view_data.table.rows[0];
  largeTable.view_data.table.rows = Array.from({ length: 64 }, (_, index) => ({
    ...structuredClone(sourceRow),
    key: `vdi:table-row:${String(index).padStart(43, "A")}`,
  }));
  const tableLimit = await materializeRenderData({
    ...largeTable,
    limits: { ...limits, max_primitives: 200 },
  });
  assert.deepEqual(
    tableLimit.diagnostics.map((item) => item.code),
    ["render.resource_limit"]
  );

  const missingTableCell = input("table");
  delete missingTableCell.view_data.table.rows[0].cells[
    missingTableCell.view_data.table.columns[0].key
  ];
  assert.equal(
    (await materializeRenderData(missingTableCell)).diagnostics[0].arguments
      .reason,
    "table_cell_keys_mismatch"
  );

  const duplicateMatrixCell = input("matrix");
  duplicateMatrixCell.view_data.matrix.cells[1].row_key =
    duplicateMatrixCell.view_data.matrix.cells[0].row_key;
  duplicateMatrixCell.view_data.matrix.cells[1].column_key =
    duplicateMatrixCell.view_data.matrix.cells[0].column_key;
  assert.equal(
    (await materializeRenderData(duplicateMatrixCell)).diagnostics[0].arguments
      .reason,
    "duplicate_matrix_coordinate"
  );

  const laneMismatch = input("flow");
  laneMismatch.view_data.flow.lanes[0].step_keys.pop();
  assert.equal(
    (await materializeRenderData(laneMismatch)).diagnostics[0].arguments.reason,
    "missing_flow_lane_membership"
  );

  const cycleMismatch = input("flow");
  cycleMismatch.view_data.flow.cycle_refs[0].connector_key =
    cycleMismatch.view_data.flow.connectors[0].key;
  assert.equal(
    (await materializeRenderData(cycleMismatch)).diagnostics[0].arguments
      .reason,
    "flow_cycle_connector_mismatch"
  );
});

test("overall bounds include position-only branch and join primitives", async () => {
  const positionOnly = input("flow", {
    layout: { ...layout, port_offset: 1000 },
  });
  positionOnly.view_data.flow.connectors = [];
  positionOnly.view_data.flow.cycle_refs = [];
  const result = await materializeRenderData(positionOnly);
  assert.equal(result.ok, true, JSON.stringify(result.diagnostics));
  const positions = [...result.data.branches, ...result.data.joins].map(
    (item) => item.position
  );
  assert.ok(positions.length > 0);
  for (const position of positions) {
    assert.ok(position.x >= result.data.bounds.x);
    assert.ok(position.y >= result.data.bounds.y);
    assert.ok(position.x <= result.data.bounds.x + result.data.bounds.width);
    assert.ok(position.y <= result.data.bounds.y + result.data.bounds.height);
  }
});

test("materialization core has no DOM, ambient clock, random state, or host locale dependency", async () => {
  const source = await readFile(
    new URL("../src/materialize.ts", import.meta.url),
    "utf8"
  );
  for (const forbidden of [
    "document.",
    "window.",
    "Date.",
    "Date(",
    "Math.random",
    "Intl.",
    "React",
    "Runtime",
    "localStorage",
  ])
    assert.equal(source.includes(forbidden), false, forbidden);
});
