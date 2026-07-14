// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package view

import (
	"sort"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/exportrecipe"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

type compiler struct {
	input               Input
	declarations        []resolve.DeclarationSymbol
	sources             map[string]resolve.DeclarationSource
	bindings            map[string][]resolve.SourceBinding
	symbols             map[string]resolve.StableSymbol
	queryRecipes        map[string]query.Recipe
	relationTypes       map[string]definition.RelationType
	columns             map[string]definition.Column
	graphEntities       map[string]bool
	projectionOverrides map[string]RelationProjection
	diagnostics         []resolve.Diagnostic
}

// Compile validates and compiles every selected View and nested Export
// declaration transactionally. It performs no Query execution, ViewData
// materialization, layout, rendering, ExportPlan construction, artifact I/O,
// asset fetching, filesystem/network/clock access, or state access.
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
	var recipes []Recipe
	var contexts []exportrecipe.ViewContext
	for _, declaration := range c.declarations {
		if declaration.Kind != resolve.KindView {
			continue
		}
		recipe := c.compileRecipe(declaration)
		recipes = append(recipes, recipe)
		contexts = append(contexts, exportContext(recipe, input.Resolve.Generation()))
	}
	exports := exportrecipe.Compile(exportrecipe.Input{
		Resolve: input.Resolve, Definition: input.Definition, Graph: input.Graph, Query: input.Query,
		Views: contexts, Registry: input.Registry,
	})
	diagnostics = append(diagnostics, c.diagnostics...)
	diagnostics = append(diagnostics, resolve.CloneDiagnostics(exports.Diagnostics)...)
	diagnostics = dedupeDiagnostics(diagnostics)
	resolve.SortDiagnostics(diagnostics)
	if hasError(diagnostics) {
		result.Diagnostics = diagnostics
		result.HasErrors = true
		return result
	}
	byView := map[string][]exportrecipe.Recipe{}
	for _, recipe := range exports.Recipes {
		byView[recipe.ViewAddress] = append(byView[recipe.ViewAddress], recipe)
	}
	for index := range recipes {
		recipes[index].Exports = exportrecipe.CloneRecipes(byView[recipes[index].Address])
		recipes[index].Dependencies.ExportAddresses = make([]string, 0, len(recipes[index].Exports))
		for _, export := range recipes[index].Exports {
			recipes[index].Dependencies.ExportAddresses = append(recipes[index].Dependencies.ExportAddresses, export.Address)
		}
	}
	result.Recipes = recipes
	result.ExportRecipes = exports
	result.Diagnostics = diagnostics
	return result
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
	if !c.input.Query.HasErrors {
		selected := map[string]bool{}
		for _, declaration := range c.input.Resolve.Declarations {
			if declaration.Kind == resolve.KindQuery {
				selected[declaration.Address] = true
			}
		}
		seen := map[string]bool{}
		for _, recipe := range c.input.Query.Recipes {
			if !selected[recipe.Address] || seen[recipe.Address] {
				c.diag("LDL1801", "stale_revision_or_semantic_hash", resolve.DeclarationSource{}, syntax.Span{}, "Query result contains a duplicate or foreign recipe", recipe.Address, "")
				coherent = false
			}
			seen[recipe.Address] = true
		}
		if len(seen) != len(selected) {
			c.diag("LDL1801", "stale_revision_or_semantic_hash", resolve.DeclarationSource{}, syntax.Span{}, "Query result is missing an effective recipe", c.input.Resolve.RootAddress, "")
			coherent = false
		}
	}
	return coherent
}

