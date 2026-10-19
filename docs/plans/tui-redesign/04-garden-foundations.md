# Plan 04 — Garden identity, contracts and connection

Status: not started. Prerequisite: [03 — Accounts](03-accounts.md).
Next: [05 — Garden board](05-garden-board.md).

## Deliverable

A real, opt-in per-repository Garden connection and canonical Issue service,
ready to feed Board, Table and Drafts. This is backend/connection groundwork,
not permission to build all three views in this plan.

References: [Garden design rules](../../design/tui-redesign/README.md),
[original context brief](../../design/tui-redesign/original-design-brief.md),
[offline state](../../design/tui-redesign/revision-02/png/e-02-120x35-161.png).
Setup interaction details from the original brief still apply, except that the
approved five-workspace shell supersedes its earlier three-tab sketch.

## 04.1 — Settle and test the canonical representation

Write a short Garden contract document alongside the implementation before
wiring the views. It must specify these decisions rather than leave later phases
to invent incompatible formats:

- One GitHub Issue is the canonical work item. Use stable GitHub repository and
  Issue identities, not title, branch name, local path or mutable owner spelling.
- Keep the local repository/execution ID distinct from the remote Garden ID.
  Clones/worktrees resolve to the same remote Garden; local ownership remains
  anchored to the actual local repository and registered roots.
- Define versioned managed metadata, type and exactly one lifecycle stage, with
  explicit ownership/provenance. A managed marker and namespaced type/stage labels
  are the proposed canonical index; Project fields/views are projections, not an
  independently writable task database. Confirm the final label names in this
  phase and reuse them everywhere.
- A supported specification/plan revision is content-addressed and stored with
  the canonical Issue. Prefer append-only, versioned revision/approval comments
  with server IDs, content hashes and authenticated authors; any body summary is
  a projection. Comments can be edited/deleted in GitHub, so re-verify hashes and
  references instead of calling them intrinsically immutable.
- Define source-spec fingerprints, allowed approval actors, revocation,
  supersession, external edit detection and invalid-schema behavior. Details of
  the approval UI and execution are plans 08/09, but their envelope must exist now.
- Define an unambiguous draft/work classification: unvalidated ideas remain
  Drafts; a verified specification acceptance puts the same Issue into the work
  lane. A later stale plan does not silently send accepted work back to Drafts.
  Imported or externally relabeled issues cannot bypass acceptance merely by
  carrying a Ready label. Surface that as an attention/conflict state.
- List proposed new package paths explicitly as proposed, including a small
  `internal/core/garden` domain/service boundary. Add narrow Issue/Project adapter
  interfaces; do not grow the PR-oriented `GitHubService` until every consumer
  genuinely needs the methods.

Capability spikes use fakes first. Verify actual GitHub conditional-write,
pagination, permission and payload-limit behavior against primary documentation
when implementing. Do not assume a read ETag provides atomic update protection.
If required protection is unavailable, append a revision without replacing the
user's Issue body and resolve competing heads visibly. A read-before-write check
alone is not a guarantee against concurrent edits.

## 04.2 — Connection, setup and association

- Resolve GitHub remote/account through core adapters. Multiple remotes or fork
  ambiguity require a target choice. Account/profile for a coding agent is not
  the GitHub identity; label them independently.
- Offer Create Garden, Connect existing Project and Skip. Preview owned
  label/field/Project changes; default import count is **zero**.
- Reuse a compatible existing scheme; do not rename or delete unrelated labels,
  bulk-enroll the backlog, or overwrite existing Issue bodies.
- Persist the remote association via the existing locked/atomic store patterns.
  A partial apply has an operation ID and completed steps; Resume reconciles
  actual remote state instead of creating duplicate Projects or promising rollback.
- Separate read access from mutation/setup access. A readable connection can be
  read-only. Explain auth/scope failures without logging credentials or triggering
  provider token refresh. Browser-assisted Project view finishing is explicit if
  the verified API cannot do the entire operation.

## 04.3 — Read model, sync and ordering

- Load only managed or explicitly imported issues; implement bounded pagination
  and cancellation. Decode malformed metadata as a visible per-item problem,
  not a reason to erase the whole Garden.
- Carry repository identity, request generation, fetched time, completeness and
  per-item revision. A late response from another repository cannot land here.
- Preserve last good data on failure. Only a successful complete empty result
  clears a list; a partial page is never treated as the entire collection.
- Detect conflicting stages, projection drift, stale approvals, deleted Issues
  and revoked access. No reconciliation read writes back or launches a process.
- Add setup/connection status to Settings → Garden using the same service.
  Do not implement a second setup flow or a speculative remote execution toggle.

## Source seams and new code boundary

Inspect/reuse `internal/git/github/service.go`, `internal/git/github/ghcli/`,
`internal/core/config/project_roots.go`, `internal/core/git/git.go`,
`internal/storage/git/` and `main.go`. New Garden domain, adapter and UI-facing
contracts are additions to be designed here; no existing Issue backend is assumed.
The UI performs no direct `gh`, filesystem or raw Git operations.

## Exit criteria and verification

- [ ] Canonical schema, actor policy, revision integrity and conflict handling are
      documented and exercised by fake-service contract tests.
- [ ] Setup imports zero issues; connect/skip/read-only/partial-resume work.
- [ ] Two clones resolve the same Garden without sharing local execution locks.
- [ ] Pagination, offline reads, successful empty reads and late-reply ordering
      preserve correct data and scope.
- [ ] Settings connection information matches the same backend state.
- [ ] No Board/Table/Drafts authoring or process launch is smuggled into this phase.
- [ ] Targeted new/core/adapter tests, applicable race tests and full Go checks pass.
- [ ] Add `docs/garden.md` as the user guide and link it from `README.md`.

Completion record: pending implementation, schema decision and validation evidence.
