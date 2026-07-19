// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package localdocument

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	accesscore "github.com/dencyuinc/layerdraw/internal/access"
	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

func TestDelegatedAgentRoutesEnforceProposalApplyAssetsRevocationAndRestart(t *testing.T) {
	root := t.TempDir()
	source := "project p \"P\" {}\n"
	project := writeProject(t, root, source)
	scheduler := &fakeScheduler{}
	host := newTestHost(t, filepath.Join(root, "data"), func(config *Config) { config.Scheduler = scheduler })
	owner, err := host.OpenProject(context.Background(), OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	batch := runtimeprotocol.RuntimeOperationBatch{DocumentID: owner.Session.Open.CommittedRevision.DocumentID, BaseRevision: owner.Session.Open.CommittedRevision, ExpectedDefinitionHash: owner.Session.Open.CommittedRevision.DefinitionHash, Operations: createLayerBatch(t, "delegated"), Preconditions: allPreconditions(t, source)}
	ownerPreview, err := host.Preview(context.Background(), runtimeprotocol.PreviewOperationsInput{Session: owner.Session.Open.Session, OperationBatch: batch})
	if err != nil {
		t.Fatal(err)
	}
	clock := host.config.Clock.Now()
	delegate := func(id string, apply bool) accesscore.Delegation {
		t.Helper()
		record, err := host.DelegateAgent(context.Background(), owner.Session, accesscore.Delegation{
			ID: id, ParentActor: accessprotocol.ActorRef{ActorID: "local-owner", Kind: "user"}, Agent: accessprotocol.ActorRef{ActorID: "agent-" + id, Kind: "agent"},
			DocumentID: string(batch.DocumentID), LocalScopeID: owner.Session.Open.Session.Scope.LocalScopeID,
			AuthoringCapabilities: append([]semantic.AuthoringCapability(nil), ownerPreview.PreviewEvaluation.AuthoringImpact.RequiredCapabilities...),
			Permissions:           accesscore.AgentPermissions{Read: true, Propose: true, Apply: apply}, IssuedAt: clock, ExpiresAt: clock.Add(time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
		return record
	}

	proposalOnly := delegate("proposal-only", false)
	proposalSession, err := host.OpenDelegatedDocument(context.Background(), batch.DocumentID, proposalOnly.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, surface := range []accesscore.ReadSurface{accesscore.SurfaceSearch, accesscore.SurfaceQuery, accesscore.SurfaceReview, accesscore.SurfaceMCP} {
		if err := host.AuthorizeReadSurface(context.Background(), proposalSession.Session.Open.Session, surface); err != nil {
			t.Fatalf("delegated %s read denied: %v", surface, err)
		}
	}
	if err := host.AuthorizeReadSurface(context.Background(), proposalSession.Session.Open.Session, accesscore.SurfaceExport); !errors.Is(err, accesscore.ErrReadDenied) {
		t.Fatalf("export=false delegation export=%v", err)
	}
	if err := host.AuthorizeReadSurface(context.Background(), proposalSession.Session.Open.Session, accesscore.ReadSurface("future")); !errors.Is(err, accesscore.ErrReadDenied) {
		t.Fatalf("unknown delegated read surface=%v", err)
	}
	if _, err := host.StateSnapshot(context.Background(), proposalSession.Session.Open.Session); err != nil {
		t.Fatalf("delegated query read=%v", err)
	}
	proposalPreview, err := host.Preview(context.Background(), runtimeprotocol.PreviewOperationsInput{Session: proposalSession.Session.Open.Session, OperationBatch: batch})
	if err != nil || proposalPreview.PreviewEvaluation.AuthoringDecision.Outcome != accessprotocol.AuthoringDecisionOutcomeAllow {
		t.Fatalf("proposal preview=%+v err=%v", proposalPreview, err)
	}
	proposalCommit := runtimeprotocol.RuntimeCommitInput{Session: proposalSession.Session.Open.Session, OperationID: "proposal_only_commit", IdempotencyKey: "proposal_only_idempotency", OperationBatch: batch, AuthoringProof: proposalPreview.AuthoringProof, Trigger: runtimeprotocol.CommitTriggerExplicitSave}
	if _, err := host.Commit(context.Background(), proposalCommit); err == nil {
		t.Fatal("proposal-only delegation applied a commit")
	}
	if inspected, err := host.Inspect(owner.Session.Open.Session); err != nil || inspected.CommittedRevision != owner.Session.Open.CommittedRevision {
		t.Fatalf("proposal-only path changed head: %+v err=%v", inspected, err)
	}

	applicable := delegate("applicable", true)
	applySession, err := host.OpenDelegatedDocument(context.Background(), batch.DocumentID, applicable.ID)
	if err != nil {
		t.Fatal(err)
	}
	applyPreview, err := host.Preview(context.Background(), runtimeprotocol.PreviewOperationsInput{Session: applySession.Session.Open.Session, OperationBatch: batch})
	if err != nil {
		t.Fatal(err)
	}
	assetBytes := []byte("delegated asset")
	assetSum := sha256.Sum256(assetBytes)
	asset := protocolcommon.BlobRef{BlobID: "asset/delegated.bin", Digest: protocolcommon.Digest("sha256:" + hex.EncodeToString(assetSum[:])), Lifetime: protocolcommon.BlobLifetimeRequest, MediaType: "application/octet-stream", Size: protocolcommon.CanonicalUint64("15")}
	if _, err := host.StageAsset(context.Background(), runtimeprotocol.StageAssetInput{Session: applySession.Session.Open.Session, ContentBlob: asset}, assetBytes); err == nil {
		t.Fatal("graph-only delegation bypassed asset authorization")
	}
	autosaveResult := make(chan AutosaveResult, 1)
	if err := host.ScheduleAutosave(context.Background(), SaveInput{Session: applySession.Session, Operations: batch.Operations, Preconditions: batch.Preconditions, OperationID: "revoked_autosave", IdempotencyKey: "revoked_autosave_idempotency"}, autosaveResult); err != nil {
		t.Fatal(err)
	}
	if err := host.RevokeDelegation(applicable.ID); err != nil {
		t.Fatal(err)
	}
	scheduler.fireLast()
	select {
	case result := <-autosaveResult:
		t.Fatalf("revoked pending autosave ran: %+v", result)
	default:
	}
	applyCommit := runtimeprotocol.RuntimeCommitInput{Session: applySession.Session.Open.Session, OperationID: "revoked_commit", IdempotencyKey: "revoked_commit_idempotency", OperationBatch: batch, AuthoringProof: applyPreview.AuthoringProof, Trigger: runtimeprotocol.CommitTriggerExplicitSave}
	if _, err := host.SaveRuntime(context.Background(), applyCommit); err == nil {
		t.Fatal("revoked delegation bypassed SaveRuntime authorization")
	}
	if _, err := host.Inspect(applySession.Session.Open.Session); !errors.Is(err, accesscore.ErrGrantStale) {
		t.Fatalf("revoked delegation review read=%v", err)
	}

	noRead, err := host.DelegateAgent(context.Background(), owner.Session, accesscore.Delegation{
		ID: "no-read", ParentActor: accessprotocol.ActorRef{ActorID: "local-owner", Kind: "user"}, Agent: accessprotocol.ActorRef{ActorID: "agent-no-read", Kind: "agent"},
		DocumentID: string(batch.DocumentID), LocalScopeID: owner.Session.Open.Session.Scope.LocalScopeID,
		AuthoringCapabilities: []semantic.AuthoringCapability{semantic.AuthoringCapabilityGraphWrite}, Permissions: accesscore.AgentPermissions{Propose: true}, IssuedAt: clock, ExpiresAt: clock.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.OpenDelegatedDocument(context.Background(), batch.DocumentID, noRead.ID); !errors.Is(err, accesscore.ErrReadDenied) {
		t.Fatalf("read=false delegation leaked open result: %v", err)
	}

	live := delegate("restart-live", true)
	if err := host.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted := newTestHost(t, filepath.Join(root, "data"), nil)
	if _, err := restarted.OpenDelegatedDocument(context.Background(), batch.DocumentID, applicable.ID); !errors.Is(err, accesscore.ErrGrantStale) {
		t.Fatalf("revoked delegation resurrected after restart: %v", err)
	}
	if _, err := restarted.OpenDelegatedDocument(context.Background(), batch.DocumentID, live.ID); err != nil {
		t.Fatalf("live delegation was not durable: %v", err)
	}
	info, err := os.Stat(delegationPath(filepath.Join(root, "data")))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("delegation file mode=%v err=%v", info.Mode().Perm(), err)
	}
}

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
