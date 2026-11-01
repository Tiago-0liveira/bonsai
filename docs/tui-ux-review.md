# TUI UX review: default `bonsai` (legacy layout)

Scope: the TUI started by plain `bonsai` (legacy layout; the redesign is opt-in via
`--tui-layout redesign`). Findings come from running the real binary at 160x45 and
100x30 and checking the code. IDs are referenced by the fix workstreams below.

Severity: R = critical, Y = moderate, G = minor. Fix order inside each workstream: R, then Y, then G, then QoL.

## Findings

### Information scent and glyphs

| ID | Sev | Finding | Fix |
| --- | --- | --- | --- |
| F1 | R | Tab labels hardcode their key (`Agents (g)` in `view.go` `tabLabel`, `Agents (g)` / `a: Switch` in `agentsview.go` `narrowRow`). A user override (`agents_tab: ctrl+g`) leaves the label wrong and `g` silently does nothing. | Build every label/hint from the live binding (`m.keys.X.Help().Key`). |
| F2 | R | `●` means four things on one row: CI dot prefix, `●N` dirty count, `● merged` PR state, session status. | Distinct shapes: CI `✓` / `✗` / `◌`; dirty `±N`; PR state as a coloured word only; agents in their own badge (F4). |
| F3 | Y | `!` (unacknowledged failed agent) has no legend anywhere. | Replace with `✗N` in the error colour (F4) and add a glyph legend to the `?` keymap overlay. |
| F4 | R | Agent state is invisible on worktree rows except the failure `!`. | Agent badge shows live counts by state: `▶N` working, `⏸N` waiting for input, `✓N` done, `✗N` failed (unacked), in a fixed column. |

### Agents tab

| ID | Sev | Finding | Fix |
| --- | --- | --- | --- |
| F5 | R | Empty state says "No running agents" / "No active agents" while the worktree has a failed session; it is only visible after `a`. | Empty state lists failed/exited sessions ("1 failed session · a view"); strip shows them dimmed. |
| F6 | Y | Header "No running agents" and body "No active agents" repeat each other. | Keep one message. |
| F7 | Y | `a` is labelled "all agents" but the picker is "All sessions in this worktree". | Label it "sessions"; add a real cross-worktree view (F8). |
| F8 | Y | No way to see every session across worktrees. | `A` opens all sessions of the repo with a worktree column; enter jumps to that worktree + session. |
| F9 | Y | Naming drift: footer `m profiles`, screen "Accounts", keymap "agent accounts", palette "Agents: accounts". | Use "accounts" everywhere. |
| F10 | Y | Detach key `Ctrl+]` is only in help. | Always show `ctrl+] detach` in the agent pane frame while in terminal mode. |

### Spawn workflow

| ID | Sev | Finding | Fix |
| --- | --- | --- | --- |
| F11 | R | Launch form never says where the agent will run (implicit selected worktree). | Header `Start in: <branch> (<short path>)`; `ctrl+w` (or a focusable row) changes the target worktree. |
| F12 | R | No task/prompt field; `agents.LaunchOptions.Prompt` exists but the TUI never sets it. | Multiline Task field, sent as `Launch.Prompt`. |
| F13 | R | No "new worktree + agent" path (2 modals + tab switch + `n`). | "Start agent here" toggle in the create-worktree flow that opens the launch form for the new worktree; palette entry "New worktree with agent…". |
| F14 | Y | Session name defaults to the account name (3 accounts are called `tfg`). | Default `<branch>-<provider>-<N>`; once a task is typed, its first few words. Still editable. |
| F15 | Y | Account list shows only 5H; the weekly limit (Codex W 44%) is hidden. | Show `5H` and `WEEK`; pre-select a "suggested" account with the most headroom (min of both windows), marked as such. |
| F16 | G | Many accounts of one provider flood the picker. | Group by provider with a header row per provider. |

### Worktree list, create flow, usage pane

