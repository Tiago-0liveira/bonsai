# Git and GitHub backend

Bonsai Web has two deliberately separate backends:

```text
app.bonsai.dev
   |                         GitHub
   | loopback HTTP/WS          |
   v                           | HTTPS webhook
127.0.0.1:7001                 v
local Bonsai API          api.bonsai.dev
   |                           |
   | daemon Git bridge         | normalized metadata only
   v                           v
local repositories         authenticated SSE
```

There is no connection from `api.bonsai.dev` to the local daemon or local
Bonsai API.

## Local Bonsai API

`bonsai serve` supervises one browser API on `127.0.0.1:7001`. Production
browser requests must use the exact `https://app.bonsai.dev` Origin and exact
loopback Host. This production path cannot start a webhook listener, Vite,
tunnel, sidecar, or development browser origin.

Contributor integration testing uses the separate hidden
`bonsai __serve-dev-stack` entrypoint. That mode keeps the same loopback,
Host/Origin, capability, strict-request, idempotency, and WebSocket trust
boundaries while explicitly authorizing its local Vite origin. The development
webhook relay only emits normalized event metadata; the browser then refreshes
canonical state from the local API. See [development.md](development.md).

The browser creates a short-lived in-memory capability with
`POST /api/session` and sends it in `X-Bonsai-Session`. Local capabilities
are never cookies, URLs, or cloud credentials and die with the local API
process.

Local Git mutations remain structured commands executed through the daemon Git
bridge. There is no generic shell or exec HTTP endpoint. GitHub PR/check/workflow
operations are performed locally through the installed `gh` CLI.

The local event endpoint is `ws://127.0.0.1:7001/events`. The first WebSocket
message authenticates with the local capability. Local events are canonical
refresh signals for local state.

## Internet GitHub relay

The internet service is implemented in `internal/server/relay` and exposed by
`cmd/bonsai-relay`. The legacy `cmd/bonsai-server` binary is a compatibility
alias for the same relay-only server; it no longer registers project, Git,
device, daemon, process, agent, or filesystem routes.

The intended production origins are fixed:

- frontend: `https://app.bonsai.dev`
- relay: `https://api.bonsai.dev`

The relay surface is intentionally small:

```text
GET  /healthz
GET  /auth/github
GET  /auth/github/callback
GET  /auth/session
POST /auth/logout
POST /webhooks/github
GET  /events
```

`/auth/session` is status-only and exists so the frontend can decide whether
to open the credentialed EventSource.

### Relay configuration

Create a GitHub App and configure:

- callback URL: `https://api.bonsai.dev/auth/github/callback`
- webhook URL: `https://api.bonsai.dev/webhooks/github`
- expiring user access tokens enabled
- webhook events required by Bonsai's normalized event allowlist

The relay needs only:

```text
GITHUB_APP_CLIENT_ID
GITHUB_APP_CLIENT_SECRET
GITHUB_WEBHOOK_SECRET
```

It does not need a daemon credential, local Bonsai capability, repository path,
shell credential, static web root, GitHub App private key, or Bonsai account
database.

Example configuration:

```json
{
  "address": "127.0.0.1:8080",
  "database": "/var/lib/bonsai/relay.json",
  "external_url": "https://api.bonsai.dev",
  "frontend_origin": "https://app.bonsai.dev"
}
```

Terminate public TLS at a trusted reverse proxy and preserve streaming
responses. The relay's configured external URL must use HTTPS.

## Relay authorization

GitHub is the identity source. `GET /auth/github` creates a random one-time
OAuth state, stores only its hash, and sets a Secure, HttpOnly, SameSite=Lax
state cookie. The callback consumes that state, obtains the GitHub numeric user
ID and the installations/repositories visible to that user, then creates a
short-lived relay session.

Relay session secrets are random and stored only as hashes. The browser receives
the secret only in a Secure, HttpOnly, SameSite=Lax cookie. Session records
contain only GitHub user/install/repository authorization metadata and expiry.

