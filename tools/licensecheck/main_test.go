// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpectedSourceLicenseUsesLongestPrefix(t *testing.T) {
	source := sourcePolicy{
		DefaultLicense: "LicenseRef-LayerDraw-1.0",
		Rules: []sourceRule{
			{Prefix: "tests/", License: "LicenseRef-LayerDraw-1.0"},
			{Prefix: "tests/conformance/", License: "Apache-2.0"},
		},
	}
	if got := expectedSourceLicense("tests/conformance/protocol_test.go", source); got != "Apache-2.0" {
		t.Fatalf("expectedSourceLicense() = %q", got)
	}
}

func TestCheckSourceHeaders(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cmd", "example", "main.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0\npackage main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := sourcePolicy{
		DefaultLicense: "LicenseRef-LayerDraw-1.0",
		Roots:          []string{"cmd"},
		Extensions:     []string{".go"},
	}
	if err := checkSourceHeaders(root, source); err != nil {
		t.Fatal(err)
	}
}

func TestCheckSourceHeadersRequiresJSONSchemaSPDXComment(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "schemas", "example", "v1.schema.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	source := sourcePolicy{DefaultLicense: "Apache-2.0", Roots: []string{"schemas"}, Extensions: []string{".go"}}
	if err := os.WriteFile(path, []byte(`{"$comment":"SPDX-License-Identifier: Apache-2.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkSourceHeaders(root, source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"$comment":"wrong"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkSourceHeaders(root, source); err == nil || !strings.Contains(err.Error(), "$comment") {
		t.Fatalf("missing JSON Schema SPDX comment was accepted: %v", err)
	}
}

func TestRequireAllowedLicense(t *testing.T) {
	allowed := map[string]bool{"MIT": true}
	denied := map[string]bool{"AGPL-3.0-only": true}
	if err := requireAllowedLicense("MIT", allowed, denied); err != nil {
		t.Fatal(err)
	}
	if err := requireAllowedLicense("AGPL-3.0-only", allowed, denied); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("denied license error = %v", err)
	}
	if err := requireAllowedLicense("MPL-2.0", allowed, denied); err == nil || !strings.Contains(err.Error(), "review") {
		t.Fatalf("unreviewed license error = %v", err)
	}
}

func TestVerifyLicenseFile(t *testing.T) {
	root := t.TempDir()
	content := []byte("license text\n")
	if err := os.WriteFile(filepath.Join(root, "LICENSE"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	if err := verifyLicenseFile(root, "LICENSE", hex.EncodeToString(digest[:])); err != nil {
		t.Fatal(err)
	}
	if err := verifyLicenseFile(root, "../LICENSE", hex.EncodeToString(digest[:])); err == nil {
		t.Fatal("path traversal was accepted")
	}
}

func TestRenderThirdPartyNotices(t *testing.T) {
	modules := []bundledModule{{
		Review:      reviewedGoModule{Module: "example.com/library", Version: "v1.0.0", License: "MIT"},
		LicenseText: []byte("MIT license text\n"),
	}}
	got := string(renderThirdPartyNotices("layerdraw-engine", modules))
	for _, expected := range []string{"example.com/library v1.0.0", "License: MIT", "MIT license text"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("notice does not contain %q:\n%s", expected, got)
		}
	}
}

func TestRenderCycloneDX(t *testing.T) {
	modules := []bundledModule{{
		Review: reviewedGoModule{Module: "example.com/library", Version: "v1.0.0", License: "MIT"},
	}}
	data, err := renderCycloneDX("layerdraw-engine", "1.2.3", modules)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		BOMFormat   string `json:"bomFormat"`
		SpecVersion string `json:"specVersion"`
		Metadata    struct {
			Component struct {
				Name string `json:"name"`
			} `json:"component"`
		} `json:"metadata"`
		Components []json.RawMessage `json:"components"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.BOMFormat != "CycloneDX" || document.SpecVersion != "1.6" {
		t.Fatalf("unexpected SBOM identity: %+v", document)
	}
	if document.Metadata.Component.Name != "layerdraw-engine" || len(document.Components) != 1 {
		t.Fatalf("unexpected SBOM content: %+v", document)
	}
}

func TestWriteDependencyInventory(t *testing.T) {
	root := t.TempDir()
	for path, content := range map[string]string{
		"go.mod":                    "module example.com/project\n",
		"tools/license-policy.json": "{}\n",
	} {
		absolutePath := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolutePath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dependencies := []dependencyRecord{
		{Ecosystem: "go", Name: "example.com/runtime", Version: "v1.0.0", Scope: "runtime", License: "MIT", ReviewSource: "reviewed-license-file"},
		{Ecosystem: "npm", Name: "example-dev", Version: "2.0.0", Scope: "development", License: "ISC", ReportedLicense: "ISC", ReviewSource: "package-metadata"},
	}
	if err := writeDependencyInventory(root, "reports/dependencies.json", "tools/license-policy.json", dependencies); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "reports", "dependencies.json"))
	if err != nil {
		t.Fatal(err)
	}
	var inventory dependencyInventory
	if err := json.Unmarshal(data, &inventory); err != nil {
		t.Fatal(err)
	}
	if inventory.Summary.Total != 2 || inventory.Summary.Runtime != 1 || inventory.Summary.Development != 1 {
		t.Fatalf("unexpected inventory summary: %+v", inventory.Summary)
	}
	if len(inventory.Inputs) != 2 || len(inventory.Dependencies) != 2 {
		t.Fatalf("unexpected inventory: %+v", inventory)
	}
	if strings.Contains(string(data), root) {
		t.Fatalf("inventory contains machine-specific root path: %s", data)
	}
}
