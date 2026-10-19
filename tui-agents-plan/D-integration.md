# Track D (part 2): wire the backend, live terminal, terminal mode, launch/accounts/usage, polish

Read first: `00-overview.md` (all decisions) and the design doc `~/bonsai-feat-tui-agents/bonsai-tui-agents-design-v2.md` (sections 2-8; authoritative for look). This track owns `internal/ui/*.go`, `main.go`, `README.md`, `docs/tui-cli.md`, `docs/agents.md` (TUI sections). **One session at a time.**
Branches stacked: `feat/tui-agents-d3` (from `feat/tui-agents` after A3 merged) → `d4` (after B2 merged; rebase) → `d5` (after C2 + C3 merged; rebase) → `d6`. Worktree `~/bonsai-tui-agents-d3` (reuse for later phases with `git switch -c`).

## Facts (verified at 1beaf2b)

- Construction: `ui.New(repoDir, cfg, state, opts ...Option)`, `ui.WithAgents(agentsapi.Backend)` (`internal/ui/agents.go:22`). `main.go:197` calls `ui.New(repoDir, cfg, state)` (no backend: tab hidden). Program options set near `main.go:200` (mouse cell motion).
- Agent state: `agentState` (`agents.go:45-80`): `api, ctx, cancel, sessions, byTree, seq/applied, subscribed, err, ack, selected, usage, usageSeq, usageAccount, usageTickGen, pickIDs, pickPath`. `enabled()` / `active()`. Cancel on quit at `agents.go:85`. `Init` batches `m.initAgents()` (`model.go:334`).
- Tab: `openAgentsTab` (`agentstab.go:180`), `agentsTabKey` (`agentstab.go:192`; `n m u` → "agents: coming soon" at :203), picker `openAgentsPicker`/`onAgentsPick` (:236/:298), selection `selectedSession`/`selectStripIndex` (:98/:120), usage fetch/tick (:50-:178). Palette "coming soon" at `palette.go:288`.
- View: `renderAgentsPane(w,h)` / `agentsRows` (`agentsview.go:88-143`); terminal area today = `placeholderArea(sel, w, body)` (:138), `emptyArea` for no session; `minTerminalRows`; breakpoints `agentsWideMin=100`, `agentsNarrowMin=60`.
- Key precedence (`update.go:208-232`): update prompt (intercepts `u`, `l`, `esc` when visible) → `m.prefs` → `m.modal` → `onKey` (`update.go:756`): list filter → Processes tab keys → **Agents tab keys (:779)** → PR tab → globals (Quit at :804) → `forwardToPane`. Mouse: `update.go:205` → `onMouse`, skipped while modal/prefs open (:244).
- `tea.ExecProcess` usage pattern for shell/editor: `update.go:960,974,2052`. Quit path `procmodal.go:240`.
- Status bar `view.go:910`; full-screen modal render `view.go:689`; `dims()`/`layout()` `view.go:603/627`; resize `onResize` `update.go:261`.
- Backend (A3): `agentsbackend.New(...) (*Backend, error)`, `.API()`, `.Close()`. Stream semantics: `Frames()` closes when the attachment ends; output frames are never dropped; `Writer()`; `Size()`; `Input/Resize/Take/Close`.
- vterm (B1+B2): `New(cols,rows)`, `Write`, `SetViewport(w,h)`, `ResizeEmulator`, `SetFocused`, `SetReplaying`, `View`, `Title`, `AltScreen`, `Close`, `SendKey(tea.KeyMsg) bool`, `SendPaste`, `Input() <-chan []byte`.
- Components (C2/C3): `launchform.Form`, `accounts.Model` (`CloseMsg`, `ExecMsg`, `ReloadMsg`, `Capturing()`), `accounts.Quick` (`OpenAccountsMsg`). `agents.LaunchOptionsFrom(fields, model, values)`; `agentsapi.Defaults.LaunchFields`.

## Key map (decided in part 1)

- `g` global: Agents tab + focus.
- Agents tab, navigation mode (pane focused): `a` all/picker, `n` new agent, `m` accounts, `u` quick usage, `1`-`9` / `[` `]` switch session, `enter` terminal mode, `s` stop (confirm).
- Terminal mode: every key to the agent except `ctrl+]`. After `ctrl+]`: `d` detach (leave terminal mode, give up control, keep showing), `a` picker, `u` usage, `m` accounts, `t` take control, `s` stop (confirm), `esc` back to agent, `ctrl+]` literal 0x1d.
- Bindings in `keys.go` spec table (rebindable, in `?` help): existing `agents_tab`, `agent_all`, `agent_new`, `agent_accounts`, `agent_usage`; add `agent_attach` (enter), `agent_stop` (s).

