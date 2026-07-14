// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {
  browserTransportLimits,
  failure,
  failureDefinitions,
  hasExactKeys,
  hasInitialTransportLimits,
  isArrayRecord,
  isBoundedOpaqueString,
  isDigest,
  isFixedArrayBuffer,
  isPlainRecord,
  maxEndpointGenerationBytes,
  transportLimitKeys,
  validateEndpointResponse,
  validateFailure,
  workerProtocol,
  workerProtocolVersion,
  type EngineWorkerBlob,
  type EngineWorkerFailure,
  type EngineWorkerTransportLimits,
} from "./protocol.js";
import {
  EngineEndpointInitializationError,
  type EngineByteEndpoint,
  type EngineByteEndpointInit,
  type EngineByteEndpointResult,
} from "./worker-runtime.js";

const expectedWasmExecDigest = "sha256:0c949f4996f9a89698e4b5c586de32249c3b69b7baadb64d220073cc04acba14";
const requiredArtifactFiles = Object.freeze([
  "LICENSE",
  "LICENSING.md",
  "NOTICE",
  "THIRD_PARTY_NOTICES.txt",
  "engine-wasm-worker-v1.json",
  "engine-wasm.cdx.json",
  "layerdraw-engine.wasm",
  "licenses/Apache-2.0.txt",
  "wasm_exec.js",
]);

export interface VerifiedArtifactLoaderOptions {
  readonly artifactBaseURL: string;
  readonly loadBytes?: (url: string) => Promise<ArrayBuffer>;
}

interface ArtifactFile {
  readonly path: string;
  readonly media_type: string;
  readonly size: number;
  readonly digest: string;
}

interface WasmBridge {
  readonly workerProtocol: string;
  readonly workerProtocolVersion: number;
  initialize(endpointGeneration: string, releaseManifestDigest: string): unknown;
  request(endpointGeneration: string, control: ArrayBuffer, blobIDs: string[], blobs: ArrayBuffer[]): unknown;
  dispose(endpointGeneration: string): unknown;
}

interface GoRuntime {
  readonly importObject: WebAssembly.Imports;
  run(instance: WebAssembly.Instance): Promise<void>;
}

interface GoRuntimeConstructor {
  new(): GoRuntime;
}

function initializationFailure(): never {
  throw new EngineEndpointInitializationError("engine.worker.initialization_failed");
}

function artifactMismatch(): never {
  throw new EngineEndpointInitializationError("engine.worker.artifact_mismatch");
}

function snapshotBytes(value: unknown): ArrayBuffer {
  if (!isFixedArrayBuffer(value)) initializationFailure();
  const snapshot = value.slice(0);
  if (!isFixedArrayBuffer(snapshot)) initializationFailure();
  return snapshot;
}

async function defaultLoadBytes(url: string): Promise<ArrayBuffer> {
  const fetchValue = Reflect.get(globalThis, "fetch");
  if (typeof fetchValue !== "function") initializationFailure();
  const response = await Reflect.apply(fetchValue, globalThis, [url]) as {ok?: unknown; arrayBuffer?: unknown};
  if (response.ok !== true || typeof response.arrayBuffer !== "function") initializationFailure();
  const value = await Reflect.apply(response.arrayBuffer, response, []) as unknown;
  if (!isFixedArrayBuffer(value)) initializationFailure();
  return value;
}

async function sha256(value: ArrayBuffer): Promise<string> {
  const cryptoValue = Reflect.get(globalThis, "crypto") as {subtle?: {digest?: unknown}} | undefined;
  const subtle = cryptoValue?.subtle;
  const digestFunction = subtle?.digest;
  if (typeof digestFunction !== "function") initializationFailure();
  const raw = await Reflect.apply(digestFunction, subtle, ["SHA-256", value]) as unknown;
  if (!isFixedArrayBuffer(raw)) initializationFailure();
  const hex = [...new Uint8Array(raw)].map((byte) => byte.toString(16).padStart(2, "0")).join("");
  return `sha256:${hex}`;
}

