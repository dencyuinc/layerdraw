// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import { cp, mkdir, readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { build } from "esbuild-wasm";

await mkdir(new URL("../frontend/dist/", import.meta.url), { recursive: true });
// Bundled brand typefaces (OFL): Inter for Latin, Noto Sans JP for Japanese —
// the canonical families named by brand/tokens.json, shipped so rendering does
// not depend on host-installed fonts.
await cp(new URL("../frontend/src-static/fonts/", import.meta.url), new URL("../frontend/dist/fonts/", import.meta.url), { recursive: true });
await build({
  entryPoints: [fileURLToPath(new URL("../frontend/src/main.ts", import.meta.url))],
  bundle: true,
  format: "iife",
  platform: "browser",
  jsx: "automatic",
  target: ["chrome120"],
  sourcemap: true,
  outfile: fileURLToPath(new URL("../frontend/dist/app.js", import.meta.url)),
});
// Token custom properties are generated from brand/tokens.json and must come
// first so every downstream rule resolves against them.
const tokens = await readFile(new URL("../../../packages/react/src/tokens.css", import.meta.url), "utf8");
const primitives = await readFile(new URL("../../../packages/react/dist/primitives.css", import.meta.url), "utf8");
const desktop = await readFile(new URL("../src/styles.css", import.meta.url), "utf8");
const editor = await readFile(new URL("../../../packages/react/src/styles.css", import.meta.url), "utf8");
await writeFile(new URL("../frontend/dist/app.css", import.meta.url), `${tokens}\n${primitives}\n${editor}\n${desktop}`, "utf8");
