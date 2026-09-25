# AGYM AI Agent Integration

Bonsai integrates with [AGYM](https://github.com/Tiago-0liveira/agy-manager) to run and monitor durable headless AI agents inside dedicated Git worktrees.

## Overview

The integration maintains a strict boundary between Bonsai and AGYM:
* **Bonsai** manages Git repository operations, worktree creation/pruning, worktree identity, deletion guards, and terminal/TUI presentation.
* **AGYM** manages user accounts, credentials, model quota tracking, exclusive scheduler leases, detached per-run supervision, and durable task execution.
* The two tools communicate over a versioned machine protocol (`agym integration ... --protocol 1 --json`) using direct process execution with standard JSON and streaming NDJSON.

Bonsai requires zero Python runtime dependencies: normal builds and tests run cleanly without `agym` or Python installed.

---

## Configuration

In your project's `.bonsai.yaml` (or via the TUI command palette `Ctrl+K` -> `Config: gym.*`):

```yaml
gym:
  # Enable background agent polling and status updates in the TUI (default: false)
  enabled: true

  # Default AGYM profile to use when starting tasks (default: "auto")
  default_profile: auto
```

---

## CLI Commands

The CLI commands are accessible under `bonsai gym`:

### 1. Status

Inspect availability and current worktree/run state:

```sh
# Current worktree status (if in git) and AGYM availability
bonsai gym status

# Inspect a specific worktree
bonsai gym status --worktree ../repo-parser

# Inspect a specific run by ID globally (works outside Git)
bonsai gym status --run run_12345 --json
```

### 2. Profiles

List available accounts and their readiness:

```sh
bonsai gym profiles
bonsai gym profiles --json
```

### 3. Usage & Quotas

View quota windows and remaining limits:

```sh
# All profiles
bonsai gym usage

# Specific profile with explicit refresh
bonsai gym usage --profile personal --refresh --json
```

### 4. Run an Agent

Launch a headless task. By default, **Bonsai automatically generates a concise Git branch name from your task prompt** using AGYM non-interactively (`agym <profile> --model gemini-3.8-flash-low -p "..."`), creates a dedicated worktree, and starts the agent in it:

```sh
# Automatic: generates branch name from task, creates new worktree, and runs agent
bonsai gym run --task "Fix memory leak in auth middleware"

# Explicit branch name (skips AI branch generation, creates worktree)
bonsai gym run --branch fix/auth-leak --task "Fix memory leak in auth middleware"

# Run in the current worktree without creating a new one
bonsai gym run --here --task "Run linter and fix import formatting"

# Run in an existing target worktree with a specific profile
bonsai gym run --worktree ../repo-feature --profile personal --task "Add unit tests"

# Read task description from stdin or file
bonsai gym run --task-file task.txt
cat prompt.txt | bonsai gym run --task-file -
```

On launch, the command prints the generated branch and worktree paths, returns the assigned Run ID, lease ID, and profile, and suggests the attach command.

### 5. Attach & View Output

Stream captured agent output in real-time:

```sh
# Attach to the active run in the current worktree
bonsai gym attach

# Attach to a specific run by ID, optionally after a sequence number
bonsai gym attach --run run_12345 --after 100 --json
```

*Pressing `Ctrl+C` detaches the observer; the background agent continues executing.*

### 6. Stop an Agent

Request cancellation of an active run:

```sh
# Stop the run in the current worktree
bonsai gym stop

# Stop a specific run and wait for confirmation
bonsai gym stop --run run_12345 --wait --timeout 30s
```

---

## TUI Usage

### Starting Agent Runs
You have two ways to start an agent task from the TUI:
* **Auto Branch & Worktree**:
  - Open command palette (`Ctrl+K`) -> `Agent: Start new task (auto worktree)`, or press `c` (create worktree) -> choose `AI agent task (auto branch)`.
  - Type your task description. Bonsai asks AGYM with a fast model for a branch name, creates the worktree, switches selection to it, and starts live streaming in the Agent tab.
* **In Selected Worktree**:
  - Select an existing worktree, press `Ctrl+K` -> `Agent: Start new task in selected worktree`.

### Agent Tab
* Switch to the **Agent** tab using the `a` key, `Shift+Tab` cycling, or the command palette (`Ctrl+K` -> `Show agent`).
* Displays:
  * **Profile**: Selected profile (e.g. `personal`).
  * **Status**: Live status (`starting`, `running`, `stopping`, `succeeded`, `failed`, `stopped`).
  * **Elapsed**: Formatted execution time (`mm:ss` / `hh:mm:ss`).
  * **Quota**: Model quota remaining and time since last observation.
  * **Captured Output**: Scrollable, auto-following viewport showing real-time logs.

### Worktree Badges
When an agent is running or assigned, the worktree row displays an active badge:
```text
› feat/parser   …   agent: personal · running
```

### Deletion Guard (Safety)
Bonsai protects your active worktrees:
* If an agent is currently active or in a pending/uncertain state, pruning the worktree (`x`) or running bulk prune (`X`) is refused:
  `cannot prune worktree: an AI agent is currently active`
* Terminal runs (`succeeded`, `failed`, `stopped`) automatically archive the binding and permit safe pruning.

---

## Troubleshooting

* **`agym executable not found in PATH`**: Ensure `agym` is installed and available in your shell's `PATH`. Normal Bonsai functions continue to work without it.
* **`workspace is already running an agent`**: Only one agent may execute in a given worktree at a time. Stop the active run or wait for it to complete before starting a new task.
* **`cannot prune worktree: agent status uncertain`**: Bonsai fails closed if it cannot confirm that an agent has finished. Verify `agym` status or stop the run explicitly before pruning.
