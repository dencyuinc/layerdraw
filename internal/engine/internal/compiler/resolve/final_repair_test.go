// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import "testing"

func TestFinalReExportCycleReturnsDiagnosticWithoutRecursion(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "a.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"a.ldl": parse(`project p "P" {}` + "\n" + `export * from "./b.ldl"`),
		"b.ldl": parse(`export * from "./a.ldl"`),
	}}})
	if !hasDiag(got, "LDL1202") || len(got.Declarations) != 0 {
		t.Fatalf("cycle result declarations=%+v diagnostics=%+v", got.Declarations, got.Diagnostics)
	}
}

func TestFinalProjectEntrySameLocalNamedImportsAcrossKindsSelectBoth(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`import { service as shared } from "./schema.ldl"
import { q as shared } from "./query.ldl"
project p "P" {}
`),
		"schema.ldl": parse(`entity_type service "Service" {}` + "\n" + `export { service }`),
		"query.ldl":  parse(`query q "Q" {}` + "\n" + `export { q }`),
	}}})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	requireAddress(t, got, "ldl:project:p:entity-type:service")
	requireAddress(t, got, "ldl:project:p:query:q")
}

func TestFinalMoveSourceMayNotAlsoBeReserved(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}
entity_type service "Service" {}
reserved {
  entity_types [old_service]
}
moves {
  entity_type old_service -> service
}
`)}}})
	if !hasDiag(got, "LDL1302") || len(got.Identity.Moves) != 0 {
		t.Fatalf("reservation/move disjoint result moves=%+v diagnostics=%+v", got.Identity.Moves, got.Diagnostics)
	}
}

func TestFinalPackRootReservationAndMoveKindsAreLimited(t *testing.T) {
	t.Parallel()
	pack := baseInput().Packs.Installs["aws"]
	pack.SourceFiles = map[string]SourceFile{"pack.ldl": parse(`reserved {
  layers [old_layer]
}
moves {
  layer old_layer -> old_layer
}
`)}
	pack.Files = map[string]string{"pack.ldl": testDigest("a")}
	in := Input{Mode: CompilePack, RootPackID: "layerdraw/aws-complete", EntryPath: "pack.ldl", Packs: ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: map[string]ResolvedPack{"aws": pack}}}
	got := Resolve(in)
	if !hasDiag(got, "LDL1102") || !hasDiag(got, "LDL1303") {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
}

func TestFinalImportExportFinalizationIsDependencyOrderIndependent(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`import { local_service } from "./a.ldl"
project p "P" {}
export { local_service }
`),
		"a.ldl": parse(`import { service as local_service } from "./b.ldl"` + "\n" + `export { local_service }`),
		"b.ldl": parse(`export { service } from "./c.ldl"`),
		"c.ldl": parse(`entity_type service "Service" {}` + "\n" + `export { service }`),
	}}})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	requireAddress(t, got, "ldl:project:p:entity-type:service")
}

func TestFinalRootBlocksOnlyEntryAndSingleOwnerBlocks(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`import { service } from "./schema.ldl"` + "\n" + `project p "P" {}`),
		"schema.ldl": parse(`entity_type service "Service" {
  reserve {
    columns [old_a]
  }
  reserve {
    columns [old_b]
  }
}
reserved {
  entity_types [old_service]
}
moves {
  entity_type old_service -> service
}
export { service }
`),
	}}})
	if !hasDiag(got, "LDL1302") || !hasDiag(got, "LDL1303") {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
}

func TestFinalAuthoredMoveVariantDisambiguatesOwnerKind(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}
entity_type shared "Shared" {
  columns {
    name "Name" string
  }
}
relation_type shared "Shared Relation" dependency {
  columns {
    name "Name" string
  }
}
moves {
  entity_type_column shared old_name -> name
  relation_type_column shared old_rel_name -> name
}
`)}}})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	requireMove(t, got, "ldl:project:p:entity-type:shared:column:old_name", "ldl:project:p:entity-type:shared:column:name")
	requireMove(t, got, "ldl:project:p:relation-type:shared:column:old_rel_name", "ldl:project:p:relation-type:shared:column:name")
}

