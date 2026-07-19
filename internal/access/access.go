// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package access owns trusted authoring decisions and local actor/delegation
// resolution. It deliberately consumes Engine-produced AuthoringImpact and
// owner-protocol HostOperationImpact values; it never parses LDL or infers a
// capability from a transport operation name.
package access

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

var (
	ErrInvalidDelegation = errors.New("access: invalid delegation")
	ErrGrantStale        = errors.New("access: grant is stale")
)

var fullAuthoringCapabilities = []semantic.AuthoringCapability{
	semantic.AuthoringCapabilityAssetWrite, semantic.AuthoringCapabilityGraphWrite,
	semantic.AuthoringCapabilityPackageManage, semantic.AuthoringCapabilityProjectConfigure,
	semantic.AuthoringCapabilityQueryWrite, semantic.AuthoringCapabilityReferenceWrite,
	semantic.AuthoringCapabilitySchemaWrite, semantic.AuthoringCapabilitySourceMaintain,
	semantic.AuthoringCapabilityViewWrite,
}

func FullAuthoringCapabilities() []semantic.AuthoringCapability {
	return append([]semantic.AuthoringCapability(nil), fullAuthoringCapabilities...)
}

type Clock interface{ Now() time.Time }

// LocalActorResolver is the host-owned OS identity boundary. Implementations
// return a stable, non-secret identifier (for example a platform user SID),
// never a credential or organization membership assertion.
type LocalActorResolver interface {
	ResolveLocalActor(context.Context) (accessprotocol.ActorRef, error)
}

type StaticLocalActorResolver struct{ ActorID string }

func (r StaticLocalActorResolver) ResolveLocalActor(context.Context) (accessprotocol.ActorRef, error) {
	if r.ActorID == "" {
		return accessprotocol.ActorRef{}, errors.New("access: local actor id is empty")
	}
	actor := accessprotocol.ActorRef{ActorID: r.ActorID, Kind: "user"}
	if _, err := accessprotocol.EncodeActorRef(actor); err != nil {
		return accessprotocol.ActorRef{}, fmt.Errorf("access: invalid local actor: %w", err)
	}
	return actor, nil
}

type AgentPermissions struct {
	Read, Export, Propose, Apply bool
}

func (p AgentPermissions) Allows(intent string) bool {
	switch intent {
	case "preview":
		return p.Read
	case "propose":
		return p.Propose
	case "apply", "publish":
		return p.Apply
	default:
		return false
	}
}

type Delegation struct {
	ID                    string
	ParentActor           accessprotocol.ActorRef
	Agent                 accessprotocol.ActorRef
	DocumentID            string
	LocalScopeID          string
	AuthoringCapabilities []semantic.AuthoringCapability
	Permissions           AgentPermissions
	IssuedAt              time.Time
	ExpiresAt             time.Time
	Generation            uint64
}

type DelegationStore struct {
	mu      sync.RWMutex
	records map[string]Delegation
	revoked map[string]uint64
}

func NewDelegationStore() *DelegationStore {
	return &DelegationStore{records: map[string]Delegation{}, revoked: map[string]uint64{}}
}

