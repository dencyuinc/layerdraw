// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package sourceplanner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/internal/engine"
)

func TestSourcePatchGuardsUnicodeLineEndingsAndSourceMaintain(t *testing.T) {
	compiler := newTestCompiler()
	planner := NewSourcePlanner(compiler)
	input := testInput(map[string][]byte{"document.ldl": []byte("project p \"Project\" {}\r\n// café keep\r\n")})
	base, before := testBase(t, compiler, input)
	start := bytes.Index(input.ProjectSourceTree["document.ldl"], []byte("keep"))
	replacement := []byte("kept")
	ref, blobs := testBlob("replacement", replacement)
	request := PreviewSourcePatchInput{Limits: testLimits(), Preconditions: fullPreconditions(before, base.Generation), Patch: SourcePatchBatch{Patches: []SourcePatchInput{{
		SourceRange:          SourceRange{Origin: SourceOrigin{Kind: OriginKindProject}, ModulePath: "document.ldl", StartByte: canonicalUint(start), EndByte: canonicalUint(start + len("keep"))},
		ExpectedSourceDigest: digest(input.ProjectSourceTree["document.ldl"]), ReplacementBlob: ref,
	}}}}
	plan, err := planner.PreviewSourcePatch(context.Background(), base, request, blobs)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Preview.Status != "valid" || len(plan.Preview.SemanticDiff.Entries) != 0 {
		t.Fatalf("unexpected preview: %+v", plan.Preview)
	}
	if got := *plan.Preview.RequiredAuthoringCapabilities; !reflect.DeepEqual(got, []AuthoringCapability{AuthoringCapabilitySourceMaintain}) {
		t.Fatalf("capabilities = %v", got)
	}
	if !bytes.Contains(plan.Candidate.ProjectSourceTree["document.ldl"], []byte("café kept\r\n")) {
		t.Fatalf("Unicode/CRLF changed: %q", plan.Candidate.ProjectSourceTree["document.ldl"])
	}

	stale := request
	stale.Patch.Patches = append([]SourcePatchInput(nil), request.Patch.Patches...)
	stale.Patch.Patches[0].ExpectedSourceDigest = Digest("sha256:" + strings.Repeat("0", 64))
	rejected, err := planner.PreviewSourcePatch(context.Background(), base, stale, blobs)
	if err != nil || rejected.Preview.Status != "invalid" {
		t.Fatalf("stale patch = %+v, %v", rejected.Preview, err)
	}

	split := request
	split.Patch.Patches = append([]SourcePatchInput(nil), request.Patch.Patches...)
	split.Patch.Patches[0].ExpectedSourceDigest = digest(input.ProjectSourceTree["document.ldl"])
	unicodeStart := bytes.Index(input.ProjectSourceTree["document.ldl"], []byte("é"))
	split.Patch.Patches[0].SourceRange.StartByte = canonicalUint(unicodeStart + 1)
	split.Patch.Patches[0].SourceRange.EndByte = canonicalUint(unicodeStart + 1)
	rejected, err = planner.PreviewSourcePatch(context.Background(), base, split, blobs)
	if err != nil || rejected.Preview.Status != "invalid" || rejected.Preview.Diagnostics[0].Code != "LDL1001" {
		t.Fatalf("split patch = %+v, %v", rejected.Preview, err)
	}
}

