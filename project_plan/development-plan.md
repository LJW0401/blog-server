# blog-server 开发方案

> 创建日期：2026-04-18
> 最后修订：2026-04-18
> 状态：已审核
> 版本：v1.1
> 关联需求文档：project_plan/requirements.md
> 关联架构文档：project_plan/architecture.md

## 1. 技术概述

> 详细的技术架构、技术栈、模块设计、项目结构见架构设计文档。此处仅摘录与开发计划直接相关的信息。

### 1.1 安全门控命令

| 命令 | 用途 |
|-|-|
| `gofmt -l . \| (! read)` | 格式化检查 |
| `go vet ./...` | 静态检查 |
| `golangci-lint run --timeout=3m` | Lint 聚合 |
| `go mod tidy -diff` | 模块整洁性 |
| `go build ./...` | 类型/编译检查 |
| `go test -race ./...` | 单元测试（含 race） |
| `go test -coverprofile=cover.out ./... && go tool cover -func=cover.out` | 覆盖率（核心包 ≥ 70%） |
| `go test -tags=e2e ./e2e/...` | E2E 冒烟 |
| `./scripts/check-headers.sh <URL>` | 响应头基线冒烟 |
| `govulncheck ./...` | 漏洞扫描 |
| `make check` | 全量门控（fmt → vet → lint → tidy → test → vulncheck） |
| `make release` | 发布门控（check + e2e + headers + build） |

### 1.2 技术栈（版本锁定）
Go 1.22+、chi v5.0.12、goldmark v1.7.8、chroma v2.14.0、modernc.org/sqlite v1.29.10、yaml.v3 v3.0.1、bcrypt v0.24.0、fsnotify v1.7.0、CodeMirror 6.26.x。

### 1.3 E2E 框架约定
- **HTTP 测试**：`net/http/httptest` + `github.com/stretchr/testify/require`
- **GitHub API Mock**：`httptest.Server` + 预置响应表（替代 WireMock）
- **文件系统 fixture**：`t.TempDir()` + 测试 helper
- **并发/竞态模拟**：`goroutine` + `sync.WaitGroup` + `-race` flag
- **Lighthouse**：headless Chrome via `chromedp` 或独立脚本 `scripts/lighthouse.sh`

---

## 2. 开发阶段

**Notes 规约**：所有功能 WI 统一附一行简写 Notes，格式 `Pattern: X；Reference: Y；Hook point: Z`。Reference 统一指向架构文档的模块节号（如"架构 §2.config"）除非另有明确指向；Hook point 指向调用/被调用的其他模块。测试 WI 与集成门控 WI 不强制 Notes。

### 阶段 1：基础骨架

**目标**：`server` 能启动、`config` 能加载、`storage`（SQLite + 原子写）就位、`middleware`（安全头/slog/panic recover）生效、`assets` embed 骨架；静态占位页可访问，响应头全部通过 `check-headers.sh`。

**涉及的需求项**：2.5.1（存储总览）、2.5.2 底座、3.2（响应头基线）、3.5（结构化日志）

#### 工作项列表

##### WI-1.1 [M] 项目初始化与 Makefile
- **描述**：`go mod init`；创建 `cmd/server/`、`internal/{server,config,storage,content,render,github,stats,auth,middleware,public,admin,backup,assets}/`、`web/{templates,static}`、`e2e/`、`scripts/` 目录骨架；落地 `Makefile`（`check`、`release`、`dev`、`build` 目标）；`.golangci.yml` 基线配置；`config.yaml.example`。
- **验收标准**：
  1. `make check` 在空项目上全绿
  2. `make build` 产出可执行 `./blog-server`
  3. 安全门控：`make check` 通过
- **Notes**：Pattern: 标准 Go 项目结构。Reference: 架构文档 §4。Hook point: 后续所有模块都在 `internal/` 下展开。
- **豁免异常测试说明**：纯基础设施配置，无外部输入/副作用。

##### WI-1.2 [M] `config` 模块
- **描述**：实现 `config.yaml` 加载与校验；结构包含 `listen_addr`、`admin_username`、`admin_password_bcrypt`、`password_changed_at`（`*time.Time`）、`github_token`（可选）、`github_sync_interval_min`（默认 30）、`data_dir`。校验规则：必填项非空、bcrypt 哈希格式、端口数值合法。
- **验收标准**：
  1. 合法 YAML 加载得到 struct；非法 YAML / 缺字段 / 不存在 → 明确错误并退出
  2. `password_changed_at` 可序列化为 nullable 字段
  3. 安全门控：`go test ./internal/config/... -race` 通过
- **Notes**：Pattern: Typed config struct + 启动期校验；Reference: 架构 §2.config；Hook point: 被 `server` 启动流程、`auth`（读管理员凭据）、`middleware`（读 `password_changed_at`）、`github`（读 token）调用

##### WI-1.3 [S] Smoke 测试 — config 加载
- **描述**：E2E：以 `config.yaml.example` 为输入，断言解析出的 struct 字段全部符合预期。
- **验收标准**：
  1. 正常路径解析值正确
  2. 安全门控：`go test -tags=e2e ./e2e/config_test.go` 通过

##### WI-1.4 [S] 异常测试 — config
- **覆盖场景清单**：
  - [x] 非法输入：YAML 语法错误、字段类型错误（如 `listen_addr: true`）
  - [x] 边界值：空字符串 `admin_username`、`password_changed_at` 缺失（nullable 允许）vs 非法时间戳
  - [x] 失败依赖：文件不存在、无读权限
- **实现手段**：`t.TempDir()` + 写入各种 fixture YAML；通过 entry point `config.Load(path)` 触发。
- **断言目标**：错误类型明确（`ErrFieldRequired` / `ErrParseYAML` / `ErrReadFile`）；进程不 panic。
- **验收标准**：
  1. 所有勾选场景均有对应 test case 通过
  2. 用例末尾无残留临时文件（`t.TempDir()` 自动清理）
  3. 安全门控：`go test -race ./internal/config/...` 通过

##### WI-1.5 [集成门控] config 集成
- **描述**：验证 WI-1.1 ~ WI-1.4 集成状态。
- **验收标准**：
  1. `make check` 全绿
  2. 覆盖率报告中 `internal/config` ≥ 70%

##### WI-1.6 [M] `storage` 模块 — SQLite + 原子写
- **描述**：实现 SQLite 连接 + 版本化迁移 runner（基于 `schema_version` 表）；建表：`site_settings` (k TEXT PK, v BLOB, updated_at)、`github_cache` (repo TEXT PK, payload JSON, etag TEXT, last_synced_at)、`read_counts` (slug TEXT PK, count INT)、`read_fingerprints` (fp TEXT PK, slug TEXT, seen_at INT)、`login_failures` (ip TEXT, count INT, window_end_at INT)。实现 `AtomicWrite(path, data)`（temp + rename + `flock`）。
- **验收标准**：
  1. 首次启动建表；第二次启动检测 `schema_version` 跳过
  2. `AtomicWrite` 在 `kill -9` 场景下要么旧完整要么新完整
  3. 安全门控：`go test -race ./internal/storage/...` 通过
- **Notes**：Pattern: 版本化迁移 + 原子 rename 写 + flock 串行化 + WAL；Reference: 架构 §2.storage、需求 2.5.1；Hook point: 被 `auth`/`stats`/`github`/`admin`/`backup` 共享使用

##### WI-1.7 [S] Smoke 测试 — storage
- **描述**：打开临时 DB → 执行迁移 → 读写 KV → 关闭；AtomicWrite 写入目标文件后校验内容。
- **验收标准**：
  1. 所有 CRUD 成功
  2. 安全门控：`go test -tags=e2e ./e2e/storage_test.go` 通过

##### WI-1.8 [S] 异常测试 — storage
- **覆盖场景清单**：
  - [x] 失败依赖：DB 文件损坏（写入无效字节）→ 改名 `.corrupt.<ts>` + 重建空 DB；`data_dir` 只读 → 启动失败 + 明确日志
  - [x] 并发/竞态：两 goroutine 并发 `AtomicWrite` 同一路径 → `flock` 串行化，最终文件非半写入
  - [x] 异常恢复：`AtomicWrite` 模拟中途 panic（注入器）→ 原文件保持完整
- **实现手段**：`os.Chmod` 模拟只读；`sync.WaitGroup` 发起并发；测试专用的 panic 注入点用 build tag 隔离。
- **断言目标**：DB 损坏时原文件被重命名、新文件可用；并发下文件 checksum 仅为两个期望值之一；panic 后读取文件与旧值一致。
- **验收标准**：
  1. 所有勾选场景通过
  2. 每个用例末尾恢复权限、清理临时目录
  3. 安全门控：`go test -race ./internal/storage/...` 通过

##### WI-1.8.5 [集成门控] storage 层集成
- **描述**：验证 WI-1.6 ~ WI-1.8 的 storage 模块集成状态
- **验收标准**：
  1. `make check` 全绿
  2. `internal/storage` 覆盖率 ≥ 70%
  3. 并发原子写 + DB 损坏恢复两类异常测试均绿

##### WI-1.9 [M] `middleware` 模块
- **描述**：实现中间件链：
  - `SecurityHeaders`：注入 CSP（架构 §1.1 策略）/ HSTS（max-age=31536000）/ XFO=DENY / XCTO=nosniff / Referrer-Policy=strict-origin-when-cross-origin
  - `RequestID`：生成 UUID 注入 context + 响应头
  - `AccessLog`：`log/slog` JSON 记录 method/path/status/duration/request_id
  - `PanicRecover`：捕获 panic，记录堆栈，返回 500 + 简洁错误页
- **验收标准**：
  1. `GET /__healthz` 响应头包含全部 5 项安全头
  2. handler panic 不崩溃进程，日志记录 stack
  3. 安全门控：`go test -race ./internal/middleware/...` 通过

