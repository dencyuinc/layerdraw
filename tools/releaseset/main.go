// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Command releaseset builds and verifies the fixed portable compiler release set.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
)

const manifestName = "layerdraw-release-manifest.json"

type releaseManifest struct {
	ManifestVersion           int                `json:"manifest_version"`
	ReleaseVersion            string             `json:"release_version"`
	SourceRevision            string             `json:"source_revision"`
	BuiltAt                   string             `json:"built_at"`
	Artifacts                 []releaseArtifact  `json:"artifacts"`
	Protocols                 []releaseProtocol  `json:"protocols"`
	LDLGenerations            []int              `json:"ldl_generations"`
	ContainerVersions         []string           `json:"container_versions"`
	RendererProfiles          []releaseProfile   `json:"renderer_profiles"`
	ExporterProfiles          []releaseProfile   `json:"exporter_profiles"`
	SearchProfiles            []releaseProfile   `json:"search_profiles"`
	EmbeddingProfiles         []releaseProfile   `json:"embedding_profiles"`
	RequiredLadybugPrimitives []string           `json:"required_ladybug_primitives"`
	GeneratedPackages         []generatedPackage `json:"generated_packages"`
}

type releaseArtifact struct {
	ArtifactID string        `json:"artifact_id"`
	Path       string        `json:"path"`
	Platform   string        `json:"platform,omitempty"`
	MediaType  string        `json:"media_type"`
	Size       string        `json:"size"`
	Digest     string        `json:"digest"`
	Legal      artifactLegal `json:"legal"`
}

type artifactLegal struct {
	SPDX      legalFile `json:"spdx"`
	CycloneDX legalFile `json:"cyclonedx"`
	Notices   legalFile `json:"third_party_notices"`
}

type legalFile struct {
	Path   string `json:"path"`
	Size   string `json:"size"`
	Digest string `json:"digest"`
}

type releaseProtocol struct {
	Name           string                   `json:"name"`
	SupportedRange string                   `json:"supported_range"`
	Versions       []releaseProtocolVersion `json:"versions"`
}

type releaseProtocolVersion struct {
	Version      string `json:"version"`
	SchemaDigest string `json:"schema_digest"`
}

type releaseProfile struct {
	ProfileID           string `json:"profile_id"`
	ProfileVersion      string `json:"profile_version"`
	SpecificationDigest string `json:"specification_digest"`
	Status              string `json:"status"`
}

type generatedPackage struct {
	PackageName    string   `json:"package_name"`
	PackageVersion string   `json:"package_version"`
	SchemaDigests  []string `json:"schema_digests"`
}

type packageAuthority struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	License      string            `json:"license"`
	Dependencies map[string]string `json:"dependencies"`
	Scripts      map[string]string `json:"scripts"`
}

type cyclonedxDocument struct {
	Schema       string                `json:"$schema"`
	BOMFormat    string                `json:"bomFormat"`
	SpecVersion  string                `json:"specVersion"`
	Version      int                   `json:"version"`
	Metadata     cyclonedxMetadata     `json:"metadata"`
	Components   []cyclonedxComponent  `json:"components"`
	Dependencies []cyclonedxDependency `json:"dependencies"`
}

type cyclonedxMetadata struct {
	Timestamp string             `json:"timestamp"`
	Component cyclonedxComponent `json:"component"`
}

type cyclonedxComponent struct {
	Type     string             `json:"type"`
	BOMRef   string             `json:"bom-ref"`
	Name     string             `json:"name"`
	Version  string             `json:"version"`
	PURL     string             `json:"purl,omitempty"`
	Hashes   []cyclonedxHash    `json:"hashes,omitempty"`
	Licenses []cyclonedxLicense `json:"licenses"`
}

type cyclonedxHash struct {
	Algorithm string `json:"alg"`
	Content   string `json:"content"`
}

type cyclonedxLicense struct {
	License struct {
		ID string `json:"id"`
	} `json:"license"`
}

