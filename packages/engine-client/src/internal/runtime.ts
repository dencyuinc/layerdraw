// SPDX-License-Identifier: Apache-2.0

import { EngineClientUnsupportedEnvironmentError } from "../index.js";

export interface InternalTimerHandle {}

/**
 * A digest lease borrows its input view until `joined` resolves.
 *
 * `abort()` is an idempotent, non-throwing hard stop, not an AbortSignal hint.
 * Before it returns, `result` and `joined` must be settled and no execution path
 * may still access the borrowed view. `joined` never rejects and, on every
 * terminal path, resolves only after `result` is terminal and the view has been
 * released. A fulfilled result is exactly 32 SHA-256 bytes. Adapters may back
 * this lease with a dedicated Worker or another hard-terminable resource.
 */
export interface InternalDigestLease {
  readonly result: Promise<Uint8Array>;
  readonly joined: Promise<void>;
  abort(): void;
}

export interface InternalClientRuntime {
  now(): number;
  randomBytes(length: number): Uint8Array;
  /** Starts an exact SHA-256 operation with the lease contract above. */
  sha256(bytes: Uint8Array): InternalDigestLease;
  transferArrayBuffer(buffer: ArrayBuffer): ArrayBuffer;
  setTimer(callback: () => void, delayMs: number): InternalTimerHandle;
  clearTimer(handle: InternalTimerHandle): void;
}

interface StructuralCrypto {
  getRandomValues<T extends Uint8Array>(value: T): T;
}

interface StructuralGlobal {
  crypto?: StructuralCrypto;
  structuredClone?: (
    value: ArrayBuffer,
    options: Readonly<{ transfer: readonly ArrayBuffer[] }>,
  ) => ArrayBuffer;
  setTimeout(callback: () => void, delayMs: number): unknown;
  clearTimeout(handle: unknown): void;
}

class DigestAborted extends Error {}

const SHA256_INITIAL = Object.freeze([
  0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
  0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
]);

const SHA256_CONSTANTS = Object.freeze([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5,
  0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
  0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
  0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
  0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
  0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3,
  0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5,
  0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
  0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
]);

const BLOCK_BYTES = 64;
const BLOCKS_PER_TURN = 256;

function rotateRight(value: number, count: number): number {
  return ((value >>> count) | (value << (32 - count))) >>> 0;
}

