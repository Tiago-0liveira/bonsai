# Local full-stack development

The product and contributor harness use separate supervisors and origins.

## Product mode

```sh
bonsai web
```

One user-level supervisor starts the local API on `127.0.0.1:7001`, serves the
embedded production UI at `/app` and works outside a Git repository. Production
requires an empty additional browser origin and accepts only the API's exact
loopback origins. Browser sessions connect automatically, without permission
queries. Release archives contain the UI; plain Go builds use a placeholder
unless built with `embedui` after `pnpm --dir web build`.

Standard updates poll GitHub. Live updates explicitly add a separate loopback
webhook receiver and, if configured, one tunnel. Only the receiver is exposed.
See [web.md](web.md) for setup, settings, doctor, logs and recovery, and
[git-backend.md](git-backend.md) for the security boundary.

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

Flags override the explicit development environment; no `serve.*` YAML key
is read. The harness always uses the existing startup/shutdown defaults
(30 seconds / 5 seconds). The removed public `bonsai serve` starts nothing.

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

## Building a binary with the web UI

A plain `go build` (or `go install`) keeps the TUI/CLI and serves an explanatory
placeholder at `/app`. To embed the full web app from source, use Node
**22.12.0**, pnpm **12.9.1** and the repository Go version:

```sh
pnpm --dir web install --frozen-lockfile
pnpm --dir web build
pnpm --dir web bundle:check
go test -tags embedui ./web/
go build -tags embedui -o bonsai .
```

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

Production builds always use the page's loopback origin. The SSE client is
compiled off in production; development/test builds require an explicit
loopback HTTP relay origin. There are no cloud credentials or fallback URLs.

## Validation

Before merging changes to this boundary, run the Go formatting/vet/build/test
suite (including race tests for touched Go packages) and the frontend
typecheck/tests/build/Playwright suite. Security regressions should cover
loopback binding, exact Host/Origin, no wildcard CORS, capability and WebSocket
authentication, webhook HMAC/dedupe/allowlists, repository-scoped relay events,
and the rule that product startup never interprets legacy YAML as development
configuration.

## Project roots and multi-repository routing

Local API protocol **3** requires the browser and backend to be upgraded together.
The user-level supervisor owns the API; each discovered repository retains its
own daemon for Git and process authority. A launch repository is not implicitly added to
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

Local Git uses daemon invalidation/watchers with a 30-second recovery poll;
process reconciliation and project discovery also use 30-second recovery scans.
GitHub polling is 30 seconds for visible projects, 15 seconds for running CI
and 5 minutes in the background. Healthy Live updates retain a 10-minute safety
poll. Repository metadata has a 20-second TTL, checks 60 seconds and pending
checks 10 seconds. Do not add component-owned polling or redundant watchers.

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
  full access, permission mode, effort, sandbox, approval) in
  `PrepareSessionRequest.Launch`. The provider maps them to its own argv; the
  manager adds nothing. Prompts go last. A provider rejects every option it does
  not declare, in `ValidateLaunch`.
- `PreparedSession.EnvUnsetPrefixes` scrubs inherited variables by prefix
  (case-insensitive on Windows); `EnvSet` still wins. `ProviderSessionID` is
  recorded in the session summary as `provider_session_id`.
- `SetupRequest.Options` carries `bonsai agent account add` flags (`--auth`,
  `--token-stdin`, `--no-seed`, `--seed-from`). Providers reject options they do
  not support rather than ignoring them.
- `SetupRequest.IO` (`SetupIO`) is the only way setup talks to the user: its
  `Launcher` runs interactive login commands, `Out` gets progress lines (the seed
  report) and `Prompt` (`SecretPrompter`) asks for a pasted key or token.
  Providers never read the process TTY or write to stdio during setup and take
  no launcher or writer at construction. `AccountService.SetupWithIO` fills
  missing fields from its own `Launcher`, its `Out` and `TerminalPrompter`, which
  returns `ErrNoTerminal` without a TTY (providers turn that into their
  "pipe it with --token-stdin" message). Notices such as a login another profile
  already uses come back as `SetupResult.Warnings`; the CLI prints each as a
  `warning: ` line where setup used to print it, so `account add` output is
  unchanged (`internal/cli/testdata/account-add.golden`).
- `AccountService.Reauthenticate` signs an existing account in again under a
  per-account lock (`<account dir>/login.lock`) with a throwaway session, stores
  the settings the provider returns (a changed auth mode) and drops the
  account's cached usage. It runs while sessions of the account are active.

Optional interfaces, found by type assertion:

