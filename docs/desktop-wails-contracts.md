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

## Native window, settings, and OS boundary

`internal/desktopapp.NativeShell` is the framework-neutral native shell
facade. Wails adapters supply displays, atomic settings storage, native window
application, safe OS URL opening, opaque file-association handoff, structured
logs, packaged accessibility probes, and crash recovery. Project lifecycle and
UI command routing remain injected owner ports so this facade cannot acquire
Runtime, Composer, or BrowserEditor semantics.

The production entrypoint is `internal/desktopapp.NewPlatformNativeShell`.
It wires `internal/adapter/desktop`'s private atomic JSON settings store,
fixed-executable OS URL opener, Wails runtime bridge, single-use opaque file
association broker, packaged accessibility bridge, and closed JSONL log store.
The Desktop frontend uses `@layerdraw/engine-client/wails`; neither the native
shell nor these adapters add a second Engine transport or interpret owner
responses. The #123 recovery owner and #124 command owner remain explicit
injected ports.

`apps/desktop` composes that native shell into the real Wails v2 executable.
`internal/desktopwails` supplies build-tag-selected macOS, Windows, and Linux
identities, Wails display/window/theme/zoom calls, native menu and
single-instance file-association routing, and a packaged DOM accessibility
round trip. Embedded visible controls and the native menu obtain the same
status generation and invoke the same `NativeShell` command route. Commands
whose Composer/BrowserEditor owner is not packaged remain explicitly
unavailable.

The executable's `--packaged-probe` mode exercises the concrete per-OS adapter
linkage, private settings round trip, and opaque association handoff without
launching another application. The `Desktop packaged probes` CI matrix builds
and executes that same binary on macOS, Windows, and Linux.

Packaged delivery closure is machine-readable in `deploy/desktop-conformance.json`. Every
normative Desktop matrix row is either bound to an existing executable test symbol or recorded as
the exact normative exclusion. The closure additionally fixes the installed workflow, MCP,
adversarial recovery, ownership-boundary, transport-parity, accessibility, release-evidence, and
performance suites. CI validates the manifest against `docs/blueprint.md` and executes each
time-bounded performance evidence test on all three packaged platforms before release evidence can
be built.

Persisted window bounds are schema-versioned and normalized against the live
display work areas. Invalid, oversized, or off-screen bounds recover onto a
usable primary display; theme and zoom use closed values and zoom is bounded to
50--300 percent. Menu, shortcut, and visible-control invocations use the same
typed command route. Pending, denied, and unavailable commands are never
invoked by the shell. A canonical status generation is returned with each menu
snapshot and the owner performs status re-evaluation and invocation atomically;
a stale generation cannot race a capability or Access-state change.

Restore snapshots the currently applied native state before mutation. Window,
settings, and durable-settings stages compensate to that snapshot on failure.
Settings updates likewise compensate the applied theme/zoom if atomic
persistence fails. A failed compensation always preserves recovery data when
possible and presents the closed recovery surface.

External web links are restricted to credential-free HTTPS URLs and email
handoff to query-free `mailto:` addresses. File associations and native dialogs
cross the frontend boundary only as opaque host tokens; OS paths never do. The
OS adapter must call native open APIs directly and must not invoke a command
shell. Structured shell logs have no arbitrary message/details field and cannot
contain document content, credentials, tokens, URLs, or native paths.

Unexpected frontend or backend failures are converted to a closed error
surface. A project-lifecycle recovery adapter may attach an opaque recovery
reference, but raw failure text is never presented. Packaged accessibility
probes verify labels, focus order, keyboard-only operation, reduced motion,
contrast, and zoom on the supported macOS, Windows, and Linux profiles.

## Lifecycle and failure boundary

Startup resolves a stable local actor, loads credentials and live agent
delegations, initializes components in dependency order, negotiates the
manifest, leaves MCP disabled until an explicit user action, and publishes
`ready`. Shutdown publishes
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

## MCP connection management

