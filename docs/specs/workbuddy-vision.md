# Spec: workbuddy Vision & MVP Boundary

> **Status**: Approved
> **Owner**: @Bigmay
> **Related ADR(s)**: [ADR-001](../adr/001-monorepo-single-binary.md), [ADR-002](../adr/002-eino-as-orchestrator.md), [ADR-003](../adr/003-distroless-runtime.md), [ADR-004](../adr/004-branch-protection-on-main.md); 后续 ADR 005–010 待补
> **Related feature branch / PR**: `docs/init-engineering-flow`, PR TBD
> **Last updated**: 2026-07-16

## Context

**当前项目状态**（截至本 spec 撰写时）：`first-agentInK8s` 是一个 Eino 多 agent demo，跑在 Docker Desktop 内置 k8s 上，已完成基础工程化基线（CI / lint / evals / STATUS 同步）。CI run `29466566000` 6/6 全绿，是一个"能演示、能扩展、能回归测试"的**demo**。

**目标转换**：demo 已经完成学习使命。下一阶段项目**从 demo 演化为 workbuddy 类产品** —— 页面上让用户配置 subagent / skill / MCP 能力，并且**用它作为工业级 AI 开发实践的载体**：

- 每个大特性走 **spec → ADR → feature branch → PR → adversarial code review → eval → 合入** 的完整流程
- 前后端 / 编排 / 存储 / 观测 / 测试都亲手走一遍工业级模式
- 不追赶产品完成度，追赶实践深度

