// SPDX-License-Identifier: Apache-2.0

import { isCapabilityID, isDigest, type Digest } from "@layerdraw/protocol/common";
import type * as Runtime from "@layerdraw/protocol/runtime";
import {
  decodeAutosaveControlResponseEnvelope,
  decodeCancelOperationResponseEnvelope,
  decodeCloseRuntimeDocumentResponseEnvelope,
  decodeCommitOperationsResponseEnvelope,
  decodeGetOperationResultResponseEnvelope,
  decodeInspectDocumentResponseEnvelope,
  decodeListRevisionsResponseEnvelope,
  decodeOpenDocumentResponseEnvelope,
  decodePreviewOperationsResponseEnvelope,
  decodeRecoverOperationsResponseEnvelope,
  decodeRestorePreviewResponseEnvelope,
  decodeRuntimeHandshakeResponseEnvelope,
  decodeSaveDocumentResponseEnvelope,
  decodeStageAssetResponseEnvelope,
  decodeStateSnapshotResponseEnvelope,
  encodeAutosaveControlRequestEnvelope,
  encodeCancelOperationRequestEnvelope,
  encodeCloseRuntimeDocumentRequestEnvelope,
  encodeCommitOperationsRequestEnvelope,
  encodeGetOperationResultRequestEnvelope,
  encodeInspectDocumentRequestEnvelope,
  encodeListRevisionsRequestEnvelope,
  encodeOpenDocumentRequestEnvelope,
  encodePreviewOperationsRequestEnvelope,
  encodeRecoverOperationsRequestEnvelope,
  encodeRestorePreviewRequestEnvelope,
  encodeRuntimeHandshakeRequestEnvelope,
  encodeSaveDocumentRequestEnvelope,
  encodeStageAssetRequestEnvelope,
  encodeStateSnapshotRequestEnvelope,
  schemaDigest as runtimeSchemaDigest,
} from "@layerdraw/protocol/runtime";
import {
  EngineClientInputError,
  EngineClientDecodeError,
  EngineClientError,
  EngineClientTransportError,
  type EngineClient,
  type EngineClientCreationOptions,
} from "./index.js";
import { createInternalEngineClient } from "./internal/client.js";
import { compareUtf8, dataObject, strictArray, utf8ByteLength } from "./internal/guards.js";
import { protocolBlobRefCollectors } from "./internal/protocol-collectors.js";
import {
  InternalTransportFault,
  type InternalByteTransport,
  type InternalTransportClose,
  type InternalTransportExchange,
  type InternalTransportFactory,
  type InternalTransportLimits,
  type InternalTransportRequest,
} from "./internal/transport.js";

const protocol = Object.freeze({ name: "runtime", version: "1.0" as const });
const BINDING_PROTOCOL_VERSION = "1.0";
const encoder = new TextEncoder();
const decoder = new TextDecoder("utf-8", { fatal: true });

const defaultRuntimeCapabilities = Object.freeze([
  "runtime.handshake", "runtime.open_document", "runtime.inspect_document",
  "runtime.preview_operations", "runtime.commit_operations", "runtime.save_document",
  "runtime.control_autosave", "runtime.get_state_snapshot", "runtime.list_revisions",
  "runtime.preview_restore", "runtime.stage_asset", "runtime.close_document",
  "runtime.cancel_operation", "runtime.get_operation_result", "runtime.recover_operations",
]);

const defaultLimits: InternalTransportLimits = Object.freeze({
  maxControlBytes: 8 * 1024 * 1024,
  maxControlDepth: 128,
  maxBlobIdBytes: 256,
  maxBuffers: 128,
  maxInputBlobBytes: 16 * 1024 * 1024,
  maxInputTotalBytes: 64 * 1024 * 1024,
  maxOutputBlobBytes: 16 * 1024 * 1024,
  maxOutputTotalBytes: 64 * 1024 * 1024,
  maxResponsePublishBytes: 72 * 1024 * 1024,
});

export interface WailsTransportLimits {
  readonly maxControlBytes: number;
  readonly maxControlDepth: number;
  readonly maxBlobIdBytes: number;
  readonly maxBuffers: number;
  readonly maxInputBlobBytes: number;
  readonly maxInputTotalBytes: number;
  readonly maxOutputBlobBytes: number;
  readonly maxOutputTotalBytes: number;
  readonly maxResponsePublishBytes: number;
}

export type WailsBindingFailureCode =
  | "BINDING_VERSION_MISMATCH"
  | "BINDING_SURFACE_INCOMPLETE"
  | "REQUEST_CANCELLED"
  | "APP_SHUTDOWN";

export type WailsBindingRecovery =
  | "upgrade_desktop"
  | "regenerate_bindings"
  | "retry"
  | "reopen_desktop";

const bindingMessages = Object.freeze({
  BINDING_VERSION_MISMATCH: "The Desktop bindings are incompatible with this client.",
  BINDING_SURFACE_INCOMPLETE: "The Desktop binding surface is incomplete.",
  REQUEST_CANCELLED: "The Desktop request was cancelled.",
  APP_SHUTDOWN: "The Desktop application is shutting down.",
});

export class WailsBindingError extends Error {
  readonly code: WailsBindingFailureCode;
  readonly recovery: WailsBindingRecovery;
  readonly retryable: boolean;

  constructor(code: WailsBindingFailureCode, recovery: WailsBindingRecovery, retryable = false) {
    super(bindingMessages[code]);
    this.name = "WailsBindingError";
    this.code = code;
    this.recovery = recovery;
    this.retryable = retryable;
  }
}

export interface WailsBlob {
  blob_id: string;
  /** Go encoding/json representation of []byte. */
  bytes: string;
}

export interface WailsExchange {
  operation: string;
  /** Go encoding/json representation of []byte. */
  control: string;
  blobs: WailsBlob[];
}

export interface WailsExchangeResult {
  operation: string;
  control: string;
  blobs: WailsBlob[];
}

export type WailsGeneratedMethod = (exchange: WailsExchange) => Promise<WailsExchangeResult>;

