// SPDX-License-Identifier: Apache-2.0

import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { chmod, mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test, { after } from "node:test";

import { createLocalHostClient } from "../dist/host.js";
import { encodeStateQuerySnapshot } from "@layerdraw/protocol/semantic";
import { makePortableRequest } from "./support.mjs";

const repositoryRoot = resolve(import.meta.dirname, "../../..");
const temporary = await mkdtemp(join(tmpdir(), "layerdraw-local-host-"));
const binaryPath = join(temporary, "layerdraw-host");
const storageRoot = join(temporary, "storage");
const projectRoot = join(temporary, "project");
const manifestBytes = await readFile(join(repositoryRoot, "deploy/development-release-manifest.json"));
const releaseManifestDigest = `sha256:${createHash("sha256").update(manifestBytes).digest("hex")}`;
const persistenceCorpus = JSON.parse(await readFile(join(repositoryRoot, "tests/conformance/testdata/local_runtime_persistence_v1.json"), "utf8"));
assert.equal(persistenceCorpus.schema_version, 1);
const processCrashFault = persistenceCorpus.fault_matrix.find((fault) => fault.id === "runtime_process_crash" && fault.surface === "typescript_stdio");
assert.notEqual(processCrashFault, undefined);

await mkdir(projectRoot, { recursive: true });
await writeFile(join(projectRoot, "document.ldl"), persistenceCorpus.workflow.initial_source);
const build = spawnSync("go", [
  "build", "-trimpath", "-buildvcs=false", "-ldflags",
  `-s -w -X main.releaseVersion=0.0.0-dev -X main.sourceRevision=abcdef0 -X main.releaseManifestDigest=${releaseManifestDigest}`,
  "-o", binaryPath, "./cmd/layerdraw-host",
], { cwd: repositoryRoot, encoding: "utf8" });
assert.equal(build.status, 0, `${build.stdout}\n${build.stderr}`);
await chmod(binaryPath, 0o755);

after(async () => { await rm(temporary, { recursive: true, force: true }); });

function recordingLifecycle(records) {
  return {
    spawn(command, arguments_, options) {
      const child = spawn(command, [...arguments_], {
        ...options, shell: false, stdio: ["pipe", "pipe", "ignore"], windowsHide: true,
      });
      records.push({ arguments: [...arguments_], child });
      return child;
    },
  };
}

const runtimeOperations = [
  "runtime.handshake", "runtime.open_document", "runtime.inspect_document",
  "runtime.preview_operations", "runtime.commit_operations", "runtime.save_document",
  "runtime.control_autosave", "runtime.get_state_snapshot", "runtime.list_revisions",
  "runtime.preview_restore", "runtime.stage_asset", "runtime.close_document",
  "runtime.cancel_operation", "runtime.get_operation_result", "runtime.recover_operations",
];

function preconditions(compiled) {
  return {
    document_generation: {
      document_handle: { endpoint_instance_id: "placeholder", value: "document_placeholder_123456" },
      value: "1",
    },
    expected_child_sets: compiled.child_set_hashes.map(({ owner_address, child_kind, hash }) => ({ owner_address, child_kind, hash })),
    expected_source_digests: compiled.source_map.files.map(({ digest, module_path, origin }) => ({ digest, module: { module_path, origin } })),
    expected_subject_hashes: compiled.subject_semantic_hashes.map(({ address, hash }) => ({ address, hash })),
    expected_subtree_hashes: compiled.subtree_hashes.map(({ owner_address, hash }) => ({ address: owner_address, hash })),
  };
}

function editBatch(revision, suffix, compiled, operations) {
  return {
    document_id: revision.document_id,
    base_revision: revision,
    expected_definition_hash: revision.definition_hash,
    operations: operations ?? { operations: [{
      operation: "create_subject", subject_kind: "layer",
      parent_address: "ldl:project:p", id: `layer_${suffix}`,
      fields: { display_name: `Layer ${suffix}`, order: "1" },
    }] },
    preconditions: preconditions(compiled),
  };
}

async function previewedCommit(client, session, revision, suffix, trigger, compiled, operations) {
  const operation_batch = editBatch(revision, suffix, compiled, operations);
  const preview = await client.previewOperations({ session, operation_batch });
  assert.equal(preview.outcome, "success", JSON.stringify(preview));
  return {
    input: {
      session, operation_batch, authoring_proof: preview.payload.authoring_proof,
      operation_id: `operation_${suffix}`, idempotency_key: `idempotency_${suffix}`,
      trigger,
    },
    preview,
  };
}

async function waitForOperation(client, session, operationId) {
  const deadline = Date.now() + 5_000;
  while (Date.now() < deadline) {
    const result = await client.getOperationResult({ session, lookup_by: "operation_id", operation_id: operationId });
    if (result.outcome === "success" && result.payload.operation_result != null) return result;
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error(`operation ${operationId} did not become terminal`);
}

test("local host exposes generated Runtime operations and the existing Engine facade", async (context) => {
  const processes = [];
  const client = await createLocalHostClient({
    binaryPath, storageRoot, expectedReleaseManifestDigest: releaseManifestDigest,
    processLifecycle: recordingLifecycle(processes),
  });
  context.after(() => client.dispose());

  assert.equal(processes.length, 2);
  assert.deepEqual(processes.map((record) => record.arguments[0]).sort(), ["engine-stdio", "stdio"]);
  for (const operation of runtimeOperations) assert.equal(client.hasCapability(operation), true, operation);
  for (const operation of ["registry.resolve", "runtime.realtime", "runtime.mcp", "runtime.remote_storage", "runtime.native_export", "runtime.organization", "runtime.workspace"]) {
    assert.equal(client.hasCapability(operation), false, operation);
  }
  assert.equal(client.engine.hasCapability("engine.compile"), true);
  assert.equal(client.engine.getCapabilities().operations["engine.preview_operations"].enabled, true);
  const portable = makePortableRequest(persistenceCorpus.workflow.initial_source);
  const compilation = await client.engine.compile(portable.request);
  assert.equal(compilation.outcome, "success");
  const compiled = compilation.response.payload;

  const engineOpened = await client.engine.workbench.openDocument({
    compile_input: portable.request.input,
    requested_limits: { max_items: "10000", max_output_bytes: "67108864" },
  }, { blobs: portable.request.blobs });
  assert.equal(engineOpened.outcome, "success");
  const engineGeneration = engineOpened.response.payload.document_generation;
  const enginePreconditions = { ...preconditions(compiled), document_generation: engineGeneration };
  const enginePreview = await client.engine.workbench.previewOperations({
    batch: { operations: [{ operation: "create_subject", subject_kind: "layer", parent_address: "ldl:project:p", id: "browser_editor", fields: { display_name: "Browser Editor", order: "99" } }] },
    limits: { max_items: "10000", max_output_bytes: "67108864" },
    preconditions: enginePreconditions,
  });
  assert.equal(enginePreview.outcome, "success", JSON.stringify(enginePreview));
  assert.equal(enginePreview.response.payload.status, "valid");
  const engineApplied = await client.engine.workbench.applyToHandle({
    base_generation: enginePreview.response.payload.base_generation,
    preview_digest: enginePreview.response.payload.preview_digest,
    preview_id: enginePreview.response.payload.preview_id,
  });
  assert.equal(engineApplied.outcome, "success", JSON.stringify(engineApplied));
  assert.equal(engineApplied.response.payload.document_generation.value, "2");

  const opened = await client.openDocument({
    document_id: "bootstrap",
    local_source: { kind: "project", path: projectRoot, entry_path: "document.ldl" },
  });
  assert.equal(opened.outcome, "success");
  assert.equal(opened.payload.committed_revision.definition_hash, persistenceCorpus.workflow.expected.initial_definition_hash);
  assert.equal(opened.payload.committed_revision.graph_hash, persistenceCorpus.workflow.expected.initial_graph_hash);
  let session = opened.payload.session;
  let documentId;

  const inspected = await client.inspectDocument({ session });
  assert.equal(inspected.outcome, "success");
  const state = await client.getStateSnapshot({ session });
  assert.equal(state.outcome, "success");
  assert.equal(state.payload.state_input.kind, persistenceCorpus.workflow.expected.state_kind);
  assert.equal(state.payload.state_input.snapshot.format, "layerdraw-query-state");
  const canonicalState = encodeStateQuerySnapshot(state.payload.state_input.snapshot);
  assert.match(state.payload.state_input.snapshot_hash, /^sha256:[0-9a-f]{64}$/);
  const repeatedState = await client.getStateSnapshot({ session });
  assert.equal(encodeStateQuerySnapshot(repeatedState.payload.state_input.snapshot), canonicalState);
  assert.equal(repeatedState.payload.state_input.snapshot_hash, state.payload.state_input.snapshot_hash);

  const first = await previewedCommit(client, session, opened.payload.committed_revision, "conformance", "explicit_save", compiled, persistenceCorpus.workflow.operations);
  const committed = await client.commitOperations(first.input);
  assert.equal(committed.outcome, "success", JSON.stringify(committed));
  assert.equal(committed.payload.operation_result.status, persistenceCorpus.workflow.expected.commit_status);
  assert.equal(committed.payload.operation_result.external_materialization.state, persistenceCorpus.workflow.expected.external_state);
  assert.equal(committed.payload.operation_result.committed_revision.definition_hash, persistenceCorpus.workflow.expected.committed_definition_hash);
  assert.equal(committed.payload.operation_result.committed_revision.graph_hash, persistenceCorpus.workflow.expected.committed_graph_hash);
  assert.match(await readFile(join(projectRoot, "document.ldl"), "utf8"), /layer_conformance/);
  const duplicate = await client.commitOperations(first.input);
  assert.deepEqual(duplicate.payload.operation_result, committed.payload.operation_result);

  const saveRoot = join(temporary, "save-project");
  await mkdir(saveRoot, { recursive: true });
  await writeFile(join(saveRoot, "document.ldl"), 'project p "P" {}\n');
  const saveOpened = await client.openDocument({ document_id: "save-bootstrap", local_source: { kind: "project", path: saveRoot, entry_path: "document.ldl" } });
  session = saveOpened.payload.session;
  const second = await previewedCommit(client, session, saveOpened.payload.committed_revision, "save", "explicit_save", compiled);
  const saved = await client.saveDocument(second.input);
  assert.equal(saved.outcome, "success", JSON.stringify(saved));
  assert.equal(saved.payload.operation_result.status, "committed");
  assert.equal(saved.payload.operation_result.external_materialization.state, "published");
  assert.match(await readFile(join(saveRoot, "document.ldl"), "utf8"), /layer_save/);

  const autosaveRoot = join(temporary, "autosave-project");
  await mkdir(autosaveRoot, { recursive: true });
  await writeFile(join(autosaveRoot, "document.ldl"), 'project p "P" {}\n');
  const autosaveOpened = await client.openDocument({ document_id: "autosave-bootstrap", local_source: { kind: "project", path: autosaveRoot, entry_path: "document.ldl" } });
  session = autosaveOpened.payload.session;
  documentId = autosaveOpened.payload.committed_revision.document_id;
  const third = await previewedCommit(client, session, autosaveOpened.payload.committed_revision, "autosave", "autosave", compiled);
  const scheduled = await client.controlAutosave({ session, action: "schedule", commit: third.input });
  assert.equal(scheduled.outcome, "success", JSON.stringify(scheduled));
  assert.equal(scheduled.payload.scheduled, true);
  const operation = await waitForOperation(client, session, "operation_autosave");
  assert.equal(operation.payload.operation_result.status, "committed");
  assert.equal(operation.payload.operation_result.external_materialization.state, "published");
  assert.match(await readFile(join(autosaveRoot, "document.ldl"), "utf8"), /layer_autosave/);
  const history = await client.listRevisions({ session, max_items: "20", max_output_bytes: "1048576" });
  assert.equal(history.outcome, "success");
  assert.ok(history.payload.items.length >= 2, JSON.stringify(history));
  const cancelled = await client.cancelOperation({ session, operation_id: "operation_autosave", cancellation_token: "cancel_autosave_123456" });
  assert.equal(cancelled.outcome, "success");
  assert.equal(cancelled.payload.status, "not_pending");
  assert.equal((await client.previewRestore({ session, revision_id: history.payload.items[0].revision.revision_id })).outcome, "success");

  const bytes = new TextEncoder().encode(persistenceCorpus.workflow.asset_text);
  const blob = {
    blob_id: "asset/test.bin",
    digest: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
    lifetime: "request",
    media_type: "application/octet-stream",
    size: String(bytes.byteLength),
  };
  const staged = await client.stageAsset({ session, content_blob: blob }, bytes);
  assert.equal(staged.outcome, "success");
  assert.equal(staged.payload.asset.blob.lifetime, "persistent");
  const autosave = await client.controlAutosave({ session, action: "cancel" });
  assert.equal(autosave.outcome, "success");
  assert.equal(autosave.payload.scheduled, false);
  assert.equal((await client.recoverOperations({ document_id: documentId })).outcome, "success");
  assert.equal((await client.closeDocument({ session })).outcome, "success");

  await client.restart();
  assert.equal(client.state, "ready");
  assert.equal(processes.length, 4);
  const reopened = await client.openDocument({ document_id: documentId });
  assert.equal(reopened.outcome, "success");
});

test("local host reports a crashed process and can explicitly restart", async (context) => {
  const processes = [];
  const client = await createLocalHostClient({
    binaryPath, storageRoot: join(temporary, "crash-storage"),
    expectedReleaseManifestDigest: releaseManifestDigest,
    processLifecycle: recordingLifecycle(processes),
  });
  context.after(() => client.dispose());
  const runtime = processes.find((record) => record.arguments[0] === "stdio");
  assert.notEqual(runtime, undefined);
  runtime.child.kill(processCrashFault.injection);
  await new Promise((resolve) => runtime.child.once("exit", resolve));
  await assert.rejects(client.recoverOperations({ document_id: "missing" }));
  assert.equal(client.state, processCrashFault.expected_client_state);
  await client.restart();
  assert.equal(client.state, processCrashFault.expected_restart_state);
  const opened = await client.openDocument({
    document_id: "bootstrap",
    local_source: { kind: "project", path: projectRoot, entry_path: "document.ldl" },
  });
  assert.equal(opened.outcome, "success");
});
