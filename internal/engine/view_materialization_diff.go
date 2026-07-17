// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

type diffSnapshotProjection struct {
	snapshot   Snapshot
	project    string
	subjects   map[string]diffProjectedSubject
	children   map[string][]string
	references map[string][]string
	assets     map[string][]string
}

type diffProjectedSubject struct {
	address string
	kind    materialize.SubjectKind
	owner   string
	fields  map[string]any
	source  ViewDataSourceRefs
}

type diffDependencyOrigin struct {
	Address     string `json:"address"`
	CanonicalID string `json:"canonical_id"`
}

type diffProjectDefinitionPayload struct {
	Project           materialize.Project            `json:"project"`
	DependencyOrigins []diffDependencyOrigin         `json:"dependency_origins"`
	EntityTypes       []materialize.EntityType       `json:"entity_types"`
	RelationTypes     []materialize.RelationType     `json:"relation_types"`
	Layers            []materialize.Layer            `json:"layers"`
	Entities          []materialize.Entity           `json:"entities"`
	Relations         []materialize.Relation         `json:"relations"`
	Queries           []materialize.Query            `json:"queries"`
	Views             []materialize.View             `json:"views"`
	References        []materialize.Reference        `json:"references"`
	Assets            []materialize.AssetBlobSummary `json:"assets"`
	Identity          materialize.IdentityHistory    `json:"identity"`
}

func prepareDiffMaterialization(ctx context.Context, recipe CompiledViewRecipe, input DiffViewMaterializationInput) (diffSnapshotProjection, diffSnapshotProjection, map[string]string, []Diagnostic) {
	reject := func(message, subject string) []Diagnostic {
		if subject == "" {
			subject = recipe.Address
		}
		return []Diagnostic{diagnostic("LDL1801", "stale_revision_or_semantic_hash", message, subject, recipe.Address)}
	}
	if err := pollContext(ctx, "view_diff_validate"); err != nil {
		return diffSnapshotProjection{}, diffSnapshotProjection{}, nil, reject(err.Error(), recipe.Address)
	}
	if _, err := buildDiffSnapshotProjection(ctx, input.RecipeSnapshot); err != nil {
		return diffSnapshotProjection{}, diffSnapshotProjection{}, nil, reject("Diff recipe revision is invalid: "+err.Error(), recipe.Address)
	}
	before, err := buildDiffSnapshotProjection(ctx, input.BeforeSnapshot)
	if err != nil {
		return diffSnapshotProjection{}, diffSnapshotProjection{}, nil, reject("Diff before revision is invalid: "+err.Error(), recipe.Address)
	}
	after, err := buildDiffSnapshotProjection(ctx, input.AfterSnapshot)
	if err != nil {
		return diffSnapshotProjection{}, diffSnapshotProjection{}, nil, reject("Diff after revision is invalid: "+err.Error(), recipe.Address)
	}
	if err := validateDiffInclude(recipe.Shape.Diff); err != nil {
		return diffSnapshotProjection{}, diffSnapshotProjection{}, nil, reject(err.Error(), recipe.Address)
	}

	detectMoves := recipe.Shape.Diff != nil && recipe.Shape.Diff.DetectMoves
	moveAuthority, err := diffMoveAuthority(before, after, detectMoves)
	if err != nil {
		return diffSnapshotProjection{}, diffSnapshotProjection{}, nil, reject(err.Error(), recipe.Address)
	}
	if before.project != after.project && moveAuthority[before.project] != after.project {
		return diffSnapshotProjection{}, diffSnapshotProjection{}, nil, reject("Diff revisions belong to incompatible Projects", recipe.Address)
	}
	moves := map[string]string{}
	if detectMoves {
		moves = moveAuthority
	}
	if recipe.Source.Diff.QueryAddress != nil {
		if err := validateDiffQueryInputs(ctx, recipe, input, before, after); err != nil {
			return diffSnapshotProjection{}, diffSnapshotProjection{}, nil, reject(err.Error(), recipe.Address)
		}
	}
	return before, after, moves, nil
}

