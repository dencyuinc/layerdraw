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

## Work Tracking

GitHub Issues are LayerDraw's only tickets and work items. Implementation and normative changes should start from a triaged Issue with explicit acceptance criteria. Maintainers own labels, Project fields, Milestones, and workflow transitions; external contributors are not expected to have permission to set them.

Link an implementation pull request with `Closes #<issue>`. The linked Issue owns type, area, priority, Size, and Milestone metadata, so do not duplicate those values on the pull request. Apply `breaking change` directly to a pull request when it changes a public compatibility contract.

Issue-free pull requests are limited to approved dependency automation, release automation, and maintainer-owned XS typo or mechanical metadata corrections that do not change public behavior, contracts, dependencies, licensing, or security posture. Questions and uncommitted ideas belong in Discussions. The normative lifecycle, Milestone, and community-label rules are defined in [Repository Governance and Delivery](docs/repository-governance-and-delivery.md#641-work-item-lifecycle).

## Local Development

LayerDraw pins Go and the current production LTS line of Node.js in `.go-version`, `.node-version`, and `.nvmrc`, and pins pnpm through the root `packageManager` field. NVM users can run `nvm use` from the repository root. After installing the pinned Go and Node.js versions, use the repository-level task contract:

```sh
make bootstrap
make ci
```

Run `make format` before committing Go source changes. Language-specific commands remain implementation details; CI and contributor workflows use the root `make` targets.

`make security` verifies Go module integrity, runs the reachability-aware Go vulnerability scanner, and rejects high or critical npm advisories. It is part of `make ci` and requires access to the official Go vulnerability database and npm registry.

`make coverage-check` enforces repository-wide, package, and changed-statement coverage thresholds from `tools/coverage-policy.json`. New production packages inherit the default threshold unless a stricter or explicitly lower-risk component rule applies.

`make license-check` enforces source SPDX headers, package manifest licenses, and reviewed third-party dependency licenses from `tools/license-policy.json`. It writes the complete Go/npm runtime and development inventory to `reports/dependency-licenses.json`; `make license-report` is the explicit report-generation alias. A new Go module or an npm package with an unknown or unapproved license fails CI until its license and evidence are reviewed. Do not weaken the allowlist or add an override merely to make an update pass.

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
