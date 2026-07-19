// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import {
  buildEntityEdit, buildFragmentEdit, buildQueryEdit, buildRelationEdit,
  buildRowEdit, buildSemanticEdit, buildSourcePatchEdit, buildViewEdit,
  ComposerContractError,
} from "../dist/index.js";

const operations = JSON.parse(await readFile(new URL("../../../schemas/fixtures/engine/workbench-preview-operations-request.json", import.meta.url), "utf8")).payload;
const sourcePatch = JSON.parse(await readFile(new URL("../../../schemas/fixtures/engine/workbench-preview-source-patch-request.json", import.meta.url), "utf8")).payload;

test("semantic builder emits the generated protocol request without interpreting it", () => {
  const edit = buildSemanticEdit(operations.batch.operations, {
    limits: operations.limits,
    preconditions: operations.preconditions,
  });
  assert.equal(edit.kind, "semantic_operations");
  assert.deepEqual(edit.request, operations);
});

test("source patch builder validates and forwards Engine-owned source edits", () => {
  assert.deepEqual(buildSourcePatchEdit(sourcePatch), { kind: "source_patch", request: sourcePatch });
  assert.throws(() => buildSourcePatchEdit({}), ComposerContractError);
});

test("typed entity, relation, row, query, and view entry points share protocol validation", () => {
  const context = { limits: operations.limits, preconditions: operations.preconditions };
  const operation = operations.batch.operations[0];
  for (const builder of [buildEntityEdit, buildRelationEdit, buildRowEdit, buildQueryEdit, buildViewEdit]) {
    assert.deepEqual(builder(operation, context).request, operations);
  }
  assert.throws(() => buildSemanticEdit([], context), ComposerContractError);
});

test("fragment builder accepts a generated-contract fragment and rejects malformed input", () => {
  const request = {
    fragment: {
      allowed_kinds: ["entity"],
      fragment_blob: sourcePatch.patch.patches[0].replacement_blob,
      insertion_owner: "ldl:project:fixture",
      intent: "insert",
    },
    limits: sourcePatch.limits,
    preconditions: sourcePatch.preconditions,
  };
  assert.deepEqual(buildFragmentEdit(request), { kind: "fragment", request });
  assert.throws(() => buildFragmentEdit({}), ComposerContractError);
});
