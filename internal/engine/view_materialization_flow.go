// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"container/heap"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

type flowProjectionInfo struct {
	projection   definition.FlowProjection
	branchColumn *definition.Column
}

type flowConnectorCandidate struct {
	key               string
	from              string
	to                string
	kind              FlowConnectorKind
	branchValue       *TypedScalar
	branchRows        []string
	relationAddresses []string
	source            ViewDataSourceRefs
}

type flowScalarIdentity struct {
	present bool
	typeTag definition.ScalarType
	text    string
	integer int64
	number  uint64
	boolean bool
}

type flowConnectorIdentity struct {
	from   string
	to     string
	kind   FlowConnectorKind
	branch flowScalarIdentity
}

type flowLaneDescriptor struct {
	mode    view.LaneBy
	address string
	missing bool
	value   *TypedScalar
	key     string
	label   string
	source  ViewDataSourceRefs
}

func (m *viewMaterializer) flow(base ViewDataBase) *FlowViewData {
	result := &FlowViewData{ViewDataBase: base, Lanes: []FlowLane{}, Steps: []FlowStep{}, Connectors: []FlowConnector{}, CycleRefs: []FlowCycleRef{}}
	shape := m.input.Recipe.Shape.Flow
	if shape == nil {
		m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Flow View shape is missing", m.input.Recipe.Address, "")
		return result
	}
	entities := m.materializationEntityAddresses()
	visible := viewStringSet(entities)
	candidates, laneColumns := m.flowCandidates(*shape, visible)
	if hasViewErrorDiagnostics(m.diagnostics) {
		return result
	}
	if !shape.PreserveParallel {
		if !m.reserveViewWork(int64(len(candidates))) {
			return result
		}
		candidates = mergeFlowCandidates(candidates)
	}
	sort.Slice(candidates, func(i, j int) bool { return compareFlowConnectorCandidates(candidates[i], candidates[j]) < 0 })
	for index := range candidates {
		candidates[index].key = m.flowConnectorKey(candidates[index])
	}
	connectors, cycleCandidates := m.applyFlowCyclePolicy(*shape, candidates)
	if hasViewErrorDiagnostics(m.diagnostics) {
		return result
	}
	stepOrder := m.flowStepOrder(entities, connectors)
	if hasViewErrorDiagnostics(m.diagnostics) {
		return result
	}

	descriptors := make(map[string]flowLaneDescriptor, len(entities))
	entityLaneIdentity := make(map[string]string, len(entities))
	for _, address := range entities {
		entity := m.entities[address]
		descriptor, ok := m.flowLaneDescriptor(*shape, laneColumns, entity)
		if !ok {
			return result
		}
		identity := flowLaneIdentity(descriptor)
		entityLaneIdentity[address] = identity
		if current, exists := descriptors[identity]; exists {
			current.source = mergeViewDataSourceRefs(current.source, descriptor.source)
			descriptors[identity] = current
		} else {
			descriptors[identity] = descriptor
		}
	}
	orderedDescriptors := make([]flowLaneDescriptor, 0, len(descriptors))
	for _, descriptor := range descriptors {
		orderedDescriptors = append(orderedDescriptors, descriptor)
	}
	sort.Slice(orderedDescriptors, func(i, j int) bool {
		return compareFlowLaneDescriptors(orderedDescriptors[i], orderedDescriptors[j]) < 0
	})
	laneIndex := make(map[string]int, len(orderedDescriptors))
	for _, descriptor := range orderedDescriptors {
		if !m.reserveViewItems(1) {
			return result
		}
		laneIndex[flowLaneIdentity(descriptor)] = len(result.Lanes)
		result.Lanes = append(result.Lanes, FlowLane{Key: descriptor.key, Label: descriptor.label, StepKeys: []string{}, Source: descriptor.source})
	}

	stepKeys := make(map[string]string, len(entities))
	indegree, outdegree := flowDegrees(connectors)
	for _, address := range stepOrder {
		if !m.reserveViewItems(1) {
			return result
		}
		entity, ok := m.entities[address]
		if !ok {
			m.addDiag("LDL1702", "view_materialization_conflict", "Flow step Entity is absent from the immutable graph", address, m.input.Recipe.Address)
			return result
		}
		identity := entityLaneIdentity[address]
		index, ok := laneIndex[identity]
		if !ok {
			m.addDiag("LDL1702", "view_materialization_conflict", "Flow step has no lane", address, m.input.Recipe.Address)
			return result
		}
		key := viewItemKey(m, "flow-step", []any{m.input.Recipe.Address, address})
		stepKeys[address] = key
		result.Steps = append(result.Steps, FlowStep{
			Key: key, EntityAddress: address, LaneKey: result.Lanes[index].Key,
			Branch: outdegree[address] > 1, Join: indegree[address] > 1,
			Source: m.diagramEntitySource(entity),
		})
		result.Lanes[index].StepKeys = append(result.Lanes[index].StepKeys, key)
		result.Lanes[index].Source = mergeViewDataSourceRefs(result.Lanes[index].Source, m.entitySource(entity))
	}
	for _, candidate := range connectors {
		if !m.reserveViewItems(1) {
			return result
		}
		result.Connectors = append(result.Connectors, flowConnector(candidate, stepKeys))
	}
	for _, candidate := range cycleCandidates {
		if !m.reserveViewItems(1) {
			return result
		}
		result.CycleRefs = append(result.CycleRefs, flowCycleRef(m, candidate, stepKeys))
	}
	return result
}