func TestSourcePatchRejectsOverlapAndAmbiguousEncoding(t *testing.T) {
	compiler := newTestCompiler()
	planner := NewSourcePlanner(compiler)
	input := testInput(map[string][]byte{"document.ldl": []byte("project p \"Project\" {}\n")})
	base, before := testBase(t, compiler, input)
	ref, blobs := testBlob("replacement", []byte("x"))
	preconditions := fullPreconditions(before, base.Generation)
	patch := func(start, end int) SourcePatchInput {
		return SourcePatchInput{SourceRange: SourceRange{Origin: SourceOrigin{Kind: OriginKindProject}, ModulePath: "document.ldl", StartByte: canonicalUint(start), EndByte: canonicalUint(end)}, ExpectedSourceDigest: digest(input.ProjectSourceTree["document.ldl"]), ReplacementBlob: ref}
	}
	request := PreviewSourcePatchInput{Limits: testLimits(), Preconditions: preconditions, Patch: SourcePatchBatch{Patches: []SourcePatchInput{patch(0, 4), patch(3, 5)}}}
	plan, err := planner.PreviewSourcePatch(context.Background(), base, request, blobs)
	if err != nil || plan.Preview.Status != "invalid" {
		t.Fatalf("overlap = %+v, %v", plan.Preview, err)
	}

	badRef, badBlobs := testBlob("utf16", []byte{0xff, 0xfe, 'x', 0})
	request.Patch.Patches = []SourcePatchInput{patch(0, 0)}
	request.Patch.Patches[0].ReplacementBlob = badRef
	plan, err = planner.PreviewSourcePatch(context.Background(), base, request, badBlobs)
	if err != nil || plan.Preview.Status != "invalid" || plan.Preview.Diagnostics[0].Code != "LDL1001" {
		t.Fatalf("encoding = %+v, %v", plan.Preview, err)
	}
}

func TestFragmentAndEquivalentPatchHaveIdenticalDiffAndImpact(t *testing.T) {
	compiler := newTestCompiler()
	planner := NewSourcePlanner(compiler)
	input := testInput(map[string][]byte{"document.ldl": []byte("project p \"Project\" {}\n")})
	base, before := testBase(t, compiler, input)
	fragmentBytes := []byte("entity_type service \"Service\" {\n representation shape rect\n}\n")
	fragmentRef, fragmentBlobs := testBlob("fragment", fragmentBytes)
	fragmentRequest := PreviewFragmentInput{Limits: testLimits(), Preconditions: fullPreconditions(before, base.Generation), Fragment: FragmentInput{Intent: "insert", InsertionOwner: "ldl:project:p", AllowedKinds: []SubjectKind{SubjectKindEntityType}, FragmentBlob: fragmentRef}}
	fragmentPlan, err := planner.PreviewFragment(context.Background(), base, fragmentRequest, fragmentBlobs)
	if err != nil || fragmentPlan.Preview.Status != "valid" {
		t.Fatalf("fragment = %+v, %v", fragmentPlan.Preview, err)
	}

	inserted := fragmentPlan.Candidate.ProjectSourceTree["document.ldl"][len(input.ProjectSourceTree["document.ldl"]):]
	patchRef, patchBlobs := testBlob("patch", inserted)
	patchRequest := PreviewSourcePatchInput{Limits: testLimits(), Preconditions: fullPreconditions(before, base.Generation), Patch: SourcePatchBatch{Patches: []SourcePatchInput{{SourceRange: SourceRange{Origin: SourceOrigin{Kind: OriginKindProject}, ModulePath: "document.ldl", StartByte: canonicalUint(len(input.ProjectSourceTree["document.ldl"])), EndByte: canonicalUint(len(input.ProjectSourceTree["document.ldl"]))}, ExpectedSourceDigest: digest(input.ProjectSourceTree["document.ldl"]), ReplacementBlob: patchRef}}}}
	patchPlan, err := planner.PreviewSourcePatch(context.Background(), base, patchRequest, patchBlobs)
	if err != nil || patchPlan.Preview.Status != "valid" {
		t.Fatalf("patch = %+v, %v", patchPlan.Preview, err)
	}
	if !reflect.DeepEqual(fragmentPlan.Preview.SemanticDiff, patchPlan.Preview.SemanticDiff) || !reflect.DeepEqual(fragmentPlan.Preview.AuthoringImpact, patchPlan.Preview.AuthoringImpact) {
		t.Fatalf("fragment/patch parity failed\nfragment=%+v\npatch=%+v", fragmentPlan.Preview, patchPlan.Preview)
	}
}