func TestFinalDerivedMoveClosureIncludesProjectAndOwnerSubtree(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}
entity_type service "Service" {
  columns {
    name "Name" string
  }
}
entity_type api "API" {
  columns {
    name "Name" string
  }
}
moves {
  project old_p -> p
  entity_type old_api -> api
}
export { api }
`)}}})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	requireClosure(t, got, "ldl:project:old_p:entity-type:api", "ldl:project:p:entity-type:api")
	requireClosure(t, got, "ldl:project:p:entity-type:old_api:column:name", "ldl:project:p:entity-type:api:column:name")
}

func TestFinalDependencySummaryIncludesTransitiveSelectedPackDependencies(t *testing.T) {
	t.Parallel()
	in := baseInput()
	aws := in.Packs.Installs["aws"]
	aws.Manifest.Dependencies = map[string]PackDependency{"net": {ID: "layerdraw/network", Version: "1.0.0"}}
	aws.Dependencies = map[string]string{"net": "net"}
	in.Packs.Installs["aws"] = aws
	in.Packs.Installs["net"] = ResolvedPack{
		CanonicalID: "layerdraw/network",
		Version:     "1.0.0",
		Digest:      testDigest("4"),
		Path:        "pack/net",
		Entry:       "pack.ldl",
		Files:       map[string]string{"pack.ldl": testDigest("5")},
		Manifest:    PackManifest{Format: "layerdraw-pack", FormatVersion: 1, Language: 1, ID: "layerdraw/network", Name: "net", Version: "1.0.0", Entry: "pack.ldl"},
		SourceFiles: map[string]SourceFile{"pack.ldl": parse(`entity_type subnet "Subnet" {}` + "\n" + `export { subnet }`)},
	}
	got := Resolve(in)
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	requireDependency(t, got, "layerdraw/aws-complete")
	requireDependency(t, got, "layerdraw/network")
}

func TestFinalDependencySummaryDeduplicatesPackOriginAliases(t *testing.T) {
	t.Parallel()
	in := baseInput()
	alias := in.Packs.Installs["aws"]
	in.Packs.Installs["aws_duplicate"] = alias
	got := Resolve(in)
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	count := 0
	for _, dep := range got.Dependencies {
		if dep.CanonicalID == "layerdraw/aws-complete" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("aws dependency count = %d in %+v", count, got.Dependencies)
	}
}

func TestFinalRejectsUnsupportedModeNoncanonicalPathsAndSemver(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{Mode: CompileMode("bad"), EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}`)}}})
	if !hasDiag(got, "LDL1201") {
		t.Fatalf("unsupported mode diagnostics = %+v", got.Diagnostics)
	}
	got = Resolve(Input{EntryPath: "dir/../document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}`)}}})
	if !hasDiag(got, "LDL1201") {
		t.Fatalf("noncanonical entry diagnostics = %+v", got.Diagnostics)
	}
	if isExactSemver("01.2.3") || isExactSemver("1.02.3") || isExactSemver("1.2.3-01") || isExactSemver("1.2.3-") {
		t.Fatal("noncanonical semver accepted")
	}
	for _, invalid := range []string{"1.2", "1.2.3+", "1.2.3+bad..meta", "1.2.3-alpha..beta", "1.2.3-01", "1.2.x"} {
		if isExactSemver(invalid) {
			t.Fatalf("invalid semver accepted: %s", invalid)
		}
	}
	for _, valid := range []string{"0.0.0", "1.2.3-alpha.1+build.5", "10.20.30-rc.1"} {
		if !isExactSemver(valid) {
			t.Fatalf("valid semver rejected: %s", valid)
		}
	}
}

func TestFinalCanonicalModulePathValidation(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"schema/%5cservice.ldl",
		"schema/%00service.ldl",
		"%2fschema/service.ldl",
		"schema/%2e%2e/service.ldl",
		"schema//service.ldl",
		"Schema/service.ldl",
		"schema/service-name.ldl",
		"schema/e\u0301.ldl",
	} {
		if norm, ok := normalizePath(raw); ok {
			t.Fatalf("invalid path %q normalized to %q", raw, norm)
		}
	}
	if norm, ok := normalizePath("schema/service_name.ldl"); !ok || norm != "schema/service_name.ldl" {
		t.Fatalf("valid lower-snake module path rejected: %q %v", norm, ok)
	}
}

