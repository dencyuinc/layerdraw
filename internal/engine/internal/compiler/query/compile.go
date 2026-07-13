// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package query

import (
	"sort"
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

type compiler struct {
	input         Input
	declarations  []resolve.DeclarationSymbol
	sources       map[string]resolve.DeclarationSource
	bindings      map[string][]resolve.SourceBinding
	symbols       map[string]resolve.StableSymbol
	columns       map[string]definition.Column
	parameters    map[string]definition.Column
	graphEntities map[string]bool
	diagnostics   []resolve.Diagnostic
	stateReads    []StateReadDependency
}

// Compile validates and compiles every selected Query transactionally. If any
// upstream or Query diagnostic is an error, no recipe is returned.
func Compile(input Input) Result {
	diagnostics := upstreamDiagnostics(input)
	resolve.SortDiagnostics(diagnostics)
	if input.Resolve.HasErrors || input.Definition.HasErrors || input.Graph.HasErrors {
		return Result{Diagnostics: diagnostics, HasErrors: true}
	}
	gate := &compiler{}
	if matches, available := definitionMatchesResolve(input); available && !matches {
		gate.diag("LDL1801", "stale_revision_or_semantic_hash", resolve.DeclarationSource{}, syntax.Span{}, "definition result generation does not match resolve result", input.Resolve.RootAddress, "")
	}
	if matches, available := graphMatchesResolve(input); available && !matches {
		gate.diag("LDL1801", "stale_revision_or_semantic_hash", resolve.DeclarationSource{}, syntax.Span{}, "graph result generation does not match resolve result", input.Resolve.RootAddress, "")
	}
	if input.Graph.Graph == nil {
		gate.diag("LDL1601", "invalid_query_or_arguments", resolve.DeclarationSource{}, syntax.Span{}, "typed graph result is unavailable", input.Resolve.RootAddress, "")
	}
	if hasError(gate.diagnostics) {
		diagnostics = append(diagnostics, gate.diagnostics...)
		resolve.SortDiagnostics(diagnostics)
		return Result{Diagnostics: diagnostics, HasErrors: true}
	}
	c := newCompiler(input)
	var recipes []Recipe
	for _, declaration := range c.declarations {
		if declaration.Kind != resolve.KindQuery {
			continue
		}
		recipes = append(recipes, c.compileRecipe(declaration))
	}
	diagnostics = append(diagnostics, c.diagnostics...)
	resolve.SortDiagnostics(diagnostics)
	if hasError(diagnostics) {
		return Result{Diagnostics: diagnostics, HasErrors: true}
	}
	return Result{Recipes: recipes, Diagnostics: diagnostics}
}

type resolveGenerationMatcher interface {
	MatchesResolve(resolve.Result) bool
}

// The required stacked parent predates the generation API. On repaired #14,
// both concrete stage Results implement this contract and Compile rejects a
// mismatch before reading either stage payload.
func definitionMatchesResolve(input Input) (bool, bool) {
	if matcher, ok := any(input.Definition).(resolveGenerationMatcher); ok {
		return matcher.MatchesResolve(input.Resolve), true
	}
	if matcher, ok := any(&input.Definition).(resolveGenerationMatcher); ok {
		return matcher.MatchesResolve(input.Resolve), true
	}
	return true, false
}

func graphMatchesResolve(input Input) (bool, bool) {
	if matcher, ok := any(input.Graph).(resolveGenerationMatcher); ok {
		return matcher.MatchesResolve(input.Resolve), true
	}
	if matcher, ok := any(&input.Graph).(resolveGenerationMatcher); ok {
		return matcher.MatchesResolve(input.Resolve), true
	}
	return true, false
}

func upstreamDiagnostics(input Input) []resolve.Diagnostic {
	if len(input.Graph.Diagnostics) != 0 {
		return append([]resolve.Diagnostic{}, input.Graph.Diagnostics...)
	}
	if len(input.Definition.Diagnostics) != 0 {
		return append([]resolve.Diagnostic{}, input.Definition.Diagnostics...)
	}
	return append([]resolve.Diagnostic{}, input.Resolve.Diagnostics...)
}

func newCompiler(input Input) *compiler {
	declarations := append([]resolve.DeclarationSymbol{}, input.Resolve.Declarations...)
	resolve.SortDeclarations(declarations)
	c := &compiler{
		input:         input,
		declarations:  declarations,
		sources:       map[string]resolve.DeclarationSource{},
		bindings:      map[string][]resolve.SourceBinding{},
		symbols:       map[string]resolve.StableSymbol{},
		columns:       map[string]definition.Column{},
		parameters:    map[string]definition.Column{},
		graphEntities: map[string]bool{},
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
	for _, entityType := range input.Definition.EntityTypes {
		for _, column := range entityType.Columns {
			c.columns[column.Address] = column
		}
	}
	for _, relationType := range input.Definition.RelationTypes {
		for _, column := range relationType.Columns {
			c.columns[column.Address] = column
		}
	}
	if input.Graph.Graph != nil {
		for _, entity := range input.Graph.Graph.Entities {
			c.graphEntities[entity.Address] = true
		}
	}
	return c
}

func (c *compiler) compileRecipe(declaration resolve.DeclarationSymbol) Recipe {
	source, ok := c.sources[declaration.Address]
	recipe := Recipe{
		ID:                   declaration.ID,
		Address:              declaration.Address,
		StateInput:           StateNone,
		Parameters:           []Parameter{},
		Where:                Predicate{Kind: PredicateAll, Children: []Predicate{}},
		RelationWhere:        Predicate{Kind: PredicateAll, Children: []Predicate{}},
		Result:               []ResultMember{ResultSeedEntities, ResultTraversedEntities, ResultPathRelations},
		ReservedParameterIDs: []string{},
	}
	if !ok || source.Node == nil {
		c.diag("LDL1101", "invalid_structure_syntax", source, declaration.Range, "missing query declaration source", declaration.Address, "")
		return recipe
	}
	tokens := directTokens(source.Node)
	if len(tokens) < 3 || tokens[0].Raw != "query" || tokens[1].Raw != declaration.ID || tokens[2].Kind != syntax.TokenString {
		c.diag("LDL1601", "invalid_query_or_arguments", source, source.Range, "invalid query identity or display name", declaration.Address, "")
	} else if display, valid := authoredString(authoredValue{raw: tokens[2].Raw, kind: tokens[2].Kind, span: tokens[2].Span}); valid {
		recipe.DisplayName = display
	} else {
		c.diag("LDL1601", "invalid_query_or_arguments", source, tokens[2].Span, "invalid query display name", declaration.Address, "")
	}
	common, commonDiagnostics := definition.CompileCommonFields(source)
	recipe.Common = common
	c.diagnostics = append(c.diagnostics, commonDiagnostics...)

	members := queryBody(source.Node)
	c.validateRecipeMembers(source, members)
	recipe.Parameters = c.compileParameters(declaration)
	recipe.ReservedParameterIDs = c.reservedParameters(declaration.Symbol)
	if state := oneMember(members, "state_input"); state != nil {
		recipe.StateInput = c.compileStatePolicy(source, *state, declaration.Address)
	}
	if selectMember := oneMember(members, "select"); selectMember != nil {
		recipe.Select = c.compileSelect(source, *selectMember, declaration.Address)
	} else {
		c.diag("LDL1601", "invalid_query_or_arguments", source, source.Range, "query requires one select block", declaration.Address, "")
	}
	if where := oneMember(members, "where"); where != nil {
		recipe.Where = c.compilePredicateRoot(source, *where, resolve.KindEntity, declaration.Address)
	}
	if where := oneMember(members, "relation_where"); where != nil {
		recipe.RelationWhere = c.compilePredicateRoot(source, *where, resolve.KindRelation, declaration.Address)
	}
	if traversal := oneMember(members, "traverse"); traversal != nil {
		recipe.Traversal = c.compileTraversal(source, *traversal, declaration.Address, recipe.Select.RelationTypeAddresses)
	}
	if result := oneMember(members, "result"); result != nil {
		recipe.Result = c.compileResult(source, *result, declaration.Address)
	}
	c.validateStatePolicy(source, declaration.Address, recipe.StateInput)
	recipe.Dependencies = c.dependencies(declaration.Address)
	return recipe
}

func (c *compiler) validateRecipeMembers(source resolve.DeclarationSource, members []authoredMember) {
	type memberRule struct{ block bool }
	rules := map[string]memberRule{
		"description": {}, "tags": {}, "annotations": {},
		"reserve": {block: true}, "parameters": {block: true}, "state_input": {},
		"select": {block: true}, "where": {block: true}, "relation_where": {block: true},
		"traverse": {}, "result": {},
	}
	seen := map[string]authoredMember{}
	for _, member := range members {
		rule, known := rules[member.head]
		validShape := known && (member.block != nil) == rule.block
		if member.head == "annotations" && known {
			validShape = true
		}
		if !validShape {
			c.diag("LDL1102", "unknown_or_duplicate_schema_member", source, member.span, "unknown or invalid query member", source.Address, "")
			continue
		}
		if previous, duplicate := seen[member.head]; duplicate {
			c.diagRelated("LDL1102", "unknown_or_duplicate_schema_member", source, member.span, "duplicate query member", source.Address, "", source, previous.span)
			continue
		}
		seen[member.head] = member
	}
}

func (c *compiler) compileParameters(query resolve.DeclarationSymbol) []Parameter {
	var declarations []resolve.DeclarationSymbol
	for _, declaration := range c.declarations {
		if declaration.Kind != resolve.KindParameter || declaration.Owner == nil || resolve.CompareStableSymbols(*declaration.Owner, query.Symbol) != 0 {
			continue
		}
		declarations = append(declarations, declaration)
	}
	resolve.SortDeclarations(declarations)
	parameters := make([]Parameter, 0, len(declarations))
	for _, declaration := range declarations {
		source := c.sources[declaration.Address]
		column, diagnostics := definition.CompileScalarSchema(declaration, source)
		c.diagnostics = append(c.diagnostics, diagnostics...)
		c.parameters[column.Address] = column
		parameters = append(parameters, parameterFromColumn(column))
	}
	return parameters
}

func parameterFromColumn(column definition.Column) Parameter {
	return Parameter{
		ID: column.ID, Address: column.Address, ValueType: column.ValueType,
		EnumValues: append([]string{}, column.EnumValues...), ReservedEnumValues: append([]string{}, column.ReservedEnumValues...),
		Required: column.Required, Default: column.Default, Format: column.Format,
		Min: column.Min, Max: column.Max, MinLength: column.MinLength, MaxLength: column.MaxLength,
	}
}

func (c *compiler) reservedParameters(owner resolve.StableSymbol) []string {
	var out []string
	for _, reservation := range c.input.Resolve.Identity.Reservations {
		if reservation.Kind == resolve.KindParameter && resolve.CompareStableSymbols(reservation.Owner, owner) == 0 {
			out = append(out, reservation.ID)
		}
	}
	sort.Strings(out)
	return out
}

func (c *compiler) compileStatePolicy(source resolve.DeclarationSource, member authoredMember, subject string) StatePolicy {
	if member.block != nil || len(member.args) != 1 {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.span, "state_input requires one policy", subject, "")
		return StateNone
	}
	switch StatePolicy(member.args[0].raw) {
	case StateNone, StateOptional, StateRequired:
		return StatePolicy(member.args[0].raw)
	default:
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.args[0].span, "invalid query state policy", subject, "")
		return StateNone
	}
}

