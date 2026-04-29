#!/usr/bin/env bash
#
# 把 ./blog-server 二进制 + ./deploy/blog-server.service 打成发布用的
# tarball 并生成 sha256 校验。复刻历史 release 附件（v1.x）的归档结构：
#
#     blog-server-linux-amd64.tar.gz
#     ├── blog-server
#     └── deploy/blog-server.service
#
# 用法：
#   make package          # 推荐：自动先 make build
#   ./scripts/package-release.sh   # 仓库里已经有 ./blog-server 时直接调
#
# 产物：
#   blog-server-linux-amd64.tar.gz
#   blog-server-linux-amd64.tar.gz.sha256
#
set -euo pipefail

cd "$(dirname "$0")/.."

BIN="blog-server"
SVC="deploy/blog-server.service"
ARCHIVE="${ARCHIVE:-blog-server-linux-amd64.tar.gz}"
SHA="${ARCHIVE}.sha256"

[[ -f "$BIN" ]] || { echo "missing $BIN — run 'make build' first" >&2; exit 1; }
[[ -f "$SVC" ]] || { echo "missing $SVC" >&2; exit 1; }

tar -czf "$ARCHIVE" "$BIN" "$SVC"
sha256sum "$ARCHIVE" > "$SHA"

size=$(du -h "$ARCHIVE" | cut -f1)
echo "[OK] packaged $ARCHIVE ($size) + $SHA"
