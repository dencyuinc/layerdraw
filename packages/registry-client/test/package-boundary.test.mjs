// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { readFile, readdir } from "node:fs/promises";
import test from "node:test";

test("Registry client remains a transport facade", async () => {
  const root = new URL("../src/", import.meta.url);
  const source = (await Promise.all((await readdir(root)).filter((name) => name.endsWith(".ts")).map((name) => readFile(new URL(name, root), "utf8")))).join("\n");
  for (const forbidden of ["node:crypto", "archive/zip", "semver", "ed25519", "resolveDependencies", "verifySignature", "rewriteLDL", "@layerdraw/library", "react"]) {
    assert.equal(source.includes(forbidden), false, `forbidden Registry semantic/UI dependency: ${forbidden}`);
  }
});
