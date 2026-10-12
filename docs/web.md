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
without stopping the app. Stop terminates the API and supervised tunnel,
including active agent sessions; repository processes have their own daemons.
The idle web supervisor retains logs and exits according to its idle policy.

## Settings and runtime files

| Platform | Settings directory | Runtime directory |
| --- | --- | --- |
| Linux | `$XDG_CONFIG_HOME/bonsai` or `~/.config/bonsai` | `$XDG_STATE_HOME/bonsai/web` or `~/.local/state/bonsai/web` |
| macOS | `~/Library/Application Support/bonsai` | `~/Library/Application Support/bonsai/web` |
| Windows | `%AppData%\bonsai` | `%LocalAppData%\bonsai\web` |

`web.json` holds ports, browser preferences and update choices. Project folders
and selected repositories live in `project-roots.json` and
`project-roots.json.repositories.json`. Environment editor preferences live in
`project-roots.json.editors.json`. Machine-managed hook state lives in `web-state.json`.
The webhook secret is in its own private `web-webhook-secret` file. Runtime
supervisor records and logs are under `.bonsai` in the runtime directory;
server-side warm snapshots are under `warm-start`.

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
