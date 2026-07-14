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

export async function assertCompileParityResponse(response, testCase, engineRelease) {
  const actual = decode(response.control);
  const expected = structuredClone(testCase.expected.response);
  expected.engine_release = engineRelease;
  requireEqual(actual, expected, `${testCase.name} canonical response semantics`);
  if (actual.outcome !== "success" || !Array.isArray(actual.diagnostics) || actual.diagnostics.length !== 0 ||
      !/^sha256:[0-9a-f]{64}$/.test(actual.payload?.definition_hash) || !Array.isArray(actual.payload?.subject_semantic_hashes) ||
      actual.payload.subject_semantic_hashes.length === 0) throw new Error(`${testCase.name} semantic hash authority is incomplete`);

  const expectedIDs = testCase.expected.blobs.map((blob) => blob.blob_id);
  const actualIDs = response.blobs.map((blob) => blob.blob_id);
  requireEqual(actualIDs, expectedIDs, `${testCase.name} ordered output blob IDs`);
  const refs = collectBlobRefs(actual);
  if (refs.length !== testCase.expected.blobs.length) throw new Error(`${testCase.name} response blob reference set is incomplete`);
  for (let index = 0; index < testCase.expected.blobs.length; index += 1) {
    const expectedBlob = testCase.expected.blobs[index];
    const actualBlob = response.blobs[index];
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

function blobRef(blobID, bytes, mediaType) {
  return sha256(bytes).then((digest) => ({
    blob_id: blobID,
    digest,
    lifetime: "request",
    media_type: mediaType,
    size: String(bytes.byteLength),
  }));
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
      required_capabilities: ["engine.compile"],
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

export async function handshakeAndCompileProjectAndPack(transport, schemaDigest, corpus, engineRelease, suffix) {
  if (corpus.schema_version !== 1 || corpus.engine_release_variable !== "$engine_release" || corpus.cases.length !== 2) {
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

  for (const testCase of corpus.cases) {
    const input = parityInput(testCase);
    const owned = [input.control, ...input.blobs.map((blob) => blob.bytes)];
    const exchange = transport.request({exchangeID: `${suffix}-${testCase.name}-exchange`, ...input});
    if (owned.some((buffer) => buffer.byteLength !== 0)) throw new Error(`${testCase.name} request ownership was not transferred`);
    await exchange.accepted;
    await assertCompileParityResponse(await exchange.response, testCase, engineRelease);
  }
  return handshakeEnvelope.payload.endpoint_instance_id;
}
