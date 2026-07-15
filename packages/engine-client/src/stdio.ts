// SPDX-License-Identifier: Apache-2.0

import { spawn, type ChildProcess } from "node:child_process";
import type { Readable, Writable } from "node:stream";
import type { Digest } from "@layerdraw/protocol/common";
import { maxWireJSONDepth } from "@layerdraw/protocol/engine";
import {
  EngineClientInputError,
  type EngineClient,
  type EngineClientCreationOptions,
} from "./index.js";
import { createInternalEngineClient } from "./internal/client.js";
import { dataObject, strictArray } from "./internal/guards.js";
import { protocolBlobRefCollectors } from "./internal/protocol-collectors.js";
import {
  encodeStdioFrame,
  StdioFrameDecoder,
  StdioFrameError,
  stdioKind,
  stdioMaxChunkBytes,
  stdioMaxControlBytes,
  stdioMaxNameBytes,
  type StdioFrame,
} from "./internal/stdio-frame.js";
import {
  InternalTransportFault,
  type InternalByteTransport,
  type InternalTransportBlob,
  type InternalTransportFactory,
  type InternalTransportLimits,
  type InternalTransportResponse,
} from "./internal/transport.js";

export interface CreateStdioEngineClientOptions {
  readonly client: EngineClientCreationOptions;
  readonly binaryPath: string;
  readonly binaryArguments?: readonly string[];
  readonly cwd?: string;
}

interface StdioProcessOptions {
  readonly binaryPath: string;
  readonly binaryArguments: readonly string[];
  readonly cwd?: string;
}

interface Deferred<T> {
  readonly promise: Promise<T>;
  readonly settled: boolean;
  resolve(value: T): void;
  reject(reason: unknown): void;
}

interface OutputAssembly {
  readonly blobId: string;
  readonly name: Uint8Array;
  readonly chunks: Uint8Array[];
  size: number;
  final: boolean;
}

interface ActiveExchange {
  readonly streamId: bigint;
  readonly inputBlobs: readonly InternalTransportBlob[];
  readonly response: Deferred<InternalTransportResponse>;
  phase: "awaiting_ready" | "uploading" | "awaiting_response" | "reading_response" | "cancelling";
  cancelPromise?: Promise<Readonly<{ reusable: boolean }>>;
  cancelRequested: boolean;
  control?: ArrayBuffer;
  nextSequence: number;
  outputBlobs: OutputAssembly[];
  currentOutput?: OutputAssembly;
  outputBytes: number;
  streamError: boolean;
}

const textEncoder = new TextEncoder();
const textDecoder = new TextDecoder("utf-8", { fatal: true });

const stdioLimits: InternalTransportLimits = Object.freeze({
  maxControlBytes: stdioMaxControlBytes,
  maxControlDepth: maxWireJSONDepth,
  maxBlobIdBytes: stdioMaxNameBytes,
  maxBuffers: 65_536,
  maxInputBlobBytes: 576 * 1_024 * 1_024,
  maxInputTotalBytes: 576 * 1_024 * 1_024,
  maxOutputBlobBytes: 512 * 1_024 * 1_024,
  maxOutputTotalBytes: 512 * 1_024 * 1_024,
  maxResponsePublishBytes: 520 * 1_024 * 1_024,
});

