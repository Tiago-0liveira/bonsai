# Garden canonical contract

## Identity and index

## Connection and setup (04.2)

The proposed package boundary is `internal/core/garden/connect` (proposed). It resolves remote targets through injected `RemoteLister` and `RepositoryResolver` ports; `internal/core/git` now provides `git.RemoteLister` (read-only `git remote -v` listing), which `internal/gardenbackend` adapts to `connect.RemoteLister`. `connect.OperationActive(store, local)` reports an in-process or cross-process running setup operation so readers can show it without taking the lock. Garden identity is constructed only from the numeric repository ID. Remotes resolving to the same numeric ID are one candidate and retain their remote names as aliases. Fork/upstream choices with different IDs remain separate candidates. A candidate choice matches by numeric repository ID or exact full name, case-insensitively; partial names never match. `RepositoryByIDResolver` resolves through the caller-provided owner/name endpoint and verifies that its numeric ID matches the expected ID; a mismatch returns `ErrRepositoryIdentityChanged`.

Remote URLs are sanitized before they reach the resolver: userinfo, query strings, and fragments are removed, including credentials in scp-style remotes. Raw resolver errors and remote URLs never enter returned errors, plans, candidates, or persisted records.

GitHub identity and coding-agent account identity are distinct types; only the GitHub identity label is persisted. Access is None, Read, Write, or Admin with Project scope tracked separately. Read permits observation and never setup. A repository fetched successfully with a missing, null or all-false `permissions` object maps to Read (never Write). `Connection.Setup()` requires Write/Admin; Project scope is checked for each Project step, so labels-only plans work without it. A Project step without Project scope fails with `ErrScopeMissing`; other steps still complete and the operation remains resumable. Adapter failures are reduced to safe error codes/messages; credentials and raw responses are not retained, and this package never refreshes tokens.

Setup modes are Create, ConnectExisting and Skip. Skip is a durable, reversible association decision. `ConnectExisting` resolves the explicitly selected Project by ID or number through the project read port and verifies both owner and ID; missing/inaccessible Projects and owner mismatch are typed failures. Existing fields are reused by name and only missing desired fields are ensured. Unrelated fields are untouched. A same-name Project is never auto-adopted absent verified ownership; it produces typed `ConflictProject`, blocks Project plan steps, and directs the user to select the Project explicitly with ConnectExisting if it is theirs. Field steps carry the selected Project ID. A Create operation may adopt only when its local operation record shows that this plan attempted creation, the random nonce in that record matches the remote operation marker (the nonce is not derived from repository/GardenID/operation ID), and `ProjectV2.creator.login` matches the authenticated viewer login case-insensitively. Unknown creator/login fails closed. GitHub exposes only a mutable login on the Actor interface; matching login is a residual spoof/adoption risk if the login changes or is reused. The local-only random nonce plus attempted-create record remains the strong operation binding. Plans are pure previews of labels and Project fields. Before each Project create/field write, Apply and Resume re-observe the owner's Projects; before label creation they re-observe labels. A newly appeared compatible label is reused; a conflicting label or unverified same-name Project is surfaced as a conflict and never overwritten. This read-before-write narrows but cannot eliminate the observe/create race. Duplicates from a lost race are surfaced and never silently deleted. Plans assert `ImportCount == 0`; they cannot enroll Issues, rename/delete labels, or edit Issue bodies. Unsupported browser-only finishing is represented by an explicit manual step.

