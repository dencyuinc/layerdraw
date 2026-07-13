# Contributing to LayerDraw

LayerDraw is planned as a public source-available project. The product source is visible and contributions are welcome, but the project is not licensed as OSI-approved Open Source.

Participation is governed by the [Code of Conduct](CODE_OF_CONDUCT.md). Questions and support requests should follow [SUPPORT.md](SUPPORT.md).

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

Before an external pull request is merged, its author must agree to the [LayerDraw Contributor License Agreement 1.0](docs/legal/contributor-license-agreement.md) by selecting the Contributor Agreement checkbox in the pull request template. The pull request and its GitHub metadata record the account, CLA version, acceptance, time, commits, and covered Contribution. The author confirms the rights and any employer authorization required to submit the Contribution. A legal entity contributing in its own name must separately accept through an authorized representative using a process designated by DENCYU.

Contributor information is handled under the [LayerDraw Contributor Privacy Notice](docs/legal/contributor-privacy-notice.md). Issues, Discussions, and feedback that are not submitted for inclusion as a Contribution do not require CLA acceptance. Dependency update pull requests created by an approved bot are exempt because the bot does not claim authorship of the upstream changes.

## Development Rules

- Keep LDL semantics in the Go Engine.
- Do not duplicate compiler, query, identity, or validation semantics in TypeScript.
- Keep framework code as a shell around protocol and host ports.
- Generated protocol bindings must be regenerated from schemas, not edited by hand.
- Do not add secrets, customer data, private SaaS configuration, or local agent configuration.
- Do not introduce dependencies or licenses that conflict with the licensing matrix in [docs/legal/README.md](docs/legal/README.md).

## Pull Requests

External contributors should fork the repository, create a focused branch in that fork, and open a pull request against `dencyuinc/layerdraw:main`. Maintainers may use branches in the upstream repository, but changes still enter `main` through pull requests.

Branch names describe the change, not the contributor or tool that created it. Use one of these forms:

- `feat/<description>`
- `fix/<description>`
- `docs/<description>`
- `refactor/<description>`
- `test/<description>`
- `build/<description>`
- `ci/<description>`
- `chore/<description>`
- `perf/<description>`
- `security/<description>`
- `revert/<description>`
- `release/<description>`

Descriptions must use lowercase kebab case, for example `chore/repository-guardrails`. Personal names and agent names such as `user/`, `agent/`, `codex/`, or `claude/` are not branch categories. Dependabot's generated `dependabot/` branches are allowed. The required repository-policy check validates this rule for upstream and fork-based pull requests.

Every pull request should explain:

- what behavior or document boundary changed
- which package, surface, or contract owns the change
- what tests, fixtures, or validation were run
- whether the change affects public protocol, LDL syntax, license boundaries, or release artifacts

Pull requests are merged by squash. Direct pushes, merge commits, and rebase merges are not part of the normal contribution workflow. Required checks and protected-branch rules apply to maintainers and external contributors alike.
