# Track A (part 2): record watching, web adoption, real TUI backend

Read first: `00-overview.md` (decisions 1, 2, 6, 8).
Branches: `feat/tui-agents-a2` (from `feat/tui-agents`) → `feat/tui-agents-a3` (stacked). Worktrees `~/bonsai-tui-agents-a2`, `~/bonsai-tui-agents-a3`.
Runs in parallel with B2 and C2/C3. **Must not touch** `internal/ui/**` (except importing `internal/ui/agentsapi`) or `main.go`.
Concurrency/locking work: steps are `[you]` unless tagged. `go test -race` every touched package.

## Facts (verified at 1beaf2b)

- `agenthost.Starter.Start(ctx, StartRequest{Project, Tree, Name, Dir, AccountID, Cols, Rows, Launch}) (Record, error)` (`internal/core/agenthost/start.go:21-60`): validation, flock admission, `Sessions.Create`, launch, returns record with state `starting`. Summary sets `Origin: s.Origin` (`start.go:118`). `Manager` builds its own `Starter{Origin:"web"}` (`manager.go:60`).
- Records: `<web home>/agents/<id>.json` + `.lock` (flock held = alive). `agenthost.List(p)` (`record.go:147`) returns all records with `Alive`; `Read(p, id)` (`record.go:111`); `Paths.ensure()` (`record.go:70`); `DefaultPaths()` (`record.go:58`). Ended records are kept 24 h (`Prune`, `record.go:172`).
- Client side: `Dial(record, Request) (*Conn, Frame /*ready*/, error)`, `Conn.Send/Recv/Close/SetDeadline`, `Query(record) (Info, error)` with `Info.Cols/Rows/Clients`, `Stop(record, reason)`, `Wait(p, id, timeout)` (`client.go:19-113`). `StopHolder(p, r, reason)` (`manager.go:318`).
- Hello `Request{Type:"hello", Mode: ModeTerminal, Offset, Generation, Take, ReadOnly, Client}`; requests `input|resize|take|stop|info`; frames `ready|output|gap|status|writer|info|error` (`protocol.go:33-75`). `Request.Client` comment says "web or cli": add "tui".
- Frame handling in existing clients: CLI attach switch `internal/cli/agents.go:344-373`, Manager attach `manager.go:600-615`, follow `manager.go:137-150` (has `default:`). Check each for unknown-type tolerance before adding a frame type (A3 step 6).
- `Manager.Adopt() map[string]Summary` (`manager.go:429`), `Manager.Restore(summary)` (`manager.go:405`); called only from `recoverAgents` (`localapi/agents.go:475`) at `server.go:202`.
- `agentruntime.New(in, out, errOut) (*Runtime, error)` (`internal/agentruntime/runtime.go:22`) gives `Accounts, Sessions, Registry, AccountService, SessionService, UsageService` (UsageService TTL 1 min, WorkerLimit 8). The CLI wraps it in `newAgentRuntime` (`internal/cli/agent.go:24`).
- Web launch options: `agentRequest.launchOptions()` (`localapi/agents.go:408`) maps request fields 1:1 into `agents.LaunchOptions{Model, Prompt, FullAccess, PermissionMode, Effort, Sandbox, Approval}`; check what the web client sends for untouched fields (`web/src/features/agents/`, launch dialog) so the TUI sends the same shape.
- fsnotify v1.9.0 is in `go.mod`.
- Contract to implement: `internal/ui/agentsapi/agentsapi.go` (`Sessions`, `Stream`, `Accounts`, `Backend`). Fake with the same semantics: `internal/ui/agentsapi/fake/fake.go` (read it: tests in D will assume the real backend behaves like the fake).

## A2: watch records, web adopts live

### Step 1: `internal/core/agenthost/watch.go`
`func Watch(ctx context.Context, p Paths) (<-chan struct{}, error)`
- `p.ensure()`, fsnotify on `p.Records`; react to `*.json` create/write/rename/remove; coalesce 100 ms; non-blocking send on a 1-buffered channel.
- Fallback rescan every 5 s (unexported `var rescanInterval` for tests): fingerprint `(id, Summary.State, Alive)` over `List(p)`, signal on change. Needed because a holder that dies without rewriting its record only releases a flock, which fsnotify cannot see.
- If fsnotify setup fails (inotify limit), log nothing, keep the rescan only.
- Close the channel when ctx ends; no goroutine left behind (test it).
- Platform: holders are Unix-only; `watch_windows.go` returns a channel that closes on ctx done (or reuse the generic file if it compiles and behaves: `agentterminal.Supported` false short-circuits).
- Tests: create / rewrite / remove a record file → signal within 1 s; dead holder (release lock, record still says running) → signal within one rescan interval (set it to 50 ms); burst of 20 writes → ≤ 2 signals; ctx cancel closes channel.

