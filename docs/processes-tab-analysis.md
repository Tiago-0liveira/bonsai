# Processes tab — code map

Reference map of how the Processes tab renders, scrolls, and dispatches keys, plus
the reusable modal/fuzzy machinery available for future work. Line numbers reflect
the `feat/process-daemon` branch and will drift as code changes.

## 1. Rendering — list + detail split

The tab is a split: a scrolling output viewport for the *selected* process on top,
a pinned footer showing the full process list on the bottom.

- `internal/ui/view.go:592-608` — `View()`: when `m.rightTab == tabProcs`, builds
  `footer := m.renderProcFooter()`, shrinks a copy of the shared `m.term` viewport
  to leave room, then composes
  `rightBody = m.tabStrip() + "\n" + term.View() + "\n" + footer`.
- `internal/ui/view.go:85-107` — `renderProcFooter()`: a divider (`rule(...)`), one
  line per process via `procLine(p, url)` (id, label, status, policy, URL), a `› `
  cursor on the selected one, then `procHint`
  (`↑/↓ select · k kill · r restart · n new · x remove · v log`, view.go:78).
- `internal/ui/view.go:109-129` — `procLine()`: color-codes status
  (running/failed/done).
- `internal/ui/update.go:500-519` — `refreshProcPane()`: loads the selected
  process's full output into `m.term` via `m.term.SetContent(m.procs.Output(sel.ID))`
  (line 515), with `m.term.SetFollow(true)` (tail-follow, line 502). Only one
  process's log shows at a time; selection changes with up/down (see §3).
- `internal/ui/procview.go:13-119` is the data adapter (`procView`), not a renderer:
  caches daemon records (`v.recs`, refreshed each tick) and exposes `List`,
  `GetByID`, `Latest`, `Output` (reads full log text via `client.Logs`,
  procview.go:64-72), `LastURL` (procview.go:74-77), and mutations
  (`Spawn`/`Kill`/`Restart`/`Remove`/`SetPolicy`).

## 2. Scrolling — Processes tab vs. elsewhere

Reusable viewport: `internal/ui/components/terminal/terminal.go` wraps
`bubbles/viewport` (`vp viewport.Model`) with `SetContent`, `SetFollow` (tail vs.
preserve-offset), `EnsureVisible`, and `Update(msg)` that forwards to `m.vp.Update`.
This is `m.term`, shared by `tabLog` and `tabProcs`.

