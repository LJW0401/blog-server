# blog-server 部署指南

> 从一台干净的 VPS 到线上可访问，约 **15 分钟**。
> 目标架构：Caddy（HTTPS + 反代）+ systemd（进程守护）+ blog-server（单静态二进制）。

---

## ⚡ 零、一键脚本（推荐）

如果你只想**最快上线**，用仓库自带的 `install.sh`：

```bash
# 服务器上执行（或者 scp 上去再跑）
curl -fsSL https://raw.githubusercontent.com/LJW0401/blog-server/main/deploy/install.sh -o install.sh
sudo bash install.sh install
```

脚本会自动：

1. 检查并安装依赖（curl / tar / htpasswd / gnupg）
2. 从 GitHub Releases 下最新 `blog-server-linux-{amd64|arm64}.tar.gz` 并 sha256 校验
3. 建 `blog` 用户和 `/opt/blog-server` 目录
4. 交互式问你：管理员用户名、密码（自动 bcrypt）、监听端口、可选 GitHub token
5. 写 `config.yaml`（0600）、systemd unit、启动服务
6. 安装 Caddy（如果没装）+ 交互问你域名 + 写 `Caddyfile` + reload

其它常用：

```bash
sudo bash install.sh update       # 升级到最新 release（失败自动回滚）
sudo bash install.sh status       # 服务状态 + 最近 20 条日志
sudo bash install.sh uninstall    # 卸载（默认先把数据 tar 到 /tmp）
sudo bash install.sh help         # 参数、环境变量说明
```

环境变量（按需）：

```bash
# 指定版本
sudo RELEASE_TAG=v1.0.0 bash install.sh install

# 已有反代，跳过 Caddy
sudo NO_CADDY=1 bash install.sh install

# 换安装目录
sudo INSTALL_DIR=/srv/blog bash install.sh install
```

**脚本不是魔法，而是把下面"手动"章节的每一步写成了自动化**。如果你想先理解再跑，直接读下面的手动流程。

---

## 适用前提

- Debian / Ubuntu 发行版（其他 systemd 发行版可类推）
- 已购买域名并解析 A / AAAA 记录到 VPS（例：`example.com`）
- VPS 的 `80` / `443` 端口对公网开放
- 能 `ssh <user>@<host>` 登录，有 sudo 权限

---

## 零、准备：拿到发布二进制

这份文档假设你从 GitHub Releases（或其他发布渠道）下载了预编译产物：

- `blog-server` — 静态二进制（~19 MB，无 CGO，`x86_64-linux` 架构）
- `blog-server.sha256` — 校验文件
- `config.yaml.example` — 配置模板
- `blog-server.service` — systemd unit 模板
- `Caddyfile.example` — Caddy 配置模板

把上面 5 个文件下载到**本地**一个目录，下面叫 `~/blog-server-release/`。

```bash
# 可选：校验二进制完整性
cd ~/blog-server-release
sha256sum -c blog-server.sha256
# 预期输出：blog-server: OK
```

---

## 一、本地：生成管理员密码哈希

**默认配置里的密码是 `666`，必须替换**。用 `htpasswd` 生成 bcrypt 哈希：

```bash
# 装工具（Ubuntu/Debian）
sudo apt install apache2-utils

# 生成——把 "你的管理员密码" 换成真实密码
htpasswd -bnBC 10 "" "你的管理员密码" | tr -d ':\n'
# 示例输出（每次 salt 不同）：
# $2y$10$aBcDeF.....XYZ
```

把这个字符串**完整复制下来**（以 `$2y$` 或 `$2a$`/`$2b$` 开头），稍后填进 `config.yaml`。

---

## 二、服务器：建专用用户和工作目录

