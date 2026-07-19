// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package localdocument

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

var fullAuthoringCapabilities = []semantic.AuthoringCapability{
	semantic.AuthoringCapabilityAssetWrite,
	semantic.AuthoringCapabilityGraphWrite,
	semantic.AuthoringCapabilityPackageManage,
	semantic.AuthoringCapabilityProjectConfigure,
	semantic.AuthoringCapabilityQueryWrite,
	semantic.AuthoringCapabilityReferenceWrite,
	semantic.AuthoringCapabilitySchemaWrite,
	semantic.AuthoringCapabilitySourceMaintain,
	semantic.AuthoringCapabilityViewWrite,
}

type localAuthority struct {
	clock  port.Clock
	random io.Reader
	mu     sync.RWMutex
	scopes map[runtimeprotocol.DocumentID]runtimeprotocol.RuntimeScope
}

func newLocalAuthority(clock port.Clock, random io.Reader) *localAuthority {
	if random == nil {
		random = rand.Reader
	}
	return &localAuthority{clock: clock, random: random, scopes: map[runtimeprotocol.DocumentID]runtimeprotocol.RuntimeScope{}}
}

func (a *localAuthority) add(documentID runtimeprotocol.DocumentID) runtimeprotocol.RuntimeScope {
	a.mu.Lock()
	defer a.mu.Unlock()
	if existing, ok := a.scopes[documentID]; ok {
		return existing
	}
	fingerprint := digestJSON(struct {
		Document runtimeprotocol.DocumentID `json:"document"`
	}{documentID})
	scope := runtimeprotocol.RuntimeScope{DocumentID: documentID, LocalScopeID: "local-owner", AccessFingerprint: fingerprint}
	a.scopes[documentID] = scope
	return scope
}

func (a *localAuthority) ResolveScope(_ context.Context, documentID runtimeprotocol.DocumentID) (runtimeprotocol.RuntimeScope, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	scope, ok := a.scopes[documentID]
	if !ok {
		return runtimeprotocol.RuntimeScope{}, port.ErrNotFound
	}
	return scope, nil
}

func (a *localAuthority) ResolveGrant(_ context.Context, scope runtimeprotocol.RuntimeScope) (accessprotocol.AuthoringGrantSnapshot, accessprotocol.AuthoringGrantSummary, error) {
	resolved, err := a.ResolveScope(context.Background(), scope.DocumentID)
	if err != nil || resolved != scope {
		return accessprotocol.AuthoringGrantSnapshot{}, accessprotocol.AuthoringGrantSummary{}, port.ErrConflict
	}
	now := protocolcommon.Rfc3339Time(a.clock.Now().UTC().Format(time.RFC3339Nano))
	grant := accessprotocol.AuthoringGrantSnapshot{AccessFingerprint: scope.AccessFingerprint, ActorRef: accessprotocol.ActorRef{ActorID: "local-owner", Kind: "user"}, GrantedCapabilities: append([]semantic.AuthoringCapability(nil), fullAuthoringCapabilities...), HostDocumentID: string(scope.DocumentID), IssuedAt: now, LocalScopeID: scope.LocalScopeID, MembershipVersion: "1", PolicyRefs: []accessprotocol.PolicyRef{}}
	summary := accessprotocol.AuthoringGrantSummary{AccessFingerprint: scope.AccessFingerprint, ConstrainedCapabilities: []semantic.AuthoringCapability{}, GrantedCapabilities: append([]semantic.AuthoringCapability(nil), fullAuthoringCapabilities...), PolicyEtag: digestJSON(struct {
		Scope runtimeprotocol.RuntimeScope `json:"scope"`
	}{scope})}
	return grant, summary, nil
}

func (a *localAuthority) Evaluate(_ context.Context, input accessprotocol.EvaluateAuthoringInput) (accessprotocol.AuthoringDecision, error) {
	required := []semantic.AuthoringCapability{}
	if input.AuthoringImpact != nil {
		required = append(required, input.AuthoringImpact.RequiredCapabilities...)
	}
	hostDigests := make([]protocolcommon.Digest, len(input.HostOperationImpacts))
	for index, impact := range input.HostOperationImpacts {
		required = append(required, impact.RequiredAuthoringCapabilities...)
		hostDigests[index] = impact.ImpactDigest
	}
	sort.Slice(required, func(i, j int) bool { return required[i] < required[j] })
	required = uniqueCapabilities(required)
	granted := map[semantic.AuthoringCapability]bool{}
	for _, capability := range input.GrantSnapshot.GrantedCapabilities {
		granted[capability] = true
	}
	missing := []semantic.AuthoringCapability{}
	for _, capability := range required {
		if !granted[capability] {
			missing = append(missing, capability)
		}
	}
	evaluationDigest := digestJSON(input)
	outcome := accessprotocol.AuthoringDecisionOutcomeAllow
	if len(missing) != 0 {
		outcome = accessprotocol.AuthoringDecisionOutcomeDeny
	}
	projection := struct {
		Evaluation protocolcommon.Digest                   `json:"evaluation"`
		Outcome    accessprotocol.AuthoringDecisionOutcome `json:"outcome"`
		Required   []semantic.AuthoringCapability          `json:"required"`
	}{evaluationDigest, outcome, required}
	decision := accessprotocol.AuthoringDecision{AccessFingerprint: input.GrantSnapshot.AccessFingerprint, ApprovalRuleRefs: []string{}, ConstraintViolations: []accessprotocol.ConstraintViolation{}, DecisionDigest: digestJSON(projection), Diagnostics: []protocolcommon.ProtocolDiagnostic{}, EvaluationDigest: evaluationDigest, HostOperationImpactDigests: hostDigests, MissingCapabilities: missing, Outcome: outcome, RequiredCapabilities: required}
	if input.AuthoringImpact != nil {
		value := input.AuthoringImpact.ImpactDigest
		decision.AuthoringImpactDigest = &value
	}
	return decision, nil
}

func (a *localAuthority) Now() time.Time { return a.clock.Now() }

func (a *localAuthority) NewID(_ context.Context, kind port.IdentityKind) (string, error) {
	buffer := make([]byte, 16)
	if _, err := io.ReadFull(a.random, buffer); err != nil {
		return "", err
	}
	prefix := string(kind)
	if prefix == "" {
		prefix = "identity"
	}
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(buffer)), nil
}

func uniqueCapabilities(input []semantic.AuthoringCapability) []semantic.AuthoringCapability {
	result := input[:0]
	for _, value := range input {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func digestJSON(value any) protocolcommon.Digest {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return protocolcommon.Digest("sha256:" + hex.EncodeToString(sum[:]))
}
