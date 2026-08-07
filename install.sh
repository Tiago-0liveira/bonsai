#!/usr/bin/env bash
#
# bonsai installer — builds the binary and installs it onto your PATH.
#
# Usage:
#   ./install.sh                     # install to /usr/local/bin (fallback ~/.local/bin)
#   PREFIX="$HOME/.local" ./install.sh   # install to $PREFIX/bin
#
set -euo pipefail

BINARY="bonsai"
SRC="."

# Resolve the repo root (directory of this script) so it works from anywhere.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

info()  { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
warn()  { printf '\033[1;33m==>\033[0m %s\n' "$*" >&2; }
die()   { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

command -v go >/dev/null 2>&1 || die "Go is required but not found on your PATH. See https://go.dev/dl/"

# Pick an install directory: explicit PREFIX, then /usr/local/bin, then ~/.local/bin.
if [ -n "${PREFIX:-}" ]; then
  BINDIR="$PREFIX/bin"
elif [ -w /usr/local/bin ] || { [ ! -e /usr/local/bin ] && [ -w /usr/local ]; }; then
  BINDIR="/usr/local/bin"
else
  BINDIR="$HOME/.local/bin"
fi

mkdir -p "$BINDIR"

info "Building ${BINARY}..."
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT
go build -o "${TMP}/${BINARY}" "${SRC}"

info "Installing to ${BINDIR}/${BINARY}"
if [ -w "${BINDIR}" ]; then
  install -m 0755 "${TMP}/${BINARY}" "${BINDIR}/${BINARY}"
else
  warn "${BINDIR} is not writable; retrying with sudo"
  sudo install -m 0755 "${TMP}/${BINARY}" "${BINDIR}/${BINARY}"
fi

info "Installed ${BINARY} to ${BINDIR}"

case ":$PATH:" in
  *":$BINDIR:"*) : ;;
  *) warn "$BINDIR is not on your PATH. Add this to your shell profile:"
     printf '    export PATH="%s:$PATH"\n' "$BINDIR" >&2 ;;
esac

cat <<EOF

Done. Try it:

  cd your-repo
  bonsai

Optional — enable 'bcd' to cd into worktrees from your shell:

  eval "\$(bonsai shell-init)"   # add to ~/.zshrc or ~/.bashrc

EOF
