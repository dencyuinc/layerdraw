// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {
  failure,
  getSafeKind,
  getSafeRouting,
  hasInitialTransportLimits,
  isDigest,
  validateDisposeMessage,
  validateEndpointResponse,
  validateFailure,
  validateInitMessage,
  validateRequestMessage,
  workerProtocol,
  workerProtocolVersion,
  type EngineByteEndpointResponse,
  type EngineWorkerBlob,
  type EngineWorkerFailure,
  type EngineWorkerFailureCode,
  type EngineWorkerTransportLimits,
  type InitMessage,
  type TransportFailureMessage,
} from "./protocol.js";

export interface EngineByteEndpointRequest {
  readonly control: ArrayBuffer;
  readonly blobs: readonly EngineWorkerBlob[];
}

export type EngineByteEndpointResult =
  | Readonly<{ok: true; response: EngineByteEndpointResponse}>
  | Readonly<{ok: false; failure: EngineWorkerFailure}>;

export interface EngineByteEndpoint {
  readonly artifactManifestDigest: string;
  readonly transportLimits: EngineWorkerTransportLimits;
  request(input: EngineByteEndpointRequest): EngineByteEndpointResult;
  dispose(): void;
}

export interface EngineByteEndpointInit {
  readonly endpointGeneration: string;
  readonly expectedArtifactManifestDigest: string;
  readonly releaseManifestDigest: string;
}

export type EngineByteEndpointFactory = (init: EngineByteEndpointInit) => EngineByteEndpoint | Promise<EngineByteEndpoint>;

export class EngineEndpointInitializationError extends Error {
  readonly failure: EngineWorkerFailure;

  constructor(code: "engine.worker.unsupported" | "engine.worker.initialization_failed" | "engine.worker.artifact_mismatch") {
    super(code);
    this.name = "EngineEndpointInitializationError";
    this.failure = failure(code);
  }
}

export interface WorkerMessageEventLike {
  readonly data: unknown;
}

export interface DedicatedWorkerScopeLike {
  postMessage(message: unknown, transfer: readonly ArrayBuffer[]): void;
  addEventListener(type: "message", listener: (event: WorkerMessageEventLike) => void): void;
  addEventListener(type: "messageerror", listener: () => void): void;
  removeEventListener(type: "message", listener: (event: WorkerMessageEventLike) => void): void;
  removeEventListener(type: "messageerror", listener: () => void): void;
  close(): void;
}

export interface EngineWorkerInstallOptions {
  readonly checkEnvironment?: () => boolean;
}

type RuntimeState = "created" | "initializing" | "idle" | "executing" | "disposing" | "disposed" | "crashed";
type InitializationFailureCode = "engine.worker.unsupported" | "engine.worker.initialization_failed" | "engine.worker.artifact_mismatch";

function initializationFailureCode(error: unknown): InitializationFailureCode {
  if (!(error instanceof EngineEndpointInitializationError)) return "engine.worker.initialization_failed";
  const code = error.failure.code;
  return code === "engine.worker.unsupported" || code === "engine.worker.artifact_mismatch" ? code : "engine.worker.initialization_failed";
}

function defaultEnvironmentCheck(): boolean {
  const globals = globalThis as unknown as Record<string, unknown>;
  if (typeof globals.WebAssembly !== "object" || typeof globals.TextEncoder !== "function" || typeof globals.TextDecoder !== "function") return false;
  if (typeof globals.fetch !== "function" || typeof globals.structuredClone !== "function") return false;
  const cryptoValue = globals.crypto as {getRandomValues?: unknown; subtle?: {digest?: unknown}} | undefined;
  const performanceValue = globals.performance as {now?: unknown} | undefined;
  if (typeof cryptoValue?.getRandomValues !== "function" || typeof cryptoValue.subtle?.digest !== "function" || typeof performanceValue?.now !== "function") return false;
  try {
    const probe = new ArrayBuffer(1);
    const clone = globals.structuredClone as (value: ArrayBuffer, options: {transfer: ArrayBuffer[]}) => ArrayBuffer;
    const received = clone(probe, {transfer: [probe]});
    return probe.byteLength === 0 && received.byteLength === 1;
  } catch {
    return false;
  }
}

