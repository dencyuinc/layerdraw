// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { chromium } from "@playwright/test";
import { build } from "esbuild-wasm";

const bundled = await build({ entryPoints: [new URL("./browser-fixture.mjs", import.meta.url).pathname], bundle: true, format: "iife", platform: "browser", write: false });
const browser = await chromium.launch({ headless: true });
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  await page.setContent("<!doctype html><meta name='viewport' content='width=device-width'><div id='root'></div>");
  await page.addStyleTag({ path: new URL("../../../packages/react/dist/tokens.css", import.meta.url).pathname });
  await page.addStyleTag({ path: new URL("../../../packages/react/dist/primitives.css", import.meta.url).pathname });
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
	const mcp = page.getByRole("region", { name: "MCP connections" });
	await mcp.waitFor();
	await mcp.getByRole("button", { name: "Enable MCP" }).click();
	await mcp.getByText("Connection instructions").waitFor();
	await mcp.getByRole("button", { name: "Connect agent" }).click();
	await mcp.getByText(/Proposal only/).waitFor();
	assert.equal(await mcp.getByRole("button", { name: "Revoke access" }).isEnabled(), true);
	await mcp.getByRole("button", { name: "Restart host" }).click();
	await mcp.getByText("host restarted").waitFor();
	assert.equal(await mcp.getByRole("button", { name: "Revoke access" }).count(), 0);
	await mcp.getByRole("button", { name: "Connect agent" }).click();
	await mcp.getByRole("button", { name: "Revoke access" }).click();
	assert.deepEqual((await page.evaluate(() => window.desktopWorkflow.calls.filter((call) => call[0].startsWith("mcp-")))).map((call) => call[0]), ["mcp-enable", "mcp-connect", "mcp-restart", "mcp-connect", "mcp-revoke"]);
  const library = page.getByRole("region", { name: "Library" });
  await library.waitFor();
  const browse = library.getByRole("form", { name: "Browse Registry" });
  await browse.getByRole("searchbox", { name: "Search" }).fill("catalog");
  await browse.getByRole("button", { name: "Browse" }).click();
  await library.getByRole("button", { name: /layerdraw\/catalog/ }).click();
  await library.getByRole("button", { name: "Preview install" }).click();
  await library.getByRole("region", { name: "Registry change preview" }).waitFor();
  await library.getByRole("button", { name: "Confirm and apply" }).click();
  await page.waitForFunction(() => window.desktopWorkflow.calls.some((call) => call[0] === "library-confirm"));
  assert.deepEqual((await page.evaluate(() => window.desktopWorkflow.calls.filter((call) => call[0].startsWith("library-")))).map((call) => call[0]), ["library-sources", "library-search", "library-select", "library-preview", "library-confirm"]);
  const inventory = page.getByRole("button", { name: "Inventory" });
  await inventory.click();
  await page.waitForFunction(() => window.desktopWorkflow.calls.some((call) => call[0] === "select" && call[1] === "view:table"));
  assert.equal(await inventory.getAttribute("aria-current"), "page");
  assert.equal(await page.getByRole("heading", { name: "Desktop roadmap" }).evaluate((element) => element === document.activeElement), true);

  const product = page.getByRole("button", { name: "Product", exact: true });
  await product.focus();
  await page.keyboard.press("Enter");
  assert.deepEqual(await page.evaluate(() => window.desktopWorkflow.calls.at(-1)), ["viewer", "node:one"]);

  await page.getByRole("button", { name: "3D" }).click();
  const threeCanvas = page.getByRole("img", { name: /Diagram 3D view/ });
  await threeCanvas.waitFor();
  await page.waitForFunction(() => document.querySelector(".ld-desktop-viewer-three-canvas")?.dataset.renderReady === "true");
  assert.equal(await threeCanvas.evaluate((canvas) => {
    const gl = canvas.getContext("webgl2") ?? canvas.getContext("webgl");
    return gl !== null && !gl.isContextLost() && gl.drawingBufferWidth > 0 && String(gl.getParameter(gl.VERSION)).includes("WebGL");
  }), true);
  assert.equal(await threeCanvas.getAttribute("data-relation-count"), "1");
  assert.equal(await threeCanvas.getAttribute("data-cross-layer-relation-count"), "1");
  const originalCamera = await threeCanvas.getAttribute("data-camera");
  const canvasBounds = await threeCanvas.boundingBox();
  assert.ok(canvasBounds);
  await page.mouse.move(canvasBounds.x + canvasBounds.width * .4, canvasBounds.y + canvasBounds.height * .4);
  await page.mouse.down();
  await page.mouse.move(canvasBounds.x + canvasBounds.width * .65, canvasBounds.y + canvasBounds.height * .55, { steps: 6 });
  await page.mouse.up();
  await page.waitForFunction((previous) => document.querySelector(".ld-desktop-viewer-three-canvas")?.dataset.camera !== previous, originalCamera);
  await page.getByRole("button", { name: "Delivery", exact: true }).focus();
  await page.keyboard.press("Enter");
  assert.deepEqual(await page.evaluate(() => window.desktopWorkflow.calls.at(-1)), ["viewer", "node:two"]);
  await page.getByRole("button", { name: "2D" }).click();
  await page.getByRole("group", { name: "diagram view" }).waitFor();
  for (const profile of [
    { probe_id: "browser-2d", viewer_mode: "2d", theme: "light", zoom_percent: 100, screen_reader: true, keyboard_only: true, reduced_motion: false, window_width: 1440, window_height: 900 },
    { probe_id: "browser-zoom-2d", viewer_mode: "2d", theme: "light", zoom_percent: 200, screen_reader: true, keyboard_only: true, reduced_motion: true, window_width: 1440, window_height: 900 },
    { probe_id: "browser-2.5d", viewer_mode: "2.5d", theme: "dark", zoom_percent: 100, screen_reader: true, keyboard_only: true, reduced_motion: true, window_width: 1440, window_height: 900 },
  ]) {
    await page.evaluate((value) => {
      document.documentElement.dataset.theme = value.theme;
      document.documentElement.dataset.zoomed = String(value.zoom_percent >= 175);
      document.documentElement.style.zoom = `${value.zoom_percent}%`;
    }, profile);
    const report = await page.evaluate((value) => window.desktopWorkflow.audit(value), profile);
    assert.equal(report.labels_complete, true);
    assert.equal(report.screen_reader_semantics, true);
    assert.equal(report.focus_order_valid, true, String(report.focus_order_failures));
    assert.equal(report.keyboard_workflow_valid, true);
    assert.ok(report.minimum_contrast >= 4.5, `contrast ${report.minimum_contrast}`);
    assert.equal(report.zoom_layout_valid, true);
    assert.equal(report.viewer_mode, profile.viewer_mode);
    assert.equal(report.viewer_keyboard_selection, true);
    assert.ok(report.viewer_item_count >= 2);
    assert.ok(report.viewer_relation_count >= 1);
    if (profile.viewer_mode === "2.5d") {
      assert.equal(report.renderer_backend, "three.js");
      assert.equal(report.webgl_verified, true);
      assert.equal(report.reduced_motion_honored, true);
      assert.equal(report.viewer_cross_layer_relation_count, 1);
    } else {
      assert.equal(report.renderer_backend, "svg");
    }
  }
  await page.evaluate(() => { document.documentElement.dataset.theme = "light"; document.documentElement.dataset.zoomed = "false"; document.documentElement.style.zoom = "100%"; });
  await page.getByRole("button", { name: "2D" }).click();

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
