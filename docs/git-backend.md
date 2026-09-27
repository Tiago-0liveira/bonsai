# Git backend

Bonsai's Git API runs in `cmd/bonsai-server`. Local Git commands execute in the
existing per-repository daemon, using a separate outbound `wss` bridge. The
Unix-socket process protocol remains local. The server never runs `git` or `gh`.
CLI/TUI imports now use compatibility exports in `internal/git/local` and
`internal/git/github/ghcli`; their existing behavior, including interactive
merge/rebase, is preserved.

## Server setup

Create a GitHub App with expiring user access tokens enabled. Configure:

- Callback URL: `https://bonsai.example/auth/github/callback`
- Webhook URL: `https://bonsai.example/webhooks/github`
- Repository permissions: metadata read, contents write (user merges), pull
  requests write, issues write (conversation), checks read, actions read, commit
  statuses read. Installation read tokens are restricted to one repository.
- Events: push, pull request, pull request review, pull request review comment,
  issue comment, check run, check suite, workflow run/job, and release. Add
  deployment read and deployment/status events only when displaying that state.
  Installation and repository access changes invalidate cached access.

Provide these environment variables to the server:

```text
GITHUB_APP_ID
GITHUB_APP_CLIENT_ID
GITHUB_APP_CLIENT_SECRET
GITHUB_APP_PRIVATE_KEY_FILE
GITHUB_WEBHOOK_SECRET
BONSAI_TOKEN_KEY              # base64 encoding of 32 random bytes
TLS_CERT / TLS_KEY            # optional with a trusted TLS reverse proxy
```

`BONSAI_TOKEN_KEY` encrypts GitHub user tokens at rest. Keep it stable across
restarts and outside the metadata database. Device and session credentials are
stored as hashes. No PAT is required or accepted by the web configuration.

Example `server.json` (member keys are numeric GitHub user IDs, not usernames):

```json
{
  "origin": "https://bonsai.example",
  "address": "127.0.0.1:8080",
  "database": "/var/lib/bonsai/git.json",
  "static_dir": "/srv/bonsai/web/dist",
  "repositories": [{
    "id": "bonsai",
    "workspace_id": "personal",
    "full_name": "owner/bonsai",
    "github_repository_id": 123456,
    "installation_id": 789,
    "default_branch": "main",
    "members": {"12345": "write", "67890": "read"}
  }]
}
```

Build and run:

```sh
npm --prefix web ci
npm --prefix web run build
go build -o /tmp/bonsai-server ./cmd/bonsai-server
/tmp/bonsai-server server.json
```

Use HTTPS, including during development (a TLS reverse proxy is sufficient).
Preserve `Origin`, WebSocket upgrades, and SSE streaming at the proxy. Sessions
use Secure, HttpOnly, SameSite cookies. Mutations require the configured Origin.
The server defaults to loopback when TLS terminates at a reverse proxy.

## Connect a repository

Sign in at `/auth/github`. From a signed-in same-origin client, enroll a device:

```js
const enrollment = await fetch('/api/devices', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ repository_ids: ['bonsai'] })
}).then(r => r.json())
```

The response contains a one-time displayed `credential` and a device ID. On the
repository's machine, place the following in `<main-repo>/.bonsai/git-bridge.json`
with permissions `0600` (the daemon creates a self-ignoring `.bonsai` directory):

```json
{
  "repository_id": "bonsai",
  "url": "wss://bonsai.example/api/daemon/connect",
  "credential": "<enrollment credential>",
  "worktree_root": "/absolute/path/to/bonsai-worktrees"
}
```

Start the updated daemon with `bonsai __daemon --repo /absolute/main/repo`.
Stop a previous idle daemon with `bonsai daemon stop` before starting the new
build. Do not force-stop supervised processes simply to reload the bridge.
The configured bridge keeps the daemon alive without supervised processes.
Revoke a device with `DELETE /api/devices/<device-id>` from its owner's session.
The credential grants Git access only to the enrolled repositories.

The legacy Unix socket also accepts `{ "kind": "git", "git": <Command> }` for
local CLI consumers. Without bridge configuration its repository ID is `local`.
Local clients provide command ID, user ID, repository ID and creation timestamp.

## API and state ownership

Project reads: `/api/projects`, `/api/projects/:id/git`, `/branches`, `/worktrees`,
`/pull-requests`, `/pull-requests/:number`, `/checks/:sha`, `/workflows?branch=...`.
Worktree reads: `/api/worktrees/:id/status`, `/files`, `/files/*path`, `/diff`.
Diff modes are `working`, `staged`, and `branch` (the latter requires `base`).
Supply `path` for a per-file diff. Files are relative to a registered worktree;
Git/Bonsai internal directories, traversal, and escaping symlinks are blocked.
File contents are bounded to 4 MiB; binary content is identified without decoding.

The `/git` snapshot includes independently sourced `local` and `remote` objects,
metadata, daemon availability, and an event cursor. A GitHub branch SHA is never
copied into `local_remote_ref_sha`. `?fresh=true` reconciles through the daemon.
An offline daemon returns a typed `daemon_offline` error for commands; snapshots
remain available with `online: false`.

Local mutations use `/api/worktrees/:id/{fetch,pull,push,commit,rebase,merge}`.
Additional endpoints are `stage`, `unstage`, `operations/continue`, and
`operations/abort`. Create worktrees at `POST /api/projects/:id/worktrees`:

```json
{"mode":"new","branch":"feat/example","base":"main"}
```

Other modes: `existing` (a free local branch) and `remote` (e.g. base
`origin/feature`, branch `feature`). Remote creation fetches origin first.
Paths are allocated by the daemon. Mutation responses include a refreshed
snapshot. Delete with `DELETE /api/worktrees/:id`; dirty removal requires
`{"confirm_discard":true}` and the main worktree cannot be removed.

Commit accepts `{"message":"..."}` and commits only the current index. Stage or
unstage explicit `{"paths":["file"]}` first. Push never forces; setting the
upstream requires `{"set_upstream":true}`. Rebase/merge accept `{"target":"main"}`.
Conflict operations return their operation ID, conflicting paths, Git state,
and abort/continue capabilities. Resolve files locally and stage resolutions,
then POST `{"operation_id":"..."}` to the recovery endpoint. After a daemon
restart, `operation_id: "recover"` recovers the actual merge/rebase in that
worktree, without guessing based on a previous command response.

PR creation accepts title/body/head/base/draft. PR mutation actions are reviews,
comments, ready, close, reopen, and merge. Review events are `APPROVE`,
`REQUEST_CHANGES`, and `COMMENT`. Merge requires `method` and `head_sha`; GitHub
checks branch protection and rejects stale heads. Mergeability remains
`unknown` until GitHub reports a decision.

Use a stable `Idempotency-Key` when retrying mutations (required for GitHub
mutations). Reusing it with different arguments is rejected. The daemon keeps
an audit/result journal across reconnects and restarts. An interrupted command
with an uncertain outcome is not automatically re-executed. GitHub mutations
with an uncertain network result require reconciliation before a new action.
This is deliberately not an exactly-once claim across external Git/GitHub writes.

## Events and recovery

Browsers load a snapshot, then connect to `/api/events?after=<sequence>` using
SSE. Events are ordered, scoped to authorized projects, and replayed using
Last-Event-ID. A `reset` event requires reloading snapshots. The most recent
10,000 events are retained. The daemon uses bidirectional WebSocket messages,
durable snapshot acknowledgment cursors, persisted pending command replay, and
full reconciliation on every connection. Reconciliation supersedes missed
intermediate snapshots; it never replays stale local file state.

Filesystem watching debounces bursts for 100 ms and emits changed semantic
snapshots, including repeated edits to already-dirty files. It watches worktrees
and Git metadata, discovers new directories/worktrees, and reconciles every
30 seconds. Git mutations trigger an additional refresh. GitHub reconciliation
runs after relevant webhooks and every five minutes. Webhooks are authenticated,
deduplicated and persisted before a 202 response; workers retry failures with
backoff. Pushes publish remote state and throttle optional daemon fetches.

## Operational limits and verification

The current store is an atomic, synced JSON metadata database with a process
lock: run one server writer per database. It persists snapshots, metadata,
delivery status, event cursors and command audits, but not file contents or full
Git history. For horizontally scaled deployments, replace the store with a
transactional shared database and shared event/command routing before scaling.
Back up the database and encryption key together. Journals and processed webhook
IDs should be retained while clients may retry; they are not auto-pruned.

Filesystem events recompute only affected worktree statuses; ref changes and
periodic reconciliation refresh the registered repository. Git status is one
invocation per worktree, not one per file. File watching skips
node_modules, Git objects and caches; periodic reconciliation remains the
correctness fallback. Very large repositories should be benchmarked before
choosing watcher/refresh limits.

Validation includes real Git worktrees/remotes and conflict recovery,
retry-after-delete and restart behavior, cross-project authorization, a real
WebSocket server-to-daemon command round trip, webhook signatures/dedupe/restart,
ordered event replay, token scope/encryption, and watcher semantic deduplication.
Run `go test -race ./...`, `npm --prefix web test`, and
`npm --prefix web run build`. Live GitHub OAuth/webhook delivery requires an
installed App and configured credentials; local tests use HTTP fixtures.

Non-Git prototype features (agents, process terminal, board, environment editor)
remain outside this backend overhaul. Git fixtures live only under test fixtures;
the running web app obtains Git state from this API.

GitHub references: [installation authentication](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/authenticating-as-a-github-app-installation),
[user access tokens](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-user-access-token-for-a-github-app),
and [webhook verification](https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries).


## Local `bonsai serve` webhook boundary

`bonsai serve` uses a stricter local runtime split than the legacy combined
deployment entrypoint. The daemon owns three core process groups: the API on
`127.0.0.1:$BONSAI_API_PORT`, the frontend on
`127.0.0.1:$BONSAI_WEB_PORT`, and a dedicated webhook listener on
`127.0.0.1:$BONSAI_WEBHOOK_PORT`. Only the webhook port is intended to be a
tunnel target.

The webhook listener accepts only `POST /github/webhook`. It bounds the raw
body, validates `X-Hub-Signature-256` before JSON parsing, allowlists GitHub
event/action pairs, validates the configured repository/installation, and
normalizes the payload to non-executable event fields. It cannot invoke git,
shell commands, worktree operations, or general API handlers.

For every serve group the daemon writes a random 256-bit session secret with
user-only permissions. Only the API and webhook process receive the secret-file
path. The webhook listener signs each normalized event with the timestamp,
GitHub delivery ID, and exact normalized body. The API rejects stale or invalid
signatures and durably deduplicates delivery IDs before processing. A tunnel
must never target the API, frontend, or daemon socket.
