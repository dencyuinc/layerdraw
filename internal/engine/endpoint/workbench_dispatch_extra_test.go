// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/sourceplanner"
)

type fakeWorkbenchDriver struct {
	descriptor engine.Descriptor
	plan       sourceplanner.SourcePlan
	exportPlan engine.ExportPlan
	generation engine.DocumentGeneration
	queryState string
	err        error
}

func newFakeWorkbenchDriver() *fakeWorkbenchDriver {
	return &fakeWorkbenchDriver{
		descriptor: engine.New(engine.BuildInfo{}).Describe(),
		plan:       fakeSourcePlan([]byte("replacement")),
	}
}

func (driver *fakeWorkbenchDriver) Describe() engine.Descriptor { return driver.descriptor }

func (driver *fakeWorkbenchDriver) CompileSnapshot(context.Context, engine.CompileInput) (engine.Snapshot, error) {
	return engine.Snapshot{}, nil
}

func (driver *fakeWorkbenchDriver) OpenDocument(context.Context, engine.OpenDocumentInput) (engine.OpenDocumentResult, error) {
	return engine.OpenDocumentResult{}, nil
}

func (driver *fakeWorkbenchDriver) ReplaceSourceTree(context.Context, engine.ReplaceSourceTreeInput) (engine.ReplaceSourceTreeResult, error) {
	return engine.ReplaceSourceTreeResult{}, nil
}

func (driver *fakeWorkbenchDriver) CloseDocument(context.Context, engine.CloseDocumentInput) (engine.CloseDocumentResult, error) {
	return engine.CloseDocumentResult{}, nil
}

func (driver *fakeWorkbenchDriver) ListModules(_ context.Context, input engine.ListModulesInput) (engine.ListModulesResult, error) {
	if driver.err != nil {
		return engine.ListModulesResult{}, driver.err
	}
	return engine.ListModulesResult{
		DocumentGeneration: input.DocumentGeneration,
		Items:              []engine.ModuleReadItem{},
		Page:               engine.PageInfo{ReturnedItems: 0, Truncation: engine.TruncationComplete},
	}, nil
}

func (driver *fakeWorkbenchDriver) ReadModules(context.Context, engine.ReadModulesInput) (engine.ReadModulesResult, error) {
	return engine.ReadModulesResult{}, nil
}

func (driver *fakeWorkbenchDriver) FindSymbols(context.Context, engine.FindSymbolsInput) (engine.FindSymbolsResult, error) {
	return engine.FindSymbolsResult{}, nil
}

func (driver *fakeWorkbenchDriver) InspectSubgraph(context.Context, engine.InspectSubgraphInput) (engine.InspectSubgraphResult, error) {
	return engine.InspectSubgraphResult{}, nil
}

func (driver *fakeWorkbenchDriver) ReadDeclarations(context.Context, engine.ReadDeclarationsInput) (engine.ReadDeclarationsResult, error) {
	return engine.ReadDeclarationsResult{}, nil
}

func (driver *fakeWorkbenchDriver) ReadRows(context.Context, engine.ReadRowsInput) (engine.ReadRowsResult, error) {
	return engine.ReadRowsResult{}, nil
}

func (driver *fakeWorkbenchDriver) ExecuteDocumentQuery(ctx context.Context, input engine.ExecuteDocumentQueryInput) (engine.ExecuteDocumentQueryResult, error) {
	if driver.err != nil {
		return engine.ExecuteDocumentQueryResult{}, driver.err
	}
	result := engine.ExecuteDocumentQueryResult{
		DocumentGeneration: input.DocumentGeneration,
		Result: engine.QueryResult{
			Arguments:    input.Arguments,
			QueryAddress: input.QueryAddress,
			StateInput:   engine.QueryStateInputRef{Kind: "none"},
			StatePolicy:  "none",
		},
		ReturnedItems: 0,
	}
	if driver.queryState != "" {
		result.Result.StatePolicy = driver.queryState
	}
	returnedBytes, err := engine.MeasureDocumentQueryLogicalBytes(ctx, result, input.Limits.MaxOutputBytes)
	if err != nil {
		return engine.ExecuteDocumentQueryResult{}, err
	}
	result.ReturnedBytes = returnedBytes
	return result, nil
}

func (driver *fakeWorkbenchDriver) MaterializeDocumentView(_ context.Context, input engine.MaterializeDocumentViewInput) (engine.MaterializeDocumentViewResult, error) {
	if driver.err != nil {
		return engine.MaterializeDocumentViewResult{}, driver.err
	}
	if input.Query == nil {
		return engine.MaterializeDocumentViewResult{}, errors.New("fake driver requires query View input")
	}
	queryAddress := input.Query.QueryResult.QueryAddress
	base := engine.ViewDataBase{
		Kind: engine.ViewDataContext, Category: "context", ProjectAddress: "ldl:project:p", ViewAddress: input.ViewAddress,
		Shape:        view.Shape{Kind: view.ShapeContext, Context: &view.ContextShape{GroupBy: view.ContextGroupNone, Incoming: true, Outgoing: true}},
		QueryAddress: &queryAddress, Revision: engine.ViewRevision{Single: &engine.SingleRevision{Kind: "single", RevisionID: "revision-1", DefinitionHash: "sha256:" + strings.Repeat("a", 64)}},
		StatePolicy: "none", StateInput: engine.QueryStateInputRef{Kind: "none"},
		Source: engine.ViewDataSourceRefs{
			SubjectAddresses: []string{"ldl:project:p", queryAddress, input.ViewAddress}, EntityAddresses: []string{}, RelationAddresses: []string{},
			LayerAddresses: []string{}, RowAddresses: []string{}, CellRefs: []engine.ViewDataCellRef{}, AssetDigests: []string{},
			State: engine.ViewDataStateRefs{Reads: []engine.StateReadRef{}},
		}, Diagnostics: []engine.Diagnostic{},
	}
	return engine.MaterializeDocumentViewResult{
		DocumentGeneration: input.Query.DocumentGeneration,
		ViewData:           engine.ViewData{Context: &engine.ContextViewData{ViewDataBase: base, Groups: []engine.ContextGroup{}}},
	}, nil
}

func (driver *fakeWorkbenchDriver) PlanExport(context.Context, engine.ExportPlanInput) (engine.ExportPlan, error) {
	return driver.exportPlan, driver.err
}

func (driver *fakeWorkbenchDriver) GetNeighbors(context.Context, engine.GetNeighborsInput) (engine.GetNeighborsResult, error) {
	return engine.GetNeighborsResult{}, nil
}

func (driver *fakeWorkbenchDriver) FindUsages(context.Context, engine.FindUsagesInput) (engine.FindUsagesResult, error) {
	return engine.FindUsagesResult{}, nil
}

func (driver *fakeWorkbenchDriver) ReadScope(context.Context, engine.ReadScopeInput) (engine.ReadScopeResult, error) {
	return engine.ReadScopeResult{}, nil
}

func (driver *fakeWorkbenchDriver) ListReferences(context.Context, engine.ListReferencesInput) (engine.ListReferencesResult, error) {
	return engine.ListReferencesResult{}, nil
}

func (driver *fakeWorkbenchDriver) ReadReferences(context.Context, engine.ReadReferencesInput) (engine.ReadReferencesResult, error) {
	return engine.ReadReferencesResult{}, nil
}

func (driver *fakeWorkbenchDriver) PreviewSourcePatch(_ context.Context, input engine.PreviewSourcePatchInput) (engine.SourcePlannerPlan, error) {
	driver.generation = input.DocumentGeneration
	return driver.plan, nil
}

func (driver *fakeWorkbenchDriver) PreviewFragment(_ context.Context, input engine.PreviewFragmentInput) (engine.SourcePlannerPlan, error) {
	driver.generation = input.DocumentGeneration
	return driver.plan, nil
}

func (driver *fakeWorkbenchDriver) FormatScope(_ context.Context, input engine.FormatScopeInput) (engine.SourcePlannerPlan, error) {
	driver.generation = input.DocumentGeneration
	return driver.plan, nil
}

func (driver *fakeWorkbenchDriver) OrganizeWorkspace(_ context.Context, input engine.OrganizeWorkspaceInput) (engine.SourcePlannerPlan, error) {
	driver.generation = input.DocumentGeneration
	return driver.plan, nil
}

func (driver *fakeWorkbenchDriver) ApplyToHandle(_ context.Context, input engine.ApplyToHandleInput) (engine.ApplyToHandleResult, error) {
	driver.generation = input.BaseGeneration
	preview := driver.plan.Preview
	return engine.ApplyToHandleResult{
		AuthoringImpact:    *preview.AuthoringImpact,
		DocumentGeneration: input.BaseGeneration,
		PreviewDigest:      input.PreviewDigest,
		ResultingHashes:    *preview.ResultingHashes,
		SourceDiff:         preview.SourceDiff,
	}, driver.err
}

func TestWorkbenchSourcePlanningDispatchesGeneratedOperations(t *testing.T) {
	driver := newFakeWorkbenchDriver()
	dispatcher := newCompileDispatcher(driver)
	negotiated := compileContext(t)
	limits := engineprotocol.WorkbenchLimits{MaxItems: "10", MaxOutputBytes: "100000"}
	generation := engineprotocol.DocumentGeneration{
		DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "engine-test", Value: "document_1234567890abcdef"},
		Value:          "7",
	}
	preconditions := engineprotocol.EngineEditPreconditions{
		DocumentGeneration:    generation,
		ExpectedChildSets:     []engineprotocol.ExpectedChildSet{},
		ExpectedSubjectHashes: []engineprotocol.ExpectedHash{},
		ExpectedSubtreeHashes: []engineprotocol.ExpectedHash{},
	}
	fragment := []byte("entity_type db \"DB\" {}\n")
	fragmentRef := testBlobRef("fragment", "text/plain; charset=utf-8", fragment)

	tests := []struct {
		name      string
		operation string
		control   []byte
		decode    func([]byte) (*engineprotocol.WorkbenchPreviewResult, error)
		source    BlobSource
	}{
		{
			name:      "preview source patch",
			operation: OperationPreviewSourcePatch,
			control: encodePreviewSourcePatchForTest(t, engineprotocol.PreviewSourcePatchRequestEnvelope{
				Operation: engineprotocol.PreviewSourcePatchRequestEnvelopeOperationValue,
				Payload: engineprotocol.PreviewSourcePatchInput{
					Limits: limits, Preconditions: preconditions,
					Patch: engineprotocol.SourcePatchBatch{Patches: []engineprotocol.SourcePatchInput{{
						ExpectedSourceDigest: sourceDigest([]byte("before")),
						ReplacementBlob:      fragmentRef,
						SourceRange:          semantic.SourceRange{ModulePath: "document.ldl", Origin: semantic.SourceOrigin{Kind: "project"}, StartByte: "0", EndByte: "0"},
					}}},
				},
				Protocol: bootstrapProtocolRef(), RequestID: "preview-source-patch",
			}),
			decode: func(control []byte) (*engineprotocol.WorkbenchPreviewResult, error) {
				response, err := engineprotocol.DecodePreviewSourcePatchResponseEnvelope(control)
				return response.Payload, err
			},
			source: sourceFor(fragmentRef, fragment),
		},
		{
			name:      "preview fragment",
			operation: OperationPreviewFragment,
			control: encodePreviewFragmentForTest(t, engineprotocol.PreviewFragmentRequestEnvelope{
				Operation: engineprotocol.PreviewFragmentRequestEnvelopeOperationValue,
				Payload: engineprotocol.PreviewFragmentInput{
					Limits: limits, Preconditions: preconditions,
					Fragment: engineprotocol.FragmentInput{
						AllowedKinds:   []semantic.SubjectKind{"entity_type"},
						FragmentBlob:   fragmentRef,
						InsertionOwner: "ldl:project:p",
						Intent:         "insert",
					},
				},
				Protocol: bootstrapProtocolRef(), RequestID: "preview-fragment",
			}),
			decode: func(control []byte) (*engineprotocol.WorkbenchPreviewResult, error) {
				response, err := engineprotocol.DecodePreviewFragmentResponseEnvelope(control)
				return response.Payload, err
			},
			source: sourceFor(fragmentRef, fragment),
		},
		{
			name:      "format scope",
			operation: OperationFormatScope,
			control: encodeFormatScopeForTest(t, engineprotocol.FormatScopeRequestEnvelope{
				Operation: engineprotocol.FormatScopeRequestEnvelopeOperationValue,
				Payload: engineprotocol.FormatScopeInput{
					Limits: limits, Preconditions: preconditions, ScopeAddresses: []semantic.StableAddress{"ldl:project:p:entity-type:service"},
				},
				Protocol: bootstrapProtocolRef(), RequestID: "format-scope",
			}),
			decode: func(control []byte) (*engineprotocol.WorkbenchPreviewResult, error) {
				response, err := engineprotocol.DecodeFormatScopeResponseEnvelope(control)
				return response.Payload, err
			},
			source: &memoryBlobSource{},
		},
		{
			name:      "organize workspace",
			operation: OperationOrganizeWorkspace,
			control: encodeOrganizeWorkspaceForTest(t, engineprotocol.OrganizeWorkspaceRequestEnvelope{
				Operation: engineprotocol.OrganizeWorkspaceRequestEnvelopeOperationValue,
				Payload: engineprotocol.OrganizeWorkspaceInput{
					Limits: limits, Preconditions: preconditions, Strategy: "standard_layout",
				},
				Protocol: bootstrapProtocolRef(), RequestID: "organize-workspace",
			}),
			decode: func(control []byte) (*engineprotocol.WorkbenchPreviewResult, error) {
				response, err := engineprotocol.DecodeOrganizeWorkspaceResponseEnvelope(control)
				return response.Payload, err
			},
			source: &memoryBlobSource{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, terminal, err := dispatcher.PrepareDispatch(context.Background(), negotiated, test.operation, test.control)
			if err != nil || terminal != nil || plan == nil {
				t.Fatalf("prepare plan=%v terminal=%+v err=%v", plan, terminal, err)
			}
			sink := &memoryBlobSink{}
			response, err := plan.ExecuteDispatch(context.Background(), test.source, sink)
			if err != nil || response.Outcome != protocolcommon.OutcomeSuccess {
				t.Fatalf("dispatch = %+v err=%v", response, err)
			}
			payload, err := test.decode(response.Control)
			if err != nil || payload == nil || payload.Status != "valid" {
				t.Fatalf("payload = %+v err=%v", payload, err)
			}
			if driver.generation.Value != 7 || driver.generation.DocumentHandle.Value != "document_1234567890abcdef" {
				t.Fatalf("document generation not mapped: %+v", driver.generation)
			}
			if len(sink.blobs) != 1 || string(sink.blobs[0].Bytes) != "replacement" {
				t.Fatalf("source plan blobs = %+v", sink.blobs)
			}
		})
	}
}

