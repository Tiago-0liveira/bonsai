#!/bin/sh
set -eu

origin="${BONSAI_RELAY_ORIGIN:-}"

case "$origin" in
  https://*) ;;
  *)
    echo "BONSAI_RELAY_ORIGIN must be an HTTPS origin such as https://relay.example.com" >&2
    exit 1
    ;;
esac

host="${origin#https://}"
case "$host" in
  ""|*/*|*\?*|*\#*|*@*)
    echo "BONSAI_RELAY_ORIGIN must not include a path, query, fragment, or userinfo" >&2
    exit 1
    ;;
esac

sed -i "s|__BONSAI_RELAY_ORIGIN__|$origin|g" /srv/index.html

exec caddy run --config /etc/caddy/Caddyfile --adapter caddyfile
