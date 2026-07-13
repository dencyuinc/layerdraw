// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package graph

import (
	"math"
	"testing"
)

func TestEveryRowScalarTypeIsNormalizedByDefinitionSemantics(t *testing.T) {
	got := compileFiles(t, map[string]string{"document.ldl": `
project p "P" {}
layers {
  app "App" @0
}
entity_type record "Record" {
  representation table
  columns {
    text "Text" string min_length 1 max_length 4
    integer "Integer" integer min -2 max 2
    number "Number" number min -1 max 1
    boolean "Boolean" boolean
    choice "Choice" enum [one, two]
    date "Date" date
    datetime "Datetime" datetime
    uri "URI" string format uri
    email "Email" string format email
    hostname "Hostname" string format hostname
    ipv4 "IPv4" string format ipv4
    ipv6 "IPv6" string format ipv6
    cidr "CIDR" string format cidr
  }
}
entities record @app {
  item "Item"
}
rows record [text, integer, number, boolean, choice, date, datetime, uri, email, hostname, ipv4, ipv6, cidr] {
  item values: "e\u0301", -2, -0.0, true, one, "2024-02-29", "2026-07-14T10:00:00.120+09:00", "https://example.com/a", "a.b+tag@example.com", "EXAMPLE.COM.", "192.0.2.1", "2001:0db8::1", "10.0.0.0/24"
}
`})
	if got.HasErrors || got.Graph == nil {
		t.Fatalf("Compile() diagnostics = %+v", got.Diagnostics)
	}
	values := cellsByAddress(got.Graph.Entities[0].Rows[0])
	prefix := "ldl:project:p:entity-type:record:column:"
	if values[prefix+"text"].String != "é" || values[prefix+"integer"].Int != -2 || values[prefix+"number"].Float != 0 || math.Signbit(values[prefix+"number"].Float) ||
		!values[prefix+"boolean"].Bool || values[prefix+"choice"].String != "one" || values[prefix+"date"].String != "2024-02-29" ||
		values[prefix+"datetime"].String != "2026-07-14T01:00:00.12Z" || values[prefix+"hostname"].String != "example.com" ||
		values[prefix+"ipv6"].String != "2001:db8::1" || values[prefix+"cidr"].String != "10.0.0.0/24" {
		t.Fatalf("normalized scalars = %+v", values)
	}
	for _, scalar := range values {
		if scalar.Type == "" {
			t.Fatalf("untyped scalar = %+v", scalar)
		}
	}
}

func TestInvalidRowsFailTransactionallyWithRegisteredDiagnostics(t *testing.T) {
	tests := []struct {
		name   string
		column string
		header string
		rows   string
		unique string
		code   string
	}{
		{name: "unknown header", column: `value "Value" string`, header: "missing", rows: `item one: "x"`, code: "LDL1402"},
		{name: "duplicate header", column: `value "Value" string`, header: "value, value", rows: `item one: "x", "y"`, code: "LDL1402"},
		{name: "cell count", column: "first \"First\" string\n    second \"Second\" string", header: "first, second", rows: `item one: "x"`, code: "LDL1402"},
		{name: "required explicitly absent", column: `value "Value" string required default "fallback"`, header: "value", rows: `item one: _`, code: "LDL1402"},
		{name: "required omitted", column: "optional \"Optional\" string\n    needed \"Needed\" string required", header: "optional", rows: `item one: "x"`, code: "LDL1402"},
		{name: "integer type", column: `value "Value" integer`, header: "value", rows: `item one: "1"`, code: "LDL1401"},
		{name: "range", column: `value "Value" number min 0 max 1`, header: "value", rows: `item one: 2.0`, code: "LDL1401"},
		{name: "format", column: `value "Value" string format cidr`, header: "value", rows: `item one: "10.0.0.1/24"`, code: "LDL1401"},
		{name: "date", column: `value "Value" date`, header: "value", rows: `item one: "2023-02-29"`, code: "LDL1401"},
		{name: "enum", column: `value "Value" enum [one, two]`, header: "value", rows: `item one: three`, code: "LDL1401"},
		{name: "unique", column: `value "Value" string`, header: "value", rows: "item one: \"x\"\n  item two: \"x\"", unique: `unique by_value [value]`, code: "LDL1403"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compileFiles(t, map[string]string{"document.ldl": rowDocument(tt.column, tt.unique, tt.header, tt.rows)})
			requireFailureCode(t, got, tt.code)
		})
	}
}

func TestUniqueConstraintsAreOwnerLocalAndSkipAbsentColumns(t *testing.T) {
	got := compileFiles(t, map[string]string{"document.ldl": `
project p "P" {}
layers {
  app "App" @0
}
entity_type record "Record" {
  representation table
  columns {
    value "Value" string
    qualifier "Qualifier" string
  }
  unique pair [value, qualifier]
}
entities record @app {
  first "First"
  second "Second"
}
rows record [value, qualifier] {
  first one: "same", _
  first two: "same", "present"
  second one: "same", "present"
}
`})
	if got.HasErrors || got.Graph == nil {
		t.Fatalf("owner-local/absent unique rows rejected: %+v", got.Diagnostics)
	}
}

func TestEntityAndRelationRowGroupTypesMustMatchOwners(t *testing.T) {
	got := compileFiles(t, map[string]string{"document.ldl": `
project p "P" {}
layers {
  app "App" @0
}
entity_type first "First" {
  representation table
  columns {
    value "Value" string
  }
}
entity_type second "Second" {
  representation table
  columns {
    value "Value" string
  }
}
relation_type one "One" reference {
  from source types [first]
  to target types [first]
  label "one"
  columns {
    value "Value" string
  }
}
relation_type two "Two" reference {
  from source types [first]
  to target types [first]
  label "two"
  columns {
    value "Value" string
  }
}
entities first @app {
  a "A"
  b "B"
}
rows second [value] {
  a bad: "x"
}
relations one {
  edge: a -> b
}
relation_rows two [value] {
  edge bad: "x"
}
`})
	requireFailureCode(t, got, "LDL1402")
	if countCode(got, "LDL1402") != 2 {
		t.Fatalf("row type diagnostics = %+v", got.Diagnostics)
	}
}

func rowDocument(column, unique, header, rows string) string {
	return `
project p "P" {}
layers {
  app "App" @0
}
entity_type record "Record" {
  representation table
  columns {
    ` + column + `
  }
  ` + unique + `
}
entities record @app {
  item "Item"
}
rows record [` + header + `] {
  ` + rows + `
}
`
}
