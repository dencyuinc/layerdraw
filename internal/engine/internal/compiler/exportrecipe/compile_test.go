// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package exportrecipe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func TestShapeFormatFidelityGolden(t *testing.T) {
	shapes := []Shape{ShapeDiagram, ShapeTable, ShapeMatrix, ShapeTree, ShapeFlow, ShapeContext, ShapeDiff}
	fixture := make(map[Shape]map[Format]string, len(shapes))
	for _, shape := range shapes {
		fixture[shape] = make(map[Format]string, len(formatOrder))
		for _, format := range formatOrder {
			maximum, supported := nativeMaximum(shape, format)
			fixture[shape][format] = "unsupported"
			if supported {
				fixture[shape][format] = string(maximum)
			}
		}
	}
	payload, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		t.Fatalf("marshal capability matrix: %v", err)
	}
	payload = append(payload, '\n')
	want, err := os.ReadFile("testdata/shape_format_fidelity.golden.json")
	if err != nil {
		t.Fatalf("read capability golden: %v", err)
	}
	if !bytes.Equal(payload, want) {
		t.Fatalf("shape/format/fidelity golden mismatch\n--- want\n%s\n--- got\n%s", want, payload)
	}
}

func TestCompileClosesEveryFormatAndOptionFamily(t *testing.T) {
	input := exportTestInput(t, allExportSource, map[string]ViewContext{
		"diagram": {Category: CategoryTopology, Shape: ShapeDiagram, DiagramComposed: true},
		"table":   {Category: CategoryInventory, Shape: ShapeTable},
		"tree":    {Category: CategoryHierarchy, Shape: ShapeTree},
		"flow":    {Category: CategoryFlow, Shape: ShapeFlow},
	})
	got := Compile(input)
	if got.HasErrors {
		t.Fatalf("Compile() diagnostics=%+v", got.Diagnostics)
	}
	if !got.MatchesResolve(input.Resolve) || !got.Generation().Matches(input.Resolve.Generation()) {
		t.Fatal("successful static Export recipe result lost its generation")
	}
	if len(got.Recipes) != 15 {
		t.Fatalf("recipes=%d, want 15", len(got.Recipes))
	}

	byID := map[string]Recipe{}
	for _, recipe := range got.Recipes {
		byID[recipe.ID] = recipe
		if recipe.Extension == "" || recipe.Filename == "" || recipe.ExporterProfile.ID != "layerdraw/"+string(recipe.Format)+"@1" || recipe.ExporterProfile.RegistryDigest == "" || recipe.ExporterProfile.SpecificationDigest == "" {
			t.Fatalf("recipe is not closed: %+v", recipe)
		}
	}
	if image := byID["svg"].Options.Image; image == nil || image.Width.Value != 640 || image.Height.Value != 480 || image.Scale != 2.5 || image.Background != "#AABBCCDD" {
		t.Fatalf("SVG options=%+v", image)
	}
	if image := byID["png"].Options.Image; image == nil || !image.Width.Auto || !image.Height.Auto || image.Scale != 1 || image.Background != "transparent" {
		t.Fatalf("PNG defaults=%+v", image)
	}
	if page := byID["pdf"].Options.Page; page == nil || page.PageSize != PageLedger || page.Orientation != OrientationLandscape || page.Fit != FitWidth || !page.Legend {
		t.Fatalf("PDF options=%+v", page)
	}
	if page := byID["pptx"].Options.Page; page == nil || page.PageSize != PageA4 || page.Orientation != OrientationPortrait || page.Fit != FitPage || page.Legend {
		t.Fatalf("PPTX defaults=%+v", page)
	}
	if structured := byID["json"].Options.Structured; structured == nil || !structured.Diagnostics || !structured.StateSummary {
		t.Fatalf("JSON options=%+v", structured)
	}
	if structured := byID["yaml"].Options.Structured; structured == nil || structured.Diagnostics || structured.StateSummary {
		t.Fatalf("YAML defaults=%+v", structured)
	}
	if html := byID["html"].Options.HTML; html == nil || !html.Interactive || !html.EmbedAssets {
		t.Fatalf("HTML options=%+v", html)
	}
	if delimited := byID["csv"].Options.Delimited; delimited == nil || !delimited.Bundle || !delimited.Header || !delimited.SourceManifest {
		t.Fatalf("CSV options=%+v", delimited)
	}
	if delimited := byID["tsv"].Options.Delimited; delimited == nil || delimited.Bundle || delimited.Header || delimited.SourceManifest || byID["tsv"].NativeMaximumFidelity != FidelityLossy {
		t.Fatalf("TSV defaults/capability=%+v recipe=%+v", delimited, byID["tsv"])
	}
	xlsx := byID["xlsx"]
	if options := xlsx.Options.XLSX; options == nil || options.Profile != XLSXComposedDiagramWorkbook || !options.LookupSheets || !options.HiddenIDs || !options.Formulas || !options.ViewDataJSON || xlsx.EffectiveMaximumFidelity != FidelityLossless || xlsx.FidelityBasis != FidelityBasisEmbeddedViewData {
		t.Fatalf("XLSX options/capability=%+v recipe=%+v", options, xlsx)
	}
	if byID["markdown"].Extension != ".md" || byID["mermaid"].Extension != ".mmd" || byID["drawio"].Extension != ".drawio" {
		t.Fatalf("canonical extensions: md=%q mmd=%q drawio=%q", byID["markdown"].Extension, byID["mermaid"].Extension, byID["drawio"].Extension)
	}
	if !byID["mermaid"].RequiresSourceManifest || !byID["drawio"].RequiresSourceManifest || byID["markdown"].RequiresSourceManifest {
		t.Fatalf("manifest requirements: mermaid=%v drawio=%v markdown=%v", byID["mermaid"].RequiresSourceManifest, byID["drawio"].RequiresSourceManifest, byID["markdown"].RequiresSourceManifest)
	}
	if byID["png"].ExporterProfile.ID != "layerdraw/png@1" {
		t.Fatalf("explicit canonical exporter profile=%+v", byID["png"].ExporterProfile)
	}
}

