# AGYM integration into Bonsai

Status: proposed architecture and implementation plan; no runtime integration is implemented by this document.

## 1. Repository inspection and decisions

Inspected on 2026-09-23:

- Bonsai `origin/main`: `ed1267baaa4c2e87059a583e7c888ee1832e86b0`.
- AGYM `main` (also remote HEAD): `27613fd72db128c17082b8b57341b2b876d25c77`.
- Additional, **unmerged** AGYM orchestration implementation: `feature/orchestrate-v1`, `eee17168ea0eb875a4ed51c606ade7d1704e0b2e`, inspected in the existing `orch/00-contracts` worktree. Other `orch/*` worktrees exist; their names are not evidence of released features. Untracked planning files were not used as implementation evidence.

The planning branch is `feat/agym-integration-plan`, based on Bonsai's fetched `origin/main`. The permanent home of this integration is Bonsai's ordinary source tree. AGYM remains independently installed and released. There is no submodule, vendored Python, plugin deployment, or special long-lived integration branch.

### Evidence that shapes the design

| Existing implementation | Consequence |
|---|---|
| Bonsai `internal/cli/cli.go`: standard-library `flag` dispatch; global commands handled before `git.MainRoot` | Extend this dispatcher; do not add another CLI framework. Global gym inventory commands must work outside Git. |
| Bonsai `internal/core/git/git.go`: `ListWorktrees`, `MainRoot`, `RepoRoot`, removal operations | Reuse discovery and validation. Branch names are display values, not durable worktree IDs. |
| Bonsai `internal/core/exec/exec.go`: shell-based `Manager.Spawn`, in-memory output buffers, process groups | Reuse the output presentation pattern, not this launcher for durable agents or machine RPC. |
| Bonsai `internal/ui/update.go`: quit calls `m.procs.KillAll()` | Agent processes cannot be registered with this manager. Quitting should cancel observers only. |
| Bonsai `internal/ui/components/terminal/terminal.go`: scrollable viewport with `SetContent` and `SetFollow` | Already sufficient for captured agent output; no terminal emulator or PTY needed. |
| Bonsai `model.go`, `view.go`, `update.go`, `palette.go`: tab enum, conditional tabs, asynchronous commands, worktree badges | Add an Agent tab and messages alongside existing Log/Processes/Inspect/Checks views. |
| Bonsai `internal/core/config/state.go`: user-global JSON, mutex, fixed temporary filename | Suitable existing preferences convention, but not a cross-process transactional run mapping store. Keep agent bindings separate. |
| AGYM `agym/cli.py`: custom dispatcher, profile fallback, startup updater; `usage --json` already exists | Add an explicit integration namespace before updater/profile dispatch. Existing JSON has no negotiated integration contract. |
| AGYM `profiles.py`, `launcher.py`, `usage.py`, `cache.py`, `quota_api.py`, `wincred.py`, `maccred.py` | All profile, environment, credential, and quota work stays behind AGYM. Reuse these internals there. |
| AGYM `rotator.py`: sequential rotation; `picker.py`: interactive quota ranking | Neither is a stable leased `auto` API. Do not reproduce either in Go. |
| AGYM main `council/providers/antigravity.py`: headless `--output-format stream-json --print`, working directory, process-tree helpers; `council/storage.py`: SQLite events/idempotency | Useful execution/recovery primitives exist. Council has optional dependencies and is not a lightweight public integration API. |
| AGYM main `council/engine.py`: execution in isolated scratch workspaces; `council/scheduler.py`: in-process semaphores | Do not send Bonsai tasks through a whole Council workflow: it changes workspace semantics and does not establish cross-process profile exclusivity. |
| AGYM main `council/recovery.py`: orphaned live processes become `UNKNOWN`, runs need attention | Durable records are not durable supervision. Bonsai restart survival still needs a separate AGYM owner process. |
| Unmerged AGYM `orchestration/{leases,scheduler,persistence,runner}.py`: `ProfileLeaseManager`, `ProfileScheduler`, `FileRunStore`, `AntigravityRunner` | Preferred reuse path after those modules are reviewed and merged. Do not create competing scheduling/storage implementations. |
| Unmerged `ModelInvocation` lacks explicit cwd; runner captures with `communicate()`; cancellation tracks process objects in memory | Add explicit worktree cwd, incremental output, detached supervision, and restart-safe control before advertising durable integration runs. |
| Unmerged lease records have PID but no start-time identity; scheduler treats unknown quota as full capacity in ranking | Strengthen stale-lease identity and unknown-quota policy in AGYM; these are integration prerequisites, not Bonsai fixes. |

