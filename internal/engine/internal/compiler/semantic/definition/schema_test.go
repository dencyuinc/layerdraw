// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package definition

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
)

func TestColumnScalarNormalization(t *testing.T) {
	t.Parallel()

	got := compileProject(t, map[string]string{"document.ldl": `
project p "Project" {}
entity_type scalar "Scalar" {
  representation shape rect
  columns {
    date_value "Date" date default "2026-02-28"
    datetime_value "Datetime" datetime default "2026-07-14T10:00:00.120+09:00"
    integer_value "Integer" integer default 9007199254740991 min -9007199254740991 max 9007199254740991
    number_value "Number" number default -0
    text_value "Text" string default "\u0065\u0301" min_length 1 max_length 1
    enum_value "Enum" enum [z, a] reserve_values [legacy_z, legacy_a] required default a
  }
}
`})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	columns := got.EntityTypes[0].Columns
	if columns[0].Default.Type != ScalarDate || columns[0].Default.String != "2026-02-28" {
		t.Fatalf("date = %+v", columns[0].Default)
	}
	if columns[1].Default.Type != ScalarDatetime || columns[1].Default.String != "2026-07-14T01:00:00.12Z" {
		t.Fatalf("datetime = %+v", columns[1].Default)
	}
	if columns[2].Default.Int != maxJSONSafeInteger || columns[3].Default.Float != 0 {
		t.Fatalf("numeric defaults = %+v %+v", columns[2].Default, columns[3].Default)
	}
	if columns[4].Default.String != "é" {
		t.Fatalf("NFC string = %+v", columns[4].Default)
	}
	if !reflect.DeepEqual(columns[5].EnumValues, []string{"z", "a"}) || !reflect.DeepEqual(columns[5].ReservedEnumValues, []string{"legacy_a", "legacy_z"}) {
		t.Fatalf("enum ordering = %+v", columns[5])
	}
}

func TestColumnScalarValidationTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		code string
	}{
		{name: "integer outside JSON safe range", line: `value "Value" integer default 9007199254740992`, code: "LDL1401"},
		{name: "invalid Gregorian date", line: `value "Value" date default "2026-02-30"`, code: "LDL1401"},
		{name: "datetime requires millisecond precision", line: `value "Value" datetime default "2026-07-14T10:00:00.1234Z"`, code: "LDL1401"},
		{name: "default below minimum length", line: `value "Value" string default "a" min_length 2`, code: "LDL1401"},
		{name: "default below numeric minimum", line: `value "Value" integer default 5 min 6`, code: "LDL1401"},
		{name: "inverted numeric range", line: `value "Value" number min 2 max 1`, code: "LDL1401"},
		{name: "active reserved overlap", line: `value "Value" enum [active] reserve_values [active]`, code: "LDL1401"},
		{name: "reserve values on string", line: `value "Value" string reserve_values [old]`, code: "LDL1401"},
		{name: "fractional integer bound", line: `value "Value" integer min 1.5`, code: "LDL1401"},
		{name: "unknown string format", line: `value "Value" string format unknown`, code: "LDL1401"},
		{name: "duplicate modifier", line: `value "Value" string required required`, code: "LDL1102"},
		{name: "empty enum", line: `value "Value" enum []`, code: "LDL1401"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compileProject(t, map[string]string{"document.ldl": fmt.Sprintf(`
project p "Project" {}
entity_type scalar "Scalar" {
  representation shape rect
  columns {
    %s
  }
}
`, tt.line)})
			if !hasDiagnosticCode(got.Diagnostics, tt.code) {
				t.Fatalf("Diagnostics = %+v, want %s", got.Diagnostics, tt.code)
			}
		})
	}
}