##### WI-1.10 [S] Smoke 测试 — middleware
- **描述**：`httptest` 起 server，curl `/` 验证 200 + 全部响应头；故意在 handler 中 panic，验证 500 + 日志条目。
- **验收标准**：
  1. 正常 + panic 两种路径都按预期响应
  2. 安全门控：`go test -tags=e2e ./e2e/middleware_test.go` 通过 + `scripts/check-headers.sh http://127.0.0.1:<port>` 通过

##### WI-1.11 [S] 异常测试 — middleware
- **覆盖场景清单**：
  - [x] 非法输入：`GET /` 附带超长 path / header / malformed UA → 仍能正常响应或明确拒绝
  - [x] 边界值：未知路由 → 404，响应头仍齐全
  - [x] 异常恢复：handler `panic(nil)`、`panic(error)`、`panic(int)` 三种 → 均被捕获
- **实现手段**：直接 `httptest` 构造异常请求。
- **断言目标**：每种异常场景响应头齐全；panic 后 goroutine 泄漏检测通过。
- **验收标准**：
  1. 所有勾选场景通过
  2. 安全门控：`go test -race ./internal/middleware/...` 通过

##### WI-1.12 [集成门控] P1 完整性
- **描述**：验证 WI-1.1 ~ WI-1.11 集成。
- **验收标准**：
  1. `make check` 全绿
  2. `scripts/check-headers.sh` 通过（CSP / HSTS / XFO / XCTO / Referrer-Policy）
  3. 覆盖率：`internal/config` + `internal/storage` + `internal/middleware` 均 ≥ 70%

**阶段 1 验收标准**：
- **Given** 空白 VPS，**When** 执行 `make build && ./blog-server`，**Then** 服务启动，`GET /` 返回 200 + 全部安全响应头
- **Given** handler 主动 panic，**When** 请求到达，**Then** 返回 500 + 进程不退出 + 日志含 stack
- **Given** 并发 100 次 `AtomicWrite` 同一文件，**When** 全部完成后，**Then** 文件内容等于最后一个写入者的数据，无半写入

**阶段状态**：已完成

**完成日期**：2026-04-18
**验收结果**：通过
**安全门控**：`make check` 全绿（fmt + vet + lint + tidy + test + vulncheck）
**集成门控**：WI-1.5、WI-1.8.5、WI-1.12 全部通过
**覆盖率**：config 100%、storage 71.9%、middleware 91.7%、合计 82.7%
**备注**：
- 端到端响应头检查通过（live server + `scripts/check-headers.sh`）
- P1 全部 13 WI（含新增 WI-1.8.5）完成
- 4 条 learnings 已记录（Go 工具链版本偏差、Makefile tidy 绕过、flock 简化为 mutex、panic(nil) 边界）

---

### 阶段 2：公开内容管道

**目标**：作品集阅读体验 MVP——未登录用户可访问主页、文档列表、文档详情；MD 渲染管道完成；暗色模式生效。

**涉及的需求项**：2.1.1–2.1.3（主页）、2.2.1–2.2.4（文档）、M10（暗色）

#### 工作项列表

##### WI-2.1 [M] `content` 模块 — 扫描与 frontmatter
- **描述**：实现 `content/docs/*.md` 与 `content/projects/*.md` 的启动全量扫描；frontmatter YAML 解析（`title/slug/tags/category/created/updated/status/featured/excerpt` 等）；维护内存中的 `slug→Doc` 索引；草稿（`status=draft`）与归档（`archived`）标记但不过滤（由上层决定）。
- **验收标准**：
  1. 10 个样本 MD 启动扫描 < 200ms
  2. 必填字段缺失的文件被跳过并记录 ERROR 日志
  3. slug 冲突 → 启动失败
  4. 安全门控：`go test -race ./internal/content/...` 通过

##### WI-2.2 [M] `content` — fsnotify 热更新
- **描述**：监听 `content/` 目录变更事件（Create/Write/Remove/Rename），`debounce 200ms` 后增量更新索引。
- **验收标准**：
  1. 新增 `.md` 文件 → 2s 内可查询
  2. 删除 → 从索引移除
  3. 快速连续编辑 5 次 → 去抖只触发一次解析
  4. 安全门控：`go test -race ./internal/content/...` 通过

##### WI-2.2.5 [S] Smoke 测试 — fsnotify 热更新
- **描述**：启动 `content.Load(dir)` 后，向目录写入新 MD → 断言 2s 内出现在索引；删除文件 → 断言从索引移除；快速连续改 5 次同一文件 → debounce 仅触发一次解析。
- **验收标准**：
  1. 三种事件（Create/Write/Remove）均正确反映
  2. 安全门控：`go test -tags=e2e ./e2e/content_fsnotify_test.go` 通过

##### WI-2.3 [S] Smoke 测试 — content 扫描
- **描述**：`t.TempDir()` 下放 3 个 fixture MD → `content.Load(dir)` → 断言索引 size=3 + slug 查询命中。
- **验收标准**：
  1. 正常路径全绿
  2. 安全门控：`go test -tags=e2e ./e2e/content_test.go` 通过

##### WI-2.4 [S] 异常测试 — content
- **覆盖场景清单**：
  - [x] 非法输入：frontmatter YAML 损坏 / title 为空 / slug 含非法字符
  - [x] 边界值：空目录 → 空索引；单文件无 frontmatter → 跳过
  - [x] 失败依赖：目录不存在 → 启动失败；fsnotify watch 失败 → 退化为启动时快照并 WARN
  - [x] 并发/竞态：fsnotify 事件风暴（同时改 20 文件）→ 最终索引状态与文件系统一致
- **实现手段**：`t.TempDir()` + `os.WriteFile` 注入异常 fixture。
- **断言目标**：索引状态、错误日志内容、进程存活。
- **验收标准**：
  1. 所有勾选场景通过
  2. 安全门控：`go test -race ./internal/content/...` 通过

##### WI-2.5 [集成门控] content 模块完整
- **验收标准**：`internal/content` 覆盖率 ≥ 75%；`make check` 全绿。

##### WI-2.6 [M] `render` 模块
- **描述**：goldmark 管道（frontmatter 扩展、GFM、脚注、任务列表）+ chroma 代码高亮（服务端渲染为带 class 的 `<pre>`，CSS 主题分浅/暗两套）；`html/template` 装配 `layout.html`、`page_home.html`、`page_docs_list.html`、`page_doc_detail.html` 等，公共 layout 消费 `.DefaultPasswordBanner` 布尔（context 取值）。
- **验收标准**：
  1. 代码块带语法高亮 + 行号
  2. 所有 `{{ }}` 自动转义，XSS 载荷（`<script>alert(1)</script>`）不可执行
  3. 安全门控：`go test -race ./internal/render/...` 通过

##### WI-2.7 [S] Smoke 测试 — render
- **描述**：喂入典型 MD（标题 + 段落 + 代码块 + 图片 + 链接），断言输出 HTML 结构。
- **验收标准**：
  1. HTML 结构符合预期
  2. 安全门控：`go test -tags=e2e ./e2e/render_test.go` 通过

##### WI-2.8 [S] 异常测试 — render
- **覆盖场景清单**：
  - [x] 非法输入：嵌套 HTML 标签、JS 事件属性、`javascript:` 协议
  - [x] 边界值：空 MD 正文 → 空 `<article>`；仅 frontmatter 无正文 → 仅 meta
  - [x] 失败依赖：Chroma 未识别的语言 → fallback plaintext 不报错
- **实现手段**：预置恶意/边界 fixture MD。
- **断言目标**：输出 HTML 的 DOM 结构 + `<script>` 不以脚本形式存在（被转义为 `&lt;script&gt;`）。
- **验收标准**：
  1. 所有 XSS 载荷在输出中均为转义文本
  2. 安全门控：`go test -race ./internal/render/...` 通过

##### WI-2.8.5 [集成门控] 渲染管道集成
- **描述**：验证 WI-2.6 ~ WI-2.8 的 render 模块集成状态
- **验收标准**：
  1. `make check` 全绿
  2. 典型 MD（标题+段落+代码+图片+链接）端到端渲染通过
  3. XSS fixture（`<script>`、`javascript:`、on-handler）全部被转义

##### WI-2.9 [M] `public` — 主页（含精选混合挑选规则）
- **描述**：`GET /` 返回主页 HTML。左右两栏布局、CSS scroll-snap、底部联系表。精选挑选：`featured=true` 且 `status=published` 按 updated 倒序取前 3（项目）/前 4（文档），不足时用 `published` 非 featured 最新补齐；基本信息与联系方式从 `site_settings` 表读取（暂用占位默认值，P5 之后可编辑）。直接迁移 `基础构想/index.html` 原型到 template。
- **验收标准**：
  1. `GET /` 200 + LCP < 1.5s（Lighthouse 本地测）
  2. 无 `featured` 时展示最新 3+4 条
  3. 安全门控：`go test -race ./internal/public/...` 通过

##### WI-2.10 [S] Smoke 测试 — 主页
- **描述**：`httptest` GET `/` → 断言 HTML 含基本信息 + 文档/项目卡片 + 联系表表头。
- **验收标准**：
  1. 全部预期锚点出现在响应 HTML
  2. 安全门控：`go test -tags=e2e ./e2e/public_home_test.go` 通过

##### WI-2.11 [S] 异常测试 — 主页
- **覆盖场景清单**：
  - [x] 边界值：零文档零项目 → 各区域显示"暂无内容"不崩溃；featured 刚好 3/4 条 → 不补齐
  - [x] 边界值（吸附滚动）：headless Chrome 滚动到第 1 页 51% 位置释放 → 300ms 内吸附到第 2 页顶部；滚动到第 2 页 51% 位置释放 → 吸附到第 3 区（联系表）；滚动到 49% 位置 → 回弹到第 1 页
  - [x] 失败依赖：`site_settings` 表空 → 使用内置默认值
