// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

const (
	workbenchProject   = "ldl:project:p"
	workbenchAlpha     = "ldl:project:p:entity:alpha"
	workbenchBeta      = "ldl:project:p:entity:beta"
	workbenchRelation  = "ldl:project:p:relation:alpha_beta"
	workbenchReference = "ldl:project:p:reference:guide"
	workbenchAlphaRow  = "ldl:project:p:entity:alpha:row:primary"
)

var generousWorkbenchLimits = WorkbenchLimits{MaxItems: 1_000, MaxOutputBytes: 1 << 20}

func openWorkbench(t *testing.T, instance Engine, input CompileInput) OpenDocumentResult {
	t.Helper()
	result, err := instance.OpenDocument(context.Background(), OpenDocumentInput{CompileInput: input, RequestedLimits: generousWorkbenchLimits})
	if err != nil {
		t.Fatalf("OpenDocument() error = %v", err)
	}
	return result
}

func TestWorkbenchOpenAndCanonicalIndexedReads(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "workbench-test"}})
	opened := openWorkbench(t, instance, projectCompileInput(allDeclarationsFixture))
	if opened.DocumentHandle.EndpointInstanceID != "workbench-test" || !strings.HasPrefix(opened.DocumentHandle.Value, documentHandlePrefix) || opened.DocumentGeneration.Value != 1 {
		t.Fatalf("unexpected handle/generation: %+v", opened)
	}
	if opened.State.StateKind != "project_available" || opened.State.ProjectAddress == nil || *opened.State.ProjectAddress != workbenchProject || opened.State.DefinitionHash == nil || opened.State.GraphHash == nil {
		t.Fatalf("incomplete state: %+v", opened.State)
	}
	if !opened.Capabilities.ListModules || !opened.Capabilities.ReadModules || !opened.Capabilities.FindSymbols || !opened.Capabilities.FindUsages || !opened.Capabilities.GetNeighbors || !opened.Capabilities.InspectSubgraph || !opened.Capabilities.ReadDeclarations || !opened.Capabilities.ReadRows || !opened.Capabilities.ReadScope || !opened.Capabilities.ListReferences || !opened.Capabilities.ReadReferences || !opened.Capabilities.ReplaceSourceTree {
		t.Fatalf("read capability missing: %+v", opened.Capabilities)
	}
	if !opened.Capabilities.ApplyToHandle || !opened.Capabilities.PreviewOperations {
		t.Fatalf("unexpected operation edit capability state: %+v", opened.Capabilities)
	}
	if !opened.Capabilities.PreviewFragment || !opened.Capabilities.PreviewSourcePatch || !opened.Capabilities.FormatScope || !opened.Capabilities.OrganizeWorkspace {
		t.Fatalf("source planning capability missing: %+v", opened.Capabilities)
	}

	modules, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits})
	if err != nil || len(modules.Items) != 1 || modules.Items[0].Module.ModulePath != "document.ldl" || modules.Items[0].Digest == "" || modules.Page.Truncation != TruncationComplete {
		t.Fatalf("ListModules() = %+v, %v", modules, err)
	}

	symbols, err := instance.FindSymbols(context.Background(), FindSymbolsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, Query: "ALPHA", MatchMode: "exact", CaseMode: "unicode_simple_fold"})
	if err != nil || len(symbols.Items) != 1 || symbols.Items[0].Address != workbenchAlpha || symbols.Items[0].MatchedField != "id" {
		t.Fatalf("FindSymbols() = %+v, %v", symbols, err)
	}

	declarations, err := instance.ReadDeclarations(context.Background(), ReadDeclarationsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, Addresses: []string{workbenchAlpha}})
	if err != nil || len(declarations.Items) != 1 || !bytes.Contains(declarations.Items[0].SourceChunk.Bytes, []byte(`alpha "Alpha"`)) {
		t.Fatalf("ReadDeclarations() = %+v, %v", declarations, err)
	}

	rows, err := instance.ReadRows(context.Background(), ReadRowsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, OwnerAddresses: []string{workbenchAlpha}})
	if err != nil || len(rows.Items) != 1 || rows.Items[0].RowAddress != workbenchAlphaRow || len(rows.Items[0].Values) != 2 {
		t.Fatalf("ReadRows() = %+v, %v", rows, err)
	}

	usages, err := instance.FindUsages(context.Background(), FindUsagesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, TargetAddresses: []string{workbenchAlpha}})
	if err != nil || len(usages.Items) == 0 {
		t.Fatalf("FindUsages() = %+v, %v", usages, err)
	}
	for _, usage := range usages.Items {
		if usage.TargetAddress != workbenchAlpha {
			t.Fatalf("usage escaped requested target: %+v", usage)
		}
	}

	neighbors, err := instance.GetNeighbors(context.Background(), GetNeighborsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, EntityAddresses: []string{workbenchAlpha}, Direction: "outgoing", Depth: 1})
	if err != nil || len(neighbors.Items) != 1 || neighbors.Items[0].EntityAddress != workbenchBeta || neighbors.Items[0].RelationAddress != workbenchRelation {
		t.Fatalf("GetNeighbors() = %+v, %v", neighbors, err)
	}

	subgraph, err := instance.InspectSubgraph(context.Background(), InspectSubgraphInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, RootAddresses: []string{workbenchAlpha}, Depth: 1})
	if err != nil || len(subgraph.Items) != 3 || len(subgraph.Relations) != 1 || subgraph.Relations[0].RelationAddress != workbenchRelation {
		t.Fatalf("InspectSubgraph() = %+v, %v", subgraph, err)
	}

	scope, err := instance.ReadScope(context.Background(), ReadScopeInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, OwnerAddress: workbenchAlpha})
	if err != nil || len(scope.Items) != 2 {
		t.Fatalf("ReadScope() = %+v, %v", scope, err)
	}

	references, err := instance.ListReferences(context.Background(), ListReferencesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits})
	if err != nil || len(references.Items) != 1 || references.Items[0].Address != workbenchReference {
		t.Fatalf("ListReferences() = %+v, %v", references, err)
	}
	content, err := instance.ReadReferences(context.Background(), ReadReferencesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, Addresses: []string{workbenchReference}})
	if err != nil || len(content.Items) != 1 || strings.TrimSpace(string(content.Items[0].TextChunk.Bytes)) != "Use the graph as the source of truth." {
		t.Fatalf("ReadReferences() = %+v, %v", content, err)
	}
}

