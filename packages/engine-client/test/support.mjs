// SPDX-License-Identifier: Apache-2.0

import { createHash } from "node:crypto";
import { Buffer } from "node:buffer";
import { readFile } from "node:fs/promises";
import {
  decodeApplyToHandleRequestEnvelope,
  decodeCompileRequestEnvelope,
  decodeCloseDocumentRequestEnvelope,
  decodeExecuteQueryRequestEnvelope,
  decodeFindSymbolsRequestEnvelope,
  decodeFindUsagesRequestEnvelope,
  decodeFormatScopeRequestEnvelope,
  decodeGetNeighborsRequestEnvelope,
  decodeHandshakeRequestEnvelope,
  decodeInspectSubgraphRequestEnvelope,
  decodeListModulesRequestEnvelope,
  decodeListReferencesRequestEnvelope,
  decodeOpenDocumentRequestEnvelope,
  decodeOrganizeWorkspaceRequestEnvelope,
  decodePreviewFragmentRequestEnvelope,
  decodePreviewSourcePatchRequestEnvelope,
  decodeReadDeclarationsRequestEnvelope,
  decodeReadModulesRequestEnvelope,
  decodeReadReferencesRequestEnvelope,
  decodeReadRowsRequestEnvelope,
  decodeReadScopeRequestEnvelope,
  decodeReplaceSourceTreeRequestEnvelope,
  encodeApplyToHandleResponseEnvelope,
  encodeCompileResponseEnvelope,
  encodeCloseDocumentResponseEnvelope,
  encodeExecuteQueryResponseEnvelope,
  encodeFindSymbolsResponseEnvelope,
  encodeFindUsagesResponseEnvelope,
  encodeFormatScopeResponseEnvelope,
  encodeGetNeighborsResponseEnvelope,
  encodeHandshakeResponseEnvelope,
  encodeInspectSubgraphResponseEnvelope,
  encodeListModulesResponseEnvelope,
  encodeListReferencesResponseEnvelope,
  encodeOpenDocumentResponseEnvelope,
  encodeOrganizeWorkspaceResponseEnvelope,
  encodePreviewFragmentResponseEnvelope,
  encodePreviewSourcePatchResponseEnvelope,
  encodeReadDeclarationsResponseEnvelope,
  encodeReadModulesResponseEnvelope,
  encodeReadReferencesResponseEnvelope,
  encodeReadRowsResponseEnvelope,
  encodeReadScopeResponseEnvelope,
  encodeReplaceSourceTreeResponseEnvelope,
} from "@layerdraw/protocol/engine";

const root = new URL("../../../", import.meta.url);
const fixture = async (path) =>
  JSON.parse(await readFile(new URL(path, root), "utf8"));

export const expectedReleaseDigest =
  "sha256:5555555555555555555555555555555555555555555555555555555555555555";

export const limits = Object.freeze({
  maxControlBytes: 8 * 1024 * 1024,
  maxControlDepth: 128,
  maxBlobIdBytes: 256,
  maxBuffers: 128,
  maxInputBlobBytes: 1024 * 1024,
  maxInputTotalBytes: 4 * 1024 * 1024,
  maxOutputBlobBytes: 1024 * 1024,
  maxOutputTotalBytes: 4 * 1024 * 1024,
  maxResponsePublishBytes: 12 * 1024 * 1024,
});

export const collectors = Object.freeze({
  collectCompileInputBlobRefs(input) {
    return [
      ...input.installed_pack_tree.map((file) => file.blob),
      ...input.project_source_tree.map((file) => file.blob),
      ...input.referenced_assets.map((asset) => asset.blob),
    ];
  },
  collectCompileResultBlobRefs(result) {
    const refs = [];
    for (const recipe of result.compiled_recipes.exports) refs.push(recipe.canonical_json);
    for (const recipe of result.compiled_recipes.queries) refs.push(recipe.canonical_json);
    for (const recipe of result.compiled_recipes.views) refs.push(recipe.canonical_json);
    if (result.normalized_artifact.project !== undefined) {
      refs.push(result.normalized_artifact.project.artifact_json);
      refs.push(result.normalized_artifact.project.canonical_json);
    }
    if (result.normalized_artifact.pack !== undefined) {
      refs.push(result.normalized_artifact.pack.artifact_json);
      refs.push(result.normalized_artifact.pack.canonical_json);
    }
    return refs;
  },
});

