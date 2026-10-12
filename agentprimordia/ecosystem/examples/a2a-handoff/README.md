# a2a-handoff — A2A 开放协议跨进程委托 Demo

两个**独立进程**经开放 Agent2Agent（A2A）协议完成全链路委托：

```
client 进程                                server 进程
───────────                              ───────────
FetchAgentCard  ─── GET /.well-known/agent.json ──▶  OpenInteropServer
SendTask        ─── POST /a2a/v1 (tasks/send)  ──▶  Demo Agent（关键词匹配）
GetTask ×N     ─── POST /a2a/v1 (tasks/get)   ──▶  轮询至终态
（结果回传）      ◀── Status.Message ─────────────  ap Agent 响应
CancelTask      ─── POST /a2a/v1 (tasks/cancel) ─▶  取消面演示
```

## 运行

一条命令（任意工作目录，约 5 秒）：

```bash
./run.sh
```

手动分两步（验证确实是两个独立进程）：

```bash
# 终端 1
go run ./agentprimordia/ecosystem/examples/a2a-handoff server
# 终端 2（默认连接 127.0.0.1:9797）
go run ./agentprimordia/ecosystem/examples/a2a-handoff client
```

**零凭据**：未设置 `AP_LLM_API_KEY` 时 server 走 Demo 模式（关键词匹配
provider，与 `ap init --template quickstart` 同一路径），无需任何外部服务。
设置 `AP_LLM_API_KEY` 后同一 demo 自动切换真实 LLM。

`run.sh` 口径（与 CI 门 `.github/workflows/a2a-smoke.yml` 一致）：本地构建
二进制、仓库外临时目录运行、`GOWORK=off`、随机端口、退出前断言关键输出串。

## 环境变量

| 变量 | 默认 | 说明 |
|------|------|------|
| `AP_A2A_HANDOFF_ADDR` | `127.0.0.1:9797` | server 监听 / client 连接地址；`127.0.0.1:0` = 随机端口（run.sh 用） |
| `AP_A2A_HANDOFF_EXEC_DELAY_MS` | `400` | server 模拟任务处理耗时，便于观察轮询；`0` = 立即完成 |

## 已知缺口（诚实登记，2026-10-12 核查）

**A2A 任务级断点恢复不在当前协议面内。** server 的任务表是进程内内存
map（`internal/agent/a2a/interop_server.go` 的 `tasks` 字段；gRPC 侧
`TaskManagerImpl` 同样为内存 map），杀掉 server 重启后原任务不可查——实证：
重启后 `tasks/get` 返回 `Task not found`（错误码 -32001）。框架的
Checkpoint 能力（`internal/persist/`）覆盖 ReActLoop 与 plan 级执行，
不覆盖 A2A 任务。因此本 demo **不含**「杀 server 重启 → 任务状态可查」
情节，也不得出任何恢复能力结论。该缺口已写入交付报告，由版本路线登记。

## 代码结构

| 文件 | 职责 |
|------|------|
| `main.go` | 子命令分发（server / client / report） |
| `card.go` | Agent Card 构造（server 与 report 共用，保证入库报告与在线 card 一致） |
| `executor.go` | 任务执行器：开放协议任务 → ap Agent（Demo 模式）→ 结果回传 |
| `server.go` | server 进程：监听、优雅关闭、协商标识行输出 |
| `client.go` | client 进程：发现 → 委托 → 轮询 → 取回结果 → 取消演示 |
| `report.go` | 离线生成协议符合性报告（JSON，供 `bench/results/a2a-interop/` 入库） |
| `executor_test.go` | 执行器 / card 符合性单测（零外部依赖） |

## 延伸阅读

- 英文概念页：[multi-agent-interop.md](../../../docs/concepts/multi-agent-interop.md)
- API 参考（两套客户端面，勿混用）：[A2A-API.md](../../../docs/api/A2A-API.md)
- 中文概念页：[A2A通信.md](../../../docs/concepts/A2A通信.md)
- 单进程参照 demo（发现→委托→查询→取消）：[a2a-interop](../a2a-interop/main.go)
- 协议符合性自检：`ap a2a interop-check`
