# ADR 006: Registry mutation + `agents.Supervisor` + Host `atomic.Pointer` swap

> **Status**: Accepted
> **Date**: 2026-07-17
> **Owner**: @Bigmay
> **Related spec(s)**: [phase-2-registry-mutation-host-swap](../specs/phase-2-registry-mutation-host-swap.md)、[workbuddy-vision](../specs/workbuddy-vision.md)

## Context

**触发**：workbuddy-vision Phase 2 主线 —— 把"启动时装配一次、进程生命周期内不变"的两个静态假设拆掉，为 Phase 3 REST API 和 Phase 4 前端 UI 铺路。**Phase 2 是纯 refactor**：不加任何外部触发接口，只做内部抽象。

**现状**（Phase 1 结束时）：

| 层 | 静态假设 |
|---|---|
| `internal/tools/registry.go` | 只有 `Register` / `Get` / `Names` / `MustResolve`；启动装完就冻结 |
| `cmd/server/main.go:96` | 构造 `*host.MultiAgent` 一次；直接传给 `httpapi.Server.HostMA` |
| `internal/httpapi/sse.go` | `Server.HostMA` 是**裸指针捕获**，进程生命周期内不变 |

**问题**：Phase 3 `PUT /api/agents/:name` 想 mutation 生效，必须能"运行时重建 host"；Phase 4 前端"disable 一个 MCP"同理。当前架构下这些改动**必须重启 pod**，退化成纯配置管理工具。

**前置技术侦察**（决策的关键依据）：

1. [`docs/research/phase-2-host-swap-risks.md`](../research/phase-2-host-swap-risks.md) —— Eino v0.9.12 全局状态扫描。**关键发现**：`host.MultiAgent` / `react.Agent` / `compose.Graph` 都是 per-instance，无跨实例可变全局状态；**但 `callbacks.AppendGlobalHandlers` 不幂等且非线程安全**（官方文档明写 "Call it once during program initialization"）
2. [`docs/research/phase-2-registry-mutation-design.md`](../research/phase-2-registry-mutation-design.md) —— Registry API 最小改动方案。**关键结论**：`Unregister` 只删 map entry，`MustResolve` 已返回的 `[]tool.BaseTool` slice 引用保持有效（interface 值语义 + MCP client lifecycle 由独立 `io.Closer` 持有）

上述两点组合让 "atomic swap + 允许旧引用跑完" 语义天然可行 —— 但也标记了**必须避开的坑**（`InstallToolCallbacks` 严禁在 Rebuild 里再调）。

## Decision

**采用三层解耦：**

1. **`internal/tools/registry.go` 加 `Unregister(name string) error`（幂等）** —— 底层能力；不加 `Replace` / `Snapshot`（YAGNI，Supervisor 是单写者，无原子性诉求）
2. **新增 `internal/agents/supervisor.go`** 提供 `Supervisor` 抽象，持 `atomic.Pointer[host.MultiAgent]` + `rebuildMu sync.Mutex`，暴露 `Current()` / `Rebuild(ctx) error` / `Shutdown(ctx) error`
3. **`internal/httpapi/sse.go` 的 `Server` 从持 `HostMA *host.MultiAgent` 换成持 `Sup *agents.Supervisor`**，`HandleChat` 首行 `hostMA := s.Sup.Current()` 一次然后走到底

关键分层：

```
cmd/server/main.go
  ↓ agents.NewSupervisor(ctx, deps)  ← 首次 build,replaces step 3~6
  ↓
internal/agents/supervisor.go          ← 新
  ├─ atomic.Pointer[*host.MultiAgent]
  ├─ rebuildMu sync.Mutex
  └─ Current() / Rebuild(ctx) / Shutdown(ctx)
  ↓
internal/httpapi/sse.go
  └─ Server{ Sup *agents.Supervisor }  ← HandleChat 首行 Current()
```

