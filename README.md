# blog-server

一个极简取向的个人站点服务端。作品集型定位——把 **个人主页 + 博客文章 + 项目展示** 统一进一个审美一致、无第三方脚本的 SSR 站点。

单一 Go 二进制（无 CGO），VPS 友好，部署就是 `scp` + `systemctl restart`。

## 特性

- **内容**：Markdown + YAML frontmatter；`draft / published / archived` 三态；fsnotify 热加载；goldmark + chroma 代码高亮
- **项目页**：本地 MD 长文 + GitHub API 数据（Star/Fork/语言/push 时间）渲染期合并；ETag 条件请求，每 30 分钟同步，429 退避
- **管理后台**：`/manage` 服务端鉴权（HMAC 签名 Cookie + 每会话 CSRF + IP 级登录限流）；文档/项目编辑器、图片管理、站点设置、修改密码；password_changed_at banner 机制
- **私密日记**：`/diary` 复用 /manage 登录态；月历视图 + 点击折叠当周 + textarea 编辑；debounce 自动保存 + Ctrl+S + 显式保存按钮；可"转正"为 docs 草稿做后续发布；`content/diary/` 进 gitignore 不入库，公共路由硬断言保证零泄露
- **统计**：文档阅读数（IP+UA 指纹 60 分钟去重，爬虫 UA 过滤）
- **备份**：每日 03:00 tar.gz 冷备份，保留 7 份，WAL checkpoint 保证 SQLite 一致
- **发布周边**：RSS 2.0、Sitemap Protocol、OG/Twitter meta、gzip、静态资源长缓存、暗色模式（跟随系统）
- **安全基线**：CSP / HSTS / XFO / XCTO / Referrer-Policy / HttpOnly+Secure+SameSite=Strict Cookie / bcrypt

## 快速上手

```bash
# 1. 构建
export PATH=/snap/go/current/bin:$PATH
go build -o blog-server ./cmd/server

# 2. 配置
cp config.yaml.example config.yaml
# 至少修改 admin_password_bcrypt 与 listen_addr

# 3. 启动
./blog-server -config config.yaml
# 浏览器访问 http://127.0.0.1:8391/
# 管理后台：/manage/login，默认 admin / 666（生产环境立即改）
```

## 目录结构

```
blog-server/
├── cmd/server/                Go 入口 + 路由装配
├── internal/
│   ├── config/                config.yaml 加载 + password_changed_at
│   ├── storage/               SQLite + 原子文件写
│   ├── content/               MD 扫描 + frontmatter + fsnotify
│   ├── render/                goldmark + chroma + html/template
│   ├── github/                GitHub API 客户端 + 同步循环 + 缓存
│   ├── stats/                 阅读计数
│   ├── auth/                  Session + CSRF + 限流
│   ├── middleware/            安全响应头 + slog + gzip + panic recover
│   ├── public/                公开页 handler（主页/文档/项目/RSS/sitemap）
│   ├── admin/                 后台 handler（登录/文档/项目/图片/设置）
│   ├── diary/                 私密日记（/diary + JSON API + 文件系统存储）
│   ├── backup/                每日冷备份
│   ├── settings/              site_settings KV 跨包共享
│   └── assets/                go:embed 模板 + 静态资源
├── content/                   MD 文件（docs/ + projects/ + diary/<私密，gitignored>）
├── images/                    上传图片
├── backups/                   tar.gz 冷备份
├── scripts/                   check-headers.sh / lighthouse.sh / migrate-test.sh
├── deploy/                    Caddy + systemd 模板 + 部署指南
└── project_plan/              需求 / 架构 / 开发方案 / learnings
```

## 安全门控 / 发布

```bash
make check       # fmt + vet + lint + tidy + test + vulncheck
make release     # check + e2e + build + sha256
make smoke URL=http://127.0.0.1:8391   # 运行中才跑：响应头 + lighthouse + migrate-test
```

## 部署到生产

见 [deploy/README.md](deploy/README.md)——Caddy + systemd + Let's Encrypt，约 10 分钟部署完成。

## 技术栈

- Go 1.22+
- chi v5（HTTP 路由）
- html/template（模板）
- goldmark + chroma（Markdown + 代码高亮）
- modernc.org/sqlite（纯 Go，无 CGO）
- bcrypt + fsnotify + govulncheck

版本锁定细节见 [`project_plan/architecture.md`](project_plan/architecture.md)。

## 文档

| 文件 | 用途 |
|-|-|
| `project_plan/requirements.md` | 需求文档（v1.1，已审核）|
| `project_plan/architecture.md` | 架构设计（v1.1，已审核，轻量模式）|
| `project_plan/development-plan.md` | 分阶段开发方案 + 进度记录 |
| `project_plan/learnings.md` | 开发期 learnings（bug/技术债/架构洞察） |

## 许可

暂未指定。个人项目。
