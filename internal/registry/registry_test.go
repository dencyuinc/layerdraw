// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registry

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

var testNow = time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)

func testDigest(ch byte) string { return "sha256:" + string(bytesRepeat(ch, 64)) }
func bytesRepeat(ch byte, n int) []byte {
	value := make([]byte, n)
	for i := range value {
		value[i] = ch
	}
	return value
}

type memoryClient struct {
	releases    []ArtifactRelease
	bytes       map[string][]byte
	offline     bool
	searchErr   error
	downloadErr error
	searches    atomic.Int64
}

func (m *memoryClient) Search(_ context.Context, source RegistrySource, input SearchInput) ([]ArtifactRelease, error) {
	m.searches.Add(1)
	if m.offline {
		return nil, errors.New("offline")
	}
	if m.searchErr != nil {
		return nil, m.searchErr
	}
	out := []ArtifactRelease{}
	for _, release := range m.releases {
		if release.SourceID == source.SourceID && (input.Query == "" || release.Identity.CanonicalID == input.Query) {
			out = append(out, cloneRelease(release))
		}
	}
	return out, nil
}
func (m *memoryClient) Download(_ context.Context, _ RegistrySource, release ArtifactRelease) ([]byte, error) {
	if m.offline {
		return nil, errors.New("offline")
	}
	if m.downloadErr != nil {
		return nil, m.downloadErr
	}
	value, ok := m.bytes[release.Digest]
	if !ok {
		return nil, errors.New("missing")
	}
	return append([]byte{}, value...), nil
}
func (m *memoryClient) OpenArtifact(ctx context.Context, source RegistrySource, release ArtifactRelease) (io.ReadCloser, error) {
	value, err := m.Download(ctx, source, release)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(value)), nil
}

type validator struct {
	fail, mismatch, nilImpact, migration bool
	aggregateCapabilities                []semantic.AuthoringCapability
	mutationInvalid                      string
	mu                                   sync.Mutex
	lastBuild                            RegistryMutationBuildInput
}

func (v *validator) ValidateRegistryArtifact(_ context.Context, release ArtifactRelease, _ []byte) (ValidatedArtifact, error) {
	if v.fail {
		return ValidatedArtifact{}, errors.New("invalid container")
	}
	canonical := release.Digest
	if v.mismatch {
		canonical = testDigest('f')
	}
	impact := &semantic.AuthoringImpact{BaseDefinitionHash: protocolcommon.Digest(testDigest('1')), ResultingDefinitionHash: protocolcommon.Digest(testDigest('2')), SemanticDiffHash: protocolcommon.Digest(testDigest('3')), SourceDiffHash: protocolcommon.Digest(testDigest('4')), ImpactDigest: protocolcommon.Digest(testDigest('5')), RequiredCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilitySchemaWrite}, Entries: []semantic.AuthoringImpactEntry{}}
	if v.nilImpact {
		impact = nil
	}
	if impact == nil {
		return ValidatedArtifact{Identity: release.Identity, CanonicalDigest: canonical, StagedTreeManifest: testDigest('6'), ResolvedLockDigest: testDigest('7'), MutationDigest: testDigest('8'), AuthoringImpactDigest: testDigest('5'), Diagnostics: []string{}}, nil
	}
	migrationDigest := ""
	if v.migration {
		migrationDigest = testDigest('e')
	}
	return ValidatedArtifact{Identity: release.Identity, CanonicalDigest: canonical, StagedTreeManifest: testDigest('6'), ResolvedLockDigest: testDigest('7'), MutationDigest: testDigest('8'), AuthoringImpactDigest: string(impact.ImpactDigest), AuthoringImpact: impact, AddressMigrationPlanDigest: migrationDigest, Diagnostics: []string{}}, nil
}
func (v *validator) BuildRegistryMutationPlan(_ context.Context, input RegistryMutationBuildInput) (ProjectMutationPlan, error) {
	v.mu.Lock()
	v.lastBuild = cloneJSONValue(input)
	v.mu.Unlock()
	if v.fail {
		return ProjectMutationPlan{}, errors.New("invalid mutation")
	}
	caps := v.aggregateCapabilities
	if len(caps) == 0 {
		caps = []semantic.AuthoringCapability{semantic.AuthoringCapabilitySchemaWrite}
	}
	impact := semantic.AuthoringImpact{BaseDefinitionHash: protocolcommon.Digest(input.Project.DefinitionHash), ResultingDefinitionHash: protocolcommon.Digest(testDigest('2')), SemanticDiffHash: protocolcommon.Digest(testDigest('3')), SourceDiffHash: protocolcommon.Digest(testDigest('4')), ImpactDigest: protocolcommon.Digest(testDigest('5')), RequiredCapabilities: append([]semantic.AuthoringCapability{}, caps...), Entries: []semantic.AuthoringImpactEntry{}}
	result := ProjectMutationPlan{BaseProjectRevision: input.Project.Revision, ExpectedDefinitionHash: input.Project.DefinitionHash, ExpectedResolvedLockDigest: input.Project.DependencySnapshot.ResolvedLockDigest, StagedTreeManifest: testDigest('6'), ResolvedLockDelta: input.ResolvedLockDelta, SourceEdits: []SourceEdit{}, MutationDigest: digestJSON(struct {
		Action Action
		Delta  ResolvedLockDelta
	}{input.Action, input.ResolvedLockDelta}), AuthoringImpact: impact, AuthoringImpactDigest: string(impact.ImpactDigest), TrustPolicyDigest: testDigest('a')}
	switch v.mutationInvalid {
	case "digest":
		result.AuthoringImpactDigest = ""
	case "precondition":
		result.BaseProjectRevision = "wrong"
	}
	return result, nil
}
func (v *validator) capturedLastBuild() RegistryMutationBuildInput {
	v.mu.Lock()
	defer v.mu.Unlock()
	return cloneJSONValue(v.lastBuild)
}

