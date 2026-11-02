@AGENTS.md

# Claude Code additions

## Token policy

- Worker routing (Codex builder/reviewer, Antigravity scout, Sonnet fallback, `gocheck.sh`) is the user-level policy in `~/.claude/CLAUDE.md`. It overrides the Claude subagent defaults below.
- Main thread decides, writes worker specs and verifies. Bulk reading goes to the scout, implementation to the Codex builder, reviews to the Codex reviewer.
- Go checks run through `~/.claude/skills/orchestrate/scripts/gocheck.sh [--race] [--full] <pkgs>` in a background shell (failures only), not an agent. `pnpm` checks: run in a background shell and read the tail.
- Do not read generated or huge files unless the task needs them: `web/pnpm-lock.yaml`, `go.sum`, `web/bundle-budget.json` (only when changing budgets), `*.golden`, `web/dist*`.
- Search with Grep/Glob first, then Read with `offset`/`limit`.
- Do not re-run Playwright e2e unless browser behavior or `web/e2e` changed. Run one spec: `pnpm --dir web exec playwright test e2e/<name>.spec.ts`.
- Nested guides (`web/AGENTS.md`, `internal/AGENTS.md`, `internal/providers/AGENTS.md`) load when you work there. Do not read them manually from elsewhere.
- Do not create plan/summary markdown files. Report in the reply.

## Sonnet fallback subagents (`.claude/agents/`)

Use these when `dispatch.sh builder|reviewer` exits 4 (Codex below 20%). They run Sonnet 5.5 at medium effort.

| Task | Agent |
| --- | --- |
| Implement/fix Go in core, daemon, server, cli, git | `go-backend` |
| Claude/Codex/Antigravity providers, accounts, usage, sessions, agentterminal | `agent-providers` |
| Bubble Tea TUI in `internal/ui` | `tui` |
| React/TS in `web/`, vitest, bundle budget, playwright | `web-frontend` |
| Review current diff against AGENTS.md invariants | `reviewer` |
| `.github/workflows`, goreleaser, release scripts, installers | `release-ci` |

`code-locator`, `test-runner` and `docs-writer` (Haiku) are last resorts: prefer the scout for finding and summarizing, `gocheck.sh` for checks, and the builder for docs.

## Delegation rules

- Give every worker a self-contained spec: goal, files or dirs, constraints, done-when commands, what to return. It does not see this conversation.
- Cross-cutting change (Go API + web client): write the contract into the spec, then Go side, then web side. Run implementers in parallel only on disjoint files.
- Always finish with `gocheck.sh` on the touched areas, then the Codex reviewer on the diff, before reporting done.
- Do trivial one-file edits inline; do not dispatch for them.
- Never commit or push unless the user asks.