func TestCompileAcceptsExplicitAutoImageDimensions(t *testing.T) {
	input := oneExportInput(t, "png", "out.png", "fidelity visual_only\n    width auto\n    height auto", ViewContext{Shape: ShapeDiagram})
	got := Compile(input)
	if got.HasErrors || len(got.Recipes) != 1 {
		t.Fatalf("Compile() recipes=%+v diagnostics=%+v", got.Recipes, got.Diagnostics)
	}
	image := got.Recipes[0].Options.Image
	if image == nil || !image.Width.Auto || image.Width.Value != 0 || !image.Height.Auto || image.Height.Value != 0 {
		t.Fatalf("explicit auto dimensions were not preserved: %+v", image)
	}
}

func TestCompileFilenameValidationIsHostIndependent(t *testing.T) {
	for _, tc := range []struct {
		name     string
		filename string
		valid    bool
	}{
		{name: "plain basename", filename: "out.png", valid: true},
		{name: "unicode basename", filename: "diagram-東京.png", valid: true},
		{name: "posix separator", filename: "nested/out.png"},
		{name: "windows separator", filename: `nested\out.png`},
		{name: "posix traversal", filename: "../out.png"},
		{name: "windows traversal", filename: `..\out.png`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Compile(oneExportInput(t, "png", tc.filename, "fidelity visual_only", ViewContext{Shape: ShapeDiagram}))
			if tc.valid && got.HasErrors {
				t.Fatalf("host-neutral basename rejected: %+v", got.Diagnostics)
			}
			if !tc.valid && (!got.HasErrors || !diagnosticContains(got.Diagnostics, "canonical basename")) {
				t.Fatalf("path-like filename accepted: recipes=%+v diagnostics=%+v", got.Recipes, got.Diagnostics)
			}
		})
	}
	if validFilename("out\x00.png", ".png") {
		t.Fatal("NUL-containing basename passed lexical validation")
	}
}

func TestCompileXLSXProfileDefaultsAndCompatibility(t *testing.T) {
	cases := []struct {
		name     string
		context  ViewContext
		profile  XLSXProfile
		explicit string
	}{
		{name: "diagram", context: ViewContext{Shape: ShapeDiagram}, profile: XLSXDiagramWorkbook},
		{name: "composed", context: ViewContext{Shape: ShapeDiagram, DiagramComposed: true}, profile: XLSXComposedDiagramWorkbook},
		{name: "table", context: ViewContext{Shape: ShapeTable}, profile: XLSXTypeWorkbook},
		{name: "matrix", context: ViewContext{Shape: ShapeMatrix}, profile: XLSXMatrixWorkbook},
		{name: "tree", context: ViewContext{Shape: ShapeTree}, profile: XLSXTreeWorkbook},
		{name: "flow", context: ViewContext{Shape: ShapeFlow}, profile: XLSXFlowWorkbook},
		{name: "context", context: ViewContext{Shape: ShapeContext}, profile: XLSXContextWorkbook},
		{name: "diff", context: ViewContext{Shape: ShapeDiff}, profile: XLSXDiffWorkbook},
		{name: "impact", context: ViewContext{Category: CategoryImpact, Shape: ShapeMatrix}, profile: XLSXImpactWorkbook, explicit: "profile impact_workbook"},
		{name: "inventory", context: ViewContext{Shape: ShapeDiagram}, profile: XLSXDiagramInventoryWorkbook, explicit: "profile diagram_inventory_workbook"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := "fidelity traceable_summary\n    source_refs\n    " + tc.explicit
			input := oneExportInput(t, "xlsx", "out.xlsx", body, tc.context)
			got := Compile(input)
			if got.HasErrors || len(got.Recipes) != 1 || got.Recipes[0].Options.XLSX == nil || got.Recipes[0].Options.XLSX.Profile != tc.profile {
				t.Fatalf("Compile()=%+v diagnostics=%+v, want profile %s", got.Recipes, got.Diagnostics, tc.profile)
			}
		})
	}

	input := oneExportInput(t, "xlsx", "out.xlsx", "fidelity traceable_summary\n    source_refs\n    profile type_workbook", ViewContext{Shape: ShapeDiagram})
	assertError(t, Compile(input), "XLSX profile is incompatible")
}