type accessPort struct {
	deny  bool
	err   error
	calls atomic.Int64
}

func (a *accessPort) EvaluateRegistryPlan(_ context.Context, input accessprotocol.EvaluateAuthoringInput) (accessprotocol.AuthoringDecision, error) {
	a.calls.Add(1)
	if a.err != nil {
		return accessprotocol.AuthoringDecision{}, a.err
	}
	caps := []semantic.AuthoringCapability{semantic.AuthoringCapabilityPackageManage}
	var impactDigest *protocolcommon.Digest
	if input.AuthoringImpact != nil {
		digest := input.AuthoringImpact.ImpactDigest
		impactDigest = &digest
		for _, capability := range input.AuthoringImpact.RequiredCapabilities {
			if capability != semantic.AuthoringCapabilityPackageManage {
				caps = append(caps, capability)
			}
		}
	}
	sort.Slice(caps, func(i, j int) bool { return caps[i] < caps[j] })
	outcome := accessprotocol.AuthoringDecisionOutcomeAllow
	if a.deny {
		outcome = accessprotocol.AuthoringDecisionOutcomeDeny
	}
	hostDigests := make([]protocolcommon.Digest, len(input.HostOperationImpacts))
	for i, impact := range input.HostOperationImpacts {
		hostDigests[i] = impact.ImpactDigest
	}
	evaluation := protocolcommon.Digest(digestJSON(input))
	decisionDigest := protocolcommon.Digest(digestJSON(struct {
		Evaluation protocolcommon.Digest                   `json:"evaluation"`
		Outcome    accessprotocol.AuthoringDecisionOutcome `json:"outcome"`
		Missing    []semantic.AuthoringCapability          `json:"missing"`
		Violations []accessprotocol.ConstraintViolation    `json:"violations"`
	}{evaluation, outcome, []semantic.AuthoringCapability{}, []accessprotocol.ConstraintViolation{}}))
	return accessprotocol.AuthoringDecision{AccessFingerprint: input.GrantSnapshot.AccessFingerprint, ApprovalRuleRefs: []string{}, AuthoringImpactDigest: impactDigest, ConstraintViolations: []accessprotocol.ConstraintViolation{}, DecisionDigest: decisionDigest, Diagnostics: []protocolcommon.ProtocolDiagnostic{}, EvaluationDigest: evaluation, HostOperationImpactDigests: hostDigests, MissingCapabilities: []semantic.AuthoringCapability{}, Outcome: outcome, RequiredCapabilities: caps}, nil
}

