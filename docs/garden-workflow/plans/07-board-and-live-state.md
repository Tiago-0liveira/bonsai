# 07 / A richer board with truthful state

Status: planned.

## Deliverable

Render six readable phases by default, retain the detailed nine-stage view, and
show real local activity alongside verified GitHub workflow state.

## Read only

Source: `internal/core/garden/work/work.go`, `internal/ui/gardenboard.go`,
`internal/ui/gardenboard_view.go`, `internal/ui/gardenlocal.go`,
`internal/gardenbackend/work.go`, `internal/ui/theme/theme.go`.
Images: [phases](../design/png/board-160x48.png),
[detailed](../design/png/board-detailed-160x48.png),
[compact](../design/png/board-80x24.png).

Entry: canonical acceptance selection, local planner/launch projections and a
stable-ID ticket overlay host exist. Unavailable reviewer evidence stays unknown.

## Specification

- Default phases: Backlog, Planning, Ready, Building, Review, Done. Map Approved
  to Backlog; Planning/Plan review to Planning; Running to Building; AI/Human
  review to Review. Preserve exact state text on cards and a detailed-stage toggle.
  Grouping never writes labels or grants acceptance.
- A blocker retains its last proven phase with a reason; use stored attempt/job
  evidence, never guess from title or labels. If no phase can be proven, show an
  explicit unplaced blocker/attention group. Detailed mode retains Blocked.
- Cards show number, readable title, type/priority, semantic status rail, observed
  activity or next decision. Integrate planner and implementation evidence without
  confusing local work with remote canonical approval. Show timestamps/staleness;
  idle, exited, failed and unknown are distinct. No guessed percentages.
- Add phase theme roles with fallback colors and ASCII labels across all presets.
  Narrow terminals window columns and scroll cards, keeping the selected ID visible.
  Both views preserve independent scroll while sharing item identity and filters.
- Retain last good collections after failed/incomplete refreshes. Local job updates
  cannot resurrect a deleted item, override newer state or manufacture eligibility.

## Checks and exit

Run `go test ./internal/core/garden/work/... ./internal/gardenbackend/... ./internal/ui/...`.
Cover every mapping, blocked-phase evidence, moved/revoked approvals, stale reads,
late job events and selection/window behavior. Demonstrate both board modes at
reference sizes. No new review executor, canonical label taxonomy or fabricated
completion data is included.
