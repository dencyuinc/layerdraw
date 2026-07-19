// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Package runtime exposes the framework-neutral host lifecycle contract. It
// orchestrates injected host ports and Engine/Access facades; it does not own
// LDL semantics, provider adapters, persistence, or commit implementation.
package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

const (
	ProtocolName    = "runtime"
	ProtocolVersion = "1.0"

	OperationHandshake          = "runtime.handshake"
	OperationOpenDocument       = "runtime.open_document"
	OperationCancelOperation    = "runtime.cancel_operation"
	OperationCommitOperations   = "runtime.commit_operations"
	OperationGetOperationResult = "runtime.get_operation_result"
	OperationListRevisions      = "runtime.list_revisions"
)

// Operations is the explicit host-implementation boundary. A capability is
// advertised only when its typed implementation is supplied; port presence
// alone never claims that Runtime orchestration exists.
type Operations struct {
	OpenDocument       OpenDocumentOperation
	CommitOperations   CommitOperationsOperation
	CancelOperation    CancelOperationOperation
	GetOperationResult GetOperationResultOperation
	ListRevisions      ListRevisionsOperation
}

type OpenDocumentOperation interface {
	OpenDocument(context.Context, runtimeprotocol.OpenRuntimeDocumentInput) (runtimeprotocol.OpenRuntimeDocumentResult, *ContractError)
}

type CommitOperationsOperation interface {
	CommitOperations(context.Context, runtimeprotocol.RuntimeCommitInput) (runtimeprotocol.RuntimeCommitResult, *ContractError)
}

type CancelOperationOperation interface {
	CancelOperation(context.Context, runtimeprotocol.CancelOperationInput) (runtimeprotocol.CancelOperationResult, *ContractError)
}

type GetOperationResultOperation interface {
	GetOperationResult(context.Context, runtimeprotocol.GetOperationResultInput) (runtimeprotocol.RuntimeOperationStatus, *ContractError)
}

type ListRevisionsOperation interface {
	ListRevisions(context.Context, runtimeprotocol.ListRevisionsInput) (runtimeprotocol.RevisionPage, *ContractError)
}

// Ports is the complete provider-neutral dependency set. Port presence
// describes storage support but never implies that an operation handler exists.
type Ports struct {
	Workbench     port.Workbench
	Grants        port.GrantSource
	Scopes        port.ScopeSource
	Documents     port.DocumentStore
	State         port.StateBackend
	StateBindings port.StateBackendBindingResolver
	StateAccess   port.StateQueryAuthorization
	Assets        port.AssetStore
	History       port.HistoryStore
	Recovery      port.RecoveryJournal
	Authoring     port.AuthoringDecision
	Clock         port.Clock
	Identities    port.IdentityGenerator
}

type Config struct {
	ReleaseVersion        protocolcommon.ReleaseVersion
	EndpointInstanceID    protocolcommon.EndpointInstanceID
	ReleaseManifestDigest protocolcommon.Digest
	Limits                runtimeprotocol.RuntimeLimits
	Ports                 Ports
	Operations            Operations
}

// Runtime is the immutable facade configuration. The host-neutral coordinator
// is installed only when its complete typed port set is present.
type Runtime struct{ config Config }

func New(config Config) (*Runtime, error) {
	if _, err := protocolcommon.EncodeReleaseVersion(config.ReleaseVersion); err != nil {
		return nil, fmt.Errorf("runtime configuration: invalid release version")
	}
	if _, err := protocolcommon.EncodeEndpointInstanceID(config.EndpointInstanceID); err != nil {
		return nil, fmt.Errorf("runtime configuration: invalid endpoint instance id")
	}
	if _, err := protocolcommon.EncodeDigest(config.ReleaseManifestDigest); err != nil {
		return nil, fmt.Errorf("runtime configuration: invalid release manifest digest")
	}
	if _, err := runtimeprotocol.EncodeRuntimeLimits(config.Limits); err != nil {
		return nil, fmt.Errorf("runtime configuration: invalid limits")
	}
	r := &Runtime{config: config}
	if operationsEmpty(config.Operations) && coordinatorPortsConfigured(config.Ports) {
		coordinator := newCoordinator(r)
		r.config.Operations = Operations{OpenDocument: coordinator, CommitOperations: coordinator, CancelOperation: coordinator, GetOperationResult: coordinator, ListRevisions: coordinator}
	}
	return r, nil
}

func operationsEmpty(operations Operations) bool {
	return operations.OpenDocument == nil && operations.CommitOperations == nil && operations.CancelOperation == nil && operations.GetOperationResult == nil && operations.ListRevisions == nil
}

func coordinatorPortsConfigured(ports Ports) bool {
	return ports.Workbench != nil && ports.Grants != nil && ports.Scopes != nil && ports.Documents != nil && ports.State != nil && ports.History != nil && ports.Recovery != nil && ports.Authoring != nil && ports.Clock != nil && ports.Identities != nil
}

