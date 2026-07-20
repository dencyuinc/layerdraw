// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {createHash} from "node:crypto";
import {readFile, writeFile} from "node:fs/promises";
import {join} from "node:path";
import {pathToFileURL} from "node:url";
import {Worker} from "node:worker_threads";

import {createBrowserEditor} from "../../../packages/client-sdk/dist/browser-editor.js";
import {createWasmEngineClient} from "../../../packages/engine-client/dist/wasm.js";

const artifactDirectory = process.cwd();

const digest = (value) => `sha256:${createHash("sha256").update(value).digest("hex")}`;
const manifestBytes = await readFile(join(artifactDirectory, "engine-wasm.manifest.json"));
const artifactManifest = JSON.parse(manifestBytes);
const artifactManifestDigest = digest(manifestBytes);
const expectedEngineRelease = process.argv[2] ?? artifactManifest.build.release_version;
const releaseManifestDigest = `sha256:${"5".repeat(64)}`;
const packageAuthorityPath = join(artifactDirectory, "package-authority.json");
await writeFile(packageAuthorityPath, `${JSON.stringify({name: "@layerdraw/engine-wasm", version: artifactManifest.build.release_version})}\n`);
let exchangeSequence = 0;

class BrowserWorkerAdapter {
  #worker;
  #listeners = new Map();

