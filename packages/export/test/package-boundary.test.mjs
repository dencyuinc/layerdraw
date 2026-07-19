// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdir, mkdtemp, readFile, readdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";
import test from "node:test";

const execute = promisify(execFile);

test("package has only explicit root, browser, and Node public boundaries", async () => {
  const manifest = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8"));
  assert.deepEqual(Object.keys(manifest.exports), [".", "./browser", "./node"]);
  assert.deepEqual(Object.keys(manifest.dependencies).sort(), ["@layerdraw/protocol", "@layerdraw/render"]);
  await assert.rejects(import("@layerdraw/export/core"), (error) => error?.code === "ERR_PACKAGE_PATH_NOT_EXPORTED");
  await assert.rejects(import("@layerdraw/export/csv"), (error) => error?.code === "ERR_PACKAGE_PATH_NOT_EXPORTED");
});

test("runtime source has no framework, DOM, Node builtin, Engine, Runtime, storage, registry, network, or ambient clock", async () => {
  const files = (await readdir(new URL("../src", import.meta.url))).filter((name) => name.endsWith(".ts"));
  const source = (await Promise.all(files.map((name) => readFile(new URL(`../src/${name}`, import.meta.url), "utf8")))).join("\n");
  assert.doesNotMatch(source, /from\s+["'](?:node:|react|@layerdraw\/(?:engine|runtime|registry))/iu);
  assert.doesNotMatch(source, /\b(?:document|window|localStorage|sessionStorage|fetch|XMLHttpRequest|Date\.now|new Date)\b/u);
  assert.doesNotMatch(source, /\.ldl|\.layerdraw|ArtifactStore|DocumentStore|EngineClient/u);
});

test("packed package carries declarations, ESM, README, and the canonical license", async () => {
  const temporary = await mkdtemp(join(tmpdir(), "layerdraw-export-pack-"));
  const { stdout } = await execute("corepack", ["pnpm", "pack", "--pack-destination", temporary, "--json"],
    { cwd: new URL("../", import.meta.url), maxBuffer: 10 * 1024 * 1024 });
  const packed = JSON.parse(stdout);
  const archive = join(temporary, packed.filename.split("/").at(-1));
  const extracted = join(temporary, "extracted");
  await mkdir(extracted);
  await execute("tar", ["-xzf", archive, "-C", extracted]);
  assert.deepEqual(await readFile(join(extracted,"package","LICENSE")), await readFile(new URL("../../../LICENSE", import.meta.url)));
  assert.ok((await readFile(join(extracted,"package","README.md"), "utf8")).includes("ExportPlan"));
  assert.ok((await readFile(join(extracted,"package","dist","index.d.ts"), "utf8")).includes("serializeExport"));
  assert.doesNotMatch(await readFile(join(extracted,"package","dist","browser.js"), "utf8"), /node:/u);
});
