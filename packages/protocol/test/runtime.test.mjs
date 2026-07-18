// SPDX-License-Identifier: Apache-2.0

import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import test from "node:test";

import accessDecisionFixture from "../../../schemas/fixtures/runtime/access-decision.json" with { type: "json" };
import commitFailedFixture from "../../../schemas/fixtures/runtime/commit-failed.json" with { type: "json" };
import commitRequestFixture from "../../../schemas/fixtures/runtime/commit-request.json" with { type: "json" };
import commitResultPreviewDecisionOnlyFixture from "../../../schemas/fixtures/runtime/commit-result-preview-decision-only.json" with { type: "json" };
import commitResultPreviewImpactOnlyFixture from "../../../schemas/fixtures/runtime/commit-result-preview-impact-only.json" with { type: "json" };
import digestCanonicalizationFixture from "../../../schemas/fixtures/runtime/digest-canonicalization.json" with { type: "json" };
import handshakeFailedFixture from "../../../schemas/fixtures/runtime/handshake-failed.json" with { type: "json" };
import handshakeRequestFixture from "../../../schemas/fixtures/runtime/handshake-request.json" with { type: "json" };
import operationAuditPendingFixture from "../../../schemas/fixtures/runtime/operation-audit-pending.json" with { type: "json" };
import operationNeedsReviewFixture from "../../../schemas/fixtures/runtime/operation-needs-review.json" with { type: "json" };
import operationRecoveringFixture from "../../../schemas/fixtures/runtime/operation-recovering.json" with { type: "json" };
import revisionPageFixture from "../../../schemas/fixtures/runtime/revision-page.json" with { type: "json" };
import { decodeAuthoringDecision, decodeHostOperationImpact, encodeAuthoringDecision } from "../dist/access.gen.js";
import {
  decodeCommitOperationsRequestEnvelope,
  decodeCommitOperationsResponseEnvelope,
  decodeOperationResult,
  decodeRuntimeCommitResult,
  decodeListRevisionsRequestEnvelope,
  decodeRevisionPage,
  decodeRuntimeHandshakeRequestEnvelope,
  decodeRuntimeHandshakeResponseEnvelope,
  decodeRuntimeOperationStatus,
  encodeCommitOperationsRequestEnvelope,
  encodeCommitOperationsResponseEnvelope,
  encodeRevisionPage,
  encodeRuntimeHandshakeRequestEnvelope,
  encodeRuntimeHandshakeResponseEnvelope,
  encodeRuntimeOperationStatus,
} from "../dist/runtime.gen.js";

const fixtures = Object.freeze({
  "access-decision.json": accessDecisionFixture,
  "commit-failed.json": commitFailedFixture,
  "commit-request.json": commitRequestFixture,
  "commit-result-preview-decision-only.json": commitResultPreviewDecisionOnlyFixture,
  "commit-result-preview-impact-only.json": commitResultPreviewImpactOnlyFixture,
  "digest-canonicalization.json": digestCanonicalizationFixture,
  "handshake-failed.json": handshakeFailedFixture,
  "handshake-request.json": handshakeRequestFixture,
  "operation-audit-pending.json": operationAuditPendingFixture,
  "operation-needs-review.json": operationNeedsReviewFixture,
  "operation-recovering.json": operationRecoveringFixture,
  "revision-page.json": revisionPageFixture,
});
const fixture = async (name) => {
  const value = fixtures[name];
  assert.ok(value, `unknown runtime fixture: ${name}`);
  return structuredClone(value);
};

test("runtime canonical fixtures round-trip through generated TypeScript codecs", async () => {
  const cases = [
    ["handshake-request.json", decodeRuntimeHandshakeRequestEnvelope, encodeRuntimeHandshakeRequestEnvelope],
    ["handshake-failed.json", decodeRuntimeHandshakeResponseEnvelope, encodeRuntimeHandshakeResponseEnvelope],
    ["commit-request.json", decodeCommitOperationsRequestEnvelope, encodeCommitOperationsRequestEnvelope],
    ["commit-failed.json", decodeCommitOperationsResponseEnvelope, encodeCommitOperationsResponseEnvelope],
    ["operation-recovering.json", decodeRuntimeOperationStatus, encodeRuntimeOperationStatus],
    ["operation-audit-pending.json", decodeRuntimeOperationStatus, encodeRuntimeOperationStatus],
    ["operation-needs-review.json", decodeRuntimeOperationStatus, encodeRuntimeOperationStatus],
    ["revision-page.json", decodeRevisionPage, encodeRevisionPage],
    ["access-decision.json", decodeAuthoringDecision, encodeAuthoringDecision],
  ];
  for (const [name, decode, encode] of cases) {
    const first = encode(decode(JSON.stringify(await fixture(name))));
    assert.equal(encode(decode(first)), first, name);
  }
});

