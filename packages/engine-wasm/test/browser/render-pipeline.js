// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import * as render from "/__layerdraw/packages/render/dist/index.js";
import { createViewer } from "/__layerdraw/packages/viewer/dist/index.js";
import { browserRasterizer, serializeExport } from "/__layerdraw/packages/export/dist/browser.js";
import { executeRenderPipelineConformance } from "/__layerdraw/tests/conformance/render_pipeline_runner.mjs";

const json = async (path) => {
  const response = await fetch(path, { cache: "no-store" });
  if (!response.ok) throw new Error(`fixture fetch failed: ${path}`);
  return response.json();
};

globalThis.runLayerDrawRenderPipelineConformance = async () => {
  const [corpus, handshake, exportFixture, expected] = await Promise.all([
    json("/__layerdraw/tests/conformance/testdata/viewdata_conformance_v1.json"),
    json("/__layerdraw/schemas/fixtures/conformance/engine/handshake-success.json"),
    json("/__layerdraw/schemas/fixtures/conformance/export-plan-transport-parity-v1.json"),
    json("/__layerdraw/tests/conformance/testdata/render_pipeline_conformance_v1.json"),
  ]);
  const actual = await executeRenderPipelineConformance({
    corpus,
    capabilityManifest: handshake.payload.capability_manifest,
    exportFixture,
    environment: "browser",
    api: { ...render, createViewer, serializeExport, brandRasterizer: browserRasterizer },
  });
  return { actual, expected };
};
globalThis.layerDrawRenderPipelineReady = true;