type cyclonedxDependency struct {
	Ref       string   `json:"ref"`
	DependsOn []string `json:"dependsOn"`
}

type spdxDocument struct {
	SPDXVersion       string             `json:"spdxVersion"`
	DataLicense       string             `json:"dataLicense"`
	SPDXID            string             `json:"SPDXID"`
	Name              string             `json:"name"`
	DocumentNamespace string             `json:"documentNamespace"`
	CreationInfo      spdxCreationInfo   `json:"creationInfo"`
	Packages          []spdxPackage      `json:"packages"`
	Relationships     []spdxRelationship `json:"relationships"`
}

type spdxCreationInfo struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

type spdxPackage struct {
	Name             string         `json:"name"`
	SPDXID           string         `json:"SPDXID"`
	VersionInfo      string         `json:"versionInfo"`
	DownloadLocation string         `json:"downloadLocation"`
	FilesAnalyzed    bool           `json:"filesAnalyzed"`
	LicenseConcluded string         `json:"licenseConcluded"`
	LicenseDeclared  string         `json:"licenseDeclared"`
	CopyrightText    string         `json:"copyrightText"`
	Checksums        []spdxChecksum `json:"checksums,omitempty"`
}

type spdxChecksum struct {
	Algorithm     string `json:"algorithm"`
	ChecksumValue string `json:"checksumValue"`
}

type spdxRelationship struct {
	SPDXElementID      string `json:"spdxElementId"`
	RelationshipType   string `json:"relationshipType"`
	RelatedSPDXElement string `json:"relatedSpdxElement"`
}

