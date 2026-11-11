#!/usr/bin/env bash
set -euo pipefail
reference=${1:?release image reference required}
error=$(mktemp)
trap 'rm -f "$error"' EXIT
if docker buildx imagetools inspect "$reference" >/dev/null 2>"$error"; then
  echo 'Release tag already exists; refusing to overwrite it.' >&2
  exit 1
fi
# Only a registry's explicit missing-manifest response permits publication.
# Authentication, rate limits, transport failures and unknown errors fail closed.
if ! grep -Eq 'manifest unknown|: not found$' "$error"; then
  cat "$error" >&2
  echo 'Could not establish that the release tag is absent.' >&2
  exit 1
fi
