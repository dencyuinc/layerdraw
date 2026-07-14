# `@layerdraw/protocol`

This Apache-2.0 package contains generated, transport-neutral wire values for
LayerDraw protocols. Import only a documented subpath (`/common`, `/semantic`,
or `/engine`); there is intentionally no root barrel export.

All files matching `src/*.gen.ts` are generated from `schemas/` by
`make generate`. They must not contain LDL parsing, normalization, hashing,
filesystem, network, framework, Runtime, or SDK behavior.

Each exported type has `is*`, `decode*`, and `encode*` functions. Decoders
validate closed shapes, scalar bounds, tagged unions, and outcome invariants;
encoders emit the canonical JSON contract documented in `schemas/README.md`.
The package has zero runtime dependencies and is safe to import in browser and
SSR JavaScript without Node, DOM, Worker, transport, or framework globals.
