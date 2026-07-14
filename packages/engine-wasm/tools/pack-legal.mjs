// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {readFile, rm, writeFile} from "node:fs/promises";

const packageRoot = new URL("../", import.meta.url);
const marker = new URL(".pack-legal-staged", packageRoot);
const files = [
  [new URL("../../../LICENSE", import.meta.url), new URL("LICENSE", packageRoot)],
  [new URL("../../../NOTICE", import.meta.url), new URL("NOTICE", packageRoot)],
];

async function prepare() {
  const staged = [];
  for (const [source, target] of files) {
    const canonical = await readFile(source);
    try {
      const existing = await readFile(target);
      if (!existing.equals(canonical)) throw new Error(`refusing to replace non-canonical ${target.pathname}`);
    } catch (error) {
      if (error !== null && typeof error === "object" && "code" in error && error.code === "ENOENT") {
        await writeFile(target, canonical, {flag: "wx"});
        staged.push(target.pathname.split("/").at(-1));
        continue;
      }
      throw error;
    }
  }
  if (staged.length > 0) await writeFile(marker, `${staged.join("\n")}\n`, {flag: "wx"});
}

async function clean() {
  let staged;
  try {
    staged = (await readFile(marker, "utf8")).trim().split("\n").filter(Boolean);
  } catch (error) {
    if (error !== null && typeof error === "object" && "code" in error && error.code === "ENOENT") return;
    throw error;
  }
  for (const name of staged) {
    const pair = files.find(([, target]) => target.pathname.endsWith(`/${name}`));
    if (pair === undefined) throw new Error(`invalid staged legal file marker: ${name}`);
    const [source, target] = pair;
    const canonical = await readFile(source);
    try {
      const staged = await readFile(target);
      if (!staged.equals(canonical)) throw new Error(`refusing to remove non-canonical ${target.pathname}`);
      await rm(target);
    } catch (error) {
      if (error !== null && typeof error === "object" && "code" in error && error.code === "ENOENT") continue;
      throw error;
    }
  }
  await rm(marker);
}

const operation = process.argv[2];
if (operation === "prepare") await prepare();
else if (operation === "clean") await clean();
else throw new Error("usage: node tools/pack-legal.mjs prepare|clean");
