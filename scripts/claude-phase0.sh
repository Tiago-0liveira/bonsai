#!/usr/bin/env bash
# Phase 0 of docs/claude-provider-plan.md — verify Claude Code CLI assumptions on a real machine.
#
# Run in your normal terminal (Linux / WSL / macOS):   bash scripts/claude-phase0.sh
#
# What it does:
#   * Uses throw-away CLAUDE_CONFIG_DIRs under a temp dir. Your real ~/.claude is only READ
#     (step 8 copies settings.json from it); nothing in it is modified.
#   * Runs every `claude` call with a scrubbed environment (no inherited CLAUDE*/ANTHROPIC_* vars).
#   * Asks you to approve two browser flows: `claude auth login` and `claude setup-token`.
#   * Makes a handful of tiny Haiku requests (a few cents of quota at most).
#   * Writes REDACTED results to ./claude-phase0-results/ — no tokens, emails or UUIDs.
#   * Logs out of the temporary login and deletes the temp dirs at the end.
# The setup-token token stays valid for a year; revoke it from your Claude account settings
# afterwards if you don't want to keep it.

set -u
OUT="$(pwd)/claude-phase0-results"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/bonsai-phase0.XXXXXX")"
chmod 700 "$WORK"
A="$WORK/cfg-login"; B="$WORK/cfg-token"; C="$WORK/cfg-seed"; CWD="$WORK/cwd"
TOKEN_FILE="$WORK/token"; HDR_FILE="$WORK/hdr"
mkdir -p "$OUT" "$A" "$B" "$C" "$CWD"
SUMMARY="$OUT/summary.txt"; : > "$SUMMARY"

CLAUDE_BIN="$(command -v claude || true)"
PY="$(command -v python3 || true)"
[ -n "$CLAUDE_BIN" ] || { echo "claude not found on PATH"; exit 1; }
[ -n "$PY" ] || { echo "python3 is required"; exit 1; }
command -v curl >/dev/null || { echo "curl is required"; exit 1; }

