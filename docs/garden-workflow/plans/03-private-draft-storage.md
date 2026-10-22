# 03 / Durable private drafts

Status: done.

## Deliverable

Provide an injected core/backend store for unpublished ideas that survives restarts
and concurrent Bonsai processes without creating anything on GitHub.

## Read only

Source: `internal/core/garden/ops/store.go`, `internal/core/garden/spec/spec.go`,
`internal/core/config/state.go`, `internal/storage/git/`,
`internal/gardenbackend/backend.go`, `internal/ui/gardenapi/gardenapi.go`.
Image: [private inbox](../design/png/inbox-120x35.png).

Entry: the existing Garden connection identifies a local repository and remote
target; atomic stores and cross-process locks are available.

## Specification

- Add a dedicated `internal/core/garden/drafts/` package (new), with a versioned
  schema and injected path/clock/ID seams. Store local Draft ID, repository scope,
  monotonic revision, description, editable spec/title/type, confirmed answers,
  timestamps and optional publication identity/state. Keep local IDs distinct from
  GitHub Issue IDs. Do not persist running-request state or terminal output.
- Authored draft content is intentional private storage, not a warm-start cache.
  Use a private config directory (0700 where supported), files restricted to the
  current user, atomic replacement, bounded decoding and cross-process locks.
  Never store credentials, provider tokens or inherited environments.
- Expose list/read/save/archive with compare-and-swap revisions. A concurrent save
  returns a conflict preserving both versions; incomplete/corrupt reads never
  overwrite or clear known drafts. Preserve unknown newer schemas without rewriting.
- Define explicit unpublished/publishing/published identities so publication can
  retain an Issue link and resume. Published drafts leave the active local inbox
  only after verified success; unknown writes stay discoverable.
- Add a small optional backend interface for these operations. Keep current
  managed GitHub drafts intact; do not silently copy, delete or relabel them.

## Checks and exit

Run focused tests for the new drafts package, `internal/core/config` and
`internal/gardenbackend`, with race checks for shared-store behavior. Prove restart
recovery, concurrent saves, crash-safe writes, permissions, oversized/corrupt files
and repository isolation using temporary directories. No UI redesign, network calls
or automatic migration is part of this deliverable.
