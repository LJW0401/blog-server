# 日记功能 架构设计文档（轻量）

> 创建日期：2026-04-19
> 状态：已审核
> 版本：v1.1
> 模式：轻量
> 关联需求文档：project_plan/diary-requirements.md

## 1. 技术栈

**零新依赖**。一切复用 blog-server 已有栈：

| 类别 | 选择 | 版本 | 说明 |
|------|------|------|------|
| 语言 | Go | 1.22+ | 与项目主仓库一致 |
| HTTP | net/http | stdlib | 延用现有 `http.ServeMux` 路由 |
| 模板 | html/template | stdlib | 复用 `internal/render.Templates` |
| 前端 JS | 原生 JS | — | 不引框架；遵循 `<script defer>` + IIFE 先例（见 mouse-nav.js） |
| 存储 | 文件系统 | — | `content/diary/YYYY-MM-DD.md`，纯文本 body + 极简 frontmatter |
| 认证 | 复用 `internal/auth` + 现有 session cookie | — | cookie scope 已是 `/`，`/diary` 自动可读同一 session |
| CSRF | 复用 `auth.CSRFValid` | — | 写接口与 /manage 走同一套 token |
| 测试 | `go test` + 现有 assets/admin 测试约定 | — | 无 |

## 2. 模块划分

- **`internal/diary/`（新）**：日记包。包含：
  - `store.go`：文件系统操作（List 某月存在的日期、Get 某日内容、Put、Delete）
  - `handlers.go`：HTTP 处理器（页面渲染 + JSON API）
  - `calendar.go`：纯函数构造月/周日历数据结构给模板用
- **`internal/assets/templates/diary.html`（新）**：日历页面模板
- **`internal/assets/static/js/diary.js`（新）**：客户端交互（日历点击折叠、AJAX 拉/存、保存按钮 + Ctrl+S、状态反馈）。代码约定沿用 `mouse-nav.js` 的先例：单文件 IIFE 包裹 + `'use strict'`，零全局泄漏，`<script defer>` 加载
- **`internal/assets/static/css/theme.css`**：加一段 `.diary-*` 样式（日历网格、绿点、当天高亮、周视图折叠、编辑器区块）
- **`internal/assets/templates/admin_dashboard.html`**：加一张 "日记" 卡片链接到 `/diary`
- **`cmd/server/main.go`**：注册 `/diary` + `/diary/api/*` 路由，用现有 authGate 或等价 redirect 守护
- **`.gitignore`**：追加 `/content/diary/`
- **`deploy/manage.sh:cmd_export`**：已自动覆盖 `content/`（包括 `diary/`），无需改动
- **`internal/content/`**：**不改**。日记不走 content.Store，避免把 diary 引入公共 docs/projects 的扫描路径

### 转正复用
**"转正"**（2.5.1）新建 `content/docs/<slug>.md` 需要写 docs 格式的 frontmatter。可以：
- 在 `internal/diary/handlers.go` 里直接 `os.WriteFile` 一个符合 docs frontmatter 约定的字符串（最小耦合），
- 或在 `internal/content/` 里暴露一个 `CreateDoc(slug, title, category, body)` helper 函数。

MVP 先选第一种（直接写文件），让 diary 包不侵入 content 包。

## 3. 接口风格

分两类：**HTML 页面路由**（GET，渲染模板）与 **JSON API**（POST，客户端 JS 调用）。

### HTML 路由
| Method | Path | 用途 |
|--------|------|------|
| GET | `/diary` | 渲染当月日历 + 空编辑器（无日期选中） |
| GET | `/diary?year=YYYY&month=MM` | 渲染指定月份 |
| GET | `/diary?date=YYYY-MM-DD` | 渲染所在月 + 该日进入周视图模式（SSR 初始态） |

非法 `year/month` / 非法 `date` → 回落到当月（不 400）。未登录 → 302 `/manage/login?next=<原 URL>`。

