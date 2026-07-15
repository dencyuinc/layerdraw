// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Command wasmartifact finalizes and verifies the deterministic Go WASM
// distribution metadata, legal notice, and artifact-local CycloneDX SBOM.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	wasmtransport "github.com/dencyuinc/layerdraw/internal/transport/wasm"
)

const (
	expectedGoVersion      = "go1.26.5"
	expectedWasmExecSHA256 = "0c949f4996f9a89698e4b5c586de32249c3b69b7baadb64d220073cc04acba14"
	manifestName           = "engine-wasm.manifest.json"
	sbomName               = "engine-wasm.cdx.json"
	legacySBOMName         = "layerdraw-engine.wasm.cdx.json"
)

var requiredBrowserPrimitives = []string{
	"Blob",
	"TextDecoder",
	"TextEncoder",
	"URL.createObjectURL",
	"URL.revokeObjectURL",
	"WebAssembly",
	"crypto.getRandomValues",
	"crypto.subtle.digest",
	"dedicated_module_worker",
	"fetch",
	"performance.now",
	"structuredClone",
	"transferable_fixed_ArrayBuffer",
}

type artifactManifest struct {
	ArtifactID              string                  `json:"artifact_id"`
	ArtifactManifestVersion int                     `json:"artifact_manifest_version"`
	Build                   manifestBuild           `json:"build"`
	Protocol                manifestProtocol        `json:"protocol"`
	RuntimeSupport          manifestRuntimeSupport  `json:"runtime_support"`
	SBOMAuthority           manifestSBOMAuthority   `json:"sbom_authority"`
	Files                   []manifestFile          `json:"files"`
	Transport               manifestTransport       `json:"transport"`
	CompilerLimits          manifestCompilerLimits  `json:"compiler_limits"`
	BrowserContract         manifestBrowserContract `json:"browser_contract"`
	Licenses                manifestLicenses        `json:"licenses"`
}

type manifestBuild struct {
	CGOEnabled     bool     `json:"cgo_enabled"`
	GoVersion      string   `json:"go_version"`
	GoExperiment   string   `json:"goexperiment"`
	GOOSGOARCH     string   `json:"goos_goarch"`
	MainPackage    string   `json:"main_package"`
	SourceRevision string   `json:"source_revision"`
	ReleaseVersion string   `json:"release_version"`
	Flags          []string `json:"flags"`
}

type manifestProtocol struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	SchemaDigest string `json:"schema_digest"`
}

type manifestRuntimeSupport struct {
	File      string `json:"file"`
	GoVersion string `json:"go_version"`
	Digest    string `json:"digest"`
}

type manifestSBOMAuthority struct {
	Digest  string                        `json:"digest"`
	Runtime manifestSBOMRuntimeComponent  `json:"runtime"`
	Modules []manifestSBOMModuleComponent `json:"modules"`
}

type manifestSBOMRuntimeComponent struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Version string `json:"version"`
	PURL    string `json:"purl"`
	BOMRef  string `json:"bom_ref"`
	Scope   string `json:"scope"`
	Digest  string `json:"digest"`
	License string `json:"license"`
}

type manifestSBOMModuleComponent struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Version string `json:"version"`
	PURL    string `json:"purl"`
	BOMRef  string `json:"bom_ref"`
	Scope   string `json:"scope"`
	License string `json:"license"`
}

type manifestFile struct {
	Path      string `json:"path"`
	MediaType string `json:"media_type"`
	Size      int64  `json:"size"`
	Digest    string `json:"digest"`
}

type manifestTransport struct {
	ID                      string `json:"id"`
	WorkerProtocol          string `json:"worker_protocol"`
	WorkerProtocolVersion   int    `json:"worker_protocol_version"`
	ContractFile            string `json:"contract_file"`
	EndpointIDProvenance    string `json:"endpoint_instance_id_provenance"`
	ReleaseDigestProvenance string `json:"release_manifest_digest_provenance"`
	SingleFlight            bool   `json:"single_flight"`
	Transfer                string `json:"transfer"`
	MaxControlBytes         int64  `json:"max_control_bytes"`
	MaxControlDepth         int64  `json:"max_control_depth"`
	MaxBlobIDBytes          int64  `json:"max_blob_id_bytes"`
	MaxBuffers              int64  `json:"max_buffers"`
	MaxInputBlobBytes       int64  `json:"max_input_blob_bytes"`
	MaxInputTotalBytes      int64  `json:"max_input_total_bytes"`
	MaxOutputBlobBytes      int64  `json:"max_output_blob_bytes"`
	MaxOutputTotalBytes     int64  `json:"max_output_total_bytes"`
	MaxResponsePublishBytes int64  `json:"max_response_publish_bytes"`
}

