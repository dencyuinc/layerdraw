// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package runtime

import (
	"bytes"
	"context"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

func TestCheckedCombinedCapacityBoundaries(t *testing.T) {
	tests := []struct {
		name      string
		left      int
		right     int
		want      int
		wantValid bool
	}{
		{name: "zero", left: 0, right: 0, want: 0, wantValid: true},
		{name: "maximum left", left: math.MaxInt, right: 0, want: math.MaxInt, wantValid: true},
		{name: "maximum sum", left: math.MaxInt - 1, right: 1, want: math.MaxInt, wantValid: true},
		{name: "overflow right", left: math.MaxInt, right: 1, wantValid: false},
		{name: "overflow left", left: 1, right: math.MaxInt, wantValid: false},
		{name: "negative left", left: -1, right: 1, wantValid: false},
		{name: "negative right", left: 1, right: -1, wantValid: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, valid := checkedCombinedCapacity(tc.left, tc.right)
			if got != tc.want || valid != tc.wantValid {
				t.Fatalf("got=(%d,%v) want=(%d,%v)", got, valid, tc.want, tc.wantValid)
			}
		})
	}
}

func TestBuildStateQuerySnapshotCanonicalizesAllSubjectKindsAfterAuthorization(t *testing.T) {
	t.Parallel()
	fixture := newStateSnapshotFixture()
	if !validStateHead(fixture.backend.head) {
		t.Fatalf("invalid fixture head: %+v", fixture.backend.head)
	}
	result, err := fixture.runtime().BuildStateQueryInput(context.Background(), fixture.input(StateInputPolicyRequired, port.BackendBindingLocal))
	if err != nil {
		t.Fatalf("%v resolver=%d authorization=%d", err, fixture.resolver.calls, fixture.authorization.calls)
	}
	if result.Snapshot == nil || result.StateInput.Kind != "snapshot" || result.StateInputRef.Kind != "snapshot" {
		t.Fatalf("state input = %+v", result)
	}
	engineRegistry := engineendpoint.StateFieldRegistry()
	if !reflect.DeepEqual(fixture.authorization.last.FieldPaths, engineRegistry) {
		t.Fatalf("Access field paths=%v, want Engine registry=%v", fixture.authorization.last.FieldPaths, engineRegistry)
	}
	engineRegistry[0] = semantic.StateFieldPath("mutated")
	if StateFieldRegistry()[0] != semantic.StateFieldPathSystemCreatedAt {
		t.Fatal("Runtime exposed mutable Engine registry storage")
	}
	if result.StateInput.SnapshotHash == nil || *result.StateInput.SnapshotHash != result.Snapshot.Hash() || result.StateInputRef.SnapshotHash == nil || *result.StateInputRef.SnapshotHash != result.Snapshot.Hash() {
		t.Fatalf("snapshot hash binding = %+v", result)
	}
	snapshot := result.Snapshot.Snapshot()
	if snapshot.DefinitionHash != fixture.backend.head.DefinitionHash || snapshot.GraphHash != fixture.backend.head.GraphHash {
		t.Fatalf("durable identity = %+v", snapshot)
	}
	if snapshot.DefinitionHash == fixture.definition.DefinitionHash || snapshot.GraphHash == fixture.definition.GraphHash {
		t.Fatal("stale durable definition identity was overwritten with the current definition")
	}
	if !reflect.DeepEqual(snapshot.InaccessibleFieldPaths, []semantic.StateFieldPath{semantic.StateFieldPathSystemUpdatedByID}) {
		t.Fatalf("inaccessible paths = %v", snapshot.InaccessibleFieldPaths)
	}
	wantKinds := map[semantic.StateSubjectKind]bool{
		semantic.StateSubjectKindEntity: true, semantic.StateSubjectKindRelation: true,
		semantic.StateSubjectKindEntityRow: true, semantic.StateSubjectKindRelationRow: true,
	}
	gotKinds := map[semantic.StateSubjectKind]bool{}
	for _, record := range result.Records {
		if hasClassification(record, StateSubjectMatching) || hasClassification(record, StateSubjectStale) {
			gotKinds[record.SubjectKind] = true
		}
		if _, leaked := record.Fields["provider.token"]; leaked {
			t.Fatal("provider field became queryable")
		}
	}
	if !reflect.DeepEqual(gotKinds, wantKinds) {
		t.Fatalf("subject kinds = %v", gotKinds)
	}
	relation := snapshotSubject(t, snapshot, "ldl:project:p:relation:r")
	if _, leaked := relation.Fields[string(semantic.StateFieldPathProvenanceSourceURI)]; leaked || !reflect.DeepEqual(relation.RedactedFieldPaths, []semantic.StateFieldPath{semantic.StateFieldPathProvenanceSourceURI}) {
		t.Fatalf("redacted relation = %+v", relation)
	}
	relationRow := snapshotSubject(t, snapshot, "ldl:project:p:relation:r:row:primary")
	if _, leaked := relationRow.Fields[string(semantic.StateFieldPathSystemUpdatedByID)]; leaked {
		t.Fatalf("inaccessible value leaked: %+v", relationRow)
	}
	if !hasRecordClassification(result.Records, "ldl:project:p:relation:r:row:primary", StateSubjectInaccessible) || !hasRecordClassification(result.Records, "ldl:project:p:relation:r", StateSubjectRedacted) {
		t.Fatalf("security classifications = %+v", result.Records)
	}
	if !hasRecordClassification(result.Records, "ldl:project:p:entity:missing", StateSubjectMissing) || !hasRecordClassification(result.Records, "ldl:project:p:entity:orphan", StateSubjectOrphaned) {
		t.Fatalf("partial-state classifications = %+v", result.Records)
	}
	wantActions := map[ReconcileActionKind]bool{
		ReconcileRemapState: true, ReconcileRefreshState: true, ReconcileTombstoneState: true,
		ReconcileArchiveOrphan: true, ReconcileManualReview: true,
	}
	gotActions := map[ReconcileActionKind]bool{}
	for _, action := range result.ReconciliationPlan.Actions {
		gotActions[action.Kind] = true
	}
	if !reflect.DeepEqual(gotActions, wantActions) {
		t.Fatalf("reconcile actions = %+v", result.ReconciliationPlan.Actions)
	}
	if result.ReconciliationPlan.PlanDigest == "" || result.AuthorizationDecisionDigest == nil || *result.AuthorizationDecisionDigest != fixture.authorization.decision.DecisionDigest {
		t.Fatalf("fixed decision/plan = %+v", result)
	}

	canonical := result.Snapshot.CanonicalJSON()
	if len(canonical) == 0 || canonical[len(canonical)-1] != '\n' || (len(canonical) > 1 && canonical[len(canonical)-2] == '\n') {
		t.Fatalf("canonical package JSON terminator = %q", canonical)
	}
	recomputedCanonical, recomputedHash, err := engineendpoint.CanonicalizeStateQuerySnapshot(result.Snapshot.Snapshot())
	if err != nil || recomputedHash != result.Snapshot.Hash() || !bytes.Equal(append(recomputedCanonical, '\n'), canonical) {
		t.Fatalf("snapshot does not bind Engine canonical authority: got=%s recomputed=%s err=%v", result.Snapshot.Hash(), recomputedHash, err)
	}
	canonical[0] = 'x'
	copySnapshot := result.Snapshot.Snapshot()
	copySnapshot.Subjects[0].Fields["system.updated_at"] = stateString("datetime", "2020-01-01T00:00:00Z")
	if result.Snapshot.CanonicalJSON()[0] == 'x' || reflect.DeepEqual(copySnapshot, result.Snapshot.Snapshot()) {
		t.Fatal("immutable snapshot exposed mutable backing storage")
	}
}