func TestDelimitedTraceabilityRequiresHeaderForEveryShape(t *testing.T) {
	body := "fidelity traceable_summary\n    source_refs\n    bundle\n    source_manifest"
	diagram := Compile(oneExportInput(t, "csv", "out.csv", body, ViewContext{Shape: ShapeDiagram}))
	assertError(t, diagram, "exceeds format capability")

	body += "\n    header"
	diagram = Compile(oneExportInput(t, "csv", "out.csv", body, ViewContext{Shape: ShapeDiagram}))
	if diagram.HasErrors || diagram.Recipes[0].NativeMaximumFidelity != FidelityTraceableSummary {
		t.Fatalf("Diagram CSV with complete traceability options failed: recipes=%+v diagnostics=%+v", diagram.Recipes, diagram.Diagnostics)
	}
}

func TestTraceableHTMLStillRequiresCompanionManifest(t *testing.T) {
	got := Compile(oneExportInput(t, "html", "out.html", "fidelity traceable_summary\n    source_refs", ViewContext{Shape: ShapeTable}))
	if got.HasErrors || len(got.Recipes) != 1 {
		t.Fatalf("Compile() recipes=%+v diagnostics=%+v", got.Recipes, got.Diagnostics)
	}
	if !got.Recipes[0].RequiresSourceManifest {
		t.Fatal("traceable HTML omitted the mandatory companion manifest")
	}
}

func TestCompileRejectsMalformedClosedExportMembers(t *testing.T) {
	cases := []struct {
		name, format, filename, body, message string
		context                               ViewContext
	}{
		{name: "missing fidelity", format: "png", filename: "out.png", body: "source_refs", message: "requires fidelity", context: ViewContext{Shape: ShapeDiagram}},
		{name: "unknown fidelity", format: "png", filename: "out.png", body: "fidelity perfect", message: "invalid Export fidelity", context: ViewContext{Shape: ShapeDiagram}},
		{name: "fidelity arity", format: "png", filename: "out.png", body: "fidelity visual_only lossy", message: "fidelity requires one value", context: ViewContext{Shape: ShapeDiagram}},
		{name: "wrong extension", format: "png", filename: "out.jpg", body: "fidelity visual_only", message: "canonical basename", context: ViewContext{Shape: ShapeDiagram}},
		{name: "path filename", format: "png", filename: "nested/out.png", body: "fidelity visual_only", message: "canonical basename", context: ViewContext{Shape: ShapeDiagram}},
		{name: "unknown option", format: "png", filename: "out.png", body: "fidelity visual_only\n    mystery true", message: "unknown or invalid Export option", context: ViewContext{Shape: ShapeDiagram}},
		{name: "duplicate option", format: "png", filename: "out.png", body: "fidelity visual_only\n    scale 1\n    scale 2", message: "duplicate Export option", context: ViewContext{Shape: ShapeDiagram}},
		{name: "option block", format: "png", filename: "out.png", body: "fidelity visual_only\n    scale {\n      nested true\n    }", message: "unknown or invalid Export option", context: ViewContext{Shape: ShapeDiagram}},
		{name: "flag arguments", format: "html", filename: "out.html", body: "fidelity traceable_summary\n    source_refs\n    interactive true", message: "interactive is a flag", context: ViewContext{Shape: ShapeDiagram}},
		{name: "dimension kind", format: "png", filename: "out.png", body: "fidelity visual_only\n    width 1.5", message: "positive integer", context: ViewContext{Shape: ShapeDiagram}},
		{name: "dimension zero", format: "png", filename: "out.png", body: "fidelity visual_only\n    width 0", message: "positive JSON-safe integer", context: ViewContext{Shape: ShapeDiagram}},
		{name: "dimension too large", format: "png", filename: "out.png", body: "fidelity visual_only\n    width 9007199254740992", message: "positive JSON-safe integer", context: ViewContext{Shape: ShapeDiagram}},
		{name: "scale kind", format: "png", filename: "out.png", body: "fidelity visual_only\n    scale nope", message: "finite positive number", context: ViewContext{Shape: ShapeDiagram}},
		{name: "scale zero", format: "png", filename: "out.png", body: "fidelity visual_only\n    scale 0", message: "finite positive number", context: ViewContext{Shape: ShapeDiagram}},
		{name: "background arity", format: "png", filename: "out.png", body: "fidelity visual_only\n    background", message: "background requires", context: ViewContext{Shape: ShapeDiagram}},
		{name: "background noncanonical", format: "png", filename: "out.png", body: "fidelity visual_only\n    background \"red\"", message: "invalid canonical color", context: ViewContext{Shape: ShapeDiagram}},
		{name: "enum", format: "pdf", filename: "out.pdf", body: "fidelity visual_only\n    page_size a5", message: "invalid page_size", context: ViewContext{Shape: ShapeDiagram}},
		{name: "profile arity", format: "png", filename: "out.png", body: "fidelity visual_only\n    exporter_profile", message: "requires one string", context: ViewContext{Shape: ShapeDiagram}},
		{name: "profile syntax", format: "png", filename: "out.png", body: "fidelity visual_only\n    exporter_profile \"UPPER@1\"", message: "invalid exporter profile ID", context: ViewContext{Shape: ShapeDiagram}},
		{name: "profile missing", format: "png", filename: "out.png", body: "fidelity visual_only\n    exporter_profile \"vendor/png@1\"", message: "profile is missing", context: ViewContext{Shape: ShapeDiagram}},
		{name: "unsupported shape", format: "bpmn", filename: "out.bpmn", body: "fidelity lossy", message: "unsupported for View shape", context: ViewContext{Shape: ShapeTable}},
		{name: "json refs", format: "json", filename: "out.json", body: "fidelity lossy", message: "requires source_refs", context: ViewContext{Shape: ShapeTable}},
		{name: "traceable refs", format: "html", filename: "out.html", body: "fidelity traceable_summary", message: "require source_refs", context: ViewContext{Shape: ShapeTable}},
		{name: "exceeds capability", format: "png", filename: "out.png", body: "fidelity traceable_summary\n    source_refs", message: "exceeds format capability", context: ViewContext{Shape: ShapeDiagram}},
		{name: "diff state summary", format: "json", filename: "out.json", body: "fidelity lossless\n    source_refs\n    state_summary", message: "forbidden for Diff", context: ViewContext{Shape: ShapeDiff, DiffSource: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Compile(oneExportInput(t, tc.format, tc.filename, tc.body, tc.context))
			assertError(t, got, tc.message)
			if len(got.Recipes) != 0 {
				t.Fatalf("transaction leaked recipes: %+v", got.Recipes)
			}
		})
	}
}

