# CLAUDE.md

> 项目的 AI 会话上下文：任何 AI（Claude Code / Cursor / Codex / ChatGPT）打开这个项目都读这份。
> 目标是**把踩过的坑固化下来**，别让下一次会话再撞一遍。
> 深度设计原理看 [`ARCHITECTURE.md`](ARCHITECTURE.md)；当前状态和已知问题看 [`STATUS.md`](STATUS.md)；工程化改进路线看 [`docs/roadmap-ai-engineering.md`](docs/roadmap-ai-engineering.md)。

---

## 项目一句话

**Eino 多 agent demo，跑在 Docker Desktop 内置 k8s 上**：Host LLM 路由 → Specialist（math / ops / research）→ 内置工具 or MCP，SSE 流式回浏览器。单二进制 + distroless 镜像。

---

## 技术栈锁死清单

| 组件 | 版本 | 备注 |
|---|---|---|
| Go | **1.26.4**（`go.mod`）+ **1.26** Dockerfile builder stage | `golang:1.26` 镜像，别改回 1.24（原来撞过） |
| Eino | `v0.9.12` | 主编排框架 |
| Ark 适配器 | `eino-ext/components/model/ark v0.1.68` | |
| MCP 适配器 | `eino-ext/components/tool/mcp v0.0.8` + `mark3labs/mcp-go v0.55.1` | |
| Node（仅前端构建阶段） | 20-alpine | |
| React / TS / Vite | 18.3 / 5.6 / 5.4 | |
| Runtime image | `gcr.io/distroless/static-debian12:nonroot` | 无 shell、无 node、无 `npx` |

**不要**：
- 把 Go 版本改回 1.24（`go.mod` 声明 1.26.4，builder 1.24 编不了）
- 换 alpine runtime（distroless 已够小、镜像 ~42MB / 压缩 ~10MB）
- 把外部 filesystem MCP 塞进 runtime 镜像（distroless 里没 npx，改镜像会翻倍。要用就走 sidecar 或放弃）

---

## 目录约定

```
cmd/server/          启动编排（main.go 顶部注释有 8 步启动顺序，别乱序）
internal/
  llm/               Ark ChatModel 工厂（单点，所有 agent 共享一个实例）
  tools/             Tool Registry + 内置技能（calculator / weather / current_time）
  agentcfg/          agents/*.yaml 加载器
  mcp/               inproc + 外部 stdio MCP 桥接（→ Registry）
  agents/            react.Agent → host.Specialist + Host + DefaultHostPrompt
  httpapi/           /api/chat SSE + /healthz + 静态嵌入
  webassets/         //go:embed all:dist（前端产物必须放这）
agents/*.yaml        Specialist 声明式配置（每个 yaml 一个 agent）
web/                 React + Vite 前端源码
k8s/                 deployment / service / secret 模板
docs/                路线图、ADR（未来）、spec（未来）
Dockerfile           三阶段：node → go → distroless/static
```

**核心约束：**
- **加新 sub-agent 的常规路径 = 只加 YAML**（如果工具已存在）。不要改 `main.go` / `agents/build.go`。
- **加新内置工具** = `internal/tools/xxx.go` + 在 `registry.go` 的 `RegisterBuiltins` 循环里加一行。
- **Host 路由 prompt** 是 `agents/build.go:101` 的 `DefaultHostPrompt` 常量，不是 YAML。改路由行为改这里。
- **前后端事件契约**在两处独立声明（`internal/httpapi/events.go` + `web/src/sseClient.ts`），加事件类型两边都要改，暂时没 codegen。

---

## 环境变量

必需（缺就 `log.Fatalf`）：
- `ARK_API_KEY` — 火山方舟 key
- `ARK_MODEL_ID` — endpoint id `ep-xxxx`

可选：
- `ARK_BASE_URL` / `ARK_REGION` — 覆盖默认
- `PORT` — 默认 8080
- `AGENTS_DIR` — 默认 `agents`（容器里 `/agents`）
- `ENABLE_FS_MCP=1` — 启用外部 filesystem MCP（**需要 `npx` 在 PATH**，distroless runtime 里没有，只有本地开发有意义）
- `FS_MCP_ROOT` — 外部 MCP 能访问的根目录，缺就用 CWD

---

## Ark endpoint 的隐性坑

> **必读**，401 就是这个原因。

Ark 控制台里 API Key 和 Endpoint 是**分开授权**的：
1. 生成 API Key 不代表这个 key 能调所有 endpoint。
2. 必须在 **控制台 → API Key → 编辑权限** 里，把目标 endpoint（`ep-xxxx`）显式加进这个 key 的可用列表。
3. 否则调用时报 401 / permission denied，跟 key 本身"看起来对"没关系。

当前生效组合：key `ark-e6f7c3ce-...-e3918` + endpoint `ep-20260609204306-xj4xt`，已授权。

Ark **Agent Plan** 套餐（另一个坑）：只能给 Claude Code 等第三方 AI 工具用，**不能直接调 Ark API**。我们这个项目用的是普通的 Ark endpoint。

---

## 部署：Docker Desktop 内置 k8s 的坑

