// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

export { serializeExport } from "./core.js";
export type { RasterizerImplementation, RasterizerRequest, RasterizerResult, SerializerInput } from "./types.js";
import type { RasterizerImplementation } from "./types.js";

/** Brands and validates an explicitly supplied browser rasterizer boundary. */
export function browserRasterizer<T extends RasterizerImplementation>(implementation: T): T {
  if (implementation.environment !== "browser") throw new TypeError("browser rasterizer environment required");
  return implementation;
}
