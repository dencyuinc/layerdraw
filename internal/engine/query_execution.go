// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"sort"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
)

type QueryExecutionInput struct {
	Recipe    CompiledQueryRecipe
	Graph     TypedMasterGraph
	Arguments map[string]TypedScalar
}

type QueryExecutionResponse struct {
	Status      string
	Result      *QueryResult
	Diagnostics []Diagnostic
}

type QueryResult struct {
	QueryAddress              string
	Arguments                 map[string]TypedScalar
	StatePolicy               string
	StateInput                QueryStateInputRef
	StateReads                []StateReadRef
	SeedEntityAddresses       []string
	ReachedEntityAddresses    []string
	TraversedEntityAddresses  []string
	PathRelationAddresses     []string
	InducedRelationAddresses  []string
	PrimaryEntityAddresses    []string
	SelectedRelationAddresses []string
	SupportEntityAddresses    []string
	Paths                     []QueryPath
	CycleRefs                 []QueryCycleRef
	Diagnostics               []Diagnostic
}

type QueryStateInputRef struct {
	Kind string
}

type StateReadRef struct {
	SubjectAddress string
	FieldPath      string
}

type QueryPath struct {
	EntityAddresses   []string
	RelationAddresses []string
}

type QueryCycleRef struct {
	Kind              string
	FromEntityAddress string
	ToEntityAddress   string
	RelationAddress   string
	Orientation       string
	RetainedPath      QueryPath
}

// ExecuteQuery evaluates one compiled Query recipe against one typed graph.
// It is a pure in-process semantic operation: it never reads storage, clock,
// network, access policy, or state backend data.
func (e Engine) ExecuteQuery(ctx context.Context, input QueryExecutionInput) QueryExecutionResponse {
	if ctx == nil {
		return rejectedQuery(diagnostic("LDL1801", "stale_revision_or_semantic_hash", "query execution requires a context", input.Recipe.Address, ""))
	}
	if err := pollContext(ctx, "query"); err != nil {
		return rejectedQuery(diagnostic("LDL1801", "stale_revision_or_semantic_hash", err.Error(), input.Recipe.Address, ""))
	}
	executor := newQueryExecutor(input)
	if !executor.validateArguments() {
		return rejectedQuery(executor.diagnostics...)
	}
	if !executor.validateStateInput() {
		return rejectedQuery(executor.diagnostics...)
	}
	result := executor.execute()
	if err := pollContext(ctx, "query"); err != nil {
		return rejectedQuery(diagnostic("LDL1801", "stale_revision_or_semantic_hash", err.Error(), input.Recipe.Address, ""))
	}
	if hasQueryError(result.Diagnostics) {
		return rejectedQuery(result.Diagnostics...)
	}
	return QueryExecutionResponse{Status: "ok", Result: &result}
}

type queryExecutor struct {
	recipe      query.Recipe
	graph       graph.MasterGraph
	arguments   map[string]definition.Scalar
	entities    map[string]graph.Entity
	relations   map[string]graph.Relation
	outgoing    map[string][]string
	incoming    map[string][]string
	diagnostics []Diagnostic
	stateReads  map[StateReadRef]bool
}

func newQueryExecutor(input QueryExecutionInput) *queryExecutor {
	e := &queryExecutor{
		recipe:     input.Recipe,
		graph:      input.Graph,
		arguments:  cloneScalars(input.Arguments),
		entities:   map[string]graph.Entity{},
		relations:  map[string]graph.Relation{},
		outgoing:   map[string][]string{},
		incoming:   map[string][]string{},
		stateReads: map[StateReadRef]bool{},
	}
	for _, entity := range input.Graph.Entities {
		e.entities[entity.Address] = entity
	}
	for _, relation := range input.Graph.Relations {
		e.relations[relation.Address] = relation
	}
	for _, adjacency := range input.Graph.Outgoing {
		e.outgoing[adjacency.EntityAddress] = append([]string{}, adjacency.RelationAddresses...)
	}
	for _, adjacency := range input.Graph.Incoming {
		e.incoming[adjacency.EntityAddress] = append([]string{}, adjacency.RelationAddresses...)
	}
	return e
}

