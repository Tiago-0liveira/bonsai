# Process live logs and launch controls

## Objective

Give each managed process one persistent output window in the web client. Open it
automatically on launch, replay output from the first attempt even if the process
fails immediately, and append subsequent retries and manual restarts to the same
window. Correct tool-specific argument forwarding and expose restart policy in
the Start process modal.

## Current problems

- Process cards render command, status and port, but never request process logs.
  Exit codes and errors reach the browser store without being displayed.
- The start dialog discards the returned process and closes. Existing runtime
  selection can leave a newly started process out of view.
- The Node provider applies npm's double-dash forwarding strategy to pnpm. The
  observed `pnpm run tauri -- dev` invocation passes an unwanted `--` to Tauri.
  An existing test encodes that incorrect expectation.
- Processes default to restarting on failure up to five times, without policy
  controls in the dialog. Periodic process refresh runs every 30 seconds.
- A failure to execute a command can remove its process record and logs.
- Existing daemon log following needs additional replay/reconnect semantics:
  follow initially reads the current file, buffers incomplete lines, and ends
  when the process becomes terminal. Log retention currently keeps one backup.

The saved password-keeper logs show individual command failures, not evidence of
a concurrency failure. They provide useful regression cases: invalid forwarded
arguments, a missing Tauri subcommand, and a Rust build failing on Linux.

## Implementation plan

### 1. Preserve process identity and launch diagnostics

- Allocate a stable process ID before execution and persist its initial state.
- Preserve the failed record and diagnostic output when the executable cannot
  start. Return the created failed process so the browser can open its window.
  Reject invalid requests before creating a process.
- Capture stdout, stderr and lifecycle messages immediately, independently of
  browser attachment.
- Keep automatic retries and manual restarts under the same process ID and
  append-only history. Distinguish execution attempts from stream generations.
- Keep failures and completed processes inspectable until explicitly removed.

Primary files: `internal/daemon/server/proc.go`,
`internal/core/procstore/procstore.go`, `internal/server/localapi/processes.go`.

### 2. Add replayable live process streaming

- Add a project-scoped WebSocket stream using the existing terminal
  authentication, origin checks and project routing patterns.
- Replay saved output before following new output through one ordered stream.
  Avoid a separate snapshot-then-follow handoff that can lose output.
- Support output cursors and stream generations for reconnects. Deliver output
  without duplicates, and explicitly report an unavailable-history gap.
- Send lifecycle status, exit code, error, effective policy and retry information
  alongside output. Update browser state promptly instead of waiting for the
  periodic process refresh.
- Follow through automatic retry backoff. Drain final output before reporting
  completion. Support a later manual restart in the same window by resuming or
  reattaching from the last cursor.
- Support partial lines, output without a trailing newline, UTF-8/ANSI sequences
  crossing chunk boundaries, and log rotation.
- Add cancellation to daemon subscriptions so closing or disconnecting a browser
  releases resources even when the process produces no output.
- Bound browser buffering and handle slow consumers without blocking the child
  process. Provide access to retained history beyond terminal scrollback.
- Define retention explicitly: do not silently remove earlier retry output.
  Preserve all output within the retention policy and visibly report truncation.
  Full indefinite history would require changing the existing one-backup policy.

Primary files: `internal/daemon/server/logs.go`,
`internal/daemon/server/logwriter.go`, `internal/daemon/client/client.go`,
`internal/server/localapi/processes.go`, and new browser stream API code.

### 3. Render a persistent process log terminal

- Replace the static process status body with a `ProcessTerminal` using xterm.
  Follow the existing agent terminal's sizing and cleanup behavior.
- Append ANSI-colored output without resetting the display on status changes,
  automatic retries or manual restarts.
- Convert stored lifecycle markers into readable separators, retaining their
  order relative to command output:

  ```text
  Started · pnpm run tauri dev
  …command output…
  Exited with code 2 · retry 1/5 in 1s
  Restarted · attempt 2
  …command output…
  Failed · retries exhausted
  ```

- Keep failed and completed output visible. Show lifecycle status, connection
  state, retry count, exit code and errors.
- Follow incoming output while the user is at the bottom; preserve their scroll
  position while they inspect earlier output.
- Add stop and restart controls with pending/error feedback. Stopping during
  backoff cancels the pending retry. Closing the card only closes its view.

Primary files: `web/src/features/terminal/BottomWorkspace.tsx`, a new
`ProcessTerminal.tsx`, `web/src/api/processes.ts`, and process types/reconciliation.

### 4. Automatically open each newly started process

