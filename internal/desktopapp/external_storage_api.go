// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
)

func (a *Application) externalStorage() (ExternalStorageAdapter, bool) {
	adapter, ok := a.config.ExternalLifecycle.(ExternalStorageAdapter)
	return adapter, ok
}

func (a *Application) InspectExternal(ctx context.Context, connectionID string) desktopcontract.Result[ExternalConnection] {
	adapter, ok := a.externalStorage()
	if !ok || connectionID == "" {
		return failed[ExternalConnection](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, false, desktopcontract.RecoveryConfigureAdapter)
	}
	return safeExternalCall(func() desktopcontract.Result[ExternalConnection] { return adapter.Inspect(ctx, connectionID) })
}

func (a *Application) RefreshExternal(ctx context.Context, connectionID string) desktopcontract.Result[ExternalConnection] {
	adapter, ok := a.externalStorage()
	if !ok || connectionID == "" {
		return failed[ExternalConnection](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, false, desktopcontract.RecoveryConfigureAdapter)
	}
	return safeExternalCall(func() desktopcontract.Result[ExternalConnection] { return adapter.Refresh(ctx, connectionID) })
}

func (a *Application) DisconnectExternal(ctx context.Context, connectionID string) desktopcontract.Result[ExternalConnection] {
	adapter, ok := a.externalStorage()
	if !ok || connectionID == "" {
		return failed[ExternalConnection](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, false, desktopcontract.RecoveryConfigureAdapter)
	}
	return safeExternalCall(func() desktopcontract.Result[ExternalConnection] { return adapter.Disconnect(ctx, connectionID) })
}

func (a *Application) SelectExternalRemote(ctx context.Context, request ExternalRemoteSelectionRequest) desktopcontract.Result[ExternalBackendBinding] {
	adapter, ok := a.externalStorage()
	if !ok {
		return failed[ExternalBackendBinding](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, false, desktopcontract.RecoveryConfigureAdapter)
	}
	return safeExternalCall(func() desktopcontract.Result[ExternalBackendBinding] { return adapter.SelectRemote(ctx, request) })
}

func (a *Application) AcquireExternalLease(ctx context.Context, session runtimeprotocol.RuntimeSessionRef, binding ExternalBackendBinding) desktopcontract.Result[ExternalLease] {
	adapter, ok := a.externalStorage()
	if !ok || binding.DocumentID != session.Scope.DocumentID {
		return failed[ExternalLease](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, false, desktopcontract.RecoveryConfigureAdapter)
	}
	done, _, _, failure := a.beginProject(session, desktopcontract.ComponentExternalStorage)
	if failure != nil {
		return desktopcontract.Result[ExternalLease]{Outcome: protocolcommon.OutcomeFailed, Failure: failure}
	}
	defer done()
	return safeExternalCall(func() desktopcontract.Result[ExternalLease] { return adapter.AcquireLease(ctx, binding) })
}

func (a *Application) WriteExternal(ctx context.Context, session runtimeprotocol.RuntimeSessionRef, request ExternalWriteRequest) desktopcontract.Result[ExternalWriteResult] {
	adapter, ok := a.externalStorage()
	if !ok || request.Binding.DocumentID != session.Scope.DocumentID || request.Revision.DocumentID != session.Scope.DocumentID {
		return failed[ExternalWriteResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, false, desktopcontract.RecoveryConfigureAdapter)
	}
	done, _, generation, failure := a.beginProject(session, desktopcontract.ComponentExternalStorage)
	if failure != nil {
		return desktopcontract.Result[ExternalWriteResult]{Outcome: protocolcommon.OutcomeFailed, Failure: failure}
	}
	defer done()
	if gate := a.revalidateExternal(ctx, ExternalPublicationIntent{Session: session, Action: "publish_local", Binding: request.Binding, Revision: request.Revision}); gate != nil {
		return desktopcontract.Result[ExternalWriteResult]{Outcome: protocolcommon.OutcomeFailed, Failure: gate}
	}
	result := safeExternalCall(func() desktopcontract.Result[ExternalWriteResult] { return adapter.Write(ctx, request) })
	if !result.Validate() || result.Outcome != protocolcommon.OutcomeSuccess || !validExternalWriteResult(result.Value) {
		return failed[ExternalWriteResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryRetry)
	}
	pending := result.Value.State != ExternalWritePublished
	if err := a.projects.mutate(session, generation, func(state *sessionLifecycle) {
		state.providerPending = pending
		if !pending && (state.terminalBlocker == CloseExternalPending || state.terminalBlocker == CloseExternalFailed) {
			state.terminalBlocker = ""
			state.terminalRecovery = nil
		}
	}); err != nil {
		return failed[ExternalWriteResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentLocalStorage, true, desktopcontract.RecoveryRetry)
	}
	return result
}

