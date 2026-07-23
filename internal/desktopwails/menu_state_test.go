// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	reviewapp "github.com/dencyuinc/layerdraw/internal/application/review"
	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	nativeexport "github.com/dencyuinc/layerdraw/internal/exporter"
	"github.com/dencyuinc/layerdraw/internal/registry"
	runtimeport "github.com/dencyuinc/layerdraw/internal/runtime/port"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func TestMenuLocaleStateDefaultsToSystemAndSticks(t *testing.T) {
	state := &menuLocaleState{}
	if state.value() != "system" {
		t.Fatalf("default locale=%q", state.value())
	}
	state.set("ja")
	if state.value() != "ja" {
		t.Fatalf("set locale=%q", state.value())
	}
}

// TestShellBindingSettingsGateAndPersistMCP covers the settings binding gate
// before restore and the MCP persist mirror once a shell is attached.
func TestShellBindingSettingsGateAndPersistMCP(t *testing.T) {
	runtime := &fakeWailsShellRuntime{
		screens: []wailsruntime.Screen{{IsPrimary: true, Width: 1920, Height: 1080}},
		width:   1280, height: 800, emitted: make(chan []any, 1),
	}
	bridge := newWailsShellBridge(runtime.calls())
	native, err := desktopapp.NewPlatformNativeShell(desktopapp.PlatformNativeShellConfig{
		Platform: CurrentPlatform(), StateRoot: t.TempDir(), Runtime: bridge,
		Commands: availableNewOpenCommandRouter{}, CrashRecovery: probeCrashRecovery{},
		Errors: wailsErrorSurface{runtime: &nativeStub{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	binding := newShellBinding(native.Shell, bridge)
	if result := binding.Settings(); result.Outcome == protocolcommon.OutcomeSuccess {
		t.Fatalf("settings before restore=%+v", result)
	}
	if restored := native.Shell.Restore(context.Background()); restored.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("restore=%+v", restored)
	}
	if result := binding.Settings(); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("settings after restore=%+v", result)
	}
	if result := binding.UpdateSettings(desktopcontract.DesktopSettings{SchemaVersion: 1, Theme: desktopcontract.ThemeLight, ZoomPercent: 100}); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("update settings=%+v", result)
	}

	frontend := NewFrontendBridge(nil)
	frontend.attachNativeShell(native.Shell)
	frontend.persistMCPEnabled(true)
	if current := native.Shell.CurrentSettings(context.Background()); current.Outcome != protocolcommon.OutcomeSuccess || !current.Value.MCPEnabled {
		t.Fatalf("MCP enable not persisted: %+v", current)
	}
	frontend.persistMCPEnabled(false)
	if current := native.Shell.CurrentSettings(context.Background()); current.Outcome != protocolcommon.OutcomeSuccess || current.Value.MCPEnabled {
		t.Fatalf("MCP disable not persisted: %+v", current)
	}
}

// TestDurableCommitInvokeFailsClosedOnBadExchanges covers the wire guards in
// front of the app-level durable commit and the recents helper fallbacks.
func TestDurableCommitInvokeFailsClosedOnBadExchanges(t *testing.T) {
	if _, err := invokeDurableRuntimeCommit(context.Background(), nil, desktopcontract.Exchange{Blobs: []desktopcontract.Blob{{ID: "b", Bytes: []byte("x")}}}); err == nil {
		t.Fatal("blob-carrying commit exchange accepted")
	}
	if _, err := invokeDurableRuntimeCommit(context.Background(), nil, desktopcontract.Exchange{Control: []byte("{}")}); err == nil {
		t.Fatal("commit without an application accepted")
	}
	if _, err := invokeDurableRuntimeCommit(context.Background(), &desktopapp.Application{}, desktopcontract.Exchange{Control: []byte("not json")}); err == nil {
		t.Fatal("malformed commit control accepted")
	}
	if result := recentProjectsOf(nil); result.Outcome == protocolcommon.OutcomeSuccess {
		t.Fatalf("nil application listed recents: %+v", result)
	}
	runtime := conformanceRuntime{}
	runtime.ShowWindow(context.Background())
	runtime.Quit(context.Background())
	runtime.Emit(context.Background(), "event")
	if name, err := runtime.OpenFile(context.Background(), "", nil); err != nil || name != "" {
		t.Fatalf("conformance open file=%q err=%v", name, err)
	}
	if name, err := runtime.SaveFile(context.Background(), "", nil); err != nil || name != "" {
		t.Fatalf("conformance save file=%q err=%v", name, err)
	}
}

// TestSharedOwnerServesAuthoringReadsForOpenProject drives the trusted-owner
// read surface (subjects, generation, structure, open-session adoption)
// against a real open project.
func TestSharedOwnerServesAuthoringReadsForOpenProject(t *testing.T) {
	instance, err := newConformanceInstance(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.close(context.Background())
	opened, err := instance.openProject(context.Background(), conformanceProjectSource)
	if err != nil {
		t.Fatal(err)
	}
	session := opened.Open.Session
	subjects, err := instance.owner.ProjectSubjects(context.Background(), session)
	if err != nil || len(subjects) == 0 {
		t.Fatalf("subjects=%d err=%v", len(subjects), err)
	}
	generation, err := instance.owner.ProjectDocumentGeneration(context.Background(), session)
	if err != nil || generation.DocumentHandle.Value == "" {
		t.Fatalf("generation=%+v err=%v", generation, err)
	}
	structure, err := instance.owner.ProjectStructure(context.Background(), session)
	if err != nil || len(structure.Layers) != 1 || structure.Layers[0].ID != "app" {
		t.Fatalf("structure=%+v err=%v", structure.Layers, err)
	}
	bridge := NewFrontendBridge(instance.app, instance.owner)
	adopted, err := bridge.ProjectOpenSession(runtimeprotocol.OpenRuntimeDocumentInput{DocumentID: session.Scope.DocumentID})
	if err != nil || adopted.Session != session {
		t.Fatalf("adopted=%+v err=%v", adopted.Session, err)
	}
	viaBridge, err := bridge.ProjectStructure(session)
	if err != nil || len(viaBridge.Layers) != 1 {
		t.Fatalf("bridge structure=%+v err=%v", viaBridge.Layers, err)
	}
	materialized, err := bridge.MaterializeProjectView(session, "ldl:project:p:view:v")
	if err != nil || materialized.ViewDataHash == "" {
		t.Fatalf("materialize=%+v err=%v", materialized.ViewDataHash, err)
	}
	recents := bridge.RecentProjects()
	if recents.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("recents=%+v", recents)
	}
	if reopened := bridge.OpenRecentProject("document_unknown-recent-project"); reopened.Outcome == protocolcommon.OutcomeSuccess {
		t.Fatalf("unknown recent opened: %+v", reopened)
	}
	if inspected := bridge.InspectExternal("unknown-connection"); inspected.Outcome == protocolcommon.OutcomeSuccess {
		t.Fatalf("unknown connection inspected: %+v", inspected)
	}
	if serialized := bridge.SerializeNativeExport(nativeexport.SerializeInput{}); serialized.Outcome == protocolcommon.OutcomeSuccess {
		t.Fatalf("empty native export serialized: %+v", serialized)
	}
	if imported := bridge.ImportExternalDialog(desktopapp.ExternalImportRequest{}); imported.Outcome == protocolcommon.OutcomeSuccess {
		t.Fatalf("empty external import accepted: %+v", imported)
	}
	if created := bridge.CreateProjectDialog(""); created.Outcome == protocolcommon.OutcomeSuccess {
		t.Fatalf("dialogless project create succeeded: %+v", created)
	}
	if closed := bridge.CloseCurrentProject(); closed.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("close=%+v", closed)
	}
}