```bash
ssh <user>@<host>

# 专用服务用户，不可登录，不占 home
sudo useradd -r -s /usr/sbin/nologin -d /opt/blog-server blog

# 工作目录
sudo mkdir -p /opt/blog-server
sudo chown blog:blog /opt/blog-server
sudo chmod 750 /opt/blog-server

# 应用需要的子目录
sudo -u blog mkdir -p \
  /opt/blog-server/content/docs \
  /opt/blog-server/content/projects \
  /opt/blog-server/images \
  /opt/blog-server/backups \
  /opt/blog-server/trash
```

---

## 三、上传二进制 + 配置

**本地执行**（在你下载 release 的目录）：

```bash
cd ~/blog-server-release
scp blog-server config.yaml.example blog-server.service Caddyfile.example <user>@<host>:/tmp/
```

**服务器上继续**：

```bash
# 1. 安装二进制
sudo install -m 0755 -o blog -g blog /tmp/blog-server /opt/blog-server/blog-server

# 2. 准备配置
sudo cp /tmp/config.yaml.example /tmp/config.yaml
sudo nano /tmp/config.yaml
```

**改这几处**（其它保持默认即可）：

```yaml
listen_addr: "127.0.0.1:8080"              # 保留 127.0.0.1，Caddy 反代
admin_username: "admin"                     # 可改其它
admin_password_bcrypt: "第一步生成的哈希字符串"
password_changed_at: null
data_dir: "/opt/blog-server"
github_token: ""                            # 可选：填一个 read-only PAT 避免 GitHub 限流
github_sync_interval_min: 30
```

保存退出（nano 是 `Ctrl+O`、`Enter`、`Ctrl+X`）。

```bash
# 3. 安装配置（权限 600 避免其他用户读到密码哈希）
sudo install -m 0600 -o blog -g blog /tmp/config.yaml /opt/blog-server/config.yaml

# 4. 清理临时文件
sudo rm /tmp/config.yaml
```

---

## 四、可选：灌入初始内容

如果你本地已经写了一些 Markdown 文章或项目文档，可以一把带到服务器：

```bash
# 本地
cd ~/你的内容目录     # 例如 git clone 下来的仓库
tar czf /tmp/content.tar.gz content/
scp /tmp/content.tar.gz <user>@<host>:/tmp/

# 服务器
ssh <user>@<host>
sudo -u blog tar xzf /tmp/content.tar.gz -C /opt/blog-server/
sudo rm /tmp/content.tar.gz
```

没这步也行，后续可通过 `/manage/docs/new` 直接创建。

---

## 五、systemd 守护进程

```bash
sudo cp /tmp/blog-server.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now blog-server
sudo systemctl status blog-server
```

**期望输出**：`active (running)`；有一行日志像：

```
level=INFO msg=listen addr=127.0.0.1:8080
```

**如果失败**，看详细日志：

```bash
sudo journalctl -u blog-server -n 100 --no-pager
```

最常见问题：`config.yaml` 权限错（必须是 `blog:blog`、`0600`）或密码哈希格式不对。

---

## 六、Caddy 反代 + 自动 HTTPS

### 6.1 装 Caddy（如果没装）

```bash
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https curl
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
  | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
  | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update && sudo apt install -y caddy
```

### 6.2 配置

```bash
sudo cp /tmp/Caddyfile.example /etc/caddy/Caddyfile
sudo nano /etc/caddy/Caddyfile
```

把文件里 **两处** `example.com` 改成你的真实域名，保存退出。

```bash
# 重载（Caddy 会立刻开始申请 Let's Encrypt 证书）
sudo systemctl reload caddy

# 看状态
sudo journalctl -u caddy -f
# 大约 10–30 秒后会看到 "certificate obtained successfully"
# 看到后按 Ctrl+C 退出日志
```

**如果证书申请失败**，常见原因：
- DNS 还没生效 → `dig <你的域名>` 检查是否指向当前 VPS IP
- 80 端口被占用 → `sudo ss -lnpt | grep :80`
- 防火墙 → `sudo ufw status` / `sudo iptables -L`

---

## 七、验收清单

在本地浏览器/终端执行：

