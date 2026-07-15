// SPDX-License-Identifier: Apache-2.0

export type InternalSafeDetails = Readonly<
  Record<string, string | number | boolean>
>;

export type InternalSafeDetailsSnapshot =
  | Readonly<{ valid: false }>
  | Readonly<{ valid: true; details?: InternalSafeDetails }>;

type SafeDetail = string | number | boolean;

const MAX_PROTOCOL_CODE_LENGTH = 128;
const MAX_SIGNAL_LENGTH = 32;
const MAX_EXIT_CODE = 0xffff_ffff;

function nonNegativeSafeInteger(value: unknown): value is number {
  return Number.isSafeInteger(value) && (value as number) >= 0;
}

function positiveSafeInteger(value: unknown): value is number {
  return Number.isSafeInteger(value) && (value as number) > 0;
}

const DETAIL_VALIDATORS: Readonly<
  Record<string, (value: unknown) => value is SafeDetail>
> = Object.freeze({
  outcome: (value): value is string =>
    value === "cancelled" ||
    value === "failed" ||
    value === "rejected" ||
    value === "success",
  diagnosticCount: nonNegativeSafeInteger,
  failureCode: (value): value is string =>
    typeof value === "string" &&
    value.length <= MAX_PROTOCOL_CODE_LENGTH &&
    /^[a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*)+$/.test(value),
  requestId: (value): value is string =>
    typeof value === "string" &&
    /^ec1-[0-9a-f]{24}-[0-9a-f]{16}$/.test(value),
  generation: positiveSafeInteger,
  blobCount: nonNegativeSafeInteger,
  limit: nonNegativeSafeInteger,
  exitCode: (value): value is number =>
    nonNegativeSafeInteger(value) && value <= MAX_EXIT_CODE,
  signal: (value): value is string =>
    typeof value === "string" &&
    value.length <= MAX_SIGNAL_LENGTH &&
    /^SIG[A-Z0-9]+$/.test(value),
  replacementSucceeded: (value): value is boolean =>
    typeof value === "boolean",
});

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
      const validator = Object.prototype.hasOwnProperty.call(
        DETAIL_VALIDATORS,
        key,
      )
        ? DETAIL_VALIDATORS[key]
        : undefined;
      const detail = descriptor.value;
      if (validator !== undefined && validator(detail)) {
        copy[key] = detail as SafeDetail;
      }
    }
    if (Object.keys(copy).length === 0) return ABSENT_DETAILS;
    return Object.freeze({
      valid: true as const,
      details: Object.freeze(copy),
    });
  } catch {
    return INVALID_DETAILS;
  }
}
