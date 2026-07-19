# ADR 007: REST API + 文件系统持久化

> **Status**: Proposed
> **Date**: 2026-07-19
> **Owner**: @mlliu920423-beep
> **Related spec(s)**: Phase 3 REST API CRUD spec (`docs/specs/phase-3-rest-api-crud.md`)

## Context

**张力 1：内部能力 vs 外部接口**
- Phase 2 已经落地 `Supervisor.Rebuild()`—— 事务化原子 swap，in-flight SSE 不中断，MCP closers 优雅关闭
- 但这个能力**只在单元测试里被调用**，外部没有触发入口
- 改配置必须编辑 yaml + 重建镜像 + 重启 pod，体验差

**张力 2：持久化方案的 tradeoff**
- 单二进制原则（ADR-001）要求零外部依赖，但"配置不丢"是用户基本预期
- 可选方案光谱：纯内存（无持久化）→ 文件系统 yaml → sqlite → etcd/Redis
- 每个方案在"透明度 / 手工编辑友好"、"ACID 保证"、"依赖重量"三个维度各有取舍

**张力 3：Host prompt 的契约边界**
- ARCHITECTURE.md § 7 明确说"Host prompt 是 Go const，改它意味着改设计"
- 但 workbuddy 作为"可配置产品"，用户可能想自定义路由逻辑（比如"优先路由到新加入的 agent"）
- 是守住"Host = 核心拓扑，不可配置"的边界，还是放开？

**约束条件（来自已落地 ADR）**：
- ADR-001：单二进制，零外部依赖
- ADR-005：MCP fail-fast 语义（enabled_if=true 但启动失败 = pod crashloop）
- ADR-006：Supervisor Rebuild 事务化，失败不动旧状态；`AppendGlobalHandlers` 只在 `main.go` 调一次

## Decision

我们决定：

### 1. **REST API 暴露 CRUD + Reload**
- 11 个 endpoint：`/api/agents` (5) + `/api/mcp` (5) + `/api/reload` (1)
- CRUD 操作先写文件 → 再自动 `Reload()` → 失败返回 500 但服务不中断
- HTTP 方法严格 REST：GET/POST/PUT/DELETE，不做 PATCH

### 2. **文件系统 yaml 持久化**
- 跟启动时读的是同一批文件：`${AGENTS_DIR}/*.yaml` + `${MCP_DIR}/*.yaml`
- 原子写：先写 `.tmp` → `os.Rename()` 覆盖，避免半写文件
- 软删除：Delete 挪到 `.trash/` 目录带时间戳，不直接 `os.Remove()`
- 并发控制：`internal/configstore.Store.mu` 互斥锁，同一时间只能一个写

### 3. **Open Questions 明确回答（本次就定）**

| # | 问题 | 决策 | 理由 |
|---|---|---|---|
| 1 | Host prompt 是否也要做成可配置？ | **Phase 3 不做，留待 Phase 3.5** | Host prompt 是"经理怎么分配工作"的核心逻辑，目前 routing 质量还在迭代（evals 还只有 6 条 case），先别放开让用户改乱。等 Phase 4 UI 上线后，观察用户是否真的有自定义路由的需求再说。 |
| 2 | Delete 是否要做硬删除选项？ | **Phase 3 只做软删除** | `DELETE /api/agents/{name}` = 挪到 `.trash/` 带时间戳。硬删除（`?hard=true`）留给 Phase 3.5 或 Phase 4 UI 回收站功能。 |
| 3 | k8s 里配置持久化怎么做？ | **Phase 3 不解决，文档说明"请挂 PVC"** | 容器根文件系统会随 pod 删除而丢失，这是 k8s 基础知识，不是 workbuddy 的责任。文档里加一句"k8s 部署请给 `/agents` 和 `/mcp` 目录挂 PVC"即可。ConfigMap sync 留给后续 phase。 |
| 4 | API 返回结果要不要带 `etag` / `last_modified`？ | **Phase 3 不加** | 单用户本地开发，并发冲突概率极低。乐观并发控制是多用户场景才需要的，现在 overkill。 |
| 5 | `/api/reload` 要不要支持 dry-run？ | **Phase 3 不加** | dry-run 需要起 MCP 子进程，有副作用且实现复杂。"Rebuild 失败但服务不中断"的语义已经足够安全——用户可以再 PUT 一个正确的配置，或者手工修 yaml 再 `POST /api/reload`。 |

## Consequences

### Positive

- ✅ **零重启改配置**—— curl 就能改 agent / MCP，下一个请求生效
- ✅ **手工编辑 / API 编辑双向兼容**—— 文件就是 yaml，用户可以 vim 改再 `POST /api/reload`
- ✅ **严格对齐 ADR-001 单二进制原则**—— 不引入 sqlite / etcd 依赖
- ✅ **向后兼容**—— 没有 API 调用时，行为跟 Phase 2 一模一样
- ✅ **失败模式简单**—— Rebuild 失败不影响服务，旧 host 继续跑
- ✅ **软删除兜底**—— 用户手滑删了配置可以从 `.trash/` 手工恢复

### Negative

