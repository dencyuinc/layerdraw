// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package localdocument

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
)

// TestCommitLayerPersistsToDiskAndReopen reproduces the Structure editor's
// first authoring loop: create a layer, commit it, and require the master
// document on disk plus a reopened session to contain it.
func TestCommitLayerPersistsToDiskAndReopen(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root, "project p \"P\" {}\n")
	host := newTestHost(t, filepath.Join(root, "data"), nil)
	owner, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	data := `{"operations":[
	 {"operation":"create_subject","subject_kind":"layer","parent_address":"ldl:project:p","id":"application","fields":{"display_name":"Application","order":"0"}}
	]}`
	batch, err := engineprotocol.DecodeSemanticOperationBatch([]byte(data))
	if err != nil {
		t.Fatalf("batch decode: %v", err)
	}
	generation, err := host.DocumentGenerationFor(owner.Session.Open.Session)
	if err != nil {
		t.Fatal(err)
	}
	operationBatch := runtimeprotocol.RuntimeOperationBatch{
		DocumentID: owner.Session.Open.CommittedRevision.DocumentID, BaseRevision: owner.Session.Open.CommittedRevision,
		ExpectedDefinitionHash: owner.Session.Open.CommittedRevision.DefinitionHash,
		Operations:             batch,
		Preconditions:          engineprotocol.EngineEditPreconditions{DocumentGeneration: generation, ExpectedChildSets: []engineprotocol.ExpectedChildSet{}, ExpectedSubjectHashes: []engineprotocol.ExpectedHash{}, ExpectedSubtreeHashes: []engineprotocol.ExpectedHash{}},
	}
	preview, err := host.PreviewEditor(context.Background(), runtimeprotocol.PreviewOperationsInput{Session: owner.Session.Open.Session, OperationBatch: operationBatch})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if _, err := host.Commit(context.Background(), runtimeprotocol.RuntimeCommitInput{
		Session: owner.Session.Open.Session, OperationBatch: operationBatch,
		AuthoringProof: preview.Runtime.AuthoringProof,
		OperationID:    "structure_commit_1", IdempotencyKey: "structure_commit_1", Trigger: "explicit_save",
	}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	structure, err := host.ProjectStructure(context.Background(), owner.Session.Open.Session)
	if err != nil {
		t.Fatalf("structure after commit: %v", err)
	}
	if len(structure.Layers) != 1 || structure.Layers[0].ID != "application" {
		t.Fatalf("structure after commit lacks the layer: %+v", structure.Layers)
	}
	onDisk := false
	_ = filepath.WalkDir(project, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		contents, readErr := os.ReadFile(path)
		if readErr == nil && strings.Contains(string(contents), "application") {
			onDisk = true
		}
		return nil
	})
	if !onDisk {
		t.Fatalf("committed layer never reached the project files on disk")
	}
	if err := host.Close(context.Background(), owner.Session); err != nil {
		t.Logf("close: %v", err)
	}
	reopened, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	restored, err := host.ProjectStructure(context.Background(), reopened.Session.Open.Session)
	if err != nil {
		t.Fatalf("structure after reopen: %v", err)
	}
	if len(restored.Layers) != 1 || restored.Layers[0].ID != "application" {
		t.Fatalf("reopened structure lacks the committed layer: %+v", restored.Layers)
	}
}
