// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build ladybug_native

package host

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

func TestDesktopNativeSearchCompositionUsesProductionLadybugBinding(t *testing.T) {
	profile := port.EmbeddingProfile{ProfileID: "local", ModelID: "projection", ModelVersion: "1", ModelDigest: "sha256:model", Dimensions: 16, Normalization: "unit", MaxInputBytes: 1024}
	composition, err := OpenDesktopNativeSearchComposition(DesktopSearchConfig{
		Root:                t.TempDir(),
		Engine:              compositionEngine{},
		PlanKey:             []byte("01234567890123456789012345678901"),
		SearchDocumentKey:   []byte("abcdefghijklmnopqrstuvwxyzABCDEF"),
		EmbeddingProfile:    profile,
		LocalModelSeed:      []byte("0123456789012345"),
		PlanProtocolVersion: "v1",
		MaxRows:             100,
		MaxBytes:            4096,
	}, filepath.Join(t.TempDir(), "desktop-search.lbug"))
	if err != nil {
		t.Fatal(err)
	}
	defer composition.Close()
	manifest, err := composition.Surface.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.SearchAvailable || !manifest.QueryAvailable || !manifest.AnalysisAvailable {
		t.Fatalf("manifest=%#v", manifest)
	}
}
