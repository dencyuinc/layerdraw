// SPDX-License-Identifier: Apache-2.0

import type { Digest } from "@layerdraw/protocol/common";
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
  schemaDigest,
} from "@layerdraw/protocol/runtime";
import type { EngineClient, EngineClientCreationOptions } from "./index.js";
import { EngineClientInputError, EngineClientTransportError } from "./index.js";
import {
  createStdioByteTransport,
  createStdioEngineClient,
  type StdioProcessLifecycle,
} from "./stdio.js";
import type {
  InternalByteTransport,
  InternalTransportBlob,
  InternalTransportExchange,
} from "./internal/transport.js";

const protocol = Object.freeze({ name: "runtime", version: "1.0" as const });
const defaultRuntimeCapabilities = Object.freeze([
  "runtime.handshake", "runtime.open_document", "runtime.inspect_document",
  "runtime.preview_operations", "runtime.commit_operations", "runtime.save_document",
  "runtime.control_autosave", "runtime.get_state_snapshot", "runtime.list_revisions",
  "runtime.preview_restore", "runtime.stage_asset", "runtime.close_document",
  "runtime.cancel_operation", "runtime.get_operation_result", "runtime.recover_operations",
]);

export interface LocalHostRequestOptions {
  readonly requestId?: string;
  readonly signal?: AbortSignal;
}

export interface CreateLocalHostClientOptions {
  readonly binaryPath: string;
  readonly storageRoot: string;
  readonly expectedReleaseManifestDigest: Digest;
  readonly cwd?: string;
  readonly processLifecycle?: StdioProcessLifecycle;
  readonly requiredCapabilities?: readonly string[];
  readonly optionalCapabilities?: readonly string[];
  readonly engineClient?: Omit<EngineClientCreationOptions, "expectedReleaseManifestDigest">;
}

export interface LocalHostClient {
  readonly engine: EngineClient;
  readonly state: "ready" | "failed" | "disposing" | "disposed";
  getCapabilities(): Runtime.RuntimeCapabilityManifest;
  hasCapability(operation: string): boolean;
  openDocument(input: Runtime.OpenRuntimeDocumentInput, options?: LocalHostRequestOptions): Promise<Runtime.OpenDocumentResponseEnvelope>;
  inspectDocument(input: Runtime.RuntimeSessionInput, options?: LocalHostRequestOptions): Promise<Runtime.InspectDocumentResponseEnvelope>;
  previewOperations(input: Runtime.PreviewOperationsInput, options?: LocalHostRequestOptions): Promise<Runtime.PreviewOperationsResponseEnvelope>;
  commitOperations(input: Runtime.RuntimeCommitInput, options?: LocalHostRequestOptions): Promise<Runtime.CommitOperationsResponseEnvelope>;
  saveDocument(input: Runtime.RuntimeCommitInput, options?: LocalHostRequestOptions): Promise<Runtime.SaveDocumentResponseEnvelope>;
  controlAutosave(input: Runtime.AutosaveControlInput, options?: LocalHostRequestOptions): Promise<Runtime.AutosaveControlResponseEnvelope>;
  getStateSnapshot(input: Runtime.RuntimeSessionInput, options?: LocalHostRequestOptions): Promise<Runtime.StateSnapshotResponseEnvelope>;
  listRevisions(input: Runtime.ListRevisionsInput, options?: LocalHostRequestOptions): Promise<Runtime.ListRevisionsResponseEnvelope>;
  previewRestore(input: Runtime.RestorePreviewInput, options?: LocalHostRequestOptions): Promise<Runtime.RestorePreviewResponseEnvelope>;
  stageAsset(input: Runtime.StageAssetInput, bytes: Uint8Array, options?: LocalHostRequestOptions): Promise<Runtime.StageAssetResponseEnvelope>;
  closeDocument(input: Runtime.RuntimeSessionInput, options?: LocalHostRequestOptions): Promise<Runtime.CloseRuntimeDocumentResponseEnvelope>;
  cancelOperation(input: Runtime.CancelOperationInput, options?: LocalHostRequestOptions): Promise<Runtime.CancelOperationResponseEnvelope>;
  getOperationResult(input: Runtime.GetOperationResultInput, options?: LocalHostRequestOptions): Promise<Runtime.GetOperationResultResponseEnvelope>;
  recoverOperations(input: Runtime.RecoverOperationsInput, options?: LocalHostRequestOptions): Promise<Runtime.RecoverOperationsResponseEnvelope>;
  restart(): Promise<void>;
  dispose(): Promise<void>;
}

