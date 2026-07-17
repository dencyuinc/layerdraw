// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"sort"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
)

type diagramProjectionCandidate struct {
	relation    graph.Relation
	projections definition.ProjectionSet
	render      definition.RenderSet
	order       int
}

type diagramNestCandidate struct {
	diagramProjectionCandidate
	parentAddress string
	childAddress  string
}

type diagramSupportIdentity struct {
	kind     DiagramSupportKind
	entity   string
	relation string
}

type diagramSupportAccumulator struct {
	materializer *viewMaterializer
	items        []DiagramSupportItem
	orders       []int
	index        map[diagramSupportIdentity]int
}

func (m *viewMaterializer) diagram(base ViewDataBase) *DiagramViewData {
	occurrences, occurrenceIndex, occurrenceByEntity := m.diagramOccurrences()
	support := newDiagramSupportAccumulator(m)
	for index, address := range m.queryResult.SupportEntityAddresses {
		entity := m.entities[address]
		support.add(index-len(m.queryResult.SupportEntityAddresses), DiagramSupportHiddenEntity, &entity.Address, nil, m.diagramEntitySource(entity))
	}

	candidates := m.diagramCandidates()
	edgeQueue := make([]diagramProjectionCandidate, 0, len(candidates))
	nestGroups := map[string][]diagramNestCandidate{}
	childOrder := []string{}
	overlayQueue := []diagramProjectionCandidate{}
	badgeQueue := []diagramProjectionCandidate{}
	composed := m.input.Recipe.Shape.Diagram != nil && m.input.Recipe.Shape.Diagram.Composed

	for _, candidate := range candidates {
		if !composed {
			edgeQueue = append(edgeQueue, candidate)
			continue
		}
		switch candidate.projections.Composed.Mode {
		case definition.ComposedEdge:
			edgeQueue = append(edgeQueue, candidate)
		case definition.ComposedHide:
			source := m.diagramRelationSource(candidate.relation)
			support.add(candidate.order, DiagramSupportHiddenRelation, nil, &candidate.relation.Address, source)
		case definition.ComposedNest:
			parent, child, ok := m.diagramProjectionEndpoints(candidate.relation, candidate.projections.Composed.ParentEndpoint, candidate.projections.Composed.ChildEndpoint)
			if !ok {
				continue
			}
			if _, exists := nestGroups[child]; !exists {
				childOrder = append(childOrder, child)
			}
			nestGroups[child] = append(nestGroups[child], diagramNestCandidate{
				diagramProjectionCandidate: candidate,
				parentAddress:              parent,
				childAddress:               child,
			})
		case definition.ComposedOverlay:
			overlayQueue = append(overlayQueue, candidate)
		case definition.ComposedBadge:
			badgeQueue = append(badgeQueue, candidate)
		default:
			m.addDiag("LDL1504", "invalid_projection_contract", "effective composed projection mode is invalid", candidate.relation.TypeAddress, m.input.Recipe.Address)
		}
	}

	adopted := map[string]diagramNestCandidate{}
	parentByChild := map[string]string{}
	for _, child := range childOrder {
		m.resolveDiagramNestGroup(nestGroups[child], adopted, parentByChild, &edgeQueue, support)
	}
	for _, child := range childOrder {
		candidate, ok := adopted[child]
		if !ok {
			continue
		}
		index, ok := occurrenceIndex[child]
		if !ok {
			m.addDiag("LDL1702", "view_materialization_conflict", "nested child has no Diagram occurrence", child, m.input.Recipe.Address)
			continue
		}
		parentKey, ok := occurrenceByEntity[candidate.parentAddress]
		if !ok {
			m.addDiag("LDL1702", "view_materialization_conflict", "nested parent has no Diagram occurrence", candidate.parentAddress, m.input.Recipe.Address)
			continue
		}
		relationAddress := candidate.relation.Address
		occurrences[index].ParentKey = &parentKey
		occurrences[index].ViaRelationAddress = &relationAddress
		occurrences[index].Source = mergeViewDataSourceRefs(occurrences[index].Source, m.diagramRelationSource(candidate.relation))
	}

	edges := m.diagramEdges(edgeQueue, occurrenceByEntity, support)
	overlays := m.diagramOverlays(overlayQueue, occurrenceByEntity)
	badges := m.diagramBadges(badgeQueue, occurrenceByEntity)
	containers := m.diagramContainers(occurrences, adopted)
	m.validateDiagramPlacements(occurrences)

	return &DiagramViewData{
		ViewDataBase: base,
		Occurrences:  occurrences,
		Edges:        edges,
		Containers:   containers,
		Overlays:     overlays,
		Badges:       badges,
		SupportItems: support.sortedItems(),
	}
}

