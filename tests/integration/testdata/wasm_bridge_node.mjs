// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

const artifactDirectory = process.argv[2];
if (!artifactDirectory) {
  throw new Error("artifact directory argument is required");
}

await import(pathToFileURL(join(artifactDirectory, "wasm_exec.js")).href);
const go = new globalThis.Go();
const bytes = await readFile(join(artifactDirectory, "layerdraw-engine.wasm"));
const { instance } = await WebAssembly.instantiate(bytes, go.importObject);
const runtime = go.run(instance);

for (let attempt = 0; attempt < 100 && !globalThis.__layerdrawEngineWasmV1; attempt += 1) {
  await new Promise((resolve) => setTimeout(resolve, 0));
}
const bridge = globalThis.__layerdrawEngineWasmV1;
if (!bridge) {
  throw new Error("Go WASM bridge did not register");
}

const generation = "node-generation-1";
const releaseManifestDigest = `sha256:${"5".repeat(64)}`;
const initialized = bridge.initialize(generation, "node-endpoint-1", releaseManifestDigest);
if (!initialized.ok || initialized.endpoint_generation !== generation) {
  throw new Error(`initialization failed: ${JSON.stringify(initialized)}`);
}
if (initialized.transport_limits.max_control_bytes !== 8_388_608) {
  throw new Error("transport limits were not returned from Go");
}

const encode = (value) => new TextEncoder().encode(JSON.stringify(value)).buffer;
const decode = (buffer) => JSON.parse(new TextDecoder().decode(new Uint8Array(buffer)));
const handshake = {
  operation: "engine.handshake",
  payload: {
    client_release: "0.0.0-dev",
    protocols: [{
      name: "engine",
      supported_range: "1.0..1.0",
      versions: [{ version: "1.0", schema_digest: initialized.protocol_schema_digest }],
    }],
    required_capabilities: ["engine.compile"],
    optional_capabilities: [],
  },
  protocol: { name: "engine", version: "1.0" },
  request_id: "node-handshake-1",
};
const handshakeResult = bridge.request(generation, encode(handshake), [], []);
if (!handshakeResult.ok || decode(handshakeResult.control).outcome !== "success") {
  throw new Error(`handshake failed: ${JSON.stringify(handshakeResult)}`);
}

const sourceBytes = new TextEncoder().encode('project p "Project" {}');
const sourceDigest = `sha256:${createHash("sha256").update(sourceBytes).digest("hex")}`;
const compile = {
  operation: "engine.compile",
  payload: {
    entry_path: "main.ldl",
    installed_pack_tree: [],
    mode: "project",
    project_source_tree: [{
      path: "main.ldl",
      blob: {
        blob_id: "source",
        digest: sourceDigest,
        lifetime: "request",
        media_type: "text/plain; charset=utf-8",
        size: String(sourceBytes.byteLength),
      },
    }],
    referenced_assets: [],
    resolved_dependencies: {
      format: "layerdraw-resolved",
      format_version: 1,
      installs: [],
      language: 1,
    },
    resource_limits: {},
  },
  protocol: { name: "engine", version: "1.0" },
  request_id: "node-compile-1",
};
const compileResult = bridge.request(generation, encode(compile), ["source"], [sourceBytes.buffer]);
const compileEnvelope = compileResult.ok ? decode(compileResult.control) : null;
if (!compileResult.ok || compileEnvelope.outcome !== "success" || compileResult.blobs.length < 2) {
  throw new Error(`compile failed: ${JSON.stringify(compileResult)}`);
}
if (compileResult.blob_ids.length !== compileResult.blobs.length || compileResult.blobs.some((buffer) => !(buffer instanceof ArrayBuffer))) {
  throw new Error("output ownership table is invalid");
}

const stale = bridge.request("stale-generation", encode(handshake), [], []);
if (stale.ok || stale.failure.code !== "engine.worker.stale_generation") {
  throw new Error(`stale generation was accepted: ${JSON.stringify(stale)}`);
}
const disposed = bridge.dispose(generation);
if (!disposed.ok) {
  throw new Error(`dispose failed: ${JSON.stringify(disposed)}`);
}

void runtime;
process.stdout.write("Go WASM bridge handshake/compile/lifecycle smoke passed.\n");
process.exit(0);
