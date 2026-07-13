// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"reflect"
	"strings"
	"testing"
)

func TestAcceptanceReservationSchemaIsResolverOwned(t *testing.T) {
	t.Parallel()
	source := `project p "P" {}
reserved {
  entity_types [valid_type, "quoted", 1, ns.qualified]
  entity_types [ignored_duplicate_category]
  unknown_category [ghost]
  layers not_a_list
  relations [one] [two]
  views {
    exports [nested]
  }
}
`
	got := Resolve(Input{Mode: CompileProject, EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(source)}}})
	var schema []Diagnostic
	for _, diagnostic := range got.Diagnostics {
		if diagnostic.Code == "LDL1102" {
			schema = append(schema, diagnostic)
		}
	}
	if len(schema) != 8 {
		t.Fatalf("schema diagnostics = %+v, want 8", schema)
	}
	for _, diagnostic := range schema {
		if diagnostic.MessageKey != "unknown_or_duplicate_schema_member" || diagnostic.Range == nil || diagnostic.Range.EndByte <= diagnostic.Range.StartByte {
			t.Fatalf("invalid schema diagnostic = %+v", diagnostic)
		}
	}
	duplicate := schemaDiagnostic(schema, "duplicate reservation category")
	if duplicate == nil || len(duplicate.Related) != 1 || duplicate.Related[0].Relation != "previous" || duplicate.Related[0].Range == nil {
		t.Fatalf("duplicate category context = %+v", duplicate)
	}
	if len(got.Identity.Reservations) != 1 || got.Identity.Reservations[0].ID != "valid_type" {
		t.Fatalf("valid reservation members = %+v", got.Identity.Reservations)
	}
}

func TestAcceptanceKnownReservationCategoryInWrongScopeKeepsIdentityDiagnostic(t *testing.T) {
	t.Parallel()
	project := Resolve(Input{Mode: CompileProject, EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`project p "P" {}
reserved {
  columns [orphan_column]
}
`),
	}}})
	if diagnosticCodeCount(project.Diagnostics, "LDL1102") != 0 || diagnosticCodeCount(project.Diagnostics, "LDL1302") != 1 {
		t.Fatalf("project wrong-scope diagnostics = %+v", project.Diagnostics)
	}
	if len(project.CandidateIdentity.Reservations) != 0 {
		t.Fatalf("wrong-scope project reservation leaked identity: %+v", project.CandidateIdentity.Reservations)
	}

	resolvedPack := baseInput().Packs.Installs["aws"]
	resolvedPack.Files = map[string]string{"pack.ldl": testDigest("a")}
	resolvedPack.SourceFiles = map[string]SourceFile{"pack.ldl": parse(`reserved {
  layers [project_only_layer]
}
`)}
	pack := Resolve(Input{Mode: CompilePack, RootPackID: resolvedPack.CanonicalID, EntryPath: resolvedPack.Entry, Packs: ResolvedDependencies{
		Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: map[string]ResolvedPack{"aws": resolvedPack},
	}})
	if diagnosticCodeCount(pack.Diagnostics, "LDL1102") != 0 || diagnosticCodeCount(pack.Diagnostics, "LDL1302") != 1 {
		t.Fatalf("pack wrong-scope diagnostics = %+v", pack.Diagnostics)
	}
	if len(pack.CandidateIdentity.Reservations) != 0 {
		t.Fatalf("wrong-scope pack reservation leaked identity: %+v", pack.CandidateIdentity.Reservations)
	}
}

