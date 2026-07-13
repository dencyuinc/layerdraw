// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import "testing"

func TestFinalFollowupRelationRowsUseRelationOwnerWhenIDsOverlap(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}
entity_type thing "Thing" {
  columns {
    name "Name" string
  }
}
relation_type link "Link" dependency {
  columns {
    weight "Weight" integer
  }
}
layers {
  app "App" @0
}
entities thing @app {
  same "Same"
  other "Other"
}
relations link {
  same: same -> other {}
}
rows thing [name] {
  same erow: "entity"
}
relation_rows link [weight] {
  same rrow: 1
}
`)}}})
	if got.HasErrors {
		t.Fatalf("diagnostics=%+v", got.Diagnostics)
	}
	requireAddress(t, got, "ldl:project:p:entity:same:row:erow")
	requireAddress(t, got, "ldl:project:p:relation:same:row:rrow")
	if hasAddress(got, "ldl:project:p:entity:same:row:rrow") {
		t.Fatalf("relation row was published under entity owner: %s", addresses(got))
	}
}

func TestFinalFollowupChildMoveOwnerCanLiveInImportedModule(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`import { service } from "./schema/service.ldl"
project p "P" {}
moves {
  entity_type_column service old_environment -> environment
}
export { service }
`),
		"schema/service.ldl": parse(`entity_type service "Service" {
  columns {
    environment "Environment" string
  }
}
export { service }
`),
	}}})
	if got.HasErrors {
		t.Fatalf("diagnostics=%+v", got.Diagnostics)
	}
	requireMove(t, got, "ldl:project:p:entity-type:service:column:old_environment", "ldl:project:p:entity-type:service:column:environment")
}

func TestFinalFollowupCrossModuleReservationMoveCollision(t *testing.T) {
	t.Parallel()
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`import { service } from "./schema/service.ldl"
project p "P" {}
moves {
  entity_type_column service old_environment -> environment
}
export { service }
`),
		"schema/service.ldl": parse(`entity_type service "Service" {
  reserve {
    columns [old_environment]
  }
  columns {
    environment "Environment" string
  }
}
export { service }
`),
	}}})
	if !hasDiag(got, "LDL1302") || hasDiag(got, "LDL1303") {
		t.Fatalf("diagnostics=%+v", got.Diagnostics)
	}
	if len(got.Diagnostics) == 0 || len(got.Diagnostics[0].Related) == 0 {
		t.Fatalf("expected related reservation range in diagnostics=%+v", got.Diagnostics)
	}
}

func TestFinalFollowupPortablePathRejectsDecodedAliasesAndNonNFC(t *testing.T) {
	t.Parallel()
	for _, valid := range []string{"assets/safe%20alias.png", "assets/name%40tag.png"} {
		if norm, ok := normalizePortablePath(valid); !ok || norm != valid {
			t.Fatalf("safe encoded path %q rejected as %q,%v", valid, norm, ok)
		}
	}
	for _, invalid := range []string{"assets/foo%2Fbar.png", "assets/cafe%CC%81.png", "assets/%2e%2e/secret.png", "assets/back\\slash.png", "assets/\x00bad.png"} {
		if norm, ok := normalizePortablePath(invalid); ok {
			t.Fatalf("unsafe encoded path %q normalized to %q", invalid, norm)
		}
	}

	pack := baseInput().Packs.Installs["aws"]
	pack.Files["assets/foo%2Fbar.png"] = testDigest("b")
	got := Resolve(Input{Mode: CompilePack, RootPackID: "layerdraw/aws-complete", EntryPath: "pack.ldl", Packs: ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: map[string]ResolvedPack{"aws": pack}}})
	if !hasDiag(got, "LDL1203") {
		t.Fatalf("pack file encoded separator accepted: %+v", got.Diagnostics)
	}
}

func TestFinalFollowupPackCompilePublishesRootWithoutExports(t *testing.T) {
	t.Parallel()
	pack := baseInput().Packs.Installs["aws"]
	pack.SourceFiles = map[string]SourceFile{"pack.ldl": parse(`entity_type private_helper "Private" {}`)}
	pack.Files = map[string]string{"pack.ldl": testDigest("a")}
	got := Resolve(Input{Mode: CompilePack, RootPackID: "layerdraw/aws-complete", EntryPath: "pack.ldl", Packs: ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: map[string]ResolvedPack{"aws": pack}}})
	if got.HasErrors {
		t.Fatalf("diagnostics=%+v", got.Diagnostics)
	}
	requireAddress(t, got, "ldl:pack:layerdraw:aws-complete")
	if hasAddress(got, "ldl:pack:layerdraw:aws-complete:entity-type:private_helper") {
		t.Fatalf("private pack declaration selected: %s", addresses(got))
	}
	requireDependency(t, got, "layerdraw/aws-complete")
}
