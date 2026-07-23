// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Generates `src/tokens.css` from the canonical `brand/tokens.json`.
//
// This is the ONLY writer for the token CSS custom properties consumed by the
// `@layerdraw/react` presentation surfaces (and, through them, every LayerDraw
// delivery form). It is invoked by the package `generate` script, so the
// repository-wide `make generate` / `tools/check-generated.sh` reproducibility
// gate covers its output exactly like the protocol codegen. Never hand-edit the
// generated file.
//
// Standard library only. No network access.

import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const HERE = fileURLToPath(new URL(".", import.meta.url));
const TOKENS_PATH = new URL("../../../brand/tokens.json", import.meta.url);
const OUT_PATH = new URL("../src/tokens.css", import.meta.url);

/** Primitive/scale sections emitted once at :root (theme-independent). */
const ROOT_SECTIONS = [
  "color",
  "presence",
  "font",
  "space",
  "size",
  "radius",
  "borderWidth",
  "shadow",
  "motion",
  "opacity",
  "layout",
  "zIndex",
];

/** Composite sections that reference other tokens and are not emitted as vars. */
const SKIP_SECTIONS = new Set(["typography"]);

/** Map a dotted token path to its CSS custom property name. */
function pathToVar(path) {
  const segments = path.split(".");
  const scoped = segments[0] === "theme" ? segments.slice(2) : segments;
  return `--ld-${scoped.join("-")}`;
}

function quoteFontFamily(name) {
  return /\s/.test(name) ? `"${name}"` : name;
}

/** Render a leaf `$value` (and its `$type`) into a CSS declaration value. */
function renderValue(value, type) {
  if (typeof value === "string") {
    const reference = value.match(/^\{(.+)\}$/u);
    if (reference) return `var(${pathToVar(reference[1])})`;
    return value;
  }
  if (typeof value === "number") return String(value);
  if (Array.isArray(value)) {
    if (type === "cubicBezier") return `cubic-bezier(${value.join(", ")})`;
    if (type === "fontFamily") return value.map(quoteFontFamily).join(", ");
    return value.join(", ");
  }
  if (value && typeof value === "object") {
    // Shadow composite: offset-x offset-y blur spread color.
    if ("offsetX" in value) {
      return `${value.offsetX} ${value.offsetY} ${value.blur} ${value.spread} ${value.color}`;
    }
  }
  return null;
}

/** Walk a token subtree, emitting `[varName, cssValue]` pairs for each leaf. */
function collect(node, path, inheritedType, out) {
  if (node === null || typeof node !== "object") return;
  if ("$value" in node) {
    const rendered = renderValue(node.$value, node.$type ?? inheritedType);
    if (rendered !== null) out.push([pathToVar(path), rendered]);
    return;
  }
  const type = node.$type ?? inheritedType;
  for (const [key, child] of Object.entries(node)) {
    if (key.startsWith("$")) continue;
    collect(child, path ? `${path}.${key}` : key, type, out);
  }
}

function block(selector, declarations) {
  const body = declarations.map(([name, value]) => `  ${name}: ${value};`).join("\n");
  return `${selector} {\n${body}\n}`;
}

/** Render the full `tokens.css` document from a parsed tokens.json object. */
export function generateTokensCss(tokens) {
  const rootDeclarations = [];
  for (const section of ROOT_SECTIONS) {
    if (tokens[section] === undefined) continue;
    collect(tokens[section], section, tokens[section].$type, rootDeclarations);
  }

  const theme = tokens.theme ?? {};
  const lightDeclarations = [];
  collect(theme.light ?? {}, "theme.light", undefined, lightDeclarations);
  const darkDeclarations = [];
  collect(theme.dark ?? {}, "theme.dark", undefined, darkDeclarations);
  const highContrastDeclarations = [];
  collect(theme.highContrast ?? {}, "theme.highContrast", undefined, highContrastDeclarations);

  void SKIP_SECTIONS; // documented intent: typography composites are not emitted.

  const banner = [
    "/* SPDX-License-Identifier: LicenseRef-LayerDraw-1.0 */",
    "/* GENERATED FROM brand/tokens.json BY packages/react/scripts/generate-tokens-css.mjs. */",
    "/* Do not edit by hand. Run `pnpm --filter @layerdraw/react generate` to update. */",
    "/* Light theme is the default at :root; dark and high-contrast override by attribute. */",
  ].join("\n");

  return [
    banner,
    "",
    block(":root", [...rootDeclarations, ...lightDeclarations]),
    "",
    block(':root[data-theme="dark"]', darkDeclarations),
    "",
    block(':root[data-theme="high-contrast"]', highContrastDeclarations),
    "",
  ].join("\n");
}

/** Parse the canonical tokens.json from disk. */
export function readTokens() {
  return JSON.parse(readFileSync(TOKENS_PATH, "utf8"));
}

export const TOKENS_CSS_PATH = OUT_PATH;

function main() {
  const css = generateTokensCss(readTokens());
  writeFileSync(OUT_PATH, css, "utf8");
  process.stdout.write("tokens.css written from brand/tokens.json\n");
  void HERE;
  void TOKENS_PATH;
}

// Run only when invoked directly (not when imported by tests).
if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) main();
