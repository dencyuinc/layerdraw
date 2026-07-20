// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine"
)

func TestPackagedConformanceExecutesCanonicalNonMCPScenarios(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"cold_start", conformanceColdStart}, {"project_open", conformanceProjectOpen},
		{"search_analysis", conformanceSearchAnalysis}, {"preview", conformancePreview},
		{"commit", conformanceCommitDurable}, {"viewer", conformanceViewer},
		{"external", conformanceExternal}, {"shutdown", conformanceShutdown},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPackagedConformanceFixtureCompiles(t *testing.T) {
	_, err := engine.New(engine.BuildInfo{}).Compile(context.Background(), engine.CompileInput{Mode: engine.CompileProject, EntryPath: "fixture.ldl", ProjectSourceTree: map[string][]byte{"fixture.ldl": []byte(conformanceProjectSource)}, ResolvedDependencies: engine.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPackagedConformanceReportIsStrictAndExclusive(t *testing.T) {
	report := PackagedConformanceReport{SchemaVersion: 1, SourceRevision: "0123456789abcdef0123456789abcdef01234567", Platform: "linux", ArtifactKind: "installed_desktop", Iterations: 5, Scenarios: map[string]ConformanceSamples{}, ProcessTreePeakRSSMebibytes: []int64{1, 1, 1, 1, 1}, ScenarioEvidence: cloneEvidence()}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded PackagedConformanceReport
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.SourceRevision != report.SourceRevision {
		t.Fatalf("report=%+v err=%v", decoded, err)
	}
	output := filepath.Join(t.TempDir(), "result.json")
	if err := writeExclusivePackagedProbe(output, encoded); err != nil {
		t.Fatal(err)
	}
	if err := writeExclusivePackagedProbe(output, encoded); err == nil {
		t.Fatal("existing attestation was overwritten")
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatal(err)
	}
}

func TestRunPackagedConformanceExecutesEveryIteration(t *testing.T) {
	t.Setenv("LAYERDRAW_CONFORMANCE_SOURCE_REVISION", "0123456789abcdef0123456789abcdef01234567")
	output := filepath.Join(t.TempDir(), "conformance.json")
	if err := RunPackagedConformance(output); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var report PackagedConformanceReport
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		t.Fatal(err)
	}
	if report.Iterations != packagedConformanceIterations || len(report.Scenarios) != len(conformanceEvidence) || len(report.ProcessTreePeakRSSMebibytes) != packagedConformanceIterations {
		t.Fatalf("incomplete report: %+v", report)
	}
	for name, evidence := range conformanceEvidence {
		if len(report.Scenarios[name].SamplesMilliseconds) != packagedConformanceIterations || report.ScenarioEvidence[name] != evidence {
			t.Fatalf("scenario %q is incomplete: samples=%+v evidence=%q", name, report.Scenarios[name], report.ScenarioEvidence[name])
		}
	}
}
