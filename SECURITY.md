# Security Policy

LayerDraw is pre-release. Do not use it for production security-sensitive workloads until a supported release is published.

## Reporting

While the repository is private or GitHub private vulnerability reporting is unavailable, report suspected vulnerabilities through 株式会社DENCYU's official contact channel:

https://dencyu.co.jp/

Do not open a public issue for a vulnerability that includes exploit details, credentials, private deployment information, or unpublished customer impact.

## Scope

Security reports may involve:

- LDL parsing, validation, package installation, or import handling
- `.layerdraw` and `.ldpack` container processing
- Registry download, signature, dependency, or rollback behavior
- MCP tools and agent-facing operations
- auth, authorization, sharing, realtime collaboration, and storage adapters
- export rendering and asset handling

Out of scope until a supported release exists:

- denial of service against local development builds
- issues that require modifying the local source tree after checkout
- reports against unreleased prototype code that is not part of the active implementation

## Public Repository Gate

Before the repository is made public, the project must verify:

- license and notice files are included in source archives and review bundles
- secret scanning and push protection are enabled where available
- branch protection, rulesets, and required checks are configured
- security contact and vulnerability disclosure flow are finalized
- private vulnerability reporting is enabled

## Supported Release Gate

Before a supported release is shipped, release artifacts must include SBOM, provenance, license files, third-party notices, signatures, and documented security support scope.
