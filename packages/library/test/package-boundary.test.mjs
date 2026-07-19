// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("Library consumes opaque host decisions and owns no Registry semantics", async () => {
  const source = await readFile(new URL("../src/index.ts", import.meta.url), "utf8");
  assert.match(source, /@layerdraw\/registry-client/);
  for (const forbidden of ["node:crypto", "archive/zip", "semver", "ed25519", "resolveDependencies", "verifySignature", "rewriteLDL", "layerdraw.resolved.json", "pack/"]) {
    assert.equal(source.includes(forbidden), false, `forbidden Library semantic: ${forbidden}`);
  }
});