func TestWorkbenchApplyToHandleDispatchesGeneratedOperation(t *testing.T) {
	driver := newFakeWorkbenchDriver()
	dispatcher := newCompileDispatcher(driver)
	negotiated := compileContext(t)
	control := encodeApplyToHandleForTest(t, engineprotocol.ApplyToHandleRequestEnvelope{
		Operation: engineprotocol.ApplyToHandleRequestEnvelopeOperationValue,
		Payload: engineprotocol.ApplyToHandleInput{
			BaseGeneration: engineprotocol.DocumentGeneration{
				DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "engine-test", Value: "document_1234567890abcdef"},
				Value:          "7",
			},
			PreviewDigest: sourceDigest([]byte("replacement")),
			PreviewID: engineprotocol.PreviewID{
				EndpointInstanceID: "engine-test",
				Value:              "preview_1234567890abcdef",
			},
		},
		Protocol: bootstrapProtocolRef(), RequestID: "apply-to-handle",
	})
	plan, terminal, err := dispatcher.PrepareDispatch(context.Background(), negotiated, OperationApplyToHandle, control)
	if err != nil || terminal != nil || plan == nil {
		t.Fatalf("prepare plan=%v terminal=%+v err=%v", plan, terminal, err)
	}
	response, err := plan.ExecuteDispatch(context.Background(), &memoryBlobSource{}, &memoryBlobSink{})
	if err != nil || response.Outcome != protocolcommon.OutcomeSuccess {
		t.Fatalf("dispatch = %+v err=%v", response, err)
	}
	decoded, err := engineprotocol.DecodeApplyToHandleResponseEnvelope(response.Control)
	if err != nil || decoded.Payload == nil || decoded.Payload.PreviewDigest != sourceDigest([]byte("replacement")) {
		t.Fatalf("decoded = %+v err=%v", decoded, err)
	}
	if driver.generation.Value != 7 || driver.generation.DocumentHandle.Value != "document_1234567890abcdef" {
		t.Fatalf("document generation not mapped: %+v", driver.generation)
	}
}

func TestWorkbenchPlanLifecycleAndScalarConversionEdges(t *testing.T) {
	driver := newFakeWorkbenchDriver()
	dispatcher := newCompileDispatcher(driver)
	negotiated := compileContext(t)
	control := encodeListModulesForTest(t, engineprotocol.ListModulesRequestEnvelope{
		Operation: engineprotocol.ListModulesRequestEnvelopeOperationValue,
		Payload: engineprotocol.ListModulesInput{
			DocumentGeneration: engineprotocol.DocumentGeneration{
				DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "engine-test", Value: "document_1234567890abcdef"},
				Value:          "1",
			},
			Limits: engineprotocol.WorkbenchLimits{MaxItems: "1", MaxOutputBytes: "1000"},
		},
		Protocol: bootstrapProtocolRef(), RequestID: "list-for-lifecycle",
	})
	plan, terminal, err := dispatcher.PrepareDispatch(context.Background(), negotiated, OperationListModules, control)
	if err != nil || terminal != nil || plan == nil {
		t.Fatalf("prepare plan=%v terminal=%+v err=%v", plan, terminal, err)
	}
	if len(plan.BlobRequirements()) != 0 || plan.AdmissionBudget().RequiredBlobCount != 0 {
		t.Fatalf("unexpected budget requirements=%+v budget=%+v", plan.BlobRequirements(), plan.AdmissionBudget())
	}
	if _, err := plan.Execute(context.Background(), &memoryBlobSource{}, &memoryBlobSink{}); err == nil {
		t.Fatal("workbench plan accepted compile Execute")
	}
	plan.Abort()
	if _, err := plan.ExecuteDispatch(context.Background(), &memoryBlobSource{}, &memoryBlobSink{}); err == nil {
		t.Fatal("aborted plan executed")
	}

	for _, input := range []struct {
		name string
		src  any
		want engineprotocol.SemanticOperationValueKind
	}{
		{name: "integer", src: struct {
			Type string
			Int  int64
		}{Type: "integer", Int: 42}, want: engineprotocol.SemanticOperationValueKindInteger},
		{name: "decimal", src: struct {
			Type  string
			Float float64
		}{Type: "number", Float: 1.5}, want: engineprotocol.SemanticOperationValueKindDecimal},
		{name: "boolean", src: struct {
			Type string
			Bool bool
		}{Type: "boolean", Bool: true}, want: engineprotocol.SemanticOperationValueKindBoolean},
	} {
		t.Run(input.name, func(t *testing.T) {
			var out engineprotocol.SemanticOperationValue
			if err := convertStruct(input.src, &out); err != nil {
				t.Fatal(err)
			}
			if out.Kind != input.want {
				t.Fatalf("kind = %q want %q", out.Kind, input.want)
			}
		})
	}

	var plannerPreviewID struct {
		Namespace string `json:"namespace"`
		Value     string `json:"value"`
	}
	if err := convertStruct(engineprotocol.PreviewID{EndpointInstanceID: "engine-test", Value: "preview-123"}, &plannerPreviewID); err != nil {
		t.Fatal(err)
	}
	if plannerPreviewID.Namespace != "engine-test" || plannerPreviewID.Value != "preview-123" {
		t.Fatalf("preview ID mapping = %+v", plannerPreviewID)
	}

	var applyInput engine.ApplyToHandleInput
	if err := convertStruct(engineprotocol.ApplyToHandleInput{
		BaseGeneration: engineprotocol.DocumentGeneration{
			DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "engine-test", Value: "document-123"},
			Value:          "7",
		},
		PreviewDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PreviewID:     engineprotocol.PreviewID{EndpointInstanceID: "engine-test", Value: "preview-123"},
	}, &applyInput); err != nil {
		t.Fatal(err)
	}
	if applyInput.BaseGeneration.DocumentHandle.EndpointInstanceID != "engine-test" || applyInput.BaseGeneration.DocumentHandle.Value != "document-123" || applyInput.BaseGeneration.Value != 7 ||
		applyInput.PreviewDigest != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ||
		applyInput.PreviewID.Namespace != "engine-test" || applyInput.PreviewID.Value != "preview-123" {
		t.Fatalf("apply input mapping = %+v", applyInput)
	}
}

func TestWorkbenchDispatchFailureAndHelperEdges(t *testing.T) {
	driver := newFakeWorkbenchDriver()
	dispatcher := newCompileDispatcher(driver)
	negotiated := compileContext(t)
	release := negotiated.EngineRelease()
	control := encodeListModulesForTest(t, engineprotocol.ListModulesRequestEnvelope{
		Operation: engineprotocol.ListModulesRequestEnvelopeOperationValue,
		Payload: engineprotocol.ListModulesInput{
			DocumentGeneration: engineprotocol.DocumentGeneration{
				DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "engine-test", Value: "document_1234567890abcdef"},
				Value:          "1",
			},
			Limits: engineprotocol.WorkbenchLimits{MaxItems: "1", MaxOutputBytes: "1000"},
		},
		Protocol: bootstrapProtocolRef(), RequestID: "list-failure",
	})
	_, terminal, err := dispatcher.PrepareDispatch(context.Background(), nil, OperationListModules, control)
	if err != nil || terminal == nil || terminal.Outcome != "" {
		t.Fatalf("unnegotiated terminal=%+v err=%v", terminal, err)
	}
	decodedTerminal, err := engineprotocol.DecodeListModulesResponseEnvelope(terminal.Control)
	if err != nil || decodedTerminal.Outcome != protocolcommon.OutcomeFailed || decodedTerminal.Failure == nil {
		t.Fatalf("unnegotiated decoded=%+v err=%v", decodedTerminal, err)
	}
	if _, _, err := dispatcher.PrepareDispatch(context.Background(), negotiated, OperationReadModules, control); err == nil {
		t.Fatal("operation mismatch was accepted")
	}
	transport, err := dispatcher.DispatchTransportResponse(OperationFormatScope, "transport-format", release)
	if err != nil || transport.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("transport response = %+v err=%v", transport, err)
	}
	if _, err := engineprotocol.DecodeFormatScopeResponseEnvelope(transport.Control); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatcher.DispatchCancellationResponse("engine.nope", "cancel-nope", release); err == nil {
		t.Fatal("unsupported cancellation operation encoded")
	}
	if _, err := dispatcher.DispatchTransportFailureResponse("engine.nope", "transport-nope", release, CompileTransportProtocolViolation); err == nil {
		t.Fatal("unsupported transport operation encoded")
	}

	plan, terminal, err := dispatcher.PrepareDispatch(context.Background(), negotiated, OperationListModules, control)
	if err != nil || terminal != nil || plan == nil {
		t.Fatalf("prepare plan=%v terminal=%+v err=%v", plan, terminal, err)
	}
	failed, err := plan.ExecuteDispatch(context.Background(), &memoryBlobSource{}, &memoryBlobSink{err: errors.New("sink failed")})
	if err != nil || failed.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("sink failure dispatch=%+v err=%v", failed, err)
	}
	failedList, err := engineprotocol.DecodeListModulesResponseEnvelope(failed.Control)
	if err != nil || failedList.Failure == nil {
		t.Fatalf("sink failure decoded=%+v err=%v", failedList, err)
	}

	for _, category := range []engine.WorkbenchErrorCategory{
		engine.WorkbenchErrorCancelled,
		engine.WorkbenchErrorCursorInvalid,
		engine.WorkbenchErrorGenerationStale,
		engine.WorkbenchErrorHandleInvalid,
		engine.WorkbenchErrorInputInvalid,
		engine.WorkbenchErrorLimitExceeded,
		engine.WorkbenchErrorNotFound,
		engine.WorkbenchErrorOperationDisabled,
		engine.WorkbenchErrorInvariant,
	} {
		failureCategory, workbenchCategory := mapWorkbenchFailureCategory(&engine.WorkbenchError{Category: category})
		if failureCategory == "" || workbenchCategory == "" {
			t.Fatalf("empty mapping for %q", category)
		}
	}
	diagnostics := workbenchDiagnostic(&engine.WorkbenchError{Code: "engine.workbench.test"})
	if len(diagnostics) != 1 || diagnostics[0].MessageKey != "workbench_test" {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	if got := optionalFailure(false, engineprotocol.WorkbenchFailure{Code: "x"}); got != nil {
		t.Fatalf("optional failure = %+v", got)
	}
	descriptor := newTestDescriptor(t)
	if got := DispatchRelease(nil, descriptor); got != descriptor.EngineRelease() {
		t.Fatalf("fallback release = %q", got)
	}
	if _, _, err := RequestContextFromControl(context.Background(), []byte("{")); err == nil {
		t.Fatal("invalid control deadline metadata was accepted")
	}
}

func TestWorkbenchPlanTerminalAndExecutionFailureEdges(t *testing.T) {
	dispatcher := newCompileDispatcher(newFakeWorkbenchDriver())
	negotiated := compileContext(t)
	listControl := encodeListModulesForTest(t, engineprotocol.ListModulesRequestEnvelope{
		Operation: engineprotocol.ListModulesRequestEnvelopeOperationValue,
		Payload: engineprotocol.ListModulesInput{
			DocumentGeneration: engineprotocol.DocumentGeneration{
				DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "engine-test", Value: "document_1234567890abcdef"},
				Value:          "1",
			},
			Limits: engineprotocol.WorkbenchLimits{MaxItems: "1", MaxOutputBytes: "1000"},
		},
		Protocol: bootstrapProtocolRef(), RequestID: "list-terminal-edges",
	})

	cancelDriver := newFakeWorkbenchDriver()
	cancelDriver.err = context.Canceled
	cancelPlan, terminal, err := newCompileDispatcher(cancelDriver).PrepareDispatch(context.Background(), negotiated, OperationListModules, listControl)
	if err != nil || terminal != nil || cancelPlan == nil {
		t.Fatalf("cancel prepare plan=%v terminal=%+v err=%v", cancelPlan, terminal, err)
	}
	cancelled, err := cancelPlan.ExecuteDispatch(context.Background(), &memoryBlobSource{}, &memoryBlobSink{})
	if err != nil || cancelled.Outcome != protocolcommon.OutcomeCancelled {
		t.Fatalf("cancelled response=%+v err=%v", cancelled, err)
	}

	fragment := []byte("entity_type db \"DB\" {}\n")
	fragmentRef := testBlobRef("missing-fragment", "text/plain; charset=utf-8", fragment)
	previewControl := encodePreviewFragmentForTest(t, engineprotocol.PreviewFragmentRequestEnvelope{
		Operation: engineprotocol.PreviewFragmentRequestEnvelopeOperationValue,
		Payload: engineprotocol.PreviewFragmentInput{
			Limits: engineprotocol.WorkbenchLimits{MaxItems: "10", MaxOutputBytes: "100000"},
			Preconditions: engineprotocol.EngineEditPreconditions{
				DocumentGeneration: engineprotocol.DocumentGeneration{
					DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "engine-test", Value: "document_1234567890abcdef"},
					Value:          "7",
				},
				ExpectedChildSets:     []engineprotocol.ExpectedChildSet{},
				ExpectedSubjectHashes: []engineprotocol.ExpectedHash{},
				ExpectedSubtreeHashes: []engineprotocol.ExpectedHash{},
			},
			Fragment: engineprotocol.FragmentInput{
				AllowedKinds:   []semantic.SubjectKind{"entity_type"},
				FragmentBlob:   fragmentRef,
				InsertionOwner: "ldl:project:p",
				Intent:         "insert",
			},
		},
		Protocol: bootstrapProtocolRef(), RequestID: "preview-missing-blob",
	})
	blobPlan, terminal, err := dispatcher.PrepareDispatch(context.Background(), negotiated, OperationPreviewFragment, previewControl)
	if err != nil || terminal != nil || blobPlan == nil {
		t.Fatalf("blob prepare plan=%v terminal=%+v err=%v", blobPlan, terminal, err)
	}
	failed, err := blobPlan.ExecuteDispatch(context.Background(), &memoryBlobSource{}, &memoryBlobSink{})
	if err != nil || failed.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("missing blob response=%+v err=%v", failed, err)
	}
}

