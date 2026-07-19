// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import test from "node:test";
import { createLibrary } from "../dist/index.js";

const digest = `sha256:${"a".repeat(64)}`;
const identity = { kind: "template", canonical_id: "layerdraw/starter", version: "1.0.0" };
const release = { identity, source_id: "official", publisher_id: "layerdraw", digest, manifest_digest: digest, dependency_metadata_digest: digest, size: 10, dependencies: [], compatibility: [], trust: { status: "verified", policy_digest: digest, evidence_digest: digest }, license: "Apache-2.0", provenance_digest: digest };
const source = { source_id: "official", kind: "official", endpoint_ref: "registry:official", trust_policy_id: "official", cache_policy: "verified", priority: 100, connected: true };
const snapshot = { resolved_lock_digest: digest, installs: [] };
const plan = { transaction_id: "tx", plan_digest: digest, action: "create_from_template", project_id: "p", base_revision: "r", artifacts: [{ release, validation: { identity, canonical_digest: digest, staged_tree_manifest: digest, resolved_lock_digest: digest, mutation_digest: digest, authoring_impact_digest: digest, address_migration_plan_digest: digest, diagnostics: [] } }], required_capabilities: ["package:manage", "schema:write"], trust_policy_digests: [digest], source_bindings: [{ source_id: "official", source_digest: digest, trust_policy_digest: digest }], dependency_snapshot: snapshot, resolved_lock_delta: { added: [], updated: [], removed: [], pinned: [] }, rollback_checkpoint: { base_project_revision: "r", base_definition_hash: digest, base_resolved_lock_digest: digest, current_pack_tree_manifest: digest }, expires_at: "2030-01-01T00:00:00Z", migration_required: false, creates_new_document: true, mutation_digest: digest, authoring_impact_digests: [digest], host_operation_impact_digest: digest, evaluation_digest: digest, host_operation_impacts: [], access_decision: {}, host_capabilities_digest: digest };
const committedTransaction = { plan, events: [{ state: "planned", evidence_digest: digest, sequence: 1 }, { state: "committed", evidence_digest: digest, sequence: 2 }], committed_revision: "r2", operation_result_id: "result" };
const repairTransaction = { plan, events: [{ state: "planned", evidence_digest: digest, sequence: 1 }, { state: "repair_required", evidence_digest: digest, sequence: 2 }] };
const capabilities = { browse: true, manage_sources: true, author_artifacts: true, plan_transactions: true, commit_transactions: true };

function client(extension = {}) {
  return {
    async listSources() { return { ok: true, value: [source] }; },
    async configureSource(input) { return { ok: true, value: { ...input, connected: false } }; },
    async connectSource() { return { ok: true, value: source }; },
    async disconnectSource() { return { ok: true, value: { ...source, connected: false } }; },
    async search() { return { ok: true, value: [release] }; },
    async plan() { return { ok: true, value: plan }; },
    async commit() { return { ok: true, value: { committed_revision: "r2", operation_result_id: "result" } }; },
    async getTransaction() { return { ok: true, value: committedTransaction }; },
    async recoverTransaction() { return { ok: true, value: committedTransaction }; },
    async authorArtifact() { return { ok: true, value: release }; },
    ...extension,
  };
}

test("Library presents host-owned browse, verified plan, template, and atomic commit workflows", async () => {
  const events = [];
  const library = createLibrary({ client: client(), capabilities, onEvent(event) { events.push(event.snapshot.status); } });
  assert.equal((await library.refreshSources()).sources[0].source_id, "official");
  assert.equal((await library.configureSource({ ...source, connected: undefined })).sources[0].connected, false);
  assert.equal((await library.connectSource("official", "credential:official")).sources[0].connected, true);
  assert.equal((await library.disconnectSource("official")).sources[0].connected, false);
  assert.equal((await library.search("starter", "template")).results[0].trust.status, "verified");
  assert.equal(library.select(release.identity).selected.digest, digest);
  const preview = await library.preview("create_from_template", { project_id: "p", revision: "r", definition_hash: digest, resolved_lock_digest: digest, dependency_snapshot: snapshot });
  assert.equal(preview.status, "awaiting_confirmation");
  assert.deepEqual(preview.plan.required_capabilities, ["package:manage", "schema:write"]);
  const committed = await library.confirm("op", "idem");
  assert.equal(committed.status, "committed");
  assert.equal(committed.transaction.committed_revision, "r2");
  assert.equal((await library.getTransaction("tx")).status, "committed");
  assert.equal((await library.recoverTransaction("tx")).transaction.operation_result_id, "result");
  assert.ok(events.includes("previewing") && events.includes("applying"));
  const authored = await library.author({ kind: "template", project_id: "p", output_name: "starter.layerdraw", publisher_id: "layerdraw", version: "1.0.0" });
  assert.equal(authored.selected.identity.kind, "template");
});

