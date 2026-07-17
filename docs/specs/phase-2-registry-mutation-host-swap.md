# Phase 2 Spec: Registry 可变 + Host 原子 swap

> **Status**: Accepted
> **Owner**: @Bigmay
> **Related ADR(s)**: ADR-006（Registry mutation + Supervisor + atomic host swap）
> **Related feature branch / PR**: `feat/registry-mutation-host-swap`, PR TBD
> **Last updated**: 2026-07-17（用户 review 通过，转 Accepted；4 个 Open Questions 全 close 走推荐；evals 不走 Supervisor 走推荐）

## Context

**触发**：`docs/specs/workbuddy-vision.md` Phase 2 主线。

**现状**（Phase 1 结束时的静态假设）：

- `internal/tools/registry.go` 只有 `Register` / `Get` / `Names` / `MustResolve` —— **只能加，不能减**。启动时装完 built-in tools + MCP tools 后就冻结
- `cmd/server/main.go:96` build 出的 `*host.MultiAgent` 直接传给 `httpapi.Server.HostMA` —— **裸指针捕获**，进程生命周期内不变
- `httpapi.Server.HandleChat` 里 `s.HostMA.Stream(ctx, ...)` 直接调固定的 host 实例

**痛点**（vision 视角 —— 见 [`workbuddy-vision.md`](workbuddy-vision.md) §Goals）：

1. **改 agent 配置必须重启 pod**：Phase 3 REST API `PUT /api/agents/:name` 要生效必须能"运行时重建 host"；不做这一步 Phase 3 就只能写"改了 yaml 然后重启 pod"，退化成纯配置管理工具
2. **改 MCP 配置同理**：Phase 3/4 用户在 UI 里 disable 一个 MCP，不能靠 pod restart 生效
3. **Registry 冻结 → 无法 UI-driven MCP 增删**：MCP tool 是 Registry 的 client，Registry 不能减就意味着 MCP 也不能 UI 增删

**约束**：

