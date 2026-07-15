// SPDX-License-Identifier: Apache-2.0

export const stdioHeaderBytes = 40;
export const stdioMaxNameBytes = 4_096;
export const stdioMaxControlBytes = 8_388_608;
export const stdioMaxChunkBytes = 1_048_576;

export const stdioKind = Object.freeze({
  requestControl: 0x01,
  requestReady: 0x02,
  blobChunk: 0x03,
  bundleEnd: 0x04,
  cancel: 0x05,
  responseControl: 0x06,
  close: 0x07,
  streamError: 0x08,
} as const);

export type StdioFrameKind = typeof stdioKind[keyof typeof stdioKind];

export interface StdioFrame {
  readonly kind: StdioFrameKind;
  readonly flags: number;
  readonly streamId: bigint;
  readonly sequence: number;
  readonly name: Uint8Array;
  readonly payload: Uint8Array;
  readonly offset: bigint;
}

export class StdioFrameError extends Error {
  constructor() {
    super("Invalid LDSP frame");
    this.name = "StdioFrameError";
  }
}

const magic = new Uint8Array([0x4c, 0x44, 0x53, 0x50]);
const utf8Decoder = new TextDecoder("utf-8", { fatal: true });

function invalid(): never {
  throw new StdioFrameError();
}

function isKind(value: number): value is StdioFrameKind {
  return value >= stdioKind.requestControl && value <= stdioKind.streamError;
}

function validUtf8(value: Uint8Array): boolean {
  try {
    utf8Decoder.decode(value);
    return true;
  } catch {
    return false;
  }
}

function validJSON(value: Uint8Array): boolean {
  try {
    JSON.parse(utf8Decoder.decode(value));
    return true;
  } catch {
    return false;
  }
}

function empty(value: Uint8Array): boolean {
  return value.byteLength === 0;
}

export function validateStdioFrame(frame: StdioFrame): void {
  if (
    !isKind(frame.kind) ||
    !Number.isSafeInteger(frame.flags) ||
    !Number.isSafeInteger(frame.sequence) ||
    frame.sequence < 0 ||
    frame.sequence > 0xffff_ffff ||
    frame.streamId < 0n ||
    frame.streamId > 0xffff_ffff_ffff_ffffn ||
    frame.offset < 0n ||
    frame.offset > 0xffff_ffff_ffff_ffffn ||
    frame.name.byteLength > stdioMaxNameBytes ||
    frame.payload.byteLength > stdioMaxControlBytes ||
    (frame.kind === stdioKind.blobChunk
      ? frame.flags !== 0 && frame.flags !== 1
      : frame.flags !== 0)
  ) {
    invalid();
  }
  const commonEmpty = frame.sequence === 0 && empty(frame.name) &&
    empty(frame.payload) && frame.offset === 0n;
  switch (frame.kind) {
    case stdioKind.requestControl:
    case stdioKind.responseControl:
      if (
        frame.streamId === 0n || frame.sequence !== 0 || !empty(frame.name) ||
        frame.offset !== 0n || empty(frame.payload) || !validJSON(frame.payload)
      ) invalid();
      return;
    case stdioKind.requestReady:
    case stdioKind.cancel:
      if (frame.streamId === 0n || !commonEmpty) invalid();
      return;
    case stdioKind.blobChunk:
      if (
        frame.streamId === 0n || frame.sequence === 0 || empty(frame.name) ||
        !validUtf8(frame.name) || frame.payload.byteLength > stdioMaxChunkBytes ||
        (frame.flags === 0 && frame.payload.byteLength !== stdioMaxChunkBytes) ||
        (frame.flags === 1 && empty(frame.payload) && frame.offset !== 0n)
      ) invalid();
      return;
    case stdioKind.bundleEnd:
      if (
        frame.streamId === 0n || frame.sequence === 0 || !empty(frame.name) ||
        !empty(frame.payload) || frame.offset !== 0n
      ) invalid();
      return;
    case stdioKind.close:
      if (frame.streamId !== 0n || !commonEmpty) invalid();
      return;
    case stdioKind.streamError:
      if (
        frame.streamId === 0n || frame.sequence !== 0 || empty(frame.name) ||
        frame.name.byteLength > 128 || !empty(frame.payload) || frame.offset !== 0n ||
        [...frame.name].some((byte) => byte > 0x7f)
      ) invalid();
  }
}

export function encodeStdioFrame(frame: StdioFrame): Uint8Array {
  validateStdioFrame(frame);
  const encoded = new Uint8Array(
    stdioHeaderBytes + frame.name.byteLength + frame.payload.byteLength,
  );
  encoded.set(magic, 0);
  encoded[4] = 1;
  encoded[5] = 0;
  encoded[6] = frame.kind;
  encoded[7] = frame.flags;
  const view = new DataView(encoded.buffer);
  view.setBigUint64(8, frame.streamId);
  view.setUint32(16, frame.sequence);
  view.setUint32(20, frame.name.byteLength);
  view.setBigUint64(24, BigInt(frame.payload.byteLength));
  view.setBigUint64(32, frame.offset);
  encoded.set(frame.name, stdioHeaderBytes);
  encoded.set(frame.payload, stdioHeaderBytes + frame.name.byteLength);
  return encoded;
}

export class StdioFrameDecoder {
  private pending = new Uint8Array(0);

  push(chunk: Uint8Array): readonly StdioFrame[] {
    if (chunk.byteLength > 0) {
      const joined = new Uint8Array(this.pending.byteLength + chunk.byteLength);
      joined.set(this.pending);
      joined.set(chunk, this.pending.byteLength);
      this.pending = joined;
    }
    const frames: StdioFrame[] = [];
    while (this.pending.byteLength >= stdioHeaderBytes) {
      const header = this.pending.subarray(0, stdioHeaderBytes);
      if (
        header[0] !== magic[0] || header[1] !== magic[1] ||
        header[2] !== magic[2] || header[3] !== magic[3] ||
        header[4] !== 1 || header[5] !== 0
      ) invalid();
      const kind = header[6];
      const flags = header[7];
      if (kind === undefined || flags === undefined || !isKind(kind)) invalid();
      const view = new DataView(header.buffer, header.byteOffset, header.byteLength);
      const nameLength = view.getUint32(20);
      const payloadLength = view.getBigUint64(24);
      if (
        nameLength > stdioMaxNameBytes ||
        payloadLength > BigInt(stdioMaxControlBytes) ||
        payloadLength > BigInt(Number.MAX_SAFE_INTEGER)
      ) invalid();
      const bodyLength = nameLength + Number(payloadLength);
      const frameLength = stdioHeaderBytes + bodyLength;
      if (this.pending.byteLength < frameLength) break;
      const nameStart = stdioHeaderBytes;
      const payloadStart = nameStart + nameLength;
      const frame: StdioFrame = Object.freeze({
        kind,
        flags,
        streamId: view.getBigUint64(8),
        sequence: view.getUint32(16),
        name: this.pending.slice(nameStart, payloadStart),
        payload: this.pending.slice(payloadStart, frameLength),
        offset: view.getBigUint64(32),
      });
      validateStdioFrame(frame);
      frames.push(frame);
      this.pending = this.pending.slice(frameLength);
    }
    return Object.freeze(frames);
  }

  finish(): void {
    if (this.pending.byteLength !== 0) invalid();
  }
}
