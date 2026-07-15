// SPDX-License-Identifier: Apache-2.0

import type {
  BlobRef,
  CapabilityID,
  CapabilityManifest,
  Digest,
  HandshakeResult,
} from "@layerdraw/protocol/common";
import {
  isCapabilityID,
  isBlobRef,
  isCompileResourceLimitConstraints,
  isDigest,
} from "@layerdraw/protocol/common";
import type {
  CompileInput,
  CompileRequestEnvelope,
  CompileResponseEnvelope,
  HandshakeRequestEnvelope,
  HandshakeResponseEnvelope,
} from "@layerdraw/protocol/engine";
import {
  decodeCompileInput,
  decodeCompileResponseEnvelope,
  decodeHandshakeResponseEnvelope,
  encodeCompileInput,
  encodeCompileRequestEnvelope,
  encodeHandshakeRequestEnvelope,
  isCompileInput,
  schemaDigest,
} from "@layerdraw/protocol/engine";
import {
  EngineClientBackpressureError,
  EngineClientDecodeError,
  EngineClientDisposeError,
  EngineClientInputError,
  EngineClientStateError,
  EngineClientTransportError,
  type ClientCancellationReason,
  type CompileOptions,
  type CompileOutcome,
  type CompileSuccessResponse,
  type EngineAbortSignal,
  type EngineClient,
  type EngineClientCreationOptions,
  type EngineClientState,
  type EngineEndpointSnapshot,
  type OutputBlob,
  type PortableCompileRequest,
} from "../index.js";
import {
  blobRefFingerprint,
  bytesToHex,
  compareUtf8,
  dataObject,
  deepFreeze,
  fixedArrayBufferByteLength,
  safeCountDetails,
  strictArray,
  uint8ViewSource,
  utf8ByteLength,
  validRequestId,
  validateCollectedRefs,
} from "./guards.js";
import {
  defaultRuntime,
  type InternalClientRuntime,
  type InternalDigestLease,
  type InternalTimerHandle,
} from "./runtime.js";
import { snapshotSafeDetails } from "./safe-details.js";
import {
  InternalTransportFault,
  type InternalByteTransport,
  type InternalCancelResult,
  type InternalProtocolBlobRefCollectors,
  type InternalTransportBlob,
  type InternalTransportClose,
  type InternalTransportExchange,
  type InternalTransportFactory,
  type InternalTransportLimits,
  type InternalTransportResponse,
} from "./transport.js";

const CLIENT_RELEASE = "0.0.0" as const;
const ENGINE_PROTOCOL = Object.freeze({ name: "engine", version: "1.0" });
const MAX_TIMEOUT_MS = 600_000;
const DEFAULT_HANDSHAKE_TIMEOUT_MS = 10_000;
const DEFAULT_COMPILE_TIMEOUT_MS = 120_000;
const DEFAULT_CANCEL_GRACE_MS = 500;
const DEFAULT_DISPOSE_TIMEOUT_MS = 2_000;

interface NormalizedCreationOptions {
  readonly expectedReleaseManifestDigest: Digest;
  readonly clientLimits?: NonNullable<EngineClientCreationOptions["clientLimits"]>;
  readonly requiredCapabilities: readonly CapabilityID[];
  readonly optionalCapabilities: readonly CapabilityID[];
  readonly handshakeTimeoutMs: number;
  readonly defaultCompileTimeoutMs: number;
  readonly cancelGraceMs: number;
  readonly disposeTimeoutMs: number;
}

interface EndpointContext {
  readonly generation: number;
  readonly transport: InternalByteTransport;
  readonly closed: Promise<unknown>;
  readonly limits: InternalTransportLimits;
  readonly snapshot: EngineEndpointSnapshot;
  readonly capabilityStatuses: ReadonlyMap<string, boolean>;
  readonly engineRelease: string;
}

interface AdmittedTransport {
  readonly transport: InternalByteTransport;
  readonly closed: Promise<unknown>;
}

const UNAVAILABLE_TRANSPORT_CLOSE = Object.freeze({
  kind: "unavailable" as const,
});

function admitTransport(transport: InternalByteTransport): AdmittedTransport {
  if (
    (typeof transport !== "object" && typeof transport !== "function") ||
    transport === null
  ) {
    throw new TypeError("Invalid transport");
  }
  let raw: unknown;
  try {
    raw = transport.closed;
  } catch {
    return Object.freeze({
      transport,
      closed: Promise.resolve(UNAVAILABLE_TRANSPORT_CLOSE),
    });
  }
  let closed: Promise<unknown>;
  try {
    closed = Promise.resolve(raw);
  } catch {
    closed = Promise.resolve(UNAVAILABLE_TRANSPORT_CLOSE);
  }
  void closed.catch(() => undefined);
  return Object.freeze({ transport, closed });
}

interface OwnedRequestBlob {
  readonly ref: BlobRef;
  readonly bytes: ArrayBuffer;
}

interface PreparedCompile {
  readonly input: CompileInput;
  readonly blobs: readonly OwnedRequestBlob[];
}

interface CancellationInterrupt {
  readonly kind: "cancel";
  readonly reason: ClientCancellationReason;
  readonly barrier?: Promise<void>;
}

interface FaultInterrupt {
  readonly kind: "fault";
  readonly fault: unknown;
}

type CompileInterrupt = CancellationInterrupt | FaultInterrupt;

interface ActiveCompile {
  readonly requestId: string;
  readonly generation: number;
  readonly interrupt: Deferred<CompileInterrupt>;
  readonly joined: Deferred<void>;
}

type CompileTerminal =
  | Readonly<{ kind: "valid"; outcome: CompileOutcome }>
  | Readonly<{ kind: "invalid"; error: unknown }>
  | Readonly<{ kind: "rejected"; error: unknown }>;

interface Deferred<T> {
  readonly promise: Promise<T>;
  readonly settled: boolean;
  resolve(value: T): void;
}

class DigestSequence {
  private readonly runtime: InternalClientRuntime;
  private current: InternalDigestLease | undefined;
  private aborted = false;

  constructor(runtime: InternalClientRuntime) {
    this.runtime = runtime;
  }

  async sha256(bytes: Uint8Array): Promise<Uint8Array> {
    if (this.aborted) throw new Error("Digest sequence aborted");
    const lease = this.runtime.sha256(bytes);
    this.current = lease;
    let digest: Uint8Array;
    try {
      digest = await lease.result;
    } finally {
      await lease.joined;
      if (this.current === lease) this.current = undefined;
    }
    if (this.aborted) throw new Error("Digest sequence aborted");
    const source = uint8ViewSource(digest);
    if (source === undefined || source.byteLength !== 32) {
      throw new Error("Invalid SHA-256 result");
    }
    const snapshot = new Uint8Array(32);
    snapshot.set(
      new Uint8Array(source.buffer, source.byteOffset, source.byteLength),
    );
    return snapshot;
  }

  async abortAndJoin(): Promise<void> {
    this.aborted = true;
    const lease = this.current;
    if (lease === undefined) return;
    lease.abort();
    await lease.joined;
  }
}

function deferred<T>(): Deferred<T> {
  let resolvePromise!: (value: T) => void;
  let settled = false;
  const promise = new Promise<T>((resolve) => {
    resolvePromise = resolve;
  });
  return {
    promise,
    get settled(): boolean {
      return settled;
    },
    resolve(value: T): void {
      if (settled) return;
      settled = true;
      resolvePromise(value);
    },
  };
}

