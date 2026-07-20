// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/sourceplanner"
)

const (
	FailureWorkbenchCancelled     = "workbench.execution.cancelled"
	FailureWorkbenchInvalid       = "workbench.request.invalid"
	FailureWorkbenchUnnegotiated  = "workbench.request.unnegotiated"
	FailureWorkbenchUnsupported   = "workbench.operation.unsupported"
	FailureWorkbenchExecution     = "workbench.execution.failed"
	FailureWorkbenchBlobSource    = "workbench.blob_source_failure"
	FailureWorkbenchBlobSink      = "workbench.blob_sink_failure"
	FailureWorkbenchResourceLimit = "workbench.resource_limit"
)

type workbenchDriver interface {
	OpenDocument(context.Context, engine.OpenDocumentInput) (engine.OpenDocumentResult, error)
	ReplaceSourceTree(context.Context, engine.ReplaceSourceTreeInput) (engine.ReplaceSourceTreeResult, error)
	CloseDocument(context.Context, engine.CloseDocumentInput) (engine.CloseDocumentResult, error)
	ListModules(context.Context, engine.ListModulesInput) (engine.ListModulesResult, error)
	ReadModules(context.Context, engine.ReadModulesInput) (engine.ReadModulesResult, error)
	FindSymbols(context.Context, engine.FindSymbolsInput) (engine.FindSymbolsResult, error)
	InspectSubgraph(context.Context, engine.InspectSubgraphInput) (engine.InspectSubgraphResult, error)
	ReadDeclarations(context.Context, engine.ReadDeclarationsInput) (engine.ReadDeclarationsResult, error)
	ReadRows(context.Context, engine.ReadRowsInput) (engine.ReadRowsResult, error)
	ExecuteDocumentQuery(context.Context, engine.ExecuteDocumentQueryInput) (engine.ExecuteDocumentQueryResult, error)
	MaterializeDocumentView(context.Context, engine.MaterializeDocumentViewInput) (engine.MaterializeDocumentViewResult, error)
	PlanExport(context.Context, engine.ExportPlanInput) (engine.ExportPlan, error)
	GetNeighbors(context.Context, engine.GetNeighborsInput) (engine.GetNeighborsResult, error)
	FindUsages(context.Context, engine.FindUsagesInput) (engine.FindUsagesResult, error)
	ReadScope(context.Context, engine.ReadScopeInput) (engine.ReadScopeResult, error)
	ListReferences(context.Context, engine.ListReferencesInput) (engine.ListReferencesResult, error)
	ReadReferences(context.Context, engine.ReadReferencesInput) (engine.ReadReferencesResult, error)
	PreviewSourcePatch(context.Context, engine.PreviewSourcePatchInput) (engine.SourcePlannerPlan, error)
	PreviewFragment(context.Context, engine.PreviewFragmentInput) (engine.SourcePlannerPlan, error)
	PreviewOperations(context.Context, engineprotocol.PreviewOperationsInput) (engineprotocol.WorkbenchPreviewResult, []OutputBlob, error)
	FormatScope(context.Context, engine.FormatScopeInput) (engine.SourcePlannerPlan, error)
	OrganizeWorkspace(context.Context, engine.OrganizeWorkspaceInput) (engine.SourcePlannerPlan, error)
	ApplyToHandle(context.Context, engine.ApplyToHandleInput) (engine.ApplyToHandleResult, error)
}

type workbenchCapableDriver interface {
	compileDriver
	workbenchDriver
}

type workbenchRoute struct {
	Operation string `json:"operation"`
	RequestID string `json:"request_id"`
}

var _ workbenchCapableDriver = engineCompileDriver{}

func (driver engineCompileDriver) OpenDocument(ctx context.Context, input engine.OpenDocumentInput) (engine.OpenDocumentResult, error) {
	return driver.compiler.OpenDocument(ctx, input)
}
func (driver engineCompileDriver) ReplaceSourceTree(ctx context.Context, input engine.ReplaceSourceTreeInput) (engine.ReplaceSourceTreeResult, error) {
	return driver.compiler.ReplaceSourceTree(ctx, input)
}
func (driver engineCompileDriver) CloseDocument(ctx context.Context, input engine.CloseDocumentInput) (engine.CloseDocumentResult, error) {
	return driver.compiler.CloseDocument(ctx, input)
}
func (driver engineCompileDriver) ListModules(ctx context.Context, input engine.ListModulesInput) (engine.ListModulesResult, error) {
	return driver.compiler.ListModules(ctx, input)
}
func (driver engineCompileDriver) ReadModules(ctx context.Context, input engine.ReadModulesInput) (engine.ReadModulesResult, error) {
	return driver.compiler.ReadModules(ctx, input)
}
func (driver engineCompileDriver) FindSymbols(ctx context.Context, input engine.FindSymbolsInput) (engine.FindSymbolsResult, error) {
	return driver.compiler.FindSymbols(ctx, input)
}
func (driver engineCompileDriver) InspectSubgraph(ctx context.Context, input engine.InspectSubgraphInput) (engine.InspectSubgraphResult, error) {
	return driver.compiler.InspectSubgraph(ctx, input)
}
func (driver engineCompileDriver) ReadDeclarations(ctx context.Context, input engine.ReadDeclarationsInput) (engine.ReadDeclarationsResult, error) {
	return driver.compiler.ReadDeclarations(ctx, input)
}
func (driver engineCompileDriver) ReadRows(ctx context.Context, input engine.ReadRowsInput) (engine.ReadRowsResult, error) {
	return driver.compiler.ReadRows(ctx, input)
}
func (driver engineCompileDriver) ExecuteDocumentQuery(ctx context.Context, input engine.ExecuteDocumentQueryInput) (engine.ExecuteDocumentQueryResult, error) {
	return driver.compiler.ExecuteDocumentQuery(ctx, input)
}
func (driver engineCompileDriver) MaterializeDocumentView(ctx context.Context, input engine.MaterializeDocumentViewInput) (engine.MaterializeDocumentViewResult, error) {
	return driver.compiler.MaterializeDocumentView(ctx, input)
}
func (driver engineCompileDriver) PlanExport(ctx context.Context, input engine.ExportPlanInput) (engine.ExportPlan, error) {
	return driver.compiler.PlanExport(ctx, input)
}
func (driver engineCompileDriver) GetNeighbors(ctx context.Context, input engine.GetNeighborsInput) (engine.GetNeighborsResult, error) {
	return driver.compiler.GetNeighbors(ctx, input)
}
func (driver engineCompileDriver) FindUsages(ctx context.Context, input engine.FindUsagesInput) (engine.FindUsagesResult, error) {
	return driver.compiler.FindUsages(ctx, input)
}
func (driver engineCompileDriver) ReadScope(ctx context.Context, input engine.ReadScopeInput) (engine.ReadScopeResult, error) {
	return driver.compiler.ReadScope(ctx, input)
}
func (driver engineCompileDriver) ListReferences(ctx context.Context, input engine.ListReferencesInput) (engine.ListReferencesResult, error) {
	return driver.compiler.ListReferences(ctx, input)
}
func (driver engineCompileDriver) ReadReferences(ctx context.Context, input engine.ReadReferencesInput) (engine.ReadReferencesResult, error) {
	return driver.compiler.ReadReferences(ctx, input)
}
func (driver engineCompileDriver) PreviewSourcePatch(ctx context.Context, input engine.PreviewSourcePatchInput) (engine.SourcePlannerPlan, error) {
	return driver.compiler.PreviewSourcePatch(ctx, input)
}
func (driver engineCompileDriver) PreviewFragment(ctx context.Context, input engine.PreviewFragmentInput) (engine.SourcePlannerPlan, error) {
	return driver.compiler.PreviewFragment(ctx, input)
}
func (driver engineCompileDriver) PreviewOperations(ctx context.Context, input engineprotocol.PreviewOperationsInput) (engineprotocol.WorkbenchPreviewResult, []OutputBlob, error) {
	mapped, err := MapPreviewOperationsPlanInput(engine.CompileInput{}, engine.Snapshot{}, input)
	if err != nil {
		return engineprotocol.WorkbenchPreviewResult{}, nil, err
	}
	planned, err := driver.compiler.PlanWorkbenchSemanticEdits(ctx, mapped)
	if err != nil {
		return engineprotocol.WorkbenchPreviewResult{}, nil, err
	}
	base := input.Preconditions.DocumentGeneration
	proposed := base
	generation, err := strconv.ParseUint(string(base.Value), 10, 64)
	if err != nil || generation == ^uint64(0) {
		return engineprotocol.WorkbenchPreviewResult{}, nil, fmt.Errorf("invalid semantic preview generation")
	}
	proposed.Value = protocolcommon.CanonicalUint64(strconv.FormatUint(generation+1, 10))
	seed := sha256.Sum256([]byte(planned.Plan.SourceDiff.Digest + "\x00" + planned.Plan.SemanticDiff.Digest + "\x00" + string(base.Value)))
	identity := SemanticPreviewIdentity{
		BaseGeneration:     base,
		PreviewID:          engineprotocol.PreviewID{EndpointInstanceID: base.DocumentHandle.EndpointInstanceID, Value: "preview_" + hex.EncodeToString(seed[:12])},
		ProposedGeneration: proposed,
	}
	result, blobs, err := MapSemanticEditPlanResult(planned.Plan, identity, mapped.Limits)
	if err != nil {
		return engineprotocol.WorkbenchPreviewResult{}, nil, err
	}
	if result.Status == "valid" {
		var preview sourceplanner.WorkbenchPreviewResult
		if err := convertStruct(result, &preview); err != nil {
			return engineprotocol.WorkbenchPreviewResult{}, nil, err
		}
		var candidate sourceplanner.CompileInput
		if err := convertStruct(planned.Candidate, &candidate); err != nil {
			return engineprotocol.WorkbenchPreviewResult{}, nil, err
		}
		attachments := make(sourceplanner.PlannerBlobs, len(blobs))
		for _, blob := range blobs {
			attachments[blob.Ref.BlobID] = append([]byte(nil), blob.Bytes...)
		}
		if err := driver.compiler.RetainWorkbenchSemanticPreview(ctx, planned.Generation, sourceplanner.SourcePlan{Preview: preview, Candidate: candidate, Attachments: attachments}); err != nil {
			return engineprotocol.WorkbenchPreviewResult{}, nil, err
		}
	}
	return result, blobs, nil
}
func (driver engineCompileDriver) FormatScope(ctx context.Context, input engine.FormatScopeInput) (engine.SourcePlannerPlan, error) {
	return driver.compiler.FormatScope(ctx, input)
}
func (driver engineCompileDriver) OrganizeWorkspace(ctx context.Context, input engine.OrganizeWorkspaceInput) (engine.SourcePlannerPlan, error) {
	return driver.compiler.OrganizeWorkspace(ctx, input)
}
func (driver engineCompileDriver) ApplyToHandle(ctx context.Context, input engine.ApplyToHandleInput) (engine.ApplyToHandleResult, error) {
	return driver.compiler.ApplyToHandle(ctx, input)
}

