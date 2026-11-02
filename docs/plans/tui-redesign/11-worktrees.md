# Plan 11 — Worktrees in the redesigned application

Status: implemented (validation record below). Prerequisite: 10 — Playground.
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

- [x] The parity checklist maps every legacy feature/binding to its replacement.
- [x] Checkouts, Processes, Branches and PR submenus are selectable and scoped.
- [x] Existing Git/process commands retain options, ownership checks and previews.
- [x] Garden/Accounts/Playground links preserve identity and return location.
- [x] Manual repositories/worktrees work with no Garden and no provider account.
- [x] Process and agent terminal focus remain distinct; leaving either workspace
      does not stop a process or accidentally send keys to a holder.
- [x] Affected UI/Git/process/daemon tests, race tests and full Go checks pass.
- [x] Update `README.md`, `docs/tui-cli.md`, `docs/agents.md` and relevant core docs.

## Legacy parity checklist

Default keys below retain configured overrides, except the intentional list
navigation changes described explicitly. The existing root handlers remain the
compatibility route until plan 12; there is no nested legacy model/event loop.

| Legacy feature / binding | Redesigned replacement |
|---|---|
| Worktree selection, `/` filter, `o` six sorts | Checkouts; selection retained by path through sorts/refreshes |
| Metrics, dirty, PR/check, process and agent badges | Existing decorated Checkouts rows, shaded with installed theme roles |
| Inspector `i`, Git Log `l`, Diff `d`, Checks `b`, PR `P` | Same handlers in detail; list/detail above 100 columns, replace-list navigation at 80 |
| Processes `v`, all processes `V` | Processes submenu, checkout process detail and existing global process picker |
| Process arrows, `j/k`, `g/G`, `/`, `K`, `r`, `p`, `m`, `L`, `y`, `n`, `x` | Same output viewport, search/follow, controls, policy, multi-view, tags, copy, scripts and removal handlers |
| Process names, policies and discovered URLs | Cached process rows and detail footer; bounded footer keeps selected process visible |
| Worktree Agents `g`; `a`, `n`, digits, brackets, Enter, `s` | Same scoped session picker, launch dialog, selection, terminal and stop confirmation |
| Agent account `m`, usage `u` | Select exact account in top-level Accounts Overview/Usage; return to Worktrees |
| Open session in Playground | New scoped `O` / `agent_playground`, exact session ID, Observe first |
| Garden task/plan context | Inspector Issue/attempt references and palette link to loaded Issue document/plan sections; manual worktrees remain independent |
| Palette `ctrl+k`, help `?`, preferences `,`, glyph legend | Unified navigation/action palette, worktree key reference, top-level Settings and existing legend |
| Mouse wheel / palette toggle | Scroll hovered list/detail using shell geometry; disabled mode returns text selection to the terminal; agent terminals receive no wheel input |
| Self-update prompt | Intentional legacy-route compatibility until cutover; `bonsai update` remains available from either layout |
| Repository config editors | Existing palette settings and modal write flows; Settings remains available |
| Scripts `s`, aliases `a`, ad hoc command/new alias | Existing discovery, script/alias/submission handlers and daemon-owned processes |
| File copy `c`, yank `y` | Same file picker, recent-file flow and clipboard target menu |
| Shell Enter, editor `e` | List Enter intentionally inspects; Open shell stays in palette and ordinary detail Enter; editor binding unchanged |
| Create `n`: new branch / existing / PR | Existing three-way create flow; unchecked Branch/PR rows preview configured checkout destination and creation hooks |
| Fetch `f`, pull `ctrl+p`, push `ctrl+u`, commit `C`, rebase `r`, update-base `u` | Same core commands, commit preview and rebase/merge choices |
| PR approve/request changes/comment, merge/close/reopen/ready | Same PR detail and palette actions, strategies and confirmations |
| Prune `x`, merged prune `X`, force/merge/hooks options | Existing prune dialogs and core/daemon ownership/lock checks |
| Tab focus, Shift+Tab detail tabs | List/submenu Tab/Shift+Tab selects submenu; detail Tab returns to list, Shift+Tab cycles details; Escape walks parents |
| Quit `q`/`ctrl+c` and keep/stop processes | Existing cached active-process choices, keep selected by default; agent input capture takes precedence |

## Completion record

Implemented on 2026-10-22. New `internal/ui/worktrees.go`, `worktrees_view.go` and
`worktrees_test.go` provide presentation/routing, ordering and integration tests.
Shared shell/session/terminal/component handlers and documentation were updated;
Git, daemon and process backend commands were retained.

Automated UI demos cover Checkouts, Processes, Branches and PRs at 80×24,
120×35, 160×48 and the minimum shell size; compact inspect/parent navigation,
manual worktrees without accounts/Garden, unchecked-row action isolation,
stable selection, stale/failed reads, process search/scroll, cancellation and
exact Accounts/Playground return identity. Fake holders verify Observe handoff
without input; existing terminal suites exercise control, replay and ownership.
No real provider account or remote backlog was changed.

Verification passed (2026-10-22):

- `gofmt -l $(git ls-files '*.go')`, formatting of new `worktrees*.go` files,
  and `git diff --check` (all empty).
- `go vet ./...`, `go build ./...`, `go test ./...`.
- `go test -race ./internal/ui/... ./internal/git/... ./internal/daemon/... ./internal/core/procstore/...`.
- After the final session-index scope/IO corrections:
  `go test ./internal/ui -run '^(TestWorktrees.*|TestPlayground.*|TestPg.*)$'`
  and the same UI/session selection under `-race`.
- Final filter, wheel, focus and handoff regressions:
  `go test -race ./internal/ui/... -run '^(TestWorktrees.*|TestRefreshPreservesFilterAndSelectedPath)$'`.

Legacy remains the default; plan 12 cutover was not performed. The updater's
legacy notification is intentionally retained on that route; `bonsai update`
continues to work independently of the selected TUI layout.
