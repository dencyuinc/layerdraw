// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func TestExtractAllDeclarationShapes(t *testing.T) {
	t.Parallel()

	file := parse(`project p "P" {}
entity_type service "Service" {
  columns { environment "Environment" string }
  unique unique_environment [environment]
}
relation_type depends_on "Depends On" dependency {
  columns { weight "Weight" integer }
}
layers {
  application "Application" @0
}
entities service @application {
  order_api "Order API"
}
rows service [environment] {
  order_api production: prod
}
relations depends_on {
  api_dep: order_api -> order_api
}
relation_rows depends_on [weight] {
  api_dep production: 1
}
query q "Q" {
  parameters { env string }
}
view v "V" topology {
  table {
    column name {}
  }
  export svg svg "v.svg" {}
}
reference guide <<-TEXT
hello
TEXT
reserved {
  entity_types [old_type]
  relation_types [old_relation_type]
  layers [old_layer]
  entities [old_entity]
  relations [old_relation]
  queries [old_query]
  views [old_view]
  references [old_reference]
}
moves {
  entity_type old_type -> service
  entity_type_column service old_environment -> environment
  relation_type_constraint depends_on old_unique -> unique_environment
  entity_row order_api old_production -> production
  query_parameter q old_env -> env
  view_export v old_svg -> svg
}
export { service as public_service }
export { service } from "./schema.ldl"
export * from "./schema.ldl"
`)
	ast := extractModule(file)
	if len(ast.imports) != 0 {
		t.Fatalf("imports = %+v, want none", ast.imports)
	}
	if len(ast.exports) != 3 {
		t.Fatalf("exports = %d, want 3", len(ast.exports))
	}
	kinds := map[SubjectKind]int{}
	for _, d := range ast.declarations {
		kinds[d.kind]++
	}
	for _, kind := range []SubjectKind{KindProject, KindEntityType, KindRelationType, KindLayer, KindEntity, KindRelation, KindQuery, KindView, KindReference, KindColumn, KindConstraint, KindRow, KindParameter, KindTableColumn, KindExport} {
		if kinds[kind] == 0 {
			t.Fatalf("missing extracted kind %s in %+v", kind, kinds)
		}
	}
	if len(ast.reservations) != 8 {
		t.Fatalf("reservations = %+v", ast.reservations)
	}
	if len(ast.moves) != 6 {
		t.Fatalf("moves = %+v", ast.moves)
	}
}

func TestExportFromStarAndDuplicateDiagnostics(t *testing.T) {
	t.Parallel()

	in := Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`project p "P" {}
export * from "./a.ldl"
export { b as a } from "./b.ldl"
`),
		"a.ldl": parse(`entity_type a "A" {}` + "\n" + `export { a }`),
		"b.ldl": parse(`entity_type b "B" {}` + "\n" + `export { b }`),
	}}}
	got := Resolve(in)
	if !hasDiag(got, "LDL1302") {
		t.Fatalf("Diagnostics = %+v, want duplicate export", got.Diagnostics)
	}
}

func TestInvalidPackMetadataBranches(t *testing.T) {
	t.Parallel()

	in := Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"Document.ldl": parse(`project p "P" {}`),
		"document.ldl": parse(`import bad from "bad"` + "\n" + `project p "P" {}`),
	}}, Packs: ResolvedDependencies{Installs: map[string]ResolvedPack{
		"Bad-Alias": {CanonicalID: "bad/id", Version: "1.0.0", Digest: "sha256:x", Path: "pack/bad", Entry: "pack.ldl"},
		"bad":       {CanonicalID: "bad_id", Version: "1.0.0", Digest: "sha256:x", Path: "pack/bad", Entry: "pack.ldl"},
		"bad2":      {CanonicalID: "layerdraw/bad", Version: "1.0.0", Digest: "sha256:x", Path: "../bad", Entry: "pack.ldl"},
		"bad3":      {CanonicalID: "layerdraw/bad-three", Version: "1.0.0", Digest: "sha256:x", Path: "pack/shared", Entry: "../pack.ldl"},
		"bad4":      {CanonicalID: "layerdraw/bad-four", Version: "1.0.0", Digest: "sha256:x", Path: "pack/shared", Entry: "pack.ldl", SourceFiles: map[string]SourceFile{"bad//path.ldl": parse(`entity_type x "X" {}`)}},
		"bad5":      {CanonicalID: "layerdraw/bad-five", Version: "1.0.0", Digest: "sha256:x", Path: "pack/shared", Entry: "pack.ldl"},
	}}}
	got := Resolve(in)
	for _, code := range []string{"LDL1201"} {
		if !hasDiag(got, code) {
			t.Fatalf("Diagnostics = %+v, want %s", got.Diagnostics, code)
		}
	}
}

func TestMoveCycleAndMultipleSuccessors(t *testing.T) {
	t.Parallel()

	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`project p "P" {}
entity_type a "A" {}
moves {
  entity x -> y
  entity x -> z
  entity_type b -> c
  entity_type c -> b
}
`),
	}}})
	if !hasDiag(got, "LDL1303") {
		t.Fatalf("Diagnostics = %+v, want invalid move graph", got.Diagnostics)
	}
}