class OpenAborted extends Error {}
class CreationTimedOut extends Error {}

export interface CreateInternalEngineClientOptions {
  readonly transportFactory: InternalTransportFactory;
  readonly protocolCollectors: InternalProtocolBlobRefCollectors;
  readonly options: EngineClientCreationOptions;
  /** Adapters may inject an isolated hard-terminable digest implementation. */
  readonly runtime?: InternalClientRuntime;
}

export async function createInternalEngineClient(
  input: CreateInternalEngineClientOptions,
): Promise<EngineClient> {
  const object = dataObject(input, [
    "transportFactory",
    "protocolCollectors",
    "options",
  ], ["runtime"]);
  if (object === undefined) {
    throw new EngineClientInputError("INVALID_ARGUMENT");
  }
  const runtime =
    (object.runtime as InternalClientRuntime | undefined) ?? defaultRuntime();
  let options: NormalizedCreationOptions;
  try {
    options = normalizeCreationOptions(
      object.options as EngineClientCreationOptions,
    );
  } catch (error) {
    throw inputFault(error);
  }
  const client = new EngineClientImplementation(
    object.transportFactory as InternalTransportFactory,
    object.protocolCollectors as InternalProtocolBlobRefCollectors,
    options,
    runtime,
  );
  await client.initialize();
  return client;
}

function normalizeCreationOptions(
  input: EngineClientCreationOptions,
): NormalizedCreationOptions {
  const value = dataObject(
    input,
    ["expectedReleaseManifestDigest"],
    [
      "clientLimits",
      "requiredCapabilities",
      "optionalCapabilities",
      "handshakeTimeoutMs",
      "defaultCompileTimeoutMs",
      "cancelGraceMs",
      "disposeTimeoutMs",
    ],
  );
  if (
    value === undefined ||
    !isDigest(value.expectedReleaseManifestDigest) ||
    (value.clientLimits !== undefined &&
      !isCompileResourceLimitConstraints(value.clientLimits))
  ) {
    throw new EngineClientInputError("INVALID_ARGUMENT");
  }

  const required = normalizeCapabilities(value.requiredCapabilities);
  const optional = normalizeCapabilities(value.optionalCapabilities);
  const requiredSet = new Set(required);
  requiredSet.add("engine.compile");
  const normalizedRequired = [...requiredSet].sort();
  const normalizedOptional = optional
    .filter((capability) => !requiredSet.has(capability))
    .sort();

  return Object.freeze({
    expectedReleaseManifestDigest: value.expectedReleaseManifestDigest,
    ...(value.clientLimits === undefined
      ? {}
      : { clientLimits: deepFreeze(value.clientLimits) }),
    requiredCapabilities: Object.freeze(normalizedRequired),
    optionalCapabilities: Object.freeze([...new Set(normalizedOptional)]),
    handshakeTimeoutMs: timeoutOption(
      value.handshakeTimeoutMs,
      DEFAULT_HANDSHAKE_TIMEOUT_MS,
    ),
    defaultCompileTimeoutMs: timeoutOption(
      value.defaultCompileTimeoutMs,
      DEFAULT_COMPILE_TIMEOUT_MS,
    ),
    cancelGraceMs: timeoutOption(value.cancelGraceMs, DEFAULT_CANCEL_GRACE_MS),
    disposeTimeoutMs: timeoutOption(
      value.disposeTimeoutMs,
      DEFAULT_DISPOSE_TIMEOUT_MS,
    ),
  });
}

function normalizeCapabilities(value: unknown): CapabilityID[] {
  if (value === undefined) return [];
  if (!strictArray(value) || value.some((item) => !isCapabilityID(item))) {
    throw new EngineClientInputError("INVALID_ARGUMENT");
  }
  return [...new Set(value as readonly CapabilityID[])];
}

function timeoutOption(value: unknown, fallback: number): number {
  if (value === undefined) return fallback;
  if (
    typeof value !== "number" ||
    !Number.isSafeInteger(value) ||
    value <= 0 ||
    value > MAX_TIMEOUT_MS
  ) {
    throw new EngineClientInputError("INVALID_ARGUMENT");
  }
  return value;
}

class EngineClientImplementation implements EngineClient {
  private readonly factory: InternalTransportFactory;
  private readonly collectors: InternalProtocolBlobRefCollectors;
  private readonly options: NormalizedCreationOptions;
  private readonly runtime: InternalClientRuntime;
  private readonly nonce: string;
  private requestCounter = 0n;
  private exchangeCounter = 0n;
  private lastGeneration = 0;
  private lastEndpointInstanceId: string | undefined;
  private endpoint: EndpointContext | undefined;
  private pendingTransport: AdmittedTransport | undefined;
  private pendingOpenAbort: Deferred<void> | undefined;
  private active: ActiveCompile | undefined;
  private replacementPromise: Promise<void> | undefined;
  private disposalPromise: Promise<void> | undefined;
  private readonly transportShutdowns = new WeakMap<
    InternalByteTransport,
    Promise<boolean>
  >();
  private _state: EngineClientState = "failed";

  constructor(
    factory: InternalTransportFactory,
    collectors: InternalProtocolBlobRefCollectors,
    options: NormalizedCreationOptions,
    runtime: InternalClientRuntime,
  ) {
    this.factory = factory;
    this.collectors = collectors;
    this.options = options;
    this.runtime = runtime;
    this.nonce = bytesToHex(runtime.randomBytes(12));
    if (!/^[a-z][a-z0-9_]*$/.test(factory.transportId)) {
      throw new EngineClientInputError("INVALID_ARGUMENT");
    }
  }

  get state(): EngineClientState {
    return this._state;
  }

  async initialize(): Promise<void> {
    try {
      const endpoint = await this.openEndpoint(1, undefined);
      this.publishEndpoint(endpoint);
    } catch (error) {
      this._state = "failed";
      throw error;
    }
  }

  getEndpoint(): EngineEndpointSnapshot {
    return this.readyEndpoint().snapshot;
  }

  getCapabilities(): CapabilityManifest {
    return this.readyEndpoint().snapshot.handshake.capability_manifest;
  }

  hasCapability(capability: CapabilityID): boolean {
    const endpoint = this.readyEndpoint();
    if (!isCapabilityID(capability)) return false;
    return endpoint.capabilityStatuses.get(capability) === true;
  }

