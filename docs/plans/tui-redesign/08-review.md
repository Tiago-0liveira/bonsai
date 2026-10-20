# Plan 08 implementation review

Verdict: **not ready**.
The workflow is substantially implemented, but read-only enforcement and exact approval integrity have release-blocking gaps.
Fix B1 and H1–H4, then close the state/history seams below and rerun the affected checks before shipping.

## Scope and verification

Reviewed the complete uncommitted change set reported by `git status`, tracked diffs and the untracked approval, planner, revision, backend, provider and UI files. Read the three applicable AGENTS guides, plans 04/04b/07/08 and the Garden contract. Findings below are code-derived failure scenarios; no additional reproduction source or tests were written, no live GitHub mutations or provider inference sessions were run, and only this review file was added.

Checks performed:

- Targeted Garden/core/backend/provider/agenthost/agentsbackend/UI tests passed. The GitHub Garden adapter tests also passed separately.
- `go test -race` passed for Garden, gardenbackend, providers, agents, agenthost, agentsbackend and the entire UI tree **without excluding either terminal integration test**. Adapter and checkout-reader race tests passed separately. An additional uncached race run of `TestAgentsIntegrationLiveReadOnlyView` and `TestAgentsIntegrationTerminalMode` passed.
- `go vet ./...`, `go build ./...`, `GOOS=windows go build ./...`, tracked and untracked Go formatting and `git diff --check` passed. These results do not establish real-provider enforcement.
- The ordinary full Go run failed in `internal/core/pkgmgr` and `internal/server/localapi`. `/tmp/.git` exists as a read-only directory; `pkgmgr.resolveLocation` tests only for the existence of `.git` (`internal/core/pkgmgr/location.go:41`), explaining the false `/tmp` root and derived discovery failures. This code is unchanged by Plan 08.
- `TestAgentUsageRealProviders` expects the literal seed source `~/.claude` (`internal/server/localapi/agents_usage_test.go:393`) but does not clear inherited `CLAUDE_CONFIG_DIR`. Claude's setup schema legitimately derives the source from that variable (`internal/providers/claude/setup_spec.go:18`). The test passed with that variable removed. This is a pre-existing test-isolation defect, not a demonstrated planner regression.
- A second full run with that variable removed reproduced broad socket-related failures, including the two terminal tests. A focused holder test passed when run directly and failed through the `env -u` invocation with `listen unix ... setsockopt: operation not permitted`. The terminal fixtures start a shell provider with no `Launch.ReadOnly` option (`internal/ui/agents_integration_test.go:114`, `:167`); the new enforcement branch is conditional on that option (`internal/core/agenthost/start.go:66`). Those exclusions are therefore not justified as planner regressions. The environment-dependent full suite is **not green**, and a clean CI/full-suite result is still required.

## Exit criteria

