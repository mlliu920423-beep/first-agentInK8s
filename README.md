# first-agentInK8s — Eino 多 Agent Demo

一个可扩展的通用 Agent 应用最小骨架：**统一 Host Agent 作为路由入口，动态注册 sub-agent / skill / MCP 能力**。

- **框架** ByteDance [Eino](https://github.com/cloudwego/eino) v0.9.12
- **路由** `flow/agent/multiagent/host` — LLM 通过 "handoff as tool call" 选择 Specialist（学 OpenAI Swarm / Coze）
- **Sub-agent** 每个 Specialist 是一个 `react.Agent`，用自己的系统提示 + 工具子集
- **能力扩展面** `agents/*.yaml` 声明式定义 — 新加一个 yaml 无需改代码即可上线（学 Claude Code sub-agents）
- **技能** 内置 Go 工具：calculator / weather / current_time
- **MCP** ① 进程内 demo MCP server（`echo` / `list_dir`），② 可选外部 `@modelcontextprotocol/server-filesystem`
- **LLM** 火山方舟 Ark，`ARK_API_KEY` + `ARK_MODEL_ID`
- **UI** React + Vite + SSE；`npm run build` 后由 Go 通过 `//go:embed` 打进单二进制
- **部署** 本地 kind + `kubectl port-forward`

## 目录结构

```
cmd/server           启动编排
internal/
  llm/               Ark ChatModel 工厂
  tools/             统一 Tool Registry + 内置技能
  agentcfg/          YAML 加载器
  mcp/               进程内 + 外部 MCP 桥接（→ Registry）
  agents/            react.Agent → host.Specialist + Host multi-agent
  httpapi/           /api/chat SSE + /healthz + 静态嵌入
  webassets/         //go:embed all:dist
agents/*.yaml        Specialist 声明式配置
web/                 React + Vite（构建产物拷到 internal/webassets/dist）
k8s/                 deployment / service / secret 示例
Dockerfile           多阶段：node → go → distroless/static
Makefile / dev.sh / dev.ps1
```

## 快速开始

### 前置

- Go 1.24+
- Node 20+
- 火山方舟账号，拿到 `ARK_API_KEY` 和 endpoint id（`ep-xxxx`）

### 方式 A：单二进制（推荐第一次跑）

```bash
# 1) 构建前端并拷入 embed 目录
cd web && npm install && npm run build && cd ..
rm -rf internal/webassets/dist && mkdir -p internal/webassets/dist
cp -r web/dist/. internal/webassets/dist/

# 2) 构建并运行
export ARK_API_KEY=...
export ARK_MODEL_ID=ep-xxxx
go build -o server ./cmd/server
./server
# 浏览器打开 http://localhost:8080
```

或 `make build && make run`。

### 方式 B：前后端分离（UI 热更新）

两个终端：

```bash
# 终端 A — Vite 开发服（:5173，把 /api 代理到 :8080）
cd web && npm install && npm run dev

# 终端 B — Go 后端（:8080）
export ARK_API_KEY=...
export ARK_MODEL_ID=ep-xxxx
go run ./cmd/server
```

浏览器 http://localhost:5173

Windows PowerShell 用户：`.\dev.ps1 web` / `.\dev.ps1 server`（先 `$env:ARK_API_KEY=...`）。

### curl 调试（不用浏览器就能看事件流）

```bash
curl -N "http://localhost:8080/api/chat?q=what%20is%2012%20times%207"
curl -N "http://localhost:8080/api/chat?q=list%20files%20in%20the%20current%20directory"
curl -N "http://localhost:8080/api/chat?q=what%20time%20is%20it%20in%20UTC"
curl  http://localhost:8080/healthz
```

正常应该看到：

```
data: {"type":"agent_switch","data":{"to":"math_agent","argument":"..."}}
data: {"type":"tool_call","data":{"name":"calculator","args":"{\"a\":12,\"op\":\"*\",\"b\":7}"}}
data: {"type":"tool_result","data":{"name":"calculator","result":"{\"result\":84}"}}
data: {"type":"token","data":{"delta":"12 × 7 = "}}
data: {"type":"token","data":{"delta":"84."}}
data: {"type":"done","data":{"reason":"complete"}}
```

## 扩展一个新 Agent（无需改 Go 代码）

新建 `agents/joke_agent.yaml`：

```yaml
name: joke_agent
description: >
  Use when the user asks for jokes, humor, or something funny.
system_prompt: |
  You are Joke Agent. Tell a short, family-friendly joke.
tools: []
max_step: 4
```

重启服务即可路由到它。这条路径最能体现"通用 Agent 可扩展"的架构目标。

## 部署到本地 kind

需要预先安装：Docker Desktop + kind + kubectl。

```bash
kind create cluster --name eino-demo

# 构建并加载镜像（免注册表）
docker build -t eino-demo:local .
kind load docker-image eino-demo:local --name eino-demo

# 注入 secret
kubectl create secret generic ark-secret \
  --from-literal=ARK_API_KEY=$ARK_API_KEY \
  --from-literal=ARK_MODEL_ID=$ARK_MODEL_ID

# 部署
kubectl apply -f k8s/deployment.yaml -f k8s/service.yaml
kubectl rollout status deploy/eino-demo

# 访问
kubectl port-forward svc/eino-demo 8080:80
# 浏览器 http://localhost:8080
```

## SSE 事件格式

`data:` 一行一个 JSON：

| type          | data 字段                          | 何时发出                       |
| ------------- | ----------------------------- | -------------------------- |
| `token`       | `{delta}`                     | 每次 LLM 输出增量文本               |
| `agent_switch`| `{to, argument}`              | Host 决定 handoff 到 Specialist |
| `tool_call`   | `{name, args}`                | 任一 Specialist 调工具            |
| `tool_result` | `{name, result}`              | 工具返回                       |
| `done`        | `{reason}`                    | 流结束                        |
| `error`       | `{message}`                   | 中途错误                       |

## 环境变量

| 变量              | 必需 | 说明                                                     |
| --------------- | ---- | -------------------------------------------------------- |
| `ARK_API_KEY`   | 是   | 火山方舟 API Key                                          |
| `ARK_MODEL_ID`  | 是   | Ark endpoint id (`ep-xxxx`) 或模型名                     |
| `ARK_BASE_URL`  | 否   | 覆盖默认 endpoint                                        |
| `ARK_REGION`    | 否   | 覆盖默认区域                                             |
| `PORT`          | 否   | HTTP 端口，默认 `8080`                                   |
| `AGENTS_DIR`    | 否   | Agent yaml 目录，默认 `agents`（容器内 `/agents`）        |
| `ENABLE_FS_MCP` | 否   | 设为 `1` 启用外部 filesystem MCP（需要 Node/`npx`；只在本地开发有意义） |
| `FS_MCP_ROOT`   | 否   | 允许外部 MCP 访问的根目录，默认 `pwd`                     |

## 显式留白 / TODO

- `weather` 是 canned data，没接真实 API
- 每次 `/api/chat` 是**单轮**（无对话记忆）；后续可以按 session id 存 message 历史
- 无鉴权 / 无限流 / 无重试
- 外部 filesystem MCP 只在本地开发生效；k8s distroless 镜像里没有 `npx`
- Host prompt 写死在代码里（`agents.DefaultHostPrompt`），有意保持路由脑在代码，避免过早抽象
