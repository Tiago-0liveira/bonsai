# Architecture and React fixes — implementation plan

Prepared 2026-10-02 from [report.md](report.md) and [report-react.md](report-react.md), against `49e973e` and the existing working-tree changes.

**Implement the immediate architecture fixes first, then React correctness, then render isolation.** Deliver each numbered batch as a separate reviewable change. All seven numbered batches are implemented. The report follow-ups below remain separate work.

**Progress — 2026-10-02**

- Starting point: clean `feat/ws-authoritative-state-and-repo-selection-next` worktree at `1b5ef88`. That commit already includes the prior worktree, refresh, file, and check-key fixes and their regression tests. No pending changes were discarded.
- Baseline: frozen dependency installation, frontend typecheck, all 93 unit tests, and production build passed. Vite reported its existing large-chunk warning.
- Batch 1: guarded agent creation/state mutation and shell/command execution; disabled palette/context-menu/inspector execution controls; replaced the launch form with an unavailability dialog; removed default agents, sessions, manufactured terminal output, and mock activity logs. Process cards identify status and configured port. Git palette commands invoke structured APIs directly. History/archive actions change presentation without inventing process completion, and external-editor notices report unavailability. Notices use a neutral information icon.
- Entry points reviewed: launch dialog and legacy launch action; project/worktree inspector and context menus; agent start/restart/stop/terminal menus and inspector controls; palette shortcut, launch/stop/shell and test/dev commands; direct store command callers; runtime cards; activity logs; external-editor preference actions. Supported Git requests and process stops before worktree deletion retain coverage.
- Validation: typecheck and production build passed; full unit run passed 109 tests, then the revised store suite passed all 34 tests (110 total covered). The browser suite passed 20 tests; its remaining PR smoke assertion passed after correcting a stale message and scoping the selector. All 21 browser scenarios are covered, including explicit agent presentation fixtures for layout and disabled execution controls.
- Existing browser failure: the PR smoke test expected `No pull requests.` instead of the existing `No open pull requests.` message. Reproduced on an isolated archive of `1b5ef88`, then corrected the test assertion without changing PR behavior.
- Batch 1 initially left the stored schema intact for batch 2. Its presentation fixture injection through legacy storage has now been replaced as part of batch 2.
- Batch 2: added schema version 1 on the existing `bonsai-web-workspace-v6` key, which is the only workspace key found in the current source and its Git history. Every read is sanitized and the existing record is rewritten before merging; failed rewrites attempt removal without a backup. Every write uses the same nested field allowlist. Environment values, simulated agents/processes/sessions/output, runtime IDs, and agent placements cannot persist or rehydrate. Allowed preferences, worktree placements, viewport, and board data survive.
- Batch 2 also removed sample environment defaults, guarded environment mutation actions, and replaced the editor with an unavailable message. Runtime selections fall back to their worktree/project; the first canonical snapshot resolves absent worktrees. Explicit rehydration preserves live managed processes while clearing simulated runtime data. The app has no automatic cross-tab hydration path; explicit rehydration is sanitized regardless of the record's declared version.
- Storage failures and malformed records use safe in-memory state and a persistent error with a retry that saves only allowed preferences/data. Stored values are never logged. Browser tests inject presentation fixtures after hydration through a hook included only in the `e2e` build; the normal production build was checked to exclude it.
- Batch 2 validation: frontend typecheck, all 129 unit tests, and production build passed. The full browser suite passed all 24 scenarios, including migration before connection, repeated reloads, malformed storage, unavailable storage and retry, environment containment, layout, and existing Git/process behavior. Additional unit cases cover orphan runtime placements and in-memory selection cleanup on rehydration.
- Batch 3: frontend CI now has `contents: read`, checks out `${{ github.sha }}` without persisted credentials, and runs on PRs into `main` and pushes to `main` using the existing frontend/workflow path filters. Removed the hard-coded branch/base-commit packaging job, its formatting/commit/push commands, and its change-bundle artifact. The `verify` job retains typecheck, tests, production build, Chromium installation, and browser checks; dependencies use a frozen install and the `web/pnpm-lock.yaml` cache input. Browser checks start their own server in CI instead of reusing an existing server.
- Required-check review: the live `main` protection configuration requires `CI passes`, with strict checks; no repository rulesets were returned. Its existing backend workflow supplies Go formatting/build/vet/tests/race checks and the required aggregate check. The frontend `verify` job name is preserved. Release and deployment workflows were not modified. Browser fixtures still mock the backend; extend path filters to backend/protocol files when the real-backend browser suite is added.
- Batch 3 validation: frozen installation passed with no lockfile changes; `actionlint` 1.7.7 reported no workflow errors; frontend typecheck, all 129 unit tests, production build, and all 24 browser tests with `CI=true` passed. Hosted validation remains pending the first actual PR/main run after these changes are pushed; local checks do not establish hosted CI success.
- Batch 4: the outer panel group, main workspace, and dock remain mounted with stable panel IDs across normal/maximized/collapsed states. The installed panel collapse API hides the dock; its content is inert and hidden, and portaled selects share the panel visibility boundary. Saved normal height survives programmatic resize/collapse; equal, negligible floating point, and invalid height updates are ignored. Retained terminal presentation defers opening/fitting until its host is measurable and outside an inert boundary.
- Runtime selection now derives scalar preferred-ID/needs-opening values, responds to asynchronous arrivals and removal, and preserves valid user choices. Opening a process records its worktree; repeated equivalent opens produce no store notification. React Hooks rules run as errors through `pnpm lint` locally and in frontend CI. The canvas's documented topology-only effect and the dialog's initialization-on-open effect retain their intended triggers.
- Batch 4 validation: frozen install, frontend typecheck, Hooks lint, all 138 unit tests, production build, and workflow `actionlint` passed. All 27 browser tests passed with `CI=true`. New tests cover both panel mounts against a Strict Mode baseline, main/dock drafts, panel identity, canvas viewport, actual resize/maximize/collapse, collapsed startup, hidden focus/portal behavior, terminal measurement guards, runtime arrival/removal/project changes, and equivalent updates without selection loops. jsdom panel tests resolve the installed library's client build because its Node export strips registration/layout effects for SSR. Hosted CI validation remains pending a push.
- Batch 5: extracted pure snapshot-to-entity reconciliation, with complete nested content comparison and sharing by stable ID. Snapshot ordering, epoch/generation guards, raw file-revision tracking, and creation/selection reconciliation remain at the transport boundary. Only changed fields are patched; unchanged entities, collections, placements, agents, and runtime lists retain identity. Catalog removal also prunes agents, worktree/group/project placements, and selection references. Project-scoped selectors isolate canvas, inspector, runtime, and PR consumers; PR catalog rows retain identity between relevant changes.
- Batch 5 validation: typecheck, Hooks lint, all 144 unit tests, and all 27 browser tests passed. Permanent probes cover process-only and inactive-project isolation, identical higher sequences with no observable update, nested checks with unchanged timestamps, connection/freshness contents, raw path changes, and worktree/project deletion. Existing ordering, file refresh, and worktree mutation regressions continue to pass.
- Batch 6: removed persistence middleware from the live store. A dedicated subscriber examines only explicitly owned preference/data fields before invoking serialization/storage. Equal serialized payloads are skipped. Dock drag measurements stay in the panel controller until completion; layout/viewport writes are coalesced with a 180 ms delay and flushed on router navigation, visibility/page lifecycle, and teardown. Horizontal panels retain their existing storage key and layout format through a guarded preference writer that captures the latest layout directly. Notices render through their own subscriber. Safe synchronous migration, explicit rehydration, storage alerts, and retry remain available through the preference boundary.
- Batch 6 validation: all 154 unit tests, typecheck and Hooks lint passed, with storage spies covering zero writer calls for live updates, equal setters, deferred final writes, lifecycle flushing, retry deduplication, and horizontal layouts. All four affected browser lifecycle tests passed, including one final-height save and immediate navigation/reload. The final full browser run also covers migration, failures, and layout interactions.
- Batch 7: extracted pure graph projection and separate node/edge, selection, and geometry reconciliation. Unchanged nodes/data/edges/measurements survive status updates; local drag and companion positions are preserved until canonical geometry changes. Placement metadata is excluded from rendered coordinates. PR labels recompute only when node rectangles or PR-edge topology change. Recursive branch/agent indexes retain structural identity; rows subscribe to their own entity and selected/collapsed booleans, with cycle detection retained. File navigation and content have separate subscriptions; worktree dialog bodies mount only while open and retain deliberate opening/draft behavior. Memoized node shells and stable React Flow callbacks now sit behind stable props.
- Batch 7 validation: typecheck, Hooks lint, all 164 unit tests, production build, and all 29 browser tests passed. Graph tests check object identity, status/selection updates, drag companions, canonical placements, and measured History geometry. Existing expansion/History/auto-layout browser checks now inspect in-memory placements independently of persistence. Closed-dialog and file-content probes verify subscription isolation. Frozen installation, workflow `actionlint`, and whitespace checks passed. Normal production output was checked to exclude the test store and profiling probes; its existing large-chunk warning remains. Hosted CI remains pending a push.

