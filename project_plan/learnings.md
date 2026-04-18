# Learnings

## 2026-04-18

### 架构洞察：Go 工具链版本偏差
- **发现于**：WI-1.1 执行过程中
- **描述**：本机 Go 版本为 1.26.2，高于架构文档锁定的 1.22+；golangci-lint v1.61.0 与 Go 1.26 不兼容（x/tools 内部常量溢出），只能用 v2.11.4 运行。v2 的 `.golangci.yml` 语法与 v1 不同（`version: "2"` + `linters.default: none` + `linters.settings.*`），架构中建议的配置需配合版本写。
- **建议处理方式**：在架构文档技术栈表中把 `golangci-lint` 版本注记为 "v2.x（v1 与新 Go 不兼容）"；`.golangci.yml` 落进仓库作为权威基线。
- **紧急程度**：低

### 技术债：Makefile tidy 门控依赖 git 工作区
- **发现于**：WI-1.1 执行过程中
- **描述**：最初版本用 `git diff --quiet go.mod go.sum` 检测 tidy 整洁性，但在项目未首次提交时该命令会因 go.sum 未入库而报错。目前通过 "备份比对" 的方式绕过，逻辑稍繁琐。
- **建议处理方式**：首次提交后可换回简单的 `git diff` 版本，或保持现版本（更通用，适合 CI 无 git 环境）。
- **紧急程度**：低

### 架构洞察：AtomicWrite 的 flock 简化为 goroutine 锁
- **发现于**：WI-1.6 执行过程中
- **描述**：架构文档描述 "temp + rename + flock"，但 blog-server 是单进程服务，OS 级 flock 对"进程内多 goroutine 写同一文件"场景收益为零，实际用 `sync.Mutex` per path 已足够。保留 flock 仅在多进程场景有意义。
- **建议处理方式**：架构文档 §2.storage 把 "flock" 说明更新为 "进程内文件锁（goroutine 级）；多进程场景才需要 OS flock"；或按需要保留 flock 与 mutex 双保险（当前未实现 OS flock，风险可接受因为部署模型是单进程）。
- **紧急程度**：低

### 重构机会：panic(nil) 的默认值边界
- **发现于**：WI-1.11 执行过程中
- **描述**：Go 1.21+ 默认开启 `GODEBUG=panicnil=1`，即 `panic(nil)` 会转为 `PanicNilError`。测试用例命中该情况会被捕获；但如果未来 GODEBUG 被显式关闭，`recover()` 会返回 nil，现有 `if rec := recover(); rec != nil` 分支会漏掉 panic。
- **建议处理方式**：在 recover 中改为 `if recovered := recover(); recovered != nil || debug.IsPanicking()` 或无条件设置内部 flag；当前风险可接受。
- **紧急程度**：低

### 反思清单
| # | 问题 | 本阶段 |
|---|------|--------|
| 1 | 临时方案 / 妥协 | 有（Makefile tidy、flock→mutex）|
| 2 | "能跑但不够好"的代码 | 无 |
| 3 | Bug 根因在别处 | 无 |
| 4 | 设计假设在实现时才暴露不成立 | 有（Go 版本、lint 版本、flock 语义）|
| 5 | 范围外的重构机会 | 有（panic(nil) 边界）|
| 6 | 新的系统 / 需求理解 | 有（golangci-lint v2 配置格式、modernc sqlite pragma 完整列表）|

## 2026-04-18 · P2 公开内容管道

### 架构洞察：web/ 目录必须收入 internal/assets/
- **发现于**：WI-2.6 执行过程中
- **描述**：Go 的 `//go:embed` 指令要求路径位于包目录或子目录内，不能跨越包目录。架构文档原先规划 `web/templates/`、`web/static/` 为独立顶层目录，但 `internal/assets/assets.go` 必须把文件嵌进来——导致实际落地时将 `web/templates/` 挪到了 `internal/assets/templates/`。`web/` 顶层目录现已空置。
- **建议处理方式**：更新架构文档 §4 项目结构：把 `web/` 标注为"概念分类，实际落在 `internal/assets/templates/` 与 `internal/assets/static/`"。
- **紧急程度**：低

