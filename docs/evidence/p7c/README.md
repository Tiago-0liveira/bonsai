# Phase 7c evidence

`phase4.json`, `pre-p7.json`, `7a.json` and `final-a.json` contain exact artifact
and static-closure measurements, source revisions and toolchains. Historical
features without separate chunks identify their parent. `determinism.json`
compares two independent clean production builds. `padding-proof.json` records
the nonzero checker exit after >11% application growth and verifies restoration
of the original SHA-256. `web/bundle-budget.json` keeps the historical references
separate from the candidate final baseline.

The compiled UI source is `17f69bb`, with `build.manifest: true` added to Vite's
configuration. This setting changes no emitted JS, HTML, CSS or font bytes.
Measurement instrumentation and checker implementation are reviewed in this PR.

Rebuild a historical reference in an isolated directory (replace the revision
for pre-P7 or standalone 7a). Use Node 22.12.0 and pnpm 12.9.1:

```sh
p7_dir=$(mktemp -d /tmp/bonsai-p7-history.XXXXXX)
git archive ee66e17 web | tar -x -C "$p7_dir"
pnpm -C "$p7_dir/web" install --frozen-lockfile
TZ=UTC LC_ALL=C LANG=C pnpm -C "$p7_dir/web" build --manifest
pnpm -C web bundle:check --report-only --build-dir "$p7_dir/web/dist" \
  --source-revision "$(git rev-parse ee66e17)" --json /tmp/phase4.json
```

Run the normal enforcement and focused tests:

```sh
pnpm -C web build
pnpm -C web bundle:check --json bundle-reports/production.json
pnpm -C web test:bundle
```

Measure both UI modes and fixture sizes on a quiet machine. Omit
`BONSAI_WARM_START_UI` for profiling and set `BONSAI_P7_SIZE=large` for 100/500:

```sh
BONSAI_P7_RUNS=30 BONSAI_P7_GATE=1 BONSAI_WARM_START_UI=dist \
  pnpm -C web test:e2e --config playwright.measure.config.ts \
  --grep 'cold start:|warm start:'
BONSAI_P7_MEASURE=1 BONSAI_P7_CACHE_REPORT=web/bundle-reports/cache.json \
  go test ./internal/server/localapi -run '^TestWarmStartCacheMeasurements$' -count=1 -v
```

Browser JSON is written to `web/bundle-reports/measurements/`; representative
traces are in `web/test-results/measurements/`. CI uploads those paths even if a
check fails. The cache JSON reports all 30 samples for each fixture, maximum
retained memory after GC with the restored state kept alive, observed live heap
growth sampled every 100 microseconds plus endpoints, and cumulative allocations.
Live-heap observation is not a guarantee of the true peak, and allocation totals
include the measurement sampler's small overhead. Historical 7a's last retained
sample and best-of-five timings use different definitions and are labeled in the
[performance documentation](../../performance.md).

Phase 7 is merged in #69, #68 and #70. Integration `2824bcb` passed
[Go CI](https://github.com/Tiago-0liveira/bonsai/actions/runs/38099776238) and
[frontend CI](https://github.com/Tiago-0liveira/bonsai/actions/runs/38099776242).
The reports preserve their original source revisions; see
[performance.md](../../performance.md).
