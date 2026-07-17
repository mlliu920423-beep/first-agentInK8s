# ADR 005: MCP driver 抽象 + `mcp/*.yaml` 声明式加载

> **Status**: Accepted
> **Date**: 2026-07-17
> **Owner**: @Bigmay
> **Related spec(s)**: [phase-1-mcp-declarative-loading](../specs/phase-1-mcp-declarative-loading.md)、[workbuddy-vision](../specs/workbuddy-vision.md)

## Context

**触发**：workbuddy-vision Phase 1 主线 —— 把 MCP server 的选择、启用、配置从"硬编码在 Go 里"改为"页面 / yaml 可配置"，作为后续 REST API（Phase 3）+ 前端配置 UI（Phase 4）的地基。

**现状**（截至本 ADR 起草）：

| 位置 | 长什么样 |
|---|---|
| `internal/mcp/inproc.go` | `StartInProc` 硬编码构造 in-process demo MCP server（`echo` + `list_dir`） |
| `internal/mcp/filesystem.go` | `StartFilesystem` 硬编码 `npx @modelcontextprotocol/server-filesystem`；gate 在 `ENABLE_FS_MCP=1` env |
| `cmd/server/main.go:63-72` | 两者硬编码各调一次，warn-and-continue on error |

**问题（vision 视角）**：

1. **加新 MCP server = 改 Go 代码 + 重编 + 重部署**。跟 vision "页面上配 MCP" 的产品目标相反。
2. **enabled_if 逻辑内嵌 Go**：`filesystem.go` 里"没 npx / 没 env 就 skip"是硬写的三段 warn-and-continue，扩展第二个可选外部 MCP 就要复制粘贴一遍。
3. **配置分散**：想知道"这个部署跑了哪些 MCP"要读 Go 源码 + 环境变量组合，没有单一权威文件。
4. **语义模糊**：现在 `ENABLE_FS_MCP=0` / npx 不在 PATH / init 超时 —— 都走同一条 warn-and-continue 路径，日志上看不出"没启用"和"启用失败"的区别。

**约束**：

