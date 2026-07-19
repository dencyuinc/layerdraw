// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// This file composes the public Engine endpoint and local Runtime lifecycle
// behind a framework-neutral dispatch root. It contains no stdio, CLI, UI, or
// provider-specific semantics.
package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/localdocument"
	layerruntime "github.com/dencyuinc/layerdraw/internal/runtime"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

const (
	OperationInspect       = "runtime.inspect_document"
	OperationPreview       = "runtime.preview_operations"
	OperationSave          = "runtime.save_document"
	OperationAutosave      = "runtime.control_autosave"
	OperationStateSnapshot = "runtime.get_state_snapshot"
	OperationRestore       = "runtime.preview_restore"
	OperationAsset         = "runtime.stage_asset"
	OperationClose         = "runtime.close_document"
	OperationRecover       = "runtime.recover_operations"
)

var runtimeOperations = []string{
	"runtime.cancel_operation", "runtime.commit_operations", OperationAutosave,
	OperationClose, "runtime.get_operation_result", OperationStateSnapshot,
	OperationInspect, "runtime.list_revisions", "runtime.open_document",
	OperationPreview, OperationRestore, OperationRecover, OperationSave, OperationAsset,
}

type Endpoint struct {
	host             *localdocument.Host
	engine           *engineendpoint.CompileDispatcher
	descriptor       *engineendpoint.Descriptor
	negotiated       *engineendpoint.NegotiatedContext
	release          protocolcommon.ReleaseVersion
	search           ConsumerSearchSurface
	searchOperations map[string]bool

	mu         sync.Mutex
	handshaken bool
	manifest   runtimeprotocol.RuntimeCapabilityManifest
}

type Config struct {
	LocalHost *localdocument.Host
	Engine    *engineendpoint.HostEngineFacade
	Search    ConsumerSearchSurface
}

type LocalConfig struct {
	Root, ReleaseVersion, SourceRevision, ReleaseManifestDigest, EndpointInstanceID, TransportID string
	Search                                                                                       ConsumerSearchSurface
}

func NewLocal(config LocalConfig) (*Endpoint, func(context.Context) error, error) {
	localHost, err := localdocument.New(localdocument.Config{Root: config.Root, ReleaseVersion: protocolcommon.ReleaseVersion(config.ReleaseVersion), EndpointInstanceID: protocolcommon.EndpointInstanceID(config.EndpointInstanceID), ReleaseManifestDigest: protocolcommon.Digest(config.ReleaseManifestDigest)})
	if err != nil {
		return nil, nil, err
	}
	engineFacade, err := engineendpoint.NewHostEngineFacade(config.ReleaseVersion, config.SourceRevision, config.ReleaseManifestDigest, config.EndpointInstanceID, config.TransportID)
	if err != nil {
		_ = localHost.Shutdown(context.Background())
		return nil, nil, err
	}
	result, err := New(Config{LocalHost: localHost, Engine: engineFacade, Search: config.Search})
	if err != nil {
		_ = localHost.Shutdown(context.Background())
		return nil, nil, err
	}
	return result, localHost.Shutdown, nil
}

func New(config Config) (*Endpoint, error) {
	if config.LocalHost == nil || config.Engine == nil {
		return nil, errors.New("host composition requires Runtime and Engine")
	}
	operations := map[string]bool{}
	if config.Search != nil {
		manifest, err := config.Search.Capabilities(context.Background())
		if err != nil {
			return nil, fmt.Errorf("host Search capability derivation failed: %w", err)
		}
		operations[OperationSearch] = manifest.SearchAvailable
		operations[OperationExecuteQuery] = manifest.QueryAvailable
		operations[OperationAnalyzeGraph] = manifest.AnalysisAvailable
	}
	return &Endpoint{host: config.LocalHost, engine: config.Engine.Dispatcher(), descriptor: config.Engine.Descriptor(), negotiated: config.Engine.Negotiated(), release: protocolcommon.ReleaseVersion(config.Engine.ReleaseVersion()), search: config.Search, searchOperations: operations}, nil
}

func (e *Endpoint) Supports(operation string) bool {
	if operation == OperationSearch || operation == OperationExecuteQuery || operation == OperationAnalyzeGraph {
		return e.searchOperations[operation]
	}
	for _, candidate := range runtimeOperations {
		if operation == candidate {
			return true
		}
	}
	return e.negotiated != nil && e.negotiated.SupportsOperation(operation)
}

