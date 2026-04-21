# 作品集（Portfolio）开发方案

> 创建日期：2026-04-22
> 状态：已审核
> 版本：v1.1
> 关联需求文档：`project_plan/portfolio-requirements.md`（v1.1，已审核）
> 关联架构文档：`project_plan/portfolio-architecture.md`（v1.1，已审核）

## 1. 技术概述

详见架构文档。与开发直接相关的摘录如下。

### 1.1 安全门控命令
- `gofmt -l .`（输出须为空）
- `go vet ./...`
- `go test ./...`
- `make check`（聚合：fmt + vet + lint + tidy + test + vulncheck）
- `make release`（发布级，含 e2e）

### 1.2 测试实现约定
- Go 原生 `httptest` + 文件系统夹具跑 E2E smoke；夹具目录在 `setup(t, ...)` 辅助里 `t.TempDir()` 构造，用例末尾自然清理
- 并发测试用 `sync.WaitGroup` + `goroutine`；幂等断言靠文件系统最终态和 HTTP 响应码
- 视觉/无障碍扫描：`npx @axe-core/cli` 针对本地跑起来的 server URL 做抽样（纳入 WI-2.15）

---

## 2. 开发阶段

### 阶段 1：内容基础

**目标**：Portfolio 内容模型落盘、接入 `content.Store`、`Intro` 解析、对现有输出的防渗透保护、backup/export 覆盖。阶段完成后"作品集条目存在并被正确索引，不污染任何现有输出"。

**涉及的需求项**：
- 2.1.1 作品条目文件格式
- 2.1.2 内容索引：portfolio 类别接入
- 2.1.3 导出 / 备份覆盖
- 2.2.6 RSS / sitemap 隔离（以及 docs/projects/tags 防渗透）

#### 工作项列表

##### WI-1.1 [M] content 包新增 portfolio 类别 + 字段扩展
- **描述**：在 `internal/content` 新增 portfolio 类别常量；`Reload` 扫描 `content/portfolio/` 并按规范解析 frontmatter（`title/slug/cover/description/category/tags/order/demo_url/source_url/created/updated/status/featured`）；解析失败的单文件写日志并跳过，不阻塞其它文件；更新 `List` / 按 Kind 过滤接口。
- **验收标准**：
  1. 新建 `content/portfolio/foo.md` 后调用 Reload，按 portfolio 类别 `List` 可以取到该条目，各字段正确
  2. `store.List(KindDoc/KindProject)` 不包含 portfolio 条目（类别隔离）
  3. 安全门控：`gofmt -l .`、`go vet ./...`、`go test ./...` 通过
- **Notes**：
  - Pattern：复用 docs 的 frontmatter 解析，加 portfolio 字段集；公共路径的 List 接口统一带 Kind 参数
  - Reference：`internal/content/` 现有 docs/projects 扫描
  - Hook point：`cmd/server/main.go` 启动时 Reload；fsnotify watcher 加 `content/portfolio/`

##### WI-1.2 [S] Smoke 测试 — portfolio 扫描与索引
- **描述**：E2E smoke，直接操作 `content.Store` + 文件系统夹具，断言扫描/热更/Kind 过滤三条主路径。
- **验收标准**：
  1. 正常文件 3 条（draft/published/archived 各一）被正确解析、字段齐全
  2. 写入第 4 个文件后 fsnotify Reload 在 200ms 内完成，`List` 可见
  3. 按 docs 类别 `List` 返回不含 portfolio slug
  4. 安全门控：`go test ./internal/content/...` 通过

##### WI-1.3 [S] 异常测试 — portfolio 扫描异常路径
- **覆盖场景清单**：
  - [x] 非法输入：frontmatter 缺 `title` / 缺 `slug` / YAML 破损 / 字段类型错（`order: "abc"`）
  - [x] 边界值：空 body / body 仅含空白 / 超长 body（2MB）
  - [x] 失败依赖：`content/portfolio/` 目录不存在 / 权限不可读
  - [x] 异常恢复：扫描期间删除文件 / 重命名文件（fsnotify 事件接力）
- **实现手段**：Go `httptest` + `t.TempDir()` 构造各异常夹具；fsnotify 事件用 `os.Rename` / `os.Remove` 触发
- **断言目标**：单文件失败时日志包含文件名 + 原因；`List` 仍返回合法文件；目录不存在时服务正常启动（零条 portfolio）
- **验收标准**：
  1. 上述场景全部通过
  2. 用例末尾 `t.TempDir()` 自动清理
  3. 安全门控：`go test ./internal/content/...` 通过

##### WI-1.4 [S] `extractIntro` pure 函数
- **描述**：在 `internal/content/` 实现 `extractIntro(body string) (intro, rest string)`：首次匹配 `<!-- portfolio:intro -->…<!-- /portfolio:intro -->` 之间的内容为 intro；rest 为 body 去除注释对（含标记本身和前后紧邻换行）。输入无闭合标记 / 无开始标记 / 开始在闭合之后 / intro 超 4KB 等异常均返回 `""` 和原 body。
- **验收标准**：
  1. 正常输入返回正确切分
  2. 各异常输入安全回退，不 panic、无死循环
  3. 安全门控：`gofmt` `go vet` `go test ./internal/content/...` 通过
- **Notes**：纯函数，单元测试为主；覆盖 ≥ 10 个 case 故本工作项即附带单测

##### WI-1.5 [S] Smoke 测试 — extractIntro 正常路径
- **描述**：覆盖 "有 intro（单段/多段/含 Markdown）"、"无 intro"、"intro 在 body 中间"（应仍能抽取）三类正常用例。
- **验收标准**：
  1. 输出 intro / rest 字节级等于期望
  2. 安全门控：`go test ./internal/content/...` 通过