function transferList(control: ArrayBuffer, blobs: readonly EngineWorkerBlob[]): ArrayBuffer[] {
  return [control, ...blobs.map((blob) => blob.bytes)];
}

export function installEngineWorker(
  scope: DedicatedWorkerScopeLike,
  createEndpoint: EngineByteEndpointFactory,
  options: EngineWorkerInstallOptions = {},
): () => void {
  let state: RuntimeState = "created";
  let generation: string | undefined;
  let endpoint: EngineByteEndpoint | undefined;
  let limits: EngineWorkerTransportLimits | undefined;
  let removed = false;

  const removeListeners = (): void => {
    if (removed) return;
    removed = true;
    scope.removeEventListener("message", onMessage);
    scope.removeEventListener("messageerror", onMessageError);
  };

  const close = (): void => {
    removeListeners();
    scope.close();
  };

  const safeDisposeEndpoint = (): void => {
    const current = endpoint;
    endpoint = undefined;
    limits = undefined;
    if (current === undefined) return;
    try {
      current.dispose();
    } catch {
      // Terminal cleanup is best effort and never exposes runtime details.
    }
  };

  const postFailure = (code: EngineWorkerFailureCode, route: Readonly<{endpoint_generation?: string; exchange_id?: string}> = {}): boolean => {
    const message: TransportFailureMessage = {
      worker_protocol: workerProtocol,
      worker_protocol_version: workerProtocolVersion,
      kind: "transport_failure",
      ...(route.endpoint_generation === undefined ? {} : {endpoint_generation: route.endpoint_generation}),
      ...(route.exchange_id === undefined ? {} : {exchange_id: route.exchange_id}),
      failure: failure(code),
    };
    try {
      scope.postMessage(message, []);
      return true;
    } catch {
      return false;
    }
  };

  const crash = (route: Readonly<{endpoint_generation?: string; exchange_id?: string}> = {}): void => {
    if (state === "disposed" || state === "crashed") return;
    state = "crashed";
    safeDisposeEndpoint();
    postFailure("engine.worker.crashed", route);
    close();
  };

  const failInitialization = (code: InitializationFailureCode, currentGeneration: string): void => {
    state = "crashed";
    postFailure(code, {endpoint_generation: currentGeneration});
    close();
  };

  const initialize = (message: InitMessage): void => {
    if (state !== "created") {
      postFailure("engine.worker.malformed_message", {endpoint_generation: message.endpoint_generation});
      return;
    }
    generation = message.endpoint_generation;
    const checkEnvironment = options.checkEnvironment ?? defaultEnvironmentCheck;
    if (!checkEnvironment()) {
      failInitialization("engine.worker.unsupported", generation);
      return;
    }
    state = "initializing";
    let created: EngineByteEndpoint | Promise<EngineByteEndpoint>;
    try {
      created = createEndpoint(Object.freeze({
        endpointGeneration: message.endpoint_generation,
        expectedArtifactManifestDigest: message.expected_artifact_manifest_digest,
        releaseManifestDigest: message.release_manifest_digest,
      }));
    } catch (error) {
      failInitialization(initializationFailureCode(error), generation);
      return;
    }
    void Promise.resolve(created).then((value) => {
      if (state === "disposing" || state === "disposed") {
        try { value.dispose(); } catch { /* Redacted terminal cleanup. */ }
        return;
      }
      if (state !== "initializing") return;
      if (!isDigest(value.artifactManifestDigest) || !hasInitialTransportLimits(value.transportLimits)) {
        try { value.dispose(); } catch { /* Invalid endpoint cleanup. */ }
        failInitialization("engine.worker.initialization_failed", message.endpoint_generation);
        return;
      }
      if (value.artifactManifestDigest !== message.expected_artifact_manifest_digest) {
        try { value.dispose(); } catch { /* Mismatch cleanup. */ }
        failInitialization("engine.worker.artifact_mismatch", message.endpoint_generation);
        return;
      }
      endpoint = value;
      limits = value.transportLimits;
      state = "idle";
      try {
        scope.postMessage({
          worker_protocol: workerProtocol,
          worker_protocol_version: workerProtocolVersion,
          kind: "ready",
          endpoint_generation: message.endpoint_generation,
          artifact_manifest_digest: value.artifactManifestDigest,
          transport_limits: value.transportLimits,
        }, []);
      } catch {
        crash({endpoint_generation: message.endpoint_generation});
      }
    }, (error: unknown) => {
      if (state !== "initializing") return;
      failInitialization(initializationFailureCode(error), message.endpoint_generation);
    });
  };

  const dispose = (messageGeneration: string): void => {
    if (generation === undefined || messageGeneration !== generation) {
      postFailure("engine.worker.stale_generation", {endpoint_generation: messageGeneration});
      return;
    }
    if (state === "disposed" || state === "disposing") return;
    state = "disposing";
    safeDisposeEndpoint();
    state = "disposed";
    postFailure("engine.worker.disposed", {endpoint_generation: generation});
    close();
  };

  const request = (value: unknown): void => {
    if (state === "initializing") {
      postFailure("engine.worker.initialization_failed", getSafeRouting(value));
      return;
    }
    if (state !== "idle" || endpoint === undefined || limits === undefined) {
      postFailure("engine.worker.malformed_message", getSafeRouting(value));
      return;
    }
    const message = validateRequestMessage(value, limits);
    if (message === undefined) {
      postFailure("engine.worker.malformed_message", getSafeRouting(value));
      return;
    }
    if (message.endpoint_generation !== generation) {
      postFailure("engine.worker.stale_generation", {endpoint_generation: message.endpoint_generation, exchange_id: message.exchange_id});
      return;
    }
    const route = {endpoint_generation: message.endpoint_generation, exchange_id: message.exchange_id};
    try {
      scope.postMessage({
        worker_protocol: workerProtocol,
        worker_protocol_version: workerProtocolVersion,
        kind: "accepted",
        ...route,
      }, []);
    } catch {
      state = "crashed";
      safeDisposeEndpoint();
      close();
      return;
    }
    state = "executing";
    let result: EngineByteEndpointResult;
    try {
      result = endpoint.request(Object.freeze({control: message.control, blobs: message.blobs}));
    } catch {
      crash(route);
      return;
    }
    if (!result.ok) {
      if (validateFailure(result.failure) === undefined) {
        crash(route);
        return;
      }
      postFailure(result.failure.code, route);
      if (result.failure.code === "engine.worker.malformed_message" || result.failure.code === "engine.worker.stale_generation") {
        state = "idle";
      } else {
        state = "crashed";
        safeDisposeEndpoint();
        close();
      }
      return;
    }
    const validated = validateEndpointResponse(result.response, limits);
    if (validated === undefined) {
      crash(route);
      return;
    }
    try {
      scope.postMessage({
        worker_protocol: workerProtocol,
        worker_protocol_version: workerProtocolVersion,
        kind: "response",
        ...route,
        control: validated.control,
        blobs: validated.blobs,
      }, transferList(validated.control, validated.blobs));
      state = "idle";
    } catch {
      state = "crashed";
      safeDisposeEndpoint();
      postFailure("engine.worker.transfer_failed", route);
      close();
    }
  };

  function onMessage(event: WorkerMessageEventLike): void {
    if (state === "disposed" || state === "crashed") return;
    const kind = getSafeKind(event.data);
    if (kind === "init") {
      const message = validateInitMessage(event.data);
      if (message === undefined) postFailure("engine.worker.malformed_message", getSafeRouting(event.data));
      else initialize(message);
      return;
    }
    if (kind === "dispose") {
      const message = validateDisposeMessage(event.data);
      if (message === undefined) postFailure("engine.worker.malformed_message", getSafeRouting(event.data));
      else dispose(message.endpoint_generation);
      return;
    }
    if (kind === "request") {
      request(event.data);
      return;
    }
    postFailure("engine.worker.malformed_message", getSafeRouting(event.data));
  }

  function onMessageError(): void {
    crash(generation === undefined ? {} : {endpoint_generation: generation});
  }

  scope.addEventListener("message", onMessage);
  scope.addEventListener("messageerror", onMessageError);

  return (): void => {
    if (state === "disposed") return;
    state = "disposed";
    safeDisposeEndpoint();
    removeListeners();
  };
}