```bash
# 1. 健康检查
curl https://<你的域名>/__healthz
# 预期：ok

# 2. 主页带安全头
curl -I https://<你的域名>/
# 预期包含：
#   strict-transport-security
#   content-security-policy
#   x-frame-options: DENY
#   x-content-type-options: nosniff
#   content-encoding: gzip

# 3. 文档/项目列表
curl -s -o /dev/null -w "%{http_code}\n" https://<你的域名>/docs
curl -s -o /dev/null -w "%{http_code}\n" https://<你的域名>/projects
# 预期：两个都 200

# 4. 管理登录页
curl -s -o /dev/null -w "%{http_code}\n" https://<你的域名>/manage/login
# 预期：200
```

浏览器访问 `https://<你的域名>` 应该看到主页和顶部的黄色 banner "默认密码未修改"。

---

## 八、首次登录 + 安全初始化

1. 打开 `https://<你的域名>/manage/login`
2. 用 `admin` + **第一步生成哈希对应的明文密码** 登录
3. **立刻进去改密码**（虽然已经 bcrypt 化了，但前台黄条会一直显示直到你点"修改密码"）
   - 后台 → 修改密码 → 旧密码填你当前在用的明文密码，新密码 ≥ 8 位
   - 保存后黄条消失
4. 后台 → 基本信息 → 填你的 name / tagline / 联系方式 / 关于我
5. 后台 → 文档管理 → 新建文档（写第一篇 Hello world）
6. 后台 → 项目管理 → 登记一个 GitHub 仓库试试（如果没配 token 在小量仓库下也能用）

---

## 九、升级到新版本

当你发布了新 release：

```bash
# 本地：下载新的 blog-server 二进制
cd ~/blog-server-release
# ... 替换为新版本

# 上传
scp blog-server <user>@<host>:/tmp/blog-server-new

# 服务器
ssh <user>@<host>
sudo install -m 0755 -o blog -g blog /tmp/blog-server-new /opt/blog-server/blog-server
sudo systemctl restart blog-server
sudo rm /tmp/blog-server-new

# 观察状态
sudo journalctl -u blog-server -n 20 --no-pager
```

停机时间 **< 1 秒**。数据（MD 文件 / SQLite / images）完全不受影响。

**回滚**：保留旧二进制再换，如：

```bash
sudo cp /opt/blog-server/blog-server /opt/blog-server/blog-server.v1
sudo install -m 0755 ... /opt/blog-server/blog-server   # 换成新的
# 如果要回滚：
sudo install -m 0755 -o blog -g blog /opt/blog-server/blog-server.v1 /opt/blog-server/blog-server
sudo systemctl restart blog-server
```

---

## 十、备份策略

### 10.1 内置每日冷备份（零配置）

应用每天 **03:00 本地时间** 自动打包：

- `/opt/blog-server/content/` → MD 文件
- `/opt/blog-server/images/` → 上传图片
- `/opt/blog-server/data.sqlite` → 统计、GitHub 缓存、site_settings

产物：`/opt/blog-server/backups/YYYYMMDD.tar.gz`，保留最新 7 份。

**不需要额外配置 cron**。

### 10.2 建议：异地同步

单机备份挡不住机器整体故障。加个 rsync cron：

```bash
sudo -u blog crontab -e
# 加一行：每天 04:00 把备份目录 rsync 到你另一台机器
0 4 * * * rsync -a /opt/blog-server/backups/ backup@another-host:/backup/blog/
```

前提是你在另一台机器设置了 `blog` 用户的 SSH key 可登录。

### 10.3 手动恢复演练

定期（比如每季度）做一次恢复演练：

```bash
# 在测试机上
mkdir /tmp/restore
cd /tmp/restore
tar xzf /path/to/backup/20260418.tar.gz
ls   # 应该有 content/ images/ data.sqlite
# 把这三项放到新环境的 data_dir 下，启动 blog-server 即恢复
```

---

## 十一、日志保留