func newCompiler(input Input) *compiler {
	declarations := append([]resolve.DeclarationSymbol{}, input.Resolve.Declarations...)
	resolve.SortDeclarations(declarations)
	c := &compiler{
		input: input, declarations: declarations, sources: map[string]resolve.DeclarationSource{}, bindings: map[string][]resolve.SourceBinding{},
		symbols: map[string]resolve.StableSymbol{}, queryRecipes: map[string]query.Recipe{}, relationTypes: map[string]definition.RelationType{},
		columns: map[string]definition.Column{}, graphEntities: map[string]bool{},
		projectionOverrides: map[string]RelationProjection{},
	}
	for _, source := range input.Resolve.DeclarationSources {
		c.sources[source.Address] = source
	}
	for _, binding := range input.Resolve.Bindings {
		c.bindings[binding.SourceAddress] = append(c.bindings[binding.SourceAddress], binding)
	}
	for _, declaration := range input.Resolve.Candidates {
		c.symbols[declaration.Address] = declaration.Symbol
	}
	for _, declaration := range declarations {
		c.symbols[declaration.Address] = declaration.Symbol
	}
	for _, recipe := range input.Query.Recipes {
		c.queryRecipes[recipe.Address] = recipe
	}
	for _, relationType := range input.Definition.RelationTypes {
		c.relationTypes[relationType.Address] = relationType
		for _, column := range relationType.Columns {
			c.columns[column.Address] = column
		}
	}
	for _, entityType := range input.Definition.EntityTypes {
		for _, column := range entityType.Columns {
			c.columns[column.Address] = column
		}
	}
	for _, entity := range input.Graph.Graph.Entities {
		c.graphEntities[entity.Address] = true
	}
	return c
}

func (c *compiler) compileRecipe(declaration resolve.DeclarationSymbol) Recipe {
	recipe := Recipe{
		ID: declaration.ID, Address: declaration.Address, StateInput: query.StateNone, StateRequirement: query.StateNone,
		RelationProjections: []RelationProjection{}, ReservedTableColumnIDs: []string{}, ReservedExportIDs: []string{}, Exports: []exportrecipe.Recipe{},
	}
	source, ok := c.sources[declaration.Address]
	if !ok || source.Node == nil {
		c.diag("LDL1101", "invalid_structure_syntax", source, declaration.Range, "missing View declaration source", declaration.Address, "")
		return recipe
	}
	tokens := directTokens(source.Node)
	if len(tokens) < 4 || tokens[0].Raw != "view" || tokens[1].Raw != declaration.ID || tokens[2].Kind != syntax.TokenString || tokens[3].Kind != syntax.TokenIdentifier {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, declarationHeaderSpan(source.Node), "invalid View header", declaration.Address, "")
	} else {
		display, valid := tokenString(tokens[2])
		if !valid {
			c.diag("LDL1701", "unsupported_view_shape_or_export", source, tokens[2].Span, "invalid View display name", declaration.Address, "")
		} else {
			recipe.DisplayName = display
		}
		recipe.Category = c.category(source, declaration, tokens[3])
	}
	common, commonDiagnostics := definition.CompileCommonFields(source)
	recipe.Common = common
	c.diagnostics = append(c.diagnostics, commonDiagnostics...)
	members := viewBody(source.Node)
	c.validateViewMembers(source, declaration, members)
	if intent := oneMember(members, "intent"); intent != nil {
		recipe.Intent = c.optionalString(source, declaration, *intent)
	}
	if state := oneMember(members, "state_input"); state != nil {
		recipe.StateInput = c.statePolicy(source, declaration, *state)
	}
	recipe.Source = c.compileSource(source, declaration, members)
	recipe.RelationProjections = c.compileProjectionOverrides(source, declaration, members)
	c.projectionOverrides = map[string]RelationProjection{}
	for _, projection := range recipe.RelationProjections {
		c.projectionOverrides[projection.RelationTypeAddress] = projection
	}
	recipe.Shape = c.compileShape(source, declaration, members)
	recipe.ReservedTableColumnIDs = c.reservations(declaration.Symbol, resolve.KindTableColumn)
	recipe.ReservedExportIDs = c.reservations(declaration.Symbol, resolve.KindExport)
	c.validateCategorySourceShape(source, declaration, recipe.Category, recipe.Source.Kind, recipe.Shape.Kind)
	c.validateState(source, declaration, &recipe)
	recipe.Dependencies = c.dependencies(recipe)
	c.projectionOverrides = map[string]RelationProjection{}
	return recipe
}

