// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func TestReviewPrivateVisitedDeclarationsAreNotPublished(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`import { exported } from "./schema.ldl"
project p "P" {}
export { exported }
`),
		"schema.ldl": parse(`entity_type exported "Exported" {}
entity_type private_helper "Private" {}
export { exported }
`),
	}}})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	requireAddress(t, got, "ldl:project:p:entity-type:exported")
	if hasAddress(got, "ldl:project:p:entity-type:private_helper") {
		t.Fatal("private helper leaked into effective declarations")
	}
}

func TestReviewDuplicateIdentityAcrossModulesFails(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`import { service as a } from "./a.ldl"
import { service as b } from "./b.ldl"
project p "P" {}
export { a, b }
`),
		"a.ldl": parse(`entity_type service "A" {}` + "\n" + `export { service }`),
		"b.ldl": parse(`entity_type service "B" {}` + "\n" + `export { service }`),
	}}})
	if !hasDiag(got, "LDL1302") || len(got.Declarations) != 0 {
		t.Fatalf("duplicate identity result = declarations=%+v diagnostics=%+v", got.Declarations, got.Diagnostics)
	}
}

func TestReviewPackCompileRequiresRootPackID(t *testing.T) {
	t.Parallel()
	in := baseInput()
	in.Mode = CompilePack
	in.EntryPath = "pack.ldl"
	other := in.Packs.Installs["aws"]
	other.CanonicalID = "layerdraw/other"
	other.Manifest.ID = "layerdraw/other"
	other.Manifest.Name = "other"
	other.Path = "pack/other"
	other.Digest = testDigest("4")
	in.Packs.Installs["other"] = other
	got := Resolve(in)
	if !hasDiag(got, "LDL1201") {
		t.Fatalf("Diagnostics = %+v, want missing root pack diagnostic", got.Diagnostics)
	}
}

func TestReviewChildMovesAreOwnerScopedAndClosureTransitive(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`project p "P" {}
entity_type service "Service" {
  columns {
    environment "Environment" string
  }
}
entity_type c "C" {}
moves {
  entity_type a -> b
  entity_type b -> c
  entity_type_column service old_environment -> environment
}
`),
	}}})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	requireMove(t, got, "ldl:project:p:entity-type:service:column:old_environment", "ldl:project:p:entity-type:service:column:environment")
	requireClosure(t, got, "ldl:project:p:entity-type:a", "ldl:project:p:entity-type:c")
}

func TestReviewInvalidUnimportedResolvedMetadataFails(t *testing.T) {
	t.Parallel()
	in := Input{
		EntryPath: "document.ldl",
		Project:   ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}`)}},
		Packs: ResolvedDependencies{Format: "wrong", FormatVersion: 99, Language: 2, Installs: map[string]ResolvedPack{
			"bad": {
				CanonicalID:  "layerdraw/bad",
				Version:      "latest",
				Digest:       "not-sha",
				Path:         "pack/bad",
				Entry:        "pack.ldl",
				Files:        map[string]string{"pack.ldl": "nope"},
				Dependencies: map[string]string{"dep": "missing"},
				Manifest: PackManifest{Format: "layerdraw-pack", FormatVersion: 1, Language: 1, ID: "layerdraw/bad", Name: "dep", Version: "latest", Entry: "pack.ldl", Dependencies: map[string]PackDependency{
					"dep": {ID: "layerdraw/missing", Version: "1.0.0"},
				}},
				SourceFiles: map[string]SourceFile{"pack.ldl": parse(`entity_type a "A" {}`)},
			},
		}},
	}
	got := Resolve(in)
	if !hasDiag(got, "LDL1201") || !hasDiag(got, "LDL1203") {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
}

func TestReviewOwnerScopedChildIDsAreAllowed(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`project p "P" {}
entity_type service "Service" {
  columns {
    name "Name" string
  }
}
entity_type database "Database" {
  columns {
    name "Name" string
  }
}
export { service, database }
`),
	}}})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	requireAddress(t, got, "ldl:project:p:entity-type:service:column:name")
	requireAddress(t, got, "ldl:project:p:entity-type:database:column:name")
}

func TestReviewPackRootPublishedOnce(t *testing.T) {
	t.Parallel()
	got := Resolve(baseInput())
	count := 0
	for _, decl := range got.Declarations {
		if decl.Address == "ldl:pack:layerdraw:aws-complete" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("pack root count = %d, declarations=%+v", count, got.Declarations)
	}
}

