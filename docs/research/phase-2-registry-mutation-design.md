# Phase 2 Registry Mutation — Design Note

> Research note，不进正式 docs / PR。Phase 2 spec 起草参考。

## Q1 `tool.BaseTool` 引用是否 Unregister-safe

**结论：SAFE。** `Unregister` 只删 map entry，不会 invalidate 已经拿到手的 slice。

推理链：

- `MustResolve` 返回 `[]tool.BaseTool`；`tool.BaseTool` 是 interface，值语义 = (itab pointer, data pointer)。一旦 return，slice 元素跟 `Registry.m` 完全脱钩 —— 后续 `delete(r.m, name)` 只是把 map bucket 里那份 interface 拷贝抹掉，不影响 caller 手上那份。
- **Built-in tools**（`calculator.go` / `weather.go` / `current_time.go`）：都是 `utils.InferTool(name, desc, func(ctx, in) (out, error))` 生成的闭包。闭包捕获的是常量（canned map）或 stdlib 调用（`time.LoadLocation`）。零 Registry 依赖，永远活着。
- **MCP tools**（`inproc.go` L122–139、`stdio.go` L85–102）：`einomcp.GetTools(ctx, &einomcp.Config{Cli: cli})` 生成的 adapter 内部持有 `cli` 引用。`cli` 的生命周期完全由 driver `Start` 返回的 `io.Closer` 管理（loader.go L104–106 把它塞进 `closers []io.Closer`，main / Supervisor 才是所有者）。`reg.Register(name, t)` 只是把 adapter 放进 map，`Unregister` 也只是从 map 里删掉 —— 不会 `Close()` `cli`，不影响 in-flight Invoke。

**推论（Phase 2 spec 需要写的）：Registry mutation 和 MCP client lifecycle 是两条正交的轴。** Rebuild 时的正确顺序是：

1. `driver.Start` → 新 `io.Closer` + 新 tool 注册进 Registry
2. `reg.Unregister(old_name)`（可选，如果 name 冲突）
3. Build 新 Host，原子换 pointer
4. 老 Host 被后续请求丢弃后，再 `oldCloser.Close()`（可以有 grace period 但不需要 drain —— 因为老 Host 引用被替换后自然没有新请求进来，只需等已进入的 Invoke 完成）

---

## Q2 建议的 API 签名

```go
// Unregister removes name from the registry.
//
// Idempotent: if name is not registered, returns nil (with a debug log line).
// Rationale — Phase 2 rebuild flows may call Unregister after a partial
// failure or on a name that never got registered (e.g. an fs.* tool when
// filesystem MCP is disabled). Making this a hard error would force every
// caller to wrap it in `if _, ok := r.Get(...); ok { r.Unregister(...) }`.
//
// IMPORTANT: Unregister only removes the map entry. Any []tool.BaseTool
// slice previously returned by MustResolve keeps its references and remains
// invokable — the tool's underlying resources (e.g. MCP client) are owned
// by whoever holds the driver's io.Closer, not by the Registry.
func (r *Registry) Unregister(name string) error
```

**不推荐加 `Replace`。** 理由：

- 语义等价于 `Unregister` + `Register`，只是加个锁把两步包一起
- Phase 2 rebuild 是 supervisor 层的单写者操作；没有其他 goroutine 会在 rebuild 期间调 `MustResolve`（Host 是被原子替换的，新 Host 在 build 完成前不接受请求）。所以原子性诉求为 0
- YAGNI：如果未来真的出现"热替换单个 tool 且请求持续打进来"的场景，再加也不迟，而且那时候 `Replace` 的语义（比如 name 不存在时行为）会有真实需求来定，而不是现在拍脑袋

如果非要加，签名建议：

```go
// Replace atomically swaps the tool registered under name.
// Returns ErrNotRegistered if name doesn't exist — Replace is NOT a Register
// fallback; the caller must have registered name earlier.
func (r *Registry) Replace(name string, t tool.BaseTool) error
```

**doc 里必须点明 "Unregister 不 Close underlying resources"**，否则调用方会误以为 Unregister 是资源回收动作。

---

## Q3 并发正确性 + 是否需要 Snapshot

**并发模型（一句话）：** `MustResolve` 持 RLock 遍历 map；`Unregister` / `Register` 持 Write lock 短暂修改；两者互斥，`MustResolve` 拿到的 slice 是快照 —— 遍历过程中不会看到半改半留的状态，遍历结束后 map 后续变更跟这份 slice 无关。

**Snapshot API 不需要加。** 理由：

