# Phase 1 Spec: MCP 声明式加载

> **Status**: Accepted
> **Owner**: @Bigmay
> **Related ADR(s)**: ADR-005（MCP driver 抽象 + 声明式 loader）
> **Related feature branch / PR**: `feat/mcp-declarative-loading`, PR TBD
> **Last updated**: 2026-07-17（用户 review 通过，转 Accepted；Open Questions 全 close）

## Context

**触发**：`docs/specs/workbuddy-vision.md` 的 Phase 1 主线。

**现状**（`internal/mcp/`）：
- `StartInProc` 硬编码在 Go 里，构造 in-process demo MCP server（`echo` / `list_dir` 两个工具）
- `StartFilesystem` 硬编码外部 stdio MCP（`npx @modelcontextprotocol/server-filesystem`），gate 在 `ENABLE_FS_MCP=1` env
- 两者都在 `cmd/server/main.go` 第 3 步硬编码调用一次

**痛点**：
1. **加新 MCP server = 改 Go 代码** —— 违反 workbuddy vision"页面上配 MCP"的产品目标
2. **enabled_if 逻辑内嵌在 Go** —— filesystem MCP 的"没 npx / 没 env 就跳过"是硬写的 warn-and-continue，扩展第二个外部 MCP 就得复制粘贴
3. **配置分散** —— 想知道"这个部署跑了哪些 MCP" 得读 Go 源码 + 环境变量组合，没有单一权威文件
4. **STATUS.md 已知问题 #1 / #2** 目前是"warn-and-continue 加日志"，语义模糊 —— 到底是"没启用"还是"启用失败"？yaml 加 `enabled_if` 显式表达能一句话说清

**约束**：
- 不改 SSE 事件 / Registry 接口 / 前端 —— Phase 1 是**纯后端 refactor**，产品行为对用户 0 变化（同 agent、同 tool 名字、同路由）
- Phase 2 会做 Registry `Unregister` + Host atomic swap；Phase 1 **不做**运行时可变，只把"启动时读什么"从 Go 挪到 yaml
- 保留现有 `ENABLE_FS_MCP=1` env 语义（向后兼容 CLAUDE.md / local dev workflow / 有可能的其他脚本）

## Goals

- **加新 MCP server 不改 Go**：只加 `mcp/<name>.yaml` 就够（如果该 transport 类型已支持）
- **`enabled_if` 声明式表达**：yaml 里明说"什么条件下起用"，运行时按声明判断，不写代码
- **Driver 抽象**：新增 transport 类型（比如未来 `sse` / `http`）只加 driver，不动 loader
- **单一权威**：`mcp/*.yaml` 是"哪些 MCP 会跑"的唯一 truth，`cmd/server/main.go` 只调 loader，不 case 具体 MCP
- **顺带解决 STATUS 已知问题**：
  - #1（`list_dir` 空）→ inproc yaml 里可配 `default_root`
  - #2（fs MCP 容器没 npx）→ `enabled_if` 表达式一句话说清"什么时候启用"，容器里显式判定为 disabled 而非"warn-and-continue"

### Non-Goals

**Phase 1 明确不做**（留给后续 phase）：

- **运行时可变**：不加 API、不做 hot reload、不做 `Unregister` —— 这些是 Phase 2/3 主线
- **`agents/*.yaml` 变化**：现有 3 份 agent yaml 不改 schema，仍然按 tool name 引用 `mcp.echo` 之类
- **前端**：`/config/mcp` 页面是 Phase 4
- **REST API**：`/api/mcp` CRUD 是 Phase 3
- **skill yaml**：`skills/<name>.yaml` 是 Phase 3
- **持久化写回**：Phase 1 只**读** yaml，不写回；运行时"手编辑 yaml 重启"是唯一 mutation 路径
- **认证 / secrets 抽象**：`${env:VAR}` 引用够用；vault / secret store 不做（vision Non-Goals）

## Options Considered

### Option A: 每 MCP 一个 driver + 顶层 loader（推荐）

**结构：**
```
internal/mcp/
  driver.go             # Driver interface + Registry
  loader.go             # 扫 mcp/*.yaml → 选 driver → Start → 挂到 tool.Registry
  cond.go               # enabled_if 表达式解析（"env:VAR=value" / "cmd:name" / "always"）
  inproc/               # driver: in-process demo MCP
    inproc.go
    tools.go            # 内置 echo / list_dir 定义（从旧 inproc.go 挪过来）
  stdio/                # driver: external stdio MCP（当前只有 filesystem 用）
    stdio.go
```

