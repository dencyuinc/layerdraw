// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {expect, test as base} from "@playwright/test";

const test = base.extend({
  page: async ({baseURL, browserName, playwright}, use) => {
    const browser = await playwright[browserName].launch();
    const context = await browser.newContext(baseURL === undefined ? {} : {baseURL});
    const page = await context.newPage();
    try {
      await use(page);
    } finally {
      await context.close();
      await browser.close();
    }
  },
});

const parityCases = [
  "single_module_project",
  "multi_module_project",
  "installed_pack_project",
  "root_pack",
  "asset_project",
  "all_declarations_project",
  "deterministic_rejection",
  "resource_limit_rejection",
  "representative_large_graph",
  "cancellation",
];
const viewDataCases = [
  "diagram", "table_automatic", "table_relation", "table_relation_rows", "matrix", "tree", "flow", "context",
  "state_optional_absent", "state_optional_present", "state_required_present", "state_required_missing",
  "composed_diagram", "hidden_diagram_projection", "definition_diff", "mismatched_query_result",
  "materialization_item_limit", "deterministic_source_map_locale", "cancelled_materialization", "malformed_materialize_wire",
];

declare global {
  interface Window {
    layerDrawHarnessReady: boolean;
    runLayerDrawRealArtifactCorpus(): Promise<{limitKeys: string[]; parityCases: string[]; viewDataCases: string[]; endpointID: string; replacementID: string}>;
    runLayerDrawEngineClientCorpus(): Promise<{cases: string[]; viewDataCases: string[]; firstGeneration: number; replacementGeneration: number; state: string}>;
    runLayerDrawDirectLifecycle(): Promise<{staleFailure: {code: string; phase: string; retryable: boolean}; staleDetached: number; crashCode: string}>;
    runLayerDrawVerifiedSnapshotRace(): Promise<{wasmExecReads: number; revoked: number}>;
  }
}

test("packaged module Worker executes the parity corpus through real Go/WASM", async ({page}) => {
  test.setTimeout(480_000);
  const failures: string[] = [];
  page.on("console", (message) => { if (message.type() === "error") failures.push(message.text()); });
  page.on("pageerror", (error) => failures.push(error.message));
  await page.goto("/test/browser/harness.html");
  await page.waitForFunction(() => window.layerDrawHarnessReady === true);
  const result = await page.evaluate(() => window.runLayerDrawRealArtifactCorpus());
  expect(result.limitKeys).toEqual([
    "max_blob_id_bytes", "max_buffers", "max_control_bytes", "max_control_depth",
    "max_input_blob_bytes", "max_input_total_bytes", "max_output_blob_bytes",
    "max_output_total_bytes", "max_response_publish_bytes",
  ]);
  expect(result.parityCases).toEqual(parityCases);
  expect(result.viewDataCases).toEqual(viewDataCases.slice(0, 18));
  expect(result.endpointID).not.toBe(result.replacementID);
  expect(failures).toEqual([]);
});

test("public Engine client compiles the parity corpus through a real Go/WASM Worker", async ({page}) => {
  test.setTimeout(480_000);
  const failures: string[] = [];
  page.on("console", (message) => { if (message.type() === "error") failures.push(message.text()); });
  page.on("pageerror", (error) => failures.push(error.message));
  await page.goto("/test/browser/harness.html");
  await page.waitForFunction(() => window.layerDrawHarnessReady === true);
  const result = await page.evaluate(() => window.runLayerDrawEngineClientCorpus());
  expect(result).toEqual({
    cases: parityCases,
    viewDataCases,
    firstGeneration: 1,
    replacementGeneration: 2,
    state: "disposed",
  });
  expect(failures).toEqual([]);
});

test("verified runtime bytes remain authoritative when the source changes after validation", async ({page}) => {
  await page.goto("/test/browser/harness.html");
  await page.waitForFunction(() => window.layerDrawHarnessReady === true);
  const result = await page.evaluate(() => window.runLayerDrawVerifiedSnapshotRace());
  expect(result).toEqual({wasmExecReads: 1, revoked: 1});
});

test("real Worker rejects stale ownership, cleans up, and surfaces an isolated runtime crash", async ({page}) => {
  await page.goto("/test/browser/harness.html");
  await page.waitForFunction(() => window.layerDrawHarnessReady === true);
  const result = await page.evaluate(() => window.runLayerDrawDirectLifecycle());
  expect(result).toEqual({
    staleFailure: {code: "engine.worker.stale_generation", phase: "lifecycle", retryable: true},
    staleDetached: 0,
    crashCode: "engine.worker.crashed",
  });
});
