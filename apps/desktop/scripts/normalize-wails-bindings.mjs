// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import { readFile, writeFile } from "node:fs/promises";

const header = "// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0\n\n";
const generated = [
  "frontend/wailsjs/go/desktopwails/FrontendBridge.d.ts",
  "frontend/wailsjs/go/desktopwails/FrontendBridge.js",
  "frontend/wailsjs/go/models.ts",
  "frontend/wailsjs/runtime/runtime.d.ts",
  "frontend/wailsjs/runtime/runtime.js",
];

for (const path of generated) {
  const url = new URL(`../${path}`, import.meta.url);
  const source = await readFile(url, "utf8");
  if (!source.startsWith(header)) await writeFile(url, `${header}${source}`);
}