### JSON API
| Method | Path | Body | 返回 | 用途 |
|--------|------|------|------|------|
| GET | `/diary/api/day?date=YYYY-MM-DD` | — | `{"body":"..."}` 或 `{"body":""}` | 切换日期时客户端拉取当天正文 |
| POST | `/diary/api/save` | form: `date`、`content`、`csrf` | `{"ok":true,"savedAt":"RFC3339"}` | 自动/手动保存 |
| POST | `/diary/api/delete` | form: `date`、`csrf` | `{"ok":true}` | 清空这一天（删文件） |
| POST | `/diary/api/promote` | form: `date`、`title`、`slug`、`category`、`csrf` | `{"ok":true,"slug":"..."}` 或 `{"ok":false,"error":"slug_conflict"}` | 转正生成 docs |

所有接口都要过 authGate + CSRF 校验（GET 不校 CSRF，POST 强制校）。

### 文件格式
```
---
date: 2026-04-19
updated_at: 2026-04-19T15:30:00+08:00
---
今天写了日记的正文……（纯文本，可能多行）
```

`date` = 文件名，冗余存一份方便手动 grep。`updated_at` 服务端在每次 save 时覆盖。

### 性能依据（对齐需求 3.1 P95 < 300ms）
- 月历渲染 = 纯 SSR，路径是"`os.Stat` × 31 (该月每一天) + html/template 渲染"；单机本地文件系统下每次 stat 亚毫秒级，总耗时远低于 100ms
- 日记 body 读 / 写 = 单次 `os.ReadFile` / `os.WriteFile`，文件普遍 < 4KB；无需索引、无需缓存
- 结论：P95 < 300ms **不需要额外优化设施**（无缓存、无预读、无连接池）

### 路径安全
所有接受 `date` 参数的接口先过 `^\d{4}-\d{2}-\d{2}$` 正则校验 + `time.Parse("2006-01-02", date)` 二次校验合法性，拒绝穿越（`..`、绝对路径、空字节等）。拒绝走 400。

## 4. 项目结构

```
blog-server/
├── cmd/server/
│   └── main.go                              # + /diary 路由注册
├── internal/
│   ├── diary/                               # ← 新
│   │   ├── store.go
│   │   ├── store_test.go
│   │   ├── calendar.go
│   │   ├── calendar_test.go
│   │   ├── handlers.go
│   │   └── handlers_test.go
│   ├── admin/                               # （2.6.2 仪表盘卡片改这里）
│   ├── public/                              # 不动
│   ├── content/                             # 不动
│   └── assets/
│       ├── templates/
│       │   ├── diary.html                   # ← 新
│       │   └── admin_dashboard.html         # （加日记卡片）
│       └── static/
│           ├── css/theme.css                # （加 .diary-* 样式）
│           └── js/diary.js                  # ← 新
├── content/
│   └── diary/                               # ← 新（运行时生成，.gitignore）
│       └── YYYY-MM-DD.md
├── project_plan/
│   ├── diary-requirements.md
│   └── diary-architecture.md                # （本文件）
└── .gitignore                               # + /content/diary/
```

## 5. 安全门控命令

与主仓库一致，全部跑 `make`：

- 代码格式：`make fmt`（零 diff 通过）
- 静态检查：`make vet`
- 全量测试：`make test`
- 一键发布前门控：`make check`（打包 fmt + vet + test + govulncheck）
- E2E smoke：项目尚未建独立 e2e 目录；diary 的 handlers 测试覆盖 HTTP 层端到端就够

新增测试约束：
- `internal/diary/store_test.go` 必须覆盖：路径穿越拒绝 / 合法 YYYY-MM-DD / 空文件行为 / 删除幂等
- `internal/diary/handlers_test.go` 必须覆盖：未登录 302 / 非法 `date` 400 / 正常 roundtrip / 转正 slug 冲突 / **公共路由隔离**（验证 /，/docs，/rss.xml，/sitemap.xml 响应体不含某条已写的日记）

## 审核记录

| 日期 | 审核人 | 评分 | 结果 | 备注 |
|------|--------|------|------|------|
| 2026-04-19 | AI Assistant | 98/100 | 通过 | 轻量模式（阈值 70）；修复：加 IIFE 约定 + 显式性能依据 |
