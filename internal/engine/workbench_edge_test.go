// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
)

type workbenchHookContext struct {
	context.Context
	hook func(compileStage)
}

func (c *workbenchHookContext) onCompileBoundary(stage compileStage) {
	c.hook(stage)
}

func requireWorkbenchCategory(t *testing.T, err error, category WorkbenchErrorCategory) {
	t.Helper()
	if !IsWorkbenchError(err, category) {
		t.Fatalf("error = %v, want category %s", err, category)
	}
}

func TestWorkbenchLifecycleBoundaryErrors(t *testing.T) {
	_, err := (Engine{}).OpenDocument(context.Background(), OpenDocumentInput{})
	requireWorkbenchCategory(t, err, WorkbenchErrorInvariant)

	badConfig := New(BuildInfo{Workbench: WorkbenchConfig{MaxDocuments: -1}})
	_, err = badConfig.OpenDocument(context.Background(), OpenDocumentInput{})
	requireWorkbenchCategory(t, err, WorkbenchErrorInvariant)
	badEndpoint := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "not allowed!"}})
	_, err = badEndpoint.OpenDocument(context.Background(), OpenDocumentInput{})
	requireWorkbenchCategory(t, err, WorkbenchErrorInvariant)
	badDepth := New(BuildInfo{Workbench: WorkbenchConfig{MaxDepth: maximumWorkbenchDepth + 1}})
	_, err = badDepth.OpenDocument(context.Background(), OpenDocumentInput{})
	requireWorkbenchCategory(t, err, WorkbenchErrorInvariant)

	instance := New(BuildInfo{})
	_, err = instance.OpenDocument(nil, OpenDocumentInput{})
	requireWorkbenchCategory(t, err, WorkbenchErrorInvariant)
	_, err = instance.OpenDocument(context.Background(), OpenDocumentInput{RequestedLimits: WorkbenchLimits{}})
	requireWorkbenchCategory(t, err, WorkbenchErrorInputInvalid)
	_, err = instance.OpenDocument(context.Background(), OpenDocumentInput{
		RequestedLimits: generousWorkbenchLimits,
		CompileInput:    CompileInput{ResourceLimits: ResourceLimits{MaxProjectSourceBytes: -1}},
	})
	requireWorkbenchCategory(t, err, WorkbenchErrorLimitExceeded)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = instance.OpenDocument(cancelled, OpenDocumentInput{RequestedLimits: generousWorkbenchLimits})
	requireWorkbenchCategory(t, err, WorkbenchErrorCancelled)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled error does not unwrap context.Canceled: %v", err)
	}

	tiny := New(BuildInfo{Workbench: WorkbenchConfig{MaxRetainedBytes: 1}})
	_, err = tiny.OpenDocument(context.Background(), OpenDocumentInput{RequestedLimits: generousWorkbenchLimits, CompileInput: projectCompileInput(`project p "P" {}`)})
	requireWorkbenchCategory(t, err, WorkbenchErrorLimitExceeded)

	opened := openWorkbench(t, instance, projectCompileInput(`project p "P" {}`))
	zeroGeneration := opened.DocumentGeneration
	zeroGeneration.Value = 0
	_, _, err = instance.acquireSnapshot(context.Background(), zeroGeneration)
	requireWorkbenchCategory(t, err, WorkbenchErrorGenerationStale)

	_, err = instance.CloseDocument(nil, CloseDocumentInput{})
	requireWorkbenchCategory(t, err, WorkbenchErrorInvariant)
	_, err = instance.CloseDocument(context.Background(), CloseDocumentInput{DocumentHandle: opened.DocumentHandle, DocumentGeneration: zeroGeneration})
	requireWorkbenchCategory(t, err, WorkbenchErrorHandleInvalid)
	mismatch := opened.DocumentHandle
	mismatch.Value += "x"
	_, err = instance.CloseDocument(context.Background(), CloseDocumentInput{DocumentHandle: mismatch, DocumentGeneration: opened.DocumentGeneration})
	requireWorkbenchCategory(t, err, WorkbenchErrorHandleInvalid)
	stale := opened.DocumentGeneration
	stale.Value++
	_, err = instance.CloseDocument(context.Background(), CloseDocumentInput{DocumentHandle: opened.DocumentHandle, DocumentGeneration: stale})
	requireWorkbenchCategory(t, err, WorkbenchErrorGenerationStale)

	instance.workbench.mu.Lock()
	instance.workbench.documents[opened.DocumentHandle.Value].generation = math.MaxUint64
	instance.workbench.mu.Unlock()
	overflow := opened.DocumentGeneration
	overflow.Value = math.MaxUint64
	_, err = instance.ReplaceSourceTree(context.Background(), ReplaceSourceTreeInput{ExpectedGeneration: overflow, CompileInput: projectCompileInput(`project q "Q" {}`)})
	requireWorkbenchCategory(t, err, WorkbenchErrorInvariant)
}

