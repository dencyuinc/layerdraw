// SPDX-License-Identifier: Apache-2.0

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  collectCompileInputBlobRefs,
  collectCompileResultBlobRefs,
  decodeCompileResult,
  decodeCompileRequestEnvelope,
  decodeCompileResponseEnvelope,
  decodeCanonicalSourcePath,
  decodeEffectiveResourceLimits,
  decodeExportRecipeBlobRef,
  decodeHandshakeRequestEnvelope,
  decodeNormalizedPackArtifactBlobRef,
  decodeNormalizedPackCanonicalBlobRef,
  decodeNormalizedProjectArtifactBlobRef,
  decodeNormalizedProjectCanonicalBlobRef,
  decodeQueryRecipeBlobRef,
  decodeViewRecipeBlobRef,
  decodeHandshakeResponseEnvelope,
  encodeCompileRequestEnvelope,
  encodeCompileResponseEnvelope,
  encodeNormalizedPackArtifact,
  encodeNormalizedProjectArtifact,
  encodeCanonicalSourcePath,
  encodeEffectiveResourceLimits,
  encodeExportRecipeBlobRef,
  encodeHandshakeRequestEnvelope,
  encodeNormalizedPackArtifactBlobRef,
  encodeNormalizedPackCanonicalBlobRef,
  encodeNormalizedProjectArtifactBlobRef,
  encodeNormalizedProjectCanonicalBlobRef,
  encodeQueryRecipeBlobRef,
  encodeViewRecipeBlobRef,
  encodeHandshakeResponseEnvelope,
  isCompileRequestEnvelope,
  isCompileResponseEnvelope,
  isCloseDocumentResponseEnvelope,
  isClassifyAuthoringImpactInput,
  isApplyToHandleInput,
  isApplyToHandleResult,
  isBoundedTextChunk,
  isCloseDocumentInput,
  isHandshakeRequestEnvelope,
  isHandshakeResponseEnvelope,
  decodePreviewOperationsRequestEnvelope,
  decodePreviewOperationsResponseEnvelope,
  encodePreviewOperationsRequestEnvelope,
  encodePreviewOperationsResponseEnvelope,
  decodeSemanticOperationBatch,
  encodeSemanticOperationBatch,
  isFindUsagesResult,
  isFindSymbolsInput,
  isEngineEditPreconditions,
  isInspectSubgraphResult,
  isInspectSubgraphInput,
  isListModulesResult,
  isOpenDocumentResult,
  isOpenDocumentResponseEnvelope,
  isReadDeclarationsResult,
  isReplaceSourceTreeResult,
  isResultingHashes,
  isSemanticOperation,
  isSemanticOperationBatch,
  isSourceDiff,
  isWorkbenchPreviewResult,
  isPreviewOperationsRequestEnvelope,
  isPreviewOperationsResponseEnvelope,
  isPreviewSourcePatchRequestEnvelope,
} from "../dist/engine.gen.js";
import {
  decodeBlobRef,
  decodeCanonicalInt64,
  decodeCanonicalNonNegativeInt64,
  decodeCanonicalNonNegativeSafeInteger,
  decodeCanonicalPositiveInt64,
  decodeCanonicalPositiveSafeInteger,
  decodeCanonicalSafeInteger,
  decodeCanonicalUint64,
  decodeByteResourceLimitCapability,
  decodeCapabilityID,
  decodeDigest,
  decodeEndpointInstanceID,
  decodeHandshakeRequest,
  decodeJsonValue,
  decodeOperationCapability,
  decodeManifestETag,
  decodeProtocolOffer,
  decodeProtocolVersion,
  decodeProtocolVersionOrRange,
  decodeProtocolVersionRange,
  decodeRequestedCapabilityStatus,
  decodeReleaseVersion,
  decodeRfc3339Time,
  decodeTotalItems,
  decodeUpgradeDiagnosticData,
  encodeBlobRef,
  encodeCanonicalInt64,
  encodeCanonicalNonNegativeInt64,
  encodeCanonicalNonNegativeSafeInteger,
  encodeCanonicalPositiveInt64,
  encodeCanonicalPositiveSafeInteger,
  encodeCanonicalSafeInteger,
  encodeCanonicalUint64,
  encodeByteResourceLimitCapability,
  encodeCapabilityID,
  encodeDigest,
  encodeEndpointInstanceID,
  encodeHandshakeRequest,
  encodeJsonValue,
  encodeOperationCapability,
  encodeManifestETag,
  encodeProtocolOffer,
  encodeProtocolVersion,
  encodeProtocolVersionOrRange,
  encodeProtocolVersionRange,
  encodeRequestedCapabilityStatus,
  encodeReleaseVersion,
  encodeRfc3339Time,
  encodeTotalItems,
  encodeUpgradeDiagnosticData,
  encodeExtensions,
  encodeJsonObject,
  isExtensions,
  isJsonObject,
  isJsonValue,
  isOperationCapability,
  maxWireJSONBytes,
  maxWireJSONDepth,
} from "../dist/common.gen.js";
import {
  isAuthoringImpact,
  isSemanticDiff,
  decodeChildSetHash,
  decodeCanonicalFiniteDecimal,
  decodeCanonicalPositiveFiniteDecimal,
  decodeColor,
  decodeCompiledExportRecipeDocument,
  decodeCompiledQueryRecipeDocument,
  decodeCompiledViewRecipeDocument,
  decodeDiagnostic,
  decodeDiagnosticArgumentValue,
  decodeEntityTypeColumnAddress,
  decodeExportDimension,
  decodeExportOptions,
  decodeExportRecipe,
  decodePackRootAddress,
  decodeParameterAddress,
  decodeProjectRootAddress,
  decodeQueryAddress,
  decodeQueryRecipe,
  decodeQueryRecipeDependencies,
  decodeQueryRecipeParameter,
  decodeQueryRecipeSelect,
  decodeQueryRecipeTraversal,
  decodeRasterBackground,
  decodeRecipeScalar,
  decodeRecipePredicateValue,
  decodeRecipePredicate,
  decodeRecipeRowPredicate,
  decodeRelationTypeColumnAddress,
  decodeSearchField,
  decodeSemanticIndex,
  decodeSourceBindingRecord,
  decodeSourceMap,
  decodeSourceSpan,
  decodeStableAddress,
  decodeViewAddress,
  decodeViewDiagramProjection,
  decodeViewDiagramShape,
  decodeViewExportAddress,
  decodeViewFlowProjection,
  decodeViewFlowShape,
  decodeViewMatrixAxis,
  decodeViewMatrixCell,
  decodeViewPlacement,
  decodeViewRecipe,
  decodeViewRecipeDependencies,
  decodeViewRecipeSource,
  decodeViewRenderSet,
  decodeViewTableColumn,
  decodeViewTableColumnSource,
  decodeViewTableProjection,
  decodeViewTableShape,
  decodeViewTreeShape,
  encodeCanonicalFiniteDecimal,
  encodeCanonicalPositiveFiniteDecimal,
  encodeColor,
  encodeCompiledExportRecipeDocument,
  encodeCompiledQueryRecipeDocument,
  encodeCompiledViewRecipeDocument,
  encodeDiagnostic,
  encodeDiagnosticArgumentValue,
  encodeEntityTypeColumnAddress,
  encodeExportDimension,
  encodeExportOptions,
  encodeExportRecipe,
  encodePackRootAddress,
  encodeParameterAddress,
  encodeProjectRootAddress,
  encodeQueryAddress,
  encodeQueryRecipe,
  encodeQueryRecipeDependencies,
  encodeQueryRecipeParameter,
  encodeQueryRecipeSelect,
  encodeQueryRecipeTraversal,
  encodeRasterBackground,
  encodeRecipePredicateValue,
  encodeRecipePredicate,
  encodeRecipeRowPredicate,
  encodeRelationTypeColumnAddress,
  encodeSearchField,
  encodeSourceSpan,
  encodeStableAddress,
  encodeViewAddress,
  encodeViewDiagramProjection,
  encodeViewDiagramShape,
  encodeViewExportAddress,
  encodeViewFlowProjection,
  encodeViewFlowShape,
  encodeViewMatrixAxis,
  encodeViewMatrixCell,
  encodeViewPlacement,
  encodeViewRecipe,
  encodeViewRecipeDependencies,
  encodeViewRecipeSource,
  encodeViewRenderSet,
  encodeViewTableColumn,
  encodeViewTableColumnSource,
  encodeViewTableProjection,
  encodeViewTableShape,
  encodeViewTreeShape,
  isDiagnosticArgumentValue,
  isRecipePredicate,
  isRecipeRowPredicate,
} from "../dist/semantic.gen.js";

const fixtureRoot = new URL("../../../schemas/fixtures/engine/", import.meta.url);
const commonFixtureRoot = new URL("../../../schemas/fixtures/common/", import.meta.url);
const conformanceCorpusURL = new URL("../../../schemas/fixtures/conformance/v1.json", import.meta.url);
const formatCorpusURL = new URL("../../../schemas/fixtures/conformance/formats-v1.json", import.meta.url);
const exportOptionsCorpusURL = new URL("../../../schemas/fixtures/conformance/export-options-v1.json", import.meta.url);
const predicateCorpusURL = new URL("../../../schemas/fixtures/conformance/predicates-v1.json", import.meta.url);
const viewSourceCorpusURL = new URL("../../../schemas/fixtures/conformance/view-sources-v1.json", import.meta.url);
const viewExportSemanticsCorpusURL = new URL("../../../schemas/fixtures/conformance/view-export-semantics-v1.json", import.meta.url);
const queryAuthorityCorpusURL = new URL("../../../schemas/fixtures/conformance/query-authority-v1.json", import.meta.url);
const semanticRootAuthorityCorpusURL = new URL("../../../schemas/fixtures/conformance/semantic-root-authority-v1.json", import.meta.url);
const unicodeScalarCorpusURL = new URL("../../../schemas/fixtures/conformance/unicode-scalars-v1.json", import.meta.url);
const canonicalEngineRoot = new URL("../../../schemas/fixtures/conformance/engine/", import.meta.url);