func (c *compiler) compileSelect(source resolve.DeclarationSource, member authoredMember, subject string) Select {
	var selected Select
	rules := map[string]struct {
		kind resolve.SubjectKind
		set  func(*[]string)
	}{
		"layers":         {resolve.KindLayer, func(values *[]string) { selected.LayerAddresses = values }},
		"entity_types":   {resolve.KindEntityType, func(values *[]string) { selected.EntityTypeAddresses = values }},
		"relation_types": {resolve.KindRelationType, func(values *[]string) { selected.RelationTypeAddresses = values }},
		"roots":          {resolve.KindEntity, func(values *[]string) { selected.RootAddresses = values }},
	}
	seen := map[string]authoredMember{}
	for _, child := range readMembers(member.block) {
		rule, known := rules[child.head]
		if !known || child.block != nil {
			c.diag("LDL1601", "invalid_query_or_arguments", source, child.span, "invalid query selector", subject, "")
			continue
		}
		if previous, duplicate := seen[child.head]; duplicate {
			c.diagRelated("LDL1601", "invalid_query_or_arguments", source, child.span, "duplicate query selector", subject, "", source, previous.span)
			continue
		}
		seen[child.head] = child
		if len(child.args) != 1 || !child.args[0].list {
			c.diag("LDL1601", "invalid_query_or_arguments", source, child.span, "query selector requires one list", subject, "")
			continue
		}
		addresses := c.boundList(source, subject, rule.kind, child.args[0])
		rule.set(&addresses)
		if rule.kind == resolve.KindEntity {
			for _, item := range listItems(child.args[0]) {
				address, ok := c.singleBindingAt(subject, resolve.KindEntity, item.span, source)
				if ok && !c.graphEntities[address] {
					c.diag("LDL1601", "invalid_query_or_arguments", source, item.span, "explicit query root is not in the compiled graph", subject, "")
				}
			}
		}
	}
	return selected
}