function deferred<T>(): Deferred<T> {
  let settled = false;
  let resolvePromise!: (value: T) => void;
  let rejectPromise!: (reason: unknown) => void;
  const promise = new Promise<T>((resolve, reject) => {
    resolvePromise = resolve;
    rejectPromise = reject;
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
    reject(reason: unknown): void {
      if (settled) return;
      settled = true;
      rejectPromise(reason);
    },
  };
}

function transportFault(
  code: "SPAWN_FAILED" | "BROKEN_PIPE" | "PROCESS_EXITED",
  retryable = true,
): InternalTransportFault {
  return new InternalTransportFault({
    kind: "transport",
    code,
    retryable,
  });
}

function frameFault(): InternalTransportFault {
  return new InternalTransportFault({
    kind: "decode",
    code: "MALFORMED_FRAME",
    retryable: false,
  });
}

function compareBytes(left: Uint8Array, right: Uint8Array): number {
  const length = Math.min(left.byteLength, right.byteLength);
  for (let index = 0; index < length; index++) {
    const difference = left[index]! - right[index]!;
    if (difference !== 0) return difference;
  }
  return left.byteLength - right.byteLength;
}

function writeBytes(stream: Writable, bytes: Uint8Array): Promise<void> {
  return new Promise((resolve, reject) => {
    try {
      stream.write(bytes, (error?: Error | null) => {
        if (error === undefined || error === null) resolve();
        else reject(new Error("stdio write failed"));
      });
    } catch {
      reject(new Error("stdio write failed"));
    }
  });
}

class StdioEngineTransport implements InternalByteTransport {
  readonly ready: Promise<InternalTransportLimits>;
  readonly closed: Promise<Readonly<{
    code: "PROCESS_EXITED" | "BROKEN_PIPE";
    retryable: boolean;
  }>>;

  private readonly readyDeferred = deferred<InternalTransportLimits>();
  private readonly closedDeferred = deferred<Readonly<{
    code: "PROCESS_EXITED" | "BROKEN_PIPE";
    retryable: boolean;
  }>>();
  private readonly decoder = new StdioFrameDecoder();
  private readonly child: ChildProcess;
  private readonly stdin: Writable;
  private readonly stdout: Readable;
  private active: ActiveExchange | undefined;
  private nextStreamId = 1n;
  private writeTail = Promise.resolve();
  private disposing = false;
  private terminated = false;

  constructor(options: StdioProcessOptions) {
    this.ready = this.readyDeferred.promise;
    this.closed = this.closedDeferred.promise;
    this.child = spawn(options.binaryPath, [...options.binaryArguments], {
      ...(options.cwd === undefined ? {} : { cwd: options.cwd }),
      shell: false,
      stdio: ["pipe", "pipe", "ignore"],
      windowsHide: true,
    });
    if (this.child.stdin === null || this.child.stdout === null) {
      throw transportFault("SPAWN_FAILED");
    }
    this.stdin = this.child.stdin;
    this.stdout = this.child.stdout;
    this.installLifecycle();
  }

  request(input: Readonly<{
    exchangeId: string;
    control: ArrayBuffer;
    blobs: readonly InternalTransportBlob[];
  }>) {
    if (this.disposing || this.terminated || this.closedDeferred.settled || this.active !== undefined) {
      throw transportFault("BROKEN_PIPE");
    }
    const streamId = this.nextStreamId;
    if (streamId > 0xffff_ffff_ffff_ffffn) throw transportFault("BROKEN_PIPE", false);
    this.nextStreamId++;
    const response = deferred<InternalTransportResponse>();
    const exchange: ActiveExchange = {
      streamId,
      inputBlobs: input.blobs,
      response,
      phase: "awaiting_ready",
      cancelRequested: false,
      nextSequence: 1,
      outputBlobs: [],
      outputBytes: 0,
      streamError: false,
    };
    this.active = exchange;
    void this.enqueueFrame({
      kind: stdioKind.requestControl,
      flags: 0,
      streamId,
      sequence: 0,
      name: new Uint8Array(0),
      payload: new Uint8Array(input.control),
      offset: 0n,
    }).catch(() => this.fail(transportFault("BROKEN_PIPE")));
    return Object.freeze({
      response: response.promise,
      cancel: (): Promise<Readonly<{ reusable: boolean }>> => this.cancel(exchange),
    });
  }

  terminate(): void {
    if (this.terminated) return;
    this.terminated = true;
    this.rejectActive(transportFault("BROKEN_PIPE"));
    try {
      this.child.kill("SIGKILL");
    } catch {
      // Process errors are intentionally redacted.
    }
  }

  async dispose(): Promise<void> {
    if (this.closedDeferred.settled) return;
    if (!this.disposing) {
      this.disposing = true;
      try {
        await this.enqueueFrame({
          kind: stdioKind.close,
          flags: 0,
          streamId: 0n,
          sequence: 0,
          name: new Uint8Array(0),
          payload: new Uint8Array(0),
          offset: 0n,
        });
      } catch {
        this.terminate();
      }
    }
    await this.closed;
  }

  private installLifecycle(): void {
    this.child.once("spawn", () => this.readyDeferred.resolve(stdioLimits));
    this.child.once("error", () => {
      if (!this.readyDeferred.settled) this.readyDeferred.reject(transportFault("SPAWN_FAILED"));
      this.fail(transportFault("PROCESS_EXITED"));
    });
    this.child.once("exit", () => {
      this.terminated = true;
      if (!this.readyDeferred.settled) this.readyDeferred.reject(transportFault("SPAWN_FAILED"));
      this.rejectActive(transportFault("PROCESS_EXITED"));
    });
    this.child.once("close", () => {
      this.terminated = true;
      if (!this.readyDeferred.settled) this.readyDeferred.reject(transportFault("SPAWN_FAILED"));
      this.rejectActive(transportFault("PROCESS_EXITED"));
      this.closedDeferred.resolve(Object.freeze({
        code: "PROCESS_EXITED",
        retryable: !this.disposing,
      }));
    });
    this.stdin.on("error", () => this.fail(transportFault("BROKEN_PIPE")));
    this.stdout.on("error", () => this.fail(transportFault("BROKEN_PIPE")));
    this.stdout.on("data", (chunk: Uint8Array) => {
      try {
        for (const frame of this.decoder.push(chunk)) this.acceptFrame(frame);
      } catch {
        this.fail(frameFault());
      }
    });
    this.stdout.once("end", () => {
      try {
        this.decoder.finish();
      } catch {
        this.fail(frameFault());
        return;
      }
      if (!this.disposing && !this.terminated && !this.closedDeferred.settled) {
        this.fail(transportFault("BROKEN_PIPE"));
      }
    });
  }

  private enqueueFrame(frame: StdioFrame): Promise<void> {
    if (this.terminated || this.closedDeferred.settled) {
      return Promise.reject(transportFault("BROKEN_PIPE"));
    }
    const encoded = encodeStdioFrame(frame);
    const operation = this.writeTail.then(() => writeBytes(this.stdin, encoded));
    this.writeTail = operation.then(
      () => undefined,
      () => undefined,
    );
    return operation;
  }

  private acceptFrame(frame: StdioFrame): void {
    const active = this.active;
    if (active === undefined || frame.streamId !== active.streamId) {
      throw new StdioFrameError();
    }
    switch (frame.kind) {
      case stdioKind.requestReady:
        if (active.phase !== "awaiting_ready") throw new StdioFrameError();
        active.phase = "uploading";
        void this.upload(active).catch(() => this.fail(transportFault("BROKEN_PIPE")));
        return;
      case stdioKind.responseControl:
        this.acceptResponseControl(active, frame);
        return;
      case stdioKind.blobChunk:
        this.acceptOutputChunk(active, frame);
        return;
      case stdioKind.bundleEnd:
        this.acceptBundleEnd(active, frame);
        return;
      case stdioKind.streamError:
        if (active.control !== undefined || active.streamError) throw new StdioFrameError();
        active.streamError = true;
        active.nextSequence = 1;
        active.phase = "reading_response";
        return;
      default:
        throw new StdioFrameError();
    }
  }

  private async upload(active: ActiveExchange): Promise<void> {
    for (const blob of active.inputBlobs) {
      const name = textEncoder.encode(blob.blobId);
      if (name.byteLength === 0 || name.byteLength > stdioMaxNameBytes) {
        throw new StdioFrameError();
      }
      const bytes = new Uint8Array(blob.bytes);
      const chunks = Math.max(1, Math.ceil(bytes.byteLength / stdioMaxChunkBytes));
      for (let index = 0; index < chunks; index++) {
        if (active.cancelRequested || this.active !== active) return;
        const offset = index * stdioMaxChunkBytes;
        const end = Math.min(offset + stdioMaxChunkBytes, bytes.byteLength);
        await this.enqueueFrame({
          kind: stdioKind.blobChunk,
          flags: index + 1 === chunks ? 1 : 0,
          streamId: active.streamId,
          sequence: this.takeSequence(active),
          name,
          payload: bytes.subarray(offset, end),
          offset: BigInt(offset),
        });
      }
    }
    if (active.cancelRequested || this.active !== active) return;
    await this.enqueueFrame({
      kind: stdioKind.bundleEnd,
      flags: 0,
      streamId: active.streamId,
      sequence: this.takeSequence(active),
      name: new Uint8Array(0),
      payload: new Uint8Array(0),
      offset: 0n,
    });
    if (active.phase === "uploading") active.phase = "awaiting_response";
  }

  private takeSequence(active: ActiveExchange): number {
    const value = active.nextSequence;
    if (value <= 0 || value > 0xffff_ffff) throw new StdioFrameError();
    active.nextSequence++;
    return value;
  }

  private acceptResponseControl(active: ActiveExchange, frame: StdioFrame): void {
    if (
      active.control !== undefined || active.streamError ||
      (active.phase !== "awaiting_ready" && active.phase !== "awaiting_response" &&
        active.phase !== "cancelling" && active.phase !== "uploading")
    ) {
      throw new StdioFrameError();
    }
    active.control = new Uint8Array(frame.payload).buffer;
    active.nextSequence = 1;
    active.phase = "reading_response";
  }

  private acceptOutputChunk(active: ActiveExchange, frame: StdioFrame): void {
    if (active.phase !== "reading_response" || active.control === undefined || active.streamError) {
      throw new StdioFrameError();
    }
    if (frame.sequence !== active.nextSequence) throw new StdioFrameError();
    active.nextSequence++;
    let current = active.currentOutput;
    const newBlob = current === undefined || compareBytes(current.name, frame.name) !== 0;
    if (newBlob) {
      if (current !== undefined && (!current.final || compareBytes(current.name, frame.name) >= 0)) {
        throw new StdioFrameError();
      }
      const blobId = textDecoder.decode(frame.name);
      const created: OutputAssembly = {
        blobId,
        name: frame.name,
        chunks: [],
        size: 0,
        final: false,
      };
      current = created;
      active.currentOutput = created;
      active.outputBlobs.push(created);
    } else if (current === undefined || current.final) {
      throw new StdioFrameError();
    }
    if (current === undefined || frame.offset !== BigInt(current.size)) {
      throw new StdioFrameError();
    }
    current.chunks.push(frame.payload);
    current.size += frame.payload.byteLength;
    active.outputBytes += frame.payload.byteLength;
    if (
      current.size > stdioLimits.maxOutputBlobBytes ||
      active.outputBytes > stdioLimits.maxOutputTotalBytes ||
      active.outputBlobs.length > stdioLimits.maxBuffers
    ) throw new StdioFrameError();
    current.final = frame.flags === 1;
  }

  private acceptBundleEnd(active: ActiveExchange, frame: StdioFrame): void {
    if (
      active.phase !== "reading_response" ||
      frame.sequence !== active.nextSequence ||
      (active.currentOutput !== undefined && !active.currentOutput.final)
    ) throw new StdioFrameError();
    this.active = undefined;
    if (active.streamError || active.control === undefined) {
      active.response.reject(frameFault());
      return;
    }
    const blobs = active.outputBlobs.map((blob) => {
      const bytes = new Uint8Array(blob.size);
      let offset = 0;
      for (const chunk of blob.chunks) {
        bytes.set(chunk, offset);
        offset += chunk.byteLength;
      }
      return Object.freeze({ blobId: blob.blobId, bytes: bytes.buffer });
    });
    active.response.resolve(Object.freeze({
      control: active.control,
      blobs: Object.freeze(blobs),
    }));
  }

  private cancel(active: ActiveExchange): Promise<Readonly<{ reusable: boolean }>> {
    if (active.cancelPromise !== undefined) return active.cancelPromise;
    active.cancelRequested = true;
    active.phase = "cancelling";
    const promise = (async () => {
      try {
        await this.enqueueFrame({
          kind: stdioKind.cancel,
          flags: 0,
          streamId: active.streamId,
          sequence: 0,
          name: new Uint8Array(0),
          payload: new Uint8Array(0),
          offset: 0n,
        });
        await active.response.promise;
        return Object.freeze({ reusable: true });
      } catch {
        return Object.freeze({ reusable: false });
      }
    })();
    active.cancelPromise = promise;
    return promise;
  }

  private rejectActive(error: InternalTransportFault): void {
    const active = this.active;
    this.active = undefined;
    active?.response.reject(error);
  }

  private fail(error: InternalTransportFault): void {
    if (!this.readyDeferred.settled) this.readyDeferred.reject(error);
    this.rejectActive(error);
    if (!this.terminated) {
      this.terminated = true;
      try {
        this.child.kill("SIGKILL");
      } catch {
        // Process errors are intentionally redacted.
      }
    }
  }
}

function stdioTransportFactory(options: StdioProcessOptions): InternalTransportFactory {
  return Object.freeze({
    transportId: "stdio",
    connectFailureCode: "SPAWN_FAILED",
    create(): InternalByteTransport {
      return new StdioEngineTransport(options);
    },
  });
}

function validProcessString(value: unknown): value is string {
  return typeof value === "string" && value.length > 0 && !value.includes("\0");
}

export async function createStdioEngineClient(
  options: CreateStdioEngineClientOptions,
): Promise<EngineClient> {
  const value = dataObject(
    options,
    ["client", "binaryPath"],
    ["binaryArguments", "cwd"],
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
  const argumentsValue = value?.binaryArguments ?? ["stdio"];
  if (
    value === undefined || client === undefined ||
    !validProcessString(value.binaryPath) ||
    (value.cwd !== undefined && !validProcessString(value.cwd)) ||
    !strictArray(argumentsValue) ||
    argumentsValue.some((argument) => !validProcessString(argument))
  ) {
    throw new EngineClientInputError("INVALID_ARGUMENT");
  }
  return createInternalEngineClient({
    transportFactory: stdioTransportFactory({
      binaryPath: value.binaryPath,
      binaryArguments: argumentsValue as readonly string[],
      ...(value.cwd === undefined ? {} : { cwd: value.cwd }),
    }),
    protocolCollectors: protocolBlobRefCollectors,
    options: value.client as EngineClientCreationOptions,
  });
}
