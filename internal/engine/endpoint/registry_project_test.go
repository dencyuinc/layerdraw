// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"
)

func TestPrepareRegistryProjectAndTemplateUseClosedArtifacts(t *testing.T) {
	ctx := context.Background()
	engine := NewLocalDocumentEngine()
	base, err := engine.CompileProject(ctx, LocalProjectInput{
		EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte("project p \"Project\" {}\n")},
		ResolvedDependencies: LocalResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	baseEncoded, _, err := base.EncodedInput()
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := engine.PrepareRegistryProject(ctx, RegistryProjectMutationInput{
		BaseEncoded: baseEncoded, Artifacts: []RegistryProjectArtifactInput{{Bytes: endpointRegistryPack(t), RegistrySource: "official"}},
	})
	if err != nil || len(prepared.EncodedInput) == 0 || prepared.DefinitionHash != base.DefinitionHash || prepared.AuthoringImpact.ImpactDigest == "" {
		t.Fatalf("prepared=%+v err=%v", prepared, err)
	}
	bridge := engine.NewRuntimeEngineBridge("registry-test-endpoint")
	working, err := bridge.Open(ctx, "document_registry", "revision_1", base.DefinitionHash, base.GraphHash, baseEncoded)
	if err != nil {
		t.Fatal(err)
	}
	retained := BridgePrepared{AuthoringImpact: prepared.AuthoringImpact, DefinitionHash: prepared.DefinitionHash, GraphHash: prepared.GraphHash, EncodedInput: prepared.EncodedInput}
	if err := bridge.RetainRegistryPrepared(ctx, working, retained); err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.Checkpoint(ctx, working, retained, "revision_2"); err != nil {
		t.Fatal(err)
	}
	if err := bridge.RetainRegistryPrepared(ctx, working, retained); err == nil {
		t.Fatal("stale Registry preparation was retained")
	}
	if _, err := engine.PrepareRegistryProject(ctx, RegistryProjectMutationInput{BaseEncoded: []byte("invalid")}); err == nil {
		t.Fatal("invalid project snapshot was accepted")
	}

	templateSource, err := engine.CompileProject(ctx, LocalProjectInput{
		EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte("project p \"Template\" {}\nlayers {\n  main \"Main\" @1\n}\n")},
		ResolvedDependencies: LocalResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	template, err := engine.WriteContainer(ctx, templateSource)
	if err != nil {
		t.Fatal(err)
	}
	fromTemplate, err := engine.PrepareRegistryTemplate(ctx, baseEncoded, template)
	if err != nil || len(fromTemplate.EncodedInput) == 0 || fromTemplate.DefinitionHash != templateSource.DefinitionHash || fromTemplate.AuthoringImpact.ImpactDigest == "" {
		t.Fatalf("fromTemplate=%+v err=%v", fromTemplate, err)
	}
	if _, err := engine.PrepareRegistryTemplate(ctx, baseEncoded, []byte("invalid")); err == nil {
		t.Fatal("invalid template container was accepted")
	}
}

func endpointRegistryPack(t *testing.T) []byte {
	t.Helper()
	files := map[string][]byte{
		"manifest.json": []byte("{\"dependencies\":{},\"entry\":\"pack.ldl\",\"format\":\"layerdraw-pack\",\"format_version\":1,\"id\":\"example/schema\",\"language\":1,\"name\":\"schema\",\"version\":\"1.0.0\"}\n"),
		"pack.ldl":      []byte("entity_type service \"Service\" {\n  representation shape rect\n}\nexport { service }\n"),
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, name := range []string{"manifest.json", "pack.ldl"} {
		entry, err := writer.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
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
