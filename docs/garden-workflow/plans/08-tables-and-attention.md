# 08 / Tables and a Needs you view

Status: planned.

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
