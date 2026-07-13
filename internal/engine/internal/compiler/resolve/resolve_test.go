// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func TestResolveProjectAndPackClosure(t *testing.T) {
	t.Parallel()

	in := baseInput()
	got := Resolve(in)
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v, want none", got.Diagnostics)
	}
	requireAddress(t, got, "ldl:project:order_platform")
	requireAddress(t, got, "ldl:project:order_platform:entity-type:service")
	requireAddress(t, got, "ldl:project:order_platform:entity-type:service:column:environment")
	requireAddress(t, got, "ldl:project:order_platform:layer:application")
	requireAddress(t, got, "ldl:project:order_platform:entity:order_api")
	requireAddress(t, got, "ldl:project:order_platform:entity:order_api:row:production")
	requireAddress(t, got, "ldl:pack:layerdraw:aws-complete:entity-type:vpc")
	requireBinding(t, got, "aws.vpc", "ldl:pack:layerdraw:aws-complete:entity-type:vpc")
	requireBinding(t, got, "network", "ldl:pack:layerdraw:aws-complete:entity-type:vpc")
	if len(got.Modules) != 4 {
		t.Fatalf("Modules = %d, want project entry, schema module, pack entry, pack module", len(got.Modules))
	}
	if len(got.Dependencies) != 1 || got.Dependencies[0].Address != "ldl:pack:layerdraw:aws-complete" {
		t.Fatalf("Dependencies = %+v", got.Dependencies)
	}
}

func TestInstalledPackAliasDoesNotChangeIdentity(t *testing.T) {
	t.Parallel()

	a := baseInput()
	b := baseInput()
	pack := b.Packs.Installs["aws"]
	delete(b.Packs.Installs, "aws")
	b.Packs.Installs["cloud"] = pack
	b.Project.Files["document.ldl"] = parse(`import { vpc as network } from "cloud.network"
project order_platform "Order Platform" {}
export { network }
`)
	gotA := Resolve(a)
	gotB := Resolve(b)
	if gotA.HasErrors || gotB.HasErrors {
		t.Fatalf("diagnostics A=%+v B=%+v", gotA.Diagnostics, gotB.Diagnostics)
	}
	if !hasAddress(gotB, "ldl:pack:layerdraw:aws-complete:entity-type:vpc") {
		t.Fatalf("renamed install alias changed pack identity: %+v", gotB.Declarations)
	}
}

func TestResolvePackDependencyLocalName(t *testing.T) {
	t.Parallel()

	in := baseInput()
	pack := in.Packs.Installs["aws"]
	pack.SourceFiles["pack.ldl"] = parse(`import dep from "network"
export * from "network.network"
`)
	pack.Dependencies = map[string]string{"network": "net"}
	pack.Manifest.Dependencies = map[string]PackDependency{"network": {ID: "layerdraw/network", Version: "1.0.0"}}
	in.Packs.Installs["aws"] = pack
	in.Packs.Installs["net"] = ResolvedPack{
		CanonicalID: "layerdraw/network",
		Version:     "1.0.0",
		Digest:      testDigest("2"),
		Path:        "pack/net",
		Entry:       "pack.ldl",
		Files:       map[string]string{"pack.ldl": testDigest("c"), "modules/network.ldl": testDigest("d")},
		Manifest:    PackManifest{Format: "layerdraw-pack", FormatVersion: 1, Language: 1, Name: "net", ID: "layerdraw/network", Version: "1.0.0", Entry: "pack.ldl"},
		SourceFiles: map[string]SourceFile{
			"pack.ldl":            parse(`export * from "./modules/network.ldl"`),
			"modules/network.ldl": parse(`entity_type subnet "Subnet" {}` + "\n" + `export { subnet }`),
		},
	}
	got := Resolve(in)
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v, want none", got.Diagnostics)
	}
	requireAddress(t, got, "ldl:pack:layerdraw:network:entity-type:subnet")
}

func TestUnusedInstalledPackIsNotDependencySummary(t *testing.T) {
	t.Parallel()

	in := baseInput()
	in.Packs.Installs["unused"] = ResolvedPack{
		CanonicalID: "layerdraw/unused",
		Version:     "1.0.0",
		Digest:      testDigest("3"),
		Path:        "pack/unused",
		Entry:       "pack.ldl",
		Files:       map[string]string{"pack.ldl": testDigest("e")},
		Manifest:    PackManifest{Format: "layerdraw-pack", FormatVersion: 1, Language: 1, Name: "unused", ID: "layerdraw/unused", Version: "1.0.0", Entry: "pack.ldl"},
		SourceFiles: map[string]SourceFile{"pack.ldl": parse(`entity_type unused "Unused" {}`)},
	}
	got := Resolve(in)
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	for _, dep := range got.Dependencies {
		if dep.CanonicalID == "layerdraw/unused" {
			t.Fatalf("unused dependency leaked into summary: %+v", got.Dependencies)
		}
	}
}

