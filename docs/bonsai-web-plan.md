# Plan: `bonsai web` — local-first web client, fast sync, optional live updates

Status: **proposed** · Baseline commit: `7efada9` · Owner: @Tiago-0liveira

This is a working plan. Execute it one phase at a time (see
[How to use this plan](#how-to-use-this-plan)). Delete it, or fold the
surviving parts into `development.md` / `git-backend.md`, once Phase 8 ships.

## Status

| Phase | Title | Model | Depends on | Status |
| ---: | --- | --- | --- | --- |
| 0 | Baseline and sync instrumentation | Sonnet 5.5 | — | done (#56) |
| 1 | Local and provider sync quick wins | Sonnet 5.5 | 0 | todo |
| 2 | In-process GitHub client and cheap polling | Opus 5.5 | 1 | todo |
| 3 | `bonsai web` command and user-level supervisor | Opus 5.5 | — | todo |
| 4 | Embedded UI and dual browser origin | Opus 5.5 | 3 | todo |
| 5 | Setup TUI and `bonsai web doctor` | Opus 5.5 | 3, 4 | todo |
| 6 | Live updates: local webhook receiver and tunnel | Opus 5.5 | 2, 5 | todo |
| 7 | Warm start and frontend load | Sonnet 5.5 | 2, 4 | todo |
| 8 | Docs, migration and release pipeline | Sonnet 5.5 | all | todo |
| F | Multi-workspace (design only, later) | Opus 5.5 | 8 | not scheduled |

There are two independent tracks. You can run them in parallel worktrees:

```text
Track A (speed):  0 ──► 1 ──► 2 ─────────────┐
                                             ├──► 6 ──► 7 ──► 8
Track B (web):    3 ──► 4 ──► 5 ─────────────┘
```

Update the Status column in the PR that finishes each phase:
`todo → in progress → done (#PR)`.

---

## Goals

1. **One command.** `bonsai web` starts everything, detaches once healthy and
   opens the browser. It only stays in the foreground when something failed to
   start, and then it says exactly why and how to fix it.
2. **Fully local by default.** The web UI is embedded in the binary and served
   from the local API, so there is no public domain, no browser "local network"
   prompt and no version skew. GitHub data comes from the user's existing `gh`
   login and is polled efficiently.
3. **The hosted client keeps working exactly as today.** `https://app.bonsai.dev/app`
   stays a supported entry point.
4. **Optional real-time.** The user can opt in to live updates. A local webhook
   receiver is exposed through a tunnel (cloudflared, ngrok, Tailscale Funnel,
   their own URL or a custom command). Bonsai registers the GitHub webhooks
   itself. Only the webhook receiver is ever exposed.
5. **A setup TUI that explains itself.** The first run of `bonsai web` (and
   `bonsai web setup` later) is a guided, friendly TUI. Every option says what
   you get, what you need, how much setup it takes and what it exposes.
6. **Opening the app feels fast.** Cut the cold-load waterfall, make polling
   nearly free and paint last-known state instantly.
7. **Don't block multi-workspace.** WSL + Windows, or other machines on the LAN,
   should be addable later without rework (Phase F).

## Non-goals (for this plan)

- Multi-workspace implementation (design rails only; see Phase F).
- Removing the hosted relay (`internal/server/relay`). It is frozen, not
  deleted. See [Decision D1](#decisions).
- Built-in GitHub OAuth/device-flow login. `gh` stays the auth source.
- Auto-installing third-party tools (`gh`, `cloudflared`, `ngrok`,
  `tailscale`). Bonsai detects them and shows the exact install command.

---

## Current state (verified at `7efada9`)

| Fact | Where |
| --- | --- |
| `bonsai serve` is per repository. It requires the cwd to be inside a Git repo and is supervised by that repo's daemon. | `internal/cli/serve.go:44`, `internal/core/procstore/procstore.go:196` |
| Production serve refuses webhook/web ports and sidecars. | `internal/daemon/server/serve.go:222` |
| The local API authorizes exactly one browser origin (production must be HTTPS). | `internal/server/localapi/security.go:33` |
| `__serve-api` requires `--repo`. | `main.go:160` |
| The dev stack already runs a local webhook receiver (`__serve-webhook`, port 7002, `/github/webhook`, HMAC, dedupe, normalize) and optional `cloudflared`/`ngrok` sidecars. | `internal/cli/serve_dev.go:227`, `internal/daemon/server/serve_dev.go:159`, `internal/server/webhooks/devrelay.go` |
| The dev receiver streams events to the **browser** over SSE, acting as a fake relay. | `internal/daemon/server/serve_dev.go:170`, `web/src/api/relayClient.ts` |
| The web app already supports runtime-injected config through `<meta name="bonsai-relay-origin">`. | `web/src/api/relayClient.ts:4`, `web/docker-entrypoint.sh` |
| The web router basepath is `/app`. | `web/src/app/router.tsx:39` |
| Every GitHub read spawns a `gh api` subprocess (no keep-alive, no ETag). | `internal/git/github/ghcli/service.go:24` |
| **All local git work runs in the per-repo daemon**, not the API process. `stateSync.gitPayload` sends `git.worktrees`, `git.branches` and `git.repository.refresh` over IPC to `client.For(main)`; `local.Service` and `core.RunContext` execute in `bonsai __daemon`. | `internal/server/localapi/state_sync.go:493`, `internal/daemon/server/git.go:20` |
| Repo, branches and PR catalog reads run serially. | `internal/server/localapi/enrichment.go:97-100` |
| CI checks are fetched serially per worktree (check-runs plus statuses each). | `internal/server/localapi/enrichment.go:412` |
| Worktree status runs serially under the repo lock, with 4 git processes per worktree including `--untracked-files=all`. | `internal/git/local/service.go:166-190`, `internal/git/local/status.go:37` |
| The provider refresh can only start after the full local refresh, because it needs `Remotes`. | `internal/server/localapi/state_sync.go:455-462` |
| WebSocket `ready` refreshes **all** projects at once. | `internal/server/localapi/state_sync.go:183` |
| The projection and provider cache are in memory only, so every restart is cold. | `internal/server/localapi/state_sync.go`, `enrichment.go` |
| A config-editor TUI precedent exists: settings with descriptions and examples. | `internal/ui/configedit.go` |
| The TUI stack is Bubble Tea v1, Bubbles v1 and Lip Gloss v1. | `go.mod` |
| User-level config already lives in `os.UserConfigDir()/bonsai/project-roots.json`: versioned, with a revision, atomic writes and cross-process locks. | `docs/development.md` |

---

## Target architecture

```text
                    ┌──────────── this computer ─────────────────────────────┐
 browser ──────────►│ 127.0.0.1:7001  bonsai API  +  embedded web UI (/app)   │
 (local UI, or      │      │   ▲                                              │
  app.bonsai.dev)   │      │   │ normalized "repo changed" signal             │
                    │      ▼   │                                              │
                    │  git, gh │   127.0.0.1:7002  webhook receiver (opt-in)    │
                    │  (local) │        ▲   ONLY POST /github/webhook           │
                    │          │        │                                       │
                    │          └────────┤                                       │
                    │                 tunnel process (opt-in, supervised)       │
                    └─────────────────────┼────────────────────────────────────┘
                                          │ HTTPS
                         GitHub webhooks ─┘   (repo hooks created by bonsai)
```

- **One user-level stack per machine.** It is started by `bonsai web` from any
  directory and supervised by a user-level daemon home, not by a repository.
- **API process.** It serves the JSON API, WebSocket events and the embedded UI.
  It accepts two browser origins: itself (`http://127.0.0.1:<port>` and
  `http://localhost:<port>`) and, when enabled, the hosted origin.
- **Webhook receiver.** It is a second loopback listener inside the API process
  (D3). Its mux has exactly one route. Verified events become
  `stateSync.Queue(project, refreshProvider, force)`. The browser never talks
  to the receiver.
- **Event sources share one shape.** Polling (default), local webhook and
  hosted relay (frozen) all emit the same normalized invalidation. The
  canonical state is always re-read from GitHub or git.

---

## Global rails (apply to every phase)

**Security boundary** (from `docs/git-backend.md`; do not weaken):
- The API binds loopback only. It keeps exact Host and Origin checks, no
  wildcard CORS, capability sessions in `X-Bonsai-Session`, first-message
  WebSocket auth, strict JSON and idempotency keys on mutations.
- Never tunnel or expose the API port, the WebSocket, the daemon socket or
  project services. The tunnel target is always the webhook receiver port.
  Validate this in code, not only in docs.
- Secrets (webhook secret, GitHub token from `gh auth token`) are never logged,
  never written to user config JSON, never sent to the browser and never put in
  process argv. Secret files are `0600` and live in the user config dir.

**Compatibility:**
- The hosted app (`app.bonsai.dev`) keeps working at every phase boundary.
- Bump `LOCAL_API_PROTOCOL_VERSION` only when unavoidable. Document it in the PR.
- `bonsai serve` keeps working, as a deprecated alias from Phase 3 on.
- Per-repo daemons, TUI and CLI behavior are unchanged unless the phase says
  otherwise.

**Build:**
- `go build ./...` must work without Node or pnpm. Builds without a UI bundle
  serve a placeholder page that explains how to build it.
- Linux, macOS and Windows stay green (CI matrix). Mind Windows paths, process
  groups and the lack of Unix sockets.

**User trust:**
- Nothing on GitHub or the network changes without explicit confirmation in
  the TUI. That covers webhooks, tunnels and public URLs.
- Never run `sudo` or install software for the user. Detect it, explain it and
  show the copy-paste command.
- Every failure message names the cause and one concrete fix.

**Process:**
- One phase = one branch (`feat/web-p<N>-<slug>`) = one PR, with Conventional
  Commits.
- Start every phase in plan mode. Confirm scope against this document before
  writing code.
- The PR description lists exit criteria with evidence (test names, command
  output, measured numbers).
- If reality contradicts this plan, stop and update the plan in the same PR,
  explaining why. Do not silently diverge.

**Validation (run before every PR):**

```sh
gofmt -l $(git ls-files '*.go')
go vet ./...
go build ./...
go test ./...
go test -race ./...
pnpm -C web typecheck
pnpm -C web lint
pnpm -C web test
pnpm -C web build
pnpm -C web test:e2e     # phases touching the browser boundary
```

---

## Decisions

Defaults are chosen. Override any of them before the phase that depends on it
by editing this section.

| # | Decision | Default | Why |
| --- | --- | --- | --- |
| D1 | Hosted relay (`api.bonsai.dev`) | **Freeze.** Keep the code and hide it under "Advanced" in setup. Nothing new depends on it. | Avoids running a cloud service. The tunnel path covers real-time. |
| D2 | User config format | **JSON**: `web.json` with `version` and `revision`, written by bonsai and edited via the TUI | Same pattern and locking as `project-roots.json`. |
| D3 | Webhook receiver placement | **Second loopback listener in the API process** | No IPC, direct `stateSync` access. The single-route mux keeps the boundary. |
| D4 | UI bundle in binary | **`go:embed` in release builds**; placeholder when `web/dist` is absent | Keeps `go build` Node-free. |
| D5 | Webhook registration | **Repository webhooks via the GitHub REST API** using the `gh` token | No GitHub App needed. Works for any repo where the user is admin. |
| D6 | Polling change detection | **REST with ETag conditional requests** (`304`s don't count against the primary rate limit). Not the Events API. | The Events API has documented latency of 30 s to hours, so it can't serve as a change signal. |
| D7 | GraphQL | **Optional** for cold load only. Polling stays REST and ETag. | GraphQL is POST, so it gets no free `304`s. |
| D8 | Default ports | API **7001**, webhook **7002**, both configurable | Matches today. |
| D9 | Supervisor home | **User-level daemon home** under the user state dir (spike in Phase 3) | `bonsai web` must run from any directory. |

---

## Phase 0 — Baseline and sync instrumentation

**Model:** Sonnet 5.5 · **Effort:** medium · **Branch:** `feat/web-p0-sync-trace`

**Objectives**
- Measure where cold-open time goes before changing anything.
- Make it repeatable, so every later phase reports before and after numbers.

**Scope**
1. Timing spans in `stateSync`, emitted as JSON log lines when
   `BONSAI_SYNC_TRACE=1`:
   - `local.inventory`, `local.status` (per worktree), `local.ready`
   - `provider.repo`, `provider.branches`, `provider.prs` (per page),
     `provider.checks` (per worktree), `provider.ready`
   - Each span carries the project ID and duration, plus a process-spawn
     counter (`git` and `gh` separately).
2. A spawn counter hook in `core.RunContext` (git) and the `ghcli` transport.
   Use a package-level atomic counter.
3. Hidden command `bonsai __sync-bench [--projects N] [--cold] [--root DIR] [--repo PATH] [--timeout D]`:
   - Runs the local API sync in-process against the configured project roots.
     Git commands are served by an **in-process** daemon adapter (the same
     `gitbridge.Executor` + `local.Service` wiring as the daemon), because in
     production they run in the per-repo daemon and a counter in the API process
     would read zero git spawns.
   - `--root DIR` benchmarks every repo under DIR using a throwaway roots file;
     `--repo PATH` keeps one project. Neither touches the user's configuration.
   - `--cold` is accepted and currently a no-op: projection and provider state
     are memory-only, so every run is cold. It stays so scripts written now keep
     working once Phase 7 persists state.
   - Prints a table: time to inventory, local ready, PR catalog, all checks,
     and spawn counts.
4. Record the baseline in this file (table below) for:
   - the bonsai repo itself (≥ 5 worktrees)
   - the same repo under `/mnt/c` on WSL, if available
   - one large repo (many branches and PRs) if you have one

**Rails**
- Zero behavior change. With tracing off the overhead must be negligible
  (one atomic add per spawn).
- No new dependencies. No API or protocol change.

**Exit criteria**
- `__sync-bench` runs on Linux and Windows.
- The baseline table is filled in.
- Unit tests cover span emission and the spawn counter.

**Implementation notes**
- Spans: `internal/core/trace`. `BONSAI_SYNC_TRACE=1` writes JSON lines to
  stderr: `{ts, span, project, worktree?, page?, dur_ms, git_spawns, gh_spawns}`.
  The spawn numbers in a span are process-wide deltas, so overlapping spans
  double-count; use the table total from `__sync-bench` for absolute counts.
- `local.inventory` and `local.status` are emitted by `internal/git/local`
  (they carry the repository root as `project`); `local.ready` and every
  `provider.*` span come from `localapi` and carry the project ID (provider
  repo/branches/prs spans carry the `owner/name`).
- Spawn counters: `core.RunContext` (git), the `ghcli` transport and
  `ghcli.Discover` (gh). TUI helpers in `internal/core/gh` are not on the sync
  path and are not counted.
- Measured timings are per project; the "total" row is the max over projects.
  `ALL CHECKS` is when every worktree's CI freshness left `loading`.

**Baseline** (commit `7efada9` + instrumentation, `bonsai __sync-bench --cold`,
WSL2, median of 3 runs, live GitHub through the user's `gh` login)

| Repo | Worktrees | Inventory | Local ready | PR catalog | All checks | git spawns | gh spawns |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| bonsai | 15 | 11 ms | 80 ms | 2173 ms | 15009 ms | 62 | 21 |
| bonsai (/mnt/c, 6 worktrees) | 6 | 472 ms | 8227 ms | 10362 ms | 11794 ms | 29 | 5 |
| large repo (many branches/PRs) | n/a | | | | | | |

Reading the numbers:
- On the Windows drive the local pass is about 100× slower (`/mnt/c` crosses the
  9P boundary), so Phase 1's git-process cuts matter most there.
- Everything after the PR catalog is the serial per-worktree checks loop:
  about 1 s per worktree natively, which is the Phase 1/2 target.
- No large repo was available, so that row stays empty.

---

## Phase 1 — Local and provider sync quick wins

**Model:** Sonnet 5.5 · **Effort:** high · **Branch:** `feat/web-p1-sync-quick-wins`

**Objectives**
- Remove the serial waterfall without changing the data model or protocol.

**Scope**
1. **Start the provider early.** Publish `Remotes` (cheap: `git remote` plus
   `get-url`) in the inventory phase, so `refreshProvider` can start while
   worktree statuses are still running.
2. **Parallel worktree status.** Inside `Service.Repository`, run
   `statusOverview` with a bounded pool (`min(GOMAXPROCS, 8)`). Keep it under
   the existing repo lock: parallel inside the lock, never concurrent with
   mutations.
3. **Fewer git processes per worktree.**
   - Take the upstream SHA from the `for-each-ref` output that
     `ListBranches` already has. This replaces `rev-parse @{upstream}`.
   - Add `%(subject)`, `%(authorname)` and `%(committerdate:unix)` to that
     `for-each-ref`, so a branch's last commit needs no `git log -1`.
     Detached HEAD may still use `log -1`.
   - Resolve the absolute git dir once per worktree and cache it by path.
   - Use `--untracked-files=normal` for the overview. Keep `all` for detail
     `Status`. Document the count semantics change in the UI tooltip if
     visible.
4. **Parallel provider reads.**
   - Run `Repository`, `Branches` and the PR catalog concurrently (errgroup).
   - Fetch checks through a bounded pool (6) instead of the serial loop at
     `enrichment.go:407`.
   - Keep the "publish PR catalog before checks" behavior.
5. **Prioritize what the user sees.**
   - `SubscriberReady` queues the active or most recently selected project
     first. The others follow at lower priority (a second semaphore lane, or
     a short delay).
   - The frontend already knows the active project. Pass it on the WebSocket
     `ready` ack, or with an existing refresh call.
6. **Fix the `registeredTarget` re-list.** Cache the worktree inventory per
   refresh cycle instead of running `git worktree list` per call.

**Rails**
- Epoch and sequence ordering, per-worktree error isolation and stale-value
  retention stay unchanged.
- No Git network operations in snapshot reads (unchanged rule).
- Fork-safe PR association (head repo + branch) stays unchanged.
- Bounded concurrency everywhere. No unbounded goroutines per worktree.

**Exit criteria**
- On the bonsai repo, measured with `__sync-bench --cold`:
  - Time to local ready is ≤ 60% of baseline.
  - Time to PR catalog is ≤ 50% of baseline.
  - git spawns per cold load are down ≥ 50%.
- Race tests pass.
- New tests cover the parallel status pool, early provider start and
  priority ordering.

---

## Phase 2 — In-process GitHub client and cheap polling

**Model:** Opus 5.5 · **Effort:** high · **Branch:** `feat/web-p2-github-http`

**Objectives**
- Stop spawning `gh` for every GitHub read.
- Make "is anything new?" checks nearly free, so polling can be frequent
  without burning rate limit.

**Scope**
1. **Token source.**
   - `gh auth token --hostname <host>`, held in memory only.
   - Re-read on `401`, and at most once per N minutes otherwise.
   - If `gh` is missing or unauthenticated, report a structured
     `github_auth` state for the setup TUI and doctor, and keep local
     features working.
2. **Transport.**
   - Use `app.Client` with a real `http.Client`: keep-alive, HTTP/2, the
     existing timeouts and a bearer token from the token source.
   - Keep the `ghcli` subprocess transport as a fallback only when the token
     cannot be obtained.
   - Decide in plan mode whether mutations also move over. Default: yes, same
     client and same token scopes.
3. **ETag cache.**
   - Store `ETag` / `Last-Modified` and the decoded body per request URL.
   - Send `If-None-Match`. A `304` reuses the cached value.
   - Keep it in memory now; Phase 7 persists it.
4. **Polling cadence.** Uses the existing provider refresh scope.
   - Visible project: PR list page 1 (sorted by `updated`) every 30 s, and
     checks for visible worktrees with pending CI every 15 s.
   - Background projects: every 5 min.
   - If page 1 returns `304`, skip the rest of the catalog.
5. **Rate-limit awareness.**
   - Read `X-RateLimit-Remaining` and `Reset`.
   - Below 10% remaining, stretch the intervals and surface the state as
     `rate_limited` with its reset time.
6. **Optional cold-load GraphQL query** (D7).
   - One query fetches the repo, default branch, open PRs with head
     repo/branch/SHA, and `statusCheckRollup`.
   - It is used only when there is no cached snapshot.

**Rails**
- Tokens are never logged, persisted, placed in argv or sent to the browser.
  Add a test that greps captured logs for the token.
- GHES and `GH_HOST` keep working (host from the remote identity).
- PR association semantics and the `checksRollup` logic stay unchanged.
- Unit tests use `httptest` fakes only. No real GitHub in CI.

**Exit criteria**
- `gh` spawns per cold load are ≤ 2.
- A warm refresh with no changes makes only `304` responses (assert with a
  fake server).
- Time to PR catalog has an additional ≥ 40% improvement over Phase 1 numbers.
- Auth-missing and rate-limited states show up in the snapshot freshness.

---

## Phase 3 — `bonsai web` command and user-level supervisor

**Model:** Opus 5.5 · **Effort:** high · **Branch:** `feat/web-p3-web-command`

**Objectives**
- `bonsai web` works from any directory.
- It detaches once healthy, stays in the foreground with a clear diagnosis
  when not, and is controlled by friendly subcommands.

**Scope**
1. **Spike first (D9).**
   - Can the existing daemon run with a user-level home (for example
     `procstore.New(<user state dir>/bonsai/web)`) whose root is not a Git
     repo? List the blockers.
   - If it can't, add a minimal user-level supervisor that reuses
     `procstore` serve-group logic.
   - Record the outcome in this plan.
2. **API without a launch repo.**
   - `__serve-api` accepts no `--repo`. Projects come only from project roots.
   - Legacy unscoped routes (which use `registry.Default()`) return an
     explicit `404 no_default_project` instead of panicking.
3. **User config `web.json`** (D2), in `os.UserConfigDir()/bonsai/`:
   - Versioned, with a revision, atomic writes and a lock (reuse the
     project-roots store helpers).
   - Machine-managed state (hook IDs, last public URL) goes in a separate
     `web-state.json`. Secrets go in `web-webhook-secret` (`0600`).

   ```json
   {
     "version": 1,
     "revision": 1,
     "setup_version": 1,
     "api_port": 7001,
     "open_browser": true,
     "interfaces": { "local": true, "hosted": true },
     "updates": {
       "mode": "standard",
       "live": {
         "tunnel": "cloudflared-quick",
         "webhook_port": 7002,
         "public_url": "",
         "command": [],
         "repositories": []
       }
     }
   }
   ```
4. **CLI surface:**

   ```text
   bonsai web                       start or reuse, wait for ready, open browser, detach
   bonsai web setup                 setup TUI (Phase 5; until then prints current config)
   bonsai web status                processes, URLs, update mode, per-repo live health
   bonsai web open                  open the UI in the browser
   bonsai web logs [api|tunnel] [--follow]
   bonsai web attach                live log viewer (today's serve TUI)
   bonsai web restart [api|tunnel]
   bonsai web stop
   bonsai web doctor                Phase 5
   flags: --attach (stay in foreground), --no-open, --no-setup, --port N
   bonsai serve                     deprecated alias: prints a one-line notice, then behaves as `bonsai web --attach`
   ```

5. **Startup output.** On success:

   ```text
   ✓ bonsai web is running
     UI        http://127.0.0.1:7001/app
     updates   standard (every ~30 s)
     stop      bonsai web stop   ·   status: bonsai web status
   ```

   On failure, stop the half-started group and exit 1:

   ```text
   ✗ bonsai web could not start
     api   exited with code 1 — port 7001 is used by another program (pid 4242, node)
     fix   bonsai web --port 7011      or change it in: bonsai web setup → Advanced
     logs  bonsai web logs api
   ```

   - Port conflicts distinguish "another bonsai web" (reuse it) from a
     foreign process (name the PID if the OS allows).
6. **Open the browser.**
   - Linux uses `xdg-open`, macOS uses `open`, Windows uses
     `rundll32 url.dll,FileProtocolHandler`.
   - On WSL, prefer `wslview`, then `cmd.exe /c start`.
   - If opening fails, print the URL and don't fail the command.

**Rails**
- Do not break per-repo `bonsai serve` users. The alias must reach the same
  healthy end state.
- The daemon must not start dev services in production mode. Live-update
  services arrive in Phase 6 behind explicit config validation, not a
  blanket relaxation of `serve.go:222`.
- The command stays non-interactive when stdin is not a TTY.

**Exit criteria**
- `cd /tmp && bonsai web` starts, prints the summary, exits 0 and leaves the
  stack healthy. A second `bonsai web` reuses it.
- A forced port conflict prints the diagnosis and leaves no orphan processes.
- `stop`, `restart`, `status` and `logs` work on Linux, macOS and Windows.
- The hosted app still connects (the API still allows the hosted origin).

---

## Phase 4 — Embedded UI and dual browser origin

**Model:** Opus 5.5 · **Effort:** high · **Branch:** `feat/web-p4-embedded-ui`

**Objectives**
- The browser loads the UI from the local API on the same origin: no CORS, no
  local-network prompt and no version skew.
- The hosted app keeps working.

**Scope**
1. **Embed** (D4).
   - A `web` embed package with a build tag or generated file that embeds
     `web/dist`.
   - When it is absent, serve a placeholder page at `/app`:
     "UI not built: run `pnpm -C web build`".
2. **Static serving in the API.**
   - `/` redirects to `/app/`. `/app/*` uses an SPA fallback to `index.html`.
   - `/app/assets/*` gets `Cache-Control: public, max-age=31536000, immutable`.
   - Precompressed or on-the-fly gzip.
   - Static routes need no session. API routes are unchanged.
3. **Headers in Go.** Port `productionSecurityHeaders` from
   `web/vite.config.ts`:
   - CSP with `connect-src 'self'`, plus the relay origin only if D1 is
     re-enabled.
   - `frame-ancestors 'none'`, `nosniff`, `no-referrer`.
4. **Runtime config.** Inject meta tags into `index.html` at serve time:
   - `bonsai-local-api-origin` = same origin
   - `bonsai-relay-origin` (empty when frozen)
   - `bonsai-entry` = `local`

   On the frontend:
   - `LOCAL_API_HTTP` reads the meta first, then falls back to the build env.
   - Skip the `localNetworkPermission` probe when same-origin.
   - Hide relay UI when no relay origin is configured.
5. **Origin allow-list.** The API accepts:
   - its own origin, exactly `http://127.0.0.1:<port>` and
     `http://localhost:<port>`
   - the hosted HTTPS origin when `interfaces.hosted` is true

   Host checks are unchanged. Update `validateBrowserOrigin` and the tests.

**Rails**
- Still no wildcard CORS. The allow-list is a fixed set computed at startup.
- The hosted build (Dockerfile and Caddy) is unchanged and still works.
- `pnpm -C web build` output is deterministic. The embed must not pick up
  e2e builds.

**Exit criteria**
- Playwright runs against both entry points (local-served and hosted-style
  origin): connect, load project, open PR detail, live WS event.
- Opening `http://127.0.0.1:7001/app` shows no permission prompt in Chromium.
- Security tests:
  - A foreign origin is rejected.
  - `localhost` and `127.0.0.1` work.
  - The hosted origin is rejected when the interface is disabled.

---

## Phase 5 — Setup TUI and `bonsai web doctor`

**Model:** Opus 5.5 · **Effort:** high · **Branch:** `feat/web-p5-setup-tui`

**Objectives**
- First run and later edits are a guided, friendly, keyboard-first TUI.
- It explains every choice in plain language and is honest about effort and
  exposure.

**Scope**
1. **Checks engine.** A pure Go package, for example `internal/websetup/checks`.
   - Each check is `{ID, Title, State(ok|warn|fail|skip), Detail, Fix{Command, Inline bool}}`.
   - Checks: git version, `gh` installed, `gh` authenticated (host),
     project roots found, port free, tunnel tools detected (with versions),
     tunnel tool logged in where relevant.
   - Shared by the TUI and `bonsai web doctor`. Doctor exits 1 on any `fail`.
2. **Triggers.**
   - `bonsai web` runs setup first when `web.json` is missing or
     `setup_version` is older than current, and stdin and stdout are TTYs.
   - Non-TTY, or `--no-setup`: write safe defaults (local UI, standard
     updates) and print one line: "Customize with `bonsai web setup`".
3. **Wizard flow** (first run). Screens are specified in
   [Setup TUI spec](#setup-tui-spec).
   1. Welcome
   2. Projects
   3. Where to open Bonsai
   4. GitHub
   5. Updates (Live options are visible but marked "available in a later
      version" until Phase 6 lands)
   6. Review
   7. Apply and progress
   8. Done
4. **Edit mode** (`bonsai web setup` after the first run).
   - Opens a dashboard of sections with current values and health. Enter
     edits a section.
   - Changes go through the same Review screen, which shows a diff.
   - Apply restarts only the affected processes.
5. **Inline fixes.**
   - For checks with `Inline`, Enter suspends the TUI and runs the command
     (`tea.ExecProcess`). Examples: `gh auth login`, `cloudflared tunnel login`.
   - Then the check re-runs automatically.
   - `c` copies the fix command (`internal/core/clipboard`).
   - `r` re-runs all checks, for fixes done in another terminal.
6. **Quality bar.**
   - Golden-file tests (`teatest` or a plain `View()` snapshot) at 80×24 and
     120×40.
   - `NO_COLOR` respected. State is never conveyed by color alone (✓ ✗ !
     plus words).
   - Mouse optional. Esc always goes back. Ctrl+C asks before discarding
     changes.

**Rails**
- Reuse `internal/ui/theme` and existing components, so it looks like the
  bonsai TUI. Adding `charmbracelet/huh` needs justification in plan mode.
- The TUI never writes config until the user confirms on the Review screen.
- Copy rules:
  - Second person, present tense, no jargon in headlines.
  - Explain "webhook" once as "GitHub's change notifications".
  - Every ✗ has a one-line fix.

**Exit criteria**
- A brand-new user goes from `bonsai web` to the UI open in ≤ 6 key presses
  when accepting defaults (with `gh` already logged in).
- Edit mode changes the port, the hosted interface and project roots, and
  applies them with a targeted restart.
- `bonsai web doctor` prints the same checks and exits 0 or 1 correctly.
- Golden tests are committed for every screen.

---

## Phase 6 — Live updates: local webhook receiver and tunnel

**Model:** Opus 5.5 · **Effort:** high · **Branch:** `feat/web-p6-live-updates`

**Objectives**
- Opt-in, near-instant GitHub updates without any Bonsai-run cloud service.
- Exactly one thing is exposed to the internet: a signature-verified webhook
  receiver.

**Scope**
1. **Receiver** (D3).
   - A second listener on `127.0.0.1:<webhook_port>`.
   - Its mux has exactly `POST /github/webhook`. Everything else returns 404.
   - Reuse `internal/server/webhooks`: HMAC before parse, 2 MiB cap, header
     bounds, event and action allowlist, delivery dedupe, normalization.
   - Each normalized event queues a provider refresh (`force`) for every
     project whose remote matches the repo. A targeted checks invalidation
     follows for `check_run`, `check_suite`, `status` and `workflow_run` by
     SHA.
2. **Tunnel providers.** Each one is a supervised sidecar plus a
   public-URL extractor.

   | Preset | Command (target is always the webhook port) | Public URL from | Account |
   | --- | --- | --- | --- |
   | `cloudflared-quick` | `cloudflared tunnel --no-autoupdate --url http://127.0.0.1:{port}` | log line `https://*.trycloudflare.com` | none |
   | `cloudflared-named` | `cloudflared tunnel run {name}` (user-provided config and hostname) | config (`public_url`) | Cloudflare + domain |
   | `ngrok` | `ngrok http 127.0.0.1:{port} --url {domain}` (older versions: `--domain`) | config, or the ngrok local API `:4040/api/tunnels` | ngrok (free static domain) |
   | `tailscale` | `tailscale funnel {port}` | `tailscale status --json` → `Self.DNSName` | Tailscale with Funnel enabled |
   | `external-url` | none; the user runs their own proxy | config (`public_url`) | n/a |
   | `custom` | user template with `{port}` | user regex on stdout and stderr | n/a |

   Verify each command and flag against the current tool versions in plan
   mode, and record the versions tested here.
3. **Daemon validation.**
   - Production mode allows exactly two extra things when
     `updates.mode = live`: the webhook listener port, and one tunnel sidecar
     whose argv references only the webhook port.
   - Reject any argv that references the API port.
   - This replaces the blanket refusal at `serve.go:222` with an explicit
     allow-rule and tests.
4. **Hook manager.** Uses the Phase 2 client.
   - For each selected repo, check the `permissions.admin` field returned by
     `GET /repos/{o}/{r}`. Non-admin repos are marked `needs_admin` and stay
     on standard updates.
   - Find or create the hook. Identify it by the stored hook ID, falling back
     to a URL marker `?bonsai=<install-id>`.
     - `config.url = <public_url>/github/webhook?bonsai=<install-id>`
     - `content_type = json`, `secret = <web-webhook-secret>`
     - Events = the receiver allowlist (`push`, `pull_request`,
       `pull_request_review`, `check_run`, `check_suite`, `status`,
       `workflow_run`).
   - When the URL changes (quick tunnel restart), `PATCH config.url`.
   - Verify with the creation `ping`, or `POST …/hooks/{id}/pings`. Show
     per-repo state: `live`, `waiting_for_ping`, `failing (<last delivery
     error>)`, `needs_admin` or `scope_missing`.
   - Token scope: GitHub documents that the `repo` scope (gh's default)
     includes repository hooks. Confirm this in plan mode. If a `403` or
     `404` points to scope, show `gh auth refresh -h <host> -s admin:repo_hook`
     as the inline fix.
   - Turning live off deletes Bonsai's hooks after confirmation. Doctor lists
     orphaned Bonsai hooks.
5. **Polling interplay.**
   - While live is healthy (recent ping or delivery), polling slows to every
     10 min as a safety net.
   - If the tunnel or hook is unhealthy, fall back to the Phase 2 cadence and
     show it.
   - Always do one full provider reconcile at startup (missed events).
6. **Setup TUI.** Enable the Live options from Phase 5:
   - provider detection and login checks
   - repo picker (admin-only selectable)
   - review screen listing every external change
   - progress: receiver ready, then tunnel URL, then hooks created, then
     ping received
7. **Web UI.**
   - A small header badge: `Live` or `Standard (every 30 s)`, with a reason
     tooltip.
   - Settings shows per-repo live state read-only, with a hint to run
     `bonsai web setup`.

**Rails**
- Tunnel target and webhook port are validated in code. Tests assert that API
  routes return 404 on the webhook port.
- The secret is generated with `crypto/rand` (32 bytes), stored `0600`,
  rotatable from setup, never logged and never in argv.
- No raw payload persistence. Normalized metadata only (same as the relay).
- Creating, changing or deleting GitHub hooks requires a Review-screen
  confirmation.

**Exit criteria**
- An end-to-end manual test on a scratch repo with `cloudflared-quick`:
  - A PR open, push or check change shows in the UI in < 5 s.
  - Restarting `bonsai web` re-PATCHes the hook URL and ping succeeds.
  - Turning live off removes the hook.
- Automated tests cover the receiver routes, signature rejection, dedupe,
  tunnel argv validation, the URL extractor per preset (fixture logs) and
  the hook manager against a fake GitHub.
- `__dev-webhook send …` fixtures work against the product receiver.

---

## Phase 7 — Warm start and frontend load

**Model:** Sonnet 5.5 · **Effort:** medium · **Branch:** `feat/web-p7-warm-start`

**Objectives**
- Opening the app paints last-known state immediately.
- The initial JS cost is proportional to the first screen.

**Scope**
1. **Persist on the server.**
   - Store the last committed project snapshot and the ETag cache in the user
     state dir (atomic writes, size cap, versioned, discarded on schema
     mismatch).
   - On startup, load them with `freshness = stale` and revalidate
     immediately.
2. **Persist in the browser** (optional): cache the last snapshot per project
   in IndexedDB and render it while the WS connects. Wrap all storage access
   in try/catch.
3. **Code-split.**
   - Lazy-load the terminal (xterm), files, the GitHub detail markdown stack
     and the canvas layout (elkjs).
   - Run ELK layout in a Web Worker if profiling shows main-thread cost.
4. **Bundle budget.** Record chunk sizes in this plan. Add a CI check that
   fails when the main chunk grows more than 10% without updating the budget.

**Rails**
- Persisted data contains no secrets and no file contents (it is metadata
  only, like today's snapshot).
- A stale snapshot is always labeled stale in the UI until revalidated.

**Exit criteria**
- With a warm cache, the worktree list and PR badges render ≤ 300 ms after WS
  `ready`.
- The main chunk shrinks by ≥ 30% versus the Phase 4 build.

---

## Phase 8 — Docs, migration and release pipeline

**Model:** Sonnet 5.5 · **Effort:** medium · **Branch:** `feat/web-p8-release`
(The pure doc copy edits can be handed to Haiku 5.5.)

**Scope**
1. **Release builds embed the UI.**
   - GoReleaser `before.hooks` runs `pnpm -C web install --frozen-lockfile`
     and `pnpm -C web build`, with the embed build tag set.
   - CI release dry-run asserts that the binary serves `/app`.
2. **Docs.**
   - README quick start becomes `bonsai web`.
   - `docs/development.md` describes the dev stack versus product live
     updates.
   - `docs/git-backend.md` updates the trust boundary: product mode may
     supervise the webhook listener and one tunnel when explicitly
     configured, and the API is never exposed.
   - Update CONTRIBUTING.
3. **Migration.**
   - `bonsai serve` prints its deprecation notice for one minor release, then
     is removed. Track it in an issue.
   - Ignore or migrate legacy `serve.*` YAML keys.
4. **Cleanup.** Fold the lasting parts of this plan into the docs, then
   delete this file.

**Exit criteria**
- A fresh install from a release archive, followed by `bonsai web`, gets the
  full first-run flow on Linux, macOS and Windows.
- The docs match the shipped behavior.

---

## Phase F — Multi-workspace (design only, later)

**Model:** Opus 5.5 · Not scheduled. Rails to respect **now** so this stays
cheap later:

- **Frontend.** Connection state, sessions and snapshots are keyed by a
  `hostId` (today always `"local"`). No new module-level singletons that
  assume one host.
- **Protocol.** Epoch and sequence are already per backend. Keep event and
  snapshot IDs host-scoped when adding new ones.
- **Config.** `web.json` reserves `"workspaces": []`. Don't use that key for
  anything else.
- **Intended model: hub and spoke.**
  - The machine running the UI is the hub. Other hosts (WSL, Windows, LAN
    machines) are paired with a short code (`bonsai web pair`).
  - The hub proxies to them server-to-server, so the browser only ever talks
    to one origin.
  - Only the hub runs the webhook receiver and tunnel, and fans events out.
  - GitHub polling is deduped by repo full name across hosts.
- **WSL + Windows.** Detect `wsl.exe` and offer "Add your WSL workspace". WSL2
  localhost forwarding covers the network path on one machine.
- **Never bind non-loopback** without pairing auth and TLS. That is a Phase F
  deliverable, not a shortcut.

---

## Setup TUI spec

General layout: a title bar with the step counter, a content area, and a
footer with key hints. Option screens use two panes: the list on the left and
the explanation on the right. Below 100 columns, the explanation stacks under
the list.

**1. Welcome**

```text
 bonsai web · setup                                              1 / 7
 ─────────────────────────────────────────────────────────────────────
  See every repo, worktree, pull request and CI run in your browser.
  Everything runs on this computer.

  Takes about a minute. Change anything later with:  bonsai web setup

                         enter  start      d  use defaults and go
```

**2. Projects.** Pick the folders that contain your repos.

```text
 Where are your repositories?                                    2 / 7
 ─────────────────────────────────────────────────────────────────────
  [x] ~/code              14 repos found
  [ ] ~/work               3 repos found
  [x] ~/bonsai             this repo
  [ ] Add another folder…

  Bonsai looks up to 4 folders deep. Nothing is moved or changed.
 space toggle · a add folder · enter next · esc back
```

**3. Where to open Bonsai**

```text
 Where do you want to open Bonsai?                               3 / 7
 ┌───────────────────────────────┐ ┌─────────────────────────────────────┐
 │ ● This computer (recommended) │ │ Opens http://127.0.0.1:7001         │
 │ ○ Hosted app + this computer  │ │                                     │
 │                               │ │ ✓ No browser permission prompt      │
 │ [x] Open the browser when     │ │ ✓ Always matches your bonsai version│
 │     bonsai web starts         │ │ ✓ Works offline                     │
 └───────────────────────────────┘ └─────────────────────────────────────┘
 Hosted app: app.bonsai.dev also works. Your browser will ask to allow
 access to this computer, and it may ask you to update bonsai after a
 web release.
```

**4. GitHub**

```text
 Connect GitHub                                                  4 / 7
 ─────────────────────────────────────────────────────────────────────
  ✓ gh installed           2.81.0
  ✗ gh not logged in       enter  run "gh auth login" now   c copy
  
  Bonsai uses your existing gh login. It never stores your token.
  Without it you still get worktrees, branches, commits and processes.
 enter fix · r re-check · s skip GitHub · esc back
```

**5. Updates.** This is the key comparison screen.

```text
 How should GitHub changes reach you?                            5 / 7
 ┌──────────────────────────────┐ ┌──────────────────────────────────────┐
 │ ● Standard (recommended)     │ │ Standard                             │
 │ ○ Live · Cloudflare quick    │ │ Bonsai checks GitHub every ~30 s     │
 │ ○ Live · Cloudflare + domain │ │ while the app is open.               │
 │ ○ Live · ngrok               │ │                                      │
 │ ○ Live · Tailscale Funnel    │ │ You need   nothing                   │
 │ ○ Live · my own URL          │ │ Setup      ○○○ none                  │
 │ ○ Live · custom command      │ │ Delay      up to ~30 s               │
 └──────────────────────────────┘ │ Exposes    nothing                   │
                                  └──────────────────────────────────────┘
 ↑↓ choose · ? compare all · enter next · esc back
```

The same panel for `Live · Cloudflare quick`:

```text
 Live · Cloudflare quick tunnel
 GitHub notifies Bonsai the moment a PR, push or check changes.

 You need   cloudflared   ✓ installed (2026.9.0)
            admin rights on the repos you pick
 Setup      ●○○ one step, no account
 Delay      ~1 s
 Exposes    only Bonsai's change receiver; unsigned requests are rejected
 Note       The public address changes each time bonsai web starts.
            Bonsai re-points your GitHub notifications automatically.
```

`?` opens a comparison matrix:

```text
                    Setup   Account          Stable URL   Delay
 Standard           none    —                —            ~30 s
 Cloudflare quick   ●○○     none             no (auto)    ~1 s
 Cloudflare+domain  ●●○     Cloudflare+domain yes         ~1 s
 ngrok              ●●○     ngrok (free)     yes          ~1 s
 Tailscale Funnel   ●●○     Tailscale        yes          ~1 s
 My own URL         ●●●     your proxy       yes          ~1 s
```

The Live options continue to **5b. Pick repositories** (admin repos are
selectable; the others are greyed with "needs admin, stays on Standard").

**6. Review.** This is the only place changes are committed.

```text
 Review                                                          6 / 7
 ─────────────────────────────────────────────────────────────────────
  Projects     ~/code, ~/bonsai (15 repos)
  Open in      this computer · http://127.0.0.1:7001 · auto-open on
  GitHub       via gh (tiago-0liveira)
  Updates      Live · Cloudflare quick tunnel

  This will change things outside this computer:
   • Start cloudflared, exposing only the change receiver (port 7002)
   • Create a GitHub webhook on 2 repos:
       Tiago-0liveira/bonsai, Tiago-0liveira/dotfiles
     Repo admins can see it as "bonsai (this-laptop)".

                                  enter  apply      esc  go back
```

**7. Apply and progress**

```text
  ✓ Saved settings
  ✓ Started Bonsai            http://127.0.0.1:7001
  ✓ Tunnel ready               https://quiet-river-1234.trycloudflare.com
  ✓ Webhook on Tiago-0liveira/bonsai      ping received
  … Webhook on Tiago-0liveira/dotfiles    waiting for ping (3 s)
```

**8. Done.** Shows the URL, "o open browser", and a 4-line cheat sheet
(`status`, `logs`, `setup`, `stop`).

**Edit mode dashboard** (`bonsai web setup` after the first run):

```text
 bonsai web · settings                               ● running · 7001
 ─────────────────────────────────────────────────────────────────────
  Projects        ~/code, ~/bonsai · 15 repos                  ✓
  Open in         this computer + hosted app                    ✓
  GitHub          gh · tiago-0liveira                           ✓
  Updates         Live · Cloudflare quick · 2 repos             ! 1 failing
  Advanced        ports 7001/7002 · rotate webhook secret
 enter edit · d doctor · q quit
```

---

## How to use this plan

### Running each phase with Claude Code

1. **Start from fresh `main`.** Each phase gets a new session and worktree,
   for example a new Code session in the desktop app. Confirm that
   `git log -1` includes every phase merged so far.
2. **Pick the phase's model** in the model selector (or `/model`). Use the
   effort level from the phase header.
3. **Paste the kickoff prompt** (below), with `N` set to the phase number.
4. **Review the plan Claude proposes** in plan mode before approving. Check
   it against the phase's Scope, Rails and Exit criteria.
5. **When it finishes,** run `/code-review high` on the branch, then let
   Claude open the PR. The PR must flip the Status row and include
   measurements where the phase asks for them.
6. **Merge, then start the next phase.** Tracks A and B can run in parallel
   in separate sessions. Phase 6 waits for both.

**Kickoff prompt:**

```text
Read docs/bonsai-web-plan.md in full. Implement Phase 0 only.
Start in plan mode: restate the phase objectives, list the files you will
touch, flag anything in the plan that no longer matches the code, and wait
for my approval. Follow the Global rails and the phase Rails strictly.
When done: run the full Validation list, update the Status table and any
"fill in" tables in the plan, and open a PR whose description maps every
Exit criterion to evidence.
```

**If a phase is too big** (most likely 3, 5 and 6): ask Claude in plan mode
to split it into stacked PRs (`p5a`, `p5b`, …). Each part must keep the
build green and the hosted app working.

### What the end-user flow looks like when done

**First run:**

```sh
bonsai web
```

1. The setup TUI opens: Welcome, Projects, Where to open, GitHub, Updates,
   Review, Apply.
2. Accepting defaults (local UI, standard updates) takes about 6 key
   presses.
3. Bonsai starts, opens `http://127.0.0.1:7001/app` and the terminal returns
   to the prompt.

**Every day after:**

```sh
bonsai web          # start or reuse, open the browser, detach
bonsai web status   # what's running, update mode, live health per repo
bonsai web stop
```

**Change anything later:**

```sh
bonsai web setup    # dashboard: edit projects, ports, hosted app, updates
bonsai web doctor   # same checks, non-interactive, exit 1 on failures
```

**Enable live updates later:** `bonsai web setup`, then Updates, then a Live
option, then pick repos, then Review, then Apply. Bonsai starts the tunnel,
creates the webhooks and waits for GitHub's ping.

**Hosted app:** turn on "Hosted app + this computer" in setup, then open
`https://app.bonsai.dev/app` while `bonsai web` is running.
