#!/usr/bin/env bash
# blog-server 一站式管理脚本
#
# 生命周期：
#   sudo bash manage.sh install     # 首次安装
#   sudo bash manage.sh update      # 升级到最新 release
#   sudo bash manage.sh uninstall   # 卸载（默认保留数据）
#
# 服务控制：
#   sudo bash manage.sh start       # 启动
#   sudo bash manage.sh stop        # 停止
#   sudo bash manage.sh restart     # 重启
#   sudo bash manage.sh enable      # 开机自启
#   sudo bash manage.sh disable     # 关闭开机自启
#   sudo bash manage.sh status      # 状态 + 最近日志 + 二进制信息
#   sudo bash manage.sh logs [N]    # 实时查看日志（或最近 N 行）
#   sudo bash manage.sh help
#
# 环境变量（可选）：
#   GITHUB_REPO    发布仓库，默认 LJW0401/blog-server
#   RELEASE_TAG    版本标签，默认 latest
#   INSTALL_DIR    安装目录，默认 $(pwd)
#   SERVICE_USER   运行用户，默认 $SUDO_USER
#   GH_MIRROR      GitHub 下载加速前缀（例：https://ghproxy.com/）
#   LOCAL_ASSET    本地 tarball 路径，设置后跳过远程下载（默认从网络下载）
#   NO_CADDY=1     跳过 Caddy 安装与配置
#   PURGE=1        uninstall 时同时删除数据和系统用户

set -euo pipefail

# ================== 可覆盖的默认值 ==================
GITHUB_REPO="${GITHUB_REPO:-LJW0401/blog-server}"
RELEASE_TAG="${RELEASE_TAG:-latest}"

# INSTALL_DIR 默认为调用脚本时的当前工作目录。
# 这样 cd 到你想放的地方、sudo bash manage.sh install，东西就留在原地，
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
    # MANAGE_SKIP_ROOT=1 允许测试在非 root 下跑 export/import，不要在生产使用。
    if [[ "${MANAGE_SKIP_ROOT:-0}" == "1" ]]; then return; fi
    if [[ $EUID -ne 0 ]]; then
        die "需要 root 权限，请用 sudo 运行：sudo bash $0 $CMD"
    fi
}

