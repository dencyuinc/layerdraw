// SPDX-License-Identifier: Apache-2.0

export type InternalSafeDetails = Readonly<
  Record<string, string | number | boolean>
>;

export type InternalSafeDetailsSnapshot =
  | Readonly<{ valid: false }>
  | Readonly<{ valid: true; details?: InternalSafeDetails }>;

const ALLOWED_DETAIL_KEYS = new Set([
  "outcome",
  "diagnosticCount",
  "failureCode",
  "requestId",
  "generation",
  "blobCount",
  "limit",
  "exitCode",
  "signal",
  "replacementSucceeded",
]);

const INVALID_DETAILS = Object.freeze({ valid: false as const });
const ABSENT_DETAILS = Object.freeze({ valid: true as const });

/** Snapshots safe scalar details without invoking caller-controlled accessors. */
export function snapshotSafeDetails(
  value: unknown,
): InternalSafeDetailsSnapshot {
  if (value === undefined) return ABSENT_DETAILS;
  try {
    if (typeof value !== "object" || value === null || Array.isArray(value)) {
      return INVALID_DETAILS;
    }
    const prototype = Object.getPrototypeOf(value);
    if (prototype !== Object.prototype && prototype !== null) {
      return INVALID_DETAILS;
    }
    const keys = Reflect.ownKeys(value);
    const copy: Record<string, string | number | boolean> = Object.create(null);
    for (const key of keys) {
      if (typeof key !== "string") return INVALID_DETAILS;
      const descriptor = Object.getOwnPropertyDescriptor(value, key);
      if (
        descriptor === undefined ||
        !descriptor.enumerable ||
        !("value" in descriptor)
      ) {
        return INVALID_DETAILS;
      }
      const detail = descriptor.value;
      if (
        ALLOWED_DETAIL_KEYS.has(key) &&
        /^[A-Za-z][A-Za-z0-9]*$/.test(key) &&
        (typeof detail === "string" ||
          typeof detail === "boolean" ||
          (typeof detail === "number" && Number.isFinite(detail)))
      ) {
        copy[key] = detail;
      }
    }
    return Object.freeze({
      valid: true as const,
      details: Object.freeze(copy),
    });
  } catch {
    return INVALID_DETAILS;
  }
}
