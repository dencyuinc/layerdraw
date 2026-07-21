// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildAndVerifyTestSignedUpdate(t *testing.T) {
	root, manifestPath := buildFixture(t, true)
	if err := run([]string{"verify", "-manifest", manifestPath, "-root", root, "-platform", "darwin", "-channel", "stable", "-current-version", "1.1.0", "-allow-test-signing"}); err != nil {
		t.Fatal(err)
	}
}

func TestMergeSBOMClosesDesktopAndCompanionGraphs(t *testing.T) {
	root := t.TempDir()
	desktop := filepath.Join(root, "desktop.json")
	companion := filepath.Join(root, "host.json")
	output := filepath.Join(root, "bundle.json")
	documents := map[string]string{
		desktop:   `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,"metadata":{"component":{"bom-ref":"desktop","name":"Desktop"}},"components":[{"bom-ref":"shared","name":"shared"}],"dependencies":[{"ref":"desktop","dependsOn":["shared"]},{"ref":"shared","dependsOn":[]}]}`,
		companion: `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,"metadata":{"component":{"bom-ref":"host","name":"Host"}},"components":[{"bom-ref":"shared","name":"shared"},{"bom-ref":"native","name":"native"}],"dependencies":[{"ref":"host","dependsOn":["shared","native"]},{"ref":"native","dependsOn":[]}]}`,
	}
	for path, content := range documents {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := run([]string{"merge-sbom", "-desktop", desktop, "-companion", companion, "-output", output}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var bundle sbomDocument
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatal(err)
	}
	if len(bundle.Components) != 3 {
		t.Fatalf("expected shared, host, native components, got %d", len(bundle.Components))
	}
	foundHost := false
	for _, dependency := range bundle.Dependencies {
		if dependency["ref"] != "desktop" {
			continue
		}
		for _, target := range dependency["dependsOn"].([]any) {
			if target == "host" {
				foundHost = true
			}
		}
	}
	if !foundHost {
		t.Fatal("Desktop root does not depend on companion host")
	}
}

func TestMergeSBOMRejectsIncompleteOrAmbiguousGraphs(t *testing.T) {
	if err := run([]string{"merge-sbom"}); err == nil {
		t.Fatal("expected incomplete arguments")
	}
	root := t.TempDir()
	valid := filepath.Join(root, "valid.json")
	invalid := filepath.Join(root, "invalid.json")
	output := filepath.Join(root, "output.json")
	document := `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,"metadata":{"component":{"bom-ref":"same","name":"root"}},"components":[],"dependencies":[]}`
	if err := os.WriteFile(valid, []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"merge-sbom", "-desktop", valid, "-companion", valid, "-output", output}); err == nil || !strings.Contains(err.Error(), "must differ") {
		t.Fatalf("expected duplicate root rejection, got %v", err)
	}
	if err := os.WriteFile(invalid, []byte(`{"bomFormat":"SPDX","specVersion":"1.6"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"merge-sbom", "-desktop", invalid, "-companion", valid, "-output", output}); err == nil || !strings.Contains(err.Error(), "not CycloneDX") {
		t.Fatalf("expected format rejection, got %v", err)
	}
}

func TestBuildFailsClosedWithoutReleaseKey(t *testing.T) {
	root := t.TempDir()
	files := fixtureFiles(t, root)
	err := run(buildArgs(root, files, false))
	if err == nil || !strings.Contains(err.Error(), "fails closed") {
		t.Fatalf("expected fail-closed signing error, got %v", err)
	}
}

func TestBuildAndVerifyReleaseSignature(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_DESKTOP_SIGNING_KEY", base64.StdEncoding.EncodeToString(privateKey))
	root := t.TempDir()
	files := fixtureFiles(t, root)
	platformSignature := filepath.Join(root, "LayerDraw.dmg.sig")
	if err := os.WriteFile(platformSignature, []byte("detached signature"), 0o644); err != nil {
		t.Fatal(err)
	}
	args := buildArgs(root, files, false)
	args = append(args, "-signing-key-env", "TEST_DESKTOP_SIGNING_KEY", "-platform-signature", platformSignature)
	if err := run(args); err != nil {
		t.Fatal(err)
	}
	err = run([]string{"verify", "-manifest", filepath.Join(root, "update.json"), "-root", root, "-platform", "darwin", "-current-version", "1.1.0", "-trusted-public-key", base64.StdEncoding.EncodeToString(publicKey)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(platformSignature, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = run([]string{"verify", "-manifest", filepath.Join(root, "update.json"), "-root", root, "-platform", "darwin", "-current-version", "1.1.0", "-trusted-public-key", base64.StdEncoding.EncodeToString(publicKey)})
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("expected detached signature digest rejection, got %v", err)
	}
}

func TestVerifyRejectsUntrustedTestSignature(t *testing.T) {
	root, manifestPath := buildFixture(t, true)
	err := run([]string{"verify", "-manifest", manifestPath, "-root", root, "-platform", "darwin", "-current-version", "1.1.0"})
	if err == nil || !strings.Contains(err.Error(), "test-signed") {
		t.Fatalf("expected test-signing rejection, got %v", err)
	}
}

func TestVerifyRejectsDigestMismatchDowngradeAndIncompatibleClient(t *testing.T) {
	tests := []struct {
		name, current, mutate, want string
	}{
		{name: "digest", current: "1.1.0", mutate: "installer", want: "digest mismatch"},
		{name: "conformance digest", current: "1.1.0", mutate: "conformance", want: "digest mismatch"},
		{name: "disabled features digest", current: "1.1.0", mutate: "disabled", want: "digest mismatch"},
		{name: "attestation digest", current: "1.1.0", mutate: "attestation", want: "digest mismatch"},
		{name: "downgrade", current: "1.2.0", want: "downgrade or reinstall"},
		{name: "incompatible", current: "0.9.0", want: "incompatible"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, manifestPath := buildFixture(t, true)
			if test.mutate != "" {
				path := map[string]string{"installer": "LayerDraw.dmg", "conformance": "desktop-conformance.json", "disabled": "desktop-disabled-features.json", "attestation": "desktop-attestation.json"}[test.mutate]
				if err := os.WriteFile(filepath.Join(root, path), []byte("tampered"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			err := run([]string{"verify", "-manifest", manifestPath, "-root", root, "-platform", "darwin", "-current-version", test.current, "-allow-test-signing"})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}

func TestVerifyRejectsMutatedSignedMetadata(t *testing.T) {
	root, manifestPath := buildFixture(t, true)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest updateManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Version = "9.9.9"
	data, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	err = run([]string{"verify", "-manifest", manifestPath, "-root", root, "-platform", "darwin", "-current-version", "1.1.0", "-allow-test-signing"})
	if err == nil || !strings.Contains(err.Error(), "signature verification failed") {
		t.Fatalf("expected signature rejection, got %v", err)
	}
}

func TestValidateTargetAndVersions(t *testing.T) {
	for _, target := range [][2]string{{"darwin", "dmg"}, {"windows", "nsis"}, {"linux", "deb"}} {
		if err := validateTarget(target[0], target[1], "stable"); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateTarget("linux", "dmg", "stable"); err == nil {
		t.Fatal("expected invalid format")
	}
	if _, err := parseVersion("1.02.3"); err == nil {
		t.Fatal("expected invalid version")
	}
	if compareVersion(version{core: [3]int{1, 2, 0}}, version{core: [3]int{1, 1, 9}}) <= 0 {
		t.Fatal("comparison failed")
	}
	betaOne, err := parseVersion("1.2.0-beta.1")
	if err != nil {
		t.Fatal(err)
	}
	betaTwo, err := parseVersion("1.2.0-beta.2")
	if err != nil {
		t.Fatal(err)
	}
	stable, err := parseVersion("1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if compareVersion(betaTwo, betaOne) <= 0 || compareVersion(stable, betaTwo) <= 0 {
		t.Fatal("prerelease precedence failed")
	}
	ordered := []string{"1.2.0-1", "1.2.0-alpha", "1.2.0-alpha.1", "1.2.0-beta.2", "1.2.0-beta.11", "1.2.0-rc.1", "1.2.0"}
	for index := 1; index < len(ordered); index++ {
		left, err := parseVersion(ordered[index-1])
		if err != nil {
			t.Fatal(err)
		}
		right, err := parseVersion(ordered[index])
		if err != nil {
			t.Fatal(err)
		}
		if compareVersion(left, right) >= 0 {
			t.Fatalf("expected %s before %s", ordered[index-1], ordered[index])
		}
	}
	for _, invalid := range []string{"1.2.0-", "1.2.0-beta_1", "1.2.0-beta.01"} {
		if _, err := parseVersion(invalid); err == nil {
			t.Fatalf("accepted invalid prerelease %s", invalid)
		}
	}
	if err := validateChannelVersion("beta", "1.2.0-beta.1"); err != nil {
		t.Fatal(err)
	}
	if err := validateChannelVersion("stable", "1.2.0-beta.1"); err == nil {
		t.Fatal("stable accepted prerelease")
	}
	if err := validateChannelVersion("nightly", "1.2.0"); err == nil {
		t.Fatal("nightly accepted stable version")
	}
}

func TestLocalArtifactPathRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"../installer.dmg", `nested\\installer.dmg`, "...dmg", ""} {
		if _, err := localArtifactPath(root, name); err == nil {
			t.Fatalf("unsafe artifact path %q accepted", name)
		}
	}
	path, err := localArtifactPath(root, "LayerDraw.dmg")
	if err != nil || path != filepath.Join(root, "LayerDraw.dmg") {
		t.Fatalf("safe artifact path=%q err=%v", path, err)
	}
}

func TestBetaChannelAcceptsMonotonicPrerelease(t *testing.T) {
	root := t.TempDir()
	files := fixtureFiles(t, root)
	args := buildArgs(root, files, true)
	setFlag(args, "-version", "1.2.0-beta.2")
	setFlag(args, "-channel", "beta")
	if err := run(args); err != nil {
		t.Fatal(err)
	}
	err := run([]string{"verify", "-manifest", filepath.Join(root, "update.json"), "-root", root, "-platform", "darwin", "-channel", "beta", "-current-version", "1.2.0-beta.1", "-allow-test-signing"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsTrailingManifestContent(t *testing.T) {
	root, manifestPath := buildFixture(t, true)
	file, err := os.OpenFile(manifestPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{}\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	err = run([]string{"verify", "-manifest", manifestPath, "-root", root, "-platform", "darwin", "-current-version", "1.1.0", "-allow-test-signing"})
	if err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("expected trailing JSON rejection, got %v", err)
	}
}

func TestInvalidCommandsAndCapabilitiesFailClosed(t *testing.T) {
	if err := run(nil); err == nil {
		t.Fatal("expected missing command error")
	}
	if err := run([]string{"unknown"}); err == nil {
		t.Fatal("expected unknown command error")
	}
	if err := run([]string{"build"}); err == nil {
		t.Fatal("expected incomplete build error")
	}
	if err := run([]string{"verify"}); err == nil {
		t.Fatal("expected incomplete verify error")
	}
	root := t.TempDir()
	path := filepath.Join(root, "capabilities.json")
	valid := string(validCapabilityFixture(t))
	if err := os.WriteFile(path, []byte(strings.Replace(valid, `"engine"`, `"engine_missing"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateCapabilities(path); err == nil {
		t.Fatalf("expected closure error, got %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(valid, `"signing_secrets": false`, `"signing_secrets": true`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateCapabilities(path); err == nil || !strings.Contains(err.Error(), "exposes") {
		t.Fatalf("expected security error, got %v", err)
	}
}

func TestVerifyRejectsWrongPlatformChannelAndKey(t *testing.T) {
	root, manifestPath := buildFixture(t, true)
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "platform", args: []string{"-platform", "linux", "-channel", "stable", "-allow-test-signing"}, want: "incompatible"},
		{name: "channel", args: []string{"-platform", "darwin", "-channel", "beta", "-allow-test-signing"}, want: "incompatible"},
		{name: "key", args: []string{"-platform", "darwin", "-channel", "stable", "-allow-test-signing", "-trusted-public-key", base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize))}, want: "identity"},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := []string{"verify", "-manifest", manifestPath, "-root", root, "-current-version", "1.1.0"}
			args = append(args, test.args...)
			err := run(args)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}

