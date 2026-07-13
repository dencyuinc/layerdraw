// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package resolve

import "testing"

func TestUnselectedPrivateRelationReferencesDoNotRejectEffectiveDocument(t *testing.T) {
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`import { record } from "./schema.ldl"
project p "P" {}`),
		"schema.ldl": parse(`entity_type record "Record" {
  representation shape rect
}
relations missing_type {
  hidden: missing_from -> missing_to
}
export { record }`),
	}}})
	if got.HasErrors || len(got.Diagnostics) != 0 {
		t.Fatalf("unselected relation references rejected the document: %+v", got.Diagnostics)
	}
	if hasAddress(got, "ldl:project:p:relation:hidden") {
		t.Fatalf("private relation entered the selected closure: %v", addresses(got))
	}
}

func TestIgnoredEmptyNonEntryGroupDoesNotPublishBinding(t *testing.T) {
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`import { record } from "./schema.ldl"
project p "P" {}`),
		"schema.ldl": parse(`entity_type record "Record" {
  representation table
  columns {
    value "Value" string
  }
}
rows record [value] {}
export { record }`),
	}}})
	if got.HasErrors {
		t.Fatalf("diagnostics=%+v", got.Diagnostics)
	}
	for _, binding := range got.Bindings {
		if binding.Module.Path == "schema.ldl" && binding.Via == "group-header" {
			t.Fatalf("ignored group binding was published: %+v", binding)
		}
	}
}

func TestEmptyEntryGroupPublishesItsSelectedHeaderBinding(t *testing.T) {
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`project p "P" {}
entity_type record "Record" {
  representation table
  columns {
    value "Value" string
  }
}
rows record [value] {}`),
	}}})
	if got.HasErrors {
		t.Fatalf("diagnostics=%+v", got.Diagnostics)
	}
	for _, binding := range got.Bindings {
		if binding.Via == "group-header" && binding.TargetAddress == "ldl:project:p:entity-type:record" {
			return
		}
	}
	t.Fatalf("entry group header binding missing: %+v", got.Bindings)
}

func TestUnselectedPrivateRowOwnerDoesNotRejectEffectiveDocument(t *testing.T) {
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`import { record } from "./schema.ldl"
project p "P" {}`),
		"schema.ldl": parse(`entity_type record "Record" {
  representation table
  columns {
    value "Value" string
  }
}
rows record [value] {
  missing row_one: "x"
}
export { record }`),
	}}})
	if got.HasErrors || len(got.Diagnostics) != 0 {
		t.Fatalf("unselected row owner rejected the document: %+v", got.Diagnostics)
	}
}

func TestEntryRowWithMissingOwnerIsRejectedAfterSelection(t *testing.T) {
	got := Resolve(Input{EntryPath: "document.ldl", Project: ProjectInput{Files: map[string]SourceFile{
		"document.ldl": parse(`project p "P" {}
entity_type record "Record" {
  representation table
  columns {
    value "Value" string
  }
}
rows record [value] {
  missing row_one: "x"
}`),
	}}})
	if !got.HasErrors || diagnosticCodeCount(got.Diagnostics, "LDL1301") != 1 {
		t.Fatalf("missing entry row owner was not rejected exactly once: %+v", got.Diagnostics)
	}
}
