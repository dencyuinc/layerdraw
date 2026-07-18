// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

export const releaseManifestDigest = `sha256:${"5".repeat(64)}`;

export async function sha256(value) {
  const digest = await crypto.subtle.digest("SHA-256", value);
  return `sha256:${[...new Uint8Array(digest)].map((byte) => byte.toString(16).padStart(2, "0")).join("")}`;
}

export function encode(value) {
  return new TextEncoder().encode(JSON.stringify(value)).buffer;
}

export function decode(value) {
  return JSON.parse(new TextDecoder("utf-8", {fatal: true}).decode(new Uint8Array(value)));
}

function decodeBase64(value) {
  const binary = atob(value);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index);
  return bytes.buffer;
}

function canonicalSemantic(value) {
  if (Array.isArray(value)) return `[${value.map(canonicalSemantic).join(",")}]`;
  if (value !== null && typeof value === "object") {
    return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${canonicalSemantic(value[key])}`).join(",")}}`;
  }
  return JSON.stringify(value);
}

function requireEqual(actual, expected, label) {
  if (canonicalSemantic(actual) !== canonicalSemantic(expected)) throw new Error(`${label} differs from the Go dispatcher oracle`);
}

function collectBlobRefs(value, result = []) {
  if (Array.isArray(value)) {
    for (const item of value) collectBlobRefs(item, result);
    return result;
  }
  if (value === null || typeof value !== "object") return result;
  const keys = Object.keys(value).sort();
  if (canonicalSemantic(keys) === canonicalSemantic(["blob_id", "digest", "lifetime", "media_type", "size"])) result.push(value);
  for (const item of Object.values(value)) collectBlobRefs(item, result);
  return result;
}

function parityInput(testCase) {
  return {
    control: decodeBase64(testCase.request.control_base64),
    blobs: testCase.request.blobs.map((blob) => ({blob_id: blob.blob_id, bytes: decodeBase64(blob.bytes_base64)})),
  };
}

export function portableParityInput(testCase) {
  const input = parityInput(testCase);
  const envelope = decode(input.control);
  const refs = collectBlobRefs(envelope.payload);
  return {
    input: envelope.payload,
    blobs: input.blobs.map((blob) => {
      const ref = refs.find((candidate) => candidate.blob_id === blob.blob_id);
      if (ref === undefined) throw new Error(`${testCase.name} input BlobRef is missing`);
      return {ref, bytes: new Uint8Array(blob.bytes)};
    }),
  };
}

