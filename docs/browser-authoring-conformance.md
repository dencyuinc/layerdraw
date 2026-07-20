# Browser authoring conformance

Browser authoring closure is defined by the executable matrix at
`tests/conformance/testdata/browser_authoring_matrix_v1.json`. The matrix binds
each required transport, failure class, package boundary, and composed browser
state to the test that executes it. `make conformance` fails when a named test
or package-boundary authority disappears.

The local Engine and injected Runtime paths use the same framework-neutral
`BrowserEditor` request shape and normalized preview assertions where their
capabilities overlap. The local path additionally opens, previews, and applies
through the built Go/WASM artifact rather than a transport stub. Their
persistence claims intentionally differ: local
Engine apply is `ephemeral` unless a host callback explicitly persists it, and
only a successful Runtime commit may return `durable` with an authoritative
Committed Revision. Rejected and needs-review Runtime outcomes never fabricate
a revision.

The composed React browser fixture exercises primary authoring, diagnostics,
approval-unavailable, denial, stale/conflict recovery, reconnect, empty/loading/
error/partial Viewer states, dense data, and both 2D and 3D publications at
`1440x900` and `390x844`. It uses accessible roles, keyboard traversal, focus
restoration, reduced-motion rules, and bounded mounted rows.

The deterministic large-document fixture contains 10,000 Engine-owned symbols
while mounting at most 40 outline options. Chromium CI enforces these budgets:

- bounded keyboard navigation and source handoff: 1,500 ms
- structured search to the final symbol: 1,500 ms
- host-controlled preview dispatch: 1,000 ms

These are interaction budgets, not Engine compile budgets. They deliberately
exclude network services and product shells. Wails/Desktop packaging, Registry,
Library, MCP, VS Code, and hosted infrastructure remain outside this suite.
