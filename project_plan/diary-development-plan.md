# 日记功能 开发方案

> 创建日期：2026-04-19
> 状态：已审核
> 版本：v1.1
> 关联需求文档：project_plan/diary-requirements.md
> 关联架构文档：project_plan/diary-architecture.md

## 1. 技术概述

> 详细的技术栈、模块划分、接口风格见架构文档 §1–4。

### 1.1 安全门控命令

- 代码格式（零 diff 通过）：`make fmt`
- 静态检查：`make vet`
- 全量测试：`make test`
- 发布前综合门控：`make check`（fmt + vet + test + govulncheck）

每个工作项的验收标准都必须以对应的安全门控命令通过为前提。

### 1.2 交付形态

一个新 Go 包 `internal/diary/` + 一个 HTML 模板 + 一段 JS + CSS 若干，通过 blog-server 服务端嵌入；本地 `make build` 产出的二进制即带此功能，无运行时外部依赖。

## 2. 开发阶段

三阶段，每阶段独立可交付：

1. **Stage 1 — 后端基础**：store、calendar、/diary 路由 + 月视图 SSR
2. **Stage 2 — 编辑器与保存**：JSON API、客户端 JS、自动保存 + 手动保存
3. **Stage 3 — P1 增强与安全断言**：清空、转正、/manage 入口、保存失败 UX、公共路由隔离硬断言

---

### Stage 1：后端基础

**目标**：未登录访问 /diary 跳登录；登录后看到当月日历，有内容的日期显示绿点；URL 带 `?year&month` 可切月。

**涉及的需求项**：2.1.1、2.1.2、2.6.1、2.6.3（部分）

#### 工作项列表

##### WI-1.1 [M] diary.Store — 文件系统读/写/列/删
- **描述**：实现 `internal/diary/store.go`：
  - `type Store struct { root string }`
  - `Validate(date string) (time.Time, error)` — 正则 `^\d{4}-\d{2}-\d{2}$` + `time.Parse` 二次校验
  - `Get(date string) (body string, exists bool, err error)`
  - `Put(date, body string) error` — 写入 frontmatter + body，自动 `mkdir -p`
  - `Delete(date string) error` — 幂等（不存在不报错）
  - `DatesIn(year int, month time.Month) (map[int]bool, error)` — 返回该月有日记的日数字集合
- **验收标准**：
  1. 上述方法签名齐全，路径穿越被 `Validate` 拒绝
  2. 安全门控：`make fmt && make vet && make test ./internal/diary/...` 通过
- **Notes**：
  - Pattern：直接文件 IO（不走 content.Store），参考 `internal/content/` 里 frontmatter 解析约定
  - Reference：`internal/content/frontmatter.go`（YAML frontmatter 写法）
  - Hook point：无外部依赖；只读写 `<installDir>/content/diary/`

##### WI-1.2 [S] Smoke 测试 — Store roundtrip
- **描述**：在 `internal/diary/store_test.go` 覆盖正常路径。
- **验收标准**：
  1. `Put("2026-04-19", "hello") → Get → body="hello", exists=true`
  2. `DatesIn(2026, 4)` 包含 `19`
  3. `Delete("2026-04-19") → Get → exists=false`
  4. 安全门控：`make test ./internal/diary/...` 通过

##### WI-1.3 [S] 异常测试 — Store 路径穿越与边界日期
- **描述**：同一测试文件下。
- **覆盖场景清单**：
  - [x] 非法输入：`".."`、`"/etc/passwd"`、`"2026-13-01"`、`"abc"`、空字节 → `Validate` 返回 error；`Put/Get/Delete` 不触碰任何文件
  - [x] 边界值：闰年 `2024-02-29` 可写；`2025-02-29` 被拒（time.Parse 会捕获）；`2026-01-31` / `2026-12-31` 跨月跨年不出错
  - [x] 失败依赖：root 目录不存在 → `Put` 自动创建目录；root 目录权限只读 → 返回 error 但不 panic
  - [x] 异常恢复：`Delete` 不存在的日期 → 返回 nil（幂等）；重复 Put 同一天覆盖最新内容
- **实现手段**：直接构造输入 + `t.TempDir()` 做独立 fixture
- **断言目标**：返回 error 类型、文件存在性（`os.Stat`）、文件内容（`os.ReadFile`）
- **验收标准**：
  1. 上述场景全部通过
  2. 每个用例末尾 `t.Cleanup(os.RemoveAll)` 保证不污染
  3. 安全门控：`make test ./internal/diary/...` 通过

