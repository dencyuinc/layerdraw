// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopcontract

import (
	"context"
	"errors"
	"reflect"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/registry"
)

type BindingTarget string

const (
	TargetEngine      BindingTarget = "engine_client"
	TargetRuntime     BindingTarget = "runtime_client"
	TargetRegistry    BindingTarget = "registry_client"
	TargetReview      BindingTarget = "review_client"
	TargetHost        BindingTarget = "host_client"
	TargetAccess      BindingTarget = "access_client"
	TargetNativeQuery BindingTarget = "native_query_client"
	TargetSearchIndex BindingTarget = "search_index_client"
	TargetEmbedding   BindingTarget = "embedding_client"
	TargetMCP         BindingTarget = "mcp_client"
)

type Exchange struct {
	Operation string `json:"operation"`
	Control   []byte `json:"control"`
	Blobs     []Blob `json:"blobs"`
}

type Blob struct {
	ID    string `json:"blob_id"`
	Bytes []byte `json:"bytes"`
}

type ExchangeResult struct {
	Control []byte `json:"control"`
	Blobs   []Blob `json:"blobs"`
}

type ClientMethod func(context.Context, Exchange) (ExchangeResult, error)

// EngineClient mirrors the existing generated Engine client methods. Function
// fields make the composition mapping explicit and impossible to satisfy with
// one generic prefix dispatcher.
type EngineClient struct {
	ApplyToHandle, CloseDocument, Compile, ExecuteQuery, FindSymbols, FindUsages,
	FormatScope, GetNeighbors, Handshake, InspectSubgraph, ListModules,
	ListReferences, MaterializeView, OpenDocument, OrganizeWorkspace, PlanExport,
	PreviewFragment, PreviewOperations, PreviewSourcePatch, ReadDeclarations,
	ReadModules, ReadReferences, ReadRows, ReadScope, ReplaceSourceTree ClientMethod
}

type RuntimeClient struct {
	CancelOperation, CommitOperations, ControlAutosave, CloseDocument,
	GetOperationResult, GetStateSnapshot, Handshake, InspectDocument,
	ListRevisions, OpenDocument, PreviewOperations, PreviewRestore,
	RecoverOperations, SaveDocument, StageAsset ClientMethod
}

// These component clients expose their established owner operations. They may
// receive Wails bindings only after the owner package supplies a generated
// envelope decoder and exact BindingMethod entry; opaque generic dispatch is
// intentionally absent.
type RegistryClient struct {
	ListSources, ConfigureSource, ConnectSource, DisconnectSource, Search,
	PlanInstall, CommitPlan, GetTransaction, RecoverTransaction, AuthorArtifact ClientMethod
}
type OwnerEnvelopeIdentity struct{ Operation, RequestID string }
type OwnerResponseIdentity struct {
	Operation, RequestID string
	Outcome              string
}
type OwnerDecoder interface {
	DecodeRequest(expectedOperation string, control []byte) (OwnerEnvelopeIdentity, error)
	DecodeResponse(expectedOperation string, control []byte) (OwnerResponseIdentity, error)
}
type ReviewClient struct {
	Decoder OwnerDecoder
	Submit  ClientMethod
}
type HostClient struct {
	Decoder OwnerDecoder
	Export  ClientMethod
}
type AccessClient struct {
	Decoder                         OwnerDecoder
	AuthorizeRead, ManageAgentScope ClientMethod
}
type NativeQueryClient struct {
	Decoder                                      OwnerDecoder
	ExecuteQuery, ExecuteSearch, ExecuteAnalysis ClientMethod
}
type SearchIndexClient struct {
	Decoder                 OwnerDecoder
	Inspect, Update, Search ClientMethod
}
type EmbeddingClient struct {
	Decoder                   OwnerDecoder
	EmbedDocument, EmbedQuery ClientMethod
}
type MCPClient struct {
	Decoder                                       OwnerDecoder
	InvokeTool, ReadResource, Connect, Disconnect ClientMethod
}

