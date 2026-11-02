# Plan 12 — Cutover, compatibility and hardening

Status: not started. Prerequisite: 11 — Worktrees.
This is the final integration plan, not a substitute for checks in earlier plans.

## Deliverable

Make the redesigned terminal UI the default only after all five workspaces and
the full journey pass acceptance. Remove obsolete presentation paths deliberately;
retain existing runtime, storage and CLI contracts.

Reference: [approved design library](../../design/tui-redesign/README.md).

## 12.1 — End-to-end acceptance

Exercise these real flows in temporary repositories/fake services and then an
explicit manual demo environment; no test uses a real home, network or provider:

1. Launch legacy and redesign preview, change Settings, restart and restore the
   selected workspace for each repository without losing old preferences.
2. Add/select Accounts, inspect partial/stale usage, edit/unset defaults and return
   from sign-in without changing profile isolation or session ownership.
3. Connect Garden with zero imports; create a Draft; refine it; resolve a remote
   edit; validate the spec; request a local read-only plan; revise/approve it.
4. Verify Ready does not run anything. Change the source spec externally and prove
   stale approval blocks launch; renew exact approval and confirm a local launch.
5. Create isolation, handle a partial start failure/retry, push meaningful changes
   and link a Draft PR without duplication.
6. Supervise linked/manual concurrent sessions, take/lose control between clients,
   resize, reconnect, exit/restart the TUI and preserve the holder.
7. Review findings, request locally confirmed fixes and accept work without
   automatic merge, Issue closure or worktree deletion.
8. Use Worktrees Git/process/create/copy/prune functions without Garden. Import
   and later remove an existing Issue without altering its original body/comments.

## 12.2 — Platform, performance and resilience

- Verify 80×24, 120×35, 160×48 and resize during modal/editor/terminal activity.
  Check long Unicode titles, long specs/plans, large diffs and hundreds of Issues/
  dozens of sessions. The selected ID and input owner must remain stable.
- Check every theme, monochrome and ASCII fallback with readable state words.
  Layout must use terminal cell width and bounded regions rather than assumptions
  from SVG pixels or the specific font used in the mockups.
- Profile event/render paths with large fixtures. Avoid full-list repaint/rebuild
  for every document keystroke, unbounded terminal history or usage refresh storms.
  Set measured regression thresholds from this baseline, not invented budgets.
- Exercise auth revoked, read-only GitHub, rate limit/backoff, offline/stale data,
  malformed metadata, contradictory stages, unsupported PTY, ended records and
  holder version mismatch. Respect platform-specific terminal support and keep
  every supported OS compiling.
- Verify persisted records contain no tokens, credentials, raw prompts, env values
  or terminal dumps beyond the explicit bounded holder storage policy. The UI
  redesign adds no public listener, tunnel or weakening of local API checks.

## 12.3 — Default switch and cleanup

- Confirm all previous completion records and the Worktrees parity checklist.
  Switch the default layout in one focused change. Preserve parsing compatibility
  for the transitional selector or explicitly document its retirement policy.
- Remove redundant old presentation wrappers only after their replacements pass;
  do not delete shared account, session, Git, config or process services as cleanup.
- Ensure installed settings migrate forward with no destructive rewrite and a
  failed migration leaves old data readable. Document recovery, supported preview
  compatibility and the actual final navigation/keymap.
- Rewrite `README.md` and `docs/tui-cli.md` around final user behavior, update the
  Garden/agents guides and development architecture, and retain the design plans
  as history. Mockups remain labeled fabricated reference material.

## Exit criteria and verification

- [ ] All preceding plans have completion evidence and no hidden deferred blocker.
- [ ] Full journey, legacy-feature parity, focus ownership and state ordering pass.
- [ ] Platform builds, affected race suites and all required full Go checks pass.
- [ ] Plain Go build/test remains independent of Node or mockup-generation tools.
- [ ] No app/browser/API contract changed accidentally through TUI migration.
- [ ] Default cutover, compatibility/migration behavior and recovery are documented.
- [ ] Final review describes actual behavior and material limits; release labeling
      follows repository policy. No remote publish/merge is implied by this plan.

Completion record: pending final integration and acceptance evidence.
