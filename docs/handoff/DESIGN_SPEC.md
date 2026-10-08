# Bonsai web client: design spec (approved mockups D through L)

Source of truth for values. Where `board-L.html` and this file differ, the board wins; tell the planner so it can fix the spec. The mockups use inline hex values; the spec below names them as semantic tokens.

## 1. Proposed theme tokens (colors only)

Stored in CSS as space-separated RGB triplets and used as `rgb(var(--token) / alpha)`. Tints and glows are always the same token at a lower alpha, never separate tokens. The default theme is "Bonsai" (dark).

| Token | Bonsai value | Role | Old variable |
|---|---|---|---|
| `bg` | `#1c110b` | page / canvas ground (deep soil) | `--bg` |
| `well` | `#160c07` | inset wells: terminal body, inputs, process nodes | (new; hardcoded `#0c0e11` today) |
| `panel` | `#251913` | islands (drawn at 94% alpha + blur) and worktree cards | `--panel` |
| `panel-2` | `#291d17` | cards, rows, hover fill on rows | `--panel-2` |
| `panel-3` | `#342721` | popovers, chips, button hover fill | `--panel-3` |
| `panel-4` | `#3f322b` | selected rows, inactive pills, small badges | (new) |
| `border-subtle` | `#2f211a` | dividers inside an island | (new) |
| `border` | `#3d271d` | island and card perimeters | `--border` |
| `border-strong` | `#4a3328` | agent node border, emphasized outlines | `--border-strong` |
| `text` | `#f5ded4` | primary text (warm parchment, never pure white) | `--text` |
| `muted` | `#bccabb` | secondary text (sage grey) | `--muted` |
| `muted-2` | `#869486` | labels, metadata, idle state | `--muted-2` |
| `faint` | `#6b5348` | dashed placeholder slots, hints | (new) |
| `accent` | `#6bfb9a` | moss: running state, links, focus, selected path | `--purple` |
| `accent-solid` | `#4ade80` | solid moss: primary buttons, status pips, pipeline "running" | (new) |
| `accent-fg` | `#00210c` | text on solid moss | (new) |
| `ok` | `#a4f0cd` | matcha: CI passing, approved PR, stacked-on links, inline code | `--blue` / part of `--green` |
| `warn` | `#ffb694` | terracotta light: attention text, behind count, diff deletions | `--orange` |
| `warn-solid` | `#d97746` | terracotta: waiting-for-you borders and dots, hover border | (new) |
| `danger` | `#ffb4ab` | failed CI, crashed process, destructive text | `--red` |
| `danger-solid` | `#93000a` | tint base for error fills (used at about 28% alpha) | (new) |

Notes for the planner:
- The old `--green` is used for both "running" and "success". In the new design **running = `accent`** and **CI passing = `ok`**. A mechanical alias in the first phase is fine (`--green` to `accent`), but a later pass must split the usages.
- Status mapping: running = `accent`, passing = `ok`, failing = `danger`, waiting for user = `warn-solid`, idle/stopped = `muted-2`, draft PR = `muted` on `panel-3`.
- The 7 worktree tag colors (`tagStyles.ts`) and the xterm ANSI palette are not designed in the mockups. Proposal: keep tag colors as a fixed categorical palette for now; derive the xterm theme from tokens (background=`well`, foreground=`text`, cursor=`accent`, green=`accent-solid`, red=`danger`, yellow=`warn`, brightBlack=`muted-2`), with an optional per-theme override.
- Agent provider badges (Claude, Antigravity, Codex) are brand/data colors, not theme tokens. The mockup uses placeholder monogram badges (CL, AG, CX); real logos are to be supplied as assets.

## 2. Fixed design constants (not themeable)

- Fonts: Hanken Grotesk (UI) and JetBrains Mono (labels, branch names, ports, paths, terminals), self-hosted. UI text 12 to 13px. Mono labels 9.5 to 11px. Island titles: mono 10.5px, uppercase, letter-spacing 0.08em, `muted`.
- Radii: islands 14px, top-bar islands 12px, worktree/hub cards 12 to 14px, process/agent nodes 10px, buttons 6 to 8px, chips and badges 5 to 6px, PR label chips 9px (pill), dots fully round.
- Island recipe: background `panel` at 94% alpha, `backdrop-filter: blur(14px)`, 1px `border`, box-shadow `inset 0 1px 0 rgb(accent-solid / .12), 0 16px 32px -12px rgba(10,7,5,.75)`. Title bar 30px high with a bottom `border-subtle`.
- Canvas ground: `bg` plus a soft radial vignette plus a 22px dot grid (`text` at 7% alpha). The canvas fills the whole viewport and the islands float above it.
- Hover: rows get `panel-2`; ghost buttons get `panel-3` and `text`; bordered buttons get a `warn-solid` border.
- Only floating overlays (command palette, modals) get a heavy drop shadow; everything else is tonal layering.

## 3. Shell layout (viewport at least 1280 by 960)

