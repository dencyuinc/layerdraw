// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {
  failure,
  validateInitMessage,
  validateRequestPayload,
  validateRequestMessage,
  validateWorkerMessage,
  workerProtocol,
  workerProtocolVersion,
  type EngineWorkerBlob,
  type EngineWorkerFailure,
  type EngineWorkerFailureCode,
  type EngineWorkerRequestPayload,
  type EngineWorkerTransportLimits,
  type ResponseMessage,
  type TransportFailureMessage,
  type WorkerToHostMessage,
} from "./protocol.js";

export interface WorkerEventLike {
  readonly data: unknown;
}

export interface WorkerLike {
  postMessage(message: unknown, transfer: readonly ArrayBuffer[]): void;
  addEventListener(type: "message", listener: (event: WorkerEventLike) => void): void;
  addEventListener(type: "messageerror" | "error", listener: () => void): void;
  removeEventListener(type: "message", listener: (event: WorkerEventLike) => void): void;
  removeEventListener(type: "messageerror" | "error", listener: () => void): void;
  terminate(): unknown;
}

export type EngineWorkerFactory = (
  moduleURL: string,
  options: Readonly<{type: "module"; name: string}>,
) => WorkerLike;

export interface CreateEngineWorkerTransportOptions {
  readonly endpointGeneration: string;
  readonly expectedArtifactManifestDigest: string;
  readonly releaseManifestDigest: string;
  readonly workerName?: string;
  readonly disposeTimeoutMilliseconds?: number;
}

export type EngineWorkerRequest = EngineWorkerRequestPayload;

export interface EngineWorkerResponse {
  readonly control: ArrayBuffer;
  readonly blobs: readonly EngineWorkerBlob[];
}

export interface EngineWorkerExchange {
  readonly exchangeID: string;
  readonly accepted: Promise<void>;
  readonly response: Promise<EngineWorkerResponse>;
}

export interface EngineWorkerTransport {
  readonly endpointGeneration: string;
  readonly ready: Promise<EngineWorkerTransportLimits>;
  /** Settles after the Worker and every in-flight exchange are terminal. */
  readonly closed: Promise<void>;
  request(input: EngineWorkerRequest): EngineWorkerExchange;
  terminate(): void;
  dispose(): Promise<void>;
}

interface Deferred<T> {
  readonly promise: Promise<T>;
  resolve(value: T): void;
  reject(reason: unknown): void;
}

interface PendingExchange {
  readonly exchangeID: string;
  readonly accepted: Deferred<void>;
  readonly response: Deferred<EngineWorkerResponse>;
  acceptedReceived: boolean;
}

type HostState = "initializing" | "idle" | "executing" | "disposing" | "disposed" | "terminated" | "crashed";

function deferred<T>(): Deferred<T> {
  let resolvePromise!: (value: T) => void;
  let rejectPromise!: (reason: unknown) => void;
  const promise = new Promise<T>((resolve, reject) => {
    resolvePromise = resolve;
    rejectPromise = reject;
  });
  return {promise, resolve: resolvePromise, reject: rejectPromise};
}

export class EngineWorkerTransportError extends Error {
  readonly failure: EngineWorkerFailure;

  constructor(value: EngineWorkerFailure) {
    super(value.code);
    this.name = "EngineWorkerTransportError";
    this.failure = value;
  }
}

function transportError(code: EngineWorkerFailureCode): EngineWorkerTransportError {
  return new EngineWorkerTransportError(failure(code));
}

function defaultWorkerFactory(moduleURL: string, options: Readonly<{type: "module"; name: string}>): WorkerLike {
  const constructor = Reflect.get(globalThis, "Worker");
  if (typeof constructor !== "function") throw transportError("engine.worker.unsupported");
  return Reflect.construct(constructor, [moduleURL, options]) as WorkerLike;
}

function responseTransferList(message: ResponseMessage): EngineWorkerResponse {
  return Object.freeze({
    control: message.control,
    blobs: Object.freeze([...message.blobs]),
  });
}

function schedule(callback: () => void, milliseconds: number): unknown {
  const setTimeoutValue = Reflect.get(globalThis, "setTimeout");
  if (typeof setTimeoutValue !== "function") {
    callback();
    return undefined;
  }
  return Reflect.apply(setTimeoutValue, globalThis, [callback, milliseconds]);
}

function cancelSchedule(handle: unknown): void {
  if (handle === undefined) return;
  const clearTimeoutValue = Reflect.get(globalThis, "clearTimeout");
  if (typeof clearTimeoutValue === "function") Reflect.apply(clearTimeoutValue, globalThis, [handle]);
}

