# Plan 08 — Specifications, approval gates and local planning

Status: not started. Prerequisite: 07 — Garden drafts.
Next: 09 — Local implementation launch.

## Deliverable

Complete the idea/specification → Approved → local read-only Planning → Plan
Review → Ready workflow. This plan ends at **Ready**. It must not start an
implementation agent, create an implementation worktree or open a PR.

References: [validation](../../design/tui-redesign/revision-02/png/g-07-120x35-142.png),
[planning](../../design/tui-redesign/revision-02/png/g-08-120x35-148.png),
[plan review](../../design/tui-redesign/revision-02/png/g-05-120x35-148.png),
[stale approval](../../design/tui-redesign/revision-02/png/e-01-120x35-164.png).

## 08.1 — Gate one: validate the specification

- Define blocking requirements/open questions and a preview of the exact
  specification revision, fingerprint and resulting Draft → Approved transition.
- Record authenticated actor, canonical revision reference, timestamp and explicit
  approval using plan 04's versioned Issue protocol. Retain superseded/revoked
  records and verify their integrity when reading them.
- The same Issue leaves the Drafts inbox and appears in approved-work Board/Table.
  Type remains unchanged. Stage Approved is visibly different from Planning.
- Validation launches nothing. Remote approval is recognized only when its
  authenticated author/permissions, schema, revision and spec match are verified.
  A prose claim in an Issue body is not proof of approval.
- Conflicting stage labels or unverified approval produce an attention state and
  unavailable downstream actions, not automatic remote corrections.

## 08.2 — Request a planner locally

- Only an explicit local Request AI plan action starts a planning attempt.
  Reuse Accounts/profile/model/effort forms, with a genuine read-only capability.
- Bind the attempt to the approved spec and inspected repository base. Record
  provider/account, role, accepted permissions and attempt ID. No implementation
  worktree is created; reading dirty local context must be explicit and captured.
- Verify each provider's enforced read-only behavior. A prompt saying “do not
  edit” or a mode named “plan” is not sufficient enforcement. A provider that
  cannot meet the contract is unavailable for this role; never fall back to
  unrestricted access. Any necessary isolation belongs in core/runtime adapters.
  The only write a planner has is its own `plan.md` in a Bonsai-owned directory
  under `~/.bonsai/garden/plans/`, confined by the provider below the model.
- Reuse existing holder-backed sessions/terminal contracts for interaction,
  progress, input questions and cancellation. Until Playground lands, provide a
  focused planner-session body using that same terminal controller, not another
  PTY client or a dependency on the future Worktrees UI.
- Preserve cancellation/failure/partial-output history. Retry is another explicit
  local attempt; displaying an externally edited issue never reruns a planner.

## 08.3 — Publish and review plan revisions

- Store a content-addressed plan revision on the canonical Issue, with source spec
  fingerprint, inspected code base, assumptions, touched areas, steps, tests,
  risks and acceptance mapping. A later edit cannot mutate an approved snapshot
  without being detected as a different/unverifiable revision.
- Render long plans as documents with section navigation and focused search.
  Unified revision comparison is the default; optional wide comparison cannot
  make 80-column review unusable.
- Request changes records a reason and keeps revision history. Starting the next
  read-only run still requires local confirmation, including when a remote comment
  requested changes. Reject returns to Approved with no accepted plan.
- Display current, latest and approved revisions separately. Do not approve
  “whatever is latest when the request completes.”

## 08.4 — Gate two: approve the exact plan

- Confirmation names the plan revision, current source spec fingerprint and actor.
  Reverify the current heads and approval authority before recording the decision.
- Ready requires an exact verified plan/spec pair. Approving does not start coding.
- A new source specification or plan revision after approval makes the mismatch
  visible and disables affected downstream actions. Preserve the old approved
  snapshot; do not silently reuse approval for new content.
- External GitHub/mobile approvals show who, which revision, source and verified
  time. Provide copy/open Issue links and document the supported external protocol;
  do not promise remote execution or a phone client.
- If the spec changed after gate one, validate the current spec before accepting a
  plan for it. Keep accepted work visible with the stale gate rather than silently
  recategorizing it as a new draft.

## Source seams

Garden domain/revision services from 04/07; `internal/ui/agentlaunch.go`,
`internal/ui/agentsapi/agentsapi.go`, `internal/ui/agentattach.go`,
`internal/ui/agentterm.go`, `internal/agentsbackend/`,
`internal/core/agents/provider.go` and `internal/core/agents/session_service.go`.
Provider implementations stay behind runtime wiring and capability interfaces.

## Exit criteria and verification

- [ ] The four distinct actions—validate, request plan, approve plan, launch—cannot
      be confused; launch remains unavailable in this plan.
- [ ] Gate one alone yields Approved with no planner running.
- [ ] Planning enforces read-only execution and creates no implementation worktree.
- [ ] Questions, cancellation, failure and retries preserve attempt/revision history.
- [ ] Gate two yields Ready with no coding agent running.
- [ ] Stale/missing/tampered/unauthorized approval and remote-label-only changes
      cannot bypass either gate or trigger a local run.
- [ ] Test state transitions, actor/revision integrity, late events, read-only
      enforcement and safe terminal focus with fakes; race/full Go checks pass.
- [ ] Update `README.md`, the Garden guide, `docs/agents.md` and `docs/tui-cli.md`.

Completion record: pending implementation, provider capability evidence and checks.
