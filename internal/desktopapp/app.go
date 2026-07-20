// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"errors"
	"path/filepath"
	"sync"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/dencyuinc/layerdraw/internal/localdocument"
)

var startupOrder = []desktopcontract.ComponentID{
	desktopcontract.ComponentLocalStorage,
	desktopcontract.ComponentAccess,
	desktopcontract.ComponentEngine,
	desktopcontract.ComponentRuntime,
	desktopcontract.ComponentNativeQuery,
	desktopcontract.ComponentSearchIndex,
	desktopcontract.ComponentEmbeddingProvider,
	desktopcontract.ComponentRegistryClient,
	desktopcontract.ComponentReview,
	desktopcontract.ComponentNativeExporters,
	desktopcontract.ComponentExternalStorage,
	desktopcontract.ComponentMCPHost,
	desktopcontract.ComponentBindingShell,
}

var injectedComponents = []desktopcontract.ComponentID{
	desktopcontract.ComponentNativeQuery,
	desktopcontract.ComponentSearchIndex,
	desktopcontract.ComponentEmbeddingProvider,
	desktopcontract.ComponentRegistryClient,
	desktopcontract.ComponentReview,
	desktopcontract.ComponentNativeExporters,
	desktopcontract.ComponentMCPHost,
	desktopcontract.ComponentBindingShell,
}

type Config struct {
	Root                  string
	ReleaseVersion        protocolcommon.ReleaseVersion
	EndpointInstanceID    protocolcommon.EndpointInstanceID
	ReleaseManifestDigest protocolcommon.Digest
	LocalActor            accesscore.LocalActorResolver
	Lifecycle             desktopcontract.LifecyclePort
	ProjectStorage        ProjectStorage
	Capabilities          CapabilityNegotiator
	Bindings              desktopcontract.ClientSet
	Adapters              map[desktopcontract.ComponentID]Adapter
	Recovery              RecoveryReporter
}

// ProjectOpenResult contains generated Runtime values only. The local session
// pointer and native project location remain inside the trusted backend.
type ProjectOpenResult struct {
	Open    runtimeprotocol.OpenRuntimeDocumentResult `json:"open"`
	History runtimeprotocol.RevisionPage              `json:"history"`
}

// Application is safe for concurrent Wails calls.
type Application struct {
	config Config

	mu        sync.Mutex
	shutdown  sync.Mutex
	state     desktopcontract.LifecycleState
	host      *localdocument.Host
	started   []desktopcontract.ComponentID
	handshake protocolcommon.HandshakeResult
	inflight  sync.WaitGroup
}

func New(config Config) (*Application, error) {
	if config.Root == "" || !filepath.IsAbs(config.Root) || config.Lifecycle == nil ||
		config.ProjectStorage == nil || config.Capabilities == nil || config.LocalActor == nil {
		return nil, errors.New("desktop composition is incomplete")
	}
	if err := config.Bindings.Validate(); err != nil {
		return nil, errors.New("desktop binding composition is incomplete")
	}
	if config.Adapters == nil {
		return nil, errors.New("desktop adapter composition is incomplete")
	}
	for _, id := range injectedComponents {
		if config.Adapters[id] == nil {
			return nil, errors.New("desktop required adapter is unavailable")
		}
	}
	return &Application{config: config, state: desktopcontract.LifecycleStopped}, nil
}

func (a *Application) State() desktopcontract.LifecycleState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

func (a *Application) Handshake() (protocolcommon.HandshakeResult, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state != desktopcontract.LifecycleReady {
		return protocolcommon.HandshakeResult{}, false
	}
	encoded, err := protocolcommon.EncodeHandshakeResult(a.handshake)
	if err != nil {
		return protocolcommon.HandshakeResult{}, false
	}
	result, err := protocolcommon.DecodeHandshakeResult(encoded)
	return result, err == nil
}

