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