Associations and operations are local-repository scoped, revisioned JSON under Bonsai config, written under the cross-process lock and atomic replacement pattern. Store reads are capped at `connect.MaxStoreBytes` before parsing, require valid UTF-8, reject duplicate keys, unknown fields, trailing data, and unsupported schema versions. Unsupported versions return `ErrUnsupportedSchema`; malformed or oversized stores return `ErrCorruptStore`. Decode errors never rewrite the source file. Idempotency records preserve the original result for Put and Remove replay and reject payload changes. Every remote action has a persisted operation step and sanitized error code. Resume validates the stored plan and hash, requires caller `LocalRepoID` and live access, verifies repository ownership and persisted access, then re-observes remote labels, fields, and Projects. It persists reconciled statuses before any remote write. A foreign or missing operation returns a typed not-found/foreign error without exposing its contents. The nonce used in a Project marker is random and persisted only in the local create step; adoption additionally requires the matching creator and viewer logins case-insensitively. Concurrent operations for one LocalRepoID fail with `ErrOperationInProgress`; different local clones have separate locks. The config package change adds `GardenAssociationsPath()` to place this store beside project-root configuration; it does not change project-root behavior or data.

A canonical work item is one GitHub Issue, identified by its numeric GitHub repository ID and Issue `id` (and `node_id` when supplied). The API confirms these numeric IDs, but does not promise stability across transfers/renames; Garden treats repository ID as identity and fails with `ErrRepositoryIdentityChanged` when an owner/name lookup resolves to a different ID. Stability of Issue IDs and `node_id` across rename/transfer remains an explicit assumption. Title, branch, local path, owner spelling, and repository name are mutable display data, never identity. `GardenID` derives only from the remote repository ID. `LocalRepoID` is the separate path-derived `config.ProjectID`; clones and worktrees share a GardenID while retaining independent local execution identity and locks.

The canonical labels are `bonsai:managed`, exactly one `bonsai:type/feature|bug|chore|research`, and exactly one `bonsai:stage/draft|ready|in-progress|review|blocked|done`. Unknown `bonsai:` labels, missing managed marker, or non-singleton type/stage sets are per-item problems. Issues carrying only a Ready label are not accepted work.

The contract uses exact lowercase canonical label spellings. GitHub label-name case-insensitivity is unverified, so adapters recheck the exact managed marker on every listed Issue and do not use server-side label filtering as proof of management.

## Metadata, revisions, and approval

Issue metadata is a JSON envelope with `schema_version: 1`, provenance, owner, acceptance/spec fingerprint, current plan hash, and a bounded `revocations` projection. Each revocation projection entry contains the revoked approval comment ID and revocation comment ID; IDs must be positive and unique and the list is capped at 2,000 entries. Provenance and owner are required. Decoders reject unknown fields for known schemas; unknown future versions return an unsupported-schema problem. Malformed or oversized metadata is reported for that item and never causes the Garden to be erased.

The GitHub adapter reads that envelope only from the first body line `<!-- bonsai:garden-metadata/v1 {json} -->`; the rest of the Issue body is preserved as display content and is never written by this adapter.

Specs and plans use byte-exact normalization: CRLF and CR become LF, ASCII spaces and tabs at each line end are removed, then leading and trailing LF bytes are removed. No Unicode whitespace is trimmed and no Unicode normalization is performed, so NFC and NFD content hashes differ. Content is SHA-256 addressed.

Revision, approval, and revocation records are append-only GitHub comments. The first line is exactly `<!-- bonsai:garden/v1 {json} -->`; optional human-readable text follows after LF. JSON is bounded by `MaxMetadataBytes`, valid UTF-8, rejects duplicate and unknown fields, and requires `schema_version: 1`; unknown versions are unsupported. Every envelope includes `schema_version`, `kind`, numeric `repo_id`, and numeric `issue_id`. Revision fields are `revision_kind`, `content_hash`, `parents` (hash list), and `source_spec_fingerprint` (required for plans). The normalized comment text after the marker must hash to `content_hash`. Approval fields are `revision_hash`, a required positive `revision_comment_id`, and `source_spec_fingerprint`. Verification accepts an approval only when that exact earlier comment ID is a valid revision envelope for the same issue and its content hash equals `revision_hash`; the ID distinguishes identical-content revisions. Revocation fields are `target_comment_id`. Verification binds every envelope to the requested repo and issue and derives state only from the ordered server comment log, including author IDs and timestamps when supplied. Verification accepts an explicit `Complete` flag; callers must pass false when pagination or retrieval was incomplete. It caps logs at 2,000 comments (`ErrTooManyComments`) and comment bodies at `MaxMetadataBytes + 1 MiB`; overlong bodies are reported per comment.

