// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	if err := verifySBOM(output, "0.0.0-dev"); err != nil {
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

func TestManifestBrowserContractAndLicensesRejectEveryFieldMutation(t *testing.T) {
	base, err := buildManifest(t.TempDir(), "0.0.0", strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyManifestAuthority(base); err != nil {
		t.Fatalf("valid authority rejected: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*artifactManifest)
	}{
		{"module dedicated Worker", func(value *artifactManifest) { value.BrowserContract.ModuleDedicatedWorker = false }},
		{"SharedArrayBuffer", func(value *artifactManifest) { value.BrowserContract.SharedArrayBuffer = true }},
		{"WASM threads", func(value *artifactManifest) { value.BrowserContract.WASMThreads = true }},
		{"product license", func(value *artifactManifest) { value.Licenses.Product = "MIT" }},
		{"runtime license", func(value *artifactManifest) { value.Licenses.RuntimeSupport = "MIT" }},
		{"SBOM path", func(value *artifactManifest) { value.Licenses.SBOM = "other.cdx.json" }},
	}
	for index, primitive := range requiredBrowserPrimitives {
		index, primitive := index, primitive
		tests = append(tests, struct {
			name   string
			mutate func(*artifactManifest)
		}{"required primitive " + primitive, func(value *artifactManifest) {
			value.BrowserContract.RequiredPrimitives = slices.Clone(value.BrowserContract.RequiredPrimitives)
			value.BrowserContract.RequiredPrimitives[index] = "falsified"
		}})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := base
			value.Build.Flags = slices.Clone(base.Build.Flags)
			value.BrowserContract.RequiredPrimitives = slices.Clone(base.BrowserContract.RequiredPrimitives)
			test.mutate(&value)
			if err := verifyManifestAuthority(value); err == nil {
				t.Fatal("falsified manifest authority was accepted")
			}
		})
	}
}

func TestSBOMRootReleaseVersionIsAuthoritative(t *testing.T) {
	output := t.TempDir()
	if err := os.WriteFile(filepath.Join(output, "layerdraw-engine.wasm"), []byte("wasm"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeSBOM(output, "1.2.3", nil); err != nil {
		t.Fatal(err)
	}
	if err := verifySBOM(output, "1.2.3"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(output, sbomName))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document["metadata"].(map[string]any)["component"].(map[string]any)["version"] = "9.9.9"
	mutated, err := canonicalJSON(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, sbomName), mutated, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifySBOM(output, "1.2.3"); err == nil {
		t.Fatal("SBOM root version mismatch was accepted")
	}
}
