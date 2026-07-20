# @layerdraw/review

Framework-neutral presentation state for Review sessions, semantic diff,
comments/annotations, approval, and AI proposals. The package consumes a
trusted host `ReviewClient`; it never computes semantic diff, Required
Capability, Access decisions, or Runtime commit results.

The host owns durable proposal state. Both Desktop UI and MCP read and mutate
that same state through adapters over the same Review application component.
`approveAndApply` is only a request: the host re-previews, re-evaluates the
current approver and delegation, and commits atomically through Runtime.