func TestWorkbenchOperationalLimitsBecomeFailedResources(t *testing.T) {
	driver := newFakeWorkbenchDriver()
	driver.err = &engine.WorkbenchError{Code: "engine.workbench.limit", Category: engine.WorkbenchErrorLimitExceeded, Resource: "items", Limit: 1, Observed: 2}
	dispatcher := newCompileDispatcher(driver)
	negotiated := compileContext(t)
	control := encodeListModulesForTest(t, engineprotocol.ListModulesRequestEnvelope{
		Operation: engineprotocol.ListModulesRequestEnvelopeOperationValue,
		Payload: engineprotocol.ListModulesInput{
			DocumentGeneration: engineprotocol.DocumentGeneration{
				DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "engine-test", Value: "document_1234567890abcdef"},
				Value:          "1",
			},
			Limits: engineprotocol.WorkbenchLimits{MaxItems: "1", MaxOutputBytes: "1000"},
		},
		Protocol: bootstrapProtocolRef(), RequestID: "list-rejected",
	})
	plan, terminal, err := dispatcher.PrepareDispatch(context.Background(), negotiated, OperationListModules, control)
	if err != nil || terminal != nil || plan == nil {
		t.Fatalf("prepare plan=%v terminal=%+v err=%v", plan, terminal, err)
	}
	response, err := plan.ExecuteDispatch(context.Background(), &memoryBlobSource{}, &memoryBlobSink{})
	if err != nil || response.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("dispatch = %+v err=%v", response, err)
	}
	decoded, err := engineprotocol.DecodeListModulesResponseEnvelope(response.Control)
	if err != nil || decoded.Payload != nil || len(decoded.Diagnostics) != 0 || decoded.Failure == nil ||
		decoded.Failure.Category != protocolcommon.ProtocolFailureCategoryResource || decoded.Failure.WorkbenchCategory != engineprotocol.WorkbenchFailureCategory("limit_exceeded") ||
		decoded.Failure.SafeDetails == nil || !reflect.DeepEqual((*decoded.Failure.SafeDetails)["resource"], stringJSON("items")) {
		t.Fatalf("decoded = %+v err=%v", decoded, err)
	}
}

func TestExecuteQueryResourceFailureUsesFailedEnvelope(t *testing.T) {
	driver := newFakeWorkbenchDriver()
	driver.err = &engine.WorkbenchError{
		Code:     "engine.workbench.limit",
		Category: engine.WorkbenchErrorLimitExceeded,
		Resource: "query_work",
		Limit:    100,
		Observed: 101,
	}
	dispatcher := newCompileDispatcher(driver)
	negotiated := compileContext(t)
	control, err := engineprotocol.EncodeExecuteQueryRequestEnvelope(engineprotocol.ExecuteQueryRequestEnvelope{
		Operation: engineprotocol.ExecuteQueryRequestEnvelopeOperationValue,
		Payload: engineprotocol.ExecuteQueryInput{
			Arguments: map[string]semantic.RecipeScalar{},
			DocumentGeneration: engineprotocol.DocumentGeneration{
				DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "engine-test", Value: "document_1234567890abcdef"},
				Value:          "1",
			},
			Limits:       engineprotocol.WorkbenchLimits{MaxItems: "100", MaxOutputBytes: "1000"},
			QueryAddress: "ldl:project:p:query:q",
		},
		Protocol: bootstrapProtocolRef(), RequestID: "execute-query-resource-failure",
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, terminal, err := dispatcher.PrepareDispatch(context.Background(), negotiated, OperationExecuteQuery, control)
	if err != nil || terminal != nil || plan == nil {
		t.Fatalf("prepare plan=%v terminal=%+v err=%v", plan, terminal, err)
	}
	response, err := plan.ExecuteDispatch(context.Background(), &memoryBlobSource{}, &memoryBlobSink{})
	if err != nil || response.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("dispatch = %+v err=%v", response, err)
	}
	decoded, err := engineprotocol.DecodeExecuteQueryResponseEnvelope(response.Control)
	if err != nil || decoded.Payload != nil || len(decoded.Diagnostics) != 0 || decoded.Failure == nil {
		t.Fatalf("decoded = %+v err=%v", decoded, err)
	}
	if decoded.Failure.Category != protocolcommon.ProtocolFailureCategoryResource ||
		decoded.Failure.WorkbenchCategory != engineprotocol.WorkbenchFailureCategoryLimitExceeded ||
		decoded.Failure.Code != "engine.workbench.limit" || decoded.Failure.SafeDetails == nil {
		t.Fatalf("failure = %+v", decoded.Failure)
	}
	wantDetails := protocolcommon.JsonObject{
		"limit":    stringJSON("100"),
		"observed": stringJSON("101"),
		"resource": stringJSON("query_work"),
	}
	if !reflect.DeepEqual(*decoded.Failure.SafeDetails, wantDetails) {
		t.Fatalf("safe details = %+v, want %+v", *decoded.Failure.SafeDetails, wantDetails)
	}
}

