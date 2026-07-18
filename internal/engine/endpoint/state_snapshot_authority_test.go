// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

func TestStateSnapshotAuthorityCanonicalizesAndHashesProtocolValue(t *testing.T) {
	t.Parallel()
	snapshot := authorityStateQuerySnapshot()

	canonical, hash, err := CanonicalizeStateQuerySnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	wantCanonical, err := semantic.EncodeStateQuerySnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := stateQuerySnapshotFromProtocol(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	_, wantHash, err := engine.CanonicalizeStateQuerySnapshot(mapped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, wantCanonical) || hash != protocolcommon.Digest(wantHash) {
		t.Fatalf("authority canonical/hash mismatch: hash=%s want=%s", hash, wantHash)
	}
}

func TestStateSnapshotAuthorityRejectsProtocolMappingAndEngineFailures(t *testing.T) {
	t.Parallel()
	malformedProtocol := authorityStateQuerySnapshot()
	malformedProtocol.Format = "not-a-state-snapshot"
	if _, _, err := CanonicalizeStateQuerySnapshot(malformedProtocol); err == nil || !strings.Contains(err.Error(), "encode StateQuerySnapshot") {
		t.Fatalf("malformed protocol error=%v", err)
	}

	huge := semantic.CanonicalFiniteDecimal("1e+999")
	malformedMapping := authorityStateQuerySnapshot()
	malformedMapping.Subjects[0].Fields[string(semantic.StateFieldPathProvenanceConfidence)] = semantic.RecipeScalar{Kind: "number", NumberValue: &huge}
	if _, err := mapExecuteQueryInput(engineprotocol.ExecuteQueryInput{
		Arguments: map[string]semantic.RecipeScalar{},
		DocumentGeneration: engineprotocol.DocumentGeneration{
			DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "endpoint", Value: "document_abcdefghijklmnop"},
			Value:          "1",
		},
		Limits:        engineprotocol.WorkbenchLimits{MaxItems: "10", MaxOutputBytes: "1024"},
		StateSnapshot: &malformedMapping,
	}); err == nil {
		t.Fatal("unrepresentable protocol scalar reached Engine mapping")
	}

	wrongType := authorityStateQuerySnapshot()
	value := "2026-07-18T00:00:00Z"
	wrongType.Subjects[0].Fields[string(semantic.StateFieldPathSystemUpdatedByID)] = semantic.RecipeScalar{Kind: "datetime", StringValue: &value}
	if _, _, err := CanonicalizeStateQuerySnapshot(wrongType); err == nil || !strings.Contains(err.Error(), "invalid typed value") {
		t.Fatalf("Engine field-schema error=%v", err)
	}
}

func TestStateSnapshotAuthorityProjectsClosedRegistryDefensively(t *testing.T) {
	t.Parallel()
	want := []semantic.StateFieldPath{
		semantic.StateFieldPathSystemCreatedAt,
		semantic.StateFieldPathSystemUpdatedAt,
		semantic.StateFieldPathSystemCreatedByKind,
		semantic.StateFieldPathSystemCreatedByID,
		semantic.StateFieldPathSystemCreatedByDisplayName,
		semantic.StateFieldPathSystemUpdatedByKind,
		semantic.StateFieldPathSystemUpdatedByID,
		semantic.StateFieldPathSystemUpdatedByDisplayName,
		semantic.StateFieldPathSystemCreatedRevision,
		semantic.StateFieldPathSystemUpdatedRevision,
		semantic.StateFieldPathProvenanceSourceKind,
		semantic.StateFieldPathProvenanceSourceLabel,
		semantic.StateFieldPathProvenanceSourceURI,
		semantic.StateFieldPathProvenanceSourceExternalID,
		semantic.StateFieldPathProvenanceObservedAt,
		semantic.StateFieldPathProvenanceVerifiedAt,
		semantic.StateFieldPathProvenanceStaleAfter,
		semantic.StateFieldPathProvenanceVerifiedByKind,
		semantic.StateFieldPathProvenanceVerifiedByID,
		semantic.StateFieldPathProvenanceVerifiedByDisplayName,
		semantic.StateFieldPathProvenanceConfidence,
	}
	registry := StateFieldRegistry()
	if !reflect.DeepEqual(registry, want) {
		t.Fatalf("registry=%v want=%v", registry, want)
	}
	registry[0] = "mutated"
	if StateFieldRegistry()[0] != semantic.StateFieldPathSystemCreatedAt {
		t.Fatal("registry projection shares mutable storage")
	}
}

func TestStateSnapshotAuthorityUsesEngineStableAddressOrder(t *testing.T) {
	t.Parallel()
	ordered := []semantic.StableAddress{
		"ldl:project:p:entity:z",
		"ldl:project:p:relation:a",
		"ldl:project:p:entity:z:row:a",
		"ldl:pack:pub:name:query:a",
	}
	for index := 1; index < len(ordered); index++ {
		if CompareStableAddresses(ordered[index-1], ordered[index]) >= 0 || CompareStableAddresses(ordered[index], ordered[index-1]) <= 0 {
			t.Fatalf("address order disagrees at %q and %q", ordered[index-1], ordered[index])
		}
	}
	if CompareStableAddresses(ordered[0], ordered[0]) != 0 {
		t.Fatal("identical addresses did not compare equal")
	}
}

func authorityStateQuerySnapshot() semantic.StateQuerySnapshot {
	updatedAt := "2026-07-18T00:00:00Z"
	return semantic.StateQuerySnapshot{
		Format:                   semantic.StateQuerySnapshotFormatValue,
		SchemaVersion:            1,
		DefinitionProjectAddress: "ldl:project:p",
		DefinitionHash:           protocolcommon.Digest("sha256:" + strings.Repeat("a", 64)),
		GraphHash:                protocolcommon.Digest("sha256:" + strings.Repeat("b", 64)),
		StateVersion:             "1",
		CapturedAt:               "2026-07-18T00:00:00Z",
		InaccessibleFieldPaths:   []semantic.StateFieldPath{},
		Subjects: []semantic.StateQuerySubject{{
			SubjectAddress:     "ldl:project:p:entity:e",
			OwnSubjectHash:     protocolcommon.Digest("sha256:" + strings.Repeat("c", 64)),
			Fields:             map[string]semantic.RecipeScalar{string(semantic.StateFieldPathSystemUpdatedAt): {Kind: "datetime", StringValue: &updatedAt}},
			RedactedFieldPaths: []semantic.StateFieldPath{},
		}},
	}
}
