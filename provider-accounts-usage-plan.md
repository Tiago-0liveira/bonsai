# Provider accounts, models and usage: implementation plan

Status: plan, 2026-10-13. Base: `origin/main` @ `ed616b7`.
Go 1.26.1 builds here and the baseline tests for `internal/providers/...`, `internal/core/agents`,
`internal/cli` and `internal/server/localapi` pass.

## Goal

1. **Antigravity**: list the account's models from `agy models`, with efforts; show the signed-in
   email, whether the profile is logged in, and the plan name ("Google AI Pro"); show credits.
2. **Claude**: keep the subscription name (today it is read and thrown away), show credit pools
   and spend, and show every limit's reset date.
3. **Codex**: it already has plan, limits and credit balance. Add the reset credits ("claims")
   with their expiry dates, and show reset dates everywhere.

## What the captures established (2026-10-13)

| Source | Fact | Consequence |
| --- | --- | --- |
| `agy models` | stdout is `id<TAB>label` per line, after one `Fetching available models...` line. No JSON flag. `gemini-3.8-flash-{high,medium,low}` are three lines; `claude-sonnet-4-6`, `claude-opus-4-6-thinking`, `gpt-oss-120b-medium` appear once. | Parse only lines containing a tab. Efforts come from grouping IDs that differ in the last segment; a lone `-medium` is not split. |
| `agy -p /usage --output-format json` | Buckets `gemini-weekly`, `gemini-5h`, `3p-weekly`, `3p-5h`, each with `remaining_fraction` and `reset_time`. No plan, no email. | Limits and resets already parse. Add a test with this payload. |
| `agy -p /credits --output-format json` | `{"remaining_credits":0,"upgrade_uri":...}`. | A credit balance, not a plan name. |
| `agy` interactive header | `email (Google AI Pro)` is drawn at the top, **after** other text, by cursor movement. | The plan can only be read from a rendered screen. See Phase 3. |
| `agy --help` | Has `--effort` and `--model`; no status or whoami command. `-p /anything` that is not a real command goes to the model and costs tokens. | Never send unknown slash commands with `-p`. Efforts are chosen through the model ID, not `--effort`. |
| `claude auth status` | `subscriptionType: "pro"`, `email`, `orgName`. | `authStatus` (`claude/status.go:16`) drops `subscriptionType`; keep it. |
| Claude `/api/oauth/usage` | `five_hour.resets_at` is `null` at 0% (window not started). `iguana_necktie` is a dollar pool: `limit_dollars 100`, `used_dollars 1.9`, `remaining_dollars 98.09`, `resets_at 2026-11-05`. `extra_usage` and `spend` exist but are disabled. No list of claims. | Credit pools come from top-level buckets that carry `limit_dollars`. Claude has no claims data; claims are Codex-only. |
| Codex `account/rateLimits/read` | `rateLimitResetCredits` lists `availableCount` and credits with `title`, `status`, `grantedAt`, `expiresAt` (fixture in `codex/testdata/phase0/rate-limits.json`). | Parse into reset credits. The `credits` block is currently turned into a warning string. |

## Decisions

| Topic | Decision |
| --- | --- |
| Data shape | Add fields to `agents.UsageSnapshot`; `omitempty`, so existing cache files and API clients keep working. |
| Plan name location | `AccountInfo.Plan` (profile list) and `UsageSnapshot.Plan` (usage). `AccountInfo.Identity` becomes the email only; Codex stops returning `"email · plan"` from `identity.go:24`. |
| Claims | Display only. Bonsai does not redeem a claim. |
| New dependencies | None. `creack/pty` and `charmbracelet/x/ansi` (`Parser` + `Handler`, checked in v0.11.6) are already in `go.mod`. |
| Antigravity plan | Read from the interactive header in a PTY, cached, never read on the profile-list path. |
| Windows | The plan probe is unsupported there (`creack/pty` is Unix only, same split as `agentterminal/process_*.go`); the plan stays blank. |
| Deferred | Claude's `seven_day_breakdown` (share of the week by product) and `/effort`. Neither was asked for. |

## Phase 1: shared usage and plan data, Claude and Codex

Everything the user asked for from Claude and Codex, plus the contract Phase 3 fills in.

**Core (`internal/core/agents`)**
- `usage.go`: add `Credits []UsageCredit` and `ResetCredits []ResetCredit` to `UsageSnapshot`.
  - `UsageCredit{ID, Label, Unit, Used, Limit, Balance *float64, Unlimited, Enabled bool, ResetsAt *time.Time}`.
    `Unit` is `"USD"` or `"credits"`; amounts are major units.
  - `ResetCredit{Title, Status string, GrantedAt, ExpiresAt *time.Time}`. The provider's credit IDs are never copied.
- `provider.go`: add `Plan string` to `AccountInfo`.
- `internal/server/localapi/agents.go` `accountItem`: emit `plan` next to `identity`.

**Claude (`internal/providers/claude`)**
- New `plan.go`: read `subscriptionType` (and `rateLimitTier` if present) from the profile's
  `.credentials.json` first, so no process starts; fall back to `auth status` (macOS keychain).
  Map to a label: `pro` → "Pro", `max` + tier → "Max 5x"/"Max 20x", `team`, `enterprise`; unknown values pass through.
