# Web performance and historical evidence

Phase 7 is merged: #69 (warm start), #68 (lazy features) and #70 (budgets).
Integration revision `2824bcb909174b2fddecaae43cc66fd7360be343` passed
[Go CI](https://github.com/Tiago-0liveira/bonsai/actions/runs/38099776238)
and [frontend CI](https://github.com/Tiago-0liveira/bonsai/actions/runs/38099776242).
The historical artifact measurements below retain their source revisions and
methods; they are not measurements of Phase 8. Exact reports remain in
[evidence/p7c](evidence/p7c/README.md) and `web/bundle-budget.json`.

## Current performance contract

Warm snapshots live under `<user state>/bonsai/web/warm-start/<namespace>`.
The namespace hashes roots settings, launch repository and browser security
mode. Each versioned document is limited to 16 MiB, each project to 2 MiB,
with at most 256 projects and a 7-day restore age limit. Oversized projects are
skipped whole. The coalescing writer exports immutable safe DTOs before I/O,
uses private files, cross-process locks and atomic replacement, and flushes
with a bounded graceful-shutdown deadline. Missing/corrupt/future caches cause
a cold start; settings remain intact.

Persisted fields are repository/worktree/branch identities, sanitized remote
identities, status counts, PR/check summaries and validation timestamps.
Capabilities, tokens, credentials, process commands/PIDs, agents, runtime
authority, terminal output, logs, file contents, patches, PR bodies, comments,
reviews, unrestricted errors and environment values are excluded. Restore
always creates sequence 0 in a new epoch. All restored metadata is stale
until validated; removed projects and changed remote identities discard it.

The entry installs chunk recovery eagerly. Workspace, PR detail, files, logs,
settings, terminals and overlays remain lazy. Branches can paint before the
canvas. Closed overlays and unused terminals do not import; activated terminals
survive dock collapse/navigation. Import failure offers an explicit reload.

`bundle:check` resolves stable manifest facades/names and traverses the static
import graph, including each root. First-screen JS is the deduplicated union
of entry, application and default workspace; CSS includes all emitted styles.
Raw integer bytes enforce the existing 70% Phase 4 application cap, 110% approved
application/closure caps and unchanged pre-P7 first-screen ceiling. CI never
updates the baseline. Budget changes require a rationale in the budget file.
The removed landing selector affects current measurements only; historical
reports and their original landing bytes remain unchanged.

## Measurement commands and limits

```sh
pnpm --dir web build
pnpm --dir web bundle:check --json bundle-reports/production.json
pnpm --dir web test:bundle
BONSAI_P7_RUNS=30 pnpm --dir web test:e2e --config playwright.measure.config.ts
BONSAI_P7_RUNS=30 BONSAI_P7_SIZE=large pnpm --dir web test:e2e --config playwright.measure.config.ts
BONSAI_P7_RUNS=30 BONSAI_WARM_START_UI=dist pnpm --dir web test:e2e --config playwright.measure.config.ts
BONSAI_P7_MEASURE=1 go test ./internal/server/localapi -run '^TestWarmStartCacheMeasurements$' -count=1 -v
```

CI uses Ubuntu 24.04, Node 22.12.0, pnpm 12.9.1, frozen installs, UTC/C locale,
pinned Chromium and 1440×900 viewports. Measurements discard three warm-ups
and collect 30 fresh-context samples for 25 worktrees/50 PRs and 100/500.
Warm revalidation is held until measured paint completes; requested assets must
match the default-workspace graph. Local warm p95 must be at most 300 ms; CI
reports timing without that machine-dependent gate. Evidence uploads include
reports and representative traces with the identified sample.

Cache measurements distinguish bytes, save/restore duration, retained heap,
sampled observed live heap and cumulative allocations. Sampling every 100 μs
cannot establish the true peak and adds a small allocation cost. Historical
best-of-five and last-retained-sample numbers keep those original labels.

## Historical lazy-feature measurements

*7b chunk measurements.* Before: integration `04e9683518ed4c677fd66dd2294cef43655ddd71`.
After: historical 7b source `065f594f0a68fdf906c825c362de536dd4bb79b9`, before integration with 7a. Node **22.12.0**, Vite
**5.4.21**, TypeScript **5.9.3**, production build, default environment.
The cached dependency lock exactly matched the baseline lock's dependency
section. Installed pnpm was **12.6.0**; the declared **12.9.1** could not be
fetched because registry access failed. Commands therefore invoked the cached
`tsc` and `vite` binaries directly (equivalent build steps); these are
same-toolchain 7b comparisons, not the pinned Phase 4/final-P7 gate in 7c.

Raw bytes come from emitted file lengths; gzip uses Node `gzipSync` with
level 9 (deterministic header). Closure totals deduplicate files and sum
per-file raw/gzip lengths. First-screen JS is the union of the entry,
application and default workspace static closures, including shared chunks;
CSS is all emitted CSS, counted once. “Eager in parent” means no separate
chunk existed; its before bytes are included in the parent. Two final
production builds matched byte-for-byte.

| Chunk / metric | Before raw / gzip bytes | After raw / gzip bytes |
| --- | ---: | ---: |
| ApplicationRoot | 735796 / 232294 | 380211 / 121783 |
| entry | 149517 / 48724 | 151858 / 49560 |
| WorkspacePage | eager in parent | 251978 / 80512 |
| FilesPage | eager in parent | 2911 / 1293 |
| FilesPanel | eager in parent | 5016 / 1728 |
| PullRequestsRoute | 210207 / 62599 | 17227 / 6543 |
| PullRequestDetail | eager in parent | 194056 / 57745 |
| SettingsPage | eager in parent | 3585 / 1442 |
| LogsPage | eager in parent | 746 / 416 |
| AgentTerminal | eager in parent | 5661 / 2698 |
| ProcessTerminal | eager in parent | 4191 / 1920 |
| CreateWorktreeDialog | eager in parent | 8003 / 2905 |
| DeleteWorktreeDialog | eager in parent | 3722 / 1647 |
| StartAgentDialog | eager in parent | 16443 / 5561 |
| StartProcessDialog | eager in parent | 10549 / 3802 |
| EnvEditor | eager in parent | 1592 / 887 |
| CommandPalette | eager in parent | 22037 / 7826 |
| xterm | 290556 / 72030 | 290556 / 72030 |
| addon-fit | 1771 / 811 | 1771 / 811 |
| Entry static closure | 149517 / 48724 | 151858 / 49560 |
| Application static closure | 887385 / 282140 | 534141 / 172466 |
| First-screen JS | 887385 / 282140 | 810248 / 261858 |
| All CSS | 91176 / 19706 | 91309 / 19849 |


ApplicationRoot decreased **48.33%** against this integration
baseline; its static closure decreased **39.81%**.
First-screen JS decreased **8.69%** in this historical comparison. The workspace
chunk still loads on `/`. Integrated measurements follow.


## Historical integrated P7 measurements

The historical Phase 4 table is preserved. The reports below rebuild Phase 4
`ee66e17`, pre-P7 `04e9683`, standalone 7a `3c4b1d6`, and integrated 7a/7b
`17f69bb` with Node **22.12.0**, pnpm **12.9.1**, Vite **5.4.21**, frozen
installs, production mode, Ubuntu **24.04**, and `TZ=UTC LC_ALL=C LANG=C`.
The integrated build adds only Vite's manifest setting, which changes no UI
artifact bytes. Exact source hashes and toolchain metadata are in
[`docs/evidence/p7c`](evidence/p7c/README.md) and `web/bundle-budget.json`.

A static closure **includes its root** and every transitive static JS import.
First-screen JS is the deduplicated union of the entry, application and default
workspace closures. This corrects the earlier definition that omitted the
workspace route. Gzip totals sum separately compressed files at level 9 with a
zero timestamp; all gates compare integer raw bytes. CSS means **all emitted
CSS**, not just entry/application styles. Unavailable historical lazy features
are explicitly included in their parent. Full reports include every lazy
feature and its static closure, including additional manifest dynamic entries.

| Metric (raw / gzip bytes) | Rebuilt Phase 4 | Pre-P7 | Standalone 7a | Integrated P7 |
| --- | ---: | ---: | ---: | ---: |
| ApplicationRoot | 730,211 / 230,214 | 735,796 / 232,294 | 737,936 / 232,980 | 381,967 / 122,372 |
| Entry | 149,517 / 48,727 | 149,517 / 48,724 | 149,517 / 48,728 | 151,858 / 49,560 |
| Application static closure (includes root) | 881,800 / 280,062 | 887,385 / 282,140 | 889,525 / 282,832 | 535,897 / 173,053 |
| Entry static closure (includes root) | 149,517 / 48,727 | 149,517 / 48,724 | 149,517 / 48,728 | 151,858 / 49,560 |
| First-screen JS | 881,800 / 280,062 | 887,385 / 282,140 | 889,525 / 282,832 | 812,388 / 262,570 |
| All emitted CSS | 91,176 / 19,706 | 91,176 / 19,706 | 91,363 / 19,731 | 91,496 / 19,878 |

ApplicationRoot is **47.69% smaller** than rebuilt Phase 4.
First-screen JS is **8.45% smaller** than pre-P7.
The unchanged first-screen ceiling is **887,385 B**. The application cap
is floor(70% × 730,211) = **511,147 B**. Application and its
static closure each have a 110% regression cap against the separate approved
final baseline; CI does not update that baseline. A budget update must explain
its rationale. An optional first-screen decision must identify its source
revision, byte ceiling and reason.

| Lazy chunk (raw / gzip bytes) | Rebuilt Phase 4 | Pre-P7 | Standalone 7a | Integrated P7 |
| --- | ---: | ---: | ---: | ---: |
| workspace | included in application | included in application | included in application | 252,362 / 80,639 |
| pullRequests | 210,872 / 62,924 | 210,207 / 62,599 | 210,597 / 62,720 | 17,618 / 6,660 |
| xterm | 290,556 / 72,029 | 290,556 / 72,030 | 290,556 / 72,030 | 290,556 / 72,029 |
| addonFit | 1,771 / 812 | 1,771 / 811 | 1,771 / 812 | 1,771 / 808 |
| landing | 18,930 / 5,244 | 18,930 / 5,244 | 18,930 / 5,244 | 18,930 / 5,242 |
| filesPage | included in application | included in application | included in application | 2,911 / 1,290 |
| filesPanel | included in application | included in application | included in application | 5,016 / 1,726 |
| pullRequestDetail | included in pullRequests | included in pullRequests | included in pullRequests | 194,056 / 57,744 |
| settings | included in application | included in application | included in application | 3,585 / 1,442 |
| logs | included in application | included in application | included in application | 746 / 413 |
| agentTerminal | included in application | included in application | included in application | 5,661 / 2,700 |
| processTerminal | included in application | included in application | included in application | 4,191 / 1,920 |
| commandPalette | included in application | included in application | included in application | 22,037 / 7,823 |
| createWorktree | included in application | included in application | included in application | 8,003 / 2,903 |
| deleteWorktree | included in application | included in application | included in application | 3,722 / 1,644 |
| startAgent | included in application | included in application | included in application | 16,443 / 5,557 |
| startProcess | included in application | included in application | included in application | 10,549 / 3,800 |
| envEditor | included in application | included in application | included in application | 1,592 / 885 |

Two independent clean builds, each with its own frozen install, agree
byte-for-byte across **44 files**, including the manifest, HTML, JS, CSS and
fonts. [`determinism.json`](evidence/p7c/determinism.json) records each SHA-256.
The deliberate failure appends a valid JS comment to the manifest-resolved
application artifact: **381,967 → 423,984 B** (> 111% of baseline). The checker
exits **1** with `FAIL application vs approved baseline (110%): 423984 B <=
420163 B`. Restoring the original artifact reproduces its SHA-256 and the
checker exits **0**. [`padding-proof.json`](evidence/p7c/padding-proof.json)
contains the diagnostic and hashes.

Historical browser and cache numbers below retain their original source and
method rather than being presented as measurements of the integrated build.

| Historical paint metric (p50 / p95 / max ms) | Pre-P7 (`04e9683`, profiling, cold) | Standalone 7a (`3c4b1d6`, profiling, warm) | Standalone 7a (`3c4b1d6`, production, warm) |
| --- | ---: | ---: | ---: |
| 25 worktrees / 50 PRs | 88.9 / 106.9 / 110.1 | 71.7 / 78.0 / 85.0 | 69.4 / 74.8 / 80.8 |
| 100 worktrees / 500 PRs | 213.8 / 236.3 / 238.3 | 117.0 / 150.8 / 174.0 | 107.4 / 142.9 / 143.6 |

Historical pre-P7 providers answered instantly: these cold results are a best
case, and pending providers produce no PR badges until they answer. Pre-P7 had
no snapshot cache. Historical 7a cache results were **69,629 B / 0.98 ms / 171
KB** for 25/50 and **332,455 B / 4.7 ms / 721 KB** for 100/500. Their timing was
best of five and their retained heap was the last positive delta, **not a peak**.
The new measurement reports maximum retained heap across 30 repetitions and
observed live heap during serialization and restore separately from transient
allocation totals; sampled live heap is not claimed as the true peak.

## Deferred performance work

Persist the GitHub ETag cache only if restart-time measurements justify it.
Eligible GET representations must be endpoint-specific safe metadata DTOs,
versioned independently, preserving all consumed fields such as repository
admin permission. Exclude detail, files, patches, comments, reviews and hooks.
Never persist Authorization, cookies or unrestricted headers.

Disk representations remain network-first: use conditional validation and retry
unconditionally once if a 304 lacks an eligible body. Keys must include auth
host, API base, URL/query, Accept, API version and a server-side SHA256 token
fingerprint. Removal, remote/token/host/version changes invalidate them; 401,
403 and 404 never fall back to successful cached data. Decoder-equivalence,
restart-time 304 and token/host invalidation tests are prerequisites.

GraphQL cold-load batching and layout workers also remain deferred until
measurements establish a benefit. Neither is part of the current acceptance gate.