func (m *viewMaterializer) flowCandidates(shape view.FlowShape, visible map[string]bool) ([]flowConnectorCandidate, map[string]definition.Column) {
	if len(shape.RelationTypeAddresses) == 0 || !canonicalStableAddressSlice(shape.RelationTypeAddresses) {
		m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Flow relation_types must be non-empty, unique, and canonical", m.input.Recipe.Address, "")
		return nil, nil
	}
	if shape.CyclePolicy != view.FlowCycleError && shape.CyclePolicy != view.FlowCycleTruncate && shape.CyclePolicy != view.FlowCycleIncludeCycleRef {
		m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Flow cycle policy is invalid", m.input.Recipe.Address, "")
		return nil, nil
	}
	laneColumns, laneOK := m.flowLaneColumns(shape)
	if !laneOK {
		return nil, nil
	}
	allowed := viewStringSet(shape.RelationTypeAddresses)
	projectionByType := make(map[string]flowProjectionInfo, len(allowed))
	var branchSchema *definition.Column
	for _, address := range shape.RelationTypeAddresses {
		projectionSet, ok := m.effectiveViewProjectionSet(address)
		if !ok || !validFlowProjection(projectionSet.Flow) {
			m.addDiag("LDL1504", "invalid_projection_contract", "effective Flow projection is missing or invalid", address, m.input.Recipe.Address)
			continue
		}
		info := flowProjectionInfo{projection: *projectionSet.Flow}
		if projectionSet.Flow.BranchValueColumnAddress != nil {
			column, ok := relationTypeColumn(m.relationTypes[address], *projectionSet.Flow.BranchValueColumnAddress)
			if !ok {
				m.addDiag("LDL1504", "invalid_projection_contract", "Flow branch Column does not belong to its RelationType", *projectionSet.Flow.BranchValueColumnAddress, m.input.Recipe.Address)
				continue
			}
			info.branchColumn = &column
			if branchSchema != nil && !sameFlowColumnSchema(*branchSchema, column) {
				m.addDiag("LDL1504", "invalid_projection_contract", "Flow branch Columns have incompatible scalar schemas", column.Address, m.input.Recipe.Address)
				continue
			}
			copy := column
			branchSchema = &copy
		}
		projectionByType[address] = info
	}
	values := []flowConnectorCandidate{}
	for _, address := range m.relationAddresses() {
		if !m.reserveViewWork(1) {
			return nil, nil
		}
		if err := pollContext(m.ctx, "view_flow_candidates"); err != nil {
			m.addDiag("LDL1801", "stale_revision_or_semantic_hash", err.Error(), m.input.Recipe.Address, "")
			return nil, nil
		}
		relation, ok := m.relations[address]
		if !ok {
			m.addDiag("LDL1702", "view_materialization_conflict", "Flow Relation is absent from the immutable graph", address, m.input.Recipe.Address)
			continue
		}
		if !allowed[relation.TypeAddress] {
			continue
		}
		info, ok := projectionByType[relation.TypeAddress]
		if !ok {
			continue
		}
		from, to, ok := projectionEndpointPair(relation, info.projection.SourceEndpoint, info.projection.TargetEndpoint)
		if !ok {
			m.addDiag("LDL1504", "invalid_projection_contract", "effective Flow projection endpoints are invalid", relation.TypeAddress, m.input.Recipe.Address)
			continue
		}
		if !visible[from] || !visible[to] {
			m.addDiag("LDL1702", "view_materialization_conflict", "Flow Relation endpoint has no materialization Entity", relation.Address, m.input.Recipe.Address)
			continue
		}
		kind := FlowConnectorKind(info.projection.ConnectorKind)
		baseSource := mergeViewDataSourceRefs(m.relationSource(relation), m.entitySource(m.entities[from]), m.entitySource(m.entities[to]))
		if info.branchColumn == nil || len(relation.Rows) == 0 {
			values = append(values, flowConnectorCandidate{from: from, to: to, kind: kind, branchRows: []string{}, relationAddresses: []string{relation.Address}, source: baseSource})
			if !m.withinViewCandidateLimit(len(values)) {
				return nil, nil
			}
			continue
		}
		for _, row := range relation.Rows {
			if !m.reserveViewWork(int64(len(row.Values)) + 1) {
				return nil, nil
			}
			candidate := flowConnectorCandidate{from: from, to: to, kind: kind, branchRows: []string{row.Address}, relationAddresses: []string{relation.Address}}
			rowSource := baseSource
			rowSource.RowAddresses = append(rowSource.RowAddresses, row.Address)
			rowSource.State.Reads = append(rowSource.State.Reads, m.stateReadsForSubjects(row.Address)...)
			for _, cell := range row.Values {
				if cell.ColumnAddress != info.branchColumn.Address {
					continue
				}
				value := cell.Value
				candidate.branchValue = &value
				rowSource.CellRefs = append(rowSource.CellRefs, ViewDataCellRef{RowAddress: row.Address, ColumnAddress: cell.ColumnAddress})
			}
			candidate.source = canonicalViewDataSourceRefs(rowSource)
			values = append(values, candidate)
			if !m.withinViewCandidateLimit(len(values)) {
				return nil, nil
			}
		}
	}
	return values, laneColumns
}

