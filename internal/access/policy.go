// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package access

import (
	"fmt"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

type PolicyEffect string

const (
	PolicyAllow   PolicyEffect = "allow"
	PolicyDeny    PolicyEffect = "deny"
	PolicyInherit PolicyEffect = "inherit"
)

type CapabilityRule struct {
	Capability semantic.AuthoringCapability
	Effect     PolicyEffect
}

// PolicySnapshot is an immutable, host-resolved AuthoringPolicy input. The
// portable document never carries it. Source identifies the host hierarchy
// layer for audit only; it does not alter intersection semantics.
type PolicySnapshot struct {
	Ref         accessprotocol.PolicyRef
	DisplayName string
	Source      string
	Rules       []CapabilityRule
	Constraints GraphConstraints
}

// ResolvePolicies intersects role/owner capabilities with every policy. An
// allow can retain a base capability but cannot add one, and any deny wins.
// Graph dimensions are also intersected, preserving nil as unrestricted and
// an explicit empty set as deny-all.
func ResolvePolicies(base []semantic.AuthoringCapability, policies []PolicySnapshot) ([]semantic.AuthoringCapability, GraphConstraints, error) {
	effective := capabilitySet(canonicalCapabilities(base))
	constraints := GraphConstraints{}
	for _, policy := range policies {
		if policy.Ref.PolicyID == "" || policy.Ref.PolicyDigest == "" || policy.Ref.PolicyVersion == "" {
			return nil, GraphConstraints{}, fmt.Errorf("access: invalid policy snapshot")
		}
		for _, rule := range policy.Rules {
			switch rule.Effect {
			case PolicyAllow, PolicyInherit:
				// A lower policy cannot expand its parent/base grant.
			case PolicyDeny:
				delete(effective, rule.Capability)
			default:
				return nil, GraphConstraints{}, fmt.Errorf("access: unknown policy effect")
			}
		}
		constraints.EntityTypes = intersectDimension(constraints.EntityTypes, policy.Constraints.EntityTypes)
		constraints.RelationTypes = intersectDimension(constraints.RelationTypes, policy.Constraints.RelationTypes)
		constraints.Layers = intersectDimension(constraints.Layers, policy.Constraints.Layers)
		constraints.Columns = intersectDimension(constraints.Columns, policy.Constraints.Columns)
		constraints.Actions = intersectDimension(constraints.Actions, policy.Constraints.Actions)
	}
	result := make([]semantic.AuthoringCapability, 0, len(effective))
	for capability := range effective {
		result = append(result, capability)
	}
	return canonicalCapabilities(result), constraints, nil
}

func intersectDimension(current, next map[string]bool) map[string]bool {
	if next == nil {
		return cloneSet(current)
	}
	if current == nil {
		return cloneSet(next)
	}
	result := map[string]bool{}
	for value, allowed := range current {
		if allowed && next[value] {
			result[value] = true
		}
	}
	return result
}

func cloneSet(input map[string]bool) map[string]bool {
	if input == nil {
		return nil
	}
	result := make(map[string]bool, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