##### WI-1.4 [集成门控] Store 层可用
- **描述**：验证 WI-1.1 ~ WI-1.3 集成状态
- **验收标准**：
  1. `make fmt && make vet && make test ./internal/diary/...` 全绿
  2. Store 方法已封装为可独立使用的 API，下一阶段 handlers 可直接注入

##### WI-1.5 [M] calendar.go — 月/周视图数据结构
- **描述**：实现 `internal/diary/calendar.go`，纯函数、无 I/O：
  - `type Day struct { Date time.Time; InMonth bool; HasEntry bool; IsToday bool }`
  - `MonthGrid(year int, month time.Month, today time.Time, entries map[int]bool) [][]Day` — 6 行 × 7 列，周一起头，跨月占位 `InMonth=false`
  - `WeekGrid(focus, today time.Time, entries map[string]bool) []Day` — 7 天，包含 focus 的那一周
- **验收标准**：
  1. 返回的 grid 形状正确，每个 Day 的 InMonth/HasEntry/IsToday 正确
  2. 安全门控：`make vet && make test ./internal/diary/...` 通过
- **Notes**：
  - Pattern：纯函数好测，无需 mock
  - Hook point：handlers 层调用时，today 由 `time.Now()` 注入（测试用 fixed time）

##### WI-1.6 [S] Smoke 测试 — calendar grid 形状
- **描述**：`internal/diary/calendar_test.go`
- **验收标准**：
  1. `MonthGrid(2026, 4, 2026-04-19, ...)` → 6 行 7 列；第 1 行首格是 3-30（周一）；4-19 的 IsToday=true
  2. `WeekGrid(2026-04-19, ...)` → 7 天从 4-13 到 4-19
  3. `entries` 里 19 → 对应 Day.HasEntry=true
  4. 安全门控：`make test ./internal/diary/...` 通过

##### WI-1.7 [S] 异常测试 — calendar 边界月份
- **描述**：
- **覆盖场景清单**：
  - [x] 边界值：1 月（上月是去年 12 月）、12 月（下月跨年）、2 月闰年 29 天 + 非闰年 28 天
  - [x] 边界值：MonthGrid 的第 1 周全部是上月占位（例如 2026-03 第一天是周日）
  - [x] 边界值：MonthGrid 的最后 1 周全部是下月占位（极端情况）
  - [x] 非法输入：month=0 / month=13 → `time.Month` 类型本身允许越界，但 calendar 应在 Normalise 或文档中说明"上游保证合法"
- **实现手段**：表格驱动 test（多年多月 matrix）
- **断言目标**：grid 第 1 行首 Day 的日期等于"周一且 ≤ 该月 1 号的最大日"
- **验收标准**：
  1. 表格驱动覆盖 1/2/12 月 + 闰年
  2. 安全门控：`make test ./internal/diary/...` 通过

##### WI-1.8 [集成门控] Calendar 层可用
- **描述**：验证 WI-1.5 ~ WI-1.7
- **验收标准**：
  1. `make fmt && make vet && make test ./internal/diary/...` 全绿
  2. MonthGrid + WeekGrid 可被 handlers 层直接消费（接口形状稳定）

##### WI-1.9 [M] /diary 路由 + 认证 + 月视图 SSR
- **描述**：
  - `internal/diary/handlers.go`：`type Handlers struct { Store *Store; Tpl *render.Templates; Auth *auth.Store; Logger *slog.Logger }`
  - `(h *Handlers) Page(w, r)`：
    - `ParseSession` 失败 → 302 `/manage/login?next=<escaped>`
    - 解析 `?year&month`，非法回落当月
    - 从 Store.DatesIn 查得本月绿点集合
    - 构造 MonthGrid，渲染 `diary.html`
  - 新模板 `internal/assets/templates/diary.html`：顶部导航（上月 / 回到本月 / 下月）+ 6×7 日历网格 + 绿点小圆（CSS 类 `.diary-dot`）+ 今天高亮（CSS 类 `.diary-today`）
  - `internal/assets/static/css/theme.css`：新增 `.diary-*` 样式若干
  - `cmd/server/main.go`：注册 `/diary` 路由
- **验收标准**：
  1. 登录态下 GET /diary 返回 200，body 含日历结构
  2. 未登录 GET /diary 返回 302 到 /manage/login?next=/diary
  3. 安全门控：`make fmt && make vet && make test ./...` 通过