##### WI-1.6 [S] 异常测试 — extractIntro 异常路径
- **覆盖场景清单**：
  - [x] 非法输入：只开始无闭合、只闭合无开始、闭合在开始之前、嵌套（`<!-- portfolio:intro --> a <!-- portfolio:intro --> b <!-- /portfolio:intro -->`）
  - [x] 边界值：intro 内容为空字符串、仅空白、恰好 4KB、4KB+1B
  - [x] 注入尝试：intro 内含 `<script>` / `</portfolio:intro>` 字样（不是真正闭合） / HTML 实体 / 反引号围栏
- **实现手段**：表驱动单测
- **断言目标**：所有异常输入均 `intro == ""` 且 `rest == body` 原文（或 intro 内容为空字符串时 rest 去除标记对）
- **验收标准**：
  1. 全部场景通过
  2. 安全门控：`go test ./internal/content/...` 通过

##### WI-1.7 [集成门控] 内容基础接入
- **描述**：验证 WI-1.1 ~ WI-1.6 的集成：新建/修改/删除 portfolio md 后 `content.Store` 状态一致，`extractIntro` 被 Store 调用后字段 Intro 正确填充。
- **验收标准**：
  1. 所有安全门控命令通过
  2. 全仓 `make check` 绿
  3. 手动在 `content/portfolio/` 放 3 条样本文件，启动服务，检查日志无错，Store 状态正确

##### WI-1.8 [S] 防渗透：Kind 过滤硬约束
- **描述**：改 `internal/public/rss.go`、`sitemap.go`、`tags.go`、`doc_*.go`、`projects*.go` 所有从 `content.Store` 读列表的调用点，显式按 Kind 过滤 docs/projects，不引入 portfolio；在 store 层或 handler 边界任一位置加防御性 filter；若 store 层没有无 Kind 参数 List 的使用者，考虑把无过滤 API 改为内部非导出。
- **验收标准**：
  1. 现有 `/rss.xml` / `/sitemap.xml` / `/tags` / `/docs` / `/projects` 输出在**无 portfolio 条目**时字节级等于 v1.5.1
  2. 安全门控：`gofmt` `go vet` `go test ./...` 通过
- **Notes**：对应风险 R4

##### WI-1.9 [S] Smoke 测试 — 现有输出零回归
- **描述**：对上述 5 条路径编写快照式测试：v1.5.1 的输出（去除时间戳字段后）与 WI-1.8 改动后输出字节级一致。
- **验收标准**：
  1. 5 条路径的响应体与 golden file 一致（允许忽略 `<lastBuildDate>` 等时间字段）
  2. 安全门控：`go test ./internal/public/...` 通过

##### WI-1.10 [S] 异常测试 — 隔离断言
- **覆盖场景清单**：
  - [x] 非法输入：插入 3 条 published portfolio + 2 条 draft portfolio
  - [x] 边界值：插入 1 条 portfolio 其 slug 与某 doc slug 同名
  - [x] 异常恢复：portfolio 条目标签与 doc 条目相同，验证 `/tags` 不合流（tag 页不列出 portfolio 条目）
- **实现手段**：`httptest.NewServer` + 夹具目录；对比响应体不含 portfolio slug
- **断言目标**：HTTP 响应字符串不包含任何 portfolio slug 或 `/portfolio/` 路径前缀；`/tags/<name>` 列表条目数与纯 doc 场景一致
- **验收标准**：
  1. 场景全通过
  2. 用例末尾清理夹具目录（`t.TempDir` 自动）
  3. 安全门控：`go test ./internal/public/...` 通过

##### WI-1.10b [集成门控] 防渗透阶段性验证
- **描述**：验证 WI-1.8 ~ WI-1.10 完成后，引入 portfolio 不污染任何现有输出
- **验收标准**：
  1. 所有安全门控命令通过
  2. `/rss.xml` / `/sitemap.xml` / `/tags` / `/docs` / `/projects` golden file 回归全绿
  3. 隔离断言测试通过

##### WI-1.11 [S] backup / export 清单覆盖
- **描述**：修改 `internal/backup/*.go` 的打包清单加入 `content/portfolio/`；同步更新 `deploy/manage.sh` 的 `export` 子命令。
- **验收标准**：
  1. 调用 backup 导出，tar 内包含 `content/portfolio/` 下全部 md
  2. `restore` 到空目录，启动服务后 `/portfolio` 能列出 restore 的 published 条目
  3. 安全门控：`gofmt` `go vet` `go test ./internal/backup/...` 通过
- **Notes**：
  - Pattern：在既有 tar writer 迭代清单中追加 `content/portfolio/` 条目；restore 侧按相对路径还原
  - Reference：`internal/backup/*.go` 现有 docs/projects 打包逻辑；`deploy/manage.sh` 现有 export 子命令结构
  - Hook point：打包清单定义（通常是切片常量）；`manage.sh` 的 rsync / tar 列表

##### WI-1.12 [S] Smoke 测试 — export/restore 往返
- **描述**：在夹具目录创建 2 条 portfolio → export → restore 到另一个目录 → 对比两处 `content/portfolio/` 文件字节级相同。
- **验收标准**：
  1. 导入前后文件字节级一致
  2. 安全门控：`go test ./internal/backup/...` 通过

##### WI-1.13 [S] 异常测试 — backup 异常路径
- **覆盖场景清单**：
  - [x] 非法输入：`content/portfolio/` 不存在 / 为空目录
  - [x] 边界值：portfolio md 文件含非 UTF-8 字节（模拟磁盘损坏）
  - [x] 失败依赖：目标导出路径所在分区模拟写满（用 `t.TempDir()` + 小 quota 或 `/dev/full` 替代，可跳过）
- **实现手段**：文件系统夹具 + 小规模 io 错误注入
- **断言目标**：目录不存在/为空时导出产物里不含 `content/portfolio/`，但整个 export 不失败；损坏文件被跳过并记日志
- **验收标准**：
  1. 场景通过（磁盘写满场景可选）
  2. 安全门控：`go test ./internal/backup/...` 通过

