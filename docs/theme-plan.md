# Bonsai web client: theme foundation and Botanical Terminal re-skin

Plan for an implementation that cheaper models will carry out one task at a time. Each task in section 4 stands on its own: it names the files, the values, the checks, and the traps. If a task and the attached design files disagree, the task wins (the task already includes the corrections listed in Appendix A).

Inputs: `docs/handoff/DESIGN_SPEC.md`, `docs/handoff/board-L.html` / `.png` (1600×960), `docs/handoff/terminals-expanded.html` / `.png`, `docs/handoff/tokens.json`, `docs/handoff/ascii-bonsai.reference.py`.

---

## 0. Rules for every task

**Working directory.** All commands run in `web/`. This worktree has no `node_modules`, so run `pnpm install` before the first task.

**Standard checks** (named `CHECKS` below; a task passes only when all of them pass):

```sh
pnpm typecheck && pnpm lint && pnpm test
```

**End-to-end tests.** Run `pnpm test:e2e`. On Linux and macOS, `playwright.config.ts` also starts a Go fixture server, so Go must be installed. The e2e build uses `--mode e2e` and the production CSP (`e2e/https-preview.mjs`).

**Screens.** `pnpm test:e2e e2e/screens.spec.ts` (added in P0-1) writes `<name>.png` files under `web/test-results/`. A "screens" acceptance means: run it, open the named PNGs, and compare them with the mockup. These are review artifacts; no pixel diffing.

**Hard invariants. Breaking any of these fails the task:**

1. Do not edit anything under `src/features/workspace/canvas/layout/`, with one exception: the size constants in `estimateNodeSize()` in `geometry.ts`, and only when a task says so. Do not touch `LAYOUT`, the placement functions, or `PR_LABEL_SIZE`.
2. No store shape changes, no API changes, no persisted-preference key changes. One exception: P3-7 relaxes the validation in `panelPreferences.ts`.
3. Keep every test hook listed in Appendix B: class names, accessible names, roles, `data-*` attributes, and three source literals that the e2e build patches.
4. Colors come only from theme tokens: `rgb(var(--token) / alpha)` in CSS and arbitrary values. After P2-6, rewritten markup uses semantic Tailwind classes (`bg-panel/94`, `text-muted-2`). The only hex values allowed are in `src/theme/`, `src/styles/globals.css` (shadow constants), `src/features/workspace/tagStyles.ts` (categorical tag palette), and `src/marketing/` (out of scope).
5. No new dependencies other than the two font packages in P0-2.
6. Status color mapping (from the spec), used everywhere:

| State | Token |
|---|---|
| running / working / connected | `accent` for text, `accent-solid` for dots and bars |
| CI passing, check success, approved, merged | `ok` |
| failing, crashed, destructive | `danger`; tint fills use `danger-solid` at about 28% |
| needs attention / agent waiting (`AgentState 'idle'`) | `warn` for text, `warn-solid` for dots and borders |
| idle / stopped / finished | `muted-2` |
| draft PR | `muted` text on `panel-3` |

---

## 1. Findings (current tree, `feat/redesign` at 687b3aa)

**Palette plumbing**
- `web/src/styles/globals.css:5-21` defines 14 RGB-triplet variables plus `--purple-soft` (`151 109 255 / .12`), which nothing uses. The variables are consumed as `rgb(var(--x) / a)`: **1,001 occurrences in 31 files**. Most are Tailwind arbitrary values. The biggest consumers are `BonsaiNode.tsx` and `BottomWorkspace.tsx` (86 lines each), then `PullRequestsPage.tsx` (54), `BoardPage.tsx` (49), `Inspector.tsx` (48), and `globals.css` itself (45).
- Counts by variable: `border` 160, `muted` 154, `muted-2` 137, `purple` 87, `panel-2` 79, `bg` 66, `green` 64, `text` 59, `red` 53, `orange` 48, `border-strong` 32, `panel` 27, `panel-3` 27, `blue` 8.
- `web/tailwind.config.ts` only sets `fontFamily` (Inter and JetBrains Mono, neither of which is loaded; no font files ship). Tailwind's default palette is still active and in use: `text-red-400` (`AgentTerminal.tsx:71`), `border-red-400` and `border-amber-400` (`ProjectRootsSettings.tsx:70,75`), `text-cyan-400` and `text-pink-400` (`BoardPage.tsx:54-55`), `text-white` (`ProjectRootsSettings.tsx:101,131`, `PullRequestsPage.tsx:246`), and `bg-black/45…60` as modal scrims in 7 places.
- `body` in `globals.css` hardcodes the Inter font stack.

**Hardcoded colors**
- xterm themes: `#0c0e11` / `#d7d9df` in `AgentTerminal.tsx:34` and `ProcessTerminal.tsx:31`, plus a 10-color theme in `FakeTerminal.tsx:28-37`. `globals.css:97` (`.runtime-tile`) uses `#0c0e11`.
- Canvas: `BonsaiCanvas.tsx:466` (dot color `rgb(44 47 55)`), `buildCanvasGraph.ts:143,258,281,306` (edge strokes as inline `rgb()` strings), `PullRequestMergeEdge.tsx:37,46` (purple strokes).
- `tagStyles.ts`: 7 categorical tag colors as hex/rgba. The `TagColor` *names* (`'purple' | 'blue' | …`) are persisted data (`types/index.ts:4`, `workspacePersistence.ts:136`, `mock/tags.ts`, `mock/board.ts`). **They are data, not theme vocabulary, and must not be renamed.**
- `styles/surfaces.css` (the loading fallback and the **local connection gate**, which is the first screen users see) uses a separate "earth" hex palette (`#241d18`, `#eee7dc`, `#89a47d`, and others). `marketing/landing.css` has its own palette and fonts (out of scope).
- `index.html` has `<meta name="theme-color" content="#241d18">`.

**JS consumers of color**
- Only xterm needs resolved color strings. React Flow edges, the background pattern, and handles accept CSS strings, and CSS variables resolve inside inline `style` on SVG. In xyflow 12.12, `<Background color>` is forwarded as `--xy-background-pattern-color-props`. So the canvas can use `rgb(var(--token))` strings and react to theme changes with no JavaScript.
- `@xterm/xterm` 5.5's DOM renderer injects `<style>` elements for its theme and cell dimensions. Under the production CSP (`style-src-elem 'self'`, in `vite.config.ts`, `public/_headers`, and `e2e/https-preview.mjs`), the browser should refuse those elements. Then ANSI colors set through `ITheme` would not show in production. **This needs verification** (P1-4 step 5), with a pure-CSS fallback ready (P1-5).
- The same CSP rules out an inline boot `<script>` and any injected `<style>` tag. A theme can only be applied through CSSOM (`documentElement.style.setProperty`). `font-src 'self'` rules out font CDNs.

**Structure that matters for the re-skin**
- Shell (`components/layout/AppShell.tsx`): `TopBar` (48px, full width), then a vertical `react-resizable-panels` group. The main panel holds the route content plus the **Inspector on the right** (310px). The bottom panel is `BottomWorkspace`, which is itself a horizontal panel group: **Branches** | runtime terminals | Files/Git Diff (toggle) | PRs (toggle). When collapsed, the dock is size 0 and an "Open workspace" pill floats over the canvas.
- The mockup differs: floating islands, Inspector and Branches in a 400px **left** rail, Files as an Inspector tab, and Terminals as a bottom island that collapses to a 34px bar.
- Canvas nodes (`nodes/BonsaiNode.tsx`): `project` (300×154, the structural root that worktree edges connect to), `defaultBranch` (232×132 satellite left of the project), `env` (150×56 satellite right of it), `worktree` (230×154 plus History), `stack`, and `agent` (188×98). `nodes/ProcessNode.tsx` has `process` (240×112) and `runtimeShelf`. The mockup's "hub" is the default branch. In code, the root is the **project** node.
- `layout/geometry.ts:estimateNodeSize` hardcodes these sizes. Auto-layout always uses the *estimated* worktree height (`canonicalLayoutNodes` drops measured height). So a shorter worktree card **must** come with a new estimate, or Auto-layout leaves 90px gaps. `buildCanvasGraph.ts` positions the satellites at fixed offsets from the project width (`x - 266`, `x + 334`), so a wider hub collides with `env` unless those offsets change.
- `processNodeSelector` deliberately returns a narrow slice (no `port`) to keep render isolation (`e2e/render-isolation.spec.ts`).

**Missing data** (the mockup shows it; the data model does not have it): worktree diff `+n −n`; hub pipeline segments and merge queue; process uptime and memory; agent "pause / resume / reply" actions; PR reviewers; a "filter branches" behavior. The plan omits these rather than faking them.

**Dead code** (verified with grep: nothing imports it)
- `components/layout/ProjectSidebar.tsx` (20 lines that use old variables) and the `.desktop-sidebar` CSS.
- `features/terminal/FakeTerminal.tsx` (only its test imports it).
- `elkjs` is a dependency that no source file imports (left for the layout track).

**Surprises**
- The e2e build patches source text (`vite.config.ts:41-44`). It needs the literals `{children}</main>` in `AppShell.tsx`, `const { project,` in `buildCanvasGraph.ts`, and `const rects = new Map` in `layout/prLabels.ts`. If you remove `{children}</main>`, `render-isolation.spec` fails, because no Profiler commits are recorded.
- The three `bg-[rgb(var(--purple))] text-white` buttons become unreadable the moment purple is aliased to moss: contrast is 1.32:1.
- The agent "idle" state is shown as *warning* today (`BonsaiNode.tsx` maps `idle` to `warning`). That matches the spec's "waiting for you" color (`warn-solid`); "finished" maps to `muted-2`.
- In the mockup, the process node background is `#1c110b` (`bg`), not `well` as the spec says, and the agent node is `#2b1d16` (same as `panel-2`). See Appendix A.

---

## 2. Architecture

**Tokens.** There are 21 semantic color tokens, stored as space-separated RGB triplets so alpha works (`rgb(var(--panel) / .94)`). Tints and glows are always a token at lower alpha, never a separate token.

| Token | Bonsai hex | Triplet | Replaces |
|---|---|---|---|
| `bg` | `#1c110b` | `28 17 11` | `--bg` |
| `well` | `#160c07` | `22 12 7` | hardcoded `#0c0e11` |
| `panel` | `#251913` | `37 25 19` | `--panel` |
| `panel-2` | `#291d17` | `41 29 23` | `--panel-2` |
| `panel-3` | `#342721` | `52 39 33` | `--panel-3` |
| `panel-4` | `#3f322b` | `63 50 43` | new |
| `border-subtle` | `#2f211a` | `47 33 26` | new |
| `border` | `#3d271d` | `61 39 29` | `--border` |
| `border-strong` | `#4a3328` | `74 51 40` | `--border-strong` |
| `text` | `#f5ded4` | `245 222 212` | `--text` |
| `muted` | `#bccabb` | `188 202 187` | `--muted` |
| `muted-2` | `#869486` | `134 148 134` | `--muted-2` |
| `faint` | `#6b5348` | `107 83 72` | new |
| `accent` | `#6bfb9a` | `107 251 154` | `--purple`, part of `--green` |
| `accent-solid` | `#4ade80` | `74 222 128` | part of `--green` (solid dots) |
| `accent-fg` | `#00210c` | `0 33 12` | `text-white` on solid buttons |
| `ok` | `#a4f0cd` | `164 240 205` | part of `--green` (passing), `--blue` "Merged" |
| `warn` | `#ffb694` | `255 182 148` | `--orange` |
| `warn-solid` | `#d97746` | `217 119 70` | `--orange` solid dots |
| `danger` | `#ffb4ab` | `255 180 171` | `--red` |
| `danger-solid` | `#93000a` | `147 0 10` | new (error tint base) |

`--blue` has 3 call sites: 2 mean "running" (they become `accent`) and 1 means "Merged" (it becomes `ok`).

Design constants that are **not** themeable live in `globals.css` next to the tokens: `--font-sans`, `--font-mono`, `--shadow-island`, `--shadow-topbar`, `--shadow-card`, `--shadow-overlay`. The shadow constants reference token variables (for example `rgb(var(--accent-solid) / .12)`). Because themes are always applied on `:root`, those references resolve correctly.

**Theme type** (`src/theme/`, no React dependency except `ThemeProvider.tsx`):

