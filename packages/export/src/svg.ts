// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { ExportPlan } from "@layerdraw/protocol/semantic";
import type { RenderData, RenderBounds, RenderPoint } from "@layerdraw/render";
import type { ExportResourceLimits } from "./types.js";

const xml = (value: unknown): string => String(value)
  .replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;")
  .replaceAll('"', "&quot;").replaceAll("'", "&apos;");
const number = (value: number): string => Object.is(value, -0) ? "0" : String(value);
const rect = (bounds: RenderBounds): string =>
  `x="${number(bounds.x)}" y="${number(bounds.y)}" width="${number(bounds.width)}" height="${number(bounds.height)}"`;
const points = (value: readonly RenderPoint[]): string => value.map((point) => `${number(point.x)},${number(point.y)}`).join(" ");

function primitives(render: RenderData): readonly Readonly<Record<string, unknown>>[] {
  return Object.entries(render)
    .filter(([key, value]) => !["source_bindings", "diagnostics"].includes(key) && Array.isArray(value))
    .flatMap(([, value]) => value as readonly Readonly<Record<string, unknown>>[])
    .filter((value) => typeof value.render_key === "string");
}

function element(primitive: Readonly<Record<string, unknown>>, id: string): string {
  const bounds = primitive.bounds as RenderBounds | undefined;
  const path = primitive.points as readonly RenderPoint[] | undefined;
  const position = primitive.position as RenderPoint | undefined;
  const text = primitive.text ?? primitive.label ?? primitive.display_identity;
  if (path) return `<polyline id="${xml(id)}" points="${points(path)}" fill="none" stroke="currentColor"/>`;
  if (bounds && text !== undefined) return `<g id="${xml(id)}"><rect ${rect(bounds)} fill="none" stroke="currentColor"/><text x="${number(bounds.x + 4)}" y="${number(bounds.y + 16)}">${xml(text)}</text></g>`;
  if (bounds) return `<rect id="${xml(id)}" ${rect(bounds)} fill="none" stroke="currentColor"/>`;
  if (position) return `<circle id="${xml(id)}" cx="${number(position.x)}" cy="${number(position.y)}" r="2"/>`;
  return `<g id="${xml(id)}"/>`;
}

export function serializeSVG(plan: ExportPlan, render: RenderData, limits: ExportResourceLimits): Uint8Array {
  const all = primitives(render);
  if (all.length > limits.max_svg_primitives) throw new RangeError("SVG primitive limit");
  const representationByKey = new Map(plan.representations
    .filter((item) => item.disposition === "rendered" && item.viewdata_key !== "viewdata-root")
    .map((item) => [item.viewdata_key, item]));
  const ids = new Map<string, string>();
  const counts = new Map<string, number>();
  for (const binding of render.source_bindings) {
    if (!binding.viewdata_key) continue;
    const representation = representationByKey.get(binding.viewdata_key);
    if (!representation?.locator) continue;
    const count = counts.get(representation.locator) ?? 0;
    ids.set(binding.render_key, count === 0 ? representation.locator : `${representation.locator}--${count}`);
    counts.set(representation.locator, count + 1);
  }
  for (const representation of representationByKey.values())
    if (!representation.locator || !counts.has(representation.locator)) throw new TypeError("missing rendered locator");
  const width = dimension(plan.serializer_options.width, render.bounds.width);
  const height = dimension(plan.serializer_options.height, render.bounds.height);
  const scale = Number(plan.serializer_options.scale ?? "1");
  if (!(width > 0 && height > 0 && scale > 0)) throw new TypeError("invalid SVG dimensions");
  const body = all.map((primitive) => {
    const id = ids.get(String(primitive.render_key));
    if (!id) throw new TypeError("rendered primitive has no planned locator");
    return element(primitive, id);
  }).join("");
  const root = plan.representations.find((item) => item.viewdata_key === "viewdata-root" && item.locator)?.locator ?? "viewdata-root";
  return new TextEncoder().encode(`<svg xmlns="http://www.w3.org/2000/svg" width="${number(width * scale)}" height="${number(height * scale)}" viewBox="${number(render.bounds.x)} ${number(render.bounds.y)} ${number(width)} ${number(height)}"><g id="${xml(root)}">${body}</g></svg>\n`);
}

export function dimension(value: ExportPlan["serializer_options"]["width"], automatic: number): number {
  if (!value || value.kind === "auto") return automatic;
  return Number(value.value);
}