  compile(
    request: PortableCompileRequest,
    options?: CompileOptions,
  ): Promise<CompileOutcome> {
    const endpoint = this.readyEndpoint();
    let normalized: NormalizedCompileOptions;
    try {
      normalized = normalizeCompileOptions(options, this.options);
    } catch (error) {
      throw inputFault(error);
    }
    const requestId = normalized.requestId ?? this.nextRequestId();
    if (this.active?.requestId === requestId) {
      throw new EngineClientStateError("DUPLICATE_REQUEST_ID", { requestId });
    }
    if (this.active !== undefined) {
      throw new EngineClientBackpressureError("SINGLE_FLIGHT_BUSY", {
        generation: endpoint.generation,
      });
    }
    if (normalized.signal !== undefined && signalAborted(normalized.signal)) {
      return Promise.resolve(
        deepFreeze({
          origin: "client",
          outcome: "cancelled",
          requestId,
          endpointGeneration: endpoint.generation,
          reason: "signal",
          blobs: [] as const,
        }),
      );
    }

    let preparedMetadata: ReturnType<
      EngineClientImplementation["prepareCompileMetadata"]
    >;
    try {
      preparedMetadata = this.prepareCompileMetadata(request, endpoint.limits);
    } catch (error) {
      throw inputFault(error);
    }
    const active: ActiveCompile = {
      requestId,
      generation: endpoint.generation,
      interrupt: deferred<CompileInterrupt>(),
      joined: deferred<void>(),
    };
    this.active = active;
    let prepared: PreparedCompile;
    try {
      prepared = this.takeCompileBytes(preparedMetadata, endpoint.limits);
    } catch (error) {
      if (this.active === active) this.active = undefined;
      throw error;
    }

    const promise = this.executeCompile(
      active,
      endpoint,
      prepared,
      normalized,
    );
    void promise.finally(() => {
      if (this.active === active) this.active = undefined;
    }).catch(() => undefined);
    return promise;
  }

  restart(): Promise<void> {
    if (this._state === "disposing") {
      throw new EngineClientStateError("DISPOSING");
    }
    if (this._state === "disposed") {
      throw new EngineClientStateError("DISPOSED");
    }
    if (this.replacementPromise !== undefined) return this.replacementPromise;
    const barrier = deferred<void>();
    const active = this.active;
    if (active !== undefined) {
      active.interrupt.resolve({
        kind: "cancel",
        reason: "restart",
        barrier: barrier.promise,
      });
    }
    return this.startReplacement(undefined, barrier);
  }

  dispose(): Promise<void> {
    if (this.disposalPromise !== undefined) return this.disposalPromise;
    this._state = "disposing";
    const barrier = deferred<void>();
    const active = this.active;
    if (active !== undefined) {
      active.interrupt.resolve({
        kind: "cancel",
        reason: "dispose",
        barrier: barrier.promise,
      });
    }
    this.pendingOpenAbort?.resolve();
    try {
      this.pendingTransport?.transport.terminate();
    } catch {
      // Raw platform failures are intentionally discarded.
    }

    const promise = (async (): Promise<void> => {
      let failed = false;
      try {
        if (active !== undefined) await active.joined.promise;
        if (this.replacementPromise !== undefined) {
          try {
            await this.replacementPromise;
          } catch {
            failed = true;
          }
        }
        const endpoint = this.endpoint;
        this.endpoint = undefined;
        if (endpoint !== undefined) {
          failed = !(await this.shutdownTransport(endpoint)) || failed;
        }
        if (
          this.pendingTransport !== undefined &&
          this.pendingTransport.transport !== endpoint?.transport
        ) {
          failed = !(await this.shutdownTransport(this.pendingTransport)) || failed;
        }
      } finally {
        this.pendingTransport = undefined;
        this._state = "disposed";
        barrier.resolve();
      }
      if (failed) throw new EngineClientDisposeError();
    })();
    this.disposalPromise = promise;
    return promise;
  }

  private readyEndpoint(): EndpointContext {
    if (this._state === "ready" && this.endpoint !== undefined) {
      return this.endpoint;
    }
    if (this._state === "disposing") {
      throw new EngineClientStateError("DISPOSING");
    }
    if (this._state === "disposed") {
      throw new EngineClientStateError("DISPOSED");
    }
    if (this._state === "failed") {
      throw new EngineClientStateError("FAILED");
    }
    throw new EngineClientStateError("NOT_READY");
  }

  private nextRequestId(): string {
    if (this.requestCounter >= 2n ** 64n - 1n) {
      throw new EngineClientStateError("FAILED");
    }
    this.requestCounter++;
    return `ec1-${this.nonce}-${this.requestCounter.toString(16).padStart(16, "0")}`;
  }

  private nextExchangeId(): string {
    if (this.exchangeCounter >= 2n ** 64n - 1n) {
      throw new EngineClientStateError("FAILED");
    }
    this.exchangeCounter++;
    return `ex1-${this.nonce}-${this.exchangeCounter
      .toString(16)
      .padStart(16, "0")}`;
  }

  private prepareCompileMetadata(
    request: PortableCompileRequest,
    limits: InternalTransportLimits,
  ): Readonly<{
    input: CompileInput;
    attachments: readonly Readonly<{
      ref: BlobRef;
      bytes: Uint8Array | ArrayBuffer;
      ownership: "copy" | "transfer";
      byteLength: number;
    }>[];
  }> {
    const object = dataObject(request, ["input", "blobs"]);
    if (object === undefined || !isCompileInput(object.input)) {
      throw new EngineClientInputError("INVALID_ARGUMENT");
    }
    let input: CompileInput;
    try {
      input = decodeCompileInput(encodeCompileInput(object.input));
    } catch {
      throw new EngineClientInputError("INVALID_ARGUMENT");
    }
    let collected: readonly BlobRef[];
    try {
      collected = validateCollectedRefs(
        this.collectors.collectCompileInputBlobRefs(input),
        true,
      );
    } catch (error) {
      if (error instanceof EngineClientInputError) throw error;
      throw new EngineClientInputError("INVALID_BLOB_TABLE");
    }
    const expected = uniqueRefs(collected, "input");
    if (!strictArray(object.blobs)) {
      throw new EngineClientInputError("INVALID_BLOB_TABLE");
    }
    if (object.blobs.length !== expected.size) {
      throw new EngineClientInputError(
        "INVALID_BLOB_TABLE",
        safeCountDetails("blobCount", object.blobs.length, expected.size),
      );
    }
    if (object.blobs.length > limits.maxBuffers) {
      throw new EngineClientInputError(
        "LIMIT_EXCEEDED",
        safeCountDetails("blobCount", object.blobs.length, limits.maxBuffers),
      );
    }

    const seen = new Set<string>();
    const seenBuffers = new Set<ArrayBuffer>();
    const attachments: Array<Readonly<{
      ref: BlobRef;
      bytes: Uint8Array | ArrayBuffer;
      ownership: "copy" | "transfer";
      byteLength: number;
    }>> = [];
    let totalBytes = 0;
    for (const raw of object.blobs) {
      const blob = dataObject(raw, ["ref", "bytes"], ["ownership"]);
      if (blob === undefined) {
        throw new EngineClientInputError("INVALID_BLOB_TABLE");
      }
      if (!isBlobRef(blob.ref) || blob.ref.lifetime !== "request") {
        throw new EngineClientInputError("INVALID_BLOB_TABLE");
      }
      const candidateRef = blob.ref;
      const wanted = expected.get(candidateRef.blob_id);
      if (
        wanted === undefined ||
        seen.has(wanted.blob_id) ||
        blobRefFingerprint(candidateRef) !== blobRefFingerprint(wanted)
      ) {
        throw new EngineClientInputError("INVALID_BLOB_TABLE");
      }
      if ((utf8ByteLength(wanted.blob_id) ?? Infinity) > limits.maxBlobIdBytes) {
        throw new EngineClientInputError("LIMIT_EXCEEDED");
      }
      const ownership = blob.ownership ?? "copy";
      let byteLength: number | undefined;
      let sourceBuffer: ArrayBuffer | undefined;
      if (ownership === "copy") {
        const source = uint8ViewSource(blob.bytes);
        byteLength = source?.byteLength;
        sourceBuffer = source?.buffer;
      } else if (ownership === "transfer") {
        byteLength = fixedArrayBufferByteLength(blob.bytes);
        sourceBuffer = blob.bytes as ArrayBuffer;
      } else {
        throw new EngineClientInputError("UNSUPPORTED_BYTE_OWNERSHIP");
      }
      if (
        byteLength === undefined ||
        sourceBuffer === undefined ||
        seenBuffers.has(sourceBuffer)
      ) {
        throw new EngineClientInputError("UNSUPPORTED_BYTE_OWNERSHIP");
      }
      seenBuffers.add(sourceBuffer);
      if (BigInt(wanted.size) !== BigInt(byteLength)) {
        throw new EngineClientInputError("BLOB_SIZE_MISMATCH", {
          blobCount: expected.size,
        });
      }
      if (byteLength > limits.maxInputBlobBytes) {
        throw new EngineClientInputError("LIMIT_EXCEEDED");
      }
      totalBytes += byteLength;
      if (!Number.isSafeInteger(totalBytes) || totalBytes > limits.maxInputTotalBytes) {
        throw new EngineClientInputError("LIMIT_EXCEEDED");
      }
      seen.add(wanted.blob_id);
      attachments.push({
        ref: wanted,
        bytes: blob.bytes as Uint8Array | ArrayBuffer,
        ownership,
        byteLength,
      });
    }
    if (seen.size !== expected.size) {
      throw new EngineClientInputError("INVALID_BLOB_TABLE");
    }
    return { input, attachments: Object.freeze(attachments) };
  }