- Phase 1 是**纯后端 refactor**：SSE 事件 / Registry 接口 / 前端 / agents/*.yaml 都不动
- Phase 2 才做运行时可变（Registry `Unregister` / Host `atomic.Pointer` swap），Phase 1 只把"启动时读什么"从 Go 挪到 yaml
- 保留现有 `ENABLE_FS_MCP=1` 语义（向后兼容 `CLAUDE.md` / `dev.ps1` / 可能的其他脚本）
- evals/routing.yaml 6/6 必须保持绿（runtime 行为 0 变化承诺）

## Decision

**采用 driver 抽象 + `mcp/*.yaml` 声明式加载 + `enabled_if` 表达式，双层解耦。**

我们决定：

**1. 引入两层抽象**（`internal/mcp/`）：

- **Loader**（`loader.go`）：扫 `mcp/*.yaml` → 求 `enabled_if` → 分派 Driver → 收 closers。只调度，不 case 具体 MCP。
- **Driver interface**（`driver.go`）：`Start(ctx, cfg, reg) (io.Closer, error)`。一 transport 一实现：`inproc/`、`stdio/`。`init()` 里注册到全局 driver registry。

**2. 目录结构**（与 `agents/*.yaml` 对称）：

```
internal/mcp/
  config.go        # yaml → Go struct
  cond.go          # enabled_if + ${env:VAR:-default} 求值
  driver.go        # Driver interface + registry
  loader.go        # LoadAll
  inproc/inproc.go # inproc driver（内建 echo / list_dir，含 default_root）
  stdio/stdio.go   # 通用 stdio driver（不叫 filesystem，因为它只是 stdio 的一个实例）
mcp/
  demo.yaml        # inproc + default_root
  filesystem.yaml  # stdio + enabled_if: env:ENABLE_FS_MCP=1 + init_timeout: 30s
```

**3. `enabled_if` 表达式语法**（**故意做窄**，MVP 只 3 种）：

| 表达式 | 语义 |
|---|---|
| `""` 或 `"always"` | 恒真 |
| `"env:VAR"` | `os.Getenv("VAR") != ""` |
| `"env:VAR=v"` | `os.Getenv("VAR") == "v"` |

拼错（例：`env:FOO=BAR=X`）→ **启动 fail-fast**，不 warn-and-skip。理由：typo 静默跳过更难 debug。

**4. `${env:VAR}` / `${env:VAR:-default}` 插值** 支持在 `command` / `args` / driver 特定字符串字段里使用。`os.ExpandEnv` 不支持 `-` 默认值，自己写 20 行 mini-parser。

**5. 启动语义变化 —— fail-fast on startup error**：

| 情况 | 旧行为（warn-and-continue） | 新行为（Phase 1 起） |
|---|---|---|
| `enabled_if` 为 false | 硬编码 return nil, nil | 显式 log `mcp: X disabled (enabled_if=...)`，skip |
| yaml 拼错 / 未知 transport | (不适用) | 启动 fail |
| `enabled_if` 为 true 但 driver.Start 失败（binary 不存在 / init 超时 / 端口占用 …） | log warn 继续 | **启动 fail** → pod crashloop |

Rationale：yaml 声明"要跑"但没跑起来 = 配置和现实不一致，应该早报早死；k8s 场景里 pod crashloop 比"跑起来但工具缺失"更容易被监控发现。

**6. main.go 改动最小**：第 3 步从两次硬编码调用换成一次 `mcp.LoadAll(ctx, mcpDir, reg)`，`mcpDir` 从新增 env `MCP_DIR`（默认 `mcp`，容器里 `/mcp`）读取。跟 `AGENTS_DIR` 同模式。

**7. Dockerfile 新增一行** `COPY --from=go-builder /app/mcp /mcp`。

## Consequences

### Positive

- **加新 MCP server = 加 yaml**（如果 transport 已支持）—— 符合 vision "配置化"目标
- **加新 transport（未来 `sse` / `http`）= 加一个包**，loader 一行不改 —— driver interface 的核心价值
- **单一权威**：`mcp/*.yaml` 是"哪些 MCP 会跑"的唯一 truth
- **语义清晰**：`enabled_if=false` = 显式 disabled；`enabled_if=true` 但启动失败 = pod crashloop。日志能一眼分清
- **解决 STATUS 已知问题 #1 + #2**：
  - #1（`list_dir` 空）→ `mcp/demo.yaml` 里 `default_root: /agents` 之类的字段兜底
  - #2（filesystem MCP 容器无 npx）→ `enabled_if: env:ENABLE_FS_MCP=1` 一句话说清"什么时候启用"，容器里显式判定 disabled 而非 warn
- **为 Phase 3/4 铺路**：REST API 就是"接管 yaml 读写"，前端就是"yaml schema → 表单"

### Negative

- **代码量增加约 300 行**（driver interface、condition parser、变量插值、loader 逻辑、yaml struct、单元测试）。Cost 换的是 Phase 3/4 不用重写。
- **fail-fast 是行为语义变化**：以前 `ENABLE_FS_MCP` 环境下 filesystem MCP 起不来只是 log warn，现在会 pod crashloop。**接受**：CLAUDE.md 硬规矩 5 之后每次改路由 / prompt / 工具的 PR 都要跑 evals，crashloop 会立刻被发现。dev 环境如果 npx 不装，只要**不设** `ENABLE_FS_MCP=1` 就走 `enabled_if=false` 分支，跟以前一样；只有"声明启用但没装"才 crashloop —— 这正是我们想要的语义。
- **yaml schema 无编译期校验**：手写 typo 只在启动才发现。**缓解**：MVP 接受此代价；Phase 3 前端接管后表单校验兜底；单开 backlog "加 JSON Schema 校验"。

### Neutral

- **保留 `ENABLE_FS_MCP=1` env** 意味着 CLAUDE.md / dev.ps1 workflow 一字不改，但也保留了"env-based enable"这种偏"外部注入"的模式。Phase 4 前端到位时可以叠加一个 `enabled: true/false` 字段，`enabled_if` 保留给"部署差异"、`enabled` 表达"UI 开关"，两者语义清晰不冲突。
- **`init_timeout` 默认 30s**：沿用当前 `filesystem.go` 硬编码值，Phase 1 承诺 0 行为变化。未来若加第二个 stdio provider 且默认不合适再单独 ADR 讨论。
- **文件字典序**：`filepath.Glob` 按 Go 库文档保证的字典序返回，加载顺序稳定。tool.Registry 按 name 去重，顺序对最终 tool 集合无影响。

## Alternatives Considered

### Alternative 1: 顶层单文件 `mcp.yaml`

一份文件配所有 MCP：

```yaml
mcps:
  - name: demo
    transport: inproc
  - name: filesystem
    transport: stdio
    command: npx
    ...
```

**为什么不选**：

- 破坏"每 MCP 一份声明"的对称性（`agents/*.yaml` 是每 agent 一份）
- Phase 3 REST API 想 CRUD 单个 MCP 要读整文件、改内容、写回 —— 并发写风险 + merge 冲突
- 前端 Phase 4 "只 disable 一个 MCP" 也是同问题
- 现在方便一时，Phase 3/4 双倍利息。目录版几乎零多余工作量还前瞻

### Alternative 2: 保持硬编码，只把 `enabled_if` 参数化

不引入 driver 抽象、不引入 loader；只把 `StartInProc` / `StartFilesystem` 的"要不要启动"抽出来到 yaml，Go 代码里还留具体调用。

**为什么不选**：

- 违反 vision "加 MCP 不改 Go" 的**根本目标**
- 只把一半配置挪走，另一半（transport 类型 / provider 逻辑）还在 Go 里 —— 比现在更混乱
- Phase 2/3/4 到来时得推倒重做 —— 相当于两次成本
- 拧半只螺丝不如不拧

### Alternative 3: CEL / expr-lang 表达式引擎替代自研 `enabled_if` mini-parser

用 [google/cel-go](https://github.com/google/cel-go) 或 [expr-lang/expr](https://github.com/expr-lang/expr) 做 `enabled_if` 求值，支持复合表达式（`env:X=1 && cmd:npx exists`）。

**为什么不选**：

- MVP 场景 3 种表达式就够（部署差异不复合）
- 引入依赖 → 增加攻击面 + 学习成本 + 二进制体积（cel-go 有 protobuf 依赖链）
- **表达能力大 = 边界模糊**：给了 `&&` 就有人写业务逻辑，`enabled_if` 会腐烂成"配置里塞代码"的反模式
- 未来真需要复合表达式，另开 ADR，届时可以选自研简单二元操作符 vs 引入 CEL —— 现在预留决策空间比预定方案好

### Alternative 4: 用 `os.ExpandEnv` 做变量插值，不自研 mini-parser

Go 标准库 `os.ExpandEnv("$FOO")` 直接可用。

**为什么不选**：

- **不支持 `-` 默认值**（`${VAR:-default}`）—— 而 spec 需要 `${env:FS_MCP_ROOT:-.}` 这种"没设变量就用当前目录"的语义
- 我们的语法用 `${env:VAR}` 前缀显式区分"环境变量"和"未来可能加的其他 source"（例如 `${file:...}` / `${secret:...}`），跟 shell `$VAR` 混用不利于未来扩展
- 自研 20 行 mini-parser 是 acceptable 成本

## Compliance / Validation

**PR merge 前必过**：

- [ ] `go build ./... && go vet ./... && golangci-lint run` 全绿
- [ ] `internal/mcp/loader_test.go` + `cond_test.go` 覆盖：
  - 3 种 `enabled_if` 表达式 × true/false 分支
  - typo → fail-fast
  - unknown transport → fail-fast
  - Start 失败 → fail-fast
  - `${env:VAR:-default}` 有 / 无变量两种
- [ ] `go run ./cmd/evals -file evals/routing.yaml` 本地 6/6 不退化
- [ ] Docker build + `ctr import` + `kubectl rollout` 一遍：
  - 容器里 pod ready
  - `mcp.echo` / `mcp.list_dir` 可用（`list_dir` 返回非空，验 STATUS #1 修好）
  - `fs.*` 因 `ENABLE_FS_MCP` 未设显式 log `mcp: filesystem disabled (enabled_if=env:ENABLE_FS_MCP=1)`

**长期可执行的一致性检查**：

- `grep -rn "mcpbridge.Start" cmd/` → 应无匹配（只应有 `mcpbridge.LoadAll`）
- `ls mcp/*.yaml` → 每一份对应且仅对应一份运行时 MCP server
- 加新 MCP 的 PR 里如果**只改** `mcp/*.yaml`（不改 Go）—— 说明 driver + loader 分层活了；如果一直得改 Go —— 说明 driver 抽象泄露了，回来重看这个 ADR

## When to revisit

- **复合 `enabled_if` 表达式**真实需求出现（例如"仅当 X=1 且 Y 命令存在"）→ 需评估是否引入表达式引擎（重开 ADR）
- **加第三种 transport**（`sse` / `http`）时验证 driver interface 是否需要扩展签名 —— 如果需要 breaking change，本 ADR 需要 Superseded
- **Phase 4 前端接管**后决定是否弃用 `ENABLE_FS_MCP=1` env、改为纯 `enabled: bool` 字段（届时开一个新 ADR 描述迁移路径）
- **`init_timeout=30s`** 若在实测中被证明太长/太短导致 pod 启动慢或误 kill，重开小 ADR 讨论
- **inproc 的 `provider` 字段**未来若要动态注册（跟内建 tool 一样是 Go 侧注册就不用）—— 需要 ADR

## References

- [phase-1-mcp-declarative-loading spec](../specs/phase-1-mcp-declarative-loading.md) —— 本 ADR 是它的技术决策沉淀
- [workbuddy-vision.md](../specs/workbuddy-vision.md) §Phase 1 —— 上层目标
- `internal/mcp/inproc.go` / `internal/mcp/filesystem.go` —— 被重构的旧实现
- `cmd/server/main.go:60-72` —— 被替换的启动点
- `internal/agentcfg/` —— agents/*.yaml loader，对称参考
- STATUS.md 已知问题 #1 / #2 —— 本 ADR 顺带解决
- [MCP 官方 spec](https://spec.modelcontextprotocol.io/)
- ADR-002（Eino 作 orchestrator）—— MCP 是 Eino ToolNode 的一种 tool 来源，本 ADR 只调 registry 层，不动 orchestrator
