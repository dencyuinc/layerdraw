// SPDX-License-Identifier: Apache-2.0

package conformance_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"testing"
)

func TestRenderPipelineConformanceFixtureIsCompleteAndClosed(t *testing.T) {
	t.Parallel()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "testdata", "render_pipeline_conformance_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		SchemaVersion int `json:"schema_version"`
		Corpus        []string
		DerivedCases  []string `json:"derived_cases"`
		Render        map[string]struct {
			Digest      string
			InputHash   string `json:"input_hash"`
			Bindings    int
			Diagnostics []string
		}
		Surfaces map[string]struct {
			Digest string
			Items  int
			Kind   string
		}
		Exports  map[string]json.RawMessage
		Failures map[string]any
		Empty    struct {
			Kind     string
			Bindings int
		}
		State struct {
			Policy                        string
			Input                         string
			RenderDigest                  string `json:"render_digest"`
			ViewerStatus                  string `json:"viewer_status"`
			SourceManifestDigest          string `json:"source_manifest_digest"`
			SourceManifestRepresentations int    `json:"source_manifest_representations"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	shapes := []string{"context", "diagram", "diff", "flow", "matrix", "table", "tree"}
	if fixture.SchemaVersion != 1 || !reflect.DeepEqual(sortedKeys(fixture.Render), shapes) || !reflect.DeepEqual(sortedKeys(fixture.Surfaces), shapes) {
		t.Fatalf("incomplete Render Pipeline fixture: schema=%d render=%v surfaces=%v", fixture.SchemaVersion, sortedKeys(fixture.Render), sortedKeys(fixture.Surfaces))
	}
	digest := regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	for _, shape := range shapes {
		render := fixture.Render[shape]
		surface := fixture.Surfaces[shape]
		if !digest.MatchString(render.Digest) || !digest.MatchString(render.InputHash) || render.Bindings < 1 || len(render.Diagnostics) != 0 {
			t.Fatalf("invalid %s RenderData proof: %+v", shape, render)
		}
		if !digest.MatchString(surface.Digest) || surface.Items != render.Bindings || surface.Kind != shape {
			t.Fatalf("invalid %s visual surface proof: %+v", shape, surface)
		}
	}
	if !reflect.DeepEqual(sortedKeys(fixture.Exports), []string{"csv_matrix", "csv_table", "json", "png", "svg"}) {
		t.Fatalf("baseline format proof is incomplete: %v", sortedKeys(fixture.Exports))
	}
	expectedFailures := map[string]any{
		"cancellation": "export.serializer_failed", "export_incompatible_profile": "export.profile_incompatible",
		"export_malformed_render": "export.render_input_mismatch", "export_missing_resource": "export.font_missing",
		"export_resource_limit": "export.serializer_failed", "incompatible_profile": "render.profile_incompatible",
		"malformed": "render.input_invalid", "missing_asset": "render.asset_missing", "missing_font": "render.font_missing",
		"partial_export_published": false, "partial_render_published": false, "partial_viewer_published": false,
		"resource_limit": "render.resource_limit", "stale_stream": "viewer.update_stale", "viewer_cancellation": "cancelled",
	}
	if !reflect.DeepEqual(fixture.Failures, expectedFailures) {
		t.Fatalf("bounded failure proof changed:\nactual=%#v\nexpected=%#v", fixture.Failures, expectedFailures)
	}
	if fixture.Empty.Kind != "context" || fixture.Empty.Bindings != 0 {
		t.Fatalf("empty result proof changed: %+v", fixture.Empty)
	}
	if fixture.State.Policy != "optional" || fixture.State.Input != "snapshot" || fixture.State.ViewerStatus != "ready" ||
		!digest.MatchString(fixture.State.RenderDigest) || !digest.MatchString(fixture.State.SourceManifestDigest) || fixture.State.SourceManifestRepresentations < 1 {
		t.Fatalf("state reference proof changed: %+v", fixture.State)
	}
	for _, required := range []string{"empty_context", "flow_cycle", "tree_cycle", "tree_duplicate"} {
		if !slices.Contains(fixture.DerivedCases, required) {
			t.Fatalf("derived semantic case %q is missing", required)
		}
	}
}

func sortedKeys[V any](value map[string]V) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