func (m *viewMaterializer) flowLaneColumns(shape view.FlowShape) (map[string]definition.Column, bool) {
	validMode := shape.LaneBy == view.LaneNone || shape.LaneBy == view.LaneLayer || shape.LaneBy == view.LaneEntityType || shape.LaneBy == view.LaneAttribute
	if !validMode {
		m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Flow lane mode is invalid", m.input.Recipe.Address, "")
		return nil, false
	}
	if shape.LaneBy != view.LaneAttribute {
		if shape.LaneColumnAddresses != nil {
			m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "non-attribute Flow lane forbids lane Columns", m.input.Recipe.Address, "")
			return nil, false
		}
		return map[string]definition.Column{}, true
	}
	if shape.LaneColumnAddresses == nil || len(*shape.LaneColumnAddresses) == 0 || !canonicalStableAddressSlice(*shape.LaneColumnAddresses) {
		m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "attribute Flow lane requires non-empty canonical lane Columns", m.input.Recipe.Address, "")
		return nil, false
	}
	all := map[string]definition.Column{}
	for _, entityType := range m.entityTypes {
		for _, column := range entityType.Columns {
			all[column.Address] = column
		}
	}
	result := make(map[string]definition.Column, len(*shape.LaneColumnAddresses))
	var schema *definition.Column
	for _, address := range *shape.LaneColumnAddresses {
		column, ok := all[address]
		if !ok {
			m.addDiag("LDL1504", "invalid_projection_contract", "Flow lane Column is absent or is not owned by an EntityType", address, m.input.Recipe.Address)
			return nil, false
		}
		if schema != nil && !sameFlowColumnSchema(*schema, column) {
			m.addDiag("LDL1504", "invalid_projection_contract", "Flow lane Columns have incompatible scalar schemas", address, m.input.Recipe.Address)
			return nil, false
		}
		copy := column
		schema = &copy
		result[address] = column
	}
	return result, true
}