func TestImmutableStateQuerySnapshotNilAndScalarDefenses(t *testing.T) {
	t.Parallel()
	var absent *ImmutableStateQuerySnapshot
	if !reflect.DeepEqual(absent.Snapshot(), semantic.StateQuerySnapshot{}) || absent.CanonicalJSON() != nil || absent.Hash() != "" {
		t.Fatal("nil immutable snapshot accessors did not return zero values")
	}

	booleanValue := true
	integerValue := protocolcommon.CanonicalSafeInteger("7")
	numberValue := semantic.CanonicalFiniteDecimal("0.5")
	stringValue := "actor-1"
	immutable := &ImmutableStateQuerySnapshot{
		snapshot: semantic.StateQuerySnapshot{Subjects: []semantic.StateQuerySubject{{
			Fields: map[string]semantic.RecipeScalar{
				"boolean": {Kind: "boolean", BooleanValue: &booleanValue},
				"integer": {Kind: "integer", IntegerValue: &integerValue},
				"number":  {Kind: "number", NumberValue: &numberValue},
				"string":  {Kind: "string", StringValue: &stringValue},
			},
			RedactedFieldPaths: []semantic.StateFieldPath{},
		}}, InaccessibleFieldPaths: []semantic.StateFieldPath{}},
		canonicalJSON: []byte("fixed"),
		hash:          testDigest('e'),
	}
	copySnapshot := immutable.Snapshot()
	*copySnapshot.Subjects[0].Fields["boolean"].BooleanValue = false
	*copySnapshot.Subjects[0].Fields["integer"].IntegerValue = "8"
	*copySnapshot.Subjects[0].Fields["number"].NumberValue = "0.6"
	*copySnapshot.Subjects[0].Fields["string"].StringValue = "mutated"
	copyJSON := immutable.CanonicalJSON()
	copyJSON[0] = 'x'
	again := immutable.Snapshot().Subjects[0].Fields
	if !*again["boolean"].BooleanValue || *again["integer"].IntegerValue != "7" || *again["number"].NumberValue != "0.5" || *again["string"].StringValue != "actor-1" || string(immutable.CanonicalJSON()) != "fixed" || immutable.Hash() != testDigest('e') {
		t.Fatalf("immutable scalar backing storage escaped: %+v", again)
	}
}

func TestBuildStateQuerySnapshotUsesEngineCanonicalAuthorityForAdversarialScalars(t *testing.T) {
	t.Parallel()
	fixture := newStateSnapshotFixture()
	const htmlSensitive = "<state>\u2028line\u2029"
	fixture.backend.state.Records[0].Fields[string(semantic.StateFieldPathProvenanceSourceLabel)] = stateString("string", htmlSensitive)
	fixture.backend.state.Records[2].Fields[string(semantic.StateFieldPathProvenanceConfidence)] = stateNumber("1e-7")

	first, err := fixture.runtime().BuildStateQueryInput(context.Background(), fixture.input(StateInputPolicyRequired, port.BackendBindingLocal))
	if err != nil {
		t.Fatal(err)
	}
	engineCanonical, engineHash, err := engine.CanonicalizeStateQuerySnapshot(engineStateSnapshot(t, first.Snapshot.Snapshot()))
	if err != nil {
		t.Fatal(err)
	}
	protocolCanonical, protocolHash, err := engineendpoint.CanonicalizeStateQuerySnapshot(first.Snapshot.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if first.Snapshot.Hash() != protocolcommon.Digest(engineHash) || protocolHash != first.Snapshot.Hash() || !bytes.Equal(first.Snapshot.CanonicalJSON(), append(protocolCanonical, '\n')) {
		t.Fatalf("Runtime snapshot diverged from Engine authority: runtime=%s engine=%s", first.Snapshot.Hash(), engineHash)
	}
	if !bytes.Contains(engineCanonical, []byte(htmlSensitive)) || bytes.Contains(engineCanonical, []byte(`\u003c`)) || bytes.Contains(engineCanonical, []byte(`\u2028`)) {
		t.Fatalf("Engine RFC 8785 string encoding = %q", engineCanonical)
	}
	if !bytes.Contains(engineCanonical, []byte(`1e-7`)) {
		t.Fatalf("Engine canonical numeric spelling missing: %s", engineCanonical)
	}
	second, err := fixture.runtime().BuildStateQueryInput(context.Background(), fixture.input(StateInputPolicyRequired, port.BackendBindingLocal))
	if err != nil {
		t.Fatal(err)
	}
	if second.Snapshot.Hash() != first.Snapshot.Hash() || !bytes.Equal(second.Snapshot.CanonicalJSON(), first.Snapshot.CanonicalJSON()) {
		t.Fatal("identical fixed state produced different canonical bytes or hash")
	}
}

func TestBuildStateQuerySnapshotPreservesActiveKindAndMissingRedaction(t *testing.T) {
	t.Parallel()
	fixture := newStateSnapshotFixture()
	target := semantic.StableAddress("ldl:project:p:entity:new")
	fixture.backend.state.Records[0].SubjectKind = semantic.StateSubjectKindRelation
	fixture.authorization.decision.RedactedFieldPaths = map[semantic.StableAddress][]semantic.StateFieldPath{
		target: {semantic.StateFieldPathSystemCreatedByID},
	}

	result, err := fixture.runtime().BuildStateQueryInput(context.Background(), fixture.input(StateInputPolicyRequired, port.BackendBindingLocal))
	if err != nil {
		t.Fatal(err)
	}
	var authorized *port.StateQueryAuthorizationSubject
	for index := range fixture.authorization.last.Subjects {
		if fixture.authorization.last.Subjects[index].SubjectAddress == target {
			authorized = &fixture.authorization.last.Subjects[index]
			break
		}
	}
	if authorized == nil || authorized.SubjectKind != semantic.StateSubjectKindEntity {
		t.Fatalf("Access subject=%+v, want active Entity kind", authorized)
	}
	subject := snapshotSubject(t, result.Snapshot.Snapshot(), target)
	if subject.OwnSubjectHash != activeHash(t, fixture.definition, target) || !reflect.DeepEqual(subject.RedactedFieldPaths, []semantic.StateFieldPath{semantic.StateFieldPathSystemCreatedByID}) {
		t.Fatalf("active redaction was lost: %+v", subject)
	}
	if !hasRecordClassification(result.Records, target, StateSubjectMissing) || !hasRecordClassification(result.Records, target, StateSubjectRedacted) {
		t.Fatalf("active classifications=%+v", result.Records)
	}
}

func TestBuildStateQuerySnapshotUsesEngineAddressOrderForRecordsAndReconciliation(t *testing.T) {
	t.Parallel()
	projectEntity := semantic.StableAddress("ldl:project:p:entity:z")
	projectRelation := semantic.StableAddress("ldl:project:p:relation:a")
	projectRow := semantic.StableAddress("ldl:project:p:entity:z:row:a")
	packQuery := semantic.StableAddress("ldl:pack:pub:name:query:a")
	ordered := []semantic.StableAddress{projectEntity, projectRelation, projectRow, packQuery}
	records := []CanonicalStateRecord{{SubjectAddress: packQuery}, {SubjectAddress: projectRow}, {SubjectAddress: projectRelation}, {SubjectAddress: projectEntity}}
	sortCanonicalStateRecords(records)
	gotRecords := make([]semantic.StableAddress, len(records))
	for index, record := range records {
		gotRecords[index] = record.SubjectAddress
	}
	if !reflect.DeepEqual(gotRecords, ordered) {
		t.Fatalf("canonical record order = %v want %v", gotRecords, ordered)
	}

	plan, err := reconciliationPlan([]CanonicalStateRecord{
		{SourceAddress: packQuery, SubjectAddress: projectEntity, Classifications: []StateSubjectClassification{StateSubjectMatching}},
		{SourceAddress: projectRow, SubjectAddress: projectEntity, Classifications: []StateSubjectClassification{StateSubjectMatching}},
		{SubjectAddress: packQuery, Classifications: []StateSubjectClassification{StateSubjectMissing}},
		{SubjectAddress: projectRow, Classifications: []StateSubjectClassification{StateSubjectMissing}},
		{SubjectAddress: projectRelation, Classifications: []StateSubjectClassification{StateSubjectMissing}},
	})
	if err != nil {
		t.Fatal(err)
	}
	gotActions := make([]string, len(plan.Actions))
	for index, action := range plan.Actions {
		gotActions[index] = string(action.Kind) + "|" + string(action.SubjectAddress) + "|" + string(optionalAddress(action.SourceAddress))
	}
	wantActions := []string{
		"remap_state|ldl:project:p:entity:z|ldl:project:p:entity:z:row:a",
		"remap_state|ldl:project:p:entity:z|ldl:pack:pub:name:query:a",
		"refresh_state|ldl:project:p:relation:a|",
		"refresh_state|ldl:project:p:entity:z:row:a|",
		"refresh_state|ldl:pack:pub:name:query:a|",
	}
	if !reflect.DeepEqual(gotActions, wantActions) {
		t.Fatalf("canonical reconciliation order = %v want %v", gotActions, wantActions)
	}
}

func TestBuildStateQueryInputResolvesOnlyExplicitBindingsAndMapsPolicies(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		policy      StateInputPolicy
		binding     port.BackendBindingKind
		wantKind    string
		wantResolve int
	}{
		{"none policy ignores local", StateInputPolicyNone, port.BackendBindingLocal, "none", 0},
		{"optional explicit none", StateInputPolicyOptional, port.BackendBindingNone, "none", 0},
		{"required explicit none", StateInputPolicyRequired, port.BackendBindingNone, "none", 0},
		{"optional local", StateInputPolicyOptional, port.BackendBindingLocal, "snapshot", 1},
		{"required packaged", StateInputPolicyRequired, port.BackendBindingPackaged, "snapshot", 1},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newStateSnapshotFixture()
			result, err := fixture.runtime().BuildStateQueryInput(context.Background(), fixture.input(test.policy, test.binding))
			if err != nil {
				t.Fatal(err)
			}
			if result.StateInput.Kind != test.wantKind || fixture.resolver.calls != test.wantResolve {
				t.Fatalf("result=%+v resolver calls=%d", result, fixture.resolver.calls)
			}
			if test.wantResolve != 0 && fixture.resolver.last.Kind != test.binding {
				t.Fatalf("resolved binding = %+v", fixture.resolver.last)
			}
		})
	}
}