func TestExecuteQueryInvalidDriverResultUsesInvariantFailureEnvelope(t *testing.T) {
	driver := newFakeWorkbenchDriver()
	driver.queryState = "invalid"
	dispatcher := newCompileDispatcher(driver)
	negotiated := compileContext(t)
	control, err := engineprotocol.EncodeExecuteQueryRequestEnvelope(engineprotocol.ExecuteQueryRequestEnvelope{
		Operation: engineprotocol.ExecuteQueryRequestEnvelopeOperationValue,
		Payload: engineprotocol.ExecuteQueryInput{
			Arguments: map[string]semantic.RecipeScalar{},
			DocumentGeneration: engineprotocol.DocumentGeneration{
				DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "engine-test", Value: "document_1234567890abcdef"},
				Value:          "1",
			},
			Limits:       engineprotocol.WorkbenchLimits{MaxItems: "100", MaxOutputBytes: "1000"},
			QueryAddress: "ldl:project:p:query:q",
		},
		Protocol: bootstrapProtocolRef(), RequestID: "execute-query-invalid-result",
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, terminal, err := dispatcher.PrepareDispatch(context.Background(), negotiated, OperationExecuteQuery, control)
	if err != nil || terminal != nil || plan == nil {
		t.Fatalf("prepare plan=%v terminal=%+v err=%v", plan, terminal, err)
	}
	response, err := plan.ExecuteDispatch(context.Background(), &memoryBlobSource{}, &memoryBlobSink{})
	if err != nil || response.Outcome != protocolcommon.OutcomeFailed {
		t.Fatalf("dispatch = %+v err=%v", response, err)
	}
	decoded, err := engineprotocol.DecodeExecuteQueryResponseEnvelope(response.Control)
	if err != nil || decoded.Payload != nil || len(decoded.Diagnostics) != 0 || decoded.Failure == nil {
		t.Fatalf("decoded = %+v err=%v", decoded, err)
	}
	if decoded.Failure.Category != protocolcommon.ProtocolFailureCategoryInvariant ||
		decoded.Failure.WorkbenchCategory != engineprotocol.WorkbenchFailureCategoryExecutionFailed ||
		decoded.Failure.Code != "engine.workbench.result_invariant" || decoded.Failure.Retryable {
		t.Fatalf("failure = %+v", decoded.Failure)
	}
}

func TestExecuteQueryMappingCoversScalarsCyclesAndErrors(t *testing.T) {
	text := "value"
	integer := protocolcommon.CanonicalSafeInteger("42")
	number := semantic.CanonicalFiniteDecimal("1.5")
	boolean := true
	for _, test := range []struct {
		name string
		in   semantic.RecipeScalar
		want engine.TypedScalar
	}{
		{name: "string", in: semantic.RecipeScalar{Kind: "string", StringValue: &text}, want: engine.TypedScalar{Type: "string", String: "value"}},
		{name: "enum", in: semantic.RecipeScalar{Kind: "enum", StringValue: &text}, want: engine.TypedScalar{Type: "enum", String: "value"}},
		{name: "integer", in: semantic.RecipeScalar{Kind: "integer", IntegerValue: &integer}, want: engine.TypedScalar{Type: "integer", Int: 42}},
		{name: "number", in: semantic.RecipeScalar{Kind: "number", NumberValue: &number}, want: engine.TypedScalar{Type: "number", Float: 1.5}},
		{name: "boolean", in: semantic.RecipeScalar{Kind: "boolean", BooleanValue: &boolean}, want: engine.TypedScalar{Type: "boolean", Bool: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := engineScalarFromRecipeScalar(test.in)
			if err != nil || got != test.want {
				t.Fatalf("scalar = %+v err=%v", got, err)
			}
		})
	}
	for _, bad := range []semantic.RecipeScalar{
		{Kind: "string"},
		{Kind: "integer"},
		{Kind: "number"},
		{Kind: "boolean"},
		{Kind: "unknown"},
		{Kind: "integer", IntegerValue: pointer(protocolcommon.CanonicalSafeInteger("not-int"))},
		{Kind: "number", NumberValue: pointer(semantic.CanonicalFiniteDecimal("not-number"))},
	} {
		if _, err := engineScalarFromRecipeScalar(bad); err == nil {
			t.Fatalf("bad scalar accepted: %+v", bad)
		}
	}

	queryResultInput := engine.ExecuteDocumentQueryResult{
		DocumentGeneration: engine.DocumentGeneration{DocumentHandle: engine.DocumentHandle{EndpointInstanceID: "engine-test", Value: "document_1234567890abcdef"}, Value: 7},
		Result: engine.QueryResult{
			Arguments: map[string]engine.TypedScalar{
				"ldl:project:p:query:q:parameter:boolean":  {Type: "boolean", Bool: true},
				"ldl:project:p:query:q:parameter:date":     {Type: "date", String: "2026-07-17"},
				"ldl:project:p:query:q:parameter:datetime": {Type: "datetime", String: "2026-07-17T12:00:00Z"},
				"ldl:project:p:query:q:parameter:enum":     {Type: "enum", String: "prod"},
				"ldl:project:p:query:q:parameter:integer":  {Type: "integer", Int: 42},
				"ldl:project:p:query:q:parameter:number":   {Type: "number", Float: 1.5},
				"ldl:project:p:query:q:parameter:string":   {Type: "string", String: "<quoted>\n\"\\\u2028😀"},
			},
			CycleRefs: []engine.QueryCycleRef{{
				FromEntityAddress: "ldl:project:p:entity:a", Kind: "cycle", Orientation: "outgoing", RelationAddress: "ldl:project:p:relation:r",
				RetainedPath:    engine.QueryPath{EntityAddresses: []string{"ldl:project:p:entity:a", "ldl:project:p:entity:b"}, RelationAddresses: []string{"ldl:project:p:relation:r"}},
				ToEntityAddress: "ldl:project:p:entity:b",
			}},
			Diagnostics: []engine.Diagnostic{{
				Code: "LDL1605", Severity: "warning", MessageKey: "optional_query_state_missing_or_stale", Message: "state missing",
				Arguments: map[string]string{"detail": "<missing>"}, OwnerAddress: "ldl:project:p:query:q", SubjectAddress: "ldl:project:p:entity:a",
				Range: &engine.SourceRange{Origin: engine.SourceOrigin{Kind: "pack", PackAddress: "ldl:pack:aws:core"}, ModulePath: "main.ldl", StartByte: 1, EndByte: 2},
			}},
			Paths:                    []engine.QueryPath{{EntityAddresses: []string{"ldl:project:p:entity:a"}, RelationAddresses: []string{}}},
			QueryAddress:             "ldl:project:p:query:q",
			StateInput:               engine.QueryStateInputRef{Kind: "none"},
			StatePolicy:              "optional",
			StateReads:               []engine.StateReadRef{{SubjectAddress: "ldl:project:p:entity:a", FieldPath: "system.updated_at"}},
			SupportEntityAddresses:   []string{"ldl:project:p:entity:a"},
			TraversedEntityAddresses: []string{"ldl:project:p:entity:b"},
		},
		ReturnedItems: 6,
	}
	returnedBytes, err := engine.MeasureDocumentQueryLogicalBytes(context.Background(), queryResultInput, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	queryResultInput.ReturnedBytes = returnedBytes
	mapped, err := mapExecuteQueryResult(context.Background(), queryResultInput, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if mapped.DocumentGeneration.Value != "7" || mapped.ReturnedBytes == "0" || mapped.ReturnedItems != "6" ||
		len(mapped.Result.CycleRefs) != 1 || len(mapped.Result.StateReads) != 1 || len(mapped.Result.Diagnostics) != 1 {
		t.Fatalf("mapped query result = %+v", mapped)
	}
	logical := mapped
	logical.ReturnedBytes = "0"
	encoded := canonicalJSONForTest(t, logical)
	if mapped.ReturnedBytes != engineprotocol.LogicalResponseByteCount(strconv.Itoa(len(encoded))) {
		t.Fatalf("returned bytes = %q encoded=%d", mapped.ReturnedBytes, len(encoded))
	}
	if _, err := mapExecuteQueryResult(context.Background(), engine.ExecuteDocumentQueryResult{ReturnedItems: -1}, 1<<20); err == nil {
		t.Fatal("negative returned items accepted")
	}
	if _, err := mapExecuteQueryResult(context.Background(), queryResultInput, 1); !engine.IsWorkbenchError(err, engine.WorkbenchErrorLimitExceeded) {
		t.Fatalf("output byte limit error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := mapExecuteQueryResult(cancelled, queryResultInput, 1<<20); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled query result mapping error = %v", err)
	}
	if err := queryMappingContext(nil); !engine.IsWorkbenchError(err, engine.WorkbenchErrorInvariant) {
		t.Fatalf("nil query mapping context error = %v", err)
	}
	if _, err := mapExecuteQueryInput(engineprotocol.ExecuteQueryInput{Arguments: map[string]semantic.RecipeScalar{"bad": {Kind: "unknown"}}}); err == nil {
		t.Fatal("bad execute query input scalar accepted")
	}
	if mapped := queryResultMappingError(errors.New("invalid driver result")); !engine.IsWorkbenchError(mapped, engine.WorkbenchErrorInvariant) {
		t.Fatalf("plain mapping error = %v", mapped)
	}
	if mapped := queryResultMappingError(context.Canceled); !errors.Is(mapped, context.Canceled) {
		t.Fatalf("cancelled mapping error = %v", mapped)
	}
	limitError := &engine.WorkbenchError{Code: "limit", Category: engine.WorkbenchErrorLimitExceeded}
	if mapped := queryResultMappingError(limitError); mapped != limitError {
		t.Fatalf("typed mapping error = %v, want identity", mapped)
	}
}

func TestMaterializeViewMappingCoversShapesProvenanceAndLimits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	generation := engine.DocumentGeneration{
		DocumentHandle: engine.DocumentHandle{EndpointInstanceID: "engine-test", Value: "document_abcdefghijklmnop"},
		Value:          1,
	}
	limits := engine.WorkbenchLimits{MaxItems: 1_000, MaxOutputBytes: 1 << 20}
	queryAddress := "ldl:project:p:query:q"
	address := "ldl:project:p:entity:alpha"
	betaAddress := "ldl:project:p:entity:beta"
	typeAddress := "ldl:project:p:entity-type:service"
	layerAddress := "ldl:project:p:layer:app"
	relationAddress := "ldl:project:p:relation:alpha_beta"
	baseSource := engine.ViewDataSourceRefs{
		SubjectAddresses: []string{"ldl:project:p", typeAddress, "ldl:project:p:relation-type:calls", queryAddress, "ldl:project:p:view:v"},
		EntityAddresses:  []string{address, betaAddress}, RelationAddresses: []string{relationAddress}, LayerAddresses: []string{layerAddress},
		RowAddresses: []string{"ldl:project:p:entity:alpha:row:primary"}, CellRefs: []engine.ViewDataCellRef{}, AssetDigests: []string{},
		State: engine.ViewDataStateRefs{Reads: []engine.StateReadRef{{SubjectAddress: address, FieldPath: "system.updated_at"}}},
	}
	diagramBase := engine.ViewDataBase{
		Kind: engine.ViewDataDiagram, Category: "topology", ProjectAddress: "ldl:project:p", ViewAddress: "ldl:project:p:view:v", QueryAddress: &queryAddress,
		Shape: view.Shape{Kind: view.ShapeDiagram, Diagram: &view.DiagramShape{
			Layout: view.LayoutManual, Direction: view.DirectionLeftToRight, Abstraction: view.AbstractionNormal,
			Placements: []view.Placement{{EntityAddress: address, X: -1.5, Y: 2, Width: 200, Height: 100}},
		}},
		Revision:    engine.ViewRevision{Single: &engine.SingleRevision{Kind: "single", RevisionID: "revision-1", DefinitionHash: "sha256:" + strings.Repeat("a", 64)}},
		StatePolicy: "none", StateInput: engine.QueryStateInputRef{Kind: "none"}, Source: baseSource,
		Diagnostics: []engine.Diagnostic{{Code: "LDL0001", Severity: "warning", MessageKey: "view_notice", Arguments: map[string]string{"view": "v"}, Related: []engine.DiagnosticRelated{}}},
	}
	alphaSource := engine.ViewDataSourceRefs{SubjectAddresses: []string{typeAddress}, EntityAddresses: []string{address}, RelationAddresses: []string{}, LayerAddresses: []string{layerAddress}, RowAddresses: []string{}, CellRefs: []engine.ViewDataCellRef{}, AssetDigests: []string{}, State: engine.ViewDataStateRefs{Reads: []engine.StateReadRef{}}}
	betaSource := alphaSource
	betaSource.EntityAddresses = []string{betaAddress}
	relationSource := engine.ViewDataSourceRefs{SubjectAddresses: []string{"ldl:project:p:relation-type:calls"}, EntityAddresses: []string{}, RelationAddresses: []string{relationAddress}, LayerAddresses: []string{}, RowAddresses: []string{}, CellRefs: []engine.ViewDataCellRef{}, AssetDigests: []string{}, State: engine.ViewDataStateRefs{Reads: []engine.StateReadRef{}}}
	alphaOccurrenceKey := viewDataTestKey("diagram-occurrence", "A")
	betaOccurrenceKey := viewDataTestKey("diagram-occurrence", "B")
	containerKey := viewDataTestKey("diagram-container", "W")
	badgeLabel := "critical"
	diagramData := engine.ViewData{Diagram: &engine.DiagramViewData{
		ViewDataBase: diagramBase,
		Occurrences: []engine.DiagramOccurrence{
			{Key: alphaOccurrenceKey, EntityAddress: address, LayerAddress: layerAddress, Role: engine.DiagramRoleNode, Source: alphaSource},
			{Key: betaOccurrenceKey, EntityAddress: betaAddress, LayerAddress: layerAddress, ParentKey: &alphaOccurrenceKey, ViaRelationAddress: &relationAddress, Role: engine.DiagramRoleNode, Source: betaSource},
		},
		Edges: []engine.DiagramEdge{{Key: viewDataTestKey("diagram-edge", "C"), FromOccurrenceKey: alphaOccurrenceKey, ToOccurrenceKey: betaOccurrenceKey, RelationAddress: relationAddress, RelationTypeAddress: "ldl:project:p:relation-type:calls", Source: relationSource}},
		Containers: []engine.DiagramContainer{{
			Key: containerKey, OccurrenceKey: alphaOccurrenceKey, ChildKeys: []string{betaOccurrenceKey}, Source: alphaSource,
		}},
		Overlays: []engine.DiagramOverlay{{
			Key: viewDataTestKey("diagram-overlay", "X"), TargetOccurrenceKey: betaOccurrenceKey, OverlayEntityAddress: address,
			RelationAddress: relationAddress, RelationTypeAddress: "ldl:project:p:relation-type:calls", Source: relationSource,
		}},
		Badges: []engine.DiagramBadge{{
			Key: viewDataTestKey("diagram-badge", "Y"), TargetOccurrenceKey: betaOccurrenceKey, RelationAddress: relationAddress,
			RelationTypeAddress: "ldl:project:p:relation-type:calls", Label: &badgeLabel, Source: relationSource,
		}},
		SupportItems: []engine.DiagramSupportItem{{
			Key: viewDataTestKey("diagram-support", "Z"), SupportKind: engine.DiagramSupportHiddenRelation,
			EntityAddress: &address, RelationAddress: &relationAddress, Source: relationSource,
		}},
	}}
	diagram, err := mapMaterializeViewResult(ctx, engine.MaterializeDocumentViewResult{DocumentGeneration: generation, ViewData: diagramData}, limits)
	if err != nil || diagram.ViewData.Diagram == nil || diagram.ViewData.Shape.Diagram == nil || len(diagram.ViewData.Shape.Diagram.Placements) != 1 ||
		len(diagram.ViewData.Diagram.Containers) != 1 || len(diagram.ViewData.Diagram.Overlays) != 1 ||
		len(diagram.ViewData.Diagram.Badges) != 1 || len(diagram.ViewData.Diagram.SupportItems) != 1 ||
		diagram.ViewData.Diagram.Badges[0].Label == nil || *diagram.ViewData.Diagram.Badges[0].Label != badgeLabel ||
		diagram.ReturnedItems == "0" || diagram.ReturnedBytes == "0" {
		t.Fatalf("diagram mapping = %+v err=%v", diagram, err)
	}

	scalar := engine.TypedScalar{Type: definition.ScalarString, String: "api"}
	nameAddress := "ldl:project:p:view:v:table-column:name"
	tableBase := diagramBase
	tableBase.Kind = engine.ViewDataTable
	tableBase.Category = "inventory"
	tableBase.Shape = view.Shape{Kind: view.ShapeTable, Table: &view.TableShape{
		RowSource: view.RowsEntity, AutomaticRelationColumns: []string{},
		Sorts: []view.TableSort{{ColumnID: "name", Direction: view.SortAscending, Absent: view.AbsentLast}},
	}}
	nameKey := viewDataTestKey("table-column", "D")
	ownerKey := viewDataTestKey("table-column", "E")
	tagsKey := viewDataTestKey("table-column", "F")
	emptyKey := viewDataTestKey("table-column", "G")
	tableData := engine.ViewData{Table: &engine.TableViewData{
		ViewDataBase: tableBase,
		Columns: []engine.TableColumn{
			{Key: nameKey, ID: "name", Address: &nameAddress, Label: "Name", ValueType: "string", SourceColumnAddresses: []string{"ldl:project:p:entity-type:service:column:name"}},
			{Key: ownerKey, ID: "owner", Label: "Owner", ValueType: "stable_address", SourceColumnAddresses: []string{}},
			{Key: tagsKey, ID: "tags", Label: "Tags", ValueType: "string_set", SourceColumnAddresses: []string{}},
			{Key: emptyKey, ID: "empty", Label: "Empty", ValueType: "string", SourceColumnAddresses: []string{}},
		},
		Rows: []engine.TableRow{{
			Key: viewDataTestKey("table-row", "H"), Source: engine.ViewDataSourceRefs{SubjectAddresses: []string{typeAddress}, EntityAddresses: []string{address}, RelationAddresses: []string{}, LayerAddresses: []string{layerAddress}, RowAddresses: []string{"ldl:project:p:entity:alpha:row:primary"}, CellRefs: []engine.ViewDataCellRef{}, AssetDigests: []string{}, State: engine.ViewDataStateRefs{Reads: []engine.StateReadRef{}}},
			Cells: map[string]engine.TableCell{
				nameKey:  {Present: true, Value: &engine.ViewDataValue{Kind: "scalar", Scalar: &scalar}, Source: engine.ViewDataSourceRefs{SubjectAddresses: []string{typeAddress}, EntityAddresses: []string{address}, RelationAddresses: []string{}, LayerAddresses: []string{layerAddress}, RowAddresses: []string{"ldl:project:p:entity:alpha:row:primary"}, CellRefs: []engine.ViewDataCellRef{{RowAddress: "ldl:project:p:entity:alpha:row:primary", ColumnAddress: "ldl:project:p:entity-type:service:column:name"}}, AssetDigests: []string{}, State: engine.ViewDataStateRefs{Reads: []engine.StateReadRef{}}}},
				ownerKey: {Present: true, Value: &engine.ViewDataValue{Kind: "stable_address", Address: &address}, Source: alphaSource},
				tagsKey:  {Present: true, Value: &engine.ViewDataValue{Kind: "string_set", StringSet: []string{"api", "prod"}}, Source: alphaSource},
				emptyKey: {Present: false, Source: alphaSource},
			},
		}},
	}}
	table, err := mapMaterializeViewResult(ctx, engine.MaterializeDocumentViewResult{DocumentGeneration: generation, ViewData: tableData}, limits)
	if err != nil || table.ViewData.Table == nil || table.ViewData.Shape.Table == nil || len(table.ViewData.Table.Rows) != 1 || len(table.ViewData.Table.Rows[0].Cells) != 4 || len(table.ViewData.Shape.Table.Sorts) != 1 {
		t.Fatalf("table mapping = %+v err=%v", table, err)
	}
	emptyStringSet, err := mapViewDataValue(engine.ViewDataValue{Kind: "string_set", StringSet: []string{}})
	if err != nil || emptyStringSet.StringSet == nil || *emptyStringSet.StringSet == nil || len(*emptyStringSet.StringSet) != 0 {
		t.Fatalf("empty string-set mapping = %+v err=%v", emptyStringSet, err)
	}

	contextBase := diagramBase
	contextBase.Kind = engine.ViewDataContext
	contextBase.Category = "context"
	contextBase.Shape = view.Shape{Kind: view.ShapeContext, Context: &view.ContextShape{GroupBy: view.ContextGroupNone, Incoming: true, Outgoing: true}}
	contextData := engine.ViewData{Context: &engine.ContextViewData{
		ViewDataBase: contextBase,
		Groups: []engine.ContextGroup{{
			Key: viewDataTestKey("context-group", "I"), Label: "All", Source: alphaSource,
			Facts: []engine.ContextFact{{
				Key: viewDataTestKey("context-fact", "J"), Direction: engine.ContextFactOutgoing, Text: "Alpha", EntityAddress: address,
				RelationAddress: relationAddress, RowAddresses: []string{"ldl:project:p:entity:alpha:row:primary"}, Source: relationSource,
			}},
			Attributes: []engine.ContextAttribute{{
				Key: viewDataTestKey("context-attribute", "a"), GroupKey: viewDataTestKey("context-group", "I"),
				OwnerAddress: address, RowAddress: "ldl:project:p:entity:alpha:row:primary",
				Values: map[string]engine.TypedScalar{"ldl:project:p:entity-type:service:column:name": {Type: definition.ScalarString, String: "Alpha"}},
				Source: alphaSource,
			}},
		}},
	}}
	contextResult, err := mapMaterializeViewResult(ctx, engine.MaterializeDocumentViewResult{DocumentGeneration: generation, ViewData: contextData}, limits)
	if err != nil || contextResult.ViewData.Context == nil || len(contextResult.ViewData.Context.Groups) != 1 ||
		len(contextResult.ViewData.Context.Groups[0].Facts) != 1 || len(contextResult.ViewData.Context.Groups[0].Attributes) != 1 ||
		len(contextResult.ViewData.Context.Groups[0].Attributes[0].Values) != 1 {
		t.Fatalf("context mapping = %+v err=%v", contextResult, err)
	}
	matrixBase := diagramBase
	matrixBase.Kind = engine.ViewDataMatrix
	matrixBase.Category = "dependency"
	matrixBase.Shape = view.Shape{Kind: view.ShapeMatrix, Matrix: &view.MatrixShape{
		RowAxis:    view.MatrixAxis{LabelField: view.AxisLabelDisplayName},
		ColumnAxis: view.MatrixAxis{LabelField: view.AxisLabelDisplayName},
		Cell:       view.MatrixCell{Direction: definition.TraversalBoth, Semantic: view.MatrixRelationRefs, Display: view.MatrixExists},
	}}
	rowAxisKey := viewDataTestKey("matrix-row", "K")
	columnAxisKey := viewDataTestKey("matrix-column", "L")
	matrixPath := engine.QueryPath{EntityAddresses: []string{address, betaAddress}, RelationAddresses: []string{relationAddress}}
	matrixData := engine.ViewData{Matrix: &engine.MatrixViewData{
		ViewDataBase: matrixBase,
		RowAxis:      []engine.MatrixAxisItem{{Key: rowAxisKey, EntityAddress: address, Label: "Alpha", Source: alphaSource}},
		ColumnAxis:   []engine.MatrixAxisItem{{Key: columnAxisKey, EntityAddress: betaAddress, Label: "Beta", Source: betaSource}},
		Cells: []engine.MatrixCell{
			{
				Key: viewDataTestKey("matrix-cell", "M"), RowKey: rowAxisKey, ColumnKey: columnAxisKey,
				SemanticRefs: []engine.MatrixSemanticRef{{RelationAddress: &relationAddress}, {Path: &matrixPath}},
				DisplayValue: engine.MatrixDisplayValue{Kind: "boolean", Boolean: true}, Source: relationSource,
			},
			{
				Key: viewDataTestKey("matrix-cell", "b"), RowKey: rowAxisKey, ColumnKey: columnAxisKey,
				SemanticRefs: []engine.MatrixSemanticRef{}, DisplayValue: engine.MatrixDisplayValue{Kind: "integer", Integer: -2}, Source: relationSource,
			},
			{
				Key: viewDataTestKey("matrix-cell", "c"), RowKey: rowAxisKey, ColumnKey: columnAxisKey,
				SemanticRefs: []engine.MatrixSemanticRef{}, DisplayValue: engine.MatrixDisplayValue{Kind: "string_set", StringSet: []string{"api", "prod"}}, Source: relationSource,
			},
			{
				Key: viewDataTestKey("matrix-cell", "d"), RowKey: rowAxisKey, ColumnKey: columnAxisKey,
				SemanticRefs: []engine.MatrixSemanticRef{}, DisplayValue: engine.MatrixDisplayValue{Kind: "attributes", Attributes: []engine.MatrixAttributeItem{{
					RelationAddress: relationAddress, RowAddress: "ldl:project:p:relation:alpha_beta:row:primary",
					ColumnAddress: "ldl:project:p:relation-type:calls:column:protocol", Value: engine.TypedScalar{Type: definition.ScalarString, String: "https"},
				}}}, Source: relationSource,
			},
		},
	}}
	matrixResult, err := mapMaterializeViewResult(ctx, engine.MaterializeDocumentViewResult{DocumentGeneration: generation, ViewData: matrixData}, limits)
	if err != nil || matrixResult.ViewData.Matrix == nil || len(matrixResult.ViewData.Matrix.Cells) != 4 ||
		len(matrixResult.ViewData.Matrix.Cells[0].SemanticRefs) != 2 || matrixResult.ViewData.Matrix.Cells[0].SemanticRefs[1].Path == nil ||
		matrixResult.ViewData.Matrix.Cells[0].DisplayValue.Boolean == nil || matrixResult.ViewData.Matrix.Cells[1].DisplayValue.Integer == nil ||
		matrixResult.ViewData.Matrix.Cells[2].DisplayValue.StringSet == nil || matrixResult.ViewData.Matrix.Cells[3].DisplayValue.Attributes == nil {
		t.Fatalf("matrix mapping = %+v err=%v", matrixResult, err)
	}

	treeBase := diagramBase
	treeBase.Kind = engine.ViewDataTree
	treeBase.Category = "hierarchy"
	treeBase.Shape = view.Shape{Kind: view.ShapeTree, Tree: &view.TreeShape{
		RelationTypeAddresses: []string{"ldl:project:p:relation-type:calls"},
		CyclePolicy:           view.TreeCycleTruncate,
		SharedChildPolicy:     view.SharedChildLink,
	}}
	treeRootKey := viewDataTestKey("tree-occurrence", "N")
	treeChildKey := viewDataTestKey("tree-occurrence", "O")
	treeData := engine.ViewData{Tree: &engine.TreeViewData{
		ViewDataBase: treeBase,
		Roots: []engine.TreeOccurrence{{
			Key: treeRootKey, EntityAddress: address, Source: alphaSource,
			Children: []engine.TreeOccurrence{{Key: treeChildKey, EntityAddress: betaAddress, ViaRelationAddress: &relationAddress, Children: []engine.TreeOccurrence{}, Source: betaSource}},
		}},
		CycleRefs: []engine.TreeRef{{Key: viewDataTestKey("tree-cycle", "e"), FromOccurrenceKey: treeChildKey, ToEntityAddress: address, RelationAddress: relationAddress, Source: relationSource}},
		LinkRefs:  []engine.TreeRef{{Key: viewDataTestKey("tree-link", "P"), FromOccurrenceKey: treeRootKey, ToEntityAddress: betaAddress, RelationAddress: relationAddress, Source: relationSource}},
	}}
	treeResult, err := mapMaterializeViewResult(ctx, engine.MaterializeDocumentViewResult{DocumentGeneration: generation, ViewData: treeData}, limits)
	if err != nil || treeResult.ViewData.Tree == nil || len(treeResult.ViewData.Tree.Roots) != 1 || len(treeResult.ViewData.Tree.Roots[0].Children) != 1 || len(treeResult.ViewData.Tree.CycleRefs) != 1 {
		t.Fatalf("tree mapping = %+v err=%v", treeResult, err)
	}

	flowBase := diagramBase
	flowBase.Kind = engine.ViewDataFlow
	flowBase.Category = "flow"
	flowBase.Shape = view.Shape{Kind: view.ShapeFlow, Flow: &view.FlowShape{
		RelationTypeAddresses: []string{"ldl:project:p:relation-type:calls"}, LaneBy: view.LaneLayer,
		CyclePolicy: view.FlowCycleIncludeCycleRef, PreserveParallel: true,
	}}
	laneKey := viewDataTestKey("flow-lane", "Q")
	alphaStepKey := viewDataTestKey("flow-step", "R")
	betaStepKey := viewDataTestKey("flow-step", "S")
	connectorKey := viewDataTestKey("flow-connector", "T")
	branchValue := engine.TypedScalar{Type: definition.ScalarString, String: "approved"}
	flowData := engine.ViewData{Flow: &engine.FlowViewData{
		ViewDataBase: flowBase,
		Lanes:        []engine.FlowLane{{Key: laneKey, Label: "App", StepKeys: []string{alphaStepKey, betaStepKey}, Source: alphaSource}},
		Steps: []engine.FlowStep{
			{Key: alphaStepKey, EntityAddress: address, LaneKey: laneKey, Branch: true, Source: alphaSource},
			{Key: betaStepKey, EntityAddress: betaAddress, LaneKey: laneKey, Join: true, Source: betaSource},
		},
		Connectors: []engine.FlowConnector{{
			Key: connectorKey, FromStepKey: alphaStepKey, ToStepKey: betaStepKey, Kind: engine.FlowConnectorSequence,
			BranchValue: &branchValue, BranchRowAddresses: []string{"ldl:project:p:entity:alpha:row:primary"}, RelationAddresses: []string{relationAddress}, Source: relationSource,
		}},
		CycleRefs: []engine.FlowCycleRef{{
			Key: viewDataTestKey("flow-cycle", "f"), ConnectorKey: connectorKey, FromStepKey: betaStepKey, ToStepKey: alphaStepKey,
			Kind: engine.FlowConnectorControl, BranchValue: &branchValue,
			BranchRowAddresses: []string{"ldl:project:p:entity:alpha:row:primary"}, RelationAddresses: []string{relationAddress}, Source: relationSource,
		}},
	}}
	flowResult, err := mapMaterializeViewResult(ctx, engine.MaterializeDocumentViewResult{DocumentGeneration: generation, ViewData: flowData}, limits)
	if err != nil || flowResult.ViewData.Flow == nil || len(flowResult.ViewData.Flow.Connectors) != 1 || len(flowResult.ViewData.Flow.CycleRefs) != 1 ||
		flowResult.ViewData.Flow.Connectors[0].Kind != semantic.FlowConnectorKindSequence || flowResult.ViewData.Flow.Connectors[0].BranchValue == nil ||
		flowResult.ViewData.Flow.CycleRefs[0].BranchValue == nil {
		t.Fatalf("flow mapping = %+v err=%v", flowResult, err)
	}

	diffBase := diagramBase
	diffBase.Kind = engine.ViewDataDiff
	diffBase.Category = "diff"
	diffBase.QueryAddress = nil
	diffBase.Shape = view.Shape{Kind: view.ShapeDiff, Diff: &view.DiffShape{Include: []view.DiffSubjectKind{view.DiffEntity}, DetectMoves: true}}
	diffBase.Revision = engine.ViewRevision{Diff: &engine.DiffRevision{
		Kind: "diff", RecipeRevisionID: "recipe-1", RecipeDefinitionHash: "sha256:" + strings.Repeat("a", 64),
		BeforeRevisionID: "before-1", BeforeDefinitionHash: "sha256:" + strings.Repeat("b", 64),
		AfterRevisionID: "after-1", AfterDefinitionHash: "sha256:" + strings.Repeat("c", 64),
	}}
	beforeValue := engine.SemanticValue{Kind: engine.SemanticValueString, String: "old"}
	afterValue := engine.SemanticValue{Kind: engine.SemanticValueString, String: "new"}
	compositeValue := engine.SemanticValue{Kind: engine.SemanticValueMap, Map: []engine.SemanticMapEntry{
		{Key: "absent", Value: engine.SemanticValue{Kind: engine.SemanticValueAbsent}},
		{Key: "address", Value: engine.SemanticValue{Kind: engine.SemanticValueAddress, Address: address}},
		{Key: "array", Value: engine.SemanticValue{Kind: engine.SemanticValueArray, Array: []engine.SemanticValue{
			{Kind: engine.SemanticValueBlob, BlobRef: &engine.SemanticBlobRef{
				BlobID: "blob-1", Digest: "sha256:" + strings.Repeat("d", 64), Lifetime: "request", MediaType: "application/octet-stream", Size: 3,
			}},
			{Kind: engine.SemanticValueBoolean, Boolean: true},
		}}},
		{Key: "decimal", Value: engine.SemanticValue{Kind: engine.SemanticValueDecimal, Decimal: "1.25"}},
		{Key: "integer", Value: engine.SemanticValue{Kind: engine.SemanticValueInteger, Integer: -7}},
		{Key: "string", Value: engine.SemanticValue{Kind: engine.SemanticValueString, String: "value"}},
		{Key: "token", Value: engine.SemanticValue{Kind: engine.SemanticValueToken, String: "token"}},
	}}
	diffData := engine.ViewData{Diff: &engine.DiffViewData{
		ViewDataBase: diffBase,
		Changes: []engine.DiffChange{{
			Key: viewDataTestKey("diff-change", "U"), Kind: engine.DiffChangeUpdated, SubjectKind: "entity",
			BeforeAddress: &address, AfterAddress: &address, Source: alphaSource, BeforeSource: &alphaSource, AfterSource: &alphaSource,
			Fields: []engine.FieldDiff{
				{
					Key: viewDataTestKey("diff-field", "V"), Path: []string{"display_name"},
					BeforePresent: true, Before: &beforeValue, AfterPresent: true, After: &afterValue,
				},
				{
					Key: viewDataTestKey("diff-field", "g"), Path: []string{"representation"},
					BeforePresent: true, Before: &compositeValue, AfterPresent: false,
				},
			},
		}},
	}}
	diffResult, err := mapMaterializeViewResult(ctx, engine.MaterializeDocumentViewResult{DocumentGeneration: generation, ViewData: diffData}, limits)
	if err != nil || diffResult.ViewData.Diff == nil || len(diffResult.ViewData.Diff.Changes) != 1 || len(diffResult.ViewData.Diff.Changes[0].Fields) != 2 ||
		diffResult.ViewData.Diff.Changes[0].Fields[1].Before == nil || diffResult.ViewData.Diff.Changes[0].Fields[1].Before.Map == nil ||
		diffResult.ViewData.Revision.Kind != "diff" {
		t.Fatalf("diff mapping = %+v err=%v", diffResult, err)
	}
	logicalDiff := diffResult
	logicalDiff.ReturnedBytes = "0"
	wantDiffBytes := len(canonicalJSONForTest(t, logicalDiff)) + 3
	if diffResult.ReturnedBytes != engineprotocol.LogicalResponseByteCount(strconv.Itoa(wantDiffBytes)) {
		t.Fatalf("diff returned bytes = %q, want %d", diffResult.ReturnedBytes, wantDiffBytes)
	}
	if _, err := mapMaterializeViewResult(ctx, engine.MaterializeDocumentViewResult{DocumentGeneration: generation, ViewData: diffData}, engine.WorkbenchLimits{MaxItems: limits.MaxItems, MaxOutputBytes: int64(wantDiffBytes - 1)}); !engine.IsWorkbenchError(err, engine.WorkbenchErrorLimitExceeded) {
		t.Fatalf("diff attachment byte limit error = %v", err)
	}

	if _, err := mapMaterializeViewResult(ctx, engine.MaterializeDocumentViewResult{DocumentGeneration: generation, ViewData: diagramData}, engine.WorkbenchLimits{MaxItems: 1, MaxOutputBytes: limits.MaxOutputBytes}); !engine.IsWorkbenchError(err, engine.WorkbenchErrorLimitExceeded) {
		t.Fatalf("item limit error = %v", err)
	}
	if _, err := mapMaterializeViewResult(ctx, engine.MaterializeDocumentViewResult{DocumentGeneration: generation, ViewData: diagramData}, engine.WorkbenchLimits{MaxItems: limits.MaxItems, MaxOutputBytes: 1}); !engine.IsWorkbenchError(err, engine.WorkbenchErrorLimitExceeded) {
		t.Fatalf("byte limit error = %v", err)
	}
	if _, err := mapMaterializeViewResult(ctx, engine.MaterializeDocumentViewResult{ViewData: diagramData}, limits); err == nil {
		t.Fatal("invalid document generation was accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := mapMaterializeViewResult(cancelled, engine.MaterializeDocumentViewResult{DocumentGeneration: generation, ViewData: diagramData}, limits); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled mapping error = %v", err)
	}
	for name, placement := range map[string]view.Placement{
		"x":      {EntityAddress: address, X: math.NaN(), Width: 1, Height: 1},
		"y":      {EntityAddress: address, Y: math.Inf(1), Width: 1, Height: 1},
		"width":  {EntityAddress: address, Width: 0, Height: 1},
		"height": {EntityAddress: address, Width: 1, Height: 0},
	} {
		invalid := view.Shape{Kind: view.ShapeDiagram, Diagram: &view.DiagramShape{
			Layout: view.LayoutManual, Direction: view.DirectionLeftToRight, Abstraction: view.AbstractionNormal,
			Placements: []view.Placement{placement},
		}}
		if _, err := mapCompiledViewShape(invalid); err == nil {
			t.Fatalf("invalid diagram %s placement was accepted", name)
		}
	}
	if _, err := mapDiagramViewData(cancelled, *diagramData.Diagram); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled diagram mapping error = %v", err)
	}
	if _, err := mapTableViewData(cancelled, *tableData.Table); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled table mapping error = %v", err)
	}
	if _, err := mapContextViewData(cancelled, *contextData.Context); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context mapping error = %v", err)
	}
	if _, err := countProtocolArrayItems(nil, []any{}, limits.MaxItems); !engine.IsWorkbenchError(err, engine.WorkbenchErrorInvariant) {
		t.Fatalf("nil count context error = %v", err)
	}
	var nested any = []any{}
	for range 130 {
		nested = []any{nested}
	}
	if _, err := countProtocolArrayItems(ctx, nested, limits.MaxItems); err == nil {
		t.Fatal("over-depth ViewData was accepted")
	}
	for _, value := range []engine.ViewDataValue{
		{Kind: "scalar"}, {Kind: "stable_address"}, {Kind: "unknown"},
	} {
		if _, err := mapViewDataValue(value); err == nil {
			t.Fatalf("invalid ViewData value accepted: %+v", value)
		}
	}
}

