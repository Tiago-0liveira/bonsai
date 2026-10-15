# internal/ guide (Go)

## Layering

- `ui` imports `core/*`, `git`, `daemon/client`, `version` only. `core/*` never imports `ui`, `cli`, `server` or `daemon`. `providers/*` import `core/agents`, never `server` or `ui`. `agentruntime` is the only place that wires providers.
- `server/localapi` is the only HTTP/WS surface for the web. Business logic belongs in `core`, `git`, `daemon`; handlers parse, authorize, call, serialize. Keep `security.go`/`sessions.go` checks on every new route (session capability, exact Host/Origin, strict JSON, idempotency for mutations).
- Daemon is Git/process authority per repo (`daemon/protocol` is the wire contract; change client, server and protocol together). `gitbridge` adapts daemon calls to `internal/git`.
- Stores (`core/agents/*_store.go`, `procstore`, `storage/git`) use atomic replace + cross-process locks. Do not write JSON state with plain `os.WriteFile`.
- Injected seams over globals: fake `CommandRunner`, stores, clocks, binary resolvers. Tests never touch the real network, real `$HOME`, real `gh` or real provider CLIs.

## Platform-specific files

- Pattern: `x_unix.go` (`//go:build !windows` or `linux || darwin`) + `x_windows.go` (+ `_other.go`/`_unsupported.go` fallback). Examples: `core/agents/account_lock_*.go`, `core/exec`, `core/procstore`, `daemon/server/exit_*.go`, `providers/antigravity/credentials_{darwin,linux,windows,other}.go`.
- Every new platform file needs its counterparts so `GOOS=windows go build ./...` and `GOOS=linux go build ./...` stay green. Check with `GOOS=windows go vet ./internal/<pkg>/...` when touching them.
- Tests that need POSIX scripts (fake provider binaries) must skip on Windows.

## Testing

- Targeted first: `go test ./internal/<pkg>/...`. Add `-race` for daemon, server, agents, procstore, watcher and anything with goroutines. CI runs `-race` on Linux only.
- Use `-count=1` when a cached pass hides flakiness. Avoid `time.Sleep` for sync: wait on channels/conditions (past flake fixes: provider span waits, stale job waits).
- Browser fixtures (`localapi` `Test*BrowserFixture`) are env-gated and started by Playwright; they are skipped in normal `go test`.
- Goldens: see root `AGENTS.md`.