func TestWorkbenchErrorTolerantOpenAndAtomicReplace(t *testing.T) {
	instance := New(BuildInfo{})
	invalid := projectCompileInput("project p")
	opened := openWorkbench(t, instance, invalid)
	if opened.State.SemanticState != "unavailable" || len(opened.State.Diagnostics) == 0 || !opened.Capabilities.ListModules || !opened.Capabilities.ReadModules || !opened.Capabilities.ReplaceSourceTree || opened.Capabilities.FindSymbols || opened.Capabilities.ReadDeclarations || opened.Capabilities.GetNeighbors {
		t.Fatalf("error-tolerant state/capabilities = %+v / %+v", opened.State, opened.Capabilities)
	}
	modules, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits})
	if err != nil || len(modules.Items) != 1 || modules.Items[0].Digest != digestBytesForWorkbench(invalid.ProjectSourceTree["document.ldl"]) {
		t.Fatalf("lossless module read = %+v, %v", modules, err)
	}
	_, err = instance.FindSymbols(context.Background(), FindSymbolsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, Query: "p", MatchMode: "exact", CaseMode: "sensitive"})
	if !IsWorkbenchError(err, WorkbenchErrorOperationDisabled) {
		t.Fatalf("semantic read on unavailable document error = %v", err)
	}

	replaced, err := instance.ReplaceSourceTree(context.Background(), ReplaceSourceTreeInput{ExpectedGeneration: opened.DocumentGeneration, CompileInput: projectCompileInput(allDeclarationsFixture)})
	if err != nil || replaced.DocumentGeneration.Value != 2 || replaced.State.SemanticState != "available" {
		t.Fatalf("ReplaceSourceTree() = %+v, %v", replaced, err)
	}
	if _, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits}); !IsWorkbenchError(err, WorkbenchErrorGenerationStale) {
		t.Fatalf("old generation error = %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := instance.ReplaceSourceTree(cancelled, ReplaceSourceTreeInput{ExpectedGeneration: replaced.DocumentGeneration, CompileInput: invalid}); !IsWorkbenchError(err, WorkbenchErrorCancelled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled replacement error = %v", err)
	}
	current, err := instance.FindSymbols(context.Background(), FindSymbolsInput{DocumentGeneration: replaced.DocumentGeneration, Limits: generousWorkbenchLimits, Query: "alpha", MatchMode: "exact", CaseMode: "sensitive"})
	if err != nil || len(current.Items) != 1 {
		t.Fatalf("cancelled replacement changed prior snapshot: %+v, %v", current, err)
	}
	resourceRejected := projectCompileInput(allDeclarationsFixture)
	resourceRejected.ResourceLimits.MaxProjectSourceBytes = 1
	if _, err := instance.ReplaceSourceTree(context.Background(), ReplaceSourceTreeInput{ExpectedGeneration: replaced.DocumentGeneration, CompileInput: resourceRejected}); !IsWorkbenchError(err, WorkbenchErrorLimitExceeded) {
		t.Fatalf("resource-rejected replacement error = %v", err)
	}
	current, err = instance.FindSymbols(context.Background(), FindSymbolsInput{DocumentGeneration: replaced.DocumentGeneration, Limits: generousWorkbenchLimits, Query: "alpha", MatchMode: "exact", CaseMode: "sensitive"})
	if err != nil || len(current.Items) != 1 {
		t.Fatalf("resource failure changed prior snapshot: %+v, %v", current, err)
	}
}

