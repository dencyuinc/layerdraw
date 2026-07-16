// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package engine

import (
	"bytes"
	"context"
	"reflect"
	"testing"
)

func TestWorkbenchPreviewSourcePatchPlansAgainstRetainedGeneration(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "source-plan"}})
	source := []byte("project p \"Project\" {}\n// keep\n")
	opened := openWorkbench(t, instance, projectCompileInput(string(source)))
	if !opened.Capabilities.PreviewSourcePatch || !opened.Capabilities.FormatScope || !opened.Capabilities.PreviewFragment || !opened.Capabilities.OrganizeWorkspace {
		t.Fatalf("source planning capabilities are disabled: %+v", opened.Capabilities)
	}

	start := bytes.Index(source, []byte("keep"))
	replacement := []byte("kept")
	blob := SourcePlannerBlobRef{
		BlobID:    "replacement",
		Digest:    sourcePlannerDigestForTest(replacement),
		Lifetime:  "request",
		MediaType: "text/plain; charset=utf-8",
		Size:      uint64(len(replacement)),
	}
	sources := []ExpectedSourceDigest{{
		Module: SourcePlannerModuleRef{Origin: SourcePlannerSourceOrigin{Kind: "project"}, ModulePath: "document.ldl"},
		Digest: sourcePlannerDigestForTest(source),
	}}
	plan, err := instance.PreviewSourcePatch(context.Background(), PreviewSourcePatchInput{
		Blobs:              SourcePlannerBlobs{"replacement": replacement},
		DocumentGeneration: opened.DocumentGeneration,
		Limits:             generousWorkbenchLimits,
		Preconditions: SourcePlannerPreconditions{
			ExpectedSourceDigests: &sources,
		},
		Patch: SourcePatchBatch{Patches: []SourcePatchInput{{
			SourceRange: SourcePlannerSourceRange{
				Origin:     SourcePlannerSourceOrigin{Kind: "project"},
				ModulePath: "document.ldl",
				StartByte:  start,
				EndByte:    start + len("keep"),
			},
			ExpectedSourceDigest: sourcePlannerDigestForTest(source),
			ReplacementBlob:      blob,
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Preview.Status != "valid" {
		t.Fatalf("preview status = %s diagnostics=%+v conflicts=%+v", plan.Preview.Status, plan.Preview.Diagnostics, plan.Preview.Conflicts)
	}
	if !bytes.Contains(plan.Candidate.ProjectSourceTree["document.ldl"], []byte("// kept\n")) {
		t.Fatalf("candidate did not include patch: %q", plan.Candidate.ProjectSourceTree["document.ldl"])
	}
	if opened.DocumentGeneration.Value != 1 {
		t.Fatalf("preview mutated generation: %+v", opened.DocumentGeneration)
	}
}

func TestWorkbenchPreviewSourcePatchRejectsStaleGeneration(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "source-plan-stale"}})
	opened := openWorkbench(t, instance, projectCompileInput("project p \"Project\" {}\n"))
	stale := opened.DocumentGeneration
	stale.Value++
	_, err := instance.PreviewSourcePatch(context.Background(), PreviewSourcePatchInput{
		DocumentGeneration: stale,
		Limits:             generousWorkbenchLimits,
	})
	if !IsWorkbenchError(err, WorkbenchErrorGenerationStale) {
		t.Fatalf("stale preview error = %v", err)
	}
}

func TestWorkbenchPreviewFragmentFormatAndOrganizePlans(t *testing.T) {
	instance := New(BuildInfo{Workbench: WorkbenchConfig{EndpointInstanceID: "source-plan-compose"}})
	source := []byte("project p \"Project\" {}\nentity_type   service   \"Service\"   {\n representation   shape   rect\n}\n")
	opened := openWorkbench(t, instance, projectCompileInput(string(source)))
	preconditions := sourcePlannerPreconditionsForTest(t, instance, opened.DocumentGeneration)

	fragment := []byte("entity_type database \"Database\" {\n representation shape cylinder\n}\n")
	fragmentRef := SourcePlannerBlobRef{
		BlobID: "fragment", Digest: sourcePlannerDigestForTest(fragment), Lifetime: "request",
		MediaType: "text/plain; charset=utf-8", Size: uint64(len(fragment)),
	}
	fragmentPlan, err := instance.PreviewFragment(context.Background(), PreviewFragmentInput{
		Blobs:              SourcePlannerBlobs{"fragment": fragment},
		DocumentGeneration: opened.DocumentGeneration,
		Limits:             generousWorkbenchLimits,
		Preconditions:      preconditions,
		Fragment: FragmentInput{
			Intent: "insert", InsertionOwner: workbenchProject,
			AllowedKinds: []SourcePlannerSubjectKind{"entity_type"}, FragmentBlob: fragmentRef,
		},
	})
	if err != nil || fragmentPlan.Preview.Status != "valid" {
		t.Fatalf("PreviewFragment() = %+v, %v", fragmentPlan.Preview, err)
	}
	if !bytes.Contains(fragmentPlan.Candidate.ProjectSourceTree["document.ldl"], []byte(`database "Database"`)) {
		t.Fatalf("fragment candidate missing database type: %q", fragmentPlan.Candidate.ProjectSourceTree["document.ldl"])
	}

	formatPlan, err := instance.FormatScope(context.Background(), FormatScopeInput{
		DocumentGeneration: opened.DocumentGeneration,
		Limits:             generousWorkbenchLimits,
		Preconditions:      preconditions,
		ScopeAddresses:     []SourcePlannerStableAddress{"ldl:project:p:entity-type:service"},
	})
	if err != nil || formatPlan.Preview.Status != "valid" {
		t.Fatalf("FormatScope() = %+v, %v", formatPlan.Preview, err)
	}
	if !bytes.Contains(formatPlan.Candidate.ProjectSourceTree["document.ldl"], []byte("entity_type service \"Service\"")) {
		t.Fatalf("format candidate did not normalize service type: %q", formatPlan.Candidate.ProjectSourceTree["document.ldl"])
	}

	organized, err := instance.OrganizeWorkspace(context.Background(), OrganizeWorkspaceInput{
		DocumentGeneration: opened.DocumentGeneration,
		Limits:             generousWorkbenchLimits,
		Preconditions:      preconditions,
		Strategy:           "standard_layout",
	})
	if err != nil || organized.Preview.Status != "valid" {
		t.Fatalf("OrganizeWorkspace() = %+v, %v", organized.Preview, err)
	}
	if _, ok := organized.Candidate.ProjectSourceTree["schema/entity_types/service.ldl"]; !ok {
		t.Fatalf("organized candidate missing standard schema module: %v", organized.Candidate.ProjectSourceTree)
	}
}

func TestSourcePlannerMappersClonePacksAndAssets(t *testing.T) {
	manifest := []byte(`{"format":"layerdraw-pack"}`)
	source := []byte("project p \"Project\" {}\n")
	asset := []byte("<svg/>")
	input := CompileInput{
		Mode: CompileProject, EntryPath: "document.ldl",
		ProjectSourceTree: map[string][]byte{"document.ldl": source},
		InstalledPackTree: map[string][]byte{"pack/a.ldl": []byte("entity_type a \"A\" {}\n")},
		ResolvedDependencies: ResolvedDependencies{Format: "layerdraw-pack", FormatVersion: 1, Language: 1, Installs: []ResolvedPack{{
			InstallName: "aws", CanonicalID: "layerdraw/aws", Version: "1.0.0", Digest: "sha256:pack", Path: "pack", Entry: "a.ldl",
			Files:        []ResolvedPackFile{{Path: "a.ldl", Digest: "sha256:file"}},
			Dependencies: []ResolvedPackDependency{{LocalName: "base", InstallName: "base-pack"}},
			ManifestPath: "layerdraw.pack.json", Manifest: manifest,
		}}},
		ReferencedAssets: []AssetInput{{Origin: SourceOriginProject, Locator: "asset.svg", Bytes: asset, Digest: "sha256:asset", MediaType: "image/svg+xml", ByteLength: int64(len(asset))}},
		ResourceLimits:   DefaultResourceLimits(),
	}

	plannerInput := mapSourcePlannerCompileInput(input)
	engineInput := mapEngineCompileInput(plannerInput)
	if !reflect.DeepEqual(input, engineInput) {
		t.Fatalf("round trip changed compile input\nwant=%+v\ngot=%+v", input, engineInput)
	}
	plannerInput.ProjectSourceTree["document.ldl"][0] = 'X'
	plannerInput.ResolvedDependencies.Installs[0].Manifest[0] = 'X'
	plannerInput.ReferencedAssets[0].Bytes[0] = 'X'
	if input.ProjectSourceTree["document.ldl"][0] == 'X' || input.ResolvedDependencies.Installs[0].Manifest[0] == 'X' || input.ReferencedAssets[0].Bytes[0] == 'X' {
		t.Fatal("planner mapping aliases caller-owned storage")
	}
}

func sourcePlannerDigestForTest(value []byte) SourcePlannerDigest {
	return SourcePlannerDigest(digestBytesForWorkbench(value))
}

func sourcePlannerPreconditionsForTest(t *testing.T, instance Engine, generation DocumentGeneration) SourcePlannerPreconditions {
	t.Helper()
	_, snapshot, err := instance.acquireSnapshot(context.Background(), generation)
	if err != nil {
		t.Fatal(err)
	}
	sources := make([]ExpectedSourceDigest, 0, len(snapshot.compiled.SourceMap.Files))
	for _, file := range snapshot.compiled.SourceMap.Files {
		if file.Origin.Kind == "project" {
			sources = append(sources, ExpectedSourceDigest{
				Module: SourcePlannerModuleRef{Origin: SourcePlannerSourceOrigin{Kind: "project"}, ModulePath: file.ModulePath},
				Digest: SourcePlannerDigest(file.Digest),
			})
		}
	}
	subjects := make([]ExpectedHash, len(snapshot.compiled.SubjectSemanticHashes))
	for index, item := range snapshot.compiled.SubjectSemanticHashes {
		subjects[index] = ExpectedHash{Address: SourcePlannerStableAddress(item.Address), Hash: SourcePlannerDigest(item.Hash)}
	}
	subtrees := make([]ExpectedHash, len(snapshot.compiled.SubtreeHashes))
	for index, item := range snapshot.compiled.SubtreeHashes {
		subtrees[index] = ExpectedHash{Address: SourcePlannerStableAddress(item.OwnerAddress), Hash: SourcePlannerDigest(item.Hash)}
	}
	children := make([]ExpectedChildSet, len(snapshot.compiled.ChildSetHashes))
	for index, item := range snapshot.compiled.ChildSetHashes {
		children[index] = ExpectedChildSet{
			OwnerAddress: SourcePlannerStableAddress(item.OwnerAddress),
			ChildKind:    SourcePlannerSubjectKind(item.ChildKind),
			Hash:         SourcePlannerDigest(item.Hash),
		}
	}
	return SourcePlannerPreconditions{
		ExpectedSourceDigests: &sources,
		ExpectedSubjectHashes: subjects,
		ExpectedSubtreeHashes: subtrees,
		ExpectedChildSets:     children,
	}
}