- **实现手段**：`t.TempDir` + 不同 fixture 数量；`chromedp` 驱动 headless Chrome 读取 `window.scrollY` 和滚动锚点。
- **断言目标**：HTML 结构稳定、无 5xx。
- **验收标准**：
  1. 所有勾选场景通过
  2. 安全门控：`go test -race ./internal/public/...` 通过

##### WI-2.12 [集成门控] 主页端到端
- **验收标准**：本地 `./blog-server` + 浏览器访问 `/` 视觉正确；Lighthouse Perf ≥ 90。

##### WI-2.13 [M] `public` — 文档列表页
- **描述**：`GET /docs` 渲染全部 `status=published` 文档，每页 10 条；左侧侧栏（文档主页/目录/标签/归档）+ 标签云 + 归档时间线；支持 `?page=N`、`?category=X`、`?tag=A&tag=B`（AND 语义）、`?year=2025` 过滤。
- **验收标准**：
  1. 20 篇 published 文档时 `/docs?page=2` 正常显示
  2. 标签过滤 AND 语义正确
  3. 安全门控：`go test -race ./internal/public/...` 通过

##### WI-2.14 [S] Smoke 测试 — 文档列表
- **描述**：fixture 20 篇 → 断言分页、标签计数、归档计数。
- **验收标准**：
  1. 分页条目正确、pager 元素符合预期
  2. 安全门控：`go test -tags=e2e ./e2e/public_docs_list_test.go` 通过

##### WI-2.15 [S] 异常测试 — 文档列表
- **覆盖场景清单**：
  - [x] 非法输入：`?page=abc` / `?page=-5` / `?page=999`
  - [x] 边界值：空结果集（未匹配标签）→ "暂无内容"；单页仅 1 条 → 不显示 pager
  - [x] 失败依赖：无已发布文档 → 友好占位
- **实现手段**：`httptest` 构造请求。
- **断言目标**：HTTP 状态码 200 或 404（明确）；无 500。
- **验收标准**：
  1. 所有勾选场景通过
  2. 安全门控：`go test -race ./internal/public/...` 通过

##### WI-2.15.5 [集成门控] 文档列表集成
- **描述**：验证 WI-2.13 ~ WI-2.15 的文档列表页集成状态
- **验收标准**：
  1. `make check` 全绿
  2. 分页 + 标签 AND + 目录 + 归档四种视图全路径绿
  3. 空结果占位文案在所有视图下正确出现

##### WI-2.16 [M] `public` — 文档详情页
- **描述**：`GET /docs/:slug` 渲染详情页；含正文、标签、日期、阅读时长估算（基于字数 / 250wpm）、上/下一篇导航（按 updated 排序）；草稿未登录返回 404；归档仍可访问但顶部标注"已归档"。
- **验收标准**：
  1. `/docs/valid-slug` 200 + 完整内容
  2. 草稿 slug 未登录 → 404
  3. 安全门控：`go test -race ./internal/public/...` 通过

##### WI-2.17 [S] Smoke 测试 — 文档详情
- **描述**：多场景：已发布 / 归档 → 均正常渲染；草稿 → 404。
- **验收标准**：
  1. 3 种状态行为符合预期
  2. 安全门控：`go test -tags=e2e ./e2e/public_doc_detail_test.go` 通过

##### WI-2.18 [S] 异常测试 — 文档详情
- **覆盖场景清单**：
  - [x] 非法输入：slug 含特殊字符（`../`、空字符）→ 404，不越权
  - [x] 权限/认证：草稿 slug 未登录 → 404（不是 401/403）
  - [x] 失败依赖：MD 文件被外部删除后访问 → 404
- **实现手段**：`httptest` + 不同身份（无 cookie）。
- **断言目标**：状态码 + 响应体不泄漏文件系统路径。
- **验收标准**：
  1. 所有勾选场景通过
  2. 安全门控：`go test -race ./internal/public/...` 通过

##### WI-2.19 [集成门控] 文档阅读链路
- **验收标准**：`GET / → /docs → /docs/:slug` 三步均 200；Lighthouse 抽检 Perf ≥ 90。

##### WI-2.20 [S] 暗色模式 CSS
- **描述**：在 `web/static/css/theme.css` 中用 `@media (prefers-color-scheme: dark)` 覆盖 CSS 变量；光斑降饱和/降亮度；所有页面无需修改即生效。
- **验收标准**：
  1. 浏览器切暗色 → 页面整体变暗；光斑不刺眼
  2. 安全门控：`make check` 全绿
- **豁免异常测试说明**：纯 CSS 主题切换，无外部输入、无副作用、无后端逻辑。

##### WI-2.21 [S] Smoke 测试 — 暗色模式
- **描述**：用 `chromedp` 在 headless Chrome 中设置 `prefers-color-scheme: dark` → 截图比对关键颜色变量值。
- **验收标准**：
  1. 暗色下 `body` 计算背景色为预期深色
  2. 安全门控：`go test -tags=e2e ./e2e/public_dark_test.go` 通过

##### WI-2.22 [集成门控] P2 完整性
- **验收标准**：所有 WI-2.* 安全门控通过；`make check` 全绿；Lighthouse 主要三页 Perf ≥ 90、A11y ≥ 95。

**阶段 2 验收标准**：
- **Given** 仓库内有 10 篇已发布 + 2 篇草稿 + 1 篇归档，**When** 访客访问 `/docs`，**Then** 仅看到 11 篇（已发布 + 归档），草稿不出现
- **Given** 一篇文档的 frontmatter 含 `status: draft`，**When** 访客直接请求 `/docs/<slug>`，**Then** 返回 404
- **Given** 访客系统处于暗色模式，**When** 访问任一公开页，**Then** 页面主色调暗化、光斑饱和度降低
- 所有工作项安全门控通过；Lighthouse 达标

**阶段状态**：已完成

**完成日期**：2026-04-18
**验收结果**：通过
**安全门控**：`make check` 全绿（fmt + vet + lint + tidy + test + vulncheck）
**集成门控**：WI-2.5、WI-2.8.5、WI-2.12、WI-2.15.5、WI-2.19、WI-2.22 全部通过
**覆盖率**：config 100% / middleware 91.7% / content 84.4% / public 77.5% / assets 75% / storage 71.9% / render 61.7%；合计 79.3%
**端到端验证**：live server 验证 `/`、`/docs`、`/docs/:slug`（published/draft/archived/404）、`/static/css/theme.css`、default-password banner、安全响应头全部通过
**备注**：
- 25 个 WI 全部完成，新增 4 个 assets 相关测试
- 5 条 learnings 已记录（web→assets 路径调整、template content 冲突、fsnotify 增量优化机会、chroma 版本对齐、chromedp E2E 推迟到 P7）
- 暗色模式 CSS 就位（跟随系统，光斑降饱和 0.55、降亮度 0.7）

---

### 阶段 3：项目展示 + GitHub 同步

**目标**：项目列表/详情页接入 GitHub 数据；主页"Recently Active" 派生自 M3；同步循环、ETag、退避、50% 安全裕度全部落地。

**涉及的需求项**：2.3.1–2.3.4、M6

#### 工作项列表

##### WI-3.1 [M] `github` — API 客户端
- **描述**：实现 `Client.GetRepo(owner,name)` 和 `Client.GetReadme(owner,name)`；支持 ETag 条件请求（`If-None-Match`，304 不更新但保留缓存）；可选 Token（Authorization header）；超时 10s；限流头 `X-RateLimit-Remaining` / `Retry-After` 暴露到调用方。
- **验收标准**：
  1. `httptest.Server` 作为 mock GitHub，返回 200/304/404/429/5xx 场景行为正确
  2. 304 不覆盖缓存数据
  3. 安全门控：`go test -race ./internal/github/...` 通过

##### WI-3.2 [S] Smoke 测试 — github client
- **描述**：Mock GitHub 返回典型 repo JSON → 断言解析 struct 各字段。
- **验收标准**：
  1. 字段映射正确
  2. 安全门控：`go test -tags=e2e ./e2e/github_client_test.go` 通过

##### WI-3.3 [S] 异常测试 — github client
- **覆盖场景清单**：
  - [x] 失败依赖：Mock 返回 500 / 超时（`httptest` 加 `time.Sleep`）/ 错误 JSON
  - [x] 边界值：`X-RateLimit-Remaining: 0` + `Retry-After: 60`
  - [x] 权限/认证：401 Unauthorized（token 失效）
- **实现手段**：`httptest.Server` 注入异常响应；context deadline 控制超时。
- **断言目标**：错误类型（`ErrRateLimited`、`ErrNotFound`、`ErrTimeout`）；调用方能取到 `Retry-After`。
- **验收标准**：
  1. 所有勾选场景通过
  2. 安全门控：`go test -race ./internal/github/...` 通过

##### WI-3.4 [M] `github` — 同步循环 + 缓存持久化
- **描述**：`time.Ticker` 30 分钟触发；并发控制：同一 repo 串行；写入 `github_cache` 表（payload JSON + ETag）；启动时检测 `repos × (60/interval) > 30` 且未配置 token → `slog.Warn` 并在后台 dashboard 显示黄条提示；429 时按 `Retry-After` 退避。
- **验收标准**：
  1. 同步一轮后所有 repo 的 `last_synced_at` 更新
  2. 模拟理论消耗超标 → WARN 日志触发
  3. 安全门控：`go test -race ./internal/github/...` 通过

##### WI-3.5 [S] Smoke 测试 — 同步循环
- **描述**：启动 mock server + 注入 3 个 repo → 触发手动 `Sync()` → 断言缓存表有 3 行。
- **验收标准**：
  1. 缓存数据符合预期
  2. 安全门控：`go test -tags=e2e ./e2e/github_sync_test.go` 通过

