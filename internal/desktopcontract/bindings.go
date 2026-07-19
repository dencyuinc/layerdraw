// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package desktopcontract

import (
	"context"
	"errors"
	"reflect"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
)

type BindingTarget string

const (
	TargetEngine   BindingTarget = "engine_client"
	TargetRuntime  BindingTarget = "runtime_client"
	TargetRegistry BindingTarget = "registry_client"
	TargetReview   BindingTarget = "review_client"
	TargetHost     BindingTarget = "host_client"
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
type RegistryClient struct{ Resolve ClientMethod }
type ReviewClient struct{ Submit ClientMethod }
type HostClient struct{ Export ClientMethod }

type ClientSet struct {
	Engine   EngineClient
	Runtime  RuntimeClient
	Registry RegistryClient
	Review   ReviewClient
	Host     HostClient
}

func (c ClientSet) Validate() error {
	if !allMethodsPresent(c.Engine) || !allMethodsPresent(c.Runtime) || !allMethodsPresent(c.Registry) || !allMethodsPresent(c.Review) || !allMethodsPresent(c.Host) {
		return errors.New("desktop contract: complete typed client set is required")
	}
	return nil
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
	binding, err := ValidateExchange(generatedMethod, exchange)
	if err != nil {
		return ExchangeResult{}, err
	}
	var owner any
	switch binding.Target {
	case TargetEngine:
		owner = c.Engine
	case TargetRuntime:
		owner = c.Runtime
	default:
		return ExchangeResult{}, errors.New("desktop contract: binding target is not executable")
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

// ValidateExchange prevents a confused deputy: the generated method, outer
// operation and exact generated envelope operation must all identify one row.
func ValidateExchange(method string, exchange Exchange) (BindingMethod, error) {
	for _, binding := range generatedBindingTable {
		if binding.GeneratedMethod != method {
			continue
		}
		if exchange.Operation != binding.Operation {
			return BindingMethod{}, errors.New("desktop contract: outer operation does not match generated method")
		}
		if err := decodeExact(binding, exchange.Control); err != nil {
			return BindingMethod{}, errors.New("desktop contract: generated request envelope is invalid")
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
	default:
		return errors.New("desktop contract: binding has no generated decoder")
	}
}
