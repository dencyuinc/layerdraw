// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
)

func TestExecuteQueryEvaluatesTypedStateForEveryGraphSubjectKind(t *testing.T) {
	t.Parallel()
	compiled := compileQueryExecutionFixture(t, allStateSubjectKindsQuerySource())
	graphValue := *compiled.TypedAST.Graph
	alpha, beta := graphValue.Entities[0], graphValue.Entities[1]
	alphaBeta := graphValue.Relations[0]
	records := []StateQuerySubject{
		stateSubject(alpha.Address, stateFields("system.updated_at", datetimeScalar("2026-01-01T00:00:00Z"))),
		stateSubject(beta.Address, stateFields("system.updated_at", datetimeScalar("2026-01-02T00:00:00Z"))),
		stateSubject(alpha.Rows[0].Address, stateFields("provenance.confidence", numberScalar(0.9))),
		stateSubject(beta.Rows[0].Address, stateFields("provenance.confidence", numberScalar(0.8))),
		stateSubject(alphaBeta.Address, stateFields("provenance.source.kind", enumScalar("api"))),
		stateSubject(alphaBeta.Rows[0].Address, stateFields("provenance.verified_at", datetimeScalar("2026-01-03T00:00:00Z"))),
	}
	state := validStateQuerySnapshot(t, compiled, records)
	wantHash, err := stateQuerySnapshotHash(state)
	if err != nil {
		t.Fatal(err)
	}
	response, err := New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{
		Recipe: compiled.TypedAST.Queries[0], Graph: graphValue, Definition: compiled.QueryDefinitionIdentity(), StateSnapshot: &state,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "ok" || response.Result == nil {
		t.Fatalf("ExecuteQuery() = %+v", response)
	}
	result := response.Result
	if !reflect.DeepEqual(result.SeedEntityAddresses, []string{alpha.Address}) || !reflect.DeepEqual(result.ReachedEntityAddresses, []string{beta.Address}) {
		t.Fatalf("state selection = seeds %v reached %v", result.SeedEntityAddresses, result.ReachedEntityAddresses)
	}
	wantInput := QueryStateInputRef{Kind: "snapshot", SnapshotHash: wantHash, StateVersion: state.StateVersion, CapturedAt: state.CapturedAt, DefinitionHash: state.DefinitionHash}
	if result.StateInput != wantInput {
		t.Fatalf("state input = %+v want %+v", result.StateInput, wantInput)
	}
	wantReads := stateReadRefsForGraph(graphValue)
	if !reflect.DeepEqual(result.StateReads, wantReads) {
		t.Fatalf("state reads =\n%+v\nwant\n%+v", result.StateReads, wantReads)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", result.Diagnostics)
	}
}

func TestExecuteQueryStateStalenessFollowsPolicy(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		source     string
		wantStatus string
	}{
		{name: "required", source: requiredStateQuerySource(), wantStatus: "rejected"},
		{name: "optional", source: optionalStateQuerySource(), wantStatus: "ok"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			compiled := compileQueryExecutionFixture(t, test.source)
			entity := compiled.TypedAST.Graph.Entities[0]
			state := validStateQuerySnapshot(t, compiled, []StateQuerySubject{
				stateSubject(entity.Address, stateFields("system.updated_at", datetimeScalar("2026-01-01T00:00:00Z"))),
			})
			state.Subjects[0].OwnSubjectHash = semanticHash('f')
			response, err := New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{
				Recipe: compiled.TypedAST.Queries[0], Graph: *compiled.TypedAST.Graph, Definition: compiled.QueryDefinitionIdentity(), StateSnapshot: &state,
			})
			if err != nil {
				t.Fatal(err)
			}
			if response.Status != test.wantStatus {
				t.Fatalf("status = %q diagnostics = %+v", response.Status, response.Diagnostics)
			}
			diagnostics := response.Diagnostics
			if response.Result != nil {
				diagnostics = response.Result.Diagnostics
			}
			wantCode := "LDL1604"
			if test.name == "optional" {
				wantCode = "LDL1605"
			}
			if !hasDiagnosticCode(diagnostics, wantCode) {
				t.Fatalf("diagnostics = %+v want %s", diagnostics, wantCode)
			}
		})
	}
}

func TestExecuteQueryRejectsInaccessibleAndRedactedStateReads(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		mutate func(*StateQuerySnapshot, string)
	}{
		{
			name: "inaccessible",
			mutate: func(state *StateQuerySnapshot, _ string) {
				state.InaccessibleFieldPaths = []string{"system.updated_at"}
				state.Subjects = []StateQuerySubject{}
			},
		},
		{
			name: "redacted",
			mutate: func(state *StateQuerySnapshot, address string) {
				state.Subjects = []StateQuerySubject{{SubjectAddress: address, OwnSubjectHash: state.Subjects[0].OwnSubjectHash, Fields: map[string]TypedScalar{}, RedactedFieldPaths: []string{"system.updated_at"}}}
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			compiled := compileQueryExecutionFixture(t, requiredStateQuerySource())
			entity := compiled.TypedAST.Graph.Entities[0]
			state := validStateQuerySnapshot(t, compiled, []StateQuerySubject{
				stateSubject(entity.Address, stateFields("system.updated_at", datetimeScalar("2026-01-01T00:00:00Z"))),
			})
			test.mutate(&state, entity.Address)
			response, err := New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{
				Recipe: compiled.TypedAST.Queries[0], Graph: *compiled.TypedAST.Graph, Definition: compiled.QueryDefinitionIdentity(), StateSnapshot: &state,
			})
			if err != nil {
				t.Fatal(err)
			}
			if response.Status != "rejected" || !hasDiagnosticCode(response.Diagnostics, "LDL1904") {
				t.Fatalf("ExecuteQuery() = %+v", response)
			}
		})
	}
}

