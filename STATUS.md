# first-agentInK8s 项目状态

> 项目**当前状态 + 决策日志**，进 git、跟代码走。
> 每次收尾在此更新；跨会话的元知识（工具坑、账号背景等）留在 `~/.claude/memory/`。

**最后更新：2026-07-15**（工程化第一批 3 个 commit 已 push 到 `origin/main`；roadmap 中三件"立刻能做"完成）

> 📍 工程化改进路线：[`docs/roadmap-ai-engineering.md`](docs/roadmap-ai-engineering.md)（AI 辅助开发的业界实践 + 本项目改进清单，2026-07-14 起草）

## 2026-07-14 / 07-15 变更

三个 commit 已推到 `origin/main`（`98234de` → `c0c5ee7`）：

| commit | 说明 |
|---|---|
| `6e29d87 chore(deps)` | `go mod tidy` —— `eino` / `eino-ext/*` / `mcp-go` / `yaml.v3` 从 indirect 升到 direct（历史元数据清理，无 runtime 变化）|
| `2aed52c docs` | 新增 `CLAUDE.md`（AI 会话上下文，锁死技术栈 + Ark endpoint 授权坑 + `ctr import` 流程 + 加 sub-agent 规范）；新增 `docs/roadmap-ai-engineering.md`（业界实践 + 13 项改进清单） |
| `c0c5ee7 feat(evals+lint)` | `evals/routing.yaml` + `cmd/evals`（路由回归，6 case，含 STATUS 已知问题 #3 的红色 case）+ `.golangci.yml` + `lefthook.yml` |

**验证：** `go build ./...` + `go vet ./...` 均通过。运行时二进制 / 镜像 / pod 行为**未变**，无需 `docker build` / `kubectl apply`。生产 pod 沿用 07-11 部署。

**尚未做（下一次会话优先级）：**
1. 装 `golangci-lint` + `lefthook`（本机未装），跑第一次 lint，按 report 调 `.golangci.yml`：
   - `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`（或 releases binary）
   - `go install github.com/evilmartians/lefthook@latest`（或 winget / scoop）
   - `golangci-lint run` → 逐条判断"修" vs "豁免"
   - `lefthook install` 装 git hook
2. `go run ./cmd/evals`（需 `ARK_API_KEY` / `ARK_MODEL_ID`）—— 实测 `research-goroutine` 是不是仍是红色 case，然后开工调 host prompt / research description，反复跑 eval 卡绿。
3. GitHub Actions CI（roadmap §4）：把 `go build` + `go vet` + `golangci-lint` + `evals` 挂上去，让 PR 有红绿。

---

## 一句话现状

**k8s 部署已跑通** —— pod `1/1 Running`，SSE / routing / Ark / 内置工具全通。剩下的都是打磨（Dockerfile 没设 WORKDIR 导致 list_dir 结果诡异；filesystem MCP 在容器里没起因为缺 npx）。

---

## 已完成

### 应用层
- Eino 多 agent 骨架：Host + math / research / ops 三条 specialist
- Ark v0.9.12 集成：`ToolCallingModel` + `Specialist.Invokable/Streamable` + `host.WithAgentCallbacks`
- SSE 流式 HTTP + tool/handoff 回调
- MCP 双集成（inproc + filesystem via `npx`，容器里只有 inproc 生效）
- 声明式 sub-agent 扩展面（`agents/*.yaml`）

### 环境 / 账号（07-06）
- **Ark 401 已解**：新 key `ark-e6f7c3ce-...-e3918` + endpoint `ep-20260609204306-xj4xt`，控制台已把 key 授权给 endpoint
- Docker Desktop 29.6.1 装好，内置 k8s Ready，`kubectl` context = `docker-desktop`
- `kubectl create secret generic ark-secret` 已建（`ARK_API_KEY` + `ARK_MODEL_ID`）

### k8s 部署（07-11）
- **Dockerfile Go 版本改 1.24 → 1.26**（原来跟 go.mod `go 1.26.4` 不匹配，直接编不了）
- `docker build -t eino-demo:local .` 成功，镜像 42MB / 压缩后 10MB
- `kubectl apply -f k8s/deployment.yaml -f k8s/service.yaml` 成功
- 用 `docker save + docker cp + ctr -n k8s.io images import` 把镜像塞进 containerd 的 k8s 命名空间（详见"决策日志"）
- Pod ready，`readinessProbe`（`/healthz`）通过，Ark secret 读到
- **浏览器验证**（`kubectl port-forward svc/eino-demo 8080:80` → http://localhost:8080）：
  - `12 乘以 7 等于多少` → ✅ `→ math_agent` + `🔧 calculator`
  - `UTC 现在几点` → ✅ `→ ops_agent` + `🔧 current_time`
  - `echo hello agents 然后列一下当前目录` → ⚠️ 部分符合预期（见下方"已知问题"）

