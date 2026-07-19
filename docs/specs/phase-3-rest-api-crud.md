# Phase 3 — REST API CRUD + 持久化 + Reload

> **Status**: In Review
> **Owner**: @mlliu920423-beep
> **Related ADR(s)**: ADR-007（`docs/adr/007-rest-api-persistence.md`）
> **Related feature branch / PR**: feat/rest-api-crud（待创建）
> **Last updated**: 2026-07-19

## Context

**现状**：
- Phase 1 已落地 MCP 声明式加载：`mcp/*.yaml` + `mcp.LoadAll()`
- Phase 2 已落地 `internal/agents/Supervisor`：`atomic.Pointer[host.MultiAgent]` + 事务化 `Rebuild()` + MCP closers 所有权
- 目前 `Rebuild()` 只在单元测试里被调用，**没有对外触发接口**
- Agents / MCP / Skills 的配置是**只读文件**，改配置需要编辑 yaml + 重建镜像 + 重启 pod

**痛点**：
1. **改配置必须重启 pod**—— k8s 里改 `agents/*.yaml` 需要重新 build 镜像（因为镜像里 `COPY agents/ /agents/` 是构建时拷入），即使本地开发也要 `^C` 再 `go run`
2. **Supervisor 的能力没有完全释放**—— `Rebuild()` 可以做到"热加载不中断 in-flight SSE"，但现在没人调它
3. **没有可编程接口**—— 想做自动化（比如 CI 测"加一个 agent 后路由是否正确"）或者 UI（Phase 4）都没法下手

**触发点**：Phase 1/2 连续合入 main，架构已到位，是时候把"内部能力"暴露成"外部接口"了。这是 workbuddy 从"demo"走向"可配置产品"的关键一步。

## Goals

**Phase 3 成功标准**：

- **零重启改配置**：`curl -X PUT /api/agents/math_agent` 之后，下一次 chat 路由行为立刻变化，不需要 `kubectl rollout restart`
- **配置持久化**：pod 重启后，通过 API 做的改动不丢失
- **现有 SSE 不中断**：`/api/agents POST` 正在跑的 `curl -N /api/chat?q=...` 不会断
- **向后兼容**：`agents/*.yaml` / `mcp/*.yaml` 的启动时语义不变——没有 API 调用时，行为跟 Phase 2 一模一样
- **幂等 + 事务**：API 调用失败不留下半成品状态

### Non-Goals

明确**Phase 3 不做**什么（留给后续 phase）：

- ❌ **Authentication / Authorization**（没有 JWT / API key / RBAC）—— 本地开发 + 单用户场景，Phase 4+ 再考虑
- ❌ **UI**（配置页面）—— 留给 Phase 4（shadcn/ui + Tailwind）
- ❌ **多租户**（每个用户一套配置）—— 始终单租户
- ❌ **Audit log**（谁什么时候改了什么）—— Phase 5+ 可观测性阶段
- ❌ **ConfigMap / PVC 集成**—— k8s 持久化留待单独 spec（当前先做文件系统持久化）
- ❌ **Host prompt API**（通过 API 改 Host 的 system prompt）—— Host prompt 目前还是 Go const，这个改法比较大，单独 Open Question 讨论
- ❌ **Skill CRUD**（`/api/skills` endpoint）—— 内置 tool 是 Go 代码写死的，没法动态增删；先做 Agents + MCP，Skills 延后到 Phase 3.5 或 Phase 4

## Options Considered

### Option A：内存状态 + 文件持久化

**描述**：
- Supervisor 内部维护"当前生效配置"（内存）
- 每个 CRUD 操作：先写文件 → 再 `Rebuild()` → 成功后更新内存
- 文件路径：`${AGENTS_DIR}/` / `${MCP_DIR}/`（跟启动时读的目录一样）
- API schema 严格对齐 yaml schema（字段名、类型一一对应）

**Pros**：
- 简单，不需要引入数据库依赖
- 文件格式跟现有 yaml 完全一致——可以手工编辑，也可以 API 编辑，双向兼容
- `kubectl cp` 可以备份/恢复配置
- 失败容易回滚：写文件失败就直接返回 500，不碰内存

**Cons**：
- 并发写的竞态（需要文件锁或 Supervisor 内部 mutex）
- 大文件写一半 crash 会留下损坏的 yaml（需要原子写：先写 `.tmp` 再 `rename`）
- k8s 里如果是容器根文件系统，pod 删除后配置丢失（需要用户手动挂 volume，或者后续 phase 做 ConfigMap sync）