  constructor(_moduleURL, options) {
    this.#worker = new Worker(new URL("../../../packages/engine-wasm/test/fixtures/node-worker-entry.mjs", import.meta.url), {
      name: options.name,
      workerData: {
        artifactBaseURL: `${pathToFileURL(artifactDirectory).href}/`,
        packageManifestURL: pathToFileURL(packageAuthorityPath).href,
      },
    });
  }

  postMessage(message, transfer) { this.#worker.postMessage(message, [...transfer]); }
  addEventListener(type, listener) {
    const wrapper = type === "message" ? (data) => listener({data}) : () => listener();
    const listeners = this.#listeners.get(type) ?? new Map();
    listeners.set(listener, wrapper);
    this.#listeners.set(type, listeners);
    this.#worker.on(type, wrapper);
  }
  removeEventListener(type, listener) {
    const wrapper = this.#listeners.get(type)?.get(listener);
    if (wrapper !== undefined) this.#worker.off(type, wrapper);
  }
  terminate() { return this.#worker.terminate(); }
}

function nextMessage(worker, predicate) {
  return new Promise((resolve, reject) => {
    const onMessage = (message) => {
      if (!predicate(message)) return;
      cleanup();
      resolve(message);
    };
    const onError = () => {
      cleanup();
      reject(new Error("Worker failed"));
    };
    const cleanup = () => {
      worker.off("message", onMessage);
      worker.off("error", onError);
    };
    worker.on("message", onMessage);
    worker.on("error", onError);
  });
}

async function createEndpoint(generation) {
  const worker = new Worker(new URL("./wasm_bridge_worker.mjs", import.meta.url), {
    workerData: {artifactDirectory},
  });
  const readyPromise = nextMessage(worker, (message) => message.kind === "ready" || message.kind === "fatal");
  worker.postMessage({
    kind: "init",
    endpoint_generation: generation,
    expected_artifact_manifest_digest: artifactManifestDigest,
    release_manifest_digest: releaseManifestDigest,
  });
  const ready = await readyPromise;
  if (ready.kind !== "ready" || !ready.initialized.ok) {
    await worker.terminate();
    throw new Error("real artifact Worker initialization failed");
  }
  return {worker, generation, ready};
}

async function request(endpoint, control, blobIDs = [], blobs = [], generation = endpoint.generation) {
  exchangeSequence += 1;
  const exchangeID = `node-exchange-${exchangeSequence}`;
  const accepted = nextMessage(endpoint.worker, (message) => message.kind === "accepted" && message.exchange_id === exchangeID);
  const response = nextMessage(endpoint.worker, (message) => message.kind === "response" && message.exchange_id === exchangeID);
  endpoint.worker.postMessage({kind: "request", endpoint_generation: generation, exchange_id: exchangeID, control, blob_ids: blobIDs, blobs}, [control, ...blobs]);
  await accepted;
  return (await response).result;
}

const encode = (value) => new TextEncoder().encode(JSON.stringify(value)).buffer;
const decode = (buffer) => JSON.parse(new TextDecoder().decode(new Uint8Array(buffer)));
const handshakeControl = (schemaDigest, requestID) => encode({
  operation: "engine.handshake",
  payload: {
    client_release: "0.0.0-dev",
    protocols: [{
      name: "engine",
      supported_range: "1.0..1.0",
      versions: [{version: "1.0", schema_digest: schemaDigest}],
    }],
    required_capabilities: ["engine.compile"],
    optional_capabilities: [],
  },
  protocol: {name: "engine", version: "1.0"},
  request_id: requestID,
});

function compileControl(sourceBytes, requestID) {
  const sourceDigest = digest(sourceBytes);
  return encode({
    operation: "engine.compile",
    payload: {
      entry_path: "main.ldl",
      installed_pack_tree: [],
      mode: "project",
      project_source_tree: [{
        path: "main.ldl",
        blob: {
          blob_id: "source",
          digest: sourceDigest,
          lifetime: "request",
          media_type: "text/plain; charset=utf-8",
          size: String(sourceBytes.byteLength),
        },
      }],
      referenced_assets: [],
      resolved_dependencies: {
        format: "layerdraw-resolved",
        format_version: 1,
        installs: [],
        language: 1,
      },
      resource_limits: {},
    },
    protocol: {name: "engine", version: "1.0"},
    request_id: requestID,
  });
}

async function handshakeAndCompile(endpoint, suffix) {
  const initialized = endpoint.ready.initialized;
  const handshake = await request(endpoint, handshakeControl(initialized.protocol_schema_digest, `node-handshake-${suffix}`));
  const envelope = handshake.ok ? decode(handshake.control) : undefined;
  if (!handshake.ok || envelope.outcome !== "success") throw new Error("generated handshake failed");
  if (envelope.engine_release !== expectedEngineRelease || envelope.payload.host_release !== expectedEngineRelease) throw new Error("Go/WASM engine release differs from the artifact/package authority");
  if (!/^wasm-[0-9a-f]{32}$/.test(envelope.payload.endpoint_instance_id)) throw new Error("endpoint identity was not minted inside Go/WASM");
  if (envelope.payload.release_manifest_digest !== releaseManifestDigest) throw new Error("verified release pin did not reach the descriptor");

  const sourceBytes = new TextEncoder().encode('project p "Project" {}');
  const compile = await request(endpoint, compileControl(sourceBytes, `node-compile-${suffix}`), ["source"], [sourceBytes.buffer]);
  const compileEnvelope = compile.ok ? decode(compile.control) : undefined;
  if (!compile.ok || compileEnvelope.outcome !== "success" || compile.blobs.length < 2) {
    throw new Error(`real Project compile failed: ${JSON.stringify({
      ok: compile.ok,
      failure: compile.failure,
      envelope: compileEnvelope,
      blob_ids: compile.blob_ids,
      blob_count: compile.blobs?.length,
    })}`);
  }
  if (compile.blob_ids.length !== compile.blobs.length || compile.blobs.some((buffer) => !(buffer instanceof ArrayBuffer))) throw new Error("output ownership table is invalid");
  return envelope.payload.endpoint_instance_id;
}

function portableRequest(source) {
  const bytes = new TextEncoder().encode(source);
  const ref = {
    blob_id: "compile/source/document.ldl", digest: digest(bytes), lifetime: "request",
    media_type: "text/plain; charset=utf-8", size: String(bytes.byteLength),
  };
  return {
    input: {
      entry_path: "document.ldl", installed_pack_tree: [], mode: "project",
      project_source_tree: [{blob: ref, path: "document.ldl"}], referenced_assets: [],
      resolved_dependencies: {format: "layerdraw-resolved", format_version: 1, installs: [], language: 1},
      resource_limits: {},
    },
    blobs: [{ref, bytes}],
  };
}

function semanticPreconditions(compiled, documentGeneration) {
  return {
    document_generation: documentGeneration,
    expected_child_sets: compiled.child_set_hashes.map(({owner_address, child_kind, hash}) => ({owner_address, child_kind, hash})),
    expected_source_digests: compiled.source_map.files.map(({digest: sourceDigest, module_path, origin}) => ({digest: sourceDigest, module: {module_path, origin}})),
    expected_subject_hashes: compiled.subject_semantic_hashes.map(({address, hash}) => ({address, hash})),
    expected_subtree_hashes: compiled.subtree_hashes.map(({owner_address, hash}) => ({address: owner_address, hash})),
  };
}

async function browserEditorRealWasmConformance() {
  const OriginalWorker = globalThis.Worker;
  globalThis.Worker = BrowserWorkerAdapter;
  let client;
  let editor;
  try {
    client = await createWasmEngineClient({
      client: {expectedReleaseManifestDigest: releaseManifestDigest},
      expectedArtifactManifestDigest: artifactManifestDigest,
    });
    const portable = portableRequest('project p "Project" {}');
    const compilation = await client.compile({input: portable.input, blobs: portable.blobs});
    if (compilation.outcome !== "success") throw new Error("real WASM BrowserEditor fixture did not compile");
    editor = createBrowserEditor({
      engine_client: client,
      asset_resolver: {resolve: async () => new Uint8Array(), put: async () => "unused", describeCapability: () => ({})},
    });
    const opened = await editor.open({
      authority: "engine",
      input: {compile_input: portable.input, requested_limits: {max_items: "10000", max_output_bytes: "67108864"}},
      blobs: portable.blobs,
    });
    if (opened.persistence !== "ephemeral") throw new Error("local Engine claimed durable persistence");
    const edit = {
      kind: "semantic_operations",
      request: {
        batch: {operations: [{operation: "create_subject", subject_kind: "layer", parent_address: "ldl:project:p", id: "wasm_browser_editor", fields: {display_name: "WASM Browser Editor", order: "1"}}]},
        limits: {max_items: "10000", max_output_bytes: "67108864"},
        preconditions: semanticPreconditions(compilation.response.payload, opened.session.document_generation),
      },
    };
    const preview = await editor.preview(edit);
    if (preview.authority !== "engine" || preview.preview.status !== "valid") throw new Error("real WASM BrowserEditor preview failed");
    const applied = await editor.apply(edit);
    if (applied.persistence !== "ephemeral" || "committed_revision" in applied) throw new Error("local Engine apply claimed a Runtime revision");
    await editor.close();
  } finally {
    await editor?.close().catch(() => undefined);
    await client?.dispose().catch(() => undefined);
    globalThis.Worker = OriginalWorker;
  }
}

const first = await createEndpoint("node-generation-1");
const expectedLimitKeys = [
  "max_blob_id_bytes", "max_buffers", "max_control_bytes", "max_control_depth",
  "max_input_blob_bytes", "max_input_total_bytes", "max_output_blob_bytes",
  "max_output_total_bytes", "max_response_publish_bytes",
];
if (first.ready.artifact_manifest_digest !== artifactManifestDigest || JSON.stringify(Object.keys(first.ready.initialized.transport_limits).sort()) !== JSON.stringify(expectedLimitKeys)) {
  throw new Error("artifact identity or exact transport-limit shape drifted");
}
const firstEndpointID = await handshakeAndCompile(first, "first");
const stale = await request(first, encode({}), [], [], "stale-generation");
if (stale.ok || stale.failure.code !== "engine.worker.stale_generation") throw new Error("stale generation was accepted");
await first.worker.terminate();

// Post Accepted, enter a deliberately slow maximum-size Go/WASM decode, then
// terminate from outside. No same-Worker cancel/dispose message is involved.
const blocked = await createEndpoint("node-generation-blocked");
const blockedHandshakeID = await handshakeAndCompile(blocked, "blocked");
exchangeSequence += 1;
const blockedExchangeID = `node-exchange-${exchangeSequence}`;
let lateResponse = false;
blocked.worker.on("message", (message) => {
  if (message.kind === "response" && message.exchange_id === blockedExchangeID) lateResponse = true;
});
const accepted = nextMessage(blocked.worker, (message) => message.kind === "accepted" && message.exchange_id === blockedExchangeID);
const slowControl = new ArrayBuffer(8_388_608);
const slowView = new Uint8Array(slowControl);
slowView.fill(0x20);
slowView[slowView.length - 1] = 0x7b;
blocked.worker.postMessage({kind: "request", endpoint_generation: blocked.generation, exchange_id: blockedExchangeID, control: slowControl, blob_ids: [], blobs: []}, [slowControl]);
await accepted;
await blocked.worker.terminate();
await new Promise((resolve) => setImmediate(resolve));
if (lateResponse) throw new Error("terminated generation published a late response");

const replacement = await createEndpoint("node-generation-replacement");
const replacementEndpointID = await handshakeAndCompile(replacement, "replacement");
if (replacementEndpointID === firstEndpointID || replacementEndpointID === blockedHandshakeID) throw new Error("replacement reused endpoint identity");
const disposeResponse = nextMessage(replacement.worker, (message) => message.kind === "disposed");
replacement.worker.postMessage({kind: "dispose", endpoint_generation: replacement.generation});
await disposeResponse;
await replacement.worker.terminate();

await browserEditorRealWasmConformance();

process.stdout.write("Go WASM real-artifact Worker handshake/compile/BrowserEditor/hard-cancel/replacement smoke passed.\n");
