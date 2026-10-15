# Local web architecture

The product has one user-level web supervisor, independent of the launch
repository. It owns the API/UI, optional webhook receiver and one configured
tunnel. Each configured project keeps a repository daemon as Git/process
authority. Canonical paths and stable hashed IDs distinguish clones even when
they share a GitHub remote. Provider reads coalesce by repository identity.

Local API protocol **3** and settings schema **1** are unchanged. HTTP capabilities
and first-frame WebSocket authentication protect the loopback boundary;
[git-backend.md](git-backend.md) describes exact hosts, origins and mutation
ownership. GitHub uses the existing `gh` token locally through the in-process
REST client, with an in-memory ETag cache. No GitHub App installation or cloud
OAuth/session is needed.

Project discovery is bounded and revision checked. Failed scans retain known
projects as unavailable, while successful scans can remove absent entries.
Settings transactions are cross-process locked and atomically replaced.
Local inventory publishes before provider enrichment. Epoch/sequence ordering
prevents stale HTTP reads and late events from overwriting current state.
Provider failure retains last successful data as stale and leaves local work
usable; only a successful empty result clears an authoritative list.

Disk warm-start metadata is an explicitly sanitized cache. It creates sequence
0 in a new backend epoch and stays stale until live validation. Runtime process
and agent authority is always read afresh. Browser persistence contains approved
preferences, never credentials, snapshots, terminal output or environment values.

The shell and retained branches remain available while lazy features load.
Import failures are recoverable with an explicit reload; there is no automatic
reload loop. Development SSE is an opt-in loopback invalidation path, and product
updates use the backend WebSocket. Public hosting has been retired in source.

## Deferred multi-workspace design

Multi-workspace pairing is future work. The proposed model is hub and spoke:
the machine serving the UI proxies to paired hosts server-to-server so the
browser still uses one origin. Only the hub receives webhooks and fans out
invalidation; polling should be coalesced by GitHub repository across hosts.
Pairing authentication and TLS are prerequisites for any non-loopback listener.
No current flag or legacy setting permits public API access.

Future connections, sessions, projections and event IDs must be scoped by host;
today the host is local. Preserve per-backend epoch/sequence semantics and avoid
adding state that makes host scoping difficult. The `workspaces` settings key
is reserved for that future model and is currently neither used nor emitted.
WSL/Windows discovery and short-code pairing need explicit design and native
verification before implementation.

Persisted ETag representations, optional cold-load GraphQL and moving layout
to a worker remain deferred until measurements justify them. They are not
required for current warm-start or bundle gates.
