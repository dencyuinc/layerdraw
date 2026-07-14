// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestCompileProjectAndPackModes(t *testing.T) {
	t.Parallel()
	engine := New(BuildInfo{})
	tests := []struct {
		name  string
		input CompileInput
		check func(*testing.T, Snapshot)
	}{
		{
			name:  "single module Project",
			input: projectCompileInput(`project p "Project" {}`),
			check: func(t *testing.T, snapshot Snapshot) {
				if snapshot.NormalizedDocument == nil || snapshot.NormalizedPackArtifact != nil || snapshot.GraphHash == nil {
					t.Fatalf("Project envelope is incomplete: %+v", snapshot.CompileOutput)
				}
			},
		},
		{
			name: "multi module Project",
			input: projectTreeCompileInput(map[string][]byte{
				"document.ldl": []byte("import { service } from \"./types.ldl\"\nproject p \"Project\" {}\n"),
				"types.ldl":    []byte("entity_type service \"Service\" {\n  representation shape rect\n}\nexport { service }\n"),
			}),
			check: func(t *testing.T, snapshot Snapshot) {
				if len(snapshot.LosslessSyntaxTree.Files) != 2 || len(snapshot.TypedAST.EntityTypes) != 1 {
					t.Fatalf("multi-module closure is incomplete: files=%d types=%d", len(snapshot.LosslessSyntaxTree.Files), len(snapshot.TypedAST.EntityTypes))
				}
			},
		},
		{
			name:  "installed Pack dependency Project",
			input: installedPackProjectInput(),
			check: func(t *testing.T, snapshot Snapshot) {
				if snapshot.NormalizedDocument == nil || len(snapshot.NormalizedDocument.Dependencies) != 1 || len(snapshot.TypedAST.EntityTypes) != 1 {
					t.Fatalf("installed Pack closure is incomplete: %+v", snapshot.NormalizedDocument)
				}
			},
		},
		{
			name:  "root Pack",
			input: rootPackInput(),
			check: func(t *testing.T, snapshot Snapshot) {
				if snapshot.NormalizedPackArtifact == nil || snapshot.NormalizedDocument != nil || snapshot.GraphHash != nil || len(snapshot.SearchDocuments) != 0 {
					t.Fatalf("Pack envelope leaked Project output: %+v", snapshot.CompileOutput)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result, err := engine.Compile(context.Background(), test.input)
			if err != nil || len(result.Diagnostics) != 0 {
				t.Fatalf("Compile() err=%v diagnostics=%+v", err, result.Diagnostics)
			}
			snapshot := result.Snapshot()
			if snapshot.DefinitionHash == "" || len(snapshot.CanonicalJSON) == 0 || len(snapshot.ArtifactJSON) == 0 || len(snapshot.StableAddresses) == 0 {
				t.Fatalf("published generation is incomplete: %+v", snapshot.CompileOutput)
			}
			test.check(t, snapshot)
		})
	}
}

func TestCompileAllDeclarationFamilies(t *testing.T) {
	t.Parallel()
	result, err := New(BuildInfo{}).Compile(context.Background(), projectCompileInput(allDeclarationsFixture))
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("Compile() err=%v diagnostics=%+v", err, result.Diagnostics)
	}
	snapshot := result.Snapshot()
	if len(snapshot.TypedAST.EntityTypes) != 1 || len(snapshot.TypedAST.RelationTypes) != 1 || len(snapshot.TypedAST.Layers) != 1 ||
		len(snapshot.TypedAST.Graph.Entities) != 2 || len(snapshot.TypedAST.Graph.Relations) != 1 || len(snapshot.CompiledQueryRecipes) != 1 ||
		len(snapshot.CompiledViewRecipes) != 1 || len(snapshot.CompiledExportRecipes) != 1 || len(snapshot.TypedAST.References) != 1 {
		t.Fatalf("declaration family missing from typed output: %+v", snapshot.TypedAST)
	}
	wantCapabilities := []AuthoringCapability{
		CapabilityProjectConfigure, CapabilitySchemaWrite, CapabilityGraphWrite,
		CapabilityQueryWrite, CapabilityViewWrite, CapabilityReferenceWrite,
	}
	got := []AuthoringCapability{}
	for _, classification := range snapshot.AuthoringSubjectClassification {
		if !slices.Contains(got, classification.Capability) {
			got = append(got, classification.Capability)
		}
	}
	for _, capability := range wantCapabilities {
		if !slices.Contains(got, capability) {
			t.Fatalf("missing authoring classification %q in %v", capability, got)
		}
	}
}