func viewDataTestKey(kind, fill string) string {
	return "vdi:" + kind + ":" + strings.Repeat(fill, 43)
}

func TestViewDataMappersRejectMalformedClosedUnionValues(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	relationAddress := "ldl:project:p:relation:alpha_beta"
	path := engine.QueryPath{EntityAddresses: []string{"ldl:project:p:entity:alpha"}, RelationAddresses: []string{relationAddress}}
	for name, input := range map[string]engine.MatrixSemanticRef{
		"empty": {},
		"both":  {RelationAddress: &relationAddress, Path: &path},
	} {
		if _, err := mapMatrixSemanticRef(input); err == nil {
			t.Fatalf("invalid Matrix semantic ref %s was accepted", name)
		}
	}
	invalidScalar := engine.TypedScalar{Type: "unsupported"}
	for name, input := range map[string]engine.MatrixDisplayValue{
		"unknown kind": {Kind: "unknown"},
		"invalid attribute scalar": {Kind: "attributes", Attributes: []engine.MatrixAttributeItem{{
			RelationAddress: relationAddress, RowAddress: "ldl:project:p:relation:alpha_beta:row:primary",
			ColumnAddress: "ldl:project:p:relation-type:calls:column:protocol", Value: invalidScalar,
		}}},
	} {
		if _, err := mapMatrixDisplayValue(input); err == nil {
			t.Fatalf("invalid Matrix display %s was accepted", name)
		}
	}
	emptySource := engine.ViewDataSourceRefs{}
	if _, err := mapFlowConnector(ctx, engine.FlowConnector{BranchValue: &invalidScalar, Source: emptySource}); err == nil {
		t.Fatal("invalid Flow connector branch value was accepted")
	}
	if _, err := mapFlowCycleRef(ctx, engine.FlowCycleRef{BranchValue: &invalidScalar, Source: emptySource}); err == nil {
		t.Fatal("invalid Flow cycle branch value was accepted")
	}
	for name, input := range map[string]engine.SemanticValue{
		"blob without ref": {Kind: engine.SemanticValueBlob},
		"unknown kind":     {Kind: "unknown"},
		"invalid array child": {Kind: engine.SemanticValueArray, Array: []engine.SemanticValue{{
			Kind: "unknown",
		}}},
		"invalid map child": {Kind: engine.SemanticValueMap, Map: []engine.SemanticMapEntry{{
			Key: "invalid", Value: engine.SemanticValue{Kind: "unknown"},
		}}},
	} {
		if _, err := mapViewDataSemanticValue(input, 0); err == nil {
			t.Fatalf("invalid Diff semantic value %s was accepted", name)
		}
	}
	if _, err := mapViewDataSemanticValue(engine.SemanticValue{Kind: engine.SemanticValueAbsent}, protocolcommon.MaxWireJSONDepth+1); err == nil {
		t.Fatal("over-depth Diff semantic value was accepted")
	}
	for name, input := range map[string]engine.ViewRevision{
		"empty":               {},
		"both":                {Single: &engine.SingleRevision{Kind: "single"}, Diff: &engine.DiffRevision{Kind: "diff"}},
		"invalid single kind": {Single: &engine.SingleRevision{Kind: "invalid"}},
		"invalid diff kind":   {Diff: &engine.DiffRevision{Kind: "invalid"}},
	} {
		if _, err := mapViewRevision(input); err == nil {
			t.Fatalf("invalid View revision %s was accepted", name)
		}
	}
	if _, err := mapViewData(ctx, engine.ViewData{}); err == nil {
		t.Fatal("empty ViewData union was accepted")
	}
	if _, err := measureViewDataBlobBytes(nil, engine.ViewData{}); !engine.IsWorkbenchError(err, engine.WorkbenchErrorInvariant) {
		t.Fatalf("nil blob measurement context error = %v", err)
	}
	missingBlob := engine.SemanticValue{Kind: engine.SemanticValueBlob}
	invalidBlobData := engine.ViewData{Diff: &engine.DiffViewData{Changes: []engine.DiffChange{{Fields: []engine.FieldDiff{{Before: &missingBlob}}}}}}
	if _, err := measureViewDataBlobBytes(ctx, invalidBlobData); err == nil {
		t.Fatal("missing Diff BlobRef was accepted by logical byte measurement")
	}
	maxBlob := engine.SemanticValue{Kind: engine.SemanticValueBlob, BlobRef: &engine.SemanticBlobRef{Size: math.MaxUint64}}
	oneBlob := engine.SemanticValue{Kind: engine.SemanticValueBlob, BlobRef: &engine.SemanticBlobRef{Size: 1}}
	overflowData := engine.ViewData{Diff: &engine.DiffViewData{Changes: []engine.DiffChange{{Fields: []engine.FieldDiff{{Before: &maxBlob, After: &oneBlob}}}}}}
	if _, err := measureViewDataBlobBytes(ctx, overflowData); err == nil {
		t.Fatal("overflowing Diff BlobRef sizes were accepted")
	}
	root := engine.TreeOccurrence{Source: emptySource, Children: []engine.TreeOccurrence{}}
	cursor := &root
	for range protocolcommon.MaxWireJSONDepth + 1 {
		cursor.Children = []engine.TreeOccurrence{{Source: emptySource, Children: []engine.TreeOccurrence{}}}
		cursor = &cursor.Children[0]
	}
	if _, err := mapTreeOccurrences(ctx, []engine.TreeOccurrence{root}); err == nil {
		t.Fatal("over-depth Tree ViewData was accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	for name, run := range map[string]func() error{
		"matrix": func() error {
			_, err := mapMatrixAxisItems(cancelled, []engine.MatrixAxisItem{{Source: emptySource}})
			return err
		},
		"tree": func() error {
			_, err := mapTreeRefs(cancelled, []engine.TreeRef{{Source: emptySource}})
			return err
		},
		"flow": func() error {
			_, err := mapFlowCycleRef(cancelled, engine.FlowCycleRef{Source: emptySource})
			return err
		},
		"diff": func() error {
			_, err := mapDiffViewData(cancelled, engine.DiffViewData{Changes: []engine.DiffChange{{Source: emptySource}}})
			return err
		},
	} {
		if err := run(); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled %s mapping error = %v", name, err)
		}
	}
}

