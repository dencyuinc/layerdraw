// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"math"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/index"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
)

func TestNestedOutputMapperGuardsRejectInvalidDomainValues(t *testing.T) {
	t.Parallel()
	projectOrigin := resolve.SourceOrigin{Kind: resolve.OriginProject}
	invalidOrigin := resolve.SourceOrigin{Kind: "invalid"}
	validModule := index.ModuleRef{Origin: projectOrigin, ModulePath: "main.ldl"}
	invalidModule := index.ModuleRef{Origin: invalidOrigin, ModulePath: "main.ldl"}
	validRange := resolve.SourceRange{Origin: projectOrigin, ModulePath: "main.ldl", StartByte: 0, EndByte: 1}
	invalidRange := resolve.SourceRange{Origin: projectOrigin, ModulePath: "main.ldl", StartByte: 2, EndByte: 1}

	sourceMaps := []engine.SourceMap{
		{Files: []index.SourceFileRecord{{Origin: invalidOrigin}}},
		{Files: []index.SourceFileRecord{{Origin: projectOrigin, ByteLength: -1}}},
		{Subjects: []index.SourceSubjectRecord{{Module: &invalidModule}}},
		{Subjects: []index.SourceSubjectRecord{{DeclarationRange: &invalidRange}}},
		{Subjects: []index.SourceSubjectRecord{{CommentRanges: []resolve.SourceRange{invalidRange}}}},
		{Bindings: []index.SourceBindingRecord{{Module: invalidModule, Range: validRange}}},
		{Bindings: []index.SourceBindingRecord{{Module: validModule, Range: invalidRange}}},
		{Exports: []index.ExportBindingRecord{{Module: invalidModule, Range: validRange}}},
		{Exports: []index.ExportBindingRecord{{Module: validModule, Range: invalidRange}}},
		{Assets: []index.SourceAssetRecord{{Origin: invalidOrigin, Range: validRange}}},
		{Assets: []index.SourceAssetRecord{{Origin: projectOrigin, Range: invalidRange}}},
		{Assets: []index.SourceAssetRecord{{Origin: projectOrigin, Range: validRange, ByteLength: -1}}},
	}
	for index, input := range sourceMaps {
		if _, err := mapSourceMap(input); err == nil {
			t.Fatalf("invalid source map %d was accepted", index)
		}
	}

	semanticIndexes := []engine.SemanticIndex{
		{Subjects: []index.SemanticSubject{{Module: &invalidModule}}},
		{References: []index.SemanticReference{{Range: invalidRange}}},
		{ScopedReads: index.ScopedReadIndexes{ByModule: []index.ScopeAddresses{{Module: invalidModule}}}},
	}
	for index, input := range semanticIndexes {
		if _, err := mapSemanticIndex(input); err == nil {
			t.Fatalf("invalid semantic index %d was accepted", index)
		}
	}
	search := []engine.SearchDocument{{Fields: []index.SearchField{{SourceRef: &invalidRange}}}}
	if _, err := mapSearchDocuments(search); err == nil {
		t.Fatal("invalid search source range was accepted")
	}
	diagnostics := []engine.Diagnostic{{Related: []resolve.DiagnosticRelated{{Range: &invalidRange}}}}
	if _, err := mapDiagnostics(diagnostics); err == nil {
		t.Fatal("invalid related diagnostic range was accepted")
	}

	if _, _, err := mapNormalizedArtifact(engine.Snapshot{CompileOutput: engine.CompileOutput{Mode: engine.CompileProject}}, nil); err == nil {
		t.Fatal("incomplete Project success union was accepted")
	}
	if _, _, err := mapNormalizedArtifact(engine.Snapshot{CompileOutput: engine.CompileOutput{Mode: engine.CompilePack}}, nil); err == nil {
		t.Fatal("incomplete Pack success union was accepted")
	}
	if _, err := mapEffectiveLimits(engine.ResourceLimits{}); err == nil {
		t.Fatal("non-positive effective limits were accepted")
	}
	duplicate := newOutputBlob("duplicate", []byte("a"))
	if err := validateUniqueOutputBlobs([]OutputBlob{duplicate, duplicate}); err == nil {
		t.Fatal("duplicate output blob was accepted")
	}
	invalidBlob := newOutputBlob("invalid", []byte("a"))
	invalidBlob.Ref.MediaType = ""
	if err := validateUniqueOutputBlobs([]OutputBlob{invalidBlob}); err == nil {
		t.Fatal("invalid output metadata was accepted")
	}
}

