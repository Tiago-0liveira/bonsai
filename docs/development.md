# Local full-stack development

Bonsai has two explicit serve modes. Keep them separate in code and tests.

## Product mode

```sh
bonsai serve
```

This starts or reuses only the local Bonsai API, bound to
`127.0.0.1:7001` by default. Its authorized browser origin is exactly
`https://app.bonsai.dev`. The user-facing web client is
`https://app.bonsai.dev/app`.

Product mode does not start Vite, a webhook listener, a tunnel, or arbitrary
sidecars. It never interprets development configuration as permission to bind
the API publicly.

## Internal development stack

Contributors and integration tests may use:

```sh
bonsai __serve-dev-stack
```

The command is intentionally hidden from normal help. By default it supervises:

```text
api       http://127.0.0.1:7001
webhook   http://127.0.0.1:7002
web       http://127.0.0.1:7003
```

The API runs in explicit development browser-security mode, authorizing only
the exact local Vite origin. Development mode still requires loopback binding,
exact Host and Origin checks, capability sessions, strict request parsing,
mutation idempotency, and first-message WebSocket authentication.

Lifecycle commands are also internal:

```sh
bonsai __serve-dev-stack status
bonsai __serve-dev-stack logs
bonsai __serve-dev-stack logs --process webhook
bonsai __serve-dev-stack restart [api|webhook|web|tunnel]
bonsai __serve-dev-stack stop
```

Detach after readiness with `-d` or `--auto-detach`.

### Development ports

Hidden flags override environment variables, which override the development
defaults:

| Service | Flag | Environment | Default |
| --- | --- | --- | ---: |
| API | `--api-port` | `BONSAI_DEV_API_PORT` | 7001 |
| webhook | `--webhook-port` | `BONSAI_DEV_WEBHOOK_PORT` | 7002 |
| Vite | `--web-port` | `BONSAI_DEV_WEB_PORT` | 7003 |

These values are development concerns and do not belong in normal user
`.bonsai.yaml`. Legacy `serve.webhook_port`, `serve.web_port`, and
`serve.server_config` keys are ignored by normal `bonsai serve` and produce a
migration warning.

## Fixture webhooks

Routine development does not require GitHub. The CLI carries built-in
fixtures (see `internal/cli/dev_webhook.go`) and signs them on send. With the
hidden stack running:

```sh
bonsai __dev-webhook send push
bonsai __dev-webhook send pull_request_opened
bonsai __dev-webhook send pull_request_synchronize
bonsai __dev-webhook send check_run_completed
```

The helper signs the fixture with the stack's private local secret and POSTs it
to `127.0.0.1:7002/github/webhook`. The webhook service verifies HMAC, enforces
payload/header and event/action limits, deduplicates deliveries, normalizes the
payload, and emits SSE metadata. It never executes a Bonsai command. The local
Vite app consumes that metadata as an invalidation signal and refreshes state
through the capability-authenticated local API.

## Optional real GitHub tunnel

A tunnel is never started automatically. To test a real GitHub delivery, set a
development-only webhook secret that also exists in the GitHub webhook
configuration, then explicitly choose one supported tunnel:

```sh
BONSAI_DEV_WEBHOOK_SECRET='development-only-secret' \
  bonsai __serve-dev-stack --tunnel cloudflared

BONSAI_DEV_WEBHOOK_SECRET='development-only-secret' \
  bonsai __serve-dev-stack --tunnel ngrok
```

Only the webhook target `http://127.0.0.1:7002` is passed to the tunnel
process. The Bonsai API and local Vite server remain loopback-only. Never tunnel
the API, WebSocket control endpoint, daemon socket, or project services.

## Frontend-only iteration

The hidden stack is preferred when changing networking or integration behavior,
because it exercises the actual trust boundary. For purely visual work you may
still run Vite directly from `web/`, but backend-dependent features will need
the corresponding development environment variables.

The supervisor sets:

```text
VITE_BONSAI_LOCAL_API_ORIGIN=http://127.0.0.1:7001
VITE_BONSAI_RELAY_ORIGIN=http://127.0.0.1:7002
```

Production builds default to `https://api.bonsai.dev` for relay events and
`http://127.0.0.1:7001` for the local Bonsai API.

## Validation