func TestFragmentMultipleDeclarationsRemainCanonical(t *testing.T) {
	compiler := newTestCompiler()
	planner := NewSourcePlanner(compiler)
	input := testInput(map[string][]byte{"document.ldl": []byte("project p \"Project\" {}\n")})
	base, before := testBase(t, compiler, input)
	value := []byte("entity_type zeta \"Zeta\" {\n representation shape rect\n}\nentity_type alpha \"Alpha\" {\n representation shape rect\n}\n")
	ref, blobs := testBlob("fragment-many", value)
	request := PreviewFragmentInput{Limits: testLimits(), Preconditions: fullPreconditions(before, base.Generation), Fragment: FragmentInput{Intent: "insert", InsertionOwner: "ldl:project:p", AllowedKinds: []SubjectKind{SubjectKindEntityType}, FragmentBlob: ref}}
	plan, err := planner.PreviewFragment(context.Background(), base, request, blobs)
	if err != nil || plan.Preview.Status != "valid" || len(plan.Preview.SemanticDiff.Entries) != 2 {
		t.Fatalf("multi-fragment = %+v, %v", plan.Preview, err)
	}
	if _, err := json.Marshal(plan.Preview); err != nil {
		t.Fatalf("preview is not canonical: %v", err)
	}
}

func TestFormatScopeIsScopedCommentPreservingAndIdempotent(t *testing.T) {
	compiler := newTestCompiler()
	planner := NewSourcePlanner(compiler)
	source := []byte("project p \"Project\" {}\n/// service docs\nentity_type   service   \"Service\"   {\n representation   shape   rect\n}\n// untouched\nreference note <<-TEXT\nhello\nTEXT\n")
	input := testInput(map[string][]byte{"document.ldl": source})
	base, before := testBase(t, compiler, input)
	request := FormatScopeInput{Limits: testLimits(), Preconditions: fullPreconditions(before, base.Generation), ScopeAddresses: []StableAddress{"ldl:project:p:entity-type:service"}}
	first, err := planner.FormatScope(context.Background(), base, request)
	if err != nil || first.Preview.Status != "valid" {
		t.Fatalf("first = %+v, %v", first.Preview, err)
	}
	if !bytes.Contains(first.Candidate.ProjectSourceTree["document.ldl"], []byte("/// service docs")) || !bytes.Contains(first.Candidate.ProjectSourceTree["document.ldl"], []byte("// untouched\nreference note")) {
		t.Fatalf("comments or unrelated scope changed: %q", first.Candidate.ProjectSourceTree["document.ldl"])
	}
	nextBase, nextSnapshot := testBase(t, compiler, first.Candidate)
	nextBase.Generation = *first.Preview.ProposedGeneration
	secondRequest := FormatScopeInput{Limits: testLimits(), Preconditions: fullPreconditions(nextSnapshot, nextBase.Generation), ScopeAddresses: request.ScopeAddresses}
	second, err := planner.FormatScope(context.Background(), nextBase, secondRequest)
	if err != nil || second.Preview.Status != "valid" || len(second.Preview.SourceDiff.Edits) != 0 {
		t.Fatalf("format not idempotent: %+v, %v", second.Preview, err)
	}
}

