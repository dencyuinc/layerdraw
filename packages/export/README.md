# @layerdraw/export

Framework-neutral, versioned serializers for Go-planned plain View exports.
The package accepts a generated `ExportPlan`, its matching complete `ViewData`,
optional matching `@layerdraw/render` `RenderData`, and exact caller-resolved
resource bytes. It never parses LDL, plans an export, resolves a registry or
resource, accesses storage or the network, or uses ambient fonts, assets, time,
locale, or timezone.

Use `@layerdraw/export` for transport-neutral types and serialization,
`@layerdraw/export/browser` for a browser rasterizer, and
`@layerdraw/export/node` for a Node rasterizer. PNG always requires an injected
versioned rasterizer implementation and profile; neither entrypoint provides an
ambient canvas, DOM, native executable, or system-font fallback.

```ts
import { serializeExport } from "@layerdraw/export";

const result = await serializeExport({
  export_plan,
  view_data,
  serializer_profile: { schema_version: 1, ref: export_plan.serializer_profile },
  assets: [],
  fonts: [],
});
```

The result is a closed success/failure union. Failures expose stable diagnostic
codes only; injected host exceptions are never returned.
