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

The default phases are Backlog, Planning, Ready, Building, Review and Done.
`d` (configurable as `garden_stages`) switches to the detailed stages: Approved,
Planning, Plan Review, Ready, Running, Reviewing, Human Review, Done and Blocked.
Approved maps to Backlog; Planning/Plan Review to Planning; Running to Building;
Reviewing/Human Review to Review. Cards keep their exact stage text.
Both modes remember their own lane cursor, scroll and window while sharing the
selected Issue and filters. The mode is session state. Below 100 columns, three
lanes fit; below 160, six phases or five stages; from 160, all lanes fit. Hidden
lanes are named in the footer, and empty lanes remain navigable.

A Blocked Issue stays in its proven phase: an implementation attempt proves
Building (Review when awaiting human review), an exact verified plan approval
proves Ready, a verified plan head or local planner proves Planning, and current
specification acceptance proves Backlog. A retained Issue with no such proof
appears in a final Unplaced lane. Cards state the reason; detailed mode retains
Blocked and names the last proven phase.

The six canonical stage labels remain unchanged. Accepted `ready` work without a
plan appears as Approved; a verified plan head appears as Plan Review; an exact
verified approved plan/spec pair appears as **Ready**. A later head change removes
Ready and shows the stale gate while retaining the accepted work and old approved
snapshot. `in-progress` maps to Running, `review` to Human Review, and `done`/`blocked`
to their matching stages. A live local planner or ready unpublished plan file
puts an otherwise Approved Issue in Planning, explicitly marked local planner.
Implementation evidence never changes a canonical stage; Reviewing remains empty
until verified reviewer evidence exists.
These presentation columns never change GitHub labels. Starting work is a separate explicit action ([local implementation launch](#start-implementation-locally)).

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
| `d` (`garden_stages` binding, Board only) | Toggle phases/detailed stages |
| `a` (`garden_attention` binding) | Toggle Needs you; return to the previous work layout |
| `tab` (focus-next binding) | Cycle Setup (until connected), the work view and Drafts |
| `ctrl+k` / `?` | Command palette / help |

Filters combine and apply to the same collection. An active filter labels the
count as filtered and shows the total eligible count. Priority is optional display
data from existing `priority:p0` through `priority:p3` labels (case-insensitive),
not a new managed label or a setup change. Missing or conflicting priority is `?`.
Search captures typed shortcuts; Enter applies it and Escape restores the previous
query and selection. The palette includes Garden: Board, Garden: Table, Garden: Needs you, Search
approved work, and Refresh.

Refresh keeps the selected stable ID even if its row or column changes. On the
Board, if an issue is deleted or filtered out, selection falls back to the nearest row of the
same column, then the nearest populated column (right wins a distance tie).
If no match remains, the current column stays focused and explicitly empty.
New attention items never take keyboard focus.

## Table

Table and Needs you present the same approved-work projection as Board. All three
share the query, selected issue, notices, freshness and issue overlay; switching
layouts performs no backend read. `v` (`garden_view`) cycles Board/Table; from
Needs you it goes to Table. `a` (`garden_attention`) toggles Needs you and returns
to the Board or Table you came from. These bindings are scoped to Garden and can
be changed in Settings → Keys → Garden. Garden remembers Board, Table or Needs
you per repository; grouping, collapse and filters last only for this session.

Rows show ISSUE, TITLE, exact STATE, SIGNAL and NEXT. At 80 columns SIGNAL is
omitted and NEXT is shortened. At 120 SIGNAL and longer actions fit; 160 also
adds TYPE and P. Local activity and approval warnings appear in SIGNAL. State
names remain complete; long titles and signals end with an ellipsis, and the
overlay shows full text. Every row has a phase rail; the selected row has a
thicker marker. When at least 18 body rows are available, a three-line bottom
strip shows the selected issue's signal, title and “Enter open · next: …”. On a
group header it shows the group and “Enter expand” or “Enter collapse” instead.
The Rows footer counts visible items and reports how many items collapsed groups
hide.

`o` cycles State/Number/Priority sorting. `g` toggles Group: None/Phase separately
in Table and Needs you. Groups follow Backlog through Done, then Unplaced;
blockers remain in their proven phase exactly once. Arrow/page/home/end keys
move through item rows and group headers. Space collapses or expands the current
group; Enter on a header does the same. Collapsing the selected issue's group
keeps its identity, focuses the header and explains that it is hidden. Expanding
that group returns to the issue. Enter or clicking an item opens the shared
overlay; Escape returns to the same line, grouping and scroll. Clicking a group
header toggles its collapse.

### Needs you

Needs you uses the current text/type/priority filters and sorted rows. Its count,
submenu and footer describe the same filtered set. The first matching request
wins, in this order:

- A blocker, with its proven phase reason or “no proven phase”.
- A live local implementation or planner explicitly waiting for input.
- Human review of a change, proven by stage or fresh implementation handoff.
- A published plan revision awaiting approval on currently accepted work.
- A finished local plan awaiting review/publication on accepted Approved/Planning work.
- Explicitly startable Ready work with current acceptance and no implementation attempt.

Idle, working, failed, exited and unknown activity alone are not requests. Stale
local evidence cannot establish an input request, handoff or plan publication.
The Table summary counts active fresh local runs, needs and blockers from the
same filtered collection; the summary chip leaves search and exclusion feedback
visible. Entering Needs you from an issue without a request selects the first
need and names both issues. Leaving restores the previous issue if that automatic
choice is unchanged and the previous issue still matches the collection. Moving
to another need keeps that explicit selection on return. A refresh that removes
the selected need chooses the item at its previous position and names the
replacement. Empty Needs you keeps
the shared selection for Board and says nothing needs you. Failed reads retain
last good rows and show the stale notice. Reading, filtering and grouping never
start work or change GitHub state.

### Local planner badges

Board cards show local planner and implementation evidence alongside workflow
state. Implementation uses this clone's newest non-cleaned stored attempt.
Preparing, starting, start-unknown, recoverable and running count as live or
unresolved, matching the launch state machine; this is not proof of agent
activity. The current launch session seam exposes no activity signal, so a live
implementation shows Activity unknown. Stored failure and exit states remain
distinct. When both summaries exist, the card gives implementation precedence.
Update times use an absolute date/time; a failed local read retains evidence and
marks it stale. Narrow cards truncate these lines to their available cells.
None of this evidence grants acceptance, Ready or completion.

Table SIGNAL cells use the same activity text as Board cards; Needs you uses the
specific request text. Approval warnings precede the signal. The footer also
shows local planner totals, for example `1 planner working · 1 plan ready` (without
the sync time below 120 columns). Bonsai reads this with the board data, again while the
Garden is in view (every 4 seconds while a planner is live or its screen is open, every 15
seconds otherwise) and after starting, cancelling or leaving a planner; a failed read keeps
the last good observations and only a successful empty read clears them.
Implementation history remains available under `i` on the opened Issue.

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

Enter, or a click on a card or row, opens the document as an opaque panel over the dimmed
Board or Table, at every terminal size. A fixed header names the issue, its state and its
approval; the pinned bar below the body holds the actions. Escape, Backspace or the
**Back** control close it and return to the same card, row and scroll in the layout it
came from. While it is open the panel owns every key and click: quit, help, the palette,
workspace and view keys wait until it is closed, and clicks outside it do nothing.
Left/Right (or a click) switches Overview, Specification, Questions, Plan and Activity;
Up/Down, Page Up/Down, Home, End and the wheel scroll the current tab. Tab and Shift+Tab
move through the enabled actions and Enter runs the focused one, exactly like its
shortcut or a click. Wide layouts add a summary rail. If a refresh removes the issue, or
it belongs to another repository, the panel closes with a notice on the board instead of
showing another issue.

Overview reads the canonical issue body. Specification and Plans read verified
comment revisions, with comment IDs, hashes and source-spec fingerprints. Questions
reads a `Questions` or `Open questions` Markdown section from the issue body;
absence is stated explicitly. Execution reports canonical stage and the absence
of a local execution projection. Draft question authoring is available through
[AI draft assistance](#elaborate-a-draft-with-ai); local launch is explicit ([guide](#start-implementation-locally)). Specifications are
approved from Drafts, not from this document ([gate one](#validate-a-specification-gate-one)). Press `p` in an opened
Issue for its [read-only planner](#request-an-ai-plan-read-only-planner), a separate explicit local action. An Issue with
[local planner work](#local-planner-badges) adds a **Local planner** line to Overview (attempt state, agent status,
plan file state, when it was last updated, and `p` to open it).
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
| `n` | Start a new local draft ([Local drafts](#local-drafts)); without the private store, capture a new idea as a GitHub Issue |
| `a` / `u` / `A` | Archive the selected local draft, undo (restore) it, show or hide archived local drafts |
| `r` | Reconcile a capture whose outcome is unknown |
| `R` (refresh binding) | Refresh |
| `v` / `esc` | Back to the Board |

In the draft document, Up/Down, Page Up/Down, Home and End scroll within the
document's bounds. Resizing or refreshing a shorter document keeps the stored
position within those bounds, so scrolling back works immediately.

### Local drafts

Local drafts are private to this machine and need no GitHub Issue, assistant or
provider. Press `n` in Drafts and write what is on your mind. The first text
creates the draft (shown as `D-1A2B3C`, with the full ID only in the inspector);
**Continue writing** opens the same draft's specification: Problem, Outcome,
Requirements, Criteria and Questions, with Summary, Constraints and any other
section you already have under *More*. `Ctrl+T` edits the title and `Ctrl+Y`
cycles the type. **Discuss with AI…** and **Invite assistant…** are optional: see
[Discuss a local draft with an assistant](#discuss-a-local-draft-with-an-assistant).
Without an agent account the editor says so and everything else works unchanged.

Text is saved by itself shortly after you stop typing (`Ctrl+S` saves now). The
status reads `Saving…`, `✓ Saved locally`, `Unsaved changes`, `Save failed — text
kept` or `Conflict — choose a version`, and never says saved while edits are
unwritten. If another Bonsai window changed the draft, your text is kept as a
recovery draft (`recovery of D-…` in the inbox) and you choose `k` to keep yours
or `o` to open theirs. Quitting with unwritten edits asks first. Bonsai remembers
which draft and section were open (never the text) and reopens them from the
inbox after a restart. Archive is reversible (`a`, then `u`).

`Ctrl+Enter` (or `Ctrl+R`) opens **Review & create issue** as a read-only
preview with the concrete reasons a draft is not ready. Creating the GitHub issue
is not available yet, so the confirmation is disabled and the draft stays saved
locally.

### Discuss a local draft with an assistant

An assistant is an invitation, never a step. Focus **Invite assistant…** (or press
`Ctrl+G`; on a compact screen it is the last tab) and choose an account that supports
tool-free structured calls, a model and an effort, plus how many questions a round may
ask (at most three) and optional guidance. The dialog lists exactly what would be sent:
the title and type, the description, the specification text, your confirmed answers,
your guidance and the repository name. No repository files are read and nothing is
sent until **Start discussion**; **Keep writing myself** (`Esc`) leaves the draft
exactly as it was. Bonsai never substitutes another account or model.

The discussion sits beside the specification (a tab on compact screens), so you can keep
writing while a request runs. It shows what the assistant understood and one focused
question at a time, with suggested choices, *Write my own answer*, notes, *I don't know*
and *Skip*; `w` or **Why?** shows why a question matters. `PgUp`/`PgDn` move between the
questions, `Tab` moves between the options, the text and the buttons, `Esc` steps back
and `Ctrl+G` returns to the text. While a request runs, `Esc` in the discussion cancels it.

**Apply answers** asks for a proposal. A proposal is drawn apart from your draft and
changes nothing until **Apply**. Each section is taken or left with `Enter`; a section
you edited after the request began is kept as yours and is replaced only if you choose
it, and suggestions nobody asked for join only when you confirm them. Applied sections
are marked `●`. Applying also records the answers you gave as confirmed answers, saved
with the draft and restored after a restart; the questions and proposals themselves are
never saved. A question you already answered or settled is not asked again.

A skipped or *I don't know* answer decides nothing: when you apply, its question is
written into the Questions section as an open item (unless a line already asks it), so it
blocks approval in the saved draft and after a restart, whatever you did with the
proposal's own Questions text. While a round is showing, its scope and acceptance
questions also count as open decisions in the readiness row; questions you never applied
are not part of the draft. Only an advisory question
(constraint or context) can be settled without an answer: **Defer** or **Assume** (of
your chosen suggestion or your own words) records a decision with the draft and writes
its line (`Deferred: …` or `Assumed: … (…)`, always one line) into Questions. That exact line then counts
as deferred (`✓ Questions resolved · 1 deferred`); a hand-typed or edited line does not.
A failed or cancelled request keeps all your text and offers **Retry** (`R`); a reply
that arrives after you cancelled, left the draft or asked again is ignored. Review &
create issue is still a read-only preview.

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

### Elaborate a draft with AI

AI assistance is optional and currently supports **Claude Code only**. It needs a
connected Garden with Write access and a signed-in, supported agent account. See
[tool-free structured calls](agents.md#tool-free-structured-calls) for provider
isolation and why Codex and Antigravity are unavailable.

For a **new idea**, press `i` on the Drafts list and describe the problem or desired
outcome in your own words; no title is needed yet. Choose the session options,
ask questions (`ctrl+s`), answer them, then request a draft (`ctrl+s` from Questions).
You can also draft directly from Setup with `ctrl+d`. Review the proposal, press
`e` to apply your choices and open the specification editor, edit as needed, then
use its normal `ctrl+s` save. The capture form prefills an editable suggested title
and type and your original description. One **Create and save** confirmation covers
two writes: create the Draft Issue, then append its first specification revision.
An unknown capture outcome uses [Reconcile](#capture); a changed remote stops at
the ordinary save preview or conflict screens. The Issue stays Draft.

For an **existing draft**, press `i` in its detail document or on the specification
editor's section rail (`esc` leaves the text area for the rail). The assistant
uses the editor's current specification, including unsaved edits. Applying a
proposal changes that editor in memory; `ctrl+s` then uses the ordinary save
preview and conflict handling. The existing Issue's title and type are unchanged.

| Screen / focus | Keys |
| --- | --- |
| Setup | `tab` / `shift+tab` cycle description (new ideas), Options and buttons; `enter` / `o` on Options opens them; `ctrl+s` asks questions; `ctrl+d` drafts without questions; `esc` returns, keeping the session |
| Session options | `tab` / `shift+tab` move between fields; `↑` / `↓` or `k` / `j` select an account; `enter` moves to model/effort; `←` / `→` cycle their values, or `enter` opens a list (`↑` / `↓` select, `enter` / `tab` commit, `esc` cancels); on Questions, `←` / `→` (also `h` / `l`, `-` / `+`) change the count; type Guidance; `esc` outside a list closes options, keeping choices; `ctrl+s` asks, `ctrl+d` drafts |
| Questions rail | `↑` / `↓` or `k` / `j` select a question; `enter`, `→` or `l` open its answer panel; `m` asks more questions; `ctrl+s` drafts; `esc` returns, keeping the session |
| Answer panel | Three zones, rail, options and text: `tab` / `shift+tab` cycle them; `pgdown` / `pgup` select the next / previous question from any zone; `ctrl+up` / `ctrl+down` scroll a long question; `ctrl+s` drafts; `esc` steps back one zone |
| Answer panel options | `↑` / `↓` move; `space` or `enter` select; `1`–`9` select that row. Selecting moves focus to the text field, except for Skip |
| Answer panel text | Type freely. Digits are text, and `q` never quits |
| Review | `↑` / `↓` select a section; `a` accepts its proposal; `k` keeps your text; `A` accepts eligible sections; `e` applies choices and opens the editor; `r` opens feedback (`enter` applies choices and requests refinement, `esc` cancels feedback); `m` applies choices and asks more questions; `o` applies choices and opens a local answer round; `esc` returns to Questions or Setup without applying choices |
| Setup outside description / Questions rail | `p` reopens the last proposal, when one exists |
| Setup outside description / Questions rail / Review outside feedback | `R` retries a failed request with the same inputs and a new request ID |

Text fields own printable keys, including `q`; `ctrl+k` and `ctrl+u` keep their
textarea editing behavior. The options are the suggestions, then **Write my own
answer**, **I don't know** and **Skip for now**. A suggestion is a choice and may carry
optional notes; Write my own is your own answer. Switching between them keeps the
typed text, so nothing is lost. A question without suggestions opens on the text field.
Question markers in the rail show a choice `●` (`●+` with notes), your own answer `✎`,
I don't know `?`, skipped `-` and unanswered `·` (ASCII `o`, `o+`, `w`, `?`, `-`, `.`).
Other rounds collapse
to headers, but question navigation still reaches every round. The assistant may
ask fewer questions or none; a session allows at most 30 questions.

Review shows a collapsed unified comparison per section: `=` means current and
proposed text match, `~` a proposed change, and `!` a section you edited since the
request. Unedited, changed sections default to accepting the proposal. Sections
edited meanwhile, or proposals that would empty a written section, default to
keeping your text. `A` skips both of those cases; only an explicit `a` accepts
them. Choosing `a`, `k` or `A` records decisions; `e`, submitting `r` feedback or
`m` or `o` applies them to the in-memory editor. Refinement and further questions send
that working draft and all earlier answers. Nothing is saved until the normal save.

Review lists draft suggestions in a `? Suggestions N` row. `↑` / `↓` move through
them and `space` confirms or unconfirms one. Only confirmed suggestions are saved, as
normal items in their section; unconfirmed ones are discarded when decisions apply.
Unconfirmed suggestions never block [gate one](#validate-a-specification-gate-one).

Skipped, “I don't know” and unanswered questions are carried into **Open questions**
and keep gate one blocked. The draft may also add at most 3 new open questions; extras
are dropped. When it adds any, review shows how many and `o` opens an answer round for
the unresolved open questions (at most 10, built locally, with no AI call). Answer them
there, then request a draft; or resolve them deliberately in the editor. These rounds
open with the text field focused. An answer to a repeated open question supersedes
its earlier skipped or unknown state; once the draft resolves that question, later
refinement keeps it resolved. Extra
specification sections are preserved, and assistant notes are shown without being saved.

Before the first request, Setup names the chosen provider/account and discloses
that your description, answers and draft go to it. Each request includes the
repository name, existing Issue number/title/type when applicable,
your new-idea description, working specification (including Extra sections),
questions and answer states/text, prompt guidance and refinement feedback.
Existing Issue bodies are not separately sent. **No repository files are read**;
the provider runs without tools in an empty temporary working directory. Fixed
instructions and the response schema are separate from your text, which travels
as JSON on stdin.

Assistant sessions live **only in memory**: navigation, resize and ordinary
refresh keep them; saving or discarding removes them; quitting Bonsai loses them.
A changed Garden connection invalidates the old session. While a request runs,
`esc` on Setup, the Questions rail or Review opens cancellation confirmation:
`enter` / `y` cancels, `l` leaves the screen with the request running, and `esc`
keeps waiting. Leave answer editing or feedback first to reach that confirmation.
Cancellation keeps your text; late replies cannot apply. On Unix it kills the
process group; Windows currently kills only the direct process. Errors keep
answers and offer explicit `R` retry; there are no automatic retries. Only one
AI request runs at a time in the TUI.

Personal defaults are under **Settings → Garden → AI drafting**; session options
override them without saving preferences. See the
[settings guide](tui-cli.md#settings-in-the-preview) for account/model/effort,
question count, guidance and Restore defaults.

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

From the overlay's Plan tab the same request is `p` ([Plan tab](#plan-tab-in-the-issue-overlay)); it never includes earlier review feedback.

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
Bonsai leaves it running, like any agent. The planner is read-only except for one
file: before the session starts, Bonsai creates a private directory for the attempt
under `~/.bonsai/garden/plans/<local repo key>/<issue>/<attempt>/` and the prompt gives
the planner the exact path of `plan.md` inside it. When the plan is complete the
planner writes it there as Markdown and says so in the conversation, then keeps
discussing; if the plan changes it rewrites the whole file. That file is the only
thing the planner can write: Claude Code is started so every other write, including
repository files, `..` paths and symlinks, is denied
([how](agents.md#the-read-only-planner-role)). [Publishing the plan](#publish-and-review-a-plan)
is still a separate step you take.

Leaving the session with `esc` or leaving the planner screen does not stop the planner.
The attempts list shows each attempt's agent status and plan file state (the selected one
adds the plan file size and when it last changed), keeps refreshing while it is open, and
says so while one is live: *Planner keeps running in the background. Find it again: the
badge on this Issue, or p.* When a plan file is ready the list says *Plan ready: press u to
review and publish.* The [badges](#local-planner-badges) on the Board and Table are the way
back from anywhere in Garden.

Bonsai only looks at the plan file, never at the terminal. It reports it as **none**
(nothing written), **writing** (the agent is working or the file changed in the last
two seconds), **ready** (non-empty, at most 1 MiB, and the agent is idle or the attempt
ended) or **invalid** (a symlink, not a regular file, too large, or not the attempt's
own path). The file is not copied into any Bonsai record. Only the attempt's path is
recorded, and the file stays on disk until you delete the directory. Bonsai reads at
most 1 MiB of it and refuses a larger file, but it does not cap what the planner
writes. Bonsai also refuses plan directories that another user owns or that group or
others can write (Unix), and never follows a symlink to the file. The attempt records
now carry `plan_path` and `plan_published_at`; downgrading Bonsai below this version is
unsupported for `garden-planner.json`, because older versions reject the new fields.

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
the text box opens at once. When the attempt's plan file is **ready** and you have no kept
draft for that attempt, Bonsai reads the file and fills the box with it (the note under
the box says so); otherwise, or if the file cannot be read (the reason is shown), the box
is empty for you to type or paste a plan. You review and edit the text either way: Bonsai
reads nothing from the planner's terminal, the text is kept only in memory until you
publish it or Bonsai closes, and a late read never overwrites what you typed.
`ctrl+s` previews exactly what would be published, and only the confirmation after the
preview publishes. The plan
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
confirmation, even when a comment on GitHub asked for changes. (The overlay's Plan tab can instead start a revision attempt from the same request.) A comment written on
GitHub or a phone without a Bonsai record only appears as a hint ("newer on GitHub"); it
neither starts a run nor decides anything. A plain **Request AI plan** carries no earlier
feedback: a recorded reason reaches a planner only through a revision attempt bound to
that exact decision ([Plan tab](#plan-tab-in-the-issue-overlay)).

A source fingerprint must match the normalized content hash of the verified
specification revision named in the plan. Supplied fingerprints cannot redefine
that identity or carry an old approval onto a different specification.

### Plan tab in the issue overlay

The **Plan** tab of the issue overlay (`←`/`→` or click) shows where planning really
stands and offers only the actions that fit. Opening or rereading it reads (the plan
review and a *revise preview*); it never starts a planner, records a decision or retries
anything. What it shows comes from the verified plan log, the local planner summary and
that preview; nothing is guessed from terminal text.

| State | What it says | Actions |
|---|---|---|
| Specification not approved | Why planning is unavailable (draft, stale, revoked, unverified); the revision history stays | `p` request plan (disabled, with the reason), `r` open review |
| Planner running | Provider / account, effort, **elapsed since the request** (measured when drawn), the latest activity the planner reported and how long ago, whether it needs input, the plan file state (none yet, writing, ready, invalid) and "read-only, no worktree". No steps or progress are shown, and *idle* is never a finished plan | `p` open planner (straight to its session), `c` stop attempt, which asks `y` first |
| Ended with a ready, unpublished plan file | The plan is not a revision until published | `u` publish plan (the existing box: it previews the exact text and source spec before anything is written), `p` open planner |
| Revision awaiting your decision | `PLAN rN / NEEDS YOUR APPROVAL`: the summary (or the first Steps lines), the lines changed since the previous revision (`+`/`-`), Risks and Tests, the spec state and the distinct Latest / Current / Approved revisions | `a` approve plan rN, `c` request changes, `r` open review (whole document, comparisons, search, history) |
| Changes requested, no attempt answers them | "Feedback recorded on rN. No revision attempt is running.", your feedback and, if a start failed, why | `b` start revision attempt, `r` open review |
| Approved | "Approved rN against sM. Ready; coding starts only when you choose." | `r`, `p` |
| Approval no longer matches | Which side changed (the specification or the plan); the approved snapshot stays visible and is never re-approved for you | `r`, `p` |

`a` rereads the review and shows the same exact confirmation as plan review, inside the
overlay: the full plan hash, source spec fingerprint, spec head and GitHub actor, with
Cancel focused first. If the plan under review moved since the tab was drawn, or the heads
move before the write, nothing is recorded and the tab reads again.

`c` opens **Request changes to plan rN**: a required, multi-line reason (up to 4 KiB)
attached to that exact revision, the account the revision will use, a consent line for
uncommitted changes when the checkout has any, and the footer *Starts one read-only
revision attempt. Nothing starts coding.* The account, model and effort are the ones of
the attempt that wrote the revision, kept only while that account still exists and can
run a read-only planner; otherwise **Choose account…** (`ctrl+g`) opens the same read-only
account choice as Request AI plan, whose primary action (*Accept read-only and revise
plan*) sends the feedback. `ctrl+s` (**Send feedback & revise plan**) records
*changes requested* on the revision, then starts one read-only revision attempt that
receives your feedback and the text of that revision. The send is one write: further keys
are ignored until it answers. Text you typed survives Back, `esc` and a tab switch for the
same revision; if the revision under review changes, it is dropped and the tab says so.

Partial success is shown, not hidden. If the decision was recorded and the attempt could
not start, the tab shows the pending feedback with the reason and **Start revision
attempt** (`b`), a separate confirmation that starts the attempt for that recorded
decision and writes no new decision; a failed attempt can be started again this way, one
that is running or finished cannot. If the outcome of recording the decision is unknown,
nothing is started: your text is kept, and reopening the tab reads what is on the Issue.
Repeating a send finds the earlier decision and attempt instead of making new ones.

`esc` from the planner session, publish box or plan review that these actions open returns
to the Plan tab with its scroll restored; the planner keeps running.

### Approve the exact plan (gate two)

In plan review (`r` in an opened Issue), press `a` to **Approve plan**. The confirmation
names the full plan revision hash, current source spec fingerprint, spec head and
GitHub actor. Cancel has initial focus; `tab` then `enter` approves the named pair.
Bonsai rereads both heads and rechecks the authenticated account and current Write
access immediately before appending the decision. A moved head or changed account
refuses the confirmation; refresh and review again. The decision is read back and
verified before reporting success. Approval writes no stage label or plan pointer.

**Ready** requires both gates and one exact current verified plan/spec pair. Approval
starts no agent, implementation worktree or PR on its own. Launch is a separate,
explicit action ([below](#start-implementation-locally)). The actions have separate keys and confirmations: `v` validates a
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

## Start implementation locally

Only a **Ready** Issue (both gates, one verified plan/spec pair) can start coding.
Ready shows the effective implementer, AI reviewer, review limit, worktree/base
and native permissions from saved policy and cached defaults. Reviewer availability
is checked in the preview.

Press `s` (**Start implementation…**) in the Ready Issue's Overview to open the
exact preview directly with the default account. `e` (**Change run
settings**) opens the account, model, effort and permissions form. `i` opens
**implementation attempts**, even when launch is blocked. Approving, refreshing,
selecting, opening or navigating never starts anything. The preview shows:

- exact repository, Issue, spec and plan revision hashes and comment IDs, both
  approval IDs, actor, and the approved base commit (uncommitted changes in your
  checkout are excluded: the worktree starts from the committed base);
- the proposed branch (`bonsai/<number>-<first 8 chars of the approved hash>`), base ref, remote, resolved
  worktree path and the number of configured create hooks. `ctrl+e` edits branch, base
  and remote; any edit discards the preview and asks for a fresh one;
- the agent account, provider, model, effort and the provider's **native permission
  fields**, shown as inherited from the profile or overridden. Launch is stricter than
  a manual session: Codex needs an explicit sandbox and approval, Claude an explicit
  permission mode, Antigravity an explicit full-access value. Unresolved permissions
  block Confirm. Dangerous choices (Codex full access or bypass, Claude
  `bypassPermissions`/`auto`/`dontAsk`, Antigravity full access) need a separate,
  initially unchecked acknowledgment each time. Defaults are edited in Accounts, not
  here.

On the form `ctrl+s` requests the preview and never launches. In the confirmation
Cancel has initial focus. Confirmation lists the authorized sequence: implementation,
then the enabled automatic review, fixes and checks, then your review. Escalated
implementer permissions need a separate unchecked `x` acknowledgment; an escalated
reviewer needs its own unchecked `r` acknowledgment. A changed review policy needs
a new preview. `y` acknowledges retry hooks and `e` edits branch, base and remote.
Confirm (**Confirm: start locally**) creates the worktree in the daemon (placement and
hooks follow your `.bonsai.yaml` `worktree.root` and `worktree.path_template`), then starts one
agent holder with the full approved spec and plan as its prompt. The holder is the
same detached process as any [agent session](agents.md#garden-implementation-launch):
leaving the screen or quitting Bonsai only closes the view, never stops the agent.

**Blockers** refuse before any effect, with a safe reason: approval stale or changed
(spec or plan moved, approval revoked, competing heads), account unavailable, native
permissions unresolved or unsupported, base moved off the approved commit, branch or
path collision, no current Write access or stale GitHub data, or another attempt
already active for the Issue. Nothing is silently repaired or downgraded.

### Attempts, retry, stop

Each launch is a retained **attempt** (`[`/`]` switch run): Runs and session IDs, approved inputs, branch,
path, accepted native options, steps, remote status and PR state. History survives
every failure and is stored locally in `garden-launch.json` under the user config
directory (`bonsai/`); it holds no prompts, terminal output, commands or credentials.
A double submit, timeout or crash never starts a second holder: Bonsai reconciles the
exact operation on reopening and while open, and shows `start unknown` or
`recoverable` until it can tell. Reconcile never starts, pushes or opens anything.

The overview shows lifecycle state, observed activity and its age, checks only when
there is evidence, and **Next** from the attempt's frozen review policy. The last
synced GitHub stage is shown separately from the local holder. Idle means the
process is still running, never completion.

- **Open session** (`o`) observes the attempt's run without selecting a worktree.
  `enter` takes control; `ctrl+]` returns to observation. From observation, `esc`
  leaves the session.
  Leaving the overlay or quitting only detaches.
- **Retry** (`r`) is separately confirmed, repeats the retained-work and permission facts,
  reuses the owned worktree (your edits are kept) and starts a new numbered Run only
  once the old one is positively closed.
- **Stop run** (`x`) asks for confirmation and shows **Stopping** until holder closure
  is proven.

On start, Bonsai mirrors **In progress** to GitHub using the existing label check; a
clean exit mirrors **Review**, a failed or stopped run mirrors **Blocked**, never
Done. If GitHub is unreachable the local state stays true and shows
`remote status pending`. A failure before the agent started leaves the Issue Ready.
Success means **awaiting human review**, even with no commits. There is no automatic
merge, commit or branch deletion. The review sequence is recorded and authorized,
but reviewer execution arrives later; this version starts no automatic reviewer
and runs no review checks.

### Push and Draft PR

Nothing is pushed or opened at launch. Once the holder has ended, **Push branch and
open/link Draft PR** (`p`) has its own confirmation (remote, head, base, Draft). It is
available only for meaningful committed changes that are not dirty; Bonsai never
commits for you. It pushes the exact head, checks both Git and GitHub, looks for an
existing PR first and links it instead of creating a second. PR state is one of
`not-created`, `pending`, `unknown` (a create may have happened: inspect, never
re-created), `unavailable` or `linked`.

### Clean up

**Cleanup retained worktree** (`w`) first shows a read-only preview (exact path, branch,
HEAD) and needs its own confirmation with Cancel focused. It refuses live or unknown
holders, uncommitted, ignored or untracked files, extra commits, symlinks and a
changed preview, and removes the worktree without force. The branch and the attempt
history remain. Cleanup is never automatic and never erases your changes.

## Human review, rework and completion

When an implementation attempt reaches awaiting human review, the Issue overlay's
Overview becomes the review panel. It reads the retained worktree and GitHub, never
starts anything, and replaces its heading as the evidence changes: **YOUR REVIEW**,
**COMPLETED**, **BLOCKED**, **STALE** or **UNKNOWN** (a work that is not ready yet
says why: uncommitted changes, a run that has not finished, an unreadable head). It
shows:

- **CHANGES**: files, `+`/`-` line totals and the final head, then the changed files.
  There is no separate "Open changes" view; read the diff in the retained terminal
  (`o`) or the linked PR (`g`).
- **Verification**, only from recorded evidence: `No checks recorded`, `No AI review`,
  `AI review authorized, not run` and `Fixes after review were not re-reviewed`
  are named, never shown as a green check. Findings read `no AI review report` until
  a review report exists. Nothing in this version runs the reviewer or the checks
  (see [Automatic review defaults](#automatic-review-defaults)), so a real run
  currently shows `AI review authorized, not run` when you enabled the policy.
- **PR line**, evidence and time, **your decision** and the reasons with their next
  step.

Every decision is a short comment on the GitHub Issue (a `handoff` record) that
binds the exact specification revision and approval, plan revision and decision,
attempt, base, final head and tree, review pass and PR. It is re-verified on every
read: only the same trusted actors who may approve a specification count, and the
record hash covers the text you wrote. Nothing is merged, closed, pushed,
relabeled or cleaned by recording one, and no decision starts a run by itself.

### Accept, request changes and revoke

- **Accept…** (`a`) previews the exact comment, head and tree. Each acknowledgment
  the evidence needs (`No AI review was run on this work`, `Fixes made after the AI
  review were not reviewed again`, `Checks are missing or failed`) is an unchecked
  toggle (`1`-`9`) and must be ticked before **Record accepted** enables. Cancel is
  focused first. Acceptance refuses a dirty worktree, a run that changed no files
  and a stale specification or plan.
- **Request changes…** (`c`) opens a text box (`ctrl+s` previews the exact record,
  `esc` goes back keeping the text). The feedback is posted to GitHub and starts
  nothing.
- **Revoke acceptance…** (`u`) withdraws a current acceptance or non-code completion
  and returns the work to your review. Done work cannot be revoked: the merge
  happened outside Bonsai's control.
- **Open PR** (`g`), **Open terminal** (`o`) and **Preview cleanup…** (`w`) reuse
  the existing screens. `R` (the configurable refresh key) reads again.

Accepted, changes requested and revoked each show who decided and when. A new head,
tree, specification, plan, review or PR marks an earlier acceptance **STALE** with
the reason; it never carries over, so review again.

### Rework

After a verified changes-requested record, **Start rework…** (`m`) is a separate
confirmation (Cancel first, with the same acknowledgments as a launch for escalated
permissions). It runs one more attempt in the retained, owned worktree after the old
holder is proven closed. Your feedback is added to the new run's prompt only and is
not stored by Bonsai. One feedback comment starts at most one rework. When the
[automatic review policy](#automatic-review-defaults) is enabled a fresh review pass
is reserved for the new run; a pass that never started on the old run is closed as
`superseded`, and a rework that fails to start closes its pass as
`implementation-not-started`.

### Done

- **Code**: Done only from a fresh GitHub read (under two minutes old) showing that
  the linked pull request is merged, its head is the head you accepted and its base
  is the one it was published against. A squash or rebase merge is fine: the merge
  commit is recorded, not compared. Done survives worktree cleanup.
- **Non-code**: **Complete without code…** (`n`) records an explicit completion
  with your evidence text. It is refused when the attempt changed code, and Done
  then reads "Completed without code".

Done is shown in the overlay. It is not mirrored to the GitHub stage label or the
board column. Bonsai never merges, closes or relabels for you.

### Why the panel is not Done

| State | Reasons |
| --- | --- |
| Not ready | Uncommitted changes; run not finished; head unreadable |
| Stale | Head, tree, specification, plan, review, attempt or PR changed after the acceptance; the PR gained commits after it |
| Blocked | PR closed without merging; GitHub answered with another PR; merged head differs from the accepted one; different base; the attempt changed code, for non-code |
| Unknown | The handoff log, the PR or GitHub could not be read (offline, not found, read failed, or the read is older than two minutes) |
| Accepted | Waiting for the PR to be published or merged; merging stays a separate action |

Unknown never means Done. The visible panel reads again about every 15 seconds and
on refresh. See [TUI keys](tui-cli.md#garden-issue-overlay).

## Automatic review defaults

Settings → Garden → **Automatic review** saves personal defaults for a bounded
review after implementation. It is off by default. Settings grants no permission:
the [launch preview](#start-implementation-locally) authorizes the exact review,
permitted fixes and checks, with separate reviewer escalation acknowledgment.
The policy is frozen per attempt; Settings edits never change a started attempt,
and a policy change before confirmation requires a new preview. The sequence is
recorded and authorized, but nothing runs the reviewer yet in this version;
execution arrives later, including review checks.

- **Review after implementation** enables the default for new implementation starts.
- **Reviewer account**, **Reviewer model**, **Reviewer effort** and **Reviewer
  permissions** choose the reviewer and its provider-native options. Empty options
  inherit the account profile. Changing account clears model, effort and permission
  overrides; it never changes the account profile itself. Effort and permission
  choices use cached launch options only. Without a cache, only **Profile default**
  is offered, with “Launch options are not cached for this account yet.”
- **Review limit** is fixed at **One review + fixes + checks**. **Checks** names the
  verification commands. **Implementer** explains that the implementation account's
  launch defaults are chosen separately at each start.
- **Availability** explains whether the saved reviewer can launch. Reasons include
  automatic review being off, missing local accounts or account services, an account
  that cannot launch, and unresolved or unsupported native permissions. Inherited
  full access is unavailable until permissions are chosen explicitly; an explicit
  escalation still needs acknowledgment at start. No permission is silently widened.
- **Restore defaults** asks for confirmation, then clears only these personal
  defaults and turns automatic review off.

The checks text form accepts at most eight commands, separated by `;`, for example
`go test ./...; go vet ./...`. Each command is a typed executable plus arguments,
with whitespace separating words: no shell, quoting, pipes or variable expansion.
Shell executables, shell scripts (`.cmd`, `.bat`, `.ps1`) and wrappers such as `env`
and `sudo` are refused. An executable is a simple name or a clean absolute path.
Checks run relative to the worktree root by default; a typed `dir` must be a clean
relative folder with no parent segments, colons or backslashes. Checks stored with
a folder, custom display name or argument containing spaces cannot be edited in
the text form without loss. Edit those typed records in `state.json`, or restore
defaults.

The separate Settings → Agents → **Garden review policy** controls the
[Playground read-only reviewer](#read-only-verification-and-human-review).
Its triggers and review limits do not enable this bounded review/fix/check policy.

These defaults use `prefs.garden_auto_review` with `version: 1` in the personal
`state.json` (normally `~/.config/bonsai/state.json`, honoring the platform user
config directory). Invalid or newer-version values load as off, preserve their raw
JSON during unrelated saves, and cannot be overwritten until restored by the newer
Bonsai. Case variants of `garden_auto_review` are also quarantined and preserved.
The separate review pass store, `garden-review-passes.json` in the same Bonsai
config directory, refuses a newer schema and is never rewritten by an older
Bonsai. These protections preserve future data when downgrading.

## Read-only verification and human review

This section covers the Playground reviewer. For the Garden Issue overlay's review,
acceptance, rework and Done, see [Human review, rework and completion](#human-review-rework-and-completion).

In the redesign Playground, select an implementation session and press `r` for
Human review. This applies to a verified Garden attempt launched by Bonsai in
this clone. A manual or unlinked session is still a valid session; linking it to
an Issue does not invent an approved plan or authorize automatic review.

Automatic review is **off by default**. Settings → Agents → Garden review policy
stores consent only in the local personal preferences. Enabling it names the
account, enforced read-only role, trigger and scope. Changing an enabled account
or trigger asks again. The account must support enforced read-only access plus a
single writable report file; unsupported accounts remain visible with a reason.
A remote Issue, PR edit or request-changes comment cannot enable reviews or start
an implementation.

The scope is Bonsai-launched Garden attempts on this machine. Triggers are Manual,
When awaiting review, or On each new commit. The new-commit trigger also follows
running attempts after their first review. Automatic runs are limited per attempt
(default 3, configurable 1–10), with one reviewer in flight per attempt and at most
two automatic starts per reconciliation. While the redesign TUI is running, it
reconciles on startup and every 15 seconds after the preceding reconciliation
finishes, including from other workspaces. Durable run identities prevent repeats
across restarts; failed and blocked runs need an explicit retry. Each reviewer run
is bounded to 30 minutes and a size-limited report; changing or disabling consent
is read from disk before subsequent reconciliation. Closing the TUI does not stop
an existing holder; the next reconciliation settles its result.

Reports retain every revision, commit, base and reviewer outcome. A finding has
severity, file/line evidence and a recommendation. `n`/`p` selects findings and
`[`/`]` selects report revisions; scroll to read the evidence and decision history.
CI has its own pending/passed/failed/unavailable state, commit and freshness. A
failed lookup keeps the last good data visibly stale and cannot count as a pass.

- **Dismiss** (`d`) requires a reason and confirmation, bound to the exact finding,
  report revision and commit. A repeated finding in a later report remains open
  with its earlier dismissal visible.
- **Retry review** (`t`) confirms one local read-only run using the configured
  account, even when automatic review is off. It adds a report revision and does
  not count against the automatic run limit.
- **Request fixes** (`f`) opens the linked canonical Issue's existing account and
  implementation form. The Issue must still be Ready with the verified approved
  specification/plan pair; the normal isolation and final local confirmation
  follow. Nothing starts from reading remote feedback.
- **Accept changes** (`a`) previews the exact HEAD, report, all unresolved findings
  and independent CI snapshot. Depending on policy, open high findings and CI
  that has not passed need separate acknowledgments. Missing or older-commit
  reports also need acknowledgment; required CI older than five minutes or for a
  different commit is stale. A final confirmation records the local human
  decision and its evidence. Moved commits, report revisions or findings require
  a fresh preview.

Acceptance, merging a PR and cleaning up a worktree are separate actions. Accept
neither merges nor closes an Issue, stops a session, or deletes a worktree.
New commits mark earlier reports and acceptance as covering older code; they do
not inherit a reviewed or accepted claim. Prior attempts, findings, dismissal
reasons and decisions remain available. See [Playground keys](tui-cli.md#playground-in-the-preview)
and [provider enforcement](agents.md#the-read-only-reviewer-role).
