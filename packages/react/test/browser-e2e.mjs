// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { chromium } from "@playwright/test";
import { build } from "esbuild-wasm";

const bundled = await build({
  entryPoints: [new URL("./browser-fixture.mjs", import.meta.url).pathname],
  bundle: true,
  format: "iife",
  platform: "browser",
  write: false,
});
const browser = await chromium.launch({ headless: true });
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  await page.setContent("<!doctype html><div id='root'></div>");
  await page.addStyleTag({ path: new URL("../dist/styles.css", import.meta.url).pathname });
  await page.addScriptTag({ content: bundled.outputFiles[0].text });
  await page.waitForFunction(() => window.editorWorkflow !== undefined);

  const apply = page.locator("button[aria-label='Apply']");
  const undo = page.getByRole("button", { name: "Undo" });
  const cancel = page.getByRole("button", { name: "Cancel preview" });
  assert.equal(await apply.getAttribute("data-action-state"), "durable");
  assert.equal(await page.getByRole("button", { name: "Redo" }).isDisabled(), true);
  await apply.focus();
  await page.keyboard.press("ArrowRight");
  assert.equal(await undo.evaluate((element) => element === document.activeElement), true);

  await page.evaluate(() => window.editorWorkflow.capability(false));
  await page.waitForFunction(() => document.querySelector("button[aria-label='Apply']")?.dataset.actionState === "unavailable");
  assert.equal(await apply.isDisabled(), true);
  await page.evaluate(() => { window.editorWorkflow.capability(true); window.editorWorkflow.deny(); });
  await page.waitForFunction(() => document.querySelector("button[aria-label='Apply']")?.dataset.actionState === "denied");
  assert.equal(await apply.isDisabled(), true);

  await page.evaluate(() => window.editorWorkflow.previewing());
  await page.waitForFunction(() => document.querySelector("button[aria-label='Cancel preview']")?.dataset.actionState === "durable");
  assert.equal(await cancel.isDisabled(), false);
  await cancel.click();
  await page.waitForFunction(() => document.querySelector("button[aria-label='Cancel preview']")?.dataset.actionState === "unavailable");

  await page.evaluate(() => window.editorWorkflow.failApply());
  await page.waitForFunction(() => document.querySelector("button[aria-label='Apply']")?.dataset.actionState === "durable");
  await apply.focus();
  await apply.click();
  await page.getByRole("status").filter({ hasText: "host apply failed" }).waitFor();
  assert.equal(await apply.evaluate((element) => element === document.activeElement), true);

  await page.evaluate(() => window.editorWorkflow.replaceEditor());
  await page.waitForFunction(() => window.editorWorkflow.listenerCounts().previous === 0 && window.editorWorkflow.listenerCounts().current === 1);
  assert.deepEqual(await page.evaluate(() => window.editorWorkflow.listenerCounts()), { previous: 0, current: 1 });

  const outline = page.getByRole("listbox", { name: "Document outline results" });
  assert.equal(await outline.getByRole("option").count(), 40, "large Engine result sets stay bounded");
  await outline.focus();
  await page.keyboard.press("ArrowDown");
  await page.waitForFunction(() => window.editorWorkflow.navigation().selection.address?.endsWith("item_0000"));
  await page.keyboard.press("End");
  await page.waitForFunction(() => window.editorWorkflow.navigation().selection.address?.endsWith("item_0039"));
  await page.keyboard.press("Enter");
  assert.equal((await page.evaluate(() => window.editorWorkflow.navigation().source.address)).endsWith("item_0039"), true);
  const search = page.getByRole("searchbox", { name: "Search structure" });
  await search.fill("Engine item 299");
  await outline.getByRole("option", { name: /Engine item 299/ }).waitFor();
  assert.equal(await outline.getByRole("option").count(), 1);
  const inspectorDraft = page.getByRole("textbox", { name: "Name" });
  await inspectorDraft.fill("Host controlled draft");
  await page.getByRole("button", { name: "Preview change" }).first().click();
  await page.waitForFunction(() => window.editorWorkflow.navigation().calls.some(([name]) => name === "preview"));
  assert.equal(await page.evaluate(() => window.editorWorkflow.navigation().calls.at(-1)[1].kind), "fragment");
  assert.equal(await page.evaluate(() => window.editorWorkflow.navigation().calls.at(-1)[1].request.fragment), "Host controlled draft");

  await page.getByText("Empty view").waitFor();
  await page.evaluate(() => window.editorWorkflow.viewer("loading"));
  await page.waitForFunction(() => document.querySelector(".ld-live-viewer")?.getAttribute("aria-busy") === "true");
  await page.evaluate(() => window.editorWorkflow.viewer("error"));
  await page.getByRole("alert").waitFor();
  await page.evaluate(() => window.editorWorkflow.viewer("partial"));
  await page.getByText("Partial view").waitFor();
  await page.evaluate(() => window.editorWorkflow.viewer("dense"));
  await page.waitForFunction(() => document.querySelector("[data-item-count='200']") !== null);
  await page.evaluate(() => window.editorWorkflow.viewer("2d"));
  await page.waitForFunction(() => document.querySelector("[data-render-shape='2d']") !== null);
  await page.evaluate(() => window.editorWorkflow.viewer("3d"));
  await page.waitForFunction(() => document.querySelector("[data-render-shape='3d']") !== null);

  await page.evaluate(() => window.editorWorkflow.recovery("approval-unavailable"));
  const approvalApply = page.getByRole("button", { name: "Request approval and apply" });
  await approvalApply.waitFor();
  assert.equal(await approvalApply.isDisabled(), true);
  await page.evaluate(() => window.editorWorkflow.recovery("conflict"));
  await page.getByText("The authoring intent conflicts with the current document.").waitFor();
  await page.getByRole("button", { name: "Refresh" }).click();
  assert.deepEqual(await page.evaluate(() => window.editorWorkflow.recoveryCalls().at(-1)), ["refresh", "browser-e2e"]);
  await page.evaluate(() => window.editorWorkflow.recovery("disconnected"));
  await page.getByText("Runtime restarted").waitFor();
  await page.getByRole("button", { name: "Reopen session" }).click();
  await page.waitForFunction(() => document.querySelector(".ld-authoring-recovery")?.dataset.workflowStatus === "review");
  assert.deepEqual(await page.evaluate(() => window.editorWorkflow.recoveryCalls().at(-1)), ["reopen"]);
  await page.evaluate(() => window.editorWorkflow.recovery("conflict"));

  const desktop = await page.locator(".ld-editor-workspace").evaluate((element) => getComputedStyle(element).gridTemplateColumns);
  assert.ok(desktop.split(" ").length >= 2);
  await page.setViewportSize({ width: 390, height: 844 });
  const mobile = await page.locator(".ld-editor-workspace").evaluate((element) => getComputedStyle(element).gridTemplateColumns);
  assert.equal(mobile.split(" ").length, 1);
  assert.equal(await page.locator(".ld-query-view-actions").evaluate((element) => getComputedStyle(element).flexDirection), "column");
  assert.equal(await page.locator(".ld-recovery-actions").evaluate((element) => getComputedStyle(element).flexDirection), "column");
  await page.emulateMedia({ reducedMotion: "reduce" });
  const motion = await apply.evaluate((element) => Number.parseFloat(getComputedStyle(element).transitionDuration));
  assert.equal(motion, 0.00001);
  console.log("React editor workflow E2E passed with query/view, recovery, responsive, capability, keyboard, focus, and motion coverage.");
} finally {
  await browser.close();
}
