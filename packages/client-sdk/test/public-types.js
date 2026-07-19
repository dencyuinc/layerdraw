// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0
editor.open({ authority: "engine", input: {} });
editor.preview({ kind: "semantic_operations", request: {} });
editor.apply({ kind: "fragment", request: {} });
editor.materializeView({});
editor.close();
factory({});
if (applyResult.persistence === "durable") {
    applyResult.committed_revision.revision_id;
}
else {
    const noCommittedRevision = applyResult.committed_revision;
    void noCommittedRevision;
}
export {};
//# sourceMappingURL=public-types.js.map