func (m *viewMaterializer) diagramOccurrences() ([]DiagramOccurrence, map[string]int, map[string]string) {
	entityAddresses := m.materializationEntityAddresses()
	primary := viewStringSet(m.primaryEntityAddresses())
	occurrences := make([]DiagramOccurrence, 0, len(entityAddresses))
	index := make(map[string]int, len(entityAddresses))
	keys := make(map[string]string, len(entityAddresses))
	for _, address := range entityAddresses {
		entity, exists := m.entities[address]
		if !exists {
			m.addDiag("LDL1702", "view_materialization_conflict", "Diagram Entity is absent from the immutable graph", address, m.input.Recipe.Address)
			continue
		}
		if _, duplicate := keys[address]; duplicate {
			m.addDiag("LDL1702", "view_materialization_conflict", "Diagram Entity produced more than one provisional occurrence", address, m.input.Recipe.Address)
			continue
		}
		role := DiagramRoleSupport
		if primary[address] {
			role = DiagramRoleNode
			if entityType, ok := m.entityTypes[entity.TypeAddress]; ok && entityType.Representation.Kind == definition.RepresentationContainer {
				role = DiagramRoleContainer
			}
		}
		key := viewItemKey(m, "diagram-occurrence", []any{m.input.Recipe.Address, entity.Address})
		index[address] = len(occurrences)
		keys[address] = key
		occurrences = append(occurrences, DiagramOccurrence{
			Key: key, EntityAddress: entity.Address, LayerAddress: entity.LayerAddress, Role: role,
			Source: m.diagramEntitySource(entity),
		})
	}
	return occurrences, index, keys
}