function startDefaultSha256(
  bytes: Uint8Array,
  host: StructuralGlobal,
): InternalDigestLease {
  let source: Uint8Array | undefined = bytes;
  let timer: unknown;
  let terminal = false;
  let offset = 0;
  const state = Uint32Array.from(SHA256_INITIAL);
  const words = new Uint32Array(64);
  const sourceLength = bytes.byteLength;
  const paddedLength = Math.ceil((sourceLength + 9) / BLOCK_BYTES) * BLOCK_BYTES;
  const bitLength = BigInt(sourceLength) * 8n;
  bytes = new Uint8Array(0);
  let resolveResult!: (value: Uint8Array) => void;
  let rejectResult!: (reason: unknown) => void;
  let resolveJoined!: () => void;
  const result = new Promise<Uint8Array>((resolve, reject) => {
    resolveResult = resolve;
    rejectResult = reject;
  });
  const joined = new Promise<void>((resolve) => {
    resolveJoined = resolve;
  });

  const finish = (digest?: Uint8Array, error?: unknown): void => {
    if (terminal) return;
    terminal = true;
    if (timer !== undefined) {
      host.clearTimeout(timer);
      timer = undefined;
    }
    source = undefined;
    if (digest === undefined) rejectResult(error);
    else resolveResult(digest);
    resolveJoined();
  };

  const byteAt = (input: Uint8Array, index: number): number => {
    if (index < sourceLength) return input[index]!;
    if (index === sourceLength) return 0x80;
    if (index >= paddedLength - 8) {
      const shift = BigInt((paddedLength - 1 - index) * 8);
      return Number((bitLength >> shift) & 0xffn);
    }
    return 0;
  };

  const compress = (input: Uint8Array, blockOffset: number): void => {
    for (let index = 0; index < 16; index++) {
      const position = blockOffset + index * 4;
      words[index] = (
        (byteAt(input, position) << 24) |
        (byteAt(input, position + 1) << 16) |
        (byteAt(input, position + 2) << 8) |
        byteAt(input, position + 3)
      ) >>> 0;
    }
    for (let index = 16; index < 64; index++) {
      const prior15 = words[index - 15]!;
      const prior2 = words[index - 2]!;
      const sigma0 =
        rotateRight(prior15, 7) ^ rotateRight(prior15, 18) ^ (prior15 >>> 3);
      const sigma1 =
        rotateRight(prior2, 17) ^ rotateRight(prior2, 19) ^ (prior2 >>> 10);
      words[index] = (
        words[index - 16]! + sigma0 + words[index - 7]! + sigma1
      ) >>> 0;
    }

    let a = state[0]!;
    let b = state[1]!;
    let c = state[2]!;
    let d = state[3]!;
    let e = state[4]!;
    let f = state[5]!;
    let g = state[6]!;
    let h = state[7]!;
    for (let index = 0; index < 64; index++) {
      const sum1 = rotateRight(e, 6) ^ rotateRight(e, 11) ^ rotateRight(e, 25);
      const choose = (e & f) ^ (~e & g);
      const temporary1 = (
        h + sum1 + choose + SHA256_CONSTANTS[index]! + words[index]!
      ) >>> 0;
      const sum0 = rotateRight(a, 2) ^ rotateRight(a, 13) ^ rotateRight(a, 22);
      const majority = (a & b) ^ (a & c) ^ (b & c);
      const temporary2 = (sum0 + majority) >>> 0;
      h = g;
      g = f;
      f = e;
      e = (d + temporary1) >>> 0;
      d = c;
      c = b;
      b = a;
      a = (temporary1 + temporary2) >>> 0;
    }
    state[0] = (state[0]! + a) >>> 0;
    state[1] = (state[1]! + b) >>> 0;
    state[2] = (state[2]! + c) >>> 0;
    state[3] = (state[3]! + d) >>> 0;
    state[4] = (state[4]! + e) >>> 0;
    state[5] = (state[5]! + f) >>> 0;
    state[6] = (state[6]! + g) >>> 0;
    state[7] = (state[7]! + h) >>> 0;
  };

  const step = (): void => {
    timer = undefined;
    let input = source;
    if (terminal || input === undefined) return;
    try {
      const stop = Math.min(
        paddedLength,
        offset + BLOCKS_PER_TURN * BLOCK_BYTES,
      );
      while (offset < stop) {
        compress(input, offset);
        offset += BLOCK_BYTES;
      }
      if (offset < paddedLength) {
        timer = host.setTimeout(step, 0);
        return;
      }
      const digest = new Uint8Array(32);
      for (let index = 0; index < state.length; index++) {
        const value = state[index]!;
        const position = index * 4;
        digest[position] = value >>> 24;
        digest[position + 1] = value >>> 16;
        digest[position + 2] = value >>> 8;
        digest[position + 3] = value;
      }
      input = undefined;
      finish(digest);
    } catch (error) {
      input = undefined;
      finish(undefined, error);
    }
  };

  if (!Number.isSafeInteger(paddedLength) || paddedLength < BLOCK_BYTES) {
    finish(undefined, new Error("Invalid digest input length"));
  } else {
    try {
      timer = host.setTimeout(step, 0);
    } catch (error) {
      finish(undefined, error);
    }
  }

  return Object.freeze({
    result,
    joined,
    abort: (): void => {
      finish(undefined, new DigestAborted());
    },
  });
}

export function defaultRuntime(): InternalClientRuntime {
  const host = globalThis as unknown as StructuralGlobal;
  const crypto = host.crypto;
  const clone = host.structuredClone;
  if (
    crypto === undefined ||
    typeof crypto.getRandomValues !== "function" ||
    typeof clone !== "function" ||
    typeof host.setTimeout !== "function" ||
    typeof host.clearTimeout !== "function"
  ) {
    throw new EngineClientUnsupportedEnvironmentError();
  }

  return Object.freeze({
    now: (): number => Date.now(),
    randomBytes: (length: number): Uint8Array =>
      crypto.getRandomValues(new Uint8Array(length)),
    sha256: (bytes: Uint8Array): InternalDigestLease =>
      startDefaultSha256(bytes, host),
    transferArrayBuffer: (buffer: ArrayBuffer): ArrayBuffer =>
      clone(buffer, { transfer: [buffer] }),
    setTimer: (callback: () => void, delayMs: number): InternalTimerHandle =>
      host.setTimeout(callback, delayMs) as InternalTimerHandle,
    clearTimer: (handle: InternalTimerHandle): void => {
      host.clearTimeout(handle);
    },
  });
}
