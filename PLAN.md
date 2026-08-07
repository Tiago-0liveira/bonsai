# bonsai — high-value features plan

Ordered by value / independence. Each phase is shippable on its own.
File refs are to the tree as of `a3efb63`.

---

## Phase 1 — More package managers (pnpm / yarn / bun / cargo / make)

**Why first:** self-contained, and fixes a live bug — `s` (scripts) always runs
`npm run <name>` even when the project uses another manager
(`internal/ui/update.go:515`).

### Current state
- `pkgmgr.PackageManager` interface exposes only `GetScripts() []string`
  (`internal/core/pkgmgr/pkgmgr.go:12`).
- `Detect` only reads `package.json`, always returns `*NPM`
  (`pkgmgr.go:40`). `NPM.Command(name)` exists but is **not** on the interface.
- UI hardcodes the run command: `m.spawn(wt.Path, msg.Value, "npm run "+msg.Value)`
  (`update.go:515`). CLI has no scripts path.

### Changes
1. **Widen the interface** (`pkgmgr.go`):
   ```go
   type PackageManager interface {
       Name() string          // "pnpm", "cargo", "make" — for the picker title
       GetScripts() []string  // selectable entries
       RunCommand(name string) string // full shell command to spawn
   }
   ```
2. **Implementations** (one file each or grouped in `pkgmgr.go`):
   - `node` backend parameterized by manager, sharing `package.json` parsing:
     - npm  → lockfile `package-lock.json` / bare `package.json`; run `npm run <s>`
     - pnpm → `pnpm-lock.yaml`; run `pnpm run <s>`
     - yarn → `yarn.lock`; run `yarn <s>`
     - bun  → `bun.lockb` or `bun.lock`; run `bun run <s>`
   - `cargo` → `Cargo.toml`; scripts = fixed subcommands
     `build test run check clippy fmt`; run `cargo <s>`.
   - `make` → `Makefile`/`makefile`; scripts = targets parsed by regex
     `^([A-Za-z0-9_-]+):` (skip `.PHONY`, pattern rules); run `make <s>`.
3. **Detect priority** (`Detect`): probe lockfiles first (pnpm > yarn > bun > npm),
   then `Cargo.toml`, then `Makefile`. Return first match, else `(nil, nil)`.
   Keep the `(nil,nil)` "none recognized" contract.
4. **Carry the manager to submit.** `scriptsMsg` only has `[]string`
   (`cmds.go:41`). Add the run-command mapping:
   - Option A (small): add `runCmd map[string]string` to `scriptsMsg`, build it in
     `loadScripts` (`cmds.go:103`), stash on `Model`, look up on submit.
   - Option B (cleaner): add `manager string` + keep pm; on submit call
     `pkgmgr`-side `RunCommand`. Prefer A to avoid holding a live pm on the model.
5. **Fix submit** (`update.go:515`): replace the hardcoded string with the looked-up
   run command. Title the picker `"Run "+pm.Name()+" script"` (`update.go:166`).
6. **Searchable scripts picker.** The scripts modal is a static `NewSelect`
   (`update.go:166`) — no filtering, painful once a project has many scripts /
   Makefile targets. Switch it to the existing fuzzy modal (`modals.NewFuzzy`,
   `modals.go:98`), same pattern as copy-file (`update.go:330`):
   filter over script names (reuse `fs.Search` or a simple substring/rank filter),
   `KindScripts` submit still maps the chosen name → `RunCommand`. Titles as
   `"Run "+pm.Name()+" script"`.
7. **Ad-hoc command in the scripts modal.** Beyond listed scripts, let the user run
   an arbitrary command (e.g. `zed .`, `docker compose up`) in the selected
   worktree. Two workable shapes:
   - **Sentinel entry** (matches existing patterns — cf. `addAliasLabel`,
     `update.go:337`): prepend `＋ run command…` to the scripts list; choosing it
     opens a `NewInput` whose value is spawned verbatim (`m.spawn(wt.Path, <typed>,
     <typed>)`, `update.go:568`). Works even when the project has no manager
     (`scripts == nil`, so still offer the modal with just this entry).
   - **Or** let the fuzzy modal submit its **raw query** when it matches nothing
     (a `ctrl+enter` / "run as command" affordance in `modals.go onEnter`).
   Prefer the sentinel entry — no modal changes, consistent with aliases.
   Spawned ad-hoc commands go through the same process manager, so they show up in
   the Processes tab (Phase 5) and can be killed/restarted. Optionally offer
   "save as alias" after a successful ad-hoc run (reuses `state.AddAlias`).
8. **CLI (optional, cheap):** add `bonsai run <script> [worktree]` mirroring
   `bonsai x`, using `pkgmgr.Detect` + `RunCommand` so scripts work headless.

