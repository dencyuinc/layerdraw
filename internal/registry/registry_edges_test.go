// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registry

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
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
		{"validator error", func(e *testEnv, _ *PlanRequest) { e.validator.fail = true }, FailureArtifactCorrupt},
		{"validator mismatch", func(e *testEnv, _ *PlanRequest) { e.validator.mismatch = true }, FailureArtifactCorrupt},
		{"missing impact", func(e *testEnv, _ *PlanRequest) { e.validator.nilImpact = true }, FailureArtifactCorrupt},
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
		if err != nil || transactionState(recovered) != StateRepairRequired || lastIdempotencyKey(recovered) != "id" {
			t.Fatalf("recover: %#v %v", recovered, err)
		}
		again, err := env.registry.RecoverTransaction(context.Background(), plan.TransactionID)
		if err != nil || transactionState(again) != StateRepairRequired {
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
	if err := env.registry.resolve(context.Background(), one.Identity, false, map[string]PlanArtifact{}, map[string]bool{}); !IsFailure(err, FailureUnavailable) {
		t.Fatal(err)
	}
	env.client.downloadErr = nil
	env.client.bytes[one.Digest] = []byte("corrupt")
	if err := env.registry.resolve(context.Background(), one.Identity, false, map[string]PlanArtifact{}, map[string]bool{}); !IsFailure(err, FailureArtifactCorrupt) {
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