Approval policy allowlists numeric GitHub user IDs; GitHub documents their numeric type but not stability, so stability is an explicit assumption. Login names are display labels only. Approval and revocation records with a missing author ID are rejected with a per-comment problem. Self-approval is allowed by default; policy may explicitly set `ForbidSelfApproval` to reject it. An approval is valid only when its referenced revision comment occurs earlier in verified order (created time, then server comment ID). Verification sorts a private copy of the log by that order, so input slice order has no effect; a revocation encountered before its target approval is ignored because that target has not yet appeared. Byte-identical duplicate comment IDs collapse to one record; conflicting records with one ID produce a per-comment problem and none is trusted. Heads are evaluated independently for spec and plan revisions: forks are competing heads, while a linear older plan revision does not remove an otherwise accepted specification from Work. On writing a revocation, Bonsai records both approval and revocation comment IDs in metadata. Verify checks every projected revocation; a missing or altered expected revocation reports a problem and keeps its approval invalid. Revocation and supersession invalidate approval; they are explicit records and never mutate prior revision content. Plan approval is valid only while its source-spec fingerprint equals the current spec fingerprint. A changed body/revision head, missing referenced comment, altered comment, unexpected label, or competing head is external-edit/conflict state requiring attention. Lane treats any verification problem in the accepted revision/approval/revocation chain, any incomplete log, and an accepted item without a valid spec head as Attention; malformed unrelated comments without a Garden marker are ignored. Once a spec was accepted, a changed spec head without valid approval is Attention regardless of stage; ideas that have never been accepted remain Draft. A user with Issue write access can edit both comments and the body projection; Garden guarantees integrity against non-privileged commenters and detects inconsistent or accidental edits, not malicious edits by such a writer. Plan 09 must re-verify at launch time and require fresh approval whenever state is inconsistent.

## Lanes and concurrency

## Read model and sync (04.3)

`internal/core/garden/sync` is the proposed read-model boundary. It receives only
Issue and Project observation capabilities; it has no Issue or setup writer
capability and never launches work. A snapshot is scoped by GardenID,
LocalRepoID, generation, and association epoch. The caller supplies a monotonic
generation and current epoch. Stores accept only matching Garden identity, LocalRepoID
and epoch, and only strictly newer generations; a foreign Garden
(`ErrForeignGarden`) or foreign LocalRepoID (`ErrForeignLocal`) is a typed error
and late generations are counted and dropped. Reassociation increments the epoch
and resets the accepted generation to zero, so callers restart at generation 1
in the new epoch; results still carrying the prior epoch are dropped, so in-flight
reads from the prior association cannot land. Only a result that is both Complete
and Source live replaces data. A result marked Complete with any other Source is
contradictory: the store treats it as incomplete, records an `inconsistent-result`
problem, and never replaces last-good data or marks deletions. The store deep-copies
every slice reachable from an Item (Issue labels, metadata and adapter problems,
classification problems, and all Verification revisions, parents, heads, approvals
and problems) on Apply and on Read, so callers never alias stored state.