- **Notes**：
  - Pattern：handlers 初始化约定（NewHandlers 构造函数）参考 `internal/admin/admin.go`
  - Reference：`internal/admin/admin.go:LoginPage`（认证 + 渲染范式）
  - Hook point：`cmd/server/main.go` 里与 `/manage/*` 认证同级

##### WI-1.10 [S] Smoke 测试 — /diary 月视图 SSR
- **描述**：`internal/diary/handlers_test.go`
- **验收标准**：
  1. Fixture 写一条 `content/diary/2026-04-19.md`；GET /diary?year=2026&month=4 响应 200
  2. body 含 "2026" "4 月"（或等价标签）、日期 19 附近含 `.diary-dot` 的 class
  3. body 含今天的 `.diary-today` class
  4. 安全门控：`make test ./internal/diary/...` 通过

##### WI-1.11 [S] 异常测试 — /diary 认证 + 非法参数
- **描述**：同一测试文件
- **覆盖场景清单**：
  - [x] 权限/认证：无 session cookie → 302 到 /manage/login?next=/diary，不暴露任何日记 fixture 内容
  - [x] 非法输入：`?year=abc&month=xyz` → 200 回落当月；`?year=2099&month=13` → 200 回落当月
  - [x] 边界值：`?year=1900&month=1` 可渲染；`?year=2200&month=12` 可渲染
  - [x] 非法输入：`?date=../etc/passwd` → 200 回落当月（不触碰非 /content/diary/ 的路径）
- **实现手段**：`httptest.NewRecorder` + 构造 cookie / 无 cookie 请求
- **断言目标**：HTTP status + body 不含 fixture 内容（对未登录用例）
- **验收标准**：
  1. 上述场景全部通过
  2. 每个用例独立 `t.TempDir()`
  3. 安全门控：`make test ./internal/diary/...` 通过

##### WI-1.12 [集成门控] Stage 1 完成
- **描述**：验证 Stage 1 全量
- **验收标准**：
  1. `make check` 全绿
  2. 手动/测试：启动本地 server，浏览器访问 /diary 登录后能看到当月日历
  3. 未登录访问能正确跳转登录

**Stage 1 验收标准**：
1. Given 登录态 + 4-19 有日记文件，When GET /diary，Then 响应 200 且日历格子 "19" 有绿点
2. Given 未登录，When GET /diary，Then 302 到 /manage/login?next=/diary
3. Given 非法 `?year=abc`，When GET /diary?year=abc，Then 200 渲染当月（不 400）
4. 所有 WI 安全门控通过

**阶段状态**：已完成
**完成日期**：2026-04-19
**验收结果**：通过
**安全门控**：`make check` 全绿
**集成门控**：WI-1.4 / 1.8 / 1.12 全部通过
**备注**：20 个测试全绿；发现并记录 2 条 learnings（UA 指纹测试坑 + 重复 UA setter 的技术债）

---

### Stage 2：编辑器与保存

**目标**：用户点击日历某一天，下方出现 textarea 加载当天正文；打字、离焦、1.5s 停顿、按 Ctrl+S、点保存按钮都能把内容存到文件；状态反馈 "已保存于 HH:MM"。

**涉及的需求项**：2.2.1、2.2.2、2.3.1、2.3.2

#### 工作项列表

##### WI-2.1 [M] JSON API：GET /diary/api/day + POST /diary/api/save
- **描述**：
  - `(h *Handlers) APIDay(w, r)`：GET，解析 `?date=`；校验；返回 `{"body":"..."}`
  - `(h *Handlers) APISave(w, r)`：POST，form 解析 `date`、`content`、`csrf`；CSRF 校验；`Store.Put` or `Store.Delete`（content 空）；返回 `{"ok":true,"savedAt":"RFC3339"}`
  - 两个 handler 都走 session 认证，无 session → 401
- **验收标准**：
  1. API 路由注册完毕，签名与响应体符合架构文档 §3
  2. 安全门控：`make fmt && make vet && make test ./...` 通过
- **Notes**：
  - Pattern：JSON 编码走 `encoding/json`（stdlib）
  - Reference：`internal/admin/settings.go` 的 form 解析 + CSRF 范式
  - Hook point：CSRF token 从 `auth.ParseSession` 返回对象取

