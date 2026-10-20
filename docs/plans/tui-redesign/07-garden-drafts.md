# Plan 07 — Separate Drafts, capture and import

Status: in progress (07.1 implemented; 07.2 and 07.3 pending). Prerequisite: [06 — Garden table](06-garden-table.md).
Next: [08 — Specifications, approvals and planning](08-garden-approval-and-planning.md).

## Deliverable

A separate Drafts inbox with minimal capture, safe specification editing and
deliberate import/removal of existing Issues. Nothing becomes accepted work or
starts a process just because it is created, edited, imported or viewed.

References: [Drafts](../../design/tui-redesign/revision-02/png/g-03-120x35-142.png),
[compact Drafts](../../design/tui-redesign/revision-02/png/g-03-80x24-142.png),
[new idea](../../design/tui-redesign/revision-02/png/g-09-120x35-142.png),
[conflict](../../design/tui-redesign/revision-02/png/e-03-120x35-142.png).

## 07.1 — Inbox and minimal capture

- Add the visually separate amber Drafts submenu. Query unvalidated managed
  ideas, any type; do not combine them into Board/Table or make type imply approval.
- Select rows by Issue ID with the same search/detail/return conventions.
- New asks for title, optional initial description and type choice. Always create
  a real repository Issue at Draft, not a GitHub Project-only draft card.
- Creation has an explicit preview/action, pending state and stable operation
  identity. On timeout, reconcile a possibly created Issue before allowing retry;
  never automatically create a duplicate to make the spinner finish.
- Make body/specification fields optional at capture. One later edit updates or
  appends a revision on the same Issue rather than opening another Issue.

## 07.2 — Safe refinement and remote edits

- Provide a full-screen structured editor for current/desired behavior,
  requirements, acceptance criteria, constraints and questions. Long documents
  have sections, focused search, position and a bounded reading width.
- Carry the base server revision/fingerprint into editing. Save shows a concise
  change preview and re-verifies remote state. Preserve dirty text on errors,
  profile changes, resize and navigation until explicitly discarded.
- Implement the concurrency strategy settled in 04. Where no atomic body update
  is possible, append a versioned specification revision and leave user-owned
  body text untouched. Divergent heads require a visible resolution, not silent
  last-writer-wins. A read-before-write check is not sufficient by itself.
- External GitHub/mobile edits reappear on refresh. Show actor, source, current
  revision and a Base/Yours/Remote/Result comparison. Resolve and publish only
  after another current-head check.
- Validate-idea can be shown as the next gate, but remains unavailable until 08
  adds its real approval record and transition. Do not change stage prematurely.

## 07.3 — Import and remove

- Existing unmanaged Issues remain unmodified until the user explicitly selects
  and confirms import. Preview the exact owned label/Project membership changes,
  conflicts and required permissions. Imported items begin in Drafts.
- Do not replace the original body, delete comments, remove unrelated labels or
  inherit an execution approval from an arbitrary existing status label.
- Record import provenance and only the metadata actually added/owned. Removal
  previews removal of those owned associations; it does not close/delete the Issue.
- Mid-import failures reconcile completed steps and support safe resume/cancel.
  Cancelling a preview performs no writes. Active linked execution blocks removal
  or requires a specifically reviewed detach policy once plan 09 exists.

## Source seams

The shared Garden service/schema, Issue adapter and list/document UI from plans
04–06. GitHub writes and local editor-draft persistence, if needed, remain in core
commands/stores; UI event handlers do not execute `gh` or write files.

## Exit criteria and verification

- [ ] New/imported ideas are Drafts only, with type shown independently.
- [ ] Minimal capture/refinement keeps the same canonical Issue.
- [ ] Concurrent edits cannot silently replace user-owned text; dirty local text
      survives failure and resolution has a preview.
- [ ] Create timeout, partial import and retry do not duplicate remote records.
- [ ] Import cancellation changes nothing; removal preserves the original Issue.
- [ ] No plan or implementation starts from capture/edit/import/remote updates.
- [ ] Fake-service mutation/conflict/focus tests, relevant race tests and full Go
      checks pass; tests do not write real GitHub Issues.
- [ ] Update `README.md`, `docs/tui-cli.md` and the Garden guide, including mobile
      revision guidance matching the actual canonical schema.

Completion record: pending implementation and validation evidence.
