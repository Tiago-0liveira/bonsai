# 10 / Explicit review policy and durable review records

Status: Done.

## Deliverable

Define and configure the bounded review contract without starting reviewers or
changing an implementation's runtime lifecycle.

## Read only

Source: `internal/core/config/state.go`, `internal/core/agents/launch_values.go`,
`internal/core/garden/launch/types.go`, `internal/core/garden/launch/store.go`,
`internal/ui/settings.go`, `internal/ui/gardenapi/launch.go`,
`internal/providers/claude/`, `internal/providers/codex/` (capabilities only).
Images: [run options](../design/png/ready-120x35.png),
[bounded review](../design/png/ai-review-120x35.png).

Entry: Garden can resolve available accounts and provider-native launch options;
durable attempt stores already isolate operation identities and locks.

## Specification

- Add an optional review policy with enabled/account/model/effort, permitted native
  options, one review/fix pass and explicit verification choices. Default disabled;
  existing records/configuration preserve manual human-review behavior.
- Expose Garden-scoped effective defaults in Settings with per-run overrides.
  Separate the implementer from reviewer. Unsupported/missing accounts or permission
  modes are unavailable with reasons; never silently fall back to full access.
- Define immutable run-bound policy snapshots: approved spec/plan IDs, repository,
  owned worktree, implementation Run ID, reviewed HEAD and later final HEAD. Persist
  operation/session identities, states, timestamps and bounded sanitized evidence
  references. No credentials, commands, terminal output or prompts in UI caches.
- Model pending, starting, start-unknown, running, checking, awaiting-human-review,
  failed, stopped and blocked explicitly. A policy's existence grants no permission
  to start: authorization must be recorded with an explicit local run confirmation.
- Define verification as reviewed typed checks (executable/argv/scope), with safe
  display names and no generic shell endpoint. Keep task-specific output private;
  completion evidence distinguishes passed, failed, skipped and unknown checks.
- Preserve older stored attempts and unknown newer schemas safely. Document any
  schema/downgrade limits; do not rewrite unrelated account preferences.

## Checks and exit

Test config inheritance, disabled defaults, immutable snapshots, bounded decoding,
old records and cross-process locks in config/agents/Garden packages with race
checks where needed. Stop at validated policy, UI defaults and storage interfaces;
no reviewer process, automatic transition or fake completion report.

## Implementation plan

### Source capability found (verified in code)

- `config.GardenReviewPrefs` (`internal/core/config/garden_review.go`) and
  `internal/core/garden/review` are the Playground's read-only reviewer reports
  (trigger manual/awaiting-review/new-commit, up to 10 runs, no fixes, no checks).
  They stay unchanged: an existing `garden_review` setting never enables the bounded
  review/fix/checks sequence.
- `launch.validateNative`/`escalated` (`internal/core/garden/launch/service.go`) hold
  the native-permission rules (decided, provider-owned, dangerous modes). Account
  resolution is `launch.Accounts`/`agentsapi.LaunchResolver` (`ResolveLaunch`).
- `launch.FileStore` is the pattern for a locked, atomically replaced, bounded JSON
  store with schema version, duplicate-key rejection and immutable identity.
- Settings: `gardenSections()`/`gardenAssistSection()` (`internal/ui/settings_registry.go`).

### Step 1: core (builder)

- `agents`: export `NativePermissions(provider, LaunchOptions) error` (sentinels
  `ErrNativeUnresolved`, `ErrNativeUnsupported`) and `EscalatedLaunch(provider,
  LaunchOptions) bool`, moved from `launch`; `launch` wraps them with its existing errors.
- `config.GardenAutoReviewPrefs` (`prefs.garden_auto_review`, `version` 1): enabled,
  account, model, effort, native options, typed `Checks []GardenCheck{Name,
  Executable, Args, Dir}`. Nil = off. Invalid or newer-version values load as off and
  their raw JSON is preserved on rewrite; saving over a newer value is refused.
  Checks: at most 8, bounded argv, no shells/`env`/`sudo` as executable, relative
  clean `Dir`, safe display names. `ParseGardenChecks`/`FormatGardenChecks` for the
  Settings text form (`;` between checks, whitespace between words, no quoting).
