# RFC 0001: Public protocol package and generation boundary

- Status: Proposed
- Authors: LayerDraw maintainers
- Approval: Pending the approvals required by `OWNERS.yaml` and repository policy
- Tracking issue: #27
- Target release: a future release after the approval and release-package gates below

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

This RFC proposes that LayerDraw publish `@layerdraw/protocol` as the transport-neutral
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
independent temporary clone for each pass, compares NUL-delimited manifests of
every non-Git filesystem entry (including ignored paths, types, modes,
contents, and symlink targets), permits changes only to the declared
generated-output set, and requires two clean passes. After the initial
workspace dependency materialization, the gate copies the active Corepack
runtime and imports its verified `pnpm@11.12.0` payload without network access
into a preserved read-only bundle used by both passes; it never selects a
global pnpm executable. Each clone receives a frozen offline dependency copy
from the existing read-only package store and distinct HOME, XDG, Go, Turbo,
and temporary caches. Dependency and package-manager copies are manifested so
mutation attempts fail without exposing caller dependency state.
Package verification builds and packs the actual npm artifact, checks its
export boundary in Node and browser conditions, and rejects source maps whose
source is neither packaged nor embedded. Issue #27 does not add a release or
publish workflow. Before stable publication, later release/package work,
especially Issue #33, must add and test jobs that publish those exact verified
artifact bytes rather than regenerating them.

Protocol compatibility and npm package compatibility are related but distinct
version axes. Wire documents follow the `major.minor` rules in
`schemas/README.md`: breaking shape or canonicalization changes require a new
protocol major, while additive documented changes may use a protocol minor.
The npm package follows SemVer through Changesets. The intended policy is for
it to join the repository's fixed LayerDraw release set only after later
release/package work, especially Issue #33, implements and verifies lockstep
versioning and a bound release manifest. The current Changesets configuration
has no fixed group and does not enforce that intent. A package release must not
imply that an endpoint supports a protocol version; negotiation remains
explicit in the wire contract.

## Approval requirements

This RFC remains Proposed. The canonical `OWNERS.yaml` currently names no
approvers for either the `engine` paths that own schemas/generated bindings or
the `web-and-sdk` paths that own the public package, and no qualifying
two-person approval record exists. Acceptance requires at least two actual
approvals, including an applicable component approver, recorded only after the
ownership mapping can satisfy repository policy.

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
- If the required approvals are later recorded, RFC acceptance will authorize
  the package boundary decision but not stable publication by itself. Stable
  publication remains gated on the release/package implementation (especially
  Issue #33) and all repository delivery gates.
