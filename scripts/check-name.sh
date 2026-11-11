#!/bin/sh
# check-name.sh fails when the old name, YarGPT, is left anywhere in the tracked
# files, except where scripts/name-allow.txt keeps it on purpose (spec ad09e72c §6).
set -eu
cd "$(dirname "$0")/.."
allow=$(mktemp)
trap 'rm -f "$allow"' EXIT
grep -v -e '^#' -e '^$' scripts/name-allow.txt > "$allow"
left=$(git grep -n -I -i 'yargpt' -- . ':!scripts/name-allow.txt' ':!scripts/check-name.sh' | grep -v -E -f "$allow" || true)
if [ -n "$left" ]; then
  echo "The old name is left in these lines. Rename it, or add a pattern with its reason to scripts/name-allow.txt:" >&2
  echo "$left" >&2
  exit 1
fi
