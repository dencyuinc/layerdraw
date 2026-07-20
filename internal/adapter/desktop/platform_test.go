// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/dencyuinc/layerdraw/internal/privatefs"
)

func adapterShellState() desktopcontract.PersistedShellState {
	return desktopcontract.PersistedShellState{
		Settings: desktopcontract.DesktopSettings{SchemaVersion: 1, Theme: desktopcontract.ThemeSystem, ZoomPercent: 100},
		Window:   desktopcontract.WindowState{SchemaVersion: 1, Bounds: desktopcontract.Rectangle{X: 20, Y: 30, Width: 1200, Height: 800}},
	}
}

func TestAtomicSettingsStoreRoundTripsPrivateState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "settings-v1.json")
	store, err := NewAtomicSettingsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	value := adapterShellState()
	if err := store.Save(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background())
	if err != nil || !reflect.DeepEqual(loaded, value) {
		t.Fatalf("load=%+v err=%v", loaded, err)
	}
	info, err := os.Stat(path)
	if err != nil || !privatefs.PermissionsMatch(info, 0o600) {
		t.Fatalf("settings permissions=%v err=%v", info.Mode().Perm(), err)
	}
}

func TestAtomicSettingsStoreRejectsUnsafeAndCorruptFiles(t *testing.T) {
	if _, err := NewAtomicSettingsStore("relative.json"); err == nil {
		t.Fatal("relative settings path accepted")
	}
	root := t.TempDir()
	target := filepath.Join(root, "target.json")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(root, "settings.json")
	if err := os.Symlink(target, symlink); err != nil {
		t.Fatal(err)
	}
	store, err := NewAtomicSettingsStore(symlink)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background()); err == nil {
		t.Fatal("settings symlink accepted")
	}
	if err := store.Save(context.Background(), adapterShellState()); err == nil {
		t.Fatal("settings symlink overwritten")
	}
	corrupt, _ := NewAtomicSettingsStore(filepath.Join(root, "corrupt.json"))
	if err := os.WriteFile(corrupt.path, []byte("{\"unknown\":true}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := corrupt.Load(context.Background()); err == nil {
		t.Fatal("unknown settings field accepted")
	}
	if err := corrupt.Save(context.Background(), desktopcontract.PersistedShellState{}); err == nil {
		t.Fatal("invalid settings state persisted")
	}
}

type runnerCall struct {
	name string
	args []string
}
type recordingRunner struct{ calls []runnerCall }

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) error {
	r.calls = append(r.calls, runnerCall{name: name, args: append([]string(nil), args...)})
	return nil
}

func TestSystemExternalOpenerUsesFixedExecutablesAndArguments(t *testing.T) {
	target := desktopcontract.ExternalTarget{Kind: desktopcontract.ExternalWebLink, Value: "https://layerdraw.com/docs"}
	tests := []struct {
		platform desktopcontract.DesktopPlatform
		name     string
		prefix   []string
	}{
		{desktopcontract.PlatformMacOS, "/usr/bin/open", []string{"--"}},
		{desktopcontract.PlatformWindows, `C:\Windows\System32\rundll32.exe`, []string{"url.dll,FileProtocolHandler"}},
		{desktopcontract.PlatformLinux, "/usr/bin/xdg-open", nil},
	}
	for _, test := range tests {
		runner := &recordingRunner{}
		opener, err := newSystemExternalOpener(test.platform, runner)
		if err != nil {
			t.Fatal(err)
		}
		if err := opener.OpenExternal(context.Background(), target); err != nil {
			t.Fatal(err)
		}
		wantArgs := append(append([]string(nil), test.prefix...), target.Value)
		if len(runner.calls) != 1 || runner.calls[0].name != test.name || !reflect.DeepEqual(runner.calls[0].args, wantArgs) {
			t.Fatalf("%s call=%+v", test.platform, runner.calls)
		}
	}
	runner := &recordingRunner{}
	opener, _ := newSystemExternalOpener(desktopcontract.PlatformLinux, runner)
	if err := opener.OpenExternal(context.Background(), desktopcontract.ExternalTarget{Kind: desktopcontract.ExternalWebLink, Value: "file:///tmp/private"}); err == nil || len(runner.calls) != 0 {
		t.Fatal("unsafe target reached operating system")
	}
	if opener, err := NewSystemExternalOpener(desktopcontract.PlatformMacOS); err != nil || opener.platform != desktopcontract.PlatformMacOS {
		t.Fatalf("production opener=%+v err=%v", opener, err)
	}
	if _, err := newSystemExternalOpener("other", runner); err == nil {
		t.Fatal("unsupported platform accepted")
	}
}