func (m *viewMaterializer) diagramCandidates() []diagramProjectionCandidate {
	projectionByType := map[string]diagramProjectionCandidate{}
	values := make([]diagramProjectionCandidate, 0, len(m.relationAddresses()))
	for _, address := range m.relationAddresses() {
		relation, ok := m.relations[address]
		if !ok {
			m.addDiag("LDL1702", "view_materialization_conflict", "Diagram Relation is absent from the immutable graph", address, m.input.Recipe.Address)
			continue
		}
		effective, cached := projectionByType[relation.TypeAddress]
		if !cached {
			projections, render, valid := m.effectiveDiagramProjection(relation.TypeAddress)
			if !valid {
				continue
			}
			effective = diagramProjectionCandidate{projections: projections, render: render}
			projectionByType[relation.TypeAddress] = effective
		}
		effective.relation = relation
		values = append(values, effective)
	}
	if m.input.Recipe.Shape.Diagram != nil && m.input.Recipe.Shape.Diagram.Composed {
		sort.SliceStable(values, func(i, j int) bool {
			left, right := values[i], values[j]
			if left.projections.Composed.Priority != right.projections.Composed.Priority {
				return left.projections.Composed.Priority > right.projections.Composed.Priority
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
	}
	for index := range values {
		values[index].order = index
	}
	return values
}

func (m *viewMaterializer) effectiveDiagramProjection(relationTypeAddress string) (definition.ProjectionSet, definition.RenderSet, bool) {
	relationType, ok := m.relationTypes[relationTypeAddress]
	if !ok {
		m.addDiag("LDL1702", "view_materialization_conflict", "Diagram RelationType is absent from the immutable definition", relationTypeAddress, m.input.Recipe.Address)
		return definition.ProjectionSet{}, definition.RenderSet{}, false
	}
	projections := relationType.Projections
	render := relationType.Render
	found := false
	for _, override := range m.input.Recipe.RelationProjections {
		if override.RelationTypeAddress != relationTypeAddress {
			continue
		}
		if found {
			m.addDiag("LDL1504", "invalid_projection_contract", "View contains duplicate effective RelationType projection overrides", relationTypeAddress, m.input.Recipe.Address)
			return definition.ProjectionSet{}, definition.RenderSet{}, false
		}
		found = true
		projections = override.Projections
		render = override.Render
	}
	if !validEffectiveComposedProjection(projections.Composed) || !validEffectiveDiagramProjection(projections.Diagram) {
		m.addDiag("LDL1504", "invalid_projection_contract", "effective Diagram projection is invalid", relationTypeAddress, m.input.Recipe.Address)
		return definition.ProjectionSet{}, definition.RenderSet{}, false
	}
	return projections, render, true
}

func validEffectiveComposedProjection(value definition.ComposedProjection) bool {
	endpoint := func(value *definition.ProjectionEndpoint) bool {
		return value != nil && (*value == definition.ProjectionEndpointFrom || *value == definition.ProjectionEndpointTo)
	}
	distinct := func(left, right *definition.ProjectionEndpoint) bool {
		return endpoint(left) && endpoint(right) && *left != *right
	}
	if value.Conflict != definition.ProjectionConflictKeepEdge && value.Conflict != definition.ProjectionConflictPreferFirst && value.Conflict != definition.ProjectionConflictDiagnostic {
		return false
	}
	switch value.Mode {
	case definition.ComposedNest:
		return distinct(value.ParentEndpoint, value.ChildEndpoint) && value.OverlayEndpoint == nil && value.TargetEndpoint == nil && value.BadgeEndpoint == nil
	case definition.ComposedOverlay:
		return distinct(value.OverlayEndpoint, value.TargetEndpoint) && value.ParentEndpoint == nil && value.ChildEndpoint == nil && value.BadgeEndpoint == nil
	case definition.ComposedBadge:
		return distinct(value.BadgeEndpoint, value.TargetEndpoint) && value.ParentEndpoint == nil && value.ChildEndpoint == nil && value.OverlayEndpoint == nil
	case definition.ComposedEdge, definition.ComposedHide:
		return value.ParentEndpoint == nil && value.ChildEndpoint == nil && value.OverlayEndpoint == nil && value.TargetEndpoint == nil && value.BadgeEndpoint == nil
	default:
		return false
	}
}

func validEffectiveDiagramProjection(value definition.DiagramProjection) bool {
	validEndpoint := func(value definition.ProjectionEndpoint) bool {
		return value == definition.ProjectionEndpointFrom || value == definition.ProjectionEndpointTo
	}
	return (value.Mode == definition.DiagramEdge || value.Mode == definition.DiagramHide) &&
		validEndpoint(value.SourceEndpoint) && validEndpoint(value.TargetEndpoint) && value.SourceEndpoint != value.TargetEndpoint
}

func (m *viewMaterializer) resolveDiagramNestGroup(
	group []diagramNestCandidate,
	adopted map[string]diagramNestCandidate,
	parentByChild map[string]string,
	edgeQueue *[]diagramProjectionCandidate,
	support *diagramSupportAccumulator,
) {
	if len(group) == 0 {
		return
	}
	sameParent := true
	for _, candidate := range group[1:] {
		if candidate.parentAddress != group[0].parentAddress {
			sameParent = false
			break
		}
	}
	if sameParent {
		m.adoptDiagramNest(group[0], adopted, parentByChild, edgeQueue)
		for _, candidate := range group[1:] {
			if candidate.projections.Composed.KeepEdge {
				*edgeQueue = append(*edgeQueue, candidate.diagramProjectionCandidate)
			} else {
				source := m.diagramRelationSource(candidate.relation)
				support.add(candidate.order, DiagramSupportSourceOnly, nil, &candidate.relation.Address, source)
			}
		}
		return
	}
	for _, candidate := range group {
		if candidate.projections.Composed.Conflict == definition.ProjectionConflictDiagnostic {
			m.addWarning("LDL1704", "composed_parent_ambiguity_retained", "composed Diagram retained ambiguous parent candidates as support data", candidate.childAddress, m.input.Recipe.Address)
			for _, retained := range group {
				source := m.diagramRelationSource(retained.relation)
				support.add(retained.order, DiagramSupportSourceOnly, nil, &retained.relation.Address, source)
			}
			return
		}
	}
	for _, candidate := range group {
		if candidate.projections.Composed.Conflict == definition.ProjectionConflictPreferFirst {
			m.adoptDiagramNest(candidate, adopted, parentByChild, edgeQueue)
			for _, retained := range group {
				if retained.relation.Address != candidate.relation.Address {
					m.retainUnselectedNest(retained, edgeQueue, support)
				}
			}
			return
		}
	}
	for _, candidate := range group {
		*edgeQueue = append(*edgeQueue, candidate.diagramProjectionCandidate)
	}
}

func (m *viewMaterializer) adoptDiagramNest(
	candidate diagramNestCandidate,
	adopted map[string]diagramNestCandidate,
	parentByChild map[string]string,
	edgeQueue *[]diagramProjectionCandidate,
) {
	if diagramNestWouldCycle(candidate.parentAddress, candidate.childAddress, parentByChild) {
		m.addDiag("LDL1702", "view_materialization_conflict", "composed Diagram nesting creates an occurrence ancestry cycle", candidate.relation.Address, m.input.Recipe.Address)
		return
	}
	adopted[candidate.childAddress] = candidate
	parentByChild[candidate.childAddress] = candidate.parentAddress
	if candidate.projections.Composed.KeepEdge {
		*edgeQueue = append(*edgeQueue, candidate.diagramProjectionCandidate)
	}
}

func diagramNestWouldCycle(parent, child string, parentByChild map[string]string) bool {
	if parent == child {
		return true
	}
	seen := map[string]bool{}
	for current := parent; current != "" && !seen[current]; current = parentByChild[current] {
		if current == child {
			return true
		}
		seen[current] = true
	}
	return false
}

func (m *viewMaterializer) retainUnselectedNest(candidate diagramNestCandidate, edgeQueue *[]diagramProjectionCandidate, support *diagramSupportAccumulator) {
	if candidate.projections.Composed.Conflict == definition.ProjectionConflictKeepEdge || candidate.projections.Composed.KeepEdge {
		*edgeQueue = append(*edgeQueue, candidate.diagramProjectionCandidate)
		return
	}
	source := m.diagramRelationSource(candidate.relation)
	support.add(candidate.order, DiagramSupportSourceOnly, nil, &candidate.relation.Address, source)
}

func (m *viewMaterializer) diagramEdges(candidates []diagramProjectionCandidate, occurrenceByEntity map[string]string, support *diagramSupportAccumulator) []DiagramEdge {
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].order < candidates[j].order })
	edges := make([]DiagramEdge, 0, len(candidates))
	for _, candidate := range candidates {
		projection := candidate.projections.Diagram
		source := m.diagramRelationSource(candidate.relation)
		if projection.Mode == definition.DiagramHide {
			support.add(candidate.order, DiagramSupportHiddenRelation, nil, &candidate.relation.Address, source)
			continue
		}
		fromAddress, toAddress, ok := m.diagramProjectionEndpointPair(candidate.relation, projection.SourceEndpoint, projection.TargetEndpoint)
		if !ok {
			continue
		}
		fromKey, fromOK := occurrenceByEntity[fromAddress]
		toKey, toOK := occurrenceByEntity[toAddress]
		if !fromOK || !toOK {
			m.addDiag("LDL1702", "view_materialization_conflict", "Diagram Relation endpoint has no occurrence", candidate.relation.Address, m.input.Recipe.Address)
			continue
		}
		key := viewItemKey(m, "diagram-edge", []any{m.input.Recipe.Address, candidate.relation.Address, fromKey, toKey})
		edges = append(edges, DiagramEdge{
			Key: key, FromOccurrenceKey: fromKey, ToOccurrenceKey: toKey,
			RelationAddress: candidate.relation.Address, RelationTypeAddress: candidate.relation.TypeAddress,
			Source: source,
		})
	}
	return edges
}

