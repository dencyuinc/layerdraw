// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"path/filepath"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/dencyuinc/layerdraw/internal/localdocument"
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
	if err != nil || location.Root == "" || !filepath.IsAbs(location.Root) || filepath.Clean(location.Root) != location.Root {
		return failed[ProjectOpenResult](desktopcontract.FailureAdapterUnavailable, component, true, desktopcontract.RecoveryRetry)
	}
	opened, err := host.OpenProject(ctx, localdocument.OpenProjectInput{Root: location.Root, EntryPath: location.EntryPath})
	if err != nil {
		return failed[ProjectOpenResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryOpenRecovery)
	}
	return desktopcontract.Result[ProjectOpenResult]{Outcome: protocolcommon.OutcomeSuccess, Value: ProjectOpenResult{Open: opened.Session.Open, History: opened.History}}
}

func (a *Application) ReloadProject(ctx context.Context, documentID runtimeprotocol.DocumentID) (result desktopcontract.Result[ProjectOpenResult]) {
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
		return failed[ProjectOpenResult](desktopcontract.FailureReconnect, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryOpenRecovery)
	}
	return desktopcontract.Result[ProjectOpenResult]{Outcome: protocolcommon.OutcomeSuccess, Value: ProjectOpenResult{Open: opened.Session.Open, History: opened.History}}
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
		return failed[runtimeprotocol.PreviewOperationsResult](desktopcontract.FailureReconnect, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
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
		return failed[runtimeprotocol.RuntimeCommitResult](desktopcontract.FailureReconnect, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
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
	tracked, err := host.SessionFor(session)
	if err != nil || host.Close(ctx, tracked) != nil {
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
