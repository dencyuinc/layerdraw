// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import {chmod, cp, mkdtemp, readFile, rm, writeFile} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {pathToFileURL} from "node:url";
import test from "node:test";

import {EngineWorkerTransportError} from "../dist/index.js";
import {createEngineWorkerTransportWithFactory} from "../dist/host.js";
import {
  assertExportPlanTransportGolden,
  encode,
  executeViewDataTransportCorpus,
  handshakeAndCompileCorpus,
  handshakeControl,
  performRequest,
  projectCompileCase,
  releaseManifestDigest,
  sha256,
} from "./shared/real-engine.mjs";
import {NodeWorkerAdapter} from "./node-worker-adapter.mjs";

const workerModuleURL = new URL("./fixtures/node-worker-entry.mjs", import.meta.url).href;
const manifestBytes = await readFile(new URL("../dist/engine-wasm.manifest.json", import.meta.url));
const manifestBuffer = manifestBytes.buffer.slice(manifestBytes.byteOffset, manifestBytes.byteOffset + manifestBytes.byteLength);
const artifactManifestDigest = await sha256(manifestBuffer);
const artifactManifest = JSON.parse(manifestBytes);
const parityCorpus = JSON.parse(await readFile(new URL("../../../tests/conformance/testdata/engine_compile_parity_v1.json", import.meta.url), "utf8"));
const viewDataCorpus = JSON.parse(await readFile(new URL("../../../tests/conformance/testdata/viewdata_conformance_v1.json", import.meta.url), "utf8"));
const exportPlanGolden = JSON.parse(await readFile(new URL("../../../schemas/fixtures/conformance/export-plan-transport-parity-v1.json", import.meta.url), "utf8"));
const expectedLimitKeys = [
  "max_blob_id_bytes", "max_buffers", "max_control_bytes", "max_control_depth",
  "max_input_blob_bytes", "max_input_total_bytes", "max_output_blob_bytes",
  "max_output_total_bytes", "max_response_publish_bytes",
];

function createEndpoint(endpointGeneration) {
  let adapter;
  const transport = createEngineWorkerTransportWithFactory({
    endpointGeneration,
    expectedArtifactManifestDigest: artifactManifestDigest,
    releaseManifestDigest,
    disposeTimeoutMilliseconds: 2_000,
  }, workerModuleURL, (url, options) => {
    adapter = new NodeWorkerAdapter(url, options);
    return adapter;
  });
  return {transport, get adapter() { return adapter; }};
}

function isFailure(code) {
  return (error) => error instanceof EngineWorkerTransportError && error.failure.code === code;
}

async function createMutatedAuthorityFixture(prefix, mutate, packageVersion = artifactManifest.build.release_version) {
  const temporary = await mkdtemp(join(tmpdir(), prefix));
  const artifactRoot = join(temporary, "dist");
  await cp(new URL("../dist/", import.meta.url), artifactRoot, {recursive: true});
  const manifestPath = join(artifactRoot, "engine-wasm.manifest.json");
  const sbomPath = join(artifactRoot, "engine-wasm.cdx.json");
  const authorityPath = join(artifactRoot, "engine-wasm.authority.json");
  const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
  const sbom = JSON.parse(await readFile(sbomPath, "utf8"));
  const authority = JSON.parse(await readFile(authorityPath, "utf8"));
  const mutation = await mutate(manifest, sbom, authority);
  const sbomChanged = mutation === true || mutation?.sbom === true;
  const authorityChanged = mutation?.authority === true;
  if (sbomChanged) {
    const sbomBytes = Buffer.from(`${JSON.stringify(sbom)}\n`);
    await writeFile(sbomPath, sbomBytes);
    const entry = manifest.files.find((file) => file.path === "engine-wasm.cdx.json");
    entry.size = sbomBytes.byteLength;
    entry.digest = await sha256(sbomBytes.buffer.slice(sbomBytes.byteOffset, sbomBytes.byteOffset + sbomBytes.byteLength));
  }
  if (authorityChanged) {
    const authorityBytes = Buffer.from(`${JSON.stringify(authority)}\n`);
    await writeFile(authorityPath, authorityBytes);
    const authorityDigest = await sha256(authorityBytes.buffer.slice(authorityBytes.byteOffset, authorityBytes.byteOffset + authorityBytes.byteLength));
    const oldDigest = manifest.sbom_authority.digest;
    const entry = manifest.files.find((file) => file.path === "engine-wasm.authority.json");
    entry.size = authorityBytes.byteLength;
    entry.digest = authorityDigest;
    manifest.sbom_authority.digest = authorityDigest;
    manifest.build.flags[2] = manifest.build.flags[2].replace(`main.sbomAuthorityDigest=${oldDigest}`, `main.sbomAuthorityDigest=${authorityDigest}`);
  }
  const mutatedManifestBytes = Buffer.from(`${JSON.stringify(manifest)}\n`);
  await writeFile(manifestPath, mutatedManifestBytes);
  const packageManifestPath = join(temporary, "package.json");
  await writeFile(packageManifestPath, `${JSON.stringify({name: "@layerdraw/engine-wasm", version: packageVersion})}\n`);
  const buffer = mutatedManifestBytes.buffer.slice(mutatedManifestBytes.byteOffset, mutatedManifestBytes.byteOffset + mutatedManifestBytes.byteLength);
  return {
    temporary,
    artifactBaseURL: `${pathToFileURL(artifactRoot).href}/`,
    packageManifestURL: pathToFileURL(packageManifestPath).href,
    artifactManifestDigest: await sha256(buffer),
  };
}

