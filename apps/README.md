# Apps

Delivery shells for product applications.

Apps contain delivery-specific composition roots. App code composes
framework-specific UI and transport around shared packages; it does not own
LDL, Runtime, Access, Registry, Export, or MCP semantics.

`desktop/` is the Desktop React shell. Its controller consumes an injected,
typed project-lifecycle port plus Browser Editor and Viewer instances. Project,
view, authoritative revision, Access, storage, persistence, and capability
state arrive in one host publication. Viewer frames are accepted only when
their project, open-session generation, selected view, and Runtime revision
match that publication. Wails bootstrap and native lifecycle implementations
remain separate dependencies.