| ID | Sev | Finding | Fix |
| --- | --- | --- | --- |
| F17 | Y | Row badges do not line up across rows; paths take half the list height and mostly repeat the branch. | Fixed columns (branch, PR, CI, dirty, agents, ahead/behind); full path only on the selected row, `~`-shortened elsewhere or hidden. |
| F18 | Y | New-branch modal has no base branch, no path preview, no key hints. | Live preview `from <base> → <path>`, editable base, hint line `enter create · esc cancel`. |
| F19 | Y | Usage pane takes 11 rows; at 30 rows only 3 of 9 worktrees fit. | Collapsed one-line-per-provider summary by default, expanded when focused; auto-collapse under ~35 rows. |
| F20 | G | Pagination is only `••`. | Title shows `Worktrees 3/9`. |
| F21 | G | Title suffix "activity" is the sort mode, unexplained. | `sort: activity`. |
| F22 | G | Provider glyphs `✹ ❃ ✦` in the usage pane have no legend. | Glyph plus short provider tag, or a legend line. |

### Consistency and accessibility

| ID | Sev | Finding | Fix |
| --- | --- | --- | --- |
| F23 | Y | Palette, keymap and create modals blank the screen instead of floating over it. | Render overlays over a dimmed base view. |
| F24 | Y | Inactive tabs, hints and paths use colour 240 (~2.9:1 on black). | Raise muted text to >= 4.5:1 (e.g. 245-248 or theme equivalent) across theme presets. |
| F25 | G | Wide layout tab is `Git Log` with no key, compact is `Log (l)`. | Always show the key. |
| F26 | G | Hint styles differ: legacy footer `a all · n new`, launch form `↑↓ Account  Enter Options  Ctrl+S Start`. | One style: `key action · key action`, lowercase keys. |
| F27 | G | CI pass/fail differ only by colour. | Covered by F2 shapes; check `NO_COLOR`/ASCII glyph mode keeps them distinct. |
| F28 | G | Status messages stay forever. | Clear transient status after ~5s or on the next key. |

## QoL backlog

| ID | Item |
| --- | --- |
| Q1 | Attention queue: `!` jumps to the next session needing you (waiting, failed, finished) across worktrees. |
| Q2 | Live agent state on rows with time since last output (`▶ 2m`). Pairs with F4. |
| Q3 | ~~Retry a failed session (`r` in the picker / empty state) with the same account and options.~~ Removed by request. |
| Q4 | Quick spawn (`N`): launch with the suggested account straight into the Task field. |
| Q5 | "Same settings, new task": open the launch form seeded from the selected session. |
| Q6 | Peek: picker detail pane shows the last ~10 lines of the session's screen. |
| Q7 | Rename a session; stop all sessions of a worktree (with confirm). |
| Q8 | Sort worktrees by agent attention (needs-you first). |
| Q9 | Prune guard: warn when pruning a worktree with live sessions, naming them. |
| Q10 | Terminal bell when a session goes working -> waiting/done, toggle in preferences. |

## Workstreams

| Workstream | IDs | Owns |
| --- | --- | --- |
| A: labels, theme, overlays | F1 (view.go side), F23-F28, F25 | `view.go`, `overlays.go`, `theme/`, `palette.go`, `keys.go` legend wiring |
| B: agents tab | F1 (agents side), F3-F10, Q1, Q3, Q6, Q7, Q10 | `agents.go`, `agentstab.go`, `agentsview.go`, `agentoverlay.go`, `agentsessions.go`, `agentattach.go` |
| C: worktree list, create, usage | F2, F17-F22, F13 (create side), Q8, Q9 | `components/worktreelist`, `components/modals`, `components/usagepane`, `usagepane.go`, create/prune flow |
| D: spawn flow | F11, F12, F13 (launch side), F14-F16, Q4, Q5 | `components/launchform`, `agentlaunch.go`, `agentlaunch_view.go` |