func buildDiffSnapshotProjection(ctx context.Context, snapshot Snapshot) (diffSnapshotProjection, error) {
	if snapshot.Mode != CompileProject || snapshot.NormalizedDocument == nil || snapshot.TypedAST.Project == nil || snapshot.TypedAST.Graph == nil {
		return diffSnapshotProjection{}, fmt.Errorf("snapshot is not a complete Project definition")
	}
	document := snapshot.NormalizedDocument
	if document.Format != materialize.NormalizedFormat || document.SchemaVersion != materialize.SchemaVersion || document.Language != materialize.LanguageVersion {
		return diffSnapshotProjection{}, fmt.Errorf("normalized definition identity is unsupported")
	}
	if document.Project.Address == "" || snapshot.TypedAST.Project.Address != document.Project.Address || len(snapshot.Diagnostics) != 0 {
		return diffSnapshotProjection{}, fmt.Errorf("Project identity or compiler status is inconsistent")
	}
	canonical, err := materialize.Canonicalize(*document)
	if err != nil || !bytes.Equal(canonical, snapshot.CanonicalJSON) {
		return diffSnapshotProjection{}, fmt.Errorf("canonical definition does not match the normalized document")
	}
	expectedHash, err := diffDefinitionHash(*document)
	if err != nil || expectedHash != snapshot.DefinitionHash {
		return diffSnapshotProjection{}, fmt.Errorf("definition hash does not match normalized content")
	}
	if snapshot.SemanticIndex.SchemaVersion != 1 || snapshot.SourceMap.SchemaVersion != 1 || snapshot.SemanticIndex.Subjects == nil || snapshot.SemanticIndex.References == nil {
		return diffSnapshotProjection{}, fmt.Errorf("semantic index is incomplete")
	}

	var canonicalRoot any
	if err := json.Unmarshal(snapshot.CanonicalJSON, &canonicalRoot); err != nil {
		return diffSnapshotProjection{}, fmt.Errorf("canonical definition is not valid JSON")
	}
	objects := map[string]map[string]any{}
	collectDiffAddressObjects(canonicalRoot, objects)
	hashes := map[string]materialize.SubjectHash{}
	for _, value := range snapshot.SubjectSemanticHashes {
		if value.Address == "" || !validSemanticHash(value.Hash) || hashes[value.Address].Address != "" {
			return diffSnapshotProjection{}, fmt.Errorf("subject hash index is invalid")
		}
		hashes[value.Address] = value
	}
	if len(hashes) != len(snapshot.SemanticIndex.Subjects) {
		return diffSnapshotProjection{}, fmt.Errorf("subject hash index does not cover semantic subjects")
	}

	projection := diffSnapshotProjection{
		snapshot: snapshot, project: document.Project.Address,
		subjects: map[string]diffProjectedSubject{}, children: map[string][]string{}, references: map[string][]string{}, assets: map[string][]string{},
	}
	kinds := map[string]materialize.SubjectKind{}
	for index, subject := range snapshot.SemanticIndex.Subjects {
		if err := pollContext(ctx, "view_diff_snapshot"); err != nil {
			return diffSnapshotProjection{}, err
		}
		if subject.Address == "" || !diffSubjectKindSupported(subject.Kind) || !validSemanticHash(subject.OwnHash) {
			return diffSnapshotProjection{}, fmt.Errorf("semantic subject is invalid")
		}
		if index > 0 && compareStableAddressText(snapshot.SemanticIndex.Subjects[index-1].Address, subject.Address) >= 0 {
			return diffSnapshotProjection{}, fmt.Errorf("semantic subjects are not in canonical unique order")
		}
		hash, ok := hashes[subject.Address]
		if !ok || hash.Kind != subject.Kind || hash.Hash != subject.OwnHash {
			return diffSnapshotProjection{}, fmt.Errorf("semantic subject hash is inconsistent")
		}
		if _, duplicate := kinds[subject.Address]; duplicate {
			return diffSnapshotProjection{}, fmt.Errorf("semantic subject address is duplicated")
		}
		kinds[subject.Address] = subject.Kind
	}
	for _, members := range snapshot.SemanticIndex.Children {
		if _, ok := kinds[members.OwnerAddress]; !ok || members.Addresses == nil {
			return diffSnapshotProjection{}, fmt.Errorf("semantic child index is invalid")
		}
		for index, address := range members.Addresses {
			if _, ok := kinds[address]; !ok || (index > 0 && compareStableAddressText(members.Addresses[index-1], address) >= 0) {
				return diffSnapshotProjection{}, fmt.Errorf("semantic child index is not canonical")
			}
			projection.children[members.OwnerAddress] = append(projection.children[members.OwnerAddress], address)
		}
	}
	for _, reference := range snapshot.SemanticIndex.References {
		if kinds[reference.SourceAddress] == "" || kinds[reference.TargetAddress] == "" || kinds[reference.TargetAddress] != reference.TargetKind {
			return diffSnapshotProjection{}, fmt.Errorf("semantic reference index is invalid")
		}
		projection.references[reference.SourceAddress] = append(projection.references[reference.SourceAddress], reference.TargetAddress)
	}
	for address := range projection.references {
		projection.references[address] = sortedUniqueStableAddresses(projection.references[address])
	}
	for _, asset := range snapshot.SourceMap.Assets {
		projection.assets[asset.SubjectAddress] = append(projection.assets[asset.SubjectAddress], asset.Digest)
	}
	for address := range projection.assets {
		projection.assets[address] = sortedUniqueStrings(projection.assets[address])
	}

	for _, semantic := range snapshot.SemanticIndex.Subjects {
		object := objects[semantic.Address]
		if object == nil {
			return diffSnapshotProjection{}, fmt.Errorf("normalized subject %q is absent", semantic.Address)
		}
		owner := ""
		if semantic.OwnerAddress != nil {
			owner = *semantic.OwnerAddress
			if kinds[owner] == "" {
				return diffSnapshotProjection{}, fmt.Errorf("subject owner is absent")
			}
		} else if semantic.Kind != materialize.SubjectProject && semantic.Kind != materialize.SubjectPack {
			return diffSnapshotProjection{}, fmt.Errorf("non-root subject has no owner")
		}
		fields, err := diffOwnFields(*document, semantic.Address, semantic.Kind, object)
		if err != nil {
			return diffSnapshotProjection{}, err
		}
		projection.subjects[semantic.Address] = diffProjectedSubject{address: semantic.Address, kind: semantic.Kind, owner: owner, fields: fields}
	}
	if root, ok := projection.subjects[projection.project]; !ok || root.kind != materialize.SubjectProject {
		return diffSnapshotProjection{}, fmt.Errorf("Project root is absent from semantic subjects")
	}
	for address, subject := range projection.subjects {
		subject.source = projection.sourceFor(address)
		projection.subjects[address] = subject
	}
	return projection, nil
}