func TestBuildStateQuerySnapshotAppliesAuthorizationBeforeScalarCanonicalization(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		record  int
		address semantic.StableAddress
		path    semantic.StateFieldPath
		value   semantic.RecipeScalar
	}{
		{name: "valid datetime has wrong scalar kind for actor id", record: 0, address: "ldl:project:p:entity:new", path: semantic.StateFieldPathSystemUpdatedByID, value: stateString("datetime", "2026-07-18T00:00:00Z")},
		{name: "invalid actor enum", record: 0, address: "ldl:project:p:entity:new", path: semantic.StateFieldPathSystemUpdatedByKind, value: stateString("enum", "robot")},
		{name: "invalid source enum", record: 1, address: "ldl:project:p:relation:r", path: semantic.StateFieldPathProvenanceSourceKind, value: stateString("enum", "robot")},
		{name: "confidence above bound", record: 2, address: "ldl:project:p:entity:new:row:primary", path: semantic.StateFieldPathProvenanceConfidence, value: stateNumber("2")},
		{name: "noncanonical number spelling", record: 2, address: "ldl:project:p:entity:new:row:primary", path: semantic.StateFieldPathProvenanceConfidence, value: stateNumber("1e-06")},
		{name: "noncanonical datetime", record: 0, address: "ldl:project:p:entity:new", path: semantic.StateFieldPathSystemUpdatedAt, value: stateString("datetime", "2026-07-18T09:00:00+09:00")},
		{name: "malformed datetime", record: 0, address: "ldl:project:p:entity:new", path: semantic.StateFieldPathSystemUpdatedAt, value: stateString("datetime", "2026-02-30T00:00:00Z")},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			newFixture := func() *stateSnapshotFixture {
				fixture := newStateSnapshotFixture()
				fixture.authorization.decision.InaccessibleFieldPaths = []semantic.StateFieldPath{}
				fixture.authorization.decision.RedactedFieldPaths = map[semantic.StableAddress][]semantic.StateFieldPath{}
				fixture.backend.state.Records[test.record].Fields[string(test.path)] = test.value
				return fixture
			}

			allowed := newFixture()
			if _, err := allowed.runtime().BuildStateQueryInput(context.Background(), allowed.input(StateInputPolicyRequired, port.BackendBindingLocal)); !isStateSnapshotError(err, StateSnapshotInvalid) {
				t.Fatalf("allowed invalid value error = %v", err)
			}

			globallyInaccessible := newFixture()
			globallyInaccessible.authorization.decision.InaccessibleFieldPaths = []semantic.StateFieldPath{test.path}
			built, err := globallyInaccessible.runtime().BuildStateQueryInput(context.Background(), globallyInaccessible.input(StateInputPolicyRequired, port.BackendBindingLocal))
			if err != nil {
				t.Fatalf("globally inaccessible raw value reached canonicalization: %v", err)
			}
			if !reflect.DeepEqual(built.Snapshot.Snapshot().InaccessibleFieldPaths, []semantic.StateFieldPath{test.path}) {
				t.Fatalf("global denial marker = %v", built.Snapshot.Snapshot().InaccessibleFieldPaths)
			}

			subjectRedacted := newFixture()
			subjectRedacted.authorization.decision.RedactedFieldPaths = map[semantic.StableAddress][]semantic.StateFieldPath{test.address: {test.path}}
			built, err = subjectRedacted.runtime().BuildStateQueryInput(context.Background(), subjectRedacted.input(StateInputPolicyRequired, port.BackendBindingLocal))
			if err != nil {
				t.Fatalf("subject-redacted raw value reached canonicalization: %v", err)
			}
			subject := snapshotSubject(t, built.Snapshot.Snapshot(), test.address)
			if !reflect.DeepEqual(subject.RedactedFieldPaths, []semantic.StateFieldPath{test.path}) {
				t.Fatalf("subject denial marker = %v", subject.RedactedFieldPaths)
			}
			if _, present := subject.Fields[string(test.path)]; present {
				t.Fatalf("subject-redacted invalid value remained present: %+v", subject)
			}
		})
	}
}

