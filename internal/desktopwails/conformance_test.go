// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

func TestPackagedConformanceRejectsInvalidInvocation(t *testing.T) {
	if err := RunPackagedConformance("relative.json"); err == nil || PackagedConformanceFailureCode(err) != "invocation.output" {
		t.Fatal("relative output was accepted")
	}
	t.Setenv("LAYERDRAW_CONFORMANCE_SOURCE_REVISION", "not-a-revision")
	if err := RunPackagedConformance(filepath.Join(t.TempDir(), "result.json")); err == nil {
		t.Fatal("invalid source revision was accepted")
	}

	for _, test := range []struct {
		platform desktopcontract.DesktopPlatform
		want     string
		ok       bool
	}{
		{desktopcontract.PlatformMacOS, "darwin", true},
		{desktopcontract.PlatformWindows, "windows", true},
		{desktopcontract.PlatformLinux, "linux", true},
		{"invalid", "", false},
	} {
		got, err := conformancePlatform(test.platform)
		if got != test.want || (err == nil) != test.ok {
			t.Fatalf("platform=%q got=%q err=%v", test.platform, got, err)
		}
	}
}

func TestPackagedConformanceFailureCodesAreClosed(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "credential-secret")
	err := conformanceFailure("scenario.cold_start", errors.New(secret))
	if got := PackagedConformanceFailureCode(err); got != "scenario.cold_start" || strings.Contains(got, secret) {
		t.Fatalf("failure code=%q", got)
	}
	if got := PackagedConformanceFailureCode(errors.New(secret)); got != "" {
		t.Fatalf("untyped failure exposed code=%q", got)
	}
	stage := &conformanceStageError{stage: "compose", err: errors.New("closed")}
	if stage.Error() != "compose" || !errors.Is(stage, stage.err) || err.Error() != "scenario.cold_start" || !errors.Is(err, errors.Unwrap(err)) {
		t.Fatal("closed conformance errors lost their code or cause")
	}
}

func TestPackagedConformanceChildFailureParserIsClosed(t *testing.T) {
	if got := parseConformanceChildFailure([]byte("LayerDraw Desktop conformance failed [scenario.cold_start.compose]\n"), "cold_start"); got != "scenario.cold_start.compose" {
		t.Fatalf("closed child code=%q", got)
	}
	for _, input := range []string{
		"LayerDraw Desktop conformance failed [scenario.other.compose]",
		"LayerDraw Desktop conformance failed [scenario.cold_start.C:\\secret]",
		"raw native error C:\\secret",
	} {
		if got := parseConformanceChildFailure([]byte(input), "cold_start"); got != "" {
			t.Fatalf("unsafe child diagnostic accepted: %q", got)
		}
	}
}

func TestPackagedConformanceRejectsNonDesktopChildProcess(t *testing.T) {
	rss, err := runConformanceScenarioProcess(context.Background(), "cold_start")
	if err == nil || rss != 0 {
		t.Fatalf("test binary impersonated installed Desktop: rss=%d err=%v", rss, err)
	}
}

func TestPackagedConformanceParsesClosedChildResults(t *testing.T) {
	original := executeConformanceScenario
	t.Cleanup(func() { executeConformanceScenario = original })
	executeConformanceScenario = func(context.Context, string, string) ([]byte, error) {
		return []byte(`{"schema_version":1,"scenario":"cold_start","isolated_worker_peak_rss_mebibytes":64}`), nil
	}
	if rss, err := runConformanceScenarioProcess(context.Background(), "cold_start"); err != nil || rss != 64 {
		t.Fatalf("child result rss=%d err=%v", rss, err)
	}
	executeConformanceScenario = func(context.Context, string, string) ([]byte, error) { return []byte(`{}`), nil }
	if _, err := runConformanceScenarioProcess(context.Background(), "cold_start"); err == nil {
		t.Fatal("invalid child result accepted")
	}
	executeConformanceScenario = func(context.Context, string, string) ([]byte, error) {
		return nil, &exec.ExitError{Stderr: []byte("LayerDraw Desktop conformance failed [scenario.cold_start.compose]\n")}
	}
	if _, err := runConformanceScenarioProcess(context.Background(), "cold_start"); PackagedConformanceFailureCode(err) != "scenario.cold_start.compose" {
		t.Fatalf("closed child failure=%v", err)
	}
}

