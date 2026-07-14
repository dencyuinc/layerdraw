// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {expect, test} from "@playwright/test";

declare global {
  interface Window {
    layerDrawHarnessReady: boolean;
    runLayerDrawRealArtifactCorpus(): Promise<{limitKeys: string[]; endpointID: string; replacementID: string}>;
    runLayerDrawDirectLifecycle(): Promise<{staleFailure: {code: string; phase: string; retryable: boolean}; staleDetached: number; crashCode: string}>;
  }
}

test("packaged module Worker handshakes and compiles Project and Pack through real Go/WASM", async ({page}) => {
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
  expect(result.endpointID).not.toBe(result.replacementID);
  expect(failures).toEqual([]);
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
