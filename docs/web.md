# Running the local web app

Install a GitHub release archive, then run `bonsai web` from any directory.
Release binaries contain the complete UI. Git 2.35+ is required; `gh` is optional
for local work and supplies GitHub access when logged in.

In an interactive terminal, the first run opens a wizard for project folders,
browser auto-open and update mode. Nothing is saved until Review is applied.
`bonsai web setup` edits those choices later. Cancelling the first wizard writes
nothing; `bonsai web --no-setup` uses saved or default settings. Scripts can use
`bonsai web --no-open --no-setup`.

The app prints and opens `http://127.0.0.1:<port>/app` (default 7001).
`http://localhost:<port>/app` also works. Deep links and refreshes use the same
local server. The browser connects automatically, without a network-permission
prompt. `--port N` overrides the current launch; setup changes the saved port.

```sh
bonsai web status
bonsai web open
bonsai web setup
bonsai web doctor
bonsai web logs api -n 200
bonsai web logs tunnel -f
bonsai web restart api
bonsai web restart tunnel
bonsai web --attach
bonsai web stop
```

Doctor prints the same checks as setup and exits 1 on failures. Missing/logged
out `gh` is a warning: local work still works. The attach/log views detach
without stopping the app. Stop terminates the API and supervised tunnel;
repository processes have their own daemons. Agent sessions are stopped too
unless **Settings → Agents → Keep agents running when Bonsai web stops** is on
(see [Agent sessions](#agent-sessions)). The idle web supervisor retains logs
and exits according to its idle policy.

## Agent sessions

Each agent runs in its own background process, not inside the API, so
`bonsai web restart api`, an upgrade or an API crash never stops it: the next
API picks it up, and the browser terminal continues where it was.

From any directory, whether Bonsai web runs or not:

```sh
bonsai agents                      # running sessions
bonsai agents --all --json         # ended ones too, as JSON
bonsai agents attach 3f9c2a1d      # by ID prefix or name
bonsai agents attach fix-login --take
bonsai agents attach fix-login --read-only
bonsai agents stop fix-login
```

The list shows `ID  NAME  PROVIDER  PROFILE  WORKTREE  STATE  ACTIVITY  UP
CLIENTS`: ACTIVITY is `working` while output arrived in the last 5 seconds,
else how long the terminal has been quiet, and CLIENTS the attached terminals
(`web*,cli`; `*` writes). A name or prefix that matches several sessions lists
them; a single running one wins.

`attach` replays the session's retained output and connects this terminal.
One client types at a time: the first to attach, until it leaves or another
takes control. In the terminal, **Ctrl-] d** detaches (the agent keeps
running), **Ctrl-] t** takes control, and **Ctrl-] Ctrl-]** sends a Ctrl-].
`--take` takes control on connect; `--read-only` only watches and never
writes. Status lines say who has control (`control taken by web`) and when the
agent exits.

`bonsai web stop` stops running agents unless **Settings → Agents → Keep
agents running when Bonsai web stops** is on (`agents.keep_running` in
`web.json`, off by default). With it off and agents running, a stop in a
terminal lists them and asks first:

```text
1 agent is still running and will be stopped. Stop anyway? [y/N]
(enable Settings → Agents → Keep agents running, or pass --keep-agents)
```

`N` or Ctrl-C stops nothing. In scripts (no terminal) the warning goes to
stderr and the stop proceeds. `bonsai web stop --stop-agents` skips the
question; `--keep-agents` keeps them for this stop only. In `bonsai web
--attach`, `X` warns the same way and stops on a second `X`. With the setting
on, the stop prints how many agents still run. `bonsai web status` shows the
running agents and what a stop does with them. While the setting is off and
agents run, Settings and the Agents tab say they will be stopped.

## Settings and runtime files

| Platform | Settings directory | Runtime directory |
| --- | --- | --- |
| Linux | `$XDG_CONFIG_HOME/bonsai` or `~/.config/bonsai` | `$XDG_STATE_HOME/bonsai/web` or `~/.local/state/bonsai/web` |
| macOS | `~/Library/Application Support/bonsai` | `~/Library/Application Support/bonsai/web` |
| Windows | `%AppData%\bonsai` | `%LocalAppData%\bonsai\web` |

