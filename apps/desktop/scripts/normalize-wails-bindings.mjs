// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import { readFile, writeFile } from "node:fs/promises";

const header = "// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0\n\n";
const generated = [
  "frontend/wailsjs/go/desktopwails/FrontendBridge.d.ts",
  "frontend/wailsjs/go/desktopwails/FrontendBridge.js",
  "frontend/wailsjs/go/desktopwails/ShellBinding.d.ts",
  "frontend/wailsjs/go/desktopwails/ShellBinding.js",
  "frontend/wailsjs/go/models.ts",
  "frontend/wailsjs/runtime/runtime.d.ts",
  "frontend/wailsjs/runtime/runtime.js",
];

for (const path of generated) {
  const url = new URL(`../${path}`, import.meta.url);
  const source = await readFile(url, "utf8");
	const headed = source.startsWith(header) ? source : `${header}${source}`;
	const normalized = path.endsWith(".d.ts")
		? headed.replaceAll("from '../models';", "from '../models.js';")
			.replaceAll("desktopapp.MCPTransportKind", "string")
			.replaceAll("desktopcontract.LifecycleState", "string")
		: headed;
	await writeFile(url, `${normalized.trimEnd()}\n`);
}
