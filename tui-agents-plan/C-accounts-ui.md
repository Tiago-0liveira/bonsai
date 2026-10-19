# Track C (part 2): launch form, accounts modal, quick usage

Read first: `00-overview.md` (decisions 2, 5) and design doc sections 4-5 (`~/bonsai-feat-tui-agents/bonsai-tui-agents-design-v2.md`).
Branches `feat/tui-agents-c2` (from `feat/tui-agents`) → `feat/tui-agents-c3` (stacked). Worktrees `~/bonsai-tui-agents-c2`, `~/bonsai-tui-agents-c3`. Runs in parallel with A2/A3 and B2.
**Must not touch** `internal/ui/*.go` (model/update/view/keys): D5 wires `n m u`. Each component ships with tests and a tiny harness so it is reviewable before wiring.

## Facts (verified at 1beaf2b)

- Contract: `internal/ui/agentsapi/agentsapi.go`. `Accounts{List, Describe, Usage, Defaults, UpdateDefaults, Remove, AddCommand, LoginCommand}`; `Account{agents.Account, Info, Running}`; `Usage{Snapshot, Headline, FetchedAt, Stale, Err}`; `Defaults{Fields, Models, Values}` (Fields = provider `ProfileFields()`). Fake: `agentsapi/fake/fake.go` (`Accounts` with `UsageCalls()`, `Updates()`).
- **Contract gap:** the launch dialog needs the provider's per-launch fields (`agents.LaunchSchema.LaunchFields()`, `internal/core/agents/provider.go:272`; served to the web at `localapi/agents.go:133`). `Defaults` only carries profile fields. Profile fields are derived from launch fields (`agents.ProfileFieldsFrom`, used by every provider's `profile.go`).
- `agents.LaunchField{ID, Label, Kind (select|toggle|model), Options []FieldOption, Default, PerModel, Help}` (`provider.go:237`); `FieldOption{Value, Label, Tone, Help, Set map[string]string}` (`Set` = preset that sends several request fields, e.g. Codex "Auto" sets sandbox + approval). `ModelOption{ID, Label, Description, Source, Efforts, DefaultEffort, NoEffort, Default}` (`provider.go:285`).
- `agents.LaunchOptions{Model, Prompt, DisplayName, FullAccess *bool, PermissionMode, Effort, Sandbox, Approval}` + `Values()` (`provider.go:121-136`). No inverse helper exists.
- `usagefmt` (`internal/core/agents/usagefmt/usagefmt.go`): `Bar(fraction, width)`, `Percent(fraction) int`, `Duration(d, precise)`, `ResetsIn(now, *t, precise)`, `Rank`/`RankOf(fraction)`, `WindowName(limit, fallback)`, `Headline(h, now)`, `Compact(h)`.
- Theme: `theme.ProviderGlyph(p, ascii)`, `theme.ProviderStyle(p)`, `theme.Current.{Dim,...}` (`internal/ui/theme/agents.go`). Existing modal look and text input: `internal/ui/components/modals` (`KindConfirm`, text-input kinds), reuse its styles for borders/titles so the new modals match.
- Web references (structure, not code): `web/src/features/agents/ProfileOverview.tsx`, `ProfileUsage.tsx`, `UsageMeter.tsx`, `ProfileDefaults.tsx`, `profileForm.ts` (defaults diff, presets), `AddProfileDialog.tsx`, `RemoveProfileDialog.tsx`, `SummaryStrip.tsx`; launch dialog `web/src/features/workspace/StartAgentDialog.tsx` (how field values + presets become start request fields).

## UX spec

Authoritative source: `~/bonsai-feat-tui-agents/bonsai-tui-agents-design-v2.md` section 4 (Accounts modal) and section 5 (`m`, `u`). Match its mockups cell for cell where width allows; they are copied here so this file stands alone.

