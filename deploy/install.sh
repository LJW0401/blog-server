#!/usr/bin/env bash
# blog-server 一键安装 / 更新 / 卸载脚本
#
#   sudo bash install.sh install     # 首次安装
#   sudo bash install.sh update      # 升级到最新 release
#   sudo bash install.sh status      # 查看服务状态
#   sudo bash install.sh uninstall   # 卸载（带数据备份）
#   sudo bash install.sh help        # 显示帮助
#
# 环境变量（可选）：
#   GITHUB_REPO    发布仓库，默认 penguin-blog/blog-server（改成你的）
#   RELEASE_TAG    版本标签，默认 latest
#   INSTALL_DIR    安装目录，默认 /opt/blog-server
#   SERVICE_USER   运行用户，默认 blog
#   GH_MIRROR      GitHub 下载加速前缀（例：https://ghproxy.com/）
#                  国内网络慢时强烈建议设置；留空走直连
#   LOCAL_ASSET    本地已经下载好的 tarball 路径（例：/tmp/blog-server-linux-amd64.tar.gz）
#                  设置后跳过远程下载；优先级最高
#   NO_CADDY=1     跳过 Caddy 安装与配置（自己接管反代时用）
#   NO_BACKUP=1    卸载时不备份数据（危险）

set -euo pipefail

# ================== 可覆盖的默认值 ==================
GITHUB_REPO="${GITHUB_REPO:-LJW0401/blog-server}"
RELEASE_TAG="${RELEASE_TAG:-latest}"

# INSTALL_DIR 默认为调用脚本时的当前工作目录。
# 这样 cd 到你想放的地方、sudo bash install.sh install，东西就留在原地，
# 方便用文件管理器 / git / scp 直接操作，不需要 sudo 才能看。
# 强烈建议在一个专用子目录里跑（mkdir ~/blog-site && cd ~/blog-site），
# 不要直接在 $HOME 或 / 下跑——uninstall 的 trash 归档会把整个目录打包。
INSTALL_DIR="${INSTALL_DIR:-$(pwd -P)}"

# SERVICE_USER 默认为实际调用 sudo 的那个用户（即 $SUDO_USER），
# 如果直接以 root 跑则退回到 'blog' 专用用户。
# 好处：文件所有权还是你自己，平时无需 sudo 就能编辑 content/*.md。
SERVICE_USER="${SERVICE_USER:-${SUDO_USER:-blog}}"

ASSET_NAME_PATTERN='blog-server-linux-{ARCH}.tar.gz'   # release 里资产命名约定
SERVICE_NAME="blog-server"
GH_MIRROR="${GH_MIRROR:-}"   # 国内网速慢时可设 https://ghproxy.com/
# ===================================================

# 把 https://github.com/... 转换成带镜像前缀的 URL。
mirror_url() {
    local u="$1"
    if [[ -z "$GH_MIRROR" ]]; then
        echo "$u"
    else
        # 规范化：去掉结尾 /，然后拼上原始 URL
        echo "${GH_MIRROR%/}/$u"
    fi
}

# ---------- 视觉辅助 ----------
RED="\033[0;31m"; GREEN="\033[0;32m"; YELLOW="\033[0;33m"; BLUE="\033[0;34m"; NC="\033[0m"
info()  { echo -e "${BLUE}[INFO]${NC}  $*"; }
ok()    { echo -e "${GREEN}[ OK ]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
err()   { echo -e "${RED}[FAIL]${NC}  $*" >&2; }
die()   { err "$*"; exit 1; }

# ---------- 前置检查 ----------
require_root() {
    if [[ $EUID -ne 0 ]]; then
        die "需要 root 权限，请用 sudo 运行：sudo bash $0 $CMD"
    fi
}

detect_arch() {
    local arch
    arch=$(uname -m)
    case "$arch" in
        x86_64|amd64) echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *) die "不支持的架构：$arch（目前只发布了 amd64 / arm64）" ;;
    esac
}

