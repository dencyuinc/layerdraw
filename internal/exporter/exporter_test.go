// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package exporter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

func TestProfilesFailuresAndResourceValidationAreClosed(t *testing.T) {
	available := Profiles()
	if len(available) != 3 || available[0].Format != semantic.ExportFormatCsv {
		t.Fatalf("profiles=%+v", available)
	}
	cause := errors.New("private provider detail")
	value := failure(FailureSerializer, cause)
	if value.Error() != string(FailureSerializer) || !errors.Is(value, cause) {
		t.Fatalf("failure boundary=%v", value)
	}
	resource := []byte("resource")
	resourceDigest := digest(resource)
	if err := validateResources([]protocolcommon.Digest{resourceDigest}, []Resource{{Digest: resourceDigest, Bytes: resource}}, FailureAssetMissing); err != nil {
		t.Fatal(err)
	}
	if err := validateResources([]protocolcommon.Digest{resourceDigest}, nil, FailureAssetMissing); !IsFailure(err, FailureAssetMissing) {
		t.Fatalf("missing=%v", err)
	}
	if err := validateResources([]protocolcommon.Digest{resourceDigest}, []Resource{{Digest: resourceDigest, Bytes: []byte("forged")}}, FailureAssetMissing); !IsFailure(err, FailureAssetMissing) {
		t.Fatalf("forged=%v", err)
	}
}

func TestClosedScalarFormattingAndStoreInputs(t *testing.T) {
	truth := true
	integer := protocolcommon.CanonicalSafeInteger("42")
	number := semantic.CanonicalFiniteDecimal("3.5")
	text := "value"
	address := semantic.StableAddress("ldl:project:p:entity:a")
	set := []string{"a", "b"}
	values := []semantic.ViewDataValue{
		{Kind: "scalar", Scalar: &semantic.RecipeScalar{Kind: "boolean", BooleanValue: &truth}},
		{Kind: "scalar", Scalar: &semantic.RecipeScalar{Kind: "integer", IntegerValue: &integer}},
		{Kind: "scalar", Scalar: &semantic.RecipeScalar{Kind: "number", NumberValue: &number}},
		{Kind: "scalar", Scalar: &semantic.RecipeScalar{Kind: "string", StringValue: &text}},
		{Kind: "address", StableAddress: &address}, {Kind: "set", StringSet: &set}, {Kind: "empty"},
	}
	for _, value := range values {
		if got := viewValueString(value); value.Kind != "empty" && got == "" {
			t.Fatalf("format=%+v", value)
		}
	}
	if _, err := NewAssetStore("relative"); err == nil {
		t.Fatal("relative asset root accepted")
	}
	if _, err := NewPreviewStore("relative", nil); err == nil {
		t.Fatal("incomplete preview store accepted")
	}
	assets, _ := NewAssetStore(filepath.Join(t.TempDir(), "assets"))
	previews, _ := NewPreviewStore(filepath.Join(t.TempDir(), "previews"), assets)
	if err := previews.Put(context.Background(), "../unsafe", PreviewMetadata{}, nil); !IsFailure(err, FailurePreviewIncompatible) {
		t.Fatalf("unsafe preview id=%v", err)
	}
	if _, err := previews.Load(context.Background(), "../unsafe", PreviewExpectation{}); !IsFailure(err, FailurePreviewIncompatible) {
		t.Fatalf("unsafe load=%v", err)
	}
}

func TestNativeJSONSerializerIsDeterministicAndBindsCompleteProvenance(t *testing.T) {
	plan, view := fixturePlanAndView(t)
	first, err := Serialize(context.Background(), SerializeInput{Plan: plan, ViewData: view})
	if err != nil {
		if typed, ok := err.(*Failure); ok {
			t.Fatalf("%v: %v", err, typed.err)
		}
		t.Fatal(err)
	}
	second, err := Serialize(context.Background(), SerializeInput{Plan: plan, ViewData: view})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Artifacts) != 1 || !bytes.Equal(first.Artifacts[0].Bytes, second.Artifacts[0].Bytes) || !bytes.Equal(first.SourceManifestJSON, second.SourceManifestJSON) {
		t.Fatal("native serialization is not deterministic")
	}
	if first.Artifacts[0].ContentDigest != digest(first.Artifacts[0].Bytes) || first.SourceManifest.ViewDataHash != plan.ViewDataHash || first.SourceManifest.InvocationHash != plan.InvocationHash || first.SourceManifest.Revision != view.Revision {
		t.Fatalf("incomplete provenance: %+v", first.SourceManifest)
	}
	if _, err := semantic.DecodeExportSourceManifest(first.SourceManifestJSON); err != nil {
		t.Fatalf("manifest is not canonical protocol data: %v", err)
	}
	view.ViewAddress = "ldl:project:p:view:forged"
	if _, err := Serialize(context.Background(), SerializeInput{Plan: plan, ViewData: view}); !IsFailure(err, FailureInputMismatch) {
		t.Fatalf("forged view accepted: %v", err)
	}
}

