// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdir, mkdtemp, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";
import test from "node:test";
import {
  assertRenderData,
  assertRenderRecipe,
  hashRenderInput,
  RenderContractError,
} from "../dist/index.js";

const execute = promisify(execFile);
const digest = `sha256:${"a".repeat(64)}`;
const digestB = `sha256:${"b".repeat(64)}`;
const profile = {
  profile_id: "layerdraw/default",
  profile_version: "1.0",
  specification_digest: digest,
};
const recipe = {
  render_recipe_schema_version: 1,
  renderer_profile: profile,
  shape: { kind: "diagram" },
  layout_algorithm: {
    kind: "layered",
    crossing_reduction: "median",
    rank_separation: 64,
  },
  layout_seed: { value: "fixed-seed" },
  density: { value: "comfortable" },
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
};

test("RenderRecipe rejects unknown options and unversioned profiles", () => {
  assert.doesNotThrow(() => assertRenderRecipe(recipe));
  assert.throws(
    () => assertRenderRecipe({ ...recipe, magic: true }),
    (error) =>
      error instanceof RenderContractError &&
      error.code === "render.recipe_invalid"
  );
  assert.throws(
    () =>
      assertRenderRecipe({
        ...recipe,
        renderer_profile: { ...profile, profile_version: "latest" },
      }),
    (error) =>
      error instanceof RenderContractError &&
      error.code === "render.profile_incompatible"
  );
  assert.throws(() =>
    assertRenderRecipe({
      ...recipe,
      layout_algorithm: {
        kind: "layered",
        crossing_reduction: "median",
        rank_separation: 64,
        undocumented: true,
      },
    })
  );
});

test("canonical RenderRecipe fixture is accepted by the TypeScript owner", async () => {
  const fixture = JSON.parse(
    await readFile(
      new URL(
        "../../../schemas/fixtures/conformance/render-export-contracts-v1.json",
        import.meta.url
      ),
      "utf8"
    )
  );
  assert.equal(fixture.schema_version, 1);
  assert.doesNotThrow(() => assertRenderRecipe(fixture.render_recipe));
});

test("packed render package carries the canonical LayerDraw license", async () => {
  const temporary = await mkdtemp(join(tmpdir(), "layerdraw-render-pack-"));
  const { stdout } = await execute(
    "corepack",
    ["pnpm", "pack", "--pack-destination", temporary, "--json"],
    {
      cwd: new URL("../", import.meta.url),
      maxBuffer: 10 * 1024 * 1024,
    }
  );
  const packed = JSON.parse(stdout);
  const archive = join(temporary, packed.filename.split("/").at(-1));
  const extracted = join(temporary, "extracted");
  await mkdir(extracted);
  await execute("tar", ["-xzf", archive, "-C", extracted]);
  const manifest = JSON.parse(
    await readFile(join(extracted, "package", "package.json"), "utf8")
  );
  assert.equal(manifest.license, "SEE LICENSE IN LICENSE");
  assert.deepEqual(
    await readFile(join(extracted, "package", "LICENSE")),
    await readFile(new URL("../../../LICENSE", import.meta.url))
  );
});