- `status.go`: add `SubscriptionType` to `authStatus`; `DescribeAccount` sets `info.Plan`.
- `usage.go`: `ParseUsage` returns a `ParsedUsage{Limits, Credits, Warnings}` struct instead of three values.
  - Credit pools: any top-level object with a non-null `limit_dollars`
    (`used_dollars`, `remaining_dollars`, `resets_at`); label from a small map, default "Usage credits".
  - `spend` wins over `extra_usage` when both are present, to avoid counting twice. `spend.used.amount_minor / 10^exponent`.
  - Disabled with no values: emit nothing.
  - `resets_at: null` stays nil on the limit.
  - `Usage()` fills `Plan`.
- Open assumption: `extra_usage.used_credits` with `decimal_places` is treated as minor units. It is `null`
  in every capture, so its test fixture is synthetic and marked as such.

**Codex (`internal/providers/codex`)**
- `usage.go`: parse `rateLimitResetCredits` into `ResetCredits`; turn `credits` into a `UsageCredit`
  (`Unit: "credits"`, `Balance` from the string, `Unlimited`) and delete `creditWarnings`. Update `usage_test.go`.
- `identity.go`/`describe.go`: `Identity` = email, `Plan` = plan.

**CLI (`internal/cli/agent_usage.go`)**
- Add a Plan column for every provider (today only the Codex table has it), a Resets cell
  (next reset, relative and absolute), and detail lines for credits and reset credits. Update
  `testdata/usage-three-providers.golden`.

**Web (`web/src`)**
- `api/agents.ts`: `UsageCredit`, `ResetCredit`, `plan` on `AgentAccount`, fields on `UsageSnapshot`.
- `features/agents/ProfileUsage.tsx`: a Credits section; a "Rate-limit resets" section (available
  count, each expiry as countdown and date).
- `UsageMeter.tsx` `LimitRow`: show the absolute reset time beside the countdown; when `resets_at`
  is missing and remaining is 100%, show "window not started".
- `AgentsPage.tsx:158`, `ProfileRow.tsx:31`, `ProfileOverview.tsx:17`: show the plan beside the email.
- Update unit tests and the e2e mocks (`agents-tab*.spec.ts`).

**Tests and done when**
- Parsers run on the real captures (`claude-usage.json` with the dollar pool and null reset; the
  Codex phase0 fixture with three reset credits) and on synthetic enabled-spend payloads.
- `/api/agents/usage` JSON contains `credits`, `reset_credits` and `plan`; old cached snapshots still load.
- `go vet`, `go test -race ./...`, and the web checks in `CONTRIBUTING.md` pass.

## Phase 2: Antigravity models and efforts

**Shared runner (`antigravity/usage.go`, `usage_cli.go`)**
- Pull the create-session, materialize, run, reconcile, clean-up sequence out of `Usage` into
  `withProfile(ctx, account, fn)`. `Usage` and `Models` both use it. No credential lock is held while `agy` runs (as today).

**`antigravity/models.go`**
- `Models(ctx, account)`: runs `agy models` with a 15 s timeout. Cache 10 min on success, 1 min on failure,
  per profile. Never returns an error: a failure returns an empty list, as Claude's `Models` does.
- `ParseModels(out)`:
  1. Keep lines with a tab; split into `id`, `label`. Trim; skip the rest (including the `Fetching…` line, whichever stream it is on).
  2. A line is a candidate when its id ends in `-<word>` and the label ends in `(<Word>)` with the same word, case-insensitive.
  3. Candidates with the same id-prefix **and** label-prefix form a group only when there are two or more.
     The group becomes one `ModelOption{ID: prefix, Label: label-prefix, Efforts, DefaultEffort, Source: "bundled"}`.
  4. Everything else stays one model with its own ID and no efforts (`NoEffort: false`, since agy did not say it has none).
  5. Efforts are ordered low, medium, high, xhigh, max; unknown words follow in listed order.
  6. `DefaultEffort` = the first effort listed by agy (`high` for Gemini 3.8 Flash, matching the footer agy shows by default).
- Fixture: your real `agy models` output in `testdata/models.txt`.

**Launch (`launch.go`, `settings.go`, `profile.go`)**
- `LaunchFields`: add `effort` (`PerModel: true`, fallback options low/medium/high).
- `composeModel(base, effort, models)`: effort empty and a grouped model → `base-<DefaultEffort>`;
  effort set → `base-<effort>`; an ID that is not a group base passes through unchanged. agy receives one
  `--model <full-id>`. `--effort` is never used.
- `ValidateLaunch`: stop rejecting effort. If the cached list knows the model, the effort must be in its
  list; if not, accept `^[a-z0-9]{1,16}$`. Keep rejecting permission mode, sandbox and approval.
- `Settings.Effort`; `UpdateProfile` handles `effort`; `ProfileDefaults` lists it. Existing profiles that store a full ID keep working.

