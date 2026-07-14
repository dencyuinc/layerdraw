// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package materialize

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/exportrecipe"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
)

func TestCanonicalizeRFC8785CompatibilityAndLDLStrings(t *testing.T) {
	value := map[string]any{
		"numbers":    []any{333333333.33333329, 1e30, 4.50, 2e-3, 1e-27, -0.0, 1e-6, 1e-7, 1e20, 1e21},
		"string":     "e\u0301\r\nline\t\b\f\"\\",
		"\U0001f600": "supplementary",
		"\ue000":     "bmp",
	}
	want := `{"numbers":[333333333.3333333,1e+30,4.5,0.002,1e-27,0,0.000001,1e-7,100000000000000000000,1e+21],"string":"é\nline\t\b\f\"\\","😀":"supplementary","":"bmp"}`
	got, err := Canonicalize(value)
	if err != nil || string(got) != want {
		t.Fatalf("Canonicalize()=%s err=%v\nwant=%s", got, err, want)
	}
	if NormalizeString("e\u0301\rline") != "é\nline" {
		t.Fatal("NormalizeString did not apply NFC/LF")
	}
	for _, number := range []string{"NaN", "Infinity", "bad"} {
		if _, err := canonicalNumber(number); err == nil {
			t.Fatalf("canonicalNumber(%q) accepted", number)
		}
	}
	if _, err := Canonicalize(math.Inf(1)); err == nil {
		t.Fatal("Canonicalize accepted infinity")
	}
	if _, err := Canonicalize(string([]byte{0xff})); err == nil {
		t.Fatal("Canonicalize accepted invalid UTF-8")
	}
	if _, err := SemanticHash("unknown", value); err == nil {
		t.Fatal("SemanticHash accepted unknown domain")
	}
	a, _ := SemanticHash(DomainGraph, value)
	b, _ := SemanticHash(DomainDefinition, value)
	if a == b || a[:7] != "sha256:" {
		t.Fatalf("domain separation failed graph=%s definition=%s", a, b)
	}
	if _, err := Canonicalize(map[string]any{"nil": nil, "bool": true, "control": "\u0001", "negative": -12.5}); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalInputUTF8TraversalAndCycleRejection(t *testing.T) {
	invalid := string([]byte{0xff})
	for _, value := range []any{
		map[string]string{invalid: "value"},
		map[string]string{"key": invalid},
		[]string{invalid},
		struct{ Value string }{Value: invalid},
	} {
		if _, err := Canonicalize(value); err == nil {
			t.Fatalf("invalid UTF-8 accepted in %T", value)
		}
	}
	if _, err := Canonicalize([1]string{"value"}); err != nil {
		t.Fatal(err)
	}
	var absent *string
	if got, err := Canonicalize(absent); err != nil || string(got) != "null" {
		t.Fatalf("nil pointer canonicalization=%s err=%v", got, err)
	}
	cycle := map[string]any{}
	cycle["self"] = cycle
	if _, err := Canonicalize(cycle); err == nil {
		t.Fatal("cyclic JSON input accepted")
	}
}

func TestClosedUnionMarshalAndConversionBranches(t *testing.T) {
	shapes := []view.Shape{
		{Kind: view.ShapeDiagram, Diagram: &view.DiagramShape{Layout: view.LayoutGrid, Direction: view.DirectionLeftToRight, Abstraction: view.AbstractionNormal, Placements: []view.Placement{}}},
		{Kind: view.ShapeTable, Table: &view.TableShape{RowSource: view.RowsEntity, Columns: []view.TableColumn{}, Sorts: []view.TableSort{}}},
		{Kind: view.ShapeMatrix, Matrix: &view.MatrixShape{}},
		{Kind: view.ShapeTree, Tree: &view.TreeShape{}},
		{Kind: view.ShapeFlow, Flow: &view.FlowShape{}},
		{Kind: view.ShapeContext, Context: &view.ContextShape{}},
		{Kind: view.ShapeDiff, Diff: &view.DiffShape{Include: []view.DiffSubjectKind{}}},
	}
	for _, source := range shapes {
		normalized := viewShape(source)
		encoded, err := Canonicalize(normalized)
		if err != nil {
			t.Fatalf("shape %s: %v", source.Kind, err)
		}
		if !strings.Contains(string(encoded), `"kind":"`+string(source.Kind)+`"`) || strings.Contains(string(encoded), `"`+string(source.Kind)+`":{`) {
			t.Fatalf("shape wrapper was not flattened: %s", encoded)
		}
	}
	if _, err := Canonicalize(ViewShape{Kind: "unknown"}); err == nil {
		t.Fatal("unknown View shape accepted")
	}

	values := []*query.PredicateValue{
		nil,
		{Kind: query.ValueLiteral, Scalar: &definition.Scalar{Type: definition.ScalarString, String: "x"}},
		{Kind: query.ValueLiteral, Address: stringPointer("ldl:project:p:entity:x")},
		{Kind: query.ValueLiteral, Scalars: []definition.Scalar{{Type: definition.ScalarInteger, Int: 1}}},
		{Kind: query.ValueLiteral, Addresses: []string{"a"}},
		{Kind: query.ValueParameter, ParameterAddress: "p"},
	}
	for _, value := range values {
		normalized := predicateValue(value)
		if normalized != nil {
			if _, err := Canonicalize(normalized); err != nil {
				t.Fatalf("predicate value %+v: %v", value, err)
			}
		}
	}
	for _, invalid := range []PredicateValue{{Kind: "unknown"}, {Kind: query.ValueLiteral}} {
		if _, err := Canonicalize(invalid); err == nil {
			t.Fatalf("invalid predicate value accepted=%+v", invalid)
		}
	}
	predicate(query.Predicate{Kind: query.PredicateAll, Children: []query.Predicate{{Kind: query.PredicateNot, Child: &query.Predicate{Kind: query.PredicateField}}}, Row: &query.RowPredicate{Kind: query.PredicateAll, Children: []query.RowPredicate{{Kind: query.PredicateNot, Child: &query.RowPredicate{Kind: query.PredicateCell}}}}})

	options := []exportOptionsCase{
		{kind: "structured", value: exportStructured()}, {kind: "image", value: exportImage(false)}, {kind: "image-auto", value: exportImage(true)},
		exportPage(), exportHTML(), exportDelimited(), exportXLSX(), exportManifest(),
	}
	for _, test := range options {
		if got := exportOptions(test.value); got.Kind == "" {
			t.Fatalf("%s options empty", test.kind)
		} else if _, err := Canonicalize(got); err != nil {
			t.Fatalf("%s options: %v", test.kind, err)
		}
	}
	for _, maximum := range []CardinalityMaximum{{Value: 1}, {Many: true}} {
		if _, err := Canonicalize(maximum); err != nil {
			t.Fatal(err)
		}
	}

	if scalarPointer(nil) != nil || clonePointer[float64](nil) != nil || cloneNormalizedString(nil) != nil || cloneStringSlicePointer(nil) != nil {
		t.Fatal("optional clone materialized absence")
	}
	if !reflect.DeepEqual(normalizedMap(nil), map[string]string{}) {
		t.Fatal("nil annotations did not normalize to empty map")
	}
	if _, err := (Scalar{}).MarshalJSON(); err == nil {
		t.Fatal("invalid Scalar accepted")
	}
	for _, value := range []Scalar{{Type: definition.ScalarString, String: "x"}, {Type: definition.ScalarInteger, Int: 2}, {Type: definition.ScalarNumber, Float: 2.5}, {Type: definition.ScalarBoolean, Bool: true}} {
		if _, err := Canonicalize(value); err != nil {
			t.Fatal(err)
		}
	}
	cloned := deepClone(struct{ Value any }{Value: map[string][]string{"k": {"v"}}})
	if cloned.Value == nil {
		t.Fatal("interface clone failed")
	}
}

type exportOptionsCase struct {
	kind  string
	value exportrecipe.Options
}

func exportStructured() exportrecipe.Options {
	return exportrecipe.Options{Kind: exportrecipe.FormatJSON, Structured: &exportrecipe.StructuredOptions{Diagnostics: true}}
}
func exportImage(auto bool) exportrecipe.Options {
	return exportrecipe.Options{Kind: exportrecipe.FormatPNG, Image: &exportrecipe.ImageOptions{Width: exportrecipe.Dimension{Auto: auto, Value: 10}, Height: exportrecipe.Dimension{Auto: auto, Value: 20}, Scale: 1, Background: "transparent"}}
}
func exportPage() exportOptionsCase {
	return exportOptionsCase{"page", exportrecipe.Options{Kind: exportrecipe.FormatPDF, Page: &exportrecipe.PageOptions{PageSize: exportrecipe.PageA4, Orientation: exportrecipe.OrientationPortrait, Fit: exportrecipe.FitPage}}}
}
func exportHTML() exportOptionsCase {
	return exportOptionsCase{"html", exportrecipe.Options{Kind: exportrecipe.FormatHTML, HTML: &exportrecipe.HTMLOptions{Interactive: true}}}
}
func exportDelimited() exportOptionsCase {
	return exportOptionsCase{"delimited", exportrecipe.Options{Kind: exportrecipe.FormatCSV, Delimited: &exportrecipe.DelimitedOptions{Header: true}}}
}
func exportXLSX() exportOptionsCase {
	return exportOptionsCase{"xlsx", exportrecipe.Options{Kind: exportrecipe.FormatXLSX, XLSX: &exportrecipe.XLSXOptions{Profile: exportrecipe.XLSXTypeWorkbook}}}
}
func exportManifest() exportOptionsCase {
	return exportOptionsCase{"manifest", exportrecipe.Options{Kind: exportrecipe.FormatMarkdown, Manifest: &exportrecipe.ManifestOptions{SourceManifest: true}}}
}
func stringPointer(value string) *string { return &value }