**yaml schema：**

```yaml
# mcp/demo.yaml
name: demo
transport: inproc
provider: builtin-demo        # inproc driver 里注册的 provider 名（内建 echo/list_dir）
enabled_if: "always"          # 或省略默认 always
```

```yaml
# mcp/filesystem.yaml
name: filesystem
transport: stdio
command: npx
args: ["-y", "@modelcontextprotocol/server-filesystem", "${env:FS_MCP_ROOT:-.}"]
enabled_if: "env:ENABLE_FS_MCP=1"
init_timeout: 30s
```

**`enabled_if` 支持的表达式（MVP 只做这三种，够 P1 用）：**
| 表达式 | 语义 |
|---|---|
| `always` 或空 | 恒真 |
| `env:VAR=value` | 环境变量 VAR == value |
| `env:VAR` | 环境变量 VAR 非空 |

不做通用表达式引擎（比如 `env:X=1 && cmd:npx`）—— MVP 一个 MCP 一个条件就够，复合条件再加需要 ADR。

**`${env:VAR}` / `${env:VAR:-default}` 变量插值** 仅在 `args`, `command`, driver-specific string fields 里生效，MVP 一定支持 `-` 默认值。

**Loader 逻辑：**

```go
// internal/mcp/loader.go
func LoadAll(ctx context.Context, dir string, reg *tools.Registry) ([]io.Closer, error) {
    files := scanYAML(dir)  // mcp/*.yaml
    var closers []io.Closer
    for _, f := range files {
        cfg := parseYAML(f)
        if !cfg.EnabledIf.Eval() {
            log.Printf("mcp: %s disabled (enabled_if=%q)", cfg.Name, cfg.EnabledIf.Raw)
            continue  // 显式 skip, 不算错
        }
        driver := driverRegistry[cfg.Transport]  // "inproc"/"stdio"
        if driver == nil {
            return nil, fmt.Errorf("mcp %s: unknown transport %q", cfg.Name, cfg.Transport)
        }
        closer, err := driver.Start(ctx, cfg, reg)
        if err != nil {
            return nil, fmt.Errorf("mcp %s: %w", cfg.Name, err)  // enabled 但启动失败 = 硬 fail
        }
        closers = append(closers, closer)
    }
    return closers, nil
}
```

**enabled 但启动失败** = 硬 error（区别于当前的 warn-and-continue）。理由：yaml 声明"要跑"结果没跑起来，应该早报早死，不该悄悄降级。

**Pros:**
- Driver 是 Go interface，加新 transport 类型（未来 `sse` / `http`）只加一个包，不动 loader
- yaml 目录扫描跟 `agents/*.yaml` 对称，学习曲线为零
- `enabled_if` 让"为什么这个 MCP 没跑"从"读 Go 源码 + 检查日志"变成"看 yaml"
- 硬 fail on startup error → 部署时 pod crashloop 立刻暴露配置问题，不是"跑得起来但 tool 缺失"

**Cons:**
- 引入 driver interface + condition parser，代码量比现在多约 300 行
- yaml 变量插值需要一个 mini-parser（可选 `os.ExpandEnv` 但它不支持默认值，得自己写 20 行）

### Option B: 顶层 yaml 单文件 `mcp.yaml`

一个文件配所有 MCP：

```yaml
# mcp.yaml
mcps:
  - name: demo
    transport: inproc
    provider: builtin-demo
  - name: filesystem
    transport: stdio
    command: npx
    ...
```

**Pros:**
- 一个文件，无目录扫描

**Cons:**
- 破坏"每 MCP 一份声明"的对称性（`agents/` 是每 agent 一份，`mcp/` 应该同构）
- Phase 3 加 REST API 时想 CRUD 单个 MCP 要读整个文件、改内容、写回全文件 —— 并发写风险 + 冲突
- 前端 Phase 4 想 "只 disable 一个 MCP" 也是同问题

**为什么不选**：现在方便一时，Phase 3/4 会付双倍利息；`mcp/*.yaml` 目录版几乎零多余工作量还前瞻。

