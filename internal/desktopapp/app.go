// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopapp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	"github.com/dencyuinc/layerdraw/internal/localdocument"
	"github.com/dencyuinc/layerdraw/internal/mcphost"
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
	desktopcontract.ComponentBindingShell,
}

type Config struct {
	Root                  string
	ReleaseVersion        protocolcommon.ReleaseVersion
	EndpointInstanceID    protocolcommon.EndpointInstanceID
	ReleaseManifestDigest protocolcommon.Digest
	Lifecycle             desktopcontract.LifecyclePort
	HostPorts             desktopcontract.HostPorts
	// MCPHost is the canonical in-process protocol adapter used by production
	// Desktop composition. HostPorts.MCP remains an injection seam for closed
	// lifecycle tests and framework adapters.
	MCPHost          *mcphost.Host
	MCPCapabilities  MCPCapabilitySource
	MCPResources     MCPResourceSource
	MCPLimits        mcphost.Limits
	CredentialRefs   []desktopcontract.CredentialRef
	DelegationFences []desktopcontract.DelegationFence
	ProjectStorage   ProjectStorage
	Capabilities     CapabilityNegotiator
	Bindings         desktopcontract.ClientSet
	Adapters         map[desktopcontract.ComponentID]Adapter
	Recovery         RecoveryReporter
	Now              func() time.Time
}

// ProjectOpenResult contains generated Runtime values only. The local session
// pointer and native project location remain inside the trusted backend.
type ProjectOpenResult struct {
	Open    runtimeprotocol.OpenRuntimeDocumentResult `json:"open"`
	History runtimeprotocol.RevisionPage              `json:"history"`
}

// BindingResult preserves the generated response outcome. Failure is present
// only for a Desktop shell failure before a trustworthy owner response exists.
type BindingResult struct {
	Outcome protocolcommon.Outcome         `json:"outcome"`
	Value   desktopcontract.ExchangeResult `json:"value,omitempty"`
	Failure *desktopcontract.Failure       `json:"failure,omitempty"`
}

func (r BindingResult) Validate() bool {
	if r.Failure != nil {
		return r.Outcome == protocolcommon.OutcomeFailed && r.Failure.Validate()
	}
	switch r.Outcome {
	case protocolcommon.OutcomeSuccess, protocolcommon.OutcomeRejected, protocolcommon.OutcomeFailed, protocolcommon.OutcomeCancelled:
		return r.Value.Operation != "" && len(r.Value.Control) != 0
	default:
		return false
	}
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
	mcpLocal  *mcphost.LocalTransport
}

