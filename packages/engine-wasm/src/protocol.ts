// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

export const workerProtocol = "layerdraw.engine_worker" as const;
export const workerProtocolVersion = 1 as const;

export const maxControlBytes = 8_388_608 as const;
export const maxControlDepth = 128 as const;
export const maxBlobIDBytes = 256 as const;
export const maxBuffers = 2_048 as const;
export const maxInputBlobBytes = 33_554_432 as const;
export const maxInputTotalBytes = 67_108_864 as const;
export const maxOutputBlobBytes = 33_554_432 as const;
export const maxOutputTotalBytes = 67_108_864 as const;
export const maxResponsePublishBytes = 75_497_472 as const;
export const maxEndpointGenerationBytes = 128 as const;
export const maxExchangeIDBytes = 128 as const;

const digestPattern = /^sha256:[0-9a-f]{64}$/;

export const transportLimitKeys = Object.freeze([
  "max_control_bytes",
  "max_control_depth",
  "max_blob_id_bytes",
  "max_buffers",
  "max_input_blob_bytes",
  "max_input_total_bytes",
  "max_output_blob_bytes",
  "max_output_total_bytes",
  "max_response_publish_bytes",
] as const);

export interface EngineWorkerBlob {
  readonly blob_id: string;
  readonly bytes: ArrayBuffer;
}

export interface EngineByteEndpointResponse {
  readonly control: ArrayBuffer;
  readonly blobs: readonly EngineWorkerBlob[];
}

export interface EngineWorkerRequestPayload {
  readonly exchangeID: string;
  readonly control: ArrayBuffer;
  readonly blobs: readonly EngineWorkerBlob[];
}

export interface EngineWorkerTransportLimits {
  readonly max_control_bytes: number;
  readonly max_control_depth: number;
  readonly max_blob_id_bytes: number;
  readonly max_buffers: number;
  readonly max_input_blob_bytes: number;
  readonly max_input_total_bytes: number;
  readonly max_output_blob_bytes: number;
  readonly max_output_total_bytes: number;
  readonly max_response_publish_bytes: number;
}

export const browserTransportLimits: Readonly<EngineWorkerTransportLimits> = Object.freeze({
  max_control_bytes: maxControlBytes,
  max_control_depth: maxControlDepth,
  max_blob_id_bytes: maxBlobIDBytes,
  max_buffers: maxBuffers,
  max_input_blob_bytes: maxInputBlobBytes,
  max_input_total_bytes: maxInputTotalBytes,
  max_output_blob_bytes: maxOutputBlobBytes,
  max_output_total_bytes: maxOutputTotalBytes,
  max_response_publish_bytes: maxResponsePublishBytes,
});

export type EngineWorkerFailureCode =
  | "engine.worker.unsupported"
  | "engine.worker.initialization_failed"
  | "engine.worker.artifact_mismatch"
  | "engine.worker.malformed_message"
  | "engine.worker.stale_generation"
  | "engine.worker.transfer_failed"
  | "engine.worker.crashed"
  | "engine.worker.terminated_by_caller"
  | "engine.worker.disposed";

export type EngineWorkerFailurePhase = "initialization" | "request" | "transfer" | "runtime" | "lifecycle";

export interface EngineWorkerFailure {
  readonly code: EngineWorkerFailureCode;
  readonly phase: EngineWorkerFailurePhase;
  readonly retryable: boolean;
}

export const failureDefinitions = Object.freeze({
  "engine.worker.unsupported": Object.freeze({phase: "initialization", retryable: false}),
  "engine.worker.initialization_failed": Object.freeze({phase: "initialization", retryable: false}),
  "engine.worker.artifact_mismatch": Object.freeze({phase: "initialization", retryable: false}),
  "engine.worker.malformed_message": Object.freeze({phase: "request", retryable: false}),
  "engine.worker.stale_generation": Object.freeze({phase: "lifecycle", retryable: true}),
  "engine.worker.transfer_failed": Object.freeze({phase: "transfer", retryable: false}),
  "engine.worker.crashed": Object.freeze({phase: "runtime", retryable: true}),
  "engine.worker.terminated_by_caller": Object.freeze({phase: "lifecycle", retryable: false}),
  "engine.worker.disposed": Object.freeze({phase: "lifecycle", retryable: false}),
} satisfies Readonly<Record<EngineWorkerFailureCode, Readonly<{phase: EngineWorkerFailurePhase; retryable: boolean}>>>);