| Plan 08 checkbox | Status | Evidence and qualification |
| --- | --- | --- |
| Four distinct actions; launch remains unavailable | Met | Separate validation, planner submission and plan-decision paths: `internal/ui/gardenvalidate.go:61`, `internal/ui/gardenplanner.go:256`, `internal/ui/gardenplan.go:326`; explicit launch-unavailable presentation at `internal/ui/gardenplan_view.go:351`. No implementation-launch path was added. |
| Gate one alone yields Approved without a planner | Met | `internal/core/garden/approve/service.go:182` writes approval/projections only; `internal/core/garden/work/work.go:130` places accepted work without a plan in Approved. Tested by `internal/core/garden/approve/approve_test.go:122` and `internal/gardenbackend/validate_test.go:61`. Projection races remain H1. |
| Planning enforces read-only and creates no implementation worktree | Partially met | No WorktreeID is passed (`internal/gardenbackend/planner.go:199`); Antigravity fails closed and Claude restricts available tools. Codex's unconditional capability claim leaves loaded mutation tools outside the demonstrated restriction: B1. |
| Questions, cancellation, failure and retries preserve history | Partially met | Existing holder terminal supports interaction; attempt states advance monotonically (`internal/core/garden/plan/service.go:301`), cancellation is recorded before stopping (`:379`), retry creates another ID (`:243`). Crash recovery can lose the session binding: M2. |
| Gate two yields Ready without a coding agent | Partially met | Happy path works (`internal/core/garden/planrev/service.go:289`, `internal/core/garden/planrev/review.go:221`, `internal/gardenbackend/gatetwo_test.go:36`). No coding agent starts, but the exact pair can change beneath confirmation or inherit a bad fingerprint/revoked decision: H2–H4. |
| Stale/missing/tampered/unauthorized and label-only changes cannot bypass gates or start local runs | Partially met | Many negative cases are tested (`internal/core/garden/planrev/gate_two_test.go:95`). Readers do not launch sessions. H3/H4 defeat the stronger Ready guarantee; H2 defeats exact confirmation. |
| State/actor/revision/late-event/read-only/focus tests; race and full Go checks pass | Partially met | Meaningful negative and ordering tests exist, and unexcluded targeted race checks passed. Provider tests assert argv, not actual enforcement; the confirmation race and other cases below are missing. Full-suite failures remain as described above. |
| README, Garden guide, agents guide and TUI guide updated | Met | All four are modified and link to the new workflow. Accuracy problems remain in the documentation section below. |

The individual phase requirements also need these qualifications:

| Phase requirements | Status | Evidence |
| --- | --- | --- |
| 08.1 blockers, preview, authenticated records and retained superseded/revoked history | Partially met | `internal/core/garden/approve/assess.go:168`, `:218`, `:241`; `internal/ui/gardenvalidate_view.go:65`. Validation checks required sections/open questions. Its confirmation shows abbreviated identifiers and writes the metadata projection unsafely (H1). |
| 08.1 same Issue/type, Drafts → Approved, attention on invalid records, validation starts nothing | Partially met | `internal/core/garden/approve/service.go:266`, `:280`; `internal/core/garden/approve/assess.go:119`; tests at `internal/core/garden/approve/work_test.go:14`. H1 can still overwrite a concurrent managed-label edit. |
| 08.2 explicit local action, shared account/model/effort form and real read-only capability | Partially met | `internal/ui/gardenplanner.go:228`, `:256`; `internal/ui/agentlaunch.go:203`; B1 prevents declaring the capability complete. |
| 08.2 approved-spec/base binding, accepted permissions, attempt identity and explicit dirty context | Partially met | `internal/core/garden/plan/service.go:192`, `:220`, `:242`; dirty context and published-base identity are weaker than advertised (M1). |
| 08.2 existing holder/terminal, interaction, cancellation, failure and explicit retry | Partially met | `internal/ui/gardenplanner.go:382`, `:426`; `internal/core/garden/plan/service.go:301`, `:379`. M2 leaves an unrecoverable crash window. |
| 08.3 content-addressed revisions, source/base and all required plan sections | Partially met | `internal/core/garden/planrev/service.go:147`; `internal/core/garden/planrev/document.go:78`, `:107`. Hash normalization is deterministic, but M1 drops dirty-base identity and H3 permits source-fingerprint reuse. |
| 08.3 long-document navigation/search and usable unified comparison at 80 columns | Met | `internal/ui/gardenplan.go:527`; `internal/ui/gardenplan_view.go:98`, `:132`; tests at `internal/ui/gardenplan_test.go:83`, `:120`. |
| 08.3 changes reason/history, local confirmation for another run, reject → Approved | Met | `internal/core/garden/planrev/service.go:295`; `internal/core/garden/plan/service.go:207`; `internal/core/garden/work/work.go:122`. Ordinary remote prose only produces a hint. |
| 08.3 separate current/latest/approved and no approval of a moving “latest” | Partially met | Separate projections exist (`internal/core/garden/planrev/review.go:73`); the confirmation itself is not pinned (H2). |
| 08.4 confirmation/reverification, exact Ready pair, no coding launch | Partially met | Backend checks actor, spec, plan and epoch (`internal/gardenbackend/planreview.go:137`, `internal/core/garden/planrev/service.go:308`). H2–H4 remain. |
| 08.4 head changes retain old approval, revalidate changed specs, keep accepted work visible | Partially met | Normal changes are covered (`internal/core/garden/planrev/gate_two_test.go:61`, `:175`). Forged source fingerprints and integrity failures break the broader promise (H3, M4). |
| 08.4 external identity/revision/source/verified time, copy/open links, documented protocol | Met with integrity qualifications | `internal/ui/gardenplan_view.go:360`; `internal/gardenbackend/planreview.go:163`; `docs/garden.md:366`. H3/H4 still apply to external records. |

