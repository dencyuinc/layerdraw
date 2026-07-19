// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import * as render from "../../render/dist/index.js";
import { createViewer } from "../../viewer/dist/index.js";
import { nodeRasterizer, serializeExport } from "../dist/node.js";
import { executeRenderPipelineConformance } from "../../../tests/conformance/render_pipeline_runner.mjs";

const readJSON = async (relative) => JSON.parse(await readFile(new URL(relative, import.meta.url), "utf8"));

test("the shared Render Pipeline corpus matches the reproducible Node fixture", async () => {
  const [corpus, handshake, exportFixture, expected] = await Promise.all([
    readJSON("../../../tests/conformance/testdata/viewdata_conformance_v1.json"),
    readJSON("../../../schemas/fixtures/conformance/engine/handshake-success.json"),
    readJSON("../../../schemas/fixtures/conformance/export-plan-transport-parity-v1.json"),
    readJSON("../../../tests/conformance/testdata/render_pipeline_conformance_v1.json"),
  ]);
  const actual = await executeRenderPipelineConformance({
    corpus,
    capabilityManifest: handshake.payload.capability_manifest,
    exportFixture,
    environment: "node",
    api: { ...render, createViewer, serializeExport, brandRasterizer: nodeRasterizer },
  });
  assert.deepEqual(actual, expected);
});