### Option B：SQLite 单文件数据库

**描述**：
- 用 `modernc.org/sqlite`（纯 Go，无 CGO），存 `data/workbuddy.db`
- 三张表：`agents` / `mcp_servers` / `skills`，每行一个 yaml blob 或结构化字段
- 启动时：先读 DB，如果 DB 是空就 import 现有 yaml 进去

**Pros**：
- ACID 事务，不会有半写文件
- 并发安全（DB 自己管锁）
- 方便未来加 audit log / 版本历史 / 回滚功能

**Cons**：
- 引入 sqlite 依赖，单二进制体积变大（~2MB 增加）
- "用文件就能看"的透明度没了——需要 `sqlite3` CLI 才能 inspect 配置
- 手工改配置困难（不能直接编辑 yaml，必须走 API 或 sqlite CLI）
- 现有 yaml 怎么迁移？import 逻辑要写得健壮

### Option C：纯内存，不持久化

**描述**：
- 所有配置只在内存里，API CRUD 直接改内存 state
- 启动时只读一次 yaml，之后不写文件
- pod 重启，配置全丢

**Pros**：
- 最简单，完全不用考虑持久化的坑
- 可以先把 API 做出来，验证 UI / 自动化 workflow，持久化后续再加

**Cons**：
- "配置不丢"是用户基本预期——改完配置重启就没了，体验非常差
- Phase 4 UI 做完会骂娘（辛辛苦苦在 UI 配了 10 个 agent，一重启全没）
- 等后续加持久化时，API schema 可能要变，浪费一次迁移成本

### Option D：etcd / Redis 外部 KV 存储

**描述**：
- 起一个 sidecar 或者 external etcd/Redis，所有配置存在那里
- Go 侧只做 KV get/set

**Pros**：
- 真正的分布式持久化，k8s 多 pod 场景也能 work

**Cons**：
- 严重 overkill（当前是单 pod 本地开发）
- 引入网络依赖 + 额外部署复杂度
- 本地开发还要先 `docker run redis`，违背"单二进制零依赖"原则（ADR-001）

## Decision

**选定 Option A：内存状态 + 文件持久化**

**理由**：
1. **对齐 ADR-001 单二进制原则**—— 不引入 DB 依赖，保持 `go build` 零外部依赖
2. **透明度最高**—— 文件就是 yaml，手工编辑 / API 编辑双向兼容
3. **向后兼容**—— 没有 API 调用时，行为跟 Phase 2 完全一样（只读文件）
4. **失败模式简单**—— 写文件失败就 500，不碰内存；原子写（tmp → rename）避免半写
5. **跟 Phase 4 UI 配合最好**—— UI 可以读文件做初始状态，写文件做持久化
6. **k8s 挂 volume 就能解决"pod 删除配置丢失"问题**—— 不需要代码层面改架构

**为什么不选 B**：sqlite 的 ACID 收益被"透明度降低" + "手工编辑困难"抵消了。当前是单用户本地开发，并发写概率低，文件锁 + 原子写足够。如果未来做多用户 / 高并发，再考虑迁 sqlite。

**为什么不选 C**：纯内存体验太差，Phase 4 UI 会被这个决策拖累。现在就做持久化，API schema 一次定好。

**为什么不选 D**：overkill，违背单二进制原则。

## Detailed Design

### 1. 新增模块：`internal/configstore/`

**职责单一**：只做"配置文件 ↔ Go struct"的读写，不碰 Registry / Supervisor / MCP。

```go
// internal/configstore/store.go
type Store struct {
    agentsDir string  // = os.Getenv("AGENTS_DIR") 或 "agents/"
    mcpDir    string  // = os.Getenv("MCP_DIR") 或 "mcp/"
    mu        sync.Mutex  // 防并发写
}

// AgentConfig = agentcfg.AgentConfig 的拷贝（或者直接引用 agentcfg）
type MCPConfig = mcp.Config  // 直接引用 mcp.Config

// Agents CRUD
func (s *Store) ListAgents() ([]*agentcfg.AgentConfig, error)
func (s *Store) GetAgent(name string) (*agentcfg.AgentConfig, error)
func (s *Store) CreateAgent(cfg *agentcfg.AgentConfig) error  // 已存在 = 409
func (s *Store) UpdateAgent(name string, cfg *agentcfg.AgentConfig) error  // 不存在 = 404
func (s *Store) DeleteAgent(name string) error  // 不存在 = 404

// MCP CRUD
func (s *Store) ListMCPs() ([]*mcp.Config, error)
func (s *Store) GetMCP(name string) (*mcp.Config, error)
func (s *Store) CreateMCP(cfg *mcp.Config) error
func (s *Store) UpdateMCP(name string, cfg *mcp.Config) error
func (s *Store) DeleteMCP(name string) error
```