### Step 2: Manager adopts at runtime
`func (m *Manager) WatchRecords(ctx context.Context)`: loop over `Watch`; on each signal `Adopt()`, plus `Restore(summary)` for **ended** records the manager does not know (a TUI session that ended before the web saw it shows as history), then `notify(project)` for each project whose set of known IDs or states changed. Exits on ctx done. Test with an in-process launcher: second `Starter{Origin:"tui"}` sharing `Paths` starts a session → manager lists it within 2 s; external stop → manager shows it ended.

### Step 3: localapi
- Start `go s.agents.manager.WatchRecords(serverCtx)` right after `recoverAgents()` (`server.go:202`). Stops with the server context.
- Newly adopted sessions not in the project journal get an `api_agent_mutations` entry in the existing `agentMutation` shape, key `adopted-<sessionID>`, so history survives API restarts. No new routes; no change to any route check.
- Test (existing `agents_host_test.go` fixture style): server running, start via separate `Starter{Origin:"tui"}` with the server's `Paths` (in-process launcher + fake provider) → `GET /api/projects/{id}/agents` contains it within 2 s and the agents state-sync event fires; stop it externally → list shows ended.

### Step 3b [you]: A1 review leftovers (one commit, `fix(agents): ...`)
Known minors from the A1 review: the `launched` flag is set only after the launcher returns; failed-start cleanup waits while holding `admit.lock`; `Manager.Start` ctx is not cancelled on API shutdown. Fix each or record in the PR why it stays.

### Step 4 [docs]: `docs/agents.md`
docs-writer: web and TUI share holders; `origin` field; `bonsai web stop` with `keep_running=false` stops only web-started agents (A1 behavior, undocumented so far); TUI-started sessions appear in the browser live.

### A2 done when
`go test -race -count=3 ./internal/core/agenthost/...`, `go test -race ./internal/server/localapi/...`, global checks (`00-overview.md`). reviewer (ask: watcher goroutine exits on every path; no double adoption; journal key collision impossible; no route behavior change). Commits: `feat(agents): watch agent host records for changes`, `feat(localapi): adopt agent sessions started outside the api`, `docs: explain agent session origin and keep_running`.

## A3: `internal/agentsbackend` (implements `agentsapi.Backend`)

Starts after A2 **and C2 step 0** (`agentsapi.Defaults.LaunchFields`, `agents.LaunchOptionsFrom`) are merged into `feat/tui-agents`; rebase `a3` onto it.

New package. Imports `internal/agentruntime`, `internal/core/agenthost`, `internal/core/agents`, `internal/core/agentterminal`, `internal/ui/agentsapi` (dependency-free contract; importing it from outside `ui` is intended). `ui` never imports this package; `main.go` does (D3).

### Step 5: construction
```go
// Backend owns the agent runtime and every attachment the TUI opened, so the
// TUI can release them on exit without stopping any holder.
type Backend struct { /* runtime, paths, starter, selfExe, mu, streams map[*stream]struct{} */ }
func New() (*Backend, error)
func (b *Backend) API() agentsapi.Backend
func (b *Backend) Close() error // closes open streams; never stops holders
```
- Runtime: `agentruntime.New(nil, io.Discard, io.Discard)`. Provider setup flows are never run from here (add/login go through `AddCommand`/`LoginCommand`), so no stdio is needed. If the CLI's `newAgentRuntime` adds wiring that matters for sessions/usage (read `internal/cli/agent.go:24-40`), move that part into `agentruntime` in this commit instead of duplicating it.
- `starter = agenthost.Starter{Accounts, Sessions, Registry, Paths, Origin: "tui"}`. `selfExe` from `os.Executable()`.
- On Windows / empty `Paths.Records`: `New` still succeeds; `Sessions.Supported()` false (the tab explains why).
- Test seams: `New(opts ...Option)` with `WithPaths(agenthost.Paths)`, `WithLauncher(agenthost.Launcher)` (in-process launcher), `WithRuntime(*agentruntime.Runtime)` (temp stores + fake provider). D3's integration test builds the backend with these; production uses none.