func (e *queryExecutor) validateArguments() bool {
	known := map[string]query.Parameter{}
	for _, parameter := range e.recipe.Parameters {
		known[parameter.Address] = parameter
	}
	for address := range e.arguments {
		if _, ok := known[address]; !ok {
			e.addDiag("LDL1601", "invalid_query_or_arguments", "unknown query argument", address, e.recipe.Address)
		}
	}
	for _, parameter := range e.recipe.Parameters {
		value, ok := e.arguments[parameter.Address]
		if !ok && parameter.Default != nil {
			e.arguments[parameter.Address] = *parameter.Default
			ok = true
			value = *parameter.Default
		}
		if !ok {
			if parameter.Required {
				e.addDiag("LDL1601", "invalid_query_or_arguments", "required query argument is missing", parameter.Address, e.recipe.Address)
			}
			continue
		}
		if !argumentMatchesParameter(value, parameter) {
			e.addDiag("LDL1601", "invalid_query_or_arguments", "query argument does not satisfy its parameter schema", parameter.Address, e.recipe.Address)
		}
	}
	return len(e.diagnostics) == 0
}

func argumentMatchesParameter(value definition.Scalar, parameter query.Parameter) bool {
	if value.Type != parameter.ValueType {
		return false
	}
	if value.Type == definition.ScalarEnum && len(parameter.EnumValues) != 0 && !stringIn(value.String, parameter.EnumValues) {
		return false
	}
	if parameter.Min != nil {
		if value.Type == definition.ScalarInteger && float64(value.Int) < *parameter.Min {
			return false
		}
		if value.Type == definition.ScalarNumber && value.Float < *parameter.Min {
			return false
		}
	}
	if parameter.Max != nil {
		if value.Type == definition.ScalarInteger && float64(value.Int) > *parameter.Max {
			return false
		}
		if value.Type == definition.ScalarNumber && value.Float > *parameter.Max {
			return false
		}
	}
	if parameter.MinLength != nil && int64(len([]rune(value.String))) < *parameter.MinLength {
		return false
	}
	if parameter.MaxLength != nil && int64(len([]rune(value.String))) > *parameter.MaxLength {
		return false
	}
	return true
}

func (e *queryExecutor) validateStateInput() bool {
	switch e.recipe.StateInput {
	case query.StateNone:
		return true
	case query.StateOptional:
		e.addWarning("LDL1605", "optional_state_snapshot_missing", "optional StateQuerySnapshot is absent; state predicates evaluate as missing", e.recipe.Address, "")
		return true
	case query.StateRequired:
		e.addDiag("LDL1604", "required_state_snapshot_missing", "required StateQuerySnapshot is absent", e.recipe.Address, "")
		return false
	default:
		e.addDiag("LDL1601", "invalid_query_or_arguments", "invalid query state policy", e.recipe.Address, "")
		return false
	}
}