| Interface | Used for |
| --- | --- |
| `Describer` | Provider label and host availability in `/api/agents/providers`; `Availability.Hint` becomes `install_hint` while the provider is unavailable |
| `LaunchValidator` | Synchronous launch-option check; errors return `400 invalid_launch_options` and leave no session |
| `LaunchSchema` | `launch_fields` in `/api/agents/providers`: the launch options the start dialog renders, see below |
| `ModelLister` | Model suggestions in `/api/agents/providers/{id}/models`; per-model `efforts`, `default_effort` and the `default` model |
| `AccountDescriber` | Display-safe `auth_mode`, `identity`, `warnings` and options in `/api/agents/accounts` and `account list`; never tokens, paths or raw settings |
| `AccountRemover` | Best-effort provider cleanup before an account is deleted; failure is a warning |
| `ProfileEditor` | Profile launch defaults: `profile_fields` in `/api/agents/providers` (the `model` field, then the launch fields with an empty "Provider default" choice), `defaults` in account responses, `PATCH /api/agents/accounts/{id}` and `bonsai agent account set`/`show`. `UpdateProfile` merges into the stored settings, never touches sign-in keys and validates with the provider's own settings parser |
| `UsagePolicy` | Provider-specific usage cache TTL |
| `UsageLayout` | `UsageHeadline`: the primary/secondary limits (5h, weekly), extra limits, score (lower remaining of the pair), `low` (< 0.20) and next reset; shared by `bonsai agent usage` and `/api/agents/usage` |
| `UsageNoter` | `usage_note` in `/api/agents/providers`: a short caveat on where usage is read from |
| `SetupSchema` | `setup` in `/api/agents/providers`: `auth_modes` (value, label, help, `recommended` per host, `secret` for a pasted key or token, `optional` when the provider can get it another way), `seed.source` for display (home-relative) and `notes` |
| `AccountAuthenticator` | Sign in again (`bonsai agent account login <account> [--auth <mode>] [--token-stdin]`): runs the provider's own login for an existing account; without `--auth` the account keeps its mode |
| `ActivityReporter` | Live activity (Working, Waiting, Idle) from the agent's own hooks, see [Activity hooks](#activity-hooks): `ActivityProfile` (also `activity: {native, inferred}` in `/api/agents/providers`: the states the provider reports itself and the ones Bonsai can only guess), `TranslateHook` (raw hook payload to normalized signals) and `HookOutput` |
| `TerminalClassifier` | Maps a terminal notification the agent wrote to its PTY (OSC 9, 777, 99, BEL) to a signal, for waiting states a provider cannot report through hooks |
| `TerminalScreenClassifier` | Recognizes fixed input-dialog framing on an in-memory terminal grid; reports inferred kinds and reasons without copying dialog contents |

`/api/agents/providers` always lists `agents.KnownProviders` (Antigravity, Claude
and Codex; unregistered ones as "Not available yet") plus any other registered
provider.

Launch fields keep the start dialog provider-neutral. Each `LaunchField` is a
`select` or a `toggle` whose `id` is the start request's JSON field
(`permission_mode`, `effort`, `full_access`, `sandbox`, `approval`). A toggle has
two options, `"false"` and `"true"`, and starts from the profile's option of the
same ID; a select starts at `default` or its first option, and an empty value
sends nothing. An option with `set` is a preset: choosing it sends those fields
instead of the field's own ID. `tone: "danger"` tints the control and shows the
option's `help`. A `per_model` field takes its values from the chosen model's
`efforts` and falls back to its own options for typed or unlisted models. Every
value a provider declares must pass its `ValidateLaunch` (each provider has a
test for this).
`/api/agents/accounts` lists accounts of registered providers only.

