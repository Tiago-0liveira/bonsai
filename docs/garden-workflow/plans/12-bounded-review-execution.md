# 12 / One review, permitted fixes, then checks

Status: planned.

## Deliverable

Execute a previously authorized review sequence after implementation ends, survive
TUI exit/restart and hand off truthful evidence without repair loops.

## Read only

Source: `internal/core/garden/launch/reconcile.go`,
`internal/core/garden/launch/service.go`, `internal/core/agenthost/manager.go`,
`internal/core/agenthost/operation.go`, `internal/agentsbackend/launch.go`,
`internal/cli/`, `internal/gardenbackend/launch.go` (runtime wiring only).
Images: [review](../design/png/ai-review-120x35.png),
[human handoff](../design/png/human-review-120x35.png).

Entry: an implementation attempt carries explicit immutable review consent,
durable review identities/states and typed check specifications. Existing APIs
identify the owned worktree and holder; check execution is part of this deliverable.

## Specification

- Put transitions in a focused `internal/core/garden/review/` service (new), wired
  to a durable local supervisor outside the TUI. Use the existing holder/job/runtime
  mechanisms; add only the minimum supervisor entry point if needed. `tea.Tick`
  cannot be the authority: implementation/review must continue after the UI exits.
- Only a positively closed successful implementation with recorded consent may
  advance. Reverify ownership, exact approved inputs, policy and account/native
  permissions before starting. Reserve the operation before effects and reconcile
  unknown starts by identity. One controller/reviewer may own the attempt at a time.
- Give the reviewer the approved spec/plan and exact implementation diff. Permit
  fixes only within the authorized worktree/native permissions. Scope changes,
  missing permissions or new requirements pause for the user. Never push/merge.
- Run one review/fix pass, then the authorized checks against the resulting work.
  Capture reviewed and final HEAD/tree identities; uncommitted changes must be
  represented, not ignored. No automatic rereview or recursive repair after failure.
- Retain findings, applied fixes, remaining concerns and check outcomes through a
  bounded private report with validated references. Sanitize display and never infer
  success from prose/exit alone. Failure/stop/unknown checks remain visible.
- Feed state/evidence to Garden projections. Closing/reopening screens or receiving
  duplicate lifecycle events cannot trigger a second pass. Retry requires a fresh
  explicit user action and proven closure of old holders.

## Checks and exit

Use fake providers/check runners and temporary repos. Prove UI exit/restart,
competing controllers, crash/unknown start, duplicate completion, spec changes,
dirty changes, failed checks and stop all preserve work without duplicate passes.
Run affected core/runtime/backend/UI tests and race checks, then the shared Go
gates. Stop at persisted evidence and human handoff; no automatic acceptance/merge.
