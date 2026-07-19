// SPDX-License-Identifier: Apache-2.0

const encoder = new TextEncoder();
const sha256 = async (bytes) => {
  const hash = new Uint8Array(await crypto.subtle.digest("SHA-256", bytes));
  return `sha256:${Array.from(hash, (byte) => byte.toString(16).padStart(2, "0")).join("")}`;
};
const canonical = (value) => {
  if (value === null || typeof value !== "object") return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
  return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${canonical(value[key])}`).join(",")}}`;
};
const semanticHash = (domain, value) =>
  sha256(encoder.encode(`layerdraw-language-1\0${domain}\0${canonical(value)}`));
const artifactDigest = (bytes) => sha256(bytes);
const stableView = (view) => {
  const value = structuredClone(view);
  const stripMessages = (item) => {
    if (Array.isArray(item)) item.forEach(stripMessages);
    else if (item && typeof item === "object") {
      delete item.message;
      Object.values(item).forEach(stripMessages);
    }
  };
  stripMessages(value.diagnostics);
  return value;
};

const corpusNames = {
  diagram: "composed_diagram",
  table: "table_automatic",
  matrix: "matrix",
  tree: "tree",
  flow: "flow",
  context: "context",
  diff: "definition_diff",
};
const pngBytes = Uint8Array.from(
  atob("iVBORw0KGgoAAAANSUhEUgAAAEAAAAAgCAYAAACinX6EAAAAHklEQVR4nO3BAQ0AAADCoPdPbQ8HFAAAAAAAAADwbiAgAAFXlYP5AAAAAElFTkSuQmCC"),
  (character) => character.charCodeAt(0)
);

function extendRepresentativeViews(corpus) {
  const views = Object.fromEntries(
    Object.entries(corpusNames).map(([shape, name]) => {
      const entry = corpus.cases.find((candidate) => candidate.name === name);
      if (entry?.expected?.normalized_response?.payload?.view_data === undefined)
        throw new Error(`missing authoritative ViewData case ${name}`);
      return [shape, structuredClone(entry.expected.normalized_response.payload.view_data)];
    })
  );
  const root = views.tree.tree.roots[0];
  const child = root.children[0];
  views.tree.tree.link_refs.push({
    key: `vdi:tree-link:${"L".repeat(43)}`,
    from_occurrence_key: root.key,
    to_entity_address: child.entity_address,
    relation_address: child.via_relation_address,
    source: child.source,
  });
  views.tree.tree.cycle_refs.push({
    key: `vdi:tree-cycle:${"C".repeat(43)}`,
    from_occurrence_key: child.key,
    to_entity_address: root.entity_address,
    relation_address: child.via_relation_address,
    source: child.source,
  });
  const connector = views.flow.flow.connectors[0];
  views.flow.flow.steps[0].branch = true;
  views.flow.flow.steps[1].join = true;
  views.flow.flow.cycle_refs.push({
    ...structuredClone(connector),
    key: `vdi:flow-cycle:${"Y".repeat(43)}`,
    connector_key: `vdi:flow-connector:${"Y".repeat(43)}`,
    from_step_key: connector.to_step_key,
    to_step_key: connector.from_step_key,
  });
  return views;
}

function recipe(shape, profile, fontDigest) {
  return {
    render_recipe_schema_version: 1,
    renderer_profile: profile.renderer_profile,
    shape: { kind: shape },
    layout_algorithm: ["diagram", "tree", "flow"].includes(shape)
      ? { kind: "layered", crossing_reduction: "median", rank_separation: 64 }
      : { kind: "native" },
    layout_seed: { value: "render-pipeline-conformance-v1" },
    density: { value: "comfortable" },
    orientation: { value: "top_to_bottom" },
    theme: {
      theme_id: "conformance",
      theme_version: "1",
      specification_digest: profile.renderer_profile.specification_digest,
      color_scheme: "light",
    },
    locale: { language_tag: "en-US" },
    timezone: { iana_name: "UTC" },
    font_policy: { families: ["Conformance Sans"], fallback: "forbid", required_digests: [fontDigest] },
    asset_policy: { mode: "resolved_only", required_digests: [] },
  };
}