func TestCompileSemanticRejectionIsDeterministicAndPublishesNothing(t *testing.T) {
	t.Parallel()
	inputs := []CompileInput{
		projectTreeCompileInput(map[string][]byte{"z.ldl": []byte("entity_type duplicate \"A\" {}\n"), "document.ldl": []byte("project p \"Project\" {}\nentity_type duplicate \"B\" {}\nentity_type duplicate \"C\" {}\n")}),
		projectTreeCompileInput(map[string][]byte{"document.ldl": []byte("project p \"Project\" {}\nentity_type duplicate \"B\" {}\nentity_type duplicate \"C\" {}\n"), "z.ldl": []byte("entity_type duplicate \"A\" {}\n")}),
	}
	var baseline []Diagnostic
	for i, input := range inputs {
		result, err := New(BuildInfo{}).Compile(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
		assertRejectedOutput(t, result)
		if len(result.Diagnostics) == 0 {
			t.Fatal("semantic rejection returned no diagnostics")
		}
		if i == 0 {
			baseline = deepClone(result.Diagnostics)
		} else if !reflect.DeepEqual(baseline, result.Diagnostics) {
			t.Fatalf("diagnostics depend on input map order:\nfirst=%+v\nsecond=%+v", baseline, result.Diagnostics)
		}
	}
	rejected, err := New(BuildInfo{}).Compile(context.Background(), inputs[0])
	if err != nil {
		t.Fatal(err)
	}
	original := rejected.Diagnostics[0].Message
	rejected.Diagnostics[0].Message = "mutated"
	firstSnapshot := rejected.Snapshot()
	if firstSnapshot.Diagnostics[0].Message != original {
		t.Fatal("CompileResult diagnostic mutation leaked into Snapshot")
	}
	firstSnapshot.Diagnostics[0].Message = "mutated again"
	if rejected.Snapshot().Diagnostics[0].Message != original {
		t.Fatal("Snapshot diagnostic mutation leaked into a later Snapshot")
	}
}

func TestCompileInvalidClosedTrees(t *testing.T) {
	t.Parallel()
	missingMode := projectCompileInput(`project p "Project" {}`)
	missingMode.Mode = ""
	invalidEnvelope := projectCompileInput(`project p "Project" {}`)
	invalidEnvelope.ResolvedDependencies.Format = "bogus"
	duplicateInstall := installedPackProjectInput()
	duplicateInstall.ResolvedDependencies.Installs = append(duplicateInstall.ResolvedDependencies.Installs, duplicateInstall.ResolvedDependencies.Installs[0])
	duplicateFile := installedPackProjectInput()
	duplicateFile.ResolvedDependencies.Installs[0].Files = append(duplicateFile.ResolvedDependencies.Installs[0].Files, duplicateFile.ResolvedDependencies.Installs[0].Files[0])
	duplicateDependency := installedPackProjectInput()
	duplicateDependency.ResolvedDependencies.Installs[0].Dependencies = []ResolvedPackDependency{
		{LocalName: "same", InstallName: "schema"},
		{LocalName: "same", InstallName: "schema"},
	}
	inconsistent := installedPackProjectInput()
	inconsistent.InstalledPackTree["pack/schema/pack.ldl"] = []byte("different")
	unexpected := installedPackProjectInput()
	unexpected.InstalledPackTree["pack/schema/unlisted.txt"] = []byte("extra")
	duplicateAsset := projectCompileInput(assetFixture)
	asset := AssetInput{Origin: SourceOriginProject, Locator: "icon.png", Bytes: testPNG, Digest: digestBytes(testPNG), MediaType: "image/png", ByteLength: int64(len(testPNG))}
	duplicateAsset.ReferencedAssets = []AssetInput{asset, asset}
	for _, test := range []struct {
		name  string
		input CompileInput
	}{
		{name: "invalid syntax", input: projectCompileInput("project")},
		{name: "missing entry", input: projectTreeCompileInput(map[string][]byte{"other.ldl": []byte(`project p "Project" {}`)})},
		{name: "missing compile mode", input: missingMode},
		{name: "invalid empty resolved envelope", input: invalidEnvelope},
		{name: "duplicate install", input: duplicateInstall},
		{name: "duplicate pack file", input: duplicateFile},
		{name: "duplicate Pack dependency", input: duplicateDependency},
		{name: "inconsistent pack bytes", input: inconsistent},
		{name: "unexpected pack bytes", input: unexpected},
		{name: "duplicate asset", input: duplicateAsset},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result, err := New(BuildInfo{}).Compile(context.Background(), test.input)
			if err != nil {
				t.Fatalf("closed input must be a semantic rejection, got %v", err)
			}
			assertRejectedOutput(t, result)
			if len(result.Diagnostics) == 0 {
				t.Fatal("invalid closed input has no diagnostics")
			}
		})
	}
}