  private takeCompileBytes(
    metadata: ReturnType<EngineClientImplementation["prepareCompileMetadata"]>,
    limits: InternalTransportLimits,
  ): PreparedCompile {
    const blobs: OwnedRequestBlob[] = [];
    let totalBytes = 0;
    for (const attachment of metadata.attachments) {
      let bytes: ArrayBuffer;
      try {
        if (attachment.ownership === "transfer") {
          bytes = this.runtime.transferArrayBuffer(attachment.bytes as ArrayBuffer);
        } else {
          const source = uint8ViewSource(attachment.bytes);
          if (source === undefined) {
            throw new EngineClientInputError("UNSUPPORTED_BYTE_OWNERSHIP");
          }
          const copy = new Uint8Array(source.byteLength);
          copy.set(
            new Uint8Array(source.buffer, source.byteOffset, source.byteLength),
          );
          bytes = copy.buffer;
        }
      } catch (error) {
        if (error instanceof EngineClientInputError) throw error;
        throw new EngineClientInputError("UNSUPPORTED_BYTE_OWNERSHIP");
      }
      totalBytes += attachment.byteLength;
      blobs.push({ ref: attachment.ref, bytes });
    }
    if (totalBytes > limits.maxInputTotalBytes) {
      throw new EngineClientBackpressureError("BYTE_BUDGET_EXCEEDED");
    }
    return Object.freeze({
      input: metadata.input,
      blobs: Object.freeze(blobs),
    });
  }

  private async executeCompile(
    active: ActiveCompile,
    endpoint: EndpointContext,
    prepared: PreparedCompile,
    options: NormalizedCompileOptions,
  ): Promise<CompileOutcome> {
    let timer: InternalTimerHandle | undefined;
    let removeAbort: (() => void) | undefined;
    let exchange: InternalTransportExchange | undefined;
    const digests = new DigestSequence(this.runtime);
    const deadlineMs = this.runtime.now() + options.timeoutMs;
    try {
      timer = this.runtime.setTimer(() => {
        active.interrupt.resolve({ kind: "cancel", reason: "timeout" });
      }, options.timeoutMs);
      if (options.signal !== undefined) {
        const listener = (): void => {
          active.interrupt.resolve({ kind: "cancel", reason: "signal" });
        };
        try {
          options.signal.addEventListener("abort", listener, { once: true });
          removeAbort = (): void => {
            try {
              options.signal?.removeEventListener("abort", listener);
            } catch {
              // Hostile signal cleanup cannot expose its thrown value.
            }
          };
          if (signalAborted(options.signal)) listener();
        } catch {
          throw new EngineClientInputError("INVALID_ARGUMENT");
        }
      }

      const verified = this.verifyInputDigests(prepared, digests);
      const beforeSend = await Promise.race([
        verified.then(
          () => ({ kind: "verified" as const }),
          (error: unknown) => ({ kind: "verify-fault" as const, error }),
        ),
        active.interrupt.promise,
      ]);
      if (beforeSend.kind === "cancel") {
        await digests.abortAndJoin();
        await verified.catch(() => undefined);
        active.joined.resolve();
        if (beforeSend.barrier !== undefined) await beforeSend.barrier;
        return clientCancellation(active, beforeSend.reason);
      }
      if (beforeSend.kind === "fault") {
        await digests.abortAndJoin();
        await verified.catch(() => undefined);
        active.joined.resolve();
        throw await this.replaceForFault(beforeSend.fault, active);
      }
      if (beforeSend.kind === "verify-fault") throw beforeSend.error;

      const envelope = {
        deadline_at: new Date(deadlineMs).toISOString(),
        operation: "engine.compile",
        payload: prepared.input,
        protocol: ENGINE_PROTOCOL,
        request_id: active.requestId,
      } satisfies CompileRequestEnvelope;
      const controlText = encodeCompileRequestEnvelope(envelope);
      validateOutgoingControl(controlText, endpoint.limits);
      const control = encodeControl(controlText);
      const transportBlobs = prepared.blobs
        .map((blob) => ({ blobId: blob.ref.blob_id, bytes: blob.bytes }))
        .sort((left, right) => compareUtf8(left.blobId, right.blobId));
      try {
        exchange = endpoint.transport.request({
          exchangeId: this.nextExchangeId(),
          control,
          blobs: transportBlobs,
        });
      } catch {
        throw await this.replaceForFault(
          new EngineClientTransportError("TRANSFER_FAILED"),
          active,
        );
      }

      const terminal = this.observeCompileTerminal(
        exchange,
        endpoint,
        active.requestId,
        digests,
      );
      const winner = await Promise.race([
        terminal.then((result) =>
          result.kind === "valid"
            ? ({ kind: "response" as const, outcome: result.outcome })
            : ({ kind: "response-fault" as const, error: result.error }),
        ),
        active.interrupt.promise,
      ]);
      if (winner.kind === "cancel") {
        await digests.abortAndJoin();
        const cancelResult = await this.cancelExchange(exchange);
        const terminalResult = await this.withBound(
          terminal,
          this.options.cancelGraceMs,
          undefined,
        );
        const terminalIsInvalid = terminalResult?.kind === "invalid";
        const reusable =
          cancelResult.reusable &&
          !terminalIsInvalid &&
          terminalResult !== undefined;
        const engineCancellation =
          terminalResult?.kind === "valid" &&
          terminalResult.outcome.origin === "engine" &&
          terminalResult.outcome.outcome === "cancelled"
            ? terminalResult.outcome
            : undefined;
        active.joined.resolve();
        if (
          !reusable &&
          winner.reason !== "restart" &&
          winner.reason !== "dispose"
        ) {
          await this.replaceAfterCancellation(active);
        }
        if (winner.barrier !== undefined) await winner.barrier;
        return engineCancellation ?? clientCancellation(active, winner.reason);
      }
      if (winner.kind === "fault") {
        await digests.abortAndJoin();
        active.joined.resolve();
        throw await this.replaceForFault(winner.fault, active);
      }
      if (winner.kind === "response-fault") {
        await digests.abortAndJoin();
        active.joined.resolve();
        throw await this.replaceForFault(winner.error, active);
      }

      await digests.abortAndJoin();
      active.joined.resolve();
      return winner.outcome;
    } finally {
      removeAbort?.();
      if (timer !== undefined) this.runtime.clearTimer(timer);
      await digests.abortAndJoin();
      active.joined.resolve();
    }
  }

