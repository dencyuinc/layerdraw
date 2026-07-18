# `@layerdraw/render`

Presentation-only, renderer-neutral contracts, deterministic materialization, and framework-neutral visual surfaces for generated semantic `ViewData` and versioned `RenderData`.

This package owns closed `RenderRecipe` and `RenderData` TypeScript types, validation, traceability checks, deterministic render-input hashing, and the pure `materializeRenderData` layout core. Materialization requires an explicit resolved renderer profile, canonical ordering and tie-breaking policy, font metrics, asset dimensions, locale, timezone, seed, and hard resource limits. It does not use DOM measurement, host font fallback, ambient time, or random state, and never generates semantic `ViewData`, `ExportPlan`, or serialized export artifacts.

`materializeRenderData` returns a discriminated result. Successful results carry shape-specific geometry plus a source binding for every primitive; failures carry stable typed diagnostics for malformed input, profile incompatibility, missing fonts/assets, invalid geometry, and resource limits.

## Graph visual surfaces

Diagram, Tree, and Flow adapters are available from the root and the `@layerdraw/render/diagram`, `@layerdraw/render/tree`, and `@layerdraw/render/flow` entrypoints:

```ts
import {
  renderDiagramVisualSurface,
  type DiagramRenderData,
} from "@layerdraw/render/diagram";

const surface = renderDiagramVisualSurface(renderData, {
  viewport: { x: 0, y: 0, width: 1280, height: 720 },
  label_overflow: { mode: "ellipsis", max_scalars: 128 },
});

surface.svg;
surface.interaction.hit_targets;
surface.interaction.focus_order;
surface.interaction.source_bindings;
```

The adapters are pure and use the supplied RenderData coordinates, bounds, routes, ordering, connector kinds, labels, and source bindings without performing layout or semantic projection. The returned SVG uses the same implementation in browser and Node runtimes, clips to the selected viewport, applies deterministic scalar-based label overflow, has a defined empty state, and uses zoom-independent strokes. Every visual primitive receives a stable hit-target ID, accessibility label, deterministic zero-based focus order, and its exact reversible RenderData source binding; inherited RenderData diagnostics are returned unchanged.

`DEFAULT_VISUAL_SURFACE_LIMITS` provides conservative per-call limits and `VISUAL_SURFACE_HARD_LIMITS` is the package ceiling. Callers may lower all five limits with a complete `limits` object, but cannot raise them above the package ceiling. `max_text_scalars` is a deterministic preassembly budget: it counts each full root accessibility label and, for every primitive, its render key, primitive kind, accessibility label, visible label, and structural string or joined key-list data attribute once before label truncation. `max_svg_bytes` is enforced by a UTF-8 byte-counted incremental writer before each markup chunk is retained, so accumulated output never exceeds the configured bound. `max_extent` checks the overall bounds and every primitive bounds endpoint, route point, and standalone position. Malformed data, malformed closed options, and exceeded limits throw `VisualSurfaceError` with a stable `render.visual_*` code and never return partial output.

Diagram labels use a closed anchor discriminant and may target an occurrence or edge path. Diagram overlays and badges use the closed occurrence-or-container decoration target union. Support geometry carries its support kind and display identity without a decoration target. Tree duplicate and cycle references and Flow connector/cycle kinds receive visibly distinct SVG treatments while preserving the supplied reference routes.

These SVG surfaces are visual adapters, not the Issue #87 export serializer: they do not create `ExportArtifact`, Source Manifest, PNG bytes, business documents, or persistence state.