async function assertLinkedAuthorityRejected(fixture, endpointGeneration) {
  let adapter;
  const transport = createEngineWorkerTransportWithFactory({
    endpointGeneration,
    expectedArtifactManifestDigest: fixture.artifactManifestDigest,
    releaseManifestDigest,
    disposeTimeoutMilliseconds: 2_000,
  }, workerModuleURL, (url, options) => {
    adapter = new NodeWorkerAdapter(url, options, {
      artifactBaseURL: fixture.artifactBaseURL,
      packageManifestURL: fixture.packageManifestURL,
    });
    return adapter;
  });
  await assert.rejects(transport.ready, isFailure("engine.worker.initialization_failed"));
  await transport.dispose();
  await adapter.exited;
}

test("real Node worker_threads owns the packaged Go/WASM lifecycle", {timeout: 120_000}, async () => {
  const exits = [];
  const first = createEndpoint("node-real-generation-1");
  exits.push(first.adapter.exited);
  const limits = await first.transport.ready;
  assert.deepEqual(Object.keys(limits).sort(), expectedLimitKeys);

  const staleControl = encode({});
  const staleFailure = first.adapter.nextMessage((message) => message.kind === "transport_failure" && message.exchange_id === "node-stale-exchange");
  first.adapter.postMessage({
    worker_protocol: "layerdraw.engine_worker",
    worker_protocol_version: 1,
    kind: "request",
    endpoint_generation: "node-stale-generation",
    exchange_id: "node-stale-exchange",
    control: staleControl,
    blobs: [],
  }, [staleControl]);
  assert.equal(staleControl.byteLength, 0);
  assert.deepEqual((await staleFailure).failure, {code: "engine.worker.stale_generation", phase: "lifecycle", retryable: true});

  const firstEndpointID = await handshakeAndCompileCorpus(first.transport, artifactManifest.protocol.schema_digest, parityCorpus, artifactManifest.build.release_version, "node-first");
  await assertExportPlanTransportGolden(first.transport, exportPlanGolden, artifactManifest.build.release_version, "node-first");
  const viewDataCases = await executeViewDataTransportCorpus(first.transport, artifactManifest.protocol.schema_digest, viewDataCorpus, artifactManifest.build.release_version, "node-first", true);
  assert.equal(viewDataCases.length, 18);
  const firstDispose = first.transport.dispose();
  assert.equal(firstDispose, first.transport.dispose());
  await firstDispose;

  const cancelled = createEndpoint("node-real-generation-cancelled");
  exits.push(cancelled.adapter.exited);
  await cancelled.transport.ready;
  const slowControl = new ArrayBuffer(8_388_608);
  new Uint8Array(slowControl).fill(0x20);
  const slow = cancelled.transport.request({exchangeID: "node-cancelled-exchange", control: slowControl, blobs: []});
  assert.equal(slowControl.byteLength, 0);
  await slow.accepted;
  cancelled.transport.terminate();
  await assert.rejects(slow.response, isFailure("engine.worker.terminated_by_caller"));
  await cancelled.transport.dispose();

  const replacement = createEndpoint("node-real-generation-replacement");
  exits.push(replacement.adapter.exited);
  await replacement.transport.ready;
  const replacementEndpointID = await handshakeAndCompileCorpus(replacement.transport, artifactManifest.protocol.schema_digest, parityCorpus, artifactManifest.build.release_version, "node-replacement");
  assert.notEqual(replacementEndpointID, firstEndpointID);
  await replacement.transport.dispose();

  const crashed = createEndpoint("node-real-generation-crashed");
  exits.push(crashed.adapter.exited);
  await crashed.transport.ready;
  crashed.adapter.crashForTest();
  await crashed.adapter.exited;
  assert.throws(
    () => crashed.transport.request({exchangeID: "after-crash", control: encode({}), blobs: []}),
    isFailure("engine.worker.crashed"),
  );
  await crashed.transport.dispose();

  await Promise.all(exits);
});