func diffDefinitionHash(document materialize.NormalizedDocument) (string, error) {
	origins := make([]diffDependencyOrigin, len(document.Dependencies))
	for index, dependency := range document.Dependencies {
		origins[index] = diffDependencyOrigin{Address: dependency.Address, CanonicalID: dependency.CanonicalID}
	}
	payload := diffProjectDefinitionPayload{
		Project: document.Project, DependencyOrigins: origins, EntityTypes: document.EntityTypes, RelationTypes: document.RelationTypes,
		Layers: document.Layers, Entities: document.Entities, Relations: document.Relations, Queries: document.Queries, Views: document.Views,
		References: document.References, Assets: document.Assets, Identity: document.Identity,
	}
	return materialize.SemanticHash(materialize.DomainDefinition, payload)
}

func collectDiffAddressObjects(value any, out map[string]map[string]any) {
	switch typed := value.(type) {
	case map[string]any:
		if address, ok := typed["address"].(string); ok && address != "" {
			out[address] = typed
		}
		for _, child := range typed {
			collectDiffAddressObjects(child, out)
		}
	case []any:
		for _, child := range typed {
			collectDiffAddressObjects(child, out)
		}
	}
}

func diffOwnFields(document materialize.NormalizedDocument, address string, kind materialize.SubjectKind, object map[string]any) (map[string]any, error) {
	fields := deepClone(object)
	delete(fields, "address")
	switch kind {
	case materialize.SubjectProject:
		fields["reservations"] = diffJSONValue(document.Identity.RootReservations[address])
		fields["moves"] = diffJSONValue(diffRootMoves(document.Identity.Moves, address))
		fields["move_closure"] = diffJSONValue(diffRootMoveClosure(document.Identity.MoveClosure, address))
	case materialize.SubjectPack:
		return map[string]any{
			"canonical_id": fields["canonical_id"],
			"version":      fields["version"],
			"digest":       fields["digest"],
		}, nil
	case materialize.SubjectEntityType:
		order := diffObjectAddressOrder(fields["columns"])
		delete(fields, "columns")
		delete(fields, "unique_constraints")
		fields["column_order"] = order
	case materialize.SubjectRelationType:
		order := diffObjectAddressOrder(fields["columns"])
		delete(fields, "columns")
		delete(fields, "unique_constraints")
		fields["column_order"] = order
	case materialize.SubjectEntity, materialize.SubjectRelation:
		delete(fields, "rows")
	case materialize.SubjectQuery:
		delete(fields, "parameters")
	case materialize.SubjectView:
		delete(fields, "exports")
		order := []any{}
		if shape, ok := fields["shape"].(map[string]any); ok {
			if shape["kind"] == string(view.ShapeTable) {
				order = diffObjectAddressOrder(shape["columns"])
				shape["columns"] = []any{}
			}
		}
		fields["table_column_order"] = order
	}
	return fields, nil
}

func diffObjectAddressOrder(value any) []any {
	items, ok := value.([]any)
	if !ok {
		return []any{}
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if address, ok := object["address"].(string); ok {
			out = append(out, address)
		}
	}
	return out
}

func diffJSONValue(value any) any {
	encoded, _ := json.Marshal(value)
	var out any
	_ = json.Unmarshal(encoded, &out)
	return out
}

