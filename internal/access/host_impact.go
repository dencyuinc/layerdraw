// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package access

import (
	"fmt"
	"sort"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

// HostOperationImpact derives the authorization fact from a closed owner
// protocol operation. Callers cannot supply a capability claim.
func HostOperationImpact(kind accessprotocol.HostOperationKind, action string, scope accessprotocol.HostResourceScope, refs []string) (accessprotocol.HostOperationImpact, error) {
	capability := semantic.AuthoringCapability("")
	switch kind {
	case accessprotocol.HostOperationKindAssetDelete, accessprotocol.HostOperationKindAssetPersist, accessprotocol.HostOperationKindAssetStage:
		capability = semantic.AuthoringCapabilityAssetWrite
	case accessprotocol.HostOperationKindPackageTransaction:
		capability = semantic.AuthoringCapabilityPackageManage
	case accessprotocol.HostOperationKindBackendConfigure, accessprotocol.HostOperationKindProjectConfigure:
		capability = semantic.AuthoringCapabilityProjectConfigure
	default:
		return accessprotocol.HostOperationImpact{}, fmt.Errorf("access: unknown host operation")
	}
	refs = append([]string(nil), refs...)
	sort.Strings(refs)
	impact := accessprotocol.HostOperationImpact{Action: action, OperationKind: kind, RequiredAuthoringCapabilities: []semantic.AuthoringCapability{capability}, ResourceRefs: refs, ResourceScope: scope}
	impact.ImpactDigest = digestJSON(impact)
	return impact, nil
}
