// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"sort"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

type treeCandidate struct {
	relation graph.Relation
	parent   string
	child    string
	priority int64
}

type treeBuilder struct {
	materializer *viewMaterializer
	shape        view.TreeShape
	children     map[string][]treeCandidate
	primary      map[string]string
	cycleRefs    []TreeRef
	linkRefs     []TreeRef
	failed       bool
}

type treeOccurrenceNode struct {
	occurrence TreeOccurrence
	children   []*treeOccurrenceNode
}

type treeExpansionFrame struct {
	node      *treeOccurrenceNode
	entity    string
	nextChild int
}

func (m *viewMaterializer) tree(base ViewDataBase) *TreeViewData {
	result := &TreeViewData{ViewDataBase: base, Roots: []TreeOccurrence{}, CycleRefs: []TreeRef{}, LinkRefs: []TreeRef{}}
	shape := m.input.Recipe.Shape.Tree
	if shape == nil {
		m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Tree View shape is missing", m.input.Recipe.Address, "")
		return result
	}
	entities := m.materializationEntityAddresses()
	visible := viewStringSet(entities)
	candidates := m.treeCandidates(*shape, visible)
	if hasViewErrorDiagnostics(m.diagnostics) {
		return result
	}
	roots := m.treeRoots(entities, candidates)
	builder := treeBuilder{
		materializer: m,
		shape:        *shape,
		children:     map[string][]treeCandidate{},
		primary:      map[string]string{},
		cycleRefs:    []TreeRef{},
		linkRefs:     []TreeRef{},
	}
	for _, candidate := range candidates {
		builder.children[candidate.parent] = append(builder.children[candidate.parent], candidate)
	}
	for _, root := range roots {
		builder.primary[root] = builder.occurrenceKey([]string{root}, nil)
	}
	for _, root := range roots {
		occurrence := builder.buildOccurrence(root, nil, []string{root}, nil, map[string]bool{}, false)
		if builder.failed {
			return result
		}
		result.Roots = append(result.Roots, occurrence)
	}
	result.CycleRefs = builder.cycleRefs
	result.LinkRefs = builder.linkRefs
	return result
}

func (m *viewMaterializer) treeCandidates(shape view.TreeShape, visible map[string]bool) []treeCandidate {
	if len(shape.RelationTypeAddresses) == 0 || !canonicalStableAddressSlice(shape.RelationTypeAddresses) {
		m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Tree relation_types must be non-empty, unique, and canonical", m.input.Recipe.Address, "")
		return nil
	}
	if shape.CyclePolicy != view.TreeCycleError && shape.CyclePolicy != view.TreeCycleTruncate && shape.CyclePolicy != view.TreeCycleDuplicateOccurrence {
		m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Tree cycle policy is invalid", m.input.Recipe.Address, "")
		return nil
	}
	if shape.SharedChildPolicy != view.SharedChildError && shape.SharedChildPolicy != view.SharedChildDuplicateOccurrence && shape.SharedChildPolicy != view.SharedChildLink {
		m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Tree shared-child policy is invalid", m.input.Recipe.Address, "")
		return nil
	}
	allowed := viewStringSet(shape.RelationTypeAddresses)
	projections := make(map[string]definition.ProjectionSet, len(allowed))
	for _, address := range shape.RelationTypeAddresses {
		projectionSet, ok := m.effectiveViewProjectionSet(address)
		if !ok || !validTreeProjection(projectionSet.Tree) {
			m.addDiag("LDL1504", "invalid_projection_contract", "effective Tree projection is missing or invalid", address, m.input.Recipe.Address)
			continue
		}
		projections[address] = projectionSet
	}
	values := []treeCandidate{}
	for _, address := range m.relationAddresses() {
		if !m.reserveViewWork(1) {
			return nil
		}
		if err := pollContext(m.ctx, "view_tree_candidates"); err != nil {
			m.addDiag("LDL1801", "stale_revision_or_semantic_hash", err.Error(), m.input.Recipe.Address, "")
			return nil
		}
		relation, ok := m.relations[address]
		if !ok {
			m.addDiag("LDL1702", "view_materialization_conflict", "Tree Relation is absent from the immutable graph", address, m.input.Recipe.Address)
			continue
		}
		if !allowed[relation.TypeAddress] {
			continue
		}
		projectionSet, ok := projections[relation.TypeAddress]
		if !ok {
			continue
		}
		parent, child, ok := projectionEndpointPair(relation, projectionSet.Tree.ParentEndpoint, projectionSet.Tree.ChildEndpoint)
		if !ok {
			m.addDiag("LDL1504", "invalid_projection_contract", "effective Tree projection endpoints are invalid", relation.TypeAddress, m.input.Recipe.Address)
			continue
		}
		if !visible[parent] || !visible[child] {
			m.addDiag("LDL1702", "view_materialization_conflict", "Tree Relation endpoint has no materialization Entity", relation.Address, m.input.Recipe.Address)
			continue
		}
		values = append(values, treeCandidate{relation: relation, parent: parent, child: child, priority: projectionSet.Composed.Priority})
		if !m.withinViewCandidateLimit(len(values)) {
			return nil
		}
	}
	sort.SliceStable(values, func(i, j int) bool {
		left, right := values[i], values[j]
		if left.priority != right.priority {
			return left.priority > right.priority
		}
		for _, pair := range [][2]string{
			{left.relation.TypeAddress, right.relation.TypeAddress},
			{left.relation.Address, right.relation.Address},
			{left.relation.FromAddress, right.relation.FromAddress},
			{left.relation.ToAddress, right.relation.ToAddress},
		} {
			if compared := compareStableAddressText(pair[0], pair[1]); compared != 0 {
				return compared < 0
			}
		}
		return false
	})
	return values
}