// TestPackagedConformanceScenariosProjectOpenAndCommit runs the project_open
// and commit packaged scenarios in-process so the durable authoring path the
// Desktop frontend depends on stays exercised by unit CI as well.
func TestPackagedConformanceScenariosProjectOpenAndCommit(t *testing.T) {
	for _, scenario := range []string{"project_open", "commit"} {
		var output bytes.Buffer
		if err := RunPackagedConformanceScenario(scenario, &output); err != nil {
			t.Fatalf("scenario %s: %v", scenario, err)
		}
		if !bytes.Contains(output.Bytes(), []byte(scenario)) {
			t.Fatalf("scenario %s report missing: %s", scenario, output.String())
		}
	}
}

// TestCompositionGuardsFailClosed covers the remaining fail-closed guards of
// the shared config store composition and the native interchange adapter.
func TestCompositionGuardsFailClosed(t *testing.T) {
	if _, err := NewNativeInterchangeAdapter(nil, "/tmp"); err == nil {
		t.Fatal("nil vault accepted")
	}
	if _, err := NewNativeInterchangeAdapter(&selectionVault{}, "relative"); err == nil {
		t.Fatal("relative root accepted")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "registry"), []byte("file"), 0o600); err == nil {
		if _, err := NewSharedConfig(root); err == nil {
			t.Fatal("registry-as-file accepted")
		}
	}
}

// TestRegistryDispatchWireGuardsFailClosed covers the typed Registry
// transport guards: malformed envelopes, undecodable requests, and dispatch
// without a bound Registry host all return closed wire failures.
func TestRegistryDispatchWireGuardsFailClosed(t *testing.T) {
	bridge := NewFrontendBridge(nil)
	for name, request := range map[string]string{
		"malformed json": "not json",
		"unknown op":     `{"wire_version":"1.0","operation":"registry.unknown","request_id":"request","input":{}}`,
		"nil registry":   `{"wire_version":"1.0","operation":"registry.list_sources","request_id":"request","input":{}}`,
	} {
		var response registry.WireResponse
		if err := json.Unmarshal([]byte(bridge.RegistryDispatch(request)), &response); err != nil {
			t.Fatalf("%s: response not decodable: %v", name, err)
		}
		if response.Failure == nil || !response.Failure.Actionable {
			t.Fatalf("%s: expected actionable wire failure, got %+v", name, response)
		}
	}
}