## Layout spec (design doc sections 3, 5, 6; match these mockups)

Authoritative: `~/bonsai-feat-tui-agents/bonsai-tui-agents-design-v2.md`. The pane is inside the existing right-pane border; tab label `Agents (g)` in the tab strip. "Rule" = full-width dim `─`.

### Navigation mode, wide (≥ 100 cells) — design scenario A/B
```text
│ AGENTS · feature/auth                                                │  title (bold "AGENTS", dim branch)
│                                                                      │  blank
│ Sessions: [1 ✳ Auth fix] [2 ◎ Tests]                 a: All  n: New │  strip
│ ──────────────────────────────────────────────────────────────────── │  rule
│ ✳ Claude · work · Opus  │  ● Running                                 │  header 1
│ 5h 72% left  ·  Week 41% left  ·  Reset 2h 14m            u: Usage │  header 2
│ ──────────────────────────────────────────────────────────────────── │  rule
│  …live agent terminal, every remaining row…                          │
```
- Title + blank rows are dropped first when the terminal area would get < 20 rows; then the rest follows the compact rule. Terminal area always gets every remaining row, never fewer than 5 ("Pane too small").
- Strip overflow (scenario B): `[1 ✳ Auth] [2 ◎ Tests] [3 ✳ API] [+4 more]   a: All`; the selected label is always visible. Selected label rendered reversed/accent; label = number, glyph (provider color), name ≤ 12 cells.
- Header 1: glyph, `Provider · profile · Model`, `│`, state dot + word (starting/stopping yellow, running green, exited dim, failed red). When this client is observing a session another client controls, append dim `· read-only`.

### Navigation mode, compact (60-99 cells) — design section 6
```text
│ [1 ✳ Auth] [+2]                     a: All │
│ ✳ work · 5h 72% · W 41%        u: Usage     │
├─────────────────────────────────────────────┤   rule (join to the pane border with ├ ┤ if the border renderer allows; else plain rule)
│   …terminal…                                │
```
### Narrow (< 60 cells)
`Agents (g)  │  ✳ Auth [1/3]  │  a: Switch` then terminal.

### Empty — design scenario C
```text
│ No running agents                                  a: All  n: New │
│ ─────────────────────────────────────────────────────────────────── │
│                   No active agents                                 │
│           Start an agent in feature/dashboard                      │
│               [n] New agent                                        │
│               [u] View account usage                               │
```
(D2 already renders this; keep it.)

### Terminal mode (this client controls or wants to) — design section 5
The header collapses to two rows so the agent gets more height, and a hint row closes the pane:
```text
│ [1 ✳ Auth] [2 ◎ Tests]                             a: All  n: New │
│ ✳ Claude · work  │  5h 72%  ·  Week 41%           Ctrl+] u: Usage │
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│                     INTERACTIVE AGENT PTY                            │
│                                                                      │
├──────────────────────────────────────────────────────────────────────┤
│ Ctrl+]  →  Bonsai command prefix                                     │
```
- Entering/leaving terminal mode changes the terminal-area height: the writer resizes the PTY to the new area (part of the resize nudge in D4).
- observingOther: hint row reads `Controlled by another client  ·  Ctrl+] t Take control  ·  Ctrl+] d Detach` (yellow).
- Compact width: same two rows but the strip and header use the compact forms.

### Control strip (after `Ctrl+]`) — design section 5
Drawn over the bottom three rows of the terminal area (no PTY resize), accent border-top:
```text
 BONSAI CONTROL
 [d] Detach  [a] Agents  [u] Usage  [m] Accounts  [t] Take control
 [s] Stop    [Esc] Return to agent
```
`[t] Take control` hidden when already the writer. Narrow widths wrap the entries over the same rows.

