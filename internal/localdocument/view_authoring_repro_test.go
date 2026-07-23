// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package localdocument

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
)

// TestPreviewCreatesQueryAndViewAtomically reproduces the desktop view
// authoring batch: a select-all query plus a diagram view sourced from it,
// previewed in one semantic operation batch on an empty project.
func TestPreviewCreatesQueryAndViewAtomically(t *testing.T) {
	root := t.TempDir()
	source := "project p \"P\" {}\n"
	project := writeProject(t, root, source)
	host := newTestHost(t, filepath.Join(root, "data"), nil)
	owner, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	data := `{"operations":[
	 {"operation":"create_subject","subject_kind":"query","parent_address":"ldl:project:p","id":"service_map_scope","fields":{"display_name":"Service map scope","select":{}}},
	 {"operation":"create_subject","subject_kind":"view","parent_address":"ldl:project:p","id":"service_map","fields":{"display_name":"Service map","category":"topology","shape":{"kind":"diagram"},"source":{"kind":"query","query_address":"ldl:project:p:query:service_map_scope","arguments":{}}}}
	]}`
	batch, err := engineprotocol.DecodeSemanticOperationBatch([]byte(data))
	if err != nil {
		t.Fatalf("batch decode: %v", err)
	}
	operationBatch := runtimeprotocol.RuntimeOperationBatch{
		DocumentID: owner.Session.Open.CommittedRevision.DocumentID, BaseRevision: owner.Session.Open.CommittedRevision,
		ExpectedDefinitionHash: owner.Session.Open.CommittedRevision.DefinitionHash,
		Operations:             batch, Preconditions: func() engineprotocol.EngineEditPreconditions {
			generation, err := host.DocumentGenerationFor(owner.Session.Open.Session)
			if err != nil {
				t.Fatal(err)
			}
			return engineprotocol.EngineEditPreconditions{DocumentGeneration: generation, ExpectedChildSets: []engineprotocol.ExpectedChildSet{}, ExpectedSubjectHashes: []engineprotocol.ExpectedHash{}, ExpectedSubtreeHashes: []engineprotocol.ExpectedHash{}}
		}(),
	}
	preview, err := host.PreviewEditor(context.Background(), runtimeprotocol.PreviewOperationsInput{Session: owner.Session.Open.Session, OperationBatch: operationBatch})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Preview.PreviewID == nil {
		t.Fatalf("preview missing id: %+v", preview.Preview)
	}
	commit, err := host.Commit(context.Background(), runtimeprotocol.RuntimeCommitInput{
		Session: owner.Session.Open.Session, OperationBatch: operationBatch,
		AuthoringProof: preview.Runtime.AuthoringProof,
		OperationID:    "desktop_editor_1", IdempotencyKey: "desktop_editor_1", Trigger: "explicit_save",
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	_ = commit
	views, err := host.ProjectViews(context.Background(), owner.Session.Open.Session)
	if err != nil || len(views) != 1 {
		t.Fatalf("views after commit=%+v err=%v", views, err)
	}
}