**Repeatable browser profile**

Run `CI=true pnpm --dir web test:e2e e2e/render-isolation.spec.ts`. The test uses the same mocked dataset and production profiling renderer before and after Batch 7, attaches `workspace-profile.json`, and records graph computations, label placements, main-workspace Profiler commits/duration, API requests, and workspace-storage writes independently. Profiling and the fixture hook exist only in the `e2e` build.

| Scenario | Graph computations, before → after | Label placements, before → after | Main commits, before → after | API requests, before → after | Preference writes, before → after | Profiler duration ms, before → after |
| --- | --- | --- | --- | --- | --- | --- |
| Process only | 0 → 0 | 0 → 0 | 0 → 0 | 0 → 0 | 0 → 0 | 0 → 0 |
| Inactive project | 0 → 0 | 0 → 0 | 0 → 0 | 0 → 0 | 0 → 0 | 0 → 0 |
| Selection only | 0 → 0 | 1 → 0 | 4 → 4 | 4 → 4 | 1 → 1 | 3.9 → 1.1 |
| Worktree status | 1 → 1 | 1 → 0 | 3 → 3 | 0 → 0 | 0 → 0 | 2.9 → 1.3 |

These durations are observations from individual local runs, not a percentage performance guarantee or CI timing threshold. Behavioral assertions use isolation/geometry counters rather than exact full-app commit counts. The selection's API requests are existing worktree file/diff/PR reads; preference writes reflect the deliberate user selection.

