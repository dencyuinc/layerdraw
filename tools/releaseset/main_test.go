// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
)

const (
	testVersion  = "1.2.3"
	testRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testBuiltAt  = "2026-07-15T00:00:00Z"
)

func TestBuildAndVerifyFixedReleaseSet(t *testing.T) {
	output, nativeSBOM, nativeNotices := releaseFixture(t, testVersion)
	if err := build(".", output, testVersion, testRevision, testBuiltAt, nativeSBOM, nativeNotices); err != nil {
		t.Fatal(err)
	}
	if err := verify(".", output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(output, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	var manifest releaseManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Artifacts) != 4 || manifest.ReleaseVersion != testVersion || manifest.Protocols[0].Versions[0].SchemaDigest != engineprotocol.SchemaDigest {
		t.Fatalf("manifest=%+v", manifest)
	}
	for _, artifact := range manifest.Artifacts {
		if artifact.Digest == "" || artifact.Legal.SPDX.Digest == "" || artifact.Legal.CycloneDX.Digest == "" || artifact.Legal.Notices.Digest == "" {
			t.Fatalf("artifact legal binding is incomplete: %+v", artifact)
		}
	}
}

func TestVerifyRejectsCorruptStaleAndUnsupportedReleaseState(t *testing.T) {
	output, nativeSBOM, nativeNotices := releaseFixture(t, testVersion)
	if err := build(".", output, testVersion, testRevision, testBuiltAt, nativeSBOM, nativeNotices); err != nil {
		t.Fatal(err)
	}
	client := filepath.Join(output, "artifacts", "layerdraw-engine-client-"+testVersion+".tgz")
	original, err := os.ReadFile(client)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(client, append(original, 0), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verify(".", output); err == nil || !strings.Contains(err.Error(), "size or digest mismatch") {
		t.Fatalf("corrupt artifact error=%v", err)
	}
	if err := os.WriteFile(client, original, 0o644); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(output, manifestName)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest releaseManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Protocols[0].Versions[0].SchemaDigest = "sha256:" + strings.Repeat("0", 64)
	forged, err := canonicalJSON(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, forged, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verify(".", output); err == nil || !strings.Contains(err.Error(), "protocol binding mismatch") {
		t.Fatalf("stale protocol binding error=%v", err)
	}
	if err := validateIdentity(testVersion, "bad", testBuiltAt); err == nil {
		t.Fatal("unsupported source identity was accepted")
	}
	if err := validateIdentity(testVersion, testRevision, "2026-07-15T00:00:00+09:00"); err == nil {
		t.Fatal("noncanonical build time was accepted")
	}
}

func TestBuildRejectsMixedPackageVersionsAndRuntimeClosure(t *testing.T) {
	output, nativeSBOM, nativeNotices := releaseFixture(t, "9.9.9")
	if err := build(".", output, testVersion, testRevision, testBuiltAt, nativeSBOM, nativeNotices); err == nil || !strings.Contains(err.Error(), "identity mismatch") {
		t.Fatalf("mixed package release error=%v", err)
	}
	output, nativeSBOM, nativeNotices = releaseFixture(t, testVersion)
	clientPath := filepath.Join(output, "artifacts", "layerdraw-engine-client-"+testVersion+".tgz")
	writePackage(t, clientPath, packageAuthority{Name: "@layerdraw/engine-client", Version: testVersion, License: "Apache-2.0", Dependencies: map[string]string{"unexpected": testVersion}}, nil)
	if err := build(".", output, testVersion, testRevision, testBuiltAt, nativeSBOM, nativeNotices); err == nil || !strings.Contains(err.Error(), "runtime dependency closure") {
		t.Fatalf("mixed dependency release error=%v", err)
	}
}

func TestCommandSurfaceAndDefensiveReaders(t *testing.T) {
	if err := run(nil); err == nil || !strings.Contains(err.Error(), "expected subcommand") {
		t.Fatalf("empty command error=%v", err)
	}
	if err := run([]string{"unknown"}); err == nil || !strings.Contains(err.Error(), "unknown subcommand") {
		t.Fatalf("unknown command error=%v", err)
	}
	if err := run([]string{"build"}); err == nil || !strings.Contains(err.Error(), "build requires") {
		t.Fatalf("incomplete build error=%v", err)
	}
	if err := run([]string{"build", "-bad-flag"}); err == nil {
		t.Fatal("unknown flag was accepted")
	}
	output, nativeSBOM, nativeNotices := releaseFixture(t, testVersion)
	if err := run([]string{
		"build", "-output", output, "-version", testVersion, "-source-revision", testRevision,
		"-built-at", testBuiltAt, "-native-sbom", nativeSBOM, "-native-notices", nativeNotices,
	}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"verify", "-output", output}); err != nil {
		t.Fatal(err)
	}
	if _, err := readTarFile(filepath.Join(t.TempDir(), "missing.tgz"), "package/package.json"); err == nil {
		t.Fatal("missing archive was accepted")
	}
	badArchive := filepath.Join(t.TempDir(), "bad.tgz")
	if err := os.WriteFile(badArchive, []byte("not gzip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readTarFile(badArchive, "package/package.json"); err == nil {
		t.Fatal("malformed archive was accepted")
	}
	missingEntry := filepath.Join(t.TempDir(), "missing-entry.tgz")
	writePackage(t, missingEntry, packageAuthority{Name: "x", Version: testVersion, License: "Apache-2.0"}, nil)
	if _, err := readTarFile(missingEntry, "package/missing"); err == nil {
		t.Fatal("missing tar entry was accepted")
	}
	malformedManifest := filepath.Join(t.TempDir(), "malformed-manifest.tgz")
	writeArchive(t, malformedManifest, map[string][]byte{
		"package/package.json": []byte("{"),
		"package/LICENSE":      []byte("license"),
	})
	if _, err := readPackageAuthority(malformedManifest); err == nil {
		t.Fatal("malformed package authority was accepted")
	}
	missingLicense := filepath.Join(t.TempDir(), "missing-license.tgz")
	writeArchive(t, missingLicense, map[string][]byte{
		"package/package.json": []byte(`{"name":"x","version":"1.2.3"}`),
	})
	if _, err := readPackageAuthority(missingLicense); err == nil || !strings.Contains(err.Error(), "LICENSE is missing") {
		t.Fatalf("missing package license error=%v", err)
	}
	if err := validateCycloneDX([]byte("{"), "x", testVersion); err == nil {
		t.Fatal("malformed CycloneDX was accepted")
	}
	if err := validateCycloneDX(cycloneDXBytes(t, "wrong", testVersion), "x", testVersion); err == nil {
		t.Fatal("mismatched CycloneDX was accepted")
	}
	spdx, err := renderSPDX("x", testVersion, testBuiltAt, "sha256:"+strings.Repeat("1", 64), packageAuthority{Name: "x", Version: testVersion, License: "Apache-2.0"})
	if err != nil || validateSPDX(spdx, "x", testVersion) != nil {
		t.Fatalf("valid SPDX rejected: %v", err)
	}
	if err := validateSPDX([]byte("{"), "x", testVersion); err == nil {
		t.Fatal("malformed SPDX was accepted")
	}
	if err := validateSPDX(spdx, "wrong", testVersion); err == nil {
		t.Fatal("mismatched SPDX was accepted")
	}
	if _, err := canonicalJSON(make(chan int)); err == nil {
		t.Fatal("unsupported canonical JSON value was accepted")
	}
	if _, _, err := fileIdentity(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing artifact was accepted")
	}
	if err := validateIdentity("not-semver", testRevision, testBuiltAt); err == nil {
		t.Fatal("invalid release version was accepted")
	}
	outsideLegal := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outsideLegal, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := legalIdentity(output, outsideLegal); err == nil || !strings.Contains(err.Error(), "outside release set") {
		t.Fatalf("outside legal file error=%v", err)
	}
	if err := verifyRelativeFile(output, "../outside", "0", "sha256:"+strings.Repeat("0", 64)); err == nil {
		t.Fatal("noncanonical release path was accepted")
	}
}

func TestBuildRejectsPackagePostinstall(t *testing.T) {
	output, nativeSBOM, nativeNotices := releaseFixture(t, testVersion)
	wasm := filepath.Join(output, "artifacts", "layerdraw-engine-wasm-"+testVersion+".tgz")
	writePackage(t, wasm, packageAuthority{Name: "@layerdraw/engine-wasm", Version: testVersion, License: "SEE LICENSE IN LICENSE", Scripts: map[string]string{"postinstall": "download"}}, map[string][]byte{
		"package/dist/engine-wasm.cdx.json": cycloneDXBytes(t, "@layerdraw/engine-wasm", testVersion),
		"package/THIRD_PARTY_NOTICES.txt":   []byte("WASM runtime notices.\n"),
	})
	if err := build(".", output, testVersion, testRevision, testBuiltAt, nativeSBOM, nativeNotices); err == nil || !strings.Contains(err.Error(), "postinstall") {
		t.Fatalf("postinstall package error=%v", err)
	}
}

func TestVerifyRejectsUnknownAndIncompleteManifestShapes(t *testing.T) {
	output, nativeSBOM, nativeNotices := releaseFixture(t, testVersion)
	if err := build(".", output, testVersion, testRevision, testBuiltAt, nativeSBOM, nativeNotices); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(output, manifestName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	unknown := append([]byte{}, data[:len(data)-2]...)
	unknown = append(unknown, []byte(",\"unknown\":true}\n")...)
	if err := os.WriteFile(path, unknown, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verify(".", output); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown manifest field error=%v", err)
	}
	var manifest releaseManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Artifacts = manifest.Artifacts[:3]
	incomplete, err := canonicalJSON(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, incomplete, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verify(".", output); err == nil || !strings.Contains(err.Error(), "shape is incomplete") {
		t.Fatalf("incomplete manifest error=%v", err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Artifacts[0].MediaType = "application/forged"
	forged, err := canonicalJSON(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, forged, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verify(".", output); err == nil || !strings.Contains(err.Error(), "metadata mismatch") {
		t.Fatalf("artifact metadata error=%v", err)
	}
}

func releaseFixture(t *testing.T, packageVersion string) (string, string, string) {
	t.Helper()
	output := t.TempDir()
	if err := os.MkdirAll(filepath.Join(output, "artifacts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "layerdraw-engine"), []byte("#!/bin/sh\nprintf 'layerdraw-engine "+testVersion+" (test)\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	nativeSBOM := filepath.Join(t.TempDir(), "native.cdx.json")
	writeCycloneDX(t, nativeSBOM, "layerdraw-engine", testVersion)
	nativeNotices := filepath.Join(t.TempDir(), "THIRD_PARTY_NOTICES.txt")
	if err := os.WriteFile(nativeNotices, []byte("Native runtime notices.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackage(t, filepath.Join(output, "artifacts", "layerdraw-protocol-"+testVersion+".tgz"), packageAuthority{Name: "@layerdraw/protocol", Version: packageVersion, License: "Apache-2.0"}, nil)
	wasmSBOM := cycloneDXBytes(t, "@layerdraw/engine-wasm", testVersion)
	writePackage(t, filepath.Join(output, "artifacts", "layerdraw-engine-wasm-"+testVersion+".tgz"), packageAuthority{Name: "@layerdraw/engine-wasm", Version: packageVersion, License: "SEE LICENSE IN LICENSE"}, map[string][]byte{"package/dist/engine-wasm.cdx.json": wasmSBOM, "package/THIRD_PARTY_NOTICES.txt": []byte("WASM runtime notices.\n")})
	writePackage(t, filepath.Join(output, "artifacts", "layerdraw-engine-client-"+testVersion+".tgz"), packageAuthority{Name: "@layerdraw/engine-client", Version: packageVersion, License: "Apache-2.0", Dependencies: map[string]string{"@layerdraw/protocol": packageVersion}}, nil)
	return output, nativeSBOM, nativeNotices
}

func writePackage(t *testing.T, path string, authority packageAuthority, extra map[string][]byte) {
	t.Helper()
	manifest, err := json.Marshal(authority)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"package/package.json": manifest, "package/LICENSE": []byte("license")}
	for name, data := range extra {
		files[name] = data
	}
	writeArchive(t, path, files)
}

func writeArchive(t *testing.T, path string, files map[string][]byte) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	compressed := gzip.NewWriter(file)
	archive := tar.NewWriter(compressed)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		data := files[name]
		if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeCycloneDX(t *testing.T, path, name, version string) {
	t.Helper()
	if err := os.WriteFile(path, cycloneDXBytes(t, name, version), 0o644); err != nil {
		t.Fatal(err)
	}
}

func cycloneDXBytes(t *testing.T, name, version string) []byte {
	t.Helper()
	document := cyclonedxDocument{
		Schema: "http://cyclonedx.org/schema/bom-1.6.schema.json", BOMFormat: "CycloneDX", SpecVersion: "1.6", Version: 1,
		Metadata:   cyclonedxMetadata{Timestamp: testBuiltAt, Component: cyclonedxComponent{Type: "application", BOMRef: name, Name: name, Version: version, Licenses: licenses("LicenseRef-LayerDraw-1.0")}},
		Components: []cyclonedxComponent{}, Dependencies: []cyclonedxDependency{{Ref: name, DependsOn: []string{}}},
	}
	data, err := canonicalJSON(document)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
