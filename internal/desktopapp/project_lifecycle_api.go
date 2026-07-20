// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/dencyuinc/layerdraw/internal/localdocument"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type CloseResolution string

const (
	CloseKeepOpen              CloseResolution = "keep_open"
	CloseDiscardEphemeral      CloseResolution = "discard_ephemeral"
	CloseCancelAutosaveDiscard CloseResolution = "cancel_autosave_and_discard"
)

type CloseResolutionResult struct {
	Closed bool `json:"closed"`
}

type QuitAssessment struct {
	CanQuit  bool              `json:"can_quit"`
	Projects []CloseAssessment `json:"projects"`
}

func (a *Application) beginProject(ref runtimeprotocol.RuntimeSessionRef, component desktopcontract.ComponentID) (func(), *localdocument.Host, uint64, *desktopcontract.Failure) {
	generation, err := a.projects.begin(ref)
	if err != nil {
		failure := failed[struct{}](desktopcontract.FailureProjectConflict, component, true, desktopcontract.RecoveryRetry).Failure
		return nil, nil, 0, failure
	}
	doneHost, host, failure := a.beginHost(component)
	if failure != nil {
		a.projects.end(ref, generation)
		return nil, nil, 0, failure
	}
	done := func() {
		doneHost()
		a.projects.end(ref, generation)
	}
	return done, host, generation, nil
}

func safeDialogSelect(ctx context.Context, port desktopcontract.NativeDialogPort, request desktopcontract.DialogRequest) (result desktopcontract.Result[desktopcontract.DialogSelection]) {
	defer func() {
		if recover() != nil {
			result = failed[desktopcontract.DialogSelection](desktopcontract.FailureBackendPanic, desktopcontract.ComponentBindingShell, false, desktopcontract.RecoveryExit)
		}
	}()
	return port.Select(ctx, request)
}

func safeWindowShow(ctx context.Context, port desktopcontract.WindowPort) (err error) {
	defer func() {
		if recover() != nil {
			err = errInjectedPanic
		}
	}()
	return port.Show(ctx)
}

func safeWindowRequestClose(ctx context.Context, port desktopcontract.WindowPort) (err error) {
	defer func() {
		if recover() != nil {
			err = errInjectedPanic
		}
	}()
	return port.RequestClose(ctx)
}

func (a *Application) CreateProjectDialog(ctx context.Context, requestID string) desktopcontract.Result[ProjectOpenResult] {
	return a.selectProject(ctx, desktopcontract.DialogCreateProject, requestID, []string{"ldl"}, a.config.ProjectStorage.Create)
}

func (a *Application) OpenProjectDialog(ctx context.Context, requestID string) desktopcontract.Result[ProjectOpenResult] {
	return a.selectProject(ctx, desktopcontract.DialogOpenProject, requestID, []string{"ldl", "layerdraw"}, a.config.ProjectStorage.Open)
}

func (a *Application) ImportProjectDialog(ctx context.Context, requestID string) desktopcontract.Result[ProjectOpenResult] {
	storage, ok := a.config.ProjectStorage.(ProjectImportStorage)
	if !ok {
		return failed[ProjectOpenResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentLocalStorage, false, desktopcontract.RecoveryConfigureAdapter)
	}
	return a.selectProject(ctx, desktopcontract.DialogImport, requestID, []string{"layerdraw"}, storage.Import)
}

func (a *Application) selectProject(ctx context.Context, kind desktopcontract.DialogKind, requestID string, extensions []string, resolve func(context.Context, string) (ProjectLocation, error)) desktopcontract.Result[ProjectOpenResult] {
	if requestID == "" {
		return failed[ProjectOpenResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentBindingShell, false, desktopcontract.RecoveryRetry)
	}
	selection := safeDialogSelect(ctx, a.config.Dialogs, desktopcontract.DialogRequest{Kind: kind, RequestID: requestID, Extensions: append([]string(nil), extensions...)})
	if !selection.Validate() {
		return failed[ProjectOpenResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentBindingShell, true, desktopcontract.RecoveryRetry)
	}
	if selection.Outcome == protocolcommon.OutcomeCancelled {
		return cancelled[ProjectOpenResult](desktopcontract.ComponentBindingShell)
	}
	if selection.Outcome != protocolcommon.OutcomeSuccess || selection.Value.Token == "" {
		return failed[ProjectOpenResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentBindingShell, true, desktopcontract.RecoveryRetry)
	}
	return a.openSelected(ctx, desktopcontract.ComponentLocalStorage, selection.Value.Token, resolve)
}

