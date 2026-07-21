// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Generates `src/brand-assets.ts` from the canonical assets in `brand/`.
//
// This is the ONLY writer for the embedded brand asset data URIs used by the
// Desktop chrome. It follows the same committed-generated-output policy as the
// token CSS: run via the package `generate` script and covered by the
// repository reproducibility gate. Never hand-edit the generated file.

import { readFileSync, writeFileSync } from "node:fs";

const ICON_PATH = new URL("../../../brand/png/layerdraw-icon-128.png", import.meta.url);
const OUT_PATH = new URL("../src/brand-assets.ts", import.meta.url);

function dataUri(url, mime) {
  return `data:${mime};base64,${readFileSync(url).toString("base64")}`;
}

const source = `// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// GENERATED FROM brand/ BY scripts/generate-brand-assets.mjs. Do not edit by
// hand. Run \`pnpm --filter @layerdraw/desktop generate\` to update.

/** Canonical LayerDraw app icon (brand/png/layerdraw-icon-128.png). */
export const layerdrawIconDataUri =
  "${dataUri(ICON_PATH, "image/png")}";
`;

writeFileSync(OUT_PATH, source, "utf8");
process.stdout.write("brand-assets.ts written from brand/\n");
