# Phase 7 — Warm start and frontend load (revised plan)

Status: **in progress** (7a merged in #69; 7b in #68; 7c prepared for CI) · Branch base: `feat/bonsai-web` (NOT `main`) · Supersedes the
Phase 7 section of `docs/bonsai-web-plan.md`

Plan only. Implementation branches start from and merge into `feat/bonsai-web`.
When Phase 7 is done, fold the outcome into `docs/bonsai-web-plan.md`'s status
table and delete this file.

Provenance: the findings below come from a read-only planning pass at
`04e9683` (`feat/bonsai-web`). File and line references were **not**
re-verified when this revision was written. Every implementer must re-check
the ones they depend on and note any that moved.

## What changed from the first draft

| # | Change | Why |
| --- | --- | --- |
| 1 | **Persisted ETag cache is deferred** to a follow-up (see [Deferred](#deferred-follow-up)). | After a restart it only turns a few `200`s into `304`s. It does nothing for the paint target, and it is the riskiest part (body stripping, token fingerprints on disk, invalidation rules). The in-memory ETag cache stays as is. |
| 2 | **The ≤ 300 ms check gates on p95, locally.** CI reports the numbers but does not block on them. | Hosted CI runners are noisy and there is no dedicated runner class. "Every one of 30 runs" would be a flaky gate. |
| 3 | **Time-to-branch-list-paint is the headline metric.** The byte budget is a regression guard. | `/` *is* the workspace page, so the canvas chunk is still fetched on the first screen. "Main chunk −30%" can pass while first-screen JS barely moves. |
| 4 | **No hosted-site work** (Caddy asset 404s, `web/Dockerfile`, hosted preview server). | `app.bonsai.dev` is being retired. Keep only chunk-failure recovery for the local-served UI. |
| 5 | ELK worker removed from scope; `elkjs` dependency removed. | `elkjs` is unused. Canvas uses a custom synchronous layout. |
| 6 | Existing splits are not claimed as new work (xterm, GitHub route, app vs landing). | They already exist. |
| 7 | Playwright list trimmed, and the Phase 7 gates reduced (see [Test plan](#5-test-plan)). | Keep the suite proportional to the risk. |

## 7a corrections (re-verified at `04e9683` while implementing 7a)

The 7a PR changed these points of this plan; the code is the reference.

| # | Plan said | Code / decision |
| --- | --- | --- |
| 1 | Measure with pnpm 12.9.1 | `pnpm` outside `web/` may be older (12.6.0 here); inside `web/` the `packageManager` field selects 12.9.1. All builds above used 12.9.1. |
| 2 | Main chunk = facade `src/app/ApplicationRoot.tsx` | The manifest has no such facade: ApplicationRoot is the shared chunk `_ApplicationRoot-*.js`. Resolve it by chunk name. |
| 3 | Branch-list badges have no freshness input | Also: the browser never read nested CI freshness at all (`wireFreshness` kept only state, time and error). 7a maps it and adds `ciStale` / `prStale` to worktrees. |
| 4 | Seed the provider cache (optional detail) | Required: `refreshProvider` rebuilds every worktree's CI as `loading` unless the checks cache hits, which would clear restored badges as soon as the catalog read finished. Restored checks are seeded per head repository and SHA. |
| 5 | Reset `fullReconcileAt` after restart | A fresh cache entry already has it zero and the in-memory ETag cache is empty, so page one cannot short-circuit. Kept that way when seeding, and tested. |
| 6 | Revalidate independent of browser subscription | `stateSync.Run` refreshed only on `SubscriberReady`; 7a queues restored projects at start. |
| 7 | Namespace = roots path + security mode | Also the canonical `RepoDir`, so the legacy per-repo `bonsai serve` never shares a document with `bonsai web`. |
| 8 | `source: live` after revalidation | `source` is `"disk"` while restored and omitted otherwise (absent means live). |
| 9 | `--repeat-each=30` | One test loops 3 warm-ups + N runs in fresh contexts and aggregates p50/p95/max (`BONSAI_P7_RUNS`, `BONSAI_P7_SIZE=large`, `BONSAI_P7_GATE=1` for the local ≤ 300 ms gate). Separate repeats cannot aggregate. |
| 10 | Background projects "in batches" | Active project first, the rest in catalog order, one `project_snapshot` each. |
| 11 | "Live updates" must not imply fresh data | `UpdatesBadge` reads update settings, never freshness; no change needed. |
| 12 | (not in plan) | After an API restart the browser retried the event socket forever with the dead capability (policy close before `ready` never invalidated it). Fixed in 7a: the epoch-change path depends on it. |
| 13 | (not in plan) | `Config.SnapshotCacheDir` enables warm start; `Run` defaults it under the user state dir; `BONSAI_WARM_START=off` disables it. Tests that call `New` never write to the user's state dir. |
| 14 | Persisted local subset | Changed-file lists and status error messages are not persisted; restored rows show change counts until local Git revalidates. |

## Objectives

1. Opening the app paints last-known worktree list and PR/CI badges immediately,
   clearly labeled stale, then revalidates.
2. Initial JS cost is proportional to the first screen, and the branch list
   paints before the canvas loads.

## Non-goals

- Browser-side snapshot cache (IndexedDB). Reconsider only if measurement shows
  a material delay before WS `ready`.
- Persisted ETag cache (deferred).
- Any change to the loopback / Host / Origin / capability boundary.
- Hosted deployment changes.

## Global rails (inherited from `bonsai-web-plan.md`)

- Security boundary unchanged. Secrets are never logged, persisted, placed in
  argv, or sent to the browser.
- `go build ./...` works without Node or pnpm (embed stub stays).
- Local-served UI keeps working. The hosted entry is not extended.
- Linux, macOS and Windows stay green.
- If the code contradicts this plan, stop and update the plan in the same PR.

---

## 7b implementation notes (2026-10-10)

Tasks 5–6 are implemented on `feat/web-p7b-lazy-boundaries`, based on
`04e9683`, targeting `feat/bonsai-web`. Tasks 0–4 landed separately in 7a
(#69); tasks 7 and 8 remain outside 7b.
See the [7b outcome and exact byte table](bonsai-web-plan.md#phase-7--warm-start-and-frontend-load).

Re-verified corrections: `AppShell` lives under `components/layout` and
`BottomWorkspace` under `features/terminal`. The command shortcut listener
must move to the eager owner. Terminal effect imports catch their errors,
so they need explicit recovery in addition to React error boundaries.
The main app/GitHub route may lack facade keys in Vite's manifest; resolve
stable chunk names and static import graphs in that case. The required setup
settings and header helpers remain shared eager code.

During the original 7b validation, the environment supplied pnpm 12.6.0,
not the declared 12.9.1; registry access was unavailable.
The cached dependency lock matched the pre-7b lock,
and direct cached build binaries were used for the same-toolchain comparison.
Full Go and browser suites were blocked by socket/browser sandbox restrictions;
package-manager path failures also reproduced on the unchanged baseline.
Unit, typecheck, lint, production/e2e build, plain Go build/vet and embedded
serving checks passed. At that point, Phase 7 awaited 7a/7c and unrestricted
validation. The first CI run passed the complete Go matrix and Linux race
checks and 66/67 browser tests (including all 11 new cases). The remaining
settings test still expected “Standard” for a failed Live tunnel, despite
`04e9683` changing the UI to “Live paused”; its expectation is corrected here.
The corrected frontend CI run `38082871917` passed all 67 browser tests;
Go CI run `38082871918` passed the Linux/macOS/Windows matrix, Linux race
checks and release dry-run. CI also verified the declared pnpm 12.9.1 frozen
install. 7b implementation is complete in #68; 7a has since merged in #69.
Phase 7 still awaits 7c. The original 7b changes did not touch persistence or
hosted deployment files.

---

## 7c verification notes (2026-10-11)

Task 8 adds manifest accounting, raw-byte enforcement, historical references,
a separate candidate final baseline, focused script tests, CI artifact upload,
and corrected measurement definitions. Exact artifact evidence is in
`docs/evidence/p7c`; the main plan records the final results.

The user requested opening the original 7c PR to run CI while #68 was still
open. This draft therefore stacks on #68's integrated head `17f69bb` and targets
`feat/bonsai-web`. Before completion, merge #68 and rebase onto the updated
integration branch. The existing sandbox restriction no longer applies:
loopback and pinned Chromium work in the current unrestricted environment.
Phase 7 remains in progress pending the integration and final CI evidence;
retain this file until those conditions pass, then fold surviving decisions
into the main plan and delete it.

The cache measurement now distinguishes the maximum retained heap after GC
across 30 repetitions from sampled live heap during serialization/restore and
from cumulative allocation bytes. The historical 7a retained figure was the
last positive sample, not a peak; its best-of-five timing is kept labeled.
First-screen JS includes the default workspace closure as well as entry and
application. A closure includes its root and counts each emitted JS file once.
No persistence, lazy-boundary, worker, ETag-cache or hosted behavior changes
are made in 7c.

---

## 1. Findings from the merged code

| Area | What exists today | Consequence |
| --- | --- | --- |
| Server snapshots | `newStateSync` creates empty maps and a random epoch (`internal/server/localapi/state_sync.go`). Projections start at sequence 0 with loading freshness. Semantic commits increment sequence and publish the full snapshot. | Persist committed *metadata* around commits. Do not persist ordering or job state. |
| Provider cache | Repository and checks entries are memory maps with no eviction policy (`enrichment.go`). TTL: repo and catalog 20 s, checks 60 s (10 s when pending). Failed reads keep stale values. | Restored entries start expired. Bound retained entries. Keep stale values during revalidation. |
| PR catalog | Reads **open** PRs, five pages per job. A complete previous catalog stays visible during replacement. A page-one `304` can skip later pages until a five-minute full reconcile (`pr_catalog.go`). | After a restart, reset the continuation and force a full catalog reconcile. A page-one `304` cannot validate unvisited pages. |
| ETag cache | `internal/git/github/ghcli/etag.go`: raw bodies, validators, 32 MiB LRU, 4 MiB entry cap. Keys include host, URL and Accept. A token-fingerprint mismatch drops the entry. Every hit still makes a network request. | Unchanged in this phase (deferred). |
| Browser cache | Zustand store starts empty. Persistence covers preferences only (`web/src/stores/workspacePersistence.ts`, `preferenceBoundary.ts`). No IndexedDB use. | No browser snapshot cache. |
| Existing splits | `main.tsx` lazy-loads app vs landing. `PullRequestsRoute` is lazy. xterm and FitAddon load dynamically inside the terminal components. Workspace, files, logs and settings are eager. | Finish the boundaries. Remove eager CSS imports. |
| Canvas | `elkjs` is a dependency but unused. Layout is custom synchronous `computeGlobalPlacements` (`web/src/features/workspace/canvas/layout/globalLayout.ts`), called at init and on Auto-layout. | Split the canvas. Remove `elkjs`. |
| Bundle budget | `build` is `tsc -b && vite build`. No budget script. Frontend CI runs build and Playwright, no size check. | Add a deterministic artifact check. |
| WS bootstrap | The socket authenticates, subscribes, sends `ready`, then catalog and per-project snapshots, then `bootstrap_complete` (`events.go`). The browser buffers snapshots until bootstrap completes (`web/src/api/git.ts`). | The active project should be emitted first so it does not wait on others. |
| Ordering guards | The browser resets sequence guards on epoch change and rejects foreign epochs, stale connection generations and non-increasing sequences (`web/src/api/git.ts`). | Keep these. |
| Snapshot content | Process summaries include commands and errors. PR summaries include bodies (`snapshot.go`, `internal/git/github/service.go`). | The persisted DTO is an explicit subset. Never serialize the live snapshot verbatim. |
| Stale labels | Inspector has one. Branch-list PR/CI badges and canvas badges have no freshness input. Open-PR catalog presentation maps stale-without-error to ready (`usePullRequestCatalog.ts`). | These surfaces need explicit stale presentation. |
| State dir | `config.UserStateDir()` and `config.WebHome()` already resolve per-OS locations (`internal/core/config/web.go`). | Reuse them. |
| Embedded serving | `/assets/*` gets immutable caching and gzip. Missing assets return 404. `/app/*` falls back to no-store HTML (`internal/server/webui/webui.go`). CSP allows same-origin scripts, no inline. | Extra chunks are same-origin `/assets/*`. No inline loaders. |

### Docs that are out of date

`docs/git-backend.md` and `docs/development.md` still describe hosted-only
serving, subprocess GitHub reads, 60/15 s TTLs and all-state/daily catalog
reconciliation. Trust the merged code. Fixing those docs belongs to Phase 8.

---

## 2. Measure first

Take two size baselines using the same Node 22 and **pnpm 12.9.1** (the version
declared in `web/package.json` and CI; verify with `pnpm --version`):

- the Phase 4 merge (find it with
  `git log --all --oneline --grep='Embedded UI'`; the first draft recorded
  `ee66e17`, verify it), and
- the pre-Phase-7 integration SHA (`04e9683` at planning time).

```sh
node --version
pnpm --version
git rev-parse HEAD

p7_dir="$(mktemp -d /tmp/bonsai-p7-phase4.XXXXXX)"
git archive <phase4-sha> web | tar -x -C "$p7_dir"
pnpm -C "$p7_dir/web" install --frozen-lockfile
pnpm -C "$p7_dir/web" build --manifest | tee /tmp/bonsai-p7-phase4-build.txt

pnpm -C web install --frozen-lockfile
pnpm -C web build --manifest | tee /tmp/bonsai-p7-before-build.txt
```

Definitions:

- **Main application chunk**: the chunk whose facade is
  `src/app/ApplicationRoot.tsx` (Phase 4's `ApplicationRoot-*.js`).
- **Entry**: tracked separately.
- **Static closure** and **first-screen JS**: measured so code moved into
  eagerly loaded shared chunks cannot hide growth.

Record results in a new Phase 7 table in `docs/bonsai-web-plan.md`. Keep the
historical Phase 4 table unchanged.

| Metric | Phase 4 recorded | Rebuilt Phase 4 | Pre-P7 | Final P7 |
| --- | ---: | --- | --- | --- |
| ApplicationRoot raw / gzip | 729.73 / 230.35 kB | fill exact bytes | fill | fill |
| Entry raw / gzip | 149.52 / 48.80 kB | fill | fill | fill |
| xterm raw / gzip | 290.37 / 72.55 kB | fill | fill | fill |
| PullRequestsRoute raw / gzip | 210.83 / 63.04 kB | fill | fill | fill |
| CSS raw / gzip | 54.61 / 11.19 kB | fill | fill | fill |
| Static closure / first-screen JS | not recorded | fill | fill | fill |
| WS `ready` → branch list + PR badges painted (p50 / p95 / max) | not recorded | — | fill | fill |
| Snapshot cache bytes / load time / peak retained memory | not recorded | — | fill | fill |

The approximate main-chunk target is about 510.8 kB (70% of 729.73). Gate
against **70% of the rebuilt exact bytes**, not the rounded table value.

**Measurement-only commit, before any behavior change:**

1. Extend the e2e-build instrumentation in `web/vite.config.ts` to mark
   receipt of authenticated WS `ready` (in `localClient.ts`) before resolving.
2. Observe the active project's expected branch rows and PR badges in the DOM.
   Record the paint after two animation frames following complete DOM
   presentation. Do not measure only a Zustand update.
3. Add `web/e2e/warm-start.spec.ts` (**new**) with the fixture in section 5.

Then:

```sh
pnpm -C web test:e2e warm-start.spec.ts --workers=1 --repeat-each=30
pnpm -C web test:e2e render-isolation.spec.ts --workers=1
```

Attach the JSON samples and traces. Record the baseline **before** persistence
or splitting changes.

---

## 3. Design decisions

### 3.1 Server snapshot storage

Location: `<user state dir>/bonsai/web/warm-start/<namespace>/snapshots.json`
(proposed path; no such file exists yet).

- **Namespace** = hash of the canonical project-roots config path plus browser
  security mode. Product, development and test fixtures stay separate. Tests
  inject an explicit temp directory.
- **Format**: one compact, versioned JSON document with its own schema
  version, namespace and save timestamp. Records carry the project ID,
  canonical path and per-component validation timestamps.
- **Limits**:

| Resource | Limit |
| --- | ---: |
| Snapshot document | 16 MiB |
| Single project snapshot | 2 MiB |
| Maximum persisted projects | 256 |
| Maximum age of a restored record | 7 days |

- Enforce serialized bytes, bounded reads and collection lengths. Evict whole
  least-recently-used projects and keep the active one preferentially. Skip an
  oversized project rather than truncating a list. Count skips in
  measurements.
- **Writer**: one coalescing writer. Flush after 1 s of inactivity, at least
  every 5 s during continuous commits, and on graceful shutdown with a bounded
  deadline. Export immutable DTO copies outside the projection and provider
  locks. Never do filesystem I/O under those locks.
- **Atomicity and locking**: reuse `gitstore.WriteJSON` (same-directory temp
  file, sync, rename, directory sync) and `procstore.Lock`, one writer per
  namespace. Test replacement on Windows. A failed write keeps the previous
  document and the app stays usable. Directories are private and files are
  owner-only where the platform supports it.
- **Rejected**: SQLite, gob, synchronous write on every commit, and storing
  this inside `web-state.json`.

**Persisted metadata DTO (an explicit subset):**

- Local repository, worktree and branch identities, sanitized remote
  identities, status metadata and counts, groups and worktree preferences.
- Provider repository metadata, open-PR summaries, worktree associations,
  checked SHAs and check summary metadata.
- Last successful per-component validation timestamps and catalog
  completeness.

**Never persisted:** process commands, PIDs and runtime authority, agents,
capabilities, credentials, tokens, environment variables, terminal output,
logs, file contents, patches, PR descriptions, comments, reviews, arbitrary
error messages.

Derived data (such as branch candidates) is rebuilt from restored metadata.
Runtime components are restored through their current authorities.

**Failure behavior:** a missing, corrupt, oversized or unsupported-version
cache is a cold start. Ignore the bad document and replace it only after valid
live data exists. Do not migrate unknown schemas. Diagnostics carry a reason
code and a corrective path, never cached contents.

### 3.2 Epoch, sequence and freshness

- A **new epoch on every API process start.** Never restore the old epoch or
  sequence as current ordering.
- Install each accepted disk projection at **sequence 0 of the new epoch**.
  Live commits continue from there.
- Load after registry discovery and before serving bootstrap snapshots. Admit
  only projects still in the current catalog with matching canonical identity.
  Never resurrect removed projects.
- Every restored component that carries data (including nested CI freshness)
  starts as `state: stale`, keeps its original `updated_at`, and gets an
  additive provenance field, recommended `source: disk`. A successful
  revalidation sets `source: live` and normal freshness.
- Keep provenance **per component**. Local Git can be ready while
  provider/CI data is still restored.
- This is additive within protocol 3. Update the wire types and mapping in
  `snapshot.go`, `web/src/api/git.ts`, `snapshotReconciliation.ts` and
  `web/src/types.ts`.
- A stale label stays visible while replacement PR pages or checks load.
  `enrichment.go` currently returns "loading" for incomplete catalogs;
  distinguish "retained restored value" from "initial empty load". Never clear
  restored badges merely because revalidation started.
- **Provider seeding**: restored entries start with expired TTLs, no error
  backoff, no running jobs and no paging continuation. Reset `fullReconcileAt`
  so startup scans every PR page. Queue local, provider and process
  revalidation immediately, independent of browser subscription, with bounded
  workers and coalescing. Restoration never triggers `git fetch`, pull or any
  mutation.
- **Bootstrap order**: emit the **active** project's snapshot first. Background
  projects follow in batches. Keep `bootstrap_complete` semantics. No protocol
  change is required. Do this task last in 7a (see tasks).
- **Browser on epoch change**: reset ordering guards, mark retained
  presentation stale and clear runtime authority until new snapshots arrive.
  Reject delayed prior-epoch responses and prior-connection callbacks. A
  disk-restored sequence-0 snapshot is treated like any snapshot in the
  *current* epoch.
- **Presentation**: stale text for worktree/local state and provider/CI badges
  in the branch list, canvas and PR list. "Live updates" transport health must
  never imply cached data is fresh.
- **Rejected**: resuming persisted sequences, restoring freshness as ready, or
  hydrating the store directly to bypass reconciliation.

### 3.3 Browser cache

None in this phase. The server cache already feeds the authenticated WS
bootstrap, and the paint clock starts at `ready`. Keep preference persistence
separate. Keep try/catch around every storage operation, including property
access. Test with all browser storage unavailable.

### 3.4 Code splitting

Keep the shell, connection handling, branch list, stale labels and snapshot
reconciliation eager. Declare lazy components at module scope. Give each
feature its own Suspense and Error Boundary.

| Modules | Boundary and loading behavior |
| --- | --- |
| `features/terminal/AgentTerminal.tsx`, `ProcessTerminal.tsx` | Component boundary at `features/terminal/BottomWorkspace.tsx`. Load when the selected runtime needs a terminal. Move xterm CSS out of `ApplicationRoot.tsx` into terminal loading. Fixed-size "Loading terminal…" fallback. Preserve selection and detach/replay semantics. |
| `features/files/FilesPage.tsx` | Route boundary for `/files`. |
| `features/files/FilesPanel.tsx` | Component boundary inside Inspector's Files tab. |
| `features/github/PullRequestDetail.tsx` | Component boundary in `PullRequestsPage.tsx`. The list stays visible. Owns the markdown stack (`react-markdown`, remark, unified). |
| `features/workspace/WorkspacePage.tsx` | Route boundary shared by `/`, `/worktrees`, `/agents`, keeping the focus wrappers. Moves `BonsaiCanvas`, React Flow and the layout behind the route. Move the React Flow CSS into this feature. The eager branch list paints while the canvas loads. |
| `features/settings/SettingsPage.tsx`, `features/logs/LogsPage.tsx` | Route boundaries with panel-local fallbacks. |
| `CreateWorktreeDialog`, `DeleteWorktreeDialog`, `StartAgentDialog`, `StartProcessDialog`, `EnvEditor`, `CommandPalette` | Component boundaries at their open-state owners in `components/layout/AppShell.tsx`. Preserve focus, dismissal and form behavior. |

Rules:

- Do not unmount loaded terminals on collapse or route change.
- Hidden mounted features must not trigger their first import unnecessarily.
- Audit shared imports and route preloading against the build graph.
- Extra chunks are same-origin `/assets/*`. Preserve `script-src 'self'`, SPA
  fallback and immutable asset caching. No inline loaders, blob scripts, CDN
  chunks or new service worker.
- **Chunk-failure recovery** (local-served UI only): register a
  `vite:preloadError` handler in the eager entry before loading the main
  surface. Show "This screen could not load" with a **Reload app** action,
  keeping the shell and stale data visible. Provide an eager fallback for
  initial-surface failure. Never auto-reload in a loop and never swallow the
  rejection without showing recovery.
- Embedded server: missing `/assets/*` already returns 404. Add a test that a
  stale hashed chunk after an upgrade fails into the recovery UI.

### 3.5 Dead dependency and layout worker

Remove `elkjs` and its lockfile entry after confirming the build graph.
Profile the real layout at 25, 100 and 500 worktrees (first placement and
Auto-layout). Add a worker **only** if layout computation shows p95 > 50 ms on
the agreed machine, or attributable tasks > 50 ms in at least 10% of runs. If
so, move the existing algorithm (not a new one) to a module worker in a
separate commit, with request generations to discard late replies. Otherwise
record the profile and stop.

### 3.6 Bundle budget

Add `web/scripts/check-bundle-budget.mjs` and `web/bundle-budget.json`
(**new**), plus a `bundle:check` package script run after the production build
in frontend CI.

- Resolve chunks by manifest facade and import graph, never hashed filenames.
- Record exact raw bytes and deterministic gzip bytes for: entry, main
  application chunk, their deduplicated static closures, first-screen JS, and
  each lazy feature (for review).
- Freeze the Phase 4 reference separately from the final Phase 7 budget.
- Completion: main application bytes ≤ **70%** of the Phase 4 reference.
- Afterwards CI fails when the main chunk **or its static closure** exceeds
  **110%** of the approved baseline. First-screen JS must not exceed the
  pre-P7 baseline without a documented decision.
- Fixed Linux CI toolchain, frozen lockfile, production mode, normalized
  environment. Compare integer bytes, not rounded logs or HTTP transfer sizes.
  Confirm two clean builds agree before freezing.
- Budget updates need an intentional file change with an explanation. CI never
  updates it.
- Add `feat/bonsai-web` to the workflow's `push.branches` (currently `main`
  only). Keep PR checks and existing validations.

---

## 4. Tasks and delivery

Three stacked PRs, all into `feat/bonsai-web`:

- **7a** persistence and stale presentation (Opus 5.5, it touches ordering)
- **7b** lazy boundaries and chunk recovery (Sonnet 5.5)
- **7c** budget and final evidence (Sonnet 5.5)

7a and 7b touch different files and can run in two parallel sessions. 7c goes
last. Record the split as a Phase 7 process exception.

| # | Task | Files | Acceptance | Order |
| --- | --- | --- | --- | --- |
| 0 | Baselines and measurement fixtures | plan; `web/vite.config.ts`; `web/e2e/warm-start.spec.ts` (new); fixture test in `localapi` | Exact Phase 4 / pre-P7 size tables and ready-to-paint JSON, with unchanged behavior | First |
| 1 | Snapshot DTO, schema and writer | new persistence files in `internal/server/localapi`; existing config/storage helpers | Round trip, permissions, caps, corrupt / version mismatch, failed replacement; no excluded fields | After 0 |
| 2 | Restore and startup revalidation | `localapi/server.go`, `state_sync.go`, `enrichment.go`, `pr_catalog.go`, `snapshot.go` | New epoch, sequence-0 stale bootstrap appears before a blocked provider finishes; full catalog reconcile; runtime authority recovers | After 1 |
| 3 | Browser ordering and stale presentation | `web/src/api/git.ts`, `snapshotReconciliation.ts`, `types.ts`; branch list, canvas graph and node, Inspector, PR catalog/list | Active project paints during bootstrap; stale labels survive partial updates; old epoch or generation cannot overwrite | Agree wire fields with 2. Completes 7a data path |
| 4 | Active-project-first bootstrap | `localapi/events.go`; `web/src/api/git.ts` | Multi-project timing test; revertable on its own | Last in 7a |
| 5 | Lazy feature boundaries; remove `elkjs` | router, `ApplicationRoot`, `AppShell`, terminal owner and components, Inspector, Files, PR page and detail; `package.json` and lockfile | Network assertions show unused features absent; navigation, focus, terminal lifecycle and layout regressions pass | Parallel with 1–4 |
| 6 | Chunk recovery | `web/src/main.tsx`; new eager recovery module; `webui` tests | Missing hashed JS/CSS and offline imports show recovery; asset 404 and page fallback still correct | After 5. Completes 7b |
| 7 | Layout worker (only if the threshold is crossed) | canvas/layout files, new worker | Profile shows need and gain; late reply cannot overwrite placement | Optional |
| 8 | Budget script, CI and final evidence | new script and budget; `package.json`, `vite.config.ts`, frontend workflow, plan | Deliberate 11% artifact growth fails; clean builds agree; exit criteria met; full validation passes | Last. Completes 7c |

Keep benchmarks isolated: `__sync-bench --cold` is a no-op and `--warm` is an
in-process forced refresh. Preserve those semantics. Do not turn it into a
personal-disk-cache benchmark.

---

## 5. Test plan

### Go

- DTO exclusion with credential, capability, command, PR-body, patch and log
  sentinels. Scan captured diagnostics and disk artifacts.
- Missing, corrupt, unknown-version, oversized and unavailable-directory cases.
- Atomic-replace failure keeps the previous valid file. Concurrent export,
  eviction, commit and shutdown.
- Restart gives a new epoch. Sequence 0 is accepted and live commits advance.
  Removed projects and late writes cannot reappear.
- A successful empty live result clears lists. Failed or partial paging keeps
  stale values and badges.
- Restored multi-page catalog with unchanged page one but a closed PR on a
  later page: startup reads every page.
- No Git network operation is triggered by restoration.
- Plain-build placeholder and tagged production embedding remain correct.

```sh
go test -race ./internal/server/localapi ./internal/git/github/ghcli ./internal/git/github/app ./internal/storage/git
```

### Frontend unit

Extend the reconciliation, storage and terminal tests for: restored
provenance, nested CI freshness, partial replacement, epoch change, stale
catalog presentation and lazy-load failure. Verify snapshot updates still do
not serialize canonical entities into preferences.

### Playwright (local-served entry)

1. Populate the cache through a real fixture commit, recreate server state
   with the same temp cache dir, hold provider revalidation pending. Worktree
   list and PR badges appear with visible stale labels.
2. Release local, catalog and checks independently. Only validated components
   lose their stale labels.
3. Missing, corrupt, oversized and unsupported-version cache take the cold
   path, then recover.
4. Epoch change with delayed old events and responses. Current state stays
   intact.
5. A removed project stays absent after restart.
6. `localStorage`, `sessionStorage` and `indexedDB` access throws. Connection,
   warm rendering and navigation still work.
7. Lazy files, PR detail, terminal and workspace load under the production CSP.
   Unused feature chunks are not fetched.
8. A 404 on an old hashed chunk, a CSS preload failure and an offline chunk
   abort all show recovery, with no reload loop or blank app.
9. Terminal input, resize, reload, replay and Stop, plus canvas placement and
   render-isolation, still pass.

### Exit measurements

**Fixture**: three projects. The active project has 25 worktrees and 50 open
PRs. A second run uses 100 worktrees and 500 PRs. Providers are deterministic
and local. No real GitHub.

**≤ 300 ms**

- Real authenticated WS bootstrap, not direct store hydration.
- Seed a valid cache, restart the fixture, fresh browser context with empty
  canonical state, three discarded warm-ups, then 30 measured runs on the same
  pinned Chromium and viewport.
- Start at browser receipt of authenticated `ready`. Finish after the expected
  branch rows/count and PR badges have painted. Provider revalidation stays
  blocked until the measurement ends.
- **Gate: p95 ≤ 300 ms, run locally on a quiet machine.** Report p50, p95 and
  max. Failures and missing paints count as failures.
- Record cache-load/startup time and navigation-to-ready separately, so
  latency moved before `ready` stays visible.
- Use the measurement build for diagnostics and confirm the final number with
  the normal production build served through the embedded UI. Record the build
  mode (the e2e build uses React profiling).
- CI **reports** the numbers as artifacts and does not block on them.

**≥ 30% (guard)**: build Phase 4 and final P7 with identical toolchains.
Compare exact main-application bytes and report the ratio. Inspect static
closures and first-screen requests to rule out eager-code relocation. Keep
gzip alongside raw.

### Validation before each PR

```sh
gofmt -l $(git ls-files '*.go')
go vet ./...
go build ./...
go test ./...
go test -race ./...
pnpm -C web typecheck
pnpm -C web lint
pnpm -C web test
pnpm -C web build
go test -tags embedui ./web/
pnpm -C web test:e2e
```

Plain `go build` must still need no frontend toolchain.

---

## 6. Risks and rollback

| Risk | Mitigation / detection | Rollback |
| --- | --- | --- |
| Restored values look fresh | Per-component and nested stale states, `source: disk`, expired TTLs, blocked-provider test | Disable restoration. Cold sync continues |
| Old repo or remote data becomes authoritative | Remote-identity check on restore, full startup reconcile, removed-project pruning | Disable restoration |
| Cache disk or memory growth | Byte and count caps, whole-entry eviction, removal pruning, measure retained memory including serialization copies | Stop persistence, delete only its namespace |
| Large cache delays startup | Bounded decode before serving, size checks before allocation, startup measurements | Skip oversized documents, lower caps |
| Partial writes, overlapping instances | Atomic replace, one writer per namespace, bounded shutdown flush | Ignore cache failure, keep last valid file |
| Bootstrap waits on background projects | Active-first emission and a multi-project timing test | Revert task 4 alone |
| Lazy chunk fails after upgrade or offline | Asset 404, no-store HTML, eager recovery UI | Revert the affected split. 7a is independent |
| Splitting regresses terminal or layout state | Keep mounted terminal lifecycle, placement and generation guards | Revert the feature split or worker commit |
| Size win is only eager bytes moved | Main, closure and first-screen budgets | Reject the budget update until the graph is fixed |

Cache failures must never weaken loopback / Host / Origin / capability
checks, expose secrets, stop local features, or require Node for ordinary Go
builds.

---

## 7. Open questions (defaults let work proceed)

| Question | Default |
| --- | --- |
| "Main chunk" = ApplicationRoot or the small entry? | ApplicationRoot, matching Phase 4's 729.73 kB. Report and budget the entry separately. |
| Pre-WS IndexedDB presentation? | No. Revisit only after measuring navigation-to-ready latency. |
| Metadata-only persistence, excluding PR descriptions and runtime summaries? | Yes. Detail and runtime authorities revalidate separately. |
| Cache limits and maximum age? | Limits above, 7-day age cap. Younger restored records are always stale until validated. |
| Layout worker threshold? | p95 > 50 ms on the stated fixtures. No ELK worker. |
| Delivery structure? | 7a / 7b / 7c stacked into `feat/bonsai-web`. Mark Phase 7 done only after final measurements and validation. |

## Deferred follow-up

**Persisted GitHub ETag cache.** Do after Phase 7, only if measurements show
restart-time GitHub reads matter.

- Persist only warm-sync GET representations (repo metadata, branch pages,
  open-PR catalog pages, check-run and status pages). Never persist detail,
  files, diffs, comments, reviews or hooks.
- Use endpoint-specific metadata representations (versioned separately) that
  keep every field the eligible requests consume, including repository
  `permissions.admin` (used by the hook manager). Never persist Authorization,
  cookies or unrestricted headers.
- Never return disk data as fresh without a successful conditional request.
  Keep network-first cache-hit behavior. A `304` without an eligible body gets
  one unconditional retry.
- Key by auth host, exact API base, URL/query, Accept, API version and a full
  SHA-256 token fingerprint (kept server-side).
- Invalidate on removed project, changed remote identity, token change
  (including background token replacement, not just a synchronous `401`),
  host change, and cache/representation version mismatch. A `401`/`403`/`404`
  must not fall back to cached successful responses.
- Tests: decoder-equivalence for retained fields, actual `304` after restart,
  rotation and host-change invalidation.

---

## Kickoff prompts

Use one per session. Set the base branch to `feat/bonsai-web`.

**7a**

```text
Read docs/phase-7-plan.md in full, then docs/bonsai-web-plan.md for the
Global rails. Implement 7a only (tasks 0-4: measurement commit, snapshot
persistence, restore and startup revalidation, browser stale presentation,
active-first bootstrap). Branch from feat/bonsai-web and open the PR into
feat/bonsai-web, not main. Do NOT implement ETag persistence (deferred) or
any frontend code splitting (7b). Start in plan mode: list the files you will
touch and flag anything in the plan that no longer matches the code. When
done, run the Validation list and map each exit criterion to evidence in the
PR description.
```

**7b**

```text
Read docs/phase-7-plan.md in full, then docs/bonsai-web-plan.md for the
Global rails. Implement 7b only (tasks 5-6: lazy feature boundaries, remove
elkjs, chunk-failure recovery). Branch from feat/bonsai-web and open the PR
into feat/bonsai-web, not main. Do not touch server persistence or the hosted
Caddy/Dockerfile. Start in plan mode and flag anything that no longer matches
the code. Include a before/after chunk table (exact bytes) in the PR.
```

**7c**

```text
Read docs/phase-7-plan.md in full. 7a and 7b are merged into feat/bonsai-web.
Implement 7c only (task 8: bundle budget script, CI check, final evidence).
Rebuild the Phase 4 reference and fill every table in the Phase 7 section of
docs/bonsai-web-plan.md with measured numbers. Deliberately add 11% to a
chunk to prove the check fails, then revert it. Branch from and open the PR
into feat/bonsai-web.
```
