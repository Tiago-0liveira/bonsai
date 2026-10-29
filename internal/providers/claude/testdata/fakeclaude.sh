#!/bin/sh
# Fake `claude` for tests. Put it first on PATH under the name "claude".
#   FAKE_CLAUDE_FIXTURES    directory holding the auth-status-*.json fixtures
#   FAKE_CLAUDE_VERSION     version to report (default 2.1.295)
#   FAKE_CLAUDE_EMAIL       email written by `auth login`
#   FAKE_CLAUDE_LOGIN_FAIL  1 = `auth login` exits 1
#   FAKE_CLAUDE_LOGOUT_FAIL 1 = `auth logout` exits 1
#   FAKE_CLAUDE_LOG         file the auth subcommands append a call line to
#   FAKE_CLAUDE_CAPTURE     directory a structured call (-p) writes argv, env, stdin, cwd to
#   FAKE_CLAUDE_REPLY       file whose content a structured call prints
#   FAKE_CLAUDE_EXIT        exit status of a structured call (default 0)
#   FAKE_CLAUDE_SPAWN_FIFO  a structured call starts a long sleeper and writes its pid here, then waits
# Interactive sessions understand `HOOK <Event> <json>`: like Claude Code, they
# run the hook command found for <Event> in the --settings JSON they were
# started with, the payload on its stdin. A real SessionStart fires on startup.
cfg="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
log() {
	if [ -n "$FAKE_CLAUDE_LOG" ]; then
		printf '%s\n' "$*" >>"$FAKE_CLAUDE_LOG"
	fi
}

case "$1" in
--version)
	echo "${FAKE_CLAUDE_VERSION:-2.1.295} (Claude Code)"
	exit 0
	;;
auth)
	case "$2" in
	status)
		if [ -f "$cfg/.credentials.json" ]; then
			cat "$FAKE_CLAUDE_FIXTURES/auth-status-claudeai.json"
			exit 0
		elif [ -n "$CLAUDE_CODE_OAUTH_TOKEN" ]; then
			cat "$FAKE_CLAUDE_FIXTURES/auth-status-oauth-token.json"
			exit 0
		fi
		cat "$FAKE_CLAUDE_FIXTURES/auth-status-logged-out.json"
		exit 1
		;;
	login)
		log "login config=$cfg"
		if [ "$FAKE_CLAUDE_LOGIN_FAIL" = 1 ]; then
			echo "login failed" >&2
			exit 1
		fi
		mkdir -p "$cfg"
		printf '{"claudeAiOauth":{"accessToken":"fake"}}' >"$cfg/.credentials.json"
		printf '{"oauthAccount":{"emailAddress":"%s"}}' "${FAKE_CLAUDE_EMAIL:-user@example.com}" >"$cfg/.claude.json"
		exit 0
		;;
	logout)
		log "logout config=$cfg home=$HOME"
		if [ "$FAKE_CLAUDE_LOGOUT_FAIL" = 1 ]; then
			exit 1
		fi
		rm -f "$cfg/.credentials.json"
		exit 0
		;;
	esac
	;;
-p)
	if [ -n "$FAKE_CLAUDE_CAPTURE" ]; then
		for arg in "$@"; do
			printf '%s\n' "$arg"
		done >"$FAKE_CLAUDE_CAPTURE/argv"
		env >"$FAKE_CLAUDE_CAPTURE/env"
		pwd >"$FAKE_CLAUDE_CAPTURE/cwd"
		cat >"$FAKE_CLAUDE_CAPTURE/stdin"
	fi
	if [ -n "$FAKE_CLAUDE_SPAWN_FIFO" ]; then
		sleep 300 &
		echo $! >"$FAKE_CLAUDE_SPAWN_FIFO"
		wait
	fi
	[ -n "$FAKE_CLAUDE_REPLY" ] && cat "$FAKE_CLAUDE_REPLY"
	exit "${FAKE_CLAUDE_EXIT:-0}"
	;;
setup-token)
	echo "sk-ant-oat01-FAKEFAKEFAKEFAKEFAKEFAKEFAKE"
	exit 0
	;;
esac

for arg in "$@"; do
	printf 'ARG:%s\n' "$arg"
done
printf 'CONFIG_DIR=%s\n' "$CLAUDE_CONFIG_DIR"
printf 'HOME=%s\n' "$HOME"
printf 'SESSION=%s\n' "$BONSAI_AGENT_SESSION_ID"
printf 'HAS_API_KEY=%s\n' "$([ -n "${ANTHROPIC_API_KEY+x}" ] && echo 1 || echo 0)"
printf 'HAS_OAUTH_TOKEN=%s\n' "$([ -n "${CLAUDE_CODE_OAUTH_TOKEN+x}" ] && echo 1 || echo 0)"
printf 'CLAUDECODE=%s\n' "${CLAUDECODE-unset}"
settings=""
prev=""
for arg in "$@"; do
	if [ "$prev" = --settings ]; then
		settings=$arg
	fi
	prev=$arg
done
fire_hook() {
	[ -n "$settings" ] || return 0
	cmd=$(printf '%s' "$settings" | grep -o '"command":"[^"]*"' | grep " $1\"\$" | head -n 1 | sed 's/^"command":"//; s/"$//')
	[ -n "$cmd" ] || return 0
	printf '%s' "$2" | sh -c "$cmd"
}
printf 'HOOKS=%s\n' "$([ -n "$settings" ] && echo 1 || echo 0)"
printf 'READY\n'
fire_hook SessionStart '{"source":"startup"}'
while IFS= read -r l; do
	case "$l" in
	"HOOK "*)
		rest=${l#HOOK }
		event=${rest%% *}
		payload=${rest#"$event"}
		fire_hook "$event" "${payload# }"
		printf 'HOOKED:%s\n' "$event"
		;;
	*)
		printf 'REPLY:%s\n' "$l"
		;;
	esac
done