type projectPort struct {
	mu           sync.RWMutex
	state        ProjectState
	err          error
	currentCalls atomic.Int64
}

func (p *projectPort) CurrentRegistryProjectState(_ context.Context, _ string) (ProjectState, error) {
	p.currentCalls.Add(1)
	p.mu.RLock()
	defer p.mu.RUnlock()
	state := p.state
	state.HostCapabilities = append([]string{}, p.state.HostCapabilities...)
	state.DependencySnapshot = cloneDependencySnapshot(p.state.DependencySnapshot)
	return state, p.err
}
func (p *projectPort) NewRegistryDocumentState(_ context.Context, _ ArtifactIdentity) (ProjectState, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	state := p.state
	state.ProjectID = "new-project"
	state.DocumentID = "new-document"
	state.Revision = ""
	state.DefinitionHash = ""
	state.DependencySnapshot = ProjectDependencySnapshot{}
	state.RuntimeSessionID = "runtime-session-template"
	state.EngineSnapshot = RegistryProjectSnapshot{Kind: RegistryProjectSnapshotEmptyTemplate, Handle: "engine-template-baseline", DocumentID: state.DocumentID, SourceClosureDigest: testDigest('e')}
	return state, p.err
}

type runtimePort struct {
	calls       atomic.Int64
	err         error
	result      *RuntimeCommitResult
	block       chan struct{}
	recovery    RuntimeRegistryOutcome
	recoveryErr error
	last        RuntimeCommitInput
}

func (r *runtimePort) CommitRegistryPlan(_ context.Context, input RuntimeCommitInput) (RuntimeCommitResult, error) {
	r.calls.Add(1)
	r.last = input
	if r.block != nil {
		<-r.block
	}
	if r.err != nil {
		return RuntimeCommitResult{}, r.err
	}
	if r.result != nil {
		return *r.result, nil
	}
	if input.AccessDecision.Outcome != accessprotocol.AuthoringDecisionOutcomeAllow || len(input.HostOperationImpacts) != 1 {
		return RuntimeCommitResult{}, errors.New("missing typed authorization binding")
	}
	result := RuntimeCommitResult{CommittedRevision: "r2", OperationResultID: input.OperationID, DocumentID: input.HostOperationImpacts[0].ResourceScope.DocumentID}
	if input.Plan.CreatesNewDocument {
		result.DocumentID = input.Plan.NewDocumentID
		result.InitialCommittedRevision = true
	}
	return result, nil
}
func (r *runtimePort) LookupRegistryCommit(_ context.Context, _ string, _ string, _ string) (RuntimeRegistryOutcome, error) {
	if r.recovery.Status == "" {
		return RuntimeRegistryOutcome{Status: RuntimeRegistryUnknown}, r.recoveryErr
	}
	return r.recovery, r.recoveryErr
}

type credentialBroker struct {
	lease CredentialLease
	err   error
}

func (b credentialBroker) ResolveRegistryConnection(_ context.Context, ref string) (CredentialLease, error) {
	lease := b.lease
	lease.ConnectionRef = ref
	return lease, b.err
}

type connector struct {
	calls atomic.Int64
	err   error
	seen  []byte
	block chan struct{}
}

func (c *connector) ProbeRegistrySource(_ context.Context, _ RegistrySource, lease CredentialLease) error {
	c.calls.Add(1)
	c.seen = append([]byte{}, lease.Credential...)
	if c.block != nil {
		<-c.block
	}
	return c.err
}

type authorPort struct {
	result AuthoredArtifact
	err    error
}