func TestSerializerRejectsUnsupportedProfilesMismatchesAndBounds(t *testing.T) {
	plan, view := fixturePlanAndView(t)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Serialize(cancelled, SerializeInput{Plan: plan, ViewData: view}); !IsFailure(err, FailureCancelled) {
		t.Fatalf("cancel=%v", err)
	}
	if _, err := Serialize(context.Background(), SerializeInput{Plan: plan, ViewData: view, MaxInputBytes: 1}); !IsFailure(err, FailureSerializer) {
		t.Fatalf("input limit=%v", err)
	}
	if _, err := Serialize(context.Background(), SerializeInput{Plan: plan, ViewData: view, MaxOutputBytes: 1}); !IsFailure(err, FailureSerializer) {
		t.Fatalf("output limit=%v", err)
	}

	profileMismatch := plan
	profileMismatch.SerializerProfile.ID = "layerdraw/other@1"
	if _, err := Serialize(context.Background(), SerializeInput{Plan: profileMismatch, ViewData: view}); !IsFailure(err, FailureProfile) {
		t.Fatalf("profile mismatch=%v", err)
	}
	recipeMismatch := plan
	recipeMismatch.RecipeAddress = "ldl:project:p:view:other:export:json"
	if _, err := Serialize(context.Background(), SerializeInput{Plan: recipeMismatch, ViewData: view}); !IsFailure(err, FailureInputMismatch) {
		t.Fatalf("recipe mismatch=%v", err)
	}
	hashMismatch := plan
	hashMismatch.ViewDataHash = protocolcommon.Digest("sha256:" + strings.Repeat("0", 64))
	if _, err := Serialize(context.Background(), SerializeInput{Plan: hashMismatch, ViewData: view}); !IsFailure(err, FailureInputMismatch) {
		t.Fatalf("hash mismatch=%v", err)
	}
	multiple := plan
	second := multiple.Artifacts[0]
	second.Role, second.LogicalPath, second.Primary = "support", "support.json", false
	multiple.Artifacts = append(multiple.Artifacts, second)
	if _, err := Serialize(context.Background(), SerializeInput{Plan: multiple, ViewData: view}); !IsFailure(err, FailureProfile) {
		t.Fatalf("multiple artifact=%v", err)
	}

	unsupported := plan
	unsupported.Format = semantic.ExportFormatYaml
	unsupported.SerializerOptions.Kind = semantic.ExportFormatYaml
	unsupported.ExporterProfile.Format, unsupported.SerializerProfile.Format = semantic.ExportFormatYaml, semantic.ExportFormatYaml
	unsupported.ExporterProfile.ID, unsupported.SerializerProfile.ID = "layerdraw/yaml@1", "layerdraw/yaml@1"
	unsupported.Artifacts[0].LogicalPath, unsupported.Artifacts[0].MediaType = "view.yaml", "application/yaml"
	if _, err := Serialize(context.Background(), SerializeInput{Plan: unsupported, ViewData: view}); !IsFailure(err, FailureUnsupported) {
		t.Fatalf("unsupported profile=%v", err)
	}

	table := tableFixture(t)
	csvPlan := delimitedPlan(t, table, semantic.ExportFormatCsv)
	encoded, _ := semantic.EncodeViewData(view)
	csvPlan.ViewDataHash, _ = viewDataHash(encoded)
	csvPlan.RecipeAddress = semantic.ViewExportAddress(string(view.ViewAddress) + ":export:csv")
	if _, err := Serialize(context.Background(), SerializeInput{Plan: csvPlan, ViewData: view}); !IsFailure(err, FailureUnsupported) {
		t.Fatalf("unsupported shape=%v", err)
	}
}

