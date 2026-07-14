// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package exportrecipe

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

type compiler struct {
	input       Input
	contexts    map[string]ViewContext
	sources     map[string]resolve.DeclarationSource
	symbols     map[string]resolve.StableSymbol
	registry    ProfileRegistry
	profiles    map[string]ProfileSpecification
	diagnostics []resolve.Diagnostic
}

// Compile validates and compiles every selected nested Export declaration
// transactionally. This static stage never executes a Query, reads state,
// accepts ViewData, produces ExportPlan, or invokes a renderer/serializer.
func Compile(input Input) Result {
	diagnostics := upstreamDiagnostics(input)
	gate := &compiler{input: input}
	coherent := gate.validateParents()
	diagnostics = append(diagnostics, gate.diagnostics...)
	resolve.SortDiagnostics(diagnostics)
	result := Result{}
	if coherent {
		result.stageGeneration = input.Resolve.Generation()
	}
	if input.Resolve.HasErrors || input.Definition.HasErrors || input.Graph.HasErrors || input.Query.HasErrors || !coherent {
		result.Diagnostics = diagnostics
		result.HasErrors = true
		return result
	}

	c := newCompiler(input)
	c.validateRegistry()
	var recipes []Recipe
	for _, declaration := range c.orderedExportDeclarations() {
		recipes = append(recipes, c.compileRecipe(declaration))
	}
	c.validateFilenameUniqueness(recipes)
	diagnostics = append(diagnostics, c.diagnostics...)
	resolve.SortDiagnostics(diagnostics)
	if hasError(diagnostics) {
		result.Diagnostics = diagnostics
		result.HasErrors = true
		return result
	}
	result.Recipes = recipes
	result.Diagnostics = diagnostics
	return result
}

func (c *compiler) orderedExportDeclarations() []resolve.DeclarationSymbol {
	declarations := append([]resolve.DeclarationSymbol{}, c.input.Resolve.Declarations...)
	resolve.SortDeclarations(declarations)
	byOwner := map[string]map[string]resolve.DeclarationSymbol{}
	for _, declaration := range declarations {
		if declaration.Kind != resolve.KindExport {
			continue
		}
		owner := ownerAddress(declaration)
		if byOwner[owner] == nil {
			byOwner[owner] = map[string]resolve.DeclarationSymbol{}
		}
		byOwner[owner][declaration.ID] = declaration
	}

	seen := map[string]bool{}
	var ordered []resolve.DeclarationSymbol
	for _, declaration := range declarations {
		if declaration.Kind != resolve.KindView || len(byOwner[declaration.Address]) == 0 {
			continue
		}
		source, ok := c.sources[declaration.Address]
		if !ok || source.Node == nil {
			c.diag("LDL1101", "invalid_structure_syntax", source, declaration.Range, "missing owner View declaration source for Export ordering", declaration.Address, "")
			continue
		}
		for _, member := range readMembers(firstNode(source.Node, syntax.NodeBlock)) {
			if member.head != "export" || len(member.args) == 0 {
				continue
			}
			nested, exists := byOwner[declaration.Address][member.args[0].raw]
			if !exists || seen[nested.Address] {
				continue
			}
			seen[nested.Address] = true
			ordered = append(ordered, nested)
		}
	}
	for _, declaration := range declarations {
		if declaration.Kind != resolve.KindExport || seen[declaration.Address] {
			continue
		}
		source := c.sources[declaration.Address]
		c.diag("LDL1101", "invalid_structure_syntax", source, declaration.Range, "Export declaration is absent from its owner View source", declaration.Address, ownerAddress(declaration))
		ordered = append(ordered, declaration)
	}
	return ordered
}

func upstreamDiagnostics(input Input) []resolve.Diagnostic {
	if len(input.Query.Diagnostics) != 0 {
		return resolve.CloneDiagnostics(input.Query.Diagnostics)
	}
	if len(input.Graph.Diagnostics) != 0 {
		return resolve.CloneDiagnostics(input.Graph.Diagnostics)
	}
	if len(input.Definition.Diagnostics) != 0 {
		return resolve.CloneDiagnostics(input.Definition.Diagnostics)
	}
	return resolve.CloneDiagnostics(input.Resolve.Diagnostics)
}

