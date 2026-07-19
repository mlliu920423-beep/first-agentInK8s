# Phase 4 — 配置 UI（shadcn/ui + Tailwind + react-router-dom）

> **Status**: Draft
> **Owner**: @mlliu920423-beep
> **Related ADR(s)**: ADR-008（待写）
> **Related feature branch / PR**: feat/config-ui（待创建）
> **Last updated**: 2026-07-19

## Context

**现状**：
- Phase 3 已完成：11 个 REST endpoint（`/api/agents/*` + `/api/mcp/*` + `/api/reload`）可配置 Agent/MCP
- 当前前端是一个单页 React 应用（`web/src/App.tsx`），只有 chat 界面，纯 CSS 暗色主题
- 改配置需要 `curl` 或手工编辑 yaml，对普通用户不友好

**痛点**：
1. **没有可视化配置界面** —— Phase 3 的 REST API 只能通过 curl 调用
2. **现有 App.tsx 是 monolith** —— 所有代码（state、渲染、样式、SSE 解析）在 250 行里，再加配置页会不可维护
3. **没有路由** —— 所有 UI 在同一页，没法导航到不同功能模块
4. **纯 CSS 手工维护** —— 没有组件库，没有设计系统

**触发点**：Phase 3 已合入 main，REST API 就位。Vision spec 明确说"Phase 4 完成时 = MVP done"。是时候把 curl 能做的事，变成页面上点按钮就能做。

## Goals

**Phase 4 成功标准**：

- **配置可视化**：打开 `/config/agents` 能看到 3 个 agent 列表，点 [+ New] 能新建
- **零重启生效**：编辑保存后自动调 REST API，chat 路由立刻变化，不需要 reload
- **保持现有 chat 功能不变**：`/` 首页的 chat UI 跟以前一样 work
- **代码可维护**：组件拆分到独立文件，UI 用 shadcn 组件库 + Tailwind 统一风格
- **单页路由**：使用 react-router-dom，SPA fallback 由后端 `static.go` 支持（已有）

### Non-Goals

- ❌ **Auth / 登录页** —— 本地开发 + 单用户，不做 JWT/RBAC
- ❌ **移动端 / 响应式** —— 桌面 web，viewport >= 1024px
- ❌ **暗/亮主题切换** —— 只有暗色
- ❌ **拖拽排序 / 筛选 / 搜索** —— 列表足够短（3-5 个 agent），不需要
- ❌ **回收站 / 版本回滚 API** —— `.trash/` 手工恢复够用
- ❌ **Skills CRUD** —— 内置 tool 是 Go 代码写死的，不能动态增删
- ❌ **测试覆盖（本阶段）** —— UI 组件测试留给后续，本阶段只做功能

## Options Considered

### Option A: shadcn/ui + Tailwind + react-router-dom（vision 方案）

**描述**：
- 重新初始化项目前端，安装 shadcn/ui、Tailwind CSS v4、react-router-dom v7
- 组件拆分到独立文件，路由用 BrowserRouter
- `web/src/` 目录重新组织为 pages/ + components/ + lib/ 结构
- `lib/api.ts` 封装 fetch 调用 REST API

**Pros**：
- shadcn 是 Radix UI 的封装，组件质量高、可访问性好
- Tailwind 设计系统统一，不需要手写 CSS
- react-router-dom 是 React 路由事实标准
- vision spec 已确定此方案

**Cons**：
- 一次性打乱 App.tsx（vision spec §Risks 明确说明）
- shadcn 初始化需要较多依赖安装
- 开发者需要学习 Tailwind 类名

### Option B: 只加路由，保留纯 CSS，不加 UI 库

**描述**：
- 只加 react-router-dom，保留现有纯 CSS 风格
- 不引入 shadcn，不引入 Tailwind
- 配置页面也用手写 CSS

**Pros**：
- 依赖最少
- 现有 chat UI 完全不动

**Cons**：
- 配置页面要手写表单、表格、dialog —— 大量重复劳动
- 可访问性差（没用 Radix）
- 长期维护成本高
- vision spec 已选 shadcn + Tailwind

### Option C: MUI（Material UI）

**描述**：
- 引入 @mui/material 全套组件库

**Pros**：
- 组件最全（表格、分页、自动完成、date picker）
- 成熟稳定

**Cons**：
- 包体积大（~2MB+）
- 自定义样式需要重写 MUI theme
- 跟现有的暗色主题风格差异大
- vision spec 已选 shadcn，不选 MUI

## Decision

**选定 Option A: shadcn/ui + Tailwind + react-router-dom**

**理由**：
1. vision spec 已确定此方案，不用再争论
2. shadcn 组件质量高（基于 Radix UI），配置页面需要的 dialog、form、table、button、badge、card 都有
3. Tailwind 开发效率高，不用来回切换 CSS 文件和 TSX
4. 一次性打乱 App.tsx 是**故意的**—— 现在拆比等 20 个页面再拆容易

