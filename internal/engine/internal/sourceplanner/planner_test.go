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

	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/index"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/materialize"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/query"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/resolve"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/definition"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/semantic/graph"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/syntax"
	"github.com/dencyuinc/layerdraw/internal/engine/internal/compiler/view"
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

type testCompiler struct{}

func newTestCompiler() testCompiler {
	return testCompiler{}
}

func (c testCompiler) Compile(ctx context.Context, input CompileInput) (CompileResult, error) {
	if err := ctx.Err(); err != nil {
		return CompileResult{}, err
	}
	resolvedInput := testResolveInput(input)
	resolved := resolve.Resolve(resolvedInput)
	if resolved.HasErrors {
		diagnostics := testDiagnostics(resolved.Diagnostics)
		return CompileResult{Output: Snapshot{Mode: CompileMode(resolved.Mode), Diagnostics: diagnostics}, Diagnostics: diagnostics}, nil
	}
	defined := definition.Compile(definition.Input{Resolve: resolved})
	if defined.HasErrors {
		diagnostics := testDiagnostics(defined.Diagnostics)
		return CompileResult{Output: Snapshot{Mode: CompileMode(resolved.Mode), Diagnostics: diagnostics}, Diagnostics: diagnostics}, nil
	}
	graphed := graph.Compile(graph.Input{Resolve: resolved, Definition: defined})
	if graphed.HasErrors {
		diagnostics := testDiagnostics(graphed.Diagnostics)
		return CompileResult{Output: Snapshot{Mode: CompileMode(resolved.Mode), Diagnostics: diagnostics}, Diagnostics: diagnostics}, nil
	}
	queried := query.Compile(query.Input{Resolve: resolved, Definition: defined, Graph: graphed})
	if queried.HasErrors {
		diagnostics := testDiagnostics(queried.Diagnostics)
		return CompileResult{Output: Snapshot{Mode: CompileMode(resolved.Mode), Diagnostics: diagnostics}, Diagnostics: diagnostics}, nil
	}
	viewed := view.Compile(view.Input{Resolve: resolved, Definition: defined, Graph: graphed, Query: queried})
	if viewed.HasErrors || viewed.ExportRecipes.HasErrors {
		diagnostics := testDiagnostics(viewed.Diagnostics)
		return CompileResult{Output: Snapshot{Mode: CompileMode(resolved.Mode), Diagnostics: diagnostics}, Diagnostics: diagnostics}, nil
	}
	materialized := materialize.Compile(materialize.Input{
		Resolve: resolved, Definition: defined, Graph: graphed, Query: queried, View: viewed,
		Resolved: testResolvedMetadata(input, resolved),
	})
	if materialized.HasErrors {
		diagnostics := testDiagnostics(materialized.Diagnostics)
		return CompileResult{Output: Snapshot{Mode: CompileMode(resolved.Mode), Diagnostics: diagnostics}, Diagnostics: diagnostics}, nil
	}
	indexed := index.Build(index.Input{Materialized: materialized})
	if indexed.HasErrors {
		diagnostics := testDiagnostics(indexed.Diagnostics)
		return CompileResult{Output: Snapshot{Mode: CompileMode(resolved.Mode), Diagnostics: diagnostics}, Diagnostics: diagnostics}, nil
	}
	materializedSnapshot := materialized.Snapshot()
	indexSnapshot := indexed.Snapshot()
	diagnostics := testDiagnostics(indexed.Diagnostics)
	output := Snapshot{
		Mode: CompileMode(resolved.Mode), TypedAST: TypedAST{Graph: graphed.Graph},
		NormalizedDocument: materializedSnapshot.Document, NormalizedPackArtifact: materializedSnapshot.Pack,
		CanonicalJSON: bytes.Clone(materializedSnapshot.CanonicalJSON), SourceMap: indexSnapshot.SourceMap,
		StableAddresses: testStableAddresses(indexSnapshot.SemanticIndex.Subjects), DefinitionHash: materializedSnapshot.Hashes.Definition,
		GraphHash: materializedSnapshot.Hashes.Graph, SubjectSemanticHashes: materializedSnapshot.Hashes.OwnSubjects,
		SubtreeHashes: materializedSnapshot.Hashes.Subtrees, ChildSetHashes: materializedSnapshot.Hashes.ChildSets,
		AuthoringSubjectClassification: testAuthoringClassifications(indexSnapshot.SemanticIndex.Subjects), Diagnostics: diagnostics,
	}
	return CompileResult{Output: output, Diagnostics: diagnostics, DefinitionHash: output.DefinitionHash}, nil
}