type artifactInput struct {
	id, path, platform, mediaType string
	packageName                   string
	nativeSBOM                    string
	nativeNotices                 string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "releaseset:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("expected subcommand: build or verify")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	output := flags.String("output", "dist/release-set", "release-set directory")
	version := flags.String("version", "", "fixed release version")
	revision := flags.String("source-revision", "", "source revision")
	builtAt := flags.String("built-at", "", "reproducible RFC3339 build time")
	nativeSBOM := flags.String("native-sbom", "", "native CycloneDX source")
	nativeNotices := flags.String("native-notices", "", "native third-party notices")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	switch args[0] {
	case "build":
		if *version == "" || *revision == "" || *builtAt == "" || *nativeSBOM == "" || *nativeNotices == "" {
			return errors.New("build requires -version, -source-revision, -built-at, -native-sbom, and -native-notices")
		}
		return build(*root, *output, *version, *revision, *builtAt, *nativeSBOM, *nativeNotices)
	case "verify":
		return verify(*root, *output)
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func build(root, output, version, revision, builtAt, nativeSBOM, nativeNotices string) error {
	if err := validateIdentity(version, revision, builtAt); err != nil {
		return err
	}
	inputs := fixedArtifacts(output, version, nativeSBOM, nativeNotices)
	if err := os.MkdirAll(filepath.Join(output, "legal"), 0o755); err != nil {
		return err
	}
	artifacts := make([]releaseArtifact, 0, len(inputs))
	packages := map[string]packageAuthority{}
	for _, input := range inputs {
		if input.packageName != "" {
			authority, err := readPackageAuthority(input.path)
			if err != nil {
				return fmt.Errorf("%s package authority: %w", input.id, err)
			}
			if authority.Name != input.packageName || authority.Version != version {
				return fmt.Errorf("%s package identity mismatch", input.id)
			}
			if authority.Scripts["postinstall"] != "" {
				return fmt.Errorf("%s contains a postinstall script", input.id)
			}
			packages[input.id] = authority
		}
		artifact, err := buildArtifact(output, input, version, builtAt, packages[input.id])
		if err != nil {
			return err
		}
		artifacts = append(artifacts, artifact)
	}
	if err := validatePackageClosure(packages, version); err != nil {
		return err
	}
	manifest := releaseManifest{
		ManifestVersion: 1, ReleaseVersion: version, SourceRevision: revision, BuiltAt: builtAt,
		Artifacts:      artifacts,
		Protocols:      []releaseProtocol{{Name: "engine", SupportedRange: "1.0..1.0", Versions: []releaseProtocolVersion{{Version: "1.0", SchemaDigest: engineprotocol.SchemaDigest}}}},
		LDLGenerations: []int{1}, ContainerVersions: []string{}, RendererProfiles: []releaseProfile{}, ExporterProfiles: []releaseProfile{},
		SearchProfiles: []releaseProfile{}, EmbeddingProfiles: []releaseProfile{}, RequiredLadybugPrimitives: []string{},
		GeneratedPackages: []generatedPackage{{PackageName: "@layerdraw/protocol", PackageVersion: version, SchemaDigests: []string{protocolcommon.SchemaDigest, semantic.SchemaDigest, engineprotocol.SchemaDigest}}},
	}
	data, err := canonicalJSON(manifest)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(output, manifestName), data, 0o644); err != nil {
		return err
	}
	return verify(root, output)
}

func fixedArtifacts(output, version, nativeSBOM, nativeNotices string) []artifactInput {
	return []artifactInput{
		{id: "layerdraw-engine", path: filepath.Join(output, "layerdraw-engine"), platform: runtime.GOOS + "/" + runtime.GOARCH, mediaType: "application/vnd.layerdraw.engine", nativeSBOM: nativeSBOM, nativeNotices: nativeNotices},
		{id: "@layerdraw/protocol", path: filepath.Join(output, "artifacts", "layerdraw-protocol-"+version+".tgz"), mediaType: "application/vnd.npm.package", packageName: "@layerdraw/protocol"},
		{id: "@layerdraw/engine-wasm", path: filepath.Join(output, "artifacts", "layerdraw-engine-wasm-"+version+".tgz"), platform: "js/wasm", mediaType: "application/vnd.npm.package", packageName: "@layerdraw/engine-wasm"},
		{id: "@layerdraw/engine-client", path: filepath.Join(output, "artifacts", "layerdraw-engine-client-"+version+".tgz"), mediaType: "application/vnd.npm.package", packageName: "@layerdraw/engine-client"},
	}
}

func buildArtifact(output string, input artifactInput, version, builtAt string, authority packageAuthority) (releaseArtifact, error) {
	size, digest, err := fileIdentity(input.path)
	if err != nil {
		return releaseArtifact{}, fmt.Errorf("%s: %w", input.id, err)
	}
	var sbom []byte
	if input.nativeSBOM != "" {
		sbom, err = os.ReadFile(input.nativeSBOM)
	} else if input.id == "@layerdraw/engine-wasm" {
		sbom, err = readTarFile(input.path, "package/dist/engine-wasm.cdx.json")
	} else {
		sbom, err = renderPackageCycloneDX(input.id, version, builtAt, digest, authority)
	}
	if err != nil {
		return releaseArtifact{}, fmt.Errorf("%s SBOM: %w", input.id, err)
	}
	if err := validateCycloneDX(sbom, input.id, version); err != nil {
		return releaseArtifact{}, err
	}
	legalID := strings.NewReplacer("@", "", "/", "-").Replace(input.id)
	sbomPath := filepath.Join(output, "legal", legalID+".cdx.json")
	if err := os.WriteFile(sbomPath, sbom, 0o644); err != nil {
		return releaseArtifact{}, err
	}
	spdx, err := renderSPDX(input.id, version, builtAt, digest, authority)
	if err != nil {
		return releaseArtifact{}, err
	}
	spdxPath := filepath.Join(output, "legal", legalID+".spdx.json")
	if err := os.WriteFile(spdxPath, spdx, 0o644); err != nil {
		return releaseArtifact{}, err
	}
	sbomLegal, err := legalIdentity(output, sbomPath)
	if err != nil {
		return releaseArtifact{}, err
	}
	spdxLegal, err := legalIdentity(output, spdxPath)
	if err != nil {
		return releaseArtifact{}, err
	}
	relative, err := filepath.Rel(output, input.path)
	if err != nil || strings.HasPrefix(relative, "..") {
		return releaseArtifact{}, fmt.Errorf("artifact %s is outside release set", input.id)
	}
	var notices []byte
	if input.nativeNotices != "" {
		notices, err = os.ReadFile(input.nativeNotices)
	} else if input.id == "@layerdraw/engine-wasm" {
		notices, err = readTarFile(input.path, "package/THIRD_PARTY_NOTICES.txt")
	} else {
		notices = []byte("This artifact has no third-party runtime dependencies.\n")
	}
	if err != nil || len(bytes.TrimSpace(notices)) == 0 {
		return releaseArtifact{}, fmt.Errorf("%s third-party notices are unavailable", input.id)
	}
	noticesPath := filepath.Join(output, "legal", legalID+".THIRD_PARTY_NOTICES.txt")
	if err := os.WriteFile(noticesPath, notices, 0o644); err != nil {
		return releaseArtifact{}, err
	}
	noticesLegal, err := legalIdentity(output, noticesPath)
	if err != nil {
		return releaseArtifact{}, err
	}
	return releaseArtifact{
		ArtifactID: input.id, Path: filepath.ToSlash(relative), Platform: input.platform, MediaType: input.mediaType,
		Size: fmt.Sprintf("%d", size), Digest: digest, Legal: artifactLegal{SPDX: spdxLegal, CycloneDX: sbomLegal, Notices: noticesLegal},
	}, nil
}

func validatePackageClosure(packages map[string]packageAuthority, version string) error {
	for _, id := range []string{"@layerdraw/protocol", "@layerdraw/engine-wasm"} {
		if len(packages[id].Dependencies) != 0 {
			return fmt.Errorf("%s runtime dependency closure is not empty", id)
		}
	}
	client := packages["@layerdraw/engine-client"]
	if len(client.Dependencies) != 1 || client.Dependencies["@layerdraw/protocol"] != version {
		return fmt.Errorf("engine-client runtime dependency closure is not fixed to protocol %s", version)
	}
	return nil
}

func verify(root, output string) error {
	data, err := os.ReadFile(filepath.Join(output, manifestName))
	if err != nil {
		return err
	}
	var manifest releaseManifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return fmt.Errorf("decode release manifest: %w", err)
	}
	if err := validateIdentity(manifest.ReleaseVersion, manifest.SourceRevision, manifest.BuiltAt); err != nil {
		return err
	}
	if manifest.ManifestVersion != 1 || len(manifest.Artifacts) != 4 || len(manifest.Protocols) != 1 || len(manifest.GeneratedPackages) != 1 || !slices.Equal(manifest.LDLGenerations, []int{1}) {
		return errors.New("release manifest shape is incomplete")
	}
	if manifest.Protocols[0].Name != "engine" || manifest.Protocols[0].SupportedRange != "1.0..1.0" || len(manifest.Protocols[0].Versions) != 1 || manifest.Protocols[0].Versions[0].SchemaDigest != engineprotocol.SchemaDigest {
		return errors.New("release manifest protocol binding mismatch")
	}
	generated := manifest.GeneratedPackages[0]
	if generated.PackageName != "@layerdraw/protocol" || generated.PackageVersion != manifest.ReleaseVersion || !slices.Equal(generated.SchemaDigests, []string{protocolcommon.SchemaDigest, semantic.SchemaDigest, engineprotocol.SchemaDigest}) {
		return errors.New("release manifest generated package binding mismatch")
	}
	wantIDs := []string{"@layerdraw/engine-client", "@layerdraw/engine-wasm", "@layerdraw/protocol", "layerdraw-engine"}
	gotIDs := make([]string, 0, len(manifest.Artifacts))
	packages := map[string]packageAuthority{}
	wantArtifacts := map[string]artifactInput{}
	for _, input := range fixedArtifacts(output, manifest.ReleaseVersion, "", "") {
		wantArtifacts[input.id] = input
	}
	for _, artifact := range manifest.Artifacts {
		gotIDs = append(gotIDs, artifact.ArtifactID)
		want, exists := wantArtifacts[artifact.ArtifactID]
		if !exists || artifact.MediaType != want.mediaType || artifact.Platform != want.platform {
			return fmt.Errorf("%s artifact metadata mismatch", artifact.ArtifactID)
		}
		if err := verifyRelativeFile(output, artifact.Path, artifact.Size, artifact.Digest); err != nil {
			return fmt.Errorf("%s: %w", artifact.ArtifactID, err)
		}
		if err := verifyRelativeFile(output, artifact.Legal.SPDX.Path, artifact.Legal.SPDX.Size, artifact.Legal.SPDX.Digest); err != nil {
			return fmt.Errorf("%s SPDX: %w", artifact.ArtifactID, err)
		}
		if err := verifyRelativeFile(output, artifact.Legal.CycloneDX.Path, artifact.Legal.CycloneDX.Size, artifact.Legal.CycloneDX.Digest); err != nil {
			return fmt.Errorf("%s CycloneDX: %w", artifact.ArtifactID, err)
		}
		if err := verifyRelativeFile(output, artifact.Legal.Notices.Path, artifact.Legal.Notices.Size, artifact.Legal.Notices.Digest); err != nil {
			return fmt.Errorf("%s third-party notices: %w", artifact.ArtifactID, err)
		}
		spdx, err := os.ReadFile(filepath.Join(output, filepath.FromSlash(artifact.Legal.SPDX.Path)))
		if err != nil {
			return err
		}
		if err := validateSPDX(spdx, artifact.ArtifactID, manifest.ReleaseVersion); err != nil {
			return err
		}
		cyclonedx, err := os.ReadFile(filepath.Join(output, filepath.FromSlash(artifact.Legal.CycloneDX.Path)))
		if err != nil {
			return err
		}
		if err := validateCycloneDX(cyclonedx, artifact.ArtifactID, manifest.ReleaseVersion); err != nil {
			return err
		}
		if want.packageName != "" {
			authority, err := readPackageAuthority(filepath.Join(output, filepath.FromSlash(artifact.Path)))
			if err != nil || authority.Name != want.packageName || authority.Version != manifest.ReleaseVersion || authority.Scripts["postinstall"] != "" {
				return fmt.Errorf("%s packaged authority mismatch", artifact.ArtifactID)
			}
			packages[artifact.ArtifactID] = authority
		} else {
			versionOutput, err := exec.Command(filepath.Join(output, filepath.FromSlash(artifact.Path)), "--version").CombinedOutput()
			if err != nil || !strings.HasPrefix(string(versionOutput), "layerdraw-engine "+manifest.ReleaseVersion+" (") {
				return errors.New("native artifact release identity mismatch")
			}
		}
	}
	sort.Strings(gotIDs)
	if !slices.Equal(gotIDs, wantIDs) {
		return fmt.Errorf("release artifact set mismatch: %v", gotIDs)
	}
	if err := validatePackageClosure(packages, manifest.ReleaseVersion); err != nil {
		return err
	}
	_ = root
	return nil
}