# 控制 service 的小包装。测试场景下可通过 MANAGE_SKIP_SYSTEMCTL=1 完全跳过，
# export/import 仍会按流程执行，但不会真的去触碰 systemd。
_svc() {
    if [[ "${MANAGE_SKIP_SYSTEMCTL:-0}" == "1" ]]; then return 0; fi
    systemctl "$@"
}
_svc_is_active() {
    if [[ "${MANAGE_SKIP_SYSTEMCTL:-0}" == "1" ]]; then return 1; fi
    systemctl is-active --quiet "$SERVICE_NAME.service"
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

    # LOCAL_ASSET 非空时跳过远程下载；默认从 GitHub Releases 拉
    if [[ -n "${LOCAL_ASSET:-}" ]]; then
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
# blog-server 生产配置（由 manage.sh 生成于 $(date -Iseconds)）
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
# ProtectHome=false: INSTALL_DIR typically lives under /home (or the caller's
# pwd). =true masks /home so systemd can't even locate the binary at ExecStart;
# =tmpfs works only on newer systemd that auto-creates bind-mount parents for
# ReadWritePaths= (older ones fail with 226/NAMESPACE). Since we already
# constrain writes via ReadWritePaths= and ProtectSystem=strict, disabling
# ProtectHome doesn't meaningfully weaken the sandbox for this service.
ProtectHome=false
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

# 为 IP 生成带 IP SAN 的自签证书（Caddy 的 `tls internal` 对 IP 支持不稳：
# 部分版本签出的 leaf 缺 SAN，或拒绝把 IP 当 SNI，导致 curl/浏览器报
# tlsv1 alert internal error。手工 openssl 签一张显式 SAN 的证书，让
# Caddy 直接加载最稳。)
generate_selfsigned_cert() {
    local ip="$1" cert_dir="/etc/caddy/certs"
    local crt="$cert_dir/blog.crt" key="$cert_dir/blog.key"

    mkdir -p "$cert_dir"
    if [[ -f "$crt" && -f "$key" ]] \
        && openssl x509 -in "$crt" -noout -ext subjectAltName 2>/dev/null \
           | grep -q "IP Address:$ip"; then
        info "自签证书已存在且包含 $ip，跳过"
    else
        info "生成带 IP SAN=$ip 的自签证书（10 年有效）"
        openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
            -subj "/CN=$ip" \
            -addext "subjectAltName=IP:$ip" \
            -keyout "$key" -out "$crt" >/dev/null 2>&1 \
            || die "openssl 签发失败（系统缺 openssl？）"
    fi
    chown caddy:caddy "$crt" "$key" 2>/dev/null || true
    chmod 644 "$crt"; chmod 600 "$key"

    # 通过全局输出变量传给调用方
    SELF_SIGNED_CRT="$crt"
    SELF_SIGNED_KEY="$key"
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
            generate_selfsigned_cert "$domain"
            domain="https://$domain"
            tls_mode="tls $SELF_SIGNED_CRT $SELF_SIGNED_KEY"
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

# blog-server (added by manage.sh on $(date -Iseconds))
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
# blog-server Caddyfile (generated by manage.sh on $(date -Iseconds))
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
            # 把归档放在 INSTALL_DIR 同级目录，而不是系统 /tmp，方便管理
            local archive
            archive="$(dirname "$INSTALL_DIR")/blog-server-uninstall-$(date +%Y%m%d-%H%M%S).tar.gz"
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

# ---------- 服务控制（薄包装 systemctl/journalctl，少打几个字） ----------
cmd_start()   { require_root; systemctl start   "$SERVICE_NAME.service" && ok "started"; }
cmd_stop()    { require_root; systemctl stop    "$SERVICE_NAME.service" && ok "stopped"; }
cmd_restart() { require_root; systemctl restart "$SERVICE_NAME.service" && ok "restarted"; }
cmd_enable()  { require_root; systemctl enable  "$SERVICE_NAME.service" && ok "enabled (开机自启)"; }
cmd_disable() { require_root; systemctl disable "$SERVICE_NAME.service" && ok "disabled"; }

cmd_logs() {
    require_root
    local n="${1:-}"
    if [[ -n "$n" ]]; then
        journalctl -u "$SERVICE_NAME" -n "$n" --no-pager
    else
        journalctl -u "$SERVICE_NAME" -f --no-pager
    fi
}

# ---------- 数据导出 / 导入 ----------
# 导出数据结构（压包成 tar.gz）：
#   blog-server-export/
#     ├── MANIFEST         固定标识行 + 元信息（版本/时间/打包项）
#     ├── data.sqlite      主库快照（为保一致性要求 service 已停或使用 --no-stop 风险自负）
#     ├── config.yaml      管理账号/GitHub token，默认包含；--no-config 可排除
#     ├── content/         Markdown 源文件（docs + projects）
#     └── images/          上传的图片
# 不含：blog-server 二进制（另装）、backups/（冗余）、trash/（软删）
EXPORT_MAGIC="blog-server-export/v1"

cmd_export() {
    require_root

    local out="" include_config=1 stop_service=1
    while (( $# > 0 )); do
        case "$1" in
            --no-config) include_config=0; shift ;;
            --no-stop)   stop_service=0;   shift ;;
            -*) die "未知选项：$1（支持 --no-config / --no-stop）" ;;
            *)  out="$1"; shift ;;
        esac
    done
    if [[ -z "$out" ]]; then
        out="$(pwd -P)/blog-server-export-$(date +%Y%m%d-%H%M%S).tar.gz"
    fi

    [[ -d "$INSTALL_DIR" ]] || die "INSTALL_DIR 不存在：$INSTALL_DIR"

    local was_active=0
    if _svc_is_active; then was_active=1; fi

    if (( stop_service )) && (( was_active )); then
        info "停止 $SERVICE_NAME.service 以获得一致的 sqlite 快照"
        _svc stop "$SERVICE_NAME.service" || warn "停止失败，继续（快照一致性可能下降）"
    elif (( was_active )); then
        warn "--no-stop：服务仍在运行，data.sqlite 快照可能不完全一致"
    fi

    local stage
    stage=$(mktemp -d)
    trap "rm -rf $stage" RETURN

    local root="$stage/blog-server-export"
    mkdir -p "$root"

    # 数据拷贝（带 . 通配空目录也 ok）
    local bundled_items=()
    if [[ -f "$INSTALL_DIR/data.sqlite" ]]; then
        cp "$INSTALL_DIR/data.sqlite" "$root/data.sqlite"
        bundled_items+=("data.sqlite")
    fi
    if (( include_config )) && [[ -f "$INSTALL_DIR/config.yaml" ]]; then
        cp "$INSTALL_DIR/config.yaml" "$root/config.yaml"
        bundled_items+=("config.yaml")
    fi
    if [[ -d "$INSTALL_DIR/content" ]]; then
        cp -a "$INSTALL_DIR/content" "$root/content"
        bundled_items+=("content/")
    fi
    if [[ -d "$INSTALL_DIR/images" ]]; then
        cp -a "$INSTALL_DIR/images" "$root/images"
        bundled_items+=("images/")
    fi

    # MANIFEST（import 用第一行魔数校验）
    cat > "$root/MANIFEST" <<EOF
$EXPORT_MAGIC
created_at: $(date -Iseconds)
hostname: $(hostname 2>/dev/null || echo unknown)
source_install_dir: $INSTALL_DIR
service_user: $SERVICE_USER
bundled: ${bundled_items[*]:-(空)}
include_config: $include_config
EOF

    info "打包到 $out"
    mkdir -p "$(dirname "$out")"
    ( cd "$stage" && tar czf "$out" blog-server-export )

    # 附带 sha256（方便跨机校验）
    if command -v sha256sum >/dev/null; then
        ( cd "$(dirname "$out")" && sha256sum "$(basename "$out")" > "$(basename "$out").sha256" )
    fi

    # 重启服务（如果我们主动停了它）
    if (( stop_service )) && (( was_active )); then
        info "重启服务"
        _svc start "$SERVICE_NAME.service" || warn "启动失败，请手动检查"
    fi

    rm -rf "$stage"
    trap - RETURN

    ok "导出完成：$out"
    info "要恢复到另一台机器："
    info "  scp $out other-host:/tmp/"
    info "  ssh other-host 'cd /path/to/install && sudo bash manage.sh import /tmp/$(basename "$out")'"
}

