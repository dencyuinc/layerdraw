// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registry

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
)

func testFinalizedPlan(t *testing.T, id string) InstallPlan {
	t.Helper()
	impact, err := accesscore.HostOperationImpact(accessprotocol.HostOperationKindPackageTransaction, "update", accessprotocol.HostResourceScope{DocumentID: "doc", LocalScopeID: "local"}, []string{"test/package"})
	if err != nil {
		t.Fatal(err)
	}
	authoring := semantic.AuthoringImpact{BaseDefinitionHash: protocolcommon.Digest(testDigest('1')), Entries: []semantic.AuthoringImpactEntry{}, ImpactDigest: protocolcommon.Digest(testDigest('5')), RequiredCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilitySchemaWrite}, ResultingDefinitionHash: protocolcommon.Digest(testDigest('2')), SemanticDiffHash: protocolcommon.Digest(testDigest('3')), SourceDiffHash: protocolcommon.Digest(testDigest('4'))}
	evaluation := protocolcommon.Digest(testDigest('e'))
	decision := accessprotocol.AuthoringDecision{AccessFingerprint: protocolcommon.Digest(testDigest('a')), ApprovalRuleRefs: []string{}, AuthoringImpactDigest: &authoring.ImpactDigest, ConstraintViolations: []accessprotocol.ConstraintViolation{}, Diagnostics: []protocolcommon.ProtocolDiagnostic{}, EvaluationDigest: evaluation, HostOperationImpactDigests: []protocolcommon.Digest{impact.ImpactDigest}, MissingCapabilities: []semantic.AuthoringCapability{}, Outcome: accessprotocol.AuthoringDecisionOutcomeAllow, RequiredCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilityPackageManage, semantic.AuthoringCapabilitySchemaWrite}}
	decision.DecisionDigest = protocolcommon.Digest(digestJSON(struct {
		Evaluation protocolcommon.Digest                   `json:"evaluation"`
		Outcome    accessprotocol.AuthoringDecisionOutcome `json:"outcome"`
		Missing    []semantic.AuthoringCapability          `json:"missing"`
		Violations []accessprotocol.ConstraintViolation    `json:"violations"`
	}{evaluation, decision.Outcome, decision.MissingCapabilities, decision.ConstraintViolations}))
	delta := ResolvedLockDelta{Added: []LockedArtifact{}, Updated: []LockedArtifact{}, Removed: []LockedArtifact{}, Pinned: []LockedArtifact{}}
	plan := InstallPlan{TransactionID: id, Action: ActionUpdate, BaseRevision: "r1", ExpectedDefinitionHash: string(authoring.BaseDefinitionHash), ExpectedResolvedLockDigest: testDigest('0'), DependencySnapshot: ProjectDependencySnapshot{ResolvedLockDigest: testDigest('0'), Installs: []LockedArtifact{}}, ResolvedLockDelta: delta, RollbackCheckpoint: RollbackCheckpoint{BaseProjectRevision: "r1", BaseDefinitionHash: string(authoring.BaseDefinitionHash), BaseResolvedLockDigest: testDigest('0')}, MutationDigest: testDigest('m'), AuthoringImpactDigests: []string{string(authoring.ImpactDigest)}, EvaluationDigest: string(evaluation), HostOperationImpactDigest: string(impact.ImpactDigest), AuthoringImpact: &authoring, HostOperationImpacts: []accessprotocol.HostOperationImpact{impact}, AccessDecision: decision, RequiredCapabilities: []string{string(semantic.AuthoringCapabilityPackageManage), string(semantic.AuthoringCapabilitySchemaWrite)}, RequestedRoot: ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "test/package", Version: "1.0.0"}}
	plan.ProjectMutationPlan = ProjectMutationPlan{RegistryTransactionID: id, BaseProjectRevision: plan.BaseRevision, ExpectedDefinitionHash: plan.ExpectedDefinitionHash, ExpectedResolvedLockDigest: plan.ExpectedResolvedLockDigest, ResolvedLockDelta: delta, MutationDigest: plan.MutationDigest, AuthoringImpact: authoring, AuthoringImpactDigest: string(authoring.ImpactDigest), HostOperationImpactDigest: plan.HostOperationImpactDigest, EvaluationDigest: plan.EvaluationDigest}
	plan.PlanDigest = digestPlan(plan)
	plan.ProjectMutationPlan.PlanDigest = plan.PlanDigest
	return plan
}