const bounds = { x: 0, y: 0, width: 10, height: 10 };
const point = { x: 0, y: 0 };
const primitiveSets = {
  diagram: {
    containers: [{ render_key: "container", bounds, child_keys: ["node"] }],
    occurrences: [
      { render_key: "node", bounds, port_keys: ["port"], label_key: "label" },
    ],
    ports: [{ render_key: "port", position: point, occurrence_key: "node" }],
    edge_paths: [
      {
        render_key: "edge",
        points: [point, { x: 1, y: 1 }],
        from_port_key: "port",
        to_port_key: "port",
      },
    ],
    labels: [
      {
        render_key: "label",
        bounds,
        text: "Node",
        anchor: { kind: "occurrence", occurrence_key: "node" },
      },
      {
        render_key: "edge-label",
        bounds,
        text: "Edge",
        anchor: { kind: "edge_path", edge_path_key: "edge" },
      },
    ],
    overlays: [
      {
        render_key: "overlay",
        bounds,
        target: { kind: "container", container_key: "container" },
        display_identity: "Overlay",
      },
    ],
    badges: [
      {
        render_key: "badge",
        bounds,
        target: { kind: "occurrence", occurrence_key: "node" },
        label: "Badge",
      },
    ],
    support_geometry: [
      {
        render_key: "support",
        bounds,
        support_kind: "source_only",
        display_identity: "Support",
      },
    ],
  },
  table: {
    columns: [
      {
        render_key: "column",
        x: 0,
        width: 10,
        frozen: true,
        id: "name",
        label: "Name",
        value_type: "stable_address",
        header_bounds: bounds,
      },
    ],
    rows: [{ render_key: "row", y: 0, height: 10, frozen: false }],
    cells: [
      {
        render_key: "cell",
        bounds,
        row_key: "row",
        column_key: "column",
        text: "value",
      },
    ],
  },
  matrix: {
    row_axes: [{ render_key: "row-axis", bounds, label: "Row" }],
    column_axes: [{ render_key: "column-axis", bounds, label: "Column" }],
    cells: [
      {
        render_key: "matrix-cell",
        bounds,
        row_axis_key: "row-axis",
        column_axis_key: "column-axis",
        text: "1",
      },
    ],
    legends: [{ render_key: "legend", bounds, label: "Direct relation" }],
    totals: [{ render_key: "total", bounds, axis_key: "row-axis", text: "1" }],
  },
  tree: {
    occurrences: [{ render_key: "tree-node", bounds, depth: 0, label: "Node" }],
    duplicate_refs: [
      {
        render_key: "duplicate",
        points: [point, { x: 1, y: 1 }],
        from_occurrence_key: "tree-node",
        to_occurrence_key: "tree-node",
      },
    ],
    cycle_refs: [
      {
        render_key: "cycle",
        points: [point, { x: 1, y: 1 }],
        from_occurrence_key: "tree-node",
        to_occurrence_key: "tree-node",
      },
    ],
  },
  flow: {
    lanes: [{ render_key: "lane", bounds, label: "Lane" }],
    steps: [{ render_key: "step", bounds, lane_key: "lane", label: "Step" }],
    branches: [{ render_key: "branch", position: point, step_keys: ["step"] }],
    joins: [{ render_key: "join", position: point, step_keys: ["step"] }],
    connectors: [
      {
        render_key: "connector",
        points: [point, { x: 1, y: 1 }],
        from_step_key: "step",
        to_step_key: "step",
        connector_kind: "data",
        label: "http",
      },
    ],
    cycle_refs: [
      {
        render_key: "flow-cycle",
        points: [point, { x: 1, y: 1 }],
        from_step_key: "step",
        to_step_key: "step",
        connector_kind: "sequence",
        label: "",
      },
    ],
  },
  context: {
    groups: [{ render_key: "group", bounds, label: "Group" }],
    facts: [{ render_key: "fact", bounds, group_key: "group", text: "Fact" }],
    relation_summaries: [
      { render_key: "summary", bounds, group_key: "group", text: "Summary" },
    ],
    truncation_markers: [
      { render_key: "truncated", bounds, group_key: "group", text: "2 more" },
    ],
  },
  diff: {
    before: [{ render_key: "before", bounds, label: "Before" }],
    after: [{ render_key: "after", bounds, label: "After" }],
    changes: [
      {
        render_key: "change",
        bounds,
        change_kind: "updated",
        before_key: "before",
        after_key: "after",
      },
    ],
    fields: [
      {
        render_key: "field",
        bounds,
        change_key: "change",
        field_path: "name",
        before_text: "a",
        after_text: "b",
      },
    ],
  },
};