func (c *compiler) boundList(source resolve.DeclarationSource, subject string, kind resolve.SubjectKind, value authoredValue) []string {
	items := listItems(value)
	addresses := make([]string, 0, len(items))
	seen := map[string]syntax.Span{}
	for _, item := range items {
		address, ok := c.singleBindingAt(subject, kind, item.span, source)
		if !ok {
			continue
		}
		if previous, duplicate := seen[address]; duplicate {
			c.diagRelated("LDL1601", "invalid_query_or_arguments", source, item.span, "duplicate query reference", subject, "", source, previous)
			continue
		}
		seen[address] = item.span
		addresses = append(addresses, address)
	}
	c.sortAddresses(addresses)
	return addresses
}

func (c *compiler) compileTraversal(source resolve.DeclarationSource, member authoredMember, subject string, selectedRelationTypes *[]string) *Traversal {
	traversal := &Traversal{}
	if member.block != nil || len(member.args) != 3 && len(member.args) != 5 {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.span, "traverse requires direction, finite range, cycle policy, and optional relation list", subject, "")
		return traversal
	}
	switch definition.TraversalDirection(member.args[0].raw) {
	case definition.TraversalOutgoing, definition.TraversalIncoming, definition.TraversalBoth:
		traversal.Direction = definition.TraversalDirection(member.args[0].raw)
	default:
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.args[0].span, "invalid traversal direction", subject, "")
	}
	minDepth, maxDepth, validRange := finiteRange(member.args[1])
	if !validRange || minDepth > maxDepth {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.args[1].span, "invalid traversal depth range", subject, "")
	} else {
		traversal.MinDepth, traversal.MaxDepth = minDepth, maxDepth
	}
	switch CyclePolicy(member.args[2].raw) {
	case CycleError, CycleVisitOnce, CycleIncludeCycleRef:
		traversal.CyclePolicy = CyclePolicy(member.args[2].raw)
	default:
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.args[2].span, "invalid traversal cycle policy", subject, "")
	}
	if len(member.args) == 5 {
		if member.args[3].raw != "relations" || !member.args[4].list {
			c.diag("LDL1601", "invalid_query_or_arguments", source, member.args[3].span, "invalid traversal relation restriction", subject, "")
			return traversal
		}
		addresses := c.boundList(source, subject, resolve.KindRelationType, member.args[4])
		traversal.RelationTypeAddresses = &addresses
		if selectedRelationTypes != nil {
			allowed := stringSet(*selectedRelationTypes)
			for _, address := range addresses {
				if !allowed[address] {
					c.diag("LDL1601", "invalid_query_or_arguments", source, member.args[4].span, "traversal relation types may only narrow select.relation_types", subject, "")
					break
				}
			}
		}
	}
	return traversal
}

