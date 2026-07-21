// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/localdocument"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type dialogHarness struct {
	mu      sync.Mutex
	results []desktopcontract.Result[desktopcontract.DialogSelection]
	seen    []desktopcontract.DialogRequest
}

func (d *dialogHarness) Select(_ context.Context, request desktopcontract.DialogRequest) desktopcontract.Result[desktopcontract.DialogSelection] {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seen = append(d.seen, request)
	if len(d.results) == 0 {
		return cancelled[desktopcontract.DialogSelection](desktopcontract.ComponentBindingShell)
	}
	result := d.results[0]
	d.results = d.results[1:]
	return result
}

type lifecycleStorageHarness struct {
	locations map[string]ProjectLocation
	errs      map[string]error
}

func (s *lifecycleStorageHarness) resolve(token string) (ProjectLocation, error) {
	if err := s.errs[token]; err != nil {
		return ProjectLocation{}, err
	}
	value, ok := s.locations[token]
	if !ok {
		return ProjectLocation{}, os.ErrNotExist
	}
	return value, nil
}
func (s *lifecycleStorageHarness) Create(_ context.Context, token string) (ProjectLocation, error) {
	return s.resolve(token)
}
func (s *lifecycleStorageHarness) Open(_ context.Context, token string) (ProjectLocation, error) {
	return s.resolve(token)
}
func (s *lifecycleStorageHarness) Import(_ context.Context, token string) (ProjectLocation, error) {
	return s.resolve(token)
}
func (s *lifecycleStorageHarness) Relocate(_ context.Context, _ runtimeprotocol.DocumentID, token string) (ProjectLocation, error) {
	return s.resolve(token)
}

type externalLifecycleHarness struct{ reconcile bool }

func (e *externalLifecycleHarness) Connect(_ context.Context, request ExternalConnectionRequest) desktopcontract.Result[ExternalConnection] {
	return desktopcontract.Result[ExternalConnection]{Outcome: protocolcommon.OutcomeSuccess, Value: ExternalConnection{ConnectionID: "connection-1", ProviderID: request.ProviderID}}
}

type lifecycleWindowHarness struct {
	shows, closes int
	panicClose    bool
}

func (w *lifecycleWindowHarness) Show(context.Context) error { w.shows++; return nil }
func (w *lifecycleWindowHarness) RequestClose(context.Context) error {
	if w.panicClose {
		panic("private window panic")
	}
	w.closes++
	return nil
}

type panicExternalLifecycle struct{}

func (panicExternalLifecycle) Connect(context.Context, ExternalConnectionRequest) desktopcontract.Result[ExternalConnection] {
	panic("private provider panic")
}
func (panicExternalLifecycle) Sync(context.Context, ExternalSyncRequest) desktopcontract.Result[ExternalSyncResult] {
	panic("private provider panic")
}
func (panicExternalLifecycle) Reconcile(context.Context, ExternalReconcileRequest) desktopcontract.Result[ExternalReconcileResult] {
	panic("private provider panic")
}

type invalidExternalLifecycle struct{}

func (invalidExternalLifecycle) Connect(context.Context, ExternalConnectionRequest) desktopcontract.Result[ExternalConnection] {
	return desktopcontract.Result[ExternalConnection]{Outcome: protocolcommon.OutcomeSuccess, Value: ExternalConnection{ConnectionID: "connection", ProviderID: "wrong"}}
}
func (invalidExternalLifecycle) Sync(context.Context, ExternalSyncRequest) desktopcontract.Result[ExternalSyncResult] {
	return desktopcontract.Result[ExternalSyncResult]{Outcome: protocolcommon.OutcomeSuccess}
}
func (invalidExternalLifecycle) Reconcile(context.Context, ExternalReconcileRequest) desktopcontract.Result[ExternalReconcileResult] {
	return desktopcontract.Result[ExternalReconcileResult]{Outcome: protocolcommon.OutcomeSuccess}
}

type panicDialog struct{}

func (panicDialog) Select(context.Context, desktopcontract.DialogRequest) desktopcontract.Result[desktopcontract.DialogSelection] {
	panic("private dialog panic")
}
func (e *externalLifecycleHarness) Sync(_ context.Context, _ ExternalSyncRequest) desktopcontract.Result[ExternalSyncResult] {
	return desktopcontract.Result[ExternalSyncResult]{Outcome: protocolcommon.OutcomeSuccess, Value: ExternalSyncResult{ProviderVersion: "2", ReconcileNeeded: true}}
}
func (e *externalLifecycleHarness) Reconcile(_ context.Context, _ ExternalReconcileRequest) desktopcontract.Result[ExternalReconcileResult] {
	return desktopcontract.Result[ExternalReconcileResult]{Outcome: protocolcommon.OutcomeSuccess, Value: ExternalReconcileResult{ProviderVersion: "3", Converged: e.reconcile}}
}

func selected(token string) desktopcontract.Result[desktopcontract.DialogSelection] {
	return desktopcontract.Result[desktopcontract.DialogSelection]{Outcome: protocolcommon.OutcomeSuccess, Value: desktopcontract.DialogSelection{Token: token}}
}

func cancelledSelection() desktopcontract.Result[desktopcontract.DialogSelection] {
	return cancelled[desktopcontract.DialogSelection](desktopcontract.ComponentBindingShell)
}

func startLifecycleApp(t *testing.T, root, project string, dialogs *dialogHarness, storage *lifecycleStorageHarness) *Application {
	t.Helper()
	config := testConfig(t, root, project)
	config.Dialogs = dialogs
	config.ProjectStorage = storage
	app, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if result := app.Start(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("start=%+v", result)
	}
	return app
}

