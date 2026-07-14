// SPDX-License-Identifier: Apache-2.0

import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {readFile} from "node:fs/promises";
import test from "node:test";

import {
  decodeCompileResponseEnvelope,
  encodeCompileResponseEnvelope,
} from "../dist/engine.gen.js";

const repositoryRoot = new URL("../../../", import.meta.url);
const manifestURL = new URL("schemas/fixtures/normalized/v1/manifest.json", repositoryRoot);

async function readManifest() {
  return JSON.parse(await readFile(manifestURL, "utf8"));
}

async function readOpaqueBytes(relativePath) {
  return new Uint8Array(await readFile(new URL(relativePath, repositoryRoot)));
}

function digest(bytes) {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}

function publicationFromControlEnvelope(envelope) {
  const normalized = envelope.payload?.normalized_artifact;
  assert.ok(normalized);
  if (normalized.kind === "project") {
    assert.ok(normalized.project);
    assert.equal(normalized.pack, undefined);
    return {
      kind: "project",
      branch: "project",
      rootAddress: normalized.project.project_address,
      canonical: {role: "normalized_project_canonical_json", ...normalized.project.canonical_json},
      artifact: {role: "normalized_project_artifact_json", ...normalized.project.artifact_json},
    };
  }
  assert.equal(normalized.kind, "pack");
  assert.ok(normalized.pack);
  assert.equal(normalized.project, undefined);
  return {
    kind: "pack",
    branch: "pack",
    rootAddress: normalized.pack.pack_address,
    canonical: {role: "normalized_pack_canonical_json", ...normalized.pack.canonical_json},
    artifact: {role: "normalized_pack_artifact_json", ...normalized.pack.artifact_json},
  };
}

function verifyOpaquePublication(publication, blobs, expected) {
  assert.equal(publication.kind, expected.kind);
  assert.equal(publication.branch, publication.kind);
  assert.equal(publication.rootAddress, expected.rootAddress);
  const contracts = {
    "project:canonical": ["normalized_project_canonical_json", "application/vnd.layerdraw.normalized-project.v1+json"],
    "project:artifact": ["normalized_project_artifact_json", "application/vnd.layerdraw.project.v1+json"],
    "pack:canonical": ["normalized_pack_canonical_json", "application/vnd.layerdraw.normalized-pack.v1+json"],
    "pack:artifact": ["normalized_pack_artifact_json", "application/vnd.layerdraw.pack.v1+json"],
  };
  const verified = {};
  for (const [name, ref, expectedBytes] of [
    ["canonical", publication.canonical, expected.canonical],
    ["artifact", publication.artifact, expected.artifact],
  ]) {
    const [role, mediaType] = contracts[`${publication.kind}:${name}`];
    assert.equal(ref.role, role);
    assert.equal(ref.media_type, mediaType);
    assert.equal(ref.lifetime, "request");
    const bytes = blobs.get(ref.blob_id);
    assert.ok(bytes instanceof Uint8Array, `${name} body is missing`);
    assert.equal(BigInt(bytes.byteLength), BigInt(ref.size));
    assert.equal(digest(bytes), ref.digest);
    assert.deepEqual(bytes, expectedBytes, `${name} body belongs to another Engine generation`);
    verified[name] = bytes;
  }
  assert.notEqual(publication.canonical.blob_id, publication.artifact.blob_id);
  assert.notEqual(verified.canonical.at(-1), 0x0a);
  const publicProfile = new Uint8Array(verified.canonical.byteLength + 1);
  publicProfile.set(verified.canonical);
  publicProfile[publicProfile.length - 1] = 0x0a;
  assert.deepEqual(verified.artifact, publicProfile);
  return verified;
}

function clonePublication(value) {
  return {...value, canonical: {...value.canonical}, artifact: {...value.artifact}};
}