export async function assertCompileParityResponse(response, testCase, engineRelease, options = {}) {
  const actual = decode(response.control);
  const expected = structuredClone(testCase.expected.response);
  expected.engine_release = engineRelease;
  requireEqual(actual, expected, `${testCase.name} canonical response semantics`);
  if (actual.outcome !== testCase.expected.outcome || !Array.isArray(actual.diagnostics)) {
    throw new Error(`${testCase.name} outcome differs from the Go dispatcher oracle`);
  }
  if (actual.outcome === "success" && (actual.diagnostics.length !== 0 ||
      !/^sha256:[0-9a-f]{64}$/.test(actual.payload?.definition_hash) || !Array.isArray(actual.payload?.subject_semantic_hashes) ||
      actual.payload.subject_semantic_hashes.length === 0)) throw new Error(`${testCase.name} semantic hash authority is incomplete`);
  if (actual.outcome === "rejected" && (actual.payload !== undefined || actual.diagnostics.length === 0)) {
    throw new Error(`${testCase.name} rejection is not closed`);
  }
  if ((actual.outcome === "failed" || actual.outcome === "cancelled") &&
      (actual.payload !== undefined || actual.failure === undefined)) throw new Error(`${testCase.name} failure is not closed`);

  const expectedIDs = testCase.expected.blobs.map((blob) => blob.blob_id);
  const actualIDs = response.blobs.map((blob) => blob.blob_id);
  if (options.orderedBlobs !== false) {
    requireEqual(actualIDs, expectedIDs, `${testCase.name} ordered output blob IDs`);
  } else {
    requireEqual([...actualIDs].sort(), [...expectedIDs].sort(), `${testCase.name} output blob IDs`);
  }
  const refs = collectBlobRefs(actual);
  if (refs.length !== testCase.expected.blobs.length) throw new Error(`${testCase.name} response blob reference set is incomplete`);
  for (let index = 0; index < testCase.expected.blobs.length; index += 1) {
    const expectedBlob = testCase.expected.blobs[index];
    const actualBlob = response.blobs.find((blob) => blob.blob_id === expectedBlob.blob_id);
    const ref = refs.find((candidate) => candidate.blob_id === expectedBlob.blob_id);
    if (actualBlob === undefined || ref === undefined) throw new Error(`${testCase.name} output blob publication is incomplete`);
    requireEqual(ref, {
      blob_id: expectedBlob.blob_id,
      digest: expectedBlob.digest,
      lifetime: expectedBlob.lifetime,
      media_type: expectedBlob.media_type,
      size: expectedBlob.size,
    }, `${testCase.name} ${expectedBlob.blob_id} metadata`);
    const expectedBytes = new Uint8Array(decodeBase64(expectedBlob.bytes_base64));
    const actualBytes = new Uint8Array(actualBlob.bytes);
    if (actualBytes.byteLength !== Number(expectedBlob.size) || actualBytes.byteLength !== expectedBytes.byteLength ||
        actualBytes.some((byte, offset) => byte !== expectedBytes[offset])) throw new Error(`${testCase.name} ${expectedBlob.blob_id} bytes differ from Go`);
    if (await sha256(actualBlob.bytes) !== expectedBlob.digest) throw new Error(`${testCase.name} ${expectedBlob.blob_id} digest differs from Go`);
  }
}

export async function assertPortableCompileParityOutcome(outcome, testCase, engineRelease) {
  if (outcome.origin !== "engine" || outcome.outcome !== testCase.expected.outcome) {
    const actual = [outcome.origin, outcome.outcome, outcome.reason]
      .filter((part) => part !== undefined)
      .join("/");
    throw new Error(`${testCase.name} portable client outcome ${actual} differs from Go`);
  }
  await assertCompileParityResponse({
    control: encode(outcome.response),
    blobs: outcome.blobs.map((blob) => ({
      blob_id: blob.ref.blob_id,
      bytes: blob.bytes.slice().buffer,
    })),
  }, testCase, engineRelease, {orderedBlobs: false});
}

function blobRef(blobID, bytes, mediaType) {
  return sha256(bytes).then((digest) => ({
    blob_id: blobID,
    digest,
    lifetime: "request",
    media_type: mediaType,
    size: String(bytes.byteLength),
  }));
}

const queryWorkflowSource = `project p "Project" {}
layers {
  app "Application" @10
}
entity_type service "Service" {
  representation shape rect
}
relation_type link "Link" dependency {
  duplicate_policy allow
  from source types [service] layers [app]
  to target types [service] layers [app]
  label "links"
}
entities service @app {
  alpha "Alpha"
  beta "Beta"
}
relations link {
  alpha_beta: alpha -> beta
}
query scope "Scope" {
  parameters {
    environment enum [prod, dev] default prod
  }
  select {
    entity_types [service]
    relation_types [link]
    roots [alpha]
  }
  result [seed_entities, induced_relations]
}
view context "Context" context {
  source query scope { environment: prod }
  context {
    group_by none
    outgoing
  }
  export json json "context.json" {
    fidelity lossless
    source_refs
  }
}
`;

const queryLimits = Object.freeze({max_items: "128", max_output_bytes: "65536"});
const queryAddress = "ldl:project:p:query:scope";
const viewAddress = "ldl:project:p:view:context";
const queryParameterAddress = `${queryAddress}:parameter:environment`;

async function queryWorkflowCompileInput() {
  const bytes = new TextEncoder().encode(queryWorkflowSource);
  const ref = await blobRef("query-workflow-source", bytes, "text/plain; charset=utf-8");
  return {
    input: {
      entry_path: "main.ldl",
      installed_pack_tree: [],
      mode: "project",
      project_source_tree: [{path: "main.ldl", blob: ref}],
      referenced_assets: [],
      resolved_dependencies: {format: "layerdraw-resolved", format_version: 1, installs: [], language: 1},
      resource_limits: {},
    },
    blob: {ref, bytes},
  };
}