func TestConformanceRuntimeAndClosedInputsStayBounded(t *testing.T) {
	runtime := conformanceRuntime{}
	ctx := context.Background()
	if _, _, err := compose(desktopapp.Config{}, nil, nil); err == nil {
		t.Fatal("nil native runtime was accepted")
	}
	if err := writeExclusivePackagedProbe("relative.json", []byte("{}")); err == nil {
		t.Fatal("relative probe output was accepted")
	}
	if value, err := runtime.OpenDirectory(ctx, "ignored"); err != nil || value != "" {
		t.Fatalf("open directory value=%q err=%v", value, err)
	}
	if value, err := runtime.OpenFile(ctx, "ignored", nil); err != nil || value != "" {
		t.Fatalf("open file value=%q err=%v", value, err)
	}
	if value, err := runtime.SaveFile(ctx, "ignored", nil); err != nil || value != "" {
		t.Fatalf("save file value=%q err=%v", value, err)
	}
	runtime.ShowWindow(ctx)
	runtime.Quit(ctx)
	runtime.Emit(ctx, "ignored")

	instance := &conformanceInstance{root: filepath.Join(t.TempDir(), "missing")}
	if _, err := instance.openProject(ctx, conformanceAuthoringSource); err == nil {
		t.Fatal("project was opened through a missing root")
	}
	if _, err := conformancePreconditions(ctx, "not valid LDL"); err == nil {
		t.Fatal("invalid source produced edit preconditions")
	}
	if report := (&WailsShellBridge{}).lastAccessibilityReport(); report != nil {
		t.Fatalf("empty shell bridge retained report %+v", report)
	}

	base, err := NewSharedConfig(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	base.Adapters[desktopcontract.ComponentExternalStorage] = disabledComponent{}
	base.Capabilities = nil
	if _, _, err := compose(base, runtime, map[string]ExternalProvider{"conformance": conformanceProvider{}}); err == nil {
		t.Fatal("external lifecycle accepted a foreign capability owner")
	}
	base, err = NewSharedConfig(filepath.Join(t.TempDir(), "noncanonical-state"))
	if err != nil {
		t.Fatal(err)
	}
	base.Adapters = nil
	base.MCPCapabilities = nil
	application, _, err := compose(base, runtime, nil)
	if err == nil || application != nil {
		t.Fatalf("incomplete noncanonical composition was accepted: application=%v err=%v", application, err)
	}

	handshake, err := (nativeCapabilities{externalStorage: true}).Negotiate(ctx, desktopcontract.DefaultManifest())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, status := range handshake.CapabilityStatuses {
		if status.CapabilityID == desktopcontract.CapabilityExternalStorage {
			found = status.Enabled
		}
	}
	if !found {
		t.Fatal("external storage capability was not negotiated")
	}
}

func TestConformanceScenariosFailClosedOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"cold_start", conformanceColdStart},
		{"project_open", conformanceProjectOpen},
		{"search_analysis", conformanceSearchAnalysis},
		{"preview", conformancePreview},
		{"commit", conformanceCommitDurable},
		{"viewer", conformanceViewer},
		{"mcp", conformanceMCP},
		{"external", conformanceExternal},
		{"shutdown", conformanceShutdown},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(ctx); err == nil {
				t.Fatal("cancelled conformance scenario succeeded")
			}
		})
	}
}

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

