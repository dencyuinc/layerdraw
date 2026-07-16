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
  ListModulesInput,
  ListModulesRequestEnvelope,
  ListModulesResponseEnvelope,
  OpenDocumentInput,
  OpenDocumentRequestEnvelope,
  OpenDocumentResponseEnvelope,
  ReadModulesInput,
  ReadModulesRequestEnvelope,
  ReadModulesResponseEnvelope,
} from "@layerdraw/protocol/engine";
import {
  decodeCompileInput,
  decodeCompileResponseEnvelope,
  decodeHandshakeResponseEnvelope,
  decodeListModulesResponseEnvelope,
  decodeOpenDocumentResponseEnvelope,
  decodeReadModulesResponseEnvelope,
  encodeCompileInput,
  encodeCompileRequestEnvelope,
  encodeHandshakeRequestEnvelope,
  encodeListModulesRequestEnvelope,
  encodeOpenDocumentRequestEnvelope,
  encodeReadModulesRequestEnvelope,
  isCompileInput,
  isListModulesInput,
  isOpenDocumentInput,
  isReadModulesInput,
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
  type EngineClientSafeDetails,
  type EngineClientState,
  type EngineEndpointSnapshot,
  type OutputBlob,
  type PortableCompileRequest,
  type WorkbenchOutcome,
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
const nativePromiseThen = Promise.prototype.then;

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

interface AdmittedTransportFactory {
  readonly transportId: string;
  readonly connectFailureCode: "SPAWN_FAILED" | "CONNECT_FAILED";
  create(endpointGeneration: string): unknown;
}

interface EndpointContext extends AdmittedTransport {
  readonly generation: number;
  readonly limits: InternalTransportLimits;
  readonly snapshot: EngineEndpointSnapshot;
  readonly capabilityStatuses: ReadonlyMap<string, boolean>;
  readonly engineRelease: string;
}

type TransportPromiseSettlement<T> =
  | Readonly<{ kind: "fulfilled"; value: T }>
  | Readonly<{ kind: "rejected"; reason: unknown }>
  | Readonly<{ kind: "unavailable" }>;

interface CapturedTransportPromise<T> {
  readonly promise: Promise<TransportPromiseSettlement<T>>;
  readonly valid: boolean;
  subscribe(observer: (settlement: TransportPromiseSettlement<T>) => void): void;
}

interface AdmittedTransport {
  readonly transport: InternalByteTransport;
  readonly ready: Promise<TransportPromiseSettlement<InternalTransportLimits>>;
  readonly closed: Promise<TransportPromiseSettlement<InternalTransportClose>>;
  readonly observeClosed: CapturedTransportPromise<InternalTransportClose>["subscribe"];
  readonly request?: (input: unknown) => unknown;
  readonly dispose?: () => unknown;
  readonly terminate?: () => unknown;
  readonly operationsValid: boolean;
}

interface AdmittedExchange {
  readonly response: Promise<
    TransportPromiseSettlement<InternalTransportResponse>
  >;
  readonly cancel?: () => unknown;
  readonly valid: boolean;
}

type TransportShutdownMode = "graceful" | "hard";

interface TransportShutdownRecord {
  readonly admitted: AdmittedTransport;
  readonly completion: Deferred<boolean>;
  hardRequested: boolean;
  hardStopAttempted: boolean;
  hardStopInvoked: boolean;
}

function admitTransportFactory(raw: unknown): AdmittedTransportFactory {
  if (
    (typeof raw !== "object" && typeof raw !== "function") ||
    raw === null
  ) {
    throw new EngineClientInputError("INVALID_ARGUMENT");
  }
  const transportId = captureProperty(raw, "transportId");
  const connectFailureCode = captureProperty(raw, "connectFailureCode");
  const createMethod = captureMethod(raw, "create");
  if (
    !transportId.valid ||
    typeof transportId.value !== "string" ||
    !/^[a-z][a-z0-9_]*$/.test(transportId.value) ||
    !connectFailureCode.valid ||
    (connectFailureCode.value !== "SPAWN_FAILED" &&
      connectFailureCode.value !== "CONNECT_FAILED") ||
    createMethod === undefined
  ) {
    throw new EngineClientInputError("INVALID_ARGUMENT");
  }
  return Object.freeze({
    transportId: transportId.value,
    connectFailureCode: connectFailureCode.value,
    create: (endpointGeneration: string): unknown =>
      Reflect.apply(createMethod, raw, [endpointGeneration]),
  });
}

function admitTransport(transport: unknown): AdmittedTransport {
  if (
    (typeof transport !== "object" && typeof transport !== "function") ||
    transport === null
  ) {
    throw new TypeError("Invalid transport");
  }
  const target = transport as InternalByteTransport;
  const closedProperty = captureProperty(target, "closed");
  const terminateMethod = captureMethod(target, "terminate");
  const disposeMethod = captureMethod(target, "dispose");
  const requestMethod = captureMethod(target, "request");
  const readyProperty = captureProperty(target, "ready");
  const ready = readyProperty.valid
    ? captureTransportPromise<InternalTransportLimits>(readyProperty.value)
    : unavailableTransportPromise<InternalTransportLimits>();
  const closed = closedProperty.valid
    ? captureTransportPromise<InternalTransportClose>(closedProperty.value)
    : unavailableTransportPromise<InternalTransportClose>();
  return Object.freeze({
    transport: target,
    ready: ready.promise,
    closed: closed.promise,
    observeClosed: closed.subscribe,
    ...(requestMethod === undefined
      ? {}
      : {
        request: (input: unknown): unknown =>
          Reflect.apply(requestMethod, target, [input]),
      }),
    ...(disposeMethod === undefined
      ? {}
      : {
        dispose: (): unknown => Reflect.apply(disposeMethod, target, []),
      }),
    ...(terminateMethod === undefined
      ? {}
      : {
        terminate: (): unknown => Reflect.apply(terminateMethod, target, []),
      }),
    operationsValid:
      requestMethod !== undefined &&
      disposeMethod !== undefined &&
      terminateMethod !== undefined,
  });
}

function admitExchange(raw: unknown): AdmittedExchange {
  if (
    (typeof raw !== "object" && typeof raw !== "function") ||
    raw === null
  ) {
    return Object.freeze({
      response: unavailableTransportPromise<InternalTransportResponse>().promise,
      valid: false,
    });
  }
  const cancelMethod = captureMethod(raw, "cancel");
  const responseProperty = captureProperty(raw, "response");
  const response = responseProperty.valid
    ? captureTransportPromise<InternalTransportResponse>(responseProperty.value)
    : unavailableTransportPromise<InternalTransportResponse>();
  return Object.freeze({
    response: response.promise,
    ...(cancelMethod === undefined
      ? {}
      : { cancel: (): unknown => Reflect.apply(cancelMethod, raw, []) }),
    valid: cancelMethod !== undefined && response.valid,
  });
}

function captureProperty(
  target: object,
  property: PropertyKey,
): Readonly<{ valid: true; value: unknown }> | Readonly<{ valid: false }> {
  try {
    return Object.freeze({ valid: true, value: Reflect.get(target, property) });
  } catch {
    return Object.freeze({ valid: false });
  }
}

function captureMethod(target: object, property: PropertyKey): Function | undefined {
  const captured = captureProperty(target, property);
  return captured.valid && typeof captured.value === "function"
    ? captured.value
    : undefined;
}

function unavailableTransportPromise<T>(): CapturedTransportPromise<T> {
  const settlement = Object.freeze({ kind: "unavailable" as const });
  return Object.freeze({
    promise: Promise.resolve(settlement),
    valid: false,
    subscribe(
      observer: (value: TransportPromiseSettlement<T>) => void,
    ): void {
      const observed = Promise.resolve().then(() => observer(settlement));
      void observed.then(undefined, () => undefined);
    },
  });
}

function captureTransportPromise<T>(raw: unknown): CapturedTransportPromise<T> {
  if (
    (typeof raw !== "object" && typeof raw !== "function") ||
    raw === null
  ) {
    return unavailableTransportPromise<T>();
  }
  let valid = true;
  let settlement: TransportPromiseSettlement<T> | undefined;
  const observers = new Set<
    (value: TransportPromiseSettlement<T>) => void
  >();
  const promise = new Promise<TransportPromiseSettlement<T>>((resolve) => {
    let settled = false;
    const publish = (value: TransportPromiseSettlement<T>): void => {
      if (settled) return;
      settled = true;
      settlement = value;
      resolve(value);
      for (const observer of observers) {
        try {
          observer(value);
        } catch {
          // Observers are core-owned terminal boundaries.
        }
      }
      observers.clear();
    };
    const fulfill = (value: unknown): void => {
      publish(Object.freeze({ kind: "fulfilled", value: value as T }));
    };
    const reject = (reason: unknown): void => {
      publish(Object.freeze({ kind: "rejected", reason }));
    };
    try {
      Reflect.apply(nativePromiseThen, raw, [fulfill, reject]);
      return;
    } catch {
      // Non-native thenables are admitted through one explicit member snapshot.
    }
    const capturedThen = captureProperty(raw, "then");
    if (!capturedThen.valid || typeof capturedThen.value !== "function") {
      valid = false;
      publish(Object.freeze({ kind: "unavailable" }));
      return;
    }
    try {
      Reflect.apply(capturedThen.value, raw, [fulfill, reject]);
    } catch (error) {
      reject(error);
    }
  });
  return Object.freeze({
    promise,
    valid,
    subscribe(observer: (value: TransportPromiseSettlement<T>) => void): void {
      if (settlement === undefined) {
        observers.add(observer);
        return;
      }
      const observed = Promise.resolve().then(() => observer(settlement!));
      void observed.then(undefined, () => undefined);
    },
  });
}

function transportPromiseValue<T>(
  settlement: TransportPromiseSettlement<T>,
): T {
  if (settlement.kind === "fulfilled") return settlement.value;
  if (settlement.kind === "rejected") throw settlement.reason;
  throw new TypeError("Unavailable transport promise");
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

type WorkbenchEnvelope =
  | OpenDocumentResponseEnvelope
  | ListModulesResponseEnvelope
  | ReadModulesResponseEnvelope;

type WorkbenchTerminal<T extends WorkbenchEnvelope> =
  | Readonly<{ kind: "valid"; outcome: WorkbenchOutcome<T> }>
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
  let factory: AdmittedTransportFactory;
  const runtime =
    (object.runtime as InternalClientRuntime | undefined) ?? defaultRuntime();
  let options: NormalizedCreationOptions;
  try {
    factory = admitTransportFactory(object.transportFactory);
    options = normalizeCreationOptions(
      object.options as EngineClientCreationOptions,
    );
  } catch (error) {
    throw inputFault(error);
  }
  const client = new EngineClientImplementation(
    factory,
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
  private readonly factory: AdmittedTransportFactory;
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
    TransportShutdownRecord
  >();
  private _state: EngineClientState = "failed";
  readonly workbench = Object.freeze({
    openDocument: (
      input: OpenDocumentInput,
      options?: CompileOptions,
    ): Promise<WorkbenchOutcome<OpenDocumentResponseEnvelope>> =>
      this.openDocument(input, options),
    listModules: (
      input: ListModulesInput,
      options?: CompileOptions,
    ): Promise<WorkbenchOutcome<ListModulesResponseEnvelope>> =>
      this.listModules(input, options),
    readModules: (
      input: ReadModulesInput,
      options?: CompileOptions,
    ): Promise<WorkbenchOutcome<ReadModulesResponseEnvelope>> =>
      this.readModules(input, options),
  });

  constructor(
    factory: AdmittedTransportFactory,
    collectors: InternalProtocolBlobRefCollectors,
    options: NormalizedCreationOptions,
    runtime: InternalClientRuntime,
  ) {
    this.factory = factory;
    this.collectors = collectors;
    this.options = options;
    this.runtime = runtime;
    this.nonce = bytesToHex(runtime.randomBytes(12));
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

  private openDocument(
    input: OpenDocumentInput,
    options?: CompileOptions,
  ): Promise<WorkbenchOutcome<OpenDocumentResponseEnvelope>> {
    if (!isOpenDocumentInput(input)) throw new EngineClientInputError("INVALID_ARGUMENT");
    return this.executeWorkbenchOperation(
      "engine.open_document",
      input,
      options,
      (envelope) => encodeOpenDocumentRequestEnvelope(envelope as OpenDocumentRequestEnvelope),
      decodeOpenDocumentResponseEnvelope,
    );
  }

  private listModules(
    input: ListModulesInput,
    options?: CompileOptions,
  ): Promise<WorkbenchOutcome<ListModulesResponseEnvelope>> {
    if (!isListModulesInput(input)) throw new EngineClientInputError("INVALID_ARGUMENT");
    return this.executeWorkbenchOperation(
      "engine.list_modules",
      input,
      options,
      (envelope) => encodeListModulesRequestEnvelope(envelope as ListModulesRequestEnvelope),
      decodeListModulesResponseEnvelope,
    );
  }

  private readModules(
    input: ReadModulesInput,
    options?: CompileOptions,
  ): Promise<WorkbenchOutcome<ReadModulesResponseEnvelope>> {
    if (!isReadModulesInput(input)) throw new EngineClientInputError("INVALID_ARGUMENT");
    return this.executeWorkbenchOperation(
      "engine.read_modules",
      input,
      options,
      (envelope) => encodeReadModulesRequestEnvelope(envelope as ReadModulesRequestEnvelope),
      decodeReadModulesResponseEnvelope,
    );
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
    const promise = Promise.resolve().then(async (): Promise<void> => {
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
    });
    this.disposalPromise = promise;
    if (active !== undefined) {
      active.interrupt.resolve({
        kind: "cancel",
        reason: "dispose",
        barrier: barrier.promise,
      });
    }
    this.pendingOpenAbort?.resolve();
    const pending = this.pendingTransport;
    if (pending !== undefined) void this.shutdownTransport(pending, "hard");
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
    let exchange: AdmittedExchange | undefined;
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
        if (endpoint.request === undefined) throw new TypeError("Invalid request");
        exchange = admitExchange(endpoint.request({
          exchangeId: this.nextExchangeId(),
          control,
          blobs: transportBlobs,
        }));
        if (!exchange.valid) throw new TypeError("Invalid exchange");
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

  private executeWorkbenchOperation<TResponse extends WorkbenchEnvelope>(
    operation: OpenDocumentRequestEnvelope["operation"] | ListModulesRequestEnvelope["operation"] | ReadModulesRequestEnvelope["operation"],
    payload: unknown,
    options: CompileOptions | undefined,
    encodeEnvelope: (envelope: {
      deadline_at: string;
      operation: string;
      payload: unknown;
      protocol: typeof ENGINE_PROTOCOL;
      request_id: string;
    }) => string,
    decodeEnvelope: (text: string) => TResponse,
  ): Promise<WorkbenchOutcome<TResponse>> {
    const endpoint = this.readyEndpoint();
    if (!endpointSupportsOperation(endpoint, operation)) {
      throw new EngineClientStateError("NOT_READY", { capability: operation });
    }
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
    const active: ActiveCompile = {
      requestId,
      generation: endpoint.generation,
      interrupt: deferred<CompileInterrupt>(),
      joined: deferred<void>(),
    };
    this.active = active;
    const promise = this.executeWorkbench(
      active,
      endpoint,
      operation,
      payload,
      normalized,
      encodeEnvelope,
      decodeEnvelope,
    );
    void promise.finally(() => {
      if (this.active === active) this.active = undefined;
    }).catch(() => undefined);
    return promise;
  }

  private async executeWorkbench<TResponse extends WorkbenchEnvelope>(
    active: ActiveCompile,
    endpoint: EndpointContext,
    operation: string,
    payload: unknown,
    options: NormalizedCompileOptions,
    encodeEnvelope: (envelope: {
      deadline_at: string;
      operation: string;
      payload: unknown;
      protocol: typeof ENGINE_PROTOCOL;
      request_id: string;
    }) => string,
    decodeEnvelope: (text: string) => TResponse,
  ): Promise<WorkbenchOutcome<TResponse>> {
    let timer: InternalTimerHandle | undefined;
    let removeAbort: (() => void) | undefined;
    let exchange: AdmittedExchange | undefined;
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

      const beforeSend = await Promise.race([
        Promise.resolve({ kind: "ready" as const }),
        active.interrupt.promise,
      ]);
      if (beforeSend.kind === "cancel") {
        active.joined.resolve();
        if (beforeSend.barrier !== undefined) await beforeSend.barrier;
        return clientCancellation(active, beforeSend.reason);
      }
      if (beforeSend.kind === "fault") {
        active.joined.resolve();
        throw await this.replaceForFault(beforeSend.fault, active);
      }

      const controlText = encodeEnvelope({
        deadline_at: new Date(deadlineMs).toISOString(),
        operation,
        payload,
        protocol: ENGINE_PROTOCOL,
        request_id: active.requestId,
      });
      validateOutgoingControl(controlText, endpoint.limits);
      const control = encodeControl(controlText);
      try {
        if (endpoint.request === undefined) throw new TypeError("Invalid request");
        exchange = admitExchange(endpoint.request({
          exchangeId: this.nextExchangeId(),
          control,
          blobs: [],
        }));
        if (!exchange.valid) throw new TypeError("Invalid exchange");
      } catch {
        throw await this.replaceForFault(
          new EngineClientTransportError("TRANSFER_FAILED"),
          active,
        );
      }

      const terminal = this.observeWorkbenchTerminal(
        exchange,
        endpoint,
        active.requestId,
        digests,
        decodeEnvelope,
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
    exchange: AdmittedExchange,
    endpoint: EndpointContext,
    requestId: string,
    digests: DigestSequence,
  ): Promise<CompileTerminal> {
    return exchange.response.then(
      async (settlement): Promise<CompileTerminal> => {
        if (settlement.kind !== "fulfilled") {
          return {
            kind: "rejected",
            error: settlement.kind === "rejected"
              ? settlement.reason
              : new EngineClientTransportError("TRANSFER_FAILED"),
          };
        }
        try {
          return {
            kind: "valid",
            outcome: await this.decodeCompileOutcome(
              settlement.value,
              endpoint,
              requestId,
              digests,
            ),
          };
        } catch (error) {
          return { kind: "invalid", error };
        }
      },
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

  private observeWorkbenchTerminal<TResponse extends WorkbenchEnvelope>(
    exchange: AdmittedExchange,
    endpoint: EndpointContext,
    requestId: string,
    digests: DigestSequence,
    decodeEnvelope: (text: string) => TResponse,
  ): Promise<WorkbenchTerminal<TResponse>> {
    return exchange.response.then(
      async (settlement): Promise<WorkbenchTerminal<TResponse>> => {
        if (settlement.kind !== "fulfilled") {
          return {
            kind: "rejected",
            error: settlement.kind === "rejected"
              ? settlement.reason
              : new EngineClientTransportError("TRANSFER_FAILED"),
          };
        }
        try {
          return {
            kind: "valid",
            outcome: await this.decodeWorkbenchOutcome(
              settlement.value,
              endpoint,
              requestId,
              digests,
              decodeEnvelope,
            ),
          };
        } catch (error) {
          return { kind: "invalid", error };
        }
      },
    );
  }

  private async decodeWorkbenchOutcome<TResponse extends WorkbenchEnvelope>(
    raw: InternalTransportResponse,
    endpoint: EndpointContext,
    requestId: string,
    digests: DigestSequence,
    decodeEnvelope: (text: string) => TResponse,
  ): Promise<WorkbenchOutcome<TResponse>> {
    const response = validateTransportResponse(raw, endpoint.limits);
    let envelope: TResponse;
    try {
      envelope = decodeEnvelope(
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
      });
    }
    const expected = uniqueRefs(collectBlobRefsDeep(envelope.payload), "output");
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
      response: envelope,
      blobs: outputs,
    });
  }

  private async cancelExchange(
    exchange: AdmittedExchange,
  ): Promise<InternalCancelResult> {
    try {
      if (exchange.cancel === undefined) return { reusable: false };
      const returned = exchange.cancel();
      const captured = captureTransportPromise<InternalCancelResult>(returned);
      if (!captured.valid) return { reusable: false };
      const settlement = await this.withBound(
        captured.promise,
        this.options.cancelGraceMs,
        undefined,
      );
      if (settlement === undefined || settlement.kind !== "fulfilled") {
        return { reusable: false };
      }
      const raw = settlement.value;
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
    if (this.isTerminating()) {
      barrier?.resolve();
      return Promise.resolve();
    }
    this._state = "replacing";
    const promise = Promise.resolve().then(async (): Promise<void> => {
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
    });
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
    this.pendingTransport = admitted;
    let exchange: AdmittedExchange | undefined;
    try {
      if (!admitted.operationsValid) {
        throw new EngineClientTransportError(this.factory.connectFailureCode);
      }
      const limits = validateTransportLimits(
        transportPromiseValue(
          await this.openStage(admitted.ready, deadline, abort),
        ),
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
        if (admitted.request === undefined) throw new TypeError("Invalid request");
        exchange = admitExchange(admitted.request({
          exchangeId: this.nextExchangeId(),
          control,
          blobs: [],
        }));
        if (!exchange.valid) throw new TypeError("Invalid exchange");
      } catch {
        throw new EngineClientTransportError("TRANSFER_FAILED");
      }
      const response = validateTransportResponse(
        transportPromiseValue(
          await this.openStage(exchange.response, deadline, abort),
        ),
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
        ...admitted,
        generation,
        limits,
        snapshot: deepFreeze({ generation, handshake: validated.result }),
        capabilityStatuses: validated.statuses,
        engineRelease: validated.result.host_release,
      };
    } catch (error) {
      if (safeInstanceOf(error, CreationTimedOut)) {
        if (exchange !== undefined) await this.cancelExchange(exchange);
        await this.shutdownTransport(admitted, "hard");
        throw new EngineClientTransportError("TIMEOUT_DURING_CREATION");
      }
      await this.shutdownTransport(admitted, "hard");
      if (safeInstanceOf(error, OpenAborted)) throw error;
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
    const observe = (
      settlement: TransportPromiseSettlement<InternalTransportClose>,
    ): void => {
      try {
        const close = settlement.kind === "fulfilled"
          ? validateTransportClose(settlement.value)
          : undefined;
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
    endpoint.observeClosed(observe);
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
    mode: TransportShutdownMode = "graceful",
  ): Promise<boolean> {
    const existing = this.transportShutdowns.get(admitted.transport);
    if (existing !== undefined) {
      if (mode === "hard") this.requestTransportHardStop(existing);
      return existing.completion.promise;
    }
    const record: TransportShutdownRecord = {
      admitted,
      completion: deferred<boolean>(),
      hardRequested: mode === "hard",
      hardStopAttempted: false,
      hardStopInvoked: false,
    };
    this.transportShutdowns.set(admitted.transport, record);
    if (mode === "hard") this.requestTransportHardStop(record);
    void this.performTransportShutdown(record).then(
      (clean) => record.completion.resolve(clean),
      () => record.completion.resolve(false),
    );
    return record.completion.promise;
  }

  private async performTransportShutdown(
    record: TransportShutdownRecord,
  ): Promise<boolean> {
    let gracefulResult = false;
    if (!record.hardRequested) {
      gracefulResult = await this.invokeTransportDispose(record.admitted);
    } else if (!record.hardStopInvoked) {
      await this.invokeTransportDispose(record.admitted);
    }

    let joined = await this.joinTransport(record.admitted);
    if (!joined && !record.hardStopAttempted) {
      this.requestTransportHardStop(record);
      joined = await this.joinTransport(record.admitted);
    }
    return gracefulResult && joined && !record.hardStopAttempted;
  }

  private async invokeTransportDispose(
    admitted: AdmittedTransport,
  ): Promise<boolean> {
    if (admitted.dispose === undefined) return false;
    let returned: unknown;
    try {
      returned = admitted.dispose();
    } catch {
      return false;
    }
    const captured = captureTransportPromise<void>(returned);
    if (!captured.valid) return false;
    const settlement = await this.withBound(
      captured.promise,
      this.options.disposeTimeoutMs,
      undefined,
    );
    return settlement?.kind === "fulfilled";
  }

  private requestTransportHardStop(record: TransportShutdownRecord): void {
    record.hardRequested = true;
    if (record.hardStopAttempted) return;
    record.hardStopAttempted = true;
    const terminate = record.admitted.terminate;
    if (terminate === undefined) return;
    let returned: unknown;
    try {
      returned = terminate();
      record.hardStopInvoked = true;
    } catch {
      return;
    }
    if (
      (typeof returned === "object" && returned !== null) ||
      typeof returned === "function"
    ) {
      const observed = captureTransportPromise<unknown>(returned).promise;
      void observed.then(undefined, () => undefined);
    }
  }

  private async joinTransport(admitted: AdmittedTransport): Promise<boolean> {
    const settlement = await this.withBound(
      admitted.closed,
      this.options.disposeTimeoutMs,
      undefined,
    );
    return settlement?.kind === "fulfilled";
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

function endpointSupportsOperation(endpoint: EndpointContext, operation: string): boolean {
  const capability = endpoint.snapshot.handshake.capability_manifest.operations[operation];
  return capability?.enabled === true;
}

function clientCancellation(
  active: ActiveCompile,
  reason: ClientCancellationReason,
): Readonly<{
  origin: "client";
  outcome: "cancelled";
  requestId: string;
  endpointGeneration: number;
  reason: ClientCancellationReason;
  blobs: readonly [];
}> {
  return deepFreeze({
    origin: "client",
    outcome: "cancelled",
    requestId: active.requestId,
    endpointGeneration: active.generation,
    reason,
    blobs: [] as const,
  });
}

function collectBlobRefsDeep(value: unknown): readonly BlobRef[] {
  const refs: BlobRef[] = [];
  const visit = (candidate: unknown): void => {
    if (isBlobRef(candidate)) {
      refs.push(candidate);
      return;
    }
    if (Array.isArray(candidate)) {
      for (const item of candidate) visit(item);
      return;
    }
    if (
      typeof candidate === "object" &&
      candidate !== null &&
      Object.getPrototypeOf(candidate) === Object.prototype
    ) {
      for (const item of Object.values(candidate)) visit(item);
    }
  };
  visit(value);
  return refs;
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
  if (safeInstanceOf(error, EngineClientInputError)) {
    try {
      const code = ownDataValue(error, "code");
      const details = ownDataValue(error, "details");
      if (isInputErrorCode(code)) {
        return new EngineClientInputError(
          code,
          details as EngineClientSafeDetails | undefined,
        );
      }
    } catch {
      // Hostile branded values are treated as unclassified input failures.
    }
  }
  return new EngineClientInputError("INVALID_ARGUMENT");
}

function publicFault(
  value: unknown,
  fallback: "SPAWN_FAILED" | "CONNECT_FAILED" | "BROKEN_PIPE" = "BROKEN_PIPE",
): EngineClientTransportError | EngineClientDecodeError {
  try {
    if (safeInstanceOf(value, EngineClientDecodeError)) {
      const code = ownDataValue(value, "code");
      const details = ownDataValue(value, "details");
      if (isDecodeErrorCode(code)) {
        return new EngineClientDecodeError(
          code,
          details as EngineClientSafeDetails | undefined,
        );
      }
    }
    if (safeInstanceOf(value, EngineClientTransportError)) {
      const code = ownDataValue(value, "code");
      const retryable = ownDataValue(value, "retryable");
      const details = ownDataValue(value, "details");
      if (isTransportErrorCode(code) && typeof retryable === "boolean") {
        return new EngineClientTransportError(
          code,
          retryable,
          details as EngineClientSafeDetails | undefined,
        );
      }
    }
    if (safeInstanceOf(value, InternalTransportFault)) {
      const kind = ownDataValue(value, "kind");
      const code = ownDataValue(value, "code");
      const retryable = ownDataValue(value, "retryable");
      const details = ownDataValue(value, "details");
      if (kind === "decode" && isDecodeErrorCode(code)) {
        return new EngineClientDecodeError(
          code,
          details as EngineClientSafeDetails | undefined,
        );
      }
      if (
        kind === "transport" &&
        isTransportErrorCode(code) &&
        typeof retryable === "boolean"
      ) {
        return new EngineClientTransportError(
          code,
          retryable,
          details as EngineClientSafeDetails | undefined,
        );
      }
    }
  } catch {
    // Raw adapter values never escape stable public classification.
  }
  return new EngineClientTransportError(fallback);
}

function safeInstanceOf(
  value: unknown,
  constructor: abstract new (...args: never[]) => unknown,
): boolean {
  try {
    return value instanceof constructor;
  } catch {
    return false;
  }
}

function ownDataValue(value: unknown, property: PropertyKey): unknown {
  if (
    (typeof value !== "object" && typeof value !== "function") ||
    value === null
  ) {
    return undefined;
  }
  const descriptor = Object.getOwnPropertyDescriptor(value, property);
  return descriptor !== undefined && "value" in descriptor
    ? descriptor.value
    : undefined;
}

const INPUT_ERROR_CODES = new Set<string>([
  "INVALID_ARGUMENT",
  "INVALID_REQUEST_ID",
  "INVALID_BLOB_TABLE",
  "UNSUPPORTED_BYTE_OWNERSHIP",
  "BLOB_SIZE_MISMATCH",
  "BLOB_DIGEST_MISMATCH",
  "LIMIT_EXCEEDED",
]);

const TRANSPORT_ERROR_CODES = new Set<string>([
  "NEGOTIATION_REJECTED",
  "SPAWN_FAILED",
  "CONNECT_FAILED",
  "BROKEN_PIPE",
  "PROCESS_EXITED",
  "WORKER_CRASHED",
  "TRANSFER_FAILED",
  "DIGEST_FAILED",
  "TIMEOUT_DURING_CREATION",
  "REPLACEMENT_FAILED",
]);

const DECODE_ERROR_CODES = new Set<string>([
  "MALFORMED_FRAME",
  "MALFORMED_MESSAGE",
  "CORRELATION_MISMATCH",
  "PROTOCOL_MISMATCH",
  "UNEXPECTED_BLOB",
  "MISSING_BLOB",
  "OUTPUT_SIZE_MISMATCH",
  "OUTPUT_DIGEST_MISMATCH",
]);

function isInputErrorCode(value: unknown): value is ConstructorParameters<
  typeof EngineClientInputError
>[0] {
  return typeof value === "string" && INPUT_ERROR_CODES.has(value);
}

function isTransportErrorCode(value: unknown): value is ConstructorParameters<
  typeof EngineClientTransportError
>[0] {
  return typeof value === "string" && TRANSPORT_ERROR_CODES.has(value);
}

function isDecodeErrorCode(value: unknown): value is ConstructorParameters<
  typeof EngineClientDecodeError
>[0] {
  return typeof value === "string" && DECODE_ERROR_CODES.has(value);
}