func TestOrganizeWorkspaceIsDeterministicStableAndPure(t *testing.T) {
	compiler := newTestCompiler()
	planner := NewSourcePlanner(compiler)
	input := testInput(map[string][]byte{
		"document.ldl": []byte("import { service } from \"./types.ldl\"\nproject p \"Project\" {}\n"),
		"types.ldl":    []byte("/// service\nentity_type service \"Service\" {\n representation shape rect\n}\nexport { service }\n"),
	})
	base, before := testBase(t, compiler, input)
	request := OrganizeWorkspaceInput{Limits: testLimits(), Preconditions: fullPreconditions(before, base.Generation), Strategy: "standard_layout"}
	first, err := planner.OrganizeWorkspace(context.Background(), base, request)
	if err != nil || first.Preview.Status != "valid" {
		t.Fatalf("first = %+v, %v", first.Preview, err)
	}
	second, err := planner.OrganizeWorkspace(context.Background(), base, request)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("organization is nondeterministic: %v", err)
	}
	if _, exists := input.ProjectSourceTree["schema/entity_types/service.ldl"]; exists {
		t.Fatal("base input mutated")
	}
	if _, exists := first.Candidate.ProjectSourceTree["schema/entity_types/service.ldl"]; !exists {
		t.Fatalf("standard module absent: %v", sortedPaths(first.Candidate.ProjectSourceTree))
	}
	afterResult, err := compiler.Compile(context.Background(), first.Candidate)
	if err != nil || len(afterResult.Diagnostics) != 0 {
		t.Fatalf("candidate does not compile: %v %+v", err, afterResult.Diagnostics)
	}
	after := afterResult.Snapshot()
	if !reflect.DeepEqual(before.StableAddresses, after.StableAddresses) || before.DefinitionHash != after.DefinitionHash {
		t.Fatalf("organization changed semantics")
	}
}

func TestOrganizeWorkspaceSplitsLegalSingleModule(t *testing.T) {
	compiler := newTestCompiler()
	planner := NewSourcePlanner(compiler)
	input := testInput(map[string][]byte{"document.ldl": []byte("project p \"Project\" {}\n/// service\nentity_type service \"Service\" {\n representation shape rect\n}\n")})
	base, before := testBase(t, compiler, input)
	request := OrganizeWorkspaceInput{Limits: testLimits(), Preconditions: fullPreconditions(before, base.Generation), Strategy: "standard_layout"}
	plan, err := planner.OrganizeWorkspace(context.Background(), base, request)
	if err != nil || plan.Preview.Status != "valid" {
		t.Fatalf("organization = %+v, %v", plan.Preview, err)
	}
	if _, exists := plan.Candidate.ProjectSourceTree["schema/entity_types/service.ldl"]; !exists {
		t.Fatalf("single module was not split: %v", sortedPaths(plan.Candidate.ProjectSourceTree))
	}
	if !bytes.Contains(plan.Candidate.ProjectSourceTree["document.ldl"], []byte("import { service } from \"./schema/entity_types/service.ldl\"")) {
		t.Fatalf("entry closure was not regenerated: %q", plan.Candidate.ProjectSourceTree["document.ldl"])
	}
	afterResult, err := compiler.Compile(context.Background(), plan.Candidate)
	if err != nil || len(afterResult.Diagnostics) != 0 || before.DefinitionHash != afterResult.DefinitionHash {
		t.Fatalf("split changed semantics: %v %+v", err, afterResult.Diagnostics)
	}
}

func TestOrganizeWorkspaceRegeneratesCrossShardSymbolClosure(t *testing.T) {
	compiler := newTestCompiler()
	planner := NewSourcePlanner(compiler)
	source := []byte("project p \"Project\" {}\nlayers {\n app \"Application\" @1\n}\nentity_type service \"Service\" {\n representation shape rect\n}\nentities service @ app {\n api \"API\"\n}\n")
	input := testInput(map[string][]byte{"document.ldl": source})
	base, before := testBase(t, compiler, input)
	request := OrganizeWorkspaceInput{Limits: testLimits(), Preconditions: fullPreconditions(before, base.Generation), Strategy: "standard_layout"}
	plan, err := planner.OrganizeWorkspace(context.Background(), base, request)
	if err != nil || plan.Preview.Status != "valid" {
		t.Fatalf("organization = %+v, %v", plan.Preview, err)
	}
	want := []string{"document.ldl", "layers/app/api.ldl", "layers/layers.ldl", "schema/entity_types/service.ldl"}
	if got := sortedPaths(plan.Candidate.ProjectSourceTree); !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	afterResult, err := compiler.Compile(context.Background(), plan.Candidate)
	if err != nil || len(afterResult.Diagnostics) != 0 || before.DefinitionHash != afterResult.DefinitionHash {
		t.Fatalf("organized closure changed semantics: %v %+v\n%q", err, afterResult.Diagnostics, plan.Candidate.ProjectSourceTree)
	}
}

