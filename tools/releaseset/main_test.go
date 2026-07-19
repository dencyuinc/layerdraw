// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	if len(manifest.Artifacts) != 5 || manifest.ReleaseVersion != testVersion || manifest.Protocols[0].Versions[0].SchemaDigest != engineprotocol.SchemaDigest {
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
	if _, err := validateCycloneDX([]byte("{"), "x", testVersion); err == nil {
		t.Fatal("malformed CycloneDX was accepted")
	}
	if _, err := validateCycloneDX(cycloneDXBytes(t, "wrong", testVersion), "x", testVersion); err == nil {
		t.Fatal("mismatched CycloneDX was accepted")
	}
	closure, err := validateCycloneDX(cycloneDXBytes(t, "x", testVersion), "x", testVersion)
	if err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("1", 64)
	spdx, err := renderSPDX("x", testVersion, testBuiltAt, digest, closure)
	if err != nil || validateSPDX(spdx, "x", testVersion, digest, closure) != nil {
		t.Fatalf("valid SPDX rejected: %v", err)
	}
	if err := validateSPDX([]byte("{"), "x", testVersion, digest, closure); err == nil {
		t.Fatal("malformed SPDX was accepted")
	}
	if err := validateSPDX(spdx, "wrong", testVersion, digest, closure); err == nil {
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
	symlink := filepath.Join(output, "linked-artifact")
	if err := os.Symlink(outsideLegal, symlink); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	size, linkedDigest, err := fileIdentity(outsideLegal)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyRelativeFile(output, "linked-artifact", fmt.Sprintf("%d", size), linkedDigest); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("symlinked release file error=%v", err)
	}
}

func TestLegalAuthoritiesBindCompleteRuntimeClosure(t *testing.T) {
	data := cycloneDXBytesWithDependency(t, "x", testVersion, "example.com/runtime", "v1.0.0", "MIT")
	closure, err := validateCycloneDX(data, "x", testVersion)
	if err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("2", 64)
	spdx, err := renderSPDX("x", testVersion, testBuiltAt, digest, closure)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSPDX(spdx, "x", testVersion, digest, closure); err != nil {
		t.Fatal(err)
	}
	if err := validateNotices([]byte("example.com/runtime v1.0.0\nLicense: MIT\n"), closure); err != nil {
		t.Fatal(err)
	}
	if err := validateNotices([]byte("incomplete\n"), closure); err == nil {
		t.Fatal("incomplete runtime notices were accepted")
	}
	var forged spdxDocument
	if err := json.Unmarshal(spdx, &forged); err != nil {
		t.Fatal(err)
	}
	forged.Packages = forged.Packages[:1]
	forgedBytes, err := canonicalJSON(forged)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSPDX(forgedBytes, "x", testVersion, digest, closure); err == nil {
		t.Fatal("truncated SPDX runtime closure was accepted")
	}
	if _, err := validateCycloneDX(cycloneDXBytesWithUnreachableDependency(t, "x", testVersion), "x", testVersion); err == nil {
		t.Fatal("unreachable CycloneDX component was accepted")
	}
}