| Order | Deliverable | Complexity | Expected impact | Rough effort |
| --- | --- | --- | --- | --- |
| 1 | Truthful execution controls | Low–medium | Removes false “running” and success states | 1–2 days |
| 2 | Safe browser-storage migration | Medium | Removes environment values and stale simulated runtimes from persistence | 1–2 days |
| 3 | Read-only, reproducible frontend CI | Low | Prevents branch mutation and revision mismatch | Half a day |
| 4 | Stable workspace lifecycle and correct runtime selection | Medium | Preserves UI state and handles asynchronously arriving processes | 1–2 days |
| 5 | Stable snapshot/entity references and scoped subscriptions | High | Stops unrelated data updates from rebuilding the active workspace | 2–4 days |
| 6 | Deliberate preference persistence | Medium | Stops live updates and resize events from rewriting browser storage | 1–2 days |
| 7 | Incremental canvas updates and narrower component work | High | Reduces graph, label, inspector, and tree work | 2–4 days |

Estimates assume one engineer familiar with the repository and include focused validation. They are planning ranges, not delivery commitments. Batches 1–4 form the first release milestone; batches 5–7 form the rendering milestone.

**Baseline and constraints**

- Record the starting diff, including the untracked regression tests. Build on the pending missing-worktree, CI-refresh, file-refresh, and check-key fixes without discarding or silently absorbing unrelated work.
- Before implementation, run frontend typecheck, unit tests, and build once; record existing failures separately. The reports' passing checks are historical evidence, not a substitute for that baseline.
- Keep protocol 3, epoch/sequence rejection, bootstrap buffering, and worktree mutation semantics intact during the React work.
- Establish deterministic render/identity/storage probes for process-only, CI-only, identical, and inactive-project updates. Use realistic browser profiling later to measure duration; do not promise a percentage speedup from unit-test render counts.
- Keep real process controls, logs, and structured Git actions usable while unsupported agent/shell controls are disabled.

**Batch 1 — Make execution behavior truthful**

