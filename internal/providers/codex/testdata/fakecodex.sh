#!/bin/sh
# Fake `codex` for tests. Put it first on PATH under the name "codex".
#   FAKE_CODEX_VERSION      version to report (default 0.162.1)
#   FAKE_CODEX_EMAIL        email in the id_token `login` writes
#   FAKE_CODEX_PLAN         plan in that id_token (default plus)
#   FAKE_CODEX_ACCOUNT      ChatGPT account ID in that id_token (default acct-fake)
#   FAKE_CODEX_LOGIN_FAIL   1 = `login` exits 1 without writing auth.json
#   FAKE_CODEX_LOGOUT_FAIL  1 = `logout` exits 1
#   FAKE_CODEX_STATUS_SLEEP seconds `login status` sleeps first
#   FAKE_CODEX_LOG          file the login/logout commands append a call line to
home="${CODEX_HOME:-$HOME/.codex}"
log() {
	if [ -n "$FAKE_CODEX_LOG" ]; then
		printf '%s\n' "$*" >>"$FAKE_CODEX_LOG"
	fi
}
b64url() {
	printf '%s' "$1" | base64 | tr '+/' '-_' | tr -d '=\n'
}

# Global -c options may precede a subcommand.
store=default
sub=
config=0
for arg in "$@"; do
	if [ $config = 1 ]; then
		config=0
		case "$arg" in cli_auth_credentials_store=*) store="${arg#*=}" ;; esac
		continue
	fi
	case "$arg" in
	-c) config=1 ;;
	--) break ;;
	-*) ;;
	*)
		sub="$arg"
		break
		;;
	esac
done
has() {
	want="$1"
	shift
	for a in "$@"; do
		[ "$a" = "$want" ] && return 0
	done
	return 1
}

if [ "$1" = --version ]; then
	echo "codex-cli ${FAKE_CODEX_VERSION:-0.162.1}"
	exit 0
fi

case "$sub" in
login)
	if has status "$@"; then
		[ -n "$FAKE_CODEX_STATUS_SLEEP" ] && sleep "$FAKE_CODEX_STATUS_SLEEP"
		if [ -f "$home/auth.json" ]; then
			echo "Logged in using ChatGPT"
			exit 0
		fi
		echo "Not logged in"
		exit 1
	fi
	if has --with-api-key "$@"; then
		IFS= read -r key
		log "login api-key home=$home store=$store"
		mkdir -p "$home"
		printf '{"auth_mode":"apikey","OPENAI_API_KEY":"%s"}' "$key" >"$home/auth.json"
		exit 0
	fi
	mode=browser
	has --device-auth "$@" && mode=device
	log "login $mode home=$home store=$store"
	if [ "$FAKE_CODEX_LOGIN_FAIL" = 1 ]; then
		echo "login failed" >&2
		exit 1
	fi
	header=$(b64url '{"alg":"none","typ":"JWT"}')
	payload=$(b64url "{\"email\":\"${FAKE_CODEX_EMAIL:-user@example.com}\",\"https://api.openai.com/auth\":{\"chatgpt_plan_type\":\"${FAKE_CODEX_PLAN:-plus}\",\"chatgpt_account_id\":\"${FAKE_CODEX_ACCOUNT:-acct-fake}\"}}")
	mkdir -p "$home"
	printf '{"auth_mode":"chatgpt","OPENAI_API_KEY":null,"tokens":{"id_token":"%s.%s.sig","access_token":"fake-access","refresh_token":"fake-refresh","account_id":"%s"},"last_refresh":"2026-10-11T00:00:00Z"}' \
		"$header" "$payload" "${FAKE_CODEX_ACCOUNT:-acct-fake}" >"$home/auth.json"
	exit 0
	;;
logout)
	log "logout home=$home store=$store"
	if [ "$FAKE_CODEX_LOGOUT_FAIL" = 1 ]; then
		exit 1
	fi
	rm -f "$home/auth.json"
	exit 0
	;;
esac

for arg in "$@"; do
	printf 'ARG:%s\n' "$arg"
done
printf 'CODEX_HOME=%s\n' "$CODEX_HOME"
printf 'HOME=%s\n' "$HOME"
printf 'SESSION=%s\n' "$BONSAI_AGENT_SESSION_ID"
printf 'HAS_OPENAI_KEY=%s\n' "$([ -n "${OPENAI_API_KEY+x}" ] && echo 1 || echo 0)"
printf 'HAS_CODEX_KEY=%s\n' "$([ -n "${CODEX_API_KEY+x}" ] && echo 1 || echo 0)"
printf 'READY\n'
while IFS= read -r l; do
	printf 'REPLY:%s\n' "$l"
done