func validateIdentity(version, revision, builtAt string) error {
	if _, err := protocolcommon.EncodeReleaseVersion(protocolcommon.ReleaseVersion(version)); err != nil {
		return fmt.Errorf("invalid release version %q", version)
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(revision) {
		return errors.New("source revision must be 40 lowercase hexadecimal characters")
	}
	parsed, err := time.Parse(time.RFC3339, builtAt)
	if err != nil || parsed.Format(time.RFC3339) != builtAt || !strings.HasSuffix(builtAt, "Z") {
		return errors.New("built-at must be canonical UTC RFC3339 seconds")
	}
	return nil
}

func readPackageAuthority(archive string) (packageAuthority, error) {
	data, err := readTarFile(archive, "package/package.json")
	if err != nil {
		return packageAuthority{}, err
	}
	var authority packageAuthority
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	if err := decoder.Decode(&authority); err != nil {
		return packageAuthority{}, err
	}
	if authority.Dependencies == nil {
		authority.Dependencies = map[string]string{}
	}
	if authority.Scripts == nil {
		authority.Scripts = map[string]string{}
	}
	if _, err := readTarFile(archive, "package/LICENSE"); err != nil {
		return packageAuthority{}, errors.New("package LICENSE is missing")
	}
	return authority, nil
}

func readTarFile(path, name string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s is missing", name)
		}
		if err != nil {
			return nil, err
		}
		if header.Name != name {
			continue
		}
		return io.ReadAll(io.LimitReader(reader, 64<<20))
	}
}