func TestWorkbenchReadRejectionMatrix(t *testing.T) {
	instance := New(BuildInfo{})
	degraded := openWorkbench(t, instance, projectCompileInput("project p"))
	valid := openWorkbench(t, instance, projectCompileInput(allDeclarationsFixture))

	semanticCalls := []func() error{
		func() error {
			_, err := instance.FindSymbols(context.Background(), FindSymbolsInput{DocumentGeneration: degraded.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.ReadDeclarations(context.Background(), ReadDeclarationsInput{DocumentGeneration: degraded.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.ReadRows(context.Background(), ReadRowsInput{DocumentGeneration: degraded.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.FindUsages(context.Background(), FindUsagesInput{DocumentGeneration: degraded.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.GetNeighbors(context.Background(), GetNeighborsInput{DocumentGeneration: degraded.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.InspectSubgraph(context.Background(), InspectSubgraphInput{DocumentGeneration: degraded.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.ReadScope(context.Background(), ReadScopeInput{DocumentGeneration: degraded.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.ListReferences(context.Background(), ListReferencesInput{DocumentGeneration: degraded.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.ReadReferences(context.Background(), ReadReferencesInput{DocumentGeneration: degraded.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
	}
	for index, call := range semanticCalls {
		if err := call(); !IsWorkbenchError(err, WorkbenchErrorOperationDisabled) {
			t.Fatalf("disabled call %d error = %v", index, err)
		}
	}

	zeroLimits := WorkbenchLimits{}
	limitCalls := []func() error{
		func() error {
			_, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: valid.DocumentGeneration, Limits: zeroLimits})
			return err
		},
		func() error {
			_, err := instance.FindSymbols(context.Background(), FindSymbolsInput{DocumentGeneration: valid.DocumentGeneration, Limits: zeroLimits})
			return err
		},
		func() error {
			_, err := instance.ReadDeclarations(context.Background(), ReadDeclarationsInput{DocumentGeneration: valid.DocumentGeneration, Limits: zeroLimits})
			return err
		},
		func() error {
			_, err := instance.ReadRows(context.Background(), ReadRowsInput{DocumentGeneration: valid.DocumentGeneration, Limits: zeroLimits})
			return err
		},
		func() error {
			_, err := instance.FindUsages(context.Background(), FindUsagesInput{DocumentGeneration: valid.DocumentGeneration, Limits: zeroLimits})
			return err
		},
		func() error {
			_, err := instance.GetNeighbors(context.Background(), GetNeighborsInput{DocumentGeneration: valid.DocumentGeneration, Limits: zeroLimits})
			return err
		},
		func() error {
			_, err := instance.InspectSubgraph(context.Background(), InspectSubgraphInput{DocumentGeneration: valid.DocumentGeneration, Limits: zeroLimits})
			return err
		},
		func() error {
			_, err := instance.ReadScope(context.Background(), ReadScopeInput{DocumentGeneration: valid.DocumentGeneration, Limits: zeroLimits})
			return err
		},
		func() error {
			_, err := instance.ListReferences(context.Background(), ListReferencesInput{DocumentGeneration: valid.DocumentGeneration, Limits: zeroLimits})
			return err
		},
		func() error {
			_, err := instance.ReadReferences(context.Background(), ReadReferencesInput{DocumentGeneration: valid.DocumentGeneration, Limits: zeroLimits})
			return err
		},
	}
	for index, call := range limitCalls {
		if err := call(); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
			t.Fatalf("limit call %d error = %v", index, err)
		}
	}

	invalidCalls := []func() error{
		func() error {
			_, err := instance.FindSymbols(context.Background(), FindSymbolsInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits, Query: "", MatchMode: "exact", CaseMode: "sensitive"})
			return err
		},
		func() error {
			_, err := instance.FindSymbols(context.Background(), FindSymbolsInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits, Query: "a", MatchMode: "bad", CaseMode: "sensitive"})
			return err
		},
		func() error {
			_, err := instance.FindSymbols(context.Background(), FindSymbolsInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits, Query: "a", MatchMode: "exact", CaseMode: "bad"})
			return err
		},
		func() error {
			_, err := instance.FindSymbols(context.Background(), FindSymbolsInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits, Query: string([]byte{0xff}), MatchMode: "exact", CaseMode: "sensitive"})
			return err
		},
		func() error {
			_, err := instance.FindSymbols(context.Background(), FindSymbolsInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits, Query: "a", MatchMode: "exact", CaseMode: "sensitive", OwnerAddresses: []string{}})
			return err
		},
		func() error {
			_, err := instance.FindSymbols(context.Background(), FindSymbolsInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits, Query: "a", MatchMode: "exact", CaseMode: "sensitive", SubjectKinds: []SemanticSubjectKind{"unknown"}})
			return err
		},
		func() error {
			_, err := instance.ReadDeclarations(context.Background(), ReadDeclarationsInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.ReadRows(context.Background(), ReadRowsInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.FindUsages(context.Background(), FindUsagesInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.GetNeighbors(context.Background(), GetNeighborsInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.InspectSubgraph(context.Background(), InspectSubgraphInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.ReadScope(context.Background(), ReadScopeInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.ReadReferences(context.Background(), ReadReferencesInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
	}
	for index, call := range invalidCalls {
		if err := call(); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
			t.Fatalf("invalid call %d error = %v", index, err)
		}
	}

	unicodeQuery, err := instance.FindSymbols(context.Background(), FindSymbolsInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits, Query: strings.Repeat("界", 1024), MatchMode: "exact", CaseMode: "sensitive"})
	if err != nil || len(unicodeQuery.Items) != 0 {
		t.Fatalf("1024-scalar query = %+v, %v", unicodeQuery, err)
	}
	canonicalAddresses := []string{"ldl:project:p:relation-type:link", "ldl:project:p:layer:app"}
	canonicalDeclarations, err := instance.ReadDeclarations(context.Background(), ReadDeclarationsInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits, Addresses: canonicalAddresses})
	if err != nil || len(canonicalDeclarations.Items) != 2 {
		t.Fatalf("structured canonical address order = %+v, %v", canonicalDeclarations, err)
	}

	missing := "ldl:project:p:entity:missing"
	notFoundCalls := []func() error{
		func() error {
			_, err := instance.ReadDeclarations(context.Background(), ReadDeclarationsInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits, Addresses: []string{missing}})
			return err
		},
		func() error {
			_, err := instance.ReadRows(context.Background(), ReadRowsInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits, OwnerAddresses: []string{missing}})
			return err
		},
		func() error {
			_, err := instance.GetNeighbors(context.Background(), GetNeighborsInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits, EntityAddresses: []string{missing}, Direction: "both", Depth: 1})
			return err
		},
		func() error {
			_, err := instance.InspectSubgraph(context.Background(), InspectSubgraphInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits, RootAddresses: []string{missing}})
			return err
		},
		func() error {
			_, err := instance.ReadScope(context.Background(), ReadScopeInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits, OwnerAddress: missing})
			return err
		},
		func() error {
			_, err := instance.ReadReferences(context.Background(), ReadReferencesInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits, Addresses: []string{missing}})
			return err
		},
	}
	for index, call := range notFoundCalls {
		if err := call(); !IsWorkbenchError(err, WorkbenchErrorNotFound) {
			t.Fatalf("not-found call %d error = %v", index, err)
		}
	}

	closed, err := instance.CloseDocument(context.Background(), CloseDocumentInput{DocumentHandle: valid.DocumentHandle, DocumentGeneration: valid.DocumentGeneration})
	if err != nil || !closed.Closed {
		t.Fatalf("CloseDocument() = %+v, %v", closed, err)
	}
	closedCalls := []func() error{
		func() error {
			_, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.FindSymbols(context.Background(), FindSymbolsInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.ReadDeclarations(context.Background(), ReadDeclarationsInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.ReadRows(context.Background(), ReadRowsInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.FindUsages(context.Background(), FindUsagesInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.GetNeighbors(context.Background(), GetNeighborsInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.InspectSubgraph(context.Background(), InspectSubgraphInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.ReadScope(context.Background(), ReadScopeInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.ListReferences(context.Background(), ListReferencesInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
		func() error {
			_, err := instance.ReadReferences(context.Background(), ReadReferencesInput{DocumentGeneration: valid.DocumentGeneration, Limits: generousWorkbenchLimits})
			return err
		},
	}
	for index, call := range closedCalls {
		if err := call(); !IsWorkbenchError(err, WorkbenchErrorHandleInvalid) {
			t.Fatalf("closed call %d error = %v", index, err)
		}
	}
}

func TestWorkbenchPackStateAndReads(t *testing.T) {
	instance := New(BuildInfo{})
	opened := openWorkbench(t, instance, rootPackInput())
	if opened.State.StateKind != "pack_available" || opened.State.PackAddress == nil || opened.State.GraphHash != nil || opened.Capabilities.ReadRows || opened.Capabilities.GetNeighbors {
		t.Fatalf("pack state/capabilities = %+v / %+v", opened.State, opened.Capabilities)
	}
	modules, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits})
	if err != nil || len(modules.Items) == 0 {
		t.Fatalf("ListModules(pack) = %+v, %v", modules, err)
	}
	symbols, err := instance.FindSymbols(context.Background(), FindSymbolsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, Query: "service", MatchMode: "substring", CaseMode: "sensitive", SubjectKinds: []SemanticSubjectKind{materialize.SubjectEntityType}})
	if err != nil || len(symbols.Items) == 0 {
		t.Fatalf("FindSymbols(pack) = %+v, %v", symbols, err)
	}
	if !strings.HasPrefix(symbols.Items[0].Address, "ldl:pack:") {
		t.Fatalf("pack symbol address = %q", symbols.Items[0].Address)
	}
}

func TestWorkbenchRetainedByteEvictionAndCloseReplaceRace(t *testing.T) {
	input := projectCompileInput(`project p "P" {}`)
	probe := New(BuildInfo{})
	probeOpened := openWorkbench(t, probe, input)
	_, probeSnapshot, err := probe.acquireSnapshot(context.Background(), probeOpened.DocumentGeneration)
	if err != nil || probeSnapshot.retained <= 0 {
		t.Fatalf("probe retained bytes = %d, %v", probeSnapshot.retained, err)
	}

	byteBounded := New(BuildInfo{Workbench: WorkbenchConfig{MaxRetainedBytes: probeSnapshot.retained + probeSnapshot.retained/2}})
	first := openWorkbench(t, byteBounded, input)
	second := openWorkbench(t, byteBounded, input)
	if _, err := byteBounded.ListModules(context.Background(), ListModulesInput{DocumentGeneration: first.DocumentGeneration, Limits: generousWorkbenchLimits}); !IsWorkbenchError(err, WorkbenchErrorHandleInvalid) {
		t.Fatalf("retained-byte evicted handle error = %v", err)
	}
	if _, err := byteBounded.ListModules(context.Background(), ListModulesInput{DocumentGeneration: second.DocumentGeneration, Limits: generousWorkbenchLimits}); err != nil {
		t.Fatalf("protected retained-byte handle error = %v", err)
	}

	tracing := New(BuildInfo{})
	opened := openWorkbench(t, tracing, input)
	var once sync.Once
	var closeErr error
	hookContext := &workbenchHookContext{Context: context.Background(), hook: func(stage compileStage) {
		if stage != stageComplete {
			return
		}
		once.Do(func() {
			_, closeErr = tracing.CloseDocument(context.Background(), CloseDocumentInput{DocumentHandle: opened.DocumentHandle, DocumentGeneration: opened.DocumentGeneration})
		})
	}}
	_, err = tracing.ReplaceSourceTree(hookContext, ReplaceSourceTreeInput{ExpectedGeneration: opened.DocumentGeneration, CompileInput: projectCompileInput(`project q "Q" {}`)})
	if closeErr != nil {
		t.Fatalf("racing close error = %v", closeErr)
	}
	requireWorkbenchCategory(t, err, WorkbenchErrorHandleInvalid)
	if _, err := tracing.ListModules(context.Background(), ListModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits}); !IsWorkbenchError(err, WorkbenchErrorHandleInvalid) {
		t.Fatalf("closed generation survived racing replacement: %v", err)
	}
}
