// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {execFile} from "node:child_process";
import {cp, mkdir, rm} from "node:fs/promises";
import {resolve} from "node:path";
import {fileURLToPath} from "node:url";
import {promisify} from "node:util";

const execute = promisify(execFile);
const packageRoot = resolve(fileURLToPath(new URL("../", import.meta.url)));
const repositoryRoot = resolve(packageRoot, "../..");
const output = resolve(packageRoot, "dist");
const artifactStage = resolve(packageRoot, ".artifact-stage");
const allowDirty = process.argv.includes("--allow-dirty") ? "1" : "0";

await rm(output, {recursive: true, force: true});
await rm(artifactStage, {recursive: true, force: true});
try {
  await execute("corepack", ["pnpm", "exec", "tsc", "-p", "tsconfig.build.json"], {cwd: packageRoot});
  const {stdout: revision} = await execute("git", ["rev-parse", "HEAD"], {cwd: repositoryRoot});
  await execute(resolve(repositoryRoot, "tools/build-engine-wasm.sh"), [], {
    cwd: repositoryRoot,
    env: {
      ...process.env,
      ENGINE_WASM_ALLOW_DIRTY: allowDirty,
      ENGINE_WASM_OUTPUT_DIR: artifactStage,
      SOURCE_REVISION: revision.trim(),
      VERSION: process.env.VERSION ?? "0.0.0-dev",
    },
    maxBuffer: 16 * 1024 * 1024,
  });
  await mkdir(output, {recursive: true});
  await cp(artifactStage, output, {recursive: true, force: false});
} finally {
  await rm(artifactStage, {recursive: true, force: true});
}