##### WI-2.2 [S] Smoke 测试 — API roundtrip
- **描述**：handlers_test.go 新增
- **验收标准**：
  1. POST /diary/api/save 带合法 csrf + date + content → `ok:true`，文件出现
  2. GET /diary/api/day?date=... → body 一致
  3. 再 POST 空 content → `ok:true`，文件消失
  4. 安全门控：`make test ./internal/diary/...` 通过

##### WI-2.3 [S] 异常测试 — API 鉴权 + 非法输入 + CSRF
- **覆盖场景清单**：
  - [x] 权限/认证：无 session → GET/POST 都 401 或 302（按现有约定）
  - [x] 权限/认证：有 session 但 POST 无 csrf → 403
  - [x] 权限/认证：csrf 不匹配 → 403
  - [x] 非法输入：`date=""` / `date=".."` / `date="abc"` / `date="2026-13-01"` → 400
  - [x] 边界值：`date=2024-02-29`（闰年合法）→ 200；`date=2025-02-29` → 400
  - [x] 边界值：content 长度 1MB → 正常存；content 仅空白字符 → 视为空 → 删文件
  - [x] 并发/竞态：两个 goroutine 同时 POST save 同一日期 → 后写入胜出，无 panic，文件最终一致
  - [x] 异常恢复：POST save 目标目录不存在 → API 自动创建（store 层负责）
- **实现手段**：`httptest.NewRecorder` + `sync.WaitGroup` 模拟并发
- **断言目标**：HTTP status + 文件系统最终状态
- **验收标准**：
  1. 全部场景通过
  2. 安全门控：`make test ./internal/diary/...` 通过

##### WI-2.4 [集成门控] API 层可用
- **验收标准**：
  1. `make fmt && make vet && make test ./...` 全绿
  2. 可手动用 curl 操作日记（带 csrf token）GET/POST 验证 roundtrip

##### WI-2.5 [M] 编辑器 UI — diary.html + diary.js + CSS
- **描述**：
  - 模板补全：textarea 区块（默认 hidden）、保存按钮、状态提示 `<span class="diary-status">`、"清空这一天" 按钮（WI-3.1 用但先放 DOM）、周视图容器
  - CSS：`.diary-calendar`、`.diary-week-mode`、`.diary-editor`、`.diary-status`、`.diary-today`、`.diary-dot`、深色模式覆盖（与现有 dark-mode block 一致的深色色卡）
  - JS：IIFE + `'use strict'`
    - `fetchDay(date)` / `saveDay(date, content)` 用 `fetch` + `credentials: 'same-origin'`
    - 日历点击 → 折叠成当周（DOM 隐藏其它周行 + 添加 `.diary-week-mode`）+ `fetchDay(date)` 填 textarea
    - debounce(1500ms) + blur + beforeunload + Ctrl/Cmd+S → 触发 `saveDay`
    - 保存前从 `<meta name="csrf">` 读 token
    - 状态反馈：`编辑中...` / `已保存于 HH:MM:SS` / 未完工的错误态留给 Stage 3
- **验收标准**：
  1. diary.html 含 textarea + 保存按钮 + `<meta name="csrf">` + `<script defer src="/static/js/diary.js">`
  2. diary.js 是 IIFE + `'use strict'`
  3. CSS 深色模式有对应覆盖
  4. 安全门控：`make fmt && make vet && make test ./...` 通过
- **Notes**：
  - Pattern：沿用 mouse-nav.js 的 IIFE + defer 模式
  - Reference：`internal/assets/static/js/mouse-nav.js`
  - Hook point：diary.html 里 `<meta name="csrf" content="{{ .CSRF }}">`，handlers 的 data 里注入

##### WI-2.6 [S] Smoke 测试 — 编辑器 UI 契约（静态扫描）
- **描述**：`internal/diary/ui_test.go`（放 diary 包内做嵌入 FS 扫描）
- **验收标准**：
  1. diary.html 含 `<textarea` + `<button...保存` + `meta name="csrf"` + `script...diary.js`
  2. diary.js 含 `addEventListener('click'...`（日历）、`fetch(` (API 调用)、`debounce` 或 `setTimeout` + `clearTimeout`（1500ms）、`Ctrl`/`metaKey`（快捷键）、`'use strict'`、IIFE `(function`
  3. theme.css 新增的暗色 block 里含 `.diary-*` 选择器（至少 2 处）
  4. 安全门控：`make test ./internal/diary/...` 通过
- **Notes**：
  - Pattern：参考 `internal/assets/mouse_nav_test.go` 的静态扫描断言

