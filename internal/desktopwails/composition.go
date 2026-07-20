// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"errors"
	"sync"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
)

const recoveryEvent = "layerdraw:desktop-recovery"
const lifecycleEvent = "layerdraw:desktop-lifecycle"

type LifecycleAdapter struct{ runtime NativeRuntime }

func (a LifecycleAdapter) Publish(ctx context.Context, event desktopcontract.LifecycleEvent) error {
	a.runtime.Emit(ctx, lifecycleEvent, event)
	return nil
}

// RecoveryReporter publishes only the closed Desktop failure vocabulary.
// Native paths, provider errors, panic values, and credentials cannot enter it.
type RecoveryReporter struct{ runtime NativeRuntime }

func (r RecoveryReporter) Report(ctx context.Context, failure desktopcontract.Failure) {
	if failure.Validate() {
		r.runtime.Emit(ctx, recoveryEvent, failure)
	}
}

// ExternalProvider is implemented by separately packaged provider adapters.
// ExternalAdapter keeps selection and SDK details behind a typed registry.
type ExternalProvider interface {
	Connect(context.Context, desktopapp.ExternalConnectionRequest) (desktopapp.ExternalConnection, error)
	Sync(context.Context, desktopapp.ExternalSyncRequest) (desktopapp.ExternalSyncResult, error)
	Reconcile(context.Context, desktopapp.ExternalReconcileRequest) (desktopapp.ExternalReconcileResult, error)
}

type ExternalAdapter struct {
	mu          sync.RWMutex
	providers   map[string]ExternalProvider
	connections map[string]string
}

func NewExternalAdapter(providers map[string]ExternalProvider) *ExternalAdapter {
	copy := make(map[string]ExternalProvider, len(providers))
	for id, provider := range providers {
		if id != "" && provider != nil {
			copy[id] = provider
		}
	}
	return &ExternalAdapter{providers: copy, connections: map[string]string{}}
}

func (a *ExternalAdapter) Connect(ctx context.Context, request desktopapp.ExternalConnectionRequest) desktopcontract.Result[desktopapp.ExternalConnection] {
	a.mu.RLock()
	provider := a.providers[request.ProviderID]
	a.mu.RUnlock()
	if provider == nil {
		return externalFailure[desktopapp.ExternalConnection](false, desktopcontract.RecoveryConfigureAdapter)
	}
	connection, err := provider.Connect(ctx, request)
	if err != nil || connection.ConnectionID == "" || connection.ProviderID != request.ProviderID {
		return externalFailure[desktopapp.ExternalConnection](true, desktopcontract.RecoveryRetry)
	}
	a.mu.Lock()
	a.connections[connection.ConnectionID] = request.ProviderID
	a.mu.Unlock()
	return desktopcontract.Result[desktopapp.ExternalConnection]{Outcome: protocolcommon.OutcomeSuccess, Value: connection}
}

func (a *ExternalAdapter) Sync(ctx context.Context, request desktopapp.ExternalSyncRequest) desktopcontract.Result[desktopapp.ExternalSyncResult] {
	provider := a.providerFor(request.ConnectionID)
	if provider == nil {
		return externalFailure[desktopapp.ExternalSyncResult](false, desktopcontract.RecoveryConfigureAdapter)
	}
	value, err := provider.Sync(ctx, request)
	if err != nil || value.ProviderVersion == "" {
		return externalFailure[desktopapp.ExternalSyncResult](true, desktopcontract.RecoveryRetry)
	}
	return desktopcontract.Result[desktopapp.ExternalSyncResult]{Outcome: protocolcommon.OutcomeSuccess, Value: value}
}

func (a *ExternalAdapter) Reconcile(ctx context.Context, request desktopapp.ExternalReconcileRequest) desktopcontract.Result[desktopapp.ExternalReconcileResult] {
	provider := a.providerFor(request.ConnectionID)
	if provider == nil {
		return externalFailure[desktopapp.ExternalReconcileResult](false, desktopcontract.RecoveryConfigureAdapter)
	}
	value, err := provider.Reconcile(ctx, request)
	if err != nil || value.ProviderVersion == "" {
		return externalFailure[desktopapp.ExternalReconcileResult](true, desktopcontract.RecoveryRetry)
	}
	return desktopcontract.Result[desktopapp.ExternalReconcileResult]{Outcome: protocolcommon.OutcomeSuccess, Value: value}
}

func (a *ExternalAdapter) providerFor(connectionID string) ExternalProvider {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.providers[a.connections[connectionID]]
}

func externalFailure[T any](retryable bool, recovery desktopcontract.RecoveryAction) desktopcontract.Result[T] {
	return desktopcontract.Result[T]{Outcome: protocolcommon.OutcomeFailed, Failure: &desktopcontract.Failure{Code: desktopcontract.FailureAdapterUnavailable, Component: desktopcontract.ComponentExternalStorage, Retryable: retryable, Recovery: recovery}}
}

// Compose installs the concrete Wails/native adapters into the shared Desktop
// composition and is the production, non-test caller of desktopapp.New.
func Compose(base desktopapp.Config, runtime NativeRuntime, providers map[string]ExternalProvider) (*desktopapp.Application, error) {
	application, _, err := compose(base, runtime, providers)
	return application, err
}

func compose(base desktopapp.Config, runtime NativeRuntime, providers map[string]ExternalProvider) (*desktopapp.Application, *selectionVault, error) {
	if runtime == nil {
		return nil, nil, errors.New("desktop Wails runtime is unavailable")
	}
	vault := newSelectionVault()
	base.Lifecycle = LifecycleAdapter{runtime: runtime}
	base.Window = WindowAdapter{runtime: runtime}
	base.Dialogs = NewDialogAdapter(runtime, vault)
	base.ProjectStorage = NewProjectStorageAdapter(vault)
	base.Recovery = RecoveryReporter{runtime: runtime}
	if len(providers) != 0 {
		external := NewExternalAdapter(providers)
		base.ExternalLifecycle = external
		if base.Adapters == nil {
			base.Adapters = map[desktopcontract.ComponentID]desktopapp.Adapter{}
		}
		if base.Adapters[desktopcontract.ComponentExternalStorage] == nil {
			return nil, nil, errors.New("external lifecycle requires its capability adapter")
		}
	}
	application, err := desktopapp.New(base)
	return application, vault, err
}