Repository sources: [Bonsai at inspected commit](https://github.com/Tiago-0liveira/bonsai/tree/ed1267baaa4c2e87059a583e7c888ee1832e86b0), [AGYM main at inspected commit](https://github.com/Tiago-0liveira/agy-manager/tree/27613fd72db128c17082b8b57341b2b876d25c77). Local feature-branch findings are explicitly provisional and must be rechecked before implementation.

## 2. Recommended architecture and ownership

```text
Bonsai CLI / Bubble Tea TUI
        |
        v
internal/core/gym       worktree identity, bindings, lifecycle coordination
        |
        v
internal/core/agym      typed exec-based JSON/NDJSON client
        |
        | installed `agym integration ...` (argv + stdin; no shell)
        v
AGYM integration facade
        |
        +-- existing profiles / quota cache / scheduler / leases / run store
        |
        +-- detached AGYM supervisor, one per active run
                    |
                    +-- headless Antigravity in the requested worktree
                    +-- durable events, status, output, stop requests
```

| Owner | Responsibility |
|---|---|
| Bonsai | Git/worktree creation and deletion, branches, diffs, PRs, CI, normal background processes, CLI/TUI, relationships to external runs. |
| AGYM | Accounts/profiles, credential isolation, model invocation, quota freshness and selection, exclusive leases, AI scheduling, durable AI run ownership, cancellation and recovery. |
| Protocol | Versioned public projections of profiles, usage, leases, runs and events; no private file layouts or credential paths. |

Bonsai owns its regular processes and short-lived AGYM command/observer subprocesses. AGYM owns Antigravity and its supervisor. A process displayed in Bonsai need not be owned by Bonsai's process manager.

The initial execution unit is one headless task and at most one active agent per worktree. Multiple worktrees may run concurrently subject to AGYM capacity. `--profile auto` is passed unchanged to AGYM. No local account ranking, quota parsing from human text, lease renewal loop, or AI retry policy belongs in Bonsai.

Use a detached per-run AGYM supervisor initially, rather than requiring a global server, HTTP endpoint, or service installation. It receives a persisted request ID, owns the runner and output pipes until completion, and exits after durable finalization. Read/stop/event commands communicate through AGYM's own existing run store and locking primitives. Their private implementation is invisible to Bonsai. If AGYM later introduces a daemon, the public CLI contract stays the same.

Detached process support must be implemented and tested on each advertised platform. On POSIX isolate the supervisor session and all standard handles; on Windows use detached creation semantics and tested process-tree ownership. Never inherit Bonsai output pipes or a kill-on-Bonsai-exit job. Do not claim survival of machine reboot, logout policies, or supervisor death merely because Bonsai restart works.

## 3. Stable AGYM machine interface

All commands below are **proposed**, not existing released commands. Public protocol documentation and schemas belong to AGYM; Bonsai pins representative conformance fixtures.

### Negotiation and transport

```sh
agym integration info --json
agym integration profiles --protocol 1 --json
agym integration usage --protocol 1 --json
agym integration usage --profile personal --refresh --protocol 1 --json
agym integration run start --request-json - --protocol 1 --json
agym integration run get --id RUN_ID --protocol 1 --json
agym integration run list --client bonsai --workspace-key KEY --protocol 1 --json
agym integration run list --request-id REQUEST_ID --client-id CLIENT_ID --protocol 1 --json
agym integration run stop --id RUN_ID --protocol 1 --json
agym integration run events --id RUN_ID --after 0 --limit 200 --protocol 1 --json
agym integration run events --id RUN_ID --after 0 --follow --protocol 1 --ndjson
agym integration lease get --id LEASE_ID --protocol 1 --json
```

`info` is the unversioned bootstrap operation; it is cheap, offline and does not start supervisors or refresh quota. Example successful response:

```json
{
  "protocol": {"major": 1, "minor": 0},
  "ok": true,
  "data": {
    "agym_version": "example-build",
    "instance_id": "agym-installation-uuid",
    "supported_protocol_majors": [1],
    "capabilities": ["profiles.read", "usage.read", "runs.headless", "runs.durable", "runs.stop", "runs.events", "runs.events.follow", "profiles.auto", "leases.read"],
    "limits": {"max_event_bytes": 262144, "max_page_events": 200}
  }
}
```

- Protocol major governs breaking changes; minor changes are additive. Bonsai initially supports major 1. It tolerates unknown fields and event types, checks required fields, and requires capabilities per action. Incompatible major disables gym actions only. No fallback to human-readable output.
- Every one-shot response contains `protocol`, `ok`, and exactly one of `data` or `error`. Errors contain `code`, safe `message`, `retryable`, optional `retry_after_seconds` and `run_id`. Suggested codes: `PROFILE_UNAVAILABLE`, `NO_CAPACITY`, `QUOTA_EXHAUSTED`, `UNSUPPORTED_CAPABILITY`, `RUN_NOT_FOUND`, `WORKSPACE_BUSY`, `WORKSPACE_MISSING`, `IDEMPOTENCY_CONFLICT`, `NEEDS_ATTENTION`.
- Exit 0 means command success (including successful retrieval of a failed run), 2 invalid request, 3 unavailable/capacity, 4 conflict, 5 internal failure. Stable JSON error codes carry detailed meaning. An error remains meaningful if an unexpected nonzero exit, signal, timeout, or malformed stdout prevents decoding; Bonsai labels these transport failures.
- stdout is exclusively UTF-8 JSON/NDJSON. No updater prompts, login/browser launches, progress bars, ANSI, banners or tracebacks. stderr is diagnostics, drained concurrently and bounded. Integration dispatch precedes startup-update behavior and imports execution-only dependencies lazily.
- Use `os/exec` argv directly, close stdin after JSON, and set `NO_COLOR=1`. Resolve the installed executable from host PATH; reject implicit current-directory resolution. No shell interpolation or credential environment construction in Go. Task text travels over stdin, not argv.
- Defaults: 3-second info deadline, 10-second local reads/start acknowledgement, 60-second explicit quota refresh. Start deadlines apply to acceptance, not agent duration. A timeout after submission means outcome unknown, not permission to issue a new request ID.
- Bound control responses (e.g. 4 MiB), event frames (256 KiB), and stderr (64 KiB). AGYM chunks long output. Reject oversized/truncated frames without unbounded memory growth. Streaming frames include protocol major, type, run ID and durable sequence; EOF alone never means the run succeeded.

### Run submission and lease ownership

Example stdin payload (schema v1):

```json
{
  "request_id": "uuid-per-user-start-action",
  "client": "bonsai",
  "client_id": "uuid-per-bonsai-installation",
  "workspace": {
    "key": "opaque-bonsai-worktree-uuid",
    "cwd": "/absolute/repo-worktree",
    "repository_key": "opaque-bonsai-repository-uuid"
  },
  "profile": "auto",
  "task": "Implement the agreed parser change and run its focused tests",
  "execution": "headless",
  "permission_policy": "profile-default"
}
```

AGYM validates an existing absolute cwd; it must not create a missing worktree. It independently canonicalizes cwd for duplicate-workspace protection, without becoming a Git/worktree manager. `profile-default` means AGYM applies its documented noninteractive policy; if it requires input, fail with an actionable error. Do not silently enable `--dangerously-skip-permissions` to make headless execution work. Any additional permission mode must be explicit and supported by AGYM; a working directory is not a security sandbox.

Recommended Bonsai workflow uses **atomic start with implicit leasing**:

1. Bonsai validates the selected worktree, creates a stable request ID and persists a pending binding before invoking AGYM.
2. AGYM serializes acceptance by canonical workspace, checks the idempotency key `(client_id, request_id)`, and persists a run intent and payload hash. Same key/same payload returns the same run; changed payload returns conflict.
3. AGYM chooses an eligible profile and acquires its lease using existing scheduler/lease mechanisms. It records run/lease association before executing. If profile acquisition fails, finalize rejection and release workspace ownership.
4. AGYM starts its supervisor from the persisted intent. A per-run OS lock prevents duplicate supervisors; the supervisor confirms ownership and durable readiness. Only then does the facade return a `starting` or `running` run snapshot with run ID, lease ID and profile reference. A durable accepted intent still exists if acknowledgement is lost.
5. The supervisor executes in the worktree, renews its own lease, persists output/events, observes stop requests and finalizes. Bonsai persists the acknowledged IDs. Observers are dispensable.

Existing file stores cannot make several unrelated files transactional just by calling atomic rename. Use a locked run-intent journal with explicit acceptance steps and recoverable rollback/roll-forward around leasing and spawn. Test every crash boundary. Keep one storage backend; if the existing orchestration store is used, extend it rather than introducing another SQLite database just for Bonsai.

A separate `lease acquire/release` protocol is deliberately unnecessary for Bonsai v1. `lease get` covers visibility. Later expose `lease acquire --request-json -` and `lease release --id ...` only with reservation expiry, owner identity, an atomic consume-on-start transition, and refusal to release a live run's lease. This avoids a Bonsai-owned lease heartbeat and the acquire-then-crash leak. Lease selection is still part of the requested workflow, performed inside AGYM's start transaction.

### Public data and event semantics

Public projections must not serialize internal Profile objects or credentials.

| Projection | Required data |
|---|---|
| Profile | Opaque stable `profile_id`, display `name`, readiness (`ready`, `auth_required`, `busy`, `unavailable`, `unknown`), safe reason. Rename does not change ID; removal cannot silently rebind a run. |
| Usage | Profile ID, model/quota group, windows with nullable remaining fraction and reset time, `observed_at`, `stale`, safe per-profile error. Account quota is not per-run token consumption. Unknown is not zero or 100%. |
| Lease | Opaque ID, profile ID, owning run ID, state, acquisition/heartbeat times and optional expiry. No credential paths or signalable PID in Bonsai state. |
| Run | Run/request IDs, client/workspace identity, cwd, selected profile, lease ID, task summary, status, created/started/finished times, elapsed time, optional session ID, safe error, optional exit code, last event sequence. |
| Event page | Run ID, ordered events, next cursor, `has_more`, oldest retained sequence, snapshot/status. |
| Event | Run ID, monotonically increasing durable `seq`, UTC timestamp, `type`, typed payload; output payload has `stream` and `text`. |

Wire run states: `starting`, `running`, `stopping`, `succeeded`, `failed`, `stopped`, `needs_attention`. Terminal exit states cannot spontaneously return to running. Recovery may resolve `needs_attention`; it never silently repeats a task. Observational `disconnected`/`stale` are Bonsai view states, not invented AGYM lifecycle transitions.

Sequence numbers are per run, survive restart and provide at-least-once replay. Follow replays `seq > after`, then tails without a replay/live gap. Durable output, profile assignment and terminal events are committed before publication; heartbeat frames have no durable sequence. Clients ignore duplicates and refetch on gaps. Unknown event types advance the cursor but do not mutate known state. Terminal status must also be obtainable from `run get` if the final frame is lost.

V1 polling (`events --after ... --limit ... --json`) is sufficient for the TUI and CLI attach. `--follow --ndjson` is an optional capability and can arrive later without delaying durable runs. Follow disconnect must leave the supervisor untouched. If history is pruned, return a structured `CURSOR_EXPIRED` with oldest available cursor and snapshot; display a gap, never pretend a complete log was recovered. Keep run/idempotency tombstones together so a retried historical request cannot accidentally recreate a task after log retention.

### Auto profile selection

AGYM owns eligibility, freshness, model-specific capacity, reservation, and tie-breaking. Reuse `ProfileScheduler` after merge; harden it so missing/stale quotas trigger a bounded refresh or an explicit unknown-capacity result, rather than silently ranking unknown at full quota. Respect both short and weekly windows, active leases, auth readiness and required model support. Busy profiles cannot become available merely because Bonsai disconnected.

V1 fails fast when capacity is unavailable and returns an optional retry time. No Bonsai queue or timer-based retry. Automatic rotation during a task is deferred: replaying a coding task against a different account may duplicate edits, and session handles may be account-bound. A later AGYM scheduler can queue or resume with a separately advertised capability.

## 4. Bonsai state and recovery

Separate durable relationships from remote truth. A proposed Go model (split between `gym/types.go` and wire types in `agym/types.go`):

```go
// Persisted under the common Git directory; UUIDs are assigned once.
type WorkspaceIdentity struct {
    RepositoryID string
    WorktreeID   string
    GitDir       string // canonical per-worktree git dir; distinguishes reuse
    Path         string // last validated canonical working directory
}

type RunBinding struct {
    SchemaVersion  int
    Workspace      WorkspaceIdentity
    AGYMInstanceID string
    RequestID      string
    RunID          string // empty while submission outcome is unknown
    LeaseID        string // relationship only; AGYM controls lifecycle
    CreatedAt      time.Time
    Submission     string // pending, acknowledged, rejected
}

// Memory only; obtained through the public API.
type AgentView struct {
    Binding     RunBinding
    Run         agym.Run
    Usage       *agym.Usage
    LastSeq     uint64
    ObservedAt  time.Time
    Stale       bool
    OutputTail  []string // bounded; never the canonical log
    Error       string
}
```

Use `<canonical git common dir>/bonsai/gym/` for repository/worktree identity, per-request binding records and tombstones. Use the OS user config directory for a small Bonsai `gym/client.json` containing installation UUID only. No AGYM paths, secrets, prompt text, authoritative profile health, PIDs, or copies of logs in bindings. Store relationships to completed runs as history; fetch task/profile/session/status on demand. Cursor is initially memory-only: on reopen replay a bounded tail, so persisting a cursor without its corresponding output cannot hide history.

Add Git helpers for canonical common dir and per-worktree git dir. Resolve symlinks/platform path semantics with filesystem/Git results; do not lowercase all paths. Worktree UUID is associated with its git-dir identity, not just branch or path. A path reused after removal receives a new UUID. External moves/deletions require reconciliation and are not silently treated as the old workspace. Moving the entire repository while a run is active is unsupported in v1; show a mismatch and require stopping/rebinding explicitly.

`gym.Store` uses an OS advisory lock per repository, reload-under-lock, unique temp files and atomic replacement. Use `golang.org/x/sys` already present in Bonsai for platform lock helpers rather than an expiring lockfile that can be stolen from a live writer. A process-local mutex alone is insufficient for simultaneous CLI and TUI writes. Corrupt binding state is surfaced; do not overwrite it with an empty store or start a potentially duplicate run.

Recovery sequence:

1. Read Bonsai bindings and fresh Git discovery. Probe AGYM capabilities and installation ID asynchronously.
2. For known runs, query snapshots and resume event replay. For pending bindings, query `(client_id, request_id)` and workspace. If found, persist IDs; if not found, retain an unresolved intent until the user explicitly retries. Never automatically replay an editing task on startup.
3. A submission timeout keeps its request ID. An explicit retry can resubmit that same payload/key; if task content was lost, first resolve via lookup, then ask for a new task only after AGYM confirms no accepted intent. AGYM must serialize lookup/start reconciliation so an in-flight intent cannot appear definitively absent.
4. If local bindings are missing, list runs by the Bonsai client/repository/workspace identities; match canonical cwd and Git identity before offering recovery. `attach --run ID` permits explicit recovery when identities were lost. Never adopt an unrelated run on matching branch text alone.
5. A changed AGYM `instance_id` (for example a replaced data root) makes old bindings unresolved; do not reinterpret IDs in another installation. Missing/pruned runs remain visibly unavailable until explicitly forgotten.

Survival guarantee: Bonsai crash/restart/exit does not stop the AGYM supervisor. AGYM supervisor death is different: reconcile using PID **and** creation identity/boot identity and durable state. A live but uncontrolled child is `needs_attention`; do not free its profile or automatically spawn a replacement. V1 may require an explicit AGYM stop/recovery action rather than promising live pipe reattachment. System reboot marks interrupted attempts accordingly; no automatic repeat of edits.

## 5. Exact Bonsai change map

Paths are relative to Bonsai. Keep the new packages internal and focused; no generic agent-provider framework.

| Add/change | File(s) | Scope |
|---|---|---|
| Add | `docs/agym-integration-plan.md` | This permanent architecture/roadmap. |
| Add | `internal/core/agym/types.go`, `errors.go` | Public protocol DTOs, capability checks and typed errors. |
| Add | `internal/core/agym/client.go` | Injectable command runner; executable resolution, deadlines, bounded JSON, info/profiles/usage/run/lease calls. |
| Add | `internal/core/agym/events.go` | Bounded pages and optional NDJSON parser, cursors and observer cancellation. |
| Add | `internal/core/agym/client_test.go`, `events_test.go`, `testdata/v1/*.json`, `testdata/v1/*.ndjson` | Fake executable and pinned wire fixtures. |
| Add | `internal/core/gym/types.go`, `store.go`, `lock_unix.go`, `lock_windows.go` | Worktree identities, bindings, cross-process atomic persistence. |
| Add | `internal/core/gym/service.go`, `reconcile.go`, `lifecycle.go` | Shared CLI/TUI start/status/attach/stop, uncertainty handling, deletion guard. Service depends on a small typed AGYM client interface. |
| Add | `internal/core/gym/store_test.go`, `service_test.go`, `reconcile_test.go`, `lifecycle_test.go` | Race/crash/recovery and lifecycle semantics. |
| Change | `internal/core/git/git.go`, `git_test.go` | Canonical common-dir/git-dir identity helpers; keep Git independent of AGYM. |
| Add | `internal/cli/gym.go`, `gym_test.go` | Six requested subcommands, standard `flag`, injected output and service. |
| Change | `internal/cli/cli.go` | Dispatch gym before unconditional repository lookup; help entry. Only worktree-specific gym operations resolve Git. |
| Change | `internal/core/config/config.go`, `config_test.go` | Optional `gym.enabled` (default false) for background TUI discovery, `gym.default_profile` (default auto). CLI gym commands are an explicit opt-in regardless of background discovery setting. |
| Add | `internal/ui/agent.go`, `agent_cmds.go`, `agent_test.go` | Agent view formatting, bounded output, async typed messages and actions. Use existing terminal component; no duplicate viewport package. |
| Change | `internal/ui/model.go`, `update.go`, `view.go` | `tabAgent`, service/cache dependencies, message routing, visibility/layout; exit cancels gym observers while ordinary processes retain current behavior. |
| Change | `internal/ui/components/worktreelist/worktreelist.go`, `worktreelist_test.go` | Optional assigned-profile/running-agent badge fields, responsive rendering; AI runs separate from normal process count. |
| Change | `internal/ui/keys.go`, `palette.go`, `configedit.go` and their existing tests | Configurable Agent action, palette start/stop/attach/status, two config settings. Use palette/Shift+Tab initially instead of taking an occupied key. |
| Change | `internal/ui/cmds.go`, `prune_test.go` | Both `pruneWorktree` and `bulkPrune` invoke the shared guard before merge, hooks, removal or branch deletion. |
| Change | `internal/ui/render_test.go`, `keys_test.go` | Missing integration, compact layouts, Agent navigation and help. |
| Add/change | `docs/agym.md`, `README.md`, `.github/workflows/ci.yml` | User contract, installation/troubleshooting, optional integration CI job, normal builds without Python/AGYM. |

Do not add fields to `git.Worktree` for AI state. Compose list items with `gym.AgentView`. Do not put bindings in `config.State` or `.bonsai.yaml`. Existing config writing is generic; extend `config/write.go` only if implementation reveals a serialization gap. `main.go` can remain unchanged if `ui.New` and CLI construct the lazy service through internal constructors; retain dependency-injection constructors for tests.

## 6. CLI behavior

```sh
bonsai gym status                            # availability + current repository runs, if in Git
bonsai gym status --worktree ../repo-parser   # one worktree
bonsai gym status --run RUN_ID --json         # global lookup, no Git required
bonsai gym profiles [--json]                  # global inventory
bonsai gym usage [--profile personal] [--refresh] [--json]
bonsai gym run --worktree ../repo-parser --profile auto --task-file task.txt
bonsai gym run --profile personal --task "Fix the parser" --json
bonsai gym stop                              # current worktree's active run
bonsai gym stop --run RUN_ID --wait --timeout 30s
bonsai gym attach                            # replay tail and follow, output only
bonsai gym attach --run RUN_ID --after 120 --json
```

- Run without `--worktree` targets the actual worktree containing cwd, **not** `MainRoot`. TUI run targets selection. Validate a supplied path against Git discovery. Detached-HEAD worktrees work. `--profile` defaults to configured value then `auto`.
- Require exactly one task source: `--task` or `--task-file`; `--task-file -` reads stdin. No implicit interactive agent. Default `run` returns on durable acceptance with IDs/profile/status and a suggested attach command.
- `status`, `profiles`, `usage` perform no login or setup. Profile display is not a guarantee it remains available at start time.
- `stop` persists an idempotent AGYM stop request. Without `--wait`, success means cancellation requested, not process dead; `--wait` only succeeds after confirmed terminal state. A terminal run is a successful no-op. Timeout is reported without deleting bindings.
- `attach` is an output observer, not a PTY, shell, or session-resume operation. Ctrl-C detaches; only `stop` stops. Polling is a valid implementation if follow capability is absent. On terminal state it exits after final output; attach transport success and agent outcome are separate, with the final status explicitly printed/encoded.
- Scope ambiguity is an error: do not guess among multiple historical runs. Default stop uses the sole active run; default attach uses the active run, otherwise most recent completed run. Explicit `--run` can operate outside Git and does not create a binding unless the workspace is validated for recovery.
- `--json` uses Bonsai's documented envelope for one-shot commands and NDJSON for attach. Existing main returns 1 on errors; retain this simple CLI exit convention initially, exposing detailed error codes in JSON. No generic duplicate stderr banner should pollute stdout.
- `gym status` succeeds with `available:false` when AGYM is absent, making diagnostics scriptable. Commands requiring AGYM return an actionable missing-dependency error. Never auto-install or auto-upgrade AGYM.

## 7. TUI design

Background discovery is off by default. Explicit gym actions, existing run bindings, or `gym.enabled: true` enable lazy asynchronous probing. An absent AGYM never blocks TUI initialization, Git views or ordinary processes. Existing bindings remain visible as unavailable even if background discovery is disabled.

Worktree row example: `feat/parser   …   agent: personal · running`. Display actual selected profile from AGYM, not `auto`. Show a compact agent status on narrow terminals; full profile remains in the Agent tab. Keep task text out of list rows.

```text
Agent
Profile  personal             Status   running
Task     Fix the parser
Elapsed  03:24                Quota    5h 64% · weekly 81% (2m ago)
Run      run_...               Session  unavailable
---------------------------------------------------------------
[scrolling captured output; follows only while at bottom]
---------------------------------------------------------------
Start · Stop · Refresh · Copy run ID · Detach view
```

- Show task, profile, status, elapsed, quota freshness/error, output, run/lease IDs and nullable Antigravity session ID. Run ID always exists after acceptance; session ID may never be supplied by the runner.
- Reuse `terminal.Model` for output and existing modal/palette patterns for task/profile selection. Profile picker shows AGYM inventory plus `auto`; it does not rank candidates. No authentication UI in Bonsai.
- Add typed `gymInfoMsg`, `gymSnapshotMsg`, `gymEventsMsg`, `gymActionMsg`, each tagged with worktree ID, run ID and selection/request generation. Discard stale selection replies. All I/O occurs in `tea.Cmd`, not rendering or `Update`.
- Poll active run snapshots on a modest cadence (e.g. 2 seconds) with capped concurrency; read event pages only for the displayed run. Cached quota refresh is much slower (e.g. 60 seconds), with explicit refresh available. Coalesce refreshes, use bounded backoff on transport failure, and do not launch one usage subprocess per UI tick.
- Initial visible output is a bounded tail (e.g. 2 MiB/10,000 lines); indicate truncation. AGYM retains the authoritative history. Sanitize terminal control sequences from agent output; do not execute OSC/title/clipboard controls. Output containing suggested commands is text only.
- On tab/worktree changes cancel that observer; on quit cancel all observers and reap CLI subprocesses. Do not call `run stop`. Elapsed display uses remote timestamps/elapsed snapshots with local monotonic advancement while running, then freezes at the terminal value.
- Keep normal Processes tab actions unchanged. The Agent tab's Stop delegates to AGYM; the normal process kill/restart keys must never target an agent through a cached PID.

## 8. Deletion, concurrency and recovery edge cases

| Case | Required behavior |
|---|---|
| AGYM missing | Normal Bonsai works. Gym diagnosis explains installation; bindings remain visible. No profile-file fallback. |
| Protocol incompatible or capability absent | Disable affected gym actions with discovered/supported version/capability details. Preserve other Bonsai features. |
| `agy` missing, account unauthenticated/removed/busy | AGYM returns typed error; Bonsai displays it. No implicit login or account fallback for an explicit profile. |
| Quota exhausted/unknown | AGYM selects another eligible profile before launch only for `auto`; otherwise fail clearly. Mid-run exhaustion records failure/attention; no automatic replay. |
| Two Bonsai clients start at once | Local lock serializes binding intent; AGYM workspace exclusivity and atomic profile lease decide remote acceptance. One active run per canonical cwd even with different request IDs. |
| Agent crashes | Supervisor records failure/exit information, persists final output, releases lease only after process-tree exit. Preserve partial edits for Bonsai diff/review. |
| Bonsai exits or crashes | Supervisor continues; restart reattaches through IDs and replay. Never release lease because observer disappears. |
| AGYM supervisor crashes | Identity-checked recovery; unknown/live child remains reserved and needs attention. Do not equate stale heartbeat with safe reclamation. |
| Stop versus completion race | Idempotent state transition; preserve actual terminal result. A repeated stop cannot target a new run or reused PID. |
| Worktree deletion from Bonsai | Check bindings and AGYM workspace runs before any merge/hooks/deletion. Refuse if active or uncertain; offer explicit stop-and-prune, wait for confirmed exit, then revalidate dirty state and prune. |
| AGYM unavailable with a linked unresolved run | Fail closed for that worktree's prune, not all worktrees. Do not repurpose Git's force-dirty flag to bypass the agent guard. |
| Worktree deleted externally | Mark binding orphaned; AGYM detects missing cwd and stops/fails conservatively. Never recreate it. External filesystem changes cannot be made race-free by Bonsai. |
| Worktree path reused/moved | Compare stable identity and canonical path; no silent adoption based on path/branch. Keep old history identifiable. |
| Stale leases | AGYM checks owner identity, generation, heartbeat and surviving child identity under its lock. Invalid timestamps alone are not proof of safe reclamation. |
| Malformed/truncated stream or CLI killed | Show disconnected, retain run IDs, query authoritative snapshot, resume from cursor. Never mark successful on EOF. |
| Disk full/corrupt durable store | Refuse new launch before execution if acceptance cannot persist. Stop safely or mark attention if recording fails mid-run; never acknowledge durable acceptance that cannot be recovered. |
| AGYM upgrade while running | Keep wire-major compatibility; running supervisor stays on its loaded implementation, migrations serialize with active writes. Unsupported combinations report unavailable instead of interpreting private data in Bonsai. |

All Bonsai-controlled starts and prune operations take the same repository/workspace lifecycle lock. Hold it across final remote verification and Git deletion so another Bonsai client cannot start between check and removal. Stop first, then recheck under lock; bulk prune checks each target independently and reports skipped active/unknown targets. Existing `pruneWorktree` currently merges first, so move the agent guard ahead of that side effect.

No promise can cover arbitrary simultaneous `git worktree remove` or unrelated tools outside this coordination. AGYM's cwd checks and workspace exclusivity limit damage. A later protocol-level workspace deletion reservation can coordinate additional clients if needed; do not build it into v1 without evidence.

Profile exclusivity must cover all AGYM launch paths that can collide, including Council, interactive launch/rotate, quota probes that launch `agy`, and profile rename/remove. Integrate these with the shared lease/credential gate or advertise concurrent integration runs as unsupported on affected platforms. Existing profile-config locks only serialize configuration writes; they are not execution leases. Never run stale Chromium-lock cleanup against a profile another live process owns.

## 9. Required AGYM changes and reuse order

The read-only machine facade is small. Durable headless execution is **not** just a JSON flag; it has real lifecycle prerequisites. Do not hide that work inside Bonsai.

| File(s) in AGYM | Proposed change |
|---|---|
| `agym/cli.py` | Reserve `integration`, dispatch before startup updater and human profile fallback; lazy imports. |
| Add `agym/integration/{__init__,cli,protocol,service}.py` | Wire schemas/envelopes, argparse namespace, public redacted projections and dispatch to existing subsystems. |
| Add `agym/integration/supervisor.py` | Private detached run entry point and durable readiness/stop loop. Later move to shared orchestration runtime if other callers need it. |
| Add `docs/integration-v1.md`, `docs/integration-v1.schema.json` | Authoritative commands, data, errors, capabilities, retention and version rules. |
| Existing `profiles.py`, `usage.py`, `cache.py` | Read-only adapters first; stable public profile IDs if absent; quota freshness remains in AGYM. |
| Unmerged `orchestration/contracts.py`, `runner.py` | After merge: explicit cwd on invocation/serialization, null stdin, incremental output sink, permission semantics, process-tree ownership/identity. Reuse Council process helpers where correct instead of copying another implementation. |
| Unmerged `orchestration/leases.py`, `scheduler.py` | After merge: run-owned heartbeat, PID/start/boot identity, reservation handoff, unknown-quota/model-aware selection, cross-entry-point exclusion. |
| Unmerged `orchestration/persistence.py` | After merge: public request lookup, acceptance journal/idempotency, workspace index, durable event sequence/replay, persisted stop intent and supervisor identity. |
| Existing launch/rotate/Council paths | Share execution lease gate as required; preserve current interactive behavior outside integration. |
| Add `tests/integration/test_{cli,protocol,runs,recovery,leases}.py` | Fake-runner protocol conformance and crash/concurrency tests. Reuse current provider/fake and orchestration test infrastructure. |
| `pyproject.toml` and packaging configuration only if needed | Ensure integration modules/entry point work in pip and standalone distribution. Do not make FastAPI/web extras mandatory for CLI inventory or headless execution. |

Recommended dependency choice: land/review the existing orchestration modules, then adapt them. If they are delayed, ship Bonsai discovery/inventory first and keep `runs.durable` absent. A Council-only compatibility implementation is possible, but is not the recommended route because of scratch-workspace and optional-dependency coupling. Do not build two durable run backends just to bypass the dependency.

## 10. Small mergeable implementation worktrees

Each row is a short-lived branch/worktree in the named repository, rebased on merged prerequisites. All eventually merge to that repository's normal main branch. Fake-backed Bonsai work can proceed while AGYM runtime work is reviewed; it must remain capability-gated.

| Step / suggested branch | Deliverable | Depends on | Acceptance boundary |
|---|---|---|---|
| B0 `docs/agym-integration-plan` | This architecture and agreed v1 wire fixtures/spec outline | None | Maintainers can identify ownership and release gates. |
| A1 `feat/integration-info` | AGYM namespace, version/capabilities, clean JSON, profiles/usage projections | None | No prompts or secrets; works without Council extras; does not advertise runs. |
| B1 `feat/gym-client` | Bonsai typed client and `status/profiles/usage` | A1 contract | Missing/old AGYM leaves normal Bonsai untouched; fake process tests pass. |
| A2 `feat/integration-run-contract` | Run/lease projections, idempotency intent and lookup/events via fake runner | Reviewed orchestration persistence/contracts | Crash-safe acceptance tests; wire fixtures; durable capability still disabled. |
| A3 `feat/integration-headless-runner` | Explicit cwd, null stdin, permissions, incremental capture and process-tree handling | Reviewed orchestration runner; A2 contracts | Fake executable runs in correct directory; stream and cancellation bounded. |
| A4 `feat/integration-leases` | Shared lease gate, strengthened identity, auto selection freshness, workspace exclusivity | Reviewed scheduler/leases; A2 | Multi-process races cannot double-lease; live owner cannot be reclaimed. |
| A5 `feat/integration-durable-runs` | Detached supervisor, readiness, stop intent, recovery and capability enablement | A2–A4 | Kill client/start wrapper; task survives; supervisor-crash tests fail safely. |
| B2 `feat/gym-bindings` | Repository/worktree identity, cross-process store, reconciliation service; no run UI yet | B1, A2 contract | Pending acknowledgement crash and path-reuse tests pass. |
| B3 `feat/gym-run` | `run/stop/attach` and **both prune guards** | B2; A5 for enabled execution | Agent survives Bonsai exit, no duplicate on timeout; deletion refuses active/unknown runs. |
| B4 `feat/gym-agent-tab` | Badges and read-only Agent view/output, lazy discovery/config | B3 | Selection races, missing AGYM, compact layouts and tail bounds tested. |
| B5 `feat/gym-agent-actions` | TUI start/stop/profile/task modals, palette/config help, user docs | B4 | Same service semantics as CLI; quit detaches; no PTY. |
| B6/A6 `test/gym-conformance` / `test/integration-packaging` | Pinned real Go↔Python contract CI and packaged executable smoke tests | B3/A5 onward | Supported-platform matrix and no-secret/no-live-quota normal CI; release compatibility recorded. |

A5 is the prerequisite to call this a usable durable runner. B3 cannot ship runnable agents without the deletion guard in the same change. B4/B5 complete the requested initial user experience. Follow-stream transport, richer scheduling and advanced recovery are separate later changes, not blockers for polling-based output.

## 11. Testing strategy and release gates

### Bonsai unit and process-boundary tests

Use two seams: a typed fake AGYM client for gym/UI tests, and a **compiled fake executable/test-helper process** for the actual `os/exec` transport. Avoid shell-script-only fakes so Windows is covered. Inject executable path/runner in tests; do not require installed Python or AGYM for `go test ./...`.

Transport cases: absent binary, unsupported major/capability, additive fields, malformed/oversized JSON, exit/envelope disagreement, delayed response, stderr flood, paths with spaces/Unicode, task shell metacharacters, task over stdin, child cleanup on observer cancellation. Verify no shell execution and no prompts leak into stdout. NDJSON cases include split frames, partial last line, output above scanner default limits, duplicates, unknown events, cursor expiration and disconnect before terminal event.

Store/service cases: two OS processes start the same worktree; atomic writes interrupted; pending intent persisted before RPC; acknowledgement lost; mapping write failure after remote acceptance; changed AGYM instance; renamed profile; reused directory; corrupt local binding; correct current-worktree targeting; explicit run recovery. Assert one remote start, not just matching mock calls.

UI tests send delayed messages across selection changes, resize to narrow widths, scroll away from tail, reconnect, and quit. Assert quit calls observer cancellation but never stop, while ordinary process behavior remains intact. Test single/bulk/force-dirty prune paths, including guard-before-merge and the start/prune race. Use deterministic clocks for elapsed/quota freshness/backoff.

### AGYM runtime tests

Use existing fake runners/providers and a controllable fake `agy` executable. Assert cwd and environment isolation, DEVNULL stdin, permissions, incremental output, proper group termination, durable finalization and lease release ordering. Multiprocess tests must cover profile contention across interactive/Council/integration entry points and both Windows credential gating and POSIX process identities.

Inject crashes between intent creation, lease allocation, spawn, PID recording, readiness, response, final event, and lease release. Killing Bonsai/the submitting CLI must leave the task alive. Killing the supervisor must produce truthful attention/failure state without duplicate execution or lease theft. Include PID reuse, missing birth-time data, stale/corrupt timestamps, machine boot change, disk-full, log truncation and repeated stop. Do not use real account credentials in CI.

### Cross-repository conformance

A separate CI job installs AGYM at an immutable tested commit/release into an isolated environment, builds Bonsai, creates a temporary Git repo plus two worktrees, and executes the six commands using fake `agy`. Verify auto allocation, streamed/polled output, client restart recovery, stop and guarded deletion. Run the same protocol fixtures in Python and Go; include the oldest supported v1 release and candidate AGYM revision when available. Ordinary Bonsai release builds retain zero Python runtime dependency.

Reuse Bonsai's existing Linux/macOS/Windows CI (`go vet`, build, tests; Linux race detector). Add a focused optional real-boundary job, and make it required when publishing durable-run capability for a supported platform. Test standalone AGYM packaging as well as `python -m agym`; detached re-execution must work in both forms. A manually authorized real-account smoke test verifies actual `agy` headless flags, noninteractive permissions and OAuth isolation separately from CI, using AGYM's existing manual-integration checklist.

Release acceptance: normal Bonsai works without AGYM; accepted tasks run in the selected worktree; auto selection happens only in AGYM; stopping is truthful/idempotent; killing/restarting Bonsai preserves run/output; uncertainty blocks unsafe deletion; no private AGYM file reads occur in Bonsai. Unsupported durability platforms omit the capability rather than degrading silently to Bonsai-owned execution.

## 12. Explicitly outside the first version

- Interactive Antigravity terminal embedding, PTY allocation, stdin forwarding, resize/alternate-screen emulation, or interactive session resume.
- Bonsai-managed credentials, login/setup, quota discovery, profile selection/rotation, lease heartbeats, AI scheduling, or agent process-tree signals.
- Multi-agent orchestration, Council workflows, automatic PRs/commits/worktree creation, coordinator/auditor UIs, and multiple concurrent agents writing the same worktree.
- Automatic retries of partially executed tasks, account switching mid-session, scheduled/queued execution, unattended continuation after supervisor crash or reboot.
- A required global daemon, network API, remote execution, containers/sandbox claims, generic plugin/provider framework, or shared Go/Python runtime.
- Automatic installation/upgrades of AGYM, vendored AGYM internals, direct reads of its run databases/log files, or human-output scraping.
- A full log-history database in Bonsai, historical quota analytics, billing, or a global profile reservation UI.

The first version is a durable headless task per worktree, observed and controlled through a small versioned CLI, with enough recovery state to remain truthful after failures.
