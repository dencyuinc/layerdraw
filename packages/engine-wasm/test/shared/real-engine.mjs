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

export async function handshakeAndCompileProjectAndPack(transport, schemaDigest, suffix) {
  const handshake = await performRequest(transport, `${suffix}-handshake-exchange`, {
    control: handshakeControl(schemaDigest, `${suffix}-handshake-request`),
    blobs: [],
  });
  const handshakeEnvelope = decode(handshake.control);
  if (handshakeEnvelope.outcome !== "success") throw new Error("generated handshake failed");
  if (!/^wasm-[0-9a-f]{32}$/.test(handshakeEnvelope.payload.endpoint_instance_id)) throw new Error("Go/WASM endpoint identity was not runtime-minted");
  if (handshakeEnvelope.payload.release_manifest_digest !== releaseManifestDigest) throw new Error("verified release manifest digest did not reach the descriptor");

  const projectInput = await projectCompileCase(`${suffix}-project-request`);
  const projectControl = projectInput.control;
  const projectBlob = projectInput.blobs[0].bytes;
  const projectExchange = transport.request({exchangeID: `${suffix}-project-exchange`, ...projectInput});
  if (projectControl.byteLength !== 0 || projectBlob.byteLength !== 0) throw new Error("Project request ownership was not transferred");
  await projectExchange.accepted;
  const project = await projectExchange.response;
  if (decode(project.control).outcome !== "success" || project.blobs.length < 2) throw new Error("real Project compile failed");

  const pack = await performRequest(transport, `${suffix}-pack-exchange`, await packCompileCase(`${suffix}-pack-request`));
  if (decode(pack.control).outcome !== "success" || pack.blobs.length < 1) throw new Error("real Pack compile failed");
  return handshakeEnvelope.payload.endpoint_instance_id;
}