  private async verifyInputDigests(
    prepared: PreparedCompile,
    digests: DigestSequence,
  ): Promise<void> {
    for (const blob of prepared.blobs) {
      let digestBytes: Uint8Array;
      try {
        digestBytes = await digests.sha256(new Uint8Array(blob.bytes));
      } catch {
        throw new EngineClientTransportError("DIGEST_FAILED");
      }
      const digest = bytesToHex(digestBytes);
      if (`sha256:${digest}` !== blob.ref.digest) {
        throw new EngineClientInputError("BLOB_DIGEST_MISMATCH", {
          blobCount: prepared.blobs.length,
        });
      }
    }
  }

  private observeCompileTerminal(
    exchange: InternalTransportExchange,
    endpoint: EndpointContext,
    requestId: string,
    digests: DigestSequence,
  ): Promise<CompileTerminal> {
    return exchange.response.then(
      async (raw): Promise<CompileTerminal> => {
        try {
          return {
            kind: "valid",
            outcome: await this.decodeCompileOutcome(
              raw,
              endpoint,
              requestId,
              digests,
            ),
          };
        } catch (error) {
          return { kind: "invalid", error };
        }
      },
      (error: unknown): CompileTerminal => ({ kind: "rejected", error }),
    );
  }

  private async decodeCompileOutcome(
    raw: InternalTransportResponse,
    endpoint: EndpointContext,
    requestId: string,
    digests: DigestSequence,
  ): Promise<CompileOutcome> {
    const response = validateTransportResponse(raw, endpoint.limits);
    let envelope: CompileResponseEnvelope;
    try {
      envelope = decodeCompileResponseEnvelope(
        decodeControl(response.control, endpoint.limits.maxControlDepth),
      );
    } catch {
      throw new EngineClientDecodeError("MALFORMED_MESSAGE");
    }
    if (envelope.request_id !== requestId) {
      throw new EngineClientDecodeError("CORRELATION_MISMATCH", {
        generation: endpoint.generation,
      });
    }
    if (
      envelope.protocol.name !== "engine" ||
      envelope.protocol.version !== "1.0"
    ) {
      throw new EngineClientDecodeError("PROTOCOL_MISMATCH");
    }
    if (envelope.engine_release !== endpoint.engineRelease) {
      throw new EngineClientDecodeError("PROTOCOL_MISMATCH");
    }
    if (envelope.outcome !== "success") {
      if (response.blobs.length !== 0) {
        throw new EngineClientDecodeError("UNEXPECTED_BLOB");
      }
      return deepFreeze({
        origin: "engine",
        outcome: envelope.outcome,
        response: envelope,
        blobs: [] as const,
      }) as CompileOutcome;
    }

    let collected: readonly BlobRef[];
    try {
      collected = validateCollectedRefs(
        this.collectors.collectCompileResultBlobRefs(envelope.payload!),
        true,
      );
    } catch {
      throw new EngineClientDecodeError("MALFORMED_MESSAGE");
    }
    const expected = uniqueRefs(collected, "output");
    if (response.blobs.length < expected.size) {
      throw new EngineClientDecodeError("MISSING_BLOB");
    }
    if (response.blobs.length > expected.size) {
      throw new EngineClientDecodeError("UNEXPECTED_BLOB");
    }
    const received = new Map<string, ArrayBuffer>();
    for (const blob of response.blobs) {
      const ref = expected.get(blob.blobId);
      if (ref === undefined || received.has(blob.blobId)) {
        throw new EngineClientDecodeError("UNEXPECTED_BLOB");
      }
      const length = fixedArrayBufferByteLength(blob.bytes);
      if (length === undefined || BigInt(ref.size) !== BigInt(length)) {
        throw new EngineClientDecodeError("OUTPUT_SIZE_MISMATCH");
      }
      let owned: ArrayBuffer;
      try {
        owned = this.runtime.transferArrayBuffer(blob.bytes);
      } catch {
        throw new EngineClientDecodeError("MALFORMED_MESSAGE");
      }
      let digestBytes: Uint8Array;
      try {
        digestBytes = await digests.sha256(new Uint8Array(owned));
      } catch {
        throw new EngineClientTransportError("DIGEST_FAILED");
      }
      const digest = bytesToHex(digestBytes);
      if (`sha256:${digest}` !== ref.digest) {
        throw new EngineClientDecodeError("OUTPUT_DIGEST_MISMATCH");
      }
      received.set(blob.blobId, owned);
    }
    const outputs: OutputBlob[] = [];
    for (const ref of expected.values()) {
      const bytes = received.get(ref.blob_id);
      if (bytes === undefined) throw new EngineClientDecodeError("MISSING_BLOB");
      outputs.push(deepFreeze({ ref, bytes: new Uint8Array(bytes) }));
    }
    return deepFreeze({
      origin: "engine",
      outcome: "success",
      response: envelope as unknown as CompileSuccessResponse,
      blobs: outputs,
    });
  }

  private async cancelExchange(
    exchange: InternalTransportExchange,
  ): Promise<InternalCancelResult> {
    try {
      const raw = await this.withBound(
        Promise.resolve().then(() => exchange.cancel()),
        this.options.cancelGraceMs,
        undefined,
      );
      const result = dataObject(raw, ["reusable"]);
      if (result === undefined || typeof result.reusable !== "boolean") {
        return { reusable: false };
      }
      return Object.freeze({ reusable: result.reusable });
    } catch {
      return { reusable: false };
    }
  }

  private async replaceAfterCancellation(active: ActiveCompile): Promise<void> {
    try {
      await this.startReplacement(active);
    } catch {
      // The client state reports a failed replacement; cancellation stays a value.
    }
  }

  private async replaceForFault(
    raw: unknown,
    active: ActiveCompile,
  ): Promise<Error> {
    const error = publicFault(raw);
    let replacementSucceeded = false;
    try {
      await this.startReplacement(active);
      replacementSucceeded = this._state === "ready";
    } catch {
      replacementSucceeded = false;
    }
    const details = Object.freeze({
      ...(error.details ?? {}),
      replacementSucceeded,
    });
    if (error instanceof EngineClientDecodeError) {
      return new EngineClientDecodeError(error.code as never, details);
    }
    return new EngineClientTransportError(
      error.code as never,
      error.retryable,
      details,
    );
  }

