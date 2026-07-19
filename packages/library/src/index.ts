// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type {
  RegistryAction, RegistryArtifactIdentity, RegistryArtifactKind, RegistryArtifactRelease,
  RegistryAuthoringInput, RegistryClient, RegistryFailure, RegistryInstallPlan,
  RegistryPlanInput, RegistrySource, RegistryTransaction,
} from "@layerdraw/registry-client";

export interface LibraryCapabilities {
  readonly browse: boolean;
  readonly manage_sources: boolean;
  readonly author_artifacts: boolean;
  readonly plan_transactions: boolean;
  readonly commit_transactions: boolean;
  readonly unavailable_reason?: string;
}
export interface LibraryProjectContext {
  readonly project_id: string;
  readonly revision: string;
  readonly definition_hash: string;
  readonly resolved_lock_digest: string;
}
export type LibraryStatus = "idle" | "loading" | "ready" | "previewing" | "awaiting_confirmation" | "applying" | "committed" | "recoverable_error" | "disabled";
export interface LibrarySnapshot {
  readonly status: LibraryStatus;
  readonly query: string;
  readonly kind?: RegistryArtifactKind | undefined;
  readonly sources: readonly RegistrySource[];
  readonly results: readonly RegistryArtifactRelease[];
  readonly selected?: RegistryArtifactRelease | undefined;
  readonly plan?: RegistryInstallPlan | undefined;
  readonly transaction?: RegistryTransaction | undefined;
  readonly failure?: RegistryFailure | undefined;
  readonly capabilities: LibraryCapabilities;
}
export type LibraryEvent = Readonly<{ kind: "changed"; snapshot: LibrarySnapshot }>;
export interface LibraryOptions { readonly client: RegistryClient; readonly capabilities: LibraryCapabilities; readonly onEvent?: (event: LibraryEvent) => void }

const unavailable = (subject: string): RegistryFailure => ({ code: "registry.policy_denied", subject, actionable: true });

export class LibraryController {
  readonly #client: RegistryClient;
  readonly #onEvent: ((event: LibraryEvent) => void) | undefined;
  #state: LibrarySnapshot;
  #operation: AbortController | undefined;

  constructor(options: LibraryOptions) {
    if (!options.client) throw new TypeError("RegistryClient is required");
    this.#client = options.client;
    this.#onEvent = options.onEvent;
    const status = options.capabilities.browse ? "idle" : "disabled";
    this.#state = Object.freeze({ status, query: "", sources: [], results: [], capabilities: structuredClone(options.capabilities) });
  }
  snapshot(): LibrarySnapshot { return structuredClone(this.#state); }
  cancel(): void { this.#operation?.abort(); this.#operation = undefined; }

  async refreshSources(): Promise<LibrarySnapshot> {
    if (!this.#state.capabilities.browse) return this.#deny("browse");
    const signal = this.#begin("loading");
    const result = await this.#client.listSources(signal);
    if (signal.aborted) return this.snapshot();
    if (!result.ok) return this.#fail(result.failure);
    return this.#publish({ ...this.#state, status: "ready", sources: result.value });
  }
  async search(query: string, kind?: RegistryArtifactKind): Promise<LibrarySnapshot> {
    if (!this.#state.capabilities.browse) return this.#deny("browse");
    const signal = this.#begin("loading", { query, ...(kind === undefined ? {} : { kind }) });
    const result = await this.#client.search({ query, ...(kind === undefined ? {} : { kind }) }, signal);
    if (signal.aborted) return this.snapshot();
    if (!result.ok) return this.#fail(result.failure);
    return this.#publish({ ...this.#state, status: "ready", query, ...(kind === undefined ? {} : { kind }), results: result.value });
  }
  select(identity: RegistryArtifactIdentity): LibrarySnapshot {
    const selected = this.#state.results.find((item) => item.identity.kind === identity.kind && item.identity.canonical_id === identity.canonical_id && item.identity.version === identity.version);
    if (!selected) return this.#fail({ code: "registry.unavailable", subject: identity.canonical_id, actionable: true });
    return this.#publish({ ...this.#state, selected, plan: undefined, transaction: undefined, failure: undefined, status: "ready" });
  }
  async preview(action: RegistryAction, project: LibraryProjectContext): Promise<LibrarySnapshot> {
    if (!this.#state.capabilities.plan_transactions) return this.#deny("plan_transactions");
    if (!this.#state.selected) return this.#fail({ code: "registry.unavailable", subject: "selection", actionable: true });
    const input: RegistryPlanInput = {
      action, project_id: project.project_id, base_revision: project.revision,
      expected_definition_hash: project.definition_hash,
      expected_resolved_lock_digest: project.resolved_lock_digest,
      requested: this.#state.selected.identity,
    };
    const signal = this.#begin("previewing");
    const result = await this.#client.plan(input, signal);
    if (signal.aborted) return this.snapshot();
    if (!result.ok) return this.#fail(result.failure);
    return this.#publish({ ...this.#state, status: "awaiting_confirmation", plan: result.value, transaction: undefined, failure: undefined });
  }
  async confirm(operationId: string, idempotencyKey: string): Promise<LibrarySnapshot> {
    if (!this.#state.capabilities.commit_transactions) return this.#deny("commit_transactions");
    const plan = this.#state.plan;
    if (!plan || this.#state.status !== "awaiting_confirmation") return this.#fail({ code: "registry.plan_stale", subject: "confirmation", actionable: true });
    const signal = this.#begin("applying");
    const result = await this.#client.commit({ transaction_id: plan.transaction_id, plan_digest: plan.plan_digest, operation_id: operationId, idempotency_key: idempotencyKey }, signal);
    if (signal.aborted) return this.snapshot();
    if (!result.ok) return this.#fail(result.failure);
    const status = result.value.state === "committed" ? "committed" : result.value.state === "repair_required" || result.value.state === "needs_review" ? "recoverable_error" : "applying";
    return this.#publish({ ...this.#state, status, transaction: result.value, failure: result.value.failure });
  }
  async author(input: RegistryAuthoringInput): Promise<LibrarySnapshot> {
    if (!this.#state.capabilities.author_artifacts) return this.#deny("author_artifacts");
    const signal = this.#begin("loading");
    const result = await this.#client.authorArtifact(input, signal);
    if (signal.aborted) return this.snapshot();
    if (!result.ok) return this.#fail(result.failure);
    const results = [result.value, ...this.#state.results.filter((item) => item.digest !== result.value.digest)];
    return this.#publish({ ...this.#state, status: "ready", results, selected: result.value });
  }
  #begin(status: LibraryStatus, extension: Partial<LibrarySnapshot> = {}): AbortSignal {
    this.cancel(); this.#operation = new AbortController(); this.#publish({ ...this.#state, ...extension, status, failure: undefined }); return this.#operation.signal;
  }
  #deny(subject: string): LibrarySnapshot { return this.#fail(unavailable(subject)); }
  #fail(failure: RegistryFailure): LibrarySnapshot { return this.#publish({ ...this.#state, status: "recoverable_error", failure }); }
  #publish(state: LibrarySnapshot): LibrarySnapshot { this.#state = Object.freeze(structuredClone(state)); const snapshot = this.snapshot(); this.#onEvent?.({ kind: "changed", snapshot }); return snapshot; }
}

export function createLibrary(options: LibraryOptions): LibraryController { return new LibraryController(options); }
