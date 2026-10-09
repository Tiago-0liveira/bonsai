/** Body of the real PR #47, as served by GitHub. */
export const pr47Body = "## Summary\n- **Phase 5 of `docs/theme-plan.md`:** popovers and command palette, dialogs, Pull Requests, Settings, Logs, Files, notices and the setup overlay move to theme tokens and the shell recipes (modal, input, popover, `.btn-*`, `island`, `shadow-overlay`). Segmented choices (branch source, provider) are 30px tabs. Accessible names and labels are unchanged.\n- **Remove the Table (board) section:** page, route, nav link, palette entry, store slice and persisted keys.\n- **Remove worktree tags everywhere:** tag model, inspector editor, new-worktree tag picker, canvas tag chips, tag-based stacking, and the backend `tag` metadata field (web and Go).\n\n## Behavior changes to review\n- Only the server's \"Local / unlinked\" connection group stacks on the canvas now; other worktrees are separate nodes. Toggling that group also re-attaches detached worktrees.\n- `PATCH /api/worktrees/{id}/metadata` rejects `{\"tag\": ...}` with 400 (strict decoder). Stored metadata that still has `tag` loads fine.\n- Persisted `boardItems`, `boardLists`, `boardPriorities`, `boardTypes` and `collapsedTagGroups` are dropped on load. Saved positions for tag stacks no longer apply.\n\n## Stacking\nBased on `feat/theme-canvas` (#46), not `main`.\n\n## Test plan\n- [x] `pnpm typecheck && pnpm lint && pnpm test` (293 tests)\n- [x] `go test ./...`\n- [x] e2e: smoke, auto-layout, execution-controls, inspector, storage-migration, screens, worktree-management (run without the Go fixture server, because port 7001 was in use)\n- [ ] Fixture-server e2e specs (`agent-terminal-fullstack`, `process-terminal-fullstack`) not run locally; need `pnpm test:e2e` with port 7001 free\n\n\ud83e\udd16 Generated with [Claude Code](https://claude.com/claude-code)"

/** The same body with every line break collapsed, as some API paths deliver it. */
export const pr47BodyFlattened = pr47Body.replace(/\n+/g, ' ')

export const pr47BodyCrlf = pr47Body.replace(/\n/g, '\r\n')

export const bodyWithoutHeadings = 'Quick fix for the flaky scrollback test.\n\n- one\n- two'

export const bodyWithDuplicateHeading = [
  '## Summary',
  'First summary.',
  '',
  '## Summary',
  'Second summary.',
  '',
  '## Testing',
  '- [x] first',
  '',
  '## Test plan',
  '- [ ] second',
].join('\n')