func renderPackageCycloneDX(id, version, builtAt, digest string, authority packageAuthority) ([]byte, error) {
	rootRef := npmPURL(id, version)
	root := cyclonedxComponent{Type: "library", BOMRef: rootRef, Name: id, Version: version, PURL: rootRef, Hashes: []cyclonedxHash{{Algorithm: "SHA-256", Content: strings.TrimPrefix(digest, "sha256:")}}, Licenses: licenses(authority.License)}
	components := []cyclonedxComponent{}
	dependencies := []cyclonedxDependency{{Ref: rootRef, DependsOn: []string{}}}
	for name, dependencyVersion := range authority.Dependencies {
		ref := npmPURL(name, dependencyVersion)
		components = append(components, cyclonedxComponent{Type: "library", BOMRef: ref, Name: name, Version: dependencyVersion, PURL: ref, Licenses: licenses("Apache-2.0")})
		dependencies[0].DependsOn = append(dependencies[0].DependsOn, ref)
		dependencies = append(dependencies, cyclonedxDependency{Ref: ref, DependsOn: []string{}})
	}
	sort.Slice(components, func(i, j int) bool { return components[i].BOMRef < components[j].BOMRef })
	sort.Strings(dependencies[0].DependsOn)
	document := cyclonedxDocument{Schema: "http://cyclonedx.org/schema/bom-1.6.schema.json", BOMFormat: "CycloneDX", SpecVersion: "1.6", Version: 1, Metadata: cyclonedxMetadata{Timestamp: builtAt, Component: root}, Components: components, Dependencies: dependencies}
	return canonicalJSON(document)
}