### Tests
Extend `pkgmgr_test.go`: temp dirs with each lockfile/manifest → assert
`Detect` picks the right `Name()`, `GetScripts()` set, and `RunCommand` prefix.
Makefile target parsing gets its own table test.

---

## Phase 2 — Per-PR base branch from gh

**Why:** ahead/behind, the prune "merge to X" label, and `{base_branch}` all use
the **global** `upstream` (`origin/main`), not the PR's real base. Wrong for repos
with multiple bases / stacked PRs. (The actual `gh pr merge --merge` already
targets the PR's own base — `gh.go:39` — so only the *display + metrics + hook var*
are wrong.)

### Current state
- `gh.PR` = `{Number, Title, Head}` (`gh.go:13`); `ListPRs` requests
  `number,title,headRefName` (`gh.go:22`).
- `prByBranch map[string]gh.PR` keyed by head branch (`model.go:49`,
  `update.go:131`).
- Metrics use `m.cfg.Upstream` for every branch (`update.go:107`).
- Prune label: `config.BaseBranch(m.cfg.Upstream)` (`update.go:442`).
- `{base_branch}` hook var derived from the passed `upstream`
  (`config/hooks.go:31`).

### Changes
1. **Fetch the base** (`gh.go`): add `BaseRefName string \`json:"baseRefName"\``;
   request `number,title,headRefName,baseRefName` (`gh.go:22`).
2. **Resolve per-branch upstream** — new helper on `Model`:
   ```go
   func (m Model) upstreamFor(branch string) string {
       if pr, ok := m.prByBranch[branch]; ok && pr.BaseRefName != "" {
           return remoteOf(m.cfg.Upstream) + "/" + pr.BaseRefName // e.g. origin/develop
       }
       return m.cfg.Upstream
   }
   ```
   `remoteOf("origin/main") == "origin"` (mirror of `config.BaseBranch`).
3. **Metrics** (`update.go:107`): metrics load fires from `onWorktrees`, which runs
   *before* `prMapMsg` arrives, so `prByBranch` may be empty on first pass. Fix:
   after `onPRMap` populates the map (`update.go:127`), re-issue `loadMetrics` for
   PR-backed branches with their real upstream. Cheap and keeps first paint fast.
4. **Prune label** (`update.go:442`): use
   `config.BaseBranch(m.upstreamFor(wt.Branch))`.
5. **Hook var** (`update.go`/`cmds.go` where `HookVars` is built for prune & alias,
   e.g. `cmds.go:139`, `update.go:526`): pass `m.upstreamFor(wt.Branch)` instead of
   `m.cfg.Upstream` so `{base_branch}` matches the PR.

### Edge cases
- No gh / unauthenticated → `prByBranch` empty → falls back to global upstream
  (current behavior preserved).
- `pr-N` branches created locally have no gh entry until `fetchPRs` matches head;
  fallback covers them.

### Tests
`config` test for `remoteOf`. UI wiring is integration-ish; cover `upstreamFor`
with a small table (map hit vs miss).

---

## Phase 3 — Worktree filter/search + configurable path template & root

Two independent sub-features; ship together or split.

### 3a. Filter/search in the worktree list
`Item.FilterValue()` already returns `branch + " " + path`
(`worktreelist.go:49`) and bubbles list filtering is on by default — but it's
**unreachable**: `onKey` (`update.go:204`) intercepts bare letters (`n c s p r x v
C R`) before forwarding, so `/` never reaches the list and any filter keystroke
triggers an action instead.

**Changes:**
- Expose filter state: `worktreelist.Model.Filtering() bool` wrapping
  `list.SettingFilter()` / `FilterState()`.
- In `onKey` (`update.go:204`), **guard first**:
  ```go
  if m.focus == focusList && m.list.Filtering() {
      return m.forwardToPane(msg) // let the list own every key while filtering
  }
  ```
- Add a `/` hint to the help bar (`keys.go` short/full help) — no new binding
  needed; `/` is the list's built-in.
- Confirm the custom `itemDelegate` (`worktreelist.go:81`) renders filtered items;
  bubbles passes the filtered slice through `Render` unchanged, so PR badges/metrics
  still work.

### 3b. Configurable worktree path template + root dir
`git.WorktreePath(repoDir, branch)` hardcodes sibling `<repo>-<branch>`
(`git.go:160`), used by `cmds.go:202/205/208` and `cli.go:97/104/111`.

**Changes:**
- Config (`config/config.go:24`):
  ```yaml
  worktree:
    root: "../"                       # base dir; default: parent of repo
    path_template: "{repo}-{branch}"  # default preserves current layout
  ```
  Add `Worktree struct { Root, PathTemplate string }`; defaults in `Load`.