func diffRootMoves(values []materialize.Move, root string) []materialize.Move {
	out := []materialize.Move{}
	for _, value := range values {
		if value.OldAddress == root || strings.HasPrefix(value.OldAddress, root+":") {
			out = append(out, value)
		}
	}
	return out
}

func diffRootMoveClosure(values []materialize.MoveResolution, root string) []materialize.MoveResolution {
	out := []materialize.MoveResolution{}
	for _, value := range values {
		if value.SourceAddress == root || strings.HasPrefix(value.SourceAddress, root+":") {
			out = append(out, value)
		}
	}
	return out
}

func diffSubjectKindSupported(kind materialize.SubjectKind) bool {
	_, ok := diffSubjectKind(kind)
	return ok
}

func diffSubjectKind(kind materialize.SubjectKind) (view.DiffSubjectKind, bool) {
	value := view.DiffSubjectKind(kind)
	switch value {
	case view.DiffProject, view.DiffPack, view.DiffEntityType, view.DiffRelationType, view.DiffLayer, view.DiffEntity, view.DiffRelation,
		view.DiffQuery, view.DiffView, view.DiffReference, view.DiffEntityTypeColumn, view.DiffEntityTypeConstraint,
		view.DiffRelationTypeColumn, view.DiffRelationTypeConstraint, view.DiffEntityRow, view.DiffRelationRow,
		view.DiffQueryParameter, view.DiffViewTableColumn, view.DiffViewExport:
		return value, true
	default:
		return "", false
	}
}

func validateDiffInclude(shape *view.DiffShape) error {
	if shape == nil || shape.Include == nil {
		return fmt.Errorf("Diff shape or include set is incomplete")
	}
	previous := -1
	seen := map[view.DiffSubjectKind]bool{}
	for _, kind := range shape.Include {
		rank := diffKindRank(kind)
		if rank < 0 || seen[kind] || rank <= previous {
			return fmt.Errorf("Diff include set is not canonical")
		}
		seen[kind] = true
		previous = rank
	}
	return nil
}

func diffKindRank(kind view.DiffSubjectKind) int {
	order := []view.DiffSubjectKind{
		view.DiffProject, view.DiffPack, view.DiffEntityType, view.DiffRelationType, view.DiffLayer, view.DiffEntity, view.DiffRelation,
		view.DiffQuery, view.DiffView, view.DiffReference, view.DiffEntityTypeColumn, view.DiffEntityTypeConstraint,
		view.DiffRelationTypeColumn, view.DiffRelationTypeConstraint, view.DiffEntityRow, view.DiffRelationRow,
		view.DiffQueryParameter, view.DiffViewTableColumn, view.DiffViewExport,
	}
	for index, value := range order {
		if value == kind {
			return index
		}
	}
	return -1
}

func diffMoveAuthority(before, after diffSnapshotProjection, requireDescendant bool) (map[string]string, error) {
	direct := map[string]string{}
	resolutions := map[string]materialize.MoveResolution{}
	for _, move := range after.snapshot.NormalizedDocument.Identity.MoveClosure {
		if !diffSubjectKindSupported(move.Kind) || move.Kind == materialize.SubjectPack || move.SourceAddress == "" || move.TerminalAddress == "" || move.SourceAddress == move.TerminalAddress {
			return nil, fmt.Errorf("Diff move closure is malformed")
		}
		if _, duplicate := direct[move.SourceAddress]; duplicate {
			return nil, fmt.Errorf("Diff move closure is not unique")
		}
		terminal, ok := after.subjects[move.TerminalAddress]
		if !ok || terminal.kind != move.Kind {
			return nil, fmt.Errorf("Diff move closure terminal is not an active subject")
		}
		if diffChildSubjectKind(move.Kind) {
			if move.OwnerAddress == nil || after.subjects[*move.OwnerAddress].address == "" || terminal.owner != *move.OwnerAddress {
				return nil, fmt.Errorf("Diff move closure child owner is invalid")
			}
		} else if move.OwnerAddress != nil {
			return nil, fmt.Errorf("Diff move closure root subject has an owner")
		}
		direct[move.SourceAddress] = move.TerminalAddress
		resolutions[move.SourceAddress] = move
	}
	if requireDescendant && !diffMoveHistoryExtends(before.snapshot.NormalizedDocument.Identity.MoveClosure, resolutions, direct) {
		return nil, fmt.Errorf("Diff move detection requires an after revision that extends before identity history")
	}

	authority := map[string]string{}
	claimed := map[string]string{}
	for address, subject := range before.subjects {
		result, changed, err := applyDiffMoveClosure(address, direct)
		if err != nil {
			return nil, err
		}
		if !changed {
			continue
		}
		target, ok := after.subjects[result]
		if !ok || target.kind != subject.kind {
			continue
		}
		if prior, collision := claimed[result]; collision && prior != address {
			return nil, fmt.Errorf("Diff move closure maps multiple subjects to one result")
		}
		if _, occupied := before.subjects[result]; occupied && result != address {
			return nil, fmt.Errorf("Diff move closure collides with an active before subject")
		}
		claimed[result] = address
		authority[address] = result
	}
	return authority, nil
}