func TestInvalidInputsProduceStableDiagnostics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   Input
		code string
	}{
		{
			name: "missing module",
			in: Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
				"document.ldl": parse(`import { missing } from "./missing.ldl"` + "\n" + `project p "P" {}`),
			}}},
			code: "LDL1201",
		},
		{
			name: "cycle",
			in: Input{EntryPath: "a.ldl", Project: ProjectInput{Files: map[string]SourceFile{
				"a.ldl": parse(`import { b } from "./b.ldl"` + "\n" + `project p "P" {}`),
				"b.ldl": parse(`import { a } from "./a.ldl"` + "\n" + `entity_type b "B" {}`),
			}}},
			code: "LDL1202",
		},
		{
			name: "duplicate declaration",
			in: Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
				"document.ldl": parse(`project p "P" {}` + "\n" + `entity_type a "A" {}` + "\n" + `entity_type a "A2" {}`),
			}}},
			code: "LDL1302",
		},
		{
			name: "reserved active",
			in: Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
				"document.ldl": parse(`project p "P" {}` + "\n" + `entity_type a "A" {}` + "\n" + `reserved {` + "\n  entity_types [a]\n}"),
			}}},
			code: "LDL1302",
		},
		{
			name: "bad move",
			in: Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
				"document.ldl": parse(`project p "P" {}` + "\n" + `moves {` + "\n  entity old -> missing\n}"),
			}}},
			code: "LDL1303",
		},
		{
			name: "pack forbidden kind",
			in: Input{Mode: CompilePack, EntryPath: "pack.ldl", Packs: ResolvedDependencies{Installs: map[string]ResolvedPack{
				"bad": {CanonicalID: "layerdraw/bad", Version: "1.0.0", Digest: testDigest("f"), Path: "pack/bad", Entry: "pack.ldl", Files: map[string]string{"pack.ldl": testDigest("9")}, Manifest: PackManifest{Format: "layerdraw-pack", FormatVersion: 1, Language: 1, ID: "layerdraw/bad", Name: "bad", Version: "1.0.0", Entry: "pack.ldl"}, SourceFiles: map[string]SourceFile{"pack.ldl": parse(`layers {` + "\n  app \"App\" @0\n}")}},
			}}},
			code: "LDL1102",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Resolve(tt.in)
			if !got.HasErrors {
				t.Fatalf("HasErrors = false, diagnostics=%+v", got.Diagnostics)
			}
			if !hasDiag(got, tt.code) {
				t.Fatalf("Diagnostics = %+v, want %s", got.Diagnostics, tt.code)
			}
			assertDiagnosticsSorted(t, got.Diagnostics)
		})
	}
}

func TestDeterministicAcrossMapAndDeclarationOrder(t *testing.T) {
	t.Parallel()

	a := baseInput()
	b := baseInput()
	b.Project.Files = map[string]SourceFile{
		"schema/service.ldl": b.Project.Files["schema/service.ldl"],
		"document.ldl": parse(`import { service } from "./schema/service.ldl"
import { vpc as network } from "aws.network"
import aws from "aws"
project order_platform "Order Platform" {}
layers {
  application "Application" @0
}
entities service @application {
  order_api "Order API"
}
rows order_api [environment] {
  order_api production: prod
}
export { order_api }
`),
	}
	gotA := Resolve(a)
	gotB := Resolve(b)
	if gotA.HasErrors || gotB.HasErrors {
		t.Fatalf("diagnostics A=%+v B=%+v", gotA.Diagnostics, gotB.Diagnostics)
	}
	if addresses(gotA) != addresses(gotB) {
		t.Fatalf("addresses differ\nA=%s\nB=%s", addresses(gotA), addresses(gotB))
	}
}