func finiteRange(value authoredValue) (int64, int64, bool) {
	rangeNode := firstNode(value.node, syntax.NodeRange)
	if rangeNode == nil {
		return 0, 0, false
	}
	tokens := nodeTokens(rangeNode)
	if len(tokens) != 3 || tokens[0].Kind != syntax.TokenInteger || tokens[1].Kind != syntax.TokenDotDot || tokens[2].Kind != syntax.TokenInteger {
		return 0, 0, false
	}
	minimum, minErr := strconv.ParseInt(tokens[0].Raw, 10, 64)
	maximum, maxErr := strconv.ParseInt(tokens[2].Raw, 10, 64)
	return minimum, maximum, minErr == nil && maxErr == nil && minimum >= 0 && maximum >= 0
}

func (c *compiler) compileResult(source resolve.DeclarationSource, member authoredMember, subject string) []ResultMember {
	if member.block != nil || len(member.args) != 1 || !member.args[0].list {
		c.diag("LDL1601", "invalid_query_or_arguments", source, member.span, "result requires one list", subject, "")
		return []ResultMember{}
	}
	allowed := map[ResultMember]int{
		ResultSeedEntities: 0, ResultTraversedEntities: 1, ResultPathRelations: 2, ResultInducedRelations: 3,
	}
	seen := map[ResultMember]syntax.Span{}
	var result []ResultMember
	for _, item := range listItems(member.args[0]) {
		value := ResultMember(item.raw)
		if _, valid := allowed[value]; !valid {
			c.diag("LDL1601", "invalid_query_or_arguments", source, item.span, "invalid query result member", subject, "")
			continue
		}
		if previous, duplicate := seen[value]; duplicate {
			c.diagRelated("LDL1601", "invalid_query_or_arguments", source, item.span, "duplicate query result member", subject, "", source, previous)
			continue
		}
		seen[value] = item.span
		result = append(result, value)
	}
	sort.SliceStable(result, func(i, j int) bool { return allowed[result[i]] < allowed[result[j]] })
	return result
}