const layout = {
  item_order: "viewdata", tie_breaker: "seeded_key_hash", coordinate_precision: 3,
  item_width: 96, item_height: 48, horizontal_gap: 24, vertical_gap: 20,
  container_padding: 12, cell_padding: 6, lane_header_size: 28, badge_size: 14,
  overlay_offset: 3, port_offset: 2, font_size: 14, asset_scale: 1,
};
const renderLimits = {
  max_primitives: 10000, max_route_points: 10000, max_text_length: 4096,
  max_depth: 128, max_extent: 1_000_000,
};
const viewerLimits = {
  max_queued_updates: 4, max_snapshot_bytes: 5_000_000, max_snapshot_items: 100_000,
  max_render_items: 100_000, max_retained_bytes: 10_000_000,
  max_presentation_references: 1000, max_display_preferences: 32,
  max_display_preference_bytes: 4096, max_event_deliveries: 32,
  min_zoom: 0.1, max_zoom: 8, max_pan: 10_000,
};

function viewerOptions(profile, recipes, font, capabilityManifest, extension = {}) {
  const manifest = structuredClone(capabilityManifest);
  manifest.renderer_profiles = [{ enabled: true, id: profile.renderer_profile.profile_id, version: profile.renderer_profile.profile_version }];
  return {
    renderer_profile: structuredClone(profile), render_recipes: structuredClone(recipes),
    asset_resolver: { async resolve() { return []; } },
    font_resolver: { async resolve() { return [structuredClone(font)]; } },
    capability_manifest: manifest, layout: structuredClone(layout),
    render_limits: structuredClone(renderLimits), viewer_limits: structuredClone(viewerLimits),
    ...extension,
  };
}

function snapshot(view, hash, sequence = 0) {
  return {
    viewer_snapshot_schema_version: 1, sequence, complete: true,
    view_address: view.view_address, revision: structuredClone(view.revision),
    view_data_hash: hash, state_input: structuredClone(view.state_input),
    view_data: structuredClone(view),
  };
}

function visualSurface(data, api) {
  const adapters = {
    diagram: api.renderDiagramVisualSurface, table: api.renderTableVisualSurface,
    matrix: api.renderMatrixVisualSurface, tree: api.renderTreeVisualSurface,
    flow: api.renderFlowVisualSurface, context: api.renderContextVisualSurface,
    diff: api.renderDiffVisualSurface,
  };
  return adapters[data.kind](data);
}

function profileFor(base, format) {
  return { ...base, id: `layerdraw/${format}@1`, format };
}

function completePlan(basePlan, view, render, format, viewHash) {
  const plan = structuredClone(basePlan);
  const formatProfile = profileFor(plan.serializer_profile, format);
  const rootRole = format === "json" ? "viewdata" : "visual";
  plan.exporter_profile = formatProfile;
  plan.serializer_profile = formatProfile;
  plan.format = format;
  plan.recipe_address = `${view.view_address}:export:${format}`;
  plan.view_data_hash = viewHash;
  plan.state_policy = view.state_policy;
  plan.state_input = structuredClone(view.state_input);
  plan.required_asset_digests = [];
  plan.required_font_digests = format === "json" ? [] : [...render.resolved_font_digests];
  plan.source_manifest_required = true;
  plan.source_manifest_path = `${view.kind}-${format}.sources.json`;
  plan.artifacts = [{
    logical_path: `${view.kind}.${format}`,
    media_type: format === "json" ? "application/json" : format === "svg" ? "image/svg+xml" : "image/png",
    primary: true, role: rootRole,
  }];
  plan.units = [{ unit_id: `unit:${rootRole}`, kind: "section", order: "0", role: rootRole, artifact_role: rootRole, viewdata_keys: ["viewdata-root"] }];
  plan.representations = [{ viewdata_key: "viewdata-root", artifact_role: rootRole, unit_id: `unit:${rootRole}`, locator: `${rootRole}-root`, disposition: format === "json" ? "embedded" : "rendered", source: view.source }];
  if (format === "json") {
    plan.requires_renderer = false;
    plan.layout_requirement = "none";
    plan.serializer_options = { kind: "json", diagnostics: false, state_summary: false };
  } else {
    const bindings = new Map();
    for (const binding of render.source_bindings) {
      const prior = bindings.get(binding.viewdata_key);
      if (prior !== undefined && canonical(prior) !== canonical(binding.source_refs)) throw new Error("inconsistent RenderData binding");
      bindings.set(binding.viewdata_key, binding.source_refs);
    }
    for (const [key, source] of [...bindings].sort(([left], [right]) => left.localeCompare(right))) {
      plan.representations.push({ viewdata_key: key, artifact_role: rootRole, unit_id: `unit:${rootRole}`, locator: `item-${plan.representations.length}`, disposition: "rendered", source });
      plan.units[0].viewdata_keys.push(key);
    }
    plan.units[0].viewdata_keys.sort();
    plan.requires_renderer = true;
    plan.layout_requirement = "presentation_geometry";
    plan.serializer_options = format === "svg"
      ? { kind: "svg", width: { kind: "auto" }, height: { kind: "auto" }, scale: "1", background: "transparent" }
      : { kind: "png", width: { kind: "value", value: "64" }, height: { kind: "value", value: "32" }, scale: "1", background: "transparent" };
  }
  return plan;
}