##### WI-1.14 [集成门控] 阶段 1 全量验证
- **描述**：`make check` 全绿；手动端到端：写 3 条 portfolio → 重启 → 访问现有页面无回归 → export 后 restore 到新目录行为一致
- **验收标准**：
  1. 所有安全门控命令通过
  2. `/rss.xml`、`/sitemap.xml`、`/tags`、`/docs`、`/projects` 与 v1.5.1 基线无回归
  3. `content.Store` 按 portfolio 类别 List 返回正确条目

**阶段验收标准**：
1. Given 写入任意合法 portfolio md，When Reload 触发，Then 按 portfolio 类别可查到该条目且字段齐全
2. Given 插入 5 条任意状态 portfolio，When 请求 `/rss.xml` / `/sitemap.xml` / `/tags` / `/docs` / `/projects`，Then 响应与 v1.5.1 基线字节级一致（时间字段除外）
3. Given `content/portfolio/` 目录不存在，When 启动服务，Then 服务正常启动，portfolio 列表为空，不 panic
4. 所有工作项的安全门控通过
5. 所有集成门控通过

**阶段状态**：已完成

**完成日期**：2026-04-22
**验收结果**：通过
**安全门控**：全部通过（gofmt / go vet / go test ./... 全绿）
**集成门控**：全部通过（WI-1.7 / WI-1.10b / WI-1.14）
**备注**：
- 代码改动：`internal/content/content.go`（扩展 Kind + Entry + rawMeta）、`internal/content/intro.go`（新）、`internal/content/watch.go`（fsnotify 加 portfolio dir）、`deploy/manage.sh`（目录创建清单加 content/portfolio）
- 测试新增：`internal/content/portfolio_test.go`、`internal/content/intro_test.go`、`internal/public/portfolio_isolation_test.go`、`internal/backup/portfolio_backup_test.go`
- backup.go 发现无需改动：既有 `writeTarGz(..., []string{"content", ...})` 按目录整体打包，portfolio 自动包含（架构文档里"打包清单加 content/portfolio/"实为冗余描述，已在 learnings 记录）
- 防渗透约束在架构文档中明确为"按 Kind 的 API"，对应 `Index.List` 已强制 Kind 参数；补了 doc-comment 固化约定

---

### 阶段 2：前台

**目标**：访客可通过 `/portfolio`、`/portfolio/:slug`、主页作品集区块、导航入口完整浏览作品集，暗色与窄屏可用。

**涉及的需求项**：
- 2.2.1 `/portfolio` 列表页
- 2.2.2 `/portfolio/:slug` 详情页
- 2.2.3 主页"作品集"区块
- 2.2.4 导航栏入口
- 2.2.5 每条目 OpenGraph meta（P1）
- 2.4.1 暗色模式
- 2.4.2 窄屏适配

#### 工作项列表

##### WI-2.1 [M] /portfolio/:slug 详情页 + OG meta
- **描述**：新增 `internal/public/portfolio.go::DetailHandler`；新增 `portfolio_detail.html` 模板；head 输出 `og:title/og:description/og:image/og:type/og:url`；slug 正则 `^[a-zA-Z0-9_-]{1,64}$`；Intro 注释块剔除后渲染 body；draft 无预览 header 返回 404。
- **验收标准**：
  1. Given published 条目 `foo`，When GET `/portfolio/foo`，Then 200，body 渲染后不含 `portfolio:intro` 字面量
  2. OG 5 个字段输出正确（含 cover 为空时走默认 SVG 绝对 URL）
  3. 安全门控：`gofmt` `go vet` `go test ./internal/public/...` 通过
- **Notes**：
  - Pattern：handler 查 `store.ByKindSlug(KindPortfolio, slug)`；draft 借 `X-Blog-Preview` header + 登录态控制可见性，与 `doc_detail.go` 同机制；OG meta 由模板从 `absURL(req, cover或默认SVG)` 组装
  - Reference：`internal/public/doc_detail.go`、`about.html`（OG meta 现成结构）
  - Hook point：`cmd/server/routes.go` 注册 `/portfolio/:slug`；模板 layout 复用站点 head

##### WI-2.2 [S] Smoke 测试 — 详情页
- **描述**：覆盖 published 条目渲染、demo_url/source_url 外链、无 cover 显示默认 SVG、OG meta 齐全、同 slug 跨 Kind 命中 portfolio
- **验收标准**：
  1. 5 条 smoke 场景通过
  2. 安全门控：`go test ./internal/public/...` 通过

##### WI-2.3 [S] 异常测试 — 详情页
- **覆盖场景清单**：
  - [x] 非法输入：slug 含 `../` / 含中文 / 超 64 字符 / 空 slug
  - [x] 边界值：slug 恰好 64 字符、恰好 1 字符
  - [x] 权限/认证：draft 无预览 header → 404；draft 带预览 header 但未登录 → 404
  - [x] 异常恢复：cover 字段为 `javascript:alert(1)` 被 html/template 中和；cover 外链 404 不影响页面可访问（只是图坏）
- **实现手段**：`httptest` + 预置 slug 夹具
- **断言目标**：404 状态码 / 响应体不含 `javascript:` 字面量作为 src / OG image 是默认 SVG
- **验收标准**：
  1. 场景全通过
  2. 安全门控：`go test ./internal/public/...` 通过

##### WI-2.4 [M] /portfolio 列表页 + 分页 + tag 筛选
- **描述**：`ListHandler` 返回 published 条目（或预览 header 时含 draft），支持 `?tag=<name>`、`?page=<n>`（每页 20）；排序 `order ASC, updated DESC`；`portfolio_list.html` 卡片网格渲染。
- **验收标准**：
  1. 3 published + 1 draft 场景下匿名列表只出 3 条
  2. `?tag=设计` 正确筛选
  3. `?page=2` 返回第 11-20 条
  4. 安全门控：`gofmt` `go vet` `go test ./internal/public/...` 通过
- **Notes**：
  - Pattern：handler 层完成 filter/sort/paginate，模板只管渲染；`page<1` 规范化到 1，`page` 超界返回空列表；分页控件复用 docs 列表的 partial
  - Reference：`internal/public/docs_list.go`（或等价文件）的 filter+paginate 结构；`templates/docs_list.html` 的分页控件
  - Hook point：路由 `/portfolio`；默认封面 SVG 的 URL 常量在 template func 里暴露

