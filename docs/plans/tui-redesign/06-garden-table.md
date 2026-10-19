# Plan 06 — Garden table

Status: not started. Prerequisite: [05 — Garden board](05-garden-board.md).
Next: [07 — Garden drafts](07-garden-drafts.md).

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

- [ ] Board and Table show identical eligible Issue sets for the same query.
- [ ] Switching views retains the exact Issue even when ordering differs.
- [ ] Every row opens the shared detail and returns to its original parent view.
- [ ] Large lists, empty filters, offline data and selection deletion are bounded.
- [ ] Drafts remain excluded, including remotely relabeled/unverified ideas.
- [ ] Focus/selection/sizing tests and full Go checks pass.
- [ ] Update `README.md`, `docs/tui-cli.md` and the Garden guide with both views.

Completion record: pending implementation and validation evidence.
