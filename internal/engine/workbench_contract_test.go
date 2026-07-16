// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
)

func TestReadModulesDegradedLosslessAndGeneratedWireBytes(t *testing.T) {
	for _, test := range []struct {
		source   string
		wantCode string
	}{
		{source: "project p", wantCode: "LDL1101"},
		{source: strings.Replace(allDeclarationsFixture, "types [service]", "types [missing]", 1), wantCode: "LDL1301"},
	} {
		instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "module-contract"}})
		input := projectCompileInput(test.source)
		opened := openWorkbench(t, instance, input)
		if opened.State.SemanticState != "unavailable" || !opened.Capabilities.ReadModules || opened.Capabilities.ReadDeclarations {
			t.Fatalf("degraded capabilities = %+v / %+v", opened.State, opened.Capabilities)
		}
		if len(opened.State.Diagnostics) == 0 || opened.State.Diagnostics[0].Code != test.wantCode {
			t.Fatalf("degraded diagnostics = %+v, want first code %s", opened.State.Diagnostics, test.wantCode)
		}
		module := ModuleRef{ModulePath: "document.ldl", Origin: SourceOrigin{Kind: "project"}}
		result, err := instance.ReadModules(context.Background(), ReadModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, Modules: []ModuleRef{module}})
		if err != nil || len(result.Items) != 1 || !bytes.Equal(result.Items[0].SourceChunk.Bytes, input.ProjectSourceTree["document.ldl"]) {
			t.Fatalf("ReadModules() = %+v, %v", result, err)
		}
		assertGeneratedReadModulesBytes(t, result)
	}
}

func TestReadRowsGeneratedWireBytes(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "row-contract"}})
	opened := openWorkbench(t, instance, projectCompileInput(allDeclarationsFixture))
	result, err := instance.ReadRows(context.Background(), ReadRowsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, OwnerAddresses: []string{workbenchAlpha}})
	if err != nil {
		t.Fatal(err)
	}
	wire := engineprotocol.ReadRowsResult{DocumentGeneration: mapDocumentGeneration(result.DocumentGeneration), Items: make([]engineprotocol.RowReadItem, 0, len(result.Items))}
	for _, item := range result.Items {
		mapped := engineprotocol.RowReadItem{OwnerAddress: semantic.StableAddress(item.OwnerAddress), RowAddress: semantic.StableAddress(item.RowAddress), Values: make([]engineprotocol.RowCell, 0, len(item.Values))}
		for _, cell := range item.Values {
			value := engineprotocol.SemanticOperationValue{}
			switch cell.Value.Type {
			case "string", "enum", "date", "datetime":
				text := cell.Value.String
				value.Kind, value.String = engineprotocol.SemanticOperationValueKindString, &text
			case "integer":
				integer := protocolcommon.CanonicalSafeInteger(strconv.FormatInt(cell.Value.Int, 10))
				value.Kind, value.Integer = engineprotocol.SemanticOperationValueKindInteger, &integer
			case "number":
				decimal := semantic.CanonicalFiniteDecimal(workbenchCanonicalBinary64(cell.Value.Float))
				value.Kind, value.Decimal = engineprotocol.SemanticOperationValueKindDecimal, &decimal
			case "boolean":
				boolean := cell.Value.Bool
				value.Kind, value.Boolean = engineprotocol.SemanticOperationValueKindBoolean, &boolean
			default:
				t.Fatalf("unsupported row scalar %q", cell.Value.Type)
			}
			mapped.Values = append(mapped.Values, engineprotocol.RowCell{ColumnAddress: semantic.ColumnAddress(cell.ColumnAddress), Value: value})
		}
		wire.Items = append(wire.Items, mapped)
	}
	wire.Page = engineprotocol.RowPageInfo{ReturnedBytes: engineprotocol.LogicalResponseByteCount(strconv.FormatInt(result.Page.ReturnedBytes, 10)), ReturnedItems: protocolcommon.CanonicalUint64(strconv.FormatInt(result.Page.ReturnedItems, 10)), Truncation: engineprotocol.TruncationOutcome(result.Page.Truncation)}
	if _, err := engineprotocol.EncodeReadRowsResult(wire); err != nil {
		t.Fatalf("generated ReadRowsResult rejected facade output: %v", err)
	}
}

func TestGeneratedReadModulesSelectionOrder(t *testing.T) {
	generation := mapDocumentGeneration(DocumentGeneration{DocumentHandle: DocumentHandle{EndpointInstanceID: "fixture-endpoint", Value: "document_abcdefghijklmnop"}, Value: 1})
	input := engineprotocol.ReadModulesInput{
		DocumentGeneration: generation,
		Limits:             engineprotocol.WorkbenchLimits{MaxItems: "2", MaxOutputBytes: "4096"},
		Modules: []semantic.ModuleRef{
			{ModulePath: "a.ldl", Origin: semantic.SourceOrigin{Kind: semantic.OriginKindProject}},
			{ModulePath: "z.ldl", Origin: semantic.SourceOrigin{Kind: semantic.OriginKindProject}},
		},
	}
	if _, err := engineprotocol.EncodeReadModulesInput(input); err != nil {
		t.Fatalf("canonical module selection rejected: %v", err)
	}
	input.Modules[0], input.Modules[1] = input.Modules[1], input.Modules[0]
	if _, err := engineprotocol.EncodeReadModulesInput(input); err == nil {
		t.Fatal("reverse module selection order accepted")
	}
}

func TestReadModulesChunkPaginationAndCanonicalSelection(t *testing.T) {
	source := "project p\n" + strings.Repeat("# bounded source <>&  \n", 256)
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "module-pages"}})
	opened := openWorkbench(t, instance, projectCompileInput(source))
	module := ModuleRef{ModulePath: "document.ldl", Origin: SourceOrigin{Kind: "project"}}
	limits := WorkbenchLimits{MaxItems: 1, MaxOutputBytes: 1_600}
	var cursor *Cursor
	var firstCursor *Cursor
	var restored []byte
	for pages := 0; ; pages++ {
		if pages > len(source) {
			t.Fatal("ReadModules cursor did not make progress")
		}
		result, err := instance.ReadModules(context.Background(), ReadModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: limits, Modules: []ModuleRef{module}, Cursor: cursor})
		if err != nil || len(result.Items) != 1 || result.Page.ReturnedBytes > limits.MaxOutputBytes {
			t.Fatalf("ReadModules(page %d) = %+v, %v", pages, result, err)
		}
		assertGeneratedReadModulesBytes(t, result)
		restored = append(restored, result.Items[0].SourceChunk.Bytes...)
		if result.Page.NextCursor == nil {
			break
		}
		if firstCursor == nil {
			copy := *result.Page.NextCursor
			firstCursor = &copy
		}
		cursor = result.Page.NextCursor
	}
	if !bytes.Equal(restored, []byte(source)) {
		t.Fatalf("restored source differs: got %d bytes, want %d", len(restored), len(source))
	}
	tampered := *firstCursor
	tampered.Value += "A"
	if _, err := instance.ReadModules(context.Background(), ReadModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: limits, Modules: []ModuleRef{module}, Cursor: &tampered}); !IsWorkbenchError(err, WorkbenchErrorCursorInvalid) {
		t.Fatalf("tampered module cursor error = %v", err)
	}

	bad := []ReadModulesInput{
		{DocumentGeneration: opened.DocumentGeneration, Limits: limits},
		{DocumentGeneration: opened.DocumentGeneration, Limits: limits, Modules: []ModuleRef{{ModulePath: "document.ldl", Origin: SourceOrigin{Kind: "pack"}}}},
		{DocumentGeneration: opened.DocumentGeneration, Limits: limits, Modules: []ModuleRef{module, module}},
		{DocumentGeneration: opened.DocumentGeneration, Limits: limits, Modules: []ModuleRef{{ModulePath: "../document.ldl", Origin: SourceOrigin{Kind: "project"}}}},
	}
	for index, input := range bad {
		if _, err := instance.ReadModules(context.Background(), input); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
			t.Fatalf("invalid selection %d error = %v", index, err)
		}
	}
}

func TestRetainedSnapshotCountsPreviouslyOmittedOwnedIndexes(t *testing.T) {
	instance := New(BuildInfo{})
	snapshot, err := instance.compileWorkingSnapshot(context.Background(), projectCompileInput(allDeclarationsFixture))
	if err != nil || len(snapshot.compiled.SearchDocuments) == 0 || len(snapshot.compiled.SearchDocuments[0].Fields) == 0 {
		t.Fatalf("compileWorkingSnapshot() = %+v, %v", snapshot, err)
	}
	before := retainedSnapshotBytes(snapshot)
	snapshot.compiled.SearchDocuments[0].Fields[0].Text += strings.Repeat("owned-index-data", 65_536)
	after := retainedSnapshotBytes(snapshot)
	if after-before < 900_000 {
		t.Fatalf("retained ownership did not include search index text: before=%d after=%d", before, after)
	}
}

func TestInspectSubgraphDepthBoundaryAndNestedContinuation(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "subgraph-bounds"}})
	opened := openWorkbench(t, instance, projectCompileInput(allDeclarationsFixture))
	depthZero, err := instance.InspectSubgraph(context.Background(), InspectSubgraphInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, RootAddresses: []string{workbenchAlpha}, Depth: 0})
	if err != nil || len(depthZero.Items) != 1 || depthZero.Items[0].Adjacency != nil || len(depthZero.Items[0].Facts.RowAddresses) != 0 {
		t.Fatalf("depth-zero disclosure = %+v, %v", depthZero, err)
	}

	limits := WorkbenchLimits{MaxItems: 1, MaxOutputBytes: 1 << 20}
	var cursor *Cursor
	seenCursor := map[string]bool{}
	seenSubjects := map[string]bool{}
	for pages := 0; ; pages++ {
		if pages > 16 {
			t.Fatal("subgraph cursor did not complete")
		}
		result, readErr := instance.InspectSubgraph(context.Background(), InspectSubgraphInput{DocumentGeneration: opened.DocumentGeneration, Limits: limits, RootAddresses: []string{workbenchAlpha}, Depth: 1, Cursor: cursor})
		if readErr != nil || len(result.Items) != 1 || len(result.Relations) > 1 || len(result.Items[0].Facts.RowAddresses) > 1 {
			t.Fatalf("bounded subgraph page = %+v, %v", result, readErr)
		}
		if adjacency := result.Items[0].Adjacency; adjacency != nil && (len(adjacency.Incoming) > 1 || len(adjacency.Outgoing) > 1) {
			t.Fatalf("unbounded adjacency page = %+v", adjacency)
		}
		seenSubjects[result.Items[0].Subject.Address] = true
		if result.Page.NextCursor == nil {
			break
		}
		if seenCursor[result.Page.NextCursor.Value] {
			t.Fatalf("repeated subgraph cursor %q", result.Page.NextCursor.Value)
		}
		seenCursor[result.Page.NextCursor.Value] = true
		cursor = result.Page.NextCursor
	}
	if len(seenSubjects) != 3 {
		t.Fatalf("continued subgraph subjects = %v", seenSubjects)
	}
}