export interface WailsEngineBindings {
  readonly EngineApplyToHandle: WailsGeneratedMethod;
  readonly EngineClassifyAuthoringImpact: WailsGeneratedMethod;
  readonly EngineCloseDocument: WailsGeneratedMethod;
  readonly EngineCompile: WailsGeneratedMethod;
  readonly EngineExecuteQuery: WailsGeneratedMethod;
  readonly EngineFindSymbols: WailsGeneratedMethod;
  readonly EngineFindUsages: WailsGeneratedMethod;
  readonly EngineFormatScope: WailsGeneratedMethod;
  readonly EngineGetNeighbors: WailsGeneratedMethod;
  readonly EngineHandshake: WailsGeneratedMethod;
  readonly EngineInspectSubgraph: WailsGeneratedMethod;
  readonly EngineListModules: WailsGeneratedMethod;
  readonly EngineListReferences: WailsGeneratedMethod;
  readonly EngineMaterializeView: WailsGeneratedMethod;
  readonly EngineOpenDocument: WailsGeneratedMethod;
  readonly EngineOrganizeWorkspace: WailsGeneratedMethod;
  readonly EnginePlanExport: WailsGeneratedMethod;
  readonly EnginePreviewFragment: WailsGeneratedMethod;
  readonly EnginePreviewOperations: WailsGeneratedMethod;
  readonly EnginePreviewSourcePatch: WailsGeneratedMethod;
  readonly EngineReadDeclarations: WailsGeneratedMethod;
  readonly EngineReadModules: WailsGeneratedMethod;
  readonly EngineReadReferences: WailsGeneratedMethod;
  readonly EngineReadRows: WailsGeneratedMethod;
  readonly EngineReadScope: WailsGeneratedMethod;
  readonly EngineReplaceSourceTree: WailsGeneratedMethod;
}

export interface WailsRuntimeBindings {
  readonly RuntimeCancelOperation: WailsGeneratedMethod;
  readonly RuntimeCommitOperations: WailsGeneratedMethod;
  readonly RuntimeControlAutosave: WailsGeneratedMethod;
  readonly RuntimeCloseDocument: WailsGeneratedMethod;
  readonly RuntimeGetOperationResult: WailsGeneratedMethod;
  readonly RuntimeGetStateSnapshot: WailsGeneratedMethod;
  readonly RuntimeHandshake: WailsGeneratedMethod;
  readonly RuntimeInspectDocument: WailsGeneratedMethod;
  readonly RuntimeListRevisions: WailsGeneratedMethod;
  readonly RuntimeOpenDocument: WailsGeneratedMethod;
  readonly RuntimePreviewOperations: WailsGeneratedMethod;
  readonly RuntimePreviewRestore: WailsGeneratedMethod;
  readonly RuntimeRecoverOperations: WailsGeneratedMethod;
  readonly RuntimeSaveDocument: WailsGeneratedMethod;
  readonly RuntimeStageAsset: WailsGeneratedMethod;
}

export interface WailsGeneratedBindings extends WailsEngineBindings, WailsRuntimeBindings {}

type GeneratedMethodName = keyof WailsGeneratedBindings;

const engineMethods = Object.freeze({
  "engine.apply_to_handle": "EngineApplyToHandle",
  "engine.classify_authoring_impact": "EngineClassifyAuthoringImpact",
  "engine.close_document": "EngineCloseDocument",
  "engine.compile": "EngineCompile",
  "engine.execute_query": "EngineExecuteQuery",
  "engine.find_symbols": "EngineFindSymbols",
  "engine.find_usages": "EngineFindUsages",
  "engine.format_scope": "EngineFormatScope",
  "engine.get_neighbors": "EngineGetNeighbors",
  "engine.handshake": "EngineHandshake",
  "engine.inspect_subgraph": "EngineInspectSubgraph",
  "engine.list_modules": "EngineListModules",
  "engine.list_references": "EngineListReferences",
  "engine.materialize_view": "EngineMaterializeView",
  "engine.open_document": "EngineOpenDocument",
  "engine.organize_workspace": "EngineOrganizeWorkspace",
  "engine.plan_export": "EnginePlanExport",
  "engine.preview_fragment": "EnginePreviewFragment",
  "engine.preview_operations": "EnginePreviewOperations",
  "engine.preview_source_patch": "EnginePreviewSourcePatch",
  "engine.read_declarations": "EngineReadDeclarations",
  "engine.read_modules": "EngineReadModules",
  "engine.read_references": "EngineReadReferences",
  "engine.read_rows": "EngineReadRows",
  "engine.read_scope": "EngineReadScope",
  "engine.replace_source_tree": "EngineReplaceSourceTree",
} as const satisfies Readonly<Record<string, GeneratedMethodName>>);

const runtimeMethods = Object.freeze({
  "runtime.cancel_operation": "RuntimeCancelOperation",
  "runtime.commit_operations": "RuntimeCommitOperations",
  "runtime.control_autosave": "RuntimeControlAutosave",
  "runtime.close_document": "RuntimeCloseDocument",
  "runtime.get_operation_result": "RuntimeGetOperationResult",
  "runtime.get_state_snapshot": "RuntimeGetStateSnapshot",
  "runtime.handshake": "RuntimeHandshake",
  "runtime.inspect_document": "RuntimeInspectDocument",
  "runtime.list_revisions": "RuntimeListRevisions",
  "runtime.open_document": "RuntimeOpenDocument",
  "runtime.preview_operations": "RuntimePreviewOperations",
  "runtime.preview_restore": "RuntimePreviewRestore",
  "runtime.recover_operations": "RuntimeRecoverOperations",
  "runtime.save_document": "RuntimeSaveDocument",
  "runtime.stage_asset": "RuntimeStageAsset",
} as const satisfies Readonly<Record<string, GeneratedMethodName>>);

export interface WailsBindingDescriptor {
  readonly generatedMethod: GeneratedMethodName;
  readonly operation: string;
  readonly target: "engine_client" | "runtime_client";
}

function bindingDescriptors(
  methods: Readonly<Record<string, GeneratedMethodName>>,
  target: WailsBindingDescriptor["target"],
): readonly WailsBindingDescriptor[] {
  return Object.freeze(Object.entries(methods).map(([operation, generatedMethod]) =>
    Object.freeze({ generatedMethod, operation, target })
  ));
}

/** Exact #120 generated binding closure consumed by this adapter. */
export const wailsEngineBindingDescriptors = bindingDescriptors(engineMethods, "engine_client");
export const wailsRuntimeBindingDescriptors = bindingDescriptors(runtimeMethods, "runtime_client");

