// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {browserTransportLimits} from "../dist/protocol.js";
import {installEngineWorker} from "../dist/worker-runtime.js";

export const artifactDigest = `sha256:${"a".repeat(64)}`;
export const releaseDigest = `sha256:${"b".repeat(64)}`;
export const generation = "test-generation-1";

export const transportLimits = browserTransportLimits;

export function bytes(...values) {
  return Uint8Array.from(values).buffer;
}

export function makeEchoEndpoint(overrides = {}) {
  let disposed = false;
  let requests = 0;
  return {
    artifactManifestDigest: artifactDigest,
    transportLimits,
    request(input) {
      requests += 1;
      if (new Uint8Array(input.control)[0] === 255) throw new Error("private adapter failure");
      return {ok: true, response: {
        control: input.control.slice(0),
        blobs: input.blobs.map((blob) => ({blob_id: blob.blob_id, bytes: blob.bytes.slice(0)})),
      }};
    },
    dispose() {
      disposed = true;
    },
    get disposed() {
      return disposed;
    },
    get requests() {
      return requests;
    },
    ...overrides,
  };
}

function enqueue(callback) {
  setImmediate(callback);
}

export class LinkedWorker {
  #hostListeners = new Map([
    ["message", new Set()],
    ["messageerror", new Set()],
    ["error", new Set()],
  ]);
  #workerListeners = new Map([
    ["message", new Set()],
    ["messageerror", new Set()],
  ]);
  #closed = false;
  #terminated = false;

  constructor(createEndpoint, options = {}) {
    const scope = {
      postMessage: (message, transfer) => {
        if (this.#closed || this.#terminated) throw new Error("closed");
        const cloned = structuredClone(message, {transfer: [...transfer]});
        enqueue(() => this.#emitHost("message", {data: cloned}));
      },
      addEventListener: (type, listener) => this.#workerListeners.get(type).add(listener),
      removeEventListener: (type, listener) => this.#workerListeners.get(type).delete(listener),
      close: () => {
        this.#closed = true;
      },
    };
    installEngineWorker(scope, createEndpoint, {checkEnvironment: options.checkEnvironment ?? (() => true)});
  }

  postMessage(message, transfer) {
    if (this.#terminated) throw new Error("terminated");
    const cloned = structuredClone(message, {transfer: [...transfer]});
    enqueue(() => {
      if (this.#terminated) return;
      for (const listener of this.#workerListeners.get("message")) listener({data: cloned});
    });
  }

  addEventListener(type, listener) {
    this.#hostListeners.get(type).add(listener);
  }

  removeEventListener(type, listener) {
    this.#hostListeners.get(type).delete(listener);
  }

  terminate() {
    this.#terminated = true;
  }

  emitMessage(value) {
    this.#emitHost("message", {data: value});
  }

  emitError(type = "error") {
    this.#emitHost(type);
  }

  get terminated() {
    return this.#terminated;
  }

  #emitHost(type, event) {
    if (this.#terminated) return;
    for (const listener of this.#hostListeners.get(type)) listener(event);
  }
}

export function linkedWorkerHarness(createEndpoint = () => makeEchoEndpoint(), options) {
  let worker;
  return {
    factory() {
      worker = new LinkedWorker(createEndpoint, options);
      return worker;
    },
    get worker() {
      return worker;
    },
  };
}

export function nextTask() {
  return new Promise((resolve) => setImmediate(resolve));
}