type ClientSet struct {
	Engine      EngineClient
	Runtime     RuntimeClient
	Registry    RegistryClient
	Review      ReviewClient
	Host        HostClient
	Access      AccessClient
	NativeQuery NativeQueryClient
	SearchIndex SearchIndexClient
	Embedding   EmbeddingClient
	MCP         MCPClient
}

func (c ClientSet) Validate() error {
	if !allMethodsPresent(c.Engine) || !allMethodsPresent(c.Runtime) || !allMethodsPresent(c.Registry) || !ownerMethodsPresent(c.Review) || !ownerMethodsPresent(c.Host) || !ownerMethodsPresent(c.Access) || !ownerMethodsPresent(c.NativeQuery) || !ownerMethodsPresent(c.SearchIndex) || !ownerMethodsPresent(c.Embedding) || !ownerMethodsPresent(c.MCP) {
		return errors.New("desktop contract: complete typed client set is required")
	}
	return nil
}

func ownerMethodsPresent(value any) bool {
	fields := reflect.ValueOf(value)
	for index := 0; index < fields.NumField(); index++ {
		field := fields.Field(index)
		if field.Kind() == reflect.Interface {
			if field.IsNil() {
				return false
			}
			continue
		}
		if field.IsNil() {
			return false
		}
	}
	return true
}

func allMethodsPresent(value any) bool {
	fields := reflect.ValueOf(value)
	for index := 0; index < fields.NumField(); index++ {
		if fields.Field(index).IsNil() {
			return false
		}
	}
	return true
}

type BindingMethod struct {
	GeneratedMethod string        `json:"generated_method"`
	Target          BindingTarget `json:"target"`
	ClientMethod    string        `json:"client_method"`
	Operation       string        `json:"operation"`
}

