// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import {
  ComposerContractError,
  retainComposerPresentation,
  toEditorOperationRequest,
} from "../dist/index.js";

const fixture = JSON.parse(await readFile(new URL("../../../schemas/fixtures/engine/workbench-preview-operations-request.json", import.meta.url), "utf8"));

test("semantic UI intent forwards the validated Engine operation request unchanged", () => {
  const request = fixture.payload;
  assert.equal(toEditorOperationRequest({ kind: "semantic_operations", request }), request);
});

test("a mismatched intent kind fails before a request reaches Engine", () => {
  assert.throws(
    () => toEditorOperationRequest({ kind: "source_patch", request: fixture.payload }),
    (error) => error instanceof ComposerContractError && error.code === "composer.invalid_editor_edit",
  );
});

test("presentation state retains authoritative preview, decision, and grant objects", () => {
  const preview = { status: "invalid" };
  const decision = { outcome: "deny" };
  const grant = { granted_capabilities: [] };
  const state = retainComposerPresentation(preview, {
    authoring_decision: decision,
    grant_summary: grant,
  });
  assert.equal(state.preview, preview);
  assert.equal(state.authoring_decision, decision);
  assert.equal(state.grant_summary, grant);
});

test("presentation state omits unavailable optional access context", () => {
  assert.deepEqual(retainComposerPresentation({ status: "invalid" }), {
    preview: { status: "invalid" },
  });
});