func TestWorkbenchErrorTolerantPackRetainsAllModules(t *testing.T) {
	instance := New(BuildInfo{})
	input := rootPackInput()
	input.InstalledPackTree["pack/schema/pack.ldl"] = []byte("pack broken")
	opened := openWorkbench(t, instance, input)
	if opened.State.StateKind != "pack_unavailable" || opened.State.SemanticState != "unavailable" || len(opened.State.Diagnostics) == 0 {
		t.Fatalf("invalid pack state = %+v", opened.State)
	}
	modules, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits})
	if err != nil || len(modules.Items) != 2 {
		t.Fatalf("invalid pack modules = %+v, %v", modules, err)
	}
	if modules.Items[0].Module.ModulePath != "manifest.json" || modules.Items[1].Module.ModulePath != "pack.ldl" {
		t.Fatalf("invalid pack module order = %+v", modules.Items)
	}
	if modules.Items[1].Digest != digestBytesForWorkbench(input.InstalledPackTree["pack/schema/pack.ldl"]) {
		t.Fatalf("invalid pack module digest = %q", modules.Items[1].Digest)
	}
}

func TestWorkbenchPaginationCursorBindingAndDefensiveCopies(t *testing.T) {
	instance := New(BuildInfo{})
	input := projectTreeCompileInput(map[string][]byte{
		"document.ldl": []byte("import { service } from \"./types.ldl\"\nproject p \"Project\" {}\n"),
		"types.ldl":    []byte("entity_type service \"Service\" {\n  representation shape rect\n}\nexport { service }\n"),
	})
	opened := openWorkbench(t, instance, input)
	pageLimits := WorkbenchLimits{MaxItems: 1, MaxOutputBytes: 8_000}
	first, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: pageLimits})
	if err != nil || len(first.Items) != 1 || first.Page.Truncation != TruncationItemLimit || first.Page.NextCursor == nil || first.Page.ReturnedBytes > pageLimits.MaxOutputBytes {
		t.Fatalf("first page = %+v, %v", first, err)
	}
	repeated, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: pageLimits})
	if err != nil || !reflect.DeepEqual(first, repeated) {
		t.Fatalf("pagination is not deterministic: first=%+v repeated=%+v err=%v", first, repeated, err)
	}
	second, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: pageLimits, Cursor: first.Page.NextCursor})
	if err != nil || len(second.Items) != 1 || second.Page.Truncation != TruncationComplete || second.Items[0].Module.ModulePath == first.Items[0].Module.ModulePath {
		t.Fatalf("second page = %+v, %v", second, err)
	}

	tampered := *first.Page.NextCursor
	tampered.Value += "x"
	if _, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: pageLimits, Cursor: &tampered}); !IsWorkbenchError(err, WorkbenchErrorCursorInvalid) {
		t.Fatalf("tampered cursor error = %v", err)
	}
	if _, err := instance.FindSymbols(context.Background(), FindSymbolsInput{DocumentGeneration: opened.DocumentGeneration, Limits: pageLimits, Cursor: first.Page.NextCursor, Query: "service", MatchMode: "exact", CaseMode: "sensitive"}); !IsWorkbenchError(err, WorkbenchErrorCursorInvalid) {
		t.Fatalf("cross-operation cursor error = %v", err)
	}

	declarationAddress := "ldl:project:p:entity-type:service"
	read, err := instance.ReadDeclarations(context.Background(), ReadDeclarationsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, Addresses: []string{declarationAddress}})
	if err != nil || len(read.Items) != 1 {
		t.Fatalf("ReadDeclarations() = %+v, %v", read, err)
	}
	original := append([]byte(nil), read.Items[0].SourceChunk.Bytes...)
	read.Items[0].SourceChunk.Bytes[0] ^= 0xff
	again, err := instance.ReadDeclarations(context.Background(), ReadDeclarationsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, Addresses: []string{declarationAddress}})
	if err != nil || !bytes.Equal(again.Items[0].SourceChunk.Bytes, original) {
		t.Fatalf("caller mutation reached retained snapshot: %+v, %v", again, err)
	}
	replaced, err := instance.ReplaceSourceTree(context.Background(), ReplaceSourceTreeInput{ExpectedGeneration: opened.DocumentGeneration, CompileInput: input})
	if err != nil {
		t.Fatalf("ReplaceSourceTree() error = %v", err)
	}
	if _, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: replaced.DocumentGeneration, Limits: pageLimits, Cursor: first.Page.NextCursor}); !IsWorkbenchError(err, WorkbenchErrorCursorInvalid) {
		t.Fatalf("prior-generation cursor error = %v", err)
	}
}