async function readFixture(name) {
  return JSON.parse(await readFile(new URL(name, fixtureRoot), "utf8"));
}

for (const [name, validate, decode, encode] of [
  ["compile-request.json", isCompileRequestEnvelope, decodeCompileRequestEnvelope, encodeCompileRequestEnvelope],
  ["compile-success.json", isCompileResponseEnvelope, decodeCompileResponseEnvelope, encodeCompileResponseEnvelope],
  ["compile-success-pack.json", isCompileResponseEnvelope, decodeCompileResponseEnvelope, encodeCompileResponseEnvelope],
  ["compile-rejected.json", isCompileResponseEnvelope, decodeCompileResponseEnvelope, encodeCompileResponseEnvelope],
  ["handshake-request.json", isHandshakeRequestEnvelope, decodeHandshakeRequestEnvelope, encodeHandshakeRequestEnvelope],
  ["handshake-success.json", isHandshakeResponseEnvelope, decodeHandshakeResponseEnvelope, encodeHandshakeResponseEnvelope],
  ["handshake-rejected.json", isHandshakeResponseEnvelope, decodeHandshakeResponseEnvelope, encodeHandshakeResponseEnvelope],
]) {
  test(`${name} validates and canonically round-trips without loss`, async () => {
    const text = await readFile(new URL(name, fixtureRoot), "utf8");
    const fixture = JSON.parse(text);
    assert.equal(validate(fixture), true);
    const canonical = encode(decode(text));
    assert.deepEqual(JSON.parse(canonical), fixture);
    assert.equal(encode(decode(canonical)), canonical);
  });
}

test("review-repair handshake fixtures validate and byte-identically round-trip", async (context) => {
  for (const name of [
    "handshake-major-mismatch-request.json",
    "handshake-range-mismatch-request.json",
    "handshake-range-mismatch-response.json",
    "handshake-schema-digest-mismatch-request.json",
    "handshake-schema-digest-mismatch-response.json",
    "handshake-required-capability-missing-request.json",
    "handshake-required-capability-missing-response.json",
    "handshake-unknown-optional-request.json",
    "handshake-unknown-optional-response.json",
    "handshake-client-limits-request.json",
    "handshake-client-limits-response.json",
    "handshake-policy-limit-response.json",
    "handshake-failed-request.json",
    "handshake-failed-response.json",
    "handshake-cancelled-request.json",
    "handshake-cancelled-response.json",
  ]) await context.test(name, async () => {
    const request = name.endsWith("-request.json");
    const validate = request ? isHandshakeRequestEnvelope : isHandshakeResponseEnvelope;
    const decode = request ? decodeHandshakeRequestEnvelope : decodeHandshakeResponseEnvelope;
    const encode = request ? encodeHandshakeRequestEnvelope : encodeHandshakeResponseEnvelope;
    const source = await readFile(new URL(name, fixtureRoot), "utf8");
    const fixture = JSON.parse(source);
    const canonical = (await readFile(new URL(name, canonicalEngineRoot), "utf8")).trim();
    assert.equal(validate(fixture), true);
    assert.equal(encode(decode(source)), canonical);
    assert.equal(encode(decode(canonical)), canonical);
  });
});

test("compatibility rejection fixtures use generated UpgradeDiagnosticData", async (context) => {
  for (const [name, requiredVersionOrRange, affectedArtifacts] of [
    ["handshake-rejected.json", "2.0..2.1", ["engine"]],
    ["handshake-range-mismatch-response.json", "1.1..1.2", ["engine"]],
    ["handshake-schema-digest-mismatch-response.json", "1.0", ["engine"]],
    ["handshake-required-capability-missing-response.json", "1.0", ["engine.future"]],
  ]) await context.test(name, async () => {
    const source = await readFile(new URL(name, canonicalEngineRoot), "utf8");
    const response = decodeHandshakeResponseEnvelope(source);
    assert.equal(response.outcome, "rejected");
    assert.equal(response.diagnostics.length, 1);
    const data = response.diagnostics[0].data;
    assert.notEqual(data, undefined);
    const decoded = decodeUpgradeDiagnosticData(JSON.stringify(data));
    assert.deepEqual(decoded, {
      affected_artifacts: affectedArtifacts,
      current_version: "1.0",
      migration_available: false,
      readonly_possible: false,
      required_version_or_range: requiredVersionOrRange,
    });
    assert.deepEqual(JSON.parse(encodeUpgradeDiagnosticData(decoded)), data);
  });
});

test("invalid compile input is rejected", async () => {
  const fixture = await readFixture("compile-invalid-request.json");
  assert.equal(isCompileRequestEnvelope(fixture), false);
});

test("Workbench preview fixture validates, preserves recursive values, and round-trips", async () => {
  const source = await readFile(new URL("workbench-preview-operations-request.json", fixtureRoot), "utf8");
  const fixture = JSON.parse(source);
  assert.equal(isPreviewOperationsRequestEnvelope(fixture), true);
  const decoded = decodePreviewOperationsRequestEnvelope(source);
  assert.equal(decoded.payload.batch.operations[0].value.kind, "map");
  assert.equal(decoded.payload.batch.operations[0].value.map[0].value.array[1].kind, "absent");
  assert.deepEqual(JSON.parse(encodePreviewOperationsRequestEnvelope(decoded)), fixture);
});