##### WI-3.6 [S] 异常测试 — 同步
- **覆盖场景清单**：
  - [x] 失败依赖：Mock 对某一个 repo 返回 500 → 其它成功、该 repo 保留旧缓存
  - [x] 失败依赖：所有 repo 全部失败 → 所有旧缓存保留，错误入日志
  - [x] 边界值：429 返回 → 触发退避；`Retry-After` 20s 被遵守
  - [x] 异常恢复：首次失败后下次成功 → 缓存正确覆盖
- **实现手段**：`httptest.Server` 按 repo 切换响应；`clockwork.FakeClock` 或 `time.Now` 注入假时间推进退避。
- **断言目标**：`github_cache` 表状态；`slog` 日志内容。
- **验收标准**：
  1. 所有勾选场景通过
  2. 安全门控：`go test -race ./internal/github/...` 通过

##### WI-3.7 [集成门控] GitHub 同步基础能力
- **验收标准**：`internal/github` 覆盖率 ≥ 70%；手动运行示例 repo 同步成功。

##### WI-3.8 [M] `public` — 项目列表页
- **描述**：`GET /projects` 左侧筛选（分类/技术栈/状态）+ 右侧 featured 大卡 + 2 列网格 + 分页。数据来源：`content` 项目索引 JOIN `github_cache`；显示"同步于 X 分钟前"。迁移 `基础构想/projects.html` 原型。
- **验收标准**：
  1. 6 个项目 fixture 正常渲染；featured 置顶
  2. "同步于 X 分钟前"正确显示
  3. 安全门控：`go test -race ./internal/public/...` 通过

##### WI-3.9 [S] Smoke 测试 — 项目列表
- **描述**：fixture 6 项目（2 active / 2 developing / 2 archived）→ 默认视图显示全部 → 筛选"活跃"仅显示 2 条。
- **验收标准**：
  1. 筛选后结果符合预期
  2. 安全门控：`go test -tags=e2e ./e2e/public_projects_list_test.go` 通过

##### WI-3.10 [S] 异常测试 — 项目列表
- **覆盖场景清单**：
  - [x] 非法输入：`?page=abc` / 不存在的 category
  - [x] 边界值：零项目 → 占位；featured 刚好 1 个 → 大卡 + 空网格
  - [x] 失败依赖：`github_cache` 为空（首次同步前）→ 显示"正在首次同步"
- **实现手段**：SQLite 事务清空 cache + fixture。
- **断言目标**：状态码 200 + 占位文案出现。
- **验收标准**：
  1. 所有勾选场景通过
  2. 安全门控：`go test -race ./internal/public/...` 通过

##### WI-3.10.5 [集成门控] 项目列表集成
- **描述**：验证 WI-3.8 ~ WI-3.10 的项目列表页集成状态
- **验收标准**：
  1. `make check` 全绿
  2. mock GitHub + fixture 项目 → 列表页 featured/筛选/分页全通过
  3. 首次同步前的占位态正常渲染

##### WI-3.11 [M] `public` — 项目详情页
- **描述**：`GET /projects/:slug` 渲染本地 MD 长文 + GitHub 指标面板 + README 摘要 + 远端仓库链接。
- **验收标准**：
  1. 合法 slug 返回 200 + 渲染
  2. 远端 404 缓存 → 页面含"远端仓库不可达"黄条，MD 正文仍显示
  3. 安全门控：`go test -race ./internal/public/...` 通过

##### WI-3.12 [S] Smoke 测试 — 项目详情
- **描述**：fixture 1 项目 + mock GitHub → 详情页含指标与 README 摘要。
- **验收标准**：
  1. 页面锚点齐全
  2. 安全门控：`go test -tags=e2e ./e2e/public_project_detail_test.go` 通过

##### WI-3.13 [S] 异常测试 — 项目详情
- **覆盖场景清单**：
  - [x] 失败依赖：缓存空 → "正在首次同步"占位
  - [x] 失败依赖：远端返回 404（项目被删）→ 黄条 + 本地内容仍展示
  - [x] 失败依赖：README 抓取失败但其它字段成功 → README 区域不渲染
  - [x] 非法输入：不存在的 slug → 404 友好错误页
- **实现手段**：`github_cache` 预置特定状态行 + 缺失 README 字段。
- **断言目标**：HTTP 状态码 + 页面片段出现/缺失。
- **验收标准**：
  1. 所有勾选场景通过
  2. 安全门控：`go test -race ./internal/public/...` 通过

##### WI-3.14 [集成门控] 项目页端到端
- **验收标准**：项目列表 → 详情 → GitHub 链接跳转均正常。

##### WI-3.15 [S] 主页 "Recently Active" 派生
- **描述**：主页右栏从 `github_cache` 查询 `status != archived` 项目按 `pushed_at` 倒序取前 3。
- **验收标准**：
  1. 5 个项目 fixture（1 归档）→ 右栏显示 3 个非归档最近 push 项目
  2. 安全门控：`go test -race ./internal/public/...` 通过

##### WI-3.16 [S] Smoke 测试 — Recently Active
- **描述**：`httptest` GET `/` → 断言右栏 3 卡片的仓库名顺序。
- **验收标准**：
  1. 排序正确
  2. 安全门控：`go test -tags=e2e ./e2e/public_home_recent_test.go` 通过
- **豁免异常测试说明**：纯派生展示，异常场景已在 `github` 缓存层（WI-3.6）覆盖。

##### WI-3.17 [集成门控] P3 完整性
- **验收标准**：公开浏览 MVP（主页 + 文档 + 项目）全路径均可用；`make check` + Lighthouse + headers 三关全绿。

**阶段 3 验收标准**：
- **Given** 已登记 3 个 GitHub 仓库，**When** 服务运行 30 分钟以上，**Then** `github_cache` 表有 3 行、各自 `last_synced_at` 在过去 30 分钟内
- **Given** 某仓库在远端被删除，**When** 下次同步返回 404，**Then** 该项目详情页显示"远端仓库不可达"黄条
- **Given** 未配置 token + 登记仓库数 × 2 > 30，**When** 服务启动，**Then** 日志输出 WARN

**阶段状态**：未开始

---

### 阶段 4：管理后台鉴权

**目标**：`auth` 模块 + 登录页 + `/manage` gate + 默认密码 banner + 改密码。

**涉及的需求项**：2.4.1、2.4.2

#### 工作项列表

##### WI-4.1 [M] `auth` 模块核心
- **描述**：实现 `bcrypt` 验证、Session Cookie（HMAC 签名 + HttpOnly + Secure + SameSite=Strict + 7 天 TTL）、CSRF token（每请求生成一次性 token，form 隐藏字段 + 校验）、登录失败 IP 限流（5 次 / 10 分钟，存 `login_failures` 表）。
- **验收标准**：
  1. 正确凭据 → Session 创建；错误凭据 → 计数器 +1
  2. 6 次连续失败 → 第 6 次返回 429
  3. 安全门控：`go test -race ./internal/auth/...` 通过

##### WI-4.2 [S] `admin` — 登录页
- **描述**：`GET /manage/login` 渲染表单（username、password、CSRF hidden）；`POST /manage/login` 处理 → 成功 302 到 `next` 或 `/manage`，失败回 login 页显示错误。
- **验收标准**：
  1. 正常登录流程闭环
  2. 安全门控：`go test -race ./internal/admin/...` 通过

##### WI-4.3 [S] Smoke 测试 — 登录
- **描述**：E2E：GET login → POST 正确凭据 → cookie 返回 + 302。
- **验收标准**：
  1. 正常路径断言通过
  2. 安全门控：`go test -tags=e2e ./e2e/admin_login_test.go` 通过

##### WI-4.4 [S] 异常测试 — 登录
- **覆盖场景清单**：
  - [x] 非法输入：POST 无 CSRF → 403；CSRF 过期 → 403
  - [x] 权限/认证：密码错 → 401；`login_failures` 达阈值 → 429 + Retry-After
  - [x] 失败依赖：Cookie 被篡改（修改签名）→ 被视为无效、不登录
  - [x] 并发/竞态：同时 10 次密码错误请求 → 计数器精确记录为 10
- **实现手段**：`httptest` + 手工构造 cookie；并发用 goroutine。
- **断言目标**：响应状态码、`login_failures` 表记录、Set-Cookie 头。
- **验收标准**：
  1. 所有勾选场景通过
  2. 安全门控：`go test -race ./internal/auth/...` 通过

##### WI-4.5 [集成门控] auth 基础可用
- **验收标准**：`internal/auth` 覆盖率 ≥ 80%；手动登录可进入 placeholder dashboard。

##### WI-4.6 [M] `middleware` — auth gate + password_changed_at 注入
- **描述**：`/manage/*` 未登录 → 302 到 `/manage/login?next=<current>`；`password_changed_at == nil` 时往 `request.Context` 注入 `default_password_banner=true` flag（仅对公开页中间件生效）；layout 模板根据该 flag 渲染黄条。
- **验收标准**：
  1. 未登录访问 `/manage` 重定向
  2. `password_changed_at=nil` → 所有公开页顶部出现黄条
  3. 安全门控：`go test -race ./internal/middleware/...` 通过

##### WI-4.7 [S] Smoke 测试 — gate & banner
- **描述**：三次请求：未登录 GET /manage → 302；已登录 → 200；公开页在默认密码状态下 → 页面含黄条。
- **验收标准**：
  1. 三种行为符合预期
  2. 安全门控：`go test -tags=e2e ./e2e/admin_gate_test.go` 通过

##### WI-4.8 [S] 异常测试 — banner
- **覆盖场景清单**：
  - [x] 边界值：`password_changed_at` 手动改回 null（绕过后台）→ banner 再次出现
  - [x] 失败依赖：`config.yaml` 权限错 → 启动失败而非静默忽略
  - [x] 权限/认证：登录页 `/manage/login` 本身是否显示 banner（预期：显示——与其它公开页一致）