func diffChildSubjectKind(kind materialize.SubjectKind) bool {
	switch kind {
	case materialize.SubjectEntityTypeColumn, materialize.SubjectEntityTypeConstraint, materialize.SubjectRelationTypeColumn,
		materialize.SubjectRelationTypeConstraint, materialize.SubjectEntityRow, materialize.SubjectRelationRow,
		materialize.SubjectQueryParameter, materialize.SubjectViewTableColumn, materialize.SubjectViewExport:
		return true
	default:
		return false
	}
}

func diffMoveHistoryExtends(before []materialize.MoveResolution, after map[string]materialize.MoveResolution, direct map[string]string) bool {
	for _, prior := range before {
		current, ok := after[prior.SourceAddress]
		if !ok || current.Kind != prior.Kind {
			return false
		}
		expectedTerminal, _, err := applyDiffMoveClosure(prior.TerminalAddress, direct)
		if err != nil || current.TerminalAddress != expectedTerminal {
			return false
		}
		priorOwner, currentOwner := "", ""
		if prior.OwnerAddress != nil {
			priorOwner = *prior.OwnerAddress
			priorOwner, _, err = applyDiffMoveClosure(priorOwner, direct)
			if err != nil {
				return false
			}
		}
		if current.OwnerAddress != nil {
			currentOwner = *current.OwnerAddress
		}
		if priorOwner != currentOwner {
			return false
		}
	}
	return true
}

func applyDiffMoveClosure(address string, direct map[string]string) (string, bool, error) {
	current := address
	changed := false
	for step := 0; step <= len(direct); step++ {
		bestSource, bestTarget := "", ""
		for source, target := range direct {
			if current == source || strings.HasPrefix(current, source+":") {
				if len(source) > len(bestSource) {
					bestSource, bestTarget = source, target
				}
			}
		}
		if bestSource == "" {
			return current, changed, nil
		}
		next := bestTarget + strings.TrimPrefix(current, bestSource)
		if next == current {
			return "", false, fmt.Errorf("Diff move closure does not make progress")
		}
		current, changed = next, true
	}
	return "", false, fmt.Errorf("Diff move closure contains a cycle")
}

func validateDiffQueryInputs(ctx context.Context, recipe CompiledViewRecipe, input DiffViewMaterializationInput, before, after diffSnapshotProjection) error {
	queryAddress := *recipe.Source.Diff.QueryAddress
	if queryAddress == "" || input.BeforeQueryResult == nil || input.AfterQueryResult == nil {
		return fmt.Errorf("Diff Query input is incomplete")
	}
	recipeQuery, recipeOK := queryRecipeInSnapshot(input.RecipeSnapshot, queryAddress)
	beforeQuery, beforeOK := queryRecipeInSnapshot(input.BeforeSnapshot, queryAddress)
	afterQuery, afterOK := queryRecipeInSnapshot(input.AfterSnapshot, queryAddress)
	if !recipeOK || !beforeOK || !afterOK {
		return fmt.Errorf("Diff Query must exist at the same StableAddress in every revision")
	}
	if recipeQuery.StateInput != query.StateNone || beforeQuery.StateInput != query.StateNone || afterQuery.StateInput != query.StateNone {
		return fmt.Errorf("Diff Query must be state-independent")
	}
	if !reflect.DeepEqual(recipeQuery.Parameters, beforeQuery.Parameters) || !reflect.DeepEqual(recipeQuery.Parameters, afterQuery.Parameters) {
		return fmt.Errorf("Diff Query parameter definitions are incompatible")
	}
	for _, side := range []struct {
		projection diffSnapshotProjection
		result     QueryResult
	}{
		{before, *input.BeforeQueryResult},
		{after, *input.AfterQueryResult},
	} {
		if err := pollContext(ctx, "view_diff_query"); err != nil {
			return err
		}
		if side.result.QueryAddress != queryAddress || side.result.StatePolicy != string(query.StateNone) || side.result.StateInput.Kind != "none" || len(side.result.StateReads) != 0 {
			return fmt.Errorf("Diff QueryResult identity or state input is invalid")
		}
		if !viewArgumentsMatch(recipe.Source.Diff.Arguments, side.result.Arguments) {
			return fmt.Errorf("Diff QueryResult arguments do not match the recipe")
		}
		for _, diagnostic := range side.result.Diagnostics {
			if diagnostic.Severity != "warning" && diagnostic.Severity != "info" {
				return fmt.Errorf("Diff QueryResult contains an error diagnostic")
			}
		}
		checker := newViewMaterializer(ctx, ViewMaterializationInput{Recipe: recipe})
		checker.snapshot = side.projection.snapshot
		checker.graph = *side.projection.snapshot.TypedAST.Graph
		checker.queryResult = deepClone(side.result)
		checker.indexSnapshot()
		checker.validateQueryResultSubjects()
		if len(checker.diagnostics) != 0 {
			return fmt.Errorf("Diff QueryResult is not a canonical selection")
		}
	}
	return nil
}

