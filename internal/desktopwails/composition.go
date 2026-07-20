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

type ExternalStorageProvider interface {
	ExternalProvider
	Inspect(context.Context, string) (desktopapp.ExternalConnection, error)
	Refresh(context.Context, string) (desktopapp.ExternalConnection, error)
	Disconnect(context.Context, string) (desktopapp.ExternalConnection, error)
	SelectRemote(context.Context, desktopapp.ExternalRemoteSelectionRequest) (desktopapp.ExternalBackendBinding, error)
	AcquireLease(context.Context, desktopapp.ExternalBackendBinding) (desktopapp.ExternalLease, error)
	Write(context.Context, desktopapp.ExternalWriteRequest) (desktopapp.ExternalWriteResult, error)
	PlanReconcile(context.Context, desktopapp.ExternalSyncRequest, bool) (desktopapp.ExternalReconcilePlan, error)
	ApplyReconcile(context.Context, desktopapp.ExternalReconcilePlan, string) (desktopapp.ExternalReconcileResult, error)
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

func (a *ExternalAdapter) storageProvider(connectionID string) ExternalStorageProvider {
	provider, _ := a.providerFor(connectionID).(ExternalStorageProvider)
	return provider
}

func (a *ExternalAdapter) Inspect(ctx context.Context, connectionID string) desktopcontract.Result[desktopapp.ExternalConnection] {
	provider := a.storageProvider(connectionID)
	if provider == nil {
		return externalFailure[desktopapp.ExternalConnection](false, desktopcontract.RecoveryConfigureAdapter)
	}
	value, err := provider.Inspect(ctx, connectionID)
	if err != nil || value.ConnectionID != connectionID || value.ProviderID == "" {
		return externalFailure[desktopapp.ExternalConnection](true, desktopcontract.RecoveryRetry)
	}
	return externalSuccess(value)
}

func (a *ExternalAdapter) Refresh(ctx context.Context, connectionID string) desktopcontract.Result[desktopapp.ExternalConnection] {
	provider := a.storageProvider(connectionID)
	if provider == nil {
		return externalFailure[desktopapp.ExternalConnection](false, desktopcontract.RecoveryConfigureAdapter)
	}
	value, err := provider.Refresh(ctx, connectionID)
	if err != nil || value.ConnectionID != connectionID {
		return externalFailure[desktopapp.ExternalConnection](true, desktopcontract.RecoveryReconnect)
	}
	return externalSuccess(value)
}

func (a *ExternalAdapter) Disconnect(ctx context.Context, connectionID string) desktopcontract.Result[desktopapp.ExternalConnection] {
	provider := a.storageProvider(connectionID)
	if provider == nil {
		return externalFailure[desktopapp.ExternalConnection](false, desktopcontract.RecoveryConfigureAdapter)
	}
	value, err := provider.Disconnect(ctx, connectionID)
	if err != nil || value.ConnectionID != connectionID || value.Status != desktopapp.ExternalConnectionDisconnected {
		return externalFailure[desktopapp.ExternalConnection](true, desktopcontract.RecoveryRetry)
	}
	return externalSuccess(value)
}

func (a *ExternalAdapter) SelectRemote(ctx context.Context, request desktopapp.ExternalRemoteSelectionRequest) desktopcontract.Result[desktopapp.ExternalBackendBinding] {
	provider := a.storageProvider(request.ConnectionID)
	if provider == nil {
		return externalFailure[desktopapp.ExternalBackendBinding](false, desktopcontract.RecoveryConfigureAdapter)
	}
	value, err := provider.SelectRemote(ctx, request)
	if err != nil || value.ConnectionID != request.ConnectionID || value.DocumentID != request.DocumentID || value.BindingID == "" {
		return externalFailure[desktopapp.ExternalBackendBinding](true, desktopcontract.RecoveryRetry)
	}
	return externalSuccess(value)
}

func (a *ExternalAdapter) AcquireLease(ctx context.Context, binding desktopapp.ExternalBackendBinding) desktopcontract.Result[desktopapp.ExternalLease] {
	provider := a.storageProvider(binding.ConnectionID)
	if provider == nil {
		return externalFailure[desktopapp.ExternalLease](false, desktopcontract.RecoveryConfigureAdapter)
	}
	value, err := provider.AcquireLease(ctx, binding)
	if err != nil || value.Token == "" || value.ExpiresAt == "" {
		return externalFailure[desktopapp.ExternalLease](true, desktopcontract.RecoveryRetry)
	}
	return externalSuccess(value)
}

func (a *ExternalAdapter) Write(ctx context.Context, request desktopapp.ExternalWriteRequest) desktopcontract.Result[desktopapp.ExternalWriteResult] {
	provider := a.storageProvider(request.Binding.ConnectionID)
	if provider == nil {
		return externalFailure[desktopapp.ExternalWriteResult](false, desktopcontract.RecoveryConfigureAdapter)
	}
	value, err := provider.Write(ctx, request)
	if err != nil {
		return externalFailure[desktopapp.ExternalWriteResult](true, desktopcontract.RecoveryRetry)
	}
	return externalSuccess(value)
}

func (a *ExternalAdapter) PlanReconcile(ctx context.Context, request desktopapp.ExternalSyncRequest, restricted bool) desktopcontract.Result[desktopapp.ExternalReconcilePlan] {
	provider := a.storageProvider(request.ConnectionID)
	if provider == nil {
		return externalFailure[desktopapp.ExternalReconcilePlan](false, desktopcontract.RecoveryConfigureAdapter)
	}
	value, err := provider.PlanReconcile(ctx, request, restricted)
	if err != nil || value.PlanID == "" || value.Binding.DocumentID != request.DocumentID {
		return externalFailure[desktopapp.ExternalReconcilePlan](true, desktopcontract.RecoveryRetry)
	}
	return externalSuccess(value)
}

func (a *ExternalAdapter) ApplyReconcile(ctx context.Context, plan desktopapp.ExternalReconcilePlan, resolution string) desktopcontract.Result[desktopapp.ExternalReconcileResult] {
	provider := a.storageProvider(plan.Binding.ConnectionID)
	if provider == nil {
		return externalFailure[desktopapp.ExternalReconcileResult](false, desktopcontract.RecoveryConfigureAdapter)
	}
	value, err := provider.ApplyReconcile(ctx, plan, resolution)
	if err != nil || value.ProviderVersion == "" {
		return externalFailure[desktopapp.ExternalReconcileResult](true, desktopcontract.RecoveryRetry)
	}
	return externalSuccess(value)
}

func externalSuccess[T any](value T) desktopcontract.Result[T] {
	return desktopcontract.Result[T]{Outcome: protocolcommon.OutcomeSuccess, Value: value}
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
	nativeInterchange, err := NewNativeInterchangeAdapter(vault, base.Root)
	if err != nil {
		return nil, nil, err
	}
	base.NativeInterchange = nativeInterchange
	if base.Adapters == nil {
		base.Adapters = map[desktopcontract.ComponentID]desktopapp.Adapter{}
	}
	base.Adapters[desktopcontract.ComponentNativeExporters] = nativeInterchange
	filteredDisabled := base.DisabledComponents[:0]
	for _, id := range base.DisabledComponents {
		if id != desktopcontract.ComponentNativeExporters {
			filteredDisabled = append(filteredDisabled, id)
		}
	}
	base.DisabledComponents = filteredDisabled
	base.Recovery = RecoveryReporter{runtime: runtime}
	if len(providers) != 0 {
		external := NewExternalAdapter(providers)
		base.ExternalLifecycle = external
		if base.Adapters[desktopcontract.ComponentExternalStorage] == nil {
			return nil, nil, errors.New("external lifecycle requires its capability adapter")
		}
		capabilities, ok := base.Capabilities.(nativeCapabilities)
		if !ok {
			return nil, nil, errors.New("external lifecycle requires the native capability owner")
		}
		capabilities.externalStorage = true
		base.Capabilities = capabilities
	}
	if base.MCPCapabilities != nil {
		application, err := desktopapp.NewCanonical(base)
		return application, vault, err
	}
	application, err := desktopapp.New(base)
	return application, vault, err
}