### Session picker (`a`, `Ctrl+] a`) — design scenario B
```text
╭────────────── AGENTS IN feature/auth ──────────────╮
│                                                    │
│  ● ✳ Fix authentication     Claude · work          │
│  ● ◎ Run integration tests  Codex · personal       │
│  ✓ ◎ Previous attempt       Codex · work           │
│                                                    │
│  / Search    Enter Open    n New    Esc Close      │
╰────────────────────────────────────────────────────╯
```
Flat list (no section headers): running first (newest first), then ended newest first. Marks `●` running (green), `✓` exited (dim), `✗` failed (red); name column aligned; `Provider · profile` dim. Footer hints as shown; `n` opens the new-agent dialog. Opens even with zero sessions (body `No agents in this worktree yet`), so `n` is always one key away.

### Global status bar
On the Agents tab in navigation mode the bottom bar shows `tab focus · g agents · ctrl+] menu · ? keys`; in terminal mode `Ctrl+] Bonsai command prefix`.

## D3: wire the real backend + live read-only view

### Step 1 [you]: `main.go`
- `backend, err := agentsbackend.New()`; error → run the TUI without agents and print one stderr line after exit (`agents unavailable: <err>`); the TUI must always start.
- `ui.New(repoDir, cfg, state, ui.WithAgents(backend.API()))`; `defer backend.Close()` after `p.Run()` returns. Never stops holders.
- `main_test.go` (or the existing main test file): `newTUIModel` builds with a fake backend; no real home (`t.Setenv("HOME", t.TempDir())`).

### Step 2 [you]: attach state machine `internal/ui/agentattach.go`
```go
type attachState int // attachNone, attachObserving, attachControlling, attachObservingOther, attachEnded
type agentView struct {
    sessionID  string
    token      uint64          // bumps on every new stream; stale msgs compare it
    stream     agentsapi.Stream
    term       *vterm.Model
    state      attachState
    replaying  bool            // until first status frame after ready
    offset     uint64
    generation string
    banner     string          // one-line notice over the bottom row
    err        string
}
```
- One live view at a time (`m.agents.view *agentView`). Show = tab visible and a session selected: `Attach(ctx, id, {ReadOnly:true})` in a `tea.Cmd` → `agentAttachedMsg{token, stream, err}`. Create `vterm.New(stream.Size())`, `SetViewport(paneW, bodyRows)`, `SetReplaying(true)`.
- Pump (no goroutines in the model): `waitFramesCmd(token, stream)` blocks for one frame, then drains what is immediately available (cap 256 frames or 4 MiB) → `agentFramesMsg{token, frames, closed bool}`. Handler: `output` → `term.Write`, track `offset`/`generation`; first `status` after `ready` → `replaying=false`, `term.SetReplaying(false)`; `gap` → recreate the emulator at the same size; `writer` → update writer flag; `size` (if A3 added it) or size change → `term.ResizeEmulator`; terminal-state `status` → `attachEnded`, banner `exited (code N) · n: new · a: all` or `failed: <error>`. Re-issue the wait unless closed. Messages with an old token are ignored.
- Detach triggers: worktree selection change, session switch (`1-9`, `[` `]`, picker), leaving the Agents tab, quit. Close the stream in a `tea.Cmd` (never block Update), clear the view, re-attach for the new selection if still on the tab.
- Errors: `agenthost.ErrVersion` (surface via `errors.Is` on a sentinel the backend re-exports, not by importing agenthost into ui) → `Started by another bonsai version; run bonsai agents attach <name>`; dial failure → retry once after 500 ms, then error row + `enter: retry`.
- View: `agentsRows` replaces `placeholderArea` with `term.View()` (exactly `body` rows × `w`), banner drawn over the last row when set. Observing: emulator stays at holder size, vterm crops (cursor row visible).
- Quit: close the view's stream and `m.agents.cancel()` (already cancels list/watch).

### Step 2b [you]: match the D2 tab to the layout spec
D2 differs from the design in a few places (`agentsview.go:109-131`, `agentstab.go:236-296`):
- Wide layout: add the `AGENTS · <branch>` title row + blank row on top (dropped first when height is short), and a rule between the strip and header 1 (design scenario A/B). Header 1 gains `· read-only` while observing a session another client controls.
- Picker: replace the `Running`/`Recent` sections with the flat list, aligned columns and footer `/ Search    Enter Open    n New    Esc Close` (check whether `modals.NewSectionedFuzzy` supports a custom footer and a single unnamed section; if not, add a small option to `modals`, not a new picker). Open it with zero sessions too (`No agents in this worktree yet`). `n` inside → "coming soon" until D5.
- Global status bar on the Agents tab: `tab focus · g agents · ctrl+] menu · ? keys` (`view.go:910`).
- Update `agentstab_test.go` assertions that encode the old layout (sections, missing title), with reasons in the commit body.

