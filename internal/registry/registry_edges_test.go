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
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

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
	if decision, err := verifyTrust(testNow, source, TrustPolicy{PolicyID: "optional"}, unsigned); err != nil || decision.Status != TrustUnsignedAllowed {
		t.Fatalf("optional unsigned: %#v %v", decision, err)
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
		if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan}); !IsFailure(err, FailurePlanStale) {
			t.Fatal(err)
		}
	})
	t.Run("project error", func(t *testing.T) {
		env, plan := prepare(t)
		env.project.err = errors.New("down")
		if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan}); !IsFailure(err, FailurePlanStale) {
			t.Fatal(err)
		}
	})
	t.Run("access denial", func(t *testing.T) {
		env, plan := prepare(t)
		env.access.deny = true
		if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan}); !IsFailure(err, FailurePlanStale) {
			t.Fatal(err)
		}
	})
	t.Run("prepublication rollback", func(t *testing.T) {
		env, plan := prepare(t)
		env.runtime.err = errors.New("before")
		if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, IdempotencyKey: "id"}); !IsFailure(err, FailureUnavailable) {
			t.Fatal(err)
		}
		tx, _ := env.registry.Transaction(plan.TransactionID)
		if transactionState(tx) != StateRolledBack {
			t.Fatal(tx.Events)
		}
		if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, IdempotencyKey: "id"}); !IsFailure(err, FailurePlanStale) {
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
		if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, IdempotencyKey: "id"}); !IsFailure(err, FailureRepairRequired) {
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
		if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan}); !IsFailure(err, FailurePlanStale) {
			t.Fatal(err)
		}
		if _, err := env.registry.GetTransaction(context.Background(), plan.TransactionID); !IsFailure(err, FailureUnavailable) {
			t.Fatal(err)
		}
	})
	t.Run("store CAS error", func(t *testing.T) {
		env, plan := prepare(t)
		env.registry.transactions = &faultStore{base: env.store, casErr: errors.New("disk")}
		if _, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan}); !IsFailure(err, FailureUnavailable) {
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
	tx := Transaction{Plan: InstallPlan{TransactionID: id, PlanDigest: "p"}, Events: []TransactionEvent{{State: StateAwaitingConfirmation, Sequence: 1}}}
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

	current := Transaction{Plan: InstallPlan{TransactionID: "tx", PlanDigest: "p"}, Events: []TransactionEvent{{State: StateRepairRequired, Sequence: 1}}}
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
		t.Fatal(err)
	}
	if env.project.currentCalls.Load() != before || plan.NewDocumentID != "new-document" || plan.BaseRevision != "" {
		t.Fatalf("template reused existing project: %#v", plan)
	}
	result, err := env.registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "template", IdempotencyKey: "template-id"})
	if err != nil || result.DocumentID != plan.NewDocumentID || !result.InitialCommittedRevision {
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
	env2.runtime.recovery = RuntimeRegistryOutcome{Status: RuntimeRegistryCommitted, Result: RuntimeCommitResult{CommittedRevision: "runtime-r2", OperationResultID: "runtime-op"}}
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
		plan := InstallPlan{TransactionID: id, PlanDigest: "plan", EvaluationDigest: testDigest('e'), ProjectMutationPlan: ProjectMutationPlan{StagedTreeManifest: testDigest('s'), AuthoringImpactDigest: testDigest('a')}}
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
		plan := InstallPlan{TransactionID: "repair-tx", PlanDigest: "plan", MutationDigest: testDigest('m')}
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
	plan := InstallPlan{TransactionID: "replay-tx", PlanDigest: "plan", MutationDigest: testDigest('m')}
	intent := RuntimeCommitInput{
		Plan:                 plan,
		OperationID:          "op",
		IdempotencyKey:       "replay-id",
		AccessDecision:       accessprotocol.AuthoringDecision{Outcome: accessprotocol.AuthoringDecisionOutcomeAllow},
		HostOperationImpacts: []accessprotocol.HostOperationImpact{{ImpactDigest: protocolcommon.Digest(testDigest('h'))}},
	}
	tx := Transaction{Plan: plan, RuntimeInput: &intent, Events: []TransactionEvent{{State: StateApplying, Sequence: 1, IdempotencyKey: "replay-id"}}}
	if err := env.store.CreateRegistryTransaction(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	result, err = env.registry.RecoverTransaction(context.Background(), "replay-tx")
	if err != nil || transactionState(result) != StateCommitted || env.runtime.calls.Load() != 1 {
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
	delta := buildLockDelta(ActionUpdate, snapshot, resolved, root)
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
		if err != nil || second.CommittedRevision != first.CommittedRevision || second.OperationResultID != first.OperationResultID {
			t.Fatalf("legacy result retry: %#v %v", second, err)
		}
	})

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
	if !processAlive(os.Getpid()) || processAlive(-1) {
		t.Fatal("process liveness detection failed")
	}
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

	liveLock, err := json.Marshal(struct {
		PID        int       `json:"pid"`
		AcquiredAt time.Time `json:"acquired_at"`
	}{os.Getpid(), time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "transactions.lock"), liveLock, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	if _, err := store.lock(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled lock wait=%v", err)
	}
	rootFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(rootFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDiskTransactionStore(rootFile); err == nil {
		t.Fatal("file accepted as transaction root")
	}
}

func TestCriticalRecoveryReplayFailure(t *testing.T) {
	env := newTestEnv(t, NewMemoryTransactionStore())
	env.runtime.err = errors.New("runtime unavailable")
	plan := InstallPlan{TransactionID: "replay-failure", PlanDigest: "plan", MutationDigest: testDigest('m')}
	intent := RuntimeCommitInput{Plan: plan, OperationID: "op", IdempotencyKey: "replay-failure"}
	tx := Transaction{Plan: plan, RuntimeInput: &intent, Events: []TransactionEvent{{State: StateApplying, Sequence: 1, IdempotencyKey: intent.IdempotencyKey}}}
	if err := env.store.CreateRegistryTransaction(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	recovered, err := env.registry.RecoverTransaction(context.Background(), plan.TransactionID)
	if !IsFailure(err, FailureRepairRequired) || transactionState(recovered) != StateNeedsReview || env.runtime.calls.Load() != 1 {
		t.Fatalf("replay failure: %#v %v", recovered, err)
	}
}

func TestCriticalRecoveryPersistenceFailures(t *testing.T) {
	for _, state := range []TransactionState{StatePlanned, StateApplying, StateRepairRequired} {
		t.Run(string(state), func(t *testing.T) {
			base := NewMemoryTransactionStore()
			env := newTestEnv(t, base)
			id := strings.Repeat(string(state[0]), 32)
			plan := InstallPlan{TransactionID: id, PlanDigest: "plan", MutationDigest: testDigest('m')}
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