func (a authorPort) AuthorRegistryArtifact(_ context.Context, _ AuthorArtifactRequest) (AuthoredArtifact, error) {
	return a.result, a.err
}

type testEnv struct {
	registry   *Registry
	client     *memoryClient
	validator  *validator
	access     *accessPort
	project    *projectPort
	runtime    *runtimePort
	connector  *connector
	privateKey ed25519.PrivateKey
	store      TransactionStore
}

func newTestEnv(t *testing.T, store TransactionStore) *testEnv {
	t.Helper()
	public, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	v := &validator{}
	access := &accessPort{}
	project := &projectPort{state: ProjectState{ProjectID: "p", DocumentID: "doc", LocalScopeID: "local", Revision: "r1", DefinitionHash: testDigest('1'), DependencySnapshot: ProjectDependencySnapshot{ResolvedLockDigest: testDigest('0'), Installs: []LockedArtifact{}}, PackTreeManifest: testDigest('9'), HostCapabilities: []string{"render.svg"}, RuntimeSessionID: "runtime-session-project", LeaseToken: "lease-token-project", EngineSnapshot: RegistryProjectSnapshot{Kind: RegistryProjectSnapshotWorking, Handle: "engine-project-snapshot", DocumentID: "doc", Revision: "r1", DefinitionHash: testDigest('1'), SourceClosureDigest: testDigest('e')}, GrantSnapshot: accessprotocol.AuthoringGrantSnapshot{AccessFingerprint: protocolcommon.Digest(testDigest('a')), ActorRef: accessprotocol.ActorRef{ActorID: "user", Kind: "user"}, GrantedCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilityPackageManage, semantic.AuthoringCapabilitySchemaWrite}, HostDocumentID: "doc", IssuedAt: protocolcommon.Rfc3339Time(testNow.Format(time.RFC3339)), LocalScopeID: "local", MembershipVersion: "1", PolicyRefs: []accessprotocol.PolicyRef{}}}}
	runtime := &runtimePort{}
	broker := credentialBroker{lease: CredentialLease{Credential: []byte("opaque"), ExpiresAt: testNow.Add(time.Hour)}}
	registry, err := New(v, access, runtime, project, broker, store)
	if err != nil {
		t.Fatal(err)
	}
	registry.now = func() time.Time { return testNow }
	policy := TrustPolicy{PolicyID: "official", RequiredSignature: true, TrustedPublishers: map[string]bool{"layerdraw": true}, PublicKeys: map[string]ed25519.PublicKey{"key-1": public}, RevokedKeys: map[string]bool{}}
	if err := registry.PutTrustPolicy(policy); err != nil {
		t.Fatal(err)
	}
	if err := registry.ConfigureSource(RegistrySource{SourceID: "official", Kind: SourceOfficial, EndpointRef: "registry:official", TrustPolicyID: "official", CachePolicy: "verified", Priority: 100, Connected: true}); err != nil {
		t.Fatal(err)
	}
	client := &memoryClient{bytes: map[string][]byte{}}
	registry.RegisterClient(SourceOfficial, client)
	connector := &connector{}
	registry.RegisterConnector(SourceOfficial, connector)
	if err := registry.ConnectSource(context.Background(), "official", "credential:official"); err != nil {
		t.Fatal(err)
	}
	return &testEnv{registry: registry, client: client, validator: v, access: access, project: project, runtime: runtime, connector: connector, privateKey: privateKey, store: store}
}

