// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
//go:build ladybug_native

package desktopwails

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateLadybugRootRequiresPinnedManifestDigests(t *testing.T) {
	root := writeTestLadybugBundle(t)
	if _, err := validateLadybugRoot(root); err != nil {
		t.Fatalf("pinned bundle rejected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "libfts.lbug_extension"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateLadybugRoot(root); err == nil {
		t.Fatal("tampered native extension accepted")
	}
}

func TestPackagedLadybugRootHonorsOnlyValidatedEnvironmentPaths(t *testing.T) {
	root := writeTestLadybugBundle(t)
	t.Setenv("LAYERDRAW_LADYBUG_NATIVE_DIR", root)
	if resolved, err := packagedLadybugRoot(); err != nil || resolved != root {
		t.Fatalf("explicit native root = %q, %v", resolved, err)
	}
	t.Setenv("LAYERDRAW_LADYBUG_NATIVE_DIR", "")
	t.Setenv("LAYERDRAW_LADYBUG_FTS_EXTENSION", filepath.Join(root, "libfts.lbug_extension"))
	if resolved, err := packagedLadybugRoot(); err != nil || resolved != root {
		t.Fatalf("FTS-derived native root = %q, %v", resolved, err)
	}
	t.Setenv("LAYERDRAW_LADYBUG_NATIVE_DIR", "relative")
	if _, err := packagedLadybugRoot(); err == nil {
		t.Fatal("relative explicit native root accepted")
	}
}

func TestValidateLadybugRootRejectsIncompleteBundles(t *testing.T) {
	if _, err := validateLadybugRoot(t.TempDir()); err == nil {
		t.Fatal("bundle without manifest accepted")
	}
	root := writeTestLadybugBundle(t)
	if err := os.Remove(filepath.Join(root, "libvector.lbug_extension")); err != nil {
		t.Fatal(err)
	}
	if _, err := validateLadybugRoot(root); err == nil {
		t.Fatal("bundle without required extension accepted")
	}
	if _, err := nativeFileDigest(filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing extension produced a digest")
	}
}

func writeTestLadybugBundle(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string][]byte{
		"libfts.lbug_extension":    []byte("fts"),
		"libvector.lbug_extension": []byte("vector"),
		"libalgo.lbug_extension":   []byte("algo"),
	}
	digests := map[string]string{}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(content)
		digests[name] = hex.EncodeToString(digest[:])
	}
	manifest, err := json.Marshal(struct {
		SchemaVersion  int               `json:"schema_version"`
		LadybugVersion string            `json:"ladybug_version"`
		Platform       string            `json:"platform"`
		Files          map[string]string `json:"files"`
	}{1, "0.17.0", packagedNativePlatform(), digests})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ladybug-native.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}
