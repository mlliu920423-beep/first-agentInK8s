# Evals

Routing regression tests for the host multi-agent.

## 是什么

Eval 是 AI 项目里对应"单元测试"的东西 —— 传统测试测函数的输入输出，eval 测的是 **prompt / 路由 / agent 行为**。

这里的 `routing.yaml` 每条 case 描述一次用户输入 + 期望的路由结果（哪个 specialist、可选地哪个 tool）。Runner 用真实的 Ark LLM 跑 host multi-agent，观察实际路由和 tool 调用，然后打分。

## 跑

```bash
export ARK_API_KEY=...
export ARK_MODEL_ID=ep-xxxx
go run ./cmd/evals -file evals/routing.yaml
```

可选参数：
- `-file <path>`：case 文件路径，默认 `evals/routing.yaml`
- `-timeout <duration>`：单条 case 超时，默认 60s
- `-agents-dir <path>`：sub-agent yaml 目录，默认 `agents`

退出码 0 = 全 pass，非 0 = 有失败。CI 里可以直接 `go run ./cmd/evals` 卡红绿。

## 输出示例

```
running 6 cases from evals/routing.yaml
case                              want-agent       got-agent        want-tool        verdict
----                              ----------       ---------        ---------        -------
math-basic-multiplication         math_agent       math_agent       calculator       PASS
math-english-arithmetic           math_agent       math_agent       calculator       PASS
ops-utc-time                      ops_agent        ops_agent        current_time     PASS
ops-list-dir                      ops_agent        ops_agent        -                PASS
research-goroutine                research_agent   ops_agent        -                FAIL: routed to [ops_agent], want research_agent
research-weather                  research_agent   research_agent   weather          PASS

total: 6, pass: 5, fail: 1
exit status 1
```

## 加新 case

编辑 `routing.yaml`：

```yaml
- name: <kebab-case-id>
  input: "<user prompt>"
  expect_agent: <agent name from agents/*.yaml>
  expect_tool: <tool name, optional>  # 省略就只断言路由
```

## 已知红色 case（保留在 eval 里）

- `research-goroutine` —— host 目前把它路由到 `ops_agent` 而不是 `research_agent`。这是 [STATUS.md](../STATUS.md) 已知问题 #3。留在 eval 里作为"红色"case，改 prompt / description 时可以看到有没有修好。

## 成本

每条 case 会真实调 Ark LLM（host 一次 + specialist 一次 + 若干 tool 循环），当前 6 条 case 一次跑 ~10 秒、成本很小。**别把 case 列表塞到几百条**，规模真大要跑再考虑：

- 换离线 mock model（Eino 有 mock 支持）
- 或分层：本地小集合 + CI 全量 + PR 触发子集