Primary files: [bonsai.ts](web/src/stores/bonsai.ts), [StartAgentDialog.tsx](web/src/features/workspace/StartAgentDialog.tsx), [CommandPalette.tsx](web/src/features/command-palette/CommandPalette.tsx), [BonsaiNode.tsx](web/src/features/workspace/nodes/BonsaiNode.tsx), [Inspector.tsx](web/src/features/inspector/Inspector.tsx), and [BottomWorkspace.tsx](web/src/features/terminal/BottomWorkspace.tsx).

1. Inventory all entry points for simulated agent creation, restart/stop, shell sessions, and manufactured command output, including menus and keyboard actions.
2. Disable unsupported execution in the connected application and explain that the capability is unavailable. Guard the action layer as well as the button so alternate callers cannot manufacture successful execution.
3. Start connected state with empty simulated agent/session/output collections. Test fixtures may still inject presentation data explicitly.
4. Route supported Git commands directly through the existing structured API actions. Label process output according to what it actually shows; a process-status card must not imply an interactive shell.
5. Retain reusable presentation components where useful. A real agent bridge, PTY transport, and a new demo product mode are separate work, rather than prerequisites for this containment fix.

Acceptance: every connected-app execution control either invokes an existing supported operation or reports unavailability. An unsupported command never creates a running agent, successful test result, dev-server URL, or success notice. Existing Git/process actions retain their behavior.

Validation: action-level tests for unsupported invocation; a browser smoke test covering palette and context-menu entry points; update tests that currently assert simulated execution as product behavior. Hydration cleanup is completed in batch 2.

**Batch 2 — Remove persisted environment values and simulated runtime state**

Primary files: [bonsai.ts](web/src/stores/bonsai.ts), [EnvEditor.tsx](web/src/features/workspace/EnvEditor.tsx), and store persistence tests. Add a focused persistence/migration module if needed.

1. Remove all environment values from the persisted payload, regardless of the `secret` display flag. The current editor has no real environment-write contract, so disable value editing in the connected app until that contract exists.
2. Version the stored schema and sanitize the existing `bonsai-web-workspace-v6` record before merging it into live state. Changing only the key or `partialize` would leave old sensitive values on disk or allow them to rehydrate.
3. Preserve allowed user preferences, placements, and user-authored board data. Drop simulated runtime records; reconcile dangling selected/open runtime IDs and agent placements to a valid project/worktree fallback.
4. Rewrite the existing record with the sanitized allowlist, or remove its legacy copy after a successful migration. Do not create a backup containing the removed values. Check actual legacy keys used by the application rather than clearing arbitrary local storage.
5. Handle malformed JSON and unavailable storage with safe in-memory defaults and a recoverable user-visible storage error. Never log stored values. Keep migrations idempotent and sanitize any supported cross-tab hydration path.

Acceptance: a legacy record containing environment values is sanitized on upgrade; the values do not enter live application state or remain in the application's migrated storage record. Preferences and board data survive. Simulated agents cannot reappear after reload.

Validation: hydration/reload tests for legacy data, repeated migration, malformed records, and storage failures. Assert on a synthetic marker value so tests verify removal without real credentials. The migration affects application-owned browser data only.

**Batch 3 — Make frontend CI read-only and reproducible**

Primary file: [frontend.yml](.github/workflows/frontend.yml). Coordinate with the existing read-only formatting and backend validation in [ci.yml](.github/workflows/ci.yml).

1. Set frontend workflow permissions to `contents: read` and remove the branch-writing, hard-coded `package-changed-files` job.
2. Verify the triggering PR merge revision or push revision consistently. Remove feature-branch/base-commit constants. Use intentional PR/main triggers and path filters; preserve required-check behavior.
3. Install with `pnpm install --frozen-lockfile` and use `web/pnpm-lock.yaml` as the dependency cache input. Fix an actual lockfile mismatch deliberately if the frozen install exposes one.
4. Retain typecheck, frontend tests, build, and browser checks. Use existing backend CI for Go formatting/tests, and include backend/protocol paths in browser integration triggers when the real-backend suite is added.
5. Omit the temporary change-bundle artifact. If packaging becomes a requirement later, make it an explicit non-mutating job that handles deleted files and records its source SHA.

Acceptance: no validation job commits or pushes; checks run against the intended triggering revision; dependency resolution uses the lockfile; normal release/deployment permissions remain separately scoped.

Validation: inspect workflow changes and trigger/check names, run the frozen install and corresponding local checks, then verify the first actual PR CI run. Do not claim hosted CI success from local YAML inspection alone.