func (m *viewMaterializer) diagramOverlays(candidates []diagramProjectionCandidate, occurrenceByEntity map[string]string) []DiagramOverlay {
	overlays := make([]DiagramOverlay, 0, len(candidates))
	for _, candidate := range candidates {
		overlayAddress, targetAddress, ok := m.diagramProjectionEndpoints(candidate.relation, candidate.projections.Composed.OverlayEndpoint, candidate.projections.Composed.TargetEndpoint)
		if !ok {
			continue
		}
		targetKey, ok := occurrenceByEntity[targetAddress]
		if !ok {
			m.addDiag("LDL1702", "view_materialization_conflict", "Diagram overlay target has no occurrence", candidate.relation.Address, m.input.Recipe.Address)
			continue
		}
		key := viewItemKey(m, "diagram-overlay", []any{m.input.Recipe.Address, candidate.relation.Address, targetKey})
		overlays = append(overlays, DiagramOverlay{
			Key: key, TargetOccurrenceKey: targetKey, OverlayEntityAddress: overlayAddress,
			RelationAddress: candidate.relation.Address, RelationTypeAddress: candidate.relation.TypeAddress,
			Source: m.diagramRelationSource(candidate.relation),
		})
	}
	return overlays
}

func (m *viewMaterializer) diagramBadges(candidates []diagramProjectionCandidate, occurrenceByEntity map[string]string) []DiagramBadge {
	badges := make([]DiagramBadge, 0, len(candidates))
	for _, candidate := range candidates {
		badgeAddress, targetAddress, ok := m.diagramProjectionEndpoints(candidate.relation, candidate.projections.Composed.BadgeEndpoint, candidate.projections.Composed.TargetEndpoint)
		if !ok {
			continue
		}
		targetKey, ok := occurrenceByEntity[targetAddress]
		if !ok {
			m.addDiag("LDL1702", "view_materialization_conflict", "Diagram badge target has no occurrence", candidate.relation.Address, m.input.Recipe.Address)
			continue
		}
		key := viewItemKey(m, "diagram-badge", []any{m.input.Recipe.Address, candidate.relation.Address, targetKey})
		badges = append(badges, DiagramBadge{
			Key: key, TargetOccurrenceKey: targetKey,
			RelationAddress: candidate.relation.Address, RelationTypeAddress: candidate.relation.TypeAddress,
			Label: m.diagramBadgeLabel(candidate, badgeAddress), Source: m.diagramRelationSource(candidate.relation),
		})
	}
	return badges
}

