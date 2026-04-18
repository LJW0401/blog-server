# blog-server 需求文档

> 创建日期：2026-04-18
> 最后修订：2026-04-18
> 状态：已审核
> 版本：v1.1

## 1. 项目概述

### 1.1 项目背景
市面上的博客托管服务（Hexo/Hugo/WordPress/Notion/语雀/掘金/公众号等）都能解决"写文章+发布"这一件事，但无法把 **个人主页、博客文章、项目展示** 整合成一个审美与交互统一的单一站点。本项目要解决的根本问题是：**让开发者拥有一个由自己完全掌控、审美一致、可以作为作品本身被展示的个人站点**——站点本身就是一件作品。

### 1.2 项目目标
- 提供统一视觉基调（Apple 风格克制极简）下的三类内容载体：个人介绍、文档/博客、项目展示
- 所有动态内容通过服务端管理后台维护，不需要改代码
- 站点本身的前端质量经得起技术同行挑剔
- 部署简单，单人可维护

### 1.3 成功标准
作品集型成功标准，不以流量为主：
- **作者自己愿意把站点链接放进简历、GitHub bio、签名档**
- Lighthouse 公开页 Performance ≥ 90、Accessibility ≥ 95、SEO ≥ 85
- **同行评价量化指标**：上线后邀请 ≥ 3 位同行开发者进行视觉与交互审视，给出≥ 4/5（五分制）的整体质量评分
- 管理后台日常使用不痛苦（新增一篇文档/项目的全流程 < 2 分钟，不含写作时间）

### 1.4 目标用户
- **主要观众**：技术招聘者 + 同行开发者
- **主要入口**：GitHub profile 链接、简历签名档、社交媒体 bio 链接（假设）
- **访问特征**：一次性为主、会点开若干项目/文章快速评估，流量规模估计 < 1k PV/日
- **作者自己**：唯一的内容维护者，同时也是站点的另一类"用户"

### 1.5 核心约束
- **时间**：无硬截止，作品集型，质量优先
- **人力**：单人开发与维护
- **部署**：自有云服务器（VPS），无外部托管依赖
- **技术栈**：未定，留待架构设计阶段决定
- **安全与隐私**：禁止引入任何第三方脚本（含字体 CDN、分析服务等）

---

## 2. 功能需求

### 2.1 M1 · 个人主页

#### 2.1.1 主页结构与吸附滚动
- **描述**：主页为单一域名入口页，分两页 + 底部联系信息区。滚动采用强制吸附（CSS scroll-snap mandatory），不允许停留在页面之间。
- **用户场景**：招聘者点开链接 → 看到"我是谁" → 下滑看作品 → 下滑到底部找联系方式。
- **输入**：无（纯展示）。数据来自 M4 维护的基本信息、M2/M3 的 featured 内容。
- **输出**：
  - 第 1 页左栏：基本信息（标题、tagline、坐标、方向、现状）+ CTA 按钮组
  - 第 1 页右栏："Recently Active" 仓库卡片（由 M6 派生自 M3）
  - 第 2 页第 1 行块：关于我（bio + 技能栈 pills + 经历时间线 + 兴趣）
  - 第 2 页第 2 行块：精选项目卡片 3 张
  - 第 2 页第 3 行块：精选文档 4 篇
  - 底部：联系方式三栏表格
- **验收标准**：
  - Given 用户首次访问主页，When 页面加载完成，Then 第 1 页完整占满视口且首屏 LCP < 1.5s
  - Given 用户在第 1 页滚动超过视口高度的 50%，When 停止滚动，Then 自动吸附到第 2 页，不停留在中间
  - Given 用户在第 2 页向上滚动超过 50%，When 停止滚动，Then 自动吸附回第 1 页
  - Given 用户继续下滑，When 越过第 2 页，Then 吸附到底部联系信息区
- **优先级**：P0
- **成熟度**：RS4（原型已建，结构已验证）