func dispatchForTest(t *testing.T, binding *HostBinding, operation WireOperation, input any) WireResponse {
	t.Helper()
	encodedInput, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	request, err := json.Marshal(WireRequest{WireVersion: RegistryWireVersion, Operation: operation, RequestID: "req", Input: encodedInput})
	if err != nil {
		t.Fatal(err)
	}
	var response WireResponse
	if err := json.Unmarshal(binding.Dispatch(context.Background(), request), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func TestWireDispatcherAllOperationsAndInputFailures(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	data := []byte("wire-pack")
	release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/wire", Version: "1.0.0"}, data, nil)
	addRelease(env, release, data)
	env.registry.RegisterPackageAuthor(authorPort{result: AuthoredArtifact{Release: release, Bytes: data}})
	binding, err := NewHostBinding(env.registry)
	if err != nil {
		t.Fatal(err)
	}

	assertOK := func(operation WireOperation, input any) WireResponse {
		t.Helper()
		response := dispatchForTest(t, binding, operation, input)
		if !response.OK {
			t.Fatalf("%s failed: %#v", operation, response.Failure)
		}
		return response
	}
	assertOK(WireListSources, struct{}{})
	configured := RegistrySource{SourceID: "local", Kind: SourceLocalDirectory, EndpointRef: "file:fixtures", TrustPolicyID: "official", CachePolicy: "verified", Priority: 1}
	assertOK(WireConfigureSource, ConfigureSourceInput{Source: configured})
	assertOK(WireConnectSource, RegistryConnectionInput{SourceID: "official", ConnectionRef: "keychain:official"})
	assertOK(WireSearch, SearchInput{Query: release.Identity.CanonicalID, Kind: ptrArtifactKind(ArtifactPack)})
	planResponse := assertOK(WirePlan, planRequest(env, ActionInstall, release.Identity))
	var plan InstallPlan
	if err := json.Unmarshal(planResponse.Value, &plan); err != nil {
		t.Fatal(err)
	}
	assertOK(WireCommit, WireCommitInput{TransactionID: plan.TransactionID, PlanDigest: plan.PlanDigest, OperationID: "op", IdempotencyKey: "idem"})
	assertOK(WireGetTransaction, TransactionIDInput{TransactionID: plan.TransactionID})
	assertOK(WireRecoverTransaction, TransactionIDInput{TransactionID: plan.TransactionID})
	assertOK(WireAuthorArtifact, AuthorArtifactRequest{Kind: ArtifactPack, ProjectID: "p", OutputName: "wire.ldpack", PublisherID: "layerdraw", Version: "1.0.0"})
	assertOK(WireDisconnectSource, SourceIDInput{SourceID: "official"})

	for _, operation := range []WireOperation{WireListSources, WireConfigureSource, WireConnectSource, WireDisconnectSource, WireSearch, WirePlan, WireCommit, WireGetTransaction, WireRecoverTransaction, WireAuthorArtifact} {
		response := dispatchForTest(t, binding, operation, map[string]any{"unknown": true})
		if response.OK || response.Failure == nil {
			t.Fatalf("strict input accepted for %s", operation)
		}
	}
	for _, wire := range [][]byte{nil, []byte(`{}`), []byte(`{"wire_version":"2","operation":"registry.list_sources","request_id":"x","input":{}}`), []byte(`{"wire_version":"1.0","operation":"bad","request_id":"x","input":{}}`), []byte(`{"wire_version":"1.0","operation":"registry.list_sources","request_id":"x","input":{}} {}`)} {
		var response WireResponse
		if json.Unmarshal(binding.Dispatch(context.Background(), wire), &response) != nil || response.OK {
			t.Fatalf("invalid wire accepted: %s", wire)
		}
	}
	if _, err := NewHostBinding(nil); !IsFailure(err, FailureUnavailable) {
		t.Fatalf("nil host: %v", err)
	}
}

func ptrArtifactKind(value ArtifactKind) *ArtifactKind { return &value }

func TestSemverTrustAndFailureEdges(t *testing.T) {
	for _, test := range []struct {
		version, expression string
		prerelease, want    bool
	}{
		{"1.2.3", "", false, true}, {"1.2.3", "*", false, true}, {"2.0.0", "^1.2.3", false, false},
		{"1.3.0", "~1.2.3", false, false}, {"1.2.9", "~1.2.3", false, true}, {"2.0.0", ">=1.0.0", false, true},
		{"1.0.0-beta", "*", false, false}, {"1.0.0-beta", "*", true, true}, {"bad", "*", true, false},
		{"0.0.3", "^0.0.3", false, true}, {"0.1.2", "^0.1.0", false, true}, {"1.2.3", "1.2.3", false, true},
	} {
		if got := matchesRange(test.version, test.expression, test.prerelease); got != test.want {
			t.Errorf("matchesRange(%q,%q)=%v", test.version, test.expression, got)
		}
	}
	for _, pair := range []struct {
		a, b string
		want int
	}{{"1.0.0", "2.0.0", -1}, {"2.0.0", "1.0.0", 1}, {"1.0.0", "1.0.0", 0}, {"1.0.0", "1.0.0-beta", 1}, {"1.0.0-alpha", "1.0.0-beta", -1}, {"bad", "worse", -1}} {
		got := compareVersions(pair.a, pair.b)
		if got < 0 {
			got = -1
		} else if got > 0 {
			got = 1
		}
		if got != pair.want {
			t.Errorf("compareVersions(%q,%q)=%d", pair.a, pair.b, got)
		}
	}
	for _, invalid := range []string{"", "1", "1.2", "1.2.3.4", "01.2.3", "1..3", "x.2.3"} {
		if _, ok := parseVersion(invalid); ok {
			t.Errorf("parsed invalid version %q", invalid)
		}
	}

	f := fail(FailureUnavailable, "x", true, errors.New("cause"))
	if f.Error() == "" || errors.Unwrap(f) == nil || !IsFailure(f, FailureUnavailable) {
		t.Fatal("failure contract")
	}
	publication := &RuntimePublicationError{Cause: errors.New("publish")}
	if publication.Error() != "publish" || !errors.Is(publication, publication.Cause) {
		t.Fatal("publication error contract")
	}
	if (&RuntimePublicationError{}).Error() == "" {
		t.Fatal("empty publication error")
	}
}

func TestTrustPolicyBranches(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	data := []byte("trust")
	release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/trust", Version: "1.0.0"}, data, nil)
	source := RegistrySource{SourceID: "official", Kind: SourceOfficial}
	public := env.privateKey.Public().(ed25519.PublicKey)
	base := TrustPolicy{PolicyID: "p", RequiredSignature: true, TrustedPublishers: map[string]bool{"layerdraw": true}, PublicKeys: map[string]ed25519.PublicKey{"key-1": public}, RevokedKeys: map[string]bool{}}
	if _, err := verifyTrust(testNow, source, base, release); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		source  RegistrySource
		policy  TrustPolicy
		release ArtifactRelease
		code    string
	}{
		{"expired policy", source, func() TrustPolicy { p := clonePolicy(base); p.ExpiresAt = timePointer(testNow); return p }(), release, FailurePolicyDenied},
		{"publisher", source, func() TrustPolicy { p := clonePolicy(base); p.TrustedPublishers = map[string]bool{}; return p }(), release, FailurePolicyDenied},
		{"missing", source, base, func() ArtifactRelease { v := cloneRelease(release); v.Signature = nil; return v }(), FailureSignatureMissing},
		{"revoked", source, func() TrustPolicy { p := clonePolicy(base); p.RevokedKeys["key-1"] = true; return p }(), release, FailureSignatureRevoked},
		{"profile", source, base, func() ArtifactRelease { v := cloneRelease(release); v.Signature.Profile = "bad"; return v }(), FailureSignatureInvalid},
		{"statement", source, base, func() ArtifactRelease { v := cloneRelease(release); v.Signature.Statement = []byte("bad"); return v }(), FailureSignatureInvalid},
	}
	for _, tc := range cases {
		if _, err := verifyTrust(testNow, tc.source, tc.policy, tc.release); !IsFailure(err, tc.code) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	unsigned := cloneRelease(release)
	unsigned.Signature = nil
	if decision, err := verifyTrust(testNow, RegistrySource{Kind: SourceLocalDirectory}, TrustPolicy{PolicyID: "local", AllowUnsignedLocal: true}, unsigned); err != nil || decision.Status != TrustUnsignedAllowed {
		t.Fatalf("local unsigned: %#v %v", decision, err)
	}
	for _, kind := range []SourceKind{SourceOfficial, SourceOrganizationPrivate, SourceSelfHosted, SourceGit} {
		if _, err := verifyTrust(testNow, RegistrySource{Kind: kind}, TrustPolicy{PolicyID: "optional"}, unsigned); !IsFailure(err, FailureSignatureMissing) {
			t.Fatalf("%s optional signature accepted: %v", kind, err)
		}
	}
}

func TestConfigurationAuthoringStoreAndRecoveryEdges(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	if err := env.registry.PutTrustPolicy(TrustPolicy{}); err == nil {
		t.Fatal("empty policy accepted")
	}
	for _, source := range []RegistrySource{{}, {SourceID: "x", EndpointRef: "e", TrustPolicyID: "missing"}, {SourceID: "x", EndpointRef: "https://x?password=p", TrustPolicyID: "official"}} {
		if err := env.registry.ConfigureSource(source); err == nil {
			t.Fatalf("invalid source accepted: %#v", source)
		}
	}
	if err := env.registry.ConnectSource(context.Background(), "missing", "ref"); !IsFailure(err, FailureUnavailable) {
		t.Fatal(err)
	}
	if err := env.registry.DisconnectSource("missing"); !IsFailure(err, FailureUnavailable) {
		t.Fatal(err)
	}
	env.connector.err = errors.New("down")
	if err := env.registry.ConnectSource(context.Background(), "official", "ref"); !IsFailure(err, FailureUnavailable) {
		t.Fatal(err)
	}
	env.connector.err = nil
	env.registry.credentials = credentialBroker{lease: CredentialLease{Credential: []byte("x"), ExpiresAt: testNow}}
	if err := env.registry.ConnectSource(context.Background(), "official", "ref"); !IsFailure(err, FailurePolicyDenied) {
		t.Fatal(err)
	}

	request := AuthorArtifactRequest{Kind: ArtifactTemplate, ProjectID: "p", OutputName: "x.layerdraw", PublisherID: "layerdraw", Version: "1.0.0"}
	env.registry.RegisterPackageAuthor(authorPort{err: errors.New("bad")})
	if _, err := env.registry.AuthorArtifact(context.Background(), request); !IsFailure(err, FailureArtifactCorrupt) {
		t.Fatal(err)
	}
	data := []byte("wrong")
	release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "x", Version: "1.0.0"}, data, nil)
	env.registry.RegisterPackageAuthor(authorPort{result: AuthoredArtifact{Release: release, Bytes: data}})
	if _, err := env.registry.AuthorArtifact(context.Background(), request); !IsFailure(err, FailureArtifactCorrupt) {
		t.Fatal(err)
	}

	store := NewMemoryTransactionStore()
	tx := Transaction{Plan: InstallPlan{TransactionID: "tx", PlanDigest: "plan"}, Events: []TransactionEvent{{State: StatePlanned, Sequence: 1}}}
	if err := store.CreateRegistryTransaction(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateRegistryTransaction(context.Background(), tx); err == nil {
		t.Fatal("duplicate transaction")
	}
	if ok, _ := store.CompareAndSwapRegistryTransaction(context.Background(), "missing", 1, tx); ok {
		t.Fatal("missing CAS")
	}
	if ok, _ := store.CompareAndSwapRegistryTransaction(context.Background(), "tx", 9, tx); ok {
		t.Fatal("wrong-version CAS")
	}
	bad := cloneTransaction(tx)
	bad.Events = append(bad.Events, TransactionEvent{State: StateCommitted, Sequence: 3})
	if ok, err := store.CompareAndSwapRegistryTransaction(context.Background(), "tx", 1, bad); ok || err == nil {
		t.Fatal("invalid append")
	}
	if _, err := env.registry.GetTransaction(context.Background(), "missing"); !IsFailure(err, FailureUnavailable) {
		t.Fatal(err)
	}
	if _, err := env.registry.RecoverTransaction(context.Background(), "missing"); !IsFailure(err, FailureUnavailable) {
		t.Fatal(err)
	}
}

func TestDiskStoreValidationEdges(t *testing.T) {
	if _, err := NewDiskTransactionStore(""); err == nil {
		t.Fatal("empty root")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDiskTransactionStore(file); err == nil {
		t.Fatal("file root accepted")
	}
	store, err := NewDiskTransactionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.GetRegistryTransaction(context.Background(), "../bad"); err == nil {
		t.Fatal("unsafe id")
	}
	if _, err := store.CompareAndSwapRegistryTransaction(context.Background(), "../bad", 0, Transaction{}); err == nil {
		t.Fatal("unsafe CAS id")
	}
	id := "dddddddddddddddddddddddddddddddd"
	tx := Transaction{Plan: InstallPlan{TransactionID: id, PlanDigest: "plan"}, Events: []TransactionEvent{{State: StatePlanned, Sequence: 1}}}
	if err := store.CreateRegistryTransaction(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateRegistryTransaction(context.Background(), tx); err == nil {
		t.Fatal("duplicate disk tx")
	}
	if err := os.WriteFile(store.path(id), []byte(`{} {}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.GetRegistryTransaction(context.Background(), id); err == nil {
		t.Fatal("trailing JSON accepted")
	}
	_ = time.Second
}

type faultStore struct {
	base      TransactionStore
	createErr error
	getErr    error
	casErr    error
	casFalse  bool
}

type commitRaceStore struct {
	first  Transaction
	latest Transaction
	gets   atomic.Int64
}

func (s *commitRaceStore) CreateRegistryTransaction(context.Context, Transaction) error {
	return errors.New("unexpected create")
}
func (s *commitRaceStore) GetRegistryTransaction(context.Context, string) (Transaction, bool, error) {
	if s.gets.Add(1) == 1 {
		return cloneTransaction(s.first), true, nil
	}
	return cloneTransaction(s.latest), true, nil
}
func (s *commitRaceStore) CompareAndSwapRegistryTransaction(context.Context, string, uint64, Transaction) (bool, error) {
	return false, nil
}

func (s *faultStore) CreateRegistryTransaction(ctx context.Context, tx Transaction) error {
	if s.createErr != nil {
		return s.createErr
	}
	return s.base.CreateRegistryTransaction(ctx, tx)
}
func (s *faultStore) GetRegistryTransaction(ctx context.Context, id string) (Transaction, bool, error) {
	if s.getErr != nil {
		return Transaction{}, false, s.getErr
	}
	return s.base.GetRegistryTransaction(ctx, id)
}
func (s *faultStore) CompareAndSwapRegistryTransaction(ctx context.Context, id string, expected uint64, tx Transaction) (bool, error) {
	if s.casErr != nil {
		return false, s.casErr
	}
	if s.casFalse {
		return false, nil
	}
	return s.base.CompareAndSwapRegistryTransaction(ctx, id, expected, tx)
}

func TestDependencyResolutionAndPlanFailureBranches(t *testing.T) {
	t.Run("dependency success and constraint", func(t *testing.T) {
		env := newTestEnv(t, NewMemoryTransactionStore())
		childData := []byte("child")
		child := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/child", Version: "1.2.0"}, childData, nil)
		rootData := []byte("root")
		root := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/root", Version: "1.0.0"}, rootData, []Dependency{{CanonicalID: child.Identity.CanonicalID, VersionRange: "^1.0.0", DigestConstraint: child.Digest}})
		addRelease(env, child, childData)
		addRelease(env, root, rootData)
		plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, root.Identity))
		if err != nil || len(plan.Artifacts) != 2 {
			t.Fatalf("dependency plan: %#v %v", plan, err)
		}
	})
	t.Run("dependency cycle", func(t *testing.T) {
		env := newTestEnv(t, NewMemoryTransactionStore())
		aData, bData := []byte("a"), []byte("b")
		aID := ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "a", Version: "1.0.0"}
		bID := ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "b", Version: "1.0.0"}
		a := signedRelease(t, env.privateKey, aID, aData, []Dependency{{CanonicalID: "b", VersionRange: "1.0.0"}})
		b := signedRelease(t, env.privateKey, bID, bData, []Dependency{{CanonicalID: "a", VersionRange: "1.0.0"}})
		addRelease(env, a, aData)
		addRelease(env, b, bData)
		if _, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, aID)); !IsFailure(err, FailureDependencyCycle) {
			t.Fatalf("cycle: %v", err)
		}
	})
	t.Run("digest constraint", func(t *testing.T) {
		env := newTestEnv(t, NewMemoryTransactionStore())
		childData, rootData := []byte("child"), []byte("root")
		child := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "child", Version: "1.0.0"}, childData, nil)
		root := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "root", Version: "1.0.0"}, rootData, []Dependency{{CanonicalID: "child", VersionRange: "1.0.0", DigestConstraint: testDigest('f')}})
		addRelease(env, child, childData)
		addRelease(env, root, rootData)
		if _, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, root.Identity)); !IsFailure(err, FailureDependencyConflict) {
			t.Fatal(err)
		}
	})

	identity := ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/base", Version: "1.0.0"}
	makeEnv := func(t *testing.T) (*testEnv, PlanRequest) {
		env := newTestEnv(t, NewMemoryTransactionStore())
		data := []byte("pack")
		release := signedRelease(t, env.privateKey, identity, data, nil)
		addRelease(env, release, data)
		return env, planRequest(env, ActionInstall, identity)
	}
	tests := []struct {
		name   string
		mutate func(*testEnv, *PlanRequest)
		code   string
	}{
		{"invalid action", func(_ *testEnv, r *PlanRequest) { r.Action = "invalid" }, FailureDependencyConflict},
		{"template action pack", func(_ *testEnv, r *PlanRequest) { r.Action = ActionCreateFromTemplate }, FailureUnsupportedFormat},
		{"project error", func(e *testEnv, _ *PlanRequest) { e.project.err = errors.New("down") }, FailureUnavailable},
		{"stale project", func(_ *testEnv, r *PlanRequest) { r.BaseRevision = "old" }, FailurePlanStale},
		{"already installed", func(e *testEnv, _ *PlanRequest) {
			e.project.state.DependencySnapshot.Installs = []LockedArtifact{{Identity: identity}}
		}, FailureDependencyConflict},
		{"missing update", func(_ *testEnv, r *PlanRequest) { r.Action = ActionUpdate }, FailureDependencyConflict},
		{"pin latest", func(e *testEnv, r *PlanRequest) {
			r.Action = ActionPin
			r.Requested.Version = "latest"
			e.project.state.DependencySnapshot.Installs = []LockedArtifact{{Identity: identity}}
		}, FailureDependencyConflict},
		{"validator error", func(e *testEnv, _ *PlanRequest) { e.validator.fail = true }, FailureUnavailable},
		{"validator mismatch", func(e *testEnv, _ *PlanRequest) { e.validator.mismatch = true }, FailureUnavailable},
		{"access denied", func(e *testEnv, _ *PlanRequest) { e.access.deny = true }, FailurePolicyDenied},
		{"access error", func(e *testEnv, _ *PlanRequest) { e.access.err = errors.New("access") }, FailurePolicyDenied},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, request := makeEnv(t)
			tc.mutate(env, &request)
			if _, err := env.registry.Plan(context.Background(), request); !IsFailure(err, tc.code) {
				t.Fatalf("want %s: %v", tc.code, err)
			}
		})
	}
	t.Run("incompatible release", func(t *testing.T) {
		env, request := makeEnv(t)
		env.client.releases[0].Compatibility = []CompatibilityDecision{{Subject: "engine", Status: "disabled"}}
		if _, err := env.registry.Plan(context.Background(), request); !IsFailure(err, FailureIncompatibleCapability) {
			t.Fatal(err)
		}
	})
	t.Run("missing host capability", func(t *testing.T) {
		env, request := makeEnv(t)
		env.client.releases[0].Compatibility = []CompatibilityDecision{{Subject: "capability:gpu", Status: "compatible"}}
		if _, err := env.registry.Plan(context.Background(), request); !IsFailure(err, FailureIncompatibleCapability) {
			t.Fatal(err)
		}
	})
	t.Run("transaction create", func(t *testing.T) {
		base := NewMemoryTransactionStore()
		env := newTestEnv(t, &faultStore{base: base, createErr: errors.New("disk")})
		data := []byte("pack")
		release := signedRelease(t, env.privateKey, identity, data, nil)
		addRelease(env, release, data)
		if _, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, identity)); !IsFailure(err, FailureUnavailable) {
			t.Fatal(err)
		}
	})
}

func TestCommitIdempotencyFailuresAndRecovery(t *testing.T) {
	prepare := func(t *testing.T) (*testEnv, InstallPlan) {
		env := newTestEnv(t, NewMemoryTransactionStore())
		data := []byte("pack")
		release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "pack", Version: "1.0.0"}, data, nil)
		addRelease(env, release, data)
		plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, release.Identity))
		if err != nil {
			t.Fatal(err)
		}
		return env, plan
	}
	t.Run("idempotent", func(t *testing.T) {
		env, plan := prepare(t)
		input := RuntimeCommitInput{Plan: plan, OperationID: "op", IdempotencyKey: "same"}
		first, err := env.registry.Commit(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
		second, err := env.registry.Commit(context.Background(), input)
		if err != nil || second != first || env.runtime.calls.Load() != 1 {
			t.Fatalf("idempotency: %#v %v", second, err)
		}
		input.IdempotencyKey = "other"
		if _, err := env.registry.Commit(context.Background(), input); !IsFailure(err, FailurePlanStale) {
			t.Fatal(err)
		}
	})
	t.Run("expired", func(t *testing.T) {
		env, plan := prepare(t)
		env.registry.now = func() time.Time { return testNow.Add(time.Hour) }
		if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "op", IdempotencyKey: "id"}); !IsFailure(err, FailurePlanStale) {
			t.Fatal(err)
		}
	})
	t.Run("project error", func(t *testing.T) {
		env, plan := prepare(t)
		env.project.err = errors.New("down")
		if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "op", IdempotencyKey: "id"}); !IsFailure(err, FailurePlanStale) {
			t.Fatal(err)
		}
	})
	t.Run("access denial", func(t *testing.T) {
		env, plan := prepare(t)
		env.access.deny = true
		if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "op", IdempotencyKey: "id"}); !IsFailure(err, FailurePlanStale) {
			t.Fatal(err)
		}
	})
	t.Run("prepublication rollback", func(t *testing.T) {
		env, plan := prepare(t)
		env.runtime.err = errors.New("before")
		if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "op", IdempotencyKey: "id"}); !IsFailure(err, FailureUnavailable) {
			t.Fatal(err)
		}
		tx, _ := env.registry.Transaction(plan.TransactionID)
		if transactionState(tx) != StateRolledBack {
			t.Fatal(tx.Events)
		}
		if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "op", IdempotencyKey: "id"}); !IsFailure(err, FailurePlanStale) {
			t.Fatal(err)
		}
	})
	t.Run("applying recovery", func(t *testing.T) {
		env, plan := prepare(t)
		tx, _, _ := env.store.GetRegistryTransaction(context.Background(), plan.TransactionID)
		version := transactionVersion(tx)
		tx.Events = append(tx.Events, TransactionEvent{State: StateApplying, EvidenceDigest: plan.EvaluationDigest, Sequence: version + 1, IdempotencyKey: "id"})
		ok, err := env.store.CompareAndSwapRegistryTransaction(context.Background(), plan.TransactionID, version, tx)
		if err != nil || !ok {
			t.Fatal(err)
		}
		if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "op", IdempotencyKey: "id"}); !IsFailure(err, FailureRepairRequired) {
			t.Fatal(err)
		}
		recovered, err := env.registry.RecoverTransaction(context.Background(), plan.TransactionID)
		if !IsFailure(err, FailureRepairRequired) || transactionState(recovered) != StateNeedsReview || lastIdempotencyKey(recovered) != "id" {
			t.Fatalf("recover: %#v %v", recovered, err)
		}
		again, err := env.registry.RecoverTransaction(context.Background(), plan.TransactionID)
		if err != nil || transactionState(again) != StateNeedsReview {
			t.Fatal(err)
		}
	})
	t.Run("store load error", func(t *testing.T) {
		env, plan := prepare(t)
		env.registry.transactions = &faultStore{base: env.store, getErr: errors.New("disk")}
		if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "op", IdempotencyKey: "id"}); !IsFailure(err, FailurePlanStale) {
			t.Fatal(err)
		}
		if _, err := env.registry.GetTransaction(context.Background(), plan.TransactionID); !IsFailure(err, FailureUnavailable) {
			t.Fatal(err)
		}
	})
	t.Run("store CAS error", func(t *testing.T) {
		env, plan := prepare(t)
		env.registry.transactions = &faultStore{base: env.store, casErr: errors.New("disk")}
		if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "op", IdempotencyKey: "id"}); !IsFailure(err, FailureUnavailable) {
			t.Fatal(err)
		}
	})
}

func TestResolverSearchOrderingAndHelperEdges(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	// Source ordering covers both the priority and stable source-id branches.
	_ = env.registry.ConfigureSource(RegistrySource{SourceID: "z", Kind: SourceOfficial, EndpointRef: "z", TrustPolicyID: "official", Priority: 1, Connected: false})
	_ = env.registry.ConfigureSource(RegistrySource{SourceID: "a", Kind: SourceOfficial, EndpointRef: "a", TrustPolicyID: "official", Priority: 1, Connected: false})
	sources := env.registry.Sources()
	if len(sources) != 3 || sources[0].SourceID != "official" || sources[1].SourceID != "a" {
		t.Fatalf("source order: %#v", sources)
	}

	// Disconnected, missing-client and client-error sources fail closed.
	env.registry.RegisterClient(SourceOfficial, nil)
	if _, err := env.registry.Search(context.Background(), SearchInput{Query: "none"}); !IsFailure(err, FailureUnavailable) {
		t.Fatal(err)
	}
	env.registry.RegisterClient(SourceOfficial, env.client)
	env.client.searchErr = errors.New("offline")
	if _, err := env.registry.Search(context.Background(), SearchInput{Query: "none"}); !IsFailure(err, FailureUnavailable) {
		t.Fatal(err)
	}
	env.client.searchErr = nil

	data := []byte("pack")
	one := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "sort", Version: "1.0.0"}, data, nil)
	two := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "sort", Version: "2.0.0"}, data, nil)
	addRelease(env, one, data)
	addRelease(env, two, data)
	resolved := map[string]PlanArtifact{"pack:sort": {Release: one}}
	if err := env.registry.resolve(context.Background(), two.Identity, false, resolved, map[string]bool{}); !IsFailure(err, FailureDependencyConflict) {
		t.Fatal(err)
	}
	if err := env.registry.resolve(context.Background(), ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "missing", Version: "1.0.0"}, false, map[string]PlanArtifact{}, map[string]bool{}); !IsFailure(err, FailureUnavailable) {
		t.Fatal(err)
	}
	if _, err := env.registry.resolveVersion(context.Background(), Dependency{CanonicalID: "sort", VersionRange: ">=9.0.0"}, false); !IsFailure(err, FailureDependencyConflict) {
		t.Fatal(err)
	}
	env.client.downloadErr = errors.New("download")
	env.registry.verifiedCache = map[string]verifiedCacheEntry{}
	if err := env.registry.resolve(context.Background(), one.Identity, false, map[string]PlanArtifact{}, map[string]bool{}); !IsFailure(err, FailureUnavailable) && !IsFailure(err, FailureDependencyConflict) {
		t.Fatal(err)
	}
	env.client.downloadErr = nil
	env.client.bytes[one.Digest] = []byte("corrupt")
	env.registry.verifiedCache = map[string]verifiedCacheEntry{}
	if err := env.registry.resolve(context.Background(), one.Identity, false, map[string]PlanArtifact{}, map[string]bool{}); !IsFailure(err, FailureUnavailable) {
		t.Fatal(err)
	}

	if containsString([]string{"a"}, "b") || !containsString([]string{"a", "b"}, "b") {
		t.Fatal("containsString")
	}
	if transactionVersion(Transaction{}) != 0 || transactionState(Transaction{}) != "" || lastIdempotencyKey(Transaction{}) != "" {
		t.Fatal("empty transaction helpers")
	}
	current := Transaction{Plan: InstallPlan{TransactionID: "x", PlanDigest: "p"}, Events: []TransactionEvent{{State: StatePlanned, Sequence: 1}}}
	if err := validateTransactionAppend(current, current); err == nil {
		t.Fatal("non append")
	}
	changed := cloneTransaction(current)
	changed.Plan.PlanDigest = "other"
	changed.Events = append(changed.Events, TransactionEvent{State: StateVerified, Sequence: 2})
	if err := validateTransactionAppend(current, changed); err == nil {
		t.Fatal("plan mutation")
	}
	badSequence := cloneTransaction(current)
	badSequence.Events = append(badSequence.Events, TransactionEvent{State: StateVerified, Sequence: 9})
	if err := validateTransactionAppend(current, badSequence); err == nil {
		t.Fatal("sequence")
	}
	badState := cloneTransaction(current)
	badState.Events = append(badState.Events, TransactionEvent{State: StateCommitted, Sequence: 2})
	if err := validateTransactionAppend(current, badState); err == nil {
		t.Fatal("transition")
	}
}

func TestDiskStoreCancellationCorruptionAndCAS(t *testing.T) {
	root := t.TempDir()
	store, err := NewDiskTransactionStore(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := store.GetRegistryTransaction(ctx, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, ok, err := store.GetRegistryTransaction(context.Background(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil || ok {
		t.Fatalf("missing: %v %v", ok, err)
	}
	if err := store.CreateRegistryTransaction(context.Background(), Transaction{}); err == nil {
		t.Fatal("invalid create")
	}
	lockPath := filepath.Join(root, "transactions.lock")
	if err := os.WriteFile(lockPath, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateRegistryTransaction(ctx, Transaction{Plan: InstallPlan{TransactionID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, Events: []TransactionEvent{{State: StatePlanned, Sequence: 1}}}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	id := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	tx := Transaction{Plan: testFinalizedPlan(t, id), Events: []TransactionEvent{{State: StateAwaitingConfirmation, Sequence: 1}}}
	if err := store.CreateRegistryTransaction(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.CompareAndSwapRegistryTransaction(context.Background(), "cccccccccccccccccccccccccccccccc", 1, Transaction{Plan: InstallPlan{TransactionID: "cccccccccccccccccccccccccccccccc"}}); err != nil || ok {
		t.Fatalf("missing CAS: %v %v", ok, err)
	}
	if ok, err := store.CompareAndSwapRegistryTransaction(context.Background(), id, 9, tx); err != nil || ok {
		t.Fatalf("version CAS: %v %v", ok, err)
	}
	next := cloneTransaction(tx)
	next.Events = append(next.Events, TransactionEvent{State: StateApplying, Sequence: 2})
	if ok, err := store.CompareAndSwapRegistryTransaction(context.Background(), id, 1, next); err != nil || !ok {
		t.Fatalf("CAS: %v %v", ok, err)
	}
	loaded, ok, err := store.GetRegistryTransaction(context.Background(), id)
	if err != nil || !ok || transactionState(loaded) != StateApplying {
		t.Fatalf("load: %#v %v", loaded, err)
	}
	if err := os.WriteFile(store.path(id), []byte(`{"plan":{"transaction_id":"other"},"events":[{"state":"planned","sequence":1}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.GetRegistryTransaction(context.Background(), id); err == nil {
		t.Fatal("identity mismatch")
	}
	if err := os.WriteFile(store.path(id), []byte(`bad`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompareAndSwapRegistryTransaction(context.Background(), id, 2, next); err == nil {
		t.Fatal("corrupt CAS")
	}
}

func TestCriticalFinalizedPlanRejectsHostImpactDocumentCorruption(t *testing.T) {
	t.Run("disk read", func(t *testing.T) {
		store, err := NewDiskTransactionStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		env := newTestEnv(t, store)
		data := []byte("disk-impact-corruption")
		release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/disk-impact-corruption", Version: "1.0.0"}, data, nil)
		addRelease(env, release, data)
		plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, release.Identity))
		if err != nil {
			t.Fatal(err)
		}
		tx, ok, err := store.GetRegistryTransaction(context.Background(), plan.TransactionID)
		if err != nil || !ok {
			t.Fatal(err)
		}
		tx.Plan.HostOperationImpacts[0].ResourceScope.DocumentID = "replacement-document"
		encoded, err := json.Marshal(tx)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(store.path(plan.TransactionID), encoded, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.GetRegistryTransaction(context.Background(), plan.TransactionID); err == nil {
			t.Fatal("Disk store accepted a DocumentID-only impact mutation")
		}
		if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "disk-corrupt", IdempotencyKey: "disk-corrupt"}); err == nil || env.runtime.calls.Load() != 0 {
			t.Fatalf("corrupt Disk plan reached Runtime: %v", err)
		}
		if _, err := env.registry.RecoverTransaction(context.Background(), plan.TransactionID); err == nil || env.runtime.calls.Load() != 0 {
			t.Fatalf("corrupt Disk plan reached recovery Runtime: %v", err)
		}
	})

	t.Run("CAS current and next", func(t *testing.T) {
		for _, corruptCurrent := range []bool{true, false} {
			name := "next"
			if corruptCurrent {
				name = "current"
			}
			t.Run(name, func(t *testing.T) {
				store := NewMemoryTransactionStore()
				env := newTestEnv(t, store)
				data := []byte("cas-impact-corruption-" + name)
				release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/cas-impact-corruption-" + name, Version: "1.0.0"}, data, nil)
				addRelease(env, release, data)
				plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, release.Identity))
				if err != nil {
					t.Fatal(err)
				}
				current, _, _ := store.GetRegistryTransaction(context.Background(), plan.TransactionID)
				next := cloneTransaction(current)
				next.Events = append(next.Events, TransactionEvent{State: StateApplying, Sequence: transactionVersion(next) + 1, IdempotencyKey: "corrupt"})
				if corruptCurrent {
					store.mu.Lock()
					corrupt := store.transactions[plan.TransactionID]
					corrupt.Plan.HostOperationImpacts[0].ResourceScope.DocumentID = "replacement-document"
					store.transactions[plan.TransactionID] = corrupt
					store.mu.Unlock()
				} else {
					next.Plan.HostOperationImpacts[0].ResourceScope.DocumentID = "replacement-document"
				}
				if ok, err := store.CompareAndSwapRegistryTransaction(context.Background(), plan.TransactionID, transactionVersion(current), next); err == nil || ok {
					t.Fatalf("CAS accepted %s DocumentID-only impact mutation: %v", name, err)
				}
				if env.runtime.calls.Load() != 0 {
					t.Fatal("corrupt CAS plan reached Runtime")
				}
			})
		}
	})
}

func TestCriticalFinalizedPlanIntegrityBranches(t *testing.T) {
	rebind := func(plan InstallPlan) InstallPlan {
		plan.PlanDigest = digestPlan(plan)
		plan.ProjectMutationPlan.PlanDigest = plan.PlanDigest
		return plan
	}
	rebindDecision := func(plan *InstallPlan) {
		decision := &plan.AccessDecision
		decision.DecisionDigest = protocolcommon.Digest(digestJSON(struct {
			Evaluation protocolcommon.Digest                   `json:"evaluation"`
			Outcome    accessprotocol.AuthoringDecisionOutcome `json:"outcome"`
			Missing    []semantic.AuthoringCapability          `json:"missing"`
			Violations []accessprotocol.ConstraintViolation    `json:"violations"`
		}{decision.EvaluationDigest, decision.Outcome, decision.MissingCapabilities, decision.ConstraintViolations}))
	}
	base := testFinalizedPlan(t, "integrity-plan")

	inconsistent := base
	inconsistent.ProjectMutationPlan.EvaluationDigest = testDigest('x')
	inconsistent = rebind(inconsistent)
	if validateFinalizedPlan(inconsistent) == nil {
		t.Fatal("inconsistent owner bindings passed finalized plan validation")
	}

	invalidImpact := clonePlan(base)
	invalidImpact.HostOperationImpacts[0].ResourceScope.DocumentID = "replacement-document"
	invalidImpact = rebind(invalidImpact)
	if validateFinalizedPlan(invalidImpact) == nil {
		t.Fatal("invalid closed impact passed a recomputed plan digest")
	}

	mutations := map[string]func(*InstallPlan){
		"outer mutation digest":         func(p *InstallPlan) { p.MutationDigest = testDigest('6') },
		"mutation plan mutation digest": func(p *InstallPlan) { p.ProjectMutationPlan.MutationDigest = testDigest('6') },
		"outer base revision":           func(p *InstallPlan) { p.BaseRevision = "other" },
		"mutation plan base revision":   func(p *InstallPlan) { p.ProjectMutationPlan.BaseProjectRevision = "other" },
		"rollback base revision":        func(p *InstallPlan) { p.RollbackCheckpoint.BaseProjectRevision = "other" },
		"outer definition hash":         func(p *InstallPlan) { p.ExpectedDefinitionHash = testDigest('6') },
		"mutation plan definition hash": func(p *InstallPlan) { p.ProjectMutationPlan.ExpectedDefinitionHash = testDigest('6') },
		"rollback definition hash":      func(p *InstallPlan) { p.RollbackCheckpoint.BaseDefinitionHash = testDigest('6') },
		"outer authoring body": func(p *InstallPlan) {
			p.AuthoringImpact.ResultingDefinitionHash = protocolcommon.Digest(testDigest('6'))
		},
		"mutation plan authoring body": func(p *InstallPlan) {
			p.ProjectMutationPlan.AuthoringImpact.SemanticDiffHash = protocolcommon.Digest(testDigest('6'))
		},
		"outer authoring body digest": func(p *InstallPlan) {
			p.AuthoringImpact.ImpactDigest = protocolcommon.Digest(testDigest('6'))
		},
		"mutation authoring body digest": func(p *InstallPlan) {
			p.ProjectMutationPlan.AuthoringImpact.ImpactDigest = protocolcommon.Digest(testDigest('6'))
		},
		"outer authoring impact missing":      func(p *InstallPlan) { p.AuthoringImpact = nil },
		"outer authoring digest list missing": func(p *InstallPlan) { p.AuthoringImpactDigests = nil },
		"outer authoring digest duplicated": func(p *InstallPlan) {
			p.AuthoringImpactDigests = append(p.AuthoringImpactDigests, p.AuthoringImpactDigests[0])
		},
		"mutation authoring digest": func(p *InstallPlan) { p.ProjectMutationPlan.AuthoringImpactDigest = testDigest('6') },
		"outer lock digest":         func(p *InstallPlan) { p.ExpectedResolvedLockDigest = testDigest('6') },
		"mutation plan lock digest": func(p *InstallPlan) { p.ProjectMutationPlan.ExpectedResolvedLockDigest = testDigest('6') },
		"snapshot lock digest":      func(p *InstallPlan) { p.DependencySnapshot.ResolvedLockDigest = testDigest('6') },
		"rollback lock digest":      func(p *InstallPlan) { p.RollbackCheckpoint.BaseResolvedLockDigest = testDigest('6') },
		"outer lock delta":          func(p *InstallPlan) { p.ResolvedLockDelta.Added = append(p.ResolvedLockDelta.Added, LockedArtifact{}) },
		"mutation plan lock delta": func(p *InstallPlan) {
			p.ProjectMutationPlan.ResolvedLockDelta.Added = append(p.ProjectMutationPlan.ResolvedLockDelta.Added, LockedArtifact{})
		},
		"outer evaluation digest":           func(p *InstallPlan) { p.EvaluationDigest = testDigest('6') },
		"mutation evaluation digest":        func(p *InstallPlan) { p.ProjectMutationPlan.EvaluationDigest = testDigest('6') },
		"decision evaluation digest":        func(p *InstallPlan) { p.AccessDecision.EvaluationDigest = protocolcommon.Digest(testDigest('6')) },
		"decision authoring digest missing": func(p *InstallPlan) { p.AccessDecision.AuthoringImpactDigest = nil },
		"decision authoring digest wrong": func(p *InstallPlan) {
			value := protocolcommon.Digest(testDigest('6'))
			p.AccessDecision.AuthoringImpactDigest = &value
		},
		"decision host digest missing": func(p *InstallPlan) { p.AccessDecision.HostOperationImpactDigests = nil },
		"decision host digest wrong": func(p *InstallPlan) {
			p.AccessDecision.HostOperationImpactDigests[0] = protocolcommon.Digest(testDigest('6'))
		},
		"decision host digest duplicated": func(p *InstallPlan) {
			p.AccessDecision.HostOperationImpactDigests = append(p.AccessDecision.HostOperationImpactDigests, p.AccessDecision.HostOperationImpactDigests[0])
		},
		"outer required capabilities": func(p *InstallPlan) { p.RequiredCapabilities = append(p.RequiredCapabilities, "view:write") },
		"decision required capability missing": func(p *InstallPlan) {
			p.AccessDecision.RequiredCapabilities = p.AccessDecision.RequiredCapabilities[:1]
		},
		"decision required capability extra": func(p *InstallPlan) {
			p.AccessDecision.RequiredCapabilities = append(p.AccessDecision.RequiredCapabilities, semantic.AuthoringCapabilityViewWrite)
		},
		"decision required capability order": func(p *InstallPlan) {
			p.AccessDecision.RequiredCapabilities[0], p.AccessDecision.RequiredCapabilities[1] = p.AccessDecision.RequiredCapabilities[1], p.AccessDecision.RequiredCapabilities[0]
		},
		"allow decision missing capability": func(p *InstallPlan) {
			p.AccessDecision.MissingCapabilities = []semantic.AuthoringCapability{semantic.AuthoringCapabilitySchemaWrite}
			rebindDecision(p)
		},
		"allow decision constraint": func(p *InstallPlan) {
			p.AccessDecision.ConstraintViolations = []accessprotocol.ConstraintViolation{{Action: "update", Code: "denied"}}
			rebindDecision(p)
		},
		"allow decision approval": func(p *InstallPlan) { p.AccessDecision.ApprovalRuleRefs = []string{"approval"} },
		"decision digest":         func(p *InstallPlan) { p.AccessDecision.DecisionDigest = protocolcommon.Digest(testDigest('6')) },
		"decision outcome": func(p *InstallPlan) {
			p.AccessDecision.Outcome = accessprotocol.AuthoringDecisionOutcomeDeny
			rebindDecision(p)
		},
	}
	for name, mutate := range mutations {
		t.Run("recomputed digest rejects "+name, func(t *testing.T) {
			plan := clonePlan(base)
			mutate(&plan)
			plan = rebind(plan)
			if validateFinalizedPlan(plan) == nil {
				t.Fatalf("recomputed PlanDigest blessed contradictory %s", name)
			}
		})
	}
	for name, replacement := range map[string]struct {
		action string
		refs   []string
	}{
		"action": {action: "create", refs: []string{base.RequestedRoot.CanonicalID}},
		"root":   {action: "update", refs: []string{"other/package"}},
	} {
		t.Run("valid host descriptor rejects wrong "+name, func(t *testing.T) {
			plan := clonePlan(base)
			host, err := accesscore.HostOperationImpact(accessprotocol.HostOperationKindPackageTransaction, replacement.action, plan.HostOperationImpacts[0].ResourceScope, replacement.refs)
			if err != nil {
				t.Fatal(err)
			}
			plan.HostOperationImpacts[0] = host
			plan.HostOperationImpactDigest = string(host.ImpactDigest)
			plan.ProjectMutationPlan.HostOperationImpactDigest = string(host.ImpactDigest)
			plan.AccessDecision.HostOperationImpactDigests = []protocolcommon.Digest{host.ImpactDigest}
			plan = rebind(plan)
			if validateFinalizedPlan(plan) == nil {
				t.Fatalf("valid re-sealed host descriptor escaped %s binding", name)
			}
		})
	}

	current := Transaction{Plan: base, Events: []TransactionEvent{{State: StateAwaitingConfirmation, Sequence: 1}}}
	next := cloneTransaction(current)
	next.Events = append(next.Events, TransactionEvent{State: StateApplying, Sequence: 2})
	corruptCurrent := cloneTransaction(current)
	corruptCurrent.Plan.HostOperationImpacts[0].ResourceScope.DocumentID = "replacement-document"
	if validateTransactionAppend(corruptCurrent, next) == nil {
		t.Fatal("append accepted a corrupt current finalized plan")
	}
	corruptNext := cloneTransaction(next)
	corruptNext.Plan.HostOperationImpacts[0].ResourceScope.DocumentID = "replacement-document"
	if validateTransactionAppend(current, corruptNext) == nil {
		t.Fatal("append accepted a corrupt next finalized plan")
	}
	changedNext := cloneTransaction(next)
	changedNext.Plan.RuntimeSessionID = "changed-session"
	changedNext.Plan = rebind(changedNext.Plan)
	if validateTransactionAppend(current, changedNext) == nil {
		t.Fatal("append accepted a rebound plan outside finalization")
	}
	invalidTransition := cloneTransaction(current)
	invalidTransition.Events = append(invalidTransition.Events, TransactionEvent{State: StateCommitted, Sequence: 2})
	if validateTransactionAppend(current, invalidTransition) == nil {
		t.Fatal("append accepted an invalid finalized transition")
	}

	disk, err := NewDiskTransactionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	diskID := "dddddddddddddddddddddddddddddddd"
	diskPlan := testFinalizedPlan(t, diskID)
	corruptDiskPlan := clonePlan(diskPlan)
	corruptDiskPlan.HostOperationImpacts[0].ResourceScope.DocumentID = "replacement-document"
	if err := disk.CreateRegistryTransaction(context.Background(), Transaction{Plan: corruptDiskPlan, Events: []TransactionEvent{{State: StateAwaitingConfirmation, Sequence: 1}}}); err == nil {
		t.Fatal("Disk create accepted a corrupt finalized plan")
	}
	diskCurrent := Transaction{Plan: diskPlan, Events: []TransactionEvent{{State: StateAwaitingConfirmation, Sequence: 1}}}
	if err := disk.CreateRegistryTransaction(context.Background(), diskCurrent); err != nil {
		t.Fatal(err)
	}
	diskNext := cloneTransaction(diskCurrent)
	diskNext.Events = append(diskNext.Events, TransactionEvent{State: StateApplying, Sequence: 2})
	diskNext.Plan.HostOperationImpacts[0].ResourceScope.DocumentID = "replacement-document"
	if ok, err := disk.CompareAndSwapRegistryTransaction(context.Background(), diskID, 1, diskNext); err == nil || ok {
		t.Fatalf("Disk CAS accepted a corrupt next finalized plan: %v", err)
	}
	invalidDiskTransition := cloneTransaction(diskCurrent)
	invalidDiskTransition.Events = append(invalidDiskTransition.Events, TransactionEvent{State: StateCommitted, Sequence: 2})
	if ok, err := disk.CompareAndSwapRegistryTransaction(context.Background(), diskID, 1, invalidDiskTransition); err == nil || ok {
		t.Fatalf("Disk CAS accepted an invalid finalized transition: %v", err)
	}

	memory := NewMemoryTransactionStore()
	memoryCurrent := Transaction{Plan: testFinalizedPlan(t, "memory-integrity"), Events: []TransactionEvent{{State: StateAwaitingConfirmation, Sequence: 1}}}
	if err := memory.CreateRegistryTransaction(context.Background(), memoryCurrent); err != nil {
		t.Fatal(err)
	}
	memoryNext := cloneTransaction(memoryCurrent)
	memoryNext.Events = append(memoryNext.Events, TransactionEvent{State: StateCommitted, Sequence: 2})
	if ok, err := memory.CompareAndSwapRegistryTransaction(context.Background(), "memory-integrity", 1, memoryNext); err == nil || ok {
		t.Fatalf("Memory CAS accepted an invalid finalized transition: %v", err)
	}

	wrongIdentity := diskCurrent
	wrongIdentity.Plan.TransactionID = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	encoded, _ := json.Marshal(wrongIdentity)
	if err := os.WriteFile(disk.path(diskID), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := disk.getUnlocked(diskID); err == nil {
		t.Fatal("unlocked Disk read accepted a mismatched transaction identity")
	}
	corruptUnlocked := diskCurrent
	corruptUnlocked.Plan.HostOperationImpacts[0].ResourceScope.DocumentID = "replacement-document"
	encoded, _ = json.Marshal(corruptUnlocked)
	if err := os.WriteFile(disk.path(diskID), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := disk.getUnlocked(diskID); err == nil {
		t.Fatal("unlocked Disk read accepted a corrupt finalized plan")
	}
	markerStripped := diskCurrent
	markerStripped.Events = append(markerStripped.Events, TransactionEvent{State: StateRolledBack, Sequence: 2})
	markerStripped.Plan.HostOperationImpacts = nil
	markerStripped.Plan.ProjectMutationPlan.PlanDigest = ""
	encoded, _ = json.Marshal(markerStripped)
	if err := os.WriteFile(disk.path(diskID), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := disk.GetRegistryTransaction(context.Background(), diskID); err == nil {
		t.Fatal("Disk read accepted a finalized rolled-back plan with stripped integrity markers")
	}
	markerStrippedNext := cloneTransaction(markerStripped)
	markerStrippedNext.Events = append(markerStrippedNext.Events, TransactionEvent{State: StateRolledBack, Sequence: 3})
	if ok, err := disk.CompareAndSwapRegistryTransaction(context.Background(), diskID, 2, markerStrippedNext); err == nil || ok {
		t.Fatalf("Disk CAS accepted a finalized rolled-back plan with stripped integrity markers: %v", err)
	}

	readErrorDisk, err := NewDiskTransactionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	readErrorID := "ffffffffffffffffffffffffffffffff"
	if err := os.Mkdir(readErrorDisk.path(readErrorID), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readErrorDisk.GetRegistryTransaction(context.Background(), readErrorID); err == nil {
		t.Fatal("Disk read error was ignored")
	}

	t.Run("Plan retry and recovery reject in-memory corruption", func(t *testing.T) {
		env := newTestEnv(t, NewMemoryTransactionStore())
		data := []byte("memory-plan-corruption")
		release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/memory-plan-corruption", Version: "1.0.0"}, data, nil)
		addRelease(env, release, data)
		request := planRequest(env, ActionInstall, release.Identity)
		plan, err := env.registry.Plan(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		memoryStore := env.store.(*MemoryTransactionStore)
		memoryStore.mu.Lock()
		corrupt := memoryStore.transactions[plan.TransactionID]
		corrupt.Plan.HostOperationImpacts[0].ResourceScope.DocumentID = "replacement-document"
		memoryStore.transactions[plan.TransactionID] = corrupt
		memoryStore.mu.Unlock()
		if _, err := env.registry.Plan(context.Background(), request); !IsFailure(err, FailureUnavailable) {
			t.Fatalf("Plan retry accepted an in-memory impact mutation: %v", err)
		}

		recoveryEnv := newTestEnv(t, NewMemoryTransactionStore())
		data = []byte("memory-recovery-corruption")
		release = signedRelease(t, recoveryEnv.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/memory-recovery-corruption", Version: "1.0.0"}, data, nil)
		addRelease(recoveryEnv, release, data)
		plan, err = recoveryEnv.registry.Plan(context.Background(), planRequest(recoveryEnv, ActionInstall, release.Identity))
		if err != nil {
			t.Fatal(err)
		}
		recoveryStore := recoveryEnv.store.(*MemoryTransactionStore)
		recoveryStore.mu.Lock()
		corrupt = recoveryStore.transactions[plan.TransactionID]
		corrupt.Events = append(corrupt.Events, TransactionEvent{State: StateApplying, Sequence: transactionVersion(corrupt) + 1, IdempotencyKey: "corrupt"})
		corrupt.Plan.HostOperationImpacts[0].ResourceScope.DocumentID = "replacement-document"
		recoveryStore.transactions[plan.TransactionID] = corrupt
		recoveryStore.mu.Unlock()
		if _, err := recoveryEnv.registry.RecoverTransaction(context.Background(), plan.TransactionID); !IsFailure(err, FailureUnavailable) || recoveryEnv.runtime.calls.Load() != 0 {
			t.Fatalf("Recovery accepted an in-memory impact mutation: %v", err)
		}
	})
}

func TestCriticalMemoryReadsAndFinalizedHistoryFailClosed(t *testing.T) {
	ctx := context.Background()
	id := "abababababababababababababababab"
	valid := Transaction{Plan: testFinalizedPlan(t, id), Events: []TransactionEvent{{State: StateCommitted, Sequence: 1}}}
	corrupt := cloneTransaction(valid)
	corrupt.Plan.HostOperationImpacts[0].ResourceScope.DocumentID = "replacement-document"

	memory := NewMemoryTransactionStore()
	if err := memory.CreateRegistryTransaction(ctx, corrupt); err == nil {
		t.Fatal("Memory create accepted a corrupt finalized transaction")
	}
	memory.mu.Lock()
	memory.transactions[id] = corrupt
	memory.mu.Unlock()
	if _, _, err := memory.GetRegistryTransaction(ctx, id); err == nil {
		t.Fatal("Memory get returned a corrupt finalized transaction")
	}

	for _, operation := range []string{"Transaction", "GetTransaction", "RecoverTransaction"} {
		t.Run(operation, func(t *testing.T) {
			env := newTestEnv(t, NewMemoryTransactionStore())
			env.registry.transactions = &commitRaceStore{first: corrupt, latest: corrupt}
			switch operation {
			case "Transaction":
				if _, ok := env.registry.Transaction(id); ok {
					t.Fatal("Transaction returned corrupt finalized state")
				}
			case "GetTransaction":
				if _, err := env.registry.GetTransaction(ctx, id); !IsFailure(err, FailureUnavailable) {
					t.Fatalf("GetTransaction returned corrupt finalized state: %v", err)
				}
			case "RecoverTransaction":
				if _, err := env.registry.RecoverTransaction(ctx, id); !IsFailure(err, FailureUnavailable) || env.runtime.calls.Load() != 0 {
					t.Fatalf("non-applying recovery returned corrupt finalized state: %v", err)
				}
			}
		})
	}

	stripped := Transaction{
		Plan: InstallPlan{TransactionID: id, PlanDigest: "planning", Action: ActionUpdate, RequestedRoot: valid.Plan.RequestedRoot},
		Events: []TransactionEvent{
			{State: StateVerified, Sequence: 1},
			{State: StateRolledBack, Sequence: 2},
		},
	}
	if !transactionRequiresFinalizedPlan(stripped) || validateStoredTransaction(stripped) == nil {
		t.Fatal("finalized event history was downgraded by stripping plan markers")
	}
	if err := NewMemoryTransactionStore().CreateRegistryTransaction(ctx, stripped); err == nil {
		t.Fatal("Memory create accepted marker-stripped finalized history")
	}

	disk, err := NewDiskTransactionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(stripped)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(disk.path(id), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := disk.GetRegistryTransaction(ctx, id); err == nil {
		t.Fatal("Disk get accepted marker-stripped finalized history")
	}
	if ok, err := disk.CompareAndSwapRegistryTransaction(ctx, id, 2, stripped); err == nil || ok {
		t.Fatalf("Disk CAS accepted marker-stripped finalized history: %v", err)
	}

	pmBody := Transaction{Plan: InstallPlan{TransactionID: id, PlanDigest: "planning", ProjectMutationPlan: ProjectMutationPlan{StagedTreeManifest: testDigest('6')}}, Events: []TransactionEvent{{State: StateRolledBack, Sequence: 1}}}
	if !planHasFinalizedBody(pmBody.Plan) || validateStoredTransaction(pmBody) == nil {
		t.Fatal("nonempty mutation-plan body was not treated as finalized")
	}
}

func TestRemainingNormativeBranches(t *testing.T) {
	template := newTestEnv(t, NewMemoryTransactionStore())
	constructed, err := New(template.validator, template.access, template.runtime, template.project, template.registry.credentials, NewMemoryTransactionStore())
	if err != nil || constructed.now().IsZero() {
		t.Fatal(err)
	}
	releases := []ArtifactRelease{
		{Identity: ArtifactIdentity{CanonicalID: "b", Version: "1.0.0"}, SourceID: "b"},
		{Identity: ArtifactIdentity{CanonicalID: "a", Version: "1.0.0"}, SourceID: "z"},
		{Identity: ArtifactIdentity{CanonicalID: "a", Version: "2.0.0"}, SourceID: "a"},
		{Identity: ArtifactIdentity{CanonicalID: "a", Version: "2.0.0"}, SourceID: "z"},
	}
	sortReleases(releases, []RegistrySource{{SourceID: "z", Priority: 2}, {SourceID: "a", Priority: 1}})
	if releases[0].SourceID != "z" || releases[3].Identity.CanonicalID != "b" {
		t.Fatal(releases)
	}
	if compareVersions("1.0.0-alpha", "1.0.0") >= 0 || matchesRange("1.0.0", "^bad", false) {
		t.Fatal("semver residual")
	}

	env := newTestEnv(t, NewMemoryTransactionStore())
	data := []byte("unsigned")
	release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "unsigned", Version: "1.0.0"}, data, nil)
	release.Signature = nil
	addRelease(env, release, data)
	if _, err := env.registry.Search(context.Background(), SearchInput{Query: "unsigned"}); !IsFailure(err, FailureUnavailable) {
		t.Fatal(err)
	}

	env = newTestEnv(t, NewMemoryTransactionStore())
	data = []byte("authored")
	release = signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "authored", Version: "1.0.0"}, data, nil)
	env.registry.RegisterPackageAuthor(authorPort{result: AuthoredArtifact{Release: release, Bytes: data}})
	env.validator.mismatch = true
	if _, err := env.registry.AuthorArtifact(context.Background(), AuthorArtifactRequest{Kind: ArtifactPack, ProjectID: "p", OutputName: "x.ldpack", PublisherID: "layerdraw", Version: "1.0.0"}); !IsFailure(err, FailureArtifactCorrupt) {
		t.Fatal(err)
	}

	env = newTestEnv(t, NewMemoryTransactionStore())
	oldData, newData := []byte("old"), []byte("new")
	old := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "repair", Version: "1.0.0"}, oldData, nil)
	newRelease := signedRelease(t, env.privateKey, old.Identity, newData, nil)
	env.project.state.DependencySnapshot = ProjectDependencySnapshot{ResolvedLockDigest: testDigest('d'), Installs: []LockedArtifact{{Identity: old.Identity, SourceID: "official", Digest: old.Digest}}}
	addRelease(env, newRelease, newData)
	if _, err := env.registry.Plan(context.Background(), planRequest(env, ActionRepair, old.Identity)); !IsFailure(err, FailureRepairRequired) {
		t.Fatal(err)
	}

	env = newTestEnv(t, NewMemoryTransactionStore())
	release = signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "migration", Version: "1.0.0"}, data, nil)
	release.Compatibility = []CompatibilityDecision{{Subject: "schema", Status: "migration_required"}}
	addRelease(env, release, data)
	env.validator.migration = true
	plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, release.Identity))
	if err != nil || !plan.MigrationRequired {
		t.Fatalf("migration: %#v %v", plan, err)
	}

	env = newTestEnv(t, NewMemoryTransactionStore())
	release = signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "versioned", Version: "2.0.0"}, data, nil)
	addRelease(env, release, data)
	if err := env.registry.resolve(context.Background(), ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "versioned", Version: "1.0.0"}, false, map[string]PlanArtifact{}, map[string]bool{}); !IsFailure(err, FailureDependencyConflict) {
		t.Fatal(err)
	}
	pre := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "pre", Version: "1.0.0-beta"}, data, nil)
	addRelease(env, pre, data)
	if err := env.registry.resolve(context.Background(), ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "pre", Version: "latest"}, false, map[string]PlanArtifact{}, map[string]bool{}); !IsFailure(err, FailureDependencyConflict) {
		t.Fatal(err)
	}

	current := Transaction{Plan: testFinalizedPlan(t, "tx"), Events: []TransactionEvent{{State: StateRepairRequired, Sequence: 1}}}
	next := cloneTransaction(current)
	next.Events = append(next.Events, TransactionEvent{State: StateNeedsReview, Sequence: 2})
	if err := validateTransactionAppend(current, next); err != nil {
		t.Fatal(err)
	}
}

