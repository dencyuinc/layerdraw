# TypeScript Packages

Publishable and private TypeScript workspace packages for protocol clients, rendering, viewer/editor surfaces, SDKs, MCP clients, registry clients, and shared UI.

TypeScript packages do not implement LDL parsing, validation, query planning, identity, or canonical semantics.

`protocol/` is the generated, runtime-dependency-free `@layerdraw/protocol`
package. It exposes only the `common`, `semantic`, and `engine` schema-group
subpaths and includes generated structural validators plus canonical codecs for
untrusted JSON.
