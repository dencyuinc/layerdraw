// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"encoding/json"
	"errors"
	"os/user"
	"reflect"
	"sync"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/host"
	"github.com/dencyuinc/layerdraw/internal/localdocument"
)

const (
	desktopRelease  = "0.0.0-dev"
	desktopDigest   = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	desktopEndpoint = "layerdraw-desktop"
)

var packagedCapabilities = []protocolcommon.CapabilityID{
	desktopcontract.CapabilityAuthoring,
}

// NewSharedConfig wires the Engine and Runtime owners that are actually
// packaged in Desktop. Other typed binding slots stay fail-closed and their
// capabilities are reported disabled until their production owners land.
func NewSharedConfig(root string) (desktopapp.Config, error) {
	owner := &sharedOwner{}
	clients, err := packagedClients(owner)
	if err != nil {
		return desktopapp.Config{}, err
	}
	adapters := map[desktopcontract.ComponentID]desktopapp.Adapter{}
	for _, id := range []desktopcontract.ComponentID{
		desktopcontract.ComponentNativeQuery, desktopcontract.ComponentSearchIndex,
		desktopcontract.ComponentEmbeddingProvider, desktopcontract.ComponentRegistryClient,
		desktopcontract.ComponentReview, desktopcontract.ComponentNativeExporters,
	} {
		adapters[id] = disabledComponent{}
	}
	adapters[desktopcontract.ComponentBindingShell] = owner
	return desktopapp.Config{
		Root: root, ReleaseVersion: desktopRelease, EndpointInstanceID: desktopEndpoint,
		ReleaseManifestDigest: desktopDigest, Adapters: adapters, Bindings: clients,
		Capabilities:                  nativeCapabilities{},
		EffectiveRequiredCapabilities: append([]protocolcommon.CapabilityID(nil), packagedCapabilities...),
		DisabledComponents: []desktopcontract.ComponentID{
			desktopcontract.ComponentNativeQuery, desktopcontract.ComponentSearchIndex,
			desktopcontract.ComponentEmbeddingProvider, desktopcontract.ComponentRegistryClient,
			desktopcontract.ComponentReview, desktopcontract.ComponentNativeExporters,
			desktopcontract.ComponentMCPHost,
		},
		HostPorts: desktopcontract.HostPorts{
			Credentials: unavailableCredentials{}, LocalActor: platformActor{},
			LocalOwner: unavailableOwner{}, Delegations: unavailableDelegations{}, MCP: disabledMCP{},
		},
	}, nil
}

type disabledComponent struct{}

func (disabledComponent) Start(context.Context) error    { return nil }
func (disabledComponent) Shutdown(context.Context) error { return nil }

// sharedOwner owns the in-process endpoint used by generated Wails Engine and
// Runtime bindings. It is started with the binding shell and closed before the
// application-local project host shuts down.
type sharedOwner struct {
	mu       sync.RWMutex
	local    *localdocument.Host
	endpoint *host.Endpoint
	engine   *engineendpoint.HostEngineFacade
}

