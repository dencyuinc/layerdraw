// SPDX-License-Identifier: Apache-2.0

import {
  collectCompileInputBlobRefs,
  collectCompileResultBlobRefs,
} from "@layerdraw/protocol/engine";
import type { InternalProtocolBlobRefCollectors } from "./transport.js";

export const protocolBlobRefCollectors: InternalProtocolBlobRefCollectors =
  Object.freeze({
    collectCompileInputBlobRefs,
    collectCompileResultBlobRefs,
  });