func New(config Config) (*Application, error) {
	if config.MCPHost != nil {
		if config.HostPorts.MCP != nil {
			return nil, errors.New("desktop MCP composition is ambiguous")
		}
		config.HostPorts.MCP = BindCanonicalMCPHost(config.MCPHost)
	}
	if config.Root == "" || !filepath.IsAbs(config.Root) || config.Lifecycle == nil ||
		config.ProjectStorage == nil || config.Capabilities == nil || config.HostPorts.Credentials == nil ||
		config.HostPorts.LocalActor == nil || config.HostPorts.LocalOwner == nil ||
		config.HostPorts.Delegations == nil || config.HostPorts.MCP == nil {
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
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Application{config: config, state: desktopcontract.LifecycleStopped}, nil
}

// NewCanonical constructs the production Desktop backend and requires the
// canonical in-process MCP Host. A raw transport lifecycle stub is not a
// production composition.
func NewCanonical(config Config) (*Application, error) {
	if config.MCPHost != nil || config.HostPorts.MCP != nil || config.MCPCapabilities == nil {
		return nil, errors.New("desktop canonical MCP composition is required")
	}
	composition, err := composeCanonicalMCP(config.Bindings, config.MCPCapabilities, config.MCPResources, config.MCPLimits)
	if err != nil {
		return nil, err
	}
	config.MCPHost = composition.host
	app, err := New(config)
	if err != nil {
		return nil, err
	}
	app.mcpLocal = composition.transport
	return app, nil
}

func (a *Application) MCPListTools(ctx context.Context) ([]mcphost.Tool, *mcphost.Failure) {
	if a.mcpLocal == nil {
		return nil, &mcphost.Failure{Code: mcphost.ErrorTransport, Retryable: true}
	}
	return a.mcpLocal.ListTools(ctx)
}
func (a *Application) MCPCallTool(ctx context.Context, request mcphost.CallToolRequest) mcphost.CallToolResult {
	if a.mcpLocal == nil {
		return mcphost.CallToolResult{Failure: &mcphost.Failure{Code: mcphost.ErrorTransport, Retryable: true}}
	}
	return a.mcpLocal.CallTool(ctx, request)
}
func (a *Application) MCPListResources(ctx context.Context) ([]mcphost.Resource, *mcphost.Failure) {
	if a.mcpLocal == nil {
		return nil, &mcphost.Failure{Code: mcphost.ErrorTransport, Retryable: true}
	}
	return a.mcpLocal.ListResources(ctx)
}
func (a *Application) MCPReadResource(ctx context.Context, request mcphost.ReadResourceRequest) mcphost.ReadResourceResult {
	if a.mcpLocal == nil {
		return mcphost.ReadResourceResult{Failure: &mcphost.Failure{Code: mcphost.ErrorTransport, Retryable: true}}
	}
	return a.mcpLocal.ReadResource(ctx, request)
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
	if err := safeLifecyclePublish(ctx, a.config.Lifecycle, desktopcontract.LifecycleEvent{State: desktopcontract.LifecycleStarting}); err != nil {
		code := desktopcontract.FailureStartup
		if errors.Is(err, errInjectedPanic) {
			code = desktopcontract.FailureBackendPanic
		}
		return a.failStartWith(ctx, code, desktopcontract.ComponentBindingShell, false, desktopcontract.RecoveryExit)
	}
	actorResult := safeResolveLocalActor(ctx, a.config.HostPorts.LocalActor)
	if isBackendPanic(actorResult.Failure) {
		return a.failStartWith(ctx, desktopcontract.FailureBackendPanic, desktopcontract.ComponentAccess, false, desktopcontract.RecoveryExit)
	}
	if !actorResult.Validate() || actorResult.Outcome != protocolcommon.OutcomeSuccess || actorResult.Value.Kind != "user" || actorResult.Value.ActorID == "" {
		return a.failStartWith(ctx, desktopcontract.FailureLocalActor, desktopcontract.ComponentAccess, true, desktopcontract.RecoveryRetry)
	}
	if _, err := accessprotocol.EncodeActorRef(actorResult.Value); err != nil {
		return a.failStartWith(ctx, desktopcontract.FailureLocalActor, desktopcontract.ComponentAccess, false, desktopcontract.RecoveryExit)
	}
	resolvedActor := staticActor{actor: actorResult.Value}
	for _, ref := range a.config.CredentialRefs {
		credential := safeResolveCredential(ctx, a.config.HostPorts.Credentials, ref)
		if isBackendPanic(credential.Failure) {
			clear(credential.Value)
			return a.failStartWith(ctx, desktopcontract.FailureBackendPanic, desktopcontract.ComponentAccess, false, desktopcontract.RecoveryExit)
		}
		if !credential.Validate() || credential.Outcome != protocolcommon.OutcomeSuccess || len(credential.Value) == 0 {
			clear(credential.Value)
			return a.failStartWith(ctx, desktopcontract.FailureCredential, desktopcontract.ComponentAccess, true, desktopcontract.RecoveryRetry)
		}
		clear(credential.Value)
	}
	for _, fence := range a.config.DelegationFences {
		now := a.config.Now().UTC()
		grantRequest := desktopcontract.LocalOwnerGrantRequest{
			Actor: actorResult.Value,
			Scope: accessprotocol.HostResourceScope{
				DocumentID:   fence.DocumentID,
				LocalScopeID: fence.LocalScopeID,
			},
			IssuedAt: protocolcommon.Rfc3339Time(now.Format(time.RFC3339Nano)),
		}
		grant := safeIssueLocalOwnerGrant(ctx, a.config.HostPorts.LocalOwner, grantRequest)
		if isBackendPanic(grant.Failure) {
			return a.failStartWith(ctx, desktopcontract.FailureBackendPanic, desktopcontract.ComponentAccess, false, desktopcontract.RecoveryExit)
		}
		if !grant.Validate() || grant.Outcome != protocolcommon.OutcomeSuccess ||
			!desktopcontract.ValidateLocalOwnerGrant(grantRequest, grant.Value, now) {
			return a.failStartWith(ctx, desktopcontract.FailureAgentDelegation, desktopcontract.ComponentAccess, false, desktopcontract.RecoveryOpenRecovery)
		}
		delegation := safeResolveDelegation(ctx, a.config.HostPorts.Delegations, fence)
		if isBackendPanic(delegation.Failure) {
			return a.failStartWith(ctx, desktopcontract.FailureBackendPanic, desktopcontract.ComponentAccess, false, desktopcontract.RecoveryExit)
		}
		if !delegation.Validate() || delegation.Outcome != protocolcommon.OutcomeSuccess ||
			!desktopcontract.ValidateDelegationFence(fence, delegation.Value, grant.Value, now) {
			return a.failStartWith(ctx, desktopcontract.FailureAgentDelegation, desktopcontract.ComponentAccess, false, desktopcontract.RecoveryOpenRecovery)
		}
	}

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
			if errors.Is(err, errInjectedPanic) {
				code, recovery = desktopcontract.FailureBackendPanic, desktopcontract.RecoveryExit
			}
			if id == desktopcontract.ComponentMCPHost {
				code, recovery = desktopcontract.FailureMCPTransport, desktopcontract.RecoveryReconnect
			}
			return a.failStartWith(ctx, code, id, true, recovery)
		}
		a.started = append(a.started, id)
	}

	handshake, err := safeNegotiate(ctx, a.config.Capabilities, desktopcontract.DefaultManifest())
	if err != nil {
		code := desktopcontract.FailureProtocolIncompatible
		if errors.Is(err, errInjectedPanic) {
			code = desktopcontract.FailureBackendPanic
		}
		return a.failStartWith(ctx, code, desktopcontract.ComponentBindingShell, false, desktopcontract.RecoveryUpgrade)
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
		var err error
		if id == desktopcontract.ComponentMCPHost {
			started := safeMCPStart(ctx, a.config.HostPorts.MCP)
			if isBackendPanic(started.Failure) {
				return a.failStartWith(ctx, desktopcontract.FailureBackendPanic, id, false, desktopcontract.RecoveryExit)
			}
			if !started.Validate() || started.Outcome != protocolcommon.OutcomeSuccess {
				err = errors.New("MCP transport start failed")
			}
		} else {
			err = safeAdapterStart(ctx, a.config.Adapters[id])
		}
		if err != nil {
			code, recovery := desktopcontract.FailureStartup, desktopcontract.RecoveryRetry
			if errors.Is(err, errInjectedPanic) {
				code, recovery = desktopcontract.FailureBackendPanic, desktopcontract.RecoveryExit
			}
			if id == desktopcontract.ComponentMCPHost {
				code, recovery = desktopcontract.FailureMCPTransport, desktopcontract.RecoveryReconnect
			}
			return a.failStartWith(ctx, code, id, true, recovery)
		}
		a.started = append(a.started, id)
	}
	if err := safeLifecyclePublish(ctx, a.config.Lifecycle, desktopcontract.LifecycleEvent{State: desktopcontract.LifecycleReady}); err != nil {
		code := desktopcontract.FailureStartup
		if errors.Is(err, errInjectedPanic) {
			code = desktopcontract.FailureBackendPanic
		}
		return a.failStartWith(ctx, code, desktopcontract.ComponentBindingShell, true, desktopcontract.RecoveryRetry)
	}
	a.mu.Lock()
	a.handshake = negotiated.Value
	a.state = desktopcontract.LifecycleReady
	a.mu.Unlock()
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
	if err := safeLifecyclePublish(context.WithoutCancel(ctx), a.config.Lifecycle, desktopcontract.LifecycleEvent{State: desktopcontract.LifecycleDraining}); err != nil {
		code := desktopcontract.FailureShutdown
		if errors.Is(err, errInjectedPanic) {
			code = desktopcontract.FailureBackendPanic
		}
		return failed[struct{}](code, desktopcontract.ComponentBindingShell, true, desktopcontract.RecoveryRetry)
	}

	drained := make(chan struct{})
	go func() { a.inflight.Wait(); close(drained) }()
	select {
	case <-drained:
	case <-ctx.Done():
		return failed[struct{}](desktopcontract.FailureShutdown, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
	}

	for index := len(a.started) - 1; index >= 0; index-- {
		id := a.started[index]
		if isCore(id) || a.config.Adapters[id] == nil {
			if id != desktopcontract.ComponentMCPHost {
				continue
			}
		}
		var err error
		if id == desktopcontract.ComponentMCPHost {
			stopped := safeMCPShutdown(ctx, a.config.HostPorts.MCP)
			if isBackendPanic(stopped.Failure) {
				return failed[struct{}](desktopcontract.FailureBackendPanic, id, false, desktopcontract.RecoveryExit)
			}
			if !stopped.Validate() || stopped.Outcome != protocolcommon.OutcomeSuccess {
				err = errors.New("MCP transport shutdown failed")
			}
		} else {
			err = safeAdapterShutdown(ctx, a.config.Adapters[id])
		}
		if err != nil {
			code, recovery := desktopcontract.FailureShutdown, desktopcontract.RecoveryRetry
			if errors.Is(err, errInjectedPanic) {
				code, recovery = desktopcontract.FailureBackendPanic, desktopcontract.RecoveryExit
			}
			return failed[struct{}](code, id, true, recovery)
		}
	}
	a.mu.Lock()
	host := a.host
	a.mu.Unlock()
	if host != nil && host.Shutdown(ctx) != nil {
		return failed[struct{}](desktopcontract.FailureShutdown, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
	}
	a.mu.Lock()
	a.host = nil
	a.started = nil
	a.handshake = protocolcommon.HandshakeResult{}
	a.state = desktopcontract.LifecycleStopped
	a.mu.Unlock()
	if err := safeLifecyclePublish(context.WithoutCancel(ctx), a.config.Lifecycle, desktopcontract.LifecycleEvent{State: desktopcontract.LifecycleStopped}); err != nil {
		code := desktopcontract.FailureShutdown
		if errors.Is(err, errInjectedPanic) {
			code = desktopcontract.FailureBackendPanic
		}
		return failed[struct{}](code, desktopcontract.ComponentBindingShell, false, desktopcontract.RecoveryExit)
	}
	return desktopcontract.Result[struct{}]{Outcome: protocolcommon.OutcomeSuccess}
}

func (a *Application) Invoke(ctx context.Context, generatedMethod string, exchange desktopcontract.Exchange) (result BindingResult) {
	done, failure := a.begin(desktopcontract.ComponentBindingShell)
	if failure != nil {
		return BindingResult{Outcome: protocolcommon.OutcomeFailed, Failure: failure}
	}
	defer done()
	defer func() {
		if recover() != nil {
			result = failedBinding(desktopcontract.FailureBackendPanic, desktopcontract.ComponentBindingShell, false, desktopcontract.RecoveryExit)
		}
	}()
	value, err := a.config.Bindings.Invoke(ctx, generatedMethod, exchange)
	if err != nil {
		return failedBinding(desktopcontract.FailureProtocolIncompatible, desktopcontract.ComponentBindingShell, false, desktopcontract.RecoveryUpgrade)
	}
	outcome, err := exchangeOutcome(value.Control)
	if err != nil {
		return failedBinding(desktopcontract.FailureProtocolIncompatible, desktopcontract.ComponentBindingShell, false, desktopcontract.RecoveryUpgrade)
	}
	return BindingResult{Outcome: outcome, Value: value}
}

func exchangeOutcome(control []byte) (protocolcommon.Outcome, error) {
	var value struct {
		Outcome protocolcommon.Outcome `json:"outcome"`
	}
	if err := json.Unmarshal(control, &value); err != nil {
		return "", err
	}
	switch value.Outcome {
	case protocolcommon.OutcomeSuccess, protocolcommon.OutcomeRejected, protocolcommon.OutcomeFailed, protocolcommon.OutcomeCancelled:
		return value.Outcome, nil
	default:
		return "", errors.New("invalid generated response outcome")
	}
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

func (a *Application) failStartWith(ctx context.Context, code desktopcontract.FailureCode, component desktopcontract.ComponentID, retryable bool, recovery desktopcontract.RecoveryAction) desktopcontract.Result[protocolcommon.HandshakeResult] {
	value := failure(code, component, retryable, recovery)
	a.report(ctx, value)
	if cleanup := a.rollback(ctx); cleanup != nil {
		a.report(ctx, *cleanup)
		return desktopcontract.Result[protocolcommon.HandshakeResult]{Outcome: protocolcommon.OutcomeFailed, Failure: cleanup}
	}
	return desktopcontract.Result[protocolcommon.HandshakeResult]{Outcome: protocolcommon.OutcomeFailed, Failure: &value}
}

func (a *Application) enterRecovery(ctx context.Context, component desktopcontract.ComponentID) desktopcontract.Result[protocolcommon.HandshakeResult] {
	if cleanup := a.rollback(ctx); cleanup != nil {
		a.report(ctx, *cleanup)
		return desktopcontract.Result[protocolcommon.HandshakeResult]{Outcome: protocolcommon.OutcomeFailed, Failure: cleanup}
	}
	a.mu.Lock()
	a.state = desktopcontract.LifecycleRecovery
	a.mu.Unlock()
	_ = safeLifecyclePublish(context.WithoutCancel(ctx), a.config.Lifecycle, desktopcontract.LifecycleEvent{State: desktopcontract.LifecycleRecovery})
	value := failure(desktopcontract.FailureStartup, component, false, desktopcontract.RecoveryOpenRecovery)
	a.report(ctx, value)
	return desktopcontract.Result[protocolcommon.HandshakeResult]{Outcome: protocolcommon.OutcomeFailed, Failure: &value}
}

func (a *Application) rollback(ctx context.Context) *desktopcontract.Failure {
	a.shutdown.Lock()
	defer a.shutdown.Unlock()
	a.mu.Lock()
	a.state = desktopcontract.LifecycleDraining
	a.mu.Unlock()
	cleanupCtx := context.WithoutCancel(ctx)
	for index := len(a.started) - 1; index >= 0; index-- {
		id := a.started[index]
		if isCore(id) {
			continue
		}
		if id == desktopcontract.ComponentMCPHost {
			stopped := safeMCPShutdown(cleanupCtx, a.config.HostPorts.MCP)
			if isBackendPanic(stopped.Failure) {
				value := failure(desktopcontract.FailureBackendPanic, id, false, desktopcontract.RecoveryExit)
				return &value
			}
			if !stopped.Validate() || stopped.Outcome != protocolcommon.OutcomeSuccess {
				value := failure(desktopcontract.FailureShutdown, id, true, desktopcontract.RecoveryRetry)
				return &value
			}
			continue
		}
		if adapter := a.config.Adapters[id]; adapter != nil {
			if err := safeAdapterShutdown(cleanupCtx, adapter); err != nil {
				code, retryable, recovery := desktopcontract.FailureShutdown, true, desktopcontract.RecoveryRetry
				if errors.Is(err, errInjectedPanic) {
					code, retryable, recovery = desktopcontract.FailureBackendPanic, false, desktopcontract.RecoveryExit
				}
				value := failure(code, id, retryable, recovery)
				return &value
			}
		}
	}
	a.mu.Lock()
	host := a.host
	a.mu.Unlock()
	if host != nil && host.Shutdown(cleanupCtx) != nil {
		value := failure(desktopcontract.FailureShutdown, desktopcontract.ComponentRuntime, true, desktopcontract.RecoveryRetry)
		return &value
	}
	a.mu.Lock()
	a.host = nil
	a.started = nil
	a.handshake = protocolcommon.HandshakeResult{}
	a.state = desktopcontract.LifecycleStopped
	a.mu.Unlock()
	if err := safeLifecyclePublish(cleanupCtx, a.config.Lifecycle, desktopcontract.LifecycleEvent{State: desktopcontract.LifecycleStopped}); err != nil {
		code := desktopcontract.FailureShutdown
		if errors.Is(err, errInjectedPanic) {
			code = desktopcontract.FailureBackendPanic
		}
		value := failure(code, desktopcontract.ComponentBindingShell, false, desktopcontract.RecoveryExit)
		return &value
	}
	return nil
}

func (a *Application) report(ctx context.Context, value desktopcontract.Failure) {
	if a.config.Recovery != nil {
		safeReport(context.WithoutCancel(ctx), a.config.Recovery, value)
	}
}

func isCore(id desktopcontract.ComponentID) bool {
	return id == desktopcontract.ComponentLocalStorage || id == desktopcontract.ComponentAccess || id == desktopcontract.ComponentEngine || id == desktopcontract.ComponentRuntime
}

func safeAdapterStart(ctx context.Context, adapter Adapter) (err error) {
	defer func() {
		if recover() != nil {
			err = errInjectedPanic
		}
	}()
	return adapter.Start(ctx)
}

func safeAdapterShutdown(ctx context.Context, adapter Adapter) (err error) {
	defer func() {
		if recover() != nil {
			err = errInjectedPanic
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

func failedBinding(code desktopcontract.FailureCode, component desktopcontract.ComponentID, retryable bool, recovery desktopcontract.RecoveryAction) BindingResult {
	value := failure(code, component, retryable, recovery)
	return BindingResult{Outcome: protocolcommon.OutcomeFailed, Failure: &value}
}

func isBackendPanic(value *desktopcontract.Failure) bool {
	return value != nil && value.Code == desktopcontract.FailureBackendPanic
}