export function createEngineWorkerTransportWithFactory(
  options: CreateEngineWorkerTransportOptions,
  workerModuleURL: string,
  workerFactory: EngineWorkerFactory,
): EngineWorkerTransport {
  const disposeTimeout = options.disposeTimeoutMilliseconds ?? 1_000;
  if (!Number.isSafeInteger(disposeTimeout) || disposeTimeout < 0 || disposeTimeout > 60_000) {
    throw transportError("engine.worker.malformed_message");
  }
  const initMessage = {
    worker_protocol: workerProtocol,
    worker_protocol_version: workerProtocolVersion,
    kind: "init",
    endpoint_generation: options.endpointGeneration,
    expected_artifact_manifest_digest: options.expectedArtifactManifestDigest,
    release_manifest_digest: options.releaseManifestDigest,
  };
  if (validateInitMessage(initMessage) === undefined || typeof workerModuleURL !== "string" || workerModuleURL.length === 0) {
    throw transportError("engine.worker.malformed_message");
  }

  const ready = deferred<EngineWorkerTransportLimits>();
  const disposed = deferred<void>();
  const closed = deferred<void>();
  let worker: WorkerLike;
  try {
    worker = workerFactory(
      workerModuleURL,
      Object.freeze({type: "module", name: options.workerName ?? "layerdraw-engine"}),
    );
  } catch (error) {
    if (error instanceof EngineWorkerTransportError) throw error;
    throw transportError("engine.worker.unsupported");
  }

  let state: HostState = "initializing";
  let limits: EngineWorkerTransportLimits | undefined;
  let pending: PendingExchange | undefined;
  let disposeTimer: unknown;
  let listenersRemoved = false;

  const removeListeners = (): void => {
    if (listenersRemoved) return;
    listenersRemoved = true;
    worker.removeEventListener("message", onMessage);
    worker.removeEventListener("messageerror", onFatalEvent);
    worker.removeEventListener("error", onFatalEvent);
  };

  const rejectPending = (error: EngineWorkerTransportError): void => {
    const current = pending;
    pending = undefined;
    if (current === undefined) return;
    if (!current.acceptedReceived) current.accepted.reject(error);
    current.response.reject(error);
  };

  const finishDisposed = (): void => {
    if (state === "disposed") return;
    state = "disposed";
    cancelSchedule(disposeTimer);
    disposeTimer = undefined;
    removeListeners();
    worker.terminate();
    disposed.resolve(undefined);
    closed.resolve(undefined);
  };

  const failTerminal = (code: EngineWorkerFailureCode, nextState: "terminated" | "crashed" = "crashed"): void => {
    if (state === "disposed" || state === "terminated" || state === "crashed") return;
    const error = transportError(code);
    state = nextState;
    if (limits === undefined) ready.reject(error);
    rejectPending(error);
    cancelSchedule(disposeTimer);
    removeListeners();
    worker.terminate();
    closed.resolve(undefined);
    if (nextState !== "terminated") disposed.resolve(undefined);
  };

  const handleFailure = (message: TransportFailureMessage): void => {
    if (message.endpoint_generation !== undefined && message.endpoint_generation !== options.endpointGeneration) return;
    const error = new EngineWorkerTransportError(message.failure);
    if (message.failure.code === "engine.worker.disposed" && state === "disposing") {
      finishDisposed();
      return;
    }
    if (state === "initializing") {
      failTerminal(message.failure.code);
      return;
    }
    if (message.exchange_id !== undefined && pending?.exchangeID === message.exchange_id) {
      const current = pending;
      pending = undefined;
      if (!current.acceptedReceived) current.accepted.reject(error);
      current.response.reject(error);
      if (message.failure.code === "engine.worker.malformed_message" || message.failure.code === "engine.worker.stale_generation") {
        state = "idle";
        return;
      }
    }
    if (message.failure.code === "engine.worker.malformed_message" || message.failure.code === "engine.worker.stale_generation") return;
    failTerminal(message.failure.code);
  };

  const handleMessage = (message: WorkerToHostMessage): void => {
    if (message.kind !== "transport_failure" && message.endpoint_generation !== options.endpointGeneration) return;
    if (state === "disposing" && message.kind !== "transport_failure") return;
    if (message.kind === "transport_failure") {
      handleFailure(message);
      return;
    }
    if (message.kind === "ready") {
      if (state !== "initializing" || message.artifact_manifest_digest !== options.expectedArtifactManifestDigest) {
        failTerminal("engine.worker.artifact_mismatch");
        return;
      }
      limits = message.transport_limits;
      state = "idle";
      ready.resolve(limits);
      return;
    }
    const current = pending;
    if (current === undefined || message.exchange_id !== current.exchangeID || state !== "executing") {
      failTerminal("engine.worker.crashed");
      return;
    }
    if (message.kind === "accepted") {
      if (current.acceptedReceived) {
        failTerminal("engine.worker.crashed");
        return;
      }
      current.acceptedReceived = true;
      current.accepted.resolve(undefined);
      return;
    }
    if (!current.acceptedReceived) {
      failTerminal("engine.worker.crashed");
      return;
    }
    pending = undefined;
    state = "idle";
    current.response.resolve(responseTransferList(message));
  };

  function onMessage(event: WorkerEventLike): void {
    if (state === "disposed" || state === "terminated" || state === "crashed") return;
    const message = validateWorkerMessage(event.data, limits);
    if (message === undefined) {
      failTerminal("engine.worker.crashed");
      return;
    }
    handleMessage(message);
  }

  function onFatalEvent(): void {
    failTerminal("engine.worker.crashed");
  }

  worker.addEventListener("message", onMessage);
  worker.addEventListener("messageerror", onFatalEvent);
  worker.addEventListener("error", onFatalEvent);

  try {
    worker.postMessage(initMessage, []);
  } catch {
    failTerminal("engine.worker.transfer_failed");
  }

  const transport: EngineWorkerTransport = {
    endpointGeneration: options.endpointGeneration,
    ready: ready.promise,
    closed: closed.promise,
    request(input: EngineWorkerRequest): EngineWorkerExchange {
      if (state === "disposed" || state === "disposing") throw transportError("engine.worker.disposed");
      if (state === "terminated") throw transportError("engine.worker.terminated_by_caller");
      if (state === "crashed") throw transportError("engine.worker.crashed");
      if (state === "initializing" || limits === undefined) throw transportError("engine.worker.initialization_failed");
      if (state === "executing") throw transportError("engine.worker.malformed_message");
      const validatedInput = validateRequestPayload(input, limits);
      if (validatedInput === undefined) throw transportError("engine.worker.malformed_message");
      const message = {
        worker_protocol: workerProtocol,
        worker_protocol_version: workerProtocolVersion,
        kind: "request",
        endpoint_generation: options.endpointGeneration,
        exchange_id: validatedInput.exchangeID,
        control: validatedInput.control,
        blobs: validatedInput.blobs,
      };
      if (validateRequestMessage(message, limits) === undefined) throw transportError("engine.worker.malformed_message");
      const accepted = deferred<void>();
      const response = deferred<EngineWorkerResponse>();
      pending = {exchangeID: validatedInput.exchangeID, accepted, response, acceptedReceived: false};
      state = "executing";
      try {
        worker.postMessage(message, [validatedInput.control, ...validatedInput.blobs.map((blob) => blob.bytes)]);
      } catch {
        pending = undefined;
        state = "idle";
        const error = transportError("engine.worker.transfer_failed");
        accepted.reject(error);
        response.reject(error);
      }
      return Object.freeze({exchangeID: validatedInput.exchangeID, accepted: accepted.promise, response: response.promise});
    },
    terminate(): void {
      failTerminal("engine.worker.terminated_by_caller", "terminated");
    },
    dispose(): Promise<void> {
      if (state === "disposed") return disposed.promise;
      if (state === "disposing") return disposed.promise;
      if (state === "terminated" || state === "crashed") {
        finishDisposed();
        return disposed.promise;
      }
      state = "disposing";
      const error = transportError("engine.worker.disposed");
      if (limits === undefined) ready.reject(error);
      rejectPending(error);
      try {
        worker.postMessage({
          worker_protocol: workerProtocol,
          worker_protocol_version: workerProtocolVersion,
          kind: "dispose",
          endpoint_generation: options.endpointGeneration,
        }, []);
        disposeTimer = schedule(finishDisposed, disposeTimeout);
      } catch {
        finishDisposed();
      }
      return disposed.promise;
    },
  };

  return Object.freeze(transport);
}

export function createEngineWorkerTransport(options: CreateEngineWorkerTransportOptions): EngineWorkerTransport {
  return createEngineWorkerTransportWithFactory(
    options,
    new URL("./worker.js", import.meta.url).href,
    defaultWorkerFactory,
  );
}