func (a *Application) PlanExternalReconcile(ctx context.Context, session runtimeprotocol.RuntimeSessionRef, request ExternalSyncRequest, restricted bool) desktopcontract.Result[ExternalReconcilePlan] {
	adapter, ok := a.externalStorage()
	if !ok || request.DocumentID != session.Scope.DocumentID {
		return failed[ExternalReconcilePlan](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, false, desktopcontract.RecoveryConfigureAdapter)
	}
	done, _, _, failure := a.beginProject(session, desktopcontract.ComponentExternalStorage)
	if failure != nil {
		return desktopcontract.Result[ExternalReconcilePlan]{Outcome: protocolcommon.OutcomeFailed, Failure: failure}
	}
	defer done()
	return safeExternalCall(func() desktopcontract.Result[ExternalReconcilePlan] {
		return adapter.PlanReconcile(ctx, request, restricted)
	})
}

func (a *Application) ApplyExternalReconcile(ctx context.Context, session runtimeprotocol.RuntimeSessionRef, plan ExternalReconcilePlan, resolution string) desktopcontract.Result[ExternalReconcileResult] {
	adapter, ok := a.externalStorage()
	if !ok || plan.Binding.DocumentID != session.Scope.DocumentID || plan.LocalRevision.DocumentID != session.Scope.DocumentID {
		return failed[ExternalReconcileResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, false, desktopcontract.RecoveryConfigureAdapter)
	}
	done, _, generation, failure := a.beginProject(session, desktopcontract.ComponentExternalStorage)
	if failure != nil {
		return desktopcontract.Result[ExternalReconcileResult]{Outcome: protocolcommon.OutcomeFailed, Failure: failure}
	}
	defer done()
	if gate := a.revalidateExternal(ctx, ExternalPublicationIntent{Session: session, Action: resolution, Binding: plan.Binding, Revision: plan.LocalRevision, Plan: &plan}); gate != nil {
		return desktopcontract.Result[ExternalReconcileResult]{Outcome: protocolcommon.OutcomeFailed, Failure: gate}
	}
	result := safeExternalCall(func() desktopcontract.Result[ExternalReconcileResult] {
		return adapter.ApplyReconcile(ctx, plan, resolution)
	})
	if !result.Validate() || result.Outcome != protocolcommon.OutcomeSuccess || result.Value.ProviderVersion == "" {
		return failed[ExternalReconcileResult](desktopcontract.FailureAdapterUnavailable, desktopcontract.ComponentExternalStorage, true, desktopcontract.RecoveryRetry)
	}
	if err := a.projects.mutate(session, generation, func(state *sessionLifecycle) { state.providerPending = !result.Value.Converged }); err != nil {
		return failed[ExternalReconcileResult](desktopcontract.FailureProjectConflict, desktopcontract.ComponentLocalStorage, true, desktopcontract.RecoveryRetry)
	}
	return result
}

func (a *Application) revalidateExternal(ctx context.Context, intent ExternalPublicationIntent) *desktopcontract.Failure {
	if a.config.ExternalPublication == nil {
		value := failed[struct{}](desktopcontract.FailurePermissionDenied, desktopcontract.ComponentAccess, false, desktopcontract.RecoveryReview)
		return value.Failure
	}
	result := safeExternalCall(func() desktopcontract.Result[struct{}] {
		return a.config.ExternalPublication.RevalidateExternalPublication(ctx, intent)
	})
	if !result.Validate() || result.Outcome != protocolcommon.OutcomeSuccess {
		if result.Failure != nil {
			return result.Failure
		}
		value := failed[struct{}](desktopcontract.FailurePermissionDenied, desktopcontract.ComponentAccess, false, desktopcontract.RecoveryReview)
		return value.Failure
	}
	return nil
}

func safeExternalCall[T any](call func() desktopcontract.Result[T]) (result desktopcontract.Result[T]) {
	defer func() {
		if recover() != nil {
			result = failed[T](desktopcontract.FailureBackendPanic, desktopcontract.ComponentExternalStorage, false, desktopcontract.RecoveryExit)
		}
	}()
	return call()
}

func validExternalWriteResult(result ExternalWriteResult) bool {
	switch result.State {
	case ExternalWritePublished:
		return result.ProviderVersion != "" && !result.Retryable
	case ExternalWriteConflict, ExternalWritePartial, ExternalWriteUnknown, ExternalWriteMoved, ExternalWriteOffline:
		return result.Retryable || result.State == ExternalWriteMoved
	default:
		return false
	}
}
