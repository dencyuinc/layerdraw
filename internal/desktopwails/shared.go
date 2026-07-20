// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopwails

import (
	"context"
	"encoding/json"
	"errors"
	"os/user"
	"reflect"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
	"github.com/dencyuinc/layerdraw/internal/desktopapp"
	"github.com/dencyuinc/layerdraw/internal/desktopcontract"
	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
)

const (
	desktopRelease  = "0.0.0-dev"
	desktopDigest   = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	desktopEndpoint = "layerdraw-desktop"
)

// NewSharedConfig supplies the existing in-process owners used by the native
// lifecycle executable. Owner-specific packages can replace these explicit
// adapters as their packaged implementations land without changing Wails.
func NewSharedConfig(root string) (desktopapp.Config, error) {
	clients, err := unavailableClients()
	if err != nil {
		return desktopapp.Config{}, err
	}
	adapters := map[desktopcontract.ComponentID]desktopapp.Adapter{}
	for _, id := range []desktopcontract.ComponentID{
		desktopcontract.ComponentNativeQuery, desktopcontract.ComponentSearchIndex,
		desktopcontract.ComponentEmbeddingProvider, desktopcontract.ComponentRegistryClient,
		desktopcontract.ComponentReview, desktopcontract.ComponentNativeExporters,
		desktopcontract.ComponentBindingShell,
	} {
		adapters[id] = lifecycleComponent{}
	}
	return desktopapp.Config{
		Root: root, ReleaseVersion: desktopRelease, EndpointInstanceID: desktopEndpoint,
		ReleaseManifestDigest: desktopDigest, Adapters: adapters, Bindings: clients,
		Capabilities: nativeCapabilities{},
		HostPorts: desktopcontract.HostPorts{
			Credentials: unavailableCredentials{}, LocalActor: platformActor{},
			LocalOwner: unavailableOwner{}, Delegations: unavailableDelegations{}, MCP: localMCP{},
		},
	}, nil
}

type lifecycleComponent struct{}

func (lifecycleComponent) Start(context.Context) error    { return nil }
func (lifecycleComponent) Shutdown(context.Context) error { return nil }

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

type localMCP struct{}

func (localMCP) Start(context.Context) desktopcontract.Result[struct{}] {
	return desktopcontract.Result[struct{}]{Outcome: protocolcommon.OutcomeSuccess}
}
func (localMCP) Shutdown(context.Context) desktopcontract.Result[struct{}] {
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
	ids := append(append([]protocolcommon.CapabilityID(nil), manifest.RequiredCapabilities...), manifest.OptionalCapabilities...)
	value.CapabilityStatuses = make([]protocolcommon.RequestedCapabilityStatus, 0, len(ids))
	for _, id := range ids {
		status := protocolcommon.RequestedCapabilityStatus{CapabilityID: id, Enabled: id != desktopcontract.CapabilityExternalStorage, ProtocolVersion: desktopcontract.DesktopProtocolVersion}
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
func (closedOwnerDecoder) DecodeResponse(expected string, control []byte) (desktopcontract.OwnerResponseIdentity, error) {
	return desktopcontract.OwnerResponseIdentity{}, errors.New("owner unavailable")
}

func unavailableClients() (desktopcontract.ClientSet, error) {
	clients := desktopcontract.ClientSet{}
	root := reflect.ValueOf(&clients).Elem()
	methodType := reflect.TypeOf(desktopcontract.ClientMethod(nil))
	method := reflect.MakeFunc(methodType, func([]reflect.Value) []reflect.Value {
		return []reflect.Value{reflect.ValueOf(desktopcontract.ExchangeResult{}), reflect.ValueOf(errors.New("shared owner binding unavailable"))}
	})
	decoder := reflect.ValueOf(closedOwnerDecoder{})
	for i := 0; i < root.NumField(); i++ {
		owner := root.Field(i)
		for j := 0; j < owner.NumField(); j++ {
			field := owner.Field(j)
			if field.Kind() == reflect.Interface {
				field.Set(decoder)
			} else {
				field.Set(method)
			}
		}
	}
	return clients, clients.Validate()
}