- New `config`-level builder (keep `git` config-agnostic):
  `func (c *Config) WorktreePath(repoDir, branch string) string` that expands
  `{repo} {branch} {root}` (slashes in branch flattened to `-`, as today) and joins
  against `Root`. Fall back to `git.WorktreePath` semantics when unset.
- Repoint callers: `cmds.go` `createWorktree` and `cli.go` `cmdCreate` already load
  `cfg` (or can) → call `cfg.WorktreePath(...)`. `git.WorktreePath` stays as the
  default-template implementation.
- Document new keys in `.bonsai.yaml`.

**Edge cases:** ensure `Root` is created (`os.MkdirAll`) before `git worktree add`;
absolute vs relative root resolved against `repoDir`.

### Tests
`config` test: template expansion + slash flattening + root join, default equals
current `git.WorktreePath` output.

---

## Phase 4 — Colored git log + commit preview modal

### 4a. Colorize the git log
`git.Log` uses `log --oneline --graph --decorate` with no color
(`git.go:217`); output isn't a TTY so git suppresses color by default. The viewport
passes ANSI through untouched (`terminal.go`; bubbletea renders raw ANSI), and
`lipgloss ...Width().Render` (`terminal.go:33`) preserves SGR codes.

**Change:** add `--color=always` (or run with `-c color.ui=always`) in `git.Log`.
One line. Verify wrapping in `SetContent` doesn't split mid-escape (lipgloss handles
this); if artifacts appear, wrap with `ansi`-aware wrapping from
`charmbracelet/x/ansi` (already a dep).

### 4b. Commit modal shows status/diff before committing
Today `C` opens a bare message input (`update.go:350`) with no visibility of what
gets `git add -A`'d (`git.go:207`).

**Changes:**
- Core: `git.StatusShort(dir)` → `git status --short`; `git.DiffStat(dir)` →
  `git diff HEAD --stat --color=always` (staged+unstaged summary).
- Modal: add an optional `body string` to the input modal, rendered above the text
  field (`modals.go` `NewInput` → `NewInputWithBody`, or a `preface` field +
  render in `View` `modeInput` branch, `modals.go:252`). Keep it scroll-safe /
  height-clamped.
- `openCommitModal` (`update.go:350`): gather status+diffstat first (a tea.Cmd so
  the git calls stay off the UI goroutine), then open the modal seeded with that
  body. Empty status → show `(working tree clean)` and still allow an empty-commit
  guard (current empty-message guard stays, `update.go:546`).

### Tests
`git` tests for `StatusShort`/`DiffStat` against a scratch repo (pattern already in
`git_test.go`). Modal body rendering: a view snapshot / substring assert.

---

## Phase 5 — Processes as a right-panel tab + full process control

Biggest change; the others don't depend on it. Replaces the process **modal**
(`KindProcesses`, `update.go:391`) with a persistent tab in the right pane and adds
kill / restart / start.

### Current state
- Right pane is a single viewport titled "Git Log" or the active process
  (`view.go:65`, `update.go:182`). `v` opens a select modal listing procs; picking
  one just sets `activeProc` and focuses the pane (`update.go:531`).
- `exec.Manager` supports `Spawn`, `List`, `GetByID`, `Latest`, `Kill` (per proc),
  `KillAll` (`exec.go`). No kill-by-id, no restart.
- `Process.Status()` already returns `running` / `failed` / `done` (`exec.go:64`).

### 5a. Manager control ops (`exec.go`)
```go
func (m *Manager) KillByID(path string, id int)      // kill one
func (m *Manager) Restart(path string, id int) (*Process, error) // respawn same label+command
func (m *Manager) Remove(path string, id int)        // drop a finished proc from the list
```
`Restart` reuses stored `Label`/`Command` (already on `Process`, `exec.go:15`) via
`Spawn`; returns the new proc so the UI can mark it active.

### 5b. Right-pane tabs
Restructure the right pane into a small tab strip on top + a body:
- Tabs: **Git Log** (default) and **Processes**.
- A new `rightTab` enum on `Model` (or a tiny `rightpane` component). `v`
  (`ViewProcs`, `keys.go:32`) selects the Processes tab and moves focus there (no
  more modal). Pressing `v` again / `tab` cycles.
- `view.go`: render tab headers (active = `paneTitle` style, inactive = dim) above
  the viewport; subtract their height in `layout()` (`view.go:40`, currently only
  `-1` for the single title line).