type Descriptor struct {
	ProtocolVersion      protocolcommon.ProtocolVersion
	ProtocolSchemaDigest protocolcommon.Digest
	Operations           []protocolcommon.CapabilityID
	StorageCapabilities  []string
	Limits               runtimeprotocol.RuntimeLimits
}

func (r *Runtime) Describe() Descriptor {
	operations := r.enabledOperations()
	return Descriptor{
		ProtocolVersion:      ProtocolVersion,
		ProtocolSchemaDigest: protocolcommon.Digest(runtimeprotocol.SchemaDigest),
		Operations:           operations,
		StorageCapabilities:  r.storageCapabilities(),
		Limits:               r.config.Limits,
	}
}

// Negotiate validates the exact Runtime schema digest and returns only
// capabilities backed by the configured ports. Unknown optional capabilities
// are explicit unsupported statuses; a missing required capability rejects the
// negotiation.
func (r *Runtime) Negotiate(request runtimeprotocol.RuntimeHandshakeRequest) (runtimeprotocol.RuntimeHandshakeResult, *ContractError) {
	if _, err := runtimeprotocol.EncodeRuntimeHandshakeRequest(request); err != nil {
		return runtimeprotocol.RuntimeHandshakeResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "invalid runtime handshake")
	}
	foundProtocol := false
	for _, offer := range request.Protocols {
		if offer.Name != ProtocolName {
			continue
		}
		for _, binding := range offer.Versions {
			if binding.Version == ProtocolVersion && binding.SchemaDigest == protocolcommon.Digest(runtimeprotocol.SchemaDigest) {
				foundProtocol = true
				break
			}
		}
	}
	if !foundProtocol {
		return runtimeprotocol.RuntimeHandshakeResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "runtime protocol range or schema digest is incompatible")
	}

	enabled := map[protocolcommon.CapabilityID]bool{}
	for _, capability := range r.enabledOperations() {
		enabled[capability] = true
	}
	statuses := make([]protocolcommon.RequestedCapabilityStatus, 0, len(request.RequiredCapabilities)+len(request.OptionalCapabilities))
	for _, capability := range request.RequiredCapabilities {
		if !enabled[capability] {
			return runtimeprotocol.RuntimeHandshakeResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "required runtime capability is unavailable")
		}
		statuses = append(statuses, capabilityStatus(capability, true, nil))
	}
	unsupported := protocolcommon.UnavailableReasonUnsupported
	for _, capability := range request.OptionalCapabilities {
		if enabled[capability] {
			statuses = append(statuses, capabilityStatus(capability, true, nil))
		} else {
			statuses = append(statuses, capabilityStatus(capability, false, &unsupported))
		}
	}

	limits := r.config.Limits
	if request.ClientLimits != nil {
		limits = intersectLimits(limits, *request.ClientLimits)
	}
	manifest := runtimeprotocol.RuntimeCapabilityManifest{
		Limits:              limits,
		Operations:          make(map[string]protocolcommon.OperationCapability, len(enabled)),
		StorageCapabilities: r.storageCapabilities(),
	}
	for capability := range enabled {
		manifest.Operations[string(capability)] = protocolcommon.OperationCapability{Enabled: true, ProtocolVersion: ProtocolVersion}
	}
	manifest.ManifestEtag = manifestETag(manifest)
	result := runtimeprotocol.RuntimeHandshakeResult{
		CapabilityManifest:    manifest,
		CapabilityStatuses:    statuses,
		EndpointInstanceID:    r.config.EndpointInstanceID,
		HostRelease:           r.config.ReleaseVersion,
		NegotiatedProtocols:   []protocolcommon.NegotiatedProtocol{{Name: ProtocolName, Version: ProtocolVersion, SchemaDigest: protocolcommon.Digest(runtimeprotocol.SchemaDigest)}},
		ReleaseManifestDigest: r.config.ReleaseManifestDigest,
	}
	if _, err := runtimeprotocol.EncodeRuntimeHandshakeResult(result); err != nil {
		return runtimeprotocol.RuntimeHandshakeResult{}, contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, "runtime handshake result violates its contract")
	}
	return result, nil
}

