// SPDX-License-Identifier: Apache-2.0

import { EngineClientUnsupportedEnvironmentError } from "../index.js";

export interface InternalTimerHandle {}

export interface InternalClientRuntime {
  now(): number;
  randomBytes(length: number): Uint8Array;
  sha256(bytes: Uint8Array): Promise<Uint8Array>;
  transferArrayBuffer(buffer: ArrayBuffer): ArrayBuffer;
  setTimer(callback: () => void, delayMs: number): InternalTimerHandle;
  clearTimer(handle: InternalTimerHandle): void;
}

interface StructuralCrypto {
  getRandomValues<T extends Uint8Array>(value: T): T;
  subtle: {
    digest(algorithm: "SHA-256", data: Uint8Array): Promise<ArrayBuffer>;
  };
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

export function defaultRuntime(): InternalClientRuntime {
  const host = globalThis as unknown as StructuralGlobal;
  const crypto = host.crypto;
  const clone = host.structuredClone;
  if (
    crypto === undefined ||
    typeof crypto.getRandomValues !== "function" ||
    crypto.subtle === undefined ||
    typeof crypto.subtle.digest !== "function" ||
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
    sha256: async (bytes: Uint8Array): Promise<Uint8Array> =>
      new Uint8Array(await crypto.subtle.digest("SHA-256", bytes)),
    transferArrayBuffer: (buffer: ArrayBuffer): ArrayBuffer =>
      clone(buffer, { transfer: [buffer] }),
    setTimer: (callback: () => void, delayMs: number): InternalTimerHandle =>
      host.setTimeout(callback, delayMs) as InternalTimerHandle,
    clearTimer: (handle: InternalTimerHandle): void => {
      host.clearTimeout(handle);
    },
  });
}