func (e *queryExecutor) execute() QueryResult {
	eligibleEntities := e.eligibleEntities()
	eligibleRelations := e.eligibleRelations(eligibleEntities)
	seeds := e.seedEntities(eligibleEntities)
	paths, reached, traversed, pathRelations, cycleRefs, traversalOK := e.traverse(seeds, eligibleEntities, eligibleRelations)
	if !traversalOK {
		return QueryResult{
			QueryAddress: e.recipe.Address,
			Arguments:    cloneScalars(e.arguments),
			StatePolicy:  string(e.recipe.StateInput),
			StateInput:   QueryStateInputRef{Kind: "none"},
			StateReads:   e.sortedStateReads(),
			Diagnostics:  sortedDiagnostics(e.diagnostics),
		}
	}
	induced := e.inducedRelations(seeds, traversed, pathRelations, eligibleRelations)
	primary := map[string]bool{}
	if resultIncludes(e.recipe.Result, query.ResultSeedEntities) {
		for _, address := range seeds {
			primary[address] = true
		}
	}
	if resultIncludes(e.recipe.Result, query.ResultTraversedEntities) {
		for _, address := range traversed {
			primary[address] = true
		}
	}
	selectedRelations := map[string]bool{}
	if resultIncludes(e.recipe.Result, query.ResultPathRelations) {
		for _, address := range pathRelations {
			selectedRelations[address] = true
		}
	}
	if resultIncludes(e.recipe.Result, query.ResultInducedRelations) {
		for _, address := range induced {
			selectedRelations[address] = true
		}
	}
	support := e.supportEntities(primary, selectedRelations, paths)
	return QueryResult{
		QueryAddress:              e.recipe.Address,
		Arguments:                 cloneScalars(e.arguments),
		StatePolicy:               string(e.recipe.StateInput),
		StateInput:                QueryStateInputRef{Kind: "none"},
		StateReads:                e.sortedStateReads(),
		SeedEntityAddresses:       seeds,
		ReachedEntityAddresses:    sortStrings(reached),
		TraversedEntityAddresses:  sortStrings(traversed),
		PathRelationAddresses:     e.sortRelationAddresses(pathRelations),
		InducedRelationAddresses:  e.sortRelationAddresses(induced),
		PrimaryEntityAddresses:    sortSet(primary),
		SelectedRelationAddresses: e.sortRelationAddresses(setKeys(selectedRelations)),
		SupportEntityAddresses:    sortSet(support),
		Paths:                     sortPaths(paths),
		CycleRefs:                 sortCycleRefs(cycleRefs),
		Diagnostics:               sortedDiagnostics(e.diagnostics),
	}
}

func (e *queryExecutor) eligibleEntities() map[string]bool {
	out := map[string]bool{}
	for _, entity := range e.graph.Entities {
		if e.selects(e.recipe.Select.LayerAddresses, entity.LayerAddress) &&
			e.selects(e.recipe.Select.EntityTypeAddresses, entity.TypeAddress) &&
			e.evalEntityPredicate(e.recipe.Where, entity) {
			out[entity.Address] = true
		}
	}
	return out
}

func (e *queryExecutor) eligibleRelations(eligibleEntities map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, relation := range e.graph.Relations {
		if !eligibleEntities[relation.FromAddress] || !eligibleEntities[relation.ToAddress] {
			continue
		}
		if e.selects(e.recipe.Select.RelationTypeAddresses, relation.TypeAddress) && e.evalRelationPredicate(e.recipe.RelationWhere, relation) {
			out[relation.Address] = true
		}
	}
	return out
}

func (e *queryExecutor) seedEntities(eligible map[string]bool) []string {
	var seeds []string
	if e.recipe.Select.RootAddresses != nil {
		for _, address := range *e.recipe.Select.RootAddresses {
			if eligible[address] {
				seeds = append(seeds, address)
			}
		}
		return sortStrings(seeds)
	}
	for _, entity := range e.graph.Entities {
		if eligible[entity.Address] {
			seeds = append(seeds, entity.Address)
		}
	}
	return sortStrings(seeds)
}

