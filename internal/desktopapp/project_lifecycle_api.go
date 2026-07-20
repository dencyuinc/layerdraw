// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"errors"
	"os"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/dencyuinc/layerdraw/internal/localdocument"
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
			_ = a.projects.markMissing(projectID, true)
		}
		return result
	}
	_ = a.projects.markMissing(projectID, false)
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
	done, host, requestFailure := a.beginHost(desktopcontract.ComponentRuntime)
	if requestFailure != nil {
		return desktopcontract.Result[ProjectOpenResult]{Outcome: protocolcommon.OutcomeFailed, Failure: requestFailure}
	}
	defer done()
	if err := host.RelocateProject(ctx, projectID, localdocumentInput(location)); err != nil {
		return failed[ProjectOpenResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentRuntime, false, desktopcontract.RecoveryReview)
	}
	_ = a.projects.markMissing(projectID, false)
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
	if !a.projects.mutate(input.Session, func(state *sessionLifecycle) { state.ephemeralEdits = input.Dirty }) {
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
		if err := host.CancelAutosave(session); err != nil {
			done()
			return failed[CloseResolutionResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
		}
		done()
	}
	a.projects.mutate(session, func(state *sessionLifecycle) {
		state.pendingPreview = false
		state.ephemeralEdits = false
		if resolution == CloseCancelAutosaveDiscard {
			state.autosavePending = false
		}
	})
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
	assessment := a.PrepareQuit()
	if !assessment.Value.CanQuit {
		return assessment
	}
	if err := safeWindowRequestClose(ctx, a.config.Window); err != nil {
		return failed[QuitAssessment](desktopcontract.FailureShutdown, desktopcontract.ComponentBindingShell, true, desktopcontract.RecoveryRetry)
	}
	return assessment
}

func (a *Application) ControlAutosave(ctx context.Context, input runtimeprotocol.AutosaveControlInput) desktopcontract.Result[runtimeprotocol.AutosaveControlResult] {
	done, host, requestFailure := a.beginHost(desktopcontract.ComponentRuntime)
	if requestFailure != nil {
		return desktopcontract.Result[runtimeprotocol.AutosaveControlResult]{Outcome: protocolcommon.OutcomeFailed, Failure: requestFailure}
	}
	defer done()
	value, err := host.ControlAutosave(ctx, input)
	if err != nil {
		return failed[runtimeprotocol.AutosaveControlResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryReview)
	}
	a.projects.mutate(input.Session, func(state *sessionLifecycle) { state.autosavePending = value.Scheduled })
	return desktopcontract.Result[runtimeprotocol.AutosaveControlResult]{Outcome: protocolcommon.OutcomeSuccess, Value: value}
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
	result := a.openRecentProject(ctx, projectID, true)
	if result.Outcome == protocolcommon.OutcomeSuccess {
		result.Value.Disposition = ProjectRestored
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
	result := safeExternalSync(ctx, a.config.ExternalLifecycle, request)
	if !result.Validate() || (result.Outcome == protocolcommon.OutcomeSuccess && result.Value.ProviderVersion == "") {
		return failed[ExternalSyncResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryRetry)
	}
	if result.Outcome == protocolcommon.OutcomeSuccess {
		a.projects.mutate(session, func(state *sessionLifecycle) { state.providerPending = result.Value.ReconcileNeeded })
	}
	return result
}

func (a *Application) ReconcileExternal(ctx context.Context, session runtimeprotocol.RuntimeSessionRef, request ExternalReconcileRequest) desktopcontract.Result[ExternalReconcileResult] {
	if a.config.ExternalLifecycle == nil || request.DocumentID != session.Scope.DocumentID {
		return failed[ExternalReconcileResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, false, desktopcontract.RecoveryConfigureAdapter)
	}
	result := safeExternalReconcile(ctx, a.config.ExternalLifecycle, request)
	if !result.Validate() || (result.Outcome == protocolcommon.OutcomeSuccess && result.Value.ProviderVersion == "") {
		return failed[ExternalReconcileResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryRetry)
	}
	if result.Outcome == protocolcommon.OutcomeSuccess {
		a.projects.mutate(session, func(state *sessionLifecycle) { state.providerPending = !result.Value.Converged })
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