#### 2.1.2 主页"精选"内容的挑选规则
- **描述**：第 2 页展示的 3 个项目和 4 篇文档采用"混合挑选"策略：优先展示后台标记 `featured=true` 的条目（按时间倒序），数量不足时用最新条目补齐。
- **用户场景**：作者精心挑选门面内容；没来得及挑的时候系统自动用最新内容顶上，不出现空位。
- **输入**：M2 文档全集 + M3 项目全集 + 各自的 `featured` 标记
- **输出**：稳定的 3 + 4 条展示数据
- **验收标准**：
  - Given 已有 ≥ 3 个 `featured=true` 项目，When 渲染主页，Then 只展示 featured 项目中最新更新的 3 个
  - Given featured 项目不足 3 个，When 渲染主页，Then 用 `status=published` 非 featured 项目按 `updated_at` 倒序补足到 3 个
  - Given featured + 已发布项目总数不足 3 个，When 渲染主页，Then 只展示现有数量，不报错
- **优先级**：P0
- **成熟度**：RS3

#### 2.1.3 背景光斑呼吸动效
- **描述**：每页背景铺设 3–4 颗柔光光斑（`radial-gradient` + `blur(80px)` + `mix-blend-mode: multiply`），具备呼吸（scale + opacity）+ 漂移（translate）动画。
- **验收标准**：
  - Given 用户访问任意公开页，When 页面加载完成，Then 背景光斑可见且平滑循环动画
  - Given 用户系统偏好 `prefers-reduced-motion: reduce`，When 渲染页面，Then 光斑降级为静态低透明度状态，无动画
- **优先级**：P0
- **成熟度**：RS4

---

### 2.2 M2 · 文档（博客）

#### 2.2.1 文档存储与元数据
- **描述**：每篇文档为服务器上的一个 `.md` 文件，顶部为 YAML frontmatter 元数据，下方为 Markdown 正文。元数据不单独存 DB。
- **frontmatter 字段**：
  - `title`（必填）
  - `slug`（必填，URL 用，英文/数字/短横线，全局唯一）
  - `tags`（数组，可空）
  - `category`（字符串，用于"目录"侧栏）
  - `created`（创建日期，YYYY-MM-DD）
  - `updated`（更新日期，YYYY-MM-DD）
  - `status`（`draft` / `published` / `archived`）
  - `featured`（布尔，默认 false）
  - `excerpt`（可选，列表页摘要；若省略则取正文首 120 字）
- **验收标准**：
  - Given 文档 frontmatter 缺失 `title` 或 `slug`，When 系统加载该文件，Then 跳过并记录错误日志，不影响其它文档
  - Given 两个文档 `slug` 重复，When 系统启动扫描，Then 启动报错并拒绝启动，日志指出冲突文件
- **优先级**：P0
- **成熟度**：RS3

#### 2.2.2 文档列表页
- **描述**：`/docs` 路径，展示所有 `status=published` 的文档，按 `updated` 倒序。左侧为四项主导航（文档主页 / 目录 / 标签 / 归档）+ 标签云 + 归档时间线；右侧为文档列表（第一篇为 featured 卡片样式，其余为分割线列表样式）+ 排序切换 + 分页。
- **用户场景**：访客想浏览作者写过的所有文章，可以按标签/归档/目录快速筛选。
- **边界条件**：
  - 草稿（`draft`）和已归档（`archived`）对公众不可见
  - 已归档文档仅出现在 "归档" 分类下
  - 每页 10 条
  - 分页采用 URL query（`?page=2`）
- **验收标准**：
  - Given 有 20 篇 published 文档，When 未登录用户访问 `/docs`，Then 首页显示最新 10 篇，分页显示 2 页
  - Given 用户点击某标签 pill，When 筛选生效，Then 仅显示含该标签的已发布文档
  - Given 用户以管理员身份登录，When 访问 `/docs`，Then 草稿不默认出现在列表中（草稿只通过管理后台入口访问）
- **优先级**：P0
- **成熟度**：RS3

#### 2.2.3 文档详情页
- **描述**：`/docs/:slug` 路径。服务端渲染 Markdown → HTML。包含文章正文、元数据头（标题、标签、日期、阅读时长估算）、阅读次数计数（M9）、上一篇/下一篇导航。
- **用户场景**：访客点击列表页条目进入。
- **边界条件**：
  - 仅 `status=published` 可被访客访问
  - `status=draft` 仅管理员登录态下可访问（草稿预览）
  - `status=archived` 可访问但列表页不默认显示
  - 非法 slug 返回 404
