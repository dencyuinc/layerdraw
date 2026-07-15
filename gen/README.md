# Generated Bindings

Committed generated bindings derived exclusively from `schemas/`. Go packages
live under `gen/go/*`; generated TypeScript sources live in
`packages/protocol/src/*.gen.ts`. Both contain generated validators and
canonical encode/decode codecs. `schema-digests.json` binds every raw schema
input, each group import closure, the aggregate schema set, and the exact
repository-owned generator version (`layerdraw-protocolgen/1`).

Generated files must be reproducible from the pinned toolchain and schema
digest. They are Apache-2.0, transport/framework/runtime free, and must not be
edited by hand.