func TestBuildStateQuerySnapshotFailurePaths(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		edit func(*stateSnapshotFixture)
		code StateSnapshotErrorCode
	}{
		{"resolver failure", func(f *stateSnapshotFixture) { f.resolver.err = errors.New("injected") }, StateSnapshotBackendUnavailable},
		{"backend failure", func(f *stateSnapshotFixture) { f.backend.headErr = errors.New("injected") }, StateSnapshotBackendUnavailable},
		{"authorization failure", func(f *stateSnapshotFixture) { f.authorization.err = errors.New("injected") }, StateSnapshotAuthorizationInvalid},
		{"stale access fingerprint", func(f *stateSnapshotFixture) { f.authorization.decision.AccessFingerprint = testDigest('9') }, StateSnapshotAuthorizationInvalid},
		{"invalid decision field", func(f *stateSnapshotFixture) {
			f.authorization.decision.InaccessibleFieldPaths = []semantic.StateFieldPath{"provider.token"}
		}, StateSnapshotAuthorizationInvalid},
		{"state head mismatch", func(f *stateSnapshotFixture) { f.backend.state.Head.StateVersion = "8" }, StateSnapshotInvalid},
		{"duplicate state record", func(f *stateSnapshotFixture) {
			f.backend.state.Records = append(f.backend.state.Records, f.backend.state.Records[0])
		}, StateSnapshotInvalid},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newStateSnapshotFixture()
			test.edit(fixture)
			_, err := fixture.runtime().BuildStateQueryInput(context.Background(), fixture.input(StateInputPolicyRequired, port.BackendBindingLocal))
			if !isStateSnapshotError(err, test.code) {
				t.Fatalf("error = %v want %s", err, test.code)
			}
		})
	}
}

func TestBuildStateQueryInputDistinguishesBackendAbsenceAndFailure(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		edit        func(*stateSnapshotFixture)
		runtime     func(*stateSnapshotFixture) *Runtime
		wantCode    StateSnapshotErrorCode
		wantNoState bool
	}{
		{name: "missing binding resolver", runtime: func(f *stateSnapshotFixture) *Runtime {
			return &Runtime{config: Config{Ports: Ports{StateAccess: f.authorization}}}
		}, wantCode: StateSnapshotBackendUnavailable},
		{name: "missing authorization", runtime: func(f *stateSnapshotFixture) *Runtime {
			return &Runtime{config: Config{Ports: Ports{StateBindings: f.resolver}}}
		}, wantCode: StateSnapshotBackendUnavailable},
		{name: "binding not found", edit: func(f *stateSnapshotFixture) { f.resolver.err = port.ErrNotFound }, wantNoState: true},
		{name: "binding unavailable", edit: func(f *stateSnapshotFixture) { f.resolver.err = errors.New("unavailable") }, wantCode: StateSnapshotBackendUnavailable},
		{name: "nil backend", edit: func(f *stateSnapshotFixture) { f.resolver.backend = nil }, wantCode: StateSnapshotBackendUnavailable},
		{name: "head not found", edit: func(f *stateSnapshotFixture) { f.backend.headErr = port.ErrNotFound }, wantNoState: true},
		{name: "head unavailable", edit: func(f *stateSnapshotFixture) { f.backend.headErr = errors.New("unavailable") }, wantCode: StateSnapshotBackendUnavailable},
		{name: "read not found", edit: func(f *stateSnapshotFixture) { f.backend.readErr = port.ErrNotFound }, wantNoState: true},
		{name: "read unavailable", edit: func(f *stateSnapshotFixture) { f.backend.readErr = errors.New("unavailable") }, wantCode: StateSnapshotBackendUnavailable},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newStateSnapshotFixture()
			if test.edit != nil {
				test.edit(fixture)
			}
			runtimeInstance := fixture.runtime()
			if test.runtime != nil {
				runtimeInstance = test.runtime(fixture)
			}
			result, err := runtimeInstance.BuildStateQueryInput(context.Background(), fixture.input(StateInputPolicyRequired, port.BackendBindingLocal))
			if test.wantNoState {
				if err != nil || result.StateInput.Kind != "none" || result.StateInputRef.Kind != "none" || result.Records == nil || result.ReconciliationPlan.PlanDigest == "" {
					t.Fatalf("no-state result=%+v error=%v", result, err)
				}
				return
			}
			if !isStateSnapshotError(err, test.wantCode) {
				t.Fatalf("error=%v want %s", err, test.wantCode)
			}
		})
	}
}

func TestBuildStateQueryInputRejectsInvalidTrustedDefinitionShapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		edit func(*BuildStateQueryInput)
		code StateSnapshotErrorCode
	}{
		{name: "scope", edit: func(input *BuildStateQueryInput) { input.Scope.DocumentID = "" }, code: StateSnapshotBindingInvalid},
		{name: "policy", edit: func(input *BuildStateQueryInput) { input.Policy = "sometimes" }, code: StateSnapshotBindingInvalid},
		{name: "none binding id", edit: func(input *BuildStateQueryInput) {
			input.Binding = port.BackendBinding{Kind: port.BackendBindingNone, BindingID: "configured"}
		}, code: StateSnapshotBindingInvalid},
		{name: "local missing id", edit: func(input *BuildStateQueryInput) { input.Binding.BindingID = "" }, code: StateSnapshotBindingInvalid},
		{name: "unknown binding", edit: func(input *BuildStateQueryInput) {
			input.Binding = port.BackendBinding{Kind: "cloud", BindingID: "configured"}
		}, code: StateSnapshotBindingInvalid},
		{name: "project address", edit: func(input *BuildStateQueryInput) { input.Definition.ProjectAddress = "invalid" }, code: StateSnapshotInvalid},
		{name: "definition hash", edit: func(input *BuildStateQueryInput) { input.Definition.DefinitionHash = "invalid" }, code: StateSnapshotInvalid},
		{name: "graph hash", edit: func(input *BuildStateQueryInput) { input.Definition.GraphHash = "invalid" }, code: StateSnapshotInvalid},
		{name: "duplicate subject", edit: func(input *BuildStateQueryInput) {
			input.Definition.SubjectHashes = append(input.Definition.SubjectHashes, input.Definition.SubjectHashes[0])
		}, code: StateSnapshotInvalid},
		{name: "subject address", edit: func(input *BuildStateQueryInput) { input.Definition.SubjectHashes[0].Address = "invalid" }, code: StateSnapshotInvalid},
		{name: "subject hash", edit: func(input *BuildStateQueryInput) { input.Definition.SubjectHashes[0].Hash = "invalid" }, code: StateSnapshotInvalid},
		{name: "duplicate move source", edit: func(input *BuildStateQueryInput) {
			input.Definition.AddressMoves = append(input.Definition.AddressMoves, input.Definition.AddressMoves[0])
		}, code: StateSnapshotInvalid},
		{name: "self move", edit: func(input *BuildStateQueryInput) {
			input.Definition.AddressMoves[0].SourceAddress = input.Definition.AddressMoves[0].TargetAddress
		}, code: StateSnapshotInvalid},
		{name: "move source", edit: func(input *BuildStateQueryInput) { input.Definition.AddressMoves[0].SourceAddress = "invalid" }, code: StateSnapshotInvalid},
		{name: "move target", edit: func(input *BuildStateQueryInput) { input.Definition.AddressMoves[0].TargetAddress = "invalid" }, code: StateSnapshotInvalid},
		{name: "move target absent", edit: func(input *BuildStateQueryInput) {
			input.Definition.AddressMoves[0].TargetAddress = "ldl:project:p:entity:absent"
		}, code: StateSnapshotInvalid},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newStateSnapshotFixture()
			input := fixture.input(StateInputPolicyRequired, port.BackendBindingLocal)
			test.edit(&input)
			_, err := fixture.runtime().BuildStateQueryInput(context.Background(), input)
			if !isStateSnapshotError(err, test.code) {
				t.Fatalf("error=%v want %s", err, test.code)
			}
		})
	}
}

