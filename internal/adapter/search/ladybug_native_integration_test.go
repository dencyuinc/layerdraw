// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build ladybug_native

package search

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type nativeRows struct{ rows []port.RawRow }

func (s *nativeRows) Push(row port.RawRow) error { s.rows = append(s.rows, row); return nil }

func TestGoLadybugFTSReturnsIndexedRows(t *testing.T) {
	session := createFTSFixture(t, filepath.Join(t.TempDir(), "query.lbug"))
	defer session.Close()
	rows := &nativeRows{}
	err := session.ExecutePrepared(context.Background(), LadybugStatement{Query: "CALL QUERY_FTS_INDEX('SearchDoc', 'search_doc_fts', 'layer') RETURN node.id AS id, score AS score"}, port.ExecutionLimits{MaxRows: 10, MaxBytes: 4096}, rows)
	if err != nil || len(rows.rows) != 1 || rows.rows[0]["id"].Value != "doc-1" {
		t.Fatalf("rows=%v err=%v", rows.rows, err)
	}
}

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
	err = session.ApplyIndex(context.Background(), []LadybugStatement{{Query: testCreateFTS}}, &ref, []LadybugIndexEvidence{evidence}, port.ExecutionLimits{MaxRows: 16, MaxBytes: 4096}, discardRowSink{})
	if !errors.Is(err, ErrPhysicalIndexMissing) {
		t.Fatalf("expected mismatched digest rejection, got %v", err)
	}
	if err := session.InspectIndex(context.Background(), ref); !errors.Is(err, ErrPhysicalIndexMissing) {
		t.Fatalf("mismatched physical index was trusted: %v", err)
	}
}

func TestGoLadybugRejectsIncrementalPlanWhoseActualDocumentSetIsStale(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "incremental.lbug")
	session, err := OpenGoLadybugSessionWithFTS(databasePath, testFTSExtension(t))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, query := range []string{"CREATE NODE TABLE SearchDoc (id STRING, content_hash STRING, physical_digest STRING, body STRING, PRIMARY KEY(id))", "CREATE (n:SearchDoc {id: 'old', content_hash: 'old-hash', physical_digest: 'old-physical', body: 'stale'})"} {
		if err := session.controlLocked(query); err != nil {
			t.Fatal(err)
		}
	}
	evidence := LadybugIndexEvidence{TableName: "SearchDoc", IndexName: "search_doc_fts", IndexType: "FTS", PropertyNames: []string{"body"}, ContentColumns: []string{"id", "content_hash", "physical_digest", "body"}, PrimaryKey: "id", ExpectedDocumentSetDigest: documentSetDigest([]map[string]any{{"id": "new", "physical_digest": "new-physical"}})}
	ref := port.PhysicalIndexRef{IdentityDigest: "sha256:" + string(make([]byte, 64)), BackendVersion: GoLadybugBackendVersion}
	statements := []LadybugStatement{{Query: testCreateFTS}}
	if err := session.ApplyIndex(context.Background(), statements, &ref, []LadybugIndexEvidence{evidence}, port.ExecutionLimits{MaxRows: 16, MaxBytes: 4096}, discardRowSink{}); !errors.Is(err, ErrPhysicalIndexMissing) {
		t.Fatalf("stale incremental document set was trusted: %v", err)
	}
}

const testCreateFTS = "CALL CREATE_FTS_INDEX('SearchDoc', 'search_doc_fts', ['body'], stemmer := 'none')"

func buildPhysicalFTSIndex(t *testing.T, databasePath string) port.PhysicalIndexRef {
	t.Helper()
	session := createFTSFixture(t, databasePath)
	evidence := testFTSEvidence()
	_, backend, err := session.inspectEvidenceLocked(context.Background(), evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.controlLocked("CALL DROP_FTS_INDEX('SearchDoc', 'search_doc_fts')"); err != nil {
		t.Fatal(err)
	}
	ref := port.PhysicalIndexRef{IdentityDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111", BackendVersion: backend}
	if err := session.ApplyIndex(context.Background(), []LadybugStatement{{Query: testCreateFTS}}, &ref, []LadybugIndexEvidence{evidence}, port.ExecutionLimits{MaxRows: 16, MaxBytes: 4096}, discardRowSink{}); err != nil {
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

func TestGoLadybugExecutesBoundedProjectedPageRank(t *testing.T) {
	extensions := []string{testFTSExtension(t), filepath.Join(filepath.Dir(testFTSExtension(t)), "libvector.lbug_extension"), filepath.Join(filepath.Dir(testFTSExtension(t)), "libalgo.lbug_extension")}
	session, err := OpenGoLadybugSessionWithExtensions(filepath.Join(t.TempDir(), "algo.lbug"), extensions)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, query := range []string{"CREATE NODE TABLE SearchNode(id STRING, PRIMARY KEY(id))", "CREATE REL TABLE SearchEdge(FROM SearchNode TO SearchNode, id STRING)", "CREATE (:SearchNode {id: 'a'}), (:SearchNode {id: 'b'})", "MATCH (a:SearchNode {id: 'a'}), (b:SearchNode {id: 'b'}) CREATE (a)-[:SearchEdge {id: 'ab'}]->(b)", `CALL PROJECT_GRAPH('test_graph', {'SearchNode': 'n.id IN ["a","b"]'}, {'SearchEdge': 'r.id IN ["ab"]'})`} {
		if err := session.controlLocked(query); err != nil {
			t.Fatalf("query=%s: %v", query, err)
		}
	}
	rows, err := session.queryLocked("CALL page_rank('test_graph') RETURN node.id AS address, rank AS metric_value ORDER BY address")
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}

func TestGoLadybugExecutesEngineAnalysisPlan(t *testing.T) {
	session, err := OpenGoLadybugSessionWithExtensions(filepath.Join(t.TempDir(), "analysis.lbug"), []string{testFTSExtension(t), filepath.Join(filepath.Dir(testFTSExtension(t)), "libvector.lbug_extension"), filepath.Join(filepath.Dir(testFTSExtension(t)), "libalgo.lbug_extension")})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, query := range []string{"CREATE NODE TABLE SearchNode(id STRING, PRIMARY KEY(id))", "CREATE REL TABLE SearchEdge(FROM SearchNode TO SearchNode, id STRING, from_id STRING, to_id STRING)", "CREATE (:SearchNode {id: 'ldl:project:p:entity:alpha'}), (:SearchNode {id: 'ldl:project:p:entity:beta'})", "MATCH (a:SearchNode {id: 'ldl:project:p:entity:alpha'}), (b:SearchNode {id: 'ldl:project:p:entity:beta'}) CREATE (a)-[:SearchEdge {id: 'ldl:project:p:relation:alpha_beta', from_id: a.id, to_id: b.id}]->(b)"} {
		if err := session.controlLocked(query); err != nil {
			t.Fatalf("fixture query=%s: %v", query, err)
		}
	}
	plan, _, err := engine.BuildNativeAnalysisPlan([]byte(`{"kind":"analyze_graph","algorithm":"page_rank","entity_addresses":["ldl:project:p:entity:alpha","ldl:project:p:entity:beta"],"relation_addresses":["ldl:project:p:relation:alpha_beta"],"parameters":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	for index, statement := range plan.Statements {
		rows := &nativeRows{}
		if err := session.ExecutePrepared(context.Background(), LadybugStatement{Query: statement.Query, Parameters: map[string]port.RawValue{}}, port.ExecutionLimits{MaxRows: 16, MaxBytes: 4096}, rows); err != nil {
			t.Fatalf("statement %d %+v: %v", index, statement, err)
		}
	}
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
