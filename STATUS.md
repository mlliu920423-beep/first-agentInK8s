# first-agentInK8s 项目状态

> 项目**当前状态 + 决策日志**，进 git、跟代码走。
> 每次收尾在此更新；跨会话的元知识（工具坑、账号背景等）留在 `~/.claude/memory/`。

**最后更新：2026-07-16（上午）**（**方向转换**：demo → workbuddy 演化 + 工业级流程 setup）

> 📍 **workbuddy 转型 vision**：[`docs/specs/workbuddy-vision.md`](docs/specs/workbuddy-vision.md)（本项目从 demo 演化为可配置多 agent 产品的 MVP 边界；下一阶段主线）
> 📍 工程化改进路线：[`docs/roadmap-ai-engineering.md`](docs/roadmap-ai-engineering.md)（AI 辅助开发的业界实践 + 本项目改进清单，2026-07-14 起草，多数已落地）
> 📍 架构决策记录：[`docs/adr/`](docs/adr/)（每决策一份，从 001 单二进制 / 002 Eino / 003 distroless 起）

## 2026-07-16 上午 方向转换

**旧目标（demo 打磨）已收尾** —— CI / evals / lint / STATUS 同步基线全部到位；`research-goroutine` 意外转绿（详见 2026-07-16 凌晨节 + memory `llm-eval-needs-multiple-samples`）。

**新目标（workbuddy 产品 + 工业级实践载体）** —— 项目**从 demo 演化为 workbuddy 类产品**：页面上配置 subagent / skill / MCP，并且**用它作为工业级 AI 开发实践的载体**：

- 每个特性走 **spec → ADR → feature branch → PR → adversarial code review → eval → 合入** 完整流程
- `main` 分支 branch protection，不允许直推
- 前端 UI 在 Phase 4 一次性升级 shadcn/ui + Tailwind
- 学习优先，产品可以"能用但不完善"

**Phase 划分**（详见 vision spec）：
1. **Phase 0**（本次会话，进行中）：元流程 setup（spec / ADR 模板 + 回溯 ADR + PR 模板 + branch protection + vision spec）
2. Phase 1：MCP 声明式加载（yaml + driver 层）
3. Phase 2：Registry 可变 + Host 原子 swap（`Unregister` / `atomic.Pointer`）
4. Phase 3：REST API（`/api/agents` `/api/mcp` `/api/skills`）
5. Phase 4：配置 UI（shadcn/ui + Tailwind）
6. Phase 5：OTel trace + Langfuse

**Phase 0 产出**（feature branch `docs/init-engineering-flow`）：
- `docs/specs/_template.md` + `docs/adr/_template.md`
- `docs/adr/001-monorepo-single-binary.md`
- `docs/adr/002-eino-as-orchestrator.md`
- `docs/adr/003-distroless-runtime.md`
- `docs/adr/004-branch-protection-on-main.md`（决策：仓库从 Private 转 Public 换取免费 branch protection）
- `docs/specs/workbuddy-vision.md`
- `.github/pull_request_template.md`
- `main` 分支 branch protection（**待做**：仓库转 Public → GitHub Settings 配 required PR + required checks `build-and-test` / `lint`；步骤见 ADR-004 Compliance 节）
- STATUS.md + CLAUDE.md 同步

**已知问题 #1 / #2 归属调整**：
- #1（list_dir 返回 [] 因为容器无 WORKDIR）→ Phase 1 MCP 声明式加载时顺带修（yaml 里配 `default_root`），无需单独 Dockerfile 改动
- #2（filesystem MCP 在容器没起因为没 npx）→ Phase 1 里改为**声明式 enabled_if gate**；本地开发 `ENABLE_FS_MCP=1` 自动 enable，容器无此 env 自动 disable，语义清晰不再"warn-and-continue"

## 2026-07-16 凌晨 变更

**GitHub Secrets 加成 + evals workflow 首跑通**（1 个 commit：`e66a375`）：

