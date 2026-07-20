// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import test from "node:test";
import { wailsEngineBindingDescriptors, wailsRuntimeBindingDescriptors } from "@layerdraw/engine-client/wails";
import { createDesktopGeneratedBindings } from "../dist/index.js";

test("generated Wails descriptor closure crosses the Go byte boundary exactly", async () => {
  const calls = [];
  const bindings = createDesktopGeneratedBindings(async (method, exchange) => {
    calls.push([method, exchange]);
    return { outcome: "success", value: { operation: exchange.operation, control: exchange.control, blobs: exchange.blobs } };
  });
  const descriptors = [...wailsEngineBindingDescriptors, ...wailsRuntimeBindingDescriptors];
  assert.equal(Object.keys(bindings).length, descriptors.length);
  for (const descriptor of descriptors) assert.equal(typeof bindings[descriptor.generatedMethod], "function");
  const output = await bindings.EngineHandshake({ operation: "engine.handshake", control: "eyJyZXF1ZXN0X2lkIjoid2FpbHMifQ==", blobs: [{ blob_id: "blob", bytes: "AQID" }] });
  assert.equal(calls[0][0], "EngineHandshake");
  assert.equal(calls[0][1].control, "eyJyZXF1ZXN0X2lkIjoid2FpbHMifQ==");
  assert.equal(output.control, "eyJyZXF1ZXN0X2lkIjoid2FpbHMifQ==");
  assert.equal(output.blobs[0].bytes, "AQID");
});

test("failed Desktop binding results never become generated responses", async () => {
  const bindings = createDesktopGeneratedBindings(async () => ({ outcome: "failed" }));
  await assert.rejects(bindings.RuntimeHandshake({ operation: "runtime.handshake", control: "{}", blobs: [] }), { code: "BINDING_VERSION_MISMATCH" });
});
