# ADR 004: `main` 分支 branch protection —— 为此把仓库从 Private 转 Public

> **Status**: Accepted
> **Date**: 2026-07-16
> **Owner**: @Bigmay
> **Related spec(s)**: [workbuddy-vision](../specs/workbuddy-vision.md)

## Context

2026-07-16 上午项目方向从 "demo 打磨" 转向 **workbuddy 演化 + 工业级实践载体**（见 [workbuddy-vision.md](../specs/workbuddy-vision.md)）。vision 的一条硬约束：

> 每个特性走 spec → ADR → feature branch → PR → adversarial code review → eval → 合入。`main` 不允许直推。

`CLAUDE.md` 已把这条写成"AI 助手做事的偏好"里的**硬规矩**（"不直推 main"）。但**规矩只在文档里 = 靠自律**：AI 一时忘了、我一时手快，`git push origin main` 就绕过整个流程。工业级实践的关键是"机器强制"，不是"人诚实"。

要让约束**机器可强制**，方案就是 GitHub **branch protection** —— 在 `main` 上配 required PR、required status checks，服务端拒绝直推。

**约束发现（决策的关键触发因素）**：
- 仓库当前是 **Private**（`mlliu920423-beep/first-agentInK8s`）
- GitHub **免费账户的私有仓库不支持 branch protection** —— 实测 `gh api repos/.../branches/main/protection` 返回 `403 Upgrade to GitHub Pro or make this repository public to enable this feature`
- 官方规则："Free 计划下，branch protection rules 仅对 Public 仓库可用；Private 需要 Pro（$4/mo）或 Team（$4/user/mo）"

所以 ADR 需要同时决策**两件事**：
1. 要不要上 branch protection
2. 为了拿到 branch protection，采用哪条路径（转 Public / 升 Pro / 走软性 guardrail）

## Decision

**采用 branch protection，为此把仓库从 Private 转 Public。**

具体动作：

1. **仓库可见性**：`mlliu920423-beep/first-agentInK8s` **从 Private 转 Public**（GitHub Settings → General → Danger Zone → Change visibility）
2. **`main` 分支保护规则**（转 Public 后配置）：
   - Require a pull request before merging
     - Require approvals: **0**（单人项目，自审即可 —— 通过 PR 描述 + `.github/pull_request_template.md` 自我 checklist 保证质量）
     - Dismiss stale pull request approvals when new commits are pushed: **on**
   - Require status checks to pass before merging
     - **必须过的 checks**：`build-and-test` + `lint`（来自 `.github/workflows/ci.yml`）
     - `evals` **不设 required**（手动 dispatch，且 Ark 调用会花钱，改路由 / prompt 的 PR 在 PR 描述里贴本地 eval 结果即可）
     - Require branches to be up to date before merging: **on**
   - Do not allow bypassing the above settings（含 admin）: **on**（对自己也生效，防"关键时刻手滑"）
   - Restrict pushes that create matching branches: 保持默认
   - Allow force pushes / Allow deletions: **off**

3. **文档同步**：
   - `STATUS.md` 决策日志加 2026-07-16 条目引用本 ADR
   - `CLAUDE.md` 已有的 "不直推 main" 硬规矩加脚注引用 ADR-004
   - workbuddy-vision.md 引用列表补上 ADR-004

## Consequences

### Positive

- **服务端强制**：`git push origin main` 会被 GitHub 直接拒，任何 AI 会话 / 手滑都撞不过。规矩从"文档"升到"闸门"。
- **Required checks 卡质量**：CI 里 `build` / `lint` 挂就没法合，杜绝"绿了才 merge"变成"我觉得应该绿"。
- **PR 强制存在**：`.github/pull_request_template.md` 是每次合入的必经审查点，spec / ADR / eval 变化在 PR 描述里可 review。
- **贡献日历公开可见**：转 Public 后 GitHub profile 页 activity 有可见性，长期学习踪迹可展示。
- **免费**：不用升 Pro。

### Negative

- **代码 100% 公开**：任何人都能看到全部实现。当前仓库无敏感信息（Ark key 在 GitHub Secrets 里、CLAUDE.md 里的 key 是被撤销的老 key、`internal/mcp/filesystem.go` 里也只有本地路径），转 Public 前必须**再扫一遍历史 commit** 确认无泄漏（`git log -p | grep -iE "key|secret|password|token"`，尤其早期 commit）。
- **贡献日历 = 学习记录 = 也 = 学习速度暴露**：不介意（学习项目，进度慢也是自己的节奏）。
- **Public 仓库可能被 crawl**：垃圾 issue / spam PR 概率上升，需要偶尔清理。
- **Admin bypass 关掉后，紧急情况也不能强行 push**：接受这个代价 —— 如果紧急情况真出现，手动开启 → 修 → 关回来，比"平时松着紧急时也松着"好。

### Neutral

- Required approvals 设 0 是单人项目的现实妥协，不是"不 review"。PR 模板 + AI adversarial review（未来会引入）承担 review 责任。**将来加协作者**要立刻改成 1。
- CI 的 `evals` job 保持"手动 dispatch"不设为 required：Ark 调用花真钱，不适合每 PR 都跑；改 host prompt / description 的 PR 由 PR 描述里的手动 eval 结果承担这层验证（见 CLAUDE.md 硬规矩第 5 条）。

