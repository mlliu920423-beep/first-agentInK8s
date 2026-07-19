# ADR 003: distroless runtime，不装 shell / node / 包管理器

> **Status**: Accepted
> **Date**: 2026-07-11（决策实际发生日；ADR 于 2026-07-16 回溯补记）
> **Owner**: @Bigmay
> **Related spec(s)**: —

## Context

项目要为 Go 二进制选一个 runtime base image。可选：

| Base image | 大小 | 装了什么 | 攻击面 |
|---|---|---|---|
| `ubuntu:24.04` / `debian:bookworm` | ~78MB / ~80MB | 完整 Linux userland | 大 |
| `alpine:3.20` | ~7MB | musl libc + BusyBox + apk | 中 |
| `gcr.io/distroless/static-debian12:nonroot` | ~2MB | glibc + tls certs + tzdata + nonroot user | 极小（**无 shell**）|

约束条件：
- 后端是 Go **静态编译**二进制（见 [ADR-002](./002-eino-as-orchestrator.md) + [ADR-001](./001-monorepo-single-binary.md)），不依赖动态链接的 libc，可以用 `static` 变体。
- 部署到 **Docker Desktop 内置 k8s**（本地）+ 未来任意 k8s。
- 想尽量小：本地开发反复 `docker build` 快，未来上远端 registry pull 快。
- 供应链安全考量：假设 attacker 找到 Go binary 里的 RCE，容器里应该没有下一步"横向"的工具。

## Decision

**采用 `gcr.io/distroless/static-debian12:nonroot` 作为 runtime base image。**

具体做法（`Dockerfile` 三阶段）：
1. `FROM node:20-alpine` —— 前端 build stage（跑 `npm ci && npm run build`）
2. `FROM golang:1.26` —— Go build stage（cp 前端 dist 到 embed 目录 + `go build -ldflags="-s -w"`）
3. `FROM gcr.io/distroless/static-debian12:nonroot` —— runtime，只 COPY 二进制 + `agents/*.yaml`

image 最终大小：~42MB / 压缩后 ~10MB。

## Consequences

### Positive

- **攻击面极小**：容器里没有 shell，就算 attacker 从 Go binary 找到 RCE，也没法在容器里执行 shell 命令、下载工具、横向移动。是 "assume breach" 思路的兜底。
- **image 小**：跟 alpine 相比也更小，因为没装 BusyBox / apk。上未来的 GHCR / 私有 registry pull 极快。
- **依赖极干净**：只有 glibc（static 变体连这个都省）、ca-certificates、tzdata、passwd 里一条 `nonroot`。CVE 扫描面几乎为零。
- **符合"最小惊喜"**：runtime image 只装应用真需要的东西，避免"以为容器里有 A 结果没有"这类底层 bug；反之你会**很快知道**哪些前置依赖被你的应用假设了。

### Negative

- **不能进容器 debug**：`docker exec -it <pod> sh` 会失败（没有 shell），`ls` / `cat` / `ps` / `top` 都没有。debug 必须靠日志 + `kubectl describe` + `kubectl exec` 换 `distroless/static-debian12:debug` 变体（带 busybox）。
- **子进程类工具跑不了**：任何依赖 `npx` / `node` / `python` / `bash` 的组件在 runtime image 里都失效。**这是当前 STATUS 已知问题 #2 的直接根因**：`internal/mcp/filesystem.go` 用 `npx @modelcontextprotocol/server-filesystem` 起子进程，distroless 里没有 npx 就跳过注册。
- **升级 debian 底层库不方便**：`apt` 不存在，只能等 Google 出新版 distroless image 再重 build。
- **写 Dockerfile / Kubernetes debug 时需要**"提前想清楚"：不能靠 `RUN apt install ...` 补齐运行时依赖。

### Neutral

- Docker Desktop 内置 k8s 的 image store 跟 docker daemon **不共享** —— 每次 `docker build` 后必须 `docker save + docker cp + ctr -n k8s.io images import`。这**不是** distroless 特有的问题（换 alpine 也一样），但和 distroless 一起决策，写进项目 runbook。见 STATUS.md "决策日志" 2026-07-11 那条。

## Alternatives Considered

### Alternative 1: `alpine:3.20`

比 distroless 大 3-4 倍但有 shell + apk，debug 友好。

**为什么不选**：
- 攻击面显著更大（BusyBox / apk 都是 attacker 可用工具）。
- 项目学习目标之一就是**体会"极简 runtime"的取舍**；alpine 是安全惯例，distroless 是安全前沿。
- 装 alpine 也没让 filesystem MCP 那个 `npx` 依赖解决（要装 node 还是得 apk add），没本质好处。

### Alternative 2: `ubuntu:24.04`

完整 Linux userland，无脑最兼容。

**为什么不选**：
- image ~78MB baseline，加 Go 二进制到 ~120MB+。distroless 一半都不到。
- 攻击面巨大，CVE 长期修补压力。
- 项目根本用不到 ubuntu 里 99% 的包。

### Alternative 3: `distroless/static-debian12:debug`（自带 busybox）

distroless 家族的"带调试工具"变体。

**为什么不选**：
- 生产 image 不该带 debug 工具（`debug` tag 明确"生产别用"）。
- 需要 debug 时可以临时把 Dockerfile 最后一行 tag 换成 `:debug` 重 build，做法明确、不污染主线。

### Alternative 4: `scratch`（完全空 image）

比 distroless 还极端，字面 "nothing"。

**为什么不选**：
- 缺 ca-certificates → HTTPS 出去（调 Ark）失败。
- 缺 tzdata → `time.LoadLocation` 拿不到时区。
- 缺 `/etc/passwd` → 无法以 nonroot 跑。
- 手动补齐这些等于自己实现 distroless，白折腾。

## Compliance / Validation

- `Dockerfile` runtime stage 必须是 `gcr.io/distroless/static-debian12:nonroot` —— CLAUDE.md "技术栈锁死清单" 明确不允许换。
- 加新组件（尤其 MCP server / 内置工具）时**先问**："这需不需要 runtime 有 shell / node / python？"
  - 需要 → 走 sidecar container 方案（另起 pod / container 跑那门 runtime），不塞进主 image
  - 不需要 → 纯 Go 实现，放 `internal/`
- CI 里 image build 是标准三阶段；PR 引入新 Dockerfile stage 时要 code review 卡是否破坏 distroless。

## When to revisit

- 项目引入必须的多语言 runtime 组件（例：官方 Python MCP server 无法替代）
- distroless 上游长期不维护 debian12 变体
- 项目从"单二进制 demo" 演化为需要多进程编排的产品（此时正常做法是引 sidecar，不是换 base image）

## References

- ARCHITECTURE.md §11 (部署路径)
- CLAUDE.md "技术栈锁死清单"、"部署：Docker Desktop 内置 k8s 的坑"
- STATUS.md 2026-07-11 决策日志（`golang:1.24 → 1.26` + `ctr import` 流程）
- STATUS.md 已知问题 #2 (filesystem MCP 在容器里没起)
- `Dockerfile`
- Google distroless: https://github.com/GoogleContainerTools/distroless
