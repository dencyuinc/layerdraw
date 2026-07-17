// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"sort"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

type contextGroupBuilder struct {
	group      ContextGroup
	facts      []contextFactCandidate
	attributes map[string]ContextAttribute
}

type contextFactCandidate struct {
	focalAddress string
	relationType string
	fact         ContextFact
}

func (m *viewMaterializer) contextView(base ViewDataBase) *ContextViewData {
	shape := m.input.Recipe.Shape.Context
	if shape == nil {
		m.addDiag("LDL1701", "unsupported_view_shape_or_export", "Context View shape is missing", m.input.Recipe.Address, "")
		return &ContextViewData{ViewDataBase: base, Groups: []ContextGroup{}}
	}
	groups := map[string]*contextGroupBuilder{}
	selectedRelations := viewStringSet(m.relationAddresses())
	for _, address := range m.materializationEntityAddresses() {
		entity := m.entities[address]
		rawKey, label := m.contextGroupIdentity(entity, shape.GroupBy)
		builder := groups[rawKey]
		if builder == nil {
			builder = &contextGroupBuilder{
				group: ContextGroup{
					Key: viewItemKey(m, "context-group", []any{m.input.Recipe.Address, rawKey}), Label: label,
					Facts: []ContextFact{}, Attributes: []ContextAttribute{}, Source: emptyViewDataSourceRefs(),
				},
				attributes: map[string]ContextAttribute{},
			}
			groups[rawKey] = builder
		}
		builder.group.Source = mergeViewDataSourceRefs(builder.group.Source, m.entitySource(entity))
		if shape.IncludeEntityRows {
			for _, row := range entity.Rows {
				m.addContextAttribute(builder, entity.Address, true, row)
			}
		}
		m.addContextFacts(builder, entity, shape, selectedRelations)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return compareStableAddressText(keys[i], keys[j]) < 0 })
	result := make([]ContextGroup, 0, len(keys))
	for _, key := range keys {
		builder := groups[key]
		sort.Slice(builder.facts, func(i, j int) bool {
			left, right := builder.facts[i], builder.facts[j]
			if compared := compareStableAddressText(left.focalAddress, right.focalAddress); compared != 0 {
				return compared < 0
			}
			if left.fact.Direction != right.fact.Direction {
				return left.fact.Direction == ContextFactOutgoing
			}
			if compared := compareStableAddressText(left.relationType, right.relationType); compared != 0 {
				return compared < 0
			}
			return compareStableAddressText(left.fact.RelationAddress, right.fact.RelationAddress) < 0
		})
		for _, candidate := range builder.facts {
			builder.group.Facts = append(builder.group.Facts, candidate.fact)
		}
		rowAddresses := make([]string, 0, len(builder.attributes))
		for rowAddress := range builder.attributes {
			rowAddresses = append(rowAddresses, rowAddress)
		}
		rowAddresses = sortedUniqueStableAddresses(rowAddresses)
		for _, rowAddress := range rowAddresses {
			builder.group.Attributes = append(builder.group.Attributes, builder.attributes[rowAddress])
		}
		result = append(result, builder.group)
	}
	return &ContextViewData{ViewDataBase: base, Groups: result}
}