func (e *Endpoint) Handshake(_ context.Context, control []byte) (engineendpoint.DispatchResponse, bool, error) {
	request, err := runtimeprotocol.DecodeRuntimeHandshakeRequestEnvelope(control)
	if err != nil {
		return engineendpoint.DispatchResponse{}, false, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.handshaken {
		response, encodeErr := e.runtimeResponse("runtime.handshake", request.RequestID, nil, protocolcommon.OutcomeRejected, nil)
		return response, false, encodeErr
	}
	baseRequest := request.Payload
	baseRequest.RequiredCapabilities = []protocolcommon.CapabilityID{}
	baseRequest.OptionalCapabilities = []protocolcommon.CapabilityID{}
	result, negotiateErr := e.host.Negotiate(baseRequest)
	if negotiateErr != nil {
		response, encodeErr := e.runtimeResponse("runtime.handshake", request.RequestID, nil, protocolcommon.OutcomeRejected, nil)
		return response, false, encodeErr
	}
	manifest := result.CapabilityManifest
	if manifest.Operations == nil {
		manifest.Operations = map[string]protocolcommon.OperationCapability{}
	}
	for _, operation := range runtimeOperations {
		manifest.Operations[operation] = protocolcommon.OperationCapability{Enabled: true, ProtocolVersion: "1.0"}
	}
	manifest.Operations["runtime.handshake"] = protocolcommon.OperationCapability{Enabled: true, ProtocolVersion: "1.0"}
	for _, operation := range e.descriptor.Operations() {
		manifest.Operations[operation] = protocolcommon.OperationCapability{Enabled: true, ProtocolVersion: "1.0"}
	}
	if e.search != nil {
		manifest.Operations[OperationSearch] = protocolcommon.OperationCapability{Enabled: e.searchOperations[OperationSearch], ProtocolVersion: "1.0"}
		manifest.Operations[OperationExecuteQuery] = protocolcommon.OperationCapability{Enabled: e.searchOperations[OperationExecuteQuery], ProtocolVersion: "1.0"}
		manifest.Operations[OperationAnalyzeGraph] = protocolcommon.OperationCapability{Enabled: e.searchOperations[OperationAnalyzeGraph], ProtocolVersion: "1.0"}
	}
	manifest.ManifestEtag = runtimeManifestETag(manifest)
	enabled := func(id protocolcommon.CapabilityID) bool {
		capability, ok := manifest.Operations[string(id)]
		return ok && capability.Enabled
	}
	statuses := make([]protocolcommon.RequestedCapabilityStatus, 0, len(request.Payload.RequiredCapabilities)+len(request.Payload.OptionalCapabilities))
	for _, id := range request.Payload.RequiredCapabilities {
		if !enabled(id) {
			response, encodeErr := e.runtimeResponse("runtime.handshake", request.RequestID, nil, protocolcommon.OutcomeRejected, nil)
			return response, false, encodeErr
		}
		statuses = append(statuses, protocolcommon.RequestedCapabilityStatus{CapabilityID: id, Enabled: true, ProtocolVersion: "1.0"})
	}
	unsupported := protocolcommon.UnavailableReasonUnsupported
	for _, id := range request.Payload.OptionalCapabilities {
		status := protocolcommon.RequestedCapabilityStatus{CapabilityID: id, Enabled: enabled(id), ProtocolVersion: "1.0"}
		if !status.Enabled {
			status.UnavailableReason = &unsupported
		}
		statuses = append(statuses, status)
	}
	result.CapabilityManifest, result.CapabilityStatuses = manifest, statuses
	e.manifest = manifest
	response, err := e.runtimeResponse("runtime.handshake", request.RequestID, result, protocolcommon.OutcomeSuccess, nil)
	if err == nil {
		e.handshaken = true
	}
	return response, err == nil, err
}

func (e *Endpoint) Prepare(ctx context.Context, operation string, control []byte) (engineendpoint.DispatchPlan, *engineendpoint.DispatchResponse, error) {
	if len(operation) > len("runtime.") && operation[:len("runtime.")] == "runtime." {
		return e.prepareRuntime(ctx, operation, control)
	}
	return e.engine.PrepareDispatch(ctx, e.negotiated, operation, control)
}

func (e *Endpoint) CancellationResponse(operation, requestID string) (engineendpoint.DispatchResponse, error) {
	if len(operation) > len("runtime.") && operation[:len("runtime.")] == "runtime." {
		return e.runtimeResponse(operation, requestID, nil, protocolcommon.OutcomeCancelled, failure("runtime.cancelled", protocolcommon.ProtocolFailureCategoryCancelled))
	}
	return e.engine.DispatchCancellationResponse(operation, requestID, e.release)
}

func (e *Endpoint) TransportResponse(operation, requestID string) (engineendpoint.DispatchResponse, error) {
	if len(operation) > len("runtime.") && operation[:len("runtime.")] == "runtime." {
		return e.runtimeResponse(operation, requestID, nil, protocolcommon.OutcomeFailed, failure("runtime.transport_failure", protocolcommon.ProtocolFailureCategoryTransport))
	}
	return e.engine.DispatchTransportResponse(operation, requestID, e.release)
}

type runtimePlan struct {
	endpoint     *Endpoint
	operation    string
	requestID    string
	requirements []engineendpoint.BlobRequirement
	run          func(context.Context, map[string][]byte) (any, error)
}

func (p *runtimePlan) BlobRequirements() []engineendpoint.BlobRequirement {
	return append([]engineendpoint.BlobRequirement(nil), p.requirements...)
}
func (p *runtimePlan) AdmissionBudget() engineendpoint.CompileAdmissionBudget {
	var bytes int64
	for _, requirement := range p.requirements {
		var value int64
		_, _ = fmt.Sscan(string(requirement.Ref.Size), &value)
		bytes += value
	}
	return engineendpoint.CompileAdmissionBudget{RequiredBlobCount: int64(len(p.requirements)), RequiredBlobBytes: bytes}
}
func (p *runtimePlan) Abort() {}
func (p *runtimePlan) Execute(context.Context, engineendpoint.BlobSource, engineendpoint.BlobSink) (engineprotocol.CompileResponseEnvelope, error) {
	return engineprotocol.CompileResponseEnvelope{}, errors.New("runtime plan is not a compile plan")
}
func (p *runtimePlan) ExecuteDispatch(ctx context.Context, source engineendpoint.BlobSource, _ engineendpoint.BlobSink) (engineendpoint.DispatchResponse, error) {
	definitions, err := source.Definitions(ctx)
	if err != nil {
		return engineendpoint.DispatchResponse{}, err
	}
	blobs := make(map[string][]byte, len(definitions))
	for _, definition := range definitions {
		if definition.Owned == nil {
			return engineendpoint.DispatchResponse{}, errors.New("runtime transport did not transfer owned bytes")
		}
		blobs[definition.BlobID] = append([]byte(nil), definition.Owned.Bytes...)
		definition.Owned.Release()
	}
	payload, runErr := p.run(ctx, blobs)
	if p.endpoint == nil {
		return engineendpoint.DispatchResponse{}, errors.New("missing host endpoint context")
	}
	if runErr != nil {
		return p.endpoint.runtimeResponse(p.operation, p.requestID, nil, protocolcommon.OutcomeFailed, failure("runtime.operation_failed", protocolcommon.ProtocolFailureCategoryIo))
	}
	return p.endpoint.runtimeResponse(p.operation, p.requestID, payload, protocolcommon.OutcomeSuccess, nil)
}

func (e *Endpoint) prepareRuntime(ctx context.Context, operation string, control []byte) (engineendpoint.DispatchPlan, *engineendpoint.DispatchResponse, error) {
	if (operation == OperationSearch || operation == OperationExecuteQuery || operation == OperationAnalyzeGraph) && !e.Supports(operation) {
		return nil, nil, errors.New("Search operation capability unavailable")
	}
	plan := &runtimePlan{endpoint: e, operation: operation}
	switch operation {
	case OperationSearch:
		request, err := decodeSearchOperationRequest[layerruntime.SearchRequest](control, operation)
		if err != nil {
			return nil, nil, err
		}
		plan.requestID = request.RequestID
		plan.run = func(ctx context.Context, _ map[string][]byte) (any, error) {
			return e.search.Search(ctx, request.Payload)
		}
	case OperationExecuteQuery, OperationAnalyzeGraph:
		request, err := decodeSearchOperationRequest[port.BoundExecutionRequest](control, operation)
		if err != nil {
			return nil, nil, err
		}
		plan.requestID = request.RequestID
		plan.run = func(ctx context.Context, _ map[string][]byte) (any, error) {
			if operation == OperationExecuteQuery {
				return e.search.ExecuteQuery(ctx, request.Payload)
			}
			return e.search.ExecuteAnalysis(ctx, request.Payload)
		}
	case "runtime.open_document":
		request, err := runtimeprotocol.DecodeOpenDocumentRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		plan.requestID = request.RequestID
		plan.run = func(ctx context.Context, _ map[string][]byte) (any, error) {
			var opened localdocument.OpenResult
			var err error
			if request.Payload.LocalSource == nil {
				opened, err = e.host.OpenDocument(ctx, request.Payload.DocumentID)
			} else {
				switch request.Payload.LocalSource.Kind {
				case "project":
					opened, err = e.host.OpenProject(ctx, localdocument.OpenProjectInput{Root: request.Payload.LocalSource.Path, EntryPath: valueOr(request.Payload.LocalSource.EntryPath, "document.ldl")})
				case "container":
					opened, err = e.host.OpenContainer(ctx, request.Payload.LocalSource.Path)
				case "import_container":
					opened, err = e.host.ImportContainer(ctx, request.Payload.LocalSource.Path)
				default:
					err = errors.New("unsupported local source")
				}
			}
			if err != nil {
				return nil, err
			}
			opened.Session.Open.CapabilityManifest = e.manifest
			return opened.Session.Open, nil
		}
	case OperationInspect:
		request, err := runtimeprotocol.DecodeInspectDocumentRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		plan.requestID = request.RequestID
		plan.run = func(context.Context, map[string][]byte) (any, error) {
			result, err := e.host.Inspect(request.Payload.Session)
			result.CapabilityManifest = e.manifest
			return result, err
		}
	case OperationPreview:
		request, err := runtimeprotocol.DecodePreviewOperationsRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		plan.requestID = request.RequestID
		plan.run = func(ctx context.Context, _ map[string][]byte) (any, error) {
			return e.host.Preview(ctx, request.Payload)
		}
	case "runtime.commit_operations":
		request, err := runtimeprotocol.DecodeCommitOperationsRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		plan.requestID = request.RequestID
		plan.run = func(ctx context.Context, _ map[string][]byte) (any, error) {
			return e.host.Commit(ctx, request.Payload)
		}
	case OperationSave:
		request, err := runtimeprotocol.DecodeSaveDocumentRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		plan.requestID = request.RequestID
		plan.run = func(ctx context.Context, _ map[string][]byte) (any, error) {
			return e.host.SaveRuntime(ctx, request.Payload)
		}
	case OperationAutosave:
		request, err := runtimeprotocol.DecodeAutosaveControlRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		plan.requestID = request.RequestID
		plan.run = func(ctx context.Context, _ map[string][]byte) (any, error) {
			return e.host.ControlAutosave(ctx, request.Payload)
		}
	case OperationStateSnapshot:
		request, err := runtimeprotocol.DecodeStateSnapshotRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		plan.requestID = request.RequestID
		plan.run = func(ctx context.Context, _ map[string][]byte) (any, error) {
			return e.host.StateSnapshot(ctx, request.Payload.Session)
		}
	case "runtime.list_revisions":
		request, err := runtimeprotocol.DecodeListRevisionsRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		plan.requestID = request.RequestID
		plan.run = func(ctx context.Context, _ map[string][]byte) (any, error) {
			return e.host.ListRevisions(ctx, request.Payload)
		}
	case OperationRestore:
		request, err := runtimeprotocol.DecodeRestorePreviewRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		plan.requestID = request.RequestID
		plan.run = func(ctx context.Context, _ map[string][]byte) (any, error) {
			return e.host.PreviewRestore(ctx, request.Payload)
		}
	case OperationAsset:
		request, err := runtimeprotocol.DecodeStageAssetRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		plan.requestID = request.RequestID
		plan.requirements = []engineendpoint.BlobRequirement{{Ref: request.Payload.ContentBlob, References: 1}}
		plan.run = func(ctx context.Context, blobs map[string][]byte) (any, error) {
			return e.host.StageAsset(ctx, request.Payload, blobs[request.Payload.ContentBlob.BlobID])
		}
	case OperationClose:
		request, err := runtimeprotocol.DecodeCloseRuntimeDocumentRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		plan.requestID = request.RequestID
		plan.run = func(ctx context.Context, _ map[string][]byte) (any, error) {
			session, err := e.host.SessionFor(request.Payload.Session)
			if err != nil {
				return nil, err
			}
			err = e.host.Close(ctx, session)
			return runtimeprotocol.CloseDocumentResult{Closed: err == nil}, err
		}
	case "runtime.cancel_operation":
		request, err := runtimeprotocol.DecodeCancelOperationRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		plan.requestID = request.RequestID
		plan.run = func(ctx context.Context, _ map[string][]byte) (any, error) {
			return e.host.Cancel(ctx, request.Payload)
		}
	case "runtime.get_operation_result":
		request, err := runtimeprotocol.DecodeGetOperationResultRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		plan.requestID = request.RequestID
		plan.run = func(ctx context.Context, _ map[string][]byte) (any, error) {
			return e.host.OperationResult(ctx, request.Payload)
		}
	case OperationRecover:
		request, err := runtimeprotocol.DecodeRecoverOperationsRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		plan.requestID = request.RequestID
		plan.run = func(ctx context.Context, _ map[string][]byte) (any, error) {
			return e.host.RecoverOperations(ctx, request.Payload.DocumentID)
		}
	default:
		return nil, nil, errors.New("unsupported Runtime operation")
	}
	return plan, nil, nil
}

func valueOr(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}

func (e *Endpoint) runtimeResponse(operation, requestID string, payload any, outcome protocolcommon.Outcome, protocolFailure *protocolcommon.ProtocolFailure) (engineendpoint.DispatchResponse, error) {
	diagnostics := []protocolcommon.ProtocolDiagnostic{}
	if outcome == protocolcommon.OutcomeRejected {
		diagnostics = append(diagnostics, protocolcommon.ProtocolDiagnostic{Code: "runtime.handshake.rejected", Message: "The Runtime handshake request was rejected.", Related: []protocolcommon.ProtocolDiagnosticRelated{}, Severity: protocolcommon.ProtocolDiagnosticSeverityError})
	}
	protocol := runtimeprotocol.RuntimeProtocolRef{Name: runtimeprotocol.RuntimeProtocolRefNameValue, Version: "1.0"}
	var control []byte
	var err error
	switch operation {
	case "runtime.handshake":
		value := runtimeprotocol.RuntimeHandshakeResponseEnvelope(runtimeprotocol.HandshakeResponseEnvelopeBase{Diagnostics: diagnostics, Failure: protocolFailure, HostRelease: &e.release, Outcome: outcome, Protocol: protocol, RequestID: requestID})
		if payload != nil {
			result := payload.(runtimeprotocol.RuntimeHandshakeResult)
			value.Payload = &result
		}
		control, err = runtimeprotocol.EncodeRuntimeHandshakeResponseEnvelope(value)
	case "runtime.open_document":
		value := runtimeprotocol.OpenDocumentResponseEnvelope{Diagnostics: diagnostics, Failure: protocolFailure, HostRelease: e.release, Outcome: outcome, Protocol: protocol, RequestID: requestID}
		if payload != nil {
			result := payload.(runtimeprotocol.OpenRuntimeDocumentResult)
			value.Payload = &result
		}
		control, err = runtimeprotocol.EncodeOpenDocumentResponseEnvelope(value)
	case OperationInspect:
		value := runtimeprotocol.InspectDocumentResponseEnvelope{Diagnostics: diagnostics, Failure: protocolFailure, HostRelease: e.release, Outcome: outcome, Protocol: protocol, RequestID: requestID}
		if payload != nil {
			result := payload.(runtimeprotocol.RuntimeInspectionResult)
			value.Payload = &result
		}
		control, err = runtimeprotocol.EncodeInspectDocumentResponseEnvelope(value)
	case OperationPreview:
		value := runtimeprotocol.PreviewOperationsResponseEnvelope{Diagnostics: diagnostics, Failure: protocolFailure, HostRelease: e.release, Outcome: outcome, Protocol: protocol, RequestID: requestID}
		if payload != nil {
			result := payload.(runtimeprotocol.PreviewOperationsResult)
			value.Payload = &result
		}
		control, err = runtimeprotocol.EncodePreviewOperationsResponseEnvelope(value)
	case "runtime.commit_operations":
		value := runtimeprotocol.CommitOperationsResponseEnvelope{Diagnostics: diagnostics, Failure: protocolFailure, HostRelease: e.release, Outcome: outcome, Protocol: protocol, RequestID: requestID}
		if payload != nil {
			result := payload.(runtimeprotocol.RuntimeCommitResult)
			value.Payload = &result
		}
		control, err = runtimeprotocol.EncodeCommitOperationsResponseEnvelope(value)
	case OperationSave:
		value := runtimeprotocol.SaveDocumentResponseEnvelope{Diagnostics: diagnostics, Failure: protocolFailure, HostRelease: e.release, Outcome: outcome, Protocol: protocol, RequestID: requestID}
		if payload != nil {
			result := payload.(runtimeprotocol.RuntimeCommitResult)
			value.Payload = &result
		}
		control, err = runtimeprotocol.EncodeSaveDocumentResponseEnvelope(value)
	case OperationAutosave:
		value := runtimeprotocol.AutosaveControlResponseEnvelope{Diagnostics: diagnostics, Failure: protocolFailure, HostRelease: e.release, Outcome: outcome, Protocol: protocol, RequestID: requestID}
		if payload != nil {
			result := payload.(runtimeprotocol.AutosaveControlResult)
			value.Payload = &result
		}
		control, err = runtimeprotocol.EncodeAutosaveControlResponseEnvelope(value)
	case OperationStateSnapshot:
		value := runtimeprotocol.StateSnapshotResponseEnvelope{Diagnostics: diagnostics, Failure: protocolFailure, HostRelease: e.release, Outcome: outcome, Protocol: protocol, RequestID: requestID}
		if payload != nil {
			result := payload.(runtimeprotocol.StateSnapshotResult)
			value.Payload = &result
		}
		control, err = runtimeprotocol.EncodeStateSnapshotResponseEnvelope(value)
	case "runtime.list_revisions":
		value := runtimeprotocol.ListRevisionsResponseEnvelope{Diagnostics: diagnostics, Failure: protocolFailure, HostRelease: e.release, Outcome: outcome, Protocol: protocol, RequestID: requestID}
		if payload != nil {
			result := payload.(runtimeprotocol.RevisionPage)
			value.Payload = &result
		}
		control, err = runtimeprotocol.EncodeListRevisionsResponseEnvelope(value)
	case OperationRestore:
		value := runtimeprotocol.RestorePreviewResponseEnvelope{Diagnostics: diagnostics, Failure: protocolFailure, HostRelease: e.release, Outcome: outcome, Protocol: protocol, RequestID: requestID}
		if payload != nil {
			result := payload.(runtimeprotocol.RestorePreviewResult)
			value.Payload = &result
		}
		control, err = runtimeprotocol.EncodeRestorePreviewResponseEnvelope(value)
	case OperationAsset:
		value := runtimeprotocol.StageAssetResponseEnvelope{Diagnostics: diagnostics, Failure: protocolFailure, HostRelease: e.release, Outcome: outcome, Protocol: protocol, RequestID: requestID}
		if payload != nil {
			result := payload.(runtimeprotocol.StageAssetResult)
			value.Payload = &result
		}
		control, err = runtimeprotocol.EncodeStageAssetResponseEnvelope(value)
	case OperationClose:
		value := runtimeprotocol.CloseRuntimeDocumentResponseEnvelope{Diagnostics: diagnostics, Failure: protocolFailure, HostRelease: e.release, Outcome: outcome, Protocol: protocol, RequestID: requestID}
		if payload != nil {
			result := payload.(runtimeprotocol.CloseDocumentResult)
			value.Payload = &result
		}
		control, err = runtimeprotocol.EncodeCloseRuntimeDocumentResponseEnvelope(value)
	case "runtime.cancel_operation":
		value := runtimeprotocol.CancelOperationResponseEnvelope{Diagnostics: diagnostics, Failure: protocolFailure, HostRelease: e.release, Outcome: outcome, Protocol: protocol, RequestID: requestID}
		if payload != nil {
			result := payload.(runtimeprotocol.CancelOperationResult)
			value.Payload = &result
		}
		control, err = runtimeprotocol.EncodeCancelOperationResponseEnvelope(value)
	case "runtime.get_operation_result":
		value := runtimeprotocol.GetOperationResultResponseEnvelope{Diagnostics: diagnostics, Failure: protocolFailure, HostRelease: e.release, Outcome: outcome, Protocol: protocol, RequestID: requestID}
		if payload != nil {
			result := payload.(runtimeprotocol.RuntimeOperationStatus)
			value.Payload = &result
		}
		control, err = runtimeprotocol.EncodeGetOperationResultResponseEnvelope(value)
	case OperationRecover:
		value := runtimeprotocol.RecoverOperationsResponseEnvelope{Diagnostics: diagnostics, Failure: protocolFailure, HostRelease: e.release, Outcome: outcome, Protocol: protocol, RequestID: requestID}
		if payload != nil {
			result := payload.(runtimeprotocol.RecoverOperationsResult)
			value.Payload = &result
		}
		control, err = runtimeprotocol.EncodeRecoverOperationsResponseEnvelope(value)
	case OperationSearch, OperationExecuteQuery, OperationAnalyzeGraph:
		var raw json.RawMessage
		if payload != nil {
			bytes, ok := payload.([]byte)
			if !ok || !json.Valid(bytes) {
				return engineendpoint.DispatchResponse{}, errors.New("invalid Search response payload")
			}
			raw = append(json.RawMessage(nil), bytes...)
		}
		control, err = json.Marshal(searchOperationResponse{Operation: operation, Protocol: runtimeprotocol.RuntimeProtocolRef{Name: runtimeprotocol.RuntimeProtocolRefNameValue, Version: "1.0"}, RequestID: requestID, HostRelease: e.release, Outcome: outcome, Payload: raw, Failure: protocolFailure})
	default:
		return engineendpoint.DispatchResponse{}, errors.New("unsupported Runtime response")
	}
	return engineendpoint.DispatchResponse{Operation: operation, RequestID: requestID, Control: control, Outcome: outcome, Failure: protocolFailure}, err
}

type searchOperationRequest[T any] struct {
	Operation string                             `json:"operation"`
	Payload   T                                  `json:"payload"`
	Protocol  runtimeprotocol.RuntimeProtocolRef `json:"protocol"`
	RequestID string                             `json:"request_id"`
}

type searchOperationResponse struct {
	Operation   string                             `json:"operation"`
	Protocol    runtimeprotocol.RuntimeProtocolRef `json:"protocol"`
	RequestID   string                             `json:"request_id"`
	HostRelease protocolcommon.ReleaseVersion      `json:"host_release"`
	Outcome     protocolcommon.Outcome             `json:"outcome"`
	Payload     json.RawMessage                    `json:"payload,omitempty"`
	Failure     *protocolcommon.ProtocolFailure    `json:"failure,omitempty"`
}

func decodeSearchOperationRequest[T any](control []byte, operation string) (searchOperationRequest[T], error) {
	var request searchOperationRequest[T]
	decoder := json.NewDecoder(strings.NewReader(string(control)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return request, errors.New("Search request has trailing content")
	}
	if request.Operation != operation || request.RequestID == "" || request.Protocol.Name != runtimeprotocol.RuntimeProtocolRefNameValue || request.Protocol.Version != "1.0" {
		return request, errors.New("Search request envelope mismatch")
	}
	return request, nil
}

func failure(code string, category protocolcommon.ProtocolFailureCategory) *protocolcommon.ProtocolFailure {
	return &protocolcommon.ProtocolFailure{Code: code, Category: category, Message: "The Runtime operation could not be completed.", Retryable: category == protocolcommon.ProtocolFailureCategoryTransport || category == protocolcommon.ProtocolFailureCategoryIo}
}

func runtimeManifestETag(manifest runtimeprotocol.RuntimeCapabilityManifest) protocolcommon.ManifestETag {
	projection := manifest
	projection.ManifestEtag = protocolcommon.ManifestETag("sha256:" + string(make([]byte, 64)))
	data, _ := json.Marshal(projection)
	var object map[string]json.RawMessage
	_ = json.Unmarshal(data, &object)
	delete(object, "manifest_etag")
	canonical, _ := json.Marshal(object)
	digest := sha256.Sum256(canonical)
	return protocolcommon.ManifestETag("sha256:" + hex.EncodeToString(digest[:]))
}
