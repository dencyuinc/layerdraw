# Access component

`internal/access` is the framework-neutral trusted decision boundary for local
Desktop and future host compositions.

It owns:

- stable local Actor resolution through a host-injected `LocalActorResolver`;
- the expanded `full_authoring` capability set for a local owner;
- immutable AuthoringPolicy snapshots whose capability and graph constraints
  are intersected without allowing lower policies to expand a parent grant;
- bounded agent delegation with independent read, export, propose, and apply
  scopes, expiry, revocation, and parent-grant revalidation;
- atomic evaluation of Engine `AuthoringImpact` and versioned
  `HostOperationImpact` values, including graph facts supplied by Engine;
- evaluation and decision digests bound to both impacts, the access
  fingerprint, and request intent;
- pre-boundary subject filtering and field redaction for Search, Query,
  Review, export, and MCP results.

It does not parse LDL, classify semantic changes, infer capabilities from UI or
transport operation names, persist organization/workspace membership, or own a
publication transaction. Runtime and each other resource owner resolve a fresh
trusted grant and invoke Access immediately before publication.

`localdocument.Config.LocalActor` is the native-host injection point. Headless
and embedded callers retain a stable `local-owner` default; a Desktop host
supplies its OS identity adapter without manufacturing organization membership.