func (m *viewMaterializer) diagramBadgeLabel(candidate diagramProjectionCandidate, badgeAddress string) *string {
	entity, ok := m.entities[badgeAddress]
	if !ok {
		m.addDiag("LDL1702", "view_materialization_conflict", "Diagram badge Entity is absent", badgeAddress, m.input.Recipe.Address)
		return nil
	}
	var value string
	switch candidate.render.Badge.Label {
	case definition.RenderBadgeLabelType:
		entityType, ok := m.entityTypes[entity.TypeAddress]
		if !ok {
			m.addDiag("LDL1702", "view_materialization_conflict", "Diagram badge EntityType is absent", entity.TypeAddress, m.input.Recipe.Address)
			return nil
		}
		value = entityType.DisplayName
	case definition.RenderBadgeLabelDisplayName:
		value = entity.DisplayName
	case definition.RenderBadgeLabelCount:
		value = "1"
	case definition.RenderBadgeLabelNone:
		return nil
	default:
		m.addDiag("LDL1504", "invalid_projection_contract", "effective Diagram badge label is invalid", candidate.relation.TypeAddress, m.input.Recipe.Address)
		return nil
	}
	return &value
}

func (m *viewMaterializer) diagramContainers(occurrences []DiagramOccurrence, adopted map[string]diagramNestCandidate) []DiagramContainer {
	children := map[string][]string{}
	for _, occurrence := range occurrences {
		if occurrence.ParentKey != nil {
			children[*occurrence.ParentKey] = append(children[*occurrence.ParentKey], occurrence.Key)
		}
	}
	containers := []DiagramContainer{}
	for _, occurrence := range occurrences {
		childKeys := children[occurrence.Key]
		if len(childKeys) == 0 {
			continue
		}
		source := occurrence.Source
		for _, child := range occurrences {
			if child.ParentKey == nil || *child.ParentKey != occurrence.Key {
				continue
			}
			source = mergeViewDataSourceRefs(source, child.Source)
			if candidate, ok := adopted[child.EntityAddress]; ok {
				source = mergeViewDataSourceRefs(source, m.diagramRelationSource(candidate.relation))
			}
		}
		key := viewItemKey(m, "diagram-container", []any{m.input.Recipe.Address, occurrence.Key})
		containers = append(containers, DiagramContainer{Key: key, OccurrenceKey: occurrence.Key, ChildKeys: append([]string{}, childKeys...), Source: source})
	}
	return containers
}

func (m *viewMaterializer) validateDiagramPlacements(occurrences []DiagramOccurrence) {
	shape := m.input.Recipe.Shape.Diagram
	if shape == nil {
		m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Diagram View lacks a Diagram shape", m.input.Recipe.Address, "")
		return
	}
	visibleRoots := map[string]bool{}
	for _, occurrence := range occurrences {
		if occurrence.ParentKey == nil && occurrence.Role != DiagramRoleSupport {
			visibleRoots[occurrence.EntityAddress] = true
		}
	}
	placed := map[string]bool{}
	for _, placement := range shape.Placements {
		if placed[placement.EntityAddress] || !visibleRoots[placement.EntityAddress] {
			m.addDiag("LDL1702", "view_materialization_conflict", "Diagram placement must target one visible root occurrence", placement.EntityAddress, m.input.Recipe.Address)
			continue
		}
		placed[placement.EntityAddress] = true
	}
	if shape.Layout != "manual" {
		return
	}
	for _, occurrence := range occurrences {
		if visibleRoots[occurrence.EntityAddress] && !placed[occurrence.EntityAddress] {
			m.addDiag("LDL1702", "view_materialization_conflict", "manual Diagram layout requires placement for every visible root occurrence", occurrence.EntityAddress, m.input.Recipe.Address)
		}
	}
}