func TestCompileCancellationAtStartAndStageBoundaries(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := New(BuildInfo{}).Compile(ctx, projectCompileInput(`project p "Project" {}`))
	assertCancelled(t, result, err, stageStart)

	for _, target := range []compileStage{stagePreprocess, stageParse, stageResolve, stageDefinition, stageGraph, stageQuery, stageViewExport, stageMaterialize, stageIndex, stageComplete} {
		t.Run(string(target), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			hooked := &compileHookContext{Context: ctx, hook: func(stage compileStage) {
				if stage == target {
					cancel()
				}
			}}
			result, err := New(BuildInfo{}).Compile(hooked, projectCompileInput(allDeclarationsFixture))
			assertCancelled(t, result, err, target)
		})
	}
}

func TestCompileCancellationAfterStagesAndBeforePublication(t *testing.T) {
	t.Parallel()
	for _, target := range []compileStage{stagePreprocess, stageParse, stageResolve, stageDefinition, stageGraph, stageQuery, stageViewExport, stageMaterialize, stageIndex, stageComplete} {
		t.Run(string(target), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			hits := 0
			hooked := &compileHookContext{Context: ctx, hook: func(stage compileStage) {
				if stage == target {
					hits++
					if hits == 2 {
						cancel()
					}
				}
			}}
			result, err := New(BuildInfo{}).Compile(hooked, projectCompileInput(allDeclarationsFixture))
			assertCancelled(t, result, err, target)
		})
	}

	t.Run("after result clone", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		hits := 0
		hooked := &compileHookContext{Context: ctx, hook: func(stage compileStage) {
			if stage == stageComplete {
				hits++
				if hits == 3 {
					cancel()
				}
			}
		}}
		result, err := New(BuildInfo{}).Compile(hooked, projectCompileInput(allDeclarationsFixture))
		assertCancelled(t, result, err, stageComplete)
	})
}

func TestCompileCancellationDuringRejectedResultClone(t *testing.T) {
	t.Parallel()
	for _, trigger := range []int{3, 4} {
		t.Run(fmt.Sprintf("definition check %d", trigger), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			hits := 0
			hooked := &compileHookContext{Context: ctx, hook: func(stage compileStage) {
				if stage == stageDefinition {
					hits++
					if hits == trigger {
						cancel()
					}
				}
			}}
			input := projectCompileInput("project p \"Project\" {}\nentity_type service \"Service\" {}\n")
			result, err := New(BuildInfo{}).Compile(hooked, input)
			assertCancelled(t, result, err, stageDefinition)
		})
	}
}

func TestCompileCancellationDuringFacadeWork(t *testing.T) {
	t.Parallel()
	for _, target := range []compileStage{stagePreprocess, stageParse, stageMaterialize, stageComplete} {
		t.Run(string(target), func(t *testing.T) {
			ctx := &deferredCancelContext{Context: context.Background(), target: target}
			result, err := New(BuildInfo{}).Compile(ctx, installedPackProjectInput())
			assertCancelled(t, result, err, target)
		})
	}
}