func TestReviewProjectDeclarationCardinality(t *testing.T) {
	t.Parallel()
	missing := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`entity_type a "A" {}`)}}})
	multiple := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}` + "\n" + `project q "Q" {}`)}}})
	if !hasDiag(missing, "LDL1302") || !hasDiag(multiple, "LDL1302") {
		t.Fatalf("missing=%+v multiple=%+v", missing.Diagnostics, multiple.Diagnostics)
	}
}

func TestReviewNamespaceAliasDuplicateAndBindingMetadata(t *testing.T) {
	t.Parallel()
	in := baseInput()
	in.Project.Files["document.ldl"] = parse(`import aws from "aws"
import aws from "aws"
project order_platform "Order Platform" {}
`)
	dup := Resolve(in)
	if !hasDiag(dup, "LDL1302") {
		t.Fatalf("Diagnostics = %+v", dup.Diagnostics)
	}
	got := Resolve(baseInput())
	for _, b := range got.Bindings {
		if b.SourceText == "network" {
			if b.Range.Empty() || b.Via == "" {
				t.Fatalf("binding metadata not populated: %+v", b)
			}
			return
		}
	}
	t.Fatal("network binding not found")
}

func TestReviewRowsResolveOrderIndependentlyAndProjectMoveUsesRoot(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}
rows service [environment] {
  order_api production: prod
}
entity_type service "Service" {
  columns {
    environment "Environment" string
  }
}
layers {
  app "App" @0
}
entities service @app {
  order_api "Order API"
}
moves {
  project old_p -> p
}
export { order_api }
`)}}})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	requireAddress(t, got, "ldl:project:p:entity:order_api:row:production")
	requireMove(t, got, "ldl:project:old_p", "ldl:project:p")
}

func TestReviewMoveFanInAndDuplicateReservationsFail(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}
entity_type c "C" {}
reserved {
  entity_types [old_service, old_service]
}
moves {
  entity_type a -> c
  entity_type b -> c
}
`)}}})
	if !hasDiag(got, "LDL1302") || !hasDiag(got, "LDL1303") {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
}

func TestReviewSyntaxDiagnosticsGatePublication(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": SourceFromParse(syntax.Parse([]byte(`project p "P" {`)))}}})
	if !hasDiag(got, "LDL1101") || len(got.Declarations) != 0 {
		t.Fatalf("syntax gate result declarations=%+v diagnostics=%+v", got.Declarations, got.Diagnostics)
	}
}

func TestReviewPackFilesAndProjectPathsAreCanonical(t *testing.T) {
	t.Parallel()
	in := baseInput()
	pack := in.Packs.Installs["aws"]
	delete(pack.Files, "modules/network.ldl")
	pack.SourceFiles["extra.ldl"] = parse(`entity_type extra "Extra" {}`)
	in.Packs.Installs["aws"] = pack
	got := Resolve(in)
	if !hasDiag(got, "LDL1201") {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	path := "cafe\u0301.ldl"
	got = Resolve(Input{EntryPath: path, Project: ProjectInput{Files: map[string]SourceFile{path: parse(`project p "P" {}`)}}})
	if !hasDiag(got, "LDL1201") {
		t.Fatalf("NFC diagnostics = %+v", got.Diagnostics)
	}
}

func TestReviewDiagnosticArgumentsAndRelatedOrdering(t *testing.T) {
	t.Parallel()
	ds := []Diagnostic{
		{Code: "LDL1201", Severity: "error", MessageKey: "same", Arguments: map[string]string{"id": "b"}, Related: []DiagnosticRelated{{Relation: "target", SubjectAddress: "z"}, {Relation: "cause", SubjectAddress: "a"}, {Relation: "cause", SubjectAddress: "a"}}},
		{Code: "LDL1201", Severity: "error", MessageKey: "same", Arguments: map[string]string{"id": "a"}},
	}
	sortDiagnostics(ds)
	if ds[0].Arguments["id"] != "a" {
		t.Fatalf("argument order = %+v", ds)
	}
	if len(ds[1].Related) != 2 || ds[1].Related[0].Relation != "cause" {
		t.Fatalf("related order/dedupe = %+v", ds[1].Related)
	}
}

func requireMove(t *testing.T, got Result, from, to string) {
	t.Helper()
	for _, mv := range got.Identity.Moves {
		if mv.FromAddress == from && mv.ToAddress == to {
			return
		}
	}
	t.Fatalf("move %s -> %s missing in %+v", from, to, got.Identity.Moves)
}

func requireClosure(t *testing.T, got Result, from, to string) {
	t.Helper()
	for _, mv := range got.Identity.MoveClosure {
		if mv.From == from && mv.To == to {
			return
		}
	}
	t.Fatalf("closure %s -> %s missing in %+v", from, to, got.Identity.MoveClosure)
}
