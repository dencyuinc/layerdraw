// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package localdocument

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/dencyuinc/layerdraw/gen/go/accessprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/gen/go/semantic"
	reviewapp "github.com/dencyuinc/layerdraw/internal/application/review"
	engineendpoint "github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/registry"
	runtimehost "github.com/dencyuinc/layerdraw/internal/runtime"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

type registryObjectReaderFixture map[string][]byte

func (r registryObjectReaderFixture) OpenRegistryStagedObject(_ context.Context, ref port.RegistryStagedObjectRef) (io.ReadCloser, error) {
	value, ok := r[ref.ObjectID]
	if !ok {
		return nil, port.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(value)), nil
}

func TestRegistryTemplateReservationAndSnapshotAreEngineBound(t *testing.T) {
	host := newTestHost(t, filepath.Join(t.TempDir(), "data"), nil)
	defer host.Shutdown(context.Background())
	identity := registry.ArtifactIdentity{Kind: registry.ArtifactTemplate, CanonicalID: "example/template", Version: "1.0.0"}
	state, err := host.NewRegistryDocumentState(context.Background(), identity)
	if err != nil || state.DocumentID == "" || state.Revision != "" || state.EngineSnapshot.Kind != registry.RegistryProjectSnapshotEmptyTemplate {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	encoded, err := host.ReadRegistryProjectSnapshot(context.Background(), state.EngineSnapshot)
	if err != nil || len(encoded) == 0 {
		t.Fatalf("snapshot bytes=%d err=%v", len(encoded), err)
	}
	stale := state.EngineSnapshot
	stale.SourceClosureDigest = registryDigestEmptyLock()
	if _, err := host.ReadRegistryProjectSnapshot(context.Background(), stale); err == nil {
		t.Fatal("stale Engine snapshot was accepted")
	}
	projectSnapshot := registry.ProjectDependencySnapshot{ResolvedLockDigest: registryDigestEmptyLock(), Installs: []registry.LockedArtifact{}}
	if err := host.ApplyRegistryProjectState("document_registry", projectSnapshot, state.PackTreeManifest); err != nil {
		t.Fatal(err)
	}
	if err := host.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := host.ApplyRegistryProjectState("document_registry", projectSnapshot, state.PackTreeManifest); err == nil {
		t.Fatal("closed host accepted Registry project metadata")
	}
}

func TestRegistryMutationMappingClosesArtifactAndDependencyClosure(t *testing.T) {
	pack := registry.LockedArtifact{Identity: registry.ArtifactIdentity{Kind: registry.ArtifactPack, CanonicalID: "example/pack", Version: "1.0.0"}}
	old := registry.LockedArtifact{Identity: registry.ArtifactIdentity{Kind: registry.ArtifactPack, CanonicalID: "example/old", Version: "1.0.0"}}
	ref := registry.StagedObjectRef{ObjectID: "pack.blob", Digest: registryDigestEmptyLock(), Size: 12, MediaType: "application/vnd.layerdraw.pack"}
	impact := &semantic.AuthoringImpact{ImpactDigest: protocolcommon.Digest(registryDigestEmptyLock())}
	input := registry.RuntimeCommitInput{
		OperationID: "operation", IdempotencyKey: "idempotency", LeaseToken: "lease", AuthoringImpact: impact,
		Plan: registry.InstallPlan{
			TransactionID: "transaction", PlanDigest: registryDigestEmptyLock(), ExpectedResolvedLockDigest: registryDigestEmptyLock(),
			DependencySnapshot: registry.ProjectDependencySnapshot{ResolvedLockDigest: registryDigestEmptyLock(), Installs: []registry.LockedArtifact{old}},
			ResolvedLockDelta:  registry.ResolvedLockDelta{Added: []registry.LockedArtifact{pack}, Removed: []registry.LockedArtifact{old}},
			Artifacts:          []registry.PlanArtifact{{Release: registry.ArtifactRelease{SourceID: "official"}, Validation: registry.ValidatedArtifact{StagedObjects: []registry.StagedObjectRef{ref}}}},
		},
		MutationPlan: registry.ProjectMutationPlan{
			MutationDigest: registryDigestEmptyLock(), StagedTreeManifest: registryDigestEmptyLock(), StagedObjects: []registry.StagedObjectRef{ref},
			EngineSnapshot: registry.RegistryProjectSnapshot{Handle: "working", GraphHash: registryDigestEmptyLock(), SourceClosureDigest: registryDigestEmptyLock()},
		},
	}
	input.Plan.ProjectMutationPlan = input.MutationPlan
	mutation, staged, err := registryMutation(input)
	if err != nil || len(staged) != 1 || len(mutation.Artifacts) != 1 || mutation.Artifacts[0].RegistrySource != "official" {
		t.Fatalf("mutation=%+v staged=%+v err=%v", mutation, staged, err)
	}
	updated := dependencySnapshotAfter(input.Plan)
	if updated.ResolvedLockDigest != input.MutationPlan.StagedTreeManifest || len(updated.Installs) != 1 || updated.Installs[0].Identity.CanonicalID != pack.Identity.CanonicalID {
		t.Fatalf("updated=%+v", updated)
	}
	if _, err := registryRuntimeInput(input, runtimeprotocol.RuntimeSessionRef{}, runtimeprotocol.CommittedRevisionRef{}); err != nil {
		t.Fatal(err)
	}

	invalid := input
	invalid.AuthoringImpact = nil
	if _, _, err := registryMutation(invalid); err == nil {
		t.Fatal("mutation without authoring impact was accepted")
	}
	invalid = input
	invalid.MutationPlan.StagedObjects[0].Size = -1
	if _, _, err := registryMutation(invalid); err == nil {
		t.Fatal("negative staged object size was accepted")
	}
	invalid = input
	invalid.Plan.Artifacts[0].Validation.StagedObjects[0].ObjectID = "unbound.blob"
	if _, _, err := registryMutation(invalid); err == nil {
		t.Fatal("unbound artifact object was accepted")
	}
}

func TestRegistryLookupAndSessionUnknownAreClosed(t *testing.T) {
	host := newTestHost(t, filepath.Join(t.TempDir(), "data"), nil)
	defer host.Shutdown(context.Background())
	if _, err := host.registrySession("missing"); err == nil {
		t.Fatal("unknown Runtime session was accepted")
	}
	outcome, err := host.LookupRegistryCommit(context.Background(), "missing", "operation", "idempotency")
	if outcome.Status != registry.RuntimeRegistryUnknown || err == nil {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	if _, err := host.registryCommittedSource(context.Background(), runtimeprotocol.CommittedRevisionRef{DocumentID: "missing", RevisionID: "missing"}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("missing committed source err=%v", err)
	}
}

func TestRegistryWorkbenchPreparesPackAndInitialTemplateFromStagedObjects(t *testing.T) {
	ctx := context.Background()
	pack := localRegistryPack(t)
	templateEngine := engineendpoint.NewLocalDocumentEngine()
	templateSource, err := templateEngine.CompileProject(ctx, engineendpoint.LocalProjectInput{
		EntryPath: "document.ldl", ProjectSourceTree: map[string][]byte{"document.ldl": []byte("project template \"From Template\" {}\nlayers {\n  main \"Main\" @1\n}\n")},
		ResolvedDependencies: engineendpoint.LocalResolvedDependencies{Format: "layerdraw-resolved", FormatVersion: 1, Language: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	template, err := templateEngine.WriteContainer(ctx, templateSource)
	if err != nil {
		t.Fatal(err)
	}
	reader := registryObjectReaderFixture{"pack.blob": pack, "template.blob": template}
	root := t.TempDir()
	host := newTestHost(t, filepath.Join(root, "data"), func(config *Config) { config.RegistryStagedObjects = reader })
	defer host.Shutdown(ctx)
	project := writeProject(t, root, "project p \"P\" {}\n")
	opened, err := host.OpenProject(ctx, OpenProjectInput{Root: project})
	if err != nil {
		t.Fatal(err)
	}
	state, err := host.CurrentRegistryProjectState(ctx, "ldl:project:p")
	if err != nil || state.RuntimeSessionID == "" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	packRef := port.RegistryStagedObjectRef{ObjectID: "pack.blob", Digest: protocolcommon.Digest(registryDigestEmptyLock()), Size: "1", MediaType: "application/vnd.layerdraw.pack"}
	prepared, err := host.workbench.PrepareRegistryRevision(ctx, port.PrepareRegistryRevisionInput{
		BaseRevision:    opened.Session.Open.CommittedRevision,
		ProjectMutation: port.RegistryProjectMutation{SnapshotHandle: state.EngineSnapshot.Handle, SourceClosureDigest: protocolcommon.Digest(state.EngineSnapshot.SourceClosureDigest), Artifacts: []port.RegistryProjectArtifactRef{{Object: packRef, RegistrySource: "official"}}},
	})
	if err != nil || len(prepared.Sources.Blobs) != 1 || prepared.AuthoringImpact.ImpactDigest == "" {
		t.Fatalf("prepared=%+v err=%v", prepared, err)
	}
	if prepared.AuthoringImpact.Entries == nil {
		prepared.AuthoringImpact.Entries = []semantic.AuthoringImpactEntry{}
	}
	packRegistryRef := registry.StagedObjectRef{ObjectID: "pack.blob", Digest: registryDigestEmptyLock(), Size: int64(len(pack)), MediaType: "application/vnd.layerdraw.pack"}
	packMutation := registry.ProjectMutationPlan{MutationDigest: registryDigestEmptyLock(), StagedTreeManifest: registryDigestEmptyLock(), StagedObjects: []registry.StagedObjectRef{packRegistryRef}, EngineSnapshot: state.EngineSnapshot}
	packPlan := registry.InstallPlan{TransactionID: "transaction-0000000000", PlanDigest: registryDigestEmptyLock(), ExpectedResolvedLockDigest: state.DependencySnapshot.ResolvedLockDigest, DependencySnapshot: state.DependencySnapshot, ResolvedLockDelta: registry.ResolvedLockDelta{}, Artifacts: []registry.PlanArtifact{{Release: registry.ArtifactRelease{SourceID: "official"}, Validation: registry.ValidatedArtifact{StagedObjects: []registry.StagedObjectRef{packRegistryRef}}}}, ProjectMutationPlan: packMutation}
	evaluation := accessprotocol.EvaluateAuthoringInput{AuthoringImpact: &prepared.AuthoringImpact, GrantSnapshot: state.GrantSnapshot, HostOperationImpacts: []accessprotocol.HostOperationImpact{}, RequestIntent: "apply", BaseRevisionDigest: digestJSON(opened.Session.Open.CommittedRevision)}
	if _, encodeErr := accessprotocol.EncodeEvaluateAuthoringInput(evaluation); encodeErr != nil {
		t.Fatalf("encode evaluation: %v", encodeErr)
	}
	packDecision, rejection := host.runtime.Authorize(host.accessContext(ctx, opened.Session), runtimehost.AuthorizationRequest{Scope: opened.Session.Open.Session.Scope, CurrentRevision: opened.Session.Open.CommittedRevision, Evaluation: evaluation})
	if rejection != nil {
		t.Fatal(rejection)
	}
	packCommitted, err := host.CommitRegistryPlan(ctx, registry.RuntimeCommitInput{Plan: packPlan, OperationID: "registry-pack-operation", IdempotencyKey: "registry-pack-key", AuthoringImpact: &prepared.AuthoringImpact, HostOperationImpacts: []accessprotocol.HostOperationImpact{}, AccessDecision: packDecision, MutationPlan: packMutation, RuntimeSessionID: state.RuntimeSessionID})
	if err != nil || packCommitted.CommittedRevision == "" || packCommitted.DocumentID != state.DocumentID {
		t.Fatalf("pack committed=%+v err=%v", packCommitted, err)
	}
	lookup, err := host.LookupRegistryCommit(ctx, state.DocumentID, "registry-pack-operation", "registry-pack-key")
	if err != nil || lookup.Status != registry.RuntimeRegistryCommitted || lookup.Result.CommittedRevision != packCommitted.CommittedRevision {
		t.Fatalf("lookup=%+v err=%v", lookup, err)
	}
	sessionRef, actor, err := host.ReviewBinding(opened.Session.Open.Session.Scope.DocumentID)
	if err != nil || sessionRef.RuntimeSessionID == "" || actor.ActorID == "" {
		t.Fatalf("review binding=%+v actor=%+v err=%v", sessionRef, actor, err)
	}
	approved, err := host.AuthorizeApprover(ctx, reviewapp.ApprovalRequest{Approver: actor, Revision: opened.Session.Open.CommittedRevision, Impact: prepared.AuthoringImpact, Decision: packDecision})
	if err != nil || approved.Outcome != accessprotocol.AuthoringDecisionOutcomeAllow {
		t.Fatalf("approval=%+v err=%v", approved, err)
	}
	wrongActor := actor
	wrongActor.ActorID = "other"
	if _, err := host.AuthorizeApprover(ctx, reviewapp.ApprovalRequest{Approver: wrongActor, Revision: opened.Session.Open.CommittedRevision, Impact: prepared.AuthoringImpact}); err == nil {
		t.Fatal("non-owner approver was accepted")
	}
	if _, _, err := host.ReviewBinding("missing"); err == nil {
		t.Fatal("missing review project was bound")
	}
	if _, err := host.Repreview(ctx, reviewapp.RepreviewInput{}); err == nil {
		t.Fatal("review preview without a session was accepted")
	}
	if views, err := host.ProjectViews(ctx, opened.Session.Open.Session); err != nil || len(views) != 0 {
		t.Fatalf("views=%+v err=%v", views, err)
	}
	if _, err := host.MaterializeProjectView(ctx, opened.Session.Open.Session, "missing"); err == nil {
		t.Fatal("missing view was materialized")
	}
	if host.DataRoot() == "" {
		t.Fatal("host data root is empty")
	}
	if err := host.CancelAutosave(opened.Session.Open.Session); err != nil {
		t.Fatal(err)
	}

	reserved, err := host.NewRegistryDocumentState(ctx, registry.ArtifactIdentity{Kind: registry.ArtifactTemplate, CanonicalID: "example/template", Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	templateRef := port.RegistryStagedObjectRef{ObjectID: "template.blob", Digest: protocolcommon.Digest(registryDigestEmptyLock()), Size: "1", MediaType: "application/vnd.layerdraw.project"}
	initial, err := host.workbench.PrepareInitialRegistryRevision(ctx, port.PrepareInitialRegistryRevisionInput{
		BaselineRevision: runtimeprotocol.CommittedRevisionRef{DocumentID: runtimeprotocol.DocumentID(reserved.DocumentID), RevisionID: "registry_empty_baseline", DefinitionHash: protocolcommon.Digest(reserved.DefinitionHash), GraphHash: protocolcommon.Digest(reserved.EngineSnapshot.GraphHash)},
		ProjectMutation:  port.RegistryProjectMutation{SnapshotHandle: reserved.EngineSnapshot.Handle, SourceClosureDigest: protocolcommon.Digest(reserved.EngineSnapshot.SourceClosureDigest), Artifacts: []port.RegistryProjectArtifactRef{{Object: templateRef}}},
	})
	if err != nil || len(initial.Sources.Blobs) != 1 || initial.DefinitionHash != templateSource.DefinitionHash {
		t.Fatalf("initial=%+v err=%v", initial, err)
	}
	scope := host.authority.add(runtimeprotocol.DocumentID(reserved.DocumentID))
	published, err := host.registryInitial.PublishInitialRegistryRevision(ctx, port.PublishInitialRegistryRevisionInput{Scope: scope, OperationID: "initial", IdempotencyKey: "initial-key", Prepared: initial})
	if err != nil || published.DocumentID != runtimeprotocol.DocumentID(reserved.DocumentID) || published.DefinitionHash != initial.DefinitionHash {
		t.Fatalf("published=%+v err=%v", published, err)
	}
	replayed, err := host.registryInitial.PublishInitialRegistryRevision(ctx, port.PublishInitialRegistryRevisionInput{Scope: scope, OperationID: "initial", IdempotencyKey: "initial-key", Prepared: initial})
	if err != nil || !sameCommittedRevision(replayed, published) {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}
	committedSource, err := host.registryCommittedSource(ctx, published)
	if err != nil || committedSource.PortableID != templateSource.PortableID {
		t.Fatalf("committed source=%+v err=%v", committedSource, err)
	}
	containerPath := filepath.Join(root, "template.layerdraw")
	if err := os.WriteFile(containerPath, template, 0o600); err != nil {
		t.Fatal(err)
	}
	imported, err := host.ImportContainer(ctx, containerPath)
	if err != nil || imported.Session.PortableID != templateSource.PortableID {
		t.Fatalf("imported=%+v err=%v", imported, err)
	}
	if err := host.Close(ctx, imported.Session); err != nil {
		t.Fatal(err)
	}
	reservedCommit, err := host.NewRegistryDocumentState(ctx, registry.ArtifactIdentity{Kind: registry.ArtifactTemplate, CanonicalID: "example/template", Version: "2.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	baseline := runtimeprotocol.CommittedRevisionRef{DocumentID: runtimeprotocol.DocumentID(reservedCommit.DocumentID), RevisionID: "registry_empty_baseline", DefinitionHash: protocolcommon.Digest(reservedCommit.DefinitionHash), GraphHash: protocolcommon.Digest(reservedCommit.EngineSnapshot.GraphHash)}
	initialCommit, err := host.workbench.PrepareInitialRegistryRevision(ctx, port.PrepareInitialRegistryRevisionInput{BaselineRevision: baseline, ProjectMutation: port.RegistryProjectMutation{SnapshotHandle: reservedCommit.EngineSnapshot.Handle, SourceClosureDigest: protocolcommon.Digest(reservedCommit.EngineSnapshot.SourceClosureDigest), Artifacts: []port.RegistryProjectArtifactRef{{Object: templateRef}}}})
	if err != nil {
		t.Fatal(err)
	}
	grant, _, err := host.authority.ResolveGrant(ctx, host.authority.add(runtimeprotocol.DocumentID(reservedCommit.DocumentID)))
	if err != nil {
		t.Fatal(err)
	}
	decision, rejection := host.runtime.Authorize(ctx, runtimehost.AuthorizationRequest{Scope: host.authority.add(runtimeprotocol.DocumentID(reservedCommit.DocumentID)), CurrentRevision: baseline, Evaluation: accessprotocol.EvaluateAuthoringInput{AuthoringImpact: &initialCommit.AuthoringImpact, GrantSnapshot: grant, HostOperationImpacts: []accessprotocol.HostOperationImpact{}, RequestIntent: "apply"}})
	if rejection != nil {
		t.Fatal(rejection)
	}
	registryRef := registry.StagedObjectRef{ObjectID: "template.blob", Digest: registryDigestEmptyLock(), Size: int64(len(template)), MediaType: "application/vnd.layerdraw.project"}
	mutationPlan := registry.ProjectMutationPlan{MutationDigest: registryDigestEmptyLock(), StagedTreeManifest: registryDigestEmptyLock(), StagedObjects: []registry.StagedObjectRef{registryRef}, EngineSnapshot: reservedCommit.EngineSnapshot}
	plan := registry.InstallPlan{TransactionID: "transaction-0000000001", PlanDigest: registryDigestEmptyLock(), ExpectedResolvedLockDigest: registryDigestEmptyLock(), CreatesNewDocument: true, NewDocumentID: reservedCommit.DocumentID, DependencySnapshot: reservedCommit.DependencySnapshot, ResolvedLockDelta: registry.ResolvedLockDelta{}, Artifacts: []registry.PlanArtifact{{Release: registry.ArtifactRelease{SourceID: "official"}, Validation: registry.ValidatedArtifact{StagedObjects: []registry.StagedObjectRef{registryRef}}}}, ProjectMutationPlan: mutationPlan}
	committed, err := host.CommitInitialRegistryTemplate(ctx, registry.RuntimeCommitInput{Plan: plan, OperationID: "registry-initial-operation", IdempotencyKey: "registry-initial-key", AuthoringImpact: &initialCommit.AuthoringImpact, HostOperationImpacts: []accessprotocol.HostOperationImpact{}, AccessDecision: decision, MutationPlan: mutationPlan})
	if err != nil || !committed.InitialCommittedRevision || committed.DocumentID != reservedCommit.DocumentID {
		t.Fatalf("committed=%+v err=%v", committed, err)
	}
	bad := port.PrepareRegistryRevisionInput{BaseRevision: opened.Session.Open.CommittedRevision, ProjectMutation: port.RegistryProjectMutation{SnapshotHandle: "missing", SourceClosureDigest: protocolcommon.Digest(state.EngineSnapshot.SourceClosureDigest)}}
	if _, err := host.workbench.PrepareRegistryRevision(ctx, bad); !errors.Is(err, port.ErrConflict) {
		t.Fatalf("stale preparation err=%v", err)
	}
}

func localRegistryPack(t *testing.T) []byte {
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
