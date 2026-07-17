// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
)

type QueryExecutionInput struct {
	Recipe        CompiledQueryRecipe
	Graph         TypedMasterGraph
	Definition    QueryDefinitionIdentity
	StateSnapshot *StateQuerySnapshot
	Arguments     map[string]TypedScalar
	Limits        QueryExecutionLimits
}

// QueryExecutionLimits bound the pure evaluator independently from any host
// transport. Zero fields select deterministic Engine defaults.
type QueryExecutionLimits struct {
	MaxItems int64
	MaxWork  int64
}

func DefaultQueryExecutionLimits() QueryExecutionLimits {
	return QueryExecutionLimits{MaxItems: 1_000_000, MaxWork: 10_000_000}
}

type QueryExecutionErrorCategory string

const (
	QueryExecutionErrorCancelled QueryExecutionErrorCategory = "cancelled"
	QueryExecutionErrorInvariant QueryExecutionErrorCategory = "invariant"
	QueryExecutionErrorResource  QueryExecutionErrorCategory = "resource"
)

// QueryExecutionError represents an operational failure. Invalid recipes or
// arguments remain deterministic QueryExecutionResponse rejections.
type QueryExecutionError struct {
	Code     string
	Category QueryExecutionErrorCategory
	Resource string
	Limit    int64
	Observed int64
	cause    error
}

func (e *QueryExecutionError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Resource != "" {
		return fmt.Sprintf("%s: %s observed %d exceeds limit %d", e.Code, e.Resource, e.Observed, e.Limit)
	}
	return e.Code
}