func TestMutationCancellationIsRecheckedAtLockedCommit(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "commit-cancel"}})
	opened := openWorkbench(t, instance, projectCompileInput(allDeclarationsFixture))

	instance.workbench.mu.Lock()
	openContext, cancelOpen := context.WithCancel(context.Background())
	openResult := make(chan error, 1)
	go func() {
		_, err := instance.OpenDocument(openContext, OpenDocumentInput{CompileInput: projectCompileInput("project blocked"), RequestedLimits: generousWorkbenchLimits})
		openResult <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancelOpen()
	instance.workbench.mu.Unlock()
	if err := <-openResult; !IsWorkbenchError(err, WorkbenchErrorCancelled) {
		t.Fatalf("cancelled OpenDocument committed: %v", err)
	}
	instance.workbench.mu.RLock()
	documentCount := len(instance.workbench.documents)
	instance.workbench.mu.RUnlock()
	if documentCount != 1 {
		t.Fatalf("cancelled open changed document count to %d", documentCount)
	}

	instance.workbench.mu.Lock()
	closeContext, cancelClose := context.WithCancel(context.Background())
	closeResult := make(chan error, 1)
	go func() {
		_, err := instance.CloseDocument(closeContext, CloseDocumentInput{DocumentHandle: opened.DocumentHandle, DocumentGeneration: opened.DocumentGeneration})
		closeResult <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancelClose()
	instance.workbench.mu.Unlock()
	if err := <-closeResult; !IsWorkbenchError(err, WorkbenchErrorCancelled) {
		t.Fatalf("cancelled CloseDocument committed: %v", err)
	}
	if _, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits}); err != nil {
		t.Fatalf("cancelled close removed document: %v", err)
	}

	var large strings.Builder
	large.WriteString("project p \"P\" {\n")
	for index := 0; index < 8_000; index++ {
		large.WriteString("# compile outside the mutation lock\n")
	}
	large.WriteString("}\n")
	replaceContext, cancelReplace := context.WithCancel(context.Background())
	replaceResult := make(chan error, 1)
	go func() {
		_, err := instance.ReplaceSourceTree(replaceContext, ReplaceSourceTreeInput{ExpectedGeneration: opened.DocumentGeneration, CompileInput: projectCompileInput(large.String())})
		replaceResult <- err
	}()
	time.Sleep(time.Millisecond)
	instance.workbench.mu.Lock()
	time.Sleep(50 * time.Millisecond)
	cancelReplace()
	instance.workbench.mu.Unlock()
	if err := <-replaceResult; !IsWorkbenchError(err, WorkbenchErrorCancelled) {
		t.Fatalf("cancelled ReplaceSourceTree committed: %v", err)
	}
	if _, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits}); err != nil {
		t.Fatalf("cancelled replace advanced generation: %v", err)
	}
}

