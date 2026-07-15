// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {createReadStream} from "node:fs";
import {stat} from "node:fs/promises";
import {createServer} from "node:http";
import {extname, resolve, sep} from "node:path";
import {fileURLToPath} from "node:url";

const root = resolve(fileURLToPath(new URL("../", import.meta.url)));
const repositoryRoot = resolve(root, "../..");
const types = new Map([
  [".html", "text/html; charset=utf-8"],
  [".js", "text/javascript; charset=utf-8"],
  [".mjs", "text/javascript; charset=utf-8"],
  [".json", "application/json; charset=utf-8"],
  [".wasm", "application/wasm"],
]);
const raceWasmExecReads = new Map();

const server = createServer(async (request, response) => {
  try {
    const pathname = decodeURIComponent(new URL(request.url ?? "/", "http://127.0.0.1").pathname);
    if (pathname === "/__layerdraw/engine-compile-parity-v1.json") {
      const path = resolve(repositoryRoot, "tests/conformance/testdata/engine_compile_parity_v1.json");
      const info = await stat(path);
      response.writeHead(200, {"cache-control": "no-store", "content-length": info.size, "content-type": "application/json; charset=utf-8"});
      createReadStream(path).pipe(response);
      return;
    }
    const race = /^\/test\/browser\/race-artifact\/([a-z0-9-]+)\/(.+)$/.exec(pathname);
    if (race !== null) {
      const [, token, relative] = race;
      if (token === undefined || relative === undefined || relative.includes("..")) throw new Error("invalid race artifact path");
      if (relative === "wasm_exec.js") {
        const reads = (raceWasmExecReads.get(token) ?? 0) + 1;
        raceWasmExecReads.set(token, reads);
        if (reads > 1) {
          const body = "globalThis.__layerdrawUnverifiedWasmExecRan = true;\n";
          response.writeHead(200, {"cache-control": "no-store", "content-length": Buffer.byteLength(body), "content-type": "text/javascript; charset=utf-8"});
          response.end(body);
          return;
        }
      }
      const path = resolve(root, "dist", relative);
      if (!path.startsWith(`${resolve(root, "dist")}${sep}`)) throw new Error("outside artifact root");
      const info = await stat(path);
      response.writeHead(200, {"cache-control": "no-store", "content-length": info.size, "content-type": types.get(extname(path)) ?? "application/octet-stream"});
      createReadStream(path).pipe(response);
      return;
    }
    const status = /^\/test\/browser\/race-status\/([a-z0-9-]+)$/.exec(pathname);
    if (status !== null) {
      const body = JSON.stringify({wasmExecReads: raceWasmExecReads.get(status[1]) ?? 0});
      response.writeHead(200, {"cache-control": "no-store", "content-length": Buffer.byteLength(body), "content-type": "application/json"});
      response.end(body);
      return;
    }
    const requested = pathname === "/" ? "/test/browser/harness.html" : pathname;
    const path = resolve(root, `.${requested}`);
    if (path !== root && !path.startsWith(`${root}${sep}`)) throw new Error("outside root");
    const info = await stat(path);
    if (!info.isFile()) throw new Error("not a file");
    response.writeHead(200, {
      "cache-control": "no-store",
      "content-length": info.size,
      "content-type": types.get(extname(path)) ?? "application/octet-stream",
      "cross-origin-resource-policy": "same-origin",
    });
    createReadStream(path).pipe(response);
  } catch {
    response.writeHead(404, {"content-type": "text/plain; charset=utf-8"});
    response.end("not found");
  }
});

server.listen(4173, "127.0.0.1");

for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => server.close(() => process.exit(0)));
}
