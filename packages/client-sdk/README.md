# @layerdraw/client-sdk

Public SDK facade contracts for LayerDraw Viewer and Browser Editor variants.

`./editor` defines the injected Browser Editor boundary. It distinguishes ephemeral Engine application, host-callback receipts, durable Runtime commits, and Runtime results that did not commit. Only the durable success variant can contain `committed_revision`.

Capabilities are selected from the supplied manifest. Required capabilities fail fast and optional capabilities produce typed unavailable results; package names and method presence are never treated as capability evidence.

The Browser Editor factory implementation is delivered separately. These contracts contain no React, Wails, native-dialog, Registry, MCP, server, or realtime dependency.

Operational failures use the closed `BrowserEditorError` code set so hosts can present cancellation, access denial, stale revisions, conflicts, capability absence, and transport failure without inspecting arbitrary thrown values.