test("Workbench malformed handles, null contracts, ordering, and outcome payloads fail closed", async () => {
  for (const name of [
    "workbench-invalid-handle-request.json",
    "workbench-invalid-null-request.json",
    "workbench-invalid-order-request.json",
  ]) assert.equal(isPreviewOperationsRequestEnvelope(await readFixture(name)), false, name);
  assert.equal(isPreviewOperationsResponseEnvelope(await readFixture("workbench-invalid-outcome-response.json")), false);
  const conflictOnly = await readFixture("workbench-preview-conflict-only-response.json");
  assert.equal(isPreviewOperationsResponseEnvelope(conflictOnly), true);
  assert.deepEqual(JSON.parse(encodePreviewOperationsResponseEnvelope(decodePreviewOperationsResponseEnvelope(JSON.stringify(conflictOnly)))), conflictOnly);
  assert.equal(isPreviewOperationsResponseEnvelope(await readFixture("workbench-invalid-empty-preview-response.json")), false);
  assert.equal(isOpenDocumentResponseEnvelope(await readFixture("workbench-open-document-response.json")), true);
  assert.equal(isOpenDocumentResponseEnvelope(await readFixture("workbench-open-pack-document-response.json")), true);
  assert.equal(isOpenDocumentResponseEnvelope(await readFixture("workbench-open-invalid-root-response.json")), true);
  assert.equal(isOpenDocumentResponseEnvelope(await readFixture("workbench-invalid-unavailable-warning-only-response.json")), false);
  assert.equal(isOpenDocumentResponseEnvelope(await readFixture("workbench-invalid-open-document-capabilities-response.json")), false);
  assert.equal(isReplaceSourceTreeResult(await readFixture("workbench-replace-pack-result.json")), true);
  assert.equal(isResultingHashes(await readFixture("workbench-invalid-pack-resulting-hashes.json")), false);
  assert.equal(isInspectSubgraphResult(await readFixture("workbench-inspect-subgraph-result.json")), true);
  assert.equal(isInspectSubgraphResult(await readFixture("workbench-invalid-subgraph-relation-result.json")), false);
  assert.equal(isInspectSubgraphResult(await readFixture("workbench-invalid-subgraph-item-facts-result.json")), false);
  assert.equal(isInspectSubgraphInput(await readFixture("workbench-invalid-subgraph-root-input.json")), false);
  assert.equal(isFindUsagesResult(await readFixture("workbench-find-usages-result.json")), true);
  assert.equal(isFindUsagesResult(await readFixture("workbench-invalid-find-usages-target-kind-result.json")), false);
  assert.equal(isCloseDocumentResponseEnvelope(await readFixture("workbench-close-failed-response.json")), true);
  assert.equal(isCloseDocumentResponseEnvelope(await readFixture("workbench-invalid-close-failed-response.json")), false);
  assert.equal(isReadDeclarationsResult(await readFixture("workbench-large-declaration-result.json")), true);
  assert.equal(isReadDeclarationsResult(await readFixture("workbench-invalid-declaration-chunk-order-result.json")), false);
  assert.equal(isReadDeclarationsResult(await readFixture("workbench-invalid-page-byte-overflow.json")), false);
  assert.equal(isFindUsagesResult(await readFixture("workbench-invalid-find-usages-order-result.json")), false);
  assert.equal(isEngineEditPreconditions(await readFixture("workbench-optional-preconditions.json")), true);
  assert.equal(isEngineEditPreconditions(await readFixture("workbench-invalid-source-digest-order.json")), false);
  assert.equal(isSemanticOperation(await readFixture("workbench-upsert-row-default-absent.json")), true);
  assert.equal(isSemanticOperation(await readFixture("workbench-create-subject-single-kind.json")), true);
  assert.equal(isSemanticOperation(await readFixture("workbench-invalid-semantic-map-order.json")), false);
  assert.equal(isSemanticOperation(await readFixture("workbench-invalid-authored-path-depth.json")), false);
  assert.equal(isSemanticOperation(await readFixture("workbench-invalid-upsert-row-overlap.json")), false);
  assert.equal(isClassifyAuthoringImpactInput(await readFixture("workbench-invalid-classify-raw-diff.json")), false);
  assert.equal(isSemanticDiff(await readFixture("workbench-invalid-semantic-diff-order.json")), false);
  assert.equal(isSourceDiff(await readFixture("workbench-invalid-source-diff-order.json")), false);
  assert.equal(isSourceDiff(await readFixture("workbench-source-diff-all-kinds.json")), true);
  assert.equal(isSourceDiff(await readFixture("workbench-invalid-replace-source-edit-identity.json")), false);
  assert.equal(isAuthoringImpact(await readFixture("workbench-invalid-authoring-impact-order.json")), false);
  assert.equal(isFindSymbolsInput(await readFixture("workbench-find-symbols-input.json")), true);
  assert.equal(isFindSymbolsInput(await readFixture("workbench-find-symbols-unrestricted-input.json")), true);
  assert.equal(isFindSymbolsInput(await readFixture("workbench-invalid-find-symbols-mode-input.json")), false);
  assert.equal(isFindSymbolsInput(await readFixture("workbench-invalid-find-symbols-empty-filter-input.json")), false);
  assert.equal(isPreviewSourcePatchRequestEnvelope(await readFixture("workbench-preview-source-patch-request.json")), true);
  assert.equal(isPreviewSourcePatchRequestEnvelope(await readFixture("workbench-invalid-overlapping-source-patch-request.json")), false);
  assert.equal(isPreviewOperationsResponseEnvelope(await readFixture("workbench-stale-generation-response.json")), true);
  assert.equal(isPreviewOperationsResponseEnvelope(await readFixture("workbench-invalid-stale-generation-response.json")), false);
  const allCreatableKinds = await readFixture("workbench-create-subject-all-kinds.json");
  assert.equal(isSemanticOperationBatch(allCreatableKinds), true);
  const decodedCreatableKinds = decodeSemanticOperationBatch(encodeSemanticOperationBatch(allCreatableKinds));
  assert.equal(decodedCreatableKinds.operations[1].fields.cardinality.from_per_to.max, 1);
  assert.equal(decodedCreatableKinds.operations[1].fields.cardinality.to_per_from.max, "many");
  assert.equal(isSemanticOperation(await readFixture("workbench-create-relation-fields.json")), true);
  for (const name of ["workbench-invalid-create-subject-foreign-field.json", "workbench-invalid-create-subject-missing-field.json", "workbench-invalid-create-subject-parent-kind.json", "workbench-invalid-create-subject-nested.json", "workbench-invalid-create-subject-cardinality.json", "workbench-invalid-create-subject-enum-options.json", "workbench-invalid-create-subject-view-shape.json", "workbench-invalid-create-subject-flow-lanes.json"]) assert.equal(isSemanticOperation(await readFixture(name)), false, name);
  assert.equal(isAuthoringImpact(await readFixture("workbench-authoring-impact-graph.json")), true);
  for (const name of ["workbench-invalid-authoring-impact-missing-facts.json", "workbench-invalid-authoring-impact-address-kind.json", "workbench-invalid-authoring-impact-action.json", "workbench-invalid-authoring-impact-capabilities.json"]) assert.equal(isAuthoringImpact(await readFixture(name)), false, name);
  for (const name of ["workbench-invalid-source-create-digest.json", "workbench-invalid-source-blob-media.json", "workbench-invalid-source-blob-lifetime.json", "workbench-invalid-source-move-module.json", "workbench-invalid-source-move-digest.json"]) assert.equal(isSourceDiff(await readFixture(name)), false, name);
  assert.equal(isWorkbenchPreviewResult(await readFixture("workbench-preview-valid-warning.json")), true);
  for (const name of ["workbench-invalid-preview-impact-digest.json", "workbench-invalid-preview-capabilities.json", "workbench-invalid-preview-semantic-hash.json", "workbench-invalid-preview-source-hash.json", "workbench-invalid-preview-resulting-hash.json", "workbench-invalid-preview-endpoint.json", "workbench-invalid-preview-generation.json", "workbench-invalid-preview-warning-only.json"]) assert.equal(isWorkbenchPreviewResult(await readFixture(name)), false, name);
  assert.equal(isApplyToHandleResult(await readFixture("workbench-apply-result.json")), true);
  for (const name of ["workbench-invalid-apply-source-hash.json", "workbench-invalid-apply-resulting-hash.json"]) assert.equal(isApplyToHandleResult(await readFixture(name)), false, name);
  assert.equal(isFindSymbolsInput(await readFixture("workbench-invalid-input-cursor-generation.json")), false);
  assert.equal(isCloseDocumentInput(await readFixture("workbench-invalid-close-generation.json")), false);
  assert.equal(isApplyToHandleInput(await readFixture("workbench-invalid-apply-endpoint.json")), false);
  assert.equal(isOpenDocumentResult(await readFixture("workbench-invalid-open-handle-generation.json")), false);
  assert.equal(isListModulesResult(await readFixture("workbench-page-empty.json")), true);
  for (const name of ["workbench-invalid-page-count.json", "workbench-invalid-page-bytes.json", "workbench-invalid-page-cursor-generation.json"]) assert.equal(isListModulesResult(await readFixture(name)), false, name);
  for (const name of ["workbench-invalid-chunk-overflow.json", "workbench-invalid-chunk-media.json"]) assert.equal(isBoundedTextChunk(await readFixture(name)), false, name);
  assert.equal(isClassifyAuthoringImpactInput(await readFixture("workbench-classify-authoring-impact-input.json")), true);
  for (const name of ["workbench-rejected-handle-response.json", "workbench-rejected-cursor-response.json", "workbench-rejected-generation-response.json", "workbench-rejected-input-response.json", "workbench-rejected-not_found-response.json", "workbench-rejected-preview-response.json", "workbench-rejected-unsupported-response.json", "workbench-rejected-precondition-response.json", "workbench-failed-execution-response.json", "workbench-cancelled-response.json"]) assert.equal(isCloseDocumentResponseEnvelope(await readFixture(name)), true, name);
});

test("generated CompileInput BlobRef collector preserves traversal, duplicates, and detachment", async () => {
  const input = structuredClone((await readFixture("compile-request.json")).payload);
  const shared = input.project_source_tree[0].blob;
  input.installed_pack_tree = [{path: "pack.ldl", blob: shared}];
  input.project_source_tree.push({path: "z.ldl", blob: shared});
  input.referenced_assets = [{origin: "project", locator: "asset.txt", blob: shared, digest: shared.digest, media_type: shared.media_type}];
  input.resolved_dependencies.installs = [{
    install_name: "dep",
    canonical_id: "publisher/pack",
    version: "1.0.0",
    digest: shared.digest,
    path: "packs/dep",
    entry: "main.ldl",
    files: [],
    dependencies: [],
    manifest_path: "manifest.json",
    manifest: shared,
  }];

  const refs = collectCompileInputBlobRefs(input);
  assert.deepEqual(refs.map((ref) => ref.blob_id), Array(5).fill(shared.blob_id));
  assert.equal(new Set(refs).size, refs.length);
  assert.ok(refs.every((ref) => ref !== shared));
  refs[0].blob_id = "mutated";
  assert.equal(shared.blob_id, "compile/source/document.ldl");
  assert.equal(refs[1].blob_id, shared.blob_id);

  const hostile = structuredClone(input);
  delete hostile.project_source_tree[0].blob;
  assert.throws(() => collectCompileInputBlobRefs(hostile), TypeError);
});

test("generated CompileResult BlobRef collector preserves wire order, aliases, and validation", async () => {
  const result = structuredClone((await readFixture("compile-success.json")).payload);
  const shared = result.compiled_recipes.queries[0].canonical_json;
  result.compiled_recipes.queries.push({address: "ldl:project:fixture:query:other", canonical_json: shared});

  const refs = collectCompileResultBlobRefs(result);
  assert.deepEqual(refs.map((ref) => ref.blob_id), [
    shared.blob_id,
    shared.blob_id,
    result.normalized_artifact.project.artifact_json.blob_id,
    result.normalized_artifact.project.canonical_json.blob_id,
  ]);
  assert.equal(new Set(refs).size, refs.length);
  assert.ok(refs.every((ref) => ref !== shared));
  refs[0].blob_id = "mutated";
  assert.equal(result.compiled_recipes.queries[0].canonical_json.blob_id, "compile/query/all");
  assert.equal(refs[1].blob_id, shared.blob_id);

  const hostile = structuredClone(result);
  delete hostile.compiled_recipes.queries[0].canonical_json;
  assert.throws(() => collectCompileResultBlobRefs(hostile), TypeError);
});

test("generated handshake request-ID boundary is exact", async () => {
  const fixture = await readFixture("handshake-request.json");
  fixture.request_id = "r".repeat(128);
  assert.equal(isHandshakeRequestEnvelope(fixture), true);
  fixture.request_id += "r";
  assert.equal(isHandshakeRequestEnvelope(fixture), false);
  assert.throws(() => encodeHandshakeRequestEnvelope(fixture));
});

test("unknown response fields are rejected while explicit extensions survive", async () => {
  const fixture = await readFixture("compile-rejected.json");
  assert.equal(isCompileResponseEnvelope({...fixture, unexpected: true}), false);
  const extended = {...fixture, extensions: {"example.test": {enabled: true}}};
  assert.equal(isCompileResponseEnvelope(extended), true);
  assert.deepEqual(JSON.parse(encodeCompileResponseEnvelope(decodeCompileResponseEnvelope(JSON.stringify(extended)))), extended);
});

test("shared common fixtures have byte-identical canonical encoding", async () => {
  const blobText = (await readFile(new URL("blob-ref-canonical.json", commonFixtureRoot), "utf8")).trim();
  assert.equal(encodeBlobRef(decodeBlobRef(blobText)), blobText);
  const jsonText = (await readFile(new URL("json-value-canonical.json", commonFixtureRoot), "utf8")).trim();
  assert.equal(encodeJsonValue(decodeJsonValue(jsonText)), jsonText);
});

test("TypeScript matches shared canonical Go/TypeScript engine-envelope bytes", async (context) => {
  for (const [name, decode, encode] of [
    ["compile-request.json", decodeCompileRequestEnvelope, encodeCompileRequestEnvelope],
    ["compile-rejected.json", decodeCompileResponseEnvelope, encodeCompileResponseEnvelope],
    ["compile-success-pack.json", decodeCompileResponseEnvelope, encodeCompileResponseEnvelope],
    ["handshake-request.json", decodeHandshakeRequestEnvelope, encodeHandshakeRequestEnvelope],
    ["handshake-success.json", decodeHandshakeResponseEnvelope, encodeHandshakeResponseEnvelope],
    ["handshake-rejected.json", decodeHandshakeResponseEnvelope, encodeHandshakeResponseEnvelope],
  ]) await context.test(name, async () => {
    const source = await readFile(new URL(name, fixtureRoot), "utf8");
    const canonical = (await readFile(new URL(name, canonicalEngineRoot), "utf8")).trim();
    assert.equal(encode(decode(source)), canonical);
    assert.equal(encode(decode(canonical)), canonical);
  });
});