func TestCompileInvariantFailureIsTypedAndEmpty(t *testing.T) {
	t.Parallel()
	ctx := &compileHookContext{Context: context.Background(), hook: func(stage compileStage) {
		if stage == stageGraph {
			panic("injected invariant")
		}
	}}
	result, err := New(BuildInfo{}).Compile(ctx, projectCompileInput(`project p "Project" {}`))
	if !reflect.DeepEqual(result, CompileResult{}) || !IsCompileError(err, ErrorCategoryInvariant) {
		t.Fatalf("invariant result=%+v err=%v", result, err)
	}
	var compileError *CompileError
	if !errors.As(err, &compileError) || compileError.Code != ErrorCodeInvariantFailure || compileError.Stage != string(stageGraph) {
		t.Fatalf("invariant error=%+v", compileError)
	}
}

func TestCompileEveryResourceLimit(t *testing.T) {
	t.Parallel()
	packFiles := projectCompileInput(`project p "Project" {}`)
	packFiles.InstalledPackTree = map[string][]byte{"a": []byte("a"), "b": []byte("b")}
	assets := projectCompileInput(`project p "Project" {}`)
	assets.ReferencedAssets = []AssetInput{{Bytes: []byte("a")}, {Bytes: []byte("b")}}
	assetBytes := projectCompileInput(`project p "Project" {}`)
	assetBytes.ReferencedAssets = []AssetInput{{Bytes: []byte("ab")}}
	declarations := projectCompileInput("project p \"Project\" {}\nentity_type one \"One\" {\n  representation shape rect\n}\n")
	duplicateDeclarations := projectCompileInput("project p \"Project\" {}\nentity_type repeated \"One\" {}\nentity_type repeated \"Two\" {}\n")
	raster := pngBytes(2, 2)
	rasterInput := projectCompileInput(assetFixture)
	rasterInput.ReferencedAssets = []AssetInput{{Origin: SourceOriginProject, Locator: "icon.png", Bytes: raster, Digest: digestBytes(raster), MediaType: "image/png", ByteLength: int64(len(raster))}}
	for _, test := range []struct {
		name  string
		code  string
		stage compileStage
		set   func(*CompileInput)
	}{
		{name: "project files", code: ErrorCodeProjectSourceFilesExceeded, stage: stagePreprocess, set: func(input *CompileInput) {
			input.ProjectSourceTree["second.ldl"] = []byte("// second")
			input.ResourceLimits.MaxProjectSourceFiles = 1
		}},
		{name: "project bytes", code: ErrorCodeProjectSourceBytesExceeded, stage: stagePreprocess, set: func(input *CompileInput) { input.ResourceLimits.MaxProjectSourceBytes = 1 }},
		{name: "pack files", code: ErrorCodePackFilesExceeded, stage: stagePreprocess, set: func(input *CompileInput) { *input = packFiles; input.ResourceLimits.MaxPackFiles = 1 }},
		{name: "pack metadata files", code: ErrorCodePackFilesExceeded, stage: stagePreprocess, set: func(input *CompileInput) {
			*input = installedPackProjectInput()
			input.ResourceLimits.MaxPackFiles = 1
		}},
		{name: "pack bytes", code: ErrorCodePackBytesExceeded, stage: stagePreprocess, set: func(input *CompileInput) {
			input.InstalledPackTree = map[string][]byte{"a": []byte("ab")}
			input.ResourceLimits.MaxPackBytes = 1
		}},
		{name: "assets", code: ErrorCodeAssetsExceeded, stage: stagePreprocess, set: func(input *CompileInput) { *input = assets; input.ResourceLimits.MaxAssets = 1 }},
		{name: "asset bytes", code: ErrorCodeAssetBytesExceeded, stage: stagePreprocess, set: func(input *CompileInput) { *input = assetBytes; input.ResourceLimits.MaxAssetBytes = 1 }},
		{name: "raster dimension", code: ErrorCodeRasterDimensionExceeded, stage: stagePreprocess, set: func(input *CompileInput) { *input = rasterInput; input.ResourceLimits.MaxRasterDimension = 1 }},
		{name: "raster pixels", code: ErrorCodeRasterPixelsExceeded, stage: stagePreprocess, set: func(input *CompileInput) {
			*input = rasterInput
			input.ResourceLimits.MaxRasterDimension = 10
			input.ResourceLimits.MaxRasterPixels = 3
		}},
		{name: "declarations", code: ErrorCodeDeclarationsExceeded, stage: stageParse, set: func(input *CompileInput) { *input = declarations; input.ResourceLimits.MaxDeclarations = 1 }},
		{name: "duplicate declarations", code: ErrorCodeDeclarationsExceeded, stage: stageParse, set: func(input *CompileInput) { *input = duplicateDeclarations; input.ResourceLimits.MaxDeclarations = 2 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := projectCompileInput(`project p "Project" {}`)
			test.set(&input)
			result, err := New(BuildInfo{}).Compile(context.Background(), input)
			if !reflect.DeepEqual(result, CompileResult{}) || !IsCompileError(err, ErrorCategoryResource) {
				t.Fatalf("resource result=%+v err=%v", result, err)
			}
			var compileError *CompileError
			if !errors.As(err, &compileError) || compileError.Code != test.code || compileError.Stage != string(test.stage) || compileError.Observed <= compileError.Limit {
				t.Fatalf("resource error=%+v", compileError)
			}
		})
	}

	invalid := projectCompileInput(`project p "Project" {}`)
	invalid.ResourceLimits.MaxAssets = -1
	result, err := New(BuildInfo{}).Compile(context.Background(), invalid)
	var compileError *CompileError
	if !reflect.DeepEqual(result, CompileResult{}) || !errors.As(err, &compileError) || compileError.Code != ErrorCodeInvalidResourceLimits {
		t.Fatalf("negative limits result=%+v error=%v", result, err)
	}
}

