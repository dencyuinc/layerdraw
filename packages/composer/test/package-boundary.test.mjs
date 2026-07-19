// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("Composer is framework-neutral and depends only on generated protocol contracts", async () => {
  const manifest = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8"));
  assert.deepEqual(Object.keys(manifest.exports), ["."]);
  assert.deepEqual(Object.keys(manifest.dependencies), ["@layerdraw/protocol"]);
  const source = await readFile(new URL("../src/index.ts", import.meta.url), "utf8");
  assert.doesNotMatch(source, /from\s+["'](?:react|node:|@layerdraw\/engine-client)/);
  assert.doesNotMatch(source, /\b(?:document|window|localStorage|sessionStorage)\b/);
  assert.doesNotMatch(source, /parseLDL|rewriteSource|classifyImpact|evaluatePolicy/);
});