**文件写入原子性**：
- 所有写操作（Create/Update/Delete）都先写 `<name>.yaml.tmp`，再 `os.Rename()` 覆盖原文件
- Rename 在 POSIX / Windows NTFS 上都是原子的
- Delete 用 `os.Rename()` 挪到 `.trash/` 目录（软删除，方便手动回滚），而不是直接 `os.Remove()`

**并发安全**：
- 所有操作拿 `Store.mu` 互斥锁
- Supervisor 的 `rebuildMu` 跟这个是**两把锁**—— 拿锁顺序：先 `Store.mu` 写文件，再 `rebuildMu` 调 `Rebuild()`
- 避免锁顺序反转导致死锁

### 2. `Supervisor` 新增方法

Phase 2 的 `Supervisor` 只有 `NewSupervisor()` / `Current()` / `Rebuild()` / `Shutdown()`。Phase 3 加：

```go
// internal/agents/supervisor.go

// Reload 从磁盘重新读所有 yaml 并 Rebuild
// = 手工触发的热加载，对应 POST /api/reload
func (s *Supervisor) Reload(ctx context.Context) error

// GetAgents 返回当前生效的 agent 配置列表（从内存 state 读）
func (s *Supervisor) GetAgents() []*agentcfg.AgentConfig

// GetMCPServers 返回当前生效的 MCP 配置列表
func (s *Supervisor) GetMCPServers() []*mcp.Config
```

**注意**：`CreateAgent` / `UpdateAgent` / `DeleteAgent` 不在 Supervisor 层面做，而是在 `httpapi` 层做：
1. `httpapi` 调 `configstore.Store` 写文件
2. 再调 `Supervisor.Reload()` 让新配置生效
3. 这样 Supervisor 本身不需要知道"配置存在哪"，保持职责单一

### 3. API Endpoint

**路由前缀**：`/api/`（跟 `/api/chat` 同前缀）

**HTTP 方法严格 REST**：
- `GET    /api/agents` —— 列出所有 agent
- `GET    /api/agents/{name}` —— 取单个 agent
- `POST   /api/agents` —— 新建 agent（name 冲突 = 409）
- `PUT    /api/agents/{name}` —— 全量更新 agent（不存在 = 404）
- `DELETE /api/agents/{name}` —— 删除 agent（不存在 = 404）
- `GET    /api/mcp` —— 列出所有 MCP server
- `GET    /api/mcp/{name}` —— 取单个 MCP
- `POST   /api/mcp` —— 新建 MCP（name 冲突 = 409）
- `PUT    /api/mcp/{name}` —— 全量更新 MCP（不存在 = 404）
- `DELETE /api/mcp/{name}` —— 删除 MCP（不存在 = 404）
- `POST   /api/reload` —— 手动触发热加载（从磁盘重读 yaml 并 Rebuild）

**Request / Response Schema**：严格对齐 yaml schema（字段名、类型一一对应）。

#### Agent Request Body（POST / PUT）
```json
{
  "name": "math_agent",
  "description": "算术计算、数值运算、单位换算。路由到这里当用户问『等于多少』『帮我算』",
  "system_prompt": "You are Math Agent. Use calculator tool. Show your steps. Be concise.",
  "tools": ["calculator"],
  "max_step": 8
}
```

#### MCP Request Body（POST / PUT）
```json
{
  "name": "fs",
  "transport": "stdio",
  "enabled_if": "env:ENABLE_FS_MCP=1",
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-filesystem", "."],
  "env": {},
  "init_timeout": "30s"
}
```

**inproc transport 的字段**：
```json
{
  "name": "mcp",
  "transport": "inproc",
  "enabled_if": "always",
  "provider": "builtin-demo",
  "default_root": "/agents"
}
```

#### Response Codes