func TestMaterializeViewInputMappingPreservesDiagnosticProvenance(t *testing.T) {
	t.Parallel()
	message := "invalid selection"
	argument := "scope"
	subject := semantic.StableAddress("ldl:project:p:entity:alpha")
	owner := semantic.StableAddress("ldl:project:p")
	relatedMessage := "previous declaration"
	sourceRange := semantic.SourceRange{
		Origin: semantic.SourceOrigin{Kind: semantic.OriginKindProject}, ModulePath: "document.ldl",
		StartByte: "1", EndByte: "2",
	}
	diagnostic := semantic.Diagnostic{
		Arguments: map[string]semantic.DiagnosticArgumentValue{"name": {Kind: semantic.DiagnosticArgumentKindString, StringValue: &argument}},
		Code:      "LDL1601", Message: &message, MessageKey: "invalid_query", OwnerAddress: &owner, ProtocolVersion: 1,
		Range: &sourceRange, Related: []semantic.DiagnosticRelated{{Message: &relatedMessage, OwnerAddress: &owner, Range: &sourceRange, Relation: semantic.DiagnosticRelationPrevious, SubjectAddress: &subject}},
		Severity: semantic.DiagnosticSeverityError, SubjectAddress: &subject,
	}
	snapshotHash := protocolcommon.Digest("sha256:" + strings.Repeat("d", 64))
	definitionHash := protocolcommon.Digest("sha256:" + strings.Repeat("e", 64))
	graphHash := protocolcommon.Digest("sha256:" + strings.Repeat("f", 64))
	stateVersion := "state-1"
	capturedAt := protocolcommon.Rfc3339Time("2026-07-18T00:00:00Z")
	input := engineprotocol.QueryExecutionResultData{
		Arguments: map[string]semantic.RecipeScalar{"ldl:project:p:query:q:parameter:scope": {Kind: "string", StringValue: &argument}},
		CycleRefs: []engineprotocol.QueryCycleRef{{
			FromEntityAddress: "ldl:project:p:entity:alpha", Kind: "entity", Orientation: "forward",
			RelationAddress: "ldl:project:p:relation:alpha_beta", RetainedPath: engineprotocol.QueryPath{EntityAddresses: []semantic.EntityAddress{"ldl:project:p:entity:alpha"}, RelationAddresses: []semantic.RelationAddress{}},
			ToEntityAddress: "ldl:project:p:entity:alpha",
		}},
		Diagnostics: []semantic.Diagnostic{diagnostic}, InducedRelationAddresses: []semantic.RelationAddress{}, PathRelationAddresses: []semantic.RelationAddress{},
		Paths:                  []engineprotocol.QueryPath{{EntityAddresses: []semantic.EntityAddress{"ldl:project:p:entity:alpha"}, RelationAddresses: []semantic.RelationAddress{}}},
		PrimaryEntityAddresses: []semantic.EntityAddress{}, QueryAddress: "ldl:project:p:query:q", ReachedEntityAddresses: []semantic.EntityAddress{},
		SeedEntityAddresses: []semantic.EntityAddress{}, SelectedRelationAddresses: []semantic.RelationAddress{}, StateInput: engineprotocol.QueryStateInputRef{
			Kind: "snapshot", SnapshotHash: &snapshotHash, DefinitionHash: &definitionHash, StateVersion: &stateVersion, CapturedAt: &capturedAt,
		},
		StatePolicy: "none", StateReads: []engineprotocol.QueryStateReadRef{{SubjectAddress: subject, FieldPath: "system.updated_at"}},
		SupportEntityAddresses: []semantic.EntityAddress{}, TraversedEntityAddresses: []semantic.EntityAddress{},
	}
	mapped, err := queryResultFromProtocol(input)
	if err != nil || len(mapped.Diagnostics) != 1 || mapped.Diagnostics[0].Message != message || mapped.Diagnostics[0].Range == nil || len(mapped.Diagnostics[0].Related) != 1 || mapped.StateInput.SnapshotHash != string(snapshotHash) {
		t.Fatalf("diagnostic mapping = %+v err=%v", mapped.Diagnostics, err)
	}
	roundTripInput := mapped
	roundTripInput.Diagnostics = []engine.Diagnostic{}
	roundTrip, err := mapQueryExecutionResultData(context.Background(), roundTripInput)
	if err != nil || roundTrip.StateInput.SnapshotHash == nil || *roundTrip.StateInput.SnapshotHash != snapshotHash || roundTrip.StateInput.CapturedAt == nil || *roundTrip.StateInput.CapturedAt != capturedAt {
		t.Fatalf("state input round trip = %+v err=%v", roundTrip.StateInput, err)
	}
	updatedAt := "2026-07-17T00:00:00Z"
	materializeInput := engineprotocol.MaterializeViewInput{
		Kind: "query", Limits: engineprotocol.WorkbenchLimits{MaxItems: "128", MaxOutputBytes: "65536"},
		Query: &engineprotocol.MaterializeQueryViewInput{
			DocumentGeneration: engineprotocol.DocumentGeneration{DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "engine-test", Value: "document_abcdefghijklmnop"}, Value: "1"},
			QueryResult:        input,
			StateSnapshot: &semantic.StateQuerySnapshot{
				Format: semantic.StateQuerySnapshotFormatValue, SchemaVersion: 1,
				DefinitionProjectAddress: "ldl:project:p", DefinitionHash: definitionHash, GraphHash: graphHash,
				StateVersion: stateVersion, CapturedAt: capturedAt, InaccessibleFieldPaths: []semantic.StateFieldPath{},
				Subjects: []semantic.StateQuerySubject{{
					SubjectAddress: subject, OwnSubjectHash: snapshotHash,
					Fields:             map[string]semantic.RecipeScalar{"system.updated_at": {Kind: "datetime", StringValue: &updatedAt}},
					RedactedFieldPaths: []semantic.StateFieldPath{},
				}},
			},
		},
		ViewAddress: "ldl:project:p:view:v",
	}
	mappedInput, err := mapMaterializeViewInput(materializeInput)
	if err != nil || mappedInput.Query == nil || mappedInput.Query.StateSnapshot == nil || mappedInput.Query.StateSnapshot.Subjects[0].Fields["system.updated_at"].String != updatedAt {
		t.Fatalf("state snapshot mapping = %+v err=%v", mappedInput, err)
	}
	diffInput := engineprotocol.MaterializeViewInput{
		Kind: "diff", Limits: materializeInput.Limits, ViewAddress: materializeInput.ViewAddress,
		Diff: &engineprotocol.MaterializeDiffViewInput{
			RecipeGeneration:  materializeInput.Query.DocumentGeneration,
			BeforeGeneration:  materializeInput.Query.DocumentGeneration,
			AfterGeneration:   materializeInput.Query.DocumentGeneration,
			BeforeQueryResult: &input,
			AfterQueryResult:  &input,
		},
	}
	mappedDiff, err := mapMaterializeViewInput(diffInput)
	if err != nil || mappedDiff.Diff == nil || mappedDiff.Query != nil || mappedDiff.Diff.BeforeQueryResult == nil || mappedDiff.Diff.AfterQueryResult == nil {
		t.Fatalf("diff source mapping = %+v err=%v", mappedDiff, err)
	}
	for name, candidate := range map[string]engineprotocol.MaterializeViewInput{
		"query missing member": {Kind: "query", Limits: materializeInput.Limits, ViewAddress: materializeInput.ViewAddress},
		"query with diff":      {Kind: "query", Limits: materializeInput.Limits, ViewAddress: materializeInput.ViewAddress, Query: materializeInput.Query, Diff: diffInput.Diff},
		"diff missing member":  {Kind: "diff", Limits: materializeInput.Limits, ViewAddress: materializeInput.ViewAddress},
		"diff with query":      {Kind: "diff", Limits: materializeInput.Limits, ViewAddress: materializeInput.ViewAddress, Query: materializeInput.Query, Diff: diffInput.Diff},
		"unknown kind":         {Kind: "unknown", Limits: materializeInput.Limits, ViewAddress: materializeInput.ViewAddress},
	} {
		if _, err := mapMaterializeViewInput(candidate); !engine.IsWorkbenchError(err, engine.WorkbenchErrorInputInvalid) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
	badGeneration := materializeInput
	badGenerationQuery := *materializeInput.Query
	badGeneration.Query = &badGenerationQuery
	badGeneration.Query.DocumentGeneration.Value = "not-a-generation"
	if _, err := mapMaterializeViewInput(badGeneration); err == nil {
		t.Fatal("malformed query generation was accepted")
	}
	badSnapshot := materializeInput
	badSnapshotQuery := *materializeInput.Query
	badSnapshot.Query = &badSnapshotQuery
	badSnapshotValue := "invalid"
	badSnapshot.Query.StateSnapshot = &semantic.StateQuerySnapshot{
		Format: semantic.StateQuerySnapshotFormatValue, SchemaVersion: 1,
		DefinitionProjectAddress: "ldl:project:p", DefinitionHash: definitionHash, GraphHash: graphHash,
		StateVersion: stateVersion, CapturedAt: capturedAt, InaccessibleFieldPaths: []semantic.StateFieldPath{},
		Subjects: []semantic.StateQuerySubject{{
			SubjectAddress: subject, OwnSubjectHash: snapshotHash, RedactedFieldPaths: []semantic.StateFieldPath{},
			Fields: map[string]semantic.RecipeScalar{"invalid": {Kind: "unsupported", StringValue: &badSnapshotValue}},
		}},
	}
	if _, err := mapMaterializeViewInput(badSnapshot); !engine.IsWorkbenchError(err, engine.WorkbenchErrorInputInvalid) {
		t.Fatalf("malformed state snapshot error = %v", err)
	}
	for name, mutate := range map[string]func(*engineprotocol.MaterializeDiffViewInput){
		"recipe generation": func(value *engineprotocol.MaterializeDiffViewInput) { value.RecipeGeneration.Value = "invalid" },
		"before generation": func(value *engineprotocol.MaterializeDiffViewInput) { value.BeforeGeneration.Value = "invalid" },
		"after generation":  func(value *engineprotocol.MaterializeDiffViewInput) { value.AfterGeneration.Value = "invalid" },
	} {
		candidate := *diffInput.Diff
		mutate(&candidate)
		if _, err := mapMaterializeDiffViewInput(candidate); err == nil {
			t.Fatalf("malformed %s was accepted", name)
		}
	}
	invalidQueryResult := input
	invalidQueryResult.Arguments = map[string]semantic.RecipeScalar{"invalid": {Kind: "unsupported"}}
	for name, mutate := range map[string]func(*engineprotocol.MaterializeDiffViewInput){
		"before query": func(value *engineprotocol.MaterializeDiffViewInput) { value.BeforeQueryResult = &invalidQueryResult },
		"after query":  func(value *engineprotocol.MaterializeDiffViewInput) { value.AfterQueryResult = &invalidQueryResult },
	} {
		candidate := *diffInput.Diff
		mutate(&candidate)
		if _, err := mapMaterializeDiffViewInput(candidate); !engine.IsWorkbenchError(err, engine.WorkbenchErrorInputInvalid) {
			t.Fatalf("malformed %s error = %v", name, err)
		}
	}
	invalid := input
	boolean := true
	invalid.Diagnostics = []semantic.Diagnostic{diagnostic}
	invalid.Diagnostics[0].Arguments = map[string]semantic.DiagnosticArgumentValue{"bad": {Kind: semantic.DiagnosticArgumentKindBoolean, BooleanValue: &boolean}}
	if _, err := queryResultFromProtocol(invalid); err == nil {
		t.Fatal("non-string diagnostic argument was accepted")
	}
	if _, err := mapMaterializeViewInput(engineprotocol.MaterializeViewInput{
		Kind: "query", Limits: engineprotocol.WorkbenchLimits{MaxItems: "1", MaxOutputBytes: "1"},
		Query: &engineprotocol.MaterializeQueryViewInput{
			DocumentGeneration: engineprotocol.DocumentGeneration{DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "engine-test", Value: "document_abcdefghijklmnop"}, Value: "1"},
			QueryResult:        invalid,
		},
		ViewAddress: "ldl:project:p:view:v",
	}); !engine.IsWorkbenchError(err, engine.WorkbenchErrorInputInvalid) {
		t.Fatalf("invalid materialize input error = %v", err)
	}
	packAddress := semantic.PackRootAddress("ldl:pack:publisher:schema")
	if _, err := sourceRangeFromProtocol(semantic.SourceRange{
		Origin: semantic.SourceOrigin{Kind: semantic.OriginKindPack, PackAddress: &packAddress}, ModulePath: "module.ldl", StartByte: "0", EndByte: "1",
	}); err != nil {
		t.Fatalf("pack source range mapping error = %v", err)
	}
	if got, err := protocolByteOffset(protocolcommon.CanonicalUint64("2147483647")); err != nil || got != math.MaxInt32 {
		t.Fatalf("portable source offset = %d err=%v", got, err)
	}
	if _, err := protocolByteOffset(protocolcommon.CanonicalUint64("2147483648")); err == nil {
		t.Fatal("unrepresentable source offset was accepted")
	}
	if got := materializeViewMappingError(nil); got != nil {
		t.Fatalf("nil mapping error = %v", got)
	}
	if got := materializeViewMappingError(context.Canceled); !errors.Is(got, context.Canceled) {
		t.Fatalf("cancellation mapping = %v", got)
	}
	workbenchErr := &engine.WorkbenchError{Code: "test", Category: engine.WorkbenchErrorInputInvalid}
	if got := materializeViewMappingError(workbenchErr); got != workbenchErr {
		t.Fatalf("workbench mapping changed error = %v", got)
	}
	if got := materializeViewMappingError(errors.New("unsafe")); !engine.IsWorkbenchError(got, engine.WorkbenchErrorInvariant) {
		t.Fatalf("unsafe mapping error = %v", got)
	}
}

