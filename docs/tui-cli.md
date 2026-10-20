# TUI, CLI and configuration

Reference for the terminal side of Bonsai: the interactive TUI (`bonsai` inside
a repository), the plain CLI subcommands, project configuration, hooks, prune and
updates. For the browser workspace see [web.md](web.md); for AI agents see
[agents.md](agents.md).

The full terminal redesign has [approved mockups](design/tui-redesign/README.md)
and [sequential implementation plans](plans/tui-redesign/README.md). It is planning
work; the reference below describes the currently implemented TUI.

## TUI

Run `bonsai` inside a repository. Press `n` to create a worktree, `enter` to
drop into a shell in the selected one, `?` for the searchable keymap and `q` to
quit. `ctrl+k` opens the command palette, which also edits every `.bonsai.yaml`
setting (with descriptions and examples) and applies it live.

The worktree list shows ahead/behind arrows (`↑N ↓M`), `#PR` badges detected via
`gh`, dirty change counts (`●N ?M`), CI dots and 6 sort modes. The right pane has
Inspector (working-tree status, last commit, diff-vs-base summary, disk usage,
stash count), Checks (GitHub Actions runs for the branch), PR (detail, reviews,
merge/approve/close), Diff, Git Log and Processes tabs. Live dev-server URLs
(`http://localhost:…`) detected in process output are shown next to each process.

### Agents tab

The Agents tab lists the agents running in the selected worktree and the
selected account's usage; worktree rows group running agents by provider with a
glyph and count directly after the branch's other badges, plus `!` for an unseen
failure. Each glyph takes the style of its most urgent session: highlighted when
an agent waits for you, normal while one works, dimmed when all of that
provider's agents are idle. A single `?` follows the glyphs when any agent in the
worktree waits for a permission, answer or plan approval (`✹?`; the badge grows by
one cell). Agents whose provider reports no activity (hooks off or unsupported)
count as working. It shows the selected session's
live terminal read-only: you can watch the agent. The title row (`AGENTS · <branch>`)
appears when the terminal is tall enough. A header shows the session's provider,
profile, model and status, with `· read-only` when another client is controlling it.
The status is `Running`, `Working`, `Stalled`, `Idle` or, when the agent waits for
you, what it asks for (`Needs permission: Bash`).
The TUI uses the same agent sessions as the web UI and `bonsai agents`. If it starts
without that backend, the Agents tab is unavailable and the TUI prints
`agents unavailable: <err>` to stderr after exit. A session started by another
bonsai version cannot be opened here; the tab says to run
`bonsai agents attach <name>` instead.