| 场景 | Code | Body |
|---|---|---|
| 成功 GET | 200 | JSON object |
| 成功 LIST | 200 | JSON array |
| 成功 POST（新建） | 201 | `{"status": "created", "name": "..."}` + `Location` header |
| 成功 PUT（更新） | 200 | `{"status": "updated", "name": "..."}` |
| 成功 DELETE | 200 | `{"status": "deleted", "name": "..."}` |
| 成功 POST /api/reload | 200 | `{"status": "reloaded", "took_ms": 42}` |
| name 不存在（GET/PUT/DELETE） | 404 | `{"error": "not found", "name": "..."}` |
| name 已存在（POST） | 409 | `{"error": "already exists", "name": "..."}` |
| 字段校验失败（必填缺失 / 类型错） | 400 | `{"error": "validation failed", "details": "..."}` |
| Rebuild 失败（POST 后 Rebuild 报错） | 500 | `{"error": "rebuild failed", "details": "..."}` |

**关键行为**：
- `POST /api/agents` 成功后，`Supervisor.Reload()` 自动被调用——下一个请求就会看到新 agent
- `PUT /api/agents/{name}` 也是一样——写文件 + 自动 Reload
- `DELETE /api/agents/{name}`：文件被挪到 `.trash/` 目录 + 自动 Reload
- `POST /api/reload` 是手动触发——比如用户手工编辑了 yaml 文件，不想重启 pod

### 4. Supervisor Rebuild 的错误处理

Phase 2 `Rebuild()` 失败时旧 host 完全不受影响。Phase 3 这个语义保持不变：
- API 调用写文件成功 → 调用 `Rebuild()`
- 如果 `Rebuild()` 失败（比如 yaml 语法错 / MCP 启动失败 / Specialist build 失败）：
  - API 返回 500 `{"error": "rebuild failed", "details": "..."}`
  - 旧 host 继续服务，**不会有停机时间**
  - 文件已经被改了—— 用户需要手工修复（或者 `POST /api/reload` 再试），或者 rollback PUT

**这个 tradeoff 是故意的**：
- 我们不做"先 dry-run build，成功才写文件"—— 因为 dry-run 需要起 MCP server，可能有副作用（比如 stdio driver 会 fork 子进程），而且 dry-run 跟真实 build 环境可能不一致
- "文件被改坏但服务还在跑" 比 "服务挂了" 强—— 至少还能通过 API rollback 或者手工修
- Phase 4 UI 可以加"先 validate 再 submit"的前端逻辑，降低用户手滑概率

### 5. 启动时行为

**向后兼容保证**：
- 启动时 `NewSupervisor()` 还是读 `${AGENTS_DIR}/*.yaml` + `${MCP_DIR}/*.yaml`，跟 Phase 2 一模一样
- 如果目录里没有 yaml，就空启动（跟现在一样）
- 如果有 `.trash/` 目录，启动时忽略它—— 不会把已删除的配置读回来

### 6. 目录结构变化

```
agents/
  ├── math_agent.yaml
  ├── ops_agent.yaml
  ├── research_agent.yaml
  └── .trash/          ← 新增：已删除文件的回收站
       └── old_agent.yaml.20260719-143022  # 带时间戳，避免重名覆盖

mcp/
  ├── mcp.yaml
  ├── filesystem.yaml
  └── .trash/          ← 新增：已删除 MCP 配置
```

**Dockerfile 不需要改**—— `.trash/` 目录是运行时创建的，不需要构建时存在。

### 7. 迁移路径

- **现有用户**：不需要做任何事—— 原来的 `agents/*.yaml` / `mcp/*.yaml` 继续 work
- **第一次 API 调用**：会自动创建 `.trash/` 目录（如果不存在）
- **配置格式**：100% 兼容现有 yaml，不需要迁移脚本

## Acceptance Criteria

**功能验收**：
- [ ] `GET /api/agents` 返回 3 个现有 agent（math / ops / research）
- [ ] `POST /api/agents` 新建一个 agent（`test_agent`），再 `GET` 能看到它
- [ ] `PUT /api/agents/test_agent` 修改 description，`POST /api/reload` 后，chat 能路由到新 agent
- [ ] `DELETE /api/agents/test_agent` 删除后，`GET` 返回 404，chat 不再路由到它
- [ ] MCP 同上：`GET /api/mcp` / `POST` / `PUT` / `DELETE` 全流程 work
- [ ] `POST /api/reload` 后 Supervisor 日志显示 rebuild 完成 + took_ms 合理
- [ ] 配置改动时，正在跑的 SSE chat 不中断（并发测试：一边 `curl -N /api/chat` 一边 `POST /api/agents`）
- [ ] pod 重启后，API 新建的配置不丢失（手工验证：`^C` → `go run` → `GET /api/agents` 还在）

