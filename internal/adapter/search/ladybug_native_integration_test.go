// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build ladybug_native

package search

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

func TestGoLadybugIndexEvidenceSurvivesRestartAndFailsClosed(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "search.lbug")
	ref := buildPhysicalFTSIndex(t, databasePath)

	session := openFTSSession(t, databasePath)
	if err := session.InspectIndex(context.Background(), ref); err != nil {
		t.Fatalf("inspect after restart: %v", err)
	}
	session.Close()

	tests := []struct {
		name   string
		mutate func(*GoLadybugSession) error
	}{
		{name: "dropped index", mutate: func(session *GoLadybugSession) error {
			return session.controlLocked("CALL DROP_FTS_INDEX('SearchDoc', 'search_doc_fts')")
		}},
		{name: "changed physical content", mutate: func(session *GoLadybugSession) error {
			return session.controlLocked("CREATE (n:SearchDoc {id: 'doc-2', body: 'changed after evidence'})")
		}},
		{name: "corrupt evidence", mutate: func(session *GoLadybugSession) error {
			return session.controlLocked("MATCH (m:" + evidenceTable + ") SET m.evidence_json = '{broken'")
		}},
		{name: "partial evidence", mutate: func(session *GoLadybugSession) error {
			return session.controlLocked("MATCH (m:" + evidenceTable + ") SET m.backend_version = ''")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "search.lbug")
			caseRef := buildPhysicalFTSIndex(t, path)
			caseSession := openFTSSession(t, path)
			if err := test.mutate(caseSession); err != nil {
				t.Fatal(err)
			}
			caseSession.Close()
			caseSession = openFTSSession(t, path)
			defer caseSession.Close()
			if err := caseSession.InspectIndex(context.Background(), caseRef); !errors.Is(err, ErrPhysicalIndexMissing) {
				t.Fatalf("expected fail-closed physical index error, got %v", err)
			}
		})
	}
}

func TestGoLadybugApplyIndexLeavesMismatchedPhysicalIndexUntrusted(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "rollback.lbug")
	session := createFTSFixture(t, databasePath)
	defer session.Close()
	evidence := testFTSEvidence()
	digest, backend, err := session.inspectEvidenceLocked(context.Background(), evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.controlLocked("CALL DROP_FTS_INDEX('SearchDoc', 'search_doc_fts')"); err != nil {
		t.Fatal(err)
	}
	ref := port.PhysicalIndexRef{IdentityDigest: "sha256:" + string(make([]byte, 64)), ContentDigest: digest + "-mismatch", BackendVersion: backend}
	err = session.ApplyIndex(context.Background(), []LadybugStatement{{Query: testCreateFTS}}, ref, evidence, port.ExecutionLimits{MaxRows: 16, MaxBytes: 4096}, discardRowSink{})
	if !errors.Is(err, ErrPhysicalIndexMissing) {
		t.Fatalf("expected mismatched digest rejection, got %v", err)
	}
	if err := session.InspectIndex(context.Background(), ref); !errors.Is(err, ErrPhysicalIndexMissing) {
		t.Fatalf("mismatched physical index was trusted: %v", err)
	}
}

const testCreateFTS = "CALL CREATE_FTS_INDEX('SearchDoc', 'search_doc_fts', ['body'], stemmer := 'none')"

func buildPhysicalFTSIndex(t *testing.T, databasePath string) port.PhysicalIndexRef {
	t.Helper()
	session := createFTSFixture(t, databasePath)
	evidence := testFTSEvidence()
	digest, backend, err := session.inspectEvidenceLocked(context.Background(), evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.controlLocked("CALL DROP_FTS_INDEX('SearchDoc', 'search_doc_fts')"); err != nil {
		t.Fatal(err)
	}
	ref := port.PhysicalIndexRef{IdentityDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111", ContentDigest: digest, BackendVersion: backend}
	if err := session.ApplyIndex(context.Background(), []LadybugStatement{{Query: testCreateFTS}}, ref, evidence, port.ExecutionLimits{MaxRows: 16, MaxBytes: 4096}, discardRowSink{}); err != nil {
		t.Fatal(err)
	}
	session.Close()
	return ref
}

func createFTSFixture(t *testing.T, databasePath string) *GoLadybugSession {
	t.Helper()
	session, err := OpenGoLadybugSessionWithFTS(databasePath, testFTSExtension(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"CREATE NODE TABLE SearchDoc (id STRING, body STRING, PRIMARY KEY(id))",
		"CREATE (n:SearchDoc {id: 'doc-1', body: 'layer draw search'})",
		testCreateFTS,
	} {
		if err := session.controlLocked(statement); err != nil {
			session.Close()
			t.Fatal(err)
		}
	}
	return session
}

func openFTSSession(t *testing.T, databasePath string) *GoLadybugSession {
	t.Helper()
	session, err := OpenGoLadybugSessionWithFTS(databasePath, testFTSExtension(t))
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func testFTSExtension(t *testing.T) string {
	t.Helper()
	path := os.Getenv("LAYERDRAW_LADYBUG_FTS_EXTENSION")
	if !filepath.IsAbs(path) {
		t.Fatal("LAYERDRAW_LADYBUG_FTS_EXTENSION must be an absolute verified path")
	}
	return path
}

func testFTSEvidence() LadybugIndexEvidence {
	return LadybugIndexEvidence{
		TableName:      "SearchDoc",
		IndexName:      "search_doc_fts",
		IndexType:      "FTS",
		PropertyNames:  []string{"body"},
		ContentColumns: []string{"id", "body"},
		PrimaryKey:     "id",
	}
}