func (e *queryExecutor) traverse(seeds []string, eligibleEntities, eligibleRelations map[string]bool) ([]QueryPath, []string, []string, []string, []QueryCycleRef, bool) {
	paths := make([]QueryPath, 0, len(seeds))
	visited := map[string]QueryPath{}
	var frontier []QueryPath
	for _, seed := range seeds {
		path := QueryPath{EntityAddresses: []string{seed}, RelationAddresses: []string{}}
		paths = append(paths, path)
		visited[seed] = path
		frontier = append(frontier, path)
	}
	if e.recipe.Traversal == nil {
		return paths, nil, nil, nil, nil, true
	}
	traversal := *e.recipe.Traversal
	reached := map[string]bool{}
	traversed := map[string]bool{}
	pathRelations := map[string]bool{}
	var cycleRefs []QueryCycleRef
	for depth := int64(0); depth < traversal.MaxDepth && len(frontier) != 0; depth++ {
		var nextFrontier []QueryPath
		for _, path := range frontier {
			current := path.EntityAddresses[len(path.EntityAddresses)-1]
			for _, candidate := range e.candidateRelations(current, traversal, eligibleRelations, eligibleEntities) {
				nextAddress := candidate.next
				nextPath := appendPath(path, candidate.relation, nextAddress)
				kind := ""
				if pathContains(path.EntityAddresses, nextAddress) {
					kind = "cycle"
				} else if _, seen := visited[nextAddress]; seen {
					kind = "merge"
				}
				if kind != "" {
					if traversal.CyclePolicy == query.CycleError && kind == "cycle" {
						e.addDiag("LDL1601", "invalid_query_or_arguments", "query traversal encountered a cycle", e.recipe.Address, "")
						return paths, setKeys(reached), setKeys(traversed), setKeys(pathRelations), cycleRefs, false
					}
					if traversal.CyclePolicy == query.CycleIncludeCycleRef {
						retained := visited[nextAddress]
						if kind == "cycle" {
							retained = path
						}
						cycleRefs = append(cycleRefs, QueryCycleRef{
							Kind: kind, FromEntityAddress: current, ToEntityAddress: nextAddress,
							RelationAddress: candidate.relation, Orientation: candidate.orientation, RetainedPath: retained,
						})
					}
					continue
				}
				visited[nextAddress] = nextPath
				paths = append(paths, nextPath)
				reached[nextAddress] = true
				nextDepth := depth + 1
				if nextDepth >= traversal.MinDepth {
					traversed[nextAddress] = true
					for _, relationAddress := range nextPath.RelationAddresses {
						pathRelations[relationAddress] = true
					}
				}
				nextFrontier = append(nextFrontier, nextPath)
			}
		}
		frontier = sortPaths(nextFrontier)
	}
	return paths, setKeys(reached), setKeys(traversed), setKeys(pathRelations), cycleRefs, true
}

type relationCandidate struct {
	relation    string
	next        string
	orientation string
}

func (e *queryExecutor) candidateRelations(entityAddress string, traversal query.Traversal, eligibleRelations, eligibleEntities map[string]bool) []relationCandidate {
	relationTypes := map[string]bool{}
	if traversal.RelationTypeAddresses != nil {
		for _, address := range *traversal.RelationTypeAddresses {
			relationTypes[address] = true
		}
	}
	var out []relationCandidate
	add := func(addresses []string, orientation string) {
		for _, relationAddress := range addresses {
			relation, ok := e.relations[relationAddress]
			if !ok || !eligibleRelations[relationAddress] {
				continue
			}
			if traversal.RelationTypeAddresses != nil && !relationTypes[relation.TypeAddress] {
				continue
			}
			next := relation.ToAddress
			if orientation == "incoming" {
				next = relation.FromAddress
			}
			if !eligibleEntities[next] {
				continue
			}
			out = append(out, relationCandidate{relation: relationAddress, next: next, orientation: orientation})
		}
	}
	switch traversal.Direction {
	case definition.TraversalOutgoing:
		add(e.outgoing[entityAddress], "outgoing")
	case definition.TraversalIncoming:
		add(e.incoming[entityAddress], "incoming")
	case definition.TraversalBoth:
		add(e.outgoing[entityAddress], "outgoing")
		add(e.incoming[entityAddress], "incoming")
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := e.relations[out[i].relation], e.relations[out[j].relation]
		ak := []string{a.TypeAddress, a.Address, a.FromAddress, a.ToAddress, out[i].orientation}
		bk := []string{b.TypeAddress, b.Address, b.FromAddress, b.ToAddress, out[j].orientation}
		for index := range ak {
			if ak[index] != bk[index] {
				return ak[index] < bk[index]
			}
		}
		return false
	})
	return out
}

func (e *queryExecutor) inducedRelations(seeds, traversed, pathRelations []string, eligibleRelations map[string]bool) []string {
	entities := map[string]bool{}
	for _, address := range seeds {
		entities[address] = true
	}
	for _, address := range traversed {
		entities[address] = true
	}
	paths := queryStringSet(pathRelations)
	var induced []string
	for _, relation := range e.graph.Relations {
		if eligibleRelations[relation.Address] && !paths[relation.Address] && entities[relation.FromAddress] && entities[relation.ToAddress] {
			induced = append(induced, relation.Address)
		}
	}
	return e.sortRelationAddresses(induced)
}