- **验收标准**：
  - Given 访客请求 `/docs/valid-slug`（published），When 服务端渲染，Then 返回 200 + 渲染后的 HTML，并记一次阅读
  - Given 访客请求 `/docs/draft-slug`（draft），When 用户未登录，Then 返回 404
  - Given 管理员登录后请求 `/docs/draft-slug`，When 服务端渲染，Then 返回 200 + 页面顶部显示 "草稿预览" 标识
- **优先级**：P0
- **成熟度**：RS3

#### 2.2.4 目录/标签/归档分类视图
- **描述**：文档列表左侧侧栏提供四种浏览入口：
  - **文档主页**：默认视图，所有已发布文档
  - **目录**：按 frontmatter 的 `category` 字段分组
  - **标签**：按 `tags` 交叉过滤（多标签激活语义为 AND）
  - **归档**：按 `updated` 年份分组倒序
- **切换规则**：任意两个视图之间切换时，前一视图的筛选条件（如已激活标签集合）自动清空。
- **验收标准**：
  - Given 用户在标签面板同时激活 "TypeScript" 和 "工程化"，When 列表刷新，Then 仅显示同时携带两个标签的文档（AND 语义）
  - Given 用户点击的 category 下无任何已发布文档，When 列表渲染，Then 显示"暂无内容"占位文案而非 404
  - Given 用户点击 "归档" → "2025"，When 筛选生效，Then 仅展示 `updated` 位于 2025-01-01 至 2025-12-31 范围内的已发布文档，按 `updated` 倒序
  - Given 用户在标签视图激活筛选后切换到"归档"视图，When 视图切换，Then 前一视图的标签筛选被清空，归档视图从空白状态开始
  - Given 标签云中某标签已无已发布文档（被归档或删除），When 侧栏渲染，Then 该标签从标签云中移除，不保留空 pill
- **优先级**：P0
- **成熟度**：RS3

---

### 2.3 M3 · 项目展示

#### 2.3.1 项目数据模型
- **描述**：每个项目 = `projects/<slug>.md` 文件 + 服务端定时从 GitHub API 拉取的元数据缓存。本地文件承载"作者写的项目长文"和"展示覆盖字段"；GitHub 数据承载 Star/Fork/语言/push 时间等客观指标。
- **本地 frontmatter 字段**：
  - `repo`（必填，`owner/name` 格式，对接 GitHub）
  - `slug`（必填，URL 用，全局唯一）
  - `display_name`（中文展示名，默认用 `repo` 的 name 部分）
  - `display_desc`（中文短描述，覆盖 GitHub description）
  - `category`（后端服务 / 前端界面 / 开发者工具 / 实验项目）
  - `stack`（字符串数组，技术栈标签）
  - `status`（`active` / `developing` / `archived`）
  - `featured`（布尔）
  - `created`、`updated`（日期）
- **GitHub 合并字段**（渲染时合并，不落入 MD）：`stars`、`forks`、`primary_language`、`pushed_at`、`readme_excerpt`
- **验收标准**：
  - Given 本地 frontmatter 缺失 `repo`，When 系统加载，Then 跳过并记录错误
  - Given GitHub API 拉取失败，When 渲染项目卡片，Then 使用上一次成功缓存 + 显示"同步于 xx 分钟前"
- **优先级**：P0
- **成熟度**：RS3

#### 2.3.2 GitHub API 同步
- **描述**：后台定时任务每 30 分钟拉取所有已登记仓库的元数据，写入 SQLite 缓存表。成功时更新 `last_synced_at`；失败时保留旧缓存。
- **Token 策略**：
  - 配置中 `github.token` 可为空 → 使用未登录 API（60 次/小时）
  - 提供 Token → 使用登录态（5000 次/小时）
  - Token 明文存配置文件，文件权限 600，不入 Git
