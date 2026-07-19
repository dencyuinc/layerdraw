// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registry

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"testing"
)

type memoryClient struct {
	releases []ArtifactRelease
	bytes    map[string][]byte
	offline  bool
}

func TestResolverAndOrderingEdges(t *testing.T) {
	r, client, _, _, privateKey := fixture(t)
	data := []byte("archive")
	old := signedRelease(t, privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/pack", Version: "1.0.0"}, data, nil)
	newer := signedRelease(t, privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/pack", Version: "1.1.0"}, data, nil)
	client.releases = []ArtifactRelease{old, newer}
	client.bytes[old.Digest] = data
	plan, err := r.Plan(context.Background(), PlanRequest{Action: ActionInstall, ProjectID: "p", BaseRevision: "r", Requested: ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/pack", Version: "latest"}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Artifacts[0].Release.Identity.Version != "1.1.0" {
		t.Fatalf("latest ordering: %#v", plan.Artifacts)
	}
	if _, err := r.Plan(context.Background(), PlanRequest{Action: ActionInstall, ProjectID: "p", BaseRevision: "r", Requested: ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/pack", Version: "9.0.0"}}); !IsFailure(err, FailureDependencyConflict) {
		t.Fatalf("missing exact: %v", err)
	}
	resolved := map[string]PlanArtifact{"pack:layerdraw/pack": {Release: old}}
	if err := r.resolve(context.Background(), newer.Identity, false, resolved, map[string]bool{}); !IsFailure(err, FailureDependencyConflict) {
		t.Fatalf("resolved conflict: %v", err)
	}
	if err := r.resolve(context.Background(), old.Identity, false, map[string]PlanArtifact{"pack:layerdraw/pack": {Release: old}}, map[string]bool{}); err != nil {
		t.Fatalf("same resolved release: %v", err)
	}
	if _, err := r.resolveVersion(context.Background(), Dependency{CanonicalID: "layerdraw/pack", VersionRange: "^9.0.0"}, false); !IsFailure(err, FailureDependencyConflict) {
		t.Fatalf("unmatched range: %v", err)
	}
	client.bytes = map[string][]byte{}
	if _, err := r.Plan(context.Background(), PlanRequest{Action: ActionInstall, ProjectID: "p", BaseRevision: "r", Requested: old.Identity}); !IsFailure(err, FailureUnavailable) {
		t.Fatalf("download failure: %v", err)
	}
	if compareVersions("bad", "worse") >= 0 || compareVersions("1.0.0-alpha", "1.0.0-beta") >= 0 || compareVersions("1.0.0", "1.0.0") != 0 {
		t.Fatal("fallback/prerelease ordering")
	}
	sources := []RegistrySource{{SourceID: "low", Priority: 1}, {SourceID: "high", Priority: 2}}
	releases := []ArtifactRelease{{Identity: ArtifactIdentity{CanonicalID: "b", Version: "1.0.0"}, SourceID: "low"}, {Identity: ArtifactIdentity{CanonicalID: "a", Version: "1.0.0"}, SourceID: "low"}, {Identity: ArtifactIdentity{CanonicalID: "a", Version: "1.0.0"}, SourceID: "high"}, {Identity: ArtifactIdentity{CanonicalID: "a", Version: "2.0.0"}, SourceID: "low"}}
	sortReleases(releases, sources)
	if releases[0].Identity.Version != "2.0.0" || releases[1].SourceID != "high" || releases[3].Identity.CanonicalID != "b" {
		t.Fatalf("release order: %#v", releases)
	}
	if err := r.PutTrustPolicy(TrustPolicy{PolicyID: "second"}); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureSource(RegistrySource{SourceID: "second", Kind: SourceOfficial, EndpointRef: "x", TrustPolicyID: "second", Priority: 200}); err != nil {
		t.Fatal(err)
	}
	if listed := r.Sources(); listed[0].SourceID != "second" {
		t.Fatalf("source priority: %#v", listed)
	}
	_, clients := r.snapshotSources()
	if clients[SourceOfficial] == nil {
		t.Fatal("client snapshot missing")
	}
}

func TestTrustDecisionEdges(t *testing.T) {
	public, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity := ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/pack", Version: "1.0.0"}
	release := signedRelease(t, privateKey, identity, []byte("archive"), nil)
	source := RegistrySource{SourceID: "s", Kind: SourceOfficial}
	policy := TrustPolicy{PolicyID: "p", RequiredSignature: true, PublicKeys: map[string]ed25519.PublicKey{"key-1": public}}
	badProfile := cloneRelease(release)
	badProfile.Signature.Profile = "sigstore"
	if err := verifyTrust(source, policy, badProfile); !IsFailure(err, FailureSignatureInvalid) {
		t.Fatalf("profile: %v", err)
	}
	badKey := cloneRelease(release)
	badKey.Signature.KeyID = "missing"
	if err := verifyTrust(source, policy, badKey); !IsFailure(err, FailureSignatureInvalid) {
		t.Fatalf("key: %v", err)
	}
	badStatement := cloneRelease(release)
	statement := []byte(`{"artifact_identity":{"kind":"pack","canonical_id":"other","version":"1.0.0"},"ArtifactDigest":"` + release.Digest + `","PublisherID":"layerdraw"}`)
	badStatement.Signature.Statement = statement
	badStatement.Signature.Signature = ed25519.Sign(privateKey, statement)
	if err := verifyTrust(source, policy, badStatement); !IsFailure(err, FailureSignatureInvalid) {
		t.Fatalf("statement: %v", err)
	}
	unsigned := cloneRelease(release)
	unsigned.Signature = nil
	local := source
	local.Kind = SourceLocalDirectory
	policy.AllowUnsignedLocal = true
	if err := verifyTrust(local, policy, unsigned); err != nil {
		t.Fatalf("local unsigned: %v", err)
	}
	policy.RequiredSignature = false
	if err := verifyTrust(source, policy, unsigned); err != nil {
		t.Fatalf("optional signature: %v", err)
	}
}

func TestDigestJSONRejectsUnsupportedValues(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("unsupported canonical value did not panic")
		}
	}()
	_ = digestJSON(make(chan int))
}

func (m *memoryClient) Search(_ context.Context, source RegistrySource, input SearchInput) ([]ArtifactRelease, error) {
	if m.offline {
		return nil, errors.New("offline")
	}
	out := []ArtifactRelease{}
	for _, release := range m.releases {
		if release.SourceID == source.SourceID && (input.Query == "" || release.Identity.CanonicalID == input.Query) {
			out = append(out, release)
		}
	}
	return out, nil
}
func (m *memoryClient) Download(_ context.Context, _ RegistrySource, release ArtifactRelease) ([]byte, error) {
	if m.offline {
		return nil, errors.New("offline")
	}
	value, ok := m.bytes[release.Digest]
	if !ok {
		return nil, errors.New("missing")
	}
	return append([]byte{}, value...), nil
}

type validator struct {
	calls    []ArtifactIdentity
	fail     bool
	mismatch bool
}

func (v *validator) ValidateRegistryArtifact(_ context.Context, release ArtifactRelease, _ []byte) (ValidatedArtifact, error) {
	v.calls = append(v.calls, release.Identity)
	if v.fail {
		return ValidatedArtifact{}, errors.New("Engine package validation failed")
	}
	if v.mismatch {
		return ValidatedArtifact{Identity: release.Identity, CanonicalDigest: digestBytes([]byte("other"))}, nil
	}
	return ValidatedArtifact{Identity: release.Identity, CanonicalDigest: release.Digest, StagedTreeManifest: digestJSON(release.Identity), ResolvedLockDigest: digestJSON(release.Dependencies), MutationDigest: digestJSON(release), AuthoringImpactDigest: digestJSON(release.Identity.CanonicalID)}, nil
}

type access struct{ deny bool }

func (a access) PreviewRegistryPlan(_ context.Context, _ InstallPlan) error {
	if a.deny {
		return errors.New("denied")
	}
	return nil
}

type runtime struct {
	calls int
	fail  bool
}

type author struct {
	result AuthoredArtifact
	err    error
}

func (a author) AuthorRegistryArtifact(_ context.Context, _ AuthorArtifactRequest) (AuthoredArtifact, error) {
	return a.result, a.err
}

func (r *runtime) CommitRegistryPlan(_ context.Context, input RuntimeCommitInput) (RuntimeCommitResult, error) {
	r.calls++
	if r.fail {
		return RuntimeCommitResult{}, errors.New("publication uncertain")
	}
	return RuntimeCommitResult{CommittedRevision: input.CurrentRevision + "-next", OperationResultID: input.OperationID}, nil
}

func fixture(t *testing.T) (*Registry, *memoryClient, *validator, *runtime, ed25519.PrivateKey) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	v := &validator{}
	rt := &runtime{}
	registry, err := New(v, access{}, rt)
	if err != nil {
		t.Fatal(err)
	}
	policy := TrustPolicy{PolicyID: "official", RequiredSignature: true, TrustedPublishers: map[string]bool{"layerdraw": true}, PublicKeys: map[string]ed25519.PublicKey{"key-1": privateKey.Public().(ed25519.PublicKey)}, RevokedKeys: map[string]bool{}}
	if err = registry.PutTrustPolicy(policy); err != nil {
		t.Fatal(err)
	}
	if err = registry.ConfigureSource(RegistrySource{SourceID: "official", Kind: SourceOfficial, EndpointRef: "https://registry.layerdraw.dev", TrustPolicyID: "official", CachePolicy: "verified", Priority: 100, Connected: true}); err != nil {
		t.Fatal(err)
	}
	client := &memoryClient{bytes: map[string][]byte{}}
	registry.RegisterClient(SourceOfficial, client)
	return registry, client, v, rt, privateKey
}

func signedRelease(t *testing.T, privateKey ed25519.PrivateKey, identity ArtifactIdentity, data []byte, dependencies []Dependency) ArtifactRelease {
	t.Helper()
	release := ArtifactRelease{Identity: identity, SourceID: "official", PublisherID: "layerdraw", Digest: digestBytes(data), Size: int64(len(data)), Dependencies: dependencies, Compatibility: []CompatibilityDecision{{Subject: "core", Required: ">=0.1.0", Available: "0.1.0", Status: "compatible"}}, License: "Apache-2.0", ProvenanceDigest: digestBytes([]byte("provenance"))}
	statement, err := json.Marshal(struct {
		Identity       ArtifactIdentity `json:"artifact_identity"`
		ArtifactDigest string           `json:"ArtifactDigest"`
		PublisherID    string           `json:"PublisherID"`
	}{identity, release.Digest, release.PublisherID})
	if err != nil {
		t.Fatal(err)
	}
	release.Signature = &SignatureEnvelope{Profile: "ed25519", KeyID: "key-1", Statement: statement, Signature: ed25519.Sign(privateKey, statement)}
	return release
}

func TestPlanResolvesVerifiesAndCommitsAtomically(t *testing.T) {
	registry, client, validator, runtime, privateKey := fixture(t)
	depBytes := []byte("dependency archive")
	rootBytes := []byte("template archive")
	dep := signedRelease(t, privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/base", Version: "1.2.0"}, depBytes, nil)
	root := signedRelease(t, privateKey, ArtifactIdentity{Kind: ArtifactTemplate, CanonicalID: "layerdraw/starter", Version: "2.0.0"}, rootBytes, []Dependency{{CanonicalID: "layerdraw/base", VersionRange: "^1.0.0", DigestConstraint: dep.Digest}})
	client.releases = []ArtifactRelease{root, dep}
	client.bytes[root.Digest] = rootBytes
	client.bytes[dep.Digest] = depBytes
	request := PlanRequest{Action: ActionCreateFromTemplate, ProjectID: "project-1", BaseRevision: "r1", ExpectedDefinitionHash: "definition", ExpectedResolvedLockDigest: "lock", Requested: root.Identity}
	plan, err := registry.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Artifacts) != 2 || plan.Artifacts[0].Release.Identity.CanonicalID != "layerdraw/base" || len(validator.calls) != 2 {
		t.Fatalf("unexpected resolved closure: %#v", plan.Artifacts)
	}
	if plan.PlanDigest == "" || plan.EvaluationDigest == "" || plan.RequiredCapabilities[0] != "package:manage" {
		t.Fatalf("incomplete plan: %#v", plan)
	}
	result, err := registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "op", IdempotencyKey: "idem", CurrentRevision: "r1", CurrentDefinitionHash: "definition", CurrentResolvedLockDigest: "lock"})
	if err != nil {
		t.Fatal(err)
	}
	if result.CommittedRevision != "r1-next" || runtime.calls != 1 {
		t.Fatalf("unexpected runtime result: %#v", result)
	}
	transaction, _ := registry.Transaction(plan.TransactionID)
	if transaction.Events[len(transaction.Events)-1].State != StateCommitted {
		t.Fatalf("not committed: %#v", transaction.Events)
	}
}