### Step 3 [builder]: tests (fake backend)
Layout rows per the spec at widths 120/80/50 (title present/absent by height, rule after strip, compact and narrow forms); picker flat order + marks + footer + empty case;
Attach issued with `ReadOnly` on tab open and on session switch; stale frames after switch ignored (token); replay flag cleared on first status; gap recreates emulator; ended banner; error + retry; leaving tab closes stream (`fake.Stream.Closed()`); quit closes everything. `ansi.Strip(View())` contains pushed output.

### Step 4 [you]: integration test `internal/ui/agents_integration_test.go` (`//go:build !windows`)
Real `agentsbackend` with injected in-process launcher, temp `Paths` and temp stores (A3 must expose these as options; see note in A3 step 5), fake provider running a shell script that prints a known screen then echoes stdin. Start via backend, open tab in a test model, drive `Update` with the pumped messages until the screen text appears. `-race`.

### D3 done when
`go test -race ./internal/ui/... ./internal/agentsbackend/...`, global checks. Manual: real binary shows web-launched sessions live, correct colors and box drawing (acceptance 1, 2). reviewer (ask: no stream leak on switch; Update never blocks; nothing ever sent to a holder in this phase). Docs (docs-writer): `docs/tui-cli.md` Agents tab now shows live terminals read-only. Commits: `feat(agents): wire the agent backend into the tui`, `feat(ui): match the agents tab layout to the design`, `feat(ui): show live agent terminals read-only in the agents tab`.

## D4: terminal mode and the `ctrl+]` prefix (needs B2)

### Step 5 [you]: enter / take / leave
- Terminal-mode layout per the spec: two-row header (strip without `Sessions:` + combined `glyph Provider · profile │ 5h 72% · Week 41%   Ctrl+] u: Usage`), rule, terminal, rule, hint row `Ctrl+]  →  Bonsai command prefix` (observingOther: yellow `Controlled by another client  ·  Ctrl+] t Take control  ·  Ctrl+] d Detach`). `cols/rows` below = the terminal area in this layout.
- `enter` (navigation mode, session selected, not ended): close the RO stream, `Attach(id, {Offset, Generation})` (auto). `Writer()` true → `attachControlling`: `Resize(cols, rows-1)` then `Resize(cols, rows)` (forces a full redraw, same as `bonsai agents attach`), `term.ResizeEmulator(cols, rows)`, `term.SetFocused(true)`. False → `attachObservingOther`, banner `Controlled by another client · Ctrl+] t to take control`; keys swallowed (one-time hint).
- `Ctrl+] t` → `stream.Take()`; on `writer=true` frame proceed as controlling (resize nudge).
- Writer lost while controlling → `attachObservingOther`, hint row `Control taken by another client  ·  Ctrl+] t Take back`, `term.SetFocused(false)`.
- `Ctrl+] d` → leave terminal mode: close stream, re-attach RO at current offset, keep showing.
- Opening an overlay (`a`, `u`, `m`, stop confirm) from terminal mode leaves terminal mode but **keeps the stream attached and rendering** underneath (never interrupt the agent). Returning requires `enter` again.
- Input pump: `waitInputCmd(token, term)` reads `term.Input()` → `agentInputMsg{token, data}` → `stream.Input(data)` only if `state == attachControlling && !replaying`; else drop. Re-issue the wait.
- `tea.WindowSizeMsg` while controlling: debounce 100 ms (tick + compare size), then `Resize(paneCols, paneRows)` + `ResizeEmulator`. Observing: `SetViewport` only.

### Step 6 [you]: key routing (`update.go`)
At the top of `case tea.KeyMsg`, before the update-prompt check: if `m.prefs == nil && m.modal == nil && no agent overlay && m.agentTerminalMode()` (controlling or observingOther):
- prefix pending → dispatch `d a u m t s esc ctrl+]`; anything else closes the strip and is swallowed.
- `tea.KeyCtrlCloseBracket` → prefix pending; draw the `BONSAI CONTROL` strip from the layout spec over the bottom three terminal rows (no PTY resize; `[t]` hidden when writer).
- else `msg.Paste` → `term.SendPaste`; otherwise `term.SendKey(msg)`. Never quit on `ctrl+c` here; never let `q`/`?`/`g` reach globals.
- Mouse in terminal mode: ignore events inside the terminal area (comment why: the TUI owns the mouse; agents get no mouse in this project).
- Status bar in terminal mode: `Ctrl+] Bonsai command prefix` replaces the normal hints; observingOther: `Ctrl+] t take control · Ctrl+] d detach`.