### Option C: 保持硬编码，只把 `enabled_if` 参数化

不引入 driver 抽象、不引入 loader；只把 `StartInProc` / `StartFilesystem` 的"要不要启动"抽出来到 yaml。

**Pros:**
- 最小改动

**Cons:**
- 违反 workbuddy vision"加 MCP 不改 Go"的根本目标
- 只是把"哪些 MCP 会跑"从 Go 挪一半到 yaml，另一半还在 main.go —— 更混乱
- Phase 2/3/4 到来时得推倒重做

**为什么不选**：拧半只螺丝不如不拧。

## Decision

**采用 Option A**：driver 抽象 + `mcp/*.yaml` 目录 + `enabled_if` 表达式解析。

**关键分层：**
```
cmd/server/main.go               # 只调 mcp.LoadAll, 不 case 具体 MCP
  ↓
internal/mcp/loader.go           # 扫 yaml → 分派 driver → 收 closers
  ↓
internal/mcp/driver.go           # Driver interface
  ↓  (impl)
internal/mcp/inproc/  stdio/     # 一 transport 一包
```

**符合 workbuddy vision 的产品终态**：Phase 3 REST API `/api/mcp` CRUD 就是"接管 yaml 读写"，Phase 4 前端把 yaml schema 映射成表单，逻辑分层今天就摆对。

## Detailed Design

### 数据模型 / Schema

**yaml struct**（`internal/mcp/config.go`）：

```go
type Config struct {
    Name        string            `yaml:"name"`
    Transport   string            `yaml:"transport"`  // "inproc" | "stdio"
    Provider    string            `yaml:"provider,omitempty"`  // inproc only
    Command     string            `yaml:"command,omitempty"`   // stdio only
    Args        []string          `yaml:"args,omitempty"`      // stdio only
    Env         map[string]string `yaml:"env,omitempty"`       // stdio only
    EnabledIf   string            `yaml:"enabled_if,omitempty"`
    InitTimeout time.Duration     `yaml:"init_timeout,omitempty"` // default 30s
    // ---
    DefaultRoot string `yaml:"default_root,omitempty"` // inproc list_dir default; 解决 STATUS #1
}
```

**Driver interface**（`internal/mcp/driver.go`）：

```go
type Driver interface {
    Name() string  // "inproc" | "stdio"
    Start(ctx context.Context, cfg Config, reg *tools.Registry) (io.Closer, error)
}

var driverRegistry = map[string]Driver{}

func RegisterDriver(d Driver) {
    driverRegistry[d.Name()] = d
}

// inproc/inproc.go 和 stdio/stdio.go 各自 init() 里注册
```

**`enabled_if` 语法**（`internal/mcp/cond.go`）：

```go
// Eval returns whether MCP should be started.
// Grammar (MVP):
//   ""          → always true
//   "always"    → always true
//   "env:VAR"       → os.Getenv("VAR") != ""
//   "env:VAR=v"     → os.Getenv("VAR") == "v"
// Anything else → error at parse time (fail early on typo).
func evalCondition(expr string) (bool, error) { ... }
```

**`${env:VAR:-default}` 插值**（`internal/mcp/cond.go` 里同放）：

```go
// expandVars supports ${env:VAR} and ${env:VAR:-default}.
// os.ExpandEnv doesn't do :-default so we roll our own tiny parser.
func expandVars(s string) string { ... }
```

### 目录布局

```
internal/mcp/
  config.go        # Config struct + YAML tags
  cond.go          # enabled_if + expandVars
  driver.go        # Driver interface + driverRegistry
  loader.go        # LoadAll
  inproc/
    inproc.go      # StartInProc → 改成 Driver 实现
    tools.go       # echo + list_dir (含 default_root fallback)
  stdio/
    stdio.go       # 通用 stdio driver（不再叫 filesystem，因为 filesystem 只是 stdio 的一个实例）
mcp/               # 新目录（跟 agents/ 对称）
  demo.yaml
  filesystem.yaml
```

**旧文件 → 新文件的迁移映射**：
- `internal/mcp/inproc.go` → `internal/mcp/inproc/inproc.go` + `tools.go`
- `internal/mcp/filesystem.go` → `internal/mcp/stdio/stdio.go`（**改名**，因为"stdio driver" 才是本质，filesystem 只是配置数据）+ `mcp/filesystem.yaml`（配置）