func TestPlannerCancellationDoesNotPublishCandidate(t *testing.T) {
	compiler := newTestCompiler()
	planner := NewSourcePlanner(compiler)
	input := testInput(map[string][]byte{"document.ldl": []byte("project p \"Project\" {}\n")})
	base, before := testBase(t, compiler, input)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := planner.OrganizeWorkspace(ctx, base, OrganizeWorkspaceInput{Limits: testLimits(), Preconditions: fullPreconditions(before, base.Generation), Strategy: "standard_layout"})
	if err == nil {
		t.Fatal("cancelled plan unexpectedly succeeded")
	}
}

func TestSourcePlannerEnforcesCompleteOutputLimits(t *testing.T) {
	compiler := newTestCompiler()
	planner := NewSourcePlanner(compiler)
	input := testInput(map[string][]byte{"document.ldl": []byte("project p \"Project\" {}\n// keep\n")})
	base, before := testBase(t, compiler, input)
	ref, blobs := testBlob("limit", []byte("kept"))
	start := bytes.Index(input.ProjectSourceTree["document.ldl"], []byte("keep"))
	request := PreviewSourcePatchInput{Limits: WorkbenchLimits{MaxItems: 1, MaxOutputBytes: 10000000}, Preconditions: fullPreconditions(before, base.Generation), Patch: SourcePatchBatch{Patches: []SourcePatchInput{{SourceRange: SourceRange{Origin: SourceOrigin{Kind: OriginKindProject}, ModulePath: "document.ldl", StartByte: canonicalUint(start), EndByte: canonicalUint(start + 4)}, ExpectedSourceDigest: digest(input.ProjectSourceTree["document.ldl"]), ReplacementBlob: ref}}}}
	_, err := planner.PreviewSourcePatch(context.Background(), base, request, blobs)
	var limitError *SourcePlannerLimitError
	if !errors.As(err, &limitError) || limitError.Resource != "max_items" {
		t.Fatalf("limit error = %T %v", err, err)
	}
}

func FuzzSourcePatchRanges(f *testing.F) {
	f.Add(uint16(0), uint16(0))
	f.Add(uint16(3), uint16(8))
	f.Add(uint16(25), uint16(26))
	f.Add(uint16(106), uint16(24))
	f.Fuzz(func(t *testing.T, rawStart, rawEnd uint16) {
		compiler := newTestCompiler()
		planner := NewSourcePlanner(compiler)
		input := testInput(map[string][]byte{"document.ldl": []byte("project p \"Project\" {}\n// λ\r\n")})
		base, before := testBase(t, compiler, input)
		start, end := int(rawStart), int(rawEnd)
		ref, blobs := testBlob("fuzz", []byte("x"))
		request := PreviewSourcePatchInput{Limits: testLimits(), Preconditions: fullPreconditions(before, base.Generation), Patch: SourcePatchBatch{Patches: []SourcePatchInput{{SourceRange: SourceRange{Origin: SourceOrigin{Kind: OriginKindProject}, ModulePath: "document.ldl", StartByte: canonicalUint(start), EndByte: canonicalUint(end)}, ExpectedSourceDigest: digest(input.ProjectSourceTree["document.ldl"]), ReplacementBlob: ref}}}}
		plan, err := planner.PreviewSourcePatch(context.Background(), base, request, blobs)
		if err != nil {
			t.Fatal(err)
		}
		if plan.Preview.Status == "valid" {
			result, err := compiler.Compile(context.Background(), plan.Candidate)
			if err != nil || len(result.Diagnostics) != 0 {
				t.Fatalf("valid patch did not compile: %v %+v", err, result.Diagnostics)
			}
		}
	})
}