const encoder = new TextEncoder();
const decoder = new TextDecoder("utf-8", { fatal: true });

function validString(value: unknown): value is string {
  return typeof value === "string" && value.length > 0 && !value.includes("\0");
}

function buffer(text: string): ArrayBuffer {
  const bytes = encoder.encode(text);
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength);
}

class LocalHostClientImpl implements LocalHostClient {
  readonly engine: EngineClient;
  state: "ready" | "failed" | "disposing" | "disposed" = "ready";
  private transport: InternalByteTransport;
  private manifest: Runtime.RuntimeCapabilityManifest;
  private counter = 0;

  constructor(
    private readonly options: CreateLocalHostClientOptions,
    engine: EngineClient,
    transport: InternalByteTransport,
    manifest: Runtime.RuntimeCapabilityManifest,
  ) {
    this.engine = engine;
    this.transport = transport;
    this.manifest = manifest;
    void transport.closed.then(() => { if (this.state === "ready") this.state = "failed"; });
  }

  getCapabilities(): Runtime.RuntimeCapabilityManifest { return this.manifest; }
  hasCapability(operation: string): boolean { return this.manifest.operations[operation]?.enabled === true; }

  private requestId(options?: LocalHostRequestOptions): string {
    if (options?.requestId !== undefined) {
      if (!validString(options.requestId) || Array.from(options.requestId).length > 128) throw new EngineClientInputError("INVALID_REQUEST_ID");
      return options.requestId;
    }
    this.counter++;
    return `local-host-${this.counter}`;
  }

  private async exchange<T>(operation: string, control: string, decode: (value: string) => T, blobs: readonly InternalTransportBlob[], options?: LocalHostRequestOptions): Promise<T> {
    if (this.state !== "ready") throw new EngineClientTransportError("PROCESS_EXITED");
    if (!this.hasCapability(operation)) throw new EngineClientTransportError("NEGOTIATION_REJECTED", false);
    let exchange: InternalTransportExchange;
    try { exchange = this.transport.request({ exchangeId: operation, control: buffer(control), blobs }); }
    catch {
      if (this.state === "ready") this.state = "failed";
      throw new EngineClientTransportError("BROKEN_PIPE");
    }
    const abort = (): void => { void exchange.cancel(); };
    if (options?.signal?.aborted) abort();
    else options?.signal?.addEventListener("abort", abort, { once: true });
    try {
      const response = await exchange.response;
      if (response.blobs.length !== 0) throw new EngineClientTransportError("BROKEN_PIPE", false);
      return decode(decoder.decode(response.control));
    } catch (error) {
      if (error instanceof EngineClientTransportError) throw error;
      if (this.state === "ready") this.state = "failed";
      throw new EngineClientTransportError("PROCESS_EXITED");
    } finally {
      options?.signal?.removeEventListener("abort", abort);
    }
  }