  private startReplacement(
    skipActive?: ActiveCompile,
    barrier?: Deferred<void>,
  ): Promise<void> {
    if (this.replacementPromise !== undefined) return this.replacementPromise;
    this._state = "replacing";
    const promise = (async (): Promise<void> => {
      let next: EndpointContext | undefined;
      try {
        const active = this.active;
        if (active !== undefined && active !== skipActive) {
          active.interrupt.resolve({
            kind: "cancel",
            reason: "restart",
            ...(barrier === undefined ? {} : { barrier: barrier.promise }),
          });
          await active.joined.promise;
        }
        const old = this.endpoint;
        this.endpoint = undefined;
        if (old !== undefined) await this.shutdownTransport(old);
        if (this.isTerminating()) return;
        const candidateGeneration = this.lastGeneration + 1;
        this.lastGeneration = candidateGeneration;
        next = await this.openEndpoint(
          candidateGeneration,
          this.lastEndpointInstanceId,
        );
        if (this.isTerminating()) {
          await this.shutdownTransport(next);
          return;
        }
        this.publishEndpoint(next);
        await Promise.resolve();
        if (this.endpoint !== next || this._state !== "ready") {
          throw new EngineClientTransportError("REPLACEMENT_FAILED");
        }
      } catch {
        if (!this.isTerminating()) {
          if (next !== undefined) {
            if (this.endpoint === next) this.endpoint = undefined;
            try {
              await this.shutdownTransport(next);
            } catch {
              // Replacement always reports one stable failure classification.
            }
          }
          this._state = "failed";
          throw new EngineClientTransportError("REPLACEMENT_FAILED");
        }
      }
    })();
    const tracked = promise.finally(() => {
      barrier?.resolve();
      if (this.replacementPromise === tracked) this.replacementPromise = undefined;
      if (this._state === "replacing") this._state = "failed";
    });
    this.replacementPromise = tracked;
    return tracked;
  }

  private async openEndpoint(
    generation: number,
    previousInstanceId: string | undefined,
  ): Promise<EndpointContext> {
    const deadline = this.runtime.now() + this.options.handshakeTimeoutMs;
    const abort = deferred<void>();
    this.pendingOpenAbort = abort;
    let admitted: AdmittedTransport;
    try {
      admitted = admitTransport(
        this.factory.create(`eg1-${this.nonce}-${generation.toString(16)}`),
      );
    } catch {
      throw new EngineClientTransportError(this.factory.connectFailureCode);
    }
    const transport = admitted.transport;
    this.pendingTransport = admitted;
    let exchange: InternalTransportExchange | undefined;
    try {
      const limits = validateTransportLimits(
        await this.openStage(transport.ready, deadline, abort),
      );
      const requestId = this.nextRequestId();
      const payload = {
        client_release: CLIENT_RELEASE,
        ...(this.options.clientLimits === undefined
          ? {}
          : { client_limits: this.options.clientLimits }),
        optional_capabilities: this.options.optionalCapabilities,
        protocols: [
          {
            name: "engine",
            supported_range: "1.0..1.0",
            versions: [{ schema_digest: schemaDigest, version: "1.0" }],
          },
        ],
        required_capabilities: this.options.requiredCapabilities,
      };
      const envelope = {
        deadline_at: new Date(deadline).toISOString(),
        operation: "engine.handshake",
        payload,
        protocol: ENGINE_PROTOCOL,
        request_id: requestId,
      } satisfies HandshakeRequestEnvelope;
      const controlText = encodeHandshakeRequestEnvelope(envelope);
      validateOutgoingControl(controlText, limits);
      const control = encodeControl(controlText);
      try {
        exchange = transport.request({
          exchangeId: this.nextExchangeId(),
          control,
          blobs: [],
        });
      } catch {
        throw new EngineClientTransportError("TRANSFER_FAILED");
      }
      const response = validateTransportResponse(
        await this.openStage(exchange.response, deadline, abort),
        limits,
      );
      if (response.blobs.length !== 0) {
        throw new EngineClientDecodeError("UNEXPECTED_BLOB");
      }
      let handshake: HandshakeResponseEnvelope;
      try {
        handshake = decodeHandshakeResponseEnvelope(
          decodeControl(response.control, limits.maxControlDepth),
        );
      } catch {
        throw new EngineClientDecodeError("MALFORMED_MESSAGE");
      }
      const validated = validateHandshake(
        handshake,
        requestId,
        this.factory.transportId,
        this.options,
        previousInstanceId,
      );
      return {
        generation,
        transport,
        closed: admitted.closed,
        limits,
        snapshot: deepFreeze({ generation, handshake: validated.result }),
        capabilityStatuses: validated.statuses,
        engineRelease: validated.result.host_release,
      };
    } catch (error) {
      if (error instanceof CreationTimedOut) {
        if (exchange !== undefined) await this.cancelExchange(exchange);
        try {
          transport.terminate();
        } catch {
          // Raw platform failures are discarded.
        }
        await this.shutdownTransport(admitted);
        throw new EngineClientTransportError("TIMEOUT_DURING_CREATION");
      }
      try {
        transport.terminate();
      } catch {
        // Raw platform failures are discarded.
      }
      await this.shutdownTransport(admitted);
      if (error instanceof OpenAborted) throw error;
      throw publicFault(error, this.factory.connectFailureCode);
    } finally {
      if (this.pendingTransport === admitted) this.pendingTransport = undefined;
      if (this.pendingOpenAbort === abort) this.pendingOpenAbort = undefined;
    }
  }

  private async openStage<T>(
    promise: Promise<T>,
    deadline: number,
    abort: Deferred<void>,
  ): Promise<T> {
    const remaining = deadline - this.runtime.now();
    if (remaining <= 0) throw new CreationTimedOut();
    let timer: InternalTimerHandle | undefined;
    try {
      return await Promise.race([
        promise,
        new Promise<never>((_resolve, reject) => {
          timer = this.runtime.setTimer(
            () => reject(new CreationTimedOut()),
            remaining,
          );
        }),
        abort.promise.then(() => {
          throw new OpenAborted();
        }),
      ]);
    } finally {
      if (timer !== undefined) this.runtime.clearTimer(timer);
    }
  }

  private publishEndpoint(endpoint: EndpointContext): void {
    this.lastGeneration = endpoint.generation;
    this.lastEndpointInstanceId =
      endpoint.snapshot.handshake.endpoint_instance_id;
    this.endpoint = endpoint;
    this._state = "ready";
    const observe = (raw: unknown): void => {
      try {
        const close = raw === UNAVAILABLE_TRANSPORT_CLOSE
          ? undefined
          : validateTransportClose(raw);
        const error = close === undefined
          ? this.unexpectedCloseFallback()
          : new EngineClientTransportError(
            close.code,
            close.retryable,
            close.details,
          );
        this.handleUnexpectedClose(endpoint, error);
      } catch {
        try {
          this.handleUnexpectedClose(endpoint, this.unexpectedCloseFallback());
        } catch {
          // The close observer is a terminal safety boundary and never rejects.
        }
      }
    };
    void endpoint.closed.then(
      observe,
      () => observe(UNAVAILABLE_TRANSPORT_CLOSE),
    ).catch(() => undefined);
  }

  private unexpectedCloseFallback(): EngineClientTransportError {
    return new EngineClientTransportError(
      this.factory.transportId === "stdio" ? "BROKEN_PIPE" : "WORKER_CRASHED",
      true,
    );
  }