func TestResolveIsRaceSafeForConcurrentCallers(t *testing.T) {
	t.Parallel()

	in := baseInput()
	want := addresses(Resolve(in))
	var wg sync.WaitGroup
	errs := make(chan string, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := addresses(Resolve(in)); got != want {
				errs <- got
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("non-deterministic concurrent result: %s", err)
	}
}

func TestPathNormalizationProperties(t *testing.T) {
	t.Parallel()

	invalid := []string{"", "/abs.ldl", "../x.ldl", "a//b.ldl", "a\\b.ldl", "a/%2e%2e/b.ldl", "a/\x00.ldl"}
	for _, p := range invalid {
		if got, ok := normalizePath(p); ok {
			t.Fatalf("normalizePath(%q) = %q, true; want false", p, got)
		}
	}
	valid := []string{"document.ldl", "schema/service.ldl", "modules/network/vpc.ldl"}
	for _, p := range valid {
		if got, ok := normalizePath(p); !ok || strings.Contains(got, "./") {
			t.Fatalf("normalizePath(%q) = %q,%v", p, got, ok)
		}
	}
}

func FuzzNormalizePath(f *testing.F) {
	for _, seed := range []string{"document.ldl", "../x.ldl", "a//b.ldl", "pack/aws/pack.ldl"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		got, ok := normalizePath(raw)
		if !ok {
			return
		}
		if got == "" || strings.Contains(got, "\\") || strings.HasPrefix(got, "/") || strings.Contains(got, "//") || hasTraversalSegment(got) {
			t.Fatalf("accepted non-canonical path %q -> %q", raw, got)
		}
		got2, ok2 := normalizePath(got)
		if !ok2 || got2 != got {
			t.Fatalf("normalizePath not idempotent: %q -> %q,%v", got, got2, ok2)
		}
	})
}

func hasTraversalSegment(path string) bool {
	for _, part := range strings.Split(path, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func baseInput() Input {
	return Input{
		EntryPath: "document.ldl",
		Project: ProjectInput{Files: map[string]SourceFile{
			"document.ldl": parse(`import aws from "aws"
import { vpc as network } from "aws.network"
import { service } from "./schema/service.ldl"
project order_platform "Order Platform" {}
layers {
  application "Application" @0
}
entities service @application {
  order_api "Order API"
}
rows order_api [environment] {
  order_api production: prod
}
moves {
  entity old_order_api -> order_api
}
reserved {
  entities [legacy_order_api]
}
export { order_api }
`),
			"schema/service.ldl": parse(`entity_type service "Service" {
  columns {
    environment "Environment" string
  }
}
export { service }
`),
		}},
		Packs: ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: map[string]ResolvedPack{
			"aws": {
				CanonicalID: "layerdraw/aws-complete",
				Version:     "1.0.0",
				Digest:      testDigest("1"),
				Path:        "pack/aws",
				Entry:       "pack.ldl",
				Files:       map[string]string{"pack.ldl": testDigest("a"), "modules/network.ldl": testDigest("b")},
				Manifest:    PackManifest{Format: "layerdraw-pack", FormatVersion: 1, Language: 1, Name: "aws", ID: "layerdraw/aws-complete", Version: "1.0.0", Entry: "pack.ldl"},
				SourceFiles: map[string]SourceFile{
					"pack.ldl":            parse(`export * from "./modules/network.ldl"`),
					"modules/network.ldl": parse(`entity_type vpc "VPC" {}` + "\n" + `export { vpc }`),
				},
			},
		}},
	}
}

func testDigest(seed string) string {
	return "sha256:" + strings.Repeat(seed[:1], 64)
}

func parse(src string) SourceFile {
	return SourceFromParse(syntax.Parse([]byte(src)))
}

func requireAddress(t *testing.T, got Result, address string) {
	t.Helper()
	if !hasAddress(got, address) {
		t.Fatalf("missing address %s in %s", address, addresses(got))
	}
}

func hasAddress(got Result, address string) bool {
	return slices.ContainsFunc(got.Declarations, func(d DeclarationSymbol) bool { return d.Address == address })
}

func requireBinding(t *testing.T, got Result, source, target string) {
	t.Helper()
	for _, b := range got.Bindings {
		if b.SourceText == source && b.TargetAddress == target {
			return
		}
	}
	t.Fatalf("missing binding %s -> %s in %+v", source, target, got.Bindings)
}

func hasDiag(got Result, code string) bool {
	return slices.ContainsFunc(got.Diagnostics, func(d Diagnostic) bool { return d.Code == code })
}

func addresses(got Result) string {
	addrs := make([]string, 0, len(got.Declarations))
	for _, d := range got.Declarations {
		addrs = append(addrs, d.Address)
	}
	return strings.Join(addrs, "\n")
}

func assertDiagnosticsSorted(t *testing.T, got []Diagnostic) {
	t.Helper()
	cp := slices.Clone(got)
	sortDiagnostics(cp)
	if !reflect.DeepEqual(got, cp) {
		t.Fatalf("diagnostics not sorted\n got=%+v\nwant=%+v", got, cp)
	}
}
