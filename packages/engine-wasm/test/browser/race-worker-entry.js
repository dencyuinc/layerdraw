// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {createVerifiedWasmEndpoint} from "../../dist/artifact.js";
import {installEngineWorker} from "../../dist/worker-runtime.js";

const token = new URL(import.meta.url).searchParams.get("token");
if (token === null || !/^[a-z0-9-]+$/.test(token)) throw new Error("missing race token");

const originalRevoke = URL.revokeObjectURL.bind(URL);
let revoked = 0;
URL.revokeObjectURL = (url) => {
  revoked += 1;
  originalRevoke(url);
};

installEngineWorker(globalThis, async (init) => {
  const endpoint = await createVerifiedWasmEndpoint(init, {
    artifactBaseURL: new URL(`./race-artifact/${token}/`, import.meta.url).href,
    packageManifestURL: new URL("../../package.json", import.meta.url).href,
  });
  globalThis.postMessage({kind: "__layerdraw_snapshot_resource", revoked});
  return endpoint;
});
