# Tools

Repository-local build, code generation, policy, release, and verification tools.

Tooling should provide stable operator entrypoints and avoid embedding product semantics outside the Engine and schema pipeline.

- `license-policy.json`: source path and third-party dependency license policy
- `licensecheck`: source and JSON Schema `$comment` SPDX verification,
  package/module verification, unified dependency inventory, and Go artifact
  legal bundle/SBOM generation
- `coverage-policy.json`: repository and package coverage thresholds
- `coveragecheck`: Go overall, package, and changed-statement coverage enforcement
- `protocolgen`: standard-library JSON Schema validation and deterministic Go
  plus TypeScript protocol generation; it is the only writer for generated
  wire source and schema digest manifests