export interface InitMessage {
  readonly worker_protocol: typeof workerProtocol;
  readonly worker_protocol_version: typeof workerProtocolVersion;
  readonly kind: "init";
  readonly endpoint_generation: string;
  readonly expected_artifact_manifest_digest: string;
  readonly release_manifest_digest: string;
}

export interface RequestMessage {
  readonly worker_protocol: typeof workerProtocol;
  readonly worker_protocol_version: typeof workerProtocolVersion;
  readonly kind: "request";
  readonly endpoint_generation: string;
  readonly exchange_id: string;
  readonly control: ArrayBuffer;
  readonly blobs: readonly EngineWorkerBlob[];
}

export interface DisposeMessage {
  readonly worker_protocol: typeof workerProtocol;
  readonly worker_protocol_version: typeof workerProtocolVersion;
  readonly kind: "dispose";
  readonly endpoint_generation: string;
}

export type HostToWorkerMessage = InitMessage | RequestMessage | DisposeMessage;

export interface ReadyMessage {
  readonly worker_protocol: typeof workerProtocol;
  readonly worker_protocol_version: typeof workerProtocolVersion;
  readonly kind: "ready";
  readonly endpoint_generation: string;
  readonly artifact_manifest_digest: string;
  readonly transport_limits: EngineWorkerTransportLimits;
}

export interface AcceptedMessage {
  readonly worker_protocol: typeof workerProtocol;
  readonly worker_protocol_version: typeof workerProtocolVersion;
  readonly kind: "accepted";
  readonly endpoint_generation: string;
  readonly exchange_id: string;
}

export interface ResponseMessage {
  readonly worker_protocol: typeof workerProtocol;
  readonly worker_protocol_version: typeof workerProtocolVersion;
  readonly kind: "response";
  readonly endpoint_generation: string;
  readonly exchange_id: string;
  readonly control: ArrayBuffer;
  readonly blobs: readonly EngineWorkerBlob[];
}

export interface TransportFailureMessage {
  readonly worker_protocol: typeof workerProtocol;
  readonly worker_protocol_version: typeof workerProtocolVersion;
  readonly kind: "transport_failure";
  readonly endpoint_generation?: string;
  readonly exchange_id?: string;
  readonly failure: EngineWorkerFailure;
}

export type WorkerToHostMessage = ReadyMessage | AcceptedMessage | ResponseMessage | TransportFailureMessage;

export function failure(code: EngineWorkerFailureCode): EngineWorkerFailure {
  const definition = failureDefinitions[code];
  return Object.freeze({code, phase: definition.phase, retryable: definition.retryable});
}

export function isPlainRecord(value: unknown): value is Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return false;
  const prototype = Object.getPrototypeOf(value);
  if (prototype !== Object.prototype && prototype !== null) return false;
  for (const key of Reflect.ownKeys(value)) {
    if (typeof key !== "string") return false;
    const descriptor = Object.getOwnPropertyDescriptor(value, key);
    if (descriptor === undefined || !("value" in descriptor) || !descriptor.enumerable) return false;
  }
  return true;
}

export function hasExactKeys(record: Record<string, unknown>, required: readonly string[], optional: readonly string[] = []): boolean {
  const allowed = new Set([...required, ...optional]);
  const keys = Object.keys(record);
  return keys.length >= required.length && keys.length <= allowed.size &&
    required.every((key) => Object.hasOwn(record, key)) && keys.every((key) => allowed.has(key));
}

export function isArrayRecord(value: unknown): value is readonly unknown[] {
  if (!Array.isArray(value) || Object.getPrototypeOf(value) !== Array.prototype) return false;
  const keys = Reflect.ownKeys(value);
  if (keys.length !== value.length + 1 || keys[keys.length - 1] !== "length") return false;
  for (let index = 0; index < value.length; index += 1) {
    if (keys[index] !== String(index)) return false;
    const descriptor = Object.getOwnPropertyDescriptor(value, String(index));
    if (descriptor === undefined || !("value" in descriptor) || !descriptor.enumerable) return false;
  }
  const lengthDescriptor = Object.getOwnPropertyDescriptor(value, "length");
  return lengthDescriptor !== undefined && "value" in lengthDescriptor && lengthDescriptor.value === value.length && !lengthDescriptor.enumerable;
}