func testResolveInput(input CompileInput) resolve.Input {
	projectFiles := make(map[string]resolve.SourceFile, len(input.ProjectSourceTree))
	for _, path := range sortedPaths(input.ProjectSourceTree) {
		projectFiles[path] = resolve.SourceFromParse(syntax.Parse(input.ProjectSourceTree[path]))
	}
	installs := make(map[string]resolve.ResolvedPack, len(input.ResolvedDependencies.Installs))
	for _, install := range input.ResolvedDependencies.Installs {
		files := make(map[string]string, len(install.Files))
		sourceFiles := map[string]resolve.SourceFile{}
		for _, file := range install.Files {
			files[file.Path] = file.Digest
			if source, ok := input.InstalledPackTree[file.Path]; ok {
				sourceFiles[file.Path] = resolve.SourceFromParse(syntax.Parse(source))
			}
			if source, ok := input.InstalledPackTree[strings.TrimSuffix(install.Path, "/")+"/"+file.Path]; ok {
				sourceFiles[file.Path] = resolve.SourceFromParse(syntax.Parse(source))
			}
		}
		dependencies := make(map[string]string, len(install.Dependencies))
		for _, dependency := range install.Dependencies {
			dependencies[dependency.LocalName] = dependency.InstallName
		}
		canonicalID := install.CanonicalID
		if canonicalID == "" {
			canonicalID = install.InstallName
		}
		manifestID, manifestName := canonicalID, canonicalID
		if slash := strings.LastIndex(canonicalID, "/"); slash >= 0 && slash+1 < len(canonicalID) {
			manifestName = canonicalID[slash+1:]
		}
		installs[install.InstallName] = resolve.ResolvedPack{
			CanonicalID: canonicalID, Version: install.Version, Digest: install.Digest, Path: install.Path, Entry: install.Entry,
			Files: files, Dependencies: dependencies, SourceFiles: sourceFiles,
			Manifest: resolve.PackManifest{
				Format: input.ResolvedDependencies.Format, FormatVersion: input.ResolvedDependencies.FormatVersion,
				ID: manifestID, Name: manifestName, Version: install.Version, Language: input.ResolvedDependencies.Language,
				Entry: install.Entry,
			},
		}
	}
	return resolve.Input{
		Mode: resolve.CompileMode(input.Mode), EntryPath: input.EntryPath, RootPackID: input.RootPackID,
		Project: resolve.ProjectInput{Files: projectFiles},
		Packs: resolve.ResolvedDependencies{
			Format: input.ResolvedDependencies.Format, FormatVersion: input.ResolvedDependencies.FormatVersion,
			Language: input.ResolvedDependencies.Language, Installs: installs,
		},
	}
}

func testResolvedMetadata(input CompileInput, resolved resolve.Result) materialize.ResolvedMetadata {
	sources := make([]materialize.ResolvedSourceFile, 0, len(resolved.Modules))
	for _, module := range resolved.Modules {
		if module.Origin.Kind == resolve.OriginProject {
			if value, ok := input.ProjectSourceTree[module.Path]; ok {
				sources = append(sources, materialize.ResolvedSourceFile{Origin: resolve.SourceOrigin{Kind: resolve.OriginProject}, ModulePath: module.Path, Bytes: bytes.Clone(value)})
			}
			continue
		}
		if value, ok := input.InstalledPackTree[module.Path]; ok {
			sources = append(sources, materialize.ResolvedSourceFile{Origin: resolve.SourceOrigin{Kind: resolve.OriginPack, PackAddress: "ldl:pack:" + module.Origin.Publisher + ":" + module.Origin.PackName}, ModulePath: module.Path, Bytes: bytes.Clone(value)})
		}
	}
	assets := make([]materialize.ResolvedAsset, len(input.ReferencedAssets))
	for index, asset := range input.ReferencedAssets {
		origin := resolve.SourceOrigin{Kind: resolve.OriginProject}
		if asset.Origin == string(OriginKindPack) {
			origin = resolve.SourceOrigin{Kind: resolve.OriginPack}
		}
		assets[index] = materialize.ResolvedAsset{
			Origin: origin, Locator: asset.Locator, Bytes: bytes.Clone(asset.Bytes), ExpectedDigest: asset.Digest,
			ExpectedMediaType: asset.MediaType, ExpectedByteLength: asset.ByteLength,
		}
	}
	return materialize.ResolvedMetadata{Assets: assets, SourceFiles: sources}
}

func testDiagnostics(in []resolve.Diagnostic) []CompileDiagnostic {
	out := make([]CompileDiagnostic, len(in))
	for index, value := range in {
		out[index] = CompileDiagnostic{
			Code: value.Code, Severity: value.Severity, MessageKey: value.MessageKey, Message: value.Message,
			Range: value.Range, SubjectAddress: value.SubjectAddress, OwnerAddress: value.OwnerAddress, Related: value.Related,
		}
	}
	return out
}

func testStableAddresses(subjects []index.SemanticSubject) []string {
	addresses := make([]string, len(subjects))
	for index, subject := range subjects {
		addresses[index] = subject.Address
	}
	return addresses
}

func testAuthoringClassifications(subjects []index.SemanticSubject) []AuthoringSubjectClassification {
	out := make([]AuthoringSubjectClassification, 0, len(subjects))
	for _, subject := range subjects {
		if capability, ok := testSubjectCapability(subject.Kind); ok {
			out = append(out, AuthoringSubjectClassification{Address: subject.Address, Kind: subject.Kind, Capability: capability})
		}
	}
	return out
}

func testSubjectCapability(kind materialize.SubjectKind) (AuthoringCapability, bool) {
	switch kind {
	case materialize.SubjectProject:
		return AuthoringCapabilityProjectConfigure, true
	case materialize.SubjectEntityType, materialize.SubjectRelationType, materialize.SubjectLayer,
		materialize.SubjectEntityTypeColumn, materialize.SubjectEntityTypeConstraint,
		materialize.SubjectRelationTypeColumn, materialize.SubjectRelationTypeConstraint:
		return AuthoringCapabilitySchemaWrite, true
	case materialize.SubjectEntity, materialize.SubjectRelation, materialize.SubjectEntityRow, materialize.SubjectRelationRow:
		return AuthoringCapabilityGraphWrite, true
	case materialize.SubjectQuery, materialize.SubjectQueryParameter:
		return AuthoringCapabilityQueryWrite, true
	case materialize.SubjectView, materialize.SubjectViewTableColumn, materialize.SubjectViewExport:
		return AuthoringCapabilityViewWrite, true
	case materialize.SubjectReference:
		return AuthoringCapabilityReferenceWrite, true
	default:
		return "", false
	}
}