func TestCompileInputAndBudgetFailureGuards(t *testing.T) {
	t.Parallel()
	value := []byte("project p \"Project\" {}")
	request := compileRequest(value)
	negotiated := compileContext(t)
	input := request.Payload
	mapped, diagnostics, failure := mapCompileInput(context.Background(), negotiated, input, sourceFor(input.ProjectSourceTree[0].Blob, value))
	if failure != nil || len(diagnostics) != 0 || len(mapped.ProjectSourceTree) != 1 {
		t.Fatalf("valid map failed: mapped=%+v diagnostics=%+v failure=%+v", mapped, diagnostics, failure)
	}

	duplicate := input
	duplicate.ProjectSourceTree = append(duplicate.ProjectSourceTree, duplicate.ProjectSourceTree[0])
	if _, diagnostics, failure := mapCompileInput(context.Background(), negotiated, duplicate, &memoryBlobSource{}); failure != nil || len(diagnostics) == 0 {
		t.Fatalf("duplicate logical input was not rejected: diagnostics=%+v failure=%+v", diagnostics, failure)
	}
	if _, diagnostics, failure := mapCompileInput(context.Background(), negotiated, input, &memoryBlobSource{}); failure == nil || len(diagnostics) != 0 {
		t.Fatalf("missing blob was not rejected: diagnostics=%+v failure=%+v", diagnostics, failure)
	}
	oversized := protocolcommon.BlobRef{BlobID: "oversized", Lifetime: protocolcommon.BlobLifetimeRequest, Size: protocolcommon.CanonicalUint64("9223372036854775808")}
	if _, _, failure := buildBlobRequirements(context.Background(), []blobUse{{ref: oversized}}); failure == nil || failure.Code != FailureCompileBlobOversized {
		t.Fatalf("oversized requirement failure=%+v", failure)
	}
	maximum := protocolcommon.BlobRef{BlobID: "a", Lifetime: protocolcommon.BlobLifetimeRequest, Size: protocolcommon.CanonicalUint64("9223372036854775807")}
	one := protocolcommon.BlobRef{BlobID: "b", Lifetime: protocolcommon.BlobLifetimeRequest, Size: protocolcommon.CanonicalUint64("1")}
	if _, _, failure := buildBlobRequirements(context.Background(), []blobUse{{ref: maximum}, {ref: one}}); failure == nil || failure.Code != FailureCompileBlobOversized {
		t.Fatalf("aggregate overflow failure=%+v", failure)
	}
	conflict := maximum
	conflict.MediaType = "different"
	if _, _, failure := buildBlobRequirements(context.Background(), []blobUse{{ref: maximum}, {ref: conflict}}); failure == nil || failure.Code != FailureCompileConflictingBlobRef {
		t.Fatalf("conflicting requirement failure=%+v", failure)
	}

	limits := engine.DefaultResourceLimits()
	limits.MaxProjectSourceFiles = 0
	if _, failure := compileAdmissionBudget(context.Background(), input, limits, 1, int64(len(value))); failure == nil || failure.Code != engine.ErrorCodeProjectSourceFilesExceeded {
		t.Fatalf("project file budget failure=%+v", failure)
	}
	limits = engine.DefaultResourceLimits()
	limits.MaxProjectSourceBytes = 1
	if _, failure := compileAdmissionBudget(context.Background(), input, limits, 1, int64(len(value))); failure == nil || failure.Code != engine.ErrorCodeProjectSourceBytesExceeded {
		t.Fatalf("project byte budget failure=%+v", failure)
	}
	invalidSizeInput := input
	invalidSizeInput.ProjectSourceTree[0].Blob.Size = protocolcommon.CanonicalUint64("not-an-integer")
	if _, failure := compileAdmissionBudget(context.Background(), invalidSizeInput, engine.DefaultResourceLimits(), 1, 0); failure == nil || failure.Code != FailureCompileInvalidRequest {
		t.Fatalf("invalid size budget failure=%+v", failure)
	}
	if got := saturatedAdd(math.MaxInt64, 1); got != math.MaxInt64 {
		t.Fatalf("saturated add=%d", got)
	}
}
