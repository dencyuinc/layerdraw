// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"sort"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
)

// semanticBatchLifecycle is the single operation-derived identity authority
// shared by precondition, rebase, and invalid-intermediate planning. It owns
// current addresses and owner closure; compiler output can refresh source and
// normalized facts, but cannot resurrect retired identities.
type semanticBatchLifecycle struct {
	subjects map[string]semanticLifecycleSubject
	root     string
	lineage  []semanticMoveEdge
}

type semanticLifecycleSubject struct {
	kind     SemanticSubjectKind
	owner    string
	ancestor bool
	module   PlannedModuleRef
}

type semanticMoveEdge struct {
	source      string
	destination string
	kind        SemanticSubjectKind
	owner       string
}

func newSemanticBatchLifecycle(snapshot Snapshot) *semanticBatchLifecycle {
	state := &semanticBatchLifecycle{subjects: make(map[string]semanticLifecycleSubject, len(snapshot.SemanticIndex.Subjects))}
	for _, subject := range snapshot.SemanticIndex.Subjects {
		owner := ""
		if subject.OwnerAddress != nil {
			owner = *subject.OwnerAddress
		}
		state.subjects[subject.Address] = semanticLifecycleSubject{kind: subject.Kind, owner: owner, ancestor: true}
		if _, source, ok := subjectRecord(snapshot, subject.Address); ok && source.Module != nil {
			current := state.subjects[subject.Address]
			current.module = PlannedModuleRef{OriginKind: SourceOriginKind(source.Module.Origin.Kind), PackAddress: source.Module.Origin.PackAddress, ModulePath: source.Module.ModulePath}
			state.subjects[subject.Address] = current
		}
		if subject.Kind == materialize.SubjectProject {
			state.root = subject.Address
		}
	}
	return state
}

func (state *semanticBatchLifecycle) available(address string) bool {
	_, ok := state.subjects[address]
	return ok
}

func (state *semanticBatchLifecycle) subject(address string) (semanticLifecycleSubject, bool) {
	value, ok := state.subjects[address]
	return value, ok
}

func (state *semanticBatchLifecycle) closure(address string) map[string]bool {
	closure := map[string]bool{}
	if !state.available(address) {
		return closure
	}
	closure[address] = true
	for changed := true; changed; {
		changed = false
		for candidate, subject := range state.subjects {
			if !closure[candidate] && closure[subject.owner] {
				closure[candidate] = true
				changed = true
			}
		}
	}
	return closure
}

func (state *semanticBatchLifecycle) create(owner string, kind SemanticSubjectKind, id string) (string, bool) {
	if !state.available(owner) || kind == "" || id == "" {
		return "", false
	}
	address := predictedChildAddress(owner, kind, id)
	if state.available(address) {
		return "", false
	}
	state.subjects[address] = semanticLifecycleSubject{kind: kind, owner: owner, module: state.subjects[owner].module}
	return address, true
}

func (state *semanticBatchLifecycle) upsertRow(owner, id string) (string, bool) {
	subject, ok := state.subject(owner)
	if !ok {
		return "", false
	}
	kind := rowKindForLifecycleOwner(subject.kind)
	address := predictedChildAddress(owner, kind, id)
	if !state.available(address) {
		state.subjects[address] = semanticLifecycleSubject{kind: kind, owner: owner}
	}
	return address, true
}

func rowKindForLifecycleOwner(kind SemanticSubjectKind) SemanticSubjectKind {
	if kind == materialize.SubjectRelation {
		return materialize.SubjectRelationRow
	}
	return materialize.SubjectEntityRow
}

