// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import {
  DEFAULT_VISUAL_SURFACE_LIMITS,
  renderDiagramVisualSurface,
  renderFlowVisualSurface,
  renderTreeVisualSurface,
  VISUAL_SURFACE_HARD_LIMITS,
  VisualSurfaceError,
} from "../dist/index.js";
import { renderDiagramVisualSurface as renderDiagramSubpath } from "../dist/diagram.js";
import { renderFlowVisualSurface as renderFlowSubpath } from "../dist/flow.js";
import { renderTreeVisualSurface as renderTreeSubpath } from "../dist/tree.js";

const digest = `sha256:${"a".repeat(64)}`;
const profile = {
  profile_id: "layerdraw/default",
  profile_version: "1.0",
  specification_digest: digest,
};
const bounds = { x: 0, y: 0, width: 240, height: 160 };
const sourceRefs = {
  asset_digests: [],
  cell_refs: [],
  entity_addresses: ["ldl:project:p:entity:a"],
  layer_addresses: [],
  relation_addresses: [],
  row_addresses: [],
  state: { reads: [] },
  subject_addresses: ["ldl:project:p:entity:a"],
};
const viewdataKey = `vdi:render-primitive:${"A".repeat(43)}`;

function base(kind, collections, extension = {}) {
  const keys = Object.values(collections)
    .flat()
    .map((primitive) => primitive.render_key);
  return {
    kind,
    render_data_schema_version: 1,
    renderer_profile: profile,
    view_data_hash: digest,
    render_input_hash: digest,
    shape: kind,
    layout_seed: "visual-fixture",
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
    diagnostics: [
      {
        code: "view.hidden_support",
        severity: "information",
        message_key: "view.hidden_support",
        arguments: { count: "1" },
      },
    ],
    ...collections,
    ...extension,
  };
}

function diagram() {
  return base("diagram", {
    containers: [
      {
        render_key: "container:outer",
        bounds: { x: 0, y: 0, width: 220, height: 130 },
        child_keys: ["occurrence:container", "occurrence:child"],
      },
      {
        render_key: "container:inner",
        bounds: { x: 10, y: 10, width: 160, height: 90 },
        child_keys: ["occurrence:child"],
      },
    ],
    occurrences: [
      {
        render_key: "occurrence:container",
        bounds: { x: 10, y: 10, width: 160, height: 90 },
        port_keys: ["port:from"],
        label_key: "label:container",
      },
      {
        render_key: "occurrence:child",
        bounds: { x: 100, y: 45, width: 90, height: 44 },
        port_keys: ["port:to"],
        label_key: "label:child",
      },
    ],
    ports: [
      {
        render_key: "port:from",
        position: { x: 170, y: 55 },
        occurrence_key: "occurrence:container",
      },
      {
        render_key: "port:to",
        position: { x: 100, y: 67 },
        occurrence_key: "occurrence:child",
      },
    ],
    edge_paths: [
      {
        render_key: "edge:calls",
        points: [
          { x: 170, y: 55 },
          { x: 185, y: 55 },
          { x: 185, y: 67 },
          { x: 100, y: 67 },
        ],
        from_port_key: "port:from",
        to_port_key: "port:to",
      },
    ],
    labels: [
      {
        render_key: "label:container",
        bounds: { x: 12, y: 12, width: 60, height: 18 },
        text: "Container & owner",
        anchor: {
          kind: "occurrence",
          occurrence_key: "occurrence:container",
        },
      },
      {
        render_key: "label:child",
        bounds: { x: 104, y: 49, width: 60, height: 18 },
        text: "A very long child label",
        anchor: { kind: "occurrence", occurrence_key: "occurrence:child" },
      },
      {
        render_key: "label:edge",
        bounds: { x: 150, y: 58, width: 32, height: 14 },
        text: "calls",
        anchor: { kind: "edge_path", edge_path_key: "edge:calls" },
      },
    ],
    overlays: [
      {
        render_key: "overlay:owner",
        bounds: { x: 150, y: 12, width: 18, height: 18 },
        target: { kind: "container", container_key: "container:inner" },
        display_identity: "Ops",
      },
    ],
    badges: [
      {
        render_key: "badge:risk",
        bounds: { x: 186, y: 40, width: 14, height: 14 },
        target: { kind: "occurrence", occurrence_key: "occurrence:child" },
        label: "!",
      },
    ],
    support_geometry: [
      {
        render_key: "support:hidden",
        bounds: { x: 0, y: 142, width: 18, height: 18 },
        support_kind: "hidden_relation",
        display_identity: "relation:hidden",
      },
    ],
  });
}

