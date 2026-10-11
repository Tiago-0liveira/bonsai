# Historical sync measurements

Preserved from the project plan at integration `2824bcb`. Source revisions,
repository states and measurement methods are retained as originally recorded;
these results are not Phase 8 measurements. A result revision absent in the
original table is not inferred here.

**Baseline** (commit `7efada9` + instrumentation, `bonsai __sync-bench --cold`,
WSL2, median of 3 runs, live GitHub through the user's `gh` login)

| Repo | Worktrees | Inventory | Local ready | PR catalog | All checks | git spawns | gh spawns |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| bonsai | 15 | 11 ms | 80 ms | 2173 ms | 15009 ms | 62 | 21 |
| bonsai (/mnt/c, 6 worktrees) | 6 | 472 ms | 8227 ms | 10362 ms | 11794 ms | 29 | 5 |
| large repo (many branches/PRs) | n/a | | | | | | |

Reading the numbers:
- On the Windows drive the local pass is about 100× slower (`/mnt/c` crosses the
  9P boundary), so Phase 1's git-process cuts matter most there.
- Everything after the PR catalog is the serial per-worktree checks loop:
  about 1 s per worktree natively, which is the Phase 1/2 target.
- No large repo was available, so that row stays empty.


**Results** (`__sync-bench --cold --repo ~/bonsai`, WSL2, median of 3 runs,
live GitHub through the user's `gh` login, baseline and Phase 1 binaries run
alternately on the same repository state)

| Metric | Baseline (`1a40462`) | Phase 1 | Phase 1 / baseline | Exit criterion |
| --- | ---: | ---: | ---: | --- |
| Worktrees | 21 | 21 | | |
| Inventory | 33 ms | 17 ms | 52% | |
| Local ready | 119 ms | 43 ms | 36% | ≤ 60% ✓ |
| PR catalog | 2191 ms | 693 ms | 32% | ≤ 50% ✓ |
| All checks | 19224 ms | 4044 ms | 21% | |
| git spawns | 67 | 30 | 45% (−55%) | ≥ 50% down ✓ |
| gh spawns | 26 | 26 | 100% | unchanged (Phase 2) |

- The Phase 0 table above was measured with 15 worktrees; the repository has
  21 now, so the baseline was re-measured on the current state instead of
  comparing against it. The `/mnt/c` copy no longer exists and was not
  re-measured, and there is still no large repo.
- PR catalog is now the slowest of the three concurrent `gh` calls (about
  620 to 670 ms each), which is the process-spawn floor that Phase 2 removes.
  All checks is bounded by 21 worktrees through a pool of 6 `gh` spawns.


*Results* (`__sync-bench --cold --repo ~/bonsai`, WSL2, median of 3 runs, live
GitHub through the user's `gh` login; Phase 1 = `b862fe7`, both binaries run
alternately on the same repository state)

| Metric | Phase 1 (`b862fe7`) | Phase 2 | Phase 2 / Phase 1 | Exit criterion |
| --- | ---: | ---: | ---: | --- |
| Worktrees | 22 | 22 | | |
| Inventory | 21 ms | 21 ms | | |
| Local ready | 43 ms | 45 ms | | |
| PR catalog | 644 ms | 366 ms | 57% (−43%) | ≥ 40% better ✓ (also 53% of the 693 ms Phase 1 table) |
| All checks | 4553 ms | 2472 ms | 54% | |
| gh spawns per cold load | 29 | 1 (startup token) | | ≤ 2 ✓ |
| GitHub HTTP requests (cold) | — | 29 | | |
| Warm forced refresh (`--warm`) | — | 47 requests, 47 × `304`, 0 gh | | only `304`s ✓ |
| PR catalog, cold process (`--no-prewarm`) | 644 ms | 732 ms | 114% | (not the criterion; see below) |

- GitHub itself answers in about 300–330 ms on a warm connection from this
  machine (measured with `curl`). The PR catalog is now that floor plus
  inventory. `gh auth token` costs about 250 ms and a TLS handshake about
  100 ms, which is why `Prewarm` takes both off the first open.
- A cold *process* (`--no-prewarm`) pays the token read and the handshake in
  series, so it is a little slower than Phase 1's three parallel `gh api`
  spawns. In production the API is up (and prewarmed) before the browser
  connects, and later opens never wait on gh because the token refreshes in
  the background.
- The warm refresh takes about 6.7 s of wall time for 47 conditional
  requests, mostly 22 worktrees × 2 checks reads through the pool of 6. It
  costs no primary rate limit.
- With `gh` removed from `PATH`, local ready is unchanged (49 ms), the
  provider reports `github_auth` and no GitHub request is made.

*Tests:* `internal/git/github/ghcli` (`token_test.go`, `http_test.go`), and
`internal/server/localapi/github_http_test.go`:
- `TestWarmProviderRefreshMakesOnlyNotModifiedRequests`
- `TestProviderFreshnessReportsMissingGitHubLogin`
- `TestProviderFreshnessReportsLowRateLimit`
- `TestGitHubTokenNeverLeaks`
- `TestNextProviderPoll`
- `TestPollProvidersFavoursTheVisibleProject`
- `TestEventSocketFocusFrames`, `TestFocusOnAProjectDiscoveredLater`
- `TestLowRateLimitKeepsPagingThePRCatalog`,
  `TestRateLimitResetOnlyExplainsTheCoreWindow`

