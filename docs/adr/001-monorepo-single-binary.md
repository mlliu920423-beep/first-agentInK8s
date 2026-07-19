# ADR 001: 单二进制 + 前端 embed 而非前后端分离部署

> **Status**: Accepted
> **Date**: 2026-07-01（决策实际发生日；ADR 于 2026-07-16 回溯补记）
> **Owner**: @Bigmay
> **Related spec(s)**: —（无 spec，早期直接实现）

## Context

项目从零启动时需要决定 web 前端如何部署：

- **前后端分离**是当前 SaaS 主流：前端 CDN + 后端 API + Nginx / ingress，一个仓库两个部署单元。
- **单二进制**：Go 后端通过 `//go:embed` 把前端 `dist/` 目录嵌进二进制，运行时 HTTP handler 直接从内存提供静态资源。

约束条件：
- 单人项目，目标是**演示多 agent 骨架**，不是搭 SaaS 基础设施。
- 目标运行环境是 **Docker Desktop 内置 k8s**（本地开发） + 未来任意 k8s。
- 不希望管理"前端仓库 vs 后端仓库两版本何时同步"的问题。
- Go 静态编译 + distroless（见 [ADR-003](./003-distroless-runtime.md)）本身就要求"能塞进单二进制的都塞进去"。

## Decision

**采用单二进制 + `//go:embed` 前端 `dist/`。**

具体做法：
- `web/` 下用 React + Vite 开发，`npm run build` 产物到 `web/dist/`
- `internal/webassets/dist/` 目录 `//go:embed all:dist` 到 Go 二进制中
- Dockerfile 三阶段：Node 阶段跑 `npm run build` → Go 阶段 `go build` 时 embed 生效 → distroless 阶段只 COPY 二进制
- HTTP handler `httpapi/static.go` 从 embed FS 提供静态资源，含 SPA fallback

**部署单元 = 单一 Go 二进制 = 单一 Docker image**。

## Consequences

### Positive

- **部署只有一个 artifact**：k8s Deployment 只关心 `eino-demo:local` 一个 image；没有前端 CDN / Nginx / ingress 分层。
- **前后端版本永远同步**：从二进制读的前端一定是编译时那份，不存在"前端部署快于后端"的窗口。
- **本地开发链路短**：`go run ./cmd/server` 就能同时提供 API + 前端；也可以 `npm run dev` 前端热更新 + 反代到 Go。
- **镜像小**：结合 distroless，最终 image ~42MB（压缩 ~10MB）。前端 dist ~几百 KB embed 进来忽略不计。

### Negative

- **前端每次改都要重新 `go build`**：本地开发用 `npm run dev` 走反代，规避；CI 里前端和 Go build 强绑定，任一失败整个 image 挂。
- **单点扩容**：想给前端加 CDN 缓存需要额外一层 Nginx / CloudFront；这个规模用不到。
- **`//go:embed all:dist` 要求目录必须有内容才编译得过**：CI 里必须先 `npm run build` 再 `go build`，任何跳过前端 stage 的路径都会 `pattern all:dist: no matching files found`。这个坑在 CI 首次上线时踩过（CLAUDE.md 已记）。

### Neutral

- 前端框架切换（React → 别的）比传统架构成本略低（只影响 embed 目录内容），但也没差太多 —— 无关紧要。

## Alternatives Considered

### Alternative 1: 前后端分离部署（Nginx + Go API + 两个 image）

一个 Deployment 跑前端静态服务，一个 Deployment 跑 Go API，Ingress 路由。

**为什么不选**：
- 部署复杂度不匹配项目规模。个人 demo 不值得两个 Deployment + Ingress + Service。
- 版本同步问题真实存在（尤其 SSE 事件 schema 前后端各自声明的情况下）—— embed 让前端 build 一定基于当前的 Go 事件 schema。

### Alternative 2: 前端跑独立 Node 服务（Next.js SSR 之类）

前端加一层 SSR，做 SEO 或首屏优化。

**为什么不选**：
- 内部工具/demo 无 SEO 需求。
- SSR 需要前端 runtime，跟 distroless 目标（runtime 极简）矛盾。
- 增加了一门语言的部署管理成本（Node vs Go）。

## Compliance / Validation

- CI 里 build job 顺序**必须**是：npm ci → npm run build → cp dist 到 embed 目录 → go build。见 `.github/workflows/ci.yml`。
- 任何绕过前端 stage 的 build 脚本都会 fail，因为 `//go:embed all:dist` 需要目录有内容。
- `Dockerfile` 三阶段布局也是这个约束的体现，改 Dockerfile 时保持此顺序。

## When to revisit

- 前端加"用户上传大文件 → 直接落对象存储"这类需求，需要前端和后端解耦部署时。
- 需要给前端加复杂 CDN / edge 逻辑时。
- 项目从 demo 演化成真 SaaS，需要独立扩容前后端时（当前 workbuddy 转型规划里明确 MVP 不上真 SaaS，暂不触发）。

## References

- ARCHITECTURE.md §1 (项目一分钟视角) / §11 (部署路径)
- CLAUDE.md "技术栈锁死清单"
- `Dockerfile`（多阶段 build）
- `.github/workflows/ci.yml`（前端 stage 必要性注释）
- `internal/webassets/embed.go`（embed 声明）