  openDocument(input: Runtime.OpenRuntimeDocumentInput, options?: LocalHostRequestOptions) { const requestId = this.requestId(options); return this.exchange("runtime.open_document", encodeOpenDocumentRequestEnvelope({ protocol, request_id: requestId, operation: "runtime.open_document", payload: input }), decodeOpenDocumentResponseEnvelope, [], options); }
  inspectDocument(input: Runtime.RuntimeSessionInput, options?: LocalHostRequestOptions) { const requestId = this.requestId(options); return this.exchange("runtime.inspect_document", encodeInspectDocumentRequestEnvelope({ protocol, request_id: requestId, operation: "runtime.inspect_document", payload: input }), decodeInspectDocumentResponseEnvelope, [], options); }
  previewOperations(input: Runtime.PreviewOperationsInput, options?: LocalHostRequestOptions) { const requestId = this.requestId(options); return this.exchange("runtime.preview_operations", encodePreviewOperationsRequestEnvelope({ protocol, request_id: requestId, operation: "runtime.preview_operations", payload: input }), decodePreviewOperationsResponseEnvelope, [], options); }
  commitOperations(input: Runtime.RuntimeCommitInput, options?: LocalHostRequestOptions) { const requestId = this.requestId(options); return this.exchange("runtime.commit_operations", encodeCommitOperationsRequestEnvelope({ protocol, request_id: requestId, operation: "runtime.commit_operations", payload: input }), decodeCommitOperationsResponseEnvelope, [], options); }
  saveDocument(input: Runtime.RuntimeCommitInput, options?: LocalHostRequestOptions) { const requestId = this.requestId(options); return this.exchange("runtime.save_document", encodeSaveDocumentRequestEnvelope({ protocol, request_id: requestId, operation: "runtime.save_document", payload: input }), decodeSaveDocumentResponseEnvelope, [], options); }
  controlAutosave(input: Runtime.AutosaveControlInput, options?: LocalHostRequestOptions) { const requestId = this.requestId(options); return this.exchange("runtime.control_autosave", encodeAutosaveControlRequestEnvelope({ protocol, request_id: requestId, operation: "runtime.control_autosave", payload: input }), decodeAutosaveControlResponseEnvelope, [], options); }
  getStateSnapshot(input: Runtime.RuntimeSessionInput, options?: LocalHostRequestOptions) { const requestId = this.requestId(options); return this.exchange("runtime.get_state_snapshot", encodeStateSnapshotRequestEnvelope({ protocol, request_id: requestId, operation: "runtime.get_state_snapshot", payload: input }), decodeStateSnapshotResponseEnvelope, [], options); }
  listRevisions(input: Runtime.ListRevisionsInput, options?: LocalHostRequestOptions) { const requestId = this.requestId(options); return this.exchange("runtime.list_revisions", encodeListRevisionsRequestEnvelope({ protocol, request_id: requestId, operation: "runtime.list_revisions", payload: input }), decodeListRevisionsResponseEnvelope, [], options); }
  previewRestore(input: Runtime.RestorePreviewInput, options?: LocalHostRequestOptions) { const requestId = this.requestId(options); return this.exchange("runtime.preview_restore", encodeRestorePreviewRequestEnvelope({ protocol, request_id: requestId, operation: "runtime.preview_restore", payload: input }), decodeRestorePreviewResponseEnvelope, [], options); }
  stageAsset(input: Runtime.StageAssetInput, bytes: Uint8Array, options?: LocalHostRequestOptions) { const requestId = this.requestId(options); const copied = bytes.slice().buffer; return this.exchange("runtime.stage_asset", encodeStageAssetRequestEnvelope({ protocol, request_id: requestId, operation: "runtime.stage_asset", payload: input }), decodeStageAssetResponseEnvelope, [{ blobId: input.content_blob.blob_id, bytes: copied }], options); }
  closeDocument(input: Runtime.RuntimeSessionInput, options?: LocalHostRequestOptions) { const requestId = this.requestId(options); return this.exchange("runtime.close_document", encodeCloseRuntimeDocumentRequestEnvelope({ protocol, request_id: requestId, operation: "runtime.close_document", payload: input }), decodeCloseRuntimeDocumentResponseEnvelope, [], options); }
  cancelOperation(input: Runtime.CancelOperationInput, options?: LocalHostRequestOptions) { const requestId = this.requestId(options); return this.exchange("runtime.cancel_operation", encodeCancelOperationRequestEnvelope({ protocol, request_id: requestId, operation: "runtime.cancel_operation", payload: input }), decodeCancelOperationResponseEnvelope, [], options); }
  getOperationResult(input: Runtime.GetOperationResultInput, options?: LocalHostRequestOptions) { const requestId = this.requestId(options); return this.exchange("runtime.get_operation_result", encodeGetOperationResultRequestEnvelope({ protocol, request_id: requestId, operation: "runtime.get_operation_result", payload: input }), decodeGetOperationResultResponseEnvelope, [], options); }
  recoverOperations(input: Runtime.RecoverOperationsInput, options?: LocalHostRequestOptions) { const requestId = this.requestId(options); return this.exchange("runtime.recover_operations", encodeRecoverOperationsRequestEnvelope({ protocol, request_id: requestId, operation: "runtime.recover_operations", payload: input }), decodeRecoverOperationsResponseEnvelope, [], options); }

