// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package access

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func TestStableLocalOwnerAndFullAuthoringPreset(t *testing.T) {
	resolver := StaticLocalActorResolver{ActorID: "os-user-S-1-5-21"}
	first, err := resolver.ResolveLocalActor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolver.ResolveLocalActor(context.Background())
	if err != nil || first != second || first.Kind != "user" {
		t.Fatalf("restart actor = %+v %+v %v", first, second, err)
	}
	full := FullAuthoringCapabilities()
	if len(full) != 9 {
		t.Fatalf("full authoring capabilities = %v", full)
	}
	full[0] = semantic.AuthoringCapability("mutated")
	if FullAuthoringCapabilities()[0] == full[0] {
		t.Fatal("full authoring preset exposed mutable storage")
	}
	if _, err := (StaticLocalActorResolver{}).ResolveLocalActor(context.Background()); err == nil {
		t.Fatal("empty local actor accepted")
	}
	permissions := AgentPermissions{Read: true, Propose: true, Apply: true}
	for _, intent := range []string{"preview", "propose", "apply", "publish"} {
		if !permissions.Allows(intent) {
			t.Fatalf("intent %s denied", intent)
		}
	}
	if permissions.Allows("unknown") || (AgentPermissions{}).Allows("preview") {
		t.Fatal("unknown or unreadable intent allowed")
	}
}

func TestDelegatedAgentCannotEscalateAndRevocationFailsClosed(t *testing.T) {
	now := time.Date(2026, 7, 19, 9, 0, 0, 0, time.UTC)
	parent := ownerGrant(now, []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite})
	store := NewDelegationStore()
	base := Delegation{ID: "agent-1", ParentActor: parent.ActorRef, Agent: accessprotocol.ActorRef{ActorID: "agent-codex", Kind: "agent"}, DocumentID: parent.HostDocumentID, LocalScopeID: parent.LocalScopeID, Permissions: AgentPermissions{Read: true, Propose: true}, AuthoringCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite}, IssuedAt: now, ExpiresAt: now.Add(time.Hour)}
	escalated := base
	escalated.ID = "escalated"
	escalated.AuthoringCapabilities = append(escalated.AuthoringCapabilities, semantic.AuthoringCapabilitySchemaWrite)
	if _, err := store.Delegate(parent, escalated); !errors.Is(err, ErrInvalidDelegation) {
		t.Fatalf("scope escalation = %v", err)
	}
	record, err := store.Delegate(parent, base)
	if err != nil {
		t.Fatal(err)
	}
	record.AuthoringCapabilities[0] = semantic.AuthoringCapabilitySchemaWrite
	resolved, err := store.Resolve(record.ID, now)
	if err != nil || !reflect.DeepEqual(resolved.AuthoringCapabilities, []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite}) {
		t.Fatalf("delegation mutated through returned alias: %+v %v", resolved, err)
	}
	grant, permissions, err := store.Grant(parent, record.ID, now.Add(time.Minute))
	if err != nil || grant.ActorRef != record.Agent || grant.AgentDelegationDigest == nil || permissions.Apply || permissions.Export {
		t.Fatalf("agent grant = %+v %+v %v", grant, permissions, err)
	}
	again, _, err := store.Grant(parent, record.ID, now.Add(2*time.Minute))
	if err != nil || again.AccessFingerprint != grant.AccessFingerprint {
		t.Fatalf("stable delegated fingerprint = %s %s %v", grant.AccessFingerprint, again.AccessFingerprint, err)
	}
	if err := store.Revoke(record.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke("missing"); !errors.Is(err, ErrGrantStale) {
		t.Fatalf("missing revoke = %v", err)
	}
	if _, _, err := store.Grant(parent, record.ID, now.Add(2*time.Minute)); !errors.Is(err, ErrGrantStale) {
		t.Fatalf("revoked grant = %v", err)
	}
}

func TestDelegationRechecksChangedParentGrant(t *testing.T) {
	now := time.Date(2026, 7, 19, 9, 0, 0, 0, time.UTC)
	parent := ownerGrant(now, []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite})
	store := NewDelegationStore()
	record, err := store.Delegate(parent, Delegation{ID: "agent", ParentActor: parent.ActorRef, Agent: accessprotocol.ActorRef{ActorID: "agent", Kind: "agent"}, DocumentID: parent.HostDocumentID, LocalScopeID: parent.LocalScopeID, AuthoringCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite}, Permissions: AgentPermissions{Read: true}, IssuedAt: now, ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	parent.GrantedCapabilities = []semantic.AuthoringCapability{}
	parent.AccessFingerprint = Fingerprint(parent)
	if _, _, err := store.Grant(parent, record.ID, now); !errors.Is(err, ErrGrantStale) {
		t.Fatalf("changed parent grant = %v", err)
	}
}