blog-server 通过 `slog` JSON 输出到 stdout，被 systemd 接管进 journald：

```bash
sudo journalctl -u blog-server -f       # 跟踪
sudo journalctl -u blog-server -n 200   # 最近 200 条
sudo journalctl -u blog-server --since "2 hours ago"
sudo journalctl -u blog-server --since today | grep ERROR
```

配置 journald 保留策略（`/etc/systemd/journald.conf`）：

```
SystemMaxUse=500M
MaxRetentionSec=30d
```

改完 `sudo systemctl restart systemd-journald`。满足需求 3.5 的"30 天日志保留"。

---

## 十二、常见问题

| 症状 | 可能原因 | 解决 |
|-|-|-|
| `systemctl start blog-server` 红色 failed | config.yaml 权限/字段错 | `sudo journalctl -u blog-server -n 50 --no-pager` 看具体错误行 |
| 访问 HTTPS 域名"SSL 证书错误" | Caddy 没拿到证书 | `sudo journalctl -u caddy -n 100`；检查 DNS / 80 端口 |
| 访问 `/manage` 返回 404 | listen_addr 与 Caddy 反代地址不一致 | config.yaml 的端口应与 Caddyfile 里 `reverse_proxy 127.0.0.1:8080` 对齐 |
| 登录成功但 banner 一直黄着 | 只用了哈希登录，未通过后台改密码 | 后台 → 修改密码 → 保存一次（哪怕改成相同值） |
| 项目页 Star 数都是 0 | 没配 GitHub token，未登录限流了 | config.yaml 填 `github_token: "ghp_..."`（只读 `public_repo` scope），`systemctl restart blog-server` |
| `systemctl restart` 后主页空了 | 内容目录没带过去 | 检查 `/opt/blog-server/content/{docs,projects}/*.md` 是否存在 |
| 图片上传后前台不显示 | 静态路径权限问题 | `sudo chown -R blog:blog /opt/blog-server/images`，`chmod 644 *.png` |

---

## 十三、卸载

```bash
sudo systemctl disable --now blog-server
sudo rm /etc/systemd/system/blog-server.service
sudo systemctl daemon-reload

sudo rm -rf /opt/blog-server
sudo userdel blog

# 如果 Caddy 只是为这个站用的：
sudo rm /etc/caddy/Caddyfile
sudo systemctl reload caddy
sudo apt remove caddy
```

备份数据请提前下载：

```bash
# 最后一次归档
ssh <user>@<host> "sudo tar czf /tmp/blog-backup.tar.gz -C /opt/blog-server content images data.sqlite"
scp <user>@<host>:/tmp/blog-backup.tar.gz ~/
```

---

## 附：systemd unit 硬化选项说明

`blog-server.service` 里的 `ProtectSystem=strict`、`MemoryDenyWriteExecute=true` 等是**主动加固**的常见选项。对当前的静态 Go 二进制完全兼容。

如果未来：
- 改回 CGO（比如用 `mattn/go-sqlite3`）→ 需要去掉 `MemoryDenyWriteExecute=true`
- 用了 exec（比如调用外部程序）→ 需要调整 `SystemCallFilter`

按需调整 unit 文件后 `systemctl daemon-reload && systemctl restart blog-server`。

---

## 总结流程图

```
本地                     服务器
────                     ────
下载 release 产物
生成 bcrypt 哈希
    │
    │ scp
    ▼
                         /tmp/
                            ├─ 建 blog 用户 + /opt/blog-server
                            ├─ install blog-server 到 /opt/
                            ├─ 编辑 + install config.yaml
                            ├─ cp blog-server.service → systemd
                            ├─ enable --now blog-server
                            ├─ cp Caddyfile → /etc/caddy/
                            ├─ reload caddy（自动拿证书）
                            └─ curl 验收
    │
    ▼
浏览器访问 https://yourdomain
→ 登录 /manage → 改密码 → 开始发文章
```

Good luck！