func TestPackagedConformanceExecutesCanonicalMCPScenario(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := conformanceMCP(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPackagedConformanceFixtureCompiles(t *testing.T) {
	_, err := engine.New(engine.BuildInfo{}).Compile(context.Background(), engine.CompileInput{Mode: engine.CompileProject, EntryPath: "fixture.ldl", ProjectSourceTree: map[string][]byte{"fixture.ldl": []byte(conformanceProjectSource)}, ResolvedDependencies: engine.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPackagedConformanceReportIsStrictAndExclusive(t *testing.T) {
	report := PackagedConformanceReport{SchemaVersion: 1, SourceRevision: "0123456789abcdef0123456789abcdef01234567", Platform: "linux", ArtifactKind: "installed_desktop", Iterations: 5, Scenarios: map[string]ConformanceSamples{}, IsolatedWorkerPeakRSSMiB: []int64{1, 1, 1, 1, 1}, ScenarioEvidence: cloneEvidence()}
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
	original := runConformanceScenarioProcess
	t.Cleanup(func() { runConformanceScenarioProcess = original })
	calls := 0
	runConformanceScenarioProcess = func(_ context.Context, name string) (int64, error) {
		calls++
		if _, ok := conformanceRunners()[name]; !ok {
			return 0, errors.New("unknown scenario")
		}
		return int64(64 + calls%3), nil
	}
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
	if report.Iterations != packagedConformanceIterations || len(report.Scenarios) != len(conformanceEvidence) || len(report.IsolatedWorkerPeakRSSMiB) != packagedConformanceIterations {
		t.Fatalf("incomplete report: %+v", report)
	}
	if calls != packagedConformanceIterations*len(conformanceEvidence) {
		t.Fatalf("isolated scenario calls=%d", calls)
	}
	for name, evidence := range conformanceEvidence {
		if len(report.Scenarios[name].SamplesMilliseconds) != packagedConformanceIterations || report.ScenarioEvidence[name] != evidence {
			t.Fatalf("scenario %q is incomplete: samples=%+v evidence=%q", name, report.Scenarios[name], report.ScenarioEvidence[name])
		}
	}
	if err := RunPackagedConformance(output); PackagedConformanceFailureCode(err) != "result.write" {
		t.Fatalf("existing result was overwritten: %v", err)
	}
}

func TestRunPackagedConformanceScenarioExecutesInstalledWorkflow(t *testing.T) {
	var output bytes.Buffer
	if err := RunPackagedConformanceScenario("cold_start", &output); err != nil {
		t.Fatal(err)
	}
	var report packagedConformanceScenarioReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil || report.SchemaVersion != 1 || report.Scenario != "cold_start" || report.WorkerPeakRSS <= 0 {
		t.Fatalf("scenario report=%+v err=%v", report, err)
	}
	if err := RunPackagedConformanceScenario("invalid", &output); PackagedConformanceFailureCode(err) != "scenario.invalid" {
		t.Fatalf("invalid scenario err=%v", err)
	}
	if err := RunPackagedConformanceScenario("cold_start", nil); PackagedConformanceFailureCode(err) != "scenario.invalid" {
		t.Fatalf("nil output err=%v", err)
	}
}

func TestRunPackagedConformancePreservesClosedWorkerFailures(t *testing.T) {
	t.Setenv("LAYERDRAW_CONFORMANCE_SOURCE_REVISION", "0123456789abcdef0123456789abcdef01234567")
	original := runConformanceScenarioProcess
	t.Cleanup(func() { runConformanceScenarioProcess = original })
	output := func() string { return filepath.Join(t.TempDir(), "conformance.json") }

	runConformanceScenarioProcess = func(context.Context, string) (int64, error) {
		return 0, conformanceFailure("scenario.cold_start.compose", errors.New("closed"))
	}
	if err := RunPackagedConformance(output()); PackagedConformanceFailureCode(err) != "scenario.cold_start.compose" {
		t.Fatalf("typed worker failure=%v", err)
	}

	runConformanceScenarioProcess = func(context.Context, string) (int64, error) { return 0, errors.New("closed") }
	if err := RunPackagedConformance(output()); PackagedConformanceFailureCode(err) != "scenario.cold_start" {
		t.Fatalf("untyped worker failure=%v", err)
	}

	runConformanceScenarioProcess = func(context.Context, string) (int64, error) { return 0, nil }
	if err := RunPackagedConformance(output()); PackagedConformanceFailureCode(err) != "measurement.memory" {
		t.Fatalf("missing worker measurement=%v", err)
	}
}
