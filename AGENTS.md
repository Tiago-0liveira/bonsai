# AGENTS.md

Bonsai: Go CLI + Bubble Tea TUI + per-repo daemon for git worktrees, plus a local web UI (React/Vite/TS in `web/`) served by `bonsai web` from the loopback API at `/app` and embedded into release binaries. It also manages AI coding-agent profiles (Claude, Codex, Antigravity): accounts, sessions, usage.

Module `github.com/Tiago-0liveira/bonsai`, Go per `go.mod` (1.26). Node 22.12.0, pnpm 12.9.1.

## Repo map

- `main.go`: entry. `internal/cli`: subcommands (`agent`, `web`, `list/create/copy/alias`, process cmds `spawn/ps/logs/kill/...`, `daemon`, hidden `__serve-dev-stack`, `__dev-webhook`).
- `internal/ui`: Bubble Tea TUI only (model/update/view, `components/`, `theme/`, `websetup/` wizard). No `os/exec`, file IO or raw git in the TUI: call core via `tea.Cmd` (`cmds.go`). Known exception: `ui/websetup/backend.go`.
- `internal/core/*`: plain Go packages: `git`, `gh`, `config`, `exec`, `fs`, `pkgmgr`, `procstore`, `updater`, `editors`, `termscreen`, etc.
- `internal/core/agents`: provider-neutral agent model (Provider interface, Registry, accounts, sessions, usage service/cache, launcher). `agentsetup`: setup jobs. `agentterminal`: PTY sessions for agents.
- `internal/providers/{claude,codex,antigravity}`: one package per provider, implement `agents.Provider`. Wired in `internal/agentruntime/runtime.go`.
- `internal/daemon/{server,client,protocol,gitbridge,watcher}`: per-repo daemon, Git/process authority.
- `internal/git/{local,github}`, `internal/storage/git`: git backend and atomic JSON stores.
- `internal/server/localapi`: loopback HTTP/WS API, projections, warm start, live hooks. `webhooks`: loopback webhook receiver. `webui`: serves embedded UI. `runtime`: thin `RunAPI` wrapper over localapi.
- `internal/websetup`, `internal/webtunnel`: `bonsai web` setup checks/plan and tunnel presets (cloudflared, ngrok, ...). `internal/livehooks`: repo webhook registration.
- `web/`: React/Vite app. `web/embed.go` (tag `embedui`) embeds `web/dist`; `embed_stub.go` is the untagged placeholder.
- `docs/`: architecture, development, git-backend, performance, releasing, web (+ `docs/evidence/**`, generated, do not read).
- Area guides load on demand: `web/AGENTS.md`, `internal/AGENTS.md`.

## Invariants (do not break)

- API binds loopback only. Exact Host and Origin checks (listen origin and `localhost:<port>`), no wildcard CORS, no public origins. Dev mode authorizes one exact Vite origin and keeps every other check.
- Capability sessions: `POST /api/session` token in `X-Bonsai-Session`, memory only (never cookies, URLs, persistent storage). WebSockets authenticate in the first data frame. No generic shell/exec endpoint. Mutations keep strict JSON parsing, body limits, idempotency, ownership and locks.
- Webhook receiver: only `POST /github/webhook`, HMAC before parsing, 2 MiB cap, allowlists, dedupe. Never expose API, daemon sockets, Vite or project services through a tunnel.
- State ordering: per-project epoch/sequence. Stale reads and late events must never overwrite newer state. Provider failure keeps last good data as stale; only a successful empty result clears a list.
- Persisted warm-start/browser data is sanitized: no tokens, credentials, process commands, terminal output, env values.
- Agents: profiles run with isolated per-account config dirs (`CLAUDE_CONFIG_DIR`, `CODEX_HOME`); inherited provider env vars are scrubbed; `HOME` is not overridden. Never write the user's `~/.claude`, `~/.claude.json` or `~/.codex`: read-only seeding source only. Never refresh a provider login or token (read stored tokens only; the provider CLI refreshes). Secrets are never logged or echoed. Providers do no direct stdio in setup: use `SetupIO`.
- Web: keep lazy loading and chunk-failure recovery (explicit reload, no loop). Production build has no test hooks (`e2e` mode only) and no cloud fallbacks. Bundle budgets in `web/bundle-budget.json` are enforced against historical revisions: raising one needs a written rationale in that file and `docs/performance.md`.
- Plain `go build ./...` and `go test ./...` must not need Node.