  private handleUnexpectedClose(
    endpoint: EndpointContext,
    error: EngineClientTransportError,
  ): void {
    if (this.endpoint !== endpoint || this._state !== "ready") return;
    if (this.replacementPromise !== undefined) {
      this.active?.interrupt.resolve({ kind: "fault", fault: error });
      this.endpoint = undefined;
      this._state = "failed";
      void this.shutdownTransport(endpoint).catch(() => undefined);
      return;
    }
    if (this.active !== undefined) {
      this.active.interrupt.resolve({ kind: "fault", fault: error });
      void this.startReplacement().catch(() => undefined);
      return;
    }
    void this.startReplacement().catch(() => undefined);
  }

  private isTerminating(): boolean {
    return this._state === "disposing" || this._state === "disposed";
  }

  private shutdownTransport(
    admitted: AdmittedTransport,
  ): Promise<boolean> {
    const existing = this.transportShutdowns.get(admitted.transport);
    if (existing !== undefined) return existing;
    const shutdown = this.performTransportShutdown(admitted);
    this.transportShutdowns.set(admitted.transport, shutdown);
    return shutdown;
  }

  private async performTransportShutdown(
    admitted: AdmittedTransport,
  ): Promise<boolean> {
    try {
      const graceful = Promise.resolve().then(() =>
        admitted.transport.dispose()
      );
      const gracefulResult = await this.settleWithin(
        graceful,
        this.options.disposeTimeoutMs,
      );
      if (!gracefulResult) {
        try {
          admitted.transport.terminate();
        } catch {
          // The stable unclean result below is the complete public outcome.
        }
      }
      const terminal = await this.withBound(
        admitted.closed.then(
          (value) => value === UNAVAILABLE_TRANSPORT_CLOSE ? false : true,
          () => false,
        ),
        this.options.disposeTimeoutMs,
        undefined,
      );
      const joined = terminal === true;
      if (!joined && gracefulResult) {
        try {
          admitted.transport.terminate();
        } catch {
          // A hostile hard-stop method cannot escape this boundary.
        }
      }
      return gracefulResult && joined;
    } catch {
      try {
        admitted.transport.terminate();
      } catch {
        // A hostile hard-stop method cannot escape this boundary.
      }
      return false;
    }
  }

  private async settleWithin(
    promise: Promise<unknown>,
    timeoutMs: number,
  ): Promise<boolean> {
    const marker = Object.freeze({});
    const result = await this.withBound(
      promise.then(
        () => true,
        () => false,
      ),
      timeoutMs,
      marker,
    );
    return result === true;
  }

  private async withBound<T, F>(
    promise: Promise<T>,
    timeoutMs: number,
    fallback: F,
  ): Promise<T | F> {
    let timer: InternalTimerHandle | undefined;
    try {
      return await Promise.race([
        promise,
        new Promise<F>((resolve) => {
          timer = this.runtime.setTimer(() => resolve(fallback), timeoutMs);
        }),
      ]);
    } finally {
      if (timer !== undefined) this.runtime.clearTimer(timer);
    }
  }
}

interface NormalizedCompileOptions {
  readonly requestId?: string;
  readonly signal?: EngineAbortSignal;
  readonly timeoutMs: number;
}

function normalizeCompileOptions(
  input: CompileOptions | undefined,
  creation: NormalizedCreationOptions,
): NormalizedCompileOptions {
  if (input === undefined) {
    return { timeoutMs: creation.defaultCompileTimeoutMs };
  }
  const value = dataObject(input, [], ["requestId", "signal", "timeoutMs"]);
  if (value === undefined) throw new EngineClientInputError("INVALID_ARGUMENT");
  if (value.requestId !== undefined && !validRequestId(value.requestId)) {
    throw new EngineClientInputError("INVALID_REQUEST_ID");
  }
  if (value.signal !== undefined && !validSignal(value.signal)) {
    throw new EngineClientInputError("INVALID_ARGUMENT");
  }
  return {
    ...(value.requestId === undefined
      ? {}
      : { requestId: value.requestId as string }),
    ...(value.signal === undefined
      ? {}
      : { signal: value.signal as EngineAbortSignal }),
    timeoutMs: timeoutOption(value.timeoutMs, creation.defaultCompileTimeoutMs),
  };
}

function validSignal(value: unknown): value is EngineAbortSignal {
  if (typeof value !== "object" || value === null) return false;
  try {
    const signal = value as EngineAbortSignal;
    return (
      typeof signal.aborted === "boolean" &&
      typeof signal.addEventListener === "function" &&
      typeof signal.removeEventListener === "function"
    );
  } catch {
    return false;
  }
}

function signalAborted(signal: EngineAbortSignal): boolean {
  try {
    return signal.aborted;
  } catch {
    throw new EngineClientInputError("INVALID_ARGUMENT");
  }
}

function clientCancellation(
  active: ActiveCompile,
  reason: ClientCancellationReason,
): CompileOutcome {
  return deepFreeze({
    origin: "client",
    outcome: "cancelled",
    requestId: active.requestId,
    endpointGeneration: active.generation,
    reason,
    blobs: [] as const,
  });
}

function uniqueRefs(
  refs: readonly BlobRef[],
  side: "input" | "output",
): ReadonlyMap<string, BlobRef> {
  const unique = new Map<string, BlobRef>();
  for (const ref of refs) {
    const prior = unique.get(ref.blob_id);
    if (prior !== undefined && blobRefFingerprint(prior) !== blobRefFingerprint(ref)) {
      if (side === "input") {
        throw new EngineClientInputError("INVALID_BLOB_TABLE");
      }
      throw new EngineClientDecodeError("MALFORMED_MESSAGE");
    }
    if (prior === undefined) unique.set(ref.blob_id, ref);
  }
  return unique;
}

function validateTransportLimits(value: unknown): InternalTransportLimits {
  const object = dataObject(value, [
    "maxControlBytes",
    "maxControlDepth",
    "maxBlobIdBytes",
    "maxBuffers",
    "maxInputBlobBytes",
    "maxInputTotalBytes",
    "maxOutputBlobBytes",
    "maxOutputTotalBytes",
    "maxResponsePublishBytes",
  ]);
  if (object === undefined) {
    throw new EngineClientTransportError("CONNECT_FAILED");
  }
  for (const entry of Object.values(object)) {
    if (
      typeof entry !== "number" ||
      !Number.isSafeInteger(entry) ||
      entry <= 0
    ) {
      throw new EngineClientTransportError("CONNECT_FAILED");
    }
  }
  const limits = object as unknown as InternalTransportLimits;
  if (
    limits.maxInputBlobBytes > limits.maxInputTotalBytes ||
    limits.maxOutputBlobBytes > limits.maxOutputTotalBytes ||
    limits.maxControlBytes + limits.maxOutputTotalBytes >
      limits.maxResponsePublishBytes
  ) {
    throw new EngineClientTransportError("CONNECT_FAILED");
  }
  return deepFreeze({ ...limits });
}