Before merging changes to this boundary, run the Go formatting/vet/build/test
suite (including race tests for touched Go packages) and the frontend
typecheck/tests/build/Playwright suite. Security regressions should cover
loopback binding, exact Host/Origin, no wildcard CORS, capability and WebSocket
authentication, webhook HMAC/dedupe/allowlists, repository-scoped relay events,
and the rule that normal `bonsai serve` cannot supervise development services.

## Project roots and multi-repository routing

Local API protocol **3** requires the browser and backend to be upgraded together.
The per-repository launch daemon still supervises the API; project discovery does
not change either serve lifecycle. A launch repository is not implicitly added to
the browser catalog, and legacy unscoped routes only resolve it if configured.

User configuration lives at `os.UserConfigDir()/bonsai/project-roots.json` with
`version: 1`, a monotonic `revision`, and `roots: [{id, path}]`. The file also
contains durable idempotency request/result records. It is separate from TUI
`state.json` and project YAML. Cross-process locks serialize reload/update
transactions, and writes use the Git store's atomic JSON replacement primitive.
Do not replace unreadable or unsupported settings with an empty configuration.

Authenticated endpoints:

- `GET /api/settings/project-roots` returns configuration, directory suggestions
  and non-persisted availability/scan diagnostics.
- `POST /api/settings/project-roots` accepts `{path, revision}`.
- `DELETE /api/settings/project-roots/{id}` accepts `{revision}`.

Both edits require `Idempotency-Key`. A stale revision or reused key with a
changed request returns 409. Successful retries return the original configuration
result. No endpoint accepts a browser-supplied worktree destination. The usual
Host, Origin, session capability and strict JSON checks remain in force.

Discovery uses four workers, a depth of four below each selected root, a bounded
entry budget of 10,000 per root, a three-second per-root scan deadline and a
20-second reconciliation deadline. It runs
at startup, after edits, and every 30 seconds, reloading revisions written by
other API instances. Failed scans retain known projects as unavailable; a
successful scan can remove absent entries. Root revision checks reject stale scan
results. Catalog changes emit a `catalog` event and the browser reloads settings
and project descriptors before refreshing projects independently.

Public project/root IDs hash canonical local paths; daemon repository IDs and
persisted metadata retain `local`. Worktree IDs retain the existing hash of
`local` and the Git-listed path. Each discovered project owns a daemon client,
provider client and one live metadata store. Metadata and provider audit writes
reload under cross-process locks. `/api/projects` enumerates local descriptors
without contacting any daemon or provider. Process IDs are daemon-local: use
`/api/projects/{projectId}/processes`, `/{id}/logs`, `/{id}/restart`, or DELETE
`/{id}`. The unscoped process endpoints address only the configured launch project.

Browser placement is intentionally distinct from CLI/TUI path templates. Each
creation resolves current settings under the repository operation lock. The
configuration lock serializes root removal with creation; an operation that
already started may finish. The deepest root containing the main repository
wins, falling back to the discovered linked worktree. Symlink containment is
checked before creating destination directories. Existing trees are never moved.

Frontend fixtures include empty-root onboarding and multiple folders. Settings
state is excluded from browser persistence; existing `local` project selections
migrate to the configured launch descriptor while stable worktree IDs and layout
preferences are kept. CI's Linux/macOS/Windows matrix exercises the native path
and locking code; the race suite covers concurrent stores and API reconciliation.


## Worktree synchronization validation

Protocol 3 deliberately separates local reconciliation from provider enrichment.
When testing the browser/backend boundary, connect the WebSocket first and wait
for authenticated `ready` before loading settings, the project catalog, and
project projections. A reconnect receives a backend epoch; project snapshots and
events are ordered by their per-project sequence inside that epoch. Tests should
exercise events before/during/after bootstrap, out-of-order responses, root
removal during an in-flight read, and slow-client disconnect followed by full
reconciliation.

The server polls local Git every 5 seconds and processes every 2 seconds while
there are browser subscribers. Project discovery remains every 30 seconds.
Provider metadata has a 60-second normal TTL and pending checks a 15-second TTL.
These intervals are initial bounded defaults; do not add component-owned polling
or a second filesystem watcher to reduce them.

Provider loss must leave local worktrees and process controls usable. Failed
reads preserve the last successful value as stale and expose a structured error;
a successful empty result is the only thing that clears an authoritative list.
Use the Playwright delayed-provider fixture to verify that local worktrees render
before PR/CI enrichment.

Bootstrap and polling must not execute `git fetch`. For network-sensitive
changes, capture Git command traffic or use a local bare remote and assert its
tracking refs change only after an explicit fetch/pull operation. Run race tests
for `internal/server/localapi`, `internal/daemon/client`, and touched local Git
packages in addition to the normal cross-platform CI matrix.

