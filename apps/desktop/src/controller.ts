// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { ViewerState } from "@layerdraw/viewer";
import type {
  DesktopLifecycleSnapshot,
  DesktopProjectContext,
  DesktopShellFailure,
  DesktopShellPorts,
  DesktopViewerFrame,
} from "./contracts.js";

export interface DesktopShellState {
  readonly lifecycle: DesktopLifecycleSnapshot;
  readonly viewer: ViewerState;
  readonly pending_action: "select_view" | "review_recovery" | undefined;
  readonly failure: DesktopShellFailure | undefined;
}

const lifecycleFailure: DesktopShellFailure = Object.freeze({
  code: "desktop.lifecycle_failed",
  message_key: "desktop.error.lifecycle_failed",
  recoverable: true,
});
const selectionFailure: DesktopShellFailure = Object.freeze({
  code: "desktop.selection_failed",
  message_key: "desktop.error.selection_failed",
  recoverable: true,
});
const viewerFailure: DesktopShellFailure = Object.freeze({
  code: "desktop.viewer_failed",
  message_key: "desktop.error.viewer_failed",
  recoverable: true,
});
const viewerRejected: DesktopShellFailure = Object.freeze({
  code: "desktop.viewer_rejected",
  message_key: "desktop.error.viewer_rejected",
  recoverable: true,
});
const contextMismatch: DesktopShellFailure = Object.freeze({
  code: "desktop.context_mismatch",
  message_key: "desktop.error.context_mismatch",
  recoverable: true,
});

function matchesProject(frame: DesktopViewerFrame, project: DesktopProjectContext | undefined): boolean {
  return project !== undefined
    && frame.project_id === project.project_id
    && frame.session_generation === project.session_generation
    && frame.view_address === project.selected_view_address
    && frame.authoritative_revision_token === project.authoritative_revision_token;
}

export class DesktopShellController {
  readonly #ports: DesktopShellPorts;
  readonly #listeners = new Set<() => void>();
  #state: DesktopShellState;
  #unsubscribe: (() => void) | undefined;
  #lifecycleGeneration = 0;
  #actionGeneration = 0;
  #lastLifecycleSequence = -1;
  #lastFrameSequence = -1;
  #lastSessionKey: string | undefined;
  #actionAbort: AbortController | undefined;
  #closed = false;

  constructor(ports: DesktopShellPorts) {
    this.#ports = ports;
    this.#state = Object.freeze({ lifecycle: ports.lifecycle.getSnapshot(), viewer: ports.viewer.getState(), pending_action: undefined, failure: undefined });
  }

  getSnapshot = (): DesktopShellState => this.#state;
  subscribe = (listener: () => void): (() => void) => {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  };

  start(): void {
    if (this.#closed || this.#unsubscribe !== undefined) return;
    this.#unsubscribe = this.#ports.lifecycle.subscribe(() => { void this.#acceptLifecycle(); });
    void this.#acceptLifecycle();
  }

  async selectView(viewAddress: string): Promise<void> {
    if (this.#closed) return;
    const project = this.#state.lifecycle.project;
    if (project === undefined || !project.views.some((view) => view.address === viewAddress)) {
      this.#publish({ ...this.#state, failure: selectionFailure });
      return;
    }
    await this.#runAction("select_view", (signal) => this.#ports.lifecycle.selectView(viewAddress, signal), selectionFailure);
  }

  async reviewRecovery(): Promise<void> {
    if (this.#closed) return;
    await this.#runAction("review_recovery", (signal) => this.#ports.lifecycle.showRecoveryOptions(signal), lifecycleFailure);
  }

  setViewerSelection(keys: readonly string[]): void {
    if (this.#closed) return;
    try {
      this.#ports.viewer.setSelection(keys);
      this.#publish({ ...this.#state, viewer: this.#ports.viewer.getState(), failure: undefined });
    } catch {
      this.#publish({ ...this.#state, failure: viewerFailure });
    }
  }

  async close(): Promise<void> {
    if (this.#closed) return;
    this.#closed = true;
    await this.stop();
    this.#listeners.clear();
  }

  async stop(): Promise<void> {
    ++this.#lifecycleGeneration;
    ++this.#actionGeneration;
    this.#actionAbort?.abort();
    this.#actionAbort = undefined;
    this.#unsubscribe?.();
    this.#unsubscribe = undefined;
    await this.#ports.viewer.cancel();
    this.#lastLifecycleSequence = -1;
    this.#lastFrameSequence = -1;
    this.#lastSessionKey = undefined;
  }

  async #acceptLifecycle(): Promise<void> {
    if (this.#closed) return;
    const lifecycle = this.#ports.lifecycle.getSnapshot();
    if (lifecycle.sequence <= this.#lastLifecycleSequence) return;
    this.#lastLifecycleSequence = lifecycle.sequence;
    const generation = ++this.#lifecycleGeneration;
    const frame = lifecycle.viewer_frame;
    const sessionKey = lifecycle.project === undefined ? undefined : `${lifecycle.project.project_id}\u0000${lifecycle.project.session_generation}`;
    if (sessionKey !== this.#lastSessionKey) {
      this.#lastSessionKey = sessionKey;
      this.#lastFrameSequence = -1;
    }
    this.#publish({ lifecycle, viewer: this.#ports.viewer.getState(), pending_action: this.#state.pending_action, failure: lifecycle.failure });
    if (frame === undefined || frame.input.sequence <= this.#lastFrameSequence) return;
    if (!matchesProject(frame, lifecycle.project)) {
      this.#publish({ ...this.#state, failure: contextMismatch });
      return;
    }
    try {
      const result = frame.kind === "snapshot"
        ? await this.#ports.viewer.setViewData(frame.input)
        : await this.#ports.viewer.applyViewDataUpdate(frame.input);
      if (this.#closed || generation !== this.#lifecycleGeneration) return;
      this.#lastFrameSequence = frame.input.sequence;
      this.#publish({
        ...this.#state,
        viewer: result.state,
        failure: result.ok ? this.#state.lifecycle.failure : viewerRejected,
      });
    } catch {
      if (!this.#closed && generation === this.#lifecycleGeneration) this.#publish({ ...this.#state, failure: viewerFailure });
    }
  }

  async #runAction(
    action: "select_view" | "review_recovery",
    invoke: (signal: AbortSignal) => Promise<void>,
    failure: DesktopShellFailure,
  ): Promise<void> {
    this.#actionAbort?.abort();
    const abort = new AbortController();
    this.#actionAbort = abort;
    const generation = ++this.#actionGeneration;
    this.#publish({ ...this.#state, pending_action: action, failure: undefined });
    try {
      await invoke(abort.signal);
    } catch {
      if (!abort.signal.aborted && !this.#closed && generation === this.#actionGeneration) {
        this.#publish({ ...this.#state, pending_action: undefined, failure });
      }
      return;
    }
    if (!this.#closed && generation === this.#actionGeneration) this.#publish({ ...this.#state, pending_action: undefined });
  }

  #publish(state: DesktopShellState): void {
    this.#state = Object.freeze(state);
    for (const listener of [...this.#listeners]) listener();
  }
}