func TestCriticalAggregateMutationAndDurableStages(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	env.validator.aggregateCapabilities = []semantic.AuthoringCapability{semantic.AuthoringCapabilitySchemaWrite, semantic.AuthoringCapabilityQueryWrite, semantic.AuthoringCapabilityViewWrite}
	childBytes, rootBytes := []byte("child-critical"), []byte("root-critical")
	child := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/child", Version: "1.0.0"}, childBytes, nil)
	root := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/root", Version: "1.0.0"}, rootBytes, []Dependency{{Kind: ArtifactPack, CanonicalID: child.Identity.CanonicalID, VersionRange: "1.0.0", DigestConstraint: child.Digest}})
	addRelease(env, child, childBytes)
	addRelease(env, root, rootBytes)
	plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, root.Identity))
	if err != nil {
		t.Fatal(err)
	}
	wantCaps := []string{"package:manage", "query:write", "schema:write", "view:write"}
	if !reflect.DeepEqual(plan.RequiredCapabilities, wantCaps) {
		t.Fatalf("aggregate capabilities=%v", plan.RequiredCapabilities)
	}
	if len(plan.AuthoringImpactDigests) != 1 || plan.AuthoringImpactDigests[0] != plan.ProjectMutationPlan.AuthoringImpactDigest || len(plan.Artifacts) != 2 {
		t.Fatalf("root-only impact leaked: %#v", plan)
	}
	tx, _ := env.registry.Transaction(plan.TransactionID)
	wantStates := []TransactionState{StatePlanned, StateDownloading, StateVerified, StateExpandedStaged, StateCompiled, StateAwaitingConfirmation}
	got := make([]TransactionState, len(tx.Events))
	for i, event := range tx.Events {
		got[i] = event.State
	}
	if !reflect.DeepEqual(got, wantStates) {
		t.Fatalf("durable stages=%v", got)
	}
	if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "aggregate", IdempotencyKey: "aggregate-id"}); err != nil {
		t.Fatal(err)
	}
	if env.runtime.last.MutationPlan.AuthoringImpactDigest != plan.ProjectMutationPlan.AuthoringImpactDigest || len(env.runtime.last.AuthoringImpact.RequiredCapabilities) != 3 {
		t.Fatal("Runtime did not receive aggregate Engine mutation")
	}
}