func (m *viewMaterializer) flowLaneDescriptor(shape view.FlowShape, columns map[string]definition.Column, entity graph.Entity) (flowLaneDescriptor, bool) {
	descriptor := flowLaneDescriptor{mode: shape.LaneBy, source: emptyViewDataSourceRefs()}
	switch shape.LaneBy {
	case view.LaneNone:
	case view.LaneLayer:
		layer, ok := m.layers[entity.LayerAddress]
		if !ok {
			m.addDiag("LDL1702", "view_materialization_conflict", "Flow lane Layer is absent from the immutable definition", entity.LayerAddress, m.input.Recipe.Address)
			return flowLaneDescriptor{}, false
		}
		descriptor.address, descriptor.label = layer.Address, layer.DisplayName
		descriptor.source.SubjectAddresses = []string{layer.Address}
		descriptor.source.LayerAddresses = []string{layer.Address}
		descriptor.source = canonicalViewDataSourceRefs(descriptor.source)
	case view.LaneEntityType:
		entityType, ok := m.entityTypes[entity.TypeAddress]
		if !ok {
			m.addDiag("LDL1702", "view_materialization_conflict", "Flow lane EntityType is absent from the immutable definition", entity.TypeAddress, m.input.Recipe.Address)
			return flowLaneDescriptor{}, false
		}
		descriptor.address, descriptor.label = entityType.Address, entityType.DisplayName
		descriptor.source.SubjectAddresses = []string{entityType.Address}
		descriptor.source = canonicalViewDataSourceRefs(descriptor.source)
	case view.LaneAttribute:
		values := map[flowScalarIdentity]TypedScalar{}
		refs := emptyViewDataSourceRefs()
		for _, row := range entity.Rows {
			for _, cell := range row.Values {
				if _, ok := columns[cell.ColumnAddress]; !ok {
					continue
				}
				identity := scalarIdentity(&cell.Value)
				values[identity] = cell.Value
				cellSource := m.entitySource(entity)
				cellSource.RowAddresses = []string{row.Address}
				cellSource.CellRefs = []ViewDataCellRef{{RowAddress: row.Address, ColumnAddress: cell.ColumnAddress}}
				cellSource.State.Reads = append(cellSource.State.Reads, m.stateReadsForSubjects(row.Address)...)
				refs = mergeViewDataSourceRefs(refs, cellSource)
			}
		}
		if len(values) > 1 {
			m.addDiag("LDL1702", "view_materialization_conflict", "Flow step has multiple distinct attribute lane values", entity.Address, m.input.Recipe.Address)
			return flowLaneDescriptor{}, false
		}
		if len(values) == 0 {
			descriptor.missing = true
		} else {
			for _, value := range values {
				copy := value
				descriptor.value = &copy
				descriptor.label = flowScalarLabel(value)
			}
		}
		descriptor.source = refs
	default:
		m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Flow lane mode is invalid", m.input.Recipe.Address, "")
		return flowLaneDescriptor{}, false
	}
	descriptor.key = viewItemKey(m, "flow-lane", []any{m.input.Recipe.Address, string(descriptor.mode), descriptor.address, descriptor.missing, descriptor.value})
	return descriptor, true
}