func renderSPDX(id, version, builtAt, digest string, authority packageAuthority) ([]byte, error) {
	license := authority.License
	if license == "" || license == "SEE LICENSE IN LICENSE" {
		license = "LicenseRef-LayerDraw-1.0"
	}
	rootID := "SPDXRef-Package-" + sanitizeID(id)
	packages := []spdxPackage{{Name: id, SPDXID: rootID, VersionInfo: version, DownloadLocation: "NOASSERTION", FilesAnalyzed: false, LicenseConcluded: license, LicenseDeclared: license, CopyrightText: "NOASSERTION", Checksums: []spdxChecksum{{Algorithm: "SHA256", ChecksumValue: strings.TrimPrefix(digest, "sha256:")}}}}
	relationships := []spdxRelationship{{SPDXElementID: "SPDXRef-DOCUMENT", RelationshipType: "DESCRIBES", RelatedSPDXElement: rootID}}
	dependencies := make([]string, 0, len(authority.Dependencies))
	for name := range authority.Dependencies {
		dependencies = append(dependencies, name)
	}
	sort.Strings(dependencies)
	for _, name := range dependencies {
		dependencyID := "SPDXRef-Package-" + sanitizeID(name)
		packages = append(packages, spdxPackage{Name: name, SPDXID: dependencyID, VersionInfo: authority.Dependencies[name], DownloadLocation: "NOASSERTION", FilesAnalyzed: false, LicenseConcluded: "Apache-2.0", LicenseDeclared: "Apache-2.0", CopyrightText: "NOASSERTION"})
		relationships = append(relationships, spdxRelationship{SPDXElementID: rootID, RelationshipType: "DEPENDS_ON", RelatedSPDXElement: dependencyID})
	}
	document := spdxDocument{SPDXVersion: "SPDX-2.3", DataLicense: "CC0-1.0", SPDXID: "SPDXRef-DOCUMENT", Name: id + "-" + version, DocumentNamespace: "https://spdx.layerdraw.dev/releases/" + version + "/" + sanitizeID(id) + "/" + strings.TrimPrefix(digest, "sha256:"), CreationInfo: spdxCreationInfo{Created: builtAt, Creators: []string{"Tool: LayerDraw releaseset"}}, Packages: packages, Relationships: relationships}
	return canonicalJSON(document)
}

