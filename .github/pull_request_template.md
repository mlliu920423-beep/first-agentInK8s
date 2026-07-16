<!--
PR 描述模板。红色部分（<...>）请全部替换或删除。
删空的小节可以整节删掉；但 Checklist 那节保留。
-->

## Summary

<一句话讲清楚这个 PR 做了什么、为什么。>

## Motivation

<问题背景 / 用户价值 / 前置 issue 或 spec。>

- Relates to: <spec / issue / prior PR 链接>

## Related spec / ADR

<如果这个 PR 有对应的 `docs/specs/<name>.md` 或 `docs/adr/00X-*.md`，列在这里。**大改动必须有 spec；架构级决策必须有 ADR**（见 `CLAUDE.md`）。>

- Spec: <link 或 "N/A — trivial change">
- ADR: <link 或 "N/A — 无架构决策">

## Changes

<按类别列改动清单。删掉无关类别。>

**Code**
- <改了什么 Go / TS 文件；核心逻辑一两句话>

**Config**
- <yaml / Dockerfile / k8s manifest / CI workflow 变化>

**Docs**
- <STATUS / ARCHITECTURE / CLAUDE 同步；spec / ADR 新增>

**Evals / Tests**
- <新增或改动的 test / eval case>

## Verification

<你怎么验证这个 PR 真的能工作。命令 + 期望输出。>

```bash
# 例：
go build ./... && go vet ./...
go run ./cmd/evals -file evals/routing.yaml
```

<非代码 PR 说明验证方式，比如 "文档 review 已过" 或 "本地 mermaid 图渲染无误">

## Screenshots / Recordings

<UI 改动附截图或 gif；非 UI PR 删掉本节>

## Checklist

<`main` 分支保护要求全绿；prefer 全部勾上，不适用的划掉并注明。>

- [ ] Spec 已写 / 已更新（大改动必须；trivial 改动可 N/A）
- [ ] ADR 已加（架构决策必须；已有决策的延续可 N/A）
- [ ] evals routing 6/6 不退化（涉及 host / agent / prompt 改动时必须）
- [ ] `STATUS.md` 已归档本次变更（合入前最后一步）
- [ ] `CLAUDE.md` 若有新的"跨会话约束"已同步
- [ ] CI 全绿（`build + vet` + `golangci-lint` 两个 job）
- [ ] 本地 `go build ./... && go vet ./...` 通过
- [ ] `MEMORY.md` 若踩到新坑已补一条

## Known tradeoffs / risks

<已知代价、技术债、后续可能重开的问题。别隐瞒。>

- <风险 1>：<描述 + 缓解方式>
- <Tradeoff 1>：<描述>

## Out of scope

<明确"本 PR 不做但相关"的事，避免 review 时被反复问。>

- <未来 PR 会做：...>