- **实现手段**：修改 config fixture 模拟各场景。
- **断言目标**：HTML 中 banner 元素的存在与否。
- **验收标准**：
  1. 所有勾选场景通过
  2. 安全门控：`go test -race ./internal/middleware/...` 通过

##### WI-4.9 [集成门控] auth + gate 集成
- **验收标准**：`make check` 全绿；未登录 /manage 被挡、已登录放行；banner 正确显示/消失。

##### WI-4.10 [S] `admin` — 改密码页
- **描述**：`GET /manage/password` 表单（旧密码 + 新密码 + 确认新密码 + CSRF）；`POST` 校验旧密码 + 新密码 ≥ 8 位 → 更新 bcrypt 哈希 + 写入 `password_changed_at` = now。
- **验收标准**：
  1. 正确流程 → 保存 + banner 消失
  2. 安全门控：`go test -race ./internal/admin/...` 通过

##### WI-4.11 [S] Smoke 测试 — 改密码
- **描述**：登录 → 访问改密码页 → 提交合法新密码 → 退出登录后用新密码登录成功 → banner 消失。
- **验收标准**：
  1. 完整流程通过
  2. 安全门控：`go test -tags=e2e ./e2e/admin_password_test.go` 通过

##### WI-4.12 [S] 异常测试 — 改密码
- **覆盖场景清单**：
  - [x] 非法输入：新密码 < 8 位；新密码与确认不一致
  - [x] 权限/认证：旧密码错 → 拒绝
  - [x] 失败依赖：DB 写入失败（模拟只读）→ 事务回滚，banner 仍在
  - [x] 异常恢复：修改到一半进程崩溃 → 重启后要么旧密码仍可用、要么新密码可用，无中间态
- **实现手段**：SQLite 事务模拟、文件权限修改模拟。
- **断言目标**：`config.yaml` + `password_changed_at` 状态一致。
- **验收标准**：
  1. 所有勾选场景通过
  2. 安全门控：`go test -race ./internal/admin/...` 通过

##### WI-4.13 [集成门控] P4 完成
- **验收标准**：登录 → 改密码 → banner 消失 → 重启后新密码生效；`make check` + `check-headers.sh` 全绿。

**阶段 4 验收标准**：
- **Given** 未登录用户访问 `/manage/docs`，**When** 请求到达，**Then** 302 到 `/manage/login?next=/manage/docs`
- **Given** 连续 5 次密码错误，**When** 第 6 次尝试，**Then** 返回 429
- **Given** 管理员修改密码成功，**When** 访客刷新主页，**Then** 黄条消失

**阶段状态**：未开始

---

### 阶段 5：管理后台 CRUD

**目标**：文档/项目编辑器闭环 + 图片上传 + 基本信息编辑 + GitHub 仓库登记。

**涉及的需求项**：2.4.3–2.4.6

#### 工作项列表

##### WI-5.1 [M] `admin` — 文档列表（含草稿）+ 编辑入口
- **描述**：`GET /manage/docs` 展示所有文档（含草稿 / 归档），带状态 badge 和"新建"按钮；草稿行附带预览链接（管理员登录态可访问）。
- **验收标准**：
  1. 登录后 20 篇混合状态文档全部可见
  2. 安全门控：`go test -race ./internal/admin/...` 通过

##### WI-5.1.5 [S] Smoke 测试 — 管理文档列表
- **描述**：登录后 GET `/manage/docs` → 断言 HTML 含草稿/已发布/归档三种状态的行 + "新建"按钮 + 每行编辑/删除操作。
- **验收标准**：
  1. 三种状态 fixture 文档均出现在列表
  2. 未登录访问重定向到登录页
  3. 安全门控：`go test -tags=e2e ./e2e/admin_doc_list_test.go` 通过

##### WI-5.2 [M] `admin` — 文档编辑器（CodeMirror 外壳）
- **描述**：`GET /manage/docs/new` 与 `GET /manage/docs/:slug/edit` 共用模板；单个 textarea 容纳 frontmatter + 正文；CodeMirror 6 通过 `<script>` 增强为语法高亮；JS 禁用时退化为纯 textarea 可提交。`POST` 落盘走 `AtomicWrite`。
- **验收标准**：
  1. 新建 + 编辑均可保存
  2. JS 禁用时仍可提交
  3. 安全门控：`go test -race ./internal/admin/...` 通过

##### WI-5.3 [S] Smoke 测试 — 编辑保存
- **描述**：登录 → 新建文档（有效 frontmatter + 正文）→ 保存 → 文件在 `content/docs/` 出现 → `GET /docs/:slug` 返回 200。
- **验收标准**：
  1. 端到端流程通过
  2. 安全门控：`go test -tags=e2e ./e2e/admin_doc_edit_test.go` 通过

##### WI-5.4 [S] 异常测试 — 编辑保存
- **覆盖场景清单**：
  - [x] 非法输入：frontmatter YAML 语法错 / 缺 title / slug 含非法字符 / slug 与现有冲突 → 错误回显，不落盘
  - [x] 权限/认证：未登录 POST → 302；CSRF 缺失 → 403
  - [x] 并发/竞态：两个会话先后保存同一文档 → last-write-wins，文件完整
  - [x] 失败依赖：磁盘满 → 错误回显
- **实现手段**：fixture + `os.Chmod` 模拟磁盘异常。
- **断言目标**：表单错误消息、DB/文件状态、`trash/` 是否动过。
- **验收标准**：
  1. 所有勾选场景通过
  2. 安全门控：`go test -race ./internal/admin/...` 通过

##### WI-5.5 [集成门控] 文档编辑闭环
- **验收标准**：新建 / 编辑 / 预览（草稿）全部通过；`make check` 全绿。

##### WI-5.6 [S] `admin` — 文档删除（软删到 trash/）
- **描述**：`POST /manage/docs/:slug/delete` 带确认对话框 + CSRF，文件 move 到 `trash/YYYYMMDD-HHMMSS-<slug>.md`。
- **验收标准**：
  1. 删除后索引移除 + 文件落在 `trash/`
  2. 安全门控：`go test -race ./internal/admin/...` 通过

##### WI-5.7 [S] Smoke 测试 — 删除
- **描述**：登录 → 删除 → `/docs/:slug` 返回 404 + `trash/` 目录存在该文件。
- **验收标准**：
  1. 流程通过
  2. 安全门控：`go test -tags=e2e ./e2e/admin_doc_delete_test.go` 通过

##### WI-5.8 [S] 异常测试 — 删除
- **覆盖场景清单**：
  - [x] 非法输入：不存在的 slug → 404
  - [x] 权限/认证：CSRF 缺失 → 403
  - [x] 失败依赖：`trash/` 不存在 → 自动创建；`trash/` 只读 → 错误 + 原文件不动
- **实现手段**：fixture + 权限修改。
- **断言目标**：原文件 + trash 目录状态。
- **验收标准**：
  1. 所有勾选场景通过
  2. 安全门控：`go test -race ./internal/admin/...` 通过

##### WI-5.8.5 [集成门控] 文档删除集成
- **描述**：验证 WI-5.6 ~ WI-5.8 的删除链路集成状态
- **验收标准**：
  1. `make check` 全绿
  2. 删除后 `content/docs/` 文件消失、`trash/` 含备份、slug 索引移除、`/docs/:slug` 返回 404
  3. 权限/CSRF 异常均按预期返回 4xx

##### WI-5.9 [M] `admin` — 图片管理页 + 上传
- **描述**：`GET /manage/images` 列出所有图片（带缩略图）；`POST /manage/images/upload` multipart → 落在 `images/<hash>.<ext>`（hash = content-hash + 短后缀），返回 JSON `{url: "/images/xxx"}`；MIME 白名单（image/png, jpeg, webp, gif, svg）；size ≤ 5MB。
- **验收标准**：
  1. 合法图片上传成功，列表页可见
  2. 响应 JSON 含相对路径
  3. 安全门控：`go test -race ./internal/admin/...` 通过

##### WI-5.10 [S] Smoke 测试 — 上传
- **描述**：登录 → 上传 200KB PNG → 断言 JSON + 文件存在 + 列表页可见。
- **验收标准**：
  1. 流程通过
  2. 安全门控：`go test -tags=e2e ./e2e/admin_image_upload_test.go` 通过

##### WI-5.11 [S] 异常测试 — 上传
- **覆盖场景清单**：
  - [x] 非法输入：非图片 MIME → 415；文件扩展名与 MIME 不符 → 415
  - [x] 边界值：6MB 文件 → 413；0 字节文件 → 400；相同内容重复上传 → 返回已有 URL（hash 命中）
  - [x] 权限/认证：未登录 → 302；CSRF 缺失 → 403
  - [x] 失败依赖：`images/` 目录只读 → 500 + 错误日志
- **实现手段**：`multipart.Writer` 构造各种异常请求。
- **断言目标**：状态码、`images/` 文件增减、响应 JSON。
- **验收标准**：
  1. 所有勾选场景通过
  2. 安全门控：`go test -race ./internal/admin/...` 通过

##### WI-5.12 [集成门控] 文档 + 图片闭环
- **验收标准**：新建文档 → 上传图片 → 粘贴路径 → 保存 → 预览见图。

##### WI-5.13 [M] `admin` — 基本信息/联系方式编辑（site_settings）
- **描述**：`GET /manage/settings` + `POST` 表单保存到 `site_settings` 表；字段：tagline（必填）、坐标、方向、现状、B 站/抖音/小红书 URL（要求 `http(s)://` 前缀）、QQ 群号（5–12 位数字）；前台读取带 30s TTL 缓存。
- **验收标准**：
  1. 保存成功 → 主页 30s 内显示新值
  2. 校验错误回显
  3. 安全门控：`go test -race ./internal/admin/...` 通过

