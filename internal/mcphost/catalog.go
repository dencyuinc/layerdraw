// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package mcphost

type toolMapping struct {
	name, operation, description string
	bound                        bool
}

// toolCatalog is the single MCP-to-owner mapping table. Availability still
// comes exclusively from the owner capability snapshot.
var toolCatalog = []toolMapping{
	{"layerdraw.list_modules", "engine.list_modules", "List modules in the bounded document scope.", true},
	{"layerdraw.find_symbols", "engine.find_symbols", "Resolve exact symbols through the Engine.", true},
	{"layerdraw.search", "runtime.search", "Run revision and Access-bound project search.", true},
	{"layerdraw.read_declarations", "engine.read_declarations", "Read bounded declarations.", true},
	{"layerdraw.read_rows", "engine.read_rows", "Read bounded typed rows.", true},
	{"layerdraw.get_neighbors", "engine.get_neighbors", "Read bounded graph neighbors.", true},
	{"layerdraw.inspect_subgraph", "engine.inspect_subgraph", "Inspect a bounded explicit subgraph.", true},
	{"layerdraw.find_usages", "engine.find_usages", "Find bounded stable-address usages.", true},
	{"layerdraw.list_references", "engine.list_references", "List bounded references.", true},
	{"layerdraw.read_references", "engine.read_references", "Read bounded references.", true},
	{"layerdraw.preview_operations", "runtime.preview_operations", "Preview semantic operations with impact and Access decision.", true},
	{"layerdraw.preview_fragment", "engine.preview_fragment", "Preview a scoped LDL fragment through Workbench.", true},
	{"layerdraw.preview_source_patch", "engine.preview_source_patch", "Preview a revision-bound source patch through Workbench.", true},
	{"layerdraw.apply_operations", "runtime.commit_operations", "Commit a previously authorized operation batch through Runtime.", true},
	{"layerdraw.apply_source_patch", "runtime.apply_source_patch", "Commit a previously authorized source patch through Runtime.", true},
	{"layerdraw.stage_asset", "runtime.stage_asset", "Stage an asset through Runtime.", true},
	{"layerdraw.format_scope", "engine.format_scope", "Format a stable source scope through Workbench.", true},
	{"layerdraw.organize_workspace", "engine.organize_workspace", "Preview canonical source organization through Workbench.", true},
	{"layerdraw.run_query", "runtime.execute_query", "Run an Engine-owned query bound by Runtime.", true},
	{"layerdraw.analyze_graph", "runtime.analyze_graph", "Run bounded Engine-owned graph analysis.", true},
	{"layerdraw.materialize_view", "engine.materialize_view", "Materialize canonical ViewData.", true},
	{"layerdraw.plan_export", "engine.plan_export", "Create a canonical ExportPlan.", true},
	{"layerdraw.serialize_export", "host.serialize_export", "Serialize a bounded artifact through the owning exporter.", true},
	{"layerdraw.import_document", "host.import_document", "Preview a document import through the owning adapter.", true},
	{"layerdraw.export_document", "host.export_document", "Publish a document export through Runtime.", true},
	{"layerdraw.list_revisions", "runtime.list_revisions", "List bounded committed revisions.", true},
	{"layerdraw.restore_revision", "runtime.restore_revision", "Preview and commit a revision restore through Runtime.", true},
	{"layerdraw.registry_search", "registry.search", "Browse the canonical Registry client.", false},
	{"layerdraw.registry_plan_install", "registry.plan_install", "Plan a Registry transaction.", true},
	{"layerdraw.registry_apply_install", "registry.apply_install", "Commit a revalidated Registry transaction through Runtime.", true},
}

var mappingByName = func() map[string]toolMapping {
	result := make(map[string]toolMapping, len(toolCatalog))
	for _, mapping := range toolCatalog {
		result[mapping.name] = mapping
	}
	return result
}()