Routes, setup jobs and the web page are described in [Agents tab](#agents-tab).

### Activity hooks

Beyond the lifecycle (`starting`, `running`, `exited`, ...), a running session
has an activity: `unknown`, `working`, `waiting` (needs the user: permission,
question, plan, elicitation) or `idle` (turn finished). It is optional and best
effort: a provider that does not implement `ActivityReporter`, hooks the user
disabled, or a holder that cannot name a hook command leave it `unknown`, which
every client shows as plain "Running". Hooks only observe: they print nothing
that changes the agent's behaviour, never block it, and always exit 0.

- The holder (`internal/core/agenthost`) finds `ActivityReporter` on the provider
  it got from the registry, creates `<runtime dir>/activity.jsonl` (0600) and
  starts tailing it before `RunSession`. It passes `PrepareSessionRequest.Activity`
  (`ActivityRequest`: the hook argv `bonsai __agent-hook <provider>`, the env var
  name `BONSAI_AGENT_ACTIVITY`, the spool path) so the provider installs hooks
  for this session only. If the spool or tail cannot start, `Activity` stays nil
  and the session runs without hooks. `Deps.HookCommand` overrides the
  `<bonsai binary> __agent-hook` default; a test binary is never used as the
  default, so tests that want hooks pass their own.
- The hidden `bonsai __agent-hook <provider> <event>` (`main.go`, implemented by
  `internal/core/agenthook` through `agentruntime.RunHook`) is what the agent
  runs per hook. It reads `$BONSAI_AGENT_ACTIVITY` and does nothing unless that is
  an existing regular file named `activity.jsonl` directly inside a session
  directory of the agent sessions root (symlinks resolved), at most 4 MiB. It
  reads at most 1 MiB from stdin, asks the provider's `TranslateHook` for
  signals, stamps them with the hook's start time and appends them with one
  `write(2)` on an `O_APPEND` descriptor. It never creates the file, so a hook
  that outlives its session finds nothing and exits 0. It constructs providers
  without stores or config: keep `New` of every provider free of disk access.
- A provider whose CLI runs with another `HOME` (Antigravity) cannot let the hook
  derive the sessions root, so it appends `--sessions-root=<abs dir>` after the
  event in the hook command it writes. The argument replaces only the root: the
  spool must still be exactly `<root>/<id>/activity.jsonl`, a regular file of at
  most 4 MiB, symlinks resolved. It is read from a config file Bonsai wrote for
  the session, never from the environment the agent could redirect, and Claude
  and Codex do not use it.
- `TranslateHook` must return only kinds, reasons and a short `Detail` (a tool
  name), never prompts, tool inputs, tool output or file contents. A malformed
  payload yields no signals. Details are sanitized again on read.
- `RunHooks.Ending` stops the tail (after one last read) before
  `SessionStore.Cleanup` removes the runtime directory, so no goroutine reads a
  vanished file and a signal written after the process ended is ignored.
- The holder also feeds terminal signals to the same tracker. A provider that
  implements `TerminalClassifier` turns its terminal notifications into signals;
  `ActivityProfile.ProgressMeansTurn` lets an OSC 9;4 progress clear end a turn.

Tests: write spool lines directly (see `internal/core/agenthost/spool_test.go`),
or drive a real hook command through a holder with
`internal/providers/claude/testdata/fakeclaude.sh`, whose interactive sessions run
the hook command found in their `--settings` for each `HOOK <Event> <json>` line.

## Claude provider

`internal/providers/claude` runs Claude Code. Each profile owns
`<account dir>/config` as its `CLAUDE_CONFIG_DIR`, shared by all its sessions;
there is no credential vault, materialize or reconcile step. Launch environment:
every inherited `CLAUDE*` and `ANTHROPIC_*` variable is removed
(`EnvUnsetPrefixes`), then `CLAUDE_CONFIG_DIR` and `BONSAI_AGENT_*` are set
(`HOME` is untouched). Token profiles also get `CLAUDE_CODE_OAUTH_TOKEN` and
`CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1`. Argv is built in `launch.go` only:
`[explicit...] --model --permission-mode --effort --name --session-id <uuid> [-- prompt]`;
the generated UUID is the session's `provider_session_id`. When the holder
asks for activity (see [Activity hooks](#activity-hooks)) and the explicit args
carry neither `--settings` nor `--bare`, `--settings '<json>'` is added before the
prompt, with `async` command hooks (timeout 5 s) running
`'<bonsai>' '__agent-hook' 'claude' <Event>` for `SessionStart`,
`UserPromptSubmit`, `PreToolUse`, `PermissionRequest`, `PostToolUse`,
`PostToolUseFailure`, `Notification`, `Elicitation`, `ElicitationResult`, `Stop`,
`StopFailure` and `SessionEnd`, and `BONSAI_AGENT_ACTIVITY` is set to the spool.
Claude merges hooks across settings layers, so this adds to the profile's own
hooks and the profile's `settings.json` is never touched.
Both `PreToolUse` and `PermissionRequest` for `AskUserQuestion` report a question;
for `ExitPlanMode`, they report plan approval. A later generic
`Notification:permission_prompt` keeps an already known question, plan or
elicitation reason. A named tool permission request such as `Bash` still reports
permission, and a generic notice without an existing input reason does too.

`SetupSpec`: `login` "Browser sign-in" (recommended) and `token` "Setup token",
whose secret is optional (without one, setup runs `claude setup-token` in the
setup terminal, then prompts); seed source `~/.claude` or the host's
`CLAUDE_CONFIG_DIR`. Sign in again runs `auth login` + `auth status` (login) or
stores a new token (token); switching from token to login removes the stored
token after the login succeeded. Describe warnings for a logged-out, missing or
expired sign-in point at `bonsai agent account login <name>`. Profile defaults
(`bonsai agent account set|show`, the Agents tab's Defaults) are `model`,
`permission_mode` and `effort`, stored in the account settings.

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
implements `UsagePolicy` (1 minute), which `UsageService` prefers over its own
TTL. Tests point `claude.UsageEndpoint` or the provider's `usageEndpoint` at an
`httptest` server; nothing reaches the network.

## Codex provider

`internal/providers/codex` runs the Codex CLI. Each profile owns
`<account dir>/home` as its `CODEX_HOME`, shared by all its sessions; there is no
credential vault, materialize or reconcile step, and Bonsai never writes
`auth.json` (Codex rotates refresh tokens, so copies would log each other out).
Every invocation passes `-c cli_auth_credentials_store="file"`, and setup also
writes it into the profile's `config.toml`, so the login always lives in the
profile's `auth.json`. Launch environment: every inherited `CODEX_*` and
`OPENAI_*` variable is removed, then `CODEX_HOME`, `BONSAI_AGENT_*` and the host's
`CODEX_CA_CERTIFICATE` are set. Argv is built in `launch.go` only:
`[explicit...] -c store --no-daemon [-m] [-c model_reasoning_effort=] [-s -a | --dangerously-bypass-approvals-and-sandbox] [-- prompt]`.
Explicit args that start with a subcommand or a positional prompt
(`resume --last`, `login`) get only the global `-c store --no-daemon` in front, so
a resumed or forked TUI never attaches to a shared daemon that would outlive
**Stop agent** or the profile. The start dialog's `permissions` select is a preset that sends `sandbox` and
`approval` together (`bypass` is Bonsai's name for the bypass flag). Codex has no
`--session-id`, so `provider_session_id` stays empty.

`SetupSpec`: `chatgpt` "Browser sign-in", `device` "Device code" (recommended
instead of the browser when `WSL_DISTRO_NAME` or `SSH_CONNECTION` is set) and
`api-key` with an "OpenAI API key" secret; seed source `~/.codex` or the host's
`CODEX_HOME`. Sign in again runs `codex login [--device-auth]` or pipes the key
into `codex login --with-api-key`, then `login status`, in the profile's own
`CODEX_HOME`; Codex replaces `auth.json`, and running sessions pick the new login
up on their next refresh. A logged-out profile's warning points at
`bonsai agent account login <name>`. Profile defaults
(`bonsai agent account set|show`, the Agents tab's Defaults) are `model`,
`effort` and the `permissions` preset, stored as `sandbox`/`approval`.

Identity (the email) and plan are decoded from the `id_token` claims in `auth.json`
without a network call; auth status is the exit code of `codex login status`
(cached 60 s, 10 s when unknown). Short commands run in their own process group,
killed on timeout.

Models and usage come from `codex app-server` (`appserver.go`), Codex's published
JSON-RPC protocol over stdio. The client starts it with the profile's environment
and the file-store override, sends `initialize` and `initialized`, makes its
calls one at a time and closes stdin; the process group is killed on return or
after 15 s, stderr is discarded and at most 1 MiB is read. Concurrent reads of
one profile share one process. Errors carry the method and JSON-RPC code only,
never a server message or body, and Bonsai never writes `auth.json`.

- `models.go` (`ModelLister`): `model/list` for logged-in profiles, hidden models
  dropped, the default first, each model's own `efforts` and `default_effort`.
  Logged-out profiles, or a failed call, fall back to `codex debug models --bundled`
  (default = lowest priority among visible models); a failure of both is an empty
  list, never an error. Cached 10 minutes, 1 minute after a fallback. The effort
  of a launch is checked against the cached list of its model (or the default
  model) and against every shipped level when the list is not cached; validation
  never starts Codex.
- `usage.go` (`Usage`, `UsagePolicy` 1 minute): `account/rateLimits/read`. A
  window is named by its length: 300 minutes `five_hour`, 10080 `weekly`, any
  other `window_<n>m`; remaining is `1 - usedPercent/100`. Limits of other metered
  features (`rateLimitsByLimitId`) get their own group and ids prefixed by the
  limit id. The plan is `UsageSnapshot.Plan`; credits become a warning. API-key
  profiles are `ErrUsageUnsupported` and logged-out ones `ErrNotAuthenticated`,
  both decided from `auth.json` without starting Codex. Failures are remembered
  for a minute.
- `bonsai agent usage` reads slots, score and next reset from each provider's
  `UsageLayout` headline; `internal/cli/agent_usage.go` (`usageSpec`) only holds
  the table renderer per provider.

Activity (see [Activity hooks](#activity-hooks)): when the holder asks for it,
`PrepareSession` takes the profile's `hooks.lock` (in the account dir; not
`login.lock`, which a re-login holds for a long time), upserts Bonsai's handlers
into `$CODEX_HOME/hooks.json` and trusts them (`activity.go`, `hooktrust.go`).
The upsert identifies Bonsai's handlers by the command ending in
`'__agent-hook' 'codex' <Event>`, replaces them in place (keeping Codex's
position-derived hook keys stable, whatever the binary path), preserves
every other entry and key (the JSON is re-indented), and refuses to overwrite a file that is
not valid hooks JSON. Handlers are `async` with `timeout` 5 except `Interrupt`
and `SessionEnd`, and have no matcher (all occurrences). Trust runs
`hooks/list` on the app-server, keeps the entries whose `sourcePath` is the
profile's `hooks.json` and whose command is Bonsai's, and writes
`hooks.state."<key>".trusted_hash` with `config/batchWrite` (`keyPath`
`hooks.state`, `upsert`, explicit `filePath` of the profile's `config.toml`).
`<account dir>/hook-trust.json` remembers the trusted hashes under a fingerprint
of the hooks file and the Codex binary, so later launches only read files; it is
re-run when either changes or `config.toml` lost the hashes. Any failure is
ignored by the launch: the hooks stay untrusted and activity falls back to the
terminal. Launch adds `-c tui.notifications=["approval-requested","async-question"]
-c tui.notification_method="osc9" -c tui.notification_condition="always"` (each
only if the explicit args do not set that key), also in front of subcommands such
as `resume`. `TranslateHook` maps `request_user_input` to a question and
`request_user_input_async` to an outstanding async question. Async tool return,
background tool completions and `Stop` leave that question waiting; submitted
input, a new prompt, resolution, interrupt or failure clears the pending flag.
The flag is tracker bookkeeping and is never serialized. It maps
`SessionStart` with `source: "compact"` to nothing; `ClassifyTerminalNotification`
matches the prefixes of Codex's OSC 9 texts (`Approval requested`, `Codex wants to
edit`, `Question:`, `Plan mode prompt`) and never keeps the rest. Hook commands
get the session's environment, so `BONSAI_AGENT_ACTIVITY` reaches them. Concurrent
sessions of an account share the file, so a hook finds its session from that
variable and never from `hooks.json`; a session started before `bonsai update`
may stop reporting once a newer binary rewrites the file (Codex trusts hooks by
hash).

Tests never need a Codex login: `testdata/fakecodex.sh` is a POSIX stand-in put
first on `PATH`; `login` writes an `auth.json` with an unsigned JWT, `app-server`
replays the `testdata/phase0/` responses (re-numbering their ids; env switches
make it exit early, hang, flood or fail), `debug models --bundled` prints
`phase0/bundled-models.json`, and other runs print their argv and environment
and echo input as `REPLY:`. The fake app-server also answers `hooks/list` from
the profile's `hooks.json` and `config/batchWrite` (writing `[hooks.state]`
sections with a fake hash), and interactive runs fire the trusted hook commands
for each `HOOK <Event> <json>` line (and send `NOTIFY <text>` as an OSC 9
notification), as `fakeclaude.sh` does. `testdata/phase0/` holds redacted responses from a
real Codex 0.162.1 (`auth.json` shape, app-server `initialize`, `account/read`,
`account/rateLimits/read`, `model/list`, and a trimmed `debug models --bundled`);
`testdata/rate-limits-multi.json` is hand-written to cover extra limit groups.

### Codex in the web client

Codex needs no web code of its own: `AgentProviderId` and `PROVIDER_LABELS`
(`web/src/api/agents.ts`) list it, and everything else is driven by the provider
list. The Codex tab is enabled when the provider is available **and** has a
profile; otherwise its tooltip says why or shows the setup command
(`SETUP_HINTS` adds `[--auth device]`). The dialog renders the `permissions`
preset and the per-model `effort` select from the declared launch fields; the
effort list follows the chosen model (`gpt-6.1-sol` offers `ultra`, `gpt-6-luna`
does not) and a value the new model lacks falls back to "Profile default". The
model placeholder is the provider's default model. The profile row shows
`auth: <mode> · <email · plan> · <auth status>` and the profile's warnings, such
as a shared login. Submitting sends `sandbox`/`approval` (or `sandbox: bypass`)
and `effort` as flat request fields.

Browser coverage: `web/e2e/codex-agents.spec.ts` runs two sessions on one profile
and one on another against the mocked API (request bodies, node and terminal
marks, effort handling). `agent-terminal-fullstack.spec.ts` runs the real API and
PTYs with `fakecodex.sh` and `fakeclaude.sh` on `PATH` (HOME is a temp dir, no
seeding, no network): two Codex profiles with two sessions each plus one Claude
session, input routed to the right PTY, reload and reattach, stop one while the
others keep running. These fixture-server specs need ports 7011-7014 free.

## Agents tab

`/app/agents` (`web/src/features/agents/`, a lazy route chunk) lists every
provider and profile with sign-in state, usage and sessions, edits profile
defaults, renames and removes profiles, and creates or signs profiles in again
in the browser. Everything it renders comes from provider-declared data
(`launch_fields`, `profile_fields`, `setup`, `usage_note`, `install_hint`,
usage headlines); no provider ID appears in `localapi`, `internal/core/agents`
or web code.

| Route | Purpose |
| --- | --- |
| `GET /api/agents/providers` | Availability, version, `install_hint`, `launch_fields`, `profile_fields`, `setup`, `usage_note` |
| `GET /api/agents/accounts` | Described profiles with `created_at`/`updated_at` and `defaults`; `ETag`, `If-None-Match` → `304` |
| `GET`/`PATCH`/`DELETE /api/agents/accounts/{id}` | One profile; rename and defaults; guarded remove |
| `GET /api/agents/usage[?account_id=]` | Cached usage snapshots with the provider's `headline`, or `error: {code, message}` |
| `POST /api/agents/usage/refresh` | `{account_id?}`; bypasses the cache at most every 30 s per profile, else `throttled: true` |
| `POST /api/agents/setup`, `POST /api/agents/accounts/{id}/login` | Start a setup job (create, sign in again) |
| `GET`/`DELETE /api/agents/setup/{job}`, `POST …/secret`, `GET …/terminal` | Read, cancel, answer and attach to a job |

Usage errors are `unsupported`, `not_authenticated` or `unavailable` with a
generic message, never a provider body. `GET` serves cached snapshots (provider
`UsagePolicy` TTLs) and nothing on the first screen loads usage; the page polls
it every 60 s while visible. Session summaries carry the accepted launch values
(`model`, `options`: effort, permission mode, sandbox, approval, full access;
never the prompt), which the sessions list and the inspector show. The Start
agent dialog orders a provider's profiles by cached headroom and warns below
20 %; it never asks for a refresh.

`GET /api/agents/accounts/{id}` re-describes one profile.
`PATCH /api/agents/accounts/{id}` takes `{name?, defaults?: {field: value | null}}`
and writes both under the registry lock in one step (`400 invalid_name`,
`409 name_taken`, `400 invalid_profile` with the provider's message; never raw
settings). `DELETE` answers `{warnings}` (provider cleanup failures), or
`409 profile_in_use` with `error.session_ids` while a session of the profile is
active in any project, or `409 setup_busy` while a sign-in-again job of the
profile runs (its login writes into the profile directory); it holds the agents
and setup locks so no API start or sign-in slips in. Both publish
`agents_changed`. `bonsai agent account remove` has no view of the API's
sessions, so it warns (and still removes) when the session store holds records
of the profile: web and `agent run` sessions keep one until they end, and a
crash can leave one behind.

Web sign-in runs as a **setup job** (`internal/core/agentsetup`), in memory and
not project-scoped. `POST /api/agents/setup` (`{provider, name, auth_mode?,
seed?, secret?}`) creates a profile and `POST /api/agents/accounts/{id}/login`
(`{auth_mode?, secret?}`) signs one in again; both need `Idempotency-Key` and
answer `202` with the job (a retry with the same key and body gets the same job,
another body `409 mutation_conflict`). `GET`/`DELETE /api/agents/setup/{job}`
read or cancel it, `POST …/secret` answers a `waiting_secret` prompt (`204`, or
`409 no_prompt`), and `GET …/terminal` is a websocket with the agent terminal
protocol whose `status` is the job. States: `preparing → running →
(waiting_secret ↔ running)* → verifying → succeeded | failed | canceled`; a
succeeded job's HTTP answer carries the described `account`. The job's
`SetupIO` runs each login command with `agentterminal.RunInStream` in one
terminal (a `── <command> ──` line between commands), writes progress lines
CRLF-normalised into it and asks for secrets through the job. Limits: 2 active
jobs (`409 setup_limit`), one per provider and name or per account
(`409 setup_busy`), 15 minutes (then `failed`), ended jobs and their output
dropped after 2 minutes. Cancel, timeout and API shutdown (`Server.Close`) kill
the login's process group; a failed or canceled create removes the profile
directory. A pasted secret is turned into a reader at once and is never in a
response, the job, its fingerprint (the body hashed without it), a journal or a
log; Claude's `setup-token` prints its token into the terminal, which is why
the output is dropped. Without PTYs (native Windows) every setup route answers
`501 terminal_unsupported`.

**Freshness.** The accounts `ETag` hashes the whole answer, so a profile added,
renamed or signed in from the CLI changes it. The page revalidates on focus, on
visibility, after its own mutations, every 30 s while visible and on the global
`{"type":"agents_changed","revision":n}` event, which the API publishes for
account, setup-job and session lifecycle changes it makes (never for terminal
output).

**Secrets.** A pasted API key or token travels once, in the start body or a
prompt answer, becomes an `io.Reader` and is never in a response, job, log,
journal, idempotency fingerprint, warm-start cache or snapshot. The browser
clears the field after sending and keeps only the job ID in `sessionStorage` to
reattach after a reload. `--seed-from` stays CLI-only; the web seeds from the
provider's own source (`setup.seed.source`) or not at all.

**Browser coverage.** Mocked: `agents-tab.spec.ts` (status, sessions, usage,
screenshots), `agents-profile.spec.ts` (defaults, rename, remove) and
`agents-setup.spec.ts` (create, sign in again, prompt, cancel, CLI fallback).
Full stack: `agents-tab-fullstack.spec.ts` against `TestAgentsTabBrowserFixture`
(port 7012), the production agent runtime with no profiles, `fakecodex.sh` and
`fakeclaude.sh` on `PATH` and a temp `HOME`: it creates a Codex profile with a
device code (typing into the job terminal) and one with an API key (checking
that only the start request carries it), reads usage from the phase0 fixtures,
saves defaults, starts two sessions from the tab and checks their launch
details, gets `409 profile_in_use` on remove, stops both and removes the profile.

## Antigravity web terminals

The web client can start an existing Bonsai Antigravity profile in a configured
project worktree. Set up a profile with
`bonsai agent account add antigravity <name>` and ensure `agy` is on the API's
PATH. Claude (`claude`) and Codex (`codex`) are supported the same way: set up a
profile with `bonsai agent account add codex <name> [--auth device]`. Profile selection uses
opaque account IDs; discovery responses contain only ID, provider, display name and the
display-safe profile details. `SetupSpec` has no modes and no seed, only a note
that agy must be exited to save the profile: Bonsai ignores agy's exit code and
captures the sign-in from `<AccountDir>/home`, run from `<AccountDir>/home/workspace`.
`bonsai agent account login` runs agy again in that home, after removing the old
token from it, and stores the new sign-in with the next vault generation, so a
running session's older tokens never overwrite it when that session ends.

Account details never start `agy` on the profile-list path: `DescribeAccount`
reads the email from the vault (a fallback identity counts as none),
`auth_status` is `logged_in` when the stored credentials are complete, and the
plan comes from `<AccountDir>/plan.json`. `Usage` first reads the quota from
the Cloud Code API agy itself uses (`cloudcode.go`): `POST
/v1internal:loadCodeAssist` (`{"metadata":{"ideType":"ANTIGRAVITY"}}`, answers
`cloudaicompanionProject`, remembered per profile in memory, and the plan:
`paidTier.name`, or `currentTier.name` without a paid tier, and the credit
balance: the sum of `paidTier.availableCredits[].creditAmount` of type
`GOOGLE_ONE_AI`, an int64 string; the list is absent at 0, and a balance of 0 is
not shown. It is asked on every
read, beside the quota call once the project is remembered, and the plan is
stored in `plan.json` with the email) and `POST
/v1internal:retrieveUserQuotaSummary` (`{"project": ...}`, the same groups and
buckets as `/usage`, parsed by `ParseUsage`), on
`daily-cloudcode-pa.googleapis.com` then `cloudcode-pa.googleapis.com`, 3 s per
host (the second is asked at once when the first fails, or after 800 ms when it
has not answered: the daily host sometimes takes 3 s; the first answer wins), with the vault's access token and `User-Agent: antigravity` (without it
loadCodeAssist reports an unsupported client). Both are internal endpoints.
Bonsai never refreshes the token: one missing or expiring within a minute skips
the API. When every host fails, `Usage` runs the built-in `/usage` command (no
tokens) in a throwaway session (`withProfile`: materialize, run, reconcile,
clean up); agy refreshes the token there, so the next read can use the API
again. That fallback has no credits, and the plan is the one stored by the last
API read, or none. No `agy` is started for the plan or the credits, and nothing
runs in the background after `Usage` returns. A `loadCodeAssist` that fails
beside a working quota call leaves the plan and credits out of that read. Tests
point `cloudCodeHosts` at an `httptest` server.

Models come from `agy models` (cached 10 minutes, 1 minute after a failure).
Lines that differ only by an effort suffix in both the ID and the label
(`gemini-3.8-flash-high<TAB>Gemini 3.8 Flash (High)`) become one model whose
default effort is the first listed; a lone `-medium` model stays whole. A launch
composes model and effort into one `--model <id>`; agy's `--effort` is never used.

Activity ([Activity hooks](#activity-hooks)) uses agy's `hooks.json`. When the
holder asks for it, `PrepareSession` writes `<session home>/.gemini/config/hooks.json`
(0600; agy reads `~/.gemini/config/hooks.json` and the session `HOME` is private)
with one group, `bonsai-activity`: command hooks (timeout 5 s, run
synchronously by `sh -c`) for `SessionStart`, `PreInvocation`, `PostInvocation`,
`PostToolUse` (matcher `*`) and `Stop`, each running
`'<bonsai>' '__agent-hook' 'antigravity' <Event> '--sessions-root=<dir>'`. A
group of the same file that is not Bonsai's is kept. `PreToolUse` is never
registered: agy requires it to answer with a permission decision, so any hook on
it would change what the agent may do. Every hook prints `{}`, which agy's
documented contract allows for all five events (every field is optional, and for
`Stop` only `decision: "continue"` holds the agent). `TranslateHook` maps `PreInvocation` to a working signal,
`PostInvocation`/`PostToolUse` to tool-finished (the tool name is the only
detail), `SessionStart` to ready and `Stop` by `terminationReason`:
`ERROR` to a failed turn (detail `error`, never the error text), `USER_CANCELED`
to an interrupt, anything else (`NO_TOOL_CALL`, limits, hooks) to a completed
turn. agy has no permission or question hook, so waiting is inferred: the
session's `settings.json` gets `"notifications": true` (agy's `/config`
Notifications, a boolean) so that permission prompts and questions are written
to the terminal. `ClassifyTerminalNotification` recognizes the observed
`Antigravity CLI needs tool confirmation` notice along with older permission
and question notices. `TerminalScreenClassifier` refines this generic hint from
a bounded in-memory terminal grid: the numbered `Question N/M:` header with its
selection footer means a question; the command permission heading, confirmation
question and command-edit footer mean permission. It returns only kinds and
reasons, never question text, options or commands. Unchanged dialog paints do
not resurrect a question after the user submits an answer. Resizing rebuilds
the grid from the provider's next paint. All screen signals remain inferred.
`ProgressMeansTurn` lets agy's OSC 9;4 clear end a turn. The settings
key is restored to the profile's value before `Reconcile` copies the session
settings back (a marker in the runtime directory says it was set), and
`hooks.json` is never copied: the sign-in/plan home has none. Tests:
`testdata/fakeagy.sh` runs the hook commands of its `hooks.json` for each
`HOOK <Event> <json>` line.

Each session runs in its own [agent host](#agent-hosts), independently of
request contexts, browser connections and the API itself. Closing a terminal or
refreshing detaches; reconnecting resumes the same process. Explicit Stop
terminates the process group, reaps it, reconciles credentials using a fresh
cleanup context, and deletes its isolated session home. API shutdown, restart
or a crash only disconnects: the next API adopts every running holder, so its
sessions stay active in the journal and the Agents tab, and a browser holding
the holder's generation resumes at its offset. API restart never relaunches an
agent. Journal entries without a holder record (from before agent hosts) are
recovered as failed; a holder that died mid-session reads as failed,
`Agent host exited unexpectedly.` Terminal output lives in the holder only, so
it is gone once an ended session's holder exits.

Native PTYs are implemented for Linux, macOS, FreeBSD, OpenBSD, NetBSD and
DragonFly using `github.com/creack/pty` v1.1.24. Linux has runtime coverage here;
macOS and Windows builds are checked, but macOS runtime behavior needs native CI.
Windows and other platforms report interactive terminals as unsupported.
The adapter launches the provider's executable and argument vector directly,
with its isolated environment and working directory; there is no generic shell
or browser-supplied executable/path option.

Limits: 16 active sessions (adopted ones included), 32 retained completed sessions, 2 MiB of recent output
per session, 64 KiB per input frame, and 1–500 terminal rows/columns. Only one
attachment writes; other attachments observe. After the writer disconnects, a
new attachment can claim writing, and any attachment (browser or
`bonsai agents attach`) can take control at any time. Slow readers are disconnected. Bounded replay
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
  An agent's generation is its holder's, so it survives API restarts; a setup
  job's is the API epoch.
- Output is `{type:"output", offset, data}` where `data` is base64 bytes and
  `offset` is the absolute starting byte offset. Replay and subscription are
  registered atomically. The browser writes decoded bytes directly to xterm.
- A cursor outside the retained range receives `{type:"gap", offset}` followed
  by the retained tail. Generation changes reset the browser display/cursor.
- Input is `{type:"input", data}` (base64); resize is
  `{type:"resize", cols, rows}`. Input is never queued across reconnects.
  Without control they are dropped (control can move at any time).
- `{type:"take"}` asks for control of an agent terminal (setup jobs close the
  socket on it). `{type:"writer", offset, writer}` tells each terminal whose
  control changed, whether it now writes; the browser's new writer sends its
  size. The CLI (`bonsai agents attach`) takes part in the same election.
- Lifecycle updates are `{type:"status", offset, status:<safe agent summary>}`.
  Final status follows output drain. Protocol errors close the connection;
  clients also understand `{type:"error", message}` for future diagnostics.

Terminal bytes, sockets and replay cursors stay outside Zustand and browser
persistence. Safe session summaries travel through authoritative project
snapshots with their existing epoch/sequence ordering and `agents` freshness.
Worktree removal and root/project deselection conflict while an affected agent
is live. Ownership is resolved from the registry and current Git inventory.

`pnpm test:e2e` starts a dedicated Go fixture API on port 7014 on Linux/macOS.
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

## Agent hosts

Each agent session runs in its own holder, `bonsai __agent-host`
(`internal/core/agenthost`), started detached (new session, stdio on
`/dev/null`) so it does not depend on the process that launched it. The
holder owns the session's PTY, its 2 MiB terminal stream and the whole
lifecycle `agentterminal.RunSession` runs: `PrepareSession`, the process,
`FinalizeSession` and the session store's cleanup. A per-agent holder rather
than one shared agent daemon keeps a crash to one session, and lets an
upgrade leave running sessions alone: an old holder keeps its own binary and
protocol version.

The launch spec, prompt included, reaches the holder on stdin and is never
written to disk or argv. The holder answers one JSON line on stdout (`{ok}` or
`{error}`) once its socket serves, then points stdout at `/dev/null`.
`BONSAI_DAEMON_BIN` overrides the binary, as for daemons.

| File | Purpose |
| --- | --- |
| `<socket dir>/agents/<session>.sock` | The holder's socket (0600, directory 0700); the socket dir is `$XDG_RUNTIME_DIR/bonsai`, else `/tmp/bonsai` |
| `<web home>/agents/<session>.json` | Its record (0600, atomic): `pid`, `socket`, `generation`, `binary_version`, `protocol`, `work_dir`, the session summary, `last_output_at`; rewritten on every state change |
| `<web home>/agents/<session>.lock` | Held by the holder for its lifetime: a record whose lock is free belongs to an ended holder, and one still showing an active session reads as failed, `Agent host exited unexpectedly.` |

The API's `agenthost.Manager` starts a holder per session, follows each over a
status connection (redialing while the holder runs), proxies each browser
terminal websocket to its own holder connection, and on start adopts every
running holder (`Adopt`). Closing the API only disconnects. Tests use
`agenthost.InProcess`, which serves holders from the test process so fake
providers still apply.

`agents.keep_running` in `web.json` (`GET`/`POST /api/settings/agents` with
`{keep_running?, notifications?, revision}`, `409` on a stale revision; the
answer adds `active_sessions` and `notifications`) only changes `bonsai web stop`:
off, it stops the live holders before `ServeStop` (so the API records their end)
and sweeps any survivor after it. API shutdown never stops holders, whatever the
setting.

`agents.notifications` (`enabled` off by default, `needs_input` and
`turn_complete` on) is read once by `bonsai __agent-host` from `web.json` and
passed in `agenthost.Deps.Notifications`; any read failure means no
notifications. The holder subscribes a desktop notifier (`agenthost/notify.go`,
sender `core/notify`) to its own activity bus; the API never notifies. The
settings endpoint takes the toggles as optional fields of `notifications`.

Ended records are pruned when a holder starts: the newest 32 are kept, none
older than a day. After its session ends the holder rewrites its record,
broadcasts the final status, waits up to 10 s for its clients to disconnect,
then exits. SIGTERM stops the session (SIGTERM to the process group, SIGKILL
after 2 s).

The holder protocol (version 1) is newline-delimited JSON. A client sends
`{type:"hello", version, mode, offset, generation, take, read_only, client}` first:
`terminal` mode attaches to the stream (replay from `offset` when `generation`
matches the holder's, else from the start; `take` writes at once, `read_only` never writes), `status`
mode receives summaries only and is never the writer. The holder answers
`{type:"ready", version, generation, writer}` (status mode adds the summary),
then the terminal stream's `output`, `gap` and `status` frames, and
`{type:"writer", writer}` to the clients whose control changed. Requests:
`input`, `resize` (writer only, else an `error` frame), `take`, `stop`
(optional `error` to record on the session) and `info`, answered by
`{type:"info", info:{summary, pid, clients:[{client, writer}], last_output_at,
cols, rows}}`. A hello of another version gets an `error` frame; clients check
the record's `protocol` first and leave such a holder alone.

## Release and deferred architecture

See [releasing.md](releasing.md) for embedded release builds and archive smoke
checks and [architecture.md](architecture.md) for future multi-workspace
constraints. These documents replace the temporary web
project and Phase 7 plans.
