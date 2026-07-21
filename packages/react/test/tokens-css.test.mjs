// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { generateTokensCss, readTokens, TOKENS_CSS_PATH } from "../scripts/generate-tokens-css.mjs";

test("committed tokens.css is in sync with brand/tokens.json", () => {
  const committed = readFileSync(TOKENS_CSS_PATH, "utf8");
  const regenerated = generateTokensCss(readTokens());
  assert.equal(
    committed,
    regenerated,
    "src/tokens.css is stale. Run `pnpm --filter @layerdraw/react generate`.",
  );
});

test("semantic tokens resolve to primitive vars and never expose raw hex look-tokens", () => {
  const css = generateTokensCss(readTokens());
  // Light default lives at :root; themes override by attribute.
  assert.match(css, /:root\s*\{/);
  assert.match(css, /:root\[data-theme="dark"\]\s*\{/);
  assert.match(css, /:root\[data-theme="high-contrast"\]\s*\{/);
  // The primary action background is a token reference, not a raw hex.
  assert.match(css, /--ld-action-primary-background: var\(--ld-color-violet-700\);/);
  // Radius tokens are capped at the workbench scale (4/6/8px + xs/full).
  assert.match(css, /--ld-radius-sm: 4px;/);
  assert.match(css, /--ld-radius-md: 6px;/);
  assert.match(css, /--ld-radius-lg: 8px;/);
  // Shadow composites collapse to a single box-shadow value.
  assert.match(css, /--ld-shadow-menu: 0px 4px 12px 0px #0D0F1329;/);
  // Font family arrays quote multi-word names.
  assert.match(css, /--ld-font-family-sans: Inter, "Noto Sans JP", system-ui, sans-serif;/);
});