detect_os() {
    if [[ ! -f /etc/os-release ]]; then
        die "无法识别发行版（缺少 /etc/os-release）"
    fi
    # shellcheck disable=SC1091
    . /etc/os-release
    case "${ID:-}${ID_LIKE:-}" in
        *debian*|*ubuntu*) echo "debian" ;;
        *rhel*|*fedora*|*centos*) echo "rhel" ;;
        *) warn "未明确支持的发行版：${ID}；脚本以 Debian/Ubuntu 为主测试"; echo "debian" ;;
    esac
}

ensure_deps() {
    info "检查依赖：curl, tar, apache2-utils（htpasswd）"
    local os
    os=$(detect_os)
    local missing=()
    command -v curl >/dev/null     || missing+=(curl)
    command -v tar >/dev/null      || missing+=(tar)
    command -v htpasswd >/dev/null || missing+=(apache2-utils)
    command -v gpg >/dev/null      || missing+=(gnupg)
    if (( ${#missing[@]} > 0 )); then
        info "安装依赖：${missing[*]}"
        if [[ "$os" == "debian" ]]; then
            apt-get update -qq
            apt-get install -y -qq "${missing[@]}"
        elif [[ "$os" == "rhel" ]]; then
            yum install -y "${missing[@]}"
        fi
    fi
    ok "依赖齐全"
}

# ---------- 下载并解压 release ----------
download_release() {
    local arch asset tmpdir url resolved_tag
    arch=$(detect_arch)
    asset="${ASSET_NAME_PATTERN/\{ARCH\}/$arch}"
    tmpdir=$(mktemp -d)
    trap "rm -rf $tmpdir" RETURN

    # 优先使用本地已有 tarball（LOCAL_ASSET）——跳过远程下载
    if [[ -n "$LOCAL_ASSET" ]]; then
        if [[ ! -f "$LOCAL_ASSET" ]]; then
            die "LOCAL_ASSET 指向的文件不存在：$LOCAL_ASSET"
        fi
        info "使用本地 tarball：$LOCAL_ASSET"
        cp "$LOCAL_ASSET" "$tmpdir/$asset"
        resolved_tag="${RELEASE_TAG}"
        [[ "$resolved_tag" == "latest" ]] && resolved_tag="local"

        # 校验（如果同目录有 .sha256）
        local local_sha="${LOCAL_ASSET}.sha256"
        if [[ -f "$local_sha" ]]; then
            info "本地 SHA256 校验……"
            cp "$local_sha" "$tmpdir/${asset}.sha256"
            ( cd "$tmpdir" && sha256sum -c "${asset}.sha256" >/dev/null ) || die "本地 SHA256 校验失败"
            ok "SHA256 校验通过"
        else
            warn "未找到 $local_sha，跳过校验"
        fi
    else
        # 远程下载路径
        if [[ "$RELEASE_TAG" == "latest" ]]; then
            info "查询 $GITHUB_REPO 的最新 release……"
            resolved_tag=$(curl -sSL "https://api.github.com/repos/$GITHUB_REPO/releases/latest" \
                | grep -oP '"tag_name":\s*"\K[^"]+' | head -1)
            [[ -z "$resolved_tag" ]] && die "无法获取最新 release tag（仓库是不是 public / 是否至少发布过一个版本？）"
        else
            resolved_tag="$RELEASE_TAG"
        fi
        info "目标版本：$resolved_tag"

        # 复用已下载的缓存：如果 /tmp/blog-server-<tag>-<arch>.tar.gz 存在且非空，直接用
        local cached="/tmp/blog-server-${resolved_tag}-${arch}.tar.gz"
        if [[ -s "$cached" ]]; then
            info "发现缓存 tarball：$cached（跳过下载；如需强制重下请先 rm）"
            cp "$cached" "$tmpdir/$asset"
        else
            url=$(mirror_url "https://github.com/$GITHUB_REPO/releases/download/$resolved_tag/$asset")
            info "下载：$url"
            # --progress-bar 显示下载进度；--connect-timeout 避免无限等待
            if ! curl -fL --progress-bar --connect-timeout 15 --retry 3 --retry-delay 2 \
                     "$url" -o "$tmpdir/$asset"; then
                if [[ -z "$GH_MIRROR" ]]; then
                    err "直连 GitHub 下载失败。国内网络可能需要镜像代理，重试："
                    err "  sudo GH_MIRROR=https://ghproxy.com/ bash $0 install"
                    err "备选镜像：ghfast.top / gh.llkk.cc / mirror.ghproxy.com"
                fi
                err "或手动下载后指定本地文件："
                err "  sudo LOCAL_ASSET=/path/to/$asset bash $0 install"
                die "下载失败。检查 (1) GITHUB_REPO 是否正确 (2) release 里是否有 $asset 这个资产"
            fi
            # 保存到缓存位置，下次同版本直接复用
            cp "$tmpdir/$asset" "$cached" 2>/dev/null || true
        fi

        # 校验 sha256（如果有）
        local sha_asset="$asset.sha256"
        local sha_url
        sha_url=$(mirror_url "https://github.com/$GITHUB_REPO/releases/download/$resolved_tag/$sha_asset")
        if curl -fsSL --connect-timeout 15 "$sha_url" -o "$tmpdir/$sha_asset" 2>/dev/null; then
            info "校验 SHA256……"
            ( cd "$tmpdir" && sha256sum -c "$sha_asset" >/dev/null ) || die "SHA256 校验失败"
            ok "SHA256 校验通过"
        else
            warn "未找到 $sha_asset，跳过校验（如果是官方 release 建议补上）"
        fi
    fi

    info "解压到 $tmpdir"
    tar xzf "$tmpdir/$asset" -C "$tmpdir"
    # 期望至少包含 blog-server 这一个二进制
    [[ -f "$tmpdir/blog-server" ]] || die "解压产物缺少 blog-server 二进制"

    # 把解压目录路径返回给调用者
    DOWNLOAD_TMP="$tmpdir"
    RESOLVED_TAG="$resolved_tag"
    # 不在 RETURN 时清理，让调用者用完再清
    trap - RETURN
}

cleanup_download() {
    [[ -n "${DOWNLOAD_TMP:-}" && -d "$DOWNLOAD_TMP" ]] && rm -rf "$DOWNLOAD_TMP"
}

# ---------- 创建用户和目录 ----------
setup_user_and_dirs() {
    if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
        info "创建系统用户 $SERVICE_USER（当前用户不存在）"
        useradd -r -s /usr/sbin/nologin -d "$INSTALL_DIR" "$SERVICE_USER"
    else
        info "以已有用户 $SERVICE_USER 运行服务"
    fi

    info "准备目录 $INSTALL_DIR"
    mkdir -p "$INSTALL_DIR"

    # 只在目录所有者不是 SERVICE_USER 时才 chown（避免把用户自己的家目录权限搞乱）。
    local current_owner
    current_owner=$(stat -c '%U' "$INSTALL_DIR")
    if [[ "$current_owner" != "$SERVICE_USER" ]]; then
        info "将 $INSTALL_DIR 所有权改为 $SERVICE_USER（原 $current_owner）"
        chown "$SERVICE_USER:$SERVICE_USER" "$INSTALL_DIR"
    fi
    # 权限：user=rwx, group=rx, other=---（如果目录在 $HOME 下，原始 755 就够了，不强改）
    if [[ "$(stat -c '%a' "$INSTALL_DIR")" == "755" ]]; then
        : # 保留
    elif [[ "$(stat -c '%a' "$INSTALL_DIR")" -lt 700 ]]; then
        chmod 750 "$INSTALL_DIR"
    fi

    local subs=(content/docs content/projects images backups trash)
    for d in "${subs[@]}"; do
        if [[ ! -d "$INSTALL_DIR/$d" ]]; then
            sudo -u "$SERVICE_USER" mkdir -p "$INSTALL_DIR/$d" 2>/dev/null || mkdir -p "$INSTALL_DIR/$d"
        fi
    done
    ok "目录就绪"
}

# ---------- 生成 config.yaml（交互式） ----------
prompt() {
    local varname="$1" prompt_text="$2" default_value="${3:-}"
    local value
    if [[ -n "$default_value" ]]; then
        read -rp "$prompt_text [$default_value]: " value
        value="${value:-$default_value}"
    else
        read -rp "$prompt_text: " value
    fi
    printf -v "$varname" '%s' "$value"
}

prompt_secret() {
    local varname="$1" prompt_text="$2"
    local value
    read -rsp "$prompt_text: " value; echo
    printf -v "$varname" '%s' "$value"
}

generate_config_if_missing() {
    local cfg="$INSTALL_DIR/config.yaml"
    if [[ -f "$cfg" ]]; then
        info "发现已有配置：$cfg（保留不覆盖；如需重置请先 uninstall 或手动删除）"
        return
    fi

    info "生成 config.yaml（交互式）"
    local listen_addr admin_user admin_password admin_hash github_token

    prompt listen_addr "服务监听地址（Caddy 反代到这里）" "127.0.0.1:8080"
    prompt admin_user "管理员用户名" "admin"

    while true; do
        prompt_secret admin_password "管理员密码（≥ 8 位）"
        if [[ ${#admin_password} -lt 8 ]]; then
            warn "密码太短，请重新输入"
            continue
        fi
        local confirm
        prompt_secret confirm "再输一次确认"
        if [[ "$admin_password" != "$confirm" ]]; then
            warn "两次输入不一致，请重新输入"
            continue
        fi
        break
    done
    admin_hash=$(htpasswd -bnBC 10 "" "$admin_password" | tr -d ':\n')
    unset admin_password confirm

    prompt github_token "GitHub Personal Access Token（可选，留空则未登录 60 req/h）" ""

    local tmp_cfg
    tmp_cfg=$(mktemp)
    cat > "$tmp_cfg" <<EOF
# blog-server 生产配置（由 install.sh 生成于 $(date -Iseconds)）
# 文件权限 0600；修改后 sudo systemctl restart $SERVICE_NAME

listen_addr: "$listen_addr"

admin_username: "$admin_user"
admin_password_bcrypt: "$admin_hash"
# 首次修改密码后由应用自动写入时间戳，此值不要手动改
password_changed_at: null

data_dir: "$INSTALL_DIR"

github_token: "$github_token"
github_sync_interval_min: 30
EOF

    install -m 0600 -o "$SERVICE_USER" -g "$SERVICE_USER" "$tmp_cfg" "$cfg"
    rm -f "$tmp_cfg"
    ok "config.yaml 写入 $cfg（权限 600）"
}

# ---------- systemd unit ----------
install_systemd_unit() {
    local unit="/etc/systemd/system/$SERVICE_NAME.service"
    info "写入 systemd unit: $unit"
    cat > "$unit" <<EOF
[Unit]
Description=blog-server — personal site
After=network-online.target
Wants=network-online.target
# StartLimit* directives must live in [Unit], not [Service] — systemd will
# emit a "Unknown key name" warning and ignore them otherwise.
StartLimitIntervalSec=30
StartLimitBurst=5

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_USER
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/blog-server -config $INSTALL_DIR/config.yaml
Restart=on-failure
RestartSec=5s

# Hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
# ProtectHome=tmpfs (not =true): when INSTALL_DIR lives under /home (e.g.
# /home/ubuntu/blog-site), =true masks the entire /home and systemd can't even
# locate the binary at ExecStart. =tmpfs mounts /home as empty tmpfs; the
# ReadWritePaths= below then bind-mounts the install dir back in.
ProtectHome=tmpfs
ReadWritePaths=$INSTALL_DIR
ProtectKernelModules=true
ProtectKernelTunables=true
ProtectControlGroups=true
RestrictNamespaces=true
RestrictRealtime=true
RestrictSUIDSGID=true
LockPersonality=true
MemoryDenyWriteExecute=true
SystemCallFilter=@system-service
SystemCallErrorNumber=EPERM
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    ok "systemd unit 就绪"
}

# ---------- Caddy 安装 + 配置 ----------
install_caddy() {
    if [[ "${NO_CADDY:-0}" == "1" ]]; then
        info "NO_CADDY=1，跳过 Caddy 安装"
        return
    fi

    if ! command -v caddy >/dev/null; then
        info "安装 Caddy……"
        local os
        os=$(detect_os)
        if [[ "$os" == "debian" ]]; then
            apt-get install -y debian-keyring debian-archive-keyring apt-transport-https
            curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
                | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
            curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
                > /etc/apt/sources.list.d/caddy-stable.list
            apt-get update -qq
            apt-get install -y caddy
        else
            warn "非 Debian 系，请手动安装 Caddy 后重跑：NO_CADDY=1 bash $0 install"
            return
        fi
    else
        info "Caddy 已安装：$(caddy version)"
    fi
}

configure_caddy() {
    if [[ "${NO_CADDY:-0}" == "1" ]]; then
        return
    fi

    local domain listen_port
    listen_port=$(awk -F'"' '/listen_addr/ {split($2,a,":"); print a[2]; exit}' "$INSTALL_DIR/config.yaml")
    listen_port="${listen_port:-8080}"

    echo
    echo "域名模式选择："
    echo "  1. 有域名（自动申请 Let's Encrypt 证书，推荐）"
    echo "  2. 仅 IP，用自签证书（浏览器会警告但能工作，适合先试试）"
    echo "  3. 跳过 Caddy 配置（稍后手动）"
    local mode
    prompt mode "选择 [1/2/3]" "1"

    local domain tls_mode
    case "$mode" in
        1)
            prompt domain "对外域名（例：blog.example.com）" ""
            if [[ -z "$domain" ]]; then
                warn "域名留空，跳过"
                return
            fi
            tls_mode=""   # 空 → Caddy 自动走 ACME
            ;;
        2)
            local default_ip
            default_ip=$(curl -fsSL --max-time 3 https://ifconfig.me 2>/dev/null || hostname -I | awk '{print $1}')
            prompt domain "VPS 公网 IP" "$default_ip"
            if [[ -z "$domain" ]]; then
                warn "IP 留空，跳过"
                return
            fi
            domain="https://$domain"
            tls_mode="tls internal"
            warn "浏览器首次访问会提示'不安全连接'，点'继续访问'即可。日后买域名后重跑 install 覆盖 Caddyfile。"
            ;;
        *)
            info "跳过 Caddy 配置"
            return
            ;;
    esac

    local caddy_cfg=/etc/caddy/Caddyfile
    if [[ -f "$caddy_cfg" ]] && [[ $(wc -l < "$caddy_cfg") -gt 5 ]]; then
        warn "已有 $caddy_cfg（非空），追加 blog-server 块而非覆盖"
        # Append block only if domain not already present
        if grep -q "^$domain " "$caddy_cfg" 2>/dev/null || grep -q "^$domain{" "$caddy_cfg" 2>/dev/null; then
            info "$domain 已在 Caddyfile 中，跳过"
            return
        fi
        cat >> "$caddy_cfg" <<EOF

# blog-server (added by install.sh on $(date -Iseconds))
$domain {
    ${tls_mode}
    reverse_proxy 127.0.0.1:$listen_port
    encode zstd gzip
    header {
        Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
        -Server
    }
}
EOF
    else
        cat > "$caddy_cfg" <<EOF
# blog-server Caddyfile (generated by install.sh on $(date -Iseconds))
$domain {
    ${tls_mode}
    reverse_proxy 127.0.0.1:$listen_port
    encode zstd gzip
    header {
        Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
        -Server
    }
}
EOF
    fi
    info "Caddyfile 写入 $caddy_cfg"
    systemctl reload caddy || systemctl restart caddy
    ok "Caddy 已重载；ACME 证书将在后台自动申请（一般 10–30 秒）"
}

# ---------- 启动服务 ----------
start_service() {
    info "启用并启动 $SERVICE_NAME.service"
    systemctl enable --now "$SERVICE_NAME.service"
    sleep 1
    if systemctl is-active --quiet "$SERVICE_NAME.service"; then
        ok "服务已启动（pid=$(systemctl show -p MainPID --value "$SERVICE_NAME.service")）"
    else
        err "服务启动失败，查看日志：sudo journalctl -u $SERVICE_NAME -n 50 --no-pager"
        journalctl -u "$SERVICE_NAME" -n 30 --no-pager || true
        die "请修复后重试"
    fi
}

# ---------- 各子命令 ----------
cmd_install() {
    require_root
    info "=== blog-server 安装 ==="
    echo
    echo "  安装目录：$INSTALL_DIR"
    echo "  运行用户：$SERVICE_USER"
    echo "  数据：    $INSTALL_DIR/{content,images,backups,data.sqlite}"
    echo
    read -rp "继续？[Y/n] " yn
    yn="${yn:-Y}"
    [[ "$yn" =~ ^[Yy] ]] || { info "已取消"; exit 0; }
    echo
    ensure_deps
    download_release
    setup_user_and_dirs

    info "安装 blog-server 二进制到 $INSTALL_DIR/"
    install -m 0755 -o "$SERVICE_USER" -g "$SERVICE_USER" \
        "$DOWNLOAD_TMP/blog-server" "$INSTALL_DIR/blog-server"

    generate_config_if_missing
    install_systemd_unit
    install_caddy
    configure_caddy
    start_service
    cleanup_download

    echo
    ok "=== 安装完成 ==="
    echo
    echo "  服务状态：sudo systemctl status $SERVICE_NAME"
    echo "  实时日志：sudo journalctl -u $SERVICE_NAME -f"
    echo "  配置文件：$INSTALL_DIR/config.yaml"
    echo "  数据目录：$INSTALL_DIR/{content,images,backups,data.sqlite}"
    echo
    echo "  下一步："
    echo "    1. 浏览器访问 https://<你刚才输入的域名>/manage/login"
    echo "    2. 用设置的管理员用户名 + 密码登录"
    echo "    3. 点 '修改密码' 保存一次（让黄条消失）"
    echo "    4. '基本信息' 填个人信息 + '文档管理' 写第一篇文章"
    echo
    echo "  升级：sudo bash $0 update"
    echo "  卸载：sudo bash $0 uninstall"
}

cmd_update() {
    require_root
    info "=== blog-server 升级 ==="
    ensure_deps
    [[ -f "$INSTALL_DIR/blog-server" ]] || die "未检测到已安装的 blog-server（$INSTALL_DIR 里没有二进制），请先 install"

    download_release

    local old_ver new_ver
    old_ver=$("$INSTALL_DIR/blog-server" -version 2>/dev/null || echo unknown)
    new_ver=$("$DOWNLOAD_TMP/blog-server" -version 2>/dev/null || echo unknown)
    info "版本：$old_ver → $new_ver"

    # 备份旧二进制
    local backup="$INSTALL_DIR/blog-server.prev"
    cp "$INSTALL_DIR/blog-server" "$backup"
    info "旧二进制备份到 $backup"

    install -m 0755 -o "$SERVICE_USER" -g "$SERVICE_USER" \
        "$DOWNLOAD_TMP/blog-server" "$INSTALL_DIR/blog-server"

    info "重启 $SERVICE_NAME.service"
    systemctl restart "$SERVICE_NAME.service"
    sleep 1
    if systemctl is-active --quiet "$SERVICE_NAME.service"; then
        ok "升级到 $RESOLVED_TAG ($new_ver) 完成"
        info "回滚：sudo cp $backup $INSTALL_DIR/blog-server && sudo systemctl restart $SERVICE_NAME"
    else
        err "新版本启动失败，自动回滚到旧版本"
        cp "$backup" "$INSTALL_DIR/blog-server"
        systemctl restart "$SERVICE_NAME.service"
        die "请查看日志定位：sudo journalctl -u $SERVICE_NAME -n 50"
    fi
    cleanup_download
}

cmd_status() {
    require_root
    echo "=== $SERVICE_NAME 状态 ==="
    systemctl status "$SERVICE_NAME.service" --no-pager || true
    echo
    echo "=== 最近 20 条日志 ==="
    journalctl -u "$SERVICE_NAME" -n 20 --no-pager || true
    echo
    if [[ -f "$INSTALL_DIR/blog-server" ]]; then
        local size mtime ver
        size=$(du -h "$INSTALL_DIR/blog-server" | cut -f1)
        mtime=$(stat -c '%y' "$INSTALL_DIR/blog-server" | cut -d. -f1)
        ver=$("$INSTALL_DIR/blog-server" -version 2>/dev/null || echo "unknown")
        echo "=== 二进制 ==="
        echo "  路径：$INSTALL_DIR/blog-server"
        echo "  版本：$ver"
        echo "  大小：$size"
        echo "  mtime：$mtime"
    fi
    # Ask the running service for its runtime-reported version via /__healthz.
    local listen_addr listen_port
    if [[ -f "$INSTALL_DIR/config.yaml" ]]; then
        listen_addr=$(awk -F'"' '/listen_addr/{print $2; exit}' "$INSTALL_DIR/config.yaml" 2>/dev/null)
        listen_port="${listen_addr##*:}"
        if [[ -n "$listen_port" ]]; then
            local healthz
            healthz=$(curl -s --max-time 2 "http://127.0.0.1:$listen_port/__healthz" 2>/dev/null || true)
            [[ -n "$healthz" ]] && echo "  运行中版本（来自 /__healthz）：${healthz}"
        fi
    fi
    echo
    if [[ -d "$INSTALL_DIR/backups" ]]; then
        echo "=== 最新备份 ==="
        ls -lh "$INSTALL_DIR/backups/" 2>/dev/null | tail -5 || true
    fi
}

cmd_uninstall() {
    require_root
    echo -e "${YELLOW}即将卸载 blog-server。${NC}"
    echo "  - 停止并禁用 systemd unit"
    echo "  - 移除 /etc/systemd/system/$SERVICE_NAME.service"
    echo "  - 保留数据目录：$INSTALL_DIR（需要删除请自行 rm -rf）"
    echo "  - 保留用户 $SERVICE_USER（系统用户才会被提示删除）"
    echo "  - 保留 Caddy（只清 Caddyfile 里的 blog-server 块）"
    if [[ "${PURGE:-0}" == "1" ]]; then
        echo -e "  - ${RED}PURGE=1：会额外删除 $INSTALL_DIR + 系统用户${NC}"
    fi
    echo
    read -rp "确认？输入 yes 继续： " yn
    [[ "$yn" == "yes" ]] || { info "已取消"; exit 0; }

    info "停止服务"
    systemctl disable --now "$SERVICE_NAME.service" 2>/dev/null || true
    rm -f "/etc/systemd/system/$SERVICE_NAME.service"
    systemctl daemon-reload

    # 从 Caddyfile 里移除 blog-server 块（保留注释/其他站点）
    local caddy_cfg=/etc/caddy/Caddyfile
    if [[ -f "$caddy_cfg" ]] && grep -q "blog-server" "$caddy_cfg"; then
        info "清理 Caddyfile 里的 blog-server 块"
        awk '
            /# blog-server/ { skip=1; next }
            skip && /^}/    { skip=0; next }
            !skip           { print }
        ' "$caddy_cfg" > "$caddy_cfg.new" && mv "$caddy_cfg.new" "$caddy_cfg"
        systemctl reload caddy || true
    fi

    # PURGE 模式：同时清数据和用户（需显式启用）
    if [[ "${PURGE:-0}" == "1" ]]; then
        if [[ -d "$INSTALL_DIR" ]]; then
            local archive="/tmp/blog-server-uninstall-$(date +%Y%m%d-%H%M%S).tar.gz"
            info "PURGE：归档数据到 $archive"
            tar czf "$archive" -C "$(dirname "$INSTALL_DIR")" "$(basename "$INSTALL_DIR")" 2>/dev/null || true
            info "PURGE：删除 $INSTALL_DIR"
            rm -rf "$INSTALL_DIR"
        fi
        # 只删除系统用户（UID < 1000）；普通登录用户保留
        if id -u "$SERVICE_USER" >/dev/null 2>&1; then
            local uid
            uid=$(id -u "$SERVICE_USER")
            if (( uid < 1000 )); then
                info "PURGE：删除系统用户 $SERVICE_USER"
                userdel "$SERVICE_USER" 2>/dev/null || true
            else
                info "保留登录用户 $SERVICE_USER（UID $uid）"
            fi
        fi
    else
        echo
        info "服务已停止；数据保留在 $INSTALL_DIR"
        info "要完全清理：sudo PURGE=1 bash $0 uninstall"
    fi

    ok "=== 卸载完成 ==="
}

cmd_help() {
    cat <<EOF
blog-server 一键安装脚本

子命令：
    install    首次安装（默认装到当前目录，下载最新 release，配 systemd + Caddy）
    update     升级到最新 release（失败自动回滚）
    status     查看服务状态 + 最近日志 + 二进制信息
    uninstall  卸载（默认只移除 systemd unit，保留数据目录给你自己管理）
    help       显示本帮助

环境变量：
    GITHUB_REPO    发布仓库，当前 $GITHUB_REPO
    RELEASE_TAG    版本标签，当前 $RELEASE_TAG
    INSTALL_DIR    安装目录，当前 $INSTALL_DIR（默认 \$(pwd)）
    SERVICE_USER   运行用户，当前 $SERVICE_USER（默认 \$SUDO_USER）
    GH_MIRROR      GitHub 下载镜像前缀（国内加速用，例 https://ghproxy.com/）
    LOCAL_ASSET    本地已有的 tarball 路径，跳过远程下载
    NO_CADDY=1     跳过 Caddy 安装与配置（已有其它反代时用）
    PURGE=1        uninstall 时同时删除数据目录和系统用户

示例：
    # 最常见：cd 到想放的目录，一行装好
    mkdir ~/blog && cd ~/blog
    sudo bash $0 install

    # 国内网络加速
    sudo GH_MIRROR=https://ghproxy.com/ bash $0 install

    # 指定版本
    sudo RELEASE_TAG=v1.0.2 bash $0 update

    # 干净卸载（保留数据）
    sudo bash $0 uninstall

    # 连数据带用户一起清
    sudo PURGE=1 bash $0 uninstall
EOF
}

# ---------- 分发 ----------
CMD="${1:-help}"
case "$CMD" in
    install)   cmd_install ;;
    update)    cmd_update ;;
    status)    cmd_status ;;
    uninstall) cmd_uninstall ;;
    help|-h|--help) cmd_help ;;
    *) err "未知子命令：$CMD"; cmd_help; exit 1 ;;
esac
