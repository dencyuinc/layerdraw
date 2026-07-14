// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {createEngineWorkerTransport, EngineWorkerTransportError} from "../../dist/index.js";
import {createEngineWorkerTransportWithFactory} from "../../dist/host.js";
import {encode, handshakeAndCompileProjectAndPack, releaseManifestDigest, sha256} from "../shared/real-engine.mjs";

const manifestResponse = await fetch("../../dist/engine-wasm.manifest.json", {cache: "no-store"});
if (!manifestResponse.ok) throw new Error("packaged artifact manifest was not served");
const manifestBytes = await manifestResponse.arrayBuffer();
const artifactManifestDigest = await sha256(manifestBytes);
const artifactManifest = JSON.parse(new TextDecoder().decode(manifestBytes));

function createTransport(endpointGeneration) {
  return createEngineWorkerTransport({
    endpointGeneration,
    expectedArtifactManifestDigest: artifactManifestDigest,
    releaseManifestDigest,
    disposeTimeoutMilliseconds: 2_000,
  });
}

function isFailure(error, code) {
  return error instanceof EngineWorkerTransportError && error.failure.code === code;
}

async function nextWorkerMessage(worker, predicate) {
  return new Promise((resolve, reject) => {
    const onMessage = (event) => {
      if (!predicate(event.data)) return;
      cleanup();
      resolve(event.data);
    };
    const onError = () => {
      cleanup();
      reject(new Error("worker failed before matching message"));
    };
    const cleanup = () => {
      worker.removeEventListener("message", onMessage);
      worker.removeEventListener("error", onError);
    };
    worker.addEventListener("message", onMessage);
    worker.addEventListener("error", onError);
  });
}

globalThis.runLayerDrawRealArtifactCorpus = async () => {
  const first = createTransport("browser-real-generation-1");
  const limits = await first.ready;
  const endpointID = await handshakeAndCompileProjectAndPack(first, artifactManifest.protocol.schema_digest, "browser-first");
  const dispose = first.dispose();
  if (dispose !== first.dispose()) throw new Error("dispose was not idempotent");
  await dispose;

  const cancelled = createTransport("browser-real-generation-cancelled");
  await cancelled.ready;
  const slowControl = new ArrayBuffer(8_388_608);
  new Uint8Array(slowControl).fill(0x20);
  const slow = cancelled.request({exchangeID: "browser-cancelled-exchange", control: slowControl, blobs: []});
  if (slowControl.byteLength !== 0) throw new Error("cancelled request did not transfer ownership");
  await slow.accepted;
  cancelled.terminate();
  try {
    await slow.response;
    throw new Error("terminated request unexpectedly published");
  } catch (error) {
    if (!isFailure(error, "engine.worker.terminated_by_caller")) throw error;
  }
  await cancelled.dispose();

  const replacement = createTransport("browser-real-generation-replacement");
  await replacement.ready;
  const replacementID = await handshakeAndCompileProjectAndPack(replacement, artifactManifest.protocol.schema_digest, "browser-replacement");
  if (replacementID === endpointID) throw new Error("replacement reused the Go/WASM endpoint identity");
  await replacement.dispose();
  return {limitKeys: Object.keys(limits).sort(), endpointID, replacementID};
};

globalThis.runLayerDrawDirectLifecycle = async () => {
  const worker = new Worker(new URL("../../dist/worker.js", import.meta.url), {type: "module"});
  const ready = nextWorkerMessage(worker, (message) => message.kind === "ready");
  worker.postMessage({
    worker_protocol: "layerdraw.engine_worker",
    worker_protocol_version: 1,
    kind: "init",
    endpoint_generation: "browser-direct-generation",
    expected_artifact_manifest_digest: artifactManifestDigest,
    release_manifest_digest: releaseManifestDigest,
  });
  await ready;
  const staleControl = encode({});
  const stale = nextWorkerMessage(worker, (message) => message.kind === "transport_failure" && message.exchange_id === "browser-stale-exchange");
  worker.postMessage({
    worker_protocol: "layerdraw.engine_worker",
    worker_protocol_version: 1,
    kind: "request",
    endpoint_generation: "browser-stale-generation",
    exchange_id: "browser-stale-exchange",
    control: staleControl,
    blobs: [],
  }, [staleControl]);
  const staleFailure = await stale;
  const disposed = nextWorkerMessage(worker, (message) => message.kind === "transport_failure" && message.failure?.code === "engine.worker.disposed");
  worker.postMessage({
    worker_protocol: "layerdraw.engine_worker",
    worker_protocol_version: 1,
    kind: "dispose",
    endpoint_generation: "browser-direct-generation",
  });
  await disposed;
  worker.terminate();

  let capturedWorker;
  const crashed = createEngineWorkerTransportWithFactory({
    endpointGeneration: "browser-crash-generation",
    expectedArtifactManifestDigest: artifactManifestDigest,
    releaseManifestDigest,
  }, new URL("./worker-entry.js", import.meta.url).href, (url, options) => {
    capturedWorker = new Worker(url, options);
    return capturedWorker;
  });
  await crashed.ready;
  const error = new Promise((resolve) => capturedWorker.addEventListener("error", (event) => {
    event.preventDefault();
    resolve();
  }, {once: true}));
  capturedWorker.postMessage({kind: "__layerdraw_test_crash"});
  await error;
  let crashCode;
  try {
    crashed.request({exchangeID: "after-crash", control: encode({}), blobs: []});
  } catch (caught) {
    crashCode = caught.failure?.code;
  }
  await crashed.dispose();
  return {staleFailure: staleFailure.failure, staleDetached: staleControl.byteLength, crashCode};
};

globalThis.runLayerDrawVerifiedSnapshotRace = async () => {
  const token = `snapshot-${crypto.randomUUID()}`;
  const worker = new Worker(new URL(`./race-worker-entry.js?token=${token}`, import.meta.url), {type: "module"});
  try {
    const ready = nextWorkerMessage(worker, (message) => message.kind === "ready");
    const resource = nextWorkerMessage(worker, (message) => message.kind === "__layerdraw_snapshot_resource");
    worker.postMessage({
      worker_protocol: "layerdraw.engine_worker",
      worker_protocol_version: 1,
      kind: "init",
      endpoint_generation: "browser-verified-snapshot-race",
      expected_artifact_manifest_digest: artifactManifestDigest,
      release_manifest_digest: releaseManifestDigest,
    });
    await ready;
    const resourceResult = await resource;
    const status = await fetch(`./race-status/${token}`, {cache: "no-store"}).then((response) => response.json());
    return {wasmExecReads: status.wasmExecReads, revoked: resourceResult.revoked};
  } finally {
    worker.terminate();
  }
};

globalThis.layerDrawHarnessReady = true;