type manifestCompilerLimits struct {
	MaxProjectSourceFiles int64 `json:"max_project_source_files"`
	MaxProjectSourceBytes int64 `json:"max_project_source_bytes"`
	MaxPackFiles          int64 `json:"max_pack_files"`
	MaxPackBytes          int64 `json:"max_pack_bytes"`
	MaxAssets             int64 `json:"max_assets"`
	MaxAssetBytes         int64 `json:"max_asset_bytes"`
	MaxRasterDimension    int64 `json:"max_raster_dimension"`
	MaxRasterPixels       int64 `json:"max_raster_pixels"`
	MaxDeclarations       int64 `json:"max_declarations"`
}

type manifestBrowserContract struct {
	ModuleDedicatedWorker bool     `json:"module_dedicated_worker"`
	SharedArrayBuffer     bool     `json:"shared_array_buffer"`
	WASMThreads           bool     `json:"wasm_threads"`
	RequiredPrimitives    []string `json:"required_primitives"`
}

type manifestLicenses struct {
	Product        string `json:"product"`
	RuntimeSupport string `json:"runtime_support"`
	SBOM           string `json:"sbom"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "wasmartifact:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("expected subcommand: validate-version, sbom-authority-digest, finalize, or verify")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	output := flags.String("output", "dist/engine-wasm", "artifact output directory")
	version := flags.String("version", "", "artifact release version")
	sourceRevision := flags.String("source-revision", "", "40-character lowercase source revision")
	goLicense := flags.String("go-license", "", "pinned Go distribution LICENSE")
	root := flags.String("root", ".", "repository root")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	switch args[0] {
	case "validate-version":
		if *version == "" {
			return errors.New("validate-version requires -version")
		}
		return validateReleaseVersion(*version)
	case "sbom-authority-digest":
		modules, err := linkedModules(*root)
		if err != nil {
			return err
		}
		fmt.Println(sbomAuthorityManifest(modules).Digest)
		return nil
	case "finalize":
		if *version == "" || *sourceRevision == "" || *goLicense == "" {
			return errors.New("finalize requires -version, -source-revision, and -go-license")
		}
		return finalize(*root, *output, *version, *sourceRevision, *goLicense)
	case "verify":
		if *version == "" {
			return errors.New("verify requires authoritative -version")
		}
		return verify(*root, *output, *version)
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func finalize(root, output, version, sourceRevision, goLicense string) error {
	if err := validateReleaseVersion(version); err != nil {
		return err
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(sourceRevision) {
		return fmt.Errorf("source revision must be 40 lowercase hexadecimal characters")
	}
	if err := verifyWasmExec(output); err != nil {
		return err
	}
	modules, err := createLegalAndSBOM(root, output, version, goLicense)
	if err != nil {
		return err
	}
	manifest, err := buildManifest(output, version, sourceRevision, modules)
	if err != nil {
		return err
	}
	data, err := canonicalJSON(manifest)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(output, manifestName), data, 0o644); err != nil {
		return err
	}
	return verify(root, output, version)
}

func validateReleaseVersion(version string) error {
	if _, err := protocolcommon.EncodeReleaseVersion(protocolcommon.ReleaseVersion(version)); err != nil {
		return fmt.Errorf("invalid release version %q", version)
	}
	return nil
}

func verify(root, output, expectedVersion string) error {
	if err := validateReleaseVersion(expectedVersion); err != nil {
		return err
	}
	if err := verifyWasmExec(output); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(output, manifestName))
	if err != nil {
		return err
	}
	var manifest artifactManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("decode artifact manifest: %w", err)
	}
	canonical, err := canonicalJSON(manifest)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, canonical) {
		return errors.New("artifact manifest is not canonical JSON")
	}
	modules, err := linkedModules(root)
	if err != nil {
		return err
	}
	if err := verifyManifestAuthority(manifest, expectedVersion, modules); err != nil {
		return err
	}
	seen := make(map[string]bool, len(manifest.Files))
	for _, file := range manifest.Files {
		if seen[file.Path] || !safeRelative(file.Path) {
			return fmt.Errorf("invalid or duplicate manifest file %q", file.Path)
		}
		seen[file.Path] = true
		path := filepath.Join(output, filepath.FromSlash(file.Path))
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		digest, err := fileDigest(path)
		if err != nil {
			return err
		}
		if info.Size() != file.Size || "sha256:"+digest != file.Digest || mediaType(file.Path) != file.MediaType {
			return fmt.Errorf("manifest file mismatch for %s", file.Path)
		}
	}
	for _, required := range []string{"layerdraw-engine.wasm", "wasm_exec.js", "engine-wasm-worker-v1.json", "LICENSE", "NOTICE", "LICENSING.md", "THIRD_PARTY_NOTICES.txt", sbomName} {
		if !seen[required] {
			return fmt.Errorf("manifest omits required file %s", required)
		}
	}
	actualFiles, err := artifactFiles(output)
	if err != nil {
		return err
	}
	if !slices.Equal(manifest.Files, actualFiles) {
		return errors.New("artifact manifest does not describe the exact output file set")
	}
	if err := verifySBOM(output, expectedVersion, modules); err != nil {
		return err
	}
	return nil
}

func verifyManifestAuthority(manifest artifactManifest, expectedVersion string, modules []bundledModule) error {
	if manifest.ArtifactID != "@layerdraw/engine-wasm" || manifest.ArtifactManifestVersion != 1 || manifest.Build.GoVersion != expectedGoVersion || manifest.Protocol.SchemaDigest != engineprotocol.SchemaDigest {
		return errors.New("artifact manifest authority mismatch")
	}
	if manifest.Build.ReleaseVersion != expectedVersion ||
		!regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(manifest.Build.SourceRevision) ||
		manifest.Build.CGOEnabled || manifest.Build.GoExperiment != "" || manifest.Build.GOOSGOARCH != "js/wasm" || manifest.Build.MainPackage != "./cmd/layerdraw-engine" {
		return errors.New("artifact manifest build identity mismatch")
	}
	expectedFlags := []string{
		"-trimpath",
		"-buildvcs=false",
		"-ldflags=-buildid= -s -w -X main.releaseVersion=" + manifest.Build.ReleaseVersion + " -X main.sourceRevision=" + manifest.Build.SourceRevision + " -X main.sbomAuthorityDigest=" + manifest.SBOMAuthority.Digest,
	}
	if !slices.Equal(manifest.Build.Flags, expectedFlags) ||
		manifest.Protocol.Name != string(engineprotocol.EngineProtocolRefNameValue) ||
		manifest.Protocol.Version != string(engineprotocol.EngineProtocolRefVersionValue) ||
		manifest.RuntimeSupport != (manifestRuntimeSupport{File: "wasm_exec.js", GoVersion: expectedGoVersion, Digest: "sha256:" + expectedWasmExecSHA256}) ||
		!reflectSBOMAuthority(manifest.SBOMAuthority, modules) {
		return errors.New("artifact manifest generated/build authority mismatch")
	}
	if manifest.Transport != transportManifest() || manifest.CompilerLimits != compilerLimitsManifest() {
		return errors.New("artifact manifest limit drift")
	}
	if !reflectBrowserContract(manifest.BrowserContract) {
		return errors.New("artifact manifest browser contract drift")
	}
	if manifest.Licenses != licenseManifest() {
		return errors.New("artifact manifest license authority drift")
	}
	return nil
}

func buildManifest(output, version, sourceRevision string, modules []bundledModule) (artifactManifest, error) {
	files, err := artifactFiles(output)
	if err != nil {
		return artifactManifest{}, err
	}
	return artifactManifest{
		ArtifactID:              "@layerdraw/engine-wasm",
		ArtifactManifestVersion: 1,
		Build: manifestBuild{
			CGOEnabled:     false,
			GoVersion:      expectedGoVersion,
			GoExperiment:   "",
			GOOSGOARCH:     "js/wasm",
			MainPackage:    "./cmd/layerdraw-engine",
			SourceRevision: sourceRevision,
			ReleaseVersion: version,
			Flags: []string{
				"-trimpath",
				"-buildvcs=false",
				"-ldflags=-buildid= -s -w -X main.releaseVersion=" + version + " -X main.sourceRevision=" + sourceRevision + " -X main.sbomAuthorityDigest=" + sbomAuthorityManifest(modules).Digest,
			},
		},
		Protocol: manifestProtocol{
			Name:         string(engineprotocol.EngineProtocolRefNameValue),
			Version:      string(engineprotocol.EngineProtocolRefVersionValue),
			SchemaDigest: engineprotocol.SchemaDigest,
		},
		RuntimeSupport: manifestRuntimeSupport{
			File:      "wasm_exec.js",
			GoVersion: expectedGoVersion,
			Digest:    "sha256:" + expectedWasmExecSHA256,
		},
		SBOMAuthority:   sbomAuthorityManifest(modules),
		Files:           files,
		Transport:       transportManifest(),
		CompilerLimits:  compilerLimitsManifest(),
		BrowserContract: browserContractManifest(),
		Licenses:        licenseManifest(),
	}, nil
}

func sbomAuthorityManifest(modules []bundledModule) manifestSBOMAuthority {
	runtimeRef := "pkg:generic/golang-wasm-runtime@" + expectedGoVersion
	authority := manifestSBOMAuthority{
		Runtime: manifestSBOMRuntimeComponent{
			Type: "framework", Name: "Go WebAssembly runtime support", Version: expectedGoVersion,
			PURL: runtimeRef, BOMRef: runtimeRef, Scope: "required",
			Digest: "sha256:" + expectedWasmExecSHA256, License: "BSD-3-Clause",
		},
		Modules: make([]manifestSBOMModuleComponent, 0, len(modules)),
	}
	for _, module := range modules {
		purl := "pkg:golang/" + module.Review.Module + "@" + module.Review.Version
		authority.Modules = append(authority.Modules, manifestSBOMModuleComponent{
			Type: "library", Name: module.Review.Module, Version: module.Review.Version,
			PURL: purl, BOMRef: purl, Scope: "required", License: module.Review.License,
		})
	}
	projection := struct {
		Runtime manifestSBOMRuntimeComponent  `json:"runtime"`
		Modules []manifestSBOMModuleComponent `json:"modules"`
	}{Runtime: authority.Runtime, Modules: authority.Modules}
	data, err := canonicalJSON(projection)
	if err != nil {
		panic(err)
	}
	digest := sha256.Sum256(data)
	authority.Digest = "sha256:" + hex.EncodeToString(digest[:])
	return authority
}

func reflectSBOMAuthority(value manifestSBOMAuthority, modules []bundledModule) bool {
	return value.Runtime == sbomAuthorityManifest(modules).Runtime && slices.Equal(value.Modules, sbomAuthorityManifest(modules).Modules)
}

func browserContractManifest() manifestBrowserContract {
	return manifestBrowserContract{
		ModuleDedicatedWorker: true,
		SharedArrayBuffer:     false,
		WASMThreads:           false,
		RequiredPrimitives:    slices.Clone(requiredBrowserPrimitives),
	}
}

func reflectBrowserContract(value manifestBrowserContract) bool {
	expected := browserContractManifest()
	return value.ModuleDedicatedWorker == expected.ModuleDedicatedWorker &&
		value.SharedArrayBuffer == expected.SharedArrayBuffer &&
		value.WASMThreads == expected.WASMThreads &&
		slices.Equal(value.RequiredPrimitives, expected.RequiredPrimitives)
}

func licenseManifest() manifestLicenses {
	return manifestLicenses{Product: "LicenseRef-LayerDraw-1.0", RuntimeSupport: "BSD-3-Clause", SBOM: sbomName}
}

func transportManifest() manifestTransport {
	limits := wasmtransport.BrowserTransportLimits()
	return manifestTransport{
		ID:                      wasmtransport.TransportID,
		WorkerProtocol:          wasmtransport.WorkerProtocol,
		WorkerProtocolVersion:   wasmtransport.WorkerProtocolVersion,
		ContractFile:            "engine-wasm-worker-v1.json",
		EndpointIDProvenance:    "runtime_crypto_rand",
		ReleaseDigestProvenance: "verified_worker_input",
		SingleFlight:            true,
		Transfer:                "array_buffer",
		MaxControlBytes:         limits.MaxControlBytes,
		MaxControlDepth:         limits.MaxControlDepth,
		MaxBlobIDBytes:          limits.MaxBlobIDBytes,
		MaxBuffers:              limits.MaxBuffers,
		MaxInputBlobBytes:       limits.MaxInputBlobBytes,
		MaxInputTotalBytes:      limits.MaxInputTotalBytes,
		MaxOutputBlobBytes:      limits.MaxOutputBlobBytes,
		MaxOutputTotalBytes:     limits.MaxOutputTotalBytes,
		MaxResponsePublishBytes: limits.MaxResponsePublishBytes,
	}
}

func compilerLimitsManifest() manifestCompilerLimits {
	limits := wasmtransport.BrowserCompilerLimitPolicy().HardMaximums
	return manifestCompilerLimits{
		MaxProjectSourceFiles: limits.MaxProjectSourceFiles,
		MaxProjectSourceBytes: limits.MaxProjectSourceBytes,
		MaxPackFiles:          limits.MaxPackFiles,
		MaxPackBytes:          limits.MaxPackBytes,
		MaxAssets:             limits.MaxAssets,
		MaxAssetBytes:         limits.MaxAssetBytes,
		MaxRasterDimension:    limits.MaxRasterDimension,
		MaxRasterPixels:       limits.MaxRasterPixels,
		MaxDeclarations:       limits.MaxDeclarations,
	}
}

func artifactFiles(output string) ([]manifestFile, error) {
	var paths []string
	err := filepath.WalkDir(output, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(output, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == manifestName || relative == legacySBOMName {
			return nil
		}
		paths = append(paths, relative)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	result := make([]manifestFile, len(paths))
	for index, relative := range paths {
		path := filepath.Join(output, filepath.FromSlash(relative))
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		digest, err := fileDigest(path)
		if err != nil {
			return nil, err
		}
		result[index] = manifestFile{Path: relative, MediaType: mediaType(relative), Size: info.Size(), Digest: "sha256:" + digest}
	}
	return result, nil
}

func mediaType(path string) string {
	switch {
	case strings.HasSuffix(path, ".wasm"):
		return "application/wasm"
	case strings.HasSuffix(path, ".js"):
		return "text/javascript"
	case strings.HasSuffix(path, ".json"):
		return "application/json"
	case strings.HasSuffix(path, ".md"):
		return "text/markdown; charset=utf-8"
	default:
		return "text/plain; charset=utf-8"
	}
}

type reviewedGoModule struct {
	Module        string `json:"module"`
	Version       string `json:"version"`
	License       string `json:"license"`
	LicenseFile   string `json:"license_file"`
	LicenseSHA256 string `json:"license_sha256"`
}

type artifactPolicy struct {
	GoModules []reviewedGoModule `json:"go_modules"`
}

type listedPackage struct {
	Module *struct {
		Path    string
		Version string
		Main    bool
		Dir     string
		Replace *struct {
			Path    string
			Version string
			Dir     string
		}
	}
}

type bundledModule struct {
	Review      reviewedGoModule
	LicenseText []byte
}

func createLegalAndSBOM(root, output, version, goLicense string) ([]bundledModule, error) {
	for source, destination := range map[string]string{
		"LICENSE":                            "LICENSE",
		"NOTICE":                             "NOTICE",
		"docs/legal/README.md":               "LICENSING.md",
		"docs/legal/licenses/Apache-2.0.txt": "licenses/Apache-2.0.txt",
	} {
		if err := copyFile(filepath.Join(root, filepath.FromSlash(source)), filepath.Join(output, filepath.FromSlash(destination))); err != nil {
			return nil, err
		}
	}
	modules, err := linkedModules(root)
	if err != nil {
		return nil, err
	}
	license, err := os.ReadFile(goLicense)
	if err != nil {
		return nil, fmt.Errorf("read Go distribution license: %w", err)
	}
	var result bytes.Buffer
	result.WriteString("Third-Party Notices for @layerdraw/engine-wasm\n\n")
	result.WriteString("This file lists third-party modules and runtime support distributed with this artifact.\n")
	for _, module := range modules {
		result.WriteString("\n================================================================================\n")
		fmt.Fprintf(&result, "%s %s\nLicense: %s\n", module.Review.Module, module.Review.Version, module.Review.License)
		result.WriteString("--------------------------------------------------------------------------------\n")
		result.Write(bytes.TrimSpace(module.LicenseText))
		result.WriteByte('\n')
	}
	result.WriteString("\n================================================================================\n")
	result.WriteString("Go WebAssembly runtime support go1.26.5\nLicense: BSD-3-Clause\n")
	result.WriteString("Files: layerdraw-engine.wasm, wasm_exec.js\n")
	result.WriteString("--------------------------------------------------------------------------------\n")
	result.Write(bytes.TrimSpace(license))
	result.WriteByte('\n')
	if err := os.WriteFile(filepath.Join(output, "THIRD_PARTY_NOTICES.txt"), result.Bytes(), 0o644); err != nil {
		return nil, err
	}
	if err := writeSBOM(output, version, modules); err != nil {
		return nil, err
	}
	return modules, nil
}

func linkedModules(root string) ([]bundledModule, error) {
	policyData, err := os.ReadFile(filepath.Join(root, "tools", "license-policy.json"))
	if err != nil {
		return nil, err
	}
	var policy artifactPolicy
	if err := json.Unmarshal(policyData, &policy); err != nil {
		return nil, err
	}
	reviews := make(map[string]reviewedGoModule, len(policy.GoModules))
	for _, review := range policy.GoModules {
		reviews[review.Module+"@"+review.Version] = review
	}
	command := exec.Command("go", "list", "-deps", "-json", "./cmd/layerdraw-engine")
	command.Dir = root
	command.Env = append(os.Environ(),
		"GOTOOLCHAIN=go1.26.5",
		"GOOS=js",
		"GOARCH=wasm",
		"CGO_ENABLED=0",
		"GOENV=off",
		"GOWORK=off",
		"GOEXPERIMENT=",
		"GOFLAGS=-mod=readonly",
	)
	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("list WASM dependencies: %w\n%s", err, exitErr.Stderr)
		}
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	seen := make(map[string]bool)
	var bundled []bundledModule
	for decoder.More() {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			return nil, err
		}
		if pkg.Module == nil || pkg.Module.Main {
			continue
		}
		path, version, directory := pkg.Module.Path, pkg.Module.Version, pkg.Module.Dir
		if pkg.Module.Replace != nil {
			path, version, directory = pkg.Module.Replace.Path, pkg.Module.Replace.Version, pkg.Module.Replace.Dir
		}
		key := path + "@" + version
		if seen[key] {
			continue
		}
		review, found := reviews[key]
		if !found {
			return nil, fmt.Errorf("linked Go module %s has no reviewed license entry", key)
		}
		licenseText, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(review.LicenseFile)))
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(licenseText)
		if hex.EncodeToString(digest[:]) != review.LicenseSHA256 {
			return nil, fmt.Errorf("linked Go module %s license digest changed", key)
		}
		bundled = append(bundled, bundledModule{Review: review, LicenseText: licenseText})
		seen[key] = true
	}
	sort.Slice(bundled, func(left, right int) bool {
		return bundled[left].Review.Module+"@"+bundled[left].Review.Version < bundled[right].Review.Module+"@"+bundled[right].Review.Version
	})
	return bundled, nil
}

func writeSBOM(output, version string, modules []bundledModule) error {
	wasmDigest, err := fileDigest(filepath.Join(output, "layerdraw-engine.wasm"))
	if err != nil {
		return err
	}
	document := sbomDocument(version, wasmDigest, modules)
	canonical, err := canonicalJSON(document)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(output, sbomName), canonical, 0o644)
}

func sbomDocument(version, wasmDigest string, modules []bundledModule) map[string]any {
	rootRef := "pkg:npm/%40layerdraw/engine-wasm@" + version
	runtimeRef := "pkg:generic/golang-wasm-runtime@" + expectedGoVersion
	rootComponent := map[string]any{
		"type": "application", "name": "@layerdraw/engine-wasm", "version": version,
		"purl": rootRef, "bom-ref": rootRef,
		"hashes":   []any{map[string]any{"alg": "SHA-256", "content": wasmDigest}},
		"licenses": []any{map[string]any{"license": map[string]any{"name": "LayerDraw License 1.0"}}},
	}
	runtimeComponent := map[string]any{
		"type":     "framework",
		"name":     "Go WebAssembly runtime support",
		"version":  expectedGoVersion,
		"purl":     runtimeRef,
		"bom-ref":  runtimeRef,
		"scope":    "required",
		"licenses": []any{map[string]any{"license": map[string]any{"id": "BSD-3-Clause"}}},
		"hashes":   []any{map[string]any{"alg": "SHA-256", "content": expectedWasmExecSHA256}},
	}
	components := make([]any, 0, len(modules)+1)
	rootDependencies := make([]any, 0, len(modules)+1)
	dependencies := make([]any, 0, len(modules)+2)
	for _, module := range modules {
		purl := "pkg:golang/" + module.Review.Module + "@" + module.Review.Version
		components = append(components, map[string]any{
			"type": "library", "name": module.Review.Module, "version": module.Review.Version,
			"purl": purl, "bom-ref": purl, "scope": "required",
			"licenses": []any{map[string]any{"license": map[string]any{"id": module.Review.License}}},
		})
		rootDependencies = append(rootDependencies, purl)
		dependencies = append(dependencies, map[string]any{"ref": purl, "dependsOn": []any{}})
	}
	components = append(components, runtimeComponent)
	rootDependencies = append(rootDependencies, runtimeRef)
	dependencies = append([]any{map[string]any{"ref": rootRef, "dependsOn": rootDependencies}}, dependencies...)
	dependencies = append(dependencies, map[string]any{"ref": runtimeRef, "dependsOn": []any{}})
	return map[string]any{
		"$schema":      "http://cyclonedx.org/schema/bom-1.6.schema.json",
		"bomFormat":    "CycloneDX",
		"specVersion":  "1.6",
		"version":      1,
		"metadata":     map[string]any{"component": rootComponent},
		"components":   components,
		"dependencies": dependencies,
	}
}

func copyFile(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o644)
}

func verifySBOM(output, expectedVersion string, modules []bundledModule) error {
	data, err := os.ReadFile(filepath.Join(output, sbomName))
	if err != nil {
		return err
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return err
	}
	canonical, err := canonicalJSON(document)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, canonical) {
		return errors.New("artifact SBOM is not canonical CycloneDX 1.6")
	}
	wasmDigest, err := fileDigest(filepath.Join(output, "layerdraw-engine.wasm"))
	if err != nil {
		return err
	}
	expected := sbomDocument(expectedVersion, wasmDigest, modules)
	if !deepEqualJSON(document, expected) {
		return errors.New("artifact SBOM release, legal, component, or dependency authority mismatch")
	}
	return nil
}

func deepEqualJSON(left, right any) bool {
	leftJSON, leftErr := canonicalJSON(left)
	rightJSON, rightErr := canonicalJSON(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func verifyWasmExec(output string) error {
	digest, err := fileDigest(filepath.Join(output, "wasm_exec.js"))
	if err != nil {
		return err
	}
	if digest != expectedWasmExecSHA256 {
		return fmt.Errorf("wasm_exec.js digest %s does not match pinned Go 1.26.5 support %s", digest, expectedWasmExecSHA256)
	}
	return nil
}

func canonicalJSON(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func fileDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func safeRelative(path string) bool {
	clean := filepath.Clean(filepath.FromSlash(path))
	return path != "" && filepath.ToSlash(clean) == path && !filepath.IsAbs(clean) && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}
