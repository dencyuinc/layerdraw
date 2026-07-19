// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopcontract

import (
	"reflect"
	"sort"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
)

// ValidateLocalOwnerGrant binds the Desktop default to the established Access
// grant contract. It does not create an Organization/Workspace membership.
func ValidateLocalOwnerGrant(request LocalOwnerGrantRequest, grant accessprotocol.AuthoringGrantSnapshot) bool {
	if request.Actor.Kind != "user" || request.Actor.ActorID == "" || grant.ActorRef != request.Actor ||
		grant.HostDocumentID != request.Scope.DocumentID || grant.LocalScopeID != request.Scope.LocalScopeID ||
		request.Scope.OrganizationScopeID != nil || grant.OrganizationScopeID != nil ||
		grant.AgentDelegationDigest != nil || grant.IssuedAt != request.IssuedAt || grant.MembershipVersion != protocolcommon.CanonicalUint64("1") {
		return false
	}
	if _, err := accessprotocol.EncodeAuthoringGrantSnapshot(grant); err != nil {
		return false
	}
	return reflect.DeepEqual(canonicalAuthoringCapabilities(grant.GrantedCapabilities), canonicalAuthoringCapabilities(accesscore.FullAuthoringCapabilities()))
}

func canonicalAuthoringCapabilities(value []semantic.AuthoringCapability) []semantic.AuthoringCapability {
	copy := append([]semantic.AuthoringCapability(nil), value...)
	sort.Slice(copy, func(i, j int) bool { return copy[i] < copy[j] })
	return copy
}

// ValidateDelegationRequest delegates validation to the established Access
// store contract and additionally rejects a pre-issued generation or a grant
// with no usable action permission.
func ValidateDelegationRequest(parent accessprotocol.AuthoringGrantSnapshot, requested accesscore.Delegation) bool {
	if requested.Generation != 0 || (!requested.Permissions.Read && !requested.Permissions.Export && !requested.Permissions.Propose && !requested.Permissions.Apply) {
		return false
	}
	issued, err := accesscore.NewDelegationStore().Delegate(parent, requested)
	return err == nil && issued.Generation == 1 && issued.DocumentID == parent.HostDocumentID && issued.LocalScopeID == parent.LocalScopeID
}

// ValidateDelegationFence ensures revocation/expiry publication checks cannot
// be redirected to another document, local scope, or generation.
func ValidateDelegationFence(fence DelegationFence, delegation accesscore.Delegation) bool {
	return fence.DelegationID != "" && fence.DelegationID == delegation.ID &&
		fence.DocumentID == delegation.DocumentID && fence.LocalScopeID == delegation.LocalScopeID &&
		fence.Generation == protocolcommon.CanonicalUint64(formatGeneration(delegation.Generation)) &&
		delegation.Agent.Kind == "agent" && delegation.Generation > 0 && delegation.ExpiresAt.After(delegation.IssuedAt)
}

func formatGeneration(value uint64) string {
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[index:])
}
