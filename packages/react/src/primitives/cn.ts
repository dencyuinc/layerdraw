// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

/** Minimal class-name joiner for the primitives module (no runtime deps). */
export function cn(...values: readonly (string | false | undefined)[]): string {
  return values.filter((value): value is string => typeof value === "string" && value !== "").join(" ");
}