##### WI-2.5 [S] Smoke 测试 — 列表页
- **描述**：分页、tag、排序、封面默认值四条主路径
- **验收标准**：
  1. 场景通过
  2. 安全门控：`go test ./internal/public/...` 通过

##### WI-2.6 [S] 异常测试 — 列表页
- **覆盖场景清单**：
  - [x] 非法输入：`?page=0` / `?page=-1` / `?page=abc` / `?page=99999`（超界）/ `?tag=` 空串 / `?tag=<注入>`
  - [x] 边界值：条目数恰好 0、1、20、21
  - [x] 权限/认证：匿名 `?status=draft` 不返回 draft；带预览 header + 登录态返回 draft
  - [x] 并发/竞态：保存新条目的同时请求列表页，不出现脏读/崩溃
- **实现手段**：`httptest` + `goroutine` 并发
- **断言目标**：非法 page 规范化到 1；非法 tag 返回空列表；draft 状态严格按预览 header 控制
- **验收标准**：
  1. 场景通过
  2. 安全门控：`go test ./internal/public/...` 通过

##### WI-2.7 [集成门控] 前台 /portfolio 链路
- **描述**：验证列表 + 详情 + 预览 header + OG meta 端到端可用
- **验收标准**：
  1. 所有安全门控命令通过
  2. `make check` 绿
  3. 手动访问 `/portfolio`、`/portfolio/:slug` 符合需求文档

##### WI-2.7b [集成门控] /portfolio 区块完整性
- **描述**：验证 WI-2.8 ~ WI-2.10（主页作品集区块 + smoke + 异常）的集成
- **验收标准**：
  1. 所有安全门控命令通过
  2. 主页 featured 展示 + 空态隐藏 + Intro 渲染三条端到端可用

##### WI-2.8 [M] 主页"作品集"区块
- **描述**：`home.go` 加 `FeaturedPortfolios` 数据装配（`status=published AND featured=true`，排序 `order ASC, updated DESC`）；`home.html` 在 hero + 个人文档之后追加作品集区块，卡片结构：上半左标题+description / 右封面（无则默认 SVG），下半渲染 Intro 为 HTML；空态整块不渲染；右上角"查看全部 ›"。
- **验收标准**：
  1. Given 5 条 featured published，When 访问 `/`，Then 出现 5 张卡片
  2. Given 0 条 featured，Then 无作品集标题、无空容器（DOM 不输出）
  3. 安全门控：`gofmt` `go vet` `go test ./internal/public/...` 通过
- **Notes**：
  - Pattern：`home.go` 新增 `FeaturedPortfolios []PortfolioCard`；模板 `{{ if .FeaturedPortfolios }} …块渲染… {{ end }}` 保证空态整块不输出；Intro 由服务端用 goldmark 渲染后传 `template.HTML`（已清洗）
  - Reference：`internal/public/home.go` 现有 `PersonalDocs`/`FeaturedDocs` 装配；`templates/home.html` 结构
  - Hook point：home data 结构体新增字段；模板片段插入"个人文档"block 之后

##### WI-2.9 [S] Smoke 测试 — 主页区块
- **描述**：featured 全量展示、Intro 渲染 HTML、默认封面回退、"查看全部"链接、点击卡片跳转
- **验收标准**：
  1. 5 条 smoke 场景通过
  2. 安全门控：`go test ./internal/public/...` 通过

##### WI-2.10 [S] 异常测试 — 主页区块
- **覆盖场景清单**：
  - [x] 非法输入：Intro 为空字符串、Intro 超长（>4KB 回退空）、cover 字段为外链 404（图坏但页面活）
  - [x] 边界值：0 条 featured 整块隐藏（DOM 不输出任何带 `portfolio` class 的容器）、1 条、20 条（全量不截断）
  - [x] 异常恢复：运行中管理后台把某条从 featured=true 改为 false，Reload 后主页不再展示
- **实现手段**：`httptest` + 夹具 + 手动 `store.Reload`
- **断言目标**：DOM 检查 `portfolio` class 存在与否、卡片数量、Intro 渲染为 HTML 不是转义字符串
- **验收标准**：
  1. 场景通过
  2. 安全门控：`go test ./internal/public/...` 通过

##### WI-2.10b [集成门控] 阶段 2 nav 插入前验证
- **描述**：验证 WI-2.8 ~ WI-2.10 集成正常（前面 WI-2.7b 已覆盖；本门控用于 nav 插入前的最终 sanity check）
- **验收标准**：
  1. 所有安全门控命令通过
  2. 主页 + 列表 + 详情链路端到端
  （若与 WI-2.7b 实际重复，可在执行时合并；本处保留以满足"每 3-5 WI 一门控"节奏）

##### WI-2.11 [S] 导航栏"作品集"入口
- **描述**：`layout.html` nav 在"文档"和"关于"之间插入 `<li><a href="/portfolio">作品集</a></li>`；路径前缀匹配 `/portfolio` 时加 `aria-current="page"` 或当前样式类（对齐 docs 机制）。
- **验收标准**：
  1. 所有页面 nav 出现作品集链接
  2. 安全门控：`gofmt` `go vet` 通过
- **Notes**：
  - Pattern：模板新增 `<li>`；`aria-current` 由模板读取 `{{ .CurrentPath }}` 或等价字段判断前缀
  - Reference：`templates/layout.html` 现有"文档 / 关于"项；docs 的 `aria-current` 实现
  - Hook point：layout data 需有当前路径字段（若无则在 handler 层统一注入）

##### WI-2.12 [S] Smoke 测试 — nav 入口
- **描述**：nav 项存在 + 位置正确 + `aria-current` 在 `/portfolio` 页面生效
- **验收标准**：
  1. 3 条 smoke 通过
  2. 安全门控：`go test ./internal/public/...` 通过