## Alternatives Considered

### Alternative 1: 保持 Private，走"软性 guardrail"

不改可见性，用以下手段模拟保护：
- `lefthook` 加 `pre-push` hook 检测 `origin/main`，拒推
- `CLAUDE.md` 硬规矩 + PR 模板 + 自己养成 "先切分支再改" 的习惯
- 加 GitHub Actions on `push: branches: [main]` 报警（例如失败即通知）

**为什么不选**：
- **hook 是客户端**：`git push --no-verify` 一秒绕过；换机器 / clone 时 hook 不自动装。
- **文档 + 习惯 = 没强制**：项目转型的核心命题就是"用机器把纪律固化"，选一个能被自己绕开的方案违背 vision。
- 报警是"事后知道"，不是"事前阻止" —— 差一档。

### Alternative 2: 升级 GitHub Pro（$4/月）

订阅 Pro，Private 仓库拿到 branch protection。

**为什么不选**：
- 花钱可以，但**这个项目本身就是要展示的学习成果**，Public 顺便让贡献日历有价值，没理由为"不想公开"付费。
- 未来如果做**多个**需要 Private + protection 的项目再考虑 Pro，不为一个项目开订阅。
- 学习成本一样，protection 强度一样，选花钱少的。

### Alternative 3: GitHub Rulesets（新一代 ruleset API）而非旧 branch protection

GitHub 2023 起推的 Rulesets 是 branch protection 的替代品，功能更强（正则匹配、bypass 更精细、可组织级复用）。

**为什么不选**：
- **免费 Private 仓库同样不支持**（限制来自计费层，不是 API 层）—— 转 Public 后其实两种都能用。
- 单人单仓项目用不上 Rulesets 的高级功能（组织级复用、精细 bypass），旧 branch protection 就够。
- 兼容性上，旧 API 稳定十年，工具链（`gh api`、Terraform GitHub provider）都成熟。
- 转 Public 后有需要可以随时叠加 Rulesets，本 ADR 不 preclude 未来这么做。

### Alternative 4: 迁移到 GitLab / Gitea 等免费不限制 protection 的托管

自己搭 Gitea 或用 GitLab.com（免费 Private 支持 protected branch）。

**为什么不选**：
- 现有 CI（GitHub Actions）+ Secrets + Issues 都在 GitHub 上，迁移成本 >>> 转 Public。
- workbuddy-vision 明确"学习优先" —— 折腾迁移不是学习内容。
- GitHub 生态（Copilot / gh CLI / Actions marketplace）对学习价值最高，不换平台。

## Compliance / Validation

**转 Public 后的立即验证**：

```bash
# 1. 确认可见性
gh api repos/mlliu920423-beep/first-agentInK8s --jq '.visibility'
# expected: "public"

# 2. 确认 branch protection 已生效
gh api repos/mlliu920423-beep/first-agentInK8s/branches/main/protection --jq '.required_status_checks.contexts,.required_pull_request_reviews.required_approving_review_count,.enforce_admins.enabled'

# 3. 尝试直推 main（应该失败）
git checkout main
git commit --allow-empty -m "test: should be rejected"
git push origin main
# expected: "protected branch update failed" 类错误
git reset --hard HEAD~1  # 回滚这条 empty commit
```

**长期检查**：
- `main` 分支上不允许直接出现 commit —— 每个 commit 都应该是 `Merge pull request #N` 或者 squash merge 后带 PR 编号。任何直接 commit 都是绕过（要么 protection 掉了，要么 admin bypass 被打开）。
- PR 模板里已经有 "spec / ADR 存在" 的 checklist（`.github/pull_request_template.md`）。
- `gh pr list --state merged` 是所有 main commit 的正规入口。

**未来加协作者时的调整点**：
- required approvals 从 0 改为 1（不能 self-approve）
- 明确谁能被指派为 reviewer

## When to revisit

- 引入敏感信息（内部 key / 商业代码 / 客户数据）需要仓库回 Private —— 此时要么升 Pro、要么接受失去 protection、要么迁 GitLab
- 加协作者，需要更严格的 review 流程（required approvals ≥ 1、CODEOWNERS）
- CI checks 结构大变（拆更多 job / 引入新的 required check），需要更新 protection 里的 contexts 列表
- 项目从学习品转向商业产品，需要重新评估"代码公开"的成本
- workbuddy MVP 完成，vision 再转向，可能连仓库结构都要改

## References

- workbuddy-vision.md（本 ADR 是 vision "不直推 main" 硬约束的落地机制）
- CLAUDE.md "AI 助手做事的偏好" 第 4 条（"不直推 main"硬规矩）
- STATUS.md 2026-07-16 上午方向转换节
- `.github/pull_request_template.md`（PR 强制走后的检查点）
- `.github/workflows/ci.yml`（`build-and-test` + `lint` 两个 required checks 来源）
- GitHub Docs: [About protected branches](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches)
- GitHub 计费：[GitHub Free 对 Public/Private 的 branch protection 限制](https://docs.github.com/en/get-started/learning-about-github/githubs-plans)
