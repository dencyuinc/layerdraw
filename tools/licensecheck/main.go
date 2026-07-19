// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Command licensecheck enforces source and dependency license policy and
// creates the legal material that accompanies a LayerDraw binary.
package main

import (
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const spdxMarker = "SPDX-License-Identifier: "

type policy struct {
	SchemaVersion             int                `json:"schema_version"`
	AllowedLicenseExpressions []string           `json:"allowed_license_expressions"`
	DeniedLicenseExpressions  []string           `json:"denied_license_expressions"`
	Source                    sourcePolicy       `json:"source"`
	GoModules                 []reviewedGoModule `json:"go_modules"`
	NPMOverrides              []npmOverride      `json:"npm_overrides"`
}

type sourcePolicy struct {
	DefaultLicense string       `json:"default_license"`
	Roots          []string     `json:"roots"`
	Extensions     []string     `json:"extensions"`
	Rules          []sourceRule `json:"rules"`
}

type sourceRule struct {
	Prefix  string `json:"prefix"`
	License string `json:"license"`
}

type reviewedGoModule struct {
	Module        string `json:"module"`
	Version       string `json:"version"`
	License       string `json:"license"`
	LicenseFile   string `json:"license_file"`
	LicenseSHA256 string `json:"license_sha256"`
}

type npmOverride struct {
	Package         string `json:"package"`
	Version         string `json:"version"`
	ReportedLicense string `json:"reported_license"`
	License         string `json:"license"`
	LicenseFile     string `json:"license_file"`
	LicenseSHA256   string `json:"license_sha256"`
}

type goModule struct {
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

type goPackage struct {
	Module *struct {
		Path    string
		Version string
		Main    bool
	} `json:"Module"`
}

type npmPackage struct {
	Name     string   `json:"name"`
	Versions []string `json:"versions"`
	Paths    []string `json:"paths"`
	License  string   `json:"license"`
}

type packageManifest struct {
	License string `json:"license"`
	Version string `json:"version"`
}

type bundledModule struct {
	Review      reviewedGoModule
	LicenseText []byte
	PURL        string
	FileSHA256  string
}

type dependencyRecord struct {
	Ecosystem       string `json:"ecosystem"`
	Name            string `json:"name"`
	Version         string `json:"version"`
	Scope           string `json:"scope"`
	License         string `json:"license"`
	ReportedLicense string `json:"reported_license,omitempty"`
	ReviewSource    string `json:"review_source"`
	LicenseFile     string `json:"license_file,omitempty"`
	LicenseSHA256   string `json:"license_sha256,omitempty"`
}

type inventoryInput struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type inventorySummary struct {
	Total       int `json:"total"`
	Runtime     int `json:"runtime"`
	Development int `json:"development"`
	Go          int `json:"go"`
	NPM         int `json:"npm"`
}

type dependencyInventory struct {
	SchemaVersion int                `json:"schema_version"`
	Inputs        []inventoryInput   `json:"inputs"`
	Summary       inventorySummary   `json:"summary"`
	Dependencies  []dependencyRecord `json:"dependencies"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "licensecheck:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("expected subcommand: check or bundle")
	}

	switch args[0] {
	case "check":
		flags := flag.NewFlagSet("check", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		root := flags.String("root", ".", "repository root")
		policyPath := flags.String("policy", "tools/license-policy.json", "license policy path")
		reportPath := flags.String("report", "reports/dependency-licenses.json", "dependency inventory output path")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return checkRepository(*root, *policyPath, *reportPath)
	case "bundle":
		flags := flag.NewFlagSet("bundle", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		root := flags.String("root", ".", "repository root")
		policyPath := flags.String("policy", "tools/license-policy.json", "license policy path")
		binary := flags.String("binary", "", "compiled Go binary")
		output := flags.String("output", "dist", "bundle output directory")
		version := flags.String("version", "0.0.0-dev", "artifact version")
		bundledName := flags.String("bundled-name", "", "co-distributed native component name")
		bundledVersion := flags.String("bundled-version", "", "co-distributed native component version")
		bundledFile := flags.String("bundled-file", "", "co-distributed native component file")
		bundledLicense := flags.String("bundled-license", "", "co-distributed native component SPDX license")
		bundledLicenseFile := flags.String("bundled-license-file", "", "reviewed license text for the co-distributed native component")
		bundledLicenseSHA256 := flags.String("bundled-license-sha256", "", "reviewed license text SHA-256")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *binary == "" {
			return errors.New("bundle requires -binary")
		}
		extra := bundledModule{}
		if *bundledName != "" || *bundledVersion != "" || *bundledFile != "" || *bundledLicense != "" || *bundledLicenseFile != "" || *bundledLicenseSHA256 != "" {
			extra.Review = reviewedGoModule{Module: *bundledName, Version: *bundledVersion, License: *bundledLicense, LicenseFile: *bundledLicenseFile, LicenseSHA256: *bundledLicenseSHA256}
			extra.PURL = fmt.Sprintf("pkg:generic/%s@%s", strings.ToLower(strings.ReplaceAll(*bundledName, " ", "-")), *bundledVersion)
			if extra.Review.Module == "" || extra.Review.Version == "" || *bundledFile == "" || extra.Review.License == "" || extra.Review.LicenseFile == "" || extra.Review.LicenseSHA256 == "" {
				return errors.New("bundled component metadata is incomplete")
			}
			p, err := loadPolicy(*root, *policyPath)
			if err != nil {
				return err
			}
			if err := requireAllowedLicense(extra.Review.License, stringSet(p.AllowedLicenseExpressions), stringSet(p.DeniedLicenseExpressions)); err != nil {
				return fmt.Errorf("bundled component: %w", err)
			}
			licensePath := extra.Review.LicenseFile
			if !filepath.IsAbs(licensePath) {
				licensePath = filepath.Join(*root, licensePath)
			}
			if err := verifyLicenseFile(filepath.Dir(licensePath), filepath.Base(licensePath), extra.Review.LicenseSHA256); err != nil {
				return fmt.Errorf("bundled component license: %w", err)
			}
			extra.LicenseText, err = os.ReadFile(licensePath)
			if err != nil {
				return fmt.Errorf("bundled component license: %w", err)
			}
			bundledPath := *bundledFile
			if !filepath.IsAbs(bundledPath) {
				bundledPath = filepath.Join(*root, bundledPath)
			}
			extra.FileSHA256, err = fileSHA256(bundledPath)
			if err != nil {
				return fmt.Errorf("bundled component: %w", err)
			}
		}
		return bundleArtifact(*root, *policyPath, *binary, *output, *version, extra)
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func checkRepository(root, policyPath, reportPath string) error {
	p, err := loadPolicy(root, policyPath)
	if err != nil {
		return err
	}
	if err := checkSourceHeaders(root, p.Source); err != nil {
		return err
	}
	goDependencies, err := checkGoModules(root, p)
	if err != nil {
		return err
	}
	npmDependencies, err := checkNPMDependencies(root, p)
	if err != nil {
		return err
	}
	dependencies := append(goDependencies, npmDependencies...)
	sort.Slice(dependencies, func(i, j int) bool {
		if dependencies[i].Ecosystem != dependencies[j].Ecosystem {
			return dependencies[i].Ecosystem < dependencies[j].Ecosystem
		}
		if dependencies[i].Name != dependencies[j].Name {
			return dependencies[i].Name < dependencies[j].Name
		}
		return dependencies[i].Version < dependencies[j].Version
	})
	if err := writeDependencyInventory(root, reportPath, policyPath, dependencies); err != nil {
		return err
	}
	summary := summarizeDependencies(dependencies)

	fmt.Printf("License policy passed: %d runtime/%d development dependencies; inventory: %s.\n",
		summary.Runtime, summary.Development, filepath.ToSlash(reportPath))
	return nil
}

func loadPolicy(root, policyPath string) (policy, error) {
	if !filepath.IsAbs(policyPath) {
		policyPath = filepath.Join(root, policyPath)
	}
	data, err := os.ReadFile(policyPath)
	if err != nil {
		return policy{}, fmt.Errorf("read policy: %w", err)
	}
	var p policy
	if err := json.Unmarshal(data, &p); err != nil {
		return policy{}, fmt.Errorf("decode policy: %w", err)
	}
	if p.SchemaVersion != 1 {
		return policy{}, fmt.Errorf("unsupported policy schema_version %d", p.SchemaVersion)
	}
	if err := validatePolicy(p); err != nil {
		return policy{}, err
	}
	return p, nil
}

func validatePolicy(p policy) error {
	allowed := stringSet(p.AllowedLicenseExpressions)
	denied := stringSet(p.DeniedLicenseExpressions)
	for expression := range allowed {
		if denied[expression] {
			return fmt.Errorf("license expression %q is both allowed and denied", expression)
		}
	}
	for _, review := range p.GoModules {
		if review.Module == "" || review.Version == "" || review.LicenseFile == "" || review.LicenseSHA256 == "" {
			return fmt.Errorf("incomplete Go module review for %s@%s", review.Module, review.Version)
		}
		if err := requireAllowedLicense(review.License, allowed, denied); err != nil {
			return fmt.Errorf("Go module %s@%s: %w", review.Module, review.Version, err)
		}
	}
	for _, override := range p.NPMOverrides {
		if override.Package == "" || override.Version == "" || override.ReportedLicense == "" || override.LicenseFile == "" || override.LicenseSHA256 == "" {
			return fmt.Errorf("incomplete npm override for %s@%s", override.Package, override.Version)
		}
		if err := requireAllowedLicense(override.License, allowed, denied); err != nil {
			return fmt.Errorf("npm override %s@%s: %w", override.Package, override.Version, err)
		}
	}
	return nil
}

func checkSourceHeaders(root string, source sourcePolicy) error {
	extensions := stringSet(source.Extensions)
	checked := 0
	for _, sourceRoot := range source.Roots {
		absoluteRoot := filepath.Join(root, filepath.FromSlash(sourceRoot))
		info, err := os.Stat(absoluteRoot)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("stat source root %s: %w", sourceRoot, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("source root %s is not a directory", sourceRoot)
		}

		err = filepath.WalkDir(absoluteRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "node_modules" || entry.Name() == "dist" || entry.Name() == "coverage" {
					return filepath.SkipDir
				}
				return nil
			}
			isJSONSchema := strings.HasSuffix(path, ".schema.json")
			if !isJSONSchema && !extensions[filepath.Ext(path)] {
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			expected := expectedSourceLicense(relative, source)
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if isJSONSchema {
				var schema struct {
					Comment string `json:"$comment"`
				}
				if err := json.Unmarshal(data, &schema); err != nil {
					return fmt.Errorf("decode JSON Schema %s: %w", relative, err)
				}
				if schema.Comment != spdxMarker+expected {
					return fmt.Errorf("%s must declare %q in $comment", relative, spdxMarker+expected)
				}
			} else {
				header := string(data)
				if len(header) > 2048 {
					header = header[:2048]
				}
				if !strings.Contains(header, spdxMarker+expected) {
					return fmt.Errorf("%s must declare %s%s near the file header", relative, spdxMarker, expected)
				}
			}
			checked++
			return nil
		})
		if err != nil {
			return err
		}
	}
	if checked == 0 {
		return errors.New("source header policy did not inspect any files")
	}
	return nil
}

func expectedSourceLicense(path string, source sourcePolicy) string {
	license := source.DefaultLicense
	longest := 0
	for _, rule := range source.Rules {
		if strings.HasPrefix(path, rule.Prefix) && len(rule.Prefix) > longest {
			license = rule.License
			longest = len(rule.Prefix)
		}
	}
	return license
}

func checkGoModules(root string, p policy) ([]dependencyRecord, error) {
	if _, err := runCommand(root, "go", "mod", "download", "all"); err != nil {
		return nil, err
	}
	modules, err := listGoModules(root)
	if err != nil {
		return nil, err
	}
	runtimeModules, err := listRuntimeGoModules(root)
	if err != nil {
		return nil, err
	}

	reviews := make(map[string]reviewedGoModule, len(p.GoModules))
	for _, review := range p.GoModules {
		key := moduleKey(review.Module, review.Version)
		if _, exists := reviews[key]; exists {
			return nil, fmt.Errorf("duplicate Go module review %s", key)
		}
		reviews[key] = review
	}
	used := make(map[string]bool, len(modules))
	allowed := stringSet(p.AllowedLicenseExpressions)
	denied := stringSet(p.DeniedLicenseExpressions)
	records := make([]dependencyRecord, 0, len(modules)-1)
	for _, module := range modules {
		if module.Main {
			continue
		}
		if module.Replace != nil {
			return nil, fmt.Errorf("Go module replacement %s => %s is not covered by the reviewed dependency model", moduleKey(module.Path, module.Version), moduleKey(module.Replace.Path, module.Replace.Version))
		}
		key := moduleKey(module.Path, module.Version)
		review, ok := reviews[key]
		if !ok {
			return nil, fmt.Errorf("Go module %s has no reviewed license entry", key)
		}
		if err := requireAllowedLicense(review.License, allowed, denied); err != nil {
			return nil, fmt.Errorf("Go module %s: %w", key, err)
		}
		if err := verifyLicenseFile(module.Dir, review.LicenseFile, review.LicenseSHA256); err != nil {
			return nil, fmt.Errorf("Go module %s: %w", key, err)
		}
		used[key] = true
		scope := "development"
		if runtimeModules[key] {
			scope = "runtime"
		}
		records = append(records, dependencyRecord{
			Ecosystem:     "go",
			Name:          module.Path,
			Version:       module.Version,
			Scope:         scope,
			License:       review.License,
			ReviewSource:  "reviewed-license-file",
			LicenseFile:   review.LicenseFile,
			LicenseSHA256: review.LicenseSHA256,
		})
	}
	for key := range reviews {
		if !used[key] {
			return nil, fmt.Errorf("stale Go module license review %s", key)
		}
	}
	return records, nil
}

func listGoModules(root string) ([]goModule, error) {
	output, err := runCommand(root, "go", "list", "-m", "-json", "all")
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	var modules []goModule
	for decoder.More() {
		var module goModule
		if err := decoder.Decode(&module); err != nil {
			return nil, fmt.Errorf("decode go list -m output: %w", err)
		}
		modules = append(modules, module)
	}
	return modules, nil
}

func listRuntimeGoModules(root string) (map[string]bool, error) {
	output, err := runCommand(root, "go", "list", "-deps", "-json", "./cmd/...")
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	runtime := make(map[string]bool)
	for decoder.More() {
		var pkg goPackage
		if err := decoder.Decode(&pkg); err != nil {
			return nil, fmt.Errorf("decode go list -deps output: %w", err)
		}
		if pkg.Module != nil && !pkg.Module.Main {
			runtime[moduleKey(pkg.Module.Path, pkg.Module.Version)] = true
		}
	}
	return runtime, nil
}

func checkNPMDependencies(root string, p policy) ([]dependencyRecord, error) {
	if _, err := os.Stat(filepath.Join(root, "package.json")); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if err := checkNPMManifests(root, p.Source); err != nil {
		return nil, err
	}

	all, err := listNPMLicenses(root, false)
	if err != nil {
		return nil, err
	}
	production, err := listNPMLicenses(root, true)
	if err != nil {
		return nil, err
	}
	productionKeys := npmPackageKeys(production)
	overrides := make(map[string]npmOverride, len(p.NPMOverrides))
	for _, override := range p.NPMOverrides {
		key := npmKey(override.Package, override.Version)
		if _, exists := overrides[key]; exists {
			return nil, fmt.Errorf("duplicate npm license override %s", key)
		}
		overrides[key] = override
	}

	allowed := stringSet(p.AllowedLicenseExpressions)
	denied := stringSet(p.DeniedLicenseExpressions)
	usedOverrides := make(map[string]bool)
	var records []dependencyRecord
	for _, packages := range all {
		for _, dependency := range packages {
			pathsByVersion, err := npmPathsByVersion(dependency.Paths)
			if err != nil {
				return nil, err
			}
			for _, version := range dependency.Versions {
				key := npmKey(dependency.Name, version)
				license := dependency.License
				if license == "" {
					license = "Unknown"
				}
				reportedLicense := license
				reviewSource := "package-metadata"
				licenseFile := ""
				licenseSHA256 := ""
				if override, ok := overrides[key]; ok {
					if override.ReportedLicense != license {
						return nil, fmt.Errorf("npm override %s expected reported license %q, got %q", key, override.ReportedLicense, license)
					}
					packagePath := pathsByVersion[version]
					if packagePath == "" {
						return nil, fmt.Errorf("npm override %s cannot locate installed package", key)
					}
					if err := verifyLicenseFile(packagePath, override.LicenseFile, override.LicenseSHA256); err != nil {
						return nil, fmt.Errorf("npm package %s: %w", key, err)
					}
					license = override.License
					reviewSource = "reviewed-license-file"
					licenseFile = override.LicenseFile
					licenseSHA256 = override.LicenseSHA256
					usedOverrides[key] = true
				}
				if err := requireAllowedLicense(license, allowed, denied); err != nil {
					return nil, fmt.Errorf("npm package %s: %w", key, err)
				}
				scope := "development"
				if productionKeys[key] {
					scope = "runtime"
				}
				records = append(records, dependencyRecord{
					Ecosystem:       "npm",
					Name:            dependency.Name,
					Version:         version,
					Scope:           scope,
					License:         license,
					ReportedLicense: reportedLicense,
					ReviewSource:    reviewSource,
					LicenseFile:     licenseFile,
					LicenseSHA256:   licenseSHA256,
				})
			}
		}
	}
	for key := range overrides {
		if !usedOverrides[key] {
			return nil, fmt.Errorf("stale npm license override %s", key)
		}
	}
	return records, nil
}

func checkNPMManifests(root string, source sourcePolicy) error {
	paths := []string{filepath.Join(root, "package.json")}
	for _, sourceRoot := range []string{"apps", "packages"} {
		absoluteRoot := filepath.Join(root, sourceRoot)
		if _, err := os.Stat(absoluteRoot); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		err := filepath.WalkDir(absoluteRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() && (entry.Name() == "node_modules" || entry.Name() == "dist") {
				return filepath.SkipDir
			}
			if !entry.IsDir() && entry.Name() == "package.json" {
				paths = append(paths, path)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var manifest packageManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return fmt.Errorf("decode npm package manifest %s: %w", path, err)
		}
		relative, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "." {
			relative = ""
		} else {
			relative += "/"
		}
		expected := "SEE LICENSE IN LICENSE"
		if expectedSourceLicense(relative+"source.ts", source) == "Apache-2.0" {
			expected = "Apache-2.0"
		}
		if manifest.License != expected {
			return fmt.Errorf("%s license = %q, want %q", filepath.ToSlash(path), manifest.License, expected)
		}
	}
	return nil
}

func listNPMLicenses(root string, productionOnly bool) (map[string][]npmPackage, error) {
	args := []string{"pnpm", "licenses", "list"}
	if productionOnly {
		args = append(args, "--prod")
	}
	args = append(args, "--json")
	output, err := runCommand(root, "corepack", args...)
	if err != nil {
		return nil, err
	}
	start := bytes.IndexByte(output, '{')
	end := bytes.LastIndexByte(output, '}')
	if start == -1 || end < start {
		if bytes.Contains(output, []byte("No licenses in packages found")) || len(bytes.TrimSpace(output)) == 0 {
			return map[string][]npmPackage{}, nil
		}
		return nil, fmt.Errorf("pnpm licenses did not return JSON: %s", strings.TrimSpace(string(output)))
	}
	var report map[string][]npmPackage
	if err := json.Unmarshal(output[start:end+1], &report); err != nil {
		return nil, fmt.Errorf("decode pnpm licenses output: %w", err)
	}
	return report, nil
}

func npmPackageKeys(report map[string][]npmPackage) map[string]bool {
	keys := make(map[string]bool)
	for _, packages := range report {
		for _, dependency := range packages {
			for _, version := range dependency.Versions {
				keys[npmKey(dependency.Name, version)] = true
			}
		}
	}
	return keys
}

func npmPathsByVersion(paths []string) (map[string]string, error) {
	result := make(map[string]string)
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(path, "package.json"))
		if err != nil {
			return nil, fmt.Errorf("read npm package manifest %s: %w", path, err)
		}
		var manifest packageManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return nil, fmt.Errorf("decode npm package manifest %s: %w", path, err)
		}
		result[manifest.Version] = path
	}
	return result, nil
}

func requireAllowedLicense(expression string, allowed, denied map[string]bool) error {
	if denied[expression] {
		return fmt.Errorf("license %q is denied", expression)
	}
	if !allowed[expression] {
		return fmt.Errorf("license %q requires explicit policy review", expression)
	}
	return nil
}

func verifyLicenseFile(base, relative, expectedDigest string) error {
	clean := filepath.Clean(relative)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("invalid license file path %q", relative)
	}
	path := filepath.Join(base, clean)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read license file %s: %w", path, err)
	}
	digest := sha256.Sum256(data)
	actual := hex.EncodeToString(digest[:])
	if actual != expectedDigest {
		return fmt.Errorf("license file digest changed for %s: got %s, want %s", path, actual, expectedDigest)
	}
	return nil
}

func writeDependencyInventory(root, reportPath, policyPath string, dependencies []dependencyRecord) error {
	inputPaths := []string{
		policyPath,
		"go.mod",
		"go.sum",
		"package.json",
		"pnpm-lock.yaml",
		"pnpm-workspace.yaml",
	}
	seen := make(map[string]bool)
	var inputs []inventoryInput
	for _, inputPath := range inputPaths {
		absolutePath := inputPath
		if !filepath.IsAbs(absolutePath) {
			absolutePath = filepath.Join(root, absolutePath)
		}
		if _, err := os.Stat(absolutePath); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		relativePath, err := filepath.Rel(root, absolutePath)
		if err != nil {
			return err
		}
		relativePath = filepath.ToSlash(relativePath)
		if seen[relativePath] {
			continue
		}
		digest, err := fileSHA256(absolutePath)
		if err != nil {
			return err
		}
		inputs = append(inputs, inventoryInput{Path: relativePath, SHA256: digest})
		seen[relativePath] = true
	}
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].Path < inputs[j].Path })

	inventory := dependencyInventory{
		SchemaVersion: 1,
		Inputs:        inputs,
		Summary:       summarizeDependencies(dependencies),
		Dependencies:  dependencies,
	}
	data, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if !filepath.IsAbs(reportPath) {
		reportPath = filepath.Join(root, reportPath)
	}
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		return err
	}
	temporaryPath := reportPath + ".tmp"
	if err := os.WriteFile(temporaryPath, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, reportPath); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	return nil
}

func summarizeDependencies(dependencies []dependencyRecord) inventorySummary {
	summary := inventorySummary{Total: len(dependencies)}
	for _, dependency := range dependencies {
		switch dependency.Scope {
		case "runtime":
			summary.Runtime++
		case "development":
			summary.Development++
		}
		switch dependency.Ecosystem {
		case "go":
			summary.Go++
		case "npm":
			summary.NPM++
		}
	}
	return summary
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func bundleArtifact(root, policyPath, binary, output, version string, extra bundledModule) error {
	p, err := loadPolicy(root, policyPath)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(binary) {
		binary = filepath.Join(root, binary)
	}
	if !filepath.IsAbs(output) {
		output = filepath.Join(root, output)
	}
	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		return fmt.Errorf("read Go build info from %s: %w", binary, err)
	}

	if _, err := runCommand(root, "go", "mod", "download", "all"); err != nil {
		return err
	}
	modules, err := listGoModules(root)
	if err != nil {
		return err
	}
	moduleDirs := make(map[string]string)
	for _, module := range modules {
		moduleDirs[moduleKey(module.Path, module.Version)] = module.Dir
	}
	reviews := make(map[string]reviewedGoModule, len(p.GoModules))
	for _, review := range p.GoModules {
		reviews[moduleKey(review.Module, review.Version)] = review
	}

	var bundled []bundledModule
	seen := make(map[string]bool)
	for _, dependency := range info.Deps {
		if dependency.Replace != nil {
			dependency = dependency.Replace
		}
		key := moduleKey(dependency.Path, dependency.Version)
		if seen[key] {
			continue
		}
		review, ok := reviews[key]
		if !ok {
			return fmt.Errorf("linked Go module %s has no reviewed license entry", key)
		}
		dir := moduleDirs[key]
		if dir == "" {
			return fmt.Errorf("linked Go module %s is not in the current module graph", key)
		}
		if err := verifyLicenseFile(dir, review.LicenseFile, review.LicenseSHA256); err != nil {
			return fmt.Errorf("linked Go module %s: %w", key, err)
		}
		licenseText, err := os.ReadFile(filepath.Join(dir, review.LicenseFile))
		if err != nil {
			return err
		}
		bundled = append(bundled, bundledModule{Review: review, LicenseText: licenseText})
		seen[key] = true
	}
	if extra.Review.Module != "" {
		bundled = append(bundled, extra)
	}
	sort.Slice(bundled, func(i, j int) bool {
		return moduleKey(bundled[i].Review.Module, bundled[i].Review.Version) < moduleKey(bundled[j].Review.Module, bundled[j].Review.Version)
	})

	if err := os.MkdirAll(output, 0o755); err != nil {
		return err
	}
	for source, destination := range map[string]string{
		"LICENSE":                            "LICENSE",
		"NOTICE":                             "NOTICE",
		"docs/legal/README.md":               "LICENSING.md",
		"docs/legal/licenses/Apache-2.0.txt": "licenses/Apache-2.0.txt",
	} {
		if err := copyFile(filepath.Join(root, filepath.FromSlash(source)), filepath.Join(output, destination)); err != nil {
			return err
		}
	}
	artifactName := filepath.Base(binary)
	if err := os.WriteFile(filepath.Join(output, "THIRD_PARTY_NOTICES.txt"), renderThirdPartyNotices(artifactName, bundled), 0o644); err != nil {
		return err
	}
	sbom, err := renderCycloneDX(artifactName, version, bundled)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(output, artifactName+".cdx.json"), sbom, 0o644); err != nil {
		return err
	}
	fmt.Printf("Bundled legal files and CycloneDX SBOM for %s with %d third-party modules.\n", artifactName, len(bundled))
	return nil
}

func renderThirdPartyNotices(artifactName string, modules []bundledModule) []byte {
	var output strings.Builder
	fmt.Fprintf(&output, "Third-Party Notices for %s\n\n", artifactName)
	output.WriteString("This file lists third-party modules linked into this artifact.\n")
	if len(modules) == 0 {
		output.WriteString("No third-party Go modules are linked into this artifact.\n")
		return []byte(output.String())
	}
	for _, module := range modules {
		output.WriteString("\n================================================================================\n")
		fmt.Fprintf(&output, "%s %s\nLicense: %s\n", module.Review.Module, module.Review.Version, module.Review.License)
		output.WriteString("--------------------------------------------------------------------------------\n")
		output.Write(bytes.TrimSpace(module.LicenseText))
		output.WriteString("\n")
	}
	return []byte(output.String())
}

func renderCycloneDX(artifactName, version string, modules []bundledModule) ([]byte, error) {
	rootRef := fmt.Sprintf("pkg:generic/%s@%s", artifactName, version)
	type license struct {
		ID   string `json:"id,omitempty"`
		Name string `json:"name,omitempty"`
	}
	type licenseChoice struct {
		License license `json:"license"`
	}
	type hash struct {
		Algorithm string `json:"alg"`
		Content   string `json:"content"`
	}
	type component struct {
		Type     string          `json:"type"`
		Name     string          `json:"name"`
		Version  string          `json:"version"`
		PURL     string          `json:"purl,omitempty"`
		BOMRef   string          `json:"bom-ref"`
		Scope    string          `json:"scope,omitempty"`
		Licenses []licenseChoice `json:"licenses,omitempty"`
		Hashes   []hash          `json:"hashes,omitempty"`
	}
	type dependency struct {
		Ref       string   `json:"ref"`
		DependsOn []string `json:"dependsOn"`
	}
	type document struct {
		Schema      string `json:"$schema"`
		BOMFormat   string `json:"bomFormat"`
		SpecVersion string `json:"specVersion"`
		Version     int    `json:"version"`
		Metadata    struct {
			Component component `json:"component"`
		} `json:"metadata"`
		Components   []component  `json:"components"`
		Dependencies []dependency `json:"dependencies"`
	}

	doc := document{
		Schema:      "http://cyclonedx.org/schema/bom-1.6.schema.json",
		BOMFormat:   "CycloneDX",
		SpecVersion: "1.6",
		Version:     1,
		Components:  make([]component, 0, len(modules)),
	}
	doc.Metadata.Component = component{
		Type:     "application",
		Name:     artifactName,
		Version:  version,
		PURL:     rootRef,
		BOMRef:   rootRef,
		Licenses: []licenseChoice{{License: license{Name: "LayerDraw License 1.0"}}},
	}
	rootDependency := dependency{Ref: rootRef, DependsOn: make([]string, 0, len(modules))}
	for _, module := range modules {
		purl := module.PURL
		if purl == "" {
			purl = fmt.Sprintf("pkg:golang/%s@%s", module.Review.Module, module.Review.Version)
		}
		hashes := []hash{}
		if module.FileSHA256 != "" {
			hashes = append(hashes, hash{Algorithm: "SHA-256", Content: module.FileSHA256})
		}
		doc.Components = append(doc.Components, component{
			Type:     "library",
			Name:     module.Review.Module,
			Version:  module.Review.Version,
			PURL:     purl,
			BOMRef:   purl,
			Scope:    "required",
			Licenses: []licenseChoice{{License: license{ID: module.Review.License}}},
			Hashes:   hashes,
		})
		rootDependency.DependsOn = append(rootDependency.DependsOn, purl)
		doc.Dependencies = append(doc.Dependencies, dependency{Ref: purl, DependsOn: []string{}})
	}
	doc.Dependencies = append([]dependency{rootDependency}, doc.Dependencies...)
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func copyFile(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read %s: %w", source, err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create destination directory for %s: %w", destination, err)
	}
	if err := os.WriteFile(destination, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", destination, err)
	}
	return nil
}

func runCommand(root, name string, args ...string) ([]byte, error) {
	command := exec.Command(name, args...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, output)
	}
	return output, nil
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func moduleKey(module, version string) string {
	return module + "@" + version
}

func npmKey(name, version string) string {
	return name + "@" + version
}
