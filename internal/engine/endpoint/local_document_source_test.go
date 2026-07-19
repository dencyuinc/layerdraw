// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"context"
	"strings"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

func TestLocalDocumentSourceAndRuntimeBridge(t *testing.T) {
	ctx := context.Background()
	project := LocalProjectInput{
		EntryPath:         "document.ldl",
		ProjectSourceTree: map[string][]byte{"document.ldl": []byte("project p \"P\" {}\n")},
		ResolvedDependencies: LocalResolvedDependencies{
			Format: "layerdraw-resolved", FormatVersion: 1, Language: 1,
		},
	}
	localEngine := NewLocalDocumentEngine()
	source, err := localEngine.CompileProject(ctx, project)
	if err != nil || source.PortableID != "ldl:project:p" || source.Digest() == "" {
		t.Fatalf("compile source=%+v err=%v", source, err)
	}
	if len(source.SubjectHashes()) == 0 || string(source.ProjectSourceTree()["document.ldl"]) != "project p \"P\" {}\n" {
		t.Fatalf("source projection=%q subjects=%+v", source.ProjectSourceTree(), source.SubjectHashes())
	}
	projectCopy := source.ProjectSourceTree()
	projectCopy["document.ldl"][0] = 'X'
	if string(source.ProjectSourceTree()["document.ldl"]) != "project p \"P\" {}\n" {
		t.Fatal("project source projection was not defensive")
	}
	encoded, ref, err := source.EncodedInput()
	if err != nil || ref.Digest != LocalCompileInputRef(encoded).Digest {
		t.Fatalf("encode ref=%+v err=%v", ref, err)
	}
	decoded, err := localEngine.ReadEncodedInput(ctx, encoded)
	if err != nil || decoded.DefinitionHash != source.DefinitionHash {
		t.Fatalf("decode source=%+v err=%v", decoded, err)
	}
	if _, err := localEngine.ReadEncodedInput(ctx, []byte(`{"unknown":true}`)); err == nil {
		t.Fatal("unknown encoded input was accepted")
	}

	archive, err := localEngine.WriteContainer(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	containerSource, err := localEngine.ReadContainer(ctx, archive)
	if err != nil || containerSource.PortableID != source.PortableID {
		t.Fatalf("container source=%+v err=%v", containerSource, err)
	}
	if _, err := localEngine.ReadContainer(ctx, []byte("not a container")); err == nil {
		t.Fatal("invalid container was accepted")
	}
	if _, err := localEngine.CompileProject(ctx, LocalProjectInput{EntryPath: "missing.ldl", ProjectSourceTree: map[string][]byte{}}); err == nil {
		t.Fatal("invalid project was accepted")
	}
	changed, err := localEngine.WithProjectTree(ctx, source, map[string][]byte{"document.ldl": []byte("project p \"Changed\" {}\n")})
	if err != nil || changed.DefinitionHash == source.DefinitionHash {
		t.Fatalf("changed source=%+v err=%v", changed, err)
	}

	bridge := localEngine.NewRuntimeEngineBridge("local-test-endpoint")
	if _, err := bridge.Open(ctx, "document_local", "revision_bad", source.GraphHash, source.GraphHash, encoded); err == nil {
		t.Fatal("mismatched semantic identity was accepted")
	}
	working, err := bridge.Open(ctx, "document_local", "revision_1", source.DefinitionHash, source.GraphHash, encoded)
	if err != nil {
		t.Fatal(err)
	}
	if found, ok := bridge.Working(working.Handle); !ok || found != working {
		t.Fatalf("working=%+v ok=%v", found, ok)
	}
	if found, ok := bridge.Opened(working.DocumentID, working.RevisionID); !ok || found != working {
		t.Fatalf("opened=%+v ok=%v", found, ok)
	}
	if digest, ok := bridge.SourceDigest(working.Handle); !ok || digest != ref.Digest {
		t.Fatalf("source digest=%s ok=%v want=%s", digest, ok, ref.Digest)
	}

	compiled, err := engine.New(engine.BuildInfo{}).Compile(ctx, source.input)
	if err != nil {
		t.Fatal(err)
	}
	preconditions := localBridgePreconditions(compiled.Snapshot(), working)
	batch, err := engineprotocol.DecodeSemanticOperationBatch([]byte(`{"operations":[{"operation":"create_subject","subject_kind":"layer","parent_address":"ldl:project:p","id":"extra","fields":{"display_name":"Extra","order":"10"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := bridge.Preview(ctx, working, batch, preconditions, 100)
	if err != nil || prepared.DefinitionHash == source.DefinitionHash || len(prepared.EncodedInput) == 0 {
		t.Fatalf("preview=%+v err=%v", prepared, err)
	}
	checkpoint, err := bridge.Checkpoint(ctx, working, prepared, "revision_2")
	if err != nil || checkpoint.Generation != "2" || checkpoint.RevisionID != "revision_2" {
		t.Fatalf("checkpoint=%+v err=%v", checkpoint, err)
	}
	if digest, ok := bridge.SourceDigest(checkpoint.Handle); !ok || digest == ref.Digest {
		t.Fatalf("checkpoint source digest=%s ok=%v", digest, ok)
	}
	if _, err := bridge.Checkpoint(ctx, working, prepared, "revision_3"); err == nil {
		t.Fatal("stale checkpoint was accepted")
	}
	if err := bridge.Close(working); err == nil {
		t.Fatal("stale close was accepted")
	}
	if err := bridge.Close(checkpoint); err != nil {
		t.Fatal(err)
	}
	if _, ok := bridge.Working(checkpoint.Handle); ok {
		t.Fatal("closed working document remained visible")
	}
	if _, ok := bridge.SourceDigest(checkpoint.Handle); ok {
		t.Fatal("closed source digest remained visible")
	}
	if _, ok := bridge.Opened("missing", "missing"); ok {
		t.Fatal("missing opened document was reported")
	}
	if _, err := bridge.Preview(ctx, checkpoint, batch, preconditions, 100); err == nil {
		t.Fatal("closed working document was previewed")
	}
	if err := bridge.Close(BridgeWorking{Handle: "unknown"}); err != nil {
		t.Fatalf("unknown close was not idempotent: %v", err)
	}
}

func TestHostEngineFacadeOwnsOneNegotiatedEngineBoundary(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	facade, err := NewHostEngineFacade("1.2.3", "abcdef0", digest, "host-facade-test", "stdio")
	if err != nil {
		t.Fatal(err)
	}
	if facade.Descriptor() == nil || facade.Dispatcher() == nil || facade.Negotiated() == nil || facade.ReleaseVersion() != "1.2.3" || !facade.Negotiated().SupportsOperation("engine.compile") {
		t.Fatalf("facade=%+v", facade)
	}
	if _, err := NewHostEngineFacade("1.2.3", "abcdef0", "invalid", "host-facade-test", "stdio"); err == nil {
		t.Fatal("invalid facade authority was accepted")
	}
}

func localBridgePreconditions(snapshot engine.Snapshot, working BridgeWorking) engineprotocol.EngineEditPreconditions {
	result := engineprotocol.EngineEditPreconditions{
		DocumentGeneration:    engineprotocol.DocumentGeneration{DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "local-test-endpoint", Value: working.Handle}, Value: protocolcommon.CanonicalUint64(working.Generation)},
		ExpectedSubjectHashes: []engineprotocol.ExpectedHash{}, ExpectedSubtreeHashes: []engineprotocol.ExpectedHash{}, ExpectedChildSets: []engineprotocol.ExpectedChildSet{},
	}
	for _, value := range snapshot.SubjectSemanticHashes {
		result.ExpectedSubjectHashes = append(result.ExpectedSubjectHashes, engineprotocol.ExpectedHash{Address: semantic.StableAddress(value.Address), Hash: protocolcommon.Digest(value.Hash)})
	}
	for _, value := range snapshot.SubtreeHashes {
		result.ExpectedSubtreeHashes = append(result.ExpectedSubtreeHashes, engineprotocol.ExpectedHash{Address: semantic.StableAddress(value.OwnerAddress), Hash: protocolcommon.Digest(value.Hash)})
	}
	for _, value := range snapshot.ChildSetHashes {
		result.ExpectedChildSets = append(result.ExpectedChildSets, engineprotocol.ExpectedChildSet{OwnerAddress: semantic.StableAddress(value.OwnerAddress), ChildKind: semantic.SubjectKind(value.ChildKind), Hash: protocolcommon.Digest(value.Hash)})
	}
	sources := []engineprotocol.ExpectedSourceDigest{}
	for _, file := range snapshot.SourceMap.Files {
		origin := semantic.SourceOrigin{Kind: semantic.OriginKind(file.Origin.Kind)}
		if file.Origin.PackAddress != "" {
			pack := semantic.PackRootAddress(file.Origin.PackAddress)
			origin.PackAddress = &pack
		}
		sources = append(sources, engineprotocol.ExpectedSourceDigest{Module: semantic.ModuleRef{Origin: origin, ModulePath: file.ModulePath}, Digest: protocolcommon.Digest(file.Digest)})
	}
	result.ExpectedSourceDigests = &sources
	return result
}
