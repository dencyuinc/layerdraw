// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Command coveragecheck enforces LayerDraw's Go coverage policy.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	profileLinePattern = regexp.MustCompile(`^(.+):(\d+)\.(\d+),(\d+)\.(\d+) (\d+) (\d+)$`)
	hunkPattern        = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)
)

type policy struct {
	SchemaVersion         int           `json:"schema_version"`
	Module                string        `json:"module"`
	OverallMinimum        float64       `json:"overall_minimum"`
	DefaultPackageMinimum float64       `json:"default_package_minimum"`
	PatchMinimum          float64       `json:"patch_minimum"`
	PackageRules          []packageRule `json:"package_rules"`
}

type packageRule struct {
	Prefix  string  `json:"prefix"`
	Minimum float64 `json:"minimum"`
}

type coverageBlock struct {
	file       string
	packageID  string
	startLine  int
	endLine    int
	statements int
	count      int
}

type coverageTotal struct {
	statements int
	covered    int
}

func main() {
	profilePath := flag.String("profile", "coverage/go.out", "Go coverprofile to evaluate")
	policyPath := flag.String("policy", "tools/coverage-policy.json", "coverage policy JSON")
	baseRef := flag.String("base", "origin/main", "Git base ref for patch coverage; empty disables committed diff analysis")
	flag.Parse()

	if err := run(*profilePath, *policyPath, *baseRef, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(profilePath, policyPath, baseRef string, stdout io.Writer) error {
	policyValue, err := readPolicy(policyPath)
	if err != nil {
		return err
	}

	profileFile, err := os.Open(profilePath)
	if err != nil {
		return fmt.Errorf("open coverage profile: %w", err)
	}
	defer profileFile.Close()

	blocks, err := parseProfile(profileFile, policyValue.Module)
	if err != nil {
		return fmt.Errorf("parse coverage profile: %w", err)
	}

	addedLines, err := collectAddedLines(baseRef)
	if err != nil {
		return fmt.Errorf("collect changed Go lines: %w", err)
	}

	return evaluate(policyValue, blocks, addedLines, stdout)
}

func readPolicy(filename string) (policy, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return policy{}, fmt.Errorf("read coverage policy: %w", err)
	}

	var value policy
	if err := json.Unmarshal(data, &value); err != nil {
		return policy{}, fmt.Errorf("decode coverage policy: %w", err)
	}
	if value.SchemaVersion != 1 {
		return policy{}, fmt.Errorf("unsupported coverage policy schema_version %d", value.SchemaVersion)
	}
	if value.Module == "" {
		return policy{}, errors.New("coverage policy module is required")
	}
	if err := validateMinimum("overall_minimum", value.OverallMinimum); err != nil {
		return policy{}, err
	}
	if err := validateMinimum("default_package_minimum", value.DefaultPackageMinimum); err != nil {
		return policy{}, err
	}
	if err := validateMinimum("patch_minimum", value.PatchMinimum); err != nil {
		return policy{}, err
	}
	seenPrefixes := make(map[string]struct{}, len(value.PackageRules))
	for index, rule := range value.PackageRules {
		if rule.Prefix == "" {
			return policy{}, fmt.Errorf("package_rules[%d].prefix is required", index)
		}
		if rule.Prefix != value.Module && !strings.HasPrefix(rule.Prefix, value.Module+"/") {
			return policy{}, fmt.Errorf("package_rules[%d].prefix %q is outside module %q", index, rule.Prefix, value.Module)
		}
		if _, exists := seenPrefixes[rule.Prefix]; exists {
			return policy{}, fmt.Errorf("package_rules[%d].prefix %q is duplicated", index, rule.Prefix)
		}
		seenPrefixes[rule.Prefix] = struct{}{}
		if err := validateMinimum(fmt.Sprintf("package_rules[%d].minimum", index), rule.Minimum); err != nil {
			return policy{}, err
		}
	}

	return value, nil
}

func validateMinimum(name string, value float64) error {
	if value < 0 || value > 100 {
		return fmt.Errorf("%s must be between 0 and 100", name)
	}
	return nil
}

func parseProfile(reader io.Reader, module string) ([]coverageBlock, error) {
	scanner := bufio.NewScanner(reader)
	if !scanner.Scan() {
		return nil, errors.New("coverage profile is empty")
	}
	if !strings.HasPrefix(scanner.Text(), "mode: ") {
		return nil, errors.New("coverage profile has no mode header")
	}

	var blocks []coverageBlock
	for scanner.Scan() {
		matches := profileLinePattern.FindStringSubmatch(scanner.Text())
		if matches == nil {
			return nil, fmt.Errorf("invalid profile line %q", scanner.Text())
		}

		startLine, _ := strconv.Atoi(matches[2])
		endLine, _ := strconv.Atoi(matches[4])
		statements, _ := strconv.Atoi(matches[6])
		count, _ := strconv.Atoi(matches[7])
		filename := strings.TrimPrefix(matches[1], module+"/")
		if filename == matches[1] {
			return nil, fmt.Errorf("profile file %q is outside module %q", matches[1], module)
		}

		blocks = append(blocks, coverageBlock{
			file:       filename,
			packageID:  module + "/" + path.Dir(filename),
			startLine:  startLine,
			endLine:    endLine,
			statements: statements,
			count:      count,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		return nil, errors.New("coverage profile contains no statement blocks")
	}

	return blocks, nil
}

func collectAddedLines(baseRef string) (map[string]map[int]struct{}, error) {
	result := make(map[string]map[int]struct{})
	sourcePaths := []string{"cmd", "internal", "tools/protocolgen"}

	if baseRef != "" && !allZeroRevision(baseRef) {
		arguments := append([]string{"diff", "--unified=0", "--diff-filter=AMR", baseRef + "...HEAD", "--"}, sourcePaths...)
		output, err := gitOutput(arguments...)
		if err != nil {
			return nil, fmt.Errorf("diff from %s: %w", baseRef, err)
		}
		if err := mergeUnifiedDiff(result, strings.NewReader(output)); err != nil {
			return nil, err
		}
	}

	arguments := append([]string{"diff", "--unified=0", "--diff-filter=AMR", "HEAD", "--"}, sourcePaths...)
	workingOutput, err := gitOutput(arguments...)
	if err != nil {
		return nil, fmt.Errorf("working tree diff: %w", err)
	}
	if err := mergeUnifiedDiff(result, strings.NewReader(workingOutput)); err != nil {
		return nil, err
	}

	arguments = append([]string{"ls-files", "--others", "--exclude-standard", "--"}, sourcePaths...)
	untrackedOutput, err := gitOutput(arguments...)
	if err != nil {
		return nil, fmt.Errorf("list untracked files: %w", err)
	}
	untrackedScanner := bufio.NewScanner(strings.NewReader(untrackedOutput))
	for untrackedScanner.Scan() {
		filename := untrackedScanner.Text()
		if path.Ext(filename) != ".go" {
			continue
		}
		data, err := os.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("read untracked file %s: %w", filename, err)
		}
		lineCount := strings.Count(string(data), "\n")
		if len(data) > 0 && data[len(data)-1] != '\n' {
			lineCount++
		}
		for line := 1; line <= lineCount; line++ {
			addLine(result, filename, line)
		}
	}
	if err := untrackedScanner.Err(); err != nil {
		return nil, fmt.Errorf("scan untracked files: %w", err)
	}

	return result, nil
}

func gitOutput(args ...string) (string, error) {
	command := exec.Command("git", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func allZeroRevision(value string) bool {
	return strings.Trim(value, "0") == ""
}

func mergeUnifiedDiff(target map[string]map[int]struct{}, reader io.Reader) error {
	scanner := bufio.NewScanner(reader)
	var filename string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "+++ b/") {
			filename = strings.TrimPrefix(line, "+++ b/")
			continue
		}
		matches := hunkPattern.FindStringSubmatch(line)
		if matches == nil || filename == "" || path.Ext(filename) != ".go" {
			continue
		}

		start, _ := strconv.Atoi(matches[1])
		count := 1
		if matches[2] != "" {
			count, _ = strconv.Atoi(matches[2])
		}
		for changedLine := start; changedLine < start+count; changedLine++ {
			addLine(target, filename, changedLine)
		}
	}
	return scanner.Err()
}

func addLine(target map[string]map[int]struct{}, filename string, line int) {
	if target[filename] == nil {
		target[filename] = make(map[int]struct{})
	}
	target[filename][line] = struct{}{}
}

func evaluate(policyValue policy, blocks []coverageBlock, addedLines map[string]map[int]struct{}, stdout io.Writer) error {
	packageTotals := make(map[string]coverageTotal)
	var overall coverageTotal
	var patch coverageTotal

	for _, block := range blocks {
		total := packageTotals[block.packageID]
		total.statements += block.statements
		overall.statements += block.statements
		if block.count > 0 {
			total.covered += block.statements
			overall.covered += block.statements
		}
		packageTotals[block.packageID] = total

		if intersects(block, addedLines[block.file]) {
			patch.statements += block.statements
			if block.count > 0 {
				patch.covered += block.statements
			}
		}
	}

	failed := false
	overallPercent := percentage(overall)
	fmt.Fprintf(stdout, "overall coverage: %.1f%% (required %.1f%%)\n", overallPercent, policyValue.OverallMinimum)
	if below(overallPercent, policyValue.OverallMinimum) {
		failed = true
	}

	packageIDs := make([]string, 0, len(packageTotals))
	for packageID := range packageTotals {
		packageIDs = append(packageIDs, packageID)
	}
	sort.Strings(packageIDs)
	for _, packageID := range packageIDs {
		minimum := packageMinimum(policyValue, packageID)
		actual := percentage(packageTotals[packageID])
		fmt.Fprintf(stdout, "package coverage: %.1f%% (required %.1f%%) %s\n", actual, minimum, packageID)
		if below(actual, minimum) {
			failed = true
		}
	}

	if patch.statements == 0 {
		fmt.Fprintln(stdout, "patch coverage: not applicable (no changed executable Go statements)")
	} else {
		patchPercent := percentage(patch)
		fmt.Fprintf(stdout, "patch coverage: %.1f%% (required %.1f%%)\n", patchPercent, policyValue.PatchMinimum)
		if below(patchPercent, policyValue.PatchMinimum) {
			failed = true
		}
	}

	if failed {
		return errors.New("Go coverage policy failed")
	}
	return nil
}

func intersects(block coverageBlock, lines map[int]struct{}) bool {
	for line := block.startLine; line <= block.endLine; line++ {
		if _, exists := lines[line]; exists {
			return true
		}
	}
	return false
}

func packageMinimum(policyValue policy, packageID string) float64 {
	minimum := policyValue.DefaultPackageMinimum
	longestPrefix := -1
	for _, rule := range policyValue.PackageRules {
		if packageID != rule.Prefix && !strings.HasPrefix(packageID, rule.Prefix+"/") {
			continue
		}
		if len(rule.Prefix) > longestPrefix {
			minimum = rule.Minimum
			longestPrefix = len(rule.Prefix)
		}
	}
	return minimum
}

func percentage(total coverageTotal) float64 {
	if total.statements == 0 {
		return 100
	}
	return float64(total.covered) * 100 / float64(total.statements)
}

func below(actual, minimum float64) bool {
	return actual+0.000_001 < minimum
}