func (state *semanticBatchLifecycle) rename(source, destination string) bool {
	subject, ok := state.subject(source)
	if !ok || destination == "" || source == destination || state.available(destination) {
		return false
	}
	closure := state.closure(source)
	addresses := make([]string, 0, len(closure))
	for address := range closure {
		addresses = append(addresses, address)
	}
	sort.Slice(addresses, func(i, j int) bool { return len(addresses[i]) < len(addresses[j]) })
	remap := func(address string) string {
		if address == source {
			return destination
		}
		if strings.HasPrefix(address, source+":") {
			return destination + strings.TrimPrefix(address, source)
		}
		return address
	}
	state.lineage = append(state.lineage, semanticMoveEdge{source: source, destination: destination, kind: subject.kind, owner: subject.owner})
	for _, address := range addresses {
		current := state.subjects[address]
		delete(state.subjects, address)
		current.owner = remap(current.owner)
		current.ancestor = false
		state.subjects[remap(address)] = current
	}
	if state.root == source {
		state.root = destination
	}
	return true
}

func (state *semanticBatchLifecycle) remove(address string) bool {
	closure := state.closure(address)
	if len(closure) == 0 {
		return false
	}
	for candidate := range closure {
		delete(state.subjects, candidate)
	}
	if closure[state.root] {
		state.root = ""
	}
	return true
}

func (state *semanticBatchLifecycle) lineageTo(destination string) []semanticMoveEdge {
	wanted := map[string]bool{destination: true}
	selected := make([]semanticMoveEdge, 0)
	for i := len(state.lineage) - 1; i >= 0; i-- {
		edge := state.lineage[i]
		if wanted[edge.destination] {
			selected = append(selected, edge)
			wanted[edge.source] = true
		}
	}
	for left, right := 0, len(selected)-1; left < right; left, right = left+1, right-1 {
		selected[left], selected[right] = selected[right], selected[left]
	}
	return selected
}

func (state *semanticBatchLifecycle) advance(operation SemanticOperation) bool {
	switch operation.Kind {
	case OperationCreateSubject:
		_, ok := state.create(operation.ParentAddress, operation.SubjectKind, operation.ID)
		return ok
	case OperationCreateRelation:
		_, ok := state.create(operation.ParentAddress, materialize.SubjectRelation, operation.ID)
		return ok
	case OperationUpsertRow:
		_, ok := state.upsertRow(operation.OwnerAddress, operation.ID)
		return ok
	case OperationDeleteSubject:
		return state.remove(operation.TargetAddress)
	case OperationDeleteRow:
		return state.remove(operation.RowAddress)
	case OperationRenameSubject:
		return state.rename(operation.TargetAddress, renamedAddress(operation.TargetAddress, operation.NewID))
	case OperationMigrateProjectIdentity:
		return state.rename(operation.ProjectAddress, renamedAddress(operation.ProjectAddress, operation.NewProjectID))
	case OperationUpdateRelationEndpoint:
		return state.available(operation.RelationAddress)
	case OperationMoveEntityToLayer:
		return state.available(operation.EntityAddress)
	case OperationUpdateSubjectField:
		return state.available(operation.TargetAddress)
	default:
		return false
	}
}

func semanticOperationPrimaryAddress(operation SemanticOperation) string {
	switch operation.Kind {
	case OperationCreateSubject, OperationCreateRelation:
		return operation.ParentAddress
	case OperationUpsertRow:
		return operation.OwnerAddress
	case OperationDeleteRow:
		return operation.RowAddress
	case OperationMigrateProjectIdentity:
		return operation.ProjectAddress
	case OperationUpdateRelationEndpoint:
		return operation.RelationAddress
	case OperationMoveEntityToLayer:
		return operation.EntityAddress
	default:
		return operation.TargetAddress
	}
}