func mergeFlowCandidates(values []flowConnectorCandidate) []flowConnectorCandidate {
	result := []flowConnectorCandidate{}
	index := map[flowConnectorIdentity]int{}
	for _, candidate := range values {
		identity := flowConnectorIdentity{from: candidate.from, to: candidate.to, kind: candidate.kind, branch: scalarIdentity(candidate.branchValue)}
		if existing, ok := index[identity]; ok {
			result[existing].relationAddresses = sortedUniqueStableAddresses(append(result[existing].relationAddresses, candidate.relationAddresses...))
			result[existing].branchRows = sortedUniqueStableAddresses(append(result[existing].branchRows, candidate.branchRows...))
			result[existing].source = mergeViewDataSourceRefs(result[existing].source, candidate.source)
			continue
		}
		candidate.relationAddresses = sortedUniqueStableAddresses(candidate.relationAddresses)
		candidate.branchRows = sortedUniqueStableAddresses(candidate.branchRows)
		index[identity] = len(result)
		result = append(result, candidate)
	}
	return result
}

func (m *viewMaterializer) applyFlowCyclePolicy(shape view.FlowShape, candidates []flowConnectorCandidate) ([]flowConnectorCandidate, []flowConnectorCandidate) {
	accepted := []flowConnectorCandidate{}
	cycles := []flowConnectorCandidate{}
	adjacency := map[string][]string{}
	for _, candidate := range candidates {
		if err := pollContext(m.ctx, "view_flow_cycles"); err != nil {
			m.addDiag("LDL1801", "stale_revision_or_semantic_hash", err.Error(), m.input.Recipe.Address, "")
			return nil, nil
		}
		closes := candidate.from == candidate.to
		if !closes {
			var ok bool
			closes, ok = m.flowPathExists(adjacency, candidate.to, candidate.from)
			if !ok {
				return nil, nil
			}
		}
		if closes {
			switch shape.CyclePolicy {
			case view.FlowCycleError:
				m.addDiag("LDL1702", "view_materialization_conflict", "Flow encountered a forbidden directed cycle", candidate.relationAddresses[0], m.input.Recipe.Address)
				return nil, nil
			case view.FlowCycleTruncate:
				cycles = append(cycles, candidate)
				continue
			case view.FlowCycleIncludeCycleRef:
				cycles = append(cycles, candidate)
			default:
				m.addDiag("LDL1701", "invalid_view_source_category_or_shape", "Flow cycle policy is invalid", m.input.Recipe.Address, "")
				return nil, nil
			}
		}
		accepted = append(accepted, candidate)
		neighbors := adjacency[candidate.from]
		if len(neighbors) == 0 || neighbors[len(neighbors)-1] != candidate.to {
			adjacency[candidate.from] = append(neighbors, candidate.to)
		}
	}
	return accepted, cycles
}