func (c *compiler) validateParents() bool {
	coherent := true
	if !c.input.Definition.MatchesResolve(c.input.Resolve) || c.input.Definition.Root.Mode != c.input.Resolve.Mode || c.input.Definition.Root.Address != c.input.Resolve.RootAddress {
		c.diag("LDL1801", "stale_revision_or_semantic_hash", resolve.DeclarationSource{}, syntax.Span{}, "definition result does not match Resolve generation", c.input.Resolve.RootAddress, "")
		coherent = false
	}
	if !c.input.Graph.MatchesResolve(c.input.Resolve) {
		c.diag("LDL1801", "stale_revision_or_semantic_hash", resolve.DeclarationSource{}, syntax.Span{}, "graph result does not match Resolve generation", c.input.Resolve.RootAddress, "")
		coherent = false
	}
	if !c.input.Query.MatchesResolve(c.input.Resolve) {
		c.diag("LDL1801", "stale_revision_or_semantic_hash", resolve.DeclarationSource{}, syntax.Span{}, "Query result does not match Resolve generation", c.input.Resolve.RootAddress, "")
		coherent = false
	}
	if !c.input.Graph.HasErrors && c.input.Graph.Graph == nil {
		c.diag("LDL1601", "invalid_query_or_arguments", resolve.DeclarationSource{}, syntax.Span{}, "typed graph result is unavailable", c.input.Resolve.RootAddress, "")
		coherent = false
	}
	selectedViews := map[string]bool{}
	for _, declaration := range c.input.Resolve.Declarations {
		if declaration.Kind == resolve.KindView {
			selectedViews[declaration.Address] = true
		}
	}
	seen := map[string]bool{}
	for _, context := range c.input.Views {
		if !context.Generation.Matches(c.input.Resolve.Generation()) || !selectedViews[context.Address] || seen[context.Address] {
			c.diag("LDL1801", "stale_revision_or_semantic_hash", resolve.DeclarationSource{}, syntax.Span{}, "View context is stale, duplicate, or outside the effective document", context.Address, "")
			coherent = false
		}
		if !validContextCategory(context.Category) || !validContextShape(context.Shape) || !validContextStatePolicy(context.StatePolicy) ||
			(context.Category == CategoryDiff) != (context.Shape == ShapeDiff) || (context.Shape == ShapeDiff) != context.DiffSource ||
			context.DiagramComposed && context.Shape != ShapeDiagram {
			c.diag("LDL1801", "stale_revision_or_semantic_hash", resolve.DeclarationSource{}, syntax.Span{}, "View context is corrupt or internally incompatible", context.Address, "")
			coherent = false
		}
		seen[context.Address] = true
	}
	if len(seen) != len(selectedViews) {
		c.diag("LDL1801", "stale_revision_or_semantic_hash", resolve.DeclarationSource{}, syntax.Span{}, "View contexts do not cover the effective document", c.input.Resolve.RootAddress, "")
		coherent = false
	}
	if !c.input.Query.HasErrors {
		queryAddresses := map[string]bool{}
		for _, declaration := range c.input.Resolve.Declarations {
			if declaration.Kind == resolve.KindQuery {
				queryAddresses[declaration.Address] = true
			}
		}
		for _, recipe := range c.input.Query.Recipes {
			if !queryAddresses[recipe.Address] {
				c.diag("LDL1801", "stale_revision_or_semantic_hash", resolve.DeclarationSource{}, syntax.Span{}, "Query recipe is outside the effective document", recipe.Address, "")
				coherent = false
			}
			delete(queryAddresses, recipe.Address)
		}
		if len(queryAddresses) != 0 {
			c.diag("LDL1801", "stale_revision_or_semantic_hash", resolve.DeclarationSource{}, syntax.Span{}, "Query result is missing an effective recipe", c.input.Resolve.RootAddress, "")
			coherent = false
		}
	}
	return coherent
}

func validContextCategory(value Category) bool {
	switch value {
	case CategoryTopology, CategoryInventory, CategoryDependency, CategoryHierarchy, CategoryFlow, CategoryImpact, CategoryDiff, CategoryContext:
		return true
	default:
		return false
	}
}

func validContextShape(value Shape) bool {
	switch value {
	case ShapeDiagram, ShapeTable, ShapeMatrix, ShapeTree, ShapeFlow, ShapeContext, ShapeDiff:
		return true
	default:
		return false
	}
}

func validContextStatePolicy(value query.StatePolicy) bool {
	switch value {
	case query.StateNone, query.StateOptional, query.StateRequired:
		return true
	default:
		return false
	}
}

