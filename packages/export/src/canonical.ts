// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

const encoder = new TextEncoder();

function string(value: string): string {
  return JSON.stringify(value);
}

export function canonicalJSON(value: unknown): string {
  if (value === null) return "null";
  if (typeof value === "string") return string(value);
  if (typeof value === "boolean") return value ? "true" : "false";
  if (typeof value === "number") {
    if (!Number.isFinite(value)) throw new TypeError("non-finite number");
    return JSON.stringify(Object.is(value, -0) ? 0 : value);
  }
  if (Array.isArray(value)) return `[${value.map(canonicalJSON).join(",")}]`;
  if (typeof value === "object") {
    const object = value as Record<string, unknown>;
    const keys = Object.keys(object).sort();
    return `{${keys.map((key) => `${string(key)}:${canonicalJSON(object[key])}`).join(",")}}`;
  }
  throw new TypeError("unsupported canonical JSON value");
}

export function canonicalBytes(value: unknown, trailingLF = false): Uint8Array {
  return encoder.encode(canonicalJSON(value) + (trailingLF ? "\n" : ""));
}

export async function sha256(bytes: Uint8Array): Promise<`sha256:${string}`> {
  const owned = new Uint8Array(bytes.byteLength);
  owned.set(bytes);
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", owned));
  return `sha256:${Array.from(digest, (item) => item.toString(16).padStart(2, "0")).join("")}`;
}

export function equalBytes(left: Uint8Array, right: Uint8Array): boolean {
  if (left.byteLength !== right.byteLength) return false;
  let difference = 0;
  for (let index = 0; index < left.byteLength; index += 1)
    difference |= left[index]! ^ right[index]!;
  return difference === 0;
}