test("runtime TypeScript codecs reject unknown fields and invalid typed outcomes", async () => {
  for (const name of ["commit-result-preview-impact-only.json", "commit-result-preview-decision-only.json"]) {
    const oneSided = await fixture(name);
    assert.throws(() => decodeRuntimeCommitResult(JSON.stringify(oneSided)), name);
  }
  const needsReview = await fixture("operation-needs-review.json");
  needsReview.operation_result.diagnostics = [];
  assert.throws(() => decodeRuntimeOperationStatus(JSON.stringify(needsReview)));
  const handshake = await fixture("handshake-request.json");
  handshake.unknown_minor_field = true;
  assert.throws(() => decodeRuntimeHandshakeRequestEnvelope(JSON.stringify(handshake)));

  delete handshake.unknown_minor_field;
  const correctUnits = {
    max_blob_bytes: "bytes",
    max_blob_total_bytes: "bytes",
    max_commit_operations: "items",
    max_history_items: "items",
    max_output_bytes: "bytes",
    max_state_mutations: "items",
  };
  for (const [field, correctUnit] of Object.entries(correctUnits)) {
    handshake.payload.client_limits = Object.fromEntries(
      Object.entries(correctUnits).map(([name, unit]) => [name, {hard_maximum: "10", unit}]),
    );
    handshake.payload.client_limits[field].unit = correctUnit === "bytes" ? "items" : "bytes";
    assert.throws(() => decodeRuntimeHandshakeRequestEnvelope(JSON.stringify(handshake)), field);
  }

  const commit = await fixture("commit-request.json");
  commit.payload.session.scope.document_id = "";
  assert.throws(() => decodeCommitOperationsRequestEnvelope(JSON.stringify(commit)));

  assert.throws(() => decodeOperationResult(JSON.stringify({
    operation_id: "operation_1",
    idempotency_key: "idem_commit_000001",
    status: "rejected",
    diagnostics: [],
    result_digest: `sha256:${"a".repeat(64)}`,
  })));
  assert.throws(() => decodeRuntimeOperationStatus(JSON.stringify({operation_id: "operation_1", idempotency_key: "idem_commit_000001", phase: "final"})));
  assert.throws(() => decodeRuntimeOperationStatus(JSON.stringify({operation_id: "operation_1", idempotency_key: "idem_commit_000001", phase: "audit_pending", recovery_started_at: "2026-07-18T10:00:00Z"})));
  assert.throws(() => decodeListRevisionsRequestEnvelope(JSON.stringify({
    operation: "runtime.list_revisions",
    payload: {max_items: "1", max_output_bytes: "1024", session: {runtime_session_id: "runtime_session_fixture_1", scope: {access_fingerprint: `sha256:${"1".repeat(64)}`, document_id: "doc_fixture", local_scope_id: "local_fixture"}, session_generation: "1"}},
    protocol: {name: "engine", version: "1.0"},
    request_id: "runtime-list-wrong-protocol",
  })));
  assert.throws(() => decodeHostOperationImpact(JSON.stringify({
    action: "stage",
    impact_digest: `sha256:${"3".repeat(64)}`,
    operation_kind: "asset_stage",
    required_authoring_capabilities: ["asset:write"],
    resource_refs: ["z", "a"],
    resource_scope: {document_id: "doc_fixture", local_scope_id: "local_fixture"},
  })));
  assert.throws(() => decodeAuthoringDecision(JSON.stringify({
    access_fingerprint: `sha256:${"1".repeat(64)}`,
    approval_rule_refs: [],
    constraint_violations: [],
    decision_digest: `sha256:${"3".repeat(64)}`,
    diagnostics: [],
    evaluation_digest: `sha256:${"4".repeat(64)}`,
    host_operation_impact_digests: [`sha256:${"b".repeat(64)}`, `sha256:${"a".repeat(64)}`],
    missing_capabilities: [],
    outcome: "allow",
    required_capabilities: [],
  })));
  assert.throws(() => decodeAuthoringDecision(JSON.stringify({
    access_fingerprint: `sha256:${"1".repeat(64)}`,
    approval_rule_refs: ["z", "a"],
    constraint_violations: [],
    decision_digest: `sha256:${"3".repeat(64)}`,
    diagnostics: [],
    evaluation_digest: `sha256:${"4".repeat(64)}`,
    host_operation_impact_digests: [],
    missing_capabilities: [],
    outcome: "approval_required",
    required_capabilities: [],
  })));
});

test("runtime digest projection fixture uses cross-language canonical JSON", async () => {
  const fixtureValue = await fixture("digest-canonicalization.json");
  const canonicalize = (value) => {
    if (Array.isArray(value)) return value.map(canonicalize);
    if (value !== null && typeof value === "object") {
      return Object.fromEntries(Object.keys(value).sort().map((key) => [key, canonicalize(value[key])]));
    }
    return value;
  };
  const canonical = JSON.stringify(canonicalize(fixtureValue.value));
  assert.equal(canonical, fixtureValue.canonical);
  assert.equal(`sha256:${createHash("sha256").update(canonical).digest("hex")}`, fixtureValue.sha256);
});

test("runtime extensions are the explicit unknown-field preservation channel", async () => {
  const value = decodeRuntimeHandshakeRequestEnvelope(JSON.stringify(await fixture("handshake-request.json")));
  const roundTrip = decodeRuntimeHandshakeRequestEnvelope(encodeRuntimeHandshakeRequestEnvelope(value));
  assert.deepEqual(roundTrip.extensions, {"com.layerdraw.fixture": {preserved: true}});
});
