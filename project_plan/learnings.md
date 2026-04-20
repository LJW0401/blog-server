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

## 2026-04-18 · P4 管理后台鉴权

### 架构洞察：config.yaml 变可写 vs site_settings 二选一
- **发现于**：WI-4.10 执行过程中
- **描述**：密码修改需持久化 `admin_password_bcrypt` + `password_changed_at`。两种路径：① 重写 config.yaml（yaml.v3 + atomic rename）；② 把可变字段迁到 site_settings 表。选择了 ①（保持架构 §2.config 的设定）。代价：用户在 config.yaml 里写的注释被 yaml.v3 discards。
- **建议处理方式**：在 `config.yaml.example` 注释里写清"本文件会被管理端覆写；持久化更改请从后台进行"。
- **紧急程度**：低

### 架构洞察：Session Cookie 的 UA binding 副作用
- **发现于**：WI-4.4 执行 `TestSession_Edge_UABindingRejectsMismatch`
- **描述**：为了满足 R10 "Cookie 未绑定 IP/UA 存在 session fixation 风险"，session payload 嵌入 UA 前 64 字节的哈希（`UAFP`），parse 时重算比对。带来副作用：浏览器切换（或 UA 被安全软件改写）会导致登录态失效。作品集型单管理员场景可接受。
- **建议处理方式**：日志里 slog.Warn "session_ua_mismatch" 帮助诊断；不放到硬检查里。
- **紧急程度**：低

### Bug：测试用 bcrypt 哈希需实际生成
- **发现于**：WI-4.11 TestPassword_Smoke 首次运行
- **描述**：把 `defaultPasswordHash` 随手写了字符串冒充 bcrypt("supersecret")，格式是对的但 salt 是假的——`VerifyPassword` 返回 mismatch，整个 admin 测试套件全挂。根因：bcrypt 是有 salt 的哈希，必须用 `bcrypt.GenerateFromPassword` 真生成一个固定哈希。
- **修复**：本地生成一次 `bcrypt("supersecret")` 的哈希并以 const 方式 inline 到 `admin_test.go` 中。
- **教训**：有 salt 的哈希无法"编造"fixture；应该在 init 时 `once.Do` 真实计算一次并缓存，或用 test helper 生成。
- **紧急程度**：已修复

### 重构机会：admin 包里的 url() / itoa() helper
- **发现于**：WI-4.1、WI-4.10 执行过程中
- **描述**：为了"少引一个 strconv/net/url 包"，admin.go 里手写了 `itoa` 和 wrapping `urlEscape`。与 P3 的 intToString 同病——typical over-engineering。
- **建议处理方式**：整理时统一换回 `strconv.Itoa` 和 `url.QueryEscape`。
- **紧急程度**：低

### 架构洞察：gate 与 login page 的分叉放置
- **发现于**：WI-4.6 实现过程中
- **描述**：`/manage/login` 本身必须**不**被 AuthGate 拦截（否则未登录就永远进不去登录页），所以只能把 protected 路由挂在另一套 mux 上，login/logout 放在 public mux。`mux.Handle("/manage", middleware.AuthGate(...))` + `mux.Handle("/manage/password", ...)` 两条独立挂载是解决之道——net/http 的 path 路由没有"除了 X 其它都拦截"的原生语法。
- **建议处理方式**：若路由数量增多，可以引入 chi.Router 的 `Group(func(r) { r.Use(authGate); r.Get(...)... })` 让结构更清晰。目前简单够用。
- **紧急程度**：低

### 反思清单
| # | 问题 | 本阶段 |
|---|------|--------|
| 1 | 临时方案 / 妥协 | 有（admin.go 里的 url()/itoa() 手写避 strconv/net/url）|
| 2 | "能跑但不够好"的代码 | 有（同上 + net/http mux 的双路由拼装）|
| 3 | Bug 根因在别处 | 有（bcrypt fixture 用字符串冒充——哈希有 salt 必须真算）|
| 4 | 设计假设在实现时才暴露不成立 | 有（UA binding 副作用、config.yaml 变可写的注释保留问题）|
| 5 | 范围外的重构机会 | 有（admin helper 换 strconv/net/url、引入 chi Router 分组）|
| 6 | 新的系统 / 需求理解 | 有（net/http mux 无 exclude 语义、session UA-fp 的取舍）|

## 2026-04-18 · P5 管理后台 CRUD

### 技术债：CodeMirror 编辑器推迟到 P7
- **发现于**：WI-5.2 执行过程中
- **描述**：需求 2.4.3 要求"带语法高亮的纯文本编辑器（CodeMirror 6）"。完整打包 CodeMirror 需要 Node.js + esbuild 构建前端 bundle；当前项目没有 Node 工具链。P5 用带 monospace 字体的纯 textarea 替代，保留 JS-disabled 降级（天然满足）。
- **建议处理方式**：P7 "发布精致化"前引入前端构建步骤——Makefile 加 `frontend-build` target 跑 esbuild，产物 embed 到 `internal/assets/static/js/editor.js`。
- **紧急程度**：中（直接影响作品集质感，但不阻塞功能）

### 架构洞察：`internal/settings` 小包拆分避免 cycle
- **发现于**：WI-5.13 执行过程中
- **描述**：admin 要写 site_settings，public 要读。如果把 settings 逻辑放在 admin 或 public 任一侧，另一方 import 就形成循环。拆出独立 `internal/settings/Store`，admin 和 public 都 import 这个 KV 包，解决得很自然。配合 Handlers 字段注入（`SettingsDB`），实现关注点分离。
- **建议处理方式**：类似"纯数据存取层"的包（比如未来的 content.Store 可能也会拆出）统一放 `internal/`，不放 admin/public 里。
- **紧急程度**：低

### 重构机会：main.go buildAdminMux 拆分模式
- **发现于**：WI-5.20 集成门控前的 lint 压轴
- **描述**：main() 的圈复杂度冲到 28（超过 20 阈值）——P5 往 mux 挂了 10 多条 admin 路由。拆成 `routes.go` 的 `buildAdminMux()` + `postOrGet` 小工具后，main 降回可控。
- **建议处理方式**：后续阶段新增路由时直接改 routes.go，main() 保持精简。未来迁到 chi.Router 的 `Group(...)` 时这层抽象也顺手迁过去。
- **紧急程度**：低

### 架构洞察：reload-on-write 避免 fsnotify 延迟
- **发现于**：WI-5.3 测试新建文档后立刻索引可见
- **描述**：admin save/delete 除了落盘之外，会主动调 `cstore.Reload()`。否则测试（以及真实用户）要等 fsnotify 的 debounce（200ms）才能看到变化。成本：每次 save 做一次全量扫描（几十个 MD 毫秒级）；好处：管理端"保存即可见"的体验直觉。
- **建议处理方式**：若项目规模 > 数百个 MD，改成只刷新当前 slug 的部分 reload。目前规模下无需。
- **紧急程度**：低

### Bug（部署操作性）：`go run` 残留进程占端口导致"新代码没生效"
- **发现于**：P5 完成后用户反馈 /manage 登录后 404
- **描述**：用户重启服务时没意识到之前的 `go run ./cmd/server` 进程还活着，占着 8391 端口。新 `./blog-server` 启动失败（ERROR 日志被忽略），浏览器请求全部打到旧进程。旧进程是 P4 之前版本，没有 /manage/docs 等路由，所以后续访问 404。`go run` 产出的二进制在 `~/.cache/go-build/.../server` —— 进程名是 `server`，ps 里不容易和我们项目联系起来。
- **修复**：`pkill -f cmd/server` 杀孤儿，然后 `go build -o blog-server ./cmd/server && ./blog-server`。
- **教训**：① 启动脚本先检查端口占用再 bind 失败 abort；② Makefile 的 dev target 可以 `go run -trimpath` + 自定义 `-o` 路径避免缓存命名混淆；③ 把"ERROR listen addr already in use"升级为 `os.Exit(1)` 让失败更明显（当前代码 goroutine 里 `os.Exit(1)`，但 sleep 后父进程未察觉就前台跑其他命令）。
- **紧急程度**：低（非代码 bug，是操作习惯 + 日志可见性不足）

### Bug：gocyclo 阈值与项目实际情况
- **发现于**：WI-5.20 lint 阶段
- **描述**：最初 `.golangci.yml` 设 gocyclo min-complexity=15；`main()` 和 `ImagesUpload` 都自然超过 15（多个 validation gate + 多个错误分支）。调整到 20 并在配置里加注释解释原因。Web handler 的典型"多 guard + early return"结构本来就是高圈复杂度。
- **建议处理方式**：保留 20；若某新函数超过 20，说明该拆了。
- **紧急程度**：低

### 反思清单
| # | 问题 | 本阶段 |
|---|------|--------|
| 1 | 临时方案 / 妥协 | 有（CodeMirror 推迟 P7、reload-on-write 简化策略）|
| 2 | "能跑但不够好"的代码 | 有（buildAdminMux 里的 switch statement 对 `/edit`/`/delete` 后缀判断——比路由框架笨）|
| 3 | Bug 根因在别处 | 无 |
| 4 | 设计假设在实现时才暴露不成立 | 有（gocyclo 15 对 web handler 过严）|
| 5 | 范围外的重构机会 | 有（全局换 chi.Router、引入 esbuild 前端构建）|
| 6 | 新的系统 / 需求理解 | 有（site_settings 跨包共享的包拆分技法、reload-on-write vs watch-only 取舍）|

## 2026-04-18 · P6 统计 + 备份 + 迁移验收

### 架构洞察：stats DB 失败不阻断页面渲染
- **发现于**：WI-6.1 设计时
- **描述**：stats.RecordRead 的签名是 `(ctx, slug, ip, ua)` 不返回 error。需求 2.8 明确："DB 写失败 → 页面仍正常渲染（计数损失可接受），错误入日志"。因此把 DB 错误吞在内部、只记 slog，允许调用方不 handle error。这是"可观测但不阻断"的典型模式，和 auth.Store 的 RegisterFailure 相同。
- **建议处理方式**：未来所有"监测/度量类"API 都应走这个约定，功能类 API 则明确返回 error。在 docstring 里标明选择。
- **紧急程度**：低