func TestRowCellCanonicalScalarWireForms(t *testing.T) {
	cases := []struct {
		name  string
		value NormalizedScalar
		want  string
	}{
		{name: "string", value: NormalizedScalar{Type: definition.ScalarString, String: "<text>"}, want: `{"column_address":"column","value":{"kind":"string","string":"\u003ctext\u003e"}}`},
		{name: "enum", value: NormalizedScalar{Type: definition.ScalarEnum, String: "choice"}, want: `{"column_address":"column","value":{"kind":"string","string":"choice"}}`},
		{name: "date", value: NormalizedScalar{Type: definition.ScalarDate, String: "2026-07-16"}, want: `{"column_address":"column","value":{"kind":"string","string":"2026-07-16"}}`},
		{name: "datetime", value: NormalizedScalar{Type: definition.ScalarDatetime, String: "2026-07-16T00:00:00Z"}, want: `{"column_address":"column","value":{"kind":"string","string":"2026-07-16T00:00:00Z"}}`},
		{name: "integer", value: NormalizedScalar{Type: definition.ScalarInteger, Int: -42}, want: `{"column_address":"column","value":{"integer":"-42","kind":"integer"}}`},
		{name: "boolean", value: NormalizedScalar{Type: definition.ScalarBoolean, Bool: true}, want: `{"column_address":"column","value":{"boolean":true,"kind":"boolean"}}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(RowCell{ColumnAddress: "column", Value: test.value})
			if err != nil || string(encoded) != test.want {
				t.Fatalf("RowCell = %s, %v; want %s", encoded, err, test.want)
			}
		})
	}

	decimals := map[float64]string{
		0:       "0",
		1.5:     "1.5",
		1e20:    "100000000000000000000",
		0.001:   "0.001",
		1e-7:    "1e-7",
		-1e21:   "-1e+21",
		-12.375: "-12.375",
	}
	for value, want := range decimals {
		if got := workbenchCanonicalBinary64(value); got != want {
			t.Errorf("canonical binary64(%g) = %q, want %q", value, got, want)
		}
		if _, err := json.Marshal(RowCell{ColumnAddress: "column", Value: NormalizedScalar{Type: definition.ScalarNumber, Float: value}}); err != nil {
			t.Errorf("number %g rejected: %v", value, err)
		}
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), math.Copysign(0, -1)} {
		if _, err := json.Marshal(RowCell{ColumnAddress: "column", Value: NormalizedScalar{Type: definition.ScalarNumber, Float: value}}); err == nil {
			t.Errorf("invalid number %g accepted", value)
		}
	}
	if _, err := json.Marshal(RowCell{ColumnAddress: "column", Value: NormalizedScalar{Type: definition.ScalarType("future")}}); err == nil {
		t.Fatal("unknown scalar type accepted")
	}
}

func TestRetainedOwnershipHandlesCyclesAndSaturation(t *testing.T) {
	type owner struct {
		Array [2]string
		Map   map[string][]string
		Next  *owner
	}
	value := &owner{Array: [2]string{"array-a", "array-b"}, Map: map[string][]string{"key": {"value-a", "value-b"}}}
	value.Next = value
	if got := retainedOwnedBytes(value); got <= int64(len("array-aarray-bkeyvalue-avalue-b")) {
		t.Fatalf("retained owner bytes = %d", got)
	}
	if got := retainedOwnedBytes(nil); got != 0 {
		t.Fatalf("invalid retained value = %d", got)
	}
	var nilOwner *owner
	if got := retainedOwnedBytes(nilOwner); got == 0 {
		t.Fatal("typed nil owner did not retain its pointer slot")
	}
	if got := retainedDynamicBytes(reflect.ValueOf([]string(nil)), map[uintptr]bool{}); got != 0 {
		t.Fatalf("nil slice dynamic bytes = %d", got)
	}
	if got := retainedDynamicBytes(reflect.ValueOf(map[string]string(nil)), map[uintptr]bool{}); got != 0 {
		t.Fatalf("nil map dynamic bytes = %d", got)
	}
	if got := saturatingMultiply(math.MaxInt64, 2); got != math.MaxInt64 {
		t.Fatalf("saturating multiply = %d", got)
	}
	if got := saturatingMultiply(0, 2); got != 0 {
		t.Fatalf("zero multiply = %d", got)
	}
	if got := saturatingAdd(math.MaxInt64, 1); got != math.MaxInt64 {
		t.Fatalf("saturating add = %d", got)
	}
}

func TestEveryWorkbenchReadRejectsGenerationAndLimitBoundaries(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "read-boundaries"}})
	opened := openWorkbench(t, instance, projectCompileInput(allDeclarationsFixture))
	invalidGeneration := DocumentGeneration{DocumentHandle: opened.DocumentHandle}
	invalidLimits := WorkbenchLimits{MaxItems: 0, MaxOutputBytes: 1}
	tests := []struct {
		name string
		read func(DocumentGeneration, WorkbenchLimits) error
	}{
		{name: "list_modules", read: func(generation DocumentGeneration, limits WorkbenchLimits) error {
			_, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: generation, Limits: limits})
			return err
		}},
		{name: "read_modules", read: func(generation DocumentGeneration, limits WorkbenchLimits) error {
			_, err := instance.ReadModules(context.Background(), ReadModulesInput{DocumentGeneration: generation, Limits: limits, Modules: []ModuleRef{{ModulePath: "document.ldl", Origin: SourceOrigin{Kind: "project"}}}})
			return err
		}},
		{name: "find_symbols", read: func(generation DocumentGeneration, limits WorkbenchLimits) error {
			_, err := instance.FindSymbols(context.Background(), FindSymbolsInput{DocumentGeneration: generation, Limits: limits, Query: "alpha", MatchMode: "exact", CaseMode: "sensitive"})
			return err
		}},
		{name: "read_declarations", read: func(generation DocumentGeneration, limits WorkbenchLimits) error {
			_, err := instance.ReadDeclarations(context.Background(), ReadDeclarationsInput{DocumentGeneration: generation, Limits: limits, Addresses: []string{workbenchAlpha}})
			return err
		}},
		{name: "read_rows", read: func(generation DocumentGeneration, limits WorkbenchLimits) error {
			_, err := instance.ReadRows(context.Background(), ReadRowsInput{DocumentGeneration: generation, Limits: limits, OwnerAddresses: []string{workbenchAlpha}})
			return err
		}},
		{name: "find_usages", read: func(generation DocumentGeneration, limits WorkbenchLimits) error {
			_, err := instance.FindUsages(context.Background(), FindUsagesInput{DocumentGeneration: generation, Limits: limits, TargetAddresses: []string{workbenchAlpha}})
			return err
		}},
		{name: "get_neighbors", read: func(generation DocumentGeneration, limits WorkbenchLimits) error {
			_, err := instance.GetNeighbors(context.Background(), GetNeighborsInput{DocumentGeneration: generation, Limits: limits, EntityAddresses: []string{workbenchAlpha}, Direction: "both", Depth: 1})
			return err
		}},
		{name: "inspect_subgraph", read: func(generation DocumentGeneration, limits WorkbenchLimits) error {
			_, err := instance.InspectSubgraph(context.Background(), InspectSubgraphInput{DocumentGeneration: generation, Limits: limits, RootAddresses: []string{workbenchAlpha}, Depth: 1})
			return err
		}},
		{name: "read_scope", read: func(generation DocumentGeneration, limits WorkbenchLimits) error {
			_, err := instance.ReadScope(context.Background(), ReadScopeInput{DocumentGeneration: generation, Limits: limits, OwnerAddress: workbenchAlpha})
			return err
		}},
		{name: "list_references", read: func(generation DocumentGeneration, limits WorkbenchLimits) error {
			_, err := instance.ListReferences(context.Background(), ListReferencesInput{DocumentGeneration: generation, Limits: limits})
			return err
		}},
		{name: "read_references", read: func(generation DocumentGeneration, limits WorkbenchLimits) error {
			_, err := instance.ReadReferences(context.Background(), ReadReferencesInput{DocumentGeneration: generation, Limits: limits, Addresses: []string{workbenchAlpha}})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.read(invalidGeneration, generousWorkbenchLimits); !IsWorkbenchError(err, WorkbenchErrorGenerationStale) {
				t.Fatalf("zero generation error = %v", err)
			}
			if err := test.read(opened.DocumentGeneration, invalidLimits); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
				t.Fatalf("invalid limits error = %v", err)
			}
			oversized := WorkbenchLimits{MaxItems: generousWorkbenchLimits.MaxItems, MaxOutputBytes: generousWorkbenchLimits.MaxOutputBytes + 1}
			if err := test.read(opened.DocumentGeneration, oversized); !IsWorkbenchError(err, WorkbenchErrorLimitExceeded) {
				t.Fatalf("limits above document grant error = %v", err)
			}
		})
	}
}

func TestReadModulesAcrossProjectAndInstalledPackOrigins(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "module-origins"}})
	opened := openWorkbench(t, instance, installedPackProjectInput())
	listed, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits})
	if err != nil || len(listed.Items) != 3 {
		t.Fatalf("ListModules installed pack = %+v, %v", listed, err)
	}
	modules := []ModuleRef{listed.Items[0].Module, listed.Items[1].Module, listed.Items[2].Module}
	read, err := instance.ReadModules(context.Background(), ReadModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, Modules: modules})
	if err != nil || len(read.Items) != 3 || read.Items[0].Module.Origin.Kind != "project" || read.Items[1].Module.Origin.Kind != "pack" || read.Items[2].Module.Origin.Kind != "pack" {
		t.Fatalf("ReadModules mixed origins = %+v, %v", read, err)
	}
	missing := ModuleRef{ModulePath: "missing.ldl", Origin: SourceOrigin{Kind: "project"}}
	if _, err := instance.ReadModules(context.Background(), ReadModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, Modules: []ModuleRef{missing}}); !IsWorkbenchError(err, WorkbenchErrorNotFound) {
		t.Fatalf("missing module error = %v", err)
	}
}

func TestDegradedWorkingDocumentOrdersProjectAndMultiplePackSources(t *testing.T) {
	input := installedPackProjectInput()
	input.ProjectSourceTree["document.ldl"] = []byte("project p")
	otherSource := []byte("entity_type worker \"Worker\" {\n  representation shape rect\n}\nexport { worker }\n")
	otherManifest := packManifestBytes("pub/other", "other", "1.0.0", "pack.ldl", nil)
	input.InstalledPackTree["pack/other/pack.ldl"] = otherSource
	input.ResolvedDependencies.Installs = append(input.ResolvedDependencies.Installs, ResolvedPack{
		InstallName: "other", CanonicalID: "pub/other", Version: "1.0.0", Digest: "sha256:" + strings.Repeat("b", 64),
		Path: "pack/other", Entry: "pack.ldl", Files: []ResolvedPackFile{{Path: "pack.ldl", Digest: digestBytes(otherSource)}},
		ManifestPath: "manifest.json", Manifest: otherManifest,
	})
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "degraded-order"}})
	opened := openWorkbench(t, instance, input)
	if opened.State.SemanticState != "unavailable" || !opened.Capabilities.ReadModules {
		t.Fatalf("degraded multi-pack state = %+v / %+v", opened.State, opened.Capabilities)
	}
	listed, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits})
	if err != nil || len(listed.Items) != 5 {
		t.Fatalf("degraded multi-pack modules = %+v, %v", listed, err)
	}
	if listed.Items[0].Module.Origin.Kind != "project" || listed.Items[1].Module.Origin.PackAddress != "ldl:pack:pub:other" || listed.Items[3].Module.Origin.PackAddress != "ldl:pack:pub:schema" {
		t.Fatalf("degraded multi-pack canonical order = %+v", listed.Items)
	}
}

func TestWorkbenchReadSelectionLimitsAreEnforcedBeforeScanning(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "selection-limits", MaxItems: 1}})
	opened := openWorkbench(t, instance, projectCompileInput(allDeclarationsFixture))
	limits := WorkbenchLimits{MaxItems: 1, MaxOutputBytes: generousWorkbenchLimits.MaxOutputBytes}
	reads := map[string]func() error{
		"read_modules": func() error {
			_, err := instance.ReadModules(context.Background(), ReadModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: limits, Modules: []ModuleRef{{ModulePath: "a.ldl", Origin: SourceOrigin{Kind: "project"}}, {ModulePath: "b.ldl", Origin: SourceOrigin{Kind: "project"}}}})
			return err
		},
		"find_symbols": func() error {
			_, err := instance.FindSymbols(context.Background(), FindSymbolsInput{DocumentGeneration: opened.DocumentGeneration, Limits: limits, Query: "a", MatchMode: "exact", CaseMode: "sensitive", OwnerAddresses: []string{workbenchAlpha, workbenchBeta}})
			return err
		},
		"read_declarations": func() error {
			_, err := instance.ReadDeclarations(context.Background(), ReadDeclarationsInput{DocumentGeneration: opened.DocumentGeneration, Limits: limits, Addresses: []string{workbenchAlpha, workbenchBeta}})
			return err
		},
		"read_rows": func() error {
			_, err := instance.ReadRows(context.Background(), ReadRowsInput{DocumentGeneration: opened.DocumentGeneration, Limits: limits, OwnerAddresses: []string{workbenchAlpha, workbenchBeta}})
			return err
		},
		"find_usages": func() error {
			_, err := instance.FindUsages(context.Background(), FindUsagesInput{DocumentGeneration: opened.DocumentGeneration, Limits: limits, TargetAddresses: []string{workbenchAlpha, workbenchBeta}})
			return err
		},
		"get_neighbors": func() error {
			_, err := instance.GetNeighbors(context.Background(), GetNeighborsInput{DocumentGeneration: opened.DocumentGeneration, Limits: limits, EntityAddresses: []string{workbenchAlpha, workbenchBeta}, Direction: "both", Depth: 1})
			return err
		},
		"inspect_subgraph": func() error {
			_, err := instance.InspectSubgraph(context.Background(), InspectSubgraphInput{DocumentGeneration: opened.DocumentGeneration, Limits: limits, RootAddresses: []string{workbenchAlpha, workbenchBeta}, Depth: 1})
			return err
		},
	}
	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			if err := read(); !IsWorkbenchError(err, WorkbenchErrorLimitExceeded) {
				t.Fatalf("selection above document limit error = %v", err)
			}
		})
	}
}

func TestGraphReadsRejectMissingRootsAndHonorIncomingDirection(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "graph-contract"}})
	opened := openWorkbench(t, instance, projectCompileInput(allDeclarationsFixture))
	missing := "ldl:project:p:entity:missing"
	if _, err := instance.GetNeighbors(context.Background(), GetNeighborsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, EntityAddresses: []string{missing}, Direction: "both", Depth: 1}); !IsWorkbenchError(err, WorkbenchErrorNotFound) {
		t.Fatalf("missing neighbor root error = %v", err)
	}
	if _, err := instance.InspectSubgraph(context.Background(), InspectSubgraphInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, RootAddresses: []string{missing}, Depth: 1}); !IsWorkbenchError(err, WorkbenchErrorNotFound) {
		t.Fatalf("missing subgraph root error = %v", err)
	}
	incoming, err := instance.GetNeighbors(context.Background(), GetNeighborsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, EntityAddresses: []string{workbenchBeta}, Direction: "incoming", Depth: 1})
	if err != nil || len(incoming.Items) != 1 || incoming.Items[0].EntityAddress != workbenchAlpha || incoming.Items[0].Direction != "incoming" {
		t.Fatalf("incoming neighbors = %+v, %v", incoming, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := instance.GetNeighbors(cancelled, GetNeighborsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, EntityAddresses: []string{workbenchAlpha}, Direction: "both", Depth: 2}); !IsWorkbenchError(err, WorkbenchErrorCancelled) {
		t.Fatalf("cancelled graph read error = %v", err)
	}
}

func TestGraphReadsPaginateBeyondDocumentPageLimit(t *testing.T) {
	const count = 12
	var source strings.Builder
	source.WriteString("project p \"Project\" {}\nlayers {\n app \"Application\" @1\n}\nentity_type service \"Service\" {\n representation shape rect\n}\n")
	source.WriteString("relation_type link \"Link\" dependency {\n duplicate_policy allow\n from source types [service] layers [app]\n to target types [service] layers [app]\n label \"links\"\n}\nentities service @app {\n")
	for index := 0; index < count; index++ {
		fmt.Fprintf(&source, "n%02d \"Node %02d\"\n", index, index)
	}
	source.WriteString("}\nrelations link {\n")
	for index := 0; index < count-1; index++ {
		fmt.Fprintf(&source, "r%02d: n%02d -> n%02d\n", index, index, index+1)
	}
	source.WriteString("}\n")

	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "lazy-graph", MaxItems: 2}})
	opened := openWorkbench(t, instance, projectCompileInput(source.String()))
	limits := WorkbenchLimits{MaxItems: 2, MaxOutputBytes: generousWorkbenchLimits.MaxOutputBytes}
	root := "ldl:project:p:entity:n00"
	var cursor *Cursor
	var neighbors []NeighborReadItem
	boundedFirstPage := newReadBoundaryCancellation(workbenchReadNeighborNode, "ldl:project:p:entity:n03")
	for {
		readContext := context.Context(context.Background())
		if cursor == nil {
			readContext = boundedFirstPage
		}
		result, err := instance.GetNeighbors(readContext, GetNeighborsInput{DocumentGeneration: opened.DocumentGeneration, Limits: limits, EntityAddresses: []string{root}, Direction: "outgoing", Depth: count - 1, Cursor: cursor})
		if err != nil || len(result.Items) == 0 || len(result.Items) > int(limits.MaxItems) {
			t.Fatalf("GetNeighbors page %d = %+v, %v", len(neighbors), result, err)
		}
		wire := mapNeighborResult(result)
		if _, err := engineprotocol.EncodeGetNeighborsResult(wire); err != nil {
			t.Fatalf("generated GetNeighborsResult rejected traversal page: %v", err)
		}
		if len(wire.Items) > 1 {
			wire.Items[0], wire.Items[1] = wire.Items[1], wire.Items[0]
			if _, err := engineprotocol.EncodeGetNeighborsResult(wire); err == nil {
				t.Fatal("generated GetNeighborsResult accepted descending traversal indexes")
			}
		}
		neighbors = append(neighbors, result.Items...)
		if result.Page.NextCursor == nil {
			break
		}
		cursor = result.Page.NextCursor
	}
	if boundedFirstPage.wasReached() || boundedFirstPage.Err() != nil {
		t.Fatal("first neighbor page traversed beyond its item window")
	}
	if len(neighbors) != count-1 {
		t.Fatalf("neighbor pages returned %d items, want %d", len(neighbors), count-1)
	}
	for index := range neighbors {
		if neighbors[index].TraversalIndex != uint64(index) {
			t.Fatalf("neighbor traversal index %d = %d", index, neighbors[index].TraversalIndex)
		}
		if index > 0 && neighbors[index-1].EntityAddress != neighbors[index].SourceEntityAddress {
			t.Fatalf("neighbor traversal is not FIFO encounter order: %+v", neighbors)
		}
	}

	cursor = nil
	subgraphLimits := WorkbenchLimits{MaxItems: 1, MaxOutputBytes: generousWorkbenchLimits.MaxOutputBytes}
	seen := map[uint64]SubgraphReadItem{}
	for pages := 0; ; pages++ {
		if pages > 8 {
			t.Fatal("InspectSubgraph cursor did not complete")
		}
		result, err := instance.InspectSubgraph(context.Background(), InspectSubgraphInput{DocumentGeneration: opened.DocumentGeneration, Limits: subgraphLimits, RootAddresses: []string{root}, Depth: 2, Cursor: cursor})
		if err != nil || len(result.Items) != 1 {
			t.Fatalf("InspectSubgraph page %d = %+v, %v", pages, result, err)
		}
		item := result.Items[0]
		if previous, exists := seen[item.TraversalIndex]; exists && (previous.Subject.Address != item.Subject.Address || previous.Depth != item.Depth) {
			t.Fatalf("nested continuation changed traversal identity: before=%+v after=%+v", previous, item)
		}
		seen[item.TraversalIndex] = item
		if result.Page.NextCursor == nil {
			break
		}
		cursor = result.Page.NextCursor
	}
	if len(seen) != 5 {
		t.Fatalf("depth-two subgraph subjects = %v", seen)
	}
	wantAddresses := []string{
		"ldl:project:p:entity:n00",
		"ldl:project:p:relation:r00",
		"ldl:project:p:entity:n01",
		"ldl:project:p:relation:r01",
		"ldl:project:p:entity:n02",
	}
	wantDepths := []int64{0, 1, 1, 2, 2}
	for index, address := range wantAddresses {
		item, exists := seen[uint64(index)]
		if !exists || item.Subject.Address != address || item.Depth != wantDepths[index] {
			t.Fatalf("subgraph traversal item %d = %+v, want %s at depth %d", index, item, address, wantDepths[index])
		}
	}
}

type readBoundaryCancellation struct {
	context.Context
	cancel   context.CancelFunc
	boundary string
	address  string
	once     sync.Once
	reached  chan struct{}
}

func newReadBoundaryCancellation(boundary, address string) *readBoundaryCancellation {
	ctx, cancel := context.WithCancel(context.Background())
	return &readBoundaryCancellation{Context: ctx, cancel: cancel, boundary: boundary, address: address, reached: make(chan struct{})}
}

func (c *readBoundaryCancellation) onWorkbenchReadBoundary(boundary, address string) {
	if boundary == c.boundary && (c.address == "" || address == c.address) {
		c.once.Do(func() {
			close(c.reached)
			c.cancel()
		})
	}
}

func (c *readBoundaryCancellation) wasReached() bool {
	select {
	case <-c.reached:
		return true
	default:
		return false
	}
}

func TestGraphCursorInvalidationAcrossReplacementAndClose(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "graph-lifecycle", MaxItems: 1}})
	opened := openWorkbench(t, instance, projectCompileInput(allDeclarationsFixture))
	limits := WorkbenchLimits{MaxItems: 1, MaxOutputBytes: generousWorkbenchLimits.MaxOutputBytes}
	read := func(generation DocumentGeneration, cursor *Cursor) (GetNeighborsResult, error) {
		return instance.GetNeighbors(context.Background(), GetNeighborsInput{DocumentGeneration: generation, Limits: limits, EntityAddresses: []string{workbenchAlpha}, Direction: "both", Depth: 2, Cursor: cursor})
	}
	first, err := read(opened.DocumentGeneration, nil)
	if err != nil || first.Page.NextCursor == nil {
		t.Fatalf("first graph page = %+v, %v", first, err)
	}
	replaced, err := instance.ReplaceSourceTree(context.Background(), ReplaceSourceTreeInput{ExpectedGeneration: opened.DocumentGeneration, CompileInput: projectCompileInput(allDeclarationsFixture)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := read(opened.DocumentGeneration, first.Page.NextCursor); !IsWorkbenchError(err, WorkbenchErrorGenerationStale) {
		t.Fatalf("pre-replacement graph cursor error = %v", err)
	}
	if _, err := read(replaced.DocumentGeneration, first.Page.NextCursor); !IsWorkbenchError(err, WorkbenchErrorCursorInvalid) {
		t.Fatalf("cross-generation graph cursor error = %v", err)
	}
	current, err := read(replaced.DocumentGeneration, nil)
	if err != nil || current.Page.NextCursor == nil {
		t.Fatalf("replacement graph page = %+v, %v", current, err)
	}
	if _, err := instance.CloseDocument(context.Background(), CloseDocumentInput{DocumentHandle: opened.DocumentHandle, DocumentGeneration: replaced.DocumentGeneration}); err != nil {
		t.Fatal(err)
	}
	if _, err := read(replaced.DocumentGeneration, current.Page.NextCursor); !IsWorkbenchError(err, WorkbenchErrorHandleInvalid) {
		t.Fatalf("closed graph cursor error = %v", err)
	}
}

func TestInspectSubgraphByteContinuationReportsOutputLimit(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "subgraph-bytes"}})
	opened := openWorkbench(t, instance, projectCompileInput(allDeclarationsFixture))
	var first InspectSubgraphResult
	var selectedOutputBytes int64
	for outputBytes := int64(700); outputBytes < generousWorkbenchLimits.MaxOutputBytes; outputBytes += 25 {
		result, err := instance.InspectSubgraph(context.Background(), InspectSubgraphInput{DocumentGeneration: opened.DocumentGeneration, Limits: WorkbenchLimits{MaxItems: 10, MaxOutputBytes: outputBytes}, RootAddresses: []string{workbenchAlpha}, Depth: 1})
		if err == nil && result.Page.NextCursor != nil && result.Page.Truncation == TruncationOutputByteLimit {
			first = result
			selectedOutputBytes = outputBytes
			break
		}
	}
	if first.Page.NextCursor == nil {
		t.Fatal("fixture did not produce a byte-limited nested subgraph continuation")
	}
	if first.Page.ReturnedBytes <= 0 || first.Page.ReturnedBytes > selectedOutputBytes {
		t.Fatalf("returned bytes %d exceed selected limit %d", first.Page.ReturnedBytes, selectedOutputBytes)
	}
	continued, err := instance.InspectSubgraph(context.Background(), InspectSubgraphInput{DocumentGeneration: opened.DocumentGeneration, Limits: WorkbenchLimits{MaxItems: 10, MaxOutputBytes: generousWorkbenchLimits.MaxOutputBytes}, RootAddresses: []string{workbenchAlpha}, Depth: 1, Cursor: first.Page.NextCursor})
	if err != nil || len(continued.Items) == 0 {
		t.Fatalf("continued byte-limited subgraph = %+v, %v", continued, err)
	}
	tampered := *first.Page.NextCursor
	tampered.Value += "A"
	if _, err := instance.InspectSubgraph(context.Background(), InspectSubgraphInput{DocumentGeneration: opened.DocumentGeneration, Limits: WorkbenchLimits{MaxItems: 10, MaxOutputBytes: generousWorkbenchLimits.MaxOutputBytes}, RootAddresses: []string{workbenchAlpha}, Depth: 1, Cursor: &tampered}); !IsWorkbenchError(err, WorkbenchErrorCursorInvalid) {
		t.Fatalf("tampered subgraph cursor error = %v", err)
	}
}

func TestFindSymbolsPublicMatchModesFiltersAndPageBounds(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "symbol-contract"}})
	opened := openWorkbench(t, instance, projectCompileInput(allDeclarationsFixture))
	read := func(input FindSymbolsInput) FindSymbolsResult {
		t.Helper()
		input.DocumentGeneration = opened.DocumentGeneration
		if input.Limits == (WorkbenchLimits{}) {
			input.Limits = generousWorkbenchLimits
		}
		result, err := instance.FindSymbols(context.Background(), input)
		if err != nil {
			t.Fatalf("FindSymbols(%+v) error = %v", input, err)
		}
		return result
	}

	for _, test := range []struct {
		name      string
		query     string
		matchMode string
		wantField string
		kind      SemanticSubjectKind
	}{
		{name: "address", query: workbenchAlpha, matchMode: "exact", wantField: "address", kind: materialize.SubjectEntity},
		{name: "display_prefix", query: "Alp", matchMode: "prefix", wantField: "display_name", kind: materialize.SubjectEntity},
		{name: "display_substring", query: " to ", matchMode: "substring", wantField: "display_name", kind: materialize.SubjectRelation},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := read(FindSymbolsInput{Query: test.query, MatchMode: test.matchMode, CaseMode: "sensitive", SubjectKinds: []SemanticSubjectKind{test.kind}})
			if len(result.Items) != 1 || result.Items[0].MatchedField != test.wantField {
				t.Fatalf("match result = %+v", result)
			}
		})
	}

	filtered := read(FindSymbolsInput{
		Query:          "primary",
		MatchMode:      "exact",
		CaseMode:       "sensitive",
		OwnerAddresses: []string{workbenchAlpha},
		SubjectKinds:   []SemanticSubjectKind{materialize.SubjectEntityRow},
	})
	if len(filtered.Items) != 1 || filtered.Items[0].Address != workbenchAlphaRow {
		t.Fatalf("owner/kind filtered symbols = %+v", filtered)
	}

	empty := read(FindSymbolsInput{Query: "no-symbol-has-this-name", MatchMode: "exact", CaseMode: "sensitive"})
	if len(empty.Items) != 0 || empty.Page.Truncation != TruncationComplete || empty.Page.ReturnedBytes <= 0 {
		t.Fatalf("empty symbol page = %+v", empty)
	}
	_, err := instance.FindSymbols(context.Background(), FindSymbolsInput{
		DocumentGeneration: opened.DocumentGeneration,
		Limits:             WorkbenchLimits{MaxItems: 1, MaxOutputBytes: 1},
		Query:              "no-symbol-has-this-name",
		MatchMode:          "exact",
		CaseMode:           "sensitive",
	})
	if !IsWorkbenchError(err, WorkbenchErrorLimitExceeded) || !strings.Contains(err.Error(), "max_output_bytes") {
		t.Fatalf("empty symbol output limit error = %v", err)
	}
	_, err = instance.FindSymbols(context.Background(), FindSymbolsInput{
		DocumentGeneration: opened.DocumentGeneration,
		Limits:             WorkbenchLimits{MaxItems: 1, MaxOutputBytes: 1},
		Query:              "alpha",
		MatchMode:          "exact",
		CaseMode:           "sensitive",
	})
	if !IsWorkbenchError(err, WorkbenchErrorLimitExceeded) {
		t.Fatalf("non-empty symbol output limit error = %v", err)
	}

	first := read(FindSymbolsInput{Limits: WorkbenchLimits{MaxItems: 1, MaxOutputBytes: generousWorkbenchLimits.MaxOutputBytes}, Query: ":", MatchMode: "substring", CaseMode: "sensitive"})
	if len(first.Items) != 1 || first.Page.NextCursor == nil || first.Page.Truncation != TruncationItemLimit {
		t.Fatalf("first bounded symbol page = %+v", first)
	}
	if _, err := instance.FindSymbols(context.Background(), FindSymbolsInput{
		DocumentGeneration: opened.DocumentGeneration,
		Limits:             WorkbenchLimits{MaxItems: 1, MaxOutputBytes: generousWorkbenchLimits.MaxOutputBytes},
		Cursor:             first.Page.NextCursor,
		Query:              "alpha",
		MatchMode:          "substring",
		CaseMode:           "sensitive",
	}); !IsWorkbenchError(err, WorkbenchErrorCursorInvalid) {
		t.Fatalf("query-bound symbol cursor error = %v", err)
	}
	macTampered := *first.Page.NextCursor
	last := len(macTampered.Value) - 1
	if macTampered.Value[last] == 'A' {
		macTampered.Value = macTampered.Value[:last] + "B"
	} else {
		macTampered.Value = macTampered.Value[:last] + "A"
	}
	if _, err := instance.FindSymbols(context.Background(), FindSymbolsInput{
		DocumentGeneration: opened.DocumentGeneration,
		Limits:             WorkbenchLimits{MaxItems: 1, MaxOutputBytes: generousWorkbenchLimits.MaxOutputBytes},
		Cursor:             &macTampered,
		Query:              ":",
		MatchMode:          "substring",
		CaseMode:           "sensitive",
	}); !IsWorkbenchError(err, WorkbenchErrorCursorInvalid) || err.Error() != "engine.workbench.cursor_invalid" {
		t.Fatalf("MAC-tampered symbol cursor error = %v", err)
	}

	foundByteBound := false
	for outputBytes := int64(400); outputBytes < 1_200; outputBytes += 25 {
		result, readErr := instance.FindSymbols(context.Background(), FindSymbolsInput{
			DocumentGeneration: opened.DocumentGeneration,
			Limits:             WorkbenchLimits{MaxItems: 100, MaxOutputBytes: outputBytes},
			Query:              ":",
			MatchMode:          "substring",
			CaseMode:           "sensitive",
		})
		if readErr == nil && len(result.Items) > 0 && result.Page.NextCursor != nil && result.Page.Truncation == TruncationOutputByteLimit {
			foundByteBound = true
			break
		}
	}
	if !foundByteBound {
		t.Fatal("symbol result did not produce a byte-bounded page")
	}
}

func TestPublicCollectionReadsReturnBoundedEmptyPages(t *testing.T) {
	const source = `project p "Project" {}
layers {
  app "Application" @1
}
entity_type service "Service" {
  representation shape rect
}
entities service @app {
  isolated "Isolated"
}
`
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "empty-pages"}})
	opened := openWorkbench(t, instance, projectCompileInput(source))
	entity := "ldl:project:p:entity:isolated"

	references, err := instance.ListReferences(context.Background(), ListReferencesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits})
	if err != nil || len(references.Items) != 0 || references.Page.Truncation != TruncationComplete || references.Page.ReturnedBytes <= 0 {
		t.Fatalf("empty references = %+v, %v", references, err)
	}
	usages, err := instance.FindUsages(context.Background(), FindUsagesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, TargetAddresses: []string{entity}})
	if err != nil || len(usages.Items) != 0 || usages.Page.Truncation != TruncationComplete {
		t.Fatalf("empty usages = %+v, %v", usages, err)
	}
	neighbors, err := instance.GetNeighbors(context.Background(), GetNeighborsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, EntityAddresses: []string{entity}, Direction: "both", Depth: 2})
	if err != nil || len(neighbors.Items) != 0 || neighbors.Page.Truncation != TruncationComplete {
		t.Fatalf("empty neighbors = %+v, %v", neighbors, err)
	}
}

func TestPackWorkingDocumentIndexesSchemaSymbolsAndReferences(t *testing.T) {
	packSource := []byte(`entity_type service "Service" {
  representation shape rect
  columns {
    environment "Environment" enum [prod, dev]
  }
}
relation_type link "Link" dependency {
  duplicate_policy allow
  from source types [service]
  to target types [service]
  label "links"
  columns {
    weight "Weight" number
  }
}
query services "Services" {
  select {
    entity_types [service]
    relation_types [link]
  }
  result [seed_entities]
}
view catalog "Catalog" inventory {
  source query services {}
  table {
    rows entity_rows
    entity_types [service]
    entity_id
    column environment {
      source attribute environment entity_types [service]
    }
  }
}
reference guide <<-TEXT
Pack guide.
TEXT
export { service, link, services, catalog, guide }
`)
	input := rootPackInput()
	input.InstalledPackTree["pack/schema/pack.ldl"] = packSource
	input.ResolvedDependencies.Installs[0].Files[0].Digest = digestBytes(packSource)
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "pack-indexes"}})
	opened := openWorkbench(t, instance, input)
	if opened.State.SemanticState != "available" || !opened.Capabilities.FindSymbols || !opened.Capabilities.ListReferences || !opened.Capabilities.ReadReferences {
		t.Fatalf("pack working document = %+v / %+v", opened.State, opened.Capabilities)
	}

	for _, query := range []string{"Environment", "Weight", "Services", "Catalog"} {
		result, err := instance.FindSymbols(context.Background(), FindSymbolsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, Query: query, MatchMode: "exact", CaseMode: "sensitive"})
		if err != nil || len(result.Items) != 1 || result.Items[0].MatchedField != "display_name" {
			t.Fatalf("pack symbol %q = %+v, %v", query, result, err)
		}
	}
	references, err := instance.ListReferences(context.Background(), ListReferencesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits})
	if err != nil || len(references.Items) != 1 {
		t.Fatalf("pack references = %+v, %v", references, err)
	}
	content, err := instance.ReadReferences(context.Background(), ReadReferencesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, Addresses: []string{references.Items[0].Address}})
	if err != nil || len(content.Items) != 1 || strings.TrimSpace(string(content.Items[0].TextChunk.Bytes)) != "Pack guide." {
		t.Fatalf("pack reference content = %+v, %v", content, err)
	}
}

func TestPackWorkingDocumentRetainsUnimportedLDLModules(t *testing.T) {
	input := rootPackInput()
	extra := []byte("reference extra <<-TEXT\nExtra module.\nTEXT\nexport { extra }\n")
	input.InstalledPackTree["pack/schema/extra.ldl"] = extra
	input.ResolvedDependencies.Installs[0].Files = append(input.ResolvedDependencies.Installs[0].Files, ResolvedPackFile{Path: "extra.ldl", Digest: digestBytes(extra)})
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "pack-extra-module"}})
	opened := openWorkbench(t, instance, input)
	listed, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits})
	if err != nil || len(listed.Items) != 3 {
		t.Fatalf("pack modules with unimported LDL = %+v, %v", listed, err)
	}
	want := ModuleRef{ModulePath: "extra.ldl", Origin: SourceOrigin{Kind: "pack", PackAddress: "ldl:pack:pub:schema"}}
	read, err := instance.ReadModules(context.Background(), ReadModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, Modules: []ModuleRef{want}})
	if err != nil || len(read.Items) != 1 || !bytes.Equal(read.Items[0].SourceChunk.Bytes, extra) {
		t.Fatalf("unimported pack module bytes = %+v, %v", read, err)
	}
}

func TestOpenDocumentEnforcesPackMetadataAndDeclarationLimits(t *testing.T) {
	metadataBounded := installedPackProjectInput()
	second := metadataBounded.ResolvedDependencies.Installs[0]
	second.InstallName = "other"
	second.CanonicalID = "pub/other"
	second.Path = "pack/other"
	metadataBounded.ResolvedDependencies.Installs = append(metadataBounded.ResolvedDependencies.Installs, second)
	metadataBounded.ResourceLimits = DefaultResourceLimits()
	metadataBounded.ResourceLimits.MaxPackFiles = 1
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "pack-resource-limits"}})
	if _, err := instance.OpenDocument(context.Background(), OpenDocumentInput{CompileInput: metadataBounded, RequestedLimits: generousWorkbenchLimits}); !IsWorkbenchError(err, WorkbenchErrorLimitExceeded) {
		t.Fatalf("pack metadata file limit error = %v", err)
	}

	declarationBounded := rootPackInput()
	packSource := []byte("entity_type service \"Service\" {\n  representation shape rect\n}\nreference guide <<-TEXT\nGuide.\nTEXT\nexport { service, guide }\n")
	declarationBounded.InstalledPackTree["pack/schema/pack.ldl"] = packSource
	declarationBounded.ResolvedDependencies.Installs[0].Files[0].Digest = digestBytes(packSource)
	declarationBounded.ResourceLimits = DefaultResourceLimits()
	declarationBounded.ResourceLimits.MaxDeclarations = 1
	if _, err := instance.OpenDocument(context.Background(), OpenDocumentInput{CompileInput: declarationBounded, RequestedLimits: generousWorkbenchLimits}); !IsWorkbenchError(err, WorkbenchErrorLimitExceeded) {
		t.Fatalf("pack declaration limit error = %v", err)
	}
}

func TestPublicSummaryAndUsageReadsPaginateByItemLimit(t *testing.T) {
	source := allDeclarationsFixture + `
reference operations <<-TEXT
Operations guide.
TEXT
`
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "summary-pages"}})
	opened := openWorkbench(t, instance, projectCompileInput(source))
	limits := WorkbenchLimits{MaxItems: 1, MaxOutputBytes: generousWorkbenchLimits.MaxOutputBytes}

	firstReferences, err := instance.ListReferences(context.Background(), ListReferencesInput{DocumentGeneration: opened.DocumentGeneration, Limits: limits})
	if err != nil || len(firstReferences.Items) != 1 || firstReferences.Page.NextCursor == nil || firstReferences.Page.Truncation != TruncationItemLimit {
		t.Fatalf("first reference summary page = %+v, %v", firstReferences, err)
	}
	secondReferences, err := instance.ListReferences(context.Background(), ListReferencesInput{DocumentGeneration: opened.DocumentGeneration, Limits: limits, Cursor: firstReferences.Page.NextCursor})
	if err != nil || len(secondReferences.Items) != 1 || secondReferences.Page.NextCursor != nil || secondReferences.Items[0].Address == firstReferences.Items[0].Address {
		t.Fatalf("second reference summary page = %+v, %v", secondReferences, err)
	}

	service := "ldl:project:p:entity-type:service"
	firstUsages, err := instance.FindUsages(context.Background(), FindUsagesInput{DocumentGeneration: opened.DocumentGeneration, Limits: limits, TargetAddresses: []string{service}})
	if err != nil || len(firstUsages.Items) != 1 || firstUsages.Page.NextCursor == nil || firstUsages.Page.Truncation != TruncationItemLimit {
		t.Fatalf("first usage page = %+v, %v", firstUsages, err)
	}
	secondUsages, err := instance.FindUsages(context.Background(), FindUsagesInput{DocumentGeneration: opened.DocumentGeneration, Limits: limits, TargetAddresses: []string{service}, Cursor: firstUsages.Page.NextCursor})
	if err != nil || len(secondUsages.Items) != 1 || reflect.DeepEqual(secondUsages.Items[0], firstUsages.Items[0]) {
		t.Fatalf("second usage page = %+v, %v", secondUsages, err)
	}
	seenCursors := map[string]bool{firstUsages.Page.NextCursor.Value: true}
	for cursor := secondUsages.Page.NextCursor; cursor != nil; {
		if seenCursors[cursor.Value] {
			t.Fatalf("usage cursor did not advance: %q", cursor.Value)
		}
		seenCursors[cursor.Value] = true
		next, readErr := instance.FindUsages(context.Background(), FindUsagesInput{DocumentGeneration: opened.DocumentGeneration, Limits: limits, TargetAddresses: []string{service}, Cursor: cursor})
		if readErr != nil || len(next.Items) != 1 {
			t.Fatalf("continued usage page = %+v, %v", next, readErr)
		}
		cursor = next.Page.NextCursor
	}
}

func TestDeclarationPagesBoundMultipleSelectionsAndCancellation(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "declaration-pages"}})
	opened := openWorkbench(t, instance, projectCompileInput(allDeclarationsFixture))
	addresses := []string{workbenchAlpha, workbenchBeta}
	limits := WorkbenchLimits{MaxItems: 1, MaxOutputBytes: generousWorkbenchLimits.MaxOutputBytes}
	first, err := instance.ReadDeclarations(context.Background(), ReadDeclarationsInput{DocumentGeneration: opened.DocumentGeneration, Limits: limits, Addresses: addresses})
	if err != nil || len(first.Items) != 1 || first.Page.NextCursor == nil || first.Page.Truncation != TruncationItemLimit {
		t.Fatalf("first declaration page = %+v, %v", first, err)
	}
	second, err := instance.ReadDeclarations(context.Background(), ReadDeclarationsInput{DocumentGeneration: opened.DocumentGeneration, Limits: limits, Addresses: addresses, Cursor: first.Page.NextCursor})
	if err != nil || len(second.Items) != 1 || second.Items[0].Address == first.Items[0].Address || second.Page.NextCursor != nil {
		t.Fatalf("second declaration page = %+v, %v", second, err)
	}

	foundByteBound := false
	for outputBytes := int64(500); outputBytes < 2_500; outputBytes += 25 {
		result, readErr := instance.ReadDeclarations(context.Background(), ReadDeclarationsInput{
			DocumentGeneration: opened.DocumentGeneration,
			Limits:             WorkbenchLimits{MaxItems: 2, MaxOutputBytes: outputBytes},
			Addresses:          addresses,
		})
		if readErr == nil && len(result.Items) == 1 && result.Page.NextCursor != nil && result.Page.Truncation == TruncationOutputByteLimit {
			foundByteBound = true
			break
		}
	}
	if !foundByteBound {
		t.Fatal("two declaration selections did not produce a byte-bounded first item")
	}

	cancelled := newReadBoundaryCancellation(workbenchReadTextItem, workbenchBeta)
	if _, err := instance.ReadDeclarations(cancelled, ReadDeclarationsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, Addresses: addresses}); !IsWorkbenchError(err, WorkbenchErrorCancelled) {
		t.Fatalf("mid-read declaration cancellation error = %v", err)
	}
}

func TestReadRowsPaginatesAcrossSelectedOwners(t *testing.T) {
	source := strings.Replace(allDeclarationsFixture,
		"  alpha primary: prod, \"api\"\n}",
		"  alpha primary: prod, \"api\"\n  beta primary: dev, \"worker\"\n}", 1)
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "row-pages"}})
	opened := openWorkbench(t, instance, projectCompileInput(source))
	limits := WorkbenchLimits{MaxItems: 1, MaxOutputBytes: generousWorkbenchLimits.MaxOutputBytes}
	owners := []string{workbenchAlpha, workbenchBeta}
	first, err := instance.ReadRows(context.Background(), ReadRowsInput{DocumentGeneration: opened.DocumentGeneration, Limits: limits, OwnerAddresses: owners})
	if err != nil || len(first.Items) != 1 || first.Page.NextCursor == nil || first.Page.Truncation != TruncationItemLimit {
		t.Fatalf("first row page = %+v, %v", first, err)
	}
	second, err := instance.ReadRows(context.Background(), ReadRowsInput{DocumentGeneration: opened.DocumentGeneration, Limits: limits, OwnerAddresses: owners, Cursor: first.Page.NextCursor})
	if err != nil || len(second.Items) != 1 || second.Items[0].OwnerAddress != workbenchBeta || second.Page.NextCursor != nil {
		t.Fatalf("second row page = %+v, %v", second, err)
	}
}

func TestPublicReadCancellationAndScopeCapacity(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "read-cancel"}})
	opened := openWorkbench(t, instance, projectCompileInput(allDeclarationsFixture))

	listContext := newReadBoundaryCancellation(workbenchReadIteratedPage, "")
	if _, err := instance.ListReferences(listContext, ListReferencesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits}); !IsWorkbenchError(err, WorkbenchErrorCancelled) {
		t.Fatalf("mid-page reference-list cancellation error = %v", err)
	}
	textContext := newReadBoundaryCancellation(workbenchReadTextItem, workbenchReference)
	if _, err := instance.ReadReferences(textContext, ReadReferencesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, Addresses: []string{workbenchReference}}); !IsWorkbenchError(err, WorkbenchErrorCancelled) {
		t.Fatalf("mid-page reference-content cancellation error = %v", err)
	}
	if _, err := instance.ReadReferences(context.Background(), ReadReferencesInput{
		DocumentGeneration: opened.DocumentGeneration,
		Limits:             WorkbenchLimits{MaxItems: 1, MaxOutputBytes: 1},
		Addresses:          []string{workbenchReference},
	}); !IsWorkbenchError(err, WorkbenchErrorLimitExceeded) {
		t.Fatalf("reference content output floor error = %v", err)
	}

	scopeBounded := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "scope-capacity", MaxItems: 1}})
	scopeOpened := openWorkbench(t, scopeBounded, projectCompileInput(allDeclarationsFixture))
	if _, err := scopeBounded.ReadScope(context.Background(), ReadScopeInput{
		DocumentGeneration: scopeOpened.DocumentGeneration,
		Limits:             WorkbenchLimits{MaxItems: 1, MaxOutputBytes: generousWorkbenchLimits.MaxOutputBytes},
		OwnerAddress:       workbenchAlpha,
	}); !IsWorkbenchError(err, WorkbenchErrorLimitExceeded) || !strings.Contains(err.Error(), "scope_items") {
		t.Fatalf("scope capacity error = %v", err)
	}
}

func TestPublicHandleAndModuleSelectionMalformedForms(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "malformed-contract"}})
	opened := openWorkbench(t, instance, projectCompileInput(allDeclarationsFixture))

	for name, value := range map[string]string{
		"missing_prefix": "not-a-document-handle",
		"invalid_base64": documentHandlePrefix + "%%%",
		"short_payload":  documentHandlePrefix + "YQ",
	} {
		t.Run(name, func(t *testing.T) {
			generation := opened.DocumentGeneration
			generation.DocumentHandle.Value = value
			if _, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: generation, Limits: generousWorkbenchLimits}); !IsWorkbenchError(err, WorkbenchErrorHandleInvalid) {
				t.Fatalf("malformed handle error = %v", err)
			}
		})
	}
	badClose := opened.DocumentHandle
	badClose.Value = "not-a-document-handle"
	if _, err := instance.CloseDocument(context.Background(), CloseDocumentInput{DocumentHandle: badClose, DocumentGeneration: DocumentGeneration{DocumentHandle: badClose, Value: 1}}); !IsWorkbenchError(err, WorkbenchErrorHandleInvalid) {
		t.Fatalf("malformed close handle error = %v", err)
	}

	packAddress := "ldl:pack:pub:schema"
	invalidSelections := []ModuleRef{
		{ModulePath: "", Origin: SourceOrigin{Kind: "project"}},
		{ModulePath: "/document.ldl", Origin: SourceOrigin{Kind: "project"}},
		{ModulePath: "dir//document.ldl", Origin: SourceOrigin{Kind: "project"}},
		{ModulePath: "./document.ldl", Origin: SourceOrigin{Kind: "project"}},
		{ModulePath: "document.ldl", Origin: SourceOrigin{Kind: "project", PackAddress: packAddress}},
		{ModulePath: "document.ldl", Origin: SourceOrigin{Kind: "pack"}},
		{ModulePath: "document.ldl", Origin: SourceOrigin{Kind: "future"}},
	}
	for index, module := range invalidSelections {
		if _, err := instance.ReadModules(context.Background(), ReadModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, Modules: []ModuleRef{module}}); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
			t.Fatalf("malformed module selection %d error = %v", index, err)
		}
	}
	packA := ModuleRef{ModulePath: "missing.ldl", Origin: SourceOrigin{Kind: "pack", PackAddress: "ldl:pack:a:a"}}
	packB := ModuleRef{ModulePath: "missing.ldl", Origin: SourceOrigin{Kind: "pack", PackAddress: "ldl:pack:b:b"}}
	if _, err := instance.ReadModules(context.Background(), ReadModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, Modules: []ModuleRef{packA, packB}}); !IsWorkbenchError(err, WorkbenchErrorNotFound) {
		t.Fatalf("canonical cross-pack module selection error = %v", err)
	}
	if _, err := instance.ReadModules(context.Background(), ReadModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, Modules: []ModuleRef{packB, packA}}); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
		t.Fatalf("reverse cross-pack module selection error = %v", err)
	}
	if _, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: WorkbenchLimits{MaxItems: generousWorkbenchLimits.MaxItems + 1, MaxOutputBytes: generousWorkbenchLimits.MaxOutputBytes}}); !IsWorkbenchError(err, WorkbenchErrorLimitExceeded) {
		t.Fatalf("broadened max_items error = %v", err)
	}

	if _, err := instance.FindSymbols(context.Background(), FindSymbolsInput{
		DocumentGeneration: opened.DocumentGeneration,
		Limits:             generousWorkbenchLimits,
		Query:              "alpha",
		MatchMode:          "exact",
		CaseMode:           "sensitive",
		OwnerAddresses:     []string{workbenchAlpha, workbenchAlpha},
	}); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
		t.Fatalf("duplicate symbol owner filter error = %v", err)
	}
	if _, err := instance.FindSymbols(context.Background(), FindSymbolsInput{
		DocumentGeneration: opened.DocumentGeneration,
		Limits:             generousWorkbenchLimits,
		Query:              "alpha",
		MatchMode:          "exact",
		CaseMode:           "sensitive",
		SubjectKinds:       []SemanticSubjectKind{materialize.SubjectRelation, materialize.SubjectEntity},
	}); !IsWorkbenchError(err, WorkbenchErrorInputInvalid) {
		t.Fatalf("unordered symbol kind filter error = %v", err)
	}
}

func TestReplacementRejectsOversizedSnapshotWithoutMutation(t *testing.T) {
	probe := New(BuildInfo{})
	probeOpened := openWorkbench(t, probe, projectCompileInput(`project p "P" {}`))
	_, probeSnapshot, err := probe.acquireSnapshot(context.Background(), probeOpened.DocumentGeneration)
	if err != nil {
		t.Fatal(err)
	}

	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "replace-retained", MaxRetainedBytes: probeSnapshot.retained + 1_024}})
	opened := openWorkbench(t, instance, projectCompileInput(`project p "P" {}`))
	large := projectCompileInput("project p \"P\" {}\n" + strings.Repeat("# retained source bytes\n", 8_000))
	if _, err := instance.ReplaceSourceTree(context.Background(), ReplaceSourceTreeInput{ExpectedGeneration: opened.DocumentGeneration, CompileInput: large}); !IsWorkbenchError(err, WorkbenchErrorLimitExceeded) {
		t.Fatalf("oversized replacement error = %v", err)
	}
	if _, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits}); err != nil {
		t.Fatalf("oversized replacement mutated current generation: %v", err)
	}
}

func TestReplacementEvictsAnotherDocumentBeforeSwappingProtectedHandle(t *testing.T) {
	smallInput := projectCompileInput(`project p "P" {}`)
	largeInput := projectCompileInput("project p \"P\" {}\n" + strings.Repeat("# retained replacement bytes\n", 2_000))
	probe := New(BuildInfo{})
	largeOpened := openWorkbench(t, probe, largeInput)
	_, largeSnapshot, err := probe.acquireSnapshot(context.Background(), largeOpened.DocumentGeneration)
	if err != nil {
		t.Fatal(err)
	}
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "replace-eviction", MaxDocuments: 3, MaxRetainedBytes: largeSnapshot.retained}})
	victim := openWorkbench(t, instance, smallInput)
	protected := openWorkbench(t, instance, smallInput)
	if _, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: victim.DocumentGeneration, Limits: generousWorkbenchLimits}); err != nil {
		t.Fatalf("victim was not live immediately before replacement: %v", err)
	}
	replaced, err := instance.ReplaceSourceTree(context.Background(), ReplaceSourceTreeInput{ExpectedGeneration: protected.DocumentGeneration, CompileInput: largeInput})
	if err != nil || replaced.DocumentGeneration.Value != protected.DocumentGeneration.Value+1 {
		t.Fatalf("replacement with eviction = %+v, %v", replaced, err)
	}
	if _, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: victim.DocumentGeneration, Limits: generousWorkbenchLimits}); !IsWorkbenchError(err, WorkbenchErrorHandleInvalid) {
		t.Fatalf("replacement victim survived = %v", err)
	}
	if _, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: replaced.DocumentGeneration, Limits: generousWorkbenchLimits}); err != nil {
		t.Fatalf("protected replacement was evicted: %v", err)
	}
}

func TestGraphOnlineTraversalHandlesCyclesAndSortedMultipleRoots(t *testing.T) {
	const source = `project p "Project" {}
layers {
  app "Application" @1
}
entity_type service "Service" {
  representation shape rect
}
relation_type link "Link" dependency {
  duplicate_policy allow
  from source types [service] layers [app]
  to target types [service] layers [app]
  label "links"
}
entities service @app {
  a "A"
  b "B"
  c "C"
  d "D"
}
relations link {
  r0: a -> b
  r1: b -> c
  r2: c -> a
}
`
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "cycle-graph"}})
	opened := openWorkbench(t, instance, projectCompileInput(source))
	a := "ldl:project:p:entity:a"
	c := "ldl:project:p:entity:c"
	roots := []string{a, c}

	neighbors, err := instance.GetNeighbors(context.Background(), GetNeighborsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, EntityAddresses: roots, Direction: "both", Depth: 2})
	if err != nil || len(neighbors.Items) != 6 {
		t.Fatalf("cyclic neighbors = %+v, %v", neighbors, err)
	}
	for index, item := range neighbors.Items {
		if item.TraversalIndex != uint64(index) {
			t.Fatalf("neighbor traversal index %d = %d", index, item.TraversalIndex)
		}
	}
	if neighbors.Items[0].SourceEntityAddress != a || neighbors.Items[0].Direction != "incoming" || neighbors.Items[1].SourceEntityAddress != a || neighbors.Items[1].Direction != "outgoing" {
		t.Fatalf("incoming-before-outgoing order = %+v", neighbors.Items[:2])
	}

	subgraph, err := instance.InspectSubgraph(context.Background(), InspectSubgraphInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, RootAddresses: roots, Depth: 2})
	if err != nil || len(subgraph.Items) != 6 || len(subgraph.Relations) != 3 {
		t.Fatalf("cyclic subgraph = %+v, %v", subgraph, err)
	}
	want := []string{
		"ldl:project:p:entity:a",
		"ldl:project:p:entity:c",
		"ldl:project:p:relation:r2",
		"ldl:project:p:relation:r0",
		"ldl:project:p:entity:b",
		"ldl:project:p:relation:r1",
	}
	for index, item := range subgraph.Items {
		if item.TraversalIndex != uint64(index) || item.Subject.Address != want[index] {
			t.Fatalf("subgraph item %d = %+v, want %s", index, item, want[index])
		}
	}
	d := "ldl:project:p:entity:d"
	seedPage, err := instance.InspectSubgraph(context.Background(), InspectSubgraphInput{
		DocumentGeneration: opened.DocumentGeneration,
		Limits:             WorkbenchLimits{MaxItems: 1, MaxOutputBytes: generousWorkbenchLimits.MaxOutputBytes},
		RootAddresses:      []string{a, c, d},
		Depth:              1,
	})
	if err != nil || len(seedPage.Items) != 1 || seedPage.Items[0].Subject.Address != a || seedPage.Page.NextCursor == nil {
		t.Fatalf("bounded multi-root seed page = %+v, %v", seedPage, err)
	}

	foundRootByteBound := false
	for outputBytes := int64(500); outputBytes < 2_000; outputBytes += 25 {
		result, readErr := instance.InspectSubgraph(context.Background(), InspectSubgraphInput{
			DocumentGeneration: opened.DocumentGeneration,
			Limits:             WorkbenchLimits{MaxItems: 10, MaxOutputBytes: outputBytes},
			RootAddresses:      roots,
			Depth:              0,
		})
		if readErr == nil && len(result.Items) == 1 && result.Page.NextCursor != nil && result.Page.Truncation == TruncationOutputByteLimit {
			foundRootByteBound = true
			break
		}
	}
	if !foundRootByteBound {
		t.Fatal("two root items did not produce a byte-bounded first page")
	}

	pageCancelled := newReadBoundaryCancellation(workbenchReadSubgraphPage, a)
	if _, err := instance.InspectSubgraph(pageCancelled, InspectSubgraphInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, RootAddresses: []string{a}, Depth: 0}); !IsWorkbenchError(err, WorkbenchErrorCancelled) {
		t.Fatalf("subgraph page cancellation error = %v", err)
	}
	nodeCancelled := newReadBoundaryCancellation(workbenchReadSubgraphNode, a)
	if _, err := instance.InspectSubgraph(nodeCancelled, InspectSubgraphInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, RootAddresses: []string{a}, Depth: 2}); !IsWorkbenchError(err, WorkbenchErrorCancelled) {
		t.Fatalf("subgraph node cancellation error = %v", err)
	}
	edgeCancelled := newReadBoundaryCancellation(workbenchReadNeighborEdge, "ldl:project:p:relation:r2")
	if _, err := instance.GetNeighbors(edgeCancelled, GetNeighborsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, EntityAddresses: []string{a}, Direction: "both", Depth: 2}); !IsWorkbenchError(err, WorkbenchErrorCancelled) {
		t.Fatalf("neighbor edge cancellation error = %v", err)
	}
	subgraphEdgeCancelled := newReadBoundaryCancellation(workbenchReadSubgraphEdge, "ldl:project:p:relation:r2")
	if _, err := instance.InspectSubgraph(subgraphEdgeCancelled, InspectSubgraphInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, RootAddresses: []string{a}, Depth: 2}); !IsWorkbenchError(err, WorkbenchErrorCancelled) {
		t.Fatalf("subgraph edge cancellation error = %v", err)
	}
	if _, err := instance.InspectSubgraph(context.Background(), InspectSubgraphInput{
		DocumentGeneration: opened.DocumentGeneration,
		Limits:             WorkbenchLimits{MaxItems: 1, MaxOutputBytes: 1},
		RootAddresses:      roots,
		Depth:              2,
	}); !IsWorkbenchError(err, WorkbenchErrorLimitExceeded) {
		t.Fatalf("subgraph output floor error = %v", err)
	}

	cancelled := newReadBoundaryCancellation(workbenchReadNeighborNode, "ldl:project:p:entity:b")
	if _, err := instance.GetNeighbors(cancelled, GetNeighborsInput{DocumentGeneration: opened.DocumentGeneration, Limits: generousWorkbenchLimits, EntityAddresses: roots, Direction: "both", Depth: 2}); !IsWorkbenchError(err, WorkbenchErrorCancelled) {
		t.Fatalf("mid-traversal cancellation error = %v", err)
	}
}

func assertGeneratedReadModulesBytes(t *testing.T, result ReadModulesResult) {
	t.Helper()
	wire := mapReadModulesResult(result)
	if _, err := engineprotocol.EncodeReadModulesResult(wire); err != nil {
		t.Fatalf("generated ReadModulesResult rejected facade output: %v", err)
	}
	wire.Page.ReturnedBytes = "0"
	var control bytes.Buffer
	encoder := json.NewEncoder(&control)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(wire); err != nil {
		t.Fatal(err)
	}
	attachments := 0
	for _, item := range result.Items {
		attachments += len(item.SourceChunk.Bytes)
	}
	want := int64(control.Len() - 1 + attachments)
	if result.Page.ReturnedBytes != want {
		t.Fatalf("returned_bytes = %d, independently encoded logical bytes = %d", result.Page.ReturnedBytes, want)
	}
}

func mapReadModulesResult(input ReadModulesResult) engineprotocol.ReadModulesResult {
	generation := mapDocumentGeneration(input.DocumentGeneration)
	result := engineprotocol.ReadModulesResult{DocumentGeneration: generation, Items: make([]engineprotocol.ModuleContentReadItem, 0, len(input.Items))}
	for _, item := range input.Items {
		origin := semantic.SourceOrigin{Kind: semantic.OriginKind(item.Module.Origin.Kind)}
		if item.Module.Origin.PackAddress != "" {
			pack := semantic.PackRootAddress(item.Module.Origin.PackAddress)
			origin.PackAddress = &pack
		}
		chunk := item.SourceChunk
		result.Items = append(result.Items, engineprotocol.ModuleContentReadItem{
			Module: semantic.ModuleRef{ModulePath: item.Module.ModulePath, Origin: origin},
			SourceChunk: engineprotocol.BoundedTextChunk{
				Blob:       protocolcommon.BlobRef{BlobID: chunk.Blob.BlobID, Digest: protocolcommon.Digest(chunk.Blob.Digest), Lifetime: protocolcommon.BlobLifetime(chunk.Blob.Lifetime), MediaType: chunk.Blob.MediaType, Size: protocolcommon.CanonicalUint64(strconv.FormatInt(chunk.Blob.Size, 10))},
				FullDigest: protocolcommon.Digest(chunk.FullDigest), Offset: protocolcommon.CanonicalUint64(strconv.FormatInt(chunk.Offset, 10)), TotalBytes: protocolcommon.CanonicalUint64(strconv.FormatInt(chunk.TotalBytes, 10)),
			},
		})
	}
	page := engineprotocol.ModuleContentPageInfo{ReturnedBytes: engineprotocol.LogicalResponseByteCount(strconv.FormatInt(input.Page.ReturnedBytes, 10)), ReturnedItems: protocolcommon.CanonicalUint64(strconv.FormatInt(input.Page.ReturnedItems, 10)), Truncation: engineprotocol.TruncationOutcome(input.Page.Truncation)}
	if input.Page.NextCursor != nil {
		page.NextCursor = &engineprotocol.ModuleContentCursor{DocumentGeneration: generation, Value: input.Page.NextCursor.Value}
	}
	result.Page = page
	return result
}

func mapNeighborResult(input GetNeighborsResult) engineprotocol.GetNeighborsResult {
	generation := mapDocumentGeneration(input.DocumentGeneration)
	result := engineprotocol.GetNeighborsResult{DocumentGeneration: generation, Items: make([]engineprotocol.NeighborReadItem, 0, len(input.Items))}
	for _, item := range input.Items {
		result.Items = append(result.Items, engineprotocol.NeighborReadItem{
			Depth:               item.Depth,
			Direction:           item.Direction,
			EntityAddress:       semantic.EntityAddress(item.EntityAddress),
			RelationAddress:     semantic.RelationAddress(item.RelationAddress),
			SourceEntityAddress: semantic.EntityAddress(item.SourceEntityAddress),
			TraversalIndex:      protocolcommon.CanonicalUint64(strconv.FormatUint(item.TraversalIndex, 10)),
		})
	}
	page := engineprotocol.NeighborPageInfo{ReturnedBytes: engineprotocol.LogicalResponseByteCount(strconv.FormatInt(input.Page.ReturnedBytes, 10)), ReturnedItems: protocolcommon.CanonicalUint64(strconv.FormatInt(input.Page.ReturnedItems, 10)), Truncation: engineprotocol.TruncationOutcome(input.Page.Truncation)}
	if input.Page.NextCursor != nil {
		page.NextCursor = &engineprotocol.NeighborCursor{DocumentGeneration: generation, Value: input.Page.NextCursor.Value}
	}
	result.Page = page
	return result
}

func mapDocumentGeneration(input DocumentGeneration) engineprotocol.DocumentGeneration {
	return engineprotocol.DocumentGeneration{
		DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: protocolcommon.EndpointInstanceID(input.DocumentHandle.EndpointInstanceID), Value: input.DocumentHandle.Value},
		Value:          protocolcommon.CanonicalUint64(strconv.FormatUint(input.Value, 10)),
	}
}