test("legacy scalar negatives remain enforced", async () => {
  for (const invalid of ["-0", "01", "18446744073709551616"]) {
    assert.throws(() => decodeCanonicalUint64(JSON.stringify(invalid)));
  }
  for (const invalid of [
    "sha256:ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
    "md5:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  ]) assert.throws(() => decodeDigest(JSON.stringify(invalid)));
});

test("TypeScript encoders inspect exactly own enumerable data properties", () => {
  const inherited = Object.create({enabled: true, protocol_version: "1.0"});
  assert.equal(isOperationCapability(inherited), false);
  assert.throws(() => encodeOperationCapability(inherited));

  const hidden = {};
  Object.defineProperty(hidden, "enabled", {value: true, enumerable: false});
  Object.defineProperty(hidden, "protocol_version", {value: "1.0", enumerable: true});
  assert.equal(isOperationCapability(hidden), false);
  assert.throws(() => encodeOperationCapability(hidden));

  const symbol = {enabled: true, protocol_version: "1.0"};
  symbol[Symbol("extension")] = true;
  assert.equal(isOperationCapability(symbol), false);
  assert.throws(() => encodeOperationCapability(symbol));

  let getterCalled = false;
  const getter = {protocol_version: "1.0"};
  Object.defineProperty(getter, "enabled", {enumerable: true, get() { getterCalled = true; return true; }});
  assert.equal(isOperationCapability(getter), false);
  assert.throws(() => encodeOperationCapability(getter));
  assert.equal(getterCalled, false);

  const nullPrototype = Object.assign(Object.create(null), {enabled: true, protocol_version: "1.0"});
  assert.equal(isOperationCapability(nullPrototype), true);
  assert.equal(encodeOperationCapability(nullPrototype), '{"enabled":true,"protocol_version":"1.0"}');

  for (const hostile of [
    new Proxy({}, {getPrototypeOf() { throw new Error("prototype trap"); }}),
    new Proxy({}, {ownKeys() { throw new Error("ownKeys trap"); }}),
    new Proxy({enabled: true, protocol_version: "1.0"}, {getOwnPropertyDescriptor() { throw new Error("descriptor trap"); }}),
  ]) assert.equal(isOperationCapability(hostile), false);
});

test("public object predicates reject non-scalar Unicode keys at every object surface", async () => {
  const compileRequest = await readFixture("compile-request.json");
  const invalidCases = [];
  for (const badKey of ["\ud800", "\udc00"]) {
    invalidCases.push(
      ["JsonValue root map", isJsonValue, encodeJsonValue, {[badKey]: null}],
      ["JsonObject root map", isJsonObject, encodeJsonObject, {[badKey]: null}],
      ["Extensions additionalProperties map", isExtensions, encodeExtensions, {[badKey]: null}],
      ["DiagnosticArgumentValue recursive object map", isDiagnosticArgumentValue, encodeDiagnosticArgumentValue, {kind: "object", object_value: {[badKey]: {kind: "string", string_value: "leaf"}}}],
      ["closed root object", isCompileRequestEnvelope, encodeCompileRequestEnvelope, {...compileRequest, [badKey]: null}],
      ["nested closed object", isCompileRequestEnvelope, encodeCompileRequestEnvelope, {...compileRequest, payload: {...compileRequest.payload, [badKey]: null}}],
      ["nested extension map", isCompileRequestEnvelope, encodeCompileRequestEnvelope, {...compileRequest, extensions: {[badKey]: null}}],
    );
  }
  for (const [name, predicate, encode, value] of invalidCases) {
    assert.equal(predicate(value), false, name);
    assert.throws(() => encode(value), TypeError, name);
  }

  const sharedJSON = {enabled: true};
  const validExtensions = {"example.界": sharedJSON, "example.😀": sharedJSON};
  assert.equal(isJsonValue(validExtensions), true);
  assert.equal(isJsonObject(validExtensions), true);
  assert.equal(isExtensions(validExtensions), true);
  assert.doesNotThrow(() => encodeJsonValue(validExtensions));
  assert.doesNotThrow(() => encodeJsonObject(validExtensions));
  assert.doesNotThrow(() => encodeExtensions(validExtensions));

  const sharedDiagnostic = {kind: "string", string_value: "shared"};
  const validDiagnostic = {kind: "object", object_value: {"界": sharedDiagnostic, "😀": sharedDiagnostic}};
  assert.equal(isDiagnosticArgumentValue(validDiagnostic), true);
  assert.doesNotThrow(() => encodeDiagnosticArgumentValue(validDiagnostic));

  const validCompileRequest = {...compileRequest, extensions: validExtensions};
  assert.equal(isCompileRequestEnvelope(validCompileRequest), true);
  assert.doesNotThrow(() => encodeCompileRequestEnvelope(validCompileRequest));
});

const sharedCodecs = {
  ByteResourceLimitCapability: [decodeByteResourceLimitCapability, encodeByteResourceLimitCapability],
  CapabilityID: [decodeCapabilityID, encodeCapabilityID],
  HandshakeRequest: [decodeHandshakeRequest, encodeHandshakeRequest],
  CanonicalFiniteDecimal: [decodeCanonicalFiniteDecimal, encodeCanonicalFiniteDecimal],
  CanonicalInt64: [decodeCanonicalInt64, encodeCanonicalInt64],
  CanonicalNonNegativeInt64: [decodeCanonicalNonNegativeInt64, encodeCanonicalNonNegativeInt64],
  CanonicalNonNegativeSafeInteger: [decodeCanonicalNonNegativeSafeInteger, encodeCanonicalNonNegativeSafeInteger],
  CanonicalPositiveFiniteDecimal: [decodeCanonicalPositiveFiniteDecimal, encodeCanonicalPositiveFiniteDecimal],
  CanonicalPositiveInt64: [decodeCanonicalPositiveInt64, encodeCanonicalPositiveInt64],
  CanonicalPositiveSafeInteger: [decodeCanonicalPositiveSafeInteger, encodeCanonicalPositiveSafeInteger],
  CanonicalSafeInteger: [decodeCanonicalSafeInteger, encodeCanonicalSafeInteger],
  CanonicalSourcePath: [decodeCanonicalSourcePath, encodeCanonicalSourcePath],
  CanonicalUint64: [decodeCanonicalUint64, encodeCanonicalUint64],
  Color: [decodeColor, encodeColor],
  CompiledExportRecipeDocument: [decodeCompiledExportRecipeDocument, encodeCompiledExportRecipeDocument],
  CompiledQueryRecipeDocument: [decodeCompiledQueryRecipeDocument, encodeCompiledQueryRecipeDocument],
  CompiledViewRecipeDocument: [decodeCompiledViewRecipeDocument, encodeCompiledViewRecipeDocument],
  Diagnostic: [decodeDiagnostic, encodeDiagnostic],
  Digest: [decodeDigest, encodeDigest],
  EndpointInstanceID: [decodeEndpointInstanceID, encodeEndpointInstanceID],
  EffectiveResourceLimits: [decodeEffectiveResourceLimits, encodeEffectiveResourceLimits],
  ExportRecipeBlobRef: [decodeExportRecipeBlobRef, encodeExportRecipeBlobRef],
  ExportDimension: [decodeExportDimension, encodeExportDimension],
  ExportOptions: [decodeExportOptions, encodeExportOptions],
  ExportRecipe: [decodeExportRecipe, encodeExportRecipe],
  EntityTypeColumnAddress: [decodeEntityTypeColumnAddress, encodeEntityTypeColumnAddress],
  JsonValue: [decodeJsonValue, encodeJsonValue],
  HandshakeRequest: [decodeHandshakeRequest, encodeHandshakeRequest],
  ManifestETag: [decodeManifestETag, encodeManifestETag],
  NormalizedPackArtifactBlobRef: [decodeNormalizedPackArtifactBlobRef, encodeNormalizedPackArtifactBlobRef],
  NormalizedPackCanonicalBlobRef: [decodeNormalizedPackCanonicalBlobRef, encodeNormalizedPackCanonicalBlobRef],
  NormalizedProjectArtifactBlobRef: [decodeNormalizedProjectArtifactBlobRef, encodeNormalizedProjectArtifactBlobRef],
  NormalizedProjectCanonicalBlobRef: [decodeNormalizedProjectCanonicalBlobRef, encodeNormalizedProjectCanonicalBlobRef],
  OperationCapability: [decodeOperationCapability, encodeOperationCapability],
  ParameterAddress: [decodeParameterAddress, encodeParameterAddress],
  ProtocolOffer: [decodeProtocolOffer, encodeProtocolOffer],
  ProtocolVersion: [decodeProtocolVersion, encodeProtocolVersion],
  ProtocolVersionOrRange: [decodeProtocolVersionOrRange, encodeProtocolVersionOrRange],
  ProtocolVersionRange: [decodeProtocolVersionRange, encodeProtocolVersionRange],
  RecipePredicate: [decodeRecipePredicate, encodeRecipePredicate],
  RecipeRowPredicate: [decodeRecipeRowPredicate, encodeRecipeRowPredicate],
  RasterBackground: [decodeRasterBackground, encodeRasterBackground],
  RelationTypeColumnAddress: [decodeRelationTypeColumnAddress, encodeRelationTypeColumnAddress],
  RequestedCapabilityStatus: [decodeRequestedCapabilityStatus, encodeRequestedCapabilityStatus],
  QueryRecipeBlobRef: [decodeQueryRecipeBlobRef, encodeQueryRecipeBlobRef],
  ReleaseVersion: [decodeReleaseVersion, encodeReleaseVersion],
  Rfc3339Time: [decodeRfc3339Time, encodeRfc3339Time],
  SearchField: [decodeSearchField, encodeSearchField],
  StableAddress: [decodeStableAddress, encodeStableAddress],
  PackRootAddress: [decodePackRootAddress, encodePackRootAddress],
  ProjectRootAddress: [decodeProjectRootAddress, encodeProjectRootAddress],
  QueryAddress: [decodeQueryAddress, encodeQueryAddress],
  QueryRecipe: [decodeQueryRecipe, encodeQueryRecipe],
  QueryRecipeDependencies: [decodeQueryRecipeDependencies, encodeQueryRecipeDependencies],
  QueryRecipeParameter: [decodeQueryRecipeParameter, encodeQueryRecipeParameter],
  QueryRecipeSelect: [decodeQueryRecipeSelect, encodeQueryRecipeSelect],
  QueryRecipeTraversal: [decodeQueryRecipeTraversal, encodeQueryRecipeTraversal],
  RecipePredicateValue: [decodeRecipePredicateValue, encodeRecipePredicateValue],
  SourceSpan: [decodeSourceSpan, encodeSourceSpan],
  TotalItems: [decodeTotalItems, encodeTotalItems],
  UpgradeDiagnosticData: [decodeUpgradeDiagnosticData, encodeUpgradeDiagnosticData],
  ViewAddress: [decodeViewAddress, encodeViewAddress],
  ViewDiagramProjection: [decodeViewDiagramProjection, encodeViewDiagramProjection],
  ViewExportAddress: [decodeViewExportAddress, encodeViewExportAddress],
  ViewFlowProjection: [decodeViewFlowProjection, encodeViewFlowProjection],
  ViewPlacement: [decodeViewPlacement, encodeViewPlacement],
  ViewRecipe: [decodeViewRecipe, encodeViewRecipe],
  ViewRecipeDependencies: [decodeViewRecipeDependencies, encodeViewRecipeDependencies],
  ViewRecipeBlobRef: [decodeViewRecipeBlobRef, encodeViewRecipeBlobRef],
  ViewRenderSet: [decodeViewRenderSet, encodeViewRenderSet],
  ViewRecipeSource: [decodeViewRecipeSource, encodeViewRecipeSource],
  ViewDiagramShape: [decodeViewDiagramShape, encodeViewDiagramShape],
  ViewFlowShape: [decodeViewFlowShape, encodeViewFlowShape],
  ViewMatrixAxis: [decodeViewMatrixAxis, encodeViewMatrixAxis],
  ViewMatrixCell: [decodeViewMatrixCell, encodeViewMatrixCell],
  ViewTableColumnSource: [decodeViewTableColumnSource, encodeViewTableColumnSource],
  ViewTableColumn: [decodeViewTableColumn, encodeViewTableColumn],
  ViewTableProjection: [decodeViewTableProjection, encodeViewTableProjection],
  ViewTableShape: [decodeViewTableShape, encodeViewTableShape],
  ViewTreeShape: [decodeViewTreeShape, encodeViewTreeShape],
};

async function readCorpus() {
  return JSON.parse(await readFile(conformanceCorpusURL, "utf8"));
}

test("shared canonical and rejection corpus matches exact bytes", async (context) => {
  const corpus = await readCorpus();
  assert.equal(corpus.schema_version, 1);
  assert.equal(corpus.max_json_bytes, maxWireJSONBytes);
  assert.equal(corpus.max_json_depth, maxWireJSONDepth);
  for (const vector of corpus.canonical_cases) await context.test(vector.name, () => {
    const codec = sharedCodecs[vector.type];
    assert.ok(codec, `unknown shared codec ${vector.type}`);
    const encoded = codec[1](codec[0](vector.input));
    assert.equal(encoded, vector.expected);
    assert.equal(codec[1](codec[0](encoded)), encoded);
  });
  for (const vector of corpus.rejection_cases) await context.test(vector.name, () => {
    const codec = sharedCodecs[vector.type];
    assert.ok(codec, `unknown shared codec ${vector.type}`);
    assert.throws(() => codec[0](vector.input));
  });
});

test("shared custom format authority vectors match TypeScript codecs", async (context) => {
  const corpus = JSON.parse(await readFile(formatCorpusURL, "utf8"));
  for (const vector of corpus.vectors) await context.test(vector.name, () => {
    const codec = sharedCodecs[vector.type];
    assert.ok(codec, `unknown format codec ${vector.type}`);
    const input = JSON.stringify(vector.value);
    if (vector.valid) assert.equal(codec[1](codec[0](input)), input);
    else assert.throws(() => codec[0](input));
  });
});

test("every closed export-option variant has shared canonical and rejection bytes", async (context) => {
  const corpus = JSON.parse(await readFile(exportOptionsCorpusURL, "utf8"));
  assert.equal(corpus.schema_version, 1);
  for (const vector of corpus.canonical_cases) await context.test(`${vector.name} canonical`, () => {
    assert.equal(encodeExportOptions(decodeExportOptions(vector.input)), vector.expected);
  });
  for (const vector of corpus.rejection_cases) await context.test(`${vector.name} rejection`, () => {
    assert.throws(() => decodeExportOptions(vector.input));
  });
});

test("every predicate branch has shared canonical and rejection bytes", async (context) => {
  const corpus = JSON.parse(await readFile(predicateCorpusURL, "utf8"));
  for (const vector of corpus.canonical_cases) await context.test(`${vector.name} canonical`, () => {
    const codec = sharedCodecs[vector.type];
    assert.equal(codec[1](codec[0](vector.input)), vector.expected);
  });
  for (const vector of corpus.rejection_cases) await context.test(`${vector.name} rejection`, () => {
    assert.throws(() => sharedCodecs[vector.type][0](vector.input));
  });
});

test("every View source and address-bearing shape contract has shared bytes", async (context) => {
  const corpus = JSON.parse(await readFile(viewSourceCorpusURL, "utf8"));
  assert.equal(corpus.schema_version, 1);
  assert.equal(corpus.canonical_cases.length, 30);
  assert.equal(corpus.rejection_cases.length, 59);
  for (const vector of corpus.canonical_cases) await context.test(`${vector.name} canonical`, () => {
    const codec = sharedCodecs[vector.type];
    assert.equal(codec[1](codec[0](vector.input)), vector.expected);
  });
  for (const vector of corpus.rejection_cases) await context.test(`${vector.name} rejection`, () => {
    assert.throws(() => sharedCodecs[vector.type][0](vector.input));
  });
});

test("complete View and Export semantics match the shared adversarial corpus", async (context) => {
  const corpus = JSON.parse(await readFile(viewExportSemanticsCorpusURL, "utf8"));
  assert.equal(corpus.schema_version, 1);
  assert.equal(corpus.canonical_cases.length, 34);
  assert.equal(corpus.rejection_cases.length, 94);
  for (const vector of corpus.canonical_cases) await context.test(`${vector.name} canonical`, () => {
    const codec = sharedCodecs[vector.type];
    assert.ok(codec, `unknown View/Export codec ${vector.type}`);
    const encoded = codec[1](codec[0](JSON.stringify(vector.value)));
    assert.deepEqual(JSON.parse(encoded), vector.value);
    assert.equal(codec[1](codec[0](encoded)), encoded);
  });
  for (const vector of corpus.rejection_cases) await context.test(`${vector.name} rejection`, () => {
    const codec = sharedCodecs[vector.type];
    assert.ok(codec, `unknown View/Export codec ${vector.type}`);
    assert.throws(() => codec[0](JSON.stringify(vector.value)));
  });
});

test("complete Query authority matches the shared canonical and rejection corpus", async (context) => {
  const corpus = JSON.parse(await readFile(queryAuthorityCorpusURL, "utf8"));
  assert.equal(corpus.schema_version, 1);
  assert.equal(corpus.canonical_cases.length, 20);
  assert.equal(corpus.rejection_cases.length, 55);
  for (const vector of corpus.canonical_cases) await context.test(`${vector.name} canonical`, () => {
    const codec = sharedCodecs[vector.type];
    assert.ok(codec, `unknown Query codec ${vector.type}`);
    const encoded = codec[1](codec[0](JSON.stringify(vector.value)));
    assert.deepEqual(JSON.parse(encoded), vector.value);
    assert.equal(codec[1](codec[0](encoded)), encoded);
  });
  for (const vector of corpus.rejection_cases) await context.test(`${vector.name} rejection`, () => {
    const codec = sharedCodecs[vector.type];
    assert.ok(codec, `unknown Query codec ${vector.type}`);
    assert.throws(() => codec[0](JSON.stringify(vector.value)));
  });
});

test("cross-cutting semantic root authority matches the shared corpus", async (context) => {
  const corpus = JSON.parse(await readFile(semanticRootAuthorityCorpusURL, "utf8"));
  assert.equal(corpus.schema_version, 1);
  assert.equal(corpus.canonical_cases.length, 2);
  assert.equal(corpus.rejection_cases.length, 5);
  for (const vector of corpus.canonical_cases) await context.test(`${vector.name} canonical`, () => {
    const codec = sharedCodecs[vector.type];
    assert.ok(codec, `unknown semantic root codec ${vector.type}`);
    const encoded = codec[1](codec[0](JSON.stringify(vector.value)));
    assert.deepEqual(JSON.parse(encoded), vector.value);
  });
  for (const vector of corpus.rejection_cases) await context.test(`${vector.name} rejection`, () => {
    const codec = sharedCodecs[vector.type];
    assert.ok(codec, `unknown semantic root codec ${vector.type}`);
    assert.throws(() => codec[0](JSON.stringify(vector.value)));
  });
});

test("published scalar-Unicode vectors match TypeScript codecs recursively", async (context) => {
  const corpus = JSON.parse(await readFile(unicodeScalarCorpusURL, "utf8"));
  assert.equal(corpus.schema_version, 1);
  assert.equal(corpus.canonical_cases.length, 2);
  assert.equal(corpus.rejection_cases.length, 9);
  for (const vector of corpus.canonical_cases) await context.test(`${vector.name} canonical`, () => {
    const codec = sharedCodecs[vector.type];
    assert.equal(codec[1](codec[0](vector.input)), vector.expected);
  });
  for (const vector of corpus.rejection_cases) await context.test(`${vector.name} rejection`, () => {
    assert.throws(() => sharedCodecs[vector.type][0](vector.input));
  });
});

test("typed-programmatic normalized roots and View sources enforce semantic closure", async () => {
  const project = structuredClone((await readFixture("compile-success.json")).payload.normalized_artifact.project);
  const pack = structuredClone((await readFixture("compile-success-pack.json")).payload.normalized_artifact.pack);
  for (const project_address of ["ldl:pack:publisher:pack", "ldl:project:p:view:v"]) {
    assert.throws(() => encodeNormalizedProjectArtifact({...project, project_address}), TypeError);
  }
  for (const pack_address of ["ldl:project:p", "ldl:pack:publisher:pack:view:v"]) {
    assert.throws(() => encodeNormalizedPackArtifact({...pack, pack_address}), TypeError);
  }

  const validColumns = [
    {kind: "field", field: "tags"},
    {kind: "attribute", column_addresses: ["ldl:project:p:entity-type:t:column:c"]},
    {kind: "relation_endpoint", endpoint: "from", field: "display_name"},
    {kind: "derived_count", direction: "both", relation_type_addresses: ["ldl:project:p:relation-type:r"]},
    {kind: "state", field_path: "system.updated_at"},
  ];
  for (const value of validColumns) assert.doesNotThrow(() => encodeViewTableColumnSource(value));
  const invalidColumns = [
    {kind: "field"},
    {kind: "attribute", column_addresses: [], field: "id"},
    {kind: "attribute", column_addresses: ["ldl:project:p"]},
    {kind: "attribute", column_addresses: ["ldl:project:p:entity-type:t:column:c", "ldl:project:p:entity-type:t:column:c"]},
    {kind: "attribute", column_addresses: ["ldl:pack:publisher:shared-pack:entity-type:t:column:c", "ldl:project:p:entity-type:t:column:c"]},
    {kind: "relation_endpoint", endpoint: "from", field: "description"},
    {kind: "derived_count"},
    {kind: "derived_count", direction: "both", relation_type_addresses: ["ldl:project:p:entity:e"]},
    {kind: "derived_count", direction: "both", relation_type_addresses: ["ldl:project:p:relation-type:r", "ldl:project:p:relation-type:r"]},
    {kind: "state"},
    {kind: "state", field_path: "review.status"},
  ];
  for (const value of invalidColumns) assert.throws(() => encodeViewTableColumnSource(value), TypeError);

  const query_address = "ldl:project:p:query:q";
  const argumentsWithValue = {"ldl:project:p:query:q:parameter:x": {kind: "string", string_value: "x"}};
  for (const value of [
    {kind: "query", query_address, arguments: {}},
    {kind: "diff", before: "base", after: "head", arguments: {}},
    {kind: "diff", before: "base", after: "head", query_address, arguments: {}},
    {kind: "diff", before: "base", after: "head", query_address, arguments: argumentsWithValue},
  ]) assert.doesNotThrow(() => encodeViewRecipeSource(value));
  for (const value of [
    {kind: "query", query_address: "ldl:project:p", arguments: {}},
    {kind: "query", query_address, arguments: {"not-an-address": {kind: "string", string_value: "x"}}},
    {kind: "diff", after: "head", arguments: {}},
    {kind: "diff", before: "base", arguments: {}},
    {kind: "diff", before: "", after: "head", arguments: {}},
    {kind: "diff", before: "base", after: "", arguments: {}},
    {kind: "diff", before: "same", after: "same", arguments: {}},
    {kind: "diff", before: "base", after: "head", arguments: argumentsWithValue},
  ]) assert.throws(() => encodeViewRecipeSource(value), TypeError);
});

test("shared recursive JsonValue limits are exact", async () => {
  const corpus = await readCorpus();
  const deep = "[".repeat(corpus.max_json_depth) + '"x"' + "]".repeat(corpus.max_json_depth);
  assert.equal(encodeJsonValue(decodeJsonValue(deep)), deep);
  assert.throws(() => decodeJsonValue("[" + deep + "]"));
  const maximum = '"' + "a".repeat(corpus.max_json_bytes - 2) + '"';
  assert.equal(encodeJsonValue(decodeJsonValue(maximum)), maximum);
  assert.throws(() => decodeJsonValue('"' + "a".repeat(corpus.max_json_bytes - 1) + '"'));
});

test("programmatic JsonValue cycles and depth fail with TypeError", () => {
  const self = {};
  self.self = self;
  assert.throws(() => encodeJsonValue(self), TypeError);
  const left = {};
  const right = {};
  left.right = right;
  right.left = left;
  assert.throws(() => encodeJsonValue(left), TypeError);

  let value = "leaf";
  for (let depth = 0; depth < maxWireJSONDepth; depth++) value = [value];
  const encoded = encodeJsonValue(value);
  assert.deepEqual(decodeJsonValue(encoded), value);
  assert.throws(() => encodeJsonValue([value]), TypeError);
});

function containerDepth(value) {
  if (value === null || typeof value !== "object") return 0;
  const children = Array.isArray(value) ? value : Object.values(value);
  return 1 + children.reduce((maximum, child) => Math.max(maximum, containerDepth(child)), 0);
}

function expectCycleError(callback) {
  assert.throws(callback, (error) => error instanceof TypeError && error.message === "protocol value contains a cycle");
}

function expectDepthError(callback) {
  assert.throws(callback, (error) => error instanceof TypeError && error.message === `protocol value exceeds depth ${maxWireJSONDepth}`);
}

test("semantic recursive codecs reject self and mutual cycles while preserving aliases", () => {
  const cases = [
    [
      "DiagnosticArgumentValue",
      encodeDiagnosticArgumentValue,
      () => { const value = {kind: "array", array_value: []}; value.array_value.push(value); return value; },
      () => { const left = {kind: "array", array_value: []}; const right = {kind: "array", array_value: [left]}; left.array_value.push(right); return left; },
      () => { const shared = {kind: "string", string_value: "shared"}; return {kind: "array", array_value: [shared, shared]}; },
    ],
    [
      "RecipePredicate",
      encodeRecipePredicate,
      () => { const value = {kind: "not"}; value.child = value; return value; },
      () => { const left = {kind: "not"}; const right = {kind: "not", child: left}; left.child = right; return left; },
      () => { const shared = {kind: "field", field: "display_name", operand_type: {kind: "scalar", scalar_type: "string"}, operator: "exists"}; return {kind: "all", children: [shared, shared]}; },
    ],
    [
      "RecipeRowPredicate",
      encodeRecipeRowPredicate,
      () => { const value = {kind: "not"}; value.child = value; return value; },
      () => { const left = {kind: "not"}; const right = {kind: "not", child: left}; left.child = right; return left; },
      () => { const shared = {kind: "state", field_path: "system.updated_at", operand_type: {kind: "scalar", scalar_type: "datetime"}, operator: "exists"}; return {kind: "all", children: [shared, shared]}; },
    ],
  ];
  for (const [name, encode, self, mutual, alias] of cases) {
    expectCycleError(() => encode(self()), `${name} self cycle`);
    expectCycleError(() => encode(mutual()), `${name} mutual cycle`);
    assert.doesNotThrow(() => encode(alias()), `${name} acyclic alias`);
  }
});

test("every recursive public predicate is total and enforces exact wire graph bounds", () => {
  const jsonAtLimit = () => {
    let value = "leaf";
    for (let depth = 0; depth < maxWireJSONDepth; depth++) value = [value];
    return value;
  };
  const diagnosticAtLimit = () => {
    let value = {kind: "array", array_value: []};
    for (let depth = 0; depth < 63; depth++) value = {kind: "array", array_value: [value]};
    return value;
  };
  const predicateAtLimit = (leaf) => {
    let value = leaf;
    for (let depth = 2; depth < maxWireJSONDepth; depth++) value = {kind: "not", child: value};
    return value;
  };
  const cases = [
    {
      name: "JsonValue",
      predicate: isJsonValue,
      encode: encodeJsonValue,
      self: () => { const value = {}; value.self = value; return value; },
      mutual: () => { const left = {}; const right = {left}; left.right = right; return left; },
      alias: () => { const shared = {value: "shared"}; return {left: shared, right: shared}; },
      atLimit: jsonAtLimit,
      tooDeep: () => [jsonAtLimit()],
    },
    {
      name: "DiagnosticArgumentValue",
      predicate: isDiagnosticArgumentValue,
      encode: encodeDiagnosticArgumentValue,
      self: () => { const value = {kind: "array", array_value: []}; value.array_value.push(value); return value; },
      mutual: () => { const left = {kind: "array", array_value: []}; const right = {kind: "array", array_value: [left]}; left.array_value.push(right); return left; },
      alias: () => { const shared = {kind: "string", string_value: "shared"}; return {kind: "array", array_value: [shared, shared]}; },
      atLimit: diagnosticAtLimit,
      tooDeep: () => { let value = {kind: "string", string_value: "leaf"}; for (let depth = 0; depth < 64; depth++) value = {kind: "array", array_value: [value]}; return value; },
    },
    {
      name: "RecipePredicate",
      predicate: isRecipePredicate,
      encode: encodeRecipePredicate,
      self: () => { const value = {kind: "not"}; value.child = value; return value; },
      mutual: () => { const left = {kind: "not"}; const right = {kind: "not", child: left}; left.child = right; return left; },
      alias: () => { const shared = {kind: "field", field: "display_name", operand_type: {kind: "scalar", scalar_type: "string"}, operator: "exists"}; return {kind: "all", children: [shared, shared]}; },
      atLimit: () => predicateAtLimit({kind: "field", field: "display_name", operand_type: {kind: "scalar", scalar_type: "string"}, operator: "exists"}),
      tooDeep: () => ({kind: "not", child: predicateAtLimit({kind: "field", field: "display_name", operand_type: {kind: "scalar", scalar_type: "string"}, operator: "exists"})}),
    },
    {
      name: "RecipeRowPredicate",
      predicate: isRecipeRowPredicate,
      encode: encodeRecipeRowPredicate,
      self: () => { const value = {kind: "not"}; value.child = value; return value; },
      mutual: () => { const left = {kind: "not"}; const right = {kind: "not", child: left}; left.child = right; return left; },
      alias: () => { const shared = {kind: "state", field_path: "system.updated_at", operand_type: {kind: "scalar", scalar_type: "datetime"}, operator: "exists"}; return {kind: "all", children: [shared, shared]}; },
      atLimit: () => predicateAtLimit({kind: "state", field_path: "system.updated_at", operand_type: {kind: "scalar", scalar_type: "datetime"}, operator: "exists"}),
      tooDeep: () => ({kind: "not", child: predicateAtLimit({kind: "state", field_path: "system.updated_at", operand_type: {kind: "scalar", scalar_type: "datetime"}, operator: "exists"})}),
    },
  ];

  for (const {name, predicate, encode, self, mutual, alias, atLimit, tooDeep} of cases) {
    for (const [cycleName, cycle] of [["self", self], ["mutual", mutual]]) {
      const value = cycle();
      assert.doesNotThrow(() => predicate(value), `${name} ${cycleName} cycle predicate threw`);
      assert.equal(predicate(value), false, `${name} ${cycleName} cycle`);
      expectCycleError(() => encode(value), `${name} ${cycleName} cycle encoder`);
    }
    const aliased = alias();
    assert.equal(predicate(aliased), true, `${name} shared alias`);
    assert.doesNotThrow(() => encode(aliased), `${name} shared alias encoder`);

    const exact = atLimit();
    assert.equal(containerDepth(exact), maxWireJSONDepth, `${name} exact depth`);
    assert.equal(predicate(exact), true, `${name} exact depth predicate`);
    assert.doesNotThrow(() => encode(exact), `${name} exact depth encoder`);

    const excessive = tooDeep();
    assert.equal(containerDepth(excessive), maxWireJSONDepth + 1, `${name} excessive depth`);
    assert.equal(predicate(excessive), false, `${name} excessive depth predicate`);
    expectDepthError(() => encode(excessive), `${name} excessive depth encoder`);
  }
});

test("semantic recursive codecs enforce exact programmatic wire depth", () => {
  let diagnosticAtLimit = {kind: "array", array_value: []};
  for (let depth = 0; depth < 63; depth++) diagnosticAtLimit = {kind: "array", array_value: [diagnosticAtLimit]};
  assert.equal(containerDepth(diagnosticAtLimit), maxWireJSONDepth);
  assert.doesNotThrow(() => encodeDiagnosticArgumentValue(diagnosticAtLimit));
  assert.deepEqual(decodeDiagnosticArgumentValue(encodeDiagnosticArgumentValue(diagnosticAtLimit)), diagnosticAtLimit);

  let diagnosticTooDeep = {kind: "string", string_value: "leaf"};
  for (let depth = 0; depth < 64; depth++) diagnosticTooDeep = {kind: "array", array_value: [diagnosticTooDeep]};
  assert.equal(containerDepth(diagnosticTooDeep), maxWireJSONDepth + 1);
  expectDepthError(() => encodeDiagnosticArgumentValue(diagnosticTooDeep));

  for (const [name, encode, leaf] of [
    ["RecipePredicate", encodeRecipePredicate, {kind: "field", field: "display_name", operand_type: {kind: "scalar", scalar_type: "string"}, operator: "exists"}],
    ["RecipeRowPredicate", encodeRecipeRowPredicate, {kind: "state", field_path: "system.updated_at", operand_type: {kind: "scalar", scalar_type: "datetime"}, operator: "exists"}],
  ]) {
    let value = leaf;
    for (let depth = 2; depth < maxWireJSONDepth; depth++) value = {kind: "not", child: value};
    assert.equal(containerDepth(value), maxWireJSONDepth, name);
    assert.doesNotThrow(() => encode(value), name);
    const tooDeep = {kind: "not", child: value};
    assert.equal(containerDepth(tooDeep), maxWireJSONDepth + 1, name);
    expectDepthError(() => encode(tooDeep), name);
  }
});

test("engine codecs apply the shared cycle and depth preflight", async () => {
  const aliased = await readFixture("compile-request.json");
  const shared = {enabled: true};
  aliased.extensions = {"example.left": shared, "example.right": shared};
  assert.equal(isCompileRequestEnvelope(aliased), true);
  assert.doesNotThrow(() => encodeCompileRequestEnvelope(aliased));

  const cyclic = await readFixture("compile-request.json");
  const self = {};
  self.self = self;
  cyclic.extensions = {"example.cycle": self};
  assert.equal(isCompileRequestEnvelope(cyclic), false);
  expectCycleError(() => encodeCompileRequestEnvelope(cyclic));

  const atLimit = await readFixture("compile-request.json");
  let extension = "leaf";
  for (let depth = 0; depth < maxWireJSONDepth - 2; depth++) extension = [extension];
  atLimit.extensions = {"example.depth": extension};
  assert.equal(containerDepth(atLimit), maxWireJSONDepth);
  assert.equal(isCompileRequestEnvelope(atLimit), true);
  assert.doesNotThrow(() => encodeCompileRequestEnvelope(atLimit));

  const tooDeep = await readFixture("compile-request.json");
  tooDeep.extensions = {"example.depth": [extension]};
  assert.equal(containerDepth(tooDeep), maxWireJSONDepth + 1);
  assert.equal(isCompileRequestEnvelope(tooDeep), false);
  expectDepthError(() => encodeCompileRequestEnvelope(tooDeep));
});

test("canonical byte limit uses emitted bytes for escaped characters and multibyte keys", async (context) => {
  const byteLength = (value) => new TextEncoder().encode(value).length;
  for (const fill of ["<", ">", "&", "\u2028", "\u2029"]) await context.test(`text U+${fill.codePointAt(0).toString(16)}`, () => {
    const base = {field_path: "p", include_in_embedding: false, lexical_weight: 1, text: ""};
    const emptyBytes = byteLength(encodeSearchField(base));
    const unitBytes = byteLength(encodeSearchField({...base, text: fill})) - emptyBytes;
    const available = maxWireJSONBytes - emptyBytes;
    const text = fill.repeat(Math.floor(available / unitBytes)) + "a".repeat(available % unitBytes);
    assert.equal(byteLength(encodeSearchField({...base, text})), maxWireJSONBytes);
    assert.throws(() => encodeSearchField({...base, text: text + "a"}), TypeError);
  });
  for (const key of ["界", "😀"]) await context.test(`key U+${key.codePointAt(0).toString(16)}`, () => {
    const emptyBytes = byteLength(encodeJsonValue({[key]: ""}));
    const text = "a".repeat(maxWireJSONBytes - emptyBytes);
    assert.equal(byteLength(encodeJsonValue({[key]: text})), maxWireJSONBytes);
    assert.throws(() => encodeJsonValue({[key]: text + "a"}), TypeError);
  });
});

test("shared response-envelope mutations are rejected before blob resolution", async (context) => {
  const corpus = await readCorpus();
  for (const vector of corpus.mutation_cases) await context.test(vector.name, async () => {
    const value = await readFixture(vector.fixture);
    switch (vector.mutation) {
      case "add_valid_failure":
        value.failure = {category: "invariant", code: "engine.invariant", message: "safe", retryable: false};
        break;
      case "add_valid_pack_variant": {
        const pack = await readFixture("compile-success-pack.json");
        value.payload.normalized_artifact.pack = pack.payload.normalized_artifact.pack;
        break;
      }
      case "remove_pack_variant":
        delete value.payload.normalized_artifact.pack;
        break;
      case "set_failed":
        value.outcome = "failed";
        break;
      case "set_cancelled":
        value.outcome = "cancelled";
        break;
      case "add_valid_success_payload": {
        const success = await readFixture("compile-success.json");
        value.payload = success.payload;
        break;
      }
      case "remove_project_graph":
        delete value.payload.normalized_artifact.graph_hash;
        break;
      case "add_pack_graph":
        value.payload.normalized_artifact.graph_hash = "sha256:" + "e".repeat(64);
        break;
      case "add_pack_search_document": {
        const success = await readFixture("compile-success.json");
        value.payload.normalized_artifact.search_documents = success.payload.normalized_artifact.search_documents;
        break;
      }
      case "corrupt_project_media_type":
        value.payload.normalized_artifact.project.artifact_json.media_type = "application/json";
        break;
      case "set_project_artifact_session_lifetime":
        value.payload.normalized_artifact.project.artifact_json.lifetime = "session";
        break;
      case "set_project_artifact_persistent_lifetime":
        value.payload.normalized_artifact.project.artifact_json.lifetime = "persistent";
        break;
      case "set_project_canonical_session_lifetime":
        value.payload.normalized_artifact.project.canonical_json.lifetime = "session";
        break;
      case "set_project_canonical_persistent_lifetime":
        value.payload.normalized_artifact.project.canonical_json.lifetime = "persistent";
        break;
      case "set_pack_artifact_session_lifetime":
        value.payload.normalized_artifact.pack.artifact_json.lifetime = "session";
        break;
      case "set_pack_artifact_persistent_lifetime":
        value.payload.normalized_artifact.pack.artifact_json.lifetime = "persistent";
        break;
      case "set_pack_canonical_session_lifetime":
        value.payload.normalized_artifact.pack.canonical_json.lifetime = "session";
        break;
      case "set_pack_canonical_persistent_lifetime":
        value.payload.normalized_artifact.pack.canonical_json.lifetime = "persistent";
        break;
      default:
        assert.fail(`unknown mutation ${vector.mutation}`);
    }
    assert.throws(() => decodeCompileResponseEnvelope(JSON.stringify(value)));
  });
});

test("shared compile request mode/root mutations are rejected", async (context) => {
  const corpus = await readCorpus();
  for (const vector of corpus.request_mutation_cases) await context.test(vector.name, async () => {
    const value = await readFixture(vector.fixture);
    switch (vector.mutation) {
      case "add_project_root":
        value.payload.root_pack_id = "publisher/root";
        break;
      case "set_pack_without_root":
        value.payload.mode = "pack";
        break;
      case "set_pack_empty_root":
        value.payload.mode = "pack";
        value.payload.root_pack_id = "";
        break;
      case "set_pack_with_root":
        value.payload.mode = "pack";
        value.payload.root_pack_id = "publisher/root";
        break;
      case "set_pack_bad_root":
        value.payload.mode = "pack";
        value.payload.root_pack_id = "Bad";
        value.payload.installed_pack_tree = value.payload.project_source_tree;
        value.payload.project_source_tree = [];
        break;
      case "project_asset_pack_id": {
        const source = value.payload.project_source_tree[0];
        value.payload.referenced_assets = [{origin: "project", pack_id: "publisher/pack", locator: "asset.svg", blob: source.blob, digest: source.blob.digest, media_type: "image/svg+xml"}];
        break;
      }
      case "pack_asset_without_id": {
        const source = value.payload.project_source_tree[0];
        value.payload.referenced_assets = [{origin: "pack", locator: "asset.svg", blob: source.blob, digest: source.blob.digest, media_type: "image/svg+xml"}];
        break;
      }
      case "bad_source_path":
        value.payload.project_source_tree[0].path = "../document.ldl";
        break;
      default:
        assert.fail(`unknown request mutation ${vector.mutation}`);
    }
    assert.throws(() => decodeCompileRequestEnvelope(JSON.stringify(value)));
  });
});

test("generated TypeScript codecs preserve compiler semantic authority", async () => {
  const corpusValue = async (url, name) => {
    const corpus = JSON.parse(await readFile(url, "utf8"));
    return structuredClone(corpus.canonical_cases.find((item) => item.name === name).value);
  };
  const parameter = (format, value) => ({
    id: "x",
    address: "ldl:project:p:query:q:parameter:x",
    value_type: "string",
    reserved_enum_values: [],
    required: false,
    format,
    default: {kind: "string", string_value: value},
  });
  for (const [format, value] of [["email", "first.last@example.com"], ["email", "first.last@EXAMPLE.COM"], ["ipv6", "::ffff:192.0.2.1"], ["cidr", "192.0.2.0/24"]]) {
    assert.doesNotThrow(() => decodeQueryRecipeParameter(JSON.stringify(parameter(format, value))));
  }
  for (const [format, value] of [["uri", "http://exa mple.com"], ["ipv6", "1:2:3:4:5:6:7:8:9"], ["ipv6", "1::2::3"], ["cidr", "192.0.2.1/24"]]) {
    assert.throws(() => decodeQueryRecipeParameter(JSON.stringify(parameter(format, value))));
  }
  assert.throws(() => decodeRecipeScalar(JSON.stringify({kind: "datetime", string_value: "2026-07-15T12:34:56.120Z"})));

  const reversedMembership = {kind: "field", field: "id", operand_type: {kind: "scalar", scalar_type: "string"}, operator: "in", value: {kind: "scalar_set", scalar_values: [{kind: "string", string_value: "z"}, {kind: "string", string_value: "a"}]}};
  assert.doesNotThrow(() => decodeRecipePredicate(JSON.stringify(reversedMembership)));

  let query = await corpusValue(queryAuthorityCorpusURL, "query_recipe_minimal");
  query.where = {kind: "field", field: "from", operator: "exists", operand_type: {kind: "address", address_kind: "entity"}};
  assert.throws(() => decodeQueryRecipe(JSON.stringify(query)));
  query = await corpusValue(queryAuthorityCorpusURL, "query_recipe_minimal");
  query.relation_where = {kind: "field", field: "layer", operator: "exists", operand_type: {kind: "address", address_kind: "layer"}};
  assert.throws(() => decodeQueryRecipe(JSON.stringify(query)));

  let view = await corpusValue(viewExportSemanticsCorpusURL, "complete_owned_view_graph");
  view.dependencies.query_addresses = [];
  assert.throws(() => decodeViewRecipe(JSON.stringify(view)));
  view = await corpusValue(viewExportSemanticsCorpusURL, "complete_owned_view_graph");
  view.dependencies.export_addresses = [];
  assert.throws(() => decodeViewRecipe(JSON.stringify(view)));
  view = await corpusValue(viewExportSemanticsCorpusURL, "complete_owned_view_graph");
  const renameExport = (source, id) => ({...structuredClone(source), id, address: `ldl:project:p:view:v:export:${id}`, filename: `${id}.json`});
  const zebra = renameExport(view.exports[0], "zebra");
  const alpha = renameExport(view.exports[0], "alpha");
  view.exports = [zebra, alpha];
  view.dependencies.export_addresses = [zebra.address, alpha.address];
  assert.doesNotThrow(() => decodeViewRecipe(JSON.stringify(view)));

  view = await corpusValue(viewExportSemanticsCorpusURL, "complete_owned_view_graph");
  const parameterAddress = "ldl:project:p:query:all:parameter:x";
  view.source.arguments = {[parameterAddress]: {kind: "string", string_value: "ldl:project:p:entity:not-a-dependency"}};
  view.dependencies.parameter_addresses = [parameterAddress];
  assert.doesNotThrow(() => decodeViewRecipe(JSON.stringify(view)));

  view = await corpusValue(viewExportSemanticsCorpusURL, "complete_owned_view_graph");
  Object.assign(view, {
    category: "diff",
    source: {kind: "diff", before: "before", after: "after", arguments: {}},
    shape: {kind: "diff", diff: {include: [], detect_moves: false}},
    exports: [],
  });
  Object.assign(view.dependencies, {query_addresses: [], export_addresses: []});
  assert.doesNotThrow(() => decodeViewRecipe(JSON.stringify(view)));
  view.dependencies.entity_addresses = ["ldl:project:p:entity:extra"];
  assert.throws(() => decodeViewRecipe(JSON.stringify(view)));
  view.dependencies.entity_addresses = [];
  view.dependencies.state_reads = [{subject_kind: "entity", field_path: "system.created_at", value_type: "datetime"}];
  assert.throws(() => decodeViewRecipe(JSON.stringify(view)));

  view = await corpusValue(viewExportSemanticsCorpusURL, "complete_owned_view_graph");
  const relationTypeAddress = "ldl:project:p:relation-type:r";
  const branchColumnAddress = `${relationTypeAddress}:column:branch`;
  view.relation_projection_overrides = {[relationTypeAddress]: {flow: {
    source_endpoint: "from", target_endpoint: "to", connector_kind: "control", branch_value_column_address: branchColumnAddress,
  }}};
  view.dependencies.relation_type_addresses = [relationTypeAddress];
  view.dependencies.column_addresses = [branchColumnAddress];
  assert.doesNotThrow(() => decodeViewRecipe(JSON.stringify(view)));
  view.dependencies.column_addresses = [];
  assert.throws(() => decodeViewRecipe(JSON.stringify(view)));

  view = await corpusValue(semanticRootAuthorityCorpusURL, "owned_table_columns_disjoint_from_reservations");
  view.relation_projection_overrides = {"ldl:project:p:relation-type:r": {table: {row_mode: "automatic", include_from: true, include_to: true, include_relation_type: true}}};
  view.dependencies.relation_type_addresses = ["ldl:project:p:relation-type:r"];
  Object.assign(view.shape.table, {automatic_relation_columns: ["from", "relation_type", "to"], columns: [], include_entity_id: false, include_type: false, include_layer: false, row_source: "automatic_relations", sorts: [{column_id: "from", direction: "ascending", absent: "last"}]});
  assert.doesNotThrow(() => decodeViewRecipe(JSON.stringify(view)));
  view.relation_projection_overrides["ldl:project:p:relation-type:r"].table = {row_mode: "automatic", include_from: false, include_to: false, include_relation_type: false};
  view.shape.table.automatic_relation_columns = [];
  assert.throws(() => decodeViewRecipe(JSON.stringify(view)));

  const exported = await corpusValue(viewExportSemanticsCorpusURL, "contract_export_svg");
  exported.source_refs = true;
  exported.requires_source_manifest = false;
  assert.throws(() => decodeExportRecipe(JSON.stringify(exported)));

  const hash = (character) => `sha256:${character.repeat(64)}`;
  const module = (path) => ({origin: {kind: "project"}, module_path: path});
  const range = (path, start = "0", end = "1") => ({...module(path), start_byte: start, end_byte: end});
  assert.throws(() => decodeChildSetHash(JSON.stringify({owner_address: "ldl:project:p:entity:e", child_kind: "query_parameter", child_addresses: [], hash: hash("a")})));
  assert.throws(() => decodeSourceBindingRecord(JSON.stringify({source_address: "ldl:project:p:view:v", target_address: "ldl:project:p:query:q:parameter:x", target_kind: "query_parameter", via: "argument", module: module("document.ldl"), range: range("document.ldl")})));

  const result = structuredClone((await readFixture("compile-success.json")).payload);
  const childSets = [
    {owner_address: "ldl:project:p", child_kind: "entity_type", child_addresses: [], hash: hash("a")},
    {owner_address: "ldl:project:p", child_kind: "relation_type", child_addresses: [], hash: hash("b")},
  ];
  result.child_set_hashes = childSets;
  assert.doesNotThrow(() => decodeCompileResult(JSON.stringify(result)));
  result.child_set_hashes = [childSets[1], childSets[0]];
  assert.throws(() => decodeCompileResult(JSON.stringify(result)));

  const semanticIndex = structuredClone((await readFixture("compile-success.json")).payload.semantic_index);
  const references = [
    {source_address: "ldl:project:p:entity:a", target_address: "ldl:project:p:entity:b", target_kind: "entity", via: "test", range: range("document.ldl")},
    {source_address: "ldl:project:p:entity:b", target_address: "ldl:project:p:entity:a", target_kind: "entity", via: "test", range: range("document.ldl")},
  ];
  semanticIndex.references = references;
  assert.doesNotThrow(() => decodeSemanticIndex(JSON.stringify(semanticIndex)));
  semanticIndex.references = [references[1], references[0]];
  assert.throws(() => decodeSemanticIndex(JSON.stringify(semanticIndex)));

  const sourceMap = structuredClone((await readFixture("compile-success.json")).payload.source_map);
  const files = [
    {origin: {kind: "project"}, module_path: "a.ldl", digest: hash("a"), byte_length: "0"},
    {origin: {kind: "project"}, module_path: "z.ldl", digest: hash("b"), byte_length: "0"},
  ];
  sourceMap.files = files;
  assert.doesNotThrow(() => decodeSourceMap(JSON.stringify(sourceMap)));
  sourceMap.files = [files[1], files[0]];
  assert.throws(() => decodeSourceMap(JSON.stringify(sourceMap)));
});
