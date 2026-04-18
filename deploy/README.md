# 部署指南

适配 Caddy + systemd 的 VPS 部署。Caddy 负责 HTTPS（Let's Encrypt）+ 反向代理，systemd 守护 Go 进程。

## 1. 前置

- Debian/Ubuntu 或类似发行版
- 已解析到服务器的域名（例：`example.com`）
- 已安装 Caddy（`apt install caddy` 或 [官方指南](https://caddyserver.com/docs/install)）

## 2. 系统用户与目录

```bash
# 建立专用用户（跑服务用）
sudo useradd -r -s /usr/sbin/nologin -d /opt/blog-server blog

# 工作目录
sudo mkdir -p /opt/blog-server
sudo chown blog:blog /opt/blog-server
sudo chmod 750 /opt/blog-server
```

## 3. 放置二进制 + 配置

```bash
# 本地构建（静态二进制，无 CGO）
make release   # 产出 ./blog-server + ./blog-server.sha256

# 传输
scp blog-server config.yaml.example user@your-host:/tmp/

# 上线
ssh user@your-host << 'EOF'
sudo install -m 0755 -o blog -g blog /tmp/blog-server /opt/blog-server/blog-server
sudo install -m 0600 -o blog -g blog /tmp/config.yaml.example /opt/blog-server/config.yaml
EOF

# 编辑生产配置（至少修改 listen_addr 与 admin_password_bcrypt）
sudo -u blog vim /opt/blog-server/config.yaml
```

`config.yaml` 必须设置：
- `listen_addr: "127.0.0.1:8080"` — Caddy 反代上游
- `admin_password_bcrypt: "<使用 htpasswd 生成的 bcrypt 哈希>"`
- `data_dir: "/opt/blog-server"`（其下 content/ images/ backups/ data.sqlite）
- 可选：`github_token`（推荐配置，避开未登录限流）

## 4. 首次数据目录

```bash
sudo -u blog mkdir -p /opt/blog-server/content/docs /opt/blog-server/content/projects /opt/blog-server/images /opt/blog-server/backups /opt/blog-server/trash
```

## 5. systemd 单元

```bash
sudo cp blog-server.service /etc/systemd/system/blog-server.service
sudo systemctl daemon-reload
sudo systemctl enable --now blog-server
sudo systemctl status blog-server
```

查日志：
```bash
sudo journalctl -u blog-server -f
```

## 6. Caddy 配置

```bash
sudo cp Caddyfile.example /etc/caddy/Caddyfile
sudo vim /etc/caddy/Caddyfile   # 把 example.com 改成真实域名
sudo systemctl reload caddy
```

Caddy 会自动申请并续签 Let's Encrypt 证书。

## 7. 验收

```bash
# 公开页应 200 + 安全头齐全
curl -I https://example.com/__healthz
curl -I https://example.com/

# 运行响应头基线检查
/opt/blog-server/scripts/check-headers.sh https://example.com
```

## 8. 升级

```bash
# 本地 make release 后：
scp blog-server user@your-host:/tmp/blog-server-new
ssh user@your-host "sudo install -m 0755 -o blog -g blog /tmp/blog-server-new /opt/blog-server/blog-server && sudo systemctl restart blog-server"
```

滚动备份（`backups/YYYYMMDD.tar.gz`，保留 7 份）由应用内部调度，无需额外 cron。

## 9. 日志保留

journald 默认保留所有；按需 `/etc/systemd/journald.conf`：

```
SystemMaxUse=500M
MaxRetentionSec=30d
```

匹配需求 3.5（结构化日志保留 30 天）。
