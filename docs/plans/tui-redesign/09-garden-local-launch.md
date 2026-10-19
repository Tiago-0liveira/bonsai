# Plan 09 — Local implementation launch and execution links

Status: not started. Prerequisite: [08 — Approval and planning](08-garden-approval-and-planning.md).
Next: [10 — Playground](10-playground.md).

## Deliverable

A Ready item can explicitly start an isolated local implementation attempt with
the exact approved plan. Worktree creation uses existing Git/core services even
though the redesigned Worktrees workspace is intentionally not built until 11.

References: [launch](../../design/tui-redesign/revision-02/png/g-06-120x35-161.png),
[confirmation](../../design/tui-redesign/revision-02/png/c-01-120x35-launch.png),
[approval blocker](../../design/tui-redesign/revision-02/png/e-01-120x35-164.png).

## 09.1 — The launch contract and preflight

- Add a core orchestration boundary, independent of workspace layout, for issue,
  approved plan/spec, repository/base, branch/worktree proposal, account, model,
  effort and native provider permissions. Do not create a generic shell endpoint.
- Resolve and show effective account defaults from plan 03. Codex sandbox/approval,
  Claude permission modes and Antigravity full access remain distinct fields.
  Unsupported or unresolved permissions block confirmation. Escalation is explicit.
- At confirmation reverify remote spec/plan/approval, local base assumptions,
  account availability, ownership, directory/branch conflicts and existing active
  attempts. A stale remote snapshot, access error or changed spec blocks launch.
- Record a durable local operation/attempt identity before effects. Stable IDs
  connect Issue, approved snapshot, local repo, worktree, holder and later PR.
  Local records track execution; they are not another canonical task store.

## 09.2 — Create isolation and start the existing holder

- Serialize conflicting task/branch/account operations with existing lock patterns.
  Double-submit and retries reconcile the same attempt instead of starting agents
  twice. Explicit parallel attempts, if supported, need distinct branch/worktree
  proposals and a clear local confirmation; never infer consent from two clicks.
- Create the isolated branch/worktree through existing core/daemon Git authority;
  respect root templates, path safety and hooks. Do not use UI `os/exec`.
- Start through the existing account/session/holder runtime. Preserve isolated
  provider config dirs, environment scrubbing, accepted launch options and origin.
- Only a real start acknowledgement makes the local attempt Running. Mirror the
  appropriate canonical stage through the Garden service; a sync failure retains
  the running local truth and marks remote status pending, not launch failure.
- TUI exit/navigation detaches rather than stopping the holder. In this transition
  phase, a selected-attempt detail can open the reused session terminal; 10 turns
  that route into the full Playground cockpit.

## 09.3 — Partial failure, completion and PR lifecycle

- Model preflight, branch/worktree creation, holder start, remote update and push/
  PR linkage as recoverable steps. A timeout after starting a holder first queries
  its operation/session identity; it does not retry blindly.
- If worktree creation succeeds and starting fails, show the retained worktree
  and safe retry/cleanup choices. Cleanup requires its own preview/confirmation;
  never erase user changes as compensation. Late results from a closed launch
  form still reconcile and report the actual operation.
- Observe real session lifecycle through the shared backend. Idle or a completed
  turn is not an exited implementation. Failed/stopped attempts retain audit and
  links; success advances to human review or configured verification, not Done.
- Open/link a Draft PR only after meaningful changes are pushed and a verified
  head exists. Use the existing GitHub PR service, discover an existing matching
  PR on retries and distinguish not-created, pending, unknown and unavailable.
- Automatic reviewer launch remains unavailable until plan 10's local policy and
  read-only review flow exist. Keep completed attempts awaiting human review
  honestly; never fabricate a passed report or auto-merge/delete resources.

## Source seams

`internal/git/local/service.go`, `internal/core/git/`, `internal/daemon/`,
`internal/git/github/service.go`, `internal/core/agenthost/manager.go`,
`internal/core/agenthost/record.go`, `internal/agentsbackend/sessions.go`,
`internal/ui/agentlaunch.go`, `internal/core/agents/session_service.go` and the
Garden services established above. Evolve explicit task/attempt metadata without
forking the agent session database or breaking existing web/CLI consumers.

## Exit criteria and verification

- [ ] Launch always displays exact approved inputs and native permission choices.
- [ ] Remote labels/comments alone never start work.
- [ ] Stale approval, ambiguous ownership and branch/path collisions fail preflight.
- [ ] Double submit, start timeout, process crash and each partial step are
      recoverable without duplicate agents/PRs or unconfirmed destructive cleanup.
- [ ] Running truth follows the local holder; a GitHub failure only marks sync stale.
- [ ] Draft PR is absent/pending until real meaningful pushed changes exist.
- [ ] Completion/failure retains attempt history and never implies automatic merge.
- [ ] Orchestration, race, adapter, terminal and full Go checks pass using fakes and
      temporary repositories. No test starts a real provider or posts a real PR.
- [ ] Update `README.md`, the Garden guide and `docs/agents.md`/`docs/tui-cli.md`.

Completion record: pending implementation and failure-recovery evidence.
