# 作品集（Portfolio）需求文档

> 创建日期：2026-04-22
> 状态：已审核
> 版本：v1.1
> 关联背景：`project_plan/portfolio-open-questions.md`

## 1. 项目概述

### 1.1 项目背景
现有 `/projects` 展示的是 **GitHub 开源仓库**（通过 GitHub API 同步），没有承载非代码成品的容器。站主希望有一块独立的作品集空间，展示设计作品、可视化 demo、往期写作合集等"拿得出手但不是开源仓库"的产物。作品集需要与 `/docs` / `/projects` 结构并列，复用已有的内容管理 / 主题 / 暗色 / 窄屏适配能力，但**在数据和路由上保持独立**，避免互相耦合。

### 1.2 项目目标
1. 站主能通过 `/manage/portfolio` 后台新增、编辑、软删除作品集条目，完整走 draft → published → archived 生命周期
2. 访客能通过 `/portfolio` 列表页（卡片网格）和 `/portfolio/:slug` 详情页访问作品
3. 主页新增"作品集"区块（位于"个人文档"之后），展示 `featured = true` 的作品，卡片布局为"上半图文双栏 + 下半长简介"
4. 作品集不污染 `/docs`、`/tags`、`/projects`、RSS、sitemap 的既有数据源

### 1.3 成功标准
1. 站主在后台新建一个作品（含封面上传），从填表到出现在 `/portfolio` 列表页耗时 < 2 分钟
2. 主页加载耗时相比 v1.5.1 增量不超过 50ms（P95，本地 100 条冷数据基准）
3. 已有 `/docs` / `/projects` / RSS / sitemap / tag 云的输出，在引入作品集后行为零变化（硬断言测试覆盖）
4. v1.6.0 发布后 4 周内，站主至少上线 3 条 `status=published` 的作品（实测 `content/portfolio/` 文件数 ≥ 3）

### 1.4 目标用户
**主要用户**：站主本人（一位习惯写 Markdown 的开发者），目前用 docs 勉强塞非代码成品，但分类不清。
**次要用户**：访问者（面试官 / 同行 / 潜在协作者），需要通过作品集快速建立对站主能力的印象。
当前替代方案：用 `/docs` 的某个 category 塞所有作品条目，但缺少卡片/封面的视觉表达。

### 1.5 核心约束
- **技术栈**：Go net/http + html/template + SQLite KV + 前端原生 JS（沿用仓库现有选型）
- **时间**：作为 v1.6.0 单一主题交付，预期 1 个迭代（S/M 粒度工作项累计 < 20 个）
- **资源**：单人维护，依赖 Claude Code 辅助。不引入新框架 / 新数据库
- **兼容**：不破坏 v1.5.x 的内容模型、路由、导出/备份脚本、trash 回收站行为

---

## 2. 功能需求

### 2.1 内容模型与存储

#### 2.1.1 作品条目文件格式
- **描述**：作品集条目以 Markdown 文件落盘，放 `content/portfolio/*.md`，frontmatter 复用 docs/projects 风格。
- **用户场景**：站主用编辑器（本地或管理后台）创建/修改作品，文件是单一事实来源。
- **字段规范**：

  | 字段 | 类型 | 必填 | 自动 | 说明 |
  |------|------|------|------|------|
  | `title` | string | ✅ | | 作品名 |
  | `slug` | string | ✅ | 管理后台提供默认 | URL 段；slug 在 portfolio Kind 内唯一，允许与 docs/projects 同 slug |
  | `cover` | string | | | 封面图 URL，本地 `/images/…` 或外链 `https://…`，空则用默认 SVG |
  | `description` | string | | | 列表页一句话描述，≤ 80 字 |
  | `category` | string | | | 类别（设计 / 可视化 / 写作合集 / …）自由文本，不做枚举 |
  | `tags` | []string | | | **独立 tag 云**，不与 docs/projects 合流 |
  | `order` | int | | 默认 0 | 手动排序权重，数值小者靠前；相等按 `updated` 倒序 |
  | `demo_url` | url | | | 可选：演示 / 外链地址 |
  | `source_url` | url | | | 可选：源码仓库链接 |
  | `created` | date | ✅ | 新建时写入 | |
  | `updated` | date | ✅ | 每次保存更新 | |
  | `status` | `draft`/`published`/`archived` | ✅ | 默认 `draft` | |
  | `featured` | bool | ✅ | 默认 `false` | 主页是否展示 |