```ts
export const THEME_TOKENS = ['bg','well','panel','panel-2','panel-3','panel-4','border-subtle','border','border-strong',
  'text','muted','muted-2','faint','accent','accent-solid','accent-fg','ok','warn','warn-solid','danger','danger-solid'] as const
export type ThemeToken = typeof THEME_TOKENS[number]
export type HexColor = `#${string}`                      // validated: /^#[0-9a-f]{6}$/i
export interface Theme {
  id: string                                             // built-in: 'bonsai'; user: 'custom:<slug>'
  name: string
  mode: 'dark' | 'light'                                 // drives color-scheme
  colors: Record<ThemeToken, HexColor>                   // all 21 required
  terminal?: Partial<Record<TerminalColor, HexColor>>    // optional ANSI overrides
}
export interface ThemePreferences { version: 1; activeId: string; custom: Theme[] }
```

**Provider behavior.** `themeStore.ts` is a vanilla module store:
- `initTheme(storage?)`: synchronous and idempotent.
- `getActiveTheme()`, `getThemes()` (built-ins plus custom), `setActiveTheme(id)`, `saveCustomTheme(input)` (returns a validation result), `removeCustomTheme(id)`, `subscribeTheme(fn)`.
- `useActiveTheme()` uses `useSyncExternalStore`.

`applyTheme(theme)` does all of the following through CSSOM (CSP-safe):
- calls `document.documentElement.style.setProperty('--<token>', triplet)` for all 21 tokens;
- sets `style.colorScheme = mode`;
- sets `dataset.theme = id`;
- updates `meta[name=theme-color]` to `colors.bg`.

`ThemeProvider` (mounted in `main.tsx` around `<Suspense>`) only re-reads storage when another tab changes it (the `storage` event) and renders its children. It provides no context value, because hooks read the module store.

**Persistence.** `interface ThemeStorage { load(): ThemePreferences | undefined; save(p: ThemePreferences): boolean; subscribe?(onChange: () => void): () => void }`.
- `localThemeStorage` uses the key `bonsai-theme-v1`, with try/catch around every call.
- `load` sanitizes its input: it drops custom themes that fail validation, and an unknown `activeId` falls back to `bonsai`.
- A failed `save` keeps state in memory.
- A daemon-backed implementation can replace it later without touching callers. It would keep the localStorage copy as a synchronous first-paint cache and call `subscribe` listeners when the daemon pushes a change.

**First paint.** The Bonsai values are written statically in `:root` in `globals.css`, so the loading fallback and the connection gate render correctly before any JavaScript runs. `main.tsx` calls `initTheme()` before `createRoot().render()`, which applies a stored non-default theme before React's first commit. While Bonsai is the only built-in theme, nothing can flash.

If a flash ever shows up with custom themes, the upgrade is an external, render-blocking `public/theme-boot.js` in `<head>`. `script-src 'self'` allows it; an inline script would be blocked. A unit test keeps the CSS and the TypeScript in sync by parsing `globals.css?raw` and comparing every triplet with `bonsaiTheme.colors`.

**JS consumers**
- **xterm**: `src/theme/terminal.ts` exports `terminalTheme(theme): ITheme` (background `well`, foreground `text`, cursor `accent`, green `accent-solid`, red `danger`, yellow `warn`, brightBlack `muted-2`, and the rest mapped; then `theme.terminal` overrides). It also exports `bindTerminalAppearance(terminal, onMetrics)`, which sets the theme and the mono font, re-applies them on every `subscribeTheme` change, and re-measures after `document.fonts` loads.
- **React Flow**: uses CSS strings only. `colorMode={theme.mode}`, and the `.react-flow` `--xy-*` variables are overridden in CSS.
- **Tag colors**: a fixed categorical palette (not themed) in `tagStyles.ts`.
- **ASCII decoration**: CSS classes on tokens.

**Validation** (`validate.ts`, implemented now because it is small):
- Every token must be present and match `/^#[0-9a-f]{6}$/i`.
- `mode` must be `dark` or `light`.
- `name` must be 1 to 40 characters.
- Contrast rules: these are **errors** (reject): `text` on `bg`, `panel`, and `panel-2` ≥ 7; `accent-fg` on `accent-solid` ≥ 4.5; `muted` on `panel` ≥ 4.5. These are **warnings** (allowed): `muted-2` on `panel` ≥ 3; each of `accent`, `ok`, `warn`, `danger` on `panel` ≥ 3.
- For reference, the Bonsai theme scores 14.35, 13.27, 9.86, 10.03, 5.37, and ≥ 10.

**Adding a user theme later.** A picker lists `getThemes()` and calls `setActiveTheme`. An editor clones a theme's `colors`, edits them, and calls `saveCustomTheme`, which validates the theme, assigns `custom:<slug>`, and persists it. A light built-in theme is one more object in `themes.ts` with `mode: 'light'`; the shadow constants may need a light variant then.

**Tailwind.** P2-6 maps the tokens to Tailwind colors as `rgb(var(--t) / <alpha-value>)` and *replaces* the default palette (keeping `transparent`, `current`, `inherit`). Why:
- It cuts most class names to a third of their length.
- New markup can no longer use the default palette (`red-400`): such a class silently fails to generate, and grep catches it.
- The token list comes from one source (`tailwind.config.ts` imports `THEME_TOKENS`).

Existing arbitrary values are only renamed, not converted. Markup gets converted when a later task rewrites it anyway.

---

## 3. Phases

Each phase ships on its own and leaves the app working.

| Phase | Result | Tasks |
|---|---|---|
| 0 Foundation | Fonts self-hosted, new tokens with aliases for the old names. **The whole app is already recolored.** | P0-1 … P0-5 |
| 1 Theme layer | Typed themes, store, persistence, validation, xterm and React Flow wiring | P1-1 … P1-5 |
| 2 Vocabulary | Old variable names gone, semantic Tailwind colors, aliases removed | P2-1 … P2-7 |
| 3 Shell | Islands, top bar, left rail (Inspector/Files tabs, Branches), canvas toolbar, Terminals bar/island | P3-1 … P3-10 |
| 4 Canvas nodes | Worktree card, process, agent, hub, satellites, edges, PR chips, ground | P4-1 … P4-9 |
| 5 Other screens | Token and recipe pass over pages, dialogs, palette, popovers | P5-1 … P5-5 |
| 6 Optional | ASCII bonsai decoration | P6-1, P6-2 |

Phase 3 and Phase 4 can run in parallel after Phase 2. Inside a phase, the order below is the dependency order unless a task says otherwise.

---

## 4. Tasks

### Phase 0: foundation

#### P0-1: Baseline and screens spec
- **Tier:** Haiku.
- **Files:** new `web/e2e/screens.spec.ts`.
- **Change:**
  1. Run `pnpm install`, `CHECKS`, and `pnpm test:e2e`. Record any test that already fails in the PR description as the pre-existing baseline.
  2. Add a spec with one `test()` per screen. Each test uses `page.setViewportSize({ width: 1600, height: 960 })`, `mockGitBackend(page)`, `openConnectedApp(page)` (from `./mockGit`), waits 600ms, then `await page.screenshot({ path: test.info().outputPath('<name>.png') })`.
  3. Screens:
     - `canvas`
     - `canvas-dock-collapsed` (call `window.__bonsaiTestStore.getState().setDockState('collapsed')` first)
     - `inspector-worktree` (click `.react-flow__node-worktree` first)
     - `github` (click `getByRole('link', { name: 'GitHub', exact: true })`)
     - `table` (click the `Table` link)
     - `logs` (click the `Logs` link)
     - `settings` (click the `Settings` link)
     - `dialog-create-worktree` (click the `New worktree` button)
     - `command-palette` (`setPaletteOpen(true)` through the test store)
     - `connect-gate` (`page.goto('/app')` *without* `openConnectedApp`)
  4. Assert only that the page has no `pageerror` events.
- **Acceptance:** `pnpm test:e2e e2e/screens.spec.ts` passes and produces 10 PNGs. `CHECKS` pass.
- **Risk:** `openConnectedApp` does `page.goto('/app')` and then clicks "Connect to local Bonsai". The session lives in memory, so a later `page.goto` drops back to the connection gate. Navigate between views with the nav links, never with `goto`.

#### P0-2: Self-hosted fonts
- **Tier:** Haiku. **Depends on:** P0-1.
- **Files:** `web/package.json`, `web/src/main.tsx`, `web/tailwind.config.ts`, `web/src/styles/globals.css`.
- **Change:**
  1. `pnpm add @fontsource-variable/hanken-grotesk@5.3.0 @fontsource-variable/jetbrains-mono@5.3.0`.
  2. In `main.tsx`, before the CSS imports: `import '@fontsource-variable/hanken-grotesk'` and `import '@fontsource-variable/jetbrains-mono'`.
  3. In `globals.css` `:root`, add `--font-sans: "Hanken Grotesk Variable", "Hanken Grotesk", ui-sans-serif, system-ui, sans-serif;` and `--font-mono: "JetBrains Mono Variable", "JetBrains Mono", ui-monospace, SFMono-Regular, Consolas, monospace;`.
  4. Set `body { font-family: var(--font-sans); }`.
  5. In Tailwind `fontFamily`, set `sans: ['"Hanken Grotesk Variable"', '"Hanken Grotesk"', 'ui-sans-serif', 'system-ui', 'sans-serif']` and `mono: ['"JetBrains Mono Variable"', '"JetBrains Mono"', 'ui-monospace', 'SFMono-Regular', 'Consolas', 'monospace']`.
- **Spec:** "Fonts: Hanken Grotesk (UI) and JetBrains Mono (labels, branch names, ports, paths, terminals), self-hosted."
- **Acceptance:**
  - `CHECKS` pass.
  - `pnpm build && ls dist/assets | grep -E 'hanken|jetbrains'` lists woff2 files.
  - `grep -rn "Inter" src --include='*.css' --include='*.ts' --include='*.tsx' | grep -v marketing` returns nothing.
  - Screens show the new fonts.
- **Risks:** The Fontsource family names end in **"Variable"**; using `"Hanken Grotesk"` alone falls back to the system font. Do not add Google Fonts links: the CSP has `font-src 'self'`.

#### P0-3: Token foundation with aliases
- **Tier:** Haiku. **Depends on:** P0-2.
- **Files:** `web/src/styles/globals.css`, `web/index.html`.
- **Change:**
  1. Replace the palette block in `:root` (lines 7-21) with the 21 tokens, copying the triplets from the section 2 table exactly. Keep `color-scheme: dark;`.
  2. Delete `--purple-soft`.
  3. Add this alias block, marked for removal in P2-7:
     ```css
     /* Legacy aliases: removed in P2-7 */
     --purple: var(--accent); --green: var(--accent); --blue: var(--ok); --orange: var(--warn); --red: var(--danger);
     ```
  4. Add the shadow constants:
     ```css
     --shadow-island: inset 0 1px 0 rgb(var(--accent-solid) / .12), 0 16px 32px -12px rgb(10 7 5 / .75);
     --shadow-topbar: inset 0 1px 0 rgb(var(--accent-solid) / .14), 0 14px 28px -12px rgb(10 7 5 / .75);
     --shadow-card:   inset 0 1px 0 rgb(var(--text) / .04), 0 14px 28px -16px rgb(10 7 5 / .85);
     --shadow-overlay: 0 16px 32px -8px rgb(10 7 5 / .75), 0 0 0 1px rgb(var(--border));
     ```
  5. In `index.html`, change `theme-color` to `#1c110b`.
- **Acceptance:**
  - `CHECKS` pass.
  - `grep -c -- '--purple-soft' src/styles/globals.css` prints `0`.
  - The screens are recolored brown and moss, with no purple left.
- **Risk:** An alias must be a plain `var(--token)` with no `rgb()` around it. Consumers already wrap it in `rgb()`.

#### P0-4: Contrast fixes for solid accent fills
- **Tier:** Haiku. **Depends on:** P0-3.
- **Files:** `src/features/settings/ProjectRootsSettings.tsx` (two buttons, "Use selected repositories" and "Add folder"), `src/features/github/PullRequestsPage.tsx` (the "Comment" button, line ~246).
- **Change:** In those three buttons, replace `bg-[rgb(var(--purple))]` with `bg-[rgb(var(--accent-solid))]` and replace `text-white` with `text-[rgb(var(--accent-fg))]`. Change nothing else.
- **Spec:** "`accent-solid`: solid moss, primary buttons… `accent-fg`: text on solid moss."
- **Acceptance:** `grep -rn "text-white" src | grep -v marketing` returns nothing. `CHECKS` pass.

