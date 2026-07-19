# @layerdraw/client-sdk

Public SDK facade contracts for LayerDraw Viewer and Browser Editor variants.

`createBrowserEditor` is the framework-neutral Browser Editor facade. It
composes the public Composer state machine with an injected Engine client and,
optionally, an injected Runtime client. The same `open`, `preview`, `apply`,
`undo`, `redo`, `materializeView`, and `close` API is used in both modes.

```ts
import { createBrowserEditor } from "@layerdraw/client-sdk/browser-editor";

const editor = createBrowserEditor({ engine_client, asset_resolver });
const opened = await editor.open({
  authority: "engine",
  input: { compile_input, requested_limits },
});

const preview = await editor.preview(edit);
if (preview.preview.status === "valid") await editor.apply(edit);
await editor.close();
```

Local Engine application is explicitly ephemeral unless a
`DocumentProvider` supplies a preconditioned host write. Runtime commits are
durable and carry the authoritative committed revision; rejected or
needs-review results cannot claim one.

Capabilities are selected from the actual Engine or Runtime handshake at
`open`. Required capabilities fail fast and optional capabilities produce
typed unavailable results; an injected host manifest cannot fabricate
transport support.

The facade and its injected provider, resolver, access, approval, and Runtime
boundaries contain no React, Wails, native-dialog, Registry, MCP, server, or
realtime dependency. In-flight calls receive `AbortSignal`s, and `close` joins
all owned cleanup.

Operational failures use the closed `BrowserEditorError` code set so hosts can present cancellation, access denial, stale revisions, conflicts, capability absence, and transport failure without inspecting arbitrary thrown values.