var generatedBindingTable = []BindingMethod{
	{"EngineApplyToHandle", TargetEngine, "ApplyToHandle", string(engineprotocol.ApplyToHandleRequestEnvelopeOperationValue)},
	{"EngineCloseDocument", TargetEngine, "CloseDocument", string(engineprotocol.CloseDocumentRequestEnvelopeOperationValue)},
	{"EngineCompile", TargetEngine, "Compile", string(engineprotocol.CompileRequestEnvelopeOperationValue)},
	{"EngineExecuteQuery", TargetEngine, "ExecuteQuery", string(engineprotocol.ExecuteQueryRequestEnvelopeOperationValue)},
	{"EngineFindSymbols", TargetEngine, "FindSymbols", string(engineprotocol.FindSymbolsRequestEnvelopeOperationValue)},
	{"EngineFindUsages", TargetEngine, "FindUsages", string(engineprotocol.FindUsagesRequestEnvelopeOperationValue)},
	{"EngineFormatScope", TargetEngine, "FormatScope", string(engineprotocol.FormatScopeRequestEnvelopeOperationValue)},
	{"EngineGetNeighbors", TargetEngine, "GetNeighbors", string(engineprotocol.GetNeighborsRequestEnvelopeOperationValue)},
	{"EngineHandshake", TargetEngine, "Handshake", string(engineprotocol.HandshakeRequestEnvelopeOperationValue)},
	{"EngineInspectSubgraph", TargetEngine, "InspectSubgraph", string(engineprotocol.InspectSubgraphRequestEnvelopeOperationValue)},
	{"EngineListModules", TargetEngine, "ListModules", string(engineprotocol.ListModulesRequestEnvelopeOperationValue)},
	{"EngineListReferences", TargetEngine, "ListReferences", string(engineprotocol.ListReferencesRequestEnvelopeOperationValue)},
	{"EngineMaterializeView", TargetEngine, "MaterializeView", string(engineprotocol.MaterializeViewRequestEnvelopeOperationValue)},
	{"EngineOpenDocument", TargetEngine, "OpenDocument", string(engineprotocol.OpenDocumentRequestEnvelopeOperationValue)},
	{"EngineOrganizeWorkspace", TargetEngine, "OrganizeWorkspace", string(engineprotocol.OrganizeWorkspaceRequestEnvelopeOperationValue)},
	{"EnginePlanExport", TargetEngine, "PlanExport", string(engineprotocol.PlanExportRequestEnvelopeOperationValue)},
	{"EnginePreviewFragment", TargetEngine, "PreviewFragment", string(engineprotocol.PreviewFragmentRequestEnvelopeOperationValue)},
	{"EnginePreviewOperations", TargetEngine, "PreviewOperations", string(engineprotocol.PreviewOperationsRequestEnvelopeOperationValue)},
	{"EnginePreviewSourcePatch", TargetEngine, "PreviewSourcePatch", string(engineprotocol.PreviewSourcePatchRequestEnvelopeOperationValue)},
	{"EngineReadDeclarations", TargetEngine, "ReadDeclarations", string(engineprotocol.ReadDeclarationsRequestEnvelopeOperationValue)},
	{"EngineReadModules", TargetEngine, "ReadModules", string(engineprotocol.ReadModulesRequestEnvelopeOperationValue)},
	{"EngineReadReferences", TargetEngine, "ReadReferences", string(engineprotocol.ReadReferencesRequestEnvelopeOperationValue)},
	{"EngineReadRows", TargetEngine, "ReadRows", string(engineprotocol.ReadRowsRequestEnvelopeOperationValue)},
	{"EngineReadScope", TargetEngine, "ReadScope", string(engineprotocol.ReadScopeRequestEnvelopeOperationValue)},
	{"EngineReplaceSourceTree", TargetEngine, "ReplaceSourceTree", string(engineprotocol.ReplaceSourceTreeRequestEnvelopeOperationValue)},
	{"RuntimeCancelOperation", TargetRuntime, "CancelOperation", string(runtimeprotocol.CancelOperationRequestEnvelopeOperationValue)},
	{"RuntimeCommitOperations", TargetRuntime, "CommitOperations", string(runtimeprotocol.CommitOperationsRequestEnvelopeOperationValue)},
	{"RuntimeControlAutosave", TargetRuntime, "ControlAutosave", string(runtimeprotocol.AutosaveControlRequestEnvelopeOperationValue)},
	{"RuntimeCloseDocument", TargetRuntime, "CloseDocument", string(runtimeprotocol.CloseRuntimeDocumentRequestEnvelopeOperationValue)},
	{"RuntimeGetOperationResult", TargetRuntime, "GetOperationResult", string(runtimeprotocol.GetOperationResultRequestEnvelopeOperationValue)},
	{"RuntimeGetStateSnapshot", TargetRuntime, "GetStateSnapshot", string(runtimeprotocol.StateSnapshotRequestEnvelopeOperationValue)},
	{"RuntimeHandshake", TargetRuntime, "Handshake", string(runtimeprotocol.RuntimeHandshakeRequestEnvelopeOperationValue)},
	{"RuntimeInspectDocument", TargetRuntime, "InspectDocument", string(runtimeprotocol.InspectDocumentRequestEnvelopeOperationValue)},
	{"RuntimeListRevisions", TargetRuntime, "ListRevisions", string(runtimeprotocol.ListRevisionsRequestEnvelopeOperationValue)},
	{"RuntimeOpenDocument", TargetRuntime, "OpenDocument", string(runtimeprotocol.OpenDocumentRequestEnvelopeOperationValue)},
	{"RuntimePreviewOperations", TargetRuntime, "PreviewOperations", string(runtimeprotocol.PreviewOperationsRequestEnvelopeOperationValue)},
	{"RuntimePreviewRestore", TargetRuntime, "PreviewRestore", string(runtimeprotocol.RestorePreviewRequestEnvelopeOperationValue)},
	{"RuntimeRecoverOperations", TargetRuntime, "RecoverOperations", string(runtimeprotocol.RecoverOperationsRequestEnvelopeOperationValue)},
	{"RuntimeSaveDocument", TargetRuntime, "SaveDocument", string(runtimeprotocol.SaveDocumentRequestEnvelopeOperationValue)},
	{"RuntimeStageAsset", TargetRuntime, "StageAsset", string(runtimeprotocol.StageAssetRequestEnvelopeOperationValue)},
	{"RegistryListSources", TargetRegistry, "ListSources", string(registry.WireListSources)},
	{"RegistryConfigureSource", TargetRegistry, "ConfigureSource", string(registry.WireConfigureSource)},
	{"RegistryConnectSource", TargetRegistry, "ConnectSource", string(registry.WireConnectSource)},
	{"RegistryDisconnectSource", TargetRegistry, "DisconnectSource", string(registry.WireDisconnectSource)},
	{"RegistrySearch", TargetRegistry, "Search", string(registry.WireSearch)},
	{"RegistryPlanInstall", TargetRegistry, "PlanInstall", string(registry.WirePlan)},
	{"RegistryCommitPlan", TargetRegistry, "CommitPlan", string(registry.WireCommit)},
	{"RegistryGetTransaction", TargetRegistry, "GetTransaction", string(registry.WireGetTransaction)},
	{"RegistryRecoverTransaction", TargetRegistry, "RecoverTransaction", string(registry.WireRecoverTransaction)},
	{"RegistryAuthorArtifact", TargetRegistry, "AuthorArtifact", string(registry.WireAuthorArtifact)},
	{"ReviewSubmit", TargetReview, "Submit", "review.submit"},
	{"HostExport", TargetHost, "Export", "host.export"},
	{"AccessAuthorizeRead", TargetAccess, "AuthorizeRead", "access.authorize_read"},
	{"AccessManageAgentScope", TargetAccess, "ManageAgentScope", "access.manage_agent_scope"},
	{"NativeExecuteQuery", TargetNativeQuery, "ExecuteQuery", "native.execute_query"},
	{"NativeExecuteSearch", TargetNativeQuery, "ExecuteSearch", "native.execute_search"},
	{"NativeExecuteAnalysis", TargetNativeQuery, "ExecuteAnalysis", "native.execute_analysis"},
	{"SearchIndexInspect", TargetSearchIndex, "Inspect", "search_index.inspect"},
	{"SearchIndexUpdate", TargetSearchIndex, "Update", "search_index.update"},
	{"SearchIndexSearch", TargetSearchIndex, "Search", "search_index.search"},
	{"EmbeddingDocument", TargetEmbedding, "EmbedDocument", "embedding.document"},
	{"EmbeddingQuery", TargetEmbedding, "EmbedQuery", "embedding.query"},
	{"MCPInvokeTool", TargetMCP, "InvokeTool", "mcp.invoke_tool"},
	{"MCPReadResource", TargetMCP, "ReadResource", "mcp.read_resource"},
	{"MCPConnect", TargetMCP, "Connect", "mcp.connect"},
	{"MCPDisconnect", TargetMCP, "Disconnect", "mcp.disconnect"},
}

