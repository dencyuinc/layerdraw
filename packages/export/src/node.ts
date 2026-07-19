// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

export { serializeExport } from "./core.js";
export type { RasterizerImplementation, RasterizerRequest, RasterizerResult, SerializerInput } from "./types.js";
import type { RasterizerImplementation } from "./types.js";

/** Brands and validates an explicitly supplied Node rasterizer boundary. */
export function nodeRasterizer<T extends RasterizerImplementation>(implementation: T): T {
  if (implementation.environment !== "node") throw new TypeError("Node rasterizer environment required");
  return implementation;
}