### main.go 变化

**Before：**
```go
if c, err := mcpbridge.StartInProc(ctx, reg); ...
if c, err := mcpbridge.StartFilesystem(ctx, reg); ...
```

**After：**
```go
mcpDir := envOr("MCP_DIR", "mcp")
mcpClosers, err := mcpbridge.LoadAll(ctx, mcpDir, reg)
if err != nil {
    log.Printf("mcp: %v", err)
    os.Exit(1)
}
closers = append(closers, mcpClosers...)
```

**新增 env**：`MCP_DIR`（默认 `mcp`，容器里 `/mcp`）—— 跟 `AGENTS_DIR` 同模式。

### Dockerfile 变化

```dockerfile
# runtime stage
COPY --from=go-builder /app/agents /agents
COPY --from=go-builder /app/mcp /mcp     # ← 新增
```

### 迁移路径

**Runtime 行为 0 变化**（Phase 1 承诺）：
- `mcp/demo.yaml` `enabled_if: always` → 等同于当前"总是启用 inproc"
- `mcp/filesystem.yaml` `enabled_if: env:ENABLE_FS_MCP=1` → 等同于当前 gate
- inproc 依然叫 `demo`（tool name 前缀 `mcp.` 不变）
- stdio filesystem 依然叫 `filesystem`（tool name 前缀 `fs.` 不变）
- 现有 `agents/*.yaml` 引用 `mcp.echo` / `mcp.list_dir` / `fs.read_file` 全部照旧
- evals/routing.yaml **一字不改**，6/6 必须保持绿

**回滚路径**：如果 Phase 1 出问题，`git revert` 单个 PR 即可回到硬编码状态；yaml 文件删除，Go 代码回滚。

## Acceptance Criteria

- [ ] `mcp/demo.yaml` + `mcp/filesystem.yaml` 存在，schema 符合 spec
- [ ] `internal/mcp/` 按新布局重组（config / cond / driver / loader + inproc/ + stdio/ 子包）
- [ ] `cmd/server/main.go` 第 3 步只调 `mcp.LoadAll`，无 case-by-MCP 分支
- [ ] `ENABLE_FS_MCP=1` 本地跑 → filesystem MCP 起来，`fs.*` tools 注册（跟现在一致）
- [ ] `ENABLE_FS_MCP` 未设 → filesystem 显式 skip，log 一行 `mcp: filesystem disabled (enabled_if=...)`
- [ ] `enabled_if` 拼写错误（例：`env:FOO=BAR=X`）→ **启动失败**，不是 warn
- [ ] enabled=true 但启动失败（例：`command: nonexistent-binary`）→ **启动失败**，pod crashloop
- [ ] inproc `list_dir` 支持 `default_root` 配置，容器里能返回非空（fix STATUS 已知问题 #1）
- [ ] `go build ./... && go vet ./... && golangci-lint run` 全绿
- [ ] `evals/routing.yaml` 本地跑 6/6 保持不退化
- [ ] Docker build + `ctr import` + `kubectl rollout` 一遍：容器里 pod ready，`mcp.echo` `mcp.list_dir` 可用，`fs.*` 因 `ENABLE_FS_MCP` 未设显式 disabled（不再 warn）
- [ ] CI `build + vet` / `golangci-lint` 全绿
- [ ] `STATUS.md` 归档 Phase 1 完成 + 已知问题 #1/#2 状态更新
- [ ] `ARCHITECTURE.md` §6 (MCP 集成) 章节改写反映新架构
- [ ] `CLAUDE.md` "目录约定" 加 `mcp/*.yaml` 说明；"加新 MCP" 从"改 Go" 改成"加 yaml"
- [ ] ADR-005 (MCP driver 抽象 + 声明式 loader) 已写
- [ ] `MCP_DIR` env 加进 CLAUDE.md 环境变量表

## Risks & Tradeoffs

- **Risk 1: `enabled_if` 表达式演化压力**：MVP 只 3 种表达式，将来可能有人想 `env:A=1 && cmd:npx`。**缓解**：拒绝，让复合条件的人写两个 MCP yaml 或改代码；如果真需要，另开 ADR 引入表达式引擎（避免 CEL-like 依赖蔓延）。