function assertQueryPayload(payload, generation, label) {
  if (payload === undefined) throw new Error(`${label} omitted its payload`);
  requireEqual(payload.document_generation, generation, `${label} document generation`);
  requireEqual(payload.result, {
    arguments: {[queryParameterAddress]: {kind: "enum", string_value: "prod"}},
    cycle_refs: [],
    diagnostics: [],
    induced_relation_addresses: [],
    path_relation_addresses: [],
    paths: [{entity_addresses: ["ldl:project:p:entity:alpha"], relation_addresses: []}],
    primary_entity_addresses: ["ldl:project:p:entity:alpha"],
    query_address: queryAddress,
    reached_entity_addresses: [],
    seed_entity_addresses: ["ldl:project:p:entity:alpha"],
    selected_relation_addresses: [],
    state_input: {kind: "none"},
    state_policy: "none",
    state_reads: [],
    support_entity_addresses: [],
    traversed_entity_addresses: [],
  }, `${label} result`);
  if (payload.returned_items !== "3") throw new Error(`${label} returned_items is not exact`);
  const logical = structuredClone(payload);
  logical.returned_bytes = "0";
  const expectedBytes = new TextEncoder().encode(canonicalSemantic(logical)).byteLength;
  if (payload.returned_bytes !== String(expectedBytes)) throw new Error(`${label} returned_bytes is not exact`);
}