func TestCompileRepeatPermutationAndDefensiveCopies(t *testing.T) {
	t.Parallel()
	input := installedPackProjectInput()
	first, err := New(BuildInfo{}).Compile(context.Background(), input)
	if err != nil || len(first.Diagnostics) != 0 {
		t.Fatalf("first compile err=%v diagnostics=%+v", err, first.Diagnostics)
	}
	permuted := installedPackProjectInput()
	slices.Reverse(permuted.ResolvedDependencies.Installs[0].Files)
	permuted.ProjectSourceTree = reverseInsertedMap(permuted.ProjectSourceTree)
	permuted.InstalledPackTree = reverseInsertedMap(permuted.InstalledPackTree)
	second, err := New(BuildInfo{}).Compile(context.Background(), permuted)
	if err != nil || !reflect.DeepEqual(first.Snapshot(), second.Snapshot()) {
		t.Fatalf("permuted compile differs: err=%v", err)
	}

	input.ProjectSourceTree["document.ldl"][0] = 'X'
	input.InstalledPackTree["pack/schema/pack.ldl"][0] = 'X'
	first.CanonicalJSON[0] = '['
	first.StableAddresses[0] = "mutated"
	first.TypedAST.EntityTypes[0].DisplayName = "mutated"
	snapshot := first.Snapshot()
	if snapshot.CanonicalJSON[0] != '{' || snapshot.StableAddresses[0] == "mutated" || snapshot.TypedAST.EntityTypes[0].DisplayName == "mutated" {
		t.Fatal("CompileResult mutation leaked into Snapshot")
	}
	snapshot.CanonicalJSON[0] = '['
	snapshot.SourceMap.Files[0].Digest = "mutated"
	again := first.Snapshot()
	if again.CanonicalJSON[0] != '{' || again.SourceMap.Files[0].Digest == "mutated" {
		t.Fatal("Snapshot mutation leaked into a later Snapshot")
	}
}