### Shared frame (accounts modal, quick usage, D5's new-agent dialog)
- Rounded border, CAPS title in the top border (`╭─ AGENT ACCOUNTS ─…╮`), one blank row under the top border.
- Two columns split by `│`; the footer is a full-width row separated by `├────┴────┤` (the split's `│` joins the separator with `┴`).
- Footer = key hints joined by `  ·  `, capitalized verbs (`j/k Navigate  ·  Tab Section  ·  Enter Select  ·  Esc Close`).
- Size: screen minus a 2-cell margin each side, capped at 100×32; left column 24 cells (26+ when it shows usage).
- Build the frame once as an internal helper in `components/accounts` (exported `Frame(title string, left, right []string, footer string, w, h int) string`) so D5's dialog reuses it.

### Accounts modal: Overview
```text
╭─ AGENT ACCOUNTS ───────────────────────────────────────────────────────╮
│                                                                       │
│ ACCOUNTS               │ ✳ Claude · work                              │
│                        │                                              │
│ CLAUDE                 │ [Overview]  Usage  Defaults                 │
│ > ✳ work           ●   │ ──────────────────────────────────────────── │
│   ✳ personal       ●   │                                              │
│                        │ Status       Connected                       │
│ CODEX                  │ Identity     work@example.com                │
│   ◎ work           ●   │ Provider     Claude Code                     │
│   ◎ personal       ●   │ Plan         Max                             │
│                        │ Auth         Browser login                   │
│ ANTIGRAVITY            │                                              │
│   ✦ personal       ●   │ Default model    Opus                        │
│                        │ Default effort   High                        │
│                        │                                              │
│                        │ Sessions         2 running                   │
│                        │                                              │
│                        │ [Sign in again]  [Remove account]           │
│                        │                                              │
│ + Add account          │                                              │
├────────────────────────┴──────────────────────────────────────────────┤
│ j/k Navigate  ·  Tab Section  ·  Enter Select  ·  Esc Close          │
╰───────────────────────────────────────────────────────────────────────╯
```
- Left column ("sidebar"): `ACCOUNTS` heading, blank, then provider groups `CLAUDE / CODEX / ANTIGRAVITY` (blank row between groups), rows `> ✳ work … ●`: `>` marks the selected account, glyph in provider color, name, auth dot right-aligned (green connected, yellow needs sign-in, dim unknown/not described yet). `+ Add account` pinned to the bottom of the column. When the column is ≥ 26 cells, a short usage summary sits before the dot (`✳ work   72%  ●`, colored by Rank).
- Right header: `<glyph> <Provider> · <account>`, blank, tab row (active tab bracketed `[Overview]`), full-width `─` rule, blank.
- Overview rows, label column aligned: `Status` (`Connected` / `Needs sign-in` / `Unknown`), `Identity`, `Provider` (provider display name, e.g. "Claude Code"), `Plan`, `Auth` (e.g. "Browser login"); blank; `Default model`, `Default effort`; blank; `Sessions  N running`; blank; buttons `[Sign in again]  [Remove account]` (focused button reversed).
- Footer: `j/k Navigate  ·  Tab Section  ·  Enter Select  ·  Esc Close`.

### Accounts modal: Usage
```text
│                        │ Overview  [Usage]  Defaults                 │
│ > ✳ work           ●   │ ──────────────────────────────────────────── │
│   ✳ personal       ●   │                                              │
│                        │ 5-hour window                                │
│ CODEX                  │ ███████░░░  72% remaining                     │
│   ◎ work           ●   │ Resets in 2h 14m                             │
│   ◎ personal       ●   │                                              │
│                        │ Weekly window                                │
│ ANTIGRAVITY            │ ████░░░░░░  41% remaining                     │
│   ✦ personal       ●   │ Resets in 3d 8h                              │
│                        │                                              │
│                        │ Additional limits                            │
│                        │ Sonnet weekly    82% remaining               │
│                        │ Credits          $12.50                      │
│                        │                                              │
│                        │ Updated 35 seconds ago                       │
│                        │                                              │
│ + Add account          │ [r] Refresh usage                            │
├────────────────────────┴──────────────────────────────────────────────┤
│ j/k Account  ·  Tab Section  ·  r Refresh  ·  Esc Close               │
```
- Each window: name line (`usagefmt.WindowName`, "5-hour window" / "Weekly window"), meter line (`usagefmt.Bar` 10 cells colored by Rank + `NN% remaining`), `Resets in …` line, blank.
- `Additional limits`: label-aligned rows (`Sonnet weekly    82% remaining`, `Credits          $12.50`). Antigravity: model pools as individual rows grouped by `Group` instead of the two shared meters (design: "displayed as individual model pools").
- `Updated 35 seconds ago` (`FetchedAt`); stale → whole block dimmed + `Showing last good data · <err>`; `[r] Refresh usage` at the bottom of the column. Loading → `Loading usage…`.
- Footer: `j/k Account  ·  Tab Section  ·  r Refresh  ·  Esc Close`.

### Accounts modal: Defaults
```text
│                        │ Overview  Usage  [Defaults]                 │
│   ✳ work           ●   │ ──────────────────────────────────────────── │
│   ✳ personal       ●   │                                              │
│                        │ Model                                        │
│ CODEX                  │ > gpt-5.5                              [▼] │
│   ◎ work           ●   │                                              │
│ > ◎ personal       ●   │ Reasoning effort                             │
│                        │ > High                                 [▼] │
│ ANTIGRAVITY            │                                              │
│   ✦ personal       ●   │ Permissions                                  │
│                        │ > Auto                                 [▼] │
│                        │                                              │
│                        │ Other provider-specific options              │
│                        │ ...                                          │
│                        │                                              │
│                        │ These defaults apply to new sessions.       │
│                        │ Running agents are not affected.            │
│                        │                                              │
│ + Add account          │               [Reset]  [Save defaults]      │
├────────────────────────┴──────────────────────────────────────────────┤
│ Tab Section  ·  Enter Edit  ·  Esc Back                               │
```
- Each field = label line + value line `> <value>` with `[▼]` right-aligned (select/model); toggles render `> On` / `> Off` with `[space]` hint instead of `[▼]`. Focused field: label in accent color + value reversed. Blank row between fields. Fields beyond the provider's first three under an `Other provider-specific options` sub-heading (scrolls when it does not fit).
- `enter` opens the dropdown inline under the value (bordered list, `>` on the highlighted option, danger options in error color with their `Help` line).
- Note text, then `[Reset]  [Save defaults]` right-aligned at the bottom. Unsaved changes add `● unsaved` to the tab row.
- Footer: `Tab Section  ·  Enter Edit  ·  Esc Back` (`Esc Back` leaves edit/dropdown first, then closes; with unsaved changes asks to discard).

### Quick usage overlay (`u`; design section 5 names it, no mockup)
Same frame, single column, title `USAGE`, ≤ 64 wide:
```text
╭─ USAGE ──────────────────────────────────────────────────╮
│                                                          │
│ ✳ Claude · work                                          │
│ 5-hour   ███████░░░  72% remaining · Resets in 2h 14m    │
│ Weekly   ████░░░░░░  41% remaining · Resets in 3d 8h     │
│ ──────────────────────────────────────────────────────── │
│ ✳ personal     5h 15% · W 63%      reset 4h 02m          │
│ ◎ work         5h 91% · W 66%      reset 1h 10m          │
│ ✦ personal     Gemini Pro 12% · Flash 80%                │
│                                                          │
│ Updated 35 seconds ago                                   │
├──────────────────────────────────────────────────────────┤
│ j/k Account  ·  r Refresh  ·  m Accounts  ·  Esc Close   │
╰──────────────────────────────────────────────────────────╯
```
Focused account (selected session's) on top with two meters; others one line each (`usagefmt.Compact`), `RankLow` highlighted, stale dimmed.

### ASCII pref (`agent_glyphs: ascii`)
Glyphs `C X A`, bars `#` / `.`, dots `*` / `o`, `[▼]` → `[v]`, `✓`/`✗` → `+`/`x`, border stays (lipgloss already handles it).

## C2: launch form + accounts modal

### Step 0 [you]: contract + launch options helper (first commit, small, merge-blocking for A3)
- `agentsapi.Defaults` gains `LaunchFields []agents.LaunchField // provider LaunchFields; the new agent dialog edits these`. Update `fake` to fill it (reuse its existing field fixtures). Mention in the PR that A3 fills it from `LaunchSchema`.
- `internal/core/agents/launch_values.go`: `func LaunchOptionsFrom(fields []LaunchField, model string, values map[string]string) LaunchOptions`: expand `FieldOption.Set` presets exactly as the web's `StartAgentDialog.tsx`/`profileForm.ts` do, then map ids (`effort`, `permission_mode`, `sandbox`, `approval`, `full_access`) to struct fields. Table test: round trip with `LaunchOptions.Values()` for each provider's real `LaunchFields()` (providers imported in `_test.go` only), and Codex preset expansion.
- Commit `feat(agents): expose launch fields to the tui and build launch options from field values`.

### Step 1 [you]: `internal/ui/components/launchform`
Generic field editor, reused by the Defaults tab (profile fields) and D5's new-agent dialog (launch fields).
```go
type Form struct { /* fields, models, initial, values map[string]string, cursor int, open bool (inline list), ascii bool */ }
func New(fields []agents.LaunchField, models []agents.ModelOption, values map[string]string, ascii bool) Form
func (f Form) Update(tea.Msg) (Form, tea.Cmd)
func (f Form) View(w, h int) string                 // exactly h lines ≤ w cells
func (f Form) Values() map[string]string           // current (model under key "model")
func (f Form) Changes() map[string]*string         // diff vs initial; nil = reset to provider default
func (f Form) Editing() bool                       // an inline list is open: parent must not treat esc as close
```
- Look = the Defaults mockup in the UX spec: label line, `> value … [▼]` line, blank row; focused field accent label + reversed value; inline bordered dropdown on `enter`; fields after the first three under `Other provider-specific options`; vertical scroll keeps the focused field visible.
- Kinds: `model` → dropdown from `models` (label + description dim; `Default` marked `(default)`), `select` → `enter` opens the dropdown (`j/k`, `enter` pick, `esc` cancel), `left/right` cycles without opening; `toggle` → `space`/`enter`, value `On`/`Off`. `Tone: danger` options rendered in the theme's error color with their `Help` below when selected.
- `PerModel`: options come from the selected model's `Efforts` (fallback `Options`); hide the field when the model has `NoEffort`; changing model re-validates the value (fallback `DefaultEffort`).
- Unknown kind: read-only row with its value (a provider adding a field never breaks the TUI).
- `r` resets focused field (`Changes()` → nil for it). `j/k`/`up/down` move between fields.
- Tests: fields from the three real providers (`_test.go` imports allowed; if a layering lint objects, hand-build equivalents): key sequences → `Values()`/`Changes()`; PerModel switch; danger help; view width/height invariants at w=40/80.

### Step 2 [you]: `internal/ui/components/accounts`
```go
type Tab int // TabOverview, TabUsage, TabDefaults
type Options struct{ ASCII bool; Initial agents.AccountID; Tab Tab }
type Model struct { ... }
func New(api agentsapi.Accounts, opts Options) Model
func (m Model) Init() tea.Cmd                     // List, then Describe + Usage(false) + Defaults for selected
func (m Model) Update(tea.Msg) (Model, tea.Cmd)
func (m Model) View() string
func (m *Model) SetSize(w, h int)
func (m Model) Capturing() bool                   // text input or form edit active: parent sends every key here
// Messages the parent handles:
type CloseMsg struct{}
type ExecMsg struct{ Cmd *exec.Cmd }             // parent runs tea.ExecProcess, then sends ReloadMsg
type ReloadMsg struct{}                           // parent → modal after ExecProcess returns
```
- Layout: exactly the three mockups in the UX spec (frame, sidebar, right header + tab row + rule, per-tab footer text). Sidebar is its own exported piece (`Sidebar{Accounts, Selected, Usage, ASCII}` with `View(w, h)`), reused by D5's new-agent dialog so both modals list accounts identically.
- Focus: `j/k` moves the account selection; `tab`/`shift+tab` cycles Overview → Usage → Defaults (also `1/2/3`); inside the right column `↓/↑` (or `tab` past the last section, as in the mockup footers) moves between buttons/fields. `enter` activates (buttons, `+ Add account`, field edit). `r` refresh usage (Usage) or reset field (Defaults), `ctrl+s` save defaults, `esc` close (leave dropdown / sub-dialog first; unsaved defaults → confirm discard).
- Overview rows per mockup: `Status` (`Connected` / `Needs sign-in` / `Unknown` from `Info`), `Identity`, `Provider` (display name), `Plan`, `Auth` (from `Describe`, async with `Loading…` in the value column; error → `Unavailable` + sanitized text), `Default model`/`Default effort` (from `Defaults.Values` + model label), `Sessions  N running` (`Account.Running`). `[Sign in again]` → `ExecMsg{LoginCommand(id)}`. `[Remove account]` → small confirm dialog in the same frame style (`REMOVE ACCOUNT` title, `Remove ✳ work? Its Bonsai config dir is deleted.`, `[Cancel]  [Remove]`); blocked with an inline message when `Running > 0`.
- `+ Add account`: right column switches to an `ADD ACCOUNT` form: provider choice (three rows with glyphs, `>` marker) → `Name` input (non-empty, no `/`, unique per provider) → `[Continue in terminal]` → `ExecMsg{AddCommand(provider, name)}`. Footer `j/k Provider  ·  Enter Continue  ·  Esc Back`. (Design wants this through the web's setup-job terminal "eventually"; this project uses the CLI flow via `tea.ExecProcess`, decision 5.)
- Usage: `Usage(id,false)` on select; `r` → `Usage(id,true)`. Layout per Usage mockup; Rank → theme colors (ok green / warn yellow / low red).
- Defaults: embeds `launchform.New(d.Fields, d.Models, d.Values, ascii)`; `[Save defaults]`/`ctrl+s` → `UpdateDefaults(id, form.Changes())`, success → `Saved` replaces `● unsaved` for 3 s, reload `Defaults`; error inline above the buttons. `[Reset]` resets every field to the provider default (Changes → nil for all).
- Every backend call is a `tea.Cmd` with `context.WithTimeout(15s)`; results carry `(accountID, seq)`; a result for an account no longer selected, or older than the latest request, is dropped (state ordering invariant). On `ReloadMsg` re-list and keep the selection by id.
- Never render tokens, env values or config dir paths. Identity = what `Describe` returns.

### Step 3 [builder]: tests
With `agentsapi/fake`: navigation across provider groups; section switching; footer text per tab matches the mockups; sidebar usage column appears only at ≥ 26 cells; describe arriving after selection change ignored; usage refresh error → stale render keeps old numbers; defaults save sends exactly changed keys (`fake.Updates()`); remove blocked while running; add flow emits `ExecMsg` with argv `agent account add <provider> <name>`; login emits `ExecMsg`; `ReloadMsg` keeps selection. Substring asserts with `ansi.Strip`; goldens `testdata/accounts-{overview,usage,defaults}-100x30.golden` (fake data matching the mockups) behind `-update` (copy the `internal/ui/websetup` golden flag pattern).

### C2 done when
`go test -race ./internal/ui/components/accounts/... ./internal/ui/components/launchform/... ./internal/core/agents/...`, global checks. reviewer (ask: secrets never rendered; late-result ordering; esc never discards unsaved defaults silently). Commits: step 0 commit, `feat(ui): add generic launch field form`, `feat(ui): add agent accounts modal`.

## C3: quick usage overlay

### Step 4 [you]: `internal/ui/components/accounts/quick.go`
```go
func NewQuick(api agentsapi.Accounts, focus agents.AccountID, ascii bool) Quick
func (q Quick) Init() tea.Cmd   // List + Usage(false) for every account, ≤ 3 in flight
func (q Quick) Update(tea.Msg) (Quick, tea.Cmd)
func (q Quick) View() string
func (q *Quick) SetSize(w, h int)
type OpenAccountsMsg struct{ Account agents.AccountID } // parent opens the accounts modal there
```
- Layout per the `USAGE` mockup (shared frame, single column); others sorted provider then name; `RankLow` rows highlighted; stale rows dimmed with `stale`; Antigravity rows show its pools compactly. Golden `testdata/quick-usage-64x16.golden`.
- Keys: `r` refresh all (`Usage(true)`, ≤ 3 concurrent), `m` → `OpenAccountsMsg{focused}`, `j/k` move focus, `esc`/`u` → `CloseMsg`.
- Tests with fake: ordering, concurrency cap (fake blocks; count in-flight), late results per account, stale dimming, `m` message.

### C3 done when
Same checks. Commit `feat(ui): add quick agent usage overlay`.

## Only touch
`internal/ui/agentsapi/**` (step 0 only), `internal/core/agents/launch_values*.go`, new `internal/ui/components/launchform/**`, new `internal/ui/components/accounts/**`.