export async function executePortableQueryClientWorkflow(client, suffix) {
  const source = await queryWorkflowCompileInput();
  const open = await client.workbench.openDocument({
    compile_input: source.input,
    requested_limits: queryLimits,
  }, {
    blobs: [source.blob],
    requestId: `${suffix}-open`,
  });
  if (open.origin !== "engine" || open.outcome !== "success" || open.response.payload === undefined) {
    throw new Error(`${suffix} query workflow could not open its document`);
  }
  const opened = open.response.payload;
  if (opened.capabilities.execute_query !== true) throw new Error(`${suffix} document did not advertise execute_query`);

  const executed = await client.workbench.executeQuery({
    arguments: {[queryParameterAddress]: {kind: "enum", string_value: "prod"}},
    document_generation: opened.document_generation,
    limits: queryLimits,
    query_address: queryAddress,
  }, {requestId: `${suffix}-execute`});
  if (executed.origin !== "engine" || executed.outcome !== "success") {
    throw new Error(`${suffix} query workflow did not succeed`);
  }
  assertQueryPayload(executed.response.payload, opened.document_generation, `${suffix} query`);

  if (opened.capabilities.materialize_view !== true) throw new Error(`${suffix} document did not advertise materialize_view`);
  const materialized = await client.workbench.materializeView({
    kind: "query",
    limits: queryLimits,
    query: {
      document_generation: opened.document_generation,
      query_result: executed.response.payload.result,
    },
    view_address: viewAddress,
  }, {requestId: `${suffix}-materialize`});
  if (materialized.origin !== "engine" || materialized.outcome !== "success" ||
      materialized.response.payload?.view_data?.shape?.kind !== "context" ||
      materialized.response.payload.view_data.context === undefined) {
    throw new Error(`${suffix} ViewData materialization did not succeed`);
  }

  if (opened.capabilities.plan_export !== true || client.getCapabilities().operations["engine.plan_export"]?.enabled !== true) {
    throw new Error(`${suffix} endpoint did not advertise plan_export`);
  }
  const exportRecipe = {
    address: `${viewAddress}:export:json`, effective_maximum_fidelity: "lossless",
    exporter_profile: {
      format: "json", id: "layerdraw/json@1",
      registry_digest: "sha256:064941009d55baaa542dd72819107d60f47782f7fec9cc7735260f512cba0c9f",
      registry_schema_version: 1,
      specification_digest: "sha256:9140bcd68dd8172a6520b4d6ad468cd1a52006e487e2276997768f06f14375b7",
    },
    extension: ".json", fidelity: "lossless", fidelity_basis: "native", filename: "context.json", format: "json", id: "json",
    native_maximum_fidelity: "lossless", options: { diagnostics: false, kind: "json", state_summary: false },
    requires_source_manifest: false, source_refs: true, view_address: viewAddress,
  };
  const planViewData = structuredClone(materialized.response.payload.view_data);
  planViewData.revision.revision_id = "export-plan-parity-revision";
  const planInput = {
    recipe: exportRecipe,
    resolved_requirements: {
      schema_version: 1,
      exporter_profile: exportRecipe.exporter_profile,
      serializer_profile: exportRecipe.exporter_profile,
      required_asset_digests: [],
      required_font_digests: [`sha256:${"b".repeat(64)}`],
    },
    view_data: planViewData,
  };
  const planned = await client.workbench.planExport(planInput, {requestId: `${suffix}-plan-export`});
  const repeated = await client.workbench.planExport(structuredClone(planInput), {requestId: `${suffix}-plan-export-repeat`});
  if (planned.origin !== "engine" || planned.outcome !== "success" || repeated.origin !== "engine" || repeated.outcome !== "success" ||
      canonicalSemantic(planned.response.payload?.export_plan) !== canonicalSemantic(repeated.response.payload?.export_plan) ||
      planned.response.payload?.export_plan?.view_data_hash === undefined || planned.response.payload.export_plan.representations.length === 0) {
    throw new Error(`${suffix} deterministic ExportPlan generation did not succeed`);
  }
  if (typeof process !== "undefined" && process.env.LAYERDRAW_CAPTURE_EXPORT_PLAN === "1") {
    console.log(`LAYERDRAW_EXPORT_PLAN_GOLDEN=${JSON.stringify({schema_version: 1, input: planInput, export_plan: planned.response.payload.export_plan})}`);
  }

  const rejected = await client.workbench.executeQuery({
    arguments: {[`${queryAddress}:parameter:unknown`]: {kind: "enum", string_value: "prod"}},
    document_generation: opened.document_generation,
    limits: queryLimits,
    query_address: queryAddress,
  }, {requestId: `${suffix}-rejected`});
  if (rejected.origin !== "engine" || rejected.outcome !== "rejected" ||
      rejected.response.payload !== undefined || rejected.response.failure !== undefined ||
      rejected.response.diagnostics.length !== 1 || rejected.response.diagnostics[0].code !== "LDL1601") {
    throw new Error(`${suffix} query rejection contract drifted`);
  }

  const closed = await client.workbench.closeDocument({
    document_generation: opened.document_generation,
    document_handle: opened.document_handle,
  }, {requestId: `${suffix}-close`});
  if (closed.origin !== "engine" || closed.outcome !== "success") {
    throw new Error(`${suffix} query workflow could not close its document`);
  }
  return {input: planInput, export_plan: planned.response.payload.export_plan};
}

export function handshakeControl(schemaDigest, requestID) {
  return encode({
    operation: "engine.handshake",
    payload: {
      client_release: "0.0.0-dev",
      protocols: [{
        name: "engine",
        supported_range: "1.0..1.0",
        versions: [{version: "1.0", schema_digest: schemaDigest}],
      }],
      required_capabilities: [
        "engine.compile", "engine.open_document", "engine.execute_query", "engine.materialize_view", "engine.close_document",
      ],
      optional_capabilities: [],
    },
    protocol: {name: "engine", version: "1.0"},
    request_id: requestID,
  });
}

export async function projectCompileCase(requestID) {
  const source = new TextEncoder().encode('project p "Project" {}');
  const reference = await blobRef("project-source", source, "text/plain; charset=utf-8");
  return {
    control: encode({
      operation: "engine.compile",
      payload: {
        entry_path: "main.ldl",
        installed_pack_tree: [],
        mode: "project",
        project_source_tree: [{path: "main.ldl", blob: reference}],
        referenced_assets: [],
        resolved_dependencies: {format: "layerdraw-resolved", format_version: 1, installs: [], language: 1},
        resource_limits: {},
      },
      protocol: {name: "engine", version: "1.0"},
      request_id: requestID,
    }),
    blobs: [{blob_id: "project-source", bytes: source.buffer}],
  };
}

