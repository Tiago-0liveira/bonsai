# AI agents and provider profiles

Bonsai runs coding agents (Claude Code, Codex and Antigravity) next to your
worktrees. Each provider can have several **profiles** (one per account), and
every profile gets its own isolated config directory, so many sessions can run
at once across worktrees without one account's login leaking into another.

- Start an agent from the web workspace (select a worktree, choose **Start
  agent**, pick a profile) or from a terminal with `bonsai agent run`.
- Manage profiles with `bonsai agent account ...` or in the web **Agents** tab
  (see [Agents tab](web.md#agents-tab)).
- See plan limits for every profile of every provider with
  `bonsai agent usage`.

| Provider | CLI to install | Minimum version |
| --- | --- | --- |
| Claude Code | `claude` | 2.1.295 |
| Codex | `codex` | 0.162.1 |
| Antigravity | `agy` | none enforced |

`bonsai agent run <account>` runs the provider CLI as that profile in the
current directory. Interactive agent terminals in the web dock need a supported
Unix platform; native Windows is not supported yet.

For how providers plug into the core, the HTTP routes and the web terminals,
see [development.md](development.md#agent-provider-contract).

## Claude Code profiles

Install `claude` (Claude Code), then add one Bonsai profile per Claude account.
Each profile owns a persistent config directory (`CLAUDE_CONFIG_DIR`) that all
of its sessions share; your real `~/.claude` is never written.

```sh
bonsai agent account add claude work            # login mode (default)
bonsai agent account add claude ci --auth token # long-lived token mode
echo "$TOKEN" | bonsai agent account add claude ci --token-stdin
bonsai agent account list                       # shows auth mode and identity
bonsai agent run work -- --model opus           # extra args go to claude
bonsai agent account login work                 # sign the profile in again
bonsai agent account login ci --auth login      # switch a token profile to login
bonsai agent account remove work                # logs the profile out, deletes it
```

- **login** runs `claude auth login` inside the profile. All features work.
- **token** runs `claude setup-token` (or reads `--token-stdin`) and stores the
  one-year token 0600 in Bonsai's data directory; it is injected as
  `CLAUDE_CODE_OAUTH_TOKEN` at launch. A token can only make model requests, so
  it cannot use Remote Control or claude.ai connectors.
- **Seeding.** A new profile starts from a one-time snapshot of your own setup:
  `settings.json`, `CLAUDE.md`, `keybindings.json`, `agents/`, `commands/`,
  `skills/`, `output-styles/` and your user-level MCP servers. Credentials,
  history, sessions, plugins and API-key settings are never copied, and symlinks
  are skipped (and reported). Bonsai prints what it copied. Use `--no-seed` to
  skip it or `--seed-from <dir>` to seed from another config directory. Seeding
  copies MCP server definitions as they are, including any `env` or header
  secrets in them, into the profile (mode 0600). Login-pinning settings
  (`forceLoginMethod`, `forceLoginOrgUUID`) and credential helpers are dropped.
- **Isolation.** Sessions run with every inherited `CLAUDE*` and `ANTHROPIC_*`
  variable removed, so a profile never silently runs as another identity.
  `HOME` is unchanged, so git, gh and ssh keep working.
- **Concurrency caveat.** Several sessions of one *login* profile refresh the
  same OAuth login, and Claude Code can occasionally lose that race and ask for
  `/login` again. Token mode has no refresh and is race-free.
- **Token exposure.** In token mode the token is in the session's environment
  (that is how Claude Code reads it). Bonsai also sets
  `CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1` to keep it out of the agent's shell
  commands, but that flag was not confirmed against a real session, so treat
  commands the agent runs as able to see the token.
- **Usage.** `bonsai agent usage` shows the plan, the 5-hour and weekly
  utilization (and per-model weekly limits when present), each next reset, the
  dollar usage credits and any granted usage-limit resets (with what they
  refill and when they expire) of login profiles. The resets are read the way
  Claude Code reads them (`?cedar_ember=1`, sent with Claude Code's `x-app` and
  `User-Agent` headers, without which the endpoint does not list them); Bonsai
  never uses a reset. Claude has no documented endpoint for this, so Bonsai
  reads the one Claude Code's own `/usage` view uses (`/api/oauth/usage`). It
  can change or disappear without notice and is best effort. Bonsai only reads
  the profile's stored login and **never refreshes it** (a refresh would log out
  running sessions), so a profile whose login expired shows an error until a
  session refreshes it. Token profiles are shown as `n/a` because long-lived
  tokens cannot read usage, and so are macOS profiles (the login lives in the
  Keychain). Results are cached for 1 minute; `--refresh` bypasses the cache.
- **Activity.** Bonsai also learns whether a running session is working, waiting
  for you (permission prompt, question, plan approval) or idle. It adds
  observe-only hooks to each session with Claude Code's `--settings` flag, so the
  profile's `settings.json` is never written and your own hooks keep running. If
  you start Claude with your own `--settings` or `--bare`, or disable hooks, the
  activity stays unknown and the session shows as plain "Running". See
  [Activity hooks](development.md#activity-hooks).
  Questions and plan approval keep their specific reason even when Claude also
  emits a permission hook or a generic permission notification for the dialog.
- Requires Claude Code 2.1.295 or newer.

## Codex profiles

Install `codex` (Codex CLI), then add one Bonsai profile per OpenAI login.
Each profile owns a persistent `CODEX_HOME` that all of its sessions share;
your real `~/.codex` is never written.

```sh
bonsai agent account add codex work                  # ChatGPT login in the browser (default)
bonsai agent account add codex box --auth device     # device code, for SSH, WSL or headless hosts
printenv OPENAI_API_KEY | bonsai agent account add codex ci --token-stdin   # API key
bonsai agent account list                            # shows auth mode, email and plan
bonsai agent run work -- --search                    # extra args go to codex
bonsai agent account login work                      # sign the profile in again
bonsai agent account login box --auth device         # same, with a device code
bonsai agent account remove work                     # logs the profile out, deletes it
```

- **Logins stay with Codex.** Bonsai runs `codex login` inside the profile and
  forces the file credential store (`cli_auth_credentials_store = "file"`), so
  Codex keeps the login in the profile's own `auth.json` on every OS, never in a
  keyring entry other profiles would share. An API key goes straight from
  `--token-stdin` (or a hidden prompt) to `codex login --with-api-key`; Bonsai
  keeps no copy of any Codex secret.
- **Seeding.** A new profile starts from a one-time snapshot of `~/.codex` (or
  `$CODEX_HOME`): `config.toml`, `AGENTS.md`, `prompts/`, `skills/` and `rules/`.
  The copied `config.toml` loses login pins, `[profiles]`, `[projects]` (trusted
  folders) and every model provider that reads or carries a key. `auth.json`,
  sessions, history, logs, caches, plugins and key files are never copied, and
  symlinks are skipped. `--no-seed` and `--seed-from <dir>` work as for Claude.
- **Isolation.** Sessions run with every inherited `CODEX_*` and `OPENAI_*`
  variable removed (except `CODEX_CA_CERTIFICATE`), so a profile never silently
  runs as another identity. `HOME` is unchanged.
- **Sessions.** Codex starts with `--no-daemon`, so each session owns its whole
  process tree instead of attaching to a shared background app-server. Two
  profiles on the same ChatGPT login share its usage limits; Bonsai warns when
  you add one.
- **Usage and models.** `bonsai agent usage` shows the 5-hour and weekly limits
  of ChatGPT profiles (plus the limits of other metered features, the credit
  balance and the granted rate-limit resets with their expiry when Codex
  reports them; Bonsai never redeems a reset), read from `codex app-server`,
  Codex's published protocol; Codex does any token refresh itself. API key
  profiles have no plan limits and are shown as `n/a`. Results are cached for
  1 minute; `--refresh` bypasses the cache. The start dialog's model suggestions
  come from the same place, with the list bundled in Codex as the fallback.
- **Activity.** Like Claude, Codex sessions report working, waiting and idle.
  Bonsai adds observe-only hooks for the session's events to the profile's own
  `$CODEX_HOME/hooks.json` (never `~/.codex`), keeping every other entry in it,
  and marks exactly those entries trusted in the profile's `config.toml` through
  Codex's app-server, so Codex runs them without a review prompt. Your other
  hooks stay as untrusted as you left them, and Bonsai never uses
  `--dangerously-bypass-hook-trust`. If the trust step fails, the session still
  starts, and Codex's approval and question notifications (asked for with `-c`
  overrides, unless your own args set them) show as inferred "waiting". See
  [Activity hooks](development.md#activity-hooks).
  Async questions remain "Needs input (question)" after the question tool returns
  and its turn finishes, until you answer, cancel or interrupt the request.
- Requires Codex CLI 0.162.1 or newer.

## Antigravity in the web workspace

Install `agy`, then add a Bonsai profile:

```sh
bonsai agent account add antigravity personal
```

The profile list shows its email, whether its stored login is complete and,
once read, its plan ("Google AI Pro").

- **Usage.** `bonsai agent usage` reads the profile's quota from the Cloud Code
  API agy itself uses (undocumented; it can change without notice) with the
  stored login, and falls back to running agy's `/usage` when that fails or the
  login is about to expire; Bonsai never refreshes the login. Results are cached
  for 1 minute. The plan and the profile's credits (only when above 0) come from
  the same API read, so no `agy` is started for them, and listing profiles never
  starts `agy` either; the `/usage` fallback shows neither credits nor a fresh
  plan. The table lists Gemini models only; `--claude-gpt` adds the Claude & GPT
  model pools. The production host is asked first and lists the Gemini pool;
  the daily host sometimes lists only the Claude & GPT pool. An answer without
  a Gemini pool makes Bonsai ask the other host and start agy's `/usage` beside
  it (also when the API read is slow), and the first source that lists a Gemini
  pool wins. A profile no source lists one for shows its Claude & GPT pool in
  the Gemini table, marked `(Claude & GPT)`, and agy is not started for it again
  for 10 minutes. The `/usage` run is stopped, with its helper processes, after
  45 seconds.
- **Sign-in.** Bonsai saves the login when `agy` exits, whatever its exit
  status. Interrupting `agy` (Ctrl+C) after signing in keeps the login. Agy's
  first-run state is kept with the profile, so its welcome and theme screens do
  not repeat in every session. Interrupting `bonsai agent run` waits for `agy`
  to end, restores the terminal mode and cleans the session up.
- **Models.** The start dialog's models come from `agy models`. A model agy
  lists once per effort (`gemini-3.8-flash-high`, `-medium`, `-low`) is one model
  with an effort choice, passed to agy as the full ID. A model listed once has no
  effort choice.
- **Activity.** Sessions write agy's `hooks.json` into their private session home
  (never the profile or `~/.gemini`), so Bonsai sees working and idle from agy's
  own hooks. agy has no hook for permission prompts or questions, so "needs
  input" is inferred from its terminal notifications and rendered input dialogs.
  Bonsai distinguishes the numbered question chooser from command permission
  prompts using their fixed UI labels, without recording their contents.
  Notifications are turned on in
  the session's settings only (your profile's `notifications` setting is not
  changed), and is shown as inferred. See [Activity hooks](development.md#activity-hooks).

### Starting agents in the web app

Start the web app with `bonsai web`, select a worktree, choose **Start agent**,
and select your profile. The terminal uses that profile's existing settings;
enter instructions directly in it. Claude Code and Codex profiles start the same
way (`bonsai agent account add codex work`); the dialog opens on the profile's
model, permissions and effort (its **Defaults** tab). The **Agents** tab does
the same profile work in the browser; see [Agents tab](web.md#agents-tab).

Closing the dock or reloading the browser detaches without stopping the agent.
Use **Stop agent** to terminate it and reconcile profile credentials. Sessions
last only while the local API runs; restarting Bonsai does not restart agents.
Recent output is bounded to 2 MiB and may show a replay-gap notice after a long
session. Interactive PTYs require a supported Unix platform; Windows support is
not yet available. See
[development details](development.md#antigravity-web-terminals).

### Sessions shared with the TUI and CLI

The web app and the TUI use the same agent holders: each session is its own
process with a record, `agents/<id>.json` under the web home. A session record's
summary has an `origin` field: `web`, `tui` or `cli` (an empty origin counts as
`web`). The web app watches these records, so a session started in the TUI or
from the CLI appears live in the browser. `bonsai agents` lists every running
session. Its `STATE` column is the lifecycle (`running`, `exited (0)`, `failed`)
and `ACTIVITY` is what the agent is doing (`Working`, `Idle`, `Needs input
(permission)`, `Stalled`, `Running` when unknown). Sessions without hook reports
(older holders, providers without hooks) keep the output-based `working` /
`idle <duration>` guess. `bonsai agents --json` adds an `activity` object
(`state`, `reason`, `detail`, `since`, `source`) for running sessions that report
one and omits it otherwise.

The TUI's Agents tab is another client of these sessions. It watches the selected
session read-only; `enter` takes control to type, and `ctrl+]` immediately returns
input to Bonsai. Press `enter` again to take control back when another client is
writing (see [TUI Agents tab](tui-cli.md#agents-tab)).

`bonsai agents attach <id|name>` opens a terminal with pinned controls below the
agent output. Press `Ctrl+]`, release it, then `d` to detach and leave the agent
running, or `k` to request a kill. The kill prompt defaults to cancel: press `y`
to stop the agent, or `n`, `Enter` or `Esc` to cancel. These controls also work
with `--read-only` and when another client holds control. Press `Ctrl+]` then `t`
to take control, or attach with `--take` to take it immediately. An explicit
`--read-only` attachment cannot take control; detach and attach again without
that flag. `Ctrl+C` reaches the agent while controlling it. When output is piped,
`--read-only` streams output with control hints on stderr instead of a pinned
display. The separate `bonsai agents stop <id|name>` command stops a session
without asking for confirmation.

`bonsai web stop` stops the agents the web started (origin `web`) unless
`agents.keep_running` is on (see [web app](web.md)). Sessions with origin `tui` or
`cli` are left running either way. `--stop-agents` and `--keep-agents` override the
setting for one stop.

### Desktop notifications

Each session's holder can raise a desktop notification when its agent starts
waiting for you or finishes a turn. They are **off by default**. The holder sends
them (best effort: `notify-send` on Linux, `osascript` on macOS, PowerShell on
Windows), so they work with the TUI and CLI as well as the web app, and one
session never notifies twice.

Turn them on in `web.json` (`agents.notifications`) or with
`POST /api/settings/agents` (see [development](development.md#agent-hosts)):

```json
"agents": { "notifications": { "enabled": true, "needs_input": true, "turn_complete": true } }
```

`enabled` is the master switch. `needs_input` (default on) notifies when the
agent waits for a permission, question or plan. `turn_complete` (default on)
notifies when a turn finishes or fails. The toggles only matter while `enabled`
is on. A holder reads the setting when it starts, so a change applies to sessions
started afterwards.

A notification names the session, the provider, the reason and, for a permission
prompt, the tool (`Bonsai: fix login needs input`, body `claude (permission:
Bash)`). It never includes prompts, tool input or output. Starting a session,
interrupting a turn, your own keystrokes and a session ending do not notify, and
a session sends at most one notification of each type every 2 seconds.

### Activity status limits

The status comes from provider hooks, so it is a best-effort signal:

- A tool that finishes while another tool waits for permission can show the
  session as working until the next hook arrives. Pressing a key in the terminal
  corrects it.
- Hooks are ordered by the time each hook process started. After the system clock
  steps backwards (common on WSL2 and after a VM resume), a hook from that moment
  can be ignored, and the status then lags until the next one.
- Codex sessions use the `-c tui.notification*` overrides described above even
  when your profile's `config.toml` sets those keys; only your own launch args
  win.
- Hook commands are quoted for POSIX shells. On Windows the status may stay
  "Running" if the agent runs hooks through a different shell.

## Profile defaults

Each profile keeps its launch defaults: the model plus Codex `permissions` and
`effort`, Claude `permission_mode` and `effort`, or Antigravity `effort` and
`full_access`. The web start dialog opens on them and can change them for one
launch; set them for good on the profile's **Defaults** tab in the Agents tab (or
the Accounts workspace of the [redesign preview](tui-cli.md#accounts-in-the-preview))
or with `account set`. A launch option still wins over them, and defaults only apply
to sessions started afterwards.

```sh
bonsai agent account show work                       # sign-in, identity, defaults and accepted values
bonsai agent account set work model=gpt-5.5 effort=high permissions=auto
bonsai agent account set work --unset effort         # back to the provider default
bonsai agent account rename work personal
```

`account login <profile> [--auth <mode>] [--token-stdin]` signs an existing
profile in again with the provider's own login (Antigravity: `agy`, then exit
it), or switches its sign-in mode. Running sessions keep going; the profile's
cached usage is dropped.

`account remove` warns when a session of the profile may still be running (its
session record is still there); the local API refuses to remove a profile while
one of its web sessions is active.

## Usage across all profiles

`bonsai agent usage [<account>] [--refresh] [--claude-gpt] [--no-progress]`
prints the plan limits of every profile of every provider in one table. The
per-provider details (what each shows, where it is read from, caching) are in
the sections above. Providers are listed Claude, Codex, then Antigravity.

On a terminal, a run that takes longer than a third of a second shows one live
line per provider on stderr, a cell per profile and who is still being waited
on. `--no-progress` or `BONSAI_PROGRESS=0` turns it off, and piped output is
unchanged. Results are cached for 1 minute per profile; `--refresh` bypasses
the cache.
