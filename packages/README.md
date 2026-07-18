# TypeScript Packages

Publishable and private TypeScript workspace packages for protocol clients, rendering, viewer/editor surfaces, SDKs, MCP clients, registry clients, and shared UI.

TypeScript packages do not implement LDL parsing, validation, query planning, identity, or canonical semantics.

`protocol/` is the generated, runtime-dependency-free `@layerdraw/protocol`
package. It exposes only the `common`, `semantic`, and `engine` schema-group
subpaths and includes generated structural validators plus canonical codecs for
untrusted JSON.

`engine-client/` exposes the transport-neutral Engine API, including Workbench
open, bounded read, preview, apply, and close calls. It forwards generated wire
values and BlobRef bytes to an Engine transport; it does not parse LDL, retain a
Working Document, classify AuthoringImpact, or write source.

`engine-wasm/` is the browser Worker transport for the same Engine Protocol
surface. It is validated by the shared compile and Workbench conformance corpus
and must not fork Workbench semantics for browser delivery.

`render/` owns the presentation-only, versioned `RenderRecipe` and closed
`RenderData` contracts. It consumes semantic `ViewData` values but neither
defines nor recomputes Go semantics. Its framework-neutral materialization core
owns deterministic layout from explicit resolved profile, font, asset, ordering,
seed, and resource-limit inputs. Its Diagram, Tree, Flow, Table, Matrix, Context,
and Diff visual adapters map only supplied RenderData geometry, presentation
values, and bindings to deterministic SVG plus portable headless interaction
metadata; export artifact serialization remains outside this package.
