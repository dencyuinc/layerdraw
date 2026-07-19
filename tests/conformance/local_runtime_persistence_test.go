// SPDX-License-Identifier: Apache-2.0

package conformance_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dencyuinc/layerdraw/gen/go/engineprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	"github.com/dencyuinc/layerdraw/internal/engine"
	"github.com/dencyuinc/layerdraw/internal/localdocument"
)

type persistenceCorpus struct {
	SchemaVersion int `json:"schema_version"`
	Workflow      struct {
		InitialSource  string                                `json:"initial_source"`
		ProjectAddress string                                `json:"project_address"`
		Operations     engineprotocol.SemanticOperationBatch `json:"operations"`
		AssetText      string                                `json:"asset_text"`
		Expected       struct {
			InitialHistoryItems     int                                          `json:"initial_history_items"`
			CommittedHistoryItems   int                                          `json:"committed_history_items"`
			InitialDefinitionHash   protocolcommon.Digest                        `json:"initial_definition_hash"`
			InitialGraphHash        protocolcommon.Digest                        `json:"initial_graph_hash"`
			CommittedDefinitionHash protocolcommon.Digest                        `json:"committed_definition_hash"`
			CommittedGraphHash      protocolcommon.Digest                        `json:"committed_graph_hash"`
			CommitStatus            runtimeprotocol.OperationResultStatus        `json:"commit_status"`
			ExternalState           runtimeprotocol.ExternalMaterializationState `json:"external_state"`
			StateKind               string                                       `json:"state_kind"`
		} `json:"expected"`
	} `json:"workflow"`
	FaultMatrix       json.RawMessage `json:"fault_matrix"`
	AdversarialMatrix []struct {
		ID   string `json:"id"`
		Path string `json:"path"`
		Test string `json:"test"`
	} `json:"adversarial_matrix"`
}

type fixedClock struct{ value time.Time }

func (c fixedClock) Now() time.Time { return c.value }

type deterministicReader struct{ value byte }

func (r *deterministicReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		r.value++
		buffer[index] = r.value
	}
	return len(buffer), nil
}

