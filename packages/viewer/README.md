# @layerdraw/viewer

Framework-neutral, readonly Viewer state over `@layerdraw/render`. The package accepts only validated semantic `ViewData` snapshots and strictly ordered full replacements. It never parses LDL, calls Engine or Runtime, mutates source data, or depends on React, DOM, Node-only APIs, ambient fonts, or ambient assets.

Hosts inject a capability manifest, one resolved renderer profile, a closed render recipe for each supported shape, font and asset resolvers, deterministic layout policy, render limits, and Viewer resource limits. Publications are defensive copies; presentation state is kept separately and exact source inspection returns only the validated ViewData item and source binding supplied by Render materialization.