func TestCompileSlicePermutationAndPackAliases(t *testing.T) {
	t.Parallel()
	input := installedPackProjectInput()
	alias := input.ResolvedDependencies.Installs[0]
	alias.InstallName = "schema_alias"
	input.ResolvedDependencies.Installs = append(input.ResolvedDependencies.Installs, alias)
	first, err := New(BuildInfo{}).Compile(context.Background(), input)
	if err != nil || len(first.Diagnostics) != 0 {
		t.Fatalf("Pack alias compile err=%v diagnostics=%+v", err, first.Diagnostics)
	}
	slices.Reverse(input.ResolvedDependencies.Installs)
	second, err := New(BuildInfo{}).Compile(context.Background(), input)
	if err != nil || !reflect.DeepEqual(first.Snapshot(), second.Snapshot()) {
		t.Fatalf("Pack install permutation changed output: err=%v diagnostics=%+v", err, second.Diagnostics)
	}

	assetSource := `project p "Project" {}
entity_type first "First" {
  image "first.svg"
  representation shape rect
}
entity_type second "Second" {
  image "second.svg"
  representation shape rect
}`
	firstSVG := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="1" height="1"/></svg>`)
	secondSVG := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><circle cx="1" cy="1" r="1"/></svg>`)
	assetInput := projectCompileInput(assetSource)
	assetInput.ReferencedAssets = []AssetInput{
		{Origin: SourceOriginProject, Locator: "first.svg", Bytes: firstSVG, Digest: digestBytes(firstSVG), MediaType: "image/svg+xml", ByteLength: int64(len(firstSVG))},
		{Origin: SourceOriginProject, Locator: "second.svg", Bytes: secondSVG, Digest: digestBytes(secondSVG), MediaType: "image/svg+xml", ByteLength: int64(len(secondSVG))},
	}
	assetFirst, err := New(BuildInfo{}).Compile(context.Background(), assetInput)
	if err != nil || len(assetFirst.Diagnostics) != 0 {
		t.Fatalf("asset compile err=%v diagnostics=%+v", err, assetFirst.Diagnostics)
	}
	slices.Reverse(assetInput.ReferencedAssets)
	assetSecond, err := New(BuildInfo{}).Compile(context.Background(), assetInput)
	if err != nil || !reflect.DeepEqual(assetFirst.Snapshot(), assetSecond.Snapshot()) {
		t.Fatalf("asset permutation changed output: err=%v diagnostics=%+v", err, assetSecond.Diagnostics)
	}
}

func TestCompileSnapshotConcurrentRead(t *testing.T) {
	t.Parallel()
	result, err := New(BuildInfo{}).Compile(context.Background(), projectCompileInput(allDeclarationsFixture))
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("Compile() err=%v diagnostics=%+v", err, result.Diagnostics)
	}
	var wait sync.WaitGroup
	for i := 0; i < 32; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			snapshot := result.Snapshot()
			if snapshot.NormalizedDocument == nil || len(snapshot.SemanticIndex.Subjects) == 0 {
				t.Errorf("concurrent Snapshot is incomplete")
			}
		}()
	}
	wait.Wait()
}

func TestCompileLargeGeneratedGraph(t *testing.T) {
	t.Parallel()
	const count = 300
	var source strings.Builder
	source.WriteString("project p \"Project\" {}\nlayers {\n app \"Application\" @1\n}\nentity_type service \"Service\" {\n representation shape rect\n}\n")
	source.WriteString("relation_type link \"Link\" dependency {\n duplicate_policy allow\n from source types [service] layers [app]\n to target types [service] layers [app]\n label \"links\"\n}\nentities service @app {\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&source, "n%03d \"Node %03d\"\n", i, i)
	}
	source.WriteString("}\nrelations link {\n")
	for i := 0; i < count-1; i++ {
		fmt.Fprintf(&source, "r%03d: n%03d -> n%03d\n", i, i, i+1)
	}
	source.WriteString("}\n")
	result, err := New(BuildInfo{}).Compile(context.Background(), projectCompileInput(source.String()))
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("large compile err=%v diagnostics=%+v", err, result.Diagnostics)
	}
	snapshot := result.Snapshot()
	if len(snapshot.NormalizedDocument.Entities) != count || len(snapshot.NormalizedDocument.Relations) != count-1 || len(snapshot.SearchDocuments) < count {
		t.Fatalf("large graph truncated: entities=%d relations=%d search=%d", len(snapshot.NormalizedDocument.Entities), len(snapshot.NormalizedDocument.Relations), len(snapshot.SearchDocuments))
	}
}

