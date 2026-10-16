<div align="">

```
          .&&&&&&&&&&.
       .&&&&&&&&&&&&&&&&.        .&&&&&.
        '&&&&&&&&&&&&&&'     .&&&&&&&&&&&.
             '-.\ /.-'        '&&&&&&&&&'
   .&&&&&.       ) (       _.-'                 _                           _
 .&&&&&&&&&.    /  /   _.-'                    | |__   ___  _ __  ___  __ _(_)
  '&&&&&&&'-._ (  (_.-'                        | '_ \ / _ \| '_ \/ __|/ _` | |
              '-\  \                           | |_) | (_) | | | \__ \ (_| | |
                 )  )                          |_.__/ \___/|_| |_|___/\__,_|_|
          _______/____\_______
          \__________________/
             \_/        \_/
```

# bonsai

**Grow and prune git worktrees, run AI agents across them. In a local web workspace, a TUI or a plain CLI.**

[![CI](https://github.com/Tiago-0liveira/bonsai/actions/workflows/ci.yml/badge.svg)](https://github.com/Tiago-0liveira/bonsai/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/Tiago-0liveira/bonsai)](https://github.com/Tiago-0liveira/bonsai/releases/latest)
[![Go version](https://img.shields.io/github/go-mod/go-version/Tiago-0liveira/bonsai)](go.mod)
[![License: MIT](https://img.shields.io/github/license/Tiago-0liveira/bonsai)](LICENSE)

</div>

## Install

```sh
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/Tiago-0liveira/bonsai/main/install/install.sh | bash
```

```powershell
# Windows (PowerShell 5.1+)
irm https://raw.githubusercontent.com/Tiago-0liveira/bonsai/main/install/install.ps1 | iex
```

Both download the latest GitHub Release, verify its SHA256 checksum and need no
Go. Release binaries embed the full web UI. Prefer Go? `go install
github.com/Tiago-0liveira/bonsai@latest` gives you the TUI and CLI, but `/app`
serves only a placeholder (no embedded web UI; see [Building](#building-from-source)).

## What you can do

### 🌳 Manage git worktrees

- **Create** a worktree from a new branch, an existing branch or a GitHub PR, and
  **copy untracked files** (`.env` and friends) into it via a fuzzy finder.
- **Lifecycle hooks** (`on_worktree_create` / `on_worktree_delete`) and
  **aliases**: run project commands (Node, Python, Go, Rust, Make) or your own as
  background processes, many per worktree, with live dev-server URLs next to each.
- See **PR and CI status**, ahead/behind, dirty counts and a **diff** against base
  for every worktree at a glance.
- **Prune** with a clear preview of exactly what will happen, optionally merging
  the PR first.

### 🖥️ Three interfaces, one core

- **Web workspace**: `bonsai web` serves a local app at
  `http://127.0.0.1:7001/app`, embedded in release binaries. No hosted service.
- **TUI**: `bonsai` inside a repo, with a searchable keymap (`?`) and an Agents tab (`g`, see [docs/agents.md](docs/agents.md)).
- **CLI**: every feature is also a plain, scriptable command.

### ⚙️ Background supervisor

`bonsai web` starts a background daemon and returns. Watch it with
`bonsai web --attach` (or `bonsai web attach`) for live logs; detaching leaves it
running. Manage it with `status`, `logs`, `restart`, `stop` and `doctor`.

### 🤖 AI agents

- Integrates **Claude Code**, **Codex** and **Antigravity**.
- Several **profiles (accounts)** per provider, each with an isolated config
  directory, so accounts never leak into each other.
- Run **many agent sessions at once** across worktrees, in the web terminal dock
  or with `bonsai agent run`.
- Per-profile **launch defaults** (model, effort, permissions) and an **Agents
  tab** in the web app to manage profiles, sign-ins and sessions.
- `bonsai agent usage` shows **plan limits** (5-hour and weekly windows, credits,
  resets) for **every profile of every provider at once**, with live progress.

