# 13 / Human review, rework and verified completion

Status: planned.

## Deliverable

Give the user a clear result to inspect, explicit rework controls and a trustworthy
Done state linked to actual completion evidence.

## Read only

Source: `internal/ui/gardenlaunch_view.go`, `internal/ui/gardenplan_view.go`,
`internal/core/garden/launch/publish.go`, `internal/core/garden/sync/`,
`internal/core/garden/work/work.go`, `internal/git/github/service.go`,
`internal/gardenbackend/launch_remote.go`.
Images: [human review](../design/png/human-review-120x35.png),
[Done](../design/png/done-120x35.png),
[blocker](../design/png/blocked-120x35.png),
[stale gate](../design/png/stale-120x35.png).

Entry: retained implementation attempts and optional review reports expose exact
reviewed/final work identities, findings and actual verification outcomes.

## Specification

- Human review shows a concise outcome, readable changes, tests/checks, AI fixes,
  unresolved concerns and exact evidence source/time. Distinguish no AI review,
  failed/skipped/unknown checks and post-review fixes without a second review.
  Missing evidence never becomes a green check.
- Open changes/retained terminal/linked PR through existing services. Acceptance
  records the human decision against exact spec/plan and final HEAD/tree/report
  identity; changed work makes it stale. Define a verified versioned decision using
  existing canonical-record conventions; a local badge or prose comment cannot
  authorize completion.
- Request changes records actionable feedback against the reviewed result. Starting
  rework remains a distinct confirmed retry using the retained owned worktree, after
  old holders are proven closed. A new run needs its own applicable approvals and
  bounded review authorization. No silent rerun from remote comments.
- Preserve separate previewed push/Draft PR publication. For coding tickets, derive
  Done from a freshly verified linked PR merge with matching reviewed PR head and
  current acceptance, not agent exit or an arbitrary closed Issue. For non-code
  deliverables, expose explicit acceptance/completion with recorded evidence; do not
  fabricate a PR requirement or silently treat lack of changes as success.
- Keep blockers, stale approvals and offline sync reasons actionable and retain
  history/worktrees. Cleanup remains separately previewed; merge is never automatic.

## Checks and exit

Run affected Garden, GitHub adapter/backend and UI tests with race checks for
concurrent state. Cover changed heads, squash-merge identity, wrong/closed-unmerged
PRs, revoked acceptance, stale/offline reads, rework deduplication and non-code
completion. Demonstrate review through Done with fakes in the real TUI. Stop at
truthful handoff/completion; no automatic merge, deletion or release cutover.