func signedRelease(t *testing.T, key ed25519.PrivateKey, identity ArtifactIdentity, data []byte, deps []Dependency) ArtifactRelease {
	t.Helper()
	release := ArtifactRelease{Identity: identity, SourceID: "official", PublisherID: "layerdraw", Digest: digestBytes(data), ManifestDigest: testDigest('b'), DependencyMetadataDigest: digestJSON(deps), Size: int64(len(data)), Dependencies: append([]Dependency{}, deps...), Compatibility: []CompatibilityDecision{{Subject: "core", Required: ">=0.1.0", Available: "0.1.0", Status: "compatible", Diagnostics: []string{}}}, License: "Apache-2.0", ProvenanceDigest: testDigest('c')}
	statement := ArtifactSignatureStatement{ArtifactIdentity: identity, ArtifactDigest: release.Digest, ManifestDigest: release.ManifestDigest, DependencyMetadataDigest: release.DependencyMetadataDigest, PublisherID: release.PublisherID, IssuedAt: testNow.Add(-time.Minute), ExpiresAt: timePointer(testNow.Add(time.Hour))}
	bytes, err := json.Marshal(statement)
	if err != nil {
		t.Fatal(err)
	}
	release.Signature = &SignatureEnvelope{Profile: "ed25519", KeyID: "key-1", Statement: bytes, Signature: ed25519.Sign(key, bytes)}
	return release
}
func timePointer(value time.Time) *time.Time { return &value }
func planRequest(env *testEnv, action Action, identity ArtifactIdentity) PlanRequest {
	state, _ := env.project.CurrentRegistryProjectState(context.Background(), "p")
	return PlanRequest{Action: action, ProjectID: "p", BaseRevision: state.Revision, ExpectedDefinitionHash: state.DefinitionHash, ExpectedResolvedLockDigest: state.DependencySnapshot.ResolvedLockDigest, Requested: identity, DependencySnapshot: state.DependencySnapshot}
}
func addRelease(env *testEnv, release ArtifactRelease, data []byte) {
	env.client.releases = append(env.client.releases, release)
	env.client.bytes[release.Digest] = append([]byte{}, data...)
}

func TestPlanRejectsUnboundEngineProjectSnapshot(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	request := planRequest(env, ActionInstall, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/base", Version: "1.0.0"})
	env.project.mu.Lock()
	env.project.state.EngineSnapshot.DefinitionHash = testDigest('f')
	env.project.mu.Unlock()
	if _, err := env.registry.Plan(context.Background(), request); !IsFailure(err, FailurePlanStale) {
		t.Fatalf("mismatched Engine snapshot was accepted: %v", err)
	}
}

func TestInstallPlanBindsTrustTypedAccessAndFreshCommitState(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	bytes := []byte("pack")
	release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/base", Version: "1.0.0"}, bytes, nil)
	addRelease(env, release, bytes)
	plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, release.Identity))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.RequiredCapabilities) != 2 || plan.RequiredCapabilities[0] != "package:manage" || plan.RequiredCapabilities[1] != "schema:write" {
		t.Fatalf("typed capabilities not preserved: %#v", plan.RequiredCapabilities)
	}
	if plan.AuthoringImpact == nil || len(plan.HostOperationImpacts) != 1 || plan.AccessDecision.Outcome != "allow" || len(plan.SourceBindings) != 1 || !plan.ExpiresAt.After(testNow) {
		t.Fatalf("incomplete plan: %#v", plan)
	}
	result, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "op", IdempotencyKey: "idem"})
	if err != nil {
		t.Fatal(err)
	}
	if result.CommittedRevision != "r2" || env.runtime.calls.Load() != 1 || env.access.calls.Load() != 2 {
		t.Fatalf("commit did not re-evaluate: %#v", result)
	}
}

func TestCommitRejectsFreshTrustCapabilityAndMembershipChanges(t *testing.T) {
	for _, mutation := range []func(*testEnv){func(env *testEnv) {
		_, policy, _, _ := env.registry.sourceContext("official")
		policy.RevokedKeys["key-1"] = true
		_ = env.registry.PutTrustPolicy(policy)
	}, func(env *testEnv) {
		env.project.mu.Lock()
		env.project.state.HostCapabilities = []string{"changed"}
		env.project.mu.Unlock()
	}, func(env *testEnv) {
		env.project.mu.Lock()
		env.project.state.GrantSnapshot.MembershipVersion = "2"
		env.project.mu.Unlock()
	}} {
		env := newTestEnv(t, NewMemoryTransactionStore())
		data := []byte("pack")
		release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/base", Version: "1.0.0"}, data, nil)
		addRelease(env, release, data)
		plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, release.Identity))
		if err != nil {
			t.Fatal(err)
		}
		mutation(env)
		if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "op", IdempotencyKey: "id"}); !IsFailure(err, FailurePlanStale) {
			t.Fatalf("fresh mutation accepted: %v", err)
		}
		if env.runtime.calls.Load() != 0 {
			t.Fatal("stale plan reached Runtime")
		}
	}
}