- **限流安全裕度约束**：稳态 API 消耗必须 ≤ 未登录限流额度的 **50%**（即 ≤ 30 req/h）。超出时必须通过以下手段降耗：①发起条件请求（`ETag` / `If-Modified-Since`），304 响应不计入限流；②拉长同步周期；③要求用户配置 Token。
- **验收标准**：
  - Given `github.token` 为空 + 已登记仓库数 × (60 / sync_interval_min) 的理论峰值 API 消耗 > 30，When 服务启动扫描配置，Then 日志输出 WARN，并在管理后台首页显示黄底提示"当前同步配置接近 GitHub 未登录限流，建议配置 Token 或拉长周期"
  - Given 服务器正常运行 24 小时，When 对实际 API 调用进行审计，Then 平均每小时对 GitHub 的未 304 请求数 ≤ 30（未登录）或 ≤ 2500（登录）
  - Given 某次同步命中 GitHub `429 Too Many Requests`，When 服务识别到限流头 `Retry-After`，Then 退避至该时间后再次尝试，期间保留旧缓存
  - Given 同步发生异常（网络/限流/404），When 某仓库同步失败，Then 仅该仓库保留旧缓存，其它成功的正常更新，错误入日志
  - Given 用户访问项目页，When 页面渲染，Then 显式可见 "同步于 X 分钟前" 字样；超过 2 小时未同步成功时字样变灰并加标"同步异常"
- **优先级**：P0
- **成熟度**：RS3

#### 2.3.3 项目列表页
- **描述**：`/projects` 路径，左侧侧栏为分类（全部/后端服务/前端界面/开发者工具/实验项目）+ 技术栈云 + 状态筛选（活跃维护/正在开发/已归档），右侧为首篇 featured 大卡 + 2 列卡片网格 + 分页。
- **验收标准**：
  - Given 用户点击 "正在开发" 状态筛选，When 筛选生效，Then 仅显示 `status=developing` 的项目
  - Given 有 featured 项目，When 渲染列表，Then featured 中最近更新的一个以大卡样式渲染在顶部；其余项目进入网格区
  - Given 当前筛选条件下无结果，When 列表渲染，Then 显示"暂无项目"占位文案而非空白
  - Given URL 携带非法 `page` 参数（负数 / 非数字 / 超出总页），When 请求到达，Then 回退到第 1 页而非 500
  - Given 每页 10 条且总数 7 条，When 渲染列表，Then 分页器不出现
- **优先级**：P0
- **成熟度**：RS3

#### 2.3.4 项目详情页
- **描述**：`/projects/:slug` 路径，服务端渲染本地 MD 正文（作者的长文）+ GitHub 元数据合并面板（Star / Fork / 最近 push / 主语言）+ GitHub README 摘要（可选）+ 仓库链接按钮。
- **验收标准**：
  - Given 访客访问 `/projects/valid-slug`，When 服务端渲染，Then 返回 200 + 渲染后的 HTML（含 GitHub 指标）
  - Given 仓库已归档（本地 `status=archived`），When 渲染详情页，Then 状态标签显示"已归档"，但页面仍可访问
  - Given 项目刚登记、GitHub 缓存尚未建立（首次同步前），When 详情页渲染，Then 指标面板显示"正在首次同步"占位而非 0 / NaN
  - Given 某仓库在 GitHub 被作者删除，When 下次同步返回 404，Then 项目详情页顶部显示"远端仓库不可达"黄色提示，本地 MD 正文仍可阅读
  - Given README 抓取失败但其他字段成功，When 页面渲染，Then README 摘要区域不渲染（不报错、不占位），其他字段正常显示
  - Given 访问不存在的 slug，When 请求到达，Then 返回 404 + 友好错误页
- **优先级**：P0
- **成熟度**：RS3

---

### 2.4 M4 · 管理后台

#### 2.4.1 登录流程
- **描述**：`/manage/login` 渲染登录表单（用户名 + 密码），提交后服务端 bcrypt 比对通过则设置 Session Cookie（HttpOnly + Secure + SameSite=Strict + CSRF token），重定向到 `/manage`。
- **用户场景**：作者在浏览器输入 `/manage` → 未登录则重定向到 `/manage/login` → 输入凭据 → 进入后台。
- **验收标准**：
  - Given 未登录用户访问任何 `/manage/*` 路径，When 请求到达，Then 302 → `/manage/login?next=...`
  - Given 用户输入正确凭据，When 提交登录表单，Then 设置 HttpOnly Cookie + 302 → 原请求路径
  - Given 用户连续 5 次密码错误，When 第 6 次尝试，Then 返回 429 并要求 10 分钟后重试（按 IP 限流）
  - Given 用户主动点"登出"，When 请求到达，Then 清除 Session 并 302 → 主页
