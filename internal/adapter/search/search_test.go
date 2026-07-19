// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package search

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type backendStub struct {
	rows      []port.RawRow
	err       error
	cancelled string
}

func (b *backendStub) ExecutePlan(_ context.Context, _ port.PlanKind, _ []byte) ([]port.RawRow, error) {
	return b.rows, b.err
}
func (b *backendStub) Cancel(_ context.Context, id string) error { b.cancelled = id; return b.err }

type verifierStub struct{ err error }

func (v verifierStub) VerifyPlan(context.Context, port.ExecutionPlan) error { return v.err }

func capability() port.QueryAdapterCapability {
	return port.QueryAdapterCapability{AdapterID: "native", Backend: "ladybug_native", BackendVersion: "1", PlanProtocolVersion: "v1", Primitives: append([]port.SearchPrimitive(nil), port.RequiredSearchPrimitives...), MaxRows: 2, MaxBytes: 64}
}
func validPlan(kind port.PlanKind) port.ExecutionPlan {
	return port.ExecutionPlan{Kind: kind, PlanID: "plan", ProtocolVersion: "v1", Token: "engine-token", Payload: []byte("opaque"), MaxRows: 2, MaxBytes: 64}
}

func TestNativeExecutorPreservesTypedRowsAndBounds(t *testing.T) {
	b := &backendStub{rows: []port.RawRow{{"address": {Kind: "string", Value: "a"}}, {"score": {Kind: "float", Value: "1"}}, {"extra": {Kind: "string", Value: "x"}}}}
	e, err := NewNativeExecutor(capability(), b, verifierStub{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.Execute(context.Background(), validPlan(port.PlanSearch))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != 2 || !got.Truncated || got.Rows[0]["address"].Value != "a" {
		t.Fatalf("unexpected result: %#v", got)
	}
	b.rows[0]["address"] = port.RawValue{Kind: "string", Value: "mutated"}
	if got.Rows[0]["address"].Value != "a" {
		t.Fatal("adapter returned backend-owned row map")
	}
	capGot, _ := e.Capabilities(context.Background())
	capGot.Primitives[0] = "changed"
	capAgain, _ := e.Capabilities(context.Background())
	if capAgain.Primitives[0] == "changed" {
		t.Fatal("capability slice aliased")
	}
	if err := e.Cancel(context.Background(), "plan"); err != nil || b.cancelled != "plan" {
		t.Fatal("cancel not forwarded")
	}
}

func TestNativeExecutorBoundsLargeProjectsAndWholeRows(t *testing.T) {
	rows := make([]port.RawRow, 10_000)
	for index := range rows {
		rows[index] = port.RawRow{"value": {Kind: "string", Value: "0123456789"}}
	}
	capability := capability()
	capability.MaxRows = 10_000
	executor, _ := NewNativeExecutor(capability, &backendStub{rows: rows}, verifierStub{})
	plan := validPlan(port.PlanSearch)
	plan.MaxRows = 10_000
	plan.MaxBytes = 64
	result, err := executor.Execute(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || result.Bytes > plan.MaxBytes || len(result.Rows) >= len(rows) {
		t.Fatalf("result=%#v", result)
	}
}

func TestNativeExecutorRejectsUnverifiedAndMalformedPlans(t *testing.T) {
	for name, plan := range map[string]port.ExecutionPlan{"empty": {}, "raw-kind": validPlan("raw_cypher"), "wrong-version": func() port.ExecutionPlan { p := validPlan(port.PlanQuery); p.ProtocolVersion = "v2"; return p }(), "unbounded": func() port.ExecutionPlan { p := validPlan(port.PlanQuery); p.MaxRows = 0; return p }()} {
		t.Run(name, func(t *testing.T) {
			e, _ := NewNativeExecutor(capability(), &backendStub{}, verifierStub{})
			if _, err := e.Execute(context.Background(), plan); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	e, _ := NewNativeExecutor(capability(), &backendStub{}, verifierStub{err: errors.New("bad token")})
	if _, err := e.Execute(context.Background(), validPlan(port.PlanAnalysis)); !errors.Is(err, ErrInvalidPlan) {
		t.Fatal(err)
	}
	if _, err := NewNativeExecutor(port.QueryAdapterCapability{}, nil, nil); err == nil {
		t.Fatal("invalid config accepted")
	}
	e, _ = NewNativeExecutor(capability(), &backendStub{err: errors.New("backend")}, verifierStub{})
	if _, err := e.Execute(context.Background(), validPlan(port.PlanQuery)); err == nil {
		t.Fatal("backend error lost")
	}
	if err := e.Cancel(context.Background(), ""); !errors.Is(err, ErrInvalidPlan) {
		t.Fatal(err)
	}
}

type modelStub struct {
	values []float32
	err    error
	seen   []string
}

func (m *modelStub) Embed(_ context.Context, text string) ([]float32, error) {
	m.seen = append(m.seen, text)
	return m.values, m.err
}
func embeddingCapability(remote bool) port.EmbeddingCapability {
	return port.EmbeddingCapability{ProviderID: "provider", Available: true, Remote: remote, Profiles: []port.EmbeddingProfile{{ProfileID: "default", ModelID: "m", ModelVersion: "1", ModelDigest: "sha256:model", Dimensions: 2, Normalization: "unit", MaxInputBytes: 32}}}
}

func TestEmbeddingProviderAcceptsOnlyConfiguredBoundedInputs(t *testing.T) {
	m := &modelStub{values: []float32{1, 2}}
	p, err := NewEmbeddingProvider(embeddingCapability(false), map[string]VectorModel{"default": m}, false)
	if err != nil {
		t.Fatal(err)
	}
	profile := embeddingCapability(false).Profiles[0]
	docs, err := p.EmbedDocuments(context.Background(), profile, []port.SearchDocumentInput{{SubjectAddress: "ldl:a", ContentHash: "sha256:a", Text: "allowed text"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].SubjectAddress != "ldl:a" || !reflect.DeepEqual(m.seen, []string{"allowed text"}) {
		t.Fatalf("docs=%#v seen=%#v", docs, m.seen)
	}
	query, err := p.EmbedQuery(context.Background(), profile, "query")
	if err != nil || len(query) != 2 {
		t.Fatalf("query=%v err=%v", query, err)
	}
	query[0] = 99
	again, _ := p.EmbedQuery(context.Background(), profile, "again")
	if again[0] == 99 {
		t.Fatal("vector aliased")
	}
	bad := profile
	bad.ModelVersion = "2"
	if _, err := p.EmbedQuery(context.Background(), bad, "x"); !errors.Is(err, ErrEmbeddingProfileMismatch) {
		t.Fatal(err)
	}
	if _, err := p.EmbedDocuments(context.Background(), profile, []port.SearchDocumentInput{{SubjectAddress: "a", ContentHash: "h", Text: string([]byte{0xff})}}); !errors.Is(err, ErrEmbeddingProfileMismatch) {
		t.Fatal(err)
	}
}

func TestEmbeddingProviderRemoteAndFailurePolicies(t *testing.T) {
	if _, err := NewEmbeddingProvider(embeddingCapability(true), map[string]VectorModel{"default": &modelStub{}}, false); !errors.Is(err, ErrRemoteEmbeddingDenied) {
		t.Fatal(err)
	}
	if _, err := NewEmbeddingProvider(port.EmbeddingCapability{}, nil, false); !errors.Is(err, ErrEmbeddingUnavailable) {
		t.Fatal(err)
	}
	m := &modelStub{values: []float32{1}}
	p, _ := NewEmbeddingProvider(embeddingCapability(false), map[string]VectorModel{"default": m}, false)
	if _, err := p.EmbedQuery(context.Background(), embeddingCapability(false).Profiles[0], "x"); !errors.Is(err, ErrEmbeddingProfileMismatch) {
		t.Fatal(err)
	}
	if described, err := p.Describe(context.Background()); err != nil || len(described.Profiles) != 1 {
		t.Fatalf("described=%#v err=%v", described, err)
	}
	failing := &modelStub{values: []float32{1, 2}, err: errors.New("offline")}
	p, _ = NewEmbeddingProvider(embeddingCapability(false), map[string]VectorModel{"default": failing}, false)
	if _, err := p.EmbedDocuments(context.Background(), embeddingCapability(false).Profiles[0], []port.SearchDocumentInput{{SubjectAddress: "a", ContentHash: "h", Text: "x"}}); err == nil {
		t.Fatal("model failure lost")
	}
}

func identity(revision string) port.SearchIndexIdentity {
	return port.SearchIndexIdentity{DocumentSnapshotRef: port.DocumentSnapshotRef{Kind: port.SnapshotHostRevision, HostDocumentID: "doc", CommittedRevision: revision, DefinitionHash: "sha256:def"}, SearchProfileID: "default", SearchProfileDigest: "sha256:search", EmbeddingProfileID: "embed", EmbeddingProfileDigest: "sha256:model", AccessProjectionDigest: "sha256:access", LadybugBackendVersion: "1", IndexSchemaVersion: "1"}
}

func TestDurableIndexBuildActivateRestartInvalidate(t *testing.T) {
	root := filepath.Join(t.TempDir(), "index")
	backend := &backendStub{}
	executor, _ := NewNativeExecutor(capability(), backend, verifierStub{})
	clock := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	store, err := NewDurableIndexStore(root, executor, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	input, err := store.ApplyPlan(context.Background(), identity("r1"), validPlan(port.PlanSearchIndex))
	if err != nil {
		t.Fatal(err)
	}
	building, err := store.Describe(context.Background(), identity("r1"))
	if err != nil || building.State != "building" {
		t.Fatalf("status=%#v err=%v", building, err)
	}
	active, err := store.Activate(context.Background(), input)
	if err != nil || active.State != "active" {
		t.Fatalf("status=%#v err=%v", active, err)
	}
	restarted, err := NewDurableIndexStore(root, executor, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := restarted.Describe(context.Background(), identity("r1"))
	if err != nil || got.State != "active" {
		t.Fatalf("status=%#v err=%v", got, err)
	}
	if _, err := restarted.Describe(context.Background(), identity("r2")); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("stale identity err=%v", err)
	}
	if err := restarted.Invalidate(context.Background(), identity("r1")); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Describe(context.Background(), identity("r1")); !errors.Is(err, port.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestDurableIndexRejectsInvalidAndCorruptState(t *testing.T) {
	root := filepath.Join(t.TempDir(), "index")
	executor, _ := NewNativeExecutor(capability(), &backendStub{}, verifierStub{})
	store, _ := NewDurableIndexStore(root, executor, nil)
	if _, err := store.ApplyPlan(context.Background(), identity("r1"), validPlan(port.PlanQuery)); !errors.Is(err, port.ErrConflict) {
		t.Fatal(err)
	}
	if _, err := store.Activate(context.Background(), port.SearchIndexApplyResult{Identity: identity("r1"), PlanID: "none"}); err == nil {
		t.Fatal("missing build accepted")
	}
	input, _ := store.ApplyPlan(context.Background(), identity("r1"), validPlan(port.PlanSearchIndex))
	key, _ := identityKey(identity("r1"))
	path := filepath.Join(root, key+".building.json")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Activate(context.Background(), input); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("err=%v", err)
	}
	if _, err := NewDurableIndexStore("relative", executor, nil); err == nil {
		t.Fatal("relative root accepted")
	}
	if _, err := store.ApplyPlan(context.Background(), port.SearchIndexIdentity{}, validPlan(port.PlanSearchIndex)); !errors.Is(err, port.ErrConflict) {
		t.Fatal(err)
	}
	if err := store.Invalidate(context.Background(), port.SearchIndexIdentity{}); !errors.Is(err, port.ErrConflict) {
		t.Fatal(err)
	}
}

func TestDurableIndexRetainsRecoverableBuildingStateOnBackendFailure(t *testing.T) {
	root := filepath.Join(t.TempDir(), "index")
	executor, _ := NewNativeExecutor(capability(), &backendStub{err: errors.New("interrupted")}, verifierStub{})
	store, _ := NewDurableIndexStore(root, executor, nil)
	if _, err := store.ApplyPlan(context.Background(), identity("r1"), validPlan(port.PlanSearchIndex)); err == nil {
		t.Fatal("backend failure lost")
	}
	status, err := store.Describe(context.Background(), identity("r1"))
	if err != nil || status.State != "building" {
		t.Fatalf("status=%#v err=%v", status, err)
	}
}
