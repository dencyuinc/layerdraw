// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package packaged_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackagedEngineReportsInjectedBuildInfo(t *testing.T) {
	binary := os.Getenv("LAYERDRAW_ENGINE_BINARY")
	if binary == "" {
		t.Skip("LAYERDRAW_ENGINE_BINARY is not set")
	}

	output, err := exec.Command(binary, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("%s --version: %v\n%s", binary, err, output)
	}

	got := string(output)
	if !strings.HasPrefix(got, "layerdraw-engine 0.0.0-dev (") {
		t.Fatalf("version output = %q", got)
	}
	if strings.Contains(got, "(unknown)") {
		t.Fatalf("packaged engine did not receive source revision: %q", got)
	}
}

func TestPackagedEngineIncludesLegalMaterialAndSBOM(t *testing.T) {
	bundle := os.Getenv("LAYERDRAW_BUNDLE_DIR")
	if bundle == "" {
		t.Skip("LAYERDRAW_BUNDLE_DIR is not set")
	}

	for _, name := range []string{"LICENSE", "NOTICE", "LICENSING.md", "THIRD_PARTY_NOTICES.txt", "licenses/Apache-2.0.txt"} {
		data, err := os.ReadFile(filepath.Join(bundle, name))
		if err != nil {
			t.Fatalf("read bundled %s: %v", name, err)
		}
		if len(data) == 0 {
			t.Fatalf("bundled %s is empty", name)
		}
	}

	data, err := os.ReadFile(filepath.Join(bundle, "layerdraw-engine.cdx.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sbom struct {
		BOMFormat   string `json:"bomFormat"`
		SpecVersion string `json:"specVersion"`
		Metadata    struct {
			Component struct {
				Name string `json:"name"`
			} `json:"component"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(data, &sbom); err != nil {
		t.Fatalf("decode bundled SBOM: %v", err)
	}
	if sbom.BOMFormat != "CycloneDX" || sbom.SpecVersion != "1.6" || sbom.Metadata.Component.Name != "layerdraw-engine" {
		t.Fatalf("unexpected bundled SBOM: %+v", sbom)
	}
}