func TestCompileRejectsDuplicateFilenameWithExactRelatedRange(t *testing.T) {
	source := strings.Replace(exportSourceTemplate, "EXPORTS", `export first png "same.png" {
    fidelity visual_only
  }
  export second png "same.png" {
    fidelity visual_only
  }`, 1)
	got := Compile(exportTestInput(t, source, map[string]ViewContext{"v": {Shape: ShapeDiagram}}))
	assertError(t, got, "duplicate Export filename")
	for _, diagnostic := range got.Diagnostics {
		if diagnostic.Message != "duplicate Export filename" {
			continue
		}
		if diagnostic.Range == nil || len(diagnostic.Related) != 1 || diagnostic.Related[0].Range == nil {
			t.Fatalf("diagnostic lacks exact primary/related ranges: %+v", diagnostic)
		}
		if gotText := source[diagnostic.Range.StartByte:diagnostic.Range.EndByte]; gotText != "\"same.png\"" {
			t.Fatalf("primary range=%q", gotText)
		}
		if gotText := source[diagnostic.Related[0].Range.StartByte:diagnostic.Related[0].Range.EndByte]; gotText != "\"same.png\"" {
			t.Fatalf("related range=%q", gotText)
		}
		return
	}
	t.Fatal("missing duplicate filename diagnostic")
}