interface Deferred<T> {
  readonly promise: Promise<T>;
  resolve(value: T): void;
  reject(error: unknown): void;
  readonly settled: boolean;
}

function deferred<T>(): Deferred<T> {
  let settled = false;
  let resolvePromise!: (value: T) => void;
  let rejectPromise!: (error: unknown) => void;
  const promise = new Promise<T>((resolve, reject) => {
    resolvePromise = resolve;
    rejectPromise = reject;
  });
  return {
    promise,
    get settled() { return settled; },
    resolve(value) { if (!settled) { settled = true; resolvePromise(value); } },
    reject(error) { if (!settled) { settled = true; rejectPromise(error); } },
  };
}

function deepFreeze<T>(value: T): T {
  if (typeof value !== "object" || value === null || Object.isFrozen(value)) return value;
  for (const child of Object.values(value)) deepFreeze(child);
  return Object.freeze(value);
}

function normalizeLimits(input: WailsTransportLimits | undefined): InternalTransportLimits {
  if (input === undefined) return defaultLimits;
  const keys = Object.keys(defaultLimits) as (keyof InternalTransportLimits)[];
  if (typeof input !== "object" || input === null || Array.isArray(input) || Object.keys(input).length !== keys.length) {
    throw new EngineClientInputError("INVALID_ARGUMENT");
  }
  const result = {} as Record<keyof InternalTransportLimits, number>;
  for (const key of keys) {
    const value = input[key];
    if (!Number.isSafeInteger(value) || value <= 0) throw new EngineClientInputError("INVALID_ARGUMENT");
    result[key] = value;
  }
  if (
    result.maxInputBlobBytes > result.maxInputTotalBytes ||
    result.maxOutputBlobBytes > result.maxOutputTotalBytes ||
    result.maxControlBytes + result.maxOutputTotalBytes > result.maxResponsePublishBytes
  ) throw new EngineClientInputError("INVALID_ARGUMENT");
  return Object.freeze(result);
}

function encodeBase64(input: ArrayBuffer): string {
  const bytes = new Uint8Array(input);
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
  let output = "";
  for (let index = 0; index < bytes.length; index += 3) {
    const first = bytes[index] ?? 0;
    const second = bytes[index + 1] ?? 0;
    const third = bytes[index + 2] ?? 0;
    const packed = (first << 16) | (second << 8) | third;
    output += alphabet[(packed >>> 18) & 63];
    output += alphabet[(packed >>> 12) & 63];
    output += index + 1 < bytes.length ? alphabet[(packed >>> 6) & 63] : "=";
    output += index + 2 < bytes.length ? alphabet[packed & 63] : "=";
  }
  return output;
}

function decodeBase64(input: unknown, maximumBytes: number): ArrayBuffer {
  if (typeof input !== "string" || input.length % 4 !== 0) {
    throw fault("decode", "MALFORMED_MESSAGE", false);
  }
  const padding = input.endsWith("==") ? 2 : input.endsWith("=") ? 1 : 0;
  const decodedLength = (input.length / 4) * 3 - padding;
  if (!Number.isSafeInteger(decodedLength) || decodedLength > maximumBytes || !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(input)) {
    throw fault("decode", "MALFORMED_MESSAGE", false);
  }
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
  const bytes = new Uint8Array(decodedLength);
  let offset = 0;
  for (let index = 0; index < input.length; index += 4) {
    const a = alphabet.indexOf(input[index] ?? "");
    const b = alphabet.indexOf(input[index + 1] ?? "");
    const c = input[index + 2] === "=" ? 0 : alphabet.indexOf(input[index + 2] ?? "");
    const d = input[index + 3] === "=" ? 0 : alphabet.indexOf(input[index + 3] ?? "");
    if (a < 0 || b < 0 || c < 0 || d < 0) throw fault("decode", "MALFORMED_MESSAGE", false);
    const packed = (a << 18) | (b << 12) | (c << 6) | d;
    if (offset < bytes.length) bytes[offset++] = (packed >>> 16) & 255;
    if (offset < bytes.length) bytes[offset++] = (packed >>> 8) & 255;
    if (offset < bytes.length) bytes[offset++] = packed & 255;
  }
  return bytes.buffer;
}

function controlDepth(text: string): number {
  let depth = 0;
  let maximum = 0;
  let inString = false;
  let escaped = false;
  for (const character of text) {
    if (inString) {
      if (escaped) escaped = false;
      else if (character === "\\") escaped = true;
      else if (character === '"') inString = false;
    } else if (character === '"') inString = true;
    else if (character === "{" || character === "[") maximum = Math.max(maximum, ++depth);
    else if (character === "}" || character === "]") depth--;
  }
  return maximum;
}

function fault(kind: "transport" | "decode", code: ConstructorParameters<typeof InternalTransportFault>[0]["code"], retryable: boolean): InternalTransportFault {
  return new InternalTransportFault({ kind, code, retryable });
}

function operationOf(control: ArrayBuffer): string {
  try {
    const value: unknown = JSON.parse(decoder.decode(control));
    if (typeof value !== "object" || value === null || Array.isArray(value)) throw new Error();
    const operation = (value as Record<string, unknown>).operation;
    if (typeof operation !== "string") throw new Error();
    return operation;
  } catch {
    throw fault("decode", "MALFORMED_MESSAGE", false);
  }
}

function snapshotMethods(value: unknown, methods: readonly GeneratedMethodName[]): Partial<Record<GeneratedMethodName, WailsGeneratedMethod>> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new WailsBindingError("BINDING_SURFACE_INCOMPLETE", "regenerate_bindings");
  }
  const record = value as Record<string, unknown>;
  const snapshot: Partial<Record<GeneratedMethodName, WailsGeneratedMethod>> = {};
  for (const method of methods) {
    let candidate: unknown;
    try { candidate = record[method]; } catch {
      throw new WailsBindingError("BINDING_SURFACE_INCOMPLETE", "regenerate_bindings");
    }
    if (typeof candidate !== "function") {
      throw new WailsBindingError("BINDING_SURFACE_INCOMPLETE", "regenerate_bindings");
    }
    snapshot[method] = candidate as WailsGeneratedMethod;
  }
  return Object.freeze(snapshot);
}