- **body 要求**：body 必填；body **顶部**用 HTML 注释包裹一段"长简介"：
  ```
  <!-- portfolio:intro -->
  这里是主页卡片下半显示的长简介，可多段，支持完整 Markdown。
  <!-- /portfolio:intro -->

  # 详情页从这里开始
  ……
  ```
  解析规则：首次匹配 `<!-- portfolio:intro -->` 与 `<!-- /portfolio:intro -->` 之间的内容为 `Intro`；未包裹时 `Intro` 为空字符串（主页卡片下半不渲染）。包裹块 **不参与** 详情页正文渲染（渲染时剔除注释块本身）。
- **验收标准**：
  - Given 一个合法 portfolio md 文件，When content.Store 扫描，Then 解析出 `Title/Slug/Cover/.../Intro/Body`，其中 `Intro` 为注释块内的 Markdown 源文本（未渲染），`Body` 为去除注释块后的完整 Markdown 源
  - Given frontmatter 缺 `title` 或 `slug` 的文件，When 扫描，Then 该文件被标记为解析失败并跳过，日志写入文件名 + 原因，不阻塞其它文件
  - Given 一个文件 body 里只有完整 body 而没有 `<!-- portfolio:intro -->`，When 扫描，Then `Intro` 为空，不报错
- **优先级**：P0
- **成熟度**：RS3

#### 2.1.2 内容索引：portfolio 类别接入
- **描述**：作品集接入现有的统一内容索引机制（扫描目录 `content/portfolio/`、热更、增量 Reload）；具体常量命名和实现方式由架构阶段决定。
- **用户场景**：所有公共读路径（主页 / /portfolio / 后台列表）走同一套索引，无独立 store。
- **验收标准**：
  - Given 内容索引 Reload 被触发，When 扫描完成，Then 按"portfolio 类别"可取到全部 portfolio 条目（含 draft）
  - Given 管理后台保存一条 portfolio，When 写入磁盘，Then 文件监听触发 Reload，新数据在 100ms 内可被按 portfolio 类别读取到
  - Given 按 docs 类别查询，Then 返回结果**不包含** portfolio 条目（类别隔离）
- **优先级**：P0
- **成熟度**：RS3

#### 2.1.3 导出 / 备份覆盖
- **描述**：`manage.sh export` 与 `internal/backup/*.go` 的打包清单加入 `content/portfolio/` 目录。
- **验收标准**：
  - Given 调用 backup 导出，When 检查导出 tar，Then 包含 `content/portfolio/` 下的所有 md
  - Given 从导出包 restore 到空目录，When 启动服务，Then `/portfolio` 能列出 restore 的条目
- **优先级**：P0
- **成熟度**：RS3

---

### 2.2 公开前台

#### 2.2.1 /portfolio 列表页
- **描述**：GET `/portfolio`，卡片网格（gallery）布局，每卡片展示封面大图 + 标题 + 一句话描述。
- **用户场景**：访客通过导航栏点进来浏览全部作品。
- **输入**：query `?tag=<name>` 可选（tag 筛选）、`?page=<n>` 可选（分页）
- **输出**：HTML 页面，展示 `status=published` 的 portfolio 条目；每页 20 条；分页控件同 `/docs`
- **排序**：`order ASC, updated DESC`
- **验收标准**：
  - Given 有 3 条 published + 1 条 draft，When 匿名 GET `/portfolio`，Then 只看到 3 条 published
  - Given 请求带 `X-Blog-Preview: 1` header 且通过 CSRF/登录态校验，When GET `/portfolio?status=draft`，Then 能看到 draft 条目
  - Given 请求 `?tag=设计`，When 其中一条有 tag "设计"，Then 仅返回该条
  - Given 30 条 published，When `GET /portfolio?page=2`，Then 返回第 11-20 条（含分页元数据）
  - Given 某条目 `cover` 为空，When 渲染卡片，Then `<img>` src 为站点默认 SVG
- **优先级**：P0
- **成熟度**：RS3

#### 2.2.2 /portfolio/:slug 详情页
- **描述**：GET `/portfolio/:slug`，展示封面 + 标题 + meta（category/tags/demo_url/source_url） + body（不含 intro 注释块）。
- **验收标准**：
  - Given `slug=foo` 的 published 条目，When GET `/portfolio/foo`，Then 返回 200，body 渲染后不包含字符串 `portfolio:intro`（注释块已剔除）
  - Given slug 不存在，When GET，Then 404
  - Given slug 对应 draft 且无预览 header，When GET，Then 404
  - Given 条目有 `demo_url` 和 `source_url`，When 渲染，Then 页面显示两个可点击外链
  - Given 同 slug 在 docs 和 portfolio 都存在，When GET `/portfolio/:slug`，Then 命中 portfolio 版本（Kind 隔离）