func (c *compiler) validateStatePolicy(source resolve.DeclarationSource, subject string, policy StatePolicy) {
	hasReads := len(c.stateReads) != 0
	if hasReads && policy == StateNone {
		c.diag("LDL1601", "invalid_query_or_arguments", source, source.Range, "state predicates require optional or required state_input", subject, "")
	}
	if !hasReads && policy != StateNone {
		c.diag("LDL1601", "invalid_query_or_arguments", source, source.Range, "state_input is forbidden without a state predicate", subject, "")
	}
}

func (c *compiler) dependencies(sourceAddress string) Dependencies {
	sets := map[resolve.SubjectKind]map[string]bool{}
	for _, binding := range c.bindings[sourceAddress] {
		if sets[binding.ExpectedKind] == nil {
			sets[binding.ExpectedKind] = map[string]bool{}
		}
		sets[binding.ExpectedKind][binding.TargetAddress] = true
	}
	dep := Dependencies{
		LayerAddresses: setKeys(sets[resolve.KindLayer]), EntityTypeAddresses: setKeys(sets[resolve.KindEntityType]),
		RelationTypeAddresses: setKeys(sets[resolve.KindRelationType]), EntityAddresses: setKeys(sets[resolve.KindEntity]),
		RelationAddresses: setKeys(sets[resolve.KindRelation]), ColumnAddresses: setKeys(sets[resolve.KindColumn]),
		ParameterAddresses: setKeys(sets[resolve.KindParameter]),
		StateReads:         append([]StateReadDependency{}, c.stateReads...),
	}
	c.sortAddresses(dep.LayerAddresses)
	c.sortAddresses(dep.EntityTypeAddresses)
	c.sortAddresses(dep.RelationTypeAddresses)
	c.sortAddresses(dep.EntityAddresses)
	c.sortAddresses(dep.RelationAddresses)
	c.sortAddresses(dep.ColumnAddresses)
	c.sortAddresses(dep.ParameterAddresses)
	sortStateReads(dep.StateReads)
	dep.StateReads = dedupeStateReads(dep.StateReads)
	c.stateReads = nil
	return dep
}

func (c *compiler) singleBindingAt(sourceAddress string, kind resolve.SubjectKind, span syntax.Span, source resolve.DeclarationSource) (string, bool) {
	var matches []resolve.SourceBinding
	for _, binding := range c.bindings[sourceAddress] {
		if binding.ExpectedKind == kind && binding.Range == span {
			matches = append(matches, binding)
		}
	}
	if len(matches) != 1 {
		c.diag("LDL1301", "unknown_or_ambiguous_symbol", source, span, "expected one precise resolver binding", sourceAddress, "")
		return "", false
	}
	return matches[0].TargetAddress, true
}

func (c *compiler) bindingsAt(sourceAddress string, kind resolve.SubjectKind, span syntax.Span) []resolve.SourceBinding {
	var matches []resolve.SourceBinding
	for _, binding := range c.bindings[sourceAddress] {
		if binding.ExpectedKind == kind && binding.Range == span {
			matches = append(matches, binding)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return c.compareAddresses(matches[i].TargetAddress, matches[j].TargetAddress) < 0
	})
	return matches
}

func (c *compiler) sortAddresses(addresses []string) {
	sort.SliceStable(addresses, func(i, j int) bool { return c.compareAddresses(addresses[i], addresses[j]) < 0 })
}

func (c *compiler) compareAddresses(a, b string) int {
	aSymbol, aOK := c.symbols[a]
	bSymbol, bOK := c.symbols[b]
	if aOK && bOK {
		return resolve.CompareStableSymbols(aSymbol, bSymbol)
	}
	return strings.Compare(a, b)
}

func oneMember(members []authoredMember, head string) *authoredMember {
	for i := range members {
		if members[i].head == head {
			return &members[i]
		}
	}
	return nil
}

func setKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	return out
}

func stringSet(values []string) map[string]bool {
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
	c.diagnostics = append(c.diagnostics, resolve.Diagnostic{
		Code: code, Severity: "error", MessageKey: key, Arguments: map[string]string{}, Message: message,
		Range: sourceRange(source, span), SubjectAddress: subject, OwnerAddress: owner,
	})
}

func (c *compiler) diagRelated(code, key string, source resolve.DeclarationSource, span syntax.Span, message, subject, owner string, previousSource resolve.DeclarationSource, previous syntax.Span) {
	c.diag(code, key, source, span, message, subject, owner)
	c.diagnostics[len(c.diagnostics)-1].Related = []resolve.DiagnosticRelated{{
		Relation: "previous", Range: sourceRange(previousSource, previous), SubjectAddress: subject, OwnerAddress: owner,
	}}
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
