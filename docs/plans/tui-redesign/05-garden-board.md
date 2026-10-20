# Plan 05 — Garden board

Status: partially implemented: board interactions implemented; Planning, Ready and Reviewing population awaits backend evidence; full verification has environmental failures. Prerequisite: [04b — Garden wiring](04b-garden-wiring.md).
Next: [06 — Garden table](06-garden-table.md).

## Deliverable

The Garden's default approved-work view: a state board with individually
selectable cards in individually navigable columns. Draft ideas never appear.
The first item detail is read-only; authoring, approvals and launches come later.

References: [120×35 board](../../design/tui-redesign/revision-02/png/g-01-120x35-161.png),
[80×24 board](../../design/tui-redesign/revision-02/png/g-01-80x24-161.png),
[160×48 board](../../design/tui-redesign/revision-02/png/g-01-160x48-161.png),
[item submenu](../../design/tui-redesign/revision-02/png/g-04-120x35-161.png).

## 05.1 — Shared Garden state and filtering

- Add the Garden workspace body and a single managed-item selector from plan 04.
  Keep collection, query, selection, freshness and active subview distinct.
- Approved-work eligibility follows the canonical acceptance contract, not just
  `stage != Draft`. Missing/unverified acceptance is not silently promoted.
- Group accepted work into Approved, Planning, Plan Review, Ready, Running,
  Reviewing, Human Review, Done and Blocked. Type is separate and remains visible.
- Use real backend data only. Empty is correct if no accepted work exists yet;
  screenshot tasks belong only in tests/design fixtures.

## 05.2 — Selection, columns and detail

- Left/right changes column; up/down changes card within a column. Retain a
  selected Issue ID and per-column scroll state. Empty columns are navigable and
  explicitly empty. Incoming events do not jump the cursor to an attention item.
- At 120/160 columns show all states as the approved design does, with compact
  headers, bounded titles and the full selected identity in the footer. At 80,
  show a four-column window and reveal the selected column with arrows; details
  replace the board rather than compressing another pane into it.
- Use terminal cell width for truncation, including CJK/combining text. No card,
  count, focus marker or border can overwrite another region.
- Enter opens the selected Issue as a readable full document with a visible
  breadcrumb and parent action. Escape returns to the same card and scroll.
- Render Overview, Specification, Questions, Plans and Execution context from the
  canonical record; later actions are unavailable with a specific reason until
  their plan is implemented. Selecting or opening never writes stage labels.

## 05.3 — Filters and feedback

- Implement scoped text/type/priority filters against the same collection; counts
  state whether they are filtered counts. Remember the Garden view per repository.
- Show sync time, offline/stale state and per-item approval/conflict indicators.
  Preserve the last good board on failed refresh; do not confuse it with empty.
- Distinguish selected card, focused region and currently opened submenu.
  Selection uses green; human gates amber; blockers red; surfaces remain neutral.
- Provide search/help/palette entries and ASCII fallback. Do not add mouse-only
  drag-and-drop or let moving a card become an implicit workflow mutation.

## Source seams

The shell/focus/theme from plans 01–02 and the Garden service/read projection from
04. Add focused Garden presentation files under `internal/ui`/`components` as
needed; keep filtering/grouping pure and reusable by plan 06.

## Exit criteria and verification

- [x] All nine columns and every visible card are keyboard-selectable.
      Limitation: Planning, Ready and Reviewing support navigation only and remain
      empty; evidence-based population of those three states is not implemented
      because the prerequisite backend does not expose it.
- [x] Empty columns and the 80-column moving window preserve selection correctly.
- [x] Drafts are excluded; type/stage are not conflated.
- [x] Enter/Escape round-trip to the exact item and board location.
- [x] Refresh, reorder, filtering-out and deletion of the selected item have
      deterministic fallbacks; unrelated updates do not steal focus.
- [x] All sizes/themes/ASCII and long Unicode titles fit measured regions.
      Review regressions now check actual Board, document and detail renders
      with ASCII-only source content, in addition to Unicode title bounds.
- [ ] State/focus/render tests and full Go checks pass.
- [x] Update `README.md`, `docs/tui-cli.md` and the new Garden guide.

## Completion record

Implemented on `feat/tui-redesign`, 2026-10-20, as uncommitted working-tree
changes. No commit or push was made. Prerequisite source was inspected, including
commit `c918e13` and the five most recent commit stats; all six G-01/G-04 reference
PNGs were opened at their three sizes.

Files and decisions:

- `internal/core/garden/work/{work.go,work_test.go}`: one UI-free selector,
  deterministic grouping/filtering, stable-ID reconciliation, per-column cursors,
  scroll and moving-window selection for reuse by plan 06.
- `internal/ui/gardenboard{.go,_view.go,_test.go}`, Garden setup/shell routing and
  `gardenapi`/fake: real read commands, board and five-section read-only document,
  scoped search/type/priority controls, palette/help, focus and view persistence.
- `internal/gardenbackend/work{.go,_test.go}` and backend composition/status:
  bounded read-only Issue loading through the existing sync store and epochs;
  reader-only GitHub adapter injection, same status source for Settings.