**为什么用本项目而不是新起一个**：
- 现成 Eino 骨架 + Ark 集成 + CI 已经跑通；起新项目要重来一遍浪费学习时间。
- 现有静态假设正好是很好的"改造靶子"（Registry 无 Unregister、Host 裸指针捕获、agents/*.yaml build-time COPY）—— 拆这些静态假设**就是**学工业级架构。
- 本项目已经积累了 STATUS / ARCHITECTURE / CLAUDE 三份跨会话文档 + 记忆索引，转型时不用重建上下文。

## Goals

### 产品目标（MVP）

**页面上让用户能做以下 3 件事**：

1. **管理 sub-agent**：新增 / 编辑 / 删除 sub-agent；改 name / description / system_prompt / 挂哪些 tool / max_step
2. **管理 MCP server**：启停内置的 in-proc MCP；配置外部 stdio MCP（`command` / `args` / `enabled_if`）
3. **管理 skill**（内置 Go tool）：启停内置 tool；覆盖其 description（影响 host 路由）

**改动生效方式**：编辑 → 保存 → 后端持久化 yaml + 触发 host 原子重建 → 下一次 `/api/chat` 请求走新配置。**不重启 pod**。

### 学习目标

**用完整流程走完这些工业实践一次**：

- Spec-driven development（每个特性先写 `docs/specs/<name>.md`）
- ADR（每个架构决策一个文件）
- Feature branch + PR + Code review（含 AI adversarial review）
- LLM eval 作为 CI gate（改 host prompt / description 后自动跑）
- OTel trace（Langfuse 或 Jaeger 看到 chat → host → specialist → tool 全链路）
- 声明式配置 vs 硬编码的边界演化（Registry / Host / 前端栈）
- 前后端 schema 双向生成（zod / JSON Schema）

## Non-Goals

**MVP 明确不做**（不代表未来不做，只是本次 vision 边界外）：

- **认证 / 授权**：不做 auth；API 无 user_id 校验；只在 struct 里预留字段
- **多租户 / 数据隔离**：所有用户共享同一份 `agents/*.yaml`；不做租户切分
- **计费 / 用量**：不 track token / 请求量
- **审计日志**：不记 who did what；只保留 git commit history 作为文档级审计
- **Prompt versioning**：改 host prompt 直接覆盖 yaml；不做版本 / 回滚
- **Model 切换**：所有 agent 共享 Ark 一个 endpoint；不做 per-agent model 或 provider 切换
- **前端登录页 / 用户 profile / 设置页**：只有 chat 页 + config 页三张
- **移动端 / 响应式**：桌面 web，viewport ≥ 1024px
- **国际化**：中文优先，英文界面待未来

## Options Considered

### Option A: 本项目直接演化

在 `first-agentInK8s` 现有代码基础上，一个 phase 一个 phase 拆静态假设、加 REST API、升前端栈。

**Pros:**
- 复用 Eino + Ark + CI + docs 基线
- 静态假设本身是很好的学习对象（每拆一个学一层工业级模式）
- 从"demo 骨架"到"产品雏形"的演化本身就是学习内容

**Cons:**
- 每次改动要小心不破坏 evals 全绿 baseline
- 前端从裸 React 升到 shadcn/Tailwind 会一次性打乱 App.tsx

### Option B: 新起项目 fork 现有代码

`workbuddy` 作为新仓库，从 `first-agentInK8s` fork，一次性重构架构（Registry 加 Unregister、Host 用 atomic pointer、REST API 完整、前端新栈），再补功能。

**Pros:**
- 不受"当前静态假设兼容"约束，架构可一次到位
- git history 干净

**Cons:**
- 重建 CI / STATUS / CLAUDE / 记忆索引，浪费学习时间
- 失去"每 phase 明显对比 before/after 的教学价值"

## Decision

**采用 Option A：本项目直接演化**，分阶段拆静态假设 + 逐特性走完整工业流程。

- 项目仓库 `mlliu920423-beep/first-agentInK8s` 保持不变
- `main` 分支设 branch protection，仅接受 PR
- 每个特性一个 feature branch，PR 内含 spec / ADR（如需）/ 代码 / eval 变化 / STATUS 更新

Phase 划分见 [plan file](../../.claude/plans/workbuddy-subagent-skill-mcp-ai-elegant-pillow.md)（当前会话决策），大意：

1. Phase 0 —— 元流程 setup（本 spec + 3 份回溯 ADR + PR 模板 + branch protection）
2. Phase 1 —— MCP 声明式加载（driver 层 + yaml loader + MCPManager）
3. Phase 2 —— Registry 可变 + Host 原子 swap（`Unregister/Replace/atomic.Pointer`）
4. Phase 3 —— REST API（`/api/agents` `/api/mcp` `/api/skills` CRUD + 持久化 + reload）
5. Phase 4 —— 配置 UI（shadcn/ui + Tailwind + react-router-dom）
6. Phase 5 —— OTel trace + Langfuse

## Detailed Design

### 用户流程草图（一句话 wireframe）

```
[/config/agents]
  ┌───────────────────────────────────────────┐
  │ Agents                        [+ New]     │
  ├───────────────────────────────────────────┤
  │ ▸ math_agent                              │
  │     "arithmetic problems only"            │
  │     Tools: calculator                     │
  │     [Edit] [Delete] [Disable]             │
  ├───────────────────────────────────────────┤
  │ ▸ research_agent                          │
  │     "for questions requiring lookup..."   │
  │     Tools: weather, mcp.echo, ...         │
  │     [Edit] [Delete] [Disable]             │
  └───────────────────────────────────────────┘

[/config/mcp]
  ┌───────────────────────────────────────────┐
  │ MCP Servers                   [+ New]     │
  ├───────────────────────────────────────────┤
  │ ⬤ demo (inproc)                Running    │
  │     Tools: mcp.echo, mcp.list_dir         │
  │     [Config] [Disable]                    │
  ├───────────────────────────────────────────┤
  │ ○ filesystem (stdio: npx ...)  Disabled   │
  │     enabled_if: env ENABLE_FS_MCP=1       │
  │     [Config] [Enable]                     │
  └───────────────────────────────────────────┘

[/config/skills]
  ┌───────────────────────────────────────────┐
  │ Built-in Skills (Go)                      │
  ├───────────────────────────────────────────┤
  │ ✓ calculator                              │
  │   Description: [Multiplies two floats.]  │← 可编辑覆盖，不改代码
  │   Used by: math_agent                     │
  ├───────────────────────────────────────────┤
  │ ✓ current_time                            │
  │   Description: [Returns current UTC ...]  │
  │   Used by: ops_agent                      │
  └───────────────────────────────────────────┘
```

### 数据模型（预期终态，Phase 3 实现）

```yaml
# agents/math_agent.yaml —— 现有 schema 保留
name: math_agent
description: |
  Handle arithmetic problems only.
system_prompt: |
  You are a specialised math assistant...
tools: [calculator]
max_step: 12
enabled: true              # ← 新增
user_id: ""                # ← 新增，预留产品化，MVP 全空
```

```yaml
# mcp/demo.yaml —— Phase 1 引入
name: demo
transport: inproc
provider: builtin-demo     # inproc 时对应 Go 代码里的 provider 名
enabled: true
enabled_if: ""             # 空 = 始终 enable

# mcp/filesystem.yaml
name: filesystem
transport: stdio
command: npx
args: ["-y", "@modelcontextprotocol/server-filesystem", "${env:FS_MCP_ROOT}"]
enabled_if: "env:ENABLE_FS_MCP=1"  # 环境不满足时不启动，不报错
```

```yaml
# skills/calculator.yaml —— Phase 3 引入（内置 tool 的 metadata 覆盖）
name: calculator
enabled: true
description_override: ""   # 空 = 用 Go 代码里的默认 description
```

### API 边界（预期终态）

```
POST   /api/agents          create
GET    /api/agents          list
GET    /api/agents/:name    read
PUT    /api/agents/:name    update
DELETE /api/agents/:name    delete

POST   /api/mcp             create
GET    /api/mcp             list
GET    /api/mcp/:name       read
PUT    /api/mcp/:name       update
DELETE /api/mcp/:name       delete
POST   /api/mcp/:name/restart

GET    /api/skills          list (read-only, Go tools 不能新增)
PUT    /api/skills/:name    update description override / enabled

POST   /api/reload          手动触发全局 reload（罕见用；正常 mutation 自动 reload）

GET    /api/chat            现有 SSE，不变
GET    /healthz             现有，不变
```

### 迁移路径

- **Phase 1 完成时**：项目仍是 demo 但 MCP 已声明式；`mcp/*.yaml` 目录出现；`ENABLE_FS_MCP=1` 语义不变
- **Phase 2 完成时**：Registry 可 mutate，但没有对外 API；只是 refactor
- **Phase 3 完成时**：**产品雏形出现** —— curl 可以配置 agent，前端还是 demo chat 页
- **Phase 4 完成时**：**MVP done** —— 页面能配 subagent / MCP / skill
- **Phase 5 完成时**：可观测性到位，为长期运营准备

## Acceptance Criteria

**MVP done = Phase 4 完成**，验收标准：

- [ ] 打开 `/config/agents` 能看到当前 3 个 agent 列表
- [ ] 点 [+ New] 能新增一个 agent，保存后 chat 页问对应问题会路由到新 agent
- [ ] 点某个 agent 的 [Edit] 修改 description，保存后**不重启 pod**，下一次 chat 路由行为立刻变化
- [ ] 点某个 agent 的 [Delete] 删除，chat 页问对应问题不再路由到它
- [ ] `/config/mcp` 页面能开启 / 关闭 `filesystem` MCP（本地开发有 npx 时）
- [ ] 所有 mutation 走 REST API，操作过程中现有正在跑的 SSE chat 不中断
- [ ] `evals/routing.yaml` 6/6 pass 保持不退化（在 Phase 3/4 添加相关 case，覆盖"配置改动后路由变化"）
- [ ] `main` 分支只有 PR 合入的 commit（除 Phase 0 建流程本身），每个 PR 有 spec / ADR / eval 变化
- [ ] `docs/adr/` 累积至少 10 份 ADR
- [ ] `docs/specs/` 累积至少 5 份特性 spec

## Risks & Tradeoffs

- **速度慢**：每个 phase 1-2 周量级，5 phase = 5-10 周才到 MVP。**接受**——学习优先。
- **静态 → 动态**改造有并发风险：Registry `Unregister` 时 in-flight 请求引用旧 tool，`host.MultiAgent` 原子 swap 时 in-flight chat 引用旧 host。**缓解**：ADR-005（Phase 2）明确"允许旧引用继续跑到完，不做 graceful drain"。
- **Host prompt 从 Go const 挪到 yaml** 违反当前 ARCHITECTURE.md 明确的约束（"改它是改设计"）。**缓解**：ADR-007（Phase 3）显式记录改主意的理由。
- **前端栈升级**（Phase 4 shadcn + Tailwind）一次性打乱 App.tsx。**缓解**：Phase 4 单独一个大 PR，不混其他改动。
- **workbuddy MVP 完成后**方向可能再转（例：真要做 SaaS 需要加 auth）。**缓解**：MVP 边界严格锁死，产品化留待新 vision spec。
- **CLAUDE 的角色转变**：从"帮跑命令的助手"变成"参与 PR review 的团队成员"。每次会话开头都要读 spec 而不只是 STATUS。可能需要在 CLAUDE.md 加"每次会话前必读 docs/specs/ 目录列表"的规矩。

## Out of Scope

以下未来可能做，本 spec 不承诺：

- Auth / SSO / RBAC（如未来对外 SaaS）
- Prompt versioning + A/B test
- Model provider abstraction（OpenAI-compat / Anthropic / 本地 vLLM）
- Multi-turn 对话 / session 持久化
- 内嵌 Monaco editor 让 skill / prompt 编辑有语法高亮
- 与外部 knowledge base 集成（Notion / Confluence / Slack）
- Agent 之间的直接对话 / debate 模式（当前是 host 单跳路由）
- eval 分布量化（跑 N 次算通过率，见 memory `llm-eval-needs-multiple-samples`）—— 会在 Phase 6+ 单独 spec

## Open Questions

- [ ] MCP secrets 在 yaml 里怎么表达？`${env:VAR}` 引用 vs 独立 secret store？（Phase 1 ADR-004 定）
- [ ] k8s 部署时 `agents/*.yaml` 挂 PVC vs ConfigMap vs Sidecar sync？（Phase 3 ADR-006 定）
- [ ] Host prompt 移出 Go const 后，如何保证"默认 prompt 有效"？（Phase 3 ADR-007 定）

## References

- Plan file: `~/.claude/plans/workbuddy-subagent-skill-mcp-ai-elegant-pillow.md`
- ADR-001 单二进制 / ADR-002 Eino / ADR-003 distroless（回溯，已完成）
- STATUS.md 2026-07-16 方向转换归档
- CLAUDE.md AI 助手做事的偏好（本 spec 完成后应更新，把"先写 spec / ADR"从建议提到硬规矩）
- ARCHITECTURE.md §13 (下一步扩展方向的着力点) —— workbuddy 演化的技术着力点
- MEMORY.md `llm-eval-needs-multiple-samples.md`（eval 分布量化的方向）
