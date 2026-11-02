# 08 / Tables and a Needs you view

Status: Done.

## Deliverable

Provide flat and phase-grouped tables plus a focused attention filter, all over the
same accepted-work projection used by the board.

## Read only

Source: `internal/ui/gardentable.go`, `internal/ui/gardentable_view.go`,
`internal/ui/gardenboard.go`, `internal/core/garden/work/work.go`,
`internal/ui/gardenapi/gardenapi.go`.
Images: [table](../design/png/table-120x35.png),
[groups](../design/png/table-grouped-120x35.png),
[attention](../design/png/attention-120x35.png),
[compact](../design/png/table-80x24.png).

Entry: accepted work has detailed state, phase, blocker/freshness and next-action
evidence. A ticket overlay can retain its Board/Table origin.

## Specification

- Rows show Issue, title, exact state, latest signal and next action. Use colored
  rails and words/symbols together. Drop optional columns with width; retain full
  state names and useful title space. Truncation is explicit and the overlay shows
  complete text.
- Group by phase without duplicating a group when blocked work retains that phase.
  Groups can collapse; preserve selected ID or explain the selected group's collapse.
  Sorting/filtering/grouping never changes backend state or issues another read.
- Needs you includes proven pending plan approval, explicitly startable Ready work,
  input requests, human review and actionable blockers. Do not classify all idle
  sessions or offline data as user requests. Counts describe the same filtered data.
- Enter/click opens the same item overlay and contextual actions as the board.
  Return restores row, group and scroll. A refresh cannot steal focus, select a
  different identity silently or clear last good rows after an error.
- Share query/type/priority and selected Issue across board/table changes. Private
  drafts stay in their own inbox. Remember only sanitized presentation preferences.

## Checks and exit

Run `go test ./internal/core/garden/work/... ./internal/ui/...`. Cover grouping,
collapse, attention predicates, filter counts, all stages in compact scrolling and
cross-view selection. Demonstrate table-origin overlays and keyboard/mouse access
at reference sizes. Stop at presentation/filtering; do not add task transitions or
new workflow storage.

## Completion

Implemented accepted-work attention predicates and filtered summaries, flat and
phase-grouped tables, independent collapse/cursor memory, and Needs you over the
same cached sorted projection. Item overlays retain their layout origin; header
navigation, mouse geometry, refresh fallback, ASCII rendering and view memory are
covered at 80×24, 120×35 and 160×48. No new backend reads, workflow stores or
transitions were added.

Files: `internal/core/garden/work/{work,attention}.go`, `attention_test.go` and
`phases_test.go`; `internal/core/config/state.go` and `state_test.go`;
`internal/ui/gardenboard*.go`, `gardentable*.go`, `gardenattention_test.go`,
`gardenoverlay*.go`, `gardensetup*.go`, `gardendrafts.go`, `gardendrafts_view.go`,
`gardenlocal_test.go`, `keys.go`, `keyscope.go` and the Garden palette entries in
`shell.go`. User documentation: `README.md`, `docs/garden.md`, `docs/tui-cli.md`
and the workflow index. Drafts keeps its existing inspector and capacity through
its own drawing helpers after removal of the work-table inspector.

Review fixes preserve the shell margin, search/exclusion feedback and shared
selection across Needs you visits. Counts use the filtered collection, collapsed
row totals count visible items, and header details describe expand/collapse.
Saved Garden key overrides use the Settings validation path before binding.

Checks used `GOCACHE=/tmp/bonsai-go-cache`:

- `gofmt -l $(git ls-files '*.go') $(git ls-files --others --exclude-standard '*.go')`
  and `git diff --check`: passed, no output.
- `go vet ./...`: passed.
- `go build ./...`: passed.
- `GOOS=windows go build ./...`: passed.
- `go test -count=1 ./internal/ui -run 'TestIssueOverlay|TestGarden'`: passed,
  including margin hits, feedback, filtered counts, selection restoration,
  collapsed rows, header details and saved key overrides. Reference renders cover
  80×24, 120×35 and 160×48 in Unicode and ASCII.
- `go test -count=1 ./internal/core/garden/... ./internal/core/config/... ./internal/ui/...`:
  all Garden core, config and UI subpackages passed. The whole UI package ran;
  only `TestAgentsIntegrationLiveReadOnlyView` and
  `TestAgentsIntegrationTerminalMode` failed, both with
  `start agent session: agent session unavailable`.
- `go test -count=1 ./internal/core/agenthost -run '^TestQueryContextCancelsInfoRead$'`:
  failed with `setsockopt: operation not permitted`, confirming the sandbox's
  Unix socket restriction behind the agent integration failures.
- `go test -race -count=1 ./internal/core/garden/... ./internal/ui/...`:
  all Garden core and UI subpackages passed; the whole UI package failed only
  the same two socket-dependent agent integration tests. No race reports.

The orchestrator verified the full `go test ./...` suite outside the sandbox
during review: the only reported failures were the Issue overlay margin cases
fixed here. Those cases pass in the focused rerun above. A post-fix full-suite
run outside the sandbox has not been performed by this builder.

Remaining limitations: the two agent integration tests require sockets that this
sandbox prohibits. Grouping/collapse remain session-only; only the sanitized
layout name is persisted. No workflow authority or backend producers changed.
