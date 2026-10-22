# 11 / Implementation preview and supervision

Status: planned.

## Deliverable

Launch from Ready with a clear implementation/review sequence and supervise the
actual holder from the ticket overlay, preserving existing launch safeguards.

## Read only

Source: `internal/ui/gardenlaunch.go`, `internal/ui/gardenlaunch_view.go`,
`internal/ui/gardenapi/launch.go`, `internal/core/garden/launch/`,
`internal/gardenbackend/launch.go`, `internal/ui/agentterm.go`.
Images: [Ready](../design/png/ready-120x35.png),
[Running](../design/png/running-120x35.png),
[compact](../design/png/running-80x24.png).

Entry: launch already verifies an exact approved spec/plan and creates one isolated
holder-backed attempt. Optional review policies can be validated and snapshotted.

## Specification

- Ready shows effective implementer/account/effort, reviewer policy, worktree/base
  and native permissions. Start implementation opens the existing exact preview;
  dangerous provider modes retain their separate unchecked acknowledgment.
- If automatic review is enabled, confirmation explicitly authorizes implementation
  followed by one review/fix/check sequence and a stop at human review. Freeze the
  exact policy on the attempt. No reviewer capability means review is unavailable,
  not promised. A policy edit cannot mutate an active attempt.
- Preserve preflight revalidation, dirty/base/ownership handling, operation locks,
  holder deduplication and safe retry. Unknown start outcomes reconcile; approval,
  navigation, a remote comment or reading a saved policy cannot launch work.
- Display real implementation state, latest observed activity/time, files/checks
  only when evidence exists and the next authorized step. Surface stale remote
  status separately from a locally running job. Idle is not process completion.
- Open session is observation first; input control is explicit with `Ctrl+]` escape.
  Stop requires confirmation and waits for proven holder closure. Leaving the
  overlay or quitting the TUI detaches; it never cancels the attempt or deletes work.

## Checks and exit

Run launch/backend/UI tests and affected race checks. Cover frozen review consent,
escalation acknowledgment, duplicates, unknown starts, account/base changes, late
messages and detach/stop. Demonstrate an actual fake-holder run through the TUI.
Stop at launch intent and supervision; no reviewer executor, push/PR/merge or
automatic cleanup behavior is added here.