func TestExecuteQueryRejectsMalformedStateSnapshots(t *testing.T) {
	t.Parallel()
	compiled := compileQueryExecutionFixture(t, requiredStateQuerySource())
	entity := compiled.TypedAST.Graph.Entities[0]
	base := validStateQuerySnapshot(t, compiled, []StateQuerySubject{
		stateSubject(entity.Address, stateFields("system.updated_at", datetimeScalar("2026-01-01T00:00:00Z"))),
	})
	tests := []struct {
		name   string
		mutate func(*StateQuerySnapshot)
	}{
		{name: "schema", mutate: func(state *StateQuerySnapshot) { state.SchemaVersion = 2 }},
		{name: "project", mutate: func(state *StateQuerySnapshot) { state.DefinitionProject = "ldl:project:other" }},
		{name: "metadata hash", mutate: func(state *StateQuerySnapshot) { state.DefinitionHash = "sha256:BAD" }},
		{name: "state version", mutate: func(state *StateQuerySnapshot) { state.StateVersion = "e\u0301" }},
		{name: "captured at", mutate: func(state *StateQuerySnapshot) { state.CapturedAt = "2026-01-01T09:00:00+09:00" }},
		{name: "inactive subject", mutate: func(state *StateQuerySnapshot) { state.Subjects[0].SubjectAddress = "ldl:project:p:entity:missing" }},
		{name: "invalid own hash", mutate: func(state *StateQuerySnapshot) { state.Subjects[0].OwnSubjectHash = "sha256:bad" }},
		{name: "unknown field", mutate: func(state *StateQuerySnapshot) {
			state.Subjects[0].Fields = stateFields("provider.unknown", stringScalar("x"))
		}},
		{name: "invalid typed field", mutate: func(state *StateQuerySnapshot) {
			state.Subjects[0].Fields = stateFields("provenance.confidence", numberScalar(2))
		}},
		{name: "present and redacted", mutate: func(state *StateQuerySnapshot) { state.Subjects[0].RedactedFieldPaths = []string{"system.updated_at"} }},
		{name: "inaccessible value", mutate: func(state *StateQuerySnapshot) { state.InaccessibleFieldPaths = []string{"system.updated_at"} }},
		{name: "duplicate subject", mutate: func(state *StateQuerySnapshot) {
			state.Subjects = append(state.Subjects, cloneStateSubject(state.Subjects[0]))
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := cloneStateSnapshot(base)
			test.mutate(&state)
			response, err := New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{
				Recipe: compiled.TypedAST.Queries[0], Graph: *compiled.TypedAST.Graph, Definition: compiled.QueryDefinitionIdentity(), StateSnapshot: &state,
			})
			if err != nil {
				t.Fatal(err)
			}
			if response.Status != "rejected" || len(response.Diagnostics) != 1 || response.Diagnostics[0].Code != "LDL1601" {
				t.Fatalf("ExecuteQuery() = %+v", response)
			}
		})
	}
}