func TestActionSpecificPlansAndOfflineRemove(t *testing.T) {
	oldBytes, newBytes := []byte("old"), []byte("new")
	for _, action := range []Action{ActionUpdate, ActionPin, ActionRemove, ActionRepair} {
		env := newTestEnv(t, NewMemoryTransactionStore())
		old := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/base", Version: "1.0.0"}, oldBytes, nil)
		newer := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/base", Version: "1.1.0"}, newBytes, nil)
		env.project.state.DependencySnapshot = ProjectDependencySnapshot{ResolvedLockDigest: testDigest('d'), Installs: []LockedArtifact{{Identity: old.Identity, SourceID: "official", Digest: old.Digest}}}
		addRelease(env, old, oldBytes)
		addRelease(env, newer, newBytes)
		requested := newer.Identity
		if action == ActionRepair {
			requested = old.Identity
		}
		if action == ActionRemove {
			env.client.offline = true
			requested = old.Identity
		}
		plan, err := env.registry.Plan(context.Background(), planRequest(env, action, requested))
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		switch action {
		case ActionUpdate:
			if len(plan.ResolvedLockDelta.Updated) != 1 {
				t.Fatalf("update delta: %#v", plan.ResolvedLockDelta)
			}
		case ActionPin:
			if len(plan.ResolvedLockDelta.Pinned) != 1 {
				t.Fatalf("pin delta: %#v", plan.ResolvedLockDelta)
			}
		case ActionRemove:
			if len(plan.ResolvedLockDelta.Removed) != 1 || env.client.searches.Load() != 0 {
				t.Fatalf("remove was not offline: %#v", plan)
			}
		case ActionRepair:
			if plan.Artifacts[0].Release.Digest != old.Digest {
				t.Fatal("repair did not bind exact lock")
			}
		}
	}
	env := newTestEnv(t, NewMemoryTransactionStore())
	templateBytes := []byte("template")
	template := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactTemplate, CanonicalID: "layerdraw/starter", Version: "1.0.0"}, templateBytes, nil)
	addRelease(env, template, templateBytes)
	plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionCreateFromTemplate, template.Identity))
	if err != nil || !plan.CreatesNewDocument {
		t.Fatalf("template flow: %#v %v", plan, err)
	}
}

func TestSearchTrustKindSignatureExpiryAndCaretZero(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	data := []byte("pack")
	release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/base", Version: "0.2.5"}, data, nil)
	addRelease(env, release, data)
	kind := ArtifactPack
	found, err := env.registry.Search(context.Background(), SearchInput{Query: "layerdraw/base", Kind: &kind})
	if err != nil || found[0].Trust == nil || found[0].Trust.Status != TrustVerified {
		t.Fatalf("search trust: %#v %v", found, err)
	}
	wrongKind := ArtifactTemplate
	if _, err := env.registry.Search(context.Background(), SearchInput{Query: "layerdraw/base", Kind: &wrongKind}); !IsFailure(err, FailureUnavailable) {
		t.Fatalf("kind mismatch accepted: %v", err)
	}
	if !matchesRange("0.2.5", "^0.2.3", false) || matchesRange("0.3.0", "^0.2.3", false) || matchesRange("0.0.4", "^0.0.3", false) {
		t.Fatal("caret zero semantics")
	}
	expired := cloneRelease(release)
	statement := ArtifactSignatureStatement{ArtifactIdentity: expired.Identity, ArtifactDigest: expired.Digest, ManifestDigest: expired.ManifestDigest, DependencyMetadataDigest: expired.DependencyMetadataDigest, PublisherID: expired.PublisherID, IssuedAt: testNow.Add(-time.Hour), ExpiresAt: timePointer(testNow.Add(-time.Second))}
	wire, _ := json.Marshal(statement)
	expired.Signature.Statement = wire
	expired.Signature.Signature = ed25519.Sign(env.privateKey, wire)
	if _, err := verifyTrust(testNow, RegistrySource{Kind: SourceOfficial}, TrustPolicy{PolicyID: "p", RequiredSignature: true, PublicKeys: map[string]ed25519.PublicKey{"key-1": env.privateKey.Public().(ed25519.PublicKey)}}, expired); !IsFailure(err, FailureSignatureInvalid) {
		t.Fatalf("expired statement accepted: %v", err)
	}
	policyA := TrustPolicy{PolicyID: "p", PublicKeys: map[string]ed25519.PublicKey{"key": bytesRepeat('a', 32)}}
	policyB := clonePolicy(policyA)
	policyB.PublicKeys["key"][0] = 'b'
	if digestTrust(policyA) == digestTrust(policyB) {
		t.Fatal("trust digest omitted key bytes")
	}
}

