// SPDX-License-Identifier: Apache-2.0

import type {
  CompileOutcome,
  CompileRequestBlob,
  EngineClient,
  EngineClientErrorKind,
  EngineWorkbenchClient,
  PortableCompileRequest,
  RequestBlobRef,
  WorkbenchOutcome,
} from "../src/index.js";
import {
  createWailsDesktopClient,
  createWailsEngineClient,
  WailsBindingError,
  type CreateWailsDesktopClientOptions,
  type CreateWailsEngineClientOptions,
  type WailsDesktopClient,
  type WailsGeneratedBindings,
} from "../src/wails.js";

declare const wailsBindings: WailsGeneratedBindings;
declare const wailsEngineOptions: CreateWailsEngineClientOptions;
declare const wailsDesktopOptions: CreateWailsDesktopClientOptions;
void wailsBindings.EngineCompile;
void createWailsEngineClient(wailsEngineOptions);
const desktop: Promise<WailsDesktopClient> = createWailsDesktopClient(wailsDesktopOptions);
void desktop;
void new WailsBindingError("BINDING_VERSION_MISMATCH", "upgrade_desktop");
import type {
  OpenDocumentInput,
  OpenDocumentResponseEnvelope,
} from "@layerdraw/protocol/engine";
import type { LocalHostClient } from "../src/host.js";

declare const client: EngineClient;
declare const ref: RequestBlobRef;
declare const bytes: Uint8Array;
declare const buffer: ArrayBuffer;

const copy: CompileRequestBlob = { ref, bytes };
const transfer: CompileRequestBlob = {
  ref,
  bytes: buffer,
  ownership: "transfer",
};
void copy;
void transfer;

// @ts-expect-error transfer ownership requires an ArrayBuffer
const invalidTransfer: CompileRequestBlob = {
  ref,
  bytes,
  ownership: "transfer",
};
void invalidTransfer;

// @ts-expect-error ArrayBuffer input must explicitly transfer ownership
const invalidCopy: CompileRequestBlob = { ref, bytes: buffer };
void invalidCopy;

declare const request: PortableCompileRequest;
const outcomePromise: Promise<CompileOutcome> = client.compile(request);
void outcomePromise;
declare const openInput: OpenDocumentInput;
const workbench: EngineWorkbenchClient = client.workbench;
const openPromise: Promise<WorkbenchOutcome<OpenDocumentResponseEnvelope>> =
  workbench.openDocument(openInput);
void openPromise;

type PublicKeys = keyof EngineClient;
const allowed: PublicKeys[] = [
  "state",
  "workbench",
  "getEndpoint",
  "getCapabilities",
  "hasCapability",
  "compile",
  "restart",
  "dispose",
];
void allowed;

// @ts-expect-error raw request is deliberately absent
client.request("engine.compile", {});
// @ts-expect-error handshake is owned by creation
client.handshake();
// @ts-expect-error transports never escape from the root contract
client.transport;

const kind: EngineClientErrorKind = "misuse";
void kind;

declare const host: LocalHostClient;
host.openDocument({ document_id: "document" });
host.inspectDocument({ session: {} as never });
host.previewOperations({} as never);
host.commitOperations({} as never);
host.saveDocument({} as never);
host.controlAutosave({} as never);
host.getStateSnapshot({ session: {} as never });
host.listRevisions({} as never);
host.previewRestore({} as never);
host.stageAsset({} as never, bytes);
host.closeDocument({ session: {} as never });
host.cancelOperation({} as never);
host.getOperationResult({} as never);
host.recoverOperations({ document_id: "document" });
host.engine.compile(request);
host.restart();
host.dispose();

function narrow(outcome: CompileOutcome): void {
  if (outcome.origin === "client") {
    outcome.reason;
    // @ts-expect-error client cancellation cannot fabricate Engine response
    outcome.response;
    return;
  }
  if (outcome.outcome === "success") {
    outcome.response.payload;
    outcome.blobs[0]?.bytes;
    // @ts-expect-error success never carries a failure
    outcome.response.failure.code;
    return;
  }
  if (outcome.outcome === "rejected") {
    outcome.response.diagnostics;
    // @ts-expect-error rejected never carries output blobs
    const output: [unknown] = outcome.blobs;
    void output;
  }
}
void narrow;