func TestCriticalHostOwnedSourceAndProbeCAS(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	input := RegistrySource{SourceID: "host-owned", Kind: SourceOfficial, EndpointRef: "registry:host", TrustPolicyID: "official", CachePolicy: "verified", Connected: true, AuthConnectionRef: "caller-secret", Revision: 99}
	if err := env.registry.ConfigureSource(input); err != nil {
		t.Fatal(err)
	}
	stored, _ := env.registry.getSource("host-owned")
	if stored.Connected || stored.AuthConnectionRef != "" || stored.Revision != 1 {
		t.Fatalf("caller connection accepted: %#v", stored)
	}
	env.connector.block = make(chan struct{})
	result := make(chan error, 1)
	go func() { result <- env.registry.ConnectSource(context.Background(), "official", "credential:new") }()
	for env.connector.calls.Load() < 2 {
		time.Sleep(time.Millisecond)
	}
	current, _ := env.registry.getSource("official")
	current.EndpointRef = "registry:changed"
	if err := env.registry.ConfigureSource(current); err != nil {
		t.Fatal(err)
	}
	close(env.connector.block)
	if err := <-result; !IsFailure(err, FailurePlanStale) {
		t.Fatalf("probe race accepted: %v", err)
	}
}

func TestCriticalDependencyImmutabilityDependentRemoveAndOfflineCache(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	data := []byte("cached")
	release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/cached", Version: "1.0.0"}, data, nil)
	addRelease(env, release, data)
	if _, err := env.registry.Search(context.Background(), SearchInput{Query: release.Identity.CanonicalID}); err != nil {
		t.Fatal(err)
	}
	env.client.offline = true
	if found, err := env.registry.Search(context.Background(), SearchInput{Query: release.Identity.CanonicalID}); err != nil || len(found) != 1 || found[0].Trust == nil {
		t.Fatalf("verified offline cache: %#v %v", found, err)
	}
	env.client.offline = false
	env.registry.maxArtifactBytes = 1
	if _, err := env.registry.Search(context.Background(), SearchInput{Query: release.Identity.CanonicalID}); !IsFailure(err, FailureUnavailable) {
		t.Fatal("host byte bound was bypassed by cache")
	}
	env2 := newTestEnv(t, NewMemoryTransactionStore())
	env2.registry.maxArtifactBytes = 1
	addRelease(env2, release, data)
	if _, err := env2.registry.Search(context.Background(), SearchInput{Query: release.Identity.CanonicalID}); !IsFailure(err, FailureUnavailable) {
		t.Fatalf("oversize presented: %v", err)
	}
	rootID := ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/root", Version: "1.0.0"}
	depID := ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/dep", Version: "1.0.0"}
	env.project.state.DependencySnapshot = ProjectDependencySnapshot{ResolvedLockDigest: testDigest('d'), Installs: []LockedArtifact{{Identity: rootID, Dependencies: []ArtifactIdentity{depID}}, {Identity: depID}}}
	if _, err := env.registry.Plan(context.Background(), planRequest(env, ActionRemove, depID)); !IsFailure(err, FailureDependencyConflict) {
		t.Fatalf("dependent remove accepted: %v", err)
	}
}

func TestCriticalTemplateNewDocumentAndAuthoritativeRecovery(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	data := []byte("template-critical")
	release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactTemplate, CanonicalID: "critical/template", Version: "1.0.0"}, data, nil)
	addRelease(env, release, data)
	before := env.project.currentCalls.Load()
	plan, err := env.registry.Plan(context.Background(), PlanRequest{Action: ActionCreateFromTemplate, Requested: release.Identity, DependencySnapshot: ProjectDependencySnapshot{}})
	if err != nil {
		t.Fatalf("%v: %v", err, errors.Unwrap(err))
	}
	if env.project.currentCalls.Load() != before || plan.NewDocumentID != "new-document" || plan.BaseRevision != "" {
		t.Fatalf("template reused existing project: %#v", plan)
	}
	result, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "template", IdempotencyKey: "template-id"})
	if err != nil || result.DocumentID != plan.NewDocumentID || !result.InitialCommittedRevision || env.runtime.initialCalls.Load() != 1 {
		t.Fatalf("template commit: %#v %v", result, err)
	}

	env2 := newTestEnv(t, NewMemoryTransactionStore())
	packBytes := []byte("recover-authoritative")
	pack := signedRelease(t, env2.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/recover", Version: "1.0.0"}, packBytes, nil)
	addRelease(env2, pack, packBytes)
	recoverPlan, err := env2.registry.Plan(context.Background(), planRequest(env2, ActionInstall, pack.Identity))
	if err != nil {
		t.Fatal(err)
	}
	tx, _, _ := env2.store.GetRegistryTransaction(context.Background(), recoverPlan.TransactionID)
	version := transactionVersion(tx)
	intent := RuntimeCommitInput{Plan: recoverPlan, OperationID: "recover-op", IdempotencyKey: "recover-id", MutationPlan: recoverPlan.ProjectMutationPlan}
	tx.RuntimeInput = &intent
	tx.Events = append(tx.Events, TransactionEvent{State: StateApplying, EvidenceDigest: recoverPlan.EvaluationDigest, Sequence: version + 1, IdempotencyKey: "recover-id"})
	ok, err := env2.store.CompareAndSwapRegistryTransaction(context.Background(), recoverPlan.TransactionID, version, tx)
	if err != nil || !ok {
		t.Fatal(err)
	}
	for _, state := range []TransactionState{StateRepairRequired, StateRepairing} {
		version = transactionVersion(tx)
		tx.Events = append(tx.Events, TransactionEvent{State: state, EvidenceDigest: recoverPlan.MutationDigest, Sequence: version + 1, IdempotencyKey: "recover-id"})
		ok, err = env2.store.CompareAndSwapRegistryTransaction(context.Background(), recoverPlan.TransactionID, version, tx)
		if err != nil || !ok {
			t.Fatal(err)
		}
	}
	env2.runtime.recovery = RuntimeRegistryOutcome{Status: RuntimeRegistryCommitted, Result: RuntimeCommitResult{CommittedRevision: "runtime-r2", OperationResultID: "recover-op", DocumentID: "doc"}}
	recovered, err := env2.registry.RecoverTransaction(context.Background(), recoverPlan.TransactionID)
	if err != nil || transactionState(recovered) != StateCommitted || recovered.CommittedRevision != "runtime-r2" || env2.runtime.calls.Load() != 0 {
		t.Fatalf("authoritative recovery: %#v %v", recovered, err)
	}
}

type byteOnlyClient struct{ inner *memoryClient }

func (c byteOnlyClient) Search(ctx context.Context, source RegistrySource, input SearchInput) ([]ArtifactRelease, error) {
	return c.inner.Search(ctx, source, input)
}
func (c byteOnlyClient) Download(ctx context.Context, source RegistrySource, release ArtifactRelease) ([]byte, error) {
	return c.inner.Download(ctx, source, release)
}

