#!/usr/bin/env bash
# Fails when go.mod pins an older Go than the latest stable release listed on
# go.dev. A network problem only warns: being offline must not block a commit.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
current=$(awk '$1 == "go" { print $2; exit }' "$root/go.mod")

releases=$(curl -fsS --max-time 5 'https://go.dev/dl/?mode=json') || {
  echo "check-go-version: could not reach go.dev, skipping" >&2
  exit 0
}
latest=$(grep -o '"version": *"go[0-9.]*"' <<<"$releases" | head -n1 | grep -o '[0-9][0-9.]*')

newest=$(printf '%s\n%s\n' "$current" "$latest" | sort -V | tail -n1)
if [ "$newest" != "$current" ]; then
  echo "Go $latest is out, go.mod still has $current. Bump it (go.mod, CI, PKGBUILD) or skip with LEFTHOOK_EXCLUDE=go-version." >&2
  exit 1
fi
echo "go $current is up to date"