**Processes tab body** (new small component, e.g.
`internal/ui/components/procpane`):
- Left-of-body or top: a compact list of this worktree's procs
  `#N label (status)` with **colored status** — green `running`, red `failed`,
  dim `done` (styles like `worktreelist.go:65`). Selecting a row shows its
  `Output()` below (reuse the existing viewport / terminal component).
- Keybinds while focused: `k` kill, `r` restart (finished → respawn), `n` start
  (opens the aliases/scripts picker → spawns for this worktree), `enter` focus
  output, `x` remove finished, `↑/↓` navigate.
- The `procTickMsg` loop (`update.go:182`, `cmds.go:225`) keeps refreshing the
  selected proc's output and the status colors live.

### 5c. Preserve process output color (stretch)
Processes run `sh -c` with pipe stdio (`exec.go:102`), so most tools drop color.
Two levels:
- **Cheap:** set `CLICOLOR_FORCE=1` + `FORCE_COLOR=1` in the spawned env; many CLIs
  honor it. Viewport already renders ANSI.
- **Full:** allocate a PTY (`github.com/creack/pty`) per process so tools see a
  terminal and emit native color; copy the PTY into the existing `syncBuffer`.
  Bigger dep + lifecycle work — do as a follow-up after 5a/5b land.

### Migration / cleanup
- Remove `KindProcesses` modal path (`modals.go:38`, `update.go:391`,
  `update.go:531`) once the tab replaces it, or keep `v`-from-anywhere as the tab
  selector.
- `openProcessModal`'s "no processes" status becomes an empty-state line in the tab.

### Tests
`exec` tests: `KillByID` stops one and leaves siblings; `Restart` yields a new
running proc with same label/command; `Remove` drops from `List`. UI tab switching
is manual/QA.

---

## Suggested delivery order
1. **Phase 1** (bug fix + high use) →
2. **Phase 3a** (tiny, pure win) →
3. **Phase 2** (correctness) →
4. **Phase 4** (log color + commit preview) →
5. **Phase 3b** (path config) →
6. **Phase 5** (tabbed process control; 5c last).

Each phase compiles and ships independently; no phase blocks another except 5c
depends on 5a/5b.

---
---

# Round 2 — GitHub deepening, worktree power, config & polish

Phases 1–5 above have largely landed on the working tree (right-pane tabs, procs
control, per-PR base, path config, colored log, commit preview, multi-manager).
This round adds the selected features. File refs are to the current working tree
(commit `a3efb63` + uncommitted changes).

The headline is **Phase 6 (PR detail tab)**; everything else layers on the same
right-pane-tab and `gh`-wrapper patterns already in place.

Shared building block used by most of this round: the right pane is one
`terminal.Model` viewport whose content is a plain (optionally ANSI-colored)
string, selected by `m.rightTab` (`model.go:26`, rendered via `tabStrip`
`view.go:107`). New tabs = new `rightTab` values + a render function that fills
`m.term.SetContent(...)`; tab-local keys hook in exactly like `procTabKey`
(`update.go:546`).

---

## Phase 6 — PR detail tab (headline)

A third right-pane tab that shows a connected PR the way GitHub does: title,
state, description, metadata, and the full activity timeline — all read-only in
this phase (actions land in Phase 8).

### Current state
- `rightTab` has only `tabLog` / `tabProcs` (`model.go:29`); `tabStrip` hardcodes
  the two labels (`view.go:107`).
- `gh.PR` carries only `{Number, Title, Head, Base}` (`gh.go:13`); the only
  read op is `ListPRs` (`gh.go:22`).
- `m.prByBranch` already maps head branch → PR (`update.go:138`), and
  `m.prForBranch` (`update.go:821`) resolves the selected worktree's PR number.

### 6a. gh core: fetch one PR in full
New in `gh.go`:
```go
type PRDetail struct {
    Number    int
    Title     string
    State     string   // OPEN / MERGED / CLOSED
    IsDraft   bool
    Author    string
    Base, Head string
    Mergeable string    // MERGEABLE / CONFLICTING / UNKNOWN
    Labels    []string
    Assignees []string
    Reviewers []string
    Body      string    // markdown
    URL       string
    Additions, Deletions, ChangedFiles int
    Comments  []TimelineItem  // issue comments + review threads, chrono
    Reviews   []Review        // APPROVED / CHANGES_REQUESTED / COMMENTED + body
    CreatedAt, UpdatedAt string
}

func ViewPR(dir string, number int) (PRDetail, error)
```
Implement with one call:
`gh pr view <n> --json number,title,state,isDraft,author,baseRefName,headRefName,mergeable,labels,assignees,reviewRequests,body,url,additions,deletions,changedFiles,comments,reviews,createdAt,updatedAt`
and unmarshal. `comments` + `reviews` come back in that JSON, so no second call
for the timeline. Mirror `ListPRs`' error handling (`gh.go:26`).