func newCompiler(input Input) *compiler {
	registry := BuiltinRegistry()
	if input.Registry != nil {
		registry = cloneRegistry(*input.Registry)
	}
	c := &compiler{
		input: input, contexts: map[string]ViewContext{}, sources: map[string]resolve.DeclarationSource{},
		symbols: map[string]resolve.StableSymbol{}, registry: registry, profiles: map[string]ProfileSpecification{},
	}
	for _, context := range input.Views {
		c.contexts[context.Address] = context
	}
	for _, source := range input.Resolve.DeclarationSources {
		c.sources[source.Address] = source
	}
	for _, declaration := range input.Resolve.Candidates {
		c.symbols[declaration.Address] = declaration.Symbol
	}
	for _, declaration := range input.Resolve.Declarations {
		c.symbols[declaration.Address] = declaration.Symbol
	}
	return c
}

func (c *compiler) validateRegistry() {
	builtin := BuiltinRegistry()
	if c.registry.Format != "layerdraw-exporter-profiles" || c.registry.SchemaVersion != 1 ||
		c.registry.Digest != registryDigest(c.registry) || c.registry.Digest != builtin.Digest ||
		len(c.registry.Profiles) != len(builtin.Profiles) {
		c.diag("LDL1701", "unsupported_view_shape_or_export", resolve.DeclarationSource{}, syntax.Span{}, "invalid exporter-profile registry identity", c.input.Resolve.RootAddress, "")
	}
	builtinProfiles := map[string]ProfileSpecification{}
	for _, profile := range builtin.Profiles {
		builtinProfiles[profile.ID] = profile
	}
	seen := map[string]bool{}
	for _, profile := range c.registry.Profiles {
		canonical, exists := builtinProfiles[profile.ID]
		valid := !seen[profile.ID] && profile.SpecificationDigest == digest(profile.Specification) && exists &&
			canonical.Format == profile.Format && canonical.SpecificationDigest == profile.SpecificationDigest
		if !valid {
			c.diag("LDL1701", "unsupported_view_shape_or_export", resolve.DeclarationSource{}, syntax.Span{}, "invalid or replaceable exporter profile", c.input.Resolve.RootAddress, "")
			continue
		}
		seen[profile.ID] = true
		c.profiles[profile.ID] = profile
	}
}

func (c *compiler) compileRecipe(declaration resolve.DeclarationSymbol) Recipe {
	owner := ""
	if declaration.Owner != nil {
		owner = resolve.StableAddress(*declaration.Owner)
	}
	recipe := Recipe{ID: declaration.ID, Address: declaration.Address, ViewAddress: owner, FidelityBasis: FidelityBasisNative}
	source, ok := c.sources[declaration.Address]
	if !ok || source.Node == nil {
		c.diag("LDL1101", "invalid_structure_syntax", source, declaration.Range, "missing Export declaration source", declaration.Address, owner)
		return recipe
	}
	context, contextOK := c.contexts[owner]
	if !contextOK {
		c.diag("LDL1801", "stale_revision_or_semantic_hash", source, headerSpan(source.Node), "Export owner View context is unavailable", declaration.Address, owner)
		return recipe
	}
	args := exportArguments(source.Node)
	if len(args) != 3 || args[0].raw != declaration.ID || args[0].kind != syntax.TokenIdentifier || args[1].kind != syntax.TokenIdentifier || args[2].kind != syntax.TokenString {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, headerSpan(source.Node), "invalid Export header", declaration.Address, owner)
		return recipe
	}
	recipe.Format = Format(args[1].raw)
	recipe.Extension = extensionFor(recipe.Format)
	filename, filenameOK := authoredString(args[2])
	if !filenameOK || !validFilename(filename, recipe.Extension) {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, args[2].span, "Export filename is not a canonical basename", declaration.Address, owner)
	} else {
		recipe.Filename = filename
	}
	members := exportBody(source.Node)
	c.validateMembers(source, declaration, recipe.Format, members)
	recipe.Fidelity = c.requiredFidelity(source, declaration, members)
	recipe.SourceRefs = flagValue(c, source, declaration, members, "source_refs")
	recipe.Options = c.compileOptions(source, declaration, context, recipe.Format, members)
	recipe.ExporterProfile = c.compileProfile(source, declaration, recipe.Format, members)
	c.validateCapability(source, declaration, context, &recipe)
	return recipe
}

func (c *compiler) validateMembers(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, format Format, members []authoredMember) {
	allowed := map[string]bool{"fidelity": true, "source_refs": true, "exporter_profile": true}
	for _, option := range optionsFor(format) {
		allowed[option] = true
	}
	seen := map[string]authoredMember{}
	for _, member := range members {
		if !allowed[member.head] || member.block != nil {
			c.diag("LDL1102", "unknown_or_duplicate_schema_member", source, member.headSpan, "unknown or invalid Export option", declaration.Address, ownerAddress(declaration))
			continue
		}
		if previous, duplicate := seen[member.head]; duplicate {
			c.diagRelated("LDL1102", "unknown_or_duplicate_schema_member", source, member.headSpan, "duplicate Export option", declaration.Address, ownerAddress(declaration), previous.headSpan)
			continue
		}
		seen[member.head] = member
	}
}

