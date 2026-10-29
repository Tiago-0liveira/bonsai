# 11 / Implementation preview and supervision

Status: Done.

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

## Implementation plan

### Source capability found (verified in code)

- Launch (`launch.Service`): exact preview re-verified at `Start` (`preflight`), per-attempt
  cross-process lock before `Reserve`, one active attempt per Issue, idempotent replay by
  preview ID + `requestHash`, holder identity written before `Holder.Start`, ambiguous
  start reconciled to `start-unknown`, retry only after proven closure, idle never a
  lifecycle change (`applyObservation`). `Stop` records the request and reports stopped
  only once closure is observed; later closure arrives through `Reconcile`.
- TUI: Ready overlay offers `s` (form, then preview, then confirm with Cancel focused and a
  separate unchecked `x` escalation acknowledgment) and `i` (attempt hub). Open session is
  a read-only observer; Enter takes control; Ctrl+] only unfocuses (writer role kept).
  Leaving the overlay or quitting detaches and never stops (`launchDetach`).
- `reviewpass` (`Resolve`, `Policy.Hash`, `NewPass`, `FileStore.Reserve`) and
  `prefs.garden_auto_review` exist from phase 10 with no launch caller.
- The board's Running column comes from the remote stage label; there is no warning when
  GitHub says Running and nothing runs locally.

### Step 1: freeze review consent at start (builder)

- `launch.Request.ReviewPolicyHash` (empty = no automatic review) joins `requestHash`
  with `omitempty`, so existing attempts replay unchanged and no launch store schema
  change is needed. A different policy needs a new preview.
- `gardenbackend.PreviewLaunch` resolves the saved policy (`reviewpass.Resolve`) and
  returns `gardenapi.LaunchReview` (enabled, available, fixed reason, frozen policy and
  hash, reviewer escalation). Enabled but unavailable: no hash, review not promised.
- `StartLaunch` (new confirmation only, not replay) re-resolves: any hash difference is
  `launch-changed` before anything runs; an escalated reviewer needs its own
  `ReviewEscalationConfirmed`. It reserves the `reviewpass.Pass` (ID derived from the
  attempt ID, state pending, `Authorization{viewer, preview ID, confirmation time,
  reviewer escalation ack}`) before `Service.Start`, so a crash never leaves an attempt
  whose authorized review is lost. A pass is only valid for the attempt whose ID it
  snapshots; a re-submit reuses it. Later Settings edits never touch a reserved pass.
- `LaunchAttempts` also returns the frozen passes by attempt ID and the latest session
  observation (state, observed time), reusing the reconcile query, never stored.

### Step 2: Ready, confirmation and supervision UI (builder)

- Ready overview: "READY TO BUILD" with implementer provider/account · effort, AI reviewer
  (or off / unavailable with reason, both "stops at your review"), review limit, worktree
  branch/base, native permissions, from cached data only (no remote reads, no start).
  "Start implementation…" goes straight to the exact preview with those defaults;
  "Change run settings" opens the existing form.
- Confirmation shows the authorized sequence: implementation, then one review/fix/check
  pass and a stop at human review; a reviewer escalation has its own unchecked
  acknowledgment beside the implementer one.
- Running view: state heading, provider/account · effort · elapsed (only from an observed
  start), observed activity with its age (idle says the process is still running),
  checks only from pass evidence, "Next:" from the frozen pass, GitHub stage as a
  separate line. Board: GitHub Running with no local job says so.
- Ctrl+] in a launch session returns to observation and releases input control. Stop:
  confirmation, then "Stopping…" and bounded observation polls until closure is proven;
  leaving or quitting still only detaches.

### Out of scope

Reviewer process, check runner, per-run reviewer override UI, push/PR/merge changes,
automatic cleanup.

## Completion

The review policy is frozen at confirmation without a launch store schema change:
`launch.Request.ReviewPolicyHash` joins `requestHash` with `omitempty` (old attempts
replay unchanged). `gardenbackend.PreviewLaunch` resolves the saved policy into
`gardenapi.LaunchReview`; `StartLaunch` re-resolves on a new confirmation (any
difference, including newly enabled or unavailable, is `launch-changed`), requires
`ReviewEscalationConfirmed` for an escalated reviewer (`launch-blocked`), and reserves the
`reviewpass.Pass` (ID derived from the attempt ID, pending, with the authorization) before
`Service.Start`. Re-submits and concurrent duplicates reuse the matching pass. A pass is only
meaningful for the attempt whose ID it snapshots. `LaunchAttempts` returns frozen passes
(`ReviewsUnknown` on a store read failure) and transient observations
(`launch.Service.ReconcileObserved`; `SessionObservation.Activity` mapped to
working/idle/waiting-permission/unknown, never persisted, never used for closure).

TUI: Ready shows implementer (cached, labeled saved default), AI reviewer from the saved
policy (availability checked in the preview), review limit, worktree/base and
permissions, with no preview, resolution or start. `s` goes straight to the exact
preview, `e` opens the run settings form. Confirmation lists the authorized sequence with
separate unchecked `x` (implementer) and `r` (reviewer) acknowledgments. The overview
supervises the active attempt (historical attempts never hide Ready): state heading,
elapsed from the observed start, activity with age (idle is never completion; stale or
terminal activity makes no running claim), checks only from pass evidence, Next from
lifecycle plus frozen pass, the last synced GitHub stage apart from the mirror target. It
refreshes on agent changes and a guarded 5 s tick while visible. Stop confirms, then
polls (1 s, at most 60) until the newest run's closure is proven; Ctrl+] in a launch
session reattaches read-only. Leaving or quitting stops only the polling. The board says
"GitHub says Running · no local job" only after a fresh successful local read.

Files: `internal/core/garden/launch/{types,service,reconcile}.go` + tests;
`internal/gardenbackend/{backend,launch,launch_git,launch_review}.go` + `launch_test.go`;
`internal/ui/gardenapi/launch.go`; `internal/ui/{agentterm,garden,gardenboard,gardenboard_view,
gardenlaunch,gardenlaunch_supervision,gardenlaunch_view,gardenoverlay,gardensetup,
gardentable_view,playground_changes_view,procmodal}.go` + tests, including
`gardenlaunch_demo_test.go` (`TestGardenLaunchFakeHolderEndToEnd`: real `launch.Service`
with a fake holder driven through the TUI with review enabled, logging 120x35/80x24
screens); `docs/garden.md`, `docs/tui-cli.md`.

Checks: `gocheck.sh --full --race` on `internal/ui`, `internal/core/garden`,
`internal/gardenbackend` (gofmt, diff --check, vet, build, Windows build, `go test ./...`).
Three Codex review rounds and a final Sonnet review; findings fixed with regression tests.

Limitations: no reviewer process or check runner, so checks never show yet and a frozen
pass stays pending. The Ready implementer shows saved defaults; provider normalization
happens in the preview. Activity is observed for the newest run only.