Reads have caller-selected page, item, and per-call timeout bounds; a zero or
negative bound is rejected as `ErrInvalidLimits` before any port call
(`Request.ValidateLimits`; `Load` reports an `invalid-limits` Problem). An invalid
cursor restarts from the first page once; a second failure is visible. A cap,
cancellation, or adapter failure yields an incomplete snapshot. A page holding
more unseen Issues than the remaining item cap emits an `item-cap` problem and
is incomplete even without a next cursor (nothing is marked Deleted). Managed
issues come from the managed index; explicitly imported issue IDs are fetched
individually by caller policy, in ascending ID order, and imports skipped at the
item cap emit `item-cap` and make the read incomplete. Every listed or imported
Issue must carry the Garden's repository ID, and an imported Issue must carry the
requested ID; a violation is a per-Issue `foreign-repo` or `identity-mismatch`
problem, the Issue never enters the snapshot, and the read is incomplete. Items are
de-duplicated by the returned Issue ID. Comment reads stop at the first cancelled
context (the remaining items stay visible, with a `cancelled` problem and an
incomplete verification). Only a complete live snapshot replaces last-good
data, including an empty snapshot. Failed or incomplete reads retain last-good
items as stale and expose a safe reason; offline backoff is a hint, never a
timer or retry loop. Malformed metadata, labels, and comment verification are
item problems and place that item in Attention. An adapter-reported data defect
(`Issue.Problems`, currently `invalid-author` for an Issue whose author has no
usable numeric ID, such as a deleted account) is an Attention item problem and
does not make the snapshot incomplete. An Issue without a stable ID
(ID 0) stays in the snapshot as an Attention item with an `invalid-id` Problem
("issue without stable ID"), is never Work, and does not make the snapshot
incomplete.

Read failures are reduced to distinct safe problem codes: `auth-required`
(401), `permission-denied` (repository-level denial), `forbidden` (a 403 not
proven to be a rate limit), `rate-limited` (429, or 403 with Retry-After or an
exhausted x-ratelimit-remaining), `not-found`, `offline` (network failure and 5xx
only) and `read-failed` (everything else). `auth-required` and
`permission-denied` yield AccessRevoked while retaining last-good data;
`forbidden` yields only a `degraded` (uncertain) finding and is never
AccessRevoked. Rate limits carry a bounded `RetryAfter` value (default one
minute when absent, capped at one hour) on the problem, the `rate-limited`
finding, the Snapshot and the connection status (state `rate-limited`); like
`BackoffHint` it is a value for callers, never a timer. Connection status is
`offline` only when the failure was a network failure. Findings are typed, safe
presentation records. Stage conflicts and projection
problems require attention; revoked or superseded approvals are stale. When an Issue
disappears from a complete read, its prior item remains marked Deleted for one
accepted snapshot cycle, then is removed. Clone-local coordination keys derive
from LocalRepoID, so clones sharing a Garden do not share local locks or state.
Project projection: the adapter does not populate `Project.IssueFields` in this
phase, so sync reports the informational `projection-unavailable` finding for every
Garden; Project-projection drift detection is exercised against fakes only until a
later plan writes the Project projection. Connection status is a pure projection of the persisted association, its
operation progress, and the latest snapshot; it does not create a second setup
flow.

Unvalidated ideas remain in Drafts (`draft` stage). A verified specification acceptance moves that same Issue to Work; it does not create another item. Once accepted, a stale plan leaves the Issue in Work and marks its approval stale. A Ready label without verified acceptance is Attention, never Work. Invalid labels or metadata also require Attention.

Lane classification consumes decoded metadata and a `Verification` result. Missing or inconsistent metadata is Attention. Work requires a valid, non-revoked approval for the current spec head and matching accepted-spec hash; competing heads and projection drift are Attention. A stale plan does not remove an otherwise valid accepted specification from Work.

Metadata acceptance requires valid lowercase 64-character SHA-256 `accepted_spec_hash` and `spec_fingerprint`; unaccepted metadata cannot carry an accepted hash. Any present plan hash must have the same format.

A read ETag is not atomic update protection, and read-before-write is not a guarantee. If an adapter cannot establish a documented conditional write, it appends a revision rather than replacing the user's Issue body and exposes competing heads. The 04.C2 adapter verifies documented read behavior against primary sources below; unsupported conditional writes remain append-only, and unverified payload/identity assumptions remain explicit.

## Proposed package paths

These are proposed additions: `internal/core/garden` for domain and narrow ports, `internal/core/garden/fake` for in-memory contract adapters, and `internal/git/github/ghcli/garden` (proposed) for the GitHub-specific adapter. Project fields/views are projections, not a second writable task database. The PR-oriented `internal/git/github.GitHubService` remains unchanged.