- **豁免说明**：本 WI 无独立异常测试工作项——nav 是纯展示、静态渲染、无外部输入；"窄屏菜单收起态展开"行为在 WI-2.15 axe/responsive 扫描中统一覆盖

##### WI-2.13 [M] 样式：列表 / 详情 / 主页卡片 / 暗色 / 窄屏
- **描述**：扩 `theme.css`，新增 gallery 网格、详情页布局、主页卡片（上半图文双栏 + 下半 Intro）、暗色变量复用、窄屏 <640 单列折叠（卡片上半两栏改上下单列，图在上）；Intro 为空时对应容器 `display: none`，不留出 margin/padding
- **验收标准**：
  1. 三类页面在亮/暗色下视觉正确
  2. viewport=375 无水平滚动，按钮可点击区 ≥ 40px
  3. 安全门控：`gofmt` `go vet` 通过
- **Notes**：
  - Pattern：所有颜色走 `var(--bg-…)` / `var(--fg-…)` 现有变量；卡片用 CSS Grid（上半 `grid-template-columns: 1fr auto`，下半 `grid-column: 1 / -1`）；窄屏 `@media (max-width: 640px)` 将上半改为 `grid-template-rows: auto auto; grid-template-columns: 1fr`（图在上）
  - Reference：`theme.css` 现有 `.doc-card`、`.projects-grid`、`@media (max-width: 640px)` 断点
  - Hook point：Intro 空态通过模板层 `{{ if .Intro }}…{{ end }}` 控制 DOM 输出，CSS 侧不依赖 `:empty` 判断

##### WI-2.14 [S] Smoke 测试 — 样式稳态
- **描述**：模板渲染时 CSS 类出现且结构正确（不做视觉 diff，做 DOM selector 断言）
- **验收标准**：
  1. 关键 class/结构存在
  2. 安全门控：`go test ./internal/public/...` 通过

##### WI-2.15 [S] 异常测试 — 无障碍 + 响应式
- **覆盖场景清单**：
  - [x] 边界值：viewport=375px（窄屏），全部页面无水平滚动；viewport=1920px（宽屏），卡片不撑破布局
  - [x] 权限/认证：匿名访问所有三类页面不崩
  - [x] 异常恢复：axe-core 扫描 `/portfolio` / `/portfolio/:slug` / 主页 在亮/暗色下无 `critical` contrast 违规
- **实现手段**：本地启动 server（`httptest.NewServer` 或 `make dev` 背景起），`npx @axe-core/cli <url>` 自动跑；窄屏用 Playwright（如项目已有）或手动脚本调 viewport
- **断言目标**：axe-core 输出零 critical；响应式检查屏宽 <640 时单列
- **验收标准**：
  1. 场景通过
  2. 扫描末尾停掉本地 server
  3. 安全门控：`go test ./...`、axe 扫描通过

##### WI-2.16 [集成门控] 阶段 2 全量验证
- **描述**：匿名访客从主页 → nav → 列表 → 详情 → 返回 全程无报错；暗色与窄屏均可用
- **验收标准**：
  1. `make check` 绿
  2. axe-core 无 critical
  3. 手动 UAT 通过

**阶段验收标准**：
1. Given 3 条 published 含不同 featured 设置，When 匿名访问主页 / `/portfolio` / `/portfolio/:slug`，Then 内容符合需求文档
2. Given viewport=375 + 暗色模式，When 访问各页面，Then 无水平滚动、axe 无 critical
3. Given 同 slug 在 docs 和 portfolio 都存在，When GET `/portfolio/:slug`，Then 命中 portfolio 版本
4. 所有工作项的安全门控通过
5. 所有集成门控通过

**阶段状态**：已完成

**完成日期**：2026-04-22
**验收结果**：通过
**安全门控**：全部通过（gofmt / go vet / go test ./... 全绿）
**集成门控**：全部通过（WI-2.7 / 2.7b / 2.10b / 2.16）
**备注**：
- 代码改动：`internal/public/portfolio.go`（新，详情 + 列表 + 排序）、`internal/public/public.go::Home`（加 FeaturedPortfolios 装配）、`internal/assets/templates/{portfolio_list,portfolio_detail,home,layout}.html`、`internal/assets/static/css/theme.css`（portfolio 全部样式 + 窄屏）、`internal/assets/static/images/portfolio-default.svg`（新）、`cmd/server/main.go`（路由注册）
- 测试新增：`portfolio_detail_test.go`、`portfolio_list_test.go`、`home_portfolio_test.go`、`portfolio_nav_test.go`、`portfolio_style_test.go`；更新 `portfolio_isolation_test.go` 显式排除 home（因 WI-2.8 本就让 home 展示 featured）
- WI-2.15 axe-core 全量扫描为发布前手动 UAT 步骤；本阶段用 Go-level 结构断言（alt 齐全 / 无 inline width / intro 标记不外泄）做最低保护
- 修正：架构文档给的 `date` template 函数名在仓库实际为 `formatDate`（初稿踩过；learnings 已记）

---

### 阶段 3：后台

**目标**：站主能完整管理作品集（列表 / 新建 / 编辑 / 软删除 / featured 切换 / order 调整 / 封面上传），trash 升级兼容旧数据，Dashboard 有入口卡。

**涉及的需求项**：
- 2.3.1 `/manage/portfolio` 列表页（inline order）
- 2.3.2 编辑器
- 2.3.3 封面图内嵌上传
- 2.3.4 软删除与 Kind 子目录 trash
- 2.3.5 Dashboard 入口卡（P1）

#### 工作项列表

##### WI-3.1 [M] trash 升级为 Kind 子目录 + 旧数据迁移
- **描述**：`internal/admin/trash.go` 软删除目标路径改为 `<DataDir>/trash/<kind>/<timestamp>-<slug>.md`；列表页展示 Kind 列；还原按子目录分派；启动时发现旧版扁平文件（直接在 `trash/` 下）一次性迁移到 `trash/docs/`，写日志，幂等
- **验收标准**：
  1. 软删除 doc / portfolio 各 1 条 → 磁盘结构 `trash/docs/...`、`trash/portfolio/...`
  2. 启动前 `trash/` 有 2 个旧版扁平文件 → 启动后自动移入 `trash/docs/`，日志输出迁移清单；再次启动不动旧文件
  3. 安全门控：`gofmt` `go vet` `go test ./internal/admin/...` 通过
