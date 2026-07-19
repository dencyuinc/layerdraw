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

type testPlatformIdentity struct {
	id  string
	err error
}

func (a testPlatformIdentity) StableLocalUserID(context.Context) (string, error) { return a.id, a.err }

type testReadSource struct{ records []Record }

func (s testReadSource) ReadUnredacted(context.Context, ReadRequest) ([]Record, error) {
	return s.records, nil
}

type observingReadSource struct {
	records []Record
	called  bool
}

func (s *observingReadSource) ReadUnredacted(context.Context, ReadRequest) ([]Record, error) {
	s.called = true
	return s.records, nil
}

type testPolicyResolver struct{ policy ProjectionPolicy }

func (r testPolicyResolver) ResolveProjectionPolicy(context.Context, ReadRequest) (ProjectionPolicy, error) {
	return r.policy, nil
}

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

func TestPlatformLocalActorResolverUsesOnlyStableHostIdentity(t *testing.T) {
	resolver := PlatformLocalActorResolver{Adapter: testPlatformIdentity{id: "platform-user-501"}}
	first, err := resolver.ResolveLocalActor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolver.ResolveLocalActor(context.Background())
	if err != nil || first != second || first.Kind != "user" || first.ActorID != "platform-user-501" {
		t.Fatalf("platform actor=%+v %+v err=%v", first, second, err)
	}
	if _, err := (PlatformLocalActorResolver{}).ResolveLocalActor(context.Background()); err == nil {
		t.Fatal("missing platform adapter accepted")
	}
	if _, err := (PlatformLocalActorResolver{Adapter: testPlatformIdentity{err: errors.New("platform unavailable")}}).ResolveLocalActor(context.Background()); err == nil {
		t.Fatal("platform identity failure ignored")
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
	if err := ValidateAuthoringDecisionBindings(decision, &impact, []accessprotocol.HostOperationImpact{host}); err != nil {
		t.Fatalf("valid decision bindings rejected: %v", err)
	}
	for name, mutate := range map[string]func(*accessprotocol.AuthoringDecision){
		"wire value":       func(value *accessprotocol.AuthoringDecision) { value.DecisionDigest = "invalid" },
		"authoring digest": func(value *accessprotocol.AuthoringDecision) { value.AuthoringImpactDigest = nil },
		"host count":       func(value *accessprotocol.AuthoringDecision) { value.HostOperationImpactDigests = nil },
		"host digest": func(value *accessprotocol.AuthoringDecision) {
			value.HostOperationImpactDigests[0] = testDigest("other-host")
		},
		"required count": func(value *accessprotocol.AuthoringDecision) {
			value.RequiredCapabilities = value.RequiredCapabilities[:1]
		},
		"required order": func(value *accessprotocol.AuthoringDecision) {
			value.RequiredCapabilities[0], value.RequiredCapabilities[1] = value.RequiredCapabilities[1], value.RequiredCapabilities[0]
		},
		"decision digest": func(value *accessprotocol.AuthoringDecision) { value.DecisionDigest = testDigest("other-decision") },
	} {
		t.Run("invalid "+name, func(t *testing.T) {
			candidate := decision
			candidate.HostOperationImpactDigests = append([]protocolcommon.Digest(nil), decision.HostOperationImpactDigests...)
			candidate.RequiredCapabilities = append([]semantic.AuthoringCapability(nil), decision.RequiredCapabilities...)
			mutate(&candidate)
			if ValidateAuthoringDecisionBindings(candidate, &impact, []accessprotocol.HostOperationImpact{host}) == nil {
				t.Fatalf("invalid %s accepted", name)
			}
		})
	}
	if ValidateAuthoringDecisionBindings(decision, nil, []accessprotocol.HostOperationImpact{host}) == nil {
		t.Fatal("nil authoring impact accepted")
	}
	if ValidateAuthoringDecisionBindings(decision, &impact, nil) == nil {
		t.Fatal("host impact count mismatch accepted")
	}
	changedImpact := impact
	changedImpact.RequiredCapabilities = append([]semantic.AuthoringCapability(nil), impact.RequiredCapabilities...)
	changedImpact.RequiredCapabilities[0] = semantic.AuthoringCapabilityQueryWrite
	if ValidateAuthoringDecisionBindings(decision, &changedImpact, []accessprotocol.HostOperationImpact{host}) == nil {
		t.Fatal("noncanonical required capability projection accepted")
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
	if got := constraintViolations(impact, allDenied); len(got) != 1 || got[0].Code != "authoring.constraint_facts_missing" {
		t.Fatalf("missing constrained graph facts = %+v", got)
	}
	impact.Entries[0].GraphFacts = &semantic.GraphAuthoringFacts{ActionFlags: []string{"future_action"}, ColumnAddresses: []semantic.ColumnAddress{}, EndpointEntityAddresses: []semantic.EntityAddress{}, EntityTypeAddresses: []semantic.EntityTypeAddress{}, LayerAddresses: []semantic.LayerAddress{}, RelationTypeAddresses: []semantic.RelationTypeAddress{}}
	if got := constraintViolations(impact, GraphConstraints{EntityTypes: map[string]bool{}}); len(got) != 1 {
		t.Fatalf("unknown constrained graph action = %+v", got)
	}
	impact.Entries[0].Capability = semantic.AuthoringCapabilitySchemaWrite
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
	boundary, err := NewReadBoundary(testReadSource{records}, testPolicyResolver{policy})
	if err != nil {
		t.Fatal(err)
	}
	for _, surface := range []ReadSurface{SurfaceSearch, SurfaceQuery, SurfaceReview, SurfaceExport, SurfaceMCP} {
		projected, err := boundary.Read(context.Background(), ReadRequest{Surface: surface, Actor: accessprotocol.ActorRef{ActorID: "agent", Kind: "agent"}, DocumentID: "doc"})
		if err != nil || len(projected) != 1 || projected[0].Fields["public"] != "ok" {
			t.Fatalf("%s projection = %+v %v", surface, projected, err)
		}
		if _, leaked := projected[0].Fields["secret"]; leaked {
			t.Fatalf("%s leaked redacted field", surface)
		}
	}
	policy.Export = false
	boundary, _ = NewReadBoundary(testReadSource{records}, testPolicyResolver{policy})
	if _, err := boundary.Read(context.Background(), ReadRequest{Surface: SurfaceExport}); !errors.Is(err, ErrReadDenied) {
		t.Fatalf("export scope = %v", err)
	}
	deniedSource := &observingReadSource{records: records}
	boundary, _ = NewReadBoundary(deniedSource, testPolicyResolver{ProjectionPolicy{Read: false}})
	if _, err := boundary.Read(context.Background(), ReadRequest{Surface: SurfaceMCP}); !errors.Is(err, ErrReadDenied) || deniedSource.called {
		t.Fatalf("denied raw read: called=%v err=%v", deniedSource.called, err)
	}
	exportSource := &observingReadSource{records: records}
	boundary, _ = NewReadBoundary(exportSource, testPolicyResolver{ProjectionPolicy{Read: true, Export: false}})
	if _, err := boundary.Read(context.Background(), ReadRequest{Surface: SurfaceExport}); !errors.Is(err, ErrReadDenied) || exportSource.called {
		t.Fatalf("denied export raw read: called=%v err=%v", exportSource.called, err)
	}
	unknownSource := &observingReadSource{records: records}
	boundary, _ = NewReadBoundary(unknownSource, testPolicyResolver{ProjectionPolicy{Read: true, Export: true}})
	if _, err := boundary.Read(context.Background(), ReadRequest{Surface: ReadSurface("future")}); !errors.Is(err, ErrReadDenied) || unknownSource.called {
		t.Fatalf("unknown surface raw read: called=%v err=%v", unknownSource.called, err)
	}

	nested := map[string]any{"value": "original"}
	boundary, _ = NewReadBoundary(testReadSource{[]Record{{SubjectAddress: "entity", Fields: map[string]any{"nested": nested}}}}, testPolicyResolver{ProjectionPolicy{Read: true}})
	projected, err := boundary.Read(context.Background(), ReadRequest{Surface: SurfaceQuery})
	if err != nil {
		t.Fatal(err)
	}
	projected[0].Fields["nested"].(map[string]any)["value"] = "changed"
	if nested["value"] != "original" {
		t.Fatal("projected nested value aliases unredacted source")
	}
}

func TestDelegationSnapshotSurvivesRestartAndPreservesRevocation(t *testing.T) {
	now := time.Date(2026, 7, 19, 9, 0, 0, 0, time.UTC)
	parent := ownerGrant(now, []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite})
	store := NewDelegationStore()
	for _, id := range []string{"live", "revoked"} {
		_, err := store.Delegate(parent, Delegation{ID: id, ParentActor: parent.ActorRef, Agent: accessprotocol.ActorRef{ActorID: "agent-" + id, Kind: "agent"}, DocumentID: parent.HostDocumentID, LocalScopeID: parent.LocalScopeID, AuthoringCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite}, Permissions: AgentPermissions{Read: true, Propose: true}, IssuedAt: now, ExpiresAt: now.Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Revoke("revoked"); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewDelegationStoreFromSnapshot(store.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := restarted.Grant(parent, "live", now.Add(time.Minute)); err != nil {
		t.Fatalf("live delegation after restart: %v", err)
	}
	if _, _, err := restarted.Grant(parent, "revoked", now.Add(time.Minute)); !errors.Is(err, ErrGrantStale) {
		t.Fatalf("revoked delegation after restart: %v", err)
	}
	clone, err := restarted.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if err := clone.Revoke("live"); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Resolve("live", now); err != nil {
		t.Fatalf("clone mutation changed source store: %v", err)
	}
	for _, invalid := range []DelegationSnapshot{{}, {Version: 1}, {Version: 1, Records: []Delegation{{ID: "bad"}}, Revoked: map[string]uint64{}}, {Version: 1, Records: store.Snapshot().Records, Revoked: map[string]uint64{"missing": 1}}} {
		if _, err := NewDelegationStoreFromSnapshot(invalid); !errors.Is(err, ErrInvalidDelegation) {
			t.Fatalf("invalid snapshot accepted: %+v err=%v", invalid, err)
		}
	}
}

func TestEvaluationDigestBindsRevisionPolicyAndDelegation(t *testing.T) {
	now := time.Date(2026, 7, 19, 9, 0, 0, 0, time.UTC)
	grant := ownerGrant(now, []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite})
	base := accessprotocol.EvaluateAuthoringInput{BaseRevisionDigest: testDigest("revision-a"), GrantSnapshot: grant, HostOperationImpacts: []accessprotocol.HostOperationImpact{}, RequestIntent: "preview"}
	evaluator := Evaluator{Clock: fixedClock{now}}
	first, err := evaluator.Evaluate(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	assertChanged := func(name string, input accessprotocol.EvaluateAuthoringInput) {
		t.Helper()
		decision, err := evaluator.Evaluate(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
		if decision.EvaluationDigest == first.EvaluationDigest {
			t.Fatalf("%s was not bound into evaluation digest", name)
		}
	}
	changedRevision := base
	changedRevision.BaseRevisionDigest = testDigest("revision-b")
	assertChanged("revision", changedRevision)
	changedPolicy := base
	changedPolicy.GrantSnapshot.PolicyRefs = []accessprotocol.PolicyRef{{PolicyID: "policy", PolicyDigest: testDigest("policy"), PolicyVersion: "2"}}
	assertChanged("policy", changedPolicy)
	changedDelegation := base
	delegation := testDigest("delegation")
	changedDelegation.GrantSnapshot.AgentDelegationDigest = &delegation
	changedDelegation.GrantSnapshot.ActorRef = accessprotocol.ActorRef{ActorID: "agent", Kind: "agent"}
	assertChanged("delegation", changedDelegation)
}

func TestHostOperationImpactDerivesClosedCapabilities(t *testing.T) {
	scope := accessprotocol.HostResourceScope{DocumentID: "doc", LocalScopeID: "local"}
	cases := []struct {
		kind   accessprotocol.HostOperationKind
		action string
		want   semantic.AuthoringCapability
	}{
		{accessprotocol.HostOperationKindAssetDelete, "delete", semantic.AuthoringCapabilityAssetWrite},
		{accessprotocol.HostOperationKindAssetPersist, "create", semantic.AuthoringCapabilityAssetWrite},
		{accessprotocol.HostOperationKindAssetStage, "stage", semantic.AuthoringCapabilityAssetWrite},
		{accessprotocol.HostOperationKindPackageTransaction, "update", semantic.AuthoringCapabilityPackageManage},
		{accessprotocol.HostOperationKindBackendConfigure, "update", semantic.AuthoringCapabilityProjectConfigure},
		{accessprotocol.HostOperationKindProjectConfigure, "update", semantic.AuthoringCapabilityProjectConfigure},
	}
	for _, test := range cases {
		impact, err := HostOperationImpact(test.kind, test.action, scope, []string{"z", "a"})
		if err != nil || !reflect.DeepEqual(impact.RequiredAuthoringCapabilities, []semantic.AuthoringCapability{test.want}) || !reflect.DeepEqual(impact.ResourceRefs, []string{"a", "z"}) || impact.ImpactDigest == "" {
			t.Fatalf("%s impact = %+v err=%v", test.kind, impact, err)
		}
	}
	if _, err := HostOperationImpact(accessprotocol.HostOperationKind("unknown"), "apply", scope, nil); err == nil {
		t.Fatal("unknown host operation accepted")
	}
	if _, err := HostOperationImpact(accessprotocol.HostOperationKindAssetStage, "update", scope, []string{"asset"}); err == nil {
		t.Fatal("invalid operation action accepted")
	}
	if _, err := HostOperationImpact(accessprotocol.HostOperationKindAssetStage, "stage", scope, []string{"asset", "asset"}); err == nil {
		t.Fatal("duplicate operation resource accepted")
	}
	impact, err := HostOperationImpact(accessprotocol.HostOperationKindPackageTransaction, "update", scope, []string{"package"})
	if err != nil || ValidateHostOperationImpact(impact) != nil {
		t.Fatalf("canonical owner impact failed validation: %+v %v", impact, err)
	}
	impact.ResourceScope.DocumentID = "replacement-document"
	if ValidateHostOperationImpact(impact) == nil {
		t.Fatal("host impact digest did not bind the complete resource scope")
	}
}

func ownerGrant(now time.Time, caps []semantic.AuthoringCapability) accessprotocol.AuthoringGrantSnapshot {
	grant := accessprotocol.AuthoringGrantSnapshot{ActorRef: accessprotocol.ActorRef{ActorID: "owner", Kind: "user"}, GrantedCapabilities: canonicalCapabilities(caps), HostDocumentID: "doc", IssuedAt: protocolcommon.Rfc3339Time(now.Format(time.RFC3339Nano)), LocalScopeID: "local", MembershipVersion: "1", PolicyRefs: []accessprotocol.PolicyRef{}}
	grant.AccessFingerprint = Fingerprint(grant)
	return grant
}

func testDigest(value string) protocolcommon.Digest { return digestJSON(value) }
