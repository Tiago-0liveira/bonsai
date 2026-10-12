# Theme switching validation — 2026-10-12

Bonsai remains the default. Modern Light and Modern Dark reuse the existing
21 Tailwind color tokens, browser preference schema, fonts, and layout. Theme
selection suppresses color transitions until the first frame has painted;
ordinary hover transitions then resume. Terminal options and static ANSI CSS
share a pure resolver, including custom overrides.

## Checks

- Frontend: 63 unit-test files / 588 tests, TypeScript, and lint passed.
- Browser: 22 theme, Settings, and lazy-feature checks passed under the real
  Go server's production CSP, including reload/first-content restoration,
  same-origin tab synchronization, nested keyboard navigation, modal focus,
  loading/failure dismissal, and a real PTY preserving its connection and output.
- All built-in palettes pass the existing contrast validator without warnings.
- Production build, 10 bundle-budget tests, all unchanged bundle limits, and
  `go test -tags embedui ./web/` passed. First-screen JavaScript is 813,240 raw
  bytes against the existing 887,385-byte ceiling; application and its static
  closure are 382,547 and 536,500 bytes, respectively.

Validation used Node 22.12.0, Go 1.26.1, Vite 5.4.21, Vitest 3.2.7, and the
installed Chromium 1243 runtime. This Windows-created worktree reused cached
WSL dependencies. Its ignored local Playwright configuration runs the normal
TypeScript/Vite build directly before the existing isolated Go fixtures,
avoiding package-manager auto-install during test startup. The tracked
Playwright configuration and package scripts remain unchanged.

## Performance method and results

At 1440×900, discard three warm-ups and record 30 alternating light/dark
selections with the picker already loaded. The final untraced run measured:

| Metric | Median | p95 |
| --- | ---: | ---: |
| Synchronous selection/application | 0.4 ms | 0.6 ms |
| First-frame paint proxy | 9.5 ms | 42.0 ms |
| Following-frame upper bound | 57.1 ms | 91.0 ms |

The paint proxy is a task scheduled after the first requestAnimationFrame
rendering opportunity. It excludes input transport and cold chunk loading,
and does not measure monitor presentation latency. The following-frame bound
is retained separately so an additional frame's scheduling is not mistaken
for first paint. An optional Chromium trace, enabled with
`BONSAI_THEME_TRACE=1`, corroborates the first renderer Paint timing; tracing
adds overhead and is excluded from the headline result. The traced run's
first Paint completion p95 was approximately 55 ms.

The switch test asserts zero API requests, graph rebuilds, PR-label
recomputations, and workspace-preference writes. Project, selection, viewport,
node placements, and dock state remain identical. Each changed selection
writes the theme preference once; selecting the current theme is a no-op.
Timing is reported without a machine-dependent CI gate.

The end-to-end tests write screenshots and JSON performance/trace attachments
into their standard output directories. This session also retained the 15
representative screenshots and reports under `web/.vite/theme-review/` for
local review; these generated artifacts are ignored by Git.
