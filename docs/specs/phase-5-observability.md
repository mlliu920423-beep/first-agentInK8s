# Phase 5 — OTel trace + Langfuse 可观测性

> **Status**: Draft
> **Owner**: @mlliu920423-beep
> **Related ADR(s)**: ADR-009（待写）
> **Related feature branch / PR**: feat/observability（待创建）
> **Last updated**: 2026-07-19

## Context

**现状**：
- Phase 1-4 已完成，MVP 功能到位（MCP 声明式加载、Host 原子 swap、REST CRUD、前端配置 UI）
- 当前可观测性靠 `log.Printf` 和 Go 标准错误处理——无法追踪"一次请求从 HTTP 入口到 LLM 再到 tool 的全链路"
- 没有 trace，无法可视化 agent 路由决策、tool 调用耗时、LLM token 消耗

**痛点**：
1. **排障困难**：用户报告"某个问题答错了"，只能从日志里手动关联时间戳拼凑完整路径
2. **性能盲区**：不知道哪个 specialist 慢、哪个 tool 调用最贵、LLM 响应时间分布
3. **没有运营指标**：每天多少请求？多少 token？路由分布？tool 调用频次？全凭感觉
4. **Langfuse 已有 eino-ext 官方支持**：`github.com/cloudwego/eino-ext/callbacks/langfuse` 实现了 Eino 的 `callbacks.Handler` 接口，接入成本极低

**触发点**：MVP 已就位，现在需要为长期运营做准备。Phase 5 是"运维就绪"的关键一步。

## Goals

**Phase 5 成功标准**：

- **全链路 trace**：一次 chat 请求 → host LLM → specialist LLM → tool 调用的完整链路在 Langfuse 上可见
- **零代码侵入**：业务代码（`internal/agents/`、`internal/tools/`、`internal/httpapi/`）**不因加 trace 而改动**
- **环境可控**：通过环境变量控制是否启用、Langfuse 地址、认证信息
- **不影响现有功能**：trace 失败不阻塞请求，请求正常返回

### Non-Goals

- ❌ **Metrics / Prometheus** —— 只做 tracing，metrics 留给后续
- ❌ **OpenTelemetry 协议** —— 先用 Langfuse 的 callback handler，不走 OTel Collector
- ❌ **自定义 dashboard** —— 用 Langfuse 默认 UI
- ❌ **告警 / 通知** —— Phase 6
- ❌ **日志聚合**（ELK / Loki）—— Phase 6

## Options Considered

### Option A: Eino-ext Langfuse callback（选定）

**描述**：
- 使用 `github.com/cloudwego/eino-ext/callbacks/langfuse`
- 在 boot 流程中插入 Langfuse handler 注册
- 不改造现有业务代码

**Pros**：
- Eino 官方支持，接口稳定
- 只需要加 ~30 行代码，改动最小
- 支持 batch / sampling / retry / sensitive data masking
- 支持 per-request trace metadata（user_id, session_id, tags）

**Cons**：
- 依赖 Langfuse 云服务（需账号）
- 网络问题可能导致 trace 丢失（batch + retry 缓解）
- 不直接支持 OpenTelemetry 协议（无法对接 Jaeger / Tempo）

### Option B: OpenTelemetry SDK 直接集成

**描述**：
- 在 `internal/tracing/` 中手写 OTel span 创建
- 在关键路径（http handler、host router、specialist、tool）手动 start/end span
- 通过 OTel exporter 发送到 Jaeger / Tempo / Langfuse OTel endpoint

**Pros**：
- 协议中立，可以切换后端
- 细粒度控制 span 内容和属性

**Cons**：
- 大量手工埋点，业务代码侵入
- 需要知道 Eino 内部 Span 机制（文档不全）
- 与 Phase 1-3 的 "不加新依赖" 精神矛盾

### Option C: 只保留现有 log，不加 tracing

**描述**：
- 继续用 `log.Printf` 做所有调试
- 不引入任何 trace 系统

**Pros**：
- 零成本
- 零依赖

**Cons**：
- 无法解决"排障困难"和"性能盲区"两个核心痛点
- 长期运营不可持续

## Decision

**选定 Option A: Eino-ext Langfuse callback**

**理由**：
1. Eino 官方维护的 Langfuse 回调，接口稳定，适配成本最低
2. 零代码侵入业务逻辑——只需要在 boot 时注册 handler
3. 支持所有关键特性（batch、sampling、retry、per-request metadata）
4. 如果未来要换后端，只需换 handler 注册，业务代码不变
5. 项目规模（单二进制、单用户）下 Langfuse 云服务是最适合的 trace 后端

## Detailed Design

### 1. 新增模块：`internal/tracing/setup.go`

```go
// Package tracing wires up the Langfuse callback handler for Eino.
package tracing

import (
    "context"
    "fmt"
    "os"

    "github.com/cloudwego/eino/callbacks"
    "github.com/cloudwego/eino-ext/callbacks/langfuse"
)

// Setup initializes the Langfuse callback handler and registers it globally.
// Returns a flush function that must be called on server shutdown.
// If LANGFUSE_ENABLED is not "1", this is a no-op.
func Setup(ctx context.Context) (flush func(context.Context) error, enabled bool, err error) {
    if os.Getenv("LANGFUSE_ENABLED") != "1" {
        return nil, false, nil
    }

    host := os.Getenv("LANGFUSE_HOST")
    pk := os.Getenv("LANGFUSE_PUBLIC_KEY")
    sk := os.Getenv("LANGFUSE_SECRET_KEY")
    if host == "" || pk == "" || sk == "" {
        return nil, false, fmt.Errorf("LANGFUSE_ENABLED=1 but missing LANGFUSE_HOST/PUBLIC_KEY/SECRET_KEY")
    }

    cbh, flush, err := langfuse.NewLangfuseHandler(&langfuse.Config{
        Host:      host,
        PublicKey: pk,
        SecretKey: sk,
    })
    if err != nil {
        return nil, false, fmt.Errorf("langfuse: %w", err)
    }

    callbacks.AppendGlobalHandlers(cbh)
    log.Printf("tracing: Langfuse enabled, host=%s", host)
    return flush, true, nil
}
```

