# 作品集（Portfolio）架构设计文档

> 创建日期：2026-04-22
> 状态：已审核
> 版本：v1.1
> 模式：轻量
> 关联需求文档：`project_plan/portfolio-requirements.md`

## 1. 技术栈

全部沿用仓库现有栈，**零新增第三方依赖**。

| 类别 | 选择 | 版本 | 说明 |
|------|------|------|------|
| 语言 | Go | 以 `go.mod` 为准（当前 `go 1.26.2`） | 现有服务端语言 |
| HTTP | `net/http` | stdlib | 现有框架 |
| 模板 | `html/template` | stdlib | 自动转义；与 docs/projects 模板同源 |
| 文件监听 | `github.com/fsnotify/fsnotify` | v1.7.0（见 `go.mod`） | 热更作品集文件 |
| Markdown | `github.com/yuin/goldmark` + `goldmark-highlighting/v2` | goldmark v1.7.8；highlighting `v2.0.0-20230729083705-37449abec8cc` | 使用 goldmark 安全模式（不渲染原始 HTML 标签）；HTML 注释会被 parser 保留在 AST / Raw HTML 块中，解析 Intro 在模板前做（见 §2.3 `extractIntro`），不依赖 Markdown 渲染阶段 |
| 内容索引 | `internal/content`（本仓库内部包） | 现有 | 新增 portfolio 类别 + `content/portfolio/` 目录扫描 |
| 站点设置 | `modernc.org/sqlite` | v1.38.0 | 作品集自身不进 DB；md 文件是单一事实来源 |
| 前端 | 原生 JS（IIFE + `'use strict'`） | — | 封面上传复用 `avatar_upload.js` 模式 |
| 默认封面 | SVG + `//go:embed` | — | 新增 `internal/assets/static/images/portfolio-default.svg`，随二进制打包 |

## 2. 模块划分

### 2.1 新增文件

| 文件 | 职责 |
|------|------|
| `internal/public/portfolio.go` | 前台 `/portfolio` 列表（分页 + tag 筛选）与 `/portfolio/:slug` 详情处理器 |
| `internal/admin/portfolio.go` | 后台列表 / 新建 / 编辑 / 软删除 / order 更新 / featured 切换 |
| `internal/admin/portfolio_cover.go` | 封面 multipart 上传（`avatar.go` 模式），MIME 白名单 + 2MB 限制 + 磁盘 magic bytes 校验 |
| `internal/assets/templates/portfolio_list.html` | 前台列表页（gallery 卡片网格） |
| `internal/assets/templates/portfolio_detail.html` | 前台详情页（封面 + meta + body） |
| `internal/assets/templates/admin_portfolio_list.html` | 后台列表页（inline order 编辑） |
| `internal/assets/templates/admin_portfolio_edit.html` | 后台新建 / 编辑编辑器（复用 doc 编辑器的预览、保存、CSRF 骨架） |
| `internal/assets/static/js/portfolio_cover_upload.js` | 封面预览框点击上传 → 回填 URL 交互 |
| `internal/assets/static/images/portfolio-default.svg` | 默认封面（站点风格），embed 到二进制 |
| `internal/public/portfolio_test.go` 等 | Smoke + 异常 / 边界 + 隔离断言 |

### 2.2 需改动的现有文件

| 文件 | 改动要点 |
|------|----------|
| `internal/content/…` | 新增 portfolio 类别常量；`Reload` 加扫描 `content/portfolio/` 分支；frontmatter 字段扩展到作品集字段集；新增"Intro 注释块"解析（独立小函数，首次匹配 `<!-- portfolio:intro -->…<!-- /portfolio:intro -->`，失败回退空串；源文本 body 同时剔除该注释对） |
| `internal/public/home.go` | 数据准备阶段加 `FeaturedPortfolios`；排序 `order ASC, updated DESC`；零条时置 nil，模板空态隐藏整块 |
| `internal/public/rss.go` · `sitemap.go` · `tags.go` · `doc_*.go` · `projects*.go` | 显式按 Kind 过滤（加防御性 filter）；增回归测试断言引入 portfolio 后这些输出字节级不变 |
| `internal/admin/trash.go` | trash 路径按 `<DataDir>/trash/<kind>/` 子目录组织；还原按子目录分派 Kind；列表页展示 Kind 列。**兼容策略**：启动时若发现 `<DataDir>/trash/` 下存在旧版扁平文件（无 `<kind>/` 子目录），按文件名启发式归类到 `docs/`（旧版本唯一使用者），一次性迁移；迁移动作写日志，可观察可回滚 |
| `internal/admin/settings.go`（或 dashboard） | Dashboard 加作品集卡片（统计 published / draft / archived / 总数） |
| `internal/backup/*.go` · `deploy/manage.sh` | 打包清单加入 `content/portfolio/`；restore 路径同步 |
| `internal/assets/templates/layout.html` | nav 加 `<a href="/portfolio">作品集</a>`（位置：文档之后、关于之前） |
| `internal/assets/templates/home.html` | hero + 个人文档区块之后追加"作品集"区块（featured 全量展示 + "查看全部 ›"） |
| `internal/assets/templates/admin_dashboard.html` | 作品集入口卡 |
| `internal/assets/static/css/theme.css` | 列表 gallery 卡片、详情页布局、主页卡片（上半图文双栏 + 下半 Intro）、暗色变量、窄屏 <640 单列折叠 |
| `cmd/server/main.go` · `cmd/server/routes.go` | 注册新 handlers；`ph.AboutPath` 式的配置注入；路由分组 |

### 2.3 依赖走向