func TestCompileValidatesGenerationAndCorruptParentsBeforeUpstreamShortCircuit(t *testing.T) {
	good := oneExportInput(t, "png", "out.png", "fidelity visual_only", ViewContext{Shape: ShapeDiagram})
	other := oneExportInput(t, "png", "other.png", "fidelity visual_only", ViewContext{Shape: ShapeDiagram})

	cases := []struct {
		name   string
		mutate func(*Input)
	}{
		{name: "definition", mutate: func(in *Input) { in.Definition = other.Definition }},
		{name: "graph", mutate: func(in *Input) { in.Graph = other.Graph }},
		{name: "query", mutate: func(in *Input) { in.Query = other.Query }},
		{name: "nil graph", mutate: func(in *Input) { in.Graph.Graph = nil }},
		{name: "stale context", mutate: func(in *Input) { in.Views[0].Generation = other.Resolve.Generation() }},
		{name: "duplicate context", mutate: func(in *Input) { in.Views = append(in.Views, in.Views[0]) }},
		{name: "missing context", mutate: func(in *Input) { in.Views = nil }},
		{name: "foreign context", mutate: func(in *Input) { in.Views[0].Address = "project:foreign" }},
		{name: "corrupt context shape", mutate: func(in *Input) { in.Views[0].Shape = Shape("unknown") }},
		{name: "corrupt context state", mutate: func(in *Input) { in.Views[0].StatePolicy = query.StatePolicy("unknown") }},
		{name: "corrupt context triad", mutate: func(in *Input) { in.Views[0].DiffSource = true }},
		{name: "foreign query recipe", mutate: func(in *Input) { in.Query.Recipes[0].Address = "project:foreign-query" }},
		{name: "missing query recipe", mutate: func(in *Input) { in.Query.Recipes = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := good
			input.Views = append([]ViewContext{}, good.Views...)
			input.Query.Recipes = append([]query.Recipe{}, good.Query.Recipes...)
			tc.mutate(&input)
			// A corrupt child must still be diagnosed even when an upstream stage
			// already contains an error.
			input.Resolve.HasErrors = true
			input.Resolve.Diagnostics = []resolve.Diagnostic{{Code: "UPSTREAM", Severity: "error", Message: "upstream", Arguments: map[string]string{"owned": "input"}}}
			got := Compile(input)
			if !got.HasErrors || len(got.Recipes) != 0 || !diagnosticContains(got.Diagnostics, "upstream") || !diagnosticContains(got.Diagnostics, "stale") && !diagnosticContains(got.Diagnostics, "does not match") && !diagnosticContains(got.Diagnostics, "unavailable") && !diagnosticContains(got.Diagnostics, "cover") && !diagnosticContains(got.Diagnostics, "outside") && !diagnosticContains(got.Diagnostics, "missing") && !diagnosticContains(got.Diagnostics, "corrupt") {
				t.Fatalf("Compile() diagnostics=%+v recipes=%+v", got.Diagnostics, got.Recipes)
			}
			got.Diagnostics[0].Arguments["owned"] = "result"
			if input.Resolve.Diagnostics[0].Arguments["owned"] != "input" {
				t.Fatal("diagnostics were not cloned defensively")
			}
		})
	}
}

func TestUpstreamDiagnosticsUsesLatestTypedStageAndClones(t *testing.T) {
	diagnostics := func(message string) []resolve.Diagnostic {
		return []resolve.Diagnostic{{Severity: "error", Message: message, Arguments: map[string]string{"owner": message}}}
	}
	input := Input{
		Resolve:    resolve.Result{Diagnostics: diagnostics("resolve")},
		Definition: definition.Result{Diagnostics: diagnostics("definition")},
		Graph:      graph.Result{Diagnostics: diagnostics("graph")},
		Query:      query.Result{Diagnostics: diagnostics("query")},
	}
	for _, tc := range []struct {
		name string
		drop func(*Input)
		want string
	}{
		{name: "query", drop: func(*Input) {}, want: "query"},
		{name: "graph", drop: func(in *Input) { in.Query.Diagnostics = nil }, want: "graph"},
		{name: "definition", drop: func(in *Input) { in.Query.Diagnostics = nil; in.Graph.Diagnostics = nil }, want: "definition"},
		{name: "resolve", drop: func(in *Input) {
			in.Query.Diagnostics = nil
			in.Graph.Diagnostics = nil
			in.Definition.Diagnostics = nil
		}, want: "resolve"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := input
			tc.drop(&value)
			got := upstreamDiagnostics(value)
			if len(got) != 1 || got[0].Message != tc.want {
				t.Fatalf("diagnostics=%+v, want %q", got, tc.want)
			}
			got[0].Arguments["owner"] = "result"
			if valueForMessage(value, tc.want).Arguments["owner"] != tc.want {
				t.Fatal("diagnostic Arguments alias upstream storage")
			}
		})
	}
}

func valueForMessage(input Input, message string) resolve.Diagnostic {
	for _, diagnostics := range [][]resolve.Diagnostic{input.Query.Diagnostics, input.Graph.Diagnostics, input.Definition.Diagnostics, input.Resolve.Diagnostics} {
		if len(diagnostics) != 0 && diagnostics[0].Message == message {
			return diagnostics[0]
		}
	}
	return resolve.Diagnostic{}
}

