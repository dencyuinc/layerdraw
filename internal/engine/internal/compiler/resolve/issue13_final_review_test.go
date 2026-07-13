// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import (
	"strings"
	"testing"
)

func TestFinalReviewDuplicateReservationCarriesOwnerAndPreviousContext(t *testing.T) {
	t.Parallel()

	source := `project p "P" {}
entity_type e "E" {
  reserve {
    columns [old_column, old_column]
  }
}
`
	got := Resolve(Input{Mode: CompileProject, EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(source)}}})
	var duplicate *Diagnostic
	for i := range got.Diagnostics {
		if got.Diagnostics[i].Code == "LDL1302" && got.Diagnostics[i].Message == "duplicate reservation" {
			duplicate = &got.Diagnostics[i]
			break
		}
	}
	if duplicate == nil {
		t.Fatalf("duplicate reservation diagnostic missing: %+v", got.Diagnostics)
	}
	wantSubject := "ldl:project:p:entity-type:e:column:old_column"
	wantOwner := "ldl:project:p:entity-type:e"
	if duplicate.SubjectAddress != wantSubject || duplicate.OwnerAddress != wantOwner || len(duplicate.Related) != 1 {
		t.Fatalf("duplicate reservation context = %+v", *duplicate)
	}
	first := strings.Index(source, "old_column")
	second := strings.LastIndex(source, "old_column")
	previous := duplicate.Related[0]
	if duplicate.Range == nil || duplicate.Range.StartByte != second || previous.Relation != "previous" || previous.Range == nil || previous.Range.StartByte != first || previous.SubjectAddress != wantSubject || previous.OwnerAddress != wantOwner {
		t.Fatalf("duplicate/current ranges = %+v related=%+v", duplicate.Range, previous)
	}
}

func TestFinalReviewActiveReservationCarriesOwnerContext(t *testing.T) {
	t.Parallel()

	got := Resolve(Input{Mode: CompileProject, EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}
entity_type e "E" {
  reserve {
    columns [active]
  }
  columns {
    active "Active" string
  }
}
`)}}})
	var active *Diagnostic
	for i := range got.Diagnostics {
		if got.Diagnostics[i].Message == "reservation uses active identity" {
			active = &got.Diagnostics[i]
			break
		}
	}
	if active == nil || active.SubjectAddress != "ldl:project:p:entity-type:e:column:active" || active.OwnerAddress != "ldl:project:p:entity-type:e" || len(active.Related) != 1 || active.Related[0].OwnerAddress != active.OwnerAddress {
		t.Fatalf("active reservation diagnostic = %+v; all=%+v", active, got.Diagnostics)
	}
}

func TestFinalReviewReservationExtractionRequiresStructuredIdentifierValues(t *testing.T) {
	t.Parallel()

	got := Resolve(Input{Mode: CompileProject, EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}
entity_type e "E" {
  reserve {
    columns ["quoted", 1, namespace.qualified, valid_column]
    constraints ["quoted_constraint", 2, namespace.qualified_constraint, valid_constraint]
  }
}
`)}}})
	want := map[string]bool{
		"ldl:project:p:entity-type:e:column:valid_column":         false,
		"ldl:project:p:entity-type:e:constraint:valid_constraint": false,
	}
	for _, reservation := range got.Identity.Reservations {
		if _, ok := want[reservation.Address]; !ok {
			t.Fatalf("malformed reservation created identity: %+v", reservation)
		}
		want[reservation.Address] = true
	}
	for address, found := range want {
		if !found {
			t.Fatalf("valid reservation %s missing: %+v", address, got.Identity.Reservations)
		}
	}

	malformed := Resolve(Input{Mode: CompileProject, EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{"document.ldl": parse(`project p "P" {}
entity_type e "E" {
  reserve {
    columns [must_not_exist missing_comma]
  }
}
`)}}})
	if len(malformed.Identity.Reservations) != 0 {
		t.Fatalf("malformed list created reservations: %+v", malformed.Identity.Reservations)
	}
}