type runtimeBridgeStub struct {
	displays []desktopcontract.Display
	window   desktopcontract.WindowState
	settings desktopcontract.DesktopSettings
	report   desktopcontract.AccessibilityReport
}

func (b *runtimeBridgeStub) Displays(context.Context) ([]desktopcontract.Display, error) {
	return b.displays, nil
}
func (b *runtimeBridgeStub) Snapshot(context.Context) (desktopcontract.WindowState, desktopcontract.DesktopSettings, error) {
	return b.window, b.settings, nil
}
func (b *runtimeBridgeStub) ApplyWindow(_ context.Context, value desktopcontract.WindowState) error {
	b.window = value
	return nil
}
func (b *runtimeBridgeStub) ApplySettings(_ context.Context, value desktopcontract.DesktopSettings) error {
	b.settings = value
	return nil
}
func (b *runtimeBridgeStub) VerifyPackagedAccessibility(context.Context, desktopcontract.AccessibilityProfile) (desktopcontract.AccessibilityReport, error) {
	return b.report, nil
}

func TestWailsRuntimeAdapterDelegatesNativeAndPackagedOperations(t *testing.T) {
	bridge := &runtimeBridgeStub{displays: []desktopcontract.Display{{ID: "primary", Primary: true, Work: desktopcontract.Rectangle{Width: 1920, Height: 1080}}}, window: adapterShellState().Window, settings: adapterShellState().Settings, report: desktopcontract.AccessibilityReport{LabelsComplete: true}}
	adapter, err := NewWailsRuntimeAdapter(bridge)
	if err != nil {
		t.Fatal(err)
	}
	if displays, err := adapter.Displays(context.Background()); err != nil || len(displays) != 1 {
		t.Fatalf("displays=%+v err=%v", displays, err)
	}
	if window, settings, err := adapter.Snapshot(context.Background()); err != nil || window != bridge.window || settings != bridge.settings {
		t.Fatalf("snapshot=%+v %+v err=%v", window, settings, err)
	}
	next := adapterShellState().Window
	next.Maximized = true
	if err := adapter.ApplyWindow(context.Background(), next); err != nil || bridge.window != next {
		t.Fatalf("window=%+v err=%v", bridge.window, err)
	}
	nextSettings := adapterShellState().Settings
	nextSettings.ZoomPercent = 150
	if err := adapter.ApplySettings(context.Background(), nextSettings); err != nil || bridge.settings != nextSettings {
		t.Fatalf("settings=%+v err=%v", bridge.settings, err)
	}
	if report, err := adapter.VerifyPackaged(context.Background(), desktopcontract.AccessibilityProfile{Platform: desktopcontract.PlatformMacOS, ZoomPercent: 100}); err != nil || !report.LabelsComplete {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	if _, err := NewWailsRuntimeAdapter(nil); err == nil {
		t.Fatal("nil Wails runtime accepted")
	}
}

func TestAssociationBrokerReturnsOpaqueSingleUseTokens(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "private-project.ldl")
	if err := os.WriteFile(path, []byte("project x {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	broker := NewAssociationBroker()
	if err := broker.AcceptOSPath(path); err != nil {
		t.Fatal(err)
	}
	handoff, err := broker.Next(context.Background())
	if err != nil || handoff.Kind != desktopcontract.FileAssociationLDL || strings.Contains(handoff.Token, "/") || strings.Contains(handoff.Token, "private-project") {
		t.Fatalf("handoff=%+v err=%v", handoff, err)
	}
	resolved, err := broker.Resolve(handoff.Token)
	if err != nil || resolved != path {
		t.Fatalf("resolve=%q err=%v", resolved, err)
	}
	if _, err := broker.Resolve(handoff.Token); err == nil {
		t.Fatal("association token replayed")
	}
	link := filepath.Join(root, "linked.ldl")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := broker.AcceptOSPath(link); err == nil {
		t.Fatal("association symlink accepted")
	}
	replaced := filepath.Join(root, "replaced.ldl")
	if err := os.WriteFile(replaced, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := broker.AcceptOSPath(replaced); err != nil {
		t.Fatal(err)
	}
	replacedHandoff, err := broker.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(replaced); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replaced, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := broker.Resolve(replacedHandoff.Token); err == nil {
		t.Fatal("replaced association target accepted")
	}
	oversized := filepath.Join(root, "oversized.ldl")
	oversizedFile, err := os.OpenFile(oversized, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := oversizedFile.Truncate(maximumAssociationBytes + 1); err != nil {
		_ = oversizedFile.Close()
		t.Fatal(err)
	}
	if err := oversizedFile.Close(); err != nil {
		t.Fatal(err)
	}
	if err := broker.AcceptOSPath(oversized); err == nil {
		t.Fatal("oversized association target accepted")
	}
	unsupported := filepath.Join(root, "project.txt")
	if err := os.WriteFile(unsupported, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := broker.AcceptOSPath(unsupported); err == nil {
		t.Fatal("unsupported association accepted")
	}
	if _, err := broker.Next(context.Background()); err == nil {
		t.Fatal("empty association queue succeeded")
	}
}

func TestJSONLogStoreWritesOnlyStructuredRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "desktop.jsonl")
	store, err := NewJSONLogStore(path)
	if err != nil {
		t.Fatal(err)
	}
	code := desktopcontract.FailureSettings
	record := desktopcontract.StructuredLogRecord{At: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), Level: desktopcontract.LogError, Event: desktopcontract.EventOperationFailed, Platform: desktopcontract.PlatformLinux, Failure: &code}
	if err := store.Write(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.Count(string(data), "\n") != 1 || strings.Contains(string(data), "private path") {
		t.Fatalf("log=%q err=%v", data, err)
	}
	info, err := os.Stat(path)
	if err != nil || !privatefs.PermissionsMatch(info, 0o600) {
		t.Fatalf("log permissions=%v err=%v", info.Mode().Perm(), err)
	}
	if _, err := NewJSONLogStore("relative.log"); err == nil {
		t.Fatal("relative log path accepted")
	}
	target := filepath.Join(t.TempDir(), "target.log")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(target), "linked.log")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	linked, _ := NewJSONLogStore(link)
	if err := linked.Write(context.Background(), record); err == nil {
		t.Fatal("log symlink accepted")
	}
}

func TestAdapterErrorsRemainPrivate(t *testing.T) {
	runner := &failingRunner{}
	opener, _ := newSystemExternalOpener(desktopcontract.PlatformLinux, runner)
	if err := opener.OpenExternal(context.Background(), desktopcontract.ExternalTarget{Kind: desktopcontract.ExternalWebLink, Value: "https://layerdraw.com"}); !errors.Is(err, runner.err) {
		t.Fatalf("runner error=%v", err)
	}
}

type failingRunner struct{ err error }

func (r *failingRunner) Run(context.Context, string, ...string) error {
	if r.err == nil {
		r.err = errors.New("private OS failure")
	}
	return r.err
}
