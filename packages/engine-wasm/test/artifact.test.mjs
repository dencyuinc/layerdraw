// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import {readFile} from "node:fs/promises";
import test from "node:test";

import {createVerifiedWasmEndpoint} from "../dist/artifact.js";
import {sha256} from "./shared/real-engine.mjs";

const canonicalManifest = JSON.parse(await readFile(new URL("../dist/engine-wasm.manifest.json", import.meta.url), "utf8"));
const canonicalPackageManifestBytes = await readFile(new URL("../package.json", import.meta.url));
const packageManifestURL = "https://package.invalid/package.json";

function arrayBuffer(value) {
  const bytes = Buffer.from(value);
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength);
}

async function assertRejectedBeforeArtifactReads(mutate) {
  const manifest = structuredClone(canonicalManifest);
  mutate(manifest);
  const bytes = arrayBuffer(`${JSON.stringify(manifest)}\n`);
  const expectedArtifactManifestDigest = await sha256(bytes);
  let reads = 0;
  await assert.rejects(createVerifiedWasmEndpoint({
    endpointGeneration: "metadata-negative-test",
    expectedArtifactManifestDigest,
    releaseManifestDigest: `sha256:${"5".repeat(64)}`,
  }, {
    artifactBaseURL: "https://artifact.invalid/dist/",
    packageManifestURL,
    async loadBytes(url) {
      reads += 1;
      if (url === packageManifestURL) return arrayBuffer(canonicalPackageManifestBytes);
      if (url.endsWith("/engine-wasm.manifest.json")) return bytes.slice(0);
      throw new Error("manifest validation read an artifact file");
    },
  }), (error) => error?.failure?.code === "engine.worker.artifact_mismatch");
  assert.equal(reads, 2, "falsified metadata reached artifact file loading");
}

test("the TypeScript loader rejects every closed browser-contract field mutation before artifact reads", async (context) => {
  const cases = [
    ["module dedicated Worker", (value) => { value.browser_contract.module_dedicated_worker = false; }],
    ["SharedArrayBuffer", (value) => { value.browser_contract.shared_array_buffer = true; }],
    ["WASM threads", (value) => { value.browser_contract.wasm_threads = true; }],
  ];
  for (let index = 0; index < canonicalManifest.browser_contract.required_primitives.length; index += 1) {
    const primitive = canonicalManifest.browser_contract.required_primitives[index];
    cases.push([`required primitive ${primitive}`, (value) => { value.browser_contract.required_primitives[index] = "falsified"; }]);
  }
  for (const [name, mutate] of cases) await context.test(name, () => assertRejectedBeforeArtifactReads(mutate));
});

test("the loader rejects a self-consistent artifact release mutation against external package authority", async () => {
  await assertRejectedBeforeArtifactReads((value) => {
    value.build.release_version = "9.9.9";
    value.build.flags[2] = value.build.flags[2].replace("main.releaseVersion=0.0.0", "main.releaseVersion=9.9.9");
  });
});

async function assertSBOMMutationRejected(mutate) {
  const manifest = structuredClone(canonicalManifest);
  const sbom = JSON.parse(await readFile(new URL("../dist/engine-wasm.cdx.json", import.meta.url), "utf8"));
  mutate(sbom);
  const sbomBytes = arrayBuffer(`${JSON.stringify(sbom)}\n`);
  const sbomEntry = manifest.files.find((file) => file.path === "engine-wasm.cdx.json");
  sbomEntry.size = sbomBytes.byteLength;
  sbomEntry.digest = await sha256(sbomBytes);
  const manifestBytes = arrayBuffer(`${JSON.stringify(manifest)}\n`);
  const expectedArtifactManifestDigest = await sha256(manifestBytes);
  await assert.rejects(createVerifiedWasmEndpoint({
    endpointGeneration: "sbom-negative-test",
    expectedArtifactManifestDigest,
    releaseManifestDigest: `sha256:${"5".repeat(64)}`,
  }, {
    artifactBaseURL: "https://artifact.invalid/dist/",
    packageManifestURL,
    async loadBytes(url) {
      if (url === packageManifestURL) return arrayBuffer(canonicalPackageManifestBytes);
      const relative = new URL(url).pathname.split("/dist/").at(-1);
      if (relative === "engine-wasm.manifest.json") return manifestBytes.slice(0);
      if (relative === "engine-wasm.cdx.json") return sbomBytes.slice(0);
      const bytes = await readFile(new URL(`../dist/${relative}`, import.meta.url));
      return arrayBuffer(bytes);
    },
  }), (error) => error?.failure?.code === "engine.worker.artifact_mismatch");
}

