# `@layerdraw/registry-client`

Typed, transport-neutral access to Registry operations owned by a trusted Go
host. The package never resolves dependencies, verifies signatures, opens
archives, classifies authoring impact, or applies package trees.

Use `@layerdraw/registry-client/host` with Desktop/Wails generated bindings.
Credential bytes are not part of this API; source configuration only carries
an opaque `auth_connection_ref` resolved by the host credential broker.