- **优先级**：P0
- **成熟度**：RS3

#### 2.4.2 密码策略与默认密码告警
- **描述**：默认密码 `666`；密码使用安全口令哈希（建议 bcrypt，cost ≥ 10）存储于配置；密码未被修改过时，**所有公开页顶部显示警示 banner**："默认密码未修改，请尽快进入后台修改"。
- **默认密码识别机制**：系统使用独立的标志位 `password_changed_at`（值为 `null` 或 ISO 时间戳）判断是否为默认状态——**不通过哈希比对默认值来判定**（避免盐化后判断不可靠）。`null` 时触发 banner；一旦任何"修改密码"操作成功，该字段被永久写入时间戳，banner 消失。
- **验收标准**：
  - Given 首次部署、`password_changed_at = null`，When 访客访问任意公开页，Then 页面顶部显示黄底警示 banner
  - Given `password_changed_at` 已有时间戳，When 访客访问公开页，Then 无 banner
  - Given 管理员修改了密码（哪怕改回 `666`），When 保存成功，Then `password_changed_at` 被设置为当前时间，banner 从此不再显示
  - Given 用户在后台修改密码，When 新密码短于 8 位，Then 拒绝并提示"密码至少 8 位"
  - Given 有人手动把 `password_changed_at` 改回 `null`（通过文件编辑），When 下次请求到达，Then banner 再次出现（系统以标志位为唯一依据）
- **优先级**：P0
- **成熟度**：RS3

#### 2.4.3 文档/项目编辑器
- **描述**：后台提供**带语法高亮的纯文本 Markdown 编辑器**（单窗口，无预览面板、无拖拽上传、无富文本按钮）。编辑器读写服务器上的 `.md` 文件，保存即落盘。
- **功能**：列表视图 → 新建/编辑/删除/归档 → 保存。frontmatter 与正文在同一编辑区内编辑。
- **验收标准**：
  - Given 管理员点击"新建文档"，When 进入编辑器，Then 展示预填的 frontmatter 模板（空值 + `status: draft`）
  - Given 管理员保存文档，When 服务端校验通过（slug 唯一、frontmatter 合法），Then 写入对应路径 + 200 响应
  - Given slug 冲突，When 保存，Then 返回错误提示，不覆盖已有文件
  - Given 管理员误删文档，When 点击"删除"，Then 先显示确认对话框；确认后将文件移入 `trash/` 目录而非直接删除
- **优先级**：P0
- **成熟度**：RS3

#### 2.4.4 图片上传（独立入口）
- **描述**：后台有独立的"图片管理"页，支持选择本地文件上传。上传后返回路径（如 `/images/xxxxxx.png`），管理员自行复制粘贴进 MD 正文。**不与编辑器耦合**，无拖拽、无粘贴上传。
- **验收标准**：
  - Given 管理员选择 ≤ 5MB 的图片，When 点击上传，Then 返回相对路径 + 可见缩略图
  - Given 文件 > 5MB 或类型非图片，When 上传，Then 返回错误提示
- **优先级**：P0
- **成熟度**：RS3

#### 2.4.5 基本信息/联系方式编辑
- **描述**：后台提供表单页编辑 M1 主页的基本信息（标题、tagline、坐标、方向、现状）和联系方式（媒体链接、QQ 群号等）。数据持久化到配置文件或轻量关系型存储（架构阶段定）。前台读取带 ≤ 30s 的缓存 TTL。
- **字段约束**：标题、tagline 非空；媒体链接必须匹配 `^https?://` 前缀；QQ 群号必须为 5–12 位纯数字。
- **验收标准**：
  - Given 管理员修改 tagline 并保存，When 访客刷新主页（或等待 30s 后刷新），Then 看到新 tagline
  - Given 管理员留空 tagline 并提交，When 表单校验，Then 返回错误 "tagline 不能为空"，数据不落盘
  - Given 管理员输入媒体链接 "bilibili.com"（缺少协议前缀），When 提交，Then 返回错误 "媒体链接需以 http:// 或 https:// 开头"
  - Given 管理员输入 QQ 群号 "abc"，When 提交，Then 返回错误 "QQ 群号必须为 5–12 位数字"
  - Given 两个浏览器会话同时打开编辑表单并先后保存，When 第二次保存到达，Then 以最后提交者为准（last-write-wins），不抛并发错误；日志中记录覆盖事件
  - Given 保存过程中服务崩溃（模拟），When 服务重启，Then 要么完整保留新值，要么完整保留旧值，不出现半写入状态（原子替换）
