# blog-server 架构设计文档

> 创建日期：2026-04-18
> 最后修订：2026-04-18
> 状态：已审核
> 版本：v1.1
> 模式：轻量
> 关联需求文档：project_plan/requirements.md

## 1. 技术栈

版本锁定策略：初次开发时采用 2026-04 时点的稳定版本；升级由 `go.mod` 显式提交记录，不跟 "latest"。

| 类别 | 选择 | 版本 | 说明 |
|------|------|------|------|
| 语言 | Go | 1.22+ | 单二进制部署、VPS 友好、内存占用低 |
| HTTP 路由 | `github.com/go-chi/chi/v5` | v5.0.12 | 轻量路由器 + 中间件链；不引入重型框架 |
| HTML 模板 | `html/template`（标准库） | 随 Go 版本 | 自动转义，SSR 首选 |
| Markdown 渲染 | `github.com/yuin/goldmark` | v1.7.8 | 扩展丰富（frontmatter、脚注、GFM） |
| 代码高亮 | `github.com/alecthomas/chroma/v2` | v2.14.0 | 服务端渲染为带类名的 HTML，配合 CSS 主题；零前端 JS 依赖 |
| SQLite 驱动 | `modernc.org/sqlite` | v1.29.10 | 纯 Go 实现，无 CGO，交叉编译友好 |
| YAML 解析 | `gopkg.in/yaml.v3` | v3.0.1 | frontmatter + 配置文件 |
| 密码哈希 | `golang.org/x/crypto/bcrypt` | v0.24.0 | cost ≥ 10 |
| 结构化日志 | `log/slog`（标准库） | 随 Go 版本 | JSON handler 输出到 stdout，由 systemd-journald 接管 |
| 文件变更监听 | `github.com/fsnotify/fsnotify` | v1.7.0 | `content/` 目录热更新 |
| CSRF / Cookie | 自实现（`crypto/hmac` + `crypto/rand`） | 标准库 | 签名 Cookie + 每请求 CSRF token |
| 漏洞扫描 | `golang.org/x/vuln/cmd/govulncheck` | v1.1.3 | 构建/发布前强制通过 |
| 编辑器前端 | CodeMirror 6（本地打包嵌入） | 6.26.x | 带语法高亮的纯文本编辑器；`go:embed` 托管，不走 CDN |
| 前端样式 | 纯手写 CSS + CSS 变量 | — | 暗色模式通过 `prefers-color-scheme` 切换 CSS 变量，零 JS |
| 数据存储 | SQLite + 文件系统 | — | MD 文件 + 图片目录 + SQLite（统计/缓存/Session/运行时配置） |

**关键放弃项**（ADR 式摘要，便于未来追溯）：
- 放弃 Node.js：不想多一层运行时依赖，部署复杂度上升
- 放弃 Rust：边际收益低、构建慢
- 放弃 SPA / 前后端分离：违背作品集型 SSR 审美、对 SEO/Lighthouse 不友好、会产出双份工作
- 放弃 CGO SQLite：为保留交叉编译和"单二进制"目标

### 1.1 运行环境与基础设施

- **TLS 与反向代理**：**Caddy** 作为前置反向代理，负责 ACME（Let's Encrypt）证书自动申请/续签 与 HTTP→HTTPS 跳转。Go 服务监听 `127.0.0.1:<port>`，由 Caddy 反代至本地；应用自身不处理 TLS
- **进程管理**：systemd unit 守护 `blog-server` 进程，重启策略 `on-failure`；优雅关停由应用自行监听 `SIGTERM`
- **日志**：应用通过 `log/slog` 以 JSON 行格式输出到 stdout；由 systemd-journald 接管，配合 `logrotate` 实现 30 天滚动清理（需求 3.5）
- **CSP 策略**：
  - `default-src 'self'`、`img-src 'self' data:`、`script-src 'self'`
  - `style-src 'self' 'unsafe-inline'`——CodeMirror 6 需内联样式；后续可升级为 nonce 策略
  - `object-src 'none'`、`frame-ancestors 'none'`、`base-uri 'self'`
  - 全站无第三方源（无 CDN/字体/分析）
- **部署拓扑**：
  ```
  Internet ──HTTPS──▶ Caddy (443) ──HTTP──▶ blog-server (127.0.0.1)
                                     │
                                     └─ content/  images/  backups/  data.sqlite
  ```