export async function packCompileCase(requestID) {
  const source = new TextEncoder().encode('entity_type service "Service" {\n  representation shape rect\n}\nexport { service }\n');
  const manifest = new TextEncoder().encode(JSON.stringify({
    format: "layerdraw-pack",
    format_version: 1,
    id: "pub/schema",
    name: "schema",
    version: "1.0.0",
    language: 1,
    entry: "pack.ldl",
    dependencies: {},
  }));
  const sourceReference = await blobRef("pack-source", source, "text/plain; charset=utf-8");
  const manifestReference = await blobRef("pack-manifest", manifest, "application/json");
  return {
    control: encode({
      operation: "engine.compile",
      payload: {
        entry_path: "pack.ldl",
        installed_pack_tree: [{path: "pack/schema/pack.ldl", blob: sourceReference}],
        mode: "pack",
        project_source_tree: [],
        referenced_assets: [],
        resolved_dependencies: {
          format: "layerdraw-resolved",
          format_version: 1,
          installs: [{
            install_name: "schema",
            canonical_id: "pub/schema",
            version: "1.0.0",
            digest: `sha256:${"a".repeat(64)}`,
            path: "pack/schema",
            entry: "pack.ldl",
            files: [{path: "pack.ldl", digest: sourceReference.digest}],
            dependencies: [],
            manifest_path: "manifest.json",
            manifest: manifestReference,
          }],
          language: 1,
        },
        resource_limits: {},
        root_pack_id: "pub/schema",
      },
      protocol: {name: "engine", version: "1.0"},
      request_id: requestID,
    }),
    blobs: [
      {blob_id: "pack-manifest", bytes: manifest.buffer},
      {blob_id: "pack-source", bytes: source.buffer},
    ],
  };
}

export async function performRequest(transport, exchangeID, input) {
  const exchange = transport.request({exchangeID, ...input});
  await exchange.accepted;
  return exchange.response;
}

export async function assertExportPlanTransportGolden(transport, golden, engineRelease, suffix) {
  if (golden.schema_version !== 1 || golden.input === undefined || golden.export_plan === undefined) {
    throw new Error("ExportPlan transport golden is incompatible");
  }
  const response = await performRequest(transport, `${suffix}-plan-export-exchange`, {
    control: encode({
      operation: "engine.plan_export",
      payload: golden.input,
      protocol: {name: "engine", version: "1.0"},
      request_id: `${suffix}-plan-export-request`,
    }),
    blobs: [],
  });
  const envelope = decode(response.control);
  if (envelope.outcome !== "success" || envelope.engine_release !== engineRelease || response.blobs.length !== 0) {
    throw new Error(`${suffix} ExportPlan request did not succeed through the real transport`);
  }
  requireEqual(envelope.payload?.export_plan, golden.export_plan, `${suffix} ExportPlan transport golden`);
}