cleanup() {
  if [ -f "$A/.credentials.json" ] || [ "$(uname)" = Darwin ]; then
    run CLAUDE_CONFIG_DIR="$A" "$CLAUDE_BIN" auth logout >/dev/null 2>&1
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT

log() { printf '%s\n' "$*" | tee -a "$SUMMARY"; }
step() { printf '\n== %s\n' "$*" | tee -a "$SUMMARY"; }

# Clean environment: keep only what a normal launch needs (+ proxy/CA/browser plumbing).
run() {
  local keep=(PATH="$PATH" HOME="$HOME" TERM="${TERM:-xterm-256color}" LANG="${LANG:-C.UTF-8}")
  local v
  for v in USER LOGNAME SHELL DISPLAY WAYLAND_DISPLAY BROWSER WSL_DISTRO_NAME WSL_INTEROP \
           HTTPS_PROXY HTTP_PROXY NO_PROXY https_proxy http_proxy no_proxy \
           NODE_EXTRA_CA_CERTS SSL_CERT_FILE TMPDIR XDG_RUNTIME_DIR; do
    [ -n "${!v:-}" ] && keep+=("$v=${!v}")
  done
  env -i "${keep[@]}" "$@"
}
# Same, but injects the setup-token from a file so it never appears in argv.
run_tok() { run TOKEN_FILE="$TOKEN_FILE" bash -c 'export CLAUDE_CODE_OAUTH_TOKEN="$(cat "$TOKEN_FILE")"; unset TOKEN_FILE; exec env "$@"' _ "$@"; }

# Redact a JSON document: strings that look like secrets/PII are replaced; numbers/bools kept.
redact() { "$PY" - "$@" <<'PYEOF'
import json, re, sys
SAFE = {"authMethod","apiProvider","subscriptionType","kind","window","period","apiKeySource"}
PAT = re.compile(r"sk-ant-|@[\w.-]+\.\w{2,}|[0-9a-f]{8}-[0-9a-f]{4}-|^[A-Za-z0-9_\-\.]{40,}$|/home/|/Users/|C:\\\\", re.I)
def r(v, k=None):
    if isinstance(v, dict): return {kk: r(vv, kk) for kk, vv in v.items()}
    if isinstance(v, list): return [r(x, k) for x in v]
    if isinstance(v, str) and k not in SAFE and PAT.search(v): return "<redacted>"
    return v
src, dst = sys.argv[1], sys.argv[2]
try: data = json.load(open(src))
except Exception as e:
    open(dst, "w").write(json.dumps({"_unparseable": True, "_error": type(e).__name__}) + "\n"); sys.exit()
json.dump(r(data), open(dst, "w"), indent=2); open(dst, "a").write("\n")
PYEOF
}
# Structure only (key names + value types), never values.
shape() { "$PY" - "$@" <<'PYEOF'
import json, sys
def s(v, d=0):
    if isinstance(v, dict): return {k: s(x, d+1) for k, x in v.items()} if d < 3 else "object"
    if isinstance(v, list): return [s(v[0], d+1)] if v else []
    return type(v).__name__
src, dst = sys.argv[1], sys.argv[2]
try: json.dump(s(json.load(open(src))), open(dst, "w"), indent=2)
except Exception as e: open(dst, "w").write('{"_unreadable": "%s"}' % type(e).__name__)
PYEOF
}
ask_enter() { printf '\n>>> %s\n    Press Enter to continue (Ctrl+C to abort)... ' "$1"; read -r _; }

# ---------------------------------------------------------------------------------------------
step "1. Version and flags"
VER="$(run "$CLAUDE_BIN" --version 2>&1)"; log "claude --version: $VER"
run "$CLAUDE_BIN" --help 2>/dev/null | grep -oE -- '--(model|permission-mode|effort|name|session-id|settings|dangerously-skip-permissions)\b' \
  | sort -u | tr '\n' ' ' | sed 's/^/flags present: /' | tee -a "$SUMMARY"; echo | tee -a "$SUMMARY"
log "platform: $(uname -s) $(uname -r)"

step "2. auth status in an empty config dir (expect logged out)"
run CLAUDE_CONFIG_DIR="$A" "$CLAUDE_BIN" auth status > "$WORK/s1.json" 2>/dev/null; log "exit code: $?"
redact "$WORK/s1.json" "$OUT/auth-status-logged-out.json"
log "files created in empty config dir: $(cd "$A" && ls -A | tr '\n' ' ')"

step "3. auth login into the temporary config dir"
ask_enter "A browser login for 'claude auth login' starts next. Approve it with the account you want to test."
run CLAUDE_CONFIG_DIR="$A" "$CLAUDE_BIN" auth login
log "auth login exit code: $?"
run CLAUDE_CONFIG_DIR="$A" "$CLAUDE_BIN" auth status > "$WORK/s2.json" 2>/dev/null; log "auth status exit code: $?"
redact "$WORK/s2.json" "$OUT/auth-status-claudeai.json"

step "4. Config dir layout after login (key names only)"
log "config dir entries: $(cd "$A" && ls -A | tr '\n' ' ')"
[ -f "$A/.claude.json" ] && shape "$A/.claude.json" "$OUT/claude-json.shape.json" && log "wrote claude-json.shape.json"
if [ -f "$A/.credentials.json" ]; then
  shape "$A/.credentials.json" "$OUT/credentials.shape.json"; log "credentials stored in file: yes (mode $(stat -c %a "$A/.credentials.json" 2>/dev/null || stat -f %Lp "$A/.credentials.json"))"
else
  log "credentials stored in file: no (expected on macOS: Keychain)"
fi
"$PY" - "$A/.claude.json" <<'PYEOF' | tee -a "$SUMMARY"
import json, sys
try: d = json.load(open(sys.argv[1]))
except Exception: print("oauthAccount: unreadable"); sys.exit()
oa = d.get("oauthAccount")
print("oauthAccount keys:", sorted(oa.keys()) if isinstance(oa, dict) else oa)
print("has emailAddress:", isinstance(oa, dict) and "@" in str(oa.get("emailAddress", "")))
PYEOF

step "5. Two concurrent sessions on the same login config dir"
for n in 1 2; do
  ( cd "$CWD" && run CLAUDE_CONFIG_DIR="$A" "$CLAUDE_BIN" -p --model haiku "Reply with exactly: SESSION-$n" > "$WORK/c$n.out" 2>&1; echo $? > "$WORK/c$n.rc" ) &
done; wait
for n in 1 2; do log "session $n: exit $(cat "$WORK/c$n.rc"), output: $(head -c 80 "$WORK/c$n.out" | tr '\n' ' ')"; done
run CLAUDE_CONFIG_DIR="$A" "$CLAUDE_BIN" auth status >/dev/null 2>&1; log "still logged in afterwards: exit $?"

step "6. Long-lived token (claude setup-token)"
ask_enter "A browser approval for 'claude setup-token' starts next. It prints a token; you'll paste it right after."
run CLAUDE_CONFIG_DIR="$B" "$CLAUDE_BIN" setup-token
printf '\nPaste the token printed above (input hidden), then Enter: '
IFS= read -rs TOKEN; echo
umask 077; printf '%s' "$TOKEN" > "$TOKEN_FILE"
case "$TOKEN" in sk-ant-oat01-*) FMT=yes;; *) FMT=no;; esac
log "token has expected oat01 prefix: $FMT  length: ${#TOKEN}"
unset TOKEN
( cd "$CWD" && run_tok CLAUDE_CONFIG_DIR="$B" "$CLAUDE_BIN" -p --model haiku "Reply with exactly: TOKEN-OK" > "$WORK/t.out" 2>&1 ); log "token -p exit $?, output: $(head -c 80 "$WORK/t.out" | tr '\n' ' ')"
run_tok CLAUDE_CONFIG_DIR="$B" "$CLAUDE_BIN" auth status > "$WORK/s3.json" 2>/dev/null; log "auth status with token: exit $?"
redact "$WORK/s3.json" "$OUT/auth-status-oauth-token.json"
log "files written to token config dir: $(cd "$B" && ls -A | tr '\n' ' ')"
printf 'Authorization: Bearer %s\nanthropic-beta: oauth-2025-04-20\n' "$(cat "$TOKEN_FILE")" > "$HDR_FILE"
CODE="$(curl -sS -o "$WORK/u-token.json" -w '%{http_code}' -H @"$HDR_FILE" https://api.anthropic.com/api/oauth/usage)"
log "usage endpoint with setup-token: HTTP $CODE"
redact "$WORK/u-token.json" "$OUT/usage-response-setup-token.json"