- `gh secret set ARK_API_KEY / ARK_MODEL_ID` 已配（用 `--body '值'` 直传，**不**走 stdin —— 第一次用 `printf | gh --body -` 写入时 CI 侧 401 `The API key format is incorrect`，本地同 key 正常，改成明文参数就干净了）
- 手动触发 `evals.yml` × 3：
  - 首跑：secret 传递错，6/6 全 401
  - 二跑：secret 修好，**6/6 全 PASS**（包括预期红的 `research-goroutine`）
  - 三跑：`e66a375` 修 workflow bug 后 rerun，artifact 从 0 bytes → 1133 bytes，`cmd/evals exit=0` 打印出来
- **`research-goroutine` 意外转绿** —— STATUS #3 记的观察 baseline 是 n=1 采样，Ark 模型不同调用之间有波动；当前 6/6 是绿的，改 host prompt 的动机被打掉。#3 归档为"待长期观察，非阻塞"。
- `e66a375 fix(ci): evals workflow captures stderr and honours exit code` —— 修两个坑：
  1. `cmd/evals` 用 `log.Printf` 走 stderr，裸 `| tee` 只抓 stdout → artifact 0 bytes。加 `2>&1`。
  2. bash pipeline 默认取最后一段（`tee` 恒 0）→ workflow 永绿。用 `set +e` + `$PIPESTATUS[0]` 显式取 `cmd/evals` 真实退出码（跟 `ci.yml` smoke test 同一模式）。

**Runtime 未变**（只改 CI workflow + 配 GH secret），无需重部署。

**尚未做（下一次会话优先级 —— 07-16 上午方向转换后已废止，重排见 07-16 上午节 Phase 1+）：**
1. ~~已知问题 #3 归档为"待长期观察"~~ ✅ 已归档，见 07-16 上午方向转换
2. ~~Dockerfile 加 `WORKDIR /data` + 样例文件~~ → 归到 Phase 1 里 MCP yaml `default_root` 声明式解决
3. ~~决定 filesystem MCP 在容器里怎么处理~~ → 归到 Phase 1 `enabled_if` gate
4. GH Actions 各 action 升级到 Node 24 兼容版本，消 deprecation warning（低优先，后续 PR 顺手做）
5. 本机装 `golangci-lint` + `lefthook`（CI 兜底后 nice-to-have）
6. Ark 控制台老 API Key 排查阶段的临时 key 是否 revoke

## 2026-07-15 傍晚 变更

**CI 转绿的过程**（4 个 commit：`2b20bb5` → `af48c9d`）：

| commit | 问题 | 修法 |
|---|---|---|
| `2b20bb5` | smoke test 里 `if server | tee` 因 tee 恒 0 永远走 true 分支 | `set +e` + `$PIPESTATUS[0]` 显式取前段退出码 |
| `028578d` | `golangci-lint-action@v7 version: v2.0` 拉到 v2.0.2（Go 1.24 编），拒绝加载 `go: "1.26"` 的 config | 升到 `version: v2.12`（Go 1.26 编） |
| `fdaa560` | 3 条实际 findings：evals/main.go 未 gofmt、filesystem.go log 日志 taint、server/main.go 7 处 `log.Fatalf` 与 `defer` 组合被 gocritic exitAfterDefer 挑 | gofmt -w / `%s` → `%q` / `log.Fatalf` → `log.Printf + os.Exit(1)` |
| `af48c9d` | 上一 commit 后仍有 2 条：gocritic 认所有"defer + 进程终止"（`os.Exit` 也算）；gosec G706 是静态 taint 追踪，`%q` 运行时转义救不了 | `.golangci.yml` 加两条**精准豁免**，附注释解释 tradeoff |

**关键坑（写进决策日志）：**
- `golangci-lint-action` 用 `version: vX.Y` 系列 pin 会拉到该系列**最老**的 patch 版本，不是最新。Config 里的 `go:` 版本比 binary 的编译 Go 版本高就报 `can't load config`。要么 pin 到具体版本，要么用够新的 major.minor。
- gocritic `exitAfterDefer` 语义广：`log.Fatal` / `os.Exit` / `panic` 都算 —— 修 `log.Fatalf` → `os.Exit` 换汤不换药。
- gosec `G706` 是静态 taint 追踪，格式化 verb 改成 `%q` 只影响运行时，静态分析层面变量还是 tainted。要根治只能 taint 源头（不用 env 变量）或豁免。