##### WI-2.7 [S] 异常测试 — 编辑器 UI 关键守卫
- **覆盖场景清单**：
  - [x] 非法输入：diary.js 不得对 `event.type === 'click'` 之外的鼠标事件做导航（反例检查）
  - [x] 失败依赖：diary.js 的 `fetch(...).catch(...)` 分支存在（即保存失败有错误处理占位，具体 UX 在 WI-3.x 完善）
  - [x] 边界值：debounce 时间常量明确存在（搜 `1500` 或对应 magic number）
- **实现手段**：静态文本扫描（go 测试里 `strings.Contains`）
- **断言目标**：源码含守卫分支与常量
- **豁免说明**：JS 的运行时行为无法在 Go 测试里真实模拟；真浏览器 E2E 放到手动验收
- **验收标准**：
  1. 静态扫描全部通过
  2. 安全门控：`make test ./internal/diary/...` 通过

##### WI-2.8 [集成门控] Stage 2 完成
- **描述**：验证 Stage 2 全量
- **验收标准**：
  1. `make check` 全绿
  2. 手动：启动 server → 登录 → /diary → 点日期 → 输入文本 → 离焦/Ctrl+S/等 1.5s → 刷新看到文件；月视图绿点出现

**Stage 2 验收标准**：
1. Given 登录 + /diary 月视图，When 点击 4-19，Then 日历折叠成 4-13~4-19 周视图，editor 显示当天正文或 placeholder
2. Given 正在编辑，When 停止输入 1.5s 或按 Ctrl+S，Then POST /diary/api/save 成功，状态变 "已保存于..."
3. Given 非法 csrf，When POST /diary/api/save，Then 403（异常路径验收）
4. 所有 WI 安全门控通过

**阶段状态**：未开始

---

### Stage 3：P1 增强与安全断言

**目标**：清空这一天 / 转正为文档 / /manage 仪表盘入口 / 保存失败反馈完备 / 公共路由隔离硬测试。

**涉及的需求项**：2.3.3、2.4.1、2.5.1、2.5.2、2.6.2、2.6.3

#### 工作项列表

##### WI-3.1 [S] 删除 API + "清空这一天" 按钮
- **描述**：
  - handlers.go：`APIDelete(w,r)` POST /diary/api/delete，form `date`、`csrf`；调 `Store.Delete`（幂等）
  - diary.html 按钮已在 WI-2.5 预留，这里绑事件：点击触发 `confirm("确定要清空 YYYY-MM-DD 的日记？此操作不可恢复")` → POST delete → 清空 textarea + 重新渲染月视图绿点（客户端局部刷新，或整页刷新都可以，MVP 用整页刷新最简）
- **验收标准**：
  1. API 注册并通过认证 + CSRF
  2. 按钮点击经 confirm 后走 delete 流程
  3. 安全门控：`make fmt && make vet && make test ./...` 通过
- **Notes**：
  - Pattern：POST + form + CSRF，与 `/diary/api/save` 同套路
  - Reference：WI-2.1 `APISave` 的实现
  - Hook point：Store.Delete 已在 WI-1.1 实现且幂等

##### WI-3.2 [S] Smoke 测试 — 删除 API
- **验收标准**：
  1. POST delete + 合法参数 → 200，文件消失
  2. 月视图刷新后不再含绿点
  3. 安全门控：`make test ./internal/diary/...` 通过

##### WI-3.3 [S] 异常测试 — 删除 API
- **覆盖场景清单**：
  - [x] 异常恢复：删除不存在的日期 → 200 ok（幂等）
  - [x] 权限/认证：无 session / 无 csrf → 401/403
  - [x] 非法输入：`date=".."`/ 空 / 非法日期 → 400
- **实现手段**：httptest
- **断言目标**：HTTP status + 文件系统（确认不漏删其它日期）
- **验收标准**：
  1. 场景全部通过
  2. 安全门控：`make test ./internal/diary/...` 通过

##### WI-3.4 [集成门控] 删除功能可用
- **验收标准**：
  1. Stage 1+2+3.1~3.3 全绿
  2. 手动验证删除路径