func semanticSourceReadSet(snapshot Snapshot, batch SemanticOperationBatch, entryPath string) map[string]PlannedModuleRef {
	state := newSemanticBatchLifecycle(snapshot)
	set := map[string]PlannedModuleRef{}
	addModule := func(module PlannedModuleRef) {
		if module.OriginKind == SourceOriginProject && module.ModulePath != "" {
			set[semanticModuleIdentity(module)] = module
		}
	}
	addAddress := func(address string) {
		if subject, ok := state.subject(address); ok {
			addModule(subject.module)
		}
	}
	entry := PlannedModuleRef{OriginKind: SourceOriginProject, ModulePath: entryPath}
	for _, operation := range batch.Operations {
		switch operation.Kind {
		case OperationCreateSubject, OperationCreateRelation:
			addAddress(operation.ParentAddress)
			if operation.Placement != nil {
				if operation.Placement.ModulePath != "" {
					addModule(PlannedModuleRef{OriginKind: SourceOriginProject, ModulePath: operation.Placement.ModulePath})
				}
				addAddress(operation.Placement.GroupAnchorAddress)
			}
			if subject, ok := state.subject(operation.ParentAddress); ok && subject.kind == materialize.SubjectProject && (operation.Placement == nil || operation.Placement.ModulePath == "") {
				addModule(entry)
			}
		case OperationUpsertRow:
			addAddress(operation.OwnerAddress)
			if operation.Placement != nil {
				if operation.Placement.ModulePath != "" {
					addModule(PlannedModuleRef{OriginKind: SourceOriginProject, ModulePath: operation.Placement.ModulePath})
				}
				addAddress(operation.Placement.GroupAnchorAddress)
			}
		case OperationDeleteSubject, OperationRenameSubject:
			addAddress(operation.TargetAddress)
			addModule(entry)
			roots := []string{operation.TargetAddress}
			for _, edge := range state.lineageTo(operation.TargetAddress) {
				roots = append(roots, edge.source, edge.destination)
			}
			for _, reference := range snapshot.SemanticIndex.References {
				for _, root := range roots {
					if reference.TargetAddress == root || strings.HasPrefix(reference.TargetAddress, root+":") {
						if source, ok := state.subject(reference.SourceAddress); ok {
							addModule(source.module)
						}
					}
				}
			}
		case OperationDeleteRow:
			addAddress(operation.RowAddress)
			addModule(entry)
		case OperationMigrateProjectIdentity:
			addAddress(operation.ProjectAddress)
			addModule(entry)
		case OperationUpdateRelationEndpoint:
			addAddress(operation.RelationAddress)
		case OperationMoveEntityToLayer:
			addAddress(operation.EntityAddress)
		case OperationUpdateSubjectField:
			addAddress(operation.TargetAddress)
		}
		before := make(map[string]semanticLifecycleSubject, len(state.subjects))
		for address, subject := range state.subjects {
			before[address] = subject
		}
		if state.advance(operation) && (operation.Kind == OperationCreateSubject || operation.Kind == OperationCreateRelation) {
			kind := operation.SubjectKind
			if operation.Kind == OperationCreateRelation {
				kind = materialize.SubjectRelation
			}
			address := predictedChildAddress(operation.ParentAddress, kind, operation.ID)
			created := state.subjects[address]
			if operation.Placement != nil && operation.Placement.ModulePath != "" {
				created.module = PlannedModuleRef{OriginKind: SourceOriginProject, ModulePath: operation.Placement.ModulePath}
			} else if parent, ok := before[operation.ParentAddress]; ok && parent.kind == materialize.SubjectProject {
				created.module = entry
			}
			state.subjects[address] = created
		}
	}
	return set
}

func projectSourceGenerationChanged(ancestor, head Snapshot) bool {
	left, right := projectModuleDigests(ancestor), projectModuleDigests(head)
	if len(left) != len(right) {
		return true
	}
	for module, digest := range left {
		if right[module] != digest {
			return true
		}
	}
	return false
}

func projectModuleDigests(snapshot Snapshot) map[string]string {
	values := map[string]string{}
	for _, file := range snapshot.SourceMap.Files {
		if file.Origin.Kind != "project" {
			continue
		}
		module := PlannedModuleRef{OriginKind: SourceOriginProject, ModulePath: file.ModulePath}
		values[semanticModuleIdentity(module)] = file.Digest
	}
	return values
}