func TestCompileRejectsCorruptRegistryAndDefensivelySnapshotsIt(t *testing.T) {
	base := BuiltinRegistry()
	copyOne := BuiltinRegistry()
	base.Profiles[0].Specification[0] = 'X'
	if reflect.DeepEqual(base.Profiles[0].Specification, copyOne.Profiles[0].Specification) {
		t.Fatal("BuiltinRegistry returned shared profile bytes")
	}

	cases := []struct {
		name   string
		mutate func(*ProfileRegistry)
	}{
		{name: "format", mutate: func(r *ProfileRegistry) { r.Format = "replaceable" }},
		{name: "schema", mutate: func(r *ProfileRegistry) { r.SchemaVersion = 2 }},
		{name: "registry digest", mutate: func(r *ProfileRegistry) { r.Digest = "sha256:bad" }},
		{name: "specification", mutate: func(r *ProfileRegistry) { r.Profiles[0].Specification[0] = 'X'; r.Digest = registryDigest(*r) }},
		{name: "duplicate", mutate: func(r *ProfileRegistry) {
			r.Profiles = append(r.Profiles, r.Profiles[0])
			r.Digest = registryDigest(*r)
		}},
		{name: "format mismatch", mutate: func(r *ProfileRegistry) { r.Profiles[0].Format = FormatPNG; r.Digest = registryDigest(*r) }},
		{name: "unknown", mutate: func(r *ProfileRegistry) { r.Profiles[0].ID = "vendor/json@1"; r.Digest = registryDigest(*r) }},
		{name: "recomputed subset", mutate: func(r *ProfileRegistry) {
			r.Profiles = append([]ProfileSpecification{}, r.Profiles[3])
			r.Digest = registryDigest(*r)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registry := BuiltinRegistry()
			tc.mutate(&registry)
			input := oneExportInput(t, "png", "out.png", "fidelity visual_only", ViewContext{Shape: ShapeDiagram})
			input.Registry = &registry
			got := Compile(input)
			assertError(t, got, "profile")
			if len(got.Recipes) != 0 {
				t.Fatal("corrupt registry produced a partial recipe")
			}
		})
	}

	registry := BuiltinRegistry()
	input := oneExportInput(t, "png", "out.png", "fidelity visual_only", ViewContext{Shape: ShapeDiagram})
	input.Registry = &registry
	got := Compile(input)
	registry.Profiles[0].Specification[0] = 'X'
	if got.HasErrors || got.Recipes[0].ExporterProfile.RegistryDigest == "" {
		t.Fatalf("registry snapshot changed successful result: %+v", got)
	}
}

func TestRegistryDigestUsesRFC8785KeyOrder(t *testing.T) {
	registry := ProfileRegistry{
		Format:        "layerdraw-exporter-profiles",
		SchemaVersion: 1,
		Profiles: []ProfileSpecification{
			{ID: "z/profile@1", Format: FormatPNG, SpecificationDigest: "sha256:z"},
			{ID: "a/profile@1", Format: FormatJSON, SpecificationDigest: "sha256:a"},
		},
	}
	want := `{"format":"layerdraw-exporter-profiles","profiles":{"a/profile@1":{"format":"json","id":"a/profile@1","specification_digest":"sha256:a"},"z/profile@1":{"format":"png","id":"z/profile@1","specification_digest":"sha256:z"}},"schema_version":1}`
	if got := string(registryCanonicalBytes(registry)); got != want {
		t.Fatalf("registry canonical bytes are not RFC 8785 ordered:\n got: %s\nwant: %s", got, want)
	}
	if got := registryDigest(registry); got != digest([]byte(want)) {
		t.Fatalf("registry digest=%q, want digest of canonical bytes", got)
	}
}

func TestLanguageOneRegistryRejectsRecomputedSubsetIdentity(t *testing.T) {
	registry := BuiltinRegistry()
	png := registry.Profiles[3]
	registry.Profiles = []ProfileSpecification{png}
	registry.Digest = registryDigest(registry)

	input := oneExportInput(t, "png", "out.png", "fidelity visual_only", ViewContext{Shape: ShapeDiagram})
	input.Registry = &registry
	got := Compile(input)
	if !got.HasErrors || len(got.Recipes) != 0 || !diagnosticContains(got.Diagnostics, "invalid exporter-profile registry identity") {
		t.Fatalf("recomputed subset registry validated as Language 1: recipes=%+v diagnostics=%+v", got.Recipes, got.Diagnostics)
	}
}

func TestStaticRecipeBoundaryExcludesExportPlanAndViewData(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(Result{}), reflect.TypeOf(Recipe{}), reflect.TypeOf(Options{})} {
		for index := 0; index < typ.NumField(); index++ {
			name := typ.Field(index).Name
			if strings.Contains(name, "ExportPlan") || strings.Contains(name, "ViewData") {
				t.Fatalf("static export recipe boundary exposes runtime field %s.%s", typ.Name(), name)
			}
		}
	}
}

