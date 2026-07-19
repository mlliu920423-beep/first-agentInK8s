# <Spec Title>

> **Status**: Draft | In Review | Approved | Implemented | Superseded
> **Owner**: @<github-username>
> **Related ADR(s)**: <adr-id>, ...
> **Related feature branch / PR**: <branch-name>, PR #<n>
> **Last updated**: YYYY-MM-DD

## Context

<为什么现在要做这件事。问题是什么？痛点在哪？现状（当前代码/流程）如何？什么触发了这份 spec —— 是用户反馈、性能问题、扩展性瓶颈，还是新战略方向？>

## Goals

<这个特性成功了长什么样。**具体、可验证**。>

- <目标 1>
- <目标 2>
- <目标 3>

### Non-Goals

<明确列**不做**什么，比 goals 更能防止 scope creep>

- <非目标 1>
- <非目标 2>

## Options Considered

### Option A: <标题>

<描述 A 方案。改哪些文件 / 引入什么新概念 / 用户界面如何变化。>

**Pros:**
- ...

**Cons:**
- ...

### Option B: <标题>

...

### Option C: <标题>（可选）

...

## Decision

<选定 X。**为什么** —— 结合 goals 和 tradeoffs 讲清楚。>

## Detailed Design

<选定方案的实现细节。>

### 数据模型 / Schema

<新的 struct / yaml schema / DB table。给出具体字段和类型。>

### API 变化

<新加或修改的 endpoint / function signature。>

### 迁移路径

<对现有代码/数据/用户如何过渡。>

## Acceptance Criteria

<**验收 checkbox**，实现完这些勾选完 spec 就 close。>

- [ ] <可测试 / 可观察的验收点 1>
- [ ] <验收点 2>
- [ ] CI 全绿
- [ ] evals routing 6/6 不退化（如涉及 host / agent）
- [ ] STATUS.md 归档
- [ ] 相关 ADR 已加（如涉及架构决策）

## Risks & Tradeoffs

<已知的风险、后续可能付出的代价、埋的技术债。>

- **风险 1**：<描述>。**缓解**：<怎么应对>。
- **Tradeoff 1**：<做了 X 就没法做 Y，接受这个>。

## Out of Scope

<明确"这次不做，但记录一下将来可能要做的"，避免 review 时被反复问>

- <未来可能：...>

## Open Questions

<写完还没定的问题，留给 review 阶段收敛>

- [ ] <问题 1>
- [ ] <问题 2>

## References

- <相关代码：`internal/foo/bar.go:123`>
- <外部资源 URL>
- <前置 spec / 相关 issue>
