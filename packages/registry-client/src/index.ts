// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { AuthoringDecision, HostOperationImpact } from "@layerdraw/protocol/access";
import type { AuthoringImpact } from "@layerdraw/protocol/semantic";

export type RegistrySourceKind =
  | "official"
  | "organization_private"
  | "self_hosted"
  | "local_directory"
  | "git";
export type RegistryArtifactKind = "pack" | "template";
export type RegistryAction = "install" | "update" | "pin" | "remove" | "repair" | "create_from_template";

export interface RegistrySource {
  readonly source_id: string;
  readonly kind: RegistrySourceKind;
  readonly endpoint_ref: string;
  readonly trust_policy_id: string;
  readonly auth_connection_ref?: string;
  readonly cache_policy: string;
  readonly priority: number;
  readonly connected: boolean;
  readonly revision: number;
}
export interface RegistryArtifactIdentity { readonly kind: RegistryArtifactKind; readonly canonical_id: string; readonly version: string }
export interface RegistryDependency { readonly kind: RegistryArtifactKind; readonly canonical_id: string; readonly version_range: string; readonly digest_constraint?: string }
export interface RegistryCompatibilityDecision {
  readonly subject: string; readonly required: string; readonly available: string;
  readonly status: "compatible" | "degraded" | "disabled" | "incompatible" | "migration_required";
  readonly diagnostics: readonly string[];
}
export interface RegistryArtifactRelease {
  readonly identity: RegistryArtifactIdentity;
  readonly source_id: string;
  readonly publisher_id: string;
  readonly digest: string;
  readonly manifest_digest: string;
  readonly dependency_metadata_digest: string;
  readonly size: number;
  readonly dependencies: readonly RegistryDependency[];
  readonly compatibility: readonly RegistryCompatibilityDecision[];
  readonly trust?: Readonly<{ status: "verified" | "unsigned_allowed"; policy_digest: string; evidence_digest: string }>;
  readonly license: string;
  readonly provenance_digest: string;
}
export interface RegistryPlanArtifact {
  readonly release: RegistryArtifactRelease;
  readonly validation: Readonly<{
    identity: RegistryArtifactIdentity; canonical_digest: string; staged_tree_manifest: string;
    resolved_lock_digest: string; mutation_digest: string; authoring_impact_digest: string;
    address_migration_plan_digest: string; diagnostics: readonly string[];
    authoring_impact?: Readonly<Record<string, unknown>>;
  }>;
}
export interface RegistryLockedArtifact { readonly identity: RegistryArtifactIdentity; readonly source_id: string; readonly publisher_id: string; readonly digest: string; readonly provenance_digest: string; readonly dependency_metadata_digest: string; readonly dependencies: readonly RegistryArtifactIdentity[]; readonly pinned: boolean }
export interface RegistryDependencySnapshot { readonly resolved_lock_digest: string; readonly installs: readonly RegistryLockedArtifact[] }
export interface RegistryLockDelta { readonly added: readonly RegistryLockedArtifact[]; readonly updated: readonly RegistryLockedArtifact[]; readonly removed: readonly RegistryLockedArtifact[]; readonly pinned: readonly RegistryLockedArtifact[] }
export interface RegistrySourceEdit {readonly path:string;readonly before_digest?:string;readonly after_digest:string}
export interface RegistryStateMigrationProposal {readonly proposal_digest:string;readonly affected_subjects:readonly string[]}
export interface RegistryProjectMutationPlan {readonly registry_transaction_id:string;readonly plan_digest:string;readonly base_project_revision?:string;readonly expected_definition_hash?:string;readonly expected_resolved_lock_digest?:string;readonly staged_tree_manifest:string;readonly resolved_lock_delta:RegistryLockDelta;readonly source_edits:readonly RegistrySourceEdit[];readonly address_migration_plan_digest?:string;readonly state_migration_proposal?:RegistryStateMigrationProposal;readonly trust_policy_digest:string;readonly mutation_digest:string;readonly authoring_impact:AuthoringImpact;readonly authoring_impact_digest:string;readonly host_operation_impact_digest:string;readonly evaluation_digest:string}
export interface RegistryInstallPlan {
  readonly transaction_id: string;
  readonly plan_digest: string;
  readonly action: RegistryAction;
  readonly project_id: string;
  readonly base_revision: string;
  readonly expected_definition_hash:string;
  readonly expected_resolved_lock_digest:string;
  readonly artifacts: readonly RegistryPlanArtifact[];
  readonly required_capabilities: readonly string[];
  readonly trust_policy_digests: readonly string[];
  readonly source_bindings: readonly Readonly<{source_id:string;source_digest:string;trust_policy_digest:string}>[];
  readonly dependency_snapshot: RegistryDependencySnapshot;
  readonly resolved_lock_delta: RegistryLockDelta;
  readonly rollback_checkpoint: Readonly<{base_project_revision:string;base_definition_hash:string;base_resolved_lock_digest:string;current_pack_tree_manifest:string}>;
  readonly expires_at: string;
  readonly migration_required: boolean;
  readonly creates_new_document: boolean;
  readonly mutation_digest: string;
  readonly authoring_impact_digests: readonly string[];
  readonly host_operation_impact_digest: string;
  readonly evaluation_digest: string;
  readonly authoring_impact: AuthoringImpact;
  readonly host_operation_impacts: readonly HostOperationImpact[];
  readonly access_decision: AuthoringDecision;
  readonly host_capabilities_digest: string;
  readonly project_mutation_plan:RegistryProjectMutationPlan;
  readonly new_document_id?:string;
  readonly runtime_session_id:string;
  readonly lease_token?:string;
}
export type RegistryTransactionState="planned"|"downloading"|"verified"|"expanded_staged"|"compiled"|"awaiting_confirmation"|"applying_project_change"|"committed"|"rolled_back"|"repair_required"|"repairing"|"superseded"|"needs_review";
export interface RegistryTransactionEvent { readonly state: RegistryTransactionState; readonly evidence_digest: string; readonly sequence: number; readonly idempotency_key?: string }
export interface RegistryTransaction {
  readonly plan: RegistryInstallPlan;
  readonly events: readonly RegistryTransactionEvent[];
  readonly planning_request?: RegistryPlanInput;
  readonly committed_revision?: string;
  readonly operation_result_id?: string;
  readonly runtime_input?:Readonly<Record<string,unknown>>;
  readonly superseding_revision?:string;
  readonly runtime_result?:RegistryCommitResult;
}
export interface RegistryFailure { readonly code: string; readonly subject: string; readonly actionable: boolean; readonly correlation_id?: string }
export type RegistryResult<T> = Readonly<{ ok: true; value: T }> | Readonly<{ ok: false; failure: RegistryFailure }>;