func (a *Application) Start(ctx context.Context) desktopcontract.Result[protocolcommon.HandshakeResult] {
	a.mu.Lock()
	if a.state != desktopcontract.LifecycleStopped || len(a.started) != 0 {
		a.mu.Unlock()
		return failed[protocolcommon.HandshakeResult](desktopcontract.FailureStartup, desktopcontract.ComponentBindingShell, false, desktopcontract.RecoveryExit)
	}
	a.state = desktopcontract.LifecycleStarting
	a.mu.Unlock()
	if err := a.config.Lifecycle.Publish(ctx, desktopcontract.LifecycleEvent{State: desktopcontract.LifecycleStarting}); err != nil {
		return a.failStart(ctx, desktopcontract.ComponentBindingShell, false)
	}
	actor, err := a.config.LocalActor.ResolveLocalActor(ctx)
	if err != nil || actor.Kind != "user" || actor.ActorID == "" {
		return a.failStartWith(ctx, desktopcontract.FailureLocalActor, desktopcontract.ComponentAccess, true, desktopcontract.RecoveryRetry)
	}
	resolvedActor := accesscore.StaticLocalActorResolver{ActorID: actor.ActorID}

	for _, id := range startupOrder {
		if id == desktopcontract.ComponentMCPHost || id == desktopcontract.ComponentBindingShell {
			continue
		}
		if id == desktopcontract.ComponentExternalStorage && a.config.Adapters[id] == nil {
			continue
		}
		if isCore(id) {
			if id == desktopcontract.ComponentLocalStorage {
				host, err := localdocument.New(localdocument.Config{
					Root: a.config.Root, ReleaseVersion: a.config.ReleaseVersion,
					EndpointInstanceID:    a.config.EndpointInstanceID,
					ReleaseManifestDigest: a.config.ReleaseManifestDigest,
					LocalActor:            resolvedActor,
				})
				if err != nil {
					if errors.Is(err, localdocument.ErrStateRecoveryRequired) {
						return a.enterRecovery(ctx, desktopcontract.ComponentLocalStorage)
					}
					return a.failStartWith(ctx, desktopcontract.FailureStartup, desktopcontract.ComponentLocalStorage, true, desktopcontract.RecoveryRetry)
				}
				a.mu.Lock()
				a.host = host
				a.mu.Unlock()
			}
			a.started = append(a.started, id)
			continue
		}
		if err := safeAdapterStart(ctx, a.config.Adapters[id]); err != nil {
			code := desktopcontract.FailureStartup
			recovery := desktopcontract.RecoveryRetry
			if id == desktopcontract.ComponentMCPHost {
				code, recovery = desktopcontract.FailureMCPTransport, desktopcontract.RecoveryReconnect
			}
			return a.failStartWith(ctx, code, id, true, recovery)
		}
		a.started = append(a.started, id)
	}

	handshake, err := a.config.Capabilities.Negotiate(ctx, desktopcontract.DefaultManifest())
	if err != nil {
		return a.failStartWith(ctx, desktopcontract.FailureProtocolIncompatible, desktopcontract.ComponentBindingShell, false, desktopcontract.RecoveryUpgrade)
	}
	negotiated := desktopcontract.NegotiateCapabilities(desktopcontract.DefaultManifest(), handshake)
	if negotiated.Outcome != protocolcommon.OutcomeSuccess {
		failure := *negotiated.Failure
		return a.failStartWith(ctx, failure.Code, failure.Component, failure.Retryable, failure.Recovery)
	}
	if !a.externalCapabilityMatches(negotiated.Value) {
		return a.failStartWith(ctx, desktopcontract.FailureProtocolIncompatible, desktopcontract.ComponentExternalStorage, false, desktopcontract.RecoveryConfigureAdapter)
	}
	for _, id := range []desktopcontract.ComponentID{desktopcontract.ComponentMCPHost, desktopcontract.ComponentBindingShell} {
		if err := safeAdapterStart(ctx, a.config.Adapters[id]); err != nil {
			code, recovery := desktopcontract.FailureStartup, desktopcontract.RecoveryRetry
			if id == desktopcontract.ComponentMCPHost {
				code, recovery = desktopcontract.FailureMCPTransport, desktopcontract.RecoveryReconnect
			}
			return a.failStartWith(ctx, code, id, true, recovery)
		}
		a.started = append(a.started, id)
	}
	a.mu.Lock()
	a.handshake = negotiated.Value
	a.state = desktopcontract.LifecycleReady
	a.mu.Unlock()
	if err := a.config.Lifecycle.Publish(ctx, desktopcontract.LifecycleEvent{State: desktopcontract.LifecycleReady}); err != nil {
		return a.failStart(ctx, desktopcontract.ComponentBindingShell, true)
	}
	return desktopcontract.Result[protocolcommon.HandshakeResult]{Outcome: protocolcommon.OutcomeSuccess, Value: negotiated.Value}
}