export function sha256(bytes) {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}

export function completedDigestLease(bytes) {
  let retained = bytes;
  let aborted = false;
  const result = Promise.resolve().then(() => {
    if (aborted) throw new Error("digest lease aborted");
    return new Uint8Array(createHash("sha256").update(retained).digest());
  }).finally(() => {
    retained = undefined;
  });
  const joined = result.then(
    () => undefined,
    () => undefined,
  );
  return Object.freeze({
    result,
    joined,
    abort() {
      aborted = true;
    },
  });
}

export function rejectedDigestLease(error) {
  const result = Promise.reject(error);
  const joined = result.then(
    () => undefined,
    () => undefined,
  );
  return Object.freeze({ result, joined, abort() {} });
}

export function stalledDigestLease(bytes) {
  let retained = bytes;
  let rejectResult;
  let abortCount = 0;
  let joinedSettled = false;
  const result = new Promise((_resolve, reject) => {
    rejectResult = reject;
  });
  const joined = result.then(
    () => undefined,
    () => undefined,
  ).then(() => {
    joinedSettled = true;
  });
  return Object.freeze({
    result,
    joined,
    abort() {
      if (retained === undefined) return;
      abortCount++;
      retained = undefined;
      rejectResult(new Error("digest lease hard-aborted"));
    },
    get abortCount() {
      return abortCount;
    },
    get joinedSettled() {
      return joinedSettled;
    },
    get retainsInput() {
      return retained !== undefined;
    },
  });
}

export function makePortableRequest(source = "project p \"P\" {}") {
  const bytes = new TextEncoder().encode(source);
  const ref = {
    blob_id: "compile/source/document.ldl",
    digest: sha256(bytes),
    lifetime: "request",
    media_type: "text/plain; charset=utf-8",
    size: String(bytes.byteLength),
  };
  return {
    request: {
      input: {
        entry_path: "document.ldl",
        installed_pack_tree: [],
        mode: "project",
        project_source_tree: [{ blob: ref, path: "document.ldl" }],
        referenced_assets: [],
        resolved_dependencies: {
          format: "layerdraw-resolved",
          format_version: 1,
          installs: [],
          language: 1,
        },
        resource_limits: {},
      },
      blobs: [{ ref, bytes }],
    },
    bytes,
    ref,
  };
}

export async function rejectedResponse(requestId) {
  const response = await fixture("schemas/fixtures/engine/compile-rejected.json");
  response.request_id = requestId;
  return response;
}

export async function successResponse(requestId) {
  const response = await fixture("schemas/fixtures/engine/compile-success.json");
  response.request_id = requestId;
  const values = [
    new TextEncoder().encode("query-output"),
    new TextEncoder().encode("artifact-output"),
    new TextEncoder().encode("canonical-output"),
  ];
  const refs = collectors.collectCompileResultBlobRefs(response.payload);
  refs.forEach((ref, index) => {
    ref.size = String(values[index].byteLength);
    ref.digest = sha256(values[index]);
  });
  const blobs = refs
    .map((ref, index) => ({
      blobId: ref.blob_id,
      bytes: values[index].slice().buffer,
    }))
    .sort((left, right) => (left.blobId < right.blobId ? -1 : 1));
  return { response, blobs, values };
}

function encode(text) {
  return new TextEncoder().encode(text).buffer;
}

function decode(buffer) {
  return new TextDecoder("utf-8", { fatal: true }).decode(buffer);
}