## Agent provider contract

Providers live under `internal/providers/<id>` and register in
`internal/agentruntime`. The core (`internal/core/agents`) is provider-neutral:
the terminal manager, local API and CLI never test a provider ID.

- `Provider` is required: setup, `PrepareSession`, `FinalizeSession` and usage.
  `Capabilities()` is enforced by the manager: `Interactive=false` cannot start a
  web terminal, `ConcurrentSameAccount=false` rejects a second active session on
  an account (`409 agent_busy`), and `ConcurrentCrossAccount=false` rejects a
  session on another account of the same provider.
- Per-launch choices arrive as `LaunchOptions` (model, prompt, display name,
  full access, permission mode, effort) in `PrepareSessionRequest.Launch`. The
  provider maps them to its own argv; the manager adds nothing. Prompts go last.
- `PreparedSession.EnvUnsetPrefixes` scrubs inherited variables by prefix
  (case-insensitive on Windows); `EnvSet` still wins. `ProviderSessionID` is
  recorded in the session summary as `provider_session_id`.
- `SetupRequest.Options` carries `bonsai agent account add` flags (`--auth`,
  `--token-stdin`, `--no-seed`, `--seed-from`). Providers reject options they do
  not support rather than ignoring them.

Optional interfaces, found by type assertion:

| Interface | Used for |
| --- | --- |
| `Describer` | Provider label and host availability in `/api/agents/providers` |
| `LaunchValidator` | Synchronous launch-option check; errors return `400 invalid_launch_options` and leave no session |
| `AccountDescriber` | Display-safe `auth_mode`, `identity`, `warnings` and options in `/api/agents/accounts` and `account list`; never tokens, paths or raw settings |
| `AccountRemover` | Best-effort provider cleanup before an account is deleted; failure is a warning |
| `UsagePolicy` | Provider-specific usage cache TTL |

`/api/agents/providers` always lists Antigravity, Claude and Codex (unregistered
ones as "Not available yet") plus any other registered provider.
`/api/agents/accounts` lists accounts of registered providers only.

## Claude provider

`internal/providers/claude` runs Claude Code. Each profile owns
`<account dir>/config` as its `CLAUDE_CONFIG_DIR`, shared by all its sessions;
there is no credential vault, materialize or reconcile step. Launch environment:
every inherited `CLAUDE*` and `ANTHROPIC_*` variable is removed
(`EnvUnsetPrefixes`), then `CLAUDE_CONFIG_DIR` and `BONSAI_AGENT_*` are set
(`HOME` is untouched). Token profiles also get `CLAUDE_CODE_OAUTH_TOKEN` and
`CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1`. Argv is built in `launch.go` only:
`[explicit...] --model --permission-mode --effort --name --session-id <uuid> [-- prompt]`;
the generated UUID is the session's `provider_session_id`.

Tests never need a Claude login: `internal/providers/claude/testdata/fakeclaude.sh`
is a POSIX stand-in that tests put first on `PATH`; it reads the Phase 0
`auth-status-*.json` fixtures from `FAKE_CLAUDE_FIXTURES`. `UsageService.All`
skips providers whose `Capabilities().Usage` is false.

Unverified assumptions (Phase 0 was inconclusive): that
`CLAUDE_CODE_SUBPROCESS_ENV_SCRUB` hides the token from Bash subprocesses, and that
`claude auth status` never rotates a refresh token. Re-check both when upgrading
`MinVersion`.

Claude usage (`usage.go`) reads the undocumented `GET /api/oauth/usage` with the
profile's stored access token. Token profiles are reported as `ErrUsageUnsupported`
without any request (setup tokens always get 403). Redirects are not followed, the
endpoint must be https or loopback, and HTTP failures are remembered for a minute. It never refreshes a login, never puts the
token or the response body in an error, and accepts both the flat-bucket and the
`limits[]` response shapes (`testdata/usage-response*.json`). The provider
implements `UsagePolicy` (5 minutes), which `UsageService` prefers over its own
TTL. Tests point `claude.UsageEndpoint` or the provider's `usageEndpoint` at an
`httptest` server; nothing reaches the network.

## Antigravity web terminals