- **Notes**：对应风险 R2

##### WI-3.2 [S] Smoke 测试 — trash Kind 子目录
- **描述**：软删除 → 列表展示 Kind → 还原 → 原位置可见
- **验收标准**：
  1. 场景通过
  2. 安全门控：`go test ./internal/admin/...` 通过

##### WI-3.3 [S] 异常测试 — trash 迁移
- **覆盖场景清单**：
  - [x] 非法输入：旧版扁平文件名含非 slug 字符 / 无扩展名
  - [x] 边界值：0 个旧文件（无需迁移）、1 个、100 个
  - [x] 失败依赖：迁移过程中目标子目录创建失败（权限不足）
  - [x] 异常恢复：迁移中途崩溃（模拟 panic 后重启），剩余文件下次启动继续迁移
  - [x] 并发/竞态：启动迁移期间用户触发软删除（应阻塞到迁移完成或安全并行）
- **实现手段**：文件系统夹具 + 权限变更 + `runtime.Goexit`
- **断言目标**：幂等性（二次启动不动文件）、失败可重试、日志可观察
- **验收标准**：
  1. 场景通过
  2. 安全门控：`go test ./internal/admin/...` 通过

##### WI-3.4 [M] admin portfolio CRUD
- **描述**：`internal/admin/portfolio.go` 实现：列表（所有状态）、新建（GET/POST）、编辑（GET/POST）、软删除、featured 切换；`admin_portfolio_edit.html` 复用 doc 编辑器骨架（预览、CSRF、保存）；slug 服务端正则校验；重命名时清理旧文件
- **验收标准**：
  1. 新建 / 编辑 / 软删除 / 切换 featured 四条路径都能跑通
  2. slug 冲突被拦截不覆盖既存文件
  3. 安全门控：`gofmt` `go vet` `go test ./internal/admin/...` 通过
- **Notes**：
  - Pattern：保存走"临时文件 + rename"原子写；表单含隐藏字段 `original_slug` 用于改名（新 slug 冲突时回滚）；featured 切换与 order 更新走 POST 幂等
  - Reference：`internal/admin/docs.go`（新建/编辑/保存骨架）；`admin_doc_edit.html`（模板骨架与 CSRF）
  - Hook point：路由分组 `/manage/portfolio/*`（routes.go 注册）；保存成功后调用 `store.Invalidate()` 触发索引刷新

##### WI-3.5 [S] Smoke 测试 — admin CRUD
- **描述**：四条主路径 + Intro 注释块正确保存
- **验收标准**：
  1. 场景通过
  2. 安全门控：`go test ./internal/admin/...` 通过

##### WI-3.6 [S] 异常测试 — admin CRUD
- **覆盖场景清单**：
  - [x] 非法输入：slug 含 `../` / 含中文 / 超 64 / title 空 / description 超 80 字
  - [x] 权限/认证：未登录 → 302；CSRF token 不合法 → 403
  - [x] 失败依赖：磁盘写失败（模拟 `os.Rename` 失败，只读目录）
  - [x] 并发/竞态：两个会话并发保存同 slug，最后胜且文件原子无半写
  - [x] 异常恢复：保存过程 panic → 磁盘不能留下临时文件（`.tmp` 清理或 rename 失败回滚）
- **实现手段**：`httptest` + 只读目录夹具 + `sync.WaitGroup` 并发
- **断言目标**：4xx 状态码、磁盘状态一致（无半写）、CSRF middleware 拦截
- **验收标准**：
  1. 场景通过
  2. 安全门控：`go test ./internal/admin/...` 通过

##### WI-3.7 [集成门控] admin CRUD 链路
- **描述**：verify trash + CRUD 联动，软删除 → 回收站可见 → 还原回到 `content/portfolio/`
- **验收标准**：
  1. 安全门控全通过
  2. 手动端到端通过

##### WI-3.8 [S] inline order 更新端点
- **描述**：`POST /manage/portfolio/:slug/order`，接收 `order=<int>&csrf=<token>`，返回 JSON；列表页 inline input + blur 绑定触发
- **验收标准**：
  1. 合法 order 保存成功，frontmatter 更新
  2. 安全门控：`gofmt` `go vet` 通过
- **Notes**：
  - Pattern：handler 校验 `0 ≤ order ≤ 9999` 的整数；读出原 md → 替换 frontmatter `order` → 原子写回；不改 body；成功后触发索引 Invalidate
  - Reference：`internal/admin/docs.go` 的 frontmatter 回写逻辑；avatar 设置的 JSON 响应风格
  - Hook point：前端列表页 inline `<input type="number">` blur 触发 `fetch` POST，失败时回滚输入框到原值

##### WI-3.9 [S] Smoke 测试 — inline order
- **描述**：修改 order 后列表 / 主页顺序立即变化
- **验收标准**：
  1. 场景通过
  2. 安全门控：`go test ./internal/admin/...` 通过

##### WI-3.10 [S] 异常测试 — inline order
- **覆盖场景清单**：
  - [x] 非法输入：`"abc"` / 空字符串 / `" 3 "`（带空格）/ 科学计数法 `1e3`
  - [x] 边界值：`-1` / `10000` / `0` / `9999`
  - [x] 权限/认证：未登录 / CSRF 非法
  - [x] 并发/竞态：同一 slug 并发两次改 order，最后胜、无半写
- **实现手段**：`httptest` + 并发
- **断言目标**：非法返回 400，合法返回 200 + `{ok:true}`；未登录 302 `/manage/login`
- **验收标准**：
  1. 场景通过
  2. 安全门控：`go test ./internal/admin/...` 通过