### Step 6: Sessions
- `Supported()`: `agentterminal.Supported && paths.Records != ""`.
- `List(ctx)`: `agenthost.List(paths)` → every record (alive and ended within retention) → `agentsapi.Session{Summary, WorkDir: filepath.Clean(record.WorkDir)}`. The UI already filters by project and state.
- `Changes(ctx)`: `agenthost.Watch(ctx, paths)`.
- `Start(ctx, req)`: map `agentsapi.StartRequest` → `agenthost.StartRequest{Project: req.ProjectID, Tree: req.WorktreeID, Dir: req.WorkDir, ...}`; `starter.Start`; return the record's summary. Launch options must match the web for the same choices (fact above): same struct, same zero-value semantics; add a test that builds both from identical choices and compares `Values()`.
- `Stop(ctx, id)`: `Read(paths, id)` → `StopHolder(paths, record, "")`. Unknown id → sentinel `ErrSessionNotFound` in `errors.go`.
- `Attach(ctx, id, opts)`: `Read` → `Dial(record, Request{Mode: ModeTerminal, Client: "tui", ReadOnly, Take, Offset, Generation})` (honor ctx with `SetDeadline` during dial). Return `*stream`:
  - reader goroutine: `Recv` → convert with `Frame.TerminalFrame()` → `frames` chan (cap 256). **Blocking send** (socket backpressure; never drop output). On `writer` frames update an `atomic.Bool`. Exit on Recv error or `done`; close `frames` once.
  - `Writer()` initial value from the `ready` frame (check which field carries it; Manager's `attach` at `manager.go:574` shows how).
  - `Size()`: from `Query(record)` at attach. Live size: if every existing client ignores unknown frame types (check CLI switch `agents.go:344-373`, Manager `manager.go:600-615`, and the web terminal client under `web/src/features/agents/`), add an additive `size` frame (`Cols`, `Rows` on `Frame`) the holder sends after hello and after each accepted resize; else poll `Query` every 2 s while attached read-only. Document the choice in the PR body.
  - `Input/Resize/Take` → `Conn.Send(Request{Type: ...})`. `Resize` from a non-writer returns the holder's error unchanged.
  - `Close()`: idempotent; close conn, signal done, wait for reader. Deregister from `Backend.streams`.
- Compile-time checks: `var _ agentsapi.Sessions = (*sessions)(nil)`, `var _ agentsapi.Stream = (*stream)(nil)`, `var _ agentsapi.Accounts = (*accounts)(nil)`.

### Step 7: Accounts
Wrap `AccountService`, `UsageService`, `Registry`:
- `List`: accounts from `Accounts` store + `Running` = active alive records per account (`List(paths)`). Fast: no describe.
- `Describe(ctx, id)`: provider's `agents.AccountDescriber` (type-assert from registry); ctx honored; not a describer → zero info, nil error.
- `Usage(ctx, id, refresh)`: `UsageService.Account` (refresh flag bypasses TTL). On error: return the last good snapshot (cache) with `Stale=true` and `Err` sanitized: short message, never tokens, paths, env values or raw HTTP bodies. Only a successful empty result clears data (state invariant).
- `Defaults(ctx, id)`: provider `ProfileEditor.ProfileFields/ProfileDefaults` + `LaunchSchema.LaunchFields` (into `Defaults.LaunchFields`, added by C2 step 0) + `ModelLister.Models`; missing interfaces → empty fields (UI renders nothing editable). `Models` errors → static fallback the provider returns, never fail the whole call.
- `UpdateDefaults(ctx, id, values)`: `AccountService.UpdateDefaults` (nil value = reset to provider default).
- `Remove(ctx, id)`: refuse while `Running > 0` with sentinel `ErrAccountInUse`; else `AccountService.Remove`.
- `AddCommand(provider, name)` → `exec.Command(selfExe, "agent", "account", "add", string(provider), name)`; `LoginCommand(id)` → `exec.Command(selfExe, "agent", "account", "login", <provider>/<name>)`. Verify exact argv against `internal/cli/agent*.go` help. No env changes (CLI scrubs).

### Step 8: tests
- [you] (`//go:build !windows`, in-process launcher + fake provider script; reuse A1's helpers in `start_test.go`): read-only attach receives replay and `Input` is rejected; auto attach alone becomes writer; second attach with `Take` demotes the first (writer frame seen); observer `Resize` rejected; `Close` while reader is blocked on a full `frames` chan returns and leaves no goroutine (counter or `runtime.NumGoroutine` settle loop, no sleep-based sync); `Changes` fires when another `Starter` starts a session; `Start` produces a record with `Origin=="tui"`.
- [builder] Accounts adapter with fake stores/provider (existing fakes in `internal/core/agents` tests): stale usage on error keeps last snapshot, sanitized `Err` contains no path; defaults round trip; remove refused while running; command argv.

### A3 done when
`go test -race ./internal/agentsbackend/... ./internal/core/agenthost/...`, global checks. reviewer (ask: secrets never in errors; reader exits on every path; `Close` never stops holders; launch Spec identical to web). Commits: `feat(agents): add tui backend for agent sessions and accounts`, plus `feat(agents): announce terminal size changes to observers` if step 6 adds the frame.

## Only touch
`internal/core/agenthost/**`, `internal/server/localapi/agents*.go`, `internal/server/localapi/server.go` (watcher start only), `internal/agentruntime/**`, `internal/cli/agent.go` (only to move shared runtime wiring), new `internal/agentsbackend/**`, `docs/agents.md`.