export function utf8ByteLength(value: string): number | undefined {
  if (!value.isWellFormed()) return undefined;
  let result = 0;
  for (const character of value) {
    const codePoint = character.codePointAt(0);
    if (codePoint === undefined) return undefined;
    result += codePoint <= 0x7f ? 1 : codePoint <= 0x7ff ? 2 : codePoint <= 0xffff ? 3 : 4;
  }
  return result;
}

export function isBoundedOpaqueString(value: unknown, maximumBytes: number): value is string {
  if (typeof value !== "string" || value.length === 0) return false;
  const length = utf8ByteLength(value);
  return length !== undefined && length <= maximumBytes;
}

function compareOpaqueStrings(left: string, right: string): number {
  const leftCharacters = [...left];
  const rightCharacters = [...right];
  const count = Math.min(leftCharacters.length, rightCharacters.length);
  for (let index = 0; index < count; index += 1) {
    const leftCodePoint = leftCharacters[index]?.codePointAt(0);
    const rightCodePoint = rightCharacters[index]?.codePointAt(0);
    if (leftCodePoint === undefined || rightCodePoint === undefined) return 0;
    if (leftCodePoint !== rightCodePoint) return leftCodePoint < rightCodePoint ? -1 : 1;
  }
  return leftCharacters.length === rightCharacters.length ? 0 : leftCharacters.length < rightCharacters.length ? -1 : 1;
}

export function isDigest(value: unknown): value is string {
  return typeof value === "string" && digestPattern.test(value);
}

export function isFixedArrayBuffer(value: unknown): value is ArrayBuffer {
  if (value === null || typeof value !== "object") return false;
  if (Object.getPrototypeOf(value) !== ArrayBuffer.prototype || Reflect.ownKeys(value).length !== 0) return false;
  return (value as ArrayBuffer & {readonly resizable?: boolean}).resizable !== true;
}

function isPositiveSafeInteger(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0;
}

export function validateTransportLimits(value: unknown): EngineWorkerTransportLimits | undefined {
  if (!isPlainRecord(value) || !hasExactKeys(value, transportLimitKeys)) return undefined;
  if (!transportLimitKeys.every((key) => isPositiveSafeInteger(value[key]))) return undefined;
  const limits = value as unknown as EngineWorkerTransportLimits;
  if (limits.max_control_bytes > maxControlBytes || limits.max_control_depth > maxControlDepth ||
      limits.max_blob_id_bytes > maxBlobIDBytes || limits.max_buffers > maxBuffers ||
      limits.max_input_blob_bytes > maxInputBlobBytes || limits.max_input_total_bytes > maxInputTotalBytes ||
      limits.max_output_blob_bytes > maxOutputBlobBytes || limits.max_output_total_bytes > maxOutputTotalBytes ||
      limits.max_response_publish_bytes > maxResponsePublishBytes) return undefined;
  if (limits.max_input_blob_bytes > limits.max_input_total_bytes || limits.max_output_blob_bytes > limits.max_output_total_bytes ||
      limits.max_control_bytes > limits.max_response_publish_bytes) return undefined;
  return Object.freeze({...limits});
}

export function hasInitialTransportLimits(value: unknown): value is EngineWorkerTransportLimits {
  const limits = validateTransportLimits(value);
  return limits !== undefined && transportLimitKeys.every((key) => limits[key] === browserTransportLimits[key]);
}

function checkedTotal(current: number, increment: number, maximum: number): number | undefined {
  return Number.isSafeInteger(increment) && increment >= 0 && current <= maximum - increment ? current + increment : undefined;
}

function isBlobRecord(value: unknown, maximumIDBytes: number): value is EngineWorkerBlob {
  return isPlainRecord(value) && hasExactKeys(value, ["blob_id", "bytes"]) &&
    isBoundedOpaqueString(value.blob_id, maximumIDBytes) && isFixedArrayBuffer(value.bytes);
}

