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
	"sync"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

var fullAuthoringCapabilities = accesscore.FullAuthoringCapabilities()

type localAuthority struct {
	clock            port.Clock
	actor            accessprotocol.ActorRef
	access           accesscore.Evaluator
	random           io.Reader
	mu               sync.RWMutex
	delegationFence  sync.RWMutex
	scopes           map[runtimeprotocol.DocumentID]runtimeprotocol.RuntimeScope
	issued           map[runtimeprotocol.DocumentID]protocolcommon.Rfc3339Time
	delegations      *accesscore.DelegationStore
	agentPermissions map[protocolcommon.Digest]accesscore.AgentPermissions
}

// AcquireAuthoringPublication linearizes delegated publication against revoke
// and expiry checks. A successful release function must be held through the
// authoritative storage publication.
func (a *localAuthority) AcquireAuthoringPublication(ctx context.Context, scope runtimeprotocol.RuntimeScope) (func(), error) {
	id, _ := ctx.Value(delegationContextKey{}).(string)
	if id == "" {
		return func() {}, nil
	}
	a.delegationFence.RLock()
	record, err := a.delegationStore().Resolve(id, a.clock.Now())
	if err != nil || record.DocumentID != string(scope.DocumentID) || record.LocalScopeID != scope.LocalScopeID {
		a.delegationFence.RUnlock()
		return nil, accesscore.ErrGrantStale
	}
	return a.delegationFence.RUnlock, nil
}

func (a *localAuthority) lockDelegationMutation() func() {
	a.delegationFence.Lock()
	return a.delegationFence.Unlock
}

type delegationContextKey struct{}

func withDelegation(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, delegationContextKey{}, id)
}

func newLocalAuthority(clock port.Clock, random io.Reader) *localAuthority {
	return newLocalAuthorityForActor(clock, random, accessprotocol.ActorRef{ActorID: "local-owner", Kind: "user"})
}

func newLocalAuthorityForActor(clock port.Clock, random io.Reader, actor accessprotocol.ActorRef) *localAuthority {
	return newLocalAuthorityWithDelegations(clock, random, actor, accesscore.NewDelegationStore())
}

func newLocalAuthorityWithDelegations(clock port.Clock, random io.Reader, actor accessprotocol.ActorRef, delegations *accesscore.DelegationStore) *localAuthority {
	if random == nil {
		random = rand.Reader
	}
	return &localAuthority{clock: clock, actor: actor, access: accesscore.Evaluator{Clock: clock}, random: random, scopes: map[runtimeprotocol.DocumentID]runtimeprotocol.RuntimeScope{}, issued: map[runtimeprotocol.DocumentID]protocolcommon.Rfc3339Time{}, delegations: delegations, agentPermissions: map[protocolcommon.Digest]accesscore.AgentPermissions{}}
}

func (a *localAuthority) delegationStore() *accesscore.DelegationStore {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.delegations
}

func (a *localAuthority) replaceDelegationStore(store *accesscore.DelegationStore) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.delegations = store
	a.agentPermissions = map[protocolcommon.Digest]accesscore.AgentPermissions{}
}