`web.json` holds ports, browser preferences, update choices and whether agents
keep running when Bonsai web stops. Project folders
and selected repositories live in `project-roots.json` and
`project-roots.json.repositories.json`. Environment editor preferences live in
`project-roots.json.editors.json`. Machine-managed hook state lives in `web-state.json`.
The webhook secret is in its own private `web-webhook-secret` file. Runtime
supervisor records and logs are under `.bonsai` in the runtime directory;
server-side warm snapshots are under `warm-start`, and each agent session's
record is under `agents`.

Settings schema remains version 1. Existing `interfaces.local` and
`interfaces.hosted` fields are ignored and disappear on the next successful
save; ports, folders, browser and update preferences survive. `bonsai serve`
has been removed immediately, and all `serve.*` YAML keys are inert.
`BONSAI_FRONTEND_ORIGIN` no longer affects access or browser opening.

## Opening files in an editor

Choose **Settings → Environment editor** to set a default editor for the
environment running Bonsai. With no default, clicking a file in the Inspector's
Files tab asks you to choose an installed editor, saves that choice, and opens
the file. Files open relative to their selected worktree, including when you
switch worktrees while the chooser is open.

Discovery runs in the environment where Bonsai runs and offers VS Code, VS Code
Insiders, Cursor, Zed, Sublime Text, Kate, Gedit and GNOME Text Editor when they
are installed. Terminal-only editors and generic system file handlers are not
offered. Per platform, a command is looked up on PATH first and then in the
usual install locations, so editors still appear when a background service has
a short PATH:

| Platform | Also searched |
| --- | --- |
| Linux | `~/.local/bin`, `/usr/local/bin`, `/snap/bin`; `/usr/share/code`, `/opt` and `~/.local/zed.app` installs |
| macOS | `/opt/homebrew/bin`, `/usr/local/bin`; app bundles in `/Applications` and `~/Applications` (`Visual Studio Code`, `Cursor`, `Zed`, `Sublime Text`) |
| Windows | `.exe` files on PATH; the `code.cmd` and `cursor.cmd` shims resolve to `Code.exe` / `Cursor.exe`; `%LOCALAPPDATA%\Programs`, `%ProgramFiles%` and `%ProgramFiles(x86)%` |

Each platform only runs its own executables: a Linux or macOS host never picks
up Windows programs, and Windows never runs shell scripts. WSL is the one
exception, described next.

### Opening a worktree folder

Right-click a worktree on the canvas and choose **Open in editor**, or use the
folder button in the Inspector header. The worktree directory opens in the
default editor. VS Code, Cursor, Zed and Sublime Text can open folders; Kate,
Gedit and GNOME Text Editor cannot. If the default cannot, Bonsai asks which
folder-capable editor to use for that one launch and leaves the default alone.
With no default, the choice is saved like it is for files.

### WSL and Windows editors

On WSL, Linux editors installed inside the distribution are listed as usual.
Bonsai also offers **Visual Studio Code (Windows)**, **Visual Studio Code
Insiders (Windows)**, **Cursor (Windows)** and **Zed (Windows)** when their
Windows installation provides its WSL-aware launcher script (`bin/code`,
`bin/cursor`, `bin/zed`). These scripts forward to the Windows editor and tell
it which distribution owns the file: VS Code and Cursor open a Remote-WSL
window (needs the *WSL* extension), and Zed runs
`zed.exe --wsl <user>@<distro>`. Bonsai passes the Linux path unchanged and lets
the vendor script do the translation.

The launcher is looked up on the Windows drive entries of PATH first, then in
the standard install folders (`Users/*/AppData/Local/Programs` and
`Program Files`), so it also works when WSL does not append the Windows PATH.
Only shell scripts that mention `WSL_DISTRO_NAME` are accepted; bare `.exe`
files, `.cmd` shims and symlinks leaving the Windows drive are not. Windows
editors without such a launcher (Sublime Text, for example) need a Linux
install inside the distribution. Windows interop must be enabled
(`[interop] enabled=true` in `wsl.conf`, the default). GUI Linux editors
instead need a graphical session such as WSLg.

