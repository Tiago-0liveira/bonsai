# Plan 10 — Playground session cockpit

Status: not started. Prerequisite: 09 — Local launch.
Next: 11 — Worktrees.

## Deliverable

The redesigned Playground aggregates current-repository sessions, including manual
and unlinked sessions, and supplies terminal, changes, PR/CI, history and review.
These subphases are sequential; preserve the existing terminal implementation.

References: [sessions](../../design/tui-redesign/revision-02/png/p-01-120x35-151.png),
[compact sessions](../../design/tui-redesign/revision-02/png/p-01-80x24-151.png),
[Observe](../../design/tui-redesign/revision-02/png/p-02-120x35-151.png),
[Control](../../design/tui-redesign/revision-02/png/p-03-120x35-151.png),
[review](../../design/tui-redesign/revision-02/png/p-05-120x35-155.png),
[changes](../../design/tui-redesign/revision-02/png/p-06-120x35-151.png).

## 10.1 — Repository-wide sessions and attention

- Replace worktree-first presentation with a flat selectable session list scoped
  to all verified worktrees/associated local records of this local repository.
  The existing backend lists holder records broadly; do not accidentally display
  another repository's sessions or match only on branch names. Other clones can
  share a Garden while still having distinct local ownership roots.
- Include manual/unlinked sessions, active and retained ended sessions. Optional
  task grouping nests the same session records and makes multiple agents/attempts
  on one Issue explicit; it does not collapse their states into one process.
- Preserve provider activity semantics from the baseline: lifecycle, Working,
  Waiting reason, Idle, inferred/stalled and unknown are separate facts. Do not
  equate silence with failure or a completed turn with task completion.
- Implement Active, Needs attention, History/failed filters and task/provider/
  account/branch search. Keep stable selection during incoming activity and expose
  attention without automatically reordering the row under the cursor.
- Reuse Accounts usage snapshots and label capacity separately from task progress.
  Add cross-links to the canonical Issue, approved plan, account and local attempt.

## 10.2 — Terminal and control ownership

- Rehome `agentView` and its existing attach/replay/input/resize handling in a
  workspace-neutral controller; replace right-pane assumptions at its outer
  layout/routing boundary rather than duplicate the client.
- Selection attaches in Observe. Enter requests control. If another client is the
  writer, add the approved takeover confirmation before calling the existing Take
  operation. Selection/navigation/usage refresh never steals writer ownership.
- In Control, every key except `Ctrl+]` goes to the agent, including `q`, Escape,
  Tab, `Ctrl+C` and `Ctrl+K`. The footer/header names the input owner. Only actual
  writer attachments send input, terminal replies and writer-driven resize.
- Leaving Control changes keyboard routing but preserves output/session. Switching
  sessions always observes the next one; do not carry writer capture by accident.
- Preserve offset/generation replay guards, bounded input queues, late-token
  rejection, debounced PTY resizing, control-loss demotion and reconnect behavior.
- Fit all three terminal sizes. Observe must not resize another client's PTY.
  Ended sessions accept no input; retained output is bounded and not advertised
  as a complete permanent transcript. Unsupported PTY/version mismatch is clear.

## 10.3 — Changes, branches, PRs and CI

- Reuse existing Git/PR/check services and task-attempt links. Display changed
  files, bounded per-file diffs, chosen base and commit/context accurately.
- Each expensive read has scope/generation and its own freshness/error. A missing
  branch/worktree or failed PR lookup is different from verified absence.
- Keep large diff navigation file/hunk-based. At 80 columns detail replaces list;
  at wider sizes use list/detail with readable lines.
- Linking an unlinked session to a task must be explicit and ownership-checked;
  absence of a Garden link is not a failed session. Do not invent an approved plan
  for manually launched work.

## 10.4 — Read-only verification and human review

- Implement optional local review policy in Settings → Agents only now. Default
  off; enabling names account, enforced read-only role, trigger and scope. The
  local consent authorizes bounded automatic reviewer runs; a remote Issue edit
  cannot enable them. Reconcile triggers idempotently across restarts.
- Review findings bind report revision and commit, with severity, file evidence,
  recommendation, dismissal reason and prior reports. CI retains independent
  pending/passed/failed/unavailable truth and freshness.
- Request fixes opens the existing locally confirmed implementation attempt form;
  never rerun code solely because a remote comment requested changes.
- Dismiss records a reason. Retry review is an explicit local read-only run.
  Accept changes previews unresolved findings/CI according to policy and records
  the human decision; it neither merges a PR nor closes/deletes a worktree.
- New commits make old reports/acceptance provenance visible instead of silently
  claiming the new code was reviewed. Preserve previous attempts and findings.

## Source seams

`internal/ui/agents.go`, `internal/ui/agentstab.go`,
`internal/ui/agentsessions.go`, `internal/ui/agentattach.go`,
`internal/ui/agentterm.go`, `internal/ui/components/vterm/`,
`internal/ui/agentsapi/`, `internal/agentsbackend/`,
`internal/core/agenthost/`, `internal/core/agentactivity/`,
existing Git/PR/check/diff bodies and Garden attempt/review contracts.

## Exit criteria and verification

- [ ] All relevant local sessions appear once; unrelated repositories stay out.
- [ ] Manual/unlinked, multiple attempts, Waiting, Idle, unknown and ended sessions
      remain understandable without false task-completion claims.
- [ ] Enter/Observe/Control/`Ctrl+]`, takeover/cancel, ownership loss, replay,
      reconnect and resize tests preserve the existing safety behavior.
- [ ] Usage never starts independent account refresh storms or exposes secrets.
- [ ] Branch/PR/CI/diff failures preserve independent last-good information.
- [ ] Review triggers are locally authorized/idempotent and reviewer writes are
      prevented; acceptance, merge and cleanup are distinct.
- [ ] Focus, terminal integration, concurrency/race and full Go checks pass.
- [ ] Update `README.md`, `docs/tui-cli.md`, `docs/agents.md` and the Garden guide.

Completion record: pending implementation, terminal regression and review evidence.