- Build 路径（`agents/build.go` L40）是 `reg.MustResolve(cfg.Tools)`，每个 specialist 按自己 YAML 里声明的 tool 名子集来取。Phase 2 rebuild 是同样的路径 —— 每个 rebuild 后的 specialist 只需要它 yaml 声明的那几个 name，不需要全量 registry dump
- 现有 `Names()`（registry.go L50）已经能满足 logging / 调试 dump 需求
- 如果未来真要 "在 rebuild 期间 snapshot 一份 registry 做 diff / validation"，再加 `Snapshot() map[string]tool.BaseTool` 也很便宜（一个 O(n) copy 而已）。当前没有实际调用点

**一个 subtle 陷阱要记在 spec 里：** `MustResolve` 内部调用 `r.namesLocked()` 生成 error message（L87），这是允许的因为它在同一次 RLock 里执行。**不要**在 Phase 2 里让 `Unregister` 调用其他 lock-held 方法（避免 self-deadlock 或写锁降级为读锁）。

---

## Q4 `fs.*` optional 语义处理

**结论：不需要改 registry.go L77 那段。** Phase 2 完全兼容，而且行为更好。

现有语义：yaml `enabled_if` = false → filesystem stdio driver 不启动 → `fs.*` 从没进过 registry → 引用 `fs.*` 的 agent 走"skip with log"分支。

Phase 2 引入 `Unregister` 之后的新场景：操作员运行时禁用 fs → Supervisor 调 `Unregister("fs.read_file")` 等 → 老 Host 手里 slice 里的 `fs.*` 仍能跑完当前请求（见 Q1）→ 新 Host build 时 `MustResolve` 走"skip with log"分支。**语义完全一致**，`strings.HasPrefix(n, "fs.")` 的判定标准是 name 本身的 namespace，不是注册时机，所以运行时 Unregister 走同一条分支。

**唯一需要注意的**（spec 里提一下，代码不改）：这段特殊逻辑目前是硬编码 `fs.` 前缀。Phase 2 之后如果需要给别的 MCP server（比如未来加个 `db.*`）也做同样的"optional"处理，应当把它抽象成 MCP yaml 里的一个 `optional: true` 字段，而不是继续加 hard-coded prefix。**但这属于 Phase 3+ 范畴，Phase 2 不动。**

---

## Phase 2 spec 要在 Detailed Design 里写的约束

- **Registry mutation 和 tool lifecycle 正交**：`Unregister` 只删 map entry，不 `Close` 底层资源。MCP client 的 `io.Closer` 由 Supervisor 的 closer list 独立持有。这条是"允许旧引用跑完不 drain"的直接保证。
- **`Unregister` 是幂等**：name 不存在返回 nil。文档明确写。
- **不提供 `Replace`**：rebuild 场景用 `Unregister` + `Register` 组合，由 Supervisor 保证单写者。
- **Rebuild 是单写者操作**：Supervisor 自己持一把 rebuild mutex，保证同一时刻只有一次 rebuild 修改 Registry；`MustResolve` 与 rebuild 通过 Registry 自带的 `sync.RWMutex` 互斥，不需要外层再加锁。
- **不引入 `Snapshot`**：build 路径按 specialist YAML 声明的 tool 子集 resolve，不需要全量 dump。
- **`fs.*` optional 前缀逻辑不变**：namespace-based 判定，与 mutation 时机无关。抽象成 yaml `optional: true` 留给 Phase 3。
- **Rebuild 序列（推荐）：**
  1. start 新 driver → 写入新 tool
  2. build 新 Host
  3. 原子换 Host pointer
  4. 旧 driver 的 `io.Closer` 在 grace period 后 Close（不需要 drain in-flight —— 请求路径由 Host 分发，老 Host 被替换后无新请求进入，in-flight 请求手里的 tool interface 值自持，仍能完成 Invoke）
- **注意 Registry 内部 lock 边界**：未来在 `Unregister` 内不要调用其他持锁方法（参考现有 `namesLocked` 命名约定）。

---

**读过的关键文件**（绝对路径）：

- `D:\Bigmay\Projects\first-agentInK8s\internal\tools\registry.go`
- `D:\Bigmay\Projects\first-agentInK8s\internal\tools\calculator.go`
- `D:\Bigmay\Projects\first-agentInK8s\internal\tools\weather.go`
- `D:\Bigmay\Projects\first-agentInK8s\internal\tools\time.go`
- `D:\Bigmay\Projects\first-agentInK8s\internal\mcp\inproc\inproc.go`
- `D:\Bigmay\Projects\first-agentInK8s\internal\mcp\stdio\stdio.go`
- `D:\Bigmay\Projects\first-agentInK8s\internal\mcp\loader.go`
- `D:\Bigmay\Projects\first-agentInK8s\internal\agents\build.go`