func TestBuildStateQueryInputRejectsInvalidHeadRecordAndDecisionShapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		edit func(*stateSnapshotFixture)
		code StateSnapshotErrorCode
	}{
		{name: "invalid head", edit: func(f *stateSnapshotFixture) { f.backend.head.BackendVersion = "" }, code: StateSnapshotInvalid},
		{name: "record address", edit: func(f *stateSnapshotFixture) { f.backend.state.Records[0].SubjectAddress = "invalid" }, code: StateSnapshotInvalid},
		{name: "record kind", edit: func(f *stateSnapshotFixture) { f.backend.state.Records[0].SubjectKind = "unknown" }, code: StateSnapshotInvalid},
		{name: "record hash", edit: func(f *stateSnapshotFixture) { f.backend.state.Records[0].OwnSubjectHash = "invalid" }, code: StateSnapshotInvalid},
		{name: "record fields nil", edit: func(f *stateSnapshotFixture) { f.backend.state.Records[0].Fields = nil }, code: StateSnapshotInvalid},
		{name: "record head hash missing", edit: func(f *stateSnapshotFixture) {
			delete(f.backend.state.Head.SubjectHashes, f.backend.state.Records[0].SubjectAddress)
			f.backend.head = f.backend.state.Head
		}, code: StateSnapshotInvalid},
		{name: "record head hash mismatch", edit: func(f *stateSnapshotFixture) {
			f.backend.state.Head.SubjectHashes[f.backend.state.Records[0].SubjectAddress] = testDigest('e')
			f.backend.head = f.backend.state.Head
		}, code: StateSnapshotInvalid},
		{name: "durable inaccessible field", edit: func(f *stateSnapshotFixture) {
			f.backend.state.InaccessibleFieldPaths = []semantic.StateFieldPath{"provider.token"}
		}, code: StateSnapshotAuthorizationInvalid},
		{name: "decision digest", edit: func(f *stateSnapshotFixture) { f.authorization.decision.DecisionDigest = "invalid" }, code: StateSnapshotAuthorizationInvalid},
		{name: "redacted unknown subject", edit: func(f *stateSnapshotFixture) {
			f.authorization.decision.RedactedFieldPaths["ldl:project:p:entity:unknown"] = []semantic.StateFieldPath{semantic.StateFieldPathSystemUpdatedAt}
		}, code: StateSnapshotAuthorizationInvalid},
		{name: "redacted decision field", edit: func(f *stateSnapshotFixture) {
			f.authorization.decision.RedactedFieldPaths["ldl:project:p:entity:new"] = []semantic.StateFieldPath{"provider.token"}
		}, code: StateSnapshotAuthorizationInvalid},
		{name: "redacted durable field", edit: func(f *stateSnapshotFixture) {
			f.backend.state.Records[0].RedactedFieldPaths = []semantic.StateFieldPath{"provider.token"}
		}, code: StateSnapshotInvalid},
		{name: "duplicate resolved snapshot subject", edit: func(f *stateSnapshotFixture) {
			copyRecord := f.backend.state.Records[0]
			copyRecord.SubjectAddress = "ldl:project:p:entity:new"
			f.backend.state.Records = append(f.backend.state.Records, copyRecord)
			f.backend.state.Head.SubjectHashes[copyRecord.SubjectAddress] = copyRecord.OwnSubjectHash
			f.backend.head = f.backend.state.Head
		}, code: StateSnapshotInvalid},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newStateSnapshotFixture()
			test.edit(fixture)
			_, err := fixture.runtime().BuildStateQueryInput(context.Background(), fixture.input(StateInputPolicyRequired, port.BackendBindingLocal))
			if !isStateSnapshotError(err, test.code) {
				t.Fatalf("error=%v want %s", err, test.code)
			}
		})
	}
}