func TestAgentIntentScopeIsIndependentFromAuthoringCapabilities(t *testing.T) {
	now := time.Date(2026, 7, 19, 9, 0, 0, 0, time.UTC)
	grant := ownerGrant(now, []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite})
	grant.ActorRef = accessprotocol.ActorRef{ActorID: "agent", Kind: "agent"}
	grant.AccessFingerprint = Fingerprint(grant)
	impact := semantic.AuthoringImpact{BaseDefinitionHash: testDigest("base"), Entries: []semantic.AuthoringImpactEntry{}, ImpactDigest: testDigest("impact"), RequiredCapabilities: []semantic.AuthoringCapability{}, ResultingDefinitionHash: testDigest("result"), SemanticDiffHash: testDigest("semantic"), SourceDiffHash: testDigest("source")}
	input := accessprotocol.EvaluateAuthoringInput{AuthoringImpact: &impact, GrantSnapshot: grant, HostOperationImpacts: []accessprotocol.HostOperationImpact{}, RequestIntent: "apply"}
	permissions := AgentPermissions{Read: true, Propose: true, Apply: false}
	decision, err := (Evaluator{Clock: fixedClock{now}}).EvaluateWithContext(context.Background(), input, EvaluationContext{CurrentAccessFingerprint: grant.AccessFingerprint, AgentPermissions: &permissions})
	if err != nil || decision.Outcome != accessprotocol.AuthoringDecisionOutcomeDeny || decision.Diagnostics[0].Code != "authoring.agent_scope_denied" {
		t.Fatalf("agent apply = %+v %v", decision, err)
	}
	input.RequestIntent = "propose"
	decision, err = (Evaluator{Clock: fixedClock{now}}).EvaluateWithContext(context.Background(), input, EvaluationContext{CurrentAccessFingerprint: grant.AccessFingerprint, AgentPermissions: &permissions})
	if err != nil || decision.Outcome != accessprotocol.AuthoringDecisionOutcomeAllow {
		t.Fatalf("agent propose = %+v %v", decision, err)
	}
}

func TestPolicySnapshotsOnlyRestrictAndConstraintsIntersect(t *testing.T) {
	ref := func(id string) accessprotocol.PolicyRef {
		return accessprotocol.PolicyRef{PolicyID: id, PolicyDigest: testDigest(id), PolicyVersion: "1"}
	}
	policies := []PolicySnapshot{
		{Ref: ref("host"), Source: "host_application", Rules: []CapabilityRule{{Capability: semantic.AuthoringCapabilitySchemaWrite, Effect: PolicyDeny}}, Constraints: GraphConstraints{EntityTypes: map[string]bool{"type:a": true, "type:b": true}}},
		{Ref: ref("project"), Source: "project", Rules: []CapabilityRule{{Capability: semantic.AuthoringCapabilityPackageManage, Effect: PolicyAllow}}, Constraints: GraphConstraints{EntityTypes: map[string]bool{"type:b": true, "type:c": true}}},
	}
	base := []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite, semantic.AuthoringCapabilitySchemaWrite}
	effective, constraints, err := ResolvePolicies(base, policies)
	if err != nil || !reflect.DeepEqual(effective, []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite}) || !reflect.DeepEqual(constraints.EntityTypes, map[string]bool{"type:b": true}) {
		t.Fatalf("policy result = %v %+v %v", effective, constraints, err)
	}
	if _, _, err := ResolvePolicies(base, []PolicySnapshot{{}}); err == nil {
		t.Fatal("invalid policy accepted")
	}
	bad := PolicySnapshot{Ref: ref("bad"), Rules: []CapabilityRule{{Capability: semantic.AuthoringCapabilityGraphWrite, Effect: "expand"}}}
	if _, _, err := ResolvePolicies(base, []PolicySnapshot{bad}); err == nil {
		t.Fatal("unknown policy effect accepted")
	}
}

func TestDelegationExpiryAndParentExpiryAreEnforced(t *testing.T) {
	now := time.Date(2026, 7, 19, 9, 0, 0, 0, time.UTC)
	parent := ownerGrant(now, []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite})
	parentExpiry := protocolcommon.Rfc3339Time(now.Add(30 * time.Minute).Format(time.RFC3339Nano))
	parent.ExpiresAt = &parentExpiry
	request := Delegation{ID: "agent", ParentActor: parent.ActorRef, Agent: accessprotocol.ActorRef{ActorID: "agent", Kind: "agent"}, DocumentID: parent.HostDocumentID, LocalScopeID: parent.LocalScopeID, AuthoringCapabilities: []semantic.AuthoringCapability{}, Permissions: AgentPermissions{Read: true}, IssuedAt: now, ExpiresAt: now.Add(time.Hour)}
	store := NewDelegationStore()
	if _, err := store.Delegate(parent, request); !errors.Is(err, ErrInvalidDelegation) {
		t.Fatalf("delegation outlived parent: %v", err)
	}
	request.ExpiresAt = now.Add(10 * time.Minute)
	if _, err := store.Delegate(parent, request); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Resolve(request.ID, request.ExpiresAt); !errors.Is(err, ErrGrantStale) {
		t.Fatalf("expired delegation = %v", err)
	}
}

