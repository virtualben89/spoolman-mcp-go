#!/bin/sh
# Print the CHANGELOG.md section of a version (e.g. "v0.1.3" or "0.1.3") plus a compare link.
# Exits non-zero if the section is missing or empty, so a release never ships without notes.
set -eu
ver=${1#v}
file=${2:-CHANGELOG.md}
section=$(awk -v v="$ver" '
  /^## \[/ { if (found) exit; if (index($0, "## [" v "]") == 1) { found = 1; next } }
  /^\[[^]]+\]: / { if (found) exit }
  found { print }
' "$file" | sed -e '/./,$!d')
if [ -z "$(printf '%s' "$section" | tr -d '[:space:]')" ]; then
  echo "release-notes: no CHANGELOG.md entry for $ver" >&2
  exit 1
fi
printf '%s\n' "$section"
link=$(grep -E "^\[$ver\]: " "$file" | sed -E 's/^\[[^]]+\]: //' || true)
if [ -n "$link" ]; then
  printf '\n**Full Changelog**: %s\n' "$link"
fi
