# @layerdraw/composer

Framework-neutral contracts for converting UI intent into existing Engine protocol edit requests and retaining Engine/Access results for presentation.

The package does not parse LDL, rewrite source, classify authoring impact, evaluate policy, persist documents, or depend on React. `EditorEdit` contains an already typed protocol request; `toEditorOperationRequest` validates and forwards that request without changing its semantics.

The state-machine implementation is delivered separately. This package establishes its public intent, request, and presentation boundary.