function tree() {
  return base("tree", {
    occurrences: [
      {
        render_key: "tree:root",
        bounds: { x: 0, y: 0, width: 100, height: 36 },
        depth: 0,
        label: "Root",
      },
      {
        render_key: "tree:child",
        bounds: { x: 40, y: 58, width: 100, height: 36 },
        parent_key: "tree:root",
        depth: 1,
        label: "Shared child",
      },
      {
        render_key: "tree:duplicate",
        bounds: { x: 140, y: 116, width: 100, height: 36 },
        parent_key: "tree:child",
        depth: 2,
        label: "Shared child",
      },
    ],
    duplicate_refs: [
      {
        render_key: "tree-ref:duplicate",
        points: [
          { x: 190, y: 116 },
          { x: 90, y: 94 },
        ],
        from_occurrence_key: "tree:duplicate",
        to_occurrence_key: "tree:child",
      },
    ],
    cycle_refs: [
      {
        render_key: "tree-ref:cycle",
        points: [
          { x: 140, y: 76 },
          { x: 50, y: 36 },
        ],
        from_occurrence_key: "tree:child",
        to_occurrence_key: "tree:root",
      },
    ],
  });
}

function flow() {
  const connectors = ["sequence", "control", "data", "error", "message"].map(
    (connector_kind, index) => ({
      render_key: `connector:${connector_kind}`,
      points: [
        { x: 72, y: 30 + index * 2 },
        { x: 120, y: 30 + index * 2 },
        { x: 120, y: 90 + index * 2 },
        { x: 168, y: 90 + index * 2 },
      ],
      from_step_key: "step:start",
      to_step_key: "step:end",
      connector_kind,
      label: `${connector_kind} <route>`,
    })
  );
  return base("flow", {
    lanes: [
      {
        render_key: "lane:one",
        bounds: { x: 0, y: 0, width: 240, height: 70 },
        label: "Origin lane",
      },
      {
        render_key: "lane:two",
        bounds: { x: 0, y: 80, width: 240, height: 70 },
        label: "Target lane",
      },
    ],
    steps: [
      {
        render_key: "step:start",
        bounds: { x: 24, y: 16, width: 48, height: 36 },
        lane_key: "lane:one",
        label: "Start",
      },
      {
        render_key: "step:end",
        bounds: { x: 168, y: 94, width: 48, height: 36 },
        lane_key: "lane:two",
        label: "End",
      },
    ],
    branches: [
      {
        render_key: "branch:start",
        position: { x: 76, y: 34 },
        step_keys: ["step:start"],
      },
    ],
    joins: [
      {
        render_key: "join:end",
        position: { x: 164, y: 112 },
        step_keys: ["step:end"],
      },
    ],
    connectors,
    cycle_refs: [
      {
        render_key: "cycle:return",
        points: [
          { x: 192, y: 130 },
          { x: 192, y: 150 },
          { x: 48, y: 150 },
          { x: 48, y: 52 },
        ],
        from_step_key: "step:end",
        to_step_key: "step:start",
        connector_kind: "sequence",
        label: "loop",
      },
    ],
  });
}

async function sha256(value) {
  const bytes = new TextEncoder().encode(value);
  const hash = new Uint8Array(await crypto.subtle.digest("SHA-256", bytes));
  return Array.from(hash, (byte) => byte.toString(16).padStart(2, "0")).join("");
}

