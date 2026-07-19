// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package localdocument

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

func TestProtocolPreviewCommitAndStateSnapshot(t *testing.T) {
	root := t.TempDir()
	source := "project p \"P\" {}\n"
	project := writeProject(t, root, source)
	host := newTestHost(t, root+"/data", nil)
	opened, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	batch := runtimeprotocol.RuntimeOperationBatch{
		DocumentID:             opened.Session.Open.CommittedRevision.DocumentID,
		BaseRevision:           opened.Session.Open.CommittedRevision,
		ExpectedDefinitionHash: opened.Session.Open.CommittedRevision.DefinitionHash,
		Operations:             createLayerBatch(t, "protocol"), Preconditions: allPreconditions(t, source),
	}
	wireRequest := runtimeprotocol.PreviewOperationsRequestEnvelope{
		Operation: runtimeprotocol.PreviewOperationsRequestEnvelopeOperationValue,
		Protocol:  runtimeprotocol.RuntimeProtocolRef{Name: runtimeprotocol.RuntimeProtocolRefNameValue, Version: "1.0"},
		RequestID: "preview_roundtrip_request",
		Payload:   runtimeprotocol.PreviewOperationsInput{Session: opened.Session.Open.Session, OperationBatch: batch},
	}
	wireBytes, err := runtimeprotocol.EncodePreviewOperationsRequestEnvelope(wireRequest)
	if err != nil {
		t.Fatal(err)
	}
	roundTripped, err := runtimeprotocol.DecodePreviewOperationsRequestEnvelope(wireBytes)
	if err != nil {
		t.Fatal(err)
	}
	if batch.BaseRevision.ProviderVersion != nil && roundTripped.Payload.OperationBatch.BaseRevision.ProviderVersion == batch.BaseRevision.ProviderVersion {
		t.Fatal("wire roundtrip unexpectedly retained the ProviderVersion pointer")
	}
	preview, err := host.Preview(context.Background(), roundTripped.Payload)
	if err != nil {
		t.Fatal(err)
	}
	commit := runtimeprotocol.RuntimeCommitInput{Session: opened.Session.Open.Session, OperationID: "protocol_commit", IdempotencyKey: "protocol_commit_idempotency", OperationBatch: batch, AuthoringProof: preview.AuthoringProof, Trigger: runtimeprotocol.CommitTriggerExplicitSave}
	result, err := host.Commit(context.Background(), commit)
	if err != nil || result.OperationResult.CommittedRevision == nil {
		t.Fatalf("commit=%+v err=%v", result, err)
	}
	if inspected, err := host.Inspect(opened.Session.Open.Session); err != nil || inspected.CommittedRevision != *result.OperationResult.CommittedRevision {
		t.Fatalf("inspect=%+v err=%v", inspected, err)
	}
	history, err := host.ListRevisions(context.Background(), runtimeprotocol.ListRevisionsInput{Session: opened.Session.Open.Session, MaxItems: "20", MaxOutputBytes: "1048576"})
	if err != nil || len(history.Items) < 2 {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	if restored, err := host.PreviewRestore(context.Background(), runtimeprotocol.RestorePreviewInput{Session: opened.Session.Open.Session, RevisionID: history.Items[0].Revision.RevisionID}); err != nil || !restored.RequiresCommit {
		t.Fatalf("restore=%+v err=%v", restored, err)
	}
	lookupOperation := commit.OperationID
	if status, err := host.OperationResult(context.Background(), runtimeprotocol.GetOperationResultInput{Session: opened.Session.Open.Session, LookupBy: "operation_id", OperationID: &lookupOperation}); err != nil || status.OperationResult == nil {
		t.Fatalf("operation=%+v err=%v", status, err)
	}
	if cancelled, err := host.Cancel(context.Background(), runtimeprotocol.CancelOperationInput{Session: opened.Session.Open.Session, OperationID: commit.OperationID, CancellationToken: "cancel_protocol_123456"}); err != nil || cancelled.Status != "not_pending" {
		t.Fatalf("cancel=%+v err=%v", cancelled, err)
	}
	if recovered, err := host.RecoverOperations(context.Background(), opened.Session.Open.CommittedRevision.DocumentID); err != nil || recovered.Operations == nil {
		t.Fatalf("recovered=%+v err=%v", recovered, err)
	}
	assetBytes := []byte("protocol asset")
	assetDigest := sha256.Sum256(assetBytes)
	assetRef := protocolcommon.BlobRef{BlobID: "asset/protocol.bin", Digest: protocolcommon.Digest("sha256:" + hex.EncodeToString(assetDigest[:])), Lifetime: protocolcommon.BlobLifetimeRequest, MediaType: "application/octet-stream", Size: protocolcommon.CanonicalUint64("14")}
	if staged, err := host.StageAsset(context.Background(), runtimeprotocol.StageAssetInput{Session: opened.Session.Open.Session, ContentBlob: assetRef}, assetBytes); err != nil || staged.Asset.Blob.Lifetime != protocolcommon.BlobLifetimePersistent {
		t.Fatalf("asset=%+v err=%v", staged, err)
	}
	invalidAsset := assetRef
	invalidAsset.Size = "1"
	if _, err := host.StageAsset(context.Background(), runtimeprotocol.StageAssetInput{Session: opened.Session.Open.Session, ContentBlob: invalidAsset}, assetBytes); err == nil {
		t.Fatal("asset size mismatch was accepted")
	}
	conflictingBatch := batch
	conflictingBatch.DocumentID = "different_document"
	if _, err := host.Preview(context.Background(), runtimeprotocol.PreviewOperationsInput{Session: opened.Session.Open.Session, OperationBatch: conflictingBatch}); err == nil {
		t.Fatal("cross-document preview was accepted")
	}
	if control, err := host.ControlAutosave(context.Background(), runtimeprotocol.AutosaveControlInput{Session: opened.Session.Open.Session, Action: runtimeprotocol.AutosaveActionCancel}); err != nil || control.Scheduled {
		t.Fatalf("autosave cancel=%+v err=%v", control, err)
	}
	if _, err := host.SessionFor(runtimeprotocol.RuntimeSessionRef{}); err == nil {
		t.Fatal("unknown session resolved")
	}

	negotiated, err := host.Negotiate(runtimeprotocol.RuntimeHandshakeRequest{ClientRelease: "1.0.0", Protocols: []protocolcommon.ProtocolOffer{{Name: "runtime", SupportedRange: "1.0..1.0", Versions: []protocolcommon.ProtocolVersionBinding{{Version: "1.0", SchemaDigest: protocolcommon.Digest(runtimeprotocol.SchemaDigest)}}}}, RequiredCapabilities: []protocolcommon.CapabilityID{}, OptionalCapabilities: []protocolcommon.CapabilityID{}})
	if err != nil || len(negotiated.NegotiatedProtocols) != 1 {
		t.Fatalf("negotiated=%+v err=%v", negotiated, err)
	}

	testProtocolSaveAndAutosave(t, root, source)

	stateHost := newTestHost(t, root+"/state-data", nil)
	stateProject := writeProject(t, root+"/state", source)
	stateOpened, err := stateHost.OpenProject(context.Background(), OpenProjectInput{Root: stateProject})
	if err != nil {
		t.Fatal(err)
	}
	state, err := stateHost.StateSnapshot(context.Background(), stateOpened.Session.Open.Session)
	if err != nil || state.StateInput.Kind != "snapshot" || state.StateInput.Snapshot == nil || state.StateInput.SnapshotHash == nil {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	_, hash, err := engineendpoint.CanonicalizeStateQuerySnapshot(*state.StateInput.Snapshot)
	if err != nil || hash != *state.StateInput.SnapshotHash {
		t.Fatalf("snapshot hash=%s want=%s err=%v", hash, *state.StateInput.SnapshotHash, err)
	}
}

func testProtocolSaveAndAutosave(t *testing.T, root, source string) {
	t.Helper()
	for _, mode := range []string{"save", "autosave"} {
		host := newTestHost(t, root+"/"+mode+"-data", nil)
		project := writeProject(t, root+"/"+mode, source)
		opened, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
		if err != nil {
			t.Fatal(err)
		}
		batch := runtimeprotocol.RuntimeOperationBatch{DocumentID: opened.Session.Open.CommittedRevision.DocumentID, BaseRevision: opened.Session.Open.CommittedRevision, ExpectedDefinitionHash: opened.Session.Open.CommittedRevision.DefinitionHash, Operations: createLayerBatch(t, mode), Preconditions: allPreconditions(t, source)}
		preview, err := host.Preview(context.Background(), runtimeprotocol.PreviewOperationsInput{Session: opened.Session.Open.Session, OperationBatch: batch})
		if err != nil {
			t.Fatal(err)
		}
		commit := runtimeprotocol.RuntimeCommitInput{Session: opened.Session.Open.Session, OperationID: runtimeprotocol.OperationID("protocol_" + mode), IdempotencyKey: runtimeprotocol.IdempotencyKey("protocol_" + mode + "_idempotency"), OperationBatch: batch, AuthoringProof: preview.AuthoringProof, Trigger: runtimeprotocol.CommitTriggerExplicitSave}
		if mode == "save" {
			result, err := host.SaveRuntime(context.Background(), commit)
			if err != nil || result.OperationResult.CommittedRevision == nil {
				t.Fatalf("save=%+v err=%v", result, err)
			}
			continue
		}
		commit.Trigger = runtimeprotocol.CommitTriggerAutosave
		control, err := host.ControlAutosave(context.Background(), runtimeprotocol.AutosaveControlInput{Session: opened.Session.Open.Session, Action: runtimeprotocol.AutosaveActionSchedule, Commit: &commit})
		if err != nil || !control.Scheduled {
			t.Fatalf("schedule=%+v err=%v", control, err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for {
			operation := commit.OperationID
			status, lookupErr := host.OperationResult(context.Background(), runtimeprotocol.GetOperationResultInput{Session: opened.Session.Open.Session, LookupBy: "operation_id", OperationID: &operation})
			if lookupErr == nil && status.OperationResult != nil && status.OperationResult.CommittedRevision != nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("autosave status=%+v err=%v", status, lookupErr)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !strings.Contains(source, "project") {
		t.Fatal("invalid protocol fixture")
	}
}

func TestProtocolRejectsUntrustedReferences(t *testing.T) {
	host := newTestHost(t, t.TempDir(), nil)
	unknown := runtimeprotocol.RuntimeSessionRef{}
	ctx := context.Background()
	if _, err := host.Inspect(unknown); err == nil {
		t.Fatal("inspect accepted an unknown session")
	}
	if _, err := host.SaveRuntime(ctx, runtimeprotocol.RuntimeCommitInput{Session: unknown}); err == nil {
		t.Fatal("save accepted an unknown session")
	}
	if _, err := host.StateSnapshot(ctx, unknown); err == nil {
		t.Fatal("state snapshot accepted an unknown session")
	}
	if _, err := host.PreviewRestore(ctx, runtimeprotocol.RestorePreviewInput{Session: unknown, RevisionID: "revision_missing"}); err == nil {
		t.Fatal("restore accepted an unknown session")
	}
	if _, err := host.StageAsset(ctx, runtimeprotocol.StageAssetInput{Session: unknown}, nil); err == nil {
		t.Fatal("asset accepted an unknown session")
	}
	if _, err := host.ListRevisions(ctx, runtimeprotocol.ListRevisionsInput{Session: unknown}); err == nil {
		t.Fatal("history accepted an unknown session")
	}
	operation := runtimeprotocol.OperationID("operation_missing")
	if _, err := host.OperationResult(ctx, runtimeprotocol.GetOperationResultInput{Session: unknown, LookupBy: "operation_id", OperationID: &operation}); err == nil {
		t.Fatal("operation lookup accepted an unknown session")
	}
	if _, err := host.Cancel(ctx, runtimeprotocol.CancelOperationInput{Session: unknown, OperationID: operation, CancellationToken: "cancel_missing_123456"}); err == nil {
		t.Fatal("cancel accepted an unknown session")
	}
	if _, err := host.ControlAutosave(ctx, runtimeprotocol.AutosaveControlInput{Session: unknown, Action: runtimeprotocol.AutosaveActionSchedule}); err == nil {
		t.Fatal("autosave schedule accepted a missing commit")
	}
	mismatched := runtimeprotocol.RuntimeCommitInput{Session: runtimeprotocol.RuntimeSessionRef{RuntimeSessionID: "other_session"}}
	if _, err := host.ControlAutosave(ctx, runtimeprotocol.AutosaveControlInput{Session: unknown, Action: runtimeprotocol.AutosaveActionSchedule, Commit: &mismatched}); err == nil {
		t.Fatal("autosave schedule accepted a mismatched session")
	}
	binding := localStateBinding{}
	if _, err := binding.ResolveStateBackend(ctx, port.ResolveStateBackendInput{}); err == nil {
		t.Fatal("invalid state binding resolved")
	}
}
