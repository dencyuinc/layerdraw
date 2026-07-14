// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package index

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
)

func TestGeneratedIndexesUseNormativeKindsAndMembershipIndexes(t *testing.T) {
	snapshot := Build(indexProject(t, indexFixture)).Snapshot()
	for _, subject := range snapshot.SourceMap.Subjects {
		if strings.Contains(string(subject.Kind), "-") || subject.Kind == "column" || subject.Kind == "row" || subject.Kind == "constraint" {
			t.Fatalf("SourceMap leaked compiler kind: %+v", subject)
		}
	}
	for _, subject := range snapshot.SemanticIndex.Subjects {
		if strings.Contains(string(subject.Kind), "-") || subject.Kind == "column" || subject.Kind == "row" || subject.Kind == "constraint" {
			t.Fatalf("SemanticIndex leaked compiler kind: %+v", subject)
		}
	}
	for _, document := range snapshot.SearchDocuments {
		if strings.Contains(string(document.SubjectKind), "-") || document.SubjectKind == "row" {
			t.Fatalf("SearchDocument leaked compiler kind: %+v", document)
		}
	}

	wantTypes := []OwnerMembers{
		{OwnerAddress: "ldl:project:p:entity-type:service", Addresses: []string{"ldl:project:p:entity:alpha", "ldl:project:p:entity:beta"}},
		{OwnerAddress: "ldl:project:p:relation-type:link", Addresses: []string{"ldl:project:p:relation:alpha_beta"}},
	}
	wantLayers := []OwnerMembers{{OwnerAddress: "ldl:project:p:layer:app", Addresses: []string{"ldl:project:p:entity:alpha", "ldl:project:p:entity:beta"}}}
	wantReferences := []ReferenceIDRecord{{ID: "guide", Addresses: []string{"ldl:project:p:reference:guide"}}}
	semantic := snapshot.SemanticIndex
	if !reflect.DeepEqual(semantic.TypeMembership, wantTypes) || !reflect.DeepEqual(semantic.LayerMembership, wantLayers) || !reflect.DeepEqual(semantic.ReferenceIDs, wantReferences) {
		t.Fatalf("membership indexes\ntypes=%+v\nlayers=%+v\nreferences=%+v", semantic.TypeMembership, semantic.LayerMembership, semantic.ReferenceIDs)
	}
	if !reflect.DeepEqual(semantic.ScopedReads.MembersByType, wantTypes) || !reflect.DeepEqual(semantic.ScopedReads.MembersByLayer, wantLayers) || !reflect.DeepEqual(semantic.ScopedReads.ReferencesByID, wantReferences) {
		t.Fatalf("scoped membership indexes=%+v", semantic.ScopedReads)
	}
}

func TestRelationAndRelationRowSearchIncludeDirectionalLabels(t *testing.T) {
	documents := Build(indexProject(t, indexFixture)).Snapshot().SearchDocuments
	for _, address := range []string{"ldl:project:p:relation:alpha_beta", "ldl:project:p:relation:alpha_beta:row:primary"} {
		document := searchDocument(t, documents, address)
		fields := map[string]string{}
		for _, field := range document.Fields {
			fields[field.FieldPath] = field.Text
		}
		if fields[SearchFieldForwardLabel] != "links" || fields[SearchFieldReverseLabel] != "linked by" {
			t.Fatalf("directional labels for %s=%+v", address, fields)
		}
	}
}

func TestRelationSearchPreservesFromToOrder(t *testing.T) {
	reversed := strings.Replace(indexFixture, "alpha_beta: alpha -> beta", "alpha_beta: beta -> alpha", 1)
	documents := Build(indexProject(t, reversed)).Snapshot().SearchDocuments
	want := []string{"ldl:project:p:entity:beta", "ldl:project:p:entity:alpha"}
	for _, address := range []string{"ldl:project:p:relation:alpha_beta", "ldl:project:p:relation:alpha_beta:row:primary"} {
		if got := searchDocument(t, documents, address).GraphEntryAddresses; !reflect.DeepEqual(got, want) {
			t.Fatalf("from-to order for %s=%v, want %v", address, got, want)
		}
	}
	forward := searchDocument(t, Build(indexProject(t, indexFixture)).Snapshot().SearchDocuments, "ldl:project:p:relation:alpha_beta")
	backward := searchDocument(t, documents, "ldl:project:p:relation:alpha_beta")
	if forward.ContentHash == backward.ContentHash {
		t.Fatal("reversing Relation endpoints did not change search content hash")
	}
}

