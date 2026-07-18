// SPDX-License-Identifier: Apache-2.0

import type {
  BlobRef,
  CapabilityID,
  CapabilityManifest,
  CompileResourceLimitConstraints,
  Digest,
  HandshakeResult,
  ProtocolFailure,
} from "@layerdraw/protocol/common";
import type {
  CompileInput,
  CompileResponseEnvelope,
  CompileResult,
  WorkbenchFailure,
} from "@layerdraw/protocol/engine";
import type * as EngineProtocol from "@layerdraw/protocol/engine";
import { snapshotSafeDetails } from "./internal/safe-details.js";

export type EngineClientState =
  | "ready"
  | "replacing"
  | "failed"
  | "disposing"
  | "disposed";

export interface EngineEndpointSnapshot {
  readonly generation: number;
  readonly handshake: HandshakeResult;
}

export type RequestBlobRef = BlobRef & { readonly lifetime: "request" };

export type CompileRequestBlob =
  | Readonly<{
      ref: RequestBlobRef;
      bytes: Uint8Array;
      ownership?: "copy";
    }>
  | Readonly<{
      ref: RequestBlobRef;
      bytes: ArrayBuffer;
      ownership: "transfer";
    }>;

export interface PortableCompileRequest {
  readonly input: CompileInput;
  readonly blobs: readonly CompileRequestBlob[];
}

export interface EngineAbortSignal {
  readonly aborted: boolean;
  readonly reason?: unknown;
  addEventListener(
    type: "abort",
    listener: () => void,
    options?: boolean | Readonly<{ once?: boolean }>,
  ): void;
  removeEventListener(type: "abort", listener: () => void): void;
}

export interface CompileOptions {
  readonly requestId?: string;
  readonly signal?: EngineAbortSignal;
  readonly timeoutMs?: number;
}

export interface WorkbenchOptions extends CompileOptions {
  readonly blobs?: readonly CompileRequestBlob[];
}

export interface OutputBlob {
  readonly ref: BlobRef;
  readonly bytes: Uint8Array;
}

type ResponseBase = Omit<
  CompileResponseEnvelope,
  "outcome" | "payload" | "failure"
>;

export type CompileSuccessResponse = ResponseBase &
  Readonly<{
    outcome: "success";
    payload: CompileResult;
    failure?: never;
  }>;

export type CompileRejectedResponse = ResponseBase &
  Readonly<{
    outcome: "rejected";
    payload?: never;
    failure?: never;
  }>;

export type CompileFailedResponse = ResponseBase &
  Readonly<{
    outcome: "failed";
    payload?: never;
    failure: ProtocolFailure;
  }>;

export type CompileCancelledResponse = ResponseBase &
  Readonly<{
    outcome: "cancelled";
    payload?: never;
    failure: ProtocolFailure;
  }>;

export type ClientCancellationReason =
  | "signal"
  | "timeout"
  | "dispose"
  | "restart";

export type CompileOutcome =
  | Readonly<{
      origin: "engine";
      outcome: "success";
      response: CompileSuccessResponse;
      blobs: readonly OutputBlob[];
    }>
  | Readonly<{
      origin: "engine";
      outcome: "rejected";
      response: CompileRejectedResponse;
      blobs: readonly [];
    }>
  | Readonly<{
      origin: "engine";
      outcome: "failed";
      response: CompileFailedResponse;
      blobs: readonly [];
    }>
  | Readonly<{
      origin: "engine";
      outcome: "cancelled";
      response: CompileCancelledResponse;
      blobs: readonly [];
    }>
  | Readonly<{
      origin: "client";
      outcome: "cancelled";
      requestId: string;
      endpointGeneration: number;
      reason: ClientCancellationReason;
      blobs: readonly [];
    }>;

type WorkbenchResponseBase = Readonly<{
  diagnostics: readonly unknown[];
  engine_release: string;
  failure?: WorkbenchFailure;
  outcome: "success" | "rejected" | "failed" | "cancelled";
  payload?: unknown;
  protocol: { readonly name: "engine"; readonly version: "1.0" };
  request_id: string;
}>;

