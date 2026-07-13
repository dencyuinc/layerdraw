// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"strings"
	"testing"
)

func TestFinalAcceptanceWrongScopeReservationCategoryIsDiagnosedOnce(t *testing.T) {
	t.Parallel()

	source := `project p "P" {}
reserved {
  columns [first, second]
}
entity_type e "E" {
  reserve {
    parameters []
  }
}
`
	got := Resolve(Input{Mode: CompileProject, EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(source)}}})
	wrongScope := diagnosticsWithCode(got.Diagnostics, "LDL1302")
	if len(wrongScope) != 2 {
		t.Fatalf("wrong-scope diagnostics = %+v, want 2", got.Diagnostics)
	}
	for index, token := range []string{"columns", "parameters"} {
		start := strings.Index(source, token)
		if wrongScope[index].Range == nil || wrongScope[index].Range.StartByte != start || wrongScope[index].Range.EndByte != start+len(token) {
			t.Fatalf("wrong-scope range for %q = %+v", token, wrongScope[index].Range)
		}
	}
	if len(got.CandidateIdentity.Reservations) != 0 || len(got.Identity.Reservations) != 0 {
		t.Fatalf("wrong-scope reservations leaked: candidate=%+v selected=%+v", got.CandidateIdentity.Reservations, got.Identity.Reservations)
	}

	malformedSource := `project p "P" {}
reserved {
  columns not_a_list
}
`
	malformed := Resolve(Input{Mode: CompileProject, EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(malformedSource)}}})
	if len(diagnosticsWithCode(malformed.Diagnostics, "LDL1102")) != 1 || len(diagnosticsWithCode(malformed.Diagnostics, "LDL1302")) != 0 {
		t.Fatalf("malformed wrong-scope diagnostics = %+v", malformed.Diagnostics)
	}
}

func TestFinalAcceptancePackWrongScopeEmptyCategoryIsDiagnosed(t *testing.T) {
	t.Parallel()

	pack := baseInput().Packs.Installs["aws"]
	packSource := `reserved {
  layers []
}
`
	pack.Files = map[string]string{"pack.ldl": testDigest("a")}
	pack.SourceFiles = map[string]SourceFile{"pack.ldl": parse(packSource)}
	got := Resolve(Input{Mode: CompilePack, RootPackID: pack.CanonicalID, EntryPath: pack.Entry, Packs: ResolvedDependencies{
		Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: map[string]ResolvedPack{"aws": pack},
	}})
	if len(diagnosticsWithCode(got.Diagnostics, "LDL1302")) != 1 || len(diagnosticsWithCode(got.Diagnostics, "LDL1102")) != 0 || len(got.CandidateIdentity.Reservations) != 0 {
		t.Fatalf("pack wrong-scope result = diagnostics=%+v identity=%+v", got.Diagnostics, got.CandidateIdentity)
	}
}

func TestFinalAcceptanceOwnerExtractionUsesDirectMembersOnly(t *testing.T) {
	t.Parallel()

	source := `project p "P" {}
entity_type e "E" {
  columns {
    direct "Direct" string
  }
  unique direct_unique [direct]
  reserve {
    columns [old_direct]
  }
  wrapper {
    columns {
      ghost "Ghost" string
    }
    unique ghost_unique [ghost]
    reserve {
      columns [old_ghost]
    }
  }
}
layers {
  app "App" @0
}
entities e @app {
  item "Item" {
    reserve_rows [old_row]
    wrapper {
      reserve_rows [ghost_row]
    }
  }
}
`
	got := Resolve(Input{Mode: CompileProject, EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(source)}}})
	if got.HasErrors {
		t.Fatalf("resolver diagnostics = %+v", got.Diagnostics)
	}
	declarationIDs := map[string]bool{}
	for _, declaration := range got.Candidates {
		declarationIDs[string(declaration.Kind)+":"+declaration.ID] = true
	}
	for _, expected := range []string{"column:direct", "constraint:direct_unique"} {
		if !declarationIDs[expected] {
			t.Fatalf("missing direct declaration %q in %+v", expected, got.Candidates)
		}
	}
	for _, forbidden := range []string{"column:ghost", "constraint:ghost_unique"} {
		if declarationIDs[forbidden] {
			t.Fatalf("nested declaration leaked: %q in %+v", forbidden, got.Candidates)
		}
	}
	reservationIDs := map[string]bool{}
	for _, reservation := range got.CandidateIdentity.Reservations {
		reservationIDs[reservation.ID] = true
	}
	for _, expected := range []string{"old_direct", "old_row"} {
		if !reservationIDs[expected] {
			t.Fatalf("missing direct reservation %q in %+v", expected, got.CandidateIdentity.Reservations)
		}
	}
	for _, forbidden := range []string{"old_ghost", "ghost_row"} {
		if reservationIDs[forbidden] {
			t.Fatalf("nested reservation leaked: %q in %+v", forbidden, got.CandidateIdentity.Reservations)
		}
	}
}

func diagnosticsWithCode(diagnostics []Diagnostic, code string) []Diagnostic {
	var out []Diagnostic
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			out = append(out, diagnostic)
		}
	}
	return out
}