func (a *Application) externalCapabilityMatches(handshake protocolcommon.HandshakeResult) bool {
	wired := a.config.Adapters[desktopcontract.ComponentExternalStorage] != nil
	for _, status := range handshake.CapabilityStatuses {
		if status.CapabilityID == desktopcontract.CapabilityExternalStorage {
			return status.Enabled == wired
		}
	}
	return false
}

func (a *Application) Shutdown(ctx context.Context) desktopcontract.Result[struct{}] {
	a.shutdown.Lock()
	defer a.shutdown.Unlock()
	a.mu.Lock()
	if a.state == desktopcontract.LifecycleStopped && len(a.started) == 0 {
		a.mu.Unlock()
		return desktopcontract.Result[struct{}]{Outcome: protocolcommon.OutcomeSuccess}
	}
	if a.state != desktopcontract.LifecycleReady && a.state != desktopcontract.LifecycleDraining && a.state != desktopcontract.LifecycleRecovery {
		a.mu.Unlock()
		return failed[struct{}](desktopcontract.FailureShutdown, desktopcontract.ComponentBindingShell, true, desktopcontract.RecoveryRetry)
	}
	a.state = desktopcontract.LifecycleDraining
	a.mu.Unlock()
	_ = a.config.Lifecycle.Publish(context.WithoutCancel(ctx), desktopcontract.LifecycleEvent{State: desktopcontract.LifecycleDraining})

	drained := make(chan struct{})
	go func() { a.inflight.Wait(); close(drained) }()
	select {
	case <-drained:
	case <-ctx.Done():
		return failed[struct{}](desktopcontract.FailureShutdown, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
	}

	var failedComponent desktopcontract.ComponentID
	for index := len(a.started) - 1; index >= 0; index-- {
		id := a.started[index]
		if isCore(id) || a.config.Adapters[id] == nil {
			continue
		}
		if safeAdapterShutdown(ctx, a.config.Adapters[id]) != nil && failedComponent == "" {
			failedComponent = id
		}
	}
	a.mu.Lock()
	host := a.host
	a.mu.Unlock()
	if host != nil && host.Shutdown(ctx) != nil && failedComponent == "" {
		failedComponent = desktopcontract.ComponentRuntime
	}
	a.mu.Lock()
	a.host = nil
	a.started = nil
	a.handshake = protocolcommon.HandshakeResult{}
	a.state = desktopcontract.LifecycleStopped
	a.mu.Unlock()
	_ = a.config.Lifecycle.Publish(context.WithoutCancel(ctx), desktopcontract.LifecycleEvent{State: desktopcontract.LifecycleStopped})
	if failedComponent != "" {
		return failed[struct{}](desktopcontract.FailureShutdown, failedComponent, true, desktopcontract.RecoveryRetry)
	}
	return desktopcontract.Result[struct{}]{Outcome: protocolcommon.OutcomeSuccess}
}

func (a *Application) Invoke(ctx context.Context, generatedMethod string, exchange desktopcontract.Exchange) (result desktopcontract.Result[desktopcontract.ExchangeResult]) {
	done, failure := a.begin(desktopcontract.ComponentBindingShell)
	if failure != nil {
		return desktopcontract.Result[desktopcontract.ExchangeResult]{Outcome: protocolcommon.OutcomeFailed, Failure: failure}
	}
	defer done()
	defer func() {
		if recover() != nil {
			result = failed[desktopcontract.ExchangeResult](desktopcontract.FailureBackendPanic, desktopcontract.ComponentBindingShell, false, desktopcontract.RecoveryExit)
		}
	}()
	value, err := a.config.Bindings.Invoke(ctx, generatedMethod, exchange)
	if err != nil {
		return failed[desktopcontract.ExchangeResult](desktopcontract.FailureProtocolIncompatible, desktopcontract.ComponentBindingShell, false, desktopcontract.RecoveryUpgrade)
	}
	return desktopcontract.Result[desktopcontract.ExchangeResult]{Outcome: protocolcommon.OutcomeSuccess, Value: value}
}

func (a *Application) begin(component desktopcontract.ComponentID) (func(), *desktopcontract.Failure) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state != desktopcontract.LifecycleReady || a.host == nil {
		code, recovery := desktopcontract.FailureReconnect, desktopcontract.RecoveryReconnect
		if a.state == desktopcontract.LifecycleDraining {
			code, recovery = desktopcontract.FailureShutdown, desktopcontract.RecoveryRetry
		}
		value := failure(code, component, true, recovery)
		return func() {}, &value
	}
	a.inflight.Add(1)
	return a.inflight.Done, nil
}

