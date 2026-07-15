# Changesets

Release intent and package change records.

Add a Changeset for each user-visible package or app change, naming the
affected public artifact, SemVer impact, user-visible summary, and any migration
or deprecation note required by repository policy. Use
`corepack pnpm exec changeset status` to inspect pending intent; for example,
`public-protocol-contract.md` records the proposed first
`@layerdraw/protocol` release.

The current Changesets configuration has no fixed group, and the repository has
no release/publish workflow. The normative fixed-release-set policy is the
target for later release/package work, especially Issue #33; that work must
implement and verify lockstep versioning, the release manifest, and publishing
of exact verified artifacts. Until those gates land, a Changeset records intent
only and does not claim release automation or authorize stable publication.