function validateTransportResponse(
  value: unknown,
  limits: InternalTransportLimits,
): InternalTransportResponse {
  const object = dataObject(value, ["control", "blobs"]);
  const controlLength = fixedArrayBufferByteLength(object?.control);
  if (
    object === undefined ||
    controlLength === undefined ||
    controlLength > limits.maxControlBytes ||
    !strictArray(object.blobs) ||
    object.blobs.length > limits.maxBuffers
  ) {
    throw new EngineClientDecodeError("MALFORMED_MESSAGE");
  }
  let prior: string | undefined;
  let total = 0;
  const blobs: InternalTransportBlob[] = [];
  for (const raw of object.blobs) {
    const blob = dataObject(raw, ["blobId", "bytes"]);
    const length = fixedArrayBufferByteLength(blob?.bytes);
    if (
      blob === undefined ||
      typeof blob.blobId !== "string" ||
      (utf8ByteLength(blob.blobId) ?? 0) < 1 ||
      (utf8ByteLength(blob.blobId) ?? Infinity) > limits.maxBlobIdBytes ||
      (prior !== undefined && compareUtf8(prior, blob.blobId) >= 0) ||
      length === undefined ||
      length > limits.maxOutputBlobBytes
    ) {
      throw new EngineClientDecodeError("MALFORMED_MESSAGE");
    }
    total += length;
    if (
      !Number.isSafeInteger(total) ||
      total > limits.maxOutputTotalBytes ||
      controlLength + total > limits.maxResponsePublishBytes
    ) {
      throw new EngineClientDecodeError("MALFORMED_MESSAGE");
    }
    prior = blob.blobId;
    blobs.push({ blobId: blob.blobId, bytes: blob.bytes as ArrayBuffer });
  }
  return { control: object.control as ArrayBuffer, blobs };
}

function validateTransportClose(value: unknown): InternalTransportClose | undefined {
  const object = dataObject(value, ["code", "retryable"], ["details"]);
  if (
    object === undefined ||
    (object.code !== "BROKEN_PIPE" &&
      object.code !== "PROCESS_EXITED" &&
      object.code !== "WORKER_CRASHED") ||
    typeof object.retryable !== "boolean"
  ) {
    return undefined;
  }
  const details = snapshotSafeDetails(object.details);
  if (!details.valid) return undefined;
  return Object.freeze({
    code: object.code,
    retryable: object.retryable,
    ...(details.details === undefined ? {} : { details: details.details }),
  });
}

function validateHandshake(
  response: HandshakeResponseEnvelope,
  requestId: string,
  transportId: string,
  options: NormalizedCreationOptions,
  previousInstanceId: string | undefined,
): Readonly<{
  result: HandshakeResult;
  statuses: ReadonlyMap<string, boolean>;
}> {
  if (response.request_id !== requestId) {
    throw new EngineClientDecodeError("CORRELATION_MISMATCH");
  }
  if (
    response.protocol.name !== "engine" ||
    response.protocol.version !== "1.0"
  ) {
    throw new EngineClientDecodeError("PROTOCOL_MISMATCH");
  }
  if (response.outcome !== "success" || response.payload === undefined) {
    const details: Record<string, string | number | boolean> = {
      outcome: response.outcome,
      requestId,
      diagnosticCount: response.diagnostics.length,
    };
    if (response.failure !== undefined) details.failureCode = response.failure.code;
    throw new EngineClientTransportError(
      "NEGOTIATION_REJECTED",
      response.failure?.retryable ?? false,
      details,
    );
  }
  const result = response.payload;
  const protocol = result.negotiated_protocols.find(
    (candidate) => candidate.name === "engine",
  );
  if (
    protocol?.version !== "1.0" ||
    protocol.schema_digest !== schemaDigest ||
    response.engine_release !== result.host_release ||
    result.release_manifest_digest !== options.expectedReleaseManifestDigest ||
    (previousInstanceId !== undefined &&
      result.endpoint_instance_id === previousInstanceId)
  ) {
    throw new EngineClientDecodeError("PROTOCOL_MISMATCH");
  }
  if (!result.capability_manifest.transports.includes(transportId)) {
    throw new EngineClientDecodeError("PROTOCOL_MISMATCH");
  }
  const operation = result.capability_manifest.operations["engine.compile"];
  if (operation?.enabled !== true || operation.protocol_version !== "1.0") {
    throw new EngineClientTransportError("NEGOTIATION_REJECTED", false, {
      requestId,
    });
  }
  const requested = new Set([
    ...options.requiredCapabilities,
    ...options.optionalCapabilities,
  ]);
  const statuses = new Map<string, boolean>();
  for (const status of result.capability_statuses) {
    if (!requested.has(status.capability_id) || statuses.has(status.capability_id)) {
      throw new EngineClientDecodeError("MALFORMED_MESSAGE");
    }
    if (status.protocol_version !== "1.0") {
      throw new EngineClientDecodeError("PROTOCOL_MISMATCH");
    }
    statuses.set(status.capability_id, status.enabled);
  }
  if (
    statuses.size !== requested.size ||
    options.requiredCapabilities.some(
      (capability) => statuses.get(capability) !== true,
    )
  ) {
    throw new EngineClientTransportError("NEGOTIATION_REJECTED", false, {
      requestId,
    });
  }
  return {
    result: deepFreeze(result),
    statuses: new Map(statuses),
  };
}

function encodeControl(text: string): ArrayBuffer {
  const bytes = new TextEncoder().encode(text);
  const copy = new Uint8Array(bytes.byteLength);
  copy.set(bytes);
  return copy.buffer;
}

function decodeControl(buffer: ArrayBuffer, maxDepth: number): string {
  try {
    const text = new TextDecoder("utf-8", { fatal: true }).decode(buffer);
    if (controlDepth(text) > maxDepth) {
      throw new EngineClientDecodeError("MALFORMED_MESSAGE");
    }
    return text;
  } catch {
    throw new EngineClientDecodeError("MALFORMED_MESSAGE");
  }
}

function validateOutgoingControl(
  text: string,
  limits: InternalTransportLimits,
): void {
  const bytes = utf8ByteLength(text);
  if (
    bytes === undefined ||
    bytes > limits.maxControlBytes ||
    controlDepth(text) > limits.maxControlDepth
  ) {
    throw new EngineClientInputError("LIMIT_EXCEEDED");
  }
}

function controlDepth(text: string): number {
  let depth = 0;
  let maximum = 0;
  let inString = false;
  let escaped = false;
  for (const character of text) {
    if (inString) {
      if (escaped) {
        escaped = false;
      } else if (character === "\\") {
        escaped = true;
      } else if (character === '"') {
        inString = false;
      }
      continue;
    }
    if (character === '"') {
      inString = true;
    } else if (character === "{" || character === "[") {
      depth++;
      maximum = Math.max(maximum, depth);
    } else if (character === "}" || character === "]") {
      depth--;
    }
  }
  return maximum;
}

function inputFault(error: unknown): EngineClientInputError {
  if (error instanceof EngineClientInputError) return error;
  return new EngineClientInputError("INVALID_ARGUMENT");
}

function publicFault(
  value: unknown,
  fallback: "SPAWN_FAILED" | "CONNECT_FAILED" | "BROKEN_PIPE" = "BROKEN_PIPE",
): EngineClientTransportError | EngineClientDecodeError {
  if (value instanceof EngineClientDecodeError) return value;
  if (value instanceof EngineClientTransportError) return value;
  if (value instanceof InternalTransportFault) {
    if (value.kind === "decode") {
      return new EngineClientDecodeError(value.code as never, value.details);
    }
    return new EngineClientTransportError(
      value.code as never,
      value.retryable,
      value.details,
    );
  }
  return new EngineClientTransportError(fallback);
}
