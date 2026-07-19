// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package runtime

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type searchEngineStub struct {
	prepareErr, completeErr error
	prepared                port.SearchPreparationInput
	result                  []byte
}

func (e *searchEngineStub) PrepareSearchIndex(_ context.Context, _ port.SearchIndexPreparationInput) (port.ExecutionPlan, error) {
	return testPlan(port.PlanSearchIndex), e.prepareErr
}

func (e *searchEngineStub) PrepareSearch(_ context.Context, in port.SearchPreparationInput) (port.PreparedSearch, error) {
	e.prepared = in
	return port.PreparedSearch{Plan: testPlan(port.PlanSearch), QueryDigest: "query"}, e.prepareErr
}
func (e *searchEngineStub) CompleteSearch(_ context.Context, _ port.CompleteSearchInput) ([]byte, error) {
	if e.result != nil {
		return e.result, e.completeErr
	}
	return []byte(`{"hits":[]}`), e.completeErr
}
func (e *searchEngineStub) PrepareQuery(context.Context, port.BoundExecutionRequest) (port.ExecutionPlan, error) {
	return testPlan(port.PlanQuery), e.prepareErr
}
func (e *searchEngineStub) CompleteQuery(context.Context, port.CompleteExecutionInput) ([]byte, error) {
	return []byte(`{"query":[]}`), e.completeErr
}
func (e *searchEngineStub) PrepareAnalysis(context.Context, port.BoundExecutionRequest) (port.ExecutionPlan, error) {
	return testPlan(port.PlanAnalysis), e.prepareErr
}
func (e *searchEngineStub) CompleteAnalysis(context.Context, port.CompleteExecutionInput) ([]byte, error) {
	return []byte(`{"analysis":[]}`), e.completeErr
}

type executorStub struct {
	capability port.QueryAdapterCapability
	rows       port.ExecutionResult
	err        error
}

func (e executorStub) Capabilities(context.Context) (port.QueryAdapterCapability, error) {
	return e.capability, e.err
}
func (e executorStub) Execute(context.Context, port.ExecutionPlan) (port.ExecutionResult, error) {
	return e.rows, e.err
}
func (e executorStub) Cancel(context.Context, string) error { return e.err }

type indexStub struct {
	status port.SearchIndexStatus
	err    error
}

type buildIndexStub struct {
	status                port.SearchIndexStatus
	applyErr, activateErr error
}

func (i buildIndexStub) Describe(context.Context, port.SearchIndexIdentity) (port.SearchIndexStatus, error) {
	return i.status, nil
}
func (i buildIndexStub) ApplyPlan(_ context.Context, id port.SearchIndexIdentity, _ port.ExecutionPlan) (port.SearchIndexApplyResult, error) {
	return port.SearchIndexApplyResult{Identity: id, PlanID: "p"}, i.applyErr
}
func (i buildIndexStub) Activate(context.Context, port.SearchIndexApplyResult) (port.SearchIndexStatus, error) {
	return i.status, i.activateErr
}
func (i buildIndexStub) Invalidate(context.Context, port.SearchIndexIdentity) error { return nil }

func (i indexStub) Describe(context.Context, port.SearchIndexIdentity) (port.SearchIndexStatus, error) {
	return i.status, i.err
}
func (i indexStub) ApplyPlan(context.Context, port.SearchIndexIdentity, port.ExecutionPlan) (port.SearchIndexApplyResult, error) {
	return port.SearchIndexApplyResult{Identity: i.status.Identity, PlanID: "p"}, i.err
}
func (i indexStub) Activate(context.Context, port.SearchIndexApplyResult) (port.SearchIndexStatus, error) {
	return i.status, i.err
}
func (i indexStub) Invalidate(context.Context, port.SearchIndexIdentity) error { return i.err }

type embeddingStub struct {
	values    []float32
	vectors   []port.EmbeddingVector
	err       error
	available bool
}

