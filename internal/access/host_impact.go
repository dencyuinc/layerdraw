// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package access

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

// HostOperationImpact derives the authorization fact from a closed owner
// protocol operation. Callers cannot supply a capability claim.
func HostOperationImpact(kind accessprotocol.HostOperationKind, action string, scope accessprotocol.HostResourceScope, refs []string) (accessprotocol.HostOperationImpact, error) {
	capability := semantic.AuthoringCapability("")
	validAction := false
	switch kind {
	case accessprotocol.HostOperationKindAssetDelete:
		capability = semantic.AuthoringCapabilityAssetWrite
		validAction = action == "delete"
	case accessprotocol.HostOperationKindAssetPersist:
		capability = semantic.AuthoringCapabilityAssetWrite
		validAction = action == "create" || action == "update"
	case accessprotocol.HostOperationKindAssetStage:
		capability = semantic.AuthoringCapabilityAssetWrite
		validAction = action == "stage"
	case accessprotocol.HostOperationKindPackageTransaction:
		capability = semantic.AuthoringCapabilityPackageManage
		validAction = action == "create" || action == "update" || action == "delete"
	case accessprotocol.HostOperationKindBackendConfigure, accessprotocol.HostOperationKindProjectConfigure:
		capability = semantic.AuthoringCapabilityProjectConfigure
		validAction = action == "update"
	default:
		return accessprotocol.HostOperationImpact{}, fmt.Errorf("access: unknown host operation")
	}
	if !validAction || scope.DocumentID == "" || scope.LocalScopeID == "" || len(refs) == 0 {
		return accessprotocol.HostOperationImpact{}, fmt.Errorf("access: invalid host operation descriptor")
	}
	refs = append([]string(nil), refs...)
	sort.Strings(refs)
	for index, ref := range refs {
		if ref == "" || (index > 0 && refs[index-1] == ref) {
			return accessprotocol.HostOperationImpact{}, fmt.Errorf("access: invalid host operation resource")
		}
	}
	impact := accessprotocol.HostOperationImpact{Action: action, OperationKind: kind, RequiredAuthoringCapabilities: []semantic.AuthoringCapability{capability}, ResourceRefs: refs, ResourceScope: scope}
	impact.ImpactDigest = digestJSON(impact)
	return impact, nil
}

// ValidateHostOperationImpact proves that an impact is the exact canonical
// output of the closed owner descriptor, including its complete resource
// scope and digest. Callers must not validate only the digest string because
// that would allow the descriptor body to drift independently.
func ValidateHostOperationImpact(impact accessprotocol.HostOperationImpact) error {
	expected, err := HostOperationImpact(impact.OperationKind, impact.Action, impact.ResourceScope, impact.ResourceRefs)
	if err != nil {
		return err
	}
	actualCanonical, err := accessprotocol.EncodeHostOperationImpact(impact)
	if err != nil {
		return fmt.Errorf("access: invalid host operation wire value: %w", err)
	}
	expectedCanonical, err := accessprotocol.EncodeHostOperationImpact(expected)
	if err != nil {
		return fmt.Errorf("access: invalid derived host operation wire value: %w", err)
	}
	if !bytes.Equal(actualCanonical, expectedCanonical) {
		return fmt.Errorf("access: host operation impact does not match its closed descriptor digest")
	}
	return nil
}
