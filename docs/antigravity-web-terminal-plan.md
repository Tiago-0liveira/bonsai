# Antigravity web launch and terminal integration

Status: implemented on 2026-10-05. Go checks and race tests pass; frontend
checks pass with 180 unit tests and 30 browser tests, including a real API/PTY
fixture. Windows and macOS cross-builds pass. The manual installed-`agy` smoke
verified launch, input, resize, reload/reattach, Stop and temporary-home cleanup;
the existing profile reported “not signed in”, so authenticated model interaction
remains unverified. See `docs/development.md` for protocol and lifetime details.

## 1. Objective and branch

Allow a user to select an existing Bonsai Antigravity profile, start `agy` in a selected repository worktree, and interact with its real terminal in the web UI. Antigravity is the only enabled provider. Claude and Codex remain visible but disabled with an explanatory label.

- Base branch: `feat/ws-authoritative-state-and-repo-selection-next-react-plan`.
- Base commit: `2ea236a78486abd9859085a0e0386c65a9b61018`.
- Implementation branch: `feat/antigravity-web-terminal`.
- Worktree: `/home/tiagoliv/bonsai-feat-antigravity-web-terminal`.

The existing Antigravity account, credential, isolated-home, and provider invocation logic remains the source of truth. “Profile” in the UI means an existing `agents.Account`, selected by its opaque account ID.

## 2. Findings from this branch

| Area | Existing implementation | Integration needed |
| --- | --- | --- |
| Profiles and provider | `internal/cli/agent.go` constructs stores and registers Antigravity; `internal/providers/antigravity/` handles credentials, settings, environment and executable resolution. | Share construction outside the CLI; expose safe profile summaries. |
| Session lifecycle | `internal/core/agents/session_service.go` creates a session, prepares the provider, runs a foreground launcher, finalizes credentials, and cleans up. | Add asynchronous managed sessions with an interactive launcher while preserving that lifecycle. |
| Process daemon | `internal/daemon/server/proc.go` and the daemon protocol support structured spawning and log output. | No PTY attach, input, or resize protocol exists in the inspected branch. Plain log streaming cannot supply the required terminal. |
| Browser API | `internal/server/localapi/server.go` exposes Git, project, process and settings routes. | Add provider/profile discovery and project-scoped agent start/list/stop/attach. |
| Authoritative state | `snapshot.go`, `state_sync.go`, and `/events` publish project snapshots with epoch and sequence ordering. | Include agent summaries in the same authoritative projection. |
| Browser launch | `StartAgentDialog.tsx` and `stores/bonsai.ts` explicitly report execution unavailable. | Replace those stubs with real API actions and progress/error states. |
| Terminal | `FakeTerminal.tsx` already uses xterm and its fit addon, but redraws stored lines and has no input transport. | Add a real incremental byte-stream terminal component. |
| Provider choices | `mock/agentProviders.ts` declares Codex, Claude, and Gemini as connected; the live dialog does not currently use a real provider catalog. | Replace mock availability with backend capabilities; use the name Antigravity. |
| Execution controls | Canvas context menus, command palette, inspector, and dock contain disabled actions. | Enable supported Antigravity actions consistently. |

## 3. User flow and scope

1. Select a worktree and choose **Start agent** from the existing UI.
2. Show Antigravity selected. Show Claude and Codex as disabled, labeled “Not available yet.” Do not present Gemini as a separate runnable provider.
3. Load existing Antigravity profiles from Bonsai. Require a profile selection, automatically selecting it only when exactly one is available. Show the selected worktree and branch explicitly.
4. Start using that profile's existing settings. Avoid invented model, reasoning, fast-mode, permission, or command options. Initial instructions can be entered directly in the terminal; adding an initial-prompt field requires verifying the actual provider argument contract first.
5. Show pending state while the backend accepts the start. Once the session exists, select its agent node and open the terminal dock automatically. Show “starting” until the process actually starts.
6. Display real ANSI output, accept keyboard input and paste, forward resize changes, and support Ctrl+C through the terminal.
7. Closing a dock panel or losing the browser connection detaches only. Reopening or refreshing reconnects to the existing session while the API remains running.
8. An explicit **Stop agent** stops the process, finalizes the profile, cleans the temporary home, and publishes the final status. Preserve bounded recent output for inspection during the API lifetime.

No profile creation/login UI, generic shell execution, automatic agent restart, or additional provider implementation is required for this increment. Missing profiles should explain the existing setup command: `bonsai agent account add antigravity <name>`.

