// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package index

import (
	"reflect"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
)

func TestBuildFailurePathsPublishNothing(t *testing.T) {
	valid := indexProject(t, indexFixture)
	brokenStages := validatedStages(t, valid)
	brokenStages.Resolve.DeclarationSources = nil
	if got := Build(Input{Materialized: materialize.Compile(brokenStages)}); !got.HasErrors || len(got.Snapshot().SourceMap.Subjects) != 0 {
		t.Fatalf("missing declarations published=%+v", got.Snapshot())
	}

	invalidMaterializationStages := validatedStages(t, valid)
	invalidMaterializationStages.Resolved.SourceFiles[0].Bytes = []byte("wrong")
	failed := materialize.Compile(invalidMaterializationStages)
	if !failed.HasErrors {
		t.Fatal("fixture did not produce rejected materialization")
	}
	if got := Build(Input{Materialized: failed}); !got.HasErrors || len(got.Snapshot().SemanticIndex.Subjects) != 0 {
		t.Fatalf("rejected materialization published=%+v", got.Snapshot())
	}

	if _, err := buildSearchDocuments(materialize.Snapshot{}, SourceMapV1{}, resolve.Result{}); err == nil {
		t.Fatal("open Project search input accepted")
	}
	if documents, err := buildSearchDocuments(materialize.Snapshot{Pack: &materialize.NormalizedPackArtifact{}}, SourceMapV1{}, resolve.Result{}); err != nil || documents != nil {
		t.Fatalf("Pack search=%+v err=%v", documents, err)
	}
}