test("Diagram, Tree, and Flow SVG bytes are repeatable and match goldens", async () => {
  const golden = JSON.parse(
    await readFile(new URL("fixtures/visual-surface-golden.json", import.meta.url), "utf8")
  );
  const cases = [
    ["diagram", diagram(), renderDiagramVisualSurface, renderDiagramSubpath],
    ["tree", tree(), renderTreeVisualSurface, renderTreeSubpath],
    ["flow", flow(), renderFlowVisualSurface, renderFlowSubpath],
  ];
  const actual = {};
  for (const [shape, data, render, subpath] of cases) {
    const first = render(data);
    const second = render(structuredClone(data));
    assert.deepEqual(second, first);
    assert.deepEqual(subpath(data), first, `${shape} subpath must use the same pure implementation`);
    actual[shape] = {
      svg_sha256: await sha256(first.svg),
      svg_bytes: new TextEncoder().encode(first.svg).byteLength,
      hit_targets: first.interaction.hit_targets.length,
    };
  }
  assert.deepEqual(actual, golden);
});

test("Diagram covers composed geometry, routing, labels, decorations, support, clipping, and diagnostics", () => {
  const surface = renderDiagramVisualSurface(diagram(), {
    viewport: { x: 5, y: 5, width: 180, height: 100 },
    label_overflow: { mode: "ellipsis", max_scalars: 8 },
  });
  for (const marker of [
    "ld-container",
    "ld-occurrence",
    "ld-port",
    "ld-edge",
    "ld-overlay",
    "ld-badge",
    "ld-support",
    'data-support-kind="hidden_relation"',
    'data-child-keys="occurrence:child"',
    'data-anchor-kind="edge_path"',
    'data-target-kind="container"',
    "Contain…",
    "clip-path",
  ])
    assert.match(surface.svg, new RegExp(marker));
  assert.match(surface.svg, /Container &amp; owner/);
  assert.deepEqual(surface.viewport, { x: 5, y: 5, width: 180, height: 100 });
  assert.deepEqual(surface.diagnostics, diagram().diagnostics);
});

test("Tree preserves roots, depth, parents, and visibly distinct duplicate and cycle references", () => {
  const surface = renderTreeVisualSurface(tree());
  assert.match(surface.svg, /ld-tree-root/);
  assert.match(surface.svg, /data-depth="0"/);
  assert.match(surface.svg, /data-depth="2"/);
  assert.match(surface.svg, /data-parent-key="tree:child"/);
  assert.match(surface.svg, /ld-tree-duplicate/);
  assert.match(surface.svg, /ld-tree-cycle/);
  assert.match(surface.svg, /data-from-occurrence-key="tree:duplicate"/);
  assert.match(surface.svg, />duplicate</);
  assert.match(surface.svg, />cycle</);
  assert.equal(
    surface.interaction.hit_targets.find((target) => target.render_key === "tree:root")
      .primitive_kind,
    "tree.root"
  );
});

test("Flow preserves lanes, ordered steps, junctions, connector kinds, labels, routes, and loops", () => {
  const surface = renderFlowVisualSurface(flow());
  for (const marker of [
    "ld-lane",
    "ld-step",
    "ld-branch",
    "ld-join",
    "ld-flow-cycle",
    "ld-flow-control",
    "ld-flow-data",
    "ld-flow-error",
    "ld-flow-message",
    "ld-flow-sequence",
    'data-connector-kind="sequence"',
    "textPath",
  ])
    assert.match(surface.svg, new RegExp(marker));
  assert.match(surface.svg, /data-lane-key="lane:one"/);
  assert.match(surface.svg, /data-from-step-key="step:start"/);
  assert.match(surface.svg, /message &lt;route&gt;/);
});

test("interaction metadata has stable identities, accessible labels, focus order, and reversible bindings", () => {
  for (const [data, render] of [
    [diagram(), renderDiagramVisualSurface],
    [tree(), renderTreeVisualSurface],
    [flow(), renderFlowVisualSurface],
  ]) {
    const surface = render(data);
    assert.deepEqual(
      surface.interaction.focus_order,
      surface.interaction.hit_targets.map((target) => target.hit_target_id)
    );
    assert.deepEqual(
      surface.interaction.hit_targets.map((target) => target.focus_order),
      surface.interaction.hit_targets.map((_, index) => index)
    );
    for (const target of surface.interaction.hit_targets) {
      assert.equal(target.hit_target_id, `hit:${target.render_key}`);
      assert.ok(target.accessibility_label.length > 0);
      assert.equal(target.source_binding.render_key, target.render_key);
      assert.equal(target.source_binding.viewdata_key, viewdataKey);
      assert.deepEqual(target.source_binding.source_refs, sourceRefs);
      assert.match(surface.svg, new RegExp(`data-hit-target-id="hit:${target.render_key}"`));
    }
  }
});