func TestFinalExportBindingsPreserveSourceMetadata(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`import { exported } from "./schema.ldl"
project p "P" {}
export { exported as public_export }
export { other as public_other } from "./other.ldl"
`),
		"schema.ldl": parse(`entity_type exported "Exported" {}` + "\n" + `export { exported }`),
		"other.ldl":  parse(`entity_type other "Other" {}` + "\n" + `export { other }`),
	}}})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	local := requireExport(t, got, "public_export")
	if local.ReExport || local.Range.Start == local.Range.End {
		t.Fatalf("local export metadata = %+v", local)
	}
	reExport := requireExport(t, got, "public_other")
	if !reExport.ReExport || reExport.Range.Start == reExport.Range.End {
		t.Fatalf("re-export metadata = %+v", reExport)
	}
}

func TestFinalCandidatesPreserveUnselectedResolvedDeclarations(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`import { exported } from "./schema.ldl"` + "\n" + `project p "P" {}`),
		"schema.ldl":   parse(`entity_type exported "Exported" {}` + "\n" + `entity_type private_helper "Private" {}` + "\n" + `export { exported }`),
	}}})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	if hasAddress(got, "ldl:project:p:entity-type:private_helper") {
		t.Fatal("private helper leaked into selected declarations")
	}
	if !hasCandidate(got, "ldl:project:p:entity-type:private_helper") {
		t.Fatalf("private helper missing from candidates: %+v", got.Candidates)
	}
}

func requireDependency(t *testing.T, got Result, canonicalID string) {
	t.Helper()
	for _, dep := range got.Dependencies {
		if dep.CanonicalID == canonicalID {
			return
		}
	}
	t.Fatalf("dependency %s missing in %+v", canonicalID, got.Dependencies)
}

func hasCandidate(got Result, address string) bool {
	for _, decl := range got.Candidates {
		if decl.Address == address {
			return true
		}
	}
	return false
}

func requireExport(t *testing.T, got Result, name string) ExportBinding {
	t.Helper()
	for _, binding := range got.Exports {
		if binding.PublicName == name {
			return binding
		}
	}
	t.Fatalf("export %s missing in %+v", name, got.Exports)
	return ExportBinding{}
}

func TestFinalDiagnosticRelatedRecordsAreSortedAndDeduplicated(t *testing.T) {
	t.Parallel()
	second := &SourceRange{Origin: SourceOrigin{Kind: OriginProject}, ModulePath: "b.ldl", StartByte: 11, EndByte: 12}
	first := &SourceRange{Origin: SourceOrigin{Kind: OriginProject}, ModulePath: "a.ldl", StartByte: 2, EndByte: 3}
	ds := []Diagnostic{
		{Code: "LDL1302", Severity: "error", MessageKey: "b", Arguments: map[string]string{"z": "1"}},
		{Code: "LDL1302", Severity: "error", MessageKey: "a", Arguments: map[string]string{"a": "1"}, Related: []DiagnosticRelated{
			{Relation: "previous", Message: "second", Range: second},
			{Relation: "previous", Message: "first", Range: first},
			{Relation: "previous", Message: "first", Range: first},
		}},
	}
	sortDiagnostics(ds)
	if ds[0].MessageKey != "a" || len(ds[0].Related) != 2 || ds[0].Related[0].Message != "first" || ds[0].Related[1].Message != "second" {
		t.Fatalf("diagnostics not canonicalized: %+v", ds)
	}
}

func TestFinalReservationOwnerKindCompatibility(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}
relation_type link "Link" dependency {
  reserve {
    constraints [old_unique]
  }
}
query q "Q" {
  reserve {
    columns [bad_col]
  }
}
view v "V" topology {
  reserve {
    table_columns [old_col]
    exports [old_export]
  }
}
entity_type bad "Bad" {
  reserve {
    parameters [bad_param]
  }
}
export { link, q, v }
`)}}})
	if !hasDiag(got, "LDL1102") {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
}
