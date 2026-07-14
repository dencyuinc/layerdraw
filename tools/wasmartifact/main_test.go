// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalJSONAndSafeRelative(t *testing.T) {
	t.Parallel()
	data, err := canonicalJSON(map[string]any{"z": 1, "a": "value"})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{\"a\":\"value\",\"z\":1}\n" {
		t.Fatalf("canonical JSON=%q", data)
	}
	for _, path := range []string{"layerdraw-engine.wasm", "licenses/Apache-2.0.txt"} {
		if !safeRelative(path) {
			t.Fatalf("safe path rejected: %s", path)
		}
	}
	for _, path := range []string{"", "../secret", "/absolute", "a/../b"} {
		if safeRelative(path) {
			t.Fatalf("unsafe path accepted: %s", path)
		}
	}
}

func TestArtifactFilesAndSBOMAreDeterministic(t *testing.T) {
	t.Parallel()
	output := t.TempDir()
	for path, value := range map[string][]byte{
		"layerdraw-engine.wasm": []byte("wasm"),
		"wasm_exec.js":          []byte("support"),
		"LICENSE":               []byte("license"),
	} {
		if err := os.WriteFile(filepath.Join(output, path), value, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	modules := []bundledModule{{Review: reviewedGoModule{Module: "example.com/module", Version: "v1.2.3", License: "MIT"}}}
	if err := writeSBOM(output, "0.0.0-dev", modules); err != nil {
		t.Fatal(err)
	}
	if err := verifySBOM(output); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(output, sbomName))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSBOM(output, "0.0.0-dev", modules); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(output, sbomName))
	if !bytes.Equal(first, second) {
		t.Fatal("artifact SBOM is not deterministic")
	}
	files, err := artifactFiles(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 || files[0].Path != "LICENSE" || files[3].Path != "wasm_exec.js" {
		t.Fatalf("artifact files are not sorted and complete: %+v", files)
	}
	if transportManifest().MaxControlBytes != 8_388_608 || compilerLimitsManifest().MaxProjectSourceBytes != 16<<20 {
		t.Fatal("manifest limit authority drifted")
	}
}
