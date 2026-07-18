# `@layerdraw/protocol`

This Apache-2.0 package contains generated, transport-neutral wire values for
LayerDraw protocols. Import only a documented subpath (`/common`, `/semantic`,
`/access`, `/engine`, or `/runtime`); there is intentionally no root barrel
export.

All files matching `src/*.gen.ts` are generated from `schemas/` by
`make generate`. They must not contain LDL parsing, normalization, hashing,
filesystem, network, framework, Runtime, or SDK behavior.

Each exported type has `is*`, `decode*`, and `encode*` functions. Decoders
validate closed shapes, scalar bounds, tagged unions, and outcome invariants;
encoders emit the canonical JSON contract documented in `schemas/README.md`.
Predicates are total boolean validators: hostile accessors or proxies, active
container cycles, container depth above 128, and non-scalar Unicode object keys
return `false` rather than throwing. Container depth 128, valid Unicode keys,
and acyclic shared aliases remain valid when the referenced schema permits them.
The package has zero runtime dependencies and is safe to import in browser and
SSR JavaScript without Node, DOM, Worker, transport, or framework globals.