## 4. Architecture and lifecycle decisions

### Runtime owner

For the first increment, introduce one agent manager owned by the local API server, shared across request-scoped server copies. It owns PTYs, processes, session metadata, output buffers, and subscribers. Request cancellation and WebSocket disconnect must never become the process lifetime context.

Reuse provider preparation/finalization and the current account/session stores through a shared runtime constructor. Extract shared lifecycle helpers only as needed so the CLI foreground path and managed path retain the same credential behavior. Do not shell out to an interpolated `agy` command or duplicate profile materialization in HTTP handlers.

API-owned sessions survive browser reloads and network reconnects, but do not survive an API restart. Graceful API shutdown must stop and reap managed children before completing cleanup. Expose that lifetime truthfully; daemon-owned sessions surviving API restarts would require a separate daemon PTY protocol extension. Crash recovery must never silently relaunch an agent or claim an old process is still attachable.

### PTY adapter

Introduce an OS-specific terminal process interface with start, read, write, resize, stop, and wait operations. Launch the provider's `PreparedSession` executable and argument vector directly, preserving its working directory and `BuildEnvironment` behavior, including environment removal.

Implement Unix PTY support behind build tags. Preserve builds on Windows with an explicit unsupported-terminal capability until a tested ConPTY implementation exists; never substitute a log-only process while claiming an interactive terminal. Verify the chosen PTY package and supported OS APIs during implementation before adding a dependency.

Each managed session has a stable ID, project/worktree/account ownership, timestamps, exit details, and one of `starting`, `running`, `stopping`, `exited`, or `failed`. Do not infer that the model is idle or thinking from terminal output. A disconnected terminal is a separate UI connection state, not a process exit.

Stop is idempotent. Give the child a bounded graceful termination interval, then terminate the process group if necessary and reap it. Run finalization exactly once after process exit with a fresh bounded cleanup context; a canceled run context must not prevent credential reconciliation. Report finalization/cleanup failures without masking the child exit result.

### Ownership and resource limits

Resolve `projectId` and `worktreeId` through the configured project registry and current Git inventory. Derive the working directory on the server. Reject missing worktrees, unavailable projects, mismatched ownership, removed accounts, unsupported providers and platforms. Browser requests cannot supply arbitrary paths, executables, environment maps, or shell strings.

Start with explicit, tested limits: 16 active sessions per API, a 2 MiB recent-output ring per session, 64 KiB maximum input message, terminal dimensions from 1 to 500, and 32 retained completed sessions. Evict only completed sessions. Disconnect slow output consumers rather than blocking the child or allowing unbounded buffering.

Support concurrent sessions for the same or different profiles as advertised by the existing provider. Test credential reconciliation under concurrency. Use one writable terminal attachment per session; additional attachments can observe output and display a read-only notice. Release writer ownership on disconnect; do not silently queue old keystrokes for replay after reconnect.

Before removing a worktree or the last root authorizing a live session, return a clear conflict instructing the user to stop that session. If the directory disappears externally, stop the owned session and publish its failure. Recheck ownership on attachment and mutations.

## 5. Proposed browser contract

Names below are proposed additions, not existing endpoints.

| Endpoint | Purpose |
| --- | --- |
| `GET /api/agents/providers` | Provider IDs, display labels, availability and structured unavailable reasons; only Antigravity may be enabled. |
| `GET /api/agents/accounts` | Safe Antigravity profile summaries: ID, name and provider; no credentials, isolated-home paths, or raw settings. |
| `POST /api/projects/{projectId}/agents` | Start with `{worktree_id, account_id, name?, cols, rows}` and `Idempotency-Key`; return accepted session summary. |
| `GET /api/projects/{projectId}/agents` | Authoritative list/recovery view, using the same summaries as project snapshots. |
| `DELETE /api/projects/{projectId}/agents/{sessionId}` | Idempotent stop with `Idempotency-Key`; completion follows through authoritative state. |
| `GET /api/projects/{projectId}/agents/{sessionId}/terminal` | WebSocket attach to a known session. |

Account/provider discovery is global; sessions are project-scoped. Distinguish agent session IDs from the browser capability sessions already implemented in `localapi/sessions.go`.

Use the existing strict JSON, request-size, Host, exact Origin, CORS, and capability rules. HTTP uses `X-Bonsai-Session`. Extend the WebSocket middleware exception only for the precise terminal route: authenticate in the first frame with the existing timeout, before subscribing to output or accepting input. Never put capability tokens in URLs. Refresh browser capabilities through the existing local client before reattachment.