func TestMixedGeneratedCollectionsUseStructuredStableSymbolOrder(t *testing.T) {
	projectRoot := "ldl:project:z"
	packRoot := "ldl:pack:a:a"
	owners := map[string]string{
		projectRoot + ":reference:z": projectRoot,
		packRoot + ":reference:a":    packRoot,
	}
	kinds := map[string]materialize.SubjectKind{
		projectRoot + ":reference:z": materialize.SubjectReference,
		packRoot + ":reference:a":    materialize.SubjectReference,
	}
	members := ownerMembers(owners, kinds, nil, resolve.Result{})
	if len(members) != 2 || members[0].OwnerAddress != projectRoot || members[1].OwnerAddress != packRoot {
		t.Fatalf("owner membership order=%+v", members)
	}

	description := "Searchable description"
	document := &materialize.NormalizedDocument{
		EntityTypes: []materialize.EntityType{
			{Common: materialize.Common{Description: &description}, ID: "pack", Address: packRoot + ":entity-type:pack", DisplayName: "Pack"},
			{ID: "project", Address: projectRoot + ":entity-type:project", DisplayName: "Project"},
		},
		References: []materialize.Reference{
			{ID: "same", Address: packRoot + ":reference:same", Text: "Pack"},
			{ID: "same", Address: projectRoot + ":reference:same", Text: "Project"},
		},
	}
	documents, err := buildSearchDocuments(materialize.Snapshot{Document: document}, SourceMapV1{}, resolve.Result{})
	if err != nil {
		t.Fatal(err)
	}
	if len(documents) != 4 || documents[0].SubjectAddress != projectRoot+":entity-type:project" || documents[2].SubjectAddress != packRoot+":entity-type:pack" {
		t.Fatalf("SearchDocument StableSymbol order=%+v", searchAddresses(documents))
	}
	ids := referenceIDs(materialize.Snapshot{Document: document}, resolve.Result{})
	if len(ids) != 1 || !reflect.DeepEqual(ids[0].Addresses, []string{projectRoot + ":reference:same", packRoot + ":reference:same"}) {
		t.Fatalf("Reference-ID StableSymbol order=%+v", ids)
	}
}

func TestPackRootUsesExactManifestProvenance(t *testing.T) {
	input := indexPack(t, packIndexFixture)
	stages := validatedStages(t, input)
	manifest := stages.Resolved.SelectedClosure[0].Manifest
	snapshot := Build(input).Snapshot()
	if len(snapshot.SourceMap.Files) != 2 {
		t.Fatalf("Pack SourceMap files=%+v", snapshot.SourceMap.Files)
	}
	var manifestFile *SourceFileRecord
	for index := range snapshot.SourceMap.Files {
		if snapshot.SourceMap.Files[index].ModulePath == "manifest.json" {
			manifestFile = &snapshot.SourceMap.Files[index]
		}
	}
	if manifestFile == nil || manifestFile.Digest != sourceDigest(manifest.Bytes) || manifestFile.ByteLength != len(manifest.Bytes) {
		t.Fatalf("manifest file provenance=%+v", manifestFile)
	}
	root := sourceSubject(t, snapshot.SourceMap, stages.Resolve.RootAddress)
	if !root.ManifestRoot || root.Module == nil || root.Module.ModulePath != "manifest.json" || root.DeclarationRange == nil || root.DeclarationRange.StartByte != 0 || root.DeclarationRange.EndByte != len(manifest.Bytes) {
		t.Fatalf("Pack root manifest binding=%+v", root)
	}
}

func searchAddresses(values []SearchDocument) []string {
	out := make([]string, len(values))
	for index := range values {
		out[index] = values[index].SubjectAddress
	}
	return out
}
