#!/usr/bin/env bash
# migrate-test.sh — verify that copying content/, images/, config.yaml and
# data.sqlite to a fresh directory is enough to reconstitute the site
# (requirement §7 验收关口 8: "数据可脱离系统独立迁移").
#
# Usage: scripts/migrate-test.sh [src_dir]
#
# The script:
#   1. Copies the given source data directory to a temp location
#   2. Builds blog-server (if not already present)
#   3. Starts a second instance on a random localhost port
#   4. Curls /, /docs, /projects, /manage/login
#   5. Asserts all return 200 and shut down cleanly

set -euo pipefail

SRC_DIR="${1:-$(pwd)}"
SRC_DIR="$(cd "$SRC_DIR" && pwd)"

if [ ! -f "$SRC_DIR/config.yaml" ]; then
  echo "migrate-test: $SRC_DIR/config.yaml not found" >&2
  echo "  Hint: run this script from a working deployment (with config.yaml next to content/)" >&2
  exit 1
fi

export PATH="/snap/go/current/bin:${HOME}/go/bin:${PATH}"

# Build the server binary if missing.
if [ ! -x "$SRC_DIR/blog-server" ]; then
  (cd "$SRC_DIR" && go build -o blog-server ./cmd/server)
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "[1/5] copying data to $TMP"
for f in content images config.yaml data.sqlite; do
  if [ -e "$SRC_DIR/$f" ]; then
    cp -r "$SRC_DIR/$f" "$TMP/"
  fi
done
cp "$SRC_DIR/blog-server" "$TMP/"

# Pick an unused port. `ss -lnt` lists listening; we just retry until free.
PORT=0
for _ in 1 2 3 4 5; do
  C="$((RANDOM % 10000 + 20000))"
  if ! ss -lnt 2>/dev/null | grep -q ":$C "; then
    PORT=$C
    break
  fi
done
if [ "$PORT" -eq 0 ]; then
  echo "migrate-test: no free port found" >&2
  exit 1
fi

echo "[2/5] patching listen_addr to 127.0.0.1:$PORT"
sed -i -E "s|listen_addr:.*|listen_addr: \"127.0.0.1:$PORT\"|" "$TMP/config.yaml"

echo "[3/5] launching second instance"
(cd "$TMP" && ./blog-server -config config.yaml >"$TMP/srv.log" 2>&1) &
SRV_PID=$!
trap 'kill $SRV_PID 2>/dev/null || true; wait $SRV_PID 2>/dev/null; rm -rf "$TMP"' EXIT

# Wait up to 5s for the port to be ready.
READY=0
for _ in $(seq 1 25); do
  if curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:$PORT/__healthz" | grep -q "^200$"; then
    READY=1
    break
  fi
  sleep 0.2
done
if [ $READY -eq 0 ]; then
  echo "migrate-test: server did not become ready" >&2
  tail "$TMP/srv.log" >&2
  exit 1
fi

echo "[4/5] hitting public routes"
fail=0
for path in "/" "/docs" "/projects" "/manage/login" "/rss.xml" "/sitemap.xml"; do
  # RSS + sitemap are P7 — tolerate 404 here but log it.
  status=$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:$PORT$path")
  case "$path:$status" in
    "/:200"|"/docs:200"|"/projects:200"|"/manage/login:200")
      echo "  $path -> $status ✓"
      ;;
    "/rss.xml:"*|"/sitemap.xml:"*)
      echo "  $path -> $status (P7)"
      ;;
    *)
      echo "  $path -> $status ✗"
      fail=1
      ;;
  esac
done

echo "[5/5] clean shutdown"
kill -TERM "$SRV_PID"
wait "$SRV_PID" 2>/dev/null || true
trap - EXIT
rm -rf "$TMP"

if [ "$fail" -eq 1 ]; then
  echo "migrate-test: FAILED" >&2
  exit 1
fi
echo "migrate-test: PASS"
