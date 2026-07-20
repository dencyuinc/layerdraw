// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { chromium } from "@playwright/test";
import { build } from "esbuild-wasm";

const bundled = await build({ entryPoints: [new URL("./browser-fixture.mjs", import.meta.url).pathname], bundle: true, format: "iife", platform: "browser", write: false });
const browser = await chromium.launch({ headless: true });
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  await page.setContent("<!doctype html><meta name='viewport' content='width=device-width'><div id='root'></div>");
  await page.addStyleTag({ path: new URL("../dist/styles.css", import.meta.url).pathname });
  await page.addScriptTag({ content: bundled.outputFiles[0].text });
  await page.getByRole("heading", { name: "Desktop roadmap" }).waitFor();

  await page.getByRole("navigation", { name: "Views" }).waitFor();
  await page.getByRole("region", { name: "Canvas" }).waitFor();
  const toolbar = page.getByRole("toolbar", { name: "Authoring commands" });
  await toolbar.waitFor();
  const undo = page.getByRole("button", { name: "Undo" });
  assert.equal(await undo.isEnabled(), true);
  await undo.click();
  assert.deepEqual(await page.evaluate(() => window.desktopWorkflow.calls.at(-1)), ["undo"]);
  const inventory = page.getByRole("button", { name: "Inventory" });
  await inventory.click();
  await page.waitForFunction(() => window.desktopWorkflow.calls.some((call) => call[0] === "select" && call[1] === "view:table"));
  assert.equal(await inventory.getAttribute("aria-current"), "page");
  assert.equal(await page.getByRole("heading", { name: "Desktop roadmap" }).evaluate((element) => element === document.activeElement), true);

  const product = page.getByRole("button", { name: "Product" });
  await product.focus();
  await page.keyboard.press("Enter");
  assert.deepEqual(await page.evaluate(() => window.desktopWorkflow.calls.at(-1)), ["viewer", "node:one"]);

  await page.evaluate(() => window.desktopWorkflow.capability(false));
  await page.waitForFunction(() => document.querySelector("button[aria-label^='System map']")?.disabled === true);
  assert.equal(await page.getByRole("button", { name: /System map.*unavailable/ }).isDisabled(), true);

  const desktopColumns = await page.locator(".ld-desktop-workspace").evaluate((element) => getComputedStyle(element).gridTemplateColumns);
  assert.ok(desktopColumns.split(" ").length >= 3);
  await page.setViewportSize({ width: 390, height: 844 });
  assert.equal(await page.locator(".ld-desktop-workspace").evaluate((element) => getComputedStyle(element).display), "block");
  assert.equal(await toolbar.evaluate((element) => getComputedStyle(element).flexWrap), "nowrap");
  await page.emulateMedia({ reducedMotion: "reduce" });
  const duration = await page.getByRole("button", { name: /System map/ }).evaluate((element) => Number.parseFloat(getComputedStyle(element).transitionDuration));
  assert.ok(duration <= 0.00001);
  console.log("Desktop shell browser E2E passed: landmarks, view/focus sync, keyboard Viewer selection, capability gating, responsive layout, and reduced motion.");
} finally {
  await browser.close();
}
