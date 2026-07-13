// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func TestResolverDefensiveBranches(t *testing.T) {
	t.Parallel()

	if got := Resolve(Input{EntryPath: "../bad.ldl"}); !hasDiag(got, "LDL1201") {
		t.Fatalf("invalid entry diagnostics = %+v", got.Diagnostics)
	}
	if got := Resolve(Input{Mode: CompilePack, EntryPath: "pack.ldl"}); !hasDiag(got, "LDL1201") {
		t.Fatalf("pack compile without pack diagnostics = %+v", got.Diagnostics)
	}
	if got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`entity_type a "A" {}`)}}}); !hasDiag(got, "LDL1302") || len(got.Declarations) != 0 {
		t.Fatalf("missing project result = declarations=%+v diagnostics=%+v", got.Declarations, got.Diagnostics)
	}

	r := &resolver{input: baseInput(), packs: map[string]packInfo{}, aliases: map[string]string{}, modules: map[ModuleKey]*moduleState{}, visiting: map[ModuleKey]bool{}}
	if _, ok := r.sourceFile(ModuleKey{Origin: Origin{Kind: OriginPack, Publisher: "missing", PackName: "pack"}, Path: "pack.ldl"}); ok {
		t.Fatal("sourceFile found missing pack")
	}
	if _, ok := r.packByOrigin(Origin{Kind: OriginPack, Publisher: "missing", PackName: "pack"}); ok {
		t.Fatal("packByOrigin found missing pack")
	}
}

func TestSemanticSymbolAccessors(t *testing.T) {
	t.Parallel()

	origin := Origin{Kind: OriginProject, ProjectID: "p"}
	entity := StableSymbol{Origin: origin, Path: []SymbolSegment{{Kind: KindEntityType, ID: "service"}}}
	if got := StableAddress(entity); got != "ldl:project:p:entity-type:service" {
		t.Fatalf("stable address = %q", got)
	}
	topLevel := MoveClosure{toSymbol: entity}
	if got := MoveClosureKind(topLevel); got != KindEntityType {
		t.Fatalf("top-level closure kind = %q", got)
	}
	if owner, ok := MoveClosureOwner(topLevel); ok {
		t.Fatalf("top-level closure owner = %+v", owner)
	}
	child := MoveClosure{toSymbol: StableSymbol{Origin: origin, Path: append(append([]SymbolSegment{}, entity.Path...), SymbolSegment{Kind: KindColumn, ID: "environment"})}}
	if got := MoveClosureKind(child); got != KindColumn {
		t.Fatalf("child closure kind = %q", got)
	}
	owner, ok := MoveClosureOwner(child)
	if !ok || StableAddress(owner) != StableAddress(entity) {
		t.Fatalf("child closure owner = %+v, %v", owner, ok)
	}
	if got := MoveClosureKind(MoveClosure{toSymbol: StableSymbol{Origin: origin}}); got != KindProject {
		t.Fatalf("project closure kind = %q", got)
	}
}

func TestResolveSpecifierBranches(t *testing.T) {
	t.Parallel()

	r := &resolver{
		packs: map[string]packInfo{
			"aws": {origin: Origin{Kind: OriginPack, Publisher: "layerdraw", PackName: "aws-complete"}, pack: ResolvedPack{Entry: "pack.ldl", Manifest: PackManifest{Name: "aws"}, Dependencies: map[string]string{"net": "network"}}},
		},
		aliases:     map[string]string{"layerdraw/aws-complete": "aws"},
		diagnostics: nil,
	}
	project := ModuleKey{Origin: Origin{Kind: OriginProject, ProjectID: "p"}, Path: "document.ldl"}
	if key, ok := r.resolveSpecifier(project, "aws.network.vpc", syntax.Span{}); !ok || key.Path != "modules/network/vpc.ldl" {
		t.Fatalf("pack specifier = %+v,%v", key, ok)
	}
	for _, spec := range []string{"bad-name", "missing", "../escape.ldl"} {
		r.resolveSpecifier(project, spec, syntax.Span{})
	}
	pack := ModuleKey{Origin: Origin{Kind: OriginPack, Publisher: "layerdraw", PackName: "aws-complete"}, Path: "pack.ldl"}
	if key, ok := r.resolveSpecifier(pack, "aws.network", syntax.Span{}); !ok || key.Path != "modules/network.ldl" {
		t.Fatalf("self pack specifier = %+v,%v", key, ok)
	}
	r.resolveSpecifier(pack, "net.network", syntax.Span{})
	r.resolveSpecifier(pack, "missing.network", syntax.Span{})
	if len(r.diagnostics) == 0 {
		t.Fatal("expected diagnostics for invalid specifier branches")
	}
}