test("clipping and scalar overflow use the same deterministic policy for every graph shape", () => {
  for (const [data, render] of [
    [diagram(), renderDiagramVisualSurface],
    [tree(), renderTreeVisualSurface],
    [flow(), renderFlowVisualSurface],
  ]) {
    const surface = render(data, {
      viewport: { x: 2, y: 3, width: 40, height: 50 },
      label_overflow: { mode: "ellipsis", max_scalars: 3 },
    });
    assert.match(surface.svg, /viewBox="2 3 40 50"/);
    assert.match(surface.svg, /clip-path="url\(#ld-surface-clip-/);
    assert.match(surface.svg, /…/);
  }
});

test("all three shapes expose deterministic empty states without fabricating hit targets", () => {
  const emptyCases = [
    base("diagram", {
      containers: [], occurrences: [], ports: [], edge_paths: [], labels: [], overlays: [], badges: [], support_geometry: [],
    }, { bounds: { x: 0, y: 0, width: 0, height: 0 }, diagnostics: [] }),
    base("tree", { occurrences: [], duplicate_refs: [], cycle_refs: [] }, { bounds: { x: 0, y: 0, width: 0, height: 0 }, diagnostics: [] }),
    base("flow", { lanes: [], steps: [], branches: [], joins: [], connectors: [], cycle_refs: [] }, { bounds: { x: 0, y: 0, width: 0, height: 0 }, diagnostics: [] }),
  ];
  const renders = [renderDiagramVisualSurface, renderTreeVisualSurface, renderFlowVisualSurface];
  for (const [index, data] of emptyCases.entries()) {
    const surface = renders[index](data);
    assert.equal(surface.empty, true);
    assert.deepEqual(surface.viewport, { x: 0, y: 0, width: 320, height: 180 });
    assert.deepEqual(surface.interaction.hit_targets, []);
    assert.match(surface.svg, /class="ld-empty"/);
  }
});

test("malformed and adversarial RenderData and options fail with stable errors", () => {
  assert.throws(
    () => renderDiagramVisualSurface({ ...diagram(), extra: true }),
    (error) =>
      error instanceof VisualSurfaceError &&
      error.code === "render.visual_data_invalid"
  );
  assert.throws(
    () => renderTreeVisualSurface({ ...tree(), kind: "flow", shape: "flow" }),
    (error) => error.code === "render.visual_data_invalid"
  );
  assert.throws(
    () => renderFlowVisualSurface(flow(), { unknown: true }),
    (error) => error.code === "render.visual_options_invalid"
  );
  assert.throws(
    () =>
      renderDiagramVisualSurface(diagram(), {
        viewport: { x: 0, y: 0, width: Number.NaN, height: 10 },
      }),
    (error) => error.code === "render.visual_options_invalid"
  );
  assert.throws(
    () =>
      renderDiagramVisualSurface(diagram(), {
        limits: { ...VISUAL_SURFACE_HARD_LIMITS, max_svg_bytes: VISUAL_SURFACE_HARD_LIMITS.max_svg_bytes + 1 },
      }),
    (error) => error.code === "render.visual_options_invalid"
  );
});

test("max_extent validates every primitive bounds endpoint, route point, and junction position", () => {
  const diagramData = diagram();
  diagramData.occurrences[0].bounds = {
    x: 999,
    y: 0,
    width: 2,
    height: 1,
  };
  const treeData = tree();
  treeData.duplicate_refs[0].points[1] = { x: 1001, y: 0 };
  const flowData = flow();
  flowData.branches[0].position = { x: 0, y: -1001 };
  for (const [data, render] of [
    [diagramData, renderDiagramVisualSurface],
    [treeData, renderTreeVisualSurface],
    [flowData, renderFlowVisualSurface],
  ])
    assert.throws(
      () =>
        render(data, {
          limits: { ...DEFAULT_VISUAL_SURFACE_LIMITS, max_extent: 1000 },
        }),
      (error) =>
        error instanceof VisualSurfaceError &&
        error.code === "render.visual_resource_limit" &&
        /max_extent/.test(error.message)
    );
});

test("emitted-string budget rejects giant render keys, key lists, and root accessibility labels before assembly", () => {
  const giantKeyData = diagram();
  const support = giantKeyData.support_geometry[0];
  const supportBinding = giantKeyData.source_bindings.find(
    (binding) => binding.render_key === support.render_key
  );
  support.render_key = `support:${"x".repeat(1000)}`;
  supportBinding.render_key = support.render_key;

  const childKeys = Array.from({ length: 40 }, (_, index) => `child:${index}`);
  const giantKeyListData = base("diagram", {
    containers: [
      {
        render_key: "container:key-list",
        bounds: { x: 0, y: 0, width: 10, height: 10 },
        child_keys: childKeys,
      },
    ],
    occurrences: childKeys.map((render_key) => ({
      render_key,
      bounds: { x: 0, y: 0, width: 1, height: 1 },
      port_keys: [],
    })),
    ports: [],
    edge_paths: [],
    labels: [],
    overlays: [],
    badges: [],
    support_geometry: [],
  });
  const limited = {
    ...DEFAULT_VISUAL_SURFACE_LIMITS,
    max_text_scalars: 100,
  };
  for (const invoke of [
    () => renderDiagramVisualSurface(giantKeyData, { limits: limited }),
    () => renderDiagramVisualSurface(giantKeyListData, { limits: limited }),
    () =>
      renderDiagramVisualSurface(diagram(), {
        accessibility_label: "a".repeat(1000),
        limits: limited,
      }),
  ])
    assert.throws(
      invoke,
      (error) =>
        error instanceof VisualSurfaceError &&
        error.code === "render.visual_resource_limit" &&
        /max_text_scalars/.test(error.message)
    );
  assert.throws(
    () =>
      renderDiagramVisualSurface(diagram(), {
        accessibility_label: "a".repeat(10_000),
        limits: {
          ...DEFAULT_VISUAL_SURFACE_LIMITS,
          max_text_scalars: 20_000,
          max_svg_bytes: 5_000,
        },
      }),
    (error) =>
      error instanceof VisualSurfaceError &&
      error.code === "render.visual_resource_limit" &&
      /max_svg_bytes/.test(error.message)
  );
});

test("explicit primitive, route, text, byte, and extent limits fail closed", () => {
  for (const [data, render] of [
    [diagram(), renderDiagramVisualSurface],
    [tree(), renderTreeVisualSurface],
    [flow(), renderFlowVisualSurface],
  ])
    assert.throws(
      () =>
        render(data, {
          limits: { ...DEFAULT_VISUAL_SURFACE_LIMITS, max_primitives: 1 },
        }),
      (error) =>
        error instanceof VisualSurfaceError &&
        error.code === "render.visual_resource_limit"
    );
  const cases = [
    { max_route_points: 1 },
    { max_text_scalars: 1 },
    { max_svg_bytes: 100 },
    { max_extent: 10 },
  ];
  for (const extension of cases)
    assert.throws(
      () =>
        renderFlowVisualSurface(flow(), {
          limits: { ...DEFAULT_VISUAL_SURFACE_LIMITS, ...extension },
        }),
      (error) =>
        error instanceof VisualSurfaceError &&
        error.code === "render.visual_resource_limit"
    );
});

test("shape entrypoints stay browser-neutral and expose documented adapters", async () => {
  const manifest = JSON.parse(
    await readFile(new URL("../package.json", import.meta.url), "utf8")
  );
  assert.deepEqual(Object.keys(manifest.exports), [".", "./diagram", "./tree", "./flow", "./table", "./matrix", "./context", "./diff"]);
  for (const file of ["../src/visual.ts", "../src/structured.ts", "../src/diagram.ts", "../src/tree.ts", "../src/flow.ts", "../src/table.ts", "../src/matrix.ts", "../src/context.ts", "../src/diff.ts"]) {
    const source = await readFile(new URL(file, import.meta.url), "utf8");
    assert.doesNotMatch(source, /node:|document\.|window\.|HTMLElement|React/);
  }
});
