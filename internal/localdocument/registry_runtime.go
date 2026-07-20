// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package localdocument

import (
	"context"
	"errors"
	"strconv"

	"github.com/dencyuinc/layerdraw/gen/go/protocolcommon"
	"github.com/dencyuinc/layerdraw/gen/go/runtimeprotocol"
	"github.com/dencyuinc/layerdraw/internal/engine/endpoint"
	"github.com/dencyuinc/layerdraw/internal/registry"
	runtimehost "github.com/dencyuinc/layerdraw/internal/runtime"
	"github.com/dencyuinc/layerdraw/internal/runtime/port"
)

// CommitRegistryPlan is the production Registry-to-Runtime owner adapter. It
// resolves the complete issued Runtime session inside the trusted host and
// maps only the staged Engine mutation that Registry already bound to its plan.
func (h *Host) CommitRegistryPlan(ctx context.Context, input registry.RuntimeCommitInput) (registry.RuntimeCommitResult, error) {
	session, err := h.registrySession(input.RuntimeSessionID)
	if err != nil {
		return registry.RuntimeCommitResult{}, err
	}
	runtimeInput, err := registryRuntimeInput(input, session.Open.Session, session.Open.CommittedRevision)
	if err != nil {
		return registry.RuntimeCommitResult{}, err
	}
	result, rejection := h.runtime.CommitRegistryPlan(h.accessContext(ctx, session), runtimeInput)
	if rejection != nil {
		return registry.RuntimeCommitResult{}, rejection
	}
	if result.OperationResult.CommittedRevision == nil {
		return registry.RuntimeCommitResult{}, errors.New("Registry publication did not commit a revision")
	}
	if err := h.applyCommit(session, result); err != nil {
		return registry.RuntimeCommitResult{}, &registry.RuntimePublicationError{Published: true, Cause: err}
	}
	if err := h.ApplyRegistryProjectState(session.Open.Session.Scope.DocumentID, dependencySnapshotAfter(input.Plan), input.MutationPlan.StagedTreeManifest); err != nil {
		return registry.RuntimeCommitResult{}, &registry.RuntimePublicationError{Published: true, Cause: err}
	}
	return registryCommitResult(result, false), nil
}

func (h *Host) CommitInitialRegistryTemplate(ctx context.Context, input registry.RuntimeCommitInput) (registry.RuntimeCommitResult, error) {
	mutation, staged, err := registryMutation(input)
	if err != nil {
		return registry.RuntimeCommitResult{}, err
	}
	snapshot := input.MutationPlan.EngineSnapshot
	baseline := runtimeprotocol.CommittedRevisionRef{DocumentID: runtimeprotocol.DocumentID(snapshot.DocumentID), RevisionID: "registry_empty_baseline", DefinitionHash: protocolcommon.Digest(snapshot.DefinitionHash), GraphHash: protocolcommon.Digest(snapshot.GraphHash)}
	result, rejection := h.runtime.CommitInitialRegistryTemplate(ctx, runtimehost.InitialRegistryCommitInput{
		DocumentID: runtimeprotocol.DocumentID(snapshot.DocumentID), OperationID: runtimeprotocol.OperationID(input.OperationID), IdempotencyKey: runtimeprotocol.IdempotencyKey(input.IdempotencyKey),
		BaselineRevision: baseline, RegistryTransactionID: input.Plan.TransactionID, PlanDigest: protocolcommon.Digest(input.Plan.PlanDigest), MutationDigest: protocolcommon.Digest(input.MutationPlan.MutationDigest),
		ExpectedResolvedLockDigest: protocolcommon.Digest(input.Plan.ExpectedResolvedLockDigest), StagedObjects: staged, AuthoringImpact: *input.AuthoringImpact,
		HostOperationImpacts: input.HostOperationImpacts, ExpectedDecision: input.AccessDecision, ProjectMutation: mutation,
	})
	if rejection != nil {
		return registry.RuntimeCommitResult{}, rejection
	}
	if result.OperationResult.CommittedRevision == nil {
		return registry.RuntimeCommitResult{}, errors.New("Registry template publication did not commit a revision")
	}
	source, err := h.registryCommittedSource(ctx, *result.OperationResult.CommittedRevision)
	if err != nil {
		return registry.RuntimeCommitResult{}, &registry.RuntimePublicationError{Published: true, Cause: err}
	}
	binding := documentBinding{DocumentID: result.OperationResult.CommittedRevision.DocumentID, Kind: "registry", Locator: "registry:" + input.Plan.TransactionID, PortableID: source.PortableID, SourceDigest: source.Digest()}
	h.mu.Lock()
	h.metadata.Bindings[bindingKey(binding.Kind, binding.Locator)] = binding
	h.metadata.RegistryProjects[string(binding.DocumentID)] = registryProjectMetadata{DependencySnapshot: dependencySnapshotAfter(input.Plan), PackTreeManifest: input.MutationPlan.StagedTreeManifest}
	err = h.saveMetadataLocked()
	h.mu.Unlock()
	if err != nil {
		return registry.RuntimeCommitResult{}, &registry.RuntimePublicationError{Published: true, Cause: err}
	}
	return registryCommitResult(result, true), nil
}