func TestNativeCSVAndTSVProfilesCoverLargeTableDeterministically(t *testing.T) {
	view := tableFixture(t)
	for _, format := range []semantic.ExportFormat{semantic.ExportFormatCsv, semantic.ExportFormatTsv} {
		plan := delimitedPlan(t, view, format)
		result, err := Serialize(context.Background(), SerializeInput{Plan: plan, ViewData: view, MaxOutputBytes: 32 << 20})
		if err != nil {
			if typed, ok := err.(*Failure); ok {
				t.Fatalf("%s: %v: %v", format, err, typed.err)
			}
			t.Fatalf("%s: %v", format, err)
		}
		separator := ","
		if format == semantic.ExportFormatTsv {
			separator = "\t"
		}
		if !strings.Contains(string(result.Artifacts[0].Bytes), separator) || len(bytes.Split(result.Artifacts[0].Bytes, []byte("\n"))) < 2 {
			t.Fatalf("%s output=%q", format, result.Artifacts[0].Bytes)
		}
		repeated, err := Serialize(context.Background(), SerializeInput{Plan: plan, ViewData: view})
		if err != nil || !bytes.Equal(result.Artifacts[0].Bytes, repeated.Artifacts[0].Bytes) {
			t.Fatalf("%s not deterministic: %v", format, err)
		}
	}
}

func TestMatrixDelimitedSerializationAndValueKinds(t *testing.T) {
	rowKey, columnKey := semantic.ViewDataItemKey("vdi:matrix-axis:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"), semantic.ViewDataItemKey("vdi:matrix-axis:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")
	integer := protocolcommon.CanonicalInt64("7")
	view := semantic.ViewData{Kind: "matrix", Matrix: &semantic.MatrixViewData{RowAxis: []semantic.MatrixAxisItem{{Key: rowKey, Label: "Row"}}, ColumnAxis: []semantic.MatrixAxisItem{{Key: columnKey, Label: "Column"}}, Cells: []semantic.MatrixCell{{RowKey: rowKey, ColumnKey: columnKey, DisplayValue: semantic.MatrixDisplayValue{Kind: "integer", Integer: &integer}}}}}
	encoded, err := serializeDelimited(semantic.ExportFormatCsv, view)
	if err != nil || !strings.Contains(string(encoded), "Row,7") {
		t.Fatalf("matrix=%q err=%v", encoded, err)
	}
	truth := true
	set := []string{"a", "b"}
	attributes := []semantic.MatrixAttributeItem{}
	for _, display := range []semantic.MatrixDisplayValue{{Kind: "boolean", Boolean: &truth}, {Kind: "string_set", StringSet: &set}, {Kind: "attributes", Attributes: &attributes}, {Kind: "empty"}} {
		_ = matrixValueString(display)
	}
	if _, err := serializeDelimited(semantic.ExportFormatCsv, semantic.ViewData{Kind: "diagram"}); !IsFailure(err, FailureUnsupported) {
		t.Fatalf("diagram csv=%v", err)
	}
}

