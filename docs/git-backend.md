# Local web security and Git backend

```text
local browser /app ── HTTP capability / authenticated WebSocket ── local API
                                                                     │
                                                       repository daemon / Git
                                                                     │
                                                        GitHub REST using gh
GitHub ── optional HTTPS tunnel ── loopback webhook receiver ── invalidation
```

`bonsai web` runs a user-level supervisor and serves its embedded UI from the
API. The API binds to an explicit loopback IP. Production requires an empty
additional browser origin and accepts exactly its listen origin and
`http://localhost:<same-port>`. Host validation uses those exact names, including
on static assets and all WebSocket upgrades. DNS-rebinding names, public
origins, different ports, wildcard origins and missing API origins are rejected.
Same-origin GET/HEAD requests may infer their origin from `Sec-Fetch-Site:
same-origin`; they still require a capability. Static UI paths and public
health/version probes need no session.

The browser obtains an in-memory capability from `POST /api/session` and sends
it as `X-Bonsai-Session`. Tokens never enter cookies, persistent browser storage
or URLs. An API restart invalidates them. WebSockets authenticate in their first
data frame. Privileged mutations retain strict JSON parsing, body limits,
idempotency, current project/worktree ownership and operation locks. There is
no generic shell or exec HTTP endpoint.

The UI connects automatically to its own loopback origin and never probes
browser network permissions. Its CSP allows `connect-src 'self'`; hashed
assets have immutable caching and gzip, while `/app` and deep links use
`no-store`. Untagged Go builds serve an explanatory placeholder. Production
embed tests reject test-store hooks, cloud fallbacks and OAuth routes.

Development mode explicitly authorizes one loopback HTTP frontend origin with
a valid port, retaining all other checks. The hidden contributor SSE relay has
no cloud session or OAuth flow. It carries invalidation metadata only. See
[development.md](development.md).

## Optional product webhook exposure

Standard mode polls visible GitHub projects every 30 seconds (15 seconds for
running CI) and background projects every 5 minutes. Conditional REST requests
use the in-memory ETag cache and respect rate-limit backoff.

Live mode adds a second loopback listener that serves only
`POST /github/webhook`. Cloudflare quick/named, ngrok, Tailscale Funnel, external
URLs and custom commands remain available. Tunnel validation targets this
receiver; never expose the API, control WebSockets, daemon sockets, Vite or
project services. Named Cloudflare setup requires explicit Review confirmation
before creating a tunnel or DNS route and never overwrites a conflicting record.

The receiver checks HMAC-SHA256 before normalization, caps bodies at 2 MiB,
bounds headers, checks event/action allowlists and deduplicates up to 4096
in-memory delivery IDs by payload digest. Repository identity and checked SHA
scope refreshes to matching local projects. Normalized events contain no
commands, local paths or unrestricted text. Secrets live in private files,
never URLs, settings, logs or browser state.

Bonsai registers repository webhooks using the existing `gh` login; repository
admin access and suitable token scopes are required. Ping/delivery health
controls whether Live is active. Healthy Live repositories retain a 10-minute
safety poll. Unhealthy hooks or tunnels fall back to ordinary polling. Setup,
status and doctor expose waiting/paused states and actionable fixes.

The hosted frontend, internet relay, cloud OAuth/session implementation and
hosting/deployment inputs have been removed. This repository change performs no
external hosting shutdown, DNS change or public deployment.

### Worktree state synchronization

Local API protocol 3 keeps one in-memory projection per discovered project. The
projection is a cache, not a second Git database or process supervisor: repository
and process truth still comes from each repository daemon and persisted process
records. An authenticated WebSocket subscription is registered before the server
sends `ready`; `ready` includes a backend epoch. Every committed project
projection has a monotonically increasing per-project sequence, and invalidation
events include `{project_id, epoch, sequence}`. Browsers discard older responses
and repeat a trailing read when an event overtakes an in-flight request.

