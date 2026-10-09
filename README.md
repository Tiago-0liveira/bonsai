<div align="center">

```
             &&& &&  &&&
          &&&\/&\|&()|/@,&&&
          &&\/(/&/&||/&_/)_&&
         &&/&_&&/|&&/&/__%_/_&
          &&& &&|&&|/&&_%_&&&&
            &---&&\&\|&/&--&
                 \|||/
                  |||
                  |||
                \ ||| /
            ~-=-~^-'^`-~-=-~
```

# bonsai

**A terminal UI for growing and pruning git worktrees.**

Manage worktrees, sync files, run scripts, and drive git — without leaving your keyboard.

</div>

---

## Why

Juggling git worktrees by hand is tedious: creating them, copying over untracked files like `.env`, running dev servers per branch, tracking which one has an open PR, and cleaning them up. **bonsai** puts all of that in one fast TUI — and every feature is also a plain CLI command you can script.

## Features

- 🌳 **Worktree list** with ahead/behind arrows (`↑N ↓M`), `#PR` badges auto-detected via `gh`, dirty change counts (`●N ?M`), CI dots, and 6 sort modes.
- 🔍 **Inspector tab** per worktree: working-tree status, last commit, diff-vs-base summary, disk usage, and stash count.
- 🚦 **Checks tab** with the branch's GitHub Actions runs, and a full **PR tab** (detail, reviews, merge/approve/close).
- 📄 **Diff tab**: files changed vs base with per-file diffs.
- 🌐 **Live dev-server URLs** detected in process output (`http://localhost:…` shown next to each process).
- 📋 **Yank menu** (`y`): copy the worktree path, branch name, or PR URL to your clipboard.
- ✨ **Create worktrees** from a new branch, an existing branch, or a GitHub PR.
- 📋 **Copy files** from main into a worktree via fuzzy finder — ranked by how often you copy them.
- 🔧 **Run project commands** from Node, Python, Go, Rust/Cargo, and Make plus **custom aliases** as background processes — many per worktree, switchable.
- 🪝 **Lifecycle hooks** (`on_worktree_create` / `on_worktree_delete`) with `{variable}` and `$BONSAI_*` substitution.
- 🔀 **Git ops** inline: pull, push, commit, rebase (drops to a real shell for conflict resolution).
- 🗑️ **Prune** with an optional **merge-PR-first** step and a clear preview of exactly what will happen.
- ⚙️ **Edit `.bonsai.yaml` from the command palette** — every setting, with descriptions and examples, applied live.
- 🖥️ **Everything works from the CLI too** — `bonsai create`, `bonsai copy`, `bonsai x <alias>`, and more.

## Install

### Script (recommended)

```sh
./install/install.sh
```