func (e *queryExecutor) supportEntities(primary, selectedRelations map[string]bool, paths []QueryPath) map[string]bool {
	support := map[string]bool{}
	for address := range selectedRelations {
		relation := e.relations[address]
		support[relation.FromAddress] = true
		support[relation.ToAddress] = true
	}
	for _, path := range paths {
		containsSelected := false
		for _, relationAddress := range path.RelationAddresses {
			if selectedRelations[relationAddress] {
				containsSelected = true
				break
			}
		}
		if containsSelected {
			for _, entityAddress := range path.EntityAddresses {
				support[entityAddress] = true
			}
		}
	}
	for address := range primary {
		delete(support, address)
	}
	return support
}

func (e *queryExecutor) evalEntityPredicate(predicate query.Predicate, entity graph.Entity) bool {
	switch predicate.Kind {
	case query.PredicateAll:
		for _, child := range predicate.Children {
			if !e.evalEntityPredicate(child, entity) {
				return false
			}
		}
		return true
	case query.PredicateAny:
		for _, child := range predicate.Children {
			if e.evalEntityPredicate(child, entity) {
				return true
			}
		}
		return false
	case query.PredicateNot:
		return predicate.Child != nil && !e.evalEntityPredicate(*predicate.Child, entity)
	case query.PredicateField:
		return e.evalPredicateValue(entityFieldValue(entity, predicate.Field), predicate.Operator, predicate.Value)
	case query.PredicateRows:
		return e.evalRowsPredicate(predicate, entity.TypeAddress, entity.Rows)
	case query.PredicateState:
		return e.evalStatePredicate(entity.Address, predicate.FieldPath, predicate.Operator, predicate.Value)
	default:
		return false
	}
}

func (e *queryExecutor) evalRelationPredicate(predicate query.Predicate, relation graph.Relation) bool {
	switch predicate.Kind {
	case query.PredicateAll:
		for _, child := range predicate.Children {
			if !e.evalRelationPredicate(child, relation) {
				return false
			}
		}
		return true
	case query.PredicateAny:
		for _, child := range predicate.Children {
			if e.evalRelationPredicate(child, relation) {
				return true
			}
		}
		return false
	case query.PredicateNot:
		return predicate.Child != nil && !e.evalRelationPredicate(*predicate.Child, relation)
	case query.PredicateField:
		return e.evalPredicateValue(relationFieldValue(relation, predicate.Field), predicate.Operator, predicate.Value)
	case query.PredicateRows:
		return e.evalRowsPredicate(predicate, relation.TypeAddress, relation.Rows)
	case query.PredicateState:
		return e.evalStatePredicate(relation.Address, predicate.FieldPath, predicate.Operator, predicate.Value)
	default:
		return false
	}
}

func (e *queryExecutor) evalRowsPredicate(predicate query.Predicate, ownerTypeAddress string, rows []graph.AttributeRow) bool {
	if !stringIn(ownerTypeAddress, predicate.TypeAddresses) || predicate.Row == nil {
		return predicate.Quantifier == query.RowsNone
	}
	matches := 0
	for _, row := range rows {
		if e.evalRowPredicate(*predicate.Row, row) {
			matches++
		}
	}
	switch predicate.Quantifier {
	case query.RowsAny:
		return matches > 0
	case query.RowsAll:
		return len(rows) > 0 && matches == len(rows)
	case query.RowsNone:
		return matches == 0
	default:
		return false
	}
}

func (e *queryExecutor) evalRowPredicate(predicate query.RowPredicate, row graph.AttributeRow) bool {
	switch predicate.Kind {
	case query.PredicateAll:
		for _, child := range predicate.Children {
			if !e.evalRowPredicate(child, row) {
				return false
			}
		}
		return true
	case query.PredicateAny:
		for _, child := range predicate.Children {
			if e.evalRowPredicate(child, row) {
				return true
			}
		}
		return false
	case query.PredicateNot:
		return predicate.Child != nil && !e.evalRowPredicate(*predicate.Child, row)
	case query.PredicateCell:
		return e.evalPredicateValue(rowCellValue(row, predicate.ColumnAddresses), predicate.Operator, predicate.Value)
	case query.PredicateState:
		return e.evalStatePredicate(row.Address, predicate.FieldPath, predicate.Operator, predicate.Value)
	default:
		return false
	}
}