**Batch 4 — Fix React lifecycle and asynchronous runtime selection**

Primary files: [AppShell.tsx](web/src/components/layout/AppShell.tsx), [BottomWorkspace.tsx](web/src/features/terminal/BottomWorkspace.tsx), [bonsai.ts](web/src/stores/bonsai.ts), and [package.json](web/package.json).

1. Keep `PanelGroup -> main Panel -> MainWorkspace` mounted across normal, maximized, and collapsed states. Use the installed panel library's supported collapse mechanism with stable panel identity.
2. Preserve main route input state, React Flow provider, viewport, and canvas refs when toggling the dock. Keep dock content mounted where its local state matters, and ensure hidden panels cannot receive focus or trigger invalid terminal measurements.
3. Add an equality guard to dock-height updates. Restore the last normal height after maximizing/collapsing without a resize feedback loop.
4. Derive a stable preferred runtime ID and whether it needs opening. Make the selection effect depend on those values and stable actions, so empty-to-populated runtime updates work without overriding a valid user selection.
5. Add React Hooks linting to local scripts and CI. Review effect intent individually; preserve documented initialization-on-open and topology-only behavior rather than indiscriminately adding unstable objects to dependency arrays.

Acceptance: dock collapse/expand produces zero main-workspace remounts after initial mount; route drafts and canvas state survive; the first arriving process is opened when appropriate; valid selected runtimes are preserved; repeated equivalent updates produce no selection loop.

Validation: lifecycle component test plus browser toggle/resize/maximize coverage; runtime arrival/removal/project-switch tests. Count mounts relative to an initial baseline so development Strict Mode does not produce a false failure.

**Batch 5 — Preserve entity identity and scope subscriptions**

Primary files: [git.ts](web/src/api/git.ts), [bonsai.ts](web/src/stores/bonsai.ts), [BonsaiCanvas.tsx](web/src/features/workspace/canvas/BonsaiCanvas.tsx), [Inspector.tsx](web/src/features/inspector/Inspector.tsx), and [usePullRequestCatalog.ts](web/src/features/github/usePullRequestCatalog.ts).

1. Extract snapshot-to-entity reconciliation into a pure, testable module. Keep transport/ordering guards and UI selection reconciliation explicit at the application boundary.
2. Reconcile entities by stable ID, retain unchanged objects, and retain collection references when membership, order, and contents are unchanged. Account for nested checks, file status, connection state, and freshness; do not compare only IDs or timestamps.
3. Emit a narrow patch containing only changed collections. Avoid allocating replacement agent, placement, and runtime collections unless deletion/selection cleanup actually changes them.
4. Provide stable project-specific collections or memoized selectors so updates to project B do not invalidate project A's graph. Initially preserve public interfaces where possible rather than migrating every consumer to a new normalized store in one change.
5. Narrow inspector and runtime consumers to their selected entity or stable project subset. Stabilize PR catalog derivation before relying on downstream `useMemo`.

Acceptance matrix:

| Update | Required result |
| --- | --- |
| Process only | Unchanged project/worktree/agent/placement references remain identical; file revision stays unchanged. |
| Inactive project | Active project's selected collections retain identity; active graph construction does not run. |
| One worktree/CI result | Only affected entities and dependent summaries change; unrelated entity references survive. |
| Identical snapshot | No observable store update or React consumer render. |
| Older epoch/sequence | Stale data remains rejected. |
| Worktree/project removal | Dangling selection, dock, and placement references are still reconciled correctly. |

Validation: restore the report's temporary render probe as permanent behavioral coverage, asserting the improved isolation. Retain existing ordering, deletion, creation-selection, and file-refresh regressions. Avoid brittle full-app exact render counts.

**Batch 6 — Persist preferences deliberately**

Depends on batch 2's schema migration; perform after batch 5 so live and user-state responsibilities are clearer.

1. Move persistence behind a dedicated preference/user-data boundary. Persist only explicitly owned preferences and board data; live Git/process snapshots do not trigger that writer.
2. Keep transient drag/resize state local to the interaction or panel controller. Commit preference changes at interaction completion where supported; otherwise debounce writes with an explicit flush on teardown/page lifecycle.
3. Avoid rewriting identical serialized preference payloads. Retain migrations and error handling from batch 2.
4. Move notice rendering into a small subscriber so notices do not update the entire shell's directly instantiated content.