function decodeJSON(value: ArrayBuffer): unknown {
  const decoder = Reflect.get(globalThis, "TextDecoder");
  if (typeof decoder !== "function") initializationFailure();
  const instance = Reflect.construct(decoder, ["utf-8", {fatal: true}]);
  const text = Reflect.apply(Reflect.get(instance, "decode"), instance, [new Uint8Array(value)]);
  if (typeof text !== "string") initializationFailure();
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return initializationFailure();
  }
}

const base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

function base64(value: ArrayBuffer): string {
  const bytes = new Uint8Array(value);
  let encoded = "";
  for (let offset = 0; offset < bytes.length; offset += 3) {
    const first = bytes[offset];
    if (first === undefined) initializationFailure();
    const second = bytes[offset + 1];
    const third = bytes[offset + 2];
    encoded += base64Alphabet.charAt(first >>> 2);
    encoded += base64Alphabet.charAt(((first & 0x03) << 4) | ((second ?? 0) >>> 4));
    encoded += second === undefined ? "=" : base64Alphabet.charAt(((second & 0x0f) << 2) | ((third ?? 0) >>> 6));
    encoded += third === undefined ? "=" : base64Alphabet.charAt(third & 0x3f);
  }
  return encoded;
}

interface VerifiedModuleResource {
  readonly url: string;
  release(): void;
}

function createVerifiedModuleResource(value: ArrayBuffer): VerifiedModuleResource {
  if (Reflect.get(globalThis, "self") === globalThis) {
    const BlobValue = Reflect.get(globalThis, "Blob") as typeof Blob | undefined;
    const URLValue = Reflect.get(globalThis, "URL") as typeof URL | undefined;
    if (typeof BlobValue !== "function" || typeof URLValue?.createObjectURL !== "function" || typeof URLValue.revokeObjectURL !== "function") {
      initializationFailure();
    }
    const blob = new BlobValue([value], {type: "text/javascript"});
    const url = URLValue.createObjectURL(blob);
    if (typeof url !== "string" || url.length === 0) initializationFailure();
    let released = false;
    return Object.freeze({
      url,
      release(): void {
        if (released) return;
        released = true;
        URLValue.revokeObjectURL(url);
      },
    });
  }
  const url = `data:text/javascript;base64,${base64(value)}`;
  return Object.freeze({url, release(): void { /* Data URLs have no external resource handle. */ }});
}

async function executeVerifiedJavaScript(value: ArrayBuffer): Promise<void> {
  const resource = createVerifiedModuleResource(value);
  try {
    await import(resource.url);
  } finally {
    resource.release();
  }
}

function isSafeArtifactPath(value: unknown): value is string {
  return typeof value === "string" && /^(?:[A-Za-z0-9._-]+\/)*[A-Za-z0-9._-]+$/.test(value) &&
    !value.split("/").some((part) => part === "." || part === "..");
}

function validateArtifactFiles(value: unknown): readonly ArtifactFile[] {
  if (!isArrayRecord(value) || value.length !== requiredArtifactFiles.length) artifactMismatch();
  const result: ArtifactFile[] = [];
  const seen = new Set<string>();
  for (const candidate of value) {
    if (!isPlainRecord(candidate) || !hasExactKeys(candidate, ["path", "media_type", "size", "digest"]) ||
        !isSafeArtifactPath(candidate.path) || typeof candidate.media_type !== "string" || candidate.media_type.length === 0 ||
        typeof candidate.size !== "number" || !Number.isSafeInteger(candidate.size) || candidate.size < 0 || !isDigest(candidate.digest) || seen.has(candidate.path)) artifactMismatch();
    seen.add(candidate.path);
    result.push(candidate as unknown as ArtifactFile);
  }
  const sorted = [...seen].sort();
  if (JSON.stringify(sorted) !== JSON.stringify([...requiredArtifactFiles].sort())) artifactMismatch();
  return result;
}