export async function handshakeAndCompileCorpus(
  transport,
  schemaDigest,
  corpus,
  engineRelease,
  suffix,
  caseNames,
) {
  if (corpus.schema_version !== 1 || corpus.engine_release_variable !== "$engine_release" ||
      corpus.cases.length !== 10 || corpus.required_features.length !== 10 || corpus.normalization.length !== 3) {
    throw new Error("transport-neutral parity corpus is incompatible");
  }
  const handshake = await performRequest(transport, `${suffix}-handshake-exchange`, {
    control: handshakeControl(schemaDigest, `${suffix}-handshake-request`),
    blobs: [],
  });
  const handshakeEnvelope = decode(handshake.control);
  if (handshakeEnvelope.outcome !== "success") throw new Error("generated handshake failed");
  if (handshakeEnvelope.engine_release !== engineRelease || handshakeEnvelope.payload.host_release !== engineRelease) throw new Error("Go/WASM release authority drifted");
  if (!/^wasm-[0-9a-f]{32}$/.test(handshakeEnvelope.payload.endpoint_instance_id)) throw new Error("Go/WASM endpoint identity was not runtime-minted");
  if (handshakeEnvelope.payload.release_manifest_digest !== releaseManifestDigest) throw new Error("verified release manifest digest did not reach the descriptor");

  const selectedNames = caseNames === undefined ? undefined : new Set(caseNames);
  if (
    selectedNames !== undefined &&
    (selectedNames.size !== caseNames.length ||
      [...selectedNames].some(
        (name) => !corpus.cases.some((testCase) => testCase.name === name),
      ))
  ) {
    throw new Error("selected parity corpus cases are invalid");
  }
  for (const testCase of corpus.cases) {
    if (selectedNames !== undefined && !selectedNames.has(testCase.name)) continue;
    if (testCase.execution === "cancel") continue;
    const input = parityInput(testCase);
    const owned = [input.control, ...input.blobs.map((blob) => blob.bytes)];
    let exchange;
    try {
      exchange = transport.request({exchangeID: `${suffix}-${testCase.name}-exchange`, ...input});
    } catch (error) {
      throw new Error(`${testCase.name} transport admission failed`, {cause: error});
    }
    if (owned.some((buffer) => buffer.byteLength !== 0)) throw new Error(`${testCase.name} request ownership was not transferred`);
    await exchange.accepted;
    await assertCompileParityResponse(await exchange.response, testCase, engineRelease);
  }
  return handshakeEnvelope.payload.endpoint_instance_id;
}

function validateViewDataCorpus(corpus) {
  if (corpus.schema_version !== 1 || corpus.engine_release_variable !== "$engine_release" ||
      corpus.documents.length !== 6 || corpus.cases.length !== 20 ||
      corpus.required_shapes.length !== 7 || corpus.required_projection_modes.length !== 14 ||
      corpus.required_source_kinds.length !== 2 || corpus.required_state_policies.length !== 3 ||
      corpus.required_failure_classes.length !== 4) {
    throw new Error("ViewData conformance corpus is incompatible");
  }
}

function viewDataDocument(corpus, id) {
  const document = corpus.documents.find((candidate) => candidate.id === id);
  if (document === undefined) throw new Error(`ViewData document ${id} is absent`);
  return document;
}

function viewDataClientBlobs(document) {
  const refs = collectBlobRefs(document.input);
  return document.blobs.map((blob) => {
    const ref = refs.find((candidate) => candidate.blob_id === blob.blob_id);
    if (ref === undefined) throw new Error(`${document.id} BlobRef ${blob.blob_id} is absent`);
    return {ref, bytes: new Uint8Array(decodeBase64(blob.bytes_base64))};
  });
}

function normalizeViewDataResponse(value, handles) {
  if (Array.isArray(value)) return value.map((item) => normalizeViewDataResponse(item, handles));
  if (value !== null && typeof value === "object") {
    const result = {};
    for (const [key, child] of Object.entries(value)) {
      if (key === "engine_release") result[key] = "$engine_release";
      else if (key === "returned_bytes") result[key] = "$returned_bytes";
      else if (key === "endpoint_instance_id") result[key] = "$endpoint";
      else result[key] = normalizeViewDataResponse(child, handles);
    }
    return result;
  }
  if (typeof value === "string") {
    for (const [handle, id] of handles) {
      if (value === handle) return `$document:${id}`;
      if (value.startsWith(`workbench:${handle}:`)) return `$revision:${id}`;
    }
  }
  return value;
}

function assertViewDataCase(outcome, testCase, handles, label) {
  const normalized = normalizeViewDataResponse(structuredClone(outcome), handles);
  requireEqual(normalized, testCase.expected.normalized_response, `${label} ${testCase.name}`);
  const publishes = normalized.outcome === "success" && normalized.payload?.view_data !== undefined;
  if (normalized.outcome !== testCase.expected.outcome || publishes !== testCase.expected.publishes_view_data) {
    throw new Error(`${label} ${testCase.name} terminality differs from the Go oracle`);
  }
  if (normalized.outcome !== "success" && normalized.payload?.view_data !== undefined) {
    throw new Error(`${label} ${testCase.name} published partial ViewData`);
  }
}