// PrepareDispatch validates and prepares any non-handshake Engine operation
// implemented by this endpoint. Compile keeps its existing plan; Workbench
// operations use the same attachment admission and single-use lifecycle.
func (d *CompileDispatcher) PrepareDispatch(
	ctx context.Context,
	negotiated *NegotiatedContext,
	operation string,
	control []byte,
) (plan DispatchPlan, terminal *DispatchResponse, err error) {
	if operation == OperationCompile {
		request, decodeErr := engineprotocol.DecodeCompileRequestEnvelope(control)
		if decodeErr != nil {
			return nil, nil, fmt.Errorf("decode compile request: %w", decodeErr)
		}
		compilePlan, compileTerminal, prepareErr := d.PrepareCompile(ctx, negotiated, request)
		if prepareErr != nil {
			return nil, nil, prepareErr
		}
		if compileTerminal != nil {
			encoded, encodeErr := engineprotocol.EncodeCompileResponseEnvelope(*compileTerminal)
			if encodeErr != nil {
				return nil, nil, encodeErr
			}
			return nil, &DispatchResponse{Operation: OperationCompile, RequestID: compileTerminal.RequestID, Control: encoded, Outcome: compileTerminal.Outcome, Failure: compileTerminal.Failure}, nil
		}
		return compilePlan, nil, nil
	}
	return d.prepareWorkbench(ctx, negotiated, operation, control)
}

// DispatchCancellationResponse builds the operation-correct cancellation
// envelope for transports that win the cancellation race before execution
// returns.
func (d *CompileDispatcher) DispatchCancellationResponse(operation, requestID string, release protocolcommon.ReleaseVersion) (DispatchResponse, error) {
	if operation == OperationCompile {
		response, err := d.CompileCancellationResponse(requestID)
		if err != nil {
			return DispatchResponse{}, err
		}
		control, err := engineprotocol.EncodeCompileResponseEnvelope(response)
		if err != nil {
			return DispatchResponse{}, err
		}
		return DispatchResponse{Operation: operation, RequestID: requestID, Control: control, Outcome: response.Outcome, Failure: response.Failure}, nil
	}
	failure := workbenchFailure(protocolcommon.ProtocolFailureCategoryCancelled, FailureWorkbenchCancelled, "cancelled", "Execution was cancelled.", false, nil)
	control, err := encodeWorkbenchTerminal(operation, nil, []semantic.Diagnostic{}, failure, protocolcommon.OutcomeCancelled, release, requestID)
	if err != nil {
		return DispatchResponse{}, err
	}
	return DispatchResponse{Operation: operation, RequestID: requestID, Control: control, Outcome: protocolcommon.OutcomeCancelled}, nil
}

// DispatchTransportResponse builds the operation-correct transport failure
// envelope for correlated stream failures.
func (d *CompileDispatcher) DispatchTransportResponse(operation, requestID string, release protocolcommon.ReleaseVersion) (DispatchResponse, error) {
	return d.DispatchTransportFailureResponse(operation, requestID, release, CompileTransportProtocolViolation)
}

// DispatchTransportFailureResponse builds the operation-correct transport
// failure envelope while preserving the compile transport failure reason.
func (d *CompileDispatcher) DispatchTransportFailureResponse(operation, requestID string, release protocolcommon.ReleaseVersion, reason CompileTransportFailure) (DispatchResponse, error) {
	if operation == OperationCompile {
		response, err := d.CompileTransportResponse(requestID, reason)
		if err != nil {
			return DispatchResponse{}, err
		}
		control, err := engineprotocol.EncodeCompileResponseEnvelope(response)
		if err != nil {
			return DispatchResponse{}, err
		}
		return DispatchResponse{Operation: operation, RequestID: requestID, Control: control, Outcome: response.Outcome, Failure: response.Failure}, nil
	}
	failure := workbenchFailure(protocolcommon.ProtocolFailureCategoryTransport, FailureWorkbenchInvalid, "execution_failed", "Transport stream failed.", false, nil)
	control, err := encodeWorkbenchTerminal(operation, nil, []semantic.Diagnostic{}, failure, protocolcommon.OutcomeFailed, release, requestID)
	if err != nil {
		return DispatchResponse{}, err
	}
	return DispatchResponse{Operation: operation, RequestID: requestID, Control: control, Outcome: protocolcommon.OutcomeFailed}, nil
}

type workbenchPlanState uint8

const (
	workbenchPlanReady workbenchPlanState = iota
	workbenchPlanExecuting
	workbenchPlanFinished
	workbenchPlanAborted
)

type workbenchPlan struct {
	mu             sync.Mutex
	driver         workbenchDriver
	requestID      string
	operation      string
	release        protocolcommon.ReleaseVersion
	requirements   []BlobRequirement
	budget         CompileAdmissionBudget
	uses           []blobUse
	state          workbenchPlanState
	abortRequested bool
	executeCancel  context.CancelFunc
	run            func(context.Context, workbenchDriver, map[string][]byte) (any, []OutputBlob, error)
	encode         func(any, []semantic.Diagnostic, *engineprotocol.WorkbenchFailure, protocolcommon.Outcome, protocolcommon.ReleaseVersion, string) ([]byte, error)
}

func (p *workbenchPlan) BlobRequirements() []BlobRequirement {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == workbenchPlanFinished || p.state == workbenchPlanAborted {
		return nil
	}
	return cloneBlobRequirements(p.requirements)
}

