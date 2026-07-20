# Internal Go Packages

Go capability packages and adapters for the LayerDraw Engine, Runtime, Access, Registry, application services, and host integrations.

This tree owns backend semantics. Framework shells call these packages through explicit facades and ports.

Native Project Search, Query/Analysis execution, durable Search Index metadata,
and embedding adapter composition are documented in
[`adapter/search/README.md`](adapter/search/README.md). Runtime owns revision,
Access, profile, and provider orchestration; Engine continues to own all domain
semantics.

- `access/` owns local Actor resolution, authoring decisions, bounded agent
  delegation, and trusted-boundary redaction. It consumes Engine and host
  protocol impacts but does not parse LDL or publish resources.
- `registry/` owns source, trust, dependency, and transaction decisions. It
  delegates archive and LDL validation/authoring to the Engine package facade
  and atomic publication to Runtime ports.
