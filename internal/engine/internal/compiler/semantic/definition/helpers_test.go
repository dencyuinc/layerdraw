// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package definition

import (
	"reflect"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func TestDefinitionHelperBranches(t *testing.T) {
	src := testSource()
	c := &compiler{}
	c.diagRelated("LDL1102", "unknown_or_duplicate_schema_member", src, syntax.Span{Start: 3, End: 4}, "dup", "subject", "owner", syntax.Span{Start: 1, End: 2})
	if len(c.diagnostics) != 1 || len(c.diagnostics[0].Related) != 1 {
		t.Fatalf("diagRelated = %+v", c.diagnostics)
	}
	if _, ok := resolveAssetLocator("document.ldl", ""); ok {
		t.Fatal("empty asset locator accepted")
	}
	if _, ok := resolveAssetLocator("document.ldl", "https://example.com/a.png"); ok {
		t.Fatal("remote asset locator accepted")
	}
	if _, ok := resolveAssetLocator("document.ldl", "../a.png"); ok {
		t.Fatal("invalid asset locator accepted")
	}
	if _, ok := resolveAssetLocator("document.ldl", "assets/a.png"); !ok {
		t.Fatal("valid asset locator rejected")
	}
	if got := sourceOrigin(resolve.Origin{Kind: resolve.OriginPack, Publisher: "pub", PackName: "pack"}); got.PackAddress != "ldl:pack:pub:pack" {
		t.Fatalf("pack source origin = %+v", got)
	}
	if text := heredocText("short"); text != "short" {
		t.Fatalf("short heredoc changed: %q", text)
	}
	if _, ok := (value{raw: "1e999", kind: syntax.TokenNumber}).number(); ok {
		t.Fatal("non-finite number accepted")
	}
	if got := childKey(nil, resolve.KindColumn, "c"); got != "|column|c" {
		t.Fatalf("nil child key = %q", got)
	}
}

func TestDefinitionInvalidHelperBranches(t *testing.T) {
	src := testSource()
	c := &compiler{}
	b := body{items: []item{
		{name: "flag", args: []value{{raw: "not_bool", kind: syntax.TokenIdentifier, span: syntax.Span{Start: 2, End: 3}}}, span: syntax.Span{Start: 2, End: 3}},
		{name: "enum", args: []value{{raw: "bad", kind: syntax.TokenIdentifier, span: syntax.Span{Start: 4, End: 5}}}, span: syntax.Span{Start: 4, End: 5}},
		{name: "int", args: []value{{raw: "bad", kind: syntax.TokenIdentifier, span: syntax.Span{Start: 6, End: 7}}}, span: syntax.Span{Start: 6, End: 7}},
		{name: "positive", args: []value{{raw: "0", kind: syntax.TokenInteger, span: syntax.Span{Start: 8, End: 9}}}, span: syntax.Span{Start: 8, End: 9}},
		{name: "atom", span: syntax.Span{Start: 10, End: 11}},
	}}
	if c.optionalBoolDefault(b, "flag", true, src, "subject", "", "LDL1504", "invalid_projection_contract") != true ||
		c.optionalEnumDefault(b, "enum", "fallback", set("good"), src, "subject", "", "LDL1504", "invalid_projection_contract") != "fallback" ||
		c.optionalIntDefault(b, "int", 4, src, "subject") != 4 ||
		c.optionalPositiveIntDefault(b, "positive", 4, src, "subject") != 4 ||
		c.optionalAtomDefault(b, "atom", "fallback", src, "subject") != "fallback" {
		t.Fatal("invalid helpers did not preserve documented defaults")
	}
	if len(c.diagnostics) != 5 {
		t.Fatalf("helper diagnostics = %+v", c.diagnostics)
	}
	wantMessages := []string{"expected boolean", "invalid enum", "expected integer", "expected positive integer", "expected atom"}
	for i, want := range wantMessages {
		if c.diagnostics[i].Message != want || c.diagnostics[i].Code != "LDL1504" {
			t.Fatalf("diagnostic[%d] = %+v, want %q", i, c.diagnostics[i], want)
		}
	}
}

func TestDefinitionComposedValidationBranches(t *testing.T) {
	src := testSource()
	c := &compiler{}
	from, to := ProjectionEndpointFrom, ProjectionEndpointTo
	c.validateComposed(ComposedProjection{Mode: "overlay", OverlayEndpoint: &from, TargetEndpoint: &from}, body{}, src, syntax.Span{Start: 1, End: 2}, "subject")
	c.validateComposed(ComposedProjection{Mode: "badge", BadgeEndpoint: &from, TargetEndpoint: &from}, body{}, src, syntax.Span{Start: 2, End: 3}, "subject")
	c.validateComposed(ComposedProjection{Mode: "edge", ParentEndpoint: &to}, body{}, src, syntax.Span{Start: 3, End: 4}, "subject")
	if len(c.diagnostics) != 3 {
		t.Fatalf("validateComposed diagnostics = %+v", c.diagnostics)
	}
	c.diagnostics = nil
	c.validateComposed(ComposedProjection{Mode: "overlay", OverlayEndpoint: &from, TargetEndpoint: &to}, body{}, src, syntax.Span{}, "subject")
	c.validateComposed(ComposedProjection{Mode: "badge", BadgeEndpoint: &from, TargetEndpoint: &to}, body{}, src, syntax.Span{}, "subject")
	if len(c.diagnostics) != 0 {
		t.Fatalf("valid composed modes diagnosed: %+v", c.diagnostics)
	}
}

func TestDefinitionSortDiagnosticsBranches(t *testing.T) {
	ds := []resolve.Diagnostic{
		{Code: "LDL1504", MessageKey: "b", Range: &resolve.SourceRange{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, ModulePath: "b.ldl", StartByte: 3, EndByte: 4}},
		{Code: "LDL1102", MessageKey: "a", SubjectAddress: "s", OwnerAddress: "o", Range: &resolve.SourceRange{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, ModulePath: "a.ldl", StartByte: 1, EndByte: 2}, Related: []resolve.DiagnosticRelated{{Range: &resolve.SourceRange{ModulePath: "z.ldl"}}, {Range: &resolve.SourceRange{ModulePath: "a.ldl"}}}},
		{Code: "LDL1201", MessageKey: "c"},
	}
	resolve.SortDiagnostics(ds)
	if ds[0].Code != "LDL1201" || ds[1].Code != "LDL1102" || ds[1].Related[0].Range.ModulePath != "a.ldl" {
		t.Fatalf("sorted diagnostics = %+v", ds)
	}
}

func TestDefinitionPackModeAndReservations(t *testing.T) {
	pack := resolve.ResolvedPack{
		CanonicalID: "pub/pack",
		Version:     "1.0.0",
		Digest:      "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Path:        "pack/pub-pack",
		Entry:       "pack.ldl",
		Files:       map[string]string{"pack.ldl": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		Manifest:    resolve.PackManifest{Format: "layerdraw-pack", FormatVersion: 1, ID: "pub/pack", Name: "pack", Version: "1.0.0", Language: 1, Entry: "pack.ldl"},
		SourceFiles: map[string]resolve.SourceFile{"pack.ldl": parse(`entity_type server "Server" {
  representation shape rect
  reserve {
    columns [old_col]
    constraints [old_unique]
  }
}
reference guide <<-TEXT
Pack guide.
TEXT
export { server, guide }
`)},
	}
	got := Compile(Input{Resolve: resolve.Resolve(resolve.Input{Mode: resolve.CompilePack, RootPackID: "pub/pack", EntryPath: "pack.ldl", Packs: resolve.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: map[string]resolve.ResolvedPack{"root": pack}}})})
	if got.HasErrors {
		t.Fatalf("Diagnostics = %+v", got.Diagnostics)
	}
	if got.Root.Mode != resolve.CompilePack || got.Root.Address != "ldl:pack:pub:pack" || got.Pack == nil || got.Pack.Address != got.Root.Address || got.Project != nil || len(got.EntityTypes) != 1 || len(got.References) != 1 {
		t.Fatalf("pack result = %+v", got)
	}
	if !reflect.DeepEqual(got.EntityTypes[0].ReservedColumnIDs, []string{"old_col"}) || !reflect.DeepEqual(got.EntityTypes[0].ReservedConstraintIDs, []string{"old_unique"}) {
		t.Fatalf("reservations = %+v", got.EntityTypes[0])
	}
}

func TestDefinitionCommonColumnAndEndpointErrorBranches(t *testing.T) {
	src := testSource()
	c := &compiler{bindings: map[bindingKey]string{
		{module: src.Module, kind: resolve.KindEntityType, text: "server", start: 0, end: 0}: "entity-address",
	}}
	b := body{items: []item{
		{name: "tags", args: []value{{raw: "", kind: syntax.TokenString, span: syntax.Span{Start: 1, End: 2}}, {raw: "dup", kind: syntax.TokenIdentifier, span: syntax.Span{Start: 2, End: 3}}, {raw: "dup", kind: syntax.TokenIdentifier, span: syntax.Span{Start: 3, End: 4}}}, span: syntax.Span{Start: 1, End: 4}},
		{name: "annotations", nested: body{items: []item{{name: "k", args: []value{{raw: "\"v\"", kind: syntax.TokenString}}, span: syntax.Span{Start: 4, End: 5}}, {name: "k", args: []value{{raw: "\"v\"", kind: syntax.TokenString}}, span: syntax.Span{Start: 5, End: 6}}, {name: "", span: syntax.Span{Start: 6, End: 7}}}}, span: syntax.Span{Start: 4, End: 7}},
		{name: "description", args: []value{{raw: "bad", kind: syntax.TokenIdentifier, span: syntax.Span{Start: 8, End: 9}}}, span: syntax.Span{Start: 8, End: 9}},
	}}
	_ = c.common(b, src, "subject", "owner")

	owner := resolve.DeclarationSymbol{Address: "owner", Symbol: resolve.StableSymbol{Origin: resolve.Origin{Kind: resolve.OriginProject, ProjectID: "p"}, Path: []resolve.SymbolSegment{{Kind: resolve.KindRelationType, ID: "r"}}}}
	c.columnDecl = map[string]resolve.DeclarationSymbol{}
	cols := c.columns(&item{nested: body{items: []item{
		{name: "bad", args: []value{{raw: "\"Bad\"", kind: syntax.TokenString}}, span: syntax.Span{Start: 10, End: 11}},
		{name: "dup", args: []value{{raw: "\"Dup\"", kind: syntax.TokenString}, {raw: "string", kind: syntax.TokenIdentifier}}, span: syntax.Span{Start: 11, End: 12}},
		{name: "dup", args: []value{{raw: "\"Dup\"", kind: syntax.TokenString}, {raw: "string", kind: syntax.TokenIdentifier}}, span: syntax.Span{Start: 12, End: 13}},
	}}}, owner, src)
	if len(cols) != 1 {
		t.Fatalf("columns = %+v", cols)
	}
	ep := c.endpoint(&item{args: []value{
		{raw: "role", kind: syntax.TokenIdentifier},
		{raw: "bad", kind: syntax.TokenIdentifier, span: syntax.Span{Start: 13, End: 14}},
		{raw: "types", kind: syntax.TokenIdentifier},
		{raw: "server", kind: syntax.TokenIdentifier, span: syntax.Span{Start: 0, End: 0}, node: listNode("server")},
		{raw: "layers", kind: syntax.TokenIdentifier},
		{raw: "missing_layer", kind: syntax.TokenIdentifier, span: syntax.Span{Start: 14, End: 15}, node: listNode("missing_layer")},
	}}, resolve.KindEntityType, owner, src)
	if len(ep.EntityTypeAddresses) != 1 {
		t.Fatalf("endpoint = %+v", ep)
	}
	_, _ = c.binding(src.Module, resolve.KindLayer, "none", syntax.Span{Start: 99, End: 100})
	for _, message := range []string{"tags requires one list", "invalid column", "duplicate column", "invalid endpoint selector", "unresolved endpoint selector"} {
		if !hasDiagnosticMessage(c.diagnostics, message) {
			t.Fatalf("diagnostics = %+v, want %q", c.diagnostics, message)
		}
	}
}

func TestDefinitionScalarAndModifierErrorBranches(t *testing.T) {
	src := testSource()
	c := &compiler{}
	cases := []Column{
		{Address: "s", ValueType: ScalarString},
		{Address: "d", ValueType: ScalarDate},
		{Address: "dt", ValueType: ScalarDatetime},
		{Address: "i", ValueType: ScalarInteger},
		{Address: "n", ValueType: ScalarNumber},
		{Address: "b", ValueType: ScalarBoolean},
		{Address: "e", ValueType: ScalarEnum, EnumValues: []string{"ok"}, ReservedEnumValues: []string{"old"}},
	}
	bad := value{raw: "\"bad\"", kind: syntax.TokenString, span: syntax.Span{Start: 1, End: 2}}
	for i := range cases {
		got := c.scalar(bad, &cases[i], src, "owner")
		if cases[i].ValueType == ScalarString && (got == nil || got.String != "bad") {
			t.Fatalf("string scalar = %+v", got)
		}
		if cases[i].ValueType != ScalarString && got != nil {
			t.Fatalf("bad scalar accepted for %s", cases[i].ValueType)
		}
	}
	_ = c.scalar(value{raw: "old", kind: syntax.TokenIdentifier, span: syntax.Span{Start: 2, End: 3}}, &Column{Address: "e", ValueType: ScalarEnum, EnumValues: []string{"old"}, ReservedEnumValues: []string{"old"}}, src, "owner")
	col := Column{Address: "col", ValueType: ScalarString}
	c.columnModifiers(&col, value{raw: "string", kind: syntax.TokenIdentifier}, []value{
		{raw: "format", kind: syntax.TokenIdentifier, span: syntax.Span{Start: 3, End: 4}}, {raw: "bad", kind: syntax.TokenIdentifier},
		{raw: "min", kind: syntax.TokenIdentifier, span: syntax.Span{Start: 4, End: 5}}, {raw: "\"x\"", kind: syntax.TokenString},
		{raw: "min_length", kind: syntax.TokenIdentifier, span: syntax.Span{Start: 5, End: 6}}, {raw: "-1", kind: syntax.TokenInteger},
		{raw: "unknown", kind: syntax.TokenIdentifier, span: syntax.Span{Start: 6, End: 7}},
	}, src, "owner")
	for _, message := range []string{"invalid date", "invalid datetime", "default type mismatch", "enum default mismatch", "invalid format", "invalid numeric bound", "invalid length bound", "unknown column modifier"} {
		if !hasDiagnosticMessage(c.diagnostics, message) {
			t.Fatalf("diagnostics = %+v, want %q", c.diagnostics, message)
		}
	}
}

func TestDefinitionRelationBranchCoverage(t *testing.T) {
	src := testSource()
	c := &compiler{}
	rt := RelationType{Address: "rt", Projections: defaultProjections(), Render: defaultRender()}
	from, to := value{raw: "from", kind: syntax.TokenIdentifier}, value{raw: "to", kind: syntax.TokenIdentifier}
	ranges := contextTemplateRanges{}
	c.projections([]item{
		{args: []value{{raw: "composed"}}, block: true, nested: body{items: []item{{name: "mode", args: []value{{raw: "overlay", kind: syntax.TokenIdentifier}}}, {name: "overlay_endpoint", args: []value{from}}, {name: "target_endpoint", args: []value{to}}}}},
		{args: []value{{raw: "composed"}}, block: true, nested: body{items: []item{{name: "mode", args: []value{{raw: "badge", kind: syntax.TokenIdentifier}}}, {name: "badge_endpoint", args: []value{from}}, {name: "target_endpoint", args: []value{to}}}}},
		{args: []value{{raw: "composed"}}, block: true, nested: body{items: []item{{name: "mode", args: []value{{raw: "hide", kind: syntax.TokenIdentifier}}}}}},
		{args: []value{{raw: "flow"}}, block: true, nested: body{items: []item{{name: "source_endpoint", args: []value{from}}, {name: "target_endpoint", args: []value{to}}, {name: "connector_kind", args: []value{{raw: "message", kind: syntax.TokenIdentifier}}}}}},
		{args: []value{{raw: "unknown"}}, block: true, span: syntax.Span{Start: 10, End: 11}},
	}, &rt, src, &ranges)
	if rt.Projections.Flow == nil || rt.Projections.Flow.ConnectorKind != "message" {
		t.Fatalf("flow projection = %+v", rt.Projections.Flow)
	}

	c.render([]item{
		{args: []value{{raw: "overlay"}}, block: true, nested: body{items: []item{{name: "kind", args: []value{{raw: "pin", kind: syntax.TokenIdentifier}}}}}},
		{args: []value{{raw: "unknown"}}, block: true, span: syntax.Span{Start: 12, End: 13}},
	}, &rt, src)
	if rt.Render.Overlay.Kind != "pin" {
		t.Fatalf("overlay render = %+v", rt.Render.Overlay)
	}

	badCardinality := body{items: []item{
		{name: "to_per_from", args: []value{{raw: "2", kind: syntax.TokenInteger, node: rangeNode("2", "1")}}, span: syntax.Span{Start: 1, End: 2}},
		{name: "from_per_to", span: syntax.Span{Start: 2, End: 3}},
	}}
	_ = c.cardinality(&item{nested: badCardinality}, Cardinality{}, src, "subject")
	_ = c.requiredEndpoint(body{}, "missing", src, src.Range, "subject")
	c.contextPlaceholders("{from.id} {bad.placeholder}", src, syntax.Span{Start: 3, End: 4}, "subject")
	for _, message := range []string{"unknown projection primitive", "unknown render primitive", "invalid cardinality", "missing endpoint", "unknown context placeholder"} {
		if !hasDiagnosticMessage(c.diagnostics, message) {
			t.Fatalf("diagnostics = %+v, want %q", c.diagnostics, message)
		}
	}
}

func TestDefinitionRemainingBranches(t *testing.T) {
	src := testSource()
	c := &compiler{}
	if got := c.representation(body{items: []item{{name: "representation", args: []value{{raw: "table", kind: syntax.TokenIdentifier}}}}}, src, "subject", "owner"); got.Kind != "table" {
		t.Fatalf("table representation = %+v", got)
	}
	_ = c.representation(body{}, src, "subject", "owner")
	_ = c.requiredString(body{}, "missing", src, "subject", "owner", "LDL1504", "invalid_projection_contract")
	_ = c.optionalColor(body{items: []item{{name: "color", args: []value{{raw: "bad", kind: syntax.TokenIdentifier}}, span: syntax.Span{Start: 1, End: 2}}}}, "color", src, "subject", "owner")
	_, _ = c.enumList(value{node: listNode("a", "a")}, false, src, "subject", "owner")

	stringValue := value{raw: "\"ok\"", kind: syntax.TokenString}
	if got := stringValue.string(); got != "ok" {
		t.Fatalf("string value = %q", got)
	}
	if got := tokenString(syntax.Token{Kind: syntax.TokenIdentifier, Raw: "atom"}); got != "atom" {
		t.Fatalf("tokenString atom = %q", got)
	}
	if got := LayersByDisplayOrder([]Layer{{ID: "b", Order: 1, Address: "b"}, {ID: "a", Order: 1, Address: "a"}}); got[0].ID != "a" {
		t.Fatalf("layer tie order = %+v", got)
	}
	rt := RelationType{Address: "rt", Projections: defaultProjections(), Render: defaultRender()}
	ranges := contextTemplateRanges{}
	c.projections([]item{{}}, &rt, src, &ranges)
	c.render([]item{{}}, &rt, src)
	_, _ = cardinalityBound(value{node: rangeNode("0", "2")})
	_, _ = cardinalityBound(value{node: &syntax.Node{Kind: syntax.NodeValue}})
	if got := columnAddress([]Column{{ID: "a", Address: "addr"}}, "missing"); got != "" {
		t.Fatalf("missing column address = %q", got)
	}
	ds := []resolve.Diagnostic{
		{Code: "B", MessageKey: "b", SubjectAddress: "b", OwnerAddress: "b", Range: &resolve.SourceRange{ModulePath: "same", StartByte: 1, EndByte: 2}},
		{Code: "A", MessageKey: "z", SubjectAddress: "z", OwnerAddress: "z", Range: &resolve.SourceRange{ModulePath: "same", StartByte: 1, EndByte: 2}},
		{Code: "A", MessageKey: "a", SubjectAddress: "a", OwnerAddress: "a", Range: &resolve.SourceRange{ModulePath: "same", StartByte: 1, EndByte: 2}},
	}
	resolve.SortDiagnostics(ds)
	if ds[0].MessageKey != "a" {
		t.Fatalf("diagnostic tie-breakers = %+v", ds)
	}
	for _, message := range []string{"missing representation", "missing required string", "expected string", "duplicate enum value"} {
		if !hasDiagnosticMessage(c.diagnostics, message) {
			t.Fatalf("diagnostics = %+v, want %q", c.diagnostics, message)
		}
	}
}

func rangeNode(min, max string) *syntax.Node {
	r := &syntax.Node{Kind: syntax.NodeRange, Children: []syntax.Element{
		syntax.TokenElement{Token: syntax.Token{Kind: syntax.TokenInteger, Raw: min}},
		syntax.TokenElement{Token: syntax.Token{Kind: syntax.TokenDotDot, Raw: ".."}},
		syntax.TokenElement{Token: syntax.Token{Kind: syntax.TokenInteger, Raw: max}},
	}}
	return &syntax.Node{Kind: syntax.NodeValue, Children: []syntax.Element{r}}
}

func testSource() resolve.DeclarationSource {
	return resolve.DeclarationSource{
		Address: "ldl:project:p:relation-type:r",
		Module:  resolve.ModuleKey{Origin: resolve.Origin{Kind: resolve.OriginProject}, Path: "document.ldl"},
		Range:   syntax.Span{Start: 1, End: 2},
	}
}

func listNode(ids ...string) *syntax.Node {
	list := &syntax.Node{Kind: syntax.NodeList}
	for _, id := range ids {
		val := &syntax.Node{Kind: syntax.NodeValue, Children: []syntax.Element{syntax.TokenElement{Token: syntax.Token{Kind: syntax.TokenIdentifier, Raw: id}}}}
		list.Children = append(list.Children, val)
	}
	return &syntax.Node{Kind: syntax.NodeValue, Children: []syntax.Element{list}}
}
