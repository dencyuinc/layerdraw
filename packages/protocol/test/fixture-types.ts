// SPDX-License-Identifier: Apache-2.0

import {
  isCompileRequestEnvelope,
  isCompileResponseEnvelope,
  isHandshakeResponseEnvelope,
} from "../src/engine.gen.js";
import type {
  CompileRequestEnvelope,
  CompileResponseEnvelope,
  HandshakeResponseEnvelope,
} from "../src/engine.gen.js";

export function narrowFixture(value: unknown):
  | CompileRequestEnvelope
  | CompileResponseEnvelope
  | HandshakeResponseEnvelope
  | undefined {
  if (isCompileRequestEnvelope(value)) return value;
  if (isCompileResponseEnvelope(value)) return value;
  if (isHandshakeResponseEnvelope(value)) return value;
  return undefined;
}
