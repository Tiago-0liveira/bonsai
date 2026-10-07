# Process deletion and latest command per worktree

## Objective

Add permanent process deletion to the process node context menu. Opening a
project should show at most one process node and runtime option for each command
in each worktree, representing its latest explicit execution. Give the user a
project preference that keeps terminal views closed when opening the project.

This plan is implemented. It builds on the process nodes, live logs, and terminal
restoration committed in `c0b19cd` before implementation began.

## Findings before implementation

- `ProcessNode.tsx` offers Open output, Open inspector, Open worktree, Stop,
  and Restart. `useProcessActions.ts` has no removal action.
- `DELETE /api/projects/{projectId}/processes/{id}` currently means **stop**.
  `processStop` calls `daemon.Kill` and leaves metadata and logs in place.
- The daemon already has `Client.Remove`, `KindRemove`, and `Server.remove`.
  Removal rejects live processes and cancels backoff. The local API's
  `daemonClient` interface does not expose removal. The server currently ignores
  the store cleanup error, and `Store.RemoveRecord` ignores log cleanup errors;
  both need attention before the UI promises permanent deletion.
- `projectProcessesSelector` returns every retained execution.
  `projectCanvasProcessesSelector`, `buildCanvasGraph`, and `BottomWorkspace`
  consequently expose repeated runs as separate nodes/options.
- Terminal restoration already uses saved views per project/worktree rather
  than opening arbitrary discovered processes. Each explicit launch adds its
  process ID to the saved open list, so repeated launches can still accumulate
  views that return when the project opens.
- Records have structured program, arguments, and working directory, but the
  browser summary exposes only the display command. `StartedAt` changes on
  automatic retries and can be empty when execution fails before starting.
  It cannot reliably identify the latest user-requested run.
- `DeleteWorktreeDialog` checks the full process inventory. That check must
  continue to account for older live executions after display deduplication.

## Recommended behavior

| Action or event | Result |
| --- | --- |
| Close output | Close only the terminal view; keep the process and node. |
| Stop process | Stop execution and retries; keep its node, record, and logs. |
| Delete process on a finished run | Permanently remove that execution's record and logs, close its view, and clear its UI references. |
| Delete process on an active run | Offer **Stop and delete process**; confirm stopping before removing its record and logs. |
| Open a project | Show only the latest execution of each command per worktree. Apply the project's terminal reopening preference. |
| Launch the same command again | Replace the displayed execution and reuse its canvas position and terminal slot; open/focus the new output. |
| Automatic retry | Update the same execution; do not create a duplicate or change command ordering. |
| Manual restart | Keep the process ID and logs; mark it as the latest explicit execution of that command. Keep its open/closed view preference. |
| Delete the displayed latest execution | Remove its node without revealing an older execution in its place. |
| Run that command after deletion | Show the newly requested execution normally. |

Use one unambiguous **Delete process** action rather than synonymous Remove and
Delete entries. Confirmation identifies the command and worktree and explains
that the selected execution's retained logs will be deleted. Deleting a run does
not implicitly delete other executions or stop other running commands.

“Same command” means the same effective invocation in the same project and
worktree: working directory, executable, and ordered arguments. Different
arguments or package directories remain separate commands. Command labels alone
are not identity. Matching commands in different worktrees remain independent.

## Implementation sequence

### 1. Define command identity and explicit execution ordering

- Derive an opaque command key in the backend from a versioned, structured
  invocation. Scope it by owning worktree; include the working directory and
  preserve argument boundaries and order. For shell/legacy records, use the
  exact stored shell command and directory instead of attempting to parse it.
- Preserve a stable worktree ownership key even when the worktree association
  cannot be resolved to a current browser worktree ID. Unassigned records from
  different directories must not collapse into one command on the project shelf.
- Persist a daemon-assigned monotonic execution order before each new launch or
  explicit manual restart, including failed starts. Automatic retries keep that
  order. Persist the counter independently of records, as process ID allocation
  already does, so deletion or daemon restart cannot reuse an order.
- Add the opaque key and execution order to process summaries and browser types.
  Do not send raw environment values to the browser. Keep serve-group members
  distinct by their existing service identity.
- Derive keys for existing records and assign deterministic legacy ordering
  from monotonic daemon IDs. Historical manual restart order cannot be recovered
  exactly; subsequent explicit execution establishes the new authoritative order.
