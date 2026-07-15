# `@layerdraw/engine-client`

Transport-neutral TypeScript contract and lifecycle core for the LayerDraw Engine.

The root export contains only the portable client interface, compile outcome types,
creation options, and safe exception classes. It does not export a raw request,
handshake, transport, LDL parser, or environment-specific implementation.

Use `@layerdraw/engine-client/stdio` to start the native `layerdraw-engine`
sidecar, or `@layerdraw/engine-client/wasm` to create an isolated browser Worker
endpoint. Both compose with the private byte-transport seam and leave the root
contract environment-neutral.