func TestCompileMissingSourceAndOwnerContextAreTransactional(t *testing.T) {
	input := oneExportInput(t, "png", "out.png", "fidelity visual_only", ViewContext{Shape: ShapeDiagram})
	input.Resolve.DeclarationSources = nil
	assertError(t, Compile(input), "missing Export declaration source")

	input = oneExportInput(t, "png", "out.png", "fidelity visual_only", ViewContext{Shape: ShapeDiagram})
	c := newCompiler(input)
	c.validateRegistry()
	delete(c.contexts, input.Views[0].Address)
	var export resolve.DeclarationSymbol
	for _, declaration := range input.Resolve.Declarations {
		if declaration.Kind == resolve.KindExport {
			export = declaration
		}
	}
	recipe := c.compileRecipe(export)
	if recipe.Address == "" || !diagnosticContains(c.diagnostics, "owner View context is unavailable") {
		t.Fatalf("recipe=%+v diagnostics=%+v", recipe, c.diagnostics)
	}
}

func TestCompileIsDeterministicAcrossInputPermutations(t *testing.T) {
	input := exportTestInput(t, allExportSource, map[string]ViewContext{
		"diagram": {Category: CategoryTopology, Shape: ShapeDiagram, DiagramComposed: true},
		"table":   {Category: CategoryInventory, Shape: ShapeTable}, "tree": {Category: CategoryHierarchy, Shape: ShapeTree}, "flow": {Category: CategoryFlow, Shape: ShapeFlow},
	})
	want := Compile(input)
	reverse(input.Resolve.Declarations)
	reverse(input.Resolve.DeclarationSources)
	reverse(input.Resolve.Candidates)
	reverse(input.Views)
	got := Compile(input)
	if got.HasErrors || !reflect.DeepEqual(got.Recipes, want.Recipes) || !reflect.DeepEqual(got.Diagnostics, want.Diagnostics) {
		t.Fatalf("permutation changed output\nwant=%+v\ngot=%+v\ndiags=%+v", want.Recipes, got.Recipes, got.Diagnostics)
	}
}

func TestCompilePreservesAuthoredExportOrder(t *testing.T) {
	source := strings.Replace(exportSourceTemplate, "EXPORTS", `export zebra png "zebra.png" {
    fidelity visual_only
  }
  export alpha svg "alpha.svg" {
    fidelity visual_only
  }
  export middle pdf "middle.pdf" {
    fidelity visual_only
  }`, 1)
	input := exportTestInput(t, source, map[string]ViewContext{"v": {Shape: ShapeDiagram}})
	got := Compile(input)
	if got.HasErrors {
		t.Fatalf("Compile() diagnostics=%+v", got.Diagnostics)
	}
	ids := make([]string, 0, len(got.Recipes))
	for _, recipe := range got.Recipes {
		ids = append(ids, recipe.ID)
	}
	if !reflect.DeepEqual(ids, []string{"zebra", "alpha", "middle"}) {
		t.Fatalf("Export order=%v, want authored order", ids)
	}

	reverse(input.Resolve.Declarations)
	reverse(input.Resolve.DeclarationSources)
	permuted := Compile(input)
	if permuted.HasErrors || !reflect.DeepEqual(permuted.Recipes, got.Recipes) {
		t.Fatalf("parent slice permutation changed authored order: recipes=%+v diagnostics=%+v", permuted.Recipes, permuted.Diagnostics)
	}
}

func TestCloneRecipesOwnsEveryOptionVariant(t *testing.T) {
	recipes := []Recipe{
		{Options: Options{Structured: &StructuredOptions{Diagnostics: true}}},
		{Options: Options{Image: &ImageOptions{Scale: 2}}},
		{Options: Options{Page: &PageOptions{Legend: true}}},
		{Options: Options{HTML: &HTMLOptions{Interactive: true}}},
		{Options: Options{Delimited: &DelimitedOptions{Header: true}}},
		{Options: Options{XLSX: &XLSXOptions{HiddenIDs: true}}},
		{Options: Options{Manifest: &ManifestOptions{SourceManifest: true}}},
	}

	cloned := CloneRecipes(recipes)
	cloned[0].Options.Structured.Diagnostics = false
	cloned[1].Options.Image.Scale = 3
	cloned[2].Options.Page.Legend = false
	cloned[3].Options.HTML.Interactive = false
	cloned[4].Options.Delimited.Header = false
	cloned[5].Options.XLSX.HiddenIDs = false
	cloned[6].Options.Manifest.SourceManifest = false

	if !recipes[0].Options.Structured.Diagnostics || recipes[1].Options.Image.Scale != 2 ||
		!recipes[2].Options.Page.Legend || !recipes[3].Options.HTML.Interactive ||
		!recipes[4].Options.Delimited.Header || !recipes[5].Options.XLSX.HiddenIDs ||
		!recipes[6].Options.Manifest.SourceManifest {
		t.Fatal("CloneRecipes aliased mutable option storage")
	}
}