func TestResourceLimitDefaultsAndErrorHelpers(t *testing.T) {
	t.Parallel()
	effective, ok := (ResourceLimits{}).Effective()
	if !ok || effective != DefaultResourceLimits() {
		t.Fatalf("zero limits=%+v ok=%v", effective, ok)
	}
	custom := ResourceLimits{MaxAssets: 7}
	effective, ok = custom.Effective()
	if !ok || effective.MaxAssets != 7 || effective.MaxDeclarations != DefaultResourceLimits().MaxDeclarations {
		t.Fatalf("custom limits=%+v ok=%v", effective, ok)
	}
	if (*CompileError)(nil).Error() != "<nil>" || (*CompileError)(nil).Unwrap() != nil {
		t.Fatal("nil CompileError helpers are not total")
	}
	err := &CompileError{Code: ErrorCodeCancelled, Category: ErrorCategoryCancelled, Stage: "parse", cause: context.Canceled}
	if !errors.Is(err, context.Canceled) || err.Error() != ErrorCodeCancelled+" at parse" || IsCompileError(errors.New("other"), ErrorCategoryCancelled) {
		t.Fatalf("CompileError helper mismatch: %v", err)
	}
	if err := validateRasterResources(AssetInput{MediaType: "image/png", Bytes: []byte("not a PNG")}, DefaultResourceLimits()); err != nil {
		t.Fatalf("malformed raster must remain a semantic validation error: %v", err)
	}
}

func assertRejectedOutput(t *testing.T, result CompileResult) {
	t.Helper()
	copy := result.CompileOutput
	copy.Diagnostics = nil
	if !reflect.DeepEqual(copy, CompileOutput{}) {
		t.Fatalf("semantic rejection published a partial result: %+v", copy)
	}
	snapshot := result.Snapshot()
	snapshot.Diagnostics = nil
	if !reflect.DeepEqual(snapshot.CompileOutput, CompileOutput{}) {
		t.Fatalf("semantic rejection Snapshot published a partial result: %+v", snapshot.CompileOutput)
	}
}

func assertCancelled(t *testing.T, result CompileResult, err error, stage compileStage) {
	t.Helper()
	if !reflect.DeepEqual(result, CompileResult{}) || !errors.Is(err, context.Canceled) || !IsCompileError(err, ErrorCategoryCancelled) {
		t.Fatalf("cancelled result=%+v err=%v", result, err)
	}
	var compileError *CompileError
	if !errors.As(err, &compileError) || compileError.Code != ErrorCodeCancelled || compileError.Stage != string(stage) {
		t.Fatalf("cancel error=%+v want stage=%s", compileError, stage)
	}
}

type compileHookContext struct {
	context.Context
	hook func(compileStage)
}

func (c *compileHookContext) onCompileBoundary(stage compileStage) {
	c.hook(stage)
}

// deferredCancelContext allows the target boundary check itself to pass, then
// cancels at the first unit of facade-owned work inside that stage.
type deferredCancelContext struct {
	context.Context
	target compileStage
	armed  bool
	passed bool
}

func (c *deferredCancelContext) onCompileBoundary(stage compileStage) {
	if stage == c.target {
		c.armed = true
	}
}

func (c *deferredCancelContext) Err() error {
	if !c.armed {
		return c.Context.Err()
	}
	if !c.passed {
		c.passed = true
		return nil
	}
	return context.Canceled
}

func projectCompileInput(source string) CompileInput {
	return projectTreeCompileInput(map[string][]byte{"document.ldl": []byte(source)})
}

func projectTreeCompileInput(files map[string][]byte) CompileInput {
	return CompileInput{Mode: CompileProject, EntryPath: "document.ldl", ProjectSourceTree: files, ResolvedDependencies: emptyResolvedDependencies()}
}

func emptyResolvedDependencies() ResolvedDependencies {
	return ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1}
}

func pngBytes(width, height int) []byte {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, width, height))); err != nil {
		panic(err)
	}
	return encoded.Bytes()
}

