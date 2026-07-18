// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package engine exposes the transport-neutral facade for LayerDraw semantics.
package engine

import "slices"

const (
	// DevelopmentVersion is used when no release version is injected at build time.
	DevelopmentVersion = "0.0.0-dev"

	// UnknownSourceRevision is used when source provenance is unavailable.
	UnknownSourceRevision = "unknown"

	// CapabilityDescribe identifies the bootstrap component-description operation.
	CapabilityDescribe = "engine.describe"

	// CapabilityCompile identifies the transport-neutral closed-input compiler.
	CapabilityCompile = "engine.compile"

	CapabilityInspectLayerdraw = "engine.inspect_layerdraw"
	CapabilityReadLayerdraw    = "engine.read_layerdraw"
	CapabilityWriteLayerdraw   = "engine.write_layerdraw"

	CapabilityCloseDocument     = "engine.close_document"
	CapabilityExecuteQuery      = "engine.execute_query"
	CapabilityFindSymbols       = "engine.find_symbols"
	CapabilityFindUsages        = "engine.find_usages"
	CapabilityGetNeighbors      = "engine.get_neighbors"
	CapabilityInspectSubgraph   = "engine.inspect_subgraph"
	CapabilityListModules       = "engine.list_modules"
	CapabilityListReferences    = "engine.list_references"
	CapabilityMaterializeView   = "engine.materialize_view"
	CapabilityPlanExport        = "engine.plan_export"
	CapabilityOpenDocument      = "engine.open_document"
	CapabilityReadDeclarations  = "engine.read_declarations"
	CapabilityReadModules       = "engine.read_modules"
	CapabilityReadReferences    = "engine.read_references"
	CapabilityReadRows          = "engine.read_rows"
	CapabilityReadScope         = "engine.read_scope"
	CapabilityReplaceSourceTree = "engine.replace_source_tree"
)

var bootstrapCapabilities = []string{
	CapabilityCloseDocument,
	CapabilityCompile,
	CapabilityDescribe,
	CapabilityExecuteQuery,
	CapabilityFindSymbols,
	CapabilityFindUsages,
	CapabilityGetNeighbors,
	CapabilityInspectLayerdraw,
	CapabilityInspectSubgraph,
	CapabilityListModules,
	CapabilityListReferences,
	CapabilityMaterializeView,
	CapabilityOpenDocument,
	CapabilityPlanExport,
	CapabilityReadDeclarations,
	CapabilityReadLayerdraw,
	CapabilityReadModules,
	CapabilityReadReferences,
	CapabilityReadRows,
	CapabilityReadScope,
	CapabilityReplaceSourceTree,
	CapabilityWriteLayerdraw,
}

// BuildInfo identifies the source used to build an Engine instance.
type BuildInfo struct {
	ReleaseVersion string
	SourceRevision string
	Workbench      WorkbenchConfig
}

// Descriptor reports capabilities linked into this Engine instance.
// It is an in-process model, not an Engine Protocol wire type.
type Descriptor struct {
	Component      string
	ReleaseVersion string
	SourceRevision string
	Capabilities   []string
}

// Engine is the public facade for the canonical Go semantic implementation.
type Engine struct {
	build     BuildInfo
	workbench *workbenchStore
}

// New creates an Engine with deterministic development defaults.
func New(build BuildInfo) Engine {
	if build.ReleaseVersion == "" {
		build.ReleaseVersion = DevelopmentVersion
	}
	if build.SourceRevision == "" {
		build.SourceRevision = UnknownSourceRevision
	}

	return Engine{build: build, workbench: newWorkbenchStore(build.Workbench)}
}

// Describe returns a defensive snapshot of the linked component capabilities.
func (e Engine) Describe() Descriptor {
	return Descriptor{
		Component:      "engine",
		ReleaseVersion: e.build.ReleaseVersion,
		SourceRevision: e.build.SourceRevision,
		Capabilities:   slices.Clone(bootstrapCapabilities),
	}
}