### Step 7 [builder]: tests
Fake stream: in terminal mode `q`, `ctrl+c`, `esc`, `tab`, arrows produce `fake.Stream.Inputs()` bytes and never quit/switch tabs; navigation mode sends nothing; observingOther sends nothing; `ctrl+] d` closes the writer stream and re-attaches RO with the last offset; `ctrl+] ctrl+]` sends `0x1d`; `ctrl+] t` calls `Take()`; resize nudge recorded (`Resizes()` = `[(c,r-1),(c,r)]`); writer lost → banner; replay-time replies never reach `Inputs()`; debounced resize.

### D4 done when
Checks as D3; integration test extended: enter terminal mode, send keys, assert the script echoed them; `ctrl+] d` leaves terminal mode and holder still runs. Manual acceptance 3-6. reviewer (ask: no key leaks to the agent in navigation mode; no input while observing; prefix cannot be bypassed; no goroutine leak on stream switch). Docs: `docs/tui-cli.md` terminal mode + prefix table. Commits: `feat(ui): control agent terminals from the agents tab`, `feat(ui): route terminal keys with a ctrl+] control prefix`.

## D5: launch, accounts, usage, stop (needs C2, C3)

### Step 8 [you]: agent overlays
- `m.agents.overlay` holds at most one of: launch dialog, `accounts.Model`, `accounts.Quick`. Rendered full-screen like `m.modal` (`view.go:689`); owns keys (check next to `m.modal` in `update.go:227`); the live stream keeps pumping underneath.
- `accounts.ExecMsg` → `tea.ExecProcess(cmd, ...)` (pattern `update.go:960`), on return send `accounts.ReloadMsg` to the overlay and re-list sessions.
- `accounts.Quick` → `OpenAccountsMsg` swaps to `accounts.New(..., {Initial: id, Tab: TabUsage})`.