func (m *viewMaterializer) diagramProjectionEndpoints(relation graph.Relation, first, second *definition.ProjectionEndpoint) (string, string, bool) {
	if first == nil || second == nil {
		m.addDiag("LDL1504", "invalid_projection_contract", "effective composed projection endpoints are absent", relation.TypeAddress, m.input.Recipe.Address)
		return "", "", false
	}
	return m.diagramProjectionEndpointPair(relation, *first, *second)
}

func (m *viewMaterializer) diagramProjectionEndpointPair(relation graph.Relation, first, second definition.ProjectionEndpoint) (string, string, bool) {
	endpoint := func(value definition.ProjectionEndpoint) (string, bool) {
		switch value {
		case definition.ProjectionEndpointFrom:
			return relation.FromAddress, true
		case definition.ProjectionEndpointTo:
			return relation.ToAddress, true
		default:
			return "", false
		}
	}
	left, leftOK := endpoint(first)
	right, rightOK := endpoint(second)
	if !leftOK || !rightOK || first == second {
		m.addDiag("LDL1504", "invalid_projection_contract", "effective Diagram projection endpoints are invalid", relation.TypeAddress, m.input.Recipe.Address)
		return "", "", false
	}
	return left, right, true
}

func (m *viewMaterializer) diagramRelationSource(relation graph.Relation) ViewDataSourceRefs {
	values := []ViewDataSourceRefs{m.relationSource(relation)}
	for _, row := range relation.Rows {
		values = append(values, m.rowSource(relation.Address, false, row))
	}
	if entity, ok := m.entities[relation.FromAddress]; ok {
		values = append(values, m.diagramEntitySource(entity))
	}
	if entity, ok := m.entities[relation.ToAddress]; ok {
		values = append(values, m.diagramEntitySource(entity))
	}
	return mergeViewDataSourceRefs(values...)
}

func (m *viewMaterializer) diagramEntitySource(entity graph.Entity) ViewDataSourceRefs {
	values := []ViewDataSourceRefs{m.entitySource(entity)}
	for _, row := range entity.Rows {
		values = append(values, m.rowSource(entity.Address, true, row))
	}
	return mergeViewDataSourceRefs(values...)
}

func newDiagramSupportAccumulator(m *viewMaterializer) *diagramSupportAccumulator {
	return &diagramSupportAccumulator{materializer: m, items: []DiagramSupportItem{}, orders: []int{}, index: map[diagramSupportIdentity]int{}}
}

func (a *diagramSupportAccumulator) add(order int, kind DiagramSupportKind, entityAddress, relationAddress *string, source ViewDataSourceRefs) {
	identity := diagramSupportIdentity{kind: kind}
	if entityAddress != nil {
		identity.entity = *entityAddress
	}
	if relationAddress != nil {
		identity.relation = *relationAddress
	}
	if index, ok := a.index[identity]; ok {
		a.items[index].Source = mergeViewDataSourceRefs(a.items[index].Source, source)
		if order < a.orders[index] {
			a.orders[index] = order
		}
		return
	}
	tuple := []any{a.materializer.input.Recipe.Address, string(kind)}
	if entityAddress != nil {
		tuple = append(tuple, identity.entity)
	}
	if relationAddress != nil {
		tuple = append(tuple, identity.relation)
	}
	key := viewItemKey(a.materializer, "diagram-support", tuple)
	item := DiagramSupportItem{Key: key, SupportKind: kind, Source: canonicalViewDataSourceRefs(source)}
	if entityAddress != nil {
		value := *entityAddress
		item.EntityAddress = &value
	}
	if relationAddress != nil {
		value := *relationAddress
		item.RelationAddress = &value
	}
	a.index[identity] = len(a.items)
	a.items = append(a.items, item)
	a.orders = append(a.orders, order)
}

func (a *diagramSupportAccumulator) sortedItems() []DiagramSupportItem {
	indices := make([]int, len(a.items))
	for index := range indices {
		indices[index] = index
	}
	sort.SliceStable(indices, func(i, j int) bool {
		return a.orders[indices[i]] < a.orders[indices[j]]
	})
	items := make([]DiagramSupportItem, len(indices))
	for index, sourceIndex := range indices {
		items[index] = a.items[sourceIndex]
	}
	return items
}
