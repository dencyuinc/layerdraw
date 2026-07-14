# Schemas

Language-neutral protocol, container, release, and wire schema sources.

Schemas are the source of truth for generated Go and TypeScript bindings.
Handwritten Engine, Runtime, or UI semantics do not live here. The initial
protocol authority is the published LayerDraw Protocol Schema Dialect v1. The
dialect composes JSON Schema draft 2020-12 with a required assertion vocabulary
for invariants that stock JSON Schema cannot express, and is split into:

- `protocol-common/`: envelopes, failures, blob references, pagination, and
  capability handshake primitives.
- `semantic/`: Actor-independent diagnostics, StableAddress-indexed compiler
  projections, hashes, source maps, semantic indexes, and search documents.
- `engine-protocol/`: `engine.handshake` and `engine.compile` operation payloads
  and concrete envelopes.
- `meta/`: the dialect meta-schema and the schema for every required
  `x-layerdraw-*` assertion keyword.
- `fixtures/`: canonical cross-language request and response vectors.

## LayerDraw assertion vocabulary

Every protocol schema declares the required
`https://schemas.layerdraw.dev/vocab/protocol/v1` vocabulary. A validator that
does not implement a required vocabulary must refuse to process the schema.
The dialect also requires the draft 2020-12 Format-Assertion vocabulary. All
recognized LayerDraw formats are assertions, including canonical integer,
binary64, protocol-version, source-path, and real-calendar UTC timestamp forms;
a validator that cannot implement any recognized format must refuse the schema.
Its assertion keywords have these exact meanings:

- `x-layerdraw-tagged-union` selects one variant from the value of `property`;
  that variant's `required` and `forbidden` members must respectively be
  present and absent, while `empty` and `non_empty` members must be arrays with
  the stated cardinality.
- `x-layerdraw-outcome-envelope: true` requires success to contain `payload`
  but no `failure`, rejection to contain neither and at least one diagnostic,
  and failure/cancellation to contain `failure` but no `payload`.
- `x-layerdraw-ordered-range: true` requires canonical `start_byte` to be no
  greater than canonical `end_byte`.
- `x-layerdraw-operator-value` makes the configured value member absent for a
  listed valueless operator and present for every other operator.
- `x-layerdraw-protocol-offer: true` requires an ordered canonical inclusive
  range and a nonempty, version-unique exact digest binding list whose versions
  all fall within that range.
- `x-layerdraw-limit-capability: true` requires both `default_value` and
  `effective_maximum` to be no greater than `hard_maximum`.
- `x-layerdraw-unique-array-keys` requires each configured array's objects to
  have distinct values for its configured key.
- `x-layerdraw-disjoint-arrays` requires the two configured string-array
  properties to have no value in common.

The root annotations `x-layerdraw-max-json-bytes` and
`x-layerdraw-max-json-depth` define the shared recursive document limits;
`x-layerdraw-go-package` and `x-layerdraw-ts-module` define generated targets.

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

Handshake `required_capabilities` and `optional_capabilities` are sets: each is
internally unique and the two are disjoint. A duplicate or overlap makes the
entire `HandshakeRequest` invalid before version/capability negotiation; it is
never deduplicated, reclassified, or mapped to multiple result statuses.

Duplicate object members are invalid at every depth. Member identity is tested
after JSON escape decoding, so `"name"` and `"\u006eame"` are duplicates.
The TypeScript JSON value model is limited to own enumerable data properties:
prototype-provided, non-enumerable, accessor, or symbol-keyed properties are
not wire data.

