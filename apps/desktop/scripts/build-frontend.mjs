// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import { mkdir, readFile, writeFile } from "node:fs/promises";
import { build } from "esbuild-wasm";

await mkdir(new URL("../frontend/dist/", import.meta.url), { recursive: true });
await build({
  entryPoints: [new URL("../frontend/src/main.ts", import.meta.url).pathname],
  bundle: true,
  format: "iife",
  platform: "browser",
  target: ["chrome120"],
  sourcemap: true,
  outfile: new URL("../frontend/dist/app.js", import.meta.url).pathname,
});
const desktop = await readFile(new URL("../src/styles.css", import.meta.url), "utf8");
const editor = await readFile(new URL("../../../packages/react/src/styles.css", import.meta.url), "utf8");
await writeFile(new URL("../frontend/dist/app.css", import.meta.url), `${editor}\n${desktop}`, "utf8");