func GeneratedBindingTable() []BindingMethod {
	return append([]BindingMethod(nil), generatedBindingTable...)
}

// Invoke validates the exact generated envelope before selecting one explicit
// ClientSet method. Reflection only reads the compile-time closed function-field
// name in BindingMethod; it never derives a method from untrusted operation text.
func (c ClientSet) Invoke(ctx context.Context, generatedMethod string, exchange Exchange) (ExchangeResult, error) {
	if err := c.Validate(); err != nil {
		return ExchangeResult{}, err
	}
	binding, err := findBinding(generatedMethod, exchange.Operation)
	if err != nil {
		return ExchangeResult{}, err
	}
	var owner any
	switch binding.Target {
	case TargetEngine:
		owner = c.Engine
	case TargetRuntime:
		owner = c.Runtime
	case TargetRegistry:
		owner = c.Registry
	case TargetReview:
		owner = c.Review
	case TargetHost:
		owner = c.Host
	case TargetAccess:
		owner = c.Access
	case TargetNativeQuery:
		owner = c.NativeQuery
	case TargetSearchIndex:
		owner = c.SearchIndex
	case TargetEmbedding:
		owner = c.Embedding
	case TargetMCP:
		owner = c.MCP
	default:
		return ExchangeResult{}, errors.New("desktop contract: binding target is not executable")
	}
	if err := decodeExact(binding, exchange.Control); err != nil {
		decoder := ownerDecoder(owner)
		if decoder == nil {
			return ExchangeResult{}, errors.New("desktop contract: owner decoder is unavailable")
		}
		identity, decodeErr := decoder.DecodeRequest(binding.Operation, exchange.Control)
		if decodeErr != nil || identity.Operation != binding.Operation || identity.RequestID == "" {
			return ExchangeResult{}, errors.New("desktop contract: owner envelope identity is invalid")
		}
	}
	field := reflect.ValueOf(owner).FieldByName(binding.ClientMethod)
	if !field.IsValid() || field.IsNil() {
		return ExchangeResult{}, errors.New("desktop contract: binding client method is unavailable")
	}
	method, ok := field.Interface().(ClientMethod)
	if !ok {
		return ExchangeResult{}, errors.New("desktop contract: binding client method has an invalid type")
	}
	return method(ctx, exchange)
}