- Phase 2 **纯 refactor**：不加外部触发接口（no REST API, no signal handler），Rebuild 只在 hard-coded 单元测试里调
- SSE 事件契约 / evals / agents/*.yaml schema 全部不变（跟 Phase 1 同款承诺）
- 允许"新旧 host 并存跑到底"：不 drain in-flight 请求，见 [Risks & Tradeoffs](#risks--tradeoffs) 详细论证
- Phase 3 才会给 Supervisor 挂 REST 触发接口，Phase 4 挂前端

**前置技术侦察**（决策依据）：

- [`docs/research/phase-2-registry-mutation-design.md`](../research/phase-2-registry-mutation-design.md) —— Registry API 最小改动方案
- [`docs/research/phase-2-host-swap-risks.md`](../research/phase-2-host-swap-risks.md) —— Eino v0.9.12 host / react / compose 全局状态扫描；关键发现是 `callbacks.AppendGlobalHandlers` 不幂等且非线程安全

## Goals

- **Registry 可 mutate**：加 `Unregister(name) error`（幂等）；`MustResolve` 已返回的 slice 引用不受影响
- **Host 可原子 swap**：新增 `internal/agents/Supervisor` 抽象持 `atomic.Pointer[host.MultiAgent]`；替换后旧引用继续跑，新请求走新 host
- **Phase 3 可挂接**：Supervisor 暴露 `Current()` / `Rebuild(ctx) error` 两个 method，Phase 3 REST handler 直接调 `Rebuild` 即可（无需再改分层）
- **Rebuild 是单写者且事务化**：一次只能有一个 Rebuild 在进行；构造新 host 失败时旧 host 继续服务，Registry 不被弄脏
- **零外部行为变化**：evals 6/6 不退化；SSE / /healthz / 前端契约字节相同

### Non-Goals

**Phase 2 明确不做**：

- **不加外部触发接口**（`/api/reload`、`SIGHUP` 都不做）—— Phase 3 主线
- **不做 REST CRUD**（`/api/agents` `/api/mcp`）—— Phase 3 主线
- **不做前端 UI**（`/config/*`）—— Phase 4 主线
- **不做 graceful drain**：Rebuild 不阻塞等 in-flight 请求完成
- **不做 Registry 快照 / 回滚 API**：Rebuild 的"事务化"通过"先构造完新 host 才 commit Registry mutation"实现，不引入显式 tx 层
- **不改 host prompt 位置**：`agents.DefaultHostPrompt` 仍是 Go const（Phase 3 挪 yaml）
- **不加 `Replace(name, tool)` API**：`Unregister + Register` 组合够用（Supervisor 是单写者，原子性诉求为 0）
- **不引入 `Snapshot()` API**：build 路径按 specialist yaml 声明子集 resolve，不需要全量 dump

## Options Considered

### Option A: `agents.Supervisor` 抽象 + `atomic.Pointer[host.MultiAgent]`（推荐）

**结构**：

```
cmd/server/main.go
  ↓ 启动时 build 出 initial Supervisor（含 *MultiAgent）
  ↓ InstallToolCallbacks() 只调一次
  ↓ httpapi.Server 持 *agents.Supervisor（不再持 *MultiAgent）
  ↓
internal/agents/supervisor.go
  ├─ Supervisor struct { current atomic.Pointer[*MultiAgent], rebuildMu sync.Mutex, deps... }
  ├─ NewSupervisor(ctx, deps) (*Supervisor, error)   —— 首次 build
  ├─ Current() *host.MultiAgent                       —— 拿当前 host（HandleChat 首行调）
  └─ Rebuild(ctx) error                                —— 单写者：build → atomic.Store
```

`Rebuild` 内部序：

1. `rebuildMu.Lock()`（防止两次 Rebuild 并发）
2. 重新 `agentcfg.Load` + `mcp.LoadAll`（**但先构造到新的临时 Registry / closers，不动全局状态**）
3. `BuildSpecialist × N + BuildHost` → 新 `*MultiAgent`
4. **任一步失败**：`defer` 里 close 已启动的 driver、清理临时 Registry、return error；**旧 Supervisor / 旧 Host / 旧 Registry 完全不变**
5. 成功：`atomic.Store(newHost)` → 换掉全局 Registry（Register 新 tools + Unregister 老的）→ Close 老 MCP driver 的 `io.Closer`（grace period 后）
6. `rebuildMu.Unlock()`

Phase 2 只暴露 `Rebuild(ctx)` 供**单元测试**调；Phase 3 REST API `POST /api/reload` 会直接调它。

**Pros**：

- Supervisor 抽象层职责单一（"当前 host 是什么、怎么换"）；Phase 3 挂 REST 时不动 httpapi
- `atomic.Pointer` 是标准库能力，无第三方依赖
- Rebuild 事务化天然：新 host 没 build 完就不 commit，旧服务不受任何影响
- 每个 HandleChat 只一次 `Current()`，之后不看 Supervisor —— 请求生命周期内 host 引用稳定

**Cons**：

- 新增一个抽象层（~150 行 Go），代码量比现在多
- `Rebuild` 里要重跑 `agentcfg.Load` + `mcp.LoadAll`，本地 IO；预期 ms 级但比"啥都不做"慢

### Option B: 直接在 `httpapi.Server` 里加 `atomic.Pointer[host.MultiAgent]`

不新建 Supervisor，就在 `httpapi.Server` 加一个 atomic pointer 字段和 `Rebuild` 方法。

**Pros**：

- 少一个抽象层
- 代码短

**Cons**：

- httpapi 层化身承担编排职责，分层混乱：Phase 3 REST handler 也在 httpapi 层，让 handler 调自己层的 Rebuild 违反"handler = 薄传输层"约定
- Phase 3 加 `/api/mcp` `/api/agents` 时想复用 Rebuild 逻辑，得从 httpapi 里挖出来 —— 又要重构一次
- 测试性差：想单测 Rebuild 得起个 HTTP server 或 export 私有方法

**为什么不选**：Phase 3/4 会证明抽象层是必要的，现在少走一步等于将来多走两步。

### Option C: 加 REST `/api/reload` + 无 Supervisor（Phase 2 与 3 合并）

一次做完："加 Registry mutation + Supervisor + REST 触发接口 + Dockerfile 挂 reload URL"。

**Pros**：

- 单个 PR 一次到位，无中间态

**Cons**：

- PR 太大，review 成本高；触碰点从"编排层"扩到"HTTP 层 + 路由 + 权限（未来）"
- 违反 workbuddy-vision 的分 phase 学习节奏（每个 phase 学一层工业模式）
- Phase 3 spec 明明就是"给 Phase 2 抽象挂 REST 接口"，两段边界清晰不该合并
- 现在没有 auth，加 `/api/reload` 意味着**任何人**可以打这个 endpoint 触发 rebuild —— 拒绝在 auth 到位前引入这个攻击面

**为什么不选**：workbuddy-vision 分 phase 的意义就在于**边界锁死** —— 合并 phase 会让每步学到的东西都不深。

## Decision

**采用 Option A：新增 `agents.Supervisor` 抽象 + `atomic.Pointer[host.MultiAgent]` + 单写者 Rebuild**。

关键分层：

```
cmd/server/main.go
  ↓
internal/agents/supervisor.go       ← 新
  ├─ 持 atomic.Pointer[*host.MultiAgent]
  ├─ 持 rebuildMu sync.Mutex
  ├─ 持 dependencies（chat model / agentsDir / mcpDir / registry）
  └─ 提供 Current() / Rebuild(ctx)
  ↓
internal/httpapi/sse.go
  └─ Server 持 *agents.Supervisor（替代 HostMA *host.MultiAgent）
  └─ HandleChat 首行 hostMA := s.Sup.Current()
```

`internal/tools/registry.go` 只新增一个方法 `Unregister(name) error`（幂等），其余保留。

## Detailed Design

### 数据模型 / Schema

**`agents.Supervisor`**：

```go
// Supervisor owns the current *host.MultiAgent and orchestrates atomic
// swaps of it on Rebuild. Concurrency model:
//   - Read side (HandleChat): lock-free via atomic.Load.
//   - Write side (Rebuild):   single-writer via rebuildMu.
// See docs/adr/006-registry-mutation-host-swap.md.
type Supervisor struct {
    current   atomic.Pointer[host.MultiAgent]
    rebuildMu sync.Mutex

    // Dependencies captured at NewSupervisor time.
    model      einomodel.ToolCallingChatModel
    reg        *tools.Registry
    agentsDir  string
    mcpDir     string
    hostPrompt string

    // Owned MCP driver closers. Replaced on successful Rebuild;
    // the old slice is Closed after the atomic swap (with a small
    // grace delay to let in-flight requests grab their handles).
    mu       sync.Mutex   // guards mcpClosers
    mcpClosers []io.Closer
}

func NewSupervisor(ctx context.Context, deps SupervisorDeps) (*Supervisor, error)
func (s *Supervisor) Current() *host.MultiAgent
func (s *Supervisor) Rebuild(ctx context.Context) error
func (s *Supervisor) Shutdown(ctx context.Context) error  // Close all mcpClosers
```

**`tools.Registry`** 新增方法：

```go
// Unregister removes name from the registry.
//
// Idempotent: if name is not registered, returns nil (with a debug log line).
// Rationale — Rebuild flows may call Unregister on names that never got
// registered (e.g. an fs.* tool when filesystem MCP is disabled).
//
// IMPORTANT: Unregister only removes the map entry. Any []tool.BaseTool
// slice previously returned by MustResolve keeps its references and remains
// invokable — the tool's underlying resources (e.g. MCP client) are owned
// by whoever holds the driver's io.Closer, not by the Registry.
func (r *Registry) Unregister(name string) error
```

### Rebuild 算法（关键）

```
1. rebuildMu.Lock() / defer Unlock()
2. cfg, err := agentcfg.Load(s.agentsDir)             // 读 yaml 到内存
   if err { return err }                              // 旧 host 继续跑
3. // 用一个临时 Registry 做 dry-run，不动全局
   tmpReg := tools.NewRegistry()
   tools.RegisterBuiltins(ctx, tmpReg)
   newClosers, err := mcp.LoadAll(ctx, s.mcpDir, tmpReg)
   if err {
       for _, c := range newClosers { _ = c.Close() }
       return err                                     // 旧 host 继续跑
   }
4. // Build 新 specialists + 新 host（用 tmpReg 做 MustResolve）
   newHost, err := buildFromRegistry(ctx, s.model, cfg, tmpReg, s.hostPrompt)
   if err {
       for _, c := range newClosers { _ = c.Close() }
       return err                                     // 旧 host 继续跑
   }
5. // ✅ 全部成功,才 commit:
   //    a. 把 tmpReg 的内容 merge 进全局 s.reg（Unregister 老的，Register 新的）
   //    b. atomic.Store 换 host
   //    c. 记下旧 closers,延迟 Close
   s.mergeRegistry(tmpReg)                            // built-in 是 idempotent
   oldClosers := s.swapClosers(newClosers)
   s.current.Store(newHost)
   go func() {
       time.Sleep(gracePeriod)                        // e.g. 30s
       for _, c := range oldClosers { _ = c.Close() }
   }()
6. return nil
```

**关键约束**（source: `docs/research/phase-2-host-swap-risks.md`）：

- **`InstallToolCallbacks()` 只在 `cmd/server/main.go` 启动时调一次**，Rebuild 严禁再调（`AppendGlobalHandlers` 不幂等且非线程安全）
- **单写者**：`rebuildMu` 保证同时最多一个 Rebuild
- **HandleChat 首行 `Current()` 一次然后走到底**：一个请求生命周期内 host 引用稳定，不看 atomic
- **旧 MCP closer 延迟 Close**：`gracePeriod` 参数化，MVP 用 30s；in-flight 请求的 tool interface 值一旦被 ReAct graph 编入就存活到 graph GC

### 迁移路径

**`cmd/server/main.go` 变化**：

```go
// Before (Phase 1)
mcpClosers, err := mcpbridge.LoadAll(ctx, mcpDir, reg)  // step 3
// ... build specialists / host ...
apiSrv := &httpapi.Server{HostMA: hostMA}
// ... shutdown: for _, c := range closers { _ = c.Close() }

// After (Phase 2)
sup, err := agents.NewSupervisor(ctx, agents.SupervisorDeps{
    Model:      arkModel,
    Registry:   reg,
    AgentsDir:  agentsDir,
    McpDir:     mcpDir,
    HostPrompt: agents.DefaultHostPrompt,
})
if err != nil { log.Printf(...); os.Exit(1) }
// step 3 + 4 + 5 + 6 全部内置在 NewSupervisor 里
apiSrv := &httpapi.Server{Sup: sup}
// ... shutdown: sup.Shutdown(ctx)  // 内含所有 io.Closer
```

**`internal/httpapi/sse.go` 变化**：

```go
type Server struct {
    Sup *agents.Supervisor   // 替代 HostMA *host.MultiAgent
}

func (s *Server) HandleChat(w http.ResponseWriter, r *http.Request) {
    // ... 前置 sink/header 逻辑不变
    hostMA := s.Sup.Current()   // 一次
    // ... 后续用 hostMA 走到底,不再看 s.Sup
}
```

**`internal/httpapi/sse.go` 里其他改动**：`s.runStream` 的签名从"隐式用 s.HostMA"变成"显式传 hostMA 参数"，确保测试时能 mock。

**`cmd/evals/main.go`**：evals 不需要 Rebuild，但为了保持 boot 流程镜像，也换成通过 Supervisor 拿 host（`sup.Current()`）。或者更简单：evals 直接调 `agents.BuildHost` 一次不走 Supervisor（Phase 2 允许，evals 场景明确不 rebuild）。**倾向后者**，因为 evals 是一次性 run 完退出，不需要 mutation。

### API 变化（外部可见）

**零变化**：

- `/api/chat` SSE 事件契约不变
- `/healthz` 不变
- 前端 `App.tsx` / `sseClient.ts` 不动
- `agents/*.yaml` / `mcp/*.yaml` schema 不动

## Acceptance Criteria

- [ ] `internal/tools/registry.go` 加 `Unregister(name) error`（幂等）+ 对应单元测试（覆盖 name 不存在、name 存在、Unregister 后 caller 手里 slice 仍能 Invoke）
- [ ] `internal/agents/supervisor.go` 新文件：`Supervisor` struct + `NewSupervisor` / `Current` / `Rebuild` / `Shutdown` 全实现
- [ ] `cmd/server/main.go` 用 `NewSupervisor` 替换 step 3~6 硬编码；`httpapi.Server` 持 `*Supervisor`
- [ ] `internal/agents/supervisor_test.go` 覆盖：
  - Rebuild 成功：新 Current() 返回新 host，旧引用仍可调
  - Rebuild 失败（如构造错的 agentsDir）：旧 host 继续服务，Registry 不脏
  - 并发 Rebuild：两个 goroutine 同时调 Rebuild，rebuildMu 序列化，结果一致
  - 并发 Rebuild + Current：读端不阻塞
  - Unregister 后旧 MustResolve slice 仍能 Invoke（跨 Rebuild in-flight 请求场景模拟）
- [ ] `httpapi` 层无 test 变化（Server 结构变但行为不变）
- [ ] `go build ./... && go vet ./... && go test ./... && golangci-lint run` 全绿
- [ ] `evals/routing.yaml` 本地跑 6/6 不退化
- [ ] Docker build + `ctr import` + `kubectl rollout` 一遍：容器 pod ready，`/api/chat` 表现跟 Phase 1 完全一致
- [ ] STATUS.md 归档 Phase 2 完成 + 上下文
- [ ] ARCHITECTURE.md §7（编排 + Host 相关章节）改写：加 Supervisor 抽象说明 + rebuild sequence 图
- [ ] CLAUDE.md 目录约定 / "AI 助手做事的偏好" 加 Supervisor 引用（可选，Phase 2 是内部 refactor 不引入新的跨会话约束）
- [ ] ADR-006 已写

## Risks & Tradeoffs

### 风险

- **风险 1: `callbacks.AppendGlobalHandlers` 误调**：research note 明确 `AppendGlobalHandlers` 不幂等且非线程安全。**缓解**：`InstallToolCallbacks()` 保留在 `cmd/server/main.go` 启动阶段调用，`Supervisor.Rebuild` 严禁调；code review 加这一条 checklist；`supervisor_test.go` 里用 `runtime.NumGoroutine` 或 handler counter 验证 Rebuild 不新增 handler
- **风险 2: 旧 MCP driver Close 时机**：旧 host 被 atomic 换掉后，理论上没有新请求会用它的 MCP tools —— 但**已经进入 ReAct graph 的 in-flight 请求**手里的 tool interface 值仍持有 MCP client 引用。**缓解**：`gracePeriod` 参数化（MVP 30s），Close 前等这个时间；`gracePeriod` 内旧请求应能完成（P99 chat 请求 < 30s）。**未来 Phase 3**：加请求计数器 + graceful drain 语义
- **风险 3: Rebuild 事务性 vs 用户体验**：如果新 yaml 有 bug（比如 typo 的 tool name），Rebuild fail-fast，旧 host 继续跑 —— 这是 feature；但**用户看不到 error**（因为 Phase 2 没有 API），只能看 log。**接受**：Phase 2 就是内部 refactor，Rebuild 只由测试触发；Phase 3 REST API return HTTP 400 with error message
- **风险 4: mcpClosers 内存泄漏**：旧 closers 在 grace period 后 goroutine Close 掉；如果 goroutine 崩溃，closers 泄漏。**缓解**：goroutine 内 `defer recover()` + log；`Supervisor.Shutdown` 兜底 Close 所有已知 closers

### Tradeoffs

- **Tradeoff 1: `Supervisor` 抽象增加代码量**：~150 行 Go + 测试 ~200 行。**接受** —— Phase 3/4 会消化这个投入
- **Tradeoff 2: 不加 `Replace` API**：`Unregister + Register` 组合等价但多一次 lock。**接受** —— Supervisor 是单写者，锁开销可忽略
- **Tradeoff 3: 不做 graceful drain**：旧请求可能在 Close 后失败（如果超过 grace period）。**接受** —— vision spec 明确"允许旧引用跑完不 drain"；grace period 30s 覆盖 P99；Phase 3+ 有需要再加
- **Tradeoff 4: `internal/httpapi/sse.go` 的 Server struct 字段名从 `HostMA` 改为 `Sup`**：小 breaking，但 Server 是 internal 且只在 main.go 用一次。**接受**

## Out of Scope

以下未来做，本 spec 不承诺：

- **REST API `/api/reload`** —— Phase 3 主线，会给 Supervisor 挂 HTTP handler
- **Rebuild 事件通知**（Server-Sent Events push "config changed"）—— Phase 4 前端可能需要，另做 spec
- **Rebuild 时的 tracing**（OTel span from HTTP handler → Rebuild → build steps）—— Phase 5 主线
- **Registry snapshot 用于 diff / audit** —— UI 里想"看看 rebuild 前后有啥变化"，Phase 4 前端可能要，届时加 `Snapshot()`
- **Rebuild 频率限制 / 抖动保护** —— UI 里用户狂点保存的场景，Phase 3 加 rate limit 或 debounce
- **Rebuild 时的 partial 成功语义**：现在是 all-or-nothing。未来"3 个 agent 里 1 个 yaml 坏了但另 2 个新"要不要接受，Phase 3 前端交互定
- **`tool.BaseTool` 的 explicit lifecycle**：Unregister 不 Close 是当前特性 —— 未来 tool 类型如果要 Closer 接口，另开 ADR

## Open Questions

- [x] **`gracePeriod` 的默认值**：**参数化**。`SUPERVISOR_MCP_GRACE_PERIOD` env var，默认 30s。允许部署侧 override，也保留 MVP 直观值
- [x] **`Supervisor.Rebuild` 的 ctx 用途**：**用 `context.WithoutCancel`**（Go 1.21+）派生 rebuild 用 ctx。防止 caller 手滑 cancel 导致 rebuild 走一半留下混乱状态。下游调用（`agentcfg.Load` / `mcp.LoadAll` / `NewMultiAgent`）拿的是这个新 ctx，值仍带过来但 cancel 不传播
- [x] **Rebuild 时 `agentcfg.Load` 是否也重跑**：**重跑**。理由：Phase 3 加 REST 时立刻能用；Phase 2 单元测试也验证了这条路径；成本 ms 级
- [x] **是否加一个测试专用 API `Sup.CurrentSnapshot()`**：**不加**。用 `Current()` 前后指针比较（`ptrA != ptrB`）即可
- [x] **evals 是否走 Supervisor 抽象**：**不走**。evals 直接调 `agents.BuildHost` 一次不 rebuild。理由：evals 是一次性 run 完退出，不需要 mutation；代码更简单；boot 流程不 100% 镜像 server 属于 acceptable divergence（明确记录在 spec）

## References

- [`docs/specs/workbuddy-vision.md`](workbuddy-vision.md) §Phase 2 —— 上层目标
- [`docs/specs/phase-1-mcp-declarative-loading.md`](phase-1-mcp-declarative-loading.md) —— 前置 phase，Registry 是 Phase 1 的产物
- [`docs/research/phase-2-registry-mutation-design.md`](../research/phase-2-registry-mutation-design.md) —— Registry API 设计的技术推理
- [`docs/research/phase-2-host-swap-risks.md`](../research/phase-2-host-swap-risks.md) —— Eino v0.9.12 全局状态扫描 + 关键 caveat
- `internal/tools/registry.go` —— 现有 Registry 实现
- `internal/agents/build.go` —— 现有 host + specialist 构造流程
- `internal/httpapi/sse.go` —— 现有 chat handler，Phase 2 触碰点
- `cmd/server/main.go` —— 启动编排，Phase 2 触碰点
- Eino v0.9.12 `flow/agent/multiagent/host` / `react` 源码路径见 research note
- Go 官方 [`sync/atomic.Pointer`](https://pkg.go.dev/sync/atomic#Pointer) —— 推荐用 pointer swap 而非 mutex-guarded field