function createWailsTransport(bindings: Partial<Record<GeneratedMethodName, WailsGeneratedMethod>>, methods: Readonly<Record<string, GeneratedMethodName>>, limits: InternalTransportLimits, shutdown?: WailsShutdownSource): InternalByteTransport {
  let stopped = false;
  const pending = new Set<Deferred<Awaited<ReturnType<WailsGeneratedMethod>>>>();
  const closed = deferred<InternalTransportClose>();
  let unsubscribe: (() => void) | undefined;
  const subscription = shutdown?.subscribe(() => terminate(new WailsBindingError("APP_SHUTDOWN", "reopen_desktop")));
  if (subscription !== undefined && typeof subscription !== "function") {
    throw new EngineClientInputError("INVALID_ARGUMENT");
  }
  unsubscribe = subscription;
  if (stopped) {
    try { unsubscribe?.(); } catch { /* Host cleanup failures are never exposed. */ }
    unsubscribe = undefined;
  }

  function terminate(reason: unknown = fault("transport", "BROKEN_PIPE", true)): void {
    if (stopped) return;
    stopped = true;
    try { unsubscribe?.(); } catch { /* Host cleanup failures are never exposed. */ }
    unsubscribe = undefined;
    for (const item of pending) item.reject(reason);
    pending.clear();
    closed.resolve(Object.freeze({ code: "BROKEN_PIPE", retryable: true }));
  }

  return Object.freeze({
    ready: Promise.resolve(limits),
    closed: closed.promise,
    request(input: InternalTransportRequest): InternalTransportExchange {
      if (stopped) throw fault("transport", "BROKEN_PIPE", true);
      let inputControlText: string;
      try { inputControlText = decoder.decode(input.control); } catch { throw fault("decode", "MALFORMED_MESSAGE", false); }
      if (
        input.control.byteLength > limits.maxControlBytes ||
        controlDepth(inputControlText) > limits.maxControlDepth ||
        input.blobs.length > limits.maxBuffers
      ) throw fault("decode", "MALFORMED_MESSAGE", false);
      let inputTotal = 0;
      let inputPrior: string | undefined;
      for (const blob of input.blobs) {
        const length = blob.bytes.byteLength;
        const idLength = typeof blob.blobId === "string" && blob.blobId.length <= limits.maxBlobIdBytes
          ? utf8ByteLength(blob.blobId)
          : undefined;
        inputTotal += length;
        if (
          idLength === undefined || idLength < 1 || idLength > limits.maxBlobIdBytes ||
          (inputPrior !== undefined && compareUtf8(inputPrior, blob.blobId) >= 0) ||
          length > limits.maxInputBlobBytes || !Number.isSafeInteger(inputTotal) ||
          inputTotal > limits.maxInputTotalBytes
        ) throw fault("decode", "MALFORMED_MESSAGE", false);
        inputPrior = blob.blobId;
      }
      const operation = operationOf(input.control);
      const methodName = methods[operation];
      if (methodName === undefined) throw fault("decode", "PROTOCOL_MISMATCH", false);
      const result = deferred<WailsExchangeResult>();
      pending.add(result);
      const exchange: WailsExchange = {
        operation,
        control: encodeBase64(input.control),
        blobs: input.blobs.map((blob) => ({ blob_id: blob.blobId, bytes: encodeBase64(blob.bytes) })),
      };
      const method = bindings[methodName];
      if (method === undefined) throw fault("decode", "PROTOCOL_MISMATCH", false);
      Promise.resolve().then(() => method(exchange)).then(
        (value) => result.resolve(value),
        () => result.reject(fault("transport", "BROKEN_PIPE", true)),
      ).finally(() => pending.delete(result));
      const response = result.promise.then((value) => {
        const object = dataObject(value, ["operation", "control", "blobs"]);
        if (object === undefined || object.operation !== operation) throw fault("decode", "CORRELATION_MISMATCH", false);
        if (!strictArray(object.blobs) || object.blobs.length > limits.maxBuffers) throw fault("decode", "MALFORMED_MESSAGE", false);
        const control = decodeBase64(object.control, limits.maxControlBytes);
        let controlText: string;
        try { controlText = decoder.decode(control); } catch { throw fault("decode", "MALFORMED_MESSAGE", false); }
        if (controlDepth(controlText) > limits.maxControlDepth) throw fault("decode", "MALFORMED_MESSAGE", false);
        let total = 0;
        let prior: string | undefined;
        const blobs = object.blobs.map((raw) => {
          const blob = dataObject(raw, ["blob_id", "bytes"]);
          if (blob === undefined || typeof blob.blob_id !== "string" || blob.blob_id.length > limits.maxBlobIdBytes) {
            throw fault("decode", "MALFORMED_MESSAGE", false);
          }
          const idLength = utf8ByteLength(blob.blob_id);
          if (idLength === undefined || idLength < 1 || idLength > limits.maxBlobIdBytes || (prior !== undefined && compareUtf8(prior, blob.blob_id) >= 0)) {
            throw fault("decode", "MALFORMED_MESSAGE", false);
          }
          const bytes = decodeBase64(blob.bytes, limits.maxOutputBlobBytes);
          total += bytes.byteLength;
          if (
            !Number.isSafeInteger(total) || total > limits.maxOutputTotalBytes ||
            control.byteLength + total > limits.maxResponsePublishBytes
          ) throw fault("decode", "MALFORMED_MESSAGE", false);
          prior = blob.blob_id;
          return Object.freeze({ blobId: blob.blob_id, bytes });
        });
        return Object.freeze({
          control,
          blobs: Object.freeze(blobs),
        });
      });
      return Object.freeze({
        response,
        async cancel() {
          result.reject(new WailsBindingError("REQUEST_CANCELLED", "retry", true));
          await response.then(() => undefined, () => undefined);
          return Object.freeze({ reusable: false });
        },
      });
    },
    terminate,
    async dispose() { terminate(new WailsBindingError("APP_SHUTDOWN", "reopen_desktop")); },
  });
}

export interface WailsShutdownSource {
  subscribe(listener: () => void): () => void;
}