- **Working case (Git Log tab):** `onKey` falls through the whole switch to
  `m.forwardToPane(msg)`, which does `m.term, cmd = m.term.Update(msg)` when focus is
  the terminal pane. With no dedicated router, all unmatched keys (arrows,
  pgup/pgdn, and the viewport's own `u/d/b/f/j/k` bindings) reach the viewport.
- **Second reusable scroll pattern:** `modals.NewScroll` builds a `modeScroll` modal
  backed by its own `viewport.Model`, sized to 3/4 screen. Used for
  `KindDiffFile` / `KindLegend`.

**Processes tab does NOT scroll via arrows/j/k.** `internal/ui/update.go:559-563`
gates process-control keys ahead of the globals when
`m.rightTab == tabProcs && m.focus == focusTerminal`; inside `procTabKey`
(update.go:1478-1537):

- `up` / `ctrl+k` → `moveProcSel(wt.Path, -1)` (changes selection, not the offset).
- `down` / `ctrl+j` → `moveProcSel(wt.Path, 1)`.
- `k` → Kill, `r` → Restart consume the letters the bubbles viewport uses for Up
  (`k`) and half-page. Likewise `u`/`b`/`f`/`d` are grabbed by their global actions
  (update-base / checks-tab / fetch / diff-tab, keys.go:83/70/80/68) in the global
  switch before ever reaching `forwardToPane`.

**Net effect:** every natural scroll key is swallowed for selection or a global
action, so the log viewport is effectively unscrollable on this tab. Only truly
unbound keys — `pgup`/`pgdown`/space — fall through to `m.term.Update` and actually
scroll. Even then, `SetFollow(true)` in `refreshProcPane` snaps back to the bottom
on the next tick refresh. Giving this tab working scroll means either routing
pgup/pgdn/space explicitly or reserving j/k for the viewport and moving
selection/kill onto other keys.

## 3. Keybindings

Defaults (`internal/ui/keys.go`):

- Process-tab actions (keys.go:87-90): `kill_proc` `k`, `restart_proc` `r`,
  `set_policy` `p`, `proc_modal` `V`.
- Tab-switch (keys.go:65): `processes` `v` (global; opens the Processes tab).
- `keymapSections` group "Processes tab": `{kill_proc, restart_proc, set_policy,
  proc_modal}`.

Note the collisions: `r` is also `rebase` (global, keys.go:82), `p` is also
`aliases` (keys.go:77), `x` is `prune` (keys.go:84) reused here as "remove finished
process". The tab gate (§2) is what lets the process meaning win.

Dispatch (`internal/ui/update.go`):

- Gate: update.go:559-563.
- `procTabKey` switch (update.go:1478-1537):
  - `keys.Kill` → `m.procs.Kill(id)`; `refreshProcPane()` (1485-1490).
  - `keys.Restart` → `m.procs.Restart(id)`, updates `m.activeProc[wt.Path]`,
    `refreshProcPane()` (1492-1501).
  - `keys.Prune` (`x`) → `m.procs.Remove(id)`, reselect latest or clear,
    `refreshProcPane()` (1503-1513).
  - `keys.SetPolicy` (`p`) → `m.cyclePolicy(id)` (procmodal.go:98-117; cycles
    no → on-failure → always → no), `refreshProcPane()` (1515-1520).
  - `keys.Create` (`n`) → `loadScripts(wt.Path)` — starts a new command (1522-1523).
  - `up`/`ctrl+k`, `down`/`ctrl+j` → `moveProcSel` (1525-1533).
  - default → `false` → falls through to the global switch, then `forwardToPane`.
- Global `V` (`ProcModal`) → `m.openProcModal()` — cross-worktree processes modal,
  works from anywhere (not gated to `tabProcs`).
- Tab-switch `v` sets `m.rightTab = tabProcs` and calls `refreshProcPane()`.

## 4. Fuzzy-finder pattern

Real fuzzy matching (`github.com/sahilm/fuzzy`) is used in exactly one place:
`internal/core/fs/fs.go` — `Search()` does `fuzzy.Find(query, files)` for the "copy
file into worktree" feature. Wired at `internal/ui/update.go` via
`modals.NewFuzzy(modals.KindCopyFile, ...)`; palette entry in `palette.go`.

Every other "fuzzy" modal is plain case-insensitive **substring** matching, not the
library:

- `internal/ui/procmodal.go:40-66` — the Processes modal's `build(query)` closure:
  `q := strings.ToLower(strings.TrimSpace(query)); ... if q != "" &&
  !strings.Contains(strings.ToLower(label), q) { continue }`.
- The command palette (`internal/ui/palette.go`) uses the same
  `NewFuzzy`/`SectionedFuzzy` machinery — substring, not `sahilm/fuzzy`.
- Worktree list filter (`/`, keys.go:71) is the bubbles `list.Model`'s own built-in
  filter (`FilterValue()` returns branch + " " + path), neither bonsai code nor
  `sahilm/fuzzy`.

If future fuzzy modals need real scoring, `fs.Search` is the template; otherwise the
substring `build` closure in `procmodal.go` is the lighter path.

## 5. Modal/overlay system — generic, reusable

`internal/ui/components/modals/modals.go` is a single generic `Model` supporting
seven modes via one struct + mode enum: `modeInput`, `modeSelect`, `modeFuzzy`,
`modeConfirm`, `modePrune`, `modeScroll`, `modeMultiSelect`. Named constructors, all
returning the same `Model`:

- `NewInput(kind, title, placeholder)` — free text (e.g. commit message).
- `NewSelect(kind, title, items)` — static list.
- `NewFuzzy(kind, title, filter FilterFunc, initial []string)` — live-filtered flat
  list; caller supplies `func(query string) []string`.
- `NewSectionedFuzzy(kind, title, filter, initial []Section)` — grouped/collapsible
  list (`Section{Name, Items}`). Already the template `openProcModal`
  (procmodal.go:36-77) uses.
- `NewMultiSelect(kind, title, items, preselected)` — checkbox list; Space toggles,
  Enter emits `MultiSubmitMsg{Kind, Selected []int}`. Already used for a
  process-related picker: `procmodal.go:127-154`
  (`NewMultiSelect(modals.KindQuit, "Keep which processes running?", ...)`), handled
  at update.go:137-140 and `onQuitSubmit` (procmodal.go:158-169). Strong template for
  a "select processes to view/kill" multi-select.
- `NewScroll(kind, title, content)` — read-only viewport (KindDiffFile/KindLegend).
- `NewConfirm(kind, title)` — yes/no.
- `NewPrune(kind, title, mergeLabel, tailSteps, canMerge)` — bespoke multi-toggle
  pipeline modal, purpose-built for prune; less reusable as a generic template.

**Routing pattern:** build with a `New*` constructor, `modal.SetSize(m.width,
m.height)`, assign `m.modal = &modal`. Rendering: `view.go:582-584` —
`if m.modal != nil { return m.modal.View() }` takes over the whole screen.
Submit/cancel routed centrally in `update.go` via `modals.SubmitMsg` /
`MultiSubmitMsg` / `CancelMsg`, keyed off `msg.Kind` (constants in modals.go). Add a
new `KindXxx` for a new modal purpose.

There is **no** restart-policy picker modal today — policy is cycled in place with a
single keypress (`cyclePolicy`, bound to `p`). A `NewSelect`/`NewMultiSelect` modal
would be the natural template to replace or augment that.