test("Library preserves actionable failures and disables unavailable host capabilities", async () => {
  const failure = { code: "registry.signature_revoked", subject: "layerdraw", actionable: true };
  const library = createLibrary({ client: client({ async search() { return { ok: false, failure }; } }), capabilities });
  assert.equal((await library.search("starter")).failure.code, "registry.signature_revoked");
  assert.equal(library.select(release.identity).failure.code, "registry.unavailable");
  assert.equal((await library.preview("install", { project_id: "p", revision: "r", definition_hash: digest, resolved_lock_digest: digest, dependency_snapshot: snapshot })).failure.subject, "selection");
  assert.equal((await library.confirm("op", "id")).failure.code, "registry.plan_stale");
  const disabled = createLibrary({ client: client(), capabilities: { browse: false, manage_sources: false, author_artifacts: false, plan_transactions: false, commit_transactions: false, unavailable_reason: "viewer_only" } });
  assert.equal(disabled.snapshot().status, "disabled");
  assert.equal((await disabled.refreshSources()).failure.subject, "browse");
  assert.equal((await disabled.search("x")).failure.subject, "browse");
  assert.equal((await disabled.configureSource({ ...source, connected: undefined })).failure.subject, "manage_sources");
  assert.equal((await disabled.connectSource("official", "credential:official")).failure.subject, "manage_sources");
  assert.equal((await disabled.disconnectSource("official")).failure.subject, "manage_sources");
  assert.equal((await disabled.preview("install", { project_id: "p", revision: "r", definition_hash: digest, resolved_lock_digest: digest, dependency_snapshot: snapshot })).failure.subject, "plan_transactions");
  assert.equal((await disabled.confirm("op", "id")).failure.subject, "commit_transactions");
  assert.equal((await disabled.author({ kind: "pack", project_id: "p", output_name: "x.ldpack", publisher_id: "p", version: "1.0.0" })).failure.subject, "author_artifacts");
});

test("Library maps repair states and cancellation without inventing recovery decisions", async () => {
  let resolveSearch;
  const pending = new Promise((resolve) => { resolveSearch = resolve; });
  const library = createLibrary({ client: client({ async search() { return pending; } }), capabilities });
  const operation = library.search("slow"); library.cancel(); resolveSearch({ ok: true, value: [release] });
  assert.equal((await operation).status, "loading");
  const ready = createLibrary({ client: client({ async getTransaction() { return { ok: true, value: repairTransaction }; }, async recoverTransaction() { return { ok: true, value: repairTransaction }; } }), capabilities });
  await ready.search("starter"); ready.select(release.identity); await ready.preview("create_from_template", { project_id: "p", revision: "r", definition_hash: digest, resolved_lock_digest: digest, dependency_snapshot: snapshot });
  assert.equal((await ready.confirm("op", "id")).status, "recoverable_error");
  assert.equal((await ready.recoverTransaction("tx")).status, "recoverable_error");
  const rejected = createLibrary({ client: client({ async authorArtifact() { return { ok: false, failure: { code: "registry.artifact_corrupt", subject: "output", actionable: true } }; } }), capabilities });
  assert.equal((await rejected.author({ kind: "pack", project_id: "p", output_name: "x.ldpack", publisher_id: "p", version: "1.0.0" })).failure.code, "registry.artifact_corrupt");
});
