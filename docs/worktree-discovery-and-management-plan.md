# Repository sync, unlinked worktree stacks, and canvas management

This is an implementation plan. No application behavior has been changed.

## Proposed behavior

After discovering a repository, publish its worktrees promptly, read each worktree's status, and derive a server-owned group for unlinked worktrees. Refresh remote refs and GitHub PRs asynchronously. Send each project's PR catalog to the web client on connection; show PRs in the bottom workspace and GitHub page, and link each worktree to its matching PR. Add straightforward create and delete flows.

Two assumptions remain provisional:

- “Stacked list” means a visual Local / unlinked worktrees group, rather than inferred commit dependencies between branches.
- Automatic sync includes fetch and a guarded fast-forward pull of the main working copy. If fetch-only behavior is preferred, keep pull as an explicit action; the rest of this plan is unchanged.

Fetching updates remote-tracking refs; pulling additionally integrates changes into a checked-out branch. Branch discovery needs fetch. See the [Git fetch](https://git-scm.com/docs/git-fetch) and [Git pull](https://git-scm.com/docs/git-pull) documentation.

## Existing implementation to reuse

- `internal/git/local/service.go` already lists local branches, remote-tracking branches, and linked worktrees, including worktrees outside the scanned folder. It preserves individual status errors.
- `internal/git/local/worktrees.go` supports existing, new, and origin branches. Removal protects the main working copy and requires explicit discard for dirty worktrees; it leaves branches intact.
- `internal/server/localapi/state_sync.go` publishes canonical snapshots with epoch and sequence ordering, asynchronous enrichment, coalesced jobs, and watcher reconciliation.
- `internal/server/localapi/enrichment.go` already requests PRs with `state=all`. The provider client paginates, but currently errors beyond 100 pages of 100 items.
- `web/src/features/workspace/CreateWorktreeDialog.tsx` already creates real worktrees through the API despite the store action's `createMockWorktree` name. The canvas currently stacks by tag and provides no worktree delete action.
- `docs/canvas-layout-restructure-plan.md` defines the existing placement rules: preserve manual coordinates, use local placement, and avoid automatic viewport movement.

## 1. Define canonical classification and branch inventory

Extend the Go domain and browser snapshot with enough information to distinguish:

| Worktree state | Presentation |
| --- | --- |
| Attached local branch with an upstream ref that exists | Existing branch node/tag grouping |
| Attached branch with no configured upstream | Local / unlinked group, “No upstream” badge |
| Attached branch whose configured upstream is missing | Local / unlinked group, “Upstream missing” badge |
| Detached HEAD | Local / unlinked group, “Detached HEAD” and short SHA |
| Unborn branch/no resolvable local branch ref | Local / unlinked group with an explicit reason |
| Status unavailable or still loading | Unknown state; retain the last known classification if available |

The main working copy remains the project/default-branch presentation and is excluded from this group. A missing PR alone does not make a worktree unlinked. A failed fetch or unavailable GitHub authentication does not imply a deleted remote branch.

Keep the complete worktree list and stable IDs. Add a derived group such as `{id, kind, worktree_ids}` and a per-worktree connection state/reason; do not overwrite user tags or merge targets. Derive it after local status collection, with the same helper used by watcher and full-refresh paths.

Build server-owned branch candidates using full ref identity, local branch identity, and upstream relationships. Each candidate should include its remote, last commit time/SHA, associated PR summaries, existing worktree IDs, and creation mode or unavailable reason. Do not hide or deduplicate branches solely by their short names. Exclude symbolic refs such as `origin/HEAD`.

Keep provider-observed branches distinct from fetched Git refs. The current browser manufactures `origin/...` entries from GitHub branch names; move that reconciliation to the server and verify the repository's actual remote identity. Provider-only candidates require a successful fetch before creation.

Primary files: `internal/git/types.go`, `internal/git/local/service.go`, `internal/server/localapi/snapshot.go`, `internal/server/localapi/state_sync.go`.

## 2. Add asynchronous repository synchronization

Use a separate sync job rather than making every snapshot read or five-second local-status poll perform network Git operations.

1. Publish cached/local worktree inventory and statuses immediately.
2. Queue remote sync on the first active project load, an explicit Sync action, and a bounded periodic schedule while the project is active. Proposed default: every five minutes.
3. Fetch origin refs with pruning through the daemon Git bridge. Ensure narrow clone refspecs do not silently omit available origin branches; use an explicit branch refspec when needed without rewriting the user's configuration.
4. Under the provisional pull policy, run `git pull --ff-only` on the main working copy only when it is clean, attached, has a valid upstream, and has no Git operation in progress. Report dirty, detached, no-upstream, and diverged skips in sync state. Never automatically stash, reset, merge, or rebase.
5. Reread local branches and worktree statuses after fetch/pull, then publish the revised canonical snapshot and grouping.
6. Refresh GitHub repository/PR data independently, reusing the provider cache and existing fork-aware association.

Serialize Git mutations per repository, coalesce concurrent sync requests, use bounded timeouts/backoff, and retain previous successful values on network failures. Skip Git network operations for repositories without a remote. Multiple tabs should share one sync job; separate clones retain their own fetch state.

The current pull implementation uses `--no-rebase --no-edit`, which can merge. Add a structured fast-forward-only policy to the domain/bridge arguments rather than reusing that behavior for automatic sync. Keep HTTP GETs side-effect free; make the UI's initial sync request an authenticated POST and use the existing mutation journal/idempotency model.

Record fetch, pull, and provider outcomes separately, including successful completion timestamps and skipped/error reasons. Update `docs/git-backend.md`, which currently specifies that automatic fetching never occurs.

Primary files: `internal/git/local/operations.go`, `internal/daemon/gitbridge/commands.go`, `internal/daemon/gitbridge/protocol.go`, `internal/server/localapi/git.go`, `internal/server/localapi/state_sync.go`.

## 3. Make PR coverage and timestamps reliable

Reuse the existing all-state PR listing, including draft, open, merged, and closed PRs. Test multi-page enumeration and replace the fixed 100-page ceiling with resumable pagination so large repositories can finish across bounded jobs. Expose completion/loading state and retain the previous complete catalog until the replacement is ready. Avoid repeatedly rereading an entire large history on every refresh; use provider update ordering and periodic complete reconciliation.

Fetch failure and provider failure should be independent: usable fetched branches remain available if GitHub is unavailable. Match PRs by repository identity plus head branch; a fork PR must not appear as an origin branch. Show fork-only or deleted-head PRs with an explicit unavailable creation reason until a dedicated PR checkout flow is supported.

Distinguish three timestamps:

- Branch row: “Last commit,” using the branch tip's commit timestamp, obtained in the existing batched ref listing.
- PR row/badge: “PR updated,” from GitHub's `updated_at`.
- Sync status: “Last synced,” from the last completed fetch; show provider freshness separately when relevant.

Use relative labels and exact timestamps on hover. Do not describe a commit date as the time a remote ref moved.

Both backend semantic snapshot comparison and frontend snapshot application currently ignore timestamp-only changes. Publish meaningful sync completion changes without turning every local-status poll into an event. Apply freshness-only store updates without incrementing the canvas topology revision.

Primary files: `internal/git/github/app/client.go`, `internal/git/github/app/pull_requests.go`, `internal/server/localapi/enrichment.go`, `web/src/api/git.ts`, `web/src/types/index.ts`.

## 4. Show PRs in existing views and add automatic stacks

Fetch each project's PR catalog when the web client connects and publish it through project snapshots. Store PRs by project on the client. Show the active project's PRs beside Files in the bottom workspace and on the GitHub page. Use the provider's repository and branch association to attach a PR badge to the correct worktree node; PR numbers alone are not unique across projects.

Render the server's unlinked group as a stable stack, collapsed by default when it contains multiple worktrees. Each row remains selectable and shows its reason, dirty count, PR badge when applicable, and management menu. Preserve expand, detach, and never-stack preferences without altering tags. Unlinked grouping takes precedence over tag grouping so a worktree appears once.

Use stable project/group IDs. Membership changes, sync timestamps, PR updates, creation, and deletion must preserve unrelated node positions and the viewport. Reuse existing stack transition/local placement code. A one-member group can render as an ordinary worktree card with the connection badge.

Primary files: `web/src/features/workspace/canvas/BonsaiCanvas.tsx`, `web/src/features/workspace/nodes/BonsaiNode.tsx`, `web/src/features/terminal/BottomWorkspace.tsx`, `web/src/features/github/PullRequestsPage.tsx`, `web/src/stores/bonsai.ts`.

## 5. Finish create and delete workflows

Creation:

- Clicking Create on a branch prefills the existing dialog with the project, source ref, and correct mode. Preserve that selection during live snapshot updates.
- If the corresponding local branch already exists, attach that branch instead of attempting `--track -b` with a duplicate name. If it is already checked out, offer Open worktree.
- Keep New branch available with local and fetched remote bases. Show pending state and inline errors; prevent duplicate submits.
- Capture the project ID when the action starts so switching projects cannot redirect the request or selection.
- Distinguish successful Git creation from a later metadata-write failure, so retrying metadata never creates a second worktree. Rename `createMockWorktree` to reflect its real behavior.
- Select the new worktree after canonical state includes it, then place it locally without automatic Fit.

Deletion:

- Add Delete worktree to worktree and stack-row menus and the inspector. Show branch and path in a confirmation dialog.
- Use the existing `DELETE /api/worktrees/{id}` route and idempotency handling. Default to retaining the branch and remote PR.
- Recheck status server-side. Require an explicit discard choice for dirty content; protect the main working copy and active Git operations. If tracked processes/agents are running, require stopping them before removal, with a clearly presented Stop and delete action only where real lifecycle support exists.
- After confirmed removal, reconcile metadata, selection, dock/runtime references, and placements through canonical state. Keep unrelated placements intact. On failure, retain the node and show the actionable error.

Primary files: `web/src/features/workspace/CreateWorktreeDialog.tsx`, `web/src/api/git.ts`, `web/src/stores/bonsai.ts`, `web/src/features/inspector/Inspector.tsx`, `internal/git/local/worktrees.go`; add a delete dialog.

## Delivery and validation

Implement in this order:

1. Domain fields, connection classification, branch candidates, and local Git tests.
2. Async fetch/guarded pull, sync freshness, PR pagination, and canonical snapshot integration.
3. Browser snapshot mapping, automatic unlinked grouping, and branch/PR panel.
4. Prefilled creation, safe deletion, metadata/lifecycle reconciliation, and UI state handling.
5. Integration coverage, canvas stability checks, and documentation updates.

Tests must cover no remote, missing upstream, detached/unborn HEAD, unreadable worktrees, branch-name collisions, narrow fetch refspecs, branches already checked out, deleted remote branches, multi-page PRs, fork PRs, dirty/diverged pull skips, concurrent tabs, project switching, repeated mutation requests, and freshness-only updates. Use temporary local bare remotes for Git tests; avoid live network dependencies.

Add browser tests for branch click → prefilled create → canonical new node, delete → confirmation → canonical removal, dirty deletion refusal/discard, and automatic stack membership. Existing manual placement and viewport stability tests must remain passing.

Run `go test ./...`, `go test -race ./...`, `go vet ./...`, `pnpm --dir web typecheck`, `pnpm --dir web test`, `pnpm --dir web build`, and the relevant Playwright suites.

Acceptance: remote branches without worktrees are discoverable after sync; timestamps advance even when branch content does not change; unlinked worktrees form one understandable stack; users can create and remove worktrees directly; failed network operations preserve useful local state; normal updates never rearrange unrelated canvas nodes.