The web client can start an existing Bonsai Antigravity profile in a configured
project worktree. Set up a profile with
`bonsai agent account add antigravity <name>` and ensure `agy` is on the API's
PATH. Claude and Codex remain unavailable. Profile selection uses opaque account
IDs; discovery responses contain only ID, provider and display name.

The local API owns these sessions, independently of request contexts and browser
connections. Closing a terminal or refreshing detaches; reconnecting resumes the
same process. Explicit Stop and graceful API shutdown terminate the process group,
reap it, reconcile credentials using a fresh cleanup context, and delete its
isolated session home. API restart never automatically relaunches an agent.
Durable mutation records recover interrupted sessions as failed; their former
terminal output is not persisted. Abrupt API/OS termination can leave temporary
homes requiring inspection; graceful cleanup is the supported path.

Native PTYs are implemented for Linux, macOS, FreeBSD, OpenBSD, NetBSD and
DragonFly using `github.com/creack/pty` v1.1.24. Linux has runtime coverage here;
macOS and Windows builds are checked, but macOS runtime behavior needs native CI.
Windows and other platforms report interactive terminals as unsupported.
The adapter launches the provider's executable and argument vector directly,
with its isolated environment and working directory; there is no generic shell
or browser-supplied executable/path option.

Limits: 16 active sessions, 32 retained completed sessions, 2 MiB of recent output
per session, 64 KiB per input frame, and 1–500 terminal rows/columns. Only one
attachment writes; other attachments observe. After the writer disconnects, a
new attachment can claim writing. Slow readers are disconnected. Bounded replay
cannot fully reconstruct every full-screen terminal after truncation; the UI
explicitly resets and reports the gap.

Protocol 3 adds `/api/agents/providers`, `/api/agents/accounts` and project-scoped
`GET/POST /api/projects/{projectId}/agents`,
`DELETE /api/projects/{projectId}/agents/{sessionId}`, and
`GET /api/projects/{projectId}/agents/{sessionId}/terminal` (WebSocket).
Start and Stop require `Idempotency-Key`. Start accepts only
`{worktree_id, account_id, name?, cols, rows}`. Start retries must retain the same
key and payload; a changed payload returns 409. Interrupted uncertain requests
require a new explicit user action. HTTP retains capability, exact Host/Origin,
CORS, size and strict JSON checks. Older backends without discovery leave launch
disabled with an upgrade explanation.

The dedicated terminal stream uses version 1 JSON envelopes:

- Client first sends `{type:"authenticate", token}`, then
  `{type:"attach", offset, generation}` within the authentication deadline.
  Tokens never appear in URLs.
- Server sends `{type:"ready", version:1, generation, writer}` before replay.
- Output is `{type:"output", offset, data}` where `data` is base64 bytes and
  `offset` is the absolute starting byte offset. Replay and subscription are
  registered atomically. The browser writes decoded bytes directly to xterm.
- A cursor outside the retained range receives `{type:"gap", offset}` followed
  by the retained tail. Generation changes reset the browser display/cursor.
- Input is `{type:"input", data}` (base64); resize is
  `{type:"resize", cols, rows}`. Input is never queued across reconnects.
- Lifecycle updates are `{type:"status", offset, status:<safe agent summary>}`.
  Final status follows output drain. Protocol errors close the connection;
  clients also understand `{type:"error", message}` for future diagnostics.

Terminal bytes, sockets and replay cursors stay outside Zustand and browser
persistence. Safe session summaries travel through authoritative project
snapshots with their existing epoch/sequence ordering and `agents` freshness.
Worktree removal and root/project deselection conflict while an affected agent
is live. Ownership is resolved from the registry and current Git inventory.

`pnpm test:e2e` starts a dedicated Go fixture API on port 7001 on Linux/macOS.
Keep that port free; it deliberately refuses to reuse an existing personal API.
The fixture uses temporary fake profiles and a real shell PTY, never personal
credentials. `agent-terminal-fullstack.spec.ts` exercises actual HTTP, WebSocket,
PTY, browser input, resize, reload/replay and Stop. Core tests also cover Ctrl+C,
writer ownership, slow readers, replay gaps, concurrency, limits and cleanup.

Validation on 2026-10-05: the manual browser smoke launched the installed `agy`
in a disposable Git worktree with an existing profile, displayed its ANSI UI,
forwarded keyboard input and resize, reattached to the same session after a
browser reload, then stopped it with no cleanup error. Its temporary session
home was confirmed removed. The CLI reported that the profile was not signed
in, so an authenticated model conversation remains unverified. No login or
profile settings were changed for this check.
