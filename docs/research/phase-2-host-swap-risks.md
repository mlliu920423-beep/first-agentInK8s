# Phase 2 Host Swap — 风险扫描报告

> Research note，不进正式 docs / PR。Phase 2 spec 起草参考。
> 扫描范围：Eino v0.9.12 `host` / `react` / `compose` / `callbacks` 包 + Ark ChatModel adapter。

## 结论

**atomic swap 安全**，前提是 Phase 2 spec 里明确写"`InstallToolCallbacks()` 只在启动时调一次，Rebuild 不再重复注册"。Eino v0.9.12 的 `host.NewMultiAgent` / `react.NewAgent` 都是 per-instance 构图，无跨实例可变全局状态；旧新 MultiAgent 可以完全并存跑到底。

## 具体发现

### 1. host.MultiAgent 全局状态: none

- `MultiAgent` struct 仅持三个字段：`runnable compose.Runnable`、`graph *compose.Graph`、`graphAddNodeOpts []GraphAddNodeOpt`。全部 per-instance（`C:\Users\Bigmay\go\pkg\mod\github.com\cloudwego\eino@v0.9.12\flow\agent\multiagent\host\types.go:35-39`）
- `NewMultiAgent` 内部 `compose.NewGraph[...]`（`compose.go:77`）每次新建，`g.Compile(ctx, ...)`（`compose.go:142`）产出独立 `runnable`。没有向任何 package-level registry 注册
- `host` 包的 package-level 变量搜过了：只有 `defaultHostNodeKey` / `defaultHostPrompt` 等 `const`，没有 `var` mutable state
- `Stream` / `Generate` 的 ctx 来自 caller（`types.go:54-63`），没有 background goroutine 自己 hold ctx。旧实例的 in-flight 请求由**请求自己的 `r.Context()`** 控制生命周期，与 atomic swap 无关

### 2. compose.Graph 是否 per-instance: 是

- `NewMultiAgent` 每次调用重新 `compose.NewGraph[[]*schema.Message, *schema.Message](compose.WithGenLocalState(...))`（`compose.go:77-78`）。`WithGenLocalState` 传入的是**工厂函数** `func(context.Context) *state { return &state{} }` —— 每次 graph 执行都构造独立 `state`，`isMultipleIntents` / `msgs` 不会跨请求泄漏
- `compose` 包的全局仅一处：`compose.globalGraphCompileCallbacks`（`graph_compile_options.go:133`），通过 `InitGraphCompileCallbacks` 设置，本项目**没调用**，且它只影响 top-level Compile 阶段，不影响运行时
- `chatModel` 通过 `agent.ChatModelWithTools` → `ToolCallingModel.WithTools` 处理。Ark 实现 `chatmodel.go:465` 是 `ncm := *cm.chatModel`（shallow copy 后替换 `rawTools` / `tools`），返回新对象；旧新 MultiAgent 各自持有独立 `ChatModel` 副本，共享底层 HTTP client 也没问题（stateless）

### 3. callbacks.AppendGlobalHandlers 幂等性: **不幂等，须固定 init-once**

- `callbacks/interface.go:103-105`：`GlobalHandlers = append(GlobalHandlers, handlers...)` —— 纯 append，没有 dedup
- `callbacks/interface.go:100-102` 官方文档明写："**This function is NOT thread-safe. Call it once during program initialization**"，`doc.go:120` 也重复了这条
- 本项目 `internal/httpapi/sse.go:85-110` 的 `InstallToolCallbacks()` 每次调用都会**新造一个** `HandlerHelper` 并 append。如果 Phase 2 的 `Supervisor.Rebuild` 顺手也调它，就会：
  1. 每次工具调用触发 N 次 SSE `tool_call` 事件（N = rebuild 次数 + 1）
  2. 在有 in-flight 请求时 append，出现数据竞争（Go race detector 会抓）
- **Handler 本身无状态**（`sinkFrom(ctx)` 从每请求 ctx 拿 sink），所以只要**只注册一次**，新旧 Host 共享同一个 handler 是没问题的 —— 新 Host 的请求带新 sink 走同一个 handler，旧 Host 的请求带旧 sink 走同一个 handler，互不干扰
- `host.WithAgentCallbacks(hostCallback{})` 是 per-`Stream`-call 传的（`sse.go:168`），完全 request-scoped，Phase 2 后模型不变

### 4. react.Agent 全局状态: 只有 gob 类型注册，无问题

- `react/react.go:61-63` 的 `init()` 里 `schema.RegisterName[*state]("_eino_react_state")` 是**包 init 期一次性**注册（`schema/serialization.go:83-90`，`gob.RegisterName` + `serialization.GenericRegister`），跟 MultiAgent 实例数无关
- `react.NewAgent`（`react.go:284-397`）同样每次新建 `compose.NewGraph` + 独立 `state`（`react.go:329-331`）
- 特别看了下 `newToolResultCollectorMiddleware()`（`react.go:65-125`）—— 中间件从 ctx 读 `toolResultSenderCtxKey{}`，是 per-request context value 机制，没有全局态
- Phase 2 rebuild 会为每个 specialist 重新调 `react.NewAgent`，产出全新 `*react.Agent`；旧 specialist 的 `ra` 闭包（`internal/agents/build.go:68-73`）在旧 `host.Specialist` 里继续持有并可以正常跑，两套图完全隔离

### 5. tool.BaseTool 引用被 Unregister 后

已由 `docs/research/phase-2-registry-mutation-design.md` 详细分析。顺手 sanity-check：