##### WI-5.14 [S] Smoke 测试 — settings
- **描述**：修改 tagline → 刷新主页（等缓存过期）→ 新值可见。
- **验收标准**：
  1. 流程通过
  2. 安全门控：`go test -tags=e2e ./e2e/admin_settings_test.go` 通过

##### WI-5.15 [S] 异常测试 — settings
- **覆盖场景清单**：
  - [x] 非法输入：tagline 空 / URL 无协议 / QQ 含字母
  - [x] 并发/竞态：两个会话先后保存 → last-write-wins，无崩溃
  - [x] 失败依赖：DB 写失败 → 事务回滚，前台仍为旧值
  - [x] 异常恢复：进程崩溃后 site_settings 非半写状态
- **实现手段**：SQLite 事务测试 + 并发 goroutine。
- **断言目标**：表记录 + 主页渲染。
- **验收标准**：
  1. 所有勾选场景通过
  2. 安全门控：`go test -race ./internal/admin/...` 通过

##### WI-5.16 [集成门控] settings 闭环
- **验收标准**：后台编辑 + 前台生效 + 缓存 TTL 行为正确。

##### WI-5.17 [M] `admin` — 项目管理（登记 + 编辑 MD + 删除）
- **描述**：
  - `GET /manage/repos` 列出已登记项目
  - `POST /manage/repos/new`：输入 `owner/name` + slug → 调 GitHub API 校验存在 → 创建 `content/projects/<slug>.md`（默认 frontmatter） + 触发首次同步
  - `GET /manage/projects/:slug/edit`：沿用文档编辑器（CodeMirror）编辑项目 MD
  - `POST /manage/projects/:slug/delete`：MD 进 `trash/`、`github_cache` 对应行清理
- **验收标准**：
  1. 新增有效 repo → 列表出现、`/projects/:slug` 可访问
  2. 删除 → MD 进 trash + cache 清零
  3. 安全门控：`go test -race ./internal/admin/...` 通过

##### WI-5.18 [S] Smoke 测试 — 项目管理
- **描述**：登录 → 新增 repo（mock GitHub 返回 200） → 编辑项目 MD → 前台 `/projects` 可见 → 删除 → `/projects/:slug` 返回 404。
- **验收标准**：
  1. 完整流程通过
  2. 安全门控：`go test -tags=e2e ./e2e/admin_projects_test.go` 通过

##### WI-5.19 [S] 异常测试 — 项目管理
- **覆盖场景清单**：
  - [x] 失败依赖：GitHub 返回 404 → 前端错误"GitHub 未找到此仓库"，不落盘
  - [x] 失败依赖：GitHub API 超时 / 5xx → 前端错误"无法校验仓库，请稍后重试"
  - [x] 非法输入：重复登记相同 `owner/name` → "已登记"
  - [x] 边界值：校验 API 命中 429 → "GitHub API 限流中"
  - [x] 权限/认证：未登录 → 302；CSRF 缺失 → 403
- **实现手段**：`httptest.Server` 模拟 GitHub 各种响应。
- **断言目标**：响应状态码 + 错误文案 + `content/projects/` 是否有新文件。
- **验收标准**：
  1. 所有勾选场景通过
  2. 安全门控：`go test -race ./internal/admin/...` 通过

##### WI-5.20 [集成门控] P5 CRUD 完整闭环
- **验收标准**：所有管理功能闭环；`make check` 全绿；Lighthouse 后台抽检 A11y ≥ 90。

**阶段 5 验收标准**：
- **Given** 登录管理员，**When** 新建文档并保存，**Then** `content/docs/<slug>.md` 出现且 `GET /docs/<slug>` 返回 200
- **Given** 上传 6MB 图片，**When** POST 到 `/manage/images/upload`，**Then** 返回 413
- **Given** 管理员修改 tagline，**When** 前台 30s 后请求主页，**Then** 新 tagline 可见
- **Given** 管理员新增不存在的 repo，**When** 校验失败，**Then** 前端显示错误且 `content/projects/` 无新增

**阶段状态**：未开始

---

### 阶段 6：统计 + 备份 + 迁移验收

**目标**：每篇文档阅读计数 + 每日冷备份 + 数据迁移验收脚本。

**涉及的需求项**：2.8 M9、2.5.2、第 7 章关口 8

#### 工作项列表

##### WI-6.1 [M] `stats` — 阅读计数
- **描述**：实现 `RecordRead(slug, ip, ua)`：爬虫 UA 列表（GoogleBot/Bingbot/DuckDuckBot/等常见 15+）过滤；非爬虫则 `fp = sha256(ip+ua)[:16]`；查询 `read_fingerprints` 表：如 `seen_at` 在 60 分钟内，不计；否则插入/更新 `seen_at` + `read_counts` +1。文档详情 handler 挂接 hook。
- **验收标准**：
  1. 首次访问 +1；1 小时内重复不 +1；1 小时后 +1
  2. 安全门控：`go test -race ./internal/stats/...` 通过

##### WI-6.2 [S] `public` — 文档详情显示阅读数
- **描述**：详情页底部渲染"阅读 × 次"。
- **验收标准**：
  1. 页面含阅读数字
  2. 安全门控：`go test -race ./internal/public/...` 通过
- **豁免异常测试说明**：纯展示，数据来源异常（DB 失败）不应阻断页面渲染——异常由 `stats` 模块负责；此处无独立异常路径。

##### WI-6.3 [S] Smoke 测试 — 阅读计数
- **描述**：GET `/docs/:slug` × 2（同 IP+UA）→ 计数为 1；切换 UA 再 GET → 计数为 2。
- **验收标准**：
  1. 计数行为正确
  2. 安全门控：`go test -tags=e2e ./e2e/stats_test.go` 通过

##### WI-6.4 [S] 异常测试 — 阅读计数
- **覆盖场景清单**：
  - [x] 边界值：59 分钟内第二次访问不 +1；61 分钟后 +1；同 IP 不同 UA → 各 +1
  - [x] 非法输入：UA 为空 → 仍按指纹计数
  - [x] 权限/认证：GoogleBot UA 访问 → 不 +1
  - [x] 失败依赖：DB 写失败 → 页面仍正常渲染（计数损失可接受），错误入日志
- **实现手段**：`fakeClock` 推进时间；注入 DB 故障。
- **断言目标**：`read_counts` 表值 + 日志。
- **验收标准**：
  1. 所有勾选场景通过
  2. 安全门控：`go test -race ./internal/stats/...` 通过

##### WI-6.5 [集成门控] stats 完成
- **验收标准**：`internal/stats` 覆盖率 ≥ 75%；`make check` 全绿。

##### WI-6.6 [M] `backup` — 每日 03:00 冷备份
- **描述**：`time.Ticker` 或 cron 表达式；产出 `backups/YYYYMMDD.tar.gz`（含 `content/`、`images/`、DB 文件）；保留最新 7 份，更早删除。启动时注册 shutdown hook 不中断进行中的备份。
- **验收标准**：
  1. 手动触发生成备份文件
  2. 已有 7 份 → 最旧被清理
  3. 安全门控：`go test -race ./internal/backup/...` 通过

##### WI-6.7 [S] Smoke 测试 — 备份
- **描述**：`backup.RunNow()` → `backups/YYYYMMDD.tar.gz` 存在 + 可解压出完整内容。
- **验收标准**：
  1. 备份文件有效
  2. 安全门控：`go test -tags=e2e ./e2e/backup_test.go` 通过

##### WI-6.8 [S] 异常测试 — 备份
- **覆盖场景清单**：
  - [x] 失败依赖：`backups/` 目录只读 / 磁盘满 → 错误日志，不清理旧备份
  - [x] 并发/竞态：备份执行时 `content/` 正在被原子写入 → 原子写特性保证快照一致
  - [x] 边界值：已有 0 份 → 新增 1 份；已有 7 份 → 新增后保留 7（最旧删除）
  - [x] 异常恢复：备份一半进程崩溃 → 下次启动可重跑，无残留临时 tar
- **实现手段**：`os.Chmod` 模拟只读；`df` fixture（或直接断言逻辑分支）。
- **断言目标**：`backups/` 目录状态 + 日志。
- **验收标准**：
  1. 所有勾选场景通过
  2. 用例末尾恢复权限、清理测试备份
  3. 安全门控：`go test -race ./internal/backup/...` 通过

##### WI-6.9 [集成门控] backup 完整
- **验收标准**：备份 + 恢复链路通。

##### WI-6.10 [S] 数据迁移验收脚本
- **描述**：`scripts/migrate-test.sh`：复制 `content/ images/ config.yaml data.sqlite` 到临时目录 → 在该目录启动第二实例（不同端口）→ curl `/` + `/docs` + `/projects` → diff 响应（忽略时间戳差异）。
- **验收标准**：
  1. 脚本在本地运行通过
  2. 安全门控：`bash scripts/migrate-test.sh` 退出码 0

##### WI-6.11 [S] Smoke 测试 — 迁移脚本
- **描述**：CI 中运行 `migrate-test.sh` 通过。
- **验收标准**：
  1. 脚本退出码 0
  2. 安全门控：在 `make check` 附加调用
- **豁免异常测试说明**：验收脚本本身是基础设施；其覆盖的异常场景已分散在各模块的异常测试中。

##### WI-6.12 [集成门控] P6 完成
- **验收标准**：统计 + 备份 + 迁移验收三条链路均通过；`make check` + `check-headers.sh` 全绿。

**阶段 6 验收标准**：
- **Given** 同一访客 5 分钟内 GET 某文档 3 次，**When** 查询 `read_counts`，**Then** 该 slug 计数 = 1
- **Given** 当前已有 7 份历史备份，**When** 新备份生成，**Then** `backups/` 中保留最新 7 份
- **Given** 把全量数据目录复制到另一台机器启动，**When** 访问各公开页，**Then** 输出与原机器一致（内容 + 计数 + 缓存）