##### WI-3.5 [M] 转正 API + 弹窗 UI
- **描述**：
  - handlers.go：`APIPromote(w,r)` POST /diary/api/promote；form `date`、`title`、`slug`、`category`（空字符串允许）、`csrf`
    - 校验 slug 为 `^[a-z0-9][a-z0-9-]*$`、title 非空
    - 读日记内容 → 若目标 `content/docs/<slug>.md` 已存在 → 返回 `{ok:false,error:"slug_conflict"}` 409
    - 否则 `os.WriteFile` 到 `content/docs/<slug>.md`，frontmatter 含 `title`/`slug`/`category`/`updated`/`status: draft`，body 为日记正文
    - 返回 `{ok:true,slug:"..."}`
    - **不修改日记原件**、**不在 docs frontmatter 写任何反向引用**
  - diary.js：点"转正为文档"按钮 → 自定义 DOM modal 或最简 3 个 `prompt()`（MVP 可先用 prompt，UI 后续打磨）；弹窗收集 title/slug/category → POST → 成功 alert 跳 `/manage/docs/<slug>/edit`；冲突 alert 文案提示
- **验收标准**：
  1. API 行为按描述；UI 可触发
  2. 安全门控：`make fmt && make vet && make test ./...` 通过
- **Notes**：
  - Pattern：docs 文件格式参考现有 `content/docs/*.md`（随便找一条）
  - Reference：`internal/content/frontmatter.go`
  - Hook point：新文件会被现有 content.Store 在下次 reload 时扫到，没额外配置

##### WI-3.6 [S] Smoke 测试 — 转正
- **验收标准**：
  1. 日记 "random body"，POST promote 带合法 title/slug/category → 200 `ok:true`
  2. `content/docs/<slug>.md` 存在，body 包含 "random body"，frontmatter 含传入的 title/category + `status: draft`
  3. 日记原件未变
  4. 安全门控：`make test ./internal/diary/...` 通过

##### WI-3.7 [S] 异常测试 — 转正
- **覆盖场景清单**：
  - [x] 非法输入：slug 含 `/`、空字符串、含空格 → 400
  - [x] 非法输入：title 为空 → 400
  - [x] 失败依赖：目标 slug 已存在 → 409 `error:"slug_conflict"`；此时 `content/docs/<slug>.md` 内容未被覆盖
  - [x] 异常恢复：日记不存在 → 404
  - [x] 边界值：同一 date 用两个不同 slug 连续转正两次 → 两份 docs 各自独立存在，日记原件仍然没动
  - [x] 权限/认证：无 csrf → 403
- **实现手段**：httptest + t.TempDir + fixture
- **断言目标**：HTTP status、文件内容（grep 日记原件未变 + 新 docs frontmatter 无 `source_diary_date` 等反向引用字段）
- **验收标准**：
  1. 场景全部通过
  2. 安全门控：`make test ./internal/diary/...` 通过

##### WI-3.8 [集成门控] 转正功能可用
- **验收标准**：
  1. Stage 3.1~3.7 全绿
  2. 手动：写日记 → 转正 → 跳 /manage/docs/<slug>/edit 看到预填正文

##### WI-3.9 [S] 保存失败反馈 — diary.js 错误态 + CSS
- **描述**：
  - diary.js：`saveDay` 的 fetch 错误（非 2xx / network error）→ 状态变 `红色 "保存失败，点击重试"`，且状态机在下次成功前不被"编辑中..."覆盖
  - 状态栏点击重试 → 再次尝试 save
  - CSS：`.diary-status.error` 红色样式
- **验收标准**：
  1. 行为按需求 2.3.3 三条 G/W/T
  2. 安全门控：`make fmt && make vet && make test ./...` 通过
- **Notes**：
  - Pattern：状态机维护一个 `state: "editing" | "saving" | "saved" | "error"`，`error` 态"粘滞"直到下次成功才清除
  - Reference：diary.js 已有的 `saveDay` + 状态反馈骨架（WI-2.5 建立）
  - Hook point：CSS 新增一条 `.diary-status.error { color: #c00 }`；暗色模式 @media block 顺带加覆盖

##### WI-3.10 [S] Smoke 测试 — 保存失败反馈（静态扫描）
- **描述**：diary.js 静态扫描（无法真跑 JS）
- **验收标准**：
  1. diary.js 含 `catch` 块 + 设置错误态 class（扫 `.error` 出现）
  2. CSS 有 `.diary-status.error` 规则
  3. 安全门控：`make test ./internal/diary/...` 通过
- **豁免说明**：运行时 UX 靠手动验收，Go 测试只静态审核存在性

##### WI-3.11 [S] /manage 仪表盘日记入口
- **描述**：`internal/assets/templates/admin_dashboard.html` 加一张 "日记" 卡片，链接 `/diary`，风格与现有卡片一致。
- **验收标准**：
  1. 登录 /manage 后，响应 body 含 "日记" 文案 + `href="/diary"`
  2. 安全门控：`make fmt && make vet && make test ./...` 通过