func testInput(tree map[string][]byte) CompileInput {
	return CompileInput{Mode: CompileProject, EntryPath: "document.ldl", ProjectSourceTree: tree, InstalledPackTree: map[string][]byte{}, ResolvedDependencies: ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1, Installs: []ResolvedPack{}}, ReferencedAssets: []AssetInput{}}
}
func testBase(t *testing.T, compiler Compiler, input CompileInput) (SourcePlanningBase, Snapshot) {
	t.Helper()
	result, err := compiler.Compile(context.Background(), input)
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("base compile: %v %+v", err, result.Diagnostics)
	}
	generation := Generation{Namespace: "test", DocumentID: "document", Value: 1}
	return SourcePlanningBase{Input: input, Generation: generation}, result.Snapshot()
}
func fullPreconditions(snapshot Snapshot, generation Generation) EngineEditPreconditions {
	sources := []ExpectedSourceDigest{}
	for _, item := range snapshot.SourceMap.Files {
		if item.Origin.Kind == "project" {
			sources = append(sources, ExpectedSourceDigest{Module: projectModule(item.ModulePath), Digest: Digest(item.Digest)})
		}
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Module.ModulePath < sources[j].Module.ModulePath })
	subjects := []ExpectedHash{}
	for _, item := range snapshot.SubjectSemanticHashes {
		subjects = append(subjects, ExpectedHash{Address: StableAddress(item.Address), Hash: Digest(item.Hash)})
	}
	subtrees := []ExpectedHash{}
	for _, item := range snapshot.SubtreeHashes {
		subtrees = append(subtrees, ExpectedHash{Address: StableAddress(item.OwnerAddress), Hash: Digest(item.Hash)})
	}
	children := []ExpectedChildSet{}
	for _, item := range snapshot.ChildSetHashes {
		children = append(children, ExpectedChildSet{OwnerAddress: StableAddress(item.OwnerAddress), ChildKind: SubjectKind(item.ChildKind), Hash: Digest(item.Hash)})
	}
	return EngineEditPreconditions{Generation: generation, ExpectedSourceDigests: &sources, ExpectedSubjectHashes: subjects, ExpectedSubtreeHashes: subtrees, ExpectedChildSets: children}
}
func testBlob(id string, value []byte) (BlobRef, PlannerBlobs) {
	ref := BlobRef{BlobID: id, Digest: digest(value), Lifetime: BlobLifetimeRequest, MediaType: textMediaType, Size: uint64(len(value))}
	return ref, PlannerBlobs{id: value}
}
func testLimits() WorkbenchLimits {
	return WorkbenchLimits{MaxItems: 10000, MaxOutputBytes: 10000000}
}

type testCompiler struct{ compiler engine.Engine }

func newTestCompiler() testCompiler {
	return testCompiler{compiler: engine.New(engine.BuildInfo{})}
}

