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
`+`, and `-0` are invalid. Compiler resource limits use
`CanonicalNonNegativeInt64` (`[0, 2^63-1]`) rather than unsigned values that
cannot enter the compiler domain. Every JSON integer field declares explicit
portable bounds and generated Go uses `int64`, never architecture-dependent
`int`. `Digest` is exactly `sha256:` followed by 64 lowercase hexadecimal
characters.

## Wire and version rules

JSON is UTF-8. Objects distinguish an absent optional property from a present
empty collection; `null` is accepted only where a schema branch says `null`.
Objects and enums are closed unless their schema explicitly says otherwise.
Unknown object properties are rejected rather than dropped. Minor-version data
is carried losslessly only by an explicit `extensions` map, whose recursive
values permit null, booleans, strings, arrays, and objects but deliberately no
JSON numbers. Capability identifiers are strings and an unknown identifier is
never treated as enabled behavior.

Every protocol JSON document is at most 8,388,608 UTF-8 bytes and at most 128
nested array/object containers; a scalar has depth zero. The limits apply to
envelopes, scalar codecs, and recursive `JsonValue` alike. Input must be valid
UTF-8 and every JSON string must contain only Unicode scalar values: malformed
UTF-8 and unpaired UTF-16 surrogate escapes are rejected. `Rfc3339Time` is an
exact real-calendar UTC timestamp with uppercase `Z` and, when present, one to
nine fractional-second digits; offsets and impossible dates are invalid.

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

Schema source accepts LF or CRLF and deterministically normalizes CRLF to LF;
bare carriage returns are invalid. Digests use the normalized LF bytes and a
repository-relative slash-separated path. A group digest covers the transitive
`$ref` import closure, sorted by path, by hashing each `path`, one NUL byte, the
normalized file bytes, and one NUL byte with SHA-256. The aggregate digest
applies the same recipe to all three schema files. `gen/schema-digests.json`
records raw file, group-closure, and aggregate digests; generated Go and
TypeScript metadata embeds the matching group digest. Engine imports common
and semantic, semantic imports common, and common imports neither, so the graph
is acyclic.

## Portable compile boundary

`CompileInput` retains ordered project/installed-Pack path-to-`BlobRef`
bindings, resolved dependency manifests, referenced assets, and limits. Arrays
are intentional: duplicate paths and blob identities survive decoding for the
dispatcher in Issue #29 to reject. Raw bytes are always out of band.
Request `ResourceLimits` are optional overrides and preserve absent versus
present values. For each request field, omission or canonical `"0"` selects
the Go facade default and a positive value overrides it; a successful result
never reports zero. Result `EffectiveResourceLimits` is a separate complete
record whose nine applied positive limits are always present.

`CompileResult` retains a validated tagged normalized Project-or-Pack artifact,
canonical/artifact blob bindings, source map, semantic index, addresses,
definition/graph/subject/subtree/child-set hashes, compiled Query/View/Export
recipe blob bindings, authoring classifications, search documents, and
effective limits. It deliberately excludes the lossless CST and tokens,
`TypedAST`, every compiler stage result/interface, mutable snapshots, Go
errors, stack traces, and LDL behavior. Mapping compiler-domain values into
these generated targets belongs exclusively to Issue #29.

The `NormalizedArtifact` union owns the mode-dependent result invariants.
Project results require `graph_hash` and may carry SearchDocuments. Pack
results forbid `graph_hash` and require `search_documents` to be empty.
Project requests forbid `root_pack_id`; Pack requests require it to be a
nonempty canonical selector.

Normalized Project/Pack canonical bytes, public artifact bytes, and each
Query/View/Export recipe document use distinct versioned media types declared
by their role-specific generated BlobRef types. Canonical and public artifact
bytes may be identical, but their role and media type remain distinct. Recipe
blobs are the canonical JSON representation of the corresponding Language 1
normalized Query, View, or Export recipe defined by the normative detailed
LDL specification; ordinary serialization of compiler-internal Go recipe
aliases is not a wire format.

`semantic.Diagnostic` is the Language 1 diagnostic protocol, not the generic
host diagnostic sketch: it fixes `protocol_version` 1, `LDLdddd` codes,
`error|warning|info`, stable `message_key`, typed recursive `arguments`,
optional localized `message`, origin-aware ranges, separate subject/owner
addresses, and ordered related entries. `protocolcommon.ProtocolDiagnostic`
is the separate non-LDL compatibility/policy diagnostic used by handshake.

Run `make generate` to update both generated languages from these schemas and
`make generate-check` to reject stale artifacts.