func validateSemanticSourceRebase(ancestor, head Snapshot, readSet map[string]PlannedModuleRef) []SemanticConflict {
	left, right := projectModuleDigests(ancestor), projectModuleDigests(head)
	conflicts := []SemanticConflict{}
	for identity, module := range readSet {
		if left[identity] != right[identity] {
			conflicts = append(conflicts, SemanticConflict{Kind: ConflictSubjectChanged, Path: []string{string(module.OriginKind), module.PackAddress, module.ModulePath}})
		}
	}
	sortSemanticConflicts(conflicts)
	return uniqueSemanticConflicts(conflicts)
}

type semanticOperationAddressFact struct {
	source    string
	target    string
	path      []string
	authority semanticOperationFactAuthority
}

type semanticOperationFactAuthority uint8

const (
	semanticFactReferencePair semanticOperationFactAuthority = iota
	semanticFactNormalizedField
)

func semanticAddressFactAuthority(kind SemanticSubjectKind, path []string) semanticOperationFactAuthority {
	if (kind == materialize.SubjectEntityTypeConstraint || kind == materialize.SubjectRelationTypeConstraint) && len(path) == 1 && path[0] == "column_addresses" {
		// Unique-column names are resolved owner-locally by the definition compiler,
		// not published as resolver bindings. Their authoritative result is the
		// normalized constraint child at the exact operation source address.
		return semanticFactNormalizedField
	}
	if (kind == materialize.SubjectEntityRow || kind == materialize.SubjectRelationRow) && len(path) != 0 && path[0] == "values" {
		// Row column keys and address-valued cells are materialized on the exact
		// row object; the definition compiler does not publish them all as
		// resolver bindings.
		return semanticFactNormalizedField
	}
	return semanticFactReferencePair
}

