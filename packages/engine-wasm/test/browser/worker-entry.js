// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {createVerifiedWasmEndpoint} from "../../dist/artifact.js";
import {installEngineWorker} from "../../dist/worker-runtime.js";

globalThis.addEventListener("message", (event) => {
  if (event.data?.kind === "__layerdraw_test_crash") throw new Error("test-only process crash after real artifact initialization");
});

installEngineWorker(globalThis, (init) => createVerifiedWasmEndpoint(init, {
  artifactBaseURL: new URL("../../dist/", import.meta.url).href,
  packageManifestURL: new URL("../../package.json", import.meta.url).href,
}));