#### P0-5: Hardcoded colors to tokens
- **Tier:** Haiku. **Depends on:** P0-3.
- **Files:** `src/styles/globals.css`, `src/styles/surfaces.css`, `src/features/workspace/canvas/BonsaiCanvas.tsx`, `src/features/workspace/canvas/buildCanvasGraph.ts`, `src/features/workspace/canvas/PullRequestMergeEdge.tsx`.
- **Change:**
  - `globals.css` `.runtime-tile`: `#0c0e11` becomes `rgb(var(--well))`.
  - `BonsaiCanvas.tsx` `<Background>`: `gap={22} size={1} color="rgb(var(--text) / .07)"`.
  - `buildCanvasGraph.ts`:
    - `'rgb(75 214 140 / .45)'` becomes `'rgb(var(--accent-solid) / .45)'`;
    - both `'rgb(50 53 62)'` become `'rgb(var(--border-strong))'`;
    - `'rgb(62 65 75)'` becomes `'rgb(var(--border-strong))'`.
  - `PullRequestMergeEdge.tsx`: `'rgb(151 109 255 / .72)'` becomes `'rgb(var(--ok) / .72)'`, and `"rgb(151 109 255 / .5)"` becomes `"rgb(var(--ok) / .5)"`.
  - `surfaces.css`: map every color like this (`T(x)` means `rgb(var(--x))`):
    - `#241d18` becomes `T(bg)`.
    - `#eee7dc` and `#ded2c1` become `T(text)`.
    - The `.local-connect` gradient becomes `radial-gradient(circle at 50% 20%, rgb(var(--panel)) 0, rgb(var(--bg)) 48%, rgb(var(--well)) 100%)`.
    - Card: `border: 1px solid rgb(var(--border))`, `background: rgb(var(--panel) / .94)`, `backdrop-filter: blur(14px)`, `box-shadow: var(--shadow-island)`, `border-radius: 14px`.
    - `#89a47d` becomes `T(accent)`, and `rgba(137,164,125,.45)` becomes `rgb(var(--accent-solid) / .45)`.
    - `#cbbba7` becomes `T(muted)`; `#8a725d` becomes `T(muted-2)`.
    - `code` background `#1c1713` becomes `T(well)`, with border `rgb(var(--border-subtle))`.
    - The primary button gets `background: T(accent-solid)`, `color: T(accent-fg)`, `border-color: T(accent-solid)`, `border-radius: 8px`.
    - Secondary button border `rgba(203,187,167,.28)` becomes `T(border-strong)`.
    - The note's top border becomes `T(border-subtle)`.
    - Every `ui-monospace, SFMono-Regular, Menlo, monospace` stack becomes `var(--font-mono)`.
- **Acceptance:**
  - `CHECKS` pass.
  - `grep -nE "#[0-9a-fA-F]{6}|rgba?\([0-9]" src/styles/surfaces.css src/features/workspace/canvas/*.ts src/features/workspace/canvas/*.tsx` returns nothing.
  - The `connect-gate` and `canvas` screens look right.
- **Risk:** The strings in `buildCanvasGraph.ts` sit inside a pure function that tests may snapshot. Run `pnpm test` (`processGraph.test.ts`, `graphReconciliation.test.ts`); if a test asserts the old string, update that expectation to the new string. Do not change logic.

**Gate G0** (section 5).

### Phase 1: theme layer

