// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Command desktoprelease creates and verifies signed Desktop update metadata.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const schemaVersion = 1

type digestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type provenance struct {
	SourceRepository string `json:"source_repository"`
	SourceRevision   string `json:"source_revision"`
	BuildWorkflow    string `json:"build_workflow"`
	BuiltAt          string `json:"built_at"`
}

type signature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id"`
	PublicKey string `json:"public_key"`
	Value     string `json:"value"`
}

type updateManifest struct {
	SchemaVersion           int         `json:"schema_version"`
	Channel                 string      `json:"channel"`
	Version                 string      `json:"version"`
	MinimumSupportedVersion string      `json:"minimum_supported_version"`
	Platform                string      `json:"platform"`
	Format                  string      `json:"format"`
	SigningMode             string      `json:"signing_mode"`
	Installer               digestFile  `json:"installer"`
	SBOM                    digestFile  `json:"sbom"`
	Licenses                digestFile  `json:"licenses"`
	Capabilities            digestFile  `json:"capabilities"`
	Conformance             digestFile  `json:"desktop_conformance"`
	Attestation             digestFile  `json:"desktop_attestation"`
	PlatformSignature       *digestFile `json:"platform_signature,omitempty"`
	Provenance              provenance  `json:"provenance"`
	Signature               signature   `json:"signature"`
}

type capabilityDeclaration struct {
	SchemaVersion int      `json:"schema_version"`
	Components    []string `json:"components"`
	Excludes      []string `json:"excludes"`
	Security      struct {
		PreconfiguredMCPEndpoints bool `json:"preconfigured_mcp_endpoints"`
		ProviderCredentials       bool `json:"provider_credentials"`
		SigningSecrets            bool `json:"signing_secrets"`
	} `json:"security"`
}