async function assertGeneratedAuthorityMutationRejected(mutate, expectedCode = "engine.worker.artifact_mismatch") {
  const manifest = structuredClone(canonicalManifest);
  const authority = JSON.parse(await readFile(new URL("../dist/engine-wasm.authority.json", import.meta.url), "utf8"));
  mutate(authority, manifest);
  const authorityBytes = arrayBuffer(`${JSON.stringify(authority)}\n`);
  const authorityDigest = await sha256(authorityBytes);
  const oldDigest = manifest.sbom_authority.digest;
  const authorityEntry = manifest.files.find((file) => file.path === "engine-wasm.authority.json");
  authorityEntry.size = authorityBytes.byteLength;
  authorityEntry.digest = authorityDigest;
  manifest.sbom_authority.digest = authorityDigest;
  manifest.build.flags[2] = manifest.build.flags[2].replace(`main.sbomAuthorityDigest=${oldDigest}`, `main.sbomAuthorityDigest=${authorityDigest}`);
  const manifestBytes = arrayBuffer(`${JSON.stringify(manifest)}\n`);
  const expectedArtifactManifestDigest = await sha256(manifestBytes);
  await assert.rejects(createVerifiedWasmEndpoint({
    endpointGeneration: "generated-authority-negative-test",
    expectedArtifactManifestDigest,
    releaseManifestDigest: `sha256:${"5".repeat(64)}`,
  }, {
    artifactBaseURL: "https://artifact.invalid/dist/",
    packageManifestURL,
    async loadBytes(url) {
      if (url === packageManifestURL) return arrayBuffer(canonicalPackageManifestBytes);
      const relative = new URL(url).pathname.split("/dist/").at(-1);
      if (relative === "engine-wasm.manifest.json") return manifestBytes.slice(0);
      if (relative === "engine-wasm.authority.json") return authorityBytes.slice(0);
      const bytes = await readFile(new URL(`../dist/${relative}`, import.meta.url));
      return arrayBuffer(bytes);
    },
  }), (error) => error?.failure?.code === expectedCode);
}

test("the loader mechanically rejects every generated authority relation mutation", async (context) => {
  const component = (value) => value.sbom_components[0];
  const cases = [
    ["authority version", (value) => { value.authority_version = 2; }],
    ["Go version", (value) => { value.go_version = "go9.9.9"; }],
    ["manifest runtime file", (value) => { value.manifest_runtime_support.file = "other.js"; }],
    ["manifest runtime Go version", (value) => { value.manifest_runtime_support.go_version = "go9.9.9"; }],
    ["manifest runtime digest", (value) => { value.manifest_runtime_support.digest = `sha256:${"0".repeat(64)}`; }],
    ["manifest product license", (value) => { value.manifest_licenses.product = "MIT"; }],
    ["manifest runtime license", (value) => { value.manifest_licenses.runtime_support = "MIT"; }],
    ["manifest SBOM path", (value) => { value.manifest_licenses.sbom = "other.cdx.json"; }],
    ["module build path", (value) => { value.module_build_info[0].path = "example.com/forged"; }, "engine.worker.initialization_failed"],
    ["module build version", (value) => { value.module_build_info[0].version = "v9.9.9"; }, "engine.worker.initialization_failed"],
    ["component type", (value) => { component(value).type = "framework"; }],
    ["component name", (value) => { component(value).name = "example.com/forged"; }],
    ["component version", (value) => { component(value).version = "v9.9.9"; }],
    ["component purl", (value) => { component(value).purl = "pkg:golang/example.com/forged@v9.9.9"; }],
    ["component bom ref", (value) => { component(value)["bom-ref"] = "pkg:golang/example.com/forged@v9.9.9"; }],
    ["component scope", (value) => { component(value).scope = "optional"; }],
    ["component license", (value) => { component(value).licenses[0].license.id = "forged"; }],
    ["root license", (value) => { value.sbom_root_licenses[0].license.name = "forged"; }],
    ["root dependency", (value) => { value.sbom_root_depends_on[0] = "forged"; }],
    ["leaf dependency ref", (value) => { value.sbom_leaf_dependencies[0].ref = "forged"; }],
    ["leaf dependency edge", (value) => { value.sbom_leaf_dependencies[0].dependsOn.push("forged"); }],
  ];
  for (const [name, mutate, expectedCode] of cases) {
    await context.test(name, () => assertGeneratedAuthorityMutationRejected(mutate, expectedCode));
  }
});