function coordinatedShutdown(upstream?: WailsShutdownSource): { readonly source: WailsShutdownSource; stop(): void } {
  let stopped = false;
  let unsubscribeUpstream: (() => void) | undefined;
  const listeners = new Set<() => void>();
  const stop = () => {
    if (stopped) return;
    stopped = true;
    try { unsubscribeUpstream?.(); } catch { /* Host cleanup failures are never exposed. */ }
    unsubscribeUpstream = undefined;
    const snapshot = [...listeners];
    listeners.clear();
    for (const listener of snapshot) listener();
  };
  const source = Object.freeze({
    subscribe(listener: () => void) {
      if (stopped) {
        listener();
        return () => undefined;
      }
      listeners.add(listener);
      if (listeners.size === 1 && upstream !== undefined) {
        let candidate: unknown;
        try { candidate = upstream.subscribe(stop); } catch {
          stop();
          throw new EngineClientInputError("INVALID_ARGUMENT");
        }
        if (typeof candidate !== "function") {
          stop();
          throw new EngineClientInputError("INVALID_ARGUMENT");
        }
        const unsubscribe = candidate as () => void;
        if (stopped) {
          try { unsubscribe(); } catch { /* Host cleanup failures are never exposed. */ }
        } else {
          unsubscribeUpstream = unsubscribe;
        }
      }
      let active = true;
      return () => {
        if (!active) return;
        active = false;
        listeners.delete(listener);
        if (listeners.size === 0) {
          try { unsubscribeUpstream?.(); } catch { /* Host cleanup failures are never exposed. */ }
          unsubscribeUpstream = undefined;
        }
      };
    },
  });
  return Object.freeze({ source, stop });
}

export interface CreateWailsEngineClientOptions {
  readonly bindings: WailsEngineBindings;
  readonly bindingProtocolVersion: string;
  readonly client: EngineClientCreationOptions;
  readonly transportLimits?: WailsTransportLimits;
  readonly shutdown?: WailsShutdownSource;
}

function validateVersion(bindingProtocolVersion: string): void {
  if (bindingProtocolVersion !== BINDING_PROTOCOL_VERSION) {
    throw new WailsBindingError("BINDING_VERSION_MISMATCH", "upgrade_desktop");
  }
}

function admitShutdown(value: unknown): WailsShutdownSource | undefined {
  if (value === undefined) return undefined;
  const object = dataObject(value, ["subscribe"], []);
  if (object === undefined || typeof object.subscribe !== "function") throw new EngineClientInputError("INVALID_ARGUMENT");
  const subscribe = object.subscribe as WailsShutdownSource["subscribe"];
  return Object.freeze({ subscribe: (listener: () => void) => Reflect.apply(subscribe, value, [listener]) as () => void });
}

function admitEngineOptions(input: unknown): CreateWailsEngineClientOptions {
  const object = dataObject(input, ["bindings", "bindingProtocolVersion", "client"], ["transportLimits", "shutdown"]);
  if (object === undefined || typeof object.bindingProtocolVersion !== "string") throw new EngineClientInputError("INVALID_ARGUMENT");
  validateVersion(object.bindingProtocolVersion);
  const bindings = snapshotMethods(object.bindings, Object.values(engineMethods)) as WailsEngineBindings;
  return Object.freeze({
    bindings,
    bindingProtocolVersion: object.bindingProtocolVersion,
    client: object.client as EngineClientCreationOptions,
    ...(object.transportLimits === undefined ? {} : { transportLimits: object.transportLimits as WailsTransportLimits }),
    ...(object.shutdown === undefined ? {} : { shutdown: admitShutdown(object.shutdown) as WailsShutdownSource }),
  });
}

function wailsEngineFactory(options: CreateWailsEngineClientOptions, limits: InternalTransportLimits): InternalTransportFactory {
  return Object.freeze({
    transportId: "wails",
    connectFailureCode: "CONNECT_FAILED" as const,
    create: () => createWailsTransport(options.bindings, engineMethods, limits, options.shutdown),
  });
}

export async function createWailsEngineClient(options: CreateWailsEngineClientOptions): Promise<EngineClient> {
  const admitted = admitEngineOptions(options);
  const limits = normalizeLimits(admitted.transportLimits);
  return createInternalEngineClient({
    transportFactory: wailsEngineFactory(admitted, limits),
    protocolCollectors: protocolBlobRefCollectors,
    options: admitted.client,
  });
}

export interface WailsRuntimeRequestOptions {
  readonly requestId?: string;
  readonly signal?: AbortSignal;
}

export interface WailsDesktopClient {
  readonly engine: EngineClient;
  readonly state: "ready" | "failed" | "disposing" | "disposed";
  getCapabilities(): Runtime.RuntimeCapabilityManifest;
  hasCapability(operation: string): boolean;
  openDocument(input: Runtime.OpenRuntimeDocumentInput, options?: WailsRuntimeRequestOptions): Promise<Runtime.OpenDocumentResponseEnvelope>;
  inspectDocument(input: Runtime.RuntimeSessionInput, options?: WailsRuntimeRequestOptions): Promise<Runtime.InspectDocumentResponseEnvelope>;
  previewOperations(input: Runtime.PreviewOperationsInput, options?: WailsRuntimeRequestOptions): Promise<Runtime.PreviewOperationsResponseEnvelope>;
  commitOperations(input: Runtime.RuntimeCommitInput, options?: WailsRuntimeRequestOptions): Promise<Runtime.CommitOperationsResponseEnvelope>;
  saveDocument(input: Runtime.RuntimeCommitInput, options?: WailsRuntimeRequestOptions): Promise<Runtime.SaveDocumentResponseEnvelope>;
  controlAutosave(input: Runtime.AutosaveControlInput, options?: WailsRuntimeRequestOptions): Promise<Runtime.AutosaveControlResponseEnvelope>;
  getStateSnapshot(input: Runtime.RuntimeSessionInput, options?: WailsRuntimeRequestOptions): Promise<Runtime.StateSnapshotResponseEnvelope>;
  listRevisions(input: Runtime.ListRevisionsInput, options?: WailsRuntimeRequestOptions): Promise<Runtime.ListRevisionsResponseEnvelope>;
  previewRestore(input: Runtime.RestorePreviewInput, options?: WailsRuntimeRequestOptions): Promise<Runtime.RestorePreviewResponseEnvelope>;
  stageAsset(input: Runtime.StageAssetInput, bytes: Uint8Array, options?: WailsRuntimeRequestOptions): Promise<Runtime.StageAssetResponseEnvelope>;
  closeDocument(input: Runtime.RuntimeSessionInput, options?: WailsRuntimeRequestOptions): Promise<Runtime.CloseRuntimeDocumentResponseEnvelope>;
  cancelOperation(input: Runtime.CancelOperationInput, options?: WailsRuntimeRequestOptions): Promise<Runtime.CancelOperationResponseEnvelope>;
  getOperationResult(input: Runtime.GetOperationResultInput, options?: WailsRuntimeRequestOptions): Promise<Runtime.GetOperationResultResponseEnvelope>;
  recoverOperations(input: Runtime.RecoverOperationsInput, options?: WailsRuntimeRequestOptions): Promise<Runtime.RecoverOperationsResponseEnvelope>;
  restart(): Promise<void>;
  dispose(): Promise<void>;
}