`POST /auth/logout` requires the exact `https://app.bonsai.dev` Origin,
removes the session, and immediately closes active SSE subscribers for that
session.

The relay has no Bonsai username/password, profile, roles, organization
membership database, or PAT flow.

## Webhook ingress

The sole GitHub ingress is `POST /webhooks/github`.

Processing order is:

1. enforce POST and a 2 MiB body bound;
2. read the raw body;
3. verify `X-Hub-Signature-256`;
4. validate bounded delivery/event headers;
5. normalize the verified JSON with `internal/server/webhooks`;
6. verify repository/installation scope against GitHub-authorized relay sessions;
7. durably deduplicate the delivery;
8. append normalized metadata to the bounded relay event store;
9. signal active authorized SSE subscribers;
10. return without GitHub API reconciliation.

Signature verification happens before JSON parsing. Unsupported event/action
pairs are safely acknowledged without creating a relay event.

Delivery dedupe persists the GitHub delivery ID, SHA-256 payload digest,
received time, normalized event metadata, and assigned sequence. Reusing a
delivery ID with the same digest is acknowledged without a second event.
Reusing it with a different digest returns conflict.

The relay never stores raw webhook payloads after normalization.

## Event schema and SSE

Cloud events are non-executable metadata. They may contain:

- delivery ID and sequence;
- GitHub event/action;
- repository and installation IDs;
- PR/issue number;
- branch/ref;
- check SHA;
- workflow branch;
- installation repository additions/removals;
- receipt timestamp.

They do not contain local paths, source files, diffs, terminal data, commands,
argv, shell, cwd, agent prompts/output, worktree paths, daemon credentials, or
local Bonsai session tokens.

`GET /events` authenticates only from the relay-session cookie. Repository IDs
in the query string are ignored and cannot expand access. Authorization is
rechecked against the session for every replay/live event.

SSE behavior includes:

- `Content-Type: text/event-stream`;
- `Cache-Control: no-cache`;
- `X-Accel-Buffering: no`;
- 20-second heartbeats;
- `Last-Event-ID` replay;
- bounded event and delivery retention;
- `reset` when a cursor is outside retained history;
- bounded subscriber queues;
- disconnect of slow consumers;
- up to eight concurrent streams per relay session.

Multiple tabs/devices use independent subscribers. The relay does not model a
user as one global socket.

## Browser invalidation flow

Relay events are invalidation signals only:

```text
GitHub webhook
   -> api.bonsai.dev
   -> normalized SSE metadata
   -> app.bonsai.dev marks the matching GitHub repository stale
   -> browser asks 127.0.0.1:7001 for fresh state
   -> local Git/gh state becomes canonical
```

The web client never applies webhook fields directly to canonical repository,
worktree, PR, check, or workflow state.

## Durable relay state

The relay uses a dedicated durable metadata store separate from local Bonsai
state. It persists OAuth-state hashes, relay-session hashes and repository
grants, webhook delivery digests, event sequences, and normalized relay events.
Retention is bounded and raw webhook bodies are not retained.

The current implementation uses an atomically replaced relay metadata file to
avoid adding a new database dependency in this branch. The store interface is
isolated so a SQLite WAL implementation can replace it before horizontal
multi-process deployment without changing the relay HTTP/SSE boundary.

## Security/architecture checks

Relay tests cover HMAC verification, payload bounds, invalid JSON, delivery
dedupe across restart, conflicting delivery digests, repository authorization,
logout, query-string spoofing, SSE replay/reset/heartbeat, multiple subscribers,
slow-consumer eviction, and OAuth-derived repository grants.

A static import test rejects direct dependencies from the relay executables and
relay package on daemon packages, local Git execution, the old cloud Git service,
and `os/exec`.

Run backend validation with:

```sh
go test ./...
go test -race ./...
```

Frontend relay code uses:

```js
new EventSource("https://api.bonsai.dev/events", {
  withCredentials: true
})
```

and refreshes the corresponding project through the hardened loopback API when
an authorized relay event arrives.