test("the loader rejects every manifest legal field that differs from generated Go authority", async (context) => {
  const cases = [
    ["product license", (value) => { value.licenses.product = "MIT"; }],
    ["runtime license", (value) => { value.licenses.runtime_support = "MIT"; }],
    ["SBOM path", (value) => { value.licenses.sbom = "other.cdx.json"; }],
  ];
  for (const [name, mutate] of cases) {
    await context.test(name, () => assertGeneratedAuthorityMutationRejected((_authority, manifest) => mutate(manifest)));
  }
});

test("the loader rejects every runtime, module, reference, and dependency mutation", async (context) => {
  const runtime = (value) => value.components.at(-1);
  const module = (value) => value.components[0];
  const rootDependency = (value) => value.dependencies[0];
  const cases = [
    ["runtime type", (value) => { runtime(value).type = "library"; }],
    ["runtime name", (value) => { runtime(value).name = "other"; }],
    ["runtime version", (value) => { runtime(value).version = "go9.9.9"; }],
    ["runtime purl", (value) => { runtime(value).purl = "pkg:generic/other@go1.26.5"; }],
    ["runtime bom ref", (value) => { runtime(value)["bom-ref"] = "pkg:generic/other@go1.26.5"; }],
    ["runtime scope", (value) => { runtime(value).scope = "optional"; }],
    ["runtime hash algorithm", (value) => { runtime(value).hashes[0].alg = "SHA-512"; }],
    ["runtime hash content", (value) => { runtime(value).hashes[0].content = "0".repeat(64); }],
    ["runtime license", (value) => { runtime(value).licenses[0].license.id = "MIT"; }],
    ["module type", (value) => { module(value).type = "framework"; }],
    ["module name", (value) => { module(value).name = "example.com/forged"; }],
    ["module version", (value) => { module(value).version = "v9.9.9"; }],
    ["module purl", (value) => { module(value).purl = "pkg:golang/example.com/forged@v1.0.0"; }],
    ["module bom ref", (value) => { module(value)["bom-ref"] = "pkg:golang/example.com/forged@v1.0.0"; }],
    ["module scope", (value) => { module(value).scope = "optional"; }],
    ["module license", (value) => { module(value).licenses[0].license.id = "MIT"; }],
    ["duplicate component", (value) => { value.components.push(structuredClone(module(value))); }],
    ["missing component", (value) => { value.components.shift(); }],
    ["root dependency ref", (value) => { rootDependency(value).ref = "pkg:npm/%40layerdraw/engine-wasm@9.9.9"; }],
    ["missing dependency edge", (value) => { rootDependency(value).dependsOn.shift(); }],
    ["extra dependency edge", (value) => { rootDependency(value).dependsOn.push("pkg:golang/forged@v1.0.0"); }],
    ["reordered dependency edges", (value) => { rootDependency(value).dependsOn.reverse(); }],
    ["leaf dependency edge", (value) => { value.dependencies[1].dependsOn.push(value.dependencies[2].ref); }],
    ["duplicate dependency ref", (value) => { value.dependencies.push(structuredClone(value.dependencies[1])); }],
  ];
  for (const [name, mutate] of cases) await context.test(name, () => assertSBOMMutationRejected(mutate));
});