> **每次 `docker build` 后必须手动 import 到 containerd 的 k8s 命名空间**，Docker Desktop 的 image store **不**共享给内置 k8s。GUI 里那个"Use containerd for pulling and storing images"勾选没有实际帮助。

标准流程：

```bash
# 1) 构建
docker build -t eino-demo:local .

# 2) 塞进 k8s 的 containerd（关键步骤，别跳过）
docker save eino-demo:local -o /tmp/eino-demo.tar
docker cp /tmp/eino-demo.tar <docker-desktop-vm-somehow>:/tmp/  # 具体命令看 STATUS.md
# 然后在 Docker Desktop VM 里：
ctr -n k8s.io images import /tmp/eino-demo.tar

# 3) 部署
kubectl apply -f k8s/deployment.yaml -f k8s/service.yaml
kubectl rollout restart deployment/eino-demo
kubectl rollout status deployment/eino-demo

# 4) 访问
kubectl port-forward svc/eino-demo 8080:80
# http://localhost:8080
```

**Deployment 里 `imagePullPolicy: Never`** —— 别改成 `IfNotPresent`，我们没推 registry。

**长期解法**（未做）：推到 GHCR 或本地 registry，让 k8s 正常 pull。

---

## 常用命令速查

```bash
# 本地开发（前端 hot reload）
cd web && npm run dev      # :5173
go run ./cmd/server        # :8080，前端代理

# 或 Windows PowerShell
.\dev.ps1 web
.\dev.ps1 server           # 先 $env:ARK_API_KEY=...; $env:ARK_MODEL_ID=...

# 单二进制构建
make build     # = npm run build → 拷 dist → go build
make run

# 容器 + k8s
make docker    # docker build -t eino-demo:local .
make deploy    # apply + rollout restart + rollout status
make port-forward

# SSE 调试（不用浏览器）
curl -N "http://localhost:8080/api/chat?q=12%20times%207"
curl -N "http://localhost:8080/api/chat?q=what%20time%20is%20it%20in%20UTC"

# Evals（路由回归测试）
go run ./cmd/evals -file evals/routing.yaml
```

---

## 已知问题（快速索引，详情看 `STATUS.md`）

1. **`list_dir` 在容器里返回 `[]`** —— Dockerfile 没 `WORKDIR /data`，distroless 的 `/` 极简
2. **外部 filesystem MCP 在 k8s 没起** —— distroless 无 `npx`；本地开发有 `ENABLE_FS_MCP=1` 可用
3. **Host 路由把"帮我查一下 goroutine 调度器"给了 ops 而不是 research** —— 路由 prompt 或 specialist description 待调优；用 `evals/routing.yaml` 量化
4. **"echo hello agents" LLM 直接复述** —— 没触发 `mcp.echo`，太简单被跳过；要显式在 prompt 里要求工具调用

---

## AI 助手做事的偏好

1. **改代码前先看 `STATUS.md` 和 `ARCHITECTURE.md`**。ARCHITECTURE 里已经把每个模块"为什么这么设计"写清楚了，别推翻已有约定。
2. **大改动先写 spec**：在 `docs/specs/<feature-name>.md` 描述现状 / 目标 / 方案 A/B/C / 选定方案 / 验收标准，再动代码。
3. **架构级决策记录在 `docs/adr/`**（还没建目录，第一次写就建）。ADR = Architecture Decision Record，一决策一文件。
4. **改 host prompt / specialist description 后必须跑 `evals/routing.yaml`**，别只靠"手点浏览器验证"。
5. **加事件类型时改两处**：`internal/httpapi/events.go` 和 `web/src/sseClient.ts`（+ `App.tsx` 里 `applyEvent` 的 switch）。
6. **不要**：
   - 把 `mcp.` 前缀里的下划线换成横线之类的"美化"（`mcp.list_dir` 是当前 in-proc MCP server 定义的名字）
   - 把 Registry 从"启动时静态注册"改成"运行时动态注册"（除非有明确需求，且改 `MustResolve` 的语义）
   - 在 Go 代码里硬编码 API key / endpoint id（只走 env）
7. **代码风格**：跟着 `gofmt` + `go vet`；`golangci-lint` 已启用（配置在 `.golangci.yml`），提交前跑一次。

---

## GitHub 相关

- 账号：`mlliu920423-beep`
- 仓库：https://github.com/mlliu920423-beep/first-agentInK8s（**Private**）
- `gh` CLI 路径（Windows）：`C:\Program Files\GitHub CLI\gh.exe`
- git commit email：`mlliu920423-beep@users.noreply.github.com`（贡献日历挂 GitHub 账号）

---

## 更新这个文件的时机

- 加了新的技术栈约束（比如换掉某个库）→ 更新"技术栈锁死清单"
- 踩了新的坑 → 加到对应章节，最好一句话能让下一个 AI 会话避开
- 加了新的常用命令 → 加到"常用命令速查"
- 改了目录结构 → 更新"目录约定"

**不要**把它当成 changelog 或 STATUS 日志用 —— 那是 `STATUS.md` 的事。这里放的是**跨会话稳定的、机器可参考的项目约束**。
