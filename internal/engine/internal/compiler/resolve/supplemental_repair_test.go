// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import "testing"

func TestSupplementalRowsUseItemOwnerNotGroupHeader(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}
entity_type application_service "Application Service" {
  columns {
    environment "Environment" enum [prod, stg, dev] required
  }
}
layers {
  application "Application" @0
}
entities application_service @application {
  order_api "Order API"
}
rows application_service [environment] {
  order_api production: prod
}
export { order_api }
`)}}})
	if got.HasErrors {
		t.Fatalf("diagnostics=%+v", got.Diagnostics)
	}
	requireAddress(t, got, "ldl:project:p:entity:order_api:row:production")
}

func TestSupplementalRelationRowsUseRelationItemOwner(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}
entity_type service "Service" {
  columns {
    environment "Environment" string
  }
}
relation_type allows "Allows traffic" network {
  columns {
    protocol "Protocol" string
  }
}
layers {
  application "Application" @0
}
entities service @application {
  api_a "A"
  api_b "B"
}
relations allows {
  api_allows_b: api_a -> api_b {}
}
relation_rows allows [protocol] {
  api_allows_b production: tcp
}
export { api_allows_b }
`)}}})
	if got.HasErrors {
		t.Fatalf("diagnostics=%+v", got.Diagnostics)
	}
	requireAddress(t, got, "ldl:project:p:relation:api_allows_b:row:production")
}

func TestSupplementalCanonicalUniqueCreatesConstraintChildren(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}
entity_type application_service "Application Service" {
  columns {
    environment "Environment" string
    owner "Owner" string
  }
  unique environment_owner [environment, owner]
}
relation_type allows "Allows traffic" network {
  columns {
    protocol "Protocol" string
    port "Port" string
    cidr "CIDR" string
  }
  unique traffic_rule [protocol, port, cidr]
}
export { application_service, allows }
`)}}})
	if got.HasErrors {
		t.Fatalf("diagnostics=%+v", got.Diagnostics)
	}
	requireAddress(t, got, "ldl:project:p:entity-type:application_service:constraint:environment_owner")
	requireAddress(t, got, "ldl:project:p:relation-type:allows:constraint:traffic_rule")
}

func TestSupplementalCanonicalViewColumnAndExportChildren(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}
view production_topology "Production Topology" topology {
  source query production_scope {}
  table {
    rows entity_rows
    entity_types [application_service]
    column environment {
      source attribute environment
    }
  }
  export topology_svg svg "production-topology.svg" {
    fidelity visual_only
  }
}
export { production_topology }
`)}}})
	if got.HasErrors {
		t.Fatalf("diagnostics=%+v", got.Diagnostics)
	}
	requireAddress(t, got, "ldl:project:p:view:production_topology:table-column:environment")
	requireAddress(t, got, "ldl:project:p:view:production_topology:export:topology_svg")
}

func TestSupplementalComposedMoveClosure(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}
entity_type application_service "Application Service" {
  columns {
    environment "Environment" string
  }
}
moves {
  project old_p -> p
  entity_type old_service -> application_service
  entity_type_column application_service old_env -> environment
}
export { application_service }
`)}}})
	if got.HasErrors {
		t.Fatalf("diagnostics=%+v", got.Diagnostics)
	}
	requireClosure(t, got, "ldl:project:old_p:entity-type:old_service:column:old_env", "ldl:project:p:entity-type:application_service:column:environment")
}

func TestSupplementalPackGenericAssetPathsAndSourceModulePaths(t *testing.T) {
	t.Parallel()
	pack := baseInput().Packs.Installs["aws"]
	pack.Files["manifest.json"] = testDigest("e")
	pack.Files["assets/application-service.png"] = testDigest("a")
	pack.Files["assets/café.png"] = testDigest("c")
	got := Resolve(Input{Mode: CompilePack, RootPackID: "layerdraw/aws-complete", EntryPath: "pack.ldl", Packs: ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: map[string]ResolvedPack{"aws": pack}}})
	if got.HasErrors {
		t.Fatalf("generic asset paths rejected: %+v", got.Diagnostics)
	}

	bad := pack
	bad.SourceFiles = map[string]SourceFile{"foo.ldl/bar.ldl": parse(`entity_type bad "Bad" {}`)}
	bad.Files = map[string]string{"foo.ldl/bar.ldl": testDigest("b")}
	got = Resolve(Input{Mode: CompilePack, RootPackID: "layerdraw/aws-complete", EntryPath: "pack.ldl", Packs: ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: map[string]ResolvedPack{"aws": bad}}})
	if !got.HasErrors || !hasDiag(got, "LDL1201") {
		t.Fatalf("nested .ldl source path not rejected: %+v", got.Diagnostics)
	}
}

func TestSupplementalPortablePathAndRootSelectorValidation(t *testing.T) {
	t.Parallel()
	for _, valid := range []string{"manifest.json", "assets/application-service.png", "assets/café.png"} {
		if norm, ok := normalizePortablePath(valid); !ok || norm != valid {
			t.Fatalf("portable path %q rejected as %q,%v", valid, norm, ok)
		}
	}
	for _, invalid := range []string{"assets/../secret.png", "assets/%2e%2e/secret.png", "assets/bad:name.png", "assets/cafe\u0301.png"} {
		if norm, ok := normalizePortablePath(invalid); ok {
			t.Fatalf("invalid portable path %q normalized to %q", invalid, norm)
		}
	}
	project := Resolve(Input{EntryPath: "document.ldl", RootPackID: "layerdraw/aws-complete", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}`)}}})
	if !hasDiag(project, "LDL1201") {
		t.Fatalf("project root pack selector accepted: %+v", project.Diagnostics)
	}
	missing := baseInput()
	missing.Mode = CompilePack
	missing.EntryPath = "pack.ldl"
	missing.RootPackID = "layerdraw/missing"
	missing.Project = ProjectInput{}
	if got := Resolve(missing); !hasDiag(got, "LDL1201") {
		t.Fatalf("missing root pack selector diagnostics = %+v", got.Diagnostics)
	}
}

