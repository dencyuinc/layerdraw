// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
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
	if first.SchemaVersion != 1 || first.EngineReleaseVariable != engineReleaseVariable || len(first.Cases) != 2 ||
		first.Cases[0].Name != "canonical_project" || first.Cases[1].Name != "canonical_root_pack" {
		t.Fatalf("incomplete parity corpus: %+v", first)
	}
	for _, test := range first.Cases {
		if len(test.Request.ControlBase64) == 0 || len(test.Request.Blobs) == 0 || len(test.Expected.Response) == 0 || len(test.Expected.Blobs) != 2 {
			t.Fatalf("incomplete %s vector", test.Name)
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