func (e *queryExecutor) evalStatePredicate(subjectAddress string, fieldPath query.StateFieldPath, operator query.Operator, value *query.PredicateValue) bool {
	e.stateReads[StateReadRef{SubjectAddress: subjectAddress, FieldPath: string(fieldPath)}] = true
	return e.evalPredicateValue(optionalScalar{}, operator, value)
}

type optionalScalar struct {
	value   definition.Scalar
	present bool
	strings []string
	address *string
}

func entityFieldValue(entity graph.Entity, field string) optionalScalar {
	switch field {
	case "id":
		return stringValue(entity.ID)
	case "display_name":
		return stringValue(entity.DisplayName)
	case "description":
		if entity.Description == nil {
			return optionalScalar{}
		}
		return stringValue(*entity.Description)
	case "address":
		return addressValue(entity.Address)
	case "type":
		return addressValue(entity.TypeAddress)
	case "layer":
		return addressValue(entity.LayerAddress)
	case "tags":
		return optionalScalar{present: true, strings: append([]string{}, entity.Tags...)}
	default:
		return optionalScalar{}
	}
}

func relationFieldValue(relation graph.Relation, field string) optionalScalar {
	switch field {
	case "id":
		return stringValue(relation.ID)
	case "display_name":
		if relation.DisplayName == nil {
			return optionalScalar{}
		}
		return stringValue(*relation.DisplayName)
	case "description":
		if relation.Description == nil {
			return optionalScalar{}
		}
		return stringValue(*relation.Description)
	case "address":
		return addressValue(relation.Address)
	case "type":
		return addressValue(relation.TypeAddress)
	case "from":
		return addressValue(relation.FromAddress)
	case "to":
		return addressValue(relation.ToAddress)
	case "tags":
		return optionalScalar{present: true, strings: append([]string{}, relation.Tags...)}
	default:
		return optionalScalar{}
	}
}

func rowCellValue(row graph.AttributeRow, columnAddresses []string) optionalScalar {
	allowed := queryStringSet(columnAddresses)
	for _, cell := range row.Values {
		if allowed[cell.ColumnAddress] {
			return optionalScalar{value: cell.Value, present: true}
		}
	}
	return optionalScalar{}
}

func stringValue(value string) optionalScalar {
	return optionalScalar{value: definition.Scalar{Type: definition.ScalarString, String: definition.NormalizeText(value)}, present: true}
}

func addressValue(value string) optionalScalar {
	copied := value
	return optionalScalar{present: true, address: &copied}
}

func (e *queryExecutor) evalPredicateValue(left optionalScalar, operator query.Operator, right *query.PredicateValue) bool {
	if !left.present {
		return operator == query.OperatorMissing
	}
	if operator == query.OperatorExists {
		return true
	}
	if operator == query.OperatorMissing {
		return false
	}
	if right == nil {
		return false
	}
	right = e.resolveParameter(right)
	if left.address != nil {
		return compareAddress(*left.address, operator, right)
	}
	if left.strings != nil {
		return compareStringSet(left.strings, operator, right)
	}
	return compareScalar(left.value, operator, right)
}

func (e *queryExecutor) resolveParameter(value *query.PredicateValue) *query.PredicateValue {
	if value.Kind != query.ValueParameter {
		return value
	}
	scalar, ok := e.arguments[value.ParameterAddress]
	if !ok {
		return &query.PredicateValue{Kind: query.ValueLiteral}
	}
	return &query.PredicateValue{Kind: query.ValueLiteral, Scalar: &scalar}
}