type searchOnlyClient struct{ inner *memoryClient }

func (c searchOnlyClient) Search(ctx context.Context, source RegistrySource, input SearchInput) ([]ArtifactRelease, error) {
	return c.inner.Search(ctx, source, input)
}

type commitOnlyRuntime struct{}

func (commitOnlyRuntime) CommitRegistryPlan(context.Context, RuntimeCommitInput) (RuntimeCommitResult, error) {
	return RuntimeCommitResult{}, nil
}

type currentOnlyProject struct{ state ProjectState }

func (p currentOnlyProject) CurrentRegistryProjectState(context.Context, string) (ProjectState, error) {
	return p.state, nil
}

type invalidTemplateRuntime struct{}

func (invalidTemplateRuntime) CommitRegistryPlan(context.Context, RuntimeCommitInput) (RuntimeCommitResult, error) {
	return RuntimeCommitResult{CommittedRevision: "r2", OperationResultID: "invalid-template"}, nil
}
func (invalidTemplateRuntime) CommitInitialRegistryTemplate(context.Context, RuntimeCommitInput) (RuntimeCommitResult, error) {
	return RuntimeCommitResult{CommittedRevision: "r2", OperationResultID: "invalid-template"}, nil
}

func TestCriticalRecoveryIntermediateReplayAndTransportFallbacks(t *testing.T) {
	base := newTestEnv(t, NewMemoryTransactionStore())
	data := []byte("fallback")
	release := signedRelease(t, base.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "fallback", Version: "1.0.0"}, data, nil)
	addRelease(base, release, data)
	source, policy, _, _ := base.registry.sourceContext("official")
	if _, _, _, err := base.registry.fetchValidated(context.Background(), source, policy, byteOnlyClient{base.client}, release); err != nil {
		t.Fatal(err)
	}
	base.registry.verifiedCache = map[string]verifiedCacheEntry{}
	if _, _, _, err := base.registry.fetchValidated(context.Background(), source, policy, searchOnlyClient{base.client}, release); !IsFailure(err, FailureUnavailable) {
		t.Fatalf("missing stream accepted: %v", err)
	}
	for index, state := range []TransactionState{StatePlanned, StateDownloading, StateVerified, StateExpandedStaged, StateCompiled} {
		store := NewMemoryTransactionStore()
		env := newTestEnv(t, store)
		id := fmt.Sprintf("resume-%d", index)
		plan := testFinalizedPlan(t, id)
		plan.ProjectMutationPlan.StagedTreeManifest = testDigest('s')
		plan.PlanDigest = digestPlan(plan)
		plan.ProjectMutationPlan.PlanDigest = plan.PlanDigest
		tx := Transaction{Plan: plan, Events: []TransactionEvent{{State: state, Sequence: 1}}}
		if err := store.CreateRegistryTransaction(context.Background(), tx); err != nil {
			t.Fatal(err)
		}
		resumed, err := env.registry.RecoverTransaction(context.Background(), id)
		if err != nil || transactionState(resumed) != StateAwaitingConfirmation {
			t.Fatalf("resume %s: %#v %v", state, resumed, err)
		}
	}
	makeRepair := func(t *testing.T) (*testEnv, Transaction) {
		env := newTestEnv(t, NewMemoryTransactionStore())
		plan := testFinalizedPlan(t, "repair-tx")
		plan.MutationDigest = testDigest('m')
		plan.ProjectMutationPlan.MutationDigest = plan.MutationDigest
		plan.PlanDigest = digestPlan(plan)
		plan.ProjectMutationPlan.PlanDigest = plan.PlanDigest
		tx := Transaction{Plan: plan, Events: []TransactionEvent{{State: StateRepairRequired, Sequence: 1, IdempotencyKey: "repair-id"}}}
		if err := env.store.CreateRegistryTransaction(context.Background(), tx); err != nil {
			t.Fatal(err)
		}
		return env, tx
	}
	env, _ := makeRepair(t)
	env.runtime.recovery = RuntimeRegistryOutcome{Status: RuntimeRegistrySuperseded, SupersedingRevision: "restore-r3"}
	result, err := env.registry.RecoverTransaction(context.Background(), "repair-tx")
	if err != nil || transactionState(result) != StateSuperseded || result.SupersedingRevision != "restore-r3" {
		t.Fatalf("superseded: %#v %v", result, err)
	}
	env, _ = makeRepair(t)
	env.runtime.recoveryErr = errors.New("lookup")
	result, err = env.registry.RecoverTransaction(context.Background(), "repair-tx")
	if !IsFailure(err, FailureRepairRequired) || transactionState(result) != StateNeedsReview {
		t.Fatalf("lookup failure: %#v %v", result, err)
	}
	env, _ = makeRepair(t)
	env.registry.runtime = commitOnlyRuntime{}
	result, err = env.registry.RecoverTransaction(context.Background(), "repair-tx")
	if !IsFailure(err, FailureRepairRequired) || transactionState(result) != StateNeedsReview {
		t.Fatalf("missing recovery port: %#v %v", result, err)
	}
	env = newTestEnv(t, NewMemoryTransactionStore())
	replayBytes := []byte("replay-fresh")
	replayRelease := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/replay-fresh", Version: "1.0.0"}, replayBytes, nil)
	addRelease(env, replayRelease, replayBytes)
	plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, replayRelease.Identity))
	if err != nil {
		t.Fatal(err)
	}
	intent := RuntimeCommitInput{
		Plan:                 plan,
		OperationID:          "op",
		IdempotencyKey:       "replay-id",
		AccessDecision:       accessprotocol.AuthoringDecision{Outcome: accessprotocol.AuthoringDecisionOutcomeAllow},
		HostOperationImpacts: []accessprotocol.HostOperationImpact{{ImpactDigest: protocolcommon.Digest(testDigest('h'))}},
	}
	tx, _, _ := env.store.GetRegistryTransaction(context.Background(), plan.TransactionID)
	version := transactionVersion(tx)
	tx.RuntimeInput = &intent
	tx.Events = append(tx.Events, TransactionEvent{State: StateApplying, Sequence: version + 1, IdempotencyKey: "replay-id"})
	if ok, err := env.store.CompareAndSwapRegistryTransaction(context.Background(), plan.TransactionID, version, tx); err != nil || !ok {
		t.Fatal(err)
	}
	env.project.mu.Lock()
	env.project.state.LeaseToken = "fresh-recovery-lease"
	env.project.mu.Unlock()
	result, err = env.registry.RecoverTransaction(context.Background(), plan.TransactionID)
	if err != nil || transactionState(result) != StateCommitted || env.runtime.calls.Load() != 1 || env.access.calls.Load() != 2 || env.runtime.last.LeaseToken != "fresh-recovery-lease" {
		t.Fatalf("idempotent replay: %#v %v", result, err)
	}
}

func TestCriticalHelperAndStaleLockBranches(t *testing.T) {
	impact := semantic.AuthoringImpact{ImpactDigest: protocolcommon.Digest(testDigest('i')), RequiredCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilitySchemaWrite}}
	host := accessprotocol.HostOperationImpact{ImpactDigest: protocolcommon.Digest(testDigest('h'))}
	if decisionBindsMutation(accessprotocol.AuthoringDecision{}, impact, host) {
		t.Fatal("unbound decision")
	}
	wrong := protocolcommon.Digest(testDigest('x'))
	decision := accessprotocol.AuthoringDecision{AuthoringImpactDigest: &wrong, HostOperationImpactDigests: []protocolcommon.Digest{host.ImpactDigest}, RequiredCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilityPackageManage, semantic.AuthoringCapabilitySchemaWrite}}
	if decisionBindsMutation(decision, impact, host) {
		t.Fatal("wrong impact")
	}
	decision.AuthoringImpactDigest = &impact.ImpactDigest
	decision.RequiredCapabilities = []semantic.AuthoringCapability{semantic.AuthoringCapabilityPackageManage}
	if decisionBindsMutation(decision, impact, host) {
		t.Fatal("missing aggregate capability")
	}
	root := ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "root"}
	child := ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "child"}
	orphan := ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "orphan"}
	snapshot := ProjectDependencySnapshot{Installs: []LockedArtifact{{Identity: root, Dependencies: []ArtifactIdentity{child}}, {Identity: child, Dependencies: []ArtifactIdentity{root}}, {Identity: orphan}}}
	closure := lockedDependencyClosure(snapshot, root)
	if len(closure) != 2 {
		t.Fatal(closure)
	}
	if len(lockedDependencyClosure(snapshot, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "missing"})) != 0 {
		t.Fatal("missing closure")
	}
	resolved := map[string]PlanArtifact{artifactKey(root): {Release: ArtifactRelease{Identity: root, Digest: "new"}}}
	delta, err := buildLockDelta(ActionUpdate, snapshot, resolved, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Removed) != 1 || delta.Removed[0].Identity != child {
		t.Fatalf("full removed closure=%#v", delta)
	}
	rootDir := t.TempDir()
	store, err := NewDiskTransactionStore(rootDir)
	if err != nil {
		t.Fatal(err)
	}
	metadata, _ := json.Marshal(struct {
		PID        int       `json:"pid"`
		AcquiredAt time.Time `json:"acquired_at"`
	}{999999, testNow.Add(-time.Hour)})
	if err := os.WriteFile(filepath.Join(rootDir, "transactions.lock"), metadata, 0o600); err != nil {
		t.Fatal(err)
	}
	id := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	tx := Transaction{Plan: InstallPlan{TransactionID: id, PlanDigest: "p"}, Events: []TransactionEvent{{State: StatePlanned, Sequence: 1}}}
	if err := store.CreateRegistryTransaction(context.Background(), tx); err != nil {
		t.Fatalf("stale lock not recovered: %v", err)
	}
}

func TestCriticalPlanAndCommitFreshnessBranches(t *testing.T) {
	prepare := func(t *testing.T, kind ArtifactKind) (*testEnv, InstallPlan) {
		t.Helper()
		env := newTestEnv(t, NewMemoryTransactionStore())
		data := []byte("freshness")
		release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: kind, CanonicalID: "critical/freshness", Version: "1.0.0"}, data, nil)
		addRelease(env, release, data)
		action := ActionInstall
		request := planRequest(env, action, release.Identity)
		if kind == ArtifactTemplate {
			request = PlanRequest{Action: ActionCreateFromTemplate, Requested: release.Identity, DependencySnapshot: ProjectDependencySnapshot{}}
		}
		plan, err := env.registry.Plan(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		return env, plan
	}

	t.Run("deterministic retry", func(t *testing.T) {
		env, plan := prepare(t, ArtifactPack)
		retry, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, plan.Artifacts[0].Release.Identity))
		if err != nil || retry.TransactionID != plan.TransactionID || retry.PlanDigest != plan.PlanDigest {
			t.Fatalf("retry did not reuse transaction: %#v %v", retry, err)
		}
	})

	t.Run("template allocator required", func(t *testing.T) {
		env := newTestEnv(t, NewMemoryTransactionStore())
		env.registry.projectState = currentOnlyProject{state: env.project.state}
		identity := ArtifactIdentity{Kind: ArtifactTemplate, CanonicalID: "critical/no-allocator", Version: "1.0.0"}
		if _, err := env.registry.Plan(context.Background(), PlanRequest{Action: ActionCreateFromTemplate, Requested: identity}); !IsFailure(err, FailureUnavailable) {
			t.Fatalf("template without allocator accepted: %v", err)
		}
	})

	for _, invalid := range []string{"digest", "precondition"} {
		t.Run("invalid engine "+invalid, func(t *testing.T) {
			env := newTestEnv(t, NewMemoryTransactionStore())
			env.validator.mutationInvalid = invalid
			data := []byte("invalid-engine")
			release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/invalid-" + invalid, Version: "1.0.0"}, data, nil)
			addRelease(env, release, data)
			if _, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, release.Identity)); !IsFailure(err, FailureArtifactCorrupt) {
				t.Fatalf("invalid Engine mutation accepted: %v", err)
			}
		})
	}

	t.Run("completed transaction cannot be replanned", func(t *testing.T) {
		env, plan := prepare(t, ArtifactPack)
		input := RuntimeCommitInput{Plan: plan, OperationID: "completed", IdempotencyKey: "completed"}
		if _, err := env.registry.Commit(context.Background(), input); err != nil {
			t.Fatal(err)
		}
		if _, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, plan.Artifacts[0].Release.Identity)); !IsFailure(err, FailurePlanStale) {
			t.Fatalf("completed transaction replanned: %v", err)
		}
	})

	t.Run("legacy committed result remains idempotent", func(t *testing.T) {
		env, plan := prepare(t, ArtifactPack)
		input := RuntimeCommitInput{Plan: plan, OperationID: "legacy", IdempotencyKey: "legacy"}
		first, err := env.registry.Commit(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
		memory := env.store.(*MemoryTransactionStore)
		memory.mu.Lock()
		tx := memory.transactions[plan.TransactionID]
		tx.RuntimeResult = nil
		memory.transactions[plan.TransactionID] = tx
		memory.mu.Unlock()
		second, err := env.registry.Commit(context.Background(), input)
		if err != nil || second != first || second.DocumentID != "doc" || second.InitialCommittedRevision {
			t.Fatalf("legacy result retry: %#v %v", second, err)
		}
	})

	for _, documentID := range []string{"", "wrong-document"} {
		t.Run("corrupt committed result retry "+documentID, func(t *testing.T) {
			env, plan := prepare(t, ArtifactPack)
			input := RuntimeCommitInput{Plan: plan, OperationID: "corrupt-retry", IdempotencyKey: "corrupt-retry"}
			if _, err := env.registry.Commit(context.Background(), input); err != nil {
				t.Fatal(err)
			}
			memory := env.store.(*MemoryTransactionStore)
			memory.mu.Lock()
			tx := memory.transactions[plan.TransactionID]
			tx.RuntimeResult.DocumentID = documentID
			memory.transactions[plan.TransactionID] = tx
			memory.mu.Unlock()
			if _, err := env.registry.Commit(context.Background(), input); !IsFailure(err, FailureRepairRequired) {
				t.Fatalf("corrupt persisted Runtime result accepted on retry: %v", err)
			}
		})
	}

	for _, documentID := range []string{"", "wrong-document"} {
		t.Run("corrupt committed result CAS race "+documentID, func(t *testing.T) {
			env, plan := prepare(t, ArtifactPack)
			input := RuntimeCommitInput{Plan: plan, OperationID: "cas-race", IdempotencyKey: "cas-race"}
			awaiting, _, _ := env.store.GetRegistryTransaction(context.Background(), plan.TransactionID)
			latest := cloneTransaction(awaiting)
			latest.CommittedRevision = "r2"
			latest.OperationResultID = input.OperationID
			latest.RuntimeResult = &RuntimeCommitResult{CommittedRevision: "r2", OperationResultID: input.OperationID, DocumentID: documentID}
			latest.Events = append(latest.Events, TransactionEvent{State: StateCommitted, EvidenceDigest: testDigest('r'), Sequence: transactionVersion(latest) + 1, IdempotencyKey: input.IdempotencyKey})
			env.registry.transactions = &commitRaceStore{first: awaiting, latest: latest}
			if _, err := env.registry.Commit(context.Background(), input); !IsFailure(err, FailureRepairRequired) {
				t.Fatalf("corrupt CAS winner Runtime result accepted: %v", err)
			}
		})
	}

	t.Run("remove commit rebuilds aggregate from removed identity", func(t *testing.T) {
		env := newTestEnv(t, NewMemoryTransactionStore())
		identity := ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/remove-commit", Version: "1.0.0"}
		env.project.state.DependencySnapshot = ProjectDependencySnapshot{ResolvedLockDigest: testDigest('d'), Installs: []LockedArtifact{{Identity: identity, SourceID: "official", Digest: testDigest('b')}}}
		plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionRemove, identity))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "remove", IdempotencyKey: "remove"}); err != nil {
			t.Fatal(err)
		}
	})

	for name, mutate := range map[string]func(*testEnv){
		"source search":      func(env *testEnv) { env.client.searchErr = errors.New("offline") },
		"immutable metadata": func(env *testEnv) { env.client.releases[0].PublisherID = "attacker" },
		"engine aggregate": func(env *testEnv) {
			env.validator.aggregateCapabilities = []semantic.AuthoringCapability{semantic.AuthoringCapabilityViewWrite}
		},
	} {
		t.Run(name, func(t *testing.T) {
			env, plan := prepare(t, ArtifactPack)
			mutate(env)
			if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: name, IdempotencyKey: name}); !IsFailure(err, FailurePlanStale) {
				t.Fatalf("freshness mutation accepted: %v", err)
			}
		})
	}

	t.Run("template runtime binding", func(t *testing.T) {
		env, plan := prepare(t, ArtifactTemplate)
		env.registry.runtime = invalidTemplateRuntime{}
		if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "template-invalid", IdempotencyKey: "template-invalid"}); !IsFailure(err, FailureRepairRequired) {
			t.Fatalf("invalid template publication accepted: %v", err)
		}
		tx, _ := env.registry.Transaction(plan.TransactionID)
		if transactionState(tx) != StateRepairRequired {
			t.Fatalf("template repair state=%s", transactionState(tx))
		}
	})
}