**Tests and done when**
- Parser: the real output; CRLF; blank lines; header only; a lone `-medium` stays whole; two groups; ordering.
- Argv golden for model only, model + effort, full ID, and explicit `--model` in args.
- Validation with and without a cached list; profile round trip.
- Web: no code change expected (`LaunchFields.tsx:41` already reads `model.efforts`); add a test with a mocked antigravity model list.
- Manual, on your machine: `agym targ -- --model gemini-3.8-flash-low -p "reply ok" --output-format json`
  proves the composed ID is accepted (one low-effort call).

## Phase 3: Antigravity account details (email, login, plan, credits)

**Email and login state (no process)**
- `DescribeAccount` reads the stored identity from the vault; a fallback identity counts as "no email".
  `Options["auth_status"]` = `logged_in` when credentials are complete, `logged_out` when the vault is
  missing or incomplete, with a warning that points at `bonsai agent account login`. Needs the signed-out
  screen to know what a stale login looks like: run `scripts/agy-plan.sh -v` on the `tmg` profile
  (its token file is missing) and keep that capture.

**Credits (`/credits`)**
- `Usage` also runs `agy -p /credits --output-format json` (built-in, no tokens) and adds one
  `UsageCredit{Unit: "credits", Balance}`; the upgrade link is not shown in Bonsai.

**Plan probe, in Go**
- `internal/core/termscreen`: a screen of N×M cells fed through `ansi.Parser` with a `Handler`:
  `Print`, `Execute` (CR, LF, BS, TAB), and `HandleCsi` for CUP/CUU/CUD/CUF/CUB/CHA/VPA/EL/ED/ECH and alt-screen.
  Colors, modes and scroll regions are ignored. ~150 lines, tested against a recorded raw capture of the real `agy`
  (add a `--raw FILE` flag to `scripts/agy-plan.py` to save one) and a synthetic late-painted header.
- `antigravity/planprobe_unix.go` (`//go:build unix`): `probePlan(ctx, exe, dir, env, email)`.
  - `pty.StartWithSize` 140×40 (`creack/pty` already sets `Setsid` and `Setctty`, so `-pid` signals the group); `TERM=xterm-256color`.
  - Reader goroutine feeds the screen; poll every 100 ms for `<email>\s*\(([^()]+)\)`; the matched email must equal the vault identity.
  - Deadline 8 s. Then `0x03`, 300 ms, `0x03`; wait up to 3 s; SIGTERM to the group; then SIGKILL. Always `Wait` so nothing is left behind.
  - Run in an empty temp working directory and the profile's materialized home. The only bytes ever written are the two Ctrl+C, so no prompt reaches the model.
  - The plan is truncated to 40 printable characters and never logged; the screen is discarded.
- `planprobe_other.go`: returns `errUnsupported` (Windows).
- Policy (`antigravity/plan.go`):
  - State in `<AccountDir>/plan.json`: `{plan, email, checked_at, failed_at}`, mode 0600.
  - Re-probe when the email changed, the plan is older than 24 h, or the plan is empty and the last failure is over 15 min old.
  - One probe at a time process-wide (a semaphore of 1) and one per profile (single flight).
  - Triggered from `Usage()` in a goroutine with its own 20 s context, and on an explicit refresh. **Never** from `DescribeAccount`, which only reads `plan.json`.
  - The probe is followed by `Reconcile`, as the other `agy` runs are, in case agy refreshed the token.
- `DescribeAccount` and `Usage` return `Plan` from `plan.json`, so a finished probe shows on the next poll without waiting for the usage cache.
- Test fake: a Go helper binary (re-exec of the test binary, like `fakecodex.sh`) that sets raw mode, paints the header late,
  and exits only after two `0x03`. Cases: found, wrong email, never paints, ignores Ctrl+C (kill path), canceled context; assert no process remains.

**Done when**
- The profile list shows `email · Google AI Pro`, the usage panel shows credits, and an unauthenticated profile reads "logged out".
- Opening the profile list repeatedly starts **zero** `agy` processes; a refresh starts at most one probe.
- A broken probe leaves the plan blank and never an error in the UI.

## Risks

| Risk | Mitigation |
| --- | --- |
| `agy` changes its header layout | Plan stays blank; the screen capture test fails loudly in CI, not for users. |
| A first-run dialog (trust folder, update) covers the header | The probe times out; empty plan; retried after the backoff. It never presses a key other than Ctrl+C. |
| One Ctrl+C is not enough to quit | Second Ctrl+C, then SIGTERM, then SIGKILL; tested. |
| `agy` refreshes the token inside the probe | `Reconcile` after every run, like `Usage`. |
| Claude `extra_usage` units | Parsed defensively; skipped when ambiguous; confirmed when an account with it enabled can be captured. |
| Changing `Identity` for Codex breaks goldens | Update the CLI goldens and web tests in Phase 1, in the same commit. |

## Verification checklist per phase

`go vet ./...`, `go build ./...`, `go test -race ./...`, then the web commands in `CONTRIBUTING.md`
(`typecheck`, `lint`, `test`, `test:bundle`, `build`, `bundle:check`, `go test -tags embedui ./web/`, `test:e2e`).
