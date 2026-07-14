// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import {execFile} from "node:child_process";
import {createHash} from "node:crypto";
import {mkdtemp, readFile, writeFile} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {promisify} from "node:util";
import test from "node:test";

const execute = promisify(execFile);
const packageRoot = new URL("../", import.meta.url);
const repositoryRoot = new URL("../../../", import.meta.url);

test("packed package is closed, legal-complete, offline-installable, and SSR-safe", async () => {
  const temporary = await mkdtemp(join(tmpdir(), "layerdraw-engine-wasm-pack-"));
  const {stdout} = await execute("corepack", ["pnpm", "pack", "--pack-destination", temporary, "--json"], {
    cwd: packageRoot,
    maxBuffer: 10 * 1024 * 1024,
  });
  const packed = JSON.parse(stdout);
  const archive = join(temporary, packed.filename.split("/").at(-1));
  const {stdout: listingText} = await execute("tar", ["-tzf", archive]);
  const listing = listingText.trim().split("\n").sort();
  for (const required of [
    "package/LICENSE",
    "package/NOTICE",
    "package/README.md",
    "package/THIRD_PARTY_NOTICES.txt",
    "package/dist/index.js",
    "package/dist/index.d.ts",
    "package/dist/worker.js",
    "package/dist/worker.d.ts",
    "package/dist/layerdraw-engine.wasm",
    "package/dist/wasm_exec.js",
    "package/dist/engine-wasm-worker-v1.json",
    "package/dist/engine-wasm.manifest.json",
    "package/dist/engine-wasm.cdx.json",
    "package/dist/LICENSING.md",
    "package/dist/licenses/Apache-2.0.txt",
    "package/package.json",
  ]) assert.ok(listing.includes(required), `missing ${required}`);
  assert.equal(listing.some((path) => /\/(src|test|tools)\//.test(path) || path.endsWith(".tsbuildinfo")), false);

  const extracted = join(temporary, "extracted");
  await execute("mkdir", [extracted]);
  await execute("tar", ["-xzf", archive, "-C", extracted]);
  assert.deepEqual(await readFile(join(extracted, "package", "LICENSE")), await readFile(new URL("LICENSE", repositoryRoot)));
  assert.deepEqual(await readFile(join(extracted, "package", "NOTICE")), await readFile(new URL("NOTICE", repositoryRoot)));
  const manifest = JSON.parse(await readFile(join(extracted, "package", "package.json"), "utf8"));
  assert.equal(manifest.license, "SEE LICENSE IN LICENSE");
  assert.equal(manifest.dependencies, undefined);
  assert.equal(manifest.scripts?.postinstall, undefined);
  assert.deepEqual(manifest.sideEffects, ["./dist/worker.js"]);

  const artifactRoot = join(extracted, "package", "dist");
  assert.deepEqual(
    await readFile(join(artifactRoot, "engine-wasm-worker-v1.json")),
    await readFile(new URL("tests/conformance/testdata/engine_wasm_worker_v1.json", repositoryRoot)),
  );
  const artifactManifestBytes = await readFile(join(artifactRoot, "engine-wasm.manifest.json"));
  const artifactManifest = JSON.parse(artifactManifestBytes);
  assert.deepEqual(artifactManifest.files.map((file) => file.path).sort(), [
    "LICENSE", "LICENSING.md", "NOTICE", "THIRD_PARTY_NOTICES.txt",
    "engine-wasm-worker-v1.json", "engine-wasm.cdx.json", "layerdraw-engine.wasm",
    "licenses/Apache-2.0.txt", "wasm_exec.js",
  ]);
  for (const file of artifactManifest.files) {
    const bytes = await readFile(join(artifactRoot, file.path));
    assert.equal(bytes.byteLength, file.size, `${file.path} size`);
    assert.equal(`sha256:${createHash("sha256").update(bytes).digest("hex")}`, file.digest, `${file.path} digest`);
  }
  const {stdout: goroot} = await execute("go", ["env", "GOROOT"]);
  assert.deepEqual(await readFile(join(artifactRoot, "wasm_exec.js")), await readFile(join(goroot.trim(), "lib", "wasm", "wasm_exec.js")));

  const consumer = join(temporary, "consumer");
  await execute("mkdir", [consumer]);
  await writeFile(join(consumer, "package.json"), JSON.stringify({name: "engine-wasm-consumer", private: true, type: "module"}));
  await execute("corepack", ["pnpm", "add", "--offline", "--ignore-scripts", archive], {cwd: consumer, maxBuffer: 10 * 1024 * 1024});
  await execute("node", ["--input-type=module", "-e", `
    Object.defineProperty(globalThis, "Worker", {configurable: true, get() { throw new Error("SSR import touched Worker"); }});
    const root = await import("@layerdraw/engine-wasm");
    if (typeof root.createEngineWorkerTransport !== "function") process.exit(2);
  `], {cwd: consumer});
  await execute("node", ["--input-type=module", "-e", `
    for (const path of ["@layerdraw/engine-wasm/protocol", "@layerdraw/engine-wasm/dist/protocol.js", "@layerdraw/engine-wasm/src/index.ts"]) {
      try { await import(path); process.exit(2); }
      catch (error) { if (error?.code !== "ERR_PACKAGE_PATH_NOT_EXPORTED") throw error; }
    }
  `], {cwd: consumer});

  await assert.rejects(readFile(new URL("LICENSE", packageRoot)), (error) => error?.code === "ENOENT");
  await assert.rejects(readFile(new URL("NOTICE", packageRoot)), (error) => error?.code === "ENOENT");
});