func (projection diffSnapshotProjection) sourceFor(address string) ViewDataSourceRefs {
	refs := emptyViewDataSourceRefs()
	add := func(value string) {
		subject, ok := projection.subjects[value]
		if !ok {
			return
		}
		refs.SubjectAddresses = append(refs.SubjectAddresses, value)
		switch subject.kind {
		case materialize.SubjectEntity:
			refs.EntityAddresses = append(refs.EntityAddresses, value)
		case materialize.SubjectRelation:
			refs.RelationAddresses = append(refs.RelationAddresses, value)
		case materialize.SubjectLayer:
			refs.LayerAddresses = append(refs.LayerAddresses, value)
		case materialize.SubjectEntityRow, materialize.SubjectRelationRow:
			refs.RowAddresses = append(refs.RowAddresses, value)
		}
	}
	current := address
	for current != "" {
		add(current)
		for _, target := range projection.references[current] {
			add(target)
		}
		current = projection.subjects[current].owner
	}
	if subject := projection.subjects[address]; subject.kind == materialize.SubjectEntityRow || subject.kind == materialize.SubjectRelationRow {
		if values, ok := subject.fields["values"].(map[string]any); ok {
			columns := make([]string, 0, len(values))
			for column := range values {
				columns = append(columns, column)
			}
			sort.Slice(columns, func(i, j int) bool { return compareStableAddressText(columns[i], columns[j]) < 0 })
			for _, column := range columns {
				refs.CellRefs = append(refs.CellRefs, ViewDataCellRef{RowAddress: address, ColumnAddress: column})
				add(column)
			}
		}
	}
	refs.AssetDigests = append(refs.AssetDigests, projection.assets[address]...)
	return canonicalViewDataSourceRefs(refs)
}

func (projection diffSnapshotProjection) queryScope(result *QueryResult, queryAddress string) map[string]bool {
	if result == nil {
		out := make(map[string]bool, len(projection.subjects))
		for address := range projection.subjects {
			out[address] = true
		}
		return out
	}
	seeds := append([]string{}, result.PrimaryEntityAddresses...)
	seeds = append(seeds, result.SupportEntityAddresses...)
	if len(seeds) == 0 {
		seeds = append(seeds, result.SeedEntityAddresses...)
		seeds = append(seeds, result.TraversedEntityAddresses...)
	}
	relations := append([]string{}, result.SelectedRelationAddresses...)
	if len(relations) == 0 {
		relations = append(relations, result.PathRelationAddresses...)
		relations = append(relations, result.InducedRelationAddresses...)
	}
	queue := append([]string{queryAddress}, seeds...)
	queue = append(queue, relations...)
	scope := map[string]bool{}
	for len(queue) != 0 {
		address := queue[0]
		queue = queue[1:]
		subject, ok := projection.subjects[address]
		if !ok || scope[address] {
			continue
		}
		scope[address] = true
		if subject.owner != "" {
			queue = append(queue, subject.owner)
		}
		queue = append(queue, projection.references[address]...)
		if diffExpandsOwnedClosure(subject.kind) {
			queue = append(queue, projection.children[address]...)
		}
	}
	return scope
}

func diffExpandsOwnedClosure(kind materialize.SubjectKind) bool {
	switch kind {
	case materialize.SubjectEntityType, materialize.SubjectRelationType, materialize.SubjectEntity, materialize.SubjectRelation, materialize.SubjectQuery, materialize.SubjectView:
		return true
	default:
		return false
	}
}

func (m *viewMaterializer) diffView(base ViewDataBase) *DiffViewData {
	changes := m.diffChanges()
	refs := emptyViewDataSourceRefs()
	refs.SubjectAddresses = append(refs.SubjectAddresses, m.input.Recipe.Address, m.diffBefore.project, m.diffAfter.project)
	if m.input.Recipe.Source.Diff.QueryAddress != nil {
		refs.SubjectAddresses = append(refs.SubjectAddresses, *m.input.Recipe.Source.Diff.QueryAddress)
	}
	for _, change := range changes {
		refs = mergeViewDataSourceRefs(refs, change.Source)
	}
	m.diffSource = canonicalViewDataSourceRefs(refs)
	return &DiffViewData{ViewDataBase: base, Changes: changes}
}

