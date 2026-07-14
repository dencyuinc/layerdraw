// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package materialize

import (
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
)

func TestGeneratedSubjectKindsUseNormativeOwnerQualifiedVocabulary(t *testing.T) {
	snapshot := Compile(projectStages(t, projectFixture)).Snapshot()
	want := map[string]SubjectKind{
		"ldl:project:p":                                           SubjectProject,
		"ldl:project:p:entity-type:service":                       SubjectEntityType,
		"ldl:project:p:entity-type:service:column:environment":    SubjectEntityTypeColumn,
		"ldl:project:p:entity-type:service:column:note":           SubjectEntityTypeColumn,
		"ldl:project:p:entity-type:service:constraint:env_unique": SubjectEntityTypeConstraint,
		"ldl:project:p:relation-type:link":                        SubjectRelationType,
		"ldl:project:p:relation-type:link:column:weight":          SubjectRelationTypeColumn,
		"ldl:project:p:layer:app":                                 SubjectLayer,
		"ldl:project:p:entity:alpha":                              SubjectEntity,
		"ldl:project:p:entity:alpha:row:primary":                  SubjectEntityRow,
		"ldl:project:p:entity:beta":                               SubjectEntity,
		"ldl:project:p:relation:alpha_beta":                       SubjectRelation,
		"ldl:project:p:relation:alpha_beta:row:primary":           SubjectRelationRow,
		"ldl:project:p:query:scope":                               SubjectQuery,
		"ldl:project:p:query:scope:parameter:environment":         SubjectQueryParameter,
		"ldl:project:p:view:inventory":                            SubjectView,
		"ldl:project:p:view:inventory:table-column:environment":   SubjectViewTableColumn,
		"ldl:project:p:view:inventory:export:data":                SubjectViewExport,
		"ldl:project:p:reference:guide":                           SubjectReference,
	}
	if len(snapshot.Hashes.OwnSubjects) != len(want) {
		t.Fatalf("own subjects=%d, want %d", len(snapshot.Hashes.OwnSubjects), len(want))
	}
	for _, subject := range snapshot.Hashes.OwnSubjects {
		if subject.Kind != want[subject.Address] {
			t.Fatalf("kind for %s=%q, want %q", subject.Address, subject.Kind, want[subject.Address])
		}
		if strings.Contains(string(subject.Kind), "-") || subject.Kind == "column" || subject.Kind == "row" || subject.Kind == "constraint" {
			t.Fatalf("compiler-only kind leaked: %+v", subject)
		}
	}
	if childSetHash(snapshot.Hashes, "ldl:project:p:entity-type:service", SubjectEntityTypeColumn) == "" ||
		childSetHash(snapshot.Hashes, "ldl:project:p:relation-type:link", SubjectRelationTypeColumn) == "" ||
		childSetHash(snapshot.Hashes, "ldl:project:p:entity:alpha", SubjectEntityRow) == "" ||
		childSetHash(snapshot.Hashes, "ldl:project:p:relation:alpha_beta", SubjectRelationRow) == "" {
		t.Fatal("owner-qualified child-set hashes are incomplete")
	}
}

func TestGeneratedSubjectKindConversionQualifiesIdentityChildren(t *testing.T) {
	entityTypeOwner := "ldl:project:p:entity-type:service"
	relationTypeOwner := "ldl:project:p:relation-type:link"
	entityOwner := "ldl:project:p:entity:alpha"
	relationOwner := "ldl:project:p:relation:alpha_beta"
	queryOwner := "ldl:project:p:query:q"
	viewOwner := "ldl:project:p:view:v"
	n := normalizer{kinds: map[string]resolve.SubjectKind{
		entityTypeOwner: resolve.KindEntityType, relationTypeOwner: resolve.KindRelationType,
		entityOwner: resolve.KindEntity, relationOwner: resolve.KindRelation,
		queryOwner: resolve.KindQuery, viewOwner: resolve.KindView,
	}}
	history := definition.IdentityHistory{
		RootReservations: map[string]map[resolve.SubjectKind][]string{"ldl:project:p": {resolve.KindEntityType: {"service"}}},
		Moves: []definition.Move{
			{Kind: resolve.KindColumn, OwnerAddress: &entityTypeOwner, OldAddress: "a", NewAddress: "b"},
			{Kind: resolve.KindConstraint, OwnerAddress: &relationTypeOwner, OldAddress: "c", NewAddress: "d"},
			{Kind: resolve.KindRow, OwnerAddress: &entityOwner, OldAddress: "e", NewAddress: "f"},
			{Kind: resolve.KindRow, OwnerAddress: &relationOwner, OldAddress: "g", NewAddress: "h"},
			{Kind: resolve.KindParameter, OwnerAddress: &queryOwner, OldAddress: "i", NewAddress: "j"},
			{Kind: resolve.KindTableColumn, OwnerAddress: &viewOwner, OldAddress: "k", NewAddress: "l"},
			{Kind: resolve.KindExport, OwnerAddress: &viewOwner, OldAddress: "m", NewAddress: "n"},
		},
	}
	generated, err := n.identity(history)
	if err != nil {
		t.Fatal(err)
	}
	want := []SubjectKind{SubjectEntityTypeColumn, SubjectRelationTypeConstraint, SubjectEntityRow, SubjectRelationRow, SubjectQueryParameter, SubjectViewTableColumn, SubjectViewExport}
	for index, kind := range want {
		if generated.Moves[index].Kind != kind {
			t.Fatalf("move %d kind=%q, want %q", index, generated.Moves[index].Kind, kind)
		}
	}
	if _, exists := generated.RootReservations["ldl:project:p"][SubjectEntityType]; !exists {
		t.Fatalf("root reservation vocabulary=%+v", generated.RootReservations)
	}
	if _, ok := GeneratedSubjectKind(resolve.KindColumn, resolve.KindView); ok {
		t.Fatal("invalid generic child/owner combination accepted")
	}
	if kind, ok := GeneratedSubjectKind(resolve.KindConstraint, resolve.KindEntityType); !ok || kind != SubjectEntityTypeConstraint {
		t.Fatalf("entity constraint conversion=%q/%v", kind, ok)
	}
	if kind, ok := GeneratedSubjectKind(resolve.KindColumn, resolve.KindRelationType); !ok || kind != SubjectRelationTypeColumn {
		t.Fatalf("relation column conversion=%q/%v", kind, ok)
	}
	badRoot := definition.IdentityHistory{RootReservations: map[string]map[resolve.SubjectKind][]string{"root": {resolve.KindRow: {"bad"}}}}
	if _, err := n.identity(badRoot); err == nil {
		t.Fatal("generic root reservation kind accepted")
	}
	badMove := definition.IdentityHistory{Moves: []definition.Move{{Kind: resolve.KindRow, OldAddress: "bad"}}}
	if _, err := n.identity(badMove); err == nil {
		t.Fatal("unqualified move kind accepted")
	}
	badClosure := definition.IdentityHistory{MoveClosure: []definition.MoveResolution{{Kind: resolve.KindColumn, SourceAddress: "bad"}}}
	if _, err := n.identity(badClosure); err == nil {
		t.Fatal("unqualified move-resolution kind accepted")
	}
}