Details: [docs/agents.md](docs/agents.md).

### 🔄 Also

- **Live GitHub updates**, optional: a webhook through a Cloudflare, ngrok or
  Tailscale tunnel replaces polling. Only the webhook receiver is ever exposed
  ([docs/web.md](docs/web.md#updates-and-exposure)).
- **Self-update**: `bonsai update` verifies and installs the latest release.

## Quick start

```sh
bonsai web                      # first run: pick project folders, then opens the UI

cd your-repo
bonsai                          # TUI: n new worktree, enter shell, ? keymap, q quit
bonsai create --pr 42           # CLI: worktree from a GitHub PR, prints its path
bonsai agent account add claude work   # then start an agent from any worktree
```

For scripts: `bonsai web --no-open --no-setup`.

## CLI cheat sheet

```sh
# worktrees
bonsai list                         # worktrees (branch, path)
bonsai create <branch>              # new branch (--existing <br> | --pr <n>)
bonsai copy <file> <worktree>       # copy a main-repo file into a worktree
bonsai path <branch|main>           # print a path (pair with `bcd`, see docs)
bonsai x <alias> [worktree]         # run an alias (x --list to see them)

# web app
bonsai web [--attach]               # start in the background / follow live logs
bonsai web status|logs|restart|stop|doctor|setup

# agents
bonsai agent account add <provider> <name>    # claude | codex | antigravity
bonsai agent account list
bonsai agent run <account> [-- <args…>]
bonsai agent usage [--refresh]

# maintenance
bonsai update [--check]             # self-update
bonsai help                         # full usage
```

Full reference, keybindings and the `bcd` shell helper: [docs/tui-cli.md](docs/tui-cli.md).

## Configuration

Drop an optional **`.bonsai.yaml`** in your repo root:

```yaml
upstream: origin/main          # base ref for ahead/behind and prune's merge target

hooks:
  on_worktree_create:
    - "cp {main}/.env {new_worktree}/.env"
    - "npm install"

aliases:
  - name: dev
    command: "npm run dev"
```

Every field is documented in the [annotated `.bonsai.yaml`](.bonsai.yaml); hook
variables are listed in [docs/tui-cli.md](docs/tui-cli.md#hook-and-alias-variables).

## Documentation

| Doc | What is in it |
| --- | --- |
| [docs/web.md](docs/web.md) | Running the web app: setup, project folders, settings, updates and tunnels, Agents tab, recovery |
| [docs/agents.md](docs/agents.md) | Claude Code, Codex and Antigravity profiles, seeding, isolation, usage, defaults |
| [docs/tui-cli.md](docs/tui-cli.md) | TUI keybindings, CLI reference, configuration, hooks, prune, updates |
| [docs/architecture.md](docs/architecture.md) | How the local web stack fits together |
| [docs/git-backend.md](docs/git-backend.md) | Local API security model, webhook receiver, Git backend |
| [docs/development.md](docs/development.md) | Contributor stack, ports, provider contract, validation |
| [docs/releasing.md](docs/releasing.md) | CI, release builds and validation |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Contributing checklist |

## Requirements

- **git 2.35+** on your `PATH`.
- **[`gh`](https://cli.github.com/)** (optional): PR badges, worktrees from PRs,
  merge-on-prune and GitHub data in the web app. Everything else works without it.
- **Agents** (optional): Claude Code 2.1.295+ (`claude`), Codex CLI 0.162.1+
  (`codex`) and/or Antigravity (`agy`). Interactive agent terminals in the web
  app are not available on Windows yet.
- Go 1.26+ only if you build from source.

## Building from source

```sh
go build -o bonsai .            # TUI + CLI; /app shows a placeholder
```

To embed the web app, build the frontend first (Node 22.12.0, pnpm 12.9.1):

```sh
pnpm --dir web install --frozen-lockfile && pnpm --dir web build
go build -tags embedui -o bonsai .
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full test and lint workflow.

## License

[MIT](LICENSE)