var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var prereleaseIdentifierPattern = regexp.MustCompile(`^[0-9A-Za-z-]+$`)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "desktoprelease:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("expected subcommand: build or verify")
	}
	switch args[0] {
	case "build":
		return buildCommand(args[1:])
	case "verify":
		return verifyCommand(args[1:])
	case "merge-sbom":
		return mergeSBOMCommand(args[1:])
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

type sbomDocument struct {
	Schema       string           `json:"$schema"`
	BOMFormat    string           `json:"bomFormat"`
	SpecVersion  string           `json:"specVersion"`
	SerialNumber string           `json:"serialNumber,omitempty"`
	Version      int              `json:"version"`
	Metadata     map[string]any   `json:"metadata"`
	Components   []map[string]any `json:"components"`
	Dependencies []map[string]any `json:"dependencies"`
}

func mergeSBOMCommand(args []string) error {
	flags := flag.NewFlagSet("merge-sbom", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	desktop := flags.String("desktop", "", "Desktop executable CycloneDX document")
	companion := flags.String("companion", "", "companion runtime CycloneDX document")
	output := flags.String("output", "", "complete Desktop bundle CycloneDX document")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *desktop == "" || *companion == "" || *output == "" {
		return errors.New("merge-sbom requires desktop, companion, and output")
	}
	primary, err := readSBOM(*desktop)
	if err != nil {
		return err
	}
	secondary, err := readSBOM(*companion)
	if err != nil {
		return err
	}
	primaryRoot, err := sbomRootRef(primary)
	if err != nil {
		return fmt.Errorf("desktop SBOM: %w", err)
	}
	secondaryRoot, err := sbomRootRef(secondary)
	if err != nil {
		return fmt.Errorf("companion SBOM: %w", err)
	}
	if primaryRoot == secondaryRoot {
		return errors.New("Desktop and companion SBOM roots must differ")
	}
	secondaryComponent, ok := secondary.Metadata["component"].(map[string]any)
	if !ok {
		return errors.New("companion SBOM root component is invalid")
	}
	components := map[string]bool{}
	for _, component := range primary.Components {
		if ref, _ := component["bom-ref"].(string); ref != "" {
			components[ref] = true
		}
	}
	for _, component := range append([]map[string]any{secondaryComponent}, secondary.Components...) {
		ref, _ := component["bom-ref"].(string)
		if ref == "" {
			return errors.New("SBOM component is missing bom-ref")
		}
		if !components[ref] {
			primary.Components = append(primary.Components, component)
			components[ref] = true
		}
	}
	dependencies := map[string]map[string]bool{}
	for _, dependency := range append(primary.Dependencies, secondary.Dependencies...) {
		ref, _ := dependency["ref"].(string)
		if ref == "" {
			return errors.New("SBOM dependency is missing ref")
		}
		if dependencies[ref] == nil {
			dependencies[ref] = map[string]bool{}
		}
		if values, ok := dependency["dependsOn"].([]any); ok {
			for _, value := range values {
				if target, ok := value.(string); ok {
					dependencies[ref][target] = true
				}
			}
		}
	}
	if dependencies[primaryRoot] == nil {
		dependencies[primaryRoot] = map[string]bool{}
	}
	dependencies[primaryRoot][secondaryRoot] = true
	primary.Dependencies = primary.Dependencies[:0]
	refs := make([]string, 0, len(dependencies))
	for ref := range dependencies {
		refs = append(refs, ref)
	}
	slices.Sort(refs)
	for _, ref := range refs {
		targets := make([]string, 0, len(dependencies[ref]))
		for target := range dependencies[ref] {
			targets = append(targets, target)
		}
		slices.Sort(targets)
		primary.Dependencies = append(primary.Dependencies, map[string]any{"ref": ref, "dependsOn": targets})
	}
	encoded, err := json.MarshalIndent(primary, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(*output, append(encoded, '\n'), 0o644)
}

func readSBOM(path string) (sbomDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return sbomDocument{}, err
	}
	var document sbomDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return sbomDocument{}, fmt.Errorf("invalid SBOM %s: %w", path, err)
	}
	if document.BOMFormat != "CycloneDX" || document.SpecVersion == "" {
		return sbomDocument{}, fmt.Errorf("%s is not CycloneDX", path)
	}
	return document, nil
}

func sbomRootRef(document sbomDocument) (string, error) {
	component, ok := document.Metadata["component"].(map[string]any)
	if !ok {
		return "", errors.New("root component is missing")
	}
	ref, _ := component["bom-ref"].(string)
	if ref == "" {
		return "", errors.New("root component bom-ref is missing")
	}
	return ref, nil
}

func buildCommand(args []string) error {
	flags := flag.NewFlagSet("build", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	installer := flags.String("installer", "", "installer artifact")
	sbom := flags.String("sbom", "", "artifact-specific CycloneDX SBOM")
	licenses := flags.String("licenses", "", "third-party license bundle")
	capabilities := flags.String("capabilities", "", "packaged capability declaration")
	conformance := flags.String("desktop-conformance", "", "machine-checked Desktop feature closure")
	attestation := flags.String("desktop-attestation", "", "signed installed Desktop conformance attestation")
	platformSignature := flags.String("platform-signature", "", "optional detached platform signature")
	output := flags.String("output", "", "output update manifest")
	version := flags.String("version", "", "release version")
	minimum := flags.String("minimum-supported-version", "", "oldest version eligible to update")
	platform := flags.String("platform", "", "darwin, windows, or linux")
	format := flags.String("format", "", "dmg, nsis, or appimage")
	channel := flags.String("channel", "stable", "stable, beta, or nightly")
	revision := flags.String("source-revision", "", "full Git source revision")
	builtAt := flags.String("built-at", "", "RFC3339 build time")
	repository := flags.String("source-repository", "https://github.com/dencyuinc/layerdraw", "source repository")
	workflow := flags.String("build-workflow", ".github/workflows/desktop-release.yml", "build workflow identity")
	keyEnv := flags.String("signing-key-env", "LAYERDRAW_DESKTOP_SIGNING_KEY", "environment variable containing base64 Ed25519 private key")
	testSigning := flags.Bool("test-signing", false, "generate an ephemeral CI-only test key")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *installer == "" || *sbom == "" || *licenses == "" || *capabilities == "" || *conformance == "" || *attestation == "" || *output == "" || *version == "" || *minimum == "" || *platform == "" || *format == "" || *revision == "" || *builtAt == "" {
		return errors.New("build requires installer, sbom, licenses, capabilities, desktop-conformance, desktop-attestation, output, version, minimum-supported-version, platform, format, source-revision, and built-at")
	}
	if !revisionPattern.MatchString(*revision) {
		return errors.New("source revision must be 40 lowercase hexadecimal characters")
	}
	when, err := time.Parse(time.RFC3339, *builtAt)
	if err != nil || when.Format(time.RFC3339) != *builtAt {
		return errors.New("built-at must be a canonical RFC3339 time")
	}
	if err := validateTarget(*platform, *format, *channel); err != nil {
		return err
	}
	if _, err := parseVersion(*version); err != nil {
		return fmt.Errorf("version: %w", err)
	}
	if err := validateChannelVersion(*channel, *version); err != nil {
		return err
	}
	if _, err := parseVersion(*minimum); err != nil {
		return fmt.Errorf("minimum-supported-version: %w", err)
	}
	if err := validateCapabilities(*capabilities); err != nil {
		return err
	}
	privateKey, mode, err := signingKey(*keyEnv, *testSigning)
	if err != nil {
		return err
	}
	manifest := updateManifest{
		SchemaVersion: schemaVersion, Channel: *channel, Version: *version,
		MinimumSupportedVersion: *minimum, Platform: *platform, Format: *format, SigningMode: mode,
		Provenance: provenance{SourceRepository: *repository, SourceRevision: *revision, BuildWorkflow: *workflow, BuiltAt: *builtAt},
	}
	for source, target := range map[string]*digestFile{*installer: &manifest.Installer, *sbom: &manifest.SBOM, *licenses: &manifest.Licenses, *capabilities: &manifest.Capabilities, *conformance: &manifest.Conformance, *attestation: &manifest.Attestation} {
		value, err := describeFile(source)
		if err != nil {
			return err
		}
		*target = value
	}
	if *platformSignature != "" {
		value, err := describeFile(*platformSignature)
		if err != nil {
			return err
		}
		manifest.PlatformSignature = &value
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	keyDigest := sha256.Sum256(publicKey)
	manifest.Signature = signature{Algorithm: "Ed25519", KeyID: hex.EncodeToString(keyDigest[:8]), PublicKey: base64.StdEncoding.EncodeToString(publicKey)}
	payload, err := signingPayload(manifest)
	if err != nil {
		return err
	}
	manifest.Signature.Value = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		return err
	}
	return os.WriteFile(*output, append(encoded, '\n'), 0o644)
}

func verifyCommand(args []string) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	manifestPath := flags.String("manifest", "", "signed update manifest")
	root := flags.String("root", ".", "artifact root")
	platform := flags.String("platform", "", "expected platform")
	channel := flags.String("channel", "stable", "expected update channel")
	current := flags.String("current-version", "", "currently installed version")
	trustedKey := flags.String("trusted-public-key", "", "base64 trusted Ed25519 public key")
	allowTest := flags.Bool("allow-test-signing", false, "accept manifest-embedded CI test key")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *manifestPath == "" || *platform == "" || *current == "" {
		return errors.New("verify requires manifest, platform, and current-version")
	}
	data, err := os.ReadFile(*manifestPath)
	if err != nil {
		return err
	}
	var manifest updateManifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("invalid manifest: trailing JSON content")
	}
	if manifest.SchemaVersion != schemaVersion || manifest.Platform != *platform || manifest.Channel != *channel {
		return errors.New("manifest schema, platform, or channel is incompatible")
	}
	if err := validateTarget(manifest.Platform, manifest.Format, manifest.Channel); err != nil {
		return fmt.Errorf("manifest target is incompatible: %w", err)
	}
	if err := validateChannelVersion(manifest.Channel, manifest.Version); err != nil {
		return fmt.Errorf("manifest version is incompatible: %w", err)
	}
	if manifest.SigningMode == "test" && !*allowTest {
		return errors.New("test-signed manifests are not accepted without explicit opt-in")
	}
	publicText := *trustedKey
	if publicText == "" && *allowTest && manifest.SigningMode == "test" {
		publicText = manifest.Signature.PublicKey
	}
	publicKey, err := base64.StdEncoding.DecodeString(publicText)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return errors.New("a valid trusted Ed25519 public key is required")
	}
	if manifest.Signature.Algorithm != "Ed25519" || manifest.Signature.PublicKey != publicText {
		return errors.New("manifest signing identity does not match trusted key")
	}
	keyDigest := sha256.Sum256(publicKey)
	if manifest.Signature.KeyID != hex.EncodeToString(keyDigest[:8]) {
		return errors.New("manifest signing key ID does not match trusted key")
	}
	signed, err := base64.StdEncoding.DecodeString(manifest.Signature.Value)
	if err != nil {
		return errors.New("manifest signature is not valid base64")
	}
	payload, err := signingPayload(manifest)
	if err != nil || !ed25519.Verify(publicKey, payload, signed) {
		return errors.New("manifest signature verification failed")
	}
	currentVersion, err := parseVersion(*current)
	if err != nil {
		return fmt.Errorf("current-version: %w", err)
	}
	targetVersion, err := parseVersion(manifest.Version)
	if err != nil {
		return fmt.Errorf("manifest version: %w", err)
	}
	minimumVersion, err := parseVersion(manifest.MinimumSupportedVersion)
	if err != nil {
		return fmt.Errorf("minimum supported version: %w", err)
	}
	if compareVersion(targetVersion, currentVersion) <= 0 {
		return errors.New("update would be a downgrade or reinstall")
	}
	if compareVersion(currentVersion, minimumVersion) < 0 {
		return errors.New("current installation is incompatible with this update")
	}
	files := []digestFile{manifest.Installer, manifest.SBOM, manifest.Licenses, manifest.Capabilities, manifest.Conformance, manifest.Attestation}
	if manifest.PlatformSignature != nil {
		files = append(files, *manifest.PlatformSignature)
	}
	for _, file := range files {
		artifactPath, err := localArtifactPath(*root, file.Path)
		if err != nil {
			return err
		}
		actual, err := describeFile(artifactPath)
		if err != nil || actual.Size != file.Size || actual.SHA256 != file.SHA256 {
			return fmt.Errorf("artifact digest mismatch for %s", file.Path)
		}
	}
	return nil
}

