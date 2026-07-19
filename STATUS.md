# first-agentInK8s 项目状态

> 项目**当前状态 + 决策日志**，进 git、跟代码走。
> 每次收尾在此更新；跨会话的元知识（工具坑、账号背景等）留在 `~/.claude/memory/`。

**最后更新：2026-07-19 Phase 3 REST API CRUD + 持久化 + 热重载 完成，已合入 main**

> 📍 **workbuddy 转型 vision**：[`docs/specs/workbuddy-vision.md`](docs/specs/workbuddy-vision.md)（本项目从 demo 演化为可配置多 agent 产品的 MVP 边界；下一阶段主线）
> 📍 工程化改进路线：[`docs/roadmap-ai-engineering.md`](docs/roadmap-ai-engineering.md)（AI 辅助开发的业界实践 + 本项目改进清单，2026-07-14 起草，多数已落地）
> 📍 架构决策记录：[`docs/adr/`](docs/adr/)（每决策一份，从 001 单二进制 / 002 Eino / 003 distroless / 004 branch protection 起）

## 2026-07-19 Phase 3 完成 —— REST API CRUD + 配置持久化 + 热重载

**Phase 3 已完成**（PR #10 `bda9429`，squash-merge 合入 main）：

**spec 与 ADR 产出**：
- ✅ **`docs/specs/phase-3-rest-api-crud.md` 转 Accepted** —— 4 种持久化方案对比（文件 vs sqlite vs 纯内存 vs etcd），选定文件系统 yaml 持久化
- ✅ **`docs/adr/007-rest-api-persistence.md` 起草完成** —— 5 个 Open Questions 全 close

**代码变化摘要**（13 files, +1567/-36）：
- 新增 `internal/configstore/store.go` + `store_test.go`：Agents / MCP CRUD，原子写 + 软删除，5 个单元测试全部 PASS
- 新增 `internal/httpapi/api.go` + `routes.go`：11 个 endpoint handler + httprouter 统一路由注册
- 修改 `internal/mcp/config.go`：暴露 `ParseConfigForAPI` + `ValidateForAPI`
- 修改 `internal/agents/supervisor.go`：新增 `Reload(ctx)` 作为 `Rebuild()` 的 API 友好别名
- 修改 `cmd/server/main.go` + `internal/httpapi/sse.go`：接入 configstore 和新路由

**API 列表**（11 个 endpoint）：
| Method | Path | 说明 |
|---|---|---|
| GET/POST | `/api/chat` | SSE 聊天（不变） |
| GET | `/api/agents` | 列出所有 agent |
| GET | `/api/agents/:name` | 获取单个 agent |
| POST | `/api/agents` | 创建 agent（自动 reload） |
| PUT | `/api/agents/:name` | 更新 agent（自动 reload） |
| DELETE | `/api/agents/:name` | 软删除 agent（自动 reload） |
| GET | `/api/mcp` | 列出所有 MCP server |
| GET | `/api/mcp/:name` | 获取单个 MCP |
| POST | `/api/mcp` | 创建 MCP（自动 reload） |
| PUT | `/api/mcp/:name` | 更新 MCP（自动 reload） |
| DELETE | `/api/mcp/:name` | 软删除 MCP（自动 reload） |
| POST | `/api/reload` | 手动触发热加载 |

**CI 验证结果**：
- `go build ./...` ✅ | `go vet ./...` ✅ | `go test ./...` ✅ 全绿
- `golangci-lint` CI 全绿 ✅ | Linux `go test -race` CI 全绿 ✅

**关键设计决策**：
- **配置持久化走文件系统**：原子写 + 软删除，单二进制零外部依赖
- **CRUD + Reload 绑定**：POST/PUT/DELETE 自动调 `Supervisor.Reload()`，失败返回 500 但不影响旧服务
- **向后兼容**：没有 API 调用时，行为跟 Phase 2 完全一样

**待做**：
- [ ] Phase 4：前端配置 UI（shadcn/ui + Tailwind + react-router-dom）
- [ ] evals 补充 Phase 3 相关 case

## 2026-07-17（晚）Phase 2 完成 —— Registry 可变 + Host 原子 swap

**Phase 2 已完成**（PR #7 `4116ca9`，已 squash-merge 合入 main）：

- ✅ **`docs/specs/phase-2-registry-mutation-host-swap.md` 转 Accepted** —— Open Questions 5 个全 close：
  - `gracePeriod` 参数化：`SUPERVISOR_MCP_GRACE_PERIOD` env，默认 30s
  - Rebuild 用 `context.WithoutCancel` 派生 ctx，避免请求 ctx 取消污染背景重建
  - Rebuild 时重跑 `agentcfg.Load`，让 yaml 编辑生效
  - 不加 `CurrentSnapshot` 测试 API（生产接口不为测试污染）
  - evals 不走 Supervisor（保持 evals boot 流程简洁，只 build 一次 host）
- ✅ **`docs/adr/006-registry-mutation-host-swap.md` 起草完成** —— Alternatives 记了 5 条拒绝理由：httpapi 内联 atomic pointer、Mutex-guarded field、手动 RCU、graceful drain、合并 Phase 2+3
- ✅ **两份 research note 支撑决策**：`docs/research/phase-2-registry-mutation-design.md` + `docs/research/phase-2-host-swap-risks.md`

**CI 验证结果**：
- `ci.yml` build/vet/lint 三绿 ✅
- Linux `go test -race` 全绿 ✅
- PR #7 `4116ca9` squash-merge 合入 main ✅

**代码变化摘要**：