func TestCredentialBrokerConnectAndDisconnect(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	if err := env.registry.ConnectSource(context.Background(), "official", "keychain:official"); err != nil {
		t.Fatal(err)
	}
	source, _ := env.registry.getSource("official")
	if !source.Connected || source.AuthConnectionRef != "keychain:official" || string(env.connector.seen) != "opaque" {
		t.Fatalf("connection not verified: %#v", source)
	}
	if err := env.registry.DisconnectSource("official"); err != nil {
		t.Fatal(err)
	}
	source, _ = env.registry.getSource("official")
	if source.Connected || source.AuthConnectionRef != "" {
		t.Fatalf("disconnect retained auth: %#v", source)
	}
	env.registry.credentials = credentialBroker{err: errors.New("locked")}
	if err := env.registry.ConnectSource(context.Background(), "official", "keychain:bad"); !IsFailure(err, FailurePolicyDenied) {
		t.Fatalf("broker bypass: %v", err)
	}
}

func TestDiskStoreCASConcurrencyRestartAndPublicationFailures(t *testing.T) {
	root := t.TempDir()
	store, err := NewDiskTransactionStore(root)
	if err != nil {
		t.Fatal(err)
	}
	env := newTestEnv(t, store)
	data := []byte("pack")
	release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/base", Version: "1.0.0"}, data, nil)
	addRelease(env, release, data)
	plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, release.Identity))
	if err != nil {
		t.Fatal(err)
	}
	env.runtime.block = make(chan struct{})
	type commitAttempt struct {
		result RuntimeCommitResult
		err    error
	}
	results := make(chan commitAttempt, 8)
	for i := 0; i < 8; i++ {
		go func() {
			result, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "op", IdempotencyKey: "same"})
			results <- commitAttempt{result: result, err: err}
		}()
	}
	for env.runtime.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	close(env.runtime.block)
	success := 0
	for i := 0; i < 8; i++ {
		attempt := <-results
		if attempt.err == nil {
			success++
			if attempt.result.DocumentID != "doc" || attempt.result.InitialCommittedRevision || attempt.result.CommittedRevision == "" || attempt.result.OperationResultID != "op" {
				t.Fatalf("CAS retry returned partial Runtime result: %#v", attempt.result)
			}
		}
	}
	if env.runtime.calls.Load() != 1 || success < 1 {
		t.Fatalf("double publication calls=%d success=%d", env.runtime.calls.Load(), success)
	}
	reopened, err := NewDiskTransactionStore(root)
	if err != nil {
		t.Fatal(err)
	}
	tx, ok, err := reopened.GetRegistryTransaction(context.Background(), plan.TransactionID)
	if err != nil || !ok || transactionState(tx) != StateCommitted {
		t.Fatalf("restart load: %#v %v", tx, err)
	}
	env2 := newTestEnv(t, NewMemoryTransactionStore())
	release = signedRelease(t, env2.privateKey, release.Identity, data, nil)
	addRelease(env2, release, data)
	plan, err = env2.registry.Plan(context.Background(), planRequest(env2, ActionInstall, release.Identity))
	if err != nil {
		t.Fatal(err)
	}
	env2.runtime.err = &RuntimePublicationError{Published: true, Cause: errors.New("finalize failed")}
	if _, err := env2.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "op", IdempotencyKey: "id"}); !IsFailure(err, FailureRepairRequired) {
		t.Fatalf("post publication: %v", err)
	}
	tx, _ = env2.registry.Transaction(plan.TransactionID)
	if transactionState(tx) != StateRepairRequired {
		t.Fatalf("missing repair state: %#v", tx.Events)
	}
}