func TestImportExportAndReferenceErrorBranches(t *testing.T) {
	t.Parallel()

	st := &moduleState{
		key:      ModuleKey{Origin: Origin{Kind: OriginProject, ProjectID: "p"}, Path: "document.ldl"},
		localTop: map[SubjectKind]map[string]DeclarationSymbol{KindEntityType: {"local": {Kind: KindEntityType, ID: "local"}}},
		imported: map[SubjectKind]map[string]DeclarationSymbol{},
	}
	target := &moduleState{exports: map[string]DeclarationSymbol{
		"remote": {Kind: KindEntityType, ID: "remote", Symbol: StableSymbol{Origin: st.key.Origin, Path: []SymbolSegment{{Kind: KindEntityType, ID: "remote"}}}, Address: "ldl:project:p:entity-type:remote"},
	}}
	r := &resolver{modules: map[ModuleKey]*moduleState{}, diagnostics: nil}
	imp := ImportDecl{Kind: ImportNamed, Items: []ImportItem{{Remote: "missing", Local: "missing"}, {Remote: "remote", Local: "local"}, {Remote: "remote", Local: "alias"}, {Remote: "remote", Local: "alias"}}}
	r.bindImport(st, &imp, target)
	if len(r.diagnostics) < 3 {
		t.Fatalf("bindImport diagnostics = %+v", r.diagnostics)
	}

	st.ast.exports = []ExportDecl{
		{Kind: ExportLocal, Items: []ExportItem{{Local: "missing", Public: "missing"}}},
		{Kind: ExportFrom, Module: ModuleKey{Origin: st.key.Origin, Path: "dep.ldl"}, Items: []ExportItem{{Local: "missing", Public: "missing"}}},
		{Kind: ExportStar, Module: ModuleKey{Origin: st.key.Origin, Path: "none.ldl"}},
	}
	r.modules[ModuleKey{Origin: st.key.Origin, Path: "dep.ldl"}] = &moduleState{exports: map[string]DeclarationSymbol{}}
	_ = r.computeExports(st)
	if len(r.diagnostics) < 5 {
		t.Fatalf("computeExports diagnostics = %+v", r.diagnostics)
	}

	st.ast.declarations = []rawDecl{{kind: KindEntity, id: "e", refs: []rawRef{{kind: KindLayer, text: "missing", span: syntax.Span{Start: 1, End: 2}}}}}
	r.resolveDeclarationRefs(st)
	if len(r.diagnostics) < 6 {
		t.Fatalf("resolveDeclarationRefs diagnostics = %+v", r.diagnostics)
	}
	if _, ok := single(nil); ok {
		t.Fatal("single(nil) = ok")
	}
}

func TestOrderingHelperBranches(t *testing.T) {
	t.Parallel()

	if compareOrigin(Origin{Kind: OriginProject, ProjectID: "a"}, Origin{Kind: OriginProject, ProjectID: "b"}) >= 0 {
		t.Fatal("project origin comparison failed")
	}
	if compareOrigin(Origin{Kind: OriginPack, Publisher: "a", PackName: "z"}, Origin{Kind: OriginPack, Publisher: "b", PackName: "a"}) >= 0 {
		t.Fatal("pack publisher comparison failed")
	}
	if compareOrigin(Origin{Kind: OriginPack, Publisher: "a", PackName: "a"}, Origin{Kind: OriginPack, Publisher: "a", PackName: "b"}) >= 0 {
		t.Fatal("pack name comparison failed")
	}
	a := StableSymbol{Origin: Origin{Kind: OriginProject, ProjectID: "p"}, Path: []SymbolSegment{{Kind: KindEntityType, ID: "a"}}}
	b := StableSymbol{Origin: Origin{Kind: OriginProject, ProjectID: "p"}, Path: []SymbolSegment{{Kind: KindEntityType, ID: "a"}, {Kind: KindColumn, ID: "b"}}}
	if compareSymbol(a, b) >= 0 {
		t.Fatal("root path length comparison failed")
	}
	ds := []Diagnostic{
		{Code: "LDL1201", Severity: "error", SubjectAddress: "b", OwnerAddress: "a", MessageKey: "a"},
		{Code: "LDL1201", Severity: "error", SubjectAddress: "a", OwnerAddress: "b", MessageKey: "b"},
		{Code: "LDL1201", Severity: "error", SubjectAddress: "a", OwnerAddress: "a", MessageKey: "c"},
	}
	sortDiagnostics(ds)
	if ds[0].SubjectAddress != "a" || ds[0].OwnerAddress != "a" {
		t.Fatalf("sortDiagnostics tie-break = %+v", ds)
	}
}

func TestAdditionalCoverageBranches(t *testing.T) {
	t.Parallel()

	if imp := extractImport(&syntax.Node{}); imp.Specifier != "" {
		t.Fatalf("empty import extraction = %+v", imp)
	}
	if exp := extractExport(nil); exp.Kind != "" {
		t.Fatalf("nil export extraction = %+v", exp)
	}
	if node := firstNode(parse(`project p "P" {}`).Root, syntax.NodeImportDecl); node != nil {
		t.Fatalf("unexpected import node: %+v", node)
	}
	if toks := directTokens(nil); toks != nil {
		t.Fatalf("directTokens(nil) = %+v", toks)
	}
	if got := stringToken(syntax.Token{Kind: syntax.TokenIdentifier, Raw: "identifier"}); got != "identifier" {
		t.Fatalf("identifier stringToken = %q", got)
	}

	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`project p "P" {}
entity_type service "Service" {}
layers {
  app "App" @0
}
entities service @app {
  a "A"
}
relation_type r "R" dependency {}
relations r {
  rel: a -> a
}
relation_rows rel [x] {
  rel old: 1
}
export { remote } from "./remote.ldl"
`),
		"remote.ldl": parse(`entity_type remote "Remote" {}` + "\n" + `export { remote }`),
	}}})
	if !hasAddress(got, "ldl:project:p:relation:rel:row:old") {
		t.Fatalf("relation row address missing: %s diagnostics=%+v", addresses(got), got.Diagnostics)
	}
	foundExportFrom := false
	for _, exp := range got.Exports {
		if exp.PublicName == "remote" && exp.TargetAddress == "ldl:project:p:entity-type:remote" {
			foundExportFrom = true
		}
	}
	if !foundExportFrom {
		t.Fatalf("export-from binding missing: %+v", got.Exports)
	}
}
