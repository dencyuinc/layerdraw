// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package graph

import (
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
)

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
			c.diag("LDL1501", "invalid_relation_endpoint_or_self_rule", c.sources[relation.Address], c.sources[relation.Address].Range, "relation type forbids self relations", relation.Address, "")
			valid[i] = false
		}
		if !endpointAllows(typeDefinition.From, from) {
			c.diag("LDL1501", "invalid_relation_endpoint_or_self_rule", c.sources[relation.Address], c.sources[relation.Address].Range, "from endpoint violates relation type restrictions", relation.Address, "")
			valid[i] = false
		}
		if !endpointAllows(typeDefinition.To, to) {
			c.diag("LDL1501", "invalid_relation_endpoint_or_self_rule", c.sources[relation.Address], c.sources[relation.Address].Range, "to endpoint violates relation type restrictions", relation.Address, "")
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
				c.validateBound(relationType.Address, entity, len(toByFrom[entity.Address]), relationType.Cardinality.ToPerFrom, "to_per_from")
			}
			if endpointAllows(relationType.To, entity) {
				c.validateBound(relationType.Address, entity, len(fromByTo[entity.Address]), relationType.Cardinality.FromPerTo, "from_per_to")
			}
		}
	}
}

func (c *compiler) validateBound(relationTypeAddress string, entity Entity, count int, bound definition.CardinalityBound, direction string) {
	violates := count < bound.Min || bound.Max == definition.CardinalityMaximumOne && count > 1
	if !violates {
		return
	}
	src := c.sources[entity.Address]
	c.diag("LDL1503", "relation_cardinality_violation", src, src.Range, direction+" cardinality violation for "+relationTypeAddress, entity.Address, "")
}