func (m *viewMaterializer) diffChanges() []DiffChange {
	queryAddress := ""
	var beforeResult, afterResult *QueryResult
	if m.input.Recipe.Source.Diff.QueryAddress != nil {
		queryAddress = *m.input.Recipe.Source.Diff.QueryAddress
		beforeResult, afterResult = m.input.Diff.BeforeQueryResult, m.input.Diff.AfterQueryResult
	}
	beforeScope := m.diffBefore.queryScope(beforeResult, queryAddress)
	afterScope := m.diffAfter.queryScope(afterResult, queryAddress)
	resultAddresses := map[string]bool{}
	inverseMoves := map[string]string{}
	for beforeAddress, afterAddress := range m.diffMoves {
		inverseMoves[afterAddress] = beforeAddress
	}
	for address := range beforeScope {
		result := address
		if moved := m.diffMoves[address]; moved != "" {
			result = moved
		}
		resultAddresses[result] = true
	}
	for address := range afterScope {
		resultAddresses[address] = true
	}
	ordered := make([]string, 0, len(resultAddresses))
	for address := range resultAddresses {
		ordered = append(ordered, address)
	}
	sort.Slice(ordered, func(i, j int) bool { return compareStableAddressText(ordered[i], ordered[j]) < 0 })
	include := map[view.DiffSubjectKind]bool{}
	for _, kind := range m.input.Recipe.Shape.Diff.Include {
		include[kind] = true
	}
	changes := []DiffChange{}
	for _, resultAddress := range ordered {
		if err := pollContext(m.ctx, "view_diff_compare"); err != nil {
			m.addDiag("LDL1801", "stale_revision_or_semantic_hash", err.Error(), m.input.Recipe.Address, "")
			return []DiffChange{}
		}
		beforeAddress := resultAddress
		if moved := inverseMoves[resultAddress]; moved != "" {
			beforeAddress = moved
		}
		before, beforeOK := m.diffBefore.subjects[beforeAddress]
		after, afterOK := m.diffAfter.subjects[resultAddress]
		if !beforeOK && !afterOK {
			continue
		}
		kind := after.kind
		if !afterOK {
			kind = before.kind
		}
		if beforeOK && afterOK && before.kind != after.kind {
			m.addDiag("LDL1801", "stale_revision_or_semantic_hash", "Diff subject kind changed at one StableAddress", resultAddress, m.input.Recipe.Address)
			return []DiffChange{}
		}
		diffKind, ok := diffSubjectKind(kind)
		if !ok || !include[diffKind] {
			continue
		}
		change, changed := m.diffChange(before, beforeOK, after, afterOK, beforeAddress != resultAddress)
		if changed {
			changes = append(changes, change)
		}
	}
	sort.Slice(changes, func(i, j int) bool { return compareDiffChanges(changes[i], changes[j]) < 0 })
	return changes
}

func (m *viewMaterializer) diffChange(before diffProjectedSubject, beforeOK bool, after diffProjectedSubject, afterOK bool, moved bool) (DiffChange, bool) {
	change := DiffChange{Fields: []FieldDiff{}, Source: emptyViewDataSourceRefs()}
	if beforeOK {
		value := before.address
		change.BeforeAddress = &value
		source := deepClone(before.source)
		change.BeforeSource = &source
	}
	if afterOK {
		value := after.address
		change.AfterAddress = &value
		source := deepClone(after.source)
		change.AfterSource = &source
	}
	change.Source = mergeViewDataSourceRefs(before.source, after.source)
	subjectKind := after.kind
	if !afterOK {
		subjectKind = before.kind
	}
	change.SubjectKind = string(subjectKind)
	switch {
	case !beforeOK:
		change.Kind = DiffChangeAdded
		change.Fields = m.diffFields(nil, after.fields, false)
	case !afterOK:
		change.Kind = DiffChangeRemoved
		change.Fields = m.diffFields(before.fields, nil, false)
	case moved:
		beforeFields := normalizeDiffMovedAddresses(before.fields, m.diffMoves).(map[string]any)
		afterFields := deepClone(after.fields)
		delete(beforeFields, "id")
		delete(afterFields, "id")
		change.Fields = m.diffFields(beforeFields, afterFields, true)
		if len(change.Fields) == 0 {
			change.Kind = DiffChangeMoved
		} else {
			change.Kind = DiffChangeMovedUpdated
		}
	default:
		beforeFields := normalizeDiffMovedAddresses(before.fields, m.diffMoves).(map[string]any)
		change.Fields = m.diffFields(beforeFields, after.fields, true)
		if len(change.Fields) == 0 {
			return DiffChange{}, false
		}
		change.Kind = DiffChangeUpdated
	}
	beforeKey, afterKey := any(nil), any(nil)
	if change.BeforeAddress != nil {
		beforeKey = *change.BeforeAddress
	}
	if change.AfterAddress != nil {
		afterKey = *change.AfterAddress
	}
	change.Key = viewItemKey(m, "diff-change", []any{m.input.Recipe.Address, string(change.Kind), beforeKey, afterKey})
	for index := range change.Fields {
		change.Fields[index].Key = viewItemKey(m, "diff-field", []any{m.input.Recipe.Address, change.Key, change.Fields[index].Path})
	}
	return change, true
}