  async restart(): Promise<void> {
    if (this.state === "disposed" || this.state === "disposing") throw new EngineClientTransportError("REPLACEMENT_FAILED", false);
    const prior = this.transport;
    this.transport = createRuntimeTransport(this.options);
    try {
      this.manifest = await handshake(this.transport, this.options);
      await this.engine.restart();
      await prior.dispose();
      this.state = "ready";
    } catch {
      this.transport.terminate();
      this.state = "failed";
      throw new EngineClientTransportError("REPLACEMENT_FAILED");
    }
  }

  async dispose(): Promise<void> {
    if (this.state === "disposed") return;
    this.state = "disposing";
    await Promise.allSettled([this.transport.dispose(), this.engine.dispose()]);
    this.state = "disposed";
  }
}

function createRuntimeTransport(options: CreateLocalHostClientOptions): InternalByteTransport {
  return createStdioByteTransport({
    binaryPath: options.binaryPath,
    binaryArguments: ["stdio", "--root", options.storageRoot],
    ...(options.cwd === undefined ? {} : { cwd: options.cwd }),
    ...(options.processLifecycle === undefined ? {} : { processLifecycle: options.processLifecycle }),
  });
}

async function handshake(transport: InternalByteTransport, options: CreateLocalHostClientOptions): Promise<Runtime.RuntimeCapabilityManifest> {
  await transport.ready;
  const required = [...(options.requiredCapabilities ?? defaultRuntimeCapabilities)];
  const optional = [...(options.optionalCapabilities ?? [])];
  const request: Runtime.RuntimeHandshakeRequestEnvelope = {
    protocol, request_id: "local-host-handshake", operation: "runtime.handshake",
    payload: { client_release: "0.0.0", protocols: [{ name: "runtime", supported_range: "1.0..1.0", versions: [{ version: "1.0", schema_digest: schemaDigest }] }], required_capabilities: required, optional_capabilities: optional },
  };
  const exchange = transport.request({ exchangeId: request.request_id, control: buffer(encodeRuntimeHandshakeRequestEnvelope(request)), blobs: [] });
  const raw = await exchange.response;
  if (raw.blobs.length !== 0) {
    transport.terminate();
    throw new EngineClientTransportError("NEGOTIATION_REJECTED", false);
  }
  const response = decodeRuntimeHandshakeResponseEnvelope(decoder.decode(raw.control));
  if (response.outcome !== "success" || response.payload === undefined || response.payload.release_manifest_digest !== options.expectedReleaseManifestDigest) {
    transport.terminate();
    throw new EngineClientTransportError("NEGOTIATION_REJECTED", false);
  }
  for (const operation of required) {
    if (response.payload.capability_manifest.operations[operation]?.enabled !== true) {
      transport.terminate();
      throw new EngineClientTransportError("NEGOTIATION_REJECTED", false);
    }
  }
  return response.payload.capability_manifest;
}

export async function createLocalHostClient(options: CreateLocalHostClientOptions): Promise<LocalHostClient> {
  if (!validString(options?.binaryPath) || !validString(options?.storageRoot) || !validString(options?.expectedReleaseManifestDigest)) {
    throw new EngineClientInputError("INVALID_ARGUMENT");
  }
  let runtimeTransport: InternalByteTransport | undefined;
  try {
    runtimeTransport = createRuntimeTransport(options);
    const [manifest, engine] = await Promise.all([
      handshake(runtimeTransport, options),
      createStdioEngineClient({
        binaryPath: options.binaryPath,
        binaryArguments: ["engine-stdio", "--root", options.storageRoot],
        ...(options.cwd === undefined ? {} : { cwd: options.cwd }),
        ...(options.processLifecycle === undefined ? {} : { processLifecycle: options.processLifecycle }),
        client: { expectedReleaseManifestDigest: options.expectedReleaseManifestDigest, ...(options.engineClient ?? {}) },
      }),
    ]);
    return new LocalHostClientImpl(options, engine, runtimeTransport, manifest);
  } catch (error) {
    runtimeTransport?.terminate();
    if (error instanceof EngineClientInputError || error instanceof EngineClientTransportError) throw error;
    throw new EngineClientTransportError("SPAWN_FAILED");
  }
}
