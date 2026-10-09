# Claude provider: multi-profile, multi-session support

Status: plan, 2026-10-09. Base: `main` @ `8d749b6`.

Add Claude Code (`claude`) as a second real agent provider next to Antigravity.
Several Claude profiles can exist at once, and several sessions per profile can
run at the same time, from the CLI (`bonsai agent run`) and from the web UI
(Start agent → live terminal). Every test runs without a real Claude login.

## 1. Decisions (agreed with the user)

| Topic | Decision |
| --- | --- |
| Isolation model | Each profile owns a persistent, Bonsai-managed **`CLAUDE_CONFIG_DIR`**. All sessions of a profile share it. There is no credential vault copy, materialize or reconcile step for login profiles. |
| Auth modes | `login` (default): `claude auth login` into the profile's config dir, with all features. `token`: a `claude setup-token` one-year token, stored by Bonsai and injected as `CLAUDE_CODE_OAUTH_TOKEN`. Token mode has no refresh race, but it can only make model requests. |
| Existing setup | When a profile is created, Bonsai **seeds** it from the user's `~/.claude` (settings, CLAUDE.md, agents, commands, skills, output styles, user MCP servers). It never copies credentials, history or project state. This can be turned off with `--no-seed`. |
| Usage | Uses the **undocumented** endpoint `GET https://api.anthropic.com/api/oauth/usage`. Best effort, read only, and it never refreshes tokens. |
| Profile creation | CLI only (`bonsai agent account add claude <name> …`). The web UI lists profiles and launches sessions. A web login flow is out of scope (§9). |
| Platforms | Linux and WSL are fully supported. On macOS, login and launch work, and usage is unsupported in v1 (the token lives in the Keychain). On native Windows, the CLI works and the web terminal stays unsupported (the existing ConPTY gap, not Claude-specific). |

## 2. Evidence: how the Claude CLI works (and why it is simpler than Antigravity)

Checked against the official docs and against `claude` **v2.1.295**, installed
and run directly:

- **Multiple accounts are a supported feature.** The docs say: "give each account
  its own configuration directory … set the `CLAUDE_CONFIG_DIR` environment
  variable … Each directory has its own settings, session history, and claude.ai
  login or API key."
  ([Authentication → Log in with multiple accounts](https://code.claude.com/docs/en/authentication))
- **Credentials follow the config dir on every OS.** Linux and Windows use
  `$CLAUDE_CONFIG_DIR/.credentials.json` (0600). On macOS the Keychain entry is
  "keyed to that directory too, so a session with a different `CLAUDE_CONFIG_DIR`
  reads a different entry." (Same page, *Credential management*.) An older bug
  report says the opposite
  ([#70697](https://claudeissues.com/issue/70697-bug-claude-config-dir-does-not-isolate-macos-keychain-credentials-per-config-dir)),
  so Phase 0 re-checks it on a Mac if one is available.
- **Several processes on one config dir is how Claude Code is normally used**
  (several terminals at once). Bonsai therefore does **not** copy credentials per
  session. Copying, as Antigravity does, would be actively harmful: OAuth refresh
  tokens rotate, so independent copies would log each other out.
- **Known upstream limitation:** several sessions refreshing the *same* login at
  the same time can lose a race and force a `/login`
  ([#80585](https://claudeissues.com/issue/80585-bug-multiple-concurrent-local-sessions-race-on-oauth-refresh-token-rotation-near),
  [#56339](https://claudeissues.com/issue/56339-multiple-cli-sessions-race-on-claude-credentials-json-token-refresh)).
  Bonsai cannot fix this inside Claude and does not try. `token` mode is the
  race-free option, and the UI and docs say so.
- **Auth commands exist and are scriptable:**
  `claude auth login [--claudeai|--console|--email|--sso]`, `claude auth logout`,
  and `claude auth status` (JSON; exits 0 when logged in and 1 when not).
  `claude setup-token` prints a token and saves nothing.
  ([CLI reference](https://code.claude.com/docs/en/cli-reference)) Observed
  `auth status` JSON:
  `{"loggedIn":true,"authMethod":"oauth_token"|"api_key"|…,"apiProvider":"firstParty","configDirectory":"…","projectsDirectory":"…","apiKeySource":"ANTHROPIC_API_KEY"?}`.
- **Environment variables override the config dir (observed).** Run with an empty
  `CLAUDE_CONFIG_DIR`, `auth status` reported `loggedIn: true`. The credential
  came from inherited `CLAUDE_CODE_*` and `ANTHROPIC_*` variables, and
  `ANTHROPIC_API_KEY=…` switched `authMethod` to `api_key`. Documented order:
  cloud provider vars, then `ANTHROPIC_AUTH_TOKEN`, then `ANTHROPIC_API_KEY`,
  then `apiKeyHelper`, then `CLAUDE_CODE_OAUTH_TOKEN`, then `/login`
  credentials. ⇒ **Bonsai must scrub inherited `CLAUDE*`/`ANTHROPIC*` variables**,
  or a profile silently runs as a different identity. This matters most when
  Bonsai itself was started from a Claude Code shell, which sets about 90 such
  variables, including `CLAUDECODE`.
- **Launch flags (verified in `--help`):** `--model`, `--permission-mode
  <default|acceptEdits|plan|auto|dontAsk|bypassPermissions>`, `--effort
  <low|medium|high|xhigh|max>`, `-n/--name`, `--session-id <uuid>`, and an
  initial prompt passed as `claude "query"`. **`--` works:** `claude -p -- "--version: reply PARSED-OK"`
  returned `PARSED-OK`, so prompts that start with `-` are safe after `--`.
- **Long-lived token:** "a one-year OAuth token … It can only make model
  requests, so it can't establish Remote Control sessions or fetch claude.ai
  connectors. MCP servers you configure locally still work." Bare mode (`--bare`)
  ignores it. ([Authentication → Generate a long-lived token](https://code.claude.com/docs/en/authentication))
- **Usage:** documented only inside a running session (statusline JSON
  `rate_limits.five_hour/seven_day`). The polled source is undocumented:
  `GET /api/oauth/usage` with `Authorization: Bearer <access token>` and
  `anthropic-beta: oauth-2025-04-20`. It returns buckets
  (`five_hour`, `seven_day`, `seven_day_opus`, …, each with `utilization` 0–100
  and `resets_at`) and, in newer responses, a `limits[]` array of
  `{kind, percent, resets_at, scope?}`. The access token is in
  `.credentials.json` → `claudeAiOauth.accessToken`
  ([ccusage](https://pypi.org/project/ccusage/)). Setup tokens most likely lack
  the scope ([#81015](https://claudeissues.com/issue/81015-feature-request-read-only-usage-scope-on-claude-setup-token-or-a-usage-read-gran)).

**What carries over from Antigravity:** the account store, registry, session IDs,
PTY manager, web terminal protocol, idempotent start and stop, and crash recovery.
**What does not:** credential vault, `Materialize`, `Reconcile`, `CaptureSetup`,
the per-session `HOME` override, generation markers, and JWT identity parsing.

## 3. Where Antigravity is hardcoded today (has to become generic)

| Location | Hardcoding |
| --- | --- |
| `internal/core/agentterminal/manager.go:96` | `if account.Provider != "antigravity"` rejects every other provider. |
| `manager.go` `Start` | Builds Antigravity args (`--model`) and patches the `dangerously_skip_permissions` setting in the manager. |
| `manager.go` `run` | Appends `--prompt-interactive=<prompt>` (an Antigravity flag); the error text says "Check agy installation". |
| `manager.go` | Does not enforce `Capabilities().ConcurrentSameAccount`. |
| `internal/server/localapi/agents.go` `agentProviders` | Static list; only `agy` is checked; Claude and Codex are always "Not available yet". |
| `agents.go` `agentAccounts` | Filters to `antigravity`; returns `full_access` only. |
| `internal/agentruntime/runtime.go` | Registers only `antigravity.New`. |
| `internal/cli/agent_usage.go` | The dashboard knows only Antigravity limit classes. |
| `web/src/api/agents.ts` | `provider: 'antigravity'` types; `mapAgent` always sets `'Antigravity'`; the 404 text names Antigravity. |
| `web/src/types/index.ts` | `providerId?: 'antigravity'`. |
| `web/src/stores/bonsai.ts:311,369,518` | `createAgent` refuses non-Antigravity; stop and open-terminal gated on `providerId === 'antigravity'`. |
| `web/src/stores/workspacePersistence.ts:101,135` | Keeps only `antigravity` agents. |
| `StartAgentDialog.tsx` | Provider tabs are static; only Antigravity is active; Antigravity-only full-access copy and setup hint. |
| `Inspector.tsx`, `BonsaiNode.tsx`, `CommandPalette.tsx` | Actions enabled only for `providerId === 'antigravity'`. |
| Tests | `StartAgentDialog.test.tsx` and `e2e/agent-terminal-fullstack.spec.ts` assert Claude is **disabled**; these expectations get updated. |

## 4. Target architecture

### 4.1 Core additions (`internal/core/agents`), all provider-neutral

```go
// Per-launch options from the UI/CLI. Providers map them to their own flags.
type LaunchOptions struct {
    Model          string
    Prompt         string
    DisplayName    string
    FullAccess     *bool   // Antigravity
    PermissionMode string  // Claude: default|acceptEdits|plan|auto|dontAsk|bypassPermissions
    Effort         string  // Claude: low|medium|high|xhigh|max
}
type PrepareSessionRequest struct { Account; Session; Args []string; Launch LaunchOptions }

type PreparedSession struct {
    …existing…
    EnvUnsetPrefixes []string // e.g. "CLAUDE", "ANTHROPIC_"; applied before EnvSet
}

type SetupOptions struct {   // added to SetupRequest
    AuthMode   string        // provider-defined; "" = default
    Seed       *bool         // nil = provider default
    SeedFrom   string
    Secret     io.Reader     // token input (stdin / tests); never logged
}

// Optional interfaces, discovered with type assertions so Antigravity needs no changes:
type Describer       interface { Label() string; Availability(context.Context) Availability }
type LaunchValidator interface { ValidateLaunch(Account, LaunchOptions) error }   // called synchronously in Start → 400
type AccountDescriber interface { DescribeAccount(context.Context, Account) AccountInfo } // safe UI fields
type AccountRemover  interface { RemoveAccount(context.Context, Account) error }  // best-effort before store removal
type UsagePolicy     interface { UsageTTL() time.Duration }
```

`Availability{Available bool; Reason string; Version string}`.
`AccountInfo{AuthMode, Identity, Options map[string]any, Warnings []string}`.
`AccountInfo` never carries tokens, paths or raw settings.

### 4.2 Claude provider (`internal/providers/claude`)

```
claude/
  provider.go      ID "claude", Capabilities{Interactive, Usage, MultiAccount, ConcurrentSameAccount, ConcurrentCrossAccount: true}
  binary.go        LookPath("claude"); `claude --version` → semver; MinVersion const; cached per process
  paths.go         configDir = AccountDir/config ; tokenPath = CredentialDir/claude-token.json
  settings.go      {auth_mode, model, permission_mode, effort, seeded_from} + validation
  environment.go   unset prefixes CLAUDE, ANTHROPIC_ ; set CLAUDE_CONFIG_DIR, BONSAI_AGENT_* (+ token)
  launch.go        PrepareSession / FinalizeSession (no-op) / ValidateLaunch
  setup.go         login mode: `claude auth login` → `claude auth status`; token mode: `claude setup-token` → read secret
  seed.go          allowlisted copy from ~/.claude (or the host's CLAUDE_CONFIG_DIR)
  status.go        parse `auth status` JSON; identity from <configDir>/.claude.json oauthAccount
  remove.go        best-effort `claude auth logout` (clears that profile's macOS Keychain entry)
  usage.go         undocumented endpoint client + tolerant parser
```

**Session environment.** `HOME` is **not** overridden: git, gh, ssh and the
user's tools keep working. The environment is built like this:

- Unset every variable with prefix `CLAUDE` or `ANTHROPIC_`. This covers
  `CLAUDECODE`, `CLAUDE_CODE_*`, `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`,
  `ANTHROPIC_BASE_URL`, `ANTHROPIC_PROFILE`, `CLAUDE_CODE_USE_BEDROCK/VERTEX/FOUNDRY`,
  `CLAUDE_CODE_OAUTH_TOKEN` and others.
- Set `CLAUDE_CONFIG_DIR=<configDir>` and the existing
  `BONSAI_AGENT_PROVIDER/ACCOUNT_ID/SESSION_ID`.
- Token mode also sets `CLAUDE_CODE_OAUTH_TOKEN=<token>`, plus
  `CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1` so the agent's Bash subprocesses don't
  inherit the token (verify in Phase 0).
- Gateway or Bedrock users are out of scope for v1. Supporting them later means
  a per-profile `env` allowlist in settings, not inheriting the parent's
  variables.

**Arguments:**
`[explicit args…] --model M --permission-mode P --effort E --name N --session-id <uuid> [-- <prompt>]`.

- Each flag appears only when it is set and not already in the explicit args
  (reuse `hasFlag`).
- `--` and the prompt always come last.
- `--session-id` is a fresh UUID per Bonsai session. It is recorded as
  `Summary.ProviderSessionID`, so a later "Resume" feature (§9) can run
  `claude --resume <uuid>`.

**Setup.**

- *Login mode:* create `configDir`, seed it, run `claude auth login` in the
  foreground with the profile environment, then `claude auth status`. Exit
  code 1 → fail, and `AccountService` removes the account dir. Identity is read
  from `configDir/.claude.json` → `oauthAccount.emailAddress` (key names
  confirmed in Phase 0). If another Claude profile already has the same
  identity, print a warning (legitimate for different defaults, but usually a
  mistake). Do not reject.
- *Token mode:* run `claude setup-token` in the foreground, then read the token
  with no echo (`golang.org/x/term`, a new direct dependency) or from
  `--token-stdin`. Validate the `sk-ant-oat01-` prefix and a sane length. Store
  `{version, token, created_at, expires_at≈+365d}` 0600 in the credential dir.
  `DescribeAccount` warns when expiry is under 30 days away.

**Seeding** is a one-time snapshot, with an allowlist only:

- Copied: `settings.json`, `CLAUDE.md`, `agents/`, `commands/`, `skills/`,
  `output-styles/`, `keybindings.json`, plus the `mcpServers` key only, taken
  from the source's `.claude.json` (`~/.claude.json` when the host has no
  `CLAUDE_CONFIG_DIR`).
- Never copied: `.credentials.json`, `projects/`, `history*`, `sessions/`,
  `todos/`, `shell-snapshots/`, `statsig/`, `backups/`, `ide/`, `plugins/`
  (plugin install records contain absolute paths; see Phase 0), or any other
  `.claude.json` key.
- Symlinks are skipped, there is a total size cap (e.g. 20 MiB), files are
  written 0600 and dirs 0700, and Bonsai prints what it copied.
  `seeded_from` is recorded in settings.

**Prepare checks.** The config dir must exist. In token mode the stored token
must exist, otherwise `ErrNotAuthenticated`. In login mode a missing login does
**not** block launch: Claude shows its own login screen in the terminal, and
`/login` there writes into the profile's config dir. The UI shows auth status
from `DescribeAccount`, which runs `claude auth status` with a 5 s timeout,
cached for 60 s.

**Remove.** Best-effort `claude auth logout` (login mode), then the existing
`store.Remove` deletes the account dir, including `config/` and the token.

**Usage** (undocumented and best effort):

- *Login profiles:* read `configDir/.credentials.json`; it is absent on macOS,
  so return `ErrUsageUnsupported("macOS keychain")`.
- *Token profiles:* try the stored token; 401 or 403 →
  `ErrUsageUnsupported("long-lived tokens cannot read usage")`.
- If `expiresAt` has passed, return "usage unavailable until a session refreshes
  the login". **Never refresh**: refreshing would rotate the token and log out
  running sessions.
- `GET` with a 10 s timeout and an injectable base URL (tests).
- The parser accepts both the flat buckets and `limits[]` and maps them to
  `UsageLimit{ID:"five_hour"|"seven_day"|"seven_day_opus"|…, Window, RemainingFraction: 1-u/100, ResetsAt}`.
  Unknown shapes return an empty-but-valid snapshot with a warning, never a
  crash.
- Error strings never include the token or the response body.
  `UsageTTL()` = 5 min.

### 4.3 Local API contract changes (backward compatible)

- `GET /api/agents/providers` is built from the registry plus `Describer`.
  Known-but-unregistered IDs (`codex`) stay listed as "Not available yet".
  Response: `{id, label, available, unavailable_reason:{message}, version?}`.
- `GET /api/agents/accounts` returns every provider:
  `{id, name, provider, auth_mode?, identity?, warnings?, full_access?}`.
  Antigravity keeps `full_access`.
- `POST /api/projects/{p}/agents` adds the optional fields `permission_mode` and
  `effort` (each at most 32 chars).
  `LaunchValidator` errors → `400 invalid_launch_options`. Fields a provider
  does not support are rejected rather than ignored.
- `agentterminal.Summary` adds `provider_session_id,omitempty`. The snapshot and
  events shape is otherwise unchanged.

## 5. Phases

Each phase is one fresh session that ends green (`go test -race ./...`,
`npm run lint && npm test` in `web`, and the existing e2e) and with a commit on
its own branch. Phases 1 and 0 can run in parallel. Phase 3 needs Phase 1's API
contract and Phase 2's provider.

| Phase | What | Suggested model | Why |
| --- | --- | --- | --- |
| 0 | Manual verification on your machine (≈20 min, no code) | you | Needs a real login, which tests must avoid |
| 1 | Make the agent stack provider-generic (backend) | **Opus 5.5** | Cross-cutting refactor of concurrency-sensitive code; Antigravity must not change |
| 2 | Claude provider, CLI and runtime registration | **Opus 5.5** | Env scrubbing, secrets, setup flow; correctness matters more than speed |
| 3 | Web UI generalization and Claude launch options | **Sonnet 5.5** | Well-specified UI work with clear tests |
| 4 | Usage via the undocumented endpoint, plus CLI dashboard | **Sonnet 5.5** | Isolated client, parser and renderer |
| 5 | Review pass (fresh context, read-only first) | **Opus 5.5** | Independent check of security and lifecycle |

### Phase 0: Verify assumptions (manual, yourself)

Run these in WSL or Linux with Claude Code installed. Paste the outputs into
`testdata/claude/` with **all tokens, emails and IDs redacted**; they become test
fixtures.

1. `claude --version`. Record the version and fix `MinVersion` in Phase 2.
2. `CLAUDE_CONFIG_DIR=/tmp/cc-a env -u ANTHROPIC_API_KEY claude auth status; echo $?`
   (logged out) → JSON + exit 1? Save as `auth-status-logged-out.json`.
3. `CLAUDE_CONFIG_DIR=/tmp/cc-a claude auth login`, then `auth status` again,
   saved as `auth-status-claudeai.json`.
4. List the key names only (not values) in `/tmp/cc-a/.claude.json` (look for
   `oauthAccount.emailAddress`) and in `/tmp/cc-a/.credentials.json` (expect
   `claudeAiOauth.{accessToken,refreshToken,expiresAt,scopes,subscriptionType}`).
5. Start two `CLAUDE_CONFIG_DIR=/tmp/cc-a claude` sessions in two worktrees and
   chat in both. Confirm both work.
6. `claude setup-token`: is the format `sk-ant-oat01-…` and the length about
   100? Does `CLAUDE_CODE_OAUTH_TOKEN=<it> CLAUDE_CONFIG_DIR=/tmp/cc-b claude`
   work interactively, and does `curl -H "Authorization: Bearer <it>" -H "anthropic-beta: oauth-2025-04-20" https://api.anthropic.com/api/oauth/usage`
   return 200 or 401/403?
7. The same curl with `accessToken` from step 4. Save the redacted body as
   `usage-response.json`.
8. Seeding: copy `~/.claude/settings.json` (with `enabledPlugins`) into
   `/tmp/cc-c`. Does Claude reinstall those plugins by itself? This decides
   whether `plugins/` stays excluded.
9. `CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1`: in a token session, ask Claude to run
   `env | grep -c CLAUDE_CODE_OAUTH_TOKEN` and expect `0`.
10. Optional, on a Mac: two config dirs → two separate Keychain entries
    (`security dump-keychain | grep "Claude Code"`).

### Phase 1: Provider-generic agent plumbing (backend, no Claude code yet)

**Change:**

1. Add `LaunchOptions` and `SetupOptions` (§4.1). Move Antigravity's `--model`,
   full-access settings patch and `--prompt-interactive=` out of
   `agentterminal/manager.go` into `providers/antigravity/launch.go`. The
   manager only forwards `LaunchOptions`.
2. `manager.Start`:
   - Replace the provider check with `registry.Get` + `Capabilities().Interactive`.
   - Call `LaunchValidator` synchronously.
   - Enforce `ConcurrentSameAccount=false` (409-style error) and
     `ConcurrentCrossAccount=false`.
   - Generic failure text: "Agent failed to start … check the <Label()>
     installation and profile."
   - Record `ProviderSessionID` if the provider returns one. Add an optional
     `PreparedSession.ProviderSessionID`.
3. `BuildEnvironment` supports `EnvUnsetPrefixes` (case-insensitive on Windows;
   EnvSet still wins).
4. `localapi`:
   - Build providers and accounts from the registry plus the optional
     interfaces.
   - Accept `permission_mode` and `effort` in `agentRequest`, with strict
     decode and length limits.
   - Keep the 404 and old-shape behaviour for older UIs.
5. `AccountService`: pass `SetupOptions`; call `AccountRemover` before
   `store.Remove` (failure → warning, not abort).
6. CLI:
   - `account add <provider> <name> [--auth <mode>] [--token-stdin] [--no-seed] [--seed-from <dir>]`.
     Flags that are unknown for a provider are rejected by the provider's setup.
   - `account list` gains AUTH and IDENTITY columns from `AccountDescriber`
     (blank for Antigravity).
7. Update `docs/development.md` with the generic provider contract.

**Tests (no logins):**

- `agentterminal`:
  - A fake provider with ID **`fake`**, not `antigravity`, starts, takes input
    and stops (proves the hardcoding is gone).
  - A provider with `ConcurrentSameAccount:false` rejects a second active session
    on the same account but allows a different account.
  - `LaunchOptions` reach `PrepareSession` unchanged.
  - A validator error leaves no session and no runtime dir behind.
  - Three concurrent sessions on one account all reach `running`, finalize once
    each and clean up their own runtime dirs (`-race`).
- `antigravity`: golden argv/env tests showing argv is **byte-identical** to
  before for model, full-access and prompt combinations.
- `agents`: `BuildEnvironment` prefix-unset tests (`CLAUDECODE`, `CLAUDE_X`,
  `ANTHROPIC_API_KEY` removed by prefixes `CLAUDE` + `ANTHROPIC_`; unrelated
  vars such as `PATH` and `GH_TOKEN` kept); `EnvSet` overrides an unset prefix;
  Windows matching is case-insensitive.
- `localapi`:
  - `providers` lists registered providers with availability from a stub
    `Describer`.
  - `accounts` returns two providers' accounts with no secret fields (assert
    the JSON keys).
  - Start rejects unsupported `permission_mode` with 400.
  - Idempotency fingerprint includes the new fields.
- CLI: flag parsing for `account add` (unknown flag, missing value, and
  `--token-stdin` with no data).

**Exit:** Antigravity works exactly as before (all existing tests unchanged
except moved assertions). A second fake provider can be launched from the API.

### Phase 2: Claude provider and CLI

**Change:**

1. Implement `internal/providers/claude` per §4.2.
2. Register it in `agentruntime.New`.
3. `Describer.Availability`: binary missing → "Install Claude Code and restart
   Bonsai"; too old → "Update Claude Code (found X, need Y)"; terminal
   unsupported → the existing message.
4. CLI help text and README section: add a profile (login and token modes),
   run, remove, seeding, and the concurrency caveat.

**Test harness:** `internal/providers/claude/testdata/fakeclaude.sh`, a POSIX
script put first on `PATH` in tests. Windows unit tests use the injected
`BinaryResolver` and `Launcher` instead. It emulates:

- `--version` → `2.1.295 (Claude Code)`; `FAKE_CLAUDE_VERSION` overrides it.
- `auth status` → JSON and exit 0 or 1 depending on whether
  `$CLAUDE_CONFIG_DIR/.credentials.json` exists. Output comes from the Phase 0
  fixtures.
- `auth login` → writes a fake `.credentials.json` and a `.claude.json` with
  `oauthAccount.emailAddress=$FAKE_CLAUDE_EMAIL`. `FAKE_CLAUDE_LOGIN_FAIL=1` →
  exit 1.
- `auth logout` → deletes the credentials and appends to a call log.
- `setup-token` → prints `sk-ant-oat01-FAKE…`.
- default → prints `ARGV:` and an env report (`CONFIG_DIR=…`,
  `HAS_API_KEY=0|1`, `HAS_OAUTH_TOKEN=0|1`, `CLAUDECODE=unset|…`), then loops
  `read l; echo "REPLY:$l"`.

**Tests:**

- Environment:
  - With the parent env polluted (`ANTHROPIC_API_KEY`, `CLAUDECODE=1`,
    `CLAUDE_CODE_OAUTH_TOKEN`, `CLAUDE_CONFIG_DIR=/elsewhere`, `ANTHROPIC_BASE_URL`),
    the prepared env has none of them, and `CLAUDE_CONFIG_DIR` points at the
    profile's dir.
  - `HOME` is unchanged.
  - Token mode sets `CLAUDE_CODE_OAUTH_TOKEN` and the scrub flag.
- Isolation:
  - Two sessions of one profile get the same `CLAUDE_CONFIG_DIR` and different
    `BONSAI_AGENT_SESSION_ID` and `--session-id`.
  - Two profiles get different dirs.
  - Nothing is ever written to the real `~/.claude`: tests set `HOME` to a temp
    dir and assert it is unchanged after setup, launch and remove.
- Argv:
  - The model, permission-mode, effort and name matrix; explicit args win.
  - A prompt starting with `-` goes after `--`.
  - An empty prompt → no `--`.
  - `ValidateLaunch` rejects bad mode or effort values and `FullAccess`.
- Setup:
  - Login success records identity.
  - Login failure removes the account dir.
  - Duplicate identity → warning.
  - Token via `--token-stdin` is stored 0600, the file contains the token, and
    the CLI's stdout and stderr never echo it.
  - A bad token prefix is rejected.
  - Token mode with a missing token file → `ErrNotAuthenticated` at launch.
- Seed:
  - Allowlisted files copied; `.credentials.json`, `projects/` and history not
    copied.
  - Only `mcpServers` taken from `.claude.json`.
  - Symlink skipped; size cap enforced.
  - `--no-seed` copies nothing.
  - The source honours the host's `CLAUDE_CONFIG_DIR`.
- Remove: `auth logout` is called with the profile env; a logout failure still
  removes the dir and warns.
- Status: parse the Phase 0 fixtures; timeout → "status unknown", not an error.
- End to end through `agentterminal` (Unix): two concurrent sessions on one fake
  Claude profile plus one Antigravity fake session. Input reaches the right PTY;
  each has its own `--session-id`; stop one, the others keep running.
- CLI integration: `bonsai agent account add claude work --token-stdin`, `list`,
  `run work -- --help` (fake), and `remove`.

**Exit:** `bonsai agent account add claude <name>` and `bonsai agent run <name>`
work against the real CLI in your manual smoke. API start with a Claude profile
works.

### Phase 3: Web UI

**Change:**

1. Types:
   - `AgentSummary.provider` / `AgentAccount.provider` / `Agent.providerId` →
     `'antigravity' | 'claude'`.
   - `PROVIDER_LABELS` map used by `mapAgent`.
   - Add `isLiveAgent(agent)` (has a backend `providerId`) and replace every
     `providerId === 'antigravity'` gate (store, persistence, Inspector,
     BonsaiNode, CommandPalette).
2. `StartAgentDialog`:
   - Provider tabs come from `/api/agents/providers` (enabled when available
     **and** the provider has at least one profile; otherwise the disabled
     reason shows).
   - Default tab: the only provider with profiles, otherwise the last used,
     remembered per viewer.
   - Profile select filtered by provider; auto-select when there is exactly one.
   - Options panel by provider:
     - Antigravity: unchanged full-access switch.
     - Claude: permission mode select (Default · Accept edits · Plan · Auto ·
       Bypass permissions, with a warning tint on Bypass), effort select (Profile
       default · low … max), model input with a `sonnet / opus / haiku / fable or
       full ID` hint.
   - Profile row shows auth mode (`login` / `token`), identity and warnings
     (e.g. token expiring).
   - Footer summary per provider; setup hint per provider
     (`bonsai agent account add claude <name>`).
   - The request fingerprint includes provider and options.
3. Store `createAgent`: generic. It sends `permission_mode` and `effort` only
   for Claude. Its error says "Select a profile and worktree."
4. `CommandPalette` copy: "Agent profiles support interactive terminals."
5. Update `e2e/mockGit.ts` with a Claude profile and session. Point
   `TestAgentBrowserFixture` at the fake `claude` too.

**Tests:**

- `StartAgentDialog.test.tsx`:
  - Claude is enabled when available and has profiles; disabled with its reason
    when not.
  - Switching provider filters profiles and resets the selection.
  - Claude submit carries `permissionMode` and `effort`; Antigravity submit
    still carries `fullAccess`.
  - Changing the permission mode changes the request key; a retry keeps it.
  - Per-provider setup hint.
- Store, persistence and inspector tests for Claude agents: kept on reload,
  stop and open terminal enabled.
- `mapAgent` label test.
- Snapshot reconciliation with a mixed list of providers.
- E2E, mocked: start a Claude agent, then a second one on the **same profile**;
  both nodes and terminals show.
- E2E, full stack (Unix, fake binary): two concurrent sessions of one Claude
  profile, input in each (`REPLY:` lines go to the right terminal), reload and
  reattach, stop one.

**Exit:** The UI starts and stops several Claude sessions per profile next to
Antigravity ones. Every Antigravity UI test passes with only the expected
"Claude disabled" assertions changed.

### Phase 4: Usage (undocumented endpoint)

**Change:**

- Implement `claude.Usage` per §4.2, plus `UsagePolicy` 5 min, honoured by
  `UsageService`.
- CLI dashboard: Claude rows (5h, weekly, and weekly per model when present)
  rendered in a Claude table section next to the Antigravity one.
- Warning in docs and help: the endpoint is undocumented and may break; Bonsai
  never refreshes tokens.

**Tests (`httptest` server, no network):**

- Headers (`Authorization` and `anthropic-beta`) are present.
- Flat-bucket and `limits[]` fixtures (Phase 0 step 7) parse to the expected
  `UsageLimit`s.
- Unknown JSON → empty snapshot, no panic.
- 401/403 on a token profile → `ErrUsageUnsupported`.
- An expired `expiresAt` makes no HTTP call.
- Missing `.credentials.json` (macOS simulated) → unsupported.
- A timeout respects the context.
- The token string never appears in any returned error (fuzz with the token
  embedded in the response body).
- The cache TTL is honoured.
- `bonsai agent usage` rendering golden tests with mixed providers.

**Exit:** `bonsai agent usage` shows Claude 5h and weekly utilization for login
profiles on Linux/WSL and fails soft everywhere else.

### Phase 5: Independent review

Use a fresh session that has not seen the implementation. Read the diff of
Phases 1–4 against this plan and check:

- no secret in logs, JSON, errors or snapshots;
- env scrubbing is complete;
- nothing writes outside the profile dir or the real `~/.claude`;
- finalize and cleanup run exactly once;
- idempotency still holds;
- Antigravity argv is unchanged.

Fix any finding in a follow-up commit.

## 6. Handoff prompt template (one per phase)

> You are implementing **Phase N** of `docs/claude-provider-plan.md` in the Bonsai
> repo. Read that plan fully (sections 2–5), then `docs/development.md` and the files
> listed in section 3 for your phase. Work only on Phase N's "Change" list. Write every
> listed test; tests must never require a real Claude or Antigravity login or network
> access. Keep Antigravity behaviour byte-identical. Finish with `go test -race ./...`
> and, if web changed, `cd web && npm run lint && npm test && npx playwright test`,
> then commit on branch `feat/claude-provider-phase-N`. If the plan is contradicted by
> code or by the Phase 0 fixtures in `testdata/claude/`, stop and report rather than
> improvising.

## 7. Risks

| Risk | Mitigation |
| --- | --- |
| Upstream refresh race logs out concurrent sessions of one login profile | Documented; token mode offered; the UI shows auth mode; Bonsai never refreshes or copies credentials itself. |
| Undocumented usage endpoint changes or disappears | Tolerant parser, fail-soft errors, isolated in `usage.go`, 5-min TTL. |
| Inherited env silently switches identity | Prefix scrub, plus a test with a polluted parent env. |
| macOS Keychain behaviour differs from the docs | Phase 0 step 10; usage disabled on macOS in v1. |
| CLI flags change between Claude versions | `MinVersion` check; argv built in one place with golden tests. |
| A seeded plugin config breaks a fresh profile | `plugins/` excluded until Phase 0 step 8 says otherwise; `--no-seed`. |
| Token in the process environment | It is the documented mechanism; same-user only; subprocess scrub flag; the token file is 0600. |

## 8. Out of scope (later)

- Creating profiles and logging in from the web UI (run `claude auth login` in a
  web terminal).
- "Resume session" after an API restart, using the recorded
  `provider_session_id` (`claude --resume <uuid>`).
- A per-profile env allowlist (gateways, Bedrock, Vertex) and editing profile
  defaults from the CLI or web.
- A native Windows web terminal (ConPTY), Codex provider, and showing usage in
  the web UI.