test("Node executes the verified wasm_exec byte snapshot after its source path changes", {timeout: 120_000}, async () => {
  const temporary = await mkdtemp(join(tmpdir(), "layerdraw-engine-wasm-snapshot-"));
  const artifactRoot = join(temporary, "dist");
  await cp(new URL("../dist/", import.meta.url), artifactRoot, {recursive: true});
  await chmod(join(artifactRoot, "wasm_exec.js"), 0o644);
  let adapter;
  try {
    const transport = createEngineWorkerTransportWithFactory({
      endpointGeneration: "node-verified-snapshot-race",
      expectedArtifactManifestDigest: artifactManifestDigest,
      releaseManifestDigest,
      disposeTimeoutMilliseconds: 2_000,
    }, workerModuleURL, (url, options) => {
      adapter = new NodeWorkerAdapter(url, options, {
        artifactBaseURL: `${pathToFileURL(artifactRoot).href}/`,
        replaceWasmExecAfterRead: true,
      });
      return adapter;
    });
    await transport.ready;
    assert.match(await readFile(join(artifactRoot, "wasm_exec.js"), "utf8"), /__layerdrawUnverifiedWasmExecRan/);
    assert.equal(await handshakeAndCompileCorpus(transport, artifactManifest.protocol.schema_digest, parityCorpus, artifactManifest.build.release_version, "node-snapshot-race").then(() => true), true);
    await transport.dispose();
    await adapter.exited;
  } finally {
    adapter?.terminate();
    await rm(temporary, {recursive: true, force: true});
  }
});

test("Ready is blocked by linked Go release authority after a self-consistent package, manifest, and SBOM forgery", {timeout: 120_000}, async () => {
  const forgedVersion = "9.9.9";
  const fixture = await createMutatedAuthorityFixture("layerdraw-engine-wasm-linked-release-", (manifest, sbom) => {
    const oldRef = `pkg:npm/%40layerdraw/engine-wasm@${manifest.build.release_version}`;
    const forgedRef = `pkg:npm/%40layerdraw/engine-wasm@${forgedVersion}`;
    manifest.build.flags[2] = manifest.build.flags[2].replace(`main.releaseVersion=${manifest.build.release_version}`, `main.releaseVersion=${forgedVersion}`);
    manifest.build.release_version = forgedVersion;
    sbom.metadata.component.version = forgedVersion;
    sbom.metadata.component.purl = forgedRef;
    sbom.metadata.component["bom-ref"] = forgedRef;
    sbom.dependencies.find((entry) => entry.ref === oldRef).ref = forgedRef;
    return true;
  }, forgedVersion);
  try {
    await assertLinkedAuthorityRejected(fixture, "node-linked-release-mismatch");
  } finally {
    await rm(fixture.temporary, {recursive: true, force: true});
  }
});

test("Ready is blocked when the verified manifest schema digest differs from generated Go authority", {timeout: 120_000}, async () => {
  const fixture = await createMutatedAuthorityFixture("layerdraw-engine-wasm-schema-authority-", (manifest) => {
    manifest.protocol.schema_digest = `sha256:${"0".repeat(64)}`;
    return false;
  });
  try {
    await assertLinkedAuthorityRejected(fixture, "node-schema-authority-mismatch");
  } finally {
    await rm(fixture.temporary, {recursive: true, force: true});
  }
});

test("Ready is blocked by linked legal authority after a self-consistent generated authority, manifest, SBOM, and digest forgery", {timeout: 120_000}, async () => {
  const fixture = await createMutatedAuthorityFixture("layerdraw-engine-wasm-legal-authority-", (_manifest, sbom, authority) => {
    authority.sbom_components[0].licenses[0].license.id = "MIT";
    sbom.components[0].licenses[0].license.id = "MIT";
    return {sbom: true, authority: true};
  });
  try {
    await assertLinkedAuthorityRejected(fixture, "node-legal-authority-mismatch");
  } finally {
    await rm(fixture.temporary, {recursive: true, force: true});
  }
});

test("Go bridge rejects an outer attachment size mismatch before attachment acquisition", {timeout: 120_000}, async () => {
  const endpoint = createEndpoint("node-attachment-bind-mismatch");
  await endpoint.transport.ready;
  await performRequest(endpoint.transport, "node-bind-handshake-exchange", {
    control: handshakeControl(artifactManifest.protocol.schema_digest, "node-bind-handshake-request"),
    blobs: [],
  });
  const input = await projectCompileCase("node-bind-project-request");
  const wrongBytes = new ArrayBuffer(1);
  const exchange = endpoint.transport.request({
    exchangeID: "node-bind-project-exchange",
    control: input.control,
    blobs: [{blob_id: input.blobs[0].blob_id, bytes: wrongBytes}],
  });
  assert.equal(wrongBytes.byteLength, 0);
  await exchange.accepted;
  await assert.rejects(exchange.response, isFailure("engine.worker.transfer_failed"));
  await endpoint.transport.dispose();
  await endpoint.adapter.exited;
});