func normalizeDiffMovedAddresses(value any, moves map[string]string) any {
	switch typed := value.(type) {
	case string:
		if moved := moves[typed]; moved != "" {
			return moved
		}
		return typed
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = normalizeDiffMovedAddresses(item, moves)
		}
		return out
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(typed))
		for _, key := range keys {
			mappedKey := key
			if moved := moves[key]; moved != "" {
				mappedKey = moved
			}
			out[mappedKey] = normalizeDiffMovedAddresses(typed[key], moves)
		}
		return out
	default:
		return deepClone(value)
	}
}

func (m *viewMaterializer) diffFields(before, after map[string]any, changedOnly bool) []FieldDiff {
	fields := []FieldDiff{}
	diffFieldMapLeaves(nil, before, after, changedOnly, &fields)
	sort.Slice(fields, func(i, j int) bool { return compareDiffPath(fields[i].Path, fields[j].Path) < 0 })
	return fields
}

func diffFieldMapLeaves(path []string, before, after map[string]any, changedOnly bool, out *[]FieldDiff) {
	keys := map[string]bool{}
	for key := range before {
		keys[key] = true
	}
	for key := range after {
		keys[key] = true
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	for _, key := range ordered {
		beforeValue, beforePresent := before[key]
		afterValue, afterPresent := after[key]
		childPath := append(append([]string{}, path...), key)
		beforeMap, beforeMapOK := beforeValue.(map[string]any)
		afterMap, afterMapOK := afterValue.(map[string]any)
		if (beforeMapOK || !beforePresent) && (afterMapOK || !afterPresent) && (beforeMapOK || afterMapOK) {
			if len(beforeMap) == 0 && len(afterMap) == 0 && beforePresent != afterPresent {
				field := FieldDiff{Path: childPath, BeforePresent: beforePresent, AfterPresent: afterPresent}
				if beforePresent {
					value := typedAuthoredValue(key, beforeValue)
					field.Before = &value
				}
				if afterPresent {
					value := typedAuthoredValue(key, afterValue)
					field.After = &value
				}
				*out = append(*out, field)
				continue
			}
			diffFieldMapLeaves(childPath, beforeMap, afterMap, changedOnly, out)
			continue
		}
		if changedOnly && beforePresent == afterPresent && reflect.DeepEqual(beforeValue, afterValue) {
			continue
		}
		field := FieldDiff{Path: childPath, BeforePresent: beforePresent, AfterPresent: afterPresent}
		if beforePresent {
			value := typedAuthoredValue(key, beforeValue)
			field.Before = &value
		}
		if afterPresent {
			value := typedAuthoredValue(key, afterValue)
			field.After = &value
		}
		*out = append(*out, field)
	}
}

func compareDiffPath(left, right []string) int {
	for index := 0; index < len(left) && index < len(right); index++ {
		if compared := strings.Compare(left[index], right[index]); compared != 0 {
			return compared
		}
	}
	return len(left) - len(right)
}

func compareDiffChanges(left, right DiffChange) int {
	leftKind, _ := diffSubjectKind(materialize.SubjectKind(left.SubjectKind))
	rightKind, _ := diffSubjectKind(materialize.SubjectKind(right.SubjectKind))
	if compared := diffKindRank(leftKind) - diffKindRank(rightKind); compared != 0 {
		return compared
	}
	leftAddress, rightAddress := diffResultAddress(left), diffResultAddress(right)
	if compared := compareStableAddressText(leftAddress, rightAddress); compared != 0 {
		return compared
	}
	return strings.Compare(string(left.Kind), string(right.Kind))
}

func diffResultAddress(change DiffChange) string {
	if change.AfterAddress != nil {
		return *change.AfterAddress
	}
	if change.BeforeAddress != nil {
		return *change.BeforeAddress
	}
	return ""
}