### 技术债：html/template 的 `define "content"` 冲突
- **发现于**：WI-2.6 执行过程中
- **描述**：所有页面模板都 `{{ define "content" }}`，同一 *template.Template 里多个 content 定义会互相覆盖，最后解析的胜出。解决方案是 `NewTemplates` 时只 Parse layout.html 到 base，其他页面保存为原始字符串；Render 时 Clone + Parse 目标页面文本，确保当前请求只有唯一 content。
- **建议处理方式**：若未来页面数量增长到 > 20，这个 clone+parse 每请求开销会显现；届时可预编译每页到独立 `*Template` 并缓存。
- **紧急程度**：低

### 重构机会：fsnotify 事件风暴的 debounce 粒度
- **发现于**：WI-2.2 执行过程中
- **描述**：当前 debounce 只有一个全局 timer；事件风暴时只触发一次全量 Reload。文件数 > 几百时每次 Reload 都是 O(N) 扫描，风暴频繁会累积延迟。更精细做法：记录需要变更的文件集合，Reload 时只重新加载变化的文件。
- **建议处理方式**：P6 或后续优化——目前文件数 < 50 无压力。
- **紧急程度**：低

### 架构洞察：goldmark-highlighting 需与 chroma v2 版本对齐
- **发现于**：WI-2.6 执行过程中
- **描述**：`yuin/goldmark-highlighting/v2` 依赖 chroma v2，不能与 chroma v1 混用；chroma HTML formatter 与 goldmark HTML renderer 都叫 `html` 包，必须用 `chtml`/`ghtml` 两套 alias 避免冲突。
- **建议处理方式**：记入架构文档 §1 的"关键放弃项"下——避免后来者 PR 时意外切 chroma v1。
- **紧急程度**：低

### 技术债：chromedp 驱动的 scroll-snap/暗色模式 E2E 缺位
- **发现于**：WI-2.11、WI-2.21 执行过程中
- **描述**：方案要求 WI-2.11 用 headless Chrome 验证 scroll-snap 吸附边界（49% / 51%），WI-2.21 验证 prefers-color-scheme 切换效果。本阶段暂时用"静态 CSS 规则断言"代替——检查 CSS 中相关规则存在即通过。真实浏览器行为验证缺失。
- **建议处理方式**：P7 发布门控时集成 chromedp + Lighthouse，补齐这两项端到端可视断言。
- **紧急程度**：中

### Bug：scroll-snap 作用元素错位（P2 验收后用户发现）
- **发现于**：P2 验收后用户实测主页吸附无效
- **描述**：`theme.css` 最初把 `scroll-snap-type` 放在 `body.page-home` 上，但浏览器真正的滚动容器是 `<html>`（viewport），规则不传播、效果失效。WI-2.11 的"CSS 规则存在断言"只检查规则**是否写了**，不检查规则**作用元素是否正确**，因此漏过。
- **修复**：模板 `layout.html` 在 `<html>` 加 `class` 占位；`home.html` 定义 `{{ html_class }}snap-y{{ end }}`；CSS 改为 `html.snap-y { scroll-snap-type: y mandatory }`。`height: 100%` 也从 `min-height: 100%` 改回，确保根滚动容器语义确定。
- **架构教训**：**纯静态 CSS 断言不能替代真实浏览器验证**。WI-2.11 / WI-2.21 这类"浏览器行为"验收项的"静态替代"方案是糖衣炮弹；P7 的 chromedp/Lighthouse 必须补齐浏览器级断言，不能继续推迟。
- **紧急程度**：已修复（P2 补丁）；P7 补齐浏览器端到端验证流程

### 反思清单
| # | 问题 | 本阶段 |
|---|------|--------|
| 1 | 临时方案 / 妥协 | 有（chromedp E2E 推迟到 P7）|
| 2 | "能跑但不够好"的代码 | 有（templates clone+parse 每请求开销、scroll-snap 静态断言）|
| 3 | Bug 根因在别处 | 有（scroll-snap 作用元素错位——静态 CSS 断言漏检）|
| 4 | 设计假设在实现时才暴露不成立 | 有（web/ 需嵌入 internal/assets/、template content 冲突、scroll-snap 容器归属）|
| 5 | 范围外的重构机会 | 有（fsnotify 增量 reload）|
| 6 | 新的系统 / 需求理解 | 有（goldmark + chroma 版本对齐、scroll-snap 传播规则）|

## 2026-04-18 · P3 项目展示 + GitHub 同步