func TestAcceptanceReservationMalformedMemberAndReserveRowsDoNotLeakIdentity(t *testing.T) {
	t.Parallel()
	malformed := `project p "P" {}
reserved {
  entity_types [must_not_exist missing_comma]
}
`
	got := Resolve(Input{Mode: CompileProject, EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(malformed)}}})
	if schemaDiagnostic(got.Diagnostics, "invalid reservation identifier") == nil && schemaDiagnostic(got.Diagnostics, "malformed reservation identifier") == nil {
		t.Fatalf("malformed reservation diagnostic missing: %+v", got.Diagnostics)
	}
	if len(got.CandidateIdentity.Reservations) != 0 {
		t.Fatalf("malformed member leaked identity: %+v", got.CandidateIdentity.Reservations)
	}

	rows := `project p "P" {}
entity_type e "E" {}
layers {
  app "App" @0
}
entities e @app {
  item "Item" {
    reserve_rows [valid_row, "quoted", 1, ns.qualified]
  }
}
export { item }
`
	rowResult := Resolve(Input{Mode: CompileProject, EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(rows)}}})
	if len(rowResult.Diagnostics) != 3 {
		t.Fatalf("reserve_rows diagnostics = %+v, want 3", rowResult.Diagnostics)
	}
	if len(rowResult.Identity.Reservations) != 1 || rowResult.Identity.Reservations[0].ID != "valid_row" || rowResult.Identity.Reservations[0].Kind != KindRow {
		t.Fatalf("reserve_rows identities = %+v", rowResult.Identity.Reservations)
	}
}

func TestAcceptanceMalformedImportedReservationIsDiagnosed(t *testing.T) {
	t.Parallel()
	files := map[string]SourceFile{
		"document.ldl": parse(`import { imported } from "./types.ldl"
project p "P" {}
export { imported }
`),
		"types.ldl": parse(`entity_type imported "Imported" {
  reserve {
    columns [valid missing_comma]
  }
}
`),
	}
	got := Resolve(Input{Mode: CompileProject, EntryPath: "document.ldl", Project: ProjectInput{Files: files}})
	diagnostic := schemaDiagnostic(got.Diagnostics, "invalid reservation identifier")
	if diagnostic == nil {
		diagnostic = schemaDiagnostic(got.Diagnostics, "malformed reservation identifier")
	}
	if diagnostic == nil || diagnostic.Range == nil || diagnostic.Range.ModulePath != "types.ldl" {
		t.Fatalf("imported reservation diagnostic = %+v", got.Diagnostics)
	}
	if len(got.CandidateIdentity.Reservations) != 0 {
		t.Fatalf("malformed imported member leaked identity: %+v", got.CandidateIdentity.Reservations)
	}
}

