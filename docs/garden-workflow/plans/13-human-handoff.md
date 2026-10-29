# 13 / Human review, rework and verified completion

Status: Done.

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

## Completion

Canonical record: a new GitHub comment envelope kind `handoff`
(`internal/core/garden/handoff.go`) with decisions accepted, changes_requested,
completed (non-code) and revoked. It binds the exact spec revision and approval ID, plan
revision and decision ID, attempt, base and final HEAD/tree, review pass and revision,
report ref and PR number/head. It is verified on read: trusted actors only (the spec
approval policy) and a record hash covering the human text; acknowledgments are listed
in the text ("Acknowledged: ...").

Completion (`internal/core/garden/handoff/derive.go`): not-ready, awaiting-decision,
changes-requested, accepted, done, stale, blocked and unknown, with reason codes. Code
Done only from a fresh (under two minutes) PR read: linked PR number, merged, PR head
equal to the accepted head (squash merges fine) and the same base. Closed-unmerged,
wrong PR, wrong base or a merged different head is blocked; a changed head, tree, spec,
plan, review or PR is stale; a failed read is unknown. Non-code needs an explicit
"Complete without code" record with evidence text and is refused when the attempt
changed code. Done survives worktree cleanup.

Rework: `launch.Service.Rework`/`CheckRework` and `gardenbackend.StartRework`
(`internal/gardenbackend/handoff.go`), a separate confirmed action after a verified
changes_requested record. It reuses the retained owned worktree, requires the old holder
proven closed, puts the feedback in the new run's prompt only (not stored) and dedupes
per feedback comment. A fresh review pass is reserved for the new run when the policy is
enabled; a never-started earlier pass closes as `superseded` and a rework that fails to
start closes its pass as `implementation-not-started`.

TUI: `internal/ui/gardenhandoff.go`, `gardenhandoff_view.go` (Overview panel headings
YOUR REVIEW / COMPLETED / BLOCKED / STALE / UNKNOWN, CHANGES, verification lines,
findings, PR line, decision line, reasons with next steps, cancel-first confirmations,
acknowledgment toggles `1`-`9`), `internal/ui/gardenapi/{handoff.go,fake/handoff.go}`,
`internal/gardenbackend/{handoff,handoff_git}.go`, and edits to the launch store/service,
`gardenoverlay.go`, `gardenboard*.go`, `internal/core/git/git.go` and
`internal/git/github`. Nothing merges, closes, pushes, relabels or cleans automatically.
Docs: `docs/garden.md`, `docs/tui-cli.md`, `README.md`.

Demo: `internal/ui/gardenhandoff_demo_test.go` (`TestGardenHandoffFakeEndToEnd`) drives
human review, request changes, rework, accept and a squash-merged Done at 120x35 and
80x24 with fakes.

Checks: `gocheck.sh --race --full` on `internal/core/garden`, `internal/gardenbackend`,
`internal/core/git`, `internal/git` and `internal/ui/gardenapi`; targeted `-race` runs of
the Handoff, GardenLaunch and GardenOverlay UI tests; the full `internal/ui` suite
without `-race`. Three Sonnet review rounds (Codex was unavailable); findings fixed with
regression tests.

Limitations: phase 12 (the detached reviewer and check runner) is not implemented, so
real runs show "AI review authorized, not run" and "No checks recorded", and findings are
never resolved from reports (`ConcernsKnown` is always false). There is no "Open changes"
route: the panel lists changed files and the diff is read in the retained terminal or the
PR. The panel says "Accepted by @login" rather than "you". Done is shown in the overlay
and is not mirrored to the GitHub stage label or board column. Once-only decision
posting is per process; replay detection on re-read covers retries. With automatic
review enabled, a rework whose run fails to start closes its new pass, which makes the
changes-requested decision stale: request changes again to retry with feedback (without
review, the same feedback is offered again). A rework racing a second client can start a
run whose pass was just closed; the panel then shows that pass's real state.
