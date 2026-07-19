# ADR 002: 用 Eino 而非 LangGraph / OpenAI Agents SDK 做多 agent 编排

> **Status**: Accepted
> **Date**: 2026-07-01（决策实际发生日；ADR 于 2026-07-16 回溯补记）
> **Owner**: @Bigmay
> **Related spec(s)**: —

## Context

项目要在 Go 里搭多 agent 编排框架，可选：

| 方案 | 语言 | 定位 | 状态 |
|---|---|---|---|
| **CloudWeGo Eino** | Go | 多 agent 编排 + tool call + MCP | 2025 中开源，字节内部大量在用 |
| **LangGraph** | Python | 图结构 agent workflow | 事实上的行业标准，但 Python |
| **OpenAI Agents SDK** | Python (主) / JS | 官方 agent 库 | 2025 春发布，绑 OpenAI 生态 |
| **自研** | Go | — | — |

约束条件：
- 后端语言**必须是 Go**（跟 [ADR-001](./001-monorepo-single-binary.md) 单二进制部署一致 + distroless 友好 + 作者更熟 Go）
- 模型 provider 是 **火山方舟 Ark**（国内可用、按量计费），OpenAI SDK 不适用
- 需要**多 agent 编排** + tool call + MCP 客户端集成，不想自己实现 ReAct 循环
- 学习目标之一：接触主流 agent 编排框架的模式

## Decision

**采用 Eino (`github.com/cloudwego/eino v0.9.12`) + `eino-ext`。**

具体版本：
- `eino v0.9.12` —— 主编排框架
- `eino-ext/components/model/ark v0.1.68` —— Ark ChatModel 适配器
- `eino-ext/components/tool/mcp v0.0.8` —— MCP client 适配器
- `mark3labs/mcp-go v0.55.1` —— MCP protocol 底层库

使用的核心抽象：
- `model.ToolCallingChatModel` —— 支持 tool call 的 chat model 接口
- `react.NewAgent` —— ReAct 循环包装（think → tool call → observe → repeat）
- `host.MultiAgent` + `host.Specialist` —— 一个 host LLM 路由到多个 specialist 的编排

## Consequences

### Positive

- **Go 原生**：跟单二进制部署 + distroless 目标完美对齐，不用为了 agent 框架换语言。
- **有开箱即用的 MCP 集成**：`eino-ext/components/tool/mcp` 直接把 MCP client 转成 Eino tool，`internal/mcp/*.go` 里的桥接层很薄（~100 行）。
- **Host + Specialist 抽象契合项目**：多 agent 骨架天然是"经理 + 下属"结构，Eino 的 `host.MultiAgent` 直接就是这个形状，不用自己组装图。
- **Callback 机制**：`host.WithAgentCallbacks` + `callbacks.AppendGlobalHandlers` 可以在 handoff / tool call 时打点，SSE 事件流和未来的 OTel trace 都基于这个。
- **字节大规模生产验证**：不是玩具项目，Eino 后端有活跃社区。

### Negative

- **API 稳定性**：v0.x 版本，API 仍在演化。已经踩过 `ToolCallingModel` interface 拆分的版本升级。将 Eino 版本 pin 在 `v0.9.12`，升级前必须过 evals。
- **文档相对 LangGraph 稀薄**：多数场景要读源码找答案；LangGraph 有大量 tutorial。
- **社区生态小**：MCP server / tool 生态是 Python + TS 主流，Go 侧只有 `mcp-go` 一家。
- **绑定字节风险**：如果 CloudWeGo 停维护，找替代要重写编排层。项目当前规模可承受重写，不是 blocker。

### Neutral

- Eino 的 graph 抽象（`compose.Graph`）比 LangGraph 更工程化（更接近 Kubernetes 风格的 DAG 声明），学习曲线偏陡但一旦上手代码组织清晰。

## Alternatives Considered

### Alternative 1: LangGraph（Python）

事实上的行业标准，但要求后端是 Python。

**为什么不选**：
- 需要引入 Python runtime → distroless 破功，image size 至少翻倍。
- 混排 Python 后端 + Go 二进制 embed 前端，部署复杂度骤增。
- 作者更熟 Go；学习目标不是"学 Python"，是"学 agent 工程"。

### Alternative 2: OpenAI Agents SDK

Python (主) / JS 有版本；官方支持度最高。

**为什么不选**：
- 强绑定 OpenAI ecosystem；国内 Ark 用不了 OpenAI API 兼容层的话，SDK 里大量假设失效。
- 同 LangGraph 的语言问题。

### Alternative 3: 自研

在 Go 里自己实现 ReAct 循环 + tool call + 路由。

**为什么不选**：
- 项目学习目标是"学工业级 agent 骨架"，不是"从零造轮子"。
- 造出来跟社区不兼容，后续接 MCP / 换模型都要自己维护适配层。
- Eino 已经把大部分抽象封装好，自研只有"控制感"上的收益。

### Alternative 4: 直接调 Ark SDK 手写编排

不用任何 agent 框架，`ark-go/v3` 客户端直接 chat completion + 手动 tool call 循环。

**为什么不选**：
- Tool call → 结果注入 → 再次 chat 的循环重复写 3 次（3 个 specialist）就是重复代码。
- 加 sub-agent 或 MCP 时没有稳定的抽象层落脚，每加一个都是新一堆胶水。

## Compliance / Validation

- `go.mod` 里的 Eino 版本**只降不升**（除非有 changelog 明说 breaking change 已处理）。CLAUDE.md "技术栈锁死清单" 明确记了 `v0.9.12`。
- 每次升级 Eino → 必须过 `evals/routing.yaml` 全绿再合入（跟当前 CI evals workflow 同一个门槛）。
- 遇到"Eino 里没有"的功能时，先看 `eino-ext` 有没有对应 component，再考虑自研；避免在 `internal/` 里长出 Eino 应该做的事。

## When to revisit

- Eino 长时间不维护（>6 个月无 release）
- Eino 出现 breaking API 且社区没跟上，升级成本 > 换框架成本
- 项目对多模态、更复杂的 agent 拓扑（graph / hierarchy）有需求，Eino 的 `host` 模式不够用（此时看 `compose.Graph` 或换框架）
- 火山方舟停售或大幅涨价，一并考虑换模型 provider 时顺带评估 agent 框架

## References

- ARCHITECTURE.md §5 (Tool Registry) / §6 (MCP 集成) / §7 (Agent 编排)
- CLAUDE.md "技术栈锁死清单"
- `internal/agents/build.go` (Host + Specialist 构造)
- `internal/llm/ark.go` (Ark ChatModel 适配)
- Eino 官方仓库：https://github.com/cloudwego/eino
- Eino 扩展包：https://github.com/cloudwego/eino-ext