func TestCriticalDiskTransactionFailureAndLivenessBranches(t *testing.T) {
	root := t.TempDir()
	store, err := NewDiskTransactionStore(root)
	if err != nil {
		t.Fatal(err)
	}
	id := "ffffffffffffffffffffffffffffffff"
	tx := Transaction{Plan: InstallPlan{TransactionID: id, PlanDigest: "plan"}, Events: []TransactionEvent{{State: StatePlanned, Sequence: 1}}}
	if err := store.CreateRegistryTransaction(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateRegistryTransaction(context.Background(), Transaction{Plan: InstallPlan{TransactionID: "bad"}}); err == nil {
		t.Fatal("invalid transaction id accepted")
	}
	missingID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	missing := cloneTransaction(tx)
	missing.Plan.TransactionID = missingID
	if ok, err := store.CompareAndSwapRegistryTransaction(context.Background(), missingID, 1, missing); err != nil || ok {
		t.Fatalf("missing transaction CAS: %v %v", ok, err)
	}
	if err := store.CreateRegistryTransaction(context.Background(), tx); err == nil {
		t.Fatal("duplicate transaction accepted")
	}
	if ok, err := store.CompareAndSwapRegistryTransaction(context.Background(), id, 99, tx); err != nil || ok {
		t.Fatalf("stale CAS accepted: %v %v", ok, err)
	}
	invalid := cloneTransaction(tx)
	invalid.Events = append(invalid.Events, TransactionEvent{State: StateCommitted, Sequence: 2})
	if ok, err := store.CompareAndSwapRegistryTransaction(context.Background(), id, 1, invalid); err == nil || ok {
		t.Fatalf("invalid transition accepted: %v %v", ok, err)
	}
	if err := os.WriteFile(store.path(id), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.GetRegistryTransaction(context.Background(), id); err == nil {
		t.Fatal("corrupt transaction accepted")
	}
	wrong, err := json.Marshal(Transaction{Plan: InstallPlan{TransactionID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PlanDigest: "plan"}, Events: []TransactionEvent{{State: StatePlanned, Sequence: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.path(id), wrong, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.GetRegistryTransaction(context.Background(), id); err == nil {
		t.Fatal("wrong transaction identity accepted")
	}
	if err := os.WriteFile(store.path(id), append(wrong, []byte(" {}")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.GetRegistryTransaction(context.Background(), id); err == nil {
		t.Fatal("trailing transaction JSON accepted")
	}

	holder, err := NewDiskTransactionStore(root)
	if err != nil {
		t.Fatal(err)
	}
	release, err := holder.lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	contender, err := NewDiskTransactionStore(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	if _, err := contender.lock(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled lock wait=%v", err)
	}
	rootFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(rootFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDiskTransactionStore(rootFile); err == nil {
		t.Fatal("file accepted as transaction root")
	}
	brokenStore := &DiskTransactionStore{root: rootFile}
	if err := brokenStore.CreateRegistryTransaction(context.Background(), Transaction{Plan: InstallPlan{TransactionID: "abababababababababababababababab"}, Events: []TransactionEvent{{State: StatePlanned, Sequence: 1}}}); err == nil {
		t.Fatal("invalid atomic-create root accepted")
	}
}

func TestCriticalRecoveryReplayFailure(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	data := []byte("replay-failure")
	release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/replay-failure", Version: "1.0.0"}, data, nil)
	addRelease(env, release, data)
	plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, release.Identity))
	if err != nil {
		t.Fatal(err)
	}
	env.runtime.err = errors.New("runtime unavailable")
	intent := RuntimeCommitInput{Plan: plan, OperationID: "op", IdempotencyKey: "replay-failure"}
	tx, _, _ := env.store.GetRegistryTransaction(context.Background(), plan.TransactionID)
	version := transactionVersion(tx)
	tx.RuntimeInput = &intent
	tx.Events = append(tx.Events, TransactionEvent{State: StateApplying, Sequence: version + 1, IdempotencyKey: intent.IdempotencyKey})
	if ok, err := env.store.CompareAndSwapRegistryTransaction(context.Background(), plan.TransactionID, version, tx); err != nil || !ok {
		t.Fatal(err)
	}
	recovered, err := env.registry.RecoverTransaction(context.Background(), plan.TransactionID)
	if !IsFailure(err, FailureRepairRequired) || transactionState(recovered) != StateNeedsReview || env.runtime.calls.Load() != 1 {
		t.Fatalf("replay failure: %#v %v", recovered, err)
	}
}

func TestCriticalRecoveryRejectsFreshTrustRevocation(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	data := []byte("recovery-revocation")
	release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/recovery-revocation", Version: "1.0.0"}, data, nil)
	addRelease(env, release, data)
	plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, release.Identity))
	if err != nil {
		t.Fatal(err)
	}
	intent := RuntimeCommitInput{Plan: plan, OperationID: "revoked-op", IdempotencyKey: "revoked-id"}
	tx, _, _ := env.store.GetRegistryTransaction(context.Background(), plan.TransactionID)
	version := transactionVersion(tx)
	tx.RuntimeInput = &intent
	tx.Events = append(tx.Events, TransactionEvent{State: StateApplying, Sequence: version + 1, IdempotencyKey: intent.IdempotencyKey})
	if ok, err := env.store.CompareAndSwapRegistryTransaction(context.Background(), plan.TransactionID, version, tx); err != nil || !ok {
		t.Fatal(err)
	}
	_, policy, _, _ := env.registry.sourceContext("official")
	policy.RevokedKeys["key-1"] = true
	if err := env.registry.PutTrustPolicy(policy); err != nil {
		t.Fatal(err)
	}
	recovered, err := env.registry.RecoverTransaction(context.Background(), plan.TransactionID)
	if !IsFailure(err, FailurePlanStale) || transactionState(recovered) != StateNeedsReview || env.runtime.calls.Load() != 0 {
		t.Fatalf("revoked recovery replay accepted: %#v %v", recovered, err)
	}
}

func TestCriticalRecoveryFreshValidationFailures(t *testing.T) {
	prepare := func(t *testing.T) (*testEnv, Transaction, RuntimeCommitInput) {
		t.Helper()
		env := newTestEnv(t, NewMemoryTransactionStore())
		data := []byte("fresh-recovery-validation")
		release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/fresh-recovery-validation", Version: "1.0.0"}, data, nil)
		addRelease(env, release, data)
		plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, release.Identity))
		if err != nil {
			t.Fatal(err)
		}
		tx, _, _ := env.store.GetRegistryTransaction(context.Background(), plan.TransactionID)
		return env, tx, RuntimeCommitInput{Plan: plan, OperationID: "fresh-op", IdempotencyKey: "fresh-id"}
	}
	tests := []struct {
		name   string
		mutate func(*testEnv, *Transaction, *RuntimeCommitInput)
	}{
		{"missing operation identity", func(_ *testEnv, _ *Transaction, input *RuntimeCommitInput) { input.OperationID = "" }},
		{"project precondition", func(env *testEnv, _ *Transaction, _ *RuntimeCommitInput) { env.project.state.Revision = "changed" }},
		{"runtime session", func(env *testEnv, _ *Transaction, _ *RuntimeCommitInput) { env.project.state.RuntimeSessionID = "" }},
		{"source binding", func(env *testEnv, _ *Transaction, _ *RuntimeCommitInput) {
			_ = env.registry.DisconnectSource("official")
		}},
		{"fresh trust", func(env *testEnv, tx *Transaction, _ *RuntimeCommitInput) {
			_, policy, _, _ := env.registry.sourceContext("official")
			policy.RevokedKeys["key-1"] = true
			_ = env.registry.PutTrustPolicy(policy)
			tx.Plan.SourceBindings[0].TrustPolicyDigest = digestTrust(policy)
		}},
		{"source search", func(env *testEnv, _ *Transaction, _ *RuntimeCommitInput) {
			env.client.searchErr = errors.New("offline")
		}},
		{"artifact metadata", func(env *testEnv, _ *Transaction, _ *RuntimeCommitInput) {
			env.client.releases[0].PublisherID = "attacker"
		}},
		{"engine mutation", func(env *testEnv, _ *Transaction, _ *RuntimeCommitInput) { env.validator.fail = true }},
		{"host impact cardinality", func(_ *testEnv, tx *Transaction, _ *RuntimeCommitInput) { tx.Plan.HostOperationImpacts = nil }},
		{"fresh access", func(env *testEnv, _ *Transaction, _ *RuntimeCommitInput) { env.access.deny = true }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, tx, input := prepare(t)
			tc.mutate(env, &tx, &input)
			if _, err := env.registry.refreshRecoveryRuntimeInput(context.Background(), tx, input); err == nil {
				t.Fatal("fresh recovery validation accepted mutation")
			}
		})
	}
}

func TestCriticalTemplateUnknownRecoveryUsesFreshRuntimeBinding(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	data := []byte("template-recovery-fresh")
	release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactTemplate, CanonicalID: "critical/template-recovery-fresh", Version: "1.0.0"}, data, nil)
	addRelease(env, release, data)
	plan, err := env.registry.Plan(context.Background(), PlanRequest{Action: ActionCreateFromTemplate, Requested: release.Identity, DependencySnapshot: ProjectDependencySnapshot{}})
	if err != nil {
		t.Fatal(err)
	}
	intent := RuntimeCommitInput{Plan: plan, OperationID: "template-recovery-op", IdempotencyKey: "template-recovery-id"}
	tx, _, _ := env.store.GetRegistryTransaction(context.Background(), plan.TransactionID)
	version := transactionVersion(tx)
	tx.RuntimeInput = &intent
	tx.Events = append(tx.Events, TransactionEvent{State: StateApplying, Sequence: version + 1, IdempotencyKey: intent.IdempotencyKey})
	if ok, err := env.store.CompareAndSwapRegistryTransaction(context.Background(), plan.TransactionID, version, tx); err != nil || !ok {
		t.Fatal(err)
	}
	recovered, err := env.registry.RecoverTransaction(context.Background(), plan.TransactionID)
	if err != nil || transactionState(recovered) != StateCommitted || recovered.RuntimeResult == nil || recovered.RuntimeResult.DocumentID != plan.NewDocumentID || env.runtime.last.RuntimeSessionID != "runtime-session-template" {
		t.Fatalf("template recovery binding: %#v %v", recovered, err)
	}
}

func TestCriticalTemplateRecoveryAndCommitRequireAllocator(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	data := []byte("template-allocator-loss")
	release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactTemplate, CanonicalID: "critical/template-allocator-loss", Version: "1.0.0"}, data, nil)
	addRelease(env, release, data)
	plan, err := env.registry.Plan(context.Background(), PlanRequest{Action: ActionCreateFromTemplate, Requested: release.Identity, DependencySnapshot: ProjectDependencySnapshot{}})
	if err != nil {
		t.Fatal(err)
	}
	tx, _, _ := env.store.GetRegistryTransaction(context.Background(), plan.TransactionID)
	env.registry.projectState = currentOnlyProject{state: env.project.state}
	if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "allocator", IdempotencyKey: "allocator"}); !IsFailure(err, FailurePlanStale) {
		t.Fatalf("commit without template allocator: %v", err)
	}
	if _, err := env.registry.refreshRecoveryRuntimeInput(context.Background(), tx, RuntimeCommitInput{Plan: plan, OperationID: "allocator", IdempotencyKey: "allocator"}); err == nil {
		t.Fatal("recovery without template allocator accepted")
	}
}

func TestCriticalRecoveryRejectsChangedAccessEvaluationDigest(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	data := []byte("recovery-access-digest")
	release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/recovery-access-digest", Version: "1.0.0"}, data, nil)
	addRelease(env, release, data)
	plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, release.Identity))
	if err != nil {
		t.Fatal(err)
	}
	input := RuntimeCommitInput{Plan: plan, OperationID: "access-op", IdempotencyKey: "access-id"}
	tx, _, _ := env.store.GetRegistryTransaction(context.Background(), plan.TransactionID)
	version := transactionVersion(tx)
	tx.RuntimeInput = &input
	tx.Events = append(tx.Events, TransactionEvent{State: StateApplying, EvidenceDigest: plan.EvaluationDigest, Sequence: version + 1, IdempotencyKey: input.IdempotencyKey})
	if ok, err := env.store.CompareAndSwapRegistryTransaction(context.Background(), plan.TransactionID, version, tx); err != nil || !ok {
		t.Fatal(err)
	}
	env.project.state.GrantSnapshot.MembershipVersion = "changed-membership"
	recovered, err := env.registry.RecoverTransaction(context.Background(), plan.TransactionID)
	if !IsFailure(err, FailurePlanStale) || transactionState(recovered) != StateNeedsReview || env.runtime.calls.Load() != 0 || env.access.calls.Load() != 2 {
		t.Fatalf("changed Access evaluation replayed stale plan: %#v %v", recovered, err)
	}
}

func TestCriticalRuntimeCommittedOutcomeValidation(t *testing.T) {
	tests := []struct {
		name     string
		template bool
		result   RuntimeCommitResult
	}{
		{"existing missing revision", false, RuntimeCommitResult{OperationResultID: "bound-op", DocumentID: "doc"}},
		{"existing missing operation result", false, RuntimeCommitResult{CommittedRevision: "r2", DocumentID: "doc"}},
		{"existing wrong operation result", false, RuntimeCommitResult{CommittedRevision: "r2", OperationResultID: "other-op", DocumentID: "doc"}},
		{"existing empty document", false, RuntimeCommitResult{CommittedRevision: "r2", OperationResultID: "bound-op"}},
		{"existing wrong document", false, RuntimeCommitResult{CommittedRevision: "r2", OperationResultID: "bound-op", DocumentID: "other-document"}},
		{"existing marked initial", false, RuntimeCommitResult{CommittedRevision: "r2", OperationResultID: "bound-op", DocumentID: "doc", InitialCommittedRevision: true}},
		{"template missing revision", true, RuntimeCommitResult{OperationResultID: "bound-op", DocumentID: "new-document", InitialCommittedRevision: true}},
		{"template wrong operation result", true, RuntimeCommitResult{CommittedRevision: "r2", OperationResultID: "other-op", DocumentID: "new-document", InitialCommittedRevision: true}},
		{"template wrong document", true, RuntimeCommitResult{CommittedRevision: "r2", OperationResultID: "bound-op", DocumentID: "other-document", InitialCommittedRevision: true}},
		{"template not initial", true, RuntimeCommitResult{CommittedRevision: "r2", OperationResultID: "bound-op", DocumentID: "new-document"}},
	}
	for _, tc := range tests {
		t.Run(tc.name+" normal", func(t *testing.T) {
			env := newTestEnv(t, NewMemoryTransactionStore())
			kind, action := ArtifactPack, ActionInstall
			if tc.template {
				kind, action = ArtifactTemplate, ActionCreateFromTemplate
			}
			data := []byte("runtime-result-" + tc.name)
			release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: kind, CanonicalID: "critical/" + strings.ReplaceAll(tc.name, " ", "-"), Version: "1.0.0"}, data, nil)
			addRelease(env, release, data)
			request := planRequest(env, action, release.Identity)
			if tc.template {
				request = PlanRequest{Action: action, Requested: release.Identity, DependencySnapshot: ProjectDependencySnapshot{}}
			}
			plan, err := env.registry.Plan(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			env.runtime.result = &tc.result
			if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "bound-op", IdempotencyKey: "bound-id"}); !IsFailure(err, FailureRepairRequired) {
				t.Fatalf("invalid Runtime result converged: %v", err)
			}
			tx, _ := env.registry.Transaction(plan.TransactionID)
			if transactionState(tx) != StateRepairRequired || tx.CommittedRevision != "" || tx.RuntimeResult != nil {
				t.Fatalf("invalid Runtime result persisted as committed: %#v", tx)
			}
		})

		t.Run(tc.name+" recovery", func(t *testing.T) {
			env := newTestEnv(t, NewMemoryTransactionStore())
			kind, action := ArtifactPack, ActionInstall
			if tc.template {
				kind, action = ArtifactTemplate, ActionCreateFromTemplate
			}
			data := []byte("runtime-recovery-result-" + tc.name)
			release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: kind, CanonicalID: "critical/recovery-" + strings.ReplaceAll(tc.name, " ", "-"), Version: "1.0.0"}, data, nil)
			addRelease(env, release, data)
			request := planRequest(env, action, release.Identity)
			if tc.template {
				request = PlanRequest{Action: action, Requested: release.Identity, DependencySnapshot: ProjectDependencySnapshot{}}
			}
			plan, err := env.registry.Plan(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			input := RuntimeCommitInput{Plan: plan, OperationID: "bound-op", IdempotencyKey: "bound-id"}
			tx, _, _ := env.store.GetRegistryTransaction(context.Background(), plan.TransactionID)
			version := transactionVersion(tx)
			tx.RuntimeInput = &input
			tx.Events = append(tx.Events, TransactionEvent{State: StateApplying, EvidenceDigest: plan.EvaluationDigest, Sequence: version + 1, IdempotencyKey: input.IdempotencyKey})
			if ok, err := env.store.CompareAndSwapRegistryTransaction(context.Background(), plan.TransactionID, version, tx); err != nil || !ok {
				t.Fatal(err)
			}
			env.runtime.recovery = RuntimeRegistryOutcome{Status: RuntimeRegistryCommitted, Result: tc.result}
			recovered, err := env.registry.RecoverTransaction(context.Background(), plan.TransactionID)
			if !IsFailure(err, FailureRepairRequired) || transactionState(recovered) != StateNeedsReview || recovered.CommittedRevision != "" || recovered.RuntimeResult != nil {
				t.Fatalf("invalid recovered Runtime result converged: %#v %v", recovered, err)
			}
		})
	}
	t.Run("recovery missing durable operation binding", func(t *testing.T) {
		env := newTestEnv(t, NewMemoryTransactionStore())
		plan := testFinalizedPlan(t, "missing-runtime-input")
		plan.MutationDigest = testDigest('m')
		plan.PlanDigest = digestPlan(plan)
		plan.ProjectMutationPlan.PlanDigest = plan.PlanDigest
		tx := Transaction{Plan: plan, Events: []TransactionEvent{{State: StateRepairRequired, Sequence: 1, IdempotencyKey: "bound-id"}}}
		if err := env.store.CreateRegistryTransaction(context.Background(), tx); err != nil {
			t.Fatal(err)
		}
		env.runtime.recovery = RuntimeRegistryOutcome{Status: RuntimeRegistryCommitted, Result: RuntimeCommitResult{CommittedRevision: "r2", OperationResultID: "bound-op"}}
		recovered, err := env.registry.RecoverTransaction(context.Background(), plan.TransactionID)
		if !IsFailure(err, FailureRepairRequired) || transactionState(recovered) != StateNeedsReview {
			t.Fatalf("unbound recovered Runtime result converged: %#v %v", recovered, err)
		}
	})
}