func (c *compiler) effectiveRelationType(address string) definition.RelationType {
	relationType := c.relationTypes[address]
	if projection, ok := c.projectionOverrides[address]; ok {
		relationType.Projections = projection.Projections
		relationType.Render = projection.Render
	}
	return relationType
}

func (c *compiler) validateViewMembers(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, members []authoredMember) {
	allowed := set("description", "tags", "annotations", "intent", "state_input", "source", "reserve", "relation_projection", "diagram", "table", "matrix", "tree", "flow", "context", "diff", "export")
	singletons := set("description", "tags", "annotations", "intent", "state_input", "source", "reserve", "diagram", "table", "matrix", "tree", "flow", "context", "diff")
	seen := map[string]authoredMember{}
	shapeCount := 0
	for _, member := range members {
		if !allowed[member.head] {
			c.diag("LDL1102", "unknown_or_duplicate_schema_member", source, member.headSpan, "unknown View member", declaration.Address, "")
			continue
		}
		if isShapeName(member.head) {
			shapeCount++
		}
		if previous, duplicate := seen[member.head]; duplicate && singletons[member.head] {
			c.diagRelated("LDL1102", "unknown_or_duplicate_schema_member", source, member.headSpan, "duplicate View member", declaration.Address, "", previous.headSpan)
			continue
		}
		seen[member.head] = member
	}
	if _, ok := seen["source"]; !ok {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, declarationHeaderSpan(source.Node), "View requires exactly one source", declaration.Address, "")
	}
	if shapeCount != 1 {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, declarationHeaderSpan(source.Node), "View requires exactly one typed shape", declaration.Address, "")
	}
}

func (c *compiler) category(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, token syntax.Token) Category {
	category := Category(token.Raw)
	switch category {
	case CategoryTopology, CategoryInventory, CategoryDependency, CategoryHierarchy, CategoryFlow, CategoryImpact, CategoryDiff, CategoryContext:
		return category
	default:
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, token.Span, "invalid View category", declaration.Address, "")
		return ""
	}
}

func (c *compiler) optionalString(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, member authoredMember) *string {
	if member.block != nil || len(member.args) != 1 {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.headSpan, member.head+" requires one string", declaration.Address, "")
		return nil
	}
	value, ok := authoredString(member.args[0])
	if !ok {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, member.args[0].span, member.head+" requires one string", declaration.Address, "")
		return nil
	}
	return &value
}

func (c *compiler) statePolicy(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, member authoredMember) query.StatePolicy {
	if member.block != nil || len(member.args) != 1 {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.headSpan, "state_input requires one policy", declaration.Address, "")
		return query.StateNone
	}
	policy := query.StatePolicy(member.args[0].raw)
	switch policy {
	case query.StateNone, query.StateOptional, query.StateRequired:
		return policy
	default:
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.args[0].span, "invalid View state policy", declaration.Address, "")
		return query.StateNone
	}
}

func (c *compiler) compileSource(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, members []authoredMember) Source {
	member := oneMember(members, "source")
	if member == nil {
		return Source{}
	}
	if len(member.args) == 0 {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.headSpan, "invalid View source", declaration.Address, "")
		return Source{}
	}
	switch member.args[0].raw {
	case "query":
		return c.compileQuerySource(source, declaration, *member)
	case "diff":
		return c.compileDiffSource(source, declaration, *member)
	default:
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.args[0].span, "View source must be query or diff", declaration.Address, "")
		return Source{}
	}
}