- **优先级**：P0
- **成熟度**：RS3

#### 2.2.3 主页"作品集"区块
- **描述**：主页 hero 区 + 个人文档区 下面新增"作品集"区块，展示 `status=published AND featured=true` 的全部条目，按 `order ASC, updated DESC`。
- **卡片样式**：
  - 整张卡片占据一行（宽屏下左右贴近内容栏边界）
  - 上半部分左右两栏：左栏是"标题 + description"，右栏是封面（无封面时用默认 SVG）
  - 下半部分为 `Intro` 段渲染后的 HTML（支持完整 Markdown 渲染）
  - 整张卡片可点击跳转 `/portfolio/:slug`
- **"查看全部 ›"**：区块右上角一个链接指向 `/portfolio`
- **空态**：若 featured 条目数为 0，整个区块**不渲染**（不显示空标题）
- **验收标准**：
  - Given 5 条 published 且 featured=true，When 匿名访问 `/`，Then 主页显示 5 张卡片，按 order/updated 排序
  - Given 其中 2 条无 `Intro`（未写 `<!-- portfolio:intro -->`），When 渲染，Then 这 2 张卡片下半部分容器的 DOM 元素**不输出**（或 `display: none`），不占用 margin / padding 高度，卡片总高与只有上半部分一致
  - Given 0 条 featured，When 访问 `/`，Then 不出现"作品集"标题或任何空容器
  - Given 某卡片 `cover` 为空，When 渲染，Then 右栏展示默认 SVG 图
  - Given 窄屏（viewport < 640px），When 渲染，Then 卡片上半两栏折叠为上下单列，图在上，图下是标题 + description，再下面是 Intro
- **优先级**：P0
- **成熟度**：RS3

#### 2.2.4 导航栏入口
- **描述**：顶部 nav 在 "文档" 与 "关于" 之间插入 `<a href="/portfolio">作品集</a>`。
- **用户场景**：访客从任意页面一步进入作品集列表。
- **输入**：无（静态渲染项）
- **输出**：nav `<ul>` 中多出一项 `<li><a href="/portfolio">作品集</a></li>`
- **验收标准**：
  - Given 匿名访客访问 `/`，When 检查 nav HTML，Then 出现 `<a href="/portfolio">作品集</a>`，顺序在"文档"之后、"关于"之前
  - Given 当前路径为 `/portfolio` 或 `/portfolio/:slug`，When 渲染 nav，Then 该链接具备 `aria-current="page"` 或等价的"当前"样式类（与现有 `/docs` 同机制）
  - Given 窄屏（<640px）菜单收起态，When 展开，Then"作品集"项与其它项同排
- **优先级**：P0
- **成熟度**：RS3

#### 2.2.5 每条目 OpenGraph meta
- **描述**：`/portfolio/:slug` 详情页 head 中输出 `og:title`（= title）、`og:description`（= description 或 body 首 160 字）、`og:image`（= cover 或默认 SVG 的绝对 URL）、`og:type="article"`、`og:url`。
- **验收标准**：
  - Given 一个 portfolio 详情页，When 抓取 HTML 并解析 meta，Then 上述 og:* 字段齐全且取值正确
  - Given 条目无 cover，When 渲染，Then og:image 为站点默认 SVG 的绝对 URL（含 scheme + host）
- **优先级**：P1
- **成熟度**：RS3

#### 2.2.6 RSS / sitemap 隔离
- **描述**：作品集不出 RSS、不上 sitemap.xml。
- **验收标准**：
  - Given 存在 10 条 published portfolio，When GET `/rss.xml`，Then 返回项里无任何 portfolio slug
  - Given 同上，When GET `/sitemap.xml`，Then 返回项里无 `/portfolio/*` URL
- **优先级**：P0
- **成熟度**：RS3

---

### 2.3 管理后台

