// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { chromium } from "@playwright/test";

const browser = await chromium.launch({ headless: true });
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  await page.setContent(`
    <main class="ld-editor-shell" style="height:900px">
      <div class="ld-editor-toolbar" role="toolbar" aria-label="Editing commands">
        <button data-layerdraw-command="undo">Undo</button>
        <button data-layerdraw-command="redo">Redo</button>
      </div>
      <div class="ld-editor-workspace">
        <section class="ld-editor-panel" data-placement="primary" aria-label="Canvas"></section>
        <section class="ld-editor-panel" data-placement="inspector" aria-label="Inspector"></section>
      </div>
      <div class="ld-visually-hidden" role="status" aria-live="polite">Preview ready.</div>
    </main>`);
  await page.addStyleTag({ path: new URL("../dist/styles.css", import.meta.url).pathname });
  const desktop = await page.locator(".ld-editor-workspace").evaluate((element) => getComputedStyle(element).gridTemplateColumns);
  assert.ok(desktop.split(" ").length >= 2, "desktop keeps the inspector beside the workspace");
  assert.equal(await page.getByRole("status").textContent(), "Preview ready.");
  await page.getByRole("button", { name: "Undo" }).focus();
  assert.equal(await page.getByRole("button", { name: "Undo" }).evaluate((element) => element === document.activeElement), true);

  await page.setViewportSize({ width: 390, height: 844 });
  const mobile = await page.locator(".ld-editor-workspace").evaluate((element) => getComputedStyle(element).gridTemplateColumns);
  assert.equal(mobile.split(" ").length, 1, "mobile stacks the inspector below the workspace");
  const inspectorBorder = await page.getByRole("region", { name: "Inspector" }).evaluate((element) => ({
    inline: getComputedStyle(element).borderInlineStartWidth,
    block: getComputedStyle(element).borderBlockStartWidth,
  }));
  assert.equal(inspectorBorder.inline, "0px");
  assert.notEqual(inspectorBorder.block, "0px");

  await page.emulateMedia({ reducedMotion: "reduce" });
  const motion = await page.getByRole("button", { name: "Undo" }).evaluate((element) => ({
    animation: getComputedStyle(element).animationDuration,
    transition: getComputedStyle(element).transitionDuration,
  }));
  assert.equal(Number.parseFloat(motion.animation), 0.00001);
  assert.equal(Number.parseFloat(motion.transition), 0.00001);
  console.log("React editor browser E2E passed at 1440x900 and 390x844 with reduced motion.");
} finally {
  await browser.close();
}
