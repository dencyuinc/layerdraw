// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package exportrecipe compiles nested LDL Export declarations into closed,
// typed static recipes. It intentionally does not accept ViewData and does not
// produce ExportPlan, artifact bytes, layouts, or serializer instructions.
package exportrecipe

import (
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
)

type Input struct {
	Resolve    resolve.Result
	Definition definition.Result
	Graph      graph.Result
	Query      query.Result
	Views      []ViewContext
	Registry   *ProfileRegistry
}

type ViewContext struct {
	Address         string
	Category        Category
	Shape           Shape
	DiffSource      bool
	DiagramComposed bool
	StatePolicy     query.StatePolicy
	Generation      resolve.StageGeneration
}

type Result struct {
	stageGeneration resolve.StageGeneration
	Recipes         []Recipe
	Diagnostics     []resolve.Diagnostic
	HasErrors       bool
}

// CloneRecipes returns recipes whose mutable option storage is independent of
// the input. Static recipe results may be projected into multiple parent
// result views, and mutating one projection must not corrupt another.
func CloneRecipes(recipes []Recipe) []Recipe {
	out := append([]Recipe{}, recipes...)
	for index := range out {
		out[index].Options = cloneOptions(recipes[index].Options)
	}
	return out
}

func cloneOptions(options Options) Options {
	out := options
	if options.Structured != nil {
		value := *options.Structured
		out.Structured = &value
	}
	if options.Image != nil {
		value := *options.Image
		out.Image = &value
	}
	if options.Page != nil {
		value := *options.Page
		out.Page = &value
	}
	if options.HTML != nil {
		value := *options.HTML
		out.HTML = &value
	}
	if options.Delimited != nil {
		value := *options.Delimited
		out.Delimited = &value
	}
	if options.XLSX != nil {
		value := *options.XLSX
		out.XLSX = &value
	}
	if options.Manifest != nil {
		value := *options.Manifest
		out.Manifest = &value
	}
	return out
}

func (r Result) MatchesResolve(resolved resolve.Result) bool {
	return r.stageGeneration.Matches(resolved.Generation())
}

func (r Result) Generation() resolve.StageGeneration {
	return r.stageGeneration
}

type Category string

const (
	CategoryTopology   Category = "topology"
	CategoryInventory  Category = "inventory"
	CategoryDependency Category = "dependency"
	CategoryHierarchy  Category = "hierarchy"
	CategoryFlow       Category = "flow"
	CategoryImpact     Category = "impact"
	CategoryDiff       Category = "diff"
	CategoryContext    Category = "context"
)

type Shape string

const (
	ShapeDiagram Shape = "diagram"
	ShapeTable   Shape = "table"
	ShapeMatrix  Shape = "matrix"
	ShapeTree    Shape = "tree"
	ShapeFlow    Shape = "flow"
	ShapeContext Shape = "context"
	ShapeDiff    Shape = "diff"
)

type Format string

const (
	FormatJSON     Format = "json"
	FormatYAML     Format = "yaml"
	FormatSVG      Format = "svg"
	FormatPNG      Format = "png"
	FormatPDF      Format = "pdf"
	FormatHTML     Format = "html"
	FormatCSV      Format = "csv"
	FormatTSV      Format = "tsv"
	FormatXLSX     Format = "xlsx"
	FormatMarkdown Format = "markdown"
	FormatPPTX     Format = "pptx"
	FormatDOCX     Format = "docx"
	FormatMermaid  Format = "mermaid"
	FormatBPMN     Format = "bpmn"
	FormatDrawIO   Format = "drawio"
)

type Fidelity string

const (
	FidelityLossless         Fidelity = "lossless"
	FidelityTraceableSummary Fidelity = "traceable_summary"
	FidelityVisualOnly       Fidelity = "visual_only"
	FidelityLossy            Fidelity = "lossy"
)

type Recipe struct {
	ID                       string
	Address                  string
	ViewAddress              string
	Format                   Format
	Extension                string
	Filename                 string
	Fidelity                 Fidelity
	SourceRefs               bool
	ExporterProfile          ExporterProfileRef
	Options                  Options
	NativeMaximumFidelity    Fidelity
	EffectiveMaximumFidelity Fidelity
	FidelityBasis            FidelityBasis
	RequiresSourceManifest   bool
}

type FidelityBasis string

const (
	FidelityBasisNative           FidelityBasis = "native"
	FidelityBasisEmbeddedViewData FidelityBasis = "embedded_viewdata"
)

type ExporterProfileRef struct {
	ID                    string
	Format                Format
	RegistrySchemaVersion int
	RegistryDigest        string
	SpecificationDigest   string
}

type Options struct {
	Kind       Format
	Structured *StructuredOptions
	Image      *ImageOptions
	Page       *PageOptions
	HTML       *HTMLOptions
	Delimited  *DelimitedOptions
	XLSX       *XLSXOptions
	Manifest   *ManifestOptions
}

type StructuredOptions struct {
	Diagnostics  bool
	StateSummary bool
}

type Dimension struct {
	Auto  bool
	Value int64
}

type ImageOptions struct {
	Width      Dimension
	Height     Dimension
	Scale      float64
	Background string
}

type PageOptions struct {
	PageSize    PageSize
	Orientation Orientation
	Fit         Fit
	Legend      bool
}

type PageSize string

const (
	PageA3     PageSize = "a3"
	PageA4     PageSize = "a4"
	PageLetter PageSize = "letter"
	PageLegal  PageSize = "legal"
	PageLedger PageSize = "ledger"
)

type Orientation string

const (
	OrientationPortrait  Orientation = "portrait"
	OrientationLandscape Orientation = "landscape"
)

type Fit string

const (
	FitNone  Fit = "none"
	FitPage  Fit = "page"
	FitWidth Fit = "width"
)

type HTMLOptions struct {
	Interactive bool
	EmbedAssets bool
}

type DelimitedOptions struct {
	Bundle         bool
	Header         bool
	SourceManifest bool
}

type XLSXOptions struct {
	Profile      XLSXProfile
	LookupSheets bool
	HiddenIDs    bool
	Formulas     bool
	ViewDataJSON bool
}

type XLSXProfile string

const (
	XLSXTypeWorkbook             XLSXProfile = "type_workbook"
	XLSXDiagramWorkbook          XLSXProfile = "diagram_workbook"
	XLSXComposedDiagramWorkbook  XLSXProfile = "composed_diagram_workbook"
	XLSXMatrixWorkbook           XLSXProfile = "matrix_workbook"
	XLSXTreeWorkbook             XLSXProfile = "tree_workbook"
	XLSXImpactWorkbook           XLSXProfile = "impact_workbook"
	XLSXFlowWorkbook             XLSXProfile = "flow_workbook"
	XLSXDiffWorkbook             XLSXProfile = "diff_workbook"
	XLSXContextWorkbook          XLSXProfile = "context_workbook"
	XLSXDiagramInventoryWorkbook XLSXProfile = "diagram_inventory_workbook"
)

type ManifestOptions struct {
	SourceManifest bool
}

type ProfileRegistry struct {
	Format        string
	SchemaVersion int
	Digest        string
	Profiles      []ProfileSpecification
}

type ProfileSpecification struct {
	ID                  string
	Format              Format
	SpecificationDigest string
	Specification       []byte
}