function renderData(kind) {
  const primitives = primitiveSets[kind];
  const keys = Object.values(primitives)
    .flat()
    .map((value) => value.render_key);
  return {
    kind,
    render_data_schema_version: 1,
    renderer_profile: profile,
    view_data_hash: digest,
    render_input_hash: digest,
    shape: kind,
    layout_seed: "fixed-seed",
    locale: "en-US",
    timezone: "UTC",
    bounds: { x: 0, y: 0, width: 100, height: 100 },
    source_bindings: keys.map((render_key) => ({
      render_key,
      viewdata_key: `vdi:render-primitive:${"A".repeat(43)}`,
    })),
    resolved_asset_digests: [],
    resolved_font_digests: [digest],
    diagnostics: [],
    ...primitives,
  };
}

test("RenderData requires every visual primitive to have a semantic binding", () => {
  const data = renderData("diagram");
  assert.doesNotThrow(() => assertRenderData(data));
  assert.throws(
    () =>
      assertRenderData({
        ...data,
        occurrences: [
          ...data.occurrences,
          { render_key: "node:2", bounds, port_keys: [] },
        ],
      }),
    (error) =>
      error.code === "render.data_invalid" && /untraceable/.test(error.message)
  );
  assert.throws(
    () =>
      assertRenderData({ ...data, source_bindings: [{ render_key: "node" }] }),
    (error) => error.code === "render.data_invalid"
  );
  assert.throws(
    () =>
      assertRenderData({
        ...data,
        source_bindings: [{ render_key: "node", source_refs: {} }],
      }),
    (error) =>
      error.code === "render.data_invalid" && /source refs/.test(error.message)
  );
  assert.throws(
    () =>
      assertRenderData({
        ...data,
        source_bindings: data.source_bindings.map((binding) => ({
          ...binding,
          viewdata_key: "not-a-viewdata-key",
        })),
      }),
    (error) =>
      error.code === "render.data_invalid" &&
      /ViewData identity/.test(error.message)
  );
  assert.throws(
    () =>
      assertRenderData({
        ...data,
        labels: [{ ...data.labels[0], render_key: "node" }],
      }),
    (error) =>
      error.code === "render.data_invalid" &&
      /duplicate visual render_key/.test(error.message)
  );
  assert.throws(
    () =>
      assertRenderData({
        ...data,
        source_bindings: [
          ...data.source_bindings,
          {
            render_key: "ghost",
            viewdata_key: `vdi:render-primitive:${"A".repeat(43)}`,
          },
        ],
      }),
    (error) =>
      error.code === "render.data_invalid" &&
      /no visual primitive/.test(error.message)
  );
});

test("render input hashes are deterministic and cover profile bytes", async () => {
  const input = {
    view_data_hash: digest,
    recipe,
    asset_digests: [],
    font_digests: [digest],
  };
  assert.equal(
    await hashRenderInput(input),
    await hashRenderInput(structuredClone(input))
  );
  assert.notEqual(
    await hashRenderInput(input),
    await hashRenderInput({
      ...input,
      recipe: { ...recipe, layout_seed: { value: "other" } },
    })
  );
  await assert.rejects(
    hashRenderInput({ ...input, extra: true }),
    (error) =>
      error.code === "render.recipe_invalid" &&
      /unknown option/.test(error.message)
  );
  await assert.rejects(
    hashRenderInput({ ...input, asset_digests: [digestB, digest] }),
    (error) =>
      error.code === "render.recipe_invalid" &&
      /sorted and unique/.test(error.message)
  );
  await assert.rejects(
    hashRenderInput({ ...input, font_digests: [digestB, digest] }),
    (error) =>
      error.code === "render.recipe_invalid" &&
      /sorted and unique/.test(error.message)
  );
});

