# `@layerdraw/render`

Presentation-only, renderer-neutral contracts and deterministic materialization for converting generated semantic `ViewData` into versioned `RenderData`.

This package owns closed `RenderRecipe` and `RenderData` TypeScript types, validation, traceability checks, deterministic render-input hashing, and the pure framework-neutral `materializeRenderData` layout core. Materialization requires an explicit resolved renderer profile, canonical ordering and tie-breaking policy, font metrics, asset dimensions, locale, timezone, seed, and hard resource limits. It does not use DOM measurement, host font fallback, ambient time, or random state, and never generates semantic `ViewData`, `ExportPlan`, or artifact bytes.

`materializeRenderData` returns a discriminated result. Successful results carry shape-specific geometry plus a source binding for every primitive; failures carry stable typed diagnostics for malformed input, profile incompatibility, missing fonts/assets, invalid geometry, and resource limits.

Diagram labels use a closed anchor discriminant and may target an occurrence or edge path. Diagram overlays and badges use the closed occurrence-or-container decoration target union. Support geometry carries its support kind and display identity without a decoration target.