### Step 9 [you]: new agent dialog (`internal/ui/agentlaunch.go`)
The design has no mockup for it; it reuses the accounts modal's look so the two feel like one system (C2 `accounts.Frame`, `accounts.Sidebar`, `launchform`):
```text
╭─ NEW AGENT · feature/auth ─────────────────────────────────────────────╮
│                                                                       │
│ ACCOUNTS               │ ✳ Claude · work                              │
│                        │ 5h 72% left  ·  Week 41% left                │
│ CLAUDE                 │ ──────────────────────────────────────────── │
│ > ✳ work      72%  ●   │                                              │
│   ✳ personal  15%  ●   │ Name                                         │
│                        │ > Auth fix                                   │
│ CODEX                  │                                              │
│   ◎ work      41%  ●   │ Model                                        │
│   ◎ personal  busy ●   │ > Opus (default)                       [▼] │
│                        │                                              │
│ ANTIGRAVITY            │ Effort                                       │
│   ✦ personal  —    ●   │ > High                                 [▼] │
│                        │                                              │
│                        │ Permissions                                  │
│                        │ > Default                              [▼] │
│                        │                                              │
│                        │ [ ] Save as defaults for this account        │
│                        │                                              │
│                        │                         [Cancel]  [Start]    │
├────────────────────────┴──────────────────────────────────────────────┤
│ j/k Account  ·  Tab Field  ·  Enter Edit  ·  ctrl+s Start  ·  Esc Close │
╰───────────────────────────────────────────────────────────────────────╯
```
- Sidebar = `accounts.Sidebar` (usage summary from the tab's usage cache, fetch missing ones); `busy` replaces the percentage when the provider forbids concurrent same-account use and `Running > 0` (admission still decides). Accounts that need sign-in are listed but `[Start]` is replaced by `[Sign in first]` → `ExecMsg{LoginCommand}`.
- Right: account header + one-line usage, rule, `Name` (text input, default = profile name), then `launchform.New(d.LaunchFields, d.Models, d.Values, ascii)` from that account's `Defaults` (changes apply to this launch only), the checkbox, buttons. Switching account reloads the form.
- "Save as defaults" → `UpdateDefaults(id, form.Changes())` after a successful start.
- Start → `Sessions.Start({ProjectID: config.ProjectID(mainRoot), WorktreeID: local.ID(mainRoot, wt.Path), WorkDir: wt.Path, AccountID, Name, Cols/Rows = current terminal area, Launch: agents.LaunchOptionsFrom(d.LaunchFields, model, values)})`, exactly the web's identities. Success → close dialog, select session, enter terminal mode (controlling). Errors (`ErrAccountBusy`, `ErrProviderBusy`, limit, validation text) inline in the dialog; dialog stays open.

### Step 10 [you]: wire keys and palette
- Agents tab `n` → launch dialog; `m` → accounts (`Initial` = selected session's account); `u` → quick usage (focus = selected session's account, else first); `s` / `Ctrl+] s` → `modals` confirm `Stop <name>? The agent process ends.` → `Sessions.Stop` → re-list.
- `Ctrl+] a/u/m/s` open the same overlays (leave terminal mode first, stream stays).
- Palette (`palette.go:288`): replace "coming soon" with real actions "Agents: open tab", "Agents: new agent", "Agents: accounts", "Agents: usage".
- Replace the D2 "coming soon" tests in `agentstab_test.go` (:504-:540) with tests for the real overlays.

### Step 11 [builder]: tests (fake backend)
Launch dialog: account grouping, busy hint, start request fields (project/worktree IDs, cols/rows, launch options from form), error stays inline, success selects + enters terminal mode, save-as-defaults sends `Changes()`. Overlay opens while controlling keeps the stream (not closed) and leaves terminal mode. ExecMsg path returns `tea.ExecProcess` cmd and reload follows. Stop confirm calls `Stop` once.

### D5 done when
Checks as D3. Manual acceptance 7-10. reviewer (ask: identities identical to web; secrets never rendered; overlay never interrupts the stream). Docs: `docs/tui-cli.md` (new agent, accounts, usage, stop), `docs/agents.md` (TUI as a client: same sessions as web, read-only by default, take control, origin/keep_running), `README.md` one feature line + link. Commits: `feat(ui): launch agents from the tui`, `feat(ui): open accounts and usage from the agents tab`, `docs: document the tui agents tab`.

## D6: polish + docs audit

- `!` acknowledgement on picker open/selection; `?` help lists agent keys in their own section; context hint per state (navigation / terminal / observingOther / ended).
- Theme check in every built-in theme (glyph and state colors readable on light themes); ASCII pref end to end (glyphs, bars, dots, strip).
- `render_test.go` goldens, one per layout-spec mockup, with a fake live session: wide navigation 160×40 (scenario A), strip overflow (B), empty (C), terminal mode, observingOther hint row, control strip open, compact 80×24, narrow 58×20, picker. Compare against the design doc side by side; any deliberate deviation gets a one-line note in the PR body.
- docs-writer audit: every key, command and path cited in `README.md`, `docs/tui-cli.md`, `docs/agents.md` exists.
- Commit `fix(ui): polish agents tab states and help` (+ `docs: ...` if the audit changes anything).
- Then: PR `feat/tui-agents` → `main` with label `release:minor`.

## Manual acceptance (real providers, scratch worktree, `bonsai web` open in a browser)

1. Launch Claude in the web for worktree X → TUI glyph `✳` on X within 1 s, no restart; add Codex → `✳ ◎`.
2. `g` on X → Agents tab shows the session read-only with correct colors/box drawing; browser keeps control.
3. `enter` → banner "Controlled by another client"; `Ctrl+] t` → TUI controls, browser shows observer state; typing, arrows, `esc` (interrupt), `ctrl+c`, tab completion, paste work in Claude Code and Codex.
4. Resize the terminal while controlling → agent redraws to the new size, no garbage.
5. Switch worktree and back → same session re-shown read-only; nothing typed into the wrong agent.
6. `Ctrl+] d`, quit the TUI → agent still running in the browser.
7. `n` in an empty worktree → launch Codex with non-default effort → appears in the browser live (A2).
8. `m` → Overview/Usage/Defaults for each provider; change a default, save, verify with `bonsai agent account show`.
9. `m` → Sign in again → TUI suspends, CLI login runs, TUI resumes with refreshed status. `+ Add account` likewise.
10. `bonsai web stop` with `keep_running=false` → web-launched agents stop, TUI-launched one keeps running.