- Return a typed browser process summary from the start endpoint, including the
  stable browser ID, daemon ID, project and worktree ownership.
- Insert the response into the store immediately and open its card without
  waiting for snapshot refresh.
- Select the target project/worktree, reveal a collapsed dock, and focus the new
  process while retaining existing runtime cards.
- Begin log attachment as soon as the response arrives, independently of terminal
  rendering. Saved replay covers output produced before the response arrives.
- Reconcile snapshots and stream updates by stable ID and ordering/version data,
  preventing duplicate cards or stale snapshots overwriting newer lifecycle state.
- Keep separate processes isolated even when their labels or commands match.

Primary files: `web/src/features/workspace/StartProcessDialog.tsx`,
`web/src/stores/bonsai.ts`, `web/src/api/snapshotReconciliation.ts`,
`internal/server/localapi/snapshot.go`, and `internal/server/localapi/processes.go`.

### 5. Correct tool-specific argument forwarding

- Assign forwarding strategies per provider instead of sharing npm's separator
  with pnpm. Correct the observed case to `pnpm run tauri dev`.
- Verify npm, pnpm, Yarn and Bun separately using controlled argv fixtures and
  the supported tools' behavior.
- Preserve Cargo's distinction between Cargo options and arguments passed to
  the resulting executable. Audit other providers and custom invocation modes.
- Keep shell-free program/argv execution and preserve each argument literally,
  including spaces. Keep the modal's one-argument-per-line input convention.
- Show entered arguments and the actual forwarding separator in the command
  preview. Keep backend resolution authoritative and ensure the preview agrees
  with it.
- Replace the test that currently expects incorrect pnpm forwarding. Invalidate
  cached discovery data if it can retain the old invocation strategy.

Primary files: `internal/core/pkgmgr/node.go`,
`internal/core/pkgmgr/invocation.go`, provider/cache tests,
`web/src/api/processes.ts`, and `StartProcessDialog.tsx`.

### 6. Expose restart policy in the Start process modal

- Offer **Use project default**, **Never**, **On failure**, and **Always**.
- Show the resolved project default rather than leaving inherited behavior
  implicit.
- Expose maximum retries for automatic restart modes. Explain that the limit
  counts additional attempts after the initial launch.
- Represent inheritance separately from an explicit policy; omit an override
  when the user chooses the project default.
- Validate policy mode and retry limits on the backend and pass explicit policy
  to the daemon. Use consistent validation rather than silently replacing invalid
  limits with defaults.
- Include effective policy and retry count in browser summaries and live status.
- Preserve selected policy while changing commands or refreshing discovery,
  subject to resolving any changed inherited default.

Primary files: `StartProcessDialog.tsx`, `web/src/api/processes.ts`,
`internal/server/localapi/processes.go`,
`internal/server/localapi/snapshot.go`, and process policy validation.

## Validation and acceptance criteria

- A process that prints and immediately exits before browser attachment shows its
  initial output, final output and exit details.
- An executable-not-found launch remains available as a failed process with a
  visible diagnostic.
- Automatic retries append output and attempt separators to one window, including
  failures during subsequent launch attempts and retry exhaustion.
- Manual restart reuses the window and retains earlier output.
- Several simultaneously running processes have isolated logs. Each new launch
  opens its own card, including a launch in another worktree or project.
- Disconnect/reconnect, browser reload, log rotation and stream cancellation
  produce no duplicated or silently missing retained output.
- Partial lines and output without a final newline appear promptly; ANSI/UTF-8
  chunk boundaries render correctly.
- Scrolling up does not jump back to the bottom on every output update.
- Stop cancels backoff; closing a card leaves the process running.
- Inherited, never, on-failure and always policies produce the configured behavior.
  Invalid modes and retry limits are rejected with useful modal feedback.
- Tool-specific argv tests cover subcommands, flags, arguments containing spaces,
  empty passthrough input, and Cargo's executable argument separator.
- Browser integration tests cover launch → auto-open → replay/live output → retry
  → terminal failure, plus multiple process isolation.

Run focused Go tests for daemon lifecycle/log streaming, local API and package
manager resolution; web tests for the modal, store, stream client and terminal;
then web type checking and the relevant full-stack browser tests.

## Delivery order

1. Preserve failed launches and define stream identity/replay/retention contracts.
2. Implement daemon and local API streaming with lifecycle updates.
3. Implement the process terminal and automatic opening/reconciliation.
4. Fix provider-specific argument forwarding and command previews.
5. Add restart policy controls and effective-policy reporting.
6. Run focused verification and full-stack acceptance scenarios.