func (r *Runtime) enabledOperations() []protocolcommon.CapabilityID {
	result := []protocolcommon.CapabilityID{OperationHandshake}
	operations := r.config.Operations
	if operations.OpenDocument != nil {
		result = append(result, OperationOpenDocument)
	}
	if operations.CommitOperations != nil {
		result = append(result, OperationCommitOperations)
	}
	if operations.CancelOperation != nil {
		result = append(result, OperationCancelOperation)
	}
	if operations.GetOperationResult != nil {
		result = append(result, OperationGetOperationResult)
	}
	if operations.ListRevisions != nil {
		result = append(result, OperationListRevisions)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func (r *Runtime) OpenDocument(ctx context.Context, input runtimeprotocol.OpenRuntimeDocumentInput) (runtimeprotocol.OpenRuntimeDocumentResult, *ContractError) {
	if r.config.Operations.OpenDocument == nil {
		return runtimeprotocol.OpenRuntimeDocumentResult{}, unavailableOperation(OperationOpenDocument)
	}
	return r.config.Operations.OpenDocument.OpenDocument(ctx, input)
}

func (r *Runtime) CommitOperations(ctx context.Context, input runtimeprotocol.RuntimeCommitInput) (runtimeprotocol.RuntimeCommitResult, *ContractError) {
	if r.config.Operations.CommitOperations == nil {
		return runtimeprotocol.RuntimeCommitResult{}, unavailableOperation(OperationCommitOperations)
	}
	return r.config.Operations.CommitOperations.CommitOperations(ctx, input)
}

func (r *Runtime) CancelOperation(ctx context.Context, input runtimeprotocol.CancelOperationInput) (runtimeprotocol.CancelOperationResult, *ContractError) {
	if r.config.Operations.CancelOperation == nil {
		return runtimeprotocol.CancelOperationResult{}, unavailableOperation(OperationCancelOperation)
	}
	return r.config.Operations.CancelOperation.CancelOperation(ctx, input)
}

func (r *Runtime) GetOperationResult(ctx context.Context, input runtimeprotocol.GetOperationResultInput) (runtimeprotocol.RuntimeOperationStatus, *ContractError) {
	if r.config.Operations.GetOperationResult == nil {
		return runtimeprotocol.RuntimeOperationStatus{}, unavailableOperation(OperationGetOperationResult)
	}
	return r.config.Operations.GetOperationResult.GetOperationResult(ctx, input)
}

func (r *Runtime) ListRevisions(ctx context.Context, input runtimeprotocol.ListRevisionsInput) (runtimeprotocol.RevisionPage, *ContractError) {
	if r.config.Operations.ListRevisions == nil {
		return runtimeprotocol.RevisionPage{}, unavailableOperation(OperationListRevisions)
	}
	return r.config.Operations.ListRevisions.ListRevisions(ctx, input)
}

func unavailableOperation(operation protocolcommon.CapabilityID) *ContractError {
	return contractError(runtimeprotocol.RuntimeFailureCodeRuntimeCapabilityUnavailable, string(operation)+" is not implemented by this host")
}

func (r *Runtime) storageCapabilities() []string {
	result := make([]string, 0, 5)
	if r.config.Ports.Assets != nil {
		result = append(result, "assets")
	}
	if r.config.Ports.Documents != nil {
		result = append(result, "conditional_document_head")
	}
	if r.config.Ports.History != nil {
		result = append(result, "history")
	}
	if r.config.Ports.Recovery != nil {
		result = append(result, "recovery_journal")
	}
	if r.config.Ports.State != nil {
		result = append(result, "state")
	}
	return result
}

func capabilityStatus(id protocolcommon.CapabilityID, enabled bool, reason *protocolcommon.UnavailableReason) protocolcommon.RequestedCapabilityStatus {
	return protocolcommon.RequestedCapabilityStatus{CapabilityID: id, Enabled: enabled, ProtocolVersion: ProtocolVersion, UnavailableReason: reason}
}

func manifestETag(manifest runtimeprotocol.RuntimeCapabilityManifest) protocolcommon.ManifestETag {
	projection := struct {
		Limits     runtimeprotocol.RuntimeLimits                 `json:"limits"`
		Operations map[string]protocolcommon.OperationCapability `json:"operations"`
		Storage    []string                                      `json:"storage_capabilities"`
	}{manifest.Limits, manifest.Operations, manifest.StorageCapabilities}
	data, _ := json.Marshal(projection)
	digest := sha256.Sum256(data)
	return protocolcommon.ManifestETag("sha256:" + hex.EncodeToString(digest[:]))
}

func intersectLimits(host, client runtimeprotocol.RuntimeLimits) runtimeprotocol.RuntimeLimits {
	return runtimeprotocol.RuntimeLimits{
		MaxBlobBytes:        minByteLimit(host.MaxBlobBytes, client.MaxBlobBytes),
		MaxBlobTotalBytes:   minByteLimit(host.MaxBlobTotalBytes, client.MaxBlobTotalBytes),
		MaxCommitOperations: minItemLimit(host.MaxCommitOperations, client.MaxCommitOperations),
		MaxHistoryItems:     minItemLimit(host.MaxHistoryItems, client.MaxHistoryItems),
		MaxOutputBytes:      minByteLimit(host.MaxOutputBytes, client.MaxOutputBytes),
		MaxStateMutations:   minItemLimit(host.MaxStateMutations, client.MaxStateMutations),
	}
}

func minByteLimit(host, client runtimeprotocol.RuntimeByteLimitValue) runtimeprotocol.RuntimeByteLimitValue {
	if decimalLess(string(client.HardMaximum), string(host.HardMaximum)) {
		return client
	}
	return host
}

func minItemLimit(host, client runtimeprotocol.RuntimeItemLimitValue) runtimeprotocol.RuntimeItemLimitValue {
	if decimalLess(string(client.HardMaximum), string(host.HardMaximum)) {
		return client
	}
	return host
}

func decimalLess(left, right string) bool {
	if len(left) != len(right) {
		return len(left) < len(right)
	}
	return left < right
}