- **优先级**：P0
- **成熟度**：RS3

#### 2.4.6 登记 GitHub 仓库
- **描述**：后台可增/删/修改项目的 `repo` 绑定。新增时在保存前即时调用 GitHub API 校验仓库存在（返回 200 才允许保存），落库后进入常规同步周期。删除时同步清理本地 MD 与缓存。
- **验收标准**：
  - Given 管理员输入 `foo/nonexistent`，When 提交新增，Then 调用 GitHub `GET /repos/{owner}/{name}`，404 时返回错误"GitHub 未找到此仓库"，不落盘
  - Given GitHub API 暂不可达（网络错误 / 5xx），When 用户提交新增，Then 返回错误"无法校验仓库，请稍后重试"，不落盘
  - Given 某 `repo` 已登记，When 管理员再次以相同 `owner/name` 新增，Then 返回错误"该仓库已登记"
  - Given 管理员删除某已登记仓库，When 提交删除，Then `projects/<slug>.md` 被移入 `trash/`，对应 GitHub 缓存行被清理，下次访问该 slug 返回 404
  - Given 校验 API 调用恰好命中 GitHub 限流，When 提交，Then 返回错误"GitHub API 限流中，请稍后重试"，并在后台日志记录；不占用后续普通同步额度
- **优先级**：P0
- **成熟度**：RS3

---

### 2.5 M5 · 存储层

#### 2.5.1 存储介质总览
- **描述**：
  - **MD 文件**：`content/docs/*.md`、`content/projects/*.md`（具体路径架构阶段定）
  - **图片**：`images/` 平铺目录
  - **配置**：`config.yaml`（含 GitHub token、管理员凭据、站点标题等），文件权限 600
  - **轻量关系型存储**（建议 SQLite）：访问统计、GitHub 缓存、Session（可选）
- **并发与完整性要求**：所有 MD 写入必须通过"写临时文件 + 原子 rename"实现，避免半写入；同一文件并发写入由进程内文件锁串行化。
- **验收标准**：
  - Given 服务启动时任一存储目录缺失，When 初始化，Then 自动创建空目录并记录 INFO 日志，不崩溃
  - Given 服务启动时 `content/` 目录为只读，When 初始化，Then 启动失败并记录 ERROR 日志，提示"内容目录不可写"
  - Given 数据库文件损坏无法打开，When 服务启动，Then 把旧文件改名为 `<file>.corrupt.<timestamp>` 并重建空 DB，内容文件不受影响
  - Given 两个并发请求尝试写同一 MD 文件，When 写入到达，Then 通过文件锁串行化；任一写入中途失败，原文件保持完整不变
  - Given 保存过程中进程被 `kill -9`，When 服务重启后读取该文件，Then 要么是旧完整版本、要么是新完整版本，不出现半写入中间态
- **优先级**：P0
- **成熟度**：RS3

#### 2.5.2 备份
- **描述**：每日 03:00 本地时间自动冷备份：`content/` + `images/` + SQLite 文件打包到 `backups/YYYYMMDD.tar.gz`，保留最新 7 份，更早的自动清理。
- **验收标准**：
  - Given 备份任务执行成功，When 检查 `backups/` 目录，Then 出现当日备份文件
  - Given 已有 7 份备份，When 新备份生成，Then 最旧一份被删除
- **优先级**：P1
- **成熟度**：RS3

---

### 2.6 M6 · 主页 "Recently Active" 仓库

- **描述**：主页右栏展示 3 个最近活跃的开源项目，数据**从 M3 派生**（不单独维护）：取 `status != archived` 的项目按 GitHub `pushed_at` 倒序取前 3。
- **验收标准**：
  - Given M3 有 5 个非归档项目，When 渲染主页，Then 右栏显示 `pushed_at` 最近的 3 个
- **优先级**：P1
- **成熟度**：RS3

---

### 2.7 M7 · RSS / 站点地图 / OG 卡片