function queryMaterializeInput(testCase, generation, queryResult) {
  const result = structuredClone(queryResult);
  if (testCase.mutation === "mismatched_query") result.query_address = "ldl:project:p:query:missing";
  const query = {document_generation: generation, query_result: result};
  if (testCase.source.state_snapshot !== undefined) query.state_snapshot = testCase.source.state_snapshot;
  return {kind: "query", limits: testCase.limits, query, view_address: testCase.view_address};
}

async function executeViewDataMaterializeCases(driver, corpus, label) {
  for (const testCase of corpus.cases) {
    if (testCase.execution !== "materialize") continue;
    const opened = new Map();
    const handles = new Map();
    const open = async (id) => {
      if (opened.has(id)) return opened.get(id);
      const payload = await driver.open(viewDataDocument(corpus, id), corpus.operation_limits, `${testCase.name}-${id}`);
      opened.set(id, payload);
      handles.set(payload.document_handle.value, id);
      return payload;
    };
    try {
      let input;
      if (testCase.source.kind === "query") {
        const document = await open(testCase.source.document);
        const executed = await driver.query({
          arguments: testCase.source.arguments ?? {},
          document_generation: document.document_generation,
          limits: corpus.operation_limits,
          query_address: testCase.source.query_address,
        }, `viewdata-${testCase.name}-query`);
        input = queryMaterializeInput(testCase, document.document_generation, executed.result);
      } else if (testCase.source.kind === "diff") {
        const recipe = await open(testCase.source.recipe_document);
        const before = await open(testCase.source.before_document);
        const after = await open(testCase.source.after_document);
        input = {
          kind: "diff",
          limits: testCase.limits,
          diff: {
            recipe_generation: recipe.document_generation,
            before_generation: before.document_generation,
            after_generation: after.document_generation,
          },
          view_address: testCase.view_address,
        };
      } else {
        throw new Error(`${testCase.name} has unsupported source kind`);
      }
      const outcome = await driver.materialize(input, `viewdata-${testCase.name}-materialize`);
      assertViewDataCase(outcome, testCase, handles, label);
    } finally {
      for (const [id, document] of opened) {
        await driver.close({
          document_generation: document.document_generation,
          document_handle: document.document_handle,
        }, `viewdata-${testCase.name}-close-${id}`);
      }
    }
  }
}

export async function executeViewDataTransportCorpus(transport, schemaDigest, corpus, engineRelease, suffix, alreadyNegotiated = false) {
  validateViewDataCorpus(corpus);
  if (!alreadyNegotiated) {
    const handshake = await performRequest(transport, `${suffix}-viewdata-handshake-exchange`, {
      control: encode({
        operation: "engine.handshake",
        payload: {
          client_release: "0.0.0-dev",
          protocols: [{name: "engine", supported_range: "1.0..1.0", versions: [{version: "1.0", schema_digest: schemaDigest}]}],
          required_capabilities: ["engine.open_document", "engine.execute_query", "engine.materialize_view", "engine.close_document"],
          optional_capabilities: [],
        },
        protocol: {name: "engine", version: "1.0"},
        request_id: `${suffix}-viewdata-handshake`,
      }),
      blobs: [],
    });
    const handshakeEnvelope = decode(handshake.control);
    if (handshakeEnvelope.outcome !== "success" || handshakeEnvelope.engine_release !== engineRelease) {
      throw new Error(`${suffix} ViewData handshake failed`);
    }
  }
  const request = async (operation, payload, requestID, blobs = []) => {
    const response = await performRequest(transport, `${suffix}-${requestID}-exchange`, {
      control: encode({operation, payload, protocol: {name: "engine", version: "1.0"}, request_id: requestID}),
      blobs,
    });
    return decode(response.control);
  };
  const driver = {
    async open(document, limits, id) {
      const response = await request("engine.open_document", {compile_input: document.input, requested_limits: limits}, `viewdata-${id}-open`,
        document.blobs.map((blob) => ({blob_id: blob.blob_id, bytes: decodeBase64(blob.bytes_base64)})));
      if (response.outcome !== "success" || response.payload === undefined) throw new Error(`${id} open failed`);
      return response.payload;
    },
    async query(input, id) {
      const response = await request("engine.execute_query", input, id);
      if (response.outcome !== "success" || response.payload === undefined) throw new Error(`${id} query failed`);
      return response.payload;
    },
    materialize: (input, id) => request("engine.materialize_view", input, id),
    async close(input, id) {
      const response = await request("engine.close_document", input, id);
      if (response.outcome !== "success") throw new Error(`${id} close failed`);
    },
  };
  await executeViewDataMaterializeCases(driver, corpus, suffix);
  const malformed = corpus.cases.find((testCase) => testCase.execution === "malformed_wire");
  const malformedResponse = await request("engine.materialize_view", {}, `viewdata-${malformed.name}-materialize`);
  if (malformedResponse.outcome === "success" || malformedResponse.payload?.view_data !== undefined) {
    throw new Error(`${suffix} malformed wire published ViewData`);
  }
  return corpus.cases.filter((testCase) => testCase.execution === "materialize").map((testCase) => testCase.name);
}

