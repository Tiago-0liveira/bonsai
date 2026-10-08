# Prompt for the planning model (paste this as-is)

You are the lead engineer planning a **visual re-skin plus theming foundation** for Bonsai's web client. You will NOT implement it. Your output is a written plan that cheaper models (Sonnet, Haiku) will execute task by task, so every task must be self-contained, small, and verifiable.

## Product context
Bonsai is a local-first tool for managing git worktrees: workspaces → projects → worktrees, with processes and AI agents (Claude, Antigravity, Codex) running per worktree and realtime GitHub PR/CI state. The web client lives in `web/` (React 18, Vite, Tailwind 3, zustand, @xyflow/react for the canvas, elkjs layout, xterm.js terminals, Radix UI, lucide-react, TanStack Router). A Go daemon serves it locally.

## Goal
1. Make the **Bonsai Botanical Terminal** look the app's **default theme**: dark cedar-brown surfaces, moss-green accent, terracotta attention color, matcha for "ok" states, Hanken Grotesk + JetBrains Mono.
2. Introduce a **small, correct theme layer** so users can later switch themes and add their own colors. Do the minimum that makes this possible; do NOT build a theme picker or editor UI now. Design the data model so one can be added later without refactoring.
3. Restyle the existing screens to match the approved mockups, **looks only**. No behavior changes, no data/API changes, no canvas layout-engine changes.

## What is attached (read these first)
- `DESIGN_SPEC.md`: the approved design distilled into tokens, the shell/island recipe, and the canvas node specs. This is the source of truth for values.
- `tokens.json`: the original design-system tokens (reference; the spec supersedes it where they differ).
- `board-L.html` + `board-L.png`: the final approved workspace mockup as a standalone page (open it in a browser; inline styles, exactly as designed) and its screenshot. Use them to resolve any measurement or color the spec does not state. Treat them as a visual reference, not code to copy.
- `terminals-expanded.html` + `terminals-expanded.png`: the Terminals island in its expanded state (board L shows it collapsed to a slim bar).
- `ascii-bonsai.reference.py`: optional reference for the decorative ASCII tree. Only used in the last, optional phase.
- The repository itself. **Inspect the real, current tree**; my notes about it may be out of date. Start with `web/src/styles/*.css`, `web/tailwind.config.ts`, `web/index.html`, `components/layout/*`, `features/inspector`, `features/terminal`, `features/workspace/nodes/BonsaiNode.tsx`, `features/workspace/canvas/BonsaiCanvas.tsx`, `features/workspace/tagStyles.ts`, `docs/canvas-layout-restructure-plan.md`.

## What I already know about the current styling (verify, don't trust)
- Colors are CSS custom properties holding space-separated RGB triplets (`--bg`, `--panel`, `--panel-2`, `--panel-3`, `--border`, `--border-strong`, `--text`, `--muted`, `--muted-2`, `--purple`, `--green`, `--blue`, `--orange`, `--red`) consumed as `rgb(var(--x) / alpha)` in roughly 900 places, mostly inside Tailwind arbitrary values.
- The accent is purple and the font is Inter. A few hardcoded hex values exist in `FakeTerminal.tsx` (xterm theme), `tagStyles.ts`, `surfaces.css`, and `marketing/landing.css`.
- Tailwind's config does not yet map colors to the variables.

## Constraints and preferences
- Keep the existing "RGB triplet CSS variable" mechanism (it supports alpha). Add a Tailwind color mapping to it only if you judge it worthwhile; justify the call.
- Theme = a typed object (`id`, `name`, `mode`, `colors` for a small fixed set of **semantic** tokens). Built-in themes live in TypeScript; a `ThemeProvider` applies the active one to `:root`; the selection and any custom themes persist in `localStorage` (behind a tiny storage interface so it can move to the daemon later). The default theme's values must also exist in static CSS so the first paint never flashes the wrong colors.
- Theme only colors (and `color-scheme`). Fonts, radii, spacing and shadow geometry are fixed design constants, not themeable.
- Semantic names only. `purple`, `green`, `blue`, `orange` and `red` must disappear from the vocabulary. Plan an aliasing step so the app never breaks mid-migration, then a cleanup step.
- JS consumers need resolved colors too: xterm's theme, any React Flow props or edge colors, and canvas decorations. Plan how they read from the active theme and react to theme changes.
- Fonts must work offline (self-hosted, e.g. via @fontsource), since the app is local-first.
- Custom themes need validation (hex format, minimum text/background contrast). Specify it; implement only if it is small.
- Do not add heavy dependencies. Prefer deleting code to adding it.
- Do not touch the canvas layout algorithm. The target hub-and-lanes layout in the board is a separate track; list it under "Out of scope / follow-ups" with a short note on how it would plug into `canvas/layout/*`.
- Existing tests (vitest) and e2e (Playwright) must keep passing.

## Required output
Produce `docs/theme-plan.md` containing:

1. **Findings**: what the code actually does today (palette plumbing, hardcoded colors, JS consumers of color, files that matter, anything surprising). Cite file paths.
2. **Architecture**: the token set (final names, with the old-to-new mapping), the theme type, provider behavior, persistence, first-paint strategy, how JS consumers subscribe, how a user theme would be added later. Keep it to about one page.
3. **Phased plan**, where each phase is independently shippable and leaves the app working. Suggested shape (change it if the code says otherwise): (0) fonts and token foundation with the default theme and old-name aliases, which already recolors the whole app; (1) provider, persistence, JS consumers (xterm, React Flow); (2) migrate old variable names to semantic ones and remove aliases; (3) shell: floating islands, top bar, left rail (Inspector with Inspector/Files tabs, Branches tree), canvas toolbar, collapsible Terminals bar; (4) canvas nodes: worktree card, process node, agent node, default-branch hub node, edges, PR chips; (5) remaining screens by token only (Files, Logs, Pull Requests, dialogs, command palette); (6) optional: ASCII bonsai decoration.
4. **Task list**. For every task give:
   - `id`, a title, and the **model tier** best suited (Haiku for mechanical, grep-verifiable edits; Sonnet for component restyling; flag anything that needs Opus-level judgment or review)
   - exact files to touch and what to change, with the relevant spec excerpt inlined so the implementer needs no other context
   - dependencies on other tasks
   - **acceptance criteria that can be checked mechanically** (a grep that must return nothing, `pnpm typecheck`, `pnpm test`, a Playwright screenshot of a named screen)
   - risks or traps
5. **Review gates**: after which tasks I (or you) should review before continuing.
6. **Out of scope / follow-ups**: theme picker UI, theme editor, light theme, canvas layout engine change, sharing themes.
7. **Decisions I need to make**: at most 5, each with your recommendation. Don't stop to ask me anything unless you are truly blocked; make the call, state the assumption, and continue.

Be concrete, not generic. If the mockup and the code disagree about something structural, say so and propose the smallest resolution.
