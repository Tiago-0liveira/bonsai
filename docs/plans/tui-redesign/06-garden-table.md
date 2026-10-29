# Plan 06 — Garden table

Status: implemented; one environmental full-suite failure noted below. Prerequisite: 05 — Garden board.
Next: 07 — Garden drafts.

## Deliverable

A row-oriented Table submenu for the **same approved work** as Board. Every row
is selectable; switching Board/Table preserves identity, filters and current
document context. This is another view, not another store or sync loop.

References: [table](../../design/tui-redesign/revision-02/png/g-02-120x35-161.png),
[compact table](../../design/tui-redesign/revision-02/png/g-02-80x24-161.png),
[wide table](../../design/tui-redesign/revision-02/png/g-02-160x48-161.png).

## 06.1 — Rows over the shared selector

- Reuse plan 05's approved-work eligibility, query and selected Issue ID.
- Show Issue number/title, independent type/stage, priority and relevant freshness
  within available width. Prefer task identity and stage before optional metadata.
- At 120/160, provide a selected-item inspector with the next human gate. At 80,
  use one table and open detail full-screen. Drop optional columns before clipping
  meaningful state labels or removing the selection marker.
- Sorting/filtering is stable and never becomes a GitHub mutation. If the current
  selected record disappears, choose a deterministic nearby row and explain why.

## 06.2 — View continuity and navigation

- Up/down and page movement keep the focused row visible in large collections.
  Enter opens the same item detail used by Board; Escape restores the correct
  parent subview, selected row and scroll anchor.
- The view-toggle action changes Board/Table within Garden only. Scoped binding
  overrides and on-screen hints must agree. Do not introduce an application-wide
  `v` override that changes existing Worktrees process behavior.
- Update counts and stale/conflict indicators from the shared projection. A
  second component must not issue a competing request that clears the first.
- Type views can be additional saved filters over accepted work, sharing this
  renderer and Issue identity; draft Features still belong in Drafts.

## Exit criteria and verification

- [x] Board and Table show identical eligible Issue sets for the same query.
- [x] Switching views retains the exact Issue even when ordering differs.
- [x] Every row opens the shared detail and returns to its original parent view.
- [x] Large lists, empty filters, offline data and selection deletion are bounded.
- [x] Drafts remain excluded, including remotely relabeled/unverified ideas.
- [ ] Focus/selection/sizing tests and full Go checks pass (see the one environmental failure below).
- [x] Update `README.md`, `docs/tui-cli.md` and the Garden guide with both views.

## Completion record

Implemented 06.1 and 06.2 on `feat/tui-redesign`.

Files and decisions:

- `internal/core/garden/work/work.go`: `Sort` (State, Number, Priority; stable,
  ties by number then ID), `Collection.Rows`, `Selection.Focus` and `NextGate`.
- `internal/ui/gardentable{.go,_view.go,_test.go}`: row keys, scroll anchor, width-
  driven columns and the inspector. `gardenboard*.go` gain `layout`, `sort`,
  `tableScroll`, `tableRow` on the one `gardenBoardState`; `gardenWorkHead` is the
  header/feedback both layouts share. Toggling never calls `gardenLoadWork`.
- `keys.go`/`keyscope.go`: new `ksGarden` scope, "Garden" section, `garden_view`
  (default `v`). It cannot collide with Worktree `processes` (`v`); an override
  changes behavior and every hint. Settings Keys lists the new section.
- `internal/core/config/state.go`: view memory accepts `table`.
- Selection deletion in Table picks the row now at the same index (previous row
  after the last), with a notice saying which. Optional columns are tried P, TYPE, STATUS with
  inspector (TYPE, P, STATUS without) and join only while TITLE keeps 44 cells.
- Docs: `README.md`, `docs/tui-cli.md`, `docs/garden.md`.

Validation: `gofmt -l` clean, `go vet` and `go build ./...` pass;
`go test ./internal/core/garden/... ./internal/core/config/... ./internal/ui/...`
and `go test -race ./internal/ui/` pass. `go test ./...` passes except
`TestAgentUsageRealProviders` (`internal/server/localapi/agents_usage_test.go:397`),
which inherits the session's `CLAUDE_CONFIG_DIR` so its seed source is not
`~/.claude`; this is environmental and unrelated to this change.

Limitation: Drafts remains disabled; the design's "Types v" saved-filter tab is
not built.
