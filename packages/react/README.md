# @layerdraw/react

Accessible, composable React bindings for an existing `BrowserEditor`.

The host constructs and owns the editor, transport, persistence, Access, and approval dependencies. `EditorProvider` only subscribes to that instance; it never constructs, closes, or replaces host dependencies.

```ts
import { EditorProvider, EditorCommandButton, EditorLiveRegion } from "@layerdraw/react";

<EditorProvider editor={editor} session={session}>
  <EditorCommandButton action="apply" capabilityId="runtime.commit_operations">
    Apply
  </EditorCommandButton>
  <EditorLiveRegion />
</EditorProvider>
```

Hooks expose the current session, Composer snapshot, preview, diagnostics, Engine-owned AuthoringImpact, trusted decision and grant, conflicts, and handshake capability state. The UI does not infer operation-to-capability mappings or decide policy: hosts pass the applicable protocol capability ID to controls.

Command controls expose `unavailable`, `denied`, `pending`, `ephemeral`, and `durable` states through accessible descriptions and `data-action-state`. Toolbar arrow/Home/End navigation, async focus restoration, polite live announcements, responsive container layouts, and reduced-motion rules are included.

The responsive verification profiles are desktop `1440x900` and mobile `390x844`. Layout primitives remain usable without product-specific Wails chrome or native file workflows.

Structured navigation is available through `DocumentOutline` and `SemanticInspector`. Feed the outline the identity and source fields returned by Engine `SymbolReadItem` values and, for renames, Engine semantic-diff before/after address mappings. Deleted selections retain only their last Engine source range. Inspector fields use a host-controlled draft and a host-supplied `buildEdit(draft)` callback that returns the complete Composer `EditorEdit`; React never parses LDL, invents identities, or rewrites source text.

`maxVisibleItems` bounds mounted outline rows. Search uses only structured Engine result fields, while listbox keyboard navigation supports Arrow keys, Home, End, and Enter for source handoff. Availability states (`read-only`, `denied`, `unavailable`, and `partial`) are exposed visually and programmatically.
