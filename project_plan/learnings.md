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