test("all closed recipe branches validate without opening extension maps", () => {
  const variants = [
    { layout_algorithm: { kind: "native" } },
    { layout_algorithm: { kind: "grid", columns: 3 } },
    { layout_algorithm: { kind: "radial", radius_step: 20 } },
    {
      orientation: { value: "left_to_right" },
      viewport: { width: 800, height: 600, device_scale: 2 },
      rasterizer_profile: profile,
      interaction_policy: { selection: true, pan: false, zoom: true },
    },
  ];
  for (const extension of variants)
    assert.doesNotThrow(() => assertRenderRecipe({ ...recipe, ...extension }));
  for (const invalid of [
    null,
    { ...recipe, render_recipe_schema_version: 2 },
    { ...recipe, shape: { kind: "unknown" } },
    { ...recipe, layout_algorithm: { kind: "unknown" } },
    { ...recipe, density: { value: "dense" } },
    { ...recipe, viewport: { width: 0, height: 1, device_scale: 1 } },
    { ...recipe, font_policy: { ...recipe.font_policy, families: [""] } },
    {
      ...recipe,
      asset_policy: {
        ...recipe.asset_policy,
        required_digests: [digest, digest],
      },
    },
    {
      ...recipe,
      font_policy: {
        ...recipe.font_policy,
        required_digests: [digestB, digest],
      },
    },
    {
      ...recipe,
      asset_policy: {
        ...recipe.asset_policy,
        required_digests: [digestB, digest],
      },
    },
  ])
    assert.throws(() => assertRenderRecipe(invalid));
});

test("all seven RenderData union members close and source-bind every primitive kind", () => {
  for (const kind of Object.keys(primitiveSets)) {
    const value = renderData(kind);
    value.diagnostics = [
      {
        code: "render.info",
        severity: "information",
        message_key: "render.info",
        arguments: {},
      },
    ];
    assert.doesNotThrow(() => assertRenderData(value));
    assert.throws(() => assertRenderData({ ...value, extra: true }));
    for (const primitive of Object.values(primitiveSets[kind]).flat()) {
      const missing = {
        ...value,
        source_bindings: value.source_bindings.filter(
          (binding) => binding.render_key !== primitive.render_key
        ),
      };
      assert.throws(
        () => assertRenderData(missing),
        (error) =>
          error.code === "render.data_invalid" &&
          error.message.includes(primitive.render_key)
      );
    }
  }
  for (const invalid of [
    { ...renderData("diagram"), resolved_font_digests: ["bad"] },
    { ...renderData("diagram"), resolved_asset_digests: [digestB, digest] },
    { ...renderData("diagram"), resolved_font_digests: [digestB, digest] },
    {
      ...renderData("flow"),
      connectors: [
        {
          render_key: "connector",
          points: [point],
          from_step_key: "step",
          to_step_key: "step",
          connector_kind: "data",
          label: "http",
        },
      ],
    },
    null,
  ]) {
    assert.throws(
      () => assertRenderData(invalid),
      (error) =>
        error instanceof RenderContractError &&
        error.code === "render.data_invalid"
    );
  }
});

test("RenderData cross-references and recursive diagnostic data are closed", () => {
  const invalidReferences = [
    {
      kind: "diagram",
      mutate: (value) => {
        value.edge_paths[0].from_port_key = "missing";
      },
    },
    {
      kind: "tree",
      mutate: (value) => {
        value.occurrences[0].parent_key = "missing";
      },
    },
    {
      kind: "flow",
      mutate: (value) => {
        value.steps[0].lane_key = "missing";
      },
    },
    {
      kind: "table",
      mutate: (value) => {
        value.cells[0].column_key = "missing";
      },
    },
    {
      kind: "matrix",
      mutate: (value) => {
        value.totals[0].axis_key = "missing";
      },
    },
    {
      kind: "context",
      mutate: (value) => {
        value.facts[0].group_key = "missing";
      },
    },
    {
      kind: "diff",
      mutate: (value) => {
        value.fields[0].change_key = "missing";
      },
    },
  ];
  for (const testCase of invalidReferences) {
    const value = structuredClone(renderData(testCase.kind));
    testCase.mutate(value);
    assert.throws(
      () => assertRenderData(value),
      (error) =>
        error.code === "render.data_invalid" && /reference/.test(error.message)
    );
  }
  const diagnostic = renderData("context");
  diagnostic.diagnostics = [
    {
      code: "render.info",
      severity: "information",
      message_key: "render.info",
      arguments: { nested: [{ invalid_number: 1 }] },
    },
  ];
  assert.throws(
    () => assertRenderData(diagnostic),
    (error) =>
      error.code === "render.data_invalid" && /JsonValue/.test(error.message)
  );
});