func (m *viewMaterializer) treeRoots(entities []string, candidates []treeCandidate) []string {
	visible := viewStringSet(entities)
	roots := []string{}
	for _, address := range m.queryResult.SeedEntityAddresses {
		if visible[address] {
			roots = append(roots, address)
		}
	}
	if len(roots) != 0 {
		return sortedUniqueStableAddresses(roots)
	}
	hasParent := map[string]bool{}
	for _, candidate := range candidates {
		hasParent[candidate.child] = true
	}
	for _, address := range entities {
		if !hasParent[address] {
			roots = append(roots, address)
		}
	}
	if len(roots) == 0 && len(entities) != 0 {
		roots = append(roots, entities[0])
	}
	return roots
}

func (b *treeBuilder) buildOccurrence(entityAddress string, via *treeCandidate, entityPath, relationPath []string, ancestry map[string]bool, claimPrimary bool) TreeOccurrence {
	root := b.newOccurrence(entityAddress, via, entityPath, relationPath, claimPrimary)
	if b.failed {
		return TreeOccurrence{}
	}
	ancestry[entityAddress] = true
	currentEntityPath := append([]string{}, entityPath...)
	currentRelationPath := append([]string{}, relationPath...)
	stack := []treeExpansionFrame{{node: root, entity: entityAddress}}
	for len(stack) != 0 {
		frame := &stack[len(stack)-1]
		children := b.children[frame.entity]
		if frame.nextChild == len(children) {
			delete(ancestry, frame.entity)
			stack = stack[:len(stack)-1]
			if len(stack) != 0 {
				currentEntityPath = currentEntityPath[:len(currentEntityPath)-1]
				currentRelationPath = currentRelationPath[:len(currentRelationPath)-1]
			}
			continue
		}
		candidate := children[frame.nextChild]
		frame.nextChild++
		if !b.materializer.reserveViewWork(1) {
			b.failed = true
			return TreeOccurrence{}
		}
		if b.materializer.materializationWork%1024 == 0 {
			if err := pollContext(b.materializer.ctx, "view_tree_expand"); err != nil {
				b.materializer.addDiag("LDL1801", "stale_revision_or_semantic_hash", err.Error(), b.materializer.input.Recipe.Address, "")
				b.failed = true
				return TreeOccurrence{}
			}
		}
		if ancestry[candidate.child] {
			if b.shape.CyclePolicy == view.TreeCycleError {
				b.materializer.addDiag("LDL1702", "view_materialization_conflict", "Tree traversal encountered a forbidden ancestry cycle", candidate.relation.Address, b.materializer.input.Recipe.Address)
				b.failed = true
				return TreeOccurrence{}
			}
			b.addRef(&b.cycleRefs, "tree-cycle-ref", frame.node.occurrence.Key, candidate)
			continue
		}
		_, shared := b.primary[candidate.child]
		if shared {
			switch b.shape.SharedChildPolicy {
			case view.SharedChildError:
				b.materializer.addDiag("LDL1702", "view_materialization_conflict", "Tree traversal encountered a forbidden shared child", candidate.relation.Address, b.materializer.input.Recipe.Address)
				b.failed = true
				return TreeOccurrence{}
			case view.SharedChildLink:
				b.addRef(&b.linkRefs, "tree-link-ref", frame.node.occurrence.Key, candidate)
				continue
			case view.SharedChildDuplicateOccurrence:
			default:
				b.materializer.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Tree shared-child policy is invalid", b.materializer.input.Recipe.Address, "")
				b.failed = true
				return TreeOccurrence{}
			}
		}
		childEntities := append(currentEntityPath, candidate.child)
		childRelations := append(currentRelationPath, candidate.relation.Address)
		child := b.newOccurrence(candidate.child, &candidate, childEntities, childRelations, !shared)
		if b.failed {
			return TreeOccurrence{}
		}
		frame.node.children = append(frame.node.children, child)
		ancestry[candidate.child] = true
		currentEntityPath = childEntities
		currentRelationPath = childRelations
		stack = append(stack, treeExpansionFrame{node: child, entity: candidate.child})
	}
	return materializeTreeOccurrence(root)
}