export interface RegistrySearchInput { readonly query: string; readonly kind?: RegistryArtifactKind; readonly include_prerelease?: boolean }
export interface RegistryPlanInput {
  readonly action: RegistryAction; readonly project_id: string; readonly base_revision: string;
  readonly expected_definition_hash: string; readonly expected_resolved_lock_digest: string;
  readonly requested: RegistryArtifactIdentity; readonly include_prerelease?: boolean;
  readonly dependency_snapshot: RegistryDependencySnapshot;
  readonly requested_pin?: boolean;
}
export interface RegistryCommitInput { readonly transaction_id: string; readonly plan_digest: string; readonly operation_id: string; readonly idempotency_key: string }
export interface RegistryCommitResult { readonly committed_revision:string;readonly operation_result_id:string;readonly document_id:string;readonly initial_committed_revision:boolean }
export interface RegistryAuthoringInput { readonly kind: RegistryArtifactKind; readonly project_id: string; readonly output_name: string; readonly publisher_id: string; readonly version: string }
export interface RegistryAuthConnectionInput { readonly source_id: string; readonly connection_ref: string }

export interface RegistryClient {
  listSources(signal?: AbortSignal): Promise<RegistryResult<readonly RegistrySource[]>>;
  configureSource(source: Omit<RegistrySource, "connected" | "auth_connection_ref" | "revision">, signal?: AbortSignal): Promise<RegistryResult<RegistrySource>>;
  connectSource(input: RegistryAuthConnectionInput, signal?: AbortSignal): Promise<RegistryResult<RegistrySource>>;
  disconnectSource(sourceId: string, signal?: AbortSignal): Promise<RegistryResult<RegistrySource>>;
  search(input: RegistrySearchInput, signal?: AbortSignal): Promise<RegistryResult<readonly RegistryArtifactRelease[]>>;
  plan(input: RegistryPlanInput, signal?: AbortSignal): Promise<RegistryResult<RegistryInstallPlan>>;
  commit(input: RegistryCommitInput, signal?: AbortSignal): Promise<RegistryResult<RegistryCommitResult>>;
  getTransaction(transactionId: string, signal?: AbortSignal): Promise<RegistryResult<RegistryTransaction>>;
  recoverTransaction(transactionId: string, signal?: AbortSignal): Promise<RegistryResult<RegistryTransaction>>;
  authorArtifact(input: RegistryAuthoringInput, signal?: AbortSignal): Promise<RegistryResult<RegistryArtifactRelease>>;
}
