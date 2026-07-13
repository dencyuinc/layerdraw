// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func TestPackCompileSuccessSelectsEntryExports(t *testing.T) {
	t.Parallel()
	in := baseInput()
	in.Mode = CompilePack
	in.EntryPath = "pack.ldl"
	in.RootPackID = "layerdraw/aws-complete"
	in.Project = ProjectInput{}
	got := Resolve(in)
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	requireAddress(t, got, "ldl:pack:layerdraw:aws-complete")
	requireAddress(t, got, "ldl:pack:layerdraw:aws-complete:entity-type:vpc")
	if len(got.Dependencies) != 1 {
		t.Fatalf("Dependencies = %+v", got.Dependencies)
	}
}

func TestOwnerReservationsAndReserveRows(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}
entity_type service "Service" {
  reserve {
    columns [old_name]
  }
  columns {
    name "Name" string
  }
}
layers {
  app "App" @0
}
entities service @app {
  api "API" {
    reserve_rows [old_row]
  }
}
export { service, api }
`)}}})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	requireReservation(t, got, "ldl:project:p:entity-type:service:column:old_name")
	requireReservation(t, got, "ldl:project:p:entity:api:row:old_row")
}

func TestMoveOwnerVariants(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}
entity_type service "Service" {
  columns {
    name "Name" string
  }
  unique unique_name [name]
}
relation_type link "Link" dependency {
  columns {
    weight "Weight" integer
  }
}
layers {
  app "App" @0
}
entities service @app {
  api "API" {}
}
rows service [name] {
  api api: "API"
}
relations link {
  rel: api -> api {}
}
relation_rows link [weight] {
  rel rel: 1
}
query q "Q" {
  parameters {
    env string
  }
}
view v "V" topology {
  table {
    rows entity_rows
    column title {
      source attribute title
    }
  }
  export svg svg "v.svg" {
    fidelity visual_only
  }
}
moves {
  relation_type_column link old_weight -> weight
  entity_type_constraint service old_unique -> unique_name
  entity_row api old_api -> api
  relation_row rel old_rel -> rel
  query_parameter q old_env -> env
  view_table_column v old_title -> title
  view_export v old_svg -> svg
}
export { service, link, api, rel, q, v }
`)}}})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	for _, pair := range [][2]string{
		{"ldl:project:p:relation-type:link:column:old_weight", "ldl:project:p:relation-type:link:column:weight"},
		{"ldl:project:p:entity-type:service:constraint:old_unique", "ldl:project:p:entity-type:service:constraint:unique_name"},
		{"ldl:project:p:entity:api:row:old_api", "ldl:project:p:entity:api:row:api"},
		{"ldl:project:p:relation:rel:row:old_rel", "ldl:project:p:relation:rel:row:rel"},
		{"ldl:project:p:query:q:parameter:old_env", "ldl:project:p:query:q:parameter:env"},
		{"ldl:project:p:view:v:table-column:old_title", "ldl:project:p:view:v:table-column:title"},
		{"ldl:project:p:view:v:export:old_svg", "ldl:project:p:view:v:export:svg"},
	} {
		requireMove(t, got, pair[0], pair[1])
	}
}

func TestAdditionalInvalidMetadataBranches(t *testing.T) {
	t.Parallel()
	in := baseInput()
	pack := in.Packs.Installs["aws"]
	pack.Manifest.Dependencies = map[string]PackDependency{
		"aws":  {ID: "bad/id", Version: "1.0.0"},
		"dup":  {ID: "layerdraw/dup", Version: "1.0.0"},
		"dup2": {ID: "layerdraw/dup", Version: "1.0.0"},
	}
	pack.Dependencies = map[string]string{"extra": "missing", "dup": "missing"}
	in.Packs.Installs["aws"] = pack
	got := Resolve(in)
	if !hasDiag(got, "LDL1201") {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
}

func TestValidSymbolRejectsInvalidRootsAndPaths(t *testing.T) {
	t.Parallel()
	invalid := []StableSymbol{
		{Origin: Origin{Kind: OriginProject}},
		{Origin: Origin{Kind: OriginPack, Publisher: "p"}},
		{Origin: Origin{Kind: OriginProject, ProjectID: "p"}, Path: []SymbolSegment{{Kind: KindColumn, ID: "c"}}},
		{Origin: Origin{Kind: OriginProject, ProjectID: "p"}, Path: []SymbolSegment{{Kind: KindEntityType, ID: "Bad"}}},
		{Origin: Origin{Kind: OriginProject, ProjectID: "p"}, Path: []SymbolSegment{{Kind: KindEntityType, ID: "a"}, {Kind: KindEntityType, ID: "b"}}},
		{Origin: Origin{Kind: OriginProject, ProjectID: "p"}, Path: []SymbolSegment{{Kind: KindEntityType, ID: "a"}, {Kind: KindColumn, ID: "Bad"}}},
		{Origin: Origin{Kind: OriginProject, ProjectID: "p"}, Path: []SymbolSegment{{Kind: KindEntityType, ID: "a"}, {Kind: KindColumn, ID: "b"}, {Kind: KindRow, ID: "c"}}},
	}
	for _, sym := range invalid {
		if validSymbol(sym) {
			t.Fatalf("validSymbol(%+v) = true", sym)
		}
	}
}

func TestAdditionalNegativeContractBranches(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`import { a } from "./other.ldl"` + "\n" + `project p "P" {}`),
		"other.ldl":    parse(`project p "P" {}` + "\n" + `entity_type a "A" {}` + "\n" + `export { a }`),
	}}})
	if !hasDiag(got, "LDL1302") {
		t.Fatalf("project outside entry diagnostics = %+v", got.Diagnostics)
	}

	got = Resolve(Input{Mode: CompilePack, RootPackID: "layerdraw/bad", EntryPath: "pack.ldl", Packs: ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: map[string]ResolvedPack{
		"bad": {CanonicalID: "layerdraw/bad", Version: "1.0.0", Digest: testDigest("8"), Path: "pack/bad", Entry: "pack.ldl", Files: map[string]string{"pack.ldl": testDigest("7")}, Manifest: PackManifest{Format: "layerdraw-pack", FormatVersion: 1, Language: 1, ID: "layerdraw/bad", Name: "bad", Version: "1.0.0", Entry: "pack.ldl"}, SourceFiles: map[string]SourceFile{"pack.ldl": parse(`project p "P" {}`)}},
	}}})
	if !hasDiag(got, "LDL1102") {
		t.Fatalf("pack project diagnostics = %+v", got.Diagnostics)
	}

	got = Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}
