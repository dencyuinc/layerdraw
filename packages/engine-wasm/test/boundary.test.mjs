// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import {readdir, readFile} from "node:fs/promises";
import test from "node:test";

const sourceRoot = new URL("../src/", import.meta.url);

test("runtime package has closed exports, no runtime dependencies, and no semantic implementation imports", async () => {
  const manifest = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8"));
  assert.deepEqual(Object.keys(manifest.exports).sort(), [".", "./worker"]);
  assert.equal(manifest.dependencies, undefined);
  assert.equal(manifest.optionalDependencies, undefined);
  assert.equal(manifest.peerDependencies, undefined);
  assert.equal(manifest.scripts.postinstall, undefined);

  const sources = await readdir(sourceRoot);
  for (const name of sources.filter((value) => value.endsWith(".ts"))) {
    const content = await readFile(new URL(name, sourceRoot), "utf8");
    assert.doesNotMatch(content, /from\s+["'](?:node:|@layerdraw\/engine-client|@layerdraw\/protocol|react|vue|svelte)/);
    assert.doesNotMatch(content, /decodeHandshake|decodeCompile|StableAddress|semantic sort|diagnostic repair|recipe normalization|LDL parser/i);
  }
  const worker = await readFile(new URL("worker.ts", sourceRoot), "utf8");
  assert.match(worker, /createVerifiedWasmEndpoint/);
  assert.doesNotMatch(worker, /EngineByteEndpointFactory|makeEcho|fake/i);
  for (const name of ["host.ts", "protocol.ts", "worker-runtime.ts", "worker.ts"]) {
    assert.doesNotMatch(await readFile(new URL(name, sourceRoot), "utf8"), /JSON\.(?:parse|stringify)/);
  }
});