export function validateBlobs(
  value: unknown,
  maximumCount: number,
  maximumBlobBytes: number,
  maximumTotalBytes: number,
  maximumIDBytes: number,
  identities: Set<ArrayBuffer>,
): value is readonly EngineWorkerBlob[] {
  if (!isArrayRecord(value) || value.length > maximumCount) return false;
  let previous: string | undefined;
  let total = 0;
  for (const item of value) {
    if (!isBlobRecord(item, maximumIDBytes) || (previous !== undefined && compareOpaqueStrings(previous, item.blob_id) >= 0)) return false;
    previous = item.blob_id;
    if (item.bytes.byteLength > maximumBlobBytes || identities.has(item.bytes)) return false;
    identities.add(item.bytes);
    const next = checkedTotal(total, item.bytes.byteLength, maximumTotalBytes);
    if (next === undefined) return false;
    total = next;
  }
  return true;
}

function validateBaseRecord(value: unknown): Record<string, unknown> | undefined {
  return isPlainRecord(value) && value.worker_protocol === workerProtocol && value.worker_protocol_version === workerProtocolVersion ? value : undefined;
}

export function validateInitMessage(value: unknown): InitMessage | undefined {
  const record = validateBaseRecord(value);
  const keys = ["worker_protocol", "worker_protocol_version", "kind", "endpoint_generation", "expected_artifact_manifest_digest", "release_manifest_digest"];
  if (record === undefined || !hasExactKeys(record, keys) || record.kind !== "init") return undefined;
  return isBoundedOpaqueString(record.endpoint_generation, maxEndpointGenerationBytes) &&
    isDigest(record.expected_artifact_manifest_digest) && isDigest(record.release_manifest_digest) ? record as unknown as InitMessage : undefined;
}

export function validateRequestMessage(value: unknown, limits: EngineWorkerTransportLimits): RequestMessage | undefined {
  const record = validateBaseRecord(value);
  const keys = ["worker_protocol", "worker_protocol_version", "kind", "endpoint_generation", "exchange_id", "control", "blobs"];
  if (record === undefined || !hasExactKeys(record, keys) || record.kind !== "request") return undefined;
  if (!isBoundedOpaqueString(record.endpoint_generation, maxEndpointGenerationBytes) || !isBoundedOpaqueString(record.exchange_id, maxExchangeIDBytes)) return undefined;
  if (!isFixedArrayBuffer(record.control) || record.control.byteLength === 0 || record.control.byteLength > limits.max_control_bytes) return undefined;
  const identities = new Set<ArrayBuffer>([record.control]);
  return validateBlobs(record.blobs, limits.max_buffers, limits.max_input_blob_bytes, limits.max_input_total_bytes, limits.max_blob_id_bytes, identities) ?
    record as unknown as RequestMessage : undefined;
}

export function validateRequestPayload(value: unknown, limits: EngineWorkerTransportLimits): EngineWorkerRequestPayload | undefined {
  if (!isPlainRecord(value) || !hasExactKeys(value, ["exchangeID", "control", "blobs"])) return undefined;
  if (!isBoundedOpaqueString(value.exchangeID, maxExchangeIDBytes) || !isFixedArrayBuffer(value.control) ||
      value.control.byteLength === 0 || value.control.byteLength > limits.max_control_bytes) return undefined;
  const identities = new Set<ArrayBuffer>([value.control]);
  return validateBlobs(value.blobs, limits.max_buffers, limits.max_input_blob_bytes, limits.max_input_total_bytes, limits.max_blob_id_bytes, identities) ?
    value as unknown as EngineWorkerRequestPayload : undefined;
}

export function validateDisposeMessage(value: unknown): DisposeMessage | undefined {
  const record = validateBaseRecord(value);
  const keys = ["worker_protocol", "worker_protocol_version", "kind", "endpoint_generation"];
  return record !== undefined && hasExactKeys(record, keys) && record.kind === "dispose" &&
    isBoundedOpaqueString(record.endpoint_generation, maxEndpointGenerationBytes) ? record as unknown as DisposeMessage : undefined;
}

export function validateFailure(value: unknown): EngineWorkerFailure | undefined {
  if (!isPlainRecord(value) || !hasExactKeys(value, ["code", "phase", "retryable"]) ||
      typeof value.code !== "string" || !Object.hasOwn(failureDefinitions, value.code)) return undefined;
  const expected = failureDefinitions[value.code as EngineWorkerFailureCode];
  return value.phase === expected.phase && value.retryable === expected.retryable ? value as unknown as EngineWorkerFailure : undefined;
}