#### P1-1: Theme core
- **Tier:** Sonnet. **Depends on:** P0-3.
- **Files (new):** `src/theme/tokens.ts`, `src/theme/themes.ts`, `src/theme/color.ts`, `src/theme/theme.test.ts`.
- **Change:**
  - `tokens.ts` exports `THEME_TOKENS`, `ThemeToken`, `HexColor`, `Theme`, `ThemePreferences`, and `TerminalColor` (xterm's 16 ANSI names plus `background`, `foreground`, `cursor`, `cursorAccent`, `selectionBackground`), exactly as in section 2. It has no imports, because `tailwind.config.ts` imports it later.
  - `themes.ts` exports `bonsaiTheme` (`id: 'bonsai'`, `name: 'Bonsai'`, `mode: 'dark'`, the colors from the table), `builtInThemes = [bonsaiTheme]`, and `DEFAULT_THEME_ID = 'bonsai'`.
  - `color.ts` exports `hexToTriplet('#1c110b') === '28 17 11'`, `relativeLuminance(hex)`, and `contrastRatio(a, b)` (the WCAG 2.x formula).
  - Test 1 imports `css from '../styles/globals.css?raw'`. For every token, it asserts that `:root` contains `--<token>: <hexToTriplet(bonsaiTheme.colors[token])>;`.
  - Test 2 checks the contrast values: text/bg ≈ 14.35 and accent-fg/accent-solid ≈ 9.86 (`toBeCloseTo(x, 1)`).
- **Acceptance:** `CHECKS` pass, and the parity test fails if you change one triplet in `globals.css` (try it, then revert).
- **Risk:** Do not import React in `src/theme/` except in `ThemeProvider.tsx`.

#### P1-2: Validation
- **Tier:** Sonnet. **Depends on:** P1-1.
- **Files (new):** `src/theme/validate.ts` and `src/theme/validate.test.ts`.
- **Change:** `validateTheme(input: unknown): { ok: true; theme: Theme; warnings: string[] } | { ok: false; errors: string[] }`, implementing the rules in section 2 (the "Validation" paragraph). The function lower-cases hex values, trims `name`, and ignores unknown keys. `terminal` overrides must also be valid hex.
- **Acceptance:** Tests cover all of these:
  - `bonsaiTheme` is valid with zero warnings;
  - a missing token, a 3-digit hex, and `text === bg` are each rejected;
  - low `muted-2` contrast gives a warning, not an error;
  - a non-object input is rejected.
  - `CHECKS` pass.

#### P1-3: Store, persistence, provider, first paint
- **Tier:** Sonnet. **Review:** Opus. **Depends on:** P1-2.
- **Files:** new `src/theme/storage.ts`, `src/theme/themeStore.ts`, `src/theme/ThemeProvider.tsx`, `src/theme/themeStore.test.ts`; edit `src/main.tsx`.
- **Change:** Implement section 2 ("Provider behavior", "Persistence", "First paint") exactly:
  - `storage.ts`: the `ThemeStorage` interface plus `localThemeStorage` (key `bonsai-theme-v1`; `subscribe` listens to `window` `storage` events for that key). `load` runs every stored custom theme through `validateTheme` and drops failures.
  - `themeStore.ts`:
    - `applyTheme(theme, root = document.documentElement)` uses only `style.setProperty`, `style.colorScheme`, `dataset.theme`, and the meta tag's `content`.
    - `setActiveTheme` with an unknown id does nothing.
    - `removeCustomTheme` on the active theme falls back to `DEFAULT_THEME_ID`.
    - `saveCustomTheme` makes ids from `'custom:' + slug(name)` and adds `-2`, `-3`, … on collision. It never overwrites a built-in theme.
  - `ThemeProvider.tsx`: `useEffect(() => storage.subscribe?.(() => reload()), [])` and returns `children`.
  - `main.tsx`: call `initTheme()` before `ReactDOM.createRoot`, and wrap `<Suspense>` in `<ThemeProvider>`.
- **Acceptance:**
  - jsdom tests: applying sets `--accent` to `107 251 154` on `document.documentElement.style`; a stored custom theme is applied by `initTheme`; corrupt JSON falls back to bonsai; an unknown active id falls back to bonsai; an invalid custom theme is dropped; a throwing `localStorage.setItem` keeps state in memory; subscribers fire once per change.
  - `grep -rn "createElement('style')\|<style" src/theme` returns nothing.
  - `CHECKS` and `pnpm test:e2e` pass (CSP: no new console violations).
- **Risks:**
  - Never inject a `<style>` element and never add an inline `<script>`. The CSP blocks both in production and in e2e.
  - `initTheme` must not throw when `localStorage` is unavailable.
  - The landing page (`/`) also runs `main.tsx`. The tokens are harmless there, because `landing.css` uses its own variables.

#### P1-4: Terminal appearance
- **Tier:** Sonnet. **Depends on:** P1-3.
- **Files:** new `src/theme/terminal.ts` and `src/theme/terminal.test.ts`; edit `src/features/terminal/AgentTerminal.tsx` and `src/features/terminal/ProcessTerminal.tsx`; delete `src/features/terminal/FakeTerminal.tsx` and `FakeTerminal.test.tsx`.
- **Change:**
  1. `terminalTheme(theme)` returns an `ITheme` (type-only import from `@xterm/xterm`):
     - background `well`, foreground `text`, cursor `accent`, cursorAccent `well`, selectionBackground `accent + '47'`;
     - black `well`, red `danger`, green `accent-solid`, yellow `warn`, blue `ok`, magenta `warn-solid`, cyan `ok`, white `muted`;
     - brightBlack `muted-2`, brightRed `danger`, brightGreen `accent`, brightYellow `warn`, brightBlue `ok`, brightMagenta `warn`, brightCyan `ok`, brightWhite `text`;
     - then spread `theme.terminal`.
  2. Export `TERMINAL_FONT = '"JetBrains Mono Variable", "JetBrains Mono", ui-monospace, SFMono-Regular, Consolas, monospace'`, the literal list from P0-2. xterm does not resolve `var(--font-mono)`.
  3. `bindTerminalAppearance(terminal: { options: { theme?: unknown; fontFamily?: string } }, onMetrics: () => void): () => void` sets `options.theme` and `options.fontFamily` immediately, then on every `subscribeTheme` call; after `document.fonts?.ready` it re-assigns `fontFamily` and calls `onMetrics()`. It returns an unsubscribe function.
  4. In both terminals:
     - add `document.fonts?.load('11px "JetBrains Mono Variable"')` to the existing `Promise.all` of the dynamic imports, wrapped so that a rejection is ignored;
     - construct the terminal with `fontSize: 11`, `fontFamily: TERMINAL_FONT`, `theme: terminalTheme(getActiveTheme())`;
     - right after `terminal.open`, call `const unbind = bindTerminalAppearance(terminal, resize)`;
     - call `unbind()` in the effect cleanup;
     - delete the hardcoded hex values.
  5. **Verify under CSP:** add a temporary e2e check, or do it by hand with `pnpm build --mode e2e && node e2e/https-preview.mjs`. Open an agent terminal (fixture: `agent-terminal-fullstack.spec.ts`) and look for console messages that contain `Refused to apply inline style`. Report the result in the PR. If any appear, do P1-5.
- **Spec:** "derive the xterm theme from tokens (background=`well`, foreground=`text`, cursor=`accent`, green=`accent-solid`, red=`danger`, yellow=`warn`, brightBlack=`muted-2`), with an optional per-theme override." Terminal output: "mono 11px output on `well`".
- **Acceptance:**
  - Unit test: `terminalTheme(bonsaiTheme).background === '#160c07'`, and an override wins.
  - `bindTerminalAppearance` called with a fake terminal updates `options.theme` after `setActiveTheme`.
  - `grep -rnE "#[0-9a-fA-F]{6}" src/features/terminal` returns nothing.
  - `grep -rn "FakeTerminal" src` returns nothing.
  - `CHECKS` pass, and the e2e terminal specs pass (`agent-terminal-fullstack`, `process-terminal-fullstack`, `execution-controls`).
- **Risks:**
  - xterm does not resolve `var(...)` in `fontFamily`. Use the literal family list.
  - Keep the existing `[inert]`, size, and StrictMode guards unchanged.
  - Deleting `FakeTerminal` removes one unit test file. It is dead code (nothing imports it), so this is intended; say so in the PR.

#### P1-5: React Flow and xterm CSS wiring
- **Tier:** Haiku. **Depends on:** P1-4.
- **Files:** `src/styles/globals.css`, `src/features/workspace/canvas/BonsaiCanvas.tsx`.
- **Change:**
  1. Add to `globals.css`:
     ```css
     .react-flow { --xy-background-color: transparent; --xy-edge-stroke-default: rgb(var(--border-strong));
       --xy-handle-background-color-default: rgb(var(--panel-3)); --xy-handle-border-color-default: rgb(var(--border-strong));
       --xy-selection-background-color: rgb(var(--accent) / .08); --xy-selection-border-default: 1px dashed rgb(var(--accent) / .55); }
     ```
  2. In `BonsaiCanvas`, pass `colorMode={useActiveTheme().mode}` to `<ReactFlow>`.
  3. **Only if P1-4 step 5 found CSP refusals**, also add a static block in `globals.css` that themes xterm through classes:
     - `.xterm .xterm-rows { color: rgb(var(--text)); }` and `.xterm .xterm-viewport, .xterm .xterm-screen { background: rgb(var(--well)); }`;
     - `.xterm .xterm-fg-N { color: … }` and `.xterm .xterm-bg-N { background-color: … }` for N = 0 to 15, using the same token map as `terminalTheme`;
     - a test in `terminal.test.ts` that parses `globals.css?raw` and checks that each `.xterm-fg-N` uses the token `terminalTheme` maps to.
- **Acceptance:** `CHECKS` pass, `pnpm test:e2e` passes, and the canvas screen is unchanged apart from the selection box color.

**Gate G1.**

### Phase 2: vocabulary migration

Rule for P2-2 to P2-4: edit **in place**. Change only the variable name inside the existing arbitrary value (`var(--purple)` becomes `var(--accent)`) and keep the alpha and the class structure. Run P2-2, P2-3, and P2-4 sequentially, because they touch the same files. Line numbers are as of 687b3aa; match on the quoted content if they have moved.

#### P2-1: Delete dead shell code
- **Tier:** Haiku. **Depends on:** G0.
- **Change:** Delete `src/components/layout/ProjectSidebar.tsx`. In `globals.css`, delete the `.desktop-sidebar { display: none; }` rule inside `@media (max-width: 800px)`.
- **Acceptance:** `grep -rn "ProjectSidebar\|desktop-sidebar" src` returns nothing. `CHECKS` pass.

#### P2-2: Purple to accent, drafts to muted
- **Tier:** Haiku. **Depends on:** P2-1.
- **Change:**
  1. First, the four Draft PR sites. Each `--purple` color used for `status === 'Draft'` becomes `text-[rgb(var(--muted))]`, plus `bg-[rgb(var(--panel-3))]` where a fill or border existed:
     - `BonsaiNode.tsx:145` (`PrBadge`, `'Draft'` branch): `border-[rgb(var(--border))] bg-[rgb(var(--panel-3))] text-[rgb(var(--muted))]`;
     - `PullRequestsPage.tsx:135` and `:164`;
     - `BottomWorkspace.tsx:661`.
  2. Then run `grep -rl 'var(--purple)' src | xargs sed -i 's/var(--purple)/var(--accent)/g'`. This includes `globals.css`.
- **Acceptance:** `grep -rn -- '--purple' src` returns only the alias line in `globals.css`. `CHECKS` pass.

#### P2-3: Split green into accent, accent-solid, and ok
- **Tier:** Haiku. **Depends on:** P2-2.
- **Change:** Apply these exact lists in order, then do the mechanical rest.
  1. Change to **`--ok`** (passing or success):
     - `Inspector.tsx:137` (the `CheckCircle2` icon in "No branch blockers reported.");
     - `Inspector.tsx:163` (the `check.status === 'success'` icon);
     - `BonsaiNode.tsx:116` (CiBadge `passed` tone, both occurrences on the line);
     - `PullRequestsPage.tsx:28` and `:227` (`CheckCircle2` icons);
     - `PullRequestsPage.tsx:245` (the Approve button, all occurrences on the line);
     - `BottomWorkspace.tsx:526` (`checkIcon` success).
  2. Change to **`--accent-solid`** (solid status dots, written `bg-[rgb(var(--green))]` with no alpha):
     - `TopBar.tsx:116`;
     - `StartAgentDialog.tsx:139` (the dot `span` only);
     - `BonsaiNode.tsx:94` (`healthy`);
     - `BonsaiNode.tsx:256` (the dot and its `shadow-[0_0_7px_rgb(var(--green)/.8)]`);
     - `BottomWorkspace.tsx:58`.
  3. Leave `BoardPage.tsx:51`; P2-5 handles it.
  4. Everything else: `grep -rl 'var(--green)' src | grep -v BoardPage | xargs sed -i 's/var(--green)/var(--accent)/g'`.
- **Acceptance:** `grep -rn -- '--green' src` returns only `BoardPage.tsx` and the alias line. `grep -rn 'var(--ok)' src --include='*.tsx' | wc -l` is ≥ 9 (the 7 lines listed here plus the 2 from P0-5). `CHECKS` pass.

#### P2-4: Orange, red, and blue
- **Tier:** Haiku. **Depends on:** P2-3.
- **Change:**
  1. Solid orange dots `bg-[rgb(var(--orange))]` (no alpha) at `TopBar.tsx:118`, `BottomWorkspace.tsx:60`, and `BonsaiNode.tsx:95` become `bg-[rgb(var(--warn-solid))]`.
  2. All other `var(--orange)` becomes `var(--warn)` (sed).
  3. All `var(--red)` becomes `var(--danger)` (sed).
  4. Blue:
     - `BonsaiNode.tsx:118` (CI running) and `ProcessNode.tsx:12` (Starting) become `var(--accent)`;
     - `BonsaiNode.tsx:147` (`Merged`) becomes `var(--ok)`;
     - leave `BoardPage.tsx:50` for P2-5.
- **Acceptance:** `grep -rnE -- '--(orange|red)\b' src` and `grep -rn -- '--blue' src | grep -v BoardPage` return only the alias line. `CHECKS` pass.

#### P2-5: Board list colors use the tag palette
- **Tier:** Haiku. **Depends on:** P2-4.
- **Files:** `src/features/workspace/tagStyles.ts`, `src/features/board/BoardPage.tsx`.
- **Change:**
  1. Export the `palette` constant from `tagStyles.ts` as `tagPalette`.
  2. In `BoardPage.tsx`, delete `colorClass()`. At each call site, replace `className={colorClass(x)}` with `style={{ color: tagPalette[x].foreground }}`, merging into an existing `style` if there is one.
- **Spec:** "keep tag colors as a fixed categorical palette for now."
- **Acceptance:** `grep -rnE -- '--(purple|green|blue|orange|red)\b' src` returns only the alias line. `grep -n "cyan-400\|pink-400" src -r` returns nothing. `CHECKS` pass.

#### P2-6: Semantic Tailwind colors
- **Tier:** Sonnet. **Depends on:** P2-5 and P1-1.
- **Files:** `web/tailwind.config.ts`, plus the files that use Tailwind's default palette.
- **Change:**
  1. Config:
     ```ts
     import { THEME_TOKENS } from './src/theme/tokens'
     const token = (name: string) => `rgb(var(--${name}) / <alpha-value>)`
     // theme.colors (REPLACES the default palette):
     colors: { transparent: 'transparent', current: 'currentColor', inherit: 'inherit',
       ...Object.fromEntries(THEME_TOKENS.map(name => [name, token(name)])) },
     extend: { fontFamily: { /* P0-2 */ }, borderColor: { DEFAULT: token('border') }, ringColor: { DEFAULT: token('accent') },
       boxShadow: { island: 'var(--shadow-island)', topbar: 'var(--shadow-topbar)', card: 'var(--shadow-card)', overlay: 'var(--shadow-overlay)' } }
     ```
  2. Fix the default-palette users:
     - `bg-black/45`, `/55`, and `/60` (7 sites: CommandPalette, CreateWorktreeDialog, DeleteWorktreeDialog, StartAgentDialog, StartProcessDialog, BottomWorkspace `EditorPreferenceDialog`, BoardPage) become `bg-well/70`;
     - `text-red-400` becomes `text-danger`;
     - `border-red-400` becomes `border-danger`;
     - `border-amber-400` becomes `border-warn-solid`.
- **Acceptance:**
  - `CHECKS` pass.
  - `grep -rnoE "\b(bg|text|border|ring|from|to|via|divide|placeholder|outline|fill|stroke)-(black|white|red|green|blue|orange|purple|yellow|amber|emerald|zinc|gray|slate|neutral|stone|sky|cyan|pink|rose|violet|indigo|lime|teal|fuchsia)\b" src --include='*.tsx' | grep -v marketing` returns nothing.
  - The screens are unchanged except the modal scrim (which is now brown).
- **Risks:**
  - Replacing `theme.colors` also changes Preflight's default border color from gray-200 to the `borderColor.DEFAULT` you set. That is why the `DEFAULT` entry is required: elements with a bare `border` class must get `--border`.
  - `tailwind.config.ts` is loaded by jiti, so the TypeScript import works. Keep `tokens.ts` free of imports.

#### P2-7: Remove the aliases
- **Tier:** Haiku. **Depends on:** P2-6.
- **Change:** Delete the alias block from `globals.css`.
- **Acceptance:**
  - `grep -rnE -- '--(purple|green|blue|orange|red)\b' src` returns nothing.
  - `grep -rnE "#[0-9a-fA-F]{6}\b|rgba?\([0-9]" src --include='*.ts' --include='*.tsx' --include='*.css' | grep -vE "src/(marketing|theme)/|tagStyles\.ts|\.test\.|src/styles/globals\.css"` returns nothing, except Tailwind shadow arbitrary values of the form `rgb(0_0_0/...)`. Those are left for the restyle phases and are listed in the PR.
  - `CHECKS` and `pnpm test:e2e` pass.

**Gate G2.**

### Phase 3: shell

Shared spec excerpt for this phase:
- **Island recipe:** background `panel` at 94% alpha, `backdrop-filter: blur(14px)`, 1px `border`, radius 14px, shadow `--shadow-island`. Title bar 30px high with a bottom `border-subtle`.
- **Island titles:** mono 10.5px, uppercase, letter-spacing .08em, `muted`.
- **Hover:** rows get `panel-2`; ghost buttons get `panel-3` and `text`; bordered buttons get a `warn-solid` border.
- **Shadows:** only floating overlays get a heavy drop shadow; everything else uses tonal layering.
- **Sizes:** UI text 12 to 13px; mono labels 9.5 to 11px.
- **Radii:** top-bar islands 12px, buttons 6 to 8px, chips and badges 5 to 6px.

Layout geometry (decision D1, inset layout), for viewports of at least 1280×960:
- **Ground:** `bg` plus vignette.
- **Top bar:** three islands, 40px high, at top 16px, left 16px, and right 16px.
- **Left rail:** x = 16, width 400, from y = 68 to bottom 16. The Inspector island is flexible; below it is a 12px gap, then the Branches island at `height: min(296px, 40vh)`.
- **Main column:** left 428, top 68, right 16, bottom 16. It contains the canvas (borderless, `rounded-[14px] overflow-hidden`), the resize handle (12px, transparent), and the Terminals island.
- **Collapsed Terminals bar:** 34px, rendered below the panel group with a 12px gap.

#### P3-1: Shell CSS recipes
- **Tier:** Haiku. **Depends on:** G2.
- **Files:** `src/styles/globals.css`.
- **Change:** Add these component classes, built from the excerpt above:
  - `.island`
  - `.island-topbar` (radius 12, `--shadow-topbar`)
  - `.island-title` (30px flex row, padding 0 12px, bottom border `border-subtle`, mono 10.5/uppercase/.08em/`muted`)
  - `.island-count` (mono 9.5, padding 0 5px, radius 5, `panel-4` background, `muted` text)
  - `.shell-ground`:
    ```css
    background: radial-gradient(ellipse at 40% 45%, transparent 48%, rgb(var(--well) / .55) 100%),
      radial-gradient(640px 380px at 38% 46%, rgb(var(--accent-solid) / .06), transparent 70%), rgb(var(--bg))
    ```
  - `.btn-ghost` (height 26, radius 6, `muted`; hover `panel-3` and `text`)
  - `.btn-bordered` (1px `border`, `panel-2` background; hover border `warn-solid`)
  - `.btn-primary` (`accent-solid` background, `accent-fg` text, radius 8, weight 600)
  - `.btn-accent-tint` (`accent`/12 background, `accent` text, 1px `accent`/35 border)
  - `.btn-danger-tint` (`danger-solid`/28 background, `danger` text, 1px `danger`/35 border)
  - `.chip` (radius 5, mono 9.5, padding 0 6px, line-height 16px)
  - `.icon-btn-20` (20×20, radius 6, 1px `border`, `panel-2` background, `muted` text)

  Delete `.react-flow__controls*` rules only in P3-8, not here.
- **Acceptance:** `CHECKS` pass. No visual change yet (the classes are unused).

#### P3-2: ProviderBadge component
- **Tier:** Haiku. **Depends on:** G2.
- **Files:** new `src/components/ui/ProviderBadge.tsx`; edit `src/features/terminal/BottomWorkspace.tsx`.
- **Change:**
  1. Create `ProviderBadge({ provider, size = 18 }: { provider: AgentProvider; size?: 16 | 18 | 20 })`. It renders a square monogram, `rounded-[5px] bg-panel-4 text-muted font-mono font-bold`, with font size 8.5px at 18px. Labels: Claude `CL`, Antigravity `AG`, Codex `CX`, Gemini `GM`.
  2. Replace the four inline monograms in `BottomWorkspace.tsx`: `ProviderMark`, `BranchAgentRow`, the runtime tile heading, and the wide-header mini tabs. Delete `ProviderMark`.
- **Spec:** "Agent provider badges are brand/data colors, not theme tokens. The mockup uses placeholder monogram badges (CL, AG, CX); real logos are to be supplied as assets." Use neutral tokens until real assets arrive.
- **Acceptance:** `grep -n "=== 'Codex' ? 'O'" -r src` returns nothing. `CHECKS` pass.

#### P3-3: AppShell skeleton (left rail, inset main column, ground)
- **Tier:** Sonnet. **Review:** Opus. **Depends on:** P3-1.
- **Files:** `src/components/layout/AppShell.tsx`, `src/features/inspector/Inspector.tsx` (outer `aside` classes only), `src/styles/globals.css` (media query).
- **Change:**
  1. Root: `relative h-full w-full overflow-hidden shell-ground`. `<TopBar />` stays first; it becomes absolute in P3-4.
  2. Body: `absolute inset-0` containing:
     - `<aside className="desktop-rail absolute left-4 top-[68px] bottom-4 w-[400px] flex flex-col gap-3">` holding `<Inspector />` (flex-1, min-h-0) and a placeholder slot for Branches (P3-7);
     - the main column `shell-main absolute left-[428px] right-4 top-[68px] bottom-4 flex flex-col`, holding the existing vertical `PanelGroup` unchanged in behavior.
  3. `MainWorkspace` keeps the literal `<main className="…">{children}</main>`. Remove `<Inspector />` from `MainWorkspace`. On the Inspector `aside`, replace `desktop-inspector … w-[310px] shrink-0 … border-l border-[rgb(var(--border))]` with `min-h-0 flex-1` (P3-5 adds the island recipe).
  4. The main panel content gets `rounded-[14px] overflow-hidden`.
  5. The dock panel content wrapper gets `pt-3`.
  6. The resize handle keeps its `id` and becomes `h-3 bg-transparent` with a centered 40px hover line in `border-strong`.
  7. Below 1100px: rename the class `.desktop-inspector` to `.desktop-rail` in the media query, and add `@media (max-width: 1100px) { .shell-main { left: 16px } }` so the main column takes the full width when the rail is hidden.
  8. The `gitError` banner moves into the main column above the panel group, styled as an island with `warn` text.
  9. Keep `reopenRef`, the collapsed pill behavior, all dialogs, `WorkspaceNotice`, and `Tooltip.Provider`.
- **Acceptance:**
  - `CHECKS` pass, including `AppShell.test.tsx` unchanged.
  - `pnpm test:e2e` passes, especially `inspector-layout`, `workspace-lifecycle`, `smoke`, and `render-isolation`.
  - `grep -c '{children}</main>' src/components/layout/AppShell.tsx` prints `1`.
  - The `canvas` and `inspector-worktree` screens show the left rail and an inset canvas.
- **Risks:**
  - `[data-panel-id="main-workspace"]` and `[data-panel-id="bottom-workspace"]` must keep their sizes (`0.0`, `68.0`, or `dockHeight`).
  - The Inspector stays `aside[aria-label=Inspector]`.
  - Do not add `role="status"` anywhere in the shell: `AppShell.test` expects exactly one status (the notice).

#### P3-4: Top bar islands
- **Tier:** Sonnet. **Depends on:** P3-3.
- **Files:** `src/components/layout/TopBar.tsx`.
- **Change:**
  - `header`: `pointer-events-none absolute inset-x-4 top-4 z-30 grid h-10 grid-cols-[1fr_auto_1fr] gap-3`. Each island gets `pointer-events-auto island island-topbar h-10 flex items-center gap-2.5 px-1.5`.
  - **Left island:**
    - brand: a 30px tile with `linear-gradient(145deg, rgb(var(--accent)), rgb(var(--accent-solid)) 48%, rgb(var(--accent-solid) / .55))`, radius 8, and `Sprout` in `accent-fg`;
    - a two-line wordmark: the text node `bonsai` (13px, 600), and below it `WEB CLIENT` (mono 8.5, .14em, `muted-2`);
    - then the workspace select styled as a 26px pill (dot `accent-solid`, `panel-2` background, 1px `border`, radius 8), `›` in `muted-2`, and the project select pill with a folder icon.
  - **Middle island:** the nav links as 30px tabs, radius 8, padding 0 12px, each with a 14px lucide icon (`LayoutGrid`, `Table`, `GitBranch`, `ScrollText`, `SlidersHorizontal`; if typecheck reports one as missing in lucide-react 0.544, pick the nearest existing icon). The active tab is `bg-panel-3 text-text font-medium`; others are `muted`. Remove the underline.
  - **Right island:** `RelayStatus` (dot plus mono 11 label), the Search button (ghost, with `Search` icon, `Search` text, and `bonsai-kbd`), the Bell icon button, and the account `TO` as a 26px circle with `accent-solid` background and `accent-fg` text.
  - `HeaderSelect` menu: `bg-panel-3 border-border-strong shadow-overlay rounded-[10px]`; the active option is `bg-accent/12`.
- **Acceptance:**
  - `CHECKS` and `pnpm test:e2e` pass. In particular, keep the `bonsai` exact text, the `Canvas`/`GitHub`/`Settings` link names, and the `Workspace` and `Project` button names with `aria-haspopup`.
  - The `canvas` screen top bar matches board L (y 16 to 56).
- **Risk:** Keep the `.top-nav-secondary` class on the nav, because the media query hides it below 800px.

#### P3-5: Inspector island with Inspector and Files tabs
- **Tier:** Sonnet. **Depends on:** P3-3. **Decision:** D2.
- **Files:** `src/features/inspector/Inspector.tsx`, `src/features/terminal/BottomWorkspace.tsx`, new `src/features/files/FilesDiffPanel.tsx`.
- **Change:**
  1. In `BottomWorkspace.tsx`:
     - move `FilesDiffPanel`, together with its helpers `FileTreeRows`, `GitStatus`, `countChanged`, and `filterTree`, into the new file. Give it a prop `embedded?: boolean`; when it is true, drop the `dock-pane` frame, the `dock-heading`, and the close button, but keep the Files/Git Diff `Tabs`;
     - remove the Files panel and its resize handle from the `BottomWorkspace` `PanelGroup`, and remove the `Files` toggle button from the runtime header (leave `rightPanels.files` in the store untouched);
     - update `panelIds` accordingly.
  2. In `Inspector.tsx`, the `aside` becomes `island flex min-h-0 flex-1 flex-col overflow-hidden`.
  3. The header becomes `.island-title` holding Radix `Tabs.List` with `Inspector` and `Files` triggers. Active trigger: `text-text` plus `box-shadow: inset 0 -2px 0 0 rgb(var(--accent-solid))`, spacing 22px.
  4. On the right of the header, a freshness label from `useBonsaiStore(s => s.syncFreshness[activeProjectId]?.state)`:
     - `ready` → dot `accent-solid`, text "live";
     - `stale` → `warn-solid`, "stale";
     - `loading` → `muted-2`, "syncing";
     - `error` or `unavailable` → `danger`, "offline".
     Mono 10, `muted-2`, **no** `role`.
  5. `Tabs.Content value="inspector"` contains the existing scroll area unchanged. `Tabs.Content value="files"` renders `<FilesDiffPanel embedded />`.
- **Acceptance:**
  - `CHECKS` pass.
  - `pnpm test:e2e` passes: `inspector-layout` (a `.inspector-*` element stays inside the viewport and scrolls) and `files.test.tsx`.
  - The `inspector-worktree` screen matches board L.
- **Risk:** Radix Tabs unmount inactive content by default. The Inspector content must stay the default tab, so that tests that look for `.inspector-section` find it.

#### P3-6: Inspector content restyle
- **Tier:** Sonnet. **Depends on:** P3-5.
- **Files:** `src/features/inspector/Inspector.tsx`, `src/styles/globals.css` (`.inspector-*` rules).
- **Change** (looks only; every label, handler, and section stays):
  - `.inspector-hero`: a 36px icon tile (`accent`/12 background, `accent` icon, radius 9) plus a mono 15/600 branch name in `h2`, a tag chip (`.chip`, `accent`/12 background, `accent` text), and a mono 10 `muted-2` line "from {mergeTargetBranch}". No gradient. Inside an island this becomes a flat `panel-2` card (radius 12, 1px `border`, padding 12).
  - `Metric` becomes a tile: `panel-2` background, 1px `border`, radius 10, padding 8 10. Label mono 9.5 uppercase .06em `muted-2`; value mono 13 600. Keep the existing tones: `warn` for uncommitted or behind > 0.
  - When a PR exists, the PR checks list becomes a **CI/CD card**:
    - title row `CI/CD` (mono 9.5 uppercase `muted-2`), with `{RUNNING|PASSING|FAILING} · {success} OF {n}` on the right, colored `accent` / `ok` / `danger`;
    - a segmented bar, one 4px segment per check with a 4px gap: success `ok`, running `accent-solid`, failed `danger`;
    - rows: status icon plus name 12px, and the status text right-aligned (mono 10 `muted-2`).
  - Pull request card: status chip right-aligned (`● OPEN` in `accent`; draft in `muted`; merged in `ok`), `#n` in `accent` mono plus the title in 13px, `branch → base` mono 10 `muted-2`, and `no conflicts` in `ok` when `mergeable !== false`, otherwise `conflicts` in `danger`.
  - QuickButton: `primary` becomes `.btn-primary` (34px); `danger` becomes `.btn-danger-tint`; the default becomes `.btn-bordered`. The standalone "Delete worktree" button uses `.btn-danger-tint`.
  - `.inspector-attention` and `.inspector-alert`: `warn-solid`/22 border and `warn-solid`/8 fill, title in `warn`.
  - The section `h3` becomes the mono 9.5 uppercase .08em `muted-2` style.
- **Acceptance:**
  - `CHECKS` pass (`Inspector.test.tsx`: role alert, buttons `Stop` and `Restart`).
  - `pnpm test:e2e` passes (`inspector`, `inspector-layout`, `smoke`, which clicks `Agent`).
  - Screen `inspector-worktree`.
- **Risk:** Keep the classes `.inspector-hero`, `.inspector-attention`, `.inspector-action`, `.inspector-count`, `.inspector-section`, and `.inspector-viewport`. Tests select them.

#### P3-7: Branches island in the left rail
- **Tier:** Sonnet. **Depends on:** P3-3 and P3-2.
- **Files:** new `src/features/branches/BranchesIsland.tsx`; edit `src/features/terminal/BottomWorkspace.tsx`, `src/components/layout/AppShell.tsx`, `src/stores/panelPreferences.ts`.
- **Change:**
  1. Move `BranchSidebar`, `BranchTreeItem`, `BranchAgentRow`, and `StatusDot` out of `BottomWorkspace.tsx` into the new file, rename the sidebar to `BranchesIsland`, and render it in the rail slot from P3-3.
  2. Remove the Branches panel and its handle from the `BottomWorkspace` `PanelGroup`.
  3. In `panelPreferences.ts` `load()`, accept layouts whose ids include `runtime` and are a subset of `['runtime','prs','files']`. Rows that contain `branches` are dropped, which gives default sizes once.
  4. Restyle as an island (`h-[min(296px,40vh)]`):
     - **Title row:** `GitBranch` icon in `ok`, "BRANCHES", `.island-count` with `index.count`, and a right-aligned `+` icon button with `aria-label="Add worktree"` that calls `setWorktreeDialogOpen(true)`.
     - **Rows:** 22px high, mono 11.5. The default branch row has a 7px `accent-solid` dot, its name in `accent`, and a `default` chip (`accent`/14 background).
     - **Tree guides:** each worktree row is prefixed with guides built from depth and last-child position: `├─ `, `└─ `, and `│  ` for every ancestor that still has siblings. Guide color `muted-2`/60. Pass `isLast` and `ancestorsHaveMore: boolean[]` down from `BranchTreeItem`.
     - **Right of each row:** a CI icon from `worktree.ciStatus` (passed `CircleCheck` `ok`; running `LoaderCircle` `accent` spinning; failed `CircleX` `danger`; else `Clock3` `muted-2`), 12px, plus `#{prNumber}` mono 10 `muted-2`.
     - **Selected row** (`dockWorktreeId`): `bg-accent/9 text-text`. Hover: `bg-panel-2`.
     - Keep the collapse chevrons (only on rows with children or agents), the agent rows (indented, with `ProviderBadge` 16), and the History line.
     - **Footer:** 26px, top border `border-subtle`. On the left, "{n} worktrees · {m} agents" mono 10 `muted-2`; on the right, a `+ new branch` text button in `accent` that calls `setWorktreeDialogOpen(true)`.
  5. Omit the "Filter branches" input; it would be new behavior.
- **Acceptance:**
  - `CHECKS` pass (`branchTree.test.ts`, `BottomWorkspace.test.tsx`, and `preferenceBoundary.test.ts` unchanged).
  - `pnpm test:e2e` passes.
  - The `canvas` screen matches the board L Branches island.
- **Risks:**
  - No button text may contain "New worktree": `smoke.spec` resolves that name to exactly one button.
  - Do not give rows `role="status"`.

#### P3-8: Canvas toolbar island
- **Tier:** Sonnet. **Depends on:** P3-3.
- **Files:** `src/features/workspace/canvas/BonsaiCanvas.tsx`, `src/styles/globals.css`.
- **Change:**
  1. Replace the two top-left and top-right toolbars and `<Controls>` with one island at `absolute right-0 top-0 z-10` (inside the canvas panel, which matches board L x ≈ 1294 to 1580 and y 72): `island island-topbar h-[30px] flex items-center gap-0.5 px-1`.
  2. Buttons are 24px ghost icon buttons in this order, with a 1×16px `border` divider between groups:
     - `aria-label="Fit"` (`Maximize` icon, existing fit handler);
     - `aria-label="Auto-layout"` (`Network`);
     - divider;
     - `aria-label="Zoom out"` (`Minus`, `zoomOut()` from `useReactFlow`);
     - `aria-label="Zoom in"` (`Plus`, `zoomIn()`);
     - `aria-label="Fit view"` (`Square`, `fitView()` with default options, as `Controls` did);
     - divider;
     - `New worktree` as a text button with a `Plus` icon, `.btn-accent-tint`, 24px high.
  3. Delete the `.react-flow__controls*` CSS rules.
- **Acceptance:** `CHECKS` and `pnpm test:e2e` pass (`Fit`, `Auto-layout`, and `New worktree` stay buttons with those exact names).
- **Risk:** `getByRole('button', { name: 'Fit', exact: true })` must not also match `Fit view`. Exact match keeps them distinct.

#### P3-9: Terminals collapsed bar
- **Tier:** Sonnet. **Depends on:** P3-3 and P3-2.
- **Files:** `src/components/layout/AppShell.tsx`.
- **Change:** Replace the floating "Open workspace" pill with an island bar, rendered only when `dockState === 'collapsed'`, as the last flex item of the main column (`mt-3 h-[34px] island rounded-[12px] flex items-center gap-2 px-3`). Contents:
  - a `TerminalSquare` icon in `ok`;
  - `TERMINALS` (`.island-title` text style, without the border);
  - `.island-count` with `openRuntimeIds.length`;
  - one mini tab per open runtime. Use the same lookup as `RuntimeWorkspace.openEntries`, extracted into a hook `useOpenRuntimeEntries()` in a **new file** `src/features/terminal/openRuntimeEntries.ts`, and use that hook from both places. Each tab is 24px: `ProviderBadge` 16 or a `TerminalSquare` icon, the name (12px, `text`), and a state dot (`accent-solid` running, `warn-solid` idle, `muted-2` otherwise). Clicking a tab calls `focusRuntime(id)` and then `setDockState('normal')`;
  - a flexible spacer;
  - the existing reopen button, now an icon-and-text ghost button `ChevronUp` + "Open workspace". It keeps `ref={reopenRef}` and the accessible name **Open workspace**.
- **Spec:** "Collapsed: 34px slim bar with the title, a count chip, one mini tab per session (provider badge, name, state dot), Open, and an expand chevron."
- **Acceptance:** `CHECKS` pass (`AppShell.test` focus test). `pnpm test:e2e` passes (`smoke`: minimize then `Open workspace`). Screen `canvas-dock-collapsed`.
- **Risks:**
  - The bar must sit outside `[data-panel-id="bottom-workspace"]`, because that panel stays size `0.0` and `inert` while collapsed.
  - `AppShell.test.tsx` replaces the whole `BottomWorkspace` module with a mock. Anything AppShell imports from that module is `undefined` in the test, which is why the hook lives in its own file.

#### P3-10: Terminals island, expanded
- **Tier:** Sonnet. **Depends on:** P3-5, P3-7, and P3-9.
- **Files:** `src/features/terminal/BottomWorkspace.tsx`, `src/features/terminal/AgentTerminal.tsx`, `src/features/terminal/ProcessTerminal.tsx`, `src/styles/globals.css` (`.dock-*` and `.runtime-*` rules).
- **Change** (see `terminals-expanded.png`):
  - `RuntimeWorkspace` `section.dock-pane` becomes `island`. `.dock-heading` becomes a 34px title row: `TerminalSquare` in `ok`, `TERMINALS`, the count chip, the mini tabs (wide header only, as now), the `+ Open` `BonsaiSelect` (compact), `PRs` toggle, divider, `Minimize workspace`, and `Maximize workspace` (keep the titles).
  - Panes (`.runtime-tile`): `well` background, 1px `border-subtle`, radius 10. The 26px `.runtime-heading` holds: the grip, `ProviderBadge` 18 or a process icon, the title 12px/600 `text`, a meta line inline (mono 10 `muted-2`: profile or model for agents, the command for processes), then the actions host, the state dot, and the close button (`.icon-btn-20`, `aria-label="Close runtime card"`).
  - `.runtime-tile-active`: border `accent-solid`/40.
  - `AgentTerminal` portal: the connection chip becomes the "running" chip (`.chip`, `accent`/12 background, 1px `accent`/35 border when connected; `panel-4`/`muted` otherwise). `Stop agent` becomes `.icon-btn-20` with `aria-label="Stop agent"` and `title`.
  - `ProcessTerminal` toolbar row becomes a 26px footer *below* the xterm host: status text mono 10 `muted-2` on the left; `Reconnect output`, `Stop process`, `Restart process`, and `Download retained logs` as `.btn-ghost` 20px text buttons on the right, keeping their visible text.
  - Delete the `.dock-tab` rules once nothing uses them (`grep`).
  - The PRs panel (`PullRequestsPanel`) keeps its structure but gets the island recipe.
- **Acceptance:**
  - `CHECKS` pass (`BottomWorkspace.test`: `Open runtime`, `Close runtime card`).
  - `pnpm test:e2e` passes: `process-terminal-fullstack`, `agent-terminal-fullstack`, `execution-controls`, `workspace-lifecycle` (it fills the PR search in the dock, so keep the textbox name `Search pull requests`).
  - The `canvas` screen with an open runtime matches `terminals-expanded.png`.
- **Risk:** Keep `.runtime-tile`, `.runtime-tile-active`, `.runtime-heading`, `.dock-pane`, `.bottom-workspace`, `[data-process-id]`, and the `.xterm*` DOM. Tests select them.

**Gate G3.**

### Phase 4: canvas nodes

Shared rules for this phase:
- Each task that changes a node's size updates the **same node's** `estimateNodeSize` entry in `layout/geometry.ts`, and nothing else in `layout/`.
- Keep the `Handle` ids and positions, `MoveSubtreeGrip`, the context menus, and every accessible name.
- Card shadow is `--shadow-card`.

#### P4-1: Worktree card
- **Tier:** Sonnet. **Depends on:** G2. **Decision:** D3.
- **Files:** `src/features/workspace/nodes/BonsaiNode.tsx` (worktree branch of `NodeShell`, plus `CiBadge` and `PrBadge`), `layout/geometry.ts` (worktree entry).
- **Spec:** "Worktree card, 312×66 [D3: 300 wide], radius 12, background `panel`, border `border` (selected: `accent` at 55% plus a 3px `accent` at 10% ring)."
  - **Row 1:** branch icon (13px, `muted-2`), name (mono 12px, 600, `text`, truncate). On the right: the tag chip (`.chip` with the existing tag foreground, border, and background) and, when `connectionLabel` is set, that label in `warn` mono 10. The mockup's diff counts are omitted (no data).
  - **Row 2** (20px, margin-top 6px):
    - CI badge: 22×20, icon only, radius 5; running spins. Tints: running `accent`/12 with `accent` icon; passed `ok`/10 with `ok`; failed `danger-solid`/28 with `danger`; other states `panel-3` with `muted-2`. Keep the text as `title` and `aria-label` (for example "CI failed · 2").
    - PR badge: icon plus `#n`, mono 10.5, radius 5, height 20. Draft: `panel-3`/`muted`. Open: `accent`/12 and `accent`. Merged: `ok`/10 and `ok`. Closed: `panel-3`/`muted-2`.
    - Ahead/behind from `subtitle` data: render `↑{ahead} ↓{behind}` mono 10 `muted-2`, with the down count in `warn` when above 0. Pass `ahead` and `behind` numbers in the node data: add `ahead` and `behind` fields in `buildCanvasGraph` next to `subtitle` (this is a presentation field only).
    - On the right: `→ {mergeTargetBranch}` mono 10, in `ok` when the target is a worktree branch and `muted-2` when it is the default branch. Add a `targetIsWorktree: boolean` field in `buildCanvasGraph`, computed from the existing `branchToWorktree`.
  - Remove the "n agents · n processes" footer (the canvas shows those nodes) and the separate "target" row.
  - **History:** when `historyItems` exist, render a 26px footer row (top border `border-subtle`; `History` icon, the text "History", count chip, `Show`/`Hide`). Its accessible name must still match `/History.*Show/`. The rows are 24px each.
  - Update the estimate to `{ width: 300, height: 66 + (historyItems?.length ? 26 + historyItems.length * 24 : 0) }` and keep the existing comment's intent.
  - Selected state: `border-accent/55` plus `shadow-[0_0_0_3px_rgb(var(--accent)/.10),var(--shadow-card)]`. Hover: `border-border-strong`.
- **Acceptance:**
  - `CHECKS` pass (`globalLayout.test.ts` and `localPlacement.test.ts` may assert exact sizes. If one fails *only* because of the new estimate numbers, update those expected numbers and list them in the PR. Any other failure means stop).
  - `pnpm test:e2e` passes: `auto-layout`, `smoke` (stack expand keeps unrelated nodes fixed), `worktree-management`, and `inspector` (History).
  - Screen `canvas`.
- **Risk:** Expanded History must fit inside the estimate, or Auto-layout overlaps nodes. Check it with Playwright: the History-expanded node height must be at most the estimate.

#### P4-2: Process node
- **Tier:** Sonnet. **Depends on:** G2.
- **Files:** `src/features/workspace/nodes/ProcessNode.tsx`, `src/stores/projectSelectors.ts` (add `port` to `processNodeSelector`'s picked fields), `layout/geometry.ts` (process entry).
- **Spec (with the board correction):** "Process node, 153×54, radius 10, background `bg` [board: `#1c110b`], border `panel-3` [failed: `warn` at 40%]."
  - **Header** (20px): a 7px state square, radius 2 (running `accent-solid`; failed or lost `warn`; starting, backoff, or stopping shows the 8px spinner in `accent`; otherwise `muted-2`). Then the name (mono 11px, 600), and the port on the right (mono 10px, `ok`) when `process.port` is set. Add `<span role="status" className="sr-only">{statusLabel}</span>` right after the square; it must be the **only** `role="status"` in the node.
  - **Second row:** on the left, either `error` (`role="alert"`, mono 9.5 `warn`, truncate, `title`), or else `{statusLabel} · {detail}` (mono 9.5 `muted-2`; `warn` when failed). On the right, **three** `.icon-btn-20` buttons that always render:
    - `aria-label="Open output"` (`TerminalSquare`);
    - `Stop process` (`Square`), disabled unless `canStop`;
    - `Restart process` (`RotateCcw`), disabled unless `canRestart`, and highlighted when failed: `border-warn-solid/55 text-warn bg-warn-solid/14`.
  - Keep the `title` (full command) on the root, and keep `data-process-node-id`.
  - Update the estimate to `{ width: 153, height: 54 }`.
- **Acceptance:** `CHECKS` and `pnpm test:e2e` (`process-nodes`, `execution-controls`, `render-isolation`) pass. Screen `canvas`.
- **Risks:**
  - The spec's per-state *pairs* of buttons would remove `Stop process` in some states. `process-nodes.spec` expects that button to exist (disabled) in failed, stopped, lost, done, and stopping. So keep all three buttons.
  - The test reads `node.getByRole('status')`, which gives a strict-mode error if there are two.

#### P4-3: Agent node
- **Tier:** Sonnet. **Depends on:** P3-2.
- **Files:** `BonsaiNode.tsx` (agent branch), `layout/geometry.ts` (fallback/agent entry).
- **Spec (with the board correction):** "Agent node, 153×54, radius 10, background `panel-2` [board `#2b1d16`], border `border-strong` (waiting for the user: `warn-solid` at 55%)."
  - **Header:** `ProviderBadge` 18, the name (sans 11.5px, 600), and a state dot on the right (6px with a 3px halo: running `accent-solid` with halo `accent-solid`/20; idle meaning "waiting" `warn-solid` with halo `warn-solid`/25 and the border change; finished `muted-2` with halo `muted-2`/15).
  - **Second row:** `task` (mono 9.5, `muted-2`; `warn` when idle, truncate), plus one `.icon-btn-20` `aria-label="Open terminal"` (`TerminalSquare`) that calls `openTerminal(entityId)`. It is disabled when `agent?.providerId !== 'antigravity'`, exactly like the menu item. Pause, resume, and reply are omitted (no actions exist).
  - Move the model, reasoning, and runtime to the root `title`.
  - Update the estimate fallback to `{ width: 153, height: 54 }`.
- **Acceptance:** `CHECKS` and `pnpm test:e2e` (`inspector`, `storage-migration`, `execution-controls`) pass. Screen `canvas`.

#### P4-4: Hub (project node) and satellite offsets
- **Tier:** Sonnet. **Depends on:** G2. **Decision:** D4.
- **Files:** `BonsaiNode.tsx` (project branch), `canvas/buildCanvasGraph.ts` (satellite positions and project data), `layout/geometry.ts` (project entry).
- **Spec:** "Default-branch hub node, 370×86, radius 14, border `accent-solid` at 55% plus a 4px ring at 8%." (Shadow from the board: `0 0 0 4px rgb(var(--accent-solid)/.08), inset 0 1px 0 rgb(var(--accent-solid)/.16), 0 14px 28px -16px rgb(10 7 5/.85)`.)
  - **Header:** branch icon in `accent`, `data.defaultBranch` (mono 13px, 600), a `DEFAULT` chip (`accent-solid`/14 background, `accent` text, mono 9.5, .04em), and on the right a labelled CI badge (`.chip` with an icon and `data.ciSummary` text; `danger` when the summary contains "failed", `accent` when "running", otherwise `ok`).
  - **Line 2:** `{title} · {subtitle}` (project name and repository), mono 10 `muted-2`, truncate.
  - **Footer** (one line, no wrapping, which fixes the mockup glitch): the 4 stats as `{value} {label}` separated by ` · `, mono 10 `muted-2`; a non-zero `ci failed` count is `danger`. No pipeline or merge queue (no data).
  - The node stays `type: 'project'` (class `.react-flow__node-project`). Keep the handles.
  - In `buildCanvasGraph`: change the default-branch satellite to `x: rootDefault.x - (232 + 34)` (unchanged unless P4-5 changes its width), and change env to `{ x: rootDefault.x + 370 + 34, y: rootDefault.y + 15 }`.
  - Update the estimate to `{ width: 370, height: 86 }`.
- **Acceptance:** `CHECKS` and `pnpm test:e2e` (`smoke` clicks `.react-flow__node-project`, then `Agent`; `.env` stays visible) pass. Screen `canvas`: env does not overlap the hub.

#### P4-5: Satellites, stack, and runtime shelf
- **Tier:** Sonnet. **Depends on:** P4-4.
- **Files:** `BonsaiNode.tsx` (`DefaultBranchCard`, `EnvCard`, `StackCard`, `MenuItem`, context menu content), `ProcessNode.tsx` (`ProcessShelfNode` and the menus).
- **Change:** Tokens and recipe only. Keep the sizes, so the estimates do not change.
  - `DefaultBranchCard`: `panel` card, 1px `border`, radius 12, `--shadow-card`; header icon tile `accent`/12; keep the content.
  - `EnvCard`: `panel` card, radius 10; icon tile `warn-solid`/14 with `warn` icon; hover border `warn-solid`.
  - `StackCard`: `panel` card, radius 12; header inset bar in the tag color as now; rows keep a 37px height; dividers `border-subtle`.
  - Context menus (both files): `bg-panel-3 border-border-strong rounded-[10px] shadow-overlay p-1`. Item highlight: `bg-accent/12 text-text`. Separators: `bg-border-subtle`.
  - Placeholder slots ("+ process", "+ agent") from the spec are omitted. They would add nodes, which is a graph change; listed as a follow-up.
- **Acceptance:** `CHECKS` and `pnpm test:e2e` (`smoke` "Expand", `worktree-management`) pass.

#### P4-6: Edges and handles
- **Tier:** Sonnet. **Depends on:** P1-5.
- **Files:** `canvas/buildCanvasGraph.ts` (edge `style` and marker fields only), `canvas/PullRequestMergeEdge.tsx`, `BonsaiNode.tsx` and `ProcessNode.tsx` (handle classes).
- **Spec:** "Worktree to default branch: 2px `accent-solid` line with an arrow into the top of the hub." In the current layout the hub sits *above* the worktrees, so the arrow goes at the hub end. "Worktree stacked on another worktree: 2px `ok`… arrow into the left edge of the base card."
  - Non-nested hierarchy edges (`structure:*` with a visible style): `{ stroke: 'rgb(var(--accent-solid) / .7)', strokeWidth: 2 }` and `markerStart: { type: MarkerType.ArrowClosed, width: 14, height: 14, color: 'rgb(var(--accent-solid))' }`. The source is the project.
  - Default edge: `rgb(var(--accent-solid) / .45)` at width 1.5.
  - Agent and process edges: `rgb(var(--border-strong))` at width 1; keep the dash for finished agents.
  - PR merge edge: `stroke: 'rgb(var(--ok) / .8)', strokeWidth: 2`, no dash. Keep `getBezierPath` and `curvature: 0.34` exactly, because `prLabels.ts` samples that same curve. Add `markerEnd` with `color: 'rgb(var(--ok))'`.
  - Handles: delete the per-handle `!border-[…] !bg-[…]` classes and keep only the size classes. The colors now come from the `--xy-handle-*` variables (P1-5). The PR handles keep `!h-2.5 !w-2.5` and get `!border-ok/60`.
- **Acceptance:** `CHECKS` and `pnpm test:e2e` (`auto-layout` "readable PR labels", `render-isolation`) pass. Screen `canvas`: arrowheads render in the correct color.
- **Risk:** React Flow derives marker ids from the marker props, including `color`. If arrowheads do not render with `rgb(var(...))` colors, set the marker `color` to a fixed placeholder and color them in CSS instead: `.react-flow__edge.edge-hub .react-flow__arrowhead * { stroke: rgb(var(--accent-solid)); fill: rgb(var(--accent-solid)); }`, with `className: 'edge-hub'` on those edges. Report which approach you used.

#### P4-7: PR label chip
- **Tier:** Haiku. **Depends on:** P4-6.
- **Files:** `canvas/PullRequestMergeEdge.tsx`.
- **Spec:** "A PR label chip ('PR #26 → feat/passkeys', mono 10px, 18px high, pill)," colored `ok`.
- **Change:**
  - The outer `button` keeps `style={{ ...PR_LABEL_SIZE, transform }}` (that is the layout footprint) but becomes transparent and borderless, with `grid place-items-center`.
  - Inside it, render a pill: `h-[18px] max-w-full rounded-full border border-ok/40 bg-bg px-2 font-mono text-[10px] text-ok truncate` with the text `PR #{n} → {targetBranch}`.
  - Keep `data-pr-edge-label`, the `title`, and the `onClick`.
  - The leader line stroke becomes `rgb(var(--ok) / .5)`.
- **Acceptance:** `CHECKS` and `pnpm test:e2e` (`auto-layout`, `smoke` "#23") pass.

#### P4-8: Canvas ground
- **Tier:** Haiku. **Depends on:** P3-3.
- **Files:** `BonsaiCanvas.tsx`.
- **Change:**
  - The host `div` uses `bg-bg` plus the vignette: `style={{ backgroundImage: 'radial-gradient(ellipse at 40% 45%, transparent 48%, rgb(var(--well) / .55) 100%), radial-gradient(640px 380px at 38% 46%, rgb(var(--accent-solid) / .06), transparent 70%)' }}`.
  - The dot grid is already 22px (P0-5).
- **Acceptance:** `CHECKS` pass. Screen `canvas`.

#### P4-9 (optional): Selected-path highlight
- **Tier:** Sonnet. **Review:** Opus. **Depends on:** P4-6.
- **Spec:** "The selected path (selected worktree to its base to the default branch) is highlighted in `accent`," at 2.4px.
- **Change:**
  1. Add a pure function `selectedPathEdgeIds(edges, selectedNodeId): Set<string>` in `canvas/selectedPath.ts`, with a unit test. It walks the `structure:` edges upward with `getStructuralParentMap`, and includes `pr:` edges from the selected node up to its base.
  2. Render through custom edge components that subscribe to `useBonsaiStore(s => s.selection)` and a memoized set. Do **not** rebuild the `edges` array on selection.
- **Acceptance:** `render-isolation.spec` "selection-only" still reports `graph: 0` and `labels: 0`. `CHECKS` and `pnpm test:e2e` pass.
- **Risk:** Rebuilding `edges` on selection reruns `createPrLabelSelector` and fails render isolation.

**Gate G4.**

### Phase 5: remaining screens (tokens and recipes only; no structural redesign)

The shared recipes are listed below. Keep every accessible name and label.
- **Modal:** scrim `bg-well/70 backdrop-blur-[2px]`; panel `bg-panel border border-border-strong rounded-[14px] shadow-overlay`; header 44px with bottom `border-subtle`; title 13px/600; subtitle mono 10 `muted-2`.
- **Input:** `bg-well border border-border rounded-[7px] h-8 px-2.5 text-[12px] placeholder:text-muted-2 focus:border-accent/55`.
- **Popover:** `bg-panel-3 border border-border-strong rounded-[10px] shadow-overlay`.

#### P5-1: Popovers and command palette
- **Tier:** Sonnet. **Depends on:** G2.
- **Files:** `components/ui/BonsaiSelect.tsx` (menu, trigger, options), `features/command-palette/CommandPalette.tsx`.
- **Change:**
  - BonsaiSelect: trigger uses the input recipe; menu uses the popover recipe; the active option is `bg-accent/12`; option meta is mono 10 `muted-2`.
  - Palette: modal recipe with width 620; input 44px; items 32px with `aria-selected` styling `bg-accent/12 text-text`.
- **Acceptance:** `CHECKS` and `pnpm test:e2e` (`smoke` checks the merge-target menu stays inside the viewport; `[cmdk-root]`) pass. Screens `command-palette` and `dialog-create-worktree`.

#### P5-2: Dialogs
- **Tier:** Sonnet. **Depends on:** P5-1.
- **Files:** `CreateWorktreeDialog.tsx`, `DeleteWorktreeDialog.tsx`, `StartAgentDialog.tsx`, `StartProcessDialog.tsx`, `EnvEditor.tsx`, and `EditorPreferenceDialog` in `BottomWorkspace.tsx`.
- **Change:**
  - Apply the modal and input recipes.
  - Primary submit becomes `.btn-primary`; destructive becomes `.btn-danger-tint`; secondary becomes `.btn-bordered`.
  - Segmented choices (for example Existing / Remote / New branch, and provider pickers) are 30px tabs: active `bg-panel-3 text-text`, inactive `muted`.
  - Replace the remaining `shadow-[…rgb(0_0_0/…)]` values with `shadow-overlay`.
- **Acceptance:** `CHECKS` pass (`StartAgentDialog.test`, `StartProcessDialog.test`, `worktreeDialogs.test`). `pnpm test:e2e` passes. Screen `dialog-create-worktree`.

#### P5-3: Pull Requests page
- **Tier:** Sonnet. **Depends on:** P3-1.
- **Files:** `features/github/PullRequestsPage.tsx`, `PullRequestTabs.tsx`.
- **Change:** The page body sits in an island inside the main column (`island h-full`). Lists use row hover `panel-2` and selected `accent`/9. Status chips follow the section 0 mapping. Review buttons: Approve uses `ok` tint, Request changes uses `.btn-danger-tint`, Comment uses `.btn-primary`.
- **Acceptance:** `CHECKS` and `pnpm test:e2e` (`smoke` GitHub flow, `checkKeys.test`) pass. Screen `github`.

#### P5-4: Board (Table view)
- **Tier:** Sonnet. **Depends on:** P2-5 and P3-1.
- **Files:** `features/board/BoardPage.tsx`.
- **Change:** Island page frame; columns are `panel-2` cards (radius 12, 1px `border`); cards are `panel` with `--shadow-card`; tag colors come from `tagPalette` (P2-5); the drag overlay uses `shadow-overlay`.
- **Acceptance:** `CHECKS` and `pnpm test:e2e` pass. Screen `table`.

#### P5-5: Settings, Logs, Files, and notices
- **Tier:** Haiku. **Depends on:** P3-1.
- **Files:** `features/settings/SettingsPage.tsx`, `ProjectRootsSettings.tsx`, `features/logs/LogsPage.tsx`, `features/files/FilesPage.tsx`, `components/layout/WorkspaceNotice.tsx`, `WorkspaceStorageError.tsx`, and the setup overlay in `app/ApplicationRoot.tsx`.
- **Change:**
  - Page frames become `island`. Page headers use the `.island-title` style.
  - Inputs use the input recipe; buttons use the P3-1 classes.
  - The notice toast uses the popover recipe with an `Info` icon in `muted`.
  - The storage error uses a `warn-solid`/22 border, `warn-solid`/8 fill, and `warn` text.
- **Acceptance:** `CHECKS` and `pnpm test:e2e` (`settings`, `storage-migration`, `ProjectRootsSettings.test`, `FilesPage.test`) pass. Screens `settings` and `logs`.

**Gate G5.**

### Phase 6 (optional): ASCII bonsai

#### P6-1: Static ASCII asset
- **Tier:** Sonnet. **Depends on:** G4.
- **Files:** new `web/scripts/gen-ascii-bonsai.py` (dev-only, not bundled) and generated `src/features/workspace/canvas/asciiBonsai.ts`.
- **Change:**
  1. Port only the tree section of `docs/handoff/ascii-bonsai.reference.py`: `random.seed(41)`, the `put`, `blob`, and `center` functions, the trunk loop, the four `blob()` calls, and the nameplate rows. Drop all the HTML and card code. Crop the grid to the bounding box of the non-space cells.
  2. Emit `export const ASCII_BONSAI: Array<Array<[text: string, role: 'leaf' | 'leaf-bright' | 'leaf-dim' | 'wood' | 'knot' | 'plate']>>`, one entry per row, with consecutive same-role characters merged into runs. Color map: `#4ade80` → `leaf`, `#6bfb9a` → `leaf-bright`, `#2fa35c` → `leaf-dim`, `#c58a62` → `wood`, `#a86f4b` → `knot`, `#f5ded4` → `plate`.
  3. Replace the project name in the nameplate with a placeholder token `{project}`.
- **Acceptance:** Running `python3 -I web/scripts/gen-ascii-bonsai.py > /tmp/x.ts` and diffing against the committed file shows no difference. `CHECKS` pass.

#### P6-2: Decoration overlay
- **Tier:** Sonnet. **Depends on:** P6-1.
- **Files:** new `canvas/AsciiBonsai.tsx`; edit `BonsaiCanvas.tsx` and `globals.css`.
- **Change:**
  - A screen-space overlay at the bottom-left of the canvas panel: `pointer-events-none select-none absolute left-6 bottom-6 z-0`, `aria-hidden`, `opacity: .62`, `font-mono text-[11px] leading-[14px] whitespace-pre`. Render it *before* `<ReactFlow>` and give `<ReactFlow className="relative z-[1]">`, so nodes paint over it. The `.react-flow` background is transparent after P1-5.
  - Classes: `.ab-leaf { color: rgb(var(--accent-solid)) }`, `.ab-leaf-bright { color: rgb(var(--accent)) }`, `.ab-leaf-dim { color: rgb(var(--accent-solid) / .6) }`, `.ab-wood { color: rgb(var(--warn) / .7) }`, `.ab-knot { color: rgb(var(--warn) / .55) }`, `.ab-plate { color: rgb(var(--text)) }`.
  - The nameplate text is the active project name.
  - Hide the overlay when the canvas panel is under 900px wide or under 520px tall (a `ResizeObserver` on the host).
  - It is not linked to the hub (that is a layout-track item). It is fixed to the screen, not to the graph, so nodes can pan over it.
- **Acceptance:** `CHECKS` and `pnpm test:e2e` (`render-isolation`: no extra commits on selection, because the overlay reads only `activeProjectId`) pass. Screen `canvas`.

---

## 5. Review gates

- **G0** (after P0-5). Human review of the `canvas`, `inspector-worktree`, and `connect-gate` screens. This is the cheapest point to catch a palette or font problem: the whole app is already recolored.
- **G1** (after P1-5). Opus or human review of `src/theme/`: is it CSP-safe (no `<style>`, no inline script), does it fall back on corrupt storage, does the CSS parity test exist? Also read the P1-4 CSP finding and decide whether P1-5's xterm CSS block was needed.
- **G2** (after P2-7). All the vocabulary greps return nothing, and `pnpm test:e2e` passes. Spot-check the 8 `ok` sites and the 4 Draft sites against Appendix C.
- **G3a** (after P3-3, before the rest of Phase 3). Opus or human review of the shell skeleton against D1, with all of e2e green. Every later shell task builds on it.
- **G3** (after P3-10). Human review against `board-L.png` and `terminals-expanded.png`.
- **G4a** (after P4-1). Review the worktree card, since it sets the node pattern, and the estimate math (Auto-layout spacing in the `canvas` screen after clicking Auto-layout).
- **G4** (after P4-8 or P4-9). Human review of the canvas against board L.
- **G5** (after P5-5). Final sweep: the greps from P2-7 plus `grep -rn "rgb(0_0_0" src` returning nothing, and the full e2e suite.

---

## 6. Out of scope and follow-ups

- **Theme picker UI.** Lists `getThemes()` and calls `setActiveTheme()`. The store is ready; this needs only a Settings section.
- **Theme editor.** A token-by-token color form that uses `validateTheme` and shows errors and warnings; saves with `saveCustomTheme`.
- **Light theme.** One more built-in `Theme` with `mode: 'light'`. It needs a light variant of the `--shadow-*` constants and a review of the vignette, which assumes dark.
- **Sharing themes.** Export and import a `Theme` as JSON, validated by `validateTheme`. Daemon-backed `ThemeStorage` for sync across browsers.
- **Hub-and-lanes layout (canvas layout track).** Plug it in as a new strategy behind `computeGlobalPlacements(nodes, edges, placements)` in `canvas/layout/globalLayout.ts`, the single entry point that Auto-layout and first-project placement call:
  - the hub at the bottom center;
  - three 312-wide lanes at x = 30, 422, and 814;
  - tiers stacked upward by `buildBranchForest` depth (tops at y = 484, 238, 52 in an 830px canvas);
  - runtime nodes in 2 columns, which means changing the 3-per-row shelf in `globalLayout.ts` and `geometry.ts:getRuntimeShelfSize`.

  The same track should also: merge the `project` and `defaultBranch` nodes into one hub; draw the stacked-worktree "rail" path (which needs `prLabels.ts:curvePoint` to sample the new path); add placeholder slot nodes; link the ASCII bonsai to the hub; and either use `elkjs` or remove it, since nothing imports it today.
- **Full-bleed canvas behind the islands** (the alternative to D1). Needs per-side `fitView` padding (xyflow 12.12 supports `{ top, left, … }` in px) at the 4 `fitView` calls, `canvasReveal` bounds that exclude the rail, and route-aware page placement.
- **Data the mockup shows but the app does not have:** worktree diff `+n −n`; hub CI/CD pipeline segments and merge queue; process uptime and memory; agent pause, resume, and reply actions plus a true "waiting for user" state; PR reviewers; a Branches filter.
- **xterm under the production CSP.** If P1-4 confirms that the injected `<style>` elements are refused, the dimension styles are refused too. Evaluate whether the WebGL or canvas renderer addon is worth the size, or whether the CSP should change. This is a security decision for the owner.
- **Marketing landing page** (`src/marketing/`). It has its own palette and fonts and is untouched.
- **Converting the remaining arbitrary-value classes** to semantic Tailwind classes. That would be a codemod; it is optional and does not affect behavior.

---

## 7. Decisions for you (with my recommendation and the assumption I proceed with)

1. **D1: Shell structure.**
   - *Recommended:* an inset layout. The islands are positioned in a grid, and the canvas fills its own rounded, borderless region (left 428, top 68, right 16, bottom 16). The ground around it is `bg` plus a vignette with no dots.
   - *Why:* it keeps `react-resizable-panels`, the dock states, `fitView` and `canvasReveal` math, and route placement unchanged, so there is zero behavior risk.
   - *Cost:* the dot grid stops at the canvas region instead of running under the top bar and the rail.
   - *Alternative:* a true full-bleed canvas under overlay islands (see follow-ups); about four extra Sonnet tasks plus an Opus review.
2. **D2: Files moves into the Inspector's "Files" tab.**
   - *Recommended:* yes, and remove the dock's Files toggle, leaving the `rightPanels.files` preference unused but in place. The mockup's Terminals island has no Files panel.
   - *Alternative:* keep both, which duplicates the same panel in two places.
3. **D3: Worktree card width.**
   - *Recommended:* 300px instead of the mockup's 312px.
   - *Why:* layouts saved today place sibling cards 230 + 72 = 302px apart, so a card up to 300px wide never overlaps a saved placement. At 312px, users with saved layouts see 10px overlaps until they click Auto-layout.
   - *Alternative:* 312px plus a release note to run Auto-layout.
4. **D4: The hub is the restyled `project` node.**
   - *Recommended:* show the default branch name with a DEFAULT pill and use the stats as the footer, because there is no pipeline or merge-queue data. The separate `defaultBranch` (latest commit) and `env` satellites stay, restyled. Merging the nodes changes the graph, so it waits for the layout track. Arrows point into the hub's *bottom* while the current layout keeps the hub on top.
   - *Alternative:* hide the `defaultBranch` satellite now. That is a graph change and may shift saved layouts.
5. **D5: Tailwind semantic colors, replacing the default palette.**
   - *Recommended:* yes (P2-6). Class names get shorter, `red-400`-style drift becomes impossible, and the token list has one source.
   - *Alternative:* keep only `rgb(var(--x))` arbitrary values, with no config change but much longer markup in Phases 3 to 5.

Calls I made without asking:
- Delete dead `FakeTerminal.tsx` (and its test) and `ProjectSidebar.tsx`.
- Leave the marketing page alone.
- Map agent `idle` to the spec's "waiting" color (`warn-solid`), preserving today's meaning.
- Keep all three process-node buttons (the tests require them).
- Omit every mockup element that has no backing data or action, instead of faking it.

---

## Appendix A: Spec corrections (board L wins)

1. The process node background is `bg` (`#1c110b`), not `well`. A failed process uses border `warn` at 40%.
2. The agent node background is `#2b1d16`; use `panel-2` (`#291d17`), which is visually identical. Same for the 20px icon buttons on nodes and the hub.
3. The tree guide characters and the Branches row icons use `#5a6a5c`; use `muted-2` at 60%. No new token.
4. `#2fa35c`, `#c58a62`, and `#a86f4b` appear only in the ASCII bonsai. They map to `accent-solid` at 60%, `warn` at 70%, and `warn` at 55% (P6-2).
5. `--blue` maps to running semantics (CI running, process starting), so it becomes `accent`. Only "Merged" becomes `ok`. The spec's "`--blue` → `ok`" is right for the Phase 0 alias only.
6. Hub, legend, and chip wrapping glitches: P4-4 enforces a single non-wrapping footer line. The legend is not built (there is no legend today); listed as a follow-up with the layout track.
7. The terminal pane footer ("> no clipboard tool found | Gemini 3.8 Flash · high") has no data source. The process footer shows status plus controls; agent panes have no footer.

## Appendix B: Test hooks to preserve

- **Classes:** `.react-flow__node-{project,worktree,stack,agent,process,env}` (keep the node `type` strings), `.runtime-tile`, `.runtime-tile-active`, `.runtime-heading`, `.dock-pane`, `.bottom-workspace`, `.inspector-hero`, `.inspector-attention`, `.inspector-action`, `.inspector-count`, `.inspector-section`, `.inspector-viewport`, `.xterm*`.
- **Attributes:** `[data-panel-id="main-workspace"]`, `[data-panel-id="bottom-workspace"]` (with `data-panel-size` 0.0, 68.0, or `dockHeight`), `[data-panel-resize-handle-id="workspace-resize"]`, `[data-process-id]`, `[data-process-node-id]`, `[data-pr-edge-label]`, `[data-bonsai-select-menu]`, `[data-radix-scroll-area-viewport]`, `[cmdk-root]`, `rf__node-*` test ids.
- **Accessible names and roles:**
  - Inspector: `complementary` "Inspector", heading "Needs attention", `h2` inside the Inspector.
  - Buttons: "Open workspace", "Minimize workspace" / "Maximize workspace" (titles), "New worktree" (exactly one button), "Fit", "Auto-layout", "Open runtime", "Close runtime card", "Open output", "Stop process", "Restart process", "Stop agent", "Download retained logs", "Agent", "Start agent", "Start process", "Delete worktree", `/Expand/`, `/History.*Show/`.
  - The project `menu` "Process actions" with its `menuitem` names.
  - `status`: exactly one per process node, and only the notice inside AppShell.
  - Textbox "Search pull requests"; links "Canvas", "GitHub", "Settings"; the exact text `bonsai`; the "Workspace" and "Project" buttons.
  - The exact text "Pull requests" inside the PRs `.dock-pane`, and that panel's id `prs` (`#prs`).
- **Source literals patched by the e2e build:** `{children}</main>` (`AppShell.tsx`), `const { project,` (`buildCanvasGraph.ts`), `const rects = new Map` (`layout/prLabels.ts`).

## Appendix C: Old variable classification (687b3aa)

- **`--green` to `--ok`:** `Inspector.tsx:137,163`; `BonsaiNode.tsx:116`; `PullRequestsPage.tsx:28,227,245`; `BottomWorkspace.tsx:526`.
- **`--green` to `--accent-solid`** (solid dots): `TopBar.tsx:116`; `StartAgentDialog.tsx:139` (the dot); `BonsaiNode.tsx:94,256`; `BottomWorkspace.tsx:58`.
- **`--green` to `--accent`:** all remaining sites (running, connected, open PR, default branch, untracked files, `+additions`).
- **`--purple` Draft to `--muted` on `--panel-3`:** `BonsaiNode.tsx:145`; `PullRequestsPage.tsx:135,164`; `BottomWorkspace.tsx:661`. All other `--purple` uses become `--accent`.
- **`--orange`:** solid dots at `TopBar.tsx:118`, `BottomWorkspace.tsx:60`, and `BonsaiNode.tsx:95` become `--warn-solid`; the rest become `--warn`.
- **`--red`:** all become `--danger`.
- **`--blue`:** `BonsaiNode.tsx:118` and `ProcessNode.tsx:12` become `--accent`; `BonsaiNode.tsx:147` becomes `--ok`.
- **`BoardPage.tsx:47-57`** (`colorClass`): replaced by `tagPalette` (P2-5).
