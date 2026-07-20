// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import {execFile} from "node:child_process";
import {createHash} from "node:crypto";
import {cp, mkdir, mkdtemp, readFile, writeFile} from "node:fs/promises";
import {tmpdir} from "node:os";
import {dirname, join} from "node:path";
import {promisify} from "node:util";
import test from "node:test";

const execute = promisify(execFile);
const packageRoot = new URL("../", import.meta.url);
const repositoryRoot = new URL("../../../", import.meta.url);

test("packed package is closed, legal-complete, offline-installable, and SSR-safe", async () => {
  const temporary = await mkdtemp(join(tmpdir(), "layerdraw-engine-wasm-pack-"));
  const {stdout} = await execute("corepack", ["pnpm", "pack", "--pack-destination", temporary, "--json"], {
    cwd: packageRoot,
    maxBuffer: 10 * 1024 * 1024,
  });
  const packed = JSON.parse(stdout);
  const archive = join(temporary, packed.filename.split("/").at(-1));
  const {stdout: listingText} = await execute("tar", ["-tzf", archive]);
  const listing = listingText.trim().split("\n").sort();
  for (const required of [
    "package/LICENSE",
    "package/NOTICE",
    "package/README.md",
    "package/THIRD_PARTY_NOTICES.txt",
    "package/dist/index.js",
    "package/dist/index.d.ts",
    "package/dist/worker.js",
    "package/dist/worker.d.ts",
    "package/dist/layerdraw-engine.wasm",
    "package/dist/wasm_exec.js",
    "package/dist/engine-wasm-worker-v1.json",
    "package/dist/engine-wasm.manifest.json",
    "package/dist/engine-wasm.authority.json",
    "package/dist/engine-wasm.cdx.json",
    "package/dist/LICENSING.md",
    "package/dist/licenses/Apache-2.0.txt",
    "package/package.json",
  ]) assert.ok(listing.includes(required), `missing ${required}`);
  assert.equal(listing.some((path) => /\/(src|test|tools)\//.test(path) || path.endsWith(".tsbuildinfo")), false);

  const extracted = join(temporary, "extracted");
  await execute("mkdir", [extracted]);
  await execute("tar", ["-xzf", archive, "-C", extracted]);
  assert.deepEqual(await readFile(join(extracted, "package", "LICENSE")), await readFile(new URL("LICENSE", repositoryRoot)));
  assert.deepEqual(await readFile(join(extracted, "package", "NOTICE")), await readFile(new URL("NOTICE", repositoryRoot)));
  assert.deepEqual(
    await readFile(join(extracted, "package", "THIRD_PARTY_NOTICES.txt")),
    await readFile(join(extracted, "package", "dist", "THIRD_PARTY_NOTICES.txt")),
  );
  const manifest = JSON.parse(await readFile(join(extracted, "package", "package.json"), "utf8"));
  assert.equal(manifest.license, "SEE LICENSE IN LICENSE");
  assert.equal(manifest.dependencies, undefined);
  assert.equal(manifest.scripts?.postinstall, undefined);
  assert.deepEqual(manifest.sideEffects, ["./dist/worker.js"]);

  const artifactRoot = join(extracted, "package", "dist");
  assert.deepEqual(
    await readFile(join(artifactRoot, "engine-wasm-worker-v1.json")),
    await readFile(new URL("tests/conformance/testdata/engine_wasm_worker_v1.json", repositoryRoot)),
  );
  const artifactManifestBytes = await readFile(join(artifactRoot, "engine-wasm.manifest.json"));
  const artifactManifest = JSON.parse(artifactManifestBytes);
  const sbom = JSON.parse(await readFile(join(artifactRoot, "engine-wasm.cdx.json"), "utf8"));
  assert.equal(artifactManifest.build.release_version, manifest.version);
  assert.equal(sbom.metadata.component.version, manifest.version);
  assert.equal(sbom.metadata.component.name, manifest.name);
  assert.deepEqual(artifactManifest.files.map((file) => file.path).sort(), [
    "LICENSE", "LICENSING.md", "NOTICE", "THIRD_PARTY_NOTICES.txt",
    "engine-wasm-worker-v1.json", "engine-wasm.authority.json", "engine-wasm.cdx.json", "layerdraw-engine.wasm",
    "licenses/Apache-2.0.txt", "wasm_exec.js",
  ]);
  for (const file of artifactManifest.files) {
    const bytes = await readFile(join(artifactRoot, file.path));
    assert.equal(bytes.byteLength, file.size, `${file.path} size`);
    assert.equal(`sha256:${createHash("sha256").update(bytes).digest("hex")}`, file.digest, `${file.path} digest`);
  }
  const {stdout: goroot} = await execute("go", ["env", "GOROOT"]);
  assert.deepEqual(await readFile(join(artifactRoot, "wasm_exec.js")), await readFile(join(goroot.trim(), "lib", "wasm", "wasm_exec.js")));
  await execute("node", [
    new URL("tests/integration/testdata/wasm_bridge_node.mjs", repositoryRoot).pathname,
    manifest.version,
  ], {cwd: artifactRoot, maxBuffer: 10 * 1024 * 1024});
  await execute("node", ["--input-type=module", "-e", `
    import {createHash} from "node:crypto";
    import {readFile} from "node:fs/promises";
    import {pathToFileURL} from "node:url";
    const artifactRoot = process.env.ARTIFACT_ROOT;
    const packageRoot = process.env.PACKAGE_ROOT;
    const {createVerifiedWasmEndpoint} = await import(pathToFileURL(artifactRoot + "/artifact.js"));
    const manifestBytes = await readFile(artifactRoot + "/engine-wasm.manifest.json");
    const manifest = JSON.parse(manifestBytes);
    const packageManifest = JSON.parse(await readFile(packageRoot + "/package.json", "utf8"));
    const arrayBuffer = (bytes) => bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength);
    const endpoint = await createVerifiedWasmEndpoint({
      endpointGeneration: "packed-release-authority",
      expectedArtifactManifestDigest: "sha256:" + createHash("sha256").update(manifestBytes).digest("hex"),
      releaseManifestDigest: "sha256:" + "5".repeat(64),
    }, {
      artifactBaseURL: pathToFileURL(artifactRoot + "/").href,
      packageManifestURL: pathToFileURL(packageRoot + "/package.json").href,
      async loadBytes(url) { return arrayBuffer(await readFile(new URL(url))); },
    });
    const control = new TextEncoder().encode(JSON.stringify({
      operation: "engine.handshake",
      payload: {
        client_release: "0.0.0-dev",
        protocols: [{name: "engine", supported_range: "1.0..1.0", versions: [{version: "1.0", schema_digest: manifest.protocol.schema_digest}]}],
        required_capabilities: ["engine.compile"],
        optional_capabilities: [],
      },
      protocol: {name: "engine", version: "1.0"},
      request_id: "packed-release-authority-request",
    })).buffer;
    const result = endpoint.request({control, blobs: []});
    if (!result.ok) throw new Error(result.failure.code);
    const response = JSON.parse(new TextDecoder().decode(result.response.control));
    if (manifest.build.release_version !== packageManifest.version || response.engine_release !== packageManifest.version ||
        response.payload.host_release !== packageManifest.version) throw new Error("packed release authorities diverged");
    endpoint.dispose();
  `], {
    cwd: repositoryRoot,
    env: {...process.env, ARTIFACT_ROOT: artifactRoot, PACKAGE_ROOT: join(extracted, "package")},
    maxBuffer: 10 * 1024 * 1024,
  });

  const consumer = join(temporary, "consumer");
  await execute("mkdir", [consumer]);
  await writeFile(join(consumer, "package.json"), JSON.stringify({name: "engine-wasm-consumer", private: true, type: "module"}));
  await execute("corepack", ["pnpm", "add", "--offline", "--ignore-scripts", archive], {cwd: consumer, maxBuffer: 10 * 1024 * 1024});
  await execute("node", ["--input-type=module", "-e", `
    Object.defineProperty(globalThis, "Worker", {configurable: true, get() { throw new Error("SSR import touched Worker"); }});
    const root = await import("@layerdraw/engine-wasm");
    if (typeof root.createEngineWorkerTransport !== "function") process.exit(2);
  `], {cwd: consumer});
  await execute("node", ["--input-type=module", "-e", `
    for (const path of ["@layerdraw/engine-wasm/protocol", "@layerdraw/engine-wasm/dist/protocol.js", "@layerdraw/engine-wasm/src/index.ts"]) {
      try { await import(path); process.exit(2); }
      catch (error) { if (error?.code !== "ERR_PACKAGE_PATH_NOT_EXPORTED") throw error; }
    }
  `], {cwd: consumer});

  await assert.rejects(readFile(new URL("LICENSE", packageRoot)), (error) => error?.code === "ENOENT");
  await assert.rejects(readFile(new URL("NOTICE", packageRoot)), (error) => error?.code === "ENOENT");
});

test("package build rejects an explicit release version that differs from package.json", async () => {
  await assert.rejects(execute("node", ["tools/build-package.mjs", "--allow-dirty"], {
    cwd: packageRoot,
    env: {...process.env, VERSION: "9.9.9-mismatch"},
  }), /VERSION must exactly equal package\.json version/);
});

test("artifact build and reproducibility entrypoints reject unset and empty VERSION", async () => {
  for (const script of ["tools/build-engine-wasm.sh", "tools/check-engine-wasm-reproducible.sh"]) {
    for (const mode of ["unset", "empty"]) {
      const environment = {...process.env};
      if (mode === "unset") delete environment.VERSION;
      else environment.VERSION = "";
      await assert.rejects(execute(new URL(script, repositoryRoot).pathname, [], {
        cwd: repositoryRoot,
        env: environment,
      }), /VERSION must be explicitly set and nonempty/, `${script} accepted ${mode} VERSION`);
    }
  }
  for (const target of ["engine-wasm", "engine-wasm-reproducible"]) {
    for (const mode of ["unset", "empty"]) {
      const environment = {...process.env};
      delete environment.VERSION;
      const arguments_ = mode === "empty" ? [target, "VERSION="] : [target];
      await assert.rejects(execute("make", arguments_, {
        cwd: repositoryRoot,
        env: environment,
      }), /VERSION must be explicitly set and nonempty for make engine-wasm/, `make ${target} accepted ${mode} VERSION`);
    }
  }
});

test("Go artifact verification rejects a self-consistent forged release against package authority", async () => {
  const temporary = await mkdtemp(join(tmpdir(), "layerdraw-engine-wasm-forged-release-"));
  const artifactRoot = join(temporary, "artifact");
  await mkdir(artifactRoot);
  const manifestPath = new URL("../dist/engine-wasm.manifest.json", import.meta.url);
  const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
  for (const file of manifest.files) {
    await mkdir(dirname(join(artifactRoot, file.path)), {recursive: true});
    await cp(new URL(`../dist/${file.path}`, import.meta.url), join(artifactRoot, file.path));
  }
  const sbomPath = join(artifactRoot, "engine-wasm.cdx.json");
  const sbom = JSON.parse(await readFile(sbomPath, "utf8"));
  const packageManifest = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8"));
  const forgedVersion = "9.9.9";
  const oldRef = `pkg:npm/%40layerdraw/engine-wasm@${packageManifest.version}`;
  const forgedRef = `pkg:npm/%40layerdraw/engine-wasm@${forgedVersion}`;
  manifest.build.release_version = forgedVersion;
  manifest.build.flags[2] = manifest.build.flags[2].replace(`main.releaseVersion=${packageManifest.version}`, `main.releaseVersion=${forgedVersion}`);
  sbom.metadata.component.version = forgedVersion;
  sbom.metadata.component.purl = forgedRef;
  sbom.metadata.component["bom-ref"] = forgedRef;
  sbom.dependencies.find((entry) => entry.ref === oldRef).ref = forgedRef;
  const sbomBytes = Buffer.from(`${JSON.stringify(sbom)}\n`);
  await writeFile(sbomPath, sbomBytes);
  const entry = manifest.files.find((file) => file.path === "engine-wasm.cdx.json");
  entry.size = sbomBytes.byteLength;
  entry.digest = `sha256:${createHash("sha256").update(sbomBytes).digest("hex")}`;
  await writeFile(join(artifactRoot, "engine-wasm.manifest.json"), `${JSON.stringify(manifest)}\n`);
  await assert.rejects(execute("go", ["run", "./tools/wasmartifact", "verify", "-root", ".", "-output", artifactRoot, "-version", packageManifest.version], {
    cwd: repositoryRoot,
  }), /artifact manifest build identity mismatch/);
});

test("Go artifact verification rejects a self-consistent generated authority digest and ldflag forgery", async () => {
  const temporary = await mkdtemp(join(tmpdir(), "layerdraw-engine-wasm-forged-authority-"));
  const artifactRoot = join(temporary, "artifact");
  await mkdir(artifactRoot);
  const manifest = JSON.parse(await readFile(new URL("../dist/engine-wasm.manifest.json", import.meta.url), "utf8"));
  for (const file of manifest.files) {
    await mkdir(dirname(join(artifactRoot, file.path)), {recursive: true});
    await cp(new URL(`../dist/${file.path}`, import.meta.url), join(artifactRoot, file.path));
  }
  const authorityPath = join(artifactRoot, "engine-wasm.authority.json");
  const authority = JSON.parse(await readFile(authorityPath, "utf8"));
  authority.sbom_components[0].licenses[0].license.id = "forged";
  const authorityBytes = Buffer.from(`${JSON.stringify(authority)}\n`);
  await writeFile(authorityPath, authorityBytes);
  const authorityDigest = `sha256:${createHash("sha256").update(authorityBytes).digest("hex")}`;
  const oldDigest = manifest.sbom_authority.digest;
  manifest.sbom_authority.digest = authorityDigest;
  manifest.build.flags[2] = manifest.build.flags[2].replace(`main.sbomAuthorityDigest=${oldDigest}`, `main.sbomAuthorityDigest=${authorityDigest}`);
  const authorityEntry = manifest.files.find((file) => file.path === "engine-wasm.authority.json");
  authorityEntry.size = authorityBytes.byteLength;
  authorityEntry.digest = authorityDigest;

  const sbomPath = join(artifactRoot, "engine-wasm.cdx.json");
  const sbom = JSON.parse(await readFile(sbomPath, "utf8"));
  sbom.components[0].licenses[0].license.id = "forged";
  const sbomBytes = Buffer.from(`${JSON.stringify(sbom)}\n`);
  await writeFile(sbomPath, sbomBytes);
  const sbomEntry = manifest.files.find((file) => file.path === "engine-wasm.cdx.json");
  sbomEntry.size = sbomBytes.byteLength;
  sbomEntry.digest = `sha256:${createHash("sha256").update(sbomBytes).digest("hex")}`;
  await writeFile(join(artifactRoot, "engine-wasm.manifest.json"), `${JSON.stringify(manifest)}\n`);

  await assert.rejects(execute("go", ["run", "./tools/wasmartifact", "verify", "-root", ".", "-output", artifactRoot, "-version", "0.0.0"], {
    cwd: repositoryRoot,
  }), /artifact manifest generated\/build authority mismatch/);
});
