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
- **Garden board and table** in the redesign preview show verified approved work as state columns or sortable rows (`v` toggles, configurable as `garden_view`) with scoped filters and read-only issue documents ([Garden guide](docs/garden.md)).
- **Garden Drafts** in the redesign preview keeps unvalidated ideas visible across stage changes, with capture protected against duplicate retries across Bonsai processes ([Drafts](docs/garden.md#drafts)). Validating a draft's specification (`v` in its document) records one verified approval of the exact revision and moves the same Issue to Approved work, without starting any agent ([gate one](docs/garden.md#validate-a-specification-gate-one)). An opened Approved Issue (`p`) offers an explicit, local **Request AI plan**: a read-only planner session bound to that specification and the checkout, with no worktree, available only for providers that enforce read-only access (Claude; Codex and Antigravity are unavailable) ([read-only planner](docs/garden.md#request-an-ai-plan-read-only-planner), [provider evidence](docs/agents.md#the-read-only-planner-role)). The plan the planner gives is published only when you choose to (`u`), as a content-addressed revision on the Issue that is re-verified on every read, and reviewed (`r`) as a document with section navigation, search and unified or side-by-side comparison; requesting changes or rejecting names one exact revision and starts nothing ([plan review](docs/garden.md#publish-and-review-a-plan)). Approve the exact plan/spec pair (`a`) to reach **Ready** without starting coding; moved heads retain the old snapshot with a stale gate. Launch remains unavailable ([gate two and external approval](docs/garden.md#approve-the-exact-plan-gate-two)).
- **AI drafting settings** in Settings → Garden save assistant, question count and prompt guidance defaults for new sessions ([settings guide](docs/tui-cli.md#settings-in-the-preview)).
- **Garden setup** in the redesign preview creates Stage and Bonsai Type fields and can resume after granting GitHub Project permissions, without restarting Bonsai ([setup and recovery](docs/tui-cli.md#garden-setup-in-the-preview)).
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
- Start, watch, control and stop sessions from the **TUI's Agents tab**, with
  provider-grouped profiles and usage, clear input-focus hints, color-coded
  capacity bars, and model/effort overrides for each new agent. Provider icons are ✹ for Claude and ❃ for Codex. Compact provider counts sit beside worktree badges,
  and a usage pane under the worktree list (30+ rows) shows remaining capacity
  with provider-colored rails and adaptive meters ([usage pane](docs/tui-cli.md#usage-pane)).
- The TUI's [Usage page](docs/tui-cli.md#agents-tab) shows compact account strips
  with aligned default and model usage rows, remaining-capacity meters, colored
  balances and reset times.
- Per-profile **launch defaults** (model, effort, permissions) and an **Agents
  tab** in the web app to manage profiles, sign-ins and sessions.
- `bonsai agent usage` shows **plan limits** (5-hour and weekly windows, credits,
  resets) for **every profile of every provider at once**, with live progress.
- `bonsai agents attach <id>` keeps detach, kill and control shortcuts visible;
  killing requires confirmation ([session controls](docs/agents.md#sessions-shared-with-the-tui-and-cli)).
- Every session shows **what the agent is doing** (working, needs input, idle) in
  the web UI, the TUI and `bonsai agents`, with optional desktop and browser
  notifications, off by default ([docs/agents.md](docs/agents.md#desktop-notifications)).
  This includes Claude questions and plan approval, pending Codex async questions,
  and inferred Antigravity input dialogs.

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

Preview of the TUI redesign (shell with tabs and footer, working Settings,
Accounts and Garden setup/board; Playground and Worktrees remain placeholders;
the default stays the current TUI):
`bonsai --tui-layout redesign`, see
[docs/tui-cli.md](docs/tui-cli.md#redesign-preview---tui-layout-redesign).

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
| [docs/garden.md](docs/garden.md) | Garden connection, approved-work board and table, filters, issue documents and freshness |
| [docs/agents.md](docs/agents.md) | Claude Code, Codex and Antigravity profiles, seeding, isolation, usage, defaults |
| [docs/tui-cli.md](docs/tui-cli.md) | TUI keybindings, CLI reference, configuration, hooks, prune, updates |
| [TUI redesign plans](docs/plans/tui-redesign/README.md) | Planned sequence: shell, Settings, Accounts, Garden, Playground, Worktrees |
| [Approved TUI mockups](docs/design/tui-redesign/README.md) | Revision 02 reference images and PDF; design artifacts, not implemented features |
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