### GitHub 远端（07-11 晚）
- 装了 `gh` CLI（winget `GitHub.cli` v2.96.0，路径 `C:\Program Files\GitHub CLI\gh.exe`）
- GitHub 账号是 `mlliu920423-beep`，git config email 改成 `mlliu920423-beep@users.noreply.github.com`
- push 前 `git rebase --root --exec 'git commit --amend --no-edit --reset-author'` 把三条历史 commit 的 author 从 `Bigmay@...` 改成 GitHub 账号邮箱，贡献日历/头像才能挂上
- 仓库：https://github.com/mlliu920423-beep/first-agentInK8s（**Private**），`origin/main` 已追踪

### 工程化基线（07-14 / 07-15）
- **`CLAUDE.md`** —— AI 会话首读文件，锁死技术栈版本 + Ark endpoint 授权坑 + `ctr import` 流程 + 加 sub-agent 规范
- **`docs/roadmap-ai-engineering.md`** —— 业界五层实践总结 + 本项目 13 项改进 backlog
- **`evals/routing.yaml` + `cmd/evals`** —— 路由回归 eval 骨架，`go run ./cmd/evals` 卡红绿；6 条 case 已编，含 STATUS 问题 #3 的"红色 case"
- **`.golangci.yml` + `lefthook.yml`** —— lint / pre-commit 配置起草完毕，本地工具链**尚未装**（roadmap 未闭环）
- **`go.mod` 元数据清理** —— 直接依赖脱离 `// indirect`

---

## 已知问题（未修，优先级低）

### 1. list_dir 返回 `[]`
Dockerfile 没 `WORKDIR`，server 的 CWD 是 `/`，distroless 镜像里 `/` 极简，`list_dir({"path": "."})` 拿不到有意义的内容。**修法：** Dockerfile 里加 `WORKDIR /data` + 一个 `RUN mkdir /data && ...` 塞几个样例文件；或前端默认传具体路径。

### 2. filesystem MCP 在容器里没起
Distroless 镜像没 `node` / `npx`，`fs.read_file` 那类外部 MCP 工具注册失败，日志明说：
```
tools: optional tool "fs.read_file" not registered (external MCP disabled?), skipping
```
**修法（三选一）：**
- 换基础镜像塞进 node（镜像会大一倍）
- 起个 sidecar container 跑 node MCP，Go 通过 stdio/socket 连
- 在容器里只用 inproc MCP，接受这个限制

### 3. host 路由把第三条给了 ops 而不是 research
本地也有此风险 —— 是 host prompt / 工具描述的语义问题，跟部署无关，独立调优。

### 4. LLM 跳过 `mcp.echo`
"echo hello agents" 太简单，LLM 直接复述、没触发工具调用。要强制走工具需要在 prompt 里更明示。

---

## 下一步（按顺序，可选）

### 0. 收尾工程化基线（07-14 起草，未闭环）
- 装 `golangci-lint` + `lefthook`，跑第一次 lint，按 report 调 `.golangci.yml`；`lefthook install` 装 git hook
- 跑一次 `go run ./cmd/evals`，实测 `research-goroutine` 红色 case
- GitHub Actions CI（roadmap §4）：`build` + `vet` + `lint` + `evals` 四个 job

### 1. 打磨部署产物
- Dockerfile 加 `WORKDIR /data` + 样例文件（解决 list_dir 空结果）
- 决定 filesystem MCP 怎么处理（推荐 sidecar 或干脆放弃）

### 2. 路由 / prompt 调优（用 eval 卡红绿而不是手点）
- 看 host prompt 是不是让 ops 的描述吞了 research 的活
- 让 `mcp.echo` 的描述更"抢戏"，或者在 research prompt 里显式要求
- **改动流程**：先跑 `go run ./cmd/evals` 记录基线 → 改 prompt → 再跑 → 对比

### 3. 扩展验证（延续 07-06 遗留）
加 `agents/joke_agent.yaml` 试自动路由，验证 YAML 扩展面无需改 Go 就能加 specialist。

### 4. 工作流
- 老 API Key 排查阶段的临时 key 是否 revoke

---

## 部署 Runbook（重跑一次时用）