func (c *compiler) requiredFidelity(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, members []authoredMember) Fidelity {
	member := oneMember(members, "fidelity")
	if member == nil {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, headerSpan(source.Node), "Export requires fidelity", declaration.Address, ownerAddress(declaration))
		return ""
	}
	if member.block != nil || len(member.args) != 1 {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.headSpan, "fidelity requires one value", declaration.Address, ownerAddress(declaration))
		return ""
	}
	value := Fidelity(member.args[0].raw)
	switch value {
	case FidelityLossless, FidelityTraceableSummary, FidelityVisualOnly, FidelityLossy:
		return value
	default:
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.args[0].span, "invalid Export fidelity", declaration.Address, ownerAddress(declaration))
		return ""
	}
}

func (c *compiler) compileProfile(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, format Format, members []authoredMember) ExporterProfileRef {
	id := "layerdraw/" + string(format) + "@1"
	if member := oneMember(members, "exporter_profile"); member != nil {
		if len(member.args) != 1 {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.headSpan, "exporter_profile requires one string", declaration.Address, ownerAddress(declaration))
			return ExporterProfileRef{}
		}
		value, ok := authoredString(member.args[0])
		if !ok || !profileIDPattern.MatchString(value) {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.args[0].span, "invalid exporter profile ID", declaration.Address, ownerAddress(declaration))
			return ExporterProfileRef{}
		}
		id = value
	}
	profile, ok := c.profiles[id]
	if !ok || profile.Format != format {
		span := headerSpan(source.Node)
		if member := oneMember(members, "exporter_profile"); member != nil && len(member.args) != 0 {
			span = member.args[0].span
		}
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, span, "exporter profile is missing or has incompatible format", declaration.Address, ownerAddress(declaration))
		return ExporterProfileRef{}
	}
	return ExporterProfileRef{ID: id, Format: format, RegistrySchemaVersion: c.registry.SchemaVersion, RegistryDigest: c.registry.Digest, SpecificationDigest: profile.SpecificationDigest}
}

var profileIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]*@[1-9][0-9]*$`)

func (c *compiler) compileOptions(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, context ViewContext, format Format, members []authoredMember) Options {
	options := Options{Kind: format}
	switch format {
	case FormatJSON, FormatYAML:
		options.Structured = &StructuredOptions{Diagnostics: flagValue(c, source, declaration, members, "diagnostics"), StateSummary: flagValue(c, source, declaration, members, "state_summary")}
	case FormatSVG, FormatPNG:
		options.Image = &ImageOptions{
			Width: dimensionValue(c, source, declaration, members, "width"), Height: dimensionValue(c, source, declaration, members, "height"),
			Scale: numberValue(c, source, declaration, members, "scale", 1), Background: backgroundValue(c, source, declaration, members),
		}
	case FormatPDF, FormatPPTX, FormatDOCX:
		options.Page = &PageOptions{
			PageSize:    PageSize(enumValue(c, source, declaration, members, "page_size", string(PageA4), set("a3", "a4", "letter", "legal", "ledger"))),
			Orientation: Orientation(enumValue(c, source, declaration, members, "orientation", string(OrientationPortrait), set("portrait", "landscape"))),
			Fit:         Fit(enumValue(c, source, declaration, members, "fit", string(FitPage), set("none", "page", "width"))),
			Legend:      flagValue(c, source, declaration, members, "legend"),
		}
	case FormatHTML:
		options.HTML = &HTMLOptions{Interactive: flagValue(c, source, declaration, members, "interactive"), EmbedAssets: flagValue(c, source, declaration, members, "embed_assets")}
	case FormatCSV, FormatTSV:
		options.Delimited = &DelimitedOptions{Bundle: flagValue(c, source, declaration, members, "bundle"), Header: flagValue(c, source, declaration, members, "header"), SourceManifest: flagValue(c, source, declaration, members, "source_manifest")}
	case FormatXLSX:
		profile := defaultXLSXProfile(context)
		if member := oneMember(members, "profile"); member != nil {
			profile = XLSXProfile(enumValue(c, source, declaration, members, "profile", string(profile), xlsxProfiles))
		}
		options.XLSX = &XLSXOptions{Profile: profile, LookupSheets: flagValue(c, source, declaration, members, "lookup_sheets"), HiddenIDs: flagValue(c, source, declaration, members, "hidden_ids"), Formulas: flagValue(c, source, declaration, members, "formulas"), ViewDataJSON: flagValue(c, source, declaration, members, "view_data_json")}
	case FormatMarkdown, FormatMermaid, FormatBPMN, FormatDrawIO:
		options.Manifest = &ManifestOptions{SourceManifest: flagValue(c, source, declaration, members, "source_manifest")}
	}
	return options
}

func (c *compiler) validateCapability(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, context ViewContext, recipe *Recipe) {
	native, allowed := nativeMaximum(context.Shape, recipe.Format)
	if !allowed {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, formatSpan(source.Node), "format is unsupported for View shape", declaration.Address, ownerAddress(declaration))
		return
	}
	if recipe.Format == FormatJSON || recipe.Format == FormatYAML {
		if !recipe.SourceRefs {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, headerSpan(source.Node), "JSON/YAML export requires source_refs", declaration.Address, ownerAddress(declaration))
		}
	}
	if recipe.Options.Delimited != nil {
		traceable := recipe.Options.Delimited.Bundle && recipe.Options.Delimited.Header && recipe.Options.Delimited.SourceManifest
		if !traceable {
			native = FidelityLossy
		}
	}
	if (context.Shape == ShapeTree || context.Shape == ShapeFlow) && recipe.Format == FormatMermaid && (recipe.Options.Manifest == nil || !recipe.Options.Manifest.SourceManifest) {
		native = FidelityLossy
	}
	recipe.NativeMaximumFidelity = native
	recipe.EffectiveMaximumFidelity = native
	if recipe.Options.XLSX != nil {
		if !xlsxProfileCompatible(context, recipe.Options.XLSX.Profile) {
			span := optionValueSpan(source.Node, "profile")
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, span, "XLSX profile is incompatible with View category or shape", declaration.Address, ownerAddress(declaration))
		}
		if recipe.Options.XLSX.ViewDataJSON && recipe.Options.XLSX.HiddenIDs {
			recipe.EffectiveMaximumFidelity = FidelityLossless
			recipe.FidelityBasis = FidelityBasisEmbeddedViewData
		}
	}
	if recipe.Options.Structured != nil && recipe.Options.Structured.StateSummary && context.DiffSource {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, optionValueSpan(source.Node, "state_summary"), "state_summary is forbidden for Diff exports", declaration.Address, ownerAddress(declaration))
	}
	if (recipe.Fidelity == FidelityLossless || recipe.Fidelity == FidelityTraceableSummary) && !recipe.SourceRefs {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, optionValueSpan(source.Node, "fidelity"), "lossless and traceable exports require source_refs", declaration.Address, ownerAddress(declaration))
	}
	if fidelityRank(recipe.Fidelity) > fidelityRank(recipe.EffectiveMaximumFidelity) {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, optionValueSpan(source.Node, "fidelity"), "requested fidelity exceeds format capability", declaration.Address, ownerAddress(declaration))
	}
	embedded := recipe.Format == FormatJSON || recipe.Format == FormatYAML || recipe.Options.XLSX != nil && recipe.Options.XLSX.ViewDataJSON
	explicitManifest := recipe.Options.Delimited != nil && recipe.Options.Delimited.SourceManifest || recipe.Options.Manifest != nil && recipe.Options.Manifest.SourceManifest
	recipe.RequiresSourceManifest = explicitManifest || context.StatePolicy != query.StateNone || recipe.SourceRefs && !embedded
}

func nativeMaximum(shape Shape, format Format) (Fidelity, bool) {
	if format == FormatJSON || format == FormatYAML {
		return FidelityLossless, true
	}
	formats := map[Shape]map[Format]Fidelity{
		ShapeDiagram: {FormatXLSX: FidelityTraceableSummary, FormatHTML: FidelityTraceableSummary, FormatCSV: FidelityTraceableSummary, FormatTSV: FidelityTraceableSummary, FormatSVG: FidelityVisualOnly, FormatPNG: FidelityVisualOnly, FormatPDF: FidelityVisualOnly, FormatPPTX: FidelityVisualOnly, FormatDOCX: FidelityVisualOnly, FormatDrawIO: FidelityVisualOnly, FormatMermaid: FidelityLossy},
		ShapeTable:   {FormatXLSX: FidelityTraceableSummary, FormatCSV: FidelityTraceableSummary, FormatTSV: FidelityTraceableSummary, FormatHTML: FidelityTraceableSummary, FormatPDF: FidelityVisualOnly, FormatPPTX: FidelityVisualOnly, FormatDOCX: FidelityVisualOnly, FormatMarkdown: FidelityLossy},
		ShapeMatrix:  {FormatXLSX: FidelityTraceableSummary, FormatCSV: FidelityTraceableSummary, FormatTSV: FidelityTraceableSummary, FormatHTML: FidelityTraceableSummary, FormatSVG: FidelityVisualOnly, FormatPNG: FidelityVisualOnly, FormatPDF: FidelityVisualOnly, FormatPPTX: FidelityVisualOnly, FormatDOCX: FidelityVisualOnly},
		ShapeTree:    {FormatXLSX: FidelityTraceableSummary, FormatCSV: FidelityTraceableSummary, FormatTSV: FidelityTraceableSummary, FormatHTML: FidelityTraceableSummary, FormatMermaid: FidelityTraceableSummary, FormatSVG: FidelityVisualOnly, FormatPNG: FidelityVisualOnly, FormatPDF: FidelityVisualOnly, FormatPPTX: FidelityVisualOnly, FormatDOCX: FidelityVisualOnly, FormatDrawIO: FidelityVisualOnly},
		ShapeFlow:    {FormatXLSX: FidelityTraceableSummary, FormatCSV: FidelityTraceableSummary, FormatTSV: FidelityTraceableSummary, FormatHTML: FidelityTraceableSummary, FormatMermaid: FidelityTraceableSummary, FormatBPMN: FidelityLossy, FormatSVG: FidelityVisualOnly, FormatPNG: FidelityVisualOnly, FormatPDF: FidelityVisualOnly, FormatPPTX: FidelityVisualOnly, FormatDOCX: FidelityVisualOnly, FormatDrawIO: FidelityVisualOnly, FormatMarkdown: FidelityLossy},
		ShapeContext: {FormatCSV: FidelityTraceableSummary, FormatTSV: FidelityTraceableSummary, FormatXLSX: FidelityTraceableSummary, FormatHTML: FidelityTraceableSummary, FormatMarkdown: FidelityTraceableSummary, FormatPDF: FidelityVisualOnly, FormatPPTX: FidelityVisualOnly, FormatDOCX: FidelityVisualOnly},
		ShapeDiff:    {FormatCSV: FidelityTraceableSummary, FormatTSV: FidelityTraceableSummary, FormatXLSX: FidelityTraceableSummary, FormatHTML: FidelityTraceableSummary, FormatMarkdown: FidelityTraceableSummary, FormatPDF: FidelityVisualOnly, FormatPPTX: FidelityVisualOnly, FormatDOCX: FidelityVisualOnly},
	}
	maximum, ok := formats[shape][format]
	return maximum, ok
}

func defaultXLSXProfile(context ViewContext) XLSXProfile {
	switch context.Shape {
	case ShapeDiagram:
		if context.DiagramComposed {
			return XLSXComposedDiagramWorkbook
		}
		return XLSXDiagramWorkbook
	case ShapeTable:
		return XLSXTypeWorkbook
	case ShapeMatrix:
		return XLSXMatrixWorkbook
	case ShapeTree:
		return XLSXTreeWorkbook
	case ShapeFlow:
		return XLSXFlowWorkbook
	case ShapeContext:
		return XLSXContextWorkbook
	case ShapeDiff:
		return XLSXDiffWorkbook
	default:
		return ""
	}
}

func xlsxProfileCompatible(context ViewContext, profile XLSXProfile) bool {
	switch profile {
	case XLSXTypeWorkbook:
		return context.Shape == ShapeTable
	case XLSXDiagramWorkbook, XLSXComposedDiagramWorkbook, XLSXDiagramInventoryWorkbook:
		return context.Shape == ShapeDiagram
	case XLSXMatrixWorkbook:
		return context.Shape == ShapeMatrix
	case XLSXTreeWorkbook:
		return context.Shape == ShapeTree
	case XLSXFlowWorkbook:
		return context.Shape == ShapeFlow
	case XLSXDiffWorkbook:
		return context.Shape == ShapeDiff
	case XLSXContextWorkbook:
		return context.Shape == ShapeContext
	case XLSXImpactWorkbook:
		return context.Category == CategoryImpact && (context.Shape == ShapeDiagram || context.Shape == ShapeTable || context.Shape == ShapeMatrix)
	default:
		return false
	}
}

var xlsxProfiles = set("type_workbook", "diagram_workbook", "composed_diagram_workbook", "matrix_workbook", "tree_workbook", "impact_workbook", "flow_workbook", "diff_workbook", "context_workbook", "diagram_inventory_workbook")

func (c *compiler) validateFilenameUniqueness(recipes []Recipe) {
	seen := map[string]Recipe{}
	for _, recipe := range recipes {
		key := recipe.ViewAddress + "\x00" + recipe.Filename
		if previous, duplicate := seen[key]; duplicate && recipe.Filename != "" {
			source := c.sources[recipe.Address]
			previousSource := c.sources[previous.Address]
			c.diagRelatedSources("LDL1701", "unsupported_view_shape_or_export", source, filenameSpan(source.Node), "duplicate Export filename", recipe.Address, recipe.ViewAddress, previousSource, filenameSpan(previousSource.Node), previous.Address)
			continue
		}
		seen[key] = recipe
	}
}

func optionsFor(format Format) []string {
	switch format {
	case FormatJSON, FormatYAML:
		return []string{"diagnostics", "state_summary"}
	case FormatSVG, FormatPNG:
		return []string{"width", "height", "scale", "background"}
	case FormatPDF, FormatPPTX, FormatDOCX:
		return []string{"page_size", "orientation", "fit", "legend"}
	case FormatXLSX:
		return []string{"profile", "lookup_sheets", "hidden_ids", "formulas", "view_data_json"}
	case FormatCSV, FormatTSV:
		return []string{"bundle", "header", "source_manifest"}
	case FormatHTML:
		return []string{"interactive", "embed_assets"}
	case FormatMarkdown, FormatMermaid, FormatBPMN, FormatDrawIO:
		return []string{"source_manifest"}
	default:
		return nil
	}
}

func extensionFor(format Format) string {
	extensions := map[Format]string{FormatJSON: ".json", FormatYAML: ".yaml", FormatSVG: ".svg", FormatPNG: ".png", FormatPDF: ".pdf", FormatHTML: ".html", FormatCSV: ".csv", FormatTSV: ".tsv", FormatXLSX: ".xlsx", FormatMarkdown: ".md", FormatPPTX: ".pptx", FormatDOCX: ".docx", FormatMermaid: ".mmd", FormatBPMN: ".bpmn", FormatDrawIO: ".drawio"}
	return extensions[format]
}

func validFilename(filename, extension string) bool {
	return filename != "" && extension != "" && filename != "." && filename != ".." &&
		!strings.ContainsAny(filename, "/\\\x00") && strings.HasSuffix(filename, extension) &&
		len(strings.TrimSuffix(filename, extension)) != 0
}

func flagValue(c *compiler, source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, members []authoredMember, name string) bool {
	member := oneMember(members, name)
	if member == nil {
		return false
	}
	if member.block != nil || len(member.args) != 0 {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, name+" is a flag", declaration.Address, ownerAddress(declaration))
		return false
	}
	return true
}

func dimensionValue(c *compiler, source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, members []authoredMember, name string) Dimension {
	member := oneMember(members, name)
	if member == nil {
		return Dimension{Auto: true}
	}
	if len(member.args) == 1 && member.args[0].kind == syntax.TokenIdentifier && member.args[0].raw == "auto" {
		return Dimension{Auto: true}
	}
	if len(member.args) != 1 || member.args[0].kind != syntax.TokenInteger {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, name+" requires auto or a positive integer", declaration.Address, ownerAddress(declaration))
		return Dimension{Auto: true}
	}
	value, err := strconv.ParseInt(member.args[0].raw, 10, 64)
	if err != nil || value <= 0 || value > 1<<53-1 {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.args[0].span, name+" requires a positive JSON-safe integer", declaration.Address, ownerAddress(declaration))
		return Dimension{Auto: true}
	}
	return Dimension{Value: value}
}

func numberValue(c *compiler, source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, members []authoredMember, name string, fallback float64) float64 {
	member := oneMember(members, name)
	if member == nil {
		return fallback
	}
	if len(member.args) != 1 || member.args[0].kind != syntax.TokenInteger && member.args[0].kind != syntax.TokenNumber {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, name+" requires a finite positive number", declaration.Address, ownerAddress(declaration))
		return fallback
	}
	value, err := strconv.ParseFloat(member.args[0].raw, 64)
	if err != nil || value <= 0 || math.IsInf(value, 0) || math.IsNaN(value) {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.args[0].span, name+" requires a finite positive number", declaration.Address, ownerAddress(declaration))
		return fallback
	}
	return value
}

func backgroundValue(c *compiler, source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, members []authoredMember) string {
	member := oneMember(members, "background")
	if member == nil {
		return "transparent"
	}
	if len(member.args) != 1 {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.span, "background requires transparent or a color", declaration.Address, ownerAddress(declaration))
		return "transparent"
	}
	if member.args[0].raw == "transparent" && member.args[0].kind == syntax.TokenIdentifier {
		return "transparent"
	}
	value, ok := authoredString(member.args[0])
	value = strings.ToUpper(value)
	if !ok || !colorPattern.MatchString(value) {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.args[0].span, "invalid canonical color", declaration.Address, ownerAddress(declaration))
		return "transparent"
	}
	return value
}

var colorPattern = regexp.MustCompile(`^#[0-9A-F]{6}([0-9A-F]{2})?$`)

