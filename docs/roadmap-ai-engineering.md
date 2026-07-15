# AI 辅助软件开发 —— 工业级实践 & 本项目改进路线

> 起草：2026-07-14。目的：把"vibe coded demo"逐步挪到"有约束的 AI 辅助项目"。
> 状态：**路线图**，不是决策；具体做哪条、什么时候做，收敛后再写进 `STATUS.md` / ADR。

---

## Part 1 —— 业界收敛出的五层实践

### 1. 规格化（Spec-driven / PRD-first）
- **代表**：Amazon Kiro、GitHub Spec Kit、Anthropic `plan mode`、Cursor `Rules for AI`、Cline `Memory Bank`。
- **核心动作**：写代码前先落 `spec.md` / `plan.md`（需求 + 验收标准 + 关键决策）；改动大时先改 spec 再改代码。
- **为什么有效**：把"我以为你懂了"变成 diff 可审的文本；跨会话 / 跨 agent 状态不再靠模型记忆。

### 2. 项目内长期记忆 & 约束
- `CLAUDE.md` / `.cursorrules` / `AGENTS.md`：项目根一个文件，说清技术栈、目录约定、命名、禁止事项、如何跑测试。每次 AI 会话自动加载。
- `.claude/commands/*.md` 自定义 slash 命令：把重复流程（发布前检查、加新 sub-agent）固化成一句调用。
- **关键**：约束要机器可验证（lint / typecheck / test），否则模型仍会漂。

### 3. Guardrails —— 让 AI 的输出必须过闸
- 静态：`golangci-lint` / `ruff` / `eslint` + 严格 typecheck。
- 结构化：pre-commit hook（`lefthook` / `pre-commit`）+ conventional commits。
- 语义：契约测试 / 快照测试 / property-based test（AI 特别容易破坏边界条件）。
- Agentic 侧：Anthropic 官方推 "adversarial verify" —— 一个 agent 用反驳视角审查前一个 agent 的产物。

### 4. Evals —— AI 项目的"单元测试"
- 单测测函数；**eval 测 prompt / agent / 路由**。工具：Braintrust、LangSmith、Promptfoo、Inspect（UK AISI）。
- 多 agent 项目尤其关键：改了 host prompt 之后，"UTC 现在几点" 还路由到 ops 吗？没有 eval 集合，就得每次手点浏览器。
- 最小可用：一个 `evals/cases.yaml`，每条 `{input, expected_route, expected_tool}`，CI 里跑一遍。

### 5. Observability & 回放
- OpenTelemetry + Langfuse / Phoenix / Helicone：LLM 调用、tool call、handoff 全打 trace。
- 回归排查："上次那条走 research，这次走 ops" —— 有 trace 秒定位；没 trace 只能猜。
- Eino 已有 callback（本项目已用 `host.WithAgentCallbacks`），只差把它接到 OTel exporter。

### 补充：新兴模式
- **Worktree-per-task**：Claude Code / Aider 都在推，AI 在隔离 git worktree 里改，改完 diff 上主分支。
- **Multi-agent code review**：PR 上跑 bug / security / perf 三视角独立 review agent，投票合并。
- **Deterministic orchestration**：把多 agent 编排从 prompt 挪到代码（就是本项目 host 干的事，业界正在标准化 —— LangGraph / OpenAI Agents SDK / Eino 同方向）。

---

## Part 2 —— 本项目改进清单（按 投入/回报 从高到低）

### 🟢 立刻能做（1–2 小时，回报巨大）

1. **加 `CLAUDE.md`**（项目根，不是 memory）
   - 内容：项目结构、Ark endpoint 注意事项、Dockerfile 版本、`ctr import` 流程、如何加 sub-agent。
   - 任何 AI（Claude / Cursor / Codex）打开项目都不用重新踩坑。
   - 现状：只有 `.claude/settings.local.json`，没规则文件。

2. **golangci-lint + pre-commit**
   - `.golangci.yml` 起手：`errcheck` / `govet` / `staticcheck` / `gosec` / `revive`。
   - `lefthook.yml` pre-commit 跑 lint + `go vet ./...` + `go build ./...`。
   - AI 生成代码的 90% 低级错误在这里就被拦。