func TestResourcesAssetsAndMissingAssetPreviewFallback(t *testing.T) {
	root := t.TempDir()
	assets, err := NewAssetStore(filepath.Join(root, "assets"))
	if err != nil {
		t.Fatal(err)
	}
	value := bytes.Repeat([]byte("asset"), 1<<15)
	digestValue, err := assets.Import(context.Background(), "application/octet-stream", value, nil)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := assets.Resolve(context.Background(), digestValue)
	if err != nil || !bytes.Equal(loaded, value) {
		t.Fatalf("asset round trip: %v", err)
	}
	wrong := protocolcommon.Digest("sha256:" + strings.Repeat("0", 64))
	if _, err := assets.Import(context.Background(), "application/octet-stream", value, &wrong); !IsFailure(err, FailureDigestMismatch) {
		t.Fatalf("digest mismatch accepted: %v", err)
	}
	if _, err := assets.Import(context.Background(), "image/svg+xml", []byte(`<svg><script>alert(1)</script></svg>`), nil); !IsFailure(err, FailureUnsafeAsset) {
		t.Fatalf("unsafe SVG accepted: %v", err)
	}
	if _, err := assets.Import(context.Background(), "application/javascript", []byte("alert(1)"), nil); !IsFailure(err, FailureUnsafeAsset) {
		t.Fatalf("executable accepted: %v", err)
	}
	if repeated, err := assets.Import(context.Background(), "application/octet-stream", value, &digestValue); err != nil || repeated != digestValue {
		t.Fatalf("dedupe=%s %v", repeated, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := assets.Import(cancelled, "application/octet-stream", value, nil); !IsFailure(err, FailureCancelled) {
		t.Fatalf("cancel import=%v", err)
	}
	if _, err := assets.Resolve(cancelled, digestValue); !IsFailure(err, FailureCancelled) {
		t.Fatalf("cancel resolve=%v", err)
	}
	if _, err := assets.Resolve(context.Background(), wrong); !IsFailure(err, FailureAssetMissing) {
		t.Fatalf("missing resolve=%v", err)
	}

	previews, err := NewPreviewStore(filepath.Join(root, "previews"), assets)
	if err != nil {
		t.Fatal(err)
	}
	artifact := []byte("derived preview")
	metadata := PreviewMetadata{SchemaVersion: 1, ArtifactDigest: digest(artifact), InvocationHash: digest([]byte("invocation")), RevisionID: "revision:1", ProfileDigest: digest([]byte("profile")), MediaType: "text/plain", AssetDigests: []protocolcommon.Digest{wrong}}
	if err := previews.Put(context.Background(), "preview_1", metadata, artifact); err != nil {
		t.Fatal(err)
	}
	badMetadata := metadata
	badMetadata.ArtifactDigest = wrong
	if err := previews.Put(context.Background(), "preview_bad", badMetadata, artifact); !IsFailure(err, FailurePreviewIncompatible) {
		t.Fatalf("bad metadata=%v", err)
	}
	preview, err := previews.Load(context.Background(), "preview_1", PreviewExpectation{InvocationHash: metadata.InvocationHash, RevisionID: metadata.RevisionID, ProfileDigest: metadata.ProfileDigest, MediaType: metadata.MediaType})
	if err != nil || preview.SourceOfTruth || len(preview.MissingAssets) != 1 {
		t.Fatalf("missing asset fallback=%+v err=%v", preview, err)
	}
	if _, err := previews.Load(context.Background(), "preview_1", PreviewExpectation{InvocationHash: metadata.InvocationHash, RevisionID: "revision:2", ProfileDigest: metadata.ProfileDigest, MediaType: metadata.MediaType}); !IsFailure(err, FailurePreviewStale) {
		t.Fatalf("stale preview accepted: %v", err)
	}
	if _, err := previews.Load(context.Background(), "preview_1", PreviewExpectation{InvocationHash: metadata.InvocationHash, RevisionID: metadata.RevisionID, ProfileDigest: wrong, MediaType: metadata.MediaType}); !IsFailure(err, FailurePreviewIncompatible) {
		t.Fatalf("profile mismatch=%v", err)
	}
	// Re-open the stores to prove restart durability and digest revalidation.
	restartedAssets, _ := NewAssetStore(filepath.Join(root, "assets"))
	restarted, _ := NewPreviewStore(filepath.Join(root, "previews"), restartedAssets)
	if _, err := restarted.Load(context.Background(), "preview_1", PreviewExpectation{InvocationHash: metadata.InvocationHash, RevisionID: metadata.RevisionID, ProfileDigest: metadata.ProfileDigest, MediaType: metadata.MediaType}); err != nil {
		t.Fatalf("restart: %v", err)
	}
	corruptDir := filepath.Join(root, "previews", "corrupt")
	_ = os.MkdirAll(corruptDir, 0o700)
	_ = os.WriteFile(filepath.Join(corruptDir, "metadata.json"), []byte(`{}`), 0o600)
	if _, err := restarted.Load(context.Background(), "corrupt", PreviewExpectation{}); !IsFailure(err, FailurePreviewIncompatible) {
		t.Fatalf("corrupt metadata=%v", err)
	}
	if _, err := restarted.Load(context.Background(), "missing", PreviewExpectation{}); !IsFailure(err, FailurePreviewStale) {
		t.Fatalf("missing preview=%v", err)
	}
	assetPath := filepath.Join(root, "assets", strings.TrimPrefix(string(digestValue), "sha256:"))
	_ = os.WriteFile(assetPath, []byte("corrupt"), 0o600)
	if _, err := restartedAssets.Import(context.Background(), "application/octet-stream", value, &digestValue); !IsFailure(err, FailureDigestMismatch) {
		t.Fatalf("corrupt durable asset=%v", err)
	}
}

func TestAtomicDestinationCancellationAndFailureNeverPublishPartialBytes(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "artifact.json")
	if err := (AtomicFileStore{}).Publish(context.Background(), destination, []byte("complete")); err != nil {
		t.Fatal(err)
	}
	if err := (AtomicFileStore{}).Publish(context.Background(), "relative", []byte("bad")); !IsFailure(err, FailureDestination) {
		t.Fatalf("relative=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (AtomicFileStore{}).Publish(ctx, destination, []byte("partial")); !IsFailure(err, FailureCancelled) {
		t.Fatalf("cancel=%v", err)
	}
	if value, _ := os.ReadFile(destination); string(value) != "complete" {
		t.Fatalf("cancel replaced published artifact: %q", value)
	}
	if err := (AtomicFileStore{}).Publish(context.Background(), filepath.Join(root, "missing", "artifact"), []byte("partial")); !IsFailure(err, FailureDestination) {
		t.Fatalf("destination failure=%v", err)
	}
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".layerdraw-export-") {
			t.Fatalf("partial file survived: %s", entry.Name())
		}
	}
}

