// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";
import {
  Button,
  cn,
  DialogBackdrop,
  DialogPopup,
  DialogRoot,
  DropdownMenuItem,
  DropdownMenuPopup,
  DropdownMenuRoot,
  Input,
  SelectItem,
  SelectPopup,
  SelectRoot,
  SelectTrigger,
  Tab,
  TabsList,
  TabsRoot,
} from "../dist/primitives/index.js";

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

test("cn joins present class fragments only", () => {
  assert.equal(cn("a", undefined, "b", false, "", "c"), "a b c");
  assert.equal(cn(), "");
});

test("Button variants restyle the Base UI primitive with token-mapped utilities only", async () => {
  for (const [variant, expected] of [
    ["primary", "bg-primary"],
    ["secondary", "bg-secondary-bg"],
    ["ghost", "hover:bg-ghost-hover"],
    ["danger", "bg-danger"],
    ["danger-quiet", "border-danger-border"],
  ]) {
    let renderer;
    await act(async () => {
      renderer = TestRenderer.create(React.createElement(Button, { variant }, "Act"));
    });
    const button = renderer.root.findByType("button");
    assert.match(button.props.className, new RegExp(expected));
    assert.match(button.props.className, /rounded-md/);
    assert.doesNotMatch(button.props.className, /slate|zinc|gray-\d/);
    await act(async () => renderer.unmount());
  }
});

test("Input renders the compact token-styled control and merges custom classes", async () => {
  let renderer;
  await act(async () => {
    renderer = TestRenderer.create(React.createElement(Input, { className: "extra", placeholder: "x" }));
  });
  const input = renderer.root.findByType("input");
  assert.match(input.props.className, /rounded-sm/);
  assert.match(input.props.className, /border-line/);
  assert.match(input.props.className, /extra$/);
  await act(async () => renderer.unmount());
});

test("Tabs compose as the segmented control with selected-state styling", async () => {
  let renderer;
  await act(async () => {
    renderer = TestRenderer.create(
      React.createElement(TabsRoot, { value: "b" },
        React.createElement(TabsList, { "aria-label": "Mode" },
          React.createElement(Tab, { value: "a" }, "A"),
          React.createElement(Tab, { value: "b" }, "B"))),
    );
  });
  const tabs = renderer.root.findAllByProps({ role: "tab" });
  assert.equal(tabs.length, 2);
  assert.match(tabs[0].props.className, /aria-selected:bg-surface/);
  const list = renderer.root.findByProps({ role: "tablist" });
  assert.match(list.props.className, /bg-subtle/);
  await act(async () => renderer.unmount());
});

test("overlay primitives are exported and closed roots render no popup", async () => {
  let renderer;
  await act(async () => {
    renderer = TestRenderer.create(
      React.createElement(DialogRoot, { open: false }, React.createElement(DialogBackdrop, null)),
    );
  });
  assert.doesNotMatch(JSON.stringify(renderer.toJSON() ?? null), /popup/);
  await act(async () => renderer.unmount());
  for (const primitive of [DialogPopup, DropdownMenuRoot, DropdownMenuPopup, DropdownMenuItem, SelectRoot, SelectTrigger, SelectPopup, SelectItem]) {
    assert.ok(typeof primitive === "function" || typeof primitive === "object");
  }
});

test("emitted primitives stylesheet resolves exclusively through token variables", async () => {
  const css = await readFile(new URL("../dist/primitives.css", import.meta.url), "utf8");
  assert.match(css, /--color-primary: var\(--ld-action-primary-background\)/);
  assert.match(css, /--radius-md: var\(--ld-radius-md\)/);
  assert.doesNotMatch(css, /slate-\d|zinc-\d|gray-\d/);
  // No raw palette hex values; only Tailwind's transparent shadow placeholders
  // (0 0 #0000) and the unused ring-offset initial value may appear.
  const hexes = [...css.matchAll(/#[0-9a-fA-F]{3,8}/gu)].map((match) => match[0]);
  assert.ok(hexes.every((hex) => hex === "#0000" || hex === "#fff"), `unexpected raw colors: ${hexes.join(", ")}`);
});