func localArtifactPath(root, name string) (string, error) {
	if name == "" || strings.Contains(name, "..") || strings.ContainsAny(name, `/\\`) || filepath.Base(name) != name {
		return "", errors.New("artifact path must be a local basename")
	}
	return filepath.Join(root, name), nil
}

func validateChannelVersion(channel, releaseVersion string) error {
	prerelease := ""
	if parts := strings.SplitN(strings.TrimPrefix(releaseVersion, "v"), "-", 2); len(parts) == 2 {
		prerelease = parts[1]
	}
	switch channel {
	case "stable":
		if prerelease != "" {
			return errors.New("stable channel refuses prerelease versions")
		}
	case "beta":
		if !strings.HasPrefix(prerelease, "beta.") {
			return errors.New("beta channel requires a beta prerelease version")
		}
	case "nightly":
		if !strings.HasPrefix(prerelease, "nightly.") {
			return errors.New("nightly channel requires a nightly prerelease version")
		}
	default:
		return fmt.Errorf("unsupported update channel %q", channel)
	}
	return nil
}

func signingKey(envName string, test bool) (ed25519.PrivateKey, string, error) {
	if test {
		if os.Getenv(envName) != "" {
			return nil, "", errors.New("test signing refuses a configured production signing key")
		}
		_, privateKey, err := ed25519.GenerateKey(rand.Reader)
		return privateKey, "test", err
	}
	raw := os.Getenv(envName)
	if raw == "" {
		return nil, "", fmt.Errorf("release signing fails closed: %s is not set", envName)
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(decoded) != ed25519.PrivateKeySize {
		return nil, "", fmt.Errorf("%s must contain a base64 Ed25519 private key", envName)
	}
	return ed25519.PrivateKey(decoded), "release", nil
}

func signingPayload(manifest updateManifest) ([]byte, error) {
	manifest.Signature.Value = ""
	return json.Marshal(manifest)
}

func describeFile(path string) (digestFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return digestFile{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return digestFile{}, fmt.Errorf("%s is not a regular file", path)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return digestFile{}, err
	}
	return digestFile{Path: filepath.Base(path), Size: info.Size(), SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func validateTarget(platform, format, channel string) error {
	expected := map[string]string{"darwin": "dmg", "windows": "nsis", "linux": "deb"}
	if expected[platform] != format {
		return fmt.Errorf("unsupported Desktop target %s/%s", platform, format)
	}
	if channel != "stable" && channel != "beta" && channel != "nightly" {
		return fmt.Errorf("unsupported update channel %q", channel)
	}
	return nil
}

func validateCapabilities(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read capabilities: %w", err)
	}
	var declaration capabilityDeclaration
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&declaration); err != nil {
		return fmt.Errorf("invalid capability declaration: %w", err)
	}
	if declaration.SchemaVersion != 1 {
		return errors.New("unsupported capability declaration schema")
	}
	required := []string{"desktop-shell", "frontend-packages", "mcp-host", "native-adapters", "native-exporters", "registry", "review"}
	for _, component := range required {
		found := false
		for _, actual := range declaration.Components {
			if actual == component {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("packaged capability %q is missing", component)
		}
	}
	if declaration.Security.PreconfiguredMCPEndpoints || declaration.Security.ProviderCredentials || declaration.Security.SigningSecrets {
		return errors.New("packaged security declaration exposes runtime credentials or endpoints")
	}
	for _, excluded := range []string{"source-maps", "test-fixtures", "development-servers"} {
		found := false
		for _, actual := range declaration.Excludes {
			if actual == excluded {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("development-only exclusion %q is missing", excluded)
		}
	}
	return nil
}

type version struct {
	core       [3]int
	prerelease []string
}

func parseVersion(text string) (version, error) {
	value := strings.TrimPrefix(text, "v")
	if strings.Contains(value, "+") {
		value = strings.SplitN(value, "+", 2)[0]
	}
	segments := strings.SplitN(value, "-", 2)
	parts := strings.Split(segments[0], ".")
	if len(parts) != 3 {
		return version{}, errors.New("expected semantic version with three numeric components")
	}
	var result version
	for index, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 || (len(part) > 1 && part[0] == '0') {
			return version{}, errors.New("invalid semantic version")
		}
		result.core[index] = value
	}
	if len(segments) == 2 {
		if segments[1] == "" {
			return version{}, errors.New("invalid semantic version prerelease")
		}
		for _, identifier := range strings.Split(segments[1], ".") {
			if identifier == "" || !prereleaseIdentifierPattern.MatchString(identifier) {
				return version{}, errors.New("invalid semantic version prerelease")
			}
			if _, err := strconv.Atoi(identifier); err == nil && len(identifier) > 1 && identifier[0] == '0' {
				return version{}, errors.New("invalid semantic version prerelease")
			}
			result.prerelease = append(result.prerelease, identifier)
		}
	}
	return result, nil
}

func compareVersion(left, right version) int {
	for index := range left.core {
		if left.core[index] < right.core[index] {
			return -1
		}
		if left.core[index] > right.core[index] {
			return 1
		}
	}
	if len(left.prerelease) == 0 && len(right.prerelease) != 0 {
		return 1
	}
	if len(left.prerelease) != 0 && len(right.prerelease) == 0 {
		return -1
	}
	for index := 0; index < len(left.prerelease) && index < len(right.prerelease); index++ {
		leftNumber, leftErr := strconv.Atoi(left.prerelease[index])
		rightNumber, rightErr := strconv.Atoi(right.prerelease[index])
		if leftErr == nil && rightErr == nil {
			if leftNumber < rightNumber {
				return -1
			}
			if leftNumber > rightNumber {
				return 1
			}
			continue
		}
		if leftErr == nil {
			return -1
		}
		if rightErr == nil {
			return 1
		}
		if left.prerelease[index] < right.prerelease[index] {
			return -1
		}
		if left.prerelease[index] > right.prerelease[index] {
			return 1
		}
	}
	if len(left.prerelease) < len(right.prerelease) {
		return -1
	}
	if len(left.prerelease) > len(right.prerelease) {
		return 1
	}
	return 0
}