### 6b. Wiring: load-on-select
- New msg + cmd in `cmds.go`: `prDetailMsg{detail gh.PRDetail; err error}` and
  `loadPRDetail(repoDir, number)`.
- Trigger points:
  - On selection change: `forwardToPane` (`update.go:398`) already reloads the log
    when the highlighted path changes — in the same branch, if the newly selected
    worktree has a PR (`m.prForBranch`) and `rightTab == tabPR`, also issue
    `loadPRDetail`.
  - On entering the PR tab (6c).
- Cache the last detail on the model: `prDetail map[int]gh.PRDetail` (keyed by PR
  number) + a `prDetailErr`. Re-fetch on `R` (Refresh, `update.go:335`).

### 6c. The tab
- Add `tabPR rightTab` (after `tabProcs`, `model.go:29`).
- Generalize `tabStrip` (`view.go:107`) from the hardcoded 2-label string to a
  loop over the visible tabs. The **PR tab is only visible when the selected
  worktree has a PR** (`m.prForBranch(selected)`), so `tabStrip` and the tab-cycle
  must consult that.
- Selecting it: extend `toggleProcsTab` into a general `cycleTab` /
  add a `P` binding (`keys.go`) that jumps straight to the PR tab and focuses the
  right pane (same focus dance as `toggleProcsTab`, `update.go:529`). `tab` from
  the right pane cycles Log → Procs → PR(if any) → Log.
- Render function `renderPRPane()` (sibling of `refreshProcPane`,
  `update.go:249`) builds an ANSI string into `m.term`:
  ```
  #123  Add OAuth login            [OPEN] ●draft
  base ← head   by @alice   +420 −37  · 12 files   mergeable: CONFLICTING ⚠
  labels: enhancement, needs-review    reviewers: @bob (approved), @carol (pending)
  ────────────────────────────────────────────
  <body markdown, rendered>
  ────────────────────────────────────────────
  Timeline
   @bob  approved · 2d ago
     LGTM apart from the naming
   @carol  commented · 1d ago
     …
  ```
  - State/draft/mergeable get color (reuse the `procRunning`/`procFailed`/`procDim`
    palette in `view.go:28`, or the Phase 14 theme).
  - Markdown body: cheapest is to show it raw; better is to pipe through
    `glamour` (`charmbracelet/glamour`, a natural fit here) into a width-fit ANSI
    render. Recommend glamour — it's the same ecosystem and makes the body read
    like GitHub. Gate width on `m.term` size.
  - Viewport already scrolls (`term.Update`), so long PRs scroll with the pane
    focused. No pager needed.
- Empty/edge states: worktree has no PR → tab hidden. gh missing/unauth →
  `prDetailErr` shown as a one-line note in the pane (mirrors the log's
  "(no commits yet)" fallback, `update.go:44`).

### Tests
- `gh` test: unmarshal a captured `gh pr view --json …` fixture into `PRDetail`
  (table over a JSON blob in `testdata/`); assert timeline ordering and counts.
- UI render: `renderPRPane` substring asserts (title, state, a comment body) given
  a fixed `PRDetail`.

---

## Phase 7 — CI / checks status

A rollup badge on each PR-backed worktree row, and a full checks list in the PR
tab.

### 7a. gh core
```go
type Check struct { Name, State, Bucket, Link string } // Bucket: pass/fail/pending/skipping
func Checks(dir string, number int) ([]Check, error)   // gh pr checks <n> --json name,state,bucket,link
```
`gh pr checks` exits non-zero when checks are failing/pending — treat a non-zero
exit **with parseable JSON** as success (parse anyway); only a JSON error is a
real error.

### 7b. Rollup + list
- Rollup helper: `func rollup([]Check) string` → `"pass" | "fail" | "pending" | ""`
  (fail if any failed, else pending if any pending, else pass).
- List row badge: add `Checks string` (rollup) to `worktreelist.Item`
  (`worktreelist.go:20`); render a colored dot in `Render` next to the `#N` badge
  (`worktreelist.go:97`) — green ●, red ●, yellow ◐.
- Fetch: after `onPRMap` populates PRs (`update.go:134`), batch a
  `loadChecks(path, number)` per PR-backed branch → `checksMsg{path, rollup}` →
  store on model → `rebuildItems`. Runs quietly like metrics; keep first paint
  fast (checks arrive after).
- PR tab: append a **Checks** section to `renderPRPane` listing each check with
  its colored bucket + name (link shown dimmed).

### Tests
`rollup` table test (all-pass, one-fail, mixed-pending, empty). gh unmarshal
fixture for `Checks`.

