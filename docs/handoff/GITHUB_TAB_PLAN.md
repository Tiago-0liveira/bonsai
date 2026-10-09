# GitHub tab redesign: implementation plan

Reference mockups: `github-stacked.png` / `.html` (board M, a PR in a stack) and `github-standalone.png` / `.html` (board N, a PR straight onto main). Open the HTML files in a browser to inspect exact measurements and colors.

**Scope.** Everything in the mockups **except** the "Review notes" and "Needs attention" cards. Do not build those, and do not build anything that exists only to feed them (job-log fetching, re-run endpoints).

**Assumptions to verify in the real tree** (my notes come from an older snapshot, commit `9993815`; the theme work has since landed in PRs #42 to #47):
- The theme layer exists: semantic tokens (`bg`, `panel`, `panel-2`, `border`, `text`, `muted`, `accent`, `ok`, `warn`, `danger`, ...) and the shell recipes (island, input, popover, `.btn-*`). Use only those. No raw hex values and no old `--purple` / `--green` names.
- `web/src/types/index.ts` already has `PullRequest` with `description`, `checks`, `commits`, `conversation`, `files`, `mergeable`, `status`, `branch`, `base`. `features/github/PullRequestsPage.tsx` already has Mark ready / Merge / Close wired to the store. Reuse both.
- The Go backend serves PRs through `internal/server/localapi/github.go`, and its JSON decoding is strict, so any new field must be added on both sides.

## Mockup versus first release (decisions already made)

| Mockup element | First release |
|---|---|
| Summary + Test plan cards | **Yes**, rendered from the PR body (see Phase 1) |
| Review notes, Needs attention | **Not built** |
| "Merge when checks pass" | Replace with the existing **Merge** button, disabled with the reason shown in the merge card |
| "Ready for review" | The existing **Mark ready** action |
| "Behind main by N" + Update | Omit until the backend provides `behindBy` (Phase 4, optional) |
| Check durations ("2m 08s") | Show only if the data has them (Phase 4); otherwise hide the column |
| Reviews tile (requested / approvals) | Show approvals counted from `conversation` entries of kind `review`; add requested reviewers in Phase 4 |
| Stack grouping, stack card, worktree card | **Yes**, derived on the client from data already in the store |
| All numbers, names and durations in the mockups | **Placeholders**; never copy them |

## Library for the markdown

Use **`react-markdown` + `remark-gfm`** (task lists, tables, strikethrough, autolinks) for rendering, and **`unified` + `remark-parse` + `remark-gfm`** to parse the PR body into sections.
- **Security:** PR bodies are untrusted. Do **not** add `rehype-raw` (so raw HTML is never rendered). External links get `target="_blank" rel="noopener noreferrer"`. Only `https:` images and links.
- No syntax highlighting in v1. Inline and fenced code use the `well` token.
- Load the GitHub page lazily (`React.lazy` / the router's lazy route) so the markdown libraries stay out of the main bundle.

## Phases (5; each shippable on its own)

### Phase 1: logic and markdown foundation. **Sonnet 5.5**
No visible UI change. All of it is pure, tested code.
1. Add the dependencies above (pnpm).
2. `lib/github/prBody.ts`: parse a body into `{ summary?, testPlan?, rest[] }`. Split on `##` headings using the mdast tree and slice the original text by node positions (no regex on markdown). Match headings case-insensitively (`Summary`, `Test plan` / `Testing`). Task lists give `{done, total, items[]}` from `listItem.checked`. Unknown sections go to `rest`. If there are no recognizable sections, `rest` holds the whole body.
3. `lib/github/prStack.ts`: PR B is stacked on A when `B.base === A.branch` and A is open. Return stack groups ordered by chain (root first), the standalone list, and for any PR its chain and `isStacked`. Cycles and missing parents must not crash.
4. `lib/github/prChecks.ts`: `{passed, running, failed, total}` and a sorted list (running first, then failed, then passed).
5. `components/Markdown.tsx`: themed react-markdown wrapper (paragraphs, bold lead, lists, task-list checkboxes, inline code, fenced code, links, tables, blockquote).
6. Vitest tests with fixtures: the real #47 body (flattened newlines and all), an empty body, a body with no headings, a body with a duplicate heading, a 3-deep stack, a cycle, an orphan base.

*Done when:* `pnpm typecheck && pnpm lint && pnpm test` pass; coverage of the three `lib` files is complete; no UI file changed.
*Haiku 5.5 may* write the extra fixtures and test cases once Sonnet has defined the functions. It should not write the parsing or stacking logic.

### Phase 2: pull request list pane. **Sonnet 5.5**
Match the left island in the mockups.
- Title bar with the open count; segmented **Open / Closed** control; search (`/` focuses it); filter chips **All / Stacked / Draft / Failing** with live counts.
- Groups: "Stack · <name>" with a rail connecting its PRs, then "Standalone". Each row has the PR number, title (one line, ellipsis), `head → base` in mono, a segmented checks bar with `passed/total`, and the time. Selected row: accent tint and a left bar.
- Selection is kept in the router search param (so a link restores it) and falls back to the first PR. Arrow keys move the selection. Loading skeleton, empty state ("No open pull requests"), and error state with retry.
- Uses `prStack.ts` and `prChecks.ts` from Phase 1.

*Done when:* the screenshot of the stacked fixture matches `github-stacked.png` (list pane only) within normal tolerance; the standalone group appears with drafts marked; keyboard navigation and search filtering have component tests.

### Phase 3: detail view. **Sonnet 5.5**
Match the right island in both mockups.
- **Header:** state chip (Open / Draft / Merged / Closed), title with number, `head → base` chip, "stacked on #N" chip only when stacked, author and updated time, Open on GitHub, copy link, previous/next.
- **Tiles:** Checks (segmented bar), Reviews, Changes (+/- bar, file count), Commits.
- **Tabs:** Overview, Commits, Files, Conversation (with counts). Overview is new; move the existing commits, files and conversation views into the other tabs unchanged except for token styling.
- **Overview, main column:** Summary card (Markdown), Test plan card (checkboxes, `x of y`, read-only), Commits card (latest 3 with a link to the tab). A section that does not exist in the body simply does not render; if the body has no recognizable sections, show one "Description" card with the full Markdown, clamped with "Show more".
- **Overview, right column:** Merge card (state, requirement rows from `mergeable` and the checks summary, then Mark ready / Merge / Close using the existing store actions; the disabled Merge button states why), Checks card (sorted as in Phase 1), Stack card (only for stacked PRs; each entry links to that PR), Worktree card (when a worktree exists for the PR's branch: its processes and agents, and a "Show on canvas" action that navigates to the canvas and selects that node).
- Both columns scroll independently inside the island; nothing is clipped at small heights.

*Done when:* both screenshots (stacked and standalone fixtures) match the mockups in structure and token usage; no hex values or removed token names in the diff; existing PR actions still work (existing tests pass); a PR with an empty body, a very long title, 0 checks and 40 checks all render without overflow.

### Phase 4: data enrichments (optional, after Phases 1 to 3 ship). **Sonnet 5.5**, reviewed by **Opus 5.5**
Each item is independent and must degrade to "hidden" when the field is absent.
- `checks[].startedAt` / `completedAt` for durations.
- `requestedReviewers[]` and an approvals/changes-requested summary for the Reviews tile.
- `behindBy` (compare base...head) for the "Behind main by N" row, and an Update branch action if you want it.
- Totals `additions` / `deletions` / `changedFiles` so the Changes tile does not depend on loading every file.

Backend: extend the GitHub service and `localapi/github.go` responses, update Go tests, then the TS types and the fixtures. Keep the strict-decoding rules in mind.
*Done when:* `go test ./...` and the web tests pass, and the UI shows the new data when present and nothing when absent.

### Phase 5: polish and verification. **Haiku 5.5** for the mechanical parts, **Opus 5.5** for the final review
- Haiku: accessible names for every icon button, roles for the segmented control and tabs, `aria-selected`, focus rings from the theme tokens; remove dead code left from the old page; update existing e2e selectors.
- Add Playwright specs for the stacked and standalone fixtures (list selection, tab switching, filter chips, Mark ready / Close).
- Opus: final review of the whole diff against the mockups and this plan (prompt below).

## Review gates
1. After Phase 1 (Opus 5.5, short): is the data contract right? Are the pure functions robust to bad input?
2. After Phase 3 (Opus 5.5): does the UI match the mockups, are the tokens used correctly, is anything out of scope?

## Prompts

**Implementer (Sonnet or Haiku), one phase or task at a time:**
> Read `docs/github-tab-plan.md` and the mockups in `docs/handoff/` (`github-stacked.png`, `github-standalone.png`, and the HTML files for exact values). Implement only Phase `<N>` (task `<X>`). Use the existing theme tokens and recipes; no hex values. Do not build Review notes or Needs attention. Don't touch files outside the phase. Run `pnpm typecheck && pnpm lint && pnpm test` (plus `go test ./...` if you changed Go) and report the results. For UI work, compare your screenshot against the mockup and list any differences.

**Reviewer (Opus 5.5), at each gate:**
> Review the diff since `<commit>` against `docs/github-tab-plan.md` and the mockups in `docs/handoff/`. List deviations from the plan or mockups, hardcoded colors or removed token names, anything out of scope (Review notes, Needs attention), unsafe markdown handling (raw HTML, unsanitized links), and missing tests. Don't fix anything.
