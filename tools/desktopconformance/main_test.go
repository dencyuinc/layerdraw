// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepositoryDesktopClosureMatchesNormativeMatrix(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	if _, err := verify(root, "deploy/desktop-conformance.json"); err != nil {
		t.Fatal(err)
	}
}

func TestDesktopClosureRejectsUnprovenDeliveredRows(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "blueprint.md"), []byte("| F01 | One | ✓ | ✓ | ✓ | - | - | - | - |\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "closure.json")
	if err := os.WriteFile(manifestPath, []byte(`{"schema_version":1,"delivery":"desktop","normative_matrix":"docs/blueprint.md#1311-feature-x-delivery-matrix","features":{"F01":{"feature":"One","delivered":true,"evidence":[]}},"acceptance_suites":{},"faults":{},"release_evidence":[],"performance_budgets":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := verify(root, "closure.json"); err == nil {
		t.Fatal("unproven delivered row was accepted")
	}
}

func TestDesktopClosureRejectsTraversalAndEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "desktop-conformance.json")
	if err := os.WriteFile(outside, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := verify(root, filepath.Join("..", filepath.Base(outside))); err == nil {
		t.Fatal("manifest traversal was accepted")
	}
	link := filepath.Join(root, "linked.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if _, err := verify(root, "linked.json"); err == nil {
		t.Fatal("manifest symlink escaping the root was accepted")
	}
}