#### 2.3.1 /manage/portfolio 列表页
- **描述**：GET `/manage/portfolio`，列出全部 portfolio 条目（所有状态），表头字段：title / slug / status / featured / order / updated / 操作。操作列：编辑 / 软删除 / 切换 published↔draft / 切换 featured。`order` 字段**在列表页可直接编辑**（inline input，失焦保存）以调整主页展示顺序。
- **验收标准**：
  - Given 登录站主访问 `/manage/portfolio`，Then 看到全部状态条目（含 draft）
  - Given 在列表页修改某条 order 为 3 并失焦，When 前端发 POST，Then 磁盘文件 frontmatter `order` 更新为 3，主页列表顺序立即变化
  - Given 未登录访客访问，Then 302 到登录页
  - Given 列表页 order 输入框填 `"abc"`、空字符串、或带空格的非整数，When 提交，Then 服务端返回 400 + 错误消息，磁盘原值不变
  - Given order 输入小于 0 或大于 9999，When 提交，Then 服务端返回 400 + "order 必须为 0~9999 的整数"，磁盘原值不变
- **优先级**：P0
- **成熟度**：RS3

#### 2.3.2 编辑器（复用 admin_doc_edit.html）
- **描述**：GET/POST `/manage/portfolio/new` 与 `/manage/portfolio/:slug/edit`。编辑器复用现有 `admin_doc_edit.html` 模板，通过 `Kind="portfolio"` 切换字段集（加 `cover` / `demo_url` / `source_url` / `featured` / `order`，去掉 docs 特有字段如 excerpt 若有）。编辑器支持：实时预览、CSRF、保存、历史记录（若 docs 已有）。
- **验收标准**：
  - Given 在 `/manage/portfolio/new` 填完字段并提交，When 保存成功，Then `content/portfolio/<slug>.md` 被写入，frontmatter 和 body 与表单一致
  - Given 编辑一条已有条目，When 修改 body 并保存，Then 磁盘文件被原子替换（临时文件 + rename），无半写入风险
  - Given 表单中 slug 与既有 portfolio 冲突，When 保存，Then 返回错误提示，不覆盖既有文件
  - Given body 里写了 `<!-- portfolio:intro -->...<!-- /portfolio:intro -->`，When 保存后访问主页，Then 卡片下半渲染出注释块内容
- **优先级**：P0
- **成熟度**：RS3

#### 2.3.3 封面图内嵌上传
- **描述**：编辑器的 `cover` 字段左侧加一个"封面预览框"，交互模式与"头像上传"一致：点击框 → 弹文件选择 → 上传 → 服务端将文件写入站点图片库目录 → 成功后自动回填 `cover` 字段（URL 带缓存打破参数）→ 预览框同步更新。同时保留右侧 URL 输入框，支持手填外链（互不冲突，以最后一次提交为准）。
- **约束**：
  - 上传 MIME 白名单：`image/png`、`image/jpeg`、`image/webp`、`image/svg+xml`
  - 文件大小 ≤ 2MB
  - 每条作品只保留一张封面：新上传覆盖旧封面并清理旧文件
  - 要求登录 + CSRF token
- **验收标准**：
  - Given 在编辑器点击封面框并选一张 PNG，When 上传完成，Then `cover` 输入框自动填为站点内可访问的图片 URL（含缓存打破参数），预览框显示该图
  - Given 上传文件超过 2MB，When POST 封面上传，Then 返回 413 且前端显示"封面过大"提示，原 cover 值不变
  - Given 旧封面为 `.jpg`，新上传 `.png`，Then 该作品仅保留新封面，旧文件从磁盘移除
  - Given 未登录或 CSRF 不合法，Then 上传被拒（401/403）
  - Given MIME 不在白名单（如 `image/gif` 或 `application/pdf`），When 上传，Then 返回 415 + "封面类型不支持"
- **优先级**：P0
- **成熟度**：RS3

#### 2.3.4 软删除与回收站
- **描述**：列表页的"软删除"按钮将文件移入 `$DataDir/trash/portfolio/<timestamp>-<slug>.md`，不做物理删除。现有 `/manage/trash` 回收站自动识别 portfolio 类型的条目并支持还原。
- **验收标准**：
  - Given 软删除一条 portfolio，When 检查磁盘，Then `content/portfolio/<slug>.md` 消失，`$DataDir/trash/portfolio/...` 出现对应文件
  - Given 从 `/manage/trash` 还原该条目，When 还原完成，Then 文件回到 `content/portfolio/`，列表与主页重新出现
  - Given trash 中有 docs 和 portfolio 各 1 条，When 访问 `/manage/trash`，Then 两条都可见且类型标注正确
- **优先级**：P0
- **成熟度**：RS3