**质量验收**：
- [ ] `go build ./...` ✅
- [ ] `go vet ./...` ✅
- [ ] `go test ./...` ✅ 全绿，含 configstore 单元测试 + 并发读写测试
- [ ] `golangci-lint` CI 全绿
- [ ] `evals/routing.yaml` 6/6 pass 保持不退化
- [ ] 新增 2+ 条 eval case：覆盖"新建 agent 后路由正确" + "删除 agent 后路由 fallback"

**文档验收**：
- [ ] STATUS.md 归档 Phase 3 完成状态
- [ ] ADR-007（持久化方案 + API 设计决策）已写
- [ ] ARCHITECTURE.md 新增 § REST API Layer 描述，更新目录树

## Risks & Tradeoffs

- **并发写竞态**：两个 `PUT` 同时写同一个文件。**缓解**：`Store.mu` 互斥锁，同一时间只能一个写。
- **Rebuild 期间请求积压**：Rebuild 可能要几百 ms（起 MCP 子进程 + build specialists），这段时间的 `Current()` 还是旧 host，不会阻塞请求。**没问题**—— atomic pointer swap 是瞬间的，中间几百 ms 继续用旧 host。
- **yaml 格式兼容问题**：API 写的 yaml 跟手工编辑的格式不一样（缩进 / 字段顺序）。**缓解**：用同一个 yaml encoder（`gopkg.in/yaml.v3`），跟 `agentcfg` / `mcp` 读取时的 decoder 一致。
- **删除后不可恢复**：用户手滑删了 agent 想找回来。**缓解**：Delete 是软删除，挪到 `.trash/` 带时间戳，用户可以手工从 trash 拷回来。Phase 4 UI 可以加"回收站"功能。
- **MCP 更新时旧子进程泄漏**：`PUT /api/mcp/fs` 会触发 Rebuild，Rebuild 时旧 MCP driver 会被 Close（有 grace period 30s）。**没问题**—— Phase 2 Supervisor 已经处理了这个问题。

## Out of Scope

这次明确不做，记录下来：

- **PATCH 方法**—— 只做 PUT 全量更新，不做部分字段 PATCH。简化实现，Phase 4 UI 也只做全量编辑。
- **版本历史 / 回滚 API**—— 软删除到 `.trash/` 是手工回滚，API 层面不提供 `GET /api/trash` / `POST /api/restore`。未来可以加。
- **批量操作**（`POST /api/agents/batch`）—— 单次操作足够，批量暂时不需要。
- **Host prompt API**（`PUT /api/host/prompt`）—— Host prompt 还是 Go const，这个改动比较大，单独讨论。
- **Skill CRUD**（`/api/skills`）—— 内置 tool 是 Go 代码，没法动态增删。未来如果做"自定义 skill"（比如让用户写一段 Go 代码或者 prompt 作为 skill），再加这个 endpoint。
- **文件 watcher**（自动 detect yaml 变化并 reload）—— 手工触发 `POST /api/reload` 足够，fsevent 跨平台有坑，延后做。

## Open Questions

~~所有 Open Questions 已在 ADR-007 明确决定，review 阶段无遗留：~~

- [x] **Host prompt 是否也要做成可配置？** → **Phase 3 不做，留待 Phase 3.5**（ADR-007 § Decision 3.1）
- [x] **Delete 是否要做硬删除选项？** → **Phase 3 只做软删除**（ADR-007 § Decision 3.2）
- [x] **k8s 里配置持久化怎么做？** → **Phase 3 不解决，文档说明"请挂 PVC"**（ADR-007 § Decision 3.3）
- [x] **API 返回结果要不要带 `etag` / `last_modified`？** → **Phase 3 不加**（ADR-007 § Decision 3.4）
- [x] **`/api/reload` 要不要支持 dry-run？** → **Phase 3 不加**（ADR-007 § Decision 3.5）

## References

- **Phase 2 Supervisor 实现**：`internal/agents/supervisor.go`
- **MCP Config schema**：`internal/mcp/config.go`
- **Agent Config schema**：`internal/agentcfg/loader.go`
- **Vision spec Phase 3 描述**：`docs/specs/workbuddy-vision.md` § Phase 划分
- **ADR-001 单二进制原则**：`docs/adr/001-monorepo-single-binary.md`
- **ADR-006 Supervisor 设计决策**：`docs/adr/006-registry-mutation-host-swap.md`