func (m *viewMaterializer) addContextFacts(builder *contextGroupBuilder, focal graph.Entity, shape *view.ContextShape, selected map[string]bool) {
	appendFact := func(relation graph.Relation, direction ContextFactDirection) {
		projection := m.contextProjection(relation.TypeAddress)
		rowAddresses := []string{}
		if shape.IncludeRelationRows && projection.IncludeAttributeRows {
			for _, row := range relation.Rows {
				rowAddresses = append(rowAddresses, row.Address)
				m.addContextAttribute(builder, relation.Address, false, row)
			}
		}
		source := mergeViewDataSourceRefs(m.entitySource(focal), m.relationSource(relation))
		for _, row := range relation.Rows {
			if containsString(rowAddresses, row.Address) {
				source = mergeViewDataSourceRefs(source, m.rowSource(relation.Address, false, row))
			}
		}
		fact := ContextFact{
			Key:       viewItemKey(m, "context-fact", []any{m.input.Recipe.Address, string(direction), focal.Address, relation.Address, rowAddresses}),
			Direction: direction, Text: m.contextFactText(relation, projection, direction), EntityAddress: focal.Address,
			RelationAddress: relation.Address, RowAddresses: sortedUniqueStableAddresses(rowAddresses), Source: source,
		}
		builder.facts = append(builder.facts, contextFactCandidate{focalAddress: focal.Address, relationType: relation.TypeAddress, fact: fact})
		builder.group.Source = mergeViewDataSourceRefs(builder.group.Source, source)
	}
	for _, relationAddress := range m.outgoing[focal.Address] {
		if selected[relationAddress] && shape.Outgoing {
			appendFact(m.relations[relationAddress], ContextFactOutgoing)
		}
	}
	for _, relationAddress := range m.incoming[focal.Address] {
		if !selected[relationAddress] || !shape.Incoming {
			continue
		}
		relation := m.relations[relationAddress]
		if relation.FromAddress == relation.ToAddress && shape.Outgoing && relation.FromAddress == focal.Address {
			appendFact(relation, ContextFactIncoming)
			continue
		}
		appendFact(relation, ContextFactIncoming)
	}
}

func (m *viewMaterializer) addContextAttribute(builder *contextGroupBuilder, owner string, entity bool, row graph.AttributeRow) {
	values := make(map[string]TypedScalar, len(row.Values))
	for _, cell := range row.Values {
		values[cell.ColumnAddress] = cell.Value
	}
	source := m.rowSource(owner, entity, row)
	if existing, ok := builder.attributes[row.Address]; ok {
		existing.Source = mergeViewDataSourceRefs(existing.Source, source)
		builder.attributes[row.Address] = existing
		return
	}
	attribute := ContextAttribute{
		Key:      viewItemKey(m, "context-attribute", []any{m.input.Recipe.Address, builder.group.Key, owner, row.Address}),
		GroupKey: builder.group.Key, OwnerAddress: owner, RowAddress: row.Address, Values: values, Source: source,
	}
	builder.attributes[row.Address] = attribute
	builder.group.Source = mergeViewDataSourceRefs(builder.group.Source, source)
}

func (m *viewMaterializer) contextGroupIdentity(entity graph.Entity, groupBy view.ContextGroupBy) (string, string) {
	switch groupBy {
	case view.ContextGroupLayer:
		return entity.LayerAddress, m.layers[entity.LayerAddress].DisplayName
	case view.ContextGroupEntityType:
		return entity.TypeAddress, m.entityTypes[entity.TypeAddress].DisplayName
	default:
		return "all", ""
	}
}

func (m *viewMaterializer) contextProjection(relationTypeAddress string) definition.ContextProjection {
	for _, projection := range m.input.Recipe.RelationProjections {
		if projection.RelationTypeAddress == relationTypeAddress {
			return projection.Projections.Context
		}
	}
	return m.relationTypes[relationTypeAddress].Projections.Context
}

func (m *viewMaterializer) contextFactText(relation graph.Relation, projection definition.ContextProjection, direction ContextFactDirection) string {
	template := projection.FactTemplate
	if direction == ContextFactIncoming && projection.ReverseFactTemplate != nil {
		template = *projection.ReverseFactTemplate
	}
	from := m.entities[relation.FromAddress]
	to := m.entities[relation.ToAddress]
	relationType := m.relationTypes[relation.TypeAddress]
	relationName := relationType.DisplayName
	if relation.DisplayName != nil {
		relationName = *relation.DisplayName
	}
	replacements := map[string]string{
		"{from.display_name}": from.DisplayName, "{to.display_name}": to.DisplayName,
		"{relation.display_name}": relationName,
	}
	for token, value := range replacements {
		template = strings.ReplaceAll(template, token, value)
	}
	return template
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