#### 2.3.5 Dashboard 入口卡
- **描述**：`/manage` 首页新增"作品集"卡片，显示 published / draft / archived 计数、总数与"新建"按钮。
- **用户场景**：站主登录后一眼看到作品集健康度（各状态分布）并能快速进入新建流程。
- **输入**：无（服务端汇总 portfolio 类别条目）
- **输出**：Dashboard 的卡片网格新增一张"作品集"卡片
- **验收标准**：
  - Given 登录访问 `/manage`，When 渲染 Dashboard，Then 看到作品集卡片；published/draft/archived 三个计数与按类别 + 状态过滤的实际条目数逐个一致
  - Given 0 条 portfolio，When 访问 `/manage`，Then 卡片仍显示，三个计数均为 0，"新建"按钮可用
  - Given 点击卡片内的"新建"按钮，When 导航完成，Then 落到 `/manage/portfolio/new`
  - Given 未登录访客访问 `/manage`，Then 302 到登录页
- **优先级**：P1
- **成熟度**：RS3

---

### 2.4 样式与适配

#### 2.4.1 暗色模式
- **描述**：所有新增页面与卡片复用 `theme.css` 的 CSS 变量体系，暗色下自动适配。
- **验收标准**：
  - Given 切换到暗色模式，When 访问 `/portfolio`、`/portfolio/:slug`、`/manage/portfolio`，Then 不出现硬编码浅色背景（背景色来源均为 CSS 变量）
  - Given 同上三个页面，When 运行 `npx @axe-core/cli <url>`（或等价 axe-core 扫描），Then 无 `critical` 级 contrast 违规；文本对比度 ≥ WCAG AA 标准（正文 4.5:1，≥ 18pt 大字 3:1）
- **优先级**：P0
- **成熟度**：RS3

#### 2.4.2 窄屏适配
- **描述**：viewport < 640px 时，列表页卡片改单列，详情页 meta 栏折叠，主页卡片上半两栏折叠为图上 / 文下。
- **验收标准**：
  - Given viewport=375px，When 访问各页面，Then 无水平滚动条；按钮可点击区 ≥ 40px
- **优先级**：P0
- **成熟度**：RS3

---

## 3. 非功能需求

### 3.1 性能要求
- `/portfolio` 列表页服务端渲染 P95 < 200ms（数据规模 <50 条，本地基准）
- 主页加载相比 v1.5.1 增量 < 50ms（P95，100 条冷数据，含主页作品集区块渲染）
- `content.Store.Reload` 在 <200 条总条目（含 docs/projects/portfolio）下 < 500ms

### 3.2 安全要求
- 所有写接口（新建/编辑/软删除/封面上传/order 修改）要求登录 + CSRF token
- 封面上传校验 MIME 白名单 + 文件大小 ≤ 2MB，防上传任意类型
- slug 字段服务端正则 `^[a-zA-Z0-9_-]{1,64}$`，避免路径穿越
- 前台渲染 Intro / body 走现有 goldmark + bluemonday 白名单，与 docs 一致

### 3.3 兼容性要求
- 浏览器：对齐现有站点（最新 2 版 Chrome / Firefox / Safari + Edge）
- 现有 `/docs`、`/projects`、`/tags`、`/rss.xml`、`/sitemap.xml` 输出在引入作品集前后 **字节级一致**（由硬断言测试保证）
- `manage.sh export` 与 `restore` 能跨 v1.5.x ↔ v1.6.x 互通（portfolio 目录在旧版本里忽略即可）

### 3.4 技术约束
- 沿用 Go net/http、html/template、SQLite KV、content.Store、goldmark、bluemonday、CSRF middleware
- 前端不引入打包器、不加新依赖
- 不引入新数据库 / 新消息队列 / 新构建脚本

### 3.5 技术风险与应对