- Canonical Issue/adapter add display number/title; the item revision includes
  those edits. `Snapshot.SuccessfulAt` tracks successful sync independently of
  the latest failed attempt. Config state remembers only Board/Setup per repo
  through its existing ordered atomic write mechanism; its validation and
  persistence have temporary-home tests.
- `README.md`, `docs/tui-cli.md`, new `docs/garden.md`, and the canonical contract
  document the shipped behavior, keys, mapping and safe limitations.

Prerequisite limitations, kept explicit rather than inventing backend data:

- The foundation has six canonical stage labels, no active planning/review job
  projection, and specification-only approval verification. Planning, Ready and
  Reviewing are navigable but empty until plans 08/09 supply that evidence.
  Accepted ready-stage work projects to Approved or Plan Review according to the
  verified presence of a plan head; review-stage work projects to Human Review.
  No new stage labels, approval semantics, writes or launch behavior were added.
- Priority is optional display data from existing `priority:p0`–`priority:p3`
  labels; missing/conflicting values are unknown. Questions come from a canonical
  Markdown Questions/Open questions section. The read model has no local
  execution context, which the document states with the plan 09 reason.

Validation:

- `gofmt -l $(git ls-files '*.go')`: passed, no output; all new untracked Go files
  checked separately with `gofmt -l`, also no output. `git diff --check` passed.
- `go vet ./...`: passed.
- `go build ./...`: passed without Node.
- `go test ./internal/ui/... ./internal/core/garden/...`: Garden/core/component
  tests passed; the command fails on existing
  `TestAgentsIntegrationLiveReadOnlyView` and `TestAgentsIntegrationTerminalMode`
  because their agent holders cannot create Unix sockets in this sandbox.
- `go test -race ./internal/ui/...`: same two existing integration failures;
  no race report. The UI race suite passed with only those two tests excluded
  using `-skip '^TestAgentsIntegration(LiveReadOnlyView|TerminalMode)$'`.
- `go test ./...`: fails on existing socket-dependent tests across agenthost,
  agentsbackend, CLI/HTTP fixtures, daemon, providers, localapi and UI. The
  concrete restrictions are `socket: operation not permitted` and
  `setsockopt: operation not permitted`. Three unchanged `core/pkgmgr` tests also
  fail because this environment has a read-only `/tmp/.git`; temporary fixtures
  are therefore detected as part of `/tmp` instead of outside a repository.
  The full-suite checkbox remains open; tests, unrelated source and security
  boundaries were not weakened to manufacture a pass.
- Additional changed-package tests passed for `internal/gardenbackend/...`,
  `internal/core/config/...`, `internal/core/garden/...`, and
  `internal/git/github/ghcli/garden/...`; race checks passed for the first three.
- Focused Garden state/render/keyboard tests passed. Fake-backed interaction
  coverage includes every column/card, empty columns, all three window sizes,
  per-column scrolling, exact Enter/Escape restoration, stage moves, reorder,
  filtering/deletion fallbacks, stale/empty/unknown reads, unrelated/foreign/late
  events, every theme and ASCII mode, long CJK/combining titles and terminal
  control sanitization. No real account, network or home directory was used by
  the new tests. No live GitHub demo was attempted in the restricted sandbox.

Go checks used `GOCACHE=/tmp/bonsai-garden-go-cache` because the default cache
outside the writable roots is read-only. Full validation still needs a runner
that permits the repository's local TCP and Unix socket test fixtures.

## Review follow-up

All findings 1–17 addressed in the working tree with focused regression tests:
status adoption commands are batched with Work, nil UI state is allowed,
view writes happen only on change (failed writes can retry), paging and scroll
use shared geometry plus collection bounds, and initial empty reads do not
consume first-item selection. Canonical acceptance is recomputed once; accepted
records with display diagnostics stay visible with conflict feedback. Adapter
identity defects and invalid acceptance remain excluded and exclusion text says
so. Remote format controls are stripped while combining marks and emoji ZWJ
sequences survive. ASCII conversion covers actual component renders. Search
clears old notices and uses a static cursor. Questions respects fenced code.
Missing repository identity returns an explicit error, sync state reuses
`gardensync.Status`, and fake imports use standard grouping.

Reviewer validation outside this sandbox: `go test ./...` passes except
`internal/server/localapi.TestAgentUsageRealProviders`, whose failure is caused
by `CLAUDE_CONFIG_DIR` inherited from the host session and is unrelated to this
change. This is reviewer-provided evidence, not a local unrestricted rerun; the
full-check criterion remains open for a clean environment. The three unavailable
board-state populations remain a separate implementation limitation.

Review rerun results in this sandbox:

- Formatting (tracked and new Go files), `git diff --check`, `go vet ./...`
  and `go build ./...` passed.
- The requested combined UI/Garden/config/backend/`ghcli/...` test command
  failed on the two known agent integration tests and the existing
  `ghcli.TestServiceSendsTheGhTokenAndRevalidatesWithETags` HTTP fixture
  (`socket: operation not permitted`). The additional ghcli failure is reported,
  not ignored. Garden, config, backend and Garden adapter packages passed.
- Focused regression tests passed. The UI/Garden/config/backend race suite
  passed with only the two known agent integration tests excluded.
- The unrestricted full-suite result above remains reviewer-provided evidence;
  this sandbox's full rerun still encounters its existing socket restrictions
  and `/tmp/.git` fixture contamination.
