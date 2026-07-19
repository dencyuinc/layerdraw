#!/usr/bin/env node
// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import { readFile, writeFile } from "node:fs/promises";
import * as render from "../packages/render/dist/index.js";
import { createViewer } from "../packages/viewer/dist/index.js";
import { nodeRasterizer, serializeExport } from "../packages/export/dist/node.js";
import { executeRenderPipelineConformance } from "../tests/conformance/render_pipeline_runner.mjs";

const readJSON = async (path) => JSON.parse(await readFile(new URL(path, import.meta.url), "utf8"));
const result = await executeRenderPipelineConformance({
  corpus: await readJSON("../tests/conformance/testdata/viewdata_conformance_v1.json"),
  capabilityManifest: (await readJSON("../schemas/fixtures/conformance/engine/handshake-success.json")).payload.capability_manifest,
  exportFixture: await readJSON("../schemas/fixtures/conformance/export-plan-transport-parity-v1.json"),
  environment: "node",
  api: { ...render, createViewer, serializeExport, brandRasterizer: nodeRasterizer },
});
await writeFile(
  new URL("../tests/conformance/testdata/render_pipeline_conformance_v1.json", import.meta.url),
  `${JSON.stringify(result, null, 2)}\n`
);