| # | 风险 | 影响 | 概率 | 应对 |
|---|------|------|------|------|
| R1 | 内容索引 Reload 与"管理后台保存-立即读取"的并发竞态：保存成功后 100ms 内读请求可能读到旧索引 | 主页 / 列表页短暂不一致 | 中 | 保存路径同步触发一次局部 Reload 再返回；E2E 测试里用"保存后立即 GET 列表"断言可见新条目 |
| R2 | trash 回收站原先按 docs/projects 类型识别，新增 portfolio 类别可能造成旧 trash 条目恢复失败或错位 | 历史 trash 数据回恢复受损 | 低-中 | trash 记录在目录名中显式带类别（`trash/portfolio/`）；恢复路径按目录名分派；加回归测试覆盖"旧版 trash 数据 + 新版二进制"场景 |
| R3 | 封面上传接口被恶意文件滥用（大文件、伪造 MIME、SVG 内嵌脚本） | 服务器磁盘 / XSS | 低-中 | 2MB 硬上限 + MIME 白名单 + 读 magic bytes 校验；SVG 经 bluemonday 清洗后再引用；上传目录仅作静态文件 `Content-Type` 响应，不随路径执行 |
| R4 | 作品集渗透进 `/docs`/`/tags`/`/projects`/RSS/sitemap：若某公共路径直接 `store.List()` 未带 Kind 过滤，会误包含 portfolio | 现有输出回归、字节级兼容破坏 | 中 | 在现有这些路径加断言测试（对比 v1.5.1 输出或显式检查不含 portfolio slug）；架构阶段统一"按 Kind 过滤"的接口约定 |
| R5 | Intro 注释块解析假设"首个匹配对"在不规范输入下（只写开不写闭 / 嵌套）行为未定 | 渲染异常或死循环 | 低 | 解析前置正则限长（Intro 最长 4KB），缺闭标签时整块回退为 `Intro=""`；用例覆盖三种异常输入 |

---

## 4. 术语表

| 术语 | 含义 |
|------|------|
| Portfolio | 作品集，独立于 `/projects` 的非代码成品展示实体 |
| Intro | 作品 body 顶部用 `<!-- portfolio:intro -->...<!-- /portfolio:intro -->` 包裹的长简介段，只在主页卡片下半渲染 |
| Featured | 作品是否置顶到主页的 bool 开关 |
| Order | 手动排序权重（整数，小者靠前） |
| Kind 隔离 | content.Store 用 Kind 常量区分 docs/projects/portfolio，互不串 |
| 硬断言测试 | 断言"作品集不污染 /docs /tags /projects RSS sitemap"的回归测试 |

---

## 5. 开放问题

1. **默认 SVG 样式**：默认封面 SVG 具体长什么样？（配色 / 图案 / 是否带站点 Logo）— 建议实现时出一版 mock 交评审
2. **历史记录**：docs 编辑器是否已有 body 历史版本？若有，portfolio 是否需要同样接入？本需求默认 **接入现有机制，无就不做**
3. **order 冲突**：两条 featured 条目 `order` 同为 0 时的 tie-break 用 `updated DESC` 已经定下，但如果 updated 时间也完全一致（手动回填），目前随机；可接受否？— 本需求默认可接受
4. **category 聚合页**：是否要 `/portfolio?category=xxx` 筛选？本需求未列入 P0；列入 P2 留做观察

---

## 6. 需求成熟度汇总

| 等级 | P0 数量 | P1 数量 | P2 数量 |
|------|---------|---------|---------|
| RS0  | 0 | 0 | 0 |
| RS1  | 0 | 0 | 0 |
| RS2  | 0 | 0 | 0 |
| RS3  | 14 | 2 | 0 |
| RS4  | 0 | 0 | 0 |
| RS5  | 0 | 0 | 0 |

**P0 条目**：2.1.1 / 2.1.2 / 2.1.3 / 2.2.1 / 2.2.2 / 2.2.3 / 2.2.4 / 2.2.6 / 2.3.1 / 2.3.2 / 2.3.3 / 2.3.4 / 2.4.1 / 2.4.2
**P1 条目**：2.2.5 / 2.3.5

---

## 7. 范围外（下期）

- 拖拽排序（本期用 order 整数手填）
- 批量操作（批量上下架 / 归档）
- 作品 ↔ project 互跳与反向链接
- 访问统计 / 热门榜
- 数据迁移 / 既有作品导入工具（本期手写 MD）
- category 聚合筛选页

---

## 8. 审核记录

| 日期 | 审核人 | 评分 | 结果 | 备注 |
|------|--------|------|------|------|
| 2026-04-22 | AI Assistant | 87/100 | 未通过 | 初稿：缺技术风险节；2.4.1 对比度不具体；2.1.2/2.3.3 实现细节侵入需求；2.3.1 缺 order 异常；2.2.4/2.3.5 G/W/T 粒度不足；1.3 #4 不可度量 |
| 2026-04-22 | AI Assistant | 95/100 | 通过 | v1.1：新增 3.5 技术风险节（R1-R5）；2.4.1 采用 WCAG AA 4.5:1 标准 + axe-core 扫描；2.1.2/2.3.3 移除实现命名细节；2.3.1 补 order 非法值异常；2.2.4/2.3.5 扩展 G/W/T；1.3 #4 替换为可度量指标 |
