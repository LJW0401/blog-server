#!/usr/bin/env bash
# Verify baseline security response headers on a running blog-server instance.
# Usage: check-headers.sh <url>
set -euo pipefail

URL="${1:-http://127.0.0.1:8080/}"

headers=$(curl -sI "$URL")

missing=()
for h in \
  "Content-Security-Policy" \
  "Strict-Transport-Security" \
  "X-Content-Type-Options" \
  "X-Frame-Options" \
  "Referrer-Policy" \
  "X-Request-ID"
do
  if ! grep -qi "^$h:" <<<"$headers"; then
    missing+=("$h")
  fi
done

if ((${#missing[@]} > 0)); then
  echo "MISSING HEADERS:" >&2
  printf '  %s\n' "${missing[@]}" >&2
  exit 1
fi
echo "[OK] all baseline security headers present at $URL"