function compareUtf8(left, right) {
  return Buffer.compare(Buffer.from(left, "utf8"), Buffer.from(right, "utf8"));
}

function promiseBox() {
  let resolve;
  let reject;
  let settled = false;
  const promise = new Promise((res, rej) => {
    resolve = (value) => {
      if (settled) return;
      settled = true;
      res(value);
    };
    reject = (error) => {
      if (settled) return;
      settled = true;
      rej(error);
    };
  });
  return {
    promise,
    resolve,
    reject,
    get settled() {
      return settled;
    },
  };
}

export class StrictFakeTransport {
  constructor(factory, generation, endpointIndex) {
    this.factory = factory;
    this.generation = generation;
    this.endpointIndex = endpointIndex;
    this.requests = [];
    this.closedBox = promiseBox();
    this.closed = this.closedBox.promise;
    this.ready = Promise.resolve(factory.readyValue ?? limits);
    this.stopped = false;
  }

  request(input) {
    if (this.stopped) throw new Error("sensitive stopped transport /private/path");
    if (
      Object.getPrototypeOf(input) !== Object.prototype ||
      !(input.control instanceof ArrayBuffer) ||
      !Array.isArray(input.blobs) ||
      typeof input.exchangeId !== "string"
    ) {
      throw new Error("strict fake received an invalid request boundary");
    }
    for (let index = 0; index < input.blobs.length; index++) {
      const blob = input.blobs[index];
      if (!(blob.bytes instanceof ArrayBuffer)) {
        throw new Error("strict fake requires owned ArrayBuffers");
      }
      if (
        index > 0 &&
        compareUtf8(input.blobs[index - 1].blobId, blob.blobId) >= 0
      ) {
        throw new Error("strict fake requires sorted unique blobs");
      }
    }
    const control = JSON.parse(decode(input.control));
    const operation = control.operation;
    const controlText = decode(input.control);
    const decoded = decodeRequestEnvelope(operation, controlText);
    const responseBox = promiseBox();
    const record = { input, decoded, operation, responseBox, cancelCount: 0 };
    this.requests.push(record);

    queueMicrotask(async () => {
      try {
        if (operation === "engine.handshake") {
          const response = await this.factory.handshake(
            decoded,
            this.endpointIndex,
            this,
          );
          responseBox.resolve({
            control: encode(encodeHandshakeResponseEnvelope(response)),
            blobs: [],
          });
          return;
        }
        if (operation !== "engine.compile") {
          const result = await this.factory.workbench(
            decoded,
            input.blobs,
            this.endpointIndex,
            this,
            record,
          );
          if (result === StrictFakeTransport.PENDING) return;
          responseBox.resolve({
            control: encode(encodeWorkbenchResponse(operation, result.response ?? result)),
            blobs: result.blobs ?? [],
          });
          return;
        }
        const result = await this.factory.compile(
          decoded,
          input.blobs,
          this.endpointIndex,
          this,
          record,
        );
        if (result === StrictFakeTransport.PENDING) return;
        responseBox.resolve({
          control: encode(encodeCompileResponseEnvelope(result.response ?? result)),
          blobs: result.blobs ?? [],
        });
      } catch (error) {
        responseBox.reject(error);
      }
    });
    return {
      response: responseBox.promise,
      cancel: async () => {
        record.cancelCount++;
        const result = await this.factory.cancel(record, this);
        responseBox.reject(new Error("strict fake exchange cancelled"));
        return result;
      },
    };
  }

  terminate() {
    this.stopped = true;
    for (const request of this.requests) {
      request.responseBox.reject(new Error("strict fake endpoint terminated"));
    }
    this.closedBox.resolve({
      code: "WORKER_CRASHED",
      retryable: true,
    });
  }

  async dispose() {
    this.stopped = true;
    for (const request of this.requests) {
      request.responseBox.reject(new Error("strict fake endpoint disposed"));
    }
    this.closedBox.resolve({ code: "WORKER_CRASHED", retryable: true });
  }