```
cmd/server
  ├─ public.Handlers (home, portfolio, docs, rss, sitemap, tags)
  │     └─ internal/content.Store  ←  fsnotify
  │            └─ content/portfolio/*.md  (单一事实源)
  └─ admin.Handlers (portfolio, portfolio_cover, trash, dashboard)
         ├─ internal/content.Store (invalidate on save)
         └─ 文件系统 (content/, images/, trash/)
```

## 3. 接口风格

### 3.1 前台（HTML）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/portfolio` | 列表（支持 `?tag=<name>`、`?page=<n>`，每页 20） |
| GET | `/portfolio/:slug` | 详情；slug 正则 `^[a-zA-Z0-9_-]{1,64}$`，不匹配 404 |
| GET | `/portfolio/*` 其它 | 404 |

Draft 预览：沿用 docs 机制——请求带 `X-Blog-Preview: 1` header + 通过 CSRF/登录态校验时返回 draft；否则 draft 条目在列表不出、详情 404。

### 3.2 后台（HTML + form POST，全部要求登录 + CSRF）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/manage/portfolio` | 列表（含所有状态，inline `order` 编辑控件） |
| GET | `/manage/portfolio/new` | 新建编辑器 |
| GET | `/manage/portfolio/:slug/edit` | 编辑已有条目 |
| POST | `/manage/portfolio/save` | 创建 / 更新（表单字段 + 隐藏 `original_slug` 用于重命名）；返回 302 到列表或编辑页 |
| POST | `/manage/portfolio/:slug/delete` | 软删除 → `<DataDir>/trash/portfolio/<timestamp>-<slug>.md` |
| POST | `/manage/portfolio/:slug/order` | inline 编辑 order，body: `order=<int>&csrf=<token>`；返回 JSON `{ok, error?}`；非法值返回 400 + `error` |
| POST | `/manage/portfolio/cover/upload` | multipart 上传封面；表单字段 `slug=<slug>&cover=<file>&csrf=<token>`；返回 JSON `{url}` 或 `{error}`（413 / 415 / 401 / 403） |

### 3.3 响应约定

- 列表 / 详情 HTML 页面：渲染 `html/template`，走站点统一 layout（含 nav、暗色 data-theme、OG meta）
- JSON 接口（order / cover upload）：`Content-Type: application/json`，成功 `{ok:true,...}`，失败 HTTP 4xx + `{ok:false,error:"<msg>"}`
- 全部写接口：CSRF middleware 拦截；未登录 302 `/manage/login?next=...`

### 3.4 内部接口约定

- 所有从 `content.Store` 读列表的公共路径，必须显式带"按 portfolio 类别过滤 / 排除"参数，由 Store 层或调用点都可；约定：**公共路径只通过带 Kind 参数的 API 获取数据**，不暴露"无过滤 List"。如需全量扫描（如 Dashboard 统计），显式拼多次调用。
- Intro 解析作为 `content` 包内独立 pure 函数 `extractIntro(body string) (intro, rest string)`：单元可测，失败输入返回 `""` + 原 body。

## 4. 项目结构

```
blog-server/
├── cmd/server/
│   ├── main.go          # 装配新 handlers
│   └── routes.go        # 注册 /portfolio, /manage/portfolio/** 路由
├── internal/
│   ├── content/         # ← 加 portfolio 类别 + extractIntro
│   ├── public/
│   │   ├── portfolio.go          # 新
│   │   ├── portfolio_test.go     # 新（smoke + 异常）
│   │   ├── portfolio_isolation_test.go  # 新（rss/sitemap/docs 不污染断言）
│   │   ├── home.go               # 改：加 FeaturedPortfolios
│   │   ├── rss.go / sitemap.go / tags.go  # 改：显式 Kind 过滤
│   │   └── ...
│   ├── admin/
│   │   ├── portfolio.go          # 新
│   │   ├── portfolio_cover.go    # 新
│   │   ├── portfolio_test.go     # 新
│   │   ├── trash.go              # 改：kind 子目录
│   │   └── ...
│   ├── backup/          # 改：打包清单加 content/portfolio/
│   └── assets/
│       ├── templates/
│       │   ├── portfolio_list.html       # 新
│       │   ├── portfolio_detail.html     # 新
│       │   ├── admin_portfolio_list.html # 新
│       │   ├── admin_portfolio_edit.html # 新
│       │   ├── layout.html               # 改：nav 加"作品集"
│       │   ├── home.html                 # 改：作品集区块
│       │   └── admin_dashboard.html      # 改：入口卡
│       └── static/
│           ├── css/theme.css             # 改
│           ├── js/portfolio_cover_upload.js   # 新
│           └── images/portfolio-default.svg   # 新（embed）
├── content/
│   └── portfolio/       # 新：作品集 md 文件
├── deploy/manage.sh     # 改：export 加 content/portfolio/
└── project_plan/
    ├── portfolio-requirements.md
    └── portfolio-architecture.md
```

## 5. 安全门控命令

- 代码检查：`gofmt -l .`（输出须为空）
- 静态分析：`go vet ./...`
- 全量测试：`go test ./...`
- 聚合检查：`make check`（现有目标：fmt + vet + lint + tidy + test + vulncheck）
- 发布级（含 E2E）：`make release`

---

## 6. 审核记录

| 日期 | 审核人 | 评分 | 结果 | 备注 |
|------|--------|------|------|------|
| 2026-04-22 | AI Assistant | 97/100 | 通过（轻量模式 ≥70） | v1.1：修正技术栈事实错误（移除仓库未依赖的 bluemonday/KaTeX，补齐 goldmark/fsnotify/sqlite 的 go.mod 版本号）；补 trash 旧数据兼容迁移策略 |