func (m *viewMaterializer) flowStepOrder(entities []string, connectors []flowConnectorCandidate) []string {
	work := int64(len(entities))*6 + int64(len(connectors))*5 + 1
	if !m.reserveViewWork(work) {
		return nil
	}
	if err := pollContext(m.ctx, "view_flow_step_order"); err != nil {
		m.addDiag("LDL1801", "stale_revision_or_semantic_hash", err.Error(), m.input.Recipe.Address, "")
		return nil
	}
	adjacency := make(map[string][]string, len(entities))
	reverse := make(map[string][]string, len(entities))
	for _, address := range entities {
		adjacency[address] = []string{}
		reverse[address] = []string{}
	}
	for _, connector := range connectors {
		adjacency[connector.from] = append(adjacency[connector.from], connector.to)
		reverse[connector.to] = append(reverse[connector.to], connector.from)
	}
	for _, address := range entities {
		adjacency[address] = sortedUniqueStableAddresses(adjacency[address])
		reverse[address] = sortedUniqueStableAddresses(reverse[address])
	}
	finish := flowFinishingOrder(entities, adjacency)
	visited := map[string]bool{}
	components := [][]string{}
	componentByEntity := map[string]int{}
	for index := len(finish) - 1; index >= 0; index-- {
		root := finish[index]
		if visited[root] {
			continue
		}
		component := []string{}
		stack := []string{root}
		visited[root] = true
		for len(stack) != 0 {
			last := len(stack) - 1
			current := stack[last]
			stack = stack[:last]
			component = append(component, current)
			for neighborIndex := len(reverse[current]) - 1; neighborIndex >= 0; neighborIndex-- {
				neighbor := reverse[current][neighborIndex]
				if !visited[neighbor] {
					visited[neighbor] = true
					stack = append(stack, neighbor)
				}
			}
		}
		sort.Slice(component, func(i, j int) bool { return compareStableAddressText(component[i], component[j]) < 0 })
		componentID := len(components)
		for _, address := range component {
			componentByEntity[address] = componentID
		}
		components = append(components, component)
	}
	componentEdges := make([]map[int]bool, len(components))
	indegree := make([]int, len(components))
	for index := range componentEdges {
		componentEdges[index] = map[int]bool{}
	}
	for from, neighbors := range adjacency {
		fromComponent := componentByEntity[from]
		for _, to := range neighbors {
			toComponent := componentByEntity[to]
			if fromComponent != toComponent && !componentEdges[fromComponent][toComponent] {
				componentEdges[fromComponent][toComponent] = true
				indegree[toComponent]++
			}
		}
	}
	ready := &flowComponentHeap{components: components}
	heap.Init(ready)
	for componentID, value := range indegree {
		if value == 0 {
			heap.Push(ready, componentID)
		}
	}
	ordered := make([]string, 0, len(entities))
	for ready.Len() != 0 {
		componentID := heap.Pop(ready).(int)
		ordered = append(ordered, components[componentID]...)
		neighbors := make([]int, 0, len(componentEdges[componentID]))
		for neighbor := range componentEdges[componentID] {
			neighbors = append(neighbors, neighbor)
		}
		sort.Slice(neighbors, func(i, j int) bool {
			return compareStableAddressText(components[neighbors[i]][0], components[neighbors[j]][0]) < 0
		})
		for _, neighbor := range neighbors {
			indegree[neighbor]--
			if indegree[neighbor] == 0 {
				heap.Push(ready, neighbor)
			}
		}
	}
	if len(ordered) != len(entities) {
		m.addDiag("LDL1702", "view_materialization_conflict", "Flow component ordering did not cover every step", m.input.Recipe.Address, "")
		return nil
	}
	return ordered
}

type flowDFSFrame struct {
	address string
	next    int
}

func flowFinishingOrder(entities []string, adjacency map[string][]string) []string {
	visited := map[string]bool{}
	finish := make([]string, 0, len(entities))
	for _, root := range entities {
		if visited[root] {
			continue
		}
		visited[root] = true
		stack := []flowDFSFrame{{address: root}}
		for len(stack) != 0 {
			frame := &stack[len(stack)-1]
			if frame.next < len(adjacency[frame.address]) {
				neighbor := adjacency[frame.address][frame.next]
				frame.next++
				if !visited[neighbor] {
					visited[neighbor] = true
					stack = append(stack, flowDFSFrame{address: neighbor})
				}
				continue
			}
			finish = append(finish, frame.address)
			stack = stack[:len(stack)-1]
		}
	}
	return finish
}

type flowComponentHeap struct {
	values     []int
	components [][]string
}

func (h flowComponentHeap) Len() int { return len(h.values) }
func (h flowComponentHeap) Less(i, j int) bool {
	return compareStableAddressText(h.components[h.values[i]][0], h.components[h.values[j]][0]) < 0
}
func (h flowComponentHeap) Swap(i, j int)   { h.values[i], h.values[j] = h.values[j], h.values[i] }
func (h *flowComponentHeap) Push(value any) { h.values = append(h.values, value.(int)) }
func (h *flowComponentHeap) Pop() any {
	last := len(h.values) - 1
	value := h.values[last]
	h.values = h.values[:last]
	return value
}

func flowConnector(candidate flowConnectorCandidate, stepKeys map[string]string) FlowConnector {
	return FlowConnector{
		Key: candidate.key, FromStepKey: stepKeys[candidate.from], ToStepKey: stepKeys[candidate.to], Kind: candidate.kind,
		BranchValue: cloneScalarPointer(candidate.branchValue), BranchRowAddresses: append([]string{}, candidate.branchRows...),
		RelationAddresses: append([]string{}, candidate.relationAddresses...), Source: deepClone(candidate.source),
	}
}