**Rebuild 事务化算法**（每一步失败旧 host 完全不受影响）：

1. `rebuildMu.Lock()`
2. 用**临时 Registry** dry-run：`agentcfg.Load` + `mcp.LoadAll` + `BuildSpecialist` × N + `BuildHost` —— **任一步失败**：清理临时 driver closers、return error、旧 host 继续跑
3. 全部成功才 commit：把临时 Registry merge 进全局 Registry（Unregister 老、Register 新）→ `atomic.Store(newHost)` → 延迟 `gracePeriod`（默认 30s，`SUPERVISOR_MCP_GRACE_PERIOD` env 可 override）后 Close 老 MCP driver 的 `io.Closer`
4. `rebuildMu.Unlock()`

**Rebuild 的 ctx** 用 `context.WithoutCancel`（Go 1.21+）派生 —— 防止 caller 手滑 cancel 导致 rebuild 走一半留下混乱状态。下游调用值仍带过来但 cancel 不传播。

**Phase 2 只暴露 `Rebuild(ctx)` 供单元测试调**；Phase 3 REST handler 会直接挂到这个 method 上，无需再改分层。

## Consequences

### Positive

- **Phase 3/4 无需再重构分层**：REST handler 直接调 `sup.Rebuild(ctx)`；前端 disable MCP → API → Rebuild 全通
- **零外部行为变化**：SSE 契约 / evals / agents.yaml / mcp.yaml 全部不动，`/api/chat` 表现跟 Phase 1 完全一致
- **Rebuild 事务性天然**：新 host 没 build 完就不 commit，配置错误的 yaml 只会 log error 不会让服务失能
- **读端 lock-free**：`atomic.Pointer.Load` 在 x86 就是 mov 指令；`HandleChat` 无锁；Rebuild 期间正常请求零阻塞
- **测试可控**：`Supervisor` 是 pure abstraction，`supervisor_test.go` 可以覆盖并发 rebuild、rebuild 失败恢复、旧 tool 引用继续可调等场景
- **旧引用跑完不 drain 语义清晰**：`gracePeriod` 参数化，MVP 30s 覆盖 P99 chat 请求

### Negative

- **代码量增加**：`Supervisor` 抽象 ~150 行 + 单元测试 ~200 行 + registry `Unregister` + 测试 ~50 行；Cost 换 Phase 3/4 无重构成本
- **httpapi.Server.HostMA → Sup 是 breaking**：外部 no-op（`Server` 是 internal），但要同步改 `cmd/server/main.go`
- **`InstallToolCallbacks` 陷阱**：`Rebuild` 严禁调 `InstallToolCallbacks()`，否则事件重复 + 数据竞争。**缓解**：spec + ADR + code review 三层提醒；`supervisor_test.go` 加一条断言"Rebuild 前后 handler 数不变"
- **旧 MCP driver Close 时机不精确**：`gracePeriod` 30s 是 heuristic，超过这个时间还在跑的请求会失败。**接受**：workbuddy-vision 明确"允许旧引用跑完不 drain"；Phase 3+ 有需要再加请求计数 + graceful drain

### Neutral

- **不加 `Replace(name, tool)` API**：`Unregister + Register` 组合等价，Supervisor 是单写者所以原子性诉求为 0。未来真需要"热替换单个 tool 且请求持续打进来"场景，再加也不迟
- **不加 `Snapshot()` API**：build 路径按 specialist yaml 声明子集 resolve，不需要全量 dump。Phase 4 UI 想 diff 前后 Registry 时再加
- **evals 不走 Supervisor**：`cmd/evals/main.go` 直接调 `agents.BuildHost` 一次不 rebuild —— evals 一次性 run 完退出，无 mutation 场景。boot 流程与 server 有一处 divergence，可接受（明确记录在 spec）
- **`Supervisor` 抽象层职责范围锁死**：只管"host 的构造 / 替换 / 生命周期"，**不管** yaml watch / 定时 reload / 外部触发。这些放 Phase 3+

