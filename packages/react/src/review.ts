// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {createElement, useEffect, useState, useSyncExternalStore} from "react";
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
  return createElement("div", {className: "ld-review-evidence"},
    createElement("section", {"aria-labelledby": "review-definition"}, createElement("h3", {id: "review-definition"}, "Definition and source"),
      createElement("p", null, `Definition ${proposal.proposed_definition_hash}`),
      value.definition_preview === undefined ? null : createElement("p", null, `${value.definition_preview.label} (${value.definition_preview.media_type}) ${value.definition_preview.digest}`),
      createElement("ul", null, ...value.source_diff.edits.map((item, index) => createElement("li", {key: `source-${index}`}, `${item.kind}${item.source_range === undefined ? "" : " at source range"}`)))),
    createElement("section", {"aria-labelledby": "review-impact"}, createElement("h3", {id: "review-impact"}, "Semantic impact"),
      createElement("ul", null, ...value.semantic_diff.entries.map((item, index) => createElement("li", {key: `semantic-${index}`}, `${item.kind}: ${item.subject_kind}`))),
      createElement("p", null, `Required capabilities: ${proposal.required_capabilities.join(", ") || "none"}`),
      createElement("p", null, "Affected usages"), createElement("ul", null, ...value.affected_usages.map((item) => createElement("li", {key: item}, item))),
      createElement("p", null, "Affected rows"), createElement("ul", null, ...value.affected_rows.map((item) => createElement("li", {key: item}, item))),
      createElement("p", null, "Affected views"), createElement("ul", null, ...value.affected_views.map((item) => createElement("li", {key: item}, item)))),
    createElement("section", {"aria-labelledby": "review-diagnostics"}, createElement("h3", {id: "review-diagnostics"}, "Diagnostics and previews"),
      createElement("ul", null, ...value.diagnostics.map((item, index) => createElement("li", {key: `${item.code}-${index}`}, `${item.code}: ${item.message_key}`))),
      createElement("ul", null, ...value.render_previews.map((item) => createElement("li", {key: item.digest}, `${item.label} (${item.media_type})`)))));
}

export function ReviewPanel({model, className}: ReviewPanelProps) {
  const state = useSyncExternalStore(model.subscribe, model.snapshot, model.snapshot);
  const [comment, setComment] = useState("");
  useEffect(() => { if (state.phase === "idle") void model.refresh().catch(() => undefined); }, [model, state.phase]);
  const selected = state.selected;
  const busy = state.phase === "loading" || state.phase === "mutating";
  const actionable = selected !== undefined && !["applied", "withdrawn", "superseded"].includes(selected.status) && !busy;
  return createElement("section", {className: ["ld-review-panel", className].filter(Boolean).join(" "), "aria-label": "Review proposals"},
    createElement("header", null, createElement("h2", null, "Review"), createElement("button", {type: "button", disabled: busy, onClick: () => { void model.refresh().catch(() => undefined); }}, "Refresh")),
    createElement("div", {className: "ld-review-layout"},
      createElement("nav", {"aria-label": "Proposals"}, createElement("ul", null, ...state.snapshot.proposals.map((item) => createElement("li", {key: item.id},
        createElement("button", {type: "button", "aria-current": selected?.id === item.id ? "true" : undefined, onClick: () => model.select(item.id)}, `${item.id} — ${item.status}`))))),
      selected === undefined ? createElement("p", null, "Select a proposal to inspect it.") : createElement("article", {"aria-label": `Proposal ${selected.id}`},
        createElement("h2", null, selected.id), createElement("p", {role: "status"}, `Status: ${selected.status}`),
        createElement("p", null, `Proposed by ${selected.proposer.kind} ${selected.proposer.actor_id}`),
        createElement("p", null, `Access evaluation ${selected.access_evaluation_digest}`), evidence(selected),
        createElement("section", {"aria-labelledby": "review-comments"}, createElement("h3", {id: "review-comments"}, "Comments"),
          createElement("ul", null, ...selected.comments.map((item) => createElement("li", {key: item.id}, `${targetLabel(item.target)}: ${item.body}${item.stale ? " (stale)" : ""}`))),
          createElement("label", null, "Project comment", createElement("textarea", {value: comment, onChange: (event: ChangeEvent<HTMLTextAreaElement>) => setComment(event.currentTarget.value)})),
          createElement("button", {type: "button", disabled: !actionable || comment.trim() === "", onClick: () => { const body = comment.trim(); void model.comment({comment_id: globalThis.crypto.randomUUID(), body, target: {kind: "project"}}).then(() => setComment("")).catch(() => undefined); }}, "Add comment")),
        createElement("div", {className: "ld-review-actions"},
          createElement("button", {type: "button", disabled: !actionable, onClick: () => { void model.approveAndApply().catch(() => undefined); }}, "Approve and apply"),
          createElement("button", {type: "button", disabled: !actionable, onClick: () => { void model.withdraw().catch(() => undefined); }}, "Withdraw")))),
    state.failure === undefined ? null : createElement("p", {role: "alert"}, state.failure.message));
}

export function reviewState(model: ReviewModel): ReviewModelState { return model.snapshot(); }