func TestWorkbenchTextChunkingHonorsUTF8AndOutputBudget(t *testing.T) {
	description := strings.Repeat("界", 1_000)
	source := "project p \"Project\" {}\nentity_type service \"Service\" {\n  description \"" + description + "\"\n  representation shape rect\n}\n"
	instance := New(BuildInfo{})
	opened := openWorkbench(t, instance, projectCompileInput(source))
	limits := WorkbenchLimits{MaxItems: 4, MaxOutputBytes: 1_400}
	address := "ldl:project:p:entity-type:service"
	var reconstructed []byte
	var cursor *Cursor
	for pages := 0; ; pages++ {
		if pages > 32 {
			t.Fatal("chunk pagination did not terminate")
		}
		result, err := instance.ReadDeclarations(context.Background(), ReadDeclarationsInput{DocumentGeneration: opened.DocumentGeneration, Limits: limits, Addresses: []string{address}, Cursor: cursor})
		if err != nil || len(result.Items) != 1 || result.Page.ReturnedBytes > limits.MaxOutputBytes || !utf8.Valid(result.Items[0].SourceChunk.Bytes) {
			t.Fatalf("chunk page = %+v, %v", result, err)
		}
		reconstructed = append(reconstructed, result.Items[0].SourceChunk.Bytes...)
		if result.Page.Truncation == TruncationComplete {
			break
		}
		if result.Page.Truncation != TruncationOutputByteLimit || result.Page.NextCursor == nil {
			t.Fatalf("invalid chunk continuation: %+v", result.Page)
		}
		cursor = result.Page.NextCursor
	}
	if !bytes.Contains(reconstructed, []byte(description)) {
		t.Fatalf("reconstructed declaration lost content: %d bytes", len(reconstructed))
	}
}

func TestWorkbenchCloseCapacityEvictionAndOpaqueHandles(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "endpoint-a", MaxDocuments: 1}})
	first := openWorkbench(t, instance, projectCompileInput(`project first "First" {}`))
	second := openWorkbench(t, instance, projectCompileInput(`project second "Second" {}`))
	if first.DocumentHandle == second.DocumentHandle {
		t.Fatal("handle collision")
	}
	if _, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: first.DocumentGeneration, Limits: generousWorkbenchLimits}); !IsWorkbenchError(err, WorkbenchErrorHandleInvalid) {
		t.Fatalf("evicted handle error = %v", err)
	}
	if result, err := instance.CloseDocument(context.Background(), CloseDocumentInput{DocumentHandle: first.DocumentHandle, DocumentGeneration: first.DocumentGeneration}); err != nil || !result.Closed {
		t.Fatalf("close evicted handle = %+v, %v", result, err)
	}

	wrongEndpoint := second.DocumentGeneration
	wrongEndpoint.DocumentHandle.EndpointInstanceID = "endpoint-b"
	if _, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: wrongEndpoint, Limits: generousWorkbenchLimits}); !IsWorkbenchError(err, WorkbenchErrorHandleInvalid) {
		t.Fatalf("wrong endpoint error = %v", err)
	}
	forged := second.DocumentGeneration
	forged.DocumentHandle.Value = documentHandlePrefix + strings.Repeat("A", 43)
	if _, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: forged, Limits: generousWorkbenchLimits}); !IsWorkbenchError(err, WorkbenchErrorHandleInvalid) {
		t.Fatalf("forged handle error = %v", err)
	}

	closed, err := instance.CloseDocument(context.Background(), CloseDocumentInput{DocumentHandle: second.DocumentHandle, DocumentGeneration: second.DocumentGeneration})
	if err != nil || !closed.Closed {
		t.Fatalf("first close = %+v, %v", closed, err)
	}
	again, err := instance.CloseDocument(context.Background(), CloseDocumentInput{DocumentHandle: second.DocumentHandle, DocumentGeneration: second.DocumentGeneration})
	if err != nil || !again.Closed {
		t.Fatalf("idempotent close = %+v, %v", again, err)
	}
	if _, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: second.DocumentGeneration, Limits: generousWorkbenchLimits}); !IsWorkbenchError(err, WorkbenchErrorHandleInvalid) {
		t.Fatalf("closed handle read error = %v", err)
	}
}