Use **Refresh editors** after installing or removing an editor. Restart Bonsai
if you changed its PATH. A missing saved editor prompts you to choose another;
Bonsai never substitutes a different editor and never falls back to a system
file handler. Launches use separate arguments, without shell interpolation, and
reject paths or symlinks outside the worktree. Native launcher scripts resolve
helpers through the environment's PATH with Windows drive mounts excluded on
WSL; Windows editor launchers keep the full PATH, as their scripts expect. The opening notice acknowledges the launch request; it does not assert
that a GUI window was displayed. Immediate process failures report the editor's
error, and later failures are retained in the local API log.

## Agents tab

**Agents** in the top bar lists every agent provider Bonsai knows (Claude Code,
Codex, Antigravity), whether its CLI is installed, and each profile with its
sign-in state, identity, warnings, plan usage and sessions in every project.
Providers are listed Claude, Codex, then Antigravity, and each can be collapsed
to hide its profiles (the choice is remembered in the browser). The strip on top
sums up the fleet (profiles signed in, sessions running, the lowest headroom and
the next reset), shows each provider's sign-ins, its 5-hour and weekly windows
pooled over all of its profiles, its running sessions, says how old the usage
reading is and re-reads it on **Check usage**. Select a profile for its tabs:

- **Overview**: sign-in method, identity, status, saved defaults, **Start
  agent** (the start dialog opens on this profile), **Sign in again** and
  **Remove profile**.
- **Usage**: plan limits with their resets. Usage is cached by the API;
  **Refresh** asks the provider again, at most every 30 seconds per profile.
- **Sessions**: running and recent sessions with the model, effort and
  permissions they were started with; open the terminal, stop the agent or show
  it on the canvas.
- **Defaults**: the model, permissions and effort new sessions of the profile
  start with. The start dialog uses the same controls and opens on them. Each
  control opens on the profile's saved choice, or where it saved none on the
  provider's default (the default model, that model's default effort or the
  middle of the levels it offers, the usual permission), tagged "default" and
  one click away; a choice left on it is not saved. The effort slider warms up
  past its middle, the thumb turns into a flame, and xhigh and above jitter.
  Running sessions are not changed.

**Add profile** creates a profile without a terminal: choose the provider, a
name and how to sign in (browser sign-in, device code, or a pasted API key or
token), and whether to copy your own settings (for example `~/.codex`) into it.
The provider's login runs in a terminal inside the dialog, so you can open its
link, read its code or answer its questions there. Closing the dialog while it
runs asks first; canceling the sign-in removes the new profile, and a reload
reattaches to a sign-in still running. A pasted key or token is
sent once and never shown, stored by Bonsai or logged. Removing a profile is
refused while one of its sessions runs; the dialog offers to stop them first.
Signing in from the browser needs a terminal, which native Windows does not
have yet: there the dialog shows the `bonsai agent account` command to run
instead. Profiles added or changed with the CLI show up without a reload.

## Updates and exposure

Standard mode needs no tunnel. It checks visible GitHub projects every 30
seconds (15 seconds while CI runs), and background projects every 5 minutes.
REST conditional requests and rate-limit backoff limit network work.

Live mode accepts Cloudflare quick/named tunnels, ngrok, Tailscale Funnel,
external URLs and custom commands. Setup explains prerequisites and reviews
external webhook/tunnel changes before applying them. Only the separate
webhook receiver on port 7002 may be exposed. The API and browser stay local.
Healthy delivery slows polling to a 10-minute safety interval; failed delivery
or a stopped tunnel restores ordinary polling. Pending or paused Live badges
show the reason; doctor and tunnel logs provide the same diagnosis.

## Recovery

If startup fails, read the printed fix and `bonsai web logs api`. For an occupied
port, stop the owning program or choose `--port N`. Invalid settings report the
file: fix it or delete it deliberately, then run setup. Bonsai never silently
replaces an unreadable settings file with empty defaults.

`bonsai web restart api` recovers a failed API. After installing another version,
run `bonsai web` again: it replaces a running older API and any legacy stack
that allowed an additional browser origin. If an incompatible supervisor remains,
`bonsai web stop` clears its stack before the next start.

If browser opening fails, use the printed local URL. A UI placeholder means
this is an untagged Go build: install a release archive or build the frontend
and rebuild with `-tags embedui`. A failed lazy chunk offers **Reload app**;
restarting from the current binary keeps UI and API versions together.
Warm-start cache failure causes a cold start and preserves settings. Browser
preference errors have an explicit recovery action and keep the app usable.

See [security](git-backend.md), [development](development.md) and
[release validation](releasing.md).
