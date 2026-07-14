# RFC 0001: Public protocol package and generation boundary

- Status: Accepted
- Authors: LayerDraw maintainers
- Approval: Dencyuman, protocol/release maintainer, 2026-07-15
- Tracking issue: #27
- Target release: the first release containing `@layerdraw/protocol`

## Context

LayerDraw needs one language-neutral contract between the Go Engine boundary
and TypeScript hosts without making compiler or runtime implementation types a
public API. The repository already defines that contract in `schemas/` using
LayerDraw Protocol Schema Dialect v1, which composes JSON Schema draft 2020-12
with required LayerDraw assertion vocabulary. Committed Go and TypeScript
bindings carry matching schema-closure digests.

Publishing the TypeScript binding introduces a public package and a release
boundary. Repository policy therefore requires an explicit decision about the
package surface, the authority of the schemas and generator, compatibility,
and the checks that keep published artifacts aligned with committed source.

## Decision

LayerDraw will publish `@layerdraw/protocol` as the transport-neutral
TypeScript representation of its generated wire contracts. The package is
Apache-2.0 licensed, has no runtime dependencies, and supports browser and SSR
consumers without Node, DOM, Worker, transport, framework, compiler, or Runtime
behavior.

The public surface is limited to the documented `@layerdraw/protocol/common`,
`@layerdraw/protocol/semantic`, and `@layerdraw/protocol/engine` exports. There
is no root barrel export and `dist/` paths are not public exports. Each subpath
provides generated wire types, structural predicates, validated decoders, and
canonical JSON encoders. Raw schemas, generator internals, generated source
paths, compiler types, and runtime types are not package entry points.

The LayerDraw protocol schemas and dialect meta-schema in `schemas/` are the
source of truth. A validator that does not implement the dialect's required
assertion vocabulary must refuse the schemas. `tools/protocolgen` is the sole
writer for the committed Go bindings in `gen/go/`, the TypeScript sources in
`packages/protocol/src/*.gen.ts`, and the schema digest manifest.
Generated files are never edited by hand. Changes to a wire contract start in
the schemas, regenerate both languages in one change, and carry shared
conformance coverage.

`make generate-check` runs generation from the committed revision in an
independent temporary clone, compares NUL-delimited manifests of every
non-Git filesystem entry (including ignored paths, types, modes, contents, and
symlink targets), permits changes only to the declared generated-output set,
and requires two clean passes. A frozen offline install materializes per-run
dependency copies from a read-only package store, and both dependencies and
tool caches live outside the cloned repository. Dependency copies are also
manifested so mutation attempts fail without exposing caller dependency state.
Package verification builds and packs the actual npm artifact, checks its
export boundary in Node and browser conditions, and rejects source maps whose
source is neither packaged nor embedded. Release jobs publish the already
verified artifact rather than regenerating it.

Protocol compatibility and npm package compatibility are related but distinct
version axes. Wire documents follow the `major.minor` rules in
`schemas/README.md`: breaking shape or canonicalization changes require a new
protocol major, while additive documented changes may use a protocol minor.
The npm package follows SemVer through Changesets and remains part of the
repository's fixed LayerDraw release set. A package release must not imply that
an endpoint supports a protocol version; negotiation remains explicit in the
wire contract.

## Approval record

On 2026-07-15, Dencyuman approved RFC 0001 in the protocol/release maintainer
role, covering both the public protocol boundary and its release policy.

## Alternatives considered

### Handwritten TypeScript contracts

Rejected because parallel handwritten Go and TypeScript shapes would create
multiple authorities and make cross-language canonicalization drift likely.

### Export compiler or Runtime types directly

Rejected because those types contain implementation and lifecycle concerns,
are not transport-neutral, and are intentionally excluded from the portable
compile boundary.

### Publish schemas only

Rejected as the only supported interface because each consumer would select
its own generator and validation behavior. The schemas remain available as
repository authority, while the package supplies the supported TypeScript
binding.

### Publish generated source and deep `dist/` imports

Rejected because source layout and compiler output are implementation details.
Only named package subpaths are compatibility commitments.

## Consequences

- TypeScript consumers get one supported, runtime-dependency-free package for
  validated LayerDraw wire values and canonical encoding.
- Schema changes must update both committed language bindings, digests,
  conformance fixtures, and the appropriate Changeset in the same change.
- Consumers must import documented subpaths and must negotiate protocol
  versions; package installation alone does not establish endpoint support.
- Generated source layout, generator APIs, and compiler/runtime types may
  change without becoming public package API, provided the declared exports
  and wire compatibility rules are honored.
- Adding a public package expands release, licensing, provenance, and packaged
  artifact verification responsibilities for every LayerDraw release.
- RFC acceptance authorizes the package boundary and stable-channel release
  policy; each release must still pass the repository delivery gates.