func enumValue(c *compiler, source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, members []authoredMember, name, fallback string, allowed map[string]bool) string {
	member := oneMember(members, name)
	if member == nil {
		return fallback
	}
	if len(member.args) != 1 || member.args[0].kind != syntax.TokenIdentifier || !allowed[member.args[0].raw] {
		span := member.span
		if len(member.args) != 0 {
			span = member.args[0].span
		}
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, span, "invalid "+name, declaration.Address, ownerAddress(declaration))
		return fallback
	}
	return member.args[0].raw
}

func oneMember(members []authoredMember, name string) *authoredMember {
	for index := range members {
		if members[index].head == name {
			return &members[index]
		}
	}
	return nil
}

func headerSpan(node *syntax.Node) syntax.Span {
	tokens := directTokens(node)
	if len(tokens) != 0 {
		return tokens[0].Span
	}
	if node != nil {
		return node.Span
	}
	return syntax.Span{}
}

func formatSpan(node *syntax.Node) syntax.Span {
	args := exportArguments(node)
	if len(args) >= 2 {
		return args[1].span
	}
	return headerSpan(node)
}

func filenameSpan(node *syntax.Node) syntax.Span {
	args := exportArguments(node)
	if len(args) >= 3 {
		return args[2].span
	}
	return headerSpan(node)
}