func (b *treeBuilder) newOccurrence(entityAddress string, via *treeCandidate, entityPath, relationPath []string, claimPrimary bool) *treeOccurrenceNode {
	pathWork := int64(len(entityPath)) + int64(len(relationPath)) + 1
	if b.failed || !b.materializer.reserveViewItems(1) || !b.materializer.reserveViewWork(pathWork) {
		b.failed = true
		return nil
	}
	if err := pollContext(b.materializer.ctx, "view_tree_expand"); err != nil {
		b.materializer.addDiag("LDL1801", "stale_revision_or_semantic_hash", err.Error(), b.materializer.input.Recipe.Address, "")
		b.failed = true
		return nil
	}
	entity, ok := b.materializer.entities[entityAddress]
	if !ok {
		b.materializer.addDiag("LDL1702", "view_materialization_conflict", "Tree occurrence Entity is absent from the immutable graph", entityAddress, b.materializer.input.Recipe.Address)
		b.failed = true
		return nil
	}
	key := b.occurrenceKey(entityPath, relationPath)
	occurrence := TreeOccurrence{Key: key, EntityAddress: entityAddress, Children: []TreeOccurrence{}, Source: b.materializer.diagramEntitySource(entity)}
	if via != nil {
		relationAddress := via.relation.Address
		occurrence.ViaRelationAddress = &relationAddress
		occurrence.Source = mergeViewDataSourceRefs(occurrence.Source, b.materializer.diagramRelationSource(via.relation))
	}
	if claimPrimary {
		b.primary[entityAddress] = key
	}
	return &treeOccurrenceNode{occurrence: occurrence, children: []*treeOccurrenceNode{}}
}

func materializeTreeOccurrence(root *treeOccurrenceNode) TreeOccurrence {
	ordered := []*treeOccurrenceNode{}
	stack := []*treeOccurrenceNode{root}
	for len(stack) != 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		ordered = append(ordered, current)
		stack = append(stack, current.children...)
	}
	for index := len(ordered) - 1; index >= 0; index-- {
		current := ordered[index]
		current.occurrence.Children = make([]TreeOccurrence, len(current.children))
		for childIndex, child := range current.children {
			current.occurrence.Children[childIndex] = child.occurrence
		}
	}
	return root.occurrence
}

func (b *treeBuilder) occurrenceKey(entityPath, relationPath []string) string {
	return viewItemKey(b.materializer, "tree-occurrence", []any{b.materializer.input.Recipe.Address, entityPath, relationPath})
}

func (b *treeBuilder) addRef(target *[]TreeRef, kind, fromKey string, candidate treeCandidate) {
	if !b.materializer.reserveViewItems(1) {
		b.failed = true
		return
	}
	parent := b.materializer.entities[candidate.parent]
	child := b.materializer.entities[candidate.child]
	source := mergeViewDataSourceRefs(
		b.materializer.diagramEntitySource(parent),
		b.materializer.diagramEntitySource(child),
		b.materializer.diagramRelationSource(candidate.relation),
	)
	*target = append(*target, TreeRef{
		Key:               viewItemKey(b.materializer, kind, []any{b.materializer.input.Recipe.Address, fromKey, candidate.child, candidate.relation.Address}),
		FromOccurrenceKey: fromKey,
		ToEntityAddress:   candidate.child,
		RelationAddress:   candidate.relation.Address,
		Source:            source,
	})
}

func (m *viewMaterializer) effectiveViewProjectionSet(relationTypeAddress string) (definition.ProjectionSet, bool) {
	relationType, ok := m.relationTypes[relationTypeAddress]
	if !ok {
		m.addDiag("LDL1702", "view_materialization_conflict", "RelationType is absent from the immutable definition", relationTypeAddress, m.input.Recipe.Address)
		return definition.ProjectionSet{}, false
	}
	projections := relationType.Projections
	found := false
	for _, override := range m.input.Recipe.RelationProjections {
		if override.RelationTypeAddress != relationTypeAddress {
			continue
		}
		if found {
			m.addDiag("LDL1504", "invalid_projection_contract", "View contains duplicate effective RelationType projection overrides", relationTypeAddress, m.input.Recipe.Address)
			return definition.ProjectionSet{}, false
		}
		found = true
		projections = override.Projections
	}
	return projections, true
}

func validTreeProjection(value *definition.TreeProjection) bool {
	return value != nil && validProjectionEndpoint(value.ParentEndpoint) && validProjectionEndpoint(value.ChildEndpoint) && value.ParentEndpoint != value.ChildEndpoint
}

func validProjectionEndpoint(value definition.ProjectionEndpoint) bool {
	return value == definition.ProjectionEndpointFrom || value == definition.ProjectionEndpointTo
}

func projectionEndpointPair(relation graph.Relation, first, second definition.ProjectionEndpoint) (string, string, bool) {
	if !validProjectionEndpoint(first) || !validProjectionEndpoint(second) || first == second {
		return "", "", false
	}
	left, right := relation.FromAddress, relation.ToAddress
	if first == definition.ProjectionEndpointTo {
		left = relation.ToAddress
	}
	if second == definition.ProjectionEndpointFrom {
		right = relation.FromAddress
	}
	return left, right, true
}
