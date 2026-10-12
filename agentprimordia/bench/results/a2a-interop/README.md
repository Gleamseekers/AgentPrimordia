# A2A 开放协议互操作符合性报告（随版发布证据）

> 本目录是 AgentPrimordia **开放 Agent2Agent（A2A）协议互操作面**的对外可引用
> 协议一致性证据，随版本发布。当前结果：**符合性得分 100%（8/8 项检查通过）**。

## 文件

| 文件 | 内容 |
|------|------|
| `a2a-interop-report.json` | demo server 在线暴露 Agent Card 的符合性报告（机器可读，含生成时间/框架版本/生成方式） |
| `a2a-interop-check.txt` | `ap a2a interop-check` CLI 原始输出（逐字保存，CLI 口径） |

## 生成方式（可复现）

两份证据均**不需要运行中的端点**——`a2a.GenerateInteropReport(card, cfg)`
（`internal/agent/a2a/interop_report.go`）是对 Agent Card + 互操作配置的静态
符合性分析（8 项检查：agent_card.name / url / version、capabilities.streaming、
default_input_modes / default_output_modes、agent_card.exposed、io_modes.text）。

1. **demo server card 口径**（`a2a-interop-report.json`）：

   ```bash
   go run ./agentprimordia/ecosystem/examples/a2a-handoff report \
     > agentprimordia/bench/results/a2a-interop/a2a-interop-report.json
   ```

   card 由 demo 的 `buildCard()` 构造，与跨进程 demo（`ecosystem/examples/a2a-handoff`）
   在线经 `/.well-known/agent.json` 暴露的 card 完全一致，因此本报告即该 demo
   server 暴露面的符合性证据。

2. **CLI 口径**（`a2a-interop-check.txt`）：

   ```bash
   go build -o ap ./cmd/ap && ./ap a2a interop-check
   ```

   注意：CLI 使用内置默认 card（`ap-agent` @ `http://localhost:8080`，
   见 `cmd/ap/a2a.go` 的 `runA2AInteropCheck`），与 demo server 的 card 不同；
   两者均 8/8 通过。

## 复跑纪律

- 版本发布或 A2A 协议面（`internal/agent/a2a/`、demo card 构造）变更后应重新
  生成并更新本目录，保持"随版证据"语义；
- 报告中 `framework_version` 取自唯一版本真值 `agentprimordia/pkg/agent.go` 的
  `const Version`（当前 7.5.1，由 `scripts/doc-version-consistency.mjs` 对账）。

## 范围与缺口声明

- 本报告只覆盖**开放协议互操作面**的 schema/配置符合性，不构成对传输性能、
  认证强度或任务持久化的结论；
- **A2A 任务级断点恢复不在当前协议面内**（任务表为进程内内存 map，重启后
  `tasks/get` 返回 `Task not found`，实证 2026-10-12），详见 demo README 的
  "已知缺口"一节。
