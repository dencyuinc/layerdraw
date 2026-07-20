// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
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
		return mapProjectOpenFailure[ProjectOpenResult](err, component)
	}
	if location.Root == "" || !filepath.IsAbs(location.Root) || filepath.Clean(location.Root) != location.Root {
		return failed[ProjectOpenResult](desktopcontract.FailureAdapterUnavailable, component, true, desktopcontract.RecoveryRetry)
	}
	var opened localdocument.OpenResult
	if location.Kind == "container" {
		opened, err = host.ImportContainer(ctx, location.Root)
	} else {
		opened, err = host.OpenProject(ctx, localdocument.OpenProjectInput{Root: location.Root, EntryPath: location.EntryPath})
	}
	if err != nil {
		return mapProjectOpenFailure[ProjectOpenResult](err, desktopcontract.ComponentRuntime)
	}
	tracked, disposition, trackErr := a.projects.opened(opened.Session.Open.Session, opened.Session.Open.CommittedRevision, opened.ExternalChange != nil)
	if trackErr != nil {
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
		return mapProjectOpenFailure[ProjectOpenResult](err, desktopcontract.ComponentRuntime)
	}
	tracked, disposition, trackErr := a.projects.opened(opened.Session.Open.Session, opened.Session.Open.CommittedRevision, opened.ExternalChange != nil)
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
	case errors.Is(err, port.ErrConflict):
		return failed[T](desktopcontract.FailureProjectConflict, component, false, desktopcontract.RecoveryReview)
	default:
		return failed[T](desktopcontract.FailureAdapterUnavailable, component, true, desktopcontract.RecoveryOpenRecovery)
	}
}

func (a *Application) Preview(ctx context.Context, input runtimeprotocol.PreviewOperationsInput) (result desktopcontract.Result[runtimeprotocol.PreviewOperationsResult]) {
	done, host, requestFailure := a.beginHost(desktopcontract.ComponentRuntime)
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
	if !a.projects.mutate(input.Session, func(state *sessionLifecycle) { state.pendingPreview = true; state.ephemeralEdits = true }) {
		return failed[runtimeprotocol.PreviewOperationsResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
	}
	return desktopcontract.Result[runtimeprotocol.PreviewOperationsResult]{Outcome: protocolcommon.OutcomeSuccess, Value: value}
}

// Commit uses Local Runtime's explicit-save path. Runtime and Access regenerate
// and validate the authoritative proof; the Wails shell never classifies the
// operation or makes an authorization decision.
func (a *Application) Commit(ctx context.Context, input runtimeprotocol.RuntimeCommitInput) (result desktopcontract.Result[runtimeprotocol.RuntimeCommitResult]) {
	done, host, requestFailure := a.beginHost(desktopcontract.ComponentRuntime)
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
	a.projects.mutate(input.Session, func(state *sessionLifecycle) {
		state.pendingPreview = false
		state.ephemeralEdits = false
		state.autosavePending = false
		if value.OperationResult.CommittedRevision != nil {
			state.committedRevision = value.OperationResult.CommittedRevision.RevisionID
		}
		if external := value.OperationResult.ExternalMaterialization; external != nil {
			state.providerPending = external.State != runtimeprotocol.ExternalMaterializationStatePublished
		}
	})
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
	assessment, ok := a.projects.assessment(session)
	if !ok {
		return failed[runtimeprotocol.CloseDocumentResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
	}
	if !assessment.CanClose {
		return failed[runtimeprotocol.CloseDocumentResult](desktopcontract.FailureReconcilePending, desktopcontract.ComponentRuntime, false, desktopcontract.RecoveryReview)
	}
	tracked, err := host.SessionFor(session)
	if err != nil {
		return failed[runtimeprotocol.CloseDocumentResult](desktopcontract.FailureReconnect, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
	}
	detached, err := a.projects.detach(session)
	if err != nil {
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
