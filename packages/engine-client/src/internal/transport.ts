// SPDX-License-Identifier: Apache-2.0

import type { BlobRef } from "@layerdraw/protocol/common";
import type { CompileInput, CompileResult } from "@layerdraw/protocol/engine";
import type {
  EngineClientDecodeErrorCode,
  EngineClientSafeDetails,
  EngineClientTransportErrorCode,
} from "../index.js";

/**
 * Private, protocol-agnostic byte ownership boundary used by future package
 * entrypoints. This module is deliberately absent from the package export map.
 */
export interface InternalTransportBlob {
  readonly blobId: string;
  readonly bytes: ArrayBuffer;
}

export interface InternalTransportRequest {
  readonly exchangeId: string;
  readonly control: ArrayBuffer;
  readonly blobs: readonly InternalTransportBlob[];
}

export interface InternalTransportResponse {
  readonly control: ArrayBuffer;
  readonly blobs: readonly InternalTransportBlob[];
}

export interface InternalTransportLimits {
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

export interface InternalCancelResult {
  /** True only after the request is joined and this endpoint can accept work. */
  readonly reusable: boolean;
}

export interface InternalTransportExchange {
  /** Settles exactly once. Rejection data is treated as untrusted by the core. */
  readonly response: Promise<InternalTransportResponse>;
  /**
   * Cancels and joins this exchange within the adapter's bounded policy. The
   * response promise must also be terminal when this promise resolves.
   */
  cancel(): Promise<InternalCancelResult>;
}

export interface InternalTransportClose {
  readonly code: Extract<
    EngineClientTransportErrorCode,
    "BROKEN_PIPE" | "PROCESS_EXITED" | "WORKER_CRASHED"
  >;
  readonly retryable: boolean;
  readonly details?: EngineClientSafeDetails;
}

export interface InternalByteTransport {
  /** Transport/runtime readiness only. The core performs Engine handshake. */
  readonly ready: Promise<InternalTransportLimits>;
  /**
   * Resolves once loops/resources stop and every exchange response is terminal.
   */
  readonly closed: Promise<InternalTransportClose>;
  request(input: InternalTransportRequest): InternalTransportExchange;
  /** Immediate hard stop. This method must not throw raw platform data. */
  terminate(): void;
  /** Graceful close followed by a complete resource join. */
  dispose(): Promise<void>;
}

export interface InternalTransportFactory {
  readonly transportId: string;
  readonly connectFailureCode: Extract<
    EngineClientTransportErrorCode,
    "SPAWN_FAILED" | "CONNECT_FAILED"
  >;
  /** Must synchronously create a fresh endpoint for every invocation. */
  create(endpointGeneration: string): InternalByteTransport;
}

/**
 * These functions are generated protocol dependencies. They remain injected
 * until the accepted #27 generator closure is composed with this package.
 */
export interface InternalProtocolBlobRefCollectors {
  collectCompileInputBlobRefs(value: CompileInput): readonly BlobRef[];
  collectCompileResultBlobRefs(value: CompileResult): readonly BlobRef[];
}

export type InternalFaultKind = "transport" | "decode";

/** Branded safe adapter failure. Arbitrary rejections are always redacted. */
export class InternalTransportFault extends Error {
  readonly kind: InternalFaultKind;
  readonly code: EngineClientTransportErrorCode | EngineClientDecodeErrorCode;
  readonly retryable: boolean;
  readonly details?: EngineClientSafeDetails;

  constructor(options: Readonly<{
    kind: InternalFaultKind;
    code: EngineClientTransportErrorCode | EngineClientDecodeErrorCode;
    retryable: boolean;
    details?: EngineClientSafeDetails;
  }>) {
    super("Internal byte transport fault");
    this.name = "InternalTransportFault";
    this.kind = options.kind;
    this.code = options.code;
    this.retryable = options.retryable;
    if (options.details !== undefined) this.details = options.details;
  }
}