func TestExecuteQueryStateIndependentIgnoresSuppliedSnapshot(t *testing.T) {
	t.Parallel()
	compiled := compileQueryExecutionFixture(t, structuralQuerySource())
	malformed := StateQuerySnapshot{Format: "invalid"}
	response, err := New(BuildInfo{}).ExecuteQuery(context.Background(), QueryExecutionInput{
		Recipe: compiled.TypedAST.Queries[0], Graph: *compiled.TypedAST.Graph, StateSnapshot: &malformed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "ok" || response.Result == nil || response.Result.StateInput != (QueryStateInputRef{Kind: "none"}) {
		t.Fatalf("ExecuteQuery() = %+v", response)
	}
}

func TestStateQuerySnapshotHashIsCanonicalAndStable(t *testing.T) {
	t.Parallel()
	compiled := compileQueryExecutionFixture(t, requiredStateQuerySource())
	entity := compiled.TypedAST.Graph.Entities[0]
	state := validStateQuerySnapshot(t, compiled, []StateQuerySubject{
		stateSubject(entity.Address, map[string]TypedScalar{
			"provenance.confidence": numberScalar(0.75),
			"system.updated_at":     datetimeScalar("2026-01-01T00:00:00Z"),
		}),
	})
	first, err := stateQuerySnapshotHash(state)
	if err != nil {
		t.Fatal(err)
	}
	reordered := cloneStateSnapshot(state)
	reordered.Subjects[0].Fields = map[string]TypedScalar{
		"system.updated_at":     datetimeScalar("2026-01-01T00:00:00Z"),
		"provenance.confidence": numberScalar(0.75),
	}
	second, err := stateQuerySnapshotHash(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("map insertion order changed canonical hash: %s != %s", first, second)
	}
	const want = "sha256:5fa29d4d1f7a3bc0fe1503d44bdc272deb7eb7b6ae08bf67845a023ac6f3e139"
	if first != want {
		t.Fatalf("canonical hash = %s want %s", first, want)
	}
}

func validStateQuerySnapshot(t *testing.T, compiled Snapshot, subjects []StateQuerySubject) StateQuerySnapshot {
	t.Helper()
	identity := compiled.QueryDefinitionIdentity()
	hashes := map[string]string{}
	for _, subject := range identity.SubjectHashes {
		hashes[subject.Address] = subject.Hash
	}
	for index := range subjects {
		if subjects[index].OwnSubjectHash == "" {
			subjects[index].OwnSubjectHash = hashes[subjects[index].SubjectAddress]
		}
	}
	sort.Slice(subjects, func(i, j int) bool {
		return compareStableAddressText(subjects[i].SubjectAddress, subjects[j].SubjectAddress) < 0
	})
	return StateQuerySnapshot{
		Format: StateQuerySnapshotFormat, SchemaVersion: StateQuerySnapshotSchemaVersion,
		DefinitionProject: identity.ProjectAddress, DefinitionHash: semanticHash('a'), GraphHash: semanticHash('b'),
		StateVersion: "state-42", CapturedAt: "2026-01-04T00:00:00Z",
		InaccessibleFieldPaths: []string{}, Subjects: subjects,
	}
}

func stateSubject(address string, fields map[string]TypedScalar) StateQuerySubject {
	return StateQuerySubject{SubjectAddress: address, Fields: fields, RedactedFieldPaths: []string{}}
}

func stateFields(path string, value TypedScalar) map[string]TypedScalar {
	return map[string]TypedScalar{path: value}
}

func datetimeScalar(value string) TypedScalar {
	return TypedScalar{Type: definition.ScalarDatetime, String: value}
}

func enumScalar(value string) TypedScalar {
	return TypedScalar{Type: definition.ScalarEnum, String: value}
}

func stringScalar(value string) TypedScalar {
	return TypedScalar{Type: definition.ScalarString, String: value}
}

func numberScalar(value float64) TypedScalar {
	return TypedScalar{Type: definition.ScalarNumber, Float: value}
}

func semanticHash(character byte) string {
	return "sha256:" + strings.Repeat(string(character), 64)
}

func cloneStateSnapshot(input StateQuerySnapshot) StateQuerySnapshot {
	out := input
	out.InaccessibleFieldPaths = append([]string{}, input.InaccessibleFieldPaths...)
	out.Subjects = make([]StateQuerySubject, len(input.Subjects))
	for index, subject := range input.Subjects {
		out.Subjects[index] = cloneStateSubject(subject)
	}
	return out
}

func cloneStateSubject(input StateQuerySubject) StateQuerySubject {
	out := input
	out.Fields = make(map[string]TypedScalar, len(input.Fields))
	for path, value := range input.Fields {
		out.Fields[path] = value
	}
	out.RedactedFieldPaths = append([]string{}, input.RedactedFieldPaths...)
	return out
}

func hasDiagnosticCode(values []Diagnostic, code string) bool {
	for _, value := range values {
		if value.Code == code {
			return true
		}
	}
	return false
}

func stateReadRefsForGraph(graphValue TypedMasterGraph) []StateReadRef {
	var reads []StateReadRef
	for _, entity := range graphValue.Entities {
		reads = append(reads, StateReadRef{SubjectAddress: entity.Address, FieldPath: "system.updated_at"})
		for _, row := range entity.Rows {
			reads = append(reads, StateReadRef{SubjectAddress: row.Address, FieldPath: "provenance.confidence"})
		}
	}
	for _, relation := range graphValue.Relations {
		reads = append(reads, StateReadRef{SubjectAddress: relation.Address, FieldPath: "provenance.source.kind"})
		for _, row := range relation.Rows {
			reads = append(reads, StateReadRef{SubjectAddress: row.Address, FieldPath: "provenance.verified_at"})
		}
	}
	sort.Slice(reads, func(i, j int) bool {
		if compared := compareStableAddressText(reads[i].SubjectAddress, reads[j].SubjectAddress); compared != 0 {
			return compared < 0
		}
		return reads[i].FieldPath < reads[j].FieldPath
	})
	return reads
}

func allStateSubjectKindsQuerySource() string {
	return strings.Replace(structuralQuerySource(), `  where all {
    rows any types [service] {
      cell environment == $environment
    }
  }
  relation_where all {
    rows any types [calls] {
      cell protocol == http
    }
  }`, `  state_input required
  where all {
    state system.updated_at exists
    rows any types [service] {
      state provenance.confidence >= 0.5
    }
  }
  relation_where all {
    state provenance.source.kind == api
    rows any types [calls] {
      state provenance.verified_at exists
    }
  }`, 1)
}