export type WorkbenchOutcome<TResponse extends WorkbenchResponseBase> =
  | Readonly<{
      origin: "engine";
      outcome: TResponse["outcome"];
      response: TResponse;
      blobs: readonly OutputBlob[];
    }>
  | Readonly<{
      origin: "client";
      outcome: "cancelled";
      requestId: string;
      endpointGeneration: number;
      reason: ClientCancellationReason;
      blobs: readonly [];
    }>;

export interface EngineWorkbenchClient {
  applyToHandle(
    input: EngineProtocol.ApplyToHandleInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.ApplyToHandleResponseEnvelope>>;
  closeDocument(
    input: EngineProtocol.CloseDocumentInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.CloseDocumentResponseEnvelope>>;
  executeQuery(
    input: EngineProtocol.ExecuteQueryInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.ExecuteQueryResponseEnvelope>>;
  materializeView(
    input: EngineProtocol.MaterializeViewInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.MaterializeViewResponseEnvelope>>;
  planExport(
    input: EngineProtocol.PlanExportInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.PlanExportResponseEnvelope>>;
  findSymbols(
    input: EngineProtocol.FindSymbolsInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.FindSymbolsResponseEnvelope>>;
  findUsages(
    input: EngineProtocol.FindUsagesInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.FindUsagesResponseEnvelope>>;
  formatScope(
    input: EngineProtocol.FormatScopeInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.FormatScopeResponseEnvelope>>;
  getNeighbors(
    input: EngineProtocol.GetNeighborsInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.GetNeighborsResponseEnvelope>>;
  inspectSubgraph(
    input: EngineProtocol.InspectSubgraphInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.InspectSubgraphResponseEnvelope>>;
  listModules(
    input: EngineProtocol.ListModulesInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.ListModulesResponseEnvelope>>;
  listReferences(
    input: EngineProtocol.ListReferencesInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.ListReferencesResponseEnvelope>>;
  openDocument(
    input: EngineProtocol.OpenDocumentInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.OpenDocumentResponseEnvelope>>;
  organizeWorkspace(
    input: EngineProtocol.OrganizeWorkspaceInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.OrganizeWorkspaceResponseEnvelope>>;
  previewFragment(
    input: EngineProtocol.PreviewFragmentInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.PreviewFragmentResponseEnvelope>>;
  previewSourcePatch(
    input: EngineProtocol.PreviewSourcePatchInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.PreviewSourcePatchResponseEnvelope>>;
  readDeclarations(
    input: EngineProtocol.ReadDeclarationsInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.ReadDeclarationsResponseEnvelope>>;
  readModules(
    input: EngineProtocol.ReadModulesInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.ReadModulesResponseEnvelope>>;
  readReferences(
    input: EngineProtocol.ReadReferencesInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.ReadReferencesResponseEnvelope>>;
  readRows(
    input: EngineProtocol.ReadRowsInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.ReadRowsResponseEnvelope>>;
  readScope(
    input: EngineProtocol.ReadScopeInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.ReadScopeResponseEnvelope>>;
  replaceSourceTree(
    input: EngineProtocol.ReplaceSourceTreeInput,
    options?: WorkbenchOptions,
  ): Promise<WorkbenchOutcome<EngineProtocol.ReplaceSourceTreeResponseEnvelope>>;
}

export interface EngineClient {
  readonly state: EngineClientState;
  readonly workbench: EngineWorkbenchClient;
  getEndpoint(): EngineEndpointSnapshot;
  getCapabilities(): CapabilityManifest;
  hasCapability(capability: CapabilityID): boolean;
  compile(
    request: PortableCompileRequest,
    options?: CompileOptions,
  ): Promise<CompileOutcome>;
  restart(): Promise<void>;
  dispose(): Promise<void>;
}

export interface EngineClientCreationOptions {
  readonly expectedReleaseManifestDigest: Digest;
  readonly clientLimits?: CompileResourceLimitConstraints;
  readonly requiredCapabilities?: readonly CapabilityID[];
  readonly optionalCapabilities?: readonly CapabilityID[];
  readonly handshakeTimeoutMs?: number;
  readonly defaultCompileTimeoutMs?: number;
  readonly cancelGraceMs?: number;
  readonly disposeTimeoutMs?: number;
}

export type EngineClientErrorKind = "misuse" | "transport" | "decode";

export type EngineClientSafeDetails = Readonly<
  Record<string, string | number | boolean>
>;

export type EngineClientInputErrorCode =
  | "INVALID_ARGUMENT"
  | "INVALID_REQUEST_ID"
  | "INVALID_BLOB_TABLE"
  | "UNSUPPORTED_BYTE_OWNERSHIP"
  | "BLOB_SIZE_MISMATCH"
  | "BLOB_DIGEST_MISMATCH"
  | "LIMIT_EXCEEDED";

export type EngineClientStateErrorCode =
  | "NOT_READY"
  | "FAILED"
  | "DISPOSING"
  | "DISPOSED"
  | "DUPLICATE_REQUEST_ID";

export type EngineClientBackpressureErrorCode =
  | "SINGLE_FLIGHT_BUSY"
  | "BYTE_BUDGET_EXCEEDED";

export type EngineClientTransportErrorCode =
  | "NEGOTIATION_REJECTED"
  | "SPAWN_FAILED"
  | "CONNECT_FAILED"
  | "BROKEN_PIPE"
  | "PROCESS_EXITED"
  | "WORKER_CRASHED"
  | "TRANSFER_FAILED"
  | "DIGEST_FAILED"
  | "TIMEOUT_DURING_CREATION"
  | "REPLACEMENT_FAILED";

export type EngineClientDecodeErrorCode =
  | "MALFORMED_FRAME"
  | "MALFORMED_MESSAGE"
  | "CORRELATION_MISMATCH"
  | "PROTOCOL_MISMATCH"
  | "UNEXPECTED_BLOB"
  | "MISSING_BLOB"
  | "OUTPUT_SIZE_MISMATCH"
  | "OUTPUT_DIGEST_MISMATCH";

const messages = Object.freeze({
  INVALID_ARGUMENT: "The client argument is invalid.",
  INVALID_REQUEST_ID: "The request ID is invalid.",
  INVALID_BLOB_TABLE: "The request blob table is invalid.",
  UNSUPPORTED_BYTE_OWNERSHIP: "The requested byte ownership is unsupported.",
  BLOB_SIZE_MISMATCH: "Blob bytes do not match the declared size.",
  BLOB_DIGEST_MISMATCH: "Blob bytes do not match the declared digest.",
  LIMIT_EXCEEDED: "A client or transport limit was exceeded.",
  NOT_READY: "The Engine client is not ready.",
  FAILED: "The Engine client is failed.",
  DISPOSING: "The Engine client is disposing.",
  DISPOSED: "The Engine client is disposed.",
  DUPLICATE_REQUEST_ID: "The request ID is already active.",
  SINGLE_FLIGHT_BUSY: "The Engine client already has an active compile.",
  BYTE_BUDGET_EXCEEDED: "The Engine client byte budget is exhausted.",
  NEGOTIATION_REJECTED: "Engine protocol negotiation was rejected.",
  SPAWN_FAILED: "The Engine endpoint could not be started.",
  CONNECT_FAILED: "The Engine endpoint could not be connected.",
  BROKEN_PIPE: "The Engine transport connection was lost.",
  PROCESS_EXITED: "The Engine process exited unexpectedly.",
  WORKER_CRASHED: "The Engine Worker stopped unexpectedly.",
  TRANSFER_FAILED: "The Engine transport could not transfer the request.",
  DIGEST_FAILED: "The client could not verify blob digests.",
  TIMEOUT_DURING_CREATION: "Engine client creation timed out.",
  REPLACEMENT_FAILED: "The Engine endpoint could not be replaced.",
  MALFORMED_FRAME: "The Engine transport produced a malformed frame.",
  MALFORMED_MESSAGE: "The Engine endpoint produced a malformed message.",
  CORRELATION_MISMATCH: "The Engine response correlation is invalid.",
  PROTOCOL_MISMATCH: "The Engine response protocol is incompatible.",
  UNEXPECTED_BLOB: "The Engine response included an unexpected blob.",
  MISSING_BLOB: "The Engine response omitted a required blob.",
  OUTPUT_SIZE_MISMATCH: "Engine output bytes do not match the declared size.",
  OUTPUT_DIGEST_MISMATCH: "Engine output bytes do not match the declared digest.",
  UNSUPPORTED_ENVIRONMENT: "The current environment cannot host the Engine client.",
  DISPOSE_FAILED: "The Engine endpoint could not be disposed cleanly.",
} as const);

function safeDetails(
  details: unknown,
): EngineClientSafeDetails | undefined {
  const snapshot = snapshotSafeDetails(details);
  return snapshot.valid
    ? snapshot.details as EngineClientSafeDetails | undefined
    : undefined;
}

export abstract class EngineClientError extends Error {
  readonly kind: EngineClientErrorKind;
  readonly code: string;
  readonly retryable: boolean;
  readonly details?: EngineClientSafeDetails;

  protected constructor(
    kind: EngineClientErrorKind,
    code: keyof typeof messages,
    retryable: boolean,
    details?: EngineClientSafeDetails,
  ) {
    super(messages[code]);
    this.name = new.target.name;
    this.kind = kind;
    this.code = code;
    this.retryable = retryable;
    const copiedDetails = safeDetails(details);
    if (copiedDetails !== undefined) this.details = copiedDetails;
  }
}

export class EngineClientInputError extends EngineClientError {
  constructor(
    code: EngineClientInputErrorCode,
    details?: EngineClientSafeDetails,
  ) {
    super("misuse", code, false, details);
  }
}

export class EngineClientStateError extends EngineClientError {
  constructor(
    code: EngineClientStateErrorCode,
    details?: EngineClientSafeDetails,
  ) {
    super("misuse", code, false, details);
  }
}

export class EngineClientBackpressureError extends EngineClientError {
  constructor(
    code: EngineClientBackpressureErrorCode,
    details?: EngineClientSafeDetails,
  ) {
    super("misuse", code, true, details);
  }
}

export class EngineClientTransportError extends EngineClientError {
  constructor(
    code: EngineClientTransportErrorCode,
    retryable = true,
    details?: EngineClientSafeDetails,
  ) {
    super("transport", code, retryable, details);
  }
}

export class EngineClientDecodeError extends EngineClientError {
  constructor(
    code: EngineClientDecodeErrorCode,
    details?: EngineClientSafeDetails,
  ) {
    super("decode", code, false, details);
  }
}

export class EngineClientUnsupportedEnvironmentError extends EngineClientTransportError {
  constructor(details?: EngineClientSafeDetails) {
    super("CONNECT_FAILED", false, details);
    this.name = new.target.name;
    Object.defineProperty(this, "code", {
      configurable: true,
      enumerable: true,
      value: "UNSUPPORTED_ENVIRONMENT",
    });
    Object.defineProperty(this, "message", {
      configurable: true,
      enumerable: false,
      value: messages.UNSUPPORTED_ENVIRONMENT,
    });
  }
}

export class EngineClientDisposeError extends EngineClientTransportError {
  constructor(details?: EngineClientSafeDetails) {
    super("CONNECT_FAILED", false, details);
    this.name = new.target.name;
    Object.defineProperty(this, "code", {
      configurable: true,
      enumerable: true,
      value: "DISPOSE_FAILED",
    });
    Object.defineProperty(this, "message", {
      configurable: true,
      enumerable: false,
      value: messages.DISPOSE_FAILED,
    });
  }
}
