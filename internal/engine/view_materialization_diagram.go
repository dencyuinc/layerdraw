// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

func (m *viewMaterializer) diagram(base ViewDataBase) *DiagramViewData {
	entityAddresses := m.materializationEntityAddresses()
	primary := viewStringSet(m.primaryEntityAddresses())
	occurrences := make([]DiagramOccurrence, 0, len(entityAddresses))
	occurrenceByEntity := make(map[string]string, len(entityAddresses))
	for _, address := range entityAddresses {
		entity := m.entities[address]
		key := viewItemKey(m, "diagram-occurrence", []any{m.input.Recipe.Address, entity.Address})
		role := DiagramRoleSupport
		if primary[address] {
			role = DiagramRoleNode
		}
		occurrences = append(occurrences, DiagramOccurrence{
			Key: key, EntityAddress: entity.Address, LayerAddress: entity.LayerAddress, Role: role,
			Source: m.entitySource(entity),
		})
		occurrenceByEntity[address] = key
	}
	edges := make([]DiagramEdge, 0, len(m.relationAddresses()))
	for _, address := range m.relationAddresses() {
		relation := m.relations[address]
		fromKey, fromOK := occurrenceByEntity[relation.FromAddress]
		toKey, toOK := occurrenceByEntity[relation.ToAddress]
		if !fromOK || !toOK {
			m.addDiag("LDL1701", "unsupported_view_shape_or_export", "Diagram Relation endpoint has no occurrence", relation.Address, m.input.Recipe.Address)
			continue
		}
		key := viewItemKey(m, "diagram-edge", []any{m.input.Recipe.Address, relation.Address, fromKey, toKey})
		edges = append(edges, DiagramEdge{
			Key: key, FromOccurrenceKey: fromKey, ToOccurrenceKey: toKey,
			RelationAddress: relation.Address, RelationTypeAddress: relation.TypeAddress,
			Source: m.relationSource(relation),
		})
	}
	return &DiagramViewData{
		ViewDataBase: base, Occurrences: occurrences, Edges: edges,
		Containers: []DiagramContainer{}, Overlays: []DiagramOverlay{}, Badges: []DiagramBadge{}, SupportItems: []DiagramSupportItem{},
	}
}