Every protocol JSON document is at most 8,388,608 UTF-8 bytes and at most 128
nested array/object containers; a scalar has depth zero. The limits apply to
envelopes, scalar codecs, and recursive `JsonValue` alike. Input must be valid
UTF-8 and every JSON string must contain only Unicode scalar values: malformed
UTF-8 and unpaired UTF-16 surrogate escapes are rejected. `Rfc3339Time` is an
exact real-calendar UTC timestamp with uppercase `Z` and, when present, one to
nine fractional-second digits; offsets and impossible dates are invalid.
Encoder byte/depth limits apply to the emitted canonical bytes. In particular,
`<`, `>`, and `&` remain literal while U+2028/U+2029 remain escaped, so an
implementation-specific pre-encoding escape policy cannot change acceptance.
Programmatic recursive `JsonValue` inputs reject cycles and container depth 129
with validation errors; container depth 128 remains valid.

Protocol versions use `major.minor`. Removing or requiring a field, changing a
closed enum, or changing canonicalization requires a new major version.
Optional fields and documented extension values may be added in a minor
version. Schema patch versions are not protocol versions. One offer's inclusive
`lower..upper` range is confined to one major; clients use separate offers for
different majors, and every offered exact version has one associated Engine
schema-closure digest.

Generated encoders produce LayerDraw canonical JSON: no insignificant
whitespace; object members sorted by UTF-16 code units; array order preserved;
strings are not Unicode-normalized and use JSON escaping with U+2028/U+2029
escaped; numeric tokens are finite base-10 safe integers with no exponent,
fraction, `+`, leading zero, or negative zero. Optional absent properties are
omitted; present empty collections remain present. TypeScript codecs return the
canonical JSON string and Go codecs return its UTF-8 bytes.

`CanonicalFiniteDecimal` is a string representation of one finite IEEE-754
binary64 value. It is exactly the shortest round-trippable ECMAScript decimal
spelling (including canonical exponent notation when required), accepts values
such as `-0.5`, and rejects negative zero and longer aliases of the same value.

## Schema digests

Schema source accepts LF or CRLF and deterministically normalizes CRLF to LF;
bare carriage returns are invalid. Digests use the normalized LF bytes and a
repository-relative slash-separated path. A group digest covers the transitive
`$ref` import closure, sorted by path, by hashing each `path`, one NUL byte, the
normalized file bytes, and one NUL byte with SHA-256. The dialect meta-schema
participates in every group closure. The aggregate digest applies the same
recipe to the meta-schema and all three protocol schema files. `gen/schema-digests.json`
records raw file, group-closure, and aggregate digests; generated Go and
TypeScript metadata embeds the matching group digest. Engine imports common
and semantic, semantic imports common, and common imports neither, so the graph
is acyclic.
Schema and dialect source also reject duplicate object members recursively
before decoding, including members whose spellings become equal after JSON
escape decoding. No decoder's last-member-wins behavior can select digest
authority for an ambiguous schema.

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

The nine limit keys and units are closed: byte limits use `bytes`, count limits
use `items`, `max_raster_dimension` uses `pixels_per_axis`, and
`max_raster_pixels` uses `pixels`. A capability descriptor reports the endpoint
`hard_maximum`, the endpoint `default_value`, and the client-scoped
`effective_maximum = min(hard_maximum, client ceiling when present)`. Both the
default and effective maximum must be no greater than the hard maximum. A
positive compile override must be no greater than the effective maximum or the
request is rejected; a zero/omitted override applies `min(default_value,
effective_maximum)`. These are wire and combination semantics; enforcing them
when dispatching a compile remains Issue #29.

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
All three recipe blob references have the literal lifetime `request`; `session`
and `persistent` are invalid even though those values remain available to other
blob roles.

`semantic.Diagnostic` is the Language 1 diagnostic protocol, not the generic
host diagnostic sketch: it fixes `protocol_version` 1, `LDLdddd` codes,
`error|warning|info`, stable `message_key`, typed recursive `arguments`,
optional localized `message`, origin-aware ranges, separate subject/owner
addresses, and ordered related entries. `protocolcommon.ProtocolDiagnostic`
is the separate non-LDL compatibility/policy diagnostic used by handshake.

Run `make generate` to update both generated languages from these schemas and
`make generate-check` to reject stale artifacts.