export interface CreateWailsDesktopClientOptions extends CreateWailsEngineClientOptions {
  readonly bindings: WailsGeneratedBindings;
  readonly expectedReleaseManifestDigest: Digest;
  readonly requiredRuntimeCapabilities?: readonly string[];
  readonly optionalRuntimeCapabilities?: readonly string[];
}

function capabilityList(value: unknown): readonly string[] | undefined {
  if (value === undefined) return undefined;
  if (!Array.isArray(value) || value.some((item) => !isCapabilityID(item)) || new Set(value).size !== value.length) {
    throw new EngineClientInputError("INVALID_ARGUMENT");
  }
  return Object.freeze([...value]) as readonly string[];
}

function admitDesktopOptions(input: unknown): CreateWailsDesktopClientOptions {
  const object = dataObject(input, ["bindings", "bindingProtocolVersion", "client", "expectedReleaseManifestDigest"], ["transportLimits", "shutdown", "requiredRuntimeCapabilities", "optionalRuntimeCapabilities"]);
  if (object === undefined || typeof object.bindingProtocolVersion !== "string" || !isDigest(object.expectedReleaseManifestDigest)) throw new EngineClientInputError("INVALID_ARGUMENT");
  validateVersion(object.bindingProtocolVersion);
  const bindings = snapshotMethods(object.bindings, [...Object.values(engineMethods), ...Object.values(runtimeMethods)]) as WailsGeneratedBindings;
  const required = capabilityList(object.requiredRuntimeCapabilities);
  const optional = capabilityList(object.optionalRuntimeCapabilities);
  if (required !== undefined && optional !== undefined && required.some((item) => optional.includes(item))) throw new EngineClientInputError("INVALID_ARGUMENT");
  return Object.freeze({
    bindings,
    bindingProtocolVersion: object.bindingProtocolVersion,
    client: object.client as EngineClientCreationOptions,
    expectedReleaseManifestDigest: object.expectedReleaseManifestDigest,
    ...(object.transportLimits === undefined ? {} : { transportLimits: object.transportLimits as WailsTransportLimits }),
    ...(object.shutdown === undefined ? {} : { shutdown: admitShutdown(object.shutdown) as WailsShutdownSource }),
    ...(required === undefined ? {} : { requiredRuntimeCapabilities: required }),
    ...(optional === undefined ? {} : { optionalRuntimeCapabilities: optional }),
  });
}

class WailsDesktopClientImpl implements WailsDesktopClient {
  state: "ready" | "failed" | "disposing" | "disposed" = "ready";
  private counter = 0;
  private restartFlight: Promise<void> | undefined;
  private disposeFlight: Promise<void> | undefined;
  private replacement: InternalByteTransport | undefined;

  constructor(
    readonly engine: EngineClient,
    private readonly options: CreateWailsDesktopClientOptions,
    private transport: InternalByteTransport,
    private manifest: Runtime.RuntimeCapabilityManifest,
  ) {
    this.observe(transport);
  }

  private observe(transport: InternalByteTransport): void {
    void transport.closed.then(() => {
      if (this.transport === transport && this.state === "ready") this.state = "failed";
    });
  }

  getCapabilities() { return this.manifest; }
  hasCapability(operation: string) { return this.manifest.operations[operation]?.enabled === true; }
  private isTerminalLifecycle() { return this.state === "disposing" || this.state === "disposed"; }

  private requestId(options?: WailsRuntimeRequestOptions): string {
    if (options?.requestId !== undefined) {
      if (typeof options.requestId !== "string" || options.requestId.length === 0 || Array.from(options.requestId).length > 128 || options.requestId.includes("\0")) throw new EngineClientInputError("INVALID_REQUEST_ID");
      return options.requestId;
    }
    return `wails-runtime-${++this.counter}`;
  }

  private async exchange<T extends { readonly request_id: string }>(operation: string, control: string, decode: (value: string) => T, blobs: readonly { readonly blobId: string; readonly bytes: ArrayBuffer }[], options?: WailsRuntimeRequestOptions): Promise<T> {
    if (this.state !== "ready") throw new WailsBindingError("APP_SHUTDOWN", "reopen_desktop");
    if (!this.hasCapability(operation)) throw new EngineClientTransportError("NEGOTIATION_REJECTED", false);
    const requestId = this.requestId(options);
    const transportExchange = this.transport.request({ exchangeId: requestId, control: textBuffer(control), blobs });
    const abort = () => { void transportExchange.cancel(); };
    if (options?.signal?.aborted) abort();
    else options?.signal?.addEventListener("abort", abort, { once: true });
    try {
      const response = await transportExchange.response;
      if (response.blobs.length !== 0) throw new EngineClientTransportError("BROKEN_PIPE", false);
      const decoded = decode(decoder.decode(response.control));
      if (decoded.request_id !== requestId) throw new EngineClientDecodeError("CORRELATION_MISMATCH");
      return decoded;
    } catch (error) {
      if (error instanceof WailsBindingError || error instanceof EngineClientError) throw error;
      if (error instanceof InternalTransportFault) {
        if (error.kind === "decode") throw new EngineClientDecodeError(error.code as ConstructorParameters<typeof EngineClientDecodeError>[0]);
        throw new EngineClientTransportError(error.code as ConstructorParameters<typeof EngineClientTransportError>[0], error.retryable);
      }
      throw new EngineClientDecodeError("MALFORMED_MESSAGE");
    } finally {
      options?.signal?.removeEventListener("abort", abort);
    }
  }