func oneExportInput(t *testing.T, format, filename, body string, context ViewContext) Input {
	t.Helper()
	export := fmt.Sprintf("export e %s %q {\n    %s\n  }", format, filename, body)
	source := strings.Replace(exportSourceTemplate, "EXPORTS", export, 1)
	return exportTestInput(t, source, map[string]ViewContext{"v": context})
}

func exportTestInput(t *testing.T, source string, contexts map[string]ViewContext) Input {
	t.Helper()
	parsed := resolve.SourceFromParse(syntax.Parse([]byte(source)))
	resolved := resolve.Resolve(resolve.Input{Mode: resolve.CompileProject, EntryPath: "document.ldl", Project: resolve.ProjectInput{Files: map[string]resolve.SourceFile{"document.ldl": parsed}}})
	defined := definition.Compile(definition.Input{Resolve: resolved})
	graphed := graph.Compile(graph.Input{Resolve: resolved, Definition: defined})
	queried := query.Compile(query.Input{Resolve: resolved, Definition: defined, Graph: graphed})
	views := make([]ViewContext, 0, len(contexts))
	for _, declaration := range resolved.Declarations {
		if declaration.Kind != resolve.KindView {
			continue
		}
		context, ok := contexts[declaration.ID]
		if !ok {
			t.Fatalf("missing test context for View %q", declaration.ID)
		}
		if context.Category == "" {
			if context.Shape == ShapeDiff {
				context.Category = CategoryDiff
				context.DiffSource = true
			} else {
				context.Category = CategoryTopology
			}
		}
		if context.StatePolicy == "" {
			context.StatePolicy = query.StateNone
		}
		context.Address = declaration.Address
		context.Generation = resolved.Generation()
		views = append(views, context)
	}
	input := Input{Resolve: resolved, Definition: defined, Graph: graphed, Query: queried, Views: views}
	if resolved.HasErrors || defined.HasErrors || graphed.HasErrors || queried.HasErrors {
		t.Fatalf("invalid test fixture: resolve=%+v definition=%+v graph=%+v query=%+v", resolved.Diagnostics, defined.Diagnostics, graphed.Diagnostics, queried.Diagnostics)
	}
	return input
}

func assertError(t *testing.T, result Result, contains string) {
	t.Helper()
	if !result.HasErrors || !diagnosticContains(result.Diagnostics, contains) {
		t.Fatalf("Compile() HasErrors=%v diagnostics=%+v, want %q", result.HasErrors, result.Diagnostics, contains)
	}
}

func diagnosticContains(diagnostics []resolve.Diagnostic, text string) bool {
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, text) {
			return true
		}
	}
	return false
}

func reverse[T any](values []T) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

const exportSourceTemplate = `project p "Project" {}

query q "Query" {
  select {}
}

view v "View" topology {
  source query q {}
  diagram {}
  EXPORTS
}
`

const allExportSource = `project p "Project" {}

query q "Query" {
  select {}
}

view diagram "Diagram" topology {
  source query q {}
  diagram {
    composed
  }
  export json json "diagram.json" {
    fidelity lossless
    source_refs
    diagnostics
    state_summary
  }
  export yaml yaml "diagram.yaml" {
    fidelity lossless
    source_refs
  }
  export svg svg "diagram.svg" {
    fidelity visual_only
    width 640
    height 480
    scale 2.5
    background "#aabbccdd"
  }
  export png png "diagram.png" {
    fidelity visual_only
    exporter_profile "layerdraw/png@1"
  }
  export pdf pdf "diagram.pdf" {
    fidelity visual_only
    page_size ledger
    orientation landscape
    fit width
    legend
  }
  export html html "diagram.html" {
    fidelity traceable_summary
    source_refs
    interactive
    embed_assets
  }
  export csv csv "diagram.csv" {
    fidelity traceable_summary
    source_refs
    bundle
    header
    source_manifest
  }
  export tsv tsv "diagram.tsv" {
    fidelity lossy
  }
  export xlsx xlsx "diagram.xlsx" {
    fidelity lossless
    source_refs
    lookup_sheets
    hidden_ids
    formulas
    view_data_json
  }
  export pptx pptx "diagram.pptx" {
    fidelity visual_only
  }
  export docx docx "diagram.docx" {
    fidelity visual_only
  }
  export drawio drawio "diagram.drawio" {
    fidelity visual_only
    source_refs
    source_manifest
  }
}

view table "Table" inventory {
  source query q {}
  table {}
  export markdown markdown "table.md" {
    fidelity lossy
  }
}

view tree "Tree" hierarchy {
  source query q {}
  tree {}
  export mermaid mermaid "tree.mmd" {
    fidelity traceable_summary
    source_refs
    source_manifest
  }
}

view flow "Flow" flow {
  source query q {}
  flow {}
  export bpmn bpmn "flow.bpmn" {
    fidelity lossy
    source_manifest
  }
}
`