func TestCriticalPlanBoundDocumentIDRejectsCurrentDocumentDrift(t *testing.T) {
	prepare := func(t *testing.T) (*testEnv, InstallPlan, RuntimeCommitInput) {
		t.Helper()
		env := newTestEnv(t, NewMemoryTransactionStore())
		data := []byte("document-drift")
		release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/document-drift", Version: "1.0.0"}, data, nil)
		addRelease(env, release, data)
		plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, release.Identity))
		if err != nil {
			t.Fatal(err)
		}
		return env, plan, RuntimeCommitInput{Plan: plan, OperationID: "document-drift", IdempotencyKey: "document-drift"}
	}
	driftProject := func(env *testEnv) {
		env.project.mu.Lock()
		env.project.state.DocumentID = "replacement-document"
		env.project.mu.Unlock()
	}

	t.Run("normal publication", func(t *testing.T) {
		env, plan, input := prepare(t)
		driftProject(env)
		env.runtime.result = &RuntimeCommitResult{CommittedRevision: "r2", OperationResultID: input.OperationID, DocumentID: "replacement-document"}
		if _, err := env.registry.Commit(context.Background(), input); !IsFailure(err, FailurePlanStale) {
			t.Fatalf("replacement Document reached normal publication for plan %s: %v", plan.TransactionID, err)
		}
		if env.runtime.calls.Load() != 0 {
			t.Fatal("Document drift reached Runtime")
		}
	})

	t.Run("recovery outcome", func(t *testing.T) {
		env, plan, input := prepare(t)
		tx, _, _ := env.store.GetRegistryTransaction(context.Background(), plan.TransactionID)
		version := transactionVersion(tx)
		tx.RuntimeInput = &input
		tx.Events = append(tx.Events, TransactionEvent{State: StateApplying, EvidenceDigest: plan.EvaluationDigest, Sequence: version + 1, IdempotencyKey: input.IdempotencyKey})
		if ok, err := env.store.CompareAndSwapRegistryTransaction(context.Background(), plan.TransactionID, version, tx); err != nil || !ok {
			t.Fatal(err)
		}
		driftProject(env)
		env.runtime.recovery = RuntimeRegistryOutcome{Status: RuntimeRegistryCommitted, Result: RuntimeCommitResult{CommittedRevision: "r2", OperationResultID: input.OperationID, DocumentID: "replacement-document"}}
		recovered, err := env.registry.RecoverTransaction(context.Background(), plan.TransactionID)
		if !IsFailure(err, FailureRepairRequired) || transactionState(recovered) != StateNeedsReview || recovered.RuntimeResult != nil {
			t.Fatalf("replacement Document converged during recovery: %#v %v", recovered, err)
		}
	})

	t.Run("recovery replay", func(t *testing.T) {
		env, plan, input := prepare(t)
		tx, _, _ := env.store.GetRegistryTransaction(context.Background(), plan.TransactionID)
		version := transactionVersion(tx)
		tx.RuntimeInput = &input
		tx.Events = append(tx.Events, TransactionEvent{State: StateApplying, EvidenceDigest: plan.EvaluationDigest, Sequence: version + 1, IdempotencyKey: input.IdempotencyKey})
		if ok, err := env.store.CompareAndSwapRegistryTransaction(context.Background(), plan.TransactionID, version, tx); err != nil || !ok {
			t.Fatal(err)
		}
		driftProject(env)
		recovered, err := env.registry.RecoverTransaction(context.Background(), plan.TransactionID)
		if !IsFailure(err, FailurePlanStale) || transactionState(recovered) != StateNeedsReview || env.runtime.calls.Load() != 0 {
			t.Fatalf("replacement Document reached recovery replay: %#v %v", recovered, err)
		}
	})

	t.Run("idempotent legacy reconstruction", func(t *testing.T) {
		env, plan, input := prepare(t)
		if _, err := env.registry.Commit(context.Background(), input); err != nil {
			t.Fatal(err)
		}
		memory := env.store.(*MemoryTransactionStore)
		memory.mu.Lock()
		tx := memory.transactions[plan.TransactionID]
		tx.RuntimeResult = nil
		memory.transactions[plan.TransactionID] = tx
		memory.mu.Unlock()
		driftProject(env)
		if _, err := env.registry.Commit(context.Background(), input); !IsFailure(err, FailurePlanStale) {
			t.Fatalf("legacy result reconstructed against replacement Document: %v", err)
		}
	})

	t.Run("CAS winner", func(t *testing.T) {
		env, plan, input := prepare(t)
		awaiting, _, _ := env.store.GetRegistryTransaction(context.Background(), plan.TransactionID)
		latest := cloneTransaction(awaiting)
		latest.CommittedRevision = "r2"
		latest.OperationResultID = input.OperationID
		latest.RuntimeResult = &RuntimeCommitResult{CommittedRevision: "r2", OperationResultID: input.OperationID, DocumentID: "replacement-document"}
		latest.Events = append(latest.Events, TransactionEvent{State: StateCommitted, EvidenceDigest: testDigest('r'), Sequence: transactionVersion(latest) + 1, IdempotencyKey: input.IdempotencyKey})
		env.registry.transactions = &commitRaceStore{first: awaiting, latest: latest}
		driftProject(env)
		if _, err := env.registry.Commit(context.Background(), input); !IsFailure(err, FailurePlanStale) {
			t.Fatalf("CAS winner from replacement Document accepted: %v", err)
		}
	})
}

func TestCriticalRecoveryPersistenceFailures(t *testing.T) {
	for _, state := range []TransactionState{StatePlanned, StateApplying, StateRepairRequired} {
		t.Run(string(state), func(t *testing.T) {
			base := NewMemoryTransactionStore()
			env := newTestEnv(t, base)
			id := strings.Repeat(string(state[0]), 32)
			plan := InstallPlan{TransactionID: id, PlanDigest: "plan"}
			if state != StatePlanned {
				plan = testFinalizedPlan(t, id)
				plan.MutationDigest = testDigest('m')
				plan.ProjectMutationPlan.MutationDigest = plan.MutationDigest
				plan.PlanDigest = digestPlan(plan)
				plan.ProjectMutationPlan.PlanDigest = plan.PlanDigest
			}
			tx := Transaction{Plan: plan, Events: []TransactionEvent{{State: state, Sequence: 1, IdempotencyKey: "persist"}}}
			if err := base.CreateRegistryTransaction(context.Background(), tx); err != nil {
				t.Fatal(err)
			}
			env.registry.transactions = &faultStore{base: base, casErr: errors.New("disk unavailable")}
			if _, err := env.registry.RecoverTransaction(context.Background(), id); !IsFailure(err, FailureUnavailable) {
				t.Fatalf("recovery persistence error lost: %v", err)
			}
		})
	}
	env := newTestEnv(t, NewMemoryTransactionStore())
	if _, _, _, ok := env.registry.sourceContext("missing"); ok {
		t.Fatal("missing source context resolved")
	}
}

func TestCriticalRecoveryResumesPersistedDownloadRequest(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	data := []byte("resume-download")
	release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/resume-download", Version: "1.0.0"}, data, nil)
	addRelease(env, release, data)
	request := planRequest(env, ActionInstall, release.Identity)
	id := strings.TrimPrefix(digestJSON(struct {
		Request    PlanRequest
		DocumentID string
	}{request, env.project.state.DocumentID}), "sha256:")[:32]
	tx := Transaction{
		Plan:            InstallPlan{TransactionID: id, PlanDigest: digestPlanningRequest(request), Action: request.Action, ProjectID: request.ProjectID},
		PlanningRequest: &request,
		Events:          []TransactionEvent{{State: StateDownloading, EvidenceDigest: digestJSON(request), Sequence: 1}},
	}
	if err := env.store.CreateRegistryTransaction(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	recovered, err := env.registry.RecoverTransaction(context.Background(), id)
	if err != nil || transactionState(recovered) != StateAwaitingConfirmation || recovered.Plan.PlanDigest == tx.Plan.PlanDigest {
		t.Fatalf("download recovery: %#v %v", recovered, err)
	}
	retry, err := env.registry.Plan(context.Background(), request)
	if err != nil || retry.PlanDigest != recovered.Plan.PlanDigest {
		t.Fatalf("finalized retry: %#v %v", retry, err)
	}
}

func TestCriticalRecoveryReturnsLatestPlanningFailure(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	request := planRequest(env, ActionInstall, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/invalid", Version: "not-semver"})
	id := strings.Repeat("f", 32)
	tx := Transaction{
		Plan:            InstallPlan{TransactionID: id, PlanDigest: digestPlanningRequest(request), Action: request.Action, ProjectID: request.ProjectID},
		PlanningRequest: &request,
		Events:          []TransactionEvent{{State: StateDownloading, EvidenceDigest: digestJSON(request), Sequence: 1}},
	}
	if err := env.store.CreateRegistryTransaction(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	recovered, err := env.registry.RecoverTransaction(context.Background(), id)
	if !IsFailure(err, FailureDependencyConflict) || transactionState(recovered) != StateDownloading {
		t.Fatalf("planning recovery failure lost latest transaction: %#v %v", recovered, err)
	}
}

func TestCriticalPlanningJournalFailures(t *testing.T) {
	for name, store := range map[string]TransactionStore{
		"load":   &faultStore{base: NewMemoryTransactionStore(), getErr: errors.New("load failed")},
		"append": &faultStore{base: NewMemoryTransactionStore(), casErr: errors.New("append failed")},
	} {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t, store)
			data := []byte("planning-journal")
			release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/planning-" + name, Version: "1.0.0"}, data, nil)
			addRelease(env, release, data)
			if _, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, release.Identity)); !IsFailure(err, FailureUnavailable) {
				t.Fatalf("planning persistence failure lost: %v", err)
			}
		})
	}
}

func TestCriticalNeedsReviewPersistenceFailure(t *testing.T) {
	base := NewMemoryTransactionStore()
	env := newTestEnv(t, base)
	plan := testFinalizedPlan(t, "needs-review-persistence")
	tx := Transaction{Plan: plan, Events: []TransactionEvent{{State: StateRepairRequired, Sequence: 1}}}
	if err := base.CreateRegistryTransaction(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	env.registry.transactions = &faultStore{base: base, casErr: errors.New("disk unavailable")}
	if _, err := env.registry.recoveryNeedsReview(context.Background(), tx, errors.New("unknown publication")); !IsFailure(err, FailureUnavailable) {
		t.Fatalf("needs-review persistence failure lost: %v", err)
	}
}

func TestCriticalResolverLockAndRootHardening(t *testing.T) {
	t.Run("requested pin persists for install and update roots", func(t *testing.T) {
		for _, action := range []Action{ActionInstall, ActionUpdate} {
			for _, requestedPin := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s-%t", action, requestedPin), func(t *testing.T) {
					env := newTestEnv(t, NewMemoryTransactionStore())
					childData, rootData := []byte("requested-pin-child"), []byte("requested-pin-root")
					child := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/requested-pin-child", Version: "1.0.0"}, childData, nil)
					root := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/requested-pin-root", Version: "1.0.0"}, rootData, []Dependency{{Kind: ArtifactPack, CanonicalID: child.Identity.CanonicalID, VersionRange: child.Identity.Version}})
					resolved := map[string]PlanArtifact{artifactKey(root.Identity): {Release: root}, artifactKey(child.Identity): {Release: child}}
					if action == ActionUpdate {
						rootLock, err := lockedFromPlan(resolved[artifactKey(root.Identity)], !requestedPin, resolved)
						if err != nil {
							t.Fatal(err)
						}
						childLock, err := lockedFromPlan(resolved[artifactKey(child.Identity)], true, resolved)
						if err != nil {
							t.Fatal(err)
						}
						rootLock.Dependencies = []ArtifactIdentity{child.Identity}
						env.project.state.DependencySnapshot = ProjectDependencySnapshot{ResolvedLockDigest: testDigest('0'), Installs: []LockedArtifact{rootLock, childLock}}
					}
					addRelease(env, child, childData)
					addRelease(env, root, rootData)
					request := planRequest(env, action, root.Identity)
					request.RequestedPin = requestedPin
					plan, err := env.registry.Plan(context.Background(), request)
					if err != nil {
						t.Fatal(err)
					}
					rootLocks := plan.ResolvedLockDelta.Added
					if action == ActionUpdate {
						rootLocks = plan.ResolvedLockDelta.Updated
					}
					var rootResult *LockedArtifact
					for index := range rootLocks {
						if artifactKey(rootLocks[index].Identity) == artifactKey(root.Identity) {
							rootResult = &rootLocks[index]
						}
					}
					if rootResult == nil || rootResult.Pinned != requestedPin {
						t.Fatalf("requested root pin was not applied: %#v", plan.ResolvedLockDelta)
					}
					for _, lock := range append(append([]LockedArtifact{}, plan.ResolvedLockDelta.Added...), plan.ResolvedLockDelta.Updated...) {
						if artifactKey(lock.Identity) == artifactKey(child.Identity) && lock.Pinned != (action == ActionUpdate) {
							t.Fatalf("dependency pin was not preserved independently: %#v", lock)
						}
					}
					if action == ActionUpdate {
						lockedChild, ok := findLocked(plan.DependencySnapshot, child.Identity)
						if !ok || !lockedChild.Pinned {
							t.Fatalf("existing dependency pin was lost from the bound snapshot: %#v", plan.DependencySnapshot)
						}
					}
					retry, err := env.registry.Plan(context.Background(), request)
					persisted, _, loadErr := env.store.GetRegistryTransaction(context.Background(), plan.TransactionID)
					if err != nil || loadErr != nil || retry.PlanDigest != plan.PlanDigest || persisted.PlanningRequest == nil || persisted.PlanningRequest.RequestedPin != requestedPin || digestJSON(persisted.Plan.ResolvedLockDelta) != digestJSON(plan.ResolvedLockDelta) {
						t.Fatalf("requested pin did not survive retry/store: %#v %#v %v %v", retry, persisted, err, loadErr)
					}
				})
			}
		}
	})

	t.Run("divergent digest for one identity", func(t *testing.T) {
		env := newTestEnv(t, NewMemoryTransactionStore())
		identity := ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/divergent", Version: "1.0.0"}
		for _, data := range [][]byte{[]byte("first"), []byte("second")} {
			release := signedRelease(t, env.privateKey, identity, data, nil)
			addRelease(env, release, data)
		}
		if _, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, identity)); !IsFailure(err, FailureDependencyConflict) {
			t.Fatalf("divergent artifact identity accepted: %v", err)
		}
	})

	t.Run("locked dependency requires explicit update", func(t *testing.T) {
		env := newTestEnv(t, NewMemoryTransactionStore())
		oldData, newData, rootData := []byte("old-dependency"), []byte("new-dependency"), []byte("root")
		oldID := ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/locked-dependency", Version: "1.0.0"}
		newID := ArtifactIdentity{Kind: ArtifactPack, CanonicalID: oldID.CanonicalID, Version: "2.0.0"}
		rootID := ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/root-with-lock", Version: "1.0.0"}
		oldRelease := signedRelease(t, env.privateKey, oldID, oldData, nil)
		newRelease := signedRelease(t, env.privateKey, newID, newData, nil)
		rootRelease := signedRelease(t, env.privateKey, rootID, rootData, []Dependency{{Kind: ArtifactPack, CanonicalID: oldID.CanonicalID, VersionRange: ">=1.0.0"}})
		env.project.state.DependencySnapshot = ProjectDependencySnapshot{ResolvedLockDigest: testDigest('0'), Installs: []LockedArtifact{{Identity: oldID, SourceID: "official", PublisherID: oldRelease.PublisherID, Digest: oldRelease.Digest, ProvenanceDigest: oldRelease.ProvenanceDigest, DependencyMetadataDigest: oldRelease.DependencyMetadataDigest, Dependencies: []ArtifactIdentity{}}}}
		addRelease(env, oldRelease, oldData)
		addRelease(env, newRelease, newData)
		addRelease(env, rootRelease, rootData)
		if _, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, rootID)); !IsFailure(err, FailureDependencyConflict) {
			t.Fatalf("locked dependency silently upgraded: %v", err)
		}
	})

	t.Run("locked dependency requires exact authority binding", func(t *testing.T) {
		prepare := func(t *testing.T) (*testEnv, ArtifactRelease, ArtifactIdentity) {
			t.Helper()
			env := newTestEnv(t, NewMemoryTransactionStore())
			childData, rootData := []byte("authority-child"), []byte("authority-root")
			child := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/authority-child", Version: "1.0.0"}, childData, nil)
			rootID := ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/authority-root", Version: "1.0.0"}
			root := signedRelease(t, env.privateKey, rootID, rootData, []Dependency{{Kind: ArtifactPack, CanonicalID: child.Identity.CanonicalID, VersionRange: child.Identity.Version, DigestConstraint: child.Digest}})
			childPlan := PlanArtifact{Release: child}
			childLock, err := lockedFromPlan(childPlan, false, map[string]PlanArtifact{artifactKey(child.Identity): childPlan})
			if err != nil {
				t.Fatal(err)
			}
			env.project.state.DependencySnapshot = ProjectDependencySnapshot{ResolvedLockDigest: testDigest('0'), Installs: []LockedArtifact{childLock}}
			addRelease(env, child, childData)
			addRelease(env, root, rootData)
			return env, child, rootID
		}
		mutations := map[string]func(*LockedArtifact){
			"source":                     func(lock *LockedArtifact) { lock.SourceID = "other-source" },
			"publisher":                  func(lock *LockedArtifact) { lock.PublisherID = "other-publisher" },
			"provenance digest":          func(lock *LockedArtifact) { lock.ProvenanceDigest = testDigest('e') },
			"dependency metadata digest": func(lock *LockedArtifact) { lock.DependencyMetadataDigest = testDigest('e') },
		}
		for name, mutate := range mutations {
			t.Run(name, func(t *testing.T) {
				env, _, rootID := prepare(t)
				mutate(&env.project.state.DependencySnapshot.Installs[0])
				if _, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, rootID)); !IsFailure(err, FailureDependencyConflict) {
					t.Fatalf("dependency authority drift accepted: %v", err)
				}
			})
		}
		t.Run("unchanged", func(t *testing.T) {
			env, child, rootID := prepare(t)
			plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, rootID))
			if err != nil {
				t.Fatal(err)
			}
			for _, updated := range plan.ResolvedLockDelta.Updated {
				if artifactKey(updated.Identity) == artifactKey(child.Identity) {
					t.Fatalf("unchanged dependency appeared in lock update: %#v", updated)
				}
			}
		})
	})

	t.Run("lock stores exact resolved dependency identity", func(t *testing.T) {
		env := newTestEnv(t, NewMemoryTransactionStore())
		childData, rootData := []byte("range-child"), []byte("range-root")
		child := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/range-child", Version: "1.2.3"}, childData, nil)
		root := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/range-root", Version: "1.0.0"}, rootData, []Dependency{{Kind: ArtifactPack, CanonicalID: child.Identity.CanonicalID, VersionRange: "^1.0.0"}})
		addRelease(env, child, childData)
		addRelease(env, root, rootData)
		plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, root.Identity))
		if err != nil {
			t.Fatal(err)
		}
		var rootLock *LockedArtifact
		for index := range plan.ResolvedLockDelta.Added {
			if plan.ResolvedLockDelta.Added[index].Identity == root.Identity {
				rootLock = &plan.ResolvedLockDelta.Added[index]
			}
		}
		if rootLock == nil || len(rootLock.Dependencies) != 1 || rootLock.Dependencies[0] != child.Identity {
			t.Fatalf("dependency range escaped into resolved lock: %#v", rootLock)
		}
	})

	t.Run("locked dependency edge drift conflicts", func(t *testing.T) {
		env := newTestEnv(t, NewMemoryTransactionStore())
		leafData, middleData, rootData := []byte("edge-leaf"), []byte("edge-middle"), []byte("edge-root")
		leaf := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/edge-leaf", Version: "1.2.3"}, leafData, nil)
		middle := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/edge-middle", Version: "1.0.0"}, middleData, []Dependency{{Kind: ArtifactPack, CanonicalID: leaf.Identity.CanonicalID, VersionRange: "^1.0.0"}})
		root := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/edge-root", Version: "1.0.0"}, rootData, []Dependency{{Kind: ArtifactPack, CanonicalID: middle.Identity.CanonicalID, VersionRange: middle.Identity.Version}})
		resolved := map[string]PlanArtifact{artifactKey(leaf.Identity): {Release: leaf}, artifactKey(middle.Identity): {Release: middle}}
		middleLock, err := lockedFromPlan(resolved[artifactKey(middle.Identity)], false, resolved)
		if err != nil {
			t.Fatal(err)
		}
		middleLock.Dependencies[0].Version = "1.2.2"
		env.project.state.DependencySnapshot = ProjectDependencySnapshot{ResolvedLockDigest: testDigest('0'), Installs: []LockedArtifact{middleLock}}
		addRelease(env, leaf, leafData)
		addRelease(env, middle, middleData)
		addRelease(env, root, rootData)
		if _, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, root.Identity)); !IsFailure(err, FailureDependencyConflict) {
			t.Fatalf("dependency edge drift accepted: %v", err)
		}
	})

	t.Run("explicit root update persists complete lock binding", func(t *testing.T) {
		mutations := map[string]func(*LockedArtifact){
			"dependency metadata": func(lock *LockedArtifact) { lock.DependencyMetadataDigest = testDigest('e') },
			"direct dependencies": func(lock *LockedArtifact) {
				lock.Dependencies = []ArtifactIdentity{{Kind: ArtifactPack, CanonicalID: "critical/binding-child", Version: "1.2.2"}}
			},
			"source authority":    func(lock *LockedArtifact) { lock.SourceID = "other-source" },
			"publisher authority": func(lock *LockedArtifact) { lock.PublisherID = "other-publisher" },
			"provenance":          func(lock *LockedArtifact) { lock.ProvenanceDigest = testDigest('e') },
		}
		for name, mutate := range mutations {
			t.Run(name, func(t *testing.T) {
				env := newTestEnv(t, NewMemoryTransactionStore())
				childData, rootData := []byte("binding-child"), []byte("binding-root")
				child := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/binding-child", Version: "1.2.3"}, childData, nil)
				root := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/binding-root", Version: "1.0.0"}, rootData, []Dependency{{Kind: ArtifactPack, CanonicalID: child.Identity.CanonicalID, VersionRange: "^1.0.0"}})
				resolved := map[string]PlanArtifact{artifactKey(root.Identity): {Release: root}, artifactKey(child.Identity): {Release: child}}
				rootLock, err := lockedFromPlan(resolved[artifactKey(root.Identity)], true, resolved)
				if err != nil {
					t.Fatal(err)
				}
				childLock, err := lockedFromPlan(resolved[artifactKey(child.Identity)], false, resolved)
				if err != nil {
					t.Fatal(err)
				}
				mutate(&rootLock)
				env.project.state.DependencySnapshot = ProjectDependencySnapshot{ResolvedLockDigest: testDigest('0'), Installs: []LockedArtifact{rootLock, childLock}}
				addRelease(env, child, childData)
				addRelease(env, root, rootData)

				plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionUpdate, root.Identity))
				if err != nil {
					t.Fatal(err)
				}
				if len(plan.ResolvedLockDelta.Updated) != 1 {
					t.Fatalf("complete binding drift omitted from update: %#v", plan.ResolvedLockDelta)
				}
				updated := plan.ResolvedLockDelta.Updated[0]
				expected, err := lockedFromPlan(resolved[artifactKey(root.Identity)], false, resolved)
				if err != nil {
					t.Fatal(err)
				}
				if !lockedArtifactBindingMatches(updated, expected) || updated.Pinned || len(updated.Dependencies) != 1 || updated.Dependencies[0] != child.Identity {
					t.Fatalf("updated lock did not persist exact graph and pin: %#v", updated)
				}
				built := env.validator.capturedLastBuild()
				persisted, _, err := env.store.GetRegistryTransaction(context.Background(), plan.TransactionID)
				if err != nil || digestJSON(built.ResolvedLockDelta) != digestJSON(plan.ResolvedLockDelta) || digestJSON(persisted.Plan.ResolvedLockDelta) != digestJSON(plan.ResolvedLockDelta) {
					t.Fatalf("complete lock graph not persisted across plan boundaries: %#v %#v %v", built.ResolvedLockDelta, persisted.Plan.ResolvedLockDelta, err)
				}
			})
		}
	})

	t.Run("pin action only pins requested root", func(t *testing.T) {
		child := PlanArtifact{Release: ArtifactRelease{Identity: ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/pin-child", Version: "1.0.0"}, SourceID: "official", PublisherID: "layerdraw", Digest: testDigest('c'), ProvenanceDigest: testDigest('p'), DependencyMetadataDigest: testDigest('m')}}
		root := PlanArtifact{Release: ArtifactRelease{Identity: ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/pin-root", Version: "1.0.0"}, SourceID: "official", PublisherID: "layerdraw", Digest: testDigest('r'), ProvenanceDigest: testDigest('q'), DependencyMetadataDigest: testDigest('n'), Dependencies: []Dependency{{Kind: ArtifactPack, CanonicalID: child.Release.Identity.CanonicalID, VersionRange: child.Release.Identity.Version}}}}
		resolved := map[string]PlanArtifact{artifactKey(root.Release.Identity): root, artifactKey(child.Release.Identity): child}
		rootLock, _ := lockedFromPlan(root, false, resolved)
		childLock, _ := lockedFromPlan(child, true, resolved)
		delta, err := buildLockDelta(ActionPin, ProjectDependencySnapshot{Installs: []LockedArtifact{rootLock, childLock}}, resolved, root.Release.Identity, false)
		if err != nil || len(delta.Pinned) != 1 || delta.Pinned[0].Identity != root.Release.Identity || !delta.Pinned[0].Pinned || len(delta.Updated) != 0 {
			t.Fatalf("pin semantics changed dependency pins: %#v %v", delta, err)
		}
		delta, err = buildLockDelta(ActionPin, ProjectDependencySnapshot{Installs: []LockedArtifact{rootLock}}, resolved, root.Release.Identity, false)
		if err != nil || len(delta.Pinned) != 1 || len(delta.Added) != 1 || delta.Added[0].Identity != child.Release.Identity || delta.Added[0].Pinned {
			t.Fatalf("pin semantics implicitly pinned a new dependency: %#v %v", delta, err)
		}
		delta, err = buildLockDelta(ActionPin, ProjectDependencySnapshot{}, resolved, root.Release.Identity, false)
		if err != nil || len(delta.Pinned) != 1 || delta.Pinned[0].Identity != root.Release.Identity || len(delta.Added) != 1 {
			t.Fatalf("pin delta lost its explicit root classification: %#v %v", delta, err)
		}
	})

	t.Run("incomplete resolved dependency graph fails closed", func(t *testing.T) {
		identity := ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/incomplete-root", Version: "1.0.0"}
		root := PlanArtifact{Release: ArtifactRelease{Identity: identity, Dependencies: []Dependency{{Kind: ArtifactPack, CanonicalID: "critical/missing-child", VersionRange: "^1.0.0"}}}}
		if _, err := buildLockDelta(ActionInstall, ProjectDependencySnapshot{}, map[string]PlanArtifact{artifactKey(identity): root}, identity, false); err == nil {
			t.Fatal("incomplete resolved dependency graph produced a lock delta")
		}
	})

	t.Run("requested root is not artifact ordering", func(t *testing.T) {
		env := newTestEnv(t, NewMemoryTransactionStore())
		childData, rootData := []byte("ordered-child"), []byte("ordered-root")
		child := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "aaa/child", Version: "1.0.0"}, childData, nil)
		root := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "zzz/root", Version: "1.0.0"}, rootData, []Dependency{{Kind: ArtifactPack, CanonicalID: child.Identity.CanonicalID, VersionRange: "1.0.0", DigestConstraint: child.Digest}})
		addRelease(env, child, childData)
		addRelease(env, root, rootData)
		plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, root.Identity))
		lastBuild := env.validator.capturedLastBuild()
		if err != nil || plan.Artifacts[0].Release.Identity != child.Identity || plan.RequestedRoot != root.Identity || lastBuild.Requested != root.Identity {
			t.Fatalf("requested root inferred from ordering: %#v %#v %v", plan.RequestedRoot, lastBuild.Requested, err)
		}
		if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "root", IdempotencyKey: "root"}); err != nil || env.validator.capturedLastBuild().Requested != root.Identity {
			t.Fatalf("commit lost requested root: %#v %v", env.validator.capturedLastBuild().Requested, err)
		}
	})
}