#### 2.7.1 RSS
- **描述**：`/rss.xml` 输出所有 `status=published` 文档的 RSS 2.0。
- **优先级**：P2
- **成熟度**：RS1

#### 2.7.2 Sitemap
- **描述**：`/sitemap.xml` 输出所有公开可访问页面。
- **优先级**：P2
- **成熟度**：RS1

#### 2.7.3 OG / Twitter 卡片
- **描述**：每个文档/项目详情页输出 Open Graph + Twitter Card meta，让链接在社交媒体/聊天工具中有美观预览。
- **优先级**：P2
- **成熟度**：RS1

---

### 2.8 M9 · 访问统计

- **描述**：仅记录**每篇文档**的阅读次数（不含项目页、不含主页）。服务端记录，不走前端埋点。
- **触发规则**：
  - 仅 `/docs/:slug` 返回 200 时 +1
  - 同一 IP + User-Agent 的指纹 **60 分钟** 内仅计 1 次（简易去重）
  - 爬虫 UA（GoogleBot/Bingbot 等常见名单）不计数
- **展示**：文档详情页底部显示"阅读 × 次"
- **验收标准**：
  - Given 访客首次访问某文档页，When 返回 200，Then 阅读次数 +1
  - Given 同一访客（相同 IP+UA）10 分钟内刷新 5 次，When 每次返回 200，Then 阅读次数只 +1
  - Given 同一访客 59 分钟内再次访问，When 返回 200，Then 阅读次数不变
  - Given 同一访客 61 分钟后再次访问，When 返回 200，Then 阅读次数 +1（去重窗口结束）
  - Given GoogleBot UA 访问某文档页，When 返回 200，Then 阅读次数不变
  - Given 同一 IP 不同 UA（移动/桌面）访问，When 均返回 200，Then 分别各计 1 次
- **优先级**：P1
- **成熟度**：RS3

---

### 2.9 M10 · 暗色模式

- **描述**：所有公开页 + 管理后台都实现暗色主题，**完全跟随系统** `prefers-color-scheme`，**不提供手动切换按钮**。
- **暗色下的光斑背景**：降低饱和度 + 降低亮度，避免深色底上刺眼；动效保留。
- **验收标准**：
  - Given 用户系统为暗色模式，When 访问任意页面，Then 使用暗色主题
  - Given 用户切换系统主题，When 无需刷新，Then 页面主题同步切换
  - Given 页面在暗色模式，When 观察背景光斑，Then 饱和度/亮度较浅色模式降低可辨
- **优先级**：P0
- **成熟度**：RS3

---

## 3. 非功能需求

### 3.1 性能要求
- 公开页 LCP < 1.5s（本地 3G 模拟）
- Lighthouse 公开页：Performance ≥ 90、Accessibility ≥ 95、SEO ≥ 85
- 首屏 HTML 单个请求 < 50KB（gzipped）
- 单 VPS 支撑 ≥ 1k PV/日，峰值 20 QPS 下 P95 响应时间 < 300ms

### 3.2 安全要求
- 全站强制 HTTPS（Let's Encrypt 自动续签），HTTP 自动 301 → HTTPS
- 响应头基线：CSP（禁止 `unsafe-inline`、禁止第三方源）、HSTS（max-age ≥ 31536000）、X-Content-Type-Options: nosniff、X-Frame-Options: DENY、Referrer-Policy: strict-origin-when-cross-origin
- Cookie：HttpOnly + Secure + SameSite=Strict
- 登录：bcrypt 哈希（cost ≥ 10）+ CSRF token + IP 限流（5 次失败锁 10 分钟）
- 不允许引入任何第三方脚本（含字体 CDN、统计、评论、广告）
- GitHub token 存配置文件（权限 600，不入 Git），不在任何响应中回显

### 3.3 兼容性要求
- 桌面：Chrome、Safari、Firefox、Edge 最近 2 个大版本
- 移动：iOS Safari ≥ 16、Chrome for Android 最近 2 个大版本
- 不支持 IE、不支持 iOS Safari < 16
- 所有页面完整响应式，断点至少覆盖 360px / 768px / 1024px / 1440px

