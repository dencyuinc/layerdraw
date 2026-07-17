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
import {
  assertPortableCompileParityOutcome,
  executePortableQueryClientWorkflow,
  portableParityInput,
} from "../../engine-wasm/test/shared/real-engine.mjs";

const repositoryRoot = resolve(import.meta.dirname, "../../..");
const temporary = await mkdtemp(join(tmpdir(), "layerdraw-engine-client-stdio-"));
const binaryPath = join(temporary, "layerdraw-engine");
const manifestBytes = await readFile(
  join(repositoryRoot, "deploy/development-release-manifest.json"),
);
const parityCorpus = JSON.parse(await readFile(
  join(repositoryRoot, "tests/conformance/testdata/engine_compile_parity_v1.json"),
  "utf8",
));
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

test("stdio client executes the portable corpus through the real Go sidecar", async (context) => {
  const client = await createStdioEngineClient({
    client: clientOptions,
    binaryPath,
    cwd: repositoryRoot,
  });
  context.after(() => client.dispose());
  assert.equal(client.state, "ready");
  assert.equal(client.hasCapability("engine.compile"), true);
  assert.deepEqual(client.getCapabilities().transports, ["stdio"]);
  const firstEndpoint = client.getEndpoint();

  for (const testCase of parityCorpus.cases) {
    if (testCase.execution === "cancel") continue;
    const outcome = await client.compile(portableParityInput(testCase), {
      requestId: testCase.expected.response.request_id,
    });
    await assertPortableCompileParityOutcome(outcome, testCase, "0.0.0-dev");
  }
  const cancellation = parityCorpus.cases.find((testCase) => testCase.execution === "cancel");
  assert.notEqual(cancellation, undefined);
  const controller = new AbortController();
  const pendingCancellation = client.compile(portableParityInput(cancellation), {
    requestId: cancellation.expected.response.request_id,
    signal: controller.signal,
  });
  controller.abort();
  const cancelled = await pendingCancellation;
  assert.equal(cancelled.origin, "client");
  assert.equal(cancelled.outcome, "cancelled");

  await executePortableQueryClientWorkflow(client, "stdio-query");

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