function exactStringArray(value: unknown, expected: readonly string[]): boolean {
  return isArrayRecord(value) && value.length === expected.length && expected.every((item, index) => value[index] === item);
}

function validateContractCorpus(value: unknown): EngineWorkerTransportLimits {
  const keys = ["spdx_license_identifier", "worker_protocol", "worker_protocol_version", "transport_id", "identifier_limits", "transport_limits", "failure_definitions", "outer_messages"];
  if (!isPlainRecord(value) || !hasExactKeys(value, keys) || value.spdx_license_identifier !== "Apache-2.0" ||
      value.worker_protocol !== workerProtocol || value.worker_protocol_version !== workerProtocolVersion || value.transport_id !== "wasm_worker") artifactMismatch();
  const identifiers = value.identifier_limits;
  if (!isPlainRecord(identifiers) || !hasExactKeys(identifiers, ["endpoint_generation_utf8_bytes", "exchange_id_utf8_bytes", "blob_id_utf8_bytes"]) ||
      identifiers.endpoint_generation_utf8_bytes !== 128 || identifiers.exchange_id_utf8_bytes !== 128 || identifiers.blob_id_utf8_bytes !== 256) artifactMismatch();
  if (!hasInitialTransportLimits(value.transport_limits)) artifactMismatch();
  if (!isArrayRecord(value.failure_definitions) || value.failure_definitions.length !== Object.keys(failureDefinitions).length) artifactMismatch();
  const failureCodes = Object.keys(failureDefinitions);
  for (let index = 0; index < failureCodes.length; index += 1) {
    const code = failureCodes[index];
    const entry = value.failure_definitions[index];
    if (code === undefined || !isPlainRecord(entry) || !hasExactKeys(entry, ["code", "phase", "retryable"]) || entry.code !== code || validateFailure(entry) === undefined) artifactMismatch();
  }
  const outer = value.outer_messages;
  if (!isPlainRecord(outer) || !hasExactKeys(outer, ["host_to_worker", "worker_to_host"]) || !isPlainRecord(outer.host_to_worker) || !isPlainRecord(outer.worker_to_host)) artifactMismatch();
  const host = outer.host_to_worker;
  const worker = outer.worker_to_host;
  if (!hasExactKeys(host, ["init", "request", "dispose"]) || !hasExactKeys(worker, ["ready", "accepted", "response", "transport_failure"]) ||
      !exactStringArray(host.init, ["worker_protocol", "worker_protocol_version", "kind", "endpoint_generation", "expected_artifact_manifest_digest", "release_manifest_digest"]) ||
      !exactStringArray(host.request, ["worker_protocol", "worker_protocol_version", "kind", "endpoint_generation", "exchange_id", "control", "blobs"]) ||
      !exactStringArray(host.dispose, ["worker_protocol", "worker_protocol_version", "kind", "endpoint_generation"]) ||
      !exactStringArray(worker.ready, ["worker_protocol", "worker_protocol_version", "kind", "endpoint_generation", "artifact_manifest_digest", "transport_limits"]) ||
      !exactStringArray(worker.accepted, ["worker_protocol", "worker_protocol_version", "kind", "endpoint_generation", "exchange_id"]) ||
      !exactStringArray(worker.response, ["worker_protocol", "worker_protocol_version", "kind", "endpoint_generation", "exchange_id", "control", "blobs"]) ||
      !exactStringArray(worker.transport_failure, ["worker_protocol", "worker_protocol_version", "kind", "endpoint_generation?", "exchange_id?", "failure"])) artifactMismatch();
  return value.transport_limits;
}

