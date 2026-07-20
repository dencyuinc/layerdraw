// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"
)

func TestReadAndCompileRegistryPackClosure(t *testing.T) {
	instance := New(BuildInfo{})
	source := []byte("entity_type service \"Service\" {\n  representation shape rect\n}\nexport { service }\n")
	manifest := RegistryPackManifest{Format: LayerdrawPackFormat, FormatVersion: 1, ID: "example/schema", Name: "schema", Version: "1.0.0", Language: 1, Entry: "pack.ldl", Dependencies: map[string]RegistryPackDependency{}}
	manifestBytes, err := canonicalArtifact(manifest)
	if err != nil {
		t.Fatal(err)
	}
	archive := packArchive(t, map[string][]byte{"manifest.json": manifestBytes, "pack.ldl": source})
	artifact, err := instance.ReadRegistryPack(context.Background(), archive, LayerdrawLimits{})
	if err != nil || artifact.Manifest.ID != manifest.ID || artifact.Digests["pack.ldl"] != rawDigest(source) {
		t.Fatalf("artifact=%+v err=%v", artifact, err)
	}
	pack := ResolvedPack{InstallName: "schema", CanonicalID: manifest.ID, Version: manifest.Version, Digest: rawDigest(archive), Path: "pack/schema", Entry: manifest.Entry, Files: []ResolvedPackFile{{Path: manifest.Entry, Digest: rawDigest(source)}}, ManifestPath: "manifest.json", Manifest: manifestBytes}
	tree := map[string][]byte{"pack/schema/pack.ldl": source, "pack/schema/manifest.json": manifestBytes}
	snapshot, err := instance.CompileRegistryPackClosure(context.Background(), manifest.ID, []ResolvedPack{pack}, tree, ResourceLimits{})
	if err != nil || snapshot.NormalizedPackArtifact == nil {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}

func TestReadRegistryPackRejectsNoncanonicalAndUnsafeContent(t *testing.T) {
	instance := New(BuildInfo{})
	source := []byte("entity_type service \"Service\" {}\n")
	noncanonical := []byte(`{"name":"schema","format":"layerdraw-pack","format_version":1,"id":"example/schema","version":"1.0.0","language":1,"entry":"pack.ldl","dependencies":{}}`)
	if _, err := instance.ReadRegistryPack(context.Background(), packArchive(t, map[string][]byte{"manifest.json": noncanonical, "pack.ldl": source}), LayerdrawLimits{}); !IsLayerdrawError(err, LayerdrawErrorManifest) {
		t.Fatalf("noncanonical manifest err=%v", err)
	}
	manifest, _ := canonicalArtifact(RegistryPackManifest{Format: LayerdrawPackFormat, FormatVersion: 1, ID: "example/schema", Name: "schema", Version: "1.0.0", Language: 1, Entry: "pack.ldl", Dependencies: map[string]RegistryPackDependency{}})
	if _, err := instance.ReadRegistryPack(context.Background(), packArchive(t, map[string][]byte{"manifest.json": manifest, "pack.ldl": source, "private/secret.txt": []byte("secret")}), LayerdrawLimits{}); !IsLayerdrawError(err, LayerdrawErrorForbiddenPortable) {
		t.Fatalf("forbidden entry err=%v", err)
	}
}

func packArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, name := range sortedByteMapKeys(files) {
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