- **Risk 2: yaml schema 无校验**：手写 typo 只在启动才发现。**缓解**：MVP 接受此代价；Phase 3 前端接管后表单校验兜底；单独 backlog 一条"加 JSON Schema 校验"未来做。

- **Risk 3: 目录扫描顺序不稳定**：`mcp/` 里两个文件，注册顺序影响什么？**缓解**：tools.Registry 现在按 name 去重，顺序无关；文件按 `filepath.Glob` 字典序（Go 保证），跟 `agents/` 一致。写单元测试锁定顺序。

- **Tradeoff 1: Driver 接口冻结成本**：一旦 driver interface 定型，改就是 breaking change。**接受**：现在只做 inproc + stdio 两种，interface 简单到就一个 `Start` 方法，改的可能性低。

- **Tradeoff 2: 保留 `ENABLE_FS_MCP` env 语义** 意味着 CLAUDE.md / dev.ps1 里的 workflow 一字不改，但也留下"env-based enable"这个偏"外部注入"的模式，未来产品化时可能想改成"从 config 页面开关"。**接受**：Phase 4 前端到位时新增一个开关字段（`enabled: true/false`）叠加在 `enabled_if` 之上，语义清晰不冲突。

## Out of Scope

以下未来可能做，本 spec 不承诺：

- **JSON Schema 校验** `mcp/*.yaml`（后续 backlog）
- **`enabled: bool` 字段**：跟 `enabled_if` 叠加使用，管 UI 开关（Phase 4 顺带）
- **MCP server 健康检查**：init 后定期 ping / 掉线重连（暂无需求，MCP 掉线就等下次 chat 触发再 reconnect）
- **多 stdio provider（不止 filesystem）**：stdio driver 已经通用化，加新的只是新 yaml，spec 不列具体 MCP
- **secrets manager 集成**：`${env:VAR}` 够用，vault 引用之类是 workbuddy vision Non-Goals
- **inproc driver 里 provider 名字的动态注册**：MVP 里 provider 是 Go 侧内建常量（`builtin-demo`），未来加新 inproc provider 走"加 Go 代码"路径（跟 built-in tool 一样）

## Open Questions

- [x] **inproc 的 `provider` 字段应叫什么**？考虑过 `provider` / `impl` / `builder`。**决定**：`provider`，因为语义是"哪份内建实现"，跟数据源 provider 类比自然
- [x] **`enabled_if` 拼错要 fail-fast 还是 warn-and-skip**？→ **fail-fast**。理由：typo 让 MCP 静默跳过更难 debug，不如 pod 起不来立刻看错误
- [x] **`default_root` 只放 inproc 还是所有 driver 共用**？→ **只 inproc**。stdio 有 `args` / `env` 表达路径参数，通用字段会污染 schema
- [x] **stdio driver `init_timeout` 默认是 30s（继承当前 filesystem）还是全项目统一到某值**？→ **沿用 30s**。理由：跟当前 `internal/mcp/filesystem.go` 语义一致，Phase 1 承诺"runtime 行为 0 变化"；stdio 目前只有 filesystem 一个实例，没有横向对比需求；未来若加第二个 stdio provider 且默认不合适，另开 ADR 讨论是否统一或每 yaml 覆盖
- [ ] **`ENABLE_FS_MCP=1` env 未来是否要弃用**？倾向 Phase 4 前端有 `enabled: true/false` 后弃用 env 版本，但不在 Phase 1 spec 承诺（**留待 Phase 4 时再决**）

## References

- `docs/specs/workbuddy-vision.md` §Phase 1 —— 本 spec 是 vision 里 Phase 1 的落地
- `internal/mcp/inproc.go` / `internal/mcp/filesystem.go` —— 当前硬编码实现
- `internal/agentcfg/` —— agents/*.yaml loader 是对称样本
- `cmd/server/main.go:60-72` —— 当前 MCP 启动位置
- STATUS.md 已知问题 #1（list_dir 空）/ #2（fs MCP 无 npx）—— 本 spec 顺带解决
- CLAUDE.md "AI 助手做事的偏好" 硬规矩 —— 本 spec 是"大改动先写 spec"的实践对象
- MCP 官方 spec：https://spec.modelcontextprotocol.io/