### 2. `cmd/server/main.go` 变化

在 step 4（InstallToolCallbacks）之后，step 5（HTTP server）之前插入：

```go
// 5. Tracing (Phase 5)
tracingFlush, tracingEnabled, err := tracing.Setup(ctx)
if err != nil {
    log.Printf("tracing: %v", err)
    os.Exit(1)
}
if tracingEnabled {
    defer func() { _ = tracingFlush(context.Background()) }()
}
```

### 3. Per-request trace metadata

在 `HandleChat` 里，可以设置 trace 的 session ID / user ID：

```go
// 在 HandleChat 里，创建 sink 之后，stream 之前
if tracingEnabled {
    ctx = langfuse.SetTrace(ctx,
        langfuse.WithSessionID(r.URL.Query().Get("session_id")),
        langfuse.WithMetadata(map[string]any{
            "method": r.Method,
            "path":   r.URL.Path,
        }),
    )
}
```

但这是可选的——**不实现也满足 acceptance criteria**。

### 4. 环境变量

| 变量 | 说明 | 默认 |
|---|---|---|
| `LANGFUSE_ENABLED` | 是否启用 trace | `""`（不启用） |
| `LANGFUSE_HOST` | Langfuse 服务地址 | `""` |
| `LANGFUSE_PUBLIC_KEY` | Langfuse 公钥 | `""` |
| `LANGFUSE_SECRET_KEY` | Langfuse 私钥 | `""` |

**默认不启用**，用户需要显式设置 `LANGFUSE_ENABLED=1` + 认证信息才生效。

### 5. 依赖变更

```bash
go get github.com/cloudwego/eino-ext/callbacks/langfuse
```

只加这一个依赖。`eino-ext/callbacks/langfuse` 内部依赖 `github.com/cloudwego/eino-ext/callbacks/callbackcommon`，这些是 eino-ext 自带，不需要额外引入。

## Acceptance Criteria

**功能验收**：
- [ ] 不设置 `LANGFUSE_ENABLED`，chat 功能正常，日志没有 trace 相关错误
- [ ] 设置 `LANGFUSE_ENABLED=1` + 有效认证信息，chat 后 Langfuse 上看到 trace
- [ ] trace 包含 host LLM / specialist LLM / tool 调用的完整 span 链
- [ ] `POST /api/reload` 后新的 trace 仍然正确（span 不因 Rebuild 而中断）

**质量验收**：
- [ ] `go build ./...` ✅
- [ ] `go vet ./...` ✅
- [ ] `go test ./...` ✅ 全绿
- [ ] `golangci-lint` CI 全绿 ✅

**文档验收**：
- [ ] STATUS.md 归档 Phase 5 完成状态
- [ ] ADR-009（Langfuse 可观测性方案）已写
- [ ] 环境变量表更新到 CLAUDE.md

## Risks & Tradeoffs

- **Langfuse 依赖**：云服务不可用或网络延迟可能导致 trace 丢失。**缓解**：batch + retry + sampling（Langfuse handler 原生支持），且 trace 失败不影响请求流程。
- **trace 开销**：每个 LLM 调用、tool 调用都要通过 callback 发送 span。**缓解**：Langfuse handler 支持 sampling（比如只 trace 10% 的请求），通过 `Config.SampleRate` 控制。
- **敏感数据**：trace 可能包含用户输入和 LLM 输出。**缓解**：Langfuse handler 支持 sensitive data masking，`Config.Mask` 字段可配置。
- **与 Rebuild 的交互**：`callbacks.AppendGlobalHandlers` 只能在启动时调一次。**不需要处理**——Phase 2 已明确此约束，`Supervisor.Rebuild` 不碰 callback。

## Out of Scope

- **Metrics / Prometheus** —— 留给 Phase 6
- **告警 / 通知** —— 留给 Phase 6
- **日志聚合**（ELK / Loki）—— 留给 Phase 6
- **自定义 dashboard** —— 用 Langfuse 默认 UI
- **OpenTelemetry Collector** —— 直接用 Langfuse 的 handler，不经过 Collector

## Open Questions

- [ ] **Sampling rate 默认值？** 100%（所有请求都 trace）还是 10%？**倾向**：默认 100%，如果有性能问题再降到 10%。
- [ ] **Langfuse Host 默认值？** 默认 `https://cloud.langfuse.com`（Langfuse 云服务），还是必须显式设置？**倾向**：必须显式设置，不设默认值，防止意外发送数据到生产环境。
- [ ] **Sensitive data masking？** 默认 mask 什么字段？**倾向**：默认不 mask，用户可以配置 `LANGFUSE_MASK=1` 启用。

## References

- **Eino-ext Langfuse callback**：https://github.com/cloudwego/eino-ext/tree/main/callbacks/langfuse
- **Langfuse 官网**：https://langfuse.com
- **Langfuse Go SDK**：https://pkg.go.dev/github.com/cloudwego/eino-ext/callbacks/langfuse
- **Eino Callback 文档**：https://github.com/cloudwego/eino