  openDocument(input: Runtime.OpenRuntimeDocumentInput, options?: WailsRuntimeRequestOptions) { const id = this.requestId(options); return this.exchange("runtime.open_document", encodeOpenDocumentRequestEnvelope({ protocol, request_id: id, operation: "runtime.open_document", payload: input }), decodeOpenDocumentResponseEnvelope, [], { ...options, requestId: id }); }
  inspectDocument(input: Runtime.RuntimeSessionInput, options?: WailsRuntimeRequestOptions) { const id = this.requestId(options); return this.exchange("runtime.inspect_document", encodeInspectDocumentRequestEnvelope({ protocol, request_id: id, operation: "runtime.inspect_document", payload: input }), decodeInspectDocumentResponseEnvelope, [], { ...options, requestId: id }); }
  previewOperations(input: Runtime.PreviewOperationsInput, options?: WailsRuntimeRequestOptions) { const id = this.requestId(options); return this.exchange("runtime.preview_operations", encodePreviewOperationsRequestEnvelope({ protocol, request_id: id, operation: "runtime.preview_operations", payload: input }), decodePreviewOperationsResponseEnvelope, [], { ...options, requestId: id }); }
  commitOperations(input: Runtime.RuntimeCommitInput, options?: WailsRuntimeRequestOptions) { const id = this.requestId(options); return this.exchange("runtime.commit_operations", encodeCommitOperationsRequestEnvelope({ protocol, request_id: id, operation: "runtime.commit_operations", payload: input }), decodeCommitOperationsResponseEnvelope, [], { ...options, requestId: id }); }
  saveDocument(input: Runtime.RuntimeCommitInput, options?: WailsRuntimeRequestOptions) { const id = this.requestId(options); return this.exchange("runtime.save_document", encodeSaveDocumentRequestEnvelope({ protocol, request_id: id, operation: "runtime.save_document", payload: input }), decodeSaveDocumentResponseEnvelope, [], { ...options, requestId: id }); }
  controlAutosave(input: Runtime.AutosaveControlInput, options?: WailsRuntimeRequestOptions) { const id = this.requestId(options); return this.exchange("runtime.control_autosave", encodeAutosaveControlRequestEnvelope({ protocol, request_id: id, operation: "runtime.control_autosave", payload: input }), decodeAutosaveControlResponseEnvelope, [], { ...options, requestId: id }); }
  getStateSnapshot(input: Runtime.RuntimeSessionInput, options?: WailsRuntimeRequestOptions) { const id = this.requestId(options); return this.exchange("runtime.get_state_snapshot", encodeStateSnapshotRequestEnvelope({ protocol, request_id: id, operation: "runtime.get_state_snapshot", payload: input }), decodeStateSnapshotResponseEnvelope, [], { ...options, requestId: id }); }
  listRevisions(input: Runtime.ListRevisionsInput, options?: WailsRuntimeRequestOptions) { const id = this.requestId(options); return this.exchange("runtime.list_revisions", encodeListRevisionsRequestEnvelope({ protocol, request_id: id, operation: "runtime.list_revisions", payload: input }), decodeListRevisionsResponseEnvelope, [], { ...options, requestId: id }); }
  previewRestore(input: Runtime.RestorePreviewInput, options?: WailsRuntimeRequestOptions) { const id = this.requestId(options); return this.exchange("runtime.preview_restore", encodeRestorePreviewRequestEnvelope({ protocol, request_id: id, operation: "runtime.preview_restore", payload: input }), decodeRestorePreviewResponseEnvelope, [], { ...options, requestId: id }); }
  stageAsset(input: Runtime.StageAssetInput, bytes: Uint8Array, options?: WailsRuntimeRequestOptions) { const id = this.requestId(options); const copied = bytes.slice().buffer; return this.exchange("runtime.stage_asset", encodeStageAssetRequestEnvelope({ protocol, request_id: id, operation: "runtime.stage_asset", payload: input }), decodeStageAssetResponseEnvelope, [{ blobId: input.content_blob.blob_id, bytes: copied }], { ...options, requestId: id }); }
  closeDocument(input: Runtime.RuntimeSessionInput, options?: WailsRuntimeRequestOptions) { const id = this.requestId(options); return this.exchange("runtime.close_document", encodeCloseRuntimeDocumentRequestEnvelope({ protocol, request_id: id, operation: "runtime.close_document", payload: input }), decodeCloseRuntimeDocumentResponseEnvelope, [], { ...options, requestId: id }); }
  cancelOperation(input: Runtime.CancelOperationInput, options?: WailsRuntimeRequestOptions) { const id = this.requestId(options); return this.exchange("runtime.cancel_operation", encodeCancelOperationRequestEnvelope({ protocol, request_id: id, operation: "runtime.cancel_operation", payload: input }), decodeCancelOperationResponseEnvelope, [], { ...options, requestId: id }); }
  getOperationResult(input: Runtime.GetOperationResultInput, options?: WailsRuntimeRequestOptions) { const id = this.requestId(options); return this.exchange("runtime.get_operation_result", encodeGetOperationResultRequestEnvelope({ protocol, request_id: id, operation: "runtime.get_operation_result", payload: input }), decodeGetOperationResultResponseEnvelope, [], { ...options, requestId: id }); }
  recoverOperations(input: Runtime.RecoverOperationsInput, options?: WailsRuntimeRequestOptions) { const id = this.requestId(options); return this.exchange("runtime.recover_operations", encodeRecoverOperationsRequestEnvelope({ protocol, request_id: id, operation: "runtime.recover_operations", payload: input }), decodeRecoverOperationsResponseEnvelope, [], { ...options, requestId: id }); }

  restart(): Promise<void> {
    if (this.isTerminalLifecycle()) return Promise.reject(new EngineClientTransportError("REPLACEMENT_FAILED", false));
    if (this.restartFlight !== undefined) return this.restartFlight;
    const flight = this.performRestart().finally(() => {
      if (this.restartFlight === flight) this.restartFlight = undefined;
    });
    this.restartFlight = flight;
    return flight;
  }

  private async performRestart(): Promise<void> {
    const prior = this.transport;
    const next = runtimeTransport(this.options);
    this.replacement = next;
    this.state = "failed";
    try {
      const manifest = await runtimeHandshake(next, this.options);
      await this.engine.restart();
      if (this.isTerminalLifecycle() || this.replacement !== next) {
        throw new EngineClientTransportError("REPLACEMENT_FAILED", false);
      }
      this.manifest = manifest;
      this.transport = next;
      this.replacement = undefined;
      this.observe(next);
      this.state = "ready";
      await prior.dispose();
    } catch {
      next.terminate();
      if (this.replacement === next) this.replacement = undefined;
      if (!this.isTerminalLifecycle()) this.state = "failed";
      throw new EngineClientTransportError("REPLACEMENT_FAILED");
    }
  }