function csvPlan(basePlan, view, viewHash) {
  const plan = structuredClone(basePlan);
  const formatProfile = profileFor(plan.serializer_profile, "csv");
  const groups = view.kind === "table"
    ? [["columns", view.table.columns], ["rows", view.table.rows]]
    : [["cells", view.matrix.cells], ["row_axis", view.matrix.row_axis], ["column_axis", view.matrix.column_axis]];
  plan.exporter_profile = formatProfile; plan.serializer_profile = formatProfile; plan.format = "csv";
  plan.recipe_address = `${view.view_address}:export:csv`; plan.view_data_hash = viewHash;
  plan.state_policy = view.state_policy; plan.state_input = structuredClone(view.state_input);
  plan.required_asset_digests = []; plan.required_font_digests = [];
  plan.requires_renderer = false; plan.layout_requirement = "none";
  plan.serializer_options = { kind: "csv", bundle: true, header: true, source_manifest: true };
  plan.source_manifest_required = true; plan.source_manifest_path = `${view.kind}-csv.sources.json`;
  plan.artifacts = groups.map(([role], index) => ({ logical_path: index === 0 ? `${view.kind}.csv` : `${view.kind}.${role}.csv`, media_type: "text/csv", primary: index === 0, role }));
  plan.units = groups.map(([role, items], index) => ({ unit_id: `unit:${role}`, kind: "sheet", order: String(index), role, artifact_role: role, viewdata_keys: items.map((item) => item.key).sort() }));
  plan.representations = groups.flatMap(([role, items]) => items.map((item, index) => ({ viewdata_key: item.key, artifact_role: role, unit_id: `unit:${role}`, locator: `row:${index + 2}`, disposition: "tabular", source: item.source ?? view.source })));
  return plan;
}

async function exportResult(api, environment, basePlan, view, render, format, viewHash, fontResource) {
  const plan = format === "csv" ? csvPlan(basePlan, view, viewHash) : completePlan(basePlan, view, render, format, viewHash);
  const input = {
    export_plan: plan, view_data: structuredClone(view),
    ...(plan.requires_renderer ? { render_data: structuredClone(render) } : {}),
    serializer_profile: { schema_version: 1, ref: plan.serializer_profile },
    assets: [], fonts: plan.required_font_digests.length === 0 ? [] : [fontResource],
    clock: { fixed_rfc3339: "2026-07-19T00:00:00Z" },
  };
  if (format === "png") {
    const rasterizerProfile = { profile_id: "layerdraw/fixed-png", profile_version: "1", specification_digest: plan.serializer_profile.specification_digest };
    const implementation = { api_version: 1, environment, profile: rasterizerProfile, async rasterize(request) { return { bytes: pngBytes, width: request.width, height: request.height, density: request.density, profile: rasterizerProfile }; } };
    input.rasterizer_profile = rasterizerProfile;
    input.rasterizer = api.brandRasterizer(implementation);
  }
  const first = await api.serializeExport(input);
  const second = await api.serializeExport(input);
  if (!first.ok || !second.ok) throw new Error(`${format} export failed: ${JSON.stringify(first)}`);
  const artifacts = await Promise.all(first.artifacts.map(async (artifact) => ({
    path: artifact.entry.logical_path, digest: await artifactDigest(artifact.bytes), bytes: artifact.bytes.byteLength,
  })));
  const repeat = await Promise.all(second.artifacts.map((artifact) => artifactDigest(artifact.bytes)));
  if (canonical(artifacts.map((item) => item.digest)) !== canonical(repeat)) throw new Error(`${format} export was not reproducible`);
  if (first.source_manifest_digest !== second.source_manifest_digest) throw new Error(`${format} Source Manifest was not reproducible`);
  if (
    first.source_manifest === undefined ||
    first.source_manifest.representations.length !== plan.representations.length ||
    first.source_manifest.artifacts.length !== first.artifacts.length ||
    first.source_manifest.artifacts.some((entry, index) => entry.content_digest !== first.artifacts[index]?.entry.content_digest)
  ) throw new Error(`${format} Source Manifest binding was incomplete`);
  return {
    plan_digest: await semanticHash("render-conformance-export-plan", plan), artifacts,
    source_manifest_digest: first.source_manifest_digest,
    source_manifest_artifacts: first.source_manifest?.artifacts.length ?? 0,
    source_manifest_representations: first.source_manifest?.representations.length ?? 0,
  };
}