## Detailed Design

### 1. 前端目录结构（重组织）

```
web/
├── index.html            # Vite HTML template（加 meta viewport）
├── vite.config.ts        # 不变（已配 proxy + relative base）
├── tsconfig.json         # 不变
├── package.json          # 新增 deps
│
├── src/
│   ├── main.tsx          # React root → <BrowserRouter><App /></BrowserRouter>
│   ├── App.tsx           # Shell layout: sidebar + <Outlet />
│   ├── routes.tsx         # Route definitions
│   │
│   ├── pages/
│   │   ├── ChatPage.tsx       # Chat UI（从当前 App.tsx 抽出）
│   │   ├── ConfigAgents.tsx   # Agent 配置列表 + CRUD
│   │   ├── ConfigMCP.tsx      # MCP server 配置列表 + CRUD
│   │   └── ConfigSkills.tsx   # 内置 tool 概览（只读）
│   │
│   ├── components/
│   │   ├── ui/                 # shadcn 组件（由 CLI 生成）
│   │   │   ├── button.tsx
│   │   │   ├── card.tsx
│   │   │   ├── dialog.tsx
│   │   │   ├── input.tsx
│   │   │   ├── badge.tsx
│   │   │   ├── table.tsx
│   │   │   ├── form.tsx
│   │   │   └── toast.tsx
│   │   ├── AgentForm.tsx       # Agent 编辑 dialog 内容
│   │   ├── MCPForm.tsx         # MCP 编辑 dialog 内容
│   │   ├── ChipView.tsx        # Chat 里的 tool call chip（从 App.tsx 抽出）
│   │   ├── Sidebar.tsx         # 侧边导航
│   │   └── ConfigLayout.tsx    # 配置页通用布局（标题 + 操作按钮 + 列表）
│   │
│   ├── lib/
│   │   ├── api.ts              # REST API client（fetch → /api/agents, /api/mcp）
│   │   ├── types.ts            # AgentConfig, MCPConfig TS 类型
│   │   └── utils.ts            # cn() helper（shadcn 的 className 合并）
│   │
│   ├── sseClient.ts            # SSE 流式解析（不变）
│   │
│   ├── styles.css              # 移除（全部迁移到 Tailwind）
│   ├── globals.css             # Tailwind directives + shadcn CSS variables
│   └── index.css               # Tailwind imports
│
└── components.json             # shadcn init 生成
```

### 2. 路由设计

```
/                   → ChatPage（现有 chat UI）
/config/agents       → ConfigAgents（Agent CRUD）
/config/mcp          → ConfigMCP（MCP CRUD）
/config/skills       → ConfigSkills（只读概览）
```

**Shell layout**（`App.tsx`）：
```
┌─────────────────────────────────────┐
│  Sidebar                            │
│  ┌─────────────────────────────┐    │
│  │  🗨  Chat                    │    │
│  │  ⚙  Agents Config           │    │
│  │  ⚙  MCP Servers             │    │
│  │  ⚙  Skills                  │    │
│  └─────────────────────────────┘    │
│                                     │
│  Content (<Outlet />)               │
│  ┌─────────────────────────────────┐│
│  │                                 ││
│  │  (chat or config page)          ││
│  │                                 ││
│  └─────────────────────────────────┘│
└─────────────────────────────────────┘
```

- Sidebar 宽 ~220px，固定
- 内容区占剩余宽度
- `/config/` 下的页面顶部有统一操作栏（标题 + [+ New] 按钮）

### 3. shadcn 组件初始化

```bash
# 1) 初始化 shadcn（创建 components.json + globals.css）
npx shadcn@latest init -y

# 2) 按需添加组件
npx shadcn@latest add button card dialog input badge table form toast
```

**components.json** 配置：
```json
{
  "style": "default",
  "tailwind": {
    "config": "tailwind.config.js",
    "css": "src/globals.css",
    "baseColor": "neutral",
    "cssVariables": true
  },
  "aliases": {
    "components": "@/components",
    "utils": "@/lib/utils"
  }
}
```

### 4. API Client（`lib/api.ts`）

```typescript
const BASE = '/api';

export async function listAgents(): Promise<AgentConfig[]> {
  const res = await fetch(`${BASE}/agents`);
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

export async function getAgent(name: string): Promise<AgentConfig> {
  const res = await fetch(`${BASE}/agents/${encodeURIComponent(name)}`);
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

export async function createAgent(cfg: AgentConfig): Promise<void> {
  const res = await fetch(`${BASE}/agents`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(cfg),
  });
  if (!res.ok) throw new Error(await res.text());
}

export async function updateAgent(name: string, cfg: AgentConfig): Promise<void> {
  const res = await fetch(`${BASE}/agents/${encodeURIComponent(name)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(cfg),
  });
  if (!res.ok) throw new Error(await res.text());
}