### 重构机会：public.Handlers 字段堆积
- **发现于**：WI-6.2 把 Stats 加入 Handlers 时
- **描述**：Handlers 现在有 Content/Tpl/GitHubCache/SettingsDB/Stats/Settings/Logger 七个字段，外加两个 cache 相关的 sync 字段——已经超过"构造函数好管"的阈值。字段一个一个往上堆的坏处：测试每次都要实例化一堆，注入顺序容易乱。
- **建议处理方式**：P7 发布前重构为 `Deps struct{ ... }` 显式依赖结构，`NewHandlers(deps)` 构造。目前不 block。
- **紧急程度**：低

### 架构洞察：backup 用 time.Until(nextBoundary) 替代 cron
- **发现于**：WI-6.6 设计时
- **描述**：原考虑 `github.com/robfig/cron`，但每日一次固定点的需求太简单——引入一个三方依赖不值。写了 10 行 `nextBoundary` 函数，`time.NewTimer(time.Until(next))` 等到 03:00 执行，然后循环。副作用：ctx.Done 时 timer 能干净取消，没额外 goroutine 逃逸。
- **建议处理方式**：等需求变复杂（多任务/多时间点）再引入 cron 库。
- **紧急程度**：低

### 重构机会：backup tar.gz 流式 write 对大文件
- **发现于**：WI-6.7 自测试
- **描述**：当前 `addTree` 用 `io.Copy(tw, f)` 流式写，不把整个文件读内存，OK。但 gzip.Writer 默认级别是 DefaultCompression（-1 → 6）；对数千 MD 文件可能较慢。测试用 sample 1 个 MD 秒级完成，真实使用数据 > 100MB 时可能值得降到 BestSpeed。
- **建议处理方式**：配置项 `backup_compression_level`，默认 BestSpeed；监控备份时长，> 30s 告警。
- **紧急程度**：低（非瓶颈 until scale）

### 反思清单
| # | 问题 | 本阶段 |
|---|------|--------|
| 1 | 临时方案 / 妥协 | 有（backup 自实现调度而非引入 cron 库）|
| 2 | "能跑但不够好"的代码 | 有（public.Handlers 字段堆积）|
| 3 | Bug 根因在别处 | 无 |
| 4 | 设计假设在实现时才暴露不成立 | 无 |
| 5 | 范围外的重构机会 | 有（Deps struct、backup 压缩级别可调、stats 按时间粒度聚合）|
| 6 | 新的系统 / 需求理解 | 有（监测类 API 用"吞错入日志"而非返回 error 的约定）|

## 2026-04-18 · quick-feature：about_* 字段上后台

### 架构洞察：public 测试夹具应统一接 SettingsDB
- **描述**：public_test 的 `setup()` 创建 Handlers 时没接 SettingsDB，导致所有 site_settings 相关路径在测试里走"SettingsDB == nil"分支。结果 about_* / tagline 缓存这类功能没法被测验证。这次补了一次全局。
- **建议处理方式**：保留；未来加 site_settings 派生的 UI 字段，直接 `h.SettingsDB.Set(key, val)` 灌进去测。
- **紧急程度**：已修

### 测试/文档缺口：后端字段 ↔ 后台表单的对应关系没有自动化守护
- **描述**：about_* 四个字段 **P5 种子化时已在 public.about() 里读 DB**，但后台 `/manage/settings` 表单没同步挂上，用户实际改不了——发现这个断层 2 次（P5 + P7 补 "关于我" 时都没注意）。真正暴露是在用户运行时说"9 项要能管理"时。
- **建议处理方式**：维护一个 const slice（`public.AboutKeys`）同时被 admin settings form 和 public.about() 读，CI 用反射/常量表验证"所有 AboutKeys 在表单出现"。短期先靠 code review + learnings 兜底。
- **紧急程度**：中

