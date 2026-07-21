// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registrysource

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/registry"
)

func TestLocalDirectoryAcceptsNativeWindowsAbsolutePath(t *testing.T) {
	root := t.TempDir()
	body := []byte("windows pack bytes")
	release := fixtureRelease("", body)
	writeCatalog(t, root, Catalog{SchemaVersion: CatalogVersion, Artifacts: []CatalogEntry{{Release: release, ArtifactPath: "artifacts/demo.ldpack"}}})
	if err := os.MkdirAll(filepath.Join(root, "artifacts"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "artifacts", "demo.ldpack"), body, 0o600); err != nil {
		t.Fatal(err)
	}

	source := registry.RegistrySource{SourceID: "windows-local", Kind: registry.SourceLocalDirectory, EndpointRef: root}
	client := LocalDirectory{}
	if err := client.ProbeRegistrySource(context.Background(), source, registry.CredentialLease{}); err != nil {
		t.Fatalf("native Windows endpoint %q was rejected: %v", root, err)
	}
	found, err := client.Search(context.Background(), source, registry.SearchInput{Query: release.Identity.CanonicalID})
	if err != nil || len(found) != 1 {
		t.Fatalf("search=%+v err=%v", found, err)
	}
	stream, err := client.OpenArtifact(context.Background(), source, found[0])
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(stream)
	if err != nil || string(got) != string(body) {
		t.Fatalf("artifact=%q err=%v", got, err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
}