The TUI is a client of the same sessions as the web app; see
[agents.md](agents.md#sessions-shared-with-the-tui-and-cli). It is read-only by
default: the terminal only watches until you press `enter`. On the focused Agents
tab, `n` starts a new agent, `m` opens accounts, `u` opens usage and `s` stops the
selected running agent after a confirmation. The command palette (`ctrl+k`) has
**Agents: open tab**, **new agent**, **accounts** and **usage** under **Agents**;
stop is available on the focused tab. Profiles, usage, the session
picker and launch/stop views fill the available Agents tab without modal borders
or size caps; the worktree list and right-pane tab bar remain visible.

Press `enter` on a session to enter terminal mode and type into it. The client
attaches as the writer and every key goes to the agent, including `esc`, `tab`,
`ctrl+c` and `q`. The PTY is resized to the pane when you enter, and window
resizes are debounced so the agent gets one redraw for the final size. Mouse
wheel events over the terminal area are ignored in terminal mode. Press `enter`
on a session that failed or dropped its attach to retry first. The binding is
`agent_attach` and can be rebound.

The terminal has a two-row header, the agent output, and a hint row at the bottom.
The bottom status bar shows **FOCUSED · Input goes to agent** while you are
controlling the agent. Press `ctrl+]` to leave focus immediately and return to
Bonsai navigation. The agent keeps running and its output continues to update.
The navigation footer shows the available actions: `a` all agents, `m` profiles,
`u` usage, `n` new agent, `s` stop, and `enter` focus the selected agent. Enter is
shown as retry while an attach has failed, and is hidden for an ended session.

If another client is the writer, the agent is read-only here: the banner reads
`Controlled by another client` and keys are swallowed. Press `ctrl+]`, then
`enter` in navigation to take control. If another client takes control away, the
hint explains how to leave focus and take it back.

Press `a` on the Agents tab to open the sessions screen. It lists the worktree's
sessions as one flat list, running ones first, then ended ones, newest first. Each
item shows the session name and its status (`● Working`, `● Waiting`, `● Idle`,
`✓ Exited`, `✗ Failed`) on one
line and `Provider · account` below it; sessions that share a name get a short ID
suffix (`· …b6364576`). The right side is an inspector that follows the selection:
provider, name, status (`Needs input (permission)` while waiting), account, worktree, model, exit code and the short ID. On a
narrow pane the inspector moves below the list. It opens even when there are no
sessions. `↑`/`↓` select, `/` searches by name, provider or account (`Esc` clears
the search), `Enter` opens the selected session, `n` starts a new one and `Esc` goes
back. Opening the screen clears the `!` failure markers on that worktree's rows, so
the failures stay listed here with `✗`.

The new-session screen (`n`) is an account table above the launch options. The
table lists every account with its **5h left** capacity (meter and percentage of
what remains) and **Resets in** countdown; only the reported five-hour window is
used, so an account without one shows `Not reported`, and loading, unavailable
and stale readings are labeled instead of drawn as a zero or full meter. `↑`/`↓`
(or `j`/`k`) select an account and `Enter` moves to the options. `Tab` and
`Shift+Tab` move through the name, model, effort, other launch options, the
**Save as account defaults** box and the buttons. Within the options, `↑`/`↓`
moves between fields, `←`/`→` changes a value, and `Enter` opens a choice list.
`Esc` dismisses that list before closing the screen. `ctrl+s` starts the
session. Model and effort start from the selected account's defaults; edits apply
only to this session unless **Save as account defaults** is checked. Effort
choices follow the selected model's capabilities. On short or narrow panes the
table shows fewer rows (`a-b of N`), then drops the meter, then uses two lines
per account; extra options scroll while Start and the footer stay in view.

In profiles (`m`), `Tab` and `Shift+Tab` cycle the right-hand Overview, Usage and
Defaults tabs while `↑`/`↓` keeps selecting profiles. No selection confirmation
is needed to browse. Press `Enter` to focus the details or edit a field; Tab
still switches sections there, including while a choice list is open. `1`/`2`/`3`
jump directly to Overview, Usage or Defaults. `Esc` returns focus to profiles;
press it again to close. Unsaved defaults require confirmation before closing or
switching profiles or adding a new one. Press `n` to add a profile; the shortcut
is shown in the footer, and arrow navigation stops at the last profile. The
accounts sit in a scrollable table (Account, Provider, Plan, Connection) above a
divider naming the selected account and the inspector tabs; only the inspector
body changes with the tab. On narrow panes the Plan column goes first, then
Connection moves under the account name, and below that a single-account
selector replaces the table. On short panes the table shows fewer rows (with a
`1-5 of 9` indicator) before the inspector loses content. Antigravity's
`Google AI` prefix is omitted from the Plan column. The Usage tab shows
remaining-capacity meters, credits and reset grants, and `r` refreshes it.
Usage (`u`) uses `↑`/`↓` to select a profile, `r` to refresh,
`m` for its profile details and `Esc` to close. The profile list stays in provider
and name order under muted provider headings, scrolling to keep the selection
visible. Compact strips connect each profile's rows with an orange Claude,
gray Codex or blue Antigravity rail; `>` marks the selected profile. Both
headline windows show remaining-capacity meters, percentages and their own
automatic reset countdowns. Wide panes put windows side by side; narrow panes
stack them and retain metadata on continuation rows. Antigravity uses the same
plain `5h` and `Week` labels as Claude and Codex for its default usage row, then
shows Claude/GPT models on one separate paired row. Default windows absent from
the reported data show `Unavailable`; model quotas never substitute for them or
appear twice. A blank row separates accounts. Additional limits and
model-specific pools remain visible. Meters are green with at least half left,
yellow below half and red below a quarter, with ASCII glyphs in ASCII mode.

Credit pools show their available balance and reported reset date separately.
Codex reset grants show the usable count and **First expires** deadline, which
is separate from automatic window resets. Balances, credit states, available
reset counts and reset times use distinct theme colors to make values easy to
scan; expiry deadlines use the warning color. Unknown balances and dates stay
unknown; expired and unavailable grants are excluded. Loading and stale labels
keep retained values visible. The Agents tab header, profile details and launch
dialog keep their compact five-hour bars and weekly percentages.
Narrow profile panes use a single column
with the selected profile shown above the content.

An account that needs a sign-in shows `[ Sign in first ]` in place of start.
Signing in or adding an account suspends the TUI and runs the provider's CLI
flow in the terminal, then returns to Bonsai. See [agents.md](agents.md) for
profiles and accounts.

### Usage pane

When the terminal has at least 30 rows and agent accounts exist, a small
bordered **Usage · left** box sits under the worktree list. Its rails and
segmented meters show remaining capacity, ordered Claude, Codex, then Antigravity.
The rails match each provider: orange for Claude, light gray for Codex, and blue
for Antigravity. Expanded meters grow to fill the available row width.
Expanded strips use three lines per account: provider and name, then the two
headline windows with remaining percentages and reset countdowns when available.
Accounts without shared windows show `No shared windows` once; loading and
unavailable states never imply a capacity value.
The pane is display only: it takes no keys, and resizing below 30 rows
hides it and gives the rows back to the list. On short terminals it shrinks to
one line per account with the provider glyph, name, primary-window meter and
percentage, and secondary-window percentage (`W` means Week), then to `+N more`.
Compact rows give spare width to the primary (usually 5h) meter first, then add
and grow a secondary (usually Week) meter when there is enough room for both.
Narrow widths shorten names and meters before sacrificing numeric values;
compact rows omit reset countdowns. Sizing remains automatic and only takes
space from the worktree list; the right-hand content keeps its full height.

Every minute the pane checks for accounts whose data is 3 minutes old or more
and refetches only those. The box title shows the age of the oldest data:
`refreshing…`, `<30s`, `<1m`, `1m`, `2m`, `3m`, then `4m+`. A trailing `!` on an
account marks numbers that are overdue or failed to refresh (the last good
numbers and their capacity warning colors stay visible). Fetched usage is stored
under the user cache directory (`bonsai/agents/usage/`), so a restart reuses
anything fetched less than 3 minutes ago without calling the provider.

Every usage view in the TUI (this pane, the Agents tab header, profiles, the
usage overlay and the new agent dialog) reads the same cache with the same
3-minute window and shows the same numbers for an account. Only `r` in profiles
or the usage overlay skips the window and asks the provider again.

### Keybindings

| Key | Action |
|-----|--------|
| `tab` | Cycle focus between panes |
| `shift+tab` | Cycle the right-pane tabs |
| `enter` | Open a shell in the selected worktree |
| `e` | Open your editor ($VISUAL/$EDITOR) in the selected worktree |
| `n` | New worktree (new branch / existing branch / PR) |
| `ctrl+n` | Create a PR from the selected worktree |
| `v` / `l` / `d` / `P` | Processes / Git Log / Diff / PR tab |
| `g` | Agents tab (when agents are available); then `a` all agents, `1`-`9` / `[` `]` switch session, `enter` terminal mode (every key goes to the agent except `ctrl+]`, press `ctrl+]` to leave focus). On the focused Agents tab, `n` (new agent), `m` (accounts), `u` (usage) and `s` (stop agent) take precedence over the globals with the same letters |
| `i` | Inspector tab (status, last commit, diff, disk, stashes) |
| `b` | Checks tab (GitHub Actions runs for the branch) |
| `o` | Cycle list sort (name / ahead / behind / PR / activity / dirty) |
| `/` | Filter the worktree list |
| `c` | Copy a file from main into the worktree (fuzzy, ranked by how often you copy it) |
| `y` | Yank: copy path / branch / PR URL to the clipboard |
| `s` | Run discovered project commands (Node / Python / Go / Rust-Cargo / Make) |
| `p` | Aliases menu (run one, or `＋ new alias`) |
| `ctrl+p` | Git pull |
| `ctrl+u` | Git push |
| `f` | Git fetch (all remotes) |
| `C` | Git commit (stages all) |
| `r` | Git rebase onto a chosen branch (drops to a real shell for conflicts) |
| `u` | Update branch from base (rebase or merge) |
| `x` | Prune the worktree (with optional PR merge) |
| `X` | Prune all merged worktrees |
| `R` | Refresh worktrees, metrics, and PRs |
| `,` | Preferences (theme, keybindings, defaults) |
| `?` | Keybindings reference (searchable) |
| `q` / `ctrl+c` | Quit |

Every key can be rebound under `keybindings` in `.bonsai.yaml`; the
[annotated example](../.bonsai.yaml) lists the action names. The terminal escape
key `ctrl+]` is fixed; `?` lists it in a separate **Agent terminal (fixed key)**
section.

The navigation status bar follows keyboard focus. With the Agents pane focused,
it shows agent actions and the selected session's Enter action. With the
worktree list focused, it shows how to focus the Agents pane. Profile and usage
views show their own navigation keys.

Unicode provider icons are ✹ for Claude (orange), ❃ for Codex (light gray), and
✦ for Antigravity (blue).

Agent glyphs follow the `agent glyphs` row in preferences (`,`). `ascii` swaps
bonsai's own drawing to ASCII: provider glyphs (`C X A`), the waiting mark (`?`), state dots, picker marks
(`*` running, `+` exited, `x` failed), rules, hint rows and agent overlay borders.
It does not touch the agent's terminal output or the status-bar separators.

### Redesign preview (`--tui-layout redesign`)

An opt-in preview of the redesigned TUI. The shell is a header with the Garden,
Playground, Worktrees, Accounts and Settings tabs and a footer with the current
workspace, focus, key hints and status. **Settings**, **Accounts** and **Garden
setup** have bodies (see [Settings](#settings-in-the-preview),
[Accounts](#accounts-in-the-preview) and [Garden setup](#garden-setup-in-the-preview));
Playground and Worktrees remain placeholders. Garden reads the GitHub login and
repository setup state; Accounts loads agent profiles and usage the first time
it opens. The last workspace is remembered per repository (see Settings). The default
stays the current TUI (`--tui-layout legacy`), which keeps its own preferences
overlay.

```sh
bonsai --tui-layout redesign                  # preview shell
bonsai --config ci.yaml --tui-layout=redesign # with the existing --config; either order
```

The flag takes `legacy` or `redesign` (also as `--tui-layout=<value>`), must come
before any subcommand, and is rejected with one (like `--config`). Any other
value exits with an error before the TUI starts. The terminal must be at least
64x10; below that the preview shows a resize notice.

| Key | Action |
|-----|--------|
| `←`/`→` or `h`/`l` | Previous / next workspace (inside Settings, moves between categories or cycles a value; see below) |
| `tab` / `shift+tab` | Next / previous workspace (the `focus_next` / `focus_prev` bindings) |
| `alt+1` to `alt+5` | Jump to a workspace (bare digits stay reserved for agent sessions) |
| `,` | Jump to Settings (the `prefs` binding) |
| `enter` / `↓` | On the Settings or Accounts tab, enter its body |
| `m` | Open Accounts (the Agents tab's `agent_accounts` binding) |
| `ctrl+k` | Command palette: "Go to <workspace>", "Settings: <category>", "Settings: Edit key bindings", "Accounts: Usage" and "Accounts: Defaults" |
| `?` | Keys that work in the preview |
| `q` / `ctrl+c` | Quit (outside the palette and help dialogs; inside them `esc` closes the dialog) |

The footer shows the effective keys, so `keys:` overrides for the palette, help,
quit and tab actions are reflected. The `ascii` glyph preference keeps all tab
labels and swaps their icons for ASCII. The icons ♧ ▣ ⌘ ◎ ⚙ may render two cells wide in some terminals, which misaligns the header; the ASCII glyph preference is the workaround. Theme presets and `theme.overrides` gain
four roles for the shell: `background`, `surface`, `raised` and `muted`.

#### Garden setup in the preview

Open Garden with `bonsai --tui-layout redesign` to preview and apply a setup
plan. Setup creates the managed labels, a GitHub Project named **Bonsai Garden**,
and the **Stage** and **Bonsai Type** single-select fields, or adds missing fields to a
Project you select. Completed steps are saved if setup stops.

GitHub reserves **Type** for its built-in issue type field. Garden uses
**Bonsai Type** for its `feature`, `bug`, `chore` and `research` options. Saved
setup plans that stopped at **Add field Type** can resume with the corrected
name; completed steps and the original saved plan are preserved.

GitHub Project access needs the `project` scope even when label creation already
works. When setup reports `scope-missing`, run `gh auth refresh -s project`
yourself, then choose **Resume setup**. The refresh key (`R` by default) and
Resume both reread the stored GitHub credentials and identity, so you do not
need to quit Bonsai. Bonsai never runs login or authorization refresh commands.

A `github-graphql` failure means GitHub rejected Bonsai's GraphQL request.
Update Bonsai and resume; if it persists, report the failing step and error
code. Raw GitHub error responses and credentials are never displayed or saved.
Resume rechecks GitHub and keeps completed labels, Projects and fields.

#### Settings in the preview

Settings is reachable with `,`, the Settings tab or the palette, and works without
an agent account or GitHub connection. Navigation has three levels: category,
section, field. `←`/`→` (or `h`/`l`) move between categories at category and
section level and cycle a value at field level. `tab` toggles between sections
and fields, `enter` chooses or edits, and `esc` goes up exactly one level. During
a text edit, `esc` cancels only that edit.

- **General**: Startup (startup workspace, remember per repo); Editor & lists
  (editor, sort, PR status, merge on prune); Repository (confirm destructive
  actions, notifications).
- **Appearance**: theme, surface contrast (layered or flat), icons (unicode or
  ASCII), density.
- **Keys**: one section per keymap group, plus the fixed Terminal row `ctrl+]`.
- **Garden** and **Agents**: the existing repository base ref. Agents also has
  "Account defaults", which opens the Accounts workspace on Defaults. Other rows
  show "Unavailable" with the plan that adds them and have no controls.

Scope and precedence: personal preferences live in
`~/.config/bonsai/state.json` and apply on this machine. They override the
repository `.bonsai.yaml`, which overrides built-in defaults. An unset value
inherits; each field shows its effective value and origin. Repository fields ask
for confirmation, showing the file path, before writing `.bonsai.yaml`; the write
keeps its comments. Personal changes autosave in order. A failed save restores the
last saved state and shows the error. Unknown fields in `state.json` are kept.

Settings adds these `state.json` fields: `prefs.contrast`, `prefs.density`,
`prefs.start_workspace`, `prefs.no_workspace_memory` and a per-repository
`workspaces` map keyed by the canonical local repository path (symlinks resolved).

The startup workspace is the first of: the explicit startup workspace, the
workspace remembered for the repository, then Garden. Turn the memory off with
"Remember per repo" under General.

Key editor: `enter` on a key row captures the next key, `esc` cancels and
`backspace` resets to the default. Captured keys include `q`, `?`, `ctrl+k` and
`tab`; `ctrl+c` still quits.

- Navigation actions are global and conflict with every other scope.
- Worktree, Tabs, Files, Run and Git share one scope. The Agents tab and the
  Processes tab are separate scopes, so a key may be reused across them.
- Always rejected: `esc`, `ctrl+c`, `ctrl+]`, `enter`, `tab`, `shift+tab`, `space`,
  the arrow keys and `alt+1` to `alt+5`. `h`/`j`/`k`/`l` are rejected only for
  Navigation actions.

#### Accounts in the preview

Accounts lists the agent profiles (Claude, Codex, Antigravity) of this user. They
are local and global: the list does not depend on the selected worktree, and the
workspace works without GitHub or terminal (PTY) support. It reuses the legacy
profiles view: a table of profiles above the **Overview**, **Usage** and
**Defaults** sections.

Focus is nested. `enter` or `↓` on the tab opens the profile list; `↑`/`↓` pick a
profile; `enter` moves into the sections; `tab` / `shift+tab` (or `1`, `2`, `3`)
switch section; `esc` goes up one level (section to list, list to the tabs). The
selected profile, section and any unsaved defaults are kept while you visit other
workspaces or reload. Leaving Accounts or changing profile never stops a running
session.

- **Overview**: identity, sign-in method, running sessions and the model and
  effort defaults. `Sign in again` and `Remove account` use the same CLI flows
  as the Agents tab; sign-in runs in the real terminal (the TUI suspends, then
  reloads). Bonsai never refreshes a login by itself. Removal asks first and is
  refused while the profile has running sessions.
- **Usage**: remaining capacity, labeled `N% left` for every window with its
  reset time, plus credits and reset grants when the provider reports them. An
  unavailable window reads "Unavailable", never 0%. A failed refresh keeps the
  last good numbers, marked stale. `r` refreshes the selected profile; `↑`/`↓` scroll when the content is taller than the pane. Usage is
  fetched through the same cache as the legacy TUI, only for the selected profile.
- **Defaults**: the provider-declared launch defaults. "Provider default" means
  inherited; a stored value the provider no longer offers shows as `(unlisted)`.
  `ctrl+s` or `Save defaults` sends only the fields you changed. A failed save is
  shown and the draft stays. Changing profile with unsaved changes asks to
  discard first.

Empty and failed states give a reason (no backend, loading, no profiles yet,
sign-in required, sign-in state unknown, usage unavailable, read failed). `n`
adds a profile.

## CLI

Any argument runs a subcommand instead of the TUI. `bonsai help` prints the full
usage.

```sh
bonsai list                     # list worktrees (branch, path)
bonsai create <branch>          # new worktree on a new branch, prints its path
bonsai create --existing <br>   # new worktree on an existing branch
bonsai create --pr <number>     # new worktree checked out from a GitHub PR
bonsai copy <file> <worktree>   # copy a main-repo file into a worktree
bonsai path <branch|main>       # print a worktree's path (for cd)
bonsai x <alias> [worktree]     # run an alias (default: current directory)
bonsai x --list                 # list available aliases
bonsai alias add <name> <cmd…>  # add a user alias
bonsai alias list               # list aliases
bonsai alias rm <name>          # remove a user alias
bonsai spawn [--wt X] [--restart MODE] <cmd…>  # run a background process
bonsai ps [--wt X] [--all]      # list background processes
bonsai logs <id> [-f] [-n N] [--grep P] [-i]   # view or follow a process log
bonsai grep [-i] [--all] <p> [id]              # search process logs
bonsai kill [--repo R] <id | --all | --wt X>   # stop process(es)
bonsai restart <id>             # restart a process
bonsai attach <id>              # stream a process log (Ctrl-C detaches)
bonsai daemon status|stop       # control the background daemon
bonsai shell-init               # print a shell 'bcd' cd helper
bonsai version (-v)             # print version and build info
bonsai update (-u)              # check or install the latest release
```

Web and agent commands:

```sh
bonsai web [--attach] [--no-open] [--no-setup] [--port N]
bonsai web setup|doctor|status|open|attach|stop
bonsai web logs [api|tunnel] [-f] [-n N] [--grep TEXT] [-i]
bonsai web restart [api|tunnel]

bonsai agent account add <provider> <name>          # new Claude Code, Codex or Antigravity profile
bonsai agent account login <account>                # sign a profile in again
bonsai agent account list                           # profiles, sign-in state and identity
bonsai agent account show <account>                 # one profile with its launch defaults
bonsai agent account set <account> <field>=<value>  # set profile launch defaults
bonsai agent account rename <account> <new-name>
bonsai agent account remove <account>
bonsai agent run <account> [-- <args…>]             # run the provider CLI as that profile
bonsai agent usage [<account>] [--refresh] [--claude-gpt] [--no-progress]
```

See [web.md](web.md) and [agents.md](agents.md) for what these do.

### Jump into a worktree from your shell

A binary can't change its parent shell's directory, so bonsai ships a helper:

```sh
eval "$(bonsai shell-init)"     # add to ~/.zshrc or ~/.bashrc
bcd feat/login                  # cd into that worktree
bcd                             # cd back to main
```

On Windows the installer adds a `bcd.bat` helper next to `bonsai.exe`.

## Configuration

bonsai reads **`.bonsai.yaml`** from your repo root. The
[annotated example](../.bonsai.yaml) documents every field. In short:

```yaml
upstream: origin/main          # base ref for ↑ahead/↓behind and prune's merge target

hooks:
  on_worktree_create:
    - "cp {main}/.env {new_worktree}/.env"
    - "npm install"
  on_worktree_delete:
    - "echo 'removing {worktree_name}'"

aliases:
  - name: dev
    command: "npm run dev"
```

### Hook and alias variables

Use `{name}` inside hook commands and alias commands. Each is also exported to
hooks as `$BONSAI_<NAME>` (uppercased).

| Variable | Value |
|----------|-------|
| `{new_worktree}` / `{worktree}` | Absolute path of the worktree |
| `{worktree_name}` | Worktree directory name (e.g. `repo-feat-login`) |
| `{main}` / `{main_branch_path}` | Absolute path of the main worktree |
| `{branch}` | Branch name |
| `{base_branch}` | Base branch from `upstream` (`origin/main` becomes `main`) |
| `{repo}` | Repository directory name |
| `{pr_number}` | Connected PR number, or empty |

User aliases you add in-app or via `bonsai alias add` persist to
`~/.config/bonsai/state.json` (which also tracks file-copy frequency for fuzzy
ranking). Aliases in `.bonsai.yaml` are read-only project defaults.

## How prune works

`x` (prune) is **local teardown** and is destructive. The confirmation modal
previews the exact pipeline, for example:

```
merge PR #20 to main → run on_worktree_delete → delete worktree → delete branch
```

- The **merge** step is **off by default**; toggle it with `space`. It only
  appears for branches with a connected PR and runs `gh pr merge`.
- `delete worktree` uses `--force`; `delete branch` uses `-D`. **Unpushed
  commits and uncommitted changes are lost.**
- It never touches the remote unless you explicitly enable merge.

## Versions and updates

```sh
bonsai -v                  # also --version or version
bonsai -u                  # also --update or update
bonsai update --check      # check the latest stable GitHub Release
bonsai update              # prompt to verify and install it
```

These commands work outside a Git repository. Updates support Linux, macOS, and
Windows on amd64 and arm64. The executable's directory must be writable;
symlinked installations update the resolved executable. Restart Bonsai after
updating. On Windows the old executable remains as `bonsai.exe.old` until the
next update. A crashed updater may leave a `.update-lock` beside the executable;
remove it only after confirming no updater is running.

Release builds include their version, commit, and build date. Source builds
report `dev` and do not replace themselves or display automatic update prompts.
Periodic update checks run in the background (once every 24 hours) with a short
timeout and cache the result in the OS user cache under
`bonsai/update_state.json`. Interactive sessions prompt before installing, while
non-interactive environments (like CI or piped commands) log a single-line
notice. Network failures never prevent startup; explicit `bonsai --update`
bypasses the cache.

## Installing

The install scripts download the latest GitHub Release, verify its SHA256
checksum and install the binary. Neither needs Go.

- `install/install.sh` (Linux and macOS) installs to `/usr/local/bin`, or
  `~/.local/bin` if that is not writable. Override with `PREFIX` (the binary goes
  to `$PREFIX/bin`). It needs `curl`, `tar` and either `sha256sum` or `shasum`.
- `install/install.ps1` (Windows, PowerShell 5.1 or later; `install\install.bat`
  calls it) installs to `%USERPROFILE%\go\bin` by default, honoring `PREFIX`,
  `GOBIN` or `GOPATH` when set, and also installs the `bcd.bat` helper.

Release archives include the complete production web UI and need no Node or Go
at runtime. `go install github.com/Tiago-0liveira/bonsai@latest` and untagged
`go build` keep the TUI/CLI but serve an explanatory placeholder at `/app`; to
build the web app from source see [CONTRIBUTING.md](../CONTRIBUTING.md) and
[development.md](development.md).
