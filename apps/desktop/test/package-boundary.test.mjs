// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { readFile, readdir } from "node:fs/promises";
import test from "node:test";

test("Desktop frontend composes typed packages without native path, transport discovery, or owner semantics", async () => {
  const names = (await readdir(new URL("../src", import.meta.url))).filter((name) => name.endsWith(".ts"));
  const source = (await Promise.all(names.map((name) => readFile(new URL(`../src/${name}`, import.meta.url), "utf8")))).join("\n");
  assert.doesNotMatch(source, /window\.__WAILS__|window\.go\.|createBrowserEditor|OpenDesktopNativeEndpoint|native[_ .-]?path|readFile|writeFile/);
  assert.doesNotMatch(source, /parseLDL|source[_ -]?rewrite|commit_operations\s*\(|evaluateAuthoring|resolveRegistry|planExport/);
  assert.match(source, /DesktopProjectLifecyclePort/);
  assert.match(source, /BrowserEditor/);
  assert.match(source, /Viewer/);
});

test("Desktop styles have explicit wide, narrow, focus, and reduced-motion behavior", async () => {
  const styles = await readFile(new URL("../dist/styles.css", import.meta.url), "utf8");
  assert.match(styles, /grid-template-columns: minmax\(11rem, 15rem\).*minmax\(15rem, 20rem\)/);
  assert.match(styles, /@media \(max-width: 42rem\)/);
  assert.match(styles, /:focus/);
  assert.match(styles, /prefers-reduced-motion: reduce/);
});
