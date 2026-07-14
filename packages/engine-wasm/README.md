# `@layerdraw/engine-wasm`

This package is the low-level, browser-only dedicated Worker transport for the LayerDraw Go WASM Engine. It is not the public Engine client: protocol negotiation, generated envelope decoding, retries, and `AbortSignal` policy belong to `@layerdraw/engine-client`.

The root export is safe to import during SSR. Creating a transport is the explicit action that starts the package's co-distributed module Worker and verified Go/WASM endpoint:

```ts
import {createEngineWorkerTransport} from "@layerdraw/engine-wasm";

const transport = createEngineWorkerTransport({
  endpointGeneration,
  expectedArtifactManifestDigest,
  releaseManifestDigest,
});
```

The adapter accepts one synchronous request at a time. `control` is opaque generated Engine Protocol data, and attachments contain only `blob_id` plus fixed `ArrayBuffer` bytes; TypeScript does not parse, negotiate, repair, or interpret Engine semantics. The Worker boundary uses ownership transfer with bounded JS/Go copies—it is not an end-to-end zero-copy claim.

Hard cancellation is endpoint replacement. A caller invalidates the generation and terminates the Worker; sending a cancel message to the same Worker cannot interrupt a CPU-bound Go `syscall/js` call. A replacement requires a new generation, new Go endpoint instance, and fresh generated handshake.

The package has no runtime dependencies, install scripts, network downloads, storage fallback, fake compiler fallback, or UI-thread WASM execution. Its module Worker verifies and runs the co-distributed real Go `.wasm`; the package also ships the byte-identical pinned `wasm_exec.js`, Worker contract corpus, artifact manifest, CycloneDX SBOM, and legal inventory beneath `dist/`.

The initial tested shell requires a dedicated module Worker, WebAssembly, transferable fixed `ArrayBuffer`, structured clone, `crypto.getRandomValues`, `performance.now`, `TextEncoder`/`TextDecoder`, and `fetch` or an adapter-provided verified-byte loader. Playwright is pinned to 1.61.1 with Chromium 149.0.7827.55 (revision 1228), Firefox 151.0 (revision 1532), and WebKit 26.5 (revision 2311); the checked-in test metadata is the exact matrix input. This does not claim Node as a production transport, SharedWorker, Service Worker, `SharedArrayBuffer`, WASM threads, mobile browsers, embedded WebViews, or a portable browser process-memory ceiling.

Serving must provide JavaScript module MIME types, `application/wasm` for the artifact, correct same-origin/CORS headers, and a CSP that explicitly permits the selected `worker-src`, `script-src`, and WASM `connect-src` origins.
