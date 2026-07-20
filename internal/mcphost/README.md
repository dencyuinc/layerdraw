# Canonical MCP Host adapter

`internal/mcphost` is the in-process MCP adapter embedded by Desktop. It maps
the normative `layerdraw.*` tools and resources to typed Engine, Runtime,
Access, Registry, Review and exporter owner ports. It is not a semantic owner
and is not a separately launched Desktop executable.

The owner capability snapshot is authoritative. A tool is advertised only
when its exact owner operation is enabled; linked packages and method presence
are never capability evidence. Features whose owners are not wired (including
Review, native interchange, or external storage while their adapters are
absent) stay unadvertised.

The adapter enforces MCP transport concerns before dispatch:

- closed request, capability, cursor, stale-binding, resource, cancellation,
  transport and owner failures without provider text or panic values;
- bounded input/output bytes, item counts and JSON depth;
- opaque one-shot continuation cursors bound to tool, normalized request
  bytes, document, revision, Access fingerprint and expiry;
- cancellation and shutdown propagation, in-flight draining, and fresh
  generation state after restart;
- defensive copies of schemas, arguments, results and owner continuations.

Owner adapters receive `OwnerRequest` or `ResourceRequest`. They must pass
generated owner-protocol values and remain responsible for schema validation,
revision checks, Access re-evaluation, Review approval, and atomic Runtime
publication. They must not accept raw LDL parsing, raw database queries,
provider credentials or self-asserted authorization through this boundary.

Production Desktop construction uses `desktopapp.NewCanonical` with an
in-process `*mcphost.Host`. `desktopapp.BindCanonicalMCPHost` is the lifecycle
adapter used by the composition root. The lower-level `HostPorts.MCP` seam is
retained for closed lifecycle and framework tests, not production wiring.
