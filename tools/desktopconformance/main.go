// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type evidenceRef string

type featureEvidence struct {
	Feature   string        `json:"feature"`
	Delivered bool          `json:"delivered"`
	Evidence  []evidenceRef `json:"evidence"`
}

type performanceBudget struct {
	MaxMilliseconds int64 `json:"max_milliseconds,omitempty"`
	MaxMebibytes    int64 `json:"max_mebibytes,omitempty"`
	MinIterations   int   `json:"min_iterations"`
	Percentile      int   `json:"percentile"`
}

type manifest struct {
	SchemaVersion      uint32                       `json:"schema_version"`
	Delivery           string                       `json:"delivery"`
	NormativeMatrix    string                       `json:"normative_matrix"`
	Features           map[string]featureEvidence   `json:"features"`
	AcceptanceSuites   map[string][]evidenceRef     `json:"acceptance_suites"`
	Faults             map[string][]evidenceRef     `json:"faults"`
	ReleaseEvidence    []string                     `json:"release_evidence"`
	PerformanceBudgets map[string]performanceBudget `json:"performance_budgets"`
}

type matrixRow struct {
	Name      string
	Delivered bool
}

var matrixPattern = regexp.MustCompile(`(?m)^\| (F\d{2}) \| ([^|]+?) \| [^|]+ \| [^|]+ \| ([✓-]) \|`)

func main() {
	root := flag.String("root", ".", "repository root")
	manifestPath := flag.String("manifest", "deploy/desktop-conformance.json", "Desktop closure manifest")
	flag.Parse()
	if flag.NArg() != 1 || flag.Arg(0) != "verify" {
		fail(errors.New("usage: desktopconformance [flags] verify"))
	}
	_, err := verify(*root, *manifestPath)
	if err != nil {
		fail(err)
	}
}

func verify(root, relativeManifest string) (manifest, error) {
	var value manifest
	if err := decodeStrict(filepath.Join(root, relativeManifest), &value); err != nil {
		return value, err
	}
	if value.SchemaVersion != 1 || value.Delivery != "desktop" || value.NormativeMatrix != "docs/blueprint.md#1311-feature-x-delivery-matrix" {
		return value, errors.New("Desktop conformance manifest identity is invalid")
	}
	matrix, err := readDesktopMatrix(filepath.Join(root, "docs/blueprint.md"))
	if err != nil {
		return value, err
	}
	if len(matrix) != 62 || len(value.Features) != len(matrix) {
		return value, fmt.Errorf("Desktop feature closure is incomplete: matrix=%d manifest=%d", len(matrix), len(value.Features))
	}
	for id, expected := range matrix {
		actual, ok := value.Features[id]
		if !ok || actual.Feature != expected.Name || actual.Delivered != expected.Delivered {
			return value, fmt.Errorf("Desktop feature closure mismatch for %s", id)
		}
		if expected.Delivered && len(actual.Evidence) == 0 {
			return value, fmt.Errorf("delivered Desktop feature %s has no executable evidence", id)
		}
		if !expected.Delivered && len(actual.Evidence) != 0 {
			return value, fmt.Errorf("excluded Desktop feature %s claims evidence", id)
		}
		if err := verifyEvidence(root, actual.Evidence...); err != nil {
			return value, fmt.Errorf("%s: %w", id, err)
		}
	}
	requiredSuites := []string{"installed_workflows", "mcp", "fault_recovery", "ownership_boundaries", "transport_parity", "accessibility"}
	if err := requireExactKeys(value.AcceptanceSuites, requiredSuites); err != nil {
		return value, fmt.Errorf("acceptance suites: %w", err)
	}
	for name, evidence := range value.AcceptanceSuites {
		if len(evidence) == 0 {
			return value, fmt.Errorf("acceptance suite %s is empty", name)
		}
		if err := verifyEvidence(root, evidence...); err != nil {
			return value, fmt.Errorf("acceptance suite %s: %w", name, err)
		}
	}
	if err := requireExactKeys(value.Faults, []string{"power_loss", "corrupt_state", "missing_file", "permission_denial", "stale_revision", "concurrent_open", "backend_crash", "provider_conflict", "mcp_disconnect", "revoked_delegation", "failed_upgrade"}); err != nil {
		return value, fmt.Errorf("fault matrix: %w", err)
	}
	for name, evidence := range value.Faults {
		if len(evidence) == 0 {
			return value, fmt.Errorf("fault %s has no recovery evidence", name)
		}
		if err := verifyEvidence(root, evidence...); err != nil {
			return value, fmt.Errorf("fault %s: %w", name, err)
		}
	}
	if err := requireExactValues(value.ReleaseEvidence, []string{"installer", "platform_signature", "sha256_digest", "cyclonedx_sbom", "license_bundle", "signed_updater_manifest", "packaged_content_check", "disabled_feature_manifest", "desktop_conformance_manifest"}); err != nil {
		return value, fmt.Errorf("release evidence: %w", err)
	}
	if err := verifyReleaseEvidence(root); err != nil {
		return value, err
	}
	requiredBudgets := []string{"cold_start", "project_open", "search_analysis", "preview", "commit", "viewer_interaction", "mcp_bounded_operations", "external_reconcile", "memory", "shutdown"}
	if err := requireExactKeys(value.PerformanceBudgets, requiredBudgets); err != nil {
		return value, fmt.Errorf("performance budgets: %w", err)
	}
	for name, budget := range value.PerformanceBudgets {
		if (budget.MaxMilliseconds <= 0) == (budget.MaxMebibytes <= 0) {
			return value, fmt.Errorf("performance budget %s must have exactly one positive limit", name)
		}
		if budget.MinIterations < 5 || budget.Percentile != 95 {
			return value, fmt.Errorf("performance budget %s must require at least five iterations at p95", name)
		}
	}
	return value, nil
}

