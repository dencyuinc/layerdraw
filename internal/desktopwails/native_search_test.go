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
