# web/ guide

React 18 + Vite 5 + TypeScript + Tailwind 3. Served by the Go API at `/app`, embedded via `web/embed.go` (tag `embedui`, embeds `web/dist`; `embed_stub.go` is the untagged placeholder). Run pnpm from the repo root with `pnpm --dir web <script>`.

## Scripts (`web/package.json`)

`typecheck` (`tsc -b`), `lint` (eslint, zero warnings), `test` (vitest run, jsdom), `test:watch`, `test:coverage` (100% enforced only for `src/lib/github/**`), `build` (`tsc -b && vite build` into `dist`), `bundle:check`, `test:bundle` (tests the budget script), `test:e2e` (playwright), `dev` (vite).

One test file: `pnpm --dir web exec vitest run src/features/agents/UsageMeter.test.tsx`. One e2e spec: `pnpm --dir web exec playwright test e2e/<name>.spec.ts`.

## Layout (`web/src`)

- `app/`: `router.tsx` (TanStack Router), `shellRoute.tsx`, `ApplicationRoot.tsx` (lazy), `chunkRecovery.tsx` (eager).
- `api/`: typed clients for the local API (`localClient.ts` session/WebSocket, `agents.ts`, `git.ts`, `processes.ts`, ...). Mirrors Go `internal/server/localapi` JSON; change both together.
- `stores/`: zustand (`bonsai.ts` main store, `projectSelectors.ts`, `reconciliation.ts` epoch/sequence ordering, `*Preferences.ts` persisted prefs).
- `features/<name>/`: `agents`, `workspace` (canvas, dialogs), `github`, `files`, `inspector`, `terminal`, `logs`, `settings`, `branches`, `command-palette`.
- `components/ui`, `components/layout`: shared primitives (Radix based). `theme/`: tokens, themes, terminal colors. `types/index.ts`: shared types. `lib/`: pure helpers.
- `mock/`: fixture data for dev and e2e (`agents.ts`, `agentProviders.ts`, `processes.ts`, `activity.ts`). `test/fixtures/`, `test/setup.ts`: vitest fixtures.

## Rules

- Keep the lazy boundaries: workspace, routes, terminals, overlays, PR detail, files, logs import lazily. Do not import them from the entry or shell. Closed overlays and unused terminals must not import.
- Chunk-failure recovery stays eager and offers an explicit reload, never an automatic loop.
- Browser storage: approved preferences only (`stores/preferenceBoundary.ts` guards this). Never store tokens, snapshots, terminal output, env values. Capability token lives in memory.
- Production must use only the page origin. `VITE_BONSAI_LOCAL_API_ORIGIN`/`VITE_BONSAI_RELAY_ORIGIN` and the SSE client are dev/test only. No external URLs, no cloud fallbacks.
- `vite build --mode e2e` writes `dist-e2e` with test hooks (`__bonsaiTestStore`, react-dom profiling). These must never reach `dist`; `go test -tags embedui ./web/` checks that.
- New dependency or new eager import: run `build` then `bundle:check`. If over budget, justify in `web/bundle-budget.json` (`rationale`) and `docs/performance.md`; do not silently raise limits.
- Tests sit beside source. Prefer vitest + Testing Library for logic and components; use Playwright (`web/e2e`) only for browser-level behavior (layout, canvas, routing, terminals). e2e starts Go fixtures (`go test ./internal/server/localapi -run Test...BrowserFixture`) on ports 7011-7014; keep them free. Shared e2e helpers: `e2e/mockGit.ts`, `e2e/presentationFixtures.ts`, `e2e/viewport.ts`; `*-fullstack.spec.ts` specs use real Go fixtures.
- Fixtures with dates must stay in the future relative to "now" (reset times), or tests rot.

## Dev loop

Preferred for backend-dependent work: `bonsai __serve-dev-stack` (API 7001, webhook 7002, Vite 7003; `status|logs|restart|stop`). Visual-only: `pnpm --dir web dev`. Details: `docs/development.md`, `web/README.md`.
