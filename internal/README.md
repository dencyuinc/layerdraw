# Internal Go Packages

Go capability packages and adapters for the LayerDraw Engine, Runtime, Access, Registry, application services, and host integrations.

This tree owns backend semantics. Framework shells call these packages through explicit facades and ports.

`internal/registry` owns source/trust/dependency/transaction decisions and delegates archive and LDL validation/authoring to the Engine package facade and atomic publication to Runtime ports.