- **Notes**：
  - Pattern：复制仪表盘已有 `.admin-section / .admin-nav` 卡片结构的一条
  - Reference：`internal/assets/templates/admin_dashboard.html` 现有"内容管理"、"系统设置"等卡片
  - Hook point：纯模板改动，无后端改动

##### WI-3.12 [S] Smoke 测试 — 仪表盘卡片
- **验收标准**：
  1. GET /manage（已登录）→ body 含 `href="/diary"` 链接
  2. 安全门控：`make test ./internal/admin/...` 通过
- **豁免说明**：纯展示 + 纯静态链接，无输入 → 豁免异常测试

##### WI-3.13 [集成门控] P1 功能可用
- **验收标准**：
  1. `make fmt && make vet && make test ./...` 全绿
  2. 手动：/manage 看到日记卡片 → 点进 /diary 正常

##### WI-3.14 [M] 硬安全断言 — 公共路由隔离测试
- **描述**：新增 `internal/public/diary_isolation_test.go`（放 public 包内，确保真走公共 handlers）：
  - 准备：预写 `content/diary/2026-04-19.md` body 含独特 marker `"DIARY_LEAK_MARKER_xyz"`
  - 对 `/`、`/docs`、`/docs/2026-04-19`、`/projects`、`/rss.xml`、`/sitemap.xml` 各发一次 GET（匿名 + 登录两种）
  - 断言每一个响应 body 都**不含** marker
- **覆盖场景清单**：
  - [x] 非法输入：try访问 `/docs/2026-04-19`（诱导把日记伪装成 doc slug）→ 404，body 无 marker
  - [x] 权限/认证：未登录访问以上所有路由 → 任何都不含 marker
  - [x] 权限/认证：已登录访问 / /docs /projects → 仍不含 marker（登录态不应泄露日记到公共视图）
- **实现手段**：httptest + 直接调 public handlers
- **断言目标**：response body `strings.Contains(body, "DIARY_LEAK_MARKER_xyz")` 必须 false
- **验收标准**：
  1. 场景全部通过
  2. 安全门控：`make test ./internal/public/...` 通过

##### WI-3.15 [S] .gitignore + 运行时目录初始化
- **描述**：
  - `.gitignore` 追加 `/content/diary/`
  - Store 构造函数里 `os.MkdirAll(root, 0o700)`，首次运行自动建立目录
- **验收标准**：
  1. `git status` 新建 `content/diary/` 后不出现在 staging
  2. 首次运行 server + 访问 /diary 不因缺目录 500
  3. 安全门控：`make fmt && make vet && make test ./...` 通过
- **Notes**：
  - Pattern：构造函数级别的幂等初始化，沿用 `internal/storage` 的同类做法
  - Reference：`internal/storage/storage.go` 的 `Open` 里 `MkdirAll(dir, ...)` 范式
  - Hook point：Store.NewStore(root) 改动一处
- **豁免异常测试说明**：本 WI 仅两个变更：（1）`.gitignore` 纯配置，无输入无副作用；（2）构造函数里调 `os.MkdirAll` 是幂等初始化，其行为已被 WI-1.2 smoke 测试间接覆盖（Put 时目录必须存在）。无需独立的 smoke / 异常 WI。如果实施中发现构造函数调用路径有新分支，应补测。

##### WI-3.16 [集成门控] Stage 3 完成 = MVP 可发布
- **描述**：Stage 1+2+3 全量验收
- **验收标准**：
  1. `make check` 全绿
  2. 端到端手动：登录 /manage → 点日记卡片 → 写日记 → 自动保存 → 清空 → 再写 → 转正 → 在 /manage/docs 里看到转正后的草稿
  3. 另开隐私浏览窗口匿名访问 /、/docs、/rss.xml、/sitemap.xml，grep 所有响应，无任何一条日记内容泄露
  4. 切到系统深色模式看 /diary 暗色版面不白底

**Stage 3 验收标准**：
1. Given 登录 + 4-19 有日记，When 点"清空这一天" → confirm 确认，Then 文件消失且日历绿点消失
2. Given 日记 "A"，When 转正为 slug=x，Then content/docs/x.md 存在 body 含 "A"；原日记未变
3. Given 同一日记，When 先后转正两次（slug=x、y），Then 两份文档独立；原日记仍是原样
4. Given 任意公共路由（/、/docs、/rss.xml、/sitemap.xml），When 访问，Then 响应不含任何日记内容（**硬断言**）
5. Given 登录 /manage，When 查看仪表盘，Then 含"日记"卡片链接 /diary

