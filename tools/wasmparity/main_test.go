// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
)

func TestGeneratedParityCorpusIsCurrentAndDeterministic(t *testing.T) {
	first, err := buildCorpus()
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildCorpus()
	if err != nil {
		t.Fatal(err)
	}
	equal, err := corpusEqual(first, second)
	if err != nil || !equal {
		t.Fatalf("two in-process corpus generations differ: equal=%v err=%v", equal, err)
	}
	wantNames := []string{
		"single_module_project", "multi_module_project", "installed_pack_project", "root_pack", "asset_project",
		"all_declarations_project", "deterministic_rejection", "resource_limit_rejection", "representative_large_graph", "cancellation",
	}
	gotNames := make([]string, len(first.Cases))
	for index, test := range first.Cases {
		gotNames[index] = test.Name
	}
	if first.SchemaVersion != 1 || first.EngineReleaseVariable != engineReleaseVariable ||
		!reflect.DeepEqual(gotNames, wantNames) || len(first.RequiredFeatures) != 10 || len(first.Normalization) != 3 {
		t.Fatalf("incomplete parity corpus: names=%v features=%v normalization=%v", gotNames, first.RequiredFeatures, first.Normalization)
	}
	for _, test := range first.Cases {
		if len(test.Features) == 0 || len(test.Request.ControlBase64) == 0 || len(test.Request.Blobs) == 0 || len(test.Expected.Response) == 0 || test.Expected.Outcome == "" {
			t.Fatalf("incomplete %s vector", test.Name)
		}
		if test.Expected.Outcome == "success" && len(test.Expected.Blobs) == 0 {
			t.Fatalf("successful %s vector has no canonical bytes", test.Name)
		}
		if test.Expected.Outcome != "success" && len(test.Expected.Blobs) != 0 {
			t.Fatalf("terminal %s vector published blobs", test.Name)
		}
	}
	want, err := canonicalCorpus(first)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join("..", "..", "tests", "conformance", "testdata", "engine_compile_parity_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("committed parity corpus is stale; run make generate")
	}
}

func TestGenerateWritesCanonicalCorpusAndRejectsInvalidTargets(t *testing.T) {
	output := filepath.Join(t.TempDir(), "nested", "corpus.json")
	if err := generate(output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil || len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("generated corpus bytes=%d err=%v", len(data), err)
	}

	parentFile := filepath.Join(t.TempDir(), "parent")
	if err := os.WriteFile(parentFile, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := generate(filepath.Join(parentFile, "corpus.json")); err == nil {
		t.Fatal("generator accepted a file as the output parent")
	}
	if err := generate(t.TempDir()); err == nil {
		t.Fatal("generator accepted a directory as the output file")
	}
}

func TestCorpusValidationAndPortableLimitOverrides(t *testing.T) {
	if err := validateCoverage(parityCorpus{Cases: []parityCase{{Name: "bad", Execution: "unknown"}}}); err == nil {
		t.Fatal("unknown execution mode was accepted")
	}
	if err := validateCoverage(parityCorpus{
		RequiredFeatures: []string{"missing"},
		Cases:            []parityCase{{Name: "valid", Execution: "compile", Features: []string{"present"}}},
	}); err == nil {
		t.Fatal("missing required feature was accepted")
	}

	values := make([]protocolcommon.CanonicalNonNegativeInt64, 9)
	for index := range values {
		values[index] = protocolcommon.CanonicalNonNegativeInt64(string(rune('1' + index)))
	}
	overrides := engineprotocol.ResourceLimits{
		MaxProjectSourceFiles: &values[0],
		MaxProjectSourceBytes: &values[1],
		MaxPackFiles:          &values[2],
		MaxPackBytes:          &values[3],
		MaxAssets:             &values[4],
		MaxAssetBytes:         &values[5],
		MaxRasterDimension:    &values[6],
		MaxRasterPixels:       &values[7],
		MaxDeclarations:       &values[8],
	}
	if got := portableResourceLimits(overrides); !reflect.DeepEqual(got, overrides) {
		t.Fatalf("portable overrides=%+v want=%+v", got, overrides)
	}

	invalid := parityCorpus{Cases: []parityCase{{Expected: parityExpected{Response: json.RawMessage("{")}}}}
	if _, err := canonicalCorpus(invalid); err == nil {
		t.Fatal("invalid raw response was canonically encoded")
	}
	if _, err := corpusEqual(invalid, parityCorpus{}); err == nil {
		t.Fatal("invalid left corpus was compared")
	}
	if _, err := corpusEqual(parityCorpus{}, invalid); err == nil {
		t.Fatal("invalid right corpus was compared")
	}
}