---

## 2. 模块划分

按"内聚 + 单一职责"切分为 13 个 `internal/` 模块：

- **server**：进程入口、HTTP server 启动、信号处理、优雅关停
- **config**：加载/校验 `config.yaml`（**仅运维级静态参数**：监听端口、GitHub token、管理员凭据哈希、`password_changed_at` 标志位）；不承载运行时可编辑数据
- **storage**：SQLite 连接 + 迁移；文件原子写入 helper（temp + rename + flock）；托管 `site_settings` 表（KV：tagline / 坐标 / 方向 / 现状 / 联系方式链接 / QQ 群号 / featured 阈值等运行时可编辑数据）
- **content**：**启动时全量扫描 `content/docs/*.md` 与 `content/projects/*.md` 到内存索引**（slug→metadata）；`fsnotify` 监听目录变更热更新；解析 frontmatter、草稿过滤
- **render**：MD → HTML 管道（goldmark + chroma）；`html/template` 模板装配
- **github**：GitHub API 客户端、30 分钟同步循环、ETag 条件请求、限流退避、缓存读写（SQLite）
- **stats**：文档阅读计数（IP+UA 指纹 60 分钟去重、爬虫 UA 过滤、SQLite 持久化）
- **auth**：登录处理、Session cookie（HttpOnly+Secure+SameSite=Strict）、CSRF token、登录失败 IP 限流（5 次/10 分钟）
- **middleware**：安全响应头（CSP/HSTS/XFO/X-Content-Type-Options/Referrer-Policy）、`log/slog` 访问日志、panic 恢复、auth gate、**通过 `context.WithValue` 向下游传递 `default_password_banner` 标志**（由 layout 模板决定是否渲染 banner，不改写响应体）
- **public**：公开路由 handler（主页、文档列表/详情、项目列表/详情、RSS/sitemap）
- **admin**：后台路由 handler（登录页、文档/项目编辑器、图片管理、基本信息表单、GitHub 仓库登记、改密码）
- **backup**：每日 03:00 定时冷备份任务（打包 `content/` + `images/` + DB 到 `backups/YYYYMMDD.tar.gz`，保留 7 份）
- **assets**：`go:embed` 内嵌前端静态资源（CSS / CodeMirror bundle / HTML 模板）

**依赖方向**（无循环）：
- `server` → 所有启动项
- `public` / `admin` → `render`、`content`、`github`、`stats`、`auth`、`middleware`
- `auth` / `stats` / `github` / `content` → `storage`
- `middleware` → `auth`、`config`
- `backup` → `storage`

---

## 3. 接口风格

**对外接口 = 浏览器 HTTP**。无第三方 API、无 webhook、无 JSON API（除单一的图片上传）。

| 场景 | 风格 |
|-|-|
| 所有公开页 | `GET /path` → 服务端渲染 HTML |
| 管理页展示 | `GET /manage/*` → HTML（登录态必需） |
| 管理页写入 | `POST /manage/*` → `303 See Other`（Post-Redirect-Get）+ 每请求 CSRF token |
| 图片上传 | `POST /manage/images` `multipart/form-data` → JSON `{url: "/images/..."}` |
| 编辑器保存 | `POST /manage/docs/:slug`（form-urlencoded）→ 303；禁用 JS 时降级为纯 `<textarea>` 仍可提交 |
| RSS / Sitemap（P2） | `GET /rss.xml` / `GET /sitemap.xml` → XML |
| GitHub 同步 | 进程内定时任务，不对外暴露 |

**路由规划**：

- 公开：`/`、`/docs`、`/docs/:slug`、`/projects`、`/projects/:slug`、`/rss.xml`、`/sitemap.xml`
- 管理：`/manage`、`/manage/login`、`/manage/logout`
  - 文档：`/manage/docs`、`/manage/docs/new`、`/manage/docs/:slug/edit`、`/manage/docs/:slug/delete`
  - 项目：`/manage/projects`、`/manage/projects/new`、`/manage/projects/:slug/edit`、`/manage/projects/:slug/delete`
  - 图片：`/manage/images`（列表）、`/manage/images/upload`（POST）
  - 设置：`/manage/settings`（基本信息、联系方式）、`/manage/password`（改密码）
  - 仓库登记：`/manage/repos`