func (c *compiler) compileQuerySource(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, member authoredMember) Source {
	result := Source{Kind: SourceQuery, Query: &QuerySource{Arguments: []Argument{}}}
	arguments := authoredValue{}
	switch {
	case member.block == nil && len(member.args) == 3 && member.args[2].object:
		arguments = member.args[2]
	case member.block != nil && len(member.args) == 2 && len(readMembers(member.block)) == 0:
		// The grammar intentionally parses a trailing {} as an empty nested
		// block. In Query-source position it is the canonical empty arguments
		// map documented by the language.
		arguments = authoredValue{node: member.block, span: member.block.Span, object: true}
	default:
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.span, "Query source requires Query reference and argument object", declaration.Address, "")
		return result
	}
	if member.args[1].kind != syntax.TokenIdentifier {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.args[1].span, "Query source requires Query reference and argument object", declaration.Address, "")
		return result
	}
	address, ok := c.singleBindingAt(declaration.Address, resolve.KindQuery, member.args[1].span, source)
	if ok {
		result.Query.QueryAddress = address
		result.Query.Arguments = c.compileArguments(source, declaration, address, arguments)
	}
	return result
}

func (c *compiler) compileDiffSource(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, member authoredMember) Source {
	result := Source{Kind: SourceDiff, Diff: &DiffSource{Arguments: []Argument{}}}
	if member.block == nil || len(member.args) != 3 || len(member.tokens) != 2 || member.tokens[1].Kind != syntax.TokenArrow || member.tokens[1].Raw != "->" {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.span, "Diff source requires before, arrow, after, and a block", declaration.Address, "")
		return result
	}
	before, beforeOK := authoredString(member.args[1])
	after, afterOK := authoredString(member.args[2])
	if !beforeOK || before == "" {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.args[1].span, "Diff before selector must be non-empty", declaration.Address, "")
	} else {
		result.Diff.Before = before
	}
	if !afterOK || after == "" {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.args[2].span, "Diff after selector must be non-empty", declaration.Address, "")
	} else {
		result.Diff.After = after
	}
	if beforeOK && afterOK && before == after {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.args[2].span, "Diff selectors must differ", declaration.Address, "")
	}
	children := readMembers(member.block)
	for index := range children {
		if children[index].head == "arguments" && children[index].block != nil && len(children[index].args) == 0 && len(readMembers(children[index].block)) == 0 {
			block := children[index].block
			children[index].args = []authoredValue{{node: block, span: block.Span, object: true}}
			children[index].block = nil
		}
	}
	c.validateClosedMembers(source, declaration.Address, "Diff source", children, map[string]memberRule{"query": {}, "arguments": {}}, true)
	queryMember := oneMember(children, "query")
	argumentsMember := oneMember(children, "arguments")
	if queryMember != nil {
		if queryMember.block != nil || len(queryMember.args) != 1 {
			c.diag("LDL1601", "invalid_query_or_arguments", source, queryMember.span, "Diff Query requires one reference", declaration.Address, "")
		} else if address, ok := c.singleBindingAt(declaration.Address, resolve.KindQuery, queryMember.args[0].span, source); ok {
			result.Diff.QueryAddress = &address
			arguments := authoredValue{}
			validArguments := false
			if argumentsMember != nil {
				switch {
				case argumentsMember.block == nil && len(argumentsMember.args) == 1 && argumentsMember.args[0].object:
					arguments, validArguments = argumentsMember.args[0], true
				case argumentsMember.block != nil && len(argumentsMember.args) == 0 && len(readMembers(argumentsMember.block)) == 0:
					arguments = authoredValue{node: argumentsMember.block, span: argumentsMember.block.Span, object: true}
					validArguments = true
				}
			}
			if !validArguments {
				c.diag("LDL1601", "invalid_query_or_arguments", source, queryMember.headSpan, "Diff Query requires one arguments object", declaration.Address, "")
			} else {
				result.Diff.Arguments = c.compileArguments(source, declaration, address, arguments)
			}
		}
	} else if argumentsMember != nil {
		c.diag("LDL1601", "invalid_query_or_arguments", source, argumentsMember.headSpan, "Diff arguments are forbidden without Query", declaration.Address, "")
	}
	return result
}

