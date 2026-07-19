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
		ID   string `json:"id,omitempty"`
		Name string `json:"name,omitempty"`
	} `json:"license"`
}

type cyclonedxDependency struct {
	Ref       string   `json:"ref"`
	DependsOn []string `json:"dependsOn"`
}

type cyclonedxAuthority struct {
	Root         cyclonedxComponent
	Components   map[string]cyclonedxComponent
	Dependencies map[string][]string
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

type desktopNativeAuthority struct {
	LadybugVersion  string `json:"ladybug_version"`
	Platform        string `json:"platform"`
	FTSExtension    string `json:"fts_extension"`
	FTSSHA256       string `json:"fts_sha256"`
	VectorExtension string `json:"vector_extension"`
	VectorSHA256    string `json:"vector_sha256"`
	AlgoExtension   string `json:"algo_extension"`
	AlgoSHA256      string `json:"algo_sha256"`
	Host            string `json:"host"`
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
	packages := map[string]packageAuthority{}
	for _, input := range inputs {
		if input.packageName == "" {
			continue
		}
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
	if err := validatePackageClosure(packages, version); err != nil {
		return err
	}
	artifacts := make([]releaseArtifact, 0, len(inputs))
	for _, input := range inputs {
		artifact, err := buildArtifact(output, input, version, builtAt, packages[input.id])
		if err != nil {
			return err
		}
		artifacts = append(artifacts, artifact)
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
		{id: "layerdraw-host-native", path: filepath.Join(output, "artifacts", "layerdraw-host-native-"+version+".tar.gz"), platform: runtime.GOOS + "/" + runtime.GOARCH, mediaType: "application/vnd.layerdraw.desktop-native", nativeSBOM: filepath.Join(output, "desktop-native-legal", "layerdraw-host-native.cdx.json"), nativeNotices: filepath.Join(output, "desktop-native-legal", "THIRD_PARTY_NOTICES.txt")},
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
	closure, err := validateCycloneDX(sbom, input.id, version)
	if err != nil {
		return releaseArtifact{}, err
	}
	if input.packageName != "" {
		if err := validatePackageSBOM(authority, closure); err != nil {
			return releaseArtifact{}, fmt.Errorf("%s SBOM: %w", input.id, err)
		}
	}
	legalID := strings.NewReplacer("@", "", "/", "-").Replace(input.id)
	sbomPath := filepath.Join(output, "legal", legalID+".cdx.json")
	if err := os.WriteFile(sbomPath, sbom, 0o644); err != nil {
		return releaseArtifact{}, err
	}
	spdx, err := renderSPDX(input.id, version, builtAt, digest, closure)
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
	if err := validateNotices(notices, closure); err != nil {
		return releaseArtifact{}, fmt.Errorf("%s third-party notices: %w", input.id, err)
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
	if manifest.ManifestVersion != 1 || len(manifest.Artifacts) != 5 || len(manifest.Protocols) != 1 || len(manifest.GeneratedPackages) != 1 || !slices.Equal(manifest.LDLGenerations, []int{1}) {
		return errors.New("release manifest shape is incomplete")
	}
	if manifest.Protocols[0].Name != "engine" || manifest.Protocols[0].SupportedRange != "1.0..1.0" || len(manifest.Protocols[0].Versions) != 1 || manifest.Protocols[0].Versions[0].SchemaDigest != engineprotocol.SchemaDigest {
		return errors.New("release manifest protocol binding mismatch")
	}
	generated := manifest.GeneratedPackages[0]
	if generated.PackageName != "@layerdraw/protocol" || generated.PackageVersion != manifest.ReleaseVersion || !slices.Equal(generated.SchemaDigests, []string{protocolcommon.SchemaDigest, semantic.SchemaDigest, engineprotocol.SchemaDigest}) {
		return errors.New("release manifest generated package binding mismatch")
	}
	wantIDs := []string{"@layerdraw/engine-client", "@layerdraw/engine-wasm", "@layerdraw/protocol", "layerdraw-engine", "layerdraw-host-native"}
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
		cyclonedx, err := os.ReadFile(filepath.Join(output, filepath.FromSlash(artifact.Legal.CycloneDX.Path)))
		if err != nil {
			return err
		}
		closure, err := validateCycloneDX(cyclonedx, artifact.ArtifactID, manifest.ReleaseVersion)
		if err != nil {
			return err
		}
		spdx, err := os.ReadFile(filepath.Join(output, filepath.FromSlash(artifact.Legal.SPDX.Path)))
		if err != nil {
			return err
		}
		if err := validateSPDX(spdx, artifact.ArtifactID, manifest.ReleaseVersion, artifact.Digest, closure); err != nil {
			return err
		}
		notices, err := os.ReadFile(filepath.Join(output, filepath.FromSlash(artifact.Legal.Notices.Path)))
		if err != nil {
			return err
		}
		if err := validateNotices(notices, closure); err != nil {
			return fmt.Errorf("%s third-party notices: %w", artifact.ArtifactID, err)
		}
		if want.packageName != "" {
			authority, err := readPackageAuthority(filepath.Join(output, filepath.FromSlash(artifact.Path)))
			if err != nil || authority.Name != want.packageName || authority.Version != manifest.ReleaseVersion || authority.Scripts["postinstall"] != "" {
				return fmt.Errorf("%s packaged authority mismatch", artifact.ArtifactID)
			}
			if err := validatePackageSBOM(authority, closure); err != nil {
				return fmt.Errorf("%s SBOM: %w", artifact.ArtifactID, err)
			}
			packages[artifact.ArtifactID] = authority
		} else if artifact.ArtifactID == "layerdraw-host-native" {
			archivePath := filepath.Join(output, filepath.FromSlash(artifact.Path))
			entries := make(map[string][]byte, 5)
			for _, name := range []string{"layerdraw-host-native", "libfts.lbug_extension", "libvector.lbug_extension", "libalgo.lbug_extension", "ladybug-native.json"} {
				data, err := readTarFile(archivePath, name)
				if err != nil || len(data) == 0 {
					return fmt.Errorf("Desktop native archive is missing %s", name)
				}
				entries[name] = data
			}
			var authority desktopNativeAuthority
			decoder := json.NewDecoder(bytes.NewReader(entries["ladybug-native.json"]))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&authority); err != nil {
				return fmt.Errorf("Desktop native authority is invalid: %w", err)
			}
			if err := decoder.Decode(&struct{}{}); err != io.EOF {
				return errors.New("Desktop native authority has trailing content")
			}
			ftsDigest := sha256.Sum256(entries["libfts.lbug_extension"])
			vectorDigest := sha256.Sum256(entries["libvector.lbug_extension"])
			algoDigest := sha256.Sum256(entries["libalgo.lbug_extension"])
			if authority.LadybugVersion != "0.17.0" || authority.Platform != runtime.GOOS+"/"+runtime.GOARCH || authority.FTSExtension != "libfts.lbug_extension" || authority.VectorExtension != "libvector.lbug_extension" || authority.AlgoExtension != "libalgo.lbug_extension" || authority.Host != "layerdraw-host-native" || authority.FTSSHA256 != hex.EncodeToString(ftsDigest[:]) || authority.VectorSHA256 != hex.EncodeToString(vectorDigest[:]) || authority.AlgoSHA256 != hex.EncodeToString(algoDigest[:]) {
				return errors.New("Desktop native authority mismatch")
			}
			if err := validateDesktopExtensionComponents(closure, authority); err != nil {
				return errors.New("Desktop extensions are not bound to their CycloneDX components")
			}
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

func validateDesktopExtensionComponents(closure cyclonedxAuthority, authority desktopNativeAuthority) error {
	for _, expected := range []struct{ purl, name, digest string }{
		{"pkg:generic/ladybugdb-fts-extension@0.17.0", "LadybugDB FTS extension", authority.FTSSHA256},
		{"pkg:generic/ladybugdb-vector-extension@0.17.0", "LadybugDB Vector extension", authority.VectorSHA256},
		{"pkg:generic/ladybugdb-algo-extension@0.17.0", "LadybugDB Algo extension", authority.AlgoSHA256},
	} {
		component, exists := closure.Components[expected.purl]
		if !exists || component.Name != expected.name || component.Version != authority.LadybugVersion || componentLicense(component) != "MIT" || len(component.Hashes) != 1 || component.Hashes[0].Algorithm != "SHA-256" || component.Hashes[0].Content != expected.digest {
			return errors.New("extension component authority mismatch")
		}
	}
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

func validatePackageSBOM(authority packageAuthority, closure cyclonedxAuthority) error {
	if closure.Root.Name != authority.Name || closure.Root.Version != authority.Version {
		return errors.New("package and CycloneDX root identities differ")
	}
	direct := make(map[string]bool, len(closure.Dependencies[closure.Root.BOMRef]))
	for _, ref := range closure.Dependencies[closure.Root.BOMRef] {
		direct[ref] = true
	}
	for name, version := range authority.Dependencies {
		matches := 0
		for ref, component := range closure.Components {
			if component.Name == name && component.Version == version && direct[ref] {
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("runtime dependency %s@%s is not a unique direct SBOM component", name, version)
		}
	}
	return nil
}

func renderSPDX(id, version, builtAt, digest string, closure cyclonedxAuthority) ([]byte, error) {
	packages, relationships := expectedSPDXContent(id, version, digest, closure)
	document := spdxDocument{
		SPDXVersion: "SPDX-2.3", DataLicense: "CC0-1.0", SPDXID: "SPDXRef-DOCUMENT", Name: id + "-" + version,
		DocumentNamespace: "https://spdx.layerdraw.dev/releases/" + version + "/" + sanitizeID(id) + "/" + strings.TrimPrefix(digest, "sha256:"),
		CreationInfo:      spdxCreationInfo{Created: builtAt, Creators: []string{"Tool: LayerDraw releaseset"}}, Packages: packages, Relationships: relationships,
	}
	return canonicalJSON(document)
}

func expectedSPDXContent(id, version, digest string, closure cyclonedxAuthority) ([]spdxPackage, []spdxRelationship) {
	rootID := "SPDXRef-Package-" + sanitizeID(id)
	refIDs := map[string]string{closure.Root.BOMRef: rootID}
	packages := []spdxPackage{spdxPackageForComponent(closure.Root, rootID, digest)}
	refs := make([]string, 0, len(closure.Components))
	for ref := range closure.Components {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	for _, ref := range refs {
		component := closure.Components[ref]
		componentID := spdxIDForComponent(component)
		refIDs[ref] = componentID
		packages = append(packages, spdxPackageForComponent(component, componentID, ""))
	}
	relationships := []spdxRelationship{{SPDXElementID: "SPDXRef-DOCUMENT", RelationshipType: "DESCRIBES", RelatedSPDXElement: rootID}}
	sources := make([]string, 0, len(closure.Dependencies))
	for ref := range closure.Dependencies {
		sources = append(sources, ref)
	}
	sort.Strings(sources)
	for _, source := range sources {
		dependencies := slices.Clone(closure.Dependencies[source])
		sort.Strings(dependencies)
		for _, dependency := range dependencies {
			relationships = append(relationships, spdxRelationship{SPDXElementID: refIDs[source], RelationshipType: "DEPENDS_ON", RelatedSPDXElement: refIDs[dependency]})
		}
	}
	return packages, relationships
}

func spdxPackageForComponent(component cyclonedxComponent, id, digest string) spdxPackage {
	license := componentLicense(component)
	result := spdxPackage{Name: component.Name, SPDXID: id, VersionInfo: component.Version, DownloadLocation: "NOASSERTION", FilesAnalyzed: false, LicenseConcluded: license, LicenseDeclared: license, CopyrightText: "NOASSERTION"}
	if digest != "" {
		result.Checksums = []spdxChecksum{{Algorithm: "SHA256", ChecksumValue: strings.TrimPrefix(digest, "sha256:")}}
	}
	return result
}

func spdxIDForComponent(component cyclonedxComponent) string {
	digest := sha256.Sum256([]byte(component.BOMRef))
	return "SPDXRef-Package-" + sanitizeID(component.Name) + "-" + hex.EncodeToString(digest[:6])
}

func componentLicense(component cyclonedxComponent) string {
	for _, value := range component.Licenses {
		if value.License.ID != "" {
			return normalizeSPDXLicense(value.License.ID)
		}
	}
	for _, value := range component.Licenses {
		if value.License.Name != "" {
			return normalizeSPDXLicense(value.License.Name)
		}
	}
	return "NOASSERTION"
}

func normalizeSPDXLicense(value string) string {
	switch value {
	case "", "SEE LICENSE IN LICENSE":
		return "NOASSERTION"
	case "LayerDraw License 1.0":
		return "LicenseRef-LayerDraw-1.0"
	default:
		return value
	}
}

func validateCycloneDX(data []byte, id, version string) (cyclonedxAuthority, error) {
	var document cyclonedxDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return cyclonedxAuthority{}, fmt.Errorf("%s CycloneDX is invalid: %w", id, err)
	}
	root := document.Metadata.Component
	if document.Schema != "http://cyclonedx.org/schema/bom-1.6.schema.json" || document.BOMFormat != "CycloneDX" || document.SpecVersion != "1.6" || document.Version != 1 || root.Name != id || root.Version != version || !validCycloneDXComponent(root) {
		return cyclonedxAuthority{}, fmt.Errorf("%s CycloneDX authority mismatch", id)
	}
	components := make(map[string]cyclonedxComponent, len(document.Components))
	known := map[string]bool{root.BOMRef: true}
	for _, component := range document.Components {
		if !validCycloneDXComponent(component) || known[component.BOMRef] {
			return cyclonedxAuthority{}, fmt.Errorf("%s CycloneDX component identity is invalid or duplicated", id)
		}
		known[component.BOMRef] = true
		components[component.BOMRef] = component
	}
	dependencies := make(map[string][]string, len(document.Dependencies))
	for _, dependency := range document.Dependencies {
		if !known[dependency.Ref] {
			return cyclonedxAuthority{}, fmt.Errorf("%s CycloneDX dependency source is unknown", id)
		}
		if _, duplicate := dependencies[dependency.Ref]; duplicate {
			return cyclonedxAuthority{}, fmt.Errorf("%s CycloneDX dependency source is duplicated", id)
		}
		seen := make(map[string]bool, len(dependency.DependsOn))
		for _, target := range dependency.DependsOn {
			if !known[target] || target == dependency.Ref || seen[target] {
				return cyclonedxAuthority{}, fmt.Errorf("%s CycloneDX dependency edge is invalid", id)
			}
			seen[target] = true
		}
		dependencies[dependency.Ref] = slices.Clone(dependency.DependsOn)
	}
	if len(dependencies) != len(known) {
		return cyclonedxAuthority{}, fmt.Errorf("%s CycloneDX dependency graph is incomplete", id)
	}
	reachable := map[string]bool{}
	stack := []string{root.BOMRef}
	for len(stack) > 0 {
		ref := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if reachable[ref] {
			continue
		}
		reachable[ref] = true
		stack = append(stack, dependencies[ref]...)
	}
	if len(reachable) != len(known) {
		return cyclonedxAuthority{}, fmt.Errorf("%s CycloneDX contains unreachable runtime components", id)
	}
	return cyclonedxAuthority{Root: root, Components: components, Dependencies: dependencies}, nil
}

func validCycloneDXComponent(component cyclonedxComponent) bool {
	return component.Type != "" && component.BOMRef != "" && component.Name != "" && component.Version != "" && componentLicense(component) != "NOASSERTION"
}

func validateSPDX(data []byte, id, version, digest string, closure cyclonedxAuthority) error {
	var document spdxDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("%s SPDX is invalid: %w", id, err)
	}
	expectedNamespace := "https://spdx.layerdraw.dev/releases/" + version + "/" + sanitizeID(id) + "/" + strings.TrimPrefix(digest, "sha256:")
	if document.SPDXVersion != "SPDX-2.3" || document.DataLicense != "CC0-1.0" || document.SPDXID != "SPDXRef-DOCUMENT" || document.Name != id+"-"+version || document.DocumentNamespace != expectedNamespace {
		return fmt.Errorf("%s SPDX authority mismatch", id)
	}
	expectedPackages, expectedRelationships := expectedSPDXContent(id, version, digest, closure)
	expectedByID := make(map[string]spdxPackage, len(expectedPackages))
	for _, value := range expectedPackages {
		expectedByID[value.SPDXID] = value
	}
	actualByID := make(map[string]spdxPackage, len(document.Packages))
	for _, value := range document.Packages {
		if value.SPDXID == "" || actualByID[value.SPDXID].SPDXID != "" {
			return fmt.Errorf("%s SPDX package identity is invalid or duplicated", id)
		}
		actualByID[value.SPDXID] = value
	}
	if len(actualByID) != len(expectedByID) {
		return fmt.Errorf("%s SPDX runtime closure is incomplete", id)
	}
	for spdxID, expected := range expectedByID {
		actual, exists := actualByID[spdxID]
		if !exists || !equalSPDXPackage(actual, expected) {
			return fmt.Errorf("%s SPDX package authority mismatch", id)
		}
	}
	expectedEdges := make(map[spdxRelationship]bool, len(expectedRelationships))
	for _, relationship := range expectedRelationships {
		expectedEdges[relationship] = true
	}
	actualEdges := make(map[spdxRelationship]bool, len(document.Relationships))
	for _, relationship := range document.Relationships {
		if actualEdges[relationship] {
			return fmt.Errorf("%s SPDX relationship is duplicated", id)
		}
		actualEdges[relationship] = true
	}
	if len(actualEdges) != len(expectedEdges) {
		return fmt.Errorf("%s SPDX dependency graph is incomplete", id)
	}
	for relationship := range expectedEdges {
		if !actualEdges[relationship] {
			return fmt.Errorf("%s SPDX dependency graph mismatch", id)
		}
	}
	return nil
}

func equalSPDXPackage(left, right spdxPackage) bool {
	return left.Name == right.Name && left.SPDXID == right.SPDXID && left.VersionInfo == right.VersionInfo && left.DownloadLocation == right.DownloadLocation && left.FilesAnalyzed == right.FilesAnalyzed && left.LicenseConcluded == right.LicenseConcluded && left.LicenseDeclared == right.LicenseDeclared && left.CopyrightText == right.CopyrightText && slices.Equal(left.Checksums, right.Checksums)
}

func validateNotices(data []byte, closure cyclonedxAuthority) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return errors.New("notice file is empty")
	}
	for _, component := range closure.Components {
		if strings.HasPrefix(component.Name, "@layerdraw/") || component.Name == "layerdraw-engine" {
			continue
		}
		for _, required := range []string{component.Name, component.Version, componentLicense(component)} {
			if required != "NOASSERTION" && !bytes.Contains(data, []byte(required)) {
				return fmt.Errorf("runtime component notice is missing %q", required)
			}
		}
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
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative)))
	if relative == "" || filepath.IsAbs(relative) || clean != relative || clean == "." || strings.HasPrefix(clean, "../") {
		return errors.New("non-canonical release path")
	}
	path := filepath.Join(root, filepath.FromSlash(relative))
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("release file is not a regular file")
	}
	actualSize, actualDigest, err := fileIdentity(path)
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
