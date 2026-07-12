# Contributing to LayerDraw

LayerDraw is planned as a public source-available project. The product source is visible and contributions are welcome, but the project is not licensed as OSI-approved Open Source.

## Contribution Scope

Good public contributions include:

- bug reports and reproducible cases
- documentation fixes
- LDL examples and conformance fixtures
- protocol and boundary review
- `.ldpack` packs and `.layerdraw` templates
- SDK, MCP, exporter, and integration improvements that follow the documented package boundaries

SaaS production configuration, credentials, private deployment policy, customer data, and private registry operations do not belong in this repository.

## Contributor License Agreement

Before external code contributions are merged, contributors must agree to the LayerDraw CLA described in [docs/legal/contributor-license-agreement.md](docs/legal/contributor-license-agreement.md). The CLA is intended to preserve the ability to offer Commercial / OEM licensing and future relicensing while contributors retain their own copyright.

The project may still accept issues, discussions, documentation suggestions, packs, templates, and design feedback before automated CLA tooling is enabled.

## Development Rules

- Keep LDL semantics in the Go Engine.
- Do not duplicate compiler, query, identity, or validation semantics in TypeScript.
- Keep framework code as a shell around protocol and host ports.
- Generated protocol bindings must be regenerated from schemas, not edited by hand.
- Do not add secrets, customer data, private SaaS configuration, or local agent configuration.
- Do not introduce dependencies or licenses that conflict with the licensing matrix in [docs/legal/README.md](docs/legal/README.md).

## Pull Requests

Every pull request should explain:

- what behavior or document boundary changed
- which package, surface, or contract owns the change
- what tests, fixtures, or validation were run
- whether the change affects public protocol, LDL syntax, license boundaries, or release artifacts

The repository will use squash merges. Public release branches and required checks will be added before implementation releases begin.