**阶段状态**：未开始

---

## 3. 风险与应对

| 风险 | 影响 | 概率 | 应对措施 |
|------|------|------|----------|
| JS 运行时行为无法在 Go 测试里覆盖 | 编辑器 UX bug 只能靠人工发现 | 中 | 每次 Stage 门控含手动浏览器验收；关键 DOM 结构靠静态扫描 + 契约测试守住 |
| content.Store reload 不扫描 diary 目录的假设破裂 | 日记可能意外出现在公共视图 | 低 | Stage 3 WI-3.14 硬断言测试；一旦实现偏离，该测试立刻挂 |
| "转正" 文件与现有 docs reload 机制交互 | 新建 docs 后 /docs 列表未更新 | 中 | 依赖现有 content reload 机制；若失效，WI-3.8 手动验收会发现，必要时补 reload 调用 |
| 日记自动保存在慢网络下语义不明 | 用户以为保存了实际没存 | 中 | WI-3.9 必须实现错误态 + 状态常驻；测试覆盖 |
| 弹窗 UI 用 prompt() 体验差 | 转正流程突兀 | 低 | MVP 接受；后续单独开 /quick-feature 打磨 |

## 4. 开发规范

### 4.1 代码规范
- Go 代码遵循 `gofmt + go vet`；命名与现有 `internal/*` 一致
- 模板使用 `html/template` 默认转义；涉及日记正文的渲染必须经过转义（日记不走 `markdownUnsafe`）
- JS 遵循 IIFE + `'use strict'`，`<script defer>` 引入，不污染全局

### 4.2 Git 规范
- 每个 Stage 开一个独立 feature 分支：`feat/diary-stage-1` / `feat/diary-stage-2` / `feat/diary-stage-3`
- 每个 Stage 完成后合入 main（或保留独立 PR，由用户决定）
- Commit 中文，沿用项目现有提交风格

### 4.3 文档规范
- 重大设计偏离本方案 → 先更新 diary-architecture.md / diary-requirements.md 再改码
- 每个 Stage 完成后在 learnings.md 追加反思清单

## 5. 工作项统计

口径说明：下表 **S / M 列仅统计功能 WI**（不含测试 WI），Smoke / 异常 / 门控 各自独立成列。

| 阶段 | 功能 S | 功能 M | Smoke 测试 | 异常测试 | 集成门控 | 总计 |
|------|-------|-------|-----------|---------|---------|------|
| Stage 1 | 0 | 3 | 3 | 3 | 3 | 12 |
| Stage 2 | 0 | 2 | 2 | 2 | 2 | 8 |
| Stage 3 | 4 | 2 | 4 | 2 | 4 | 16 |
| **合计** | **4** | **7** | **9** | **7** | **9** | **36** |

注：Stage 3 的 "WI-3.14 公共路由隔离硬测试" 算作**功能 M**（实现测试基础设施），本身不再配对 smoke / 异常。所以 Stage 3 的异常测试列是 2（3.3 + 3.7），而不是"每功能都配对"的 4。

### 豁免异常测试（Notes 已注明理由）
- **WI-3.12** 仪表盘卡片 smoke：纯展示链接，无输入，无副作用
- **WI-3.15** .gitignore + mkdir：纯配置 + 幂等初始化；行为已被 WI-1.2 间接覆盖
- **WI-3.10** 保存失败反馈：部分豁免（JS 运行时交互靠手动验收 + 静态扫描）

### 粒度检查
- 禁止 L/XL：0 违规 ✅
- 边缘 M（关注）：WI-1.9 SSR（跨 4 文件）、WI-2.5 编辑器 UI（跨 3 文件 + 6 个 JS 行为）—— 实施时若拆成 2 个 M 子项会更稳

## 审核记录

| 日期 | 审核人 | 评分 | 结果 | 备注 |
|------|--------|------|------|------|
| 2026-04-19 | AI Assistant | 92/100 | 未通过 | WI-3.15 无豁免说明；4 个功能 WI 缺 Notes |
| 2026-04-19 | AI Assistant | 98/100 | 通过 | 修复 WI-3.1/3.9/3.11/3.15 Notes；WI-3.15 加豁免说明；门控措辞 + 统计表口径拉平 |