- Keep per-record revision ordering separate from ordering between executions.

Primary files: `internal/core/procstore/procstore.go`, daemon spawn/restart and
protocol/client code, `internal/server/localapi/snapshot.go`,
`web/src/api/git.ts`, `web/src/api/snapshotReconciliation.ts`,
`web/src/types/index.ts`.

### 2. Project one visible execution per command consistently

- Add a shared, memoized process projection that groups by project/worktree/
  command key and selects the greatest explicit execution order.
- Use it for canvas nodes, process counts, runtime options, and ordinary process
  navigation. Keep the complete canonical inventory for lifecycle supervision,
  worktree deletion checks, and explicit management of older active runs.
- A newer failed or stopped execution still wins over an older successful or
  running one. Do not silently stop or delete older executions as a side effect
  of deduplication. If older instances are still active, expose their count and
  explicit controls within worktree process management without adding duplicate
  command nodes or default terminal tabs.
- Remap an existing command's selection, canvas placement, reveal target, and
  open terminal slot to the latest process ID. Keep other commands' positions
  and saved view order unchanged. Switch output to the latest execution rather
  than mixing logs from separate records.
- An incoming snapshot may update the backing execution of an already-open
  command view; it must not open a closed command view. Explicit launch still
  opens output. Preserve the existing protection for launches awaiting their
  first authoritative snapshot.
- Keep projection changes insensitive to PID/revision-only traffic so existing
  render and preference-write isolation remains intact.

Primary files: `web/src/stores/projectSelectors.ts`,
`web/src/features/workspace/canvas/buildCanvasGraph.ts`,
`web/src/features/workspace/canvas/BonsaiCanvas.tsx`,
`web/src/features/terminal/BottomWorkspace.tsx`,
`web/src/stores/runtimePreferences.ts`, `web/src/api/processes.ts`.

### 3. Implement permanent deletion and prevent reappearance

- Preserve the existing DELETE-as-stop contract for current callers. Add a
  project-scoped `POST /api/projects/{projectId}/processes/{id}/remove` action,
  with an explicit stop-first option for **Stop and delete process**.
- Expose daemon removal through the local API. Coordinate stop, retry
  cancellation, removal, and concurrent restart in the daemon; a loose browser
  sequence of Stop followed by Remove must not report success during a race.
  Refuse deletion if the process cannot be confirmed stopped.
- Make repeated removal safe. Remove the record, current/rotated logs, cursor,
  and other process-owned artifacts. Propagate cleanup failures and retain
  enough state to retry; report success only after durable deletion completes.
- Persist a command visibility cutoff through the deleted execution's order.
  Older retained executions at or below that cutoff stay absent from ordinary
  process surfaces. A later explicit launch or restart receives a higher order
  and becomes visible. This prevents deleting the latest run from revealing an
  older duplicate, including after browser or daemon restart.
- Publish that visibility metadata with process authority and return it from
  the mutation. Protect the backend snapshot cache from a pre-deletion read
  completing late. Client-side deletion guards must also reject late snapshots,
  stream status frames, and pending action responses for deleted IDs.
- After confirmed success, remove the process from canonical browser state,
  all saved views and active IDs, selection/inspector state, node placements,
  pending reveal targets, and cached stream resources. Dispose the output
  connection and reconnect timers immediately. Never open a replacement view
  merely because deletion closed the active one.
- Add the action to the context menu and shared inspector controls, with
  pending/error feedback and protection against duplicate submissions. Keep
  errors visible when a stop succeeds but deletion still fails.

Primary files: `internal/server/localapi/processes.go`,
`internal/server/localapi/server.go`, `internal/server/localapi/state_sync.go`,
daemon removal handlers and process store, `web/src/api/processes.ts`,
`web/src/api/processStream.ts`, `web/src/stores/bonsai.ts`,
`web/src/features/terminal/useProcessActions.ts`,
`web/src/features/terminal/ProcessActions.tsx`,
`web/src/features/workspace/nodes/ProcessNode.tsx`, snapshot reconciliation.

### 4. Add a project terminal reopening preference

- Provide **When opening this project: Keep terminals closed / Restore last
  open views**. Recommend Keep terminals closed as the initial default.
- Apply the choice on initial project opening, browser reload, and returning
  from another project. Explicit launches and Open output remain available.
  Worktree switching continues to honor views explicitly opened during the
  current project visit.