Require stable mutation keys across retries. Concurrent identical start requests produce one process and one session ID; reusing a key with different payload returns 409. Persist non-secret request/result metadata using existing atomic storage patterns. On API restart, an old accepted key must resolve to a terminated/interrupted result, never a replacement process. A start left uncertain by a crash requires a new explicit user action, not automatic replay. Make this reconciliation happen before accepting new starts.

Agent summaries include IDs, provider, profile display name, lifecycle state, timestamps, exit code and safe error messages. Never serialize a raw account or `PreparedSession` into browser state.

### Terminal stream

Use a dedicated socket so output volume cannot delay Git/project snapshots. Define and document versioned frames:

- Client: `authenticate`, `attach` with replay cursor, `input`, `resize`.
- Server: `ready` with stream generation/writer status, `output` with byte offsets, `status`, `gap`, `error`.

Encode output bytes losslessly, such as base64 in the JSON output envelope, and pass decoded `Uint8Array` data to xterm. Preserve ANSI sequences, carriage returns, and split UTF-8 sequences; never convert output into lines or render it as HTML.

Take the replay snapshot and register the live subscriber atomically, so attach loses no bytes and duplicates none. Reconnect resumes from the last received offset. If the cursor predates the retained ring, emit an explicit gap/reset notice and replay the retained tail; bounded replay does not promise perfect reconstruction of a full-screen terminal after truncation. After exit, drain remaining output before the final stream status. Include a generation change when the backend restarts so old cursors cannot attach to a new stream accidentally.

## 6. Ordered implementation steps

### Phase 1 — Shared runtime and managed terminal lifecycle

- Extract reusable runtime construction from `internal/cli/agent.go` into an agent runtime package outside `cli`.
- Add the managed session service and PTY adapter without changing provider account setup or credential formats.
- Preserve the CLI foreground launcher and cover its existing lifecycle during the extraction.
- Implement bounded output storage, input, resize, single-writer attachment, stop, exit, and shutdown cleanup.
- Add deterministic helper-process tests that do not require real credentials or contact an external model.

**Exit criterion:** a test session starts in the requested worktree, accepts terminal input and resize, produces ANSI output, and finalizes/cleans up exactly once on both normal exit and stop.

### Phase 2 — API, authorization, and authoritative state

- Add `internal/server/localapi/agents.go` and `agent_terminal.go`; wire the shared manager into `Server`, route registration and shutdown.
- Add provider/account discovery, strict start/stop contracts, ownership checks, request deduplication and crash reconciliation.
- Update `security.go` for precise first-message terminal authentication and add route-level tests.
- Extend `snapshot.go`, `state_sync.go`, snapshot cloning/equality, and lifecycle-triggered publication with authoritative agent summaries and freshness.
- Ensure session changes publish even without browser subscribers. Initial bootstrap and later reconnect must discover existing sessions.
- Preserve the existing project epoch/sequence ordering; terminal byte offsets are separate.
- Keep protocol 3 through additive capability negotiation: a new client talking to an older backend leaves execution disabled with “Update Bonsai to enable Antigravity.” If implementation requires a breaking snapshot change, bump both protocol constants together instead.

**Exit criterion:** authenticated API integration tests start, list, attach to, and stop one real helper process; duplicate requests do not spawn twice; project snapshots contain the correct session.

### Phase 3 — Real profile selection and launch controls

- Add typed `web/src/api/agents.ts` and safe authenticated terminal connection helpers alongside `localClient.ts`.
- Replace production reliance on `mock/agentProviders.ts`; separate provider IDs from display names and support `antigravity` explicitly in `types/index.ts`.
- Rebuild `StartAgentDialog.tsx` with profile loading, worktree selection/context, submit pending state, retryable errors, and actionable missing-profile/missing-binary messages.
- Replace `createAgent` and execution stubs in `stores/bonsai.ts` with API-backed actions. Keep a pending request keyed by its idempotency key; do not manufacture an active agent locally.
- Reconcile sessions into `agents` and `worktree.agentIds` in `snapshotReconciliation.ts`, scoped by project. Handle the snapshot arriving before the POST response and vice versa.
- Update the command palette, workspace nodes, inspector and dock entry points. Agent terminals become available; generic shell and arbitrary command execution retain their existing unsupported state.
- Keep Claude/Codex disabled for mouse, keyboard, command palette and API requests. Render Antigravity with an explicit badge rather than the current generic Gemini fallback.

