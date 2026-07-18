# `@layerdraw/render`

Presentation-only, renderer-neutral contracts for converting generated semantic `ViewData` into versioned `RenderData`.

This package owns closed `RenderRecipe` and `RenderData` TypeScript types, validation, traceability checks, and deterministic render-input hashing. It performs no layout or visual rendering and never generates semantic `ViewData` or `ExportPlan`.

Diagram labels use a closed anchor discriminant and may target an occurrence or edge path. Diagram overlays, badges, and support geometry may target an occurrence or container; no other decoration target kind is accepted.