```powershell
# 1. build（镜像进 docker 那份 store）
docker build -t eino-demo:local .

# 2. 把镜像塞进 k8s 那份 store（每次 build 都要重跑！）
docker save eino-demo:local -o eino-demo.tar
docker cp eino-demo.tar desktop-control-plane:/root/eino-demo.tar
docker exec desktop-control-plane ctr -n k8s.io images import /root/eino-demo.tar
# 验证：
docker exec desktop-control-plane crictl images | Select-String eino

# 3. 应用 k8s manifests（首次或改动后）
kubectl apply -f k8s/deployment.yaml -f k8s/service.yaml

# 4. 每次镜像变化触发滚动更新
kubectl rollout restart deployment eino-demo
kubectl rollout status deployment/eino-demo

# 5. 转发到本机验证
kubectl port-forward svc/eino-demo 8080:80
# 浏览器打开 http://localhost:8080

# 6. 清理临时文件
rm eino-demo.tar
```

**为什么第 2 步不能省** —— 见"决策日志"最下面那条。

---

## 环境约定（给 Claude 和未来的自己）

- Shell：**PowerShell**，不是 bash。设环境变量用 `$env:VAR="value"`，跑本目录 exe 写 `.\server.exe`；bash 的 `VAR=value ./cmd`、反斜杠续行不通用
- Windows **没装 make**，直接跑 Makefile 里的底层命令
- Go 1.26.4 / Node 24.16 / Docker Desktop 29.6.1（含 k8s v1.36.1）/ kubectl v1.36.1 / `npx` 都已装
- Ark 走"在线推理端点 + 按量付费"，**不是** Agent Plan（套餐不能直接调 API）
- **Docker Desktop 的 docker daemon 和内置 k8s image store 是分开的**，即便 Settings 里勾了 "Use containerd for pulling and storing images"（07-11 实测）—— 需要手动 `ctr images import` 才能让 k8s 看见 docker build 的镜像

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
| 2026-07-06 | 弃用 kind，走 Docker Desktop 内置 k8s | 链路短一节，不用 `kind load` |
| 2026-07-06 | `imagePullPolicy: Never` | 本地 image，禁止 fallback 到 registry 拉远端（会失败还慢） |
| 2026-07-02 | Ark 走在线推理端点 + 按量付费 | Agent Plan 套餐只给 AI 工具用，不能直连 API |
| 2026-07-10 | 项目建 git 仓库；`STATUS.md` 进 git 作为状态唯一入口 | memory 里堆日期版状态用户看不见、换机器丢；`STATUS.md` 跟代码走可 review 可回溯 |
| 2026-07-11 | Dockerfile `golang:1.24` → `1.26` | go.mod 声明 `go 1.26.4`，1.24 toolchain 编不了 |
| 2026-07-11 | 每次 build 后手动 `ctr -n k8s.io images import` | Docker Desktop 4.x 的 docker daemon 和内置 k8s containerd 的 image store 分开，即便 GUI 勾了 "Use containerd for images" 也不共享（实测）。写死进 runbook |
| 2026-07-11 | 推 GitHub Private 仓库 | 首个跑通的 agent 项目，值得留档；日常 `git push` 走 gh 缓存 token，无需单独配 credential |
| 2026-07-14 | 引入 `evals/` + `cmd/evals` 作为路由回归骨架 | Roadmap §4 观点：AI 项目里 eval = 传统单测。改 host prompt / description 后手点浏览器不 scalable，要卡红绿 |
| 2026-07-14 | 加 `CLAUDE.md` 作为 AI 会话首读文件 | 跨 AI 会话（Claude Code / Cursor / Codex）稳定的项目约束，避免每次重新踩 Ark 401 / Go 版本 / ctr import 那些坑 |
| 2026-07-14 | 选 `golangci-lint` + `lefthook` 而不是 `pre-commit` + `revive` | lefthook 单 Go binary、Windows/Linux 一致；golangci-lint 是社区默认 aggregator。装工具尚未闭环 |

---

## 未决 / TODO

- [x] ~~项目是否建 Git 仓库~~ → 已建（07-10）
- [x] ~~上 k8s~~ → 完成（07-11）
- [x] ~~推 GitHub 远端仓库~~ → 完成（07-11 Private）
- [x] ~~加 `CLAUDE.md` / 工程化 roadmap~~ → 完成（07-14）
- [x] ~~路由回归 eval 骨架~~ → 完成（07-14，红色 case 待实测）
- [x] ~~`golangci-lint` / `lefthook` 配置~~ → 配置起草完成（07-14），本机工具链未装
- [ ] 装 `golangci-lint` + `lefthook`，跑第一次 lint 并按 report 调配置
- [ ] `go run ./cmd/evals` 实测，`research-goroutine` 转绿
- [ ] GitHub Actions CI（build + vet + lint + evals）
- [ ] Dockerfile 加 WORKDIR + 样例文件（fix list_dir）
- [ ] 决定 filesystem MCP 在容器里怎么处理（sidecar / 放弃）
- [ ] 老 API Key 排查阶段建的临时 key 是否 revoke