func TestStringFormatNormalizationTable(t *testing.T) {
	t.Parallel()

	valid := []struct {
		format string
		input  string
		want   string
	}{
		{format: "uri", input: "https://example.com/a", want: "https://example.com/a"},
		{format: "email", input: "user@example.com", want: "user@example.com"},
		{format: "hostname", input: "API.Example.COM.", want: "api.example.com"},
		{format: "ipv4", input: "192.0.2.1", want: "192.0.2.1"},
		{format: "ipv6", input: "2001:0db8::1", want: "2001:db8::1"},
		{format: "cidr", input: "192.0.2.0/24", want: "192.0.2.0/24"},
	}
	for _, tt := range valid {
		t.Run("valid_"+tt.format, func(t *testing.T) {
			got := compileFormattedDefault(t, tt.format, tt.input)
			if got.HasErrors || got.EntityTypes[0].Columns[0].Default.String != tt.want {
				t.Fatalf("result = %+v diagnostics = %+v", got.EntityTypes, got.Diagnostics)
			}
		})
	}

	invalid := []struct {
		format string
		input  string
	}{
		{format: "uri", input: "/relative"},
		{format: "email", input: `"quoted"@example.com`},
		{format: "hostname", input: "-bad.example"},
		{format: "hostname", input: "éxample.com"},
		{format: "ipv4", input: "192.168.001.1"},
		{format: "ipv6", input: "192.0.2.1"},
		{format: "ipv6", input: "fe80::1%eth0"},
		{format: "cidr", input: "192.0.2.1/24"},
	}
	for _, tt := range invalid {
		t.Run("invalid_"+tt.format+"_"+tt.input, func(t *testing.T) {
			got := compileFormattedDefault(t, tt.format, tt.input)
			if !hasDiagnosticCode(got.Diagnostics, "LDL1401") {
				t.Fatalf("Diagnostics = %+v", got.Diagnostics)
			}
		})
	}
}

func TestCanonicalCommonFieldsAndDiagnosticRange(t *testing.T) {
	t.Parallel()

	valid := compileProject(t, map[string]string{"document.ldl": `
project p "Project" {
  description "Cafe\u0301"
  tags ["z", "e\u0301"]
  annotations { "team/name": "Cafe\u0301", owner: "platform" }
}
`})
	if valid.HasErrors {
		t.Fatalf("Diagnostics = %+v", valid.Diagnostics)
	}
	if *valid.Project.Description != "Café" || !reflect.DeepEqual(valid.Project.Tags, []string{"z", "é"}) || valid.Project.Annotations["team/name"] != "Café" {
		t.Fatalf("common = %+v", valid.Project.Common)
	}

	source := "project p \"Project\" {\n  tags [dup, dup]\n}\n"
	invalid := compileProject(t, map[string]string{"document.ldl": source})
	var duplicate *resolve.Diagnostic
	for i := range invalid.Diagnostics {
		if invalid.Diagnostics[i].Message == "duplicate tag" {
			duplicate = &invalid.Diagnostics[i]
			break
		}
	}
	if duplicate == nil {
		t.Fatalf("Diagnostics = %+v", invalid.Diagnostics)
	}
	first := strings.Index(source, "dup")
	second := strings.LastIndex(source, "dup")
	if duplicate.Code != "LDL1102" || duplicate.MessageKey != "unknown_or_duplicate_schema_member" || duplicate.Range.StartByte != second || duplicate.Range.EndByte != second+3 || duplicate.SubjectAddress != "ldl:project:p" || duplicate.OwnerAddress != "" {
		t.Fatalf("duplicate diagnostic = %+v", duplicate)
	}
	if len(duplicate.Related) != 1 || duplicate.Related[0].Range.StartByte != first || duplicate.Related[0].Relation != "previous" {
		t.Fatalf("related = %+v", duplicate.Related)
	}
}

func TestAuthoredAssetOriginResolution(t *testing.T) {
	t.Parallel()

	got := compileProject(t, map[string]string{
		"document.ldl": `import { server } from "./types/server.ldl"
project p "Project" {}
`,
		"types/server.ldl": `entity_type server "Server" {
  image "../assets/server.png"
  representation shape rect
}
export { server }
`,
	})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	asset := got.EntityTypes[0].Image
	if asset == nil || asset.AuthoredPath != "../assets/server.png" || asset.Locator != "assets/server.png" || asset.ModulePath != "types/server.ldl" || asset.SourceRange == nil {
		t.Fatalf("asset = %+v", asset)
	}

	for _, locator := range []string{"../escape.png", "https://example.com/a.png", "data:image/png;base64,x", `C:\\a.png`} {
		invalid := compileProject(t, map[string]string{"document.ldl": fmt.Sprintf(`project p "Project" {}
entity_type e "E" {
  image %q
  representation shape rect
}
`, locator)})
		if !hasDiagnosticCode(invalid.Diagnostics, "LDL1201") {
			t.Fatalf("locator %q diagnostics = %+v", locator, invalid.Diagnostics)
		}
	}
}