func TestWireDispatcherStrictParityAndRecovery(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	binding, err := NewHostBinding(env.registry)
	if err != nil {
		t.Fatal(err)
	}
	request := WireRequest{WireVersion: RegistryWireVersion, Operation: WireListSources, RequestID: "req-1", Input: json.RawMessage(`{}`)}
	wire, _ := json.Marshal(request)
	var response WireResponse
	if err := json.Unmarshal(binding.Dispatch(context.Background(), wire), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.Operation != WireListSources || response.RequestID != "req-1" {
		t.Fatalf("wire mismatch: %#v", response)
	}
	bad := binding.Dispatch(context.Background(), []byte(`{"wire_version":"1.0","operation":"registry.list_sources","request_id":"x","input":{},"extra":true}`))
	if json.Unmarshal(bad, &response) != nil || response.OK || response.Failure.Code != FailureUnsupportedFormat {
		t.Fatalf("strict wire accepted unknown field: %s", bad)
	}
	contract, err := os.ReadFile(filepath.Join("..", "..", "schemas", "fixtures", "conformance", "registry", "host-wire-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		WireVersion string   `json:"wire_version"`
		Operations  []string `json:"operations"`
	}
	if err := json.Unmarshal(contract, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.WireVersion != RegistryWireVersion || len(fixture.Operations) != 10 {
		t.Fatalf("wire source mismatch: %#v", fixture)
	}
}

func TestAuthoringAndFailureEdges(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	request := AuthorArtifactRequest{Kind: ArtifactPack, ProjectID: "p", OutputName: "pack.ldpack", PublisherID: "layerdraw", Version: "1.0.0"}
	if _, err := env.registry.AuthorArtifact(context.Background(), request); !IsFailure(err, FailureUnavailable) {
		t.Fatalf("missing author: %v", err)
	}
	data := []byte("authored")
	release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/pack", Version: "1.0.0"}, data, nil)
	env.registry.RegisterPackageAuthor(authorPort{result: AuthoredArtifact{Release: release, Bytes: data}})
	result, err := env.registry.AuthorArtifact(context.Background(), request)
	if err != nil || result.Release.Digest != release.Digest {
		t.Fatalf("author: %#v %v", result, err)
	}
	for _, invalid := range []AuthorArtifactRequest{{}, {Kind: ArtifactPack, ProjectID: "p", OutputName: "bad.layerdraw", PublisherID: "p", Version: "1.0.0"}} {
		if _, err := env.registry.AuthorArtifact(context.Background(), invalid); !IsFailure(err, FailureUnsupportedFormat) {
			t.Fatalf("invalid author accepted: %#v", invalid)
		}
	}
	if _, err := New(nil, env.access, env.runtime, env.project, env.registry.credentials, env.store); err == nil {
		t.Fatal("nil validator accepted")
	}
	if err := env.registry.ConfigureSource(RegistrySource{SourceID: "bad", EndpointRef: "https://x?token=redacted", TrustPolicyID: "official"}); !IsFailure(err, FailurePolicyDenied) {
		t.Fatalf("credential in config: %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("unsupported digest value")
		}
	}()
	_ = digestJSON(make(chan int))
}