func ownerDecoder(owner any) OwnerDecoder {
	field := reflect.ValueOf(owner).FieldByName("Decoder")
	if !field.IsValid() || field.IsNil() {
		return nil
	}
	decoder, _ := field.Interface().(OwnerDecoder)
	return decoder
}

// ValidateExchange prevents a confused deputy: the generated method, outer
// operation and exact generated envelope operation must all identify one row.
func ValidateExchange(method string, exchange Exchange) (BindingMethod, error) {
	binding, err := findBinding(method, exchange.Operation)
	if err != nil {
		return BindingMethod{}, err
	}
	if err := decodeExact(binding, exchange.Control); err != nil {
		return BindingMethod{}, errors.New("desktop contract: generated request envelope is invalid")
	}
	return binding, nil
}

func findBinding(method, operation string) (BindingMethod, error) {
	for _, binding := range generatedBindingTable {
		if binding.GeneratedMethod != method {
			continue
		}
		if operation != binding.Operation {
			return BindingMethod{}, errors.New("desktop contract: outer operation does not match generated method")
		}
		return binding, nil
	}
	return BindingMethod{}, errors.New("desktop contract: generated binding method is not approved")
}

func decodeExact(binding BindingMethod, control []byte) error {
	switch binding.Operation {
	case string(engineprotocol.ApplyToHandleRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeApplyToHandleRequestEnvelope(control)
		return err
	case string(engineprotocol.CloseDocumentRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeCloseDocumentRequestEnvelope(control)
		return err
	case string(engineprotocol.CompileRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeCompileRequestEnvelope(control)
		return err
	case string(engineprotocol.ExecuteQueryRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeExecuteQueryRequestEnvelope(control)
		return err
	case string(engineprotocol.FindSymbolsRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeFindSymbolsRequestEnvelope(control)
		return err
	case string(engineprotocol.FindUsagesRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeFindUsagesRequestEnvelope(control)
		return err
	case string(engineprotocol.FormatScopeRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeFormatScopeRequestEnvelope(control)
		return err
	case string(engineprotocol.GetNeighborsRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeGetNeighborsRequestEnvelope(control)
		return err
	case string(engineprotocol.HandshakeRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeHandshakeRequestEnvelope(control)
		return err
	case string(engineprotocol.InspectSubgraphRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeInspectSubgraphRequestEnvelope(control)
		return err
	case string(engineprotocol.ListModulesRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeListModulesRequestEnvelope(control)
		return err
	case string(engineprotocol.ListReferencesRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeListReferencesRequestEnvelope(control)
		return err
	case string(engineprotocol.MaterializeViewRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeMaterializeViewRequestEnvelope(control)
		return err
	case string(engineprotocol.OpenDocumentRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeOpenDocumentRequestEnvelope(control)
		return err
	case string(engineprotocol.OrganizeWorkspaceRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeOrganizeWorkspaceRequestEnvelope(control)
		return err
	case string(engineprotocol.PlanExportRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodePlanExportRequestEnvelope(control)
		return err
	case string(engineprotocol.PreviewFragmentRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodePreviewFragmentRequestEnvelope(control)
		return err
	case string(engineprotocol.PreviewOperationsRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodePreviewOperationsRequestEnvelope(control)
		return err
	case string(engineprotocol.PreviewSourcePatchRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodePreviewSourcePatchRequestEnvelope(control)
		return err
	case string(engineprotocol.ReadDeclarationsRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeReadDeclarationsRequestEnvelope(control)
		return err
	case string(engineprotocol.ReadModulesRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeReadModulesRequestEnvelope(control)
		return err
	case string(engineprotocol.ReadReferencesRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeReadReferencesRequestEnvelope(control)
		return err
	case string(engineprotocol.ReadRowsRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeReadRowsRequestEnvelope(control)
		return err
	case string(engineprotocol.ReadScopeRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeReadScopeRequestEnvelope(control)
		return err
	case string(engineprotocol.ReplaceSourceTreeRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeReplaceSourceTreeRequestEnvelope(control)
		return err
	case string(runtimeprotocol.CancelOperationRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeCancelOperationRequestEnvelope(control)
		return err
	case string(runtimeprotocol.CommitOperationsRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeCommitOperationsRequestEnvelope(control)
		return err
	case string(runtimeprotocol.AutosaveControlRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeAutosaveControlRequestEnvelope(control)
		return err
	case string(runtimeprotocol.CloseRuntimeDocumentRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeCloseRuntimeDocumentRequestEnvelope(control)
		return err
	case string(runtimeprotocol.GetOperationResultRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeGetOperationResultRequestEnvelope(control)
		return err
	case string(runtimeprotocol.StateSnapshotRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeStateSnapshotRequestEnvelope(control)
		return err
	case string(runtimeprotocol.RuntimeHandshakeRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeRuntimeHandshakeRequestEnvelope(control)
		return err
	case string(runtimeprotocol.InspectDocumentRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeInspectDocumentRequestEnvelope(control)
		return err
	case string(runtimeprotocol.ListRevisionsRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeListRevisionsRequestEnvelope(control)
		return err
	case string(runtimeprotocol.OpenDocumentRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeOpenDocumentRequestEnvelope(control)
		return err
	case string(runtimeprotocol.PreviewOperationsRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodePreviewOperationsRequestEnvelope(control)
		return err
	case string(runtimeprotocol.RestorePreviewRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeRestorePreviewRequestEnvelope(control)
		return err
	case string(runtimeprotocol.RecoverOperationsRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeRecoverOperationsRequestEnvelope(control)
		return err
	case string(runtimeprotocol.SaveDocumentRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeSaveDocumentRequestEnvelope(control)
		return err
	case string(runtimeprotocol.StageAssetRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeStageAssetRequestEnvelope(control)
		return err
	case string(registry.WireListSources), string(registry.WireConfigureSource), string(registry.WireConnectSource), string(registry.WireDisconnectSource), string(registry.WireSearch), string(registry.WirePlan), string(registry.WireCommit), string(registry.WireGetTransaction), string(registry.WireRecoverTransaction), string(registry.WireAuthorArtifact):
		_, err := registry.DecodeWireRequest(control, registry.WireOperation(binding.Operation))
		return err
	default:
		return errors.New("desktop contract: binding has no generated decoder")
	}
}

// decodeExactResponse is deliberately operation-closed like decodeExact. A
// parity adapter must decode the owner's typed response before normalizing it;
// merely observing a common outcome field is insufficient.
func decodeExactResponse(binding BindingMethod, control []byte) error {
	switch binding.Operation {
	case string(engineprotocol.ApplyToHandleRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeApplyToHandleResponseEnvelope(control)
		return err
	case string(engineprotocol.CloseDocumentRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeCloseDocumentResponseEnvelope(control)
		return err
	case string(engineprotocol.CompileRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeCompileResponseEnvelope(control)
		return err
	case string(engineprotocol.ExecuteQueryRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeExecuteQueryResponseEnvelope(control)
		return err
	case string(engineprotocol.FindSymbolsRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeFindSymbolsResponseEnvelope(control)
		return err
	case string(engineprotocol.FindUsagesRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeFindUsagesResponseEnvelope(control)
		return err
	case string(engineprotocol.FormatScopeRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeFormatScopeResponseEnvelope(control)
		return err
	case string(engineprotocol.GetNeighborsRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeGetNeighborsResponseEnvelope(control)
		return err
	case string(engineprotocol.HandshakeRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeHandshakeResponseEnvelope(control)
		return err
	case string(engineprotocol.InspectSubgraphRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeInspectSubgraphResponseEnvelope(control)
		return err
	case string(engineprotocol.ListModulesRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeListModulesResponseEnvelope(control)
		return err
	case string(engineprotocol.ListReferencesRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeListReferencesResponseEnvelope(control)
		return err
	case string(engineprotocol.MaterializeViewRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeMaterializeViewResponseEnvelope(control)
		return err
	case string(engineprotocol.OpenDocumentRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeOpenDocumentResponseEnvelope(control)
		return err
	case string(engineprotocol.OrganizeWorkspaceRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeOrganizeWorkspaceResponseEnvelope(control)
		return err
	case string(engineprotocol.PlanExportRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodePlanExportResponseEnvelope(control)
		return err
	case string(engineprotocol.PreviewFragmentRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodePreviewFragmentResponseEnvelope(control)
		return err
	case string(engineprotocol.PreviewOperationsRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodePreviewOperationsResponseEnvelope(control)
		return err
	case string(engineprotocol.PreviewSourcePatchRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodePreviewSourcePatchResponseEnvelope(control)
		return err
	case string(engineprotocol.ReadDeclarationsRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeReadDeclarationsResponseEnvelope(control)
		return err
	case string(engineprotocol.ReadModulesRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeReadModulesResponseEnvelope(control)
		return err
	case string(engineprotocol.ReadReferencesRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeReadReferencesResponseEnvelope(control)
		return err
	case string(engineprotocol.ReadRowsRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeReadRowsResponseEnvelope(control)
		return err
	case string(engineprotocol.ReadScopeRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeReadScopeResponseEnvelope(control)
		return err
	case string(engineprotocol.ReplaceSourceTreeRequestEnvelopeOperationValue):
		_, err := engineprotocol.DecodeReplaceSourceTreeResponseEnvelope(control)
		return err
	case string(runtimeprotocol.CancelOperationRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeCancelOperationResponseEnvelope(control)
		return err
	case string(runtimeprotocol.CommitOperationsRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeCommitOperationsResponseEnvelope(control)
		return err
	case string(runtimeprotocol.AutosaveControlRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeAutosaveControlResponseEnvelope(control)
		return err
	case string(runtimeprotocol.CloseRuntimeDocumentRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeCloseRuntimeDocumentResponseEnvelope(control)
		return err
	case string(runtimeprotocol.GetOperationResultRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeGetOperationResultResponseEnvelope(control)
		return err
	case string(runtimeprotocol.StateSnapshotRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeStateSnapshotResponseEnvelope(control)
		return err
	case string(runtimeprotocol.RuntimeHandshakeRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeRuntimeHandshakeResponseEnvelope(control)
		return err
	case string(runtimeprotocol.InspectDocumentRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeInspectDocumentResponseEnvelope(control)
		return err
	case string(runtimeprotocol.ListRevisionsRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeListRevisionsResponseEnvelope(control)
		return err
	case string(runtimeprotocol.OpenDocumentRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeOpenDocumentResponseEnvelope(control)
		return err
	case string(runtimeprotocol.PreviewOperationsRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodePreviewOperationsResponseEnvelope(control)
		return err
	case string(runtimeprotocol.RestorePreviewRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeRestorePreviewResponseEnvelope(control)
		return err
	case string(runtimeprotocol.RecoverOperationsRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeRecoverOperationsResponseEnvelope(control)
		return err
	case string(runtimeprotocol.SaveDocumentRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeSaveDocumentResponseEnvelope(control)
		return err
	case string(runtimeprotocol.StageAssetRequestEnvelopeOperationValue):
		_, err := runtimeprotocol.DecodeStageAssetResponseEnvelope(control)
		return err
	case string(registry.WireListSources), string(registry.WireConfigureSource), string(registry.WireConnectSource), string(registry.WireDisconnectSource), string(registry.WireSearch), string(registry.WirePlan), string(registry.WireCommit), string(registry.WireGetTransaction), string(registry.WireRecoverTransaction), string(registry.WireAuthorArtifact):
		_, err := registry.DecodeWireResponse(control, registry.WireOperation(binding.Operation))
		return err
	default:
		return errors.New("desktop contract: binding has no generated response decoder")
	}
}
