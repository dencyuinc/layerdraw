// SPDX-License-Identifier: Apache-2.0

import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { chmod, mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import test, { after } from "node:test";

import {
  EngineClientInputError,
  EngineClientTransportError,
} from "../dist/index.js";
import { createStdioEngineClient } from "../dist/stdio.js";
import { makePortableRequest } from "./support.mjs";

const repositoryRoot = resolve(import.meta.dirname, "../../..");
const temporary = await mkdtemp(join(tmpdir(), "layerdraw-engine-client-stdio-"));
const binaryPath = join(temporary, "layerdraw-engine");
const manifestBytes = await readFile(
  join(repositoryRoot, "deploy/development-release-manifest.json"),
);
const releaseManifestDigest = `sha256:${createHash("sha256").update(manifestBytes).digest("hex")}`;
const build = spawnSync(
  "go",
  [
    "build",
    "-trimpath",
    "-buildvcs=false",
    "-ldflags",
    `-s -w -X main.releaseVersion=0.0.0-dev -X main.sourceRevision=abcdef0 -X main.releaseManifestDigest=${releaseManifestDigest}`,
    "-o",
    binaryPath,
    "./cmd/layerdraw-engine",
  ],
  { cwd: repositoryRoot, encoding: "utf8" },
);
assert.equal(build.status, 0, `${build.stdout}\n${build.stderr}`);
await chmod(binaryPath, 0o755);

after(async () => {
  await rm(temporary, { recursive: true, force: true });
});

const clientOptions = Object.freeze({
  expectedReleaseManifestDigest: releaseManifestDigest,
  handshakeTimeoutMs: 10_000,
  defaultCompileTimeoutMs: 30_000,
  disposeTimeoutMs: 2_000,
});

test("stdio client negotiates, compiles through the real Go sidecar, restarts, and disposes", async () => {
  const client = await createStdioEngineClient({
    client: clientOptions,
    binaryPath,
    cwd: repositoryRoot,
  });
  assert.equal(client.state, "ready");
  assert.equal(client.hasCapability("engine.compile"), true);
  assert.deepEqual(client.getCapabilities().transports, ["stdio"]);
  const firstEndpoint = client.getEndpoint();

  const outcome = await client.compile(makePortableRequest().request);
  assert.equal(outcome.origin, "engine");
  assert.equal(outcome.outcome, "success");
  assert.ok(outcome.blobs.length >= 2);
  for (const blob of outcome.blobs) {
    assert.ok(blob.bytes instanceof Uint8Array);
    assert.equal(blob.bytes.byteLength, Number(blob.ref.size));
  }

  await client.restart();
  assert.equal(client.getEndpoint().generation, firstEndpoint.generation + 1);
  assert.notEqual(
    client.getEndpoint().handshake.endpoint_instance_id,
    firstEndpoint.handshake.endpoint_instance_id,
  );
  await client.dispose();
  assert.equal(client.state, "disposed");
});

test("stdio client rejects closed option shapes and redacts spawn failures", async () => {
  await assert.rejects(
    createStdioEngineClient({ client: clientOptions, binaryPath, extra: true }),
    (error) => error instanceof EngineClientInputError && error.code === "INVALID_ARGUMENT",
  );
  await assert.rejects(
    createStdioEngineClient({ client: clientOptions, binaryPath: "bad\0path" }),
    (error) => error instanceof EngineClientInputError && error.code === "INVALID_ARGUMENT",
  );
  await assert.rejects(
    createStdioEngineClient({
      client: { ...clientOptions, handshakeTimeoutMs: 500 },
      binaryPath: join(temporary, "missing-engine"),
    }),
    (error) => {
      assert.ok(error instanceof EngineClientTransportError);
      assert.equal(error.code, "SPAWN_FAILED");
      assert.equal(JSON.stringify(error).includes(temporary), false);
      return true;
    },
  );
});

test("stdio client treats an ended output pipe as a bounded transport failure", async () => {
  const started = performance.now();
  await assert.rejects(
    createStdioEngineClient({
      client: { ...clientOptions, handshakeTimeoutMs: 2_000 },
      binaryPath: "/bin/sh",
      binaryArguments: ["-c", "exec 1>&-; sleep 5"],
    }),
    (error) => {
      assert.ok(error instanceof EngineClientTransportError);
      assert.equal(error.code, "BROKEN_PIPE");
      return true;
    },
  );
  assert.ok(performance.now() - started < 1_000);
});