A cold project publishes known worktree/branch/remote inventory first (the
`git.inventory` read, a subset of `git.repository.refresh`), which also lets the
provider cache warm while worktree statuses run. Full local status (a bounded
pool, one `git status` per worktree with untracked directories listed once) and
processes follow independently. Local Git reacts to daemon invalidation/watchers with a 30-second recovery
poll. Process recovery and project-root discovery also scan every 30 seconds. One inaccessible worktree produces an
explicit status error without erasing healthy siblings. Missing/deleted upstream
tracking is represented as unavailable divergence, not `0 ahead / 0 behind`.

GitHub enrichment is asynchronous. Provider repository/PR/branch data is cached
for 20 seconds; checks reuse data for 60 seconds, or 10 seconds while pending. Provider reads are
coalesced across clients and local clones, use bounded worker pools, and retain
last successful values as stale on failure. PR association uses provider
repository identity plus head branch so fork PRs are not matched only by branch
name. CI keeps the checked SHA and distinguishes `unknown`, `none`, `running`,
`passed`, and `failed`. The rollup uses the existing combined check-runs and
commit-status API; it does not make a second workflow-runs request.

`POST /api/projects/{projectId}/refresh` accepts a scoped `local`, `provider`,
or `all` invalidation and immediately returns cached projection state while work
is queued. Product webhooks and contributor SSE use the provider scope for every matching
local clone.
Ordinary snapshot reads do not force provider requests.

An active canvas sends an authenticated, idempotent
`POST /api/projects/{projectId}/sync` on initial load and every five minutes.
Explicit Sync uses the same queue. Multiple tabs coalesce into one daemon job
per clone; local inventory is published before network Git work. Snapshot GETs
and local status polling stay free of Git network mutations.

The daemon fetches origin with pruning and an explicit full branch refspec,
covering narrow clones without changing their configuration. It then pulls the
main working copy with `--ff-only --no-rebase --no-autostash` only when clean,
attached, and tracking a valid origin upstream with no active Git operation.
Dirty, detached, unborn, missing-upstream, and diverged cases are reported as
skips. Fetch, pull, and provider outcomes and successful timestamps remain
separate; errors preserve useful local state and previous successful freshness.
Repositories without origin skip automatic Git network operations. Push remains
explicit. Ahead/behind describes the fetched local tracking refs.

Canonical snapshots include connection reasons, a stable Local / unlinked
worktree group, and branch candidates identified by full ref and remote identity.
The main working copy is excluded from the group. Provider-only branches require
fetching before checkout; fork/deleted-head PRs cannot manufacture origin refs.
Branch timestamps are tip commit times; PR timestamps are provider update times;
Last synced is the successful fetch completion time. Freshness-only browser
updates preserve canvas placement and viewport state.

PR catalogs list all states and resume in batches of five pages per enrichment
job. Initial pages are published as they arrive; the previous complete catalog
remains visible during replacement. The PR catalog and worktree associations are
published before CI checks finish. Updated ordering supports incremental refresh
between daily complete reconciliations. The canvas exposes worktree creation and
confirmed deletion.
Deletion retains branches and PRs, rechecks dirty state and Git operations, and
requires tracked processes and agents to stop. Creation and removal use durable
mutation journals; metadata retries after creation do not create another tree.
Worktrees whose directories have disappeared remain visible as missing Git
registrations. Confirmed removal clears only the selected registration and its
metadata, retaining the branch and respecting Git's worktree locks and tracked
process protections. Other stale registrations are not automatically pruned.

## Durable state and recovery

Settings schema and local API protocol remain **1** and **3**, respectively.
Old version-1 `interfaces` fields are ignored and omitted on the next save.
Obsolete `serve.*` YAML cannot affect product or contributor startup. A running
stack with a nonempty browser origin is replaced; older API version replacement
is retained.

Project settings are revisioned, locked and atomically replaced. Never turn an
unreadable or future-schema settings file into empty defaults. Warm-start
metadata is a disposable cache with an explicit safe DTO and strict size/age
limits; restored values remain stale until current authorities validate them.
See [performance.md](performance.md), [web.md](web.md) and
[architecture.md](architecture.md).