func flowCycleRef(m *viewMaterializer, candidate flowConnectorCandidate, stepKeys map[string]string) FlowCycleRef {
	return FlowCycleRef{
		Key: viewItemKey(m, "flow-cycle-ref", []any{m.input.Recipe.Address, candidate.key}), ConnectorKey: candidate.key,
		FromStepKey: stepKeys[candidate.from], ToStepKey: stepKeys[candidate.to], Kind: candidate.kind,
		BranchValue: cloneScalarPointer(candidate.branchValue), BranchRowAddresses: append([]string{}, candidate.branchRows...),
		RelationAddresses: append([]string{}, candidate.relationAddresses...), Source: deepClone(candidate.source),
	}
}

func (m *viewMaterializer) flowConnectorKey(candidate flowConnectorCandidate) string {
	return viewItemKey(m, "flow-connector", []any{
		m.input.Recipe.Address, candidate.from, candidate.to, string(candidate.kind), candidate.branchValue != nil,
		candidate.branchValue, candidate.relationAddresses, candidate.branchRows,
	})
}

func flowDegrees(connectors []flowConnectorCandidate) (map[string]int, map[string]int) {
	indegree, outdegree := map[string]int{}, map[string]int{}
	for _, connector := range connectors {
		outdegree[connector.from]++
		indegree[connector.to]++
	}
	return indegree, outdegree
}

func (m *viewMaterializer) flowPathExists(adjacency map[string][]string, start, target string) (bool, bool) {
	visited := map[string]bool{start: true}
	stack := []string{start}
	for len(stack) != 0 {
		if !m.reserveViewWork(1) {
			return false, false
		}
		if m.materializationWork%1024 == 0 {
			if err := pollContext(m.ctx, "view_flow_cycle_path"); err != nil {
				m.addDiag("LDL1801", "stale_revision_or_semantic_hash", err.Error(), m.input.Recipe.Address, "")
				return false, false
			}
		}
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		if current == target {
			return true, true
		}
		for index := len(adjacency[current]) - 1; index >= 0; index-- {
			neighbor := adjacency[current][index]
			if !visited[neighbor] {
				visited[neighbor] = true
				stack = append(stack, neighbor)
			}
		}
	}
	return false, true
}

func compareFlowConnectorCandidates(left, right flowConnectorCandidate) int {
	for _, pair := range [][2]string{{left.from, right.from}, {left.to, right.to}} {
		if compared := compareStableAddressText(pair[0], pair[1]); compared != 0 {
			return compared
		}
	}
	if compared := flowConnectorKindRank(left.kind) - flowConnectorKindRank(right.kind); compared != 0 {
		return compared
	}
	if (left.branchValue != nil) != (right.branchValue != nil) {
		if left.branchValue == nil {
			return -1
		}
		return 1
	}
	if left.branchValue != nil {
		if compared := compareFlowScalars(*left.branchValue, *right.branchValue); compared != 0 {
			return compared
		}
	}
	if compared := compareFlowAddressSlices(left.relationAddresses, right.relationAddresses); compared != 0 {
		return compared
	}
	return compareFlowAddressSlices(left.branchRows, right.branchRows)
}

func compareFlowAddressSlices(left, right []string) int {
	for index := 0; index < len(left) && index < len(right); index++ {
		if compared := compareStableAddressText(left[index], right[index]); compared != 0 {
			return compared
		}
	}
	return len(left) - len(right)
}

func compareFlowLaneDescriptors(left, right flowLaneDescriptor) int {
	if left.mode != right.mode {
		return strings.Compare(string(left.mode), string(right.mode))
	}
	if left.address != right.address {
		return compareStableAddressText(left.address, right.address)
	}
	if left.missing != right.missing {
		if left.missing {
			return -1
		}
		return 1
	}
	if left.value == nil || right.value == nil {
		return 0
	}
	return compareFlowScalars(*left.value, *right.value)
}