---

## Phase 8 — PR actions (reviews · merge strategy · close/reopen · create)

All actions run from the PR tab (or the worktree row) via `gh`, gated by Phase 12
confirmations for the destructive ones. Each is a `tea.Cmd` returning `opDoneMsg`
(the existing refresh-on-done path, `update.go:219`) plus a `loadPRDetail`
re-fetch so the tab updates.

### 8a. gh core (`gh.go`)
```go
func CreatePR(dir, title, body, base string, draft bool) (int, error) // gh pr create …
func MergePRStrategy(dir string, n int, strat string) error            // strat: merge|squash|rebase
func ClosePR(dir string, n int) error                                  // gh pr close <n>
func ReopenPR(dir string, n int) error                                 // gh pr reopen <n>
func ReadyPR(dir string, n int) error                                  // gh pr ready <n>  (undraft)
func ReviewPR(dir string, n int, kind, body string) error              // gh pr review --approve|--request-changes|--comment
```
`MergePR` (`gh.go:41`) becomes the `strat=="merge"` case of `MergePRStrategy`;
keep it as a thin wrapper so Phase-5 prune-merge still compiles.

### 8b. Create PR from worktree
- New binding (e.g. `ctrl+n` or a `create-pr` action key, `keys.go`) enabled when
  the selected worktree has **no** open PR.
- Flow (reuse the modal chain like the new-alias flow, `update.go:668`):
  `KindPRTitle` (input) → `KindPRBody` (input, optional) → base defaults to
  `config.BaseBranch(m.upstreamFor(branch))`, offer draft toggle via a
  `NewConfirm`-style "Draft?" step or a prune-style checkbox. On submit call
  `CreatePR`; on success push first if the branch has no upstream (detect via a
  `git.HasUpstream`/`push -u` fallback), then refresh PRs.
- Add modal `Kind`s: `KindPRTitle`, `KindPRBody`, `KindPRDraft`.

### 8c. Reviews / close / reopen / ready
- PR-tab-local keys (added to `procTabKey`'s sibling `prTabKey`, gated on
  `rightTab==tabPR && focus==focusTerminal`):
  - `a` approve, `R`… (careful: `R` is Refresh) → use distinct keys, e.g.
    `a` approve, `x`? (taken) → pick a small non-conflicting set:
    `a` approve · `c` request-changes · `m` merge · `X` close · `O` reopen ·
    `d` ready-for-review. (Resolve against existing globals in `keys.go`;
    tab-local bindings shadow globals only while the PR tab owns focus, same as
    proc keys.)
  - `a`/`c` open a `KindReviewBody` input (optional message) → `ReviewPR`.
  - `m` opens a **merge-strategy** select modal (`KindMergeStrategy`,
    `NewSelect` of `merge|squash|rebase`) → Phase-12 confirm → `MergePRStrategy`.
- Every destructive action (`merge`, `close`) routes through the Phase 12 confirm
  wrapper.

### Tests
gh wrappers are thin shell-outs — cover argument construction by extracting the
`[]string` args into a testable builder (e.g. `mergeArgs(n, strat)`), table-test
those. Modal-chain wiring is manual QA.

---

## Phase 9 — Worktree list: dirty badge + sort

### 9a. Dirty badge
- Core: `git.Dirty(dir string) (bool, error)` → `git status --porcelain` non-empty
  (cheap; add near `Status`, `git.go:223`). Optionally `DirtyCount` for a number.
- Msg/cmd: `dirtyMsg{path string; dirty bool}` + `loadDirty(path)`; batch alongside
  metrics in `onWorktrees` (`update.go:109`).
- Item: add `Dirty bool` (`worktreelist.go:20`); render a `●` (yellow) in
  `Description` or after the title (`worktreelist.go:41/100`).

### 9b. Sort
- `sortMode` enum on `Model` (`name | ahead | behind | pr | activity`) with a key
  to cycle (e.g. `o` for "order"; free in `keys.go`).
- Sort `m.worktrees` in `rebuildItems` (`update.go:168`) before building items —
  but **always pin the main worktree first** (`IsMain`), and preserve the
  highlighted path across re-sorts (look up new index by path).
- `activity` needs a timestamp: `git.LastCommitUnix(dir)` (`git log -1
  --format=%ct`), fetched like metrics and cached on the model.
- Show the active sort mode in the list title (`worktreelist.go:120`, `l.Title`)
  e.g. `Worktrees · ↑ahead`.

### Tests
`git.Dirty` against a scratch repo (clean vs touched). Sort: table over a slice of
worktrees per mode, asserting main-first + order.

---

