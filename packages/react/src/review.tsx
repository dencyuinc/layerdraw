// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {useEffect, useState, useSyncExternalStore} from "react";
import type {ChangeEvent} from "react";
import type {ReviewModel, ReviewModelState, ReviewProposal, ReviewTarget} from "@layerdraw/review";

export interface ReviewPanelProps { readonly model: ReviewModel; readonly className?: string }

function targetLabel(target: ReviewTarget): string {
  if (target.kind === "project") return "Project";
  if (target.kind === "diff_entry") return `Diff ${target.diff_key}`;
  if (target.kind === "source_scope") return "Source scope";
  return `${target.kind} ${target.stable_address}`;
}

function evidence(proposal: ReviewProposal) {
  const value = proposal.evidence;
  return (
    <div className="ld-review-evidence">
      <section aria-labelledby="review-definition">
        <h3 id="review-definition">Definition and source</h3>
        <p>{`Definition ${proposal.proposed_definition_hash}`}</p>
        {value.definition_preview === undefined ? null : <p>{`${value.definition_preview.label} (${value.definition_preview.media_type}) ${value.definition_preview.digest}`}</p>}
        <ul>{value.source_diff.edits.map((item, index) => <li key={`source-${index}`}>{`${item.kind}${item.source_range === undefined ? "" : " at source range"}`}</li>)}</ul>
      </section>
      <section aria-labelledby="review-impact">
        <h3 id="review-impact">Semantic impact</h3>
        <ul>{value.semantic_diff.entries.map((item, index) => <li key={`semantic-${index}`}>{`${item.kind}: ${item.subject_kind}`}</li>)}</ul>
        <p>{`Required capabilities: ${proposal.required_capabilities.join(", ") || "none"}`}</p>
        <p>Affected usages</p>
        <ul>{value.affected_usages.map((item) => <li key={item}>{item}</li>)}</ul>
        <p>Affected rows</p>
        <ul>{value.affected_rows.map((item) => <li key={item}>{item}</li>)}</ul>
        <p>Affected views</p>
        <ul>{value.affected_views.map((item) => <li key={item}>{item}</li>)}</ul>
      </section>
      <section aria-labelledby="review-diagnostics">
        <h3 id="review-diagnostics">Diagnostics and previews</h3>
        <ul>{value.diagnostics.map((item, index) => <li key={`${item.code}-${index}`}>{`${item.code}: ${item.message_key}`}</li>)}</ul>
        <ul>{value.render_previews.map((item) => <li key={item.digest}>{`${item.label} (${item.media_type})`}</li>)}</ul>
      </section>
    </div>
  );
}

export function ReviewPanel({model, className}: ReviewPanelProps) {
  const state = useSyncExternalStore(model.subscribe, model.snapshot, model.snapshot);
  const [comment, setComment] = useState("");
  useEffect(() => { if (state.phase === "idle") void model.refresh().catch(() => undefined); }, [model, state.phase]);
  const selected = state.selected;
  const busy = state.phase === "loading" || state.phase === "mutating";
  const actionable = selected !== undefined && !["applied", "withdrawn", "superseded"].includes(selected.status) && !busy;
  return (
    <section className={["ld-review-panel", className].filter(Boolean).join(" ")} aria-label="Review proposals">
      <header>
        <h2>Review</h2>
        <button type="button" disabled={busy} onClick={() => { void model.refresh().catch(() => undefined); }}>Refresh</button>
      </header>
      <div className="ld-review-layout">
        <nav aria-label="Proposals">
          <ul>
            {state.snapshot.proposals.map((item) => (
              <li key={item.id}>
                <button type="button" aria-current={selected?.id === item.id ? "true" : undefined} onClick={() => model.select(item.id)}>{`${item.id} — ${item.status}`}</button>
              </li>
            ))}
          </ul>
        </nav>
        {selected === undefined ? <p>Select a proposal to inspect it.</p> : (
          <article aria-label={`Proposal ${selected.id}`}>
            <h2>{selected.id}</h2>
            <p role="status">{`Status: ${selected.status}`}</p>
            <p>{`Proposed by ${selected.proposer.kind} ${selected.proposer.actor_id}`}</p>
            <p>{`Access evaluation ${selected.access_evaluation_digest}`}</p>
            {evidence(selected)}
            <section aria-labelledby="review-comments">
              <h3 id="review-comments">Comments</h3>
              <ul>{selected.comments.map((item) => <li key={item.id}>{`${targetLabel(item.target)}: ${item.body}${item.stale ? " (stale)" : ""}`}</li>)}</ul>
              <label>
                Project comment
                <textarea value={comment} onChange={(event: ChangeEvent<HTMLTextAreaElement>) => setComment(event.currentTarget.value)} />
              </label>
              <button
                type="button"
                disabled={!actionable || comment.trim() === ""}
                onClick={() => { const body = comment.trim(); void model.comment({comment_id: globalThis.crypto.randomUUID(), body, target: {kind: "project"}}).then(() => setComment("")).catch(() => undefined); }}
              >
                Add comment
              </button>
            </section>
            <div className="ld-review-actions">
              <button type="button" disabled={!actionable} onClick={() => { void model.approveAndApply().catch(() => undefined); }}>Approve and apply</button>
              <button type="button" disabled={!actionable} onClick={() => { void model.withdraw().catch(() => undefined); }}>Withdraw</button>
            </div>
          </article>
        )}
      </div>
      {state.failure === undefined ? null : <p role="alert">{state.failure.message}</p>}
    </section>
  );
}

export function reviewState(model: ReviewModel): ReviewModelState { return model.snapshot(); }