func TestTrustDependencyAndStaleFailuresFailClosed(t *testing.T) {
	registry, client, _, runtime, privateKey := fixture(t)
	bytes := []byte("archive")
	release := signedRelease(t, privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/pack", Version: "1.0.0"}, bytes, nil)
	client.releases = []ArtifactRelease{release}
	client.bytes[release.Digest] = bytes
	release.Signature.Signature[0] ^= 0xff
	client.releases[0] = release
	_, err := registry.Plan(context.Background(), PlanRequest{Action: ActionInstall, ProjectID: "p", BaseRevision: "r", ExpectedDefinitionHash: "d", ExpectedResolvedLockDigest: "l", Requested: release.Identity})
	if !IsFailure(err, FailureSignatureInvalid) {
		t.Fatalf("expected signature failure, got %v", err)
	}
	release = signedRelease(t, privateKey, release.Identity, bytes, nil)
	client.releases[0] = release
	plan, err := registry.Plan(context.Background(), PlanRequest{Action: ActionInstall, ProjectID: "p", BaseRevision: "r", ExpectedDefinitionHash: "d", ExpectedResolvedLockDigest: "l", Requested: release.Identity})
	if err != nil {
		t.Fatal(err)
	}
	_, err = registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "op", IdempotencyKey: "i", CurrentRevision: "changed", CurrentDefinitionHash: "d", CurrentResolvedLockDigest: "l"})
	if !IsFailure(err, FailurePlanStale) || runtime.calls != 0 {
		t.Fatalf("stale plan reached runtime: %v", err)
	}
	client.offline = true
	_, err = registry.Search(context.Background(), SearchInput{Query: "layerdraw/pack"})
	if !IsFailure(err, FailureUnavailable) {
		t.Fatalf("offline did not fail closed: %v", err)
	}
}

