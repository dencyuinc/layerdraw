// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

func TestPlanExportInProcessMatchesTransportGolden(t *testing.T) {
	encoded, err := os.ReadFile("../../../schemas/fixtures/conformance/export-plan-transport-parity-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		SchemaVersion int64           `json:"schema_version"`
		Input         json.RawMessage `json:"input"`
		ExportPlan    json.RawMessage `json:"export_plan"`
	}
	if err := json.Unmarshal(encoded, &fixture); err != nil || fixture.SchemaVersion != 1 {
		t.Fatalf("fixture schema=%d err=%v", fixture.SchemaVersion, err)
	}
	payload, err := engineprotocol.DecodePlanExportInput(fixture.Input)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := mapPlanExportInput(payload)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := engine.New(engine.BuildInfo{}).PlanExport(context.Background(), mapped)
	if err != nil {
		t.Fatal(err)
	}
	actualJSON, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	actualPlan, err := semantic.DecodeExportPlan(actualJSON)
	if err != nil {
		t.Fatal(err)
	}
	expectedPlan, err := semantic.DecodeExportPlan(fixture.ExportPlan)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actualPlan, expectedPlan) {
		t.Fatalf("in-process ExportPlan differs from transport golden\nactual: %s\nexpected: %s", actualJSON, fixture.ExportPlan)
	}
	control, err := engineprotocol.EncodePlanExportRequestEnvelope(engineprotocol.PlanExportRequestEnvelope{
		Operation: engineprotocol.PlanExportRequestEnvelopeOperationValue, Payload: payload,
		Protocol: bootstrapProtocolRef(), RequestID: "plan-export-golden",
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := newCompileDispatcher(&fakeWorkbenchDriver{exportPlan: actual})
	prepared, terminal, err := dispatcher.PrepareDispatch(context.Background(), compileContext(t), OperationPlanExport, control)
	if err != nil || terminal != nil || prepared == nil {
		t.Fatalf("prepared=%v terminal=%+v err=%v", prepared, terminal, err)
	}
	response, err := prepared.ExecuteDispatch(context.Background(), &memoryBlobSource{}, &memoryBlobSink{})
	if err != nil || response.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	decoded, err := engineprotocol.DecodePlanExportResponseEnvelope(response.Control)
	if err != nil || decoded.Payload == nil || !reflect.DeepEqual(decoded.Payload.ExportPlan, expectedPlan) {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
}

func TestMapPlanExportInputPreservesCanonicalBoundaryValues(t *testing.T) {
	if err := remapExportValue(func() {}, &engine.ExportPlanSourceRefs{}); err == nil {
		t.Fatal("non-JSON export mapping input was accepted")
	}
	digest := protocolcommon.Digest("sha256:" + strings.Repeat("a", 64))
	revisionID := "r1"
	no := false
	source := semantic.ViewDataSourceRefs{AssetDigests: []protocolcommon.Digest{}, CellRefs: []semantic.ViewDataCellRef{}, EntityAddresses: []semantic.EntityAddress{}, LayerAddresses: []semantic.LayerAddress{}, RelationAddresses: []semantic.RelationAddress{}, RowAddresses: []semantic.StableAddress{}, State: semantic.ViewDataStateRefs{Reads: []semantic.ViewDataStateReadRef{}}, SubjectAddresses: []semantic.StableAddress{}}
	shape := semantic.ViewContextShape{GroupBy: "none", Incoming: true, Outgoing: true}
	payload := engineprotocol.PlanExportInput{
		ViewData: semantic.ViewData{Category: "context", Context: &semantic.ContextViewData{Groups: []semantic.ContextGroup{}}, Diagnostics: []semantic.Diagnostic{}, Kind: "context", ProjectAddress: "ldl:project:p", Revision: semantic.ViewRevision{DefinitionHash: &digest, Kind: "single", RevisionID: &revisionID}, Shape: semantic.ViewRecipeShape{Context: &shape, Kind: "context"}, Source: source, StateInput: semantic.ViewDataStateInputRef{Kind: "none"}, StatePolicy: "none", ViewAddress: "ldl:project:p:view:v"},
		Recipe:   semantic.ExportRecipe{Address: "ldl:project:p:view:v:export:e", EffectiveMaximumFidelity: "lossless", ExporterProfile: semantic.ExporterProfileRef{Format: "json", ID: "layerdraw/json@1", RegistryDigest: digest, RegistrySchemaVersion: 1, SpecificationDigest: digest}, Extension: ".json", Fidelity: "lossless", FidelityBasis: "native", Filename: "v.json", Format: "json", ID: "e", NativeMaximumFidelity: "lossless", Options: semantic.ExportOptions{Diagnostics: &no, Kind: "json", StateSummary: &no}, SourceRefs: true, ViewAddress: "ldl:project:p:view:v"},
	}
	payload.ResolvedRequirements = semantic.ResolvedExportProfileRequirements{SchemaVersion: 1, ExporterProfile: payload.Recipe.ExporterProfile, SerializerProfile: payload.Recipe.ExporterProfile, RequiredAssetDigests: []protocolcommon.Digest{}, RequiredFontDigests: []protocolcommon.Digest{digest}}
	mapped, err := mapPlanExportInput(payload)
	if err != nil || mapped.ViewData.ViewAddress != "ldl:project:p:view:v" || len(mapped.ViewData.CanonicalJSON) == 0 {
		t.Fatalf("mapped=%+v err=%v", mapped, err)
	}
	closure := runPlanExport(payload)
	if _, _, err := closure(context.Background(), &fakeWorkbenchDriver{}, nil); err == nil {
		t.Fatal("invalid fake plan output was accepted")
	}
	planned, err := engine.New(engine.BuildInfo{}).PlanExport(context.Background(), mapped)
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := closure(context.Background(), &fakeWorkbenchDriver{exportPlan: planned}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.(engineprotocol.PlanExportResult).ExportPlan.InvocationHash == "" {
		t.Fatal("mapped export plan lost its hash closure")
	}
	sentinel := errors.New("planner failed")
	if _, _, err := closure(context.Background(), &fakeWorkbenchDriver{err: sentinel}, nil); !errors.Is(err, sentinel) {
		t.Fatalf("planner error = %v", err)
	}
	payload.StateSummary = &semantic.ExternalStateSummary{
		DefinitionHash: digest, Format: semantic.ExternalStateSummaryFormatValue,
		Payload:     protocolcommon.JsonValue{Kind: protocolcommon.JsonValueKindBoolean, Boolean: true},
		PayloadHash: digest, SchemaVersion: 1, StateVersion: "s1",
	}
	mapped, err = mapPlanExportInput(payload)
	if err != nil || mapped.StateSummary == nil || len(mapped.StateSummary.CanonicalJSON) == 0 || len(mapped.StateSummary.PayloadCanonicalJSON) == 0 {
		t.Fatalf("state summary mapping=%+v err=%v", mapped.StateSummary, err)
	}
	invalidStateSummary := payload
	invalidStateSummary.StateSummary = &semantic.ExternalStateSummary{SchemaVersion: 0}
	if _, err := mapPlanExportInput(invalidStateSummary); err == nil {
		t.Fatal("invalid state summary was accepted")
	}
	invalidView := payload
	invalidView.ViewData.Kind = "invalid"
	if _, err := mapPlanExportInput(invalidView); err == nil {
		t.Fatal("invalid ViewData was accepted")
	}
	invalidRequirements := payload
	invalidRequirements.ResolvedRequirements.SchemaVersion = 0
	if _, err := mapPlanExportInput(invalidRequirements); err == nil {
		t.Fatal("invalid requirements were accepted")
	}
	payload.Recipe.Filename = "bad/path.json"
	if _, err := mapPlanExportInput(payload); err == nil {
		t.Fatal("invalid recipe was accepted")
	}
	if _, _, err := runPlanExport(payload)(context.Background(), &fakeWorkbenchDriver{}, nil); !engine.IsWorkbenchError(err, engine.WorkbenchErrorInputInvalid) {
		t.Fatalf("invalid dispatch error = %v", err)
	}
}

func TestPlanExportPreservesEveryClosedSerializerOptionVariant(t *testing.T) {
	digest := protocolcommon.Digest("sha256:" + strings.Repeat("a", 64))
	revisionID := "r1"
	source := semantic.ViewDataSourceRefs{AssetDigests: []protocolcommon.Digest{}, CellRefs: []semantic.ViewDataCellRef{}, EntityAddresses: []semantic.EntityAddress{}, LayerAddresses: []semantic.LayerAddress{}, RelationAddresses: []semantic.RelationAddress{}, RowAddresses: []semantic.StableAddress{}, State: semantic.ViewDataStateRefs{Reads: []semantic.ViewDataStateReadRef{}}, SubjectAddresses: []semantic.StableAddress{}}
	shape := semantic.ViewContextShape{GroupBy: "none", Incoming: true, Outgoing: true}
	viewData := semantic.ViewData{Category: "context", Context: &semantic.ContextViewData{Groups: []semantic.ContextGroup{}}, Diagnostics: []semantic.Diagnostic{}, Kind: "context", ProjectAddress: "ldl:project:p", Revision: semantic.ViewRevision{DefinitionHash: &digest, Kind: "single", RevisionID: &revisionID}, Shape: semantic.ViewRecipeShape{Context: &shape, Kind: "context"}, Source: source, StateInput: semantic.ViewDataStateInputRef{Kind: "none"}, StatePolicy: "none", ViewAddress: "ldl:project:p:view:v"}

	for _, test := range exportOptionVariants() {
		t.Run(string(test.format), func(t *testing.T) {
			profile := semantic.ExporterProfileRef{Format: test.format, ID: "layerdraw/" + string(test.format) + "@1", RegistryDigest: digest, RegistrySchemaVersion: 1, SpecificationDigest: digest}
			nativeMaximum := exportOptionNativeMaximum(test.format)
			payload := engineprotocol.PlanExportInput{
				ViewData:             viewData,
				Recipe:               semantic.ExportRecipe{Address: "ldl:project:p:view:v:export:e", EffectiveMaximumFidelity: nativeMaximum, ExporterProfile: profile, Extension: test.extension, Fidelity: "lossy", FidelityBasis: "native", Filename: "v" + test.extension, Format: test.format, ID: "e", NativeMaximumFidelity: nativeMaximum, Options: test.options, RequiresSourceManifest: true, SourceRefs: true, ViewAddress: "ldl:project:p:view:v"},
				ResolvedRequirements: semantic.ResolvedExportProfileRequirements{SchemaVersion: 1, ExporterProfile: profile, SerializerProfile: profile, RequiredAssetDigests: []protocolcommon.Digest{}, RequiredFontDigests: []protocolcommon.Digest{}},
			}
			mapped, err := mapPlanExportInput(payload)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := engine.New(engine.BuildInfo{}).PlanExport(context.Background(), mapped)
			if err != nil {
				t.Fatal(err)
			}
			result, _, err := runPlanExport(payload)(context.Background(), &fakeWorkbenchDriver{exportPlan: plan}, nil)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := engineprotocol.EncodePlanExportResult(result.(engineprotocol.PlanExportResult))
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := engineprotocol.DecodePlanExportResult(encoded)
			if err != nil {
				t.Fatal(err)
			}
			want, err := semantic.EncodeExportOptions(test.options)
			if err != nil {
				t.Fatal(err)
			}
			got, err := semantic.EncodeExportOptions(decoded.ExportPlan.SerializerOptions)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("serializer options changed across plan transport: got=%s want=%s err=%v", got, want, err)
			}
		})
	}
}

func exportOptionNativeMaximum(format semantic.ExportFormat) semantic.ExportFidelity {
	switch format {
	case "json", "yaml":
		return "lossless"
	case "xlsx", "html":
		return "traceable_summary"
	case "svg", "png", "pdf", "pptx", "docx", "drawio":
		return "visual_only"
	default:
		return "lossy"
	}
}

type exportOptionVariant struct {
	format    semantic.ExportFormat
	extension string
	options   semantic.ExportOptions
}

func exportOptionVariants() []exportOptionVariant {
	no := false
	auto := semantic.ExportDimension{Kind: "auto"}
	background := semantic.RasterBackground("transparent")
	scale := semantic.CanonicalPositiveFiniteDecimal("1")
	fit, orientation, pageSize := "page", "portrait", "a4"
	profile := "context_workbook"
	manifest := func(format semantic.ExportFormat) semantic.ExportOptions {
		return semantic.ExportOptions{Kind: format, SourceManifest: pointer(no)}
	}
	paged := func(format semantic.ExportFormat) semantic.ExportOptions {
		return semantic.ExportOptions{Fit: &fit, Kind: format, Legend: pointer(no), Orientation: &orientation, PageSize: &pageSize}
	}
	raster := func(format semantic.ExportFormat) semantic.ExportOptions {
		return semantic.ExportOptions{Background: &background, Height: &auto, Kind: format, Scale: &scale, Width: &auto}
	}
	return []exportOptionVariant{
		{format: "bpmn", extension: ".bpmn", options: manifest("bpmn")},
		{format: "csv", extension: ".csv", options: semantic.ExportOptions{Bundle: pointer(no), Header: pointer(no), Kind: "csv", SourceManifest: pointer(no)}},
		{format: "docx", extension: ".docx", options: paged("docx")},
		{format: "drawio", extension: ".drawio", options: manifest("drawio")},
		{format: "html", extension: ".html", options: semantic.ExportOptions{EmbedAssets: pointer(no), Interactive: pointer(no), Kind: "html"}},
		{format: "json", extension: ".json", options: semantic.ExportOptions{Diagnostics: pointer(no), Kind: "json", StateSummary: pointer(no)}},
		{format: "markdown", extension: ".md", options: manifest("markdown")},
		{format: "mermaid", extension: ".mmd", options: manifest("mermaid")},
		{format: "pdf", extension: ".pdf", options: paged("pdf")},
		{format: "png", extension: ".png", options: raster("png")},
		{format: "pptx", extension: ".pptx", options: paged("pptx")},
		{format: "svg", extension: ".svg", options: raster("svg")},
		{format: "tsv", extension: ".tsv", options: semantic.ExportOptions{Bundle: pointer(no), Header: pointer(no), Kind: "tsv", SourceManifest: pointer(no)}},
		{format: "xlsx", extension: ".xlsx", options: semantic.ExportOptions{Formulas: pointer(no), HiddenIDs: pointer(no), Kind: "xlsx", LookupSheets: pointer(no), Profile: &profile, ViewDataJSON: pointer(no)}},
		{format: "yaml", extension: ".yaml", options: semantic.ExportOptions{Diagnostics: pointer(no), Kind: "yaml", StateSummary: pointer(no)}},
	}
}