cmd_import() {
    require_root
    local src="${1:-}"
    [[ -n "$src" ]] || die "用法：sudo bash $0 import <bundle.tar.gz>"
    [[ -f "$src" ]] || die "文件不存在：$src"

    # SHA256 旁证（如果有同名 .sha256）
    local sha_file="${src}.sha256"
    if [[ -f "$sha_file" ]] && command -v sha256sum >/dev/null; then
        info "校验 SHA256"
        ( cd "$(dirname "$src")" && sha256sum -c "$(basename "$sha_file")" >/dev/null ) \
            || die "SHA256 校验失败：bundle 可能损坏或被篡改"
        ok "SHA256 通过"
    fi

    local stage
    stage=$(mktemp -d)
    trap "rm -rf $stage" RETURN

    info "解压到临时目录"
    tar xzf "$src" -C "$stage" || die "解包失败：不是合法的 tar.gz"

    local root="$stage/blog-server-export"
    [[ -d "$root" ]] || die "bundle 结构非法（缺少顶层 blog-server-export/ 目录），可能不是本脚本生成"
    [[ -f "$root/MANIFEST" ]] || die "缺少 MANIFEST，拒绝导入（避免把任意 tarball 解到数据目录）"
    local first_line
    first_line=$(head -1 "$root/MANIFEST")
    if [[ "$first_line" != "$EXPORT_MAGIC" ]]; then
        die "MANIFEST 魔数不匹配：期望 $EXPORT_MAGIC，实际 $first_line"
    fi
    info "MANIFEST 校验通过"
    cat "$root/MANIFEST" | sed 's/^/    /'

    # 现役数据先行备份到 /tmp，防止灾难性误操作
    local existing=()
    for item in content images data.sqlite config.yaml; do
        [[ -e "$INSTALL_DIR/$item" ]] && existing+=("$item")
    done
    if (( ${#existing[@]} > 0 )); then
        local backup_dir="$INSTALL_DIR/tmp"
        mkdir -p "$backup_dir"
        local pre_backup="$backup_dir/blog-server-preimport-$(date +%Y%m%d-%H%M%S).tar.gz"
        info "将当前数据先备份到 $pre_backup"
        ( cd "$INSTALL_DIR" && tar czf "$pre_backup" "${existing[@]}" 2>/dev/null ) \
            || warn "预备份失败，继续但无回滚"
    fi

    local was_active=0
    if _svc_is_active; then was_active=1; fi
    if (( was_active )); then
        info "停止服务以替换数据"
        _svc stop "$SERVICE_NAME.service"
    fi

    mkdir -p "$INSTALL_DIR"
    # data.sqlite：简单覆盖；同时删除 -wal/-shm 残留，否则会污染新快照
    if [[ -f "$root/data.sqlite" ]]; then
        rm -f "$INSTALL_DIR/data.sqlite" "$INSTALL_DIR/data.sqlite-wal" "$INSTALL_DIR/data.sqlite-shm"
        cp "$root/data.sqlite" "$INSTALL_DIR/data.sqlite"
        info "恢复：data.sqlite"
    fi
    if [[ -f "$root/config.yaml" ]]; then
        cp "$root/config.yaml" "$INSTALL_DIR/config.yaml"
        chmod 0600 "$INSTALL_DIR/config.yaml" 2>/dev/null || true
        info "恢复：config.yaml（权限 600）"
    fi
    if [[ -d "$root/content" ]]; then
        rm -rf "$INSTALL_DIR/content"
        cp -a "$root/content" "$INSTALL_DIR/content"
        info "恢复：content/"
    fi
    if [[ -d "$root/images" ]]; then
        rm -rf "$INSTALL_DIR/images"
        cp -a "$root/images" "$INSTALL_DIR/images"
        info "恢复：images/"
    fi

    # 修正所有权（只在有权限时）
    if id -u "$SERVICE_USER" >/dev/null 2>&1 && [[ $EUID -eq 0 ]]; then
        chown -R "$SERVICE_USER:$SERVICE_USER" "$INSTALL_DIR/content" "$INSTALL_DIR/images" \
            "$INSTALL_DIR/data.sqlite" "$INSTALL_DIR/config.yaml" 2>/dev/null || true
    fi

    if (( was_active )); then
        info "重启服务"
        _svc start "$SERVICE_NAME.service" || warn "启动失败，请手动检查 journalctl"
    fi

    rm -rf "$stage"
    trap - RETURN
    ok "=== 导入完成 ==="
    info "如果有问题要回滚，使用 $INSTALL_DIR/tmp/blog-server-preimport-*.tar.gz"
}

cmd_help() {
    cat <<EOF
blog-server 管理脚本

生命周期：
    install    首次安装（装到当前目录，下载最新 release，配 systemd + Caddy）
    update     升级到最新 release（失败自动回滚）
    uninstall  卸载（默认保留数据；PURGE=1 完全清理）

服务控制：
    start      启动服务
    stop       停止服务
    restart    重启服务
    enable     开机自启
    disable    关闭开机自启
    status     状态 + 最近日志 + 二进制信息
    logs [N]   实时查看日志；给 N 则只显示最近 N 行

数据迁移：
    export [PATH] [--no-config] [--no-stop]
               把 content/ + images/ + data.sqlite + config.yaml 打包成
               blog-server-export-<时间>.tar.gz。默认会先停服务以获得
               一致快照，完成后自动重启。附带 .sha256 方便跨机校验。
    import <PATH>
               把通过 export 产出的 bundle 恢复到本机 INSTALL_DIR。
               会先把现有数据备份到 /tmp/blog-server-preimport-*.tar.gz，
               再覆盖。自动校验 MANIFEST 魔数，防止误导入任意 tarball。

其他：
    help       显示本帮助

环境变量：
    GITHUB_REPO    发布仓库，当前 $GITHUB_REPO
    RELEASE_TAG    版本标签，当前 $RELEASE_TAG
    INSTALL_DIR    安装目录，当前 $INSTALL_DIR（默认 \$(pwd)）
    SERVICE_USER   运行用户，当前 $SERVICE_USER（默认 \$SUDO_USER）
    GH_MIRROR      GitHub 下载镜像前缀（国内加速用，例 https://ghproxy.com/）
    LOCAL_ASSET    本地已有的 tarball 路径，跳过远程下载（默认走网络）
    NO_CADDY=1     跳过 Caddy 安装与配置（已有其它反代时用）
    PURGE=1        uninstall 时同时删除数据目录和系统用户

示例：
    # 初次安装：cd 到想放的目录
    mkdir ~/blog && cd ~/blog
    sudo bash $0 install

    # 日常运维
    sudo bash $0 restart
    sudo bash $0 logs 100
    sudo bash $0 logs          # 实时跟踪

    # 国内网络加速
    sudo GH_MIRROR=https://ghproxy.com/ bash $0 install

    # 指定版本
    sudo RELEASE_TAG=v1.0.4 bash $0 update
EOF
}

# ---------- 分发 ----------
CMD="${1:-help}"
shift || true
case "$CMD" in
    install)   cmd_install ;;
    update)    cmd_update ;;
    status)    cmd_status ;;
    uninstall) cmd_uninstall ;;
    start)     cmd_start ;;
    stop)      cmd_stop ;;
    restart)   cmd_restart ;;
    enable)    cmd_enable ;;
    disable)   cmd_disable ;;
    logs)      cmd_logs "${1:-}" ;;
    export)    cmd_export "$@" ;;
    import)    cmd_import "${1:-}" ;;
    help|-h|--help) cmd_help ;;
    *) err "未知子命令：$CMD"; cmd_help; exit 1 ;;
esac