- ❌ **文件锁只能防单进程内的并发写**—— 如果两个进程同时写同一个文件（比如本地 `go run` 两个实例），还是可能竞态。**接受**—— 单用户场景，不会同时跑两个实例。
- ❌ **k8s 不挂 PVC 会丢配置**—— 需要文档明确提醒用户。**接受**—— 这是 k8s 基础常识。
- ❌ **没有 PATCH，全量 PUT 有点冗余**—— UI 要先 GET 再 PUT，但实现复杂度低。**接受**—— Phase 4 UI 可以在前端做 diff。
- ❌ **没有版本历史 / 一键回滚**—— 只能手工从 `.trash/` 拷回来。**接受**—— Phase 4 UI 再加回收站功能。

### Neutral

- `configstore` 是新增模块，`httpapi` 层会变厚（从 2 个 endpoint 变成 13 个）—— 正常架构演进。
- `Supervisor` 新增 3 个 public method（`Reload` / `GetAgents` / `GetMCPServers`），表面积变大—— 但都是读操作或幂等操作，风险可控。
- evals  runner 要加 2 条新 case（新建 agent + 删除 agent）—— eval 集变大是好事，覆盖率更高。

## Alternatives Considered

### Alternative 1: SQLite 单文件数据库

**描述**：用 `modernc.org/sqlite`（纯 Go，无 CGO），三张表（agents / mcp_servers / skills），每行存结构化字段或 yaml blob。

**为什么不选**：
- 引入 ~2MB 依赖，违反 ADR-001"最小依赖"精神（虽然还是单二进制）
- 透明度降低——用户不能再用 vim 直接看配置，必须装 `sqlite3` CLI
- 手工编辑困难——不能直接改 yaml，必须走 API 或写 SQL
- 收益（ACID 事务）被单用户场景稀释——并发冲突概率极低，文件锁 + 原子写足够
- 现有 yaml 要写 import 逻辑，迁移成本不低

**如果未来出现以下情况可以重开这个决策**：多用户 / 高并发 / 需要版本历史 / 需要 audit log。

### Alternative 2: 纯内存不持久化

**描述**：所有配置只在内存，启动时只读一次 yaml，之后不写文件，pod 重启配置全丢。

**为什么不选**：
- "配置不丢"是用户基本预期——辛苦配了 10 个 agent，一重启全没，体验极差
- Phase 4 UI 会被这个决策拖累——用户不敢在 UI 里认真配置
- 等后续加持久化时，API schema 可能要变，浪费一次迁移成本
- 唯一好处是"实现简单"，但被用户体验代价完全抵消

### Alternative 3: etcd / Redis 外部 KV 存储

**描述**：起一个 sidecar 或者 external etcd/Redis，所有配置存在那里。

**为什么不选**：
- 严重 overkill—— 单 pod 本地开发不需要分布式存储
- 引入网络依赖 + 额外部署复杂度
- 本地开发还要先 `docker run redis`，违背 ADR-001"单二进制零依赖，go run 就能跑"的原则
- 部署文档变厚，新手入门门槛变高

### Alternative 4: 只做 `/api/reload`，不做 CRUD

**描述**：只暴露一个 reload 接口，让用户手工编辑 yaml 后 `POST /api/reload` 生效，不做 CRUD。

**为什么不选**：
- 这是 Phase 2.5，不是 Phase 3—— 只解决了"不重启"，没解决"可编程"和"UI 可配置"
- workbuddy vision 的目标是"页面上能配 subagent / MCP / skill"，CRUD 是必要前提
- 只做 reload 会让 Phase 4 UI 还是要自己写 yaml，体验断层

## Compliance / Validation

怎么知道这个决策被代码贯彻：

1. **`go mod graph | grep sqlite` 应该为空**—— 不能引入 sqlite 依赖
2. **所有写文件操作必须先写 `.tmp` 再 `os.Rename()`**—— code review 关注点，或者加 lint rule
3. **Delete 不能直接 `os.Remove()`**—— 必须挪到 `.trash/` 带时间戳
4. **`configstore` 所有 public method 拿 `mu.Lock()`**—— 并发安全检查
5. **Host prompt 还是 Go const**—— 不能出现 `GET /api/host/prompt` endpoint（除非 Phase 3.5 ADR 重开决策）

## When to revisit

以下条件出现时，应该重开这个 ADR：

1. **需要多用户支持**—— 单进程文件锁不够用，要考虑 sqlite 或数据库
2. **需要版本历史 / 一键回滚 / audit log**—— 文件系统搞不定，要数据库
3. **k8s 部署场景变多**—— 用户频繁抱怨"pod 重启配置丢了"，要考虑 ConfigMap sync 或 Operator 模式
4. **用户强烈要求自定义 Host prompt**—— 要重开"Host prompt 可配置"的决策
5. **并发写冲突真的发生了**—— 文件锁不够用，要考虑乐观并发控制（etag）或数据库

## References

- **Phase 3 spec**：`docs/specs/phase-3-rest-api-crud.md`
- **ADR-001 单二进制原则**：`docs/adr/001-monorepo-single-binary.md`
- **ADR-006 Supervisor 设计**：`docs/adr/006-registry-mutation-host-swap.md`
- **Supervisor 实现**：`internal/agents/supervisor.go`
- **MCP Config schema**：`internal/mcp/config.go`
- **Agent Config schema**：`internal/agentcfg/loader.go`
- **POSIX rename atomicity**：https://pubs.opengroup.org/onlinepubs/9699919799/functions/rename.html
- **Windows NTFS atomic rename**：https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-movefileexa
