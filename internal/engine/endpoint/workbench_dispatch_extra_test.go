// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/sourceplanner"
)

type fakeWorkbenchDriver struct {
	descriptor engine.Descriptor
	plan       sourceplanner.SourcePlan
	generation engine.DocumentGeneration
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

func TestWorkbenchExecutionErrorsBecomeRejectedDiagnostics(t *testing.T) {
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
	if err != nil || response.Outcome != protocolcommon.OutcomeRejected {
		t.Fatalf("dispatch = %+v err=%v", response, err)
	}
	decoded, err := engineprotocol.DecodeListModulesResponseEnvelope(response.Control)
	if err != nil || decoded.Payload != nil || len(decoded.Diagnostics) != 1 || decoded.Failure != nil {
		t.Fatalf("decoded = %+v err=%v", decoded, err)
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
