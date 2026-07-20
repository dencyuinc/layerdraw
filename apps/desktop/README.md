# LayerDraw Desktop shell

This private application package composes host-injected Desktop lifecycle,
Browser Editor, and Viewer contracts. It does not open native paths, interpret
LDL, construct revisions, infer capabilities, or own Runtime/Access/storage
semantics. The Wails bootstrap supplies the lifecycle port and exact clients;
the frontend renders only authoritative snapshots and closed failures.

The MCP panel uses generated `FrontendBridge` connection-management methods. It
keeps the local surface off by default, displays only non-secret instructions
and host-issued connection metadata, requires explicit apply confirmation, and
renders proposal-only, revoked, expired, and restarted states without
implementing Access or Review decisions in TypeScript.
