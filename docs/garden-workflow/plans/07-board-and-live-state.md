# 07 / A richer board with truthful state

Status: Done.

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

## Completion

Implemented phase/evidence selection and lane cursors in `internal/core/garden/work`,
implementation summaries in `internal/gardenbackend/planner.go` and
`internal/ui/gardenapi`, scoped local refresh/reselection in `internal/ui/gardenlocal.go`,
and the phase/detailed board, seven-row cards, hit geometry, configurable
`garden_stages` binding and phase palette roles in `internal/ui`. Association
changes clear old local observations. Updated the user guides and added core,
backend, UI, palette and fake-backend render tests.

Checks run: the requested targeted suite, Garden/backend race suite, formatting,
`go vet ./...`, `go build ./...`, `GOOS=windows go build ./...` and `go test ./...`.
Race, formatting, vet and both builds pass. Garden-focused tests and six reference
renders pass. The targeted suite fails only the existing agent-host UI integration
tests (`start agent session: agent session unavailable`); the full suite also
encounters sandbox socket restrictions (`setsockopt: operation not permitted` /
`socket: operation not permitted`), unrelated pkgmgr Git-root expectations under
`/tmp`, and shellinit's inherited guard assertion. These gates need rerunning in
an unrestricted development environment. Go used `GOCACHE=/tmp/bonsai-go-cache`
because the default cache is read-only in this sandbox.

Implementation session observations expose no activity signal, so active or
unresolved implementation attempts display Activity unknown. Stored failures and
exits remain distinct; no review executor or completion inference was added.
The board prioritizes implementation when both local summaries exist; long
activity timestamps and blocker reasons truncate to terminal cells. Table retains
its existing planner badges and footer totals. Fake-backend renders cover both
modes at 80x24, 120x35 and 160x48; plain-text dumps are under the requested
scratchpad `p07-renders/` directory. No commit was made.