- New `internal/core/garden/reviewpass`: `Resolve(prefs, *Override, Accounts)`
  (built-in → personal → run override, provider profile for inherited values) with
  `Available` + fixed `Reason`; an inherited dangerous mode is unavailable, an explicit
  one sets `Escalated` (separate acknowledgment). `Policy` (reviewer, checks, one
  pass), `Snapshot` (repo, Issue, approved spec/plan IDs, attempt, owned worktree,
  policy hash), `Authorization` (actor, confirmation time, preview ID, escalation ack),
  `Pass` (states pending … blocked, fixed codes, write-once implementation run /
  reviewed HEAD / final HEAD, reviewer operation/session identity, check results
  passed/failed/skipped/unknown with evidence references), and a `FileStore`
  (`config.GardenReviewPassPath`) with cross-process locks, one active pass per attempt.

### Step 2: Settings (builder)

Garden → "Automatic review" section: enabled toggle, reviewer (account/model/effort/
native via the existing launch form in a defaults mode), fixed "One review + fixes +
checks", checks text, implementer note (chosen per start), live availability with
reason from `reviewpass.Resolve`, restore defaults. No account profile is written.

### Step 3: docs

`docs/garden.md`, `docs/tui-cli.md`, README Garden line if needed; status Done.

### Out of scope

Reviewer process, check runner, freezing the policy on launch attempts, Ready overlay
display, per-run override UI, automatic transitions, completion reports.

## Completion

The policy is a new `prefs.garden_auto_review` (`config.GardenAutoReviewPrefs`, version 1),
separate from the Playground's `garden_review`: off by default, reviewer account/model/effort
and native permissions, typed checks (`GardenCheck`: executable, argv, relative `dir`, safe
name; at most 8; shells, wrappers, script extensions, shell metacharacters, drive or parent
paths refused). It loads only from an exact, canonical, valid key; any other spelling,
invalid or newer value loads as off, is preserved on rewrite and cannot be overwritten
(`ErrGardenAutoReviewNewer`). `agents.NativePermissions`/`EscalatedLaunch` now hold the
native-permission rules launch already used (launch errors unchanged).

`internal/core/garden/reviewpass` defines the bounded contract without executing it:
`Resolve` layers built-in → personal → per-run `Override` → account profile and returns
availability with fixed reasons (inherited full access is unavailable; an explicit
escalation is marked for separate acknowledgment); `Policy` (one pass, hashed), `Snapshot`
(repository, Issue, approved spec/plan IDs, attempt, owned worktree), `Authorization`
(required to create a `Pass`), states pending … blocked with a transition graph tied to
the reviewer session (a pass is final only after its reviewer is closed), write-once
implementation run / reviewed HEAD / final HEAD, check results passed/failed/skipped/unknown
with evidence references, and a canonical-JSON `FileStore` (`garden-review-passes.json`)
with cross-process locks and one active pass per attempt. Settings → Garden → Automatic
review edits the defaults using cached launch options only and shows live availability.

Files: `internal/core/agents/{errors,launch_values}.go`, `launch_values_native_test.go`;
`internal/core/config/{state,errors,project_roots,garden_auto_review}.go` + test;
`internal/core/garden/launch/service.go`; `internal/core/garden/reviewpass/`;
`internal/ui/{settings_autoreview,settings_registry,settings,settings_review,shell}.go`,
`settings_autoreview_test.go`, `components/modals/modals.go`; `README.md`, `docs/garden.md`,
`docs/tui-cli.md`.

Checks: `gocheck.sh --full` (gofmt, diff --check, vet, build, Windows build, `go test ./...`)
and `-race` on `internal/ui`, `internal/core/config`, `internal/core/garden/...`,
`internal/core/agents`. Four Codex review rounds; findings fixed with regression tests.

Limitations: no reviewer process, check runner, launch-time freeze, Ready display or per-run
override UI yet. The checks text form cannot express a folder, custom name or spaced argument
(such records are refused for text editing). Exact duplicate JSON keys keep only their last
occurrence when preserved. Restore defaults cannot clear a preserved newer policy.