func installedPackProjectInput() CompileInput {
	packSource := []byte("entity_type service \"Service\" {\n  representation shape rect\n}\nexport { service }\n")
	manifest := packManifestBytes("pub/schema", "schema", "1.0.0", "pack.ldl", nil)
	return CompileInput{
		Mode: CompileProject, EntryPath: "document.ldl",
		ProjectSourceTree: map[string][]byte{"document.ldl": []byte("import { service } from \"schema\"\nproject p \"Project\" {}\n")},
		InstalledPackTree: map[string][]byte{"pack/schema/pack.ldl": packSource},
		ResolvedDependencies: ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: []ResolvedPack{{
			InstallName: "schema", CanonicalID: "pub/schema", Version: "1.0.0", Digest: "sha256:" + strings.Repeat("a", 64),
			Path: "pack/schema", Entry: "pack.ldl", Files: []ResolvedPackFile{{Path: "pack.ldl", Digest: digestBytes(packSource)}},
			ManifestPath: "manifest.json", Manifest: manifest,
		}}},
	}
}

func rootPackInput() CompileInput {
	input := installedPackProjectInput()
	input.Mode = CompilePack
	input.EntryPath = "pack.ldl"
	input.RootPackID = "pub/schema"
	input.ProjectSourceTree = map[string][]byte{}
	return input
}

func packManifestBytes(id, name, version, entry string, dependencies map[string]map[string]string) []byte {
	if dependencies == nil {
		dependencies = map[string]map[string]string{}
	}
	value := map[string]any{"format": "layerdraw-pack", "format_version": 1, "id": id, "name": name, "version": version, "language": 1, "entry": entry, "dependencies": dependencies}
	encoded, err := jsonMarshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

func jsonMarshal(value any) ([]byte, error) {
	// Kept behind a helper so fixtures do not depend on object key order.
	return materializeCanonicalJSON(value)
}

func materializeCanonicalJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}

func reverseInsertedMap(input map[string][]byte) map[string][]byte {
	keys := sortedByteMapKeys(input)
	slices.Reverse(keys)
	out := make(map[string][]byte, len(input))
	for _, key := range keys {
		out[key] = bytes.Clone(input[key])
	}
	return out
}

var testPNG = []byte{
	0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n',
	0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89,
	0x00, 0x00, 0x00, 0x0d, 'I', 'D', 'A', 'T',
	0x08, 0xd7, 0x63, 0x60, 0x60, 0x60, 0xf8, 0x0f, 0x00, 0x01, 0x04, 0x01, 0x00,
	0x5f, 0xdc, 0xcc, 0x59,
	0x00, 0x00, 0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
}

const assetFixture = `project p "Project" {}
entity_type service "Service" {
  image "icon.png"
  representation shape rect
}
`

const allDeclarationsFixture = `project p "Project" {
  description "Root description"
}
layers {
  app "Application" @10
}
entity_type service "Service" {
  representation shape rect
  columns {
    environment "Environment" enum [prod, dev] required default prod
    note "Note" string
  }
  unique env_unique [environment]
}
relation_type link "Link" dependency {
  duplicate_policy allow
  from source types [service] layers [app]
  to target types [service] layers [app]
  label "links"
  columns {
    weight "Weight" number
  }
}
entities service @app {
  alpha "Alpha"
  beta "Beta"
}
rows service [environment, note] {
  alpha primary: prod, "api"
}
relations link {
  alpha_beta: alpha -> beta "Alpha to Beta"
}
relation_rows link [weight] {
  alpha_beta primary: 1.5
}
query scope "Scope" {
  parameters {
    environment enum [prod, dev] default prod
  }
  select {
    entity_types [service]
    relation_types [link]
    roots [alpha]
  }
  result [seed_entities, induced_relations]
}
view inventory "Inventory" inventory {
  source query scope {}
  table {
    rows entity_rows
    entity_types [service]
    entity_id
    column environment {
      source attribute environment entity_types [service]
    }
  }
  export data json "inventory.json" {
    fidelity lossless
    source_refs
    diagnostics
  }
}
reference guide <<-TEXT
Use the graph as the source of truth.
TEXT
`
