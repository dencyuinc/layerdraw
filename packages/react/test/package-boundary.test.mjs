// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { readFile, readdir } from "node:fs/promises";
import test from "node:test";

test("package exposes composable React-only entrypoints and no product shell dependency", async () => {
  const manifest = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8"));
  assert.deepEqual(Object.keys(manifest.exports), [".", "./provider", "./controls", "./layout", "./navigation", "./query-viewer", "./recovery", "./styles.css"]);
  const files = (await readdir(new URL("../src", import.meta.url))).filter((name) => name.endsWith(".ts"));
  const source = (await Promise.all(files.map((name) => readFile(new URL(`../src/${name}`, import.meta.url), "utf8")))).join("\n");
  assert.doesNotMatch(source, /createBrowserEditor|@wails|native.file|from\s+["'](?:node:|@layerdraw\/(?:engine-client|registry-client|server-client|mcp-client))/);
  assert.doesNotMatch(source, /required_capabilities\.includes|missing_capabilities\.includes|schema:write|graph:write/);
  assert.doesNotMatch(source, /parseLDL|parseLdl|rewriteSource|synthesizeIdentity/);
});

test("navigation declarations require Engine-owned stable identities and Composer edits", async () => {
  const declaration = await readFile(new URL("../dist/navigation.d.ts", import.meta.url), "utf8");
  assert.match(declaration, /Pick<SymbolReadItem, "address" \| "display_name" \| "kind" \| "source_range">/);
  assert.match(declaration, /buildEdit\?: \(draft: string\) => EditorEdit/);
  assert.match(declaration, /identityReplacements\?: ReadonlyMap<StableAddress, StableAddress>/);
  assert.doesNotMatch(declaration, /(?:sourceText|rawSource|parseLDL)\??:/);
});

test("responsive and reduced-motion rules cover documented desktop and mobile shells", async () => {
  const styles = await readFile(new URL("../dist/styles.css", import.meta.url), "utf8");
  assert.match(styles, /grid-template-columns: minmax\(0, 1fr\) minmax/);
  assert.match(styles, /@container \(max-width: 48rem\)/);
  assert.match(styles, /@media \(max-width: 30rem\)/);
  assert.match(styles, /prefers-reduced-motion: reduce/);
  assert.match(styles, /scroll-behavior: auto !important/);
});

test("public declarations expose the editor state and hook surface", async () => {
  const declaration = await readFile(new URL("../dist/provider.d.ts", import.meta.url), "utf8");
  for (const name of ["useEditorSession", "useEditorPreview", "useEditorDiagnostics", "useEditorImpact", "useEditorGrant", "useEditorDecision", "useEditorConflicts", "useEditorCapabilities"])
    assert.match(declaration, new RegExp(`function ${name}`));
  assert.match(declaration, /editor: BrowserEditor/);
  assert.doesNotMatch(declaration, /BrowserEditorOptions/);
});