func TestRuntimeSnapshotFeedsEnginePoliciesStalenessAndRedaction(t *testing.T) {
	t.Parallel()
	const source = `
project p "Project" {}
layers {
  app "Application" @10
}
entity_type service "Service" {
  representation shape rect
}
entities service @app {
  alpha "Alpha"
}
query current "Current" {
  state_input required
  select {
    roots [alpha]
  }
  where all {
    state system.updated_at exists
  }
}
query optional_current "Optional current" {
  state_input optional
  select {
    roots [alpha]
  }
  where all {
    state system.updated_at exists
  }
}
query structural "Structural" {
  select {
    roots [alpha]
  }
}
`
	compiled, err := engine.New(engine.BuildInfo{}).Compile(context.Background(), engine.CompileInput{
		Mode: engine.CompileProject, EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte(source)},
		ResolvedDependencies: engine.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := compiled.Snapshot()
	if len(snapshot.Diagnostics) != 0 || snapshot.TypedAST.Project == nil || snapshot.TypedAST.Graph == nil || snapshot.GraphHash == nil {
		t.Fatalf("compile snapshot = %+v", snapshot.Diagnostics)
	}
	queryIndexes := map[string]int{}
	for index, query := range snapshot.TypedAST.Queries {
		queryIndexes[query.ID] = index
	}
	for _, queryID := range []string{"current", "optional_current", "structural"} {
		if _, ok := queryIndexes[queryID]; !ok {
			t.Fatalf("compiled query %q is missing", queryID)
		}
	}
	definition := StateQueryDefinition{ProjectAddress: semantic.ProjectRootAddress(snapshot.TypedAST.Project.Address), DefinitionHash: protocolcommon.Digest(snapshot.DefinitionHash), GraphHash: protocolcommon.Digest(*snapshot.GraphHash)}
	for _, subject := range snapshot.SubjectSemanticHashes {
		definition.SubjectHashes = append(definition.SubjectHashes, semantic.SubjectHash{Address: semantic.StableAddress(subject.Address), Hash: protocolcommon.Digest(subject.Hash), Kind: semantic.SubjectKind(subject.Kind)})
	}
	entityAddress := semantic.StableAddress(snapshot.TypedAST.Graph.Entities[0].Address)
	entityHash := activeHash(t, definition, entityAddress)
	head := port.StateHead{StateVersion: "1", BackendVersion: "state-1", DefinitionHash: definition.DefinitionHash, GraphHash: definition.GraphHash, CapturedAt: "2026-07-18T00:00:00Z", SubjectHashes: map[semantic.StableAddress]protocolcommon.Digest{entityAddress: entityHash}}
	backend := &snapshotBackend{head: head, state: port.StateSnapshot{Head: head, Records: []port.StateRecord{{SubjectAddress: entityAddress, SubjectKind: semantic.StateSubjectKindEntity, OwnSubjectHash: entityHash, Fields: map[string]semantic.RecipeScalar{string(semantic.StateFieldPathSystemUpdatedAt): stateString("datetime", "2026-07-18T00:00:00Z")}}}}}
	scope := testScope()
	authorization := &snapshotAuthorization{decision: port.StateQueryAuthorizationDecision{AccessFingerprint: scope.AccessFingerprint, DecisionDigest: testDigest('8'), InaccessibleFieldPaths: []semantic.StateFieldPath{}, RedactedFieldPaths: map[semantic.StableAddress][]semantic.StateFieldPath{}}}
	runtimeInstance := &Runtime{config: Config{Ports: Ports{StateBindings: &snapshotResolver{backend: backend}, StateAccess: authorization}}}
	built, err := runtimeInstance.BuildStateQueryInput(context.Background(), BuildStateQueryInput{Scope: scope, Binding: port.BackendBinding{Kind: port.BackendBindingLocal, BindingID: "local"}, Policy: StateInputPolicyRequired, Definition: definition})
	if err != nil {
		t.Fatal(err)
	}
	engineState := engineStateSnapshot(t, built.Snapshot.Snapshot())
	engineInstance := engine.New(engine.BuildInfo{})
	execute := func(queryID string, state *engine.StateQuerySnapshot) engine.QueryExecutionResponse {
		t.Helper()
		response, executeErr := engineInstance.ExecuteQuery(context.Background(), engine.QueryExecutionInput{Recipe: snapshot.TypedAST.Queries[queryIndexes[queryID]], Graph: *snapshot.TypedAST.Graph, Definition: snapshot.QueryDefinitionIdentity(), StateSnapshot: state})
		if executeErr != nil {
			t.Fatal(executeErr)
		}
		return response
	}
	response := execute("current", &engineState)
	if response.Status != "ok" || response.Result == nil {
		t.Fatalf("Engine response = %+v", response)
	}
	if response.Result.StateInput.SnapshotHash != string(built.Snapshot.Hash()) || response.Result.StateInput.Kind != "snapshot" {
		t.Fatalf("Engine StateInputRef = %+v want hash %s", response.Result.StateInput, built.Snapshot.Hash())
	}

	adversarialPaths := []semantic.StateFieldPath{
		semantic.StateFieldPathSystemUpdatedByKind,
		semantic.StateFieldPathSystemCreatedByID,
	}
	wantCanonicalPaths := []semantic.StateFieldPath{
		semantic.StateFieldPathSystemCreatedByID,
		semantic.StateFieldPathSystemUpdatedByKind,
	}
	authorization.decision.InaccessibleFieldPaths = adversarialPaths
	inaccessiblePair, err := runtimeInstance.BuildStateQueryInput(context.Background(), BuildStateQueryInput{Scope: scope, Binding: port.BackendBinding{Kind: port.BackendBindingLocal, BindingID: "local"}, Policy: StateInputPolicyRequired, Definition: definition})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(authorization.last.FieldPaths, engineendpoint.StateFieldRegistry()) || !reflect.DeepEqual(inaccessiblePair.Snapshot.Snapshot().InaccessibleFieldPaths, wantCanonicalPaths) {
		t.Fatalf("inaccessible registry projection: access=%v snapshot=%v", authorization.last.FieldPaths, inaccessiblePair.Snapshot.Snapshot().InaccessibleFieldPaths)
	}
	inaccessiblePairState := engineStateSnapshot(t, inaccessiblePair.Snapshot.Snapshot())
	response = execute("current", &inaccessiblePairState)
	if response.Status != "ok" || response.Result == nil {
		t.Fatalf("multi-path inaccessible snapshot self-rejected: %+v", response)
	}

	authorization.decision.InaccessibleFieldPaths = []semantic.StateFieldPath{}
	authorization.decision.RedactedFieldPaths = map[semantic.StableAddress][]semantic.StateFieldPath{entityAddress: adversarialPaths}
	redactedPair, err := runtimeInstance.BuildStateQueryInput(context.Background(), BuildStateQueryInput{Scope: scope, Binding: port.BackendBinding{Kind: port.BackendBindingLocal, BindingID: "local"}, Policy: StateInputPolicyRequired, Definition: definition})
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshotSubject(t, redactedPair.Snapshot.Snapshot(), entityAddress).RedactedFieldPaths; !reflect.DeepEqual(got, wantCanonicalPaths) {
		t.Fatalf("redacted paths=%v, want %v", got, wantCanonicalPaths)
	}
	redactedPairState := engineStateSnapshot(t, redactedPair.Snapshot.Snapshot())
	response = execute("current", &redactedPairState)
	if response.Status != "ok" || response.Result == nil {
		t.Fatalf("multi-path redacted snapshot self-rejected: %+v", response)
	}
	authorization.decision.RedactedFieldPaths = map[semantic.StableAddress][]semantic.StateFieldPath{}

	requiredMissing, err := runtimeInstance.BuildStateQueryInput(context.Background(), BuildStateQueryInput{Scope: scope, Binding: port.BackendBinding{Kind: port.BackendBindingNone}, Policy: StateInputPolicyRequired, Definition: definition})
	if err != nil {
		t.Fatal(err)
	}
	response = execute("current", nil)
	if response.Status != "rejected" || !hasEngineDiagnostic(response.Diagnostics, "LDL1604") || requiredMissing.StateInput.Kind != "none" {
		t.Fatalf("required no-state response = %+v", response)
	}
	optionalMissing, err := runtimeInstance.BuildStateQueryInput(context.Background(), BuildStateQueryInput{Scope: scope, Binding: port.BackendBinding{Kind: port.BackendBindingNone}, Policy: StateInputPolicyOptional, Definition: definition})
	if err != nil {
		t.Fatal(err)
	}
	response = execute("optional_current", nil)
	if response.Status != "ok" || response.Result == nil || response.Result.StateInput.Kind != "none" || !hasEngineDiagnostic(response.Result.Diagnostics, "LDL1605") || optionalMissing.StateInput.Kind != "none" {
		t.Fatalf("optional no-state response = %+v", response)
	}
	noState, err := runtimeInstance.BuildStateQueryInput(context.Background(), BuildStateQueryInput{Scope: scope, Binding: port.BackendBinding{Kind: port.BackendBindingLocal, BindingID: "local"}, Policy: StateInputPolicyNone, Definition: definition})
	if err != nil {
		t.Fatal(err)
	}
	response = execute("structural", nil)
	if response.Status != "ok" || response.Result == nil || response.Result.StateInput.Kind != "none" || noState.StateInput.Kind != "none" {
		t.Fatalf("state-independent response = %+v", response)
	}

	staleHash := testDigest('9')
	backend.head.SubjectHashes[entityAddress] = staleHash
	backend.state.Head = backend.head
	backend.state.Records[0].OwnSubjectHash = staleHash
	stale, err := runtimeInstance.BuildStateQueryInput(context.Background(), BuildStateQueryInput{Scope: scope, Binding: port.BackendBinding{Kind: port.BackendBindingLocal, BindingID: "local"}, Policy: StateInputPolicyRequired, Definition: definition})
	if err != nil {
		t.Fatal(err)
	}
	staleState := engineStateSnapshot(t, stale.Snapshot.Snapshot())
	response = execute("current", &staleState)
	if response.Status != "rejected" || !hasEngineDiagnostic(response.Diagnostics, "LDL1604") || !hasRecordClassification(stale.Records, entityAddress, StateSubjectStale) {
		t.Fatalf("required stale response = %+v records=%+v", response, stale.Records)
	}

	backend.head.SubjectHashes = map[semantic.StableAddress]protocolcommon.Digest{}
	backend.state = port.StateSnapshot{Head: backend.head, Records: []port.StateRecord{}}
	authorization.decision.RedactedFieldPaths = map[semantic.StableAddress][]semantic.StateFieldPath{entityAddress: {semantic.StateFieldPathSystemUpdatedAt}}
	redacted, err := runtimeInstance.BuildStateQueryInput(context.Background(), BuildStateQueryInput{Scope: scope, Binding: port.BackendBinding{Kind: port.BackendBindingLocal, BindingID: "local"}, Policy: StateInputPolicyRequired, Definition: definition})
	if err != nil {
		t.Fatal(err)
	}
	redactedSubject := snapshotSubject(t, redacted.Snapshot.Snapshot(), entityAddress)
	if redactedSubject.OwnSubjectHash != entityHash || !reflect.DeepEqual(redactedSubject.RedactedFieldPaths, []semantic.StateFieldPath{semantic.StateFieldPathSystemUpdatedAt}) ||
		!hasRecordClassification(redacted.Records, entityAddress, StateSubjectMissing) || !hasRecordClassification(redacted.Records, entityAddress, StateSubjectRedacted) {
		t.Fatalf("missing redaction was not preserved: subject=%+v records=%+v", redactedSubject, redacted.Records)
	}
	redactedState := engineStateSnapshot(t, redacted.Snapshot.Snapshot())
	response = execute("current", &redactedState)
	if response.Status != "rejected" || !hasEngineDiagnostic(response.Diagnostics, "LDL1904") {
		t.Fatalf("redacted missing subject response = %+v", response)
	}

	authorization.decision.RedactedFieldPaths = map[semantic.StableAddress][]semantic.StateFieldPath{}
	authorization.decision.InaccessibleFieldPaths = []semantic.StateFieldPath{semantic.StateFieldPathSystemUpdatedAt}
	inaccessible, err := runtimeInstance.BuildStateQueryInput(context.Background(), BuildStateQueryInput{Scope: scope, Binding: port.BackendBinding{Kind: port.BackendBindingLocal, BindingID: "local"}, Policy: StateInputPolicyRequired, Definition: definition})
	if err != nil {
		t.Fatal(err)
	}
	inaccessibleState := engineStateSnapshot(t, inaccessible.Snapshot.Snapshot())
	response = execute("current", &inaccessibleState)
	if response.Status != "rejected" || !hasEngineDiagnostic(response.Diagnostics, "LDL1904") {
		t.Fatalf("globally inaccessible response = %+v", response)
	}
}

type stateSnapshotFixture struct {
	definition    StateQueryDefinition
	backend       *snapshotBackend
	resolver      *snapshotResolver
	authorization *snapshotAuthorization
	scope         runtimeprotocol.RuntimeScope
}

func newStateSnapshotFixture() *stateSnapshotFixture {
	scope := testScope()
	addresses := struct{ moved, target, relation, entityRow, relationRow, missing, orphan, archived semantic.StableAddress }{
		"ldl:project:p:entity:old", "ldl:project:p:entity:new", "ldl:project:p:relation:r",
		"ldl:project:p:entity:new:row:primary", "ldl:project:p:relation:r:row:primary",
		"ldl:project:p:entity:missing", "ldl:project:p:entity:orphan", "ldl:project:p:entity:archived",
	}
	definition := StateQueryDefinition{ProjectAddress: "ldl:project:p", DefinitionHash: testDigest('c'), GraphHash: testDigest('d')}
	definition.SubjectHashes = []semantic.SubjectHash{
		{Address: addresses.target, Hash: testDigest('1'), Kind: semantic.SubjectKindEntity},
		{Address: addresses.relation, Hash: testDigest('2'), Kind: semantic.SubjectKindRelation},
		{Address: addresses.entityRow, Hash: testDigest('3'), Kind: semantic.SubjectKindEntityRow},
		{Address: addresses.relationRow, Hash: testDigest('4'), Kind: semantic.SubjectKindRelationRow},
		{Address: addresses.missing, Hash: testDigest('5'), Kind: semantic.SubjectKindEntity},
	}
	definition.AddressMoves = []StateAddressMove{{SourceAddress: addresses.moved, TargetAddress: addresses.target}}
	head := port.StateHead{StateVersion: "7", BackendVersion: "state-7", DefinitionHash: testDigest('a'), GraphHash: testDigest('b'), CapturedAt: "2026-07-18T00:00:00Z", SubjectHashes: map[semantic.StableAddress]protocolcommon.Digest{
		addresses.moved: testDigest('1'), addresses.relation: testDigest('9'), addresses.entityRow: testDigest('3'), addresses.relationRow: testDigest('4'), addresses.orphan: testDigest('6'), addresses.archived: testDigest('7'),
	}}
	state := port.StateSnapshot{Head: head, InaccessibleFieldPaths: []semantic.StateFieldPath{}, Records: []port.StateRecord{
		{SubjectAddress: addresses.moved, SubjectKind: semantic.StateSubjectKindEntity, OwnSubjectHash: testDigest('1'), Fields: map[string]semantic.RecipeScalar{string(semantic.StateFieldPathSystemUpdatedAt): stateString("datetime", "2026-07-18T00:00:00Z"), string(semantic.StateFieldPathSystemUpdatedByKind): stateString("enum", "agent"), string(semantic.StateFieldPathSystemUpdatedByID): stateString("string", "agent.local"), "provider.token": stateString("string", "secret")}, ProviderFields: map[string]any{"raw": "secret"}},
		{SubjectAddress: addresses.relation, SubjectKind: semantic.StateSubjectKindRelation, OwnSubjectHash: testDigest('9'), Fields: map[string]semantic.RecipeScalar{string(semantic.StateFieldPathProvenanceSourceKind): stateString("enum", "api"), string(semantic.StateFieldPathProvenanceSourceURI): stateString("string", "https://internal.example")}},
		{SubjectAddress: addresses.entityRow, SubjectKind: semantic.StateSubjectKindEntityRow, OwnSubjectHash: testDigest('3'), Fields: map[string]semantic.RecipeScalar{string(semantic.StateFieldPathProvenanceConfidence): stateNumber("0.9")}},
		{SubjectAddress: addresses.relationRow, SubjectKind: semantic.StateSubjectKindRelationRow, OwnSubjectHash: testDigest('4'), Fields: map[string]semantic.RecipeScalar{string(semantic.StateFieldPathProvenanceVerifiedAt): stateString("datetime", "2026-07-18T00:00:00Z"), string(semantic.StateFieldPathSystemUpdatedByID): stateString("string", "user-1")}},
		{SubjectAddress: addresses.orphan, SubjectKind: semantic.StateSubjectKindEntity, OwnSubjectHash: testDigest('6'), Fields: map[string]semantic.RecipeScalar{string(semantic.StateFieldPathSystemUpdatedAt): stateString("datetime", "2026-07-18T00:00:00Z")}},
		{SubjectAddress: addresses.archived, SubjectKind: semantic.StateSubjectKindEntity, OwnSubjectHash: testDigest('7'), Fields: map[string]semantic.RecipeScalar{}, Tombstoned: true},
	}}
	backend := &snapshotBackend{head: head, state: state}
	authorization := &snapshotAuthorization{decision: port.StateQueryAuthorizationDecision{
		AccessFingerprint: scope.AccessFingerprint, DecisionDigest: testDigest('8'),
		InaccessibleFieldPaths: []semantic.StateFieldPath{semantic.StateFieldPathSystemUpdatedByID},
		RedactedFieldPaths:     map[semantic.StableAddress][]semantic.StateFieldPath{addresses.relation: {semantic.StateFieldPathProvenanceSourceURI}},
	}}
	resolver := &snapshotResolver{backend: backend}
	return &stateSnapshotFixture{definition: definition, backend: backend, resolver: resolver, authorization: authorization, scope: scope}
}

func (f *stateSnapshotFixture) runtime() *Runtime {
	return &Runtime{config: Config{Ports: Ports{StateBindings: f.resolver, StateAccess: f.authorization}}}
}

func (f *stateSnapshotFixture) input(policy StateInputPolicy, binding port.BackendBindingKind) BuildStateQueryInput {
	id := ""
	if binding != port.BackendBindingNone {
		id = "explicit-binding"
	}
	return BuildStateQueryInput{Scope: f.scope, Binding: port.BackendBinding{Kind: binding, BindingID: id}, Policy: policy, Definition: f.definition}
}

type snapshotResolver struct {
	backend port.StateBackend
	err     error
	calls   int
	last    port.BackendBinding
}

func (r *snapshotResolver) ResolveStateBackend(_ context.Context, input port.ResolveStateBackendInput) (port.StateBackend, error) {
	r.calls++
	r.last = input.Binding
	return r.backend, r.err
}

type snapshotAuthorization struct {
	decision port.StateQueryAuthorizationDecision
	err      error
	calls    int
	last     port.StateQueryAuthorizationInput
}

func (a *snapshotAuthorization) EvaluateStateQuery(_ context.Context, input port.StateQueryAuthorizationInput) (port.StateQueryAuthorizationDecision, error) {
	a.calls++
	a.last = input
	return a.decision, a.err
}

type snapshotBackend struct {
	head    port.StateHead
	state   port.StateSnapshot
	headErr error
	readErr error
}

func (b *snapshotBackend) GetHead(context.Context, port.GetStateHeadInput) (port.StateHead, error) {
	return b.head, b.headErr
}
func (b *snapshotBackend) ReadState(context.Context, port.ReadStateInput) (port.StateSnapshot, error) {
	return b.state, b.readErr
}
func (b *snapshotBackend) WriteState(context.Context, port.WriteStateInput) (port.StateWriteResult, error) {
	return port.StateWriteResult{}, errors.New("not implemented")
}
func (b *snapshotBackend) AcquireLease(context.Context, port.AcquireLeaseInput) (port.StateLease, error) {
	return port.StateLease{}, errors.New("not implemented")
}
func (b *snapshotBackend) RenewLease(context.Context, port.RenewLeaseInput) (port.StateLease, error) {
	return port.StateLease{}, errors.New("not implemented")
}
func (b *snapshotBackend) ReleaseLease(context.Context, port.ReleaseLeaseInput) error {
	return errors.New("not implemented")
}
func (b *snapshotBackend) ValidateLease(context.Context, port.ValidateLeaseInput) (port.StateLease, error) {
	return port.StateLease{}, errors.New("not implemented")
}
func (b *snapshotBackend) AppendAuditEvent(context.Context, port.AppendAuditEventInput) (port.AuditEventRef, error) {
	return port.AuditEventRef{}, errors.New("not implemented")
}
func (b *snapshotBackend) ListAuditEvents(context.Context, port.ListAuditEventsInput) (port.AuditEventPage, error) {
	return port.AuditEventPage{}, errors.New("not implemented")
}
func (b *snapshotBackend) ExportSnapshot(context.Context, port.ExportStateSnapshotInput) (port.StateSnapshot, error) {
	return b.state, nil
}

func testScope() runtimeprotocol.RuntimeScope {
	return runtimeprotocol.RuntimeScope{DocumentID: "document_abcdefghijklmnop", LocalScopeID: "local", AccessFingerprint: testDigest('f')}
}

func testDigest(character byte) protocolcommon.Digest {
	return protocolcommon.Digest("sha256:" + strings.Repeat(string(character), 64))
}

func stateString(kind, value string) semantic.RecipeScalar {
	return semantic.RecipeScalar{Kind: kind, StringValue: &value}
}

func stateNumber(value string) semantic.RecipeScalar {
	number := semantic.CanonicalFiniteDecimal(value)
	return semantic.RecipeScalar{Kind: "number", NumberValue: &number}
}

func snapshotSubject(t *testing.T, snapshot semantic.StateQuerySnapshot, address semantic.StableAddress) semantic.StateQuerySubject {
	t.Helper()
	for _, subject := range snapshot.Subjects {
		if subject.SubjectAddress == address {
			return subject
		}
	}
	t.Fatalf("snapshot omitted %s: %+v", address, snapshot.Subjects)
	return semantic.StateQuerySubject{}
}

func hasClassification(record CanonicalStateRecord, classification StateSubjectClassification) bool {
	for _, candidate := range record.Classifications {
		if candidate == classification {
			return true
		}
	}
	return false
}

func hasRecordClassification(records []CanonicalStateRecord, address semantic.StableAddress, classification StateSubjectClassification) bool {
	for _, record := range records {
		if record.SubjectAddress == address && hasClassification(record, classification) {
			return true
		}
	}
	return false
}

func isStateSnapshotError(err error, code StateSnapshotErrorCode) bool {
	var snapshotError *StateSnapshotError
	return errors.As(err, &snapshotError) && snapshotError.Code == code
}

func activeHash(t *testing.T, definition StateQueryDefinition, address semantic.StableAddress) protocolcommon.Digest {
	t.Helper()
	for _, subject := range definition.SubjectHashes {
		if subject.Address == address {
			return subject.Hash
		}
	}
	t.Fatalf("missing active hash for %s", address)
	return ""
}

func engineStateSnapshot(t *testing.T, input semantic.StateQuerySnapshot) engine.StateQuerySnapshot {
	t.Helper()
	result := engine.StateQuerySnapshot{Format: string(input.Format), SchemaVersion: int(input.SchemaVersion), DefinitionProject: string(input.DefinitionProjectAddress), DefinitionHash: string(input.DefinitionHash), GraphHash: string(input.GraphHash), StateVersion: input.StateVersion, CapturedAt: string(input.CapturedAt), InaccessibleFieldPaths: []string{}, Subjects: make([]engine.StateQuerySubject, len(input.Subjects))}
	for _, path := range input.InaccessibleFieldPaths {
		result.InaccessibleFieldPaths = append(result.InaccessibleFieldPaths, string(path))
	}
	for index, subject := range input.Subjects {
		mapped := engine.StateQuerySubject{SubjectAddress: string(subject.SubjectAddress), OwnSubjectHash: string(subject.OwnSubjectHash), Fields: map[string]engine.TypedScalar{}, RedactedFieldPaths: []string{}}
		for path, value := range subject.Fields {
			scalar := engine.TypedScalar{}
			switch value.Kind {
			case "string", "enum", "date", "datetime":
				switch value.Kind {
				case "string":
					scalar.Type = "string"
				case "enum":
					scalar.Type = "enum"
				case "date":
					scalar.Type = "date"
				case "datetime":
					scalar.Type = "datetime"
				}
				if value.StringValue == nil {
					t.Fatalf("missing integration string scalar = %+v", value)
				}
				scalar.String = *value.StringValue
			case "integer":
				scalar.Type = "integer"
				if value.IntegerValue == nil {
					t.Fatalf("missing integration integer scalar = %+v", value)
				}
				parsed, err := strconv.ParseInt(string(*value.IntegerValue), 10, 64)
				if err != nil {
					t.Fatal(err)
				}
				scalar.Int = parsed
			case "number":
				scalar.Type = "number"
				if value.NumberValue == nil {
					t.Fatalf("missing integration number scalar = %+v", value)
				}
				parsed, err := strconv.ParseFloat(string(*value.NumberValue), 64)
				if err != nil {
					t.Fatal(err)
				}
				scalar.Float = parsed
			case "boolean":
				scalar.Type = "boolean"
				if value.BooleanValue == nil {
					t.Fatalf("missing integration boolean scalar = %+v", value)
				}
				scalar.Bool = *value.BooleanValue
			default:
				t.Fatalf("unexpected integration scalar = %+v", value)
			}
			mapped.Fields[path] = scalar
		}
		for _, path := range subject.RedactedFieldPaths {
			mapped.RedactedFieldPaths = append(mapped.RedactedFieldPaths, string(path))
		}
		result.Subjects[index] = mapped
	}
	return result
}

func hasEngineDiagnostic(values []engine.Diagnostic, code string) bool {
	for _, value := range values {
		if value.Code == code {
			return true
		}
	}
	return false
}

var _ port.StateBackend = (*snapshotBackend)(nil)
var _ port.StateBackendBindingResolver = (*snapshotResolver)(nil)
var _ port.StateQueryAuthorization = (*snapshotAuthorization)(nil)