func (a *Application) RecentProjects() desktopcontract.Result[[]RecentProject] {
	projects := a.projects.recent()
	return desktopcontract.Result[[]RecentProject]{Outcome: protocolcommon.OutcomeSuccess, Value: projects}
}

func (a *Application) PinProject(projectID runtimeprotocol.DocumentID, pinned bool) desktopcontract.Result[[]RecentProject] {
	if err := a.projects.pin(projectID, pinned); err != nil {
		return failed[[]RecentProject](desktopcontract.FailureProjectMissing, desktopcontract.ComponentLocalStorage, false, desktopcontract.RecoveryLocate)
	}
	return a.RecentProjects()
}

func (a *Application) OpenRecentProject(ctx context.Context, projectID runtimeprotocol.DocumentID) desktopcontract.Result[ProjectOpenResult] {
	return a.openRecentProject(ctx, projectID, false)
}

func (a *Application) openRecentProject(ctx context.Context, projectID runtimeprotocol.DocumentID, allowRecovery bool) desktopcontract.Result[ProjectOpenResult] {
	if existing, ok := a.projects.existing(projectID); ok {
		done, host, requestFailure := a.beginHost(desktopcontract.ComponentRuntime)
		if requestFailure != nil {
			return desktopcontract.Result[ProjectOpenResult]{Outcome: protocolcommon.OutcomeFailed, Failure: requestFailure}
		}
		defer done()
		session, err := host.SessionFor(existing)
		if err != nil {
			return failed[ProjectOpenResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
		}
		_ = safeWindowShow(ctx, a.config.Window)
		return desktopcontract.Result[ProjectOpenResult]{Outcome: protocolcommon.OutcomeSuccess, Value: ProjectOpenResult{Open: session.Open, ProjectID: projectID, Disposition: ProjectFocused}}
	}
	result := a.reloadProject(ctx, projectID, allowRecovery)
	if result.Outcome != protocolcommon.OutcomeSuccess {
		if result.Failure != nil && result.Failure.Code == desktopcontract.FailureProjectMissing {
			if err := a.projects.markMissing(projectID, true); err != nil {
				return failed[ProjectOpenResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentLocalStorage, true, desktopcontract.RecoveryRetry)
			}
		}
		return result
	}
	if err := a.projects.markMissing(projectID, false); err != nil {
		_ = a.CloseProject(ctx, result.Value.Open.Session)
		return failed[ProjectOpenResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentLocalStorage, true, desktopcontract.RecoveryRetry)
	}
	return result
}

func (a *Application) RelocateProject(ctx context.Context, projectID runtimeprotocol.DocumentID, selectionToken string) desktopcontract.Result[ProjectOpenResult] {
	storage, ok := a.config.ProjectStorage.(ProjectRelocationStorage)
	if !ok || selectionToken == "" {
		return failed[ProjectOpenResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentLocalStorage, false, desktopcontract.RecoveryConfigureAdapter)
	}
	location, err := storage.Relocate(ctx, projectID, selectionToken)
	if err != nil || location.Root == "" {
		return mapStorageFailure[ProjectOpenResult](err)
	}
	wasMissing, known := a.projects.missing(projectID)
	if !known {
		return failed[ProjectOpenResult](desktopcontract.FailureProjectMissing, desktopcontract.ComponentLocalStorage, false, desktopcontract.RecoveryLocate)
	}
	if err := a.projects.markMissing(projectID, false); err != nil {
		return failed[ProjectOpenResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentLocalStorage, true, desktopcontract.RecoveryRetry)
	}
	done, host, requestFailure := a.beginHost(desktopcontract.ComponentRuntime)
	if requestFailure != nil {
		_ = a.projects.markMissing(projectID, wasMissing)
		return desktopcontract.Result[ProjectOpenResult]{Outcome: protocolcommon.OutcomeFailed, Failure: requestFailure}
	}
	defer done()
	if err := host.RelocateProject(ctx, projectID, localdocumentInput(location)); err != nil {
		if rollbackErr := a.projects.markMissing(projectID, wasMissing); rollbackErr != nil {
			return failed[ProjectOpenResult](desktopcontract.FailureRecoveryRequired, desktopcontract.ComponentLocalStorage, true, desktopcontract.RecoveryOpenRecovery)
		}
		return failed[ProjectOpenResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentRuntime, false, desktopcontract.RecoveryReview)
	}
	return a.OpenRecentProject(ctx, projectID)
}

func localdocumentInput(location ProjectLocation) localdocument.OpenProjectInput {
	return localdocument.OpenProjectInput{Root: location.Root, EntryPath: location.EntryPath}
}

func mapStorageFailure[T any](err error) desktopcontract.Result[T] {
	if errors.Is(err, os.ErrPermission) {
		return failed[T](desktopcontract.FailurePermissionDenied, desktopcontract.ComponentLocalStorage, true, desktopcontract.RecoveryRetry)
	}
	if errors.Is(err, os.ErrNotExist) {
		return failed[T](desktopcontract.FailureProjectMissing, desktopcontract.ComponentLocalStorage, true, desktopcontract.RecoveryLocate)
	}
	return failed[T](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentLocalStorage, true, desktopcontract.RecoveryRetry)
}

func (a *Application) SetEphemeralState(input EphemeralStateInput) desktopcontract.Result[CloseAssessment] {
	if input.Dirty && !validRecoveryArtifact(input.Recovery) || input.Dirty && input.Recovery == nil {
		return failed[CloseAssessment](desktopcontract.FailureRecoveryRequired, desktopcontract.ComponentLocalStorage, false, desktopcontract.RecoveryOpenRecovery)
	}
	if err := a.projects.mutate(input.Session, 0, func(state *sessionLifecycle) {
		if input.Dirty {
			state.ephemeralEdits = true
			state.recovery = cloneRecoveryArtifact(input.Recovery)
			return
		}
		if state.recovery != nil && state.recovery.Kind == RecoveryEditorState {
			state.recovery = nil
		}
		state.ephemeralEdits = state.pendingPreview
	}); err != nil {
		return failed[CloseAssessment](desktopcontract.FailureProjectConflict, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
	}
	assessment, _ := a.projects.assessment(input.Session)
	return desktopcontract.Result[CloseAssessment]{Outcome: protocolcommon.OutcomeSuccess, Value: assessment}
}

func (a *Application) PrepareClose(session runtimeprotocol.RuntimeSessionRef) desktopcontract.Result[CloseAssessment] {
	assessment, ok := a.projects.assessment(session)
	if !ok {
		return failed[CloseAssessment](desktopcontract.FailureProjectConflict, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
	}
	return desktopcontract.Result[CloseAssessment]{Outcome: protocolcommon.OutcomeSuccess, Value: assessment}
}

func (a *Application) ResolveClose(ctx context.Context, session runtimeprotocol.RuntimeSessionRef, resolution CloseResolution) desktopcontract.Result[CloseResolutionResult] {
	if resolution == CloseKeepOpen {
		return desktopcontract.Result[CloseResolutionResult]{Outcome: protocolcommon.OutcomeSuccess, Value: CloseResolutionResult{Closed: false}}
	}
	if resolution != CloseDiscardEphemeral && resolution != CloseCancelAutosaveDiscard {
		return failed[CloseResolutionResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentRuntime, false, desktopcontract.RecoveryReview)
	}
	if resolution == CloseCancelAutosaveDiscard {
		done, host, requestFailure := a.beginHost(desktopcontract.ComponentRuntime)
		if requestFailure != nil {
			return desktopcontract.Result[CloseResolutionResult]{Outcome: protocolcommon.OutcomeFailed, Failure: requestFailure}
		}
		if err := a.cancelAutosave(host, session); err != nil {
			done()
			return failed[CloseResolutionResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
		}
		done()
	}
	if err := a.projects.mutate(session, 0, func(state *sessionLifecycle) {
		state.pendingPreview = false
		state.ephemeralEdits = false
		state.recovery = nil
		if resolution == CloseCancelAutosaveDiscard {
			state.autosavePending = false
		}
	}); err != nil {
		return failed[CloseResolutionResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentLocalStorage, true, desktopcontract.RecoveryRetry)
	}
	assessment, ok := a.projects.assessment(session)
	if !ok || !assessment.CanClose {
		return failed[CloseResolutionResult](desktopcontract.FailureReconcilePending, desktopcontract.ComponentRuntime, false, desktopcontract.RecoveryReview)
	}
	closed := a.CloseProject(ctx, session)
	if closed.Outcome != protocolcommon.OutcomeSuccess {
		return desktopcontract.Result[CloseResolutionResult]{Outcome: closed.Outcome, Failure: closed.Failure}
	}
	return desktopcontract.Result[CloseResolutionResult]{Outcome: protocolcommon.OutcomeSuccess, Value: CloseResolutionResult{Closed: true}}
}

func (a *Application) PrepareQuit() desktopcontract.Result[QuitAssessment] {
	projects := a.projects.allAssessments()
	result := QuitAssessment{CanQuit: true, Projects: projects}
	for _, project := range projects {
		if !project.CanClose {
			result.CanQuit = false
		}
	}
	return desktopcontract.Result[QuitAssessment]{Outcome: protocolcommon.OutcomeSuccess, Value: result}
}

func (a *Application) RequestWindowClose(ctx context.Context) desktopcontract.Result[QuitAssessment] {
	assessment := a.FenceQuit(ctx)
	if assessment.Outcome != protocolcommon.OutcomeSuccess {
		return assessment
	}
	if err := safeWindowRequestClose(ctx, a.config.Window); err != nil {
		a.shutdown.Lock()
		a.rollbackQuitFence()
		a.shutdown.Unlock()
		return failed[QuitAssessment](desktopcontract.FailureShutdown, desktopcontract.ComponentBindingShell, true, desktopcontract.RecoveryRetry)
	}
	return assessment
}

// FenceQuit is the native-close entrypoint. It shares the same authoritative
// fence as Shutdown, so there is no gap between UI assessment and teardown.
func (a *Application) FenceQuit(ctx context.Context) desktopcontract.Result[QuitAssessment] {
	a.shutdown.Lock()
	defer a.shutdown.Unlock()
	if requestFailure := a.fenceQuitLocked(ctx); requestFailure != nil {
		projects := a.projects.allAssessments()
		return desktopcontract.Result[QuitAssessment]{Outcome: protocolcommon.OutcomeFailed, Value: QuitAssessment{CanQuit: false, Projects: projects}, Failure: requestFailure}
	}
	assessment := desktopcontract.Result[QuitAssessment]{Outcome: protocolcommon.OutcomeSuccess, Value: QuitAssessment{CanQuit: true, Projects: a.projects.allAssessments()}}
	return assessment
}

func (a *Application) ControlAutosave(ctx context.Context, input runtimeprotocol.AutosaveControlInput) desktopcontract.Result[runtimeprotocol.AutosaveControlResult] {
	done, host, generation, requestFailure := a.beginProject(input.Session, desktopcontract.ComponentRuntime)
	if requestFailure != nil {
		return desktopcontract.Result[runtimeprotocol.AutosaveControlResult]{Outcome: protocolcommon.OutcomeFailed, Failure: requestFailure}
	}
	completion := make(chan localdocument.AutosaveResult, 1)
	value, err := host.ControlAutosaveWithResult(ctx, input, completion)
	if err != nil {
		done()
		return failed[runtimeprotocol.AutosaveControlResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryReview)
	}
	if input.Action == runtimeprotocol.AutosaveActionCancel {
		terminal := localdocument.AutosaveResult{Err: context.Canceled}
		select {
		case terminal = <-completion:
		default:
		}
		if err := a.projects.mutate(input.Session, generation, func(state *sessionLifecycle) {
			state.autosaveGeneration++
			applyAutosaveResult(state, terminal)
		}); err != nil {
			done()
			return failed[runtimeprotocol.AutosaveControlResult](desktopcontract.FailureRecoveryRequired, desktopcontract.ComponentLocalStorage, false, desktopcontract.RecoveryOpenRecovery)
		}
		done()
		return desktopcontract.Result[runtimeprotocol.AutosaveControlResult]{Outcome: protocolcommon.OutcomeSuccess, Value: value}
	}
	var autosaveGeneration uint64
	var scheduledRecovery *RecoveryArtifact
	if value.Scheduled {
		if input.Commit == nil {
			done()
			return failed[runtimeprotocol.AutosaveControlResult](desktopcontract.FailureRecoveryRequired, desktopcontract.ComponentLocalStorage, false, desktopcontract.RecoveryOpenRecovery)
		}
		payload, marshalErr := json.Marshal(input.Commit.OperationBatch)
		if marshalErr != nil || len(payload) > maxRecoveryPayloadBytes {
			_ = a.cancelAutosave(host, input.Session)
			done()
			return failed[runtimeprotocol.AutosaveControlResult](desktopcontract.FailureRecoveryRequired, desktopcontract.ComponentLocalStorage, false, desktopcontract.RecoveryOpenRecovery)
		}
		scheduledRecovery = &RecoveryArtifact{Kind: RecoveryPreviewOperations, Payload: payload}
	}
	if err := a.projects.mutate(input.Session, generation, func(state *sessionLifecycle) {
		state.autosaveGeneration++
		autosaveGeneration = state.autosaveGeneration
		state.autosavePending = value.Scheduled
		if value.Scheduled {
			state.autosave = AutosaveScheduled
			state.ephemeralEdits = true
			state.recovery = cloneRecoveryArtifact(scheduledRecovery)
		} else {
			state.autosave = AutosaveIdle
		}
	}); err != nil {
		if value.Scheduled {
			_ = a.cancelAutosave(host, input.Session)
		}
		a.projects.requireRecovery(input.Session)
		done()
		return failed[runtimeprotocol.AutosaveControlResult](desktopcontract.FailureRecoveryRequired, desktopcontract.ComponentLocalStorage, false, desktopcontract.RecoveryOpenRecovery)
	}
	if value.Scheduled {
		go a.collectAutosave(input.Session, generation, autosaveGeneration, completion, done)
	} else {
		done()
	}
	return desktopcontract.Result[runtimeprotocol.AutosaveControlResult]{Outcome: protocolcommon.OutcomeSuccess, Value: value}
}

func (a *Application) collectAutosave(session runtimeprotocol.RuntimeSessionRef, generation, autosaveGeneration uint64, completion <-chan localdocument.AutosaveResult, done func()) {
	defer done()
	result, ok := <-completion
	if !ok {
		return
	}
	_ = a.projects.completeAutosave(session, generation, autosaveGeneration, func(state *sessionLifecycle) {
		applyAutosaveResult(state, result)
	})
}

func applyAutosaveResult(state *sessionLifecycle, result localdocument.AutosaveResult) {
	state.autosavePending = false
	state.autosave = AutosaveFailed
	if result.Err != nil {
		if errors.Is(result.Err, context.Canceled) {
			state.autosave = AutosaveIdle
			return
		}
		if errors.Is(result.Err, port.ErrConflict) {
			state.autosave = AutosaveConflict
		}
		state.ephemeralEdits = true
		return
	}
	op := result.Result.OperationResult
	if op.CommittedRevision != nil {
		state.committedRevision = op.CommittedRevision.RevisionID
	}
	switch op.Status {
	case runtimeprotocol.OperationResultStatusCommitted,
		runtimeprotocol.OperationResultStatusCommittedExternalPending,
		runtimeprotocol.OperationResultStatusCommittedExternalFailed,
		runtimeprotocol.OperationResultStatusCommittedStateStale:
		terminalRecovery := cloneRecoveryArtifact(state.recovery)
		state.autosave = AutosaveCommitted
		state.pendingPreview = false
		state.ephemeralEdits = false
		state.recovery = nil
		state.providerPending = false
		state.terminalBlocker = ""
		state.terminalRecovery = nil
		switch op.Status {
		case runtimeprotocol.OperationResultStatusCommittedExternalPending:
			state.terminalBlocker = CloseExternalPending
		case runtimeprotocol.OperationResultStatusCommittedExternalFailed:
			state.terminalBlocker = CloseExternalFailed
		case runtimeprotocol.OperationResultStatusCommittedStateStale:
			state.terminalBlocker = CloseStateStale
		}
		if state.terminalBlocker != "" {
			state.terminalRecovery = terminalRecovery
		}
	case runtimeprotocol.OperationResultStatusNeedsReview:
		state.autosave = AutosaveNeedsReview
		state.ephemeralEdits = true
	default:
		state.autosave = AutosaveFailed
		state.ephemeralEdits = true
	}
}

func (a *Application) AutosaveStatus(session runtimeprotocol.RuntimeSessionRef) desktopcontract.Result[CloseAssessment] {
	if a.projects.autosaveRecoveryRequired(session) {
		return failed[CloseAssessment](desktopcontract.FailureRecoveryRequired, desktopcontract.ComponentLocalStorage, false, desktopcontract.RecoveryOpenRecovery)
	}
	return a.PrepareClose(session)
}

func (a *Application) RecoveryCandidates() desktopcontract.Result[[]RecoveryCandidate] {
	return desktopcontract.Result[[]RecoveryCandidate]{Outcome: protocolcommon.OutcomeSuccess, Value: a.projects.recoveries()}
}

func (a *Application) ResolveRecovery(ctx context.Context, projectID runtimeprotocol.DocumentID, choice RecoveryChoice) desktopcontract.Result[ProjectOpenResult] {
	if choice == RecoveryDiscard {
		if err := a.projects.discardRecovery(projectID); err != nil {
			return failed[ProjectOpenResult](desktopcontract.FailureProjectMissing, desktopcontract.ComponentLocalStorage, false, desktopcontract.RecoveryOpenRecovery)
		}
		return desktopcontract.Result[ProjectOpenResult]{Outcome: protocolcommon.OutcomeSuccess, Value: ProjectOpenResult{ProjectID: projectID}}
	}
	if choice != RecoveryRestore {
		return failed[ProjectOpenResult](desktopcontract.FailureRecoveryRequired, desktopcontract.ComponentRuntime, false, desktopcontract.RecoveryOpenRecovery)
	}
	recovery, ok := a.projects.recovery(projectID)
	if !ok {
		return failed[ProjectOpenResult](desktopcontract.FailureProjectMissing, desktopcontract.ComponentLocalStorage, false, desktopcontract.RecoveryOpenRecovery)
	}
	result := a.openRecentProject(ctx, projectID, true)
	if result.Outcome == protocolcommon.OutcomeSuccess {
		recovery.CommittedRevision = result.Value.Open.CommittedRevision.RevisionID
		if err := a.projects.applyRecovery(result.Value.Open.Session, recovery); err != nil {
			_ = a.CloseProject(ctx, result.Value.Open.Session)
			return failed[ProjectOpenResult](desktopcontract.FailureRecoveryRequired, desktopcontract.ComponentLocalStorage, true, desktopcontract.RecoveryOpenRecovery)
		}
		result.Value.Disposition = ProjectRestored
		result.Value.Recovery = cloneRecoveryArtifact(recovery.Recovery)
		if result.Value.Recovery == nil {
			result.Value.Recovery = cloneRecoveryArtifact(recovery.TerminalRecovery)
		}
	}
	return result
}

func (a *Application) ConnectExternal(ctx context.Context, request ExternalConnectionRequest) desktopcontract.Result[ExternalConnection] {
	if a.config.ExternalLifecycle == nil {
		return failed[ExternalConnection](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, false, desktopcontract.RecoveryConfigureAdapter)
	}
	result := safeExternalConnect(ctx, a.config.ExternalLifecycle, request)
	if !result.Validate() || (result.Outcome == protocolcommon.OutcomeSuccess && (result.Value.ConnectionID == "" || result.Value.ProviderID != request.ProviderID)) {
		return failed[ExternalConnection](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryRetry)
	}
	return result
}

func (a *Application) SyncExternal(ctx context.Context, session runtimeprotocol.RuntimeSessionRef, request ExternalSyncRequest) desktopcontract.Result[ExternalSyncResult] {
	if a.config.ExternalLifecycle == nil || request.DocumentID != session.Scope.DocumentID {
		return failed[ExternalSyncResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, false, desktopcontract.RecoveryConfigureAdapter)
	}
	done, _, generation, failure := a.beginProject(session, desktopcontract.ComponentExternalStorage)
	if failure != nil {
		return desktopcontract.Result[ExternalSyncResult]{Outcome: protocolcommon.OutcomeFailed, Failure: failure}
	}
	defer done()
	result := safeExternalSync(ctx, a.config.ExternalLifecycle, request)
	if !result.Validate() || (result.Outcome == protocolcommon.OutcomeSuccess && result.Value.ProviderVersion == "") {
		return failed[ExternalSyncResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryRetry)
	}
	if result.Outcome == protocolcommon.OutcomeSuccess {
		if err := a.projects.mutate(session, generation, func(state *sessionLifecycle) {
			state.providerPending = result.Value.ReconcileNeeded
			if !result.Value.ReconcileNeeded && (state.terminalBlocker == CloseExternalPending || state.terminalBlocker == CloseExternalFailed) {
				state.terminalBlocker = ""
				state.terminalRecovery = nil
			}
		}); err != nil {
			return failed[ExternalSyncResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentLocalStorage, true, desktopcontract.RecoveryRetry)
		}
	}
	return result
}

func (a *Application) ReconcileExternal(ctx context.Context, session runtimeprotocol.RuntimeSessionRef, request ExternalReconcileRequest) desktopcontract.Result[ExternalReconcileResult] {
	if a.config.ExternalLifecycle == nil || request.DocumentID != session.Scope.DocumentID {
		return failed[ExternalReconcileResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, false, desktopcontract.RecoveryConfigureAdapter)
	}
	done, _, generation, failure := a.beginProject(session, desktopcontract.ComponentExternalStorage)
	if failure != nil {
		return desktopcontract.Result[ExternalReconcileResult]{Outcome: protocolcommon.OutcomeFailed, Failure: failure}
	}
	defer done()
	result := safeExternalReconcile(ctx, a.config.ExternalLifecycle, request)
	if !result.Validate() || (result.Outcome == protocolcommon.OutcomeSuccess && result.Value.ProviderVersion == "") {
		return failed[ExternalReconcileResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryRetry)
	}
	if result.Outcome == protocolcommon.OutcomeSuccess {
		if err := a.projects.mutate(session, generation, func(state *sessionLifecycle) {
			state.providerPending = !result.Value.Converged
			if result.Value.Converged && (state.terminalBlocker == CloseExternalPending || state.terminalBlocker == CloseExternalFailed) {
				state.terminalBlocker = ""
				state.terminalRecovery = nil
			}
		}); err != nil {
			return failed[ExternalReconcileResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentLocalStorage, true, desktopcontract.RecoveryRetry)
		}
	}
	return result
}

func safeExternalConnect(ctx context.Context, adapter ExternalLifecycleAdapter, request ExternalConnectionRequest) (result desktopcontract.Result[ExternalConnection]) {
	defer func() {
		if recover() != nil {
			result = failed[ExternalConnection](desktopcontract.FailureBackendPanic, desktopcontract.ComponentExternalStorage, false, desktopcontract.RecoveryExit)
		}
	}()
	return adapter.Connect(ctx, request)
}

func safeExternalSync(ctx context.Context, adapter ExternalLifecycleAdapter, request ExternalSyncRequest) (result desktopcontract.Result[ExternalSyncResult]) {
	defer func() {
		if recover() != nil {
			result = failed[ExternalSyncResult](desktopcontract.FailureBackendPanic, desktopcontract.ComponentExternalStorage, false, desktopcontract.RecoveryExit)
		}
	}()
	return adapter.Sync(ctx, request)
}

func safeExternalReconcile(ctx context.Context, adapter ExternalLifecycleAdapter, request ExternalReconcileRequest) (result desktopcontract.Result[ExternalReconcileResult]) {
	defer func() {
		if recover() != nil {
			result = failed[ExternalReconcileResult](desktopcontract.FailureBackendPanic, desktopcontract.ComponentExternalStorage, false, desktopcontract.RecoveryExit)
		}
	}()
	return adapter.Reconcile(ctx, request)
}
