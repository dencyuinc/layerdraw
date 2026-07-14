// SPDX-License-Identifier: Apache-2.0

import assert from "node:assert/strict";
import { test } from "node:test";
import {
  EngineClientBackpressureError,
  EngineClientDecodeError,
  EngineClientDisposeError,
  EngineClientInputError,
  EngineClientStateError,
  EngineClientTransportError,
  EngineClientUnsupportedEnvironmentError,
} from "../dist/index.js";
import { createInternalEngineClient } from "../dist/internal/client.js";
import { InternalTransportFault } from "../dist/internal/transport.js";
import {
  collectors,
  creationOptions,
  limits,
  makeFactory,
  makePortableRequest,
} from "./support.mjs";

test("exception taxonomy has stable names, kinds, codes, retryability, and details", () => {
  const cases = [
    [new EngineClientInputError("INVALID_ARGUMENT"), "misuse", false],
    [new EngineClientStateError("FAILED"), "misuse", false],
    [
      new EngineClientBackpressureError("SINGLE_FLIGHT_BUSY"),
      "misuse",
      true,
    ],
    [new EngineClientTransportError("BROKEN_PIPE"), "transport", true],
    [new EngineClientDecodeError("MALFORMED_MESSAGE"), "decode", false],
    [
      new EngineClientUnsupportedEnvironmentError({ generation: 1 }),
      "transport",
      false,
    ],
    [new EngineClientDisposeError({ exitCode: 9 }), "transport", false],
  ];
  for (const [error, kind, retryable] of cases) {
    assert.equal(error.name, error.constructor.name);
    assert.equal(error.kind, kind);
    assert.equal(error.retryable, retryable);
    assert.equal("cause" in error, false);
    assert.ok(error.message.length > 0);
  }
  assert.equal(cases[5][0].code, "UNSUPPORTED_ENVIRONMENT");
  assert.equal(cases[6][0].code, "DISPOSE_FAILED");

  const filtered = new EngineClientTransportError("BROKEN_PIPE", true, {
    generation: 2,
    workerUrl: "https://secret.invalid/worker.js",
    path: "/Users/private/source.ldl",
    limit: Number.NaN,
  });
  assert.deepEqual({ ...filtered.details }, { generation: 2 });
  assert.ok(Object.isFrozen(filtered.details));
});

test("invalid creation arguments fail before an endpoint or bytes are retained", async () => {
  const invalidOptions = [
    {},
    { expectedReleaseManifestDigest: "bad" },
    { ...creationOptions, handshakeTimeoutMs: 0 },
    { ...creationOptions, defaultCompileTimeoutMs: 600_001 },
    { ...creationOptions, disposeTimeoutMs: 1.5 },
    { ...creationOptions, requiredCapabilities: ["not valid"] },
    { ...creationOptions, optionalCapabilities: Object.freeze({}) },
    { ...creationOptions, clientLimits: { max_assets: "-1" } },
  ];
  for (const options of invalidOptions) {
    const factory = await makeFactory();
    await assert.rejects(
      createInternalEngineClient({
        transportFactory: factory,
        protocolCollectors: collectors,
        options,
      }),
      EngineClientInputError,
    );
    assert.equal(factory.endpoints.length, 0);
  }
  await assert.rejects(
    createInternalEngineClient({}),
    EngineClientInputError,
  );
});

test("invalid internal transport identity and ready limits fail closed", async () => {
  const invalidIdentity = await makeFactory();
  invalidIdentity.transportId = "not.valid";
  await assert.rejects(
    createInternalEngineClient({
      transportFactory: invalidIdentity,
      protocolCollectors: collectors,
      options: creationOptions,
    }),
    EngineClientInputError,
  );

  for (const readyValue of [
    { ...limits, maxBuffers: 0 },
    { ...limits, maxInputBlobBytes: limits.maxInputTotalBytes + 1 },
    { ...limits, maxResponsePublishBytes: 1 },
    { ...limits, unexpected: 1 },
  ]) {
    const factory = await makeFactory({ readyValue });
    await assert.rejects(
      createInternalEngineClient({
        transportFactory: factory,
        protocolCollectors: collectors,
        options: creationOptions,
      }),
      EngineClientTransportError,
    );
  }
});

test("branded adapter failures preserve only the stable safe classification", async () => {
  for (const [kind, code, Expected] of [
    ["decode", "MALFORMED_FRAME", EngineClientDecodeError],
    ["transport", "BROKEN_PIPE", EngineClientTransportError],
  ]) {
    const factory = await makeFactory({
      compile() {
        throw new InternalTransportFault({
          kind,
          code,
          retryable: true,
          details: {
            generation: 1,
            path: "/Users/private/secret.ldl",
          },
        });
      },
    });
    const client = await createInternalEngineClient({
      transportFactory: factory,
      protocolCollectors: collectors,
      options: creationOptions,
    });
    await assert.rejects(client.compile(makePortableRequest().request), (error) => {
      assert.ok(error instanceof Expected);
      assert.equal(error.code, code);
      assert.equal(error.details.generation, 1);
      assert.equal("path" in error.details, false);
      assert.equal(error.details.replacementSucceeded, true);
      return true;
    });
    await client.dispose();
  }
});

test("synchronous transfer failure is redacted and automatically replaced", async () => {
  const factory = await makeFactory();
  const client = await createInternalEngineClient({
    transportFactory: factory,
    protocolCollectors: collectors,
    options: creationOptions,
  });
  factory.endpoints[0].request = () => {
    throw new Error("stderr /private/path source LDL");
  };
  await assert.rejects(client.compile(makePortableRequest().request), (error) => {
    assert.ok(error instanceof EngineClientTransportError);
    assert.equal(error.code, "TRANSFER_FAILED");
    assert.equal(error.details.replacementSucceeded, true);
    return true;
  });
  assert.equal(factory.endpoints.length, 2);
  await client.dispose();
});

test("dispose reports one stable failure after forcing terminal state", async () => {
  const factory = await makeFactory();
  const client = await createInternalEngineClient({
    transportFactory: factory,
    protocolCollectors: collectors,
    options: creationOptions,
  });
  factory.endpoints[0].dispose = async () => {
    throw new Error("sensitive close failure");
  };
  const disposal = client.dispose();
  await assert.rejects(disposal, (error) => {
    assert.ok(error instanceof EngineClientDisposeError);
    assert.equal(error.code, "DISPOSE_FAILED");
    assert.equal("cause" in error, false);
    return true;
  });
  assert.equal(client.state, "disposed");
  assert.equal(client.dispose(), disposal);
});