func TestAcceptanceIdentityHistoryUsesStructuredStableSymbolOrder(t *testing.T) {
	t.Parallel()
	in := baseInput()
	in.Project.Files["document.ldl"] = parse(`import { vpc } from "aws.network"
project order_platform "Order Platform" {}
entity_type local_entity_type "Entity Type" {
  columns {
    current_column "Column" string
  }
}
relation_type local_relation_type "Relation Type" dependency {}
layers {
  local_layer "Layer" @0
}
reserved {
  layers [old_layer]
  relation_types [old_relation_type]
  entity_types [old_entity_type]
}
moves {
  layer older_layer -> local_layer
  relation_type older_relation_type -> local_relation_type
  entity_type older_entity_type -> local_entity_type
  entity_type_column local_entity_type older_column -> current_column
}
export { vpc }
`)
	pack := in.Packs.Installs["aws"]
	pack.SourceFiles["pack.ldl"] = parse(`reserved {
  relation_types [pack_old_relation_type]
  entity_types [pack_old_entity_type]
}
moves {
  relation_type pack_older_relation_type -> pack_relation_type
  entity_type pack_older_entity_type -> pack_entity_type
}
entity_type pack_entity_type "Pack Entity" {}
relation_type pack_relation_type "Pack Relation" dependency {}
export * from "./modules/network.ldl"
export { pack_entity_type, pack_relation_type }
`)
	in.Packs.Installs["aws"] = pack
	got := Resolve(in)
	if got.HasErrors {
		t.Fatalf("diagnostics = %+v", got.Diagnostics)
	}
	wantReservationKinds := []SubjectKind{KindEntityType, KindRelationType, KindLayer}
	var reservationKinds []SubjectKind
	for _, item := range got.CandidateIdentity.Reservations {
		reservationKinds = append(reservationKinds, item.Kind)
	}
	if !reflect.DeepEqual(reservationKinds, wantReservationKinds) {
		t.Fatalf("reservation kind order = %+v, want %+v (%+v)", reservationKinds, wantReservationKinds, got.CandidateIdentity.Reservations)
	}
	wantMoveKinds := []SubjectKind{KindEntityType, KindRelationType, KindLayer, KindColumn}
	var moveKinds []SubjectKind
	for _, item := range got.CandidateIdentity.Moves {
		moveKinds = append(moveKinds, item.Kind)
	}
	if !reflect.DeepEqual(moveKinds, wantMoveKinds) {
		t.Fatalf("move kind order = %+v, want %+v (%+v)", moveKinds, wantMoveKinds, got.CandidateIdentity.Moves)
	}
	var closureKinds []SubjectKind
	for _, item := range got.CandidateIdentity.MoveClosure {
		if len(item.fromSymbol.Path) == 0 {
			continue
		}
		closureKinds = append(closureKinds, item.fromSymbol.Path[len(item.fromSymbol.Path)-1].Kind)
	}
	wantClosureKinds := []SubjectKind{KindEntityType, KindRelationType, KindLayer, KindColumn, KindColumn, KindColumn}
	if !reflect.DeepEqual(closureKinds, wantClosureKinds) {
		t.Fatalf("move closure order = %+v, want %+v (%+v)", closureKinds, wantClosureKinds, got.CandidateIdentity.MoveClosure)
	}
	if !reflect.DeepEqual(got.Identity.Reservations, got.CandidateIdentity.Reservations) || !reflect.DeepEqual(got.Identity.Moves, got.CandidateIdentity.Moves) || !reflect.DeepEqual(got.Identity.MoveClosure, got.CandidateIdentity.MoveClosure) {
		t.Fatalf("selected project identity order diverged from candidate: selected=%+v candidate=%+v", got.Identity, got.CandidateIdentity)
	}

	packInput := Input{Mode: CompilePack, RootPackID: pack.CanonicalID, EntryPath: pack.Entry, Packs: ResolvedDependencies{
		Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: map[string]ResolvedPack{"aws": pack},
	}}
	packResult := Resolve(packInput)
	if packResult.HasErrors {
		t.Fatalf("pack diagnostics = %+v", packResult.Diagnostics)
	}
	packReservationKinds := []SubjectKind{packResult.Identity.Reservations[0].Kind, packResult.Identity.Reservations[1].Kind}
	packMoveKinds := []SubjectKind{packResult.Identity.Moves[0].Kind, packResult.Identity.Moves[1].Kind}
	packClosureKinds := []SubjectKind{packResult.Identity.MoveClosure[0].fromSymbol.Path[0].Kind, packResult.Identity.MoveClosure[1].fromSymbol.Path[0].Kind}
	wantPackKinds := []SubjectKind{KindEntityType, KindRelationType}
	if !reflect.DeepEqual(packReservationKinds, wantPackKinds) || !reflect.DeepEqual(packMoveKinds, wantPackKinds) || !reflect.DeepEqual(packClosureKinds, wantPackKinds) {
		t.Fatalf("pack identity order reservations=%+v moves=%+v closure=%+v", packReservationKinds, packMoveKinds, packClosureKinds)
	}
	if !reflect.DeepEqual(packResult.Identity, packResult.CandidateIdentity) {
		t.Fatalf("selected pack identity order diverged from candidate: selected=%+v candidate=%+v", packResult.Identity, packResult.CandidateIdentity)
	}
}