func loadPersistenceCorpus(t *testing.T) persistenceCorpus {
	t.Helper()
	data, err := os.ReadFile("testdata/local_runtime_persistence_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var corpus persistenceCorpus
	if err := decoder.Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.SchemaVersion != 1 {
		t.Fatalf("unsupported persistence corpus version %d", corpus.SchemaVersion)
	}
	return corpus
}

func TestLocalRuntimePersistenceCorpusThroughDirectGo(t *testing.T) {
	corpus := loadPersistenceCorpus(t)
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "document.ldl"), []byte(corpus.Workflow.InitialSource), 0o600); err != nil {
		t.Fatal(err)
	}
	dataRoot := filepath.Join(root, "runtime")
	host := newPersistenceHost(t, dataRoot, 0x42)

	opened, err := host.OpenProject(context.Background(), localdocument.OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	if opened.Session.PortableID != corpus.Workflow.ProjectAddress || len(opened.History.Items) != corpus.Workflow.Expected.InitialHistoryItems {
		t.Fatalf("initial projection=%s history=%d", opened.Session.PortableID, len(opened.History.Items))
	}
	if opened.Session.Open.CommittedRevision.DefinitionHash != corpus.Workflow.Expected.InitialDefinitionHash || opened.Session.Open.CommittedRevision.GraphHash != corpus.Workflow.Expected.InitialGraphHash {
		t.Fatalf("initial semantic identity=%+v", opened.Session.Open.CommittedRevision)
	}
	session := opened.Session.Open.Session
	inspection, err := host.Inspect(session)
	if err != nil || inspection.CommittedRevision != opened.Session.Open.CommittedRevision {
		t.Fatalf("inspection=%+v err=%v", inspection, err)
	}
	state, err := host.StateSnapshot(context.Background(), session)
	if err != nil || state.StateInput.Kind != corpus.Workflow.Expected.StateKind || state.StateInput.Snapshot == nil || state.StateInput.SnapshotHash == nil {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	repeatedState, err := host.StateSnapshot(context.Background(), session)
	if err != nil || !reflect.DeepEqual(repeatedState, state) {
		t.Fatalf("state snapshot was not deterministic: first=%+v repeated=%+v err=%v", state, repeatedState, err)
	}

	preconditions := persistencePreconditions(t, corpus.Workflow.InitialSource)
	operationBatch := runtimeprotocol.RuntimeOperationBatch{
		DocumentID:             opened.Session.Open.CommittedRevision.DocumentID,
		BaseRevision:           opened.Session.Open.CommittedRevision,
		ExpectedDefinitionHash: opened.Session.Open.CommittedRevision.DefinitionHash,
		Operations:             corpus.Workflow.Operations,
		Preconditions:          preconditions,
	}
	preview, err := host.Preview(context.Background(), runtimeprotocol.PreviewOperationsInput{Session: session, OperationBatch: operationBatch})
	if err != nil {
		t.Fatal(err)
	}
	commitInput := runtimeprotocol.RuntimeCommitInput{
		Session: session, OperationID: "operation_conformance", IdempotencyKey: "idempotency_conformance",
		OperationBatch: operationBatch, AuthoringProof: preview.AuthoringProof, Trigger: runtimeprotocol.CommitTriggerExplicitSave,
	}
	committed, err := host.Commit(context.Background(), commitInput)
	if err != nil {
		t.Fatal(err)
	}
	if committed.OperationResult.Status != corpus.Workflow.Expected.CommitStatus || committed.OperationResult.CommittedRevision == nil || committed.OperationResult.ExternalMaterialization == nil || committed.OperationResult.ExternalMaterialization.State != corpus.Workflow.Expected.ExternalState {
		t.Fatalf("commit=%+v", committed)
	}
	if committed.OperationResult.CommittedRevision.DefinitionHash != preview.DefinitionHash || committed.OperationResult.CommittedRevision.GraphHash != preview.GraphHash {
		t.Fatalf("preview/commit semantic identity diverged: preview=%+v commit=%+v", preview, committed.OperationResult.CommittedRevision)
	}
	if preview.DefinitionHash != corpus.Workflow.Expected.CommittedDefinitionHash || preview.GraphHash != corpus.Workflow.Expected.CommittedGraphHash {
		t.Fatalf("committed semantic identity drifted: definition=%s graph=%s", preview.DefinitionHash, preview.GraphHash)
	}
	if materialized, err := os.ReadFile(filepath.Join(project, "document.ldl")); err != nil || !bytes.Contains(materialized, []byte("layer_conformance")) {
		t.Fatalf("materialized source=%q err=%v", materialized, err)
	}
	retry, err := host.Commit(context.Background(), commitInput)
	if err != nil || !reflect.DeepEqual(retry.OperationResult, committed.OperationResult) {
		t.Fatalf("idempotent retry=%+v err=%v", retry, err)
	}
	history, err := host.ListRevisions(context.Background(), runtimeprotocol.ListRevisionsInput{Session: session, MaxItems: "20", MaxOutputBytes: "1048576"})
	if err != nil || len(history.Items) != corpus.Workflow.Expected.CommittedHistoryItems {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	if history.Items[0].Revision.RevisionID == history.Items[1].Revision.RevisionID || history.Items[0].CommittedAt > history.Items[1].CommittedAt {
		t.Fatalf("history ordering=%+v", history.Items)
	}
	if restored, err := host.PreviewRestore(context.Background(), runtimeprotocol.RestorePreviewInput{Session: session, RevisionID: history.Items[0].Revision.RevisionID}); err != nil || !restored.RequiresCommit {
		t.Fatalf("restore preview=%+v err=%v", restored, err)
	}

	asset := []byte(corpus.Workflow.AssetText)
	sum := sha256.Sum256(asset)
	assetRef := protocolcommon.BlobRef{BlobID: "asset/conformance.bin", Digest: protocolcommon.Digest("sha256:" + hex.EncodeToString(sum[:])), Lifetime: protocolcommon.BlobLifetimeRequest, MediaType: "application/octet-stream", Size: protocolcommon.CanonicalUint64(fmt.Sprint(len(asset)))}
	staged, err := host.StageAsset(context.Background(), runtimeprotocol.StageAssetInput{Session: session, ContentBlob: assetRef}, asset)
	if err != nil || staged.Asset.Blob.Lifetime != protocolcommon.BlobLifetimePersistent || staged.Asset.Blob.Digest != assetRef.Digest {
		t.Fatalf("staged asset=%+v err=%v", staged, err)
	}
	operationID := commitInput.OperationID
	status, err := host.OperationResult(context.Background(), runtimeprotocol.GetOperationResultInput{Session: session, LookupBy: "operation_id", OperationID: &operationID})
	if err != nil || status.OperationResult == nil || !reflect.DeepEqual(*status.OperationResult, committed.OperationResult) {
		t.Fatalf("operation result=%+v err=%v", status, err)
	}
	if cancelled, err := host.Cancel(context.Background(), runtimeprotocol.CancelOperationInput{Session: session, OperationID: operationID, CancellationToken: "cancel_conformance_1234"}); err != nil || cancelled.Status != "not_pending" {
		t.Fatalf("terminal cancellation=%+v err=%v", cancelled, err)
	}
	documentID := committed.OperationResult.CommittedRevision.DocumentID
	if err := host.Close(context.Background(), opened.Session); err != nil {
		t.Fatal(err)
	}
	if err := host.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	restarted := newPersistenceHost(t, dataRoot, 0x43)
	reopened, err := restarted.OpenDocument(context.Background(), documentID)
	if err != nil || !reflect.DeepEqual(reopened.Session.Open.CommittedRevision, *committed.OperationResult.CommittedRevision) {
		t.Fatalf("reopen=%+v err=%v", reopened, err)
	}
	if recovered, err := restarted.RecoverOperations(context.Background(), documentID); err != nil || len(recovered.Operations) != 0 {
		t.Fatalf("recovery=%+v err=%v", recovered, err)
	}

	containerEngine := engine.New(engine.BuildInfo{})
	firstContainer, err := containerEngine.WriteLayerdraw(context.Background(), engine.LayerdrawWriteInput{CompileInput: persistenceCompileInput(corpus.Workflow.InitialSource)})
	if err != nil {
		t.Fatal(err)
	}
	secondContainer, err := containerEngine.WriteLayerdraw(context.Background(), engine.LayerdrawWriteInput{CompileInput: persistenceCompileInput(corpus.Workflow.InitialSource)})
	if err != nil || !bytes.Equal(firstContainer, secondContainer) {
		t.Fatalf("container bytes were not deterministic: err=%v", err)
	}
	containerPath := filepath.Join(root, "portable.layerdraw")
	if err := os.WriteFile(containerPath, firstContainer, 0o600); err != nil {
		t.Fatal(err)
	}
	containerOpened, err := restarted.OpenContainer(context.Background(), containerPath)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := restarted.ImportContainer(context.Background(), containerPath)
	if err != nil || imported.Session.PortableID != containerOpened.Session.PortableID || imported.Session.Open.Session.Scope.DocumentID == containerOpened.Session.Open.Session.Scope.DocumentID {
		t.Fatalf("import-as-new opened=%+v imported=%+v err=%v", containerOpened, imported, err)
	}
}

func TestLocalRuntimePersistenceMatrixNamesExecutableAuthorities(t *testing.T) {
	corpus := loadPersistenceCorpus(t)
	root := portableCompileRepositoryRoot(t)
	wantAdversarial := []string{
		"asset_digest_mismatch", "concurrent_writers", "final_record_symlink", "intermediate_symlink",
		"malformed_protocol_frame", "oversized_protocol_frame", "path_traversal", "persisted_corruption",
		"private_root_permission_drift", "stale_head", "truncated_container",
	}
	adversarial := make([]string, 0, len(corpus.AdversarialMatrix))
	for _, authority := range corpus.AdversarialMatrix {
		if authority.ID == "" || authority.Path == "" || authority.Test == "" || filepath.IsAbs(authority.Path) || strings.Contains(filepath.ToSlash(authority.Path), "../") {
			t.Fatalf("invalid persistence authority: %+v", authority)
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(authority.Path)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "func "+authority.Test+"(") {
			t.Fatalf("%s does not contain executable authority %s", authority.Path, authority.Test)
		}
		adversarial = append(adversarial, authority.ID)
	}
	slices.Sort(adversarial)
	if !slices.Equal(adversarial, wantAdversarial) {
		t.Fatalf("adversarial persistence closure drifted: %v", adversarial)
	}

	var faults []struct {
		ID      string `json:"id"`
		Surface string `json:"surface"`
	}
	if err := json.Unmarshal(corpus.FaultMatrix, &faults); err != nil {
		t.Fatal(err)
	}
	surfaceSet := map[string]bool{}
	ids := map[string]bool{}
	for _, fault := range faults {
		if fault.ID == "" || fault.Surface == "" || ids[fault.ID] {
			t.Fatalf("invalid or duplicate persistence fault: %+v", fault)
		}
		ids[fault.ID], surfaceSet[fault.Surface] = true, true
	}
	surfaces := make([]string, 0, len(surfaceSet))
	for surface := range surfaceSet {
		surfaces = append(surfaces, surface)
	}
	slices.Sort(surfaces)
	wantSurfaces := []string{"external_recovery_phase", "filesystem_atomic", "filesystem_error", "in_memory", "local_lifecycle", "local_recovery_phase", "typescript_stdio"}
	if !slices.Equal(surfaces, wantSurfaces) {
		t.Fatalf("persistence fault surfaces drifted: %v", surfaces)
	}
}

func newPersistenceHost(t *testing.T, root string, seed byte) *localdocument.Host {
	t.Helper()
	host, err := localdocument.New(localdocument.Config{
		Root: root, ReleaseVersion: "1.0.0", EndpointInstanceID: "persistence-conformance",
		ReleaseManifestDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Clock:                 fixedClock{value: time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)},
		Random:                &deterministicReader{value: seed},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	return host
}

func persistenceCompileInput(source string) engine.CompileInput {
	return engine.CompileInput{
		Mode: engine.CompileProject, EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte(source)},
		ResolvedDependencies: engine.ResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1},
	}
}

func persistencePreconditions(t *testing.T, source string) engineprotocol.EngineEditPreconditions {
	t.Helper()
	compiled, err := engine.New(engine.BuildInfo{}).Compile(context.Background(), persistenceCompileInput(source))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := compiled.Snapshot()
	result := engineprotocol.EngineEditPreconditions{
		DocumentGeneration:    engineprotocol.DocumentGeneration{DocumentHandle: engineprotocol.DocumentHandle{EndpointInstanceID: "placeholder", Value: "document_placeholder_123456"}, Value: "1"},
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
	sources := make([]engineprotocol.ExpectedSourceDigest, 0, len(snapshot.SourceMap.Files))
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
