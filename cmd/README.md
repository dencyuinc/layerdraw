# Commands

Go composition roots for LayerDraw executables such as `layerdraw-engine`, `layerdraw-host`, `layerdraw-server`, and `layerdraw-registry`.

Command packages wire capabilities and transports. They do not define LDL semantics.

`layerdraw-host stdio --root ABSOLUTE_PATH` exposes the local Runtime lifecycle
and the existing Engine facade over bounded LDSP framing. JSON control remains
separate from request and response blob bytes. `layerdraw-host engine-stdio`
provides the Engine-only endpoint used by the composed TypeScript client. The
host requires either a linked release-manifest digest or the matching release
manifest beside the executable; it does not discover or download binaries.