  crash(details) {
    this.stopped = true;
    for (const request of this.requests) {
      request.responseBox.reject(new Error("strict fake endpoint crashed"));
    }
    this.closedBox.resolve({
      code: "WORKER_CRASHED",
      retryable: true,
      details,
    });
  }
}

StrictFakeTransport.PENDING = Symbol("pending");

function decodeRequestEnvelope(operation, controlText) {
  switch (operation) {
    case "engine.handshake":
      return decodeHandshakeRequestEnvelope(controlText);
    case "engine.compile":
      return decodeCompileRequestEnvelope(controlText);
    case "engine.apply_to_handle":
      return decodeApplyToHandleRequestEnvelope(controlText);
    case "engine.close_document":
      return decodeCloseDocumentRequestEnvelope(controlText);
    case "engine.execute_query":
      return decodeExecuteQueryRequestEnvelope(controlText);
    case "engine.find_symbols":
      return decodeFindSymbolsRequestEnvelope(controlText);
    case "engine.find_usages":
      return decodeFindUsagesRequestEnvelope(controlText);
    case "engine.format_scope":
      return decodeFormatScopeRequestEnvelope(controlText);
    case "engine.get_neighbors":
      return decodeGetNeighborsRequestEnvelope(controlText);
    case "engine.inspect_subgraph":
      return decodeInspectSubgraphRequestEnvelope(controlText);
    case "engine.open_document":
      return decodeOpenDocumentRequestEnvelope(controlText);
    case "engine.list_modules":
      return decodeListModulesRequestEnvelope(controlText);
    case "engine.list_references":
      return decodeListReferencesRequestEnvelope(controlText);
    case "engine.organize_workspace":
      return decodeOrganizeWorkspaceRequestEnvelope(controlText);
    case "engine.preview_fragment":
      return decodePreviewFragmentRequestEnvelope(controlText);
    case "engine.preview_source_patch":
      return decodePreviewSourcePatchRequestEnvelope(controlText);
    case "engine.read_declarations":
      return decodeReadDeclarationsRequestEnvelope(controlText);
    case "engine.read_modules":
      return decodeReadModulesRequestEnvelope(controlText);
    case "engine.read_references":
      return decodeReadReferencesRequestEnvelope(controlText);
    case "engine.read_rows":
      return decodeReadRowsRequestEnvelope(controlText);
    case "engine.read_scope":
      return decodeReadScopeRequestEnvelope(controlText);
    case "engine.replace_source_tree":
      return decodeReplaceSourceTreeRequestEnvelope(controlText);
    default:
      throw new Error(`unsupported fake operation ${operation}`);
  }
}

function encodeWorkbenchResponse(operation, response) {
  switch (operation) {
    case "engine.apply_to_handle":
      return encodeApplyToHandleResponseEnvelope(response);
    case "engine.close_document":
      return encodeCloseDocumentResponseEnvelope(response);
    case "engine.execute_query":
      return encodeExecuteQueryResponseEnvelope(response);
    case "engine.find_symbols":
      return encodeFindSymbolsResponseEnvelope(response);
    case "engine.find_usages":
      return encodeFindUsagesResponseEnvelope(response);
    case "engine.format_scope":
      return encodeFormatScopeResponseEnvelope(response);
    case "engine.get_neighbors":
      return encodeGetNeighborsResponseEnvelope(response);
    case "engine.inspect_subgraph":
      return encodeInspectSubgraphResponseEnvelope(response);
    case "engine.open_document":
      return encodeOpenDocumentResponseEnvelope(response);
    case "engine.list_modules":
      return encodeListModulesResponseEnvelope(response);
    case "engine.list_references":
      return encodeListReferencesResponseEnvelope(response);
    case "engine.organize_workspace":
      return encodeOrganizeWorkspaceResponseEnvelope(response);
    case "engine.preview_fragment":
      return encodePreviewFragmentResponseEnvelope(response);
    case "engine.preview_source_patch":
      return encodePreviewSourcePatchResponseEnvelope(response);
    case "engine.read_declarations":
      return encodeReadDeclarationsResponseEnvelope(response);
    case "engine.read_modules":
      return encodeReadModulesResponseEnvelope(response);
    case "engine.read_references":
      return encodeReadReferencesResponseEnvelope(response);
    case "engine.read_rows":
      return encodeReadRowsResponseEnvelope(response);
    case "engine.read_scope":
      return encodeReadScopeResponseEnvelope(response);
    case "engine.replace_source_tree":
      return encodeReplaceSourceTreeResponseEnvelope(response);
    default:
      throw new Error(`unsupported fake workbench response ${operation}`);
  }
}

