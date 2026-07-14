// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {Worker} from "node:worker_threads";

export class NodeWorkerAdapter {
  #worker;
  #listeners = new Map();
  #exit;

  constructor(moduleURL, options) {
    this.#worker = new Worker(new URL(moduleURL), {name: options.name});
    this.#exit = new Promise((resolve) => this.#worker.once("exit", resolve));
  }

  postMessage(message, transfer) {
    this.#worker.postMessage(message, [...transfer]);
  }

  addEventListener(type, listener) {
    const wrapper = type === "message" ? (data) => listener({data}) : () => listener();
    let byType = this.#listeners.get(type);
    if (byType === undefined) {
      byType = new Map();
      this.#listeners.set(type, byType);
    }
    byType.set(listener, wrapper);
    this.#worker.on(type, wrapper);
  }

  removeEventListener(type, listener) {
    const byType = this.#listeners.get(type);
    const wrapper = byType?.get(listener);
    if (wrapper === undefined) return;
    this.#worker.off(type, wrapper);
    byType.delete(listener);
  }

  terminate() {
    return this.#worker.terminate();
  }

  crashForTest() {
    this.#worker.postMessage({kind: "__layerdraw_test_crash"});
  }

  nextMessage(predicate) {
    return new Promise((resolve, reject) => {
      const onMessage = (message) => {
        if (!predicate(message)) return;
        cleanup();
        resolve(message);
      };
      const onError = () => {
        cleanup();
        reject(new Error("worker crashed before matching message"));
      };
      const cleanup = () => {
        this.#worker.off("message", onMessage);
        this.#worker.off("error", onError);
      };
      this.#worker.on("message", onMessage);
      this.#worker.on("error", onError);
    });
  }

  get exited() {
    return this.#exit;
  }
}