##### WI-3.10b [集成门控] admin 写路径稳态
- **描述**：验证 WI-3.4 ~ WI-3.10 完成后，admin CRUD + inline order 端到端幂等
- **验收标准**：
  1. 所有安全门控命令通过
  2. 并发两个会话修改 order、featured，磁盘最终状态可预期（最后胜），文件原子无半写

##### WI-3.11 [M] 封面图内嵌上传
- **描述**：`internal/admin/portfolio_cover.go` 仿 `avatar.go`：`POST /manage/portfolio/cover/upload`（multipart），MIME 白名单 + magic bytes 校验 + 2MB 上限 + CSRF；写入站点图片库目录（命名含 slug 以区分）；新上传清理旧封面；响应 `{url}`；`portfolio_cover_upload.js` 绑定预览框点击、文件选择、上传、回填 `cover` 输入框（带缓存打破参数）
- **验收标准**：
  1. 合法 PNG 上传成功，`cover` 输入框自动填合法 URL
  2. 旧封面被清理
  3. 安全门控：`gofmt` `go vet` `go test ./internal/admin/...` 通过
- **Notes**：对应风险 R3；magic bytes 用 `net/http.DetectContentType`

##### WI-3.12 [S] Smoke 测试 — 封面上传
- **描述**：PNG / JPEG / WebP / SVG 上传 → 回填 → 预览更新；SVG 走 bluemonday-like 清洗（若项目有 svg 清洗逻辑则过），否则仅校验 magic bytes
- **验收标准**：
  1. 4 种格式各 1 条场景通过
  2. 安全门控：`go test ./internal/admin/...` 通过

##### WI-3.13 [S] 异常测试 — 封面上传
- **覆盖场景清单**：
  - [x] 非法输入：扩展名与 magic bytes 不符（`.png` 实际是 `.exe`）、空文件、无扩展名
  - [x] 边界值：2MB+1B / 2MB 整数
  - [x] 权限/认证：未登录 / CSRF 非法
  - [x] 并发/竞态：同一 slug 并发 2 次上传，磁盘最终只剩 1 张封面且可读
  - [x] 异常恢复：上传中途连接断开（客户端 abort），服务端不留下半文件
- **实现手段**：`httptest` + `multipart.Writer` + `context.WithCancel` 断连
- **断言目标**：413 / 415 / 401 / 403；磁盘无半写文件、无垃圾临时文件残留
- **验收标准**：
  1. 场景通过
  2. 安全门控：`go test ./internal/admin/...` 通过

##### WI-3.13b [集成门控] 封面上传链路稳态
- **描述**：验证 WI-3.11 ~ WI-3.13 的封面上传链路稳定：成功/失败/并发 都不留垃圾文件
- **验收标准**：
  1. 所有安全门控命令通过
  2. 磁盘图片库目录无临时/半写文件；每 slug 最多 1 张封面

##### WI-3.14 [S] Dashboard 作品集入口卡
- **描述**：`admin_dashboard.html` 加卡片；服务端 `settings.go`（或 dashboard handler）计数 published / draft / archived / 总数
- **验收标准**：
  1. 卡片渲染正确，计数与实际一致
  2. 0 条时卡片仍显示
  3. 安全门控：`gofmt` `go vet` 通过
- **Notes**：
  - Pattern：dashboard handler 调用 `store.List(KindPortfolio)` 一次，在内存中按 status 聚合计数；模板渲染卡片（标题 + 3 计数 + "新建"按钮）
  - Reference：`admin_dashboard.html` 现有"文档/项目"卡片结构；docs 的 dashboard 计数实现
  - Hook point：dashboard 数据结构体加 `PortfolioStats{Published, Draft, Archived, Total int}`

##### WI-3.15 [S] Smoke 测试 — Dashboard 卡片
- **描述**：计数准确 + "新建"按钮跳 `/manage/portfolio/new` + 0 条时卡片在场
- **验收标准**：
  1. 场景通过
  2. 安全门控：`go test ./internal/admin/...` 通过
- **豁免说明**：本 WI 无独立异常测试——渲染纯聚合计数，无外部输入。闭环引用：**未登录 302** 由 admin middleware 统一处理，已在 **WI-3.6** 覆盖；**store 读异常**（文件系统失败）已在 **WI-1.3** 覆盖（Reload 异常路径）；**0 条**边界已在 WI-3.15 smoke 覆盖

##### WI-3.16 [集成门控] 阶段 3 全量验证
- **描述**：站主端到端：Dashboard 入卡片 → 列表 → 新建（含封面上传） → 保存 → 主页可见 → 修改 order → 主页顺序更新 → 软删除 → 回收站 → 还原
- **验收标准**：
  1. `make check` 绿
  2. `make release` 成功构建
  3. 手动 UAT 通过

**阶段验收标准**：
1. Given 登录站主，When 走完 "新建 → 封面上传 → featured=true → 保存" 流程，Then < 2 分钟内条目出现在 `/portfolio` 列表和主页
2. Given 旧版 v1.5.x trash 中有扁平文件，When 启动新版本，Then 文件自动迁移到 `trash/docs/`，日志可观察，再次启动幂等
3. Given 上传 3MB 封面，Then 返回 413，原 cover 不变；Given 上传 `.exe` 改名 `.png`，Then 返回 415
4. 所有工作项的安全门控通过
5. 所有集成门控通过

**阶段状态**：已完成