function validateManifest(value: unknown): readonly ArtifactFile[] {
  const keys = ["artifact_id", "artifact_manifest_version", "build", "protocol", "runtime_support", "files", "transport", "compiler_limits", "browser_contract", "licenses"];
  if (!isPlainRecord(value) || !hasExactKeys(value, keys) || value.artifact_id !== "@layerdraw/engine-wasm" || value.artifact_manifest_version !== 1) artifactMismatch();
  const runtime = value.runtime_support;
  if (!isPlainRecord(runtime) || !hasExactKeys(runtime, ["file", "go_version", "digest"]) || runtime.file !== "wasm_exec.js" || runtime.go_version !== "go1.26.5" || runtime.digest !== expectedWasmExecDigest) artifactMismatch();
  const transport = value.transport;
  if (!isPlainRecord(transport) || !hasExactKeys(transport, [
    "id", "worker_protocol", "worker_protocol_version", "contract_file", "endpoint_instance_id_provenance",
    "release_manifest_digest_provenance", "single_flight", "transfer", ...transportLimitKeys,
  ]) || transport.id !== "wasm_worker" || transport.worker_protocol !== workerProtocol ||
      transport.worker_protocol_version !== workerProtocolVersion || transport.contract_file !== "engine-wasm-worker-v1.json" ||
      transport.endpoint_instance_id_provenance !== "runtime_crypto_rand" || transport.release_manifest_digest_provenance !== "verified_worker_input" ||
      transport.single_flight !== true || transport.transfer !== "array_buffer") artifactMismatch();
  for (const key of transportLimitKeys) if (transport[key] !== browserTransportLimits[key]) artifactMismatch();
  return validateArtifactFiles(value.files);
}

function validateBridge(value: unknown): WasmBridge {
  if (!isPlainRecord(value) || value.workerProtocol !== workerProtocol || value.workerProtocolVersion !== workerProtocolVersion ||
      typeof value.initialize !== "function" || typeof value.request !== "function" || typeof value.dispose !== "function") initializationFailure();
  return value as unknown as WasmBridge;
}

function validateInitialized(value: unknown, generation: string): EngineWorkerTransportLimits {
  if (!isPlainRecord(value) || !hasExactKeys(value, ["ok", "endpoint_generation", "protocol_schema_digest", "transport_limits"]) ||
      value.ok !== true || value.endpoint_generation !== generation || !isDigest(value.protocol_schema_digest) || !hasInitialTransportLimits(value.transport_limits)) initializationFailure();
  return value.transport_limits;
}

function validateBridgeResult(value: unknown, generation: string, limits: EngineWorkerTransportLimits): EngineByteEndpointResult {
  const crashed = (): EngineByteEndpointResult => Object.freeze({ok: false, failure: failure("engine.worker.crashed")});
  if (!isPlainRecord(value) || typeof value.ok !== "boolean") return crashed();
  if (!value.ok) {
    if (!hasExactKeys(value, ["ok", "failure"])) return crashed();
    const validated = validateFailure(value.failure);
    return validated === undefined ? crashed() : Object.freeze({ok: false, failure: validated});
  }
  if (!hasExactKeys(value, ["ok", "endpoint_generation", "control", "blob_ids", "blobs"]) || value.endpoint_generation !== generation ||
      !isFixedArrayBuffer(value.control) || !isArrayRecord(value.blob_ids) || !isArrayRecord(value.blobs) || value.blob_ids.length !== value.blobs.length) {
    return crashed();
  }
  const blobs: EngineWorkerBlob[] = [];
  for (let index = 0; index < value.blob_ids.length; index += 1) {
    const blobID = value.blob_ids[index];
    const bytes = value.blobs[index];
    if (!isBoundedOpaqueString(blobID, limits.max_blob_id_bytes) || !isFixedArrayBuffer(bytes)) {
      return crashed();
    }
    blobs.push(Object.freeze({blob_id: blobID, bytes}));
  }
  const response = {control: value.control, blobs: Object.freeze(blobs)};
  const validatedResponse = validateEndpointResponse(response, limits);
  return validatedResponse === undefined ? crashed() :
    Object.freeze({ok: true, response: validatedResponse});
}

