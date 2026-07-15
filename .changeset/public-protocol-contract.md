---
"@layerdraw/protocol": minor
---

Publish the new `@layerdraw/protocol` package with generated, transport-neutral
wire types, validators, decoders, and canonical JSON encoders at the `common`,
`semantic`, and `engine` subpaths.

This minor release establishes the first public package contract. Consumers
should migrate handwritten protocol shapes or repository-internal generated
imports to `@layerdraw/protocol/common`, `@layerdraw/protocol/semantic`, or
`@layerdraw/protocol/engine`; the package root and `dist/` paths are not public
exports. No previous public package entry point is deprecated, so there is no
deprecation window or automated migration.
