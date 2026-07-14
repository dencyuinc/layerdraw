# `@layerdraw/engine-client`

Transport-neutral TypeScript contract and lifecycle core for the LayerDraw Engine.

The root export contains only the portable client interface, compile outcome types,
creation options, and safe exception classes. It does not export a raw request,
handshake, transport, LDL parser, or environment-specific implementation.

Native stdio and browser Worker adapters are separate follow-up entrypoints. They
compose with the private byte-transport seam and do not change this root contract.