func TestRunMaterializeViewRoutesMappedResultsAndErrors(t *testing.T) {
	t.Parallel()
	payload := engineprotocol.MaterializeViewInput{
		Kind:   "query",
		Limits: engineprotocol.WorkbenchLimits{MaxItems: "128", MaxOutputBytes: "65536"},
		Query: &engineprotocol.MaterializeQueryViewInput{
			DocumentGeneration: engineprotocol.DocumentGeneration{DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "engine-test", Value: "document_abcdefghijklmnop"}, Value: "1"},
			QueryResult: engineprotocol.QueryExecutionResultData{
				Arguments: map[string]semantic.RecipeScalar{}, CycleRefs: []engineprotocol.QueryCycleRef{}, Diagnostics: []semantic.Diagnostic{},
				InducedRelationAddresses: []semantic.RelationAddress{}, PathRelationAddresses: []semantic.RelationAddress{}, Paths: []engineprotocol.QueryPath{},
				PrimaryEntityAddresses: []semantic.EntityAddress{}, QueryAddress: "ldl:project:p:query:q", ReachedEntityAddresses: []semantic.EntityAddress{},
				SeedEntityAddresses: []semantic.EntityAddress{}, SelectedRelationAddresses: []semantic.RelationAddress{}, StateInput: engineprotocol.QueryStateInputRef{Kind: "none"},
				StatePolicy: "none", StateReads: []engineprotocol.QueryStateReadRef{}, SupportEntityAddresses: []semantic.EntityAddress{}, TraversedEntityAddresses: []semantic.EntityAddress{},
			},
		},
		ViewAddress: "ldl:project:p:view:v",
	}
	driver := newFakeWorkbenchDriver()
	result, blobs, err := runMaterializeView(payload)(context.Background(), driver, nil)
	if err != nil || blobs != nil {
		t.Fatalf("materialize runner result = %+v blobs=%v err=%v", result, blobs, err)
	}
	if mapped, ok := result.(engineprotocol.MaterializeViewResult); !ok || mapped.ViewData.Context == nil {
		t.Fatalf("materialize runner payload = %#v", result)
	}
	driver.err = context.DeadlineExceeded
	if _, _, err := runMaterializeView(payload)(context.Background(), driver, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("driver error = %v", err)
	}
	invalid := payload
	boolean := true
	invalidQuery := *payload.Query
	invalid.Query = &invalidQuery
	invalid.Query.QueryResult.Diagnostics = []semantic.Diagnostic{{
		Arguments: map[string]semantic.DiagnosticArgumentValue{"bad": {Kind: semantic.DiagnosticArgumentKindBoolean, BooleanValue: &boolean}},
		Code:      "LDL1601", MessageKey: "invalid_query", ProtocolVersion: 1, Related: []semantic.DiagnosticRelated{}, Severity: semantic.DiagnosticSeverityError,
	}}
	if _, _, err := runMaterializeView(invalid)(context.Background(), driver, nil); !engine.IsWorkbenchError(err, engine.WorkbenchErrorInputInvalid) {
		t.Fatalf("invalid input error = %v", err)
	}
}

func TestWorkbenchTerminalEnvelopeSupportsEveryOperation(t *testing.T) {
	failure := workbenchFailure(protocolcommon.ProtocolFailureCategoryInvariant, FailureWorkbenchInvalid, "execution_failed", "failed", false, nil)
	operations := []string{
		OperationOpenDocument,
		OperationReplaceSourceTree,
		OperationCloseDocument,
		OperationListModules,
		OperationReadModules,
		OperationFindSymbols,
		OperationInspectSubgraph,
		OperationReadDeclarations,
		OperationReadRows,
		OperationExecuteQuery,
		OperationMaterializeView,
		OperationGetNeighbors,
		OperationFindUsages,
		OperationReadScope,
		OperationListReferences,
		OperationReadReferences,
		OperationPreviewSourcePatch,
		OperationPreviewFragment,
		OperationFormatScope,
		OperationOrganizeWorkspace,
		OperationApplyToHandle,
	}
	for _, operation := range operations {
		t.Run(operation, func(t *testing.T) {
			control, err := encodeWorkbenchTerminal(operation, nil, []semantic.Diagnostic{}, failure, protocolcommon.OutcomeFailed, "0.0.0-dev", "terminal")
			if err != nil || len(control) == 0 {
				t.Fatalf("terminal control len=%d err=%v", len(control), err)
			}
		})
	}
	if _, err := encodeWorkbenchTerminal("engine.nope", nil, nil, failure, protocolcommon.OutcomeFailed, "0.0.0-dev", "bad"); err == nil {
		t.Fatal("unsupported operation encoded")
	}
	modulePath := engineprotocol.CanonicalSourcePath("document.ldl")
	groupAnchor := semantic.StableAddress("ldl:project:p:entity:alpha")
	placement := mapGeneratedPlacement(&engineprotocol.PlacementHint{Position: "after", ModulePath: &modulePath, GroupAnchorAddress: &groupAnchor})
	if placement == nil || placement.ModulePath != "document.ldl" || placement.GroupAnchorAddress != "ldl:project:p:entity:alpha" || placement.Position != "after" {
		t.Fatalf("placement = %+v", placement)
	}
}

