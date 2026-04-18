#!/usr/bin/env bash
# lighthouse.sh — resource-level smoke checks approximating Lighthouse's
# Performance pillar without needing headless Chrome. Validates:
#   - HTML root <= 50KB gzipped (LCP-friendly)
#   - Accept-Encoding: gzip produces `Content-Encoding: gzip`
#   - /static/css/theme.css Cache-Control contains max-age
#   - All the security headers are present (piggy-backs check-headers.sh)
#
# This is NOT a replacement for real Lighthouse (which we defer until P7 can
# build a CodeMirror bundle via Node toolchain + run headless Chrome in CI).
# Suitable for CI smoke gating and for `make release`.

set -euo pipefail

URL="${1:-http://127.0.0.1:8080}"
URL="${URL%/}"   # strip trailing slash

fail=0

echo "[1/4] response headers baseline"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
"$SCRIPT_DIR/check-headers.sh" "$URL/" || fail=1

echo "[2/4] gzip encoding for HTML"
enc=$(curl -s -H "Accept-Encoding: gzip" -o /dev/null -D - "$URL/" | awk -F': ' 'tolower($1)=="content-encoding"{print tolower($2)}' | tr -d '\r\n')
if [[ "$enc" != *"gzip"* ]]; then
  echo "  ✗ Content-Encoding missing gzip (got '$enc')" >&2
  fail=1
else
  echo "  ✓ gzip active"
fi

echo "[3/4] html root size (gzipped) <= 50KB"
sz=$(curl -s -H "Accept-Encoding: gzip" -o /dev/null -w "%{size_download}" "$URL/")
if (( sz > 50000 )); then
  echo "  ✗ home size ${sz}B > 50000B" >&2
  fail=1
else
  echo "  ✓ home size = ${sz}B"
fi

echo "[4/4] static asset cache headers"
cache=$(curl -s -o /dev/null -D - "$URL/static/css/theme.css" | awk -F': ' 'tolower($1)=="cache-control"{print $2}' | tr -d '\r\n')
if [[ "$cache" != *"max-age"* ]]; then
  echo "  ✗ /static/css/theme.css Cache-Control missing max-age ('$cache')" >&2
  fail=1
else
  echo "  ✓ Cache-Control: $cache"
fi

if (( fail == 0 )); then
  echo "[OK] lighthouse smoke PASS"
  exit 0
fi
echo "[FAIL] lighthouse smoke FAILED" >&2
exit 1