func (c *compiler) compileArguments(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, queryAddress string, value authoredValue) []Argument {
	recipe, exists := c.queryRecipes[queryAddress]
	if !exists {
		c.diag("LDL1801", "stale_revision_or_semantic_hash", source, value.span, "referenced Query recipe is unavailable", declaration.Address, "")
		return []Argument{}
	}
	parameters := map[string]query.Parameter{}
	for _, parameter := range recipe.Parameters {
		parameters[parameter.Address] = parameter
	}
	provided := map[string]Argument{}
	seen := map[string]syntax.Span{}
	for _, item := range objectItems(value) {
		if item.keyKind != syntax.TokenIdentifier {
			c.diag("LDL1601", "invalid_query_or_arguments", source, item.keySpan, "Query argument key must be a parameter ID", declaration.Address, "")
			continue
		}
		address, ok := c.singleBindingAt(declaration.Address, resolve.KindParameter, item.keySpan, source)
		if !ok {
			continue
		}
		if previous, duplicate := seen[address]; duplicate {
			c.diagRelated("LDL1601", "invalid_query_or_arguments", source, item.keySpan, "duplicate Query argument", declaration.Address, "", previous)
			continue
		}
		seen[address] = item.keySpan
		parameter, valid := parameters[address]
		if !valid {
			c.diag("LDL1801", "stale_revision_or_semantic_hash", source, item.keySpan, "parameter binding is outside the referenced Query recipe", declaration.Address, "")
			continue
		}
		column := parameterColumn(parameter)
		scalar, valid := definition.NormalizeScalarLiteral(item.value.raw, item.value.kind, column)
		if !valid {
			c.diag("LDL1601", "invalid_query_or_arguments", source, item.value.span, "Query argument does not satisfy parameter schema", declaration.Address, "")
			continue
		}
		provided[address] = Argument{ParameterAddress: address, Value: scalar}
	}
	arguments := make([]Argument, 0, len(recipe.Parameters))
	for _, parameter := range recipe.Parameters {
		if argument, ok := provided[parameter.Address]; ok {
			arguments = append(arguments, argument)
			continue
		}
		if parameter.Default != nil {
			arguments = append(arguments, Argument{ParameterAddress: parameter.Address, Value: *parameter.Default, Defaulted: true})
			continue
		}
		if parameter.Required {
			c.diag("LDL1601", "invalid_query_or_arguments", source, value.span, "required Query argument is missing", declaration.Address, "")
		}
	}
	return arguments
}

func parameterColumn(parameter query.Parameter) definition.Column {
	return definition.Column{
		ID: parameter.ID, Address: parameter.Address, ValueType: parameter.ValueType,
		EnumValues: append([]string{}, parameter.EnumValues...), ReservedEnumValues: append([]string{}, parameter.ReservedEnumValues...),
		Required: parameter.Required, Default: parameter.Default, Format: parameter.Format,
		Min: parameter.Min, Max: parameter.Max, MinLength: parameter.MinLength, MaxLength: parameter.MaxLength,
	}
}

func (c *compiler) validateCategorySourceShape(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, category Category, sourceKind SourceKind, shape ShapeKind) {
	diffCount := 0
	if category == CategoryDiff {
		diffCount++
	}
	if sourceKind == SourceDiff {
		diffCount++
	}
	if shape == ShapeDiff {
		diffCount++
	}
	if diffCount != 0 && diffCount != 3 {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, declarationHeaderSpan(source.Node), "diff category, source, and shape must occur together", declaration.Address, "")
	}
}