- `internal/tools/registry.go:69-90` 的 `MustResolve` 返回 `[]tool.BaseTool` 切片，interface 值语义 = (itab, data) 一旦复制就与 map 脱钩；`Unregister` 只 `delete(r.m, name)`，不动 caller 手上的 slice
- ReAct 的 `compose.NewToolNode(ctx, &config.ToolsConfig)`（`react.go:325`）在 `NewAgent` 时把 `config.ToolsConfig.Tools`（就是 MustResolve 的返回）编进 graph 节点里。graph 节点持 `tool.BaseTool` 引用直到 graph 被 GC，Registry map 的删除不会切断这份引用
- 结论保持：Unregister 与 MCP `io.Closer` 生命周期正交，SAFE

## 建议

**Phase 2 spec 要显式声明的约束：**

1. **`InstallToolCallbacks()` 只在 `cmd/server/main.go` 启动阶段调用一次**，`Supervisor.Rebuild` 严禁再调。Rebuild 应当**只重建 MultiAgent + Specialists + Registry mutation**，不动 `callbacks.GlobalHandlers`
2. **rebuild 是单写者**（跟 registry-mutation 设计一致），因为 `AppendGlobalHandlers` 不幂等且非线程安全 —— 即便你在 Rebuild 里想 append，也不能在 in-flight 请求存在时做
3. **atomic swap 不 cancel 旧 MultiAgent 的 in-flight 请求** —— 旧请求由请求自己的 `r.Context()` 控制。这是 feature 不是 bug（"新旧共存并各自跑完"），但 spec 要明说，否则未来看到不 Close 老实例会以为漏了资源回收
4. **MultiAgent 无 `Close()`**（`types.go:35-73` 确认）—— 老实例只需从 atomic pointer 拿掉，GC 自然回收 graph / runnable。不用担心遗漏 teardown
5. **每次 Rebuild 会调 `react.NewAgent` × specialist 数 × `NewMultiAgent`** —— 每次都做完整的 `compose.NewGraph` + `Compile`。测一下 rebuild 耗时（3 个 specialist × ReAct graph + 1 个 Host graph）：预期 ms 级，但 spec 里给一个"Rebuild 频率上限"防止用户 UI 狂点
6. **`ChatModel.WithTools` shallow-copy 语义**（Ark 实现）—— 底层 HTTP client 是共享的。如果未来换 model provider，spec 里应有一条 "provider 必须保证 `WithTools` 返回可以并发使用的新对象"

**潜在的实现注意点：**

- Supervisor 存 `atomic.Pointer[host.MultiAgent]`，`Current()` 返回 `*host.MultiAgent`；`httpapi.Server` 不再持 `HostMA` 字段，改持 `sup *agents.Supervisor`（或类似），每个 `HandleChat` 首行 `hostMA := s.sup.Current()` 拿一次然后走到底。**不要在同一次请求里多次 `Current()`**，避免中途 rebuild 把上下游拆到两个实例
- `Supervisor.Rebuild` 内部顺序建议：`build 全部新 specialist → 新 hostMA` **先构造完成**，任一失败就 abort 不换指针（旧 host 继续服务）；成功后一步 `atomic.Store` 换指针；然后 grace period 结束再 Close 老 MCP driver 的 `io.Closer`
- Rebuild 有 partial 失败（新 hostMA 构造失败但 Registry 已经 mutate 了）的场景：spec 里要写"Rebuild 必须先 dry-run build 到 hostMA 成功再 commit Registry 修改" 或提供 Registry snapshot / rollback 机制。当前 `Registry.Register` + `Unregister` 组合没有 tx 语义，用户在 UI 加一个坏 MCP 就可能把 Registry 弄脏。这是 Phase 2 spec 需要另开小节讨论的**次生风险**（不是 host swap 本身的坑）

## 读过的文件

- `D:\Bigmay\Projects\first-agentInK8s\internal\agents\build.go`
- `D:\Bigmay\Projects\first-agentInK8s\internal\httpapi\sse.go`
- `D:\Bigmay\Projects\first-agentInK8s\internal\tools\registry.go`
- `D:\Bigmay\Projects\first-agentInK8s\cmd\server\main.go`
- `D:\Bigmay\Projects\first-agentInK8s\STATUS.md`
- `D:\Bigmay\Projects\first-agentInK8s\docs\research\phase-2-registry-mutation-design.md`
- `C:\Users\Bigmay\go\pkg\mod\github.com\cloudwego\eino@v0.9.12\flow\agent\multiagent\host\compose.go`
- `C:\Users\Bigmay\go\pkg\mod\github.com\cloudwego\eino@v0.9.12\flow\agent\multiagent\host\types.go`
- `C:\Users\Bigmay\go\pkg\mod\github.com\cloudwego\eino@v0.9.12\flow\agent\multiagent\host\callback.go`
- `C:\Users\Bigmay\go\pkg\mod\github.com\cloudwego\eino@v0.9.12\flow\agent\multiagent\host\options.go`
- `C:\Users\Bigmay\go\pkg\mod\github.com\cloudwego\eino@v0.9.12\flow\agent\react\react.go`
- `C:\Users\Bigmay\go\pkg\mod\github.com\cloudwego\eino@v0.9.12\flow\agent\utils.go`
- `C:\Users\Bigmay\go\pkg\mod\github.com\cloudwego\eino@v0.9.12\callbacks\interface.go`
- `C:\Users\Bigmay\go\pkg\mod\github.com\cloudwego\eino@v0.9.12\schema\serialization.go`
- `C:\Users\Bigmay\go\pkg\mod\github.com\cloudwego\eino@v0.9.12\compose\graph_compile_options.go`
- `C:\Users\Bigmay\go\pkg\mod\github.com\cloudwego\eino-ext\components\model\ark@v0.1.68\chatmodel.go`
- `C:\Users\Bigmay\go\pkg\mod\github.com\cloudwego\eino-ext\components\model\ark@v0.1.68\responses_api.go`