### 重构机会：静态资源应有 fingerprint 破缓存
- **发现于**：用户把 CSS 里新增的 "关于我" 卡片样式后，页面仍显示为纯文字
- **描述**：P7 加了 `Cache-Control: public, max-age=604800`（7 天）给 /static/*。这在生产是对的，但开发期会让"改了 CSS 用户看不到"成为反复卡点（已经是第二次，第一次是 P2 验收后的 scroll-snap bug）。
- **建议处理方式**：给静态资源 URL 加内容哈希 `/static/css/theme.<sha8>.css`，这样 CSS 变了 URL 就变、浏览器自动拉新版本。实现：启动时计算每个静态资源的 sha，在模板 funcmap 里暴露 `{{ staticURL "css/theme.css" }}`。
- **紧急程度**：中（影响反复开发体验，生产侧不痛）

### Bug：pickFeatured 仍用 `==published` 过滤项目（种子化后暴露）
- **发现于**：种子化 3 个项目后用户发现主页"主要开源项目"为空
- **描述**：`pickFeatured` 是 P2 写的，当时项目和文档共用 `StatusPublished`。P3 修 "status 跨 kind" 时只改了 `filterProjectsByStatus`，没改 pickFeatured；测试的 fixture 当时也不足以暴露——`TestHome_Smoke_RecentlyActiveDerived` 只测了右栏派生路径，featured 槽位走 `pickFeatured(projs, 3)` 从未被断言过。
- **修复**：抽 `isFrontpageVisible(entry)` 辅助函数，按 `Kind` 分派。docs→published，projects→active|developing。新增回归测试 `TestHome_Edge_FeaturedProjectsIncludeActiveAndDeveloping`。
- **教训**：修"跨 kind 共用类型"的 bug 要全仓库 `grep` 相关常量引用。这次第二次栽在同一个设计缺陷上——可能是时候拆 `DocStatus` / `ProjectStatus` 两个独立类型。
- **紧急程度**：已修复；设计级重构可排上 backlog

## 2026-04-18 · P7 精致化 + 发布门控

### 架构洞察：gzip 中间件的"content-type 延迟嗅探"
- **发现于**：WI-7.9 实现 Gzip middleware 时
- **描述**：gzip 压缩决策依赖响应的 `Content-Type`，但 handler 不一定在 `Write()` 前显式 `Set("Content-Type")`；Go 的 ResponseWriter 会在首次 Write 时自动嗅探 MIME。解法：在 `gzippedResponseWriter` 里第一次 Write 时如果 CT 还没定（空字符串），不提交 `sniffed`，下一次 Write 再判定。这个"lazy sniff"模式能兼容两种 handler 写法。
- **建议处理方式**：未来增加第三方 gzip-lib 时保留此行为；有文档覆盖更好。
- **紧急程度**：低

### 技术债：真 Lighthouse 仍缺失 — scripts/lighthouse.sh 是替身
- **发现于**：WI-7.9、WI-7.10
- **描述**：真 Lighthouse 需要 headless Chrome + Node + chrome-launcher；本地沙箱没有。写了 `scripts/lighthouse.sh` 用 curl 检查 4 项核心指标（响应头基线、gzip active、HTML ≤ 50KB gzipped、静态资源 Cache-Control）作为替身。核心性能指标（LCP、FCP、CLS）实际上仍未量化测试。
- **建议处理方式**：发布后在真实域名上跑一次 `npx lighthouse https://example.com --output html` 人工归档。项目迁移到有 Node 的 CI 时引入自动化。
- **紧急程度**：中（影响需求 3.1 的 Perf ≥ 90 客观证明）

### 架构洞察：`make release` 不依赖运行中的 server
- **发现于**：WI-7.14 设计时
- **描述**：最初让 release 目标启后台 server + 跑 smoke，但沙箱的后台进程管理不稳定。重构为：release 只做 check + e2e + build + sha256；runtime gates（lighthouse/headers/migrate-test）抽到 `make smoke URL=...`，假定 caller 已手动启动服务。这种"构建/运行时"分离让 CI 流水线和本地发布都干净。
- **建议处理方式**：保留；CI 里 release + start server + smoke 三步串联即可。
- **紧急程度**：低

### 重构机会：systemd unit 里 `MemoryDenyWriteExecute=true` 与 Go 的交互
- **发现于**：WI-7.11 写 systemd unit 时
- **描述**：静态 Go 二进制无 W+X 页，`MemoryDenyWriteExecute=true` 理应不冲突。但未来若切 CGO、引入动态加载库（modernc sqlite 纯 Go 不涉及 CGO，但万一改回用 mattn/go-sqlite3 需要 CGO）会触发 SIGSYS。
- **建议处理方式**：hardening 选项齐全好，但每条都注明"为什么可用"会更防踩雷。当前项目注释足够，未来添加依赖时 review 一遍。
- **紧急程度**：低

### 反思清单
| # | 问题 | 本阶段 |
|---|------|--------|
| 1 | 临时方案 / 妥协 | 有（lighthouse.sh 是资源级替身，不是真 Lighthouse）|
| 2 | "能跑但不够好"的代码 | 有（make release 需要手动拆分 smoke，非一键）|
| 3 | Bug 根因在别处 | 无 |
| 4 | 设计假设在实现时才暴露不成立 | 有（gzip middleware 的 "CT 延迟嗅探"）|
| 5 | 范围外的重构机会 | 有（引入 Node 工具链启真 Lighthouse + CodeMirror 打包）|
| 6 | 新的系统 / 需求理解 | 有（systemd hardening 选项与 Go 二进制的交互、构建/运行时 gate 分离）|

## 2026-04-19

### Bug 修复：新建文档保存后浏览器显示 400 空白页（editorError 双 WriteHeader）
- **发现于**：用户报告（手动测试：点"新建文档"→ 不改直接"保存"）
- **现象**：Firefox 显示 "此网站似乎存在问题 / localhost:8888 sent back an error. 错误代码：400"
- **根因**：`internal/admin/docs.go` 的 `editorError` 先调 `w.WriteHeader(400)` 再调 `render.Render`。Render 内部先 `Header().Set("Content-Type", "text/html")` 再自己 `WriteHeader(status)`——但上一步已经把 response header 刷出去了，Content-Type 没赶上，第二次 WriteHeader 被 Go 忽略。浏览器收到 400 + 无 Content-Type 的响应，叠加 `X-Content-Type-Options: nosniff`，就拒绝渲染 body 里实际存在的 HTML，改显示自己的通用错误页
- **修复**：删除 `editorError` 里那行冗余的 `w.WriteHeader(http.StatusBadRequest)`，交给 Render 统一写头。加注释标记此处易踩
- **回归测试**：`internal/admin/docs_regression_test.go::TestDocsEdit_Bug_DefaultFormSaveShowsEditorNot400` — 断言 400 响应的 `Content-Type` 是 `text/html` 且 body 含 `<form>` 和具体错误消息
- **为什么原测试没覆盖**：既有 `TestDocsEdit_Edge_InvalidFrontmatter` 只断言状态码 `w.Code != 400`——通过。**错误响应的可用性（Content-Type + body 是否真能被浏览器渲染）没有被校验**，属于"异常路径只看 status code 不看响应可用性"这一类盲点。测试用例的断言粒度应该包含"客户端能正确理解响应"，而不仅"服务端返回了指定状态码"
- **紧急程度**：中（用户首次体验即踩，但不丢数据）
- **衍生改进建议**（下次处理，不在本次范围）：`internal/admin/images.go` 的 `ImagesUpload` 有同样模式——先 `w.WriteHeader(413/415)` 再调 `redirectImages` 里的 `http.Redirect`，首次 WriteHeader 会让 `Location` 头发不出去，跳转失效。应按同样方式删除前置 WriteHeader

### Bug 修复：已登记仓库点击删除后返回 "csrf"（403）
- **发现于**：用户报告（管理后台点删除）
- **现象**：点"删除"→ 页面只剩文字 "csrf"，实际返回 HTTP 403
- **根因**：`internal/assets/templates/admin_{docs,repos}_list.html` 的删除表单用 `{{ $.CSRF }}` 取 CSRF 值。Go 模板里 `$` 永远是传给 Execute 的**根值**（此处是 render payload `{Data, Banner, RequestID, Now}`），不是当前 `with` / `range` 的作用域。根上并没有 `CSRF` 字段，`{{ $.CSRF }}` 取出空字符串，渲染出 `<input value="">`。浏览器提交时 csrf 字段为空，服务端 `auth.CSRFValid` 返回 false → `http.Error(w, "csrf", 403)`
- **修复**：在 `{{ with .Data }}` 作用域里先 `{{ $csrf := .CSRF }}` 绑定局部变量，`range` 内改用 `{{ $csrf }}`。两份 list 模板一起修
- **回归测试**：`internal/admin/repos_delete_csrf_regression_test.go::TestReposList_Bug_DeleteFormHasValidCSRF` — 渲染 list 页，正则提取 delete 表单的 hidden csrf 值，断言非空且等于会话 CSRF；再用该值 POST delete，断言 303 redirect 而非 403
- **为什么原测试没覆盖**：CRUD 测试直接传 `b.CSRF`（会话真值）给 handler，**跳过了"模板实际渲染出了什么"这一步**。整个 E2E 链条 "session 里有 csrf → 模板能把它渲染到 form → 浏览器把 form 提交回来 → 服务端校验"，测试只压了首尾两截。**所有通过模板渲染再回传的 hidden / form 字段都应有一次"渲染出什么"的断言**，否则模板作用域/拼写错了不会被捕获
- **紧急程度**：高（删除功能在管理后台完全不可用，且错误信息对用户毫无指导性）
- **衍生改进建议**（下次处理，不在本次范围）：
  - `internal/admin/images.go` 中 `w.WriteHeader(413/415)` 再 `http.Redirect` 的双 WriteHeader 问题（已在上一条 learnings 里提过）
  - 考虑在模板里全站搜一次 `{{ $.` 用法，确认其他 with/range 场景下 $ 含义是否正确
  - 渲染 payload 其实可以把 CSRF 提到 root（render.Render 里统一注入），让模板不需要跨 with 作用域取值——但这是架构改动，单独规划

### 快速功能：联系/媒体加 GitHub/Gitee URL + URL 填写后自动链接化
- **类型**：架构洞察
- **描述**：footer 住在 layout.html，对所有页面共享。把 settings 暴露给 footer 的正确做法是 **把 Settings 加到 render payload 根**，而不是让每个 handler 在 Data 里塞 `"Settings"`（那样有 10+ 个 handler 要改）。最终选择在 `render.Templates` 上加了一个 `SettingsFn func() any` 可选字段，main.go 一处注入。这条模式可以复用到任何"跨所有页面可见"的全局字段（如未来加版权、访客计数）
- **建议处理方式**：保留这个注入点，文档化"需要 layout 级可见的全局数据往 `Templates.SettingsFn` 或同类注入点走"；不再让每个 handler 单独在 Data 里塞
- **紧急程度**：低

- 2026-04-19 快速功能 auto-dismiss-info-banner 完成，无 learnings（已执行反思清单）

## 2026-04-19

### 快速功能：docs-views-hierarchy（目录树 / 标签目录 / 归档时间线）
- **类型**：架构洞察
- **描述**：Go `html/template` 的 `{{ template "name" . }}` 可以把 view-specific 数据原样交给子模板，不必靠 `{{ if/with }}` 塞一堆 view 判断进主 content block。本次 `docs_list.html` 主内容按 `.View` 分叉到 4 个 `define` 子模板（view_list / view_category / view_tag / view_archive），每个子模板各自只关心自己的数据形状，可读性和复用性都好。**以后"多视图同页面"场景复用这个模式**
- **建议处理方式**：文档化一下"主模板分叉到多个 define 子模板"的约定
- **紧急程度**：低

### 快速功能：docs-views-hierarchy — `<details>` 展开动画
- **类型**：技术债
- **描述**：`<details>/<summary>` 原生展开无过渡动画，点击时体验略"硬"。纯 CSS 对 `<details>` 做 max-height transition 不工作（content 高度未知）；流畅做法需要 JS 读测量 scrollHeight 或用 `<dialog>`-like 方案。当前足够可用，但可打磨
- **建议处理方式**：未来如果追求极致交互再补 JS；现在不是瓶颈
- **紧急程度**：低

### 快速功能：docs-views-hierarchy — DocsList handler 长度
- **类型**：重构机会
- **描述**：`DocsList` 已经 80+ 行做了多件事（过滤、计数、分页、现在还加了 view-specific payloads）。继续加视图/过滤规则会越堆越乱
- **建议处理方式**：拆成 `handler()` + `buildDocsListData(ctx, q)`；后者纯函数便于单测覆盖边界条件
- **紧急程度**：低

- 2026-04-19 快速功能 drop-title-periods 完成，无 learnings（已执行反思清单）

## 2026-04-19

### 快速功能：projects-category-filter-ux — 上游 filter HREF 拼接不保留其它 query
- **类型**：技术债
- **描述**：`buildCategoryItems` / `buildStackItems` / `buildStatusItems` 生成 sidebar HREF 时都直接 `"/projects?category="+n` 拼接，**不保留当前请求的其它 filter 参数**。用户如果已有 `?status=active` 再点一个 category，status 就被覆盖丢失。本次 pill 清除链接做对了（基于当前 q 派生），但 sidebar 链接这个老坑没顺手修——在 /bug-fix 规则下不扩大范围
- **建议处理方式**：把 `buildCategoryItems`/`buildStackItems`/`buildStatusItems` 都改成接收 `url.Values`，基于它派生新 URL；同时 pager 也有类似问题（翻页丢 filter），可统一成一个 hrefBuilder 辅助
- **紧急程度**：中（多 filter 组合的用户会觉得"复选"不工作）

### 快速功能：projects-category-filter-ux — URL 派生模式
- **类型**：架构洞察
- **描述**：导航链接（filter tab / sidebar / pill / pager）**应该从当前 url.Values 派生新链接而非字符串拼接**。`cloneValues + Del + Add + q.Encode()` 的模式干净可复用，且天然保留了其它 query 参数。在 `/docs` 页面也有对应的 `buildTagItems` 已经这么做了——`projects` 侧是漏网之鱼
- **建议处理方式**：下次遇到类似 handler，优先用派生模式而非拼接模式；既有代码里有用拼接的地方值得一并翻新
- **紧急程度**：低

### 快速功能：projects-category-filter-ux — 测试缺口
- **类型**：重构机会（测试）
- **描述**：既有 projects_test.go 未覆盖 "sidebar 链接保留其它 filter" 和 "pager 保留 filter" 的行为。本次新加了 pill 的覆盖，但 sidebar/pager 还是盲区
- **建议处理方式**：配合上述 hrefBuilder 统一重构时一起补测
- **紧急程度**：低

- 2026-04-19 快速功能 home-doc-excerpt 完成，无 learnings（已执行反思清单）
- 2026-04-19 快速功能 home-doc-title-bigger 完成，无 learnings（已执行反思清单）

### 快速功能：about-bio-markdown — 默认文案分支与 markdown 路径不一致
- **类型**：技术债
- **描述**：home.html 的 `.about-bio` 容器里，当 `about_bio` 为空时走硬编码的默认 HTML（含 `<strong>` 与 `<span class="muted">`），与"用户填写→走 `markdownUnsafe`"是两条独立渲染路径。想统一默认文案样式（例如加颜色）要改两个地方，容易漏
- **建议处理方式**：把默认文案改为一段 Markdown 常量，同样经过 `markdownUnsafe` 渲染；或把它搬到 settings 默认值里做 seed
- **紧急程度**：低

### 快速功能：about-bio-markdown — Unsafe renderer 的作用域约束
- **类型**：架构洞察
- **描述**：引入了第二份 goldmark 实例（`NewMarkdownUnsafe`，开启 `WithUnsafe`）来让 admin 能在 bio 里写 `<span style="color:...">`。docs 的渲染仍走安全版 `NewMarkdown`，`TestMarkdown_Edge_ScriptEscaped` 照常通过，两套管线互不污染。这条边界要守住：后续任何公开可投稿的入口都不得用 `markdownUnsafe`
- **建议处理方式**：`markdownUnsafe` 的 godoc 已注明 "Never use this on user-submitted content"；后续如果引入评论/访客留言，必须在代码评审中卡住
- **紧急程度**：低

## 2026-04-19

### Bug 修复：暗色模式下"关于我"卡片仍是白底
- **发现于**：用户报告
- **现象**：系统切到深色模式后，主页"关于我"三张 `.about-card` 背景仍是白色，与周围暗色页面反差刺眼
- **根因**：`theme.css:263` 把 `.about-card` 的背景写死 `background: #fff`，而 `@media (prefers-color-scheme: dark)` block（487-524）只覆盖了 `.repo-card/.project-card/.doc-item/.proj`，漏了 `.about-card`。亮色规则硬编码颜色 + 暗色规则漏写覆盖，白色就透了过来
- **修复**：在暗色 block 加 `.about-card { background: #151518; border-color: rgba(255,255,255,0.06); }`，与 `.repo-card/.project-card` 同风格
- **回归测试**：`internal/assets/about_card_dark_mode_test.go:TestTheme_Regression_AboutCardDarkModeOverride`（扫 `@media (prefers-color-scheme: dark)` 块必须包含 `.about-card` 及 `background`）
- **为什么原测试没覆盖**：项目既有的 CSS 测试都是"功能型"（banner 淡出、form-err 不自动消失），没有**覆盖清单**式的暗色规则校验。硬编码颜色只在渲染时可见，Go 测试跑不出来，靠人眼观察 + 视觉回归才能发现，两者都没建
- **紧急程度**：中（不影响功能，但用户观感问题明显）
- **衍生改进建议**：
  1. `.about-card .pill` 在 262-274 块里也写死了 `background: #f0f0f2`，暗色下会是亮灰底 — 本次未改，下次可顺手
  2. 所有卡片类（含 `.contact-table td`、`.featured` 等）都应统一用 `var(--bg-alt)` 或走"亮色 + 暗色覆盖对列表"，避免再漏。可以加一条元测试："凡是选择器里含 `-card` 且在亮色块设了 `background: #xxx`，必须在暗色块有对应 `background` 覆盖"

### Bug 修复：暗色模式下后台管理卡片仍是白底
- **发现于**：用户报告
- **现象**：系统深色模式下，`/manage` 下所有页面（dashboard / docs / repos / settings / images / login / password）的容器卡片仍是白色 `#fff`，整个后台与暗色系统反差刺眼
- **根因**：`theme.css:378 .admin-card` 与 `theme.css:402 .admin-section` 都硬编码 `background: #fff`；暗色 `@media` block 只覆盖了前台卡片家族（`.repo-card/.project-card/.about-card/.doc-item/.proj` 等），整个 `.admin-*` 家族被遗漏。这是与前两次暗色 bug 同一模式的**第三次复发**
- **修复**：在暗色 block 加 `.admin-card, .admin-section { background: #151518; border-color: rgba(255,255,255,0.06); }` + `.admin-table` 行边框色适配 + `.admin-card input:focus` 不再闪回白底
- **回归测试**：`internal/assets/admin_card_dark_mode_test.go:TestTheme_Regression_AdminCardDarkModeOverride`
- **为什么原测试没覆盖**：继前两次（about-card / about-card .pill）后，**"亮色硬编码 + 暗色遗漏" 已经出现第三次**。前一条衍生建议就提了"加元测试：凡 `-card` 选择器在亮色块设 background 必在暗色块覆盖"，但没落地，于是又踩一次。属于**已识别但未建防线**
- **紧急程度**：中（不影响功能，但使用后台的人就是管理员本人，观感体验差）
- **衍生改进建议**：
  1. `.admin-warning` 用了 `#fff8dd/#714d00/#ffe083`，暗色下亮黄会刺眼；可仿 `.draft-banner` 用 `#332b00/#ffd280`
  2. `.admin-table td code` 用 `var(--bg-alt)` 在暗色下等于 `#151518`，与卡片同色导致 code 块消隐；应偏移一档（例如 `#1c1c1e` 或 `#222225`）
  3. **立即应加的元测试**：扫描亮色规则里所有包含 `background: #` 字面量的选择器，若选择器形如 `.xxx-card` / `.xxx-section` / `.admin-*`，断言暗色 @media 块里必须包含对应覆盖。这是彻底断掉"亮色硬编码 + 暗色遗漏"这条模式的唯一方法
- **最重要的反思**：同类 bug 连续三次复发，说明光靠人眼 review 不够，必须把衍生建议转化为自动化断言。下次再遇到 CSS 加硬编码色值时，第一件事是写元测试，而不是先写功能

### 快速功能：404-page — 管理后台未使用品牌化 404
- **类型**：技术债
- **描述**：`cmd/server/routes.go:42,58,81`（admin mux 下的 /manage 变体、docs/projects 子路由 default 分支）与 `internal/admin/*.go` 里大量 `http.NotFound` 调用未替换。本次判断"admin 是技术面、保留纯文本 404 即可"，但管理员误操作或链接过期时弹一个 "404 page not found" 纯字体页观感也差
- **建议处理方式**：给 admin 包加 `h.NotFound` 等价方法，渲染一个简化版、带"返回 /manage"按钮的 admin-404 模板；复用 admin-shell 外壳即可
- **紧急程度**：低

### 快速功能：404-page — 测试能发现 body 泄露
- **类型**：架构洞察
- **描述**：遍历攻击测试 `TestDocDetail_Edge_TraversalSlugRendersBrandedNotFound` 顺带断言 body 不含 `/etc/` 或 `passwd`——这是把安全断言和 UI 断言捏在一起的好做法。早期 `http.NotFound` 会把原路径回显到 body，品牌化后天然没有这个问题，但这条断言能卡住未来如果有人回归去回显 url 的情况
- **建议处理方式**：在其他涉及用户输入 → 错误页的测试里也加"body 不得回显输入"的断言，作为一种 XSS / 信息泄露的轻量防线
- **紧急程度**：低

### Bug 修复：admin 改完站点设置后底部 30s 内看到的是旧值
- **发现于**：用户报告（添加 qq 群号后底部没更新）
- **现象**：在 `/manage/settings` 保存新的 qq 群号（或任意 settings 字段）后，立即刷新主页，底部仍显示旧值；得等约 30s 才更新
- **根因**：`internal/public/public.go:resolveSettings` 有 30s 内存 TTL 缓存（注释 "(per requirement 2.4.5)"），但 `internal/admin/settings.go:SettingsSubmit` 在 `SetMany` 成功后**没有通知公共侧失效**。写路径与读路径靠时间耦合，中间存在最多 30s 的读到旧值窗口
- **修复**：
  1. public 侧新增 `Handlers.InvalidateSettings()` 方法（重置 cachedAt）
  2. admin 侧 `SettingsHandlers` 加可选 `Invalidate func()` 回调，`SettingsSubmit` 在 `SetMany` 成功后调用；验证失败路径不调用（避免无谓清空缓存）
  3. `cmd/server/main.go` 将两侧连起来：`Invalidate: ph.InvalidateSettings`
- **回归测试**：
  - `internal/public/settings_cache_invalidation_test.go:TestSettings_Regression_FooterQQUpdatesAfterInvalidate`（public 层：缓存热 → 改库 → 未 invalidate 仍为旧值 → Invalidate 后为新值）
  - `internal/admin/settings_invalidate_test.go`（admin 层：成功调回调、验证失败不调、nil 回调不 panic）
- **为什么原测试没覆盖**：既有的 `TestSettings_Smoke_Roundtrip` 只测"保存后通过 SettingsPage (admin) 读回"，完全不走 public 侧渲染。写-读耦合路径跨了 admin / public 两个包，单元测试被包边界切开了；又没有 end-to-end 集成测试把两侧串起来，所以这条"跨包状态同步"的缺陷只能靠人眼发现
- **紧急程度**：中（实际是体验 bug，用户会以为保存没成功反复提交；不影响正确性）
- **衍生改进建议**：
  1. public 还有其它缓存（RecentRepos、GitHub cache 等）可能存在类似"写入方不通知读取方"的问题，建议统一梳理一遍数据时效策略
  2. 目前 `Invalidate` 是手写回调，规模再大可以换成更通用的 pub/sub 或观察者；现阶段一个 func() 足够
  3. "跨包状态同步"这一类别值得加到测试清单的固定检查项：凡是一侧写、另一侧读且存在缓存的路径，都应有集成测试覆盖"写完立即读"

### 快速功能：readme-excerpt-card — 内容裁剪后的 Markdown 可能结构不完整
- **类型**：技术债
- **描述**：`internal/github/client.go:GetReadmeExcerpt` 按 rune 数截断 README（再加省略号 `…`）。一旦截断点落在代码块、列表项、链接中间，goldmark 渲染出的 HTML 可能出现未闭合的 `<pre>` / 悬挂的 `[text`，虽然模板用 `.readme-excerpt-box` 做了 `overflow: hidden` 兜底，但视觉上可能出现"半截代码块"这种奇怪态
- **建议处理方式**：截断时尽量在段落边界处停（遇到 `\n\n` 就截），或者改成在服务端先渲染 HTML 再按可视长度裁剪；短期可接受
- **紧急程度**：低

- 2026-04-19 快速功能 readme-excerpt-card 完成，6 项反思其余条目均为"无"

### 快速功能：manage-export-import — SQLite 一致性依赖停服
- **类型**：技术债
- **描述**：`export` 的做法是 `systemctl stop → cp data.sqlite → systemctl start`。如果用户加 `--no-stop`，在 WAL 模式下拷出的 data.sqlite 可能不完整（WAL 变更未 checkpoint）。更稳妥的做法是 `sqlite3 data.sqlite ".backup out.sqlite"`（online backup API），但需要 sqlite3 CLI 且增加一次依赖
- **建议处理方式**：后续加依赖检测，有 sqlite3 时优先用 `.backup`；无则退回 stop/cp。目前的降级是 warn 后继续，可接受
- **紧急程度**：低

### 快速功能：manage-export-import — 测试用环境变量绕开 root/systemctl
- **类型**：架构洞察
- **描述**：给 shell 脚本做单元测试的难点是 `require_root` 和 `systemctl`。本次用了 `MANAGE_SKIP_ROOT` / `MANAGE_SKIP_SYSTEMCTL` 两个环境变量做"测试钩子"，在 go test 里通过 exec.Command 注入环境变量走通。这种 "env-var seams for shell testability" 的模式够轻，值得在以后 shell 功能里沿用
- **建议处理方式**：其它 shell skill（deploy、安装脚本）如果要加测试，可以参照这个模式
- **紧急程度**：低

### 快速功能：manage-export-import — deploy 包只有测试的占位 go 文件
- **类型**：技术债（轻微）
- **描述**：为了让 `go test ./...` 自动涵盖 deploy 目录下的 bash 脚本测试，添加了一个空的 `deploy/deploy.go` 占位 package。这不是运行时代码，纯粹是 Go 工具链的仪式
- **建议处理方式**：接受即可；如果后续真在 deploy 里放运行时 Go 代码，占位文件可以删除
- **紧急程度**：低

- 2026-04-19 快速功能 login-prefill-username 完成，无 learnings（已执行反思清单）

## 2026-04-19 · 日记功能 Stage 1 完成

### 架构洞察：auth.Store.ParseSession 对 User-Agent 指纹绑定
- **发现于**：WI-1.10/11 handlers_test.go 编写过程
- **描述**：第一版测试注入了 cookie 但没设 User-Agent 头，所有 authenticated 请求都被 302 到登录页，初看像 cookie 格式错误。根因：`ParseSession` 会比对 cookie 里的 UA 指纹和请求 UA 指纹，httptest 默认 UA 为空 → fingerprint mismatch。测试 helper 里调 `IssueSession("admin", "test/ua")`，请求里也必须 `req.Header.Set("User-Agent", "test/ua")` 才能匹配
- **建议处理方式**：diary 测试已局部搞定；后续如果其它包做类似集成测试，**应考虑抽一个共用 `newAuthenticatedRequest(cookie, ua)` helper**，避免每个包重新踩坑。或者在 diary 包内提升到 testutil 子包
- **紧急程度**：低

### 技术债：测试请求里重复写 "test/ua"
- **类型**：技术债
- **描述**：`handlers_test.go` 里每个 authenticated 用例都重复 `req.Header.Set("User-Agent", "test/ua")`，共 8 处。能跑但不够好
- **建议处理方式**：抽一个 `newAuthenticatedRequest(method, url, cookie)` helper。Stage 2/3 新加的 API 用例会多，届时顺手重构
- **紧急程度**：低

## 2026-04-19 · 日记功能 Stage 2 完成

### 架构洞察：XHR 端点的未登录响应要 401 JSON，不能 302 HTML
- **发现于**：WI-2.1 APIDay/APISave 设计
- **描述**：与 `Page` handler 不同，API 端点给 fetch 调用；如果未登录回 302，浏览器会跟随跳转到 `/manage/login`，JSON 解析失败，客户端拿不到"未登录"信号。必须返回 401 + JSON body，让前端状态机能识别并引导重新登录
- **建议处理方式**：已落实。后续所有 XHR 端点（/diary/api/* + 后续的 delete/promote）都遵循"HTML 路由 302、API 路由 401 JSON"的约定
- **紧急程度**：低（已实现）

### 技术债：测试 helper setupHandlers 扩成两份
- **发现于**：WI-2.2/2.3 api_test.go 需要 CSRF token
- **描述**：Stage 1 的 `setupHandlers` 只返回 (h, dir, cookie)；Stage 2 的 POST 测试要 CSRF，为了向后兼容又加了 `setupHandlersWithCSRF` 并让前者调用后者。这种"包装一层给老调用方"的做法有点技术债感
- **建议处理方式**：Stage 3 无论如何都要用 CSRF，届时可以把旧 `setupHandlers` 删掉，统一用带 CSRF 的版本
- **紧急程度**：低

### 架构洞察：beforeunload flush 靠 sendBeacon 而不是 fetch
- **发现于**：WI-2.5 diary.js 实现
- **描述**：页面关闭时 fetch 往往会被浏览器取消（特别是长链接）；`navigator.sendBeacon` 是专为此类场景设计的 fire-and-forget 端点，被浏览器保证在卸载过程也能送达。需要传 Blob + 对应的 Content-Type
- **建议处理方式**：已用 sendBeacon。静态扫描测试未覆盖这个（只能靠人工审查），写进 learnings 方便下次
- **紧急程度**：低

## 2026-04-19 · 日记功能 Stage 3 完成（MVP 可发布）

### 架构洞察：公共路由硬断言独立 fixture 而非复用 setup
- **发现于**：WI-3.14 实现过程
- **描述**：初版想复用现有 public_test 里的 setup()，但 setup 不返回 dir，需要绕一圈反射或预留出口，最后决定**独立构造 fixture**（自己的 t.TempDir + cstore + tpl + storage + handlers）。结果测试更独立、意图更清晰、未来 setup 重构也不影响它。对"安全底线断言"这种不该被任何其它用例污染的测试尤其合适
- **建议处理方式**：未来类似"跨包安全断言"类测试都采用独立 fixture 模式，不复用通用 setup
- **紧急程度**：低

### 技术债：escapeYAML 是最小化实现，未覆盖所有边角
- **发现于**：WI-3.5 APIPromote 实现
- **描述**：为避免日记 title / category 含冒号等破坏 docs frontmatter，写了一个极简的 `escapeYAML` 函数（只在含 `:`/#/"`/换行 时加双引号）。但它不处理：1）开头是 YAML 保留字（`true/false/null/~`）；2）纯数字会被 YAML 解析成整数；3）开头 `-`/`?` 等有语义的字符。现阶段单管理员 + 普通标题场景够用
- **建议处理方式**：长期可以改用 gopkg.in/yaml.v3 的 encoder 做正经 marshal；或让用户自己输入时就避免这些边角
- **紧急程度**：低

### 架构洞察：整页刷新比局部 DOM 更新更简单
- **发现于**：WI-3.1 清空日记按钮 + WI-3.5 转正按钮
- **描述**：清空后要更新月视图绿点；转正后要跳转到 docs 编辑页。两处都直接 `window.location.href` / `window.location.reload()`，没做 SPA 式局部刷新。相比维护 DOM 同步，整页刷新代码量 / bug 面都小得多，也契合站点 SSR 优先的架构立场
- **建议处理方式**：短期没必要优化；若日后编辑器要避免刷新损失状态，再引入局部更新
- **紧急程度**：低

### 架构洞察：错误态粘滞 = 状态机要显式读前值
- **发现于**：WI-3.9 保存失败反馈
- **描述**：原实现 input 事件一律 `setStatus('editing')`，覆盖了 error。修复方式是 input 事件前先读 `status.getAttribute('data-state')` 看是不是 error 态，是就跳过。这是"状态机 previous-state 依赖"的典型例子——以后任何需要"粘滞"的状态都要在覆写入口前读旧值
- **建议处理方式**：已落实；若以后状态变多，考虑引入一个小的 state 对象统一管理
- **紧急程度**：低

### 快速功能：promote-direct-redirect — 用 SSR query 预填比 JSON API + client redirect 简单
- **类型**：架构洞察
- **描述**：原 Stage 3 里做的"转正"是客户端 3 个 prompt 采集 title/slug/category → POST /api/promote 写 docs → 跳编辑页；现在改成"点按钮 → GET /manage/docs/new?diary_date=XXX → 后端读日记预填 body"，流程少一半、无 prompt 体验串串、代码少一半。删掉了 APIPromote + escapeYAML + isValidDocSlug + categoryLine（100 行左右）+ 7 条 promote_test.go 用例
- **建议处理方式**：以后遇到"跨区块搬一份数据 + 让用户继续编辑" 类需求，优先考虑 query 参数 + SSR 预填，而不是 JSON API + client 跳转。admin 表单本身已经有完整的 CRUD 校验，再来一层 promote API 是重复
- **紧急程度**：低（已完成）

### 技术债（已还）：Stage 3 的 APIPromote 其实是过度设计
- **类型**：技术债（已解决）
- **描述**：当初按"需求文档 2.5.1 转正弹窗"落地成独立 API，但需求定义里没说一定要 JSON API。用户看到 UX 串 prompt 才提出改进。原来那套有完整的 slug 冲突 / 非法 title / YAML escape 检测，堆积了一堆"因为不走现有表单所以要复刻校验"的代码
- **建议处理方式**：已删光。回溯需求文档 §2.5.1 应该把"通过 /manage/docs/new 路径 + query 预填"作为实现方式在 §7 设计决策里写清，避免下次开发再走弯路 —— 但文档已是 v1.1 已审核状态，暂不回改
- **紧急程度**：低

### 快速功能：diary-week-keyboard-nav — SSR 预填 data-* + 页面重载代替客户端局部更新
- **类型**：架构洞察
- **描述**：给日记周视图加 ← / → 箭头键切周，原本可以在客户端用 DOM 操作（隐藏/显示不同周的行）实现同月切换，跨月才重载。但实际实现里干脆**所有切周都走完整 `location.href = /diary?date=...`**——服务端根据 date 决定月份，模板通过 `data-focus-date` 让 JS 在加载后自动进入周视图。好处：1）代码量减半；2）跨月无特殊分支；3）切周前天然 flush 未保存的 textarea 内容（因为页面要刷新）。代价：每次切周有一次网络往返，但本地 SSR 下 P95 <100ms，体验无感
- **建议处理方式**：固定成模式 —— 日记这类"同一入口不同视图参数"的 SSR 页面优先考虑 query-driven reload，不要为了"平滑过渡"硬上客户端 state machine
- **紧急程度**：低

### Bug 修复：日记周视图下跨月占位日灰掉不可点
- **发现于**：用户报告（26 年 4-5 月交接那周看到 May 1-3 灰色且点不了）
- **现象**：周视图显示的那一行 7 天里，属于上月末 / 下月初的占位格子视觉上置灰（opacity 0.55 + cursor default），点了也没反应
- **根因**：`diary.js:onCellClick` 老代码 `if (!cell || cell.classList.contains('diary-out-of-month')) return;` 把所有跨月格点击早退；CSS 又给 `.diary-out-of-month` 独立灰化样式。两者叠加 → 用户感知"灰色不可点"。月视图下这还合规（经典日历风），但周视图下一个"7 天一周"里夹杂几天不可点违反直觉
- **修复**：
  1. `diary.js:onCellClick` 改成对跨月格走 `location.href = /diary?date=...`，复用 ← / → 箭头切周那条重载路径，服务端决定目标月份并自动进入周视图
  2. CSS 加 `.diary-week-mode .diary-out-of-month` 覆盖，周视图下颜色/光标/hover 阴影全部恢复正常；数字稍浅保留"不是本月"的微弱暗示
- **回归测试**：`internal/diary/cross_month_click_test.go:TestCrossMonthClick_Regression_OutOfMonthCellIsNavigable`
  - 静态扫描 `diary.js` 里不再有 `classList.contains('diary-out-of-month')) return`
  - 必须含 `'/diary?date=' +` 导航入口
  - `theme.css` 必须含 `.diary-week-mode .diary-out-of-month` 覆盖
- **为什么原测试没覆盖**：Stage 1/2 的测试全在 Go 层（server handler + 静态 JS 扫描），没模拟任何"真实 DOM + 真实点击"语义。而这个 bug 是"静态 code 合法 + 动态交互结果违和" —— 纯静态扫描抓不到。需要手动浏览器 smoke 或引入真正的浏览器测试（Playwright 之类）才能未来自动捕获这类
- **紧急程度**：中（影响跨月周的可用性，但有 workaround：先用月翻页按钮切换月份再进周视图）
- **衍生改进建议**：
  1. 考虑在 CI 里加一层基于 Playwright 的 E2E 冒烟（至少覆盖"月视图点格 → 周视图出现 → 切一下日期 → 保存"这条主路径）
  2. `.diary-out-of-month` 在月视图下也让它可点会更好用 —— 目前月视图下点 5-1 占位仍然无反应。本次不扩大范围，留待后续

- 2026-04-19 快速功能 月视图跨月格同风格 完成：把 `.diary-out-of-month` 基础样式改为和本月格一致的可点外观（仅日期数字稍浅提示"非本月"），删除周视图专属覆盖。衍生建议 2 落地。无其他 learnings（已执行反思清单）

## 2026-04-19

### Bug 修复：/manage/login?next=/diary 登录后跳回 /manage 而非 /diary
- **发现于**：用户手动测试
- **现象**：访问 http://127.0.0.1:8391/manage/login?next=/diary，输入账号密码登录成功，浏览器落到 /manage 而不是 /diary
- **根因**：`internal/admin/admin.go` 两处 `strings.HasPrefix(target, "/manage")` 白名单把非 `/manage` 前缀一律拍回 `/manage`。diary 上线前后端整合时，漏了"next 白名单需要同步扩列"这一步。`LoginSubmit` 成功分支 和 `nextFrom`（已登录访问 login 页）两处都有
- **修复**：抽成 `isSafeNext(n)` 帮助函数，统一处理空串 / 协议相对 URL (`//evil.com`) / 带 scheme 的外链 / 白名单前缀 (`/manage`、`/diary`)。`LoginSubmit` 和 `nextFrom` 都改用该函数
- **回归测试**：`internal/admin/login_next_diary_test.go:TestLogin_Regression_NextDiaryRespected`（3 条断言：POST 成功分支 / 已登录 GET 分支 / 外链挡回默认页）
- **为什么原测试没覆盖**：
  1. admin 包的 login 测试只测了"登录成功默认去 /manage"和异常路径（密码错、空字段、限流），没有参数化 `next` 字段的测试
  2. diary feature 的测试在 `internal/diary/`，只测了 /diary 的未登录 302 跳走，没覆盖"登录回跳 /diary"这条跨包往返路径
  3. 跨模块集成点（/diary 未登录 → /manage/login?next=/diary → 登录成功 → /diary）没有单一 handler 拥有端到端责任，两端都各自通过了自己的单测，漏掉了中间的握手
- **紧急程度**：中（影响用户体验但无数据/安全后果；已在白名单内兜底外链）
- **衍生改进建议**：未来再加需要登录的顶层路由时，除了在 isSafeNext 里加前缀，也要在 login 测试里加一条 next= 该路由的 smoke case。或者上一层："登录回跳"本身值得一个专门的 E2E 覆盖矩阵，参数化列出所有需要登录的入口

### 快速功能：文档编辑器 编辑/预览 切换
- **类型**：重构机会 + 架构洞察
- **描述**：
  1. frontmatter 剥离逻辑在本次预览端点里又写了一份 (`stripFrontmatter`)，而 `internal/content/content.go` 已有私有 `splitFrontmatter`、`internal/admin/docs.go` 已有 `extractSlugFromBody`。三处解析大同小异但语义略不同（一个返回 body、一个返回 fm 字节、一个抽 slug），临时共用会把接口拧成复合返回反而更糟。下次若有第四处 frontmatter 消费再抽公共 util
  2. 架构洞察：`render.Templates.Markdown()` 已暴露共享 goldmark 实例，任何需要 server-side 渲染 MD 片段的 handler 直接取用即可，不用自己 new。后续同类需求（例如 settings 页的"介绍"实时预览）也可复用
- **建议处理方式**：记录即可，无需立即动作
- **紧急程度**：低

- 2026-04-19 快速功能 编辑/预览切换 完成：新增 `/manage/docs/preview` (POST, CSRF+auth)，共享公共 /docs 的 goldmark safe 渲染器；模板加 tab 切换 + 小段内联 JS。异常测试 7 条覆盖 CSRF/401/边界/未闭合 frontmatter/XSS/405

### Bug（本次修复内发现）：ParseForm 不解析 multipart body
- **发现于**：编辑/预览切换功能手动测试
- **现象**：点预览按钮返回 403 Forbidden
- **根因**：前端用 `new FormData()` + `fetch` → 浏览器自动设成 `multipart/form-data`。服务端 `r.ParseForm()` 只处理 `application/x-www-form-urlencoded`，读不到 csrf 字段 → CSRF 校验不过
- **修复**：前端改用 `URLSearchParams`，显式 `Content-Type: application/x-www-form-urlencoded`
- **回归测试**：`TestPreview_Regression_MultipartRejected` 钉死契约
- **紧急程度**：低（一次性修复，模式清晰）
- **衍生建议**：若未来要支持图片上传等二进制场景，handler 这边要改用 `r.FormValue()` 或显式 `r.ParseMultipartForm()`；`r.Form.Get` 静默失败是个陷阱

## 2026-04-19

### Bug 修复：markdown 公式 $..$ / $$..$$ 不渲染
- **发现于**：用户手动测试新搭建的博客
- **现象**：文档里写 `$E=mc^2$`、`$$x_i=y_j$$` 保存后，公开 /docs/:slug 页只显示字面 `$E=mc^2$`，没有数学符号渲染
- **根因**：不是服务端解析 bug。goldmark 默认**透传**了 `$...$` 和 `$$...$$`（没把下划线吃成 emphasis），但项目本身就没有客户端数学渲染器。换言之这是**缺一个前端渲染层**，不是"解析出错"
- **修复**：embed KaTeX 0.16.11（JS 275K + CSS 23K + auto-render 3.5K + 20 个 woff2 字体 ~300K，合计 ~604K）到 `internal/assets/static/math/`。在 `doc_detail.html` 和 `admin_doc_edit.html` 挂 CSS/JS，新增 `math-init.js` 用 auto-render 扫描 `$..$` / `$$..$$` / `\(..\)` / `\[..\]` 四种分隔符，throwOnError:false 让错误公式不整页崩。admin 编辑器 Tab 切到预览时 fetch 注入 HTML 后重新调 `window.renderKatexIn(preview)` 对新节点再渲染一次
- **回归测试**：
  - `internal/public/doc_math_render_test.go:TestDocDetail_Regression_KatexAssetsEmbedded`：断言 /docs/:slug 页 HTML 含 katex.min.css/js/auto-render.min.js/math-init.js 四个资源引用，且 `$...$` 原样保留（证明 goldmark 没吃）
  - `internal/admin/doc_preview_test.go:TestEditor_Regression_MathAssetsEmbedded`：断言 /manage/docs/new 编辑页 HTML 含同四件套 + doc_edit.js
- **为什么原测试没覆盖**：
  1. 项目过往没有"文档内含公式"这种输入用例；所有 doc 测试正文都用的普通中文 + markdown，从没 $...$
  2. 渲染层的"功能性"只被间接测过（页面能返回 200、含 article 标签），没断言过任何可选渲染器资源（代码高亮 CSS、数学公式 JS 等）。这是一类"前端功能资源 embed"的系统性覆盖缺口
  3. 根因是"缺前端渲染器"而不是"代码渲染错"——这种"缺"型 bug 最容易逃过 server-side 测试，必须靠"资源引用断言"兜底
- **紧急程度**：中（个人博客写带公式的技术笔记属核心用例）
- **衍生改进建议**：
  1. 后续凡是依赖 client-side JS 做渲染的功能（code 高亮自定义、图表、diagram、音视频嵌入等），都应该有一个"资源引用断言"型的 regression test，即便内容本身要浏览器才能看见
  2. 考虑加 CSP font-src 的检查测试：现在 CSP 是 font-src 'self'，embed 的 KaTeX woff2 走 /static/math/fonts/...  满足 self 同源，OK；但未来若有人引入 CDN 字体会静默被拒绝
  3. 当前 init 只扫 `.doc-body, .diary-preview, .editor-preview` 三种容器。如果未来在 /projects/:slug 或 README 摘要等地方也要支持公式，需扩展选择器

### 快速功能：文档/项目删除确认改走外部 JS
- **类型**：架构洞察 + Bug + 测试缺口
- **描述**：修这个"小功能"时发现它其实是个潜伏 bug——两份 admin list 模板里一直都有 `onsubmit="return confirm(...)"`，但 CSP `script-src 'self'` 会**静默拦截所有 inline 事件处理器**（不止 `<script>` 标签），过去这个确认框从来没真正弹出过，用户误点就软删
- **根因模式**：CSP self 模式下 `onclick=` / `onsubmit=` / `onchange=` 一类 HTML 属性全是 noop。项目里凡是靠 inline handler 做的交互都可能是哑的。已知位点两处（文档/项目 delete），但没有 lint 或测试规则扫其他模板
- **修复**：新增 `internal/assets/static/js/confirm_submit.js` 监听 `[data-confirm]` 表单 submit；两份列表模板把 `onsubmit="return confirm('X')"` 改成 `data-confirm="X"`，末尾引入外部 JS
- **建议处理方式**：补一个 template 扫描型测试（`go test` 遍历所有 `internal/assets/templates/*.html`，grep 到 `\bon[a-z]+\s*=` 就 fail），一次性杜绝同类问题；或把 CSP 改成 nonce 模式（文档写的是"refined to nonce-based in a later phase"——就是它该做了）
- **紧急程度**：高（本次修复后解除，但同类其他 inline handler 若存在则一直是哑的）
- **衍生改进建议**：
  1. **模板内联事件 lint**：加一条 grep 测试扫所有模板文件，命中任何 `on\w+=` 属性就失败，绝杀同类问题
  2. **CSP 升级到 nonce**：现在 middleware 注释说"refined to nonce-based in a later phase"，就是这个阶段了。nonce 模式下可以重新放开内联 JS，不用再一次次地把小交互外置成独立 .js 文件

## 2026-04-20

### 快速功能：回收站管理页 /manage/trash
- **类型**：技术债 + 架构洞察
- **描述（分两部分）**：
  1. **性能/规模没做分页**。`TrashHandlers.scan` 一次 `os.ReadDir` 全部条目再排序，当前个人博客场景够用；但 trash 是**永不自动清理**的（见刚才给用户的答复，无任何 retention 策略），日积月累可能堆到成千上万个文件，一次加载全列表会变卡。更稳妥的做法是要么：
     - 引入简单分页（按 trashed-at 滑动窗口）；或者
     - 配合**定时清理任务**（比如 30 天自动 purge）让 trash 自然收敛
     当前两者都没做
  2. **并发 restore 的竞态**：两个同时提交的 restore 请求针对同一 filename，`os.Stat` 检查目标不存在后再 `os.Rename`，中间是 TOCTOU 窗口；真正高并发场景会同时通过检查、第二个 rename 可能覆盖第一个。本地管理后台单用户几乎触发不了，但不加锁是一点技术债
- **建议处理方式**：
  - 若要上规模：把 scan 换成"按时间倒序最多取 100 条，后面走分页"
  - 加 retention 任务（TTL 可配置，默认 30 天）做自动清理
  - restore 路径换成 `os.Rename + 条件错误判断（linkat/EEXIST）`的原子语义，去掉 stat-then-rename 窗口
- **紧急程度**：低（当前规模/使用模式都不触发）

### 快速功能：标签视图选中标签后下方展示命中文档
- **类型**：Bug + 架构洞察
- **描述**：
  1. **潜在 pager bug（pre-existing）**：`view_list` 的分页链接用的是 `?page={{ .N }}`，相对 URL 的 `?` 会**替换整个 query string**，不是合并。所以用户在 `/docs?view=tag&tag=foo` 翻到第二页，点"下一页"后 URL 变成 `/docs?page=2`，view 和 tag 参数全丢。过去只在 `view=all` 下被翻页，这个 bug 没人触发；这次加了 tag 视图下的筛选列表，如果筛选结果多于一页，会立刻暴露。本次不修（/quick-feature 保守范围），但**强烈建议**下一个 quick-feature 就修
  2. **架构洞察**：`DocsList` handler 实际上一直在服务端算 filtered docs，可所有 view=tag|category|archive 的 template 分支都只渲染自家结构（目录树 / tag 卡片 / 归档树），没用这份 filtered 数据。用户看到的是"点了过滤结果没变"的错觉。即 **数据在 data 里但 template 没展示**，这是个容易踩的坑：未来扩展 view 时要警觉
- **建议处理方式**：
  - Pager 链接改成保留全部现有 query + 覆盖/追加 `page` 参数。最简做法是 handler 把完整 href 字符串（`/docs?view=tag&tag=foo&page=2`）预算好塞进 `pager`，template 直接 `href="{{ .Pager.PrevHref }}"`
  - 做个小扫描：其他 view 是不是也有"数据到了但没展示"的潜在 UX 坑（category/archive 似乎各自只渲染树结构，和 tag 同模式，都该考虑选中后要不要展示命中列表）
- **紧急程度**：中（分页 bug 一旦筛选结果 >10 篇立刻复现）

### 快速功能：首页从 footer 往回滑吸附到 page-2 底部
- **类型**：架构洞察 + 测试缺口
- **描述**：
  1. **CSS scroll-snap 非对称方向吸附技巧**：`.page` 和 `.footer` 原有 `scroll-snap-align: start`，向下滚时每段顶部对齐；但从 footer 往回滚时，下一个上方 snap 点是 page-2 的 START（page-2 顶部），用户看不到刚才滑离的 page-2 底部、要再往下才能回到原位置。解决办法是塞一个 0 高 sentinel `<div>` 在 page-2 和 footer 之间，用 `scroll-snap-align: end` 把 page-2 的 BOTTOM 也注册成吸附点；`scroll-snap-stop: normal` 保证向下滚时不被它强制挡停。这是做"非对称吸附方向"的标准套路，以后类似需求直接照搬
  2. **系统性测试缺口**：整个项目没有任何 headless browser e2e（Playwright / Cypress / Chromedp）。所有纯 UI 交互（本次的 scroll-snap、前面 KaTeX 的公式渲染、CodeMirror 编辑器预期接入等）都只能"人眼 + 重启服务手测"。server-side 测试最多能到"元素/CSS 规则存在"这一层，没法验证运行时滚动/交互/渲染是否真 work。已有两次累积（KaTeX、scroll-snap）提示这是项目级测试资产缺口
- **建议处理方式**：
  - scroll-snap 本次不扩大范围，CSS 方案够用
  - 长期看应评估：加 Playwright e2e（Go 生态可用 `chromedp` 或 `rod`），专跑"浏览器执行后才能验证的行为"。暂列高优先级 **衍生任务**
- **紧急程度**：中（scroll-snap 本身够用；测试缺口已累积两次，再不处理还会继续踩）

### 快速功能：diary 页窄屏适配
- **类型**：架构洞察
- **描述**：diary 页之前**一个 @media 都没有**，所有 shell padding / cell 高度 / editor padding 默认值一路生效到 0px 视口宽。补了两级断点（900px 桌面窄窗 + 640px 手机竖屏）后问题解决。巡查其他公开页面也有可能有同类现象 —— home / docs / projects 各自的 `@media (max-width: 860px)` 是在 layout 级加的但覆盖面不全，比如文档详情页的 `.doc-body` 以及 `/projects/:slug` 详情大概率也有窄屏"直接压缩"的毛病，本次范围不扩大
- **建议处理方式**：列入待办，下一轮专门做一次"窄屏适配 审视 + 批量补"，把 home/docs/projects/manage 几个详情页都过一遍。断点建议统一用这次的 900/640 两级节奏
- **紧急程度**：中（diary 修了后可用，其他页用户会陆续报）


## 2026-04-20

### 快速功能：home/docs/projects 窄屏适配
- **类型**：架构洞察
- **描述**：延续了 diary 页窄屏适配的 TODO。原 860px 断点的处理是**把 `.nav-links` 直接 `display:none`**（连入口都没了，不是折叠进汉堡菜单），以及**把 `body.page-docs .shell` 的 220px 侧栏塌成 1fr 单列**。本次改为：860px 保持两栏但侧栏收窄到 170px、nav links 缩小 + wrap；新增 560px 断点让侧栏进一步收到 124px 仍保持在侧。用户明确要求手机上侧栏也要"在侧面"，和常见的汉堡菜单做法不同，算项目的特殊偏好
- **建议处理方式**：manage 后台页 (`/manage/*`) 和两个详情页 (`doc_detail` / `project_detail`) 还没做窄屏适配，属于本次范围外的遗留；下次再扫一遍
- **紧急程度**：中

### 快速功能：home/docs/projects 窄屏适配 — 测试缺口
- **类型**：测试/文档缺口
- **描述**：项目完全没有前端视觉回归测试，所有 CSS 改动都依赖人工在浏览器里拖窗口检查。本次改了 3 个断点覆盖 4 个页面类型（home/docs_list/projects_list + 可能还有 doc_detail），没有自动化手段验证"nav links 确实可见"、"sidebar 确实还在侧"这种断言
- **建议处理方式**：如果后续再有多轮窄屏调整，可以考虑加一个 Playwright 最小集，跑几个固定宽度下的截图 diff（375/560/860/1280）。不紧急，但值得列入 backlog
- **紧急程度**：低


## 2026-04-20（续）

### 快速功能：admin 窄屏适配
- **类型**：技术债
- **描述**：为了让手机也能横向滚动查看 `.admin-table`（文档列表/回收站等多列表格），偷懒在 560px 断点给整个 `.admin-section` 加了 `overflow-x: auto`。这个 section 里同时装着表格、表单、小组件，给整个容器加横滚会让非表格内容也可能意外产生滚动条，不干净。更规整的做法是在 admin_docs_list/admin_trash/admin_repos_list 这几个模板里，给 `<table class="admin-table">` 外套一层 `<div class="admin-table-wrap">`，单独给 wrap 加 overflow-x:auto
- **建议处理方式**：下次动 admin 模板时顺手补 wrap div，然后把 CSS 里 `.admin-section { overflow-x: auto }` 换成 `.admin-table-wrap { overflow-x: auto }`
- **紧急程度**：低

- 2026-04-20 快速功能 new-doc-excerpt-field 完成，无 learnings（已执行反思清单）


## 2026-04-20（续 2）

### Bug 修复：文档详情页的上/下一篇混入 draft/archived
- **发现于**：用户报告（"文档中上下切换会出现草稿状态的文章，还有上下切换会乱序"）
- **现象**：在 `/docs/:slug` 点上一篇/下一篇时，会跳到未发布的 draft 或 archived 条目；更常见的表现是"跳过了本该相邻的一篇"——因为 draft 按 Updated desc 排序时插队到 published 之间，公开列表看不到它，用户感觉"顺序乱了"
- **根因**：`internal/public/doc_detail.go:40` 直接把 `h.Content.Docs().List(KindDoc)`（全量，含 draft/archived）传给 `prevNext`。而 `/docs` 列表、archive 视图、category 视图都只展示 published，两边数据集不一致
- **修复**：调 `prevNext` 前先 `filterByStatus(..., StatusPublished)`。一行改动。当前文档若是 draft/archived（管理员预览路径），过滤后列表里没它，prev/next 自然都是 nil，作为降级行为接受
- **回归测试**：`internal/public/public_test.go::TestDocDetail_Bug_PrevNextSkipsNonPublished`——在 3 篇 published 之间插 1 篇 draft + 1 篇 archived，断言 b 的 prev/next 都指 published，且 HTML 里不出现 draft-x / arch-y 的 href
- **为什么原测试没覆盖**：既有的 `TestDocDetail_Smoke_PrevNextNavigation` 只放了 3 篇都 published 的数据，没有测"异种状态混合"这一条清单项。属于"异常/边界测试场景清单不完整"——只覆盖了 happy path，边界值（状态混合）漏了。以后写 smoke 时应主动想：列表型功能是否存在可见性过滤？过滤跟导航是不是用的同一个数据源？
- **紧急程度**：中（公开站直接可见，影响用户信任；但不会崩溃）
- **衍生改进建议**：顺手扫了一眼同模块其它地方：`feeds.go:53` RSS 和 `feeds.go:121` sitemap 的 doc 循环各自内联过滤了 published（不是共享 helper，重复写了三遍）；但 `feeds.go:130` sitemap 的 project 循环**没有**按状态过滤，`public.go:223` 首页推荐位也值得再确认一遍。下次可以把 `filterByStatus` 抽出来让三处复用，顺带补 project sitemap 这个口子


## 2026-04-21

### 快速功能：关于我页面 (/about)
- **类型**：重构机会
- **描述**：About 的 status 访问控制（draft 仅登录可见、archived/published 公开、其它 404）跟 DocDetail 的 `switch e.Status { ... }` 几乎一模一样，这次直接在 about.go 里手写了一份。还没抽出来的原因是重复只有 2 处，硬抽 helper 边际收益低；如果将来再多一个入口（比如 /cv、/resume）就值得提一个 `canView(status, loggedIn) bool`
- **建议处理方式**：等第 3 个复用点出现时一起抽
- **紧急程度**：低

### 快速功能：关于我页面 — About doc 在 /docs 列表/RSS/sitemap 里同时出现
- **类型**：架构洞察
- **描述**：/about 复用 content store 的 doc（slug=about）作为数据源，但这篇 doc 仍会被 `/docs` 列表、RSS feed、sitemap 当作普通博客文章展示 —— about 会出现在文档目录里，也会作为 prev/next 邻居出现在别的博客文章底部。用户报的"上下篇按钮" bug 那次我把 draft/archived 过滤了，但 about 是 published 状态的正常 doc，过滤不到它。这属于"一个 slug 同时承担两种语义"的设计债
- **建议处理方式**：两个候选。(a) 用户自律：把 about 设为 status=archived，就不会进 /docs 列表/RSS/sitemap，但归档横幅也跟着出现在 /about，体验差。(b) 代码里特判：在 docs list / RSS / sitemap / prev-next 里显式过滤 `slug != "about"`，但多处特判是技术债开始。(c) 换个存储位置：让 about 不用 content/docs 而是存在比如 content/pages/ 里，彻底分离。长远看 (c) 最干净，需要 content store 支持 page kind
- **紧急程度**：中（功能可用，但有副作用需要用户知道；优先级取决于用户是否真的把 about 写成很不像博客的内容）


## 2026-04-21（续）

### 快速功能：/about 从 content/docs 迁到 settings
- **类型**：架构洞察
- **描述**：原来想用 content/docs 存 about（slug=about），一次实现就报告过两个副作用（它会漏进 /docs 列表/RSS/sitemap/prev-next）。用户明确"单独管理，不跟其他文档混"后，这次直接换成 `about_detail` 写到 `site_settings` KV。重构范围：admin/settings.go 加 key；admin_settings.html 加 textarea（rows=16）；public.AboutData 加 `Detail` 字段 + 解析；public/about.go 改从 `h.about().Detail` 读；about.html 去掉 doc 相关的条件（draft/archived banner）；测试全量重写（6 个用例覆盖：渲染 / 空 404 / 仅空白 404 / 原始 HTML 不破页 / 导航链接 / 确认 content/docs/about.md 不再被读取）
- **新理解**：以后写"定位是 site-meta 的独立页面"（关于我 / 简历 / 订阅须知 / 友链），默认走 settings KV + 专属 handler；只有"博客/文章语义，需要标签/状态/归档/阅读数"的内容才用 content/docs。判断标准是：这篇内容应该出现在 /docs 列表里吗？如果不应该，就不该用 content store
- **建议处理方式**：把"site-meta vs content 选型"这条默认规则补到 ARCHITECTURE.md 或 PURPOSE.md 的某一节；下次再加 /cv /links 这类页面时避免重蹈覆辙
- **紧急程度**：低


## 2026-04-21（续 2）

### 快速功能：/about 再次迁移 — settings → content/about.md 裸文件 + 独立管理页
- **类型**：架构洞察
- **描述**：前一轮（续 1）把 about 放进 `site_settings.about_detail` KV，用户否决了该方案，要求"内容"维度管理且有编辑/预览功能。最终落地：文件 `{DataDir}/content/about.md`（裸 Markdown，无 frontmatter，不在 content/docs 子目录），admin 端加 `/manage/about` 独立页（AboutHandlers + admin_about_edit.html，复用 `.editor-tabs` + doc_edit.js + `/manage/docs/preview` 预览端点）；public 端 /about 直接 `os.ReadFile` 该文件，空或缺失 → 404。atomic 写用 temp-file + rename
- **新理解**：项目内的"独立页面"有三种落脚点，各有适用场景：
  1. **site_settings KV**（`about_bio` / `tagline` 等）：短字段、站点元数据、通常只读，不需要富文本编辑器。用 admin_settings.html 单表单多字段
  2. **content/docs markdown**（带 frontmatter）：有标签/状态/归档/SEO/列表展示需求 → 走博客文档流水线
  3. **content/ 根下的单文件 markdown + 独立 admin 页**（这次）：需要长文本 + 编辑预览 + 不想进博客列表/feed → 一次性的 site page。`/about` 是第一个，以后 `/cv`、`/subscribe`、`/links` 若也这么做，可考虑抽 `PageHandlers` + 共享编辑模板
- **建议处理方式**：出现第 2 个同类页面时再抽象。`admin_doc_edit.html` 和 `admin_about_edit.html` 的 editor 区块重复度 70%，如果后续还要加 /cv 之类，值得抽一个 template block（Go template 虽无宏但可用 `{{ template "editor-shell" . }}` 组合）
- **紧急程度**：低

### 快速功能：/about — 回补测试清单
- **类型**：测试/文档缺口
- **描述**：本次异常测试比较完整（非法输入-空 path / 边界值-空白/空文件 / 权限-无 cookie 401 / 权限-CSRF 错 403 / 异常恢复-原子写 overwrite 不留 .tmp / 边界值-清空 body 允许），但有一条漏网的条件没测：预览端点 `/manage/docs/preview` 在 about 场景下被复用，约定了它对"无 frontmatter 的纯 markdown 正文"也能正确渲染。虽然 stripFrontmatter 的实现看起来对无 frontmatter 返回原文本，但没有专门针对"about 预览"路径的集成断言
- **建议处理方式**：下次动 Preview 或 about 任一侧时，补一个"POST /manage/docs/preview body=纯 markdown → 返回 HTML 片段含 `<strong>`"的用例，作为两侧契约的锚点
- **紧急程度**：低


## 2026-04-21（续 3）

### 快速功能：/about 加默认文案
- **类型**：架构洞察
- **描述**：初稿的 /about 在文件空/缺失时直接 404，新部署站点用户体验差。改成：把默认 Markdown 嵌在 `internal/assets/defaults/about.md`（embed 进二进制），两侧都能通过 `assets.DefaultAbout()` 拿到。前台 handler 在文件缺失 OR trim 后为空时回退到默认；admin 编辑器首次访问（文件不存在）预填默认让管理员"在模板上改"而非从空白开始
- **新理解**："文件缺失"和"文件存在但空"是两个不同的语义状态，两端应该**分别处理**：
  - **前台**统一回退到默认（给终端用户最友好的视觉）
  - **后台**必须区分（否则管理员"显式清空"会被默认覆盖，永远清不掉）
  - 落实到代码：前台 `trimmed == ""` 即回退；后台只在 `os.ReadFile` err（文件不存在）时回退，文件存在即使内容空也原样显示。这种"前后台对同一个数据的可见性故意不一致"以前没写过，但逻辑上是对的
- **建议处理方式**：下次再遇到"嵌入默认内容 + 用户可覆盖"的模式，按这个骨架抄即可。嵌默认用 `//go:embed`，覆盖用 DataDir 下的同名文件
- **紧急程度**：低


## 2026-04-21（续 4）

### 快速功能：主页 hero 头像
- **类型**：技术债
- **描述**：`resolveSettings` 对 `avatar_url` 仅用 `v != ""` 判空，不 TrimSpace；admin `SettingsSubmit` 在 POST 时对所有字段统一 TrimSpace 后落库，所以正常路径不会存"   "。但如果有人直接改 DB、或者将来绕过 admin 写入，前台会输出 `<img src="   ">` 这种空盒子。resolveSettings 里 7 个 `if v != ""` 是同款写法，都没有 TrimSpace
- **建议处理方式**：下次整理 settings KV 时把 resolveSettings 里的判空改成 `if v := strings.TrimSpace(kv[...]); v != ""`，统一一份；当前留 `TestHome_Edge_AvatarWhitespaceNotShownAsEmptyBox` 作为未来回归锚点——改完后 flip 断言即可
- **紧急程度**：低


## 2026-04-21（续 5）

### 快速功能：头像点击上传
- **类型**：重构机会
- **描述**：`internal/admin/avatar.go` 的 MIME 嗅探 + SVG 特判段（约 25 行：`DetectContentType`、`text/xml` / `text/plain` + `.svg` 后缀兜底、`allowedImageMIMEs` 查表、sniff 后 `file.Seek(0)`）是从 `images.go` 直接复制的。当前两个入口（`/manage/images/upload` + `/manage/avatar/upload`）相似度高但各自独立；将来再加一个（比如 favicon / OG cover 上传），就值得抽 `validateAndNormalizeImage(header, file) (ext string, err error)` 这种 helper。现在 2 处还能接受
- **建议处理方式**：等第 3 个入口出现时抽；现有两处留为"重复代码观察样本"
- **紧急程度**：低

### 快速功能：头像点击上传 — 测试缺口
- **类型**：测试/文档缺口
- **描述**：avatar.go 里有个"文件已落盘但 settings.Set 失败"的降级路径（不 remove 文件，前端还能拿到 url 手动保存），测试没覆盖 —— 因为要注入会返回 error 的 settings.Store mock，而当前 admin 测试都用真 storage，改动比较大。属于"防御性分支但不敏感"的测试债
- **建议处理方式**：下次 settings 层若要改接口，顺手加个 `type SettingsWriter interface { Set(k, v string) error }`，让 AvatarHandlers 依赖接口而非具体类型，测试时注 mock
- **紧急程度**：低
