// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"context"
	"testing"
)

func FuzzWorkbenchMalformedHandle(f *testing.F) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "fuzz-endpoint"}})
	opened, err := instance.OpenDocument(context.Background(), OpenDocumentInput{CompileInput: projectCompileInput(`project p "P" {}`), RequestedLimits: generousWorkbenchLimits})
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range []string{"", "document_", "document_bad", "document_abcdefghijklmnop", "\x00"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		generation := opened.DocumentGeneration
		generation.DocumentHandle.Value = value
		_, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: generation, Limits: generousWorkbenchLimits})
		if !IsWorkbenchError(err, WorkbenchErrorHandleInvalid) {
			t.Fatalf("value %q error = %v", value, err)
		}
	})
}

func FuzzWorkbenchMalformedCursor(f *testing.F) {
	instance := New(BuildInfo{})
	opened, err := instance.OpenDocument(context.Background(), OpenDocumentInput{CompileInput: projectTreeCompileInput(map[string][]byte{"document.ldl": []byte("import { t } from \"./t.ldl\"\nproject p \"P\" {}"), "t.ldl": []byte("entity_type t \"T\" { representation shape rect }\nexport { t }")}), RequestedLimits: generousWorkbenchLimits})
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range []string{"", listModulesCursorPrefix, listModulesCursorPrefix + "bad", "find_symbols_cursor_abcdefghijklmnop"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		cursor := &Cursor{DocumentGeneration: opened.DocumentGeneration, Value: value}
		_, err := instance.ListModules(context.Background(), ListModulesInput{DocumentGeneration: opened.DocumentGeneration, Limits: WorkbenchLimits{MaxItems: 1, MaxOutputBytes: 8_000}, Cursor: cursor})
		if !IsWorkbenchError(err, WorkbenchErrorCursorInvalid) {
			t.Fatalf("cursor %q error = %v", value, err)
		}
	})
}

func FuzzWorkbenchSourceRanges(f *testing.F) {
	instance := New(BuildInfo{})
	opened, err := instance.OpenDocument(context.Background(), OpenDocumentInput{CompileInput: projectCompileInput(`project p "P" {}`), RequestedLimits: generousWorkbenchLimits})
	if err != nil {
		f.Fatal(err)
	}
	_, snapshot, err := instance.acquireSnapshot(context.Background(), opened.DocumentGeneration)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(int64(-1), int64(0))
	f.Add(int64(0), int64(1))
	f.Add(int64(100), int64(2))
	f.Fuzz(func(t *testing.T, start, end int64) {
		rangeValue := SourceRange{Origin: SourceOrigin{Kind: "project"}, ModulePath: "document.ldl"}
		if start >= 0 && start <= int64(^uint(0)>>1) {
			rangeValue.StartByte = int(start)
		} else {
			rangeValue.StartByte = -1
		}
		if end >= 0 && end <= int64(^uint(0)>>1) {
			rangeValue.EndByte = int(end)
		} else {
			rangeValue.EndByte = -1
		}
		_, _ = snapshot.source(rangeValue)
	})
}