func verifySemanticOperationFacts(base, result Snapshot, batch SemanticOperationBatch) []SemanticConflict {
	state := newSemanticBatchLifecycle(base)
	facts := []semanticOperationAddressFact{}
	var collectValue func(string, SemanticSubjectKind, []string, SemanticValue)
	collectValue = func(source string, sourceKind SemanticSubjectKind, path []string, value SemanticValue) {
		if value.Kind == SemanticValueAddress {
			facts = append(facts, semanticOperationAddressFact{source: source, target: value.Address, path: append([]string(nil), path...), authority: semanticAddressFactAuthority(sourceKind, path)})
		}
		for _, item := range value.Array {
			collectValue(source, sourceKind, path, item)
		}
		for _, item := range value.Map {
			collectValue(source, sourceKind, append(path, item.Key), item.Value)
		}
	}
	remapFacts := func(source, destination string) {
		remap := func(address string) string {
			if address == source {
				return destination
			}
			if strings.HasPrefix(address, source+":") {
				return destination + strings.TrimPrefix(address, source)
			}
			return address
		}
		for index := range facts {
			facts[index].source = remap(facts[index].source)
			facts[index].target = remap(facts[index].target)
		}
	}
	removeSourceFacts := func(source string) {
		kept := facts[:0]
		for _, fact := range facts {
			if fact.source != source && !strings.HasPrefix(fact.source, source+":") {
				kept = append(kept, fact)
			}
		}
		facts = kept
	}
	clearPathFacts := func(source string, path []string) {
		kept := facts[:0]
		for _, fact := range facts {
			matches := fact.source == source && len(fact.path) >= len(path)
			if matches {
				for index := range path {
					matches = matches && fact.path[index] == path[index]
				}
			}
			if !matches {
				kept = append(kept, fact)
			}
		}
		facts = kept
	}
	for _, operation := range batch.Operations {
		source := ""
		switch operation.Kind {
		case OperationCreateSubject:
			source = predictedChildAddress(operation.ParentAddress, operation.SubjectKind, operation.ID)
			for _, field := range operation.Fields {
				collectValue(source, operation.SubjectKind, []string{field.Key}, field.Value)
			}
		case OperationCreateRelation:
			source = predictedChildAddress(operation.ParentAddress, materialize.SubjectRelation, operation.ID)
			for _, target := range []struct {
				address string
				path    string
			}{{operation.TypeAddress, "type_address"}, {operation.FromAddress, "from_address"}, {operation.ToAddress, "to_address"}} {
				facts = append(facts, semanticOperationAddressFact{source: source, target: target.address, path: []string{target.path}})
			}
			for _, field := range operation.Fields {
				collectValue(source, materialize.SubjectRelation, []string{field.Key}, field.Value)
			}
		case OperationUpsertRow:
			owner, _ := state.subject(operation.OwnerAddress)
			rowKind := rowKindForLifecycleOwner(owner.kind)
			source = predictedChildAddress(operation.OwnerAddress, rowKind, operation.ID)
			clearPathFacts(source, []string{"values"})
			for _, cell := range operation.Values {
				facts = append(facts, semanticOperationAddressFact{source: source, target: cell.ColumnAddress, path: []string{"values"}, authority: semanticAddressFactAuthority(rowKind, []string{"values"})})
				collectValue(source, rowKind, []string{"values", cell.ColumnAddress}, cell.Value)
			}
		case OperationUpdateRelationEndpoint:
			clearPathFacts(operation.RelationAddress, []string{operation.Endpoint + "_address"})
			facts = append(facts, semanticOperationAddressFact{source: operation.RelationAddress, target: operation.EntityAddress, path: []string{operation.Endpoint + "_address"}})
		case OperationMoveEntityToLayer:
			clearPathFacts(operation.EntityAddress, []string{"layer_address"})
			facts = append(facts, semanticOperationAddressFact{source: operation.EntityAddress, target: operation.LayerAddress, path: []string{"layer_address"}})
		case OperationUpdateSubjectField:
			clearPathFacts(operation.TargetAddress, operation.Path)
			if operation.Value != nil {
				subject, _ := state.subject(operation.TargetAddress)
				collectValue(operation.TargetAddress, subject.kind, operation.Path, *operation.Value)
			}
		case OperationRenameSubject:
			remapFacts(operation.TargetAddress, renamedAddress(operation.TargetAddress, operation.NewID))
		case OperationMigrateProjectIdentity:
			remapFacts(operation.ProjectAddress, renamedAddress(operation.ProjectAddress, operation.NewProjectID))
		case OperationDeleteSubject:
			removeSourceFacts(operation.TargetAddress)
		case OperationDeleteRow:
			removeSourceFacts(operation.RowAddress)
		}
		state.advance(operation)
	}
	available := map[string]bool{}
	for _, address := range result.StableAddresses {
		available[address] = true
	}
	references := map[string]bool{}
	for _, reference := range result.SemanticIndex.References {
		references[reference.SourceAddress+"\x00"+reference.TargetAddress] = true
	}
	conflicts := []SemanticConflict{}
	for _, fact := range facts {
		verified := references[fact.source+"\x00"+fact.target]
		if fact.authority == semanticFactNormalizedField {
			object := normalizedSubject(result, fact.source)
			value, pathExists := semanticObjectPath(object, fact.path)
			verified = pathExists && normalizedObjectContainsAddress(value, fact.target)
		}
		if !available[fact.target] || !verified {
			conflicts = append(conflicts, SemanticConflict{Kind: ConflictReferenceBroken, TargetAddress: fact.target, OwnerAddress: fact.source, Path: append([]string(nil), fact.path...)})
		}
	}
	sortSemanticConflicts(conflicts)
	return uniqueSemanticConflicts(conflicts)
}

func normalizedObjectContainsAddress(value any, address string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == address || normalizedObjectContainsAddress(child, address) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if normalizedObjectContainsAddress(child, address) {
				return true
			}
		}
	case string:
		return typed == address
	}
	return false
}
