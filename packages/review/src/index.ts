// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { AuthoringDecision, ActorRef } from "@layerdraw/protocol/access";
import type { SourceDiff } from "@layerdraw/protocol/engine";
import type { CommittedRevisionRef } from "@layerdraw/protocol/runtime";
import type { AuthoringCapability, AuthoringImpact, Diagnostic, SemanticDiff, StableAddress, SourceRange } from "@layerdraw/protocol/semantic";

export type ReviewStatus = "proposed" | "stale" | "conflicting" | "superseded" | "denied" | "withdrawn" | "approved" | "applied" | "needs_review";
export type ReviewTarget =
  | Readonly<{ kind: "project" }>
  | Readonly<{ kind: "view" | "entity" | "relation"; stable_address: StableAddress }>
  | Readonly<{ kind: "source_scope"; source_range: SourceRange }>
  | Readonly<{ kind: "diff_entry"; diff_key: string }>;

export interface ReviewComment {
  readonly id: string; readonly author: ActorRef; readonly body: string; readonly target: ReviewTarget;
  readonly created_at: string; readonly updated_at: string; readonly stale: boolean; readonly base_revision: string;
}
export interface ArtifactPreview { readonly kind: string; readonly label: string; readonly digest: string; readonly media_type: string }
export interface ReviewEvidence {
  readonly semantic_diff: SemanticDiff; readonly source_diff: SourceDiff; readonly authoring_impact: AuthoringImpact;
  readonly diagnostics: readonly Diagnostic[]; readonly affected_usages: readonly StableAddress[];
  readonly affected_rows: readonly StableAddress[]; readonly affected_views: readonly StableAddress[];
  readonly definition_preview?: ArtifactPreview; readonly render_previews: readonly ArtifactPreview[];
}
export interface ReviewProposal {
  readonly id: string; readonly generation: number; readonly status: ReviewStatus;
  readonly current_revision: CommittedRevisionRef; readonly proposed_definition_hash: string; readonly proposed_graph_hash: string;
  readonly evidence: ReviewEvidence; readonly proposer: ActorRef; readonly agent_delegation_digest?: string;
  readonly access_evaluation_digest: string; readonly access_decision_digest: string;
  readonly required_capabilities: readonly AuthoringCapability[]; readonly comments: readonly ReviewComment[];
  readonly created_at: string; readonly updated_at: string; readonly approved_by?: ActorRef;
  readonly committed_revision?: CommittedRevisionRef; readonly last_failure?: string;
}
export interface ReviewSnapshot { readonly version: 1; readonly proposals: readonly ReviewProposal[] }
export interface ReviewCommentRequest { readonly proposal_id: string; readonly generation: number; readonly comment_id: string; readonly body: string; readonly target: ReviewTarget }
export interface ReviewApprovalRequest { readonly proposal_id: string; readonly generation: number }
export interface ReviewClient {
  snapshot(options: Readonly<{signal: AbortSignal}>): Promise<ReviewSnapshot>;
  comment(input: ReviewCommentRequest, options: Readonly<{signal: AbortSignal}>): Promise<ReviewProposal>;
  approveAndApply(input: ReviewApprovalRequest, options: Readonly<{signal: AbortSignal}>): Promise<ReviewProposal>;
  withdraw(input: Readonly<{proposal_id: string; generation: number}>, options: Readonly<{signal: AbortSignal}>): Promise<ReviewProposal>;
}

export type ReviewFailureCode = "review.cancelled" | "review.conflict" | "review.denied" | "review.stale" | "review.transport_failed" | "review.invalid_state";
export class ReviewError extends Error {
  constructor(readonly code: ReviewFailureCode, message: string) { super(message); this.name = "ReviewError"; }
}
export interface ReviewModelState {
  readonly phase: "idle" | "loading" | "ready" | "mutating" | "failed" | "closed";
  readonly snapshot: ReviewSnapshot; readonly selected_id?: string | undefined; readonly selected?: ReviewProposal | undefined;
  readonly failure?: ReviewError | undefined; readonly decision?: AuthoringDecision | undefined;
}
export interface ReviewModel {
  snapshot(): ReviewModelState; subscribe(listener: (state: ReviewModelState) => void): () => void;
  refresh(): Promise<ReviewSnapshot>; select(proposalID?: string): ReviewModelState;
  comment(input: Omit<ReviewCommentRequest, "proposal_id" | "generation">): Promise<ReviewProposal>;
  approveAndApply(): Promise<ReviewProposal>; withdraw(): Promise<ReviewProposal>; close(): Promise<void>;
}