---

## 4. 项目结构

```
blog-server/
├── cmd/
│   └── server/
│       └── main.go              # 进程入口，装配各模块
├── internal/
│   ├── server/                  # HTTP server 启动与关停
│   ├── config/                  # 配置加载、password_changed_at 维护
│   ├── storage/                 # SQLite + 原子文件写入 helper
│   ├── content/                 # MD 扫描、frontmatter、slug 索引
│   ├── render/                  # Markdown + 模板渲染
│   ├── github/                  # GitHub API 客户端与同步循环
│   ├── stats/                   # 文档阅读计数
│   ├── auth/                    # 登录/Session/CSRF/限流
│   ├── middleware/              # 安全头、日志、auth gate、banner 注入
│   ├── public/                  # 公开页 handler
│   ├── admin/                   # 后台页 handler
│   ├── backup/                  # 每日冷备份任务
│   └── assets/                  # go:embed 静态资源入口
├── web/
│   ├── templates/               # html/template 模板文件
│   └── static/
│       ├── css/                 # 纯手写 CSS（已有 HTML 原型迁移过来）
│       └── js/                  # CodeMirror 打包产物 + 极少量增强脚本
├── e2e/                         # e2e 冒烟测试
├── scripts/
│   └── check-headers.sh         # 生产响应头验证脚本
├── content/
│   ├── docs/                    # 文档 MD 文件
│   └── projects/                # 项目 MD 文件
├── images/                      # 上传图片
├── backups/                     # 每日备份产物
├── trash/                       # 删除的 MD 暂存（软删除）
├── config.yaml.example          # 配置文件示例
├── Makefile                     # 聚合安全门控命令
├── go.mod
├── go.sum
└── README.md
```

---

## 5. 安全门控命令

统一通过 `Makefile` 聚合，CI/本地一致：

- **格式化检查**：`gofmt -l . | (! read)`
- **静态检查（vet）**：`go vet ./...`
- **Lint**：`golangci-lint run --timeout=3m`（聚合 staticcheck / errcheck / ineffassign 等）
- **模块整洁性**：`go mod tidy -diff`（确保无遗留/意外依赖）
- **类型检查（编译）**：`go build ./...`
- **单元测试**：`go test -race ./...`
- **覆盖率门槛**：`go test -coverprofile=cover.out ./... && go tool cover -func=cover.out`
  - 核心包（`auth`、`content`、`render`、`github`、`stats`）覆盖率 ≥ **70%**
- **E2E 冒烟测试**：`go test -tags=e2e ./e2e/...`
  - 覆盖：登录流程（成功/失败/限流）、MD 保存与渲染、GitHub 同步 mock、默认密码 banner 触发与消失、草稿预览鉴权
- **安全基线冒烟**：`./scripts/check-headers.sh <URL>`
  - 用 curl 验证生产响应头齐全：CSP / HSTS / X-Frame-Options / X-Content-Type-Options / Referrer-Policy / Cookie 的 HttpOnly+Secure+SameSite
- **依赖漏洞扫描**：`govulncheck ./...`
- **构建发布二进制**：`CGO_ENABLED=0 go build -ldflags="-s -w" -o blog-server ./cmd/server`
- **全量门控**：`make check`（依次执行 fmt → vet → lint → tidy → test → vulncheck）
- **发布门控**：`make release`（`check` + `e2e` + `check-headers` + 构建 + 二进制完整性校验）

---

## 审核记录

| 日期 | 审核人 | 评分 | 结果 | 备注 |
|------|--------|------|------|------|
| 2026-04-18 | AI Assistant | 91/100 | 通过 | lite 模式（阈值 70）；首轮即通过。建议项：版本号锁定、运行环境交代、模块职责细化 |
| 2026-04-18 | AI Assistant | 98/100 | 通过 | 按建议全部落地：版本锁定至具体小版本；新增 §1.1 运行环境（Caddy/slog/CSP/拓扑图）；`config` 限定到运维级静态参数，运行时配置迁至 `storage.site_settings`；`middleware` banner 改为 `context.WithValue` 传递；`content` 明确启动扫描 + `fsnotify` 热更新 |
