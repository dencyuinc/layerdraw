---
"@layerdraw/client-sdk": minor
"@layerdraw/engine-client": minor
---

Add the framework-neutral Browser Editor facade with Composer-backed preview,
apply, undo, redo, materialization, cancellation, capability validation, and
complete injected-resource cleanup. Local Engine edits remain explicitly
ephemeral unless a host provider persists them, while Runtime commits preserve
authoritative durable revisions.

Expose the Engine semantic preview operation through the portable client so
browser and desktop hosts use the same generated request and normalized result
contracts.