- In restore mode, deduplicate saved process references by command key and map
  them to their latest visible execution while preserving tab order and focus.
  A command with no saved open intent stays closed; an empty panel stays empty.
- Defer reference remapping/pruning until an authoritative process snapshot and
  visibility metadata are available. Loading, connection failures, and another
  project's snapshot must not erase preferences or attach arbitrary streams.
- Version and migrate browser preferences, including the new project setting.
  Preserve saved layout and unrelated settings; add only view metadata to the
  existing persistence allowlist, never process output or command arguments.

Primary files: `web/src/stores/runtimePreferences.ts`,
`web/src/stores/workspacePersistence.ts`,
`web/src/stores/preferenceBoundary.ts`, `web/src/stores/bonsai.ts`,
`web/src/api/git.ts`, project settings UI.

## Verification and acceptance criteria

1. Run one command repeatedly on worktree A: exactly one node and runtime option
   remain, pointing to the latest explicit execution. Run it on worktree B:
   one separate entry appears there. Different arguments/directories stay separate.
2. Cover failed starts, manual restart of an older record, automatic retries,
   equal/legacy ordering, unresolved worktrees, and repeated snapshots. Latest
   selection is deterministic, and live duplicates remain manageable.
3. Delete a finished or active latest run: its node, view, inspector selection,
   metadata, logs, and stream connection disappear. An earlier run does not
   replace it. Refresh, reconnect, browser reload, and daemon restart preserve
   that result. A new explicit launch appears normally.
4. Cover idempotent deletion, cleanup errors, failed stop, stop/restart/delete
   races, delayed snapshots/status frames, and actions completing after project
   switching. No failed deletion is presented as complete.
5. Verify Keep terminals closed and Restore last open views across multiple
   projects/worktrees, legacy duplicate saved views, saved empty panels, and
   initial loading. Restore never creates multiple views for the same command.
6. Retain checks that worktree deletion sees every live process, closing output
   does not stop a process, and PID-only updates do not recompute the graph or
   write browser preferences.
7. Run focused Go tests for process store, daemon, and local API; focused Vitest
   tests for process APIs/actions, projection, graph, runtime preferences, and
   persistence; Playwright process-node, native process-terminal, restoration,
   and worktree-management scenarios. Complete web typecheck/lint/build and
   repository Go vet checks. Use isolated fixtures for process lifecycle tests.

Deliver in this order: identity and projection, durable deletion and its UI,
then the reopening preference and migration. Validate each stage before moving
on, followed by the full-stack acceptance scenarios.


## Implementation notes

- Command identity uses a versioned hash of worktree ownership, effective working
  directory, executable, ordered arguments (or exact legacy shell command), and
  serve service identity. Environment values remain outside browser summaries.
- Explicit execution order uses an independent persisted counter. Legacy records
  fall back to daemon ID order. Automatic retries retain the explicit order.
- Ordinary process surfaces share the latest-command projection. Older live runs
  remain in canonical inventory and have explicit controls under the worktree
  inspector's advanced settings. Worktree deletion still checks all live runs.
- Permanent deletion uses the project-scoped `/processes/{id}/remove` endpoint
  with `{ "stop_first": true | false }`. DELETE continues to mean stop. Daemon
  lifecycle serialization prevents concurrent restart from reviving a removed
  execution. A persisted private deletion journal retains retry metadata until
  artifact cleanup and visibility publication complete; startup retries pending
  cleanup. Public snapshots never contain the private journal or environment.
- Deletion cutoffs and deleted IDs travel with process authority. The local API
  serializes process reads with removal commits. Browser guards reject deleted
  status/action responses, dispose output connections, and preserve higher
  cutoffs across delayed snapshots.
- Workspace preferences are version 3. Each project can keep terminals closed
  (the default) or restore its saved views. Explicitly opened views remain usable
  across worktree switches during a project visit. Restoration waits for both
  process inventory and visibility authority, and remaps references before
  pruning them.

Historical manual restart order in legacy records remains unrecoverable; a new
explicit launch or restart establishes authoritative ordering, as planned.


Validated with process-store, daemon, and local API tests under the Go race
detector; repository `go vet ./...`; all 232 Vitest tests; web typecheck, lint,
and production build; and 13 Playwright process-node, native process-terminal,
restoration, worktree-management, and storage-migration scenarios.