func TestBuildRejectsInvalidReleaseInputs(t *testing.T) {
	root := t.TempDir()
	files := fixtureFiles(t, root)
	for _, test := range []struct{ name, flag, value, want string }{
		{name: "revision", flag: "-source-revision", value: "abc", want: "source revision"},
		{name: "time", flag: "-built-at", value: "yesterday", want: "built-at"},
		{name: "version", flag: "-version", value: "1.2", want: "version"},
		{name: "minimum", flag: "-minimum-supported-version", value: "01.0.0", want: "minimum-supported-version"},
		{name: "target", flag: "-format", value: "zip", want: "unsupported Desktop target"},
		{name: "channel", flag: "-channel", value: "unsafe", want: "unsupported update channel"},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := buildArgs(root, files, true)
			setFlag(args, test.flag, test.value)
			err := run(args)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
	t.Setenv("BAD_DESKTOP_KEY", "not-base64")
	args := buildArgs(root, files, false)
	args = append(args, "-signing-key-env", "BAD_DESKTOP_KEY")
	if err := run(args); err == nil || !strings.Contains(err.Error(), "base64 Ed25519") {
		t.Fatalf("expected key error, got %v", err)
	}
	t.Setenv("CONFLICTING_KEY", base64.StdEncoding.EncodeToString(make([]byte, ed25519.PrivateKeySize)))
	args = buildArgs(root, files, true)
	args = append(args, "-signing-key-env", "CONFLICTING_KEY")
	if err := run(args); err == nil || !strings.Contains(err.Error(), "refuses") {
		t.Fatalf("expected isolation error, got %v", err)
	}
}

func buildFixture(t *testing.T, testSigning bool) (string, string) {
	t.Helper()
	root := t.TempDir()
	files := fixtureFiles(t, root)
	if err := run(buildArgs(root, files, testSigning)); err != nil {
		t.Fatal(err)
	}
	return root, filepath.Join(root, "update.json")
}

func fixtureFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	contents := map[string]string{
		"installer": "desktop installer", "sbom": `{"bomFormat":"CycloneDX"}`,
		"licenses": "Third-party notices", "capabilities": string(validCapabilityFixture(t)),
		"conformance": `{"schema_version":1,"delivery":"desktop","normative_matrix":"docs/blueprint.md#1311-feature-x-delivery-matrix","features":{"F01":{"feature":"Project management","delivered":false,"evidence":[]}},"acceptance_suites":{},"faults":{},"release_evidence":[],"performance_budgets":{}}`,
		"disabled":    `{"schema_version":1,"delivery":"desktop","normative_matrix":"docs/blueprint.md#1311-feature-x-delivery-matrix","disabled_features":[{"feature_id":"F01","feature":"Project management","status":"disabled","reason_code":"server_only","reason":"Not in Desktop."}]}`,
		"attestation": `{"schema_version":1,"source_revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`,
	}
	result := map[string]string{}
	for name, content := range contents {
		filename := map[string]string{"installer": "LayerDraw.dmg", "sbom": "LayerDraw.cdx.json", "licenses": "THIRD_PARTY_NOTICES.txt", "capabilities": "desktop-capabilities.json", "conformance": "desktop-conformance.json", "disabled": "desktop-disabled-features.json", "attestation": "desktop-attestation.json"}[name]
		path := filepath.Join(root, filename)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		result[name] = path
	}
	return result
}

func validCapabilityFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "deploy", "desktop-capabilities.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func buildArgs(root string, files map[string]string, testSigning bool) []string {
	args := []string{"build", "-installer", files["installer"], "-sbom", files["sbom"], "-licenses", files["licenses"], "-capabilities", files["capabilities"], "-desktop-conformance", files["conformance"], "-disabled-features", files["disabled"], "-desktop-attestation", files["attestation"], "-output", filepath.Join(root, "update.json"), "-version", "1.2.0", "-minimum-supported-version", "1.0.0", "-platform", "darwin", "-format", "dmg", "-channel", "stable", "-source-revision", strings.Repeat("a", 40), "-built-at", "2026-07-20T00:00:00Z"}
	if testSigning {
		args = append(args, "-test-signing")
	}
	return args
}

func setFlag(args []string, name, value string) {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == name {
			args[index+1] = value
			return
		}
	}
}