## Phase 10 — Diff vs base · update from base · fetch

### 10a. Diff-vs-base tab
- Core: `git.DiffBase(dir, base string) (string, error)` →
  `git diff --color=always --stat <base>...HEAD` for the summary; a second
  `git.DiffBaseFull` (`git diff --color=always <base>...HEAD`) for the body.
  Use the `...` (merge-base) form so it's "what this branch adds".
- New `tabDiff rightTab`; render `SetContent(statLine + "\n\n" + fullDiff)` into
  `m.term` (ANSI passes through as with the log, `git.go:217`). Base comes from
  `m.upstreamFor(branch)`. Load on tab-enter + on selection change (like the log).
- Only meaningful for non-main worktrees; on main show "(base branch)".

### 10b. Update from base
- Modal `KindUpdateBase` (`NewSelect` of `rebase | merge`); target =
  `m.upstreamFor(branch)`.
- `rebase` → reuse `git.RebaseCmd` via `tea.ExecProcess` (`update.go:738`) so
  conflicts are resolved interactively; `merge` → `git.MergeCmd(dir, base)`
  (new, same shape as `RebaseCmd`, `git.go:229`) also via `ExecProcess`.
- Guard behind Phase 12 confirm (it rewrites/merges history).
- Binding: e.g. `u` (free in `keys.go`).

### 10c. Fetch
- `git.Fetch(dir) error` → `git fetch --all --prune` (`git.go`).
- Binding `f` → `opDoneMsg`-returning cmd (mirror `gitPull`, `cmds.go:125`); on
  done, re-run metrics so ahead/behind refreshes.

### Tests
`git.DiffBase` / `Fetch` against a scratch repo with a diverged branch.

---

## Phase 11 — Bulk prune merged

One action that finds every worktree whose branch is already merged and prunes
them together, behind a single confirmation listing the victims.

### Detection
Two signals, union them:
- **gh**: `gh pr list --state merged --json number,headRefName --limit 100` →
  merged head branches (new `ListMergedPRs`, `gh.go`).
- **git**: `git branch --merged <base>` per base (new `git.MergedBranches`).
Skip the main worktree and any dirty worktree (from 9a) unless the user opts in.

### Flow
- Binding (e.g. `shift+X`, distinct from prune `x`).
- Build the candidate list, open a confirm/checklist modal (extend the prune modal
  or a new `KindBulkPrune` select-multiple; simplest: a `NewConfirm` showing the
  count + the branch list in its body via `SetBody`).