async function waitForBridge(): Promise<WasmBridge> {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    const value = Reflect.get(globalThis, "__layerdrawEngineWasmV1");
    if (value !== undefined) return validateBridge(value);
    await new Promise<void>((resolve) => {
      const setTimeoutValue = Reflect.get(globalThis, "setTimeout");
      if (typeof setTimeoutValue !== "function") initializationFailure();
      Reflect.apply(setTimeoutValue, globalThis, [resolve, 0]);
    });
  }
  return initializationFailure();
}

export async function createVerifiedWasmEndpoint(init: EngineByteEndpointInit, options: VerifiedArtifactLoaderOptions): Promise<EngineByteEndpoint> {
  if (!isBoundedOpaqueString(init.endpointGeneration, maxEndpointGenerationBytes) || !isDigest(init.expectedArtifactManifestDigest) ||
      !isDigest(init.releaseManifestDigest) || typeof options.artifactBaseURL !== "string" || options.artifactBaseURL.length === 0) initializationFailure();
  const loadBytes = options.loadBytes ?? defaultLoadBytes;
  const resolveURL = (path: string): string => new URL(path, options.artifactBaseURL).href;
  let manifestBytes: ArrayBuffer;
  try {
    manifestBytes = snapshotBytes(await loadBytes(resolveURL("engine-wasm.manifest.json")));
  } catch {
    return initializationFailure();
  }
  if (await sha256(manifestBytes) !== init.expectedArtifactManifestDigest) artifactMismatch();
  const files = validateManifest(decodeJSON(manifestBytes));
  const loaded = new Map<string, ArrayBuffer>();
  for (const file of files) {
    let bytes: ArrayBuffer;
    try {
      bytes = snapshotBytes(await loadBytes(resolveURL(file.path)));
    } catch {
      return artifactMismatch();
    }
    if (bytes.byteLength !== file.size || await sha256(bytes) !== file.digest) artifactMismatch();
    loaded.set(file.path, bytes);
  }
  const corpus = loaded.get("engine-wasm-worker-v1.json");
  const wasm = loaded.get("layerdraw-engine.wasm");
  const wasmExec = loaded.get("wasm_exec.js");
  if (corpus === undefined || wasm === undefined || wasmExec === undefined || await sha256(wasmExec) !== expectedWasmExecDigest) artifactMismatch();
  const limits = validateContractCorpus(decodeJSON(corpus));
  try {
    await executeVerifiedJavaScript(wasmExec);
    const GoValue = Reflect.get(globalThis, "Go") as GoRuntimeConstructor | undefined;
    if (typeof GoValue !== "function") initializationFailure();
    const go = new GoValue();
    const instantiated = await WebAssembly.instantiate(new Uint8Array(wasm), go.importObject);
    void go.run(instantiated.instance);
    const bridge = await waitForBridge();
    const initialized = bridge.initialize(init.endpointGeneration, init.releaseManifestDigest);
    const bridgeLimits = validateInitialized(initialized, init.endpointGeneration);
    if (!transportLimitKeys.every((key) => bridgeLimits[key] === limits[key])) initializationFailure();
    let disposed = false;
    const endpoint: EngineByteEndpoint = {
      artifactManifestDigest: init.expectedArtifactManifestDigest,
      transportLimits: bridgeLimits,
      request(input): EngineByteEndpointResult {
        if (disposed) return Object.freeze({ok: false, failure: failure("engine.worker.disposed")});
        const blobIDs = input.blobs.map((blob) => blob.blob_id);
        const blobBuffers = input.blobs.map((blob) => blob.bytes);
        return validateBridgeResult(bridge.request(init.endpointGeneration, input.control, blobIDs, blobBuffers), init.endpointGeneration, bridgeLimits);
      },
      dispose(): void {
        if (disposed) return;
        disposed = true;
        const result = bridge.dispose(init.endpointGeneration);
        if (!isPlainRecord(result) || !hasExactKeys(result, ["ok"]) || result.ok !== true) initializationFailure();
      },
    };
    return Object.freeze(endpoint);
  } catch (error) {
    if (error instanceof EngineEndpointInitializationError) throw error;
    return initializationFailure();
  }
}