func TestProjectDefinitionHashPayloadRequiresEmptyGraphCollections(t *testing.T) {
	input := projectStages(t, `project empty "Empty" {}`)
	document := Compile(input).Snapshot().Document
	payload, _, _, err := buildHashPayloads(input, document, nil)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := Canonicalize(payload)
	if err != nil {
		t.Fatal(err)
	}
	text := string(canonical)
	for _, field := range []string{`"layers":[]`, `"entities":[]`, `"relations":[]`} {
		if !strings.Contains(text, field) {
			t.Fatalf("required empty collection %s absent from %s", field, text)
		}
	}
}

func TestPredicateCanonicalJSONRetainsRequiredEmptyChildren(t *testing.T) {
	input := projectStages(t, projectFixture)
	canonical := string(Compile(input).Snapshot().CanonicalJSON)
	if strings.Count(canonical, `"children":[],"kind":"all"`) < 2 {
		t.Fatalf("default Query predicates lack required empty children arrays: %s", canonical)
	}

	row, err := Canonicalize(RowPredicate{Kind: query.PredicateAll})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(row), `{"children":[],"kind":"all"}`; got != want {
		t.Fatalf("empty RowPredicate=%s, want %s", got, want)
	}
	field, err := Canonicalize(Predicate{Kind: query.PredicateField, Field: "id", Operator: query.OperatorExists})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(field), `"children"`) {
		t.Fatalf("non-aggregate Predicate emitted children: %s", field)
	}
}

func TestClosedPackManifestBytesMustMatchTypedBasics(t *testing.T) {
	input := packStages(t, packFixture)
	if result := Compile(input); result.HasErrors {
		t.Fatalf("valid raw manifest rejected: %+v", result.Diagnostics)
	}
	input.Resolved.SelectedClosure[0].Manifest.Bytes = []byte(`{"format":"layerdraw-pack","format_version":1,"id":"pub/other","name":"core","version":"1.0.0","language":1,"entry":"pack.ldl"}`)
	if result := Compile(input); !result.HasErrors || result.Snapshot().Pack != nil {
		t.Fatalf("typed/raw manifest mismatch published: %+v", result.Snapshot())
	}
	for _, raw := range [][]byte{nil, []byte(`{"format":`)} {
		if _, err := decodeManifestBasics(raw); err == nil {
			t.Fatalf("invalid manifest JSON accepted: %q", raw)
		}
	}
}

func TestStableAddressFallbackUsesStructuredOriginOrder(t *testing.T) {
	project := "ldl:project:z:reference:item"
	pack := "ldl:pack:a:a:reference:item"
	if !lessAddress(resolve.Result{}, project, pack) || lessAddress(resolve.Result{}, pack, project) {
		t.Fatal("generated addresses did not use Project-before-Pack StableSymbol order")
	}
	for _, address := range []string{"short", "ldl:unknown:x", "ldl:pack:only", "ldl:project:p:entity", "ldl:project:p:unknown:id", "ldl:project:p:entity:"} {
		if _, ok := stableSymbolFromAddress(address); ok {
			t.Fatalf("invalid StableAddress accepted: %q", address)
		}
	}
	for _, address := range []string{"ldl:project:p", "ldl:pack:pub:name", "ldl:project:p:entity-type:t:column:c", "ldl:pack:pub:name:view:v:export:e"} {
		if _, ok := stableSymbolFromAddress(address); !ok {
			t.Fatalf("valid StableAddress rejected: %q", address)
		}
	}
}