func (c *compiler) validateState(source resolve.DeclarationSource, declaration resolve.DeclarationSymbol, recipe *Recipe) {
	directReads := shapeStateReads(recipe.Shape)
	if len(directReads) == 0 && recipe.StateInput != query.StateNone {
		c.diag("LDL1601", "invalid_query_or_arguments", source, stateDiagnosticSpan(source.Node), "state_input is forbidden without direct Table state reads", declaration.Address, "")
	}
	if len(directReads) != 0 && recipe.StateInput == query.StateNone {
		c.diag("LDL1601", "invalid_query_or_arguments", source, stateDiagnosticSpan(source.Node), "direct Table state reads require optional or required state_input", declaration.Address, "")
	}
	queryPolicy := query.StateNone
	var queryAddress string
	if recipe.Source.Query != nil {
		queryAddress = recipe.Source.Query.QueryAddress
	} else if recipe.Source.Diff != nil && recipe.Source.Diff.QueryAddress != nil {
		queryAddress = *recipe.Source.Diff.QueryAddress
	}
	if sourceQuery, ok := c.queryRecipes[queryAddress]; ok {
		queryPolicy = sourceQuery.StateInput
	}
	recipe.StateRequirement = composeStatePolicy(queryPolicy, recipe.StateInput)
	if (recipe.Source.Kind == SourceDiff || recipe.Shape.Kind == ShapeDiff || recipe.Category == CategoryDiff) && recipe.StateRequirement != query.StateNone {
		c.diag("LDL1701", "unsupported_view_shape_or_export", source, declarationHeaderSpan(source.Node), "Diff source and shape forbid state-dependent Query or View reads", declaration.Address, "")
	}
}

func composeStatePolicy(a, b query.StatePolicy) query.StatePolicy {
	if a == query.StateRequired || b == query.StateRequired {
		return query.StateRequired
	}
	if a == query.StateOptional || b == query.StateOptional {
		return query.StateOptional
	}
	return query.StateNone
}

func (c *compiler) reservations(owner resolve.StableSymbol, kind resolve.SubjectKind) []string {
	out := []string{}
	for _, reservation := range c.input.Resolve.Identity.Reservations {
		if reservation.Kind == kind && resolve.CompareStableSymbols(reservation.Owner, owner) == 0 {
			out = append(out, reservation.ID)
		}
	}
	sort.Strings(out)
	return out
}

func (c *compiler) dependencies(recipe Recipe) Dependencies {
	sets := map[resolve.SubjectKind]map[string]bool{}
	sourceAddresses := map[string]bool{recipe.Address: true}
	for _, declaration := range c.declarations {
		if declaration.Owner != nil && resolve.StableAddress(*declaration.Owner) == recipe.Address && declaration.Kind == resolve.KindTableColumn {
			sourceAddresses[declaration.Address] = true
		}
	}
	for sourceAddress := range sourceAddresses {
		for _, binding := range c.bindings[sourceAddress] {
			if sets[binding.ExpectedKind] == nil {
				sets[binding.ExpectedKind] = map[string]bool{}
			}
			sets[binding.ExpectedKind][binding.TargetAddress] = true
		}
	}
	var queryAddress string
	if recipe.Source.Query != nil {
		queryAddress = recipe.Source.Query.QueryAddress
	} else if recipe.Source.Diff != nil && recipe.Source.Diff.QueryAddress != nil {
		queryAddress = *recipe.Source.Diff.QueryAddress
	}
	queryRecipe, hasQuery := c.queryRecipes[queryAddress]
	if hasQuery {
		mergeDependencySet(sets, resolve.KindLayer, queryRecipe.Dependencies.LayerAddresses)
		mergeDependencySet(sets, resolve.KindEntityType, queryRecipe.Dependencies.EntityTypeAddresses)
		mergeDependencySet(sets, resolve.KindRelationType, queryRecipe.Dependencies.RelationTypeAddresses)
		mergeDependencySet(sets, resolve.KindEntity, queryRecipe.Dependencies.EntityAddresses)
		mergeDependencySet(sets, resolve.KindRelation, queryRecipe.Dependencies.RelationAddresses)
		mergeDependencySet(sets, resolve.KindColumn, queryRecipe.Dependencies.ColumnAddresses)
		mergeDependencySet(sets, resolve.KindParameter, queryRecipe.Dependencies.ParameterAddresses)
	}
	dependencies := Dependencies{
		QueryAddresses: setKeys(sets[resolve.KindQuery]), ParameterAddresses: setKeys(sets[resolve.KindParameter]),
		LayerAddresses: setKeys(sets[resolve.KindLayer]), EntityTypeAddresses: setKeys(sets[resolve.KindEntityType]),
		RelationTypeAddresses: setKeys(sets[resolve.KindRelationType]), EntityAddresses: setKeys(sets[resolve.KindEntity]),
		RelationAddresses: setKeys(sets[resolve.KindRelation]), ColumnAddresses: setKeys(sets[resolve.KindColumn]), ExportAddresses: []string{},
		StateReads: append([]query.StateReadDependency{}, shapeStateReads(recipe.Shape)...),
	}
	if hasQuery {
		dependencies.StateReads = append(dependencies.StateReads, queryRecipe.Dependencies.StateReads...)
	}
	for _, values := range [][]string{dependencies.QueryAddresses, dependencies.ParameterAddresses, dependencies.LayerAddresses, dependencies.EntityTypeAddresses, dependencies.RelationTypeAddresses, dependencies.EntityAddresses, dependencies.RelationAddresses, dependencies.ColumnAddresses} {
		c.sortAddresses(values)
	}
	dependencies.StateReads = canonicalStateReads(dependencies.StateReads)
	return dependencies
}