func (a *localAuthority) add(documentID runtimeprotocol.DocumentID) runtimeprotocol.RuntimeScope {
	a.mu.Lock()
	defer a.mu.Unlock()
	if existing, ok := a.scopes[documentID]; ok {
		return existing
	}
	// Preserve the pre-Access local-owner scope identity so existing embedded
	// and headless stores remain readable. A Desktop/host-injected platform
	// Actor gets an actor-bound fingerprint from its first use.
	fingerprint := digestJSON(struct {
		Document runtimeprotocol.DocumentID `json:"document"`
	}{documentID})
	if a.actor != (accessprotocol.ActorRef{ActorID: "local-owner", Kind: "user"}) {
		fingerprint = digestJSON(struct {
			Document runtimeprotocol.DocumentID `json:"document"`
			Actor    accessprotocol.ActorRef    `json:"actor"`
		}{documentID, a.actor})
	}
	scope := runtimeprotocol.RuntimeScope{DocumentID: documentID, LocalScopeID: "local-owner", AccessFingerprint: fingerprint}
	a.scopes[documentID] = scope
	a.issued[documentID] = protocolcommon.Rfc3339Time(a.clock.Now().UTC().Format(time.RFC3339Nano))
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

func (a *localAuthority) ResolveGrant(ctx context.Context, scope runtimeprotocol.RuntimeScope) (accessprotocol.AuthoringGrantSnapshot, accessprotocol.AuthoringGrantSummary, error) {
	resolved, err := a.ResolveScope(context.Background(), scope.DocumentID)
	if err != nil || resolved != scope {
		return accessprotocol.AuthoringGrantSnapshot{}, accessprotocol.AuthoringGrantSummary{}, port.ErrConflict
	}
	a.mu.RLock()
	issued := a.issued[scope.DocumentID]
	a.mu.RUnlock()
	grant := accessprotocol.AuthoringGrantSnapshot{AccessFingerprint: scope.AccessFingerprint, ActorRef: a.actor, GrantedCapabilities: append([]semantic.AuthoringCapability(nil), fullAuthoringCapabilities...), HostDocumentID: string(scope.DocumentID), IssuedAt: issued, LocalScopeID: scope.LocalScopeID, MembershipVersion: "1", PolicyRefs: []accessprotocol.PolicyRef{}}
	summary := accessprotocol.AuthoringGrantSummary{AccessFingerprint: scope.AccessFingerprint, ConstrainedCapabilities: []semantic.AuthoringCapability{}, GrantedCapabilities: append([]semantic.AuthoringCapability(nil), fullAuthoringCapabilities...), PolicyEtag: digestJSON(struct {
		Scope runtimeprotocol.RuntimeScope `json:"scope"`
	}{scope})}
	if id, _ := ctx.Value(delegationContextKey{}).(string); id != "" {
		agent, permissions, err := a.delegationStore().Grant(grant, id, a.clock.Now())
		if err != nil {
			return accessprotocol.AuthoringGrantSnapshot{}, accessprotocol.AuthoringGrantSummary{}, err
		}
		a.mu.Lock()
		a.agentPermissions[*agent.AgentDelegationDigest] = permissions
		a.mu.Unlock()
		grant = agent
		summary.AccessFingerprint, summary.GrantedCapabilities, summary.ExpiresAt = agent.AccessFingerprint, append([]semantic.AuthoringCapability(nil), agent.GrantedCapabilities...), agent.ExpiresAt
	}
	return grant, summary, nil
}

func (a *localAuthority) Evaluate(ctx context.Context, input accessprotocol.EvaluateAuthoringInput) (accessprotocol.AuthoringDecision, error) {
	if input.GrantSnapshot.ActorRef.Kind == "agent" && input.GrantSnapshot.AgentDelegationDigest != nil {
		a.mu.RLock()
		permissions, ok := a.agentPermissions[*input.GrantSnapshot.AgentDelegationDigest]
		a.mu.RUnlock()
		if !ok {
			return accessprotocol.AuthoringDecision{}, accesscore.ErrGrantStale
		}
		return a.access.EvaluateWithContext(ctx, input, accesscore.EvaluationContext{CurrentAccessFingerprint: input.GrantSnapshot.AccessFingerprint, AgentPermissions: &permissions})
	}
	return a.access.Evaluate(ctx, input)
}

func (a *localAuthority) AuthorizeRead(ctx context.Context, scope runtimeprotocol.RuntimeScope, surface accesscore.ReadSurface) error {
	resolved, err := a.ResolveScope(context.Background(), scope.DocumentID)
	if err != nil || resolved != scope {
		return port.ErrConflict
	}
	policy := accesscore.ProjectionPolicy{Read: true, Export: true}
	if id, _ := ctx.Value(delegationContextKey{}).(string); id != "" {
		record, err := a.delegationStore().Resolve(id, a.clock.Now())
		if err != nil || record.DocumentID != string(scope.DocumentID) || record.LocalScopeID != scope.LocalScopeID {
			return accesscore.ErrGrantStale
		}
		policy.Read, policy.Export = record.Permissions.Read, record.Permissions.Export
	}
	return accesscore.AuthorizeReadSurface(surface, policy)
}

func (a *localAuthority) EvaluateStateQuery(ctx context.Context, input port.StateQueryAuthorizationInput) (port.StateQueryAuthorizationDecision, error) {
	if err := a.AuthorizeRead(ctx, input.Scope, accesscore.SurfaceQuery); err != nil {
		return port.StateQueryAuthorizationDecision{}, err
	}
	resolved, err := a.ResolveScope(context.Background(), input.Scope.DocumentID)
	if err != nil || resolved != input.Scope {
		return port.StateQueryAuthorizationDecision{}, port.ErrConflict
	}
	return port.StateQueryAuthorizationDecision{
		AccessFingerprint:      input.Scope.AccessFingerprint,
		DecisionDigest:         digestJSON(input),
		InaccessibleFieldPaths: []semantic.StateFieldPath{},
		RedactedFieldPaths:     map[semantic.StableAddress][]semantic.StateFieldPath{},
	}, nil
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

func digestJSON(value any) protocolcommon.Digest {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return protocolcommon.Digest("sha256:" + hex.EncodeToString(sum[:]))
}