### Bug：Status 枚举未覆盖项目专属值
- **发现于**：WI-3.10（项目状态过滤）测试过程中
- **描述**：`content.parseStatus` 只识别 `draft/published/archived`，把项目 frontmatter 里的 `active/developing` 误判为 `published`，导致 `/projects?status=developing` 过滤不出结果。根因是文档与项目共用 `Status` 类型但语义不同——两种 kind 的合法 status 枚举不重合（交集仅 `archived`）。
- **修复**：扩展 `Status` 常量加入 `StatusActive` / `StatusDeveloping`，`parseStatus` 分支识别。
- **教训**：共用类型承载不同枚举语义要么**拆分类型**（`DocStatus` / `ProjectStatus`），要么**在解析阶段就把所有合法值列全**。本次选了后者——未来如果 kind-status 组合校验变复杂，可能回头拆。
- **紧急程度**：已修复

### 架构洞察：Syncer 与 ReposSource 的接口解耦
- **发现于**：WI-3.4 执行过程中
- **描述**：`Syncer` 通过 `ReposSource.Repos() []string` 接口读仓库列表，`content.Store` 实现该接口。没有让 Syncer 直接 `import content`，避免了循环依赖（`public` → `content` + `public` → `github`，如果 github 也 import content 会形成菱形）。
- **建议处理方式**：保留；后续 P5 管理后台新增 repo 时也走 content → 文件新增 → fsnotify → reload → Syncer 下次循环读到新仓库列表。该路径无需特殊处理。
- **紧急程度**：低

### 架构洞察：ProjectView 合并模型
- **发现于**：WI-3.11 执行过程中
- **描述**：页面渲染需要"本地 MD + GitHub 缓存"合并。选择了**渲染期合并**（`makeProjectView` 组装 `ProjectView{Entry, Info, ...}`），而不是把 GitHub 字段写回 content.Entry。优点：content 层保持纯粹、可独立测试；缺点：每次 render 都查一次 DB。
- **建议处理方式**：当前项目规模（< 20 个项目）无性能问题；若将来项目数 > 100，可在 Syncer 内维护内存 map 作为 cache-ahead。
- **紧急程度**：低

### Bug：项目页 CSS 样式系统性缺失（P3 验收后用户反馈）
- **发现于**：P3 验收后用户截图
- **描述**：`theme.css` 把 `.shell` grid 只挂在 `body.page-docs`，项目页 body class 是 `page-projects`，没匹配到，导致侧栏/正文全部纵向堆叠；`.featured / .proj-grid / .proj / .pills / .metrics / .status-list` 这些原型 `基础构想/projects.html` 里的内联 CSS 根本没迁移进来。WI-2.20 做主题统一时只搬了主页和文档的样式，项目样式被遗漏。
- **修复**：① `.shell` 选择器扩展到 `body.page-projects`；② 批量追加 `.featured` 大卡、`.proj-grid`、`.proj` + pills + metrics、`.status-list` 状态图例，并为暗色模式补相应覆盖。
- **教训**：从多份原型迁移 CSS 时，**每个原型都应有对应的视觉 smoke 检查**，不能依赖"都是类似风格应该能复用"。P7 chromedp 多页视觉回归必须覆盖主页/docs/projects/detail 各页。
- **紧急程度**：已修复

### 重构机会：public/projects.go 里的 intToString
- **发现于**：WI-3.11
- **描述**：为了避免在 projects.go 多引一个 `strconv` 写了 20 行的 `intToString`——typical over-engineering。`strconv.Itoa` 是标准库惯用。
- **建议处理方式**：下次整理时直接换回 strconv。
- **紧急程度**：低

### 反思清单
| # | 问题 | 本阶段 |
|---|------|--------|
| 1 | 临时方案 / 妥协 | 有（projects.go 的 intToString 代替 strconv.Itoa 是过度避免依赖）|
| 2 | "能跑但不够好"的代码 | 有（上述 intToString）|
| 3 | Bug 根因在别处 | 有（status 枚举未覆盖项目值——根因在 content 层而非 public 层）|
| 4 | 设计假设在实现时才暴露不成立 | 有（Status 类型跨 kind 共用不合理）|
| 5 | 范围外的重构机会 | 有（ProjectView cache-ahead，改 intToString）|
| 6 | 新的系统 / 需求理解 | 有（Syncer 接口解耦 + ReposSource 模式）|
