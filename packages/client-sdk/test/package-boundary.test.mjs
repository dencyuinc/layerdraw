// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { readFile, readdir } from "node:fs/promises";
import test from "node:test";

test("client SDK editor contract has no React, Wails, Registry, MCP, server, or realtime dependency", async () => {
  const manifest = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8"));
  assert.deepEqual(Object.keys(manifest.exports), [".", "./editor", "./browser-editor"]);
  const files = (await readdir(new URL("../src", import.meta.url))).filter((name) => name.endsWith(".ts"));
  const source = (await Promise.all(files.map((name) => readFile(new URL(`../src/${name}`, import.meta.url), "utf8")))).join("\n");
  assert.doesNotMatch(source, /from\s+["'](?:react|node:|@wails|@layerdraw\/(?:registry|mcp|server))/);
  assert.doesNotMatch(source, /\b(?:window|localStorage|sessionStorage)\b|\bdocument\s*(?:\.|\[)/);
});

test("public declaration exposes lifecycle methods and persistence-discriminated apply results", async () => {
  const declaration = await readFile(new URL("../dist/editor.d.ts", import.meta.url), "utf8");
  for (const method of ["open", "preview", "apply", "materializeView", "close"])
    assert.match(declaration, new RegExp(`${method}\\(`));
  assert.match(declaration, /persistence: "ephemeral"/);
  assert.match(declaration, /persistence: "durable"/);
  assert.match(declaration, /committed_revision\?: never/);
  assert.match(declaration, /status: "needs_review" \| "rejected"/);
  assert.match(declaration, /status: "committed" \| "committed_external_failed" \| "committed_external_pending" \| "committed_state_stale"/);
});