func TestEvaluatorBindsImpactsAndRejectsMissingOrStaleGrant(t *testing.T) {
	now := time.Date(2026, 7, 19, 9, 0, 0, 0, time.UTC)
	grant := ownerGrant(now, []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite})
	impact := semantic.AuthoringImpact{BaseDefinitionHash: testDigest("base"), Entries: []semantic.AuthoringImpactEntry{}, ImpactDigest: testDigest("impact"), RequiredCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite, semantic.AuthoringCapabilitySchemaWrite}, ResultingDefinitionHash: testDigest("result"), SemanticDiffHash: testDigest("semantic"), SourceDiffHash: testDigest("source")}
	host := accessprotocol.HostOperationImpact{Action: "stage", ImpactDigest: testDigest("host"), OperationKind: accessprotocol.HostOperationKindAssetStage, RequiredAuthoringCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilityAssetWrite}, ResourceRefs: []string{"asset:a"}, ResourceScope: accessprotocol.HostResourceScope{DocumentID: grant.HostDocumentID, LocalScopeID: grant.LocalScopeID}}
	input := accessprotocol.EvaluateAuthoringInput{AuthoringImpact: &impact, GrantSnapshot: grant, HostOperationImpacts: []accessprotocol.HostOperationImpact{host}, RequestIntent: "apply"}
	decision, err := (Evaluator{Clock: fixedClock{now}}).Evaluate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	wantMissing := []semantic.AuthoringCapability{semantic.AuthoringCapabilityAssetWrite, semantic.AuthoringCapabilitySchemaWrite}
	if decision.Outcome != accessprotocol.AuthoringDecisionOutcomeDeny || !reflect.DeepEqual(decision.MissingCapabilities, wantMissing) || decision.AuthoringImpactDigest == nil || *decision.AuthoringImpactDigest != impact.ImpactDigest || !reflect.DeepEqual(decision.HostOperationImpactDigests, []protocolcommon.Digest{host.ImpactDigest}) {
		t.Fatalf("decision = %+v", decision)
	}
	stale, err := (Evaluator{Clock: fixedClock{now}}).EvaluateWithContext(context.Background(), input, EvaluationContext{CurrentAccessFingerprint: testDigest("changed")})
	if err != nil || stale.Outcome != accessprotocol.AuthoringDecisionOutcomeDeny || len(stale.Diagnostics) != 1 || stale.Diagnostics[0].Code != "authoring.policy_changed" {
		t.Fatalf("stale = %+v %v", stale, err)
	}
}

func TestEvaluatorAllowsEmptyImpactAndRejectsExpiredAgentGrant(t *testing.T) {
	now := time.Date(2026, 7, 19, 9, 0, 0, 0, time.UTC)
	grant := ownerGrant(now, FullAuthoringCapabilities())
	input := accessprotocol.EvaluateAuthoringInput{GrantSnapshot: grant, HostOperationImpacts: []accessprotocol.HostOperationImpact{}, RequestIntent: "preview"}
	decision, err := (Evaluator{Clock: fixedClock{now}}).Evaluate(context.Background(), input)
	if err != nil || decision.Outcome != accessprotocol.AuthoringDecisionOutcomeAllow || decision.AuthoringImpactDigest != nil {
		t.Fatalf("empty impact = %+v %v", decision, err)
	}
	expires := protocolcommon.Rfc3339Time(now.Format(time.RFC3339Nano))
	grant.ExpiresAt = &expires
	input.GrantSnapshot = grant
	decision, err = (Evaluator{Clock: fixedClock{now}}).Evaluate(context.Background(), input)
	if err != nil || decision.Outcome != accessprotocol.AuthoringDecisionOutcomeDeny || decision.Diagnostics[0].Code != "authoring.policy_changed" {
		t.Fatalf("expired = %+v %v", decision, err)
	}
}