func TestIndexClosedHelperBranches(t *testing.T) {
	if subjectKindRank("unknown") != 99 {
		t.Fatal("unknown generated subject kind accepted")
	}
	if got := adjacency(materialize.Input{}); len(got) != 0 {
		t.Fatalf("nil graph adjacency=%+v", got)
	}
	if emptySlice(nil) == nil || !reflect.DeepEqual(emptySlice([]string{"x"}), []string{"x"}) || cloneStrings(nil) == nil || !reflect.DeepEqual(cloneStrings([]string{"x"}), []string{"x"}) {
		t.Fatal("closed slice helpers failed")
	}
	if cloneRange(nil) != nil {
		t.Fatal("nil range materialized")
	}
	if cloneModuleRef(nil) != nil || sourceFileLength(nil, ModuleRef{}) != 0 {
		t.Fatal("nil source helper produced provenance")
	}
	rangeValue := &resolve.SourceRange{ModulePath: "x"}
	if cloneRange(rangeValue) == rangeValue {
		t.Fatal("range clone aliased")
	}

	project := indexProject(t, indexFixture)
	projectStages := validatedStages(t, project)
	projectModule := projectStages.Resolve.Declarations[0].Module
	if got := moduleRef(projectStages, projectModule); got.Origin.Kind != resolve.OriginProject {
		t.Fatalf("Project module=%+v", got)
	}
	pack := indexPack(t, packIndexFixture)
	packStages := validatedStages(t, pack)
	packModule := packStages.Resolve.Declarations[0].Module
	if got := moduleRef(packStages, packModule); got.Origin.PackAddress != packStages.Resolve.RootAddress {
		t.Fatalf("Pack root module=%+v", got)
	}
	dependencyInput := packStages
	dependencyInput.Resolve.Mode = resolve.CompileProject
	dependencyInput.Resolved.SelectedClosure = []materialize.ResolvedPackClosure{{ResolvedPackSummary: materialize.ResolvedPackSummary{CanonicalID: "pub/core", Address: "ldl:pack:pub:core"}}}
	if got := moduleRef(dependencyInput, packModule); got.Origin.PackAddress != "ldl:pack:pub:core" {
		t.Fatalf("dependency module=%+v", got)
	}

	projectOrigin := resolve.SourceOrigin{Kind: resolve.OriginProject}
	packOriginA := resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: "a"}
	packOriginB := resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: "b"}
	if !lessModule(projectOrigin, "z", packOriginA, "a") || !lessModule(packOriginA, "z", packOriginB, "a") || !lessModule(packOriginA, "a", packOriginA, "z") {
		t.Fatal("module ordering failed")
	}

	module := resolve.ResolvedModule{}
	if ranges := commentRanges(module, syntax.Span{}, ModuleRef{}); len(ranges) != 0 {
		t.Fatalf("empty module comments=%+v", ranges)
	}
	module.Path = "x.ldl"
	module.File.Tokens = []syntax.Token{{Kind: syntax.TokenIdentifier, Span: syntax.Span{Start: 0, End: 1}}}
	if ranges := commentRanges(module, syntax.Span{Start: 2, End: 3}, ModuleRef{ModulePath: "x.ldl"}); len(ranges) != 0 {
		t.Fatalf("non-doc comments=%+v", ranges)
	}

	sourceMap := SourceMapV1{Subjects: []SourceSubjectRecord{{Address: "b"}, {Address: "a"}}, Bindings: []SourceBindingRecord{{SourceAddress: "b", Range: resolve.SourceRange{StartByte: 2}}, {SourceAddress: "b", Range: resolve.SourceRange{StartByte: 1}}, {SourceAddress: "a"}}, Exports: []ExportBindingRecord{{Module: ModuleRef{ModulePath: "b"}}, {Module: ModuleRef{ModulePath: "a"}}}, Assets: []SourceAssetRecord{{SubjectAddress: "b", Locator: "b"}, {SubjectAddress: "a", Locator: "a"}}}
	sortSourceMap(&sourceMap, resolve.Result{})
	semantic := SemanticIndexV1{Subjects: []SemanticSubject{{Address: "b"}, {Address: "a"}}, References: []SemanticReference{{SourceAddress: "b", Range: resolve.SourceRange{StartByte: 2}}, {SourceAddress: "b", Range: resolve.SourceRange{StartByte: 1}}, {SourceAddress: "a"}}, Adjacency: []AdjacencyRecord{{EntityAddress: "b"}, {EntityAddress: "a"}}, Dependencies: []DependencyRecord{{SubjectAddress: "b"}, {SubjectAddress: "a"}}}
	sortSemantic(&semantic, resolve.Result{})
	if sourceMap.Subjects[0].Address != "a" || semantic.Subjects[0].Address != "a" {
		t.Fatal("fallback index ordering failed")
	}
	projectAddress := "ldl:project:z:reference:a"
	packAddress := "ldl:pack:a:a:reference:a"
	sourceMap.Bindings = []SourceBindingRecord{
		{SourceAddress: projectAddress, TargetAddress: packAddress, TargetKind: materialize.SubjectReference, TargetOwnerAddress: packAddress, Via: "z", Range: resolve.SourceRange{StartByte: 1, EndByte: 3}},
		{SourceAddress: projectAddress, TargetAddress: projectAddress, TargetKind: materialize.SubjectReference, TargetOwnerAddress: packAddress, Via: "z", Range: resolve.SourceRange{StartByte: 1, EndByte: 3}},
		{SourceAddress: projectAddress, TargetAddress: projectAddress, TargetKind: materialize.SubjectEntity, TargetOwnerAddress: packAddress, Via: "z", Range: resolve.SourceRange{StartByte: 1, EndByte: 3}},
		{SourceAddress: projectAddress, TargetAddress: projectAddress, TargetKind: materialize.SubjectEntity, TargetOwnerAddress: projectAddress, Via: "z", Range: resolve.SourceRange{StartByte: 1, EndByte: 3}},
		{SourceAddress: projectAddress, TargetAddress: projectAddress, TargetKind: materialize.SubjectEntity, TargetOwnerAddress: projectAddress, Via: "a", Range: resolve.SourceRange{StartByte: 1, EndByte: 3}},
		{SourceAddress: projectAddress, TargetAddress: projectAddress, TargetKind: materialize.SubjectEntity, TargetOwnerAddress: projectAddress, Via: "a", Range: resolve.SourceRange{StartByte: 1, EndByte: 2}},
	}
	sourceMap.Exports = []ExportBindingRecord{{PublicName: "z", Module: ModuleRef{ModulePath: "same"}, Range: resolve.SourceRange{StartByte: 1}}, {PublicName: "a", Module: ModuleRef{ModulePath: "same"}, Range: resolve.SourceRange{StartByte: 1}}}
	sourceMap.Assets = []SourceAssetRecord{{SubjectAddress: projectAddress, Locator: "z"}, {SubjectAddress: projectAddress, Locator: "a"}}
	sortSourceMap(&sourceMap, resolve.Result{})
	semantic.References = []SemanticReference{
		{SourceAddress: projectAddress, TargetAddress: packAddress, TargetKind: materialize.SubjectReference, Via: "z", Range: resolve.SourceRange{StartByte: 1, EndByte: 3}},
		{SourceAddress: projectAddress, TargetAddress: projectAddress, TargetKind: materialize.SubjectReference, Via: "z", Range: resolve.SourceRange{StartByte: 1, EndByte: 3}},
		{SourceAddress: projectAddress, TargetAddress: projectAddress, TargetKind: materialize.SubjectEntity, Via: "z", Range: resolve.SourceRange{StartByte: 1, EndByte: 3}},
		{SourceAddress: projectAddress, TargetAddress: projectAddress, TargetKind: materialize.SubjectEntity, Via: "a", Range: resolve.SourceRange{StartByte: 1, EndByte: 3}},
		{SourceAddress: projectAddress, TargetAddress: projectAddress, TargetKind: materialize.SubjectEntity, Via: "a", Range: resolve.SourceRange{StartByte: 1, EndByte: 2}},
	}
	sortSemantic(&semantic, resolve.Result{})

	for _, scalar := range []materialize.Scalar{{Type: definition.ScalarInteger, Int: 4}, {Type: definition.ScalarBoolean, Bool: true}, {Type: "unknown"}} {
		_ = scalarText(scalar)
	}
	document := SearchDocument{SchemaVersion: 1, SubjectAddress: "x", SubjectKind: materialize.SubjectReference, Fields: []SearchField{{FieldPath: "text", Text: "x"}}}
	if hash, err := searchContentHash(document); err != nil || hash == "" {
		t.Fatalf("search hash=%q err=%v", hash, err)
	}

	cloned := deepClone(struct {
		Pointer *string
		Value   any
		Values  map[string][]string
	}{Pointer: stringTestPointer("x"), Value: []string{"x"}, Values: map[string][]string{"k": {"v"}}})
	if cloned.Pointer == nil || cloned.Value == nil || cloned.Values["k"][0] != "v" || cloneValue(reflect.Value{}).IsValid() {
		t.Fatal("defensive clone branches failed")
	}
}

func stringTestPointer(value string) *string { return &value }
