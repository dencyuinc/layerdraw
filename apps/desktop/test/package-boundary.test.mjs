// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { readFile, readdir } from "node:fs/promises";
import test from "node:test";

test("Desktop frontend composes typed packages without native path, transport discovery, or owner semantics", async () => {
  const names = (await readdir(new URL("../src", import.meta.url))).filter((name) => name.endsWith(".ts"));
	const trustedOwner = await readFile(new URL("../src/wails-project-owner.ts", import.meta.url), "utf8");
  const source = (await Promise.all(names.filter((name) => name !== "wails-project-owner.ts").map((name) => readFile(new URL(`../src/${name}`, import.meta.url), "utf8")))).join("\n");
  assert.doesNotMatch(source, /window\.__WAILS__|window\.go\.|createBrowserEditor|OpenDesktopNativeEndpoint|native[_ .-]?path|readFile|writeFile/);
  assert.doesNotMatch(source, /parseLDL|source[_ -]?rewrite|commit_operations\s*\(|evaluateAuthoring|resolveRegistry|planExport/);
  assert.match(source, /DesktopProjectLifecyclePort/);
  assert.match(source, /BrowserEditor/);
  assert.match(source, /Viewer/);
	assert.match(trustedOwner, /createWailsDesktopClient/);
	assert.match(trustedOwner, /createBrowserEditor/);
	assert.match(trustedOwner, /createViewer/);
	assert.doesNotMatch(trustedOwner, /window\.__WAILS__|window\.go\.|native[_ .-]?path|readFile|writeFile|parseLDL|source[_ -]?rewrite/);
});

test("Desktop styles have explicit wide, narrow, focus, and reduced-motion behavior", async () => {
  const styles = await readFile(new URL("../dist/styles.css", import.meta.url), "utf8");
  assert.match(styles, /grid-template-columns: minmax\(11rem, 15rem\).*minmax\(15rem, 20rem\)/);
  assert.match(styles, /@media \(max-width: 42rem\)/);
  assert.match(styles, /:focus/);
  assert.match(styles, /prefers-reduced-motion: reduce/);
});

test("Desktop declares and packages its complete normative frontend closure", async () => {
  const manifest = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8"));
  for (const dependency of ["@layerdraw/client-sdk", "@layerdraw/composer", "@layerdraw/engine-client", "@layerdraw/export", "@layerdraw/library", "@layerdraw/protocol", "@layerdraw/react", "@layerdraw/registry-client", "@layerdraw/render", "@layerdraw/review", "@layerdraw/viewer"]) {
    assert.equal(manifest.dependencies[dependency], "workspace:*", dependency);
  }
  const tsconfig = JSON.parse(await readFile(new URL("../tsconfig.json", import.meta.url), "utf8"));
  assert.ok(tsconfig.include.includes("frontend/src/**/*.ts"));
  const installer = await readFile(new URL("../../../tools/build-desktop-installer.sh", import.meta.url), "utf8");
  assert.match(installer, /export registry-client library review react desktop/);
});

test("normal packaged startup injects the DTO-only project, Registry, and Review owners", async () => {
	const main = await readFile(new URL("../frontend/src/main.ts", import.meta.url), "utf8");
	assert.match(main, /createDesktopWailsProjectOwner\(\{ ProjectPublication, PreviewEditor, MaterializeProjectView \}.*generatedBindings\)/);
	assert.match(main, /project,\s*registry: \{ RegistryDispatch \},\s*review:/s);
	assert.match(main, /if \(await isPackagedProbeMode\(\)\)/);
});
