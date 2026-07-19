// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { readFile, readdir } from "node:fs/promises";
import test from "node:test";

test("package exposes only the framework-neutral root and has the documented dependency closure", async () => {
  const manifest = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8"));
  assert.deepEqual(Object.keys(manifest.exports), ["."]);
  assert.deepEqual(Object.keys(manifest.dependencies).sort(), ["@layerdraw/protocol", "@layerdraw/render"]);
  for (const forbidden of ["react", "node:", "engine", "runtime", "ldl", "layerdraw container"])
    assert.equal(Object.keys(manifest.dependencies).some((name) => name.toLowerCase().includes(forbidden)), false);
});

test("runtime source has no framework, DOM, Node-only, Engine, Runtime, LDL, or container dependency", async () => {
  const files = (await readdir(new URL("../src", import.meta.url))).filter((name) => name.endsWith(".ts"));
  const source = (await Promise.all(files.map((name) => readFile(new URL(`../src/${name}`, import.meta.url), "utf8")))).join("\n");
  assert.doesNotMatch(source, /from\s+["'](?:node:|react|@layerdraw\/(?:engine|runtime))/i);
  assert.doesNotMatch(source, /\b(?:document|window|localStorage|sessionStorage)\b/);
  assert.doesNotMatch(source, /\.ldl|\.layerdraw|ViewRecipe|ExportPlan|DocumentStore|EngineClient/);
});

test("built package contains declarations, ESM, license, and no undeclared public subpath", async () => {
  const declaration = await readFile(new URL("../dist/index.d.ts", import.meta.url), "utf8");
  const implementation = await readFile(new URL("../dist/index.js", import.meta.url), "utf8");
  assert.match(declaration, /export interface Viewer/);
  assert.match(declaration, /export declare function createViewer/);
  assert.doesNotMatch(implementation, /require\(|node:/);
  assert.ok((await readFile(new URL("../LICENSE", import.meta.url), "utf8")).length > 0);
});