## Commands (run from repo root)

Go, cheap first: `go test ./internal/<pkg>/...`, then before finishing `gofmt -l $(git ls-files '*.go')` (must print nothing), `go vet ./...`, `go build ./...`, `go test ./...`. Concurrency/daemon/server/agents changes also: `go test -race ./internal/<pkg>/...`. Changed `go.mod`: `go mod tidy` (CI fails on diff).

Web (needs `pnpm --dir web install --frozen-lockfile`): `pnpm --dir web typecheck`, `lint`, `test` (vitest), `test:bundle`, `build`, `bundle:check`. Embed check after build: `go test -tags embedui ./web/`. e2e: `pnpm --dir web exec playwright install chromium` once, then `pnpm --dir web test:e2e` (slow; starts Go fixtures on ports 7011-7014).

Which to run:
- Go only: targeted tests, then full Go suite.
- `web/src` only: typecheck, lint, targeted `pnpm --dir web exec vitest run <path>`, then `test`. Run `build` + `bundle:check` if imports/chunks/deps changed. e2e only if browser behavior or `web/e2e` changed.
- API contract change (Go `localapi` + web `src/api`): both stacks.
- Docs only: no tests; verify every command and path you cite exists.

Dev stack: `bonsai __serve-dev-stack` (hidden; API 7001, webhook 7002, Vite 7003). See `docs/development.md`.

## Golden files

- `internal/cli/testdata/account-add.golden`: `go test ./internal/cli -run TestAccountAddOutputGolden -update`.
- `internal/cli/testdata/usage-three-providers.golden`: no `-update` flag; edit by hand to match the test output.
- `internal/ui/websetup/testdata/*.golden`: `go test ./internal/ui/websetup -run Golden -update`.
- Review the golden diff after updating; never update to make an unexplained failure pass.

## Conventions

- Commits: conventional, with scope: `feat(agents): ...`, `fix(web): ...`, `test(web): ...`, `docs: ...`, `fix(ci): ...`. Lowercase imperative subject, no trailing period. One concern per commit.
- PRs carry at most one release label: `release:patch|minor|major|none` (`none` publishes no release; unlabeled defaults to patch). See `docs/releasing.md`; CI validates it.
- Go: stdlib-first, small interfaces, `context.Context` first arg, wrap errors with `%w`, typed sentinel errors in `errors.go`, table tests, fakes via injected interfaces (see `CommandRunner` in providers). Tests must not touch the real network or the real home dir: use `t.TempDir()` and `t.Setenv`. Platform code in `_unix.go`/`_windows.go` files with build tags; keep every OS compiling.
- TS: strict, function components, zustand stores in `web/src/stores`, features under `web/src/features/<name>`, tests beside source (`*.test.ts(x)`), eslint with zero warnings.
- Comments explain why, not what. No dead code, no speculative abstractions.
- User-visible behavior change: update `README.md` and the relevant `docs/*.md` in the same change. Link to docs, do not copy them.

## Docs

`CONTRIBUTING.md` (checks), `docs/architecture.md`, `docs/git-backend.md` (security boundary), `docs/development.md`, `docs/web.md` (user guide), `docs/agents.md` (agent profiles), `docs/tui-cli.md` (TUI/CLI), `docs/performance.md` (budgets), `docs/releasing.md`, `web/README.md`.