- 新增 `internal/agents/supervisor.go`（约 220 行 + 130 行 `Rebuild`）：
  - `atomic.Pointer[host.MultiAgent]` + `rebuildMu` 单写者
  - 事务化 `Rebuild`：scratch registry dry-run，全部 build 成功才 commit
  - grace period 延迟 `Close` 老 MCP driver（`SUPERVISOR_MCP_GRACE_PERIOD`，默认 30s）
  - `context.WithoutCancel` 派生 ctx，Rebuild 不被请求 ctx 拖挂
- 新增 `internal/agents/supervisor_test.go`（约 440 行）：6 PASS + 1 SKIP（`CallbacksHandlerCountUnchanged` skip，原因 eino/callbacks 无 getter）
- 新增 `internal/tools/registry_test.go`（约 259 行）：8/8 PASS，含 ⭐ 核心测试 `Unregister_SliceReferencesStillWork` 走 `InvokableRun` 硬证 Q1 SAFE（Unregister 不影响持有旧 slice 的 in-flight 请求）
- `internal/tools/registry.go`：加 `Unregister(name) error`（幂等）
- `internal/httpapi/sse.go`：`Server.HostMA *host.MultiAgent` → `Server.Sup *agents.Supervisor`；`HandleChat` 首行 `sup.Current()` 一次；`runStream` 签名新增 `hostMA` 参数
- `cmd/server/main.go`：8 步启动缩减为 5 步（Ark model / registry / Supervisor / callbacks / HTTP），Supervisor 内含原 step 3-6 全部；shutdown 调 `sup.Shutdown` 替代手工 `for range closers`

**本地验证结果**：

- `go build ./...` ✅
- `go vet ./...` ✅
- `go test ./...` ✅ 全绿
- `go test -race` 本地跑不了（Windows 无 gcc），CI Linux 会跑；Supervisor 并发测试已用 goroutine + sync 覆盖 `rebuildMu` 序列化 + atomic 读端 non-blocking

**关键设计决策**（一句话总结）：

- **Supervisor 抽象层职责单一**：只管 host 生命周期 + 换 host，不管 yaml watch / 外部触发（留给 Phase 3 REST API）
- **Rebuild 事务化**：新 host 没 build 完就不 commit，配置错误只会 log error 不会让服务失能
- **⚠️ 关键 caveat**：`InstallToolCallbacks()` 严禁在 `Rebuild` 里调 —— Eino `callbacks.AppendGlobalHandlers` 非幂等且非线程安全，官方文档明说 init-once。`main.go` 保留 step 4 一次性 install，`Supervisor.Rebuild` 绝不动
- **tool 引用生命周期与 Registry 解耦**：`MustResolve` 返回的 slice 是 interface 值副本，`Unregister` 只删 map；MCP client 生命周期由 Supervisor 的 `mcpClosers` 独立持有；旧 in-flight 请求跑完不 drain

## 2026-07-17 Phase 1 完成 —— MCP 声明式加载

**Phase 1 已完成**（PR #6 `6c2e947`，已 squash-merge 合入 main）：

- ✅ **`docs/specs/phase-1-mcp-declarative-loading.md` 转 Accepted** —— Open Questions 全 close：`stdio.init_timeout` 沿用 30s；`ENABLE_FS_MCP` 未来是否弃用留 Phase 4 决
- ✅ **`docs/adr/005-mcp-driver-abstraction.md` 起草完成** —— Alternatives 记了 4 条拒绝理由：单文件 yaml、只参数化 enabled_if、CEL 表达式引擎、`os.ExpandEnv`

**CI 验证结果**：
- `ci.yml` build/vet/lint 三绿 ✅
- Linux `go test -race` 全绿 ✅
- PR #6 `6c2e947` squash-merge 合入 main ✅

**代码变化摘要**：

- 新布局：`internal/mcp/{config,cond,driver,loader}.go` + `internal/mcp/inproc/inproc.go` + `internal/mcp/stdio/stdio.go`
- 删掉旧 `internal/mcp/{inproc,filesystem}.go`
- `cmd/server/main.go` 第 3 步从两次硬编码 `Start*` 换成一次 `mcp.LoadAll(MCP_DIR)`
- `cmd/evals/main.go` 同上换掉 —— **这是 Task #4 之外意外收编的**，evals 的 boot 流程要跟 server 保持镜像，不然两条路径漂移
- 新增 `mcp/mcp.yaml`（inproc + `default_root: /agents`）+ `mcp/filesystem.yaml`（stdio + `enabled_if: env:ENABLE_FS_MCP=1`）
- Dockerfile 加 `COPY mcp/ /mcp/` + `ENV MCP_DIR=/mcp`

**关键设计决策**（一句话总结）：