func mergeDependencySet(sets map[resolve.SubjectKind]map[string]bool, kind resolve.SubjectKind, values []string) {
	if sets[kind] == nil {
		sets[kind] = map[string]bool{}
	}
	for _, value := range values {
		sets[kind][value] = true
	}
}

func (c *compiler) singleBindingAt(sourceAddress string, kind resolve.SubjectKind, span syntax.Span, source resolve.DeclarationSource) (string, bool) {
	bindings := c.bindingsAt(sourceAddress, kind, span)
	if len(bindings) != 1 {
		c.diag("LDL1301", "unknown_or_ambiguous_symbol", source, span, "expected one precise resolver-owned binding", sourceAddress, ownerForSource(c.declarations, sourceAddress))
		return "", false
	}
	return bindings[0].TargetAddress, true
}

func (c *compiler) bindingsAt(sourceAddress string, kind resolve.SubjectKind, span syntax.Span) []resolve.SourceBinding {
	var out []resolve.SourceBinding
	for _, binding := range c.bindings[sourceAddress] {
		if binding.ExpectedKind == kind && binding.Range == span {
			out = append(out, binding)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return c.compareAddresses(out[i].TargetAddress, out[j].TargetAddress) < 0 })
	return out
}

func (c *compiler) sortAddresses(values []string) {
	sort.SliceStable(values, func(i, j int) bool { return c.compareAddresses(values[i], values[j]) < 0 })
}

func (c *compiler) compareAddresses(a, b string) int {
	aSymbol, aOK := c.symbols[a]
	bSymbol, bOK := c.symbols[b]
	if aOK && bOK {
		return resolve.CompareStableSymbols(aSymbol, bSymbol)
	}
	return strings.Compare(a, b)
}

func exportContext(recipe Recipe, generation resolve.StageGeneration) exportrecipe.ViewContext {
	context := exportrecipe.ViewContext{Address: recipe.Address, Category: exportrecipe.Category(recipe.Category), Shape: exportrecipe.Shape(recipe.Shape.Kind), StatePolicy: recipe.StateRequirement, Generation: generation}
	context.DiffSource = recipe.Source.Kind == SourceDiff
	if recipe.Shape.Diagram != nil {
		context.DiagramComposed = recipe.Shape.Diagram.Composed
	}
	return context
}

func oneMember(members []authoredMember, head string) *authoredMember {
	for index := range members {
		if members[index].head == head {
			return &members[index]
		}
	}
	return nil
}

func isShapeName(name string) bool {
	switch name {
	case "diagram", "table", "matrix", "tree", "flow", "context", "diff":
		return true
	default:
		return false
	}
}

type memberRule struct {
	block bool
	flag  bool
}

