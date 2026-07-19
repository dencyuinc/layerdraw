// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import test from "node:test";
import { createHostRegistryClient } from "../dist/host.js";

const digest = `sha256:${"a".repeat(64)}`;
const source = { source_id: "official", kind: "official", endpoint_ref: "registry:official", trust_policy_id: "official", cache_policy: "verified", priority: 100, connected: true };
const release = { identity: { kind: "pack", canonical_id: "layerdraw/base", version: "1.0.0" }, source_id: "official", publisher_id: "layerdraw", digest, size: 10, dependencies: [], compatibility: [], signature_status: "verified", license: "Apache-2.0", provenance_digest: digest };
const plan = { transaction_id: "tx", plan_digest: digest, action: "install", project_id: "p", base_revision: "r", artifacts: [], required_capabilities: ["package:manage"], trust_policy_digests: [digest], mutation_digest: digest, authoring_impact_digests: [digest], host_operation_impact_digest: digest, evaluation_digest: digest };
const transaction = { transaction_id: "tx", state: "committed", plan_digest: digest, evidence_digest: digest, committed_revision: "r2" };

test("host adapter maps every operation without interpreting registry semantics", async () => {
  const calls = [];
  const binding = { async invoke(operation, input) {
    calls.push({ operation, input: structuredClone(input) });
    const values = {
      "registry.list_sources": [source], "registry.configure_source": source,
      "registry.connect_source": source, "registry.disconnect_source": { ...source, connected: false },
      "registry.search": [release], "registry.plan_install": plan,
      "registry.commit_plan": transaction, "registry.get_transaction": transaction,
      "registry.author_artifact": release,
    };
    return { ok: true, value: values[operation] };
  }};
  const client = createHostRegistryClient(binding);
  assert.deepEqual((await client.listSources()).value, [source]);
  assert.equal((await client.configureSource({ ...source, connected: undefined })).ok, true);
  assert.equal((await client.connectSource({ source_id: "official", connection_ref: "keychain:official" })).ok, true);
  assert.equal((await client.disconnectSource("official")).ok, true);
  assert.deepEqual((await client.search({ query: "base", kind: "pack" })).value, [release]);
  assert.deepEqual((await client.plan({ action: "install", project_id: "p", base_revision: "r", expected_definition_hash: digest, expected_resolved_lock_digest: digest, requested: release.identity })).value, plan);
  assert.deepEqual((await client.commit({ transaction_id: "tx", plan_digest: digest, operation_id: "op", idempotency_key: "id" })).value, transaction);
  assert.equal((await client.getTransaction("tx")).ok, true);
  assert.equal((await client.authorArtifact({ kind: "pack", project_id: "p", output_name: "base.ldpack", publisher_id: "layerdraw", version: "1.0.0" })).ok, true);
  assert.deepEqual(calls.map((call) => call.operation), ["registry.list_sources", "registry.configure_source", "registry.connect_source", "registry.disconnect_source", "registry.search", "registry.plan_install", "registry.commit_plan", "registry.get_transaction", "registry.author_artifact"]);
  assert.equal(JSON.stringify(calls).includes("keychain:official"), true);
  assert.equal(JSON.stringify(calls).includes("signature decision"), false);
});

test("host adapter fail-closes invalid, thrown, and cancelled responses", async () => {
  assert.throws(() => createHostRegistryClient(), TypeError);
  const invalid = createHostRegistryClient({ async invoke() { return { ok: true }; } });
  assert.equal((await invalid.listSources()).failure.code, "registry.unavailable");
  const malformedFailure = createHostRegistryClient({ async invoke() { return { ok: false, failure: { code: 1 } }; } });
  assert.equal((await malformedFailure.search({ query: "x" })).failure.subject, "invalid_host_response");
  const failure = createHostRegistryClient({ async invoke() { return { ok: false, failure: { code: "registry.signature_revoked", subject: "publisher", actionable: true } }; } });
  assert.equal((await failure.search({ query: "x" })).failure.code, "registry.signature_revoked");
  const thrown = createHostRegistryClient({ async invoke() { throw new Error("credential leak"); } });
  assert.deepEqual(await thrown.listSources(), { ok: false, failure: { code: "registry.unavailable", subject: "registry.list_sources", actionable: true } });
  const controller = new AbortController(); controller.abort();
  assert.equal((await thrown.listSources(controller.signal)).failure.code, "registry.cancelled");
});
