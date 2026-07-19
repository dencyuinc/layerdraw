---
"@layerdraw/protocol": minor
---

Bind Access authoring evaluations to a required `base_revision_digest` in the
generated Access and Runtime protocol contracts. Wire clients must regenerate
their bindings and include the digest of the canonical committed revision;
missing or mismatched revision evidence is rejected before publication.