func TestNativeDialogsPreserveCancelAndImportThroughOpaqueTokens(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root)
	archive, err := engine.New(engine.BuildInfo{}).WriteLayerdraw(context.Background(), engine.LayerdrawWriteInput{CompileInput: engine.CompileInput{Mode: engine.CompileProject, EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte("project imported \"Imported\" {}\n")}, ResolvedDependencies: engine.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	container := filepath.Join(root, "import.layerdraw")
	if err := os.WriteFile(container, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	dialogs := &dialogHarness{results: []desktopcontract.Result[desktopcontract.DialogSelection]{cancelledSelection(), selected("create"), selected("import")}}
	storage := &lifecycleStorageHarness{locations: map[string]ProjectLocation{
		"create": {Root: project, EntryPath: "document.ldl"},
		"import": {Root: container, Kind: "container"},
	}, errs: map[string]error{}}
	app := startLifecycleApp(t, root, project, dialogs, storage)
	if result := app.OpenProjectDialog(context.Background(), "open-1"); result.Outcome != protocolcommon.OutcomeCancelled || result.Failure == nil || result.Failure.Code != desktopcontract.FailureDialogCancelled {
		t.Fatalf("cancel=%+v", result)
	}
	created := app.CreateProjectDialog(context.Background(), "create-1")
	if created.Outcome != protocolcommon.OutcomeSuccess || created.Value.ProjectID == "" {
		t.Fatalf("create=%+v", created)
	}
	imported := app.ImportProjectDialog(context.Background(), "import-1")
	if imported.Outcome != protocolcommon.OutcomeSuccess || imported.Value.ProjectID == "" || imported.Value.ProjectID == created.Value.ProjectID {
		t.Fatalf("import=%+v", imported)
	}
	if len(dialogs.seen) != 3 || dialogs.seen[0].Kind != desktopcontract.DialogOpenProject || dialogs.seen[1].Kind != desktopcontract.DialogCreateProject || dialogs.seen[2].Kind != desktopcontract.DialogImport {
		t.Fatalf("dialogs=%+v", dialogs.seen)
	}
	if result := app.Shutdown(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("shutdown=%+v", result)
	}
}

func TestRecentPinDuplicateMissingRelocateAndPathRedaction(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root)
	dialogs := &dialogHarness{}
	storage := &lifecycleStorageHarness{locations: map[string]ProjectLocation{"open": {Root: project, EntryPath: "document.ldl"}}, errs: map[string]error{}}
	app := startLifecycleApp(t, root, project, dialogs, storage)
	first := app.OpenProject(context.Background(), "open")
	second := app.OpenProject(context.Background(), "open")
	if first.Outcome != protocolcommon.OutcomeSuccess || second.Value.Disposition != ProjectFocused || second.Value.Open.Session != first.Value.Open.Session {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	if pinned := app.PinProject(first.Value.ProjectID, true); pinned.Outcome != protocolcommon.OutcomeSuccess || len(pinned.Value) != 1 || !pinned.Value[0].Pinned {
		t.Fatalf("pin=%+v", pinned)
	}
	if closed := app.CloseProject(context.Background(), first.Value.Open.Session); closed.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("close=%+v", closed)
	}
	moved := filepath.Join(root, "moved")
	if err := os.Rename(project, moved); err != nil {
		t.Fatal(err)
	}
	missing := app.OpenRecentProject(context.Background(), first.Value.ProjectID)
	encoded, _ := json.Marshal(missing)
	if missing.Failure == nil || missing.Failure.Code != desktopcontract.FailureProjectMissing || stringContains(string(encoded), project) {
		t.Fatalf("missing=%s", encoded)
	}
	storage.locations["moved"] = ProjectLocation{Root: moved, EntryPath: "document.ldl"}
	relocated := app.RelocateProject(context.Background(), first.Value.ProjectID, "moved")
	if relocated.Outcome != protocolcommon.OutcomeSuccess || relocated.Value.ProjectID != first.Value.ProjectID {
		t.Fatalf("relocate=%+v", relocated)
	}
	if closed := app.CloseProject(context.Background(), relocated.Value.Open.Session); closed.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("close relocated=%+v", closed)
	}
	storage.errs["denied"] = os.ErrPermission
	denied := app.RelocateProject(context.Background(), first.Value.ProjectID, "denied")
	encoded, _ = json.Marshal(denied)
	if denied.Failure == nil || denied.Failure.Code != desktopcontract.FailurePermissionDenied || stringContains(string(encoded), moved) {
		t.Fatalf("denied=%s", encoded)
	}
	_ = app.Shutdown(context.Background())
}