func (e embeddingStub) Describe(context.Context) (port.EmbeddingCapability, error) {
	return port.EmbeddingCapability{ProviderID: "p", Available: e.available}, e.err
}
func (e embeddingStub) EmbedDocuments(_ context.Context, _ port.EmbeddingProfile, b port.SearchDocumentBatch) ([]port.EmbeddingVector, error) {
	if e.vectors != nil {
		return e.vectors, e.err
	}
	if len(b.Documents) == 0 {
		return nil, e.err
	}
	return []port.EmbeddingVector{{SubjectAddress: b.Documents[0].SubjectAddress, ContentHash: b.Documents[0].ContentHash, Values: e.values}}, e.err
}
func (e embeddingStub) EmbedQuery(context.Context, port.EmbeddingProfile, string) ([]float32, error) {
	return e.values, e.err
}

type batchVerifierStub struct{ err error }

func (v batchVerifierStub) VerifySearchDocumentBatch(context.Context, port.SearchDocumentBatch) error {
	return v.err
}

func testPlan(kind port.PlanKind) port.ExecutionPlan {
	return port.ExecutionPlan{Kind: kind, PlanID: "p", ProtocolVersion: "v1", Token: "t", Payload: []byte("x"), MaxRows: 10, MaxBytes: 1024}
}
func testIdentity() port.SearchIndexIdentity {
	return port.SearchIndexIdentity{DocumentSnapshotRef: port.DocumentSnapshotRef{Kind: port.SnapshotHostRevision, HostDocumentID: "doc", CommittedRevision: "r1", DefinitionHash: "sha256:def"}, SearchProfileID: "search", SearchProfileDigest: "sha256:search", EmbeddingProfileID: "embed", EmbeddingProfileDigest: "sha256:model", AccessProjectionDigest: "sha256:access", LadybugBackendVersion: "1", IndexSchemaVersion: "1"}
}
func testRequest(mode string) SearchRequest {
	profile := port.EmbeddingProfile{ProfileID: "embed", ModelID: "m", ModelVersion: "1", ModelDigest: "sha256:model", Dimensions: 2, Normalization: "unit", MaxInputBytes: 100}
	r := SearchRequest{Snapshot: testIdentity().DocumentSnapshotRef, AccessProjectionDigest: "sha256:access", SearchProfile: port.SearchProfile{ProfileID: "search", SpecificationDigest: "sha256:search", LexicalCandidateLimit: 10, SemanticCandidateLimit: 10, MaxHits: 5}, IndexIdentity: testIdentity(), Mode: mode, QueryText: "hello", EngineRequest: []byte(`{"text":"hello"}`), MaxOutputBytes: 1024}
	if mode != "lexical" {
		r.EmbeddingProfile = &profile
	}
	return r
}
func allCapabilities() port.QueryAdapterCapability {
	return port.QueryAdapterCapability{AdapterID: "native", BackendVersion: "1", PlanProtocolVersion: "v1", Primitives: append([]port.SearchPrimitive(nil), port.RequiredSearchPrimitives...), MaxRows: 100, MaxBytes: 1024}
}

func TestSearchServiceCapabilitiesAreSharedAndEmbeddingIsTypedOptional(t *testing.T) {
	s := NewSearchService(&searchEngineStub{}, executorStub{capability: allCapabilities()}, indexStub{}, nil)
	m, err := s.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !m.QueryAvailable || !m.SearchAvailable || !m.AnalysisAvailable || m.EmbeddingAvailable || m.EmbeddingReason == "" {
		t.Fatalf("manifest=%#v", m)
	}
	s = NewSearchService(&searchEngineStub{}, executorStub{capability: allCapabilities()}, indexStub{}, embeddingStub{available: true})
	m, err = s.Capabilities(context.Background())
	if err != nil || !m.EmbeddingAvailable {
		t.Fatalf("manifest=%#v err=%v", m, err)
	}
	missing := allCapabilities()
	missing.Primitives = missing.Primitives[1:]
	s = NewSearchService(&searchEngineStub{}, executorStub{capability: missing}, indexStub{}, nil)
	missingManifest, err := s.Capabilities(context.Background())
	if err != nil || missingManifest.QueryAvailable || !missingManifest.SearchAvailable || !missingManifest.AnalysisAvailable {
		t.Fatalf("manifest=%#v err=%v", missingManifest, err)
	}
}