func (h *Host) LookupRegistryCommit(ctx context.Context, documentID, operationID, idempotencyKey string) (registry.RuntimeRegistryOutcome, error) {
	scope, err := h.authority.ResolveScope(ctx, runtimeprotocol.DocumentID(documentID))
	if err != nil {
		return registry.RuntimeRegistryOutcome{Status: registry.RuntimeRegistryUnknown}, err
	}
	op := runtimeprotocol.OperationID(operationID)
	record, err := h.recovery.Get(ctx, port.GetRecoveryRecordInput{Scope: scope, OperationID: &op})
	if err != nil && idempotencyKey != "" {
		key := runtimeprotocol.IdempotencyKey(idempotencyKey)
		record, err = h.recovery.Get(ctx, port.GetRecoveryRecordInput{Scope: scope, IdempotencyKey: &key})
	}
	if err != nil || record.CommitResult == nil || record.CommitResult.OperationResult.CommittedRevision == nil {
		return registry.RuntimeRegistryOutcome{Status: registry.RuntimeRegistryUnknown}, err
	}
	return registry.RuntimeRegistryOutcome{Status: registry.RuntimeRegistryCommitted, Result: registryCommitResult(*record.CommitResult, false)}, nil
}

func (h *Host) registrySession(id string) (*Session, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	session := h.sessions[runtimeprotocol.RuntimeSessionID(id)]
	if session == nil || session.closed {
		return nil, errors.New("Registry Runtime session is closed or unknown")
	}
	return session, nil
}

func registryRuntimeInput(input registry.RuntimeCommitInput, session runtimeprotocol.RuntimeSessionRef, base runtimeprotocol.CommittedRevisionRef) (runtimehost.RegistryCommitInput, error) {
	mutation, staged, err := registryMutation(input)
	if err != nil {
		return runtimehost.RegistryCommitInput{}, err
	}
	var lease *runtimeprotocol.LeaseToken
	if input.LeaseToken != "" {
		value := runtimeprotocol.LeaseToken(input.LeaseToken)
		lease = &value
	}
	return runtimehost.RegistryCommitInput{Session: session, OperationID: runtimeprotocol.OperationID(input.OperationID), IdempotencyKey: runtimeprotocol.IdempotencyKey(input.IdempotencyKey), BaseRevision: base,
		RegistryTransactionID: input.Plan.TransactionID, PlanDigest: protocolcommon.Digest(input.Plan.PlanDigest), MutationDigest: protocolcommon.Digest(input.MutationPlan.MutationDigest), ExpectedResolvedLockDigest: protocolcommon.Digest(input.Plan.ExpectedResolvedLockDigest),
		StagedObjects: staged, AuthoringImpact: *input.AuthoringImpact, HostOperationImpacts: input.HostOperationImpacts, ExpectedDecision: input.AccessDecision, ProjectMutation: mutation, LeaseToken: lease}, nil
}

