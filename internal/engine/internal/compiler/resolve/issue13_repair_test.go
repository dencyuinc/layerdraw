// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import "testing"

func TestResolveRootCanonicalIDUsesSelectedPackMetadata(t *testing.T) {
	t.Parallel()

	project := Resolve(baseInput())
	if project.HasErrors || project.RootCanonicalID != "" {
		t.Fatalf("project root canonical ID = %q, diagnostics = %+v", project.RootCanonicalID, project.Diagnostics)
	}

	pack := baseInput().Packs.Installs["aws"]
	got := Resolve(Input{
		Mode:       CompilePack,
		RootPackID: pack.CanonicalID,
		EntryPath:  pack.Entry,
		Packs: ResolvedDependencies{
			Format:        "layerdraw-resolved",
			FormatVersion: 1,
			Language:      1,
			Installs:      map[string]ResolvedPack{"install_alias": pack},
		},
	})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	if got.RootAddress != "ldl:pack:layerdraw:aws-complete" || got.RootCanonicalID != "layerdraw/aws-complete" {
		t.Fatalf("pack root = address %q canonical ID %q", got.RootAddress, got.RootCanonicalID)
	}
}

func TestDuplicateOwnerScopedDiagnosticCarriesCurrentAndPreviousAddresses(t *testing.T) {
	t.Parallel()

	got := Resolve(Input{Mode: CompileProject, EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`project p "P" {}
entity_type e "E" {
  representation shape rect
  columns {
    c "C" string
    c "C2" string
  }
}
`),
	}}})
	var duplicate *Diagnostic
	for i := range got.Diagnostics {
		if got.Diagnostics[i].Code == "LDL1302" && got.Diagnostics[i].Message == "duplicate declaration identity" {
			duplicate = &got.Diagnostics[i]
		}
	}
	if duplicate == nil {
		t.Fatalf("duplicate diagnostic missing from %+v", got.Diagnostics)
	}
	wantSubject := "ldl:project:p:entity-type:e:column:c"
	wantOwner := "ldl:project:p:entity-type:e"
	if duplicate.SubjectAddress != wantSubject || duplicate.OwnerAddress != wantOwner || len(duplicate.Related) != 1 {
		t.Fatalf("duplicate diagnostic = %+v", *duplicate)
	}
	related := duplicate.Related[0]
	if related.Relation != "previous" || related.SubjectAddress != wantSubject || related.OwnerAddress != wantOwner || related.Range == nil || related.Range.ModulePath != "document.ldl" {
		t.Fatalf("related previous = %+v", related)
	}
}

func TestCompareStableSymbolsPlacesProjectBeforePack(t *testing.T) {
	t.Parallel()

	project := StableSymbol{Origin: Origin{Kind: OriginProject, ProjectID: "z"}, Path: []SymbolSegment{{Kind: KindEntityType, ID: "local"}}}
	pack := StableSymbol{Origin: Origin{Kind: OriginPack, Publisher: "a", PackName: "a"}, Path: []SymbolSegment{{Kind: KindEntityType, ID: "external"}}}
	if CompareStableSymbols(project, pack) >= 0 || CompareStableSymbols(pack, project) <= 0 {
		t.Fatal("structured symbol comparison did not rank project before pack")
	}
}
