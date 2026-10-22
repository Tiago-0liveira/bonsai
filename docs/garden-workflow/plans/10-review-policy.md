# 10 / Explicit review policy and durable review records

Status: planned.

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