func registryMutation(input registry.RuntimeCommitInput) (port.RegistryProjectMutation, []port.RegistryStagedObjectRef, error) {
	if input.AuthoringImpact == nil || input.MutationPlan.EngineSnapshot.Handle == "" || input.MutationPlan.EngineSnapshot.GraphHash == "" {
		return port.RegistryProjectMutation{}, nil, errors.New("Registry Engine mutation binding is incomplete")
	}
	staged := make([]port.RegistryStagedObjectRef, len(input.MutationPlan.StagedObjects))
	byID := make(map[string]port.RegistryStagedObjectRef, len(staged))
	for index, ref := range input.MutationPlan.StagedObjects {
		if ref.Size < 0 {
			return port.RegistryProjectMutation{}, nil, errors.New("Registry staged object size is invalid")
		}
		mapped := port.RegistryStagedObjectRef{ObjectID: ref.ObjectID, Digest: protocolcommon.Digest(ref.Digest), Size: protocolcommon.CanonicalUint64(strconv.FormatInt(ref.Size, 10)), MediaType: ref.MediaType}
		staged[index], byID[ref.ObjectID] = mapped, mapped
	}
	artifacts := []port.RegistryProjectArtifactRef{}
	for _, planned := range input.Plan.Artifacts {
		for _, ref := range planned.Validation.StagedObjects {
			mapped, ok := byID[ref.ObjectID]
			if !ok {
				return port.RegistryProjectMutation{}, nil, errors.New("Registry artifact is not bound to the mutation closure")
			}
			artifacts = append(artifacts, port.RegistryProjectArtifactRef{Object: mapped, RegistrySource: planned.Release.SourceID})
		}
	}
	removed := make([]string, len(input.Plan.ResolvedLockDelta.Removed))
	for index, item := range input.Plan.ResolvedLockDelta.Removed {
		removed[index] = item.Identity.CanonicalID
	}
	return port.RegistryProjectMutation{SnapshotHandle: input.MutationPlan.EngineSnapshot.Handle, SourceClosureDigest: protocolcommon.Digest(input.MutationPlan.EngineSnapshot.SourceClosureDigest), Artifacts: artifacts, RemoveCanonicalIDs: removed}, staged, nil
}

func registryCommitResult(result runtimeprotocol.RuntimeCommitResult, initial bool) registry.RuntimeCommitResult {
	revision := result.OperationResult.CommittedRevision
	if revision == nil {
		return registry.RuntimeCommitResult{}
	}
	return registry.RuntimeCommitResult{CommittedRevision: string(revision.RevisionID), OperationResultID: string(result.OperationResult.OperationID), DocumentID: string(revision.DocumentID), InitialCommittedRevision: initial}
}

func dependencySnapshotAfter(plan registry.InstallPlan) registry.ProjectDependencySnapshot {
	items := map[string]registry.LockedArtifact{}
	for _, item := range plan.DependencySnapshot.Installs {
		items[item.Identity.CanonicalID] = item
	}
	for _, item := range plan.ResolvedLockDelta.Removed {
		delete(items, item.Identity.CanonicalID)
	}
	for _, group := range [][]registry.LockedArtifact{plan.ResolvedLockDelta.Added, plan.ResolvedLockDelta.Updated, plan.ResolvedLockDelta.Pinned} {
		for _, item := range group {
			items[item.Identity.CanonicalID] = item
		}
	}
	result := registry.ProjectDependencySnapshot{ResolvedLockDigest: plan.ProjectMutationPlan.StagedTreeManifest, Installs: make([]registry.LockedArtifact, 0, len(items))}
	for _, item := range items {
		result.Installs = append(result.Installs, item)
	}
	return result
}

func (h *Host) registryCommittedSource(ctx context.Context, revision runtimeprotocol.CommittedRevisionRef) (endpoint.LocalSource, error) {
	scope := h.authority.add(revision.DocumentID)
	snapshot, err := h.documents.ReadRevision(ctx, port.ReadRevisionInput{Scope: scope, RevisionID: revision.RevisionID})
	if err != nil {
		return endpoint.LocalSource{}, err
	}
	blobs, err := h.documents.ReadSourceBlobs(ctx, port.ReadSourceBlobsInput{Scope: scope, Revision: snapshot.Revision, Blobs: snapshot.SourceBlobs})
	if err != nil {
		return endpoint.LocalSource{}, err
	}
	for _, blob := range blobs.Blobs {
		if blob.Ref == snapshot.Manifest {
			return h.engine.ReadEncodedInput(ctx, blob.Contents)
		}
	}
	return endpoint.LocalSource{}, port.ErrNotFound
}

var _ registry.RuntimePort = (*Host)(nil)
var _ registry.TemplateInitialPublicationPort = (*Host)(nil)
var _ registry.RuntimeRecoveryPort = (*Host)(nil)