// TestRunPackagedConformanceScenarioRejectsInvalidInvocation covers the
// scenario runner guard branches.
func TestRunPackagedConformanceScenarioRejectsInvalidInvocation(t *testing.T) {
	var output bytes.Buffer
	if err := RunPackagedConformanceScenario("does_not_exist", &output); err == nil {
		t.Fatal("unknown scenario accepted")
	}
	if err := RunPackagedConformanceScenario("project_open", nil); err == nil {
		t.Fatal("nil output accepted")
	}
}

type fixedCredentialPort struct {
	result desktopcontract.Result[[]byte]
}

func (p fixedCredentialPort) Resolve(context.Context, desktopcontract.CredentialRef) desktopcontract.Result[[]byte] {
	return p.result
}

// TestSharedAdaptersFailClosedWithoutBackends covers the Review runtime,
// Registry staged-object reader, credential resolver, and Review proposal
// adapters when their backing services are absent or invalid.
func TestSharedAdaptersFailClosedWithoutBackends(t *testing.T) {
	ctx := context.Background()
	review := desktopReviewRuntime{owner: &sharedOwner{}}
	if _, err := review.Repreview(ctx, reviewapp.RepreviewInput{}); err == nil {
		t.Fatal("Repreview without a local document succeeded")
	}
	if _, err := review.Commit(ctx, runtimeprotocol.RuntimeCommitInput{}); err == nil {
		t.Fatal("Commit without an application succeeded")
	}
	if _, err := (registryObjectReader{}).OpenRegistryStagedObject(ctx, runtimeport.RegistryStagedObjectRef{Size: "not-a-size"}); err == nil {
		t.Fatal("invalid staged object size accepted")
	}
	denied := registryCredentialResolver{port: fixedCredentialPort{result: desktopcontract.Result[[]byte]{Outcome: protocolcommon.OutcomeFailed}}}
	if _, err := denied.ResolveCredential(ctx, "credential"); err == nil {
		t.Fatal("failed credential resolution succeeded")
	}
	granted := registryCredentialResolver{port: fixedCredentialPort{result: desktopcontract.Result[[]byte]{Outcome: protocolcommon.OutcomeSuccess, Value: []byte("secret")}}}
	if value, err := granted.ResolveCredential(ctx, "credential"); err != nil || string(value) != "secret" {
		t.Fatalf("credential=%q err=%v", value, err)
	}
	if _, err := reviewProposal(nil, errors.New("upstream")); err == nil {
		t.Fatal("upstream review error swallowed")
	}
	if _, err := reviewProposal("not a proposal", nil); err == nil {
		t.Fatal("mismatched review proposal accepted")
	}
}

// TestPackagedProbeRejectsInvalidActionAndStateKey covers the probe guard
// branches for unknown actions and malformed state keys.
func TestPackagedProbeRejectsInvalidActionAndStateKey(t *testing.T) {
	t.Setenv("LAYERDRAW_DESKTOP_PROBE_ACTION", "nonsense")
	if _, err := executePackagedProbe(); err == nil {
		t.Fatal("invalid probe action accepted")
	}
	t.Setenv("LAYERDRAW_DESKTOP_PROBE_ACTION", "initialize")
	t.Setenv("LAYERDRAW_DESKTOP_PROBE_STATE_KEY", "zz")
	if _, err := executePackagedProbe(); err == nil {
		t.Fatal("invalid probe state key accepted")
	}
}

// TestNativeInterchangeAdapterGuardsFailClosed covers the staged-artifact and
// import guards of the native interchange adapter.
func TestNativeInterchangeAdapterGuardsFailClosed(t *testing.T) {
	ctx := context.Background()
	vault := newSelectionVault()
	adapter, err := NewNativeInterchangeAdapter(vault, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if len(adapter.Profiles()) == 0 {
		t.Fatal("no native export profiles")
	}
	destination := filepath.Join(t.TempDir(), "export.json")
	token, err := vault.issue(destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Publish(ctx, token, "unknown-artifact"); err == nil {
		t.Fatal("unknown staged artifact published")
	}
	if _, err := adapter.Import(ctx, "unknown-token", nativeexport.OperationsJSONProfile); err == nil {
		t.Fatal("import with unknown token succeeded")
	}
	directory := t.TempDir()
	token, err = vault.issue(directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Import(ctx, token, nativeexport.OperationsJSONProfile); err == nil {
		t.Fatal("directory import selection accepted")
	}
	source := filepath.Join(t.TempDir(), "operations.json")
	if err := os.WriteFile(source, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err = vault.issue(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Import(ctx, token, "unsupported-profile"); err == nil {
		t.Fatal("unsupported import profile accepted")
	}
	if err := adapter.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestNewSharedConfigFailsClosedOnBrokenStateRoot covers the composition
// error branches when registry or external storage roots are unusable.
func TestNewSharedConfigFailsClosedOnBrokenStateRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "external-storage-reference"), []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSharedConfig(root); err == nil {
		t.Fatal("external storage over a file accepted")
	}
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "registry", "objects"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "registry", "transactions"), []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSharedConfig(root); err == nil {
		t.Fatal("transactions store over a file accepted")
	}
}