## Alternatives Considered

### Alternative 1: 直接在 `httpapi.Server` 里加 `atomic.Pointer` + Rebuild

不新建 Supervisor，就在 `Server` struct 里加 atomic pointer 字段和 `Rebuild` method。

**为什么不选**：

- **分层混乱**：httpapi 层化身承担编排职责；Phase 3 REST handler 也在 httpapi 层，让 handler 调自己层的 Rebuild 违反"handler = 薄传输层"约定
- **测试性差**：想单测 Rebuild 得起 HTTP server 或 export 私有方法
- **Phase 3 重构成本**：加 `/api/mcp` `/api/agents` 时想复用 Rebuild 逻辑，得从 httpapi 里挖出来再抽层 —— 又要重构一次
- 现在少走一步等于将来多走两步

### Alternative 2: Mutex-guarded field 替代 `atomic.Pointer`

`Server` 里持 `hostMA *host.MultiAgent` + `sync.RWMutex`。HandleChat 拿 RLock 读，Rebuild 拿 WLock 写。

**为什么不选**：

- 读端加锁：每次 `HandleChat` 有一次 lock/unlock 开销，虽然 RLock 很轻但对比 `atomic.Load` 是无锁
- **锁范围难界定**：请求要 hold RLock 多久？如果只在拿引用时 hold 一瞬间，效果跟 atomic pointer 完全一样但代码更啰嗦；如果 hold 到 Stream 结束，则 Rebuild 要等所有请求完成 = graceful drain，跟本 spec Non-Goals 冲突
- `atomic.Pointer[T]` 是 Go 1.19+ 标准库泛型，无第三方依赖，可读性也好

### Alternative 3: RCU 手动实现（Read-Copy-Update）

用 `unsafe.Pointer` + `atomic.LoadPointer` / `atomic.StorePointer` 手撸 RCU 模式。

**为什么不选**：

- `atomic.Pointer[T]` 就是 RCU 的高层封装，标准库已经写好，重复造轮子
- `unsafe.Pointer` 直接用会绕过类型系统，PR review 时增加心智负担

### Alternative 4: Graceful drain（Nginx-style reload）

Rebuild 时阻塞等所有 in-flight 请求完成才换 host。

**为什么不选**：

- **workbuddy-vision 明确"允许旧引用跑完不 drain"**（Risks 章节）—— 不 drain 就是 feature 不是 bug
- SSE 请求可能长达数十秒甚至更久，drain 会让 Rebuild 延迟不可控
- 需要维护请求计数器 + wg.Wait 等基础设施，Phase 2 nested Non-Goals
- **grace period + 允许旧引用跑完** 的组合更简单且覆盖 P99：新请求马上用新 host，旧请求继续跑到 30s 内完成

### Alternative 5: 合并 Phase 2 + Phase 3（一次做完 refactor + REST API）

一个大 PR：Supervisor + Registry mutation + `/api/reload` REST handler + 认证 stub。

**为什么不选**：

- **workbuddy-vision 分 phase 的意义就在于边界锁死** —— 合并 phase 让每步学到的东西都不深
- PR 太大 review 成本高；触碰点从"编排层"扩到"HTTP 层 + 路由 + 权限"
- **auth 未到位前不能加 `/api/reload`**：任何人可以打这个 endpoint 触发 rebuild，攻击面明确
- Phase 3 spec 明明就是"给 Phase 2 抽象挂 REST 接口"，边界清晰不该合并

## Compliance / Validation

**PR merge 前必过**：

- [ ] `go build ./... && go vet ./... && go test ./... && golangci-lint run` 全绿
- [ ] `internal/tools/registry_test.go` 覆盖：
  - `Unregister` 幂等（name 不存在返回 nil）
  - `Unregister` 后 caller 手里的 `[]tool.BaseTool` slice 仍可调