func compareFlowScalars(left, right TypedScalar) int {
	if left.Type != right.Type {
		return flowScalarTypeRank(left.Type) - flowScalarTypeRank(right.Type)
	}
	switch left.Type {
	case definition.ScalarString, definition.ScalarEnum, definition.ScalarDate, definition.ScalarDatetime:
		return strings.Compare(left.String, right.String)
	case definition.ScalarInteger:
		return compareInt(left.Int, right.Int)
	case definition.ScalarNumber:
		return compareFloat(left.Float, right.Float)
	case definition.ScalarBoolean:
		return compareInt(boolRank(left.Bool), boolRank(right.Bool))
	default:
		return strings.Compare(string(left.Type), string(right.Type))
	}
}

func flowScalarTypeRank(value definition.ScalarType) int {
	switch value {
	case definition.ScalarString:
		return 0
	case definition.ScalarEnum:
		return 1
	case definition.ScalarInteger:
		return 2
	case definition.ScalarNumber:
		return 3
	case definition.ScalarBoolean:
		return 4
	case definition.ScalarDate:
		return 5
	case definition.ScalarDatetime:
		return 6
	default:
		return 7
	}
}

func flowConnectorKindRank(value FlowConnectorKind) int {
	switch value {
	case FlowConnectorSequence:
		return 0
	case FlowConnectorControl:
		return 1
	case FlowConnectorData:
		return 2
	case FlowConnectorMessage:
		return 3
	case FlowConnectorError:
		return 4
	default:
		return 5
	}
}

func flowLaneIdentity(value flowLaneDescriptor) string {
	identity := scalarIdentity(value.value)
	return strings.Join([]string{
		string(value.mode), value.address, strconv.FormatBool(value.missing), string(identity.typeTag), identity.text,
		strconv.FormatInt(identity.integer, 10), strconv.FormatUint(identity.number, 10), strconv.FormatBool(identity.boolean),
	}, "\x00")
}

func scalarIdentity(value *TypedScalar) flowScalarIdentity {
	if value == nil {
		return flowScalarIdentity{}
	}
	identity := flowScalarIdentity{present: true, typeTag: value.Type}
	switch value.Type {
	case definition.ScalarString, definition.ScalarEnum, definition.ScalarDate, definition.ScalarDatetime:
		identity.text = value.String
	case definition.ScalarInteger:
		identity.integer = value.Int
	case definition.ScalarNumber:
		number := value.Float
		if number == 0 {
			number = 0
		}
		identity.number = math.Float64bits(number)
	case definition.ScalarBoolean:
		identity.boolean = value.Bool
	}
	return identity
}

func flowScalarLabel(value TypedScalar) string {
	switch value.Type {
	case definition.ScalarString, definition.ScalarEnum, definition.ScalarDate, definition.ScalarDatetime:
		return value.String
	case definition.ScalarInteger:
		return strconv.FormatInt(value.Int, 10)
	case definition.ScalarNumber:
		return strconv.FormatFloat(value.Float, 'g', -1, 64)
	case definition.ScalarBoolean:
		return strconv.FormatBool(value.Bool)
	default:
		return ""
	}
}

func relationTypeColumn(relationType definition.RelationType, address string) (definition.Column, bool) {
	for _, column := range relationType.Columns {
		if column.Address == address {
			return column, true
		}
	}
	return definition.Column{}, false
}

func sameFlowColumnSchema(left, right definition.Column) bool {
	return left.ValueType == right.ValueType && reflect.DeepEqual(left.EnumValues, right.EnumValues) && equalStringFormat(left.Format, right.Format)
}

func equalStringFormat(left, right *definition.StringFormat) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func validFlowProjection(value *definition.FlowProjection) bool {
	if value == nil || !validProjectionEndpoint(value.SourceEndpoint) || !validProjectionEndpoint(value.TargetEndpoint) || value.SourceEndpoint == value.TargetEndpoint {
		return false
	}
	switch value.ConnectorKind {
	case definition.FlowConnectorSequence, definition.FlowConnectorControl, definition.FlowConnectorData, definition.FlowConnectorMessage, definition.FlowConnectorError:
		return true
	default:
		return false
	}
}

func cloneScalarPointer(value *TypedScalar) *TypedScalar {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