export async function executeViewDataClientCorpus(client, corpus, suffix) {
  validateViewDataCorpus(corpus);
  const driver = {
    async open(document, limits, id) {
      const outcome = await client.workbench.openDocument({compile_input: structuredClone(document.input), requested_limits: limits}, {
        blobs: viewDataClientBlobs(document), requestId: `viewdata-${id}-open`,
      });
      if (outcome.origin !== "engine" || outcome.outcome !== "success" || outcome.response.payload === undefined) throw new Error(`${id} open failed`);
      return outcome.response.payload;
    },
    async query(input, id) {
      const outcome = await client.workbench.executeQuery(input, {requestId: id});
      if (outcome.origin !== "engine" || outcome.outcome !== "success" || outcome.response.payload === undefined) throw new Error(`${id} query failed`);
      return outcome.response.payload;
    },
    async materialize(input, id) {
      const outcome = await client.workbench.materializeView(input, {requestId: id});
      if (outcome.origin !== "engine") throw new Error(`${id} did not reach the Engine`);
      return outcome.response;
    },
    async close(input, id) {
      const outcome = await client.workbench.closeDocument(input, {requestId: id});
      if (outcome.origin !== "engine" || outcome.outcome !== "success") throw new Error(`${id} close failed`);
    },
  };
  await executeViewDataMaterializeCases(driver, corpus, suffix);

  const malformed = corpus.cases.find((testCase) => testCase.execution === "malformed_wire");
  let malformedRejected = false;
  try {
    await client.workbench.materializeView({}, {requestId: `viewdata-${malformed.name}-materialize`});
  } catch (error) {
    malformedRejected = error?.code === "INVALID_ARGUMENT";
  }
  if (!malformedRejected) throw new Error(`${suffix} typed client accepted malformed ViewData input`);

  const cancelledCase = corpus.cases.find((testCase) => testCase.execution === "cancel");
  const sourceCase = corpus.cases.find((testCase) => testCase.name === "context");
  const document = viewDataDocument(corpus, sourceCase.source.document);
  const opened = await driver.open(document, corpus.operation_limits, `${cancelledCase.name}-cancel`);
  try {
    const executed = await driver.query({
      arguments: sourceCase.source.arguments ?? {}, document_generation: opened.document_generation,
      limits: corpus.operation_limits, query_address: sourceCase.source.query_address,
    }, `viewdata-${cancelledCase.name}-query`);
    const controller = new AbortController();
    controller.abort();
    const cancelled = await client.workbench.materializeView(
      queryMaterializeInput(cancelledCase, opened.document_generation, executed.result),
      {requestId: `viewdata-${cancelledCase.name}-materialize`, signal: controller.signal},
    );
    if (cancelled.origin !== "client" || cancelled.outcome !== "cancelled" || cancelled.response !== undefined) {
      throw new Error(`${suffix} cancellation published partial ViewData`);
    }
  } finally {
    await driver.close({
      document_generation: opened.document_generation,
      document_handle: opened.document_handle,
    }, `viewdata-${cancelledCase.name}-close`);
  }
  return corpus.cases.map((testCase) => testCase.name);
}
