# @layerdraw/composer

Framework-neutral contracts for converting UI intent into existing Engine protocol edit requests and retaining Engine/Access results for presentation.

The package does not parse LDL, rewrite source, classify authoring impact, evaluate policy, persist documents, or depend on React. `EditorEdit` contains an already typed protocol request; `toEditorOperationRequest` validates and forwards that request without changing its semantics.

`Composer` provides the framework-neutral session state machine. It sequences and cancels previews, suppresses stale responses, preserves authoritative preview/access evidence through apply, distinguishes ephemeral, host-defined, durable, and non-committed outcomes, and exposes typed recoverable failures. Semantic undo and redo replay host-validated intents; they never create Runtime revisions locally.

Typed builders cover entity, relation, row, query, view, source-fragment, and source-patch requests. All builders validate generated protocol shapes and leave semantic validation, source rewriting, impact classification, policy evaluation, and persistence to Engine, Access, and Runtime.
