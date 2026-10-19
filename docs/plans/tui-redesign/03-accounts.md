# Plan 03 — Accounts, usage and launch defaults

Status: not started. Prerequisite: [02 — Settings](02-settings.md).
Next: [04 — Garden foundations](04-garden-foundations.md).

## Deliverable

A top-level Accounts workspace with selectable profiles, Overview/Usage/Defaults,
provider identity and meaningful usage bars. This supplies account selection to
later planners, launchers and Playground; it does not implement those workspaces.

References: [Accounts](../../design/tui-redesign/revision-02/png/a-01-120x35-codex.png),
[compact Accounts](../../design/tui-redesign/revision-02/png/a-01-80x24-codex.png),
[Usage](../../design/tui-redesign/revision-02/png/a-02-120x35-codex.png),
[Defaults](../../design/tui-redesign/revision-02/png/a-03-120x35-codex.png).

## 03.1 — Reuse the existing account contract

- Adapt `agentsapi.Accounts`, `components/accounts` and `agentsbackend/accounts.go`
  to a workspace body. Keep provider-specific model and permission schemas.
- Decouple account availability from `Sessions.Supported()`. The baseline
  `WithAgents` path is session-oriented; an unsupported PTY must not hide Accounts.
- Preserve selected account ID, scroll and inspector section across refresh.
  Account scope is local-user/global, not the currently selected worktree.
- Empty, loading, missing CLI, read-only usage, sign-in-required and failed-read
  states need real capability reasons, not fabricated profiles or meters.

## 03.2 — Capacity, freshness and defaults

- Reuse the shared usage store and its request ordering/cache rather than add an
  independent refresh loop for every row. Show last good data as stale on error.
- Reconcile the baseline's remaining-capacity data with the mockup's `% used`
  labels explicitly. Choose a visible metric per meter and normalize correctly;
  never invert values silently. Preserve credits, reset grants, extra/model pools
  and unavailable windows rather than force every provider into two percentages.
- Unknown is not zero. Never sum incomparable provider windows. Attribute a
  model-specific pool to its reported model and avoid duplicate default rows.
- Provider accents remain within the existing palette. Warnings follow capacity
  semantics, not an arbitrary task-completion percentage.
- Defaults edit provider-declared fields, distinguish inherited from resolved
  values and handle unsupported values. Save only explicit changes; show failures
  and protect dirty drafts during profile/section switching.

## 03.3 — Account lifecycle and navigation

- Reuse explicit add/sign-in CLI flows through the current command boundary;
  suspend/resume the TUI correctly. Never refresh login automatically, write the
  user's provider home, or change `HOME`.
- Confirm removal, display running-session implications, and preserve existing
  account locks and ownership rules. Do not stop sessions merely by leaving the
  workspace or changing a profile.
- Settings → account defaults and old Agents account shortcuts open the same
  account selection in redesign mode. No second profile store is introduced.

## Source seams

`internal/ui/agentsapi/agentsapi.go`, `internal/ui/components/accounts/`,
`internal/ui/usagestore/`, `internal/ui/usagepane.go`,
`internal/agentsbackend/accounts.go`, `internal/core/agents/usage.go`,
`internal/core/agents/account_service.go` and existing account/usage tests.

## Exit criteria and verification

- [ ] Every profile is selectable; nested inspector focus and return are clear.
- [ ] Accounts remains usable when terminal support or GitHub is unavailable.
- [ ] Usage labels, windows, resets, credit data and stale states are truthful.
- [ ] Defaults round-trip through the existing backend without widening access.
- [ ] Add/login/remove paths use isolated test homes and fake providers in tests.
- [ ] Switching Accounts, Settings and other empty tabs never stops live holders.
- [ ] Targeted UI/backend/agents tests, affected race tests and full Go checks pass.
- [ ] `README.md`, `docs/tui-cli.md` and `docs/agents.md` reflect the new destination.

Completion record: pending implementation and validation evidence.