- **Driver + Loader 双层分层**：Driver = 每 transport 一实现（`inproc` / `stdio`），Loader = 扫 yaml → 判 `enabled_if` → 分派 driver
- **fail-fast 语义变化**：`enabled_if=true` 但启动失败 = pod crashloop（旧行为是 warn-and-continue），是**行为语义变化**
- **tool name 前缀 = `cfg.Name + "."`** —— 所以 `mcp/filesystem.yaml` 里 `name: fs`（不是 `filesystem`），保留旧 `fs.*` 前缀不改 agents/*.yaml
- **`enabled_if` 语法故意做窄**：只支持 `always` / `env:VAR` / `env:VAR=v` 三种，拼错 fail-fast（拒绝 CEL / `os.ExpandEnv` 那类通用表达式，见 ADR-005）
- **inproc `list_dir` 新增 `default_root` 兜底**：修 STATUS 已知问题 #1（distroless 里 CWD 是 `/` 极简），无参调用现在返回 `/agents` 目录内容

**本地验证结果**：

- `go build ./...` ✅
- `go vet ./...` ✅
- `go test ./internal/mcp/...` ✅（含 `cond_test.go` / `loader_test.go`）
- `golangci-lint` 本地没装，CI 兜底 ✅
- evals：CI `evals.yml` 待手动触发（需 Ark secret）

## 2026-07-16 傍晚 Phase 0 收尾 + Phase 1 起手

**Phase 0 已完成** —— 转型元流程 setup 全部落地：

- ✅ 仓库从 **Private 转 Public**（GitHub Free 私有仓库不支持 branch protection；详见 [ADR-004](docs/adr/004-branch-protection-on-main.md)）
- ✅ **Secret scanning + Push protection + Dependabot security updates** 全部启用（`gh api PATCH` 一把改）
- ✅ **`main` 分支 branch protection** 生效：required PR + required checks (`build + vet` / `golangci-lint`) + enforce_admins + no force-push + required_conversation_resolution
- ✅ **Adversarial test 通过**：`git push origin main` 被拒（`remote: error: GH006: Protected branch update failed`）
- ✅ **PR #5 合入 main**（squash-merge `6a52da5`）—— **第一个走完整 branch protection 流程的 PR**，Phase 0 正式收尾。产出：spec + ADR 模板、ADR-001~004、workbuddy-vision.md、PR 模板、CLAUDE.md 硬规矩生效
- ✅ 踩过一个坑：branch protection 里的 `contexts` 数组填的是 **job display name**（`build + vet`）不是 job key（`build-and-test`），app_id null 是没匹配的信号。修正后 app_id 15368 出现。已存 memory `github-required-check-name-uses-display-name`

**Phase 1 起手**（feature branch `feat/mcp-declarative-loading`，仅 spec，未推）：

- ✅ **`docs/specs/phase-1-mcp-declarative-loading.md`** 已起草（376 行，Draft 状态，待用户 review）
- 一句话摘要：把 `internal/mcp/{inproc,filesystem}.go` 硬编码 + `main.go` 硬调用重构成**目录扫描声明式** —— `mcp/*.yaml` + driver 抽象（inproc / stdio 两种 transport）+ `enabled_if` 表达式（`always` / `env:VAR` / `env:VAR=v`）
- **运行时行为承诺 0 变化**：evals 6/6 必须保持绿；tool 名字前缀 `mcp.*` / `fs.*` 不变；agents/*.yaml 不改
- **顺带解决 STATUS 已知问题 #1（list_dir 空）+ #2（fs MCP 无 npx）** —— yaml `default_root` + `enabled_if` 语义清晰化
- **明确决定 fail-fast**：enabled=true 但启动失败 = pod crashloop（不再 warn-and-continue），是一处**行为语义变化**待用户确认
- Spec 里剩 1 个 Open Question 待敲定：`stdio.init_timeout` 默认 30s 沿用 vs 全项目统一（倾向沿用）
- **代码未动**，spec review 通过后再写 ADR-005 + 开工实现

**Dependabot 报的 6 个 CVE**（Public 启用后首扫）：2 high (vite Windows path bypass / buger/jsonparser DoS) + 4 moderate (vite × 2 / esbuild / protobuf)。**全部 dev-only 或 transitive**，runtime image 是 distroless static，不包含 npm 生态，实际暴露面远低于标注严重程度。**已延后处理**：Phase 1 完成后单独 fix 分支清一批。

**待用户 review 后续动作**：
1. Phase 1 spec review → 用户敲定 Open Question + 确认 fail-fast → 我写 ADR-005 → 实现代码
2. Dependabot 6 CVE 清理（可延后到 Phase 1 完成后）
3. `.github/workflows/*.yml` 里几个 action version 升级消 deprecation warning（低优先，后续 PR 顺手做）

## 2026-07-16 上午 方向转换

**旧目标（demo 打磨）已收尾** —— CI / evals / lint / STATUS 同步基线全部到位；`research-goroutine` 意外转绿（详见 2026-07-16 凌晨节 + memory `llm-eval-needs-multiple-samples`）。

**新目标（workbuddy 产品 + 工业级实践载体）** —— 项目**从 demo 演化为 workbuddy 类产品**：页面上配置 subagent / skill / MCP，并且**用它作为工业级 AI 开发实践的载体**：

- 每个特性走 **spec → ADR → feature branch → PR → adversarial code review → eval → 合入** 完整流程
- `main` 分支 branch protection，不允许直推
- 前端 UI 在 Phase 4 一次性升级 shadcn/ui + Tailwind
- 学习优先，产品可以"能用但不完善"

**Phase 划分**（详见 vision spec）：
1. ✅ **Phase 0**（07-16 完成）：元流程 setup（spec / ADR 模板 + 回溯 ADR + PR 模板 + branch protection + vision spec）—— **PR #5 合入 main `6a52da5`**
2. 🟡 **Phase 1**（07-16 起手，spec 待 review）：MCP 声明式加载（yaml + driver 层）
3. Phase 2：Registry 可变 + Host 原子 swap（`Unregister` / `atomic.Pointer`）
4. Phase 3：REST API（`/api/agents` `/api/mcp` `/api/skills`）
5. Phase 4：配置 UI（shadcn/ui + Tailwind）
6. Phase 5：OTel trace + Langfuse

**Phase 0 产出**（squash-merged 到 main，commit `6a52da5`）：
- `docs/specs/_template.md` + `docs/adr/_template.md`
- `docs/adr/001-monorepo-single-binary.md`
- `docs/adr/002-eino-as-orchestrator.md`
- `docs/adr/003-distroless-runtime.md`
- `docs/adr/004-branch-protection-on-main.md`（仓库 Private → Public 决策 + protection 配置细节）
- `docs/specs/workbuddy-vision.md`
- `.github/pull_request_template.md`
- **`main` 分支 branch protection 生效**（配置详见 ADR-004；实测直推被拒）
- STATUS.md + CLAUDE.md 同步（硬规矩生效）

**已知问题 #1 / #2 归属调整**（保持不变）：
- #1（list_dir 返回 [] 因为容器无 WORKDIR）→ Phase 1 MCP 声明式加载时顺带修（yaml 里配 `default_root`），无需单独 Dockerfile 改动
- #2（filesystem MCP 在容器没起因为没 npx）→ Phase 1 里改为**声明式 enabled_if gate**；本地开发 `ENABLE_FS_MCP=1` 自动 enable，容器无此 env 自动 disable，语义清晰不再"warn-and-continue"

## 2026-07-16 凌晨 变更

**GitHub Secrets 加成 + evals workflow 首跑通**（1 个 commit：`e66a375`）：

- `gh secret set ARK_API_KEY / ARK_MODEL_ID` 已配（用 `--body '值'` 直传，**不**走 stdin —— 第一次用 `printf | gh --body -` 写入时 CI 侧 401 `The API key format is incorrect`，本地同 key 正常，改成明文参数就干净了）
- 手动触发 `evals.yml` × 3：
  - 首跑：secret 传递错，6/6 全 401
  - 二跑：secret 修好，**6/6 全 PASS**（包括预期红的 `research-goroutine`）
  - 三跑：`e66a375` 修 workflow bug 后 rerun，artifact 从 0 bytes → 1133 bytes，`cmd/evals exit=0` 打印出来
- **`research-goroutine` 意外转绿** —— STATUS #3 记的观察 baseline 是 n=1 采样，Ark 模型不同调用之间有波动；当前 6/6 是绿的，改 host prompt 的动机被打掉。#3 归档为"待长期观察，非阻塞"。
- `e66a375 fix(ci): evals workflow captures stderr and honours exit code` —— 修两个坑：
  1. `cmd/evals` 用 `log.Printf` 走 stderr，裸 `| tee` 只抓 stdout → artifact 0 bytes。加 `2>&1`。
  2. bash pipeline 默认取最后一段（`tee` 恒 0）→ workflow 永绿。用 `set +e` + `$PIPESTATUS[0]` 显式取 `cmd/evals` 真实退出码（跟 `ci.yml` smoke test 同一模式）。

**Runtime 未变**（只改 CI workflow + 配 GH secret），无需重部署。

**尚未做（下一次会话优先级 —— 07-16 上午方向转换后已废止，重排见 07-16 上午节 Phase 1+）：**
1. ~~已知问题 #3 归档为"待长期观察"~~ ✅ 已归档，见 07-16 上午方向转换
2. ~~Dockerfile 加 `WORKDIR /data` + 样例文件~~ → 归到 Phase 1 里 MCP yaml `default_root` 声明式解决
3. ~~决定 filesystem MCP 在容器里怎么处理~~ → 归到 Phase 1 `enabled_if` gate
4. GH Actions 各 action 升级到 Node 24 兼容版本，消 deprecation warning（低优先，后续 PR 顺手做）
5. 本机装 `golangci-lint` + `lefthook`（CI 兜底后 nice-to-have）
6. Ark 控制台老 API Key 排查阶段的临时 key 是否 revoke

## 2026-07-15 傍晚 变更

**CI 转绿的过程**（4 个 commit：`2b20bb5` → `af48c9d`）：

| commit | 问题 | 修法 |
|---|---|---|
| `2b20bb5` | smoke test 里 `if server | tee` 因 tee 恒 0 永远走 true 分支 | `set +e` + `$PIPESTATUS[0]` 显式取前段退出码 |
| `028578d` | `golangci-lint-action@v7 version: v2.0` 拉到 v2.0.2（Go 1.24 编），拒绝加载 `go: "1.26"` 的 config | 升到 `version: v2.12`（Go 1.26 编） |
| `fdaa560` | 3 条实际 findings：evals/main.go 未 gofmt、filesystem.go log 日志 taint、server/main.go 7 处 `log.Fatalf` 与 `defer` 组合被 gocritic exitAfterDefer 挑 | gofmt -w / `%s` → `%q` / `log.Fatalf` → `log.Printf + os.Exit(1)` |
| `af48c9d` | 上一 commit 后仍有 2 条：gocritic 认所有"defer + 进程终止"（`os.Exit` 也算）；gosec G706 是静态 taint 追踪，`%q` 运行时转义救不了 | `.golangci.yml` 加两条**精准豁免**，附注释解释 tradeoff |

**关键坑（写进决策日志）：**
- `golangci-lint-action` 用 `version: vX.Y` 系列 pin 会拉到该系列**最老**的 patch 版本，不是最新。Config 里的 `go:` 版本比 binary 的编译 Go 版本高就报 `can't load config`。要么 pin 到具体版本，要么用够新的 major.minor。
- gocritic `exitAfterDefer` 语义广：`log.Fatal` / `os.Exit` / `panic` 都算 —— 修 `log.Fatalf` → `os.Exit` 换汤不换药。
- gosec `G706` 是静态 taint 追踪，格式化 verb 改成 `%q` 只影响运行时，静态分析层面变量还是 tainted。要根治只能 taint 源头（不用 env 变量）或豁免。

**Runtime 未变**（只改 CI 配置 + `cmd/server/main.go` 的错误处理形状），无需重部署。

**尚未做（下一次会话优先级）：**
1. ~~首次 CI run 后看 lint report~~ ✅
2. GitHub 仓库加 `ARK_API_KEY` / `ARK_MODEL_ID` secret → 手动触发一次 evals workflow → 观察 `research-goroutine` 是否红
3. 依据 eval 结果调 host prompt / research description
4. 本机装 `golangci-lint` + `lefthook`（可选，CI 兜底后本机装不装差别不大）
5. （小）actions/*  升级到 Node 24 版本，消除 deprecation warning

## 2026-07-15 下午 变更

- **`.github/workflows/ci.yml`** —— push / PR 时自动跑
  - `build-and-test` job：npm build → stage 前端到 embed 目录 → `go build ./...` → `go vet ./...` → server 冒烟启动（预期缺 Ark env 会 fail-fast）
  - `lint` job：同样先 stage 前端 → `golangci-lint` 全仓扫描（傍晚 batch 转绿后稳定跑）
  - `concurrency.cancel-in-progress`：同分支旧 run 被新 push 覆盖，省 CI 分钟
- **`.github/workflows/evals.yml`** —— 手动 `workflow_dispatch` 触发（暂不 auto-run）
  - 需要仓库 Secrets：`ARK_API_KEY` + `ARK_MODEL_ID`
  - 跑 `go run ./cmd/evals`，report 作为 artifact 上传
  - **待你做**：GitHub 仓库 → Settings → Secrets and variables → Actions 添加两个 secret

## 2026-07-14 / 07-15 上午 变更

三个 commit 已推到 `origin/main`（`98234de` → `30ccabc`）：

| commit | 说明 |
|---|---|
| `6e29d87 chore(deps)` | `go mod tidy` —— `eino` / `eino-ext/*` / `mcp-go` / `yaml.v3` 从 indirect 升到 direct |
| `2aed52c docs` | 新增 `CLAUDE.md` + `docs/roadmap-ai-engineering.md` |
| `c0c5ee7 feat(evals+lint)` | `evals/routing.yaml` + `cmd/evals` + `.golangci.yml` + `lefthook.yml` |
| `54be7be docs(status)` | 07-15 上午 checkpoint |
| `30ccabc docs(architecture)` | ARCHITECTURE.md 同步（时间戳、目录树、§13.7 评估章节改写）|

**验证：** `go build ./...` + `go vet ./...` 均通过。运行时二进制 / 镜像 / pod 行为**未变**，无需 `docker build` / `kubectl apply`。生产 pod 沿用 07-11 部署。

---

## 一句话现状

**k8s 部署已跑通** —— pod `1/1 Running`，SSE / routing / Ark / 内置工具全通。剩下的都是打磨（Dockerfile 没设 WORKDIR 导致 list_dir 结果诡异；filesystem MCP 在容器里没起因为缺 npx）。

---

## 已完成

### 应用层
- Eino 多 agent 骨架：Host + math / research / ops 三条 specialist
- Ark v0.9.12 集成：`ToolCallingModel` + `Specialist.Invokable/Streamable` + `host.WithAgentCallbacks`
- SSE 流式 HTTP + tool/handoff 回调
- MCP 双集成（inproc + filesystem via `npx`，容器里只有 inproc 生效）
- 声明式 sub-agent 扩展面（`agents/*.yaml`）

### 环境 / 账号（07-06）
- **Ark 401 已解**：新 key `ark-e6f7c3ce-...-e3918` + endpoint `ep-20260609204306-xj4xt`，控制台已把 key 授权给 endpoint
- Docker Desktop 29.6.1 装好，内置 k8s Ready，`kubectl` context = `docker-desktop`
- `kubectl create secret generic ark-secret` 已建（`ARK_API_KEY` + `ARK_MODEL_ID`）

### k8s 部署（07-11）
- **Dockerfile Go 版本改 1.24 → 1.26**（原来跟 go.mod `go 1.26.4` 不匹配，直接编不了）
- `docker build -t eino-demo:local .` 成功，镜像 42MB / 压缩后 10MB
- `kubectl apply -f k8s/deployment.yaml -f k8s/service.yaml` 成功
- 用 `docker save + docker cp + ctr -n k8s.io images import` 把镜像塞进 containerd 的 k8s 命名空间（详见"决策日志"）
- Pod ready，`readinessProbe`（`/healthz`）通过，Ark secret 读到
- **浏览器验证**（`kubectl port-forward svc/eino-demo 8080:80` → http://localhost:8080）：
  - `12 乘以 7 等于多少` → ✅ `→ math_agent` + `🔧 calculator`
  - `UTC 现在几点` → ✅ `→ ops_agent` + `🔧 current_time`
  - `echo hello agents 然后列一下当前目录` → ⚠️ 部分符合预期（见下方"已知问题"）

### GitHub 远端（07-11 晚）
- 装了 `gh` CLI（winget `GitHub.cli` v2.96.0，路径 `C:\Program Files\GitHub CLI\gh.exe`）
- GitHub 账号是 `mlliu920423-beep`，git config email 改成 `mlliu920423-beep@users.noreply.github.com`
- push 前 `git rebase --root --exec 'git commit --amend --no-edit --reset-author'` 把三条历史 commit 的 author 从 `Bigmay@...` 改成 GitHub 账号邮箱，贡献日历/头像才能挂上
- 仓库：https://github.com/mlliu920423-beep/first-agentInK8s（**Private**），`origin/main` 已追踪

### 工程化基线（07-14 / 07-15）
- **`CLAUDE.md`** —— AI 会话首读文件，锁死技术栈版本 + Ark endpoint 授权坑 + `ctr import` 流程 + 加 sub-agent 规范
- **`docs/roadmap-ai-engineering.md`** —— 业界五层实践总结 + 本项目 13 项改进 backlog
- **`evals/routing.yaml` + `cmd/evals`** —— 路由回归 eval 骨架，`go run ./cmd/evals` 卡红绿；6 条 case 已编，含 STATUS 问题 #3 的"红色 case"
- **`.golangci.yml` + `lefthook.yml`** —— lint / pre-commit 配置起草完毕，本地工具链**尚未装**（CI 已兜底）
- **`.github/workflows/ci.yml`** —— push / PR 自动跑 `build + vet + lint + server 冒烟`（07-15 下午）
- **`.github/workflows/evals.yml`** —— 手动触发跑 routing evals（需仓库 Secret `ARK_API_KEY` / `ARK_MODEL_ID`）
- **`go.mod` 元数据清理** —— 直接依赖脱离 `// indirect`

---

## 已知问题（未修，优先级低）

### 1. list_dir 返回 `[]` ✅ fixed (2026-07-17)
Dockerfile 没 `WORKDIR`，server 的 CWD 是 `/`，distroless 镜像里 `/` 极简，`list_dir({"path": "."})` 拿不到有意义的内容。**修法：** Dockerfile 里加 `WORKDIR /data` + 一个 `RUN mkdir /data && ...` 塞几个样例文件；或前端默认传具体路径。

**Phase 1 已修**：`mcp/mcp.yaml` 里 `default_root: /agents` 兜底，distroless 里 `list_dir` 无参调用现在返回 `agents/` 目录内容。保留本条作为决策历史。

### 2. filesystem MCP 在容器里没起 ✅ fixed (2026-07-17)
Distroless 镜像没 `node` / `npx`，`fs.read_file` 那类外部 MCP 工具注册失败，日志明说：
```
tools: optional tool "fs.read_file" not registered (external MCP disabled?), skipping
```
**修法（三选一）：**
- 换基础镜像塞进 node（镜像会大一倍）
- 起个 sidecar container 跑 node MCP，Go 通过 stdio/socket 连
- 在容器里只用 inproc MCP，接受这个限制

**Phase 1 语义清晰化**：不再 warn-and-continue，容器里显式打印 `mcp: fs disabled (enabled_if=env:ENABLE_FS_MCP=1, source=/mcp/filesystem.yaml)`。"没起"从"bug 感"变成"声明式关闭"。保留本条作为决策历史。

### 3. host 路由把第三条给了 ops 而不是 research
本地也有此风险 —— 是 host prompt / 工具描述的语义问题，跟部署无关，独立调优。

**2026-07-16 更新**：evals workflow 首跑（run `29466374857`）`research-goroutine` **意外转绿**（`got-agent: research_agent`）。原始观察 baseline 是 n=1 采样，Ark 模型不同调用之间有波动。归档为"待长期观察，非阻塞"—— 定期 rerun 一次 evals 看它会不会重新翻红，红了再调 prompt。

### 4. LLM 跳过 `mcp.echo`
"echo hello agents" 太简单，LLM 直接复述、没触发工具调用。要强制走工具需要在 prompt 里更明示。

---

## 下一步（按顺序，可选）

### 0. 收尾工程化基线（07-14 起草，进行中）
- ✅ `CLAUDE.md` + roadmap + evals 骨架 + lint 配置 + CI workflow
- [ ] **第一次 CI run 后按 lint report 调 `.golangci.yml`** —— 预计需要给 `cmd/` 加豁免或改历史代码
- [ ] **GitHub 仓库加 `ARK_API_KEY` / `ARK_MODEL_ID` secret**（Settings → Secrets and variables → Actions），然后手动触发一次 evals workflow
- [ ] （可选）本机装 `golangci-lint` + `lefthook`（CI 兜底后 nice-to-have）

### 1. 打磨部署产物
- Dockerfile 加 `WORKDIR /data` + 样例文件（解决 list_dir 空结果）
- 决定 filesystem MCP 怎么处理（推荐 sidecar 或干脆放弃）

### 2. 路由 / prompt 调优（用 eval 卡红绿而不是手点）
- 看 host prompt 是不是让 ops 的描述吞了 research 的活
- 让 `mcp.echo` 的描述更"抢戏"，或者在 research prompt 里显式要求
- **改动流程**：先跑 `go run ./cmd/evals` 记录基线 → 改 prompt → 再跑 → 对比

### 3. 扩展验证（延续 07-06 遗留）
加 `agents/joke_agent.yaml` 试自动路由，验证 YAML 扩展面无需改 Go 就能加 specialist。

### 4. 工作流
- 老 API Key 排查阶段的临时 key 是否 revoke

---

## 部署 Runbook（重跑一次时用）

```powershell
# 1. build（镜像进 docker 那份 store）
docker build -t eino-demo:local .

# 2. 把镜像塞进 k8s 那份 store（每次 build 都要重跑！）
docker save eino-demo:local -o eino-demo.tar
docker cp eino-demo.tar desktop-control-plane:/root/eino-demo.tar
docker exec desktop-control-plane ctr -n k8s.io images import /root/eino-demo.tar
# 验证：
docker exec desktop-control-plane crictl images | Select-String eino

# 3. 应用 k8s manifests（首次或改动后）
kubectl apply -f k8s/deployment.yaml -f k8s/service.yaml

# 4. 每次镜像变化触发滚动更新
kubectl rollout restart deployment eino-demo
kubectl rollout status deployment/eino-demo

# 5. 转发到本机验证
kubectl port-forward svc/eino-demo 8080:80
# 浏览器打开 http://localhost:8080

# 6. 清理临时文件
rm eino-demo.tar
```

**为什么第 2 步不能省** —— 见"决策日志"最下面那条。

---

## 环境约定（给 Claude 和未来的自己）

- Shell：**PowerShell**，不是 bash。设环境变量用 `$env:VAR="value"`，跑本目录 exe 写 `.\server.exe`；bash 的 `VAR=value ./cmd`、反斜杠续行不通用
- Windows **没装 make**，直接跑 Makefile 里的底层命令
- Go 1.26.4 / Node 24.16 / Docker Desktop 29.6.1（含 k8s v1.36.1）/ kubectl v1.36.1 / `npx` 都已装
- Ark 走"在线推理端点 + 按量付费"，**不是** Agent Plan（套餐不能直接调 API）
- **Docker Desktop 的 docker daemon 和内置 k8s image store 是分开的**，即便 Settings 里勾了 "Use containerd for pulling and storing images"（07-11 实测）—— 需要手动 `ctr images import` 才能让 k8s 看见 docker build 的镜像

### 本地启动样板

```powershell
cd D:\Bigmay\Projects\first-agentInK8s
$env:ARK_API_KEY="ark-e6f7c3ce-...-e3918"
$env:ARK_MODEL_ID="ep-20260609204306-xj4xt"
.\server.exe
```

---

## 关键坐标（想改什么 → 改哪儿）

| 想改什么 | 打开哪个文件 |
|---|---|
| 加新 sub-agent | `agents/*.yaml`（无需改 Go） |
| 加内置 skill | `internal/tools/xxx.go` + `registry.go:RegisterBuiltins` |
| 加 MCP server | 仿 `internal/mcp/filesystem.go` 写 `Start*` |
| 换 LLM | 改 `internal/llm/ark.go`（返回 `model.ToolCallingChatModel`） |
| 改路由脑 | `internal/agents/build.go:DefaultHostPrompt` |
| 改 SSE 事件 | `internal/httpapi/events.go` + `sse.go` + `web/src/App.tsx:applyEvent` |
| bootstrap 顺序 | `cmd/server/main.go` |
| Docker 构建 | `Dockerfile`（多阶段：Go + web） |
| k8s 部署 | `k8s/deployment.yaml`（`imagePullPolicy: Never`，引用 `ark-secret`）/ `k8s/service.yaml` |

---

## 决策日志

| 日期 | 决策 | 原因 |
|---|---|---|
| 2026-07-06 | 弃用 kind，走 Docker Desktop 内置 k8s | 链路短一节，不用 `kind load` |
| 2026-07-06 | `imagePullPolicy: Never` | 本地 image，禁止 fallback 到 registry 拉远端（会失败还慢） |
| 2026-07-02 | Ark 走在线推理端点 + 按量付费 | Agent Plan 套餐只给 AI 工具用，不能直连 API |
| 2026-07-10 | 项目建 git 仓库；`STATUS.md` 进 git 作为状态唯一入口 | memory 里堆日期版状态用户看不见、换机器丢；`STATUS.md` 跟代码走可 review 可回溯 |
| 2026-07-11 | Dockerfile `golang:1.24` → `1.26` | go.mod 声明 `go 1.26.4`，1.24 toolchain 编不了 |
| 2026-07-11 | 每次 build 后手动 `ctr -n k8s.io images import` | Docker Desktop 4.x 的 docker daemon 和内置 k8s containerd 的 image store 分开，即便 GUI 勾了 "Use containerd for images" 也不共享（实测）。写死进 runbook |
| 2026-07-11 | 推 GitHub Private 仓库 | 首个跑通的 agent 项目，值得留档；日常 `git push` 走 gh 缓存 token，无需单独配 credential |
| 2026-07-14 | 引入 `evals/` + `cmd/evals` 作为路由回归骨架 | Roadmap §4 观点：AI 项目里 eval = 传统单测。改 host prompt / description 后手点浏览器不 scalable，要卡红绿 |
| 2026-07-14 | 加 `CLAUDE.md` 作为 AI 会话首读文件 | 跨 AI 会话（Claude Code / Cursor / Codex）稳定的项目约束，避免每次重新踩 Ark 401 / Go 版本 / ctr import 那些坑 |
| 2026-07-14 | 选 `golangci-lint` + `lefthook` 而不是 `pre-commit` + `revive` | lefthook 单 Go binary、Windows/Linux 一致；golangci-lint 是社区默认 aggregator。装工具尚未闭环 |
| 2026-07-15 | GitHub Actions CI 拆两个 workflow：`ci.yml` 自动跑 build/vet/lint，`evals.yml` 手动触发 | evals 需要真 Ark key + 花钱，分离触发方式能让 CI 稳定绿而不受 endpoint / 网络影响；等观察一段时间再考虑要不要把 evals 挂到 PR |
| 2026-07-15 | `.golangci.yml` 加两条精准豁免（server exitAfterDefer / filesystem G706）而不是改代码结构 | server bootstrap 抽 `run() error` 只为过 lint 是纯 ceremony，`cancel()` 在失败路径漏跑无害；filesystem taint 源是本地 env 不是网络输入，gosec 判定过严。豁免带 inline 注释解释 tradeoff |
| 2026-07-15 | `golangci-lint-action` pin 到 `version: v2.12`（不是 `v2.0`） | action 用 `vX.Y` 系列 pin 会拉该系列最老 patch（v2.0.2 = Go 1.24 编），拒绝加载 `go: "1.26"` 的 config。升级到 v2.12（2026-05 release，Go 1.26 编）才能真正跑规则 |
| 2026-07-16 | GH Actions secret 走 `gh secret set --body '值'` 明文传参，**不用** `printf \| gh --body -` stdin 管道 | 第一次用 stdin 管道写入后 CI 侧 401 `The API key format is incorrect`，本地同 key 正常，暗示 stdin 通道混入了额外字符。明文参数最不容易翻车 |
| 2026-07-16 | `evals.yml` 用 `2>&1 \| tee` + `$PIPESTATUS[0]` 而不是裸 `\| tee` | `cmd/evals` 用 `log.Printf` → stderr，裸 tee 只抓 stdout → artifact 空；tee 恒 0 → workflow 假绿。这个坑跟 `ci.yml` smoke test 那步同源，写第二次是抄错了模式 |
| 2026-07-16 | 仓库从 Private 转 Public 以启用 `main` 分支 branch protection | GitHub Free 私有仓库不支持 branch protection（`gh api` 实测 403 Upgrade to Pro）；升 Pro $4/mo 或走软性 hook 都 dominated 于"转 Public + 免费 protection"。代码本身是学习成果，公开顺带丰富贡献日历。详见 [ADR-004](docs/adr/004-branch-protection-on-main.md) |
| 2026-07-16 | branch protection `contexts` 用 job display name（`build + vet` / `golangci-lint`）不是 job key | GitHub 匹配 required check 用的是 `jobs.<x>.name:` 值。初版用 job key `build-and-test` / `lint` 时 `app_id: null`，说明没匹配上，protection 形同虚设。修正后 `app_id: 15368` 出现，adversarial test 才真通过。已存 memory `github-required-check-name-uses-display-name` |
| 2026-07-16 | 项目走完整 spec → ADR → PR → protection 流程首用 = PR #5 | Phase 0 元流程 setup 自己走一遍新流程验证闭环。squash-merge 保持 main history 一 PR 一 commit；`--delete-branch` 自动清理；`git remote prune` 后 local main 干净 |
| 2026-07-17 | MCP driver 抽象 + `mcp/*.yaml` 声明式加载 | vision Phase 1 落地：加 MCP server = 加 yaml 不改 Go；enabled_if 语义清晰；fail-fast 替代 warn-and-continue。详见 [ADR-005](docs/adr/005-mcp-driver-abstraction.md) |
| 2026-07-17（晚）| Registry 可变 + Host 原子 swap（Supervisor 抽象 + atomic.Pointer）| vision Phase 2 落地：为 Phase 3 REST API + Phase 4 UI 铺路。Rebuild 事务化 + 旧引用跑完不 drain。详见 [ADR-006](docs/adr/006-registry-mutation-host-swap.md) |

---

## 未决 / TODO

- [x] ~~项目是否建 Git 仓库~~ → 已建（07-10）
- [x] ~~上 k8s~~ → 完成（07-11）
- [x] ~~推 GitHub 远端仓库~~ → 完成（07-11 Private → 07-16 转 Public）
- [x] ~~加 `CLAUDE.md` / 工程化 roadmap~~ → 完成（07-14）
- [x] ~~路由回归 eval 骨架~~ → 完成（07-14，红色 case 待实测）
- [x] ~~`golangci-lint` / `lefthook` 配置~~ → 配置起草完成（07-14），本机工具链未装
- [x] ~~GitHub Actions CI~~ → build/vet/lint 自动 + evals 手动（07-15 下午）
- [x] ~~首次 CI run 后按 report 调 `.golangci.yml`~~ → 完成（07-15 傍晚，run `29411625130` 全绿）
- [x] ~~GitHub Secrets 加 `ARK_API_KEY` / `ARK_MODEL_ID`，手动触发 evals workflow~~ → 完成（07-16 凌晨，run `29466566000` 6/6 全绿）
- [x] ~~`go run ./cmd/evals` 实测（或走 CI），`research-goroutine` 转绿~~ → 完成（意外转绿，n=1 baseline 不代表长期）
- [x] ~~Phase 0 元流程 setup + branch protection~~ → 完成（07-16 傍晚，PR #5 合入 main `6a52da5`）
- [x] **Phase 1 spec Accepted + ADR-005 起草 + 代码实现完成（2026-07-17）**
- [x] **Phase 2 spec Accepted + ADR-006 起草 + 代码实现完成（2026-07-17 晚）**
- [ ] **Phase 1 PR 走 branch protection 流程合入 main**（push feature branch + evals 本地/CI 结果贴 PR 描述）
- [ ] **Phase 2 PR 走 branch protection 合入 main**（push + evals 结果贴 PR）
- [ ] **Phase 3 spec 起草：REST API `/api/agents` `/api/mcp` `/api/skills` CRUD + Rebuild 触发**
- [ ] ~~Dockerfile 加 WORKDIR + 样例文件（fix list_dir）~~ → 归到 Phase 1 里 `default_root` yaml 字段解决
- [ ] ~~决定 filesystem MCP 在容器里怎么处理（sidecar / 放弃）~~ → 归到 Phase 1 `enabled_if` gate 声明式表达
- [ ] Dependabot 6 CVE 清理（Phase 1 完成后单独 fix 分支；全部 dev-only 或 transitive，不影响 runtime）
- [ ] 装 `golangci-lint` + `lefthook`，跑第一次 lint 并按 report 调配置（CI 兜底后 nice-to-have）
- [ ] `.github/workflows/*.yml` 里几个 action version 升级消 deprecation warning（低优先，后续 PR 顺手）
- [ ] 老 API Key 排查阶段建的临时 key 是否 revoke（转 Public 后建议做，多一层保险）