export async function deleteAgent(name: string): Promise<void> {
  const res = await fetch(`${BASE}/agents/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  });
  if (!res.ok) throw new Error(await res.text());
}

// MCP 同理...
export async function listMCPs(): Promise<MCPConfig[]> {
  const res = await fetch(`${BASE}/mcp`);
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}
```

### 5. ConfigAgents 页面流程

```
页面加载 → GET /api/agents → 显示 agent 列表（Card 或 Table）
  → 每个 agent 显示：name, description, tools, max_step

[+ New] → Dialog → AgentForm
  → 表单字段：name, description, system_prompt, tools(多选), max_step
  → Save → POST /api/agents → 关 dialog → 重新 GET 列表

[Edit] → Dialog → AgentForm（预填，name 字段 disabled）
  → Save → PUT /api/agents/{name} → 关 dialog → 重新 GET 列表

[Delete] → Confirm dialog → DELETE /api/agents/{name} → 刷新列表
```

### 6. ConfigMCP 页面流程

同 ConfigAgents，但多了 transport 类型切换：
- `transport` = inproc → 显示 provider + default_root 字段
- `transport` = stdio → 显示 command + args + env + init_timeout 字段

MCP 列表显示 status indicator：
- ⚫ Running（`enabled_if` = always）
- ○ Disabled（`enabled_if` = env:XXX 且环境变量不满足）

### 7. ConfigSkills 页面（只读）

```
页面加载 → 从硬编码列表显示 3 个内置 tool：
  - calculator：由 math_agent 使用
  - current_time：由 ops_agent 使用
  - weather：由 research_agent 使用
```

**没有 CRUD，没有 API 调用。** 只是把"当前可用的内置 tool"信息展示给用户。

### 8. ChatPage 迁移

从现有 `App.tsx` 抽到 `pages/ChatPage.tsx`：
- 保持所有现有功能（SSE 流式、chip 渲染、send 按钮）
- 只把 ChipView 抽成独立组件
- 不再有全局 CSS class，改用 Tailwind
- 样式上保持跟现有一样的视觉风格（暗色、蓝气泡）

### 9. SPA Fallback（后端无需改动）

后端 `internal/httpapi/static.go` 已有 SPA fallback：
```go
if _, err := fs.Stat(root, p); err != nil {
    // SPA fallback — serve index.html
}
```
`/config/agents` 这类路径会 fallback 到 `index.html`，前端 `BrowserRouter` 接管路由。

### 10. 安装步骤

```bash
cd web

# 1) 安装路由
npm install react-router-dom

# 2) 初始化 shadcn
npx shadcn@latest init -y

# 3) 安装组件
npx shadcn@latest add button card dialog input badge table form toast

# 4) 开发
npm run dev  # :5173
```

## Acceptance Criteria

- [ ] `http://localhost:5173/` 打开看到 chat 页面，功能跟之前一样
- [ ] 点击 sidebar "Agents Config" → 看到 3 个 agent 列表（math / ops / research）
- [ ] 点 [+ New] → 填写表单 → Save → 列表出现新 agent
- [ ] 点 [Edit] → 修改 description → Save → 列表更新
- [ ] 点 [Delete] → Confirm → 列表消失
- [ ] MCP 页面能看能编辑，transport 切换展示不同字段
- [ ] Skills 页面显示 3 个内置 tool
- [ ] 所有 mutation 走 REST API
- [ ] `npm run build` 成功
- [ ] `go build ./...` 成功（dist 嵌入 Go 二进制）

## Risks & Tradeoffs

- **前端栈升级一次性打乱 App.tsx** —— vision spec 已明确接受。**缓解**：拆分成 ChatPage + ChipView。
- **shadcn 版本兼容问题** —— 如遇版本冲突，只装需要的组件。
- **没有 undo / 回滚** —— Delete 有 confirm dialog。

## Out of Scope

- UI 单元测试 / E2E 测试
- 移动端 / 响应式
- 暗/亮主题切换
- 排序 / 筛选 / 搜索
- 自定义 CSS 主题
- Loading skeleton / 动画

## Open Questions

- [ ] **Tailwind 版本** — v3（shadcn 稳定）还是 v4（最新）？**倾向**：用 shadcn init 自动生成的版本。
- [ ] **表单方案** — shadcn Form（react-hook-form + zod）还是纯 HTML form？**倾向**：shadcn Form + zod。
- [ ] **状态管理** — 纯 fetch + useState 够用还是需要 tanstack-query？**倾向**：纯 fetch，不引入 query 库。

## References

- **Vision spec Phase 4**：`docs/specs/workbuddy-vision.md`
- **Phase 3 REST API spec**：`docs/specs/phase-3-rest-api-crud.md`
- **当前前端**：`web/src/App.tsx`
- **shadcn/ui**：https://ui.shadcn.com
- **react-router-dom**：https://reactrouter.com
