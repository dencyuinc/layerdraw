// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package host

import (
	"context"
	"testing"

	searchadapter "github.com/dencyuinc/layerdraw/internal/adapter/search"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type compositionEngine struct{}

func (compositionEngine) ProduceSearchDocumentBatch(_ context.Context, request port.SearchDocumentBatchRequest) (port.SearchDocumentBatch, error) {
	return port.SearchDocumentBatch{Snapshot: request.Snapshot, AccessProjectionDigest: request.AccessProjectionDigest, EmbeddingProfileDigest: request.EmbeddingProfileDigest, Documents: append([]port.SearchDocumentInput(nil), request.Documents...)}, nil
}

func (compositionEngine) PrepareSearchIndex(context.Context, port.SearchIndexPreparationInput) (port.ExecutionPlan, error) {
	return port.ExecutionPlan{}, nil
}
func (compositionEngine) PrepareSearch(context.Context, port.SearchPreparationInput) (port.PreparedSearch, error) {
	return port.PreparedSearch{}, nil
}
func (compositionEngine) CompleteSearch(context.Context, port.CompleteSearchInput) ([]byte, error) {
	return nil, nil
}
func (compositionEngine) PrepareQuery(context.Context, port.BoundExecutionRequest) (port.ExecutionPlan, error) {
	return port.ExecutionPlan{}, nil
}
func (compositionEngine) CompleteQuery(context.Context, port.CompleteExecutionInput) ([]byte, error) {
	return nil, nil
}
func (compositionEngine) PrepareAnalysis(context.Context, port.BoundExecutionRequest) (port.ExecutionPlan, error) {
	return port.ExecutionPlan{}, nil
}
func (compositionEngine) CompleteAnalysis(context.Context, port.CompleteExecutionInput) ([]byte, error) {
	return nil, nil
}

type compositionLadybug struct {
	physical map[port.PhysicalIndexRef]bool
}

func (*compositionLadybug) ExecutePrepared(context.Context, searchadapter.LadybugStatement, port.ExecutionLimits, port.RowSink) error {
	return nil
}
func (*compositionLadybug) Interrupt() {}
func (s *compositionLadybug) ApplyIndex(_ context.Context, _ []searchadapter.LadybugStatement, ref port.PhysicalIndexRef, _ searchadapter.LadybugIndexEvidence, _ port.ExecutionLimits, _ port.RowSink) error {
	if s.physical == nil {
		s.physical = map[port.PhysicalIndexRef]bool{}
	}
	s.physical[ref] = true
	return nil
}
func (s *compositionLadybug) InspectIndex(_ context.Context, ref port.PhysicalIndexRef) error {
	if !s.physical[ref] {
		return searchadapter.ErrPhysicalIndexMissing
	}
	return nil
}

func TestDesktopSearchCompositionProvidesOneWailsMCPSurface(t *testing.T) {
	profile := port.EmbeddingProfile{ProfileID: "local", ModelID: "projection", ModelVersion: "1", ModelDigest: "sha256:model", Dimensions: 16, Normalization: "unit", MaxInputBytes: 1024}
	composition, err := NewDesktopSearchComposition(DesktopSearchConfig{Root: t.TempDir(), Engine: compositionEngine{}, DocumentProducer: compositionEngine{}, Ladybug: &compositionLadybug{}, PlanKey: []byte("01234567890123456789012345678901"), SearchDocumentKey: []byte("abcdefghijklmnopqrstuvwxyzABCDEF"), EmbeddingProfile: profile, LocalModelSeed: []byte("0123456789012345"), BackendVersion: "1", PlanProtocolVersion: "v1", MaxRows: 100, MaxBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := composition.Surface.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.SearchAvailable || !manifest.QueryAvailable || !manifest.AnalysisAvailable || !manifest.EmbeddingAvailable {
		t.Fatalf("manifest=%#v", manifest)
	}
}
