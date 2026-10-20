# Garden

Garden is the repository's approved-work board in the opt-in terminal preview:

```sh
bonsai --tui-layout redesign
```

Use Enter or Down from the Garden workspace tab to focus its body. The legacy
TUI remains the default. See the [TUI guide](tui-cli.md#redesign-preview---tui-layout-redesign)
for workspace navigation, themes, glyph preferences and key overrides.

## Connection and setup

Garden uses the stored **GitHub** identity, separately from coding-agent profiles.
Choose Create Garden, Connect existing Project, or Skip for this repository.
Ambiguous remotes require an explicit repository choice. Preview lists the exact
label and Project changes before confirmation; Cancel has initial focus. Setup
imports **zero** issues and preserves unrelated labels, fields and issue bodies.
Read-only GitHub access can open the board without offering setup writes.

Interrupted setup records completed steps. Resume re-observes GitHub rather than
rolling back. Skip is reversible for this clone. Settings → Garden shows the same
connection and synchronization state. See [setup and recovery](tui-cli.md#garden-setup-in-the-preview)
for Project permissions, the Bonsai Type field and recovery commands.

## Board

Only managed issues with a verified, current specification acceptance appear.
Drafts, missing/unverified acceptance and invalid canonical data are excluded,
including issues someone merely relabeled Ready. A valid but stale plan does not
remove an accepted specification. An empty board is normal before work has been
accepted; setup never creates demonstration tasks.

Type (`feature`, `bug`, `chore`, `research`) is independent of workflow state.
Cards show the GitHub issue number, title, type, optional priority and approval or
conflict feedback. Display diagnostics stay visible when canonical acceptance
remains valid; they cannot promote unverified work. Issue identity uses the stable remote issue ID, so title edits
and reordered results preserve selection.

The nine columns are Approved, Planning, Plan Review, Ready, Running, Reviewing,
Human Review, Done and Blocked. At 120 and 160 columns all nine are visible. At
80 columns, a four-column window follows the selected column; its range is shown
below the board. Each column keeps its own cursor and scroll, including empty
columns. The footer gives the selected issue's identity and board state; open
the document to read a title too long for the footer.

The six canonical stage labels remain unchanged. Accepted `ready` work without a
plan appears as Approved; a verified plan head appears as Plan Review; an exact
verified approved plan/spec pair appears as **Ready**. A later head change removes
Ready and shows the stale gate while retaining the accepted work and old approved
snapshot. `in-progress` maps to Running, `review` to Human Review, and `done`/`blocked`
to their matching columns. Planning and Reviewing await active-job evidence.
These presentation columns never change GitHub labels. Launch remains unavailable.

| Key in Board | Action |
| --- | --- |
| `←` / `→` or `h` / `l` | Select previous / next column, including empty columns |
| `↑` / `↓` or `k` / `j` | Select previous / next card |
| `pgup` / `pgdown` | Move by a page of cards in the current column |
| `enter` | Open the selected issue document |
| `/` | Search issue number or title within approved work |
| `t` | Cycle Any, feature, bug, chore, research |
| `p` | Cycle Any, P0, P1, P2, P3, unknown priority |
| `x` | Clear all filters |
| `R` (refresh binding) | Refresh the collection |
| `esc` | Return to setup when available, otherwise workspace navigation |
| `v` (`garden_view` binding) | Switch Board/Table, keeping the selected issue |
| `tab` (focus-next binding) | Cycle Setup (until connected), the work view and Drafts |
| `ctrl+k` / `?` | Command palette / help |

Filters combine and apply to the same collection. An active filter labels the
count as filtered and shows the total eligible count. Priority is optional display
data from existing `priority:p0` through `priority:p3` labels (case-insensitive),
not a new managed label or a setup change. Missing or conflicting priority is `?`.
Search captures typed shortcuts; Enter applies it and Escape restores the previous
query and selection. The palette includes Garden: Board, Garden: Table, Search
approved work, and Refresh.

Refresh keeps the selected stable ID even if its row or column changes. On the
Board, if an issue is deleted or filtered out, selection falls back to the nearest row of the
same column, then the nearest populated column (right wins a distance tie).
If no match remains, the current column stays focused and explicitly empty.
New attention items never take keyboard focus.

## Table

The Table is a second view of the same approved work, not a second store: Board
and Table share one collection, query, selected issue, notices, freshness and
issue document; switching views issues no extra read. `v` (the Garden-scoped
`garden_view` binding; it cannot be rebound to a key Garden itself reads, such as
`/`, `t`, `p`, `x`, `o`, `h`/`j`/`k`/`l` or the page keys) toggles Board/Table and keeps the exact selected issue
even though ordering differs; filters stay. The `v` key only applies inside
Garden: Worktrees keeps `v` for Processes, and overriding `garden_view` in
`keys:` changes both the key and every on-screen hint.

Rows show issue number, title, type, state (full names, never clipped), priority
and approval/conflict status. Columns drop by available width, not by clipping
the title or state: 80 columns show ISSUE, TITLE, TYPE and STATE; 120 adds a
selected-item inspector (type, state, approval, next human gate) in place of TYPE
and keeps P; 160 shows every column. The selected row always carries a marker.

| Key in Table | Action |
| --- | --- |
| `↑` / `↓` or `k` / `j` | Select previous / next row |
| `pgup` / `pgdown`, `home` / `end` | Move by a page / to the first or last row |
| `o` | Cycle sort: State, Number, Priority (view order only, never a GitHub change) |
| `enter` | Open the same issue document as Board |
| `/`, `t`, `p`, `x`, `R`, `esc`, `tab` | As on Board |

Rows are ordered by state then issue number by default. If the selected issue
disappears, the row now in its place is selected (the previous row if it was the
last), with a notice saying which; if nothing remains, no row is selected. Escape from the
document returns to the same row and scroll position in the Table.

## Read-only issue document

Enter replaces the board with the document at every terminal size. The breadcrumb
includes the issue number, and the visible Board parent action uses Escape or
Backspace. Escape returns to the same card and board scroll. Left/Right switches
Overview, Specification, Questions, Plans and Execution; Up/Down, Page Up/Down,
Home and End scroll the current section. Wide layouts add a section rail.

Overview reads the canonical issue body. Specification and Plans read verified
comment revisions, with comment IDs, hashes and source-spec fingerprints. Questions
reads a `Questions` or `Open questions` Markdown section from the issue body;
absence is stated explicitly. Execution reports canonical stage and the absence
of a local execution projection. Specification/plan approval and question
authoring are unavailable until the planning steps of plan 08; local launch is unavailable until plan 09. Specifications are
approved from Drafts, not from this document ([gate one](#validate-a-specification-gate-one)). Press `p` in an opened
Issue for its [read-only planner](#request-an-ai-plan-read-only-planner), a separate explicit local action.
Selecting, opening, searching and refreshing never mutate workflow or launch work.

## Freshness and local memory

The footer shows the last **successful** sync time. Failed or incomplete refreshes
retain the last good board with stale/offline, access or rate-limit feedback.
Only a successful complete read can clear the collection. Before any successful
read, a failure says that approved work is unknown rather than calling it empty.
Per-item conflict and stale-approval indicators remain separate from selection.

The last Garden subview (Board, Table or Setup) is remembered per local repository in
Bonsai's existing atomic UI state store. No query text, issue content, credentials,
commands or terminal output are persisted for this feature. Filters and issue
selection survive view navigation in the current process; they are not saved to
disk. Another Bonsai process has the existing UI-state last-writer limitation.

For the canonical schema and verification boundaries, see the
[Garden contract](design/tui-redesign/garden-contract.md).

## Drafts

Drafts is a separate, amber inbox for ideas that have not been validated. Reach it
with `tab` from the Board or Table; `v` or `esc` returns to the Board. It lists
every managed Issue without verified specification acceptance, of any type, with
the type written as a word. An unvalidated Issue relabelled `ready` or another
stage stays in Drafts as an attention row. Drafts never appear on the Board or Table, and a type or a `ready` label
never makes an idea approved. An item whose revision heads compete, whose labels
or metadata are inconsistent, or whose specification is accepted but still
labelled draft stays in the inbox as `! attention` with the reason, so a
concurrent edit never makes it vanish.

| Key in Drafts | Action |
| --- | --- |
| `↑` / `↓` or `k` / `j`, `pgup` / `pgdown`, `home` / `end` | Select a draft (kept by Issue ID across refreshes) |
| `enter` | Open the draft document: Issue body, current specification and its approval gate. In it, `e` edits the specification and `v` validates it ([gate one](#validate-a-specification-gate-one)) |
| `/`, `t`, `x` | Search number or title, cycle the type filter, clear filters |
| `n` | Capture a new idea |
| `r` | Reconcile a capture whose outcome is unknown |
| `R` (refresh binding) | Refresh |
| `v` / `esc` | Back to the Board |

In the draft document, Up/Down, Page Up/Down, Home and End scroll within the
document's bounds. Resizing or refreshing a shorter document keeps the stored
position within those bounds, so scrolling back works immediately.

### Capture

`n` asks for a title, an optional description and a type, then shows a preview:
the repository, the exact `bonsai:` labels and "Nothing is approved, planned or
started". Creating always makes a real repository Issue at Draft, never a Project
card, and needs Write access (read-only users see the reason instead of the
action). While GitHub answers, keys are ignored and the call is never cancelled.

The Issue body starts with the metadata line and a second marker line holding a
random nonce, then your description. Bonsai records the operation in
`garden-operations.json` (beside `garden-associations.json`) before it calls
GitHub. If GitHub does not confirm the creation (timeout, offline, 5xx), the
capture is **unknown**: it may exist. Retry is refused; **Reconcile** looks for
the nonce among all repository Issues, including closed Issues and those whose
managed label was removed, and either finds the Issue or, after a short
settle time and a complete read that finds nothing, allows a retry of the same
operation. A restart shows `N capture may already exist` and `r` reconciles it.
Create and Reconcile hold the same operation lock across Bonsai processes until
the remote call and local outcome are recorded. An incomplete reconcile never
permits a retry. If the local operations store cannot be read, the inbox retains
known unresolved captures and blocks new capture until a successful refresh.
Rejections (validation, permission, rate limit) are definite: nothing was
created and Retry is safe. The text you typed survives errors, resizing and
leaving the workspace until you create or explicitly discard it (`esc` on a
non-empty form asks first). It is kept in memory only.

### Validate a specification (gate one)

Open a draft (`enter`) and press `v` to validate its specification. The preview
reads and verifies GitHub, writes nothing and names exactly what will happen: the
specification revision (`s1`, `s2`, ... oldest first), its fingerprint, the
approver, the transition **Draft → Approved** and the writes in order. Back has the
first focus, so Enter alone never validates. The primary button records the approval
of that exact revision:

1. one approval comment (the versioned `bonsai:garden/v1` record: the account GitHub
   authenticates as the author, the revision hash and comment ID, the source
   fingerprint, the server timestamp and an explicit statement that it approves
   the specification only);
2. the stage label `bonsai:stage/draft` → `bonsai:stage/ready`.

Acceptance is derived from the append-only approval. Existing Issue bodies are never
rewritten: GitHub has no conditional body update. Stage projection compares managed
labels before writing, changes only the stage and verifies afterwards; concurrent
changes require attention.

The same Issue then leaves Drafts and appears in the Board and Table as **Approved**
(a different column from **Planning**), with its Type unchanged. Validation starts
no planner, agent, worktree or pull request: a planner is a separate, explicit local
action ([Request AI plan](#request-an-ai-plan-read-only-planner)).

Validation is unavailable, with the reasons listed, when:

- the Issue has no specification revision yet (edit and save one first), or its
  Summary, Requirements or Acceptance criteria are empty;
- the Open questions section still lists an unresolved question (checked `[x]` items,
  `None` and `n/a` are resolved);
- revision heads compete, the comment log is incomplete, a record fails
  verification (edited or deleted comment, approval by an account that is not
  yours), the metadata line is invalid, the `bonsai:` labels conflict, or the Issue
  is already past Ready (running, in review, blocked or done).

Bonsai never corrects labels, metadata or records on GitHub for you; it shows the
attention state and keeps downstream actions unavailable. **Approval is recognized
only from verified records.** A line in the Issue body such as "approved", a `ready`
label or `accepted` metadata alone is shown as *approval unverified* (an attention
row in Drafts) and is never Work. Revoked, deleted, edited or unauthorized approval records never authorize downstream
actions. A specification edited after acceptance stays visible in Work with a stale
gate: revalidate its current revision deliberately before accepting a plan for it. Superseded and revoked approval
records are retained and listed in the draft document with their verified state.

If the head moves between preview and confirmation nothing is approved (the Issue is
shown again, read afresh). If GitHub does not confirm the approval comment, or the
label write fails afterwards, the screen says so and the Issue stays a
draft: *Validate again* finds the recorded approval by the log and finishes the
remaining writes, and never records a second approval. An approval comment posted
from GitHub or a phone by the same signed-in account is recognized the same way; the
next Validate only finishes applying it to the Issue.

### Request an AI plan (read-only planner)

Open an Approved Issue on the Board or Table and press `p`. This only reads: it
verifies gate one and inspects the checkout, and starts nothing. Looking at an Issue,
refreshing it, or editing it on GitHub never starts or reruns a planner; only the
**Request AI plan** button does, and only locally.

The button is offered only for a **verified Approved** specification: the verified approval
must name the current head revision, and its stage projection must be complete. A draft, a
stale or revoked approval, an unverified claim (a `ready` label or `accepted`
metadata without a verified record) or any attention state shows why instead, and the
request is refused again by the backend if the specification moved after the preview.

The screen shows what the attempt will be bound to: the specification revision and
fingerprint (s1, s2, ...), the repository base (commit and branch) and whether the
checkout has uncommitted changes. The planner reads the checkout it was started from;
**no implementation worktree is created.** Uncommitted changes are read only after
you press `x` to include them (the decision belongs to that exact set of changes: a
changed checkout asks again), and the attempt records that they were accepted, with a
count and a digest but never paths or contents.

Request AI plan opens the account choice, the same Accounts, model and effort form as
a new agent session without any permission, sandbox, approval or full-access field:
the role fixes the permissions. Each account shows what its provider enforces, and
choosing it and pressing the primary action (*Accept read-only and request plan*,
`ctrl+s`) is what accepts that permission. A provider that cannot enforce read-only
access below the model (Codex and Antigravity today) is listed with the reason and cannot be
chosen; nothing falls back to unrestricted access. Which providers qualify, and the
evidence, is in [docs/agents.md](agents.md#the-read-only-planner-role).

The planner then runs as an ordinary holder-backed agent session, shown in the same
terminal controller as the Agents tab: it is focused at once so you can answer its
questions, `ctrl+]` unfocuses it (while it is focused every other key, including `q`,
`esc` and `?`, belongs to the agent), `enter` focuses it again, `c` then `y` cancels
the attempt and `esc` returns to the attempts without stopping the session. Closing
Bonsai leaves it running, like any agent. The planner is read-only, so it gives the
plan as text in its conversation; [publishing it](#publish-and-review-a-plan) is a
separate step you take.

Every attempt is kept in a local history (attempt ID, specification revision and
fingerprint, repository base, provider and profile, accepted permissions, and
whether it is starting, running, ended, failed or cancelled). A failed or cancelled
attempt and its retained session stay listed; **Retry is another explicit Request AI
plan**, never an automatic rerun, and only one attempt per Issue runs at a time. An
attempt for an older specification stays visible and is marked as such. The history
holds no prompt, token, environment value or terminal output; the session's own
retained screen is viewed through the same session record. Attempts follow what the
agent session reports: a late or repeated report never moves an attempt backwards,
and a finished attempt never changes.

### Publish and review a plan

A plan becomes part of the Issue only when you publish it, and nothing is published
for you. On the planner screen select the attempt the plan came from and press `u`:
a text box opens, you type or paste the plan the planner gave (Bonsai reads nothing
from the planner's terminal, and the text is kept only in memory until you publish
it or Bonsai closes), and `ctrl+s` previews exactly what would be published. The plan
needs the sections **Assumptions, Touched areas, Steps, Tests, Risks and Acceptance
mapping**; Bonsai adds the **Source specification** (revision and fingerprint) and
the **Inspected base** (the commit the attempt read) from the attempt record, so a
pasted section cannot claim another base. Publishing appends one comment to the
Issue, needs Write access, and refuses when the specification changed since the
attempt or another plan revision landed since the preview.

A plan revision is content addressed: the comment carries the hash of its normalized
text, the source specification fingerprint and the revisions it supersedes, as plan 04's
versioned protocol does for specifications. Every read verifies the schema, the hash
and the authenticated GitHub author again; editing a published comment afterwards
makes it an unverifiable record (the Issue shows an attention state and the plan
screens stay unavailable) instead of silently changing a snapshot. Superseded
revisions stay in the history. A revision written by an account other than the signed-in
one is listed but never reviewable.

In an opened Issue, `r` (or `r` on the planner screen) opens the review. It shows three
different revisions and never merges them: **Current** (the single revision under
review for the current specification), **Latest** (the newest published, which may be
by someone else or for an older specification) and **Approved** (the last verified approved snapshot, retained even when stale). The plan is a document: `←`/`→` jump between sections,
`↑`/`↓`/`pgup`/`pgdn` scroll, `/` searches the text (`n`/`N` step through matches) and
`[`/`]` choose another revision. `d` compares the revision in view with the one before
it, unified by default; `w` switches to two columns when the terminal is at least 100
columns wide (80 columns keep the unified comparison).

`c` requests changes on the revision under review and needs a reason; `x` rejects it
(the reason is optional) and returns the Issue to Approved with no accepted plan. Both
append one decision comment naming that exact revision hash and specification
fingerprint, never "the latest", keep every revision in the history and start nothing:
the next read-only run is another explicit **Request AI plan** with its own local
confirmation, even when a comment on GitHub asked for changes. A comment written on
GitHub or a phone without a Bonsai record only appears as a hint ("newer on GitHub"); it
neither starts a run nor decides anything. The next explicit local planning attempt
includes the verified request-changes reason as sanitized review context.

A source fingerprint must match the normalized content hash of the verified
specification revision named in the plan. Supplied fingerprints cannot redefine
that identity or carry an old approval onto a different specification.

### Approve the exact plan (gate two)

In plan review (`r` in an opened Issue), press `a` to **Approve plan**. The confirmation
names the full plan revision hash, current source spec fingerprint, spec head and
GitHub actor. Cancel has initial focus; `tab` then `enter` approves the named pair.
Bonsai rereads both heads and rechecks the authenticated account and current Write
access immediately before appending the decision. A moved head or changed account
refuses the confirmation; refresh and review again. The decision is read back and
verified before reporting success. Approval writes no stage label or plan pointer.

**Ready** requires both gates and one exact current verified plan/spec pair. Approval
starts no agent, implementation worktree or PR. **Launch is unavailable** in this
preview (plan 09). The actions have separate keys and confirmations: `v` validates a
specification, `p` requests a local read-only plan, and `a` approves the exact plan.
In plan review `v` also reuses the gate-one flow to revalidate changed accepted work;
Escape returns to the review. A new plan or spec revision makes approval stale,
retains the old approved snapshot and disables downstream actions. A changed spec
must pass gate one again, and a plan for the current spec must pass gate two.

The review shows each accepted decision's GitHub author, revision, declared source
(local or external), server-recorded time and the time Bonsai verified it. `o` opens
the canonical Issue in the local browser; `y` copies its link. Opening, copying,
refreshing or reading a remote decision never starts a local run.

A versioned `revocation` record may target a gate-one approval or a gate-two
decision comment ID. It must follow the target in server order, carry a valid
record hash and come from an authorized account. A revoked plan decision stays in
history but grants no approval; an earlier superseded decision is not restored.
Missing or altered revocations listed in the metadata's expected-revocation
projection block Ready. An unrecorded deletion cannot be detected from a fresh
GitHub log alone.

### External approval protocol (GitHub or mobile browser)

Use the same GitHub account that Bonsai is signed into, with current repository
Write access. The default approval policy trusts that account's numeric user ID,
not its mutable login or a name written in the comment. Other accounts, labels and
prose claims cannot approve. No phone client or remote execution is provided.

Post this exact comment format on the canonical Issue, replacing the placeholders
with the numeric `repo_id`, numeric `issue_id`, plan `content_hash`, plan comment's
numeric server ID and `source_spec_fingerprint` from its versioned revision record.
Keep the first line on one line and the human text exactly `Approved.`. The fixed
`record_hash` below is the SHA-256 of that normalized human text (no final newline).
Do not include the Markdown fences in the posted comment.

```text
<!-- bonsai:garden/v1 {"schema_version":1,"kind":"decision","repo_id":REPO_ID,"issue_id":ISSUE_ID,"revision_hash":"PLAN_HASH","revision_comment_id":PLAN_COMMENT_ID,"source_spec_fingerprint":"SPEC_FINGERPRINT","record_hash":"7e0019a3769b3e37d8a83d04a70720b100041b63e8000b03eafc85071a7a67bf","decision":"approved","source":"external"} -->
Approved.
```

The plan must be the single current head and its source must have verified gate-one
approval **before** this decision is posted. A later validation cannot authorize an
earlier plan decision. Bonsai verifies the authenticated comment author, permissions,
schema, hashes, comment reference and exact source match on refresh. A record that
fails verification, a `ready` label alone or "I approve" prose cannot yield Ready.
`source` may be omitted (external) or be `local` for Bonsai-written records; it is
declared provenance, never evidence of identity or authority.

For external **specification** approval, use the same format with `kind:"approval"`,
the spec revision hash/comment ID and its fingerprint, and omit `decision` and
`source`. The human text and fixed `record_hash` stay as above. Then use the local
Validate flow to finish stage projection before gate two.
The exact schema and normalization rules are in the
[Garden contract](design/tui-redesign/garden-contract.md#gate-two-and-external-approval-plan-084).

GitHub comments are append-only by Bonsai convention, not an atomic transaction.
Concurrent changes after the final read can still happen; Bonsai checks the result
again and reports an unverified outcome instead of Ready. It never silently retargets
an approval, overwrites a body or starts a run in response.