test("diagram anchors and decoration targets are closed and unambiguous", () => {
  assert.doesNotThrow(() => assertRenderData(renderData("diagram")));
  for (const mutate of [
    (value) => {
      delete value.overlays[0].target;
    },
    (value) => {
      delete value.badges[0].target;
    },
    (value) => {
      value.labels[0].anchor = { kind: "edge_path", edge_path_key: "missing" };
    },
    (value) => {
      value.labels[0].anchor = {
        kind: "container",
        container_key: "container",
      };
    },
    (value) => {
      value.overlays[0].target = {
        kind: "container",
        container_key: "missing",
      };
    },
    (value) => {
      value.badges[0].target = { kind: "edge_path", edge_path_key: "edge" };
    },
  ]) {
    const value = structuredClone(renderData("diagram"));
    mutate(value);
    assert.throws(
      () => assertRenderData(value),
      (error) => error.code === "render.data_invalid"
    );
  }
});

test("Flow connector semantics are closed and required", () => {
  assert.doesNotThrow(() => assertRenderData(renderData("flow")));
  for (const mutate of [
    (value) => {
      value.connectors[0].connector_kind = "unknown";
    },
    (value) => {
      delete value.connectors[0].connector_kind;
    },
    (value) => {
      value.connectors[0].label = 1;
    },
    (value) => {
      delete value.cycle_refs[0].label;
    },
  ]) {
    const value = structuredClone(renderData("flow"));
    mutate(value);
    assert.throws(
      () => assertRenderData(value),
      (error) => error.code === "render.data_invalid"
    );
  }
});

test("diff change kinds close before and after presence", () => {
  const valid = [
    { change_kind: "added", after_key: "after" },
    { change_kind: "removed", before_key: "before" },
    { change_kind: "updated", before_key: "before", after_key: "after" },
    { change_kind: "moved", before_key: "before", after_key: "after" },
    { change_kind: "moved_updated", before_key: "before", after_key: "after" },
    { change_kind: "unchanged", before_key: "before", after_key: "after" },
  ];
  for (const change of valid) {
    const value = renderData("diff");
    value.changes = [{ render_key: "change", bounds, ...change }];
    assert.equal(
      Object.hasOwn(value.changes[0], "before_key"),
      change.change_kind !== "added"
    );
    assert.equal(
      Object.hasOwn(value.changes[0], "after_key"),
      change.change_kind !== "removed"
    );
    assert.doesNotThrow(() => assertRenderData(value));
  }
  for (const change of [
    { change_kind: "added", before_key: "before", after_key: "after" },
    { change_kind: "added" },
    { change_kind: "removed", before_key: "before", after_key: "after" },
    { change_kind: "removed" },
    { change_kind: "updated", after_key: "after" },
    { change_kind: "moved", before_key: "before" },
    { change_kind: "moved_updated" },
    { change_kind: "unchanged", before_key: "before" },
  ]) {
    const value = renderData("diff");
    value.changes = [{ render_key: "change", bounds, ...change }];
    assert.throws(
      () => assertRenderData(value),
      (error) =>
        error.code === "render.data_invalid" && /requires/.test(error.message)
    );
  }
});