func TestSearchBindsRevisionAccessProfilesAndProvider(t *testing.T) {
	engine := &searchEngineStub{}
	id := testIdentity()
	s := NewSearchService(engine, executorStub{}, indexStub{status: port.SearchIndexStatus{Identity: id, State: "active"}}, embeddingStub{values: []float32{.1, .2}, available: true})
	got, err := s.Search(context.Background(), testRequest("hybrid"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"hits":[]}` {
		t.Fatalf("got=%s", got)
	}
	if engine.prepared.Snapshot != id.DocumentSnapshotRef || engine.prepared.AccessProjectionDigest != "sha256:access" || len(engine.prepared.QueryEmbedding) != 2 || engine.prepared.IndexIdentity != id {
		t.Fatalf("binding=%#v", engine.prepared)
	}
}

func TestSearchReturnsDeterministicCrossSurfaceEngineResult(t *testing.T) {
	id := testIdentity()
	service := NewSearchService(&searchEngineStub{}, executorStub{}, indexStub{status: port.SearchIndexStatus{Identity: id, State: "active"}}, nil)
	gui, err := service.Search(context.Background(), testRequest("lexical"))
	if err != nil {
		t.Fatal(err)
	}
	mcp, err := service.Search(context.Background(), testRequest("lexical"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gui) != string(mcp) {
		t.Fatalf("gui=%s mcp=%s", gui, mcp)
	}
}

func TestSearchRejectsStaleUnavailableAndProviderFailure(t *testing.T) {
	id := testIdentity()
	active := indexStub{status: port.SearchIndexStatus{Identity: id, State: "active"}}
	for name, testCase := range map[string]struct {
		s    *SearchService
		r    SearchRequest
		want error
	}{
		"not-ready":       {NewSearchService(&searchEngineStub{}, executorStub{}, indexStub{err: port.ErrNotFound}, nil), testRequest("lexical"), ErrSearchIndexNotReady},
		"building":        {NewSearchService(&searchEngineStub{}, executorStub{}, indexStub{status: port.SearchIndexStatus{Identity: id, State: "building"}}, nil), testRequest("lexical"), ErrSearchIndexNotReady},
		"provider-failed": {NewSearchService(&searchEngineStub{}, executorStub{}, active, embeddingStub{err: errors.New("offline")}), testRequest("semantic"), ErrSearchEmbeddingUnavailable},
		"wrong-dimension": {NewSearchService(&searchEngineStub{}, executorStub{}, active, embeddingStub{values: []float32{1}}), testRequest("semantic"), ErrSearchEmbeddingProfile},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := testCase.s.Search(context.Background(), testCase.r)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("err=%v want=%v", err, testCase.want)
			}
		})
	}
	r := testRequest("lexical")
	r.IndexIdentity.AccessProjectionDigest = "sha256:other"
	s := NewSearchService(&searchEngineStub{}, executorStub{}, active, nil)
	if _, err := s.Search(context.Background(), r); !errors.Is(err, ErrSearchIndexStale) {
		t.Fatal(err)
	}
}

func TestSearchRejectsNonFiniteQueryEmbeddingBeforeEngine(t *testing.T) {
	for name, values := range map[string][]float32{
		"nan":          {float32(math.NaN()), 1},
		"positive-inf": {float32(math.Inf(1)), 1},
		"negative-inf": {float32(math.Inf(-1)), 1},
	} {
		t.Run(name, func(t *testing.T) {
			engine := &searchEngineStub{}
			service := NewSearchService(engine, executorStub{}, indexStub{status: port.SearchIndexStatus{Identity: testIdentity(), State: "active"}}, embeddingStub{values: values})
			if _, err := service.Search(context.Background(), testRequest("semantic")); !errors.Is(err, ErrSearchEmbeddingProfile) {
				t.Fatalf("err=%v", err)
			}
			if len(engine.prepared.QueryEmbedding) != 0 || engine.prepared.Request != nil {
				t.Fatal("non-finite embedding reached Engine")
			}
		})
	}
}

func TestQueryAndAnalysisUseEnginePreparedPlans(t *testing.T) {
	s := NewSearchService(&searchEngineStub{}, executorStub{}, nil, nil)
	input := port.BoundExecutionRequest{Snapshot: testIdentity().DocumentSnapshotRef, AccessProjectionDigest: "a", Request: []byte("opaque"), MaxOutputBytes: 100}
	if got, err := s.ExecuteQuery(context.Background(), input); err != nil || string(got) != `{"query":[]}` {
		t.Fatalf("got=%s err=%v", got, err)
	}
	if got, err := s.ExecuteAnalysis(context.Background(), input); err != nil || string(got) != `{"analysis":[]}` {
		t.Fatalf("got=%s err=%v", got, err)
	}
	s = NewSearchService(&searchEngineStub{}, executorStub{err: context.Canceled}, nil, nil)
	if _, err := s.ExecuteQuery(context.Background(), input); !errors.Is(err, ErrSearchCancelled) {
		t.Fatal(err)
	}
	if _, err := s.ExecuteAnalysis(context.Background(), port.BoundExecutionRequest{}); !errors.Is(err, ErrAnalysisInvalidScope) {
		t.Fatal(err)
	}
}

func TestSearchServiceFailureNormalizationAndValidation(t *testing.T) {
	ctx := context.Background()
	if _, err := NewSearchService(nil, nil, nil, nil).Capabilities(ctx); !errors.Is(err, ErrSearchCapabilityMissing) {
		t.Fatal(err)
	}
	if _, err := NewSearchService(&searchEngineStub{}, executorStub{err: errors.New("offline")}, indexStub{}, nil).Capabilities(ctx); !errors.Is(err, ErrSearchBackendFailed) {
		t.Fatal(err)
	}
	m, err := NewSearchService(&searchEngineStub{}, executorStub{capability: allCapabilities()}, indexStub{}, embeddingStub{err: errors.New("offline")}).Capabilities(ctx)
	if err != nil || m.EmbeddingReason == "" {
		t.Fatalf("manifest=%#v err=%v", m, err)
	}
	active := indexStub{status: port.SearchIndexStatus{Identity: testIdentity(), State: "active"}}
	invalid := []SearchRequest{testRequest("bad"), testRequest("lexical"), testRequest("semantic")}
	invalid[1].QueryText = ""
	invalid[2].EmbeddingProfile = nil
	for _, request := range invalid {
		if _, err := NewSearchService(&searchEngineStub{}, executorStub{}, active, nil).Search(ctx, request); !errors.Is(err, ErrSearchInvalidRequest) {
			t.Fatalf("request=%#v err=%v", request, err)
		}
	}
	if _, err := NewSearchService(nil, nil, nil, nil).Search(ctx, testRequest("lexical")); !errors.Is(err, ErrSearchCapabilityMissing) {
		t.Fatal(err)
	}
	if _, err := NewSearchService(&searchEngineStub{}, executorStub{}, indexStub{err: errors.New("disk")}, nil).Search(ctx, testRequest("lexical")); !errors.Is(err, ErrSearchBackendFailed) {
		t.Fatal(err)
	}
	wrong := testIdentity()
	wrong.SearchProfileID = "other"
	if _, err := NewSearchService(&searchEngineStub{}, executorStub{}, indexStub{status: port.SearchIndexStatus{Identity: wrong, State: "active"}}, nil).Search(ctx, testRequest("lexical")); !errors.Is(err, ErrSearchIndexStale) {
		t.Fatal(err)
	}
	if _, err := NewSearchService(&searchEngineStub{prepareErr: errors.New("bad")}, executorStub{}, active, nil).Search(ctx, testRequest("lexical")); !errors.Is(err, ErrSearchInvalidRequest) {
		t.Fatal(err)
	}
	if _, err := NewSearchService(&searchEngineStub{}, executorStub{err: errors.New("backend")}, active, nil).Search(ctx, testRequest("lexical")); !errors.Is(err, ErrSearchBackendFailed) {
		t.Fatal(err)
	}
	if _, err := NewSearchService(&searchEngineStub{}, executorStub{err: context.Canceled}, active, nil).Search(ctx, testRequest("lexical")); !errors.Is(err, ErrSearchCancelled) {
		t.Fatal(err)
	}
	if _, err := NewSearchService(&searchEngineStub{completeErr: errors.New("rows")}, executorStub{}, active, nil).Search(ctx, testRequest("lexical")); !errors.Is(err, ErrSearchBackendFailed) {
		t.Fatal(err)
	}
	if _, err := NewSearchService(&searchEngineStub{result: make([]byte, 2048)}, executorStub{}, active, nil).Search(ctx, testRequest("lexical")); !errors.Is(err, ErrSearchInvalidRequest) {
		t.Fatal(err)
	}
	if _, err := NewSearchService(&searchEngineStub{}, executorStub{}, active, embeddingStub{err: context.Canceled}).Search(ctx, testRequest("semantic")); !errors.Is(err, ErrSearchCancelled) {
		t.Fatal(err)
	}
}

func TestQueryAndAnalysisEngineFailures(t *testing.T) {
	ctx := context.Background()
	input := port.BoundExecutionRequest{MaxOutputBytes: 1}
	if _, err := NewSearchService(nil, nil, nil, nil).ExecuteQuery(ctx, input); !errors.Is(err, ErrSearchInvalidRequest) {
		t.Fatal(err)
	}
	if _, err := NewSearchService(&searchEngineStub{prepareErr: errors.New("bad")}, executorStub{}, nil, nil).ExecuteQuery(ctx, input); !errors.Is(err, ErrSearchInvalidRequest) {
		t.Fatal(err)
	}
	if _, err := NewSearchService(&searchEngineStub{completeErr: errors.New("bad")}, executorStub{}, nil, nil).ExecuteQuery(ctx, input); err == nil {
		t.Fatal("completion error lost")
	}
	if _, err := NewSearchService(&searchEngineStub{prepareErr: errors.New("bad")}, executorStub{}, nil, nil).ExecuteAnalysis(ctx, input); !errors.Is(err, ErrAnalysisInvalidScope) {
		t.Fatal(err)
	}
	if _, err := NewSearchService(&searchEngineStub{completeErr: errors.New("bad")}, executorStub{}, nil, nil).ExecuteAnalysis(ctx, input); err == nil {
		t.Fatal("completion error lost")
	}
}

func TestRebuildIndexEmbedsFilteredDocumentsAndActivates(t *testing.T) {
	id := testIdentity()
	profile := testRequest("semantic").EmbeddingProfile
	batch := port.SearchDocumentBatch{Snapshot: id.DocumentSnapshotRef, AccessProjectionDigest: id.AccessProjectionDigest, EmbeddingProfileDigest: profile.ModelDigest, Documents: []port.SearchDocumentInput{{SubjectAddress: "a", ContentHash: "h", Text: "allowed"}}, Token: "verified"}
	request := SearchIndexBuildRequest{Snapshot: id.DocumentSnapshotRef, AccessProjectionDigest: id.AccessProjectionDigest, SearchProfile: testRequest("lexical").SearchProfile, EmbeddingProfile: profile, IndexIdentity: id, Batch: batch}
	service := NewVerifiedSearchService(&searchEngineStub{}, executorStub{}, indexStub{status: port.SearchIndexStatus{Identity: id, State: "active"}}, embeddingStub{values: []float32{1, 2}}, batchVerifierStub{})
	status, err := service.RebuildIndex(context.Background(), request)
	if err != nil || status.State != "active" {
		t.Fatalf("status=%#v err=%v", status, err)
	}
	service = NewSearchService(&searchEngineStub{}, executorStub{}, indexStub{status: port.SearchIndexStatus{Identity: id}}, nil)
	if _, err := service.RebuildIndex(context.Background(), request); !errors.Is(err, ErrSearchEmbeddingUnavailable) {
		t.Fatal(err)
	}
	service = NewVerifiedSearchService(&searchEngineStub{}, executorStub{}, indexStub{status: port.SearchIndexStatus{Identity: id}}, embeddingStub{values: []float32{1}}, batchVerifierStub{})
	if _, err := service.RebuildIndex(context.Background(), request); !errors.Is(err, ErrSearchEmbeddingProfile) {
		t.Fatal(err)
	}
	service = NewVerifiedSearchService(&searchEngineStub{}, executorStub{}, indexStub{status: port.SearchIndexStatus{Identity: id}}, embeddingStub{vectors: []port.EmbeddingVector{{SubjectAddress: "wrong", ContentHash: "h", Values: []float32{1, 2}}}}, batchVerifierStub{})
	if _, err := service.RebuildIndex(context.Background(), request); !errors.Is(err, ErrSearchEmbeddingProfile) {
		t.Fatal(err)
	}
	service = NewVerifiedSearchService(&searchEngineStub{}, executorStub{}, indexStub{status: port.SearchIndexStatus{Identity: id}}, embeddingStub{values: []float32{1, 2}}, batchVerifierStub{err: errors.New("tampered")})
	if _, err := service.RebuildIndex(context.Background(), request); !errors.Is(err, ErrSearchEmbeddingProfile) {
		t.Fatal(err)
	}
}

func TestRebuildIndexNormalizesFailures(t *testing.T) {
	id := testIdentity()
	profile := testRequest("semantic").EmbeddingProfile
	batch := port.SearchDocumentBatch{Snapshot: id.DocumentSnapshotRef, AccessProjectionDigest: id.AccessProjectionDigest, EmbeddingProfileDigest: profile.ModelDigest, Documents: []port.SearchDocumentInput{{SubjectAddress: "a", ContentHash: "h", Text: "allowed"}}, Token: "verified"}
	request := SearchIndexBuildRequest{Snapshot: id.DocumentSnapshotRef, AccessProjectionDigest: id.AccessProjectionDigest, SearchProfile: testRequest("lexical").SearchProfile, EmbeddingProfile: profile, IndexIdentity: id, Batch: batch}
	if _, err := NewSearchService(nil, nil, nil, nil).RebuildIndex(context.Background(), request); !errors.Is(err, ErrSearchCapabilityMissing) {
		t.Fatal(err)
	}
	bad := request
	bad.Snapshot.DefinitionHash = ""
	if _, err := NewSearchService(&searchEngineStub{}, nil, indexStub{}, nil).RebuildIndex(context.Background(), bad); !errors.Is(err, ErrSearchInvalidRequest) {
		t.Fatal(err)
	}
	service := NewVerifiedSearchService(&searchEngineStub{}, nil, indexStub{}, embeddingStub{err: context.Canceled}, batchVerifierStub{})
	if _, err := service.RebuildIndex(context.Background(), request); !errors.Is(err, ErrSearchCancelled) {
		t.Fatal(err)
	}
	service = NewVerifiedSearchService(&searchEngineStub{}, nil, indexStub{}, embeddingStub{err: errors.New("offline")}, batchVerifierStub{})
	if _, err := service.RebuildIndex(context.Background(), request); !errors.Is(err, ErrSearchEmbeddingUnavailable) {
		t.Fatal(err)
	}
	service = NewVerifiedSearchService(&searchEngineStub{prepareErr: errors.New("bad")}, nil, indexStub{}, embeddingStub{values: []float32{1, 2}}, batchVerifierStub{})
	if _, err := service.RebuildIndex(context.Background(), request); !errors.Is(err, ErrSearchInvalidRequest) {
		t.Fatal(err)
	}
	service = NewVerifiedSearchService(&searchEngineStub{}, nil, buildIndexStub{applyErr: errors.New("disk")}, embeddingStub{values: []float32{1, 2}}, batchVerifierStub{})
	if _, err := service.RebuildIndex(context.Background(), request); !errors.Is(err, ErrSearchBackendFailed) {
		t.Fatal(err)
	}
	service = NewVerifiedSearchService(&searchEngineStub{}, nil, buildIndexStub{activateErr: errors.New("disk")}, embeddingStub{values: []float32{1, 2}}, batchVerifierStub{})
	if _, err := service.RebuildIndex(context.Background(), request); !errors.Is(err, ErrSearchBackendFailed) {
		t.Fatal(err)
	}
}