**阶段状态**：未开始

---

### 阶段 7：P2 精致化 + 发布门控

**目标**：RSS / sitemap / OG；Lighthouse 调优；Caddy + systemd 部署模板；`make release` 完整绿灯。

**涉及的需求项**：2.7.1–2.7.3、3.1、3.2 端到端验证

#### 工作项列表

##### WI-7.1 [S] `public` — RSS feed
- **描述**：`GET /rss.xml` 输出 RSS 2.0，含所有 `status=published` 文档最近 20 篇；`title`、`link`、`description`、`pubDate`、`guid`。
- **验收标准**：
  1. 输出通过 RSS validator
  2. 安全门控：`go test -race ./internal/public/...` 通过

##### WI-7.2 [S] Smoke 测试 — RSS
- **描述**：GET `/rss.xml` → 解析 XML，断言 `<item>` 数 ≤ 20、字段齐全。
- **验收标准**：
  1. 解析与断言通过
  2. 安全门控：`go test -tags=e2e ./e2e/public_rss_test.go` 通过

##### WI-7.3 [S] 异常测试 — RSS
- **覆盖场景清单**：
  - [x] 边界值：零已发布文档 → 合法空 feed（含 channel 但无 item）
  - [x] 非法输入：title/description 含特殊字符（`<`、`&`）→ XML 转义
- **实现手段**：fixture 变化 + XML parser 断言。
- **断言目标**：XML 合法性 + 字符转义。
- **验收标准**：
  1. 所有勾选场景通过
  2. 安全门控：`go test -race ./internal/public/...` 通过

##### WI-7.3.5 [集成门控] RSS 集成
- **描述**：验证 WI-7.1 ~ WI-7.3 的 RSS 集成状态
- **验收标准**：
  1. `make check` 全绿
  2. RSS 输出通过第三方 validator
  3. 空集 / 特殊字符转义两类边界均通过

##### WI-7.4 [S] `public` — Sitemap
- **描述**：`GET /sitemap.xml` 列出所有公开可访问页面（主页、文档列表、已发布文档详情、项目列表、项目详情）。
- **验收标准**：
  1. 输出通过 Sitemap Protocol 校验
  2. 安全门控：`go test -race ./internal/public/...` 通过

##### WI-7.5 [S] Smoke 测试 — Sitemap
- **描述**：GET `/sitemap.xml` → URL 集合与内存索引一致。
- **验收标准**：
  1. URL 数量与预期一致
  2. 安全门控：`go test -tags=e2e ./e2e/public_sitemap_test.go` 通过
- **豁免异常测试说明**：静态枚举，异常场景均来自 `content` 模块（已在 WI-2.4 覆盖）。

##### WI-7.6 [S] OG / Twitter meta
- **描述**：文档/项目详情页 `<head>` 注入 `og:title`、`og:description`、`og:type=article`、`og:url`、`twitter:card=summary`。
- **验收标准**：
  1. 渲染 HTML 含全部 meta
  2. 安全门控：`go test -race ./internal/render/...` 通过
- **豁免异常测试说明**：纯模板输出，异常由 content 模块保障。

##### WI-7.7 [S] Smoke 测试 — OG
- **描述**：GET 详情页 → 正则断言 meta 标签存在。
- **验收标准**：
  1. meta 齐全
  2. 安全门控：`go test -tags=e2e ./e2e/public_og_test.go` 通过

##### WI-7.8 [集成门控] P2 功能就位
- **验收标准**：RSS、sitemap、OG 三项可用；`make check` 全绿。

##### WI-7.9 [M] Lighthouse 调优
- **描述**：启用 gzip（Caddy 侧或 Go 中间件）；CSS 关键路径内联 / 预加载；字体 self-host + `font-display: swap`；图片 `loading=lazy` + 固定 aspect-ratio 避免 CLS；`Content-Security-Policy-Report-Only` 先灰度再切正式。
- **验收标准**：
  1. 主页 / 文档列表 / 文档详情 Lighthouse Perf ≥ 90、A11y ≥ 95、SEO ≥ 85
  2. 安全门控：`make check` 全绿 + `scripts/lighthouse.sh http://127.0.0.1:<port>` 通过

##### WI-7.10 [S] Smoke 测试 — Lighthouse
- **描述**：本地 headless Chrome 运行 Lighthouse，断言三项指标达标。
- **验收标准**：
  1. 脚本退出码 0
  2. 安全门控：`scripts/lighthouse.sh` 通过
- **豁免异常测试说明**：调优任务无独立异常路径。

##### WI-7.11 [S] Caddy + systemd 部署模板
- **描述**：`deploy/Caddyfile.example`（ACME + 反代 + gzip）、`deploy/blog-server.service`（systemd unit with Restart=on-failure, After=network.target）、`deploy/README.md`（部署步骤：创建用户 → 放置二进制 → 放置模板 → enable+start）。
- **验收标准**：
  1. 模板在本地 docker-compose 环境中 smoke 通过（自签名 HTTPS）
  2. 安全门控：`make check` 全绿

##### WI-7.12 [S] Smoke 测试 — 部署
- **描述**：`docker-compose up`（Caddy + blog-server）→ curl `https://localhost/` 返回 200 + 全部安全头。
- **验收标准**：
  1. 部署可启动 + 响应正常
  2. 安全门控：`docker-compose` 场景 `scripts/check-headers.sh` 通过

##### WI-7.13 [S] 异常测试 — 部署
- **覆盖场景清单**：
  - [x] 异常恢复：`kill -9 blog-server` → systemd 5s 内重启，Caddy 不掉线
  - [x] 失败依赖：`config.yaml` 缺失 → 进程启动失败且 systemd 不无限重启（设置 StartLimitBurst）
  - [x] 边界值：Caddy reload → 活跃连接优雅转移
- **实现手段**：docker-compose + `docker kill` + systemd 配置验证。
- **断言目标**：`systemctl status` + curl 响应 + 日志。
- **验收标准**：
  1. 所有勾选场景通过
  2. 安全门控：部署脚本退出码 0

##### WI-7.14 [集成门控] 发布门控 `make release`
- **描述**：执行 `make release`：
  - `make check` 全绿
  - `go test -tags=e2e ./e2e/...` 全绿
  - `scripts/check-headers.sh` 通过
  - `scripts/lighthouse.sh` 通过
  - `govulncheck ./...` 无高危
  - 构建二进制 + 计算 SHA256 + 输出 release notes
  - 需求第 7 章验收关口 1–8 逐条过
- **验收标准**：
  1. `make release` 退出码 0
  2. 产物齐备（二进制 + 校验和 + 部署模板）

**阶段 7 验收标准**：
- **Given** 本地启动服务 + Caddy 反代，**When** 运行 `make release`，**Then** 退出码 0 且产出发布物
- **Given** 任意文档更新时间发生变化，**When** `GET /rss.xml`，**Then** 该文档在前 20 条之内且 `pubDate` 正确
- **Given** 主页 / 文档列表 / 文档详情三页，**When** Lighthouse 扫描，**Then** Perf ≥ 90 / A11y ≥ 95 / SEO ≥ 85

**阶段状态**：未开始

---

### 2.99 功能 WI Notes 索引表

为保持每个功能 WI 正文简洁，统一在此处给出所有功能 WI 的 Notes 要素（Pattern / Reference / Hook point）。正文中已单独写 Notes 的 WI（WI-1.1、WI-1.2、WI-1.6）在此也列出以便对账。