**Exit criterion:** starting from any supported worktree entry point sends the chosen real account ID and correct worktree ID, then opens the backend-created agent; unsupported providers cannot be launched.

### Phase 4 — Interactive terminal and reconnect

- Add `AgentTerminal.tsx` with a transport/controller module. Reuse `FakeTerminal.tsx` styling and the established StrictMode/layout safeguards, but use incremental `terminal.write` and real `onData`/resize handling.
- Mount the real terminal in `BottomWorkspace.tsx`; remove the agent-unavailable placeholder for supported live sessions.
- Keep terminal byte buffers and sockets outside the main Zustand entity store and browser persistence. Terminal output must not trigger whole-canvas reconciliation or localStorage writes.
- Fit and resize only visible, nonzero-size terminals. Clean up listeners, observers and sockets on unmount without stopping the child.
- On reconnect, authenticate again, resume the stream, and show connecting/disconnected/read-only/exited states independently of agent lifecycle.
- Wire explicit Stop to the backend, preserve recent output after exit, and make Open terminal attach to the existing session rather than launch another.

**Exit criterion:** typing, paste, ANSI colors, Ctrl+C, resizing, dock reopen and page reload all work against the same helper session without duplicate input/output or duplicate processes.

### Phase 5 — Verification and documentation

- Add focused frontend tests for provider disabling, account selection, errors, duplicate-submit prevention, project scoping and snapshot races.
- Extend `web/e2e/mockGit.ts` with explicit provider/session capabilities and terminal frames; update `execution-controls.spec.ts` to distinguish supported Antigravity actions from unsupported generic execution.
- Add a full-stack browser test with a deterministic fake provider/PTY helper. HTTP/WebSocket mocks alone do not prove the process-to-browser connection.
- Exercise two worktrees and two profiles, the same profile concurrently, capability failures, slow clients, truncated replay, API shutdown, lost-start responses, and root/worktree removal conflicts.
- Run one manual real-`agy` smoke test with an already configured profile in a disposable worktree: launch from the UI, observe startup, send harmless input, resize, reconnect, stop, and verify temporary session cleanup. Automated tests must never borrow personal credentials.
- Document supported platforms, API-owned lifetime, bounded replay and profile setup in `docs/development.md` and the relevant user documentation.

**Exit criterion:** required checks pass and the actual browser-to-Antigravity path is demonstrated, or any unavailable real-profile smoke test is explicitly recorded as unverified.

## 7. Validation commands and acceptance checklist

Follow the repository's Go version and frontend package manager. Run focused tests during each phase, then the boundary checks required by `docs/development.md`:

```sh
go vet ./...
go build ./...
go test ./...
go test -race ./internal/core/agents/... ./internal/providers/antigravity/... ./internal/server/localapi/... ./internal/daemon/client/...
```

Include any new runtime/terminal package in the race test command. Run `gofmt` on changed Go files. From `web/`:

```sh
pnpm install --frozen-lockfile
pnpm typecheck
pnpm lint
pnpm test
pnpm build
pnpm test:e2e
```

- [x] Only Antigravity is selectable; Claude and Codex clearly appear disabled.
- [x] Existing Bonsai profiles populate the selector without exposing secrets.
- [x] The child uses the chosen profile's isolated environment and selected worktree directory.
- [x] Start retries and repeated clicks cannot create duplicate sessions.
- [x] Real interactive terminal output, input and resizing work end to end.
- [x] Closing/reopening the dock or refreshing the browser attaches to the same running process.
- [x] Stop/exit/shutdown reap children, finalize credentials and clean session directories once.
- [x] Session metadata reconciles through authoritative snapshots without cross-project leakage.
- [x] Replay gaps, backend restarts, unsupported platforms and missing setup show honest states.
- [x] Existing Git, worktree, managed-process and browser-security behavior remains covered.

## 8. Suggested implementation commits

1. `refactor(agents): share runtime construction and session lifecycle`
2. `feat(agents): manage interactive Antigravity PTY sessions`
3. `feat(localapi): expose scoped agent lifecycle and terminal transport`
4. `feat(web): launch Antigravity using existing profiles`
5. `feat(web): attach interactive agent terminals and reconnect`
6. `test(agents): verify full-stack launch, terminal and cleanup`

Implement in that order. Complete the working helper-process vertical slice before polishing provider cards or expanding configuration controls.
