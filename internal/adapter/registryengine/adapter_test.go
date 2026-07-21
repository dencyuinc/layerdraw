// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package registryengine

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"testing"

	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/registry"
)

type snapshotReader struct {
	encoded []byte
	err     error
}

func (r snapshotReader) ReadRegistryProjectSnapshot(context.Context, registry.RegistryProjectSnapshot) ([]byte, error) {
	return append([]byte(nil), r.encoded...), r.err
}

func TestAdapterValidatesAndPlansPackMutation(t *testing.T) {
	ctx := context.Background()
	base := localProject(t, "project p \"P\" {}\n")
	objects, err := registry.NewDiskStagedObjectStore(t.TempDir(), registry.DefaultMaxStagedObjectBytes)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := New(objects, snapshotReader{encoded: base})
	if err != nil {
		t.Fatal(err)
	}
	pack := registryPack(t)
	release := registry.ArtifactRelease{
		Identity: registry.ArtifactIdentity{Kind: registry.ArtifactPack, CanonicalID: "example/schema", Version: "1.0.0"},
		SourceID: "official", Digest: digestValue(pack), DependencyMetadataDigest: digestValue("lock"),
	}
	validated, err := adapter.ValidateRegistryArtifact(ctx, release, pack)
	if err != nil || len(validated.StagedObjects) != 1 || validated.CanonicalDigest != release.Digest {
		t.Fatalf("validated=%+v err=%v", validated, err)
	}
	input := registry.RegistryMutationBuildInput{
		Action:            registry.ActionInstall,
		Project:           registry.ProjectState{Revision: "revision_1", DefinitionHash: digestValue("definition"), DependencySnapshot: registry.ProjectDependencySnapshot{ResolvedLockDigest: digestValue("old-lock")}, EngineSnapshot: registry.RegistryProjectSnapshot{Handle: "working"}},
		Artifacts:         []registry.PlanArtifact{{Release: release, Validation: validated}},
		ResolvedLockDelta: registry.ResolvedLockDelta{Added: []registry.LockedArtifact{{Identity: release.Identity}}},
	}
	plan, err := adapter.BuildRegistryMutationPlan(ctx, input)
	if err != nil || plan.BaseProjectRevision != input.Project.Revision || len(plan.StagedObjects) != 1 || plan.AuthoringImpactDigest == "" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}

	wrong := release
	wrong.Identity.CanonicalID = "example/wrong"
	if _, err := adapter.ValidateRegistryArtifact(ctx, wrong, pack); err == nil {
		t.Fatal("pack archive identity mismatch was accepted")
	}
	unsupported := release
	unsupported.Identity.Kind = registry.ArtifactKind("unknown")
	if _, err := adapter.ValidateRegistryArtifact(ctx, unsupported, pack); err == nil {
		t.Fatal("unsupported artifact kind was accepted")
	}
	badClosure := input
	badClosure.Artifacts[0].Validation.StagedObjects = nil
	if _, err := adapter.BuildRegistryMutationPlan(ctx, badClosure); err == nil {
		t.Fatal("pack plan without exactly one staged object was accepted")
	}
	readFailure, err := New(objects, snapshotReader{err: errors.New("snapshot unavailable")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readFailure.BuildRegistryMutationPlan(ctx, input); err == nil {
		t.Fatal("snapshot read failure was hidden")
	}
}

func TestAdapterValidatesAndPlansTemplateMutation(t *testing.T) {
	ctx := context.Background()
	base := localProject(t, "project p \"P\" {}\n")
	template := localContainer(t, "project p \"Template\" {}\nlayers {\n  main \"Main\" @1\n}\n")
	objects, err := registry.NewDiskStagedObjectStore(t.TempDir(), registry.DefaultMaxStagedObjectBytes)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := New(objects, snapshotReader{encoded: base})
	if err != nil {
		t.Fatal(err)
	}
	release := registry.ArtifactRelease{
		Identity: registry.ArtifactIdentity{Kind: registry.ArtifactTemplate, CanonicalID: "example/template", Version: "1.0.0"},
		Digest:   digestValue(template), DependencyMetadataDigest: digestValue("template-lock"),
	}
	validated, err := adapter.ValidateRegistryArtifact(ctx, release, template)
	if err != nil || len(validated.StagedObjects) != 1 || validated.AuthoringImpactDigest == "" {
		t.Fatalf("validated=%+v err=%v", validated, err)
	}
	input := registry.RegistryMutationBuildInput{
		Action: registry.ActionCreateFromTemplate, NewDocumentID: "document_new",
		Project:   registry.ProjectState{DefinitionHash: digestValue("empty"), DependencySnapshot: registry.ProjectDependencySnapshot{ResolvedLockDigest: digestValue("empty-lock")}, EngineSnapshot: registry.RegistryProjectSnapshot{Handle: "template"}},
		Artifacts: []registry.PlanArtifact{{Release: release, Validation: validated}},
	}
	plan, err := adapter.BuildRegistryMutationPlan(ctx, input)
	if err != nil || plan.BaseProjectRevision != "" || len(plan.StagedObjects) != 1 || plan.MutationDigest == "" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	if _, err := adapter.ValidateRegistryArtifact(ctx, release, []byte("not a container")); err == nil {
		t.Fatal("invalid template container was accepted")
	}
	input.Artifacts = nil
	if _, err := adapter.BuildRegistryMutationPlan(ctx, input); err == nil {
		t.Fatal("template plan without one artifact was accepted")
	}
}

func TestNewRejectsMissingPorts(t *testing.T) {
	objects, err := registry.NewDiskStagedObjectStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(nil, snapshotReader{}); err == nil {
		t.Fatal("nil object store was accepted")
	}
	if _, err := New(objects, nil); err == nil {
		t.Fatal("nil snapshot reader was accepted")
	}
}

func localProject(t *testing.T, source string) []byte {
	t.Helper()
	local := engineendpoint.NewLocalDocumentEngine()
	compiled, err := local.CompileProject(context.Background(), engineendpoint.LocalProjectInput{
		EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte(source)},
		ResolvedDependencies: engineendpoint.LocalResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _, err := compiled.EncodedInput()
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func localContainer(t *testing.T, source string) []byte {
	t.Helper()
	local := engineendpoint.NewLocalDocumentEngine()
	compiled, err := local.CompileProject(context.Background(), engineendpoint.LocalProjectInput{
		EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte(source)},
		ResolvedDependencies: engineendpoint.LocalResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	container, err := local.WriteContainer(context.Background(), compiled)
	if err != nil {
		t.Fatal(err)
	}
	return container
}

func registryPack(t *testing.T) []byte {
	t.Helper()
	files := map[string][]byte{
		"manifest.json": []byte("{\"dependencies\":{},\"entry\":\"pack.ldl\",\"format\":\"layerdraw-pack\",\"format_version\":1,\"id\":\"example/schema\",\"language\":1,\"name\":\"schema\",\"version\":\"1.0.0\"}\n"),
		"pack.ldl":      []byte("entity_type service \"Service\" {\n  representation shape rect\n}\nexport { service }\n"),
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, name := range []string{"manifest.json", "pack.ldl"} {
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