- Top bar: three separate islands, 40px high, at top/left/right 16px. Left: brand mark, workspace and project breadcrumbs. Middle: view switcher. Right: status, search, alerts, account.
- Left rail, 400px wide, x=16: **Inspector** island from y=68 down to the Branches island (tabs: Inspector / Files). **Branches** island at the bottom, 296px high: an indented hierarchy list of the default branch and worktrees (tree guide characters `├─ │ └─`), each row with a CI icon and PR number, selected row tinted with `accent` at 9%.
- Canvas toolbar: one small island at top-right (y=68): Fit, Auto-layout | zoom out, zoom in, fit view | "New worktree" (accent-tinted button).
- Terminals: a bottom island from x=428 to the right margin. **Collapsed**: 34px slim bar with the title, a count chip, one mini tab per session (provider badge, name, state dot), Open, and an expand chevron. **Expanded**: 296px high with split panes, each pane with a 26px header (provider badge, title, running chip, stop button), mono 11px output on `well`, and a 26px footer.

## 4. Canvas nodes (see board L)

**Worktree card**, 312×66, radius 12, background `panel`, border `border` (selected: `accent` at 55% plus a 3px `accent` at 10% ring).
- Row 1: branch icon, name (mono 12px, 600) | diff on the right (`+n` in `accent`, `−n` in `warn`).
- Row 2: CI badge (22×20, icon only; running spins; tinted `accent`/12%, `ok`/10%, `danger-solid`/28%), PR badge (icon + `#n`; draft = `panel-3`/`muted`, open = `accent` tint, approved = `ok` tint), ahead/behind (`↑3 ↓1`, the down count in `warn` when above 0) | right-aligned `→ base-branch` (`ok` when the base is a worktree, `muted-2` when it is the default branch).

**Process node**, 153×54, radius 10, background `well`, border `panel-3`.
- Header: 7px state square (radius 2: running `accent-solid`, failed `warn`, idle `muted-2`), name (mono 11px, 600), port on the right (mono 10px, `ok`).
- Second row: metadata (mono 9.5px, `muted-2`; failed shows `warn`) plus two 20px icon buttons. Running: Open, Stop. Failed: Logs, Restart (highlighted). Idle: Logs, Start.

**Agent node**, 153×54, radius 10, background about 1 step lighter than `panel-2`, border `border-strong` (waiting for the user: `warn-solid` at 55%).
- Header: 18px provider badge, name (sans 11.5px, 600), state dot at right (6px with a 3px halo: working `accent-solid`, waiting `warn-solid`, idle `muted-2`).
- Second row: current task (mono 9.5px; waiting shows `warn` text) plus two 20px icon buttons: terminal, then pause (working) / resume (idle) / reply (waiting, highlighted).

**Placeholder slots**: dashed 1px border (`border`), `faint` text, "+ process" / "+ agent", same size as a node. A worktree with nothing running shows two shorter slots (30px).

**Default-branch hub node**, 370×86, radius 14, border `accent-solid` at 55% plus a 4px ring at 8%.
- Header: branch icon, name (mono 13px, 600), a "DEFAULT" pill, and a labelled CI/CD badge on the right.
- A 4-segment pipeline (4px bars with 9px mono labels: lint, test, build, deploy; done = `ok`, running = `accent-solid`).
- Footer: merge-queue status text, a "Merge queue" button, and a pull icon button.

**Links**
- Worktree to default branch: 2px `accent-solid` line with an arrow into the top of the hub (selected path: 2.4px, brighter `accent`).
- Worktree stacked on another worktree: 2px `ok` rail 14px to the left of the card column, with a start dot on the child, rounded corners, and an arrow into the left edge of the base card. A PR label chip ("PR #26 → feat/passkeys", mono 10px, 18px high, pill) sits just under the child group. The selected path (selected worktree to its base to the default branch) is highlighted in `accent`.
- Depth rule: the farther a worktree is from the default branch, the higher it sits on the canvas.

**Target layout (separate track, not part of the looks-only pass)**: hub at the bottom center; three vertical lanes (x=30, 422, 814; 312 wide); tiers stacked upward by depth (tier tops at y=484, 238, 52 in an 830px tall canvas); each group is a card with up to four 2-column process/agent nodes under it. The decorative ASCII bonsai sits translucent (about 62% opacity) in the bottom-left corner with a project nameplate, linked to the hub.

## 5. Screens without a mockup

Files, Logs, Pull Requests, dialogs (Create Worktree, Start Agent, Env editor) and the command palette: restyle by tokens and the island recipe only; do not redesign their structure.

## 6. Known glitches in the mockup (do not replicate)

- Default-branch hub node: the footer text ("#29 merging · 1 queued · 8 PRs target this") and the "Merge queue" button wrap onto two lines, and the pipeline labels crowd each other. Intended: one line each, no wrapping.
- Legend (bottom-right): the "higher = farther from rust-backend" hint wraps. Intended: a single line.
- The decorative ASCII bonsai is partly hidden behind the "PR #34 → rust-backend" chip; spacing between the two should be fixed in the real app.
