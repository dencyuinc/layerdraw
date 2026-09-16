// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package endpoint

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/internal/engine"
)

type countedBridgeEngine struct {
	engine.Engine
	plans, compiles int
}

func (e *countedBridgeEngine) Compile(ctx context.Context, input engine.CompileInput) (engine.CompileResult, error) {
	e.compiles++
	return e.Engine.Compile(ctx, input)
}

func (e *countedBridgeEngine) PlanSemanticEdits(ctx context.Context, input engine.SemanticEditPlanInput) (engine.SemanticEditPlan, error) {
	e.plans++
	return e.Engine.PlanSemanticEdits(ctx, input)
}

func bridgePreviewFixture(t *testing.T) (*RuntimeEngineBridge, *countedBridgeEngine, BridgeWorking, engineprotocol.SemanticOperationBatch, engineprotocol.EngineEditPreconditions) {
	t.Helper()
	ctx := context.Background()
	local := NewLocalDocumentEngine()
	source, err := local.CompileProject(ctx, LocalProjectInput{
		EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte("project p \"Project\" {}\n")},
		ResolvedDependencies: LocalResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _, err := source.EncodedInput()
	if err != nil {
		t.Fatal(err)
	}
	bridge := local.NewRuntimeEngineBridge("local-test-endpoint")
	working, err := bridge.Open(ctx, "document_local", "revision_1", source.DefinitionHash, source.GraphHash, encoded)
	if err != nil {
		t.Fatal(err)
	}
	driver := &countedBridgeEngine{Engine: local.engine}
	bridge.engine = driver
	pre := localBridgePreconditions(bridge.docs[working.Handle].snapshot, working)
	batch, err := engineprotocol.DecodeSemanticOperationBatch([]byte(`{"operations":[{"operation":"create_subject","subject_kind":"layer","parent_address":"ldl:project:p","id":"extra","fields":{"display_name":"Extra","order":"10"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	return bridge, driver, working, batch, pre
}

func TestRuntimeBridgeReusesOnlyItsOwnedCandidate(t *testing.T) {
	bridge, driver, working, batch, pre := bridgePreviewFixture(t)
	ctx := context.Background()
	first, err := bridge.Preview(ctx, working, batch, pre, 100)
	if err != nil {
		t.Fatal(err)
	}
	retained := bridge.docs[working.Handle].prepared
	retainedBytes := bridge.preparedBytes
	expected := bytes.Clone(first.EncodedInput)
	first.EncodedInput[0] = '!'
	first.AuthoringImpact.ImpactDigest = "sha256:mutated"
	first.Preview.Diagnostics = nil
	source, ok := first.Source()
	if !ok {
		t.Fatal("missing prepared source")
	}
	tree := source.ProjectSourceTree()
	tree["document.ldl"][0] = '!'
	if source.ProjectSourceTree()["document.ldl"][0] == '!' {
		t.Fatal("source projection aliases retained input")
	}

	second, err := bridge.Preview(ctx, working, batch, pre, 100)
	if err != nil {
		t.Fatal(err)
	}
	if driver.plans != 1 || bridge.docs[working.Handle].prepared != retained || bridge.preparedBytes != retainedBytes {
		t.Fatal("identical preview was recomputed or leaked retention")
	}
	if !bytes.Equal(second.EncodedInput, expected) || second.AuthoringImpact.ImpactDigest == first.AuthoringImpact.ImpactDigest {
		t.Fatal("public result changed retained candidate")
	}
	if _, err := bridge.Checkpoint(ctx, working, first, "revision_bad"); err == nil {
		t.Fatal("mutated candidate was accepted")
	}
	altered, _ := detachBridgePrepared(second)
	altered.EncodedInput = append(altered.EncodedInput, '\n')
	if _, err := bridge.Checkpoint(ctx, working, altered, "revision_bad"); err == nil {
		t.Fatal("same semantic hashes with different source bytes were accepted")
	}
	checkpoint, err := bridge.Checkpoint(ctx, working, second, "revision_2")
	if err != nil {
		t.Fatal(err)
	}
	if driver.compiles != 0 || driver.plans != 1 {
		t.Fatalf("checkpoint recomputed: compile=%d plan=%d", driver.compiles, driver.plans)
	}
	if bridge.preparedBytes != 0 || bridge.docs[working.Handle].prepared != nil {
		t.Fatal("checkpoint retained obsolete candidate")
	}
	if !reflect.DeepEqual(bridge.docs[working.Handle].snapshot, retained.snapshot) {
		t.Fatal("checkpoint installed a different snapshot")
	}
	if found, ok := bridge.Opened(working.DocumentID, "revision_2"); !ok || found != checkpoint {
		t.Fatal("new revision was not indexed")
	}
	if _, ok := bridge.Opened(working.DocumentID, "revision_1"); ok {
		t.Fatal("old revision lookup returned the new generation")
	}
	if _, err := bridge.Preview(ctx, working, batch, pre, 100); err == nil {
		t.Fatal("old generation was accepted")
	}
}

func TestRuntimeBridgeCandidateLimitsCancellationAndReplacement(t *testing.T) {
	bridge, driver, working, batch, pre := bridgePreviewFixture(t)
	ctx := context.Background()
	first, err := bridge.Preview(ctx, working, batch, pre, 100)
	if err != nil {
		t.Fatal(err)
	}
	retained := bridge.docs[working.Handle].prepared
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := bridge.Preview(cancelled, working, batch, pre, 100); err == nil {
		t.Fatal("cancelled cache hit was accepted")
	}
	if _, err := bridge.Checkpoint(cancelled, working, first, "revision_2"); err == nil {
		t.Fatal("cancelled checkpoint was accepted")
	}
	if _, err := bridge.Preview(ctx, working, batch, pre, 0); err == nil {
		t.Fatal("cache bypassed operation limit")
	}
	wrong := pre
	wrong.DocumentGeneration.Value = "99"
	if _, err := bridge.Preview(ctx, working, batch, wrong, 100); err == nil {
		t.Fatal("wrong generation was accepted")
	}
	wrong = pre
	wrong.DocumentGeneration.DocumentHandle.EndpointInstanceID = "other"
	if _, err := bridge.Preview(ctx, working, batch, wrong, 100); err == nil {
		t.Fatal("other endpoint was accepted")
	}
	if driver.plans != 1 {
		t.Fatal("admission failures executed planner")
	}
	// Use another valid request without sharing the previous operation's fields.
	other, err := engineprotocol.DecodeSemanticOperationBatch([]byte(`{"operations":[{"operation":"create_subject","subject_kind":"layer","parent_address":"ldl:project:p","id":"other","fields":{"display_name":"Other","order":"11"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	bridge.maxPreparedBytes = 1
	if _, err := bridge.Preview(ctx, working, other, pre, 100); err == nil {
		t.Fatal("retention limit was ignored")
	}
	if bridge.docs[working.Handle].prepared != retained {
		t.Fatal("failed admission replaced valid candidate")
	}
	bridge.maxPreparedBytes = engine.DefaultWorkbenchConfig().MaxRetainedBytes
	if _, err := bridge.Preview(ctx, working, other, pre, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.Checkpoint(ctx, working, first, "revision_2"); err == nil {
		t.Fatal("replaced candidate was accepted")
	}
	if err := bridge.Close(working); err != nil {
		t.Fatal(err)
	}
	if bridge.preparedBytes != 0 {
		t.Fatal("close leaked retained bytes")
	}
}

func TestRuntimeBridgeRegistryCheckpointUsesVerifiedSnapshot(t *testing.T) {
	bridge, driver, working, batch, pre := bridgePreviewFixture(t)
	ctx := context.Background()
	prepared, err := bridge.Preview(ctx, working, batch, pre, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := bridge.RetainRegistryPrepared(ctx, working, prepared); err != nil {
		t.Fatal(err)
	}
	if driver.compiles != 1 {
		t.Fatal("Registry input was not verified")
	}
	if _, err := bridge.Checkpoint(ctx, working, prepared, "revision_registry"); err != nil {
		t.Fatal(err)
	}
	if driver.compiles != 1 {
		t.Fatal("Registry checkpoint recompiled verified input")
	}
}
