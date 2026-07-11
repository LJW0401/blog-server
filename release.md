## 安装与升级

首次安装：

```bash
mkdir -p ~/blog && cd ~/blog
curl -fsSL https://github.com/LJW0401/blog-server/releases/latest/download/manage.sh -o manage.sh
chmod +x manage.sh
sudo ./manage.sh install
```

已有部署升级：

```bash
cd /path/to/your/install
sudo ./manage.sh update
```

国内网络可在命令前设置镜像，例如：

```bash
sudo GH_MIRROR=https://ghproxy.com/ ./manage.sh update
```

## 本次更新

- 为文档和作品正文图片增加响应式最大宽度，避免图片超出文字内容区。
- 增加 GitHub Actions CI，在 Pull Request 和主分支推送时执行格式、静态检查、竞态测试与漏洞扫描。
- 增加 tag 自动发布，提供 Linux amd64、arm64 安装包及 SHA256 校验文件。
- 升级 Go 及 Goldmark 安全补丁版本，修复已知的标准库和 Markdown 渲染漏洞。

## 发布附件

| 文件 | 用途 |
|------|------|
| `manage.sh` | 一键安装、升级和运维脚本 |
| `blog-server-linux-amd64.tar.gz` | Linux x86_64 安装包 |
| `blog-server-linux-amd64.tar.gz.sha256` | x86_64 安装包完整性校验 |
| `blog-server-linux-arm64.tar.gz` | Linux ARM64 安装包 |
| `blog-server-linux-arm64.tar.gz.sha256` | ARM64 安装包完整性校验 |

安装包内包含 `blog-server` 可执行文件和 `deploy/blog-server.service` systemd 服务单元。