func (e *QueryExecutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func IsQueryExecutionError(err error, category QueryExecutionErrorCategory) bool {
	var target *QueryExecutionError
	return errors.As(err, &target) && target.Category == category
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
	Kind           string
	SnapshotHash   string
	StateVersion   string
	CapturedAt     string
	DefinitionHash string
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
func (e Engine) ExecuteQuery(ctx context.Context, input QueryExecutionInput) (QueryExecutionResponse, error) {
	if ctx == nil {
		return QueryExecutionResponse{}, &QueryExecutionError{Code: "engine.query.nil_context", Category: QueryExecutionErrorInvariant}
	}
	limits, err := effectiveQueryExecutionLimits(input.Limits)
	if err != nil {
		return QueryExecutionResponse{}, err
	}
	executor := newQueryExecutor(ctx, input, limits)
	if executor.err != nil {
		return QueryExecutionResponse{}, executor.err
	}
	if !executor.validateArguments() {
		if executor.err != nil {
			return QueryExecutionResponse{}, executor.err
		}
		response := executor.rejected(executor.diagnostics...)
		return response, executor.err
	}
	if !executor.validateStateInput() {
		if executor.err != nil {
			return QueryExecutionResponse{}, executor.err
		}
		response := executor.rejected(executor.diagnostics...)
		return response, executor.err
	}
	result := executor.execute()
	if executor.err != nil {
		return QueryExecutionResponse{}, executor.err
	}
	if hasQueryError(result.Diagnostics) {
		response := executor.rejected(result.Diagnostics...)
		return response, executor.err
	}
	if !executor.ensureResultItems(queryResultItemCount(result)) {
		return QueryExecutionResponse{}, executor.err
	}
	return QueryExecutionResponse{Status: "ok", Result: &result}, nil
}

func effectiveQueryExecutionLimits(input QueryExecutionLimits) (QueryExecutionLimits, error) {
	defaults := DefaultQueryExecutionLimits()
	if input.MaxItems < 0 || input.MaxWork < 0 {
		return QueryExecutionLimits{}, &QueryExecutionError{Code: "engine.query.invalid_limits", Category: QueryExecutionErrorInvariant}
	}
	if input.MaxItems == 0 {
		input.MaxItems = defaults.MaxItems
	}
	if input.MaxWork == 0 {
		input.MaxWork = defaults.MaxWork
	}
	return input, nil
}

type queryExecutor struct {
	ctx                context.Context
	limits             QueryExecutionLimits
	work               int64
	err                error
	recipe             query.Recipe
	graph              graph.MasterGraph
	definition         QueryDefinitionIdentity
	stateSource        *StateQuerySnapshot
	stateInput         QueryStateInputRef
	arguments          map[string]definition.Scalar
	entities           map[string]graph.Entity
	relations          map[string]graph.Relation
	outgoing           map[string][]string
	incoming           map[string][]string
	diagnostics        []Diagnostic
	stateReads         map[StateReadRef]bool
	stateSubjects      map[string]validatedStateSubject
	stateInaccessible  map[query.StateFieldPath]bool
	currentStateHashes map[string]string
	staleStateSubjects map[string]bool
	deniedStateReads   map[StateReadRef]bool
}

func newQueryExecutor(ctx context.Context, input QueryExecutionInput, limits QueryExecutionLimits) *queryExecutor {
	e := &queryExecutor{
		ctx:                ctx,
		limits:             limits,
		recipe:             input.Recipe,
		graph:              input.Graph,
		definition:         input.Definition,
		stateSource:        input.StateSnapshot,
		stateInput:         QueryStateInputRef{Kind: "none"},
		arguments:          map[string]definition.Scalar{},
		entities:           map[string]graph.Entity{},
		relations:          map[string]graph.Relation{},
		outgoing:           map[string][]string{},
		incoming:           map[string][]string{},
		stateReads:         map[StateReadRef]bool{},
		staleStateSubjects: map[string]bool{},
		deniedStateReads:   map[StateReadRef]bool{},
	}
	e.arguments = e.cloneScalars(input.Arguments)
	if e.err != nil {
		return e
	}
	if !e.step() {
		return e
	}
	for _, entity := range input.Graph.Entities {
		if !e.step() {
			return e
		}
		e.entities[entity.Address] = entity
	}
	for _, relation := range input.Graph.Relations {
		if !e.step() {
			return e
		}
		e.relations[relation.Address] = relation
	}
	for _, adjacency := range input.Graph.Outgoing {
		if !e.step() {
			return e
		}
		if !e.charge(int64(len(adjacency.RelationAddresses))) {
			return e
		}
		e.outgoing[adjacency.EntityAddress] = append([]string{}, adjacency.RelationAddresses...)
	}
	for _, adjacency := range input.Graph.Incoming {
		if !e.step() {
			return e
		}
		if !e.charge(int64(len(adjacency.RelationAddresses))) {
			return e
		}
		e.incoming[adjacency.EntityAddress] = append([]string{}, adjacency.RelationAddresses...)
	}
	return e
}

func (e *queryExecutor) step() bool {
	return e.charge(1)
}

func (e *queryExecutor) charge(amount int64) bool {
	if e.err != nil {
		return false
	}
	if err := e.ctx.Err(); err != nil {
		e.err = &QueryExecutionError{Code: "engine.query.cancelled", Category: QueryExecutionErrorCancelled, cause: err}
		return false
	}
	if amount < 0 {
		e.err = &QueryExecutionError{Code: "engine.query.invalid_work_charge", Category: QueryExecutionErrorInvariant}
		return false
	}
	if amount > math.MaxInt64-e.work || e.work+amount > e.limits.MaxWork {
		e.err = &QueryExecutionError{Code: "engine.query.limit_exceeded", Category: QueryExecutionErrorResource, Resource: "query_work", Limit: e.limits.MaxWork, Observed: saturatingAdd(e.work, amount)}
		return false
	}
	e.work += amount
	return true
}

func (e *queryExecutor) ensureResultItems(observed int64) bool {
	if e.err != nil {
		return false
	}
	if observed > e.limits.MaxItems {
		e.err = &QueryExecutionError{Code: "engine.query.limit_exceeded", Category: QueryExecutionErrorResource, Resource: "query_result_items", Limit: e.limits.MaxItems, Observed: observed}
		return false
	}
	return true
}

func (e *queryExecutor) validateArguments() bool {
	known := map[string]query.Parameter{}
	for _, parameter := range e.recipe.Parameters {
		if !e.step() {
			return false
		}
		known[parameter.Address] = parameter
	}
	for address := range e.arguments {
		if !e.step() {
			return false
		}
		if _, ok := known[address]; !ok {
			e.addDiag("LDL1601", "invalid_query_or_arguments", "unknown query argument", address, e.recipe.Address)
		}
	}
	for _, parameter := range e.recipe.Parameters {
		if !e.step() {
			return false
		}
		value, ok := e.arguments[parameter.Address]
		if !ok && parameter.Default != nil {
			ok = true
			value = *parameter.Default
		}
		if !ok {
			if parameter.Required {
				e.addDiag("LDL1601", "invalid_query_or_arguments", "required query argument is missing", parameter.Address, e.recipe.Address)
			}
			continue
		}
		normalized, valid := e.normalizeQueryArgument(value, parameter)
		if !valid {
			if e.err != nil {
				return false
			}
			e.addDiag("LDL1601", "invalid_query_or_arguments", "query argument does not satisfy its parameter schema", parameter.Address, e.recipe.Address)
			continue
		}
		e.arguments[parameter.Address] = normalized
	}
	return len(e.diagnostics) == 0
}

func argumentMatchesParameter(value definition.Scalar, parameter query.Parameter) bool {
	_, valid := normalizeQueryArgument(value, parameter)
	return valid
}

func normalizeQueryArgument(value definition.Scalar, parameter query.Parameter) (definition.Scalar, bool) {
	return normalizeQueryArgumentObserved(value, parameter, nil)
}

func (e *queryExecutor) normalizeQueryArgument(value definition.Scalar, parameter query.Parameter) (definition.Scalar, bool) {
	return normalizeQueryArgumentObserved(value, parameter, e.charge)
}

func normalizeQueryArgumentObserved(value definition.Scalar, parameter query.Parameter, observe definition.ScalarWorkObserver) (definition.Scalar, bool) {
	return definition.NormalizeScalarValue(value, definition.Column{
		Address:            parameter.Address,
		ValueType:          parameter.ValueType,
		EnumValues:         parameter.EnumValues,
		ReservedEnumValues: parameter.ReservedEnumValues,
		Format:             parameter.Format,
		Min:                parameter.Min,
		Max:                parameter.Max,
		MinLength:          parameter.MinLength,
		MaxLength:          parameter.MaxLength,
	}, observe)
}

func (e *queryExecutor) validateStateInput() bool {
	switch e.recipe.StateInput {
	case query.StateNone:
		e.stateInput = QueryStateInputRef{Kind: "none"}
		return true
	case query.StateOptional:
		if e.stateSource == nil {
			e.addWarning("LDL1605", "optional_query_state_missing_or_stale", "optional Query state is unavailable; state predicates evaluate as missing", e.recipe.Address, "")
			e.stateInput = QueryStateInputRef{Kind: "none"}
			return true
		}
		return e.validateStateSnapshot(*e.stateSource)
	case query.StateRequired:
		if e.stateSource == nil {
			e.addDiag("LDL1604", "required_query_state_unavailable_or_stale", "required Query state is unavailable", e.recipe.Address, "")
			return false
		}
		return e.validateStateSnapshot(*e.stateSource)
	default:
		e.addDiag("LDL1601", "invalid_query_or_arguments", "invalid query state policy", e.recipe.Address, "")
		return false
	}
}

func (e *queryExecutor) execute() QueryResult {
	eligibleEntities := e.eligibleEntities()
	if e.err != nil {
		return QueryResult{}
	}
	eligibleRelations := e.eligibleRelations(eligibleEntities)
	if e.err != nil {
		return QueryResult{}
	}
	seeds := e.seedEntities(eligibleEntities)
	if e.err != nil {
		return QueryResult{}
	}
	paths, reached, traversed, pathRelations, cycleRefs, traversalOK := e.traverse(seeds, eligibleEntities, eligibleRelations)
	if e.err != nil {
		return QueryResult{}
	}
	if !traversalOK {
		return QueryResult{
			QueryAddress: e.recipe.Address,
			Arguments:    e.cloneScalars(e.arguments),
			StatePolicy:  string(e.recipe.StateInput),
			StateInput:   e.stateInput,
			StateReads:   e.sortedStateReads(),
			Diagnostics:  e.sortedDiagnostics(e.diagnostics),
		}
	}
	induced := e.inducedRelations(seeds, traversed, pathRelations, eligibleRelations)
	if e.err != nil {
		return QueryResult{}
	}
	primary := map[string]bool{}
	if resultIncludes(e.recipe.Result, query.ResultSeedEntities) {
		for _, address := range seeds {
			if !e.step() {
				return QueryResult{}
			}
			primary[address] = true
		}
	}
	if resultIncludes(e.recipe.Result, query.ResultTraversedEntities) {
		for _, address := range traversed {
			if !e.step() {
				return QueryResult{}
			}
			primary[address] = true
		}
	}
	selectedRelations := map[string]bool{}
	if resultIncludes(e.recipe.Result, query.ResultPathRelations) {
		for _, address := range pathRelations {
			if !e.step() {
				return QueryResult{}
			}
			selectedRelations[address] = true
		}
	}
	if resultIncludes(e.recipe.Result, query.ResultInducedRelations) {
		for _, address := range induced {
			if !e.step() {
				return QueryResult{}
			}
			selectedRelations[address] = true
		}
	}
	support := e.supportEntities(primary, selectedRelations, paths)
	return QueryResult{
		QueryAddress:              e.recipe.Address,
		Arguments:                 e.cloneScalars(e.arguments),
		StatePolicy:               string(e.recipe.StateInput),
		StateInput:                e.stateInput,
		StateReads:                e.sortedStateReads(),
		SeedEntityAddresses:       seeds,
		ReachedEntityAddresses:    e.sortStableAddresses(reached),
		TraversedEntityAddresses:  e.sortStableAddresses(traversed),
		PathRelationAddresses:     e.sortRelationAddresses(pathRelations),
		InducedRelationAddresses:  e.sortRelationAddresses(induced),
		PrimaryEntityAddresses:    e.sortSet(primary),
		SelectedRelationAddresses: e.sortRelationAddresses(e.mapKeys(selectedRelations)),
		SupportEntityAddresses:    e.sortSet(support),
		Paths:                     e.sortPaths(paths),
		CycleRefs:                 e.sortCycleRefs(cycleRefs),
		Diagnostics:               e.sortedDiagnostics(e.diagnostics),
	}
}

func (e *queryExecutor) eligibleEntities() map[string]bool {
	out := map[string]bool{}
	for _, entity := range e.graph.Entities {
		if !e.step() {
			return out
		}
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
		if !e.step() {
			return out
		}
		if !e.selects(e.recipe.Select.RelationTypeAddresses, relation.TypeAddress) {
			continue
		}
		predicateMatches := e.evalRelationPredicate(e.recipe.RelationWhere, relation)
		if e.err != nil {
			return out
		}
		if predicateMatches && eligibleEntities[relation.FromAddress] && eligibleEntities[relation.ToAddress] {
			out[relation.Address] = true
		}
	}
	return out
}

func (e *queryExecutor) seedEntities(eligible map[string]bool) []string {
	var seeds []string
	if e.recipe.Select.RootAddresses != nil {
		for _, address := range *e.recipe.Select.RootAddresses {
			if !e.step() {
				return nil
			}
			if eligible[address] {
				seeds = append(seeds, address)
			} else {
				e.addInfo("LDL1602", "query_root_ineligible", "query root is not eligible for the current arguments and predicates", address, e.recipe.Address)
			}
		}
		return e.sortStableAddresses(seeds)
	}
	for _, entity := range e.graph.Entities {
		if !e.step() {
			return nil
		}
		if eligible[entity.Address] {
			seeds = append(seeds, entity.Address)
		}
	}
	return e.sortStableAddresses(seeds)
}

func (e *queryExecutor) traverse(seeds []string, eligibleEntities, eligibleRelations map[string]bool) ([]QueryPath, []string, []string, []string, []QueryCycleRef, bool) {
	paths := make([]QueryPath, 0, len(seeds))
	visited := map[string]QueryPath{}
	var frontier []QueryPath
	for _, seed := range seeds {
		if !e.step() {
			return nil, nil, nil, nil, nil, false
		}
		path := QueryPath{EntityAddresses: []string{seed}, RelationAddresses: []string{}}
		paths = append(paths, path)
		visited[seed] = path
		frontier = append(frontier, path)
	}
	if !e.ensureTraversalItems(seeds, nil, nil, nil, paths, nil) {
		return nil, nil, nil, nil, nil, false
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
		if !e.step() {
			return nil, nil, nil, nil, nil, false
		}
		var nextFrontier []QueryPath
		for _, path := range frontier {
			if !e.step() {
				return nil, nil, nil, nil, nil, false
			}
			current := path.EntityAddresses[len(path.EntityAddresses)-1]
			for _, candidate := range e.candidateRelations(current, traversal, eligibleRelations, eligibleEntities) {
				if !e.step() {
					return nil, nil, nil, nil, nil, false
				}
				nextAddress := candidate.next
				nextPath := e.appendPath(path, candidate.relation, nextAddress)
				if e.err != nil {
					return nil, nil, nil, nil, nil, false
				}
				kind := ""
				if e.pathContains(path.EntityAddresses, nextAddress) {
					kind = "cycle"
				} else if _, seen := visited[nextAddress]; seen {
					kind = "merge"
				}
				if kind != "" {
					if traversal.CyclePolicy == query.CycleError && kind == "cycle" {
						e.addDiag("LDL1603", "query_cycle_policy_violation", "query traversal encountered a cycle forbidden by its cycle policy", e.recipe.Address, "")
						return paths, e.mapKeys(reached), e.mapKeys(traversed), e.mapKeys(pathRelations), cycleRefs, false
					}
					if traversal.CyclePolicy == query.CycleIncludeCycleRef {
						cycleRefs = append(cycleRefs, QueryCycleRef{
							Kind: kind, FromEntityAddress: current, ToEntityAddress: nextAddress,
							RelationAddress: candidate.relation, Orientation: candidate.orientation, RetainedPath: path,
						})
						if !e.ensureTraversalItems(seeds, reached, traversed, pathRelations, paths, cycleRefs) {
							return nil, nil, nil, nil, nil, false
						}
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
						if !e.step() {
							return nil, nil, nil, nil, nil, false
						}
						pathRelations[relationAddress] = true
					}
				}
				nextFrontier = append(nextFrontier, nextPath)
				if !e.ensureTraversalItems(seeds, reached, traversed, pathRelations, paths, cycleRefs) {
					return nil, nil, nil, nil, nil, false
				}
			}
		}
		frontier = e.sortPaths(nextFrontier)
	}
	return paths, e.mapKeys(reached), e.mapKeys(traversed), e.mapKeys(pathRelations), cycleRefs, true
}

func (e *queryExecutor) ensureTraversalItems(seeds []string, reached, traversed, pathRelations map[string]bool, paths []QueryPath, cycleRefs []QueryCycleRef) bool {
	observed := int64(len(seeds) + len(reached) + len(traversed) + len(pathRelations) + len(paths) + len(cycleRefs))
	return e.ensureResultItems(observed)
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
			if !e.step() {
				return nil
			}
			relationTypes[address] = true
		}
	}
	var out []relationCandidate
	seen := map[string]bool{}
	add := func(addresses []string, orientation string) {
		for _, relationAddress := range addresses {
			if !e.step() {
				return
			}
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
			if seen[relationAddress] {
				continue
			}
			seen[relationAddress] = true
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
	out = queryStableSort(e, out, func(left, right relationCandidate) int {
		a, b := e.relations[left.relation], e.relations[right.relation]
		if compared := compareRelationTuple(a, b); compared != 0 {
			return compared
		}
		return compareInt(int64(orientationRank(left.orientation)), int64(orientationRank(right.orientation)))
	})
	return out
}

func (e *queryExecutor) inducedRelations(seeds, traversed, pathRelations []string, eligibleRelations map[string]bool) []string {
	entities := map[string]bool{}
	for _, address := range seeds {
		if !e.step() {
			return nil
		}
		entities[address] = true
	}
	for _, address := range traversed {
		if !e.step() {
			return nil
		}
		entities[address] = true
	}
	paths := e.stringSet(pathRelations)
	var induced []string
	for _, relation := range e.graph.Relations {
		if !e.step() {
			return nil
		}
		if eligibleRelations[relation.Address] && !paths[relation.Address] && entities[relation.FromAddress] && entities[relation.ToAddress] {
			induced = append(induced, relation.Address)
		}
	}
	return e.sortRelationAddresses(induced)
}

func (e *queryExecutor) supportEntities(primary, selectedRelations map[string]bool, paths []QueryPath) map[string]bool {
	support := map[string]bool{}
	for address := range selectedRelations {
		if !e.step() {
			return nil
		}
		relation := e.relations[address]
		support[relation.FromAddress] = true
		support[relation.ToAddress] = true
	}
	for _, path := range paths {
		if !e.step() {
			return nil
		}
		containsSelected := false
		for _, relationAddress := range path.RelationAddresses {
			if !e.step() {
				return nil
			}
			if selectedRelations[relationAddress] {
				containsSelected = true
				break
			}
		}
		if containsSelected {
			for _, entityAddress := range path.EntityAddresses {
				if !e.step() {
					return nil
				}
				support[entityAddress] = true
			}
		}
	}
	for address := range primary {
		if !e.step() {
			return nil
		}
		delete(support, address)
	}
	return support
}

func (e *queryExecutor) evalEntityPredicate(predicate query.Predicate, entity graph.Entity) bool {
	if !e.step() {
		return false
	}
	switch predicate.Kind {
	case query.PredicateAll:
		result := true
		for _, child := range predicate.Children {
			matched := e.evalEntityPredicate(child, entity)
			if e.err != nil {
				return false
			}
			result = matched && result
		}
		return result
	case query.PredicateAny:
		result := false
		for _, child := range predicate.Children {
			matched := e.evalEntityPredicate(child, entity)
			if e.err != nil {
				return false
			}
			result = matched || result
		}
		return result
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
	if !e.step() {
		return false
	}
	switch predicate.Kind {
	case query.PredicateAll:
		result := true
		for _, child := range predicate.Children {
			matched := e.evalRelationPredicate(child, relation)
			if e.err != nil {
				return false
			}
			result = matched && result
		}
		return result
	case query.PredicateAny:
		result := false
		for _, child := range predicate.Children {
			matched := e.evalRelationPredicate(child, relation)
			if e.err != nil {
				return false
			}
			result = matched || result
		}
		return result
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
	if !e.step() {
		return false
	}
	if !e.stringIn(ownerTypeAddress, predicate.TypeAddresses) || predicate.Row == nil {
		return predicate.Quantifier == query.RowsNone
	}
	matches := 0
	for _, row := range rows {
		if !e.step() {
			return false
		}
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
	if !e.step() {
		return false
	}
	switch predicate.Kind {
	case query.PredicateAll:
		result := true
		for _, child := range predicate.Children {
			matched := e.evalRowPredicate(child, row)
			if e.err != nil {
				return false
			}
			result = matched && result
		}
		return result
	case query.PredicateAny:
		result := false
		for _, child := range predicate.Children {
			matched := e.evalRowPredicate(child, row)
			if e.err != nil {
				return false
			}
			result = matched || result
		}
		return result
	case query.PredicateNot:
		return predicate.Child != nil && !e.evalRowPredicate(*predicate.Child, row)
	case query.PredicateCell:
		return e.evalPredicateValue(e.rowCellValue(row, predicate.ColumnAddresses), predicate.Operator, predicate.Value)
	case query.PredicateState:
		return e.evalStatePredicate(row.Address, predicate.FieldPath, predicate.Operator, predicate.Value)
	default:
		return false
	}
}

func (e *queryExecutor) evalStatePredicate(subjectAddress string, fieldPath query.StateFieldPath, operator query.Operator, value *query.PredicateValue) bool {
	read := StateReadRef{SubjectAddress: subjectAddress, FieldPath: string(fieldPath)}
	e.stateReads[read] = true
	if e.stateInput.Kind == "none" {
		return e.evalPredicateValue(optionalScalar{}, operator, value)
	}
	if e.stateInaccessible[fieldPath] {
		e.denyStateRead(read, "state field is inaccessible")
		return e.evalPredicateValue(optionalScalar{}, operator, value)
	}
	subject, exists := e.stateSubjects[subjectAddress]
	if !exists {
		return e.evalPredicateValue(optionalScalar{}, operator, value)
	}
	if subject.ownSubjectHash != e.currentStateHashes[subjectAddress] {
		e.markStaleStateSubject(subjectAddress)
		return e.evalPredicateValue(optionalScalar{}, operator, value)
	}
	if subject.redacted[fieldPath] {
		e.denyStateRead(read, "state field is redacted")
		return e.evalPredicateValue(optionalScalar{}, operator, value)
	}
	field, present := subject.fields[fieldPath]
	if !present {
		return e.evalPredicateValue(optionalScalar{}, operator, value)
	}
	return e.evalPredicateValue(optionalScalar{value: field, present: true}, operator, value)
}

func (e *queryExecutor) markStaleStateSubject(subjectAddress string) {
	if e.staleStateSubjects[subjectAddress] {
		return
	}
	e.staleStateSubjects[subjectAddress] = true
	if e.recipe.StateInput == query.StateRequired {
		e.addDiag("LDL1604", "required_query_state_unavailable_or_stale", "required Query state record is stale", subjectAddress, e.recipe.Address)
		return
	}
	e.addWarning("LDL1605", "optional_query_state_missing_or_stale", "optional Query state record is stale; its fields evaluate as missing", subjectAddress, e.recipe.Address)
}

func (e *queryExecutor) denyStateRead(read StateReadRef, message string) {
	if e.deniedStateReads[read] {
		return
	}
	e.deniedStateReads[read] = true
	e.addDiag("LDL1904", "state_field_access_denied", message, read.SubjectAddress, e.recipe.Address)
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
		return optionalScalar{present: true, strings: entity.Tags}
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
		return optionalScalar{present: true, strings: relation.Tags}
	default:
		return optionalScalar{}
	}
}

func (e *queryExecutor) rowCellValue(row graph.AttributeRow, columnAddresses []string) optionalScalar {
	allowed := e.stringSet(columnAddresses)
	for _, cell := range row.Values {
		if !e.step() {
			return optionalScalar{}
		}
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
		return e.compareAddress(*left.address, operator, right)
	}
	if left.strings != nil {
		return e.compareStringSet(left.strings, operator, right)
	}
	return e.compareScalar(left.value, operator, right)
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

func (e *queryExecutor) compareAddress(left string, operator query.Operator, right *query.PredicateValue) bool {
	switch operator {
	case query.OperatorEqual:
		return right.Address != nil && left == *right.Address
	case query.OperatorNotEqual:
		return right.Address != nil && left != *right.Address
	case query.OperatorIn:
		return e.stringIn(left, right.Addresses)
	case query.OperatorNotIn:
		return !e.stringIn(left, right.Addresses)
	default:
		return false
	}
}

func (e *queryExecutor) compareStringSet(left []string, operator query.Operator, right *query.PredicateValue) bool {
	values := queryStableSort(e, left, strings.Compare)
	if e.err != nil {
		return false
	}
	switch operator {
	case query.OperatorEqual:
		return e.scalarStringsEqual(values, right.Scalars)
	case query.OperatorNotEqual:
		return !e.scalarStringsEqual(values, right.Scalars)
	case query.OperatorContains:
		return right.Scalar != nil && e.stringIn(right.Scalar.String, values)
	default:
		return false
	}
}

func (e *queryExecutor) compareScalar(left definition.Scalar, operator query.Operator, right *query.PredicateValue) bool {
	switch operator {
	case query.OperatorEqual:
		return right.Scalar != nil && scalarsEqual(left, *right.Scalar)
	case query.OperatorNotEqual:
		return right.Scalar != nil && !scalarsEqual(left, *right.Scalar)
	case query.OperatorIn:
		return e.scalarIn(left, right.Scalars)
	case query.OperatorNotIn:
		return !e.scalarIn(left, right.Scalars)
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
		if math.IsNaN(left.Float) || math.IsNaN(right.Float) || math.IsInf(left.Float, 0) || math.IsInf(right.Float, 0) {
			return 0, false
		}
		return compareFloat(left.Float, right.Float), true
	case definition.ScalarDate:
		return strings.Compare(left.String, right.String), true
	case definition.ScalarDatetime:
		leftTime, leftErr := time.Parse(time.RFC3339Nano, left.String)
		rightTime, rightErr := time.Parse(time.RFC3339Nano, right.String)
		if leftErr != nil || rightErr != nil {
			return 0, false
		}
		if leftTime.Before(rightTime) {
			return -1, true
		}
		if leftTime.After(rightTime) {
			return 1, true
		}
		return 0, true
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
	return selector == nil || e.stringIn(address, *selector)
}

func (e *queryExecutor) sortRelationAddresses(addresses []string) []string {
	out := queryStableSort(e, addresses, func(left, right string) int {
		return compareRelationTuple(e.relations[left], e.relations[right])
	})
	return e.dedupeStrings(out)
}

func (e *queryExecutor) sortedStateReads() []StateReadRef {
	out := make([]StateReadRef, 0, len(e.stateReads))
	for read := range e.stateReads {
		if !e.step() {
			return nil
		}
		out = append(out, read)
	}
	return queryStableSort(e, out, func(left, right StateReadRef) int {
		if compared := compareStableAddressText(left.SubjectAddress, right.SubjectAddress); compared != 0 {
			return compared
		}
		return query.CompareStateFieldPaths(query.StateFieldPath(left.FieldPath), query.StateFieldPath(right.FieldPath))
	})
}

func (e *queryExecutor) addDiag(code, key, message, subject, owner string) {
	e.appendDiagnostic(diagnostic(code, key, message, subject, owner))
}

func (e *queryExecutor) addWarning(code, key, message, subject, owner string) {
	d := diagnostic(code, key, message, subject, owner)
	d.Severity = "warning"
	e.appendDiagnostic(d)
}

func (e *queryExecutor) addInfo(code, key, message, subject, owner string) {
	d := diagnostic(code, key, message, subject, owner)
	d.Severity = "info"
	e.appendDiagnostic(d)
}

func (e *queryExecutor) appendDiagnostic(value Diagnostic) {
	if !e.ensureResultItems(int64(len(e.diagnostics) + 1)) {
		return
	}
	e.diagnostics = append(e.diagnostics, value)
}

func diagnostic(code, key, message, subject, owner string) Diagnostic {
	return Diagnostic{Code: code, Severity: "error", MessageKey: key, Message: message, SubjectAddress: subject, OwnerAddress: owner, Arguments: map[string]string{}}
}

func (e *queryExecutor) rejected(diagnostics ...Diagnostic) QueryExecutionResponse {
	return QueryExecutionResponse{Status: "rejected", Diagnostics: e.sortedDiagnostics(diagnostics)}
}

func (e *queryExecutor) sortedDiagnostics(diagnostics []Diagnostic) []Diagnostic {
	out := make([]Diagnostic, len(diagnostics))
	for index, value := range diagnostics {
		if !e.step() {
			return nil
		}
		if value.Range != nil || len(value.Related) != 0 || len(value.Arguments) != 0 {
			e.err = &QueryExecutionError{Code: "engine.query.diagnostic_invariant", Category: QueryExecutionErrorInvariant}
			return nil
		}
		out[index] = value
		out[index].Arguments = map[string]string{}
		out[index].Related = []resolve.DiagnosticRelated{}
	}
	return queryStableSort(e, out, compareQueryDiagnostics)
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

func compareQueryDiagnostics(left, right Diagnostic) int {
	if queryDiagnosticSeverityRank(left.Severity) != queryDiagnosticSeverityRank(right.Severity) {
		return compareInt(int64(queryDiagnosticSeverityRank(left.Severity)), int64(queryDiagnosticSeverityRank(right.Severity)))
	}
	for _, pair := range [][2]string{
		{left.Code, right.Code},
		{left.SubjectAddress, right.SubjectAddress},
		{left.OwnerAddress, right.OwnerAddress},
		{left.MessageKey, right.MessageKey},
	} {
		if compared := strings.Compare(pair[0], pair[1]); compared != 0 {
			return compared
		}
	}
	return 0
}

func queryDiagnosticSeverityRank(value string) int {
	switch value {
	case "error":
		return 0
	case "warning":
		return 1
	case "info":
		return 2
	default:
		return 3
	}
}

func (e *queryExecutor) cloneScalars(values map[string]definition.Scalar) map[string]definition.Scalar {
	out := make(map[string]definition.Scalar, len(values))
	for key, value := range values {
		if !e.step() {
			return nil
		}
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

func (e *queryExecutor) appendPath(path QueryPath, relationAddress, entityAddress string) QueryPath {
	if !e.charge(int64(len(path.EntityAddresses) + len(path.RelationAddresses) + 2)) {
		return QueryPath{}
	}
	return QueryPath{
		EntityAddresses:   append(append([]string{}, path.EntityAddresses...), entityAddress),
		RelationAddresses: append(append([]string{}, path.RelationAddresses...), relationAddress),
	}
}

func (e *queryExecutor) pathContains(values []string, value string) bool {
	for _, item := range values {
		if !e.step() {
			return false
		}
		if item == value {
			return true
		}
	}
	return false
}

func (e *queryExecutor) sortPaths(paths []QueryPath) []QueryPath {
	return queryStableSort(e, paths, func(a, b QueryPath) int {
		aEnd, bEnd := "", ""
		if len(a.EntityAddresses) != 0 {
			aEnd = a.EntityAddresses[len(a.EntityAddresses)-1]
		}
		if len(b.EntityAddresses) != 0 {
			bEnd = b.EntityAddresses[len(b.EntityAddresses)-1]
		}
		if compared := compareStableAddressText(aEnd, bEnd); compared != 0 {
			return compared
		}
		return e.compareQueryPaths(a, b)
	})
}

func (e *queryExecutor) sortCycleRefs(refs []QueryCycleRef) []QueryCycleRef {
	return queryStableSort(e, refs, func(a, b QueryCycleRef) int {
		if a.Kind != b.Kind {
			return strings.Compare(a.Kind, b.Kind)
		}
		for _, pair := range [][2]string{{a.FromEntityAddress, b.FromEntityAddress}, {a.ToEntityAddress, b.ToEntityAddress}, {a.RelationAddress, b.RelationAddress}} {
			if compared := compareStableAddressText(pair[0], pair[1]); compared != 0 {
				return compared
			}
		}
		if orientationRank(a.Orientation) != orientationRank(b.Orientation) {
			return compareInt(int64(orientationRank(a.Orientation)), int64(orientationRank(b.Orientation)))
		}
		return e.compareQueryPaths(a.RetainedPath, b.RetainedPath)
	})
}

func (e *queryExecutor) stringSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		if !e.step() {
			return nil
		}
		out[value] = true
	}
	return out
}

func (e *queryExecutor) mapKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		if !e.step() {
			return nil
		}
		out = append(out, value)
	}
	return out
}

func (e *queryExecutor) sortSet(values map[string]bool) []string {
	return e.sortStableAddresses(e.mapKeys(values))
}

func (e *queryExecutor) sortStableAddresses(values []string) []string {
	out := queryStableSort(e, values, compareStableAddressText)
	return e.dedupeStrings(out)
}

func (e *queryExecutor) dedupeStrings(values []string) []string {
	out := values[:0]
	var previous string
	for _, value := range values {
		if !e.step() {
			return nil
		}
		if len(out) != 0 && value == previous {
			continue
		}
		out = append(out, value)
		previous = value
	}
	return out
}

func (e *queryExecutor) stringIn(value string, values []string) bool {
	for _, item := range values {
		if !e.step() {
			return false
		}
		if item == value {
			return true
		}
	}
	return false
}

func (e *queryExecutor) scalarIn(value definition.Scalar, values []definition.Scalar) bool {
	for _, item := range values {
		if !e.step() {
			return false
		}
		if scalarsEqual(item, value) {
			return true
		}
	}
	return false
}

func scalarsEqual(left, right definition.Scalar) bool {
	if left.Type != right.Type {
		return false
	}
	switch left.Type {
	case definition.ScalarString, definition.ScalarEnum, definition.ScalarDate, definition.ScalarDatetime:
		return left.String == right.String
	case definition.ScalarInteger:
		return left.Int == right.Int
	case definition.ScalarNumber:
		return !math.IsNaN(left.Float) && !math.IsNaN(right.Float) && left.Float == right.Float
	case definition.ScalarBoolean:
		return left.Bool == right.Bool
	default:
		return false
	}
}

func compareRelationTuple(left, right graph.Relation) int {
	for _, pair := range [][2]string{{left.TypeAddress, right.TypeAddress}, {left.Address, right.Address}, {left.FromAddress, right.FromAddress}, {left.ToAddress, right.ToAddress}} {
		if compared := compareStableAddressText(pair[0], pair[1]); compared != 0 {
			return compared
		}
	}
	return 0
}

func orientationRank(value string) int {
	if value == "outgoing" {
		return 0
	}
	return 1
}

func (e *queryExecutor) compareQueryPaths(left, right QueryPath) int {
	maximum := max(len(left.EntityAddresses), len(right.EntityAddresses))
	for index := 0; index < maximum; index++ {
		if !e.step() {
			return 0
		}
		if index >= len(left.EntityAddresses) {
			return -1
		}
		if index >= len(right.EntityAddresses) {
			return 1
		}
		if compared := compareStableAddressText(left.EntityAddresses[index], right.EntityAddresses[index]); compared != 0 {
			return compared
		}
		if index < len(left.RelationAddresses) && index < len(right.RelationAddresses) {
			if !e.step() {
				return 0
			}
			if compared := compareStableAddressText(left.RelationAddresses[index], right.RelationAddresses[index]); compared != 0 {
				return compared
			}
		}
	}
	return compareInt(int64(len(left.RelationAddresses)), int64(len(right.RelationAddresses)))
}

func (e *queryExecutor) scalarStringsEqual(left []string, right []definition.Scalar) bool {
	if len(left) != len(right) {
		return false
	}
	values := make([]string, len(right))
	for index, scalar := range right {
		if !e.step() {
			return false
		}
		values[index] = scalar.String
	}
	values = queryStableSort(e, values, strings.Compare)
	if e.err != nil {
		return false
	}
	for index := range left {
		if !e.step() {
			return false
		}
		if left[index] != values[index] {
			return false
		}
	}
	return true
}

func queryStableSort[T any](executor *queryExecutor, values []T, compare func(T, T) int) []T {
	if !executor.charge(int64(len(values))) {
		return nil
	}
	out := append([]T{}, values...)
	if len(out) < 2 {
		return out
	}
	scratch := make([]T, len(out))
	source, target := out, scratch
	inScratch := false
	for width := 1; width < len(out); {
		for left := 0; left < len(out); left += 2 * width {
			middle := min(left+width, len(out))
			right := min(middle+width, len(out))
			i, j, destination := left, middle, left
			for i < middle && j < right {
				if !executor.step() {
					return nil
				}
				compared := compare(source[i], source[j])
				if executor.err != nil {
					return nil
				}
				if compared <= 0 {
					target[destination] = source[i]
					i++
				} else {
					target[destination] = source[j]
					j++
				}
				destination++
			}
			remaining := (middle - i) + (right - j)
			if !executor.charge(int64(remaining)) {
				return nil
			}
			destination += copy(target[destination:], source[i:middle])
			copy(target[destination:], source[j:right])
		}
		source, target = target, source
		inScratch = !inScratch
		if width > len(out)/2 {
			break
		}
		width *= 2
	}
	if inScratch {
		if !executor.charge(int64(len(out))) {
			return nil
		}
		copy(out, source)
	}
	return out
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
