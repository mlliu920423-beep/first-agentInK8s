# first-agentInK8s 项目状态

> 项目**当前状态 + 决策日志**，进 git、跟代码走。
> 每次收尾在此更新；跨会话的元知识（工具坑、账号背景等）留在 `~/.claude/memory/`。

**最后更新：2026-07-10 白天**（用户晚上继续；上次实操 07-06）

---

## 一句话现状

本地 `./server.exe` 三条 specialist（math / research / ops）已跑通；上 k8s 卡在 `docker build -t eino-demo:local .` 报错未记录，**下次开工先跑 `docker build` 拿错误再动手**。今天白天没动代码，只做了工作流整顿：项目建了 git 仓库，状态存档从 memory 迁到本文件。

---

## 已完成

### 应用层
- Eino 多 agent 骨架：Host + math / research / ops 三条 specialist
- Ark v0.9.12 集成：`ToolCallingModel` + `Specialist.Invokable/Streamable` + `host.WithAgentCallbacks`
- SSE 流式 HTTP + tool/handoff 回调
- MCP 双集成（inproc + filesystem via `npx`）
- 声明式 sub-agent 扩展面（`agents/*.yaml`）

### 环境 / 账号（07-06）
- **Ark 401 已解**：新 key `ark-e6f7c3ce-...-e3918` + endpoint `ep-20260609204306-xj4xt`，控制台已把 key 授权给 endpoint
- 本地浏览器验证三条 prompt 全 OK：
  - `12 乘以 7 等于多少` → `→ math_agent` + `🔧 calculator`
  - `UTC 现在几点` → `→ ops_agent` + `🔧 current_time`
  - `echo hello agents 然后列一下当前目录` → `→ research_agent` + `🔧 mcp.echo` + `🔧 mcp.list_dir`
- Docker Desktop 29.6.1 装好，内置 k8s Ready，`kubectl` context = `docker-desktop`
- `kubectl create secret generic ark-secret` 已建（`ARK_API_KEY` + `ARK_MODEL_ID` 两个 literal）

### k8s 脚手架（07-06）
- `Makefile`：删掉 `kind-load` 目标，`deploy` 直接依赖 `docker`
- `k8s/deployment.yaml`：`imagePullPolicy` 从 `IfNotPresent` 改成 `Never`

### 工作流整顿（07-10 白天）
- `git init` + 首次 commit（分支 main，41 文件）；全局配 `user.name=Bigmay` / `user.email=Bigmay@users.noreply.github.com`
- `web/tsconfig.tsbuildinfo` 加进 `.gitignore`（TS 增量缓存，不该入库）
- **状态存档规则改了**：本文件（`STATUS.md`）是项目状态的唯一入口，跟代码走进 git；memory 只留跨会话元知识（工具坑、账号背景、约定）

---

## 下一步（按顺序）

### 1. 排查 `docker build` 失败 ← 从这里开始

在 `D:\Bigmay\Projects\first-agentInK8s`（PowerShell）跑：

```powershell
docker build -t eino-demo:local .
```

把**完整错误**贴出来。别猜着改，先看到错误再动。

**已知嫌疑点：**
- Dockerfile 里 `golang:1.24`，但 go.mod 是 1.26.4 → `go mod download` / `go build` 可能因版本约束失败（**最可疑**）
- `web/` 阶段 npm install 网络问题
- `COPY web/package-lock.json*` 通配防文件不存在，值得核一下确实生效

### 2. build 通了之后

```powershell
kubectl apply -f k8s/deployment.yaml -f k8s/service.yaml
kubectl rollout status deployment/eino-demo
kubectl port-forward svc/eino-demo 8080:80
```

浏览器 http://localhost:8080，跑三条 prompt 验 math / research / ops。
Pod 起不来就 `kubectl describe pod` + `kubectl logs` 定位。

### 3. 扩展验证（可选）

加 `agents/joke_agent.yaml` 试自动路由，验证 YAML 扩展面无需改 Go 就能加 specialist。

---

## 环境约定（给 Claude 和未来的自己）

- Shell：**PowerShell**，不是 bash。设环境变量用 `$env:VAR="value"`，跑本目录 exe 写 `.\server.exe`；bash 的 `VAR=value ./cmd`、反斜杠续行不通用
- Windows **没装 make**，直接跑 Makefile 里的底层命令
- Go 1.26.4 / Node 24.16 / Docker Desktop 29.6.1（含 k8s v1.36.1）/ kubectl v1.36.1 / `npx` 都已装
- Ark 走"在线推理端点 + 按量付费"，**不是** Agent Plan（套餐不能直接调 API）
- Docker Desktop 的 docker daemon 和内置 k8s 共享，`docker build` 完镜像立刻在集群里可用，无需 `kind load`

### 本地启动样板

```powershell
cd D:\Bigmay\Projects\first-agentInK8s
$env:ARK_API_KEY="ark-e6f7c3ce-...-e3918"
$env:ARK_MODEL_ID="ep-20260609204306-xj4xt"
.\server.exe
```

---

## 关键坐标（想改什么 → 改哪儿）

| 想改什么 | 打开哪个文件 |
|---|---|
| 加新 sub-agent | `agents/*.yaml`（无需改 Go） |
| 加内置 skill | `internal/tools/xxx.go` + `registry.go:RegisterBuiltins` |
| 加 MCP server | 仿 `internal/mcp/filesystem.go` 写 `Start*` |
| 换 LLM | 改 `internal/llm/ark.go`（返回 `model.ToolCallingChatModel`） |
| 改路由脑 | `internal/agents/build.go:DefaultHostPrompt` |
| 改 SSE 事件 | `internal/httpapi/events.go` + `sse.go` + `web/src/App.tsx:applyEvent` |
| bootstrap 顺序 | `cmd/server/main.go` |
| Docker 构建 | `Dockerfile`（多阶段：Go + web） |
| k8s 部署 | `k8s/deployment.yaml`（`imagePullPolicy: Never`，引用 `ark-secret`）/ `k8s/service.yaml` |

---

## 决策日志

| 日期 | 决策 | 原因 |
|---|---|---|
| 2026-07-06 | 弃用 kind，走 Docker Desktop 内置 k8s | 与本机 docker 共享 daemon，build 完不用 `kind load`，链路短一节 |
| 2026-07-06 | `imagePullPolicy: Never` | 本地 image，禁止 fallback 到 registry 拉远端（会失败还慢） |
| 2026-07-02 | Ark 走在线推理端点 + 按量付费 | Agent Plan 套餐只给 AI 工具用，不能直连 API |
| 2026-07-10 | 项目建 git 仓库；`STATUS.md` 进 git 作为状态唯一入口 | memory 里堆日期版状态用户看不见、换机器丢；`STATUS.md` 跟代码走可 review 可回溯 |

---

## 未决 / TODO

- [x] ~~项目是否建 Git 仓库~~ → 已建（07-10）
- [ ] 老 API Key 排查阶段建的临时 key 是否 revoke
- [ ] `docker build` 通了以后，是否推 GitHub 远端仓库