- [ ] `internal/agents/supervisor_test.go` 覆盖：
  - Rebuild 成功：新 `Current()` 返回新指针；旧 `*MultiAgent` 引用仍可调
  - Rebuild 失败（例：agentsDir 为空 / yaml typo）：旧 host 继续服务，Registry 不脏
  - 并发 Rebuild：两 goroutine 同时调，`rebuildMu` 序列化，结果一致
  - 并发 Rebuild + Current：读端不阻塞（可用 `-race` 验证）
  - `callbacks.GlobalHandlers` 数量 Rebuild 前后不变
- [ ] `go run ./cmd/evals -file evals/routing.yaml` 本地 6/6 不退化
- [ ] Docker build + `ctr import` + `kubectl rollout` 一遍：容器 pod ready，`/api/chat` 表现跟 Phase 1 完全一致

**长期可执行的一致性检查**：

- `grep -rn "s.HostMA\." internal/httpapi/` → 应无匹配（应全部改成 `s.Sup.Current().`）
- `grep -rn "InstallToolCallbacks" internal/` → 应只在 `cmd/server/main.go` 调用一次
- `grep -rn "callbacks.AppendGlobalHandlers" internal/` → 应无匹配（我们只通过 InstallToolCallbacks 间接调）
- `grep -rn "atomic.Pointer" internal/agents/` → 应有且只有 `Supervisor` 里那一处
- Phase 3 PR 里如果加了 REST handler 调 Rebuild，不应该也需要改 Supervisor —— 说明抽象活了

## When to revisit

- **Rebuild 频繁度真实场景过高**（Phase 4 前端用户狂点保存）→ 需要 rate limit / debounce，可能得 revisit rebuildMu 单写者假设
- **`gracePeriod` 30s 覆盖不了 P99**（真实 chat 请求超时）→ 加请求计数 + graceful drain
- **Phase 3 REST API 需要 rebuild 的分布式一致性**（多 pod 部署时 rebuild 不同步）→ 需要 leader election 或事件广播，本 ADR 假设单 pod
- **Eino major 升级**（v0.10+）如果 `host.MultiAgent` / `compose.Graph` 引入了新的全局状态 → 本 ADR 的核心假设失效，需要重新扫描
- **`callbacks.AppendGlobalHandlers` 变成幂等 / 线程安全**（Eino 未来可能改）→ Rebuild 里也调也无害，简化约束
- **加协作者 / 多租户**（不同用户各自 rebuild）→ Supervisor 从单例变成 per-tenant，本 ADR 假设单 supervisor

## References

- [phase-2-registry-mutation-host-swap spec](../specs/phase-2-registry-mutation-host-swap.md) —— 本 ADR 是它的技术决策沉淀
- [workbuddy-vision.md](../specs/workbuddy-vision.md) §Phase 2 —— 上层目标
- [`docs/research/phase-2-host-swap-risks.md`](../research/phase-2-host-swap-risks.md) —— Eino v0.9.12 全局状态扫描 + key caveat
- [`docs/research/phase-2-registry-mutation-design.md`](../research/phase-2-registry-mutation-design.md) —— Registry API 设计推理
- `internal/tools/registry.go` —— Registry 现有实现
- `internal/agents/build.go` —— host + specialist 构造流程
- `internal/httpapi/sse.go` —— Phase 2 触碰点
- `cmd/server/main.go` —— Phase 2 触碰点
- Go 官方 [`sync/atomic.Pointer`](https://pkg.go.dev/sync/atomic#Pointer)
- Go 1.21 [`context.WithoutCancel`](https://pkg.go.dev/context#WithoutCancel)
- ADR-002（Eino 作 orchestrator）—— Supervisor 是 host.MultiAgent 的 owner，不动 orchestrator 内部
- ADR-005（MCP driver 抽象）—— MCP driver 的 `io.Closer` 生命周期由 Supervisor 持有，Phase 2 是它的直接依赖方