func TestProjectRootSurvivesSelectedPackRoot(t *testing.T) {
	t.Parallel()

	pack := definitionTestPack(`entity_type external "External" {
  representation shape rect
}
export { external }
`)
	resolved := resolve.Resolve(resolve.Input{
		Mode:      resolve.CompileProject,
		EntryPath: "document.ldl",
		Project: resolve.ProjectInput{Files: map[string]resolve.SourceFile{
			"document.ldl": parse(`import { external } from "schema"
project p "Project" {}
`),
		}},
		Packs: resolve.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: map[string]resolve.ResolvedPack{"schema": pack}},
	})
	got := Compile(Input{Resolve: resolved})
	if got.HasErrors || got.Root.Mode != resolve.CompileProject || got.Root.Address != "ldl:project:p" || got.Project == nil || got.Pack != nil || len(got.Dependencies) != 1 {
		t.Fatalf("project root result = %+v diagnostics = %+v", got, got.Diagnostics)
	}
}

func TestEndpointResolutionModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		document  string
		modules   map[string]string
		wantError bool
		wantCode  string
	}{
		{name: "local", document: endpointDocument("server", "server", ""), modules: map[string]string{"schema.ldl": localEntitySource()}},
		{name: "named import", document: endpointDocument("imported", "imported", `import { server as imported } from "./schema.ldl"`), modules: map[string]string{"schema.ldl": exportedEntitySource()}},
		{name: "namespace import", document: endpointDocument("ns.server", "ns.server", `import ns from "./schema.ldl"`), modules: map[string]string{"schema.ldl": exportedEntitySource()}},
		{name: "unknown", document: endpointDocument("missing", "missing", ""), wantError: true, wantCode: "LDL1301"},
		{name: "wrong kind", document: endpointDocument("app", "app", ""), wantError: true, wantCode: "LDL1301"},
		{name: "ambiguous", document: endpointDocument("server", "server", "import { server } from \"./a.ldl\"\nimport { server } from \"./b.ldl\""), modules: map[string]string{
			"a.ldl": "entity_type a \"A\" {\n  representation shape rect\n}\nexport { a as server }\n",
			"b.ldl": "entity_type b \"B\" {\n  representation shape rect\n}\nexport { b as server }\n",
		}, wantError: true, wantCode: "LDL1302"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string]string{"document.ldl": tt.document}
			for path, source := range tt.modules {
				files[path] = source
			}
			got := compileProject(t, files)
			if tt.wantError {
				if !hasDiagnosticCode(got.Diagnostics, tt.wantCode) {
					t.Fatalf("Diagnostics = %+v", got.Diagnostics)
				}
				return
			}
			if got.HasErrors || len(got.RelationTypes) != 1 || len(got.RelationTypes[0].From.EntityTypeAddresses) != 1 {
				t.Fatalf("result = %+v diagnostics = %+v", got.RelationTypes, got.Diagnostics)
			}
		})
	}
}

func TestProjectionOverlayAndClosedSchemas(t *testing.T) {
	t.Parallel()

	valid := compileProject(t, map[string]string{"document.ldl": relationFixture(`
  reverse "is related by"
  projection context {
    include_attribute_rows true
  }
  projection matrix {
    row_endpoint from
    column_endpoint to
    include_relation_rows false
  }
`)})
	if valid.HasErrors {
		t.Fatalf("Diagnostics = %+v", valid.Diagnostics)
	}
	projection := valid.RelationTypes[0].Projections
	if projection.Context.FactTemplate != "{from.display_name} relates {to.display_name}" || projection.Context.ReverseFactTemplate == nil || *projection.Context.ReverseFactTemplate != "{to.display_name} is related by {from.display_name}" || !projection.Context.IncludeAttributeRows || projection.Matrix == nil {
		t.Fatalf("projection = %+v", projection)
	}

	invalid := []string{
		`projection matrix {
    row_endpoint from
  }`,
		`projection tree {
    parent_endpoint from
    child_endpoint to
    unknown true
  }`,
		`projection flow {
    source_endpoint from
    target_endpoint to
    connector_kind data
    branch_value_column missing
  }`,
		`render nested {
    unknown true
  }`,
		`projection context {
    fact_template "{from.id"
  }`,
		`projection composed {
    mode overlay
    overlay_endpoint from
    target_endpoint to
    child_endpoint from
  }`,
	}
	for _, fragment := range invalid {
		got := compileProject(t, map[string]string{"document.ldl": relationFixture("\n  " + fragment + "\n")})
		if !got.HasErrors {
			t.Fatalf("fragment accepted: %s\nresult=%+v", fragment, got.RelationTypes)
		}
	}
}

