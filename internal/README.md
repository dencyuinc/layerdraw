# Internal Go Packages

Go capability packages and adapters for the LayerDraw Engine, Runtime, Access, Registry, application services, and host integrations.

This tree owns backend semantics. Framework shells call these packages through explicit facades and ports.

- `access/` owns local Actor resolution, authoring decisions, bounded agent
  delegation, and trusted-boundary redaction. It consumes Engine and host
  protocol impacts but does not parse LDL or publish resources.