func TestAtomicPublishSetReplacesTogetherAndRollsBackBeforeCommit(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "view.json"), filepath.Join(root, "view.sources.json")
	if err := os.WriteFile(first, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (AtomicFileStore{}).PublishSet(context.Background(), map[string][]byte{first: []byte("new"), second: []byte("manifest")}); err != nil {
		t.Fatal(err)
	}
	if value, _ := os.ReadFile(first); string(value) != "new" {
		t.Fatalf("first=%q", value)
	}
	if value, _ := os.ReadFile(second); string(value) != "manifest" {
		t.Fatalf("second=%q", value)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (AtomicFileStore{}).PublishSet(ctx, map[string][]byte{first: []byte("bad"), second: []byte("bad")}); !IsFailure(err, FailureCancelled) {
		t.Fatalf("cancel=%v", err)
	}
	if value, _ := os.ReadFile(first); string(value) != "new" {
		t.Fatalf("cancel replaced first=%q", value)
	}
	if err := (AtomicFileStore{}).PublishSet(context.Background(), map[string][]byte{}); !IsFailure(err, FailureDestination) {
		t.Fatalf("empty=%v", err)
	}
	if err := (AtomicFileStore{}).PublishSet(context.Background(), map[string][]byte{first: []byte("x"), filepath.Join(t.TempDir(), "other"): []byte("y")}); !IsFailure(err, FailureDestination) {
		t.Fatalf("cross-directory=%v", err)
	}
}

func TestExternalImportProducesGeneratedPreviewOperationsOnly(t *testing.T) {
	value := []byte(`{"format":"layerdraw-semantic-operations","schema_version":1,"operations":[{"operation":"delete_subject","target_address":"ldl:project:p:entity:a"}]}`)
	preview, err := ImportOperationsJSON(context.Background(), value, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Profile != OperationsJSONProfile || len(preview.Batch.Operations) != 1 || bytes.Contains(preview.Canonical, []byte("project p")) {
		t.Fatalf("unexpected import preview: %+v", preview)
	}
	if _, err := ImportOperationsJSON(context.Background(), append(value, []byte("{}")...), 0, 0); !IsFailure(err, FailureImportInvalid) {
		t.Fatalf("trailing data accepted: %v", err)
	}
	if _, err := ImportOperationsJSON(context.Background(), append(value, []byte("{")...), 0, 0); !IsFailure(err, FailureImportInvalid) {
		t.Fatalf("malformed trailing data accepted: %v", err)
	}
	if _, err := ImportOperationsJSON(context.Background(), bytes.Repeat([]byte("x"), 1024), 32, 1); !IsFailure(err, FailureImportInvalid) {
		t.Fatalf("limit ignored: %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ImportOperationsJSON(cancelled, value, 0, 0); !IsFailure(err, FailureCancelled) {
		t.Fatalf("cancel ignored: %v", err)
	}
	if _, err := ImportOperationsJSON(context.Background(), value, 1, 1); !IsFailure(err, FailureImportInvalid) {
		t.Fatalf("byte limit ignored: %v", err)
	}
}

func fixturePlanAndView(t *testing.T) (semantic.ExportPlan, semantic.ViewData) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "fixtures", "conformance", "export-plan-transport-parity-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		ExportPlan json.RawMessage `json:"export_plan"`
		Input      struct {
			ViewData json.RawMessage `json:"view_data"`
		} `json:"input"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	plan, err := semantic.DecodeExportPlan(fixture.ExportPlan)
	if err != nil {
		t.Fatal(err)
	}
	view, err := semantic.DecodeViewData(fixture.Input.ViewData)
	if err != nil {
		t.Fatal(err)
	}
	plan.RequiredFontDigests = []protocolcommon.Digest{}
	return plan, view
}

func tableFixture(t *testing.T) semantic.ViewData {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "tests", "conformance", "testdata", "viewdata_conformance_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Expected struct {
				Normalized struct {
					Payload struct {
						ViewData json.RawMessage `json:"view_data"`
					} `json:"payload"`
				} `json:"normalized_response"`
			} `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, testCase := range fixture.Cases {
		if len(testCase.Expected.Normalized.Payload.ViewData) != 0 {
			view, err := semantic.DecodeViewData(testCase.Expected.Normalized.Payload.ViewData)
			if err == nil && view.Kind == "table" {
				return view
			}
		}
	}
	t.Fatal("table fixture unavailable")
	return semantic.ViewData{}
}

func delimitedPlan(t *testing.T, view semantic.ViewData, format semantic.ExportFormat) semantic.ExportPlan {
	t.Helper()
	plan, _ := fixturePlanAndView(t)
	extension := ".csv"
	media := "text/csv"
	if format == semantic.ExportFormatTsv {
		extension, media = ".tsv", "text/tab-separated-values"
	}
	plan.Format = format
	plan.SerializerOptions = semantic.ExportOptions{Kind: format}
	bundle, header, sourceManifest := false, true, true
	plan.SerializerOptions.Bundle, plan.SerializerOptions.Header, plan.SerializerOptions.SourceManifest = &bundle, &header, &sourceManifest
	plan.ExporterProfile.Format, plan.SerializerProfile.Format = format, format
	plan.ExporterProfile.ID, plan.SerializerProfile.ID = "layerdraw/"+string(format)+"@1", "layerdraw/"+string(format)+"@1"
	plan.RecipeAddress = semantic.ViewExportAddress(string(view.ViewAddress) + ":export:" + string(format))
	plan.Artifacts = []semantic.ExportArtifactEntry{{LogicalPath: "table" + extension, MediaType: media, Primary: true, Role: "table"}}
	plan.NativeMaximumFidelity, plan.EffectiveMaximumFidelity, plan.RequestedFidelity = semantic.ExportFidelityLossy, semantic.ExportFidelityLossy, semantic.ExportFidelityLossy
	plan.SourceManifestRequired = true
	manifestPath := "table.sources.json"
	plan.SourceManifestPath = &manifestPath
	plan.RequiredAssetDigests, plan.RequiredFontDigests = []protocolcommon.Digest{}, []protocolcommon.Digest{}
	plan.Units = []semantic.ExportPlanUnit{{ArtifactRole: "table", Kind: "sheet", Order: "0", Role: "table", UnitID: "unit:table", ViewdataKeys: []string{"viewdata-root"}}}
	role, unit, locator := "table", "unit:table", "unit:table:viewdata-root"
	plan.Representations = []semantic.ExportRepresentation{{ArtifactRole: &role, Disposition: "tabular", Locator: &locator, Source: view.Source, UnitID: &unit, ViewdataKey: "viewdata-root"}}
	encoded, err := semantic.EncodeViewData(view)
	if err != nil {
		t.Fatal(err)
	}
	plan.ViewDataHash, err = viewDataHash(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := semantic.EncodeExportPlan(plan); err != nil {
		t.Fatalf("invalid %s plan: %v", format, err)
	}
	return plan
}