export async function makeFactory(overrides = {}) {
  const handshakeFixture = await fixture(
    "schemas/fixtures/engine/handshake-success.json",
  );
  const factory = {
    transportId: "fake",
    connectFailureCode: "CONNECT_FAILED",
    endpoints: [],
    readyValue: overrides.readyValue,
    create(generation) {
      if (overrides.create) return overrides.create(generation, this);
      const endpoint = new StrictFakeTransport(this, generation, this.endpoints.length);
      this.endpoints.push(endpoint);
      return endpoint;
    },
    async handshake(request, endpointIndex, transport) {
      if (overrides.handshake) {
        return overrides.handshake(request, endpointIndex, transport);
      }
      const response = structuredClone(handshakeFixture);
      response.request_id = request.request_id;
      response.payload.endpoint_instance_id = `fake-endpoint-${endpointIndex + 1}`;
      response.payload.capability_manifest.transports = ["fake"];
      const required = new Set(request.payload.required_capabilities);
      response.payload.capability_statuses = [
        ...request.payload.required_capabilities.map((capability_id) => ({
          capability_id,
          enabled: true,
          protocol_version: "1.0",
        })),
        ...request.payload.optional_capabilities.map((capability_id) => ({
          capability_id,
          enabled: false,
          protocol_version: "1.0",
          unavailable_reason: "unsupported",
        })),
      ].filter(
        (status, index, all) =>
          all.findIndex((other) => other.capability_id === status.capability_id) ===
          index,
      );
      if (!required.has("engine.compile")) {
        throw new Error("client omitted engine.compile");
      }
      return response;
    },
    async compile(request) {
      if (overrides.compile) return overrides.compile(...arguments);
      return { response: await rejectedResponse(request.request_id), blobs: [] };
    },
    async workbench(request) {
      if (overrides.workbench) return overrides.workbench(...arguments);
      return {
        response: {
          diagnostics: [],
          engine_release: "0.0.0-dev",
          failure: {
            category: "io",
            code: "workbench.fake.unimplemented",
            message: "Fake workbench response is not implemented.",
            retryable: false,
            workbench_category: "execution_failed",
          },
          outcome: "failed",
          protocol: { name: "engine", version: "1.0" },
          request_id: request.request_id,
        },
        blobs: [],
      };
    },
    async cancel(record, transport) {
      if (overrides.cancel) return overrides.cancel(record, transport);
      return { reusable: true };
    },
  };
  return factory;
}

export const creationOptions = Object.freeze({
  expectedReleaseManifestDigest: expectedReleaseDigest,
  handshakeTimeoutMs: 1_000,
  defaultCompileTimeoutMs: 1_000,
  cancelGraceMs: 10,
  disposeTimeoutMs: 50,
});

export function waitFor(predicate, timeoutMs = 1_000) {
  return new Promise((resolve, reject) => {
    const deadline = Date.now() + timeoutMs;
    const check = () => {
      if (predicate()) return resolve();
      if (Date.now() >= deadline) return reject(new Error("waitFor timed out"));
      setImmediate(check);
    };
    check();
  });
}
