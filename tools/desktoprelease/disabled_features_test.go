// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryDisabledFeaturesExactlyMatchDesktopMatrix(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	if err := validateDisabledFeatures(filepath.Join(root, "deploy", "desktop-disabled-features.json"), filepath.Join(root, "deploy", "desktop-conformance.json")); err != nil {
		t.Fatal(err)
	}
}

func TestDisabledFeatureManifestRejectsMissingReasonAndMatrixDrift(t *testing.T) {
	root := t.TempDir()
	conformance := filepath.Join(root, "conformance.json")
	disabled := filepath.Join(root, "disabled.json")
	if err := os.WriteFile(conformance, []byte(`{"schema_version":1,"features":{"F01":{"feature":"One","delivered":false}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	base := `{"schema_version":1,"delivery":"desktop","normative_matrix":"docs/blueprint.md#1311-feature-x-delivery-matrix","disabled_features":[{"feature_id":"F01","feature":"One","status":"disabled","reason_code":"server_only","reason":"Not in Desktop."}]}`
	if err := os.WriteFile(disabled, []byte(base), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateDisabledFeatures(disabled, conformance); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{
		strings.Replace(base, `"reason":"Not in Desktop."`, `"reason":""`, 1),
		strings.Replace(base, `"feature_id":"F01"`, `"feature_id":"F02"`, 1),
		base + `{}`,
	} {
		if err := os.WriteFile(disabled, []byte(mutation), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := validateDisabledFeatures(disabled, conformance); err == nil {
			t.Fatalf("invalid disabled feature manifest accepted: %s", mutation)
		}
	}
}