### 3.4 技术约束
- 部署在单台自有 VPS
- 无 CDN、无外部托管（字体、图片、脚本）
- 数据可持久化至 SQLite + 本地文件系统
- 内容（MD + 图片）必须能**脱离系统独立迁移**（无任何只锁在 DB 的业务数据）
- 技术栈选型留待架构设计阶段

### 3.5 运维要求
- 结构化日志（JSON 行），保留 30 天后自动轮转清理
- 每日 03:00 冷备份 MD + images + SQLite，保留 7 份
- 服务启动不依赖外部服务（GitHub 不可用时仍能正常启动，项目页走缓存）

---

## 4. 术语表

| 术语 | 含义 |
|-|-|
| 作品集型站点 | 站点本身即作品，优先审美/交互质量，不以流量为首要指标 |
| frontmatter | MD 文件顶部的 YAML 元数据块，`---` 包围 |
| featured | 手动标记为"精选"的文档或项目，会进入主页展示位 |
| slug | URL 路径中标识条目的短字符串（如 `typescript-engineering`） |
| 草稿预览 | 管理员登录态下按 URL 访问未发布内容的能力 |
| 吸附滚动 | CSS `scroll-snap-type: y mandatory`，滚动时强制停靠在指定锚点 |
| 管理页 | `/manage` 路径下的后台，需鉴权 |

---

## 5. 开放问题

| # | 问题 | 说明 |
|-|-|-|
| O1 | 技术栈未定 | 留给架构设计阶段决定（Go / Node / Rust 等均可） |
| O2 | 是否引入 Git 版本化 MD 内容 | 本版本未要求，但作者可能后续希望把 `content/` 作为 Git 仓库托管 |
| O3 | 数据迁移/导出功能 | 当前依赖文件系统可直接拷贝，未设计专门的导出入口 |
| O4 | 访问统计的长期存储策略 | SQLite 保留全量阅读日志是否合适，N 年后可能膨胀 |
| O5 | 默认密码不强制修改的风险 | 通过"前台持续警示 banner"平衡；如果 banner 策略出现漏洞将成为实际安全问题 |
| O6 | 图床容量 | 单个 VPS 磁盘有限，若图片增多需考虑清理/迁移策略 |
| O7 | 草稿预览的 URL 形式 | 是与 published 同路径（靠登录态区分）还是用独立签名链接？本版本默认前者 |

---

## 6. 需求成熟度汇总

| 等级 | P0 数量 | P1 数量 | P2 数量 |
|------|---------|---------|---------|
| RS0  | 0       | 0       | 0       |
| RS1  | 0       | 0       | 3       |
| RS2  | 0       | 0       | 0       |
| RS3  | 16      | 3       | 0       |
| RS4  | 2       | 0       | 0       |
| RS5  | 0       | 0       | 0       |

- **P0 需求**：共 18 条，**全部达到 RS3 及以上**，满足"P0 在定稿时 ≥ RS3"的目标
- **P1 需求**：共 3 条（M5 备份、M6 主页活跃仓库、M9 阅读统计），均为 RS3
- **P2 需求**：共 3 条（RSS / sitemap / OG），均为 RS1，体现"可以有"的定位

---

## 7. 验收总关口

上线发布前必须满足：
1. 所有 P0 需求实现并通过各自验收标准
2. Lighthouse 公开页三项指标达标
3. 安全基线响应头全部正确输出
4. 首次部署默认密码 banner 警示正常触发
5. GitHub 不可达时项目页走缓存、不崩溃
6. 每日备份任务运行正常，可成功恢复
7. 所有公开页在 iPhone 13 宽度（390px）下无横向滚动、无溢出
8. **数据可脱离系统独立迁移**：在另一台机器上仅复制 `content/` + `images/` + `config.yaml` + DB 文件，即可重建完整站点（含所有文章、项目、统计历史）

---

## 审核记录

| 日期 | 审核人 | 评分 | 结果 | 备注 |
|------|--------|------|------|------|
| 2026-04-18 | AI Assistant | 70/100 | 未通过 | 4 个 P0 停留 RS2；GitHub 限流风险、默认密码识别机制未显式约束 |
| 2026-04-18 | AI Assistant | 93/100 | 通过 | 按 B1–B6 全部整改：P0 全部达 RS3；补充限流安全裕度、password_changed_at 机制、并发/原子性约束、量化成功标准 |
