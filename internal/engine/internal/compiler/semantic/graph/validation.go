// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package graph

import (
	"sort"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

type relationSpans struct {
	from syntax.Span
	to   syntax.Span
}

func (c *compiler) validateRelations() {
	valid := make([]bool, len(c.relations))
	for i, relation := range c.relations {
		typeDefinition, typeOK := c.relationTypes[relation.TypeAddress]
		fromIndex, fromOK := c.entityIndex[relation.FromAddress]
		toIndex, toOK := c.entityIndex[relation.ToAddress]
		if !typeOK || !fromOK || !toOK {
			continue
		}
		from := c.entities[fromIndex]
		to := c.entities[toIndex]
		valid[i] = true
		if relation.FromAddress == relation.ToAddress && !typeDefinition.AllowSelf {
			c.diag("LDL1501", "invalid_relation_endpoint_or_self_rule", c.sources[relation.Address], c.endpointSpans[relation.Address].to, "relation type forbids self relations", relation.Address, "")
			valid[i] = false
		}
		if !endpointAllows(typeDefinition.From, from) {
			c.diag("LDL1501", "invalid_relation_endpoint_or_self_rule", c.sources[relation.Address], c.endpointSpans[relation.Address].from, "from endpoint violates relation type restrictions", relation.Address, "")
			valid[i] = false
		}
		if !endpointAllows(typeDefinition.To, to) {
			c.diag("LDL1501", "invalid_relation_endpoint_or_self_rule", c.sources[relation.Address], c.endpointSpans[relation.Address].to, "to endpoint violates relation type restrictions", relation.Address, "")
			valid[i] = false
		}
	}
	c.validateDuplicates(valid)
	c.validateCardinality(valid)
}

func endpointAllows(rule definition.EndpointRule, entity Entity) bool {
	return (len(rule.EntityTypeAddresses) == 0 || contains(rule.EntityTypeAddresses, entity.TypeAddress)) &&
		(len(rule.LayerAddresses) == 0 || contains(rule.LayerAddresses, entity.LayerAddress))
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (c *compiler) validateDuplicates(valid []bool) {
	type endpointPair struct {
		from string
		to   string
	}
	previous := map[endpointPair][]int{}
	for i, relation := range c.relations {
		if !valid[i] {
			continue
		}
		pair := endpointPair{from: relation.FromAddress, to: relation.ToAddress}
		for _, priorIndex := range previous[pair] {
			prior := c.relations[priorIndex]
			if !c.duplicateConflict(prior.TypeAddress, relation.TypeAddress) {
				continue
			}
			c.diagRelated("LDL1502", "relation_duplicate_policy_violation", c.sources[relation.Address], c.sources[relation.Address].Range, "relation duplicate policy violation", relation.Address, "", c.sources[prior.Address])
			break
		}
		previous[pair] = append(previous[pair], i)
	}
}

func (c *compiler) duplicateConflict(firstType, secondType string) bool {
	first, firstOK := c.relationTypes[firstType]
	second, secondOK := c.relationTypes[secondType]
	if !firstOK || !secondOK {
		return false
	}
	if firstType == secondType {
		return first.DuplicatePolicy != definition.DuplicateAllow
	}
	return first.DuplicatePolicy == definition.DuplicateDenyAnyBetweenSameEndpoints ||
		second.DuplicatePolicy == definition.DuplicateDenyAnyBetweenSameEndpoints
}

func (c *compiler) validateCardinality(valid []bool) {
	type violationKey struct {
		entityAddress       string
		relationTypeAddress string
	}
	violations := map[violationKey][]cardinalityViolation{}
	for _, relationType := range c.input.Definition.RelationTypes {
		toByFrom := map[string]map[string]bool{}
		fromByTo := map[string]map[string]bool{}
		for i, relation := range c.relations {
			if !valid[i] || relation.TypeAddress != relationType.Address {
				continue
			}
			if toByFrom[relation.FromAddress] == nil {
				toByFrom[relation.FromAddress] = map[string]bool{}
			}
			toByFrom[relation.FromAddress][relation.ToAddress] = true
			if fromByTo[relation.ToAddress] == nil {
				fromByTo[relation.ToAddress] = map[string]bool{}
			}
			fromByTo[relation.ToAddress][relation.FromAddress] = true
		}
		for _, entity := range c.entities {
			if endpointAllows(relationType.From, entity) {
				if violatesBound(len(toByFrom[entity.Address]), relationType.Cardinality.ToPerFrom) {
					key := violationKey{entityAddress: entity.Address, relationTypeAddress: relationType.Address}
					violations[key] = append(violations[key], cardinalityViolation{direction: "to_per_from"})
				}
			}
			if endpointAllows(relationType.To, entity) {
				if violatesBound(len(fromByTo[entity.Address]), relationType.Cardinality.FromPerTo) {
					key := violationKey{entityAddress: entity.Address, relationTypeAddress: relationType.Address}
					violations[key] = append(violations[key], cardinalityViolation{direction: "from_per_to"})
				}
			}
		}
	}
	keys := make([]violationKey, 0, len(violations))
	for key := range violations {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].entityAddress != keys[j].entityAddress {
			return keys[i].entityAddress < keys[j].entityAddress
		}
		return keys[i].relationTypeAddress < keys[j].relationTypeAddress
	})
	for _, key := range keys {
		entity := c.entities[c.entityIndex[key.entityAddress]]
		c.emitCardinality(entity, key.relationTypeAddress, violations[key])
	}
}

func violatesBound(count int, bound definition.CardinalityBound) bool {
	return count < bound.Min || bound.Max == definition.CardinalityMaximumOne && count > 1
}

type cardinalityViolation struct {
	direction string
}

func (c *compiler) emitCardinality(entity Entity, relationTypeAddress string, violations []cardinalityViolation) {
	src := c.sources[entity.Address]
	span := src.Range
	if toks := directTokens(src.Node); len(toks) > 0 {
		span = toks[0].Span
	}
	c.diag("LDL1503", "relation_cardinality_violation", src, span, "relation cardinality violation", entity.Address, relationTypeAddress)
	diagnostic := &c.diagnostics[len(c.diagnostics)-1]
	typeSource := c.sources[relationTypeAddress]
	for _, violation := range violations {
		diagnostic.Related = append(diagnostic.Related, resolve.DiagnosticRelated{
			Relation:       "cause",
			Message:        violation.direction + " cardinality bound",
			Range:          sourceRange(typeSource, cardinalityBoundSpan(typeSource, violation.direction)),
			SubjectAddress: entity.Address,
			OwnerAddress:   relationTypeAddress,
		})
	}
}

func cardinalityBoundSpan(src resolve.DeclarationSource, direction string) syntax.Span {
	for _, nested := range descendants(src.Node, syntax.NodeNestedBlock) {
		toks := directTokens(nested)
		if len(toks) == 0 || toks[0].Raw != "cardinality" {
			continue
		}
		for _, statement := range nodeChildren(firstNode(nested, syntax.NodeBlock)) {
			head := directTokens(statement)
			if statement.Kind != syntax.NodeStatement || len(head) == 0 || head[0].Raw != direction {
				continue
			}
			for _, child := range nodeChildren(statement) {
				if child.Kind == syntax.NodeValue {
					return child.Span
				}
			}
			return head[0].Span
		}
	}
	return src.Range
}
