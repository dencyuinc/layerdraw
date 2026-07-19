// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import {
  createViewer,
  ViewerContractError,
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
const renderLimits = {
  max_primitives: 10000,
  max_route_points: 10000,
  max_text_length: 4096,
  max_depth: 128,
  max_extent: 1_000_000,
};
const viewerLimits = {
  max_queued_updates: 4,
  max_snapshot_bytes: 5_000_000,
  max_snapshot_items: 100_000,
  max_render_items: 100_000,
  max_retained_bytes: 10_000_000,
  max_presentation_references: 1000,
  max_display_preferences: 32,
  max_display_preference_bytes: 4096,
  max_event_deliveries: 4,
  min_zoom: 0.1,
  max_zoom: 8,
  max_pan: 10_000,
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
const views = Object.fromEntries(
  Object.entries(corpusNames).map(([shape, name]) => [
    shape,
    corpus.cases.find((candidate) => candidate.name === name).expected
      .normalized_response.payload.view_data,
  ])
);

const handshake = JSON.parse(
  await readFile(
    new URL(
      "../../../schemas/fixtures/conformance/engine/handshake-success.json",
      import.meta.url
    ),
    "utf8"
  )
);
const manifest = handshake.payload.capability_manifest;
manifest.renderer_profiles = [
  { enabled: true, id: "layerdraw/default", version: "1.0" },
];

function recipe(shape, extension = {}) {
  return {
    render_recipe_schema_version: 1,
    renderer_profile: rendererProfile,
    shape: { kind: shape },
    layout_algorithm: ["diagram", "tree", "flow"].includes(shape)
      ? { kind: "layered", crossing_reduction: "median", rank_separation: 64 }
      : { kind: "native" },
    layout_seed: { value: "viewer-fixture" },
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

function recipes(extensionByShape = {}) {
  return Object.fromEntries(
    Object.keys(corpusNames).map((shape) => [
      shape,
      recipe(shape, extensionByShape[shape]),
    ])
  );
}

function options(extension = {}) {
  return {
    renderer_profile: structuredClone(resolvedProfile),
    render_recipes: recipes(),
    asset_resolver: { async resolve() { return []; } },
    font_resolver: { async resolve() { return [structuredClone(font)]; } },
    capability_manifest: structuredClone(manifest),
    layout: structuredClone(layout),
    render_limits: structuredClone(renderLimits),
    viewer_limits: structuredClone(viewerLimits),
    ...extension,
  };
}

function revised(view, id, definitionHash = digestB) {
  const result = structuredClone(view);
  if (result.revision.kind === "single") {
    result.revision.revision_id = id;
    result.revision.definition_hash = definitionHash;
  } else {
    result.revision.recipe_revision_id = id;
    result.revision.recipe_definition_hash = definitionHash;
  }
  return result;
}

function snapshot(view, extension = {}) {
  return {
    viewer_snapshot_schema_version: 1,
    sequence: 0,
    complete: true,
    view_address: view.view_address,
    revision: structuredClone(view.revision),
    view_data_hash: digest,
    state_input: structuredClone(view.state_input),
    view_data: structuredClone(view),
    ...extension,
  };
}

function update(current, next, sequence = 1, extension = {}) {
  return {
    viewer_update_schema_version: 1,
    sequence,
    previous_sequence: sequence - 1,
    complete: true,
    view_address: next.view_address,
    previous_revision: structuredClone(current.revision),
    revision: structuredClone(next.revision),
    previous_view_data_hash: digest,
    view_data_hash: digestB,
    previous_state_input: structuredClone(current.state_input),
    state_input: structuredClone(next.state_input),
    view_data: structuredClone(next),
    ...extension,
  };
}

async function opened(shape = "diagram", viewerOptions = options()) {
  const viewer = createViewer(viewerOptions);
  const result = await viewer.setViewData(snapshot(views[shape]));
  assert.equal(result.ok, true, JSON.stringify(result));
  return viewer;
}

test("all seven authoritative ViewData shapes open deterministically through Render only", async () => {
  for (const shape of Object.keys(corpusNames)) {
    const events = [];
    const viewer = createViewer(options({ event_sink: { emit(event) { events.push(event); } } }));
    const input = snapshot(views[shape]);
    const first = await viewer.setViewData(input);
    assert.equal(first.ok, true, `${shape}: ${JSON.stringify(first)}`);
    assert.equal(first.state.status, "ready");
    assert.equal(first.state.publication.render_data.kind, shape);
    assert.equal(first.state.publication.view_data_hash, digest);
    assert.ok(events.some((event) => event.state.status === "loading"));
    const rendered = structuredClone(first.state.publication.render_data);
    const second = await viewer.setViewData(input);
    assert.equal(second.ok, true);
    assert.deepEqual(second.state.publication.render_data, rendered);
    await viewer.dispose();
  }
});

test("snapshot envelopes are closed, validated, bounded, and never partially published", async () => {
  const viewer = createViewer(options());
  const malformed = snapshot(views.diagram);
  malformed.unknown = true;
  const rejected = await viewer.setViewData(malformed);
  assert.equal(rejected.ok, false);
  assert.equal(rejected.error.code, "viewer.input_invalid");
  assert.equal(rejected.state.status, "recoverable_error");
  assert.equal(viewer.getPublication(), undefined);

  const mismatch = snapshot(views.diagram, { view_address: views.table.view_address });
  assert.equal((await viewer.setViewData(mismatch)).error.code, "viewer.update_mismatch");

  const tiny = createViewer(options({ viewer_limits: { ...viewerLimits, max_snapshot_bytes: 10 } }));
  const tinyResult = await tiny.setViewData(snapshot(views.diagram));
  assert.equal(tinyResult.error.code, "viewer.resource_limit");
  assert.equal(tinyResult.state.status, "recoverable_error");
  const noItems = createViewer(options({ viewer_limits: { ...viewerLimits, max_snapshot_items: 1 } }));
  assert.equal((await noItems.setViewData(snapshot(views.diagram))).error.code, "viewer.resource_limit");
});

test("snapshot and update materializations share bounded in-flight admission", async () => {
  let started;
  const began = new Promise((resolve) => { started = resolve; });
  const viewer = createViewer(options({
    viewer_limits: { ...viewerLimits, max_queued_updates: 1 },
    font_resolver: {
      resolve() {
        started();
        return new Promise(() => {});
      },
    },
  }));
  const opening = viewer.setViewData(snapshot(views.diagram));
  await began;
  const rejected = await Promise.all(
    Array.from({ length: 20 }, () =>
      viewer.setViewData(snapshot(views.diagram))
    )
  );
  assert.ok(
    rejected.every(
      (result) =>
        !result.ok &&
        result.error.code === "viewer.resource_limit" &&
        result.state.status === "recoverable_error"
    )
  );
  await viewer.cancel();
  assert.equal((await opening).outcome, "cancelled");
});

test("ordered replacements accept one next sequence and identical duplicates only", async () => {
  const current = views.diagram;
  const next = revised(current, "revision-2");
  const viewer = await opened();
  const replacement = update(current, next);
  const applied = await viewer.applyViewDataUpdate(replacement);
  assert.equal(applied.ok, true);
  assert.equal(applied.state.publication.sequence, 1);
  assert.equal((await viewer.applyViewDataUpdate(replacement)).outcome, "duplicate");

  const conflict = structuredClone(replacement);
  conflict.complete = false;
  assert.equal((await viewer.applyViewDataUpdate(conflict)).error.code, "viewer.update_conflict");
  assert.equal(viewer.getPublication().sequence, 1);
  assert.equal((await viewer.applyViewDataUpdate(replacement)).state.status, "ready");

  const stale = update(current, next, 0, { previous_sequence: 0 });
  assert.equal((await viewer.applyViewDataUpdate(stale)).error.code, "viewer.update_stale");
  const gap = update(next, revised(next, "revision-4"), 3, { previous_sequence: 1, previous_view_data_hash: digestB });
  assert.equal((await viewer.applyViewDataUpdate(gap)).error.code, "viewer.update_gap");
});

test("address, revision, hash, state, replacement envelope, and no-snapshot mismatches reject without adoption", async () => {
  const current = views.diagram;
  const next = revised(current, "revision-2");
  const changes = [
    { view_address: views.table.view_address },
    { previous_revision: next.revision },
    { previous_view_data_hash: digestB },
    { previous_state_input: { kind: "snapshot", snapshot_hash: digest, state_version: "1", captured_at: "2026-01-01T00:00:00Z", definition_hash: digest } },
  ];
  for (const change of changes) {
    const viewer = await opened();
    const result = await viewer.applyViewDataUpdate(update(current, next, 1, change));
    assert.equal(result.error.code, "viewer.update_mismatch");
    assert.equal(viewer.getPublication().sequence, 0);
  }
  const noSnapshot = createViewer(options());
  assert.equal((await noSnapshot.applyViewDataUpdate(update(current, next))).error.code, "viewer.update_mismatch");

  const badReplacement = update(current, next, 1, { revision: current.revision });
  const viewer = await opened();
  const identityFailure = await viewer.applyViewDataUpdate(badReplacement);
  assert.equal(identityFailure.error.code, "viewer.update_mismatch");
  assert.equal(identityFailure.state.status, "stale_update");

  const malformed = update(current, next);
  malformed.unknown = true;
  const malformedViewer = await opened();
  const malformedResult = await malformedViewer.applyViewDataUpdate(malformed);
  assert.equal(malformedResult.error.code, "viewer.input_invalid");
  assert.equal(malformedResult.state.status, "recoverable_error");

  const emptyContext = structuredClone(views.context);
  emptyContext.context.groups = [];
  const boundedViewer = createViewer(
    options({ viewer_limits: { ...viewerLimits, max_snapshot_items: 1 } })
  );
  assert.equal(
    (await boundedViewer.setViewData(snapshot(emptyContext))).ok,
    true
  );
  const largeReplacement = revised(views.diagram, "large-revision");
  largeReplacement.view_address = emptyContext.view_address;
  const boundedUpdate = update(emptyContext, largeReplacement);
  const boundedResult = await boundedViewer.applyViewDataUpdate(boundedUpdate);
  assert.equal(boundedResult.error.code, "viewer.resource_limit");
  assert.equal(boundedResult.state.status, "recoverable_error");
});

test("presentation state is immutable, clamped, preserved compatibly, and cleared deterministically", async () => {
  const current = views.diagram;
  const next = revised(current, "revision-2");
  const viewer = await opened();
  const keys = viewer.getPublication().render_data.source_bindings.map((binding) => binding.render_key);
  const first = keys[0];
  const second = keys[1];
  const state = viewer.updatePresentation({
    selection_keys: [first],
    hover_key: first,
    focus_key: second,
    zoom: 100,
    pan: { x: -50_000, y: 50_000 },
    expanded_keys: [first],
    sorting: [{ render_key: second, direction: "descending" }],
    display_preferences: { labels: "compact", opacity: 0.5 },
  });
  assert.equal(state.zoom, 8);
  assert.deepEqual(state.pan, { x: -10_000, y: 10_000 });
  state.selection_keys.push?.("host-mutation");
  assert.deepEqual(viewer.getPublication().presentation.selection_keys, [first]);
  assert.deepEqual(viewer.setSelection(["not-a-render-key"]).selection_keys, []);
  viewer.setSelection([first]);

  await viewer.applyViewDataUpdate(update(current, next));
  assert.deepEqual(viewer.getPublication().presentation.selection_keys, [first]);
  assert.equal(viewer.getPublication().presentation.focus_key, second);
  assert.deepEqual(viewer.getPublication().presentation.display_preferences, { labels: "compact", opacity: 0.5 });

  const incompatible = revised(views.table, "revision-3");
  incompatible.view_address = next.view_address;
  const switched = update(next, incompatible, 2, {
    previous_sequence: 1,
    previous_view_data_hash: digestB,
    view_data_hash: digest,
  });
  await viewer.applyViewDataUpdate(switched);
  const presentation = viewer.getPublication().presentation;
  assert.deepEqual(presentation.selection_keys, []);
  assert.equal(presentation.hover_key, undefined);
  assert.equal(presentation.focus_key, undefined);
  assert.deepEqual(presentation.expanded_keys, []);
  assert.deepEqual(presentation.sorting, []);
  assert.equal(presentation.zoom, 8);
});

test("exact inspection returns the supplied item and binding without inference", async () => {
  const viewer = await opened("context");
  const publication = viewer.getPublication();
  const binding = publication.render_data.source_bindings.find((candidate) => candidate.viewdata_key);
  const inspection = viewer.inspectSource(binding.render_key);
  assert.equal(inspection.viewdata_key, binding.viewdata_key);
  assert.deepEqual(inspection.source_binding, binding);
  assert.equal(inspection.view_data_item.key, binding.viewdata_key);
  assert.deepEqual(inspection.view_data_item.source, binding.source_refs);
  inspection.view_data_item.label = "mutated";
  assert.notEqual(viewer.inspectSource(binding.render_key).view_data_item.label, "mutated");
  assert.equal(viewer.inspectSource("missing"), undefined);
});

test("partial-stream, empty, unsupported profile, missing resources, fatal, and recovery states are explicit", async () => {
  const partial = createViewer(options());
  assert.equal((await partial.setViewData(snapshot(views.diagram, { complete: false }))).state.status, "partial_stream");

  const emptyView = structuredClone(views.context);
  emptyView.context.groups = [];
  const empty = createViewer(options());
  assert.equal((await empty.setViewData(snapshot(emptyView))).state.status, "empty");
  assert.equal(empty.getState().reason, "view_empty");

  const profile = structuredClone(resolvedProfile);
  profile.supported_shapes = profile.supported_shapes.filter((shape) => shape !== "diagram");
  const unsupported = createViewer(options({ renderer_profile: profile }));
  assert.equal((await unsupported.setViewData(snapshot(views.diagram))).state.status, "unsupported_profile");

  let fontAvailable = false;
  const recoverableFont = createViewer(options({
    font_resolver: { async resolve() { return fontAvailable ? [font] : [{ ...font, family: "Other", digest: digestB }]; } },
  }));
  assert.equal((await recoverableFont.setViewData(snapshot(views.diagram))).state.status, "missing_font");
  fontAvailable = true;
  assert.equal((await recoverableFont.setViewData(snapshot(views.diagram))).state.status, "ready");

  const assetRecipes = recipes({ diagram: { asset_policy: { mode: "resolved_only", required_digests: [digestB] } } });
  let assetAvailable = false;
  const recoverableAsset = createViewer(options({
    render_recipes: assetRecipes,
    asset_resolver: { async resolve() { return assetAvailable ? [{ digest: digestB, width: 10, height: 10 }] : []; } },
  }));
  assert.equal((await recoverableAsset.setViewData(snapshot(views.diagram))).state.status, "missing_asset");
  assetAvailable = true;
  assert.equal((await recoverableAsset.setViewData(snapshot(views.diagram))).state.status, "ready");

  const fatal = createViewer(options({ render_limits: { ...renderLimits, max_extent: 1 } }));
  const fatalResult = await fatal.setViewData(snapshot(views.diagram));
  assert.equal(fatalResult.state.status, "recoverable_error");
  assert.equal(fatalResult.error.code, "viewer.resource_limit");

  const invalidLayout = createViewer(options({ layout: { ...layout, item_width: -1 } }));
  const fatalConfiguration = await invalidLayout.setViewData(snapshot(views.diagram));
  assert.equal(fatalConfiguration.state.status, "fatal");
  assert.equal(fatalConfiguration.error.recoverable, false);
});

test("resolver rejection, malformed resolver output, and throwing event sinks cannot corrupt publication", async () => {
  const rejected = createViewer(options({
    font_resolver: { async resolve() { throw new Error("secret host detail"); } },
  }));
  const failure = await rejected.setViewData(snapshot(views.diagram));
  assert.equal(failure.state.status, "recoverable_error");
  assert.deepEqual(failure.error.details, { reason: "Error" });
  assert.equal(rejected.getPublication(), undefined);

  const malformed = createViewer(options({
    asset_resolver: { async resolve() { return null; } },
  }));
  assert.equal((await malformed.setViewData(snapshot(views.diagram))).error.code, "viewer.resolver_failed");

  let viewer;
  let calls = 0;
  viewer = createViewer(options({
    event_sink: {
      emit(event) {
        calls += 1;
        const wasReady = event.state.status === "ready";
        event.state.status = "host-mutated";
        if (wasReady) viewer.setSelection([]);
        throw new Error("host callback");
      },
    },
  }));
  assert.equal((await viewer.setViewData(snapshot(views.diagram))).ok, true);
  assert.equal(viewer.getState().status, "ready");
  assert.ok(calls >= 2 && calls <= viewerLimits.max_event_deliveries * 3);
});

test("hostile presentation values always fail through ViewerContractError", async () => {
  const viewer = await opened();
  const cyclic = {};
  cyclic.self = cyclic;
  const hostile = [
    null,
    undefined,
    [],
    { sorting: null },
    { sorting: [null] },
    { sorting: [{ render_key: "x", direction: "sideways" }] },
    { pan: null },
    { pan: { x: 0 } },
    { pan: { x: 0, y: 0, z: 0 } },
    { pan: () => {} },
    { selection_keys: null },
    { expanded_keys: {} },
    { hover_key: 42 },
    { focus_key: {} },
    { zoom: "1" },
    { zoom: null },
    { display_preferences: null },
    { display_preferences: { invalid_number: Number.NaN } },
    { display_preferences: cyclic },
  ];
  for (const value of hostile) {
    assert.throws(
      () => viewer.updatePresentation(value),
      (error) =>
        error instanceof ViewerContractError &&
        ["viewer.input_invalid", "viewer.resource_limit"].includes(error.code),
      JSON.stringify(Object.keys(value ?? {}))
    );
  }
});

test("createViewer snapshots deterministic configuration and callback identities", async () => {
  const events = [];
  const configured = options({
    event_sink: { emit(event) { events.push(event.state.status); } },
  });
  const viewer = createViewer(configured);

  configured.renderer_profile.supported_shapes.length = 0;
  configured.render_recipes.diagram.asset_policy.required_digests.push(digestB);
  configured.capability_manifest.renderer_profiles[0].enabled = false;
  configured.layout.item_width = -1;
  configured.render_limits.max_primitives = 1;
  configured.viewer_limits.max_render_items = 1;
  configured.asset_resolver.resolve = async () => { throw new Error("mutated asset resolver"); };
  configured.font_resolver.resolve = async () => { throw new Error("mutated font resolver"); };
  configured.event_sink.emit = () => { throw new Error("mutated event sink"); };

  const result = await viewer.setViewData(snapshot(views.diagram));
  assert.equal(result.ok, true, JSON.stringify(result));
  assert.equal(result.state.status, "ready");
  assert.ok(events.includes("loading") && events.includes("ready"));
});

test("cancellation aborts resolvers even when the host ignores AbortSignal and teardown is idempotent", async () => {
  let started;
  const began = new Promise((resolve) => { started = resolve; });
  const viewer = createViewer(options({
    font_resolver: { resolve() { started(); return new Promise(() => {}); } },
  }));
  const opening = viewer.setViewData(snapshot(views.diagram));
  await began;
  const cancelling = viewer.cancel();
  assert.equal(viewer.getState().status, "cancelling");
  assert.equal((await opening).outcome, "cancelled");
  await cancelling;
  assert.equal(viewer.getState().status, "empty");
  await viewer.dispose();
  await viewer.dispose();
  assert.equal(viewer.getState().status, "disposed");
  assert.equal((await viewer.setViewData(snapshot(views.diagram))).error.code, "viewer.disposed");
  assert.throws(() => viewer.setSelection([]), ViewerContractError);
});

test("queued update admission, render items, retained bytes, and presentation resources are bounded", async () => {
  const current = views.diagram;
  const next = revised(current, "revision-2");
  let hold;
  const held = new Promise((resolve) => { hold = resolve; });
  let stall = false;
  const viewer = await opened("diagram", options({
    viewer_limits: { ...viewerLimits, max_queued_updates: 1 },
    font_resolver: { async resolve() { if (stall) await held; return [font]; } },
  }));
  stall = true;
  const first = viewer.applyViewDataUpdate(update(current, next));
  const overflow = await viewer.applyViewDataUpdate(update(next, revised(next, "revision-3"), 2, { previous_view_data_hash: digestB }));
  assert.equal(overflow.error.code, "viewer.resource_limit");
  hold();
  assert.equal((await first).ok, true);

  const renderBound = createViewer(options({ viewer_limits: { ...viewerLimits, max_render_items: 1 } }));
  assert.equal((await renderBound.setViewData(snapshot(views.diagram))).error.code, "viewer.resource_limit");
  const retainedBound = createViewer(options({ viewer_limits: { ...viewerLimits, max_retained_bytes: 10 } }));
  assert.equal((await retainedBound.setViewData(snapshot(views.diagram))).error.code, "viewer.resource_limit");

  const presentationBound = await opened("diagram", options({
    viewer_limits: { ...viewerLimits, max_presentation_references: 1, max_display_preferences: 1, max_display_preference_bytes: 10 },
  }));
  assert.throws(() => presentationBound.updatePresentation({ selection_keys: ["a", "b"] }), (error) => error.code === "viewer.resource_limit");
  assert.throws(() => presentationBound.updatePresentation({ display_preferences: { a: true, b: false } }), (error) => error.code === "viewer.resource_limit");
  assert.throws(() => presentationBound.updatePresentation({ display_preferences: { a: "a value longer than ten bytes" } }), (error) => error.code === "viewer.resource_limit");
  assert.throws(() => presentationBound.updatePresentation({ unknown: true }), (error) => error.code === "viewer.input_invalid");
  assert.throws(() => presentationBound.updatePresentation({ sorting: [{ render_key: "a", direction: "ascending", extra: true }] }), (error) => error.code === "viewer.input_invalid");
});

test("factory rejects invalid capability, profile, recipes, resolvers, event sinks, and limits", () => {
  const cases = [];
  const disabled = options();
  disabled.capability_manifest.renderer_profiles[0] = { enabled: false, id: "layerdraw/default", version: "1.0", unavailable_reason: "unsupported" };
  cases.push(disabled);
  const invalidRecipe = options();
  invalidRecipe.render_recipes.diagram.shape.kind = "tree";
  cases.push(invalidRecipe);
  cases.push(options({ asset_resolver: {} }));
  cases.push(options({ event_sink: {} }));
  cases.push(options({ viewer_limits: { ...viewerLimits, max_zoom: 0 } }));
  cases.push(options({ viewer_limits: { ...viewerLimits, min_zoom: 9, max_zoom: 8 } }));
  for (const value of cases)
    assert.throws(() => createViewer(value), ViewerContractError);
});

test("public results are defensive copies and browser-neutral inputs remain repeatable", async () => {
  const input = snapshot(views.matrix);
  const viewer = createViewer(options());
  await viewer.setViewData(input);
  input.view_data.matrix.cells.length = 0;
  const first = viewer.getPublication();
  const second = viewer.getPublication();
  assert.deepEqual(first, second);
  first.render_data.source_bindings.length = 0;
  first.presentation.pan.x = 99;
  assert.notEqual(viewer.getPublication().render_data.source_bindings.length, 0);
  assert.deepEqual(viewer.getPublication().presentation.pan, { x: 0, y: 0 });
});