func TestCriticalOperationIdentityAndSemverHardening(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	data := []byte("operation-identity")
	release := signedRelease(t, env.privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "critical/operation-identity", Version: "1.0.0"}, data, nil)
	addRelease(env, release, data)
	plan, err := env.registry.Plan(context.Background(), planRequest(env, ActionInstall, release.Identity))
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []RuntimeCommitInput{{Plan: plan, IdempotencyKey: "id"}, {Plan: plan, OperationID: "op"}} {
		if _, err := env.registry.Commit(context.Background(), input); !IsFailure(err, FailurePlanStale) || env.runtime.calls.Load() != 0 {
			t.Fatalf("empty operation identity reached Runtime: %v", err)
		}
	}
	ordered := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0"}
	for i := 0; i+1 < len(ordered); i++ {
		if compareVersions(ordered[i], ordered[i+1]) >= 0 {
			t.Fatalf("SemVer precedence %s >= %s", ordered[i], ordered[i+1])
		}
	}
	for _, invalid := range []string{"1.0.0-01", "1.0.0-", "1.0.0+", "1.0.0+a..b", "1.0.0+bad_underscore"} {
		if _, ok := parseVersion(invalid); ok {
			t.Fatalf("invalid SemVer accepted: %s", invalid)
		}
	}
	if compareVersions("1.0.0+build.1", "1.0.0+build.2") != 0 || !matchesRange("1.2.3+build.9", "^1.0.0", false) {
		t.Fatal("build metadata changed SemVer precedence")
	}
	for _, comparison := range []struct {
		left, right string
		want        int
	}{{"1.0.0-2", "1.0.0-3", -1}, {"1.0.0-alpha", "1.0.0-1", 1}, {"1.0.0-alpha.1", "1.0.0-alpha", 1}, {"1.0.0-alpha", "1.0.0-alpha", 0}} {
		got := compareVersions(comparison.left, comparison.right)
		if (comparison.want < 0 && got >= 0) || (comparison.want > 0 && got <= 0) || (comparison.want == 0 && got != 0) {
			t.Fatalf("SemVer comparison %s %s = %d", comparison.left, comparison.right, got)
		}
	}
	if isASCIIDigits("") {
		t.Fatal("empty numeric identifier accepted")
	}
	if compareVersions("1.0.0-beta.11", "1.0.0-beta.2") <= 0 {
		t.Fatal("numeric prerelease length precedence reversed")
	}
	if _, err := planBoundDocumentID(InstallPlan{CreatesNewDocument: true}); err == nil {
		t.Fatal("template without Document identity accepted")
	}
	if _, err := planBoundDocumentID(InstallPlan{HostOperationImpacts: []accessprotocol.HostOperationImpact{{ImpactDigest: protocolcommon.Digest(testDigest('h')), ResourceScope: accessprotocol.HostResourceScope{DocumentID: "doc"}}}}); err == nil {
		t.Fatal("host impact outside the plan digest binding accepted")
	}
	boundImpact := accessprotocol.HostOperationImpact{ImpactDigest: protocolcommon.Digest(testDigest('h')), ResourceScope: accessprotocol.HostResourceScope{DocumentID: "doc"}}
	if _, err := planBoundDocumentID(InstallPlan{CreatesNewDocument: true, NewDocumentID: "other-document", HostOperationImpactDigest: string(boundImpact.ImpactDigest), HostOperationImpacts: []accessprotocol.HostOperationImpact{boundImpact}}); err == nil {
		t.Fatal("template Document identity outside its host impact accepted")
	}
	if _, err := persistedRuntimeCommitResult(Transaction{Plan: InstallPlan{}, CommittedRevision: "r2", OperationResultID: "op", RuntimeResult: &RuntimeCommitResult{CommittedRevision: "other", OperationResultID: "op", DocumentID: "doc"}}, RuntimeCommitInput{OperationID: "op"}, "doc"); err == nil {
		t.Fatal("mismatched persisted Runtime convergence fields accepted")
	}
}

func TestCriticalDeepCloneAndLiveLockOwnership(t *testing.T) {
	impact := &semantic.AuthoringImpact{Entries: []semantic.AuthoringImpactEntry{{AfterRefs: []semantic.StableAddress{"project:p/entity:a"}}}, RequiredCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilitySchemaWrite}}
	plan := InstallPlan{Artifacts: []PlanArtifact{{Release: ArtifactRelease{Dependencies: []Dependency{{Kind: ArtifactPack, CanonicalID: "dep"}}, Compatibility: []CompatibilityDecision{{Diagnostics: []string{"original"}}}}}}, DependencySnapshot: ProjectDependencySnapshot{Installs: []LockedArtifact{{Dependencies: []ArtifactIdentity{{Kind: ArtifactPack, CanonicalID: "locked"}}}}}, AuthoringImpact: impact, HostOperationImpacts: []accessprotocol.HostOperationImpact{{ResourceRefs: []string{"root"}}}, ProjectMutationPlan: ProjectMutationPlan{SourceEdits: []SourceEdit{{Path: "main.ldl"}}, AuthoringImpact: *impact}}
	cloned := clonePlan(plan)
	cloned.Artifacts[0].Release.Dependencies[0].CanonicalID = "changed"
	cloned.Artifacts[0].Release.Compatibility[0].Diagnostics[0] = "changed"
	cloned.DependencySnapshot.Installs[0].Dependencies[0].CanonicalID = "changed"
	cloned.AuthoringImpact.Entries[0].AfterRefs[0] = "project:p/entity:changed"
	cloned.HostOperationImpacts[0].ResourceRefs[0] = "changed"
	cloned.ProjectMutationPlan.SourceEdits[0].Path = "changed"
	if plan.Artifacts[0].Release.Dependencies[0].CanonicalID != "dep" || plan.Artifacts[0].Release.Compatibility[0].Diagnostics[0] != "original" || plan.DependencySnapshot.Installs[0].Dependencies[0].CanonicalID != "locked" || plan.AuthoringImpact.Entries[0].AfterRefs[0] != "project:p/entity:a" || plan.HostOperationImpacts[0].ResourceRefs[0] != "root" || plan.ProjectMutationPlan.SourceEdits[0].Path != "main.ldl" {
		t.Fatal("clonePlan aliases nested mutable state")
	}
	request := PlanRequest{DependencySnapshot: plan.DependencySnapshot}
	runtimeInput := RuntimeCommitInput{Plan: plan, AuthoringImpact: impact, HostOperationImpacts: plan.HostOperationImpacts}
	transaction := Transaction{Plan: plan, PlanningRequest: &request, RuntimeInput: &runtimeInput, Events: []TransactionEvent{{State: StatePlanned, Sequence: 1}}}
	transactionClone := cloneTransaction(transaction)
	transactionClone.PlanningRequest.DependencySnapshot.Installs[0].Dependencies[0].CanonicalID = "changed"
	transactionClone.RuntimeInput.AuthoringImpact.Entries[0].AfterRefs[0] = "project:p/entity:changed"
	transactionClone.RuntimeInput.HostOperationImpacts[0].ResourceRefs[0] = "changed"
	if transaction.PlanningRequest.DependencySnapshot.Installs[0].Dependencies[0].CanonicalID != "locked" || transaction.RuntimeInput.AuthoringImpact.Entries[0].AfterRefs[0] != "project:p/entity:a" || transaction.RuntimeInput.HostOperationImpacts[0].ResourceRefs[0] != "root" {
		t.Fatal("cloneTransaction aliases nested mutable state")
	}
	_ = cloneLockDelta(ResolvedLockDelta{Added: []LockedArtifact{{Dependencies: []ArtifactIdentity{{CanonicalID: "deep"}}}}})
	if cloneAuthoringImpact(nil) != nil {
		t.Fatal("nil authoring impact clone changed")
	}

	root := t.TempDir()
	lockPath := filepath.Join(root, "transactions.lock")
	holder, err := NewDiskTransactionStore(root)
	if err != nil {
		t.Fatal(err)
	}
	release, err := holder.lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	old := time.Now().Add(-time.Hour)
	metadata, _ := json.Marshal(struct {
		PID        int       `json:"pid"`
		AcquiredAt time.Time `json:"acquired_at"`
	}{os.Getpid(), old})
	if err := os.WriteFile(lockPath, metadata, 0o600); err != nil {
		t.Fatal(err)
	}
	contender, err := NewDiskTransactionStore(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := contender.lock(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("old live lock was stolen: %v", err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("live lock removed: %v", err)
	}
}

func TestCriticalDiskLockNeverOverlapsAcrossStoreInstances(t *testing.T) {
	root := t.TempDir()
	stores := make([]*DiskTransactionStore, 12)
	for i := range stores {
		store, err := NewDiskTransactionStore(root)
		if err != nil {
			t.Fatal(err)
		}
		stores[i] = store
	}
	var inside atomic.Int64
	var overlap atomic.Bool
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, store := range stores {
		store := store
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < 40; i++ {
				release, err := store.lock(context.Background())
				if err != nil {
					overlap.Store(true)
					return
				}
				if inside.Add(1) != 1 {
					overlap.Store(true)
				}
				time.Sleep(time.Microsecond)
				inside.Add(-1)
				release()
			}
		}()
	}
	close(start)
	wg.Wait()
	if overlap.Load() || inside.Load() != 0 {
		t.Fatal("disk transaction lock allowed overlapping critical sections")
	}
}