Downloads the latest GitHub Release, verifies its SHA256 checksum, and installs it to `/usr/local/bin` (or `~/.local/bin` if that isn't writable). Override the location with `PREFIX`:

```sh
PREFIX="$HOME/.local" ./install/install.sh
```

On Windows, run `install\install.bat` (which calls `install.ps1`), or run
`./install/install.ps1` from PowerShell. It installs to `%USERPROFILE%\go\bin`
by default, honoring `PREFIX`, `GOBIN`, or `GOPATH` when set, and installs the
`bcd.bat` helper.
Neither installer requires Go. Linux/macOS need `curl`, `tar`, and either
`sha256sum` or `shasum`; Windows uses PowerShell 5.1 or later.

### With Go

```sh
go install github.com/Tiago-0liveira/bonsai@latest
```

### From source

```sh
go build -o bonsai .
```

## Versions and updates

```sh
bonsai -v                  # also --version or version
bonsai -u                  # also --update or update
bonsai update --check      # check the latest stable GitHub Release
bonsai update              # prompt to verify and install it
```

These commands work outside a Git repository. Updates support Linux, macOS,
and Windows on amd64 and arm64. The executable's directory must be writable;
symlinked installations update the resolved executable. Restart Bonsai after
updating. On Windows the old executable remains as `bonsai.exe.old` until the
next update. A crashed updater may leave a `.update-lock` beside the executable;
remove it only after confirming no updater is running.

Release builds include their version, commit, and build date. Source builds
report `dev` and do not replace themselves or display automatic update prompts.
Periodic update checks run in the background (once every 24 hours) with a short
timeout and cache the result in the OS user cache under `bonsai/update_state.json`.
Interactive sessions prompt before installing, while non-interactive environments
(like CI or piped commands) log a single-line notice.
Network failures never prevent startup; explicit `bonsai --update` bypasses the cache.

## CI and releases

Pull requests targeting `main` run formatting, module checks, vet, builds and
tests on Linux/macOS/Windows, Linux race tests, and a GoReleaser snapshot build.
The `CI passes` aggregate must pass before merging into protected `main`.

A merged PR triggers a queued release, defaulting to the next patch version.
Use at most one label: `release:patch`, `release:minor`, `release:major`, or
`release:none`. With no existing stable tags, the first patch is `v0.0.1`.
Release jobs check out the exact merge commit, create a tag, build all six
platform archives and `checksums.txt`, then publish the completed draft.
They run only for merged PRs, including fork PRs; direct pushes and manual tags
do not trigger releases. A failed run can be rerun: it reuses that merge's tag
and rebuilds an incomplete draft. Published releases are left intact.

```sh
goreleaser release --snapshot --clean  # local release dry-run
```

## Requirements

- **Go 1.26+** (to build)
- **git** on your `PATH`
- **[`gh`](https://cli.github.com/)** (optional) — only needed for PR badges, creating worktrees from PRs, and merge-on-prune. Everything else works without it.

## Quick start

```sh
cd your-repo
bonsai
```

Press `n` to create a worktree, `enter` to drop into a shell in the selected one, `?` for the full keymap, `q` to quit.

## Keybindings

| Key | Action |
|-----|--------|
| `tab` | Cycle focus between panes |
| `shift+tab` | Cycle the right-pane tabs |
| `enter` | Open a shell in the selected worktree |
| `e` | Open your editor ($VISUAL/$EDITOR) in the selected worktree |
| `n` | New worktree (new branch / existing branch / PR) |
| `ctrl+n` | Create a PR from the selected worktree |
| `v` / `l` / `d` / `P` | Processes / Git Log / Diff / PR tab |
| `i` | Inspector tab (status, last commit, diff, disk, stashes) |
| `b` | Checks tab (GitHub Actions runs for the branch) |
| `o` | Cycle list sort (name / ahead / behind / PR / activity / dirty) |
| `/` | Filter the worktree list |
| `c` | Copy a file from main into the worktree (fuzzy) |
| `y` | Yank: copy path / branch / PR URL to the clipboard |
| `s` | Run discovered project commands (Node / Python / Go / Rust-Cargo / Make) |
| `p` | Aliases menu (run one, or `＋ new alias`) |
| `ctrl+p` | Git pull |
| `ctrl+u` | Git push |
| `f` | Git fetch (all remotes) |
| `C` | Git commit (stages all) |
| `r` | Git rebase onto a chosen branch |
| `u` | Update branch from base (rebase or merge) |
| `x` | Prune the worktree (with optional PR merge) |
| `X` | Prune all merged worktrees |
| `R` | Refresh worktrees, metrics, and PRs |
| `,` | Preferences (theme, keybindings, defaults) |
| `?` | Keybindings reference (searchable) |
| `q` / `ctrl+c` | Quit |

## CLI

Any argument runs a subcommand instead of the TUI:

```sh
bonsai list                     # list worktrees (branch, path)
bonsai create <branch>          # new worktree on a new branch → prints its path
bonsai create --existing <br>   # new worktree on an existing branch
bonsai create --pr <number>     # new worktree checked out from a GitHub PR
bonsai copy <file> <worktree>   # copy a main-repo file into a worktree
bonsai path <branch|main>       # print a worktree's path (for cd)
bonsai x <alias> [worktree]     # run an alias (default: current directory)
bonsai x --list                 # list available aliases
bonsai alias add <name> <cmd…>  # add a user alias
bonsai alias list               # list aliases
bonsai alias rm <name>          # remove a user alias
bonsai shell-init               # print a shell 'bcd' cd helper
bonsai serve                    # start/reuse the secured loopback API and attach
bonsai serve -d                 # start/reuse, verify readiness, then detach
bonsai serve status             # inspect the current worktree's API process
bonsai serve logs --process api
bonsai serve restart [api]
bonsai serve stop
bonsai version (-v)             # print version and build info
bonsai update (-u)              # check or install latest release
bonsai help                     # full usage
```

### Jump into a worktree from your shell

A binary can't change its parent shell's directory, so bonsai ships a helper:

```sh
eval "$(bonsai shell-init)"     # add to ~/.zshrc or ~/.bashrc
bcd feat/login                  # cd into that worktree
bcd                             # cd back to main
```

## Configuration

bonsai reads **`.bonsai.yaml`** from your repo root. See the [annotated example](.bonsai.yaml) — it documents every field. In short:

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

### Hook & alias variables

Use `{name}` inside hook commands and alias commands. Each is also exported to hooks as `$BONSAI_<NAME>` (uppercased).

| Variable | Value |
|----------|-------|
| `{new_worktree}` / `{worktree}` | Absolute path of the worktree |
| `{worktree_name}` | Worktree directory name (e.g. `repo-feat-login`) |
| `{main}` / `{main_branch_path}` | Absolute path of the main worktree |
| `{branch}` | Branch name |
| `{base_branch}` | Base branch from `upstream` (`origin/main` → `main`) |
| `{repo}` | Repository directory name |
| `{pr_number}` | Connected PR number, or empty |

User aliases you add in-app or via `bonsai alias add` persist to `~/.config/bonsai/state.json` (which also tracks file-copy frequency for fuzzy ranking). Aliases in `.bonsai.yaml` are read-only project defaults.

## How prune works

`x` (prune) is **local teardown** and is destructive. The confirmation modal previews the exact pipeline, e.g.:

```
merge PR #20 to main → run on_worktree_delete → delete worktree → delete branch
```

- The **merge** step is **off by default** — toggle it with `space`. It only appears for branches with a connected PR and runs `gh pr merge`.
- `delete worktree` uses `--force`; `delete branch` uses `-D`. **Unpushed commits and uncommitted changes are lost.**
- It never touches the remote unless you explicitly enable merge.

## Architecture

Strict split between presentation and core:

- **`internal/ui`** — all Bubble Tea (model / update / view / components). No `os/exec`, file I/O, or raw git.
- **`internal/core`** — `git`, `gh`, `config`, `exec`, `fs`, `pkgmgr`, `notify`, `clipboard`. Plain Go types and errors, independently testable.
- **`internal/cli`** — the non-interactive subcommands, sharing the same core.

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Lip Gloss](https://github.com/charmbracelet/lipgloss), [Viper](https://github.com/spf13/viper), and [sahilm/fuzzy](https://github.com/sahilm/fuzzy).

## License

MIT

## Web Git backend

The hardened loopback API and the separate GitHub webhook relay are documented in [docs/git-backend.md](docs/git-backend.md). The internet relay has no route to the local daemon or repositories.


### Local serve API

`bonsai serve` is a thin client for one daemon-supervised browser API bound to
`127.0.0.1:7001` by default. It does not start a webhook listener, Vite server,
public tunnel, or cloud daemon bridge. Detaching with `q` or Ctrl+C leaves the
API running; `X` in the serve view or `bonsai serve stop` explicitly stops it.

Production browser access is restricted to one exact HTTPS frontend origin and
exact Host `127.0.0.1:7001`. The frontend defaults to
`https://app.bonsai.dev` and can be selected with `BONSAI_FRONTEND_ORIGIN`. Normal
`bonsai serve` never enables development origins and never supervises a local
webhook relay, Vite server, or tunnel. Start it, then open the configured hosted frontend's `/app` route. The API never uses browser cookies for local authorization.

Contributors can reproduce the production topology entirely on loopback with
the hidden `bonsai __serve-dev-stack` harness. It is intentionally absent from
normal help and quick-start documentation; see [docs/development.md](docs/development.md).

The browser creates a short-lived local capability with `POST /api/session` and
sends it in `X-Bonsai-Session` for privileged requests. The token is held only
in server/browser memory and is invalidated when the local API restarts. Git
mutations continue through the daemon's structured Git bridge, while local
GitHub operations use the installed `gh` CLI.


### Internet GitHub relay

`cmd/bonsai-relay` serves the small cloud-only surface at `api.bonsai.dev`:
GitHub OAuth/session routes, `POST /webhooks/github`, `GET /events`, and
`GET /healthz`. Webhook signatures are verified before JSON normalization,
delivery IDs are durably deduplicated, and SSE is scoped to repositories the
GitHub-authenticated relay session may access.

The relay sends normalized notification metadata only. It cannot access local
files, worktrees, processes, the daemon, or local Bonsai session capabilities.
Browser relay events invalidate local state; the browser then refreshes canonical
state from the loopback API.

### Browser project folders

After connecting the browser to `bonsai serve`, choose the local folders Bonsai
should scan. You can select a suggested directory or enter an absolute path
(or `~/projects`), then confirm **Add folder**. Nothing is registered automatically.
The same editor is available from **Settings**. Folder settings belong to your
local user and survive browser and backend restarts.

Discovery includes each selected folder and up to four levels of descendants,
skipping dependency, build and cache directories and directory symlinks. You can
explicitly select a symlinked folder. Overlapping folders are supported; separate
clones remain separate projects, even when they share a GitHub remote. Linked
worktrees outside the selected folders remain visible through their repository.
Unavailable folders and scan limits appear in Settings; select a deeper folder
when a scan is truncated. The catalog reconciles every 30 seconds.

New browser worktrees default to
`<owning-folder>/.bonsai/worktrees/<project-id>/<branch-hash>`. An explicit
`.bonsai.yaml` `worktree.root` must resolve within a configured folder. Settings
changes affect future creation without a daemon restart. The CLI/TUI continues
using its existing `worktree.root` and `worktree.path_template` behavior.
Removing a folder never deletes files or worktrees, stops processes, or shuts
down the hosting daemon. Existing metadata and worktree IDs are preserved.

Existing users must confirm their project folders once. `bonsai serve` still
starts from a Git repository, but the browser catalog may be empty until folders
are configured. There is no global discovery daemon or cloud filesystem scan.

### Claude Code profiles

Install `claude` (Claude Code), then add one Bonsai profile per Claude account.
Each profile owns a persistent config directory (`CLAUDE_CONFIG_DIR`) that all
of its sessions share; your real `~/.claude` is never written.

```sh
bonsai agent account add claude work            # login mode (default)
bonsai agent account add claude ci --auth token # long-lived token mode
echo "$TOKEN" | bonsai agent account add claude ci --token-stdin
bonsai agent account list                       # shows auth mode and identity
bonsai agent run work -- --model opus           # extra args go to claude
bonsai agent account remove work                # logs the profile out, deletes it
```

- **login** runs `claude auth login` inside the profile. All features work.
- **token** runs `claude setup-token` (or reads `--token-stdin`) and stores the
  one-year token 0600 in Bonsai's data directory; it is injected as
  `CLAUDE_CODE_OAUTH_TOKEN` at launch. A token can only make model requests, so
  it cannot use Remote Control or claude.ai connectors.
- **Seeding.** A new profile starts from a one-time snapshot of your own setup:
  `settings.json`, `CLAUDE.md`, `keybindings.json`, `agents/`, `commands/`,
  `skills/`, `output-styles/` and your user-level MCP servers. Credentials,
  history, sessions, plugins and API-key settings are never copied, and symlinks
  are skipped. Bonsai prints what it copied. Use `--no-seed` to skip it or
  `--seed-from <dir>` to seed from another config directory.
- **Isolation.** Sessions run with every inherited `CLAUDE*` and `ANTHROPIC_*`
  variable removed, so a profile never silently runs as another identity.
  `HOME` is unchanged, so git, gh and ssh keep working.
- **Concurrency caveat.** Several sessions of one *login* profile refresh the
  same OAuth login, and Claude Code can occasionally lose that race and ask for
  `/login` again. Token mode has no refresh and is race-free.
- **Token exposure.** In token mode the token is in the session's environment
  (that is how Claude Code reads it). Bonsai also sets
  `CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1` to keep it out of the agent's shell
  commands, but that flag was not confirmed against a real session, so treat
  commands the agent runs as able to see the token.
- **Seeding.** Seeding copies MCP server definitions as they are, including any
  `env` or header secrets in them, into the profile (mode 0600). Symlinked files
  and directories in the source are skipped and reported. Login-pinning settings
  (`forceLoginMethod`, `forceLoginOrgUUID`) and credential helpers are dropped.
- **Usage.** `bonsai agent usage` shows the 5-hour and weekly utilization (and
  per-model weekly limits when present) of login profiles. Claude has no
  documented endpoint for this, so Bonsai reads the one Claude Code's own
  `/usage` view uses (`/api/oauth/usage`). It can change or disappear without
  notice and is best effort. Bonsai only reads the profile's stored login and
  **never refreshes it** (a refresh would log out running sessions), so a profile
  whose login expired shows an error until a session refreshes it. Token
  profiles are shown as `n/a` because long-lived tokens cannot read usage, and so
  are macOS profiles (the login lives in the Keychain). Results are cached for
  5 minutes; `--refresh` bypasses the cache.
- Requires Claude Code 2.1.295 or newer.

### Antigravity in the web workspace

Install `agy`, then add a Bonsai profile:

```sh
bonsai agent account add antigravity personal
```

Connect the web app to `bonsai serve`, select a worktree, choose **Start agent**,
and select your profile. The terminal uses that profile's existing settings;
enter instructions directly in it. Codex is not available yet.

Closing the dock or reloading the browser detaches without stopping the agent.
Use **Stop agent** to terminate it and reconcile profile credentials. Sessions
last only while the local API runs; restarting Bonsai does not restart agents.
Recent output is bounded to 2 MiB and may show a replay-gap notice after a long
session. Interactive PTYs require a supported Unix platform; Windows support is
not yet available. See [development details](docs/development.md#antigravity-web-terminals).