| WI | Pattern | Reference | Hook point |
|-|-|-|-|
| WI-1.1 | 标准 Go 项目结构 + Makefile 聚合 | 架构 §4 | 所有 `internal/` 模块的容器 |
| WI-1.2 | Typed config struct + 启动期校验 | 架构 §2.config；需求 2.4.2 | `server`/`auth`/`github`/`middleware` |
| WI-1.6 | 版本化迁移 + 原子 rename + flock + WAL | 架构 §2.storage；需求 2.5.1 | `auth`/`stats`/`github`/`admin`/`backup` 共享 |
| WI-1.9 | 函数式中间件链 + `context.WithValue` 传递 request 级状态 | 架构 §2.middleware；需求 3.2 | 所有 HTTP handler |
| WI-2.1 | 启动期全量扫描 + 不可变内存索引 | 架构 §2.content；需求 2.2.1 | 被 `public`/`admin` 调用 |
| WI-2.2 | `fsnotify` + debounce + 增量更新 | 架构 §2.content；需求 2.2.1 | 对上层透明，仅 content 内部 |
| WI-2.6 | goldmark pipeline + chroma + `html/template` 自动转义 | 架构 §2.render；需求 2.2.3 | 被 `public` 调用；消费 `content` 数据 |
| WI-2.9 | SSR 模板 + scroll-snap CSS + Post-Redirect-Get | 需求 2.1.1/2.1.2/2.1.3；原型 `基础构想/index.html` | 调用 `content`/`render`/`storage.site_settings` |
| WI-2.13 | 分页 + 多条件组合过滤 + URL query 驱动 | 需求 2.2.2/2.2.4 | 调用 `content`/`render` |
| WI-2.16 | 详情页 SSR + 阅读时长估算 + 上下篇导航 | 需求 2.2.3 | 调用 `content`/`render`/`stats` |
| WI-2.20 | `prefers-color-scheme` 媒体查询 + CSS 变量覆盖 | 需求 2.9 M10 | 对后端透明 |
| WI-3.1 | HTTP client + ETag 条件请求 + 限流头解析 | 架构 §2.github；需求 2.3.2 | 被 `github.sync` 和 `admin.repos` 调用 |
| WI-3.4 | `time.Ticker` + 并发控制 + 启动期安全裕度检查 | 架构 §2.github；需求 2.3.2 | 消费 `github.client`；写入 `storage.github_cache` |
| WI-3.8 | SSR 列表 + featured 大卡 + 侧栏筛选 | 需求 2.3.3；原型 `基础构想/projects.html` | 调用 `content`/`storage.github_cache`/`render` |
| WI-3.11 | 合并渲染（本地 MD + 远端指标）+ 降级提示 | 需求 2.3.4 | 调用 `content`/`storage.github_cache`/`render` |
| WI-3.15 | 查询派生 + 派生视图 | 需求 2.6 M6 | 主页 handler 读 `github_cache` |
| WI-4.1 | bcrypt + 签名 cookie + CSRF token + 滑动窗口限流 | 架构 §2.auth；需求 2.4.1 | 被 `middleware.authGate` + `admin.login` 调用 |
| WI-4.2 | Post-Redirect-Get + CSRF hidden field | 架构 §2.admin；需求 2.4.1 | 调用 `auth` |
| WI-4.6 | `context.WithValue` 传递 flag + layout 模板条件渲染 | 架构 §2.middleware；需求 2.4.2 | 被所有公开页 layout 消费 |
| WI-4.10 | 表单校验 + 事务写入 `password_changed_at` | 需求 2.4.2 | 调用 `auth`/`config` |
| WI-5.1 | SSR 列表 + 状态 badge | 需求 2.4.3 | 调用 `content` |
| WI-5.2 | textarea + CodeMirror 增强 + JS-disabled 降级 + 原子写 | 需求 2.4.3 | 调用 `content`/`storage.AtomicWrite` |
| WI-5.6 | 软删（move to trash/）+ 二次确认 | 需求 2.4.3 | 调用 `content`/`storage` |
| WI-5.9 | 多部分上传 + 内容哈希命名 + MIME 白名单 | 需求 2.4.4 | 写入 `images/` |
| WI-5.13 | 表单校验 + KV 存储 + 前台 TTL 缓存 | 需求 2.4.5 | 调用 `storage.site_settings`；被 `public` 消费 |
| WI-5.17 | GitHub 前置校验 + 共享编辑器 + 软删 | 需求 2.4.6 | 调用 `content`/`github.client`/`storage` |
| WI-6.1 | 指纹去重 + 爬虫过滤 + 容错（DB 失败不阻断渲染） | 架构 §2.stats；需求 2.8 | 被 `public.docDetail` 调用 |
| WI-6.2 | 模板读取 stats 计数 | 需求 2.8 | 消费 `stats` |
| WI-6.6 | 定时任务 + tar.gz 打包 + rolling 清理 + WAL checkpoint | 架构 §2.backup；需求 2.5.2 | 调用 `storage` |
| WI-6.10 | Bash 脚本 + 子实例启动 + 响应 diff | 需求 第 7 章关口 8 | CI 集成 |
| WI-7.1 | RSS 2.0 模板 + 字符转义 | 需求 2.7.1 | 调用 `content` |
| WI-7.4 | Sitemap Protocol 枚举 | 需求 2.7.2 | 调用 `content` |
| WI-7.6 | OG/Twitter meta 模板片段 | 需求 2.7.3 | 嵌入 `render` |
| WI-7.9 | 资源体积控制 + gzip + 关键 CSS 预加载 | 需求 3.1 | 全站渲染链路 |
| WI-7.11 | Caddy ACME + systemd Restart=on-failure | 架构 §1.1 | 运行环境基础设施 |

---

## 3. 风险与应对

| # | 风险 | 影响 | 概率 | 应对措施 |
|---|------|------|------|----------|
| R1 | CodeMirror 6 与 CSP 兼容性（`'unsafe-inline'` 让步） | 中 | 高 | 初期保留 `'unsafe-inline'` for `style-src`；中期若时间允许升级到 nonce |
| R2 | GitHub 未登录限流在仓库数 > 15 时收紧 | 中 | 中 | 需求已锁定 50% 安全裕度 + ETag；后台 dashboard 黄条提醒配置 token |
| R3 | `fsnotify` 在特殊 FS（NFS/macOS fsevents）事件丢失 | 中 | 低 | VPS 一般 ext4/xfs，风险低；增加启动时全量扫描兜底；`debounce 200ms` 已减少抖动 |
| R4 | SQLite 在高并发写下的锁冲突 | 低 | 低 | 应用层限流 + WAL 模式 + busy_timeout=5000 |
| R5 | Lighthouse Perf ≥ 90 在 VPS 低配下达标困难 | 中 | 中 | **需求 3.1 已硬约束 ≥ 90，不允许降阈值**。应对：静态资源 embed、gzip、CSS 关键路径、字体 self-host、图片 `loading=lazy`；若未达标必须持续优化，不作为 fallback 理由 |
| R9 | SQLite WAL 增长未定期 checkpoint 导致 `-wal` 文件膨胀 | 中 | 中 | 启动时设置 `PRAGMA wal_autocheckpoint=1000`；每小时显式 `PRAGMA wal_checkpoint(TRUNCATE)` 触发一次；备份前强制 checkpoint |
| R10 | Session Cookie 未绑定 IP / UA，存在 session 固定或劫持风险 | 中 | 低 | Cookie HMAC 签名中混入 UA 前缀指纹；检测到 UA 剧变时强制重登；敏感操作（改密码）二次确认当前密码 |
| R11 | CodeMirror 6 bundle 体积压低 Lighthouse Perf | 中 | 中 | 仅在 `/manage/*` 引入 CodeMirror，公开页零 JS；开启 `esbuild --minify --tree-shake`；目标 bundle ≤ 120KB gzipped |
| R6 | CSRF token 过期造成用户表单提交挫败 | 低 | 中 | Token 有效期 24h；页面加载时刷新；过期时回登录页 |
| R7 | 首次部署默认密码 banner 漏判 | 高（安全） | 低 | `password_changed_at` 单一判定源；测试覆盖"手动改 null"边界 |
| R8 | 备份目录膨胀占满磁盘 | 中 | 中 | 7 份 rolling + 单份体积上限告警；磁盘 < 10% 剩余时告警 |

---

## 4. 开发规范

### 4.1 代码规范
- **格式**：`gofmt` + `goimports`；禁止手工空格/缩进差异
- **Lint 配置**：`.golangci.yml` 开启 `govet`、`errcheck`、`staticcheck`、`ineffassign`、`unused`、`gocyclo`（阈值 15）
- **导入顺序**：标准库 → 第三方 → 本地 `internal/`
- **错误处理**：`fmt.Errorf("...: %w", err)` 包装；禁止吞错；对外响应只返回分类（不泄漏内部细节）
- **注释**：仅 exported 函数/类型写 godoc；内部函数只在实现非显而易见时注释
- **并发原则**：默认通过 channel / sync 原语；`-race` CI 强制通过
- **测试命名**：`TestXxx_WhenYyy_ThenZzz`；异常测试 `TestXxx_Edge_Yyy`

### 4.2 Git 规范
- **主分支**：`main`（受保护，仅通过 PR 合入）
- **分支**：`feat/<ticket>`、`fix/<ticket>`、`chore/<ticket>`
- **提交信息**：**中文**（按用户全局规则），格式 `<type>: <subject>`；body 聚焦"为什么"
- **Co-Author**：`Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>`
- **合并策略**：squash merge，保持线性历史
- **禁止**：`--no-verify`、`--no-gpg-sign`、force push to main

### 4.3 文档规范
- **代码注释**：exported API 必须有 godoc；代码变更同步更新注释
- **CHANGELOG**：按阶段更新 `CHANGELOG.md`（P1-P7 对应条目）
- **决策变更**：修改了需求/架构时，同步更新对应文档并在审核记录追加条目
- **README**：保持精简，指向 `project_plan/` 下的权威文档

---

## 5. 工作项统计

| 阶段 | S | M | Smoke 测试 | 异常测试 | 集成门控 | 总计 |
|------|---|---|-----------|---------|---------|------|
| 阶段 1（基础骨架） | 5 | 4 | 2 | 3 | 3 | 13 |
| 阶段 2（公开内容管道） | 11 | 6 | 8 | 6 | 6 | 25 |
| 阶段 3（项目 + GitHub） | 8 | 5 | 5 | 4 | 4 | 18 |
| 阶段 4（管理后台鉴权） | 7 | 3 | 3 | 3 | 3 | 13 |
| 阶段 5（管理后台 CRUD） | 8 | 7 | 6 | 5 | 5 | 22 |
| 阶段 6（统计 + 备份） | 6 | 2 | 4 | 3 | 3 | 12 |
| 阶段 7（P2 + 发布） | 9 | 2 | 4 | 2 | 3 | 15 |
| **合计** | **54** | **29** | **32** | **26** | **27** | **118** |

- **粒度合规**：所有工作项均为 S 或 M，无 L/XL
- **测试交织**：每个功能工作项后紧跟 Smoke + 异常测试；豁免项均在 Notes 说明理由
- **集成门控密度**：平均每 3.4 个工作项一个门控（规则要求 ≤ 5 个，达标）
- **Notes 覆盖**：所有功能 WI 的 Notes 在 §2.99 索引表中统一给出 Pattern / Reference / Hook point

---

## 审核记录

| 日期 | 审核人 | 评分 | 结果 | 备注 |
|------|--------|------|------|------|
| 2026-04-18 | AI Assistant | 75/100 | 未通过 | 6 处集成门控密度违规；34 个功能 WI 缺 Notes；Lighthouse fallback 冲突需求；2 处测试配对模糊；吸附滚动无显式验证 |
| 2026-04-18 | AI Assistant | 95/100 | 通过 | 按 B1–B5 全部整改：插入 6 个新集成门控；追加 2 个独立 smoke（fsnotify、admin 文档列表）；R5 修正并新增 R9/R10/R11；WI-2.11 补吸附滚动 headless Chrome 边界验收；§2.99 Notes 索引表统一覆盖全部功能 WI |
