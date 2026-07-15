// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
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