func TestCloseDistinguishesPreviewEphemeralAutosaveAndProviderReconcile(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root)
	storage := &lifecycleStorageHarness{locations: map[string]ProjectLocation{"open": {Root: project, EntryPath: "document.ldl"}}, errs: map[string]error{}}
	app := startLifecycleApp(t, root, project, &dialogHarness{}, storage)
	opened := app.OpenProject(context.Background(), "open")
	if focused := app.OpenRecentProject(context.Background(), opened.Value.ProjectID); focused.Outcome != protocolcommon.OutcomeSuccess || focused.Value.Disposition != ProjectFocused {
		t.Fatalf("focused recent=%+v", focused)
	}
	if missing := app.OpenRecentProject(context.Background(), "document_unknown"); missing.Failure == nil || missing.Failure.Code != desktopcontract.FailureProjectMissing {
		t.Fatalf("missing recent=%+v", missing)
	}
	app.config.ProjectStorage = storageStub{ProjectLocation{Root: project, EntryPath: "document.ldl"}}
	if unavailable := app.ImportProjectDialog(context.Background(), "import"); unavailable.Failure == nil || unavailable.Failure.Code != desktopcontract.FailureAdapterUnavailable {
		t.Fatalf("missing import adapter=%+v", unavailable)
	}
	app.config.ProjectStorage = storage
	batch, err := engineprotocol.DecodeSemanticOperationBatch([]byte(`{"operations":[{"operation":"create_subject","subject_kind":"layer","parent_address":"ldl:project:p","id":"draft","fields":{"display_name":"Draft","order":"1"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	commit := runtimeprotocol.RuntimeCommitInput{Session: opened.Value.Open.Session, OperationID: "desktop_close_preview", IdempotencyKey: "desktop_close_preview_idem", OperationBatch: runtimeprotocol.RuntimeOperationBatch{DocumentID: opened.Value.ProjectID, BaseRevision: opened.Value.Open.CommittedRevision, ExpectedDefinitionHash: opened.Value.Open.CommittedRevision.DefinitionHash, Operations: batch, Preconditions: preconditionsFor(t, "project p \"P\" {}\n")}}
	preview := app.Preview(context.Background(), runtimeprotocol.PreviewOperationsInput{Session: commit.Session, OperationBatch: commit.OperationBatch})
	if preview.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("preview=%+v", preview)
	}
	assessment := app.PrepareClose(commit.Session)
	if assessment.Value.CanClose || len(assessment.Value.Blockers) != 2 || assessment.Value.Blockers[0] != ClosePendingPreview || assessment.Value.Blockers[1] != CloseEphemeralEdits {
		t.Fatalf("assessment=%+v", assessment)
	}
	if close := app.CloseProject(context.Background(), commit.Session); close.Failure == nil || close.Failure.Code != desktopcontract.FailureReconcilePending {
		t.Fatalf("unsafe close=%+v", close)
	}
	autosave := app.ControlAutosave(context.Background(), runtimeprotocol.AutosaveControlInput{Action: runtimeprotocol.AutosaveActionSchedule, Session: commit.Session, Commit: &commit})
	if autosave.Outcome != protocolcommon.OutcomeSuccess || !autosave.Value.Scheduled {
		t.Fatalf("autosave=%+v", autosave)
	}
	assessment = app.PrepareClose(commit.Session)
	if len(assessment.Value.Blockers) != 3 || assessment.Value.Blockers[2] != CloseAutosavePending {
		t.Fatalf("autosave assessment=%+v", assessment)
	}
	resolved := app.ResolveClose(context.Background(), commit.Session, CloseCancelAutosaveDiscard)
	if resolved.Outcome != protocolcommon.OutcomeSuccess || !resolved.Value.Closed {
		t.Fatalf("resolve=%+v", resolved)
	}

	opened = app.OpenProject(context.Background(), "open")
	external := &externalLifecycleHarness{reconcile: false}
	app.config.ExternalLifecycle = external
	syncResult := app.SyncExternal(context.Background(), opened.Value.Open.Session, ExternalSyncRequest{ConnectionID: "connection-1", DocumentID: opened.Value.ProjectID, Revision: opened.Value.Open.CommittedRevision})
	if syncResult.Outcome != protocolcommon.OutcomeSuccess || !syncResult.Value.ReconcileNeeded {
		t.Fatalf("sync=%+v", syncResult)
	}
	if result := app.ResolveClose(context.Background(), opened.Value.Open.Session, CloseDiscardEphemeral); result.Failure == nil || result.Failure.Code != desktopcontract.FailureReconcilePending {
		t.Fatalf("pending reconcile closed=%+v", result)
	}
	external.reconcile = true
	reconciled := app.ReconcileExternal(context.Background(), opened.Value.Open.Session, ExternalReconcileRequest{ConnectionID: "connection-1", DocumentID: opened.Value.ProjectID, Resolution: "accept_authoritative"})
	if reconciled.Outcome != protocolcommon.OutcomeSuccess || !reconciled.Value.Converged {
		t.Fatalf("reconcile=%+v", reconciled)
	}
	if close := app.CloseProject(context.Background(), opened.Value.Open.Session); close.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("close=%+v", close)
	}
	_ = app.Shutdown(context.Background())
}

func TestCrashRecoveryRequiresExplicitRestoreOrDiscardAcrossRestart(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root)
	storage := &lifecycleStorageHarness{locations: map[string]ProjectLocation{"open": {Root: project, EntryPath: "document.ldl"}}, errs: map[string]error{}}
	crashed := startLifecycleApp(t, root, project, &dialogHarness{}, storage)
	opened := crashed.OpenProject(context.Background(), "open")
	if opened.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatal(opened.Failure)
	}
	if dirty := crashed.SetEphemeralState(EphemeralStateInput{Session: opened.Value.Open.Session, Dirty: true, Recovery: &RecoveryArtifact{Kind: RecoveryEditorState, Payload: json.RawMessage(`{"selection":"layer-1"}`)}}); dirty.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("persist recovery payload=%+v", dirty)
	}

	restarted := startLifecycleApp(t, root, project, &dialogHarness{}, storage)
	candidates := restarted.RecoveryCandidates()
	if candidates.Outcome != protocolcommon.OutcomeSuccess || len(candidates.Value) != 1 || candidates.Value[0].ProjectID != opened.Value.ProjectID {
		t.Fatalf("candidates=%+v", candidates)
	}
	if bypass := restarted.OpenRecentProject(context.Background(), opened.Value.ProjectID); bypass.Failure == nil || bypass.Failure.Code != desktopcontract.FailureRecoveryRequired {
		t.Fatalf("recovery bypass=%+v", bypass)
	}
	restored := restarted.ResolveRecovery(context.Background(), opened.Value.ProjectID, RecoveryRestore)
	if restored.Outcome != protocolcommon.OutcomeSuccess || restored.Value.Disposition != ProjectRestored || restored.Value.Recovery == nil || string(restored.Value.Recovery.Payload) != `{"selection":"layer-1"}` {
		t.Fatalf("restore=%+v", restored)
	}
	if candidates = restarted.RecoveryCandidates(); len(candidates.Value) != 0 {
		t.Fatalf("restored candidate remained=%+v", candidates)
	}
	if close := restarted.ResolveClose(context.Background(), restored.Value.Open.Session, CloseDiscardEphemeral); close.Outcome != protocolcommon.OutcomeSuccess || !close.Value.Closed {
		t.Fatalf("close=%+v", close)
	}
	_ = restarted.Shutdown(context.Background())

	secondRoot := t.TempDir()
	secondProject := writeProject(t, secondRoot)
	secondStorage := &lifecycleStorageHarness{locations: map[string]ProjectLocation{"open": {Root: secondProject, EntryPath: "document.ldl"}}, errs: map[string]error{}}
	crashedAgain := startLifecycleApp(t, secondRoot, secondProject, &dialogHarness{}, secondStorage)
	secondOpened := crashedAgain.OpenProject(context.Background(), "open")
	discarder := startLifecycleApp(t, secondRoot, secondProject, &dialogHarness{}, secondStorage)
	discarded := discarder.ResolveRecovery(context.Background(), secondOpened.Value.ProjectID, RecoveryDiscard)
	if discarded.Outcome != protocolcommon.OutcomeSuccess || len(discarder.RecoveryCandidates().Value) != 0 {
		t.Fatalf("discard=%+v", discarded)
	}
	_ = discarder.Shutdown(context.Background())
	_ = crashed
	_ = crashedAgain
}

func TestExplicitSaveRejectsStaleAuthoritativeRevision(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root)
	storage := &lifecycleStorageHarness{locations: map[string]ProjectLocation{"open": {Root: project, EntryPath: "document.ldl"}}, errs: map[string]error{}}
	app := startLifecycleApp(t, root, project, &dialogHarness{}, storage)
	opened := app.OpenProject(context.Background(), "open")
	batch, err := engineprotocol.DecodeSemanticOperationBatch([]byte(`{"operations":[{"operation":"create_subject","subject_kind":"layer","parent_address":"ldl:project:p","id":"stale","fields":{"display_name":"Stale","order":"1"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	stale := opened.Value.Open.CommittedRevision
	stale.RevisionID = "revision_stale"
	input := runtimeprotocol.RuntimeCommitInput{Session: opened.Value.Open.Session, OperationID: "stale_save", IdempotencyKey: "stale_save_idem", OperationBatch: runtimeprotocol.RuntimeOperationBatch{DocumentID: opened.Value.ProjectID, BaseRevision: stale, ExpectedDefinitionHash: stale.DefinitionHash, Operations: batch, Preconditions: preconditionsFor(t, "project p \"P\" {}\n")}}
	result := app.Commit(context.Background(), input)
	if result.Failure == nil || result.Failure.Code != desktopcontract.FailureProjectConflict || result.Failure.Recovery != desktopcontract.RecoveryReview {
		t.Fatalf("stale save=%+v", result)
	}
	if assessment := app.PrepareClose(opened.Value.Open.Session); !assessment.Value.CanClose {
		t.Fatalf("stale save dirtied durable session=%+v", assessment)
	}
	_ = app.CloseProject(context.Background(), opened.Value.Open.Session)
	_ = app.Shutdown(context.Background())
}

func TestStorageFailureClassificationDoesNotLeakAdapterErrors(t *testing.T) {
	secret := "/Users/example/private/secret-project"
	denied := mapStorageFailure[string](&os.PathError{Op: "open", Path: secret, Err: os.ErrPermission})
	encoded, err := json.Marshal(denied)
	if err != nil {
		t.Fatal(err)
	}
	if denied.Failure == nil || denied.Failure.Code != desktopcontract.FailurePermissionDenied || stringContains(string(encoded), secret) {
		t.Fatalf("failure leaked=%s", encoded)
	}
	missing := mapStorageFailure[string](&os.PathError{Op: "open", Path: secret, Err: os.ErrNotExist})
	if missing.Failure == nil || missing.Failure.Code != desktopcontract.FailureProjectMissing {
		t.Fatalf("missing=%+v", missing)
	}
	other := mapStorageFailure[string](errors.New(secret))
	encoded, _ = json.Marshal(other)
	if other.Failure == nil || stringContains(string(encoded), secret) {
		t.Fatalf("adapter failure leaked=%s", encoded)
	}
}

func TestLifecycleControlSurfaceAndExternalAdapterFailuresAreClosed(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root)
	storage := &lifecycleStorageHarness{locations: map[string]ProjectLocation{"open": {Root: project, EntryPath: "document.ldl"}}, errs: map[string]error{}}
	config := testConfig(t, root, project)
	config.ProjectStorage = storage
	window := &lifecycleWindowHarness{}
	config.Window = window
	app, err := New(config)
	if err != nil || app.Start(context.Background()).Outcome != protocolcommon.OutcomeSuccess {
		t.Fatal(err)
	}
	opened := app.OpenProject(context.Background(), "open")
	if _, ok := app.projects.session(opened.Value.Open.Session); !ok {
		t.Fatal("tracked session unavailable")
	}
	if phantom := app.SetEphemeralState(EphemeralStateInput{Session: opened.Value.Open.Session, Dirty: true}); phantom.Failure == nil || phantom.Failure.Code != desktopcontract.FailureRecoveryRequired {
		t.Fatalf("phantom dirty state accepted=%+v", phantom)
	}
	dirty := app.SetEphemeralState(EphemeralStateInput{Session: opened.Value.Open.Session, Dirty: true, Recovery: &RecoveryArtifact{Kind: RecoveryEditorState, Reference: "editor-buffer:primary"}})
	if dirty.Outcome != protocolcommon.OutcomeSuccess || dirty.Value.CanClose {
		t.Fatalf("dirty=%+v", dirty)
	}
	quit := app.PrepareQuit()
	if quit.Value.CanQuit || len(quit.Value.Projects) != 1 || quit.Value.Projects[0].ProjectID != opened.Value.ProjectID {
		t.Fatalf("quit=%+v", quit)
	}
	if closeWindow := app.RequestWindowClose(context.Background()); closeWindow.Value.CanQuit || window.closes != 0 {
		t.Fatalf("blocked window close=%+v", closeWindow)
	}
	if clean := app.SetEphemeralState(EphemeralStateInput{Session: opened.Value.Open.Session, Dirty: false}); !clean.Value.CanClose {
		t.Fatalf("clean=%+v", clean)
	}
	if closeWindow := app.RequestWindowClose(context.Background()); !closeWindow.Value.CanQuit || window.closes != 1 {
		t.Fatalf("window close=%+v", closeWindow)
	}
	if app.State() != desktopcontract.LifecycleDraining {
		t.Fatalf("window close did not retain quit fence: %s", app.State())
	}
	if done, _, _, requestFailure := app.beginProject(opened.Value.Open.Session, desktopcontract.ComponentRuntime); requestFailure == nil {
		done()
		t.Fatal("new project work entered after native quit fence")
	}
	window.panicClose = true
	if closeWindow := app.RequestWindowClose(context.Background()); closeWindow.Failure == nil || closeWindow.Failure.Code != desktopcontract.FailureShutdown {
		t.Fatalf("panic window=%+v", closeWindow)
	}

	external := &externalLifecycleHarness{reconcile: true}
	app.config.ExternalLifecycle = external
	connected := app.ConnectExternal(context.Background(), ExternalConnectionRequest{ProviderID: "provider", CredentialRef: desktopcontract.CredentialRef{ID: "credential"}})
	if connected.Outcome != protocolcommon.OutcomeSuccess || connected.Value.ConnectionID == "" {
		t.Fatalf("connect=%+v", connected)
	}
	app.config.ExternalLifecycle = panicExternalLifecycle{}
	if result := app.ConnectExternal(context.Background(), ExternalConnectionRequest{ProviderID: "provider"}); result.Failure == nil || result.Failure.Code != desktopcontract.FailureBackendPanic {
		t.Fatalf("panic connect=%+v", result)
	}
	if result := app.SyncExternal(context.Background(), opened.Value.Open.Session, ExternalSyncRequest{DocumentID: opened.Value.ProjectID}); result.Failure == nil || result.Failure.Code != desktopcontract.FailureBackendPanic {
		t.Fatalf("panic sync=%+v", result)
	}
	if result := app.ReconcileExternal(context.Background(), opened.Value.Open.Session, ExternalReconcileRequest{DocumentID: opened.Value.ProjectID}); result.Failure == nil || result.Failure.Code != desktopcontract.FailureBackendPanic {
		t.Fatalf("panic reconcile=%+v", result)
	}
	if result := app.PinProject("document_unknown", true); result.Failure == nil || result.Failure.Code != desktopcontract.FailureProjectMissing {
		t.Fatalf("unknown pin=%+v", result)
	}
	if result := app.ResolveRecovery(context.Background(), opened.Value.ProjectID, "unknown"); result.Failure == nil || result.Failure.Code != desktopcontract.FailureRecoveryRequired {
		t.Fatalf("unknown recovery=%+v", result)
	}
	if result := app.SetEphemeralState(EphemeralStateInput{Session: runtimeprotocol.RuntimeSessionRef{}, Dirty: true, Recovery: &RecoveryArtifact{Kind: RecoveryEditorState, Reference: "editor-buffer:unknown"}}); result.Failure == nil {
		t.Fatalf("unknown session=%+v", result)
	}
	app.config.ExternalLifecycle = nil
	_ = app.CloseProject(context.Background(), opened.Value.Open.Session)
	_ = app.Shutdown(context.Background())
}

func TestLifecycleMetadataRejectsUnsafeOrMalformedFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "project-lifecycle.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"projects":{},"recoveries":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := newProjectLifecycle(root, time.Now); runtime.GOOS != "windows" && err == nil {
		t.Fatal("permissive Unix metadata accepted")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":3,"projects":{},"recoveries":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newProjectLifecycle(root, time.Now); err == nil {
		t.Fatal("unknown metadata version accepted")
	}
	if err := os.WriteFile(path, []byte(`{"version":2,"projects":{},"recoveries":{},"pending_preview":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newProjectLifecycle(root, time.Now); err == nil {
		t.Fatal("phantom recovery field accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte(`{"version":1,"projects":{},"recoveries":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := newProjectLifecycle(root, time.Now); err == nil {
		t.Fatal("symlink metadata accepted")
	}
}

func TestCloseRollbackPreservesRetryAfterCatalogPersistenceFailure(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root)
	storage := &lifecycleStorageHarness{locations: map[string]ProjectLocation{"open": {Root: project, EntryPath: "document.ldl"}}, errs: map[string]error{}}
	app := startLifecycleApp(t, root, project, &dialogHarness{}, storage)
	opened := app.OpenProject(context.Background(), "open")
	faulted := false
	app.projects.saveFault = func() error {
		if !faulted {
			faulted = true
			return errors.New("injected catalog persistence failure")
		}
		return nil
	}
	first := app.CloseProject(context.Background(), opened.Value.Open.Session)
	if first.Failure == nil || first.Failure.Code != desktopcontract.FailureAdapterUnavailable {
		t.Fatalf("catalog failure=%+v", first)
	}
	if assessment := app.PrepareClose(opened.Value.Open.Session); assessment.Outcome != protocolcommon.OutcomeSuccess || !assessment.Value.CanClose {
		t.Fatalf("session was not restored=%+v", assessment)
	}
	app.projects.saveFault = nil
	if retry := app.CloseProject(context.Background(), opened.Value.Open.Session); retry.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("retry=%+v", retry)
	}
	_ = app.Shutdown(context.Background())
}

func TestAutosaveSchedulePersistenceFailureCancelsAndBlocksClose(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root)
	storage := &lifecycleStorageHarness{locations: map[string]ProjectLocation{"open": {Root: project, EntryPath: "document.ldl"}}, errs: map[string]error{}}
	app := startLifecycleApp(t, root, project, &dialogHarness{}, storage)
	opened := app.OpenProject(context.Background(), "open")
	batch, err := engineprotocol.DecodeSemanticOperationBatch([]byte(`{"operations":[{"operation":"create_subject","subject_kind":"layer","parent_address":"ldl:project:p","id":"autosave","fields":{"display_name":"Autosave","order":"1"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	commit := runtimeprotocol.RuntimeCommitInput{Session: opened.Value.Open.Session, OperationID: "autosave_persist_failure", IdempotencyKey: "autosave_persist_failure_idem", OperationBatch: runtimeprotocol.RuntimeOperationBatch{DocumentID: opened.Value.ProjectID, BaseRevision: opened.Value.Open.CommittedRevision, ExpectedDefinitionHash: opened.Value.Open.CommittedRevision.DefinitionHash, Operations: batch, Preconditions: preconditionsFor(t, "project p \"P\" {}\n")}}
	cancelled := false
	originalCancel := app.cancelAutosave
	app.cancelAutosave = func(host *localdocument.Host, session runtimeprotocol.RuntimeSessionRef) error {
		cancelled = true
		return originalCancel(host, session)
	}
	app.projects.saveFault = func() error { return errors.New("lifecycle persistence unavailable") }
	result := app.ControlAutosave(context.Background(), runtimeprotocol.AutosaveControlInput{Action: runtimeprotocol.AutosaveActionSchedule, Session: commit.Session, Commit: &commit})
	if result.Failure == nil || result.Failure.Code != desktopcontract.FailureRecoveryRequired || !cancelled {
		t.Fatalf("orphan autosave was not cancelled: result=%+v cancelled=%v", result, cancelled)
	}
	assessment := app.PrepareClose(commit.Session)
	if assessment.Value.CanClose || !slices.Contains(assessment.Value.Blockers, CloseRecoveryRequired) {
		t.Fatalf("recovery-required autosave allowed close: %+v", assessment)
	}
	if closeResult := app.CloseProject(context.Background(), commit.Session); closeResult.Failure == nil || closeResult.Failure.Code != desktopcontract.FailureReconcilePending {
		t.Fatalf("recovery-required journal detached: %+v", closeResult)
	}
	if _, ok := app.projects.recovery(opened.Value.ProjectID); !ok {
		t.Fatal("recovery journal removed after blocked close")
	}
}

func TestLifecycleNegativeBoundariesRemainTyped(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root)
	storage := &lifecycleStorageHarness{locations: map[string]ProjectLocation{"open": {Root: project, EntryPath: "document.ldl"}}, errs: map[string]error{}}
	app := startLifecycleApp(t, root, project, &dialogHarness{}, storage)
	for _, test := range []struct {
		err  error
		code desktopcontract.FailureCode
	}{
		{os.ErrPermission, desktopcontract.FailurePermissionDenied},
		{os.ErrNotExist, desktopcontract.FailureProjectMissing},
		{localdocument.ErrStateRecoveryRequired, desktopcontract.FailureRecoveryRequired},
		{port.ErrConflict, desktopcontract.FailureProjectConflict},
		{errors.New("opaque"), desktopcontract.FailureAdapterUnavailable},
	} {
		result := mapProjectOpenFailure[string](test.err, desktopcontract.ComponentRuntime)
		if result.Failure == nil || result.Failure.Code != test.code {
			t.Fatalf("map %v=%+v", test.err, result)
		}
	}
	if result := app.CreateProjectDialog(context.Background(), ""); result.Failure == nil {
		t.Fatalf("empty request=%+v", result)
	}
	app.config.Dialogs = panicDialog{}
	if result := app.OpenProjectDialog(context.Background(), "panic"); result.Failure == nil || result.Failure.Code != desktopcontract.FailureAdapterUnavailable {
		t.Fatalf("panic dialog=%+v", result)
	}
	app.config.ExternalLifecycle = nil
	if result := app.ConnectExternal(context.Background(), ExternalConnectionRequest{}); result.Failure == nil || result.Failure.Code != desktopcontract.FailureAdapterUnavailable {
		t.Fatalf("missing external=%+v", result)
	}
	app.config.ExternalLifecycle = invalidExternalLifecycle{}
	if result := app.ConnectExternal(context.Background(), ExternalConnectionRequest{ProviderID: "expected"}); result.Failure == nil || result.Failure.Code != desktopcontract.FailureAdapterUnavailable {
		t.Fatalf("invalid connect=%+v", result)
	}
	opened := app.OpenProject(context.Background(), "open")
	if result := app.SyncExternal(context.Background(), opened.Value.Open.Session, ExternalSyncRequest{DocumentID: opened.Value.ProjectID}); result.Failure == nil || result.Failure.Code != desktopcontract.FailureAdapterUnavailable {
		t.Fatalf("invalid sync=%+v", result)
	}
	if result := app.ReconcileExternal(context.Background(), opened.Value.Open.Session, ExternalReconcileRequest{DocumentID: opened.Value.ProjectID}); result.Failure == nil || result.Failure.Code != desktopcontract.FailureAdapterUnavailable {
		t.Fatalf("invalid reconcile=%+v", result)
	}
	if result := app.PrepareClose(runtimeprotocol.RuntimeSessionRef{}); result.Failure == nil {
		t.Fatalf("unknown prepare=%+v", result)
	}
	if result := app.ResolveClose(context.Background(), opened.Value.Open.Session, CloseKeepOpen); result.Outcome != protocolcommon.OutcomeSuccess || result.Value.Closed {
		t.Fatalf("keep open=%+v", result)
	}
	if result := app.ResolveClose(context.Background(), opened.Value.Open.Session, "unknown"); result.Failure == nil || result.Failure.Code != desktopcontract.FailureProjectConflict {
		t.Fatalf("unknown close=%+v", result)
	}
	app.config.ExternalLifecycle = nil
	_ = app.CloseProject(context.Background(), opened.Value.Open.Session)
	_ = app.Shutdown(context.Background())
}

func TestCloseRollbackPreservesRetryAfterHostCloseFailure(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root)
	storage := &lifecycleStorageHarness{locations: map[string]ProjectLocation{"open": {Root: project, EntryPath: "document.ldl"}}, errs: map[string]error{}}
	app := startLifecycleApp(t, root, project, &dialogHarness{}, storage)
	opened := app.OpenProject(context.Background(), "open")
	original := app.closeProjectSession
	app.closeProjectSession = func(context.Context, *localdocument.Host, *localdocument.Session) error {
		return errors.New("injected host close failure")
	}
	first := app.CloseProject(context.Background(), opened.Value.Open.Session)
	if first.Failure == nil || first.Failure.Code != desktopcontract.FailureReconnect {
		t.Fatalf("host failure=%+v", first)
	}
	if assessment := app.PrepareClose(opened.Value.Open.Session); assessment.Outcome != protocolcommon.OutcomeSuccess || !assessment.Value.CanClose {
		t.Fatalf("session was not restored=%+v", assessment)
	}
	app.closeProjectSession = original
	if retry := app.CloseProject(context.Background(), opened.Value.Open.Session); retry.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("retry=%+v", retry)
	}
	_ = app.Shutdown(context.Background())
}

func TestProjectCloseFenceDrainsThenReassessesAndRollsBack(t *testing.T) {
	lifecycle, err := newProjectLifecycle(t.TempDir(), func() time.Time { return desktopTestNow })
	if err != nil {
		t.Fatal(err)
	}
	ref := runtimeprotocol.RuntimeSessionRef{RuntimeSessionID: "session_fence_1234", SessionGeneration: "1", Scope: runtimeprotocol.RuntimeScope{DocumentID: "document_fence", LocalScopeID: "local", AccessFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	revision := runtimeprotocol.CommittedRevisionRef{DocumentID: ref.Scope.DocumentID, RevisionID: "revision_fence", DefinitionHash: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", GraphHash: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}
	if _, _, err := lifecycle.opened(ref, revision, false, ""); err != nil {
		t.Fatal(err)
	}
	generation, err := lifecycle.begin(ref)
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan CloseAssessment, 1)
	go func() {
		assessment, _ := lifecycle.fenceClose(context.Background().Done(), ref)
		finished <- assessment
	}()
	select {
	case <-finished:
		t.Fatal("close fence did not drain inflight work")
	case <-time.After(10 * time.Millisecond):
	}
	if err := lifecycle.mutate(ref, generation, func(state *sessionLifecycle) { state.ephemeralEdits = true }); err != nil {
		t.Fatal(err)
	}
	lifecycle.end(ref, generation)
	assessment := <-finished
	if assessment.CanClose || len(assessment.Blockers) != 1 || assessment.Blockers[0] != CloseEphemeralEdits {
		t.Fatalf("post-drain assessment=%+v", assessment)
	}
	lifecycle.rollbackClose(ref)
	if _, err := lifecycle.begin(ref); err != nil {
		t.Fatalf("rollback did not reopen session: %v", err)
	}
}

func TestRecoveryJournalTracksDirtyAutosaveAndProviderState(t *testing.T) {
	root := t.TempDir()
	lifecycle, err := newProjectLifecycle(root, func() time.Time { return desktopTestNow })
	if err != nil {
		t.Fatal(err)
	}
	ref := runtimeprotocol.RuntimeSessionRef{RuntimeSessionID: "session_recovery_1234", SessionGeneration: "1", Scope: runtimeprotocol.RuntimeScope{DocumentID: "document_recovery", LocalScopeID: "local", AccessFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	revision := runtimeprotocol.CommittedRevisionRef{DocumentID: ref.Scope.DocumentID, RevisionID: "revision_recovery", DefinitionHash: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", GraphHash: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}
	if _, _, err := lifecycle.opened(ref, revision, false, ""); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.mutate(ref, 0, func(state *sessionLifecycle) {
		state.pendingPreview, state.ephemeralEdits, state.autosavePending, state.providerPending = true, true, true, true
		state.recovery = &RecoveryArtifact{Kind: RecoveryPreviewOperations, Payload: json.RawMessage(`{"operations":[]}`)}
		state.autosave = AutosaveScheduled
		state.autosaveGeneration = 2
	}); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.completeAutosave(ref, 1, 1, func(state *sessionLifecycle) { state.autosave = AutosaveCommitted }); err == nil {
		t.Fatal("stale autosave completion changed a newer schedule")
	}
	restarted, err := newProjectLifecycle(root, func() time.Time { return desktopTestNow })
	if err != nil {
		t.Fatal(err)
	}
	candidates := restarted.recoveries()
	if len(candidates) != 1 || !candidates[0].PendingPreview || !candidates[0].EphemeralEdits || !candidates[0].AutosavePending || !candidates[0].ProviderPending || candidates[0].Autosave != AutosaveScheduled {
		t.Fatalf("recovery state=%+v", candidates)
	}
	if candidates[0].Recovery == nil || candidates[0].Recovery.Kind != RecoveryPreviewOperations || string(candidates[0].Recovery.Payload) != `{"operations":[]}` {
		t.Fatalf("recoverable preview payload=%+v", candidates[0].Recovery)
	}
}

func TestAutosaveCompletionOutcomesAreClosedAndGenerationBound(t *testing.T) {
	tests := []struct {
		name     string
		result   localdocument.AutosaveResult
		outcome  AutosaveOutcome
		dirty    bool
		provider bool
		terminal CloseBlocker
	}{
		{name: "cancelled", result: localdocument.AutosaveResult{Err: context.Canceled}, outcome: AutosaveIdle, dirty: true},
		{name: "conflict", result: localdocument.AutosaveResult{Err: port.ErrConflict}, outcome: AutosaveConflict, dirty: true},
		{name: "failure", result: localdocument.AutosaveResult{Err: errors.New("closed failure")}, outcome: AutosaveFailed, dirty: true},
		{name: "needs_review", result: localdocument.AutosaveResult{Result: runtimeprotocol.RuntimeCommitResult{OperationResult: runtimeprotocol.OperationResult{Status: runtimeprotocol.OperationResultStatusNeedsReview}}}, outcome: AutosaveNeedsReview, dirty: true},
		{name: "rejected", result: localdocument.AutosaveResult{Result: runtimeprotocol.RuntimeCommitResult{OperationResult: runtimeprotocol.OperationResult{Status: runtimeprotocol.OperationResultStatusRejected}}}, outcome: AutosaveFailed, dirty: true},
		{name: "external_pending", result: localdocument.AutosaveResult{Result: runtimeprotocol.RuntimeCommitResult{OperationResult: runtimeprotocol.OperationResult{Status: runtimeprotocol.OperationResultStatusCommittedExternalPending, ExternalMaterialization: &runtimeprotocol.ExternalMaterializationStatus{State: runtimeprotocol.ExternalMaterializationStatePending}}}}, outcome: AutosaveCommitted, terminal: CloseExternalPending},
		{name: "external_failed", result: localdocument.AutosaveResult{Result: runtimeprotocol.RuntimeCommitResult{OperationResult: runtimeprotocol.OperationResult{Status: runtimeprotocol.OperationResultStatusCommittedExternalFailed}}}, outcome: AutosaveCommitted, terminal: CloseExternalFailed},
		{name: "state_stale", result: localdocument.AutosaveResult{Result: runtimeprotocol.RuntimeCommitResult{OperationResult: runtimeprotocol.OperationResult{Status: runtimeprotocol.OperationResultStatusCommittedStateStale}}}, outcome: AutosaveCommitted, terminal: CloseStateStale},
		{name: "committed", result: localdocument.AutosaveResult{Result: runtimeprotocol.RuntimeCommitResult{OperationResult: runtimeprotocol.OperationResult{Status: runtimeprotocol.OperationResultStatusCommitted}}}, outcome: AutosaveCommitted},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lifecycle, err := newProjectLifecycle(t.TempDir(), func() time.Time { return desktopTestNow })
			if err != nil {
				t.Fatal(err)
			}
			ref := runtimeprotocol.RuntimeSessionRef{RuntimeSessionID: runtimeprotocol.RuntimeSessionID(fmt.Sprintf("session_autosave_%04d", index)), SessionGeneration: "1", Scope: runtimeprotocol.RuntimeScope{DocumentID: runtimeprotocol.DocumentID(fmt.Sprintf("document_autosave_%d", index)), LocalScopeID: "local", AccessFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
			revision := runtimeprotocol.CommittedRevisionRef{DocumentID: ref.Scope.DocumentID, RevisionID: "revision_autosave", DefinitionHash: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", GraphHash: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}
			if _, _, err := lifecycle.opened(ref, revision, false, ""); err != nil {
				t.Fatal(err)
			}
			if err := lifecycle.mutate(ref, 0, func(state *sessionLifecycle) {
				state.autosaveGeneration = 1
				state.autosavePending = true
				state.ephemeralEdits = true
				state.recovery = &RecoveryArtifact{Kind: RecoveryPreviewOperations, Payload: json.RawMessage(`{"operations":[]}`)}
				state.autosave = AutosaveScheduled
			}); err != nil {
				t.Fatal(err)
			}
			application := &Application{projects: lifecycle}
			completion := make(chan localdocument.AutosaveResult, 1)
			completion <- test.result
			done := false
			application.collectAutosave(ref, 1, 1, completion, func() { done = true })
			status := application.AutosaveStatus(ref)
			dirty, pending := false, false
			for _, blocker := range status.Value.Blockers {
				dirty = dirty || blocker == CloseEphemeralEdits
				pending = pending || blocker == CloseAutosavePending
			}
			if !done || status.Outcome != protocolcommon.OutcomeSuccess || status.Value.Autosave != test.outcome || pending || dirty != test.dirty {
				t.Fatalf("completion=%+v done=%v", status, done)
			}
			state, _ := lifecycle.session(ref)
			if state.providerPending != test.provider {
				t.Fatalf("provider pending=%v", state.providerPending)
			}
			if state.terminalBlocker != test.terminal {
				t.Fatalf("terminal blocker=%q want=%q", state.terminalBlocker, test.terminal)
			}
			if (state.terminalRecovery != nil) != (test.terminal != "") {
				t.Fatalf("terminal recovery=%+v blocker=%q", state.terminalRecovery, test.terminal)
			}
		})
	}
	recorder := &lifecycleRecorder{}
	application := &Application{config: Config{Lifecycle: recorder}, state: desktopcontract.LifecycleDraining}
	application.rollbackDraining()
	if application.State() != desktopcontract.LifecycleReady {
		t.Fatalf("shutdown rollback state=%s", application.State())
	}
}

func TestLifecycleSortingRestoreAndAutosavePersistenceFailures(t *testing.T) {
	lifecycle, err := newProjectLifecycle(t.TempDir(), func() time.Time { return desktopTestNow })
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.state.Projects = map[string]persistedProject{
		"document_b": {ProjectID: "document_b", LastOpenedAt: "2026-01-01T00:00:00Z", Missing: true},
		"document_a": {ProjectID: "document_a", LastOpenedAt: "2026-01-01T00:00:00Z"},
		"document_c": {ProjectID: "document_c", LastOpenedAt: "2025-01-01T00:00:00Z", Pinned: true},
	}
	recent := lifecycle.recent()
	if len(recent) != 3 || recent[0].ProjectID != "document_c" || recent[1].ProjectID != "document_a" || recent[2].Availability != ProjectMissing {
		t.Fatalf("recent ordering=%+v", recent)
	}
	if err := lifecycle.restore(nil); err != nil {
		t.Fatal(err)
	}
	ref := runtimeprotocol.RuntimeSessionRef{RuntimeSessionID: "session_failure_1234", SessionGeneration: "1", Scope: runtimeprotocol.RuntimeScope{DocumentID: "document_failure", LocalScopeID: "local", AccessFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	revision := runtimeprotocol.CommittedRevisionRef{DocumentID: ref.Scope.DocumentID, RevisionID: "revision_failure", DefinitionHash: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", GraphHash: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}
	if _, _, err := lifecycle.opened(ref, revision, false, ""); err != nil {
		t.Fatal(err)
	}
	state, ok := lifecycle.session(ref)
	if !ok || state == nil {
		t.Fatal("opened session unavailable")
	}
	if err := lifecycle.restore(state); err == nil {
		t.Fatal("duplicate session restore succeeded")
	}
	if _, ok := lifecycle.session(runtimeprotocol.RuntimeSessionRef{}); ok {
		t.Fatal("unknown session resolved")
	}
	state, _ = lifecycle.session(ref)
	lifecycle.saveFault = func() error { return errors.New("persistence unavailable") }
	if err := lifecycle.completeAutosave(ref, state.generation, state.autosaveGeneration, func(value *sessionLifecycle) { value.autosave = AutosaveCommitted }); err == nil {
		t.Fatal("autosave persistence failure hidden")
	}
	after, _ := lifecycle.session(ref)
	if after.autosave == AutosaveCommitted {
		t.Fatal("failed autosave persistence was not rolled back")
	}
	status := (&Application{projects: lifecycle}).AutosaveStatus(ref)
	if status.Failure == nil || status.Failure.Code != desktopcontract.FailureRecoveryRequired || status.Failure.Recovery != desktopcontract.RecoveryOpenRecovery || status.Failure.Retryable {
		t.Fatalf("autosave persistence failure status=%+v", status)
	}
	closeStatus := (&Application{projects: lifecycle}).PrepareClose(ref)
	if closeStatus.Value.CanClose || !slices.Contains(closeStatus.Value.Blockers, CloseRecoveryRequired) {
		t.Fatalf("autosave recovery requirement omitted from close=%+v", closeStatus)
	}
	failing, err := newProjectLifecycle(t.TempDir(), func() time.Time { return desktopTestNow })
	if err != nil {
		t.Fatal(err)
	}
	failing.saveFault = func() error { return errors.New("open journal unavailable") }
	if _, _, err := failing.opened(ref, revision, false, ""); err == nil || len(failing.sessions) != 0 || len(failing.state.Projects) != 0 || len(failing.state.Recoveries) != 0 {
		t.Fatalf("failed open leaked state: sessions=%d projects=%d recoveries=%d err=%v", len(failing.sessions), len(failing.state.Projects), len(failing.state.Recoveries), err)
	}
	lifecycle.saveFault = func() error { return errors.New("project metadata unavailable") }
	if err := lifecycle.markMissing(ref.Scope.DocumentID, true); err == nil {
		t.Fatal("missing-state persistence failure hidden")
	}
	if missing, _ := lifecycle.missing(ref.Scope.DocumentID); missing {
		t.Fatal("failed missing-state persistence was not rolled back")
	}
	if err := lifecycle.pin(ref.Scope.DocumentID, true); err == nil {
		t.Fatal("pin persistence failure hidden")
	}
	if err := lifecycle.discardRecovery(ref.Scope.DocumentID); err == nil {
		t.Fatal("discard persistence failure hidden")
	}
	if _, ok := lifecycle.recovery(ref.Scope.DocumentID); !ok {
		t.Fatal("failed discard removed authoritative in-memory recovery")
	}
}

func TestCreateProjectAppliesDialogDisplayNameThroughEngine(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "My Diagram")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "document.ldl"), []byte("project main \"Untitled\" {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	storage := &lifecycleStorageHarness{locations: map[string]ProjectLocation{
		"create": {Root: project, EntryPath: "document.ldl", Kind: "project", DisplayName: "My Diagram"},
	}, errs: map[string]error{}}
	app := startLifecycleApp(t, root, project, &dialogHarness{}, storage)
	created := app.CreateProject(context.Background(), "create")
	if created.Outcome != protocolcommon.OutcomeSuccess || created.Value.Disposition != ProjectOpened {
		t.Fatalf("create=%+v", created)
	}
	if len(created.Value.History.Items) < 2 {
		t.Fatalf("naming commit missing from history=%+v", created.Value.History)
	}
	data, err := os.ReadFile(filepath.Join(project, "document.ldl"))
	if err != nil || string(data) != "project main \"My Diagram\" {}\n" {
		t.Fatalf("committed source=%q err=%v", data, err)
	}
	recent := app.RecentProjects()
	if recent.Outcome != protocolcommon.OutcomeSuccess || len(recent.Value) != 1 || recent.Value[0].DisplayName != "My Diagram" {
		t.Fatalf("recent=%+v", recent)
	}
	publication, err := app.ProjectPublication(context.Background())
	if err != nil || publication.Project == nil || publication.Project.DisplayName != "My Diagram" {
		t.Fatalf("publication=%+v err=%v", publication, err)
	}
	if result := app.Shutdown(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("shutdown=%+v", result)
	}
}

func TestOpenFailureSurfacesExplicitCodeAndLogsUnderlyingCause(t *testing.T) {
	root := t.TempDir()
	project := writeProject(t, root)
	storage := &lifecycleStorageHarness{locations: map[string]ProjectLocation{"open": {Root: project, EntryPath: "document.ldl", Kind: "project"}}, errs: map[string]error{}}
	type diagnostic struct {
		stage string
		err   error
	}
	var mu sync.Mutex
	var diagnostics []diagnostic
	config := testConfig(t, root, project)
	config.Dialogs = &dialogHarness{}
	config.ProjectStorage = storage
	config.OpenDiagnostics = func(_ context.Context, stage string, err error) {
		mu.Lock()
		defer mu.Unlock()
		diagnostics = append(diagnostics, diagnostic{stage: stage, err: err})
	}
	app, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if result := app.Start(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("start=%+v", result)
	}
	opened := app.OpenProject(context.Background(), "open")
	if opened.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("open=%+v", opened)
	}
	if closed := app.CloseProject(context.Background(), opened.Value.Open.Session); closed.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("close=%+v", closed)
	}
	// Externally replace the bound source with a valid document that carries a
	// different portable project identity.
	if err := os.WriteFile(filepath.Join(project, "document.ldl"), []byte("project replaced \"Replaced\" {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened := app.OpenProject(context.Background(), "open")
	if reopened.Outcome != protocolcommon.OutcomeFailed || reopened.Failure == nil ||
		reopened.Failure.Code != desktopcontract.FailureProjectConflict || reopened.Failure.Recovery != desktopcontract.RecoveryReview {
		t.Fatalf("identity change failure=%+v", reopened)
	}
	encoded, _ := json.Marshal(reopened)
	if stringContains(string(encoded), project) {
		t.Fatalf("failure leaked the native path: %s", encoded)
	}
	mu.Lock()
	recorded := append([]diagnostic(nil), diagnostics...)
	mu.Unlock()
	if len(recorded) == 0 {
		t.Fatal("underlying open cause was not logged")
	}
	last := recorded[len(recorded)-1]
	if last.stage != "project open" || !errors.Is(last.err, localdocument.ErrPortableIdentityChanged) {
		t.Fatalf("diagnostic=%+v", last)
	}
	storage.errs["denied"] = os.ErrPermission
	if denied := app.OpenProject(context.Background(), "denied"); denied.Failure == nil || denied.Failure.Code != desktopcontract.FailurePermissionDenied {
		t.Fatalf("denied=%+v", denied)
	}
	mu.Lock()
	stages := make([]string, 0, len(diagnostics))
	for _, value := range diagnostics {
		stages = append(stages, value.stage)
	}
	mu.Unlock()
	if !slices.Contains(stages, "project storage selection") {
		t.Fatalf("storage stage missing from diagnostics=%v", stages)
	}
	_ = app.Shutdown(context.Background())
}

// TestRecentReopenAfterExternalIdentityChangeFailsExplicitly drives the real
// recent-project route (create with a dialog name, app restart on the same
// data root, external identity edit, OpenRecentProject) and requires an
// explicit conflict instead of silently reopening the committed document.
func TestRecentReopenAfterExternalIdentityChangeFailsExplicitly(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "QA-Test-Project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "document.ldl"), []byte("project main \"Untitled\" {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	storage := &lifecycleStorageHarness{locations: map[string]ProjectLocation{
		"create": {Root: project, EntryPath: "document.ldl", Kind: "project", DisplayName: "QA-Test-Project"},
	}, errs: map[string]error{}}
	app := startLifecycleApp(t, root, project, &dialogHarness{}, storage)
	created := app.CreateProject(context.Background(), "create")
	if created.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("create=%+v", created)
	}
	// Quit like the native shell does: no explicit project close first.
	if result := app.Shutdown(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("shutdown=%+v", result)
	}
	if err := os.WriteFile(filepath.Join(project, "document.ldl"), []byte("project renamedident \"QA-Test-Project\" {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var stages []string
	var causes []error
	config := testConfig(t, root, project)
	config.Dialogs = &dialogHarness{}
	config.ProjectStorage = storage
	config.OpenDiagnostics = func(_ context.Context, stage string, err error) {
		mu.Lock()
		defer mu.Unlock()
		stages = append(stages, stage)
		causes = append(causes, err)
	}
	restarted, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if result := restarted.Start(context.Background()); result.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("restart=%+v", result)
	}
	recent := restarted.RecentProjects()
	if recent.Outcome != protocolcommon.OutcomeSuccess || len(recent.Value) != 1 || recent.Value[0].DisplayName != "QA-Test-Project" {
		t.Fatalf("recent=%+v", recent)
	}
	reopened := restarted.OpenRecentProject(context.Background(), created.Value.ProjectID)
	if reopened.Outcome != protocolcommon.OutcomeFailed || reopened.Failure == nil ||
		reopened.Failure.Code != desktopcontract.FailureProjectConflict || reopened.Failure.Recovery != desktopcontract.RecoveryReview {
		t.Fatalf("identity change reopen=%+v", reopened)
	}
	if publication, publicationErr := restarted.ProjectPublication(context.Background()); publicationErr != nil || publication.Project != nil {
		t.Fatalf("workspace opened despite conflict: %+v err=%v", publication, publicationErr)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(stages) == 0 || stages[len(stages)-1] != "project reload" || !errors.Is(causes[len(causes)-1], localdocument.ErrPortableIdentityChanged) {
		t.Fatalf("diagnostics stages=%v causes=%v", stages, causes)
	}
	_ = restarted.Shutdown(context.Background())
}
