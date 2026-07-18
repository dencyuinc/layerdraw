// SPDX-License-Identifier: Apache-2.0

package conformance

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

func TestRenderExportCanonicalFixtureRoundTripsGeneratedSemanticCodecs(t *testing.T) {
	path := filepath.Join("..", "..", "schemas", "fixtures", "conformance", "render-export-contracts-v1.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		SchemaVersion        int             `json:"schema_version"`
		ExportPlan           json.RawMessage `json:"export_plan"`
		ExportSourceManifest json.RawMessage `json:"export_source_manifest"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.SchemaVersion != 1 {
		t.Fatalf("schema_version=%d", fixture.SchemaVersion)
	}
	plan, err := semantic.DecodeExportPlan(fixture.ExportPlan)
	if err != nil {
		t.Fatalf("decode ExportPlan: %v", err)
	}
	encodedPlan, err := semantic.EncodeExportPlan(plan)
	if err != nil {
		t.Fatalf("encode ExportPlan: %v", err)
	}
	if !renderExportJSONEqual(encodedPlan, fixture.ExportPlan) {
		t.Fatal("ExportPlan codec round trip changed the fixture")
	}
	manifest, err := semantic.DecodeExportSourceManifest(fixture.ExportSourceManifest)
	if err != nil {
		t.Fatalf("decode ExportSourceManifest: %v", err)
	}
	encodedManifest, err := semantic.EncodeExportSourceManifest(manifest)
	if err != nil {
		t.Fatalf("encode ExportSourceManifest: %v", err)
	}
	if !renderExportJSONEqual(encodedManifest, fixture.ExportSourceManifest) {
		t.Fatal("ExportSourceManifest codec round trip changed the fixture")
	}
	if plan.SerializerOptions.Kind != plan.Format || plan.SerializerProfile != manifest.SerializerProfile || plan.ExporterProfile != manifest.ExporterProfile {
		t.Fatalf("serializer authority is not explicit across Plan and Source Manifest: plan=%+v manifest=%+v", plan, manifest)
	}
}

func TestRenderDataStaysOutsideGoSemanticSchema(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "semantic", "v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Definitions map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	if _, exists := schema.Definitions["RenderData"]; exists {
		t.Fatal("RenderData must remain TypeScript-owned and outside the Go semantic schema")
	}
}

func renderExportJSONEqual(left, right []byte) bool {
	var l, r any
	return json.Unmarshal(left, &l) == nil && json.Unmarshal(right, &r) == nil && bytes.Equal(marshalRenderExportJSON(l), marshalRenderExportJSON(r))
}

func marshalRenderExportJSON(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}
