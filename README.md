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
- 🔧 **Run package scripts** and **custom aliases** as background processes — many per worktree, switchable.
- 🪝 **Lifecycle hooks** (`on_worktree_create` / `on_worktree_delete`) with `{variable}` and `$BONSAI_*` substitution.
- 🔀 **Git ops** inline: pull, push, commit, rebase (drops to a real shell for conflict resolution).
- 🗑️ **Prune** with an optional **merge-PR-first** step and a clear preview of exactly what will happen.
- 🖥️ **Everything works from the CLI too** — `bonsai create`, `bonsai copy`, `bonsai x <alias>`, and more.

## Install

### Script (recommended)

```sh
./install.sh
```

Builds the binary and installs it to `/usr/local/bin` (or `~/.local/bin` if that isn't writable). Override the location with `PREFIX`:

```sh
PREFIX="$HOME/.local" ./install.sh
```

### With Go

```sh
go install github.com/Tiago-0liveira/bonsai@latest
```

### From source

```sh
go build -o bonsai .
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
| `n` | New worktree (new branch / existing branch / PR) |
| `ctrl+n` | Create a PR from the selected worktree |
| `v` / `l` / `d` / `P` | Processes / Git Log / Diff / PR tab |
| `i` | Inspector tab (status, last commit, diff, disk, stashes) |
| `b` | Checks tab (GitHub Actions runs for the branch) |
| `o` | Cycle list sort (name / ahead / behind / PR / activity / dirty) |
| `/` | Filter the worktree list |
| `c` | Copy a file from main into the worktree (fuzzy) |
| `y` | Yank: copy path / branch / PR URL to the clipboard |
| `s` | Run a `package.json` script |
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