// Delegate rejects scope escalation instead of silently clipping it. This
// makes a malformed MCP/host request visible and prevents caller assumptions
// from diverging from the effective grant.
func (s *DelegationStore) Delegate(parent accessprotocol.AuthoringGrantSnapshot, requested Delegation) (Delegation, error) {
	if requested.ID == "" || requested.Agent.Kind != "agent" || requested.Agent.ActorID == "" || requested.ParentActor != parent.ActorRef || requested.DocumentID != parent.HostDocumentID || requested.LocalScopeID != parent.LocalScopeID || !requested.ExpiresAt.After(requested.IssuedAt) {
		return Delegation{}, ErrInvalidDelegation
	}
	if parent.ExpiresAt != nil {
		parentExpiry, err := time.Parse(time.RFC3339Nano, string(*parent.ExpiresAt))
		if err != nil || requested.ExpiresAt.After(parentExpiry) {
			return Delegation{}, ErrInvalidDelegation
		}
	}
	parentCaps := capabilitySet(parent.GrantedCapabilities)
	requested.AuthoringCapabilities = canonicalCapabilities(requested.AuthoringCapabilities)
	for _, capability := range requested.AuthoringCapabilities {
		if !parentCaps[capability] {
			return Delegation{}, ErrInvalidDelegation
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.records[requested.ID]; exists {
		return Delegation{}, ErrInvalidDelegation
	}
	requested.Generation = 1
	s.records[requested.ID] = cloneDelegation(requested)
	return cloneDelegation(requested), nil
}

func (s *DelegationStore) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[id]
	if !ok {
		return ErrGrantStale
	}
	s.revoked[id] = record.Generation
	return nil
}

func (s *DelegationStore) Resolve(id string, now time.Time) (Delegation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.records[id]
	if !ok || s.revoked[id] >= record.Generation || !now.Before(record.ExpiresAt) {
		return Delegation{}, ErrGrantStale
	}
	return cloneDelegation(record), nil
}

func (s *DelegationStore) Grant(parent accessprotocol.AuthoringGrantSnapshot, id string, now time.Time) (accessprotocol.AuthoringGrantSnapshot, AgentPermissions, error) {
	record, err := s.Resolve(id, now)
	if err != nil || record.ParentActor != parent.ActorRef || record.DocumentID != parent.HostDocumentID || record.LocalScopeID != parent.LocalScopeID {
		return accessprotocol.AuthoringGrantSnapshot{}, AgentPermissions{}, ErrGrantStale
	}
	parentCaps := capabilitySet(parent.GrantedCapabilities)
	for _, capability := range record.AuthoringCapabilities {
		if !parentCaps[capability] {
			return accessprotocol.AuthoringGrantSnapshot{}, AgentPermissions{}, ErrGrantStale
		}
	}
	digest := digestJSON(struct {
		ID           string                         `json:"id"`
		Generation   uint64                         `json:"generation"`
		Agent        accessprotocol.ActorRef        `json:"agent"`
		Capabilities []semantic.AuthoringCapability `json:"capabilities"`
		Permissions  AgentPermissions               `json:"permissions"`
		ExpiresAt    string                         `json:"expires_at"`
	}{record.ID, record.Generation, record.Agent, record.AuthoringCapabilities, record.Permissions, record.ExpiresAt.UTC().Format(time.RFC3339Nano)})
	expires := protocolcommon.Rfc3339Time(record.ExpiresAt.UTC().Format(time.RFC3339Nano))
	grant := parent
	grant.ActorRef = record.Agent
	grant.AgentDelegationDigest = &digest
	grant.GrantedCapabilities = append([]semantic.AuthoringCapability(nil), record.AuthoringCapabilities...)
	grant.IssuedAt = protocolcommon.Rfc3339Time(record.IssuedAt.UTC().Format(time.RFC3339Nano))
	grant.ExpiresAt = &expires
	grant.AccessFingerprint = Fingerprint(grant)
	return grant, record.Permissions, nil
}

func Fingerprint(grant accessprotocol.AuthoringGrantSnapshot) protocolcommon.Digest {
	// The fingerprint cannot include itself. Empty is used only in this private
	// canonical projection and is never emitted as a wire grant.
	grant.AccessFingerprint = ""
	return digestJSON(grant)
}

type GraphConstraints struct {
	EntityTypes, RelationTypes, Layers, Columns map[string]bool
	Actions                                     map[string]bool
}

type EvaluationContext struct {
	Constraints GraphConstraints
	// AgentPermissions is required for an agent Actor. It independently gates
	// preview/read, propose, and authoritative apply/publish entry points.
	AgentPermissions *AgentPermissions
	// CurrentAccessFingerprint is supplied by the trusted host immediately
	// before publication. A mismatch fails closed as a changed policy/grant.
	CurrentAccessFingerprint protocolcommon.Digest
}

type Evaluator struct{ Clock Clock }

func (e Evaluator) Evaluate(ctx context.Context, input accessprotocol.EvaluateAuthoringInput) (accessprotocol.AuthoringDecision, error) {
	return e.EvaluateWithContext(ctx, input, EvaluationContext{CurrentAccessFingerprint: input.GrantSnapshot.AccessFingerprint})
}

func (e Evaluator) EvaluateWithContext(_ context.Context, input accessprotocol.EvaluateAuthoringInput, current EvaluationContext) (accessprotocol.AuthoringDecision, error) {
	// The Runtime/host protocol boundary validates and canonicalizes the wire
	// value before invoking Access. Re-encoding here would make the behavior
	// owner depend on transport representation and would reject useful internal
	// table-test projections that carry only the facts under evaluation.
	now := time.Now()
	if e.Clock != nil {
		now = e.Clock.Now()
	}
	code := ""
	if current.CurrentAccessFingerprint != input.GrantSnapshot.AccessFingerprint {
		code = "authoring.policy_changed"
	}
	if input.GrantSnapshot.ExpiresAt != nil {
		expires, err := time.Parse(time.RFC3339Nano, string(*input.GrantSnapshot.ExpiresAt))
		if err != nil || !now.Before(expires) {
			code = "authoring.policy_changed"
		}
	}
	if input.GrantSnapshot.ActorRef.Kind == "agent" && (current.AgentPermissions == nil || !current.AgentPermissions.Allows(input.RequestIntent)) {
		code = "authoring.agent_scope_denied"
	}
	required := []semantic.AuthoringCapability{}
	if input.AuthoringImpact != nil {
		required = append(required, input.AuthoringImpact.RequiredCapabilities...)
	}
	hostDigests := make([]protocolcommon.Digest, len(input.HostOperationImpacts))
	for i, impact := range input.HostOperationImpacts {
		required = append(required, impact.RequiredAuthoringCapabilities...)
		hostDigests[i] = impact.ImpactDigest
	}
	required = canonicalCapabilities(required)
	granted := capabilitySet(input.GrantSnapshot.GrantedCapabilities)
	missing := []semantic.AuthoringCapability{}
	for _, capability := range required {
		if !granted[capability] {
			missing = append(missing, capability)
		}
	}
	violations := constraintViolations(input.AuthoringImpact, current.Constraints)
	if code == "" && len(missing) > 0 {
		code = "authoring.capability_denied"
	}
	if code == "" && len(violations) > 0 {
		code = "authoring.constraint_denied"
	}
	evaluation := struct {
		Authoring *protocolcommon.Digest  `json:"authoring_impact_digest,omitempty"`
		Host      []protocolcommon.Digest `json:"host_operation_impact_digests"`
		Access    protocolcommon.Digest   `json:"access_fingerprint"`
		Intent    string                  `json:"request_intent"`
	}{Host: hostDigests, Access: input.GrantSnapshot.AccessFingerprint, Intent: input.RequestIntent}
	if input.AuthoringImpact != nil {
		evaluation.Authoring = &input.AuthoringImpact.ImpactDigest
	}
	evaluationDigest := digestJSON(evaluation)
	outcome := accessprotocol.AuthoringDecisionOutcomeAllow
	diagnostics := []protocolcommon.ProtocolDiagnostic{}
	if code != "" {
		outcome = accessprotocol.AuthoringDecisionOutcomeDeny
		diagnostics = append(diagnostics, protocolcommon.ProtocolDiagnostic{Code: code, Message: "authoring request was denied by Access", Related: []protocolcommon.ProtocolDiagnosticRelated{}, Severity: protocolcommon.ProtocolDiagnosticSeverityError})
	}
	projection := struct {
		Evaluation protocolcommon.Digest                   `json:"evaluation"`
		Outcome    accessprotocol.AuthoringDecisionOutcome `json:"outcome"`
		Missing    []semantic.AuthoringCapability          `json:"missing"`
		Violations []accessprotocol.ConstraintViolation    `json:"violations"`
	}{evaluationDigest, outcome, missing, violations}
	decision := accessprotocol.AuthoringDecision{AccessFingerprint: input.GrantSnapshot.AccessFingerprint, ApprovalRuleRefs: []string{}, ConstraintViolations: violations, DecisionDigest: digestJSON(projection), Diagnostics: diagnostics, EvaluationDigest: evaluationDigest, HostOperationImpactDigests: hostDigests, MissingCapabilities: missing, Outcome: outcome, RequiredCapabilities: required}
	if input.AuthoringImpact != nil {
		digest := input.AuthoringImpact.ImpactDigest
		decision.AuthoringImpactDigest = &digest
	}
	return decision, nil
}

func constraintViolations(impact *semantic.AuthoringImpact, constraints GraphConstraints) []accessprotocol.ConstraintViolation {
	if impact == nil {
		return []accessprotocol.ConstraintViolation{}
	}
	result := []accessprotocol.ConstraintViolation{}
	for _, entry := range impact.Entries {
		if entry.GraphFacts == nil {
			continue
		}
		facts := entry.GraphFacts
		denied := !allAllowedEntityTypes(facts.EntityTypeAddresses, constraints.EntityTypes) || !allAllowedRelationTypes(facts.RelationTypeAddresses, constraints.RelationTypes) || !allAllowedLayers(facts.LayerAddresses, constraints.Layers) || !allAllowedColumns(facts.ColumnAddresses, constraints.Columns) || !allAllowedStrings(facts.ActionFlags, constraints.Actions)
		if denied {
			result = append(result, accessprotocol.ConstraintViolation{Action: string(entry.Action), Code: "authoring.constraint_denied", SubjectAddress: entry.SubjectAddress})
		}
	}
	return result
}

func allAllowedEntityTypes(v []semantic.EntityTypeAddress, p map[string]bool) bool {
	for _, x := range v {
		if p != nil && !p[string(x)] {
			return false
		}
	}
	return true
}
func allAllowedRelationTypes(v []semantic.RelationTypeAddress, p map[string]bool) bool {
	for _, x := range v {
		if p != nil && !p[string(x)] {
			return false
		}
	}
	return true
}
func allAllowedLayers(v []semantic.LayerAddress, p map[string]bool) bool {
	for _, x := range v {
		if p != nil && !p[string(x)] {
			return false
		}
	}
	return true
}
func allAllowedColumns(v []semantic.ColumnAddress, p map[string]bool) bool {
	for _, x := range v {
		if p != nil && !p[string(x)] {
			return false
		}
	}
	return true
}
func allAllowedStrings(v []string, p map[string]bool) bool {
	for _, x := range v {
		if p != nil && !p[x] {
			return false
		}
	}
	return true
}

func canonicalCapabilities(input []semantic.AuthoringCapability) []semantic.AuthoringCapability {
	result := append([]semantic.AuthoringCapability(nil), input...)
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	out := result[:0]
	for _, value := range result {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}
func capabilitySet(input []semantic.AuthoringCapability) map[semantic.AuthoringCapability]bool {
	result := map[semantic.AuthoringCapability]bool{}
	for _, value := range input {
		result[value] = true
	}
	return result
}

func cloneDelegation(input Delegation) Delegation {
	input.AuthoringCapabilities = append([]semantic.AuthoringCapability(nil), input.AuthoringCapabilities...)
	return input
}

func digestJSON(value any) protocolcommon.Digest {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return protocolcommon.Digest("sha256:" + hex.EncodeToString(sum[:]))
}
