// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/dencyuinc/layerdraw/internal/localdocument"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

func (a *Application) CreateProject(ctx context.Context, selectionToken string) desktopcontract.Result[ProjectOpenResult] {
	return a.openSelected(ctx, desktopcontract.ComponentLocalStorage, selectionToken, a.config.ProjectStorage.Create)
}

func (a *Application) OpenProject(ctx context.Context, selectionToken string) desktopcontract.Result[ProjectOpenResult] {
	return a.openSelected(ctx, desktopcontract.ComponentLocalStorage, selectionToken, a.config.ProjectStorage.Open)
}

func (a *Application) openSelected(ctx context.Context, component desktopcontract.ComponentID, token string, resolve func(context.Context, string) (ProjectLocation, error)) (result desktopcontract.Result[ProjectOpenResult]) {
	// Opening a project crosses the storage, Runtime, native-search, and
	// lifecycle owners. Serialize that publication boundary so concurrent UI
	// or association requests for the same project cannot race native index
	// creation before projectLifecycle can turn the later request into Focused.
	a.projectOpen.Lock()
	defer a.projectOpen.Unlock()
	done, host, requestFailure := a.beginHost(component)
	if requestFailure != nil {
		return desktopcontract.Result[ProjectOpenResult]{Outcome: protocolcommon.OutcomeFailed, Failure: requestFailure}
	}
	defer done()
	defer func() {
		if recover() != nil {
			result = failed[ProjectOpenResult](desktopcontract.FailureBackendPanic, component, false, desktopcontract.RecoveryExit)
		}
	}()
	if token == "" {
		return cancelled[ProjectOpenResult](component)
	}
	location, err := resolve(ctx, token)
	if err != nil {
		a.reportOpenFailure(ctx, "project storage selection", err)
		return mapProjectOpenFailure[ProjectOpenResult](err, component)
	}
	if location.Root == "" || !filepath.IsAbs(location.Root) || filepath.Clean(location.Root) != location.Root {
		return failed[ProjectOpenResult](desktopcontract.FailureAdapterUnavailable, component, true, desktopcontract.RecoveryRetry)
	}
	var opened localdocument.OpenResult
	if location.Kind == "container" {
		if location.PinnedContent != nil {
			opened, err = host.OpenContainerContent(ctx, location.Root, location.PinnedContent, true)
		} else {
			opened, err = host.ImportContainer(ctx, location.Root)
		}
	} else {
		opened, err = host.OpenProject(ctx, localdocument.OpenProjectInput{Root: location.Root, EntryPath: location.EntryPath, PinnedEntry: location.PinnedContent})
	}
	if err != nil {
		a.reportOpenFailure(ctx, "project open", err)
		return mapProjectOpenFailure[ProjectOpenResult](err, desktopcontract.ComponentRuntime)
	}
	// A creation dialog carries the user-chosen title. It is committed through
	// the Engine semantic-operation path before the session is published, so the
	// committed source reflects the name the user typed into the native dialog.
	if location.DisplayName != "" && location.Kind != "container" && opened.ExternalChange == nil {
		history, nameErr := host.CommitProjectDisplayName(ctx, opened.Session, location.DisplayName)
		if nameErr != nil {
			a.reportOpenFailure(ctx, "project display name commit", nameErr)
			_ = host.Close(context.Background(), opened.Session)
			return mapProjectOpenFailure[ProjectOpenResult](nameErr, desktopcontract.ComponentRuntime)
		}
		opened.History = history
		// The tracked recents entry must carry the committed name, not the
		// pre-commit bootstrap title captured when the session opened.
		opened.Session.DisplayName = location.DisplayName
	}
	if a.config.NativeSearchLifecycle != nil {
		if err := a.config.NativeSearchLifecycle.RefreshSearchIndex(ctx, opened.Session); err != nil {
			a.reportOpenFailure(ctx, "native search index refresh", err)
			_ = host.Close(context.Background(), opened.Session)
			return failed[ProjectOpenResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentNativeQuery, true, desktopcontract.RecoveryRetry)
		}
	}
	tracked, disposition, trackErr := a.projects.opened(opened.Session.Open.Session, opened.Session.Open.CommittedRevision, opened.ExternalChange != nil, opened.Session.DisplayName)
	if trackErr != nil {
		a.reportOpenFailure(ctx, "project lifecycle tracking", trackErr)
		_ = host.Close(ctx, opened.Session)
		return failed[ProjectOpenResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentLocalStorage, true, desktopcontract.RecoveryRetry)
	}
	if disposition == ProjectFocused {
		_ = host.Close(ctx, opened.Session)
		existing, existingErr := host.SessionFor(tracked)
		if existingErr != nil {
			return failed[ProjectOpenResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
		}
		_ = safeWindowShow(ctx, a.config.Window)
		return desktopcontract.Result[ProjectOpenResult]{Outcome: protocolcommon.OutcomeSuccess, Value: ProjectOpenResult{Open: existing.Open, ProjectID: existing.Open.Session.Scope.DocumentID, Disposition: disposition}}
	}
	return desktopcontract.Result[ProjectOpenResult]{Outcome: protocolcommon.OutcomeSuccess, Value: ProjectOpenResult{Open: opened.Session.Open, History: opened.History, ProjectID: opened.Session.Open.Session.Scope.DocumentID, Disposition: disposition, ReconcilePending: opened.ExternalChange != nil}}
}

func (a *Application) ReloadProject(ctx context.Context, documentID runtimeprotocol.DocumentID) (result desktopcontract.Result[ProjectOpenResult]) {
	return a.reloadProject(ctx, documentID, false)
}

func (a *Application) reloadProject(ctx context.Context, documentID runtimeprotocol.DocumentID, allowRecovery bool) (result desktopcontract.Result[ProjectOpenResult]) {
	if !allowRecovery && a.projects.hasRecovery(documentID) {
		return failed[ProjectOpenResult](desktopcontract.FailureRecoveryRequired, desktopcontract.ComponentRuntime, false, desktopcontract.RecoveryOpenRecovery)
	}
	done, host, requestFailure := a.beginHost(desktopcontract.ComponentRuntime)
	if requestFailure != nil {
		return desktopcontract.Result[ProjectOpenResult]{Outcome: protocolcommon.OutcomeFailed, Failure: requestFailure}
	}
	defer done()
	defer func() {
		if recover() != nil {
			result = failed[ProjectOpenResult](desktopcontract.FailureBackendPanic, desktopcontract.ComponentRuntime, false, desktopcontract.RecoveryExit)
		}
	}()
	opened, err := host.OpenDocument(ctx, documentID)
	if err != nil {
		a.reportOpenFailure(ctx, "project reload", err)
		return mapProjectOpenFailure[ProjectOpenResult](err, desktopcontract.ComponentRuntime)
	}
	if a.config.NativeSearchLifecycle != nil {
		if err := a.config.NativeSearchLifecycle.RefreshSearchIndex(ctx, opened.Session); err != nil {
			_ = host.Close(context.Background(), opened.Session)
			return failed[ProjectOpenResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentNativeQuery, true, desktopcontract.RecoveryRetry)
		}
	}
	tracked, disposition, trackErr := a.projects.opened(opened.Session.Open.Session, opened.Session.Open.CommittedRevision, opened.ExternalChange != nil, opened.Session.DisplayName)
	if trackErr != nil {
		_ = host.Close(ctx, opened.Session)
		return failed[ProjectOpenResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentLocalStorage, true, desktopcontract.RecoveryRetry)
	}
	if disposition == ProjectFocused && tracked != opened.Session.Open.Session {
		_ = host.Close(ctx, opened.Session)
		existing, existingErr := host.SessionFor(tracked)
		if existingErr != nil {
			return failed[ProjectOpenResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
		}
		return desktopcontract.Result[ProjectOpenResult]{Outcome: protocolcommon.OutcomeSuccess, Value: ProjectOpenResult{Open: existing.Open, ProjectID: documentID, Disposition: disposition}}
	}
	return desktopcontract.Result[ProjectOpenResult]{Outcome: protocolcommon.OutcomeSuccess, Value: ProjectOpenResult{Open: opened.Session.Open, History: opened.History, ProjectID: documentID, Disposition: disposition, ReconcilePending: opened.ExternalChange != nil}}
}

func mapProjectOpenFailure[T any](err error, component desktopcontract.ComponentID) desktopcontract.Result[T] {
	switch {
	case errors.Is(err, os.ErrPermission):
		return failed[T](desktopcontract.FailurePermissionDenied, component, true, desktopcontract.RecoveryRetry)
	case errors.Is(err, os.ErrNotExist), errors.Is(err, port.ErrNotFound):
		return failed[T](desktopcontract.FailureProjectMissing, component, true, desktopcontract.RecoveryLocate)
	case errors.Is(err, localdocument.ErrStateRecoveryRequired):
		return failed[T](desktopcontract.FailureRecoveryRequired, component, false, desktopcontract.RecoveryOpenRecovery)
	case errors.Is(err, localdocument.ErrPortableIdentityChanged), errors.Is(err, port.ErrConflict):
		return failed[T](desktopcontract.FailureProjectConflict, component, false, desktopcontract.RecoveryReview)
	default:
		return failed[T](desktopcontract.FailureAdapterUnavailable, component, true, desktopcontract.RecoveryOpenRecovery)
	}
}

// reportOpenFailure hands the underlying open failure to the backend-only
// diagnostics sink before it is collapsed into the closed failure vocabulary.
func (a *Application) reportOpenFailure(ctx context.Context, stage string, err error) {
	if a.config.OpenDiagnostics == nil || err == nil {
		return
	}
	defer func() { _ = recover() }()
	a.config.OpenDiagnostics(context.WithoutCancel(ctx), stage, err)
}

func (a *Application) Preview(ctx context.Context, input runtimeprotocol.PreviewOperationsInput) (result desktopcontract.Result[runtimeprotocol.PreviewOperationsResult]) {
	done, host, generation, requestFailure := a.beginProject(input.Session, desktopcontract.ComponentRuntime)
	if requestFailure != nil {
		return desktopcontract.Result[runtimeprotocol.PreviewOperationsResult]{Outcome: protocolcommon.OutcomeFailed, Failure: requestFailure}
	}
	defer done()
	defer func() {
		if recover() != nil {
			result = failed[runtimeprotocol.PreviewOperationsResult](desktopcontract.FailureBackendPanic, desktopcontract.ComponentRuntime, false, desktopcontract.RecoveryExit)
		}
	}()
	value, err := host.Preview(ctx, input)
	if err != nil {
		if errors.Is(err, port.ErrConflict) {
			return failed[runtimeprotocol.PreviewOperationsResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentRuntime, false, desktopcontract.RecoveryReview)
		}
		return failed[runtimeprotocol.PreviewOperationsResult](desktopcontract.FailureReconnect, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
	}
	payload, err := json.Marshal(input.OperationBatch)
	if err != nil || len(payload) > maxRecoveryPayloadBytes {
		return failed[runtimeprotocol.PreviewOperationsResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentLocalStorage, false, desktopcontract.RecoveryOpenRecovery)
	}
	if err := a.projects.mutate(input.Session, generation, func(state *sessionLifecycle) {
		state.pendingPreview = true
		state.ephemeralEdits = true
		state.recovery = &RecoveryArtifact{Kind: RecoveryPreviewOperations, Payload: payload}
	}); err != nil {
		return failed[runtimeprotocol.PreviewOperationsResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
	}
	return desktopcontract.Result[runtimeprotocol.PreviewOperationsResult]{Outcome: protocolcommon.OutcomeSuccess, Value: value}
}

// Commit uses Local Runtime's explicit-save path. Runtime and Access regenerate
// and validate the authoritative proof; the Wails shell never classifies the
// operation or makes an authorization decision.
func (a *Application) Commit(ctx context.Context, input runtimeprotocol.RuntimeCommitInput) (result desktopcontract.Result[runtimeprotocol.RuntimeCommitResult]) {
	done, host, generation, requestFailure := a.beginProject(input.Session, desktopcontract.ComponentRuntime)
	if requestFailure != nil {
		return desktopcontract.Result[runtimeprotocol.RuntimeCommitResult]{Outcome: protocolcommon.OutcomeFailed, Failure: requestFailure}
	}
	defer done()
	defer func() {
		if recover() != nil {
			result = failed[runtimeprotocol.RuntimeCommitResult](desktopcontract.FailureBackendPanic, desktopcontract.ComponentRuntime, false, desktopcontract.RecoveryExit)
		}
	}()
	value, err := host.SaveRuntime(ctx, input)
	if err != nil {
		if errors.Is(err, port.ErrConflict) {
			return failed[runtimeprotocol.RuntimeCommitResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentRuntime, false, desktopcontract.RecoveryReview)
		}
		return failed[runtimeprotocol.RuntimeCommitResult](desktopcontract.FailureReconnect, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
	}
	recoveryPayload, marshalErr := json.Marshal(input.OperationBatch)
	if marshalErr != nil || len(recoveryPayload) > maxRecoveryPayloadBytes {
		return failed[runtimeprotocol.RuntimeCommitResult](desktopcontract.FailureRecoveryRequired, desktopcontract.ComponentLocalStorage, false, desktopcontract.RecoveryOpenRecovery)
	}
	if err := a.projects.mutate(input.Session, generation, func(state *sessionLifecycle) {
		if value.OperationResult.CommittedRevision != nil {
			state.committedRevision = value.OperationResult.CommittedRevision.RevisionID
		}
		switch value.OperationResult.Status {
		case runtimeprotocol.OperationResultStatusCommitted,
			runtimeprotocol.OperationResultStatusCommittedExternalPending,
			runtimeprotocol.OperationResultStatusCommittedExternalFailed,
			runtimeprotocol.OperationResultStatusCommittedStateStale:
			state.pendingPreview = false
			state.ephemeralEdits = false
			state.recovery = nil
			state.autosavePending = false
			state.autosave = AutosaveIdle
			state.providerPending = false
			state.terminalBlocker = ""
			state.terminalRecovery = nil
			switch value.OperationResult.Status {
			case runtimeprotocol.OperationResultStatusCommittedExternalPending:
				state.terminalBlocker = CloseExternalPending
			case runtimeprotocol.OperationResultStatusCommittedExternalFailed:
				state.terminalBlocker = CloseExternalFailed
			case runtimeprotocol.OperationResultStatusCommittedStateStale:
				state.terminalBlocker = CloseStateStale
			}
			if state.terminalBlocker != "" {
				state.terminalRecovery = &RecoveryArtifact{Kind: RecoveryPreviewOperations, Payload: recoveryPayload}
			}
		default:
			state.ephemeralEdits = true
		}
	}); err != nil {
		return failed[runtimeprotocol.RuntimeCommitResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentLocalStorage, true, desktopcontract.RecoveryRetry)
	}
	return desktopcontract.Result[runtimeprotocol.RuntimeCommitResult]{Outcome: protocolcommon.OutcomeSuccess, Value: value}
}

func (a *Application) CloseProject(ctx context.Context, session runtimeprotocol.RuntimeSessionRef) (result desktopcontract.Result[runtimeprotocol.CloseDocumentResult]) {
	done, host, requestFailure := a.beginHost(desktopcontract.ComponentRuntime)
	if requestFailure != nil {
		return desktopcontract.Result[runtimeprotocol.CloseDocumentResult]{Outcome: protocolcommon.OutcomeFailed, Failure: requestFailure}
	}
	defer done()
	defer func() {
		if recover() != nil {
			result = failed[runtimeprotocol.CloseDocumentResult](desktopcontract.FailureBackendPanic, desktopcontract.ComponentRuntime, false, desktopcontract.RecoveryExit)
		}
	}()
	assessment, err := a.projects.fenceClose(ctx.Done(), session)
	if err != nil {
		return failed[runtimeprotocol.CloseDocumentResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
	}
	if !assessment.CanClose {
		a.projects.rollbackClose(session)
		return failed[runtimeprotocol.CloseDocumentResult](desktopcontract.FailureReconcilePending, desktopcontract.ComponentRuntime, false, desktopcontract.RecoveryReview)
	}
	tracked, err := host.SessionFor(session)
	if err != nil {
		a.projects.rollbackClose(session)
		return failed[runtimeprotocol.CloseDocumentResult](desktopcontract.FailureReconnect, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
	}
	detached, err := a.projects.detach(session)
	if err != nil {
		a.projects.rollbackClose(session)
		return failed[runtimeprotocol.CloseDocumentResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentLocalStorage, true, desktopcontract.RecoveryRetry)
	}
	if err := a.closeProjectSession(ctx, host, tracked); err != nil {
		if restoreErr := a.projects.restore(detached); restoreErr != nil {
			return failed[runtimeprotocol.CloseDocumentResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentLocalStorage, true, desktopcontract.RecoveryOpenRecovery)
		}
		return failed[runtimeprotocol.CloseDocumentResult](desktopcontract.FailureReconnect, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
	}
	return desktopcontract.Result[runtimeprotocol.CloseDocumentResult]{Outcome: protocolcommon.OutcomeSuccess, Value: runtimeprotocol.CloseDocumentResult{Closed: true}}
}

func (a *Application) beginHost(component desktopcontract.ComponentID) (func(), *localdocument.Host, *desktopcontract.Failure) {
	done, requestFailure := a.begin(component)
	if requestFailure != nil {
		return done, nil, requestFailure
	}
	a.mu.Lock()
	host := a.host
	a.mu.Unlock()
	if host == nil {
		done()
		value := failure(desktopcontract.FailureReconnect, component, true, desktopcontract.RecoveryReconnect)
		return func() {}, nil, &value
	}
	return done, host, nil
}
