---
"@layerdraw/engine-client": minor
"@layerdraw/engine-wasm": minor
---

Add the transport-neutral Engine client with isolated Node stdio and browser
WASM Worker entry points. Both transports now expose the same negotiated
compile outcomes, bounded BlobRef transfer, cancellation, restart, and
disposal behavior.

Expose the WASM Worker transport's terminal `closed` signal so higher-level
clients can join endpoint shutdown without interpreting Worker internals.

This is the first public release intent for these entry points, so no migration
or deprecation action is required.
