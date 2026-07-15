// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {createVerifiedWasmEndpoint} from "./artifact.js";
import {installEngineWorker, type DedicatedWorkerScopeLike} from "./worker-runtime.js";

installEngineWorker(globalThis as unknown as DedicatedWorkerScopeLike, (init) => createVerifiedWasmEndpoint(init, {
  artifactBaseURL: new URL("./", import.meta.url).href,
  packageManifestURL: new URL("../package.json", import.meta.url).href,
}));
