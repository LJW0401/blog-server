# blog-server v1.9.4

本版本完善了日记与文档阅读体验，并建立从代码检查到双架构产物发布的自动化流程。

## 新功能与改进

- **日记状态即时同步**：保存日记后，月视图会立即更新对应日期的小绿点；保存空内容时同步移除，无需刷新页面。
- **正文图片响应式显示**：文档、项目、关于页和作品正文中的大图不再超出文字内容区，小图保持原始尺寸，缩放时维持纵横比。
- **持续集成**：Pull Request 和 `main` 推送会自动执行格式检查、静态分析、竞态测试及漏洞扫描。
- **自动发布**：推送 `v*` 标签后自动构建 Linux amd64、arm64 安装包，生成 SHA256 校验文件并发布到 GitHub Releases。

## 安全更新

- Go 从 1.26.2 升级至 1.26.5，修复标准库中的已知可达漏洞。
- Goldmark 从 1.7.8 升级至 1.7.17，修复 Markdown 链接和图片渲染相关的 XSS 漏洞。

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

## 发布附件

| 文件 | 用途 |
|------|------|
| `manage.sh` | 一键安装、升级和运维脚本 |
| `blog-server-linux-amd64.tar.gz` | Linux x86_64 安装包 |
| `blog-server-linux-amd64.tar.gz.sha256` | x86_64 安装包完整性校验 |
| `blog-server-linux-arm64.tar.gz` | Linux ARM64 安装包 |
| `blog-server-linux-arm64.tar.gz.sha256` | ARM64 安装包完整性校验 |

安装包内包含 `blog-server` 可执行文件和 `deploy/blog-server.service` systemd 服务单元。

## 完整变更

[查看 v1.9.3...v1.9.4 的完整差异](https://github.com/LJW0401/blog-server/compare/v1.9.3...v1.9.4)