func optionValueSpan(node *syntax.Node, name string) syntax.Span {
	for _, member := range exportBody(node) {
		if member.head == name {
			if len(member.args) != 0 {
				return member.args[0].span
			}
			return member.headSpan
		}
	}
	return headerSpan(node)
}

func ownerAddress(declaration resolve.DeclarationSymbol) string {
	if declaration.Owner == nil {
		return ""
	}
	return resolve.StableAddress(*declaration.Owner)
}

func fidelityRank(fidelity Fidelity) int {
	switch fidelity {
	case FidelityLossless:
		return 3
	case FidelityTraceableSummary:
		return 2
	case FidelityVisualOnly:
		return 1
	case FidelityLossy:
		return 0
	default:
		return 99
	}
}

func set(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func hasError(diagnostics []resolve.Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == "error" {
			return true
		}
	}
	return false
}

func (c *compiler) diag(code, key string, source resolve.DeclarationSource, span syntax.Span, message, subject, owner string) {
	c.diagnostics = append(c.diagnostics, resolve.Diagnostic{Code: code, Severity: "error", MessageKey: key, Arguments: map[string]string{}, Message: message, Range: sourceRange(source, span), SubjectAddress: subject, OwnerAddress: owner})
}

func (c *compiler) diagRelated(code, key string, source resolve.DeclarationSource, span syntax.Span, message, subject, owner string, previous syntax.Span) {
	c.diag(code, key, source, span, message, subject, owner)
	c.diagnostics[len(c.diagnostics)-1].Related = []resolve.DiagnosticRelated{{Relation: "previous", Range: sourceRange(source, previous), SubjectAddress: subject, OwnerAddress: owner}}
}

func (c *compiler) diagRelatedSources(code, key string, source resolve.DeclarationSource, span syntax.Span, message, subject, owner string, previousSource resolve.DeclarationSource, previous syntax.Span, previousSubject string) {
	c.diag(code, key, source, span, message, subject, owner)
	c.diagnostics[len(c.diagnostics)-1].Related = []resolve.DiagnosticRelated{{Relation: "previous", Range: sourceRange(previousSource, previous), SubjectAddress: previousSubject, OwnerAddress: owner}}
}

func sourceRange(source resolve.DeclarationSource, span syntax.Span) *resolve.SourceRange {
	if source.Module.Path == "" {
		return nil
	}
	origin := resolve.SourceOrigin{Kind: source.Module.Origin.Kind}
	if source.Module.Origin.Kind == resolve.OriginPack {
		origin.PackAddress = resolve.StableAddress(resolve.StableSymbol{Origin: source.Module.Origin})
	}
	return &resolve.SourceRange{Origin: origin, ModulePath: source.Module.Path, StartByte: span.Start, EndByte: span.End}
}
