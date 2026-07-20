// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package mcphost

type toolMapping struct {
	name, operation, previewOperation, description string
	requiredOperations                             []string
	bound                                          bool
}

// toolCatalog is the single MCP-to-owner mapping table. Availability still
// comes exclusively from the owner capability snapshot.
var toolCatalog = []toolMapping{
	{name: "layerdraw.list_modules", operation: "engine.list_modules", description: "List modules in the bounded document scope.", bound: true},
	{name: "layerdraw.find_symbols", operation: "engine.find_symbols", description: "Resolve exact symbols through the Engine.", bound: true},
	{name: "layerdraw.search", operation: "native.execute_search", description: "Run revision and Access-bound project search.", bound: true},
	{name: "layerdraw.read_declarations", operation: "engine.read_declarations", description: "Read bounded declarations.", bound: true},
	{name: "layerdraw.read_rows", operation: "engine.read_rows", description: "Read bounded typed rows.", bound: true},
	{name: "layerdraw.get_neighbors", operation: "engine.get_neighbors", description: "Read bounded graph neighbors.", bound: true},
	{name: "layerdraw.inspect_subgraph", operation: "engine.inspect_subgraph", description: "Inspect a bounded explicit subgraph.", bound: true},
	{name: "layerdraw.find_usages", operation: "engine.find_usages", description: "Find bounded stable-address usages.", bound: true},
	{name: "layerdraw.list_references", operation: "engine.list_references", description: "List bounded references.", bound: true},
	{name: "layerdraw.read_references", operation: "engine.read_references", description: "Read bounded references.", bound: true},
	{name: "layerdraw.preview_operations", operation: "runtime.preview_operations", description: "Preview semantic operations with impact and Access decision.", bound: true},
	{name: "layerdraw.preview_fragment", operation: "engine.preview_fragment", description: "Preview a scoped LDL fragment through Workbench.", bound: true},
	{name: "layerdraw.preview_source_patch", operation: "engine.preview_source_patch", description: "Preview a revision-bound source patch through Workbench.", bound: true},
	{name: "layerdraw.apply_operations", operation: "runtime.commit_operations", previewOperation: "runtime.preview_operations", requiredOperations: []string{"runtime.preview_operations", "runtime.commit_operations"}, description: "Re-preview, revalidate, and commit an operation batch through Runtime.", bound: true},
	{name: "layerdraw.apply_source_patch", operation: "runtime.commit_operations", previewOperation: "engine.preview_source_patch", requiredOperations: []string{"engine.preview_source_patch", "runtime.commit_operations"}, description: "Re-preview a source patch and commit its authorized operation batch through Runtime.", bound: true},
	{name: "layerdraw.stage_asset", operation: "runtime.stage_asset", description: "Stage an asset through Runtime.", bound: true},
	{name: "layerdraw.format_scope", operation: "engine.format_scope", description: "Format a stable source scope through Workbench.", bound: true},
	{name: "layerdraw.organize_workspace", operation: "engine.organize_workspace", description: "Preview canonical source organization through Workbench.", bound: true},
	{name: "layerdraw.run_query", operation: "native.execute_query", description: "Run an Engine-owned query bound by Runtime.", bound: true},
	{name: "layerdraw.analyze_graph", operation: "native.execute_analysis", description: "Run bounded Engine-owned graph analysis.", bound: true},
	{name: "layerdraw.materialize_view", operation: "engine.materialize_view", description: "Materialize canonical ViewData.", bound: true},
	{name: "layerdraw.plan_export", operation: "engine.plan_export", description: "Create a canonical ExportPlan.", bound: true},
	{name: "layerdraw.serialize_export", description: "Serialize a bounded artifact through the configured native exporter.", bound: true},
	{name: "layerdraw.import_document", description: "Preview a document import through the configured interchange adapter.", bound: true},
	{name: "layerdraw.export_document", description: "Publish a document export through the configured interchange adapter.", bound: true},
	{name: "layerdraw.list_revisions", operation: "runtime.list_revisions", description: "List bounded committed revisions.", bound: true},
	{name: "layerdraw.restore_revision", operation: "runtime.commit_operations", previewOperation: "runtime.preview_restore", requiredOperations: []string{"runtime.preview_restore", "runtime.commit_operations"}, description: "Preview, revalidate, and commit a revision restore through Runtime.", bound: true},
	{name: "layerdraw.registry_search", operation: "registry.search", description: "Browse the canonical Registry client.", bound: false},
	{name: "layerdraw.registry_plan_install", operation: "registry.plan_install", description: "Plan a Registry transaction.", bound: true},
	{name: "layerdraw.registry_apply_install", operation: "registry.commit_plan", previewOperation: "registry.plan_install", requiredOperations: []string{"registry.plan_install", "registry.commit_plan"}, description: "Re-plan, revalidate, and commit a Registry transaction.", bound: true},
	{name: "layerdraw.review_list_proposals", operation: "review.list_proposals", description: "List canonical Review proposals and their current lifecycle state.", bound: true},
	{name: "layerdraw.review_create_proposal", operation: "review.create_proposal", description: "Save an Engine-previewed human or agent proposal for review.", bound: true},
	{name: "layerdraw.review_comment", operation: "review.comment", description: "Attach a revision-aware comment to a canonical Review target.", bound: true},
	{name: "layerdraw.review_approve_apply", operation: "review.approve_apply", description: "Re-preview, reauthorize the current approver, and atomically apply a proposal.", bound: true},
	{name: "layerdraw.review_withdraw", operation: "review.withdraw", description: "Withdraw a proposal through its canonical Review owner.", bound: true},
}

var mappingByName = func() map[string]toolMapping {
	result := make(map[string]toolMapping, len(toolCatalog))
	for _, mapping := range toolCatalog {
		result[mapping.name] = mapping
	}
	return result
}()

type ToolRoute struct {
	Name               string
	Operation          string
	PreviewOperation   string
	RequiredOperations []string
}

func ToolRoutes() []ToolRoute {
	result := make([]ToolRoute, len(toolCatalog))
	for index, mapping := range toolCatalog {
		result[index] = ToolRoute{Name: mapping.name, Operation: mapping.operation, PreviewOperation: mapping.previewOperation, RequiredOperations: append([]string(nil), mapping.requiredOperations...)}
	}
	return result
}
