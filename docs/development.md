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

Routine development does not require GitHub. The repository includes signed
fixtures under `testdata/webhooks/`. With the hidden stack running:

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

Local API protocol **2** requires the browser and backend to be upgraded together.
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
