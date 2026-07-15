// SPDX-License-Identifier: Apache-2.0

import type { BlobRef } from "@layerdraw/protocol/common";
import { isBlobRef } from "@layerdraw/protocol/common";
import {
  EngineClientInputError,
  type EngineClientSafeDetails,
} from "../index.js";

const arrayBufferByteLength = Object.getOwnPropertyDescriptor(
  ArrayBuffer.prototype,
  "byteLength",
)?.get;
const arrayBufferResizable = Object.getOwnPropertyDescriptor(
  ArrayBuffer.prototype,
  "resizable",
)?.get;
const typedArrayPrototype = Object.getPrototypeOf(Uint8Array.prototype) as object;
const typedArrayByteLength = Object.getOwnPropertyDescriptor(
  typedArrayPrototype,
  "byteLength",
)?.get;
const typedArrayByteOffset = Object.getOwnPropertyDescriptor(
  typedArrayPrototype,
  "byteOffset",
)?.get;
const typedArrayBuffer = Object.getOwnPropertyDescriptor(
  typedArrayPrototype,
  "buffer",
)?.get;

export function strictArray(value: unknown): value is readonly unknown[] {
  try {
    if (!Array.isArray(value) || Object.getPrototypeOf(value) !== Array.prototype) {
      return false;
    }
    const descriptors = Object.getOwnPropertyDescriptors(value);
    const keys = Reflect.ownKeys(value);
    if (
      keys.some((key) => typeof key !== "string") ||
      keys.length !== value.length + 1
    ) {
      return false;
    }
    for (let index = 0; index < value.length; index++) {
      const descriptor = descriptors[String(index)];
      if (
        descriptor === undefined ||
        !descriptor.enumerable ||
        !("value" in descriptor)
      ) {
        return false;
      }
    }
    return Object.keys(value).length === value.length;
  } catch {
    return false;
  }
}

export function dataObject(
  value: unknown,
  required: readonly string[],
  optional: readonly string[] = [],
): Record<string, unknown> | undefined {
  try {
    if (
      typeof value !== "object" ||
      value === null ||
      Array.isArray(value)
    ) {
      return undefined;
    }
    const prototype = Object.getPrototypeOf(value);
    if (prototype !== Object.prototype && prototype !== null) {
      return undefined;
    }
    const descriptors = Object.getOwnPropertyDescriptors(value);
    const allowed = new Set([...required, ...optional]);
    for (const key of Reflect.ownKeys(value)) {
      if (typeof key !== "string" || !allowed.has(key)) return undefined;
      const descriptor = descriptors[key];
      if (
        descriptor === undefined ||
        !descriptor.enumerable ||
        !("value" in descriptor)
      ) {
        return undefined;
      }
    }
    if (required.some((key) => descriptors[key] === undefined)) return undefined;
    return Object.fromEntries(
      Object.entries(descriptors).map(([key, descriptor]) => [
        key,
        "value" in descriptor ? descriptor.value : undefined,
      ]),
    );
  } catch {
    return undefined;
  }
}

export function fixedArrayBufferByteLength(value: unknown): number | undefined {
  if (arrayBufferByteLength === undefined) return undefined;
  try {
    const length = arrayBufferByteLength.call(value) as number;
    if (arrayBufferResizable !== undefined) {
      const resizable = arrayBufferResizable.call(value) as boolean;
      if (resizable) return undefined;
    }
    // A zero-length, non-detached buffer constructs a view successfully.
    new Uint8Array(value as ArrayBuffer);
    return length;
  } catch {
    return undefined;
  }
}

export interface Uint8ViewSnapshotSource {
  readonly buffer: ArrayBuffer;
  readonly byteOffset: number;
  readonly byteLength: number;
}

export function uint8ViewSource(
  value: unknown,
): Uint8ViewSnapshotSource | undefined {
  if (
    typedArrayByteLength === undefined ||
    typedArrayByteOffset === undefined ||
    typedArrayBuffer === undefined ||
    !ArrayBuffer.isView(value)
  ) {
    return undefined;
  }
  try {
    if (Object.prototype.toString.call(value) !== "[object Uint8Array]") {
      return undefined;
    }
    const buffer = typedArrayBuffer.call(value) as ArrayBuffer;
    if (fixedArrayBufferByteLength(buffer) === undefined) return undefined;
    return {
      buffer,
      byteOffset: typedArrayByteOffset.call(value) as number,
      byteLength: typedArrayByteLength.call(value) as number,
    };
  } catch {
    return undefined;
  }
}

export function utf8ByteLength(value: string): number | undefined {
  let bytes = 0;
  for (let index = 0; index < value.length; index++) {
    const code = value.charCodeAt(index);
    if (code <= 0x7f) bytes++;
    else if (code <= 0x7ff) bytes += 2;
    else if (code >= 0xd800 && code <= 0xdbff) {
      const low = value.charCodeAt(index + 1);
      if (!(low >= 0xdc00 && low <= 0xdfff)) return undefined;
      bytes += 4;
      index++;
    } else if (code >= 0xdc00 && code <= 0xdfff) {
      return undefined;
    } else {
      bytes += 3;
    }
  }
  return bytes;
}

export function compareUtf8(left: string, right: string): number {
  const leftBytes = new TextEncoder().encode(left);
  const rightBytes = new TextEncoder().encode(right);
  const shared = Math.min(leftBytes.byteLength, rightBytes.byteLength);
  for (let index = 0; index < shared; index++) {
    const difference = leftBytes[index]! - rightBytes[index]!;
    if (difference !== 0) return difference;
  }
  return leftBytes.byteLength - rightBytes.byteLength;
}

export function validRequestId(value: unknown): value is string {
  if (typeof value !== "string") return false;
  const bytes = utf8ByteLength(value);
  if (bytes === undefined || bytes < 1 || bytes > 512) return false;
  const points = Array.from(value).length;
  return points >= 1 && points <= 128;
}

export function blobRefFingerprint(ref: BlobRef): string {
  return [ref.blob_id, ref.digest, ref.lifetime, ref.media_type, ref.size].join(
    "\u0000",
  );
}

export function validateCollectedRefs(
  refs: readonly BlobRef[],
  requireRequestLifetime: boolean,
): readonly BlobRef[] {
  if (!strictArray(refs)) {
    throw new EngineClientInputError("INVALID_BLOB_TABLE");
  }
  for (const ref of refs) {
    if (!isBlobRef(ref) || (requireRequestLifetime && ref.lifetime !== "request")) {
      throw new EngineClientInputError("INVALID_BLOB_TABLE");
    }
  }
  return refs;
}

export function safeCountDetails(
  name: string,
  value: number,
  limit?: number,
): EngineClientSafeDetails {
  const details: Record<string, number | string | boolean> = { [name]: value };
  if (limit !== undefined) details.limit = limit;
  return details;
}

export function deepFreeze<T>(value: T): T {
  if (
    typeof value !== "object" ||
    value === null ||
    ArrayBuffer.isView(value) ||
    fixedArrayBufferByteLength(value) !== undefined ||
    Object.isFrozen(value)
  ) {
    return value;
  }
  const descriptors = Object.getOwnPropertyDescriptors(value);
  for (const descriptor of Object.values(descriptors)) {
    if ("value" in descriptor) deepFreeze(descriptor.value);
  }
  return Object.freeze(value);
}

export function bytesToHex(bytes: Uint8Array): string {
  let result = "";
  for (const byte of bytes) result += byte.toString(16).padStart(2, "0");
  return result;
}
