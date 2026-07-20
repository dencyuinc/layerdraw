// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

	restarted := startLifecycleApp(t, root, project, &dialogHarness{}, storage)
	candidates := restarted.RecoveryCandidates()
	if candidates.Outcome != protocolcommon.OutcomeSuccess || len(candidates.Value) != 1 || candidates.Value[0].ProjectID != opened.Value.ProjectID {
		t.Fatalf("candidates=%+v", candidates)
	}
	if bypass := restarted.OpenRecentProject(context.Background(), opened.Value.ProjectID); bypass.Failure == nil || bypass.Failure.Code != desktopcontract.FailureRecoveryRequired {
		t.Fatalf("recovery bypass=%+v", bypass)
	}
	restored := restarted.ResolveRecovery(context.Background(), opened.Value.ProjectID, RecoveryRestore)
	if restored.Outcome != protocolcommon.OutcomeSuccess || restored.Value.Disposition != ProjectRestored {
		t.Fatalf("restore=%+v", restored)
	}
	if candidates = restarted.RecoveryCandidates(); len(candidates.Value) != 0 {
		t.Fatalf("restored candidate remained=%+v", candidates)
	}
	if close := restarted.CloseProject(context.Background(), restored.Value.Open.Session); close.Outcome != protocolcommon.OutcomeSuccess {
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
	dirty := app.SetEphemeralState(EphemeralStateInput{Session: opened.Value.Open.Session, Dirty: true})
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
	if result := app.SetEphemeralState(EphemeralStateInput{Session: runtimeprotocol.RuntimeSessionRef{}, Dirty: true}); result.Failure == nil {
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
	if _, err := newProjectLifecycle(root, time.Now); err == nil {
		t.Fatal("permissive metadata accepted")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":2,"projects":{},"recoveries":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newProjectLifecycle(root, time.Now); err == nil {
		t.Fatal("unknown metadata version accepted")
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
