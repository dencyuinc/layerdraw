// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build ladybug_native

package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	lbug "github.com/LadybugDB/go-ladybug"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

const evidenceTable = "LayerDrawSearchIndexEvidence"

const GoLadybugBackendVersion = "0.17.0"

var ladybugIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// GoLadybugSession is the production native binding. Physical index evidence
// lives inside Ladybug and is revalidated against catalog and table content in
// one read transaction on every activation/restart inspection.
type GoLadybugSession struct {
	db   *lbug.Database
	conn *lbug.Connection
	mu   sync.Mutex
}

func OpenGoLadybugSession(databasePath string) (*GoLadybugSession, error) {
	return OpenGoLadybugSessionWithFTS(databasePath, "")
}

func OpenGoLadybugSessionWithFTS(databasePath, ftsExtensionPath string) (*GoLadybugSession, error) {
	return OpenGoLadybugSessionWithExtensions(databasePath, []string{ftsExtensionPath})
}

func OpenGoLadybugSessionWithExtensions(databasePath string, extensionPaths []string) (*GoLadybugSession, error) {
	if databasePath == "" || databasePath == ":memory:" || !strings.HasPrefix(databasePath, "/") {
		return nil, fmt.Errorf("absolute on-disk Ladybug path required")
	}
	for _, extensionPath := range extensionPaths {
		if extensionPath != "" && (!filepath.IsAbs(extensionPath) || strings.Contains(extensionPath, "'")) {
			return nil, fmt.Errorf("absolute Ladybug extension path required")
		}
	}
	db, err := lbug.OpenDatabase(databasePath, lbug.DefaultSystemConfig())
	if err != nil {
		return nil, err
	}
	conn, err := lbug.OpenConnection(db)
	if err != nil {
		db.Close()
		return nil, err
	}
	session := &GoLadybugSession{db: db, conn: conn}
	for _, extensionPath := range extensionPaths {
		if extensionPath != "" {
			if err := session.controlLocked("LOAD EXTENSION '" + extensionPath + "'"); err != nil {
				session.Close()
				return nil, err
			}
		}
	}
	return session, nil
}

func (s *GoLadybugSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conn.Close()
	s.db.Close()
}

func (s *GoLadybugSession) Interrupt() { s.conn.Interrupt() }

func (s *GoLadybugSession) BackendVersion() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.queryLocked("CALL DB_VERSION() RETURN *")
	if err != nil || len(rows) != 1 {
		return "", ErrPhysicalIndexMissing
	}
	version := fmt.Sprint(rows[0]["version"])
	if version == "" {
		return "", ErrPhysicalIndexMissing
	}
	return version, nil
}

func (s *GoLadybugSession) ExecutePrepared(ctx context.Context, statement LadybugStatement, limits port.ExecutionLimits, sink port.RowSink) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.executePreparedLocked(ctx, statement, limits, sink)
}