func validateCycloneDX(data []byte, id, version string) error {
	var value struct {
		BOMFormat   string `json:"bomFormat"`
		SpecVersion string `json:"specVersion"`
		Metadata    struct {
			Component struct{ Name, Version string } `json:"component"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("%s CycloneDX is invalid: %w", id, err)
	}
	if value.BOMFormat != "CycloneDX" || value.SpecVersion != "1.6" || value.Metadata.Component.Name != id || value.Metadata.Component.Version != version {
		return fmt.Errorf("%s CycloneDX authority mismatch", id)
	}
	return nil
}

func validateSPDX(data []byte, id, version string) error {
	var value struct {
		SPDXVersion string `json:"spdxVersion"`
		DataLicense string `json:"dataLicense"`
		SPDXID      string `json:"SPDXID"`
		Packages    []struct {
			Name        string `json:"name"`
			VersionInfo string `json:"versionInfo"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("%s SPDX is invalid: %w", id, err)
	}
	if value.SPDXVersion != "SPDX-2.3" || value.DataLicense != "CC0-1.0" || value.SPDXID != "SPDXRef-DOCUMENT" || len(value.Packages) == 0 || value.Packages[0].Name != id || value.Packages[0].VersionInfo != version {
		return fmt.Errorf("%s SPDX authority mismatch", id)
	}
	return nil
}

func licenses(expression string) []cyclonedxLicense {
	if expression == "" || expression == "SEE LICENSE IN LICENSE" {
		expression = "LicenseRef-LayerDraw-1.0"
	}
	value := cyclonedxLicense{}
	value.License.ID = expression
	return []cyclonedxLicense{value}
}

func npmPURL(name, version string) string {
	return "pkg:npm/" + strings.ReplaceAll(name, "@", "%40") + "@" + version
}

func sanitizeID(value string) string {
	return regexp.MustCompile(`[^A-Za-z0-9.-]+`).ReplaceAllString(value, "-")
}

func legalIdentity(root, path string) (legalFile, error) {
	size, digest, err := fileIdentity(path)
	if err != nil {
		return legalFile{}, err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(relative, "..") {
		return legalFile{}, errors.New("legal file is outside release set")
	}
	return legalFile{Path: filepath.ToSlash(relative), Size: fmt.Sprintf("%d", size), Digest: digest}, nil
}

func verifyRelativeFile(root, relative, size, digest string) error {
	if relative == "" || filepath.IsAbs(relative) || strings.Contains(filepath.ToSlash(relative), "../") {
		return errors.New("non-canonical release path")
	}
	actualSize, actualDigest, err := fileIdentity(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		return err
	}
	if fmt.Sprintf("%d", actualSize) != size || actualDigest != digest {
		return errors.New("release file size or digest mismatch")
	}
	return nil
}

func fileIdentity(path string) (int64, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, "", err
	}
	digest := sha256.Sum256(data)
	return int64(len(data)), "sha256:" + hex.EncodeToString(digest[:]), nil
}

func canonicalJSON(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