func TestSourceCredentialsRevocationAndRecoveryAreActionable(t *testing.T) {
	registry, client, _, runtime, privateKey := fixture(t)
	if err := registry.ConfigureSource(RegistrySource{SourceID: "bad", Kind: SourceSelfHosted, EndpointRef: "https://registry.example?token=redacted", TrustPolicyID: "official"}); !IsFailure(err, FailurePolicyDenied) {
		t.Fatalf("credential was accepted: %v", err)
	}
	bytes := []byte("archive")
	release := signedRelease(t, privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/pack", Version: "1.0.0"}, bytes, nil)
	client.releases = []ArtifactRelease{release}
	client.bytes[release.Digest] = bytes
	source, policy, _, _ := registry.sourceContext("official")
	policy.RevokedKeys["key-1"] = true
	if err := registry.PutTrustPolicy(policy); err != nil {
		t.Fatal(err)
	}
	_, err := registry.Plan(context.Background(), PlanRequest{Action: ActionInstall, ProjectID: "p", BaseRevision: "r", ExpectedDefinitionHash: "d", ExpectedResolvedLockDigest: "l", Requested: release.Identity})
	if !IsFailure(err, FailureSignatureRevoked) {
		t.Fatalf("revocation ignored: %v", err)
	}
	policy.RevokedKeys["key-1"] = false
	if err := registry.PutTrustPolicy(policy); err != nil {
		t.Fatal(err)
	}
	plan, err := registry.Plan(context.Background(), PlanRequest{Action: ActionRepair, ProjectID: "p", BaseRevision: "r", ExpectedDefinitionHash: "d", ExpectedResolvedLockDigest: "l", Requested: release.Identity})
	if err != nil {
		t.Fatal(err)
	}
	runtime.fail = true
	_, err = registry.Commit(context.Background(), RuntimeCommitInput{Plan: plan, OperationID: "op", IdempotencyKey: "id", CurrentRevision: "r", CurrentDefinitionHash: "d", CurrentResolvedLockDigest: "l"})
	if !IsFailure(err, FailureRepairRequired) {
		t.Fatalf("partial publication not recoverable: %v", err)
	}
	tx, _ := registry.Transaction(plan.TransactionID)
	if tx.Events[len(tx.Events)-1].State != StateRepairRequired {
		t.Fatalf("missing recovery state: %#v", tx.Events)
	}
	_ = source
}

func TestAuthoringAndHostBindingDelegateWithoutSemantics(t *testing.T) {
	r, client, validator, _, privateKey := fixture(t)
	if _, err := NewHostBinding(nil); !IsFailure(err, FailureUnavailable) {
		t.Fatalf("nil binding: %v", err)
	}
	binding, err := NewHostBinding(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(binding.ListSources()) != 1 {
		t.Fatal("binding lost sources")
	}
	if err := binding.SetConnected("official", false); err != nil {
		t.Fatal(err)
	}
	if err := binding.ConfigureSource(RegistrySource{SourceID: "second", Kind: SourceOfficial, EndpointRef: "registry:second", TrustPolicyID: "official"}); err != nil {
		t.Fatal(err)
	}
	if _, err := binding.Search(context.Background(), SearchInput{}); !IsFailure(err, FailureUnavailable) {
		t.Fatalf("binding search: %v", err)
	}
	if _, ok := binding.Transaction("missing"); ok {
		t.Fatal("binding transaction")
	}
	for _, request := range []AuthorArtifactRequest{{}, {Kind: ArtifactPack, ProjectID: "p", OutputName: "wrong.layerdraw", PublisherID: "layerdraw", Version: "1.0.0"}, {Kind: ArtifactTemplate, ProjectID: "p", OutputName: "wrong.ldpack", PublisherID: "layerdraw", Version: "1.0.0"}} {
		if _, err := binding.AuthorArtifact(context.Background(), request); !IsFailure(err, FailureUnsupportedFormat) {
			t.Fatalf("author request accepted: %#v %v", request, err)
		}
	}
	request := AuthorArtifactRequest{Kind: ArtifactPack, ProjectID: "p", OutputName: "pack.ldpack", PublisherID: "layerdraw", Version: "1.0.0"}
	if _, err := r.AuthorArtifact(context.Background(), request); !IsFailure(err, FailureUnavailable) {
		t.Fatalf("missing author: %v", err)
	}
	bytes := []byte("authored pack")
	release := signedRelease(t, privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/pack", Version: "1.0.0"}, bytes, nil)
	r.RegisterPackageAuthor(author{err: errors.New("write failed")})
	if _, err := r.AuthorArtifact(context.Background(), request); !IsFailure(err, FailureArtifactCorrupt) {
		t.Fatalf("author error: %v", err)
	}
	r.RegisterPackageAuthor(author{result: AuthoredArtifact{Release: release, Bytes: []byte("wrong")}})
	if _, err := r.AuthorArtifact(context.Background(), request); !IsFailure(err, FailureArtifactCorrupt) {
		t.Fatalf("author digest: %v", err)
	}
	r.RegisterPackageAuthor(author{result: AuthoredArtifact{Release: release, Bytes: bytes}})
	validator.fail = true
	if _, err := r.AuthorArtifact(context.Background(), request); !IsFailure(err, FailureArtifactCorrupt) {
		t.Fatalf("author validation: %v", err)
	}
	validator.fail = false
	result, err := r.AuthorArtifact(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	result.Bytes[0] = 'X'
	if bytes[0] == 'X' {
		t.Fatal("authored bytes aliased")
	}
	client.releases = []ArtifactRelease{release}
	client.bytes[release.Digest] = bytes
	if err := binding.SetConnected("official", true); err != nil {
		t.Fatal(err)
	}
	plan, err := binding.Plan(context.Background(), PlanRequest{Action: ActionInstall, ProjectID: "p", BaseRevision: "r", Requested: release.Identity})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := binding.Commit(context.Background(), RuntimeCommitInput{Plan: plan, CurrentRevision: "r"}); err != nil {
		t.Fatal(err)
	}
}

func TestConfigurationSearchAndValueContracts(t *testing.T) {
	if _, err := New(nil, access{}, &runtime{}); err == nil {
		t.Fatal("accepted nil validator")
	}
	if _, err := New(&validator{}, nil, &runtime{}); err == nil {
		t.Fatal("accepted nil access")
	}
	if _, err := New(&validator{}, access{}, nil); err == nil {
		t.Fatal("accepted nil runtime")
	}
	r, client, _, _, _ := fixture(t)
	if err := r.PutTrustPolicy(TrustPolicy{}); err == nil {
		t.Fatal("accepted empty policy")
	}
	if err := r.ConfigureSource(RegistrySource{}); err == nil {
		t.Fatal("accepted empty source")
	}
	if err := r.ConfigureSource(RegistrySource{SourceID: "unknown", Kind: SourceGit, EndpointRef: "git:repo", TrustPolicyID: "missing"}); !IsFailure(err, FailurePolicyDenied) {
		t.Fatalf("unknown policy: %v", err)
	}
	if err := r.SetConnected("missing", true); !IsFailure(err, FailureUnavailable) {
		t.Fatalf("missing source: %v", err)
	}
	if err := r.SetConnected("official", false); err != nil {
		t.Fatal(err)
	}
	if got := r.Sources(); len(got) != 1 || got[0].Connected {
		t.Fatalf("source clone/order: %#v", got)
	}
	if _, err := r.Search(context.Background(), SearchInput{}); !IsFailure(err, FailureUnavailable) {
		t.Fatalf("disconnected source: %v", err)
	}
	if err := r.SetConnected("official", true); err != nil {
		t.Fatal(err)
	}
	client.releases = []ArtifactRelease{{Identity: ArtifactIdentity{Kind: "invalid", CanonicalID: "x", Version: "1"}, SourceID: "official"}}
	if _, err := r.Search(context.Background(), SearchInput{}); !IsFailure(err, FailureUnavailable) {
		t.Fatalf("invalid release accepted: %v", err)
	}
	client.releases = nil
	client.offline = true
	if _, err := r.Search(context.Background(), SearchInput{}); !IsFailure(err, FailureUnavailable) {
		t.Fatalf("source error leaked: %v", err)
	}
	f := &Failure{Code: FailureUnavailable, Subject: "x", Cause: errors.New("cause")}
	if f.Error() != "registry.unavailable: x" || f.Unwrap() == nil {
		t.Fatalf("failure contract: %#v", f)
	}
	if _, ok := r.Transaction("missing"); ok {
		t.Fatal("unknown transaction exists")
	}
	if _, _, _, ok := r.sourceContext("missing"); ok {
		t.Fatal("unknown source context exists")
	}
}

func TestPlanningRejectsMalformedRequestsPoliciesAndEngineResults(t *testing.T) {
	r, client, v, _, privateKey := fixture(t)
	for _, request := range []PlanRequest{{}, {Action: ActionInstall, ProjectID: "p", BaseRevision: "r", Requested: ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "x", Version: ""}}, {Action: ActionCreateFromTemplate, ProjectID: "p", BaseRevision: "r", Requested: ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "x", Version: "1.0.0"}}} {
		if _, err := r.Plan(context.Background(), request); err == nil {
			t.Fatalf("accepted malformed request: %#v", request)
		}
	}
	data := []byte("archive")
	release := signedRelease(t, privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/pack", Version: "1.0.0"}, data, nil)
	client.releases = []ArtifactRelease{release}
	client.bytes[release.Digest] = data
	v.fail = true
	if _, err := r.Plan(context.Background(), PlanRequest{Action: ActionInstall, ProjectID: "p", BaseRevision: "r", Requested: release.Identity}); !IsFailure(err, FailureArtifactCorrupt) {
		t.Fatalf("validator failure: %v", err)
	}
	v.fail = false
	v.mismatch = true
	if _, err := r.Plan(context.Background(), PlanRequest{Action: ActionInstall, ProjectID: "p", BaseRevision: "r", Requested: release.Identity}); !IsFailure(err, FailureArtifactCorrupt) {
		t.Fatalf("validator mismatch: %v", err)
	}
	v.mismatch = false
	release.Compatibility = []CompatibilityDecision{{Subject: "render", Status: "incompatible"}}
	client.releases[0] = release
	if _, err := r.Plan(context.Background(), PlanRequest{Action: ActionInstall, ProjectID: "p", BaseRevision: "r", Requested: release.Identity}); !IsFailure(err, FailureIncompatibleCapability) {
		t.Fatalf("compatibility: %v", err)
	}
	release.Compatibility = nil
	client.releases[0] = release
	client.bytes[release.Digest] = []byte("wrong")
	if _, err := r.Plan(context.Background(), PlanRequest{Action: ActionInstall, ProjectID: "p", BaseRevision: "r", Requested: release.Identity}); !IsFailure(err, FailureArtifactCorrupt) {
		t.Fatalf("digest: %v", err)
	}
	client.bytes[release.Digest] = data
	r.access = access{deny: true}
	if _, err := r.Plan(context.Background(), PlanRequest{Action: ActionInstall, ProjectID: "p", BaseRevision: "r", Requested: release.Identity}); !IsFailure(err, FailurePolicyDenied) {
		t.Fatalf("access: %v", err)
	}
}

func TestDependencyCycleConflictAndVersionRules(t *testing.T) {
	r, client, _, _, privateKey := fixture(t)
	aBytes, bBytes := []byte("a"), []byte("b")
	a := signedRelease(t, privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/a", Version: "1.0.0"}, aBytes, []Dependency{{CanonicalID: "layerdraw/b", VersionRange: "^1.0.0"}})
	b := signedRelease(t, privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/b", Version: "1.1.0"}, bBytes, []Dependency{{CanonicalID: "layerdraw/a", VersionRange: "1.0.0"}})
	client.releases = []ArtifactRelease{a, b}
	client.bytes[a.Digest] = aBytes
	client.bytes[b.Digest] = bBytes
	if _, err := r.Plan(context.Background(), PlanRequest{Action: ActionInstall, ProjectID: "p", BaseRevision: "r", Requested: a.Identity}); !IsFailure(err, FailureDependencyCycle) {
		t.Fatalf("cycle: %v", err)
	}
	b.Dependencies = nil
	a.Dependencies[0].DigestConstraint = digestBytes([]byte("other"))
	client.releases = []ArtifactRelease{a, b}
	if _, err := r.Plan(context.Background(), PlanRequest{Action: ActionInstall, ProjectID: "p", BaseRevision: "r", Requested: a.Identity}); !IsFailure(err, FailureDependencyConflict) {
		t.Fatalf("digest constraint: %v", err)
	}
	for _, test := range []struct {
		version, expr string
		pre, want     bool
	}{{"1.2.3", "^1.0.0", false, true}, {"2.0.0", "^1.0.0", false, false}, {"1.2.3", "~1.2.0", false, true}, {"1.3.0", "~1.2.0", false, false}, {"1.2.3", ">=1.2.0", false, true}, {"1.0.0-beta", "*", false, false}, {"1.0.0-beta", "*", true, true}, {"bad", "*", true, false}, {"1.0.0", "1.0.0", false, true}} {
		if got := matchesRange(test.version, test.expr, test.pre); got != test.want {
			t.Errorf("matchesRange(%q,%q)=%v", test.version, test.expr, got)
		}
	}
	for _, value := range []string{"", "01.0.0", "1.x.0", "1.0"} {
		if _, ok := parseVersion(value); ok {
			t.Errorf("parsed %q", value)
		}
	}
	if compareVersions("1.0.0", "1.0.0-beta") <= 0 || compareVersions("1.0.0-beta", "1.0.0") >= 0 || compareVersions("1.0.1", "1.0.0") <= 0 || compareVersions("1.1.0", "1.0.9") <= 0 || compareVersions("2.0.0", "1.9.9") <= 0 {
		t.Fatal("semantic ordering")
	}
}

func TestTrustProfilesAndPlanIntegrity(t *testing.T) {
	r, client, _, runtime, privateKey := fixture(t)
	data := []byte("archive")
	release := signedRelease(t, privateKey, ArtifactIdentity{Kind: ArtifactPack, CanonicalID: "layerdraw/pack", Version: "1.0.0"}, data, nil)
	client.releases = []ArtifactRelease{release}
	client.bytes[release.Digest] = data
	source, policy, _, _ := r.sourceContext("official")
	policy.TrustedPublishers = map[string]bool{"other": true}
	if err := r.PutTrustPolicy(policy); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Plan(context.Background(), PlanRequest{Action: ActionInstall, ProjectID: "p", BaseRevision: "r", Requested: release.Identity}); !IsFailure(err, FailurePolicyDenied) {
		t.Fatalf("publisher: %v", err)
	}
	policy.TrustedPublishers = nil
	release.Signature = nil
	client.releases[0] = release
	if err := r.PutTrustPolicy(policy); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Plan(context.Background(), PlanRequest{Action: ActionInstall, ProjectID: "p", BaseRevision: "r", Requested: release.Identity}); !IsFailure(err, FailureSignatureMissing) {
		t.Fatalf("missing signature: %v", err)
	}
	policy.RequiredSignature = false
	if err := r.PutTrustPolicy(policy); err != nil {
		t.Fatal(err)
	}
	plan, err := r.Plan(context.Background(), PlanRequest{Action: ActionInstall, ProjectID: "p", BaseRevision: "r", Requested: release.Identity})
	if err != nil {
		t.Fatal(err)
	}
	tampered := plan
	tampered.Action = ActionRemove
	if _, err := r.Commit(context.Background(), RuntimeCommitInput{Plan: tampered, CurrentRevision: "r"}); !IsFailure(err, FailurePlanStale) {
		t.Fatalf("tampered plan: %v", err)
	}
	if runtime.calls != 0 {
		t.Fatal("tampered plan reached runtime")
	}
	localPolicy := TrustPolicy{PolicyID: "local", RequiredSignature: true, AllowUnsignedLocal: true}
	if err := r.PutTrustPolicy(localPolicy); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureSource(RegistrySource{SourceID: "local", Kind: SourceLocalDirectory, EndpointRef: "file:registry", TrustPolicyID: "local", Connected: true, Priority: 1}); err != nil {
		t.Fatal(err)
	}
	localClient := &memoryClient{releases: []ArtifactRelease{{Identity: release.Identity, SourceID: "local", PublisherID: "local", Digest: release.Digest, Size: int64(len(data)), License: "x"}}, bytes: map[string][]byte{release.Digest: data}}
	r.RegisterClient(SourceLocalDirectory, localClient)
	_ = source
}