export function validateWorkerMessage(value: unknown, limits?: EngineWorkerTransportLimits): WorkerToHostMessage | undefined {
  const record = validateBaseRecord(value);
  if (record === undefined || typeof record.kind !== "string") return undefined;
  if (record.kind === "ready") {
    const keys = ["worker_protocol", "worker_protocol_version", "kind", "endpoint_generation", "artifact_manifest_digest", "transport_limits"];
    return hasExactKeys(record, keys) && isBoundedOpaqueString(record.endpoint_generation, maxEndpointGenerationBytes) &&
      isDigest(record.artifact_manifest_digest) && hasInitialTransportLimits(record.transport_limits) ? record as unknown as ReadyMessage : undefined;
  }
  if (record.kind === "accepted") {
    const keys = ["worker_protocol", "worker_protocol_version", "kind", "endpoint_generation", "exchange_id"];
    return hasExactKeys(record, keys) && isBoundedOpaqueString(record.endpoint_generation, maxEndpointGenerationBytes) &&
      isBoundedOpaqueString(record.exchange_id, maxExchangeIDBytes) ? record as unknown as AcceptedMessage : undefined;
  }
  if (record.kind === "response") {
    const keys = ["worker_protocol", "worker_protocol_version", "kind", "endpoint_generation", "exchange_id", "control", "blobs"];
    if (limits === undefined || !hasExactKeys(record, keys) || !isBoundedOpaqueString(record.endpoint_generation, maxEndpointGenerationBytes) ||
        !isBoundedOpaqueString(record.exchange_id, maxExchangeIDBytes)) return undefined;
    return validateEndpointResponse({control: record.control, blobs: record.blobs}, limits) === undefined ? undefined : record as unknown as ResponseMessage;
  }
  if (record.kind === "transport_failure") {
    const required = ["worker_protocol", "worker_protocol_version", "kind", "failure"];
    if (!hasExactKeys(record, required, ["endpoint_generation", "exchange_id"]) || validateFailure(record.failure) === undefined) return undefined;
    if (record.endpoint_generation !== undefined && !isBoundedOpaqueString(record.endpoint_generation, maxEndpointGenerationBytes)) return undefined;
    if (record.exchange_id !== undefined && !isBoundedOpaqueString(record.exchange_id, maxExchangeIDBytes)) return undefined;
    return record as unknown as TransportFailureMessage;
  }
  return undefined;
}

export function validateEndpointResponse(value: unknown, limits: EngineWorkerTransportLimits): EngineByteEndpointResponse | undefined {
  if (!isPlainRecord(value) || !hasExactKeys(value, ["control", "blobs"]) || !isFixedArrayBuffer(value.control) ||
      value.control.byteLength === 0 || value.control.byteLength > limits.max_control_bytes) return undefined;
  const identities = new Set<ArrayBuffer>([value.control]);
  if (!validateBlobs(value.blobs, limits.max_buffers, limits.max_output_blob_bytes, limits.max_output_total_bytes, limits.max_blob_id_bytes, identities)) return undefined;
  const published = (value.blobs as readonly EngineWorkerBlob[]).reduce((total, blob) => total + blob.bytes.byteLength, value.control.byteLength);
  return published <= limits.max_response_publish_bytes ? value as unknown as EngineByteEndpointResponse : undefined;
}

export function getSafeKind(value: unknown): string | undefined {
  return isPlainRecord(value) && typeof value.kind === "string" && value.kind.length <= 64 ? value.kind : undefined;
}

export function getSafeRouting(value: unknown): Readonly<{endpoint_generation?: string; exchange_id?: string}> {
  if (!isPlainRecord(value)) return Object.freeze({});
  const result: {endpoint_generation?: string; exchange_id?: string} = {};
  if (isBoundedOpaqueString(value.endpoint_generation, maxEndpointGenerationBytes)) result.endpoint_generation = value.endpoint_generation;
  if (isBoundedOpaqueString(value.exchange_id, maxExchangeIDBytes)) result.exchange_id = value.exchange_id;
  return Object.freeze(result);
}
