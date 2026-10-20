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

The current canonical backend has six stage labels and verifies specification
approvals only. The board conservatively projects accepted `ready` work without
a plan to Approved, and with a verified plan revision to Plan Review;
`in-progress` maps to Running, `review` to Human Review, and `done`/`blocked` to
their matching columns. **Planning, Ready and Reviewing remain explicitly empty**
until later plans provide active-job evidence and verified exact-plan approvals.
These presentation columns never add or change GitHub labels.

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
| `tab` (focus-next binding) | Switch Setup/Board when both are available |
| `ctrl+k` / `?` | Command palette / help |

Filters combine and apply to the same collection. An active filter labels the
count as filtered and shows the total eligible count. Priority is optional display
data from existing `priority:p0` through `priority:p3` labels (case-insensitive),
not a new managed label or a setup change. Missing or conflicting priority is `?`.
Search captures typed shortcuts; Enter applies it and Escape restores the previous
query and selection. The palette includes Garden: Board, Search approved work,
and Refresh. Table and Drafts remain unavailable until their own plans land.

Refresh keeps the selected stable ID even if its row or column changes. If an
issue is deleted or filtered out, selection falls back to the nearest row of the
same column, then the nearest populated column (right wins a distance tie).
If no match remains, the current column stays focused and explicitly empty.
New attention items never take keyboard focus.

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
authoring are unavailable until plan 08; local launch is unavailable until plan 09.
Selecting, opening, searching and refreshing never mutate workflow or launch work.

## Freshness and local memory

The footer shows the last **successful** sync time. Failed or incomplete refreshes
retain the last good board with stale/offline, access or rate-limit feedback.
Only a successful complete read can clear the collection. Before any successful
read, a failure says that approved work is unknown rather than calling it empty.
Per-item conflict and stale-approval indicators remain separate from selection.

The last Garden subview (Board or Setup) is remembered per local repository in
Bonsai's existing atomic UI state store. No query text, issue content, credentials,
commands or terminal output are persisted for this feature. Filters and issue
selection survive view navigation in the current process; they are not saved to
disk. Another Bonsai process has the existing UI-state last-writer limitation.

For the canonical schema and verification boundaries, see the
[Garden contract](design/tui-redesign/garden-contract.md).
