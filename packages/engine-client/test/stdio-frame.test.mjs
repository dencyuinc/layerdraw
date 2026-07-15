// SPDX-License-Identifier: Apache-2.0

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import test from "node:test";

import {
  encodeStdioFrame,
  StdioFrameDecoder,
  StdioFrameError,
  stdioKind,
  validateStdioFrame,
} from "../dist/internal/stdio-frame.js";

const corpus = resolve(import.meta.dirname, "../../../tests/conformance/stdio/v1");
const manifest = JSON.parse(await readFile(resolve(corpus, "manifest.json"), "utf8"));

test("TypeScript decodes and byte-identically re-encodes the Go LDSP 1.0 corpus", async () => {
  assert.equal(manifest.fixtures.length, 8);
  for (const fixture of manifest.fixtures) {
    const bytes = new Uint8Array(await readFile(resolve(corpus, fixture.file)));
    for (const split of [bytes.byteLength, 1, 7, 39]) {
      const decoder = new StdioFrameDecoder();
      const decoded = [];
      for (let offset = 0; offset < bytes.byteLength; offset += split) {
        decoded.push(...decoder.push(bytes.subarray(offset, offset + split)));
      }
      decoder.finish();
      assert.equal(decoded.length, 1, fixture.file);
      const frame = decoded[0];
      assert.equal(frame.kind, fixture.kind_value);
      assert.equal(frame.flags, fixture.flags);
      assert.equal(frame.streamId, BigInt(fixture.stream_id));
      assert.equal(frame.sequence, fixture.sequence);
      assert.equal(Buffer.from(frame.name).toString("hex"), fixture.name_hex);
      assert.equal(Buffer.from(frame.payload).toString("hex"), fixture.payload_hex);
      assert.equal(frame.offset, BigInt(fixture.offset));
      assert.deepEqual(encodeStdioFrame(frame), bytes);
    }
  }
});

test("decoder handles adjacent frames and rejects truncated or invalid headers", async () => {
  const first = new Uint8Array(await readFile(resolve(corpus, "request-ready.frame")));
  const second = new Uint8Array(await readFile(resolve(corpus, "bundle-end.frame")));
  const joined = new Uint8Array(first.byteLength + second.byteLength);
  joined.set(first);
  joined.set(second, first.byteLength);
  const decoder = new StdioFrameDecoder();
  assert.equal(decoder.push(new Uint8Array(0)).length, 0);
  assert.equal(decoder.push(joined).length, 2);
  decoder.finish();

  for (const mutate of [
    (value) => { value[0] = 0; },
    (value) => { value[4] = 2; },
    (value) => { value[6] = 0xff; },
    (value) => { value[7] = 1; },
    (value) => new DataView(value.buffer).setUint32(20, 4_097),
    (value) => new DataView(value.buffer).setBigUint64(24, 8_388_609n),
  ]) {
    const invalid = first.slice();
    mutate(invalid);
    assert.throws(() => new StdioFrameDecoder().push(invalid), StdioFrameError);
  }
  const truncated = new StdioFrameDecoder();
  truncated.push(first.subarray(0, first.byteLength - 1));
  assert.throws(() => truncated.finish(), StdioFrameError);
});

test("context-free validator enforces every kind-specific field contract", () => {
  const empty = new Uint8Array(0);
  const base = {
    flags: 0,
    streamId: 1n,
    sequence: 0,
    name: empty,
    payload: empty,
    offset: 0n,
  };
  const invalid = [
    {...base, kind: stdioKind.requestControl},
    {...base, kind: stdioKind.requestReady, streamId: 0n},
    {...base, kind: stdioKind.blobChunk, sequence: 1, name: new Uint8Array([0xff]), flags: 1},
    {...base, kind: stdioKind.blobChunk, sequence: 1, name: new Uint8Array([97]), payload: new Uint8Array(1), flags: 0},
    {...base, kind: stdioKind.bundleEnd, sequence: 0},
    {...base, kind: stdioKind.cancel, payload: new Uint8Array([1])},
    {...base, kind: stdioKind.responseControl, payload: new TextEncoder().encode("not-json")},
    {...base, kind: stdioKind.close, streamId: 1n},
    {...base, kind: stdioKind.streamError, name: new Uint8Array([0x80])},
  ];
  for (const frame of invalid) {
    assert.throws(() => validateStdioFrame(frame), StdioFrameError);
  }
});
