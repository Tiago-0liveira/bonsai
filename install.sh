#!/usr/bin/env bash
# Install an official, checksum-verified release. Go is not required.
set -euo pipefail
repo=Tiago-0liveira/bonsai
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
for tool in curl tar; do command -v "$tool" >/dev/null || die "$tool is required"; done
case "$(uname -s)" in Linux) os=linux ;; Darwin) os=darwin ;; *) die 'Unsupported OS' ;; esac
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) die 'Unsupported architecture' ;; esac
if [ -n "${PREFIX:-}" ]; then
  bindir="$PREFIX/bin"
elif [ -w /usr/local/bin ]; then
  bindir=/usr/local/bin
else
  bindir="$HOME/.local/bin"
fi
tmp="$(mktemp -d)"
staged=''
trap 'rm -rf "$tmp"; if [ -n "$staged" ]; then rm -f "$staged"; fi' EXIT
# Resolve the tag once so a concurrent release cannot mix archive and checksum versions.
release="$(curl --proto '=https' --tlsv1.2 -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest")"
tag="${release##*/}"
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die 'No stable release found'
asset="bonsai_${os}_${arch}.tar.gz"
base="https://github.com/$repo/releases/download/$tag"
curl --proto '=https' --tlsv1.2 -fsSL "$base/$asset" -o "$tmp/$asset"
curl --proto '=https' --tlsv1.2 -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"
expected="$(awk -v name="$asset" '$2 == name {print $1}' "$tmp/checksums.txt")"
[[ "$expected" =~ ^[[:xdigit:]]{64}$ ]] || die 'Missing or invalid SHA256 checksum'
if command -v sha256sum >/dev/null; then
  actual="$(sha256sum "$tmp/$asset")"
else
  command -v shasum >/dev/null || die 'sha256sum or shasum is required'
  actual="$(shasum -a 256 "$tmp/$asset")"
fi
[ "${actual%% *}" = "$expected" ] || die 'SHA256 checksum mismatch'
tar -xzf "$tmp/$asset" -C "$tmp" bonsai
[ -f "$tmp/bonsai" ] && [ ! -L "$tmp/bonsai" ] || die 'Archive has no regular bonsai binary'
mkdir -p "$bindir"
staged="$(mktemp "$bindir/.bonsai-install.XXXXXX")"
install -m 0755 "$tmp/bonsai" "$staged"
mv -f "$staged" "$bindir/bonsai"
printf 'Installed Bonsai %s to %s/bonsai\n' "$tag" "$bindir"
case ":$PATH:" in *":$bindir:"*) ;; *) printf 'Add %s to your PATH.\n' "$bindir" ;; esac
printf 'Optional shell helper: eval "$(bonsai shell-init)"\n'