entity_type service "Service" {}
reserved {
  columns [old_column]
}
moves {
  entity_type_column missing old -> new
  project old -> wrong
}
`)}}})
	if !hasDiag(got, "LDL1302") || !hasDiag(got, "LDL1303") {
		t.Fatalf("invalid owner diagnostics = %+v", got.Diagnostics)
	}
}

func TestDependencyCycleAndPackEntryMismatch(t *testing.T) {
	t.Parallel()
	in := baseInput()
	aws := in.Packs.Installs["aws"]
	aws.Manifest.Dependencies = map[string]PackDependency{"other": {ID: "layerdraw/other", Version: "1.0.0"}}
	aws.Dependencies = map[string]string{"other": "other"}
	other := aws
	other.CanonicalID = "layerdraw/other"
	other.Manifest.ID = "layerdraw/other"
	other.Manifest.Name = "other"
	other.Path = "pack/other"
	other.Digest = testDigest("6")
	other.Manifest.Dependencies = map[string]PackDependency{"aws": {ID: "layerdraw/aws-complete", Version: "1.0.0"}}
	other.Dependencies = map[string]string{"aws": "aws"}
	in.Packs.Installs["aws"] = aws
	in.Packs.Installs["other"] = other
	got := Resolve(in)
	if !hasDiag(got, "LDL1202") {
		t.Fatalf("dependency cycle diagnostics = %+v", got.Diagnostics)
	}

	single := baseInput()
	single.Mode = CompilePack
	single.EntryPath = "modules/network.ldl"
	single.RootPackID = "layerdraw/aws-complete"
	single.Project = ProjectInput{}
	got = Resolve(single)
	if !hasDiag(got, "LDL1201") {
		t.Fatalf("pack entry mismatch diagnostics = %+v", got.Diagnostics)
	}
}

func TestBindingCollisionAndDiagnosticRelatedRangeKey(t *testing.T) {
	t.Parallel()
	st := &moduleState{key: ModuleKey{Origin: Origin{Kind: OriginProject, ProjectID: "p"}, Path: "document.ldl"}, imported: map[SubjectKind]map[string]DeclarationSymbol{}}
	r := &resolver{}
	a := DeclarationSymbol{Address: "ldl:project:p:entity-type:a", Symbol: StableSymbol{Origin: st.key.Origin, Path: []SymbolSegment{{Kind: KindEntityType, ID: "a"}}}}
	b := DeclarationSymbol{Address: "ldl:project:p:entity-type:b", Symbol: StableSymbol{Origin: st.key.Origin, Path: []SymbolSegment{{Kind: KindEntityType, ID: "b"}}}}
	r.addImportedBinding(st, KindEntityType, "x", syntaxSpan(1, 2), "import:x", a)
	r.addImportedBinding(st, KindEntityType, "x", syntaxSpan(3, 4), "import:x", b)
	if !hasDiag(Result{Diagnostics: r.diagnostics}, "LDL1302") {
		t.Fatalf("binding collision diagnostics = %+v", r.diagnostics)
	}
	ds := []Diagnostic{{Related: []DiagnosticRelated{{Range: &SourceRange{StartByte: 12, EndByte: 34}}}}}
	sortDiagnostics(ds)
	if len(ds[0].Related) != 1 {
		t.Fatalf("related diagnostics = %+v", ds)
	}
}

func syntaxSpan(start, end int) syntax.Span {
	return syntax.Span{Start: start, End: end}
}

func requireReservation(t *testing.T, got Result, address string) {
	t.Helper()
	for _, res := range got.Identity.Reservations {
		if res.Address == address {
			return
		}
	}
	t.Fatalf("reservation %s missing in %+v", address, got.Identity.Reservations)
}