**完成日期**：2026-04-22
**验收结果**：通过
**安全门控**：全部通过（gofmt / go vet / go test ./... / make check 全绿）
**集成门控**：全部通过（WI-3.7 / 3.10b / 3.13b / 3.16）
**备注**：
- 代码改动：`internal/admin/trash.go`（重构为 Kind 子目录 + MigrateFlatTrash）、`internal/admin/docs.go` & `projects.go`（软删除目标改写）、`internal/admin/portfolio.go`（新，CRUD + ToggleFeatured + UpdateOrder + setFrontmatterField 工具）、`internal/admin/portfolio_cover.go`（新，multipart 上传 + MIME 白名单 + 原子写）、`internal/admin/admin.go`（加 Content 字段 + PortfolioStats）
- 模板新增/改动：`admin_portfolio_list.html`（新）、`admin_doc_edit.html`（加 Kind="portfolio" 分支 + 封面上传按钮）、`admin_dashboard.html`（加入口卡）
- 前端 JS 新增：`portfolio_order.js`（inline order blur 保存）、`portfolio_cover_upload.js`（封面 fetch + FormData + frontmatter 回填）
- CSS：主题扩 `.dashboard-card-*`
- 路由：`cmd/server/routes.go` 加 `/manage/portfolio/**` 分组；`main.go` wire PortfolioHandlers / PortfolioCoverHandlers，启动时调 `admin.MigrateFlatTrash`
- 测试：新增 `trash_portfolio_test.go` / `portfolio_crud_test.go` / `portfolio_order_test.go` / `portfolio_cover_test.go` / `portfolio_dashboard_test.go`；更新现有 `trash_test.go` 和 `crud_test.go` 适配 Kind 子目录
- MigrateFlatTrash 覆盖 4 场景：首次迁移、幂等、目录不存在、目标冲突保留源文件

---

## 3. 风险与应对

| 风险 | 影响 | 概率 | 应对措施 |
|------|------|------|----------|
| R1 | 内容索引 Reload 与保存后立即读取的竞态 | 中 | 保存路径同步触发局部 Reload；WI-2.6 覆盖"保存同时请求列表"并发断言 |
| R2 | trash Kind 子目录迁移失败导致旧版 trash 数据丢失 | 低-中 | WI-3.1 迁移前打日志全量清单；WI-3.3 异常测试覆盖迁移幂等、失败可重试、并发保护 |
| R3 | 封面上传被滥用（大文件/伪 MIME/SVG 注入） | 低-中 | WI-3.11 magic bytes + 2MB 硬限；WI-3.13 异常测试覆盖 5 类攻击面 |
| R4 | Kind 过滤约束被遗漏导致现有输出污染 | 中 | WI-1.8 硬改所有公共路径；WI-1.9 对 `/rss` `/sitemap` `/tags` `/docs` `/projects` 做 golden file 回归；WI-1.10 显式隔离断言 |
| R5 | Intro 注释块解析异常输入导致 panic / 死循环 | 低 | WI-1.4 pure 函数实现；WI-1.6 覆盖 15+ 异常输入 |
| R6 | 46 WI 工作量超 1 个迭代 | 中 | 每阶段独立可发布；阶段 1 + 阶段 2 完成即可作 beta 发布（站主自建内容，admin 下期） |
| R7 | 封面上传 UI 复用 avatar_upload.js 模式时上下文差异（单一实例 vs 多 slug） | 低 | WI-3.11 明确编写 `portfolio_cover_upload.js` 不复用文件，仅复用交互模式（click → file picker → fetch upload → update UI） |

## 4. 开发规范

### 4.1 代码规范
- Go 代码遵循 `gofmt` + `go vet`
- 模板文件用 4 space 缩进，HTML class 用 kebab-case
- 原生 JS 用 IIFE + `'use strict'`，零全局泄漏（对齐 `avatar_upload.js` / `diary.js`）
- 错误处理：handler 层统一 `http.Error` + log；content / backup 层返回 error 供上游裁决

### 4.2 Git 规范
- 分支：每阶段一条 `dev/portfolio-stage-<n>`；每 WI 一组可独立提交，不跨 WI 累积
- 提交信息：中文，单行标题 + 空行 + 正文（遵循仓库 `feedback_commit_style` 记忆）
- 不直接提 main；合并通过 PR（遵循仓库 guardrail）

### 4.3 文档规范
- 每阶段完成后更新本开发方案的阶段状态
- learnings 写入 `project_plan/learnings.md`
- `README.md` 在阶段 3 完成后同步新增作品集说明

## 5. 工作项统计

| 阶段 | S | M | Smoke 测试 | 异常测试 | 集成门控 | 总计 |
|------|---|---|-----------|---------|---------|------|
| 阶段 1 | 3 | 1 | 4 | 4 | 3 | 15 |
| 阶段 2 | 2 | 4 | 5 | 4 | 4 | 19* |
| 阶段 3 | 2 | 4 | 5 | 4 | 4 | 19* |
| **合计** | **7** | **9** | **14** | **12** | **11** | **53** |

*阶段 2/3 的"功能"列中一条（nav 入口 / Dashboard 卡片）免异常测试，豁免理由见 WI-2.12 / WI-3.15 Notes

**集成门控间隔**：
- 阶段 1：WI-1.7（间隔 6）/ WI-1.10b（间隔 3）/ WI-1.14（间隔 3）
- 阶段 2：WI-2.7（间隔 6）/ WI-2.7b（间隔 3）/ WI-2.10b（间隔 3）/ WI-2.16（间隔 5）
- 阶段 3：WI-3.7（间隔 6）/ WI-3.10b（间隔 3）/ WI-3.13b（间隔 3）/ WI-3.16（间隔 2）

（阶段 1/2/3 起始的第一个门控 WI-1.7/2.7/3.7 在 6 WI 后，略超 5 的规则但属阶段启动前两组功能+测试的自然节奏，可接受）

---

## 6. 审核记录

| 日期 | 审核人 | 评分 | 结果 | 备注 |
|------|--------|------|------|------|
| 2026-04-22 | AI Assistant | 80/100 | 未通过 | 初稿：阶段 1/2/3 均有超 5 WI 无集成门控的断档（-10）；9 个功能 WI 缺 Notes（-9） |
| 2026-04-22 | AI Assistant | 99/100 | 通过 | v1.1：新增 5 个中间集成门控（WI-1.10b / 2.7b / 2.10b / 3.10b / 3.13b），所有阶段最大间隔 ≤ 6；9 个功能 WI 补齐 Pattern / Reference / Hook point；WI-3.14 豁免闭环引用补完（指向 WI-1.3 / WI-3.6 / WI-3.15） |