- On confirm, run the existing `pruneWorktree` (`cmds.go:140`) per candidate in
  sequence (no merge step — they're already merged), aggregate into one
  `opDoneMsg` ("pruned 3 worktrees"), then `loadWorktrees`.

### Tests
`MergedBranches` parse test. Candidate-selection unit (given worktrees + merged
set + dirty set → expected victims, main always excluded).

---

## Phase 12 — Confirm destructive ops (cross-cutting)

A reusable confirmation gate so merge / close / force-push / update-from-base /
bulk-prune all prompt before firing. Prune already has its bespoke modal
(`KindPrune`); this generalizes the pattern to the new destructive actions.

### Design
- `NewConfirm` already exists (`modals.go:108`) and defaults the cursor to "No"
  (`modals.go:109`) — good.
- Add a `pendingConfirm` field on `Model`: a `func() tea.Cmd` (the action to run
  on "yes") plus a title/body. Helper:
  ```go
  func (m Model) confirm(title, body string, action tea.Cmd) (tea.Model, tea.Cmd)
  ```
  opens a `NewConfirm` (with `SetBody(body)`), stashes `action`.
- Route `KindConfirm` "yes" in `onModalSubmit` (`update.go:646`) to run the stashed
  `pendingConfirm` action.
- **Config toggle** to disable globally for power users:
  `confirm_destructive: true` (default) in `.bonsai.yaml` → `Config`
  (`config.go:36`). When false, `confirm(...)` runs the action immediately.

### Wire-in points
Merge (8c), close (8c), update-from-base (10b), bulk-prune (11), and optionally a
force-push variant. Prune stays as-is (already confirmed).

### Tests
`confirm` with toggle on → modal opened, action deferred; toggle off → action cmd
returned directly.

---

## Phase 13 — Configurable keybindings

Let users remap the global keys from config; keep the current bindings as
defaults.

### Current state
`newKeyMap` hardcodes every binding (`keys.go:29`). Help is derived from the
`key.Binding` help text (`keys.go:54`), so remapping must update both keys and
help.

### Changes
- Config: `keys: map[string]string` (`config.go`), action name → key(s), e.g.
  ```yaml
  keys:
    new_worktree: "n"
    prune: "x"
    pull: "ctrl+p"
  ```
- Define stable **action name constants** (one per `keyMap` field). `newKeyMap`
  becomes `newKeyMap(overrides map[string]string)`: start from defaults, and for
  each override rebuild that `key.Binding` with `key.WithKeys(<new>)` +
  `key.WithHelp(<new>, <same desc>)`.
- Validate: reject a mapping that collides with another action (two actions on the
  same key) — surface as a load-time warning in the status bar, fall back to
  default for the offender.
- Thread `cfg.Keys` into `New` (`model.go:80`) → `newKeyMap`.

### Tests
`newKeyMap` with an override map: asserts the rebound key matches and help text is
preserved; collision detection.

---

## Phase 14 — Themes

Centralize the scattered `lipgloss.Color` literals into one theme, selectable by
preset and overridable per-color from config.

### Current state
Colors are hardcoded in at least three files: `view.go:15-33`,
`worktreelist.go:65-71`, `modals.go:233-246`. Adding a theme means routing all of
them through one source.

### Changes
- New `internal/ui/theme` package: a `Theme` struct of named roles
  (`Accent, Border, BorderFocus, Dim, Success, Danger, Warning, Text, PRBadge,
  …`), plus built-in presets (`bonsai` = current pink/205 look, `dracula`,
  `nord`, `mono`). Each role is a `lipgloss.Color`.
- Replace the package-level `var (... lipgloss.NewStyle()...)` blocks with a
  constructor that takes a `Theme` and returns the style set — or expose the
  `Theme` and build styles at render time. Given the styles are currently
  package-globals, cleanest is a `theme.Styles` struct built once in `New` and
  passed to the components (`worktreelist.New`, `modals.New*`, `view`).
- Config:
  ```yaml
  theme:
    preset: bonsai        # or dracula / nord / mono
    overrides:            # optional per-role hex/256 overrides
      accent: "212"
      danger: "#ff5555"
  ```
  Load preset, apply overrides (`config.go`).

### Scope note
This is mostly mechanical but touches every styled component — do it as one
focused refactor commit (no behavior change) then add config on top. The `refactor`
skill fits the first half.

### Tests
`theme` resolution: preset + overrides → expected role colors; unknown preset
falls back to `bonsai`.

---

## Phase 15 — Desktop notifications

Notify the user when a background process finishes (or fails) and when a PR's CI
rollup changes — useful when bonsai isn't focused.

### Core
- New `internal/core/notify` package: `Notify(title, body string)` dispatching by
  OS —
  - darwin: `osascript -e 'display notification "<body>" with title "<title>"'`
  - linux: `notify-send <title> <body>`
  - windows: PowerShell toast (or skip v1).
  Fire-and-forget; never error the UI (log/swallow).

### Triggers
- **Process finish**: `onProcTick` (`update.go:229`) already polls. Track the last
  seen `Status()` per proc id; on a `running → done/failed` transition, `Notify`
  ("proc #N label — done/failed"). Needs a `seenStatus map[procKey]string` on the
  model (or a `Notified bool` on `exec.Process`, set under its mutex — cleaner,
  keep it in `exec`).
- **CI change** (depends on Phase 7): when a PR's rollup transitions
  (e.g. pending → fail/pass), `Notify`. Compare against the cached rollup before
  overwriting in the `checksMsg` handler.

### Config
```yaml
notifications:
  process: true
  ci: true
```
Default off? Recommend **on for process, off for ci** (ci polling cadence TBD) —
your call; both behind config toggles regardless.

### Tests
Status-transition detector (sequence of statuses → expected notify count). The OS
shell-out itself is manual QA / behind an interface for a fake in tests.

---

## Round 2 — suggested delivery order
1. **Phase 6** (PR detail tab) — the headline; unlocks 7 & 8.
2. **Phase 7** (checks) — small, high signal, feeds 15's CI trigger.
3. **Phase 12** (confirm gate) — do before 8/10/11 so their destructive actions
   are guarded from the start.
4. **Phase 8** (PR actions) — biggest GitHub win, needs 6+12.
5. **Phase 9** (dirty badge + sort) — independent, quick.
6. **Phase 10** (diff tab + update-from-base + fetch) — independent.
7. **Phase 11** (bulk prune) — needs 9a (dirty) + 12 (confirm).
8. **Phase 13** (keybindings) — independent config work.
9. **Phase 14** (themes) — mechanical refactor; do when the style set is stable.
10. **Phase 15** (notifications) — last; process trigger standalone, CI trigger
    needs 7.

Dependencies: 7→6 (tab section), 8→6+12, 11→9a+12, 15(ci)→7. Everything else is
independent and individually shippable.