func verifyReleaseEvidence(root string) error {
	requiredMarkers := map[string][]string{
		"tools/build-desktop-installer.sh":       {"LayerDraw-$version.dmg", "LayerDraw-$version.exe", "LayerDraw-$version.deb", "desktop-conformance.json", "LayerDraw-bundle.cdx.json", "THIRD_PARTY_NOTICES.txt"},
		"tools/build-desktop-update-metadata.sh": {"desktoprelease build", "desktoprelease verify", "-desktop-conformance"},
		"tools/desktoprelease/main.go":           {`json:"sha256"`, `json:"signature"`, `json:"desktop_conformance"`},
		"tools/smoke-desktop-installer.sh":       {"desktop-capabilities.json", "desktop-conformance.json", "corrupt-installer"},
		"tools/smoke-desktop-installer.ps1":      {"desktop-capabilities.json", "desktop-conformance.json", "corrupt.exe"},
		".github/workflows/desktop-release.yml":  {"LAYERDRAW_RELEASE_SIGNING", "desktop-release/*"},
	}
	for relative, markers := range requiredMarkers {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			return fmt.Errorf("release evidence %s: %w", relative, err)
		}
		for _, marker := range markers {
			if !bytes.Contains(data, []byte(marker)) {
				return fmt.Errorf("release evidence %s is missing %q", relative, marker)
			}
		}
	}
	return nil
}

func readDesktopMatrix(path string) (map[string]matrixRow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	rows := make(map[string]matrixRow)
	for _, match := range matrixPattern.FindAllSubmatch(data, -1) {
		rows[string(match[1])] = matrixRow{Name: strings.TrimSpace(string(match[2])), Delivered: string(match[3]) == "✓"}
	}
	return rows, nil
}

func verifyEvidence(root string, references ...evidenceRef) error {
	for _, reference := range references {
		parts := strings.Split(string(reference), "#")
		if len(parts) != 2 || !strings.HasPrefix(parts[1], "Test") || filepath.IsAbs(parts[0]) || strings.Contains(parts[0], "..") {
			return fmt.Errorf("invalid evidence reference %q", reference)
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(parts[0])))
		if err != nil {
			return fmt.Errorf("read evidence %q: %w", reference, err)
		}
		declaration := "func " + parts[1] + "("
		if !bytes.Contains(data, []byte(declaration)) {
			return fmt.Errorf("evidence test %q does not exist", reference)
		}
	}
	return nil
}

func decodeStrict(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(new(any)) == nil {
		return errors.New("manifest contains trailing JSON")
	}
	return nil
}

func requireExactKeys[T any](values map[string]T, expected []string) error {
	actual := make([]string, 0, len(values))
	for key := range values {
		actual = append(actual, key)
	}
	return requireExactValues(actual, expected)
}

func requireExactValues(actual, expected []string) error {
	actual = append([]string(nil), actual...)
	expected = append([]string(nil), expected...)
	sort.Strings(actual)
	sort.Strings(expected)
	if strings.Join(actual, "\x00") != strings.Join(expected, "\x00") {
		return fmt.Errorf("got %v, want %v", actual, expected)
	}
	return nil
}

func fail(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