func TestGraphConstraintsConsumeEngineFactsWithoutLDLParsing(t *testing.T) {
	address := semantic.StableAddress("project:p/entity:e")
	impact := &semantic.AuthoringImpact{Entries: []semantic.AuthoringImpactEntry{{Action: semantic.AuthoringActionUpdate, Capability: semantic.AuthoringCapabilityGraphWrite, SubjectAddress: &address, GraphFacts: &semantic.GraphAuthoringFacts{ActionFlags: []string{"update"}, ColumnAddresses: []semantic.ColumnAddress{}, EndpointEntityAddresses: []semantic.EntityAddress{}, EntityTypeAddresses: []semantic.EntityTypeAddress{"project:p/entity-type:allowed"}, LayerAddresses: []semantic.LayerAddress{}, RelationTypeAddresses: []semantic.RelationTypeAddress{}}}}}
	allowed := GraphConstraints{EntityTypes: map[string]bool{"project:p/entity-type:allowed": true}, Actions: map[string]bool{"update": true}}
	if got := constraintViolations(impact, allowed); len(got) != 0 {
		t.Fatalf("allowed facts = %+v", got)
	}
	allowed.EntityTypes = map[string]bool{}
	if got := constraintViolations(impact, allowed); len(got) != 1 || got[0].SubjectAddress == nil {
		t.Fatalf("denied facts = %+v", got)
	}
	impact.Entries[0].GraphFacts = &semantic.GraphAuthoringFacts{ActionFlags: []string{"delete"}, ColumnAddresses: []semantic.ColumnAddress{"project:p/entity-type:t/column:c"}, EndpointEntityAddresses: []semantic.EntityAddress{}, EntityTypeAddresses: []semantic.EntityTypeAddress{}, LayerAddresses: []semantic.LayerAddress{"project:p/layer:l"}, RelationTypeAddresses: []semantic.RelationTypeAddress{"project:p/relation-type:r"}}
	allDenied := GraphConstraints{RelationTypes: map[string]bool{}, Layers: map[string]bool{}, Columns: map[string]bool{}, Actions: map[string]bool{}}
	if got := constraintViolations(impact, allDenied); len(got) != 1 {
		t.Fatalf("multi-dimensional denial = %+v", got)
	}
	impact.Entries[0].GraphFacts = nil
	if got := constraintViolations(impact, allDenied); len(got) != 0 {
		t.Fatalf("non-graph entry = %+v", got)
	}
	if got := constraintViolations(nil, allDenied); len(got) != 0 {
		t.Fatalf("nil impact = %+v", got)
	}
	if allAllowedLayers([]semantic.LayerAddress{"layer"}, map[string]bool{}) || !allAllowedLayers([]semantic.LayerAddress{"layer"}, map[string]bool{"layer": true}) {
		t.Fatal("layer constraint mismatch")
	}
	if allAllowedColumns([]semantic.ColumnAddress{"column"}, map[string]bool{}) || !allAllowedColumns([]semantic.ColumnAddress{"column"}, map[string]bool{"column": true}) {
		t.Fatal("column constraint mismatch")
	}
	if allAllowedStrings([]string{"delete"}, map[string]bool{}) || !allAllowedStrings([]string{"delete"}, map[string]bool{"delete": true}) {
		t.Fatal("action constraint mismatch")
	}
}

func TestProjectionRedactsBeforeEveryTrustedBoundary(t *testing.T) {
	records := []Record{{SubjectAddress: "entity:visible", Fields: map[string]any{"public": "ok", "secret": "do-not-leak"}}, {SubjectAddress: "entity:hidden", Fields: map[string]any{"public": "hidden"}}}
	policy := ProjectionPolicy{Read: true, Export: true, AllowedSubjects: map[string]bool{"entity:visible": true}, AllowedFields: map[string]bool{"public": true}}
	for _, surface := range []ReadSurface{SurfaceSearch, SurfaceQuery, SurfaceReview, SurfaceExport, SurfaceMCP} {
		projected, err := Project(surface, policy, records)
		if err != nil || len(projected) != 1 || projected[0].Fields["public"] != "ok" {
			t.Fatalf("%s projection = %+v %v", surface, projected, err)
		}
		if _, leaked := projected[0].Fields["secret"]; leaked {
			t.Fatalf("%s leaked redacted field", surface)
		}
	}
	policy.Export = false
	if _, err := Project(SurfaceExport, policy, records); !errors.Is(err, ErrReadDenied) {
		t.Fatalf("export scope = %v", err)
	}
}

func ownerGrant(now time.Time, caps []semantic.AuthoringCapability) accessprotocol.AuthoringGrantSnapshot {
	grant := accessprotocol.AuthoringGrantSnapshot{ActorRef: accessprotocol.ActorRef{ActorID: "owner", Kind: "user"}, GrantedCapabilities: canonicalCapabilities(caps), HostDocumentID: "doc", IssuedAt: protocolcommon.Rfc3339Time(now.Format(time.RFC3339Nano)), LocalScopeID: "local", MembershipVersion: "1", PolicyRefs: []accessprotocol.PolicyRef{}}
	grant.AccessFingerprint = Fingerprint(grant)
	return grant
}

func testDigest(value string) protocolcommon.Digest { return digestJSON(value) }