func TestResolveAuthoredAssetLocatorAcceptanceMatrix(t *testing.T) {
	t.Parallel()
	valid := map[string]string{
		"../assets/image.png":        "assets/image.png",
		"./assets/../icons/icon.png": "types/icons/icon.png",
		"assets/file%20name.png":     "types/assets/file%20name.png",
	}
	for raw, want := range valid {
		if got, ok := ResolveAuthoredAssetLocator("types/entity.ldl", raw); !ok || got != want {
			t.Errorf("valid %q = %q,%v, want %q", raw, got, ok, want)
		}
	}
	invalid := []string{
		"", "/assets/a.png", `assets\a.png`, "https://example.com/a.png", "assets//a.png", "assets/a.png/",
		"../../../escape.png", "assets/%2e%2e/escape.png", "assets/foo%2Fbar.png", "assets/foo%5Cbar.png", "assets/%GG.png",
		"assets/e\u0301.png", "assets/a\u0085b.png", "assets/a\u009fb.png",
	}
	for _, raw := range invalid {
		if got, ok := ResolveAuthoredAssetLocator("types/entity.ldl", raw); ok {
			t.Errorf("invalid %q accepted as %q", raw, got)
		}
	}
	if _, ok := ResolveAuthoredAssetLocator("types/entity.ldl", string([]byte{'a', 0xff, 'b'})); ok {
		t.Fatal("invalid UTF-8 accepted")
	}
}

func schemaDiagnostic(diagnostics []Diagnostic, message string) *Diagnostic {
	for i := range diagnostics {
		if diagnostics[i].Message == message {
			return &diagnostics[i]
		}
	}
	return nil
}

func diagnosticCodeCount(diagnostics []Diagnostic, code string) int {
	count := 0
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			count++
		}
	}
	return count
}

func TestAcceptanceReservationDiagnosticsHaveNoSemanticDuplicates(t *testing.T) {
	t.Parallel()
	source := `project p "P" {}
reserved { entity_types ["bad"] }
`
	got := Resolve(Input{Mode: CompileProject, EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(source)}}})
	seen := map[string]bool{}
	for _, diagnostic := range got.Diagnostics {
		key := strings.Join([]string{diagnostic.Code, diagnostic.MessageKey, diagnostic.Message, rangeKey(diagnostic.Range)}, "|")
		if seen[key] {
			t.Fatalf("duplicate diagnostic = %+v", diagnostic)
		}
		seen[key] = true
	}
}

func TestAcceptanceDuplicateReservationContainersCarryPreviousRanges(t *testing.T) {
	t.Parallel()
	source := `project p "P" {}
entity_type e "E" {
  reserve {}
  reserve {}
}
layers {
  app "App" @0
}
entities e @app {
  item "Item" {
    reserve_rows [old_a]
    reserve_rows [old_b]
  }
}
reserved {}
reserved {}
moves {}
moves {}
`
	got := Resolve(Input{Mode: CompileProject, EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(source)}}})
	want := map[string]bool{
		"duplicate root reserved block":    false,
		"duplicate root moves block":       false,
		"duplicate owner reserve block":    false,
		"duplicate reserve_rows statement": false,
	}
	for _, diagnostic := range got.Diagnostics {
		if _, ok := want[diagnostic.Message]; !ok {
			continue
		}
		if len(diagnostic.Related) != 1 || diagnostic.Related[0].Relation != "previous" || diagnostic.Related[0].Range == nil {
			t.Fatalf("duplicate container context = %+v", diagnostic)
		}
		want[diagnostic.Message] = true
	}
	for message, found := range want {
		if !found {
			t.Fatalf("missing %q in %+v", message, got.Diagnostics)
		}
	}

	r := &resolver{}
	r.addPreviousRange(ModuleKey{}, zeroSpan())
	SortDiagnostics([]Diagnostic{{Code: "LDL1102", Severity: "error", Arguments: map[string]string{}}})
	SortDeclarations([]DeclarationSymbol{{Symbol: StableSymbol{Origin: Origin{Kind: OriginProject, ProjectID: "p"}}}})
}