Acceptance: a process/CI-only update causes zero preference writes; an unchanged setter causes zero writes; one completed resize persists its final normal height without writing once per movement; reload restores the saved preference. Final values survive navigation while a deferred write is pending.

Validation: storage spies, fake-timer tests for deferred writes, and browser resize/reload checks. Rendering no-op tests and storage-write tests remain separate because the current middleware treats them differently.

**Batch 7 — Update the graph and component subtrees incrementally**

Depends on stable entities/selectors from batch 5.

1. Separate graph topology, geometry, selection, and status derivation in [BonsaiCanvas.tsx](web/src/features/workspace/canvas/BonsaiCanvas.tsx). Reconcile React Flow nodes and edges by ID, preserving unchanged `data`, geometry, selection, and measured values.
2. Trigger PR-label placement from geometry/topology changes, not selection-only or unrelated status updates. Preserve current worktree-history sizing and local-placement behavior.
3. Build branch-child and worktree-agent indexes once for the recursive dock tree. Use row-specific collapsed/selected booleans and retain cycle detection.
4. Split file navigation from file content rendering; gate expensive closed-dialog bodies. Preserve deliberate form-reset/draft semantics when changing mounting boundaries.
5. Add `memo` and stable callbacks only at expensive boundaries whose props are now stable. Measure before introducing list virtualization or replacing the wire protocol with deltas.

Acceptance: inactive-project and process-only changes do not rebuild the active graph; a selection-only change does not rerun label geometry; one status change preserves unrelated node objects; dragging/group expansion/history/auto-layout remain correct.

Validation: existing layout tests and browser layout specs, graph identity tests, and repeatable React Profiler scenarios on the same dataset/build. Record graph computations, commits, duration, network requests, and storage writes separately.

**Follow-up work from the reports**

These remain explicit follow-ups after the immediate fixes above, rather than being hidden inside a React refactor:

| Work | Implementation direction | Completion evidence |
| --- | --- | --- |
| WebSocket lifecycle and deadlines | Distinguish slow-consumer recovery from authorization failure; define/recheck stream authorization lifetime; bound authentication/bootstrap and HTTP operations. Preserve mutation idempotency when a timeout leaves the result unknown. | Expiry/reconnect/slow-consumer tests, hanging-startup timeout tests, and mutation recovery coverage. |
| Worktree-specific file data | Replace the global invalidation dependency with worktree revisions and shared keyed reads; abort obsolete requests and expose retained-data freshness/errors. | Two worktrees do not refetch each other's data; target switching and content-sensitive edits stay correct. |
| Contract and real-backend browser tests | Validate serialized protocol fixtures; start a real isolated local API/daemon with temporary repositories using the contributor security mode. | Browser bootstrap, worktree mutation, and server restart/reconnect pass without mocked local HTTP/WS. |
| Cache and journal retention | Bound provider cache entries; define safe idempotency retention before pruning completed request records. | Bounded growth under synthetic churn and preserved retry guarantees. |
| Documentation | Correct snapshot/refresh semantics, discovery depth, intervals, and UI description of automatic fetch/guarded pull. | Documentation reviewed against the final implementation and visible Sync behavior. |
| Bundle and relay scaling | Profile route/terminal/layout loading; keep the relay single-instance until a transactional/shared event design is required. | Before/after bundle measurements; a separate design before multi-writer deployment. |

**Release checks and recovery**

For each batch, run the tests relevant to its behavior. At each release milestone run `pnpm --dir web typecheck`, `pnpm --dir web test`, and `pnpm --dir web build`, plus the new lint script after it exists. Run the affected Playwright suites for UI lifecycle and workflow changes. Backend follow-ups also require relevant Go tests/race checks and existing required CI checks.

Keep storage migrations forward-compatible and do not roll back to a build that rehydrates or writes environment values. Rendering batches should be independently revertible without reverting the containment fixes. If an optimization changes selection, layout, or ordering behavior, fix or revert that batch before proceeding.

The first milestone is complete when execution behavior is truthful, old persisted environment values are handled, CI is non-mutating, and dock/runtime transitions preserve correct UI state. The second is complete when targeted updates preserve unrelated identities, avoid unnecessary preference writes, and pass measured graph/render isolation checks.
