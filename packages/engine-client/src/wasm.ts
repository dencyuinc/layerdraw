// SPDX-License-Identifier: Apache-2.0

import {
  EngineWorkerTransportError,
  createEngineWorkerTransport,
  type EngineWorkerFailure,
  type EngineWorkerTransport,
  type EngineWorkerTransportLimits,
} from "@layerdraw/engine-wasm";
import { isDigest, type Digest } from "@layerdraw/protocol/common";
import {
  EngineClientInputError,
  type EngineClient,
  type EngineClientCreationOptions,
} from "./index.js";
import { createInternalEngineClient } from "./internal/client.js";
import { dataObject } from "./internal/guards.js";
import { protocolBlobRefCollectors } from "./internal/protocol-collectors.js";
import {
  InternalTransportFault,
  type InternalByteTransport,
  type InternalTransportRequest,
  type InternalTransportFactory,
  type InternalTransportLimits,
} from "./internal/transport.js";

export interface CreateWasmEngineClientOptions {
  readonly client: EngineClientCreationOptions;
  readonly expectedArtifactManifestDigest: Digest;
  readonly workerName?: string;
  readonly workerDisposeTimeoutMs?: number;
}

interface WasmAdapterOptions {
  readonly expectedArtifactManifestDigest: Digest;
  readonly releaseManifestDigest: Digest;
  readonly workerName?: string;
  readonly workerDisposeTimeoutMs?: number;
}

function workerFault(error: unknown): InternalTransportFault {
  const failure = error instanceof EngineWorkerTransportError
    ? error.failure
    : undefined;
  const code = failure?.code;
  if (
    code === "engine.worker.unsupported" ||
    code === "engine.worker.initialization_failed" ||
    code === "engine.worker.artifact_mismatch"
  ) {
    return new InternalTransportFault({
      kind: "transport",
      code: "CONNECT_FAILED",
      retryable: workerFailureRetryable(failure),
    });
  }
  if (
    code === "engine.worker.malformed_message" ||
    code === "engine.worker.stale_generation"
  ) {
    return new InternalTransportFault({
      kind: "decode",
      code: "MALFORMED_MESSAGE",
      retryable: false,
    });
  }
  if (code === "engine.worker.transfer_failed") {
    return new InternalTransportFault({
      kind: "transport",
      code: "TRANSFER_FAILED",
      retryable: workerFailureRetryable(failure),
    });
  }
  return new InternalTransportFault({
    kind: "transport",
    code: "WORKER_CRASHED",
    retryable: workerFailureRetryable(failure),
  });
}

function workerFailureRetryable(failure: EngineWorkerFailure | undefined): boolean {
  return failure?.retryable ?? true;
}

function mapLimits(limits: EngineWorkerTransportLimits): InternalTransportLimits {
  return Object.freeze({
    maxControlBytes: limits.max_control_bytes,
    maxControlDepth: limits.max_control_depth,
    maxBlobIdBytes: limits.max_blob_id_bytes,
    maxBuffers: limits.max_buffers,
    maxInputBlobBytes: limits.max_input_blob_bytes,
    maxInputTotalBytes: limits.max_input_total_bytes,
    maxOutputBlobBytes: limits.max_output_blob_bytes,
    maxOutputTotalBytes: limits.max_output_total_bytes,
    maxResponsePublishBytes: limits.max_response_publish_bytes,
  });
}

function adaptWorker(transport: EngineWorkerTransport): InternalByteTransport {
  const ready = transport.ready.then(mapLimits, (error: unknown) => {
    throw workerFault(error);
  });
  const closed = transport.closed.then(() => Object.freeze({
    code: "WORKER_CRASHED" as const,
    retryable: true,
  }));
  return Object.freeze({
    ready,
    closed,
    request(input: InternalTransportRequest) {
      let exchange;
      try {
        exchange = transport.request({
          exchangeID: input.exchangeId,
          control: input.control,
          blobs: input.blobs.map((blob) => ({
            blob_id: blob.blobId,
            bytes: blob.bytes,
          })),
        });
      } catch (error) {
        throw workerFault(error);
      }
      const response = exchange.response.then(
        (value) => Object.freeze({
          control: value.control,
          blobs: Object.freeze(value.blobs.map((blob) => Object.freeze({
            blobId: blob.blob_id,
            bytes: blob.bytes,
          }))),
        }),
        (error: unknown) => {
          throw workerFault(error);
        },
      );
      return Object.freeze({
        response,
        async cancel() {
          transport.terminate();
          await response.then(
            () => undefined,
            () => undefined,
          );
          return Object.freeze({ reusable: false });
        },
      });
    },
    terminate(): void {
      transport.terminate();
    },
    async dispose(): Promise<void> {
      await transport.dispose();
    },
  });
}

function wasmTransportFactory(options: WasmAdapterOptions): InternalTransportFactory {
  return Object.freeze({
    transportId: "wasm_worker",
    connectFailureCode: "CONNECT_FAILED",
    create(endpointGeneration: string): InternalByteTransport {
      return adaptWorker(createEngineWorkerTransport({
        endpointGeneration,
        expectedArtifactManifestDigest: options.expectedArtifactManifestDigest,
        releaseManifestDigest: options.releaseManifestDigest,
        ...(options.workerName === undefined ? {} : { workerName: options.workerName }),
        ...(options.workerDisposeTimeoutMs === undefined
          ? {}
          : { disposeTimeoutMilliseconds: options.workerDisposeTimeoutMs }),
      }));
    },
  });
}

export async function createWasmEngineClient(
  options: CreateWasmEngineClientOptions,
): Promise<EngineClient> {
  const value = dataObject(
    options,
    ["client", "expectedArtifactManifestDigest"],
    ["workerName", "workerDisposeTimeoutMs"],
  );
  const client = dataObject(
    value?.client,
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
    client === undefined ||
    !isDigest(value.expectedArtifactManifestDigest) ||
    (value.workerName !== undefined && typeof value.workerName !== "string") ||
    (value.workerDisposeTimeoutMs !== undefined &&
      (typeof value.workerDisposeTimeoutMs !== "number" ||
        !Number.isSafeInteger(value.workerDisposeTimeoutMs) ||
        value.workerDisposeTimeoutMs < 0 ||
        value.workerDisposeTimeoutMs > 60_000))
  ) {
    throw new EngineClientInputError("INVALID_ARGUMENT");
  }
  return createInternalEngineClient({
    transportFactory: wasmTransportFactory({
      expectedArtifactManifestDigest: value.expectedArtifactManifestDigest as Digest,
      releaseManifestDigest: client.expectedReleaseManifestDigest as Digest,
      ...(value.workerName === undefined
        ? {}
        : { workerName: value.workerName }),
      ...(value.workerDisposeTimeoutMs === undefined
        ? {}
        : { workerDisposeTimeoutMs: value.workerDisposeTimeoutMs }),
    }),
    protocolCollectors: protocolBlobRefCollectors,
    options: value.client as EngineClientCreationOptions,
  });
}
