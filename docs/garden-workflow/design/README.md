# Garden workflow sketches / exploration 01

An exploratory visual review before writing an implementation plan. These are
proposals, not approved designs or working application features. All accounts,
issues, repository names, activity, checks and timestamps are fictional.

- [Open the PNG gallery and clickable explorer](index.html)
- [Six-screen overview](overview.png)
- [SVG/PNG manifest and cell-based click targets](manifest.json)
- [Original visual reference](../../design/tui-redesign/revision-02/index.html)

The gallery opens offline: no external fonts, libraries, analytics or network
requests are needed. Its buttons select screenshots only. Nothing creates a
GitHub issue, records approval, runs an agent or merges a pull request.

## What to review

| Area | Sketches |
|---|---|
| Drafting | Private inbox; single-field capture; manual specification editor; optional assistant; contextual questions; final specification; approval/publication preview |
| Views | Six-phase board; full nine-stage board; row table; phase-grouped table; Needs you filter |
| Ticket overlays | Approved, Planning, Plan review, Ready, Running, AI review, Human review, Done, Blocked; feedback for a plan revision |
| Recovery | Changed specification/obsolete approval; offline table retaining last successful data |
| Compact | 80×24 capture, inbox, manual editor, board, table, plan review and active run |

There are **24 scenes and 44 SVG/PNG pairs**. The main references use 120×35,
with selected 80×24 and 160×48 alternatives. The explorer opens every board card
and table row; table-origin overlays preserve the table backdrop and selected
row. Its stage shortcuts also expose each overlay directly. The static
gallery has category filters, size switches, full-size image inspection and
links to every vector source.

## Visual decisions

The shell, five workspace tabs, olive backgrounds, shaded submenu, breadcrumb,
focus footer and DejaVu Sans Mono follow revision 02. Every visible TUI element
is an integer terminal-cell operation: text, solid background or a box border.
The reference cell is 11×26 px, text is 18 px, and the outside margin is 18 px.
The actual application must use its terminal dimensions and configured theme.

Green marks accepted/available work, amber marks a human decision, and red marks
a blocker. Muted blue, cyan and plum extend the reference palette for planning,
implementation and AI review; these are proposed semantic theme colors. Color
is always accompanied by a word or symbol. An ASCII rendering can substitute
plain borders, `>`, `*`, `[ ]`, `!` and written state names for Unicode glyphs.

The no-AI editor uses a compact section rail. Inviting an assistant opens a
discussion pane beside the same editable specification. This keeps manual
drafting complete and avoids an unused conversation pane when AI is off.

The default board groups work into Backlog, Planning, Ready, Building, Review
and Done. Cards retain their exact detailed state and next action. A blocker
remains attached to its phase; the alternative detailed board retains a Blocked
column and all nine original stages. Board and Table represent the same issues.

An overlay uses opaque colored cells and a plain border, with the parent drawn
in a dim solid palette. No alpha blending, blur, shadows, fractional positioning
or graphical controls are required. On a compact terminal it becomes a focused
sheet. Tab cycles actions, Enter activates, Escape restores the parent selection;
long documents scroll in their active section. Terminal input is a separate,
explicit session action. These sketches do not introduce a new compositor
dependency: Bonsai currently uses Lip Gloss v1. See the upstream
[Lip Gloss styling/composition reference](https://github.com/charmbracelet/lipgloss)
for the underlying terminal drawing concepts; its v2 APIs are not assumed here.

## Proposed behavior boundaries

- Drafts are private and recoverable locally until **Approve & create issue**.
  This differs from the current branch's GitHub-first draft capture. A future
  implementation must define protected draft storage separately from sanitized
  warm-start/UI caches.
- Approval names the current specification. Plan approval names an exact
  specification/plan pair. New revisions explain why another decision is needed.
- Opening a card, changing a view or accepting a plan never starts coding.
  Starting implementation previews the worktree, accounts and permissions.
- AI assistance remains optional. Only accounts capable of enforcing a role's
  required permissions are available; unsupported providers never fall back to
  unrestricted access.
- The reviewer has one review/fix pass followed by relevant checks. Human review
  reports what was checked and whether the fixes received a second review.
- Agent exit does not mean Done. The illustrated coding-ticket completion is a
  confirmed PR merge. Publishing, merging and cleanup remain explicit actions.
- Stale GitHub data cannot authorize an approval or a new run. The last good
  view and retained work stay visible.

## Regenerate

Only the design generator needs Python, Node and Sharp. Normal Go builds and
tests do not depend on these files or rendering tools.

Regenerate SVGs, the manifest and gallery using Python's standard library:

```sh
python3 -I docs/garden-workflow/design/generate.py
```

To also render PNGs and the overview, pass a Node executable. The optional
`--modules` argument points to a `node_modules` directory containing Sharp;
omit it if Sharp resolves normally. `--fonts` points to a directory containing
DejaVu Sans Mono regular and bold. Fontconfig configuration and caches are
created in a temporary directory and removed after rendering.

```sh
python3 -I docs/garden-workflow/design/generate.py \
  --node /absolute/path/to/node \
  --modules /absolute/path/to/node_modules \
  --fonts /absolute/path/to/dejavu-font-directory
```

`generate.py` authors all SVGs, `render.mjs` rasterizes them and builds the
overview, and `gallery.html` is the offline HTML template. `index.html` embeds
the generated manifest so it also works directly from disk. `SHA256SUMS`
covers the exported SVGs, PNGs, manifest, gallery and overview.

To serve the gallery locally from the repository root:

```sh
python3 -m http.server 8767 --bind 127.0.0.1 --directory docs
```

Open `http://127.0.0.1:8767/garden-workflow/design/`. This serves design assets only
and keeps the sibling revision 02 links available.
