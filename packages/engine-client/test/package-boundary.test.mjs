// SPDX-License-Identifier: Apache-2.0

import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import {
  mkdtemp,
  readFile,
  readdir,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import { test } from "node:test";

const packageRoot = resolve(import.meta.dirname, "..");
const repoRoot = resolve(packageRoot, "../..");

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    cwd: options.cwd ?? packageRoot,
    encoding: "utf8",
    env: { ...process.env, NO_COLOR: "1" },
  });
  assert.equal(
    result.status,
    0,
    `${command} ${args.join(" ")} failed\n${result.stdout}\n${result.stderr}`,
  );
  return result.stdout;
}

function digest(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

test("export map is closed and isolates root, stdio, and WASM environments", async () => {
  const manifest = JSON.parse(
    await readFile(join(packageRoot, "package.json"), "utf8"),
  );
  assert.deepEqual(Object.keys(manifest.exports), [".", "./stdio", "./wasm"]);
  assert.equal(manifest.sideEffects, false);
  assert.equal(manifest.type, "module");
  assert.equal(manifest.license, "Apache-2.0");
  assert.deepEqual(Object.keys(manifest.dependencies), ["@layerdraw/protocol"]);
  assert.deepEqual(manifest.peerDependencies, {
    "@layerdraw/engine-wasm": "workspace:*",
  });
  assert.equal(manifest.peerDependenciesMeta["@layerdraw/engine-wasm"].optional, true);
  assert.equal(JSON.stringify(manifest.exports).includes("*"), false);

  const source = await readFile(join(packageRoot, "src/index.ts"), "utf8");
  const emitted = await readFile(join(packageRoot, "dist/index.js"), "utf8");
  const declarations = await readFile(join(packageRoot, "dist/index.d.ts"), "utf8");
  for (const text of [source, emitted, declarations]) {
    assert.equal(/from ["']node:/.test(text), false);
    assert.equal(
      /\b(?:new\s+Worker|globalThis\.Worker|WebAssembly|document\.|window\.)\b/.test(
        text,
      ),
      false,
    );
    assert.equal(/export\s+\*/.test(text), false);
  }

  const root = await import("@layerdraw/engine-client");
  const stdio = await import("@layerdraw/engine-client/stdio");
  const wasm = await import("@layerdraw/engine-client/wasm");
  assert.equal(typeof root.EngineClientInputError, "function");
  assert.equal(typeof stdio.createStdioEngineClient, "function");
  assert.equal(typeof wasm.createWasmEngineClient, "function");
  const wasmSource = await readFile(join(packageRoot, "dist/wasm.js"), "utf8");
  assert.equal(/from ["']node:/.test(wasmSource), false);
  assert.match(await readFile(join(packageRoot, "dist/stdio.js"), "utf8"), /node:child_process/);
  await assert.rejects(
    import("@layerdraw/engine-client/internal/client"),
    (error) => error.code === "ERR_PACKAGE_PATH_NOT_EXPORTED",
  );
});

test("pack is deterministic, legal-complete, and installable offline", async () => {
  const first = await mkdtemp(join(tmpdir(), "engine-client-pack-a-"));
  const second = await mkdtemp(join(tmpdir(), "engine-client-pack-b-"));
  const install = await mkdtemp(join(tmpdir(), "engine-client-install-"));
  const protocolPack = await mkdtemp(join(tmpdir(), "protocol-pack-"));
  try {
    run("pnpm", ["pack", "--pack-destination", first]);
    run("pnpm", ["pack", "--pack-destination", second]);
    run("pnpm", [
      "--dir",
      join(repoRoot, "packages/protocol"),
      "pack",
      "--pack-destination",
      protocolPack,
    ]);
    const firstName = (await readdir(first)).find((name) => name.endsWith(".tgz"));
    const secondName = (await readdir(second)).find((name) => name.endsWith(".tgz"));
    const protocolName = (await readdir(protocolPack)).find((name) =>
      name.endsWith(".tgz"),
    );
    assert.ok(firstName && secondName && protocolName);
    const firstBytes = await readFile(join(first, firstName));
    const secondBytes = await readFile(join(second, secondName));
    assert.equal(digest(firstBytes), digest(secondBytes));

    const listing = run("tar", ["-tzf", join(first, firstName)]);
    assert.match(listing, /package\/LICENSE/);
    assert.match(listing, /package\/README\.md/);
    assert.match(listing, /package\/dist\/index\.js/);
    assert.match(listing, /package\/dist\/stdio\.js/);
    assert.match(listing, /package\/dist\/wasm\.js/);
    assert.doesNotMatch(listing, /package\/src\//);
    assert.doesNotMatch(listing, /package\/test\//);

    await writeFile(
      join(install, "package.json"),
      JSON.stringify({
        name: "smoke",
        private: true,
        type: "module",
        dependencies: {
          "@layerdraw/engine-client": `file:${join(first, firstName)}`,
          "@layerdraw/protocol": `file:${join(protocolPack, protocolName)}`,
        },
      }),
    );
    await writeFile(
      join(install, "pnpm-workspace.yaml"),
      `packages:\n  - .\noverrides:\n  '@layerdraw/protocol': 'file:${join(protocolPack, protocolName)}'\n`,
    );
    run(
      "pnpm",
      ["install", "--offline", "--ignore-scripts"],
      { cwd: install },
    );
    const smoke = run(
      "node",
      [
        "--input-type=module",
        "-e",
        "import('@layerdraw/engine-client').then(m=>{if(typeof m.EngineClientInputError!=='function')process.exit(2)})",
      ],
      { cwd: install },
    );
    assert.equal(smoke, "");
    const forbidden = spawnSync(
      "node",
      [
        "--input-type=module",
        "-e",
        "import('@layerdraw/engine-client/internal/client')",
      ],
      { cwd: install, encoding: "utf8" },
    );
    assert.notEqual(forbidden.status, 0);
    assert.match(forbidden.stderr, /ERR_PACKAGE_PATH_NOT_EXPORTED/);
  } finally {
    await Promise.all(
      [first, second, install, protocolPack].map((path) =>
        rm(path, { recursive: true, force: true }),
      ),
    );
  }
});