func TestHelperBranches(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"entity_types", "relation_types", "layers", "entities", "relations", "queries", "views", "references", "columns", "constraints", "parameters", "table_columns", "exports"} {
		if _, ok := reservationKind(raw); !ok {
			t.Fatalf("reservationKind(%q) not recognized", raw)
		}
	}
	if _, ok := reservationKind("unknown"); ok {
		t.Fatal("reservationKind accepted unknown")
	}
	for _, raw := range []string{"project", "entity_type", "relation_type", "layer", "entity", "relation", "query", "view", "reference", "entity_type_column", "relation_type_column", "entity_type_constraint", "relation_type_constraint", "entity_row", "relation_row", "query_parameter", "view_table_column", "view_export"} {
		if _, _ = moveKind(raw); raw == "" {
			t.Fatal("unreachable")
		}
	}
	if _, child := moveKind("entity_row"); !child {
		t.Fatal("entity_row should be child move")
	}
	if kind, child := moveKind("unknown"); kind != "" || child {
		t.Fatalf("unknown move kind = %q,%v", kind, child)
	}
	for _, kind := range []SubjectKind{KindEntityType, KindRelationType, KindLayer, KindEntity, KindRelation, KindQuery, KindView, KindReference, KindColumn, KindConstraint, KindRow, KindParameter, KindTableColumn, KindExport, "future"} {
		if rank := kindRank(kind); rank < 0 {
			t.Fatalf("kindRank(%q) = %d", kind, rank)
		}
	}
	for _, severity := range []string{"error", "warning", "info", "debug"} {
		if rank := severityRank(severity); rank < 0 {
			t.Fatalf("severityRank(%q) = %d", severity, rank)
		}
	}
	if _, _, ok := parseCanonicalID("bad"); ok {
		t.Fatal("parseCanonicalID accepted missing slash")
	}
	if _, _, ok := parseCanonicalID("Bad/name"); ok {
		t.Fatal("parseCanonicalID accepted non-canonical publisher")
	}
	for _, ident := range []string{"a", "a1_b2"} {
		if !isIdent(ident) {
			t.Fatalf("isIdent(%q) = false", ident)
		}
	}
	for _, ident := range []string{"", "A", "a-b"} {
		if isIdent(ident) {
			t.Fatalf("isIdent(%q) = true", ident)
		}
	}
	for _, kebab := range []string{"a", "a-b2"} {
		if !isKebab(kebab) {
			t.Fatalf("isKebab(%q) = false", kebab)
		}
	}
	for _, kebab := range []string{"", "A", "a--b", "a-", "a_b"} {
		if isKebab(kebab) {
			t.Fatalf("isKebab(%q) = true", kebab)
		}
	}
}

func TestPathAndDiagnosticHelperBranches(t *testing.T) {
	t.Parallel()

	if _, ok := normalizePath(string([]byte{0xff})); ok {
		t.Fatal("normalizePath accepted invalid UTF-8")
	}
	if _, ok := resolveRelative("a/b.ldl", "c.ldl"); ok {
		t.Fatal("resolveRelative accepted non-relative specifier")
	}
	if _, ok := resolveRelative("a/b.ldl", "../c.txt"); ok {
		t.Fatal("resolveRelative accepted non-LDL target")
	}
	if _, ok := resolveRelative("a/b.ldl", "../../c.ldl"); ok {
		t.Fatal("resolveRelative accepted origin escape")
	}
	if got := caseFoldCollisions([]string{"A.ldl", "a.ldl", "b.ldl"}); len(got) != 1 {
		t.Fatalf("caseFoldCollisions = %+v", got)
	}
	ds := []Diagnostic{
		{Code: "LDL1301", Severity: "warning", MessageKey: "b", Range: nil},
		{Code: "LDL1201", Severity: "error", MessageKey: "a", Range: &SourceRange{Origin: SourceOrigin{Kind: OriginPack, PackAddress: "ldl:pack:a:b"}, ModulePath: "b.ldl", StartByte: 2, EndByte: 3}},
		{Code: "LDL1201", Severity: "info", MessageKey: "c", Range: &SourceRange{Origin: SourceOrigin{Kind: OriginProject}, ModulePath: "a.ldl", StartByte: 1, EndByte: 1}},
		{Code: "LDL1201", Severity: "debug", MessageKey: "d", Range: &SourceRange{Origin: SourceOrigin{Kind: OriginProject}, ModulePath: "a.ldl", StartByte: 1, EndByte: 2}},
	}
	sortDiagnostics(ds)
	if ds[0].Range != nil || ds[1].Range.Origin.Kind != OriginProject || ds[len(ds)-1].Range.Origin.Kind != OriginPack {
		t.Fatalf("unexpected diagnostic order: %+v", ds)
	}
	if cmpSourceRange(nil, nil) != 0 || cmpSourceRange(nil, ds[1].Range) >= 0 || cmpSourceRange(ds[1].Range, nil) <= 0 {
		t.Fatal("nil source range comparison is not deterministic")
	}
}

func TestExtractorSmallBranches(t *testing.T) {
	t.Parallel()

	if children := nodeChildren(nil); children != nil {
		t.Fatalf("nodeChildren(nil) = %+v", children)
	}
	if firstRaw(nil) != "" {
		t.Fatal("firstRaw(nil) should be empty")
	}
	if got := stringToken(syntax.Token{Kind: syntax.TokenString, Raw: `"unterminated`}); got != `"unterminated` {
		t.Fatalf("bad string token unquote = %q", got)
	}
	if got := firstSymbolRef(&syntax.Node{}); got != "" {
		t.Fatalf("firstSymbolRef(empty) = %q", got)
	}
	refs := symbolRefs(firstNode(parse(`relations r {
  link: a.b -> c.d
}
`).Root, syntax.NodeDeclaration))
	if joined := strings.Join(refs, ","); joined != "r,a.b,c.d" {
		t.Fatalf("qualified symbol refs = %q", joined)
	}
}
