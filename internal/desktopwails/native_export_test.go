// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	nativeexport "github.com/dencyuinc/layerdraw/internal/exporter"
)

func TestNativeInterchangeStagesPublishesAndImportsThroughOpaqueSelections(t *testing.T) {
	root := t.TempDir()
	vault := newSelectionVault()
	adapter, err := NewNativeInterchangeAdapter(vault, root)
	if err != nil {
		t.Fatal(err)
	}
	plan, view := nativeFixture(t)
	result, err := adapter.Serialize(context.Background(), nativeexport.SerializeInput{Plan: plan, ViewData: view})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result)
	if bytes.Contains(encoded, []byte("layerdraw-viewdata")) {
		t.Fatal("artifact bytes crossed the Wails result")
	}
	destination := filepath.Join(root, "published.json")
	token, err := vault.issue(destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Publish(context.Background(), token, result.Artifact.ArtifactID); err != nil {
		t.Fatal(err)
	}
	artifact, err := os.ReadFile(destination)
	if err != nil || !bytes.Contains(artifact, []byte(`"format":"layerdraw-viewdata"`)) {
		t.Fatalf("published artifact: %v %q", err, artifact)
	}
	manifest, err := os.ReadFile(filepath.Join(root, "published.sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := semantic.DecodeExportSourceManifest(manifest); err != nil {
		t.Fatalf("published source manifest: %v", err)
	}
	if err := adapter.Publish(context.Background(), token, result.Artifact.ArtifactID); err == nil {
		t.Fatal("opaque selection or staged artifact replayed")
	}

	importPath := filepath.Join(root, "operations.json")
	if err := os.WriteFile(importPath, []byte(`{"format":"layerdraw-semantic-operations","schema_version":1,"operations":[{"operation":"delete_subject","target_address":"ldl:project:p:entity:a"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	importToken, _ := vault.issue(importPath)
	preview, err := adapter.Import(context.Background(), importToken, nativeexport.OperationsJSONProfile)
	if err != nil || len(preview.Batch.Operations) != 1 {
		t.Fatalf("import preview=%+v err=%v", preview, err)
	}
	if err := adapter.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNativeInterchangeRejectsUnsupportedProfileAndCancelledPublish(t *testing.T) {
	root := t.TempDir()
	vault := newSelectionVault()
	adapter, _ := NewNativeInterchangeAdapter(vault, root)
	path := filepath.Join(root, "input.json")
	_ = os.WriteFile(path, []byte(`{}`), 0o600)
	token, _ := vault.issue(path)
	if _, err := adapter.Import(context.Background(), token, "unknown@1"); err == nil {
		t.Fatal("unsupported import profile accepted")
	}
	plan, view := nativeFixture(t)
	result, err := adapter.Serialize(context.Background(), nativeexport.SerializeInput{Plan: plan, ViewData: view})
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "cancelled.json")
	token, _ = vault.issue(destination)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := adapter.Publish(ctx, token, result.Artifact.ArtifactID); !nativeexport.IsFailure(err, nativeexport.FailureCancelled) {
		t.Fatalf("cancel=%v", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatal("cancelled destination was published")
	}
}

func nativeFixture(t *testing.T) (semantic.ExportPlan, semantic.ViewData) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "fixtures", "conformance", "export-plan-transport-parity-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		ExportPlan json.RawMessage `json:"export_plan"`
		Input      struct {
			ViewData json.RawMessage `json:"view_data"`
		} `json:"input"`
	}
	if json.Unmarshal(raw, &fixture) != nil {
		t.Fatal("fixture")
	}
	plan, err := semantic.DecodeExportPlan(fixture.ExportPlan)
	if err != nil {
		t.Fatal(err)
	}
	view, err := semantic.DecodeViewData(fixture.Input.ViewData)
	if err != nil {
		t.Fatal(err)
	}
	plan.RequiredFontDigests = []protocolcommon.Digest{}
	return plan, view
}