func (o *sharedOwner) BindLocalHost(localHost *localdocument.Host) error {
	if localHost == nil {
		return errors.New("desktop local host is unavailable")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.endpoint != nil {
		return errors.New("desktop shared owner is already started")
	}
	o.local = localHost
	return nil
}

func (o *sharedOwner) Start(context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.endpoint != nil {
		return nil
	}
	if o.local == nil {
		return errors.New("desktop local host is not bound")
	}
	engine, err := engineendpoint.NewHostEngineFacade(desktopRelease, "unknown", desktopDigest, desktopEndpoint, engineendpoint.TransportInProcess)
	if err != nil {
		return err
	}
	endpoint, err := host.New(host.Config{LocalHost: o.local, Engine: engine})
	if err != nil {
		return err
	}
	o.endpoint, o.engine = endpoint, engine
	return nil
}

func (o *sharedOwner) Shutdown(context.Context) error {
	o.mu.Lock()
	o.endpoint, o.engine, o.local = nil, nil, nil
	o.mu.Unlock()
	return nil
}

func (o *sharedOwner) Invoke(ctx context.Context, exchange desktopcontract.Exchange) (desktopcontract.ExchangeResult, error) {
	o.mu.RLock()
	endpoint, engine := o.endpoint, o.engine
	o.mu.RUnlock()
	if endpoint == nil || engine == nil {
		return desktopcontract.ExchangeResult{}, errors.New("desktop shared owner is not started")
	}
	if exchange.Operation == string(engineprotocol.HandshakeRequestEnvelopeOperationValue) {
		request, err := engineprotocol.DecodeHandshakeRequestEnvelope(exchange.Control)
		if err != nil {
			return desktopcontract.ExchangeResult{}, err
		}
		response, _, err := engine.Descriptor().Negotiate(ctx, request)
		if err != nil {
			return desktopcontract.ExchangeResult{}, err
		}
		control, err := engineprotocol.EncodeHandshakeResponseEnvelope(response)
		return desktopcontract.ExchangeResult{Operation: exchange.Operation, Control: control}, err
	}
	if exchange.Operation == string(runtimeHandshakeOperation()) {
		response, _, err := endpoint.Handshake(ctx, exchange.Control)
		if err != nil {
			return desktopcontract.ExchangeResult{}, err
		}
		return desktopcontract.ExchangeResult{Operation: response.Operation, Control: response.Control}, nil
	}
	plan, terminal, err := endpoint.Prepare(ctx, exchange.Operation, exchange.Control)
	if err != nil {
		return desktopcontract.ExchangeResult{}, err
	}
	if terminal != nil {
		return desktopcontract.ExchangeResult{Operation: terminal.Operation, Control: terminal.Control}, nil
	}
	sink := &exchangeBlobSink{}
	response, err := plan.ExecuteDispatch(ctx, exchangeBlobSource(exchange.Blobs), sink)
	if err != nil {
		return desktopcontract.ExchangeResult{}, err
	}
	return desktopcontract.ExchangeResult{Operation: response.Operation, Control: response.Control, Blobs: sink.blobs}, nil
}

func runtimeHandshakeOperation() protocolcommon.CapabilityID { return "runtime.handshake" }

type exchangeBlobSource []desktopcontract.Blob

func (source exchangeBlobSource) Definitions(context.Context) ([]engineendpoint.BlobDefinition, error) {
	result := make([]engineendpoint.BlobDefinition, len(source))
	for index, blob := range source {
		bytes := append([]byte(nil), blob.Bytes...)
		result[index] = engineendpoint.BlobDefinition{BlobID: blob.ID, Owned: &engineendpoint.OwnedBlob{Bytes: bytes, Release: func() {}}}
	}
	return result, nil
}

type exchangeBlobSink struct{ blobs []desktopcontract.Blob }

func (sink *exchangeBlobSink) Publish(_ context.Context, blobs []engineendpoint.OutputBlob) error {
	sink.blobs = make([]desktopcontract.Blob, len(blobs))
	for index, blob := range blobs {
		sink.blobs[index] = desktopcontract.Blob{ID: blob.Ref.BlobID, Bytes: append([]byte(nil), blob.Bytes...)}
	}
	return nil
}

type platformActor struct{}

func (platformActor) ResolveLocalActor(context.Context) desktopcontract.Result[accessprotocol.ActorRef] {
	current, err := user.Current()
	if err != nil || current.Uid == "" {
		return closedFailure[accessprotocol.ActorRef](desktopcontract.FailureLocalActor)
	}
	return desktopcontract.Result[accessprotocol.ActorRef]{Outcome: protocolcommon.OutcomeSuccess, Value: accessprotocol.ActorRef{ActorID: "local-user-" + current.Uid, Kind: "user"}}
}

type unavailableCredentials struct{}

func (unavailableCredentials) Resolve(context.Context, desktopcontract.CredentialRef) desktopcontract.Result[[]byte] {
	return closedFailure[[]byte](desktopcontract.FailureCredential)
}

type unavailableOwner struct{}

func (unavailableOwner) IssueLocalOwnerGrant(context.Context, desktopcontract.LocalOwnerGrantRequest) desktopcontract.Result[accessprotocol.AuthoringGrantSnapshot] {
	return closedFailure[accessprotocol.AuthoringGrantSnapshot](desktopcontract.FailureAgentDelegation)
}

type unavailableDelegations struct{}

func (unavailableDelegations) Delegate(context.Context, accessprotocol.AuthoringGrantSnapshot, accesscore.Delegation) desktopcontract.Result[accesscore.Delegation] {
	return closedFailure[accesscore.Delegation](desktopcontract.FailureAgentDelegation)
}
func (unavailableDelegations) Resolve(context.Context, desktopcontract.DelegationFence) desktopcontract.Result[accesscore.Delegation] {
	return closedFailure[accesscore.Delegation](desktopcontract.FailureAgentDelegation)
}
func (unavailableDelegations) Revoke(context.Context, desktopcontract.DelegationFence) desktopcontract.Result[accesscore.DelegationSnapshot] {
	return closedFailure[accesscore.DelegationSnapshot](desktopcontract.FailureAgentDelegation)
}

// disabledMCP is a lifecycle-compatible closed port. It opens no listener and
// its tool/resource capabilities remain disabled in nativeCapabilities.
type disabledMCP struct{}

func (disabledMCP) Start(context.Context) desktopcontract.Result[struct{}] {
	return desktopcontract.Result[struct{}]{Outcome: protocolcommon.OutcomeSuccess}
}
func (disabledMCP) Shutdown(context.Context) desktopcontract.Result[struct{}] {
	return desktopcontract.Result[struct{}]{Outcome: protocolcommon.OutcomeSuccess}
}

func closedFailure[T any](code desktopcontract.FailureCode) desktopcontract.Result[T] {
	return desktopcontract.Result[T]{Outcome: protocolcommon.OutcomeFailed, Failure: &desktopcontract.Failure{Code: code, Component: desktopcontract.ComponentAccess, Recovery: desktopcontract.RecoveryConfigureAdapter}}
}

type nativeCapabilities struct{}

func (nativeCapabilities) Negotiate(ctx context.Context, manifest desktopcontract.Manifest) (protocolcommon.HandshakeResult, error) {
	descriptor, err := engineendpoint.NewDescriptor(engineendpoint.DescriptorConfig{
		EngineRelease: desktopRelease, SourceRevision: "unknown",
		ReleaseManifestDigest: desktopDigest, EndpointInstanceID: desktopEndpoint,
		Transports: []string{engineendpoint.TransportInProcess}, Limits: engineendpoint.DefaultLimitPolicy(),
	})
	if err != nil {
		return protocolcommon.HandshakeResult{}, err
	}
	response, _, err := descriptor.Negotiate(ctx, engineprotocol.HandshakeRequestEnvelope{
		Operation: engineprotocol.HandshakeRequestEnvelopeOperationValue,
		Payload: protocolcommon.HandshakeRequest{ClientRelease: desktopRelease,
			Protocols:            []protocolcommon.ProtocolOffer{{Name: engineendpoint.ProtocolName, SupportedRange: "1.0..1.0", Versions: []protocolcommon.ProtocolVersionBinding{{Version: engineendpoint.ProtocolVersion, SchemaDigest: protocolcommon.Digest(engineprotocol.SchemaDigest)}}}},
			RequiredCapabilities: []protocolcommon.CapabilityID{engineendpoint.OperationCompile}, OptionalCapabilities: []protocolcommon.CapabilityID{}},
		Protocol: engineprotocol.EngineProtocolRef{Name: engineprotocol.EngineProtocolRefNameValue, Version: engineprotocol.EngineProtocolRefVersionValue}, RequestID: "desktop-startup",
	})
	if err != nil || response.Payload == nil {
		return protocolcommon.HandshakeResult{}, errors.New("desktop capability negotiation failed")
	}
	value := *response.Payload
	enabled := make(map[protocolcommon.CapabilityID]bool, len(packagedCapabilities))
	for _, id := range packagedCapabilities {
		enabled[id] = true
	}
	ids := append(append([]protocolcommon.CapabilityID(nil), manifest.RequiredCapabilities...), manifest.OptionalCapabilities...)
	value.CapabilityStatuses = make([]protocolcommon.RequestedCapabilityStatus, 0, len(ids))
	for _, id := range ids {
		status := protocolcommon.RequestedCapabilityStatus{CapabilityID: id, Enabled: enabled[id], ProtocolVersion: desktopcontract.DesktopProtocolVersion}
		if !status.Enabled {
			reason := protocolcommon.UnavailableReasonNotConfigured
			status.UnavailableReason = &reason
		}
		value.CapabilityStatuses = append(value.CapabilityStatuses, status)
	}
	return value, nil
}

type closedOwnerDecoder struct{}

func (closedOwnerDecoder) DecodeRequest(expected string, control []byte) (desktopcontract.OwnerEnvelopeIdentity, error) {
	var value struct {
		Operation string `json:"operation"`
		RequestID string `json:"request_id"`
	}
	if json.Unmarshal(control, &value) != nil || value.Operation != expected || value.RequestID == "" {
		return desktopcontract.OwnerEnvelopeIdentity{}, errors.New("invalid request")
	}
	return desktopcontract.OwnerEnvelopeIdentity{Operation: value.Operation, RequestID: value.RequestID}, nil
}
func (closedOwnerDecoder) DecodeResponse(string, []byte) (desktopcontract.OwnerResponseIdentity, error) {
	return desktopcontract.OwnerResponseIdentity{}, errors.New("owner unavailable")
}

func packagedClients(owner *sharedOwner) (desktopcontract.ClientSet, error) {
	clients := desktopcontract.ClientSet{}
	root := reflect.ValueOf(&clients).Elem()
	methodType := reflect.TypeOf(desktopcontract.ClientMethod(nil))
	available := reflect.ValueOf(desktopcontract.ClientMethod(owner.Invoke))
	unavailable := reflect.MakeFunc(methodType, func([]reflect.Value) []reflect.Value {
		return []reflect.Value{reflect.ValueOf(desktopcontract.ExchangeResult{}), reflect.ValueOf(errors.New("desktop owner is not packaged"))}
	})
	decoder := reflect.ValueOf(closedOwnerDecoder{})
	for index := 0; index < root.NumField(); index++ {
		ownerField := root.Field(index)
		actual := root.Type().Field(index).Name == "Engine" || root.Type().Field(index).Name == "Runtime"
		for fieldIndex := 0; fieldIndex < ownerField.NumField(); fieldIndex++ {
			field := ownerField.Field(fieldIndex)
			if field.Kind() == reflect.Interface {
				field.Set(decoder)
			} else if actual {
				field.Set(available)
			} else {
				field.Set(unavailable)
			}
		}
	}
	return clients, clients.Validate()
}