func TestWorkbenchReplaceCASAndConcurrentReaders(t *testing.T) {
	instance := New(BuildInfo{})
	opened := openWorkbench(t, instance, projectCompileInput(allDeclarationsFixture))
	start := make(chan struct{})
	var wait sync.WaitGroup
	errorsSeen := make(chan error, 18)
	for index := 0; index < 16; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, err := instance.FindSymbols(context.Background(), FindSymbolsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, Query: "alpha", MatchMode: "exact", CaseMode: "sensitive"})
			if err == nil && (len(result.Items) != 1 || result.Items[0].Address != workbenchAlpha) {
				err = errors.New("reader observed mixed generation")
			}
			if err != nil && !IsWorkbenchError(err, WorkbenchErrorGenerationStale) {
				errorsSeen <- err
			}
		}()
	}
	results := make(chan error, 2)
	for _, name := range []string{"replacement_a", "replacement_b"} {
		name := name
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := instance.ReplaceSourceTree(context.Background(), ReplaceSourceTreeInput{ExpectedGeneration: opened.DocumentGeneration, CompileInput: projectCompileInput("project " + name + " \"Replacement\" {}")})
			results <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errorsSeen)
	close(results)
	for err := range errorsSeen {
		t.Fatalf("concurrent reader: %v", err)
	}
	var successes, stale int
	for err := range results {
		if err == nil {
			successes++
		} else if IsWorkbenchError(err, WorkbenchErrorGenerationStale) {
			stale++
		} else {
			t.Fatalf("replacement error = %v", err)
		}
	}
	if successes != 1 || stale != 1 {
		t.Fatalf("CAS results successes=%d stale=%d", successes, stale)
	}
}

func TestWorkbenchRejectsMalformedInputsAndIndexRanges(t *testing.T) {
	instance := New(BuildInfo{})
	opened := openWorkbench(t, instance, projectCompileInput(allDeclarationsFixture))
	if _, err := instance.ReadRows(context.Background(), ReadRowsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, OwnerAddresses: []string{workbenchBeta, workbenchAlpha}}); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
		t.Fatalf("unordered owners error = %v", err)
	}
	if _, err := instance.GetNeighbors(context.Background(), GetNeighborsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, EntityAddresses: []string{workbenchAlpha}, Direction: "sideways", Depth: 1}); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
		t.Fatalf("invalid direction error = %v", err)
	}
	if _, err := instance.InspectSubgraph(context.Background(), InspectSubgraphInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, RootAddresses: []string{workbenchAlpha}, Depth: 33}); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
		t.Fatalf("invalid depth error = %v", err)
	}
	if _, err := instance.ReadDeclarations(context.Background(), ReadDeclarationsInput{DocumentGeneration: opened.DocumentGeneration, Limits: WorkbenchLimits{MaxItems: 1, MaxOutputBytes: generousWorkbenchLimits.MaxOutputBytes + 1}, Addresses: []string{workbenchAlpha}}); !IsWorkbenchError(err, WorkbenchErrorLimitExceeded) {
		t.Fatalf("broadened limit error = %v", err)
	}

	instance.workbench.mu.Lock()
	document := instance.workbench.documents[opened.DocumentHandle.Value]
	for index := range document.snapshot.compiled.SourceMap.Subjects {
		if document.snapshot.compiled.SourceMap.Subjects[index].Address == workbenchAlpha {
			document.snapshot.compiled.SourceMap.Subjects[index].DeclarationRange.EndByte = 1 << 30
		}
	}
	instance.workbench.mu.Unlock()
	if _, err := instance.ReadDeclarations(context.Background(), ReadDeclarationsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, Addresses: []string{workbenchAlpha}}); !IsWorkbenchError(err, WorkbenchErrorInvariant) {
		t.Fatalf("malformed retained range error = %v", err)
	}
}
