@AGENTS.md

# Claude Code additions

## Token policy

- Main thread decides and edits. Delegate broad searches, test/lint runs and diff review to cheap subagents in `.claude/agents/` and use only their compressed result.
- Do not run `go test ./...` or `pnpm test` inline when output may be long: delegate to `test-runner` (returns failures only).
- Do not read generated or huge files unless the task needs them: `web/pnpm-lock.yaml`, `go.sum`, `web/bundle-budget.json` (only when changing budgets), `*.golden`, `web/dist*`.
- Search with Grep/Glob first, then Read with `offset`/`limit`. Prefer `code-locator` over reading several files to find one symbol.
- Do not re-run Playwright e2e unless browser behavior or `web/e2e` changed. Run one spec: `pnpm --dir web exec playwright test e2e/<name>.spec.ts`.
- Nested guides (`web/AGENTS.md`, `internal/AGENTS.md`, `internal/providers/AGENTS.md`) load when you work there. Do not read them manually from elsewhere.
- Do not create plan/summary markdown files. Report in the reply.

## Subagents (`.claude/agents/`)

| Task | Agent | Model |
| --- | --- | --- |
| Find where X is defined/used, map a dir | `code-locator` | haiku |
| Run targeted Go/pnpm checks, report failures | `test-runner` | haiku |
| Implement/fix Go in core, daemon, server, cli, git | `go-backend` | sonnet |
| Claude/Codex/Antigravity providers, accounts, usage, sessions, agentterminal | `agent-providers` | sonnet |
| Bubble Tea TUI in `internal/ui` | `tui` | sonnet |
| React/TS in `web/`, vitest, bundle budget, playwright | `web-frontend` | sonnet |
| Review current diff against AGENTS.md invariants | `reviewer` | sonnet |
| Update README/docs for a behavior change | `docs-writer` | haiku |
| `.github/workflows`, goreleaser, release scripts, installers | `release-ci` | haiku |

## Delegation rules

- Give the agent a self-contained prompt: goal, files or dirs, constraints, what to return. It does not see this conversation.
- Cross-cutting change (Go API + web client): do the Go side with `go-backend`, then `web-frontend` with the new contract in the prompt. Do not run implementers in parallel on the same files.
- Always finish with `test-runner` on the touched areas, then `reviewer` on the diff before reporting done.
- Do trivial one-file edits inline; do not spawn an agent for them.
- Never commit or push unless the user asks.