test("normalized fixture blobs remain verified opaque byte arrays", async (context) => {
  const manifest = await readManifest();
  assert.equal(manifest.schema_version, 1);
  assert.equal(manifest.fixtures.length, 2);
  for (const fixture of manifest.fixtures) await context.test(fixture.kind, async () => {
    const controlText = await readFile(new URL(fixture.control_envelope, repositoryRoot), "utf8");
    const control = decodeCompileResponseEnvelope(controlText);
    const canonicalControl = encodeCompileResponseEnvelope(control);
    assert.equal(encodeCompileResponseEnvelope(decodeCompileResponseEnvelope(canonicalControl)), canonicalControl);
    const canonicalControlPath = fixture.control_envelope.replace("/engine/", "/conformance/engine/");
    assert.equal(canonicalControl, (await readFile(new URL(canonicalControlPath, repositoryRoot), "utf8")).trim());

    const publication = publicationFromControlEnvelope(control);
    const canonical = await readOpaqueBytes(fixture.canonical_json.file);
    const artifact = await readOpaqueBytes(fixture.artifact_json.file);
    const canonicalBefore = canonical.slice();
    const artifactBefore = artifact.slice();
    const blobs = new Map([
      [fixture.canonical_json.blob_id, canonical],
      [fixture.artifact_json.blob_id, artifact],
    ]);
    const verified = verifyOpaquePublication(publication, blobs, {
      kind: fixture.kind,
      rootAddress: fixture.root_address,
      canonical,
      artifact,
    });

    const canonicalMetadata = {...fixture.canonical_json};
    delete canonicalMetadata.file;
    const artifactMetadata = {...fixture.artifact_json};
    delete artifactMetadata.file;
    assert.deepEqual(publication.canonical, canonicalMetadata);
    assert.deepEqual(publication.artifact, artifactMetadata);
    assert.strictEqual(verified.canonical, canonical);
    assert.strictEqual(verified.artifact, artifact);
    assert.deepEqual(canonical, canonicalBefore);
    assert.deepEqual(artifact, artifactBefore);
  });
});

test("opaque normalized publication checks reject boundary mismatches", async (context) => {
  const manifest = await readManifest();
  const fixture = manifest.fixtures.find((value) => value.kind === "project");
  const packFixture = manifest.fixtures.find((value) => value.kind === "pack");
  const control = decodeCompileResponseEnvelope(await readFile(new URL(fixture.control_envelope, repositoryRoot), "utf8"));
  const packControl = decodeCompileResponseEnvelope(await readFile(new URL(packFixture.control_envelope, repositoryRoot), "utf8"));
  const publication = publicationFromControlEnvelope(control);
  const packPublication = publicationFromControlEnvelope(packControl);
  const canonical = await readOpaqueBytes(fixture.canonical_json.file);
  const artifact = await readOpaqueBytes(fixture.artifact_json.file);
  const expected = {kind: "project", rootAddress: fixture.root_address, canonical, artifact};
  const baseBlobs = new Map([[publication.canonical.blob_id, canonical], [publication.artifact.blob_id, artifact]]);

  const cases = [
    ["missing body", publication, new Map([[publication.artifact.blob_id, artifact]])],
    ["digest mismatch", (() => { const value = clonePublication(publication); value.canonical.digest = `sha256:${"0".repeat(64)}`; return value; })(), baseBlobs],
    ["size mismatch", (() => { const value = clonePublication(publication); value.artifact.size = String(artifact.byteLength + 1); return value; })(), baseBlobs],
    ["swapped refs", (() => { const value = clonePublication(publication); [value.canonical, value.artifact] = [value.artifact, value.canonical]; return value; })(), baseBlobs],
    ["wrong Project Pack media", (() => { const value = clonePublication(publication); value.canonical.media_type = packPublication.canonical.media_type; return value; })(), baseBlobs],
    ["wrong role media", (() => { const value = clonePublication(publication); value.canonical.media_type = value.artifact.media_type; return value; })(), baseBlobs],
    ["branch mismatch", {...clonePublication(publication), branch: "pack"}, baseBlobs],
    ["root mismatch", {...clonePublication(publication), rootAddress: "ldl:project:other"}, baseBlobs],
  ];
  for (const [name, candidate, blobs] of cases) await context.test(name, () => {
    assert.throws(() => verifyOpaquePublication(candidate, blobs, expected));
  });

  await context.test("mismatched fixture generation", () => {
    const driftCanonical = canonical.slice();
    assert.equal(driftCanonical[393], 0x46);
    driftCanonical[393] = 0x44;
    const driftArtifact = new Uint8Array(driftCanonical.byteLength + 1);
    driftArtifact.set(driftCanonical);
    driftArtifact[driftArtifact.length - 1] = 0x0a;
    const drift = clonePublication(publication);
    drift.canonical.digest = digest(driftCanonical);
    drift.canonical.size = String(driftCanonical.byteLength);
    drift.artifact.digest = digest(driftArtifact);
    drift.artifact.size = String(driftArtifact.byteLength);
    const blobs = new Map([[drift.canonical.blob_id, driftCanonical], [drift.artifact.blob_id, driftArtifact]]);
    assert.throws(() => verifyOpaquePublication(drift, blobs, expected), /another Engine generation/);
  });
});
