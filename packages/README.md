# TypeScript Packages

Publishable and private TypeScript workspace packages for protocol clients, rendering, viewer/editor surfaces, SDKs, MCP clients, registry clients, and shared UI.

TypeScript packages do not implement LDL parsing, validation, query planning, identity, or canonical semantics.

`registry-client/` is the transport-only Registry facade, including the local
host/Wails entrypoint. `library/` is its framework-neutral Library UI model for
browse, verified plan presentation, confirmation, repair state, and artifact
authoring; neither package resolves or verifies Registry content itself.

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

`export/` owns the versioned, framework-neutral browser and Node serialization
boundary for Go-planned SVG, PNG, JSON, and CSV plain View exports. It consumes
only an existing `ExportPlan`, matching `ViewData` and optional `RenderData`,
and exact injected resource/profile inputs; it neither plans exports nor
resolves ambient resources, storage, registries, network state, or clocks.

`viewer/` owns the framework-neutral readonly and ordered-streaming Viewer
state machine over `render/`. It validates closed ViewData envelopes, rejects
gaps and identity mismatches without adoption, keeps immutable interaction
state separate from semantic data, exposes exact source inspection, and bounds
resolver work, queued replacements, retained publications, presentation refs,
and host event delivery. It has no React, DOM, Node-only, Engine, Runtime, LDL,
container, persistence, collaboration, or export-planning dependency.