func (c *compiler) validateClosedMembers(source resolve.DeclarationSource, subject, label string, members []authoredMember, rules map[string]memberRule, singleton bool) {
	seen := map[string]authoredMember{}
	for _, member := range members {
		rule, known := rules[member.head]
		valid := known && (member.block != nil) == rule.block
		if rule.flag {
			valid = valid && len(member.args) == 0
		}
		if !valid {
			c.diag("LDL1102", "unknown_or_duplicate_schema_member", source, member.headSpan, "unknown or invalid "+label+" member", subject, ownerForSource(c.declarations, subject))
			continue
		}
		if previous, duplicate := seen[member.head]; duplicate && singleton {
			c.diagRelated("LDL1102", "unknown_or_duplicate_schema_member", source, member.headSpan, "duplicate "+label+" member", subject, ownerForSource(c.declarations, subject), previous.headSpan)
			continue
		}
		seen[member.head] = member
	}
}

func tokenString(token syntax.Token) (string, bool) {
	return authoredString(authoredValue{raw: token.Raw, kind: token.Kind, span: token.Span})
}

func declarationHeaderSpan(node *syntax.Node) syntax.Span {
	tokens := directTokens(node)
	if len(tokens) >= 2 {
		return syntax.Span{Start: tokens[0].Span.Start, End: tokens[1].Span.End}
	}
	if len(tokens) == 1 {
		return tokens[0].Span
	}
	if node != nil {
		return node.Span
	}
	return syntax.Span{}
}

func stateDiagnosticSpan(node *syntax.Node) syntax.Span {
	for _, member := range viewBody(node) {
		if member.head == "state_input" {
			if len(member.args) != 0 {
				return member.args[0].span
			}
			return member.headSpan
		}
	}
	return declarationHeaderSpan(node)
}

func ownerForSource(declarations []resolve.DeclarationSymbol, address string) string {
	for _, declaration := range declarations {
		if declaration.Address == address && declaration.Owner != nil {
			return resolve.StableAddress(*declaration.Owner)
		}
	}
	return ""
}

func set(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func setKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
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

func dedupeDiagnostics(diagnostics []resolve.Diagnostic) []resolve.Diagnostic {
	resolve.SortDiagnostics(diagnostics)
	if len(diagnostics) < 2 {
		return diagnostics
	}
	out := diagnostics[:0]
	keys := map[string]bool{}
	for _, diagnostic := range diagnostics {
		key := diagnosticKey(diagnostic)
		if keys[key] {
			continue
		}
		keys[key] = true
		out = append(out, diagnostic)
	}
	return out
}

func diagnosticKey(diagnostic resolve.Diagnostic) string {
	rangeKey := ""
	if diagnostic.Range != nil {
		rangeKey = string(diagnostic.Range.Origin.Kind) + "|" + diagnostic.Range.Origin.PackAddress + "|" + diagnostic.Range.ModulePath + "|" + strings.Join([]string{itoa(diagnostic.Range.StartByte), itoa(diagnostic.Range.EndByte)}, ":")
	}
	return diagnostic.Code + "|" + diagnostic.Severity + "|" + diagnostic.MessageKey + "|" + diagnostic.SubjectAddress + "|" + diagnostic.OwnerAddress + "|" + rangeKey + "|" + diagnostic.Message
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [32]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}

func (c *compiler) diag(code, key string, source resolve.DeclarationSource, span syntax.Span, message, subject, owner string) {
	c.diagnostics = append(c.diagnostics, resolve.Diagnostic{Code: code, Severity: "error", MessageKey: key, Arguments: map[string]string{}, Message: message, Range: sourceRange(source, span), SubjectAddress: subject, OwnerAddress: owner})
}

func (c *compiler) diagRelated(code, key string, source resolve.DeclarationSource, span syntax.Span, message, subject, owner string, previous syntax.Span) {
	c.diag(code, key, source, span, message, subject, owner)
	c.diagnostics[len(c.diagnostics)-1].Related = []resolve.DiagnosticRelated{{Relation: "previous", Range: sourceRange(source, previous), SubjectAddress: subject, OwnerAddress: owner}}
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
