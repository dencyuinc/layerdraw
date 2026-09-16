// SPDX-License-Identifier: Apache-2.0

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { encodeViewData, decodeViewData, isTreeOccurrence } from "../dist/semantic.gen.js";

const corpus = JSON.parse(await readFile(new URL("../../../tests/conformance/testdata/viewdata_conformance_v1.json", import.meta.url), "utf8"));
const views = corpus.cases.flatMap((item) => item.expected?.normalized_response?.payload?.view_data ?? []);

test("encoders produce canonical ViewData without parsing their own output", (t) => {
  const encoded = [];
  const parse = t.mock.method(JSON, "parse", () => { throw new Error("encoder parsed its output"); });
  try {
    for (const view of views) encoded.push(encodeViewData(view));
    assert.equal(parse.mock.callCount(), 0);
  } finally { parse.mock.restore(); }
  assert.ok(encoded.length >= 7);
  encoded.forEach((value, index) => {
    assert.deepEqual(decodeViewData(value), views[index]);
    assert.equal(encodeViewData(decodeViewData(value)), value);
  });
});

test("recursive shape validation does not rerun wire preflight on each subtree", () => {
  const root = structuredClone(views.find((view) => view.kind === "tree").tree.roots[0]);
  root.children = [];
  let visits = 0;
  const leaf = new Proxy(root, { getPrototypeOf(target) { visits++; return Reflect.getPrototypeOf(target); } });
  assert.equal(isTreeOccurrence(leaf), true);
  visits = 0;
  assert.equal(isTreeOccurrence({ ...root, children: [leaf] }), true);
  const shallowVisits = visits;
  let nested = leaf;
  for (let depth = 0; depth < 30; depth++) nested = { ...root, children: [nested] };
  visits = 0;
  assert.equal(isTreeOccurrence(nested), true);
  assert.equal(visits, shallowVisits, "leaf work must not grow with ancestor count");
});