## Findings

### Blocker

**B1 — Codex is offered as read-only without constraining its loaded mutation tools.**

**Locations:** `internal/providers/codex/launch.go:168`, `:203`, `:25`; `internal/providers/codex/seed.go:34`.

The launch forces `-s read-only -a never` but retains the profile's CODEX_HOME and configuration. It does not disable configured MCP servers, plugins/apps or hooks. Seeding preserves `mcp_servers` and other execution configuration. A profile with an automatically allowed MCP mutation can therefore retain that tool in a planner session; a remote MCP server's writes are not constrained by a sandbox around local shell commands. The hardcoded `Supported: true` grants the role regardless of this configuration. Patch-tool coverage is also unverified, as the builders acknowledge.

This is a code/configuration inference supported by the official [sandbox configuration reference](https://developers.openai.com/codex/config-reference) and [MCP configuration/approval controls](https://developers.openai.com/codex/mcp), not a real-CLI exploit reproduced in this review. Those controls are distinct; emitting shell sandbox flags does not establish that every enabled tool is read-only. The argv-only tests at `internal/providers/codex/readonly_test.go:11` cannot prove the advertised guarantee.

**Suggested fix:** mark Codex unavailable for the planner until the complete tool surface is constrained and verified. Use a dedicated, sanitized runtime configuration or an outer isolation boundary; suppress mutation-capable integrations and execution hooks without changing the user's profile. Verify shell, patch and MCP mutation denial against supported CLI versions/platforms, including inherited/project configuration. Fail closed when that boundary cannot be established. Antigravity's explicit refusal is the appropriate current pattern.

### High

**H1 — Gate-one metadata projection can overwrite concurrent user-owned Issue text.**

**Locations:** `internal/git/github/ghcli/garden/adapter.go:777`, `:800`; `internal/core/garden/approve/service.go:225`.

Scenario: ReplaceMetadata GETs the body; someone saves a GitHub/mobile body edit; Bonsai PATCHes its earlier complete body with a changed first line. There is no atomic condition on that PATCH. The newer description is lost. Post-write verification only checks approval/projection state, so it can return success after the overwrite. Comparing the previously read metadata line does not protect the rest of the body. This directly contradicts plan 04's explicit fallback requirement (`docs/plans/tui-redesign/04-garden-foundations.md:52`). The adapter test at `internal/git/github/ghcli/garden/metadata_test.go:73` proves byte preservation only without an interleaving.

The label projection has a related stale-read risk: `approve.projectLabel` builds the entire desired managed-label set from an earlier Issue (`internal/core/garden/approve/service.go:280`); `UpdateLabels` then reconciles away a concurrently changed type/stage (`internal/git/github/ghcli/garden/adapter.go:746`). A conflicting remote stage can be silently corrected despite the stated attention-only policy.

**Suggested fix:** move canonical acceptance/projection completion to append-only records and derive acceptance without PATCHing an existing user body. Treat body metadata as an optional projection unless a documented atomic write mechanism is available. Make stage updates narrowly scoped and surface concurrent managed-label changes instead of restoring an old set. Another GET or a post-write check is not a loss-prevention fix.

**H2 — An in-flight refresh can retarget an already-open approval confirmation.**

**Locations:** `internal/ui/gardenplan.go:160`, `:326`, `:371`, `:549`.

Scenario: r1 is displayed with `shown == ""`; Refresh starts reading; before it finishes, the user presses `a` and selects Approve. The valid refresh returns r2. `onGardenPlanReview` replaces `p.view` while leaving `planDecide` active. The eventual Enter calls `p.current()` again and constructs the decision from r2's hash, source, actor and spec. Backend checks all pass for that replacement. The user can approve a revision they never reviewed, despite the original r1 confirmation. Changes-request/rejection prompts can similarly drift.

**Suggested fix:** capture an immutable decision target, actor, spec and stamp when entering the confirmation. Submit only that target. A refresh that changes its prerequisites must invalidate the confirmation and require a new review; it must not change the target. Existing late-reply tests cover rejected old generations, not a valid newer reply arriving during confirmation.

**H3 — A caller-supplied spec fingerprint lets a new specification inherit an old plan approval.**

**Locations:** `internal/core/garden/revision.go:423`, `:645`; `internal/core/garden/planrev/review.go:163`, `:164`, `:221`.

The verifier derives a spec fingerprint from the content hash only when the envelope field is empty. A spec envelope may otherwise supply any fingerprint, even one belonging to different content. Plan review checks staleness against that value and validates only the plan document's Fingerprint field, not its Specification Revision field.

Scenario: S1/P1 have both gates approved. Append valid, different spec S2 with parent S1 and explicitly set S2's `source_spec_fingerprint` to S1's fingerprint. Validate S2 at gate one. P1 now remains non-stale, its old decision still passes `approvedAt` against its historical S1, and `Ready` becomes true for the changed current spec without a new gate-two decision. Any commenter can supply the spec record; revalidation of S2 is still required, but it must not implicitly approve P1 for S2.

**Suggested fix:** derive spec fingerprints unconditionally from the normalized supported specification, rejecting a supplied mismatch. Validate the plan's source revision hash against the current approved spec as well as its fingerprint. Keep the exact source identity through historical approval checks. Do not permit an external field to redefine content identity.

**H4 — Revoking a gate-two approval is silently ignored.**

**Locations:** `internal/core/garden/revision.go:563`, `:570`; `internal/core/garden/planrev/review.go:178`.

After an approved plan decision, append an authenticated, correctly hashed `revocation` targeting that decision comment ID. `approvalByID` indexes only gate-one `approval` records. Because the target comment exists, the verifier neither marks a problem nor invalidates the plan decision. `Derive` continues to report Ready. This conflicts with the contract's claim that revoked records cannot yield Ready (`docs/design/tui-redesign/garden-contract.md:287`).

**Suggested fix:** support revocation of gate-two decisions in the versioned protocol, retain revoked decisions for audit, remove their authority, and preserve deletion/tamper detection for expected revocations. At minimum, unsupported revocation targets must be a visible blocking error, never silently ignored.

### Medium

**M1 — The inspected dirty context is neither content-bound nor preserved in the published plan hash.**

**Locations:** `internal/core/git/checkout.go:48`, `:54`; `internal/core/garden/plan/service.go:224`; `internal/core/garden/planrev/document.go:95`.

Editing the contents of an already-modified file leaves porcelain status unchanged. Adding a file under an already-untracked directory may also leave `--untracked-files=normal` unchanged. The accepted DirtyDigest and start check therefore miss those changes. Furthermore, publication includes only the dirty-file count, omitting even that status digest. Two attempts with the same commit/branch/spec/text and different dirty contexts of equal count produce the same published plan hash and are treated as a replay. This weakens the claimed inspected-base binding and the guide's “changed checkout asks again” guarantee.

**Suggested fix:** compute a bounded fingerprint of the actual accepted tracked/index/untracked context and include its identity in the owned Inspected base section. Check branch identity as well. Since the planner reads a live checkout, document ongoing drift or detect and invalidate it; a pre-start status check cannot promise a fixed snapshot throughout the session.

**M2 — A crash after session creation loses the planner/session association.**

**Locations:** `internal/core/garden/plan/service.go:250`, `:260`, `:271`, `:353`.

The attempt is persisted without a SessionID, then the holder is started, then SessionID is persisted. If Bonsai dies between the latter two operations, the holder survives. Reconcile later marks the attempt failed after two minutes without finding or stopping that holder. Another attempt is permitted while the first may still run; its terminal/partial output cannot be recovered through the attempt. `context.WithoutCancel` handles cancellation, not a process crash.

**Suggested fix:** persist a session/attempt correlation before the holder can run, or include an immutable attempt ID in holder metadata and reconcile by that ID. Recovery must recover or stop an ambiguous session before clearing the live-attempt reservation.

**M3 — Active local planning never reaches the Board/Table Planning state.**

**Locations:** `internal/core/garden/work/work.go:107`; `internal/gardenbackend/work.go:19`.

The placement function cannot return Planning, and Work never joins local planner attempts. Start a planner and return to the Board: the item remains Approved, or Plan Review if it already has a plan. This leaves the implementation short of the specified Approved → Planning → Plan Review workflow and hides the active run from its primary projection. The docs explicitly defer Planning even though this phase now supplies the necessary local evidence.

**Suggested fix:** join reconciled, clone-scoped live attempts into the read projection, with normal epoch/generation rules. Show Planning only from that local evidence, restore the canonical review state when it ends, and never derive it from a remote label.

**M4 — Plan integrity failures send already-accepted work back to Drafts.**

**Locations:** `internal/core/garden/work/work.go:74`, `:340`; `internal/core/garden/work/drafts.go:42`.

Take an accepted Issue whose current spec approval is valid, then edit its plan comment so its hash fails verification. Lane becomes Attention. `retained` only recognizes a *superseded spec approval*, so this accepted Issue is excluded from Work and included in Drafts. A malformed marker from an unrelated commenter has the same effect. Its plan review is then inaccessible through the normal Board route. Normal superseded-spec tests do not cover this case.

**Suggested fix:** distinguish historical verified acceptance from current downstream eligibility. Retain accepted work in Work with a blocking attention state for plan/log integrity problems; never grant actions from that retained status. Test both selectors together against the actual verifier output.

## Missing high-value tests

1. **Provider enforcement:** supported-version integration tests showing that shell writes, patch writes, auto-approved MCP mutations, hooks and project/profile overrides cannot mutate local or remote state. Fake binaries that echo argv do not prove this. Keep these tests isolated from real homes/accounts/network; use a controlled local fixture for integration tools.
2. **Exact confirmation:** delay a valid Refresh response, enter Approve on r1, deliver r2, then confirm. Assert no r2 decision is submitted. Repeat for spec/actor changes and changes-request/rejection prompts.
3. **Canonical identity:** post S2 with different content and S1's supplied fingerprint, revalidate S2, and assert P1 cannot produce Ready. Cover inconsistent Source specification revision/hash fields and duplicate-content comment IDs; decisions should retain the precise revision identity they verified.
4. **Revocation:** revoke a gate-two approval; then edit/delete the revocation. Assert no Ready and retained audit history. The existing revocation test exercises gate one only (`internal/core/garden/approve/approve_test.go:518`).
5. **Remote interleavings:** edit the body between metadata GET/PATCH and change managed type/stage between approval verification and projection. Assert user text is never lost and conflicting state is not silently restored. Include two independent service instances, not only one service mutex.
6. **Crash/ambiguous start:** leave a live holder after runtime start but before SessionID persistence, restart the service, reconcile and retry. Assert the holder is recovered/stopped and there is no unaccounted second run. Include cancellation/start completion and persistence/stop failures.
7. **Dirty-context identity:** modify contents without changing status, add nested untracked files, switch branches at the same commit, and publish equal-count different dirty bases. Assert consent/binding/hash behavior explicitly.
8. **Cross-phase projection:** active attempt → Planning → end/failure/cancel; accepted Issue + corrupted/unauthorized plan record remains Work with attention and no downstream actions. Test real core/backend projections rather than scripting a successful fake review.

Existing tests are not merely happy-path tests: stale heads, unauthorized actors, wrong schema, label/prose claims, partial projection, explicit dirty consent, terminal key capture and monotonic observations have useful assertions. The gaps concentrate at transitions between those independently tested units.

The UI fake also duplicates workflow rules: `internal/ui/gardenapi/fake/fake.go:583` synthesizes acceptance from the supplied preview instead of re-verifying current heads, and `internal/ui/gardenapi/fake/planreview.go:50` inserts already-verified decisions directly. These are useful UI fixtures, but successful UI tests against them do not establish production protocol integrity. Keep security assertions in tests that traverse the real verifier and services.

## Documentation inaccuracies and limits

- `docs/garden.md:216` promises every non-metadata byte is kept, and `:406` promises bodies are never overwritten. H1 invalidates the concurrency guarantee. The adapter comment acknowledges the risk; the user guide does not.
- `docs/garden.md:128` still says specification/plan approval is unavailable until Plan 08, and `:130` says specifications are approved only from Drafts. Plan review now offers `v` revalidation. `internal/ui/gardenboard_view.go:304` also still defers question authoring to Plan 08, which has no separate question-authoring feature.
- `docs/agents.md:329` claims Codex sandboxes everything the model runs, while `:334` concedes only shell scope was established. Capability availability must match evidence (B1). Claude's flags are real: the official [CLI reference](https://code.claude.com/docs/en/cli-reference) documents `--restricted` from v2.1.248 and the tool/MCP controls. The conservative Antigravity refusal is appropriate without stronger enforcement evidence.
- `docs/garden.md:271` says a changed checkout asks again. The digest covers status entries, not changed contents (M1). The canonical contract accurately calls it a status-entry digest, but that is a narrower guarantee.
- `docs/garden.md:405` says approval cannot silently retarget, and `docs/design/tui-redesign/garden-contract.md:263` describes request bindings as confirmation bindings. The current UI rebuilds them at submit time (H2).
- `docs/design/tui-redesign/garden-contract.md:287` promises revoked records never yield Ready, but plan-decision revocations are ignored (H4). The protocol should explicitly document both gates' revocation behavior.
- The guide's blanket “editing a published comment makes it unverifiable” is too strong. Hashes detect inconsistent edits; an authorized writer can rewrite an envelope and recompute unkeyed hashes. The contract already acknowledges that trust limitation. Keep the user guide equally precise; no cryptographic immutability or edit-history verification was demonstrated.
- `docs/plans/tui-redesign/08-garden-approval-and-planning.md:3` still says “not started” and `:102` retains a pending completion record. Update those only after recording the actual fixes/provider evidence/check results.
- The documented `p/r/u/a/v/o/y`, `ctrl+s`, `ctrl+]`, comparison keys, config path helper, package paths and external approval envelope exist. The fixed `Approved.` record/hash example is exercised by `internal/core/garden/planrev/gate_two_test.go:218`. No new broken command/path was identified in the changed user-guide links.

## Things done well

- Domain readers do not own launch capabilities; gate two appends a decision only. No implementation worktree/PR path was added.
- GitHub author IDs, strict schema parsing, bounded logs, normalized content hashes, exact comment references at verification and post-append rereads provide a useful integrity foundation. They correctly reject many prose/label/unauthorized claims.
- Unsupported providers fail closed; per-account configuration, environment scrubbing and unchanged HOME behavior are reused. New planner attempt records exclude prompts, terminal output, environment values and credentials; writes use the locked atomic store pattern.
- The UI reuses the existing terminal controller, prioritizes focused terminal input, retains explicit cancellation/retry history, and keeps backend I/O out of Update/View. Cross-OS compilation and the unexcluded terminal race checks passed.
