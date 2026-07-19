// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

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
}
export interface RegistryArtifactIdentity { readonly kind: RegistryArtifactKind; readonly canonical_id: string; readonly version: string }
export interface RegistryDependency { readonly canonical_id: string; readonly version_range: string; readonly digest_constraint?: string }
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
  readonly size: number;
  readonly dependencies: readonly RegistryDependency[];
  readonly compatibility: readonly RegistryCompatibilityDecision[];
  readonly signature_status: "verified" | "unsigned_allowed" | "missing" | "invalid" | "revoked";
  readonly license: string;
  readonly provenance_digest: string;
}
export interface RegistryPlanArtifact {
  readonly release: RegistryArtifactRelease;
  readonly staged_tree_manifest: string;
  readonly resolved_lock_digest: string;
  readonly diagnostics: readonly string[];
}
export interface RegistryInstallPlan {
  readonly transaction_id: string;
  readonly plan_digest: string;
  readonly action: RegistryAction;
  readonly project_id: string;
  readonly base_revision: string;
  readonly artifacts: readonly RegistryPlanArtifact[];
  readonly required_capabilities: readonly string[];
  readonly trust_policy_digests: readonly string[];
  readonly mutation_digest: string;
  readonly authoring_impact_digests: readonly string[];
  readonly host_operation_impact_digest: string;
  readonly evaluation_digest: string;
}
export interface RegistryTransaction {
  readonly transaction_id: string;
  readonly state: "planned" | "downloading" | "verified" | "expanded_staged" | "compiled" | "awaiting_confirmation" | "applying_project_change" | "committed" | "rolled_back" | "repair_required" | "repairing" | "superseded" | "needs_review";
  readonly plan_digest: string;
  readonly evidence_digest: string;
  readonly committed_revision?: string;
  readonly failure?: RegistryFailure;
}
export interface RegistryFailure { readonly code: string; readonly subject: string; readonly actionable: boolean; readonly correlation_id?: string }
export type RegistryResult<T> = Readonly<{ ok: true; value: T }> | Readonly<{ ok: false; failure: RegistryFailure }>;

export interface RegistrySearchInput { readonly query: string; readonly kind?: RegistryArtifactKind; readonly include_prerelease?: boolean }
export interface RegistryPlanInput {
  readonly action: RegistryAction; readonly project_id: string; readonly base_revision: string;
  readonly expected_definition_hash: string; readonly expected_resolved_lock_digest: string;
  readonly requested: RegistryArtifactIdentity; readonly include_prerelease?: boolean;
}
export interface RegistryCommitInput { readonly transaction_id: string; readonly plan_digest: string; readonly operation_id: string; readonly idempotency_key: string }
export interface RegistryAuthoringInput { readonly kind: RegistryArtifactKind; readonly project_id: string; readonly output_name: string; readonly publisher_id: string; readonly version: string }
export interface RegistryAuthConnectionInput { readonly source_id: string; readonly connection_ref: string }

export interface RegistryClient {
  listSources(signal?: AbortSignal): Promise<RegistryResult<readonly RegistrySource[]>>;
  configureSource(source: Omit<RegistrySource, "connected">, signal?: AbortSignal): Promise<RegistryResult<RegistrySource>>;
  connectSource(input: RegistryAuthConnectionInput, signal?: AbortSignal): Promise<RegistryResult<RegistrySource>>;
  disconnectSource(sourceId: string, signal?: AbortSignal): Promise<RegistryResult<RegistrySource>>;
  search(input: RegistrySearchInput, signal?: AbortSignal): Promise<RegistryResult<readonly RegistryArtifactRelease[]>>;
  plan(input: RegistryPlanInput, signal?: AbortSignal): Promise<RegistryResult<RegistryInstallPlan>>;
  commit(input: RegistryCommitInput, signal?: AbortSignal): Promise<RegistryResult<RegistryTransaction>>;
  getTransaction(transactionId: string, signal?: AbortSignal): Promise<RegistryResult<RegistryTransaction>>;
  authorArtifact(input: RegistryAuthoringInput, signal?: AbortSignal): Promise<RegistryResult<RegistryArtifactRelease>>;
}