func TestCompileDeterministicAcrossResolvePermutations(t *testing.T) {
	t.Parallel()

	input := resolve.Input{Mode: resolve.CompileProject, EntryPath: "document.ldl", Project: resolve.ProjectInput{Files: map[string]resolve.SourceFile{
		"document.ldl": parse(`project p "Project" {}
entity_type z "Z" {
  representation shape rect
}
entity_type a "A" {
  representation shape rect
}
reference z_ref <<-TEXT
  Z
TEXT
reference a_ref <<-TEXT
  A
TEXT
`),
	}}}
	resolved := resolve.Resolve(input)
	want := Compile(Input{Resolve: resolved})
	reorderedSource := resolve.Resolve(resolve.Input{Mode: resolve.CompileProject, EntryPath: "document.ldl", Project: resolve.ProjectInput{Files: map[string]resolve.SourceFile{
		"document.ldl": parse(`project p "Project" {}
reference a_ref <<-TEXT
  A
TEXT
entity_type a "A" {
  representation shape rect
}
reference z_ref <<-TEXT
  Z
TEXT
entity_type z "Z" {
  representation shape rect
}
`),
	}}})
	if sourceOrder := Compile(Input{Resolve: reorderedSource}); !reflect.DeepEqual(sourceOrder, want) {
		t.Fatalf("authored declaration order changed semantics\nwant=%+v\ngot=%+v", want, sourceOrder)
	}
	slices.Reverse(resolved.Declarations)
	slices.Reverse(resolved.DeclarationSources)
	slices.Reverse(resolved.Bindings)
	slices.Reverse(resolved.Modules)
	slices.Reverse(resolved.Diagnostics)
	got := Compile(Input{Resolve: resolved})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("permuted result differs\nwant=%+v\ngot=%+v", want, got)
	}
	if got.EntityTypes[0].ID != "a" || got.EntityTypes[1].ID != "z" || got.References[0].ID != "a_ref" {
		t.Fatalf("structured order = entities %+v references %+v", got.EntityTypes, got.References)
	}
}

func compileFormattedDefault(t *testing.T, format, value string) Result {
	t.Helper()
	return compileProject(t, map[string]string{"document.ldl": fmt.Sprintf(`
project p "Project" {}
entity_type formatted "Formatted" {
  representation shape rect
  columns {
    value "Value" string default %q format %s
  }
}
`, value, format)})
}

func hasDiagnosticCode(diagnostics []resolve.Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func definitionTestPack(source string) resolve.ResolvedPack {
	return resolve.ResolvedPack{
		CanonicalID: "layerdraw/schema",
		Version:     "1.0.0",
		Digest:      "sha256:" + strings.Repeat("a", 64),
		Path:        "pack/schema",
		Entry:       "pack.ldl",
		Files:       map[string]string{"pack.ldl": "sha256:" + strings.Repeat("b", 64)},
		Manifest:    resolve.PackManifest{Format: "layerdraw-pack", FormatVersion: 1, ID: "layerdraw/schema", Name: "schema", Version: "1.0.0", Language: 1, Entry: "pack.ldl"},
		SourceFiles: map[string]resolve.SourceFile{"pack.ldl": parse(source)},
	}
}

func endpointDocument(from, to, imports string) string {
	local := ""
	if imports == "" && from == "server" {
		local = `entity_type server "Server" {
  representation shape rect
}`
	}
	return fmt.Sprintf(`%s
project p "Project" {}
layers {
  app "Application" @0
}
%s
relation_type relation "Relation" dependency {
  from source types [%s]
  to target types [%s]
  label "relates"
}
`, imports, local, from, to)
}

func localEntitySource() string {
	return ""
}

func exportedEntitySource() string {
	return `entity_type server "Server" {
  representation shape rect
}
export { server }
`
}

func relationFixture(extra string) string {
	return fmt.Sprintf(`project p "Project" {}
entity_type endpoint "Endpoint" {
  representation shape rect
}
relation_type relation "Relation" dependency {
  from source types [endpoint]
  to target types [endpoint]
  label "relates"%s
}
`, extra)
}