func compareAddress(left string, operator query.Operator, right *query.PredicateValue) bool {
	switch operator {
	case query.OperatorEqual:
		return right.Address != nil && left == *right.Address
	case query.OperatorNotEqual:
		return right.Address != nil && left != *right.Address
	case query.OperatorIn:
		return stringIn(left, right.Addresses)
	case query.OperatorNotIn:
		return !stringIn(left, right.Addresses)
	default:
		return false
	}
}

func compareStringSet(left []string, operator query.Operator, right *query.PredicateValue) bool {
	values := append([]string{}, left...)
	sort.Strings(values)
	switch operator {
	case query.OperatorEqual:
		return scalarStringsEqual(values, right.Scalars)
	case query.OperatorNotEqual:
		return !scalarStringsEqual(values, right.Scalars)
	case query.OperatorContains:
		return right.Scalar != nil && stringIn(right.Scalar.String, values)
	default:
		return false
	}
}

func compareScalar(left definition.Scalar, operator query.Operator, right *query.PredicateValue) bool {
	switch operator {
	case query.OperatorEqual:
		return right.Scalar != nil && left == *right.Scalar
	case query.OperatorNotEqual:
		return right.Scalar != nil && left != *right.Scalar
	case query.OperatorIn:
		return scalarIn(left, right.Scalars)
	case query.OperatorNotIn:
		return !scalarIn(left, right.Scalars)
	case query.OperatorContains:
		return right.Scalar != nil && left.Type == definition.ScalarString && strings.Contains(left.String, right.Scalar.String)
	case query.OperatorStartsWith:
		return right.Scalar != nil && left.Type == definition.ScalarString && strings.HasPrefix(left.String, right.Scalar.String)
	case query.OperatorEndsWith:
		return right.Scalar != nil && left.Type == definition.ScalarString && strings.HasSuffix(left.String, right.Scalar.String)
	case query.OperatorLess, query.OperatorLessEqual, query.OperatorGreater, query.OperatorGreaterEq:
		if right.Scalar == nil {
			return false
		}
		cmp, ok := scalarCompare(left, *right.Scalar)
		if !ok {
			return false
		}
		return operatorCompare(cmp, operator)
	default:
		return false
	}
}

func scalarCompare(left, right definition.Scalar) (int, bool) {
	if left.Type != right.Type {
		return 0, false
	}
	switch left.Type {
	case definition.ScalarInteger:
		return compareInt(left.Int, right.Int), true
	case definition.ScalarNumber:
		return compareFloat(left.Float, right.Float), true
	case definition.ScalarDate, definition.ScalarDatetime:
		return strings.Compare(left.String, right.String), true
	default:
		return 0, false
	}
}

func operatorCompare(cmp int, operator query.Operator) bool {
	switch operator {
	case query.OperatorLess:
		return cmp < 0
	case query.OperatorLessEqual:
		return cmp <= 0
	case query.OperatorGreater:
		return cmp > 0
	case query.OperatorGreaterEq:
		return cmp >= 0
	default:
		return false
	}
}

func (e *queryExecutor) selects(selector *[]string, address string) bool {
	return selector == nil || stringIn(address, *selector)
}

func (e *queryExecutor) sortRelationAddresses(addresses []string) []string {
	out := append([]string{}, addresses...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := e.relations[out[i]], e.relations[out[j]]
		ak := []string{a.TypeAddress, a.Address, a.FromAddress, a.ToAddress}
		bk := []string{b.TypeAddress, b.Address, b.FromAddress, b.ToAddress}
		for index := range ak {
			if ak[index] != bk[index] {
				return ak[index] < bk[index]
			}
		}
		return false
	})
	return dedupeStrings(out)
}

func (e *queryExecutor) sortedStateReads() []StateReadRef {
	out := make([]StateReadRef, 0, len(e.stateReads))
	for read := range e.stateReads {
		out = append(out, read)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SubjectAddress != out[j].SubjectAddress {
			return out[i].SubjectAddress < out[j].SubjectAddress
		}
		return out[i].FieldPath < out[j].FieldPath
	})
	return out
}

func (e *queryExecutor) addDiag(code, key, message, subject, owner string) {
	e.diagnostics = append(e.diagnostics, diagnostic(code, key, message, subject, owner))
}

