#!/bin/sh
# Fake `codex` for tests. Put it first on PATH under the name "codex".
#   FAKE_CODEX_VERSION      version to report (default 0.162.1)
#   FAKE_CODEX_EMAIL        email in the id_token `login` writes
#   FAKE_CODEX_PLAN         plan in that id_token (default plus)
#   FAKE_CODEX_ACCOUNT      ChatGPT account ID in that id_token (default acct-fake)
#   FAKE_CODEX_LOGIN_FAIL   1 = `login` exits 1 without writing auth.json
#   FAKE_CODEX_LOGOUT_FAIL  1 = `logout` exits 1
#   FAKE_CODEX_STATUS_SLEEP seconds `login status` sleeps first
#   FAKE_CODEX_LOG          file the login/logout/app-server commands append a call line to
#   FAKE_CODEX_FIXTURES     directory with initialize.json, account-read.json, rate-limits.json,
#                           model-list.json (app-server replies) and bundled-models.json
#                           (`debug models --bundled`); default: phase0/ next to this script
#   FAKE_CODEX_APPSERVER    app-server misbehaviour: exit (no reply), exit-after-init, hang
#                           (starts a child, never replies), huge (endless line), noisy
#                           (notifications and a server request before each reply), error
#                           (every method after initialize fails with a secret-looking message)
#   FAKE_CODEX_DELAY        seconds the app-server waits before it handles anything
#   FAKE_CODEX_PIDFILE      file the app-server appends its own and its child's pid to
#   FAKE_CODEX_DEBUG_FAIL   1 = `debug models` exits 1
#   FAKE_CODEX_LOGIN_PIDFILE file the interactive `login` appends its own and its child's pid to
#   FAKE_CODEX_LOGIN_HANG   1 = interactive `login` prints FAKE_LOGIN_WAITING and never finishes
#   FAKE_CODEX_LOGIN_PROMPT 1 = interactive `login` prints its terminal size, reads a line
#                           after "Enter code: ", echoes it as GOT:<line> and prints the size again
#   FAKE_CODEX_DEVICE_CODE  code `login --device-auth` prints with a verification URL
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
fixtures="${FAKE_CODEX_FIXTURES:-$(dirname "$(readlink -f "$0")")/phase0}"
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
	if [ -n "$FAKE_CODEX_LOGIN_PIDFILE" ]; then
		printf '%s\n' "$$" >>"$FAKE_CODEX_LOGIN_PIDFILE"
	fi
	if [ "$mode" = device ] && [ -n "$FAKE_CODEX_DEVICE_CODE" ]; then
		echo "Open https://auth.openai.com/codex/device and enter $FAKE_CODEX_DEVICE_CODE"
	fi
	if [ "$FAKE_CODEX_LOGIN_HANG" = 1 ]; then
		echo FAKE_LOGIN_WAITING
		sleep 60 &
		if [ -n "$FAKE_CODEX_LOGIN_PIDFILE" ]; then
			printf '%s\n' "$!" >>"$FAKE_CODEX_LOGIN_PIDFILE"
		fi
		wait
		exit 1
	fi
	if [ "$FAKE_CODEX_LOGIN_PROMPT" = 1 ]; then
		echo "SIZE:$(stty size)"
		printf 'Enter code: '
		IFS= read -r code
		echo "GOT:$code"
		echo "SIZE:$(stty size)"
	fi
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
app-server)
	log "app-server home=$home store=$store"
	if [ -n "$FAKE_CODEX_PIDFILE" ]; then
		printf '%s\n' "$$" >>"$FAKE_CODEX_PIDFILE"
	fi
	[ -n "$FAKE_CODEX_DELAY" ] && sleep "$FAKE_CODEX_DELAY"
	case "$FAKE_CODEX_APPSERVER" in
	exit) exit 3 ;;
	hang)
		sleep 60 &
		if [ -n "$FAKE_CODEX_PIDFILE" ]; then
			printf '%s\n' "$!" >>"$FAKE_CODEX_PIDFILE"
		fi
		wait
		exit 0
		;;
	huge)
		head -c 3000000 /dev/zero | tr '\0' x
		exit 0
		;;
	esac
	while IFS= read -r line; do
		method=$(printf '%s' "$line" | sed -n 's/.*"method" *: *"\([^"]*\)".*/\1/p')
		id=$(printf '%s' "$line" | sed -n 's/.*"id" *: *\([0-9][0-9]*\).*/\1/p')
		[ -n "$method" ] && log "rpc $method"
		[ -z "$id" ] && continue
		if [ "$FAKE_CODEX_APPSERVER" = noisy ]; then
			printf '%s\n' '{"method":"remoteControl/status/changed","params":{"status":"disabled"}}' '{"id":99,"method":"item/tool/call","params":{}}' 'not json at all'
		fi
		if [ "$FAKE_CODEX_APPSERVER" = error ] && [ "$method" != initialize ]; then
			printf '{"id":%s,"error":{"code":-32603,"message":"upstream said Bearer sk-SECRETSECRET1234"}}\n' "$id"
			continue
		fi
		case "$method" in
		initialize) file=initialize.json ;;
		account/read) file=account-read.json ;;
		account/rateLimits/read) file=rate-limits.json ;;
		model/list) file=model-list.json ;;
		*)
			printf '{"id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
			continue
			;;
		esac
		tr -d '\n' <"$fixtures/$file" | sed "s/\"id\": *[0-9][0-9]*/\"id\": $id/"
		printf '\n'
		if [ "$FAKE_CODEX_APPSERVER" = exit-after-init ] && [ "$method" = initialize ]; then
			exit 0
		fi
	done
	exit 0
	;;
debug)
	if has models "$@" && has --bundled "$@"; then
		log "debug-models home=$home store=$store"
		[ "$FAKE_CODEX_DEBUG_FAIL" = 1 ] && exit 1
		cat "$fixtures/bundled-models.json"
		exit 0
	fi
	exit 2
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