Desktop exposes only the supported local transport and non-secret client
instructions. Enabling, disabling, and restarting the MCP host are explicit
settings actions; packaged Desktop never silently opens the surface.
Connection, session, and delegation identifiers are host-issued random values.
The UI displays client and agent identity, effective AuthoringGrantSummary,
read/export/propose/apply scopes, constrained authoring capabilities, expiry,
generation, and closed status.

Delegation creation resolves the current open local-owner session and calls the
canonical local-document Access delegation path. A requested capability cannot
exceed the current human grant, and apply requires separate explicit
confirmation. The canonical MCP catalog owns each tool's read, export, propose,
or apply classification; UI and connection gates consume that classification
instead of duplicating operation semantics. Proposal-only connections advertise
preview/proposal operations and never direct apply tools.

Connection metadata contains no native path, credential, token, or arbitrary
diagnostic text. It is atomically stored as mode `0600`; revoked generations
remain recorded, expired connections stay expired, and a live connection is
restored only as `host_restarted` so the client must reconnect. Disable,
restart, revocation, and expiry cancel in-flight connection contexts before new
calls can enter. The local-document publication fence remains authoritative for
work already linearized at revocation.

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

Disabled surface, transport failure, connection-version mismatch, scope denial,
disconnect, and host restart have separate closed Desktop failure codes and
recovery actions. The React panel reports them through an accessible live region
and never renders provider errors, secret material, or local paths.

For generated paginated operations, the Desktop owner adapter uses a closed
operation table to decode the exact response page, enforce the aggregate item
limit, retain the owner continuation behind an opaque MCP cursor, and inject it
through the typed generated request codec on the next call. Capability
discovery advertises an explicit bounded schema matching the returned effective
snapshot and AuthoringGrantSummary.

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

## React shell synchronization

The Desktop React shell receives its Browser Editor, Viewer, and project
lifecycle through explicit injected ports. It does not discover Wails globals
or construct clients from generated method presence. One lifecycle publication
contains the selected project and view, open-session generation, authoritative
Runtime revision token, Browser Editor session, Access summary, storage origin,
persistence state, and negotiated capability statuses.

Each Viewer snapshot or ordered update carries the same project, session,
selected-view, and authoritative-revision context. The shell rejects a frame
whose context differs from the current lifecycle publication and ignores stale
lifecycle and Viewer sequences. Close or dependency replacement fences late
work and cancels Viewer materialization. Reopen of the same stable project ID
uses a new host-issued session generation so pre-close frames cannot reappear.
Recovery state exposes a handoff to the host-owned restore/discard chooser; the
shell never restores or discards a candidate implicitly.

UI actions are rendered from the effective capability status supplied by the
host. A linked package or generated binding never makes an action available by
itself. Closed localized shell failures do not expose native locations,
credentials, document content, provider errors, or panic values. Lifecycle
implementations own native dialogs, storage, recovery, leases, and revision
rules; the React shell owns only interaction, focus, accessibility, responsive
layout, and presentation of owner-produced Viewer render data.

## Native interchange

Desktop advertises `desktop.export` only when the concrete `internal/exporter`
adapter is started. The native profile catalog is closed to JSON, CSV, and TSV
schema version 1 until another profile has executable conformance evidence.
Serialization accepts generated `ExportPlan` and `ViewData`, revalidates their
revision/state/profile/hash binding, verifies exact asset and font digests, and
returns an opaque staged-artifact ID plus a complete generated Source Manifest;
artifact bytes and native destinations never cross the Wails response.

Publish uses a save dialog token and stages the primary artifact and Source
Manifest in the destination filesystem before replacing either file. Cancel,
permission, capacity, and rename failures roll back without leaving a newly
published partial pair. External import is separately advertised by its
versioned adapter profile and returns only a generated
`SemanticOperationBatch`; the caller must pass it through Engine Workbench
preview and Runtime/Access before any publication. Redacted `.layerdraw`
creation accepts only a trusted Runtime/Access projection receipt, rejects old
derived artifacts, and delegates canonical container rehashing to Engine.
