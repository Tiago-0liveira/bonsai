# 09 / Planning, plan review and focused feedback

Status: planned.

## Deliverable

Make the existing read-only planner and revision-bound decisions usable from the
ticket overlay without reconstructing session settings for each revision.

## Read only

Source: `internal/ui/gardenplanner.go`, `internal/ui/gardenplanner_view.go`,
`internal/ui/gardenplan.go`, `internal/ui/gardenplan_view.go`,
`internal/core/garden/plan/`, `internal/core/garden/planrev/`,
`internal/gardenbackend/planner.go`, `internal/gardenbackend/planreview.go`.
Images: [planning](../design/png/planning-120x35.png),
[review](../design/png/plan-review-120x35.png),
[feedback](../design/png/plan-feedback-120x35.png).

Entry: the overlay host can display real planner attempts and documents; canonical
spec acceptance and exact plan/spec approvals remain enforced.

## Specification

- Approved work offers a previewed read-only planning request. Reuse supported
  account/model/effort and native capability checks; keep checkout/base and dirty
  read consent explicit. Leaving the overlay never stops the holder.
- Show actual elapsed time, latest reported activity, needs-input and plan-file
  state. Do not infer a finished plan from terminal prose or idle status alone.
  Plan publication remains an explicit preview of exact text and source spec.
- Open the current reviewable candidate by default. Summarize approach, changes
  from its predecessor, risks and verification; retain full document navigation,
  comparisons and history. Keep latest, current and approved snapshots distinct.
- Request changes captures feedback against one exact revision. Send feedback &
  revise plan explicitly records that decision and requests one local attempt with
  the retained supported settings. Journal/reconcile partial success; reopening or
  rereading feedback never starts another attempt. Plain GitHub comments are hints.
- Approve names the exact current plan/spec pair and grants Ready without coding.
  Changed/revoked spec or plan explains the stale gate, retains history and requires
  another deliberate decision; no automatic rebasing of approval.

## Checks and exit

Run `go test ./internal/core/garden/plan/... ./internal/core/garden/planrev/... ./internal/gardenbackend/... ./internal/ui/...`
and affected race tests. Cover feedback/start partial failures, duplicate actions,
late replies, moved heads and keyboard/scroll at narrow sizes. Stop at usable
planning/review; no implementation start or automatic reviewer launch changes.