func TestWorkbenchConversionCoversPrimitiveContainersAndErrors(t *testing.T) {
	var primitive struct {
		Name     string         `json:"name"`
		Count    int64          `json:"count"`
		Size     uint64         `json:"size"`
		Enabled  bool           `json:"enabled"`
		Items    []string       `json:"items"`
		Lookup   map[string]int `json:"lookup"`
		Optional *string        `json:"optional,omitempty"`
	}
	source := struct {
		Name     string         `json:"name"`
		Count    string         `json:"count"`
		Size     string         `json:"size"`
		Enabled  bool           `json:"enabled"`
		Items    []string       `json:"items"`
		Lookup   map[string]int `json:"lookup"`
		Optional string         `json:"optional"`
	}{
		Name: "x", Count: "42", Size: "7", Enabled: true, Items: []string{"a", "b"}, Lookup: map[string]int{"k": 1}, Optional: "set",
	}
	if err := convertStruct(source, &primitive); err != nil {
		t.Fatal(err)
	}
	if primitive.Count != 42 || primitive.Size != 7 || primitive.Optional == nil || *primitive.Optional != "set" || primitive.Lookup["k"] != 1 {
		t.Fatalf("primitive = %+v", primitive)
	}
	if err := convertStruct(source, primitive); err == nil {
		t.Fatal("non-pointer destination accepted")
	}
	var badInt struct {
		Count int64 `json:"count"`
	}
	if err := convertStruct(struct {
		Count string `json:"count"`
	}{Count: "nope"}, &badInt); err == nil {
		t.Fatal("invalid int string accepted")
	}
	var badUint struct {
		Size uint64 `json:"size"`
	}
	if err := convertStruct(struct {
		Size string `json:"size"`
	}{Size: "-1"}, &badUint); err == nil {
		t.Fatal("invalid uint string accepted")
	}
	var unsupported struct {
		Count int64 `json:"count"`
	}
	if err := convertStruct(struct {
		Count []string `json:"count"`
	}{Count: []string{"x"}}, &unsupported); err == nil {
		t.Fatal("unsupported conversion accepted")
	}
	if _, ok := stringFromField(reflectZero()); ok {
		t.Fatal("invalid string field was accepted")
	}
	if _, ok := uintStringFromField(reflectZero()); ok {
		t.Fatal("invalid uint field was accepted")
	}
	if _, ok := boolFromField(reflectZero()); ok {
		t.Fatal("invalid bool field was accepted")
	}
}

func reflectZero() reflect.Value {
	return reflect.Value{}
}

func canonicalJSONForTest(t *testing.T, value any) []byte {
	t.Helper()
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		t.Fatal(err)
	}
	return bytes.TrimSuffix(output.Bytes(), []byte{'\n'})
}

func TestEngineCompileDriverWorkbenchForwardersAreReachable(t *testing.T) {
	driver := engineCompileDriver{compiler: engine.New(engine.BuildInfo{})}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := driver.ReplaceSourceTree(cancelled, engine.ReplaceSourceTreeInput{}); err == nil {
		t.Fatal("replace forwarder returned nil error for cancelled empty input")
	}
	if _, err := driver.PreviewSourcePatch(cancelled, engine.PreviewSourcePatchInput{}); err == nil {
		t.Fatal("preview source patch forwarder returned nil error")
	}
	if _, err := driver.PreviewFragment(cancelled, engine.PreviewFragmentInput{}); err == nil {
		t.Fatal("preview fragment forwarder returned nil error")
	}
	if _, err := driver.FormatScope(cancelled, engine.FormatScopeInput{}); err == nil {
		t.Fatal("format scope forwarder returned nil error")
	}
	if _, err := driver.OrganizeWorkspace(cancelled, engine.OrganizeWorkspaceInput{}); err == nil {
		t.Fatal("organize workspace forwarder returned nil error")
	}
	if _, err := driver.PlanExport(cancelled, engine.ExportPlanInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("plan export forwarder error = %v", err)
	}

	dispatcher := NewCompileDispatcher(engine.New(engine.BuildInfo{}))
	for _, reason := range []CompileTransportFailure{CompileTransportProtocolViolation, CompileTransportDuplicateRequest, CompileTransportResourceLimit} {
		response, err := dispatcher.DispatchTransportFailureResponse(OperationCompile, "compile-transport", "0.0.0-dev", reason)
		if err != nil || response.Outcome != protocolcommon.OutcomeFailed {
			t.Fatalf("compile transport reason=%v response=%+v err=%v", reason, response, err)
		}
		decoded, err := engineprotocol.DecodeCompileResponseEnvelope(response.Control)
		if err != nil || decoded.Failure == nil {
			t.Fatalf("compile transport decoded=%+v err=%v", decoded, err)
		}
	}
}

func TestWorkbenchDispatchDefensiveBranches(t *testing.T) {
	var nilDispatcher *CompileDispatcher
	if _, _, err := nilDispatcher.PrepareDispatch(context.Background(), compileContext(t), OperationCompile, []byte("{}")); err == nil {
		t.Fatal("nil dispatcher PrepareDispatch accepted")
	}
	if _, _, err := NewCompileDispatcher(engine.New(engine.BuildInfo{})).PrepareDispatch(nil, compileContext(t), OperationCompile, []byte("{}")); err == nil {
		t.Fatal("nil context PrepareDispatch accepted")
	}
	dispatcher := newCompileDispatcher(newFakeWorkbenchDriver())
	negotiated := compileContext(t)
	if _, _, err := dispatcher.PrepareDispatch(context.Background(), negotiated, "engine.unknown", []byte("{}")); err == nil {
		t.Fatal("unknown workbench operation accepted")
	}
	if _, _, err := dispatcher.PrepareDispatch(context.Background(), negotiated, OperationListModules, []byte("{")); err == nil {
		t.Fatal("invalid workbench control accepted")
	}
	if _, _, err := dispatcher.PrepareDispatch(context.Background(), negotiated, OperationListModules, []byte(`{"operation":"engine.read_modules","request_id":"bad","protocol":{"name":"engine","version":"1.0"},"payload":{}}`)); err == nil {
		t.Fatal("mismatched workbench operation accepted")
	}
	if _, _, err := dispatcher.PrepareDispatch(context.Background(), negotiated, OperationListModules, []byte(`{"operation":"engine.list_modules","request_id":"","protocol":{"name":"engine","version":"1.0"},"payload":{}}`)); err == nil {
		t.Fatal("empty request id accepted")
	}
	var nilPlan *workbenchPlan
	if got := nilPlan.BlobRequirements(); got != nil {
		t.Fatalf("nil requirements = %+v", got)
	}
	if got := nilPlan.AdmissionBudget(); got.RequiredBlobCount != 0 {
		t.Fatalf("nil budget = %+v", got)
	}
	nilPlan.Abort()
	if _, err := nilPlan.ExecuteDispatch(context.Background(), &memoryBlobSource{}, &memoryBlobSink{}); err == nil {
		t.Fatal("nil plan executed")
	}
	plan := &workbenchPlan{}
	if _, err := plan.ExecuteDispatch(context.Background(), nil, &memoryBlobSink{}); err == nil {
		t.Fatal("invalid plan args accepted")
	}
	if _, err := plan.Execute(context.Background(), &memoryBlobSource{}, &memoryBlobSink{}); err == nil {
		t.Fatal("workbench Execute accepted")
	}
}

func mustEncode(t *testing.T, value []byte, err error) []byte {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func encodePreviewSourcePatchForTest(t *testing.T, value engineprotocol.PreviewSourcePatchRequestEnvelope) []byte {
	t.Helper()
	encoded, err := engineprotocol.EncodePreviewSourcePatchRequestEnvelope(value)
	return mustEncode(t, encoded, err)
}

func encodePreviewFragmentForTest(t *testing.T, value engineprotocol.PreviewFragmentRequestEnvelope) []byte {
	t.Helper()
	encoded, err := engineprotocol.EncodePreviewFragmentRequestEnvelope(value)
	return mustEncode(t, encoded, err)
}

func encodeFormatScopeForTest(t *testing.T, value engineprotocol.FormatScopeRequestEnvelope) []byte {
	t.Helper()
	encoded, err := engineprotocol.EncodeFormatScopeRequestEnvelope(value)
	return mustEncode(t, encoded, err)
}

func encodeOrganizeWorkspaceForTest(t *testing.T, value engineprotocol.OrganizeWorkspaceRequestEnvelope) []byte {
	t.Helper()
	encoded, err := engineprotocol.EncodeOrganizeWorkspaceRequestEnvelope(value)
	return mustEncode(t, encoded, err)
}

func encodeApplyToHandleForTest(t *testing.T, value engineprotocol.ApplyToHandleRequestEnvelope) []byte {
	t.Helper()
	encoded, err := engineprotocol.EncodeApplyToHandleRequestEnvelope(value)
	return mustEncode(t, encoded, err)
}

func encodeListModulesForTest(t *testing.T, value engineprotocol.ListModulesRequestEnvelope) []byte {
	t.Helper()
	encoded, err := engineprotocol.EncodeListModulesRequestEnvelope(value)
	return mustEncode(t, encoded, err)
}

func fakeSourcePlan(bytes []byte) sourceplanner.SourcePlan {
	digest := sourceDigest(bytes)
	blob := sourceplanner.BlobRef{
		BlobID: "replacement", Digest: sourceplanner.Digest(digest), Lifetime: sourceplanner.BlobLifetimeRequest,
		MediaType: "text/plain; charset=utf-8", Size: uint64(len(bytes)),
	}
	emptyDigest := sourceplanner.Digest("sha256:" + hex.EncodeToString(make([]byte, sha256.Size)))
	beforeDigest := sourceplanner.Digest(sourceDigest([]byte("before")))
	sourceRange := sourceplanner.SourceRange{
		Origin: sourceplanner.SourceOrigin{Kind: "project"}, ModulePath: "document.ldl", StartByte: 0, EndByte: 6,
	}
	impact := sourceplanner.AuthoringImpact{
		BaseDefinitionHash: emptyDigest, Entries: []sourceplanner.AuthoringImpactEntry{}, ImpactDigest: emptyDigest,
		RequiredCapabilities: []sourceplanner.AuthoringCapability{}, ResultingDefinitionHash: emptyDigest,
		SemanticDiffHash: emptyDigest, SourceDiffHash: sourceplanner.Digest(digest),
	}
	capabilities := []sourceplanner.AuthoringCapability{}
	previewDigest := sourceplanner.Digest(digest)
	previewID := sourceplanner.PreviewID{Namespace: "engine-test", Value: "preview_1234567890abcdef"}
	proposedGeneration := sourceplanner.Generation{Namespace: "engine-test", DocumentID: "document_1234567890abcdef", Value: 8}
	projectAddress := sourceplanner.ProjectRootAddress("ldl:project:p")
	graphHash := emptyDigest
	resultingHashes := sourceplanner.ResultingHashes{
		ChildSetHashes: []sourceplanner.ChildSetHash{}, DefinitionHash: emptyDigest, GraphHash: &graphHash,
		Mode: sourceplanner.CompileProject, ProjectAddress: &projectAddress,
		SubjectHashes: []sourceplanner.SubjectHash{}, SubtreeHashes: []sourceplanner.SubtreeHash{},
	}
	return sourceplanner.SourcePlan{
		Preview: sourceplanner.WorkbenchPreviewResult{
			AuthoringImpact:               &impact,
			AuthoringImpactDigest:         &emptyDigest,
			BaseGeneration:                sourceplanner.Generation{Namespace: "engine-test", DocumentID: "document_1234567890abcdef", Value: 7},
			ChangedSourceFiles:            []sourceplanner.ModuleRef{},
			Conflicts:                     []sourceplanner.SemanticConflict{},
			Diagnostics:                   []sourceplanner.Diagnostic{},
			PreviewDigest:                 &previewDigest,
			PreviewID:                     &previewID,
			ProposedGeneration:            &proposedGeneration,
			RequiredAuthoringCapabilities: &capabilities,
			ResultingHashes:               &resultingHashes,
			SemanticDiff:                  sourceplanner.SemanticDiff{Digest: emptyDigest, Entries: []sourceplanner.SemanticDiffEntry{}},
			SourceDiff: sourceplanner.SourceDiff{Digest: sourceplanner.Digest(digest), Edits: []sourceplanner.SourceEdit{{
				Kind: sourceplanner.SourceEditKindReplace, BeforeDigest: &beforeDigest, AfterDigest: &blob.Digest, ReplacementBlob: &blob, SourceRange: &sourceRange,
			}}},
			Status: "valid",
		},
		Attachments: sourceplanner.PlannerBlobs{"replacement": bytes},
	}
}

func sourceDigest(bytes []byte) protocolcommon.Digest {
	sum := sha256.Sum256(bytes)
	return protocolcommon.Digest("sha256:" + hex.EncodeToString(sum[:]))
}