func (a *Application) failStart(ctx context.Context, component desktopcontract.ComponentID, retryable bool) desktopcontract.Result[protocolcommon.HandshakeResult] {
	return a.failStartWith(ctx, desktopcontract.FailureStartup, component, retryable, desktopcontract.RecoveryRetry)
}

func (a *Application) failStartWith(ctx context.Context, code desktopcontract.FailureCode, component desktopcontract.ComponentID, retryable bool, recovery desktopcontract.RecoveryAction) desktopcontract.Result[protocolcommon.HandshakeResult] {
	a.rollback(ctx)
	value := failure(code, component, retryable, recovery)
	a.report(ctx, value)
	return desktopcontract.Result[protocolcommon.HandshakeResult]{Outcome: protocolcommon.OutcomeFailed, Failure: &value}
}

func (a *Application) enterRecovery(ctx context.Context, component desktopcontract.ComponentID) desktopcontract.Result[protocolcommon.HandshakeResult] {
	a.rollbackAdapters(ctx)
	a.mu.Lock()
	a.host = nil
	a.state = desktopcontract.LifecycleRecovery
	a.mu.Unlock()
	_ = a.config.Lifecycle.Publish(context.WithoutCancel(ctx), desktopcontract.LifecycleEvent{State: desktopcontract.LifecycleRecovery})
	value := failure(desktopcontract.FailureStartup, component, false, desktopcontract.RecoveryOpenRecovery)
	a.report(ctx, value)
	return desktopcontract.Result[protocolcommon.HandshakeResult]{Outcome: protocolcommon.OutcomeFailed, Failure: &value}
}

func (a *Application) rollback(ctx context.Context) {
	a.rollbackAdapters(ctx)
	a.mu.Lock()
	if a.host != nil {
		_ = a.host.Shutdown(context.WithoutCancel(ctx))
	}
	a.host = nil
	a.started = nil
	a.state = desktopcontract.LifecycleStopped
	a.mu.Unlock()
	_ = a.config.Lifecycle.Publish(context.WithoutCancel(ctx), desktopcontract.LifecycleEvent{State: desktopcontract.LifecycleStopped})
}

func (a *Application) rollbackAdapters(ctx context.Context) {
	for index := len(a.started) - 1; index >= 0; index-- {
		id := a.started[index]
		if !isCore(id) && a.config.Adapters[id] != nil {
			_ = safeAdapterShutdown(context.WithoutCancel(ctx), a.config.Adapters[id])
		}
	}
}

func (a *Application) report(ctx context.Context, value desktopcontract.Failure) {
	if a.config.Recovery != nil {
		a.config.Recovery.Report(context.WithoutCancel(ctx), value)
	}
}

func isCore(id desktopcontract.ComponentID) bool {
	return id == desktopcontract.ComponentLocalStorage || id == desktopcontract.ComponentAccess || id == desktopcontract.ComponentEngine || id == desktopcontract.ComponentRuntime
}

func safeAdapterStart(ctx context.Context, adapter Adapter) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("adapter start panic")
		}
	}()
	return adapter.Start(ctx)
}

func safeAdapterShutdown(ctx context.Context, adapter Adapter) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("adapter shutdown panic")
		}
	}()
	return adapter.Shutdown(ctx)
}

func failure(code desktopcontract.FailureCode, component desktopcontract.ComponentID, retryable bool, recovery desktopcontract.RecoveryAction) desktopcontract.Failure {
	return desktopcontract.Failure{Code: code, Component: component, Retryable: retryable, Recovery: recovery}
}

func failed[T any](code desktopcontract.FailureCode, component desktopcontract.ComponentID, retryable bool, recovery desktopcontract.RecoveryAction) desktopcontract.Result[T] {
	value := failure(code, component, retryable, recovery)
	return desktopcontract.Result[T]{Outcome: protocolcommon.OutcomeFailed, Failure: &value}
}

func cancelled[T any](component desktopcontract.ComponentID) desktopcontract.Result[T] {
	value := failure(desktopcontract.FailureDialogCancelled, component, false, desktopcontract.RecoveryRetry)
	return desktopcontract.Result[T]{Outcome: protocolcommon.OutcomeCancelled, Failure: &value}
}