func (e *queryExecutor) addWarning(code, key, message, subject, owner string) {
	d := diagnostic(code, key, message, subject, owner)
	d.Severity = "warning"
	e.diagnostics = append(e.diagnostics, d)
}

func diagnostic(code, key, message, subject, owner string) Diagnostic {
	return Diagnostic{Code: code, Severity: "error", MessageKey: key, Message: message, SubjectAddress: subject, OwnerAddress: owner, Arguments: map[string]string{}}
}

func rejectedQuery(diagnostics ...Diagnostic) QueryExecutionResponse {
	return QueryExecutionResponse{Status: "rejected", Diagnostics: sortedDiagnostics(diagnostics)}
}

func sortedDiagnostics(diagnostics []Diagnostic) []Diagnostic {
	out := resolve.CloneDiagnostics(diagnostics)
	resolve.SortDiagnostics(out)
	return out
}

func hasQueryError(diagnostics []Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == "error" {
			return true
		}
	}
	return false
}

func cloneScalars(values map[string]definition.Scalar) map[string]definition.Scalar {
	out := map[string]definition.Scalar{}
	for key, value := range values {
		out[key] = value
	}
	return out
}

func resultIncludes(members []query.ResultMember, target query.ResultMember) bool {
	for _, member := range members {
		if member == target {
			return true
		}
	}
	return false
}

func appendPath(path QueryPath, relationAddress, entityAddress string) QueryPath {
	return QueryPath{
		EntityAddresses:   append(append([]string{}, path.EntityAddresses...), entityAddress),
		RelationAddresses: append(append([]string{}, path.RelationAddresses...), relationAddress),
	}
}

func pathContains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func sortPaths(paths []QueryPath) []QueryPath {
	out := append([]QueryPath{}, paths...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		aEnd, bEnd := "", ""
		if len(a.EntityAddresses) != 0 {
			aEnd = a.EntityAddresses[len(a.EntityAddresses)-1]
		}
		if len(b.EntityAddresses) != 0 {
			bEnd = b.EntityAddresses[len(b.EntityAddresses)-1]
		}
		if aEnd != bEnd {
			return aEnd < bEnd
		}
		return pathKey(a) < pathKey(b)
	})
	return out
}

func sortCycleRefs(refs []QueryCycleRef) []QueryCycleRef {
	out := append([]QueryCycleRef{}, refs...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		ak := []string{a.Kind, a.FromEntityAddress, a.ToEntityAddress, a.RelationAddress, a.Orientation, pathKey(a.RetainedPath)}
		bk := []string{b.Kind, b.FromEntityAddress, b.ToEntityAddress, b.RelationAddress, b.Orientation, pathKey(b.RetainedPath)}
		for index := range ak {
			if ak[index] != bk[index] {
				return ak[index] < bk[index]
			}
		}
		return false
	})
	return out
}

func pathKey(path QueryPath) string {
	return strings.Join(path.EntityAddresses, "\x00") + "\x01" + strings.Join(path.RelationAddresses, "\x00")
}

func queryStringSet(values []string) map[string]bool {
	out := map[string]bool{}
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
	return sortStrings(out)
}

func sortSet(values map[string]bool) []string {
	return sortStrings(setKeys(values))
}

func sortStrings(values []string) []string {
	out := append([]string{}, values...)
	sort.Strings(out)
	return dedupeStrings(out)
}

func dedupeStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:0]
	var previous string
	for _, value := range values {
		if len(out) != 0 && value == previous {
			continue
		}
		out = append(out, value)
		previous = value
	}
	return out
}

func stringIn(value string, values []string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func scalarIn(value definition.Scalar, values []definition.Scalar) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func scalarStringsEqual(left []string, right []definition.Scalar) bool {
	if len(left) != len(right) {
		return false
	}
	values := make([]string, len(right))
	for index, scalar := range right {
		values[index] = scalar.String
	}
	sort.Strings(values)
	for index := range left {
		if left[index] != values[index] {
			return false
		}
	}
	return true
}

func compareInt(left, right int64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func compareFloat(left, right float64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
