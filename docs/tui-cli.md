# TUI, CLI and configuration

Reference for the terminal side of Bonsai: the interactive TUI (`bonsai` inside
a repository), the plain CLI subcommands, project configuration, hooks, prune and
updates. For the browser workspace see [web.md](web.md); for AI agents see
[agents.md](agents.md).

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
glyph and count, plus `!` for an unseen failure. It shows the selected session's
live terminal read-only: you can watch the agent. The title row (`AGENTS · <branch>`)
appears when the terminal is tall enough. A header shows the session's provider,
profile, model and state, with `· read-only` when another client is controlling it.
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
stop is on the tab and in the `ctrl+]` menu only. Profiles, usage, the session
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
The bottom status bar reads `Ctrl+] Bonsai command prefix` in terminal mode, or
`Ctrl+] t take control · Ctrl+] d detach` while observing another client's session.
Only `ctrl+]` is handled by Bonsai; it opens the **BONSAI CONTROL** strip:

| After `ctrl+]` | Action |
|----------------|--------|
| `d` | Detach: leave terminal mode and keep showing the agent read-only |
| `a` | Open the agents picker |
| `t` | Take control (only when another client is the writer) |
| `u` | Usage |
| `m` | Accounts |
| `s` | Stop the selected agent (asks first) |
| `esc` | Back to the agent |
| `ctrl+]` | Send a literal `ctrl+]` (0x1d) to the agent |

Any other key after `ctrl+]` closes the strip and is not forwarded to the agent.

If another client is the writer, the agent is read-only here: the banner reads
`Controlled by another client` and keys are swallowed. Press `ctrl+]` then `t` to
take control. If another client takes control away, the hint row reads
`Control taken by another client  ·  Ctrl+] t Take back`; `ctrl+] t` takes it back.

Press `a` on the Agents tab to open the session picker. It lists the worktree's
sessions as one flat list, running ones first, then ended ones, marked `●` running,
`✓` exited and `✗` failed. It opens even when there are no sessions. `Enter` opens
the chosen session. Opening the picker clears the `!` failure markers on that
worktree's rows, so the failures stay listed here with `✗`.

The new-agent dialog (`n`) selects a profile with `↑`/`↓` (or `j`/`k`). `Tab`
and `Shift+Tab` move through the name, model, effort, other launch options and
buttons. Within the options, `↑`/`↓` moves between fields, `←`/`→` changes a value,
and `Enter` opens a choice list. `Esc` dismisses that list before closing the
dialog. `ctrl+s` starts the agent. Model and effort start from the selected
profile's defaults; edits apply only to this new agent unless **Save profile
defaults** is checked. Effort choices follow the selected model's capabilities.

In profiles (`m`), `Tab` and `Shift+Tab` move focus between the profile list,
section tabs and details. Use `↑`/`↓` to select a profile, `←`/`→` to switch the
focused section tabs, and `Enter` to enter the details or edit a field. `1`/`2`/`3`
jump directly to Overview, Usage or Defaults. `Esc` returns focus to profiles;
press it again to close. Unsaved defaults require confirmation before closing or
switching profiles. Usage (`u`) uses `↑`/`↓` to select a profile, `r` to refresh,
`m` for its profile details and `Esc` to close. The profile list stays in provider
and name order while the selection moves, scrolling to keep it visible.
Five-hour limits use remaining-capacity bars; weekly and other limits use
percentages. Both are green with at least half left, yellow below half and red
below a quarter. Antigravity uses the same 5-hour and weekly summary as Claude
and Codex, with its other pools available in profile usage details.
Narrow profile and launch panes use a single column
with the selected profile shown above the content.

An account that needs a sign-in shows `[Sign in first]` in place of start.
Signing in or adding an account suspends the TUI and runs the provider's CLI
flow in the terminal, then returns to Bonsai. See [agents.md](agents.md) for
profiles and accounts.

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
| `g` | Agents tab (when agents are available); then `a` all agents, `1`-`9` / `[` `]` switch session, `enter` terminal mode (every key goes to the agent except `ctrl+]`, see the prefix table above). On the focused Agents tab, `n` (new agent), `m` (accounts), `u` (usage) and `s` (stop agent) take precedence over the globals with the same letters |
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
[annotated example](../.bonsai.yaml) lists the action names. The keys after
`ctrl+]` are fixed and not rebindable; `?` lists them in a separate **Agent
terminal (ctrl+] first, fixed keys)** section.

The navigation status bar shows `tab focus · g agents · ? keys` on the Agents tab,
with a state prefix when it applies: `connecting…`, `enter retry` after a failed
attach, or `n new · a all` for an ended session. `ctrl+]` is not advertised there.

Agent glyphs follow the `agent glyphs` row in preferences (`,`). `ascii` swaps
bonsai's own drawing to ASCII: provider glyphs (`C X A`), state dots, picker marks
(`*` running, `+` exited, `x` failed), rules, hint rows and agent overlay borders.
It does not touch the agent's terminal output or the status-bar separators.

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