func (s *GoLadybugSession) ApplyIndex(ctx context.Context, statements []LadybugStatement, ref *port.PhysicalIndexRef, evidence []LadybugIndexEvidence, limits port.ExecutionLimits, sink port.RowSink) (err error) {
	if ref == nil || !validEvidenceSet(*ref, evidence) {
		return ErrInvalidPlan
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// FTS/HNSW extension DDL is restricted by Ladybug to auto-transaction mode.
	// Invalidate any prior evidence first, then execute the physical statements.
	// A crash or partial failure can therefore leave untrusted physical data, but
	// never durable evidence that would allow it to be activated.
	if err = s.controlLocked("BEGIN TRANSACTION"); err != nil {
		return err
	}
	if err = s.controlLocked("CREATE NODE TABLE IF NOT EXISTS " + evidenceTable + " (identity_digest STRING PRIMARY KEY, content_digest STRING, backend_version STRING, evidence_json STRING)"); err != nil {
		_ = s.controlLocked("ROLLBACK")
		return err
	}
	if err = s.validateEvidenceTableLocked(); err != nil {
		_ = s.controlLocked("ROLLBACK")
		return ErrPhysicalIndexMissing
	}
	discard := discardRowSink{}
	deleteStatement := LadybugStatement{Query: "MATCH (m:" + evidenceTable + ") WHERE m.identity_digest = $identity DELETE m", Parameters: map[string]port.RawValue{"identity": {Kind: "string", Value: ref.IdentityDigest}}}
	if err = s.executePreparedLocked(ctx, deleteStatement, port.ExecutionLimits{MaxRows: 1, MaxBytes: 1}, discard); err != nil {
		_ = s.controlLocked("ROLLBACK")
		return err
	}
	if err = s.controlLocked("COMMIT"); err != nil {
		return err
	}
	existingIndexes := map[string]bool{}
	if rows, indexErr := s.queryLocked("CALL SHOW_INDEXES() RETURN *"); indexErr == nil {
		for _, row := range rows {
			existingIndexes[fmt.Sprint(row["index_name"])] = true
		}
	}
	for _, item := range evidence {
		if !existingIndexes[item.IndexName] {
			continue
		}
		procedure := ""
		switch item.IndexType {
		case "FTS":
			procedure = "DROP_FTS_INDEX"
		case "HNSW":
			procedure = "DROP_VECTOR_INDEX"
		default:
			return ErrPhysicalIndexMissing
		}
		if err := s.controlLocked("CALL " + procedure + "('" + item.TableName + "', '" + item.IndexName + "')"); err != nil {
			return err
		}
	}
	for _, statement := range statements {
		if statement.Query == "" {
			return ErrInvalidPlan
		}
		if len(statement.Parameters) == 0 {
			err = s.controlLocked(statement.Query)
		} else {
			err = s.executePreparedLocked(ctx, statement, limits, sink)
		}
		if err != nil {
			return err
		}
	}
	// Bind catalog, schema, index metadata, backend version, and actual ordered
	// table content to the durable evidence row in one database transaction.
	if err = s.controlLocked("BEGIN TRANSACTION"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.controlLocked("ROLLBACK")
		}
	}()
	digest, backend, err := s.inspectEvidenceSetLocked(ctx, evidence)
	if err != nil || backend != ref.BackendVersion || (ref.ContentDigest != "" && digest != ref.ContentDigest) {
		return ErrPhysicalIndexMissing
	}
	ref.ContentDigest = digest
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		return ErrInvalidPlan
	}
	createStatement := LadybugStatement{Query: "CREATE (m:" + evidenceTable + " {identity_digest: $identity, content_digest: $content, backend_version: $backend, evidence_json: $evidence})", Parameters: map[string]port.RawValue{
		"identity": {Kind: "string", Value: ref.IdentityDigest},
		"content":  {Kind: "string", Value: ref.ContentDigest},
		"backend":  {Kind: "string", Value: ref.BackendVersion},
		"evidence": {Kind: "string", Value: string(evidenceJSON)},
	}}
	if err = s.executePreparedLocked(ctx, createStatement, port.ExecutionLimits{MaxRows: 1, MaxBytes: 1}, discard); err != nil {
		return err
	}
	if err = s.controlLocked("COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *GoLadybugSession) InspectIndex(ctx context.Context, ref port.PhysicalIndexRef) (err error) {
	if ref.IdentityDigest == "" || ref.ContentDigest == "" || ref.BackendVersion == "" {
		return ErrPhysicalIndexMissing
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.controlLocked("BEGIN TRANSACTION READ ONLY"); err != nil {
		return ErrPhysicalIndexMissing
	}
	defer func() { _ = s.controlLocked("ROLLBACK") }()
	if err = s.validateEvidenceTableLocked(); err != nil {
		return ErrPhysicalIndexMissing
	}
	rows, err := s.queryPreparedLocked(LadybugStatement{Query: "MATCH (m:" + evidenceTable + ") WHERE m.identity_digest = $identity RETURN m.content_digest AS content_digest, m.backend_version AS backend_version, m.evidence_json AS evidence_json", Parameters: map[string]port.RawValue{"identity": {Kind: "string", Value: ref.IdentityDigest}}})
	if err != nil || len(rows) != 1 || fmt.Sprint(rows[0]["content_digest"]) != ref.ContentDigest || fmt.Sprint(rows[0]["backend_version"]) != ref.BackendVersion {
		return ErrPhysicalIndexMissing
	}
	var evidence []LadybugIndexEvidence
	if err := json.Unmarshal([]byte(fmt.Sprint(rows[0]["evidence_json"])), &evidence); err != nil || !validEvidenceSet(ref, evidence) {
		return ErrPhysicalIndexMissing
	}
	digest, backend, err := s.inspectEvidenceSetLocked(ctx, evidence)
	if err != nil || digest != ref.ContentDigest || backend != ref.BackendVersion {
		return ErrPhysicalIndexMissing
	}
	return nil
}

type discardRowSink struct{}

func (discardRowSink) Push(port.RawRow) error { return nil }

func (s *GoLadybugSession) executePreparedLocked(ctx context.Context, statement LadybugStatement, _ port.ExecutionLimits, sink port.RowSink) error {
	prepared, err := s.conn.Prepare(statement.Query)
	if err != nil {
		return err
	}
	defer prepared.Close()
	args := map[string]any{}
	for key, value := range statement.Parameters {
		converted, err := ladybugValue(value)
		if err != nil {
			return err
		}
		args[key] = converted
	}
	result, err := s.conn.Execute(prepared, args)
	if err != nil {
		return err
	}
	defer result.Close()
	for result.HasNext() {
		select {
		case <-ctx.Done():
			s.conn.Interrupt()
			return ctx.Err()
		default:
		}
		tuple, err := result.Next()
		if err != nil {
			return err
		}
		values, err := tuple.GetAsMap()
		tuple.Close()
		if err != nil {
			return err
		}
		row := port.RawRow{}
		for key, value := range values {
			row[key] = port.RawValue{Kind: fmt.Sprintf("%T", value), Value: fmt.Sprint(value)}
		}
		if err := sink.Push(row); err != nil {
			s.conn.Interrupt()
			return err
		}
	}
	return nil
}

func (s *GoLadybugSession) controlLocked(query string) error {
	result, err := s.conn.Query(query)
	if result != nil {
		result.Close()
	}
	return err
}

func (s *GoLadybugSession) queryPreparedLocked(statement LadybugStatement) ([]map[string]any, error) {
	prepared, err := s.conn.Prepare(statement.Query)
	if err != nil {
		return nil, err
	}
	defer prepared.Close()
	args := map[string]any{}
	for key, value := range statement.Parameters {
		converted, err := ladybugValue(value)
		if err != nil {
			return nil, err
		}
		args[key] = converted
	}
	result, err := s.conn.Execute(prepared, args)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	return collectLadybugRows(result)
}

func (s *GoLadybugSession) queryLocked(query string) ([]map[string]any, error) {
	result, err := s.conn.Query(query)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	return collectLadybugRows(result)
}

func collectLadybugRows(result *lbug.QueryResult) ([]map[string]any, error) {
	var rows []map[string]any
	for result.HasNext() {
		tuple, err := result.Next()
		if err != nil {
			return nil, err
		}
		values, err := tuple.GetAsMap()
		tuple.Close()
		if err != nil {
			return nil, err
		}
		rows = append(rows, values)
	}
	return rows, nil
}

func (s *GoLadybugSession) validateEvidenceTableLocked() error {
	rows, err := s.queryLocked("CALL TABLE_INFO('" + evidenceTable + "') RETURN *")
	if err != nil || len(rows) != 4 {
		return ErrPhysicalIndexMissing
	}
	want := map[string]string{"identity_digest": "STRING", "content_digest": "STRING", "backend_version": "STRING", "evidence_json": "STRING"}
	for _, row := range rows {
		name, kind := fmt.Sprint(row["name"]), fmt.Sprint(row["type"])
		if want[name] != kind {
			return ErrPhysicalIndexMissing
		}
		if name == "identity_digest" && fmt.Sprint(row["primary key"]) != "true" {
			return ErrPhysicalIndexMissing
		}
		delete(want, name)
	}
	if len(want) != 0 {
		return ErrPhysicalIndexMissing
	}
	return nil
}

func (s *GoLadybugSession) inspectEvidenceLocked(ctx context.Context, evidence LadybugIndexEvidence) (string, string, error) {
	select {
	case <-ctx.Done():
		return "", "", ctx.Err()
	default:
	}
	versionRows, err := s.queryLocked("CALL DB_VERSION() RETURN *")
	if err != nil || len(versionRows) != 1 {
		return "", "", ErrPhysicalIndexMissing
	}
	backend := fmt.Sprint(versionRows[0]["version"])
	schemaRows, err := s.queryLocked("CALL TABLE_INFO('" + evidence.TableName + "') RETURN *")
	if err != nil || len(schemaRows) == 0 {
		return "", "", ErrPhysicalIndexMissing
	}
	if !schemaContainsEvidence(schemaRows, evidence) {
		return "", "", ErrPhysicalIndexMissing
	}
	var matched map[string]any
	if evidence.IndexName != "" {
		indexRows, err := s.queryLocked("CALL SHOW_INDEXES() RETURN *")
		if err != nil {
			return "", "", ErrPhysicalIndexMissing
		}
		for _, row := range indexRows {
			if fmt.Sprint(row["table_name"]) == evidence.TableName && fmt.Sprint(row["index_name"]) == evidence.IndexName {
				if matched != nil {
					return "", "", ErrPhysicalIndexMissing
				}
				matched = row
			}
		}
		if matched == nil || fmt.Sprint(matched["index_type"]) != evidence.IndexType || fmt.Sprint(matched["extension_loaded"]) != "true" || matched["index_definition"] == nil || !samePropertyNames(matched["property_names"], evidence.PropertyNames) {
			return "", "", ErrPhysicalIndexMissing
		}
	}
	contentRows, err := s.queryLocked(contentQuery(evidence))
	if err != nil {
		return "", "", ErrPhysicalIndexMissing
	}
	if evidence.ExpectedDocumentSetDigest != "" {
		rows, queryErr := s.queryLocked("MATCH (n:" + evidence.TableName + ") RETURN n.id AS id, n.physical_digest AS physical_digest ORDER BY id")
		if queryErr != nil || documentSetDigest(rows) != evidence.ExpectedDocumentSetDigest {
			return "", "", ErrPhysicalIndexMissing
		}
	}
	digest, err := evidenceDigest(evidence, backend, schemaRows, matched, contentRows)
	if err != nil {
		return "", "", ErrPhysicalIndexMissing
	}
	return digest, backend, nil
}

func documentSetDigest(rows []map[string]any) string {
	hash := sha256.New()
	for _, row := range rows {
		_, _ = io.WriteString(hash, fmt.Sprint(row["id"])+"\x00"+fmt.Sprint(row["physical_digest"])+"\n")
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func (s *GoLadybugSession) inspectEvidenceSetLocked(ctx context.Context, evidence []LadybugIndexEvidence) (string, string, error) {
	digests := make([]string, len(evidence))
	backend := ""
	for index, item := range evidence {
		digest, currentBackend, err := s.inspectEvidenceLocked(ctx, item)
		if err != nil || (backend != "" && currentBackend != backend) {
			return "", "", ErrPhysicalIndexMissing
		}
		digests[index], backend = digest, currentBackend
	}
	data, err := json.Marshal(digests)
	if err != nil {
		return "", "", ErrPhysicalIndexMissing
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:]), backend, nil
}

func validEvidenceSet(ref port.PhysicalIndexRef, evidence []LadybugIndexEvidence) bool {
	if len(evidence) == 0 {
		return false
	}
	for _, item := range evidence {
		if !validEvidence(ref, item) {
			return false
		}
	}
	return true
}

func validEvidence(ref port.PhysicalIndexRef, evidence LadybugIndexEvidence) bool {
	if ref.IdentityDigest == "" || ref.BackendVersion == "" || !ladybugIdentifier.MatchString(evidence.TableName) || !ladybugIdentifier.MatchString(evidence.PrimaryKey) || len(evidence.ContentColumns) == 0 {
		return false
	}
	if (evidence.IndexName == "") != (evidence.IndexType == "" || len(evidence.PropertyNames) == 0) {
		return false
	}
	if evidence.IndexName != "" && !ladybugIdentifier.MatchString(evidence.IndexName) {
		return false
	}
	if evidence.ExpectedDocumentSetDigest != "" && (!strings.HasPrefix(evidence.ExpectedDocumentSetDigest, "sha256:") || len(evidence.ExpectedDocumentSetDigest) != len("sha256:")+64) {
		return false
	}
	seen := map[string]bool{}
	for _, name := range append(append([]string{}, evidence.PropertyNames...), evidence.ContentColumns...) {
		if !ladybugIdentifier.MatchString(name) {
			return false
		}
		seen[name] = true
	}
	return seen[evidence.PrimaryKey]
}

func schemaContainsEvidence(rows []map[string]any, evidence LadybugIndexEvidence) bool {
	want := map[string]bool{evidence.PrimaryKey: true}
	for _, name := range evidence.PropertyNames {
		want[name] = true
	}
	for _, name := range evidence.ContentColumns {
		want[name] = true
	}
	primary := evidence.AllowNonPrimary
	for _, row := range rows {
		name := fmt.Sprint(row["name"])
		delete(want, name)
		if name == evidence.PrimaryKey && fmt.Sprint(row["primary key"]) == "true" {
			primary = true
		}
	}
	return len(want) == 0 && primary
}

func samePropertyNames(value any, expected []string) bool {
	if values, ok := value.([]any); ok {
		actual := make([]string, len(values))
		for index, item := range values {
			actual[index] = fmt.Sprint(item)
		}
		return slices.Equal(actual, expected)
	}
	actual := strings.Trim(fmt.Sprint(value), "[]")
	fields := strings.Fields(strings.ReplaceAll(actual, ",", " "))
	return slices.Equal(fields, expected)
}

func contentQuery(evidence LadybugIndexEvidence) string {
	columns := make([]string, 0, len(evidence.ContentColumns))
	for _, column := range evidence.ContentColumns {
		columns = append(columns, "n."+column+" AS "+column)
	}
	pattern := "(n:" + evidence.TableName + ")"
	if evidence.Relation {
		pattern = "()-[n:" + evidence.TableName + "]->()"
	}
	return "MATCH " + pattern + " RETURN " + strings.Join(columns, ", ") + " ORDER BY n." + evidence.PrimaryKey
}

func evidenceDigest(evidence LadybugIndexEvidence, backend string, schemaRows []map[string]any, indexRow map[string]any, contentRows []map[string]any) (string, error) {
	canonical := struct {
		Evidence LadybugIndexEvidence `json:"evidence"`
		Backend  string               `json:"backend"`
		Schema   []map[string]string  `json:"schema"`
		Index    map[string]string    `json:"index"`
		Content  []map[string]string  `json:"content"`
	}{Evidence: evidence, Backend: backend, Schema: stringifyRows(schemaRows), Index: stringifyRow(indexRow), Content: stringifyRows(contentRows)}
	data, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func stringifyRows(rows []map[string]any) []map[string]string {
	result := make([]map[string]string, len(rows))
	for index, row := range rows {
		result[index] = stringifyRow(row)
	}
	return result
}

func stringifyRow(row map[string]any) map[string]string {
	result := make(map[string]string, len(row))
	for key, value := range row {
		result[key] = fmt.Sprintf("%T:%v", value, value)
	}
	return result
}

func ladybugValue(value port.RawValue) (any, error) {
	switch value.Kind {
	case "string":
		return value.Value, nil
	case "int64":
		return strconv.ParseInt(value.Value, 10, 64)
	case "float64":
		return strconv.ParseFloat(value.Value, 64)
	case "bool":
		return strconv.ParseBool(value.Value)
	case "float32_array":
		var result []float32
		if err := json.Unmarshal([]byte(value.Value), &result); err != nil || len(result) == 0 {
			return nil, ErrInvalidPlan
		}
		return result, nil
	default:
		return nil, ErrInvalidPlan
	}
}
