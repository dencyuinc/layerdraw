// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import process from "node:process";
import { createHostRegistryClient } from "../../../packages/registry-client/dist/host.js";

let body = "";
for await (const chunk of process.stdin) body += chunk;
const goResponse = JSON.parse(body);
let emitted;
const client = createHostRegistryClient({
  async invoke(request) {
    emitted = request;
    return goResponse;
  },
});
const result = await client.listSources();
if (!result.ok || result.value.length !== 1 || result.value[0].source_id !== "official") {
  throw new Error("TypeScript Registry client rejected the Go wire response");
}
process.stdout.write(JSON.stringify(emitted));
