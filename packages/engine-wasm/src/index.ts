// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

export {
  EngineWorkerTransportError,
  createEngineWorkerTransport,
  type CreateEngineWorkerTransportOptions,
  type EngineWorkerExchange,
  type EngineWorkerRequest,
  type EngineWorkerResponse,
  type EngineWorkerTransport,
  type WorkerEventLike,
  type WorkerLike,
} from "./host.js";
export {
  maxBlobIDBytes,
  maxBuffers,
  maxControlBytes,
  maxControlDepth,
  maxEndpointGenerationBytes,
  maxExchangeIDBytes,
  workerProtocol,
  workerProtocolVersion,
  type EngineWorkerBlob,
  type EngineWorkerFailure,
  type EngineWorkerFailureCode,
  type EngineWorkerFailurePhase,
  type EngineWorkerTransportLimits,
} from "./protocol.js";