**Runtime 未变**（只改 CI 配置 + `cmd/server/main.go` 的错误处理形状），无需重部署。

**尚未做（下一次会话优先级）：**
1. ~~首次 CI run 后看 lint report~~ ✅
2. GitHub 仓库加 `ARK_API_KEY` / `ARK_MODEL_ID` secret → 手动触发一次 evals workflow → 观察 `research-goroutine` 是否红
3. 依据 eval 结果调 host prompt / research description
4. 本机装 `golangci-lint` + `lefthook`（可选，CI 兜底后本机装不装差别不大）
5. （小）actions/*  升级到 Node 24 版本，消除 deprecation warning

## 2026-07-15 下午 变更

- **`.github/workflows/ci.yml`** —— push / PR 时自动跑
  - `build-and-test` job：npm build → stage 前端到 embed 目录 → `go build ./...` → `go vet ./...` → server 冒烟启动（预期缺 Ark env 会 fail-fast）
  - `lint` job：同样先 stage 前端 → `golangci-lint` 全仓扫描（傍晚 batch 转绿后稳定跑）
  - `concurrency.cancel-in-progress`：同分支旧 run 被新 push 覆盖，省 CI 分钟
- **`.github/workflows/evals.yml`** —— 手动 `workflow_dispatch` 触发（暂不 auto-run）
  - 需要仓库 Secrets：`ARK_API_KEY` + `ARK_MODEL_ID`
  - 跑 `go run ./cmd/evals`，report 作为 artifact 上传
  - **待你做**：GitHub 仓库 → Settings → Secrets and variables → Actions 添加两个 secret

## 2026-07-14 / 07-15 上午 变更

三个 commit 已推到 `origin/main`（`98234de` → `30ccabc`）：

| commit | 说明 |
|---|---|
| `6e29d87 chore(deps)` | `go mod tidy` —— `eino` / `eino-ext/*` / `mcp-go` / `yaml.v3` 从 indirect 升到 direct |
| `2aed52c docs` | 新增 `CLAUDE.md` + `docs/roadmap-ai-engineering.md` |
| `c0c5ee7 feat(evals+lint)` | `evals/routing.yaml` + `cmd/evals` + `.golangci.yml` + `lefthook.yml` |
| `54be7be docs(status)` | 07-15 上午 checkpoint |
| `30ccabc docs(architecture)` | ARCHITECTURE.md 同步（时间戳、目录树、§13.7 评估章节改写）|

**验证：** `go build ./...` + `go vet ./...` 均通过。运行时二进制 / 镜像 / pod 行为**未变**，无需 `docker build` / `kubectl apply`。生产 pod 沿用 07-11 部署。

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
- **`.golangci.yml` + `lefthook.yml`** —— lint / pre-commit 配置起草完毕，本地工具链**尚未装**（CI 已兜底）
- **`.github/workflows/ci.yml`** —— push / PR 自动跑 `build + vet + lint + server 冒烟`（07-15 下午）
- **`.github/workflows/evals.yml`** —— 手动触发跑 routing evals（需仓库 Secret `ARK_API_KEY` / `ARK_MODEL_ID`）
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

**2026-07-16 更新**：evals workflow 首跑（run `29466374857`）`research-goroutine` **意外转绿**（`got-agent: research_agent`）。原始观察 baseline 是 n=1 采样，Ark 模型不同调用之间有波动。归档为"待长期观察，非阻塞"—— 定期 rerun 一次 evals 看它会不会重新翻红，红了再调 prompt。

### 4. LLM 跳过 `mcp.echo`
"echo hello agents" 太简单，LLM 直接复述、没触发工具调用。要强制走工具需要在 prompt 里更明示。

---

## 下一步（按顺序，可选）

### 0. 收尾工程化基线（07-14 起草，进行中）
- ✅ `CLAUDE.md` + roadmap + evals 骨架 + lint 配置 + CI workflow
- [ ] **第一次 CI run 后按 lint report 调 `.golangci.yml`** —— 预计需要给 `cmd/` 加豁免或改历史代码
- [ ] **GitHub 仓库加 `ARK_API_KEY` / `ARK_MODEL_ID` secret**（Settings → Secrets and variables → Actions），然后手动触发一次 evals workflow
- [ ] （可选）本机装 `golangci-lint` + `lefthook`（CI 兜底后 nice-to-have）

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
| 2026-07-15 | GitHub Actions CI 拆两个 workflow：`ci.yml` 自动跑 build/vet/lint，`evals.yml` 手动触发 | evals 需要真 Ark key + 花钱，分离触发方式能让 CI 稳定绿而不受 endpoint / 网络影响；等观察一段时间再考虑要不要把 evals 挂到 PR |
| 2026-07-15 | `.golangci.yml` 加两条精准豁免（server exitAfterDefer / filesystem G706）而不是改代码结构 | server bootstrap 抽 `run() error` 只为过 lint 是纯 ceremony，`cancel()` 在失败路径漏跑无害；filesystem taint 源是本地 env 不是网络输入，gosec 判定过严。豁免带 inline 注释解释 tradeoff |
| 2026-07-15 | `golangci-lint-action` pin 到 `version: v2.12`（不是 `v2.0`） | action 用 `vX.Y` 系列 pin 会拉该系列最老 patch（v2.0.2 = Go 1.24 编），拒绝加载 `go: "1.26"` 的 config。升级到 v2.12（2026-05 release，Go 1.26 编）才能真正跑规则 |
| 2026-07-16 | GH Actions secret 走 `gh secret set --body '值'` 明文传参，**不用** `printf \| gh --body -` stdin 管道 | 第一次用 stdin 管道写入后 CI 侧 401 `The API key format is incorrect`，本地同 key 正常，暗示 stdin 通道混入了额外字符。明文参数最不容易翻车 |
| 2026-07-16 | `evals.yml` 用 `2>&1 \| tee` + `$PIPESTATUS[0]` 而不是裸 `\| tee` | `cmd/evals` 用 `log.Printf` → stderr，裸 tee 只抓 stdout → artifact 空；tee 恒 0 → workflow 假绿。这个坑跟 `ci.yml` smoke test 那步同源，写第二次是抄错了模式 |
| 2026-07-16 | 仓库从 Private 转 Public 以启用 `main` 分支 branch protection | GitHub Free 私有仓库不支持 branch protection（`gh api` 实测 403 Upgrade to Pro）；升 Pro $4/mo 或走软性 hook 都 dominated 于"转 Public + 免费 protection"。代码本身是学习成果，公开顺带丰富贡献日历。详见 [ADR-004](docs/adr/004-branch-protection-on-main.md) |

---

## 未决 / TODO

- [x] ~~项目是否建 Git 仓库~~ → 已建（07-10）
- [x] ~~上 k8s~~ → 完成（07-11）
- [x] ~~推 GitHub 远端仓库~~ → 完成（07-11 Private）
- [x] ~~加 `CLAUDE.md` / 工程化 roadmap~~ → 完成（07-14）
- [x] ~~路由回归 eval 骨架~~ → 完成（07-14，红色 case 待实测）
- [x] ~~`golangci-lint` / `lefthook` 配置~~ → 配置起草完成（07-14），本机工具链未装
- [x] ~~GitHub Actions CI~~ → build/vet/lint 自动 + evals 手动（07-15 下午）
- [x] ~~首次 CI run 后按 report 调 `.golangci.yml`~~ → 完成（07-15 傍晚，run `29411625130` 全绿）
- [x] ~~GitHub Secrets 加 `ARK_API_KEY` / `ARK_MODEL_ID`，手动触发 evals workflow~~ → 完成（07-16 凌晨，run `29466566000` 6/6 全绿）
- [x] ~~`go run ./cmd/evals` 实测（或走 CI），`research-goroutine` 转绿~~ → 完成（意外转绿，n=1 baseline 不代表长期）
- [ ] 装 `golangci-lint` + `lefthook`，跑第一次 lint 并按 report 调配置
- [ ] Dockerfile 加 WORKDIR + 样例文件（fix list_dir）
- [ ] 决定 filesystem MCP 在容器里怎么处理（sidecar / 放弃）
- [ ] 老 API Key 排查阶段建的临时 key 是否 revoke
