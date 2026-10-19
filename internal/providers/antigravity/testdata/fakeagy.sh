#!/bin/sh
# Fake interactive agy for holder tests (POSIX only).
#   HOOK <Event> <json>  run the hook command found for <Event> in
#                        $HOME/.gemini/config/hooks.json, the payload on its
#                        stdin, the way agy does (sh -c, cwd = the file's dir)
#   NOTIFY <text>        write a terminal notification (OSC 9)
# A real SessionStart fires on startup.
hooks="$HOME/.gemini/config/hooks.json"
fire_hook() {
	[ -f "$hooks" ] || return 0
	cmd=$(awk -v ev="\"$1\":" '$1 == ev { found = 1 } found && /"command":/ { sub(/^[^:]*: "/, ""); sub(/",?$/, ""); print; exit }' "$hooks")
	[ -n "$cmd" ] || return 0
	out=$(cd "$(dirname "$hooks")" && printf '%s' "$2" | sh -c "$cmd")
	printf 'HOOKOUT:%s\n' "$out"
}
printf 'HOME=%s\n' "$HOME"
printf 'HOOKS=%s\n' "$([ -f "$hooks" ] && echo 1 || echo 0)"
printf 'SPOOLENV=%s\n' "$([ -n "$BONSAI_AGENT_ACTIVITY" ] && echo 1 || echo 0)"
printf 'READY\n'
fire_hook SessionStart '{"conversationId":"c1"}'
while IFS= read -r l; do
	case "$l" in
	"HOOK "*)
		rest=${l#HOOK }
		event=${rest%% *}
		payload=${rest#"$event"}
		fire_hook "$event" "${payload# }"
		printf 'HOOKED:%s\n' "$event"
		;;
	"NOTIFY "*)
		printf '\033]9;%s\007NOTIFIED\n' "${l#NOTIFY }"
		;;
	*)
		printf 'REPLY:%s\n' "$l"
		;;
	esac
done