## GitHub adapter boundary (04.C2, proposed)

The real adapter path is **proposed** as `internal/git/github/ghcli/garden`. It consumes an injected HTTP client whose production transport is the existing `ghcli` client; the adapter does not modify the PR-oriented `GitHubService`. Constructors (`New`, `FromService`) require a non-nil client and return an error instead of falling back to `http.DefaultClient`. Only a bare `https` origin is supported (for example `https://api.github.com`): GitHub Enterprise base paths such as `/api/v3` fail closed with `ErrUnsupportedBase` in this phase. The reader and writer values hold the adapter privately, so a reader cannot reach setup or write methods. Its reader and writer capabilities are separate interfaces from `internal/core/garden`. Project and repository-label setup methods only observe/create; issue writes append comments and add/remove named managed labels through their dedicated endpoints. Issue body and title are never sent by a mutation.

### GitHub behavior: verified (URL + date) | unverified (why)

All verification below is from the primary GitHub docs fetched 2026-10-19.

| Verified behavior | Primary source |
|---|---|
| Issue listing returns pull requests (identified by `pull_request`), caps `per_page` at 100, and uses `Link` with `rel="next"`. | [REST issues: list repository issues](https://docs.github.com/en/rest/issues/issues#list-repository-issues), [REST pagination](https://docs.github.com/en/rest/using-the-rest-api/using-pagination-in-the-rest-api) (2026-10-19) |
| Comment Simple User `id` is numeric int64; stability is not stated. | [REST issue comments](https://docs.github.com/en/rest/issues/comments) (2026-10-19) |
| Conditional requests do not support writes (POST/PUT/PATCH/DELETE); there is no documented If-Match concurrency protection. Authorized conditional GET returning 304 does not count against primary rate limits. This supports append-only revisions. | [Conditional requests](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api#use-conditional-requests) (2026-10-19) |
| Repository lookup is `GET /repos/{owner}/{repo}`; `GET /repositories/{id}` is not listed. Repository response includes `permissions`, `fork`, and `parent`. Issue `id` differs from `number`; REST issue routes use `number`. | [REST repositories: get a repository](https://docs.github.com/en/rest/repos/repos#get-a-repository), [REST issues](https://docs.github.com/en/rest/issues/issues#list-repository-issues) (2026-10-19) |
| Project V2 create accepts owner/title/repository/team IDs; update supports short description/readme; fields support `SINGLE_SELECT`, which requires at least one option. `creator` is Actor; Actor exposes login but no numeric ID. `databaseId` is deprecated in favor of `fullDatabaseId`. | [GraphQL Projects](https://docs.github.com/en/graphql/reference/projects), [GraphQL Users/Actor](https://docs.github.com/en/graphql/reference/users) (2026-10-19) |
| `read:project` grants read and `project` read/write; fine-grained PATs cannot access user-owned Projects. GitHub App Project creation using `repositoryId` also requires Contents permission. `X-OAuth-Scopes` is sent for classic OAuth/PAT tokens. | [OAuth scopes](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/scopes-for-oauth-apps) (2026-10-19) |
| HTTP 410 on an Issue or repository means it is gone or transferred; the adapter maps it to the not-found error, not `ErrOffline` (offline is for network failures and 5xx). | Adapter decision; 410 handling is not separately confirmed in the reviewed docs. |
| Secondary limits use 403 or 429 and `retry-after`; primary exhaustion uses `x-ratelimit-remaining: 0`. The adapter therefore treats only 429, or 403 with `Retry-After` or `x-ratelimit-remaining: 0`, as rate limiting (hint from `Retry-After` seconds or date, else `x-ratelimit-reset`). Any other 403 is `ErrForbidden`: uncertain, never proof of revoked access. | [REST rate limits](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api#exceeding-the-rate-limit) (2026-10-19) |

| Unverified behavior (fail closed or retain as an explicit assumption) | Why / adapter response |
|---|---|
| Maximum comment body length | Not stated in the docs reviewed. Adapter imposes a conservative 64 KiB cap and maps 422 to a typed validation error. |
| Whether the issue `labels` filter is AND or OR; label case-insensitivity; duplicate-label create status | Not established by the reviewed sources. The adapter rechecks managed labels client-side; a 422 create is not treated as success, so setup can re-observe and resume. |
| `X-OAuth-Scopes` absence for fine-grained/App tokens | Header is documented for classic tokens. Absence alone means unknown; adapter does not invent `ErrScopeMissing` from it and lets the Project call decide. |
| GraphQL `errors[].type` strings `INSUFFICIENT_SCOPES`, `RATE_LIMITED`, `NOT_FOUND`, `FORBIDDEN` | UNVERIFIED: none of these strings was confirmed from primary docs (no network access during this review). Exact-string mapping: `INSUFFICIENT_SCOPES` -> `ErrScopeMissing` (even when the scopes header is absent, fails closed); `RATE_LIMITED` -> rate-limited error; `NOT_FOUND` -> `ErrNotFound`; `FORBIDDEN` -> `ErrForbidden`. Any other or missing type, and `data: null`, becomes the generic `ErrGraphQL`, never `ErrOffline`. The most specific mapping wins when entries disagree (scope, rate limit, forbidden, not found, generic). |
| GraphQL `repositoryOwner(login:)` with `... on ProjectV2Owner { projectsV2(first, after) }`, and `login` on the owner | UNVERIFIED: no network access to GitHub docs while implementing; field and interface names are from memory of the schema. Fails closed: a null owner is `ErrNotFound`, a differing login is `ErrGraphQL`, and an owner type without `projectsV2` yields an empty list that the existing same-name/adoption checks treat as not found rather than as adopted. Verify against https://docs.github.com/en/graphql/reference/interfaces#projectv2owner before release. |
| GraphQL connection `pageInfo.hasNextPage` / `endCursor` semantics | UNVERIFIED against primary docs. The adapter pages Projects up to 10 pages of 100, and returns `ErrTruncated` (never a partial list) when more remain, when `hasNextPage` has no cursor, or when any Project reports more than 100 fields. |
| Stability of comment author IDs, repository/Issue IDs and `node_id` across rename/transfer | Numeric type/identity fields are documented, stability is not. Repository lookups verify the expected numeric ID and return `ErrRepositoryIdentityChanged` on mismatch. |
| Whether an ETag always changes after every edit | The rate-limit behavior of 304 is verified; ETag invalidation on edits is only implied, not explicit in the reviewed source. |
| Creator login mutability/reuse for Project adoption | Actor exposes login but the docs do not make login immutable. Adoption also requires the local attempted-create record and unguessable local-only nonce; creator unavailable fails closed. |

Conditional-request caching holds only through the shared `ghcli` transport: this adapter sends no `If-None-Match` itself. Where that transport serves a 304 from its cache the adapter sees an ordinary 200; a bare 304 reaching the adapter fails closed. It never treats a read ETag as write concurrency protection.

### Adapter read behavior

- Pull requests: a row with a `pull_request` key that is not the literal JSON `null` is a pull request. Listing skips PRs; the Issue-number lookup never caches a PR; `GetIssue` returns not-found for a PR, and `UpdateLabels` (which reads through `GetIssue`) and `AppendComment` can therefore not act on one. `"pull_request": null` is treated as absent.
- Identity: `GetIssue` rejects a response whose ID or number differs from the request (not-found) and evicts the cached number.
- Pagination: a `Link` header is parsed as RFC 8288 (commas and semicolons inside `<...>` or quoted values do not split entries); malformed headers and more than one `rel="next"` fail closed; the next page must be exactly the current page plus one on the same origin and path. A comment log over 2,000 comments (`MaxComments`) returns `ErrTooManyComments` once 2,001 comments have been read (at most 2,001 are kept).
- Authors: an Issue row whose author has no positive numeric ID (a deleted `ghost` account) is emitted with the `invalid-author` Issue problem and an empty author ID, and does not fail the page; a row without a positive Issue ID or number is emitted with ID 0 (the `invalid-id` Attention item). A comment author without a positive ID has an empty author ID, which verification rejects for approvals and revocations.
- Status mapping: 401 -> auth-required (matches both `connect.ErrAuthRequired` and `garden.ErrAuthRequired`); 404 and 410 -> `ErrNotFound`; rate limits as above (the error is a `garden.RateLimitError` carrying `RetryAfter` and matches both `connect.ErrRateLimited` and `garden.ErrRateLimited`); other 403 and 405 -> `ErrForbidden`; 422 -> validation; everything else (network failure, 5xx, unexpected 3xx) -> `ErrOffline`. A 404, 410 or 403 when re-fetching a known repository to verify it (a previously readable Garden) is `ErrPermissionDenied`, the only adapter source of AccessRevoked besides 401.
- Repository cache: a verified owner/name is trusted for one minute (injected clock) and then re-fetched, where a different numeric ID returns `ErrRepositoryIdentityChanged`. `Seed(repoID, owner, name)` records a persisted name for warm start without verification; the first use verifies it. Seed never replaces an existing observation. Owner/name segments of `.` or `..` are rejected.
- Projects: `ListProjects` does not populate `IssueFields`.
- Projects owner: `ListProjects(ctx, repoID)` keeps its port signature. The adapter resolves the verified repository from `repoID` (cached, re-verified by numeric ID) and lists the Projects of that repository's owner (user or organization) via `repositoryOwner(login:)`, not the viewer's own Projects. Every listed Project (and a newly created one) carries `Project.Owner` = the owner login; `creator.login` stays the adoption check. The owner never comes from caller input. A null owner returns `ErrNotFound`; a response login that differs (case-insensitively) from the requested owner fails closed with `ErrGraphQL`. Create, Resume and adoption therefore observe the same owner Create writes under, and `gardenbackend` passes the repository owner as `Desired.Owner` for ConnectExisting so connect's owner plus ID verification is real; `ProjectsView.Owner` is the repository owner.
- `Adapter.Viewer` (`GET /user`) returns the numeric ID (stable identity) and login (mutable label used for creator/adoption checks), not the owner.


## Board presentation (05)

`internal/core/garden/work` is the UI-free approved-work selector shared by Board
and the forthcoming Table. It recomputes canonical Work acceptance and excludes
unusable Issue identities, deleted items and all Draft-stage items. Accepted
display diagnostics remain visible as conflict feedback. Query, grouping and
identity selection stay independent of the TUI. Types never imply
stages. `Issue.Number` and `Issue.Title` are display data from the GitHub reader;
`Issue.ID` remains the selection key. Both display fields affect the item revision.

The six canonical stage labels cannot encode all nine approved-design columns.
The conservative read-only projection is: accepted `ready` without a plan →
Approved, accepted `ready` with a verified plan head → Plan Review,
`in-progress` → Running, `review` → Human Review, `done` → Done, `blocked` →
Blocked. Planning, Ready and Reviewing remain empty until later plans supply
active-job evidence and exact-plan approval verification. The current verifier
accepts specification approvals only; plan approval records are not silently
trusted by Board. No additional labels or canonical mutations are introduced.

Optional priority filters observe existing `priority:p0`–`priority:p3` labels,
case-insensitively; missing/conflicting values are unknown. These labels are not
part of the owned setup scheme. Only the presentation name Board/Setup is saved
in the existing per-repository UI state; issue data and queries remain in memory.
`Snapshot.SuccessfulAt` retains the last complete-live sync time independently
of `FetchedAt` (latest attempt). The backend's bounded Work read uses the same
store, generation, scope and epoch as setup/status, with no writer capability.