  dispose(): Promise<void> {
    if (this.disposeFlight !== undefined) return this.disposeFlight;
    if (this.state === "disposed") return Promise.resolve();
    this.state = "disposing";
    this.replacement?.terminate();
    const flight = (async () => {
      await Promise.allSettled([this.transport.dispose(), this.engine.dispose(), this.restartFlight]);
      this.state = "disposed";
    })();
    this.disposeFlight = flight;
    return flight;
  }
}

function textBuffer(text: string): ArrayBuffer {
  const bytes = encoder.encode(text);
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength);
}

function runtimeTransport(options: CreateWailsDesktopClientOptions): InternalByteTransport {
  return createWailsTransport(options.bindings, runtimeMethods, normalizeLimits(options.transportLimits), options.shutdown);
}

async function runtimeHandshake(transport: InternalByteTransport, options: CreateWailsDesktopClientOptions): Promise<Runtime.RuntimeCapabilityManifest> {
  await transport.ready;
  const required = [...(options.requiredRuntimeCapabilities ?? defaultRuntimeCapabilities)];
  const optional = [...(options.optionalRuntimeCapabilities ?? [])];
  const request: Runtime.RuntimeHandshakeRequestEnvelope = {
    protocol,
    request_id: "wails-runtime-handshake",
    operation: "runtime.handshake",
    payload: {
      client_release: "0.0.0",
      protocols: [{ name: "runtime", supported_range: "1.0..1.0", versions: [{ version: "1.0", schema_digest: runtimeSchemaDigest }] }],
      required_capabilities: required,
      optional_capabilities: optional,
    },
  };
  const exchange = transport.request({ exchangeId: request.request_id, control: textBuffer(encodeRuntimeHandshakeRequestEnvelope(request)), blobs: [] });
  const raw = await exchange.response;
  if (raw.blobs.length !== 0) throw new EngineClientTransportError("NEGOTIATION_REJECTED", false);
  const response = decodeRuntimeHandshakeResponseEnvelope(decoder.decode(raw.control));
  if (response.request_id !== request.request_id) throw new EngineClientDecodeError("CORRELATION_MISMATCH");
  if (response.protocol.name !== "runtime" || response.protocol.version !== "1.0") throw new EngineClientDecodeError("PROTOCOL_MISMATCH");
  if (response.outcome !== "success" || response.payload === undefined || response.payload.release_manifest_digest !== options.expectedReleaseManifestDigest) {
    throw new EngineClientTransportError("NEGOTIATION_REJECTED", false);
  }
  const result = response.payload;
  const negotiated = result.negotiated_protocols.find((candidate) => candidate.name === "runtime");
  if (
    negotiated?.version !== "1.0" || negotiated.schema_digest !== runtimeSchemaDigest ||
    response.host_release !== result.host_release
  ) throw new EngineClientDecodeError("PROTOCOL_MISMATCH");
  const requested = new Set([...required, ...optional]);
  const statuses = new Map<string, boolean>();
  for (const status of result.capability_statuses) {
    if (!requested.has(status.capability_id) || statuses.has(status.capability_id)) throw new EngineClientDecodeError("MALFORMED_MESSAGE");
    if (status.protocol_version !== "1.0") throw new EngineClientDecodeError("PROTOCOL_MISMATCH");
    const advertised = result.capability_manifest.operations[status.capability_id];
    if (status.enabled) {
      if (advertised?.enabled !== true || advertised.protocol_version !== "1.0") throw new EngineClientDecodeError("PROTOCOL_MISMATCH");
    } else if (advertised !== undefined && (advertised.enabled !== false || advertised.protocol_version !== "1.0")) {
      throw new EngineClientDecodeError("PROTOCOL_MISMATCH");
    }
    statuses.set(status.capability_id, status.enabled);
  }
  if (statuses.size !== requested.size || required.some((operation) => statuses.get(operation) !== true)) {
    throw new EngineClientTransportError("NEGOTIATION_REJECTED", false);
  }
  for (const operation of required) {
    const advertised = result.capability_manifest.operations[operation];
    if (advertised?.enabled !== true || advertised.protocol_version !== "1.0") throw new EngineClientTransportError("NEGOTIATION_REJECTED", false);
  }
  return deepFreeze(result.capability_manifest);
}

export async function createWailsDesktopClient(options: CreateWailsDesktopClientOptions): Promise<WailsDesktopClient> {
  const admitted = admitDesktopOptions(options);
  const shutdown = coordinatedShutdown(admitted.shutdown);
  const coordinatedOptions: CreateWailsDesktopClientOptions = Object.freeze({ ...admitted, shutdown: shutdown.source });
  const transport = runtimeTransport(coordinatedOptions);
  try {
    const engineOptions: CreateWailsEngineClientOptions = {
      bindings: coordinatedOptions.bindings,
      bindingProtocolVersion: coordinatedOptions.bindingProtocolVersion,
      client: coordinatedOptions.client,
      ...(coordinatedOptions.transportLimits === undefined ? {} : { transportLimits: coordinatedOptions.transportLimits }),
      shutdown: shutdown.source,
    };
    const manifestPromise = runtimeHandshake(transport, coordinatedOptions).catch((error: unknown) => {
      shutdown.stop();
      throw error;
    });
    const enginePromise = createWailsEngineClient(engineOptions).catch((error: unknown) => {
      shutdown.stop();
      throw error;
    });
    const [manifestResult, engineResult] = await Promise.allSettled([
      manifestPromise,
      enginePromise,
    ]);
    if (manifestResult.status === "fulfilled" && engineResult.status === "fulfilled") {
      return new WailsDesktopClientImpl(engineResult.value, coordinatedOptions, transport, manifestResult.value);
    }
    if (engineResult.status === "fulfilled") await engineResult.value.dispose();
    if (manifestResult.status === "rejected") throw manifestResult.reason;
    if (engineResult.status === "rejected") throw engineResult.reason;
    throw new EngineClientTransportError("CONNECT_FAILED", false);
  } catch (error) {
    transport.terminate();
    throw error;
  }
}