export async function executeRenderPipelineConformance({ corpus, capabilityManifest, exportFixture, environment, api }) {
  if (environment !== "node" && environment !== "browser") throw new TypeError("environment must be node or browser");
  const fontBytes = encoder.encode("layerdraw-render-conformance-font-v1");
  const fontDigest = await sha256(fontBytes);
  const specificationDigest = await sha256(encoder.encode("layerdraw-render-conformance-profile-v1"));
  const rendererProfile = { profile_id: "layerdraw/conformance", profile_version: "1", specification_digest: specificationDigest };
  const profile = { renderer_profile: rendererProfile, supported_shapes: Object.keys(corpusNames).sort(), supported_algorithms: ["grid", "layered", "native", "radial"] };
  const font = { family: "Conformance Sans", digest: fontDigest, units_per_em: 1000, ascent: 750, descent: 250, line_gap: 100, default_advance: 500, glyph_advances: { " ": 250, i: 250, m: 750 } };
  const fontResource = { digest: fontDigest, bytes: fontBytes };
  const views = extendRepresentativeViews(corpus);
  const stateView = structuredClone(
    corpus.cases.find((candidate) => candidate.name === "state_optional_present")
      ?.expected?.normalized_response?.payload?.view_data
  );
  if (stateView?.state_input?.kind !== "snapshot") throw new Error("stateful ViewData case is missing");
  const recipes = Object.fromEntries(Object.keys(corpusNames).map((shape) => [shape, recipe(shape, profile, fontDigest)]));
  const rendered = {};
  const renderSummary = {};
  const surfaceSummary = {};
  for (const shape of Object.keys(corpusNames)) {
    const viewHash = await semanticHash("export-viewdata", stableView(views[shape]));
    const input = { view_data: views[shape], view_data_hash: viewHash, recipe: recipes[shape], resolved_profile: profile, resolved_fonts: [font], resolved_assets: [], layout, limits: renderLimits };
    const first = await api.materializeRenderData(input);
    const second = await api.materializeRenderData(input);
    if (!first.ok || canonical(first) !== canonical(second)) throw new Error(`${shape} RenderData was not deterministic`);
    rendered[shape] = first.data;
    renderSummary[shape] = { digest: await sha256(encoder.encode(canonical(first.data))), input_hash: first.data.render_input_hash, bounds: first.data.bounds, bindings: first.data.source_bindings.length, diagnostics: first.data.diagnostics.map((item) => item.code) };
    const surface = visualSurface(first.data, api);
    surfaceSummary[shape] = { digest: await sha256(encoder.encode(surface.svg)), items: surface.interaction.hit_targets.length, kind: surface.kind };
  }
  const empty = structuredClone(views.context); empty.context.groups = [];
  const emptyHash = await semanticHash("export-viewdata", stableView(empty));
  const emptyResult = await api.materializeRenderData({ view_data: empty, view_data_hash: emptyHash, recipe: recipes.context, resolved_profile: profile, resolved_fonts: [font], resolved_assets: [], layout, limits: renderLimits });
  if (!emptyResult.ok) throw new Error("empty ViewData did not materialize");

  const transitions = {};
  for (const shape of Object.keys(corpusNames)) {
    const events = [];
    const viewHash = rendered[shape].view_data_hash;
    const viewer = api.createViewer(viewerOptions(profile, recipes, font, capabilityManifest, { event_sink: { emit(event) { events.push(event.state.status); } } }));
    const opened = await viewer.setViewData(snapshot(views[shape], viewHash));
    if (!opened.ok) throw new Error(`${shape} Viewer failed: ${JSON.stringify(opened)}`);
    transitions[shape] = [...events, opened.state.status];
    await viewer.dispose();
  }
  const staleViewer = api.createViewer(viewerOptions(profile, recipes, font, capabilityManifest));
  await staleViewer.setViewData(snapshot(views.diagram, rendered.diagram.view_data_hash));
  const staleView = structuredClone(views.diagram);
  if (staleView.revision.kind === "single") staleView.revision.revision_id = "render-conformance-stale";
  else staleView.revision.recipe_revision_id = "render-conformance-stale";
  const staleHash = await semanticHash("export-viewdata", stableView(staleView));
  const replacement = { viewer_update_schema_version: 1, sequence: 1, previous_sequence: 0, complete: true, view_address: views.diagram.view_address, previous_revision: views.diagram.revision, revision: staleView.revision, previous_view_data_hash: rendered.diagram.view_data_hash, view_data_hash: staleHash, previous_state_input: views.diagram.state_input, state_input: staleView.state_input, view_data: staleView };
  const adopted = await staleViewer.applyViewDataUpdate(replacement);
  if (!adopted.ok) throw new Error(`Viewer replacement failed: ${JSON.stringify(adopted)}`);
  const stale = await staleViewer.applyViewDataUpdate({ ...replacement, sequence: 0 });
  await staleViewer.dispose();

  const json = {};
  for (const shape of Object.keys(corpusNames)) json[shape] = await exportResult(api, environment, exportFixture.export_plan, views[shape], rendered[shape], "json", rendered[shape].view_data_hash, fontResource);
  const exports = {
    json,
    svg: await exportResult(api, environment, exportFixture.export_plan, views.context, rendered.context, "svg", rendered.context.view_data_hash, fontResource),
    png: await exportResult(api, environment, exportFixture.export_plan, views.context, rendered.context, "png", rendered.context.view_data_hash, fontResource),
    csv_table: await exportResult(api, environment, exportFixture.export_plan, views.table, rendered.table, "csv", rendered.table.view_data_hash, fontResource),
    csv_matrix: await exportResult(api, environment, exportFixture.export_plan, views.matrix, rendered.matrix, "csv", rendered.matrix.view_data_hash, fontResource),
  };
  const stateHash = await semanticHash("export-viewdata", stableView(stateView));
  const stateRender = await api.materializeRenderData({ view_data: stateView, view_data_hash: stateHash, recipe: recipes.table, resolved_profile: profile, resolved_fonts: [font], resolved_assets: [], layout, limits: renderLimits });
  if (!stateRender.ok) throw new Error(`stateful ViewData failed: ${JSON.stringify(stateRender)}`);
  const stateViewer = api.createViewer(viewerOptions(profile, recipes, font, capabilityManifest));
  const stateOpened = await stateViewer.setViewData(snapshot(stateView, stateHash));
  if (!stateOpened.ok) throw new Error(`stateful Viewer failed: ${JSON.stringify(stateOpened)}`);
  await stateViewer.dispose();
  const stateExport = await exportResult(api, environment, exportFixture.export_plan, stateView, stateRender.data, "json", stateHash, fontResource);

  const malformed = await api.materializeRenderData({ view_data: { kind: "diagram" }, view_data_hash: rendered.diagram.view_data_hash, recipe: recipes.diagram, resolved_profile: profile, resolved_fonts: [font], resolved_assets: [], layout, limits: renderLimits });
  const missingFont = await api.materializeRenderData({ view_data: views.diagram, view_data_hash: rendered.diagram.view_data_hash, recipe: recipes.diagram, resolved_profile: profile, resolved_fonts: [{ ...font, family: "Unavailable Sans" }], resolved_assets: [], layout, limits: renderLimits });
  const assetRecipe = structuredClone(recipes.diagram); assetRecipe.asset_policy.required_digests = [specificationDigest];
  const missingAsset = await api.materializeRenderData({ view_data: views.diagram, view_data_hash: rendered.diagram.view_data_hash, recipe: assetRecipe, resolved_profile: profile, resolved_fonts: [font], resolved_assets: [], layout, limits: renderLimits });
  const incompatible = structuredClone(profile); incompatible.renderer_profile.profile_version = "2";
  const incompatibleResult = await api.materializeRenderData({ view_data: views.diagram, view_data_hash: rendered.diagram.view_data_hash, recipe: recipes.diagram, resolved_profile: incompatible, resolved_fonts: [font], resolved_assets: [], layout, limits: renderLimits });
  const limited = await api.materializeRenderData({ view_data: views.diagram, view_data_hash: rendered.diagram.view_data_hash, recipe: recipes.diagram, resolved_profile: profile, resolved_fonts: [font], resolved_assets: [], layout, limits: { ...renderLimits, max_primitives: 1 } });
  const cancelledPlan = completePlan(exportFixture.export_plan, views.context, rendered.context, "json", rendered.context.view_data_hash);
  const controller = new AbortController(); controller.abort();
  const cancelled = await api.serializeExport({ export_plan: cancelledPlan, view_data: views.context, serializer_profile: { schema_version: 1, ref: cancelledPlan.serializer_profile }, assets: [], fonts: [], signal: controller.signal });
  const visualPlan = completePlan(exportFixture.export_plan, views.context, rendered.context, "svg", rendered.context.view_data_hash);
  const visualInput = { export_plan: visualPlan, view_data: views.context, render_data: rendered.context, serializer_profile: { schema_version: 1, ref: visualPlan.serializer_profile }, assets: [], fonts: [fontResource] };
  const exportMissingResource = await api.serializeExport({ ...visualInput, fonts: [] });
  const malformedRender = structuredClone(rendered.context); malformedRender.source_bindings.pop();
  const malformedRenderResult = await api.serializeExport({ ...visualInput, render_data: malformedRender });
  const incompatibleExport = await api.serializeExport({ ...visualInput, serializer_profile: { schema_version: 1, ref: { ...visualPlan.serializer_profile, id: "layerdraw/incompatible@1" } } });
  const exportLimit = await api.serializeExport({ ...visualInput, serializer_profile: { schema_version: 1, ref: visualPlan.serializer_profile, limits: { max_output_bytes: 1 } } });
  let cancellationStarted;
  const started = new Promise((resolve) => { cancellationStarted = resolve; });
  const cancellingViewer = api.createViewer(viewerOptions(profile, recipes, font, capabilityManifest, { font_resolver: { resolve() { cancellationStarted(); return new Promise(() => {}); } } }));
  const pendingViewer = cancellingViewer.setViewData(snapshot(views.diagram, rendered.diagram.view_data_hash));
  await started;
  await cancellingViewer.cancel();
  const viewerCancellation = await pendingViewer;
  await cancellingViewer.dispose();
  return {
    schema_version: 1,
    corpus: [...Object.values(corpusNames), "state_optional_present"],
    derived_cases: ["empty_context", "flow_cycle", "tree_cycle", "tree_duplicate"],
    render: renderSummary, surfaces: surfaceSummary, viewer: { transitions, stale: stale.error?.code }, exports,
    failures: {
      malformed: malformed.diagnostics[0]?.code, missing_font: missingFont.diagnostics[0]?.code,
      missing_asset: missingAsset.diagnostics[0]?.code,
      incompatible_profile: incompatibleResult.diagnostics[0]?.code, resource_limit: limited.diagnostics[0]?.code,
      stale_stream: stale.error?.code, cancellation: cancelled.diagnostics[0]?.code,
      viewer_cancellation: viewerCancellation.outcome,
      export_missing_resource: exportMissingResource.diagnostics[0]?.code,
      export_malformed_render: malformedRenderResult.diagnostics[0]?.code,
      export_incompatible_profile: incompatibleExport.diagnostics[0]?.code,
      export_resource_limit: exportLimit.diagnostics[0]?.code,
      partial_render_published: "data" in limited, partial_export_published: "artifacts" in cancelled,
      partial_viewer_published: cancellingViewer.getPublication() !== undefined,
    },
    empty: { kind: emptyResult.data.kind, bindings: emptyResult.data.source_bindings.length },
    state: {
      policy: stateView.state_policy,
      input: stateView.state_input.kind,
      render_digest: await sha256(encoder.encode(canonical(stateRender.data))),
      viewer_status: stateOpened.state.status,
      source_manifest_digest: stateExport.source_manifest_digest,
      source_manifest_representations: stateExport.source_manifest_representations,
    },
  };
}
