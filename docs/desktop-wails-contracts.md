# Desktop Wails transport and composition contracts

This document freezes the boundary consumed by the Desktop implementation. The
machine-checked source is `internal/desktopcontract`; generated Wails bindings
must remain a mechanical transport for existing generated protocol envelopes.

## Composition closure

The Go backend links Engine, Runtime, Access, native Ladybug Query/Search/
Analysis, Search Index, a configured Embedding Provider, Registry client,
Review, native exporters, MCP Host, local and external storage adapters, and the
Wails binding shell in-process. It does not start or require `layerdraw-host`.

The embedded React assets are exactly `@layerdraw/protocol`,
`@layerdraw/engine-client/wails`, `@layerdraw/registry-client/host`, Composer,
Render, Export, Viewer, Review, React, and Library. A package being linked does
not enable its capability: startup negotiates the typed Desktop manifest.
Authoring, Query, Search, Analysis, Registry, Review, Export, MCP tools, MCP
resources, and agent-scope management are required. External storage is
optional and must report a typed unavailable status when it is not configured.

## Binding and ownership

Generated methods select one exact Engine or Runtime client method and forward
the existing operation, control envelope, and protocol blobs unchanged only
after the matching generated request decoder accepts the envelope. Before a
result can cross Wails, its outer operation and request ID must match the
request and the matching generated or owner response decoder must accept its
operation-specific result envelope. Registry, Review,
and Host owner methods may be exposed only with the same exact generated-decoder
registration; no prefix or opaque generic dispatch exists.
They do not infer capability from method presence, interpret LDL, rewrite
source, classify authoring impact, make Access decisions, resolve Registry
semantics, plan exports, or implement MCP semantics. Browser and Desktop retain
the same success, rejected/failed, and cancelled outcome; the compatibility
fixture is `schemas/fixtures/desktop/wails-binding-compatibility-v1.json`.
The exact binding/capability closure is
`schemas/fixtures/desktop/owner-binding-parity-v1.json`; conformance derives all
Engine and Runtime request/success fixtures from the normative schemas and
compares canonical results through distinct Browser-worker and Desktop-Wails
adapter shapes.

Wails owns only window lifecycle, native-dialog invocation, generated binding,
and frontend asset embedding. Native dialogs return opaque host-issued tokens,
not native paths. The storage adapter resolves those tokens inside the trusted
backend.

## Lifecycle and failure boundary

Startup resolves a stable local actor, loads credentials and live agent
delegations, initializes components in dependency order, negotiates the
manifest, starts MCP transport, and publishes `ready`. Shutdown publishes
`draining`, rejects new work, joins in-flight work, stops MCP, releases adapters
and locks, and publishes `stopped`. Corrupt or incompatible state publishes
`recovery`; it is never silently reset.

Startup, shutdown, credential, local-actor, agent-delegation, MCP transport,
native-dialog cancellation, backend panic, reconnect, adapter-unavailable, and
protocol-incompatible outcomes use the closed typed failure codes in
`internal/desktopcontract`. Component and recovery values are also closed
enums. The common outcome and capability handshake/status values are reused
directly from generated protocol bindings, including `rejected`; Desktop does
not define parallel wire vocabularies. Failure values have no arbitrary details
surface and never include credentials, document content, native paths, provider
error text, or panic values.

The executable-neutral composition root is `internal/desktopapp`. It constructs
the completed local Engine, Runtime, Access, and storage host in-process, starts
registered capability adapters in dependency order, validates the effective
generated handshake, starts MCP, and only then publishes `ready`. Its project
storage port accepts opaque native-dialog tokens; trusted absolute locations
remain backend-only. Shutdown changes admission to `draining` before joining
requests and releases adapters in reverse order. A cancelled shutdown remains
draining and can be resumed without releasing resources still in use. MCP and
the remaining registered adapters stop before the local Runtime releases its
sessions and storage locks.

Production construction uses `desktopapp.NewCanonical`, which creates the
concrete generated-client owner adapter, local transport, and one in-process
`internal/mcphost.Host`; neither a prebuilt host nor a raw MCP lifecycle port is
accepted by that constructor. The local transport is also the backing endpoint
for the Wails MCP list/call/read bindings. `internal/mcphost` contains the single
normative tool mapping table. Advertisement is derived only from the current owner capability
snapshot, so an unconfigured Review, native interchange, external-storage, or
other owner operation is absent rather than inferred from a linked package.
Desktop does not launch a sibling MCP process.

The MCP adapter owns only transport concerns. Discovery and execution are
fenced to one host generation, including synchronous discovery during transport
startup; partial startup is shut down and rolled back before another generation
may start. Its opaque continuations are
single-use and bound to tool/request bytes, document, committed revision,
Access fingerprint, expiry, and host generation. Complete request and response
envelopes, capability aggregates, continuations, strings, item counts, and JSON
depth are bounded before publication. Disconnect,
cancellation, shutdown, malformed or replayed cursors, stale bindings, owner
panic, and oversized values produce closed failures without paths, source,
credentials, or provider text. The typed owner adapter still performs generated
request validation and the final Runtime/Access/Review revalidation; MCP tool
visibility is never an authorization boundary.

Startup resolves the configured credential references and live delegation
fences through the typed `HostPorts`; credential bytes are discarded before
adapter startup. Injected-port panics are contained as
`desktop.backend_panic`. The binding facade preserves the generated owner
response outcome, including `rejected`, `failed`, and `cancelled`; a separate
closed Desktop failure is present only when no trustworthy owner response was
produced.

## Local authority

Desktop resolves a stable OS-backed local actor and creates a local-owner grant
whose default authoring profile is `full_authoring`. Every preview, apply,
autosave, asset, Registry, external reconcile, export, and MCP path still passes
through Runtime and Access. Agent access is an explicit document/local-scope
delegation with capability, action, expiry, and generation fences. Revocation
and expiry are re-evaluated at publication.

Desktop does not invent an Organization, Workspace, membership, sharing, or
realtime model. A connected server remains authoritative for those concepts.