func (c testCompiler) Compile(ctx context.Context, input CompileInput) (CompileResult, error) {
	installs := make([]engine.ResolvedPack, len(input.ResolvedDependencies.Installs))
	for index, install := range input.ResolvedDependencies.Installs {
		files := make([]engine.ResolvedPackFile, len(install.Files))
		for fileIndex, file := range install.Files {
			files[fileIndex] = engine.ResolvedPackFile{Path: file.Path, Digest: file.Digest}
		}
		dependencies := make([]engine.ResolvedPackDependency, len(install.Dependencies))
		for dependencyIndex, dependency := range install.Dependencies {
			dependencies[dependencyIndex] = engine.ResolvedPackDependency{LocalName: dependency.LocalName, InstallName: dependency.InstallName}
		}
		installs[index] = engine.ResolvedPack{
			InstallName: install.InstallName, CanonicalID: install.CanonicalID, Version: install.Version,
			Digest: install.Digest, Path: install.Path, Entry: install.Entry, Files: files,
			Dependencies: dependencies, ManifestPath: install.ManifestPath, Manifest: bytes.Clone(install.Manifest),
		}
	}
	assets := make([]engine.AssetInput, len(input.ReferencedAssets))
	for index, asset := range input.ReferencedAssets {
		assets[index] = engine.AssetInput{
			Origin: engine.SourceOriginKind(asset.Origin), PackID: asset.PackID, Locator: asset.Locator,
			Bytes: bytes.Clone(asset.Bytes), Digest: asset.Digest, MediaType: asset.MediaType, ByteLength: asset.ByteLength,
		}
	}
	result, err := c.compiler.Compile(ctx, engine.CompileInput{
		Mode: engine.CompileMode(input.Mode), EntryPath: input.EntryPath, RootPackID: input.RootPackID,
		ProjectSourceTree: cloneTree(input.ProjectSourceTree), InstalledPackTree: cloneTree(input.InstalledPackTree),
		ResolvedDependencies: engine.ResolvedDependencies{
			Format: input.ResolvedDependencies.Format, FormatVersion: input.ResolvedDependencies.FormatVersion,
			Language: input.ResolvedDependencies.Language, Installs: installs,
		},
		ReferencedAssets: assets,
		ResourceLimits: engine.ResourceLimits{
			MaxProjectSourceFiles: input.ResourceLimits.MaxProjectSourceFiles,
			MaxProjectSourceBytes: input.ResourceLimits.MaxProjectSourceBytes,
			MaxPackFiles:          input.ResourceLimits.MaxPackFiles, MaxPackBytes: input.ResourceLimits.MaxPackBytes,
			MaxAssets: input.ResourceLimits.MaxAssets, MaxAssetBytes: input.ResourceLimits.MaxAssetBytes,
			MaxRasterDimension: input.ResourceLimits.MaxRasterDimension, MaxRasterPixels: input.ResourceLimits.MaxRasterPixels,
			MaxDeclarations: input.ResourceLimits.MaxDeclarations,
		},
	})
	if err != nil {
		return CompileResult{}, err
	}
	snapshot := result.Snapshot()
	diagnostics := make([]CompileDiagnostic, len(snapshot.Diagnostics))
	for index, value := range snapshot.Diagnostics {
		diagnostics[index] = CompileDiagnostic{
			Code: value.Code, Severity: value.Severity, MessageKey: value.MessageKey, Message: value.Message,
			Range: value.Range, SubjectAddress: value.SubjectAddress, OwnerAddress: value.OwnerAddress, Related: value.Related,
		}
	}
	classifications := make([]AuthoringSubjectClassification, len(snapshot.AuthoringSubjectClassification))
	for index, value := range snapshot.AuthoringSubjectClassification {
		classifications[index] = AuthoringSubjectClassification{Address: value.Address, Kind: value.Kind, Capability: AuthoringCapability(value.Capability)}
	}
	output := Snapshot{
		Mode: CompileMode(snapshot.Mode), TypedAST: TypedAST{Graph: snapshot.TypedAST.Graph},
		NormalizedDocument: snapshot.NormalizedDocument, NormalizedPackArtifact: snapshot.NormalizedPackArtifact,
		CanonicalJSON: bytes.Clone(snapshot.CanonicalJSON), SourceMap: snapshot.SourceMap,
		StableAddresses: append([]string(nil), snapshot.StableAddresses...), DefinitionHash: snapshot.DefinitionHash,
		GraphHash: snapshot.GraphHash, SubjectSemanticHashes: snapshot.SubjectSemanticHashes,
		SubtreeHashes: snapshot.SubtreeHashes, ChildSetHashes: snapshot.ChildSetHashes,
		AuthoringSubjectClassification: classifications, Diagnostics: diagnostics,
	}
	return CompileResult{Output: output, Diagnostics: diagnostics, DefinitionHash: output.DefinitionHash}, nil
}