const empty: ReviewSnapshot = Object.freeze({version: 1, proposals: Object.freeze([])});
function mapError(error: unknown): ReviewError {
  if (error instanceof ReviewError) return error;
  if (typeof error === "object" && error !== null && "name" in error && error.name === "AbortError") return new ReviewError("review.cancelled", "The Review operation was cancelled.");
  return new ReviewError("review.transport_failed", "The trusted Review host operation failed.");
}
function proposal(snapshot: ReviewSnapshot, id?: string): ReviewProposal | undefined { return id === undefined ? undefined : snapshot.proposals.find((item) => item.id === id); }

export function createReviewModel(client: ReviewClient): ReviewModel {
  if (client === undefined || typeof client.snapshot !== "function") throw new ReviewError("review.invalid_state", "A trusted Review client is required.");
  let state: ReviewModelState = Object.freeze({phase: "idle", snapshot: empty});
  let controller: AbortController | undefined;
  let sequence = 0;
  const listeners = new Set<(value: ReviewModelState) => void>();
  const publish = (next: ReviewModelState) => { state = Object.freeze(next); for (const listener of listeners) listener(state); };
  const ensureSelected = () => {
    if (state.phase === "closed" || state.selected === undefined) throw new ReviewError("review.invalid_state", "Select a live proposal first.");
    if (["applied", "withdrawn", "superseded"].includes(state.selected.status)) throw new ReviewError("review.invalid_state", "The selected proposal is terminal.");
    return state.selected;
  };
  const mutate = async (operation: (selected: ReviewProposal, signal: AbortSignal) => Promise<ReviewProposal>) => {
    const selected = ensureSelected(); controller?.abort(); const owned = new AbortController(); controller = owned; const current = ++sequence;
    publish({...state, phase: "mutating", failure: undefined});
    try {
      const result = await operation(selected, owned.signal);
      if (current !== sequence || state.phase === "closed") throw new ReviewError("review.cancelled", "The Review operation was superseded.");
      const proposals = state.snapshot.proposals.map((item) => item.id === result.id ? result : item);
      const snapshot = Object.freeze({version: 1 as const, proposals: Object.freeze(proposals)});
      publish({phase: "ready", snapshot, selected_id: result.id, selected: result}); return result;
    } catch (error) {
      const failure = mapError(error); if (current === sequence && state.phase !== "closed") publish({...state, phase: "failed", failure}); throw failure;
    } finally { if (controller === owned) controller = undefined; }
  };
  const model: ReviewModel = Object.freeze({
    snapshot: () => state,
    subscribe(listener: (state: ReviewModelState) => void) { if (state.phase === "closed") return () => {}; listeners.add(listener); listener(state); return () => listeners.delete(listener); },
    async refresh() {
      if (state.phase === "closed") throw new ReviewError("review.invalid_state", "The Review model is closed.");
      controller?.abort(); const owned = new AbortController(); controller = owned; const current = ++sequence; publish({...state, phase: "loading", failure: undefined});
      try { const snapshot = await client.snapshot({signal: owned.signal}); if (current !== sequence || (state as ReviewModelState).phase === "closed") throw new ReviewError("review.cancelled", "The Review refresh was superseded."); const selected = proposal(snapshot, state.selected_id); publish({phase: "ready", snapshot, ...(selected === undefined ? {} : {selected_id: selected.id, selected})}); return snapshot; }
      catch (error) { const failure = mapError(error); if (current === sequence && (state as ReviewModelState).phase !== "closed") publish({...state, phase: "failed", failure}); throw failure; }
      finally { if (controller === owned) controller = undefined; }
    },
    select(id?: string) { if (state.phase === "closed") throw new ReviewError("review.invalid_state", "The Review model is closed."); const selected = proposal(state.snapshot, id); publish({...state, ...(selected === undefined ? {selected_id: undefined, selected: undefined} : {selected_id: selected.id, selected})}); return state; },
    comment(input: Omit<ReviewCommentRequest, "proposal_id" | "generation">) { return mutate((selected, signal) => client.comment({...input, proposal_id: selected.id, generation: selected.generation}, {signal})); },
    approveAndApply() { return mutate((selected, signal) => client.approveAndApply({proposal_id: selected.id, generation: selected.generation}, {signal})); },
    withdraw() { return mutate((selected, signal) => client.withdraw({proposal_id: selected.id, generation: selected.generation}, {signal})); },
    async close() { if (state.phase === "closed") return; sequence++; controller?.abort(); controller = undefined; listeners.clear(); publish({phase: "closed", snapshot: empty}); },
  });
  return model;
}
