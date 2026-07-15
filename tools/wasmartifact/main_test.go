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
	if err := verifySBOM(output, "0.0.0-dev", modules); err != nil {
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
	modules := []bundledModule{{Review: reviewedGoModule{Module: "example.com/module", Version: "v1.2.3", License: "MIT"}}}
	base, err := buildManifest(t.TempDir(), "0.0.0", strings.Repeat("a", 40), modules)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyManifestAuthority(base, "0.0.0", modules); err != nil {
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
		{"release authority", func(value *artifactManifest) { value.Build.ReleaseVersion = "9.9.9" }},
		{"SBOM authority digest", func(value *artifactManifest) { value.SBOMAuthority.Digest = "sha256:" + strings.Repeat("0", 64) }},
		{"runtime component type", func(value *artifactManifest) { value.SBOMAuthority.Runtime.Type = "library" }},
		{"runtime component digest", func(value *artifactManifest) {
			value.SBOMAuthority.Runtime.Digest = "sha256:" + strings.Repeat("0", 64)
		}},
		{"runtime component license", func(value *artifactManifest) { value.SBOMAuthority.Runtime.License = "MIT" }},
		{"module component license", func(value *artifactManifest) { value.SBOMAuthority.Modules[0].License = "Apache-2.0" }},
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
			value.SBOMAuthority.Modules = slices.Clone(base.SBOMAuthority.Modules)
			test.mutate(&value)
			if err := verifyManifestAuthority(value, "0.0.0", modules); err == nil {
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
	if err := verifySBOM(output, "1.2.3", nil); err != nil {
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
	if err := verifySBOM(output, "1.2.3", nil); err == nil {
		t.Fatal("SBOM root version mismatch was accepted")
	}
}

func TestSBOMRejectsEveryRuntimeModuleAndDependencyMutation(t *testing.T) {
	modules := []bundledModule{
		{Review: reviewedGoModule{Module: "example.com/a", Version: "v1.2.3", License: "MIT"}},
		{Review: reviewedGoModule{Module: "example.com/b", Version: "v2.3.4", License: "Apache-2.0"}},
	}
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"runtime type", func(document map[string]any) { sbomRuntime(document)["type"] = "library" }},
		{"runtime name", func(document map[string]any) { sbomRuntime(document)["name"] = "other" }},
		{"runtime version", func(document map[string]any) { sbomRuntime(document)["version"] = "go9.9.9" }},
		{"runtime purl", func(document map[string]any) { sbomRuntime(document)["purl"] = "pkg:generic/other@go1.26.5" }},
		{"runtime bom ref", func(document map[string]any) { sbomRuntime(document)["bom-ref"] = "pkg:generic/other@go1.26.5" }},
		{"runtime scope", func(document map[string]any) { sbomRuntime(document)["scope"] = "optional" }},
		{"runtime hash algorithm", func(document map[string]any) {
			sbomRuntime(document)["hashes"].([]any)[0].(map[string]any)["alg"] = "SHA-512"
		}},
		{"runtime hash content", func(document map[string]any) {
			sbomRuntime(document)["hashes"].([]any)[0].(map[string]any)["content"] = strings.Repeat("0", 64)
		}},
		{"runtime license", func(document map[string]any) {
			sbomRuntime(document)["licenses"].([]any)[0].(map[string]any)["license"].(map[string]any)["id"] = "MIT"
		}},
		{"module type", func(document map[string]any) { sbomModule(document)["type"] = "framework" }},
		{"module name", func(document map[string]any) { sbomModule(document)["name"] = "example.com/forged" }},
		{"module version", func(document map[string]any) { sbomModule(document)["version"] = "v9.9.9" }},
		{"module purl", func(document map[string]any) { sbomModule(document)["purl"] = "pkg:golang/example.com/forged@v1.2.3" }},
		{"module bom ref", func(document map[string]any) {
			sbomModule(document)["bom-ref"] = "pkg:golang/example.com/forged@v1.2.3"
		}},
		{"module scope", func(document map[string]any) { sbomModule(document)["scope"] = "optional" }},
		{"module license", func(document map[string]any) {
			sbomModule(document)["licenses"].([]any)[0].(map[string]any)["license"].(map[string]any)["id"] = "BSD-3-Clause"
		}},
		{"duplicate component", func(document map[string]any) {
			document["components"] = append(document["components"].([]any), sbomModule(document))
		}},
		{"missing component", func(document map[string]any) { document["components"] = document["components"].([]any)[1:] }},
		{"root dependency ref", func(document map[string]any) {
			sbomRootDependency(document)["ref"] = "pkg:npm/%40layerdraw/engine-wasm@9.9.9"
		}},
		{"missing dependency edge", func(document map[string]any) {
			values := sbomRootDependency(document)["dependsOn"].([]any)
			sbomRootDependency(document)["dependsOn"] = values[1:]
		}},
		{"extra dependency edge", func(document map[string]any) {
			sbomRootDependency(document)["dependsOn"] = append(sbomRootDependency(document)["dependsOn"].([]any), "pkg:golang/forged@v1.0.0")
		}},
		{"reordered dependency edges", func(document map[string]any) {
			values := sbomRootDependency(document)["dependsOn"].([]any)
			values[0], values[1] = values[1], values[0]
		}},
		{"leaf dependency edge", func(document map[string]any) {
			document["dependencies"].([]any)[1].(map[string]any)["dependsOn"] = []any{"pkg:golang/example.com/b@v2.3.4"}
		}},
		{"duplicate dependency ref", func(document map[string]any) {
			document["dependencies"] = append(document["dependencies"].([]any), document["dependencies"].([]any)[1])
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := t.TempDir()
			if err := os.WriteFile(filepath.Join(output, "layerdraw-engine.wasm"), []byte("wasm"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := writeSBOM(output, "1.2.3", modules); err != nil {
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
			test.mutate(document)
			mutated, err := canonicalJSON(document)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(output, sbomName), mutated, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := verifySBOM(output, "1.2.3", modules); err == nil {
				t.Fatal("falsified SBOM authority was accepted")
			}
		})
	}
}

func sbomRuntime(document map[string]any) map[string]any {
	components := document["components"].([]any)
	return components[len(components)-1].(map[string]any)
}

func sbomModule(document map[string]any) map[string]any {
	return document["components"].([]any)[0].(map[string]any)
}

func sbomRootDependency(document map[string]any) map[string]any {
	return document["dependencies"].([]any)[0].(map[string]any)
}
