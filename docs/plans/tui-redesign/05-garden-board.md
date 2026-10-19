# Plan 05 — Garden board

Status: not started. Prerequisite: [04 — Garden foundations](04-garden-foundations.md).
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

- [ ] All nine columns and every visible card are keyboard-selectable.
- [ ] Empty columns and the 80-column moving window preserve selection correctly.
- [ ] Drafts are excluded; type/stage are not conflated.
- [ ] Enter/Escape round-trip to the exact item and board location.
- [ ] Refresh, reorder, filtering-out and deletion of the selected item have
      deterministic fallbacks; unrelated updates do not steal focus.
- [ ] All sizes/themes/ASCII and long Unicode titles fit measured regions.
- [ ] State/focus/render tests and full Go checks pass.
- [ ] Update `README.md`, `docs/tui-cli.md` and the new Garden guide.

Completion record: pending implementation and validation evidence.