3. **`evals/routing.yaml` + 20 行跑测脚本**
   ```yaml
   - input: "12 乘以 7"
     expect_agent: math_agent
     expect_tool: calculator
   - input: "UTC 现在几点"
     expect_agent: ops_agent
   - input: "帮我查一下 goroutine 调度器"
     expect_agent: research_agent
   ```
   直接解决 STATUS 里"问题 3：host 路由把第三条给了 ops 而不是 research"。改 prompt 从"手点验证"变成量化。

### 🟡 一天内（明显提升可靠性）

4. **CI（GitHub Actions）** ✅ 07-15 下午
   - `build`：`go build ./...` + `npm run build`（前端 embed 要求 dist 存在）。 → `ci.yml/build-and-test`
   - `lint`：`golangci-lint run`。 → `ci.yml/lint`
   - `test`：`go test ./...`（先补 `internal/agentcfg` YAML 解析、`internal/tools` 注册这些纯函数）。 → **暂未做**（还没 test 可跑）
   - `evals`：跑 routing eval（离线 mock model 或便宜 endpoint）。 → `evals.yml`，手动 `workflow_dispatch`，需要仓库 Secret `ARK_API_KEY` / `ARK_MODEL_ID`

5. **OpenTelemetry 埋点**
   - 用现有 `host.WithAgentCallbacks`，把回调事件推给 OTel span。
   - 本地跑 `jaeger` 或 `otel-desktop-viewer`，一条 chat 请求看到 host → specialist → tool 的全链路耗时和参数。
   - 上生产换 exporter 即可。

6. **`STATUS.md` 拆两半**
   - 现状（一句话 + 已知问题 + 下一步）留 `STATUS.md`。
   - 决策记录挪到 `docs/adr/00X-*.md`（**ADR = Architecture Decision Record**），一决策一文件：为什么 Go 1.26、为什么 distroless、为什么 `ctr import` 而不是 registry。
   - AI 改架构前会读 ADR，不会撞已经排除的方案。

### 🟠 一周内的正经工程化

7. **修 STATUS 里 4 个已知问题时用 spec-driven 流程试一次**
   - 例：修 `list_dir` 返回空之前写 `docs/specs/fix-list-dir-empty.md`：现状 / 目标 / 方案 A/B/C / 选定 / 验收。

8. **前端纳入约束**
   - `web/` 现在 vibe 感最重。加 `eslint` + `prettier` + `typescript strict` + `vitest` 跑 SSE 解析。
   - 前后端契约（`/api/chat` 事件 schema）用 JSON Schema 或 Go struct + `zod` 双向生成，别两边手写。

9. **Docker image supply chain**
   - `docker scout` 或 `trivy` 扫 CVE，加进 CI。
   - `distroless/static` 已够干净，主要扫 Go module 依赖。
   - 顺手推到 GHCR，别再手 `ctr import`（STATUS 里已承认是权宜）。

10. **多 agent code review**
    - `.claude/commands/review-pr.md`：起两个 sub-agent，一个查路由逻辑改动，一个查 prompt 改动是否影响 eval，各出一份 review，人工合并。

### 🔵 长期方向（不急）

11. **sub-agent yaml schema 校验**：`agents/schema.json`，加载时校验，防运行时才炸。
12. **kind → k3d / kubevirt / GKE Autopilot**：Docker Desktop image 不共享 k8s 是本地开发环境奇葩，上真 k8s 一劳永逸。
13. **上 Langfuse**：多 agent 自己搭 trace UI 不划算，Langfuse OSS 版 docker-compose 起。

---

## 建议起手三件

如果只做 3 件事，按此顺序：

1. **`CLAUDE.md`**（30 分钟，任何 AI 会话立刻少踩一半坑）
2. **`evals/routing.yaml` + CI 跑**（半天，路由/prompt 问题从"手点"变"PR 红绿"）
3. **golangci-lint + lefthook pre-commit**（1 小时，AI 生成代码下限抬一大截）

做完这三样，项目就从 vibe coded demo 挪到 有约束的 AI 辅助项目。