step "7. Usage endpoint with the login access token"
if [ -f "$A/.credentials.json" ]; then
  "$PY" - "$A/.credentials.json" "$HDR_FILE" <<'PYEOF'
import json, os, sys
d = json.load(open(sys.argv[1])); t = (d.get("claudeAiOauth") or {}).get("accessToken", "")
fd = os.open(sys.argv[2], os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
os.write(fd, ("Authorization: Bearer %s\nanthropic-beta: oauth-2025-04-20\n" % t).encode()); os.close(fd)
PYEOF
  CODE="$(curl -sS -o "$WORK/u-login.json" -w '%{http_code}' -H @"$HDR_FILE" https://api.anthropic.com/api/oauth/usage)"
  log "usage endpoint with login token: HTTP $CODE"
  redact "$WORK/u-login.json" "$OUT/usage-response.json"
else
  log "skipped (no .credentials.json file)"
fi
rm -f "$HDR_FILE"

step "8. Seeding: do enabledPlugins reinstall in a fresh config dir?"
SRC="$HOME/.claude/settings.json"
if [ -f "$SRC" ]; then
  cp "$SRC" "$C/settings.json"
  log "enabledPlugins in your settings.json: $("$PY" -c 'import json,sys; print(len(json.load(open(sys.argv[1])).get("enabledPlugins") or {}))' "$SRC" 2>/dev/null || echo unreadable)"
  ( cd "$CWD" && run_tok CLAUDE_CONFIG_DIR="$C" "$CLAUDE_BIN" -p --model haiku "Reply with exactly: SEED-OK" > "$WORK/seed.out" 2>&1 ); log "seeded -p exit $?, output: $(head -c 80 "$WORK/seed.out" | tr '\n' ' ')"
  log "seeded config dir entries afterwards: $(cd "$C" && ls -A | tr '\n' ' ')"
  [ -d "$C/plugins" ] && log "plugins dir entries: $(cd "$C/plugins" && ls -A | tr '\n' ' ')"
else
  log "skipped (no ~/.claude/settings.json)"
fi

step "9. Does CLAUDE_CODE_SUBPROCESS_ENV_SCRUB hide the token from Bash subprocesses?"
Q='Run this bash command and reply with only the number it prints: printenv CLAUDE_CODE_OAUTH_TOKEN | wc -c'
( cd "$CWD" && run_tok CLAUDE_CONFIG_DIR="$B" "$CLAUDE_BIN" -p --model haiku --allowedTools Bash "$Q" > "$WORK/e0.out" 2>&1 )
( cd "$CWD" && run_tok CLAUDE_CONFIG_DIR="$B" CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1 "$CLAUDE_BIN" -p --model haiku --allowedTools Bash "$Q" > "$WORK/e1.out" 2>&1 )
log "token bytes visible to Bash without scrub: $(head -c 40 "$WORK/e0.out" | tr '\n' ' ')"
log "token bytes visible to Bash with scrub=1:  $(head -c 40 "$WORK/e1.out" | tr '\n' ' ')"

if [ "$(uname)" = Darwin ]; then
  step "10. macOS Keychain entries per config dir"
  security dump-keychain 2>/dev/null | grep -oE '"Claude Code-credentials[^"]*"' | sort -u | sed 's/[0-9a-f]\{6,\}/<hash>/g' | tee -a "$SUMMARY"
fi

# ---------------------------------------------------------------------------------------------
step "Leak check on results"
if grep -rEl 'sk-ant-|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-' "$OUT"; then
  log "WARNING: the files listed above may contain secrets/PII — inspect before sharing."
else
  log "no tokens, emails or UUIDs found in $OUT"
fi
log "done. Share the folder: $OUT"
