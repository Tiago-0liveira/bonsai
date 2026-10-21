# Plan 11 — Worktrees in the redesigned application

Status: not started. Prerequisite: 10 — Playground.
Next: 12 — Cutover and hardening.

## Deliverable

Finish the fifth major workspace without sacrificing the existing Git, process,
shell/editor, copy, alias or PR workflows. This is a presentation/routing migration
over existing authorities, not a Git backend rewrite.

References: [checkouts](../../design/tui-redesign/revision-02/png/w-01-120x35-main.png),
[compact checkouts](../../design/tui-redesign/revision-02/png/w-01-80x24-main.png),
[processes](../../design/tui-redesign/revision-02/png/w-02-120x35-api.png),
[branches](../../design/tui-redesign/revision-02/png/w-03-120x35-151.png),
[PRs](../../design/tui-redesign/revision-02/png/w-04-120x35-67.png).

## 11.1 — Inventory and move read surfaces

- Build a parity checklist from the existing root UI: worktree list/sorts/filter,
  Inspector, Git Log, Diff, Checks, PR, Processes, Agents, usage, palette/config,
  scripts/aliases, file copy, shell/editor, create and prune variants.
- Move worktree-specific state behind the Worktrees body gradually. Keep shell
  routing and shared session/account stores outside it. Do not transplant the
  entire old root model with another event loop and duplicate background timers.
- Checkouts provides selectable worktrees, metrics, dirty/ahead-behind/PR badges
  and inspector sections. Branch and PR submenus reuse backend IDs and services;
  Git state remains daemon/core authority.
- Use the common shade/icon/focus system and breadcrumb/parent conventions.
  Large terminal list/detail becomes a single navigable surface at 80 columns.

## 11.2 — Processes and existing actions

- Reuse process store/log/terminal behavior, including selection, search,
  follow-versus-paused output, multiple-process views, names, policies and URLs.
  Process logs are not agent terminal input and retain their own scoped controls.
- Port create/copy/editor/shell, scripts/aliases, fetch/pull/push/rebase/commit,
  update-base, PR review/merge/close/ready and prune flows without dropping options.
- Keep exact consequence previews and confirmations for destructive operations.
  A new workspace layout must not weaken active-session/account/worktree locks or
  remove path ownership checks. Preserve daemon error handling and stale ordering.
- A dirty or occupied worktree cannot disappear through an automatic Garden
  cleanup. Accepting task work, merging a PR and pruning remain separate actions.

## 11.3 — Connect to Accounts, Garden and Playground

- The worktree Agents shortcut `g` remains a worktree-scoped route to its real
  sessions. `a`/`n` keep their existing agent-context meanings while that surface
  owns focus; global account/settings navigation does not steal those bindings.
- Open in Playground carries the exact session ID and starts in Observe.
  Account/usage actions select the same account in the top-level Accounts space.
- Link task/plan context for Garden-created worktrees without making every manual
  worktree a managed Issue or requiring Garden setup to use Git tools.
- Keep selected worktree, local process and document scroll through round-trips.
  Moving between workspaces does not stop processes or holders.
- Compare against the parity checklist before deleting obsolete wrappers; keep
  only intentional legacy-route compatibility until plan 12.

## Source seams

`internal/ui/model.go`, `internal/ui/update.go`, `internal/ui/view.go`,
`internal/ui/cmds.go`, `internal/ui/procview.go`, `internal/ui/procmodal.go`,
`internal/ui/components/worktreelist/`, `internal/ui/components/terminal/`,
existing PR/diff/check/inspector handlers, `internal/git/`, `internal/daemon/`
and `internal/core/procstore/`.

## Exit criteria and verification

- [ ] The parity checklist maps every legacy feature/binding to its replacement.
- [ ] Checkouts, Processes, Branches and PR submenus are selectable and scoped.
- [ ] Existing Git/process commands retain options, ownership checks and previews.
- [ ] Garden/Accounts/Playground links preserve identity and return location.
- [ ] Manual repositories/worktrees work with no Garden and no provider account.
- [ ] Process and agent terminal focus remain distinct; leaving either workspace
      does not stop a process or accidentally send keys to a holder.
- [ ] Affected UI/Git/process/daemon tests, race tests and full Go checks pass.
- [ ] Update `README.md`, `docs/tui-cli.md`, `docs/agents.md` and relevant core docs.

Completion record: pending implementation, parity checklist and validation evidence.