func TestSupplementalSemverHyphenPrereleaseAccepted(t *testing.T) {
	t.Parallel()
	if !isExactSemver("1.2.3-alpha-beta") || !isExactSemver("1.2.3-alpha-1+build-x") {
		t.Fatal("valid SemVer with hyphenated prerelease/build rejected")
	}
}

func TestSupplementalPackCompileAllowsRootWithDependencies(t *testing.T) {
	t.Parallel()
	in := baseInput()
	root := in.Packs.Installs["aws"]
	root.Manifest.Dependencies = map[string]PackDependency{"net": {ID: "layerdraw/network", Version: "1.2.3-alpha-1+build-x"}}
	root.Dependencies = map[string]string{"net": "net"}
	root.SourceFiles["pack.ldl"] = parse(`import { subnet } from "net"
export { subnet }
`)
	root.Manifest.Version = "1.2.3-alpha-beta"
	root.Version = "1.2.3-alpha-beta"
	in.Packs.Installs["aws"] = root
	in.Packs.Installs["net"] = ResolvedPack{
		CanonicalID:  "layerdraw/network",
		Version:      "1.2.3-alpha-1+build-x",
		Digest:       testDigest("d"),
		Path:         "pack/net",
		Entry:        "pack.ldl",
		Files:        map[string]string{"pack.ldl": testDigest("e")},
		Manifest:     PackManifest{Format: "layerdraw-pack", FormatVersion: 1, ID: "layerdraw/network", Name: "net", Version: "1.2.3-alpha-1+build-x", Language: 1, Entry: "pack.ldl"},
		SourceFiles:  map[string]SourceFile{"pack.ldl": parse(`entity_type subnet "Subnet" {}` + "\n" + `export { subnet }`)},
		Dependencies: map[string]string{},
	}
	got := Resolve(Input{Mode: CompilePack, RootPackID: "layerdraw/aws-complete", EntryPath: "pack.ldl", Packs: in.Packs})
	if got.HasErrors {
		t.Fatalf("pack root with dependency rejected: %+v", got.Diagnostics)
	}
	requireAddress(t, got, "ldl:pack:layerdraw:network:entity-type:subnet")
}

func TestSupplementalPackSourceFileCaseFoldCollisionRejected(t *testing.T) {
	t.Parallel()
	pack := baseInput().Packs.Installs["aws"]
	pack.SourceFiles["Modules/network.ldl"] = pack.SourceFiles["modules/network.ldl"]
	pack.Files["Modules/network.ldl"] = testDigest("u")
	got := Resolve(Input{Mode: CompilePack, RootPackID: "layerdraw/aws-complete", EntryPath: "pack.ldl", Packs: ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: map[string]ResolvedPack{"aws": pack}}})
	if !got.HasErrors || !hasDiag(got, "LDL1201") {
		t.Fatalf("pack source case-fold collision not rejected: %+v", got.Diagnostics)
	}
}

func TestSupplementalPrivateOwnerReservationAndBindingNotPublished(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`import { public_entity } from "./schema.ldl"
project p "P" {}
export { public_entity }
`),
		"schema.ldl": parse(`entity_type public_type "Public" {}
entity_type private_type "Private" {
  reserve {
    columns [old_private_col]
  }
  columns {
    current_col "Current" string
  }
}
layers {
  application "Application" @0
}
entities public_type @application {
  public_entity "Public"
}
entities private_type @application {
  private_entity "Private"
}
export { public_entity }
`),
	}}})
	if got.HasErrors {
		t.Fatalf("diagnostics=%+v", got.Diagnostics)
	}
	if hasAddress(got, "ldl:project:p:entity-type:private_type") || hasAddress(got, "ldl:project:p:entity:private_entity") {
		t.Fatalf("private declarations selected: %s", addresses(got))
	}
	if !hasCandidate(got, "ldl:project:p:entity-type:private_type") {
		t.Fatalf("private candidate missing: %+v", got.Candidates)
	}
	requireCandidateReservation(t, got, "ldl:project:p:entity-type:private_type:column:old_private_col")
	for _, res := range got.Identity.Reservations {
		if res.Address == "ldl:project:p:entity-type:private_type:column:old_private_col" {
			t.Fatalf("private owner reservation published: %+v", got.Identity.Reservations)
		}
	}
	for _, binding := range got.Bindings {
		if binding.TargetAddress == "ldl:project:p:entity-type:private_type" || binding.TargetAddress == "ldl:project:p:layer:application" {
			t.Fatalf("private/nonselected binding published: %+v", got.Bindings)
		}
	}
}

func requireCandidateReservation(t *testing.T, got Result, address string) {
	t.Helper()
	for _, res := range got.CandidateIdentity.Reservations {
		if res.Address == address {
			return
		}
	}
	t.Fatalf("candidate reservation %s missing in %+v", address, got.CandidateIdentity.Reservations)
}
