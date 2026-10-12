# Login flow re-check (Phase 6)

Date: 2026-10-12. Host: WSL2 (Linux 6.18, `WSL_DISTRO_NAME` set). Versions:
Claude Code 2.1.296, codex-cli 0.162.1, agy on `PATH` (no version flag).

The plan's assumptions come from the maintainer's manual run of all three
logins in the web terminal tab the same day (outputs not recorded). This
re-check ran in a scratch `CLAUDE_CONFIG_DIR` / `CODEX_HOME` and did **not**
complete a real sign-in (no account, no network in an implementing session).

## Checked here

| Assumption | Result |
| --- | --- |
| `claude auth login` exists and needs no extra flags | Yes: `claude auth login [--claudeai\|--console] [--email] [--sso]`; claude.ai is the default. |
| `claude auth status` exits non-zero when logged out | Yes: exit 1 with `{"loggedIn": false, ...}` JSON on stdout. |
| `claude setup-token` needs no other input | Help lists no options besides `-h`. Printing the token once is not re-verified here (needs a subscription login). |
| `codex login` / `--device-auth` / `--with-api-key` | All present in `codex login --help` (0.162.1); `--with-access-token` also exists (unused). |
| `codex login status` exits non-zero when logged out | Yes: exit 1, `Not logged in`. |
| Codex in a `/tmp` home | Prints `WARNING: proceeding, even though we could not create PATH aliases ...`; harmless, and profile homes are not under `/tmp`. |
| agy has no login subcommand | Yes: `agy --help` lists none; sign-in happens in the TUI, so setup ends when agy exits (`CaptureSetup` is the check). |

## Not re-checked (still the maintainer's report)

- Claude and Codex print a URL or device code that works without a local browser.
- `claude auth login` and `codex login` exit 0 on success and non-zero on cancel.
- Resizing the terminal mid-login does not break the flow.
- Signing a profile in again while one of its sessions runs: assumed safe.
  Codex and Claude replace the profile's stored login; running sessions keep
  their in-memory tokens and read the new file on their next refresh. Antigravity
  sessions keep their materialized copy; the vault generation bump keeps their
  older tokens from overwriting the new login when they end. Phase 6 therefore
  allows sign-in again with active sessions.

No contradiction with the plan was found. Phase 7 should repeat the unchecked
items with a real login through the job PTY.

## Antigravity note shown in the web

"Sign in when agy starts, then exit agy to save the profile."
