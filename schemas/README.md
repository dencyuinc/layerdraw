# Schemas

Language-neutral protocol, container, release, and wire schema sources.

Schemas are the source of truth for generated Go and TypeScript bindings.
Handwritten Engine, Runtime, or UI semantics do not live here. The initial
protocol authority is JSON Schema draft 2020-12 and is split into:

- `protocol-common/`: envelopes, failures, blob references, pagination, and
  capability handshake primitives.
- `semantic/`: Actor-independent diagnostics, StableAddress-indexed compiler
  projections, hashes, source maps, semantic indexes, and search documents.
- `engine-protocol/`: `engine.handshake` and `engine.compile` operation payloads
  and concrete envelopes.
- `fixtures/`: canonical cross-language request and response vectors.

Canonical fields and enum values use `lower_snake_case`. Quantities that map to
64-bit integers use canonical decimal strings. `CanonicalInt64` covers
`[-2^63, 2^63-1]`; `CanonicalUint64` covers `[0, 2^64-1]`; leading zeroes,
`+`, and `-0` are invalid. JSON integer fields are limited to the portable
JavaScript safe-integer range. `Digest` is exactly `sha256:` followed by 64
lowercase hexadecimal characters.

## Wire and version rules

JSON is UTF-8. Objects distinguish an absent optional property from a present
empty collection; `null` is accepted only where a schema branch says `null`.
Objects and enums are closed unless their schema explicitly says otherwise.
Unknown object properties are rejected rather than dropped. Minor-version data
is carried losslessly only by an explicit `extensions` map, whose recursive
values permit null, booleans, strings, arrays, and objects but deliberately no
JSON numbers. Capability identifiers are strings and an unknown identifier is
never treated as enabled behavior.

Protocol versions use `major.minor`. Removing or requiring a field, changing a
closed enum, or changing canonicalization requires a new major version.
Optional fields and documented extension values may be added in a minor
version. Schema patch versions are not protocol versions.

Generated encoders produce LayerDraw canonical JSON: no insignificant
whitespace; object members sorted by UTF-16 code units; array order preserved;
strings are not Unicode-normalized and use JSON escaping with U+2028/U+2029
escaped; numeric tokens are finite base-10 safe integers with no exponent,
fraction, `+`, leading zero, or negative zero. Optional absent properties are
omitted; present empty collections remain present. TypeScript codecs return the
canonical JSON string and Go codecs return its UTF-8 bytes.

## Schema digests

Every schema file uses LF bytes and a repository-relative slash-separated
path. A group digest covers the transitive `$ref` import closure, sorted by
path, by hashing each `path`, one NUL byte, the exact file bytes, and one NUL
byte with SHA-256. The aggregate digest applies the same recipe to all three
schema files. `gen/schema-digests.json` records raw file, group-closure, and
aggregate digests; generated Go and TypeScript metadata embeds the matching
group digest. Engine imports common and semantic, semantic imports common, and
common imports neither, so the graph is acyclic.

## Portable compile boundary

`CompileInput` retains ordered project/installed-Pack path-to-`BlobRef`
bindings, resolved dependency manifests, referenced assets, and limits. Arrays
are intentional: duplicate paths and blob identities survive decoding for the
dispatcher in Issue #29 to reject. Raw bytes are always out of band.

`CompileResult` retains a validated tagged normalized Project-or-Pack artifact,
canonical/artifact blob bindings, source map, semantic index, addresses,
definition/graph/subject/subtree/child-set hashes, compiled Query/View/Export
recipe blob bindings, authoring classifications, search documents, and
effective limits. It deliberately excludes the lossless CST and tokens,
`TypedAST`, every compiler stage result/interface, mutable snapshots, Go
errors, stack traces, and LDL behavior. Mapping compiler-domain values into
these generated targets belongs exclusively to Issue #29.

Run `make generate` to update both generated languages from these schemas and
`make generate-check` to reject stale artifacts.