func TestAuthorityValidatorsRejectStructuralForgery(t *testing.T) {
	data := cycloneDXBytesWithDependency(t, "x", testVersion, "example.com/runtime", "v1.0.0", "MIT")
	closure, err := validateCycloneDX(data, "x", testVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePackageSBOM(packageAuthority{Name: "wrong", Version: testVersion}, closure); err == nil {
		t.Fatal("mismatched package root was accepted")
	}
	if err := validatePackageSBOM(packageAuthority{Name: "x", Version: testVersion, Dependencies: map[string]string{"missing": "v1.0.0"}}, closure); err == nil {
		t.Fatal("missing package dependency was accepted")
	}
	if got := componentLicense(cyclonedxComponent{Licenses: namedLicenses("LayerDraw License 1.0")}); got != "LicenseRef-LayerDraw-1.0" {
		t.Fatalf("named license=%q", got)
	}
	if got := componentLicense(cyclonedxComponent{}); got != "NOASSERTION" {
		t.Fatalf("missing license=%q", got)
	}
	if normalizeSPDXLicense("SEE LICENSE IN LICENSE") != "NOASSERTION" || normalizeSPDXLicense("MIT") != "MIT" {
		t.Fatal("SPDX license normalization mismatch")
	}

	mutateCycloneDX := func(mutate func(*cyclonedxDocument)) []byte {
		t.Helper()
		var document cyclonedxDocument
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		mutate(&document)
		result, err := canonicalJSON(document)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	for name, forged := range map[string][]byte{
		"duplicate component": mutateCycloneDX(func(value *cyclonedxDocument) { value.Components = append(value.Components, value.Components[0]) }),
		"unknown source":      mutateCycloneDX(func(value *cyclonedxDocument) { value.Dependencies[1].Ref = "unknown" }),
		"duplicate source":    mutateCycloneDX(func(value *cyclonedxDocument) { value.Dependencies = append(value.Dependencies, value.Dependencies[0]) }),
		"unknown target":      mutateCycloneDX(func(value *cyclonedxDocument) { value.Dependencies[0].DependsOn = []string{"unknown"} }),
		"duplicate edge": mutateCycloneDX(func(value *cyclonedxDocument) {
			value.Dependencies[0].DependsOn = append(value.Dependencies[0].DependsOn, value.Dependencies[0].DependsOn[0])
		}),
		"incomplete graph": mutateCycloneDX(func(value *cyclonedxDocument) { value.Dependencies = value.Dependencies[:1] }),
	} {
		if _, err := validateCycloneDX(forged, "x", testVersion); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}

	digest := "sha256:" + strings.Repeat("3", 64)
	spdx, err := renderSPDX("x", testVersion, testBuiltAt, digest, closure)
	if err != nil {
		t.Fatal(err)
	}
	mutateSPDX := func(mutate func(*spdxDocument)) []byte {
		t.Helper()
		var document spdxDocument
		if err := json.Unmarshal(spdx, &document); err != nil {
			t.Fatal(err)
		}
		mutate(&document)
		result, err := canonicalJSON(document)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	for name, forged := range map[string][]byte{
		"duplicate package":      mutateSPDX(func(value *spdxDocument) { value.Packages = append(value.Packages, value.Packages[0]) }),
		"forged checksum":        mutateSPDX(func(value *spdxDocument) { value.Packages[0].Checksums[0].ChecksumValue = strings.Repeat("0", 64) }),
		"duplicate relationship": mutateSPDX(func(value *spdxDocument) { value.Relationships = append(value.Relationships, value.Relationships[0]) }),
		"missing relationship":   mutateSPDX(func(value *spdxDocument) { value.Relationships = value.Relationships[:1] }),
	} {
		if err := validateSPDX(forged, "x", testVersion, digest, closure); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	firstParty := cyclonedxAuthority{Components: map[string]cyclonedxComponent{"protocol": {Name: "@layerdraw/protocol", Version: testVersion, Licenses: licenses("Apache-2.0")}}}
	if err := validateNotices([]byte("No third-party dependencies.\n"), firstParty); err != nil {
		t.Fatal(err)
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

func TestDesktopFTSComponentBindsPackagedExtensionDigest(t *testing.T) {
	digest := strings.Repeat("a", 64)
	ref := "pkg:generic/ladybugdb-fts-extension@0.17.0"
	component := cyclonedxComponent{Type: "library", BOMRef: ref, Name: "LadybugDB FTS extension", Version: "0.17.0", PURL: ref, Hashes: []cyclonedxHash{{Algorithm: "SHA-256", Content: digest}}, Licenses: licenses("MIT")}
	closure := cyclonedxAuthority{Components: map[string]cyclonedxComponent{ref: component}}
	authority := desktopNativeAuthority{LadybugVersion: "0.17.0", FTSSHA256: digest}
	if err := validateDesktopFTSComponent(closure, authority); err != nil {
		t.Fatal(err)
	}
	component.Hashes[0].Content = strings.Repeat("b", 64)
	closure.Components[ref] = component
	if err := validateDesktopFTSComponent(closure, authority); err == nil {
		t.Fatal("forged FTS component digest was accepted")
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
	if err := os.WriteFile(nativeSBOM, cycloneDXBytesWithDependency(t, "layerdraw-engine", testVersion, "example.com/native", "v1.0.0", "MIT"), 0o644); err != nil {
		t.Fatal(err)
	}
	nativeNotices := filepath.Join(t.TempDir(), "THIRD_PARTY_NOTICES.txt")
	if err := os.WriteFile(nativeNotices, []byte("example.com/native v1.0.0\nLicense: MIT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	desktopLegal := filepath.Join(output, "desktop-native-legal")
	if err := os.MkdirAll(desktopLegal, 0o755); err != nil {
		t.Fatal(err)
	}
	ftsExtension := []byte("verified extension")
	ftsDigest := sha256.Sum256(ftsExtension)
	desktopSBOM := cycloneDXBytesWithDependency(t, "layerdraw-host-native", testVersion, "LadybugDB FTS extension", "0.17.0", "MIT")
	var desktopDocument cyclonedxDocument
	if err := json.Unmarshal(desktopSBOM, &desktopDocument); err != nil {
		t.Fatal(err)
	}
	desktopDocument.Components[0].BOMRef = "pkg:generic/ladybugdb-fts-extension@0.17.0"
	desktopDocument.Components[0].PURL = desktopDocument.Components[0].BOMRef
	desktopDocument.Components[0].Hashes = []cyclonedxHash{{Algorithm: "SHA-256", Content: hex.EncodeToString(ftsDigest[:])}}
	desktopDocument.Dependencies[0].DependsOn[0] = desktopDocument.Components[0].BOMRef
	desktopDocument.Dependencies[1].Ref = desktopDocument.Components[0].BOMRef
	desktopSBOM, err := canonicalJSON(desktopDocument)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(desktopLegal, "layerdraw-host-native.cdx.json"), desktopSBOM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(desktopLegal, "THIRD_PARTY_NOTICES.txt"), []byte("LadybugDB FTS extension 0.17.0\nLicense: MIT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	desktopAuthority, err := json.Marshal(desktopNativeAuthority{
		LadybugVersion: "0.17.0",
		Platform:       runtime.GOOS + "/" + runtime.GOARCH,
		FTSExtension:   "libfts.lbug_extension",
		FTSSHA256:      hex.EncodeToString(ftsDigest[:]),
		Host:           "layerdraw-host-native",
	})
	if err != nil {
		t.Fatal(err)
	}
	writeArchive(t, filepath.Join(output, "artifacts", "layerdraw-host-native-"+testVersion+".tar.gz"), map[string][]byte{
		"layerdraw-host-native": []byte("#!/bin/sh\nprintf 'layerdraw-host " + testVersion + " (test)\\n'\n"),
		"libfts.lbug_extension": ftsExtension,
		"ladybug-native.json":   append(desktopAuthority, '\n'),
	})
	writePackage(t, filepath.Join(output, "artifacts", "layerdraw-protocol-"+testVersion+".tgz"), packageAuthority{Name: "@layerdraw/protocol", Version: packageVersion, License: "Apache-2.0"}, nil)
	wasmSBOM := cycloneDXBytesWithDependency(t, "@layerdraw/engine-wasm", testVersion, "example.com/wasm", "v2.0.0", "BSD-3-Clause")
	writePackage(t, filepath.Join(output, "artifacts", "layerdraw-engine-wasm-"+testVersion+".tgz"), packageAuthority{Name: "@layerdraw/engine-wasm", Version: packageVersion, License: "SEE LICENSE IN LICENSE"}, map[string][]byte{"package/dist/engine-wasm.cdx.json": wasmSBOM, "package/THIRD_PARTY_NOTICES.txt": []byte("example.com/wasm v2.0.0\nLicense: BSD-3-Clause\n")})
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

func cycloneDXBytesWithDependency(t *testing.T, name, version, dependencyName, dependencyVersion, dependencyLicense string) []byte {
	t.Helper()
	rootRef := name
	dependencyRef := "pkg:generic/" + dependencyName + "@" + dependencyVersion
	document := cyclonedxDocument{
		Schema: "http://cyclonedx.org/schema/bom-1.6.schema.json", BOMFormat: "CycloneDX", SpecVersion: "1.6", Version: 1,
		Metadata:     cyclonedxMetadata{Timestamp: testBuiltAt, Component: cyclonedxComponent{Type: "application", BOMRef: rootRef, Name: name, Version: version, Licenses: licenses("LicenseRef-LayerDraw-1.0")}},
		Components:   []cyclonedxComponent{{Type: "library", BOMRef: dependencyRef, Name: dependencyName, Version: dependencyVersion, Licenses: licenses(dependencyLicense)}},
		Dependencies: []cyclonedxDependency{{Ref: rootRef, DependsOn: []string{dependencyRef}}, {Ref: dependencyRef, DependsOn: []string{}}},
	}
	data, err := canonicalJSON(document)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func cycloneDXBytesWithUnreachableDependency(t *testing.T, name, version string) []byte {
	t.Helper()
	data := cycloneDXBytesWithDependency(t, name, version, "example.com/runtime", "v1.0.0", "MIT")
	var document cyclonedxDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document.Dependencies[0].DependsOn = []string{}
	result, err := canonicalJSON(document)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func namedLicenses(name string) []cyclonedxLicense {
	value := cyclonedxLicense{}
	value.License.Name = name
	return []cyclonedxLicense{value}
}