func (p *workbenchPlan) AdmissionBudget() CompileAdmissionBudget {
	if p == nil {
		return CompileAdmissionBudget{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == workbenchPlanFinished || p.state == workbenchPlanAborted {
		return CompileAdmissionBudget{}
	}
	return p.budget
}

func (p *workbenchPlan) Abort() {
	if p == nil {
		return
	}
	p.mu.Lock()
	cancel := p.executeCancel
	if p.state == workbenchPlanReady {
		p.state = workbenchPlanAborted
	}
	p.abortRequested = true
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (p *workbenchPlan) Execute(context.Context, BlobSource, BlobSink) (engineprotocol.CompileResponseEnvelope, error) {
	return engineprotocol.CompileResponseEnvelope{}, fmt.Errorf("workbench plan is not a compile plan")
}

func (p *workbenchPlan) ExecuteDispatch(ctx context.Context, source BlobSource, sink BlobSink) (DispatchResponse, error) {
	if p == nil || ctx == nil || source == nil || sink == nil {
		return DispatchResponse{}, fmt.Errorf("invalid workbench dispatch")
	}
	p.mu.Lock()
	if p.state != workbenchPlanReady {
		p.mu.Unlock()
		return DispatchResponse{}, fmt.Errorf("workbench plan consumed")
	}
	executeContext, cancel := context.WithCancel(ctx)
	p.state = workbenchPlanExecuting
	p.executeCancel = cancel
	aborted := p.abortRequested
	p.mu.Unlock()
	defer cancel()
	if aborted {
		return DispatchResponse{}, context.Canceled
	}
	owned, lease, failure := acquireBlobUses(executeContext, p.uses, source)
	if lease != nil {
		defer lease.Release(executeContext)
	}
	if failure != nil {
		return p.failed(FailureWorkbenchBlobSource, protocolcommon.ProtocolFailureCategoryTransport)
	}
	payload, blobs, err := p.run(executeContext, p.driver, owned)
	if err != nil {
		return p.errorResponse(err)
	}
	control, err := p.encode(payload, []semantic.Diagnostic{}, nil, protocolcommon.OutcomeSuccess, p.release, p.requestID)
	if err != nil {
		return p.errorResponse(&engine.WorkbenchError{
			Code:     "engine.workbench.result_invariant",
			Category: engine.WorkbenchErrorInvariant,
		})
	}
	if err := sink.Publish(executeContext, cloneOutputBlobs(blobs)); err != nil {
		return p.failed(FailureWorkbenchBlobSink, protocolcommon.ProtocolFailureCategoryIo)
	}
	p.mu.Lock()
	p.state = workbenchPlanFinished
	p.executeCancel = nil
	p.mu.Unlock()
	return DispatchResponse{Operation: p.operation, RequestID: p.requestID, Control: control, Outcome: protocolcommon.OutcomeSuccess}, nil
}

func (p *workbenchPlan) failed(code string, category protocolcommon.ProtocolFailureCategory) (DispatchResponse, error) {
	failure := workbenchFailure(category, code, "execution_failed", "Workbench operation failed.", true, nil)
	control, err := p.encode(nil, []semantic.Diagnostic{}, &failure, protocolcommon.OutcomeFailed, p.release, p.requestID)
	if err != nil {
		return DispatchResponse{}, err
	}
	return DispatchResponse{Operation: p.operation, RequestID: p.requestID, Control: control, Outcome: protocolcommon.OutcomeFailed}, nil
}

func (p *workbenchPlan) errorResponse(err error) (DispatchResponse, error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		failure := workbenchFailure(protocolcommon.ProtocolFailureCategoryCancelled, FailureWorkbenchCancelled, "cancelled", "Execution was cancelled.", false, nil)
		control, encodeErr := p.encode(nil, []semantic.Diagnostic{}, &failure, protocolcommon.OutcomeCancelled, p.release, p.requestID)
		if encodeErr != nil {
			return DispatchResponse{}, encodeErr
		}
		return DispatchResponse{Operation: p.operation, RequestID: p.requestID, Control: control, Outcome: protocolcommon.OutcomeCancelled}, nil
	}
	var wb *engine.WorkbenchError
	if errors.As(err, &wb) {
		switch wb.Category {
		case engine.WorkbenchErrorCursorInvalid,
			engine.WorkbenchErrorGenerationStale,
			engine.WorkbenchErrorHandleInvalid,
			engine.WorkbenchErrorInputInvalid,
			engine.WorkbenchErrorNotFound,
			engine.WorkbenchErrorOperationDisabled:
			control, encodeErr := p.encode(nil, workbenchDiagnostic(wb), nil, protocolcommon.OutcomeRejected, p.release, p.requestID)
			if encodeErr != nil {
				return DispatchResponse{}, encodeErr
			}
			return DispatchResponse{Operation: p.operation, RequestID: p.requestID, Control: control, Outcome: protocolcommon.OutcomeRejected}, nil
		}
		category, workbenchCategory := mapWorkbenchFailureCategory(wb)
		var details protocolcommon.JsonObject
		if wb.Resource != "" {
			details = protocolcommon.JsonObject{}
			details["resource"] = stringJSON(wb.Resource)
			details["limit"] = stringJSON(strconv.FormatInt(wb.Limit, 10))
			details["observed"] = stringJSON(strconv.FormatInt(wb.Observed, 10))
		}
		outcome := protocolcommon.OutcomeFailed
		if wb.Category == engine.WorkbenchErrorCancelled {
			outcome = protocolcommon.OutcomeCancelled
		}
		failure := workbenchFailure(category, wb.Code, workbenchCategory, "Workbench operation failed.", false, details)
		control, encodeErr := p.encode(nil, []semantic.Diagnostic{}, &failure, outcome, p.release, p.requestID)
		if encodeErr != nil {
			return DispatchResponse{}, encodeErr
		}
		return DispatchResponse{Operation: p.operation, RequestID: p.requestID, Control: control, Outcome: outcome}, nil
	}
	var queryRejected *engine.QueryExecutionRejection
	if errors.As(err, &queryRejected) {
		diagnostics, mapErr := mapDiagnostics(queryRejected.Diagnostics)
		if mapErr != nil {
			return DispatchResponse{}, mapErr
		}
		control, encodeErr := p.encode(nil, diagnostics, nil, protocolcommon.OutcomeRejected, p.release, p.requestID)
		if encodeErr != nil {
			return DispatchResponse{}, encodeErr
		}
		return DispatchResponse{Operation: p.operation, RequestID: p.requestID, Control: control, Outcome: protocolcommon.OutcomeRejected}, nil
	}
	var viewRejected *engine.ViewMaterializationRejection
	if errors.As(err, &viewRejected) {
		diagnostics, mapErr := mapDiagnostics(viewRejected.Diagnostics)
		if mapErr != nil {
			return DispatchResponse{}, mapErr
		}
		control, encodeErr := p.encode(nil, diagnostics, nil, protocolcommon.OutcomeRejected, p.release, p.requestID)
		if encodeErr != nil {
			return DispatchResponse{}, encodeErr
		}
		return DispatchResponse{Operation: p.operation, RequestID: p.requestID, Control: control, Outcome: protocolcommon.OutcomeRejected}, nil
	}
	return p.failed(FailureWorkbenchExecution, protocolcommon.ProtocolFailureCategoryIo)
}

func (d *CompileDispatcher) prepareWorkbench(ctx context.Context, negotiated *NegotiatedContext, operation string, control []byte) (DispatchPlan, *DispatchResponse, error) {
	if d == nil {
		return nil, nil, fmt.Errorf("nil dispatcher")
	}
	if ctx == nil {
		return nil, nil, fmt.Errorf("nil workbench context")
	}
	driver, ok := d.compiler.(workbenchDriver)
	if !ok {
		return nil, nil, fmt.Errorf("workbench driver unavailable")
	}
	requestID, deadline, err := decodeWorkbenchRequestID(operation, control)
	if err != nil {
		return nil, nil, err
	}
	if err := ValidateRequestID(requestID); err != nil {
		return nil, nil, fmt.Errorf("untrustworthy workbench request ID: %w", err)
	}
	if negotiated == nil || !negotiated.SupportsOperation(operation) {
		response, encodeErr := encodeWorkbenchTerminal(operation, nil, []semantic.Diagnostic{}, workbenchFailure(protocolcommon.ProtocolFailureCategoryInvariant, FailureWorkbenchUnnegotiated, "execution_failed", "Operation was not negotiated.", false, nil), protocolcommon.OutcomeFailed, protocolcommon.ReleaseVersion("0.0.0-dev"), requestID)
		if encodeErr != nil {
			return nil, nil, encodeErr
		}
		return nil, &DispatchResponse{Operation: operation, RequestID: requestID, Control: response}, nil
	}
	_ = deadline
	plan, err := d.buildWorkbenchPlan(ctx, negotiated, driver, operation, control, requestID)
	if err != nil {
		response, encodeErr := encodeWorkbenchTerminal(operation, nil, []semantic.Diagnostic{}, workbenchFailure(protocolcommon.ProtocolFailureCategoryInvariant, FailureWorkbenchInvalid, "execution_failed", "Request is invalid.", false, nil), protocolcommon.OutcomeFailed, negotiated.EngineRelease(), requestID)
		if encodeErr != nil {
			return nil, nil, encodeErr
		}
		return nil, &DispatchResponse{Operation: operation, RequestID: requestID, Control: response}, nil
	}
	return plan, nil, nil
}

func (d *CompileDispatcher) buildWorkbenchPlan(ctx context.Context, negotiated *NegotiatedContext, driver workbenchDriver, operation string, control []byte, requestID string) (*workbenchPlan, error) {
	plan := &workbenchPlan{driver: driver, requestID: requestID, operation: operation, release: negotiated.EngineRelease()}
	setPayload := func(payload any, run func(context.Context, workbenchDriver, map[string][]byte) (any, []OutputBlob, error)) error {
		uses, failure := enumerateWorkbenchBlobUses(payload)
		if failure != nil {
			return fmt.Errorf("workbench blob refs: %s", failure.Code)
		}
		if failure := validateBlobAliases(ctx, uses); failure != nil {
			return fmt.Errorf("workbench blob aliases: %s", failure.Code)
		}
		requirements, bytes, failure := buildBlobRequirements(ctx, uses)
		if failure != nil {
			return fmt.Errorf("workbench blob requirements: %s", failure.Code)
		}
		plan.uses = uses
		plan.requirements = requirements
		plan.budget = CompileAdmissionBudget{RequiredBlobCount: int64(len(requirements)), RequiredBlobBytes: bytes}
		plan.run = run
		plan.encode = func(result any, diagnostics []semantic.Diagnostic, failure *engineprotocol.WorkbenchFailure, outcome protocolcommon.Outcome, release protocolcommon.ReleaseVersion, requestID string) ([]byte, error) {
			return encodeWorkbenchTerminal(operation, result, diagnostics, derefFailure(failure), outcome, release, requestID)
		}
		return nil
	}
	switch operation {
	case OperationOpenDocument:
		request, err := engineprotocol.DecodeOpenDocumentRequestEnvelope(control)
		if err != nil {
			return nil, err
		}
		prepared, diagnostics, failure := prepareCompileInput(ctx, negotiated, request.Payload.CompileInput)
		if failure != nil || len(diagnostics) != 0 {
			return nil, fmt.Errorf("invalid open compile input")
		}
		plan.uses, plan.requirements, plan.budget = prepared.uses, prepared.requirements, prepared.budget
		plan.run = func(ctx context.Context, driver workbenchDriver, owned map[string][]byte) (any, []OutputBlob, error) {
			input := engine.OpenDocumentInput{CompileInput: mapPreparedCompileInput(prepared, owned)}
			if err := convertStruct(request.Payload.RequestedLimits, &input.RequestedLimits); err != nil {
				return nil, nil, err
			}
			result, err := driver.OpenDocument(ctx, input)
			return result, collectOutputBlobs(result), err
		}
	case OperationReplaceSourceTree:
		request, err := engineprotocol.DecodeReplaceSourceTreeRequestEnvelope(control)
		if err != nil {
			return nil, err
		}
		prepared, diagnostics, failure := prepareCompileInput(ctx, negotiated, request.Payload.CompileInput)
		if failure != nil || len(diagnostics) != 0 {
			return nil, fmt.Errorf("invalid replace compile input")
		}
		plan.uses, plan.requirements, plan.budget = prepared.uses, prepared.requirements, prepared.budget
		plan.run = func(ctx context.Context, driver workbenchDriver, owned map[string][]byte) (any, []OutputBlob, error) {
			input := engine.ReplaceSourceTreeInput{CompileInput: mapPreparedCompileInput(prepared, owned)}
			if err := convertStruct(request.Payload.ExpectedGeneration, &input.ExpectedGeneration); err != nil {
				return nil, nil, err
			}
			result, err := driver.ReplaceSourceTree(ctx, input)
			return result, collectOutputBlobs(result), err
		}
	default:
		payload, run, err := workbenchPayloadRunner(operation, control)
		if err != nil {
			return nil, err
		}
		if err := setPayload(payload, run); err != nil {
			return nil, err
		}
	}
	if plan.encode == nil {
		plan.encode = func(result any, diagnostics []semantic.Diagnostic, failure *engineprotocol.WorkbenchFailure, outcome protocolcommon.Outcome, release protocolcommon.ReleaseVersion, requestID string) ([]byte, error) {
			return encodeWorkbenchTerminal(operation, result, diagnostics, derefFailure(failure), outcome, release, requestID)
		}
	}
	return plan, nil
}

func derefFailure(value *engineprotocol.WorkbenchFailure) engineprotocol.WorkbenchFailure {
	if value == nil {
		return engineprotocol.WorkbenchFailure{}
	}
	return *value
}

func workbenchPayloadRunner(operation string, control []byte) (any, func(context.Context, workbenchDriver, map[string][]byte) (any, []OutputBlob, error), error) {
	switch operation {
	case OperationCloseDocument:
		request, err := engineprotocol.DecodeCloseDocumentRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		return request.Payload, runMapped[engine.CloseDocumentInput](request.Payload, func(ctx context.Context, driver workbenchDriver, input engine.CloseDocumentInput) (any, error) {
			return driver.CloseDocument(ctx, input)
		}), nil
	case OperationListModules:
		request, err := engineprotocol.DecodeListModulesRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		return request.Payload, runMapped[engine.ListModulesInput](request.Payload, func(ctx context.Context, driver workbenchDriver, input engine.ListModulesInput) (any, error) {
			return driver.ListModules(ctx, input)
		}), nil
	case OperationReadModules:
		request, err := engineprotocol.DecodeReadModulesRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		return request.Payload, runMapped[engine.ReadModulesInput](request.Payload, func(ctx context.Context, driver workbenchDriver, input engine.ReadModulesInput) (any, error) {
			return driver.ReadModules(ctx, input)
		}), nil
	case OperationFindSymbols:
		request, err := engineprotocol.DecodeFindSymbolsRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		return request.Payload, runMapped[engine.FindSymbolsInput](request.Payload, func(ctx context.Context, driver workbenchDriver, input engine.FindSymbolsInput) (any, error) {
			return driver.FindSymbols(ctx, input)
		}), nil
	case OperationInspectSubgraph:
		request, err := engineprotocol.DecodeInspectSubgraphRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		return request.Payload, runMapped[engine.InspectSubgraphInput](request.Payload, func(ctx context.Context, driver workbenchDriver, input engine.InspectSubgraphInput) (any, error) {
			return driver.InspectSubgraph(ctx, input)
		}), nil
	case OperationReadDeclarations:
		request, err := engineprotocol.DecodeReadDeclarationsRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		return request.Payload, runMapped[engine.ReadDeclarationsInput](request.Payload, func(ctx context.Context, driver workbenchDriver, input engine.ReadDeclarationsInput) (any, error) {
			return driver.ReadDeclarations(ctx, input)
		}), nil
	case OperationReadRows:
		request, err := engineprotocol.DecodeReadRowsRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		return request.Payload, runMapped[engine.ReadRowsInput](request.Payload, func(ctx context.Context, driver workbenchDriver, input engine.ReadRowsInput) (any, error) {
			return driver.ReadRows(ctx, input)
		}), nil
	case OperationExecuteQuery:
		request, err := engineprotocol.DecodeExecuteQueryRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		return request.Payload, runExecuteQuery(request.Payload), nil
	case OperationMaterializeView:
		request, err := engineprotocol.DecodeMaterializeViewRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		return request.Payload, runMaterializeView(request.Payload), nil
	case OperationPlanExport:
		request, err := engineprotocol.DecodePlanExportRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		return request.Payload, runPlanExport(request.Payload), nil
	case OperationGetNeighbors:
		request, err := engineprotocol.DecodeGetNeighborsRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		return request.Payload, runMapped[engine.GetNeighborsInput](request.Payload, func(ctx context.Context, driver workbenchDriver, input engine.GetNeighborsInput) (any, error) {
			return driver.GetNeighbors(ctx, input)
		}), nil
	case OperationFindUsages:
		request, err := engineprotocol.DecodeFindUsagesRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		return request.Payload, runMapped[engine.FindUsagesInput](request.Payload, func(ctx context.Context, driver workbenchDriver, input engine.FindUsagesInput) (any, error) {
			return driver.FindUsages(ctx, input)
		}), nil
	case OperationReadScope:
		request, err := engineprotocol.DecodeReadScopeRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		return request.Payload, runMapped[engine.ReadScopeInput](request.Payload, func(ctx context.Context, driver workbenchDriver, input engine.ReadScopeInput) (any, error) {
			return driver.ReadScope(ctx, input)
		}), nil
	case OperationListReferences:
		request, err := engineprotocol.DecodeListReferencesRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		return request.Payload, runMapped[engine.ListReferencesInput](request.Payload, func(ctx context.Context, driver workbenchDriver, input engine.ListReferencesInput) (any, error) {
			return driver.ListReferences(ctx, input)
		}), nil
	case OperationReadReferences:
		request, err := engineprotocol.DecodeReadReferencesRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		return request.Payload, runMapped[engine.ReadReferencesInput](request.Payload, func(ctx context.Context, driver workbenchDriver, input engine.ReadReferencesInput) (any, error) {
			return driver.ReadReferences(ctx, input)
		}), nil
	case OperationPreviewSourcePatch:
		request, err := engineprotocol.DecodePreviewSourcePatchRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		return request.Payload, runSourcePlan[engine.PreviewSourcePatchInput](request.Payload, func(ctx context.Context, driver workbenchDriver, input engine.PreviewSourcePatchInput) (engine.SourcePlannerPlan, error) {
			return driver.PreviewSourcePatch(ctx, input)
		}), nil
	case OperationPreviewFragment:
		request, err := engineprotocol.DecodePreviewFragmentRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		return request.Payload, runSourcePlan[engine.PreviewFragmentInput](request.Payload, func(ctx context.Context, driver workbenchDriver, input engine.PreviewFragmentInput) (engine.SourcePlannerPlan, error) {
			return driver.PreviewFragment(ctx, input)
		}), nil
	case OperationPreviewOperations:
		request, err := engineprotocol.DecodePreviewOperationsRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		return request.Payload, func(ctx context.Context, driver workbenchDriver, _ map[string][]byte) (any, []OutputBlob, error) {
			return driver.PreviewOperations(ctx, request.Payload)
		}, nil
	case OperationFormatScope:
		request, err := engineprotocol.DecodeFormatScopeRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		return request.Payload, runSourcePlan[engine.FormatScopeInput](request.Payload, func(ctx context.Context, driver workbenchDriver, input engine.FormatScopeInput) (engine.SourcePlannerPlan, error) {
			return driver.FormatScope(ctx, input)
		}), nil
	case OperationOrganizeWorkspace:
		request, err := engineprotocol.DecodeOrganizeWorkspaceRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		return request.Payload, runSourcePlan[engine.OrganizeWorkspaceInput](request.Payload, func(ctx context.Context, driver workbenchDriver, input engine.OrganizeWorkspaceInput) (engine.SourcePlannerPlan, error) {
			return driver.OrganizeWorkspace(ctx, input)
		}), nil
	case OperationApplyToHandle:
		request, err := engineprotocol.DecodeApplyToHandleRequestEnvelope(control)
		if err != nil {
			return nil, nil, err
		}
		return request.Payload, runMapped[engine.ApplyToHandleInput](request.Payload, func(ctx context.Context, driver workbenchDriver, input engine.ApplyToHandleInput) (any, error) {
			return driver.ApplyToHandle(ctx, input)
		}), nil
	default:
		return nil, nil, fmt.Errorf("unsupported workbench operation %q", operation)
	}
}

func runMapped[T any](payload any, call func(context.Context, workbenchDriver, T) (any, error)) func(context.Context, workbenchDriver, map[string][]byte) (any, []OutputBlob, error) {
	return func(ctx context.Context, driver workbenchDriver, _ map[string][]byte) (any, []OutputBlob, error) {
		var input T
		if err := convertStruct(payload, &input); err != nil {
			return nil, nil, err
		}
		result, err := call(ctx, driver, input)
		return result, collectOutputBlobs(result), err
	}
}

func runSourcePlan[T any](payload any, call func(context.Context, workbenchDriver, T) (engine.SourcePlannerPlan, error)) func(context.Context, workbenchDriver, map[string][]byte) (any, []OutputBlob, error) {
	return func(ctx context.Context, driver workbenchDriver, owned map[string][]byte) (any, []OutputBlob, error) {
		var input T
		if err := convertStruct(payload, &input); err != nil {
			return nil, nil, err
		}
		if err := copyDocumentGenerationFromPreconditions(payload, &input); err != nil {
			return nil, nil, err
		}
		value := reflect.ValueOf(&input).Elem()
		if field := value.FieldByName("Blobs"); field.IsValid() && field.CanSet() {
			field.Set(reflect.ValueOf(engine.SourcePlannerBlobs(owned)))
		}
		plan, err := call(ctx, driver, input)
		if err != nil {
			return nil, nil, err
		}
		return plan.Preview, sourcePlanOutputBlobs(plan), nil
	}
}

func decodeWorkbenchRequestID(operation string, control []byte) (string, *protocolcommon.Rfc3339Time, error) {
	var route workbenchRoute
	if err := json.Unmarshal(control, &route); err != nil {
		return "", nil, err
	}
	if route.Operation != operation || route.RequestID == "" {
		return "", nil, fmt.Errorf("operation mismatch")
	}
	var meta struct {
		DeadlineAt *protocolcommon.Rfc3339Time `json:"deadline_at,omitempty"`
	}
	_ = json.Unmarshal(control, &meta)
	return route.RequestID, meta.DeadlineAt, nil
}

func encodeWorkbenchTerminal(operation string, payload any, diagnostics []semantic.Diagnostic, failure engineprotocol.WorkbenchFailure, outcome protocolcommon.Outcome, release protocolcommon.ReleaseVersion, requestID string) ([]byte, error) {
	if diagnostics == nil {
		diagnostics = []semantic.Diagnostic{}
	}
	hasFailure := failure.Code != ""
	mapPayload := func(target any) error {
		if outcome == protocolcommon.OutcomeSuccess && payload != nil {
			return convertStruct(payload, target)
		}
		return nil
	}
	switch operation {
	case OperationOpenDocument:
		var out engineprotocol.OpenDocumentResult
		if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodeOpenDocumentResponseEnvelope(engineprotocol.OpenDocumentResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationReplaceSourceTree:
		var out engineprotocol.ReplaceSourceTreeResult
		if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodeReplaceSourceTreeResponseEnvelope(engineprotocol.ReplaceSourceTreeResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationCloseDocument:
		var out engineprotocol.CloseDocumentResult
		if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodeCloseDocumentResponseEnvelope(engineprotocol.CloseDocumentResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationListModules:
		var out engineprotocol.ListModulesResult
		if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodeListModulesResponseEnvelope(engineprotocol.ListModulesResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationReadModules:
		var out engineprotocol.ReadModulesResult
		if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodeReadModulesResponseEnvelope(engineprotocol.ReadModulesResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationFindSymbols:
		var out engineprotocol.FindSymbolsResult
		if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodeFindSymbolsResponseEnvelope(engineprotocol.FindSymbolsResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationInspectSubgraph:
		var out engineprotocol.InspectSubgraphResult
		if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodeInspectSubgraphResponseEnvelope(engineprotocol.InspectSubgraphResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationReadDeclarations:
		var out engineprotocol.ReadDeclarationsResult
		if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodeReadDeclarationsResponseEnvelope(engineprotocol.ReadDeclarationsResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationReadRows:
		var out engineprotocol.ReadRowsResult
		if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodeReadRowsResponseEnvelope(engineprotocol.ReadRowsResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationExecuteQuery:
		var out engineprotocol.ExecuteQueryResult
		if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodeExecuteQueryResponseEnvelope(engineprotocol.ExecuteQueryResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationMaterializeView:
		var out engineprotocol.MaterializeViewResult
		if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodeMaterializeViewResponseEnvelope(engineprotocol.MaterializeViewResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationPlanExport:
		var out engineprotocol.PlanExportResult
		if typed, ok := payload.(engineprotocol.PlanExportResult); ok && outcome == protocolcommon.OutcomeSuccess {
			out = typed
		} else if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodePlanExportResponseEnvelope(engineprotocol.PlanExportResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationGetNeighbors:
		var out engineprotocol.GetNeighborsResult
		if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodeGetNeighborsResponseEnvelope(engineprotocol.GetNeighborsResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationFindUsages:
		var out engineprotocol.FindUsagesResult
		if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodeFindUsagesResponseEnvelope(engineprotocol.FindUsagesResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationReadScope:
		var out engineprotocol.ReadScopeResult
		if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodeReadScopeResponseEnvelope(engineprotocol.ReadScopeResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationListReferences:
		var out engineprotocol.ListReferencesResult
		if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodeListReferencesResponseEnvelope(engineprotocol.ListReferencesResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationReadReferences:
		var out engineprotocol.ReadReferencesResult
		if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodeReadReferencesResponseEnvelope(engineprotocol.ReadReferencesResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationPreviewSourcePatch:
		var out engineprotocol.WorkbenchPreviewResult
		if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodePreviewSourcePatchResponseEnvelope(engineprotocol.PreviewSourcePatchResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationPreviewFragment:
		var out engineprotocol.WorkbenchPreviewResult
		if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodePreviewFragmentResponseEnvelope(engineprotocol.PreviewFragmentResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationPreviewOperations:
		var out engineprotocol.WorkbenchPreviewResult
		if typed, ok := payload.(engineprotocol.WorkbenchPreviewResult); ok && outcome == protocolcommon.OutcomeSuccess {
			out = typed
		} else if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodePreviewOperationsResponseEnvelope(engineprotocol.PreviewOperationsResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationFormatScope:
		var out engineprotocol.WorkbenchPreviewResult
		if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodeFormatScopeResponseEnvelope(engineprotocol.FormatScopeResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationOrganizeWorkspace:
		var out engineprotocol.WorkbenchPreviewResult
		if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodeOrganizeWorkspaceResponseEnvelope(engineprotocol.OrganizeWorkspaceResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	case OperationApplyToHandle:
		var out engineprotocol.ApplyToHandleResult
		if err := mapPayload(&out); err != nil {
			return nil, err
		}
		return engineprotocol.EncodeApplyToHandleResponseEnvelope(engineprotocol.ApplyToHandleResponseEnvelope{Diagnostics: diagnostics, EngineRelease: release, Failure: optionalFailure(hasFailure, failure), Outcome: outcome, Payload: optionalPayload(outcome, out), Protocol: bootstrapProtocolRef(), RequestID: requestID})
	default:
		return nil, fmt.Errorf("unsupported workbench response %q", operation)
	}
}

func optionalPayload[T any](outcome protocolcommon.Outcome, payload T) *T {
	if outcome != protocolcommon.OutcomeSuccess {
		return nil
	}
	return &payload
}

func optionalFailure(has bool, failure engineprotocol.WorkbenchFailure) *engineprotocol.WorkbenchFailure {
	if !has {
		return nil
	}
	return &failure
}

func workbenchFailure(category protocolcommon.ProtocolFailureCategory, code, workbenchCategory, message string, retryable bool, details protocolcommon.JsonObject) engineprotocol.WorkbenchFailure {
	var safeDetails *protocolcommon.JsonObject
	if details != nil {
		safeDetails = &details
	}
	return engineprotocol.WorkbenchFailure{
		Category: category, Code: code, SafeDetails: safeDetails, Message: message,
		Retryable: retryable, WorkbenchCategory: engineprotocol.WorkbenchFailureCategory(workbenchCategory),
	}
}

func mapWorkbenchFailureCategory(err *engine.WorkbenchError) (protocolcommon.ProtocolFailureCategory, string) {
	switch err.Category {
	case engine.WorkbenchErrorCancelled:
		return protocolcommon.ProtocolFailureCategoryCancelled, "cancelled"
	case engine.WorkbenchErrorCursorInvalid:
		return protocolcommon.ProtocolFailureCategoryInvariant, "execution_failed"
	case engine.WorkbenchErrorGenerationStale:
		return protocolcommon.ProtocolFailureCategoryInvariant, "execution_failed"
	case engine.WorkbenchErrorHandleInvalid:
		return protocolcommon.ProtocolFailureCategoryInvariant, "execution_failed"
	case engine.WorkbenchErrorInputInvalid:
		return protocolcommon.ProtocolFailureCategoryInvariant, "execution_failed"
	case engine.WorkbenchErrorInvariant:
		return protocolcommon.ProtocolFailureCategoryInvariant, "execution_failed"
	case engine.WorkbenchErrorLimitExceeded:
		return protocolcommon.ProtocolFailureCategoryResource, "limit_exceeded"
	case engine.WorkbenchErrorNotFound:
		return protocolcommon.ProtocolFailureCategoryInvariant, "execution_failed"
	case engine.WorkbenchErrorOperationDisabled:
		return protocolcommon.ProtocolFailureCategoryInvariant, "execution_failed"
	default:
		return protocolcommon.ProtocolFailureCategoryIo, "execution_failed"
	}
}

func workbenchDiagnostic(err *engine.WorkbenchError) []semantic.Diagnostic {
	return []semantic.Diagnostic{{
		Arguments: map[string]semantic.DiagnosticArgumentValue{},
		Code:      "LDL1801", MessageKey: strings.ReplaceAll(strings.TrimPrefix(err.Code, "engine."), ".", "_"),
		ProtocolVersion: 1, Related: []semantic.DiagnosticRelated{}, Severity: semantic.DiagnosticSeverityError,
	}}
}

func enumerateWorkbenchBlobUses(payload any) ([]blobUse, *protocolcommon.ProtocolFailure) {
	refs := []protocolcommon.BlobRef{}
	collectBlobRefs(reflect.ValueOf(payload), &refs)
	uses := make([]blobUse, 0, len(refs))
	for _, ref := range refs {
		uses = append(uses, blobUse{ref: ref})
	}
	return uses, nil
}

func collectBlobRefs(value reflect.Value, refs *[]protocolcommon.BlobRef) {
	if !value.IsValid() {
		return
	}
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}
	if value.Type() == reflect.TypeOf(protocolcommon.BlobRef{}) {
		*refs = append(*refs, value.Interface().(protocolcommon.BlobRef))
		return
	}
	switch value.Kind() {
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			collectBlobRefs(value.Field(i), refs)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			collectBlobRefs(value.Index(i), refs)
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			collectBlobRefs(value.MapIndex(key), refs)
		}
	}
}

func collectOutputBlobs(value any) []OutputBlob {
	if applied, ok := value.(engine.ApplyToHandleResult); ok {
		return sourcePlanOutputBlobs(engine.SourcePlannerPlan{
			Preview:     sourceplanner.WorkbenchPreviewResult{SourceDiff: applied.SourceDiff},
			Attachments: applied.Attachments,
		})
	}
	blobs := []OutputBlob{}
	collectOutputBlobsValue(reflect.ValueOf(value), &blobs)
	return blobs
}

func collectOutputBlobsValue(value reflect.Value, blobs *[]OutputBlob) {
	if !value.IsValid() {
		return
	}
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}
	if value.Type() == reflect.TypeOf(engine.BoundedTextChunk{}) {
		chunk := value.Interface().(engine.BoundedTextChunk)
		*blobs = append(*blobs, OutputBlob{Ref: protocolcommon.BlobRef{
			BlobID: chunk.Blob.BlobID, Digest: protocolcommon.Digest(chunk.Blob.Digest),
			Lifetime: protocolcommon.BlobLifetime(chunk.Blob.Lifetime), MediaType: chunk.Blob.MediaType,
			Size: protocolcommon.CanonicalUint64(strconv.FormatInt(chunk.Blob.Size, 10)),
		}, Bytes: append([]byte(nil), chunk.Bytes...)})
		return
	}
	switch value.Kind() {
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			collectOutputBlobsValue(value.Field(i), blobs)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			collectOutputBlobsValue(value.Index(i), blobs)
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			collectOutputBlobsValue(value.MapIndex(key), blobs)
		}
	}
}

func sourcePlanOutputBlobs(plan engine.SourcePlannerPlan) []OutputBlob {
	refs := map[string]protocolcommon.BlobRef{}
	collectBlobLikeRefs(reflect.ValueOf(plan.Preview), refs)
	result := make([]OutputBlob, 0, len(plan.Attachments))
	for id, bytes := range plan.Attachments {
		ref, ok := refs[id]
		if !ok {
			continue
		}
		result = append(result, OutputBlob{Ref: ref, Bytes: append([]byte(nil), bytes...)})
	}
	return result
}

func collectBlobLikeRefs(value reflect.Value, refs map[string]protocolcommon.BlobRef) {
	if !value.IsValid() {
		return
	}
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}
	if value.Kind() == reflect.Struct {
		fields := sourceFields(value)
		blobID, hasBlobID := stringFromField(fields["blob_id"])
		digest, hasDigest := stringFromField(fields["digest"])
		lifetime, hasLifetime := stringFromField(fields["lifetime"])
		mediaType, hasMediaType := stringFromField(fields["media_type"])
		size, hasSize := uintStringFromField(fields["size"])
		if hasBlobID && hasDigest && hasLifetime && hasMediaType && hasSize {
			refs[blobID] = protocolcommon.BlobRef{
				BlobID: blobID, Digest: protocolcommon.Digest(digest),
				Lifetime: protocolcommon.BlobLifetime(lifetime), MediaType: mediaType,
				Size: protocolcommon.CanonicalUint64(size),
			}
		}
		for i := 0; i < value.NumField(); i++ {
			collectBlobLikeRefs(value.Field(i), refs)
		}
		return
	}
	switch value.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			collectBlobLikeRefs(value.Index(i), refs)
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			collectBlobLikeRefs(value.MapIndex(key), refs)
		}
	}
}

func stringFromField(value reflect.Value) (string, bool) {
	if !value.IsValid() {
		return "", false
	}
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return "", false
		}
		value = value.Elem()
	}
	if value.Kind() == reflect.String {
		return value.String(), true
	}
	return "", false
}

func uintStringFromField(value reflect.Value) (string, bool) {
	if text, ok := stringFromField(value); ok {
		return text, true
	}
	if !value.IsValid() {
		return "", false
	}
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return "", false
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Uint, reflect.Uint64, reflect.Uint32:
		return strconv.FormatUint(value.Uint(), 10), true
	case reflect.Int, reflect.Int64, reflect.Int32:
		return strconv.FormatInt(value.Int(), 10), true
	}
	return "", false
}

func convertStruct(input any, output any) error {
	return convertValue(reflect.ValueOf(input), reflect.ValueOf(output))
}

func convertValue(src, dst reflect.Value) error {
	if !dst.IsValid() {
		return nil
	}
	if dst.Kind() != reflect.Pointer || dst.IsNil() {
		return fmt.Errorf("destination must be pointer")
	}
	return assignValue(src, dst.Elem())
}

func assignValue(src, dst reflect.Value) error {
	if !src.IsValid() {
		return nil
	}
	for src.Kind() == reflect.Pointer || src.Kind() == reflect.Interface {
		if src.IsNil() {
			return nil
		}
		src = src.Elem()
	}
	if dst.Kind() == reflect.Pointer {
		if isZeroValue(src) {
			return nil
		}
		value := reflect.New(dst.Type().Elem())
		if err := assignValue(src, value.Elem()); err != nil {
			return err
		}
		dst.Set(value)
		return nil
	}
	if dst.Kind() == reflect.String {
		switch src.Kind() {
		case reflect.String:
			dst.SetString(src.String())
			return nil
		case reflect.Int, reflect.Int64, reflect.Int32:
			dst.SetString(strconv.FormatInt(src.Int(), 10))
			return nil
		case reflect.Uint, reflect.Uint64, reflect.Uint32:
			dst.SetString(strconv.FormatUint(src.Uint(), 10))
			return nil
		}
	}
	if (dst.Kind() == reflect.Int || dst.Kind() == reflect.Int64 || dst.Kind() == reflect.Int32) && src.Kind() == reflect.String {
		value, err := strconv.ParseInt(src.String(), 10, 64)
		if err != nil {
			return err
		}
		dst.SetInt(value)
		return nil
	}
	if (dst.Kind() == reflect.Uint || dst.Kind() == reflect.Uint64 || dst.Kind() == reflect.Uint32) && src.Kind() == reflect.String {
		value, err := strconv.ParseUint(src.String(), 10, 64)
		if err != nil {
			return err
		}
		dst.SetUint(value)
		return nil
	}
	if dst.Kind() == reflect.Bool && src.Kind() == reflect.Bool {
		dst.SetBool(src.Bool())
		return nil
	}
	if dst.Kind() == reflect.Slice && (src.Kind() == reflect.Slice || src.Kind() == reflect.Array) {
		out := reflect.MakeSlice(dst.Type(), src.Len(), src.Len())
		for i := 0; i < src.Len(); i++ {
			if err := assignValue(src.Index(i), out.Index(i)); err != nil {
				return err
			}
		}
		dst.Set(out)
		return nil
	}
	if dst.Kind() == reflect.Map && src.Kind() == reflect.Map {
		out := reflect.MakeMapWithSize(dst.Type(), src.Len())
		for _, key := range src.MapKeys() {
			dstKey := reflect.New(dst.Type().Key()).Elem()
			if err := assignValue(key, dstKey); err != nil {
				return err
			}
			dstValue := reflect.New(dst.Type().Elem()).Elem()
			if err := assignValue(src.MapIndex(key), dstValue); err != nil {
				return err
			}
			out.SetMapIndex(dstKey, dstValue)
		}
		dst.Set(out)
		return nil
	}
	if dst.Type() == reflect.TypeOf(semantic.Diagnostic{}) && src.Type() == reflect.TypeOf(engine.Diagnostic{}) {
		mapped, err := mapDiagnostic(src.Interface().(engine.Diagnostic))
		if err != nil {
			return err
		}
		dst.Set(reflect.ValueOf(mapped))
		return nil
	}
	if dst.Type() == reflect.TypeOf(engineprotocol.DocumentGeneration{}) && src.Kind() == reflect.Struct {
		generation, ok := documentGenerationFromPlanner(src)
		if ok {
			dst.Set(reflect.ValueOf(generation))
			return nil
		}
	}
	if dst.Type() == reflect.TypeOf(engineprotocol.PreviewID{}) && src.Kind() == reflect.Struct {
		previewID, ok := previewIDFromPlanner(src)
		if ok {
			dst.Set(reflect.ValueOf(previewID))
			return nil
		}
	}
	if src.Type() == reflect.TypeOf(engineprotocol.DocumentGeneration{}) && dst.Kind() == reflect.Struct {
		if setPlannerGeneration(src.Interface().(engineprotocol.DocumentGeneration), dst) {
			return nil
		}
	}
	if src.Kind() == reflect.Struct && dst.Kind() == reflect.Struct {
		if setPlannerPreviewID(src, dst) {
			return nil
		}
	}
	if dst.Type() == reflect.TypeOf(engineprotocol.SemanticOperationValue{}) && src.Kind() == reflect.Struct {
		value, err := semanticOperationValueFromScalar(src)
		if err != nil {
			return err
		}
		dst.Set(reflect.ValueOf(value))
		return nil
	}
	if dst.Kind() == reflect.Struct && src.Kind() == reflect.Struct {
		fields := sourceFields(src)
		dstType := dst.Type()
		for i := 0; i < dst.NumField(); i++ {
			field := dst.Field(i)
			if !field.CanSet() {
				continue
			}
			name := jsonFieldName(dstType.Field(i))
			if name == "" || name == "-" {
				name = structFieldName(dstType.Field(i).Name)
			}
			source, ok := fields[name]
			if !ok && name == "namespace" {
				source, ok = fields["endpoint_instance_id"]
			}
			if ok {
				if err := assignValue(source, field); err != nil {
					return fmt.Errorf("%s: %w", dstType.Field(i).Name, err)
				}
			}
		}
		return nil
	}
	if src.Type().AssignableTo(dst.Type()) {
		dst.Set(src)
		return nil
	}
	if src.Type().ConvertibleTo(dst.Type()) {
		dst.Set(src.Convert(dst.Type()))
		return nil
	}
	return fmt.Errorf("cannot convert %s to %s", src.Type(), dst.Type())
}

func copyDocumentGenerationFromPreconditions(payload any, output any) error {
	source := reflect.ValueOf(payload)
	target := reflect.ValueOf(output)
	if !source.IsValid() || !target.IsValid() || target.Kind() != reflect.Pointer || target.IsNil() {
		return nil
	}
	for source.Kind() == reflect.Pointer || source.Kind() == reflect.Interface {
		if source.IsNil() {
			return nil
		}
		source = source.Elem()
	}
	if source.Kind() != reflect.Struct {
		return nil
	}
	sourcePreconditions := sourceFields(source)["preconditions"]
	if !sourcePreconditions.IsValid() {
		return nil
	}
	for sourcePreconditions.Kind() == reflect.Pointer || sourcePreconditions.Kind() == reflect.Interface {
		if sourcePreconditions.IsNil() {
			return nil
		}
		sourcePreconditions = sourcePreconditions.Elem()
	}
	generation := sourceFields(sourcePreconditions)["document_generation"]
	if !generation.IsValid() {
		return nil
	}
	target = target.Elem()
	if target.Kind() != reflect.Struct {
		return nil
	}
	field := target.FieldByName("DocumentGeneration")
	if !field.IsValid() || !field.CanSet() {
		return nil
	}
	return assignValue(generation, field)
}

func documentGenerationFromPlanner(src reflect.Value) (engineprotocol.DocumentGeneration, bool) {
	fields := sourceFields(src)
	namespace, hasNamespace := stringFromField(fields["namespace"])
	documentID, hasDocumentID := stringFromField(fields["document_id"])
	value, hasValue := uintStringFromField(fields["value"])
	if !hasNamespace || !hasDocumentID || !hasValue {
		return engineprotocol.DocumentGeneration{}, false
	}
	return engineprotocol.DocumentGeneration{
		DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: protocolcommon.EndpointInstanceID(namespace), Value: documentID},
		Value:          protocolcommon.CanonicalUint64(value),
	}, true
}

func previewIDFromPlanner(src reflect.Value) (engineprotocol.PreviewID, bool) {
	fields := sourceFields(src)
	namespace, hasNamespace := stringFromField(fields["namespace"])
	value, hasValue := stringFromField(fields["value"])
	if !hasNamespace || !hasValue {
		return engineprotocol.PreviewID{}, false
	}
	return engineprotocol.PreviewID{EndpointInstanceID: protocolcommon.EndpointInstanceID(namespace), Value: value}, true
}

func setPlannerGeneration(source engineprotocol.DocumentGeneration, target reflect.Value) bool {
	fields := sourceFields(target)
	namespace := fields["namespace"]
	documentID := fields["document_id"]
	value := fields["value"]
	if !namespace.IsValid() || !namespace.CanSet() || !documentID.IsValid() || !documentID.CanSet() || !value.IsValid() || !value.CanSet() {
		return false
	}
	namespace.SetString(string(source.DocumentHandle.EndpointInstanceID))
	documentID.SetString(source.DocumentHandle.Value)
	parsed, err := strconv.ParseUint(string(source.Value), 10, 64)
	if err != nil {
		return false
	}
	value.SetUint(parsed)
	return true
}

func setPlannerPreviewID(source, target reflect.Value) bool {
	srcFields := sourceFields(source)
	endpoint := srcFields["endpoint_instance_id"]
	sourceValue := srcFields["value"]
	targetFields := sourceFields(target)
	namespace := targetFields["namespace"]
	value := targetFields["value"]
	if !namespace.IsValid() || !namespace.CanSet() || !value.IsValid() || !value.CanSet() {
		return false
	}
	endpointValue, hasEndpoint := stringFromField(endpoint)
	previewValue, hasValue := stringFromField(sourceValue)
	if !hasEndpoint || !hasValue {
		return false
	}
	namespace.SetString(endpointValue)
	value.SetString(previewValue)
	return true
}

func semanticOperationValueFromScalar(src reflect.Value) (engineprotocol.SemanticOperationValue, error) {
	fields := sourceFields(src)
	kind, ok := stringFromField(fields["type"])
	if !ok || kind == "" {
		return engineprotocol.SemanticOperationValue{}, fmt.Errorf("missing scalar type")
	}
	switch kind {
	case "string", "enum", "date", "datetime":
		value, _ := stringFromField(fields["string"])
		return engineprotocol.SemanticOperationValue{Kind: engineprotocol.SemanticOperationValueKindString, String: &value}, nil
	case "integer":
		value, ok := intStringFromField(fields["int"])
		if !ok {
			return engineprotocol.SemanticOperationValue{}, fmt.Errorf("missing integer scalar value")
		}
		integer := protocolcommon.CanonicalSafeInteger(value)
		return engineprotocol.SemanticOperationValue{Kind: engineprotocol.SemanticOperationValueKindInteger, Integer: &integer}, nil
	case "number":
		value, ok := floatStringFromField(fields["float"])
		if !ok {
			return engineprotocol.SemanticOperationValue{}, fmt.Errorf("missing decimal scalar value")
		}
		decimal := semantic.CanonicalFiniteDecimal(value)
		return engineprotocol.SemanticOperationValue{Kind: engineprotocol.SemanticOperationValueKindDecimal, Decimal: &decimal}, nil
	case "boolean":
		value, ok := boolFromField(fields["bool"])
		if !ok {
			return engineprotocol.SemanticOperationValue{}, fmt.Errorf("missing boolean scalar value")
		}
		return engineprotocol.SemanticOperationValue{Kind: engineprotocol.SemanticOperationValueKindBoolean, Boolean: &value}, nil
	default:
		return engineprotocol.SemanticOperationValue{}, fmt.Errorf("unsupported scalar type %q", kind)
	}
}

func intStringFromField(value reflect.Value) (string, bool) {
	if !value.IsValid() {
		return "", false
	}
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return "", false
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Int, reflect.Int64, reflect.Int32:
		return strconv.FormatInt(value.Int(), 10), true
	case reflect.Uint, reflect.Uint64, reflect.Uint32:
		return strconv.FormatUint(value.Uint(), 10), true
	case reflect.String:
		return value.String(), true
	default:
		return "", false
	}
}

func floatStringFromField(value reflect.Value) (string, bool) {
	if !value.IsValid() {
		return "", false
	}
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return "", false
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(value.Float(), 'g', -1, 64), true
	case reflect.String:
		return value.String(), true
	default:
		return "", false
	}
}

func boolFromField(value reflect.Value) (bool, bool) {
	if !value.IsValid() {
		return false, false
	}
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return false, false
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Bool {
		return false, false
	}
	return value.Bool(), true
}

func isZeroValue(value reflect.Value) bool {
	if !value.IsValid() {
		return true
	}
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return true
		}
		value = value.Elem()
	}
	return value.IsZero()
}

func sourceFields(src reflect.Value) map[string]reflect.Value {
	fields := map[string]reflect.Value{}
	srcType := src.Type()
	for i := 0; i < src.NumField(); i++ {
		name := jsonFieldName(srcType.Field(i))
		if name == "" || name == "-" {
			name = structFieldName(srcType.Field(i).Name)
		}
		fields[name] = src.Field(i)
	}
	return fields
}

func jsonFieldName(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "" {
		return ""
	}
	return strings.Split(tag, ",")[0]
}

func structFieldName(name string) string {
	var out strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			out.WriteByte('_')
		}
		out.WriteRune(r)
	}
	return strings.ToLower(out.String())
}